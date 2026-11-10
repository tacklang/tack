package parser_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestWrappingOperatorGrouping pins where the wrapping operators bind: +%
// and -% as + and - do, *% as * does, every one left-associative, and the
// prefix -% as tight as the prefix -, never folded into a literal.
func TestWrappingOperatorGrouping(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"a +% b", "(a +% b)"},
		{"a +% b *% c", "(a +% (b *% c))"},
		{"a *% b +% c", "((a *% b) +% c)"},
		{"a -% b -% c", "((a -% b) -% c)"},
		{"a + b -% c", "((a + b) -% c)"},
		{"a *% b * c", "((a *% b) * c)"},
		{"a +% b == c", "((a +% b) == c)"},
		{"a +% b << c", "(a +% (b << c))"},
		{"-%a *% b", "((-%a) *% b)"},
		{"-%-%a", "(-%(-%a))"},
		{"-%5", "(-%5)"},
		{"- -%a", "(-(-%a))"},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			if got := shape(t, declared(t, tt.expr)); got != tt.want {
				t.Errorf("%s grouped as %s, want %s", tt.expr, got, tt.want)
			}
		})
	}
}

// TestCompoundWrappingAssignments checks each wrapping compound assignment
// parses to an assignment applying its operator.
func TestCompoundWrappingAssignments(t *testing.T) {
	for op, want := range map[string]ast.BinaryOp{
		"+%=": ast.AddWrap, "-%=": ast.SubWrap, "*%=": ast.MulWrap,
	} {
		src := "package main\n\nfunc main() {\n    x " + op + " y\n}\n"
		file, diags := parse(t, src)
		if len(diags) > 0 {
			t.Fatalf("%s: parse reported %+v", op, diags)
		}
		a, ok := file.Decls[0].(*ast.Func).Body[0].(*ast.Assign)
		if !ok || a.Op != want {
			t.Errorf("%s parsed as %#v, want an assignment applying %v",
				op, file.Decls[0].(*ast.Func).Body[0], want)
		}
	}
}
