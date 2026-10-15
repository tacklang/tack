package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// A variadic parameter, args ...T, is a function's last parameter, the
// parser makes sure, and inside the function it is a view, []T, under
// every rule a view parameter follows: read-only, never mut, len, indexes,
// range, slices, passed on. So its element type cannot be a view, and the
// parameter cannot be mut.
//
// A call passes it zero or more values after the fixed parameters' ones,
// each checked against T as an argument to a plain parameter of type T
// is: an untyped constant takes T, a narrower integer widens to it, and a
// mut argument, a view or anything else T does not accept is reported.
// Or it passes a view on whole, with a spread, f(a, v...), whose v is
// checked as an argument to a view parameter of type []T is: a view of
// that type, an array place of exactly T, a slice of one, or []uint8(s)
// for a ...uint8. A spread is the whole variadic, so it follows exactly
// one argument per fixed parameter. A spread in a call to a function or
// builtin that is not variadic, or in a conversion, is reported.
//
// The values are plain arguments: each is a copy taken when the call
// evaluates it, so one that reads a place another argument passes as mut
// is not an error, as for any plain argument (see exclusive.go). A spread
// is a view argument, copied as one when it may overlap a mut argument
// (see codegen/c).

// variadicParam returns the type of param, a variadic parameter, args
// ...T: the view type []T, T any included. A mut variadic, and one whose
// element type is a view, are reported, and give types.Invalid, as does
// an element type that was reported.
func (c *Checker) variadicParam(param *ast.Param) types.Type {
	if _, ok := param.TypeExpr.(*ast.ViewType); ok {
		c.errorf(
			param.TypeExpr.Spanned(),
			"the element type of a variadic parameter cannot be a view",
		)
		return types.Invalid
	}
	elem := c.paramElemType(param.TypeExpr)
	if elem == types.Invalid {
		return types.Invalid
	}
	if param.Mut {
		c.errorf(param.Span, "a variadic parameter cannot be mut; it is a read-only view")
		return types.Invalid
	}
	return c.table.View(elem)
}

// isVariadic reports whether params, a callee's, ends in a variadic
// parameter.
func isVariadic(params []param) bool {
	return len(params) > 0 && params[len(params)-1].variadic
}

// checkVariadicCall checks a call to callee, whose parameters params end
// in a variadic one: one argument for each fixed parameter, then either
// any number of values for the variadic, or a spread alone. It returns
// the call's mut arguments passed to mut parameters with nothing
// reported, for the exclusivity check.
func (c *Checker) checkVariadicCall(call *ast.Call, callee string, params []param) []*ast.MutArg {
	fixed, last := params[:len(params)-1], params[len(params)-1]
	if !c.checkVariadicArity(call, callee, fixed, last) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return nil
	}
	var muts []*ast.MutArg
	for i, arg := range call.Args[:len(fixed)] {
		if m := c.checkArg(arg, fixed[i], callee); m != nil {
			muts = append(muts, m)
		}
	}
	if call.Spread {
		c.checkArg(call.Args[len(fixed)], last, callee)
		return muts
	}
	value := param{name: last.name, typ: types.Invalid}
	if last.typ != types.Invalid {
		value.typ = c.table.Elem(last.typ)
	}
	for _, arg := range call.Args[len(fixed):] {
		c.checkArg(arg, value, callee)
	}
	return muts
}

// checkVariadicArity reports a call to callee passing too few arguments
// for its fixed parameters, fixed, or a spread after more than one
// argument per fixed parameter, which would pass values for the variadic
// parameter last both one by one and as a view. It returns whether the
// count was right.
func (c *Checker) checkVariadicArity(
	call *ast.Call,
	callee string,
	fixed []param,
	last param,
) bool {
	n := len(call.Args)
	switch {
	case !call.Spread && n < len(fixed):
		c.errorf(
			call.Spanned(),
			"%s expects at least %d argument(s), got %d",
			callee,
			len(fixed),
			n,
		)
		return false
	case call.Spread && n-1 < len(fixed):
		c.errorf(call.Spanned(), "%s expects %d argument(s) before %s..., got %d",
			callee, len(fixed), c.text(call.Args[n-1].Spanned()), n-1)
		return false
	case call.Spread && n-1 > len(fixed):
		extra := ast.Span{
			File:  call.Args[len(fixed)].Spanned().File,
			Start: call.Args[len(fixed)].Spanned().Start,
			End:   call.EllipsisSpan.End,
		}
		c.errorf(extra, "cannot use ... in a call with other values for %s", last.name)
		return false
	}
	return true
}

// spreadElsewhere reports the spread of call, which calls or converts
// with name, nothing variadic, and returns whether there was one: a
// conversion, T(x...) or []uint8(s...), or a call to a function or
// builtin with no variadic parameter.
func (c *Checker) spreadElsewhere(call *ast.Call, name string) bool {
	if !call.Spread {
		return false
	}
	if call.Conv != types.Invalid || isViewConversion(call) {
		c.errorf(call.EllipsisSpan, "cannot use ... in a conversion to %s", name)
	} else {
		c.errorf(call.EllipsisSpan, "cannot use ... in a call to non-variadic %s", name)
	}
	return true
}
