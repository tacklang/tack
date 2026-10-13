package typeck_test

import (
	"testing"
)

// knownStringLenCases use the length of an immutable string variable that
// holds a known string, a literal or another such variable, as the known
// value it is: to prove an index, as a divisor, an array length or a
// conversion's operand. stringing's k is an immutable global holding "k".
var knownStringLenCases = []struct{ name, src string }{
	{"constant index into a local", stringing("x := \"abc\"", "return x[0] + x[2]")},
	{"constant index into a global", stringing("return k[0]")},
	{"chain of locals", stringing("x := \"abc\"", "y := x", "z := y", "return z[2]")},
	{"local from a global", stringing("x := k", "return x[0]")},
	{"len as a known value", stringing("x := \"abc\"", "n := len(x)", "return x[n - 1]")},
	{
		"guard by a constant",
		stringing("x := \"abcdef\"", "if 0 <= i && i < 6 {", "    return x[i]", "}"),
	},
	{
		"guard by len minus one",
		stringing("x := \"abc\"", "if 0 <= i && i <= len(x) - 1 {", "    return x[i]", "}"),
	},
	{
		"range over the count",
		stringing("x := \"abc\"", "for j := range 3 {", "    return x[j]", "}"),
	},
	{
		"range over the string",
		stringing("x := \"abc\"", "for j := range x {", "    return x[j]", "}"),
	},
	{
		"range over the string indexes an array",
		indexing("w := \"abc\"", "for k := range w {", "    return a[k]", "}"),
	},
	{"unsigned index", stringing("x := \"abc\"", "if u < 3 {", "    return x[u]", "}")},
	{
		"array length",
		stringing("x := \"abc\"", "arr := [len(x)]int64{1, 2, 3}", "exit(arr[2])"),
	},
	{"array length of a global", stringing("arr := [len(k)]int64{7}", "exit(arr[0])")},
	{"divisor", stringing("x := \"ab\"", "exit(10 / len(x))")},
	{"conversion", stringing("x := \"abc\"", "return uint8(len(x))")},
	{"empty string length", stringing("x := \"\"", "arr := [len(x)]int64{}", "exit(len(arr))")},
	{"raw string", stringing("x := `a\\b`", "return x[2]")},
	{
		"checked index proven",
		stringing("x := \"abc\"", "v, ok := x[0]", "if ok {", "    return v", "}"),
	},
}

// TestAcceptsKnownStringLengths checks each of knownStringLenCases has no
// diagnostics.
func TestAcceptsKnownStringLengths(t *testing.T) {
	for _, tt := range knownStringLenCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// unknownStringLenCases are uses of a string variable's length that must
// still be rejected, or that a known length rejects at compile time, with
// the one diagnostic each must produce.
var unknownStringLenCases = []struct {
	name string
	src  string
	want spanned
}{
	{
		"constant index past a local",
		stringing("x := \"abc\"", "return x[3]"),
		spanned{"index 3 is out of range for a string of 3 bytes", "3"},
	},
	{
		"constant index into an empty local",
		stringing("x := \"\"", "return x[0]"),
		spanned{"index 0 is out of range for a string of 0 bytes", "0"},
	},
	{
		"constant index past a global",
		stringing("return k[1]"),
		spanned{"index 1 is out of range for a string of 1 byte", "1"},
	},
	{
		"constant index past a chain",
		stringing("x := \"abc\"", "y := x", "return y[5]"),
		spanned{"index 5 is out of range for a string of 3 bytes", "5"},
	},
	{
		"checked index past the length",
		stringing("x := \"abc\"", "v, ok := x[3]", "if ok {", "    return v", "}"),
		spanned{"index 3 is out of range for a string of 3 bytes", "3"},
	},
	{
		"guard past the length",
		stringing("x := \"abc\"", "if 0 <= i && i < 4 {", "    return x[i]", "}"),
		spanned{
			"index i may be out of range; check 0 <= i && i < len(x) first, or use v, ok := ...[i]",
			"i",
		},
	},
	{
		"mutable local",
		stringing("return t[0]"),
		spanned{"index 0 may be out of range; check len(t) > 0 first, or use v, ok := ...[0]", "0"},
	},
	{
		"mutable global",
		stringing("return g[0]"),
		spanned{"index 0 may be out of range; check len(g) > 0 first, or use v, ok := ...[0]", "0"},
	},
	{
		"immutable local from a parameter",
		stringing("x := s", "return x[0]"),
		spanned{"index 0 may be out of range; check len(x) > 0 first, or use v, ok := ...[0]", "0"},
	},
	{
		"immutable local from a mutable one",
		stringing("x := t", "return x[0]"),
		spanned{"index 0 may be out of range; check len(x) > 0 first, or use v, ok := ...[0]", "0"},
	},
	{
		"immutable local from a call",
		stringing("x := name()", "return x[0]"),
		spanned{"index 0 may be out of range; check len(x) > 0 first, or use v, ok := ...[0]", "0"},
	},
	{
		"array length of a mutable string",
		stringing("arr := [len(t)]int64{1, 2, 3, 4}", "exit(arr[0])"),
		spanned{"array length must be a constant integer", "len(t)"},
	},
	{
		"divisor of an empty string's length",
		stringing("x := \"\"", "exit(10 / len(x))"),
		spanned{"division by zero", "len(x)"},
	},
}

// TestRejectsUnknownStringLengths checks each of unknownStringLenCases is
// one error, covering the right source.
func TestRejectsUnknownStringLengths(t *testing.T) {
	for _, tt := range unknownStringLenCases {
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
