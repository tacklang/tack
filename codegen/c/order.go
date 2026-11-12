package c

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
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
	return g.sequencedAs(operands, rendered, g.operandTypes(operands), build)
}

// sequencedLit is sequenced for the values of an array or struct literal,
// which, in a deep scope, need no temporaries of their own: there, every
// value that is not pure, and every part of one, was held in a step as it
// was rendered, in order (see spilled), so what is left of each reads
// only those steps' temporaries and pure parts. Holding them again would
// copy every inner literal's value into the frame, one copy for each
// level of a deep literal.
func (g *generator) sequencedLit(
	operands []ast.Expr,
	rendered []string,
	build func([]string) string,
) string {
	if g.spill != nil && g.spill.deep {
		return build(rendered)
	}
	return g.sequenced(operands, rendered, build)
}

// operandTypes returns the C type of a temporary holding each of
// operands, as operandType gives it.
func (g *generator) operandTypes(operands []ast.Expr) []string {
	ctypes := make([]string, len(operands))
	for i, e := range operands {
		ctypes[i] = g.operandType(e)
	}
	return ctypes
}

// sequencedAs is sequenced for operands whose rendered values have the C
// types ctypes, which a temporary holding one is declared with: a call's
// argument to a view parameter is a view, whatever the argument's own
// type.
func (g *generator) sequencedAs(
	operands []ast.Expr,
	rendered, ctypes []string,
	build func([]string) string,
) string {
	if !g.orderShows(operands) {
		return build(rendered)
	}
	return g.hoist(operands, rendered, ctypes, build)
}

// hoist returns the C expression build makes of operands, already
// rendered as rendered, with every operand that is not pure assigned to a
// temporary of the C type in ctypes first, in order, with C's comma
// operator, and given to build in its place, whether or not the order
// could show. A pure operand is passed as rendered, since reading it
// again, or not at all, changes nothing. With no operand to hold, it is
// build's expression alone.
func (g *generator) hoist(
	operands []ast.Expr,
	rendered, ctypes []string,
	build func([]string) string,
) string {
	args := slices.Clone(rendered)
	steps := make([]string, 0, len(operands)+1)
	for i, e := range operands {
		if g.pure(e) {
			continue
		}
		t := g.temp("o")
		g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", ctypes[i], t))
		steps = append(steps, fmt.Sprintf("%s = %s", t, rendered[i]))
		args[i] = t
	}
	if len(steps) == 0 {
		return build(args)
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
// global another one reads, or moves a variable's value, emptying a
// variable another one reads.
func (g *generator) orderShows(operands []ast.Expr) bool {
	impure, calling := 0, false
	for _, e := range operands {
		if !g.pure(e) {
			impure++
			t := g.traitsOf(e)
			calling = calling || t.calls || t.moves
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
// so it is pure when its indexes are. A name whose value it moves empties
// its variable, which is never pure.
func (g *generator) pure(e ast.Expr) bool {
	if m, ok := e.(*ast.MutArg); ok {
		return g.pureIndexes(m.X)
	}
	t := g.traitsOf(e)
	return !t.calls && !t.moves && !t.changeable
}

// traits is what evaluating an expression can do that its order shows:
// call a function (see callsFunc), move a variable's value (see moves),
// or read a variable a call can change (see changeable); and whether it
// holds a ?, which can return from the function before the rest of the
// expression runs (see question.go).
type traits struct {
	calls, moves, changeable, question bool
}

// or returns what either t or u can do.
func (t traits) or(u traits) traits {
	return traits{
		t.calls || u.calls,
		t.moves || u.moves,
		t.changeable || u.changeable,
		t.question || u.question,
	}
}

// traitsOf returns what evaluating e can do, anywhere in it. Within a
// function they are kept for each expression asked about, and a part
// already asked about is not walked again, so asking about every part of
// a chain, innermost first, as rendering it does, takes time in
// proportion to the chain, not to its square.
func (g *generator) traitsOf(e ast.Expr) traits {
	if g.frame != nil {
		if t, ok := g.frame.traits[e]; ok {
			return t
		}
	}
	var t traits
	ast.Walk(e, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit {
			// A closure's value is its function and the addresses of its
			// captures, which nothing changes; its body runs only when the
			// call it is passed to calls it.
			return false
		}
		if x, isExpr := n.(ast.Expr); isExpr && x != e && g.frame != nil {
			if known, ok := g.frame.traits[x]; ok {
				t = t.or(known)
				return false
			}
		}
		t = t.or(g.ownTraits(n))
		return true
	})
	if g.frame != nil {
		if g.frame.traits == nil {
			g.frame.traits = make(map[ast.Expr]traits)
		}
		g.frame.traits[e] = t
	}
	return t
}

// ownTraits returns what n does itself, not counting its parts: a call
// calls, a name may move its variable or read one a call can change, and
// a ? may return.
func (g *generator) ownTraits(n ast.Node) traits {
	switch x := n.(type) {
	case *ast.Call:
		return traits{calls: callsItself(x)}
	case *ast.Ident:
		return traits{moves: x.Move, changeable: g.changeable(x)}
	case *ast.Question:
		return traits{question: true}
	default:
		return traits{}
	}
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
// depth, deferred code included, the variables whose values it moves,
// which a move empties, and the captures a closure in it may write, which
// the call the closure is passed to can change.
func mutated(body []ast.Stmt) map[ast.Decl]bool {
	found := make(map[ast.Decl]bool)
	for _, stmt := range body {
		ast.Walk(stmt, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.MutArg:
				if root := ast.Root(x.X); root != nil {
					found[root.Def] = true
				}
			case *ast.Ident:
				if x.Move {
					found[x.Def] = true
				}
			case *ast.FuncLit:
				for _, cp := range x.Captures {
					if cp.Write {
						found[cp.Decl] = true
					}
				}
			}
			return true
		})
	}
	return found
}

// changesList reports whether call calls append, pop or remove, the
// builtins that change a list's length, swap, which changes what two of
// its elements hold, replace, which changes what its place holds, a
// list's length among it, put or delete, which change a map, insert,
// which changes a pool, as remove does, or add or add_all, which change
// an arena's length, and so what an id past its end reads, and may move
// its buffer.
func changesList(call *ast.Call) bool {
	return isBuiltin(call, "append") || isBuiltin(call, "pop") || isBuiltin(call, "remove") ||
		isBuiltin(call, "swap") || isBuiltin(call, "replace") || isBuiltin(call, "put") ||
		isBuiltin(call, "delete") || isBuiltin(call, "insert") || isBuiltin(call, "add") ||
		isBuiltin(call, "add_all")
}

// callsFunc reports whether evaluating e calls a Tack function, a method
// included, or append, pop or remove, which grow or shrink a list and so
// change what reading it gives. Any other builtin in an expression is len
// or make, which have no effect, or reserve, whose effect, a larger or
// moved buffer, changes nothing a read of the list gives, and a
// conversion is not a call.
func callsFunc(e ast.Expr) bool {
	found := false
	ast.Walk(e, func(n ast.Node) bool {
		if call, ok := n.(*ast.Call); ok && callsItself(call) {
			found = true
		}
		return !found
	})
	return found
}

// callsItself reports whether call, not counting its operands, calls a
// Tack function, a method, a function value, whichever function it
// names, or a builtin that changes a list, as callsFunc counts them.
func callsItself(call *ast.Call) bool {
	if call.Receiver() != nil || call.Method != nil || call.Dynamic != nil {
		return true
	}
	id, isName := call.Fn.(*ast.Ident)
	if !isName {
		return false
	}
	fn, isFunc := id.Def.(*ast.Func)
	return isFunc && fn != nil || changesList(call)
}

// maxDepth is how deeply the C of one subexpression may nest its
// parentheses, brackets and braces before its value is held in a
// temporary instead (see spilled). C11 (5.2.4.1) promises 63 levels of
// parenthesized expressions in a full expression; a held value's step and
// the expression around it add a few levels of their own, so 32 keeps
// every expression this bounds within that, and far below where clang's
// parser runs out of stack, past about a thousand. No ordinary program
// nests nearly as deep: none of the examples does, so none of their C
// changes.
const maxDepth = 32

// spillScope is an expression being rendered on its own, in a function
// (see exprSteps): the steps that hold its deep parts, in the order they
// run, and what keeps that order Tack's.
type spillScope struct {
	// steps assign the temporaries, each a C assignment, in order.
	steps []string

	// inline counts the subexpressions rendered so far, outside every
	// step, that are not pure: a step holding a later subexpression runs
	// before them, which would change the order unless each is pure.
	inline int

	// deep says every subexpression that is not pure is held in a
	// temporary of its own, as it is rendered, so that every one of them
	// runs in order, as a step, and none is left in the expression for a
	// later step to run before.
	deep bool

	// retry is set when a deep subexpression could not be held because
	// one of inline came before it: the expression is rendered again,
	// deep.
	retry bool
}

// exprSteps renders e on its own, as expr does, and returns the steps
// that hold its deep parts, C assignments to run first, in order, and the
// expression that then gives its value. It needs a function to declare
// the temporaries in, and without one renders e as it is. An expression
// that holds a part after a subexpression that is not pure, and would run
// it first, is rendered again with every such subexpression held, in
// order (see spillScope.deep). deep starts it so, as the right side of an
// && or an || does when the expression around it is rendered so.
//
// e itself is never held, though it may be deep: its value is the
// expression's, read right after the steps, so a place stays a place, and
// a context may take its address.
func (g *generator) exprSteps(e ast.Expr, deep bool) ([]string, string, []diag.Diagnostic) {
	if lit, ok := g.literal(e); ok {
		return nil, lit, nil
	}
	if g.frame == nil {
		x, diags := g.exprNode(e)
		return nil, x, diags
	}
	outer, temps := g.spill, len(g.frame.temps)
	defer func() { g.spill = outer }()
	for {
		sc := &spillScope{deep: deep}
		g.spill = sc
		x, diags := g.exprNode(e)
		if !sc.retry {
			return sc.steps, x, diags
		}
		g.frame.temps, deep = g.frame.temps[:temps], true
	}
}

// errRetry is what spilled returns, as a diagnostic, once its scope is
// to be rendered again: every caller returns on a diagnostic, so the
// rest of the expression, which would be thrown away, is not rendered.
// exprSteps never lets it out.
var errRetry = []diag.Diagnostic{
	{Severity: diag.Error, Message: "expression to be rendered again, deep"},
}

// spilled renders e, a subexpression of the expression g.spill is
// rendering, and holds its value in a temporary, assigned by a step, when
// its C nests deeper than maxDepth, or, in a deep scope, when it is not
// pure. A step runs before everything left in the expression, so a
// subexpression is held only when no subexpression that is not pure was
// rendered before it and left in place; otherwise the scope is marked to
// be rendered again, deep, and rendering it stops (see errRetry).
func (g *generator) spilled(e ast.Expr) (string, []diag.Diagnostic) {
	sc := g.spill
	before := sc.inline
	x, diags := g.exprNode(e)
	if len(diags) > 0 {
		return "", diags
	}
	pure := g.pure(e)
	hold := heldForOrder(sc, e, pure)
	if !hold && cDepth(x) > maxDepth {
		if before > 0 && !sc.deep {
			sc.retry = true
			return "", errRetry
		}
		hold = true
	}
	if !hold || !g.holdable(e) {
		if !pure {
			sc.inline++
		}
		return x, nil
	}
	t := g.temp("d")
	g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", g.operandType(e), t))
	sc.steps = append(sc.steps, t+" = "+x)
	sc.inline = before
	return t, nil
}

// heldForOrder reports whether e, rendered in the scope sc, is held for
// its order alone: sc is deep, and e is not pure and does more than
// build a value of its parts (see builds).
func heldForOrder(sc *spillScope, e ast.Expr, pure bool) bool {
	return sc.deep && !pure && !builds(e)
}

// builds reports whether e only builds a value of its parts, an array or
// struct literal or a union's construction, and so does nothing of its
// own that has an order: in a deep scope, each of its parts that is not
// pure is held in a step as it is rendered, in order, so what is left of
// e reads only those steps' temporaries and pure parts, and need not be
// held itself unless it nests too deep. Holding each level of a deep
// literal would keep a copy of every inner value in the frame, a frame
// that grows with the square of the depth when the values do.
func builds(e ast.Expr) bool {
	if _, ok := e.(*ast.CompositeLit); ok {
		return true
	}
	_, _, _, built := ast.Construction(e)
	return built
}

// holdable reports whether e's value can be held in a temporary: it has
// a type with values, which a C variable can hold.
func (g *generator) holdable(e ast.Expr) bool {
	typ := ast.TypeOf(e)
	return typ != types.Invalid && !types.IsUntyped(typ) && g.cType(typ) != "void"
}

// otherScope renders e, the right side of an && or an ||, which runs only
// when the left side does not decide, on its own: its steps stay inside
// it, so they run only when it does. It is rendered deep when the
// expression around it is.
func (g *generator) otherScope(e ast.Expr) (string, []diag.Diagnostic) {
	if g.spill == nil {
		return g.expr(e, types.Invalid)
	}
	if lit, ok := g.literal(e); ok {
		return lit, nil
	}
	sc := g.spill
	g.spill = nil
	steps, x, diags := g.exprSteps(e, sc.deep)
	g.spill = sc
	if len(diags) > 0 {
		return "", diags
	}
	if !g.pure(e) {
		sc.inline++
	}
	return withSteps(steps, x), nil
}

// withSteps returns the C expression that runs steps, in order, then
// gives x, or x alone when there are none. It is parenthesized twice, so
// that one pair taken off, as a condition's are (see stripParens), still
// leaves the steps and x together.
func withSteps(steps []string, x string) string {
	if len(steps) == 0 {
		return x
	}
	return "((" + commaList(steps) + ", " + x + "))"
}

// flatCommas is how many expressions commaList joins with commas in one
// run before grouping them.
const flatCommas = 8

// commaList joins exprs, C expressions, with the comma operator, so they
// run in order. C parses a run of commas as a chain nested one level per
// comma, which runs clang out of stack at a few thousand, so a long run
// is grouped in halves, each parenthesized, to any depth: the order is
// the same, and the nesting grows with the logarithm of the length.
func commaList(exprs []string) string {
	if len(exprs) <= flatCommas {
		return strings.Join(exprs, ", ")
	}
	half := len(exprs) / 2
	return "(" + commaList(exprs[:half]) + "), (" + commaList(exprs[half:]) + ")"
}

// cDepth returns how deeply x, rendered C, nests its parentheses,
// brackets and braces, outside its character and string literals.
func cDepth(x string) int {
	depth, deepest := 0, 0
	for i := 0; i < len(x); i++ {
		switch x[i] {
		case '"', '\'':
			i = literalEnd(x, i)
		case '(', '[', '{':
			depth++
			deepest = max(deepest, depth)
		case ')', ']', '}':
			depth--
		}
	}
	return deepest
}

// literalEnd returns the index of the quote that closes the character or
// string literal of C that opens at x[start], or the end of x.
func literalEnd(x string, start int) int {
	for i := start + 1; i < len(x); i++ {
		switch x[i] {
		case '\\':
			i++
		case x[start]:
			return i
		}
	}
	return len(x)
}
