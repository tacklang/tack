package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestParsesLoops checks each loop form gives a For with the right condition
// and body.
func TestParsesLoops(t *testing.T) {
	tests := []struct {
		src  string
		cond string // the shape of the condition, or "" for none
		body int
	}{
		{"for {\n    }", "", 0},
		{"for {\n        break\n    }", "", 1},
		{"for a {\n    }", "a", 0},
		{"for i < 10 {\n        i = i + 1\n    }", "(i < 10)", 1},
		{"for !done && i < 3 {\n        f()\n        continue\n    }", "((!done) && (i < 3))", 2},
		{"for (a) {\n    }", "a", 0},
		{"for true {\n    }", "true", 0},
		{"for f() {\n    }", "f()", 0},
	}

	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			body := mainBody(t, "    "+tt.src)
			loop, ok := body[0].(*ast.For)
			if !ok {
				t.Fatalf("statement is %T, want *ast.For", body[0])
			}
			got := ""
			if loop.Cond != nil {
				got = shape(t, loop.Cond)
			}
			if got != tt.cond {
				t.Errorf("condition = %q, want %q", got, tt.cond)
			}
			if len(loop.Body) != tt.body {
				t.Errorf("body has %d statements, want %d", len(loop.Body), tt.body)
			}
		})
	}
}

// break and continue are statements of their own, anywhere a statement can
// be; whether they are inside a loop is the type checker's to judge.
func TestParsesBreakAndContinue(t *testing.T) {
	body := mainBody(t, "    break\n    continue\n    if a {\n        break\n    }")
	if _, ok := body[0].(*ast.Break); !ok {
		t.Errorf("statement 0 is %T, want *ast.Break", body[0])
	}
	if _, ok := body[1].(*ast.Continue); !ok {
		t.Errorf("statement 1 is %T, want *ast.Continue", body[1])
	}
	if _, ok := body[2].(*ast.If).Then[0].(*ast.Break); !ok {
		t.Errorf("if body holds %T, want *ast.Break", body[2].(*ast.If).Then[0])
	}
}

// Loops nest, and a loop is followed by other statements as usual.
func TestParsesNestedLoops(t *testing.T) {
	body := mainBody(
		t,
		"    for a {\n        for {\n            break\n        }\n        continue\n    }\n    f()",
	)
	if len(body) != 2 {
		t.Fatalf("main has %d statements, want 2", len(body))
	}
	outer := body[0].(*ast.For)
	if inner, ok := outer.Body[0].(*ast.For); !ok || inner.Cond != nil {
		t.Errorf(
			"first statement in the loop is %#v, want a for without a condition",
			outer.Body[0],
		)
	}
}

// A loop's span runs from for through the closing brace, and a jump's covers
// its keyword.
func TestLoopSpans(t *testing.T) {
	src := "package main\n\nfunc main() {\n    for a {\n        break\n    }\n    continue\n}\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}

	body := file.Decls[0].(*ast.Func).Body
	loop := body[0].(*ast.For)
	if got := src[loop.Span.Start:loop.Span.End]; got != "for a {\n        break\n    }" {
		t.Errorf("loop span covers %q", got)
	}
	brk := loop.Body[0].(*ast.Break)
	if got := src[brk.Span.Start:brk.Span.End]; got != "break" {
		t.Errorf("break span covers %q, want %q", got, "break")
	}
	cont := body[1].(*ast.Continue)
	if got := src[cont.Span.Start:cont.Span.End]; got != "continue" {
		t.Errorf("continue span covers %q, want %q", got, "continue")
	}
}

// A loop on one line with the closing brace of its enclosing block works
// like an if does.
func TestLoopBeforeClosingBraceOnOneLine(t *testing.T) {
	src := "package main\n\nfunc main() {\n    for { break }\n    if a { for a { continue } }\n}\n"
	if _, diags := parse(t, src); len(diags) > 0 {
		t.Errorf("parse reported %+v", diags)
	}
}

// TestRejectsMalformedLoops checks each malformed loop or jump is a parse
// error, including the forms that are not part of the language yet.
func TestRejectsMalformedLoops(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{"for\n    }", "expected 'value' found '}'"},
		{"for a\n    }", "expected '{' found ';'"},
		{"for a {", "expected"},
		{"for a {\n    } else {\n    }", "expected ';' found 'else'"},
		{"break 1", "expected ';' found '1'"},
		{"break outer", "expected ';' found 'outer'"},
		{"continue outer", "expected ';' found 'outer'"},
		{"x := break", "expected 'value' found 'break'"},
		{"for for {\n    }", "expected 'value' found 'for'"},
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
