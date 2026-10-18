package parser_test

import (
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

// parsedDefers pairs defers with how many statements each one's block
// holds.
var parsedDefers = []struct {
	src  string
	want int
}{
	{"defer {}", 0},
	{"defer {\n    }", 0},
	{"defer { f() }", 1},
	{"defer { x = 1 }", 1},
	{"defer { x++ }", 1},
	{"defer { a, b = f() }", 1},
	{"defer { f(); g() }", 2},
	{
		"defer {\n        x := 1\n        if x > 0 {\n        }\n        for {\n            break\n        }\n    }",
		3,
	},
	{"defer {\n        switch x {\n        case 1:\n        }\n        return\n    }", 2},
	{"defer { defer {} }", 1},
	{"defer\n    {}", 0},
}

// TestParsesDefers checks a defer takes a block of any statements, a defer
// among them, which the checker rejects.
func TestParsesDefers(t *testing.T) {
	for _, tt := range parsedDefers {
		t.Run(tt.src, func(t *testing.T) {
			if got := len(firstDefer(t, "    "+tt.src).Body); got != tt.want {
				t.Errorf("block holds %d statements, want %d", got, tt.want)
			}
		})
	}
}

// TestDeferSpans checks a defer spans its keyword through its closing
// brace, leaving the semicolon out.
func TestDeferSpans(t *testing.T) {
	for _, want := range []string{"defer {}", "defer { x += 2 }", "defer {\n        f()\n    }"} {
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

// blockRequired is the message a defer not followed by a block gets.
const blockRequired = "defer must be followed by a block: defer { ... }"

// malformedDefers pairs malformed or truncated defers with the message each
// must be reported with, and the text a defer not followed by a block must
// span: the token after the keyword.
var malformedDefers = []struct {
	src  string
	want string
	span string
}{
	{"defer", blockRequired, "}"},
	{"defer;", blockRequired, ";"},
	{"defer f()", blockRequired, "f"},
	{"defer f(g(1), \"s\")", blockRequired, "f"},
	{"defer x = 1", blockRequired, "x"},
	{"defer a[i].x = 1", blockRequired, "a"},
	{"defer x += 2", blockRequired, "x"},
	{"defer x++", blockRequired, "x"},
	{"defer a, b = f()", blockRequired, "a"},
	{"defer x := 1", blockRequired, "x"},
	{"defer var x = 1", blockRequired, "var"},
	{"defer mut x int64", blockRequired, "mut"},
	{"defer a, b := f()", blockRequired, "a"},
	{"defer if x {\n    }", blockRequired, "if"},
	{"defer for {\n    }", blockRequired, "for"},
	{"defer switch {\n    }", blockRequired, "switch"},
	{"defer return", blockRequired, "return"},
	{"defer break", blockRequired, "break"},
	{"defer continue", blockRequired, "continue"},
	{"defer defer {}", blockRequired, "defer"},
	{"defer 1", blockRequired, "1"},
	{"defer (f())", blockRequired, "("},
	{"defer \"s\"", blockRequired, "\"s\""},
	{"defer type T struct {}", blockRequired, "type"},
	{"defer }", blockRequired, "}"},
	{"defer { f() } g()", "expected ';' found 'g'", ""},
	{"defer { case 1: }", "expected 'statement' found 'case'", ""},
	{"defer { f( }", "expected 'value' found '}'", ""},
	{"defer {\n        defer f()\n    }", blockRequired, "f"},
	{"defer {\n        for {\n            defer x++\n        }\n    }", blockRequired, "x"},
}

// TestRejectsMalformedDefers checks a defer followed by anything but a
// block, at any depth, and every malformed defer, is a parse error at the
// token at fault.
func TestRejectsMalformedDefers(t *testing.T) {
	for _, tt := range malformedDefers {
		t.Run(tt.src, func(t *testing.T) {
			src := "package main\n\nfunc main() {\n    " + tt.src + "\n}\n"
			_, diags := parse(t, src)
			if len(diags) == 0 {
				t.Fatalf("%q parsed without error", tt.src)
			}
			if diags[0].Message != tt.want {
				t.Errorf("message = %q, want %q", diags[0].Message, tt.want)
			}
			if got := src[diags[0].Span.Start:diags[0].Span.End]; tt.span != "" && got != tt.span {
				t.Errorf("spans %q, want %q", got, tt.span)
			}
		})
	}
}

// TestTruncatedDefersNeverParse cuts defers off after every byte and checks
// each prefix is a parse error, never a panic or a tree without a bad
// node.
func TestTruncatedDefersNeverParse(t *testing.T) {
	src := "package main\n\nfunc main() {\n    defer { f(1) }\n    defer { x += 2 }\n    defer {\n        for {\n            break\n        }\n        g()\n    }\n}\n"
	end := strings.LastIndex(src, "}")
	for n := strings.Index(src, "defer"); n < end; n++ {
		file, diags := parse(t, src[:n])
		if len(diags) == 0 || file != nil && countBad(file) == 0 {
			t.Errorf("prefix %q parsed, want an error", src[:n])
		}
	}
}

// TestParsesDeeplyNestedDefers checks a defer block nested a thousand deep
// parses, though the checker rejects it, and so does a block of ten
// thousand defers.
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
	if got := len(mainBody(t, strings.Repeat("defer { f() }\n", n))); got != n {
		t.Errorf("got %d statements, want %d", got, n)
	}
}
