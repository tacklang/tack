package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
)

// TestParsesOwnParams checks that own before a parameter's name marks it
// own, with the keyword's span, and that its span starts at own; a
// parameter beside it, plain or mut, is neither.
func TestParsesOwnParams(t *testing.T) {
	src := "package main\n\nfunc f(own t Text, k int64, mut a list[int64], own l list[T]) {\n}\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	params := file.Decls[0].(*ast.Func).Params
	want := []struct {
		name, span, ownSpan string
		own, mut            bool
	}{
		{"t", "own t Text", "own", true, false},
		{"k", "k int64", "", false, false},
		{"a", "mut a list[int64]", "", false, true},
		{"l", "own l list[T]", "own", true, false},
	}
	if len(params) != len(want) {
		t.Fatalf("got %d parameters, want %d", len(params), len(want))
	}
	for i, w := range want {
		p := params[i]
		if p.Name != w.name || p.Own != w.own || p.Mut != w.mut {
			t.Errorf("param %d: got %s own=%v mut=%v, want %s own=%v mut=%v",
				i, p.Name, p.Own, p.Mut, w.name, w.own, w.mut)
		}
		if got := src[p.Span.Start:p.Span.End]; got != w.span {
			t.Errorf("param %s spans %q, want %q", p.Name, got, w.span)
		}
		if got := src[p.OwnSpan.Start:p.OwnSpan.End]; got != w.ownSpan {
			t.Errorf("param %s own keyword spans %q, want %q", p.Name, got, w.ownSpan)
		}
	}
}

// TestParsesOwnFuncTypeParams checks that own before a function type's
// parameter type marks it own, its span starting at own.
func TestParsesOwnFuncTypeParams(t *testing.T) {
	src := "package main\n\nfunc apply(f func(own list[int64], int64, mut Text) int64) {\n}\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	ft := file.Decls[0].(*ast.Func).Params[0].TypeExpr.(*ast.FuncType)
	if len(ft.Params) != 3 {
		t.Fatalf("got %d parameters, want 3", len(ft.Params))
	}
	p := ft.Params[0]
	if !p.Own || p.Mut || src[p.Span.Start:p.Span.End] != "own list[int64]" ||
		src[p.OwnSpan.Start:p.OwnSpan.End] != "own" {
		t.Errorf("first parameter: own=%v mut=%v span %q",
			p.Own, p.Mut, src[p.Span.Start:p.Span.End])
	}
	if ft.Params[1].Own || ft.Params[2].Own || !ft.Params[2].Mut {
		t.Errorf("only the first parameter should be own, and the last mut")
	}
}

// TestParsesOwnArgs checks that own before an argument makes an OwnArg
// spanning the keyword through what follows it, whatever that is, since
// the type checker says which are allowed; that own before a method call
// covers the whole call, not its receiver, as mut does; and that plain
// arguments beside it stay plain.
func TestParsesOwnArgs(t *testing.T) {
	tests := []struct {
		arg, x string
	}{
		{"own b", "b"},
		{"own  b", "b"},
		{"own p.x", "p.x"},
		{"own a[i]", "a[i]"},
		{"own g()", "g()"},
		{"own t.m()", "t.m()"},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			src := "package main\n\nfunc main() {\n    f(1, " + tt.arg + ", x)\n}\n"
			args := callArgs(t, src)
			if len(args) != 3 {
				t.Fatalf("got %d arguments, want 3", len(args))
			}
			o, ok := args[1].(*ast.OwnArg)
			if !ok {
				t.Fatalf("argument is %T, want *ast.OwnArg", args[1])
			}
			if got := src[o.Span.Start:o.Span.End]; got != tt.arg {
				t.Errorf("own argument spans %q, want %q", got, tt.arg)
			}
			if got := src[o.X.Spanned().Start:o.X.Spanned().End]; got != tt.x {
				t.Errorf("what follows own spans %q, want %q", got, tt.x)
			}
			for _, i := range []int{0, 2} {
				if _, isOwn := args[i].(*ast.OwnArg); isOwn {
					t.Errorf("argument %d is an own argument", i)
				}
			}
		})
	}
}

// TestRejectsMutAndOwnTogether checks that a parameter written both mut
// and own, in either order, in a function or a function type, is reported
// at the second keyword.
func TestRejectsMutAndOwnTogether(t *testing.T) {
	tests := []struct {
		params, second string
	}{
		{"mut own l list[int64]", "own"},
		{"own mut l list[int64]", "mut"},
		{"f func(mut own list[int64])", "own"},
		{"f func(own mut list[int64])", "mut"},
	}
	for _, tt := range tests {
		t.Run(tt.params, func(t *testing.T) {
			src := "package main\n\nfunc g(" + tt.params + ") {\n}\n"
			_, diags := parse(t, src)
			if len(diags) != 1 ||
				!strings.Contains(diags[0].Message, "cannot be both mut and own") {
				t.Fatalf("got %+v, want one error about mut and own", diags)
			}
			first := strings.IndexAny(src, "(") + 1
			want := strings.Index(src[first:], " "+tt.second+" ") + first + 1
			if strings.HasPrefix(tt.params, "f ") {
				inner := strings.Index(src, "func(") + len("func(")
				want = strings.Index(src[inner:], " "+tt.second+" ") + inner + 1
			}
			if diags[0].Span.Start != want {
				t.Errorf("reported at %d, want %d", diags[0].Span.Start, want)
			}
		})
	}
}

// TestRejectsOwnInterfaceReceiver checks that own before an interface's
// method name, which would take its receiver as own, is reported there.
func TestRejectsOwnInterfaceReceiver(t *testing.T) {
	src := "package main\n\ntype I interface {\n    own m()\n}\n"
	_, diags := parse(t, src)
	if len(diags) != 1 || !strings.Contains(diags[0].Message, "cannot take its receiver as own") {
		t.Fatalf("got %+v, want one error about an own receiver", diags)
	}
	if got := src[diags[0].Span.Start:diags[0].Span.End]; got != "own" {
		t.Errorf("reported at %q, want own", got)
	}
}

// ownSeeds are sources with own parameters and arguments, whole and cut
// off at the keyword, for FuzzParseOwn.
var ownSeeds = []string{
	"package main\n\nfunc f(own t Text, k int64) {\n    g(own t, own k.x, own a[i], own h())\n}\n",
	"package main\n\nfunc f(g func(own list[int64], mut T) int64) {\n}\n",
	"package main\n\ntype I interface {\n    mut take(own l list[int64]) error\n}\n",
	"package main\n\nfunc (own s S) m() {\n}\n",
	"package main\n\nfunc f(mut own l list[int64], own mut m list[T]) {\n}\n",
	"package main\n\nfunc f(own",
	"package main\n\nfunc main() {\n    f(own",
	"package main\n\nfunc main() {\n    x := own own own t\n}\n",
}

// FuzzParseOwn parses arbitrary source, seeded with own parameters and
// arguments, and checks the parser never panics, gives a tree exactly
// when it reports nothing, and reports each problem at a span inside the
// source.
func FuzzParseOwn(f *testing.F) {
	for _, src := range ownSeeds {
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
