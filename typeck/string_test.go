package typeck_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// stringing wraps statements in a function that takes a string s, an index
// i, a uint8 index u, a uint64 index w and an int8 index b, with a mutable
// string t, a mutable global string g and an immutable one k, and returns a
// uint8, ending in return 0.
func stringing(lines ...string) string {
	body := ""
	for _, l := range lines {
		body += "    " + l + "\n"
	}
	return "package main\n\nmut g = \"g\"\nvar k = \"k\"\n\n" +
		"func f(s string, i int64, u uint8, w uint64, b int8) uint8 {\n" +
		"    mut t := \"tack\"\n" + body +
		"    print(t)\n    print(g)\n    print(k)\n    return 0\n}\n\n" +
		"func name() string {\n    return \"name\"\n}\n\n" +
		"func main() {\n    exit(f(name(), 1, 2, 3, 4))\n}\n"
}

// provenStringIndexCases are indexes into strings the checker can prove in
// range, each by one of the rules.
var provenStringIndexCases = []struct{ name, src string }{
	{"range over len", stringing("for j := range len(s) {", "    return s[j]", "}")},
	{
		"an expression",
		stringing("if 0 <= i && i < len(s) - 1 {", "    return s[i + 1]", "}"),
	},
	{
		"range over len of a mutable string",
		stringing("for j := range len(t) {", "    return t[j]", "}"),
	},
	{
		"nested ranges",
		stringing(
			"for x := range len(s) {",
			"    for y := range len(t) {",
			"        return s[x] + t[y]",
			"    }",
			"}",
		),
	},
	{"guard", stringing("if 0 <= i && i < len(s) {", "    return s[i]", "}")},
	{"guard reversed", stringing("if len(s) > i && i >= 0 {", "    return s[i]", "}")},
	{"guard negated", stringing("if !(i >= len(s)) && !(i < 0) {", "    return s[i]", "}")},
	{"early return", stringing("if i < 0 || i >= len(s) {", "    return 0", "}", "return s[i]")},
	{
		"else branch",
		stringing("if i < 0 || len(s) <= i {", "    return 0", "} else {", "    return s[i]", "}"),
	},
	{"right of and", stringing("if 0 <= i && i < len(s) && s[i] == 'a' {", "    return 1", "}")},
	{"right of or", stringing("if i < 0 || i >= len(s) || s[i] == 'a' {", "    return 1", "}")},
	{"unsigned needs only an upper bound", stringing("if u < len(s) {", "    return s[u]", "}")},
	{"int8 index", stringing("if b >= 0 && b < len(s) {", "    return s[b]", "}")},
	{"length above zero", stringing("if len(s) > 0 {", "    return s[0]", "}")},
	{"length at least one", stringing("if len(s) >= 1 {", "    return s[0]", "}")},
	{"length not zero", stringing("if len(s) != 0 {", "    return s[0]", "}")},
	{"zero below length", stringing("if 0 < len(s) {", "    return s[0]", "}")},
	{"length above a larger constant", stringing("if len(s) > 3 {", "    return s[3] + s[2]", "}")},
	{"length equal", stringing("if len(s) == 2 {", "    return s[1]", "}")},
	{"empty string ruled out", stringing("if len(s) == 0 {", "    return 0", "}", "return s[0]")},
	{
		"constant bound below the length",
		stringing("if len(s) > 5 && i >= 0 && i < 5 {", "    return s[i]", "}"),
	},
	{"mutable string guarded", stringing("if len(t) > 0 {", "    return t[0]", "}")},
	{
		"mutable string reassigned before",
		stringing("t = \"\"", "if len(t) > 0 {", "    return t[0]", "}"),
	},
	{"immutable global", stringing("if len(k) > 0 {", "    return k[0]", "}")},
	{"literal constant", stringing("return \"tack\"[3]")},
	{"literal range", stringing("for j := range 4 {", "    return \"tack\"[j]", "}")},
	{
		"literal range over its len",
		stringing("for j := range len(\"tack\") {", "    return \"tack\"[j]", "}"),
	},
	{"checked variable index", stringing("v, ok := s[i]", "if ok {", "    return v", "}")},
	{"checked constant index", stringing("v, ok := s[0]", "if ok {", "    return v", "}")},
	{
		"checked assigned",
		stringing(
			"mut v := 'a'",
			"mut ok := false",
			"v, ok = t[w]",
			"if ok {",
			"    return v",
			"}",
		),
	},
	{"checked call", stringing("v, ok := name()[i]", "if ok {", "    return v", "}")},
	{"checked literal", stringing("v, ok := \"tack\"[i]", "if ok {", "    return v", "}")},
	{"checked mutable global", stringing("v, ok := g[0]", "if ok {", "    return v", "}")},
	{
		"checked element of an array of strings",
		stringing(
			"names := [2]string{s, t}",
			"v, ok := names[1][i]",
			"if ok {",
			"    return v",
			"}",
		),
	},
	{
		"byte is a uint8",
		stringing(
			"if 0 <= i && i < len(s) {",
			"    var x int64 = s[i]",
			"    code, fits := int32(x)",
			"    if fits { exit(code) }",
			"}",
		),
	},
	{
		"checked with a proven index",
		stringing(
			"for j := range len(s) {",
			"    v, ok := s[j]",
			"    if ok {",
			"        return v",
			"    }",
			"}",
		),
	},
	{
		"checked with a guarded constant",
		stringing(
			"if len(s) > 0 {",
			"    v, ok := s[0]",
			"    if ok {",
			"        return v",
			"    }",
			"}",
		),
	},
	{
		"checked constant into a literal",
		stringing("v, ok := \"tack\"[1]", "if ok {", "    return v", "}"),
	},
}

// TestAcceptsProvenStringIndexes checks each of provenStringIndexCases has
// no diagnostics.
func TestAcceptsProvenStringIndexes(t *testing.T) {
	for _, tt := range provenStringIndexCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// unprovenStringIndexCases are string indexes that may be out of range, or
// are, and assignments to a string's bytes, with the diagnostic each must
// produce.
var unprovenStringIndexCases = []struct {
	name string
	src  string
	want spanned
}{
	{
		"unguarded",
		stringing("return s[i]"),
		spanned{
			"index i may be out of range; check 0 <= i && i < len(s) first, or use v, ok := ...[i]",
			"i",
		},
	},
	{
		"no lower bound",
		stringing("if i < len(s) {", "    return s[i]", "}"),
		spanned{
			"index i may be out of range; check 0 <= i && i < len(s) first, or use v, ok := ...[i]",
			"i",
		},
	},
	{
		"no upper bound",
		stringing("if i >= 0 {", "    return s[i]", "}"),
		spanned{
			"index i may be out of range; check 0 <= i && i < len(s) first, or use v, ok := ...[i]",
			"i",
		},
	},
	{
		"off by one",
		stringing("if i >= 0 && i <= len(s) {", "    return s[i]", "}"),
		spanned{
			"index i may be out of range; check 0 <= i && i < len(s) first, or use v, ok := ...[i]",
			"i",
		},
	},
	{
		"unsigned beyond the string",
		stringing("return s[u]"),
		spanned{
			"index u may be out of range; check u < len(s) first, or use v, ok := ...[u]",
			"u",
		},
	},
	{
		"guard on another string",
		stringing("if 0 <= i && i < len(s) {", "    return t[i]", "}"),
		spanned{
			"index i may be out of range; check 0 <= i && i < len(t) first, or use v, ok := ...[i]",
			"i",
		},
	},
	{
		"an expression that may reach the length",
		stringing("if 0 <= i && i < len(s) {", "    return s[i + 1]", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "i + 1"},
	},
	{
		"string reassigned inside the guard",
		stringing("if len(t) > 0 {", "    t = \"\"", "    return t[0]", "}"),
		spanned{"index 0 may be out of range; check len(t) > 0 first, or use v, ok := ...[0]", "0"},
	},
	{
		"string reassigned after the early return",
		stringing("if len(t) == 0 {", "    return 0", "}", "t = \"\"", "return t[0]"),
		spanned{"index 0 may be out of range; check len(t) > 0 first, or use v, ok := ...[0]", "0"},
	},
	{
		"string reassigned inside the loop",
		stringing("for j := range len(t) {", "    t = \"x\"", "    return t[j]", "}"),
		spanned{
			"index j may be out of range; check 0 <= j && j < len(t) first, or use v, ok := ...[j]",
			"j",
		},
	},
	{
		"index reassigned inside the guard",
		stringing("mut j := i", "if 0 <= j && j < len(s) {", "    j++", "    return s[j]", "}"),
		spanned{
			"index j may be out of range; check 0 <= j && j < len(s) first, or use v, ok := ...[j]",
			"j",
		},
	},
	{
		"length too small for the constant",
		stringing("if len(s) > 0 {", "    return s[1]", "}"),
		spanned{"index 1 may be out of range; check len(s) > 1 first, or use v, ok := ...[1]", "1"},
	},
	{
		"mutable global",
		stringing("if len(g) > 0 {", "    return g[0]", "}"),
		spanned{"index 0 may be out of range; use v, ok := ...[0]", "0"},
	},
	{
		"variable into a mutable global",
		stringing("if 0 <= i && i < len(g) {", "    return g[i]", "}"),
		spanned{"index i may be out of range; use v, ok := ...[i]", "i"},
	},
	{
		"uint64 index",
		stringing("return s[w]"),
		spanned{
			"index w may be out of range; check w < len(s) first, or use v, ok := ...[w]",
			"w",
		},
	},
	{
		"constant into a call",
		stringing("return name()[0]"),
		spanned{"index 0 may be out of range; use v, ok := ...[0]", "0"},
	},
	{
		"variable into a call",
		stringing("if 0 <= i && i < 3 {", "    return name()[i]", "}"),
		spanned{"index i may be out of range; use v, ok := ...[i]", "i"},
	},
	{
		"variable into a literal",
		stringing("return \"tack\"[i]"),
		spanned{
			"index i may be out of range; check 0 <= i && i < 4 first, or use v, ok := ...[i]",
			"i",
		},
	},
	{
		"literal too short",
		stringing("return \"tack\"[4]"),
		spanned{"index 4 is out of range for a string of 4 bytes", "4"},
	},
	{
		"literal one byte",
		stringing("return \"x\"[1]"),
		spanned{"index 1 is out of range for a string of 1 byte", "1"},
	},
	{
		"empty literal",
		stringing("return \"\"[0]"),
		spanned{"index 0 is out of range for a string of 0 bytes", "0"},
	},
	{"negative", stringing("return s[-1]"), spanned{"index -1 is out of range for a string", "-1"}},
	{
		"negative literal",
		stringing("return \"tack\"[-1]"),
		spanned{"index -1 is out of range for a string of 4 bytes", "-1"},
	},
	{
		"checked constant out of a literal",
		stringing("v, ok := \"tack\"[9]", "if ok {", "    return v", "}"),
		spanned{"index 9 is out of range for a string of 4 bytes", "9"},
	},
	{
		"checked negative",
		stringing("v, ok := s[-1]", "if ok {", "    return v", "}"),
		spanned{"index -1 is out of range for a string", "-1"},
	},
	{
		"assigned",
		stringing("t[0] = 65"),
		spanned{"cannot assign to an element of a string; strings are immutable", "t[0]"},
	},
	{
		"assigned in a guard",
		stringing("if len(t) > 0 {", "    t[0] = 65", "}"),
		spanned{"cannot assign to an element of a string; strings are immutable", "t[0]"},
	},
	{
		"compound assigned",
		stringing("t[i] += 1"),
		spanned{"cannot assign to an element of a string; strings are immutable", "t[i]"},
	},
	{
		"incremented",
		stringing("t[0]++"),
		spanned{"cannot assign to an element of a string; strings are immutable", "t[0]"},
	},
	{
		"parameter assigned",
		stringing("s[0] = 65"),
		spanned{"cannot assign to an element of a string; strings are immutable", "s[0]"},
	},
	{
		"assigned with several",
		stringing("mut ok := false", "t[0], ok = s[i]", "if ok {", "}"),
		spanned{"cannot assign to an element of a string; strings are immutable", "t[0]"},
	},
	{
		"element of an array of strings assigned",
		stringing("mut names [2]string", "names[0][1] = 65", "print(names[1])"),
		spanned{"cannot assign to an element of a string; strings are immutable", "names[0][1]"},
	},
	{
		"string index",
		stringing("return s[\"a\"]"),
		spanned{"index must be an integer, got string", "\"a\""},
	},
	{
		"bool index",
		stringing("return s[true]"),
		spanned{"index must be an integer, got bool", "true"},
	},
	{
		"byte is not an int8",
		stringing("if len(s) > 0 {", "    var x int8 = s[0]", "    exit(x)", "}"),
		spanned{"cannot use uint8 as int8 in declaration of x", "s[0]"},
	},
}

// TestRejectsUnprovenStringIndexes checks each string index is one error,
// covering the index, the whole element where the ok is the problem, or the
// whole target where a byte is assigned.
func TestRejectsUnprovenStringIndexes(t *testing.T) {
	for _, tt := range unprovenStringIndexCases {
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

// stringComparisonCases are comparisons of two strings, each of which must
// check cleanly and give a bool.
var stringComparisonCases = []string{
	"s == t",
	"s != \"\"",
	"\"a\" < \"b\"",
	"s <= k",
	"name() > g",
	"t >= s",
	"(s < t) == (t > s)",
	"s == s",
	"\"\" < \"\"",
}

// TestAcceptsStringComparisons checks every comparison operator applies to
// two strings, of any origin: a parameter, a literal, a call, and mutable
// and immutable variables, local and package-level.
func TestAcceptsStringComparisons(t *testing.T) {
	for _, cond := range stringComparisonCases {
		t.Run(cond, func(t *testing.T) {
			if _, diags := checkTree(
				t,
				stringing("if "+cond+" {", "    return 1", "}"),
			); len(
				diags,
			) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// A comparison of two string literals is left to run time: the checker
// folds comparisons of untyped integer constants only, and a string
// literal is a typed string.
func TestStringLiteralComparisonIsNotFolded(t *testing.T) {
	file, diags := checkTree(t, mainLines("if \"a\" < \"b\" {", "    exit(1)", "}"))
	if len(diags) > 0 {
		t.Fatalf("got %v, want no diagnostics", diags)
	}
	cond := file.Decls[0].(*ast.Func).Body[0].(*ast.If).Cond.(*ast.Binary)
	if cond.Const != nil {
		t.Errorf("Const = %#v, want nil", cond.Const)
	}
	if cond.Type != types.Bool {
		t.Errorf("type = %v, want bool", cond.Type)
	}
}

// Comparing a string with a literal says nothing about its length, so it
// proves no index into it.
func TestStringComparisonProvesNoIndex(t *testing.T) {
	_, diags := checkTree(t, stringing("if s == \"abc\" {", "    return s[0]", "}"))
	want := "index 0 may be out of range; check len(s) > 0 first, or use v, ok := ...[0]"
	if len(diags) != 1 || diags[0].Message != want {
		t.Errorf("got %v, want only %q", diags, want)
	}
}
