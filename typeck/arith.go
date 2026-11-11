package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/types"
)

// The facts of facts.go bound subjects. This file bounds arithmetic on
// them: an index, a slice's bound, a divisor or a conversion's operand
// that is +, - or * on two integers, or unary - on one, is bounded by
// what its operands are, each a known value, a subject, the length of a
// string, view or list subject, or such arithmetic itself, worked out
// exactly from the operands' bounds (see rangeOf). Arithmetic wraps at its
// type's width, so an operation that could wrap, given its operands'
// bounds, has no bound at all, and nor does any arithmetic holding it: a
// bound is only ever one every value the operation can give lies within.
// A target-width type wraps at 32 bits on some targets, so an operation
// in one is bounded only when it cannot wrap there.
//
// Only one leaf is read at a time: x - x is bounded as the difference of
// two values each anywhere in x's range, never as 0, so each read of x
// is bounded on its own, whatever happens between the two. No leaf calls
// anything that could write: a call is no leaf, and the length of a
// subject only reads it. Every leaf is read inside the expression the
// bound is for, and a write anywhere in that expression's use, a call's
// mut argument before it, ends the facts about what it writes (see
// reaches), so each leaf's bound holds where it is read.
//
// Arithmetic that adds a known amount to one value, i + 1 or len(a) - 1,
// is a node plus an offset (see linear), and so a query's term too (see
// arithTerm): the relations in force can bound it by a length or by
// another subject. Each step of it must not wrap, which its bounds prove
// (see rangeOf), or else the relations in force do, as for a relation
// through x + c (see relates).

// isArithmetic reports whether e is an operation rangeOf bounds: +, - or
// * on integers, or unary - on one.
func isArithmetic(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Binary:
		return (x.Op == ast.Add || x.Op == ast.Sub || x.Op == ast.Mul) && types.IsInteger(x.Type)
	case *ast.Unary:
		return x.Op == ast.Neg && types.IsInteger(x.Type)
	default:
		return false
	}
}

// isWrapping reports whether e is a wrapping operator on integers, +% -%
// *% or -%x, whose value is always one of its type's.
func isWrapping(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Binary:
		return x.Op.IsWrapping() && types.IsInteger(x.Type)
	case *ast.Unary:
		return x.Op == ast.NegWrap && types.IsInteger(x.Type)
	default:
		return false
	}
}

// operandRange returns what is known about the value of e where it is
// used, when e is a subject, the length of a string, view or list
// subject, arithmetic (see isArithmetic) or a wrapping operator, and
// false for anything else: a subject's interval (see intervalOf), a
// length's bound, the one it has as an operand of arithmetic (see
// leafRange), so that len(s) is proven wherever len(s) + 0 is,
// arithmetic's bound (see rangeOf), or, for a wrapping operator and for
// arithmetic with none, its type's bounds, which every value of it lies
// within, wrapped or not, so that the relations in force may still prove
// more.
func (c *Checker) operandRange(e ast.Expr) (interval, bool) {
	if c.isSubject(e) {
		return c.intervalOf(e), true
	}
	if _, ok := c.stringLen(e); ok {
		return c.leafRange(e)
	}
	if isWrapping(e) {
		return typeInterval(e), true
	}
	if !isArithmetic(e) {
		return interval{}, false
	}
	if known, ok := c.rangeOf(e); ok {
		return known, true
	}
	return typeInterval(e), true
}

// rangeOf returns the bound of e, an integer expression, where it is used,
// and false when it has none: a known value is its own bound; a subject's
// is what its type and the facts in force say (see intervalOf); the
// length of a string, view or list subject's runs from the least length
// the facts give it (see minLen) to the greatest they give it, or the
// largest int64 when they give none (see maxLen); and arithmetic's
// is worked out exactly from its operands' bounds, and is a bound only
// when every value it allows is a value of the operation's type on every
// target, so that the operation cannot wrap (see fitted). Anything else
// has none, and nor does arithmetic with an operand that has none.
func (c *Checker) rangeOf(e ast.Expr) (interval, bool) {
	c.work.ranged++
	if v, ok := constValue(e); ok {
		return single(v), true
	}
	switch x := e.(type) {
	case *ast.Binary:
		if isArithmetic(x) {
			return c.binaryRange(x)
		}
	case *ast.Unary:
		if isArithmetic(x) {
			return c.negRange(x)
		}
	}
	return c.leafRange(e)
}

// leafRange returns the bound of e, an operand of arithmetic that is not
// arithmetic itself, and false when it has none (see rangeOf). A subject
// known not to be zero, and at or above it, is at or above 1, and at or
// below -1 when at or below it. A subject's bound counts only with both
// its ends, which every integer type gives it.
func (c *Checker) leafRange(e ast.Expr) (interval, bool) {
	if v, ok := c.knownValue(e); ok {
		return single(v), true
	}
	if !types.IsInteger(ast.TypeOf(e)) {
		return interval{}, false
	}
	if _, ok := c.stringLen(e); ok {
		s := e.(*ast.Call).Args[0]
		known := lengthRange()
		known.lo = c.minLen(s)
		if hi, ok := c.maxLen(s); ok && hi.Cmp(known.hi) < 0 {
			known.hi = hi
		}
		return known, true
	}
	if !c.isSubject(e) {
		return interval{}, false
	}
	known := c.intervalOf(e).tightened()
	return known, known.hasLo && known.hasHi
}

// binaryRange returns the bound of bin, + - or * on integers, from its
// operands' bounds (see rangeOf): the sum of their lowest values and of
// their highest, the lowest of one less the highest of the other, or the
// least and greatest of the four products of their ends. A product of two
// values that are never zero is never zero, when it cannot wrap.
func (c *Checker) binaryRange(bin *ast.Binary) (interval, bool) {
	a, okA := c.rangeOf(bin.X)
	if !okA {
		return interval{}, false
	}
	b, okB := c.rangeOf(bin.Y)
	if !okB {
		return interval{}, false
	}
	var known interval
	var okLo, okHi bool
	switch bin.Op {
	case ast.Add:
		known.lo, okLo = a.lo.Add(b.lo)
		known.hi, okHi = a.hi.Add(b.hi)
	case ast.Sub:
		known.lo, okLo = a.lo.Sub(b.hi)
		known.hi, okHi = a.hi.Sub(b.lo)
	default: // ast.Mul
		known.lo, known.hi, okLo = products(a, b)
		okHi = okLo
		known.nonzero = a.excludesZero() && b.excludesZero()
	}
	if !okLo || !okHi {
		return interval{}, false
	}
	return fitted(known, bin.Type)
}

// products returns the least and the greatest of the products of a's ends
// with b's, which bound the product of any value of a with any value of
// b, and false when one of them is past what a constant holds.
func products(a, b interval) (lo, hi constant.Int, ok bool) {
	ends := [4][2]constant.Int{{a.lo, b.lo}, {a.lo, b.hi}, {a.hi, b.lo}, {a.hi, b.hi}}
	for i, pair := range ends {
		p, fits := pair[0].Mul(pair[1])
		if !fits {
			return lo, hi, false
		}
		if i == 0 || p.Cmp(lo) < 0 {
			lo = p
		}
		if i == 0 || p.Cmp(hi) > 0 {
			hi = p
		}
	}
	return lo, hi, true
}

// negRange returns the bound of u, unary - on an integer, from its
// operand's: the operand's bounds negated and swapped. The negation of a
// value that is never zero is never zero.
func (c *Checker) negRange(u *ast.Unary) (interval, bool) {
	a, ok := c.rangeOf(u.X)
	if !ok {
		return interval{}, false
	}
	lo, okLo := a.hi.Neg()
	hi, okHi := a.lo.Neg()
	if !okLo || !okHi {
		return interval{}, false
	}
	return fitted(
		interval{lo: lo, hi: hi, hasLo: true, hasHi: true, nonzero: a.excludesZero()},
		u.Type,
	)
}

// fitted returns known, the exact bounds of an operation's result, with
// both its ends set, as the bound of an operation of type typ, when every
// value known allows is a value of typ on every target (see
// portableBounds), so that the operation cannot wrap anywhere, and false
// otherwise.
func fitted(known interval, typ types.Type) (interval, bool) {
	if types.PortableBits(typ) == 0 {
		return interval{}, false
	}
	known.hasLo, known.hasHi = true, true
	lo, hi := portableBounds(typ)
	if known.lo.Cmp(lo) < 0 || known.hi.Cmp(hi) > 0 {
		return interval{}, false
	}
	return known, true
}

// arithmeticShape reports whether e is arithmetic that rangeOf reads
// through to its every leaf, whatever their bounds: + - * or unary - on
// integers whose every operand is a known value, a subject of an integer
// type, the length of a string, view or list subject, or such arithmetic.
// A note on an unproven operand of that shape says its bounds fall short,
// not that it is never proven.
func (c *Checker) arithmeticShape(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Binary:
		return isArithmetic(x) && c.shapeOperand(x.X) && c.shapeOperand(x.Y)
	case *ast.Unary:
		return isArithmetic(x) && c.shapeOperand(x.X)
	default:
		return false
	}
}

// shapeOperand reports whether x, an operand of arithmetic, is one
// rangeOf reads (see arithmeticShape).
func (c *Checker) shapeOperand(x ast.Expr) bool {
	if _, ok := constValue(x); ok {
		return true
	}
	if isArithmetic(x) {
		return c.arithmeticShape(x)
	}
	if _, ok := c.knownValue(x); ok {
		return true
	}
	if _, ok := c.stringLen(x); ok {
		return true
	}
	return c.isSubject(x) && types.IsInteger(ast.TypeOf(x))
}

// single returns the interval holding v alone.
func single(v constant.Int) interval {
	return interval{lo: v, hi: v, hasLo: true, hasHi: true, nonzero: !v.IsZero()}
}

// arithTerm returns e as a query's term when it is arithmetic that linear
// reads as a node plus a known offset, i + 1 or len(a) - 1, and false
// otherwise. The term keeps the conditions that its steps do not wrap,
// which a query must prove (see relates), unless e's bound already proves
// that no operation in it can wrap (see rangeOf).
func (c *Checker) arithTerm(e ast.Expr) (linTerm, bool) {
	if !isArithmetic(e) {
		return linTerm{}, false
	}
	t, ok := c.linear(e)
	if !ok || t.node.isZero() {
		return linTerm{}, false
	}
	if _, fits := c.rangeOf(e); fits {
		t.conds = nil
	}
	return t, true
}
