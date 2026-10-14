package parser_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
)

// viewSrc declares a function whose parameters and result have view
// types, nested in an array type or holding one, and a mut one.
const viewSrc = "package main\n\nfunc f(a []int64, b [][3]Point, c [2][]bool, mut d []uint8) []int64 {\n}\n"

// TestParsesViewTypes checks that []T is a ViewType wherever a type is
// written, spanning the brackets through its element, and that a view
// parameter can be mut, which the type checker reports, not the parser.
func TestParsesViewTypes(t *testing.T) {
	file, diags := parse(t, viewSrc)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	fn := file.Decls[0].(*ast.Func)
	want := []string{"[]int64", "[][3]Point", "[2][]bool", "[]uint8"}
	for i, p := range fn.Params {
		if got := viewSrc[p.TypeExpr.Spanned().Start:p.TypeExpr.Spanned().End]; got != want[i] {
			t.Errorf("param %s's type spans %q, want %q", p.Name, got, want[i])
		}
	}
	if !fn.Params[3].Mut {
		t.Error("mut d []uint8 is not mut")
	}
	if _, ok := fn.Results[0].(*ast.ViewType); !ok {
		t.Errorf("the result is %T, want *ast.ViewType", fn.Results[0])
	}
}

// TestParsesNestedViewTypes checks a view of arrays and an array of views
// nest as written.
func TestParsesNestedViewTypes(t *testing.T) {
	file, diags := parse(t, viewSrc)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	fn := file.Decls[0].(*ast.Func)
	if v, ok := fn.Params[1].TypeExpr.(*ast.ViewType); !ok {
		t.Errorf("[][3]Point is %T, want *ast.ViewType", fn.Params[1].TypeExpr)
	} else if _, ok := v.Elem.(*ast.ArrayType); !ok {
		t.Errorf("[][3]Point's element is %T, want *ast.ArrayType", v.Elem)
	}
	if a, ok := fn.Params[2].TypeExpr.(*ast.ArrayType); !ok || a.Len == nil {
		t.Errorf("[2][]bool is %T, want an *ast.ArrayType with a length", fn.Params[2].TypeExpr)
	} else if _, ok := a.Elem.(*ast.ViewType); !ok {
		t.Errorf("[2][]bool's element is %T, want *ast.ViewType", a.Elem)
	}
}

// TestParsesViewConversions checks that []T(x) in an expression is a
// Call whose Fn is the ViewType, spanning the type through the closing
// parenthesis, with its arguments as any call's, and that []T{...} is a
// composite literal of the view type, which the type checker reports.
func TestParsesViewConversions(t *testing.T) {
	body := mainBody(t, "    f([]uint8(s), []uint8(g(1), 2))\n    x := []int64{1, 2}")
	call := body[0].(*ast.ExprStmt).X.(*ast.Call)
	for i, nargs := range []int{1, 2} {
		conv, ok := call.Args[i].(*ast.Call)
		if !ok {
			t.Fatalf("argument %d is %T, want *ast.Call", i, call.Args[i])
		}
		if _, ok := conv.Fn.(*ast.ViewType); !ok {
			t.Errorf("argument %d calls %T, want *ast.ViewType", i, conv.Fn)
		}
		if len(conv.Args) != nargs {
			t.Errorf("argument %d has %d arguments, want %d", i, len(conv.Args), nargs)
		}
		if conv.Span.Start != conv.Fn.Spanned().Start {
			t.Errorf("argument %d's span does not start at its type", i)
		}
	}
	lit, ok := body[1].(*ast.Var).Value.(*ast.CompositeLit)
	if !ok {
		t.Fatalf("[]int64{1, 2} is %T, want *ast.CompositeLit", body[1].(*ast.Var).Value)
	}
	if _, ok := lit.TypeExpr.(*ast.ViewType); !ok || len(lit.Elems) != 2 {
		t.Errorf("[]int64{1, 2} has type %T and %d elements", lit.TypeExpr, len(lit.Elems))
	}
}
