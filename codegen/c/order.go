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
		g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", g.operandType(e), t))
		steps = append(steps, fmt.Sprintf("%s = %s", t, rendered[i]))
		args[i] = t
	}
	return "(" + strings.Join(append(steps, build(args)), ", ") + ")"
}

// operandType returns the C type of a temporary holding operand: its
// type's, or a pointer to it for a mut argument, which is the address of
// its place.
func (g *generator) operandType(operand ast.Expr) string {
	if _, ok := operand.(*ast.MutArg); ok {
		return g.cType(ast.TypeOf(operand)) + " *"
	}
	return g.cType(ast.TypeOf(operand))
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
// function and reads no mutable package-level variable, and no variable
// the function passes as mut anywhere, which a call in another operand
// could change. Constants, immutable globals, and the parameters and
// locals the function never passes as mut are pure, since a call can
// assign a parameter or a local of another function only through a mut
// argument, and so are len and conversions of pure operands. A mut
// argument evaluates to the address of its place, which no call changes,
// so it is pure when its indexes are.
func (g *generator) pure(e ast.Expr) bool {
	if m, ok := e.(*ast.MutArg); ok {
		return g.pureIndexes(m.X)
	}
	pure := !callsFunc(e)
	ast.Walk(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && g.changeable(id) {
			pure = false
		}
		return pure
	})
	return pure
}

// pureIndexes reports whether every index in place, a chain of fields and
// elements, is pure.
func (g *generator) pureIndexes(place ast.Expr) bool {
	for {
		switch x := place.(type) {
		case *ast.Index:
			if !g.pure(x.Index) {
				return false
			}
			place = x.X
		case *ast.Selector:
			place = x.X
		default:
			return true
		}
	}
}

// changeable reports whether a call can change the variable id names
// while an expression is evaluated: a mutable global, or a variable or
// parameter the function being lowered passes as mut somewhere.
func (g *generator) changeable(id *ast.Ident) bool {
	if v, isVar := id.Def.(*ast.Var); isVar && g.globals[v] && v.Mutable {
		return true
	}
	return g.frame != nil && g.frame.mutated[id.Def]
}

// mutated returns the variables and parameters body passes as mut, at any
// depth, deferred code included.
func mutated(body []ast.Stmt) map[ast.Decl]bool {
	found := make(map[ast.Decl]bool)
	for _, stmt := range body {
		ast.Walk(stmt, func(n ast.Node) bool {
			if m, ok := n.(*ast.MutArg); ok {
				if root := ast.Root(m.X); root != nil {
					found[root.Def] = true
				}
			}
			return true
		})
	}
	return found
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
