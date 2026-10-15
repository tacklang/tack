package c_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen"
	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/internal/stdtest"
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
	out, diags := c.New().Generate(prog, stdtest.MustBuiltins())
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
	strict := []string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-I", stdtest.IncludeDir()}
	header := filepath.Join(dir, c.HeaderName)
	obj := filepath.Join(dir, "helpers.o")
	bin := filepath.Join(dir, "prog")
	for _, args := range [][]string{
		append(strict, "-c", "-include", header, "-o", obj, filepath.Join(dir, "helpers.c")),
		append(append(strict, "-o", bin, filepath.Join(dir, "program.c"), obj), stdObjects(t)...),
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
		"func main() {\n    print(\"hi\")\n    code, _ := int32(str0() + print2(1) + v_x())\n    exit(code)\n}\n"
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
		"int64_t add(int64_t /* a */, int64_t /* b */); // extern func add(a int64, b int64) int64\n" +
		"bool flip(bool /* b */); // extern func flip(b bool) bool\n" +
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
	argv := c.New().CompileCmd(src, header, stdtest.IncludeDir(), obj, codegen.DefaultOpt)
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
		"tack_results_c_split c_split(int64_t /* x */); // extern func split(x int64) (int64, int64)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("tack.h is missing %q:\n%s", want, got)
		}
	}
}

// TestHeaderNamesParamsInComments checks tack.h names each parameter in
// a comment after its type, so a name that is a C keyword or one of
// tack.h's type names, legal in Tack, cannot break the prototype.
func TestHeaderNamesParamsInComments(t *testing.T) {
	src := "package main\n\nextern func f(int int64, char bool, tack_string string)\n\n" +
		"func main() {\n}\n"
	got := string(c.New().Header(ast.SingleFile(compile(t, src))))
	want := "void f(int64_t /* int */, bool /* char */, tack_string /* tack_string */); " +
		"// extern func f(int int64, char bool, tack_string string)\n"
	if !strings.Contains(got, want) {
		t.Errorf("tack.h is missing %q:\n%s", want, got)
	}
}

// TestHeaderDeclaresMutParamsAsPointers checks tack.h declares a mut
// parameter as a pointer to its type, an integer's, a struct's and an
// array's, with mut in the Tack declaration beside it.
func TestHeaderDeclaresMutParamsAsPointers(t *testing.T) {
	src := "package main\n\ntype Point struct {\n    x int64\n}\n\n" +
		"extern func move(mut p Point, mut n int64, k int64, mut a [2]int8) bool\n\n" +
		"func main() {\n}\n"
	got := string(c.New().Header(ast.SingleFile(compile(t, src))))
	want := "bool move(Point * /* p */, int64_t * /* n */, int64_t /* k */, tack_array_2_int8 * /* a */); " +
		"// extern func move(mut p Point, mut n int64, k int64, mut a [2]int8) bool\n"
	if !strings.Contains(got, want) {
		t.Errorf("tack.h is missing %q:\n%s", want, got)
	}
}

// TestExternMutExampleIsWarningFree builds examples/extern-mut with
// clang's warnings as errors, its C file against its tack.h, so the
// pointers a mut parameter becomes agree between the generated source,
// the header and the C file, and checks every check it prints passed.
func TestExternMutExampleIsWarningFree(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "extern-mut")
	src, err := os.ReadFile(filepath.Join(dir, "main.tck"))
	if err != nil {
		t.Fatal(err)
	}
	csrc, err := os.ReadFile(filepath.Join(dir, "mut.c"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(buildExtern(t, string(src), string(csrc))).Output()
	if err != nil {
		t.Fatalf("extern-mut failed: %v", err)
	}
	if strings.Contains(string(out), "FAIL") || strings.Count(string(out), " ok\n") != 8 {
		t.Errorf("extern-mut printed\n%s", out)
	}
}

// TestExternViewsExampleIsWarningFree builds examples/extern-views with
// clang's warnings as errors, its C file against its tack.h, so the
// tack_view_ structs a view parameter becomes agree between the generated
// source, the header and the C file, and checks every check it prints
// passed.
func TestExternViewsExampleIsWarningFree(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "extern-views")
	src, err := os.ReadFile(filepath.Join(dir, "main.tck"))
	if err != nil {
		t.Fatal(err)
	}
	csrc, err := os.ReadFile(filepath.Join(dir, "views.c"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(buildExtern(t, string(src), string(csrc))).Output()
	if err != nil {
		t.Fatalf("extern-views failed: %v", err)
	}
	if strings.Contains(string(out), "FAIL") || strings.Count(string(out), " ok\n") != 7 {
		t.Errorf("extern-views printed\n%s", out)
	}
}

// TestExternSizesExampleIsWarningFree builds examples/extern-sizes with
// clang's warnings as errors, its C file against its tack.h, so size_t and
// uintptr_t agree between the generated source, the header and the C
// file, and checks every check it prints passed.
func TestExternSizesExampleIsWarningFree(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "extern-sizes")
	src, err := os.ReadFile(filepath.Join(dir, "main.tck"))
	if err != nil {
		t.Fatal(err)
	}
	csrc, err := os.ReadFile(filepath.Join(dir, "sizes.c"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(buildExtern(t, string(src), string(csrc))).Output()
	if err != nil {
		t.Fatalf("extern-sizes failed: %v", err)
	}
	if strings.Contains(string(out), "FAIL") || strings.Count(string(out), " ok\n") != 6 {
		t.Errorf("extern-sizes printed\n%s", out)
	}
}

// TestHeaderRejectsFieldsNamedLikeObjectMacros checks that a field of a
// struct tack.h defines cannot be named like an object-like macro tack.h
// defines or includes: the macro would replace the member's name, leaving
// a member with no name, which changes the struct's layout, or one C
// cannot parse. The program is rejected when it is generated, before the
// driver writes tack.h.
func TestHeaderRejectsFieldsNamedLikeObjectMacros(t *testing.T) {
	for _, tt := range []struct{ field, want string }{
		{"TACK_H", "tack.h's include guard"},
		{"TACK_STD_TACK_H", "the include guard of std's tack.h"},
		{"NULL", "a macro of <stddef.h>"},
		{"INT64_MAX", "a macro of <stdint.h>"},
		{"UINT_FAST8_WIDTH", "a macro of <stdint.h>"},
		{"__bool_true_false_are_defined", "a macro of <stdbool.h>"},
	} {
		t.Run(tt.field, func(t *testing.T) {
			src := "package main\n\ntype Shape struct {\n    a int64\n    " + tt.field +
				" int64\n}\n\nextern func area(s Shape) int64\n\nfunc main() {\n}\n"
			out, diags := c.New().Generate(ast.SingleFile(compile(t, src)), stdtest.MustBuiltins())
			want := "field " + tt.field + " of type Shape cannot be named " + tt.field +
				" in tack.h: that is " + tt.want
			if out != nil || len(diags) != 1 || diags[0].Message != want {
				t.Errorf("Generate = %d bytes, %+v; want no output and one error %q",
					len(out), diags, want)
			}
		})
	}
}

// TestExternFieldsNamedLikeFunctionMacros passes a struct whose fields are
// named like function-like macros tack.h and the headers it includes
// define, and like tack.h's string type, to C and reads each back: a
// function-like macro expands only where its name is followed by (, which
// neither a member's declaration nor p.name is, and a member's name is in
// a namespace of its own, apart from typedef names.
func TestExternFieldsNamedLikeFunctionMacros(t *testing.T) {
	src := "package main\n\n" +
		"type F struct {\n    TACK_STR int64\n    offsetof int64\n    INT64_C int64\n" +
		"    tack_string int64\n}\n\n" +
		"extern func sum(f F, s string) int64\n\n" +
		"func main() {\n    code, _ := int32(sum(F{TACK_STR: 1, offsetof: 2, INT64_C: 4, tack_string: 8}, \"abc\"))\n    exit(code)\n}\n"
	csrc := "#include <stddef.h>\n\n" +
		"int64_t sum(F f, tack_string s) {\n" +
		"    return f.TACK_STR + f.offsetof + f.INT64_C + f.tack_string + s.len * 10;\n}\n"
	if got := exitStatus(t, buildExtern(t, src, csrc)); got != 45 {
		t.Errorf("exit status = %d, want 45", got)
	}
}
