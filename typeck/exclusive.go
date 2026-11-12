package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
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
// checkListBorrows), and so is a view a call returns, read in place,
// alongside a mut argument that may overlap what it may point into (see
// checkViewBorrows). An index and a slice read their list, or the view a
// call returns, in place as well, after the index or the bounds are
// evaluated, so a mut argument in those that may overlap what they read
// is an error (checkIndexBorrows, checkSliceBorrows), and an element of a
// list is assigned in place, so the value assigned cannot pass the list,
// or anything holding it, as mut (checkAssignBorrows).

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
// receiver among them, that reads a list in place while another operand
// of the same call, or a call nested in one, passes a place that may
// overlap it as mut, or holds a closure that may write the variable it
// starts from (see writersIn), with a note at that mut argument or write. A mut argument
// inside the argument itself is in one of its indexes or slice bounds,
// which checkIndexBorrows and checkSliceBorrows report. An
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
// An argument that is a view a call returns is checked by
// checkViewBorrows, first, and a variable passed as own, which nothing
// else in the call may name, by checkOwnExclusive, before that.
func (c *Checker) checkListBorrows(call *ast.Call) {
	c.checkOwnExclusive(call)
	c.checkViewBorrows(call)
	c.checkMutArenaElements(call)
	operands := call.Operands()
	for i, arg := range operands {
		if appendedValue(call, i) {
			continue
		}
		place := c.borrowedList(arg)
		if place == nil {
			continue
		}
		for _, w := range writersOutside(operands, i) {
			if placesMayOverlap(place, w.place) {
				c.errorNotes(
					arg.Spanned(),
					[]diag.Diagnostic{w.note()},
					"cannot pass %s alongside %s in this call: %s is not copied for a call, "+
						"and %s during it",
					c.text(place.Spanned()),
					w.what(c),
					c.borrowHolder(place, "a list"),
					w.only(c),
				)
				break
			}
		}
	}
}

// appendedValue reports whether the operand at i of call is a value a call
// to append, add, add_all or insert adds, not a spread, which is copied, or moved when it is
// owned, and so no borrow: a move out of a list's element is reported as
// one (see move.go). The value replace stores is checked as a borrow, so
// an owned one that may overlap the place, replace(mut l, l), is
// reported.
func appendedValue(call *ast.Call, i int) bool {
	return ((isAppend(call) || givesWithError(call)) && !call.Spread || isPut(call)) && i > 0
}

// borrowedList returns the place arg reads a list from in place, when it
// does: arg itself, a place of an owned type, or what a slice of a list
// slices, at any depth of slices, or the arena a span's view of one is
// of, a[s], whose elements are read where the arena holds them. It
// returns nil for anything else, a mut argument included.
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
	if c.isSpanView(arg) && ast.Root(arg) != nil {
		return arg
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

// pendingAssign is an assignment whose target is being checked: the
// target, a variable, an element or a field, and the value it is given,
// which has been checked already.
type pendingAssign struct {
	target ast.Expr
	value  ast.Expr
}

// checkCompareBorrows reports an operand of bin, a comparison of two
// values holding lists, that is a place read in place, a list or a
// struct or a union holding one (borrowedList), while the other operand
// passes as mut, at any depth, a place that may overlap it, or holds a
// closure that may write it (see writersIn), with a note at that mut
// argument or write. Both operands are read where they are, as an
// array's are not: an array is copied before the other operand runs (see
// codegen/c), but a list is not copied to be compared, since copying one
// would allocate, so a call in the other operand that grew it could free
// the buffer the comparison then reads.
func (c *Checker) checkCompareBorrows(bin *ast.Binary) {
	operands := []ast.Expr{bin.X, bin.Y}
	for i, operand := range operands {
		place := c.borrowedList(operand)
		if place == nil {
			continue
		}
		for _, w := range writersIn([]ast.Expr{operands[1-i]}) {
			if placesMayOverlap(place, w.place) {
				c.errorNotes(
					operand.Spanned(),
					[]diag.Diagnostic{w.note()},
					"cannot compare %s alongside %s in the other operand: a list is not copied "+
						"to be compared, and %s during its call",
					c.text(operand.Spanned()),
					w.what(c),
					w.only(c),
				)
				return
			}
		}
	}
}

// checkIndexBorrows reports ix, an element read or assigned, when what it
// indexes is read in place, a list or a part of one (borrowedList) or a
// view a call returns (viewBorrows), while its index passes as mut, at any
// depth, a place that may overlap it, with a note at that mut argument,
// and reports whether it did. The list is not copied to be indexed, so a
// call in the index that grows it could free the buffer the element is
// then read from; an array is copied instead (see codegen/c), and a
// string's bytes never change.
func (c *Checker) checkIndexBorrows(ix *ast.Index) bool {
	return c.checkReadBorrows(ix.X, []ast.Expr{ix.Index}, "index", "in its index", "indexed")
}

// checkSliceBorrows reports s, a slice, when what it slices is read in
// place, as checkIndexBorrows says, while a bound passes as mut a place
// that may overlap it, and reports whether it did.
func (c *Checker) checkSliceBorrows(s *ast.Slice) bool {
	return c.checkReadBorrows(s.X, []ast.Expr{s.Lo, s.Hi}, "slice", "in its bounds", "sliced")
}

// checkReadBorrows reports x, what an index or a slice reads, when it is
// read in place and a mut argument in operands, at any depth, or a
// closure in them that writes a variable (see writersIn), may overlap it
// or what the view it is may point into, with a note at that mut argument
// or write, and reports whether it did. verb, where and done word the
// message: "index", "in its index" and "indexed" for an index. An operand
// may be nil, a bound left out.
func (c *Checker) checkReadBorrows(x ast.Expr, operands []ast.Expr, verb, where, done string) bool {
	ws := writersIn(operands)
	if len(ws) == 0 {
		return false
	}
	if place := c.borrowedList(x); place != nil {
		for _, w := range ws {
			if placesMayOverlap(place, w.place) {
				holder := "a list"
				if c.table.IsMap(ast.TypeOf(x)) {
					holder = "a map"
				}
				holder = c.borrowHolder(x, holder)
				c.errorNotes(
					x.Spanned(),
					[]diag.Diagnostic{w.note()},
					"cannot %s %s alongside %s %s: %s is not copied to be %s, "+
						"and %s during its call",
					verb,
					c.text(x.Spanned()),
					w.what(c),
					where,
					holder,
					done,
					w.only(c),
				)
				return true
			}
		}
		return false
	}
	call := c.viewCallOf(x)
	if call == nil {
		return false
	}
	for _, place := range c.viewBorrows(call) {
		for _, w := range ws {
			if placesMayOverlap(place, w.place) {
				c.errorNotes(
					x.Spanned(),
					[]diag.Diagnostic{w.note()},
					"cannot %s %s alongside %s %s: the view it returns may point into %s, "+
						"and %s during its call",
					verb,
					c.text(x.Spanned()),
					w.what(c),
					where,
					c.text(place.Spanned()),
					w.only(c),
				)
				return true
			}
		}
	}
	return false
}

// checkAssignBorrows reports the target of the assignment being checked
// when ix, an element of a list, an arena or a pool or a value of a map of type
// base on the target's path, is assigned while the value passes as mut, at any depth,
// that list or map or a place holding it, or holds a closure that writes
// the variable it is in (see writersIn), with a note at that mut argument
// or write, and reports whether it did. The element is assigned in place,
// in the list's or the map's buffer, which such a call could move or free.
// A mut argument inside it, an element or a field of one, may be passed:
// it cannot move its own buffer. It reports nothing outside an
// assignment's target.
func (c *Checker) checkAssignBorrows(ix *ast.Index, base types.Type) bool {
	a := c.assigning
	isMap := c.table.IsMap(base)
	if a == nil || !c.table.IsList(base) && !isMap && !c.table.IsArena(base) && !c.table.IsPool(base) {
		return false
	}
	holder := "an element is assigned in place in its list"
	switch {
	case isMap:
		holder = "a value is assigned in place in its map"
	case c.table.IsArena(base):
		holder = "an element is assigned in place in its arena"
	case c.table.IsPool(base):
		holder = "an element is assigned in place in its pool"
	}
	root, steps := path(ix.X)
	if root == nil {
		return false
	}
	for _, w := range writersIn([]ast.Expr{a.value}) {
		_, inside := path(w.place)
		if !placesMayOverlap(ix.X, w.place) || len(inside) > len(steps) {
			continue
		}
		c.errorNotes(
			a.target.Spanned(),
			[]diag.Diagnostic{w.note()},
			"cannot assign to %s alongside %s in its value: %s, "+
				"and %s during its call",
			c.text(a.target.Spanned()),
			w.what(c),
			holder,
			w.only(c),
		)
		return true
	}
	return false
}
