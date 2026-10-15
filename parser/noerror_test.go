package parser_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestParsesNoError checks noerror becomes a NoErrorLit spanning only the
// keyword, wherever a value is expected.
func TestParsesNoError(t *testing.T) {
	src := "package main\n\nfunc main() {\n    f(noerror)\n}\n"
	args := callArgs(t, src)
	if len(args) != 1 {
		t.Fatalf("call has %d arguments, want 1", len(args))
	}
	lit, ok := args[0].(*ast.NoErrorLit)
	if !ok {
		t.Fatalf("argument is %T, want *ast.NoErrorLit", args[0])
	}
	if got := src[lit.Span.Start:lit.Span.End]; got != "noerror" {
		t.Errorf("argument spans %q, want %q", got, "noerror")
	}
}

// TestNoErrorInEveryPosition checks noerror is an operand like any other:
// a declared value, a returned one, an operand of ==, a field's value, a
// package-level initializer, and one written in parentheses; and that it
// cannot be declared or assigned as a name, since it is a keyword.
func TestNoErrorInEveryPosition(t *testing.T) {
	ok := []struct{ name, src string }{
		{"declaration", "package main\n\nfunc main() {\n    e := noerror\n}\n"},
		{"return", "package main\n\nfunc f() error {\n    return noerror\n}\n"},
		{"operand", "package main\n\nfunc main() {\n    x := e == noerror\n}\n"},
		{"field value", "package main\n\nfunc main() {\n    x := P{e: noerror}\n}\n"},
		{"package level", "package main\n\nvar none = noerror\n\nfunc main() {\n}\n"},
		{"in parentheses", "package main\n\nfunc main() {\n    x := (noerror)\n}\n"},
	}
	for _, tt := range ok {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := parse(t, tt.src); len(diags) > 0 {
				t.Errorf("parse reported %+v", diags)
			}
		})
	}
	for _, src := range []string{
		"package main\n\nfunc main() {\n    noerror := 1\n}\n",
		"package main\n\nfunc main() {\n    noerror = 1\n}\n",
		"package main\n\nfunc noerror() {\n}\n",
	} {
		if _, diags := parse(t, src); len(diags) == 0 {
			t.Errorf("parse accepted noerror as a name in\n%s", src)
		}
	}
}
