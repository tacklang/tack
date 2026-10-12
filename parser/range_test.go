package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestParsesRangeLoops checks every range form records what it ranges
// over, its variables if any, and its body.
func TestParsesRangeLoops(t *testing.T) {
	tests := []struct {
		src   string
		name  string // the index variable, or "" for none
		elem  string // the element variable, or "" for none
		count string
	}{
		{"for i := range 10 {\n        f()\n    }", "i", "", "10"},
		{"for i := range n + 1 {\n        f()\n    }", "i", "", "(n + 1)"},
		{"for k := range f() {\n        f()\n    }", "k", "", "f()"},
		{"for range 3 {\n        f()\n    }", "", "", "3"},
		{"for range -1 {\n        f()\n    }", "", "", "-1"},
		{"for i, x := range a {\n        f()\n    }", "i", "x", "a"},
		{"for _, x := range b {\n        f()\n    }", "_", "x", "b"},
		{"for i, _ := range s {\n        f()\n    }", "i", "_", "s"},
		{"for i, x := range f() {\n        f()\n    }", "i", "x", "f()"},
		{"for i, j := range 3 {\n        f()\n    }", "i", "j", "3"},
	}

	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			loop, ok := mainBody(t, "    "+tt.src)[0].(*ast.ForRange)
			if !ok {
				t.Fatalf("statement is %T, want *ast.ForRange", mainBody(t, "    "+tt.src)[0])
			}
			if name := rangeVarName(t, loop.Var); name != tt.name {
				t.Errorf("variable = %q, want %q", name, tt.name)
			}
			if elem := rangeVarName(t, loop.Elem); elem != tt.elem {
				t.Errorf("element = %q, want %q", elem, tt.elem)
			}
			if got := shape(t, loop.X); got != tt.count {
				t.Errorf("count = %s, want %s", got, tt.count)
			}
			if len(loop.Body) != 1 {
				t.Errorf("body has %d statements, want 1", len(loop.Body))
			}
		})
	}
}

// rangeVarName returns the name of a range loop's variable v, or "" when
// it is nil, and reports one that is not immutable with no type or value
// written, as the parser makes every one.
func rangeVarName(t *testing.T, v *ast.Var) string {
	t.Helper()
	if v == nil {
		return ""
	}
	if v.Mutable || v.Value != nil || v.TypeExpr != nil {
		t.Errorf("variable is %+v, want immutable with no type or value", v)
	}
	return v.Name
}

// The loop's span runs from for through the closing brace, and the
// variable's from its name.
func TestRangeSpans(t *testing.T) {
	src := "package main\n\nfunc main() {\n    for idx := range 4 {\n    }\n}\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	loop := file.Decls[0].(*ast.Func).Body[0].(*ast.ForRange)
	if got := src[loop.Span.Start:loop.Span.End]; got != "for idx := range 4 {\n    }" {
		t.Errorf("loop spans %q", got)
	}
	if got := src[loop.Var.NameSpan.Start:loop.Var.NameSpan.End]; got != "idx" {
		t.Errorf("name spans %q", got)
	}
}

// TestRejectsMalformedRangeLoops checks the forms that are not part of the
// language are parse errors, as are a missing count and range outside a
// loop header.
func TestRejectsMalformedRangeLoops(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{"for i = range 3 {\n    }", "expected 'value' found 'range'"},
		{"for mut i := range 3 {\n    }", "cannot use mut on a range loop variable"},
		{"for mut i, x := range a {\n    }", "cannot use mut on a range loop variable"},
		{"for _, mut x := range a {\n    }", "cannot use mut on a range loop variable"},
		{"for mut _, mut x := range a {\n    }", "cannot use mut on a range loop variable"},
		{"for var i = range 3 {\n    }", "expected 'value' found 'range'"},
		{"for i, j = range a {\n    }", "expected 'value' found 'range'"},
		{"for i, := range a {\n    }", "expected"},
		{"for , x := range a {\n    }", "expected"},
		{"for i x := range a {\n    }", "expected"},
		{"for i, x := range {\n    }", "expected 'value' found '{'"},
		{"for i, x := range a", "expected '{' found ';'"},
		{"for i, x := range", "expected 'value'"},
		{"for i, x :=", "expected"},
		{"for i, x", "expected"},
		{"for i, mut", "expected"},
		{"for i[0], x := range a {\n    }", "expected"},
		{
			"for i, x, y := range a {\n    }",
			"assignment mismatch: 3 variables but range gives at most 2 values",
		},
		{
			"for i, x, y, z := range a {\n    }",
			"assignment mismatch: 4 variables but range gives at most 2 values",
		},
		{"for i := range {\n    }", "expected 'value' found '{'"},
		{"for range {\n    }", "expected 'value' found '{'"},
		{"for i := range 3; i < 2; i++ {\n    }", "expected '{' found ';'"},
		{"for i := range 3", "expected '{' found ';'"},
		{"x := range 3", "expected 'value' found 'range'"},
		{"range 3", "expected 'statement' found 'range'"},
		{"for 1 := range 3 {\n    }", "expected '{' found ':='"},
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
