package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestParsesCharLiterals checks a character literal becomes a CharLit
// holding its one decoded byte, with a span covering it and its quotes.
func TestParsesCharLiterals(t *testing.T) {
	src := "package main\n\nfunc main() {\n    f('a', '\\n', '\\'', '\\\\', '\"', '\xff')\n}\n"
	want := []struct {
		text  string
		value byte
	}{
		{"'a'", 'a'},
		{`'\n'`, '\n'},
		{`'\''`, '\''},
		{`'\\'`, '\\'},
		{`'"'`, '"'},
		{"'\xff'", 0xff},
	}
	args := callArgs(t, src)
	if len(args) != len(want) {
		t.Fatalf("call has %d arguments, want %d", len(args), len(want))
	}
	for i, w := range want {
		lit, ok := args[i].(*ast.CharLit)
		if !ok {
			t.Fatalf("argument %d is %T, want *ast.CharLit", i, args[i])
		}
		if lit.Value != w.value {
			t.Errorf("argument %d = %d, want %d", i, lit.Value, w.value)
		}
		if got := src[lit.Span.Start:lit.Span.End]; got != w.text {
			t.Errorf("argument %d spans %q, want %q", i, got, w.text)
		}
	}
}

// TestCharLiteralGrouping checks a character literal is an operand like any
// other: an operator's, an index's, and one a - before it negates rather
// than joining it, as it does an integer literal.
func TestCharLiteralGrouping(t *testing.T) {
	tests := []struct{ expr, want string }{
		{"'a' + 1", "('a' + 1)"},
		{"'z' - 'a' * 2", "('z' - ('a' * 2))"},
		{"c == 'a' || c == 'b'", "((c == 'a') || (c == 'b'))"},
		{"-'a'", "(-'a')"},
		{"!'a'", "(!'a')"},
		{"('a')", "'a'"},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			if got := shape(t, declared(t, tt.expr)); got != tt.want {
				t.Errorf("%s parsed as %s, want %s", tt.expr, got, tt.want)
			}
		})
	}
}

// TestCharLiteralsInEveryPosition checks a character literal parses
// wherever a value can be written, including an index, an array length, an
// array literal's elements and a package-level initializer.
func TestCharLiteralsInEveryPosition(t *testing.T) {
	tests := []struct{ name, src string }{
		{"declaration", "package main\n\nfunc main() {\n    x := 'a'\n}\n"},
		{"assignment", "package main\n\nfunc main() {\n    x = 'a'\n}\n"},
		{"return", "package main\n\nfunc f() uint8 {\n    return 'a'\n}\n"},
		{"index", "package main\n\nfunc main() {\n    x := a['a']\n}\n"},
		{"indexed", "package main\n\nfunc main() {\n    x := 'a'[0]\n}\n"},
		{"element target", "package main\n\nfunc main() {\n    a['a'] = 'b'\n}\n"},
		{"array length", "package main\n\nfunc main() {\n    mut a ['a']int64\n}\n"},
		{"array literal", "package main\n\nvar a = [2]uint8{'a', 'b'}\n\nfunc main() {\n}\n"},
		{"package level", "package main\n\nvar c = 'a'\n\nfunc main() {\n}\n"},
		{"range count", "package main\n\nfunc main() {\n    for range 'a' {\n    }\n}\n"},
		{"conversion", "package main\n\nfunc main() {\n    x := int64('a')\n}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := parse(t, tt.src); len(diags) > 0 {
				t.Errorf("parse reported %+v", diags)
			}
		})
	}
}

// TestRejectsMalformedCharUses checks a character literal is not a name: it
// cannot be called, declared or assigned as one.
func TestRejectsMalformedCharUses(t *testing.T) {
	tests := []struct{ src, want string }{
		{"package main\n\nfunc main() {\n    x := 'a'()\n}\n", "expected ';' found '('"},
		{"package main\n\nfunc main() {\n    'a' = 1\n}\n", "expected 'statement' found 'a'"},
		{
			"package main\n\nfunc main() {\n    var 'a' = 1\n}\n",
			"expected 'variable name' found 'a'",
		},
		{"package main\n\nfunc f('a' int64) {\n}\n", "found 'a'"},
		{"package main\n\nfunc main() {\n    x := 'a' 'b'\n}\n", "expected ';' found 'b'"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			_, diags := parse(t, tt.src)
			if len(diags) == 0 {
				t.Fatalf("no diagnostic, want one containing %q", tt.want)
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
		})
	}
}
