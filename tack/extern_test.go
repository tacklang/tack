package main

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/codegen"
	"github.com/tacklang/tack/internal/invalid"
)

// TestBuiltExternExits42 builds examples/extern, whose extern funcs are
// written in the helpers.c beside it, and checks every value that crossed
// the boundary came back as Tack expects: each integer width, the extremes
// of int64 and uint64, bools, and state kept on the C side.
func TestBuiltExternExits42(t *testing.T) {
	bin := buildWithClang(t, "extern")
	if out, code := runBuilt(t, bin); out != "extern ok\n" || code != 42 {
		t.Errorf("printed %q and exited %d, want %q and 42", out, code, "extern ok\n")
	}
}

// TestBuiltExternFromItsDirectory builds examples/extern from its
// directory, where it is still a single file with no tack.mod, so the
// .c files beside the target are linked the same way.
func TestBuiltExternFromItsDirectory(t *testing.T) {
	t.Chdir(filepath.Join(examplesDir, "extern"))
	bin := buildTargetWithClang(t, "extern", "main.tck")
	if out, code := runBuilt(t, bin); out != "extern ok\n" || code != 42 {
		t.Errorf("printed %q and exited %d, want %q and 42", out, code, "extern ok\n")
	}
}

// update rewrites the expected sections of examples/invalid-c.txtar.
var update = flag.Bool("update", false, "rewrite the expected output of examples/invalid-c.txtar")

// TestInvalidC builds every case of examples/invalid-c.txtar with clang,
// from a fresh directory holding its files, and checks the build fails
// with the expected output on stderr and nothing on stdout. When clang
// rejects a C file, its own report, which varies with its version, comes
// first; the expected output then holds what tack says after it, and the
// test checks clang's report is there and names an error.
func TestInvalidC(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	path := filepath.FromSlash(invalid.CPath)
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
			got := buildInvalidC(t, *c)
			if *update {
				c.Expected = []byte(got)
				return
			}
			if got != string(c.Expected) {
				t.Errorf("output differs from %s\n\n--- want ---\n%s\n--- got ---\n%s",
					path, c.Expected, got)
			}
		})
	}
	if *update {
		rewriteInvalidC(t, path, comment, cases)
	}
}

// rewriteInvalidC writes comment and cases to the archive at path.
func rewriteInvalidC(t *testing.T, path string, comment []byte, cases []invalid.TreeCase) {
	t.Helper()
	out, err := invalid.FormatTree(comment, cases)
	if err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// buildInvalidC writes the files of c to a fresh directory, builds its
// target from there, and returns what tack wrote to stderr from its first
// line, after checking the build failed, wrote nothing to stdout, and left
// nothing in the temp directory.
func buildInvalidC(t *testing.T, c invalid.TreeCase) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range c.Files {
		name := filepath.Join(root, c.Name, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, f.Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	tmp := isolateTemp(t)
	r := runTack(t, "--out", filepath.Join(t.TempDir(), "bin"), filepath.FromSlash(c.Target()))
	if r.code != exitFailure || r.stdout != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want a failed build", r.code, r.stdout, r.stderr)
	}
	assertEmptyDir(t, tmp)
	if strings.HasPrefix(r.stderr, "tack: ") {
		return r.stderr
	}
	i := strings.Index(r.stderr, "\ntack: cannot compile ")
	if i < 0 {
		return r.stderr
	}
	if before := r.stderr[:i]; !strings.Contains(before, "error") {
		t.Errorf("clang's report before tack's does not name an error:\n%s", before)
	}
	return r.stderr[i+1:]
}

// addProgram is a single-file program whose one extern func, add, its C
// file defines, and which exits with add(40, 2).
const addProgram = "package main\n\nextern func add(a int64, b int64) int64\n\n" +
	"func main() {\n    code, _ := int32(add(40, 2))\n    exit(code)\n}\n"

// addC defines add for addProgram.
const addC = "int64_t add(int64_t a, int64_t b) {\n    return a + b;\n}\n"

// buildIn builds main.tck, in dir, from dir, and returns the result and
// the binary's path.
func buildIn(t *testing.T, dir string) (result, string) {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	t.Chdir(dir)
	tmp := isolateTemp(t)
	bin := filepath.Join(t.TempDir(), "prog")
	r := runTack(t, "--out", bin, "main.tck")
	assertEmptyDir(t, tmp)
	return r, bin
}

// TestEmitHeaderWritesTackH checks --emit-header writes tack.h into the
// directory of a single-file target, prints nothing, and that the C file
// beside it, which includes it by name, then compiles and links.
func TestEmitHeaderWritesTackH(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"main.tck":  addProgram,
		"helpers.c": "#include \"tack.h\"\n\n" + addC,
	})
	r := runTack(t, "--emit-header", filepath.Join(dir, "main.tck"))
	if r.code != exitOK || r.stdout != "" || r.stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want a silent success", r.code, r.stdout, r.stderr)
	}
	header, err := os.ReadFile(filepath.Join(dir, "tack.h"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(header), "int64_t add(int64_t /* a */, int64_t /* b */);") {
		t.Errorf("tack.h does not declare add:\n%s", header)
	}
	if r, bin := buildIn(t, dir); r.code != exitOK {
		t.Fatalf("build: exit %d, stderr %q", r.code, r.stderr)
	} else if _, code := runBuilt(t, bin); code != 42 {
		t.Errorf("exited %d, want 42", code)
	}
}

// TestEmitHeaderWritesEveryPackageWithC checks --emit-header on a module
// writes one tack.h, for the whole program, into the main package's
// directory and that of each package with C files or extern funcs, and
// into no other.
func TestEmitHeaderWritesEveryPackageWithC(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"tack.mod": "module calc\n",
		"cmd/main.tck": "package main\n\nimport util from \"calc/util\"\nimport plain from \"calc/plain\"\n\n" +
			"func main() {\n    code, _ := int32(util.Sum(40, plain.Two()))\n    exit(code)\n}\n",
		"util/util.tck": "package util\n\nextern func add(a int64, b int64) int64\n\n" +
			"pub func Sum(a int64, b int64) int64 {\n    return add(a, b)\n}\n",
		"util/util.c":     addC,
		"plain/plain.tck": "package plain\n\npub func Two() int64 {\n    return 2\n}\n",
	})
	r := runTack(t, "--emit-header", filepath.Join(dir, "cmd"))
	if r.code != exitOK || r.stdout != "" || r.stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want a silent success", r.code, r.stdout, r.stderr)
	}
	for _, d := range []string{"cmd", "util"} {
		if _, err := os.Stat(filepath.Join(dir, d, "tack.h")); err != nil {
			t.Errorf("no tack.h in %s: %v", d, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "plain", "tack.h")); err == nil {
		t.Error("tack.h written into plain, which has no C")
	}
}

// TestEmitHeaderOfABadProgram checks --emit-header reports the program's
// diagnostics and writes nothing.
func TestEmitHeaderOfABadProgram(t *testing.T) {
	dir := writeDir(t, map[string]string{"main.tck": "package main\n\nextern func main()\n"})
	assertDiagnostics(t, runTack(t, "--emit-header", filepath.Join(dir, "main.tck")))
	if _, err := os.Stat(filepath.Join(dir, "tack.h")); err == nil {
		t.Error("tack.h written for a program with errors")
	}
}

// TestEmitHeaderUsageErrors checks --emit-header, which writes files
// instead of printing a dump, still cannot be combined with --out or a
// dump flag.
func TestEmitHeaderUsageErrors(t *testing.T) {
	path := writeSource(t, []byte(addProgram))
	assertUsageError(t, runTack(t, "--emit-header", "--out", "x", path), "--out cannot be combined")
	assertUsageError(t, runTack(t, "--emit-header", "--emit", path), "cannot be combined")
}

// TestEmitHeaderUnwritable checks a tack.h that cannot be written is a
// failure that names it.
func TestEmitHeaderUnwritable(t *testing.T) {
	dir := writeDir(t, map[string]string{"main.tck": addProgram})
	if err := os.Mkdir(filepath.Join(dir, "tack.h"), 0o755); err != nil {
		t.Fatal(err)
	}
	assertFailure(t, runTack(t, "--emit-header", filepath.Join(dir, "main.tck")), "cannot write")
}

// TestStaleHeaderBesideCFileIsIgnored checks a build compiles C files
// against the tack.h it generates, not one left beside them by an earlier
// --emit-header: here the stale one declares add with other types, and a C
// file that includes it by name still builds against the fresh one.
func TestStaleHeaderBesideCFileIsIgnored(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"main.tck": addProgram,
		"tack.h": "#ifndef TACK_H\n#define TACK_H\n#include <stdint.h>\n" +
			"int32_t add(int32_t);\n#endif\n",
		"helpers.c": "#include \"tack.h\"\n\n" + addC,
	})
	r, bin := buildIn(t, dir)
	if r.code != exitOK {
		t.Fatalf("build: exit %d, stderr %q", r.code, r.stderr)
	}
	if _, code := runBuilt(t, bin); code != 42 {
		t.Errorf("exited %d, want 42", code)
	}
}

// TestCFileWithoutTheHeaderIsStillChecked checks a C file that does not
// include tack.h is still compiled against it, so a definition that does
// not match its extern func is caught.
func TestCFileWithoutTheHeaderIsStillChecked(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"main.tck":  addProgram,
		"helpers.c": "#include <stdint.h>\n\nint32_t add(int32_t a) {\n    return a;\n}\n",
	})
	r, _ := buildIn(t, dir)
	if r.code != exitFailure || !strings.Contains(r.stderr, "conflicting types for 'add'") ||
		!strings.HasSuffix(
			r.stderr,
			"tack: cannot compile helpers.c: clang failed: exit status 1\n",
		) {
		t.Errorf(
			"exit %d, stderr %q; want clang's conflicting types and tack's failure",
			r.code,
			r.stderr,
		)
	}
}

// TestNonUTF8CSourceBuilds checks a C file holding bytes that are not
// UTF-8, in a comment and a string, compiles and links: tack never reads
// C source, it hands it to clang.
func TestNonUTF8CSourceBuilds(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"main.tck": addProgram,
		"helpers.c": "// \xff\xfe not UTF-8\nstatic const char *junk = \"\x80\xc3\";\n" +
			"int64_t add(int64_t a, int64_t b) {\n    return a + b + (junk[0] != 0) - 1;\n}\n",
	})
	r, bin := buildIn(t, dir)
	if r.code != exitOK {
		t.Fatalf("build: exit %d, stderr %q", r.code, r.stderr)
	}
	if _, code := runBuilt(t, bin); code != 42 {
		t.Errorf("exited %d, want 42", code)
	}
}

// TestHelperCFilesAndUnusedDefinitions checks C files may define what no
// extern func names, helpers static or not, and that a file with no
// definitions at all, or only comments, is fine beside one that has them.
func TestHelperCFilesAndUnusedDefinitions(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"main.tck": addProgram,
		"a.c": "static int64_t twice(int64_t x) { return 2 * x; }\n" +
			"int64_t half(int64_t x) { return x / 2; }\n" +
			"int64_t add(int64_t a, int64_t b) { return half(twice(a)) + b; }\n",
		"b.c":     "",
		"c.c":     "// nothing here\n",
		".skip.c": "this is not C and is never compiled\n",
	})
	r, bin := buildIn(t, dir)
	if r.code != exitOK {
		t.Fatalf("build: exit %d, stderr %q", r.code, r.stderr)
	}
	if _, code := runBuilt(t, bin); code != 42 {
		t.Errorf("exited %d, want 42", code)
	}
}

// TestCFileNamedLikeAnOptionIsRejected checks the driver refuses to build
// a program beside a C file whose name starts with -, which clang could
// read as an option, and names the file.
func TestCFileNamedLikeAnOptionIsRejected(t *testing.T) {
	dir := writeDir(t, map[string]string{"main.tck": addProgram, "-fplugin=x.c": addC})
	tmp := isolateTemp(t)
	r := runTack(t, "--out", filepath.Join(t.TempDir(), "bin"), filepath.Join(dir, "main.tck"))
	assertFailure(t, r, "file -fplugin=x.c")
	assertEmptyDir(t, tmp)
}

// TestOutNamedLikeAnOption checks an --out that starts with - is written
// as a file, not passed to clang as an option.
func TestOutNamedLikeAnOption(t *testing.T) {
	dir := writeDir(t, map[string]string{"main.tck": addProgram, "helpers.c": addC})
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	t.Chdir(dir)
	if r := runTack(t, "--out", "-Wl,-x", "main.tck"); r.code != exitOK {
		t.Fatalf("build: exit %d, stderr %q", r.code, r.stderr)
	}
	if _, code := runBuilt(t, filepath.Join(dir, "-Wl,-x")); code != 42 {
		t.Errorf("exited %d, want 42", code)
	}
}

// TestBuiltExternValuesExits42 builds examples/extern-values, whose
// extern funcs pass strings, structs, arrays and an empty struct and array
// to C and back, and return several results, and checks every value that
// crossed came back as Tack expects.
func TestBuiltExternValuesExits42(t *testing.T) {
	bin := buildWithClang(t, "extern-values")
	if out, code := runBuilt(t, bin); out != "hello from C\n" || code != 42 {
		t.Errorf("printed %q and exited %d, want %q and 42", out, code, "hello from C\n")
	}
}

// TestBuiltExternMutPassesEveryCheck builds examples/extern-mut, whose
// extern funcs take mut parameters, as pointers in C, and checks what each
// call did to the caller's variable: an integer, a struct, a field, an
// element, a whole array, a bool alongside a result, two mut parameters
// under a C name of their own, and a Tack mut parameter passed on to C.
func TestBuiltExternMutPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "extern-mut")
	want := "integer ok\n" +
		"struct ok\n" +
		"field ok\n" +
		"element ok\n" +
		"array ok\n" +
		"bool and a result ok\n" +
		"two mut parameters, C name ok\n" +
		"passed on to C ok\n"
	if out, code := runBuilt(t, bin); out != want || code != 0 {
		t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 0", out, code, want)
	}
}

// TestBuiltExternSizesPassesEveryCheck builds examples/extern-sizes,
// whose extern funcs take and return unsafe_size_t and unsafe_uintptr_t
// as C's size_t and uintptr_t, and checks each value that crossed: a
// sizeof, an address that comes back to C unchanged, a struct, an array
// and two results of them, a mut parameter, and SIZE_MAX. Some checks
// hold only on a 64-bit host.
func TestBuiltExternSizesPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "extern-sizes")
	want := "size_t result ok\n" +
		"uintptr_t round trip ok\n" +
		"struct of both types ok\n" +
		"array and two results ok\n" +
		"mut parameter ok\n" +
		"SIZE_MAX ok\n"
	if out, code := runBuilt(t, bin); out != want || code != 0 {
		t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 0", out, code, want)
	}
}

// TestBuiltExternStringsPassesEveryCheck builds examples/extern-strings,
// whose C half makes the strings it returns with tack.h's string helpers,
// and checks each string Tack got: a literal holding a zero byte, the
// empty literal, a prefix of an array, cut at its length, bytes from a
// buffer, an array's length, and a string built at file scope.
func TestBuiltExternStringsPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "extern-strings")
	want := "literal ok\n" +
		"empty ok\n" +
		"prefix ok\n" +
		"prefix past the end ok\n" +
		"buffer ok\n" +
		"size ok\n" +
		"global ok\n"
	if out, code := runBuilt(t, bin); out != want || code != 0 {
		t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 0", out, code, want)
	}
}

// TestBuiltExternViewsPassesEveryCheck builds examples/extern-views,
// whose extern funcs take views, as tack_view_ structs in C, and checks
// what C read through each: integers, an empty array, a string's bytes,
// structs, arrays under a C name of their own, strings, and a Tack view
// passed on to C.
func TestBuiltExternViewsPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "extern-views")
	want := "integers ok\n" +
		"empty array ok\n" +
		"string bytes ok\n" +
		"structs ok\n" +
		"arrays, C name ok\n" +
		"strings ok\n" +
		"passed on to C ok\n"
	if out, code := runBuilt(t, bin); out != want || code != 0 {
		t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 0", out, code, want)
	}
}

// TestBuiltExternVariadicsPassesEveryCheck builds
// examples/extern-variadics, whose extern funcs take variadic parameters,
// as tack_view_ structs in C, and checks what C read through each:
// widened integers, no values, a spread of an array and of a slice,
// strings after a fixed parameter under a C name of their own, a spread
// of strings, and a Tack variadic passed on to C.
func TestBuiltExternVariadicsPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "extern-variadics")
	want := "integers ok\n" +
		"no values ok\n" +
		"spread of an array and a slice ok\n" +
		"strings, C name ok\n" +
		"spread of strings ok\n" +
		"passed on to C ok\n"
	if out, code := runBuilt(t, bin); out != want || code != 0 {
		t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 0", out, code, want)
	}
}

// TestBuiltAtEveryOptLevel builds examples/extern-strings, generated
// source and a C file both, at every optimization level, and checks each
// binary passes every check: a level must not change what a program does.
func TestBuiltAtEveryOptLevel(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "prog")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("extern-strings"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); code != 0 || strings.Count(out, " ok\n") != 7 {
				t.Errorf("printed\n%s\nand exited %d", out, code)
			}
		})
	}
}

// TestBuiltExternModuleExits42 builds examples/extern-module, whose main
// package and geo package each have C files defining their own extern
// funcs, over types tack.h names Point for main's and geo_Point and
// geo_Rect for geo's.
func TestBuiltExternModuleExits42(t *testing.T) {
	bin := buildTargetWithClang(t, "prog", filepath.Join(examplesDir, "extern-module"))
	if out, code := runBuilt(t, bin); out != "module ok\n" || code != 42 {
		t.Errorf("printed %q and exited %d, want %q and 42", out, code, "module ok\n")
	}
}

// TestBuiltExternGuide builds examples/extern-guide, the worked example of
// docs/extern.md, and checks it ran every check: a string, a struct and
// several results crossed to C and back, and the Tack wrapper kept a zero
// divisor from reaching C.
func TestBuiltExternGuide(t *testing.T) {
	bin := buildWithClang(t, "extern-guide")
	if out, code := runBuilt(t, bin); out != "ok\n" || code != 0 {
		t.Errorf("printed %q and exited %d, want %q and 0", out, code, "ok\n")
	}
}

// externDoc is the page that documents extern C, whose worked example is
// examples/extern-guide.
var externDoc = filepath.Join("..", "docs", "extern.md")

// TestExternDocShowsTheGuide checks docs/extern.md quotes the worked
// example as it is: main.tck and guide.c whole, and the tack.h
// --emit-header writes for it, so the page cannot drift from what the
// compiler does.
func TestExternDocShowsTheGuide(t *testing.T) {
	doc, err := os.ReadFile(externDoc)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(examplesDir, "extern-guide")
	files := map[string]string{}
	for _, name := range []string{"main.tck", "guide.c"} {
		data, readErr := os.ReadFile(filepath.Join(src, name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		files[name] = string(data)
	}
	dir := writeDir(t, files)
	if r := runTack(t, "--emit-header", filepath.Join(dir, "main.tck")); r.code != exitOK {
		t.Fatalf("--emit-header: exit %d, stderr %q", r.code, r.stderr)
	}
	header, err := os.ReadFile(filepath.Join(dir, "tack.h"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"```tack\n" + files["main.tck"] + "```\n",
		"```c\n" + files["guide.c"] + "```\n",
		"```c\n" + string(header) + "```\n",
	} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("%s does not quote, whole and fenced:\n%s", externDoc, want)
		}
	}
}
