package typeck

import (
	"math"
	"slices"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// A conversion T(x) of an integer that is neither a constant, nor a known
// value, nor of a type that widens to T compiles in its plain form when
// the facts in force where it is written prove x a value of T: x is a
// subject, a variable or a path below an immutable one, the length of a
// string, view or list subject, or arithmetic on such values (see
// arith.go), as an index or a divisor must be to be
// proven, and its type and the facts about it keep it within T's range,
// or the relations in force do, for a bound they do not give (see
// factsFit). The facts are the ones an index's proof asks about, from the
// same places, and ended by the same writes. Arithmetic is bounded only
// where it cannot wrap: under v < 0, uint64(-(v + 1)) is proven, and
// uint64(-v) is not, since -v wraps when v is the least int64.
//
// A target-width T's range is the one it has on every target, from 0 to
// types.PortableMax, as for a known value (see fitsPortably). A
// target-width x's own type bounds it at its widest, a 64-bit target's
// (see typeInterval), so a proof holds whatever its width.

// factsFit reports whether the facts in force prove x, a subject, a
// length or arithmetic of an integer type, a value of the integer type to
// on every target: at or above to's smallest value and at or below its
// largest one there (see portableBounds), by the bounds x's type, or a
// length's range, and the facts give it (see operandRange), or, for a
// query's term, by the relations in force (see atLeastBy and atMostOf).
func (c *Checker) factsFit(x ast.Expr, to types.Type) bool {
	known, ok := c.operandRange(x)
	if !ok {
		return false
	}
	lo, hi := portableBounds(to)
	term, ok := c.queryTerm(x)
	if !ok {
		return known.hasLo && known.lo.Cmp(lo) >= 0 && known.hasHi && known.hi.Cmp(hi) <= 0
	}
	return c.atLeastBy(known, term, lo) && c.atMostOf(known, term, hi)
}

// portableBounds returns the smallest and largest values the integer type
// typ holds on every target: its own for a fixed-width type, and 0 and
// types.PortableMax for a target-width one.
func portableBounds(typ types.Type) (lo, hi constant.Int) {
	n := types.PortableBits(typ)
	if types.IsUnsigned(typ) {
		return constant.Int{}, constant.FromUint64(math.MaxUint64 >> (64 - n))
	}
	lo = constant.FromInt64(math.MinInt64 >> (64 - n))
	return lo, constant.FromInt64(math.MaxInt64 >> (64 - n))
}

// conversionError reports call, the conversion T(x) of an integer of type
// from to the integer type to, which nothing proves cannot fail. A subject
// x gets the check that would prove it (see fitCheck), before the
// two-value form, with notes that explain why the facts about it do not
// prove it (see conversionProof), except a mutable global, which no check
// proves, and gets a note saying so. Anything else gets the two-value form
// alone, with the notes operandNotes gives it.
func (c *Checker) conversionError(call *ast.Call, from, to types.Type) {
	arg := call.Args[0]
	msg := "converting " + c.typeName(from) + " to " + c.typeName(to) + " can fail"
	form := "v, ok := " + c.typeName(to) + "(...)"
	var notes []diag.Diagnostic
	switch g, isGlobal := c.mutableGlobal(arg); {
	case isGlobal:
		msg += orUse(form, false)
		notes = globalNote(g)
	case c.isSubject(arg):
		msg += "; check " + fitCheck(arg, c.nameOf(arg), to) + " first" + orUse(form, true)
		notes = c.explain(c.conversionProof(arg, to))
	case c.isLengthSubject(arg):
		msg += orUse(form, false)
		notes = c.explain(c.conversionProof(arg, to))
	default:
		msg += orUse(form, false)
		notes = c.operandNotes(arg)
	}
	c.errorNotes(call.Span, notes, "%s", msg)
}

// fitCheck writes the check that proves the subject x, called name, a
// value of the integer type to: each bound of to's range on every target
// that x's type does not already keep it within, the lower one first. A
// bound is written as a constant x's type holds on every target, so the
// check compiles: for a target-width x, whose constants stop at
// types.PortableMax, an upper bound past that is written as
// types.PortableMax, which proves the conversion too.
func fitCheck(x ast.Expr, name string, to types.Type) string {
	lo, hi := portableBounds(to)
	own := typeInterval(x)
	needLo, needHi := own.lo.Cmp(lo) < 0, own.hi.Cmp(hi) > 0
	if _, most := portableBounds(ast.TypeOf(x)); hi.Cmp(most) > 0 {
		hi = most
	}
	switch {
	case needLo && needHi:
		return lo.String() + " <= " + name + " && " + name + " <= " + hi.String()
	case needLo:
		return name + " >= " + lo.String()
	default:
		return name + " <= " + hi.String()
	}
}

// conversionProof returns the proof that the subject or length x is a
// value of the integer type to (see factsFit). A fact is relevant to it
// when it bounds x on a side x's type, or a length's range, leaves open,
// and a fact that bounds x short of to's range on that side is the one a
// note points at: it allows the value it stops at.
func (c *Checker) conversionProof(x ast.Expr, to types.Type) proof {
	about, _ := c.noteKey(x)
	name := c.noteName(x)
	lo, hi := portableBounds(to)
	own := c.ownInterval(x)
	needLo, needHi := own.lo.Cmp(lo) < 0, own.hi.Cmp(hi) > 0
	return proof{
		about:     about,
		relations: true,
		proven:    func() bool { return c.factsFit(x, to) },
		relevant: func(f fact) bool {
			return needLo && f.hasLo || needHi && (f.hasHi || bindsOther(f))
		},
		claim: func(facts []fact) string { return describe(name, facts) },
		weak: func(facts []fact, would bool) []diag.Diagnostic {
			known := own
			for _, f := range facts {
				known = known.narrow(f)
			}
			if known.empty() {
				return nil
			}
			var notes []diag.Diagnostic
			if known.lo.Cmp(lo) < 0 {
				notes = c.appendSideNote(notes, facts, x, known.lo, lo, would, true)
			}
			if known.hi.Cmp(hi) > 0 {
				notes = c.appendSideNote(notes, facts, x, known.hi, hi, would, false)
			}
			return notes
		},
	}
}

// appendSideNote appends to notes the note that explains why the facts
// about the subject x, called name, which with its type's bounds give v as
// its lowest value, when lower is set, or its highest, do not keep it at
// or above, or at or below, the limit to's range sets on that side: the
// innermost fact that set v allows name == v, or else the innermost one
// that bounds name on the other side only bounds it there. A bound by
// another value (see bindsOther) is one from above, so on that side,
// where relations may have taken part, no note is given beyond the first.
// Without either, notes are returned as they are. A v a != rules out is
// not one a note names (see rulesOut).
func (c *Checker) appendSideNote(
	notes []diag.Diagnostic,
	facts []fact,
	x ast.Expr,
	v, limit constant.Int,
	would, lower bool,
) []diag.Diagnostic {
	name := c.noteName(x)
	setsV := func(f fact) bool { return f.hasHi && f.hi.Cmp(v) == 0 }
	other := func(f fact) bool { return f.hasLo }
	side, past := "below", "above "
	if lower {
		setsV = func(f fact) bool { return f.hasLo && f.lo.Cmp(v) == 0 }
		other = func(f fact) bool { return f.hasHi || bindsOther(f) }
		side, past = "above", "below "
	}
	if f, ok := setting(facts, setsV); ok {
		if c.rulesOut(x, v) {
			return notes
		}
		msg := f.guard.subject() + " " + verb(
			"allows",
			"allow",
			would,
		) + " " + name + " == " + v.String()
		return append(notes, note(f.span(), msg))
	}
	if !lower && slices.ContainsFunc(facts, bindsOther) {
		return notes
	}
	if f, ok := setting(facts, other); ok {
		msg := f.guard.subject() + " " + verb("bounds", "bound", would) + " " + name +
			" only " + side + "; " + name + " may be " + past + limit.String()
		return append(notes, note(f.span(), msg))
	}
	return notes
}

// bindsOther reports whether f bounds its subject by another value, from
// above: by a string's length, or by a relation (see fact.relational),
// which the relations in force may chain to a bound by a known value.
func bindsOther(f fact) bool {
	return !f.lenOf.isZero() || f.relational()
}
