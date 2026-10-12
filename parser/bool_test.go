package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestParsesBoolLiterals checks true and false become BoolLit nodes, with
// spans covering only the keyword, wherever a value is expected.
func TestParsesBoolLiterals(t *testing.T) {
	src := "package main\n\nfunc main() {\n    f(true, false)\n}\n"
	args := callArgs(t, src)
	if len(args) != 2 {
		t.Fatalf("call has %d arguments, want 2", len(args))
	}

	for i, want := range []bool{true, false} {
		lit, ok := args[i].(*ast.BoolLit)
		if !ok {
			t.Fatalf("argument %d is %T, want *ast.BoolLit", i, args[i])
		}
		if lit.Value != want {
			t.Errorf("argument %d = %v, want %v", i, lit.Value, want)
		}
		word := "true"
		if !want {
			word = "false"
		}
		if got := src[lit.Span.Start:lit.Span.End]; got != word {
			t.Errorf("argument %d spans %q, want %q", i, got, word)
		}
	}
}

// A bool literal is an operand like any other, so it works as a declared
// value, an if condition, a return value and an operand of an operator.
func TestBoolLiteralsInEveryPosition(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"declaration", "package main\n\nfunc main() {\n    x := true\n}\n"},
		{"assignment", "package main\n\nfunc main() {\n    x = false\n}\n"},
		{"if condition", "package main\n\nfunc main() {\n    if true {\n    }\n}\n"},
		{"return", "package main\n\nfunc f() bool {\n    return false\n}\n"},
		{"operand", "package main\n\nfunc main() {\n    x := true == (1 < 2)\n}\n"},
		{"package level", "package main\n\nvar on = true\n\nfunc main() {\n}\n"},
		{"in parentheses", "package main\n\nfunc main() {\n    x := (false)\n}\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := parse(t, tt.src); len(diags) > 0 {
				t.Errorf("parse reported %+v", diags)
			}
		})
	}
}

// TestRejectsBoolKeywordsAsNames checks true and false, being keywords, are
// a parse error wherever a name is expected.
func TestRejectsBoolKeywordsAsNames(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{"package main\n\nfunc main() {\n    true := 1\n}\n", "expected 'statement' found 'true'"},
		{"package main\n\nfunc main() {\n    false = 1\n}\n", "expected 'statement' found 'false'"},
		{
			"package main\n\nfunc main() {\n    var true = 1\n}\n",
			"expected 'variable name' found 'true'",
		},
		{
			"package main\n\nfunc main() {\n    mut false int\n}\n",
			"expected 'variable name' found 'false'",
		},
		{"package main\n\nvar true = 1\n", "expected 'variable name' found 'true'"},
		{"package main\n\nfunc true() {\n}\n", "found 'true'"},
		{"package main\n\nfunc f(false int) {\n}\n", "found 'false'"},
		{"package main\n\nfunc main() {\n    true()\n}\n", "expected 'statement' found 'true'"},
		{"package main\n\nfunc main() {\n    x := true()\n}\n", "expected ';' found '('"},
		{"package true\n", "found 'true'"},
	}

	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			_, diags := parse(t, tt.src)
			if len(diags) == 0 {
				t.Fatalf("%q parsed without error", tt.src)
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
		})
	}
}
