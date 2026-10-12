package parser_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
)

// parenSpanCases are expressions, each written as the value of x, with the
// text the whole expression must span: an expression that starts or ends
// with a parenthesised operand includes the parentheses.
var parenSpanCases = []struct {
	expr string
	want string
}{
	{"(a) == (b)", "(a) == (b)"},
	{"(a) == b", "(a) == b"},
	{"a == (b)", "a == (b)"},
	{"((a)) + ((b))", "((a)) + ((b))"},
	{"(a + b) * c", "(a + b) * c"},
	{"a * (b + c)", "a * (b + c)"},
	{"(a) + b * (c)", "(a) + b * (c)"},
	{"-(a)", "-(a)"},
	{"!(a)", "!(a)"},
	{"(a)[0]", "(a)[0]"},
	{"(a)[0] + (b)", "(a)[0] + (b)"},
	{"(a) && (b) || (c)", "(a) && (b) || (c)"},
	{"(a)", "a"},
	{"((a + b))", "a + b"},
	{"f((a)) + (b)", "f((a)) + (b)"},
}

// TestParenthesesAreInTheSpan checks the span of an expression whose first
// or last operand is parenthesised covers the parentheses, while a
// parenthesised expression on its own still spans only what is inside them.
func TestParenthesesAreInTheSpan(t *testing.T) {
	for _, tt := range parenSpanCases {
		t.Run(tt.expr, func(t *testing.T) {
			src := "package main\nfunc main() {\n    x := " + tt.expr + "\n}\n"
			file, diags := parse(t, src)
			if len(diags) > 0 {
				t.Fatalf("rejected: %v", diagMessages(diags))
			}
			fn, ok := file.Decls[0].(*ast.Func)
			if !ok {
				t.Fatalf("declaration is %T, want *ast.Func", file.Decls[0])
			}
			v, ok := fn.Body[0].(*ast.Var)
			if !ok {
				t.Fatalf("statement is %T, want *ast.Var", fn.Body[0])
			}
			span := v.Value.Spanned()
			if got := src[span.Start:span.End]; got != tt.want {
				t.Errorf("span covers %q, want %q", got, tt.want)
			}
		})
	}
}
