package c

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// A ?, f(...)?, lowers to a GNU C statement expression, which clang
// takes, that calls f into a temporary of its result's C type, an error
// or f's results struct, tests the error's code, and gives the call's
// other results when it is 0:
//
//	({ tack_results_f tack_q3 = tack_f(...); if (tack_q3.r1.code != 0) {
//	    <frees> <return> } tack_q3.r0; })
//
// When the code is not 0, it frees what the call gave beside the error
// that is owned, which nothing else holds, a list the call made before it
// failed among it, then returns from the function the way a return
// written there would, its value fn's results struct with every result
// but the last zeroed and the error last, (R){.r2 = tack_q3.r1}, or the
// error alone: through the cleanup chain of every block it leaves, which
// runs the defers reached and frees the owned locals declared, or by
// C's return when no block has any (see deferredReturn). A goto out of a
// statement expression is GNU C's, and only ever jumps forward, out of
// the expression, to the end of a block around it. The zeroed results
// are the empty values a Tack caller can never read: it reads a result
// beside an error only once the error's code is proven 0, and this one's
// is not 0. A list or a map zeroed holds no buffer, and freeing it frees
// nothing, a union zeroed holds its first case's zeroed payload, and a
// function value zeroed is a null pointer, never called.
//
// What the expression around the ? has built before it, and not yet
// given to what takes it, is freed first, last first: an owned value no
// variable holds, a call's result or a variable's value moved, made for
// an operand that a later operand's ? can leave behind, such as the list
// in measure(numbers()?, parse(s)?) when parse fails (see ownedGuard).
//
// A ? written as a statement gives nothing; one of a call with several
// results before its error, bound by a, b := f()?, gives the results
// struct whole, which the binding reads field by field.

// pendingFree is an owned value the expression being rendered has made
// and not yet given to what takes it: the C place ref holding it, of type
// typ, which a ? that returns before then frees.
type pendingFree struct {
	ref string
	typ types.Type
}

// questionExpr renders q, a ?, as the statement expression the comment
// above describes, and returns it and the C type of the call's value, an
// error or its results struct. value says whether it gives its value, the
// call's one other result or its results struct, or nothing, as a
// statement does.
func (g *generator) questionExpr(q *ast.Question, value bool) (string, string, []diag.Diagnostic) {
	call := ast.Questioned(q)
	switch {
	case call == nil:
		return "", "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "internal error: a ? must follow a call",
			Span:     q.Span,
		}}
	case g.deferring || g.frame == nil || g.frame.fn == nil:
		return "", "", []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "cannot return from a defer with ?",
			Span:     q.Mark(),
		}}
	}
	ctype, init, n, diags := g.questioned(call)
	if len(diags) > 0 {
		return "", "", diags
	}
	t := g.temp("q")
	errRef := t
	if n > 1 {
		errRef = fmt.Sprintf("%s.r%d", t, n-1)
	}
	parts := []string{
		fmt.Sprintf("%s %s = %s;", ctype, t, init),
		fmt.Sprintf("if (%s.code != 0) {", errRef),
	}
	// The call's other results are its caller's, the ?'s, which drops
	// them when it returns: what f returned beside its error, a list
	// made so far among it, is freed first, the last made.
	for i := len(q.Results) - 1; i >= 0; i-- {
		parts = append(parts, g.frees(q.Results[i], fmt.Sprintf("%s.r%d", t, i))...)
	}
	parts = append(parts, g.questionReturn(errRef)...)
	parts = append(parts, "}")
	if value {
		switch len(q.Results) {
		case 0:
		case 1:
			parts = append(parts, t+".r0;")
		default:
			parts = append(parts, t+";")
		}
	}
	return "({ " + strings.Join(parts, " ") + " })", ctype, nil
}

// questioned renders call, the call a ? follows, and returns the C type of
// a temporary holding its value, an error or its results struct, the
// expression that gives it, and how many results the call has, its error
// included.
func (g *generator) questioned(call *ast.Call) (ctype, init string, n int, diags []diag.Diagnostic) {
	if fn := ast.Callee(call); fn != nil || isBuiltin(call, "add") || isBuiltin(call, "add_all") {
		n = len(ast.CallResults(call))
	}
	if n <= 1 {
		init, diags = g.callValue(call)
		return g.cType(types.Error), init, 1, diags
	}
	src, diags := g.callValues(call)
	if len(diags) > 0 {
		return "", "", 0, diags
	}
	return src.temps[0].typ, src.temps[0].init, n, nil
}

// questionReturn returns the C statements of a ? whose error, at errRef,
// is set: the frees of every value pending, last first, then the return
// of the error from the function being lowered, the other results
// zeroed, as generateReturn lowers a return there.
func (g *generator) questionReturn(errRef string) []string {
	var lines []string
	for i := len(g.pending) - 1; i >= 0; i-- {
		p := g.pending[i]
		lines = append(lines, g.frees(p.typ, p.ref)...)
	}
	fn := g.frame.fn
	value := errRef
	if n := len(fn.ResultTypes); n > 1 {
		value = fmt.Sprintf("(%s){.r%d = %s}", g.resultsName(fn), n-1, errRef)
	}
	if route := g.stops(len(g.scopes)); len(route) > 0 {
		return append(lines, g.deferredReturn(value, route)...)
	}
	return append(lines, fmt.Sprintf("return %s;", value))
}

// questionStmt lowers q, a ? written as a statement, which drops what it
// gives: the type checker has made sure none of it is owned.
func (g *generator) questionStmt(q *ast.Question) ([]string, []diag.Diagnostic) {
	for _, typ := range q.Results {
		if g.owned(typ) {
			return nil, []diag.Diagnostic{{
				Severity: diag.Error,
				Message:  "internal error: a discarded value owns what it holds",
				Span:     q.Span,
			}}
		}
	}
	x, _, diags := g.questionExpr(q, false)
	if len(diags) > 0 {
		return nil, diags
	}
	return []string{x + ";"}, nil
}

// questionValues lowers a, b := f()?, the source of a MultiVar or a
// MultiAssign: the ? gives the call's results struct, held in a
// temporary and read field by field, as a call of several results is
// (see callValues), its error never read.
func (g *generator) questionValues(q *ast.Question) (multiSource, []diag.Diagnostic) {
	x, ctype, diags := g.questionExpr(q, true)
	return multiSource{
		temps: []multiTemp{{typ: ctype, init: x}},
		field: func(tmps []string, i int) string { return fmt.Sprintf("%s.r%d", tmps[0], i) },
	}, diags
}

// ownedGuard follows the operands of one construct that takes them in
// order, a call's arguments, a literal's values or a return's, as they
// are rendered, and keeps each operand that gives an owned value no
// variable holds, before an operand that holds a ?, pending (see
// pendingFree) while the operands after it are rendered, so that a ? in
// them that returns frees it.
type ownedGuard struct {
	g    *generator
	ops  []ast.Expr
	last int // the last of ops that holds a ?, or -1
	mark int // how many values were pending before the construct
}

// guardOwned starts following ops, the operands of one construct, in
// the order they are evaluated. done must be called once they are all
// rendered and built into the construct, which takes them.
func (g *generator) guardOwned(ops []ast.Expr) *ownedGuard {
	last := -1
	for i, e := range ops {
		if e != nil && g.traitsOf(e).question {
			last = i
		}
	}
	return &ownedGuard{g: g, ops: ops, last: last, mark: len(g.pending)}
}

// hold returns x, operand i rendered, kept pending when it gives an owned
// value no variable holds and an operand after it holds a ?. It cannot
// give one unless it calls a function or moves a variable, since a value
// of an owned type built of nothing else holds no buffer, so it is not
// pure, and is evaluated in order, before every operand after it: the
// construct holds its operands in temporaries, in order, when a later one
// calls a function (see sequenced), and a deep expression holds every one
// that is not pure in a step, in order (see spilled). So the value is
// copied, as it is evaluated, into a temporary of its own, (tack_p4 = x),
// or, in a deep expression, x is the step's temporary that holds it, or
// held in one now: any ? after it finds it there.
func (og *ownedGuard) hold(i int, x string) string {
	g := og.g
	e := og.ops[i]
	if i >= og.last || g.frame == nil || g.pure(e) || !g.givesOwned(e) {
		return x
	}
	typ := ast.TypeOf(e)
	if sc := g.spill; sc != nil && sc.deep {
		if !isTempName(x) {
			t := g.temp("d")
			g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", g.operandType(e), t))
			sc.steps = append(sc.steps, t+" = "+x)
			x = t
		}
		g.pending = append(g.pending, pendingFree{ref: x, typ: typ})
		return x
	}
	t := g.temp("p")
	g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", g.operandType(e), t))
	g.pending = append(g.pending, pendingFree{ref: t, typ: typ})
	return "(" + t + " = " + x + ")"
}

// done ends the construct: every value it kept pending has been given to
// it.
func (og *ownedGuard) done() {
	og.g.pending = og.g.pending[:og.mark]
}

// givesOwned reports whether evaluating e gives a value of an owned type
// that no variable holds once it is given: anything but a place, which
// is lent, and a mut argument, which is an address; a name whose value is
// moved, and an own argument, give the value the variable held.
func (g *generator) givesOwned(e ast.Expr) bool {
	if !g.owned(ast.TypeOf(e)) {
		return false
	}
	switch x := e.(type) {
	case *ast.MutArg:
		return false
	case *ast.OwnArg:
		return true
	case *ast.Ident:
		return x.Move
	case *ast.Selector, *ast.Index:
		return ast.Root(e) == nil
	default:
		return true
	}
}

// isTempName reports whether x is the name of a temporary the generator
// made, tack_ and a letter and a number (see temp), and nothing else.
func isTempName(x string) bool {
	rest, ok := strings.CutPrefix(x, "tack_")
	if !ok || len(rest) < 2 || rest[0] < 'a' || rest[0] > 'z' {
		return false
	}
	i := 1
	for i < len(rest) && rest[i] >= 'a' && rest[i] <= 'z' {
		i++
	}
	if i == len(rest) {
		return false
	}
	for ; i < len(rest); i++ {
		if rest[i] < '0' || rest[i] > '9' {
			return false
		}
	}
	return true
}
