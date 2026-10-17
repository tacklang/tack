package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// any is a reference to a value of some other type, known only at run
// time, and like a view it is second-class: it can only be written as the
// type of a parameter, a f(a any), or the element type of a variadic
// parameter, args ...any, or of a view parameter, v []any. A field, a
// global, a result and an array element cannot be one, and neither can a
// local whose type is written. An immutable local whose type is left out
// may hold one declared from an any (see anyLocal), and refers to what
// that any refers to; nothing that outlives the call can hold one, so no
// any outlives the call that made it, and the value it refers to, the
// caller's, lives for the whole call. A mut any is an error, a parameter
// or a local: C would have to check the type before every write, a
// function that changes the caller's value takes mut p T, and a mutable
// local could be assigned an any that does not live as long.
//
// A value passes to an any parameter as it is, with no conversion
// written: any value of one of the basic types, an untyped constant as an
// int64, an any passed on, or a view of one of the basic types, []uint8
// or []string. A view comes from a view parameter, a slice of a list, an
// array, a view or a string, or a call returning one, t.bytes(), and an
// any holding it refers to the view, and so to the caller's elements, for
// the call: what holds those elements is borrowed as a view argument's
// is, under the same rules (see checkListBorrows and checkViewBorrows). A
// list or an array is not viewed for an any, since no view type is
// asked for; a slice of it, l[:], is its view. Arrays, structs, lists and
// views of any other type cannot be passed as any in this version. The
// value is a copy taken at the call, as a plain argument's is, and a
// view's elements are as they were when the call began (see codegen/c).
//
// Inside a function, an any can be passed on, to another any parameter
// or as a value of an ...any, held in an immutable local, or asked which
// type it holds, with a type assertion or a type switch (assert.go);
// []any, a variadic ...any included, follows every view rule, so it has
// len, indexes and slices, and can be spread or passed to a []any
// parameter, and an element is an any like any other. Everything else, ==,
// an expression switch on one, a conversion to or from any, arithmetic,
// printing and a range over one, is reported: an any holds no type the
// checker knows. Each of those follows from any being a type of its own
// that no other type widens to and that supports no operator; only a
// mutable local declared from one, which would otherwise take any from
// its value, needs a rule of its own here.

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
// which becomes an int64, an any passed on, or a view of a basic type, a
// slice or a call returning one being allowed there as it is where a view
// parameter takes it. Anything else is reported, []uint8(s) where it is
// checked (see viewConversionElsewhere).
func (c *Checker) anyArg(arg ast.Expr) {
	c.allowView(arg)
	got := c.valueOf(arg, c.expr(arg))
	if got == types.Invalid || got == types.Any || c.table.InAny(got) {
		return
	}
	c.errorf(arg.Spanned(), "cannot pass %s as any: %s", c.typeName(got), anyHolds)
}

// anyHolds says which types an any can hold, for an error about one it
// cannot.
const anyHolds = "only the builtin types and views of them can be passed as any for now"

// anyLocal reports v, a local variable that takes its type from its value,
// when that type is any and v is mutable, and returns whether it was. An
// immutable any local, declared from an any, b := a, v, ok := args[i], a
// range's element or a type switch's binding, refers to the caller's
// value as the any it was declared from does, for no longer, since no any
// can be stored anywhere that outlives the call; a mutable one could be
// assigned an any that does not, so none may be declared. A blank, _,
// declares no variable, and is not reported.
func (c *Checker) anyLocal(v *ast.Var) bool {
	if v == nil || v.Type != types.Any || v.Name == ast.Blank || !v.Mutable {
		return false
	}
	c.errorf(
		v.NameSpan,
		"cannot declare mut %s as any; an any variable cannot be reassigned",
		v.Name,
	)
	v.Type = types.Invalid
	return true
}
