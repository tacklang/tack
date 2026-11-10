package typeck_test

import (
	"testing"
)

// orFactCases are proofs from a condition that holds when at least one of
// several sides does: || being true, && being false, and a case with
// several values. Each variable every side constrains gets the union of
// the sides' facts, the smallest interval holding all of them and not
// zero when no side allows zero.
var orFactCases = []struct{ name, src string }{
	{"below or above zero", divide("if b < 0 || b > 0 {", "    return a / b", "}")},
	{"above or below zero", divide("if b > 0 || b < 0 {", "    return a / b", "}")},
	{
		"at most minus one or at least one",
		divide("if b <= -1 || b >= 1 {", "    return a / b", "}"),
	},
	{"two non-zero values", divide("if b == 1 || b == 2 {", "    return a / b", "}")},
	{"values either side of zero", divide("if b == -3 || b == 7 {", "    return a / b", "}")},
	{"negative or a positive value", divide("if b < 0 || b == 5 {", "    return a / b", "}")},
	{"three sides", divide("if b < -1 || b == 3 || b > 10 {", "    return a / b", "}")},
	{"and inside a side", divide("if b > 0 && x || b < 0 {", "    return a / b", "}")},
	{"not equal zero as a side", divide("if !(b == 0) || b > 5 {", "    return a / b", "}")},
	{
		"else of an and",
		divide("if b >= 0 && b <= 0 {", "    return 0", "} else {", "    return a / b", "}"),
	},
	{
		"else of a negated or",
		divide("if !(b < 0 || b > 0) {", "    return 0", "} else {", "    return a / b", "}"),
	},
	{
		"after an and that returns",
		divide("if b >= 0 && b <= 0 {", "    return 0", "}", "return a / b"),
	},
	{"loop condition", divide("for b < 0 || b > 0 {", "    return a / b", "}")},
	{"right of and", divide("if (b < 0 || b > 0) && a / b > 1 {", "    return 1", "}")},
	{"right of or", divide("if !(b < 0 || b > 0) || a / b > 1 {", "    return 1", "}")},
	{"tagless switch", divide("switch {", "case b < 0 || b > 0:", "    return a / b", "}")},
	{
		"tagless case with two values",
		divide("switch {", "case b < 0, b > 0:", "    return a / b", "}"),
	},
	{"case with two values", divide("switch b {", "case 1, 2:", "    return a / b", "}")},
	{
		"case with values either side of zero",
		divide("switch b {", "case -1, 1:", "    return a / b", "}"),
	},
	{
		"case with two values, then the statements after",
		divide("switch b {", "case 0, 0 - 1:", "    return 0", "default:", "    return a / b", "}"),
	},
	{"index equal to either", indexing("if i == 0 || i == 1 {", "    return a[i]", "}")},
	{"index range or value", indexing("if (0 <= i && i < 2) || i == 2 {", "    return a[i]", "}")},
	{"index case", indexing("switch i {", "case 0, 1, 2:", "    return a[i]", "}")},
	{"string length", stringing("if len(s) > 1 || len(s) == 1 {", "    return s[0]", "}")},
}

// TestAcceptsOrFacts checks each of orFactCases has no diagnostics.
func TestAcceptsOrFacts(t *testing.T) {
	for _, tt := range orFactCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// divisorB is the diagnostic for a divisor b not proven non-zero.
var divisorB = spanned{"divisor b may be zero; check b != 0 first, or use q, ok := ... / b", "b"}

// indexI is the diagnostic for an index i into a [3]int64 not proven.
var indexI = spanned{
	"index i may be out of range; check 0 <= i && i < 3 first, or use v, ok := ...[i]",
	"i",
}

// noOrFactCases are conditions of several sides that do not prove the
// division or index they guard, with the one diagnostic each must produce:
// a side says nothing about the variable, or the union of the sides allows
// zero, or a value out of range.
var noOrFactCases = []struct {
	name string
	src  string
	want spanned
}{
	{"one side is a bool", divide("if b < 0 || x {", "    return a / b", "}"), divisorB},
	{
		"one side is about another variable",
		divide("if b < 0 || a > 0 {", "    return a / b", "}"),
		divisorB,
	},
	{"sides cover zero", divide("if b < 0 || b >= 0 {", "    return a / b", "}"), divisorB},
	{"one side allows zero", divide("if b == 0 || b == 1 {", "    return a / b", "}"), divisorB},
	{"not zero or a bool", divide("if b != 0 || x {", "    return a / b", "}"), divisorB},
	{"third side is a bool", divide("if b > 0 || b < 0 || x {", "    return a / b", "}"), divisorB},
	{
		"sides about different variables",
		divide("if b > 0 || a != 0 {", "    return a / b", "}"),
		divisorB,
	},
	{
		"side against an unknown value",
		divide("if b > 0 || b < a {", "    return a / b", "}"),
		divisorB,
	},
	{
		"else of an and with a bool",
		divide("if b >= 0 && x {", "    return 0", "} else {", "    return a / b", "}"),
		divisorB,
	},
	{
		"or false keeps both sides",
		divide("if b < 0 || b > 0 {", "    return 0", "}", "return a / b"),
		divisorB,
	},
	{"case with zero", divide("switch b {", "case 0, 1:", "    return a / b", "}"), divisorB},
	{
		"case with an unknown value",
		divide("switch b {", "case 1, a:", "    return a / b", "}"),
		divisorB,
	},
	{
		"tagless case with a bool",
		divide("switch {", "case b < 0, x:", "    return a / b", "}"),
		divisorB,
	},
	{
		"region assigns the variable",
		divide(
			"mut d := b",
			"if d < 0 || d > 0 {",
			"    d -%= 1",
			"    code, fits := int32(a / d)",
			"    if fits { exit(code) }",
			"}",
		),
		spanned{"divisor d may be zero; check d != 0 first, or use q, ok := ... / d", "d"},
	},
	{"index past the end", indexing("if i == 0 || i == 3 {", "    return a[i]", "}"), indexI},
	{"index with no lower bound", indexing("if i == 0 || i < 2 {", "    return a[i]", "}"), indexI},
	{
		"index case past the end",
		indexing("switch i {", "case 0, 1, 3:", "    return a[i]", "}"),
		indexI,
	},
	{
		"string length that may be zero",
		stringing("if len(s) > 1 || len(s) == 0 {", "    return s[0]", "}"),
		spanned{"index 0 may be out of range; check len(s) > 0 first, or use v, ok := ...[0]", "0"},
	},
}

// TestRejectsOrWithoutFacts checks each of noOrFactCases is one error,
// covering the right source.
func TestRejectsOrWithoutFacts(t *testing.T) {
	for _, tt := range noOrFactCases {
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
