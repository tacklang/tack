package parser_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
)

// visibilitySrc declares a name of every kind with and without pub or lib.
const visibilitySrc = "package lexer\n\npub func F() {\n}\n\nlib type T struct {\n    pub a int64\n" +
	"    lib b int64\n    c int64\n}\n\npub var V int64 = 1\nlib mut M int64\nfunc g() {\n}\n"

// visibleText is what a declaration or field should start with, and the
// visibility it should have.
type visibleText struct {
	text string
	vis  ast.Visibility
}

// declVisibility returns the visibility of decl and the span of the
// keyword that gave it.
func declVisibility(decl ast.Decl) (ast.Visibility, ast.Span) {
	switch d := decl.(type) {
	case *ast.Func:
		return d.Vis, d.VisSpan
	case *ast.TypeDecl:
		return d.Vis, d.VisSpan
	case *ast.Var:
		return d.Vis, d.VisSpan
	}
	return ast.Private, ast.Span{}
}

// TestParsesVisibility checks pub and lib on a function, a type, a
// variable and a mutable variable: each declaration records its visibility
// and the keyword's span, and its span starts at the keyword, while one
// without a keyword is private.
func TestParsesVisibility(t *testing.T) {
	src := visibilitySrc
	file := mustParse(t, src)
	want := []visibleText{
		{"pub func F", ast.Pub},
		{"lib type T", ast.Lib},
		{"pub var V", ast.Pub},
		{"lib mut M", ast.Lib},
		{"func g", ast.Private},
	}
	if len(file.Decls) != len(want) {
		t.Fatalf("got %d declarations, want %d", len(file.Decls), len(want))
	}
	for i, decl := range file.Decls {
		vis, visSpan := declVisibility(decl)
		span := decl.Spanned()
		if got := src[span.Start:min(span.End, span.Start+len(want[i].text))]; vis != want[i].vis ||
			got != want[i].text {
			t.Errorf("declaration %d is %s starting %q, want %s starting %q",
				i, vis, got, want[i].vis, want[i].text)
		}
		if vis != ast.Private && src[visSpan.Start:visSpan.End] != vis.String() {
			t.Errorf("declaration %d's keyword covers %q", i, src[visSpan.Start:visSpan.End])
		}
	}
}

// TestParsesFieldVisibility checks pub and lib on fields: each records its
// visibility, and its span starts at the keyword.
func TestParsesFieldVisibility(t *testing.T) {
	src := visibilitySrc
	file := mustParse(t, src)
	st, _ := file.Decls[1].(*ast.TypeDecl).TypeExpr.(*ast.StructType)
	want := []visibleText{
		{"pub a int64", ast.Pub},
		{"lib b int64", ast.Lib},
		{"c int64", ast.Private},
	}
	for i, w := range want {
		f := st.Fields[i]
		if f.Vis != w.vis || src[f.Span.Start:f.Span.End] != w.text {
			t.Errorf("field %d is %s covering %q, want %s covering %q",
				i, f.Vis, src[f.Span.Start:f.Span.End], w.vis, w.text)
		}
	}
}
