package parser_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestBitwiseOperatorGrouping pins Go's precedence for the bitwise and
// shift operators: << >> & and &^ bind as * does, | and ^ as + does, every
// one left-associative, and the prefix ^ tighter than any of them.
func TestBitwiseOperatorGrouping(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"a & b", "(a & b)"},
		{"a &^ b", "(a &^ b)"},
		{"a << b >> c", "((a << b) >> c)"},
		{"a + b << c", "(a + (b << c))"},
		{"a * b & c", "((a * b) & c)"},
		{"a & b * c", "((a & b) * c)"},
		{"a | b & c", "(a | (b & c))"},
		{"a ^ b | c", "((a ^ b) | c)"},
		{"a | b + c", "((a | b) + c)"},
		{"a | b == c", "((a | b) == c)"},
		{"a &^ b << c", "((a &^ b) << c)"},
		{"^a & b", "((^a) & b)"},
		{"^^a", "(^(^a))"},
		{"-^a", "(-(^a))"},
		{"a == b && c & d != 0", "((a == b) && ((c & d) != 0))"},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			if got := shape(t, declared(t, tt.expr)); got != tt.want {
				t.Errorf("%s grouped as %s, want %s", tt.expr, got, tt.want)
			}
		})
	}
}

// TestCompoundBitwiseAssignments checks each bitwise and shift compound
// assignment parses to an assignment applying its operator.
func TestCompoundBitwiseAssignments(t *testing.T) {
	for op, want := range map[string]ast.BinaryOp{
		"&=": ast.BitAnd, "|=": ast.BitOr, "^=": ast.BitXor,
		"&^=": ast.AndNot, "<<=": ast.Shl, ">>=": ast.Shr,
	} {
		src := "package main\n\nfunc main() {\n    x " + op + " y\n}\n"
		file, diags := parse(t, src)
		if len(diags) > 0 {
			t.Fatalf("%s: parse reported %+v", op, diags)
		}
		a, ok := file.Decls[0].(*ast.Func).Body[0].(*ast.Assign)
		if !ok || a.Op != want {
			t.Errorf(
				"%s parsed as %#v, want an assignment applying %v",
				op,
				file.Decls[0].(*ast.Func).Body[0],
				want,
			)
		}
	}
}
