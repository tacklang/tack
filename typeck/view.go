package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// A view, []T, is a read-only view of elements of type T, and the type of
// a parameter only: a local, a field, a result, a global or an element of
// any of those cannot be one, and neither can a view's own element. A
// view parameter is never mut, so nothing in its function can assign it,
// an element of it, or pass either as mut, and it holds the same elements
// for the whole call: the caller passes an array that is a place, a
// variable or a field or element of one, which the backend copies first
// when the callee could otherwise see it change (see codegen/c), or a
// string's bytes, []uint8(s). An array that is not a place, a call's
// result or a literal, would need a temporary, and is an error.
//
// A view has len, an int64 with the facts a string's length has; an
// element, v[i], with an index proven in range or the checked form;
// range, whose index is proven for it; and being passed on to another
// view parameter of the same type. Nothing else: no comparison, no
// conversion, no printing, and no copy into a variable. Views of two
// element types are two types, and nothing widens into a view but an
// array of exactly its element type.

// paramType returns the type param is declared with: a view, when its
// type is written []T, which only a parameter may have, and never a mut
// one, or when it is variadic, args ...T (see variadicParam); any other
// type as declaredType gives it.
func (c *Checker) paramType(param *ast.Param) types.Type {
	if param.Variadic {
		return c.variadicParam(param)
	}
	vt, ok := param.TypeExpr.(*ast.ViewType)
	if !ok {
		return c.declaredType(param.TypeExpr)
	}
	typ := c.viewType(vt)
	if param.Mut && typ != types.Invalid {
		c.errorf(param.Span, "a view cannot be a mut parameter; views are read-only")
		return types.Invalid
	}
	return typ
}

// viewType returns the view type a written []T denotes, adding it to the
// table. Its element type may be any type but a view: one is reported, and
// gives types.Invalid, as does an element type that was reported.
func (c *Checker) viewType(vt *ast.ViewType) types.Type {
	if _, ok := vt.Elem.(*ast.ViewType); ok {
		c.errorf(vt.Span, "the element type of a view cannot be a view")
		return types.Invalid
	}
	elem := c.declaredType(vt.Elem)
	if elem == types.Invalid {
		return types.Invalid
	}
	return c.table.View(elem)
}

// viewElsewhere reports a view type written anywhere but as a parameter's
// type, and returns types.Invalid.
func (c *Checker) viewElsewhere(vt *ast.ViewType) types.Type {
	c.errorf(vt.Span, "a view can only be the type of a parameter")
	return types.Invalid
}

// isViewConversion reports whether call converts to a view type,
// []uint8(s).
func isViewConversion(call *ast.Call) bool {
	_, ok := call.Fn.(*ast.ViewType)
	return ok
}

// viewArg checks arg, passed to a plain parameter of the view type want in
// a call to callee: a conversion []uint8(s) of a string, a view of the
// same type, passed on, a slice of one or of an array, or an array of
// exactly its element type that is a place. A string needs the conversion, which a []uint8 parameter's error
// says, and any other value is used as want under the usual rules, which
// report it.
func (c *Checker) viewArg(arg ast.Expr, want types.Type, callee string) {
	if call, ok := arg.(*ast.Call); ok && isViewConversion(call) {
		c.use(arg, c.viewConversion(call), want, "in argument to "+callee)
		return
	}
	c.allowViewSlice(arg)
	got := c.valueOf(arg, c.expr(arg))
	switch {
	case got == types.Invalid:
	case got == types.String && c.table.Elem(want) == types.Uint8:
		text := c.text(arg.Spanned())
		c.errorf(
			arg.Spanned(),
			"cannot use string as %s in argument to %s; view its bytes with []uint8(%s)",
			c.typeName(want),
			callee,
			text,
		)
	case c.table.IsArray(got) && c.table.Elem(got) == c.table.Elem(want):
		if !c.isPlace(arg) {
			c.errorf(arg.Spanned(), "cannot view a temporary array; store it in a variable first")
		}
	default:
		c.use(arg, got, want, "in argument to "+callee)
	}
}

// isPlace reports whether e is a place a view can point into: a variable
// or a parameter, or a field or an element of one, to any depth.
func (c *Checker) isPlace(e ast.Expr) bool {
	root := ast.Root(e)
	if root == nil {
		return false
	}
	switch root.Def.(type) {
	case *ast.Var, *ast.Param:
		return true
	default:
		return false
	}
}

// viewConversion checks a conversion to a view type, []uint8(s), passed
// to a view parameter, records the view type on the call and returns it.
// It takes one argument, a string, and views its bytes, so only []uint8
// is a type it converts to; anything else is reported, and gives
// types.Invalid.
func (c *Checker) viewConversion(call *ast.Call) types.Type {
	vt, _ := call.Fn.(*ast.ViewType)
	to := c.viewType(vt)
	if !c.checkArity(call, c.text(vt.Span), 1) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return types.Invalid
	}
	from := c.valueOf(call.Args[0], c.expr(call.Args[0]))
	switch {
	case from == types.Invalid || to == types.Invalid:
		return types.Invalid
	case from == types.String && c.table.Elem(to) == types.Uint8:
		call.Conv = to
		return to
	}
	c.badViewConversion(call, from, to)
	return types.Invalid
}

// badViewConversion reports call, a conversion of a value of type from to
// the view type to, which is not []uint8(s) of a string, saying what to
// write instead where there is something.
func (c *Checker) badViewConversion(call *ast.Call, from, to types.Type) {
	switch {
	case from == types.String:
		c.errorf(call.Span, "cannot convert string to %s; a string's bytes are viewed as []uint8",
			c.typeName(to))
	case c.table.IsArray(from) && c.table.Elem(from) == c.table.Elem(to):
		c.errorf(call.Span, "cannot convert %s to %s; pass the array itself to a view parameter",
			c.typeName(from), c.typeName(to))
	default:
		c.errorf(call.Span, "cannot convert %s to %s", c.typeName(from), c.typeName(to))
	}
}

// viewConversionElsewhere reports a conversion to a view type used
// anywhere but as an argument to a view parameter, after checking it for
// errors of its own, and returns types.Invalid.
func (c *Checker) viewConversionElsewhere(call *ast.Call) types.Type {
	vt, _ := call.Fn.(*ast.ViewType)
	if c.viewConversion(call) != types.Invalid {
		text := c.text(vt.Span)
		c.errorf(call.Span, "%s(...) can only be passed to a %s parameter", text, text)
	}
	return types.Invalid
}

// viewRoot returns the name of the view parameter target is part of, the
// view itself or an element or a field of one, and whether it is one.
func (c *Checker) viewRoot(target ast.Expr) (*ast.Ident, bool) {
	root := ast.Root(target)
	if root == nil {
		return nil, false
	}
	p, ok := root.Def.(*ast.Param)
	return root, ok && c.table.IsView(p.Type)
}

// readOnly reports what, assigning to or passing as mut a part of the
// view root, at span: a view is read-only.
func (c *Checker) readOnly(span ast.Span, what string, root *ast.Ident) {
	c.errorf(span, "cannot %s: %s is a read-only view", what, root.Name)
}
