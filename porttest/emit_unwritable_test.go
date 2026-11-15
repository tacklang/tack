package porttest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// unwritableHeaderSrc is the program TestEmitHeaderUnwritable writes
// tack.h for: an extern func, so the main package's directory gets a
// tack.h, and a function nothing calls, so the run has a warning to show
// before the write fails.
const unwritableHeaderSrc = `package main

extern func twice(x int64) int64

func unused() {
}

func main() {
    if twice(2) > 3 {
        exit(0)
    }
}
`

// unwritableHeaderC is the C file beside it, which defines the extern
// func.
const unwritableHeaderC = "#include \"tack.h\"\n\nint64_t twice(int64_t x) { return x * 2; }\n"

// TestEmitHeaderUnwritable runs --emit-header, with --check and without,
// in a tree whose tack.h is a directory, so the write fails after the
// program is lowered: each compiler in a tree of its own, built afresh in
// a temporary directory. The port must answer as Go does, its warnings
// and then cannot write: it words the error a directory gives itself, so
// a decline, which it once made here, fails too. It runs only as TestPort
// does.
func TestEmitHeaderUnwritable(t *testing.T) {
	ready(t)
	tree := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		files := map[string]string{"main.tck": unwritableHeaderSrc, "helpers.c": unwritableHeaderC}
		for name, src := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Mkdir(filepath.Join(dir, "tack.h"), 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	for name, args := range map[string][]string{
		"emit-header":       {"--emit-header", "main.tck"},
		"check-emit-header": {"--check", "--emit-header", "main.tck"},
	} {
		t.Run(name, func(t *testing.T) {
			want := runCompiler(t, env.goBin, env.goCache, input{name: "main.tck", dir: tree(t), file: "main.tck"}, args)
			if want.code == 0 {
				t.Fatalf("Go wrote tack.h over a directory:\n%s", want.stderr)
			}
			got := runCompiler(t, env.portRun, env.portCache, input{name: "main.tck", dir: tree(t), file: "main.tck"}, args)
			if got.notImplemented() {
				t.Fatalf("the port declines: %s", got.stderr)
			}
			if got.code != want.code || !bytes.Equal(got.stdout, want.stdout) ||
				!bytes.Equal(got.stderr, want.stderr) {
				t.Errorf("the port exited %d, writing\n%s\nGo exited %d, writing\n%s",
					got.code, got.stderr, want.code, want.stderr)
			}
		})
	}
}
