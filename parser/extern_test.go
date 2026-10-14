package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// onlyFunc parses src, which must parse cleanly, and returns its first
// declaration, a function.
func onlyFunc(t *testing.T, src string) *ast.Func {
	t.Helper()
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	fn, ok := file.Decls[0].(*ast.Func)
	if !ok {
		t.Fatalf("first declaration is %T, want *ast.Func", file.Decls[0])
	}
	return fn
}

// TestExternFunc checks an extern func parses with its signature, no
// body, its C name its own, and its span and ExternSpan starting at
// extern.
func TestExternFunc(t *testing.T) {
	src := "package main\n\nextern func add(a int64, b int64) int64\n"
	fn := onlyFunc(t, src)
	switch {
	case !fn.Extern || !fn.NoBody || fn.CName != "add" || len(fn.Body) != 0:
		t.Errorf("Extern %v, NoBody %v, CName %q, %d statements; want true, true, add, 0",
			fn.Extern, fn.NoBody, fn.CName, len(fn.Body))
	case len(fn.Params) != 2 || len(fn.Results) != 1:
		t.Errorf("%d params and %d results, want 2 and 1", len(fn.Params), len(fn.Results))
	}
	start := strings.Index(src, "extern")
	if fn.Span.Start != start || fn.ExternSpan.Start != start || fn.ExternSpan.End != start+6 {
		t.Errorf("Span starts at %d, ExternSpan %d-%d; want %d, %d-%d",
			fn.Span.Start, fn.ExternSpan.Start, fn.ExternSpan.End, start, start, start+6)
	}
}

// TestNoBody checks a function written without a body is told apart from
// one with an empty body, and that neither is extern.
func TestNoBody(t *testing.T) {
	for _, tc := range []struct {
		src    string
		noBody bool
	}{
		{"package main\n\nfunc f()\n", true},
		{"package main\n\nfunc f() int64\n", true},
		{"package main\n\nfunc f() {}\n", false},
		{"package main\n\nfunc f() {\n}\n", false},
	} {
		fn := onlyFunc(t, tc.src)
		if fn.NoBody != tc.noBody || fn.Extern || fn.CName != "" {
			t.Errorf("%q: NoBody %v, Extern %v, CName %q; want %v, false, empty",
				tc.src, fn.NoBody, fn.Extern, fn.CName, tc.noBody)
		}
	}
}

// TestExternErrors checks each malformed extern declaration is reported
// with what was expected, never a panic.
func TestExternErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"package main\n\nextern func f() {\n}\n", "extern func f cannot have a body"},
		{"package main\n\nextern func f() int64 { return 1 }\n", "extern func f cannot have a body"},
		{"package main\n\nextern var x = 1\n", "expected 'func' found 'var'"},
		{"package main\n\nextern\n", "expected 'func'"},
		{"package main\n\nextern", "expected 'func'"},
		{"package main\n\nextern func\n", "function name"},
		{"package main\n\nextern func f(", "expected"},
		{"package main\n\nextern extern func f()\n", "expected 'func' found 'extern'"},
		{"package main\n\nfunc main() {\n    extern func f()\n}\n", "found 'extern'"},
	} {
		_, diags := parse(t, tc.src)
		if len(diags) == 0 || !strings.Contains(diags[0].Message, tc.want) {
			t.Errorf("%q: diagnostics %+v, want one containing %q", tc.src, diags, tc.want)
		}
	}
}
