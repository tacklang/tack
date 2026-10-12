package parser_test

import (
	"math"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestNegativeLiterals checks a - directly before an integer literal makes
// one negative literal, so the smallest int can be written, with a span
// covering the sign and the digits.
func TestNegativeLiterals(t *testing.T) {
	tests := []struct {
		expr string
		want int64
	}{
		{"-5", -5},
		{"-0", 0},
		{"- 5", -5},
		{"-9223372036854775807", -math.MaxInt64},
		{"-9223372036854775808", math.MinInt64},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			src := "package main\n\nfunc main() {\n    x := " + tt.expr + "\n}\n"
			file, diags := parse(t, src)
			if len(diags) > 0 {
				t.Fatalf("parse reported %+v", diags)
			}
			lit, ok := file.Decls[0].(*ast.Func).Body[0].(*ast.Var).Value.(*ast.IntLit)
			if !ok {
				t.Fatalf("value is not an *ast.IntLit")
			}
			if lit.Value.Int64() != tt.want {
				t.Errorf("value = %s, want %d", lit.Value, tt.want)
			}
			if got := src[lit.Span.Start:lit.Span.End]; got != tt.expr {
				t.Errorf("span covers %q, want %q", got, tt.expr)
			}
		})
	}
}

// TestNegationGrouping pins - as a prefix operator binding tighter than any
// binary operator, as ! does.
func TestNegationGrouping(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"-x", "(-x)"},
		{"- -x", "(-(-x))"},
		{"- -5", "(--5)"},
		{"-(5)", "(-5)"},
		{"-(1 + 2)", "(-(1 + 2))"},
		{"-x * y", "((-x) * y)"},
		{"a - -1", "(a - -1)"},
		{"a - -b", "(a - (-b))"},
		{"-2 * 3", "(-2 * 3)"},
		{"-f()", "(-f())"},
		{"-x < y", "((-x) < y)"},
		{"!-x", "(!(-x))"},
		{"-true", "(-true)"},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			if got := shape(t, declared(t, tt.expr)); got != tt.want {
				t.Errorf("%s grouped as %s, want %s", tt.expr, got, tt.want)
			}
		})
	}
}

// TestRejectsMalformedNegation checks the cases around - that are not
// accepted: a literal out of range even with its sign, -- as a prefix, and
// a unary +.
func TestRejectsMalformedNegation(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"-9223372036854775809", "integer literal -9223372036854775809 is out of range"},
		{"--5", "expected 'value' found '--'"},
		{"--x", "expected 'value' found '--'"},
		{"+5", "expected 'value' found '+'"},
		{"-", "expected 'value' found '}'"},
		{"x -", "expected 'value' found '}'"},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			_, diags := parse(t, "package main\n\nfunc main() {\n    x := "+tt.expr+"\n}\n")
			if len(diags) == 0 {
				t.Fatalf("%q parsed without error", tt.expr)
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
		})
	}
}
