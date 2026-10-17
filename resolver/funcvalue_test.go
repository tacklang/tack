package resolver_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
)

// funcValueSrc names a function as a value, calls through a parameter and
// a local holding one, and calls a function whose name a parameter
// shares.
const funcValueSrc = "package main\n\nfunc double(x int64) int64 {\n    return x * 2\n}\n\n" +
	"func apply(g func(int64) int64, double int64) int64 {\n    h := g\n    return h(double(g(1)))\n}\n\n" +
	"func main() {\n    f := double\n    exit(int32(apply(f, 1)))\n}\n"

// TestBindsFuncValues checks that a function's name used as a value is
// bound to the function, that a call through a parameter or a local binds
// its callee to them, and that a call of a function whose name a
// parameter shares binds to the function, the parameter recorded as
// hidden.
func TestBindsFuncValues(t *testing.T) {
	file, diags := resolveTree(t, funcValueSrc)
	if len(diags) > 0 {
		t.Fatalf("resolve reported %+v", diags)
	}
	double := file.Decls[0].(*ast.Func)
	apply := file.Decls[1].(*ast.Func)
	main := file.Decls[2].(*ast.Func)
	if f := main.Body[0].(*ast.Var).Value.(*ast.Ident); f.Def != double {
		t.Errorf("double used as a value is bound to %v, want the function", f.Def)
	}
	h := apply.Body[0].(*ast.Var)
	outer := apply.Body[1].(*ast.Return).Values[0].(*ast.Call)
	if id := outer.Fn.(*ast.Ident); id.Def != h {
		t.Errorf("h(...) calls %v, want the local h", id.Def)
	}
	inner := outer.Args[0].(*ast.Call)
	if id := inner.Fn.(*ast.Ident); id.Def != double || id.Hidden != apply.Params[1] {
		t.Errorf("double(...) calls %v, hiding %v; want the function, hiding the parameter",
			id.Def, id.Hidden)
	}
	if id := inner.Args[0].(*ast.Call).Fn.(*ast.Ident); id.Def != apply.Params[0] {
		t.Errorf("g(...) calls %v, want the parameter g", id.Def)
	}
}

// TestRejectsBuiltinValue checks that a builtin's name is no value.
func TestRejectsBuiltinValue(t *testing.T) {
	diags := resolve(t, inMain("    f := print\n    f(\"x\")"))
	want := "cannot use the builtin print as a value; only a declared function can be one"
	if len(diags) == 0 || diags[0].Message != want {
		t.Fatalf("got %v, want %q first", messagesOf(diags), want)
	}
}
