package parser_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// firstDefer parses body as main's statements and returns the first one
// as a defer.
func firstDefer(t *testing.T, body string) *ast.Defer {
	t.Helper()
	stmts := mainBody(t, body)
	d, ok := stmts[0].(*ast.Defer)
	if !ok {
		t.Fatalf("statement is %T, want *ast.Defer", stmts[0])
	}
	return d
}

// deferShape renders a defer as "block/n", n being how many statements its
// block holds, or as the type of the one statement it defers.
func deferShape(d *ast.Defer) string {
	if d.Block {
		return fmt.Sprintf("block/%d", len(d.Body))
	}
	if len(d.Body) != 1 {
		return fmt.Sprintf("unbraced/%d", len(d.Body))
	}
	return fmt.Sprintf("%T", d.Body[0])
}

// parsedDefers pairs defers with the shape deferShape gives them.
var parsedDefers = []struct {
	src  string
	want string
}{
	{"defer f()", "*ast.ExprStmt"},
	{"defer f(g(1), \"s\")", "*ast.ExprStmt"},
	{"defer x = 1", "*ast.Assign"},
	{"defer a[i].x = Point{x: 1}", "*ast.Assign"},
	{"defer x += 2", "*ast.Assign"},
	{"defer x %= 2", "*ast.Assign"},
	{"defer x++", "*ast.IncDec"},
	{"defer a[0]--", "*ast.IncDec"},
	{"defer a, b = f()", "*ast.MultiAssign"},
	{"defer a, _ = f()", "*ast.MultiAssign"},
	{"defer {}", "block/0"},
	{"defer {\n    }", "block/0"},
	{"defer { f(); g() }", "block/2"},
	{
		"defer {\n        x := 1\n        if x > 0 {\n        }\n        for {\n            break\n        }\n    }",
		"block/3",
	},
	{"defer {\n        switch x {\n        case 1:\n        }\n        return\n    }", "block/2"},
	{"defer { defer f() }", "block/1"},
	{"defer\n    f()", "*ast.ExprStmt"},
}

// TestParsesDefers checks every statement that may follow defer directly,
// and a block of any statements.
func TestParsesDefers(t *testing.T) {
	for _, tt := range parsedDefers {
		t.Run(tt.src, func(t *testing.T) {
			if got := deferShape(firstDefer(t, "    "+tt.src)); got != tt.want {
				t.Errorf("shape = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDeferSpans checks a defer spans its keyword through the end of its
// statement or closing brace, leaving the semicolon out.
func TestDeferSpans(t *testing.T) {
	for _, want := range []string{"defer f(1)", "defer x += 2", "defer x++", "defer {\n        f()\n    }"} {
		src := "package main\n\nfunc main() {\n    " + want + "\n    g()\n}\n"
		file, diags := parse(t, src)
		if len(diags) > 0 {
			t.Fatalf("parse reported %+v", diags)
		}
		d := file.Decls[0].(*ast.Func).Body[0].(*ast.Defer)
		if got := src[d.Span.Start:d.Span.End]; got != want {
			t.Errorf("defer spans %q, want %q", got, want)
		}
	}
}

// malformedDefers pairs malformed or truncated defers with the message each
// must be reported with.
var malformedDefers = []struct {
	src  string
	want string
}{
	{"defer", "expected 'call, assignment or block' found '}'"},
	{"defer;", "expected 'call, assignment or block' found ';'"},
	{"defer if x {\n    }", "expected 'call, assignment or block' found 'if'"},
	{"defer for {\n    }", "expected 'call, assignment or block' found 'for'"},
	{"defer switch {\n    }", "expected 'call, assignment or block' found 'switch'"},
	{"defer return", "expected 'call, assignment or block' found 'return'"},
	{"defer break", "expected 'call, assignment or block' found 'break'"},
	{"defer continue", "expected 'call, assignment or block' found 'continue'"},
	{"defer defer f()", "expected 'call, assignment or block' found 'defer'"},
	{"defer 1", "expected 'call, assignment or block' found '1'"},
	{"defer (f())", "expected 'call, assignment or block' found '('"},
	{"defer type T struct {}", "expected 'call, assignment or block' found 'type'"},
	{"defer x := 1", "a variable declaration cannot be deferred"},
	{"defer var x = 1", "a variable declaration cannot be deferred"},
	{"defer mut x int64", "a variable declaration cannot be deferred"},
	{"defer a, b := f()", "a variable declaration cannot be deferred"},
	{"defer x", "expected 'statement' found 'x'"},
	{"defer f() g()", "expected ';' found 'g'"},
	{"defer f(", "expected 'value' found '}'"},
	{"defer x =", "expected 'value' found '}'"},
	{"defer { f() } g()", "expected ';' found 'g'"},
	{"defer { case 1: }", "expected 'statement' found 'case'"},
	{"defer {\n        defer if x {}\n    }", "expected 'call, assignment or block' found 'if'"},
}

// TestRejectsMalformedDefers checks every statement that may not follow
// defer directly, and every malformed defer, is a parse error.
func TestRejectsMalformedDefers(t *testing.T) {
	for _, tt := range malformedDefers {
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

// TestTruncatedDefersNeverParse cuts defers off after every byte and checks
// each prefix is a parse error, never a panic or a tree.
func TestTruncatedDefersNeverParse(t *testing.T) {
	src := "package main\n\nfunc main() {\n    defer f(1)\n    defer x += 2\n    defer {\n        for {\n            break\n        }\n        defer g()\n    }\n}\n"
	end := strings.LastIndex(src, "}")
	for n := strings.Index(src, "defer"); n < end; n++ {
		file, diags := parse(t, src[:n])
		if file != nil || len(diags) == 0 {
			t.Errorf("prefix %q parsed, want an error", src[:n])
		}
	}
}

// TestParsesDeeplyNestedDefers checks a defer block nested a thousand deep
// parses, and so does a block of ten thousand defers.
func TestParsesDeeplyNestedDefers(t *testing.T) {
	const depth = 1000
	d := firstDefer(t, strings.Repeat("defer {\n", depth)+strings.Repeat("}\n", depth))
	for range depth - 1 {
		d = d.Body[0].(*ast.Defer)
	}
	if len(d.Body) != 0 {
		t.Errorf("innermost defer has %d statements, want none", len(d.Body))
	}

	const n = 10000
	if got := len(mainBody(t, strings.Repeat("defer f()\n", n))); got != n {
		t.Errorf("got %d statements, want %d", got, n)
	}
}
