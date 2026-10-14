package c_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen/c"
)

// buildExtern compiles src, a program with extern funcs, and csrc, the C
// file that defines them, with clang's warnings on and treated as errors,
// the C file against the program's tack.h, and links them. It returns the
// binary's path.
func buildExtern(t *testing.T, src, csrc string) string {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	prog := ast.SingleFile(compile(t, src))
	out, diags := c.New().Generate(prog)
	if len(diags) > 0 {
		t.Fatalf("generate reported %+v", diags)
	}
	dir := t.TempDir()
	files := map[string][]byte{
		"program.c":  out,
		c.HeaderName: c.New().Header(prog),
		"helpers.c":  []byte(csrc),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	strict := []string{"-std=c11", "-Wall", "-Wextra", "-Werror"}
	header := filepath.Join(dir, c.HeaderName)
	obj := filepath.Join(dir, "helpers.o")
	bin := filepath.Join(dir, "prog")
	for _, args := range [][]string{
		append(strict, "-c", "-include", header, "-o", obj, filepath.Join(dir, "helpers.c")),
		append(strict, "-o", bin, filepath.Join(dir, "program.c"), obj),
	} {
		if msg, err := exec.Command("clang", args...).CombinedOutput(); err != nil {
			t.Fatalf(
				"clang %s: %v\n%s\n%s\n%s",
				strings.Join(args, " "),
				err,
				msg,
				out,
				files[c.HeaderName],
			)
		}
	}
	return bin
}

// exitStatus runs bin and returns its exit status.
func exitStatus(t *testing.T, bin string) int {
	t.Helper()
	err := exec.Command(bin).Run()
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("running %s: %v", bin, err)
	}
	return exit.ExitCode()
}

// TestExternIntegersAndBoolsCrossToC calls C functions taking and
// returning every integer width and bool, the extremes among them, and
// checks each value comes back as Tack sent it.
func TestExternIntegersAndBoolsCrossToC(t *testing.T) {
	src := "package main\n\n" +
		"extern func id8(x int8) int8\n" +
		"extern func id16(x int16) int16\n" +
		"extern func id32(x int32) int32\n" +
		"extern func id64(x int64) int64\n" +
		"extern func idu8(x uint8) uint8\n" +
		"extern func idu16(x uint16) uint16\n" +
		"extern func idu32(x uint32) uint32\n" +
		"extern func idu64(x uint64) uint64\n" +
		"extern func not(b bool) bool\n" +
		"extern func nothing()\n\n" +
		"func main() {\n" +
		"    nothing()\n" +
		"    if id8(-128) != -128 || id16(-32768) != -32768 || id32(-2147483648) != -2147483648 {\n" +
		"        exit(1)\n    }\n" +
		"    if id64(-9223372036854775807 - 1) != -9223372036854775807 - 1 {\n        exit(2)\n    }\n" +
		"    if idu8(255) != 255 || idu16(65535) != 65535 || idu32(4294967295) != 4294967295 {\n" +
		"        exit(3)\n    }\n" +
		"    if idu64(18446744073709551615) != 18446744073709551615 {\n        exit(4)\n    }\n" +
		"    if not(true) || !not(false) {\n        exit(5)\n    }\n" +
		"    exit(42)\n}\n"
	csrc := "int8_t id8(int8_t x) { return x; }\n" +
		"int16_t id16(int16_t x) { return x; }\n" +
		"int32_t id32(int32_t x) { return x; }\n" +
		"int64_t id64(int64_t x) { return x; }\n" +
		"uint8_t idu8(uint8_t x) { return x; }\n" +
		"uint16_t idu16(uint16_t x) { return x; }\n" +
		"uint32_t idu32(uint32_t x) { return x; }\n" +
		"uint64_t idu64(uint64_t x) { return x; }\n" +
		"bool not(bool b) { return !b; }\n" +
		"void nothing(void) {}\n"
	if got := exitStatus(t, buildExtern(t, src, csrc)); got != 42 {
		t.Errorf("exit status = %d, want 42", got)
	}
}

// TestExternNamedLikeGeneratedCode gives extern funcs the C names of
// things the generated source declares for itself, a pooled string, and
// of C library functions it calls, fwrite and exit's helpers: the
// generated source calls each extern func by its mangled name, bound to
// its C name only at the symbol, so neither side sees the other's.
func TestExternNamedLikeGeneratedCode(t *testing.T) {
	src := "package main\n\n" +
		"extern func str0() int64\n" +
		"extern func print2(x int64) int64\n" +
		"extern func v_x() int64\n\n" +
		"func main() {\n    print(\"hi\")\n    exit(str0() + print2(1) + v_x())\n}\n"
	csrc := "int64_t str0(void) { return 40; }\n" +
		"int64_t print2(int64_t x) { return x; }\n" +
		"int64_t v_x(void) { return 1; }\n"
	if got := exitStatus(t, buildExtern(t, src, csrc)); got != 42 {
		t.Errorf("exit status = %d, want 42", got)
	}
}

// TestHeaderDeclaresEveryExternFunc pins tack.h for a program of integer
// and bool extern funcs: an include guard, the headers its types need, and
// a prototype for each extern func under its C name, without parameter
// names, in declaration order, under a comment naming the package, each
// with its Tack declaration in a comment.
func TestHeaderDeclaresEveryExternFunc(t *testing.T) {
	src := "package main\n\n" +
		"extern func add(a int64, b int64) int64\n" +
		"func helper() {}\n" +
		"extern func flip(b bool) bool\n" +
		"extern func tick()\n\n" +
		"func main() {\n    helper()\n}\n"
	got := string(c.New().Header(ast.SingleFile(compile(t, src))))
	want := "// Generated by the Tack compiler. Do not edit.\n//\n" +
		"// It declares every extern func of the program, for the C files that\n" +
		"// define them, and the types their signatures use.\n\n" +
		"#ifndef TACK_H\n#define TACK_H\n\n" +
		"#include <stdbool.h>\n#include <stdint.h>\n\n" +
		"// package main\n" +
		"int64_t add(int64_t, int64_t); // extern func add(a int64, b int64) int64\n" +
		"bool flip(bool); // extern func flip(b bool) bool\n" +
		"void tick(void); // extern func tick()\n\n" +
		"#endif\n"
	if got != want {
		t.Errorf("Header =\n%s\nwant\n%s", got, want)
	}
}

// TestCompileCmdTakesAnOptionLikeNameAsAFile checks that a C file named
// like a clang option, given absolute as the driver gives every path, is
// compiled as a file: -DBAD would otherwise define a macro and compile
// nothing, and the object would not exist.
func TestCompileCmdTakesAnOptionLikeNameAsAFile(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "-DBAD.c")
	header := filepath.Join(dir, c.HeaderName)
	obj := filepath.Join(dir, "-o.o")
	for name, data := range map[string]string{src: "int defined_here;\n", header: "\n"} {
		if err := os.WriteFile(name, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	argv := c.New().CompileCmd(src, header, obj)
	if msg, err := exec.Command(argv[0], argv[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("%q: %v\n%s", argv, err, msg)
	}
	if _, err := os.Stat(obj); err != nil {
		t.Errorf("no object at %s: %v", obj, err)
	}
}

// TestExternValuesExampleIsWarningFree builds examples/extern-values with
// clang's warnings as errors, its C file against its tack.h, so neither
// the generated source nor the header causes a warning, and checks it
// exits 42 as the driver's test does.
func TestExternValuesExampleIsWarningFree(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "extern-values")
	src, err := os.ReadFile(filepath.Join(dir, "main.tck"))
	if err != nil {
		t.Fatal(err)
	}
	csrc, err := os.ReadFile(filepath.Join(dir, "values.c"))
	if err != nil {
		t.Fatal(err)
	}
	if got := exitStatus(t, buildExtern(t, string(src), string(csrc))); got != 42 {
		t.Errorf("exit status = %d, want 42", got)
	}
}

// TestHeaderUsesTheGivenCName checks tack.h declares an extern func given
// extern("c_name") under that name, and its results struct after it.
func TestHeaderUsesTheGivenCName(t *testing.T) {
	src := "package main\n\nextern(\"c_split\") func split(x int64) (int64, int64)\n\n" +
		"func main() {\n}\n"
	got := string(c.New().Header(ast.SingleFile(compile(t, src))))
	for _, want := range []string{
		"} tack_results_c_split;\n",
		"tack_results_c_split c_split(int64_t); // extern func split(x int64) (int64, int64)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("tack.h is missing %q:\n%s", want, got)
		}
	}
}
