package typeck

import (
	"math"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/types"
)

// A three-clause loop's body knows what its condition says, as any loop's
// does. A counting loop tells it one thing more: the bound its variable
// starts from. A counting loop
//
//	for mut i := c; cond; post { body }
//
// declares i with a known value c, steps it in post by a known amount, up
// by i++ or i += k, or down by i-- or i -= k, with k a known positive
// value, and has a condition that bounds i on the side it moves towards:
// above when it counts up, below when it counts down. The body then knows
// that i is at least c, counting up, or at most c, counting down.
//
// That holds at every entry to the body. i starts at c, and nothing but
// post changes it: the body may not assign it, which assume checks for
// this fact like any other, and a condition or an init cannot. post runs
// only after the condition held on entry to the body, so i was within the
// condition's bound, and the step from there must not wrap past the end of
// i's type, which countingFact checks: hi + k is at most the type's
// largest value, counting up. So post only ever moves i further from c,
// and every later entry still finds i on c's side. continue runs post too,
// and break leaves the loop. The fact is put in force for the body alone,
// not the condition, the post, or the code after the loop.

// counting says whether a loop counts.
type counting int

const (
	// notCounting is a loop that does not count.
	notCounting counting = iota
	// counts is a counting loop.
	counts
	// wraps is a loop that would count but for a step that could wrap past
	// the end of its variable's type.
	wraps
)

// countingFact returns the fact a counting loop gives its body about its
// variable, the bound it counts from, given cond, the facts its condition
// gives when true, and whether the loop counts. For a loop whose step
// could wrap, it returns the fact the loop would give.
func (c *Checker) countingFact(loop *ast.For, cond []fact) (fact, counting) {
	v, ok := loop.Init.(*ast.Var)
	if !ok || !v.Mutable || v.Value == nil || !types.IsInteger(v.Type) {
		return fact{}, notCounting
	}
	start, ok := c.knownValue(v.Value)
	if !ok {
		return fact{}, notCounting
	}
	step, ok := c.counterStep(loop.Post, v)
	if !ok {
		return fact{}, notCounting
	}
	f := fact{about: declKey(v), lo: start, hasLo: true}
	if step.IsNegative() {
		f = fact{about: declKey(v), hi: start, hasHi: true}
	}
	if !stepStaysInType(v, step, cond) {
		return f, wraps
	}
	return f, counts
}

// wrapDrop returns why the fact f, which the counting loop would give its
// body, does not hold: its post could step its variable past the end of
// its type. For a target-width type the text names the 32-bit target,
// the only one on which that end is types.PortableMax.
func wrapDrop(loop *ast.For, f fact) *drop {
	v, _ := f.about.decl.(*ast.Var)
	lo, hi := stepBounds(v.Type)
	past, start, side := hi, f.lo, "above"
	if f.hasHi {
		past, start, side = lo, f.hi, "below"
	}
	prefix := ""
	if types.TargetWidth(v.Type) {
		prefix = "on a 32-bit target, "
	}
	return &drop{
		kind: dropWraps,
		at:   loop.Post.Spanned(),
		text: prefix + "this step could take " + v.Name + " past " + past.String() +
			", so the loop does not keep " + v.Name + " at or " + side + " " + start.String(),
	}
}

// stepBounds returns the smallest and largest values of the integer type
// typ that a counter's step must stay within to be sure not to wrap: its
// type's bounds, except that a target-width type is sure to hold values
// only up to types.PortableMax, where it may wrap on a 32-bit target.
func stepBounds(typ types.Type) (lo, hi constant.Int) {
	lo, hi = typeBounds(typ)
	if types.TargetWidth(typ) {
		hi = constant.FromUint64(types.PortableMax)
	}
	return lo, hi
}

// counterStep returns how much post changes the variable v by, when it
// steps v by a known non-zero amount: 1 for v++, -1 for v--, k for
// v += k and -k for v -= k, with k a known positive value.
func (c *Checker) counterStep(post ast.Stmt, v *ast.Var) (constant.Int, bool) {
	switch p := post.(type) {
	case *ast.IncDec:
		if !isVar(p.Target, v) {
			return constant.Int{}, false
		}
		if p.Dec {
			return constant.FromInt64(-1), true
		}
		return constant.FromInt64(1), true
	case *ast.Assign:
		if !isVar(p.Target, v) {
			return constant.Int{}, false
		}
		return c.compoundStep(p)
	default:
		return constant.Int{}, false
	}
}

// compoundStep returns how much the compound assignment a changes its
// target by, when it is += or -= of a known positive value: that value,
// negated for -=.
func (c *Checker) compoundStep(a *ast.Assign) (constant.Int, bool) {
	if a.Op != ast.Add && a.Op != ast.Sub {
		return constant.Int{}, false
	}
	k, known := c.knownValue(a.Value)
	if !known || k.IsZero() || k.IsNegative() {
		return constant.Int{}, false
	}
	if a.Op == ast.Sub {
		return k.Neg()
	}
	return k, true
}

// isVar reports whether e names the variable v.
func isVar(e ast.Expr, v *ast.Var) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Def == v
}

// stepStaysInType reports whether stepping the variable v by step, from
// any value the condition's facts cond allow it on entry to the body,
// stays within v's type, so the step never wraps: counting up, the
// highest such value plus step is at most the type's largest; counting
// down, the lowest plus step is at least its smallest. A bound by len(s)
// keeps v below the largest int64, which no length exceeds.
func stepStaysInType(v *ast.Var, step constant.Int, cond []fact) bool {
	typeLo, typeHi := stepBounds(v.Type)
	known := interval{lo: typeLo, hi: typeHi, hasLo: true, hasHi: true}
	belowLen := fact{hi: constant.FromInt64(math.MaxInt64 - 1), hasHi: true}
	for _, f := range cond {
		if f.about != declKey(v) {
			continue
		}
		known = known.narrow(f)
		if !f.lenOf.isZero() {
			known = known.narrow(belowLen)
		}
	}
	if step.IsNegative() {
		after, ok := known.lo.Add(step)
		return ok && after.Cmp(typeLo) >= 0
	}
	after, ok := known.hi.Add(step)
	return ok && after.Cmp(typeHi) <= 0
}
