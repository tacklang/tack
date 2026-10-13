package compilertest

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/tacklang/tack/codegen"
	// Registering the backends is what puts them in codegen.Names(), which is
	// what this harness iterates: a new backend gets golden coverage by being
	// imported here.
	_ "github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/invalid"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/typeck"
)

var update = flag.Bool("update", false, "regenerate golden files")

// caseTimeout bounds each pipeline run. The parser has had infinite loops
// before; this turns a hang into a readable failure instead of a stuck suite.
const caseTimeout = 5 * time.Second

const examplesDir = "../examples"

// frontEndBackend is used for the single run that decides whether a program
// even reaches code generation. Any registered backend would answer the same.
const frontEndBackend = "c"

// result is what one pipeline run produced: the stage that ended it, and that
// stage's rendered output.
type result struct {
	stage  string // lex, parse, load, resolve, typeck, codegen, ok, or panic
	output string
}

func (r result) String() string {
	return fmt.Sprintf("stage: %s\n---\n%s", r.stage, r.output)
}

// program is one program the harness compiles: its files, in an in-memory
// file system keyed by slash-separated paths, and what to build from them.
// Those paths are also the names diagnostics show, so keep them
// repo-relative to keep goldens portable.
type program struct {
	files  fstest.MapFS
	target string // a file, or a directory, in files
	single bool   // target is a file, compiled alone
}

// singleFile returns the program the one file name, holding src, makes.
func singleFile(name string, src []byte) program {
	return program{files: fstest.MapFS{name: {Data: src}}, target: name, single: true}
}

// runPipeline mirrors tack/main.go, minus the flag handling and os.Exit calls.
// A problem the loader reports as an error rather than as diagnostics, such
// as a directory with no Tack files, ends the run at the load stage with
// the error's text as its output.
func runPipeline(backendName string, p program) result {
	backend, ok := codegen.Get(backendName)
	if !ok {
		return result{"codegen", fmt.Sprintf("no backend registered under %q", backendName)}
	}

	l := &loader.Loader{FS: loader.FromFS(p.files)}
	var res *loader.Result
	var err error
	if p.single {
		res, err = l.File(p.target)
	} else {
		res, err = l.Dir(p.target)
	}
	if err != nil {
		return result{"load", err.Error() + "\n"}
	}
	if len(res.Diags) > 0 {
		return result{string(res.Stage), diag.RenderAllPretty(res.Files, res.Diags)}
	}

	prog := res.Program
	if resDiags := resolver.New(prog).Resolve(); len(resDiags) > 0 {
		return result{"resolve", diag.RenderAllPretty(res.Files, resDiags)}
	}

	if checkDiags := typeck.New(prog, res.Files).Check(); len(checkDiags) > 0 {
		return result{"typeck", diag.RenderAllPretty(res.Files, checkDiags)}
	}

	out, diags := backend.Generate(prog)
	if len(diags) > 0 {
		return result{"codegen", diag.RenderAllPretty(res.Files, diags)}
	}

	return result{"ok", string(out)}
}

// run executes runPipeline under a timeout, converting hangs and panics into
// test failures rather than letting them take down the run.
func run(t *testing.T, backendName string, p program) result {
	t.Helper()

	ch := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- result{"panic", fmt.Sprintf("%v", r)}
			}
		}()
		ch <- runPipeline(backendName, p)
	}()

	select {
	case r := <-ch:
		if r.stage == "panic" {
			t.Fatalf("pipeline panicked: %s", r.output)
		}
		return r
	case <-time.After(caseTimeout):
		t.Fatalf("pipeline did not terminate within %s (likely an infinite loop)", caseTimeout)
		return result{}
	}
}

// runSource runs the pipeline on src alone, as the file name.
func runSource(t *testing.T, backendName, name string, src []byte) result {
	t.Helper()
	return run(t, backendName, singleFile(name, src))
}

// goldenName turns ../examples/hello into testdata/hello.golden. A backend
// name qualifies it further: with "c" it becomes testdata/hello.c.golden.
//
// Output before code generation is the same whatever the backend is, so those
// cases keep the unqualified name rather than duplicating a file per backend.
func goldenName(dir, backendName string) string {
	rel, err := filepath.Rel(examplesDir, dir)
	if err != nil {
		rel = dir
	}

	name := strings.ReplaceAll(rel, string(filepath.Separator), "_")
	if backendName != "" {
		name += "." + backendName
	}
	return filepath.Join("testdata", name+".golden")
}

// checkGolden compares got against the golden file at path, or rewrites it
// under -update.
func checkGolden(t *testing.T, path, got string) {
	t.Helper()

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating testdata dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v (run `go test ./test -update` to create it)", path, err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s\n\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

// example is one program under examples/: the directory holding it, and
// the program itself.
type example struct {
	dir  string // the directory, as ../examples/<name>
	prog program
}

// findExamples returns every program under examples/: each directory
// directly in it that holds Tack files. A directory holding only main.tck
// is compiled as that file alone; any other is compiled as the package its
// files make together.
func findExamples(t *testing.T) []example {
	t.Helper()

	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Fatalf("reading %s: %v", examplesDir, err)
	}
	var examples []example
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(examplesDir, e.Name())
		if prog, ok := readExample(t, dir); ok {
			examples = append(examples, example{dir: dir, prog: prog})
		}
	}
	if len(examples) == 0 {
		t.Fatalf("no programs found under %s", examplesDir)
	}
	return examples
}

// readExample reads every file under dir into a program, keyed by its path
// without the leading ../, so the names diagnostics show do not depend on
// where the test runs. It reports false for a directory with no Tack files.
func readExample(t *testing.T, dir string) (program, bool) {
	t.Helper()

	files := fstest.MapFS{}
	var sources []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading example: %w", err)
		}
		name := strings.TrimPrefix(filepath.ToSlash(path), "../")
		files[name] = &fstest.MapFile{Data: data}
		if filepath.Ext(path) == ".tck" {
			sources = append(sources, name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
	target := strings.TrimPrefix(filepath.ToSlash(dir), "../")
	switch {
	case len(sources) == 0:
		return program{}, false
	case len(sources) == 1 && path.Base(sources[0]) == "main.tck":
		return program{files: files, target: sources[0], single: true}, true
	default:
		return program{files: files, target: target}, true
	}
}

// TestGolden locks in the current output for every program in examples/. It is
// a regression net, not a correctness claim: the goldens record what the
// compiler does today, so a diff means something changed, not that it broke.
//
// Programs that reach code generation are pinned once per registered backend,
// so adding a backend cannot silently change another one's output.
func TestGolden(t *testing.T) {
	for _, ex := range findExamples(t) {
		t.Run(filepath.ToSlash(ex.dir), func(t *testing.T) {
			if got := run(t, frontEndBackend, ex.prog); got.stage != "ok" {
				checkGolden(t, goldenName(ex.dir, ""), got.String())
				return
			}

			for _, backendName := range codegen.Names() {
				t.Run(backendName, func(t *testing.T) {
					got := run(t, backendName, ex.prog)
					checkGolden(t, goldenName(ex.dir, backendName), got.String())
				})
			}
		})
	}
}

// TestInvalid runs every case in examples/invalid.txtar, one subtest per
// case, and compares the output against the case's expected section, or
// rewrites that section under -update. Like TestGolden it pins what the
// compiler does today; it adds one real assertion, that every case is
// rejected before it compiles.
//
// The program's path in diagnostics is <case>/main.tck, the name its source
// has in the archive.
func TestInvalid(t *testing.T) {
	path := filepath.FromSlash(invalid.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	comment, cases, err := invalid.Parse(data)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(cases) == 0 {
		t.Fatalf("%s holds no cases", path)
	}

	for i := range cases {
		c := &cases[i]
		t.Run(c.Name, func(t *testing.T) {
			got := runSource(t, frontEndBackend, c.Name+"/main.tck", c.Source)
			if got.stage == "ok" {
				t.Fatalf("the program compiled; an invalid example must be rejected")
			}
			if *update {
				c.Expected = []byte(got.String())
				return
			}
			if got.String() != string(c.Expected) {
				t.Errorf("output differs from %s\n\n--- want ---\n%s\n--- got ---\n%s",
					path, c.Expected, got)
			}
		})
	}

	if *update {
		writeInvalid(t, path, data, comment, cases)
	}
}

// writeInvalid writes cases back to the archive at path if they changed
// from old, the archive's current content.
func writeInvalid(t *testing.T, path string, old, comment []byte, cases []invalid.Case) {
	t.Helper()
	data, err := invalid.Format(comment, cases)
	if err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
	if string(data) == string(old) {
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// treeProgram returns the program the files of c make, built from its
// target.
func treeProgram(c invalid.TreeCase) program {
	files := make(fstest.MapFS, len(c.Files))
	for _, f := range c.Files {
		files[c.Name+"/"+f.Path] = &fstest.MapFile{Data: f.Data}
	}
	return program{files: files, target: c.Target()}
}

// TestInvalidPackages is TestInvalid for the cases of several files, in
// examples/invalid-packages.txtar. Each case is built from its directory,
// or the one its .target file names, and diagnostics name its files by
// their paths in the archive.
func TestInvalidPackages(t *testing.T) {
	path := filepath.FromSlash(invalid.TreePath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	comment, cases, err := invalid.ParseTree(data)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(cases) == 0 {
		t.Fatalf("%s holds no cases", path)
	}

	for i := range cases {
		c := &cases[i]
		t.Run(c.Name, func(t *testing.T) {
			got := run(t, frontEndBackend, treeProgram(*c))
			if got.stage == "ok" {
				t.Fatalf("the program compiled; an invalid example must be rejected")
			}
			if *update {
				c.Expected = []byte(got.String())
				return
			}
			if got.String() != string(c.Expected) {
				t.Errorf("output differs from %s\n\n--- want ---\n%s\n--- got ---\n%s",
					path, c.Expected, got)
			}
		})
	}

	if *update {
		writeTree(t, path, data, comment, cases)
	}
}

// writeTree writes cases back to the archive of tree cases at path if they
// changed from old, the archive's current content.
func writeTree(t *testing.T, path string, old, comment []byte, cases []invalid.TreeCase) {
	t.Helper()
	data, err := invalid.FormatTree(comment, cases)
	if err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
	if string(data) == string(old) {
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// TestAcceptsValidPrograms covers syntax the language is meant to accept but
// which no file in examples/ exercises. These assert intended behavior, so a
// failure here is a real bug rather than a stale golden.
func TestAcceptsValidPrograms(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "two top-level functions",
			src:  "package main\n\nfunc other() {\n}\n\nfunc main() {\n    print(\"hi\")\n}\n",
		},
		{
			name: "function body on one line",
			src:  "package main\n\nfunc main() { print(\"hi\") }\n",
		},
		{
			name: "no trailing newline at end of file",
			src:  "package main\n\nfunc main() {\n    print(\"hi\")\n}",
		},
		{
			name: "blank line between statements",
			src:  "package main\n\nfunc main() {\n    print(\"a\")\n\n    print(\"b\")\n}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runSource(t, frontEndBackend, "case.tck", []byte(tt.src))
			if got.stage != "ok" {
				t.Errorf("program should compile, but failed at the %s stage:\n%s\nsource:\n%s",
					got.stage, got.output, tt.src)
			}
		})
	}
}

// TestRejectsInvalidPrograms checks that malformed input is reported by the
// stage that should notice it, with a message that names the actual problem.
func TestRejectsInvalidPrograms(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		wantStage string
		wantMsg   string
	}{
		{
			name:      "unterminated string mid-line",
			src:       "package main\n\nfunc main() {\n    print(\"oops)\n}\n",
			wantStage: "lex",
			wantMsg:   "unterminated string",
		},
		{
			name:      "unterminated string at end of file",
			src:       "package main\n\nfunc main() {\n    print(\"oops",
			wantStage: "lex",
			wantMsg:   "unterminated string",
		},
		{
			name:      "invalid character",
			src:       "package main\n\nfunc main() {\n    @ print(\"hi\")\n}\n",
			wantStage: "lex",
			wantMsg:   "invalid character",
		},
		{
			name:      "missing package clause",
			src:       "func main() {}\n",
			wantStage: "parse",
			wantMsg:   "expected 'package'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A neutral filename: the case name must not leak into the rendered
			// output, or the wantMsg check below could match it by accident.
			got := runSource(t, frontEndBackend, "case.tck", []byte(tt.src))
			if got.stage != tt.wantStage {
				t.Errorf("stage = %q, want %q\noutput:\n%s", got.stage, tt.wantStage, got.output)
			}
			if !strings.Contains(got.output, tt.wantMsg) {
				t.Errorf("output does not mention %q:\n%s", tt.wantMsg, got.output)
			}
		})
	}
}

// TestCRLFMatchesLF runs every example and every invalid case again with
// each \n turned into \r\n, and checks the result is the same: the stage
// that ends the run, its diagnostics with their lines, columns and source
// context, or the generated code. Line endings must not change anything a
// program means or anything said about it. Cases that already hold a
// carriage return are left out, since adding one before a line feed
// changes what that byte is. A marker under a source line is compared up
// to its caret: a span that runs past the end of its line, such as an
// unterminated raw string's, is underlined for every byte it covers, and
// covers one more for each carriage return.
func TestCRLFMatchesLF(t *testing.T) {
	type named struct {
		name string
		prog program
	}
	examples := findExamples(t)
	cases, err := invalid.Load(invalid.Path)
	if err != nil {
		t.Fatal(err)
	}
	trees, err := invalid.LoadTree(invalid.TreePath)
	if err != nil {
		t.Fatal(err)
	}
	programs := make([]named, 0, len(examples)+len(cases)+len(trees))
	for _, ex := range examples {
		programs = append(programs, named{ex.prog.target, ex.prog})
	}
	for _, c := range cases {
		programs = append(programs, named{c.Name, singleFile(c.Name+"/main.tck", c.Source)})
	}
	for _, c := range trees {
		programs = append(programs, named{"tree/" + c.Name, treeProgram(c)})
	}

	for _, p := range programs {
		crlf, ok := withCRLF(p.prog)
		if !ok {
			continue
		}
		t.Run(p.name, func(t *testing.T) {
			lf := run(t, frontEndBackend, p.prog)
			crlf := run(t, frontEndBackend, crlf)
			if carets(crlf.String()) != carets(lf.String()) {
				t.Errorf("CRLF gave\n%s\nwant, as with LF,\n%s", crlf, lf)
			}
		})
	}
}

// withCRLF returns p with every \n of every file turned into \r\n, and
// false if any file already holds a carriage return.
func withCRLF(p program) (program, bool) {
	files := make(fstest.MapFS, len(p.files))
	for name, f := range p.files {
		if bytes.ContainsRune(f.Data, '\r') {
			return program{}, false
		}
		files[name] = &fstest.MapFile{Data: bytes.ReplaceAll(f.Data, []byte("\n"), []byte("\r\n"))}
	}
	p.files = files
	return p, true
}

// carets returns out, rendered output, with every marker line cut after
// its caret.
func carets(out string) string {
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "   |\t") {
			if c := strings.IndexByte(line, '^'); c >= 0 {
				lines[i] = line[:c+1]
			}
		}
	}
	return strings.Join(lines, "\n")
}

// TestLexIdentifierAtEOF pins the tokenisation of an identifier that ends the
// file, which the scanning loops' upper bound gets wrong.
func TestLexIdentifierAtEOF(t *testing.T) {
	src := []byte("package main") // no trailing newline; "main" is the last token

	f := &source.File{ID: source.FileID("test"), Filename: "eof.tck", Data: src}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatalf("unexpected diagnostics: %s", diag.RenderAllPretty(f, diags))
	}

	var idents []string
	for _, tk := range lex.Tokens() {
		if tk.Kind() == lexer.Ident {
			idents = append(idents, fmt.Sprintf("%v", tk.Value()))
		}
	}

	want := []string{"main"}
	if len(idents) != len(want) || idents[0] != want[0] {
		t.Errorf("identifiers = %q, want %q (the trailing identifier is being split)", idents, want)
	}
}
