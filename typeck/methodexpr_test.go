package typeck_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
)

// methodExprSource names P's methods through P, as values and called:
// a plain receiver, a mut one, and a method whose own parameter is
// scope.
const methodExprSource = `package main

type P struct {
    a int64
}

func (p P) get(x int64) int64 {
    return p.a
}

func (mut p P) set(x int64) {
    p.a = x
}

func (p P) run(scope f func()) {
    f()
}

func main() {
    g := P.get
    s := P.set
    r := P.run
    mut p := P{a: 1}
    s(mut p, 2)
    r(p, func() {})
    if g(p, 1) == P.get(p, 2) {
        print("same")
    }
}
`

// TestRecordsMethodExprs checks a method expression used as a value is
// given its method's signature as a function type, the receiver first,
// mut and scope marks kept, and records the method on its selector, and
// that one called records the method on the call, with no receiver and
// every argument passed, the receiver first.
func TestRecordsMethodExprs(t *testing.T) {
	file, diags := checkFile(t, methodExprSource)
	if len(diags) > 0 {
		t.Fatalf("check reported %v", diags)
	}
	var main *ast.Func
	methods := map[string]*ast.Func{}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.Func); ok {
			if fn.Method {
				methods[fn.Name] = fn
			} else {
				main = fn
			}
		}
	}
	for i, want := range []struct{ method, typ string }{
		{"get", "func(P, int64) int64"},
		{"set", "func(mut P, int64)"},
		{"run", "func(P, scope func())"},
	} {
		sel := main.Body[i].(*ast.Var).Value.(*ast.Selector)
		if sel.Method != methods[want.method] {
			t.Errorf("P.%s records method %v", want.method, sel.Method)
		}
		if got := file.Types.Name(sel.Type); got != want.typ {
			t.Errorf("P.%s has type %s, want %s", want.method, got, want.typ)
		}
	}
	cond := main.Body[6].(*ast.If).Cond.(*ast.Binary)
	call := cond.Y.(*ast.Call)
	if call.Method != methods["get"] || call.Receiver() != nil || call.Dynamic != nil {
		t.Errorf("P.get(p, 2) records method %v, receiver %v, dynamic %v",
			call.Method, call.Receiver(), call.Dynamic)
	}
	if ast.Callee(call) != methods["get"] || len(call.Operands()) != 2 {
		t.Errorf("P.get(p, 2) calls %v with %d operands", ast.Callee(call), len(call.Operands()))
	}
}
