package typeck_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/typeck"
	"github.com/tacklang/tack/types"
)

// checkedFile runs the front end on src up to and including type
// checking, as check does, and returns the checked file with every
// diagnostic the type checker reported, warnings included.
func checkedFile(t *testing.T, src string) (*ast.File, []diag.Diagnostic) {
	t.Helper()
	f := &source.File{ID: source.FileID("test"), Filename: "case.tck", Data: []byte(src)}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatalf("lex: %s", diag.RenderAllPretty(f, diags))
	}
	file, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
	if len(diags) > 0 {
		t.Fatalf("parse: %s", diag.RenderAllPretty(f, diags))
	}
	if diags := resolver.New(ast.SingleFile(file), stdtest.MustBuiltins()).Resolve(); len(diags) > 0 {
		t.Fatalf("resolve: %s", diag.RenderAllPretty(f, diags))
	}
	return file, typeck.New(ast.SingleFile(file), f, stdtest.MustBuiltins()).Check()
}

// paramSwitchBody is a generic function whose switch on its type
// parameter has an error in its int64 case, an int64 added to a bool, and
// one in its default, a T returned as an int64, and whose int64 case
// calls h, with the calls %CALLS% in main.
const paramSwitchBody = `package main

type A struct {
    x int64
}

func h[U Data](u U) int64 {
    return 1
}

func g[T Data](v T) int64 {
    switch T {
    case int64:
        return h(v) +% (v + true)
    case list[E]:
        return len(v)
    default:
        return v
    }
}

func main() {
%CALLS%
}
`

// TestParamSwitchCheckedOnce checks a switch on a type parameter is
// checked once, whatever calls bind its type parameter to: the same
// errors, an int64 added to a bool in the case narrowed to int64 and a T
// returned as an int64 in the default, with no call, with calls binding
// each case's type, and with several.
func TestParamSwitchCheckedOnce(t *testing.T) {
	var want []string
	for _, calls := range []string{
		"",
		"    g(1)",
		"    g(A{x: 1})\n    g(true)\n    l := make(list[int64], c_allocator)\n    g(l)\n    g(5)",
	} {
		_, diags := checkedFile(t, strings.Replace(paramSwitchBody, "%CALLS%", calls, 1))
		got := messages(errorsOf(diags))
		if want == nil {
			want = got
			if len(want) != 2 || !strings.Contains(want[0], "operator + is not defined") ||
				!strings.Contains(want[1], "cannot use T as int64") {
				t.Fatalf("with no call: %v", want)
			}
			continue
		}
		if !slices.Equal(got, want) {
			t.Errorf("with calls %q: %v, want %v", calls, got, want)
		}
	}
}

// narrowingSrc narrows v, a parameter, and w, a local of type T, in a
// case of int64 and one of list[E], and reads both outside the switch.
const narrowingSrc = `package main

func h[U Data](u U) int64 {
    return 1
}

func g[T Data](v T, w T) int64 {
    switch T {
    case int64:
        return v +% w
    case list[E]:
        return len(v) +% len(w)
    case bool, int8:
        return h(v)
    default:
        return h(w)
    }
}

func main() {
    if g(1, 2) == 3 && g(true, false) == 1 {
        print("ok")
    }
}
`

// TestParamSwitchNarrowsNames checks a case naming one type records it,
// and a list of the case's own type parameter for list[E], as the type of
// every name of a value of type T in it, and a case naming several
// types, the default and the code outside the switch record none; that
// each case records how its values match; that a switch with a default
// is not marked exhaustive; and that only the instances the cases g's
// calls match reach are found: h for bool, from the case of bool and
// int8, and none for int64, whose case calls no h.
func TestParamSwitchNarrowsNames(t *testing.T) {
	file, diags := checkedFile(t, narrowingSrc)
	if errs := errorsOf(diags); len(errs) > 0 {
		t.Fatalf("check reported %v", messages(errs))
	}
	g := file.Decls[1].(*ast.Func)
	sw := g.Body[0].(*ast.Switch)
	if sw.Exhaustive {
		t.Error("a switch with a default is marked exhaustive")
	}
	narrowed := func(cl *ast.CaseClause) []types.Type {
		var got []types.Type
		for _, stmt := range cl.Body {
			ast.Walk(stmt, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && (id.Name == "v" || id.Name == "w") {
					got = append(got, id.Narrowed)
				}
				return true
			})
		}
		return got
	}
	if got := narrowed(sw.Cases[0]); !slices.Equal(got, []types.Type{types.Int64, types.Int64}) {
		t.Errorf("case int64 narrows v and w to %v, want int64", got)
	}
	lists := narrowed(sw.Cases[1])
	if len(lists) != 2 || lists[0] != lists[1] || !file.Types.IsList(lists[0]) ||
		!file.Types.IsTypeParam(file.Types.Elem(lists[0])) {
		t.Errorf("case list[E] narrows v and w to %v, want list[E]", lists)
	}
	for _, cl := range sw.Cases[2:] {
		if got := narrowed(cl); !slices.Equal(got, []types.Type{types.Invalid}) {
			t.Errorf("case at %v narrows to %v, want nothing", cl.Span, got)
		}
	}
	kinds := []ast.TypeCaseKind{}
	for _, cl := range sw.Cases {
		for _, tc := range cl.TypeCases {
			kinds = append(kinds, tc.Kind)
		}
	}
	want := []ast.TypeCaseKind{ast.TypeExact, ast.TypeList, ast.TypeExact, ast.TypeExact}
	if !slices.Equal(kinds, want) {
		t.Errorf("cases match as %v, want %v", kinds, want)
	}
	var found []string
	for _, inst := range file.Instances {
		if inst.Func.Name == "h" {
			found = append(found, file.Types.Name(inst.TypeArgs[0]))
		}
	}
	if !slices.Equal(found, []string{"bool"}) {
		t.Errorf("instances of h: %v, want [bool]", found)
	}
}
