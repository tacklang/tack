package typeck_test

import (
	"math"
	"testing"

	"github.com/tacklang/tack/types"
)

// invalidNegationCases are negations the checker must reject, with the
// diagnostics each must produce.
var invalidNegationCases = []struct {
	name string
	src  string
	want []spanned
}{
	{"bool", inMain("if -true {\n    }"), []spanned{{"operator - is not defined on bool", "true"}}},
	{
		"string",
		inMain("print(-\"s\")"),
		[]spanned{{"operator - is not defined on string", "\"s\""}},
	},
	{
		"comparison",
		inMain("if -(1 < 2) {\n    }"),
		[]spanned{{"operator - is not defined on bool", "1 < 2"}},
	},
	{
		"not of a negation",
		inMain("if !-5 {\n    }"),
		[]spanned{{"operator ! is not defined on int64", "-5"}},
	},
	{
		// The negation has no type once its operand is reported, so the
		// argument it is passed as is not reported again.
		"negation of a not",
		mainLines("b := true", "exit(-!b)"),
		[]spanned{{"operator - is not defined on bool", "!b"}},
	},
	{
		"error inside",
		inMain("exit(-(1 + \"s\"))"),
		[]spanned{{"operator + is not defined on string", "\"s\""}},
	},
}

// TestNegation checks - takes an int and yields one.
func TestNegation(t *testing.T) {
	ok := []string{
		mainLines("x := 5", "exit(int32(-x))"),
		mainLines("x := 5", "exit(int32(0 - -x + -3))"),
		"package main\n\nfunc f() int64 {\n    return 2\n}\n\nfunc main() {\n    code, fits := int32(-(f() % 100))\n    if fits { exit(code) }\n}\n",
		mainLines("x := -9223372036854775808", "if x < 0 {", "}"),
	}
	for _, src := range ok {
		if _, diags := checkTree(t, src); len(diags) > 0 {
			t.Errorf("%q: got %v, want no diagnostics", src, diags)
		}
	}

	for _, tt := range invalidNegationCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != len(tt.want) {
				t.Fatalf("got %v, want %v", diags, tt.want)
			}
			for i, d := range diags {
				if d.Message != tt.want[i].msg {
					t.Errorf("message %d = %q, want %q", i, d.Message, tt.want[i].msg)
				}
				if got := tt.src[d.Span.Start:d.Span.End]; got != tt.want[i].at {
					t.Errorf("diagnostic %d covers %q, want %q", i, got, tt.want[i].at)
				}
			}
		})
	}
}

// TestFoldsNegation checks the value computed for package-level negations,
// and that negating the smallest int is the overflow it is.
func TestFoldsNegation(t *testing.T) {
	tests := []struct {
		want any
		decl string
	}{
		{int64(-5), "var x = -5"},
		{int64(5), "var x = -(-5)"},
		{int64(5), "var x = - -5"},
		{int64(-3), "var x = -(1 + 2)"},
		{int64(math.MinInt64), "var x = -9223372036854775808"},
		{int64(math.MinInt64), "var x = -9223372036854775807 - 1"},
		{int64(math.MaxInt64), "var x = -(-9223372036854775807)"},
	}
	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			file, diags := checkTree(t, withGlobal(tt.decl))
			if len(diags) > 0 {
				t.Fatalf("got %v, want no diagnostics", diags)
			}
			if v := globalX(t, file); v.Type != types.Int64 || machine(v.Const) != tt.want {
				t.Errorf("got %v %#v, want int64 %#v", v.Type, v.Const, tt.want)
			}
		})
	}

	for _, decl := range []string{"var x = -(-9223372036854775808)", "var x = - -9223372036854775808"} {
		_, diags := checkTree(t, withGlobal(decl))
		if len(diags) != 1 || diags[0].Message != "constant 9223372036854775808 overflows int64" {
			t.Errorf("%s: got %v, want an overflow", decl, diags)
		}
	}
}
