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

// countingFact returns the fact a counting loop gives its body about its
// variable, the bound it counts from, given cond, the facts its condition
// gives when true. It reports false when the loop does not count.
func (c *Checker) countingFact(loop *ast.For, cond []fact) (fact, bool) {
	v, ok := loop.Init.(*ast.Var)
	if !ok || !v.Mutable || v.Value == nil || !types.IsInteger(v.Type) {
		return fact{}, false
	}
	start, ok := c.knownValue(v.Value)
	if !ok {
		return fact{}, false
	}
	step, ok := c.counterStep(loop.Post, v)
	if !ok || !stepStaysInType(v, step, cond) {
		return fact{}, false
	}
	if step.IsNegative() {
		return fact{decl: v, hi: start, hasHi: true}, true
	}
	return fact{decl: v, lo: start, hasLo: true}, true
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
	typeLo, typeHi := typeBounds(v.Type)
	known := interval{lo: typeLo, hi: typeHi, hasLo: true, hasHi: true}
	belowLen := fact{hi: constant.FromInt64(math.MaxInt64 - 1), hasHi: true}
	for _, f := range cond {
		if f.decl != v {
			continue
		}
		known = known.narrow(f)
		if f.lenOf != nil {
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
