package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestParsesCompoundAssignment checks each compound assignment records its
// operator, target and value, and a plain = records none.
func TestParsesCompoundAssignment(t *testing.T) {
	tests := []struct {
		src   string
		value string
		op    ast.BinaryOp
	}{
		{"x = 1", "1", ast.InvalidOp},
		{"x += 1", "1", ast.Add},
		{"x -= y * 2", "(y * 2)", ast.Sub},
		{"x *= f()", "f()", ast.Mul},
		{"x /= 2", "2", ast.Div},
		{"x %= 3 + 4", "(3 + 4)", ast.Rem},
	}

	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			body := mainBody(t, "    "+tt.src)
			a, ok := body[0].(*ast.Assign)
			if !ok {
				t.Fatalf("statement is %T, want *ast.Assign", body[0])
			}
			if a.Op != tt.op {
				t.Errorf("op = %v, want %v", a.Op, tt.op)
			}
			if a.Target.(*ast.Ident).Name != "x" {
				t.Errorf("target = %q, want x", a.Target.(*ast.Ident).Name)
			}
			if got := shape(t, a.Value); got != tt.value {
				t.Errorf("value = %s, want %s", got, tt.value)
			}
		})
	}
}

// A compound assignment's span runs from the target through the value.
func TestCompoundAssignmentSpan(t *testing.T) {
	src := "package main\n\nfunc main() {\n    total += 10\n}\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	a := file.Decls[0].(*ast.Func).Body[0].(*ast.Assign)
	if got := src[a.Span.Start:a.Span.End]; got != "total += 10" {
		t.Errorf("span covers %q, want %q", got, "total += 10")
	}
}

// TestRejectsMalformedCompoundAssignment checks a missing or extra operand is
// a parse error, the bitwise and shift forms as much as the others.
func TestRejectsMalformedCompoundAssignment(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{"x +=", "expected 'value' found '}'"},
		{"x += 1 2", "expected ';' found '2'"},
		{"1 += 2", "expected 'statement' found '1'"},
		{"x += y += 1", "expected ';' found '+='"},
		{"y := x += 1", "expected ';' found '+='"},
		{"x &= 1 2", "expected ';' found '2'"},
		{"x <<=", "expected 'value' found '}'"},
		{"var x += 1", "expected 'type or =' found '+='"},
		{"true += 1", "expected 'statement' found 'true'"},
	}

	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			_, diags := parse(t, "package main\n\nfunc main() {\n    "+tt.src+"\n}\n")
			if len(diags) == 0 {
				t.Fatalf("%q parsed without error", tt.src)
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
		})
	}
}
