package ast_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestMethodCallParts checks what a method call says of itself: its
// receiver, a plain value or a mut argument, every operand it passes,
// the receiver first, and its callee once one is recorded.
func TestMethodCallParts(t *testing.T) {
	recv := &ast.Ident{Name: "b"}
	arg := &ast.Ident{Name: "s"}
	method := &ast.Func{Name: "write", Method: true, Params: []*ast.Param{{Name: "b"}, {Name: "s"}}}
	call := &ast.Call{Fn: &ast.Selector{X: recv, Name: "write"}, Args: []ast.Expr{arg}}

	if call.Receiver() != recv {
		t.Errorf("receiver %v, want b", call.Receiver())
	}
	if ops := call.Operands(); len(ops) != 2 || ops[0] != recv || ops[1] != arg {
		t.Errorf("operands %v, want b then s", ops)
	}
	if ast.Callee(call) != nil {
		t.Error("an unchecked method call has a callee")
	}
	call.Method = method
	if ast.Callee(call) != method {
		t.Error("Callee is not the recorded method")
	}
	if method.Recv() != method.Params[0] {
		t.Error("Recv is not the first parameter")
	}

	mut := &ast.MutArg{X: recv}
	call.Fn = &ast.Selector{X: mut, Name: "write"}
	if call.Receiver() != mut {
		t.Errorf("receiver %v, want mut b", call.Receiver())
	}
}

// TestFunctionCallParts checks a call that is not a method call has no
// receiver, passes its arguments alone, and calls its name's function.
func TestFunctionCallParts(t *testing.T) {
	fn := &ast.Func{Name: "f", Params: []*ast.Param{{Name: "x"}}}
	arg := &ast.Ident{Name: "a"}
	call := &ast.Call{Fn: &ast.Ident{Name: "f", Def: fn}, Args: []ast.Expr{arg}}
	if call.Receiver() != nil || len(call.Operands()) != 1 || call.Operands()[0] != arg {
		t.Errorf("receiver %v, operands %v", call.Receiver(), call.Operands())
	}
	if ast.Callee(call) != fn || fn.Recv() != nil {
		t.Errorf("callee %v, receiver %v", ast.Callee(call), fn.Recv())
	}
	var builtin *ast.Func
	call.Fn = &ast.Ident{Name: "print", Def: builtin}
	if ast.Callee(call) != nil {
		t.Error("a builtin has a callee")
	}
}

// TestTypeDeclMethod checks a type finds its methods by name.
func TestTypeDeclMethod(t *testing.T) {
	m := &ast.Func{Name: "len", Method: true}
	td := &ast.TypeDecl{Name: "B", Methods: []*ast.Func{m}}
	if td.Method("len") != m || td.Method("cap") != nil {
		t.Errorf("Method(len) = %v, Method(cap) = %v", td.Method("len"), td.Method("cap"))
	}
}
