package parser_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// shape renders an expression fully parenthesised, so a test can pin how it
// was grouped: every binary and unary expression gets one pair.
func shape(t *testing.T, e ast.Expr) string {
	t.Helper()

	switch x := e.(type) {
	case *ast.Binary:
		return "(" + shape(t, x.X) + " " + x.Op.String() + " " + shape(t, x.Y) + ")"
	case *ast.Unary:
		return "(" + x.Op.String() + shape(t, x.X) + ")"
	case *ast.Ident:
		return x.Name
	case *ast.IntLit:
		return strconv.FormatInt(x.Value, 10)
	case *ast.BoolLit:
		return strconv.FormatBool(x.Value)
	case *ast.Call:
		return shape(t, x.Fn) + "()"
	default:
		t.Fatalf("unexpected expression %T", e)
		return ""
	}
}

// declared parses x := expr inside main and returns the declared value.
func declared(t *testing.T, expr string) ast.Expr {
	t.Helper()

	body := mainBody(t, "    x := "+expr)
	v, ok := body[0].(*ast.Var)
	if !ok {
		t.Fatalf("statement is %T, want *ast.Var", body[0])
	}
	return v.Value
}

// TestLogicalOperatorGrouping pins Go's precedence: ! binds tighter than any
// binary operator, comparisons tighter than &&, and && tighter than ||, with
// every binary operator left-associative.
func TestLogicalOperatorGrouping(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"a && b", "(a && b)"},
		{"a || b", "(a || b)"},
		{"!a", "(!a)"},
		{"!!a", "(!(!a))"},
		{"a || b && c", "(a || (b && c))"},
		{"a && b || c", "((a && b) || c)"},
		{"a && b && c", "((a && b) && c)"},
		{"a || b || c", "((a || b) || c)"},
		{"1 < 2 && 3 < 4", "((1 < 2) && (3 < 4))"},
		{"a == b || c != d", "((a == b) || (c != d))"},
		{"1 + 2 < 3 && a", "(((1 + 2) < 3) && a)"},
		{"!a == b", "((!a) == b)"},
		{"!a && b", "((!a) && b)"},
		{"!(a && b)", "(!(a && b))"},
		{"(a || b) && c", "((a || b) && c)"},
		{"!f() || true", "((!f()) || true)"},
		{"!(1 < 2)", "(!(1 < 2))"},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			if got := shape(t, declared(t, tt.expr)); got != tt.want {
				t.Errorf("%s grouped as %s, want %s", tt.expr, got, tt.want)
			}
		})
	}
}

// A unary expression's span runs from the ! through the end of its operand.
func TestUnarySpan(t *testing.T) {
	src := "package main\n\nfunc main() {\n    x := !!flag\n}\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}

	outer := file.Decls[0].(*ast.Func).Body[0].(*ast.Var).Value.(*ast.Unary)
	if got := src[outer.Span.Start:outer.Span.End]; got != "!!flag" {
		t.Errorf("outer span covers %q, want %q", got, "!!flag")
	}
	inner := outer.X.(*ast.Unary)
	if got := src[inner.Span.Start:inner.Span.End]; got != "!flag" {
		t.Errorf("inner span covers %q, want %q", got, "!flag")
	}
}

// Deeply nested negation parses without trouble.
func TestDeeplyNestedNot(t *testing.T) {
	expr := strings.Repeat("!", 10000) + "a"
	e := declared(t, expr)
	for i := range 10000 {
		u, ok := e.(*ast.Unary)
		if !ok {
			t.Fatalf("level %d is %T, want *ast.Unary", i, e)
		}
		e = u.X
	}
	if id, ok := e.(*ast.Ident); !ok || id.Name != "a" {
		t.Errorf("innermost operand is %#v, want a", e)
	}
}

// TestRejectsMalformedLogicalExpressions checks a missing operand is a parse
// error, and that the other unary operators the lexer knows are not
// accepted.
func TestRejectsMalformedLogicalExpressions(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"a &&", "expected 'value' found '}'"},
		{"|| a", "expected 'value' found '||'"},
		{"a || || b", "expected 'value' found '||'"},
		{"!", "expected 'value' found '}'"},
		{"a !", "expected ';' found '!'"},
		{"^a", "expected 'value' found '^'"},
		{"a & b", "expected ';' found '&'"},
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
