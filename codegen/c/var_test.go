package c_test

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// run builds src strictly, runs it, and returns what it printed and its exit
// status.
func run(t *testing.T, src string) (stdout string, status int) {
	t.Helper()

	out, err := exec.Command(buildStrict(t, src)).Output()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exit):
		return string(out), exit.ExitCode()
	default:
		t.Fatalf("running the generated program: %v", err)
		return "", 0
	}
}

// runSanitized is run, with the program built under clang's undefined
// behavior sanitizer, set to stop it at the first undefined operation.
func runSanitized(t *testing.T, src string) (stdout string, status int) {
	t.Helper()

	bin := buildWith(t, src, "-O2", "-fsanitize=undefined", "-fno-sanitize-recover=all")
	out, err := exec.Command(bin).Output()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exit):
		return string(out), exit.ExitCode()
	default:
		t.Fatalf("running the generated program: %v", err)
		return "", 0
	}
}

// program wraps statements in a main function.
func program(lines ...string) string {
	return "package main\n\nfunc main() {\n    " + strings.Join(lines, "\n    ") + "\n}\n"
}

// A variable declared without mut is a const C local, and a read of it
// names that local.
func TestImmutableVariableIsAConstLocal(t *testing.T) {
	got := generate(t, program("x := 7", "exit(int32(x))"))

	if !strings.Contains(got, "const int64_t v_x = 7;") {
		t.Errorf("immutable variable not emitted as a const local:\n%s", got)
	}
	if !strings.Contains(got, "tack_builtin_exit(((int32_t)v_x));") {
		t.Errorf("read of x not lowered to v_x:\n%s", got)
	}
}

// A variable declared with mut is a C local without const, and an
// assignment to it is a C assignment.
func TestMutableVariableIsAPlainLocal(t *testing.T) {
	got := generate(
		t,
		program("mut x := 7", "x = 8", "code, fits := int32(x)", "if fits { exit(code) }"),
	)

	if !strings.Contains(got, "int64_t v_x = 7;") || strings.Contains(got, "const int64_t v_x") {
		t.Errorf("mutable variable not emitted as a plain local:\n%s", got)
	}
	if !strings.Contains(got, "v_x = 8;") {
		t.Errorf("assignment not lowered:\n%s", got)
	}
}

// variableRuntimeCases are programs and what each must print and exit with.
var variableRuntimeCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{"read back", program("x := 42", "exit(int32(x))"), "", 42},
	{"computed from others", program("a := 6", "b := 7", "c := a * b", "exit(int32(c))"), "", 42},
	{
		"reassigned",
		program("mut x := 1", "x = 41", "code, fits := int32(x)", "if fits { exit(code) }"),
		"",
		41,
	},
	{
		"updated from itself",
		program(
			"mut x := 5",
			"x = x * x",
			"x = x + 1",
			"code, fits := int32(x)",
			"if fits { exit(code) }",
		),
		"",
		26,
	},
	{"declared type", program("var x int64 = 9", "exit(int32(x))"), "", 9},
	{"string variable", program("s := \"hi\"", "print(s)"), "hi", 0},
	{"string reassigned", program("mut s := \"a\"", "s = \"b\"", "print(s)"), "b", 0},
	{"bool variable", program("b := 1 < 2", "if b {", "    exit(3)", "}"), "", 3},
	{
		"bool reassigned",
		program("mut b := 1 < 2", "b = 2 < 1", "if b {", "    exit(3)", "}", "exit(4)"),
		"",
		4,
	},
	{
		"zero int64",
		program("mut n int64", "code, fits := int32(n + 5)", "if fits { exit(code) }"),
		"",
		5,
	},
	{"zero string", program("mut s string", "print(s)"), "", 0},
	{"zero bool", program("mut b bool", "if b {", "    exit(1)", "}", "exit(2)"), "", 2},
	{
		"assigned in a branch",
		program(
			"mut x := 1",
			"if x < 2 {",
			"    x = 10",
			"}",
			"code, fits := int32(x)",
			"if fits { exit(code) }",
		),
		"",
		10,
	},
	{
		"same name in both branches",
		program(
			"if 1 < 2 {",
			"    x := 7",
			"    exit(int32(x))",
			"} else {",
			"    x := 8",
			"    exit(int32(x))",
			"}",
		),
		"",
		7,
	},
	{"names the C side uses", program(
		"int := 1", "str0 := 2", "tack_print := 3", "exit(int32(int + str0 + tack_print))"), "", 6},
	{"named like a builtin", program("exit := 9", "exit(int32(exit))"), "", 9},
}

// TestVariablesAtRuntime runs programs whose exit status or output shows that
// variables hold, update, and pass on the right values.
func TestVariablesAtRuntime(t *testing.T) {
	tests := variableRuntimeCases

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, status := run(t, tt.src)
			if stdout != tt.stdout || status != tt.status {
				t.Errorf(
					"got stdout %q, status %d; want %q, %d",
					stdout,
					status,
					tt.stdout,
					tt.status,
				)
			}
		})
	}
}

// A variable holds a value computed once. Later changes to what it was
// computed from do not reach it.
func TestVariableCopiesItsValue(t *testing.T) {
	src := program(
		"mut a := 1",
		"b := a",
		"a = 2",
		"code, fits := int32(b * 10 + a)",
		"if fits { exit(code) }",
	)
	if _, status := run(t, src); status != 12 {
		t.Errorf("status = %d, want 12", status)
	}
}

// Variables work alongside parameters and calls.
func TestVariablesWithParametersAndCalls(t *testing.T) {
	src := "package main\n\n" +
		"func square(n int64) int64 {\n    r := n * n\n    return r\n}\n\n" +
		"func main() {\n    mut total := 0\n    total = total + square(3)\n    total = total + square(4)\n    code, fits := int32(total)\n    if fits { exit(code) }\n}\n"
	if _, status := run(t, src); status != 25 {
		t.Errorf("status = %d, want 25", status)
	}
}
