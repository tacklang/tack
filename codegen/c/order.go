package c

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tacklang/tack/ast"
)

// sequenced returns the C expression build makes of operands, already
// rendered as rendered, evaluated left to right. C leaves the order of a
// call's arguments, an operator's operands and an initializer's values
// unspecified, so where the order could show, every operand that is not
// pure is assigned to a temporary of its own type first, in order, with
// C's comma operator, and build is given the temporaries in its place.
// The order shows when at least two operands are not pure and one of them
// calls a function. The temporaries are declared at the top of the
// function (see funcFrame), so this works inside any expression, a loop's
// condition and the right of && included.
func (g *generator) sequenced(
	operands []ast.Expr,
	rendered []string,
	build func([]string) string,
) string {
	if !g.orderShows(operands) {
		return build(rendered)
	}
	args := slices.Clone(rendered)
	steps := make([]string, 0, len(operands)+1)
	for i, e := range operands {
		if g.pure(e) {
			continue
		}
		t := g.temp("o")
		g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", g.cType(ast.TypeOf(e)), t))
		steps = append(steps, fmt.Sprintf("%s = %s", t, rendered[i]))
		args[i] = t
	}
	return "(" + strings.Join(append(steps, build(args)), ", ") + ")"
}

// orderShows reports whether the order operands are evaluated in could
// change what a program does: at least two of them are not pure, and one
// of those calls a function, which can print, exit, or assign a mutable
// global another one reads.
func (g *generator) orderShows(operands []ast.Expr) bool {
	impure, calling := 0, false
	for _, e := range operands {
		if !g.pure(e) {
			impure++
			calling = calling || callsFunc(e)
		}
	}
	return impure >= 2 && calling
}

// pure reports whether evaluating e can neither do anything a program
// could see nor be changed by what another operand does: it calls no
// function and reads no mutable package-level variable. Constants,
// parameters, locals and immutable globals are pure, since no call can
// assign a parameter or a local of another function, and so are len and
// conversions of pure operands.
func (g *generator) pure(e ast.Expr) bool {
	pure := !callsFunc(e)
	ast.Walk(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if v, isVar := id.Def.(*ast.Var); isVar && g.globals[v] && v.Mutable {
				pure = false
			}
		}
		return pure
	})
	return pure
}

// callsFunc reports whether evaluating e calls a Tack function. A builtin
// in an expression is len, which has no effect, and a conversion is not a
// call.
func callsFunc(e ast.Expr) bool {
	found := false
	ast.Walk(e, func(n ast.Node) bool {
		if call, ok := n.(*ast.Call); ok {
			if id, isName := call.Fn.(*ast.Ident); isName {
				if fn, isFunc := id.Def.(*ast.Func); isFunc && fn != nil {
					found = true
				}
			}
		}
		return !found
	})
	return found
}
