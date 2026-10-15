package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// any is a reference to a value of some other type, known only at run
// time, and like a view it is second-class: it can only be the type of a
// parameter, a f(a any), or the element type of a variadic parameter,
// args ...any, or of a view parameter, v []any. A local, a field, a
// global, a result and an array element cannot be one, so no any outlives
// the call that made it, and the value it refers to, the caller's, lives
// for the whole call. A mut any is an error: C would have to check the
// type before every write, and a function that changes the caller's value
// takes mut p T.
//
// A value passes to an any parameter as it is, with no conversion
// written: any value of one of the basic types, an untyped constant as an
// int64, or an any passed on. Arrays, structs and views cannot be passed
// as any in this version. The value is a copy taken at the call, as a
// plain argument's is (see codegen/c).
//
// Inside a function, an any can only be passed on, to another any
// parameter or as a value of an ...any; []any, a variadic ...any
// included, follows every view rule, so it has len, indexes and slices,
// and can be spread or passed to a []any parameter, but an element can
// only be passed on too. Everything else, ==, a switch on one, a
// conversion to or from any, arithmetic, printing, a range over one and
// declaring a variable of it, is reported: an any holds no type the
// checker knows, and the Tack-side operations that would ask which type it
// holds are not in this version. Each of those follows from any being a
// type of its own that no other type widens to and that supports no
// operator; only declaring a variable of it, which would otherwise take
// any from its value, needs a rule of its own here.

// anyMessage is the error for any written as the type of anything but a
// parameter.
const anyMessage = "any can only be the type of a parameter"

// paramElemType returns the type expr denotes as the type of a parameter,
// the element type of a variadic parameter, or the element type of a view
// parameter: as declaredType gives it, except that any is allowed there.
func (c *Checker) paramElemType(expr ast.Expr) types.Type {
	if isAnyName(expr) {
		return types.Any
	}
	return c.declaredType(expr)
}

// isAnyName reports whether expr is the type name any, unqualified.
func isAnyName(expr ast.Expr) bool {
	id, ok := expr.(*ast.Ident)
	if !ok || id.Pkg != "" || id.Def != nil {
		return false
	}
	typ, known := types.Lookup(id.Name)
	return known && typ == types.Any
}

// anyParam reports param, an any parameter, when it is mut, and returns
// its type: types.Any, or types.Invalid for a mut one.
func (c *Checker) anyParam(param *ast.Param) types.Type {
	if param.Mut {
		c.errorf(
			param.Span,
			"mut has no effect on an any parameter; take mut p T to change the caller's value",
		)
		return types.Invalid
	}
	return types.Any
}

// anyArg checks arg, passed to an any parameter, or as a value of an
// ...any one: a value of a basic type, an untyped constant,
// which becomes an int64, or an any passed on. Anything else is reported.
func (c *Checker) anyArg(arg ast.Expr) {
	got := c.valueOf(arg, c.expr(arg))
	if got == types.Invalid || got == types.Any || types.InAny(got) {
		return
	}
	c.errorf(
		arg.Spanned(),
		"cannot pass %s as any: only the builtin types can be passed as any for now",
		c.typeName(got),
	)
}

// anyLocal reports v, a local variable that takes its type from its value,
// when that type is any, which no variable can have, and returns whether
// it was. A blank, _, declares no variable, and is not reported.
func (c *Checker) anyLocal(v *ast.Var) bool {
	if v == nil || v.Type != types.Any || v.Name == ast.Blank {
		return false
	}
	c.errorf(v.NameSpan, "cannot declare %s as any; %s", v.Name, anyMessage)
	v.Type = types.Invalid
	return true
}
