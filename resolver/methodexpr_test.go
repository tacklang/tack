package resolver_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
)

// errorsOf returns the messages of the errors among diags, leaving out
// the warnings of what the programs here declare and do not use.
func errorsOf(diags []diag.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if d.Severity == diag.Error {
			out = append(out, d.Message)
		}
	}
	return out
}

// methodExprProgram declares a struct P with a method m, an interface I
// and a generic function h, and a function f whose body is lines, one
// statement each.
func methodExprProgram(lines ...string) string {
	src := "package main\n\ntype P struct {\n    x int64\n}\n\n" +
		"func (p P) m() int64 {\n    return p.x\n}\n\n" +
		"type I interface {\n    m() int64\n}\n\n" +
		"func h[W I](w W) {\n    f := W.m\n}\n\n" +
		"func f() {\n"
	for _, l := range lines {
		src += "    " + l + "\n"
	}
	return src + "}\n\nfunc main() {\n}\n"
}

// TestBindsMethodExprOperands checks the operand of a selector through a
// type is bound to the type's declaration, a struct's, an interface's or
// a type parameter's, or, for a basic type, to nothing, with nothing
// reported, and that a call through a type has no receiver: whether the
// type has the method is the type checker's to say.
func TestBindsMethodExprOperands(t *testing.T) {
	file, diags := resolveTree(t, methodExprProgram(
		"a := P.m",
		"b := P.m(P{x: 1})",
		"c := I.m",
		"d := int64.abs",
		"e := P.nope",
	))
	if errs := errorsOf(diags); len(errs) > 0 {
		t.Fatalf("resolve reported %q", errs)
	}
	strct := file.Decls[0].(*ast.TypeDecl)
	iface := file.Decls[2].(*ast.TypeDecl)
	h := funcNamed(file, "h")
	fn := funcNamed(file, "f")
	value := func(i int) ast.Expr { return fn.Body[i].(*ast.Var).Value }
	operand := func(e ast.Expr) *ast.Ident {
		if call, ok := e.(*ast.Call); ok {
			e = call.Fn
		}
		return e.(*ast.Selector).X.(*ast.Ident)
	}
	for i, want := range []ast.Decl{strct, strct, iface, nil, strct} {
		got := operand(value(i))
		if got.Def != want {
			t.Errorf("statement %d: %s bound to %v, want %v", i, got.Name, got.Def, want)
		}
		if _, ok := ast.MethodExpr(value(i)); !ok {
			if _, isCall := value(i).(*ast.Call); !isCall {
				t.Errorf("statement %d is no method expression", i)
			}
		}
	}
	if call := value(1).(*ast.Call); call.Receiver() != nil {
		t.Errorf("P.m(P{x: 1}) has receiver %v", call.Receiver())
	}
	inH := operand(h.Body[0].(*ast.Var).Value)
	if inH.Def != h.TypeParams[0] {
		t.Errorf("W bound to %v, want the type parameter", inH.Def)
	}
}

// TestSelectorThroughValue checks a selector whose operand names a
// variable is a field's, or a method call's, which has a receiver, and no
// method expression.
func TestSelectorThroughValue(t *testing.T) {
	file, diags := resolveTree(t, methodExprProgram("q := P{x: 1}", "r := q.m()", "g := q.x"))
	if errs := errorsOf(diags); len(errs) > 0 {
		t.Fatalf("resolve reported %q", errs)
	}
	fn := funcNamed(file, "f")
	call := fn.Body[1].(*ast.Var).Value.(*ast.Call)
	if call.Receiver() == nil {
		t.Errorf("q.m() has no receiver")
	}
	if _, ok := ast.MethodExpr(fn.Body[2].(*ast.Var).Value); ok {
		t.Errorf("q.x read as a method expression")
	}
}
