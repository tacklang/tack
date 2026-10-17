package parser_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
)

// funcTypeSrc writes function types as parameters' types, a mut
// parameter among them, with no result, one, and several, one nested in
// another, a trailing comma, a field's type, and a parameter without a
// result followed by another without a comma between them.
const funcTypeSrc = "package main\n\ntype S struct {\n    run func(int64) int64\n}\n\n" +
	"func f(a func(), b func(int64, mut S) (int64, error), c func(func(int64) bool,) int64, " +
	"d func() n int64) {\n}\n"

// TestParsesFuncTypes checks that func(A, mut B) R is parsed wherever a
// type is written, spanning the keyword through its last result, and
// that a name and a type after one with no result start the next
// parameter.
func TestParsesFuncTypes(t *testing.T) {
	file, diags := parse(t, funcTypeSrc)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	fn := file.Decls[1].(*ast.Func)
	want := []string{
		"func()",
		"func(int64, mut S) (int64, error)",
		"func(func(int64) bool,) int64",
		"func()",
		"int64",
	}
	if len(fn.Params) != len(want) {
		t.Fatalf("got %d parameters, want %d", len(fn.Params), len(want))
	}
	for i, p := range fn.Params {
		if got := funcTypeSrc[p.TypeExpr.Spanned().Start:p.TypeExpr.Spanned().End]; got != want[i] {
			t.Errorf("param %s's type spans %q, want %q", p.Name, got, want[i])
		}
	}
	if fn.Params[4].Name != "n" {
		t.Errorf("the last parameter is %s, want n", fn.Params[4].Name)
	}
	st := file.Decls[0].(*ast.TypeDecl).TypeExpr.(*ast.StructType)
	if _, ok := st.Fields[0].TypeExpr.(*ast.FuncType); !ok {
		t.Errorf("the field's type is %T, want *ast.FuncType", st.Fields[0].TypeExpr)
	}
}

// TestFuncTypeParts checks a FuncType's parts: its parameters' types as
// written, mut where it is, and its results.
func TestFuncTypeParts(t *testing.T) {
	file, diags := parse(t, funcTypeSrc)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	fn := file.Decls[1].(*ast.Func)
	b, ok := fn.Params[1].TypeExpr.(*ast.FuncType)
	if !ok {
		t.Fatalf("b's type is %T, want *ast.FuncType", fn.Params[1].TypeExpr)
	}
	if len(b.Params) != 2 || b.Params[0].Mut || !b.Params[1].Mut || len(b.Results) != 2 {
		t.Errorf(
			"b's type has %d parameters and %d results, want 2 and 2, the second parameter mut",
			len(b.Params),
			len(b.Results),
		)
	}
	if got := funcTypeSrc[b.Params[1].Span.Start:b.Params[1].Span.End]; got != "mut S" {
		t.Errorf("b's second parameter spans %q, want %q", got, "mut S")
	}
}

// TestFuncTypeNesting checks a function type of no parameters and no
// results, and one whose parameter is a function type.
func TestFuncTypeNesting(t *testing.T) {
	file, diags := parse(t, funcTypeSrc)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	fn := file.Decls[1].(*ast.Func)
	c := fn.Params[2].TypeExpr.(*ast.FuncType)
	if _, ok := c.Params[0].TypeExpr.(*ast.FuncType); !ok || len(c.Params) != 1 {
		t.Errorf("c's parameter is %T, want one *ast.FuncType", c.Params[0].TypeExpr)
	}
	if a := fn.Params[0].TypeExpr.(*ast.FuncType); len(a.Params) != 0 || len(a.Results) != 0 {
		t.Errorf(
			"a's type has %d parameters and %d results, want none",
			len(a.Params),
			len(a.Results),
		)
	}
}

// TestRejectsFuncTypeShapes checks the function types the parser
// reports: a parameter with a name, a variadic one, a missing comma, an
// unclosed list, and a function literal.
func TestRejectsFuncTypeShapes(t *testing.T) {
	for _, tt := range []struct{ name, src, want string }{
		{
			"a named parameter", "package main\n\nfunc f(g func(x int64)) {\n}\n",
			"a function type's parameters have no names; write only the type, not x",
		},
		{
			"a variadic parameter", "package main\n\nfunc f(g func(...int64)) {\n}\n",
			"a function type cannot have a variadic parameter",
		},
		{
			"a missing comma", "package main\n\nfunc f(g func(int64 bool)) {\n}\n",
			"expected ')' found 'bool'",
		},
		{
			"an unclosed list", "package main\n\nfunc f(g func(int64) {\n}\n",
			"expected 'parameter name' found '{'",
		},
		{
			"a function literal", "package main\n\nfunc main() {\n    f := func() {\n    }\n}\n",
			"function literals are not supported; declare the function and use its name as a value",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := parse(t, tt.src)
			if len(diags) == 0 || diags[0].Message != tt.want {
				t.Fatalf("got %+v, want %q first", diags, tt.want)
			}
		})
	}
}

// funcTypeSeeds are function types in every place one is written, nested,
// cut short, and beside function literals and names before types.
var funcTypeSeeds = []string{
	funcTypeSrc,
	"package main\n\nvar f func(int64) int64 = g\n",
	"package main\n\nfunc f() func() func(mut int64) (int64, error) {\n}\n",
	"package main\n\nfunc f(g func() n int64, h func(int64) int64 m int64) {\n}\n",
	"package main\n\nfunc main() {\n    a := [2]func(){f, g}\n    l := make(list[func(int64)], c_allocator)\n" +
		"    m := make(map[string]func() bool, c_allocator)\n    x := v.(func())\n}\n",
	"package main\n\nfunc main() {\n    f := func(x int64) int64 {\n        return x\n    }\n}\n",
	"package main\n\nfunc f(g func(",
	"package main\n\nfunc f(g func(mut",
	"package main\n\nfunc f(g func(...int64)) {\n}\n",
	"package main\n\nfunc f(g func(x int64, y int64)) {\n}\n",
}

// FuzzParseFuncTypes parses arbitrary source, seeded with function types,
// and checks the parser never panics, gives a tree exactly when it
// reports nothing, and reports each problem at a span inside the source.
func FuzzParseFuncTypes(f *testing.F) {
	for _, src := range funcTypeSeeds {
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		file := &source.File{ID: "fuzz", Filename: "fuzz.tck", Data: []byte(src)}
		lex := lexer.New(file)
		if diags := lex.Lex(); len(diags) > 0 {
			return
		}
		tree, diags := parser.NewParser(file.ID, lex.Tokens()).Parse()
		if (tree == nil) == (len(diags) == 0) {
			t.Fatalf("tree %v with %d diagnostics", tree != nil, len(diags))
		}
		for _, d := range diags {
			checkSpan(t, src, d)
		}
	})
}
