package porttest

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
)

// embeddedFile is where the port keeps the files Go's compiler embeds
// with go:embed, relative to its source tree's root, as tools/embed
// writes them.
const embeddedFile = "builtin/embedded.tck"

// TestEmbedded checks the port's embedded files against this module's
// originals, which Go's builtin package embeds: that embedded.tck, read
// by this module's lexer and parser, as the Go compiler reads it when it
// builds the port, holds builtin/builtin.tck as doc, the runtime's
// header, builtin/runtime/tack/tack.h, as header, and every C file of
// builtin/runtime, in the byte order of their names, as Go's
// RuntimeCFiles lists them, as runtime_names and runtime_data, byte for
// byte; and that the port's tools/embed, built with the Go compiler and
// run on the originals, writes embedded.tck as it is, so the file is the
// tool's and nothing else's. It needs the port's source tree, and skips
// without one only when TACK_PORT_BIN names the port. It runs only as
// TestPort does, with TACK_PORT_TEST=1 and -count.
func TestEmbedded(t *testing.T) {
	ready(t)
	src, _ := portSource(os.Getenv("TACK_PORT"))
	committed, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(embeddedFile)))
	if err != nil {
		if os.Getenv("TACK_PORT_BIN") != "" && !dirExists(filepath.Join(src, "driver")) {
			t.Skip("no port source tree to check: TACK_PORT_BIN names the port")
		}
		t.Fatal(err)
	}
	vars, err := embeddedStrings(committed)
	if err != nil {
		t.Fatal(err)
	}
	builtinDir := filepath.Join(env.root, "builtin")
	cFiles, err := filepath.Glob(filepath.Join(builtinDir, "runtime", "*.c"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(cFiles)
	wantNames := make([]string, len(cFiles))
	wantData := make([]string, len(cFiles))
	for i, c := range cFiles {
		wantNames[i] = filepath.Base(c)
		wantData[i] = readString(t, c)
	}
	want := map[string][]string{
		"doc":           {readString(t, filepath.Join(builtinDir, "builtin.tck"))},
		"header":        {readString(t, filepath.Join(builtinDir, "runtime", "tack", "tack.h"))},
		"runtime_names": wantNames,
		"runtime_data":  wantData,
	}
	for name, w := range want {
		got, ok := vars[name]
		switch {
		case !ok:
			t.Errorf("%s declares no %s", embeddedFile, name)
		case len(got) != len(w):
			t.Errorf("%s's %s holds %d strings, want %d", embeddedFile, name, len(got), len(w))
		default:
			for i := range w {
				if d := firstDiff([]byte(w[i]), []byte(got[i])); d != "" {
					t.Errorf("%s's %s[%d] differs from the original: %s", embeddedFile, name, i, d)
				}
			}
		}
	}
	if len(vars) != len(want) {
		t.Errorf("%s declares %d globals, want %d", embeddedFile, len(vars), len(want))
	}

	written := runEmbedTool(t, src, builtinDir, cFiles)
	if d := firstDiff(committed, written); d != "" {
		t.Errorf("%s is not what tools/embed writes; run make port-embed: %s", embeddedFile, d)
	}
}

// readString returns the bytes of the file p as a string.
func readString(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p) //nolint:gosec // G304: a file of this module
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// embeddedStrings lexes and parses data, a Tack source file of globals
// whose values are string literals or arrays of them, and returns each
// global's strings, by name, as the lexer decodes them.
func embeddedStrings(data []byte) (map[string][]string, error) {
	f := &source.File{ID: embeddedFile, Filename: embeddedFile, Data: data}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		return nil, fmt.Errorf("%s does not lex: %s", embeddedFile, diags[0].Message)
	}
	tree, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
	if len(diags) > 0 || tree == nil {
		return nil, fmt.Errorf("%s does not parse", embeddedFile)
	}
	out := map[string][]string{}
	for _, d := range tree.Decls {
		v, ok := d.(*ast.Var)
		if !ok {
			return nil, fmt.Errorf("%s declares something other than a global", embeddedFile)
		}
		switch x := v.Value.(type) {
		case *ast.StrLit:
			out[v.Name] = []string{x.Value}
		case *ast.CompositeLit:
			var strs []string
			for _, e := range x.Elems {
				s, ok := e.(*ast.StrLit)
				if !ok {
					return nil, fmt.Errorf("%s's %s holds something other than a string", embeddedFile, v.Name)
				}
				strs = append(strs, s.Value)
			}
			out[v.Name] = strs
		default:
			return nil, fmt.Errorf("%s's %s is no string or array of strings", embeddedFile, v.Name)
		}
	}
	return out, nil
}

// runEmbedTool builds the port's tools/embed, in its source tree src,
// with the Go compiler, and runs it on the originals in builtinDir, the
// runtime's C files cFiles among them, in order, writing into a
// temporary file, and returns what it wrote.
func runEmbedTool(t *testing.T, src, builtinDir string, cFiles []string) []byte {
	t.Helper()
	tmp := t.TempDir()
	tool := filepath.Join(tmp, "embed")
	build := exec.Command(env.goBin, "--out", tool, "./tools/embed")
	build.Dir = src
	build.Env = compilerEnv(env.goCache)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building tools/embed: %v\n%s", err, out)
	}
	out := filepath.Join(tmp, "embedded.tck")
	args := []string{
		out, "builtin",
		"doc=" + filepath.Join(builtinDir, "builtin.tck"),
		"header=" + filepath.Join(builtinDir, "runtime", "tack", "tack.h"),
	}
	for _, c := range cFiles {
		args = append(args, "runtime[]="+c)
	}
	run := exec.Command(tool, args...) //nolint:gosec // G204: the tool just built
	var stderr bytes.Buffer
	run.Stderr = &stderr
	if err := run.Run(); err != nil {
		t.Fatalf("running tools/embed: %v\n%s", err, stderr.Bytes())
	}
	written, err := os.ReadFile(out) //nolint:gosec // G304: the file the tool wrote
	if err != nil {
		t.Fatal(err)
	}
	return written
}
