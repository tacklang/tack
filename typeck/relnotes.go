package typeck

import (
	"slices"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
)

// This file holds the notes an error for a missing proof carries about
// the relations in force (see relations.go): that a chain lacks one link,
// that a relation through + or - holds only where its step cannot
// overflow, or that a query left relations out past its cap.

// relationNotes returns the note that explains why the relations in
// force do not prove x - y <= w, at span, the use: that the graph left
// relations out past its cap; that a relation through + or - would prove
// it but for its condition (see wrapping); or that one link is missing
// from a chain that would prove it (see linkNote). It returns none when
// nothing is so. Each says what holds and what to check, which a stored
// value's fact, left out of the graph the notes ask (see
// Checker.plainRelations), cannot make untrue: the graph the proof asked
// holds every edge this one does, so it did not prove x - y <= w either.
func (c *Checker) relationNotes(x, y linTerm, w constant.Int, span ast.Span) []diag.Diagnostic {
	c.plainRelations = true
	defer func() { c.plainRelations = false }()
	g, w, ok := c.query(x, y, w)
	if !ok || g == nil || g.proves(x.node, y.node, w) {
		return nil
	}
	if g.capped {
		return []diag.Diagnostic{
			note(span, "the checker follows at most 64 related values from here, "+
				"so relations further along are not used; check "+
				relationText(x.node, y.node, x.isLen, y.isLen, w)+" directly"),
		}
	}
	if cond, ok := g.wrapping(x.node, y.node, w); ok {
		return []diag.Diagnostic{
			note(
				span,
				"a relation through + or - here holds only where its step cannot overflow; check "+cond+" too",
			),
		}
	}
	if msg, ok := c.linkNote(g, x, y, w); ok {
		return []diag.Diagnostic{note(span, msg)}
	}
	return nil
}

// linkNote returns the note that one link is missing from a chain that
// would prove x - y <= w in g: a relation known between x and a third
// subject, or between one and y, with what to check to complete it, the
// second only when a fact in scope, held or not, relates x to the third
// subject, so that the check named is one the code was making. A link is
// named only between two values neither of which is a constant, so a
// bound by a known value, which the notes on the facts' own bounds
// explain, gets none, and the third subject is never a length, which the
// notes on a bound by another length explain.
func (c *Checker) linkNote(g *relGraph, x, y linTerm, w constant.Int) (string, bool) {
	if x.node.isZero() || y.node.isZero() {
		return "", false
	}
	if msg, ok := c.linkAfter(g, x, y, w); ok {
		return msg, true
	}
	return c.linkBefore(g, x, y, w)
}

// middle reports whether k can be the third subject of a link note about
// x and y: neither of them, nor the constant, nor a length.
func (c *Checker) middle(k factKey, x, y linTerm) bool {
	return k != x.node && k != y.node && !k.isZero() && !c.isLength(k)
}

// linkAfter returns the link note for a relation known from x to a third
// subject m, found last first, which m - y <= w - (what is known of x - m)
// would complete, when g does not prove that check can never hold (see
// refutes).
func (c *Checker) linkAfter(g *relGraph, x, y linTerm, w constant.Int) (string, bool) {
	for i := len(g.nodes) - 1; i >= 0; i-- {
		m := g.nodes[i]
		d, known := g.bound(x.node, m)
		if !known || !c.middle(m, x, y) {
			continue
		}
		if link, ok := w.Sub(constant.FromInt64(d)); ok && !g.refutes(m, y.node, link) {
			return relationText(x.node, m, x.isLen, false, constant.FromInt64(d)) +
				" holds here; check " + relationText(m, y.node, false, y.isLen, link) + " too", true
		}
	}
	return "", false
}

// linkBefore returns the link note for a relation known from a third
// subject m to y, found last first, with m compared to x somewhere in
// scope, which x - m <= w - (what is known of m - y) would complete, when
// g does not prove that check can never hold (see refutes).
func (c *Checker) linkBefore(g *relGraph, x, y linTerm, w constant.Int) (string, bool) {
	for i := len(g.nodes) - 1; i >= 0; i-- {
		m := g.nodes[i]
		d, known := g.boundTo(m, y.node)
		if !known || !c.middle(m, x, y) || !c.compared(x.node, m) {
			continue
		}
		if link, ok := w.Sub(constant.FromInt64(d)); ok && !g.refutes(x.node, m, link) {
			return relationText(m, y.node, false, y.isLen, constant.FromInt64(d)) +
				" holds here; check " + relationText(x.node, m, x.isLen, false, link) + " too", true
		}
	}
	return "", false
}

// refutes reports whether g proves that a - b <= w can never hold, by
// proving b - a <= -w - 1: a check a link note names must be one that
// could hold, and under 0 <= i && i < len(s), no i < len(t) -
// 9223372036854775807 can, every length being at most the largest int64.
func (g *relGraph) refutes(a, b factKey, w constant.Int) bool {
	neg, ok := w.Neg()
	if ok {
		neg, ok = neg.Sub(constant.FromInt64(1))
	}
	return ok && g.proves(b, a, neg)
}

// wrapping returns the side condition, written as a check, that keeps the
// graph from proving x - y <= w: the first of an edge left out for its
// conditions whose own failing condition, once proved, with every edge
// left out added, would let the graph prove it.
func (g *relGraph) wrapping(x, y factKey, w constant.Int) (string, bool) {
	if len(g.pending) == 0 || !g.withPending().proves(x, y, w) {
		return "", false
	}
	for _, e := range g.pending {
		for _, cond := range e.conds {
			if check, failing := g.failing(cond); failing {
				return check, true
			}
		}
	}
	return "", false
}

// failing returns, written as a check, the bound of cond that the graph
// does not prove, and whether there is one.
func (g *relGraph) failing(cond noWrap) (string, bool) {
	zero := factKey{}
	name := cond.of.name()
	if cond.isLen {
		name = "len(" + name + ")"
	}
	if cond.hasHi && !g.proves(cond.of, zero, cond.hi) {
		return below(name, cond.hi), true
	}
	if lo, ok := cond.lo.Neg(); cond.hasLo && (!ok || !g.proves(zero, cond.of, lo)) {
		return cond.lo.String() + " <= " + name, true
	}
	return "", false
}

// compared reports whether a fact in scope, in force or dropped, names
// both a and b.
func (c *Checker) compared(a, b factKey) bool {
	for _, f := range c.inForce() {
		keys := append([]factKey{f.about}, f.keys()...)
		if slices.Contains(keys, a) && slices.Contains(keys, b) {
			return true
		}
	}
	return false
}

// isLength reports whether a fact in force uses k as a length.
func (c *Checker) isLength(k factKey) bool {
	for _, f := range c.inForce() {
		if f.lenOf == k || f.atMostLen == k || f.diff != nil &&
			(f.diff.x == k && f.diff.xLen || f.diff.y == k && f.diff.yLen) {
			return true
		}
	}
	return false
}

// relationText writes x - y <= w as a check: x <= y, x < y, x < y - c or
// x <= y + c, x < k or k <= y when one side is the constant 0. A node that
// is a length is written len(s).
func relationText(x, y factKey, xLen, yLen bool, w constant.Int) string {
	name := func(k factKey, isLen bool) string {
		if isLen {
			return "len(" + k.name() + ")"
		}
		return k.name()
	}
	one := constant.FromInt64(1)
	switch {
	case y.isZero():
		return below(name(x, xLen), w)
	case x.isZero():
		lo, _ := w.Neg()
		return lo.String() + " <= " + name(y, yLen)
	case w.IsZero():
		return name(x, xLen) + " <= " + name(y, yLen)
	case !w.IsNegative():
		return name(x, xLen) + " <= " + name(y, yLen) + " + " + w.String()
	case w.Cmp(constant.FromInt64(-1)) == 0:
		return name(x, xLen) + " < " + name(y, yLen)
	}
	gap, _ := w.Neg()
	gap, _ = gap.Sub(one)
	return name(x, xLen) + " < " + name(y, yLen) + " - " + gap.String()
}

// indexRelationNotes returns the notes that explain why the relations in
// force do not prove the index i, a subject or a length, in range for x,
// an array, string, view or list: for each bound its own facts do not
// prove, at least 0 and below x's length, what relationNotes says (see
// lower and upperRelationNotes).
func (c *Checker) indexRelationNotes(i, x ast.Expr) []diag.Diagnostic {
	notes := c.lowerRelationNotes(i)
	return uniqueNotes(append(notes, c.upperRelationNotes(i, x, constant.FromInt64(-1))...))
}

// lowerRelationNotes returns relationNotes for the subject or length b at
// least 0, when neither its own bounds nor the relations prove it.
func (c *Checker) lowerRelationNotes(b ast.Expr) []diag.Diagnostic {
	if known := c.noteInterval(b); known.hasLo && !known.lo.IsNegative() {
		return nil
	}
	t, ok := c.queryTerm(b)
	if !ok {
		return nil
	}
	return c.relationNotes(zeroTerm(), t, constant.Int{}, b.Spanned())
}

// upperRelationNotes returns relationNotes for the subject or length b at
// most the length of x plus w, a known length or len(x) for a subject x,
// when neither b's own bounds nor the relations prove it.
func (c *Checker) upperRelationNotes(b, x ast.Expr, w constant.Int) []diag.Diagnostic {
	t, ok := c.queryTerm(b)
	if !ok {
		return nil
	}
	if n, known := c.extentOf(x); known {
		limit, fits := constant.FromInt64(n).Add(w)
		if hi := c.noteInterval(b); !fits || hi.hasHi && hi.hi.Cmp(limit) <= 0 {
			return nil
		}
		return c.relationNotes(t, zeroTerm(), limit, b.Spanned())
	}
	l, ok := c.lenTerm(x)
	if !ok || w.IsNegative() && c.belowLen(b, x) {
		return nil
	}
	return c.relationNotes(t, l, w, b.Spanned())
}

// orderRelationNotes returns relationNotes for the slice bound lo at most
// hi, both subjects.
func (c *Checker) orderRelationNotes(lo, hi ast.Expr) []diag.Diagnostic {
	tl, okL := c.queryTerm(lo)
	th, okH := c.queryTerm(hi)
	if !okL || !okH || tl.node.isZero() || th.node.isZero() {
		return nil
	}
	return c.relationNotes(tl, th, constant.Int{}, hi.Spanned())
}
