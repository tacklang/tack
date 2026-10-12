package compilertest

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tacklang/tack/codegen"
	// Registering the backends is what puts them in codegen.Names(), which is
	// what this harness iterates: a new backend gets golden coverage by being
	// imported here.
	_ "github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
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
	stage  string // lex, parse, resolve, typeck, codegen, ok, or panic
	output string
}

func (r result) String() string {
	return fmt.Sprintf("stage: %s\n---\n%s", r.stage, r.output)
}

// runPipeline mirrors tack/main.go, minus the flag handling and os.Exit calls.
// filename is used only for diagnostic rendering, so pass a repo-relative path
// to keep goldens portable.
func runPipeline(backendName, filename string, src []byte) result {
	backend, ok := codegen.Get(backendName)
	if !ok {
		return result{"codegen", fmt.Sprintf("no backend registered under %q", backendName)}
	}

	f := &source.File{
		ID:       source.FileID("test"), // fixed, not a UUID, so output is deterministic
		Filename: filename,
		Data:     src,
	}

	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		return result{"lex", diag.RenderAllPretty(f, diags)}
	}

	p := parser.NewParser(f.ID, lex.Tokens())
	file, diags := p.Parse()
	if len(diags) > 0 {
		return result{"parse", diag.RenderAllPretty(f, diags)}
	}

	if resDiags := resolver.New(file).Resolve(); len(resDiags) > 0 {
		return result{"resolve", diag.RenderAllPretty(f, resDiags)}
	}

	if checkDiags := typeck.New(file).Check(); len(checkDiags) > 0 {
		return result{"typeck", diag.RenderAllPretty(f, checkDiags)}
	}

	out, diags := backend.Generate(file)
	if len(diags) > 0 {
		return result{"codegen", diag.RenderAllPretty(f, diags)}
	}

	return result{"ok", string(out)}
}

// run executes runPipeline under a timeout, converting hangs and panics into
// test failures rather than letting them take down the run.
func run(t *testing.T, backendName, filename string, src []byte) result {
	t.Helper()

	ch := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- result{"panic", fmt.Sprintf("%v", r)}
			}
		}()
		ch <- runPipeline(backendName, filename, src)
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

// goldenName turns ../examples/invalid/garbage/main.tck into
// testdata/invalid_garbage.golden. A backend name qualifies it further:
// ../examples/hello/main.tck with "c" becomes testdata/hello.c.golden.
//
// Output before code generation is the same whatever the backend is, so those
// cases keep the unqualified name rather than duplicating a file per backend.
func goldenName(path, backendName string) string {
	rel, err := filepath.Rel(examplesDir, filepath.Dir(path))
	if err != nil {
		rel = filepath.Dir(path)
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

func findExamples(t *testing.T) []string {
	t.Helper()

	var paths []string
	err := filepath.WalkDir(examplesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".tck" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", examplesDir, err)
	}
	if len(paths) == 0 {
		t.Fatalf("no .tck files found under %s", examplesDir)
	}
	return paths
}

// TestGolden locks in the current output for every program in examples/. It is
// a regression net, not a correctness claim: the goldens record what the
// compiler does today, so a diff means something changed, not that it broke.
//
// Programs that reach code generation are pinned once per registered backend,
// so adding a backend cannot silently change another one's output.
func TestGolden(t *testing.T) {
	for _, path := range findExamples(t) {
		t.Run(filepath.ToSlash(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}

			// Strip the leading ../ so the filename in rendered diagnostics is
			// stable regardless of where the test is invoked from.
			name := strings.TrimPrefix(filepath.ToSlash(path), "../")

			if got := run(t, frontEndBackend, name, src); got.stage != "ok" {
				checkGolden(t, goldenName(path, ""), got.String())
				return
			}

			for _, backendName := range codegen.Names() {
				t.Run(backendName, func(t *testing.T) {
					got := run(t, backendName, name, src)
					checkGolden(t, goldenName(path, backendName), got.String())
				})
			}
		})
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
			got := run(t, frontEndBackend, "case.tck", []byte(tt.src))
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
			got := run(t, frontEndBackend, "case.tck", []byte(tt.src))
			if got.stage != tt.wantStage {
				t.Errorf("stage = %q, want %q\noutput:\n%s", got.stage, tt.wantStage, got.output)
			}
			if !strings.Contains(got.output, tt.wantMsg) {
				t.Errorf("output does not mention %q:\n%s", tt.wantMsg, got.output)
			}
		})
	}
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
