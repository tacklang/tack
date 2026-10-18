package main

import (
	"os"
	"path/filepath"
	"testing"
)

// plantedRuntime are files a TACK_HOME may hold where the builtins and
// the runtime header once came from, each written to break a build that
// read it: a std/builtin declaring a builtin that is not one, C files of
// the builtins that do not compile, and a <tack/tack.h> that stops any C
// including it.
var plantedRuntime = map[string]string{
	"src/std/builtin/builtin.tck":  "package builtin\n\nvar x = 1\n",
	"src/std/builtin/print.c":      "#error planted print.c\n",
	"src/std/builtin/exit.c":       "#error planted exit.c\n",
	"src/std/builtin/list.c":       "#error planted list.c\n",
	"src/std/builtin/planted.c":    "#error planted planted.c\n",
	"src/std/include/tack/tack.h":  "#error planted tack.h\n",
	"src/std/builtin/extra.tck":    "package builtin\n\nextern(\"tack_builtin_x\") func x()\n",
	"src/std/include/tack/extra.h": "#error planted extra.h\n",
}

// The builtins and the runtime are built into the compiler: whatever a
// TACK_HOME holds where they once came from changes nothing about a
// build. A program calling print and exit, with a list, which needs every
// one of the runtime's C files and its header, builds and runs as it does
// with the repository's std, and cannot call a builtin a planted file
// declares.
func TestPlantedRuntimeChangesNothing(t *testing.T) {
	needClang(t)
	home := copyStd(t)
	for rel, src := range plantedRuntime {
		path := filepath.Join(home, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	src := program(t, "    mut l := make(list[int64], c_allocator)\n"+
		"    e := append(mut l, 4)\n    if e.code != 0 {\n        return\n    }\n"+
		"    print(\"hi\\n\")\n    exit(3)\n")
	bin := filepath.Join(t.TempDir(), "bin")
	r := runTackEnv(t, t.Context(), envAt(t, home), "--out", bin, src)
	if r.code != exitOK || r.stderr != "" {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if out, code := runBuilt(t, bin); out != "hi\n" || code != 3 {
		t.Errorf("printed %q, exited %d; want %q and 3", out, code, "hi\n")
	}

	r = runTackEnv(t, t.Context(), envAt(t, home), "--check", program(t, "    x()\n"))
	assertDiagnostics(t, r)
}
