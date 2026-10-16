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
// never sees it change. A list is the exception: it is never copied for a
// call, so an argument that reads one in place, borrowed or viewed, while
// the same call passes an overlapping place as mut, is an error (see
// checkListBorrows).

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
	ka, okA := c.subjectKey(a.index)
	kb, okB := c.subjectKey(b.index)
	if okA && okB && ka == kb {
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
// subject, a variable or a path below an immutable one, and false for
// anything else.
func (c *Checker) indexInterval(e ast.Expr) (interval, bool) {
	if v, ok := c.knownValue(e); ok {
		return interval{lo: v, hi: v, hasLo: true, hasHi: true}, true
	}
	if c.isSubject(e) {
		return c.intervalOf(e), true
	}
	return interval{}, false
}

// single reports whether known allows exactly one value.
func (known interval) single() bool {
	return known.hasLo && known.hasHi && known.lo.Cmp(known.hi) == 0
}

// checkListBorrows reports each argument of call, a method call's
// receiver among them, that reads a list in place while the same call, or a call nested in its arguments, passes a
// place that may overlap it as mut, with a note at that mut argument. An
// argument reads a list in place when it is an owned place, a list or a
// struct holding one, passed to a plain parameter, which borrows it, or a
// list, or a slice of one, passed to a view parameter. A plain argument
// of any other type is a copy, taken before the call, and an array viewed
// alongside a mut argument is viewed as a copy (see codegen/c); a list is
// never copied for a call, since copying one would allocate, and then
// growing it through the mut argument could free the buffer the borrow
// reads. Places overlap as codegen/c's mayOverlap says: two different
// fields never do, and two elements always may. A value append adds is
// no borrow: it is copied, or moved, out of the place it is read from.
func (c *Checker) checkListBorrows(call *ast.Call) {
	var muts []*ast.MutArg
	for _, arg := range call.Operands() {
		ast.Walk(arg, func(n ast.Node) bool {
			if m, ok := n.(*ast.MutArg); ok {
				muts = append(muts, m)
			}
			return true
		})
	}
	if len(muts) == 0 {
		return
	}
	for i, arg := range call.Operands() {
		if appendedValue(call, i) {
			continue
		}
		place := c.borrowedList(arg)
		if place == nil {
			continue
		}
		for _, m := range muts {
			if placesMayOverlap(place, m.X) {
				borrowed, passed := c.text(place.Spanned()), c.text(m.X.Spanned())
				c.errorNotes(
					arg.Spanned(),
					[]diag.Diagnostic{note(m.Span, "passed as mut here")},
					"cannot pass %s alongside mut %s in this call: a list is not copied for a call, "+
						"and a mut argument must be the only way to reach %s during it",
					borrowed,
					passed,
					passed,
				)
				break
			}
		}
	}
}

// appendedValue reports whether the operand at i of call is a value a call
// to append adds, not a spread, which is copied, or moved when it is
// owned, and so no borrow: a move out of a list's element is reported as
// one (see move.go).
func appendedValue(call *ast.Call, i int) bool {
	return isAppend(call) && !call.Spread && i > 0
}

// borrowedList returns the place arg reads a list from in place, when it
// does: arg itself, a place of an owned type, or what a slice of a list
// slices, at any depth of slices. It returns nil for anything else, a mut
// argument included.
func (c *Checker) borrowedList(arg ast.Expr) ast.Expr {
	if _, isMut := arg.(*ast.MutArg); isMut {
		return nil
	}
	for {
		s, ok := arg.(*ast.Slice)
		if !ok {
			break
		}
		arg = s.X
	}
	if ast.Root(arg) == nil || !c.table.Owned(ast.TypeOf(arg)) {
		return nil
	}
	return arg
}

// placesMayOverlap reports whether the places a and b may share memory:
// they start from the same variable or parameter, and no field of one
// differs from the other's at the same depth. Two elements always may.
func placesMayOverlap(a, b ast.Expr) bool {
	rootA, stepsA := path(a)
	rootB, stepsB := path(b)
	if rootA == nil || rootA != rootB {
		return false
	}
	for i := range min(len(stepsA), len(stepsB)) {
		if stepsA[i].index == nil && stepsB[i].index == nil && stepsA[i].field != stepsB[i].field {
			return false
		}
	}
	return true
}
