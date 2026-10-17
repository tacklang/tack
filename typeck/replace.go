package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// replace(mut place, new) moves the value of place out, as its result,
// and moves new into place, in one step, so the place never holds no
// value: old := replace(mut s.frames, empty) takes a list out of a
// struct, which nothing else can, since a field cannot be moved on its
// own (see move.go), and replace(mut s.frames, old) puts it back. place
// is anything a mut argument may name, and new is used as the place's
// type, under assignment's rules, and moved in when owned, as an
// appended value is (see move.go). The arguments are evaluated left to
// right, as any call's are: the place, with every index in it, first,
// then new, and only then is the old value read and the new one stored,
// so new may read the place's old value when it is copyable. Written as a
// statement, the call is an assignment of new to the place, which frees
// the old value when it is owned.

// isReplace reports whether call calls replace.
func isReplace(call *ast.Call) bool { return isBuiltinCall(call, "replace") }

// replaceCall checks a call to replace, records its result type, the
// place's, on the call and returns it. Its first argument is the place, a
// mut argument, checked as one passed to a mut parameter is, and its
// second the value moved into it, usable as the place's type. Anything
// else is reported, and gives types.Invalid.
func (c *Checker) replaceCall(call *ast.Call) types.Type {
	if !c.checkArity(call, "replace", 2) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return types.Invalid
	}
	before := len(c.diags)
	typ := c.replacedPlace(call.Args[0])
	if typ == types.Invalid {
		c.argExpr(call.Args[1])
		return types.Invalid
	}
	c.checkArg(call.Args[1], param{name: "new", typ: typ}, "replace")
	c.checkListBorrows(call)
	if len(c.diags) > before {
		return types.Invalid
	}
	call.Type = typ
	return typ
}

// replacedPlace checks arg, the place a call to replace takes, which must
// be passed as mut, mut place, and be a place, and returns its type,
// types.Invalid when anything was reported.
func (c *Checker) replacedPlace(arg ast.Expr) types.Type {
	m, isMut := arg.(*ast.MutArg)
	if !isMut {
		c.expr(arg)
		c.missingMut(arg, param{name: "place"}, "replace")
		return types.Invalid
	}
	c.checkErrorMut(m)
	return c.place(m.X, m)
}
