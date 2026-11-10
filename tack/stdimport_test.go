package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/codegen"
	"github.com/tacklang/tack/internal/stdtest"
)

// stdImportOut is what examples/std-import prints: fmt.println's lines,
// an empty one among them, one holding a newline, greet's line through its
// own import of std/fmt, and a slice of a string.
const stdImportOut = "std ok\n\ntwo\nlines\nhello, tack\nie\n"

// TestBuiltStdImportAtEveryOptLevel builds examples/std-import, a module
// whose main package and greet package both import std/fmt, and
// examples/std-import-file, a single file importing it, at every
// optimization level, and checks what each prints and its status.
func TestBuiltStdImportAtEveryOptLevel(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	for _, tc := range []struct {
		name, target, out string
		code              int
	}{
		{"module", filepath.Join(examplesDir, "std-import"), stdImportOut, 42},
		{"single file", example("std-import-file"), "single file ok\n", 0},
	} {
		for _, opt := range codegen.Opts {
			t.Run(tc.name+"/O"+string(opt), func(t *testing.T) {
				tmp := isolateTemp(t)
				bin := filepath.Join(t.TempDir(), "prog")
				r := runTack(t, "-O"+string(opt), "--out", bin, tc.target)
				if r.code != exitOK || r.stdout != "" || r.stderr != "" {
					t.Fatalf("build: exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
				}
				assertEmptyDir(t, tmp)
				if out, code := runBuilt(t, bin); out != tc.out || code != tc.code {
					t.Errorf("printed %q and exited %d, want %q and %d", out, code, tc.out, tc.code)
				}
			})
		}
	}
}

// stdHome writes a Tack installation into a fresh directory and returns
// it, for TACK_HOME: a std whose builtin and include directories are the
// repository's, and whose other files are files, by path below src/std.
func stdHome(t *testing.T, files map[string]string) string {
	t.Helper()
	under := make(map[string]string, len(files))
	for name, src := range files {
		under["src/std/"+name] = src
	}
	home := writeDir(t, under)
	for _, d := range []string{"builtin", "include"} {
		if err := os.Symlink(
			filepath.Join(stdtest.Dir(), d),
			filepath.Join(home, "src", "std", d),
		); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// utilStd is a std of two packages besides the builtins: util, whose
// extern func add is C, in util.c, behind a pub func, with a lib func and
// a private one the lib func calls, so std stays free of warnings; and
// text, which imports std/util and uses its lib func, as a package of std
// may.
func utilStd() map[string]string {
	return map[string]string{
		"util/util.tck": "package util\n\n" +
			"extern(\"std_util_add\") func add(a int64, b int64) int64\n\n" +
			"pub func Sum(a int64, b int64) int64 {\n    return add(a, b)\n}\n\n" +
			"lib func secret() int64 {\n    return 6 +% hidden()\n}\n\n" +
			"func hidden() int64 {\n    return 1\n}\n",
		"util/util.c": "#include \"tack.h\"\n\n" +
			"int64_t std_util_add(int64_t a, int64_t b) {\n    return a + b;\n}\n",
		"text/text.tck": "package text\n\nimport util from \"std/util\"\n\n" +
			"pub func Total() int64 {\n    return util.Sum(30, util.secret()) +% hidden()\n}\n\n" +
			"func hidden() int64 {\n    return 5\n}\n",
	}
}

// usesUtil is a main package that imports std/text and std/util and exits
// with text.Total() + util.Sum(0, 0), 42 with utilStd, or runs body
// instead when it is not empty.
func usesUtil(body string) string {
	if body == "" {
		body = "    code, fits := int32(text.Total() +% util.Sum(0, 0))\n    if fits { exit(code) }\n"
	}
	return "package main\n\nimport text from \"std/text\"\nimport util from \"std/util\"\n\n" +
		"func main() {\n" + body + "}\n"
}

// TestBuiltCustomStd builds programs against a std of their own, a module
// and a single file, and checks each runs: a std package's C file is
// compiled and linked, a std package imports another and uses its lib
// func, and a program uses a std package's pub func.
func TestBuiltCustomStd(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	home := stdHome(t, utilStd())
	dir := writeDir(t, map[string]string{
		"mod/tack.mod":   "module calc\n",
		"mod/main.tck":   usesUtil(""),
		"single/one.tck": usesUtil(""),
	})
	for _, target := range []string{filepath.Join(dir, "mod"), filepath.Join(dir, "single", "one.tck")} {
		t.Run(filepath.Base(target), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "prog")
			r := runTackEnv(t, t.Context(), envAt(t, home), "--out", bin, target)
			if r.code != exitOK || r.stdout != "" || r.stderr != "" {
				t.Fatalf("build: exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != "" || code != 42 {
				t.Errorf("printed %q and exited %d, want nothing and 42", out, code)
			}
		})
	}
}

// TestCustomStdHidesLibAndPrivate checks a program can use neither a lib
// nor a private name of a std package, from a module or from a single
// file.
func TestCustomStdHidesLibAndPrivate(t *testing.T) {
	home := stdHome(t, utilStd())
	body := "    exit(util.secret() + util.hidden())\n"
	dir := writeDir(t, map[string]string{
		"mod/tack.mod":   "module calc\n",
		"mod/main.tck":   usesUtil(body),
		"single/one.tck": usesUtil(body),
	})
	for _, tc := range []struct{ target, lib string }{
		{filepath.Join(dir, "mod"), "secret is lib in module std and cannot be used from module calc"},
		{
			filepath.Join(dir, "single", "one.tck"),
			"secret is lib in module std and cannot be used from a single file, which has no module",
		},
	} {
		r := runTackEnv(t, t.Context(), envAt(t, home), "--emit", tc.target)
		assertDiagnostics(t, r)
		for _, want := range []string{tc.lib, "hidden is not pub in package std/util"} {
			if !strings.Contains(r.stderr, want) {
				t.Errorf("%s: stderr does not contain %q:\n%s", tc.target, want, r.stderr)
			}
		}
	}
}

// TestCustomStdExternWithoutC checks an extern func of a std package that
// no C file of the package defines is reported at its declaration, in
// std's file, as one of a user package is.
func TestCustomStdExternWithoutC(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	files := utilStd()
	files["util/util.c"] = "int unrelated;\n"
	home := stdHome(t, files)
	src := writeSource(t, []byte(usesUtil("")))
	r := runTackEnv(
		t,
		t.Context(),
		envAt(t, home),
		"--out",
		filepath.Join(t.TempDir(), "prog"),
		src,
	)
	assertDiagnostics(t, r)
	want := filepath.Join(home, "src", "std", "util", "util.tck") +
		":3:29: error: extern func add has no C definition in package util"
	if !strings.Contains(r.stderr, want) {
		t.Errorf("stderr does not contain %q:\n%s", want, r.stderr)
	}
}

// TestEmitHeaderSkipsStd checks --emit-header writes tack.h into the
// program's own package, and not into a std package with C files and an
// extern func, which is not the program's.
func TestEmitHeaderSkipsStd(t *testing.T) {
	home := stdHome(t, utilStd())
	dir := writeDir(t, map[string]string{"tack.mod": "module calc\n", "main.tck": usesUtil("")})
	r := runTackEnv(t, t.Context(), envAt(t, home), "--emit-header", dir)
	if r.code != exitOK || r.stdout != "" || r.stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want a silent success", r.code, r.stdout, r.stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "tack.h")); err != nil {
		t.Errorf("no tack.h in the main package: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "src", "std", "util", "tack.h")); err == nil {
		t.Error("tack.h written into std/util")
	}
}

// TestStdImportWithoutStd checks a program importing a std package, with
// no std where TACK_HOME says, fails with the missing-std error, as every
// program that needs std does.
func TestStdImportWithoutStd(t *testing.T) {
	home := t.TempDir()
	r := runTackEnv(t, t.Context(), envAt(t, home), "--emit", example("std-import-file"))
	if r.code != exitFailure || r.stdout != "" || r.stderr != missingStd(home) {
		t.Errorf(
			"exit %d, stdout %q, stderr %q; want 1 and %q",
			r.code,
			r.stdout,
			r.stderr,
			missingStd(home),
		)
	}
}
