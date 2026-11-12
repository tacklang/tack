package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// drop(own x) frees the value of x, a local variable or an own parameter
// of a type that owns something, at once, as the end of its block would,
// and leaves x moved from. To the checker it is exactly a move of x into
// a call that frees it: the operand is written own x, as every move into
// a call is, and the move check (see move.go) follows it as one, so a
// later use is a use after a move, a drop in a loop that the next
// iteration reaches again, a drop of a variable a defer reads or of one
// declared outside deferred code or captured by a closure, and a drop of
// a list being ranged over or of a binding that refers into one, are
// each the error a move would be. A move is no write, so the facts about
// x need no rule of their own: x cannot be read again until it is
// assigned, and the assignment ends them.
//
// What can be dropped follows from that: a field or an element cannot,
// since neither can be moved out of what holds it, and replace, pop,
// remove and delete take one out; a plain or a mut parameter cannot,
// since neither owns its value; and a value no variable holds cannot, as
// only a variable is written after own. A value whose type owns nothing,
// a copyable one or a view, is reported, since there would be nothing to
// free; in a generic function a value of a type parameter owns something
// whatever its type argument, so its drop is allowed, and an instance
// whose type argument owns nothing drops nothing (see codegen/c). drop
// gives no value, and is only ever a call statement.

// isDrop reports whether call calls drop.
func isDrop(call *ast.Call) bool { return isBuiltinCall(call, "drop") }

// dropCall checks a call to drop, drop(own x): its one argument must be
// own before a variable that owns something (see dropOperand), and a
// call anywhere but as a statement is reported, since it gives no value.
// An operand that can be dropped but is written without own, or with mut,
// is reported with the own it needs. It returns types.Invalid, the type
// of nothing.
func (c *Checker) dropCall(call *ast.Call) types.Type {
	if !c.checkArity(call, "drop", 1) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return types.Invalid
	}
	arg := call.Args[0]
	x := arg
	_, isOwn := arg.(*ast.OwnArg)
	switch a := arg.(type) {
	case *ast.OwnArg:
		x = a.X
	case *ast.MutArg:
		x = a.X
	}
	if !c.dropOperand(x) {
		return types.Invalid
	}
	if !isOwn {
		c.errorf(arg.Spanned(), "drop takes x as own; pass own %s", c.text(x.Spanned()))
		return types.Invalid
	}
	if c.droppedCall != call {
		c.errorf(call.Span, "drop returns no value")
		return types.Invalid
	}
	return types.Invalid
}

// dropOperand checks x, what a call to drop is given, and reports
// whether it can be dropped: the name of a local variable or a parameter
// whose type owns something. Anything else is reported, after it is
// checked for errors of its own, unless it has one, which the drop
// follows from: a value whose type owns nothing, a view, a field, an
// element, a method named through its type, a union's case built
// through its type, and any value no variable holds. Whether a parameter
// owns its value, and whether the variable may be moved where the drop
// is, are the move check's (see move.go).
func (c *Checker) dropOperand(x ast.Expr) bool {
	text := c.text(x.Spanned())
	if !droppable(x) {
		if c.soundOperand(x) {
			c.errorf(x.Spanned(), "cannot drop %s: only a variable can be dropped", text)
		}
		return false
	}
	before := len(c.diags)
	typ := c.expr(x)
	if typ == types.Invalid || len(c.diags) > before {
		return false
	}
	switch {
	case c.table.IsView(typ):
		c.errorf(x.Spanned(), "cannot drop %s: a view only borrows what it points into", text)
	case !c.table.Owned(typ):
		c.errorf(x.Spanned(),
			"cannot drop %s: %s owns no list or map, so there is nothing to free; remove the drop",
			text, c.typeName(typ))
	default:
		return c.dropPlace(x)
	}
	return false
}

// droppable reports whether x is written as a drop's operand may be: the
// name of a local variable or a parameter, or a field or an element,
// which dropPlace reports once its type is known to own something. A
// method named through its type, a union's case built through its type
// and any other expression are values no variable holds.
func droppable(x ast.Expr) bool {
	if _, isMethodExpr := ast.MethodExpr(x); isMethodExpr {
		return false
	}
	if _, _, _, built := ast.Construction(x); built {
		return false
	}
	switch x := x.(type) {
	case *ast.Ident:
		switch x.Def.(type) {
		case *ast.Var, *ast.Param:
			return true
		}
	case *ast.Selector, *ast.Index:
		return true
	}
	return false
}

// givesNoValue reports whether call calls swap or drop, the generic
// builtins that give no value, each reporting a use of it as one unless
// it is a statement's call.
func givesNoValue(call *ast.Call) bool { return isSwap(call) || isDrop(call) }

// dropPlace reports x, the operand of a drop, of a type that owns
// something, when it is a field or an element, which cannot be moved out
// of what holds it, with what takes one out, and reports whether it is
// a variable's name instead.
func (c *Checker) dropPlace(x ast.Expr) bool {
	text := c.text(x.Spanned())
	switch x := x.(type) {
	case *ast.Selector:
		c.errorf(x.Span,
			"cannot drop %s: a field cannot be moved on its own; take it out with replace", text)
		return false
	case *ast.Index:
		what, holder := "an element", "a list; take it out with pop or remove"
		switch typ := ast.TypeOf(x.X); {
		case c.table.IsView(typ):
			holder = "a view, which only borrows it"
		case c.table.IsMap(typ):
			what, holder = "a value", "a map; take it out with delete"
		case c.table.IsArena(typ):
			holder = "an arena"
		case c.table.IsPool(typ):
			holder = "a pool; take it out with remove"
		}
		c.errorf(x.Span, "cannot drop %s: %s cannot be moved out of %s", text, what, holder)
		return false
	}
	return true
}
