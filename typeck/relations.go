package typeck

import (
	"math"
	"slices"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/types"
)

// The facts of facts.go bound one value at a time. Relations bound the
// difference of two: a fact x - y <= w, a difference bound, says how far
// apart two values are, and the checker chains such bounds, so i < n and
// n <= len(a) together prove i < len(a). A value in a relation is a node:
// a subject, the length of a string, view or list subject, or the constant
// zero, against which a bound on one value, i <= 5, is i - 0 <= 5. Every
// fact in force is a bound of that shape, or two: a lower bound, an upper
// one, a bound by a length (lenOf, atMostLen), a relation between two
// subjects (atMost), or a difference bound (diff). The checker asks about
// them only when a value's own bounds fail to prove what is needed (see
// relates), so the proofs facts.go makes on their own are made as before.
//
// Two things give difference bounds:
//
//   - a comparison of two values, each a subject, a length or a known
//     value, plus or minus a known amount: i < n gives i - n <= -1,
//     i + 1 < len(a) gives i - len(a) <= -2, i == j gives two bounds.
//     Integers of any two types compare by their values, so the bound is
//     exact whatever the types (see linear);
//   - a declaration or a plain assignment that stores such a value in an
//     integer variable: k := j - 1 gives k - j <= -1 and j - k <= 1, and
//     n := len(a) puts n at len(a), holding in the rest of the block after
//     it up to a write to k or to j that reaches the use (see storedFacts).
//     One that stores a known value gives none: an immutable variable
//     holding one is already a known value.
//
// Arithmetic wraps at its type's width, so x + c is x's value plus c only
// when the sum stays within the type. A bound through x + c or x - c
// therefore carries a side condition, that x lies where the step cannot
// wrap (noWrap), and counts only where the other facts in force prove it
// does: i + 1 < len(a) relates i to len(a) only where i < len(a), say, also
// holds, for an int64 i, and not at all on its own, since i + 1 wraps when
// i is the largest int64. A narrower type wraps sooner: for an int8 i,
// i < len(a) keeps nothing from wrapping, and i < 100 does. len(a) - c,
// for c from 0 to 2^63, never wraps, since a length is never negative.
//
// A query, relates, builds a graph of the bounds in force that hold where
// it is asked (see holds): each bound x - y <= w an edge from x to y of
// weight w, every length at least 0 and at most the largest int64. A
// shortest path from x to y, by Bellman and Ford's method, bounds x - y.
// A bound with a side condition joins the graph once the graph without it
// proves its condition; that repeats until none joins. The graph holds the
// nodes linked to the query's two, at most relationCap of them, found
// breadth first from those two through an index of the facts by the nodes
// they name (see relgraph.go), so a query costs a time bounded by the
// cap and the facts about the nodes it reaches, however many facts are in
// force; an edge to a node past the cap is left out, which can only prove
// less. Weights are exact int64s, and a sum past an int64 is left out
// too, never wrapped: a weaker graph proves less, never more.

// relationCap is the most nodes a query's graph holds.
const relationCap = 64

// diffBound is a difference bound, x - y <= w, between two nodes, each a
// subject, the length of a string, view or list subject when xLen or yLen
// says so, or, for the zero key, the constant 0. It holds only where each
// of conds does.
type diffBound struct {
	x, y       factKey
	conds      []noWrap
	w          constant.Int
	xLen, yLen bool
}

// noWrap is a side condition: the node of lies at or above lo, when hasLo,
// and at or below hi, when hasHi, so that a step computed from it does not
// wrap. isLen says the node is a length.
type noWrap struct {
	of           factKey
	lo, hi       constant.Int
	hasLo, hasHi bool
	isLen        bool
}

// linTerm is a value as a node plus a known offset, node + off: a subject,
// a length when isLen says so, or, for the zero key, the constant off.
// conds are what must hold for the value to be node + off, every step of
// arithmetic from the node staying within its type, and own the bounds
// the node's type, or a length's range, gives it.
type linTerm struct {
	node  factKey
	conds []noWrap
	off   constant.Int
	own   interval
	isLen bool
}

// lengthRange is the range of any length: from 0 to the largest int64.
func lengthRange() interval {
	return interval{hi: constant.FromInt64(math.MaxInt64), hasLo: true, hasHi: true}
}

// zeroTerm is the constant 0 as a term.
func zeroTerm() linTerm {
	return linTerm{}
}

// linear returns e as a node plus a known offset: a known value, a
// subject of an integer type that can have facts, the length of a string,
// view or list subject, or one of those plus or minus a known value, k + e
// included, each step with the condition that it does not wrap, which is
// left out where the node's own bounds already keep it from wrapping.
func (c *Checker) linear(e ast.Expr) (linTerm, bool) {
	if t, ok := c.linearLeaf(e); ok {
		return t, true
	}
	bin, ok := e.(*ast.Binary)
	if !ok || bin.Op != ast.Add && bin.Op != ast.Sub || !types.IsInteger(bin.Type) {
		return linTerm{}, false
	}
	base, k, ok := c.knownStep(bin)
	if !ok {
		return linTerm{}, false
	}
	t, ok := c.linear(base)
	if !ok || t.node.isZero() {
		return linTerm{}, false
	}
	return t.step(k, bin.Type)
}

// linearLeaf returns e as a term when it is a known value, a subject of
// an integer type that can have facts, or the length of a string, view or
// list subject.
func (c *Checker) linearLeaf(e ast.Expr) (linTerm, bool) {
	if v, ok := c.knownValue(e); ok {
		return linTerm{off: v}, true
	}
	if k, ok := c.stringLen(e); ok {
		return linTerm{node: k, isLen: true, own: lengthRange()}, true
	}
	if k, ok := c.keyOf(e); ok && types.IsInteger(ast.TypeOf(e)) {
		return linTerm{node: k, own: typeInterval(e)}, true
	}
	return linTerm{}, false
}

// knownStep returns the operand of bin, a + or a -, that is not a known
// value, and the known amount bin adds to it: the right operand, or, for
// k + e, the left, negated for a -.
func (c *Checker) knownStep(bin *ast.Binary) (ast.Expr, constant.Int, bool) {
	base := bin.X
	k, known := c.knownValue(bin.Y)
	if !known && bin.Op == ast.Add {
		base = bin.Y
		k, known = c.knownValue(bin.X)
	}
	if !known {
		return nil, k, false
	}
	if bin.Op == ast.Sub {
		var ok bool
		if k, ok = k.Neg(); !ok {
			return nil, k, false
		}
	}
	return base, k, true
}

// step returns t plus k, computed in the type typ, with the condition
// that the sum stays within typ (see stepCond).
func (t linTerm) step(k constant.Int, typ types.Type) (linTerm, bool) {
	off, ok := t.off.Add(k)
	if !ok {
		return linTerm{}, false
	}
	cond, ok := t.stepCond(off, typ)
	if !ok {
		return linTerm{}, false
	}
	t.off = off
	if cond.hasLo || cond.hasHi {
		t.conds = append(slices.Clip(t.conds), cond)
	}
	return t, true
}

// stepCond returns the condition that t's node plus off is a value of
// typ: the node lies between typ's bounds less off. A bound past what a
// constant can hold is one the node meets whatever its value, and so is
// one t's own bounds already meet; both are left out.
func (t linTerm) stepCond(off constant.Int, typ types.Type) (noWrap, bool) {
	lo, hi := stepBounds(typ)
	cond := noWrap{of: t.node, isLen: t.isLen}
	switch v, ok := lo.Sub(off); {
	case ok:
		cond.lo, cond.hasLo = v, !t.own.hasLo || t.own.lo.Cmp(v) < 0
	case off.IsNegative():
		return noWrap{}, false
	}
	switch v, ok := hi.Sub(off); {
	case ok:
		cond.hi, cond.hasHi = v, !t.own.hasHi || t.own.hi.Cmp(v) > 0
	case !off.IsNegative():
		return noWrap{}, false
	}
	return cond, true
}

// linearFacts returns the difference bounds left op right gives, left
// and right each a value linear can read, of two different nodes, or nil.
func (c *Checker) linearFacts(left ast.Expr, op ast.BinaryOp, right ast.Expr) []fact {
	a, okA := c.linear(left)
	b, okB := c.linear(right)
	if !okA || !okB || a.node == b.node {
		return nil
	}
	// a.node + a.off op b.node + b.off, so a.node - b.node op d.
	d, ok := b.off.Sub(a.off)
	if !ok {
		return nil
	}
	conds := append(slices.Clip(a.conds), b.conds...)
	var facts []fact
	for _, bound := range differences(op, d) {
		x, y := a, b
		if bound.swap {
			x, y = b, a
		}
		facts = append(facts, diffFact(x, y, bound.w, conds))
	}
	return facts
}

// difference is a bound x - y <= w on two values, or on y - x when swap.
type difference struct {
	w    constant.Int
	swap bool
}

// differences returns the bounds x op y gives when x - y op d: for <, x -
// y <= d - 1; for >, y - x <= -d - 1; for <= and >= without the 1, and
// both for ==. A bound past what a constant can hold is left out.
func differences(op ast.BinaryOp, d constant.Int) []difference {
	one := constant.FromInt64(1)
	negD, okNeg := d.Neg()
	var out []difference
	add := func(w constant.Int, swap, ok bool) {
		if ok {
			out = append(out, difference{w: w, swap: swap})
		}
	}
	switch op {
	case ast.Lt:
		w, ok := d.Sub(one)
		add(w, false, ok)
	case ast.Le:
		add(d, false, true)
	case ast.Gt:
		w, ok := negD.Sub(one)
		add(w, true, ok && okNeg)
	case ast.Ge:
		add(negD, true, okNeg)
	case ast.Eq:
		add(d, false, true)
		add(negD, true, okNeg)
	}
	return out
}

// diffFact returns the fact x.node - y.node <= w, holding where conds
// do, about x's node, or y's when x's is the constant.
func diffFact(x, y linTerm, w constant.Int, conds []noWrap) fact {
	about := x.node
	if about.isZero() {
		about = y.node
	}
	return fact{about: about, diff: &diffBound{
		x: x.node, y: y.node, w: w, conds: conds, xLen: x.isLen, yLen: y.isLen,
	}}
}

// storedFacts returns the facts a statement gives the statements after it
// by what it stores, and the guard they come from: a declaration with a
// value, or a plain assignment to a variable, of an integer type, whose
// value linear reads as another node plus an offset, relates the variable
// to that node both ways. A value computed from the variable itself, as
// in k = k - 1, gives none. A known value stored in a mutable variable,
// mut n := 0, a declaration with none, which stores its zero value, or n
// = 5, puts the variable at that value; an immutable variable holding a
// known value is already one.
func (c *Checker) storedFacts(stmt ast.Stmt) (*guard, []fact) {
	if v, ok := stmt.(*ast.Var); ok && v.Value == nil && v.Mutable && types.IsInteger(v.Type) &&
		c.canHoldFact(v) {
		return &guard{span: stmt.Spanned(), kind: guardValue}, []fact{bound(declKey(v), ast.Eq, constant.Int{})}
	}
	if g, facts := c.steppedFacts(stmt); g != nil {
		return g, facts
	}
	target, value, ok := c.storedTarget(stmt)
	if !ok {
		return nil, nil
	}
	if k, known := c.knownValue(value); known {
		if v, isVar := stmt.(*ast.Var); isVar && !v.Mutable {
			return nil, nil
		}
		return &guard{span: stmt.Spanned(), kind: guardValue}, []fact{bound(target.node, ast.Eq, k)}
	}
	t, ok := c.linear(value)
	if !ok || t.node.isZero() || t.node.decl == target.node.decl {
		return c.rangeStored(stmt, target.node, value, value.Spanned().End)
	}
	negOff, ok := t.off.Neg()
	if !ok {
		return nil, nil
	}
	g := &guard{span: stmt.Spanned(), kind: guardStore}
	return g, []fact{
		diffFact(target, t, t.off, t.conds),
		diffFact(t, target, negOff, t.conds),
	}
}

// steppedFacts returns the fact an increment, a decrement or a compound
// assignment by + - or * of an integer subject gives the statements after
// it: the bounds of the value it stores (see rangeStored).
func (c *Checker) steppedFacts(stmt ast.Stmt) (*guard, []fact) {
	var bin *ast.Binary
	switch s := stmt.(type) {
	case *ast.IncDec:
		one := &ast.IntLit{Value: constant.FromInt64(1), Span: s.Target.Spanned()}
		bin = &ast.Binary{X: s.Target, Y: one, Op: ast.Add, Span: s.Span}
		if s.Dec {
			bin.Op = ast.Sub
		}
	case *ast.Assign:
		if s.Op != ast.Add && s.Op != ast.Sub && s.Op != ast.Mul {
			return nil, nil
		}
		bin = &ast.Binary{X: s.Target, Y: s.Value, Op: s.Op, Span: s.Span}
	default:
		return nil, nil
	}
	bin.Type = ast.TypeOf(bin.X)
	k, ok := c.keyOf(bin.X)
	if !ok || !types.IsInteger(bin.Type) {
		return nil, nil
	}
	return c.rangeStored(stmt, k, bin, bin.Y.Spanned().End)
}

// rangeStored returns the fact that stmt, storing value in the integer
// subject k, gives the statements after it: k lies within the bounds of
// the value where it was computed, at, before stmt stored it (see
// operandBounds), when those are narrower than k's type's. The value was
// proven to fit, or the program is rejected, so the value is the one
// stored.
func (c *Checker) rangeStored(stmt ast.Stmt, k factKey, value ast.Expr, at int) (*guard, []fact) {
	outer := c.at
	c.at = at
	known := c.operandBounds(value)
	if bin, ok := value.(*ast.Binary); ok && isPlainArith(bin) {
		known = c.exactRange(bin)
	}
	c.at = outer
	whole := typeInterval(value)
	if !known.hasLo || !known.hasHi || known.lo.Cmp(whole.lo) <= 0 && known.hi.Cmp(whole.hi) >= 0 {
		return nil, nil
	}
	known = known.within(whole)
	f := fact{about: k, lo: known.lo, hi: known.hi, hasLo: true, hasHi: true}
	return &guard{span: stmt.Spanned(), kind: guardValue}, []fact{f}
}

// storedTarget returns the variable stmt stores to, as a term, and the
// value it stores, when stmt is a declaration with a value, or a plain
// assignment to a name, of an integer type, the variable one that can
// have facts.
func (c *Checker) storedTarget(stmt ast.Stmt) (linTerm, ast.Expr, bool) {
	switch s := stmt.(type) {
	case *ast.Var:
		if s.Value == nil || !types.IsInteger(s.Type) || !c.canHoldFact(s) {
			return linTerm{}, nil, false
		}
		return linTerm{node: declKey(s)}, s.Value, true
	case *ast.Assign:
		id, ok := s.Target.(*ast.Ident)
		if s.Op != ast.InvalidOp || !ok || !types.IsInteger(ast.TypeOf(id)) {
			return linTerm{}, nil, false
		}
		k, ok := c.keyOf(id)
		return linTerm{node: k}, s.Value, ok
	default:
		return linTerm{}, nil, false
	}
}

// allKeys writes into buf every subject f names, its own first, then
// those keys gives, and returns the part of buf written, so that a caller
// asking about each fact of a long chain allocates nothing for it.
func (f fact) allKeys(buf *[7]factKey) []factKey {
	buf[0], buf[1], buf[2], buf[3], buf[4] = f.about, f.lenOf, f.atMostLen, f.atMost, f.inMap
	if f.diff == nil {
		return buf[:5]
	}
	buf[5], buf[6] = f.diff.x, f.diff.y
	return buf[:7]
}

// keys returns the subjects f names besides its own, about: the length
// that bounds it, the subject a relation bounds it by, the map it is a key
// of, and a difference bound's two nodes. A write to any of them can end
// f. A subject f does not have is the zero factKey, kept in its place, as
// allKeys keeps it.
func (f fact) keys() []factKey {
	keys := []factKey{f.lenOf, f.atMostLen, f.atMost, f.inMap}
	if f.diff != nil {
		keys = append(keys, f.diff.x, f.diff.y)
	}
	return keys
}

// queryTerm returns e as a query's term: a known value, a subject of an
// integer type, the length of a string, view or list subject, or
// arithmetic that adds a known amount to one of those last two, with the
// conditions that it does not wrap that its own bound does not prove
// (see arithTerm), which a query proves before it counts (see relates).
func (c *Checker) queryTerm(e ast.Expr) (linTerm, bool) {
	if v, ok := c.knownValue(e); ok {
		return linTerm{off: v}, true
	}
	if k, ok := c.stringLen(e); ok {
		return linTerm{node: k, isLen: true}, true
	}
	if k, ok := c.subjectKey(e); ok && types.IsInteger(ast.TypeOf(e)) {
		return linTerm{node: k}, true
	}
	return c.arithTerm(e)
}

// lenTerm returns the length of s, a string, view or list subject, as a
// query's term.
func (c *Checker) lenTerm(s ast.Expr) (linTerm, bool) {
	k, ok := c.stringVar(s)
	return linTerm{node: k, isLen: true}, ok
}

// relates reports whether the relations in force prove x - y <= w, x and
// y each a query's term: whether the graph proves each term's conditions,
// that it is its node plus its offset, unwrapped (see arithTerm), and
// x.node - y.node <= w - x.off + y.off.
func (c *Checker) relates(x, y linTerm, w constant.Int) bool {
	g, w, ok := c.query(x, y, w)
	return ok && g != nil && g.holdsAll(x.conds) && g.holdsAll(y.conds) &&
		g.proves(x.node, y.node, w)
}

// query returns the graph for proving x - y <= w, and the bound the graph
// must prove on x's node less y's for it, or false when no bound can
// prove it. Two terms of the same node with no conditions need no facts:
// the graph of that node alone proves their difference.
func (c *Checker) query(x, y linTerm, w constant.Int) (*relGraph, constant.Int, bool) {
	w, ok := w.Sub(x.off)
	if ok {
		w, ok = w.Add(y.off)
	}
	if !ok {
		return nil, w, false
	}
	if x.node == y.node && len(x.conds) == 0 && len(y.conds) == 0 {
		g := &relGraph{index: make(map[factKey]int)}
		g.found(x.node)
		g.admit(nil)
		return g, w, true
	}
	return c.relationGraph(x.node, y.node, x.isLen, y.isLen), w, true
}

// atLeast reports whether the relations in force prove the query's term
// e at least 0.
func (c *Checker) atLeast(e ast.Expr) bool {
	t, ok := c.queryTerm(e)
	return ok && c.relates(zeroTerm(), t, constant.Int{})
}

// atMostBy reports whether the relations in force prove a - b <= w, a and
// b each a query's term, or nil for the constant 0.
func (c *Checker) atMostBy(a, b ast.Expr, w constant.Int) bool {
	ta, okA := c.exprTerm(a)
	tb, okB := c.exprTerm(b)
	return okA && okB && c.relates(ta, tb, w)
}

// exprTerm returns e as a query's term, as queryTerm does, or the
// constant 0 for a nil e.
func (c *Checker) exprTerm(e ast.Expr) (linTerm, bool) {
	if e == nil {
		return zeroTerm(), true
	}
	return c.queryTerm(e)
}
