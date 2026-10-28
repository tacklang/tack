package typeck_test

import (
	"testing"
)

// converting wraps statements in a function that takes an int64 x, with an
// immutable global k holding 200, a mutable global g, and an array a of
// three elements, and returns a uint8, ending in return 0.
func converting(lines ...string) string {
	body := ""
	for _, l := range lines {
		body += "    " + l + "\n"
	}
	return "package main\n\nvar k int64 = 200\nvar big uint64 = 18446744073709551615\n" +
		"mut g int64 = 1\n\n" +
		"func f(x int64) uint8 {\n    a := [3]int64{1, 2, 3}\n" + body +
		"    code, fits := int32(a[0] + g + k)\n    if fits { exit(code) }\n    if big > 0 {\n        return 0\n    }\n    return 0\n}\n\n" +
		"func main() {\n    exit(f(1))\n}\n"
}

// knownConversionCases are conversions of a known value that is a value of
// the type converted to, which compile like a constant's, at each boundary.
var knownConversionCases = []struct{ name, src string }{
	{"immutable local", converting("n := 5", "return uint8(n)")},
	{"character literal to int8", converting("exit(int8('a'))")},
	{"immutable global", converting("return uint8(k)")},
	{"len of an array", converting("return uint8(len(a))")},
	{"len of a string literal", converting("return uint8(len(\"tack\"))")},
	{"arithmetic on known values", converting("n := 5", "return uint8(n * 51)")},
	{"uint8 maximum", converting("n := 255", "return uint8(n)")},
	{
		"zero across signs",
		converting("n := 0", "w := uint64(n)", "if w == 0 {", "    return 1", "}"),
	},
	{"int8 minimum", converting("n := -128", "exit(int8(n))")},
	{"int8 maximum", converting("n := 127", "exit(int8(n))")},
	{
		"int64 maximum to uint64",
		converting("n := 9223372036854775807", "w := uint64(n)", "if w > 0 {", "    return 1", "}"),
	},
	{"uint8 to int8", converting("var u uint8 = 127", "exit(int8(u))")},
	{
		"uint64 to int64",
		converting(
			"var u uint64 = 9223372036854775807",
			"w := int64(u)",
			"if w > 0 {",
			"    return 1",
			"}",
		),
	},
	{"chain of immutable locals", converting("n := 2", "m := n * 100", "return uint8(m)")},
	{"negated known value", converting("n := 128", "exit(int8(-n))")},
	{
		"checked known value",
		converting("n := 5", "v, ok := uint8(n)", "if ok {", "    return v", "}"),
	},
	{
		"checked character literal",
		converting("v, ok := int8('a')", "if ok {", "    exit(v)", "}"),
	},
}

// TestAcceptsKnownConversions checks each of knownConversionCases has no
// diagnostics.
func TestAcceptsKnownConversions(t *testing.T) {
	for _, tt := range knownConversionCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// rejectedConversionCases are conversions the checker must still reject,
// with the one diagnostic each must produce: a known value that is not a
// value of the type, in either form, and every value that is not known,
// guarded or not.
var rejectedConversionCases = []struct {
	name string
	src  string
	want spanned
}{
	{
		"known value too large",
		converting("n := 300", "return uint8(n)"),
		spanned{"constant 300 overflows uint8", "n"},
	},
	{
		"known value negative to unsigned",
		converting("n := -1", "w := uint64(n)", "if w > 0 {", "    return 1", "}"),
		spanned{"constant -1 overflows uint64", "n"},
	},
	{
		"known value below int8",
		converting("n := -129", "exit(int8(n))"),
		spanned{"constant -129 overflows int8", "n"},
	},
	{
		"character arithmetic past int8",
		converting("c := 'a' + 100", "exit(int8(c))"),
		spanned{"constant 197 overflows int8", "c"},
	},
	{
		"immutable global too large",
		converting("exit(int8(k))"),
		spanned{"constant 200 overflows int8", "k"},
	},
	{
		"uint64 maximum to int64",
		converting("exit(int64(big))"),
		spanned{"constant 18446744073709551615 overflows int64", "big"},
	},
	{
		"arithmetic past the type",
		converting("return uint8(len(a) * 100)"),
		spanned{"constant 300 overflows uint8", "len(a) * 100"},
	},
	{
		"checked known value too large",
		converting("n := 300", "v, ok := uint8(n)", "if ok {", "    return v", "}"),
		spanned{"constant 300 overflows uint8", "n"},
	},
	{
		"mutable local",
		converting("mut n int64 = 5", "return uint8(n)"),
		spanned{
			"converting int64 to uint8 can fail; check 0 <= n && n <= 255 first, or use v, ok := uint8(...)",
			"uint8(n)",
		},
	},
	{
		"mutable global",
		converting("return uint8(g)"),
		spanned{"converting int64 to uint8 can fail; use v, ok := uint8(...)", "uint8(g)"},
	},
	{
		"parameter",
		converting("return uint8(x)"),
		spanned{
			"converting int64 to uint8 can fail; check 0 <= x && x <= 255 first, or use v, ok := uint8(...)",
			"uint8(x)",
		},
	},
	{
		"immutable local from a parameter",
		converting("n := x", "return uint8(n)"),
		spanned{
			"converting int64 to uint8 can fail; check 0 <= n && n <= 255 first, or use v, ok := uint8(...)",
			"uint8(n)",
		},
	},
	{
		"parameter guarded one past the top",
		converting("if 0 <= x && x <= 256 {", "    return uint8(x)", "}"),
		spanned{
			"converting int64 to uint8 can fail; check 0 <= x && x <= 255 first, or use v, ok := uint8(...)",
			"uint8(x)",
		},
	},
	{
		"parameter guarded one past the bottom",
		converting("if -1 <= x && x < 256 {", "    return uint8(x)", "}"),
		spanned{
			"converting int64 to uint8 can fail; check 0 <= x && x <= 255 first, or use v, ok := uint8(...)",
			"uint8(x)",
		},
	},
	{
		"mutable local guarded then assigned",
		converting(
			"mut n := x",
			"if 0 <= n && n < 256 {",
			"    n = 300",
			"    return uint8(n)",
			"}",
		),
		spanned{
			"converting int64 to uint8 can fail; check 0 <= n && n <= 255 first, or use v, ok := uint8(...)",
			"uint8(n)",
		},
	},
	{
		"guarded mutable global",
		converting("if 0 <= g && g < 256 {", "    return uint8(g)", "}"),
		spanned{"converting int64 to uint8 can fail; use v, ok := uint8(...)", "uint8(g)"},
	},
	{
		"guarded parameter plus one",
		converting("if 0 <= x && x < 255 {", "    return uint8(x + 1)", "}"),
		spanned{"converting int64 to uint8 can fail; use v, ok := uint8(...)", "uint8(x + 1)"},
	},
}

// provenConversionCases are conversions of a value no constant gives,
// which the facts in force prove a value of the type converted to.
var provenConversionCases = []struct{ name, src string }{
	{"guarded parameter", converting("if 0 <= x && x < 256 {", "    return uint8(x)", "}")},
	{
		"parameter compared equal to a constant",
		converting("if x == 5 {", "    return uint8(x)", "}"),
	},
	{"guarded at both edges", converting("if x >= 0 && x <= 255 {", "    return uint8(x)", "}")},
	{"early exit", converting("if x < 0 || x > 255 {", "    return 0", "}", "return uint8(x)")},
	{
		"else branch",
		converting("if x < 0 || x > 255 {", "    return 0", "} else {", "    return uint8(x)", "}"),
	},
	{
		"mutable local guarded",
		converting("mut n := x", "if 0 <= n && n < 256 {", "    return uint8(n)", "}", "n = 1"),
	},
	{
		"checked form where the plain one is proven",
		converting(
			"if 0 <= x && x < 256 {",
			"    v, ok := uint8(x)",
			"    if ok {",
			"        return v",
			"    }",
			"}",
		),
	},
}

// TestAcceptsProvenConversions checks that each of provenConversionCases
// compiles.
func TestAcceptsProvenConversions(t *testing.T) {
	for _, tt := range provenConversionCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) != 0 {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
		})
	}
}

// TestRejectsConversionsOfUnknownOrUnfitValues checks each of
// rejectedConversionCases is one error, covering the right source.
func TestRejectsConversionsOfUnknownOrUnfitValues(t *testing.T) {
	for _, tt := range rejectedConversionCases {
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
