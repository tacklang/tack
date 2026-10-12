package typeck_test

import (
	"testing"
)

// indexing wraps statements in a function that takes an index i, an
// unsigned index u, a small signed index s and a count n, with an array a of
// three elements and a mutable one m, and returns an int64, ending in
// return 0.
func indexing(lines ...string) string {
	body := ""
	for _, l := range lines {
		body += "    " + l + "\n"
	}
	return "package main\n\nmut g int64 = 1\n\n" +
		"func f(i int64, u uint8, s int8, n int64) int64 {\n" +
		"    a := [3]int64{1, 2, 3}\n    mut m [3]int64\n" + body + "    return a[0] + m[0]\n}\n\n" +
		"func main() {\n    exit(f(1, 2, 3, 4) + g)\n}\n"
}

// provenIndexCases are indexes the checker can prove in range, each by one
// of the rules.
var provenIndexCases = []struct{ name, src string }{
	{"range over len", indexing("for k := range len(a) {", "    m[k] = a[k]", "}")},
	{"range over a smaller count", indexing("for k := range 2 {", "    m[k] = a[k]", "}")},
	{"range over an empty count", indexing("for k := range 0 {", "    m[k] = a[k]", "}")},
	{"guard", indexing("if 0 <= i && i < len(a) {", "    return a[i]", "}")},
	{"guard with constants", indexing("if i >= 0 && i < 3 {", "    return a[i]", "}")},
	{"guard by greater and at most", indexing("if i > -1 && i <= 2 {", "    return a[i]", "}")},
	{"equal to a constant", indexing("if i == 1 {", "    return a[i]", "}")},
	{"early return", indexing("if i < 0 || i >= len(a) {", "    return 0", "}", "return a[i]")},
	{
		"else branch",
		indexing("if i < 0 || i > 2 {", "    return 0", "} else {", "    return a[i]", "}"),
	},
	{"unsigned needs only an upper bound", indexing("if u < 3 {", "    return a[u]", "}")},
	{
		"unsigned within the type",
		"package main\n\nfunc main() {\n    mut seen [256]bool\n    var b uint8 = 9\n    seen[b] = true\n    if seen[b] {\n    }\n}\n",
	},
	{
		"signed within the type above",
		"package main\n\nfunc f(s int8) bool {\n    mut t [128]bool\n    if s >= 0 {\n        t[s] = true\n    }\n    return t[0]\n}\n\nfunc main() {\n    if f(1) {\n    }\n}\n",
	},
	{
		"nested",
		indexing(
			"mut grid [3][3]int64",
			"for r := range 3 {",
			"    for c := range 3 {",
			"        grid[r][c] = a[c]",
			"    }",
			"}",
			"return grid[2][2]",
		),
	},
	{
		"guarded write",
		indexing("if 0 <= i && i < 3 {", "    m[i] = 5", "    m[i] += 1", "    m[i]++", "}"),
	},
	{"loop condition", indexing("mut k := i", "for k >= 0 && k < 3 {", "    return a[k]", "}")},
	{"checked read", indexing("v, ok := a[i]", "if ok {", "    return v", "}")},
	{
		"checked read assigned",
		indexing("mut v := 0", "mut ok := false", "v, ok = a[n]", "if ok {", "    return v", "}"),
	},
	{
		"checked read of a call",
		"package main\n\nfunc arr() [2]int64 {\n    return [2]int64{1, 2}\n}\n\nfunc f(i int64) int64 {\n    v, ok := arr()[i]\n    if ok {\n        return v\n    }\n    return 0\n}\n\nfunc main() {\n    exit(f(1))\n}\n",
	},
	{"constant from len", indexing("return a[len(a) - 1]")},
}

// TestAcceptsProvenIndexes checks each of provenIndexCases has no
// diagnostics.
func TestAcceptsProvenIndexes(t *testing.T) {
	for _, tt := range provenIndexCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// unprovenIndexCases are indexes that may be out of range, or are, with the
// diagnostic each must produce.
var unprovenIndexCases = []struct {
	name string
	src  string
	want spanned
}{
	{
		"unguarded read",
		indexing("return a[i]"),
		spanned{
			"index i may be out of range; check 0 <= i && i < 3 first, or use v, ok := ...[i]",
			"i",
		},
	},
	{
		"unguarded write",
		indexing("m[i] = 1"),
		spanned{"index i may be out of range; check 0 <= i && i < 3 first", "i"},
	},
	{
		"unguarded compound",
		indexing("m[i] += 1"),
		spanned{"index i may be out of range; check 0 <= i && i < 3 first", "i"},
	},
	{
		"an expression",
		indexing("if 0 <= i && i < 2 {", "    return a[i + 1]", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "i + 1"},
	},
	{
		"range over a larger count",
		indexing("for k := range 4 {", "    m[k] = 1", "}"),
		spanned{"index k may be out of range; check 0 <= k && k < 3 first", "k"},
	},
	{
		"range over an unknown count",
		indexing("for k := range n {", "    return a[k]", "}"),
		spanned{
			"index k may be out of range; check 0 <= k && k < 3 first, or use v, ok := ...[k]",
			"k",
		},
	},
	{
		"no lower bound",
		indexing("if i < 3 {", "    return a[i]", "}"),
		spanned{
			"index i may be out of range; check 0 <= i && i < 3 first, or use v, ok := ...[i]",
			"i",
		},
	},
	{
		"no upper bound",
		indexing("if i >= 0 {", "    return a[i]", "}"),
		spanned{
			"index i may be out of range; check 0 <= i && i < 3 first, or use v, ok := ...[i]",
			"i",
		},
	},
	{
		"off by one",
		indexing("if i >= 0 && i <= 3 {", "    return a[i]", "}"),
		spanned{
			"index i may be out of range; check 0 <= i && i < 3 first, or use v, ok := ...[i]",
			"i",
		},
	},
	{
		"one side of an or",
		indexing("if i >= 0 || i < 3 {", "    return a[i]", "}"),
		spanned{
			"index i may be out of range; check 0 <= i && i < 3 first, or use v, ok := ...[i]",
			"i",
		},
	},
	{
		"unsigned beyond the array",
		indexing("return a[u]"),
		spanned{
			"index u may be out of range; check 0 <= u && u < 3 first, or use v, ok := ...[u]",
			"u",
		},
	},
	{
		"signed below zero",
		"package main\n\nfunc f(s int8) bool {\n    mut t [128]bool\n    t[s] = true\n    return t[0]\n}\n\nfunc main() {\n    if f(1) {\n    }\n}\n",
		spanned{"index s may be out of range; check 0 <= s && s < 128 first", "s"},
	},
	{
		"assigned inside the guard",
		indexing("mut k := i", "if 0 <= k && k < 3 {", "    k++", "    return a[k]", "}"),
		spanned{
			"index k may be out of range; check 0 <= k && k < 3 first, or use v, ok := ...[k]",
			"k",
		},
	},
	{
		"mutable global",
		indexing("if 0 <= g && g < 3 {", "    return a[g]", "}"),
		spanned{
			"index g may be out of range; check 0 <= g && g < 3 first, or use v, ok := ...[g]",
			"g",
		},
	},
	{
		"checked with a proven index",
		indexing(
			"for k := range 3 {",
			"    v, ok := a[k]",
			"    if ok {",
			"        return v",
			"    }",
			"}",
		),
		spanned{"the index cannot be out of range here; drop the ok", "a[k]"},
	},
	{
		"checked with a constant",
		indexing("v, ok := a[1]", "if ok {", "    return v", "}"),
		spanned{"the index cannot be out of range here; drop the ok", "a[1]"},
	},
	{
		"checked with a constant out of range",
		indexing("v, ok := a[7]", "if ok {", "    return v", "}"),
		spanned{"index 7 is out of range for [3]int64", "7"},
	},
	{
		"checked bound to three names",
		indexing("v, ok, z := a[i]", "if ok && z {", "    return v", "}"),
		spanned{"assignment mismatch: 3 variables but an index gives 2 values", "v, ok, z := a[i]"},
	},
}

// TestRejectsUnprovenIndexes checks each index is one error, covering the
// index, or the whole element where the ok is the problem.
func TestRejectsUnprovenIndexes(t *testing.T) {
	for _, tt := range unprovenIndexCases {
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
