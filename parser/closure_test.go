package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestParsesFuncLits checks that a function literal written as an
// argument is a FuncLit whose Func has no name, is marked a closure,
// holds the literal's parameters, scope, mut and own ones among them,
// its results and its body, and spans from the keyword func through the
// closing brace, the Func's NameSpan being the keyword alone.
func TestParsesFuncLits(t *testing.T) {
	src := "package main\n\nfunc main() {\n" +
		"    each(v, func(x int64, mut y T, own l list[int64], scope g func()) (int64, error) {\n" +
		"        return x, noerror\n    })\n}\n"
	file := mustParse(t, src)
	call := file.Decls[0].(*ast.Func).Body[0].(*ast.ExprStmt).X.(*ast.Call)
	lit, ok := call.Args[1].(*ast.FuncLit)
	if !ok {
		t.Fatalf("the second argument is %T, want *ast.FuncLit", call.Args[1])
	}
	fn := lit.Func
	if !fn.Closure || fn.Name != "" || fn.Span != lit.Span {
		t.Errorf("Func: closure=%v name=%q span %v, want a closure, no name, span %v",
			fn.Closure, fn.Name, fn.Span, lit.Span)
	}
	if got := src[lit.Span.Start:lit.Span.End]; !strings.HasPrefix(got, "func(x int64") ||
		!strings.HasSuffix(got, "}") {
		t.Errorf("the literal spans %q", got)
	}
	if got := src[fn.NameSpan.Start:fn.NameSpan.End]; got != "func" {
		t.Errorf("NameSpan is %q, want func", got)
	}
	want := []struct {
		name, span      string
		mut, own, scope bool
	}{
		{"x", "x int64", false, false, false},
		{"y", "mut y T", true, false, false},
		{"l", "own l list[int64]", false, true, false},
		{"g", "scope g func()", false, false, true},
	}
	if len(fn.Params) != len(want) {
		t.Fatalf("got %d parameters, want %d", len(fn.Params), len(want))
	}
	for i, w := range want {
		p := fn.Params[i]
		if p.Name != w.name || p.Mut != w.mut || p.Own != w.own || p.Scope != w.scope {
			t.Errorf("param %d: got %s mut=%v own=%v scope=%v", i, p.Name, p.Mut, p.Own, p.Scope)
		}
		if got := src[p.Span.Start:p.Span.End]; got != w.span {
			t.Errorf("param %s spans %q, want %q", p.Name, got, w.span)
		}
	}
	if len(fn.Results) != 2 || len(fn.Body) != 1 {
		t.Errorf("got %d results and %d statements, want 2 and 1", len(fn.Results), len(fn.Body))
	}
}

// TestParsesScopeParams checks that scope before a parameter's name, of a
// function or of a function type, marks it scope with the keyword's span,
// its own span starting there, and leaves the others plain.
func TestParsesScopeParams(t *testing.T) {
	src := "package main\n\nfunc by(mut l list[T], scope less func(T, T) bool, f func(scope func(int64))) {\n}\n"
	file := mustParse(t, src)
	params := file.Decls[0].(*ast.Func).Params
	less := params[1]
	if !less.Scope || less.Mut || less.Own || src[less.ScopeSpan.Start:less.ScopeSpan.End] != "scope" ||
		src[less.Span.Start:less.Span.End] != "scope less func(T, T) bool" {
		t.Errorf("less: scope=%v span %q", less.Scope, src[less.Span.Start:less.Span.End])
	}
	if params[0].Scope || params[2].Scope {
		t.Errorf("only less should be scope")
	}
	ft := params[2].TypeExpr.(*ast.FuncType)
	if p := ft.Params[0]; !p.Scope || src[p.Span.Start:p.Span.End] != "scope func(int64)" {
		t.Errorf("the function type's parameter: scope=%v span %q", p.Scope, src[p.Span.Start:p.Span.End])
	}
}

// TestFuncLitBodyAllowsLiterals checks that a struct literal in the
// header of an if inside a closure needs parentheses as anywhere, while
// one in the closure's body, written inside the header of an outer if,
// needs none: the literal's braces open a block of its own.
func TestFuncLitBodyAllowsLiterals(t *testing.T) {
	src := "package main\n\nfunc main() {\n" +
		"    if first(v, func(x int64) bool {\n        p := P{x: x}\n        return p.x > 0\n    }) {\n" +
		"        print(\"x\")\n    }\n}\n"
	mustParse(t, src)
}

// TestRejectsScopeMisuse checks the errors of scope written where it
// cannot be: with mut or own, in either order, and before a result.
func TestRejectsScopeMisuse(t *testing.T) {
	tests := []struct {
		src, want, at string
	}{
		{
			"package main\n\nfunc f(scope mut g func()) {\n}\n",
			"a parameter cannot be both scope and mut", "mut",
		},
		{
			"package main\n\nfunc f(mut scope g func()) {\n}\n",
			"a parameter cannot be both scope and mut", "scope",
		},
		{
			"package main\n\nfunc f(scope own g func()) {\n}\n",
			"a parameter cannot be both scope and own", "own",
		},
		{
			"package main\n\nfunc f(own scope g func()) {\n}\n",
			"a parameter cannot be both scope and own", "scope",
		},
		{
			"package main\n\nfunc f() scope func() {\n}\n",
			"a result cannot be scope", "scope",
		},
		{
			"package main\n\nfunc f(g func() scope func()) {\n}\n",
			"a result cannot be scope", "scope",
		},
	}
	for _, tt := range tests {
		_, diags := parse(t, tt.src)
		if len(diags) == 0 {
			t.Errorf("no error for:\n%s", tt.src)
			continue
		}
		d := diags[0]
		if !strings.Contains(d.Message, tt.want) || tt.src[d.Span.Start:d.Span.End] != tt.at {
			t.Errorf("got %q at %q, want %q at %q, for:\n%s",
				d.Message, tt.src[d.Span.Start:d.Span.End], tt.want, tt.at, tt.src)
		}
	}
}

// TestRejectsCalledFuncLit checks that a closure called where it is
// written is reported at its keyword, and that func and a name where an
// operand is, a declaration, is reported as no value, as any other
// keyword there is.
func TestRejectsCalledFuncLit(t *testing.T) {
	src := "package main\n\nfunc main() {\n    x := func() int64 { return 1 }()\n}\n"
	_, diags := parse(t, src)
	if len(diags) == 0 || !strings.Contains(diags[0].Message, "a closure cannot be called where it is written") ||
		src[diags[0].Span.Start:diags[0].Span.End] != "func" {
		t.Errorf("got %+v", diags)
	}
	src = "package main\n\nvar limit int64 = (3 +\n\nfunc main() {\n}\n"
	_, diags = parse(t, src)
	if len(diags) == 0 || diags[0].Message != "expected 'value' found 'func'" {
		t.Errorf("got %+v", diags)
	}
}

// funcLitSeeds are sources with closures and scope parameters, nested,
// broken off and misplaced, for FuzzParseClosures to start from.
var funcLitSeeds = []string{
	"package main\n\nfunc each(v []int64, scope f func(int64)) {\n    for _, x := range v {\n        f(x)\n    }\n}\n",
	"package main\n\nfunc main() {\n    each(v, func(x int64) {\n        each(w, func(y int64) {\n            n = x + y\n        })\n    })\n}\n",
	"package main\n\nfunc main() {\n    sort.by(mut l, func(a int64, b int64) bool { return a < b })\n}\n",
	"package main\n\nfunc main() {\n    f := func() {}\n    g := func() int64 { return 1 }()\n}\n",
	"package main\n\nfunc main() {\n    call(func(",
	"package main\n\nfunc main() {\n    call(func() {",
	"package main\n\nfunc f(scope scope g func()) {\n}\n",
	"package main\n\nfunc f(scope mut g func(scope own func())) scope func() {\n}\n",
	"package main\n\nfunc main() {\n    if first(v, func(x int64) bool { return P{x: x}.x > 0 }) {\n    }\n}\n",
	"package main\n\nfunc main() {\n    call(func() { defer f(); return })\n    x := (func() {})\n}\n",
}

// FuzzParseClosures parses arbitrary source, seeded with closures and
// scope parameters, and checks the parser never panics, gives a tree
// exactly when it reports nothing, and reports each problem at a span
// inside the source.
func FuzzParseClosures(f *testing.F) {
	for _, src := range funcLitSeeds {
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		checkParse(t, src)
	})
}
