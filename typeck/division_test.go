package typeck_test

import (
	"testing"
)

// divide wraps statements in a function that takes a dividend a, a divisor
// b and a flag x, and returns an int64, ending in return 0.
func divide(lines ...string) string {
	body := ""
	for _, l := range lines {
		body += "    " + l + "\n"
	}
	return "package main\n\nvar k = 3\n\nmut g int64 = 1\n\n" +
		"func f(a int64, b int64, x bool) int64 {\n" + body + "    return 0\n}\n\n" +
		"func main() {\n    exit(f(1, 2, true) + k + g)\n}\n"
}

// provenDivisorCases are divisions whose divisor the checker can prove is
// not zero, each by one of the rules.
var provenDivisorCases = []struct{ name, src string }{
	{"constant", divide("return a / 2 + a % -1")},
	{"immutable constant local", divide("d := 4", "return a / d")},
	{"immutable constant global", divide("return a / k")},
	{"not equal zero", divide("if b != 0 {", "    return a / b", "}")},
	{"zero on the left", divide("if 0 != b {", "    return a % b", "}")},
	{"greater than zero", divide("if b > 0 {", "    return a / b", "}")},
	{"at least one", divide("if b >= 1 {", "    return a / b", "}")},
	{"negative", divide("if b < 0 {", "    return a / b", "}")},
	{"at most minus one", divide("if b <= -1 {", "    return a / b", "}")},
	{"equal to a non-zero constant", divide("if b == 7 {", "    return a / b", "}")},
	{"constant on the left", divide("if 0 < b {", "    return a / b", "}")},
	{
		"else of equal zero",
		divide("if b == 0 {", "    return 1", "} else {", "    return a / b", "}"),
	},
	{
		"else of not not zero",
		divide("if !(b != 0) {", "    return 1", "} else {", "    return a / b", "}"),
	},
	{
		"else of an or",
		divide("if b == 0 || x {", "    return 1", "} else {", "    return a / b", "}"),
	},
	{"one side of an and", divide("if x && b != 0 {", "    return a / b", "}")},
	{"nested if", divide("if b != 0 {", "    if x {", "        return a / b", "    }", "}")},
	{"after an early return", divide("if b == 0 {", "    return 0", "}", "return a / b")},
	{
		"after an early return in an else",
		divide(
			"if b != 0 {",
			"    print(\"\")",
			"} else {",
			"    return 0",
			"}",
			"return a / b",
		),
	},
	{
		"after both branches return",
		divide(
			"if x {",
			"    return 1",
			"}",
			"if b <= 0 {",
			"    return 0",
			"}",
			"return a / b",
		),
	},
	{"right side of and", divide("if b != 0 && a / b > 1 {", "    return 1", "}")},
	{"right side of or", divide("if b == 0 || a / b > 1 {", "    return 1", "}")},
	{
		"after a break",
		divide("for {", "    if b == 0 {", "        break", "    }", "    return a / b", "}"),
	},
	{
		"after a continue",
		divide(
			"for range 3 {",
			"    if b == 0 {",
			"        continue",
			"    }",
			"    return a / b",
			"}",
		),
	},
	{"loop condition", divide("for b != 0 {", "    return a / b", "}")},
	{
		"mutable but not changed",
		divide("mut d := b", "if d != 0 {", "    return a / d", "}", "d = 0", "return d"),
	},
	{"compound guarded", divide("mut c := a", "if b != 0 {", "    c /= b", "}", "return c")},
	{"narrow divisor", divide("var n int8 = 5", "if n != 0 {", "    return a / n", "}")},
	{"checked division", divide("q, ok := a / b", "if ok {", "    return q", "}")},
	{"checked remainder", divide("r, ok := a % (b + 1)", "if ok {", "    return r", "}")},
	{
		"checked division assigned",
		divide(
			"mut q := 0",
			"mut ok := false",
			"q, ok = a / b",
			"if ok {",
			"    return q",
			"}",
		),
	},
	{
		"checked with a constant",
		divide("q, ok := a / 3", "if ok {", "    return q", "}"),
	},
	{
		"checked with a proven divisor",
		divide(
			"if b != 0 {",
			"    q, ok := a / b",
			"    if ok {",
			"        return q",
			"    }",
			"}",
		),
	},
	{
		"checked constants",
		divide("q, ok := 7 / 2", "if ok {", "    return q", "}"),
	},
	{
		"checked constants assigned to a narrower variable",
		divide(
			"mut q uint8 = 0",
			"mut ok := false",
			"q, ok = 7 / 2",
			"if ok {",
			"    return q",
			"}",
		),
	},
}

// TestAcceptsProvenDivisors checks each of provenDivisorCases has no
// diagnostics.
func TestAcceptsProvenDivisors(t *testing.T) {
	for _, tt := range provenDivisorCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// unprovenDivisorCases are divisions whose divisor may be zero, or is, with
// the diagnostic each must produce.
var unprovenDivisorCases = []struct {
	name string
	src  string
	want spanned
}{
	{
		"unguarded",
		divide("return a / b"),
		spanned{"divisor b may be zero; check b != 0 first, or use q, ok := ... / b", "b"},
	},
	{
		"unguarded remainder",
		divide("return a % b"),
		spanned{"divisor b may be zero; check b != 0 first, or use q, ok := ... / b", "b"},
	},
	{
		"an expression",
		divide("return a / (b + 1)"),
		spanned{"divisor may be zero; use q, ok := ... / ...", "b + 1"},
	},
	{
		"a call",
		divide("return a / f(a, b, x)"),
		spanned{"divisor may be zero; use q, ok := ... / ...", "f(a, b, x)"},
	},
	{"literal zero", divide("return a / 0"), spanned{"division by zero", "0"}},
	{"literal zero remainder", divide("return a % 0"), spanned{"division by zero", "0"}},
	{"constant zero local", divide("z := 0", "return a / z"), spanned{"division by zero", "z"}},
	{
		"compound literal zero",
		divide("mut c := a", "c /= 0", "return c"),
		spanned{"division by zero", "0"},
	},
	{
		"compound unguarded",
		divide("mut c := a", "c %= b", "return c"),
		spanned{"divisor b may be zero; check b != 0 first", "b"},
	},
	{
		"after the guarded block",
		divide("if b != 0 {", "    print(\"\")", "}", "return a / b"),
		spanned{"divisor b may be zero; check b != 0 first, or use q, ok := ... / b", "b"},
	},
	{
		"else of not zero",
		divide("if b != 0 {", "    return 1", "} else {", "    return a / b", "}"),
		spanned{"divisor b may be zero; check b != 0 first, or use q, ok := ... / b", "b"},
	},
	{
		"one side of an or",
		divide("if b != 0 || x {", "    return a / b", "}"),
		spanned{"divisor b may be zero; check b != 0 first, or use q, ok := ... / b", "b"},
	},
	{
		"greater than minus one",
		divide("if b > -1 {", "    return a / b", "}"),
		spanned{"divisor b may be zero; check b != 0 first, or use q, ok := ... / b", "b"},
	},
	{
		"at most zero",
		divide("if b <= 0 {", "    return a / b", "}"),
		spanned{"divisor b may be zero; check b != 0 first, or use q, ok := ... / b", "b"},
	},
	{
		"compared with a variable",
		divide("if b != a {", "    return a / b", "}"),
		spanned{"divisor b may be zero; check b != 0 first, or use q, ok := ... / b", "b"},
	},
	{
		"assigned inside the guard",
		divide("mut d := b", "if d != 0 {", "    d = 0", "    return a / d", "}"),
		spanned{"divisor d may be zero; check d != 0 first, or use q, ok := ... / d", "d"},
	},
	{
		"assigned later inside the guard",
		divide("mut d := b", "if d != 0 {", "    c := a / d", "    d--", "    return c + d", "}"),
		spanned{"divisor d may be zero; check d != 0 first, or use q, ok := ... / d", "d"},
	},
	{
		"assigned after the early return",
		divide(
			"mut d := b",
			"if d == 0 {",
			"    return 0",
			"}",
			"c := a / d",
			"d = 0",
			"return c + d",
		),
		spanned{"divisor d may be zero; check d != 0 first, or use q, ok := ... / d", "d"},
	},
	{
		"loop body that changes the divisor",
		divide("mut d := b", "for d != 0 {", "    print(\"\")", "    return a / d", "    d--", "}"),
		spanned{"divisor d may be zero; check d != 0 first, or use q, ok := ... / d", "d"},
	},
	{
		"mutable global",
		divide("if g != 0 {", "    return a / g", "}"),
		spanned{"divisor g may be zero; check g != 0 first, or use q, ok := ... / g", "g"},
	},
	{
		"exit does not leave",
		divide("if b == 0 {", "    exit(1)", "}", "return a / b"),
		spanned{"divisor b may be zero; check b != 0 first, or use q, ok := ... / b", "b"},
	},
	{
		"checked constants that do not fit the variable",
		divide(
			"mut q uint8 = 0",
			"mut ok := false",
			"q, ok = 600 / 2",
			"if ok {",
			"    return q",
			"}",
		),
		spanned{"constant 300 overflows uint8", "600 / 2"},
	},
	{
		"checked by zero",
		divide("q, ok := a / 0", "if ok {", "    return q", "}"),
		spanned{"division by zero", "0"},
	},
	{
		"checked bound to three names",
		divide("q, ok, z := a / b", "if ok && z {", "    return q", "}"),
		spanned{
			"assignment mismatch: 3 variables but a division gives 2 values",
			"q, ok, z := a / b",
		},
	},
}

// TestRejectsUnprovenDivisors checks each division is one error, covering
// the divisor, or the whole division where the ok is the problem.
func TestRejectsUnprovenDivisors(t *testing.T) {
	for _, tt := range unprovenDivisorCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != 1 {
				t.Fatalf("got %v, want only %q", diags, tt.want.msg)
			}
			if diags[0].Message != tt.want.msg {
				t.Errorf("message = %q, want %q", diags[0].Message, tt.want.msg)
			}
			if got := tt.src[diags[0].Span.Start:diags[0].Span.End]; got != tt.want.at {
				t.Errorf("diagnostic covers %q, want %q", got, tt.want.at)
			}
		})
	}
}
