package typeck_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// overflowMsg is the message for typed arithmetic on known values that
// overflows its type, wherever it is written.
func overflowMsg(typ string) string { return "constant expression overflows " + typ }

// invalidKnownOverflows are function bodies with typed arithmetic on known
// values whose exact result is not a value of its type, with the one
// diagnostic each must produce and the code it must cover: the step that
// overflowed, not the expression around it.
var invalidKnownOverflows = []struct {
	name string
	src  string
	want spanned
}{
	{"product", mainLines("x := 'a' * 3", "exit(x)"), spanned{overflowMsg("uint8"), "'a' * 3"}},
	{
		"difference below zero",
		mainLines("x := 'a' - 'b'", "exit(x)"),
		spanned{overflowMsg("uint8"), "'a' - 'b'"},
	},
	{
		"sum",
		mainLines("x := 'z' + 'z' + 'z'", "exit(x)"),
		spanned{overflowMsg("uint8"), "'z' + 'z' + 'z'"},
	},
	{
		"inner step",
		mainLines("x := 'a' * 3 / 3", "exit(x)"),
		spanned{overflowMsg("uint8"), "'a' * 3"},
	},
	{
		"reported once",
		mainLines("x := 'a' * 3 * 3", "exit(x)"),
		spanned{overflowMsg("uint8"), "'a' * 3"},
	},
	{"as an argument", mainLines("exit('a' * 3)"), spanned{overflowMsg("uint8"), "'a' * 3"}},
	{
		"in a condition",
		mainLines("if 'a' * 3 > 0 {", "}"),
		spanned{overflowMsg("uint8"), "'a' * 3"},
	},
	{
		"as a range count",
		mainLines("for i := range 'a' * 3 {", "    exit(i)", "}"),
		spanned{overflowMsg("uint8"), "'a' * 3"},
	},
	{
		"as a compound value",
		mainLines("mut x uint8 = 0", "x += 'a' * 3", "exit(x)"),
		spanned{overflowMsg("uint8"), "'a' * 3"},
	},
	{
		"as an array length",
		mainLines("mut a ['a' * 3]int64", "exit(len(a))"),
		spanned{overflowMsg("uint8"), "'a' * 3"},
	},
	{
		"as a parameter's array length",
		"package main\n\nfunc f(a ['a' * 3]int64) {\n}\n\nfunc main() {\n}\n",
		spanned{overflowMsg("uint8"), "'a' * 3"},
	},
	{
		"returned",
		"package main\n\nfunc f() uint8 {\n    return 'a' * 3\n}\n\nfunc main() {\n    exit(f())\n}\n",
		spanned{overflowMsg("uint8"), "'a' * 3"},
	},
	{
		"immutable local",
		mainLines("c := 'z'", "x := c * c", "exit(x)"),
		spanned{overflowMsg("uint8"), "c * c"},
	},
	{
		"chain of immutable locals",
		mainLines("a := 'z'", "b := a - 'a'", "x := b * 11", "exit(x)"),
		spanned{overflowMsg("uint8"), "b * 11"},
	},
	{
		"immutable global",
		"package main\n\nvar big uint8 = 200\n\nfunc main() {\n    exit(big + big)\n}\n",
		spanned{overflowMsg("uint8"), "big + big"},
	},
	{
		"int64 above the largest",
		mainLines("var m int64 = 9223372036854775807", "exit(m + 1)"),
		spanned{overflowMsg("int64"), "m + 1"},
	},
	{
		"int64 below the smallest",
		mainLines("var m int64 = -9223372036854775808", "exit(m - 1)"),
		spanned{overflowMsg("int64"), "m - 1"},
	},
	{
		"uint64 above the largest",
		mainLines("var m uint64 = 18446744073709551615", "x := m + 1", "if x == x {", "}"),
		spanned{overflowMsg("uint64"), "m + 1"},
	},
	{
		"uint64 product beyond every constant",
		mainLines("var m uint64 = 18446744073709551615", "x := m * m", "if x == x {", "}"),
		spanned{overflowMsg("uint64"), "m * m"},
	},
	{
		"int8 product",
		mainLines("var a int8 = 64", "exit(a * 2)"),
		spanned{overflowMsg("int8"), "a * 2"},
	},
	{
		"widened operand",
		mainLines("var w uint16 = 65535", "exit(w + 'a')"),
		spanned{overflowMsg("uint16"), "w + 'a'"},
	},
	{
		"len of a string literal",
		mainLines(`exit(len("abc") * 4611686018427387904)`),
		spanned{overflowMsg("int64"), `len("abc") * 4611686018427387904`},
	},
	{
		"len of an array",
		mainLines("mut a [2]int64", "exit(len(a) - 9223372036854775807 - 4)"),
		spanned{overflowMsg("int64"), "len(a) - 9223372036854775807 - 4"},
	},
	{
		"negated int8 minimum",
		mainLines("var m int8 = -128", "exit(-m)"),
		spanned{overflowMsg("int8"), "-m"},
	},
	{
		"negated int64 minimum",
		mainLines("var m int64 = -9223372036854775808", "exit(-m)"),
		spanned{overflowMsg("int64"), "-m"},
	},
	{
		"int64 minimum divided by -1",
		mainLines("var m int64 = -9223372036854775808", "var d int64 = -1", "exit(m / d)"),
		spanned{overflowMsg("int64"), "m / d"},
	},
	{
		"int8 minimum divided by -1",
		mainLines("var m int8 = -128", "var d int8 = -1", "exit(m / d)"),
		spanned{overflowMsg("int8"), "m / d"},
	},
	{
		"checked division of the minimum by -1",
		mainLines(
			"var m int8 = -128",
			"var d int8 = -1",
			"q, ok := m / d",
			"if ok {",
			"    exit(q)",
			"}",
		),
		spanned{overflowMsg("int8"), "m / d"},
	},
}

// TestRejectsKnownOverflowInFunctions checks each of invalidKnownOverflows
// is one error, covering the step that overflowed.
func TestRejectsKnownOverflowInFunctions(t *testing.T) {
	for _, tt := range invalidKnownOverflows {
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

// validRuntimeArithmetic are function bodies whose typed arithmetic either
// fits its type exactly, at the boundaries, or wraps, written with a
// wrapping operator, on a value the checker does not know, or knows and
// computes wrapped.
var validRuntimeArithmetic = []struct{ name, src string }{
	{"mutable local", mainLines("mut c = 'a'", "x := c *% 3", "exit(x)")},
	{"compound on a mutable local", mainLines("mut c = 'a'", "c *%= 3", "exit(c)")},
	{"increment past the largest", mainLines("mut b int8 = 127", "b +%= 1", "exit(b)")},
	{"decrement below zero", mainLines("mut u uint8 = 0", "u -%= 1", "exit(u)")},
	{
		"immutable local of a mutable one",
		mainLines("mut m = 'a'", "c := m", "x := c *% 3", "exit(x)"),
	},
	{
		"parameter",
		"package main\n\nfunc f(c uint8) uint8 {\n    return c *% 3\n}\n\nfunc main() {\n    exit(f('a'))\n}\n",
	},
	{
		"mutable global",
		"package main\n\nmut g uint8 = 200\n\nfunc main() {\n    exit(g +% g)\n}\n",
	},
	{"element of an immutable array", mainLines("a := [2]uint8{200, 200}", "exit(a[0] +% a[1])")},
	{"known values wrapped", mainLines("var m int8 = 127", "x := m +% 1", "exit(int32(x) +% 128)")},
	{"largest uint8", mainLines("exit('a' + 158)")},
	{"zero uint8", mainLines("exit('a' - 'a' + 0)")},
	{
		"largest int64",
		mainLines("var m int64 = 9223372036854775806", "x := m + 1", "if x == x {", "}"),
	},
	{
		"smallest int64",
		mainLines("var m int64 = -9223372036854775807", "x := m - 1", "if x == x {", "}"),
	},
	{"negated int8 above the minimum", mainLines("var m int8 = -127", "exit(-m)")},
	{"minimum divided by 1", mainLines("var m int8 = -128", "var d int8 = 1", "exit(m / d)")},
	{"minimum remainder by -1", mainLines("var m int8 = -128", "var d int8 = -1", "exit(m % d)")},
	{"untyped steps are exact", mainLines("x := 9223372036854775807 + 1 - 1", "if x == x {", "}")},
	{
		"negated untyped minimum",
		mainLines("x := -(-9223372036854775807 - 1 + 1)", "if x == x {", "}"),
	},
}

// TestAcceptsRuntimeArithmetic checks each of validRuntimeArithmetic has no
// diagnostics.
func TestAcceptsRuntimeArithmetic(t *testing.T) {
	for _, tt := range validRuntimeArithmetic {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// unprovenRuntimeArithmetic are the plain forms of the wrapping operations
// in validRuntimeArithmetic, on values the checker cannot bound within the
// type, or bounds past it, each one error saying it may overflow,
// covering the operation.
var unprovenRuntimeArithmetic = []struct {
	name string
	src  string
	want spanned
}{
	{
		"mutable local", mainLines("mut c = 'a'", "x := c * 3", "exit(x)"),
		spanned{"c * 3 may overflow uint8; bound c first, or use v, ok := c * 3, or c *% 3 to wrap", "c * 3"},
	},
	{
		"compound on a mutable local", mainLines("mut c = 'a'", "c *= 3", "exit(c)"),
		spanned{"c *= 3 may overflow uint8; use c *%= 3 to wrap, or v, ok := c * 3 and c = v when ok", "c *= 3"},
	},
	{
		"increment past the largest", mainLines("mut b int8 = 127", "b++", "exit(b)"),
		spanned{"b++ may overflow int8; use b +%= 1 to wrap, or v, ok := b + 1 and b = v when ok", "b++"},
	},
	{
		"decrement below zero", mainLines("mut u uint8 = 0", "u--", "exit(u)"),
		spanned{
			"u-- may go below 0 in uint8; check u > 0 first, use u -%= 1 to wrap, or v, ok := u - 1 and u = v when ok",
			"u--",
		},
	},
	{
		"parameter",
		"package main\n\nfunc f(c uint8) uint8 {\n    return c * 3\n}\n\nfunc main() {\n    exit(f('a'))\n}\n",
		spanned{"c * 3 may overflow uint8; bound c first, or use v, ok := c * 3, or c *% 3 to wrap", "c * 3"},
	},
	{
		"mutable global",
		"package main\n\nmut g uint8 = 200\n\nfunc main() {\n    exit(g + g)\n}\n",
		spanned{"g + g may overflow uint8; use v, ok := g + g, or g +% g to wrap", "g + g"},
	},
	{
		"element of an immutable array", mainLines("a := [2]uint8{200, 200}", "exit(a[0] + a[1])"),
		spanned{
			"a[0] + a[1] may overflow uint8; bound a[0] and a[1] first, or use v, ok := a[0] + a[1], or a[0] +% a[1] to wrap",
			"a[0] + a[1]",
		},
	},
}

// TestRejectsUnprovenRuntimeArithmetic checks each of
// unprovenRuntimeArithmetic is one error, covering the operation.
func TestRejectsUnprovenRuntimeArithmetic(t *testing.T) {
	for _, tt := range unprovenRuntimeArithmetic {
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

// TestKnownValuesOfALongChainAreFast checks a chain of immutable locals,
// each using the one before twice, is checked in steps linear in its
// length (see checkGrowth): knownVar and knownArithmetic remember each
// answer rather than computing it again for every use, which would take
// steps doubling with each local. The chains are short, 5 and 20 locals,
// so that a regression fails with a count rather than running for ever.
func TestKnownValuesOfALongChainAreFast(t *testing.T) {
	const n = 5
	diags, small := checkWork(t, mainLines(knownChain(n)...))
	if len(diags) > 0 {
		t.Fatalf("got %v, want no diagnostics", diags)
	}
	_, grown := checkWork(t, mainLines(knownChain(4*n)...))
	checkGrowth(t, "known values of locals worked out", small.Locals, grown.Locals)
	checkGrowth(t, "known values worked out", small.Arithmetic, grown.Arithmetic)
}

// knownChain returns the lines of a chain of n immutable locals after x0,
// each the one before added to itself, and a use of the last.
func knownChain(n int) []string {
	lines := []string{"x0 := 'a' - 'a'"}
	for i := 1; i <= n; i++ {
		lines = append(
			lines,
			"x"+strconv.Itoa(i)+" := x"+strconv.Itoa(i-1)+" + x"+strconv.Itoa(i-1),
		)
	}
	return append(lines, "exit(x"+strconv.Itoa(n)+")")
}

// TestKnownValuesOfLongExpressionsAreFast checks expressions of
// thousands of terms, each of whose operators asks for the known value
// of the chain below it, are checked in steps linear in their length (see
// checkGrowth): a chain of a parameter, whose value is not known, nested
// left and right, and a chain of an immutable local whose value is, in
// every sized integer type. A chain nested on the right nests a level per
// term, so it is segments of 999 terms each, within the parser's limit of
// a thousand levels, joined by + into one as long as the others.
func TestKnownValuesOfLongExpressionsAreFast(t *testing.T) {
	const n = 2000
	diags, small := checkWork(t, longExpressions(n))
	if len(diags) > 0 {
		t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
	}
	_, grown := checkWork(t, longExpressions(4*n))
	checkGrowth(t, "known values worked out", small.Arithmetic, grown.Arithmetic)
}

// longExpressions returns a program whose functions, one for each sized
// integer type, each return a sum of chains of n terms: of a parameter,
// nested left and right, the right in segments of 999, and of a known
// local. n is a multiple of a thousand.
func longExpressions(n int) string {
	var b strings.Builder
	b.WriteString("package main\n\n")
	for _, typ := range []string{"int8", "int16", "int32", "int64", "uint8", "uint16", "uint32", "uint64"} {
		left := strings.Repeat("x +% ", n-1) + "x"
		segment := strings.Repeat("x *% (", 998) + "x" + strings.Repeat(")", 998)
		right := strings.Join(slices.Repeat([]string{segment}, n/1000), " +% ")
		known := strings.Repeat("k + ", n-1) + "k"
		b.WriteString("func f_" + typ + "(x " + typ + ") " + typ + " {\n" +
			"    k := " + typ + "(0)\n" +
			"    return " + left + " +% " + right + " +% (" + known + ")\n}\n\n")
	}
	b.WriteString("func main() {\n}\n")
	return b.String()
}
