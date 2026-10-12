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
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/typeck"
)

// compile runs the front end and fails the test if any stage rejects src, so
// the tests below only ever hand the backend a file the resolver accepted.
func compile(t *testing.T, src string) *ast.File {
	t.Helper()

	f := &source.File{ID: source.FileID("test"), Filename: "case.tck", Data: []byte(src)}

	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatalf("lex: %s", diag.RenderAllPretty(f, diags))
	}

	file, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
	if len(diags) > 0 {
		t.Fatalf("parse: %s", diag.RenderAllPretty(f, diags))
	}

	if diags := resolver.New(file).Resolve(); len(diags) > 0 {
		t.Fatalf("resolve: %s", diag.RenderAllPretty(f, diags))
	}

	// The backend reads the kinds the checker records, so a tree that has not
	// been checked is not a tree it is meant to be handed.
	if diags := typeck.New(file).Check(); len(diags) > 0 {
		t.Fatalf("typeck: %s", diag.RenderAllPretty(f, diags))
	}

	return file
}

// generate runs the C backend and fails the test if it reports a diagnostic.
func generate(t *testing.T, src string) string {
	t.Helper()

	out, diags := c.New().Generate(compile(t, src))
	if len(diags) > 0 {
		t.Fatalf("generate reported %d diagnostic(s): %+v", len(diags), diags)
	}
	return string(out)
}

func TestBackendIdentity(t *testing.T) {
	e := c.New()
	if e.Name() != "c" {
		t.Errorf("Name() = %q, want %q", e.Name(), "c")
	}
	if e.Ext() != ".c" {
		t.Errorf("Ext() = %q, want %q", e.Ext(), ".c")
	}
}

func TestBuildCmdCompilesSourceToOutput(t *testing.T) {
	got := c.New().BuildCmd("in.c", "prog")
	want := []string{"clang", "-o", "prog", "in.c"}

	if len(got) != len(want) {
		t.Fatalf("BuildCmd = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("BuildCmd = %q, want %q", got, want)
		}
	}
}

func TestGeneratesEntryPointCallingTackMain(t *testing.T) {
	got := generate(t, "package main\n\nfunc main() {\n}\n")

	for _, want := range []string{
		"#include <stdio.h>",
		"static void tack_main_main(void);",
		"static void tack_main_main(void) {",
		"int main(void) {\n    tack_main_main();\n    return 0;\n}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated C is missing %q:\n%s", want, got)
		}
	}
}

func TestGeneratesCallBetweenTackFunctions(t *testing.T) {
	got := generate(t, "package main\n\nfunc greet() {\n}\n\nfunc main() {\n    greet()\n}\n")

	if !strings.Contains(got, "static void tack_main_greet(void) {") {
		t.Errorf("generated C does not define greet:\n%s", got)
	}
	if !strings.Contains(got, "    tack_main_greet();") {
		t.Errorf("generated C does not call greet from main:\n%s", got)
	}
}

// Forward declarations are what let a Tack function call one declared after it,
// which the language allows and C does not without a prototype.
func TestForwardDeclaresEveryFunctionBeforeAnyDefinition(t *testing.T) {
	got := generate(t, "package main\n\nfunc main() {\n    later()\n}\n\nfunc later() {\n}\n")

	proto := strings.Index(got, "static void tack_main_later(void);")
	def := strings.Index(got, "static void tack_main_main(void) {")
	if proto == -1 {
		t.Fatalf("generated C has no prototype for later:\n%s", got)
	}
	if def == -1 {
		t.Fatalf("generated C has no definition for main:\n%s", got)
	}
	if proto > def {
		t.Errorf(
			"prototype for later appears after main's definition, so the call will not compile:\n%s",
			got,
		)
	}
}

// Strings are emitted as byte arrays rather than C string literals so that
// arbitrary bytes survive without an escaping scheme.
func TestEmitsStringLiteralsAsByteArrays(t *testing.T) {
	got := generate(t, "package main\n\nfunc main() {\n    print(\"hi\")\n}\n")

	if !strings.Contains(got, "104, 105") {
		t.Errorf("generated C does not contain the bytes of \"hi\":\n%s", got)
	}
	if strings.Contains(got, `"hi"`) {
		t.Errorf("generated C emitted a C string literal instead of a byte array:\n%s", got)
	}
}

func TestPrintWritesTheStringThenANewline(t *testing.T) {
	got := generate(t, "package main\n\nfunc main() {\n    print(\"hi\")\n}\n")

	want := "tack_print((tack_string){str1, 2});\n    tack_print((tack_string){str0, 1});"
	if !strings.Contains(got, want) {
		t.Errorf("print did not lower to the string followed by a newline:\n%s", got)
	}
}

// The newline print appends is pooled only when something prints, so a
// program that never prints carries no unused constant.
func TestPoolsNoNewlineWhenNothingPrints(t *testing.T) {
	got := generate(t, "package main\n\nfunc main() {\n    exit(3)\n}\n")

	if strings.Contains(got, "unsigned char str") {
		t.Errorf("a program that never prints emitted a pooled string:\n%s", got)
	}
}

// A program that prints still pools the newline first, so it keeps the label
// str0 and every literal's label is unchanged.
func TestPoolsTheNewlineFirstWhenSomethingPrints(t *testing.T) {
	got := generate(t, "package main\n\nfunc main() {\n    print(\"hi\")\n}\n")

	if !strings.Contains(got, "static const unsigned char str0[2] = {10, 0};") {
		t.Errorf("the newline is not pooled as str0:\n%s", got)
	}
}

func TestRejectsUnsupportedStatement(t *testing.T) {
	// No Tack source can express a bare identifier statement today, so the
	// tree is built by hand. It pins what the backend does when the front end
	// grows a node the backend has not learned to lower: report, not emit.
	file := &ast.File{
		Package: "main",
		Decls: []ast.Decl{
			&ast.Func{Name: "main", Body: []ast.Stmt{
				&ast.ExprStmt{X: &ast.Ident{Name: "x"}},
			}},
		},
	}

	out, diags := c.New().Generate(file)
	if len(diags) == 0 {
		t.Fatalf("generate accepted an unsupported statement and emitted:\n%s", out)
	}
	if diags[0].Severity != diag.Error {
		t.Errorf("severity = %v, want %v", diags[0].Severity, diag.Error)
	}
}

func TestGeneratesExitAsALibcCall(t *testing.T) {
	got := generate(t, "package main\n\nfunc main() {\n    exit(42)\n}\n")

	if !strings.Contains(got, "#include <stdlib.h>") {
		t.Errorf("generated C does not include stdlib.h, so exit is undeclared:\n%s", got)
	}
	if !strings.Contains(got, "    exit(42);") {
		t.Errorf("generated C does not call exit:\n%s", got)
	}
}

func TestExitAcceptsTheFullIntRange(t *testing.T) {
	got := generate(t, "package main\n\nfunc main() {\n    exit(9223372036854775807)\n}\n")

	if !strings.Contains(got, "exit(9223372036854775807);") {
		t.Errorf("generated C lost precision on a large literal:\n%s", got)
	}
}

func TestGeneratesStringAsAPointerLengthStruct(t *testing.T) {
	got := generate(t, "package main\n\nfunc main() {\n    print(\"hi\")\n}\n")

	if !strings.Contains(got, "int64_t len;") || !strings.Contains(got, "tack_string;") {
		t.Errorf("generated C has no tack_string definition:\n%s", got)
	}
	if !strings.Contains(got, "#include <stdint.h>") {
		t.Errorf("generated C does not include stdint.h, so int64_t is undeclared:\n%s", got)
	}
}

func TestStringLiteralBecomesAStringValue(t *testing.T) {
	got := generate(t, "package main\n\nfunc main() {\n    print(\"hi\")\n}\n")

	if !strings.Contains(got, "tack_print((tack_string){str1, 2});") {
		t.Errorf("a string literal did not become a tack_string value:\n%s", got)
	}
}

func TestGeneratesParameterisedSignature(t *testing.T) {
	got := generate(t, "package main\n\nfunc f(x int, s string) {\n}\n\nfunc main() {\n}\n")

	want := "static void tack_main_f(int64_t p_x, tack_string p_s)"
	if !strings.Contains(got, want+";") {
		t.Errorf("generated C is missing the prototype %q:\n%s", want, got)
	}
	if !strings.Contains(got, want+" {") {
		t.Errorf("generated C is missing the definition %q:\n%s", want, got)
	}
}

// Parameter names are prefixed because Tack has no reserved words: a
// parameter can legally be called str0, exit, or int, each of which would
// collide with something the generated C already defines.
func TestParameterNamesCannotCollideWithGeneratedNames(t *testing.T) {
	got := generate(t, "package main\n\nfunc f(str0 int, exit int) {\n}\n\nfunc main() {\n}\n")

	if !strings.Contains(got, "static void tack_main_f(int64_t p_str0, int64_t p_exit)") {
		t.Errorf("parameter names were not kept clear of generated names:\n%s", got)
	}
}

func TestPassesArgumentsAtTheCallSite(t *testing.T) {
	got := generate(
		t,
		"package main\n\nfunc f(x int, s string) {\n}\n\nfunc main() {\n    f(7, \"hi\")\n}\n",
	)

	if !strings.Contains(got, "tack_main_f(7, (tack_string){str0, 2});") {
		t.Errorf("call site does not pass its arguments:\n%s", got)
	}
}

func TestLowersBuiltinsAppliedToParameters(t *testing.T) {
	got := generate(
		t,
		"package main\n\nfunc f(x int, s string) {\n    print(s)\n    exit(x)\n}\n\nfunc main() {\n}\n",
	)

	if !strings.Contains(got, "tack_print(p_s);") {
		t.Errorf("print of a parameter was not lowered:\n%s", got)
	}
	if !strings.Contains(got, "exit(p_x);") {
		t.Errorf("exit of a parameter was not lowered:\n%s", got)
	}
}

// TestGeneratedProgramRuns is the only test here that proves the emitted C is
// correct rather than merely stable: it compiles it with a real compiler and
// checks what the binary prints.
func TestGeneratedProgramRuns(t *testing.T) {
	e := c.New()
	if _, err := exec.LookPath(e.BuildCmd("", "")[0]); err != nil {
		t.Skipf("%v", err)
	}

	src := "package main\n\n" +
		"func greet() {\n    print(\"héllo → 世界\")\n}\n\n" +
		"func main() {\n    greet()\n    print(\"Hello, World!\")\n}\n"

	dir := t.TempDir()
	csrc := filepath.Join(dir, "out"+e.Ext())
	bin := filepath.Join(dir, "prog")

	if err := os.WriteFile(csrc, []byte(generate(t, src)), 0o644); err != nil {
		t.Fatalf("writing generated C: %v", err)
	}

	cmd := e.BuildCmd(csrc, bin)
	build := exec.Command(cmd[0], cmd[1:]...)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("generated C did not compile: %v\n%s", err, out)
	}

	got, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("running the generated program: %v", err)
	}

	want := "héllo → 世界\nHello, World!\n"
	if string(got) != want {
		t.Errorf("program printed %q, want %q", got, want)
	}
}

// TestGeneratedProgramExitsWithTheGivenStatus is the acceptance test for
// integers: it is the only way to observe one until print can format them.
func TestGeneratedProgramExitsWithTheGivenStatus(t *testing.T) {
	e := c.New()
	if _, err := exec.LookPath(e.BuildCmd("", "")[0]); err != nil {
		t.Skipf("%v", err)
	}

	dir := t.TempDir()
	csrc := filepath.Join(dir, "out"+e.Ext())
	bin := filepath.Join(dir, "prog")

	src := "package main\n\nfunc main() {\n    print(\"before\")\n    exit(42)\n}\n"
	if err := os.WriteFile(csrc, []byte(generate(t, src)), 0o644); err != nil {
		t.Fatalf("writing generated C: %v", err)
	}

	cmd := e.BuildCmd(csrc, bin)
	if out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("generated C did not compile: %v\n%s", err, out)
	}

	err := exec.Command(bin).Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("running the generated program: err = %v, want a non-zero exit", err)
	}
	if exit.ExitCode() != 42 {
		t.Errorf("exit status = %d, want 42", exit.ExitCode())
	}
}

// build compiles src through the C backend and returns the binary's path.
func build(t *testing.T, src string) string {
	t.Helper()

	e := c.New()
	if _, err := exec.LookPath(e.BuildCmd("", "")[0]); err != nil {
		t.Skipf("%v", err)
	}

	dir := t.TempDir()
	csrc := filepath.Join(dir, "out"+e.Ext())
	bin := filepath.Join(dir, "prog")

	if err := os.WriteFile(csrc, []byte(generate(t, src)), 0o644); err != nil {
		t.Fatalf("writing generated C: %v", err)
	}

	cmd := e.BuildCmd(csrc, bin)
	if out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("generated C did not compile: %v\n%s", err, out)
	}
	return bin
}

func TestGeneratedProgramPassesArgumentsAtRuntime(t *testing.T) {
	src := "package main\n\n" +
		"func greet(s string, n int) {\n    print(s)\n    exit(n)\n}\n\n" +
		"func main() {\n    greet(\"hello\", 7)\n}\n"

	out, err := exec.Command(build(t, src)).Output()

	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("running the generated program: err = %v, want a non-zero exit", err)
	}
	if exit.ExitCode() != 7 {
		t.Errorf("exit status = %d, want 7", exit.ExitCode())
	}
	if string(out) != "hello\n" {
		t.Errorf("program printed %q, want %q", out, "hello\n")
	}
}

// A parameter read after an intervening call is what fails when parameters
// live only in the registers they arrived in.
func TestParameterSurvivesAnInterveningCall(t *testing.T) {
	src := "package main\n\n" +
		"func other() {\n    print(\"side effect\")\n}\n\n" +
		"func f(n int) {\n    other()\n    exit(n)\n}\n\n" +
		"func main() {\n    f(9)\n}\n"

	out, err := exec.Command(build(t, src)).Output()

	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("running the generated program: err = %v, want a non-zero exit", err)
	}
	if exit.ExitCode() != 9 {
		t.Errorf(
			"exit status = %d, want 9 (the parameter did not survive the call)",
			exit.ExitCode(),
		)
	}
	if string(out) != "side effect\n" {
		t.Errorf("program printed %q, want %q", out, "side effect\n")
	}
}

func TestGeneratedProgramForwardsAParameter(t *testing.T) {
	src := "package main\n\n" +
		"func g(s string) {\n    print(s)\n}\n\n" +
		"func f(s string) {\n    g(s)\n}\n\n" +
		"func main() {\n    f(\"forwarded\")\n}\n"

	out, err := exec.Command(build(t, src)).Output()
	if err != nil {
		t.Fatalf("running the generated program: %v", err)
	}
	if string(out) != "forwarded\n" {
		t.Errorf("program printed %q, want %q", out, "forwarded\n")
	}
}

func TestGeneratesResultTypeInSignature(t *testing.T) {
	got := generate(
		t,
		"package main\n\nfunc seven() int {\n    return 7\n}\n\nfunc hi() string {\n    return \"hi\"\n}\n\nfunc main() {\n}\n",
	)

	for _, want := range []string{
		"static int64_t tack_main_seven(void)",
		"static tack_string tack_main_hi(void)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated C is missing %q:\n%s", want, got)
		}
	}
}

func TestGeneratesReturnStatement(t *testing.T) {
	got := generate(t, "package main\n\nfunc seven() int {\n    return 7\n}\n\nfunc main() {\n}\n")

	if !strings.Contains(got, "    return 7;") {
		t.Errorf("generated C has no return statement:\n%s", got)
	}
}

func TestGeneratesBareReturn(t *testing.T) {
	got := generate(t, "package main\n\nfunc f() {\n    return\n}\n\nfunc main() {\n}\n")

	if !strings.Contains(got, "    return;") {
		t.Errorf("generated C has no bare return:\n%s", got)
	}
}

func TestCallInArgumentPositionIsACExpression(t *testing.T) {
	got := generate(
		t,
		"package main\n\nfunc seven() int {\n    return 7\n}\n\nfunc main() {\n    exit(seven())\n}\n",
	)

	if !strings.Contains(got, "exit(tack_main_seven());") {
		t.Errorf("a call argument did not become a C expression:\n%s", got)
	}
}

func TestNestedCallsNest(t *testing.T) {
	src := "package main\n\nfunc g(x int) int {\n    return x\n}\n\n" +
		"func h() int {\n    return 3\n}\n\nfunc main() {\n    exit(g(h()))\n}\n"

	got := generate(t, src)
	if !strings.Contains(got, "exit(tack_main_g(tack_main_h()));") {
		t.Errorf("nested calls did not nest:\n%s", got)
	}
}

func TestGeneratedProgramReturnsAnIntResult(t *testing.T) {
	src := "package main\n\nfunc seven() int {\n    return 7\n}\n\nfunc main() {\n    exit(seven())\n}\n"

	err := exec.Command(build(t, src)).Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("running the generated program: err = %v, want a non-zero exit", err)
	}
	if exit.ExitCode() != 7 {
		t.Errorf("exit status = %d, want 7", exit.ExitCode())
	}
}

func TestGeneratedProgramReturnsAStringResult(t *testing.T) {
	src := "package main\n\nfunc greeting() string {\n    return \"hello from a result\"\n}\n\n" +
		"func main() {\n    print(greeting())\n}\n"

	out, err := exec.Command(build(t, src)).Output()
	if err != nil {
		t.Fatalf("running the generated program: %v", err)
	}
	if string(out) != "hello from a result\n" {
		t.Errorf("program printed %q, want %q", out, "hello from a result\n")
	}
}

func TestGeneratedProgramThreadsAValueThroughSeveralCalls(t *testing.T) {
	src := "package main\n\n" +
		"func base() int {\n    return 5\n}\n\n" +
		"func same(n int) int {\n    return n\n}\n\n" +
		"func main() {\n    exit(same(same(base())))\n}\n"

	err := exec.Command(build(t, src)).Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("running the generated program: err = %v, want a non-zero exit", err)
	}
	if exit.ExitCode() != 5 {
		t.Errorf("exit status = %d, want 5", exit.ExitCode())
	}
}

func TestGeneratesBinaryExpression(t *testing.T) {
	got := generate(t, "package main\n\nfunc main() {\n    exit(1 + 2 * 3)\n}\n")

	if !strings.Contains(got, "exit((1 + (2 * 3)));") {
		t.Errorf("arithmetic did not survive with its grouping:\n%s", got)
	}
}

func TestGeneratesBoolInSignature(t *testing.T) {
	got := generate(
		t,
		"package main\n\nfunc pick(b bool) bool {\n    return b\n}\n\nfunc main() {\n}\n",
	)

	if !strings.Contains(got, "#include <stdbool.h>") {
		t.Errorf("generated C does not include stdbool.h:\n%s", got)
	}
	if !strings.Contains(got, "static bool tack_main_pick(bool p_b)") {
		t.Errorf("bool did not reach the signature:\n%s", got)
	}
}

func TestGeneratesIfElse(t *testing.T) {
	src := "package main\n\nfunc main() {\n    if 1 < 2 {\n        print(\"yes\")\n    } else {\n        print(\"no\")\n    }\n}\n"

	got := generate(t, src)
	for _, want := range []string{"if ((1 < 2)) {", "} else {", "}"} {
		if !strings.Contains(got, want) {
			t.Errorf("generated C is missing %q:\n%s", want, got)
		}
	}
}

func TestNestsStatementsInsideBranches(t *testing.T) {
	src := "package main\n\nfunc main() {\n    if 1 < 2 {\n        print(\"yes\")\n    }\n}\n"

	got := generate(t, src)
	if !strings.Contains(got, "\n        tack_print(") {
		t.Errorf("the branch body was not indented inside the if:\n%s", got)
	}
}

func TestGeneratedProgramComputesFib(t *testing.T) {
	src := "package main\n\n" +
		"func fib(n int) int {\n" +
		"    if n < 2 {\n        return n\n    }\n" +
		"    return fib(n - 1) + fib(n - 2)\n" +
		"}\n\n" +
		"func main() {\n    exit(fib(10))\n}\n"

	err := exec.Command(build(t, src)).Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("running the generated program: err = %v, want a non-zero exit", err)
	}
	if exit.ExitCode() != 55 {
		t.Errorf("fib(10) exited %d, want 55", exit.ExitCode())
	}
}

func TestGeneratedProgramTakesTheElseBranch(t *testing.T) {
	src := "package main\n\n" +
		"func classify(n int) string {\n" +
		"    if n < 10 {\n        return \"small\"\n    } else {\n        return \"large\"\n    }\n" +
		"}\n\n" +
		"func main() {\n    print(classify(3))\n    print(classify(30))\n}\n"

	out, err := exec.Command(build(t, src)).Output()
	if err != nil {
		t.Fatalf("running the generated program: %v", err)
	}
	if string(out) != "small\nlarge\n" {
		t.Errorf("program printed %q, want %q", out, "small\nlarge\n")
	}
}

func TestGeneratedProgramEvaluatesEveryOperator(t *testing.T) {
	// (((7 + 3) - 2) * 3) / 2 % 7 == 12 % 7 == 5
	src := "package main\n\nfunc main() {\n    exit(((7 + 3) - 2) * 3 / 2 % 7)\n}\n"

	err := exec.Command(build(t, src)).Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("running the generated program: err = %v, want a non-zero exit", err)
	}
	if exit.ExitCode() != 5 {
		t.Errorf("exit status = %d, want 5", exit.ExitCode())
	}
}

// A call whose arguments mix calls and literals must pass each one in its own
// position. sub is not commutative, so swapped arguments change the result.
func TestGeneratedProgramMixesCallAndLiteralArguments(t *testing.T) {
	// sub(10, 3) * 10 + sub(20, 10) == 7 * 10 + 10 == 80
	src := "package main\n\n" +
		"func sub(a int, b int) int {\n    return a - b\n}\n\n" +
		"func ten() int {\n    return 10\n}\n\n" +
		"func main() {\n    exit(sub(ten(), 3) * 10 + sub(20, ten()))\n}\n"

	err := exec.Command(build(t, src)).Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("running the generated program: err = %v, want a non-zero exit", err)
	}
	if exit.ExitCode() != 80 {
		t.Errorf("exit status = %d, want 80", exit.ExitCode())
	}
}

// Both operands of a binary expression can be calls. The subtraction pins the
// operand order, which addition alone would not.
func TestGeneratedProgramCombinesTwoCallResults(t *testing.T) {
	// (7 + 2) * 10 + (7 - 2) == 95
	src := "package main\n\n" +
		"func seven() int {\n    return 7\n}\n\n" +
		"func two() int {\n    return 2\n}\n\n" +
		"func main() {\n    exit((seven() + two()) * 10 + (seven() - two()))\n}\n"

	err := exec.Command(build(t, src)).Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("running the generated program: err = %v, want a non-zero exit", err)
	}
	if exit.ExitCode() != 95 {
		t.Errorf("exit status = %d, want 95", exit.ExitCode())
	}
}
