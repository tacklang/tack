package parser_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
)

// listSrc declares list types as a field, a parameter, a mut one, an
// element of an array and a result, and makes one with make and
// c_allocator, and writes a literal of one.
const listSrc = "package main\n\ntype S struct {\n    items list[int64]\n}\n\n" +
	"func f(a list[[2]Point], mut b list[S], c [2]list[bool]) list[int64] {\n" +
	"    l := make(list[int64], c_allocator)\n    m := list[int64]{1}\n    return l\n}\n"

// TestParsesListTypes checks that list[T] is a ListType wherever a type
// is written, spanning the keyword through the closing bracket and
// holding its element type as written.
func TestParsesListTypes(t *testing.T) {
	file, diags := parse(t, listSrc)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	fn := file.Decls[1].(*ast.Func)
	want := []string{"list[[2]Point]", "list[S]", "[2]list[bool]"}
	for i, p := range fn.Params {
		if got := listSrc[p.TypeExpr.Spanned().Start:p.TypeExpr.Spanned().End]; got != want[i] {
			t.Errorf("param %s's type spans %q, want %q", p.Name, got, want[i])
		}
	}
	if l, ok := fn.Params[0].TypeExpr.(*ast.ListType); !ok {
		t.Errorf("list[[2]Point] is %T, want *ast.ListType", fn.Params[0].TypeExpr)
	} else if _, ok := l.Elem.(*ast.ArrayType); !ok {
		t.Errorf("list[[2]Point]'s element is %T, want *ast.ArrayType", l.Elem)
	}
	if a, ok := fn.Params[2].TypeExpr.(*ast.ArrayType); !ok {
		t.Errorf("[2]list[bool] is %T, want *ast.ArrayType", fn.Params[2].TypeExpr)
	} else if _, ok := a.Elem.(*ast.ListType); !ok {
		t.Errorf("[2]list[bool]'s element is %T, want *ast.ListType", a.Elem)
	}
	if _, ok := fn.Results[0].(*ast.ListType); !ok {
		t.Errorf("the result is %T, want *ast.ListType", fn.Results[0])
	}
	st := file.Decls[0].(*ast.TypeDecl).TypeExpr.(*ast.StructType)
	if _, ok := st.Fields[0].TypeExpr.(*ast.ListType); !ok {
		t.Errorf("the field's type is %T, want *ast.ListType", st.Fields[0].TypeExpr)
	}
}

// TestParsesMakeAndCAllocator checks that make's first argument is a
// ListType, its second a CAllocatorLit, and that list[T]{...} is a
// composite literal of the list type, which the type checker reports.
func TestParsesMakeAndCAllocator(t *testing.T) {
	file, diags := parse(t, listSrc)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	body := file.Decls[1].(*ast.Func).Body
	call, isCall := body[0].(*ast.Var).Value.(*ast.Call)
	if !isCall || len(call.Args) != 2 {
		t.Fatalf("make's call is %T, want a call of two arguments", body[0].(*ast.Var).Value)
	}
	if _, isType := call.Args[0].(*ast.ListType); !isType {
		t.Errorf("make's first argument is %T, want *ast.ListType", call.Args[0])
	}
	if alloc, isAlloc := call.Args[1].(*ast.CAllocatorLit); !isAlloc {
		t.Errorf("make's second argument is %T, want *ast.CAllocatorLit", call.Args[1])
	} else if got := listSrc[alloc.Span.Start:alloc.Span.End]; got != "c_allocator" {
		t.Errorf("c_allocator spans %q", got)
	}
	lit, isLit := body[1].(*ast.Var).Value.(*ast.CompositeLit)
	if !isLit {
		t.Fatalf("list[int64]{1} is %T, want *ast.CompositeLit", body[1].(*ast.Var).Value)
	}
	if _, isType := lit.TypeExpr.(*ast.ListType); !isType {
		t.Errorf("the literal's type is %T, want *ast.ListType", lit.TypeExpr)
	}
}

// TestListNeedsBrackets checks list without its brackets, or with no
// element type, is a parse error, not a crash.
func TestListNeedsBrackets(t *testing.T) {
	for _, src := range []string{
		"package main\n\nfunc f(a list) {\n}\n",
		"package main\n\nfunc f(a list[]) {\n}\n",
		"package main\n\nfunc f(a list[int64) {\n}\n",
		"package main\n\nfunc main() {\n    list := 3\n}\n",
	} {
		if _, diags := parse(t, src); len(diags) == 0 {
			t.Errorf("%q parsed without a diagnostic", src)
		}
	}
}
