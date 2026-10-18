package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestParsesEnsures checks that ensures after a function's results, or
// after its parameters when it has none, gives the function its contract,
// with the keyword's span, the condition parsed as an if's is, and the
// body after it; a method's and an extern func's included.
func TestParsesEnsures(t *testing.T) {
	tests := []struct {
		src, clause string
	}{
		{"func f(d int64) ensures d != 0 {\n}\n", "d != 0"},
		{"func f(d int64) int64 ensures d != 0 && d < 5 {\n    return d\n}\n", "d != 0 && d < 5"},
		{
			"func f(err error) (int64, error) ensures err.code == 0 {\n    return 0, err\n}\n",
			"err.code == 0",
		},
		{"func (s Stack) full() ensures len(s.items) > 0 {\n}\n", "len(s.items) > 0"},
		{"func f[T Any](l []T) ensures len(l) > 0 {\n}\n", "len(l) > 0"},
		{"func f(m map[int64]int64, k int64) ensures has(m, k) {\n}\n", "has(m, k)"},
		{"func f(p P) ensures p == (P{x: 1}) {\n}\n", "p == (P{x: 1})"},
		{"extern func f(d int64) ensures d != 0\n", "d != 0"},
	}
	for _, tt := range tests {
		src := "package main\n\n" + tt.src
		file, diags := parse(t, src)
		if len(diags) > 0 {
			t.Errorf("%q: parse reported %+v", tt.src, diags)
			continue
		}
		fn := file.Decls[0].(*ast.Func)
		if fn.Ensures == nil {
			t.Errorf("%q: no ensures clause", tt.src)
			continue
		}
		if got := src[fn.EnsuresSpan.Start:fn.EnsuresSpan.End]; got != "ensures" {
			t.Errorf("%q: ensures keyword spans %q", tt.src, got)
		}
		span := fn.Ensures.Spanned()
		if got := src[span.Start:span.End]; got != tt.clause {
			t.Errorf("%q: clause spans %q, want %q", tt.src, got, tt.clause)
		}
		if got := src[fn.Span.Start:fn.Span.End]; got != strings.TrimSuffix(tt.src, "\n") {
			t.Errorf("%q: function spans %q", tt.src, got)
		}
	}
}

// TestParsesNoEnsures checks that a function without the keyword has no
// contract, and that a name holding the word is no keyword.
func TestParsesNoEnsures(t *testing.T) {
	src := "package main\n\nfunc f(ensured int64) int64 {\n    return ensured\n}\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	if fn := file.Decls[0].(*ast.Func); fn.Ensures != nil || fn.EnsuresSpan != (ast.Span{}) {
		t.Errorf("got a contract %+v at %+v", fn.Ensures, fn.EnsuresSpan)
	}
}

// TestRejectsBadEnsures checks the errors of a clause written wrong: no
// condition, an unfinished one, one before the results, a struct literal
// without parentheses, and ensures used as a name.
func TestRejectsBadEnsures(t *testing.T) {
	tests := []struct {
		src, msg string
	}{
		{"func f(d int64) ensures {\n}\n", "expected 'value' found '{'"},
		{"func f(d int64) ensures d != {\n}\n", "expected 'value' found '{'"},
		{"func f() ensures d != 0 int64 {\n    return 1\n}\n", "missing function body"},
		{
			"func f(p P) ensures p == P{x: 1} {\n}\n",
			"a struct literal in an ensures header must be in parentheses",
		},
		{"func f(ensures int64) {\n}\n", "expected 'parameter name' found 'ensures'"},
	}
	for _, tt := range tests {
		src := "package main\n\n" + tt.src
		_, diags := parse(t, src)
		if len(diags) == 0 || diags[0].Message != tt.msg {
			t.Errorf("%q: got %+v, want first error %q", tt.src, diags, tt.msg)
		}
	}
}

// ensuresSeeds are sources with contracts, whole, broken and in places a
// contract cannot go, for FuzzParseEnsures.
var ensuresSeeds = []string{
	"package main\n\nfunc f(d int64) ensures d != 0 {\n    if d == 0 {\n        exit(1)\n    }\n}\n",
	"package main\n\nfunc f(i int64, l []int64) int64 ensures 0 <= i && i < len(l) {\n    return i\n}\n",
	"package main\n\nfunc (s S) f(m map[int64]int64, k int64) ensures has(m, k) && !(s.n == 0) {\n}\n",
	"package main\n\nextern(\"c_f\") func f(d int64) ensures d != 0\n",
	"package main\n\nfunc f(d int64) ensures\n",
	"package main\n\nfunc f(d int64) ensures d != 0 ensures d > 0 {\n}\n",
	"package main\n\nfunc f(p P) ensures p == P{x: 1} {\n}\n",
	"package main\n\ntype I interface {\n    f(d int64) ensures d != 0\n}\n",
	"package main\n\nfunc f(g func(int64) ensures true) {\n}\n",
	"package main\n\nfunc main() {\n    ensures := 1\n}\n",
}

// FuzzParseEnsures parses arbitrary source, seeded with contracts, and
// checks the parser never panics, gives a tree exactly when it reports
// nothing, and reports each problem at a span inside the source.
func FuzzParseEnsures(f *testing.F) {
	for _, src := range ensuresSeeds {
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		checkParse(t, src)
	})
}
