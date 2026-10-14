package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
)

// Exclusivity. During a call, a place passed as mut is reachable only
// through its parameter, so no two mut arguments of one call may overlap:
// the same place twice, mut x and mut x, or a place and a part of it,
// mut p and mut p.x, or mut a and mut a[i]. Places are compared as paths,
// a variable and the fields and elements after it, so different fields
// of one struct never overlap, swap(mut p.x, mut p.y). Two elements of
// one array overlap unless their indexes are known to differ: known
// values that differ, or values the facts in force keep apart, such as i
// in [0, 1] and j in [2, 3]. Indexes the same, a known value or the same
// variable, are the same element.
//
// A plain argument that reads a place passed as mut in the same call is
// not an error: it is evaluated, a copy, before the call begins, left to
// right with the rest, and the backend passes it by value, so the callee
// never sees it change.

// overlap says how two places relate.
type overlap int

const (
	// disjoint places share nothing.
	disjoint overlap = iota
	// same places are one place.
	same
	// contains is a place holding the other, its prefix.
	contains
	// within is a place inside the other.
	within
	// mayOverlap is two places that may share an element, at indexes not
	// known to differ.
	mayOverlap
)

// checkExclusive reports each of muts, the mut arguments of one call
// whose places were checked without error, in order, that overlaps an
// earlier one, with a note at the earlier one. Each is reported once,
// against the first it overlaps.
func (c *Checker) checkExclusive(muts []*ast.MutArg) {
	for j, later := range muts {
		for _, earlier := range muts[:j] {
			if how := c.overlapOf(earlier.X, later.X); how != disjoint {
				c.reportOverlap(earlier, later, how)
				break
			}
		}
	}
}

// reportOverlap reports later, a mut argument whose place relates to
// earlier's as how says, with a note at earlier.
func (c *Checker) reportOverlap(earlier, later *ast.MutArg, how overlap) {
	first, second := c.text(earlier.X.Spanned()), c.text(later.X.Spanned())
	notes := []diag.Diagnostic{note(earlier.Span, "first passed as mut here")}
	const only = "a mut argument must be the only way to reach %s during the call"
	switch how {
	case same:
		c.errorNotes(
			later.Span,
			notes,
			"%s is passed as mut twice in this call; "+only,
			second,
			second,
		)
	case contains:
		c.errorNotes(
			later.Span,
			notes,
			"%s is part of %s, and both are passed as mut in this call; "+only,
			second,
			first,
			second,
		)
	case within:
		c.errorNotes(
			later.Span,
			notes,
			"%s is part of %s, and both are passed as mut in this call; "+only,
			first,
			second,
			first,
		)
	default:
		c.errorNotes(later.Span, notes,
			"%s and %s may overlap, and both are passed as mut in this call; "+
				"elements of one array can be passed as mut together only at indexes known to differ",
			first, second)
	}
}

// step is one step of a place's path: a field, by name, or an element, by
// its index.
type step struct {
	index ast.Expr // nil for a field
	field string
}

// path returns the variable or parameter a place starts from, and the
// fields and elements that follow it, outermost first.
func path(place ast.Expr) (ast.Decl, []step) {
	var steps []step
	for {
		switch x := place.(type) {
		case *ast.Selector:
			steps = append(steps, step{field: x.Name})
			place = x.X
		case *ast.Index:
			steps = append(steps, step{index: x.Index})
			place = x.X
		case *ast.Ident:
			for i, j := 0, len(steps)-1; i < j; i, j = i+1, j-1 {
				steps[i], steps[j] = steps[j], steps[i]
			}
			return x.Def, steps
		default:
			return nil, nil
		}
	}
}

// overlapOf returns how the places a and b, both of a checked mut
// argument, relate.
func (c *Checker) overlapOf(a, b ast.Expr) overlap {
	rootA, stepsA := path(a)
	rootB, stepsB := path(b)
	if rootA == nil || rootA != rootB {
		return disjoint
	}
	maybe := false
	n := min(len(stepsA), len(stepsB))
	for i := range n {
		switch c.stepOverlap(stepsA[i], stepsB[i]) {
		case disjoint:
			return disjoint
		case mayOverlap:
			maybe = true
		}
	}
	switch {
	case maybe:
		return mayOverlap
	case len(stepsA) == len(stepsB):
		return same
	case len(stepsA) < len(stepsB):
		return contains
	default:
		return within
	}
}

// stepOverlap returns how two steps at the same depth of two paths from
// one variable relate: same, disjoint, or mayOverlap for two indexes not
// known to be equal or to differ. A field and an index never meet at one
// depth, since the value there has one type.
func (c *Checker) stepOverlap(a, b step) overlap {
	if a.index == nil || b.index == nil {
		if a.field == b.field {
			return same
		}
		return disjoint
	}
	if samePath(a.index, b.index) {
		return same
	}
	x, okX := c.indexInterval(a.index)
	y, okY := c.indexInterval(b.index)
	if !okX || !okY {
		return mayOverlap
	}
	return intervalOverlap(x, y)
}

// intervalOverlap returns how two indexes relate, given what is known
// about each: disjoint when no value one allows is one the other allows,
// same when both allow exactly one, equal, value, and mayOverlap
// otherwise.
func intervalOverlap(x, y interval) overlap {
	switch {
	case x.hasHi && y.hasLo && x.hi.Cmp(y.lo) < 0, y.hasHi && x.hasLo && y.hi.Cmp(x.lo) < 0:
		return disjoint
	case x.single() && y.single() && x.lo.Cmp(y.lo) == 0:
		return same
	default:
		return mayOverlap
	}
}

// indexInterval returns what is known about the value of an index: a
// known value, or what its type and the facts in force say about a
// variable, and false for anything else.
func (c *Checker) indexInterval(e ast.Expr) (interval, bool) {
	if v, ok := c.knownValue(e); ok {
		return interval{lo: v, hi: v, hasLo: true, hasHi: true}, true
	}
	if id, ok := e.(*ast.Ident); ok {
		return c.intervalOf(id), true
	}
	return interval{}, false
}

// single reports whether known allows exactly one value.
func (known interval) single() bool {
	return known.hasLo && known.hasHi && known.lo.Cmp(known.hi) == 0
}
