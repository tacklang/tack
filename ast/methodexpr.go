package ast

import "github.com/tacklang/tack/types"

// TypeOperand reports whether x, the operand of a selector, names a type
// rather than a value: a name, plain or qualified, the resolver bound to
// a type's declaration, a struct, a union or an interface, or to a type
// parameter, or a basic type's name, int64 or string, which nothing can
// declare or shadow and so binds to nothing. A selector through a type
// builds a union's case (see Construction) or names a method (see
// MethodExpr); it has no receiver.
func TypeOperand(x Expr) bool {
	id, ok := x.(*Ident)
	if !ok {
		return false
	}
	switch d := id.Def.(type) {
	case *TypeDecl:
		return d != nil
	case *TypeParam:
		return d != nil
	case nil:
		_, basic := types.Lookup(id.Name)
		return basic && id.Pkg == ""
	default:
		return false
	}
}

// MethodExpr reports whether e is a method expression and returns its
// selector: a selector whose operand names a type (see TypeOperand) and
// which builds no union's case (see Construction), Pair.first or
// lexer.Token.Kind. It names the method of that name of the type as a
// function value whose first parameter is the method's receiver; the
// type checker reports a type that has no method of that name, and one
// whose methods are not values, an interface or a type parameter. A call
// whose callee is one, Pair.first(p), calls the method, passing its
// receiver first among its arguments.
func MethodExpr(e Expr) (*Selector, bool) {
	sel, ok := e.(*Selector)
	if !ok || !TypeOperand(sel.X) {
		return nil, false
	}
	if _, _, _, built := Construction(sel); built {
		return nil, false
	}
	return sel, true
}
