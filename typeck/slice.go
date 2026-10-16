package typeck

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// A slice, x[i:j], is the bytes of a string, or the elements of an array, a
// view or a list, from i up to, but not including, j. A bound left out is 0 for i
// and len(x) for j. A string's slice is a string, a value like any other,
// storable anywhere. An array's, a view's or a list's is a view of its element type,
// which is usable only where a view parameter's value would be one: passed
// to a view parameter, given to len, or sliced again, the result of which
// is under the same rule; anywhere else it would need to be stored, and is
// reported. An array must be a place, as for a view parameter (see
// viewArg), since a view of a temporary would outlive it.
//
// The bounds are integers of any types, and must be proven to satisfy
// 0 <= i <= j <= len(x), each of the three by what proves an index (see
// facts.go): a known value, a variable's type and the facts in force, and
// two relations only a slice asks about, j <= len(x) and i <= j. A bound
// left out needs no proof of its own, but still takes part in the order:
// x[:j] needs 0 <= j and x[i:] needs i <= len(x). A known bound that is
// negative, past a known length, or past the other known bound is always
// an error, the checked form included. A string's slice that is not
// proven may be bound with ok instead, t, ok := s[i:j], which gives "" and
// false when the bounds are out of range; a view's cannot, since the view
// would need a variable, which only a parameter may be.
//
// A slice is never a place: it is not assigned, incremented or passed as
// mut, and neither is an element of one.

// slice checks a slice used as a value and returns its type, which it
// records on s: a string for a string's slice, and a view of the elements
// for an array's or a view's, which only a view parameter's argument, len
// and another slice may take, as viewSlice says. Its bounds are checked
// by sliceInRange, unless it is a view where one cannot be, which is
// reported instead and gives types.Invalid.
func (c *Checker) slice(s *ast.Slice) types.Type {
	viewOK := c.viewSlice == s
	c.viewSlice = nil
	s.Type = c.sliceType(s)
	switch {
	case s.Type == types.Invalid:
	case c.table.IsView(s.Type) && !viewOK:
		c.errorf(s.Span, "%s is a view, %s, which can only be passed to a view parameter or to len",
			c.text(s.Span), c.typeName(s.Type))
		s.Type = types.Invalid
	default:
		c.sliceInRange(s)
	}
	return s.Type
}

// allowView marks e, when it is a slice or a call, as written where a
// view may be, so that checking it does not report it as a view out of
// place: a slice of an array, a view or a list, or a call that returns a
// view (see viewCallValue).
func (c *Checker) allowView(e ast.Expr) {
	switch x := e.(type) {
	case *ast.Slice:
		c.viewSlice = x
	case *ast.Call:
		c.viewCall = x
	}
}

// sliceType checks what s slices and its bounds, and returns the slice's
// type: a string's is a string, and an array's, a view's or a list's a view of its
// elements. An array or a list must be a place. Anything else, a bound that is not
// an integer and an array that is a temporary are reported, and give
// types.Invalid. What s slices may itself be a slice, which s then takes
// as len would.
func (c *Checker) sliceType(s *ast.Slice) types.Type {
	c.allowView(s.X)
	base := c.valueOf(s.X, c.expr(s.X))
	c.viewSlice, c.viewCall = nil, nil
	loOK, hiOK := c.sliceBound(s.Lo), c.sliceBound(s.Hi)
	var typ types.Type
	switch {
	case base == types.Invalid:
		return types.Invalid
	case base == types.String, c.table.IsView(base):
		typ = base
	case c.table.IsList(base):
		if !c.isPlace(s.X) {
			c.errorf(s.X.Spanned(), "cannot slice a temporary list; store it in a variable first")
			return types.Invalid
		}
		typ = c.table.View(c.table.Elem(base))
	case c.table.IsArray(base):
		if !c.isPlace(s.X) {
			c.errorf(s.X.Spanned(), "cannot slice a temporary array; store it in a variable first")
			return types.Invalid
		}
		typ = c.table.View(c.table.Elem(base))
	default:
		c.errorf(s.X.Spanned(), "cannot slice %s", c.typeName(base))
		return types.Invalid
	}
	if !loOK || !hiOK {
		return types.Invalid
	}
	return typ
}

// sliceBound checks one bound of a slice, which must be an integer of any
// type, and reports whether it is one; a bound left out, nil, is.
func (c *Checker) sliceBound(b ast.Expr) bool {
	if b == nil {
		return true
	}
	got := c.valueOf(b, c.expr(b))
	switch {
	case got == types.Invalid:
		return false
	case !types.IsInteger(got):
		c.errorf(b.Spanned(), "slice bound must be an integer, got %s", c.typeName(got))
		return false
	default:
		return true
	}
}

// sliceResults checks a slice bound to want names, which must be two: the
// slice, or "" when its bounds are out of range, and whether they were in
// range. The form needs no proof, and is allowed where one exists, when ok
// is always true; a known bound that is always out of range is reported by
// sliceInRange. Only a string's slice can be bound so: an array's or a
// view's is a view, which no variable may hold.
func (c *Checker) sliceResults(s *ast.Slice, want int, span ast.Span) ([]types.Type, bool) {
	c.checkedSlice, c.viewSlice = s, s
	typ := c.expr(s)
	c.checkedSlice, c.viewSlice = nil, nil
	switch {
	case typ == types.Invalid:
		return nil, false
	case typ != types.String:
		c.errorf(
			s.Span,
			"%s is a view, %s, which cannot be bound with ok; prove its bounds instead",
			c.text(s.Span),
			c.typeName(typ),
		)
		return nil, false
	case want != 2:
		c.errorf(span, "assignment mismatch: %d variables but a slice gives 2 values", want)
		return nil, false
	}
	return []types.Type{typ, types.Bool}, true
}

// sliceInRange reports the bounds of s when they are not proven to be in
// range: a known bound out of range whatever s slices, or the two out of
// order, always, and otherwise, unless s is being bound with ok, whatever
// sliceNeeds finds missing, with the check that would prove it.
func (c *Checker) sliceInRange(s *ast.Slice) {
	if c.constantSliceMistake(s) || c.checkedSlice == s {
		return
	}
	need := c.sliceNeeds(s)
	if !need.any() {
		return
	}
	msg, notes := c.sliceError(s, need)
	c.errorNotes(s.Bounds(), notes, "%s", msg)
}

// constantSliceMistake reports, and reports whether it found, a known
// bound of s that no length makes valid: negative, or past the length of
// what s slices when that is known, or a known low bound above a known
// high one.
func (c *Checker) constantSliceMistake(s *ast.Slice) bool {
	if c.constantBoundOutside(s, s.Lo) || c.constantBoundOutside(s, s.Hi) {
		return true
	}
	if s.Lo == nil || s.Hi == nil {
		return false
	}
	lo, okLo := c.knownValue(s.Lo)
	hi, okHi := c.knownValue(s.Hi)
	if okLo && okHi && lo.Cmp(hi) > 0 {
		c.errorf(s.Bounds(), "slice bounds %s:%s are out of order", lo, hi)
		return true
	}
	return false
}

// constantBoundOutside reports, and reports whether it is, the bound b of
// s, nil when left out, when it is known and outside the range any slice
// of what s slices allows: negative, or past its length when that is
// known.
func (c *Checker) constantBoundOutside(s *ast.Slice, b ast.Expr) bool {
	if b == nil {
		return false
	}
	v, ok := c.knownValue(b)
	if !ok {
		return false
	}
	n, nKnown := c.extentOf(s.X)
	if !v.IsNegative() && (!nKnown || v.Cmp(constant.FromInt64(n)) <= 0) {
		return false
	}
	c.errorf(b.Spanned(), "slice bound %s is out of range for %s", v, c.extentName(s.X, n, nKnown))
	return true
}

// extentName names what x, sliced, is in an error about a bound out of
// range for it: an array by its type, and a string, a view or a list by
// what it is, with its length when that, n, is known, which a list's
// never is.
func (c *Checker) extentName(x ast.Expr, n int64, known bool) string {
	typ := ast.TypeOf(x)
	switch {
	case c.table.IsArray(typ):
		return c.typeName(typ)
	case typ == types.String && known:
		return "a string of " + plural64(n, "byte")
	case typ == types.String:
		return "a string"
	case c.table.IsList(typ):
		return "a list"
	case known:
		return "a view of " + plural64(n, "element")
	default:
		return "a view"
	}
}

// sliceNeed is what is missing from the proof that a slice's bounds are in
// range: that its low bound is not negative, lo; that its bounds are in
// order, order, which for a bound left out is that the other one is not
// negative, or not past the length; and that its high bound is not past
// the length, hi.
type sliceNeed struct {
	lo, order, hi bool
}

// any reports whether anything is missing.
func (n sliceNeed) any() bool {
	return n.lo || n.order || n.hi
}

// sliceNeeds returns what the facts in force do not prove about the bounds
// of s.
func (c *Checker) sliceNeeds(s *ast.Slice) sliceNeed {
	need := sliceNeed{
		lo: s.Lo != nil && !c.notNegative(s.Lo),
		hi: s.Hi != nil && !c.withinLen(s.Hi, s.X),
	}
	switch {
	case s.Lo == nil && s.Hi == nil:
	case s.Lo == nil:
		need.order = !c.notNegative(s.Hi)
	case s.Hi == nil:
		need.order = !c.withinLen(s.Lo, s.X)
	default:
		need.order = !c.inOrder(s.Lo, s.Hi, s.X)
	}
	return need
}

// notNegative reports whether the bound b is known not to be negative: a
// known value that is not, a len, which never is, or anything whose type,
// and for a subject the facts in force, keep it at or above 0.
func (c *Checker) notNegative(b ast.Expr) bool {
	lo, ok := c.lowest(b)
	return ok && !lo.IsNegative()
}

// lowest returns the least value the bound b can have where it is used,
// when there is one: its known value, 0 for a len, and otherwise what its
// type, and for a subject the facts in force, give it.
func (c *Checker) lowest(b ast.Expr) (constant.Int, bool) {
	if v, ok := c.knownValue(b); ok {
		return v, true
	}
	if isLenCall(b) {
		return constant.Int{}, true
	}
	known := typeInterval(b)
	if c.isSubject(b) {
		known = c.intervalOf(b)
	}
	return known.lo, known.hasLo
}

// highest returns the greatest value the bound b can have where it is
// used, when there is one: its known value, and otherwise what its type,
// and for a subject the facts in force, give it.
func (c *Checker) highest(b ast.Expr) (constant.Int, bool) {
	if v, ok := c.knownValue(b); ok {
		return v, true
	}
	known := typeInterval(b)
	if c.isSubject(b) {
		known = c.intervalOf(b)
	}
	return known.hi, known.hasHi
}

// withinLen reports whether the bound b is known to be at most the length
// of x, which s slices: a known value or a subject's upper bound at most a
// known length, or at most the least length the facts give x, when x is a
// subject; len(x) itself; or a subject a fact in force bounds by len(x),
// below it or at most it.
func (c *Checker) withinLen(b, x ast.Expr) bool {
	limit := c.minLen(x)
	if n, ok := c.extentOf(x); ok {
		limit = constant.FromInt64(n)
	}
	if v, ok := c.knownValue(b); ok {
		return v.Cmp(limit) <= 0
	}
	if c.isLenOf(b, x) {
		return true
	}
	if !c.isSubject(b) {
		return false
	}
	if known := c.intervalOf(b); known.hasHi && known.hi.Cmp(limit) <= 0 {
		return true
	}
	return c.belowLen(b, x) ||
		c.related(b, x, func(f fact, k factKey) bool { return f.atMostLen == k })
}

// inOrder reports whether the bound lo is known to be at most the bound
// hi, of a slice of x: they are the same subject; hi is len(x) and lo is
// at most it; the greatest value lo can have is at most the least hi can;
// or a fact in force puts lo at or below hi.
func (c *Checker) inOrder(lo, hi, x ast.Expr) bool {
	if c.sameSubject(lo, hi) {
		return true
	}
	if c.isLenOf(hi, x) {
		return c.withinLen(lo, x)
	}
	top, okTop := c.highest(lo)
	bottom, okBottom := c.lowest(hi)
	if okTop && okBottom && top.Cmp(bottom) <= 0 {
		return true
	}
	return c.related(lo, hi, func(f fact, k factKey) bool { return f.atMost == k })
}

// related reports whether a fact in force about the subject a relates it
// to the subject b as rel says, given the fact and b's key.
func (c *Checker) related(a, b ast.Expr, rel func(f fact, k factKey) bool) bool {
	ak, okA := c.subjectKey(a)
	bk, okB := c.subjectKey(b)
	if !okA || !okB {
		return false
	}
	for _, f := range c.active {
		if f.about == ak && f.inForce() && rel(f, bk) {
			return true
		}
	}
	return false
}

// sameSubject reports whether a and b are the same subject, a variable or
// a path below an immutable one.
func (c *Checker) sameSubject(a, b ast.Expr) bool {
	ak, okA := c.subjectKey(a)
	bk, okB := c.subjectKey(b)
	return okA && okB && ak == bk
}

// isLenOf reports whether e is len(x), x being a subject.
func (c *Checker) isLenOf(e, x ast.Expr) bool {
	arg, ok := lenArg(e)
	return ok && c.sameSubject(arg, x)
}

// extentOf returns the length of x, a string, an array or a view, when the
// checker knows it: an array's, from its type, a known string's, and a
// slice's whose bounds are known, each of them, or the length of what it
// slices for a high bound left out, so that x[1:3] has 2.
func (c *Checker) extentOf(x ast.Expr) (int64, bool) {
	if s, ok := x.(*ast.Slice); ok {
		return c.sliceExtent(s)
	}
	typ := ast.TypeOf(x)
	if c.table.IsArray(typ) {
		return c.table.Len(typ), true
	}
	if typ == types.String {
		return c.knownLen(x)
	}
	return 0, false
}

// sliceExtent returns the length of the slice s when its bounds are known,
// in order and not negative: the high bound, or the length of what s
// slices for one left out, less the low one, or 0 for one left out.
func (c *Checker) sliceExtent(s *ast.Slice) (int64, bool) {
	var lo, hi constant.Int
	if s.Lo != nil {
		v, ok := c.knownValue(s.Lo)
		if !ok {
			return 0, false
		}
		lo = v
	}
	if s.Hi != nil {
		v, ok := c.knownValue(s.Hi)
		if !ok {
			return 0, false
		}
		hi = v
	} else {
		n, ok := c.extentOf(s.X)
		if !ok {
			return 0, false
		}
		hi = constant.FromInt64(n)
	}
	n, ok := hi.Sub(lo)
	if !ok || lo.IsNegative() || n.IsNegative() || !n.Fits(64, true) {
		return 0, false
	}
	return n.Int64(), true
}

// knownSlice returns the value of s, a slice of a known string whose
// bounds are known and in range, which is a known string itself.
func (c *Checker) knownSlice(s *ast.Slice) (string, bool) {
	str, ok := c.knownString(s.X)
	if !ok {
		return "", false
	}
	lo, hi := int64(0), int64(len(str))
	for i, b := range []ast.Expr{s.Lo, s.Hi} {
		if b == nil {
			continue
		}
		v, known := c.knownValue(b)
		if !known || v.IsNegative() || v.Cmp(constant.FromInt64(int64(len(str)))) > 0 {
			return "", false
		}
		if i == 0 {
			lo = v.Int64()
		} else {
			hi = v.Int64()
		}
	}
	if lo > hi {
		return "", false
	}
	return str[lo:hi], true
}

// sliceError returns the error for the bounds of s that need is missing
// proofs of, and the notes that explain it. It suggests the check that
// would prove them when every part of it can be written and checked, and,
// for a string, the checked form.
func (c *Checker) sliceError(s *ast.Slice, need sliceNeed) (string, []diag.Diagnostic) {
	msg := c.boundsName(s) + " may be out of range"
	checks, ok := c.sliceChecks(s, need)
	if ok {
		msg += "; check " + strings.Join(checks, " && ") + " first"
	}
	if ast.TypeOf(s.X) == types.String {
		msg += orUse("v, ok := ..."+c.boundsForm(s), ok)
	}
	return msg, c.sliceNotes(s, need)
}

// boundsName names the bounds of s in an error, as written, each by its
// known value, its name, or ...: slice bounds i:j, or slice bound i, or j,
// when only one is written.
func (c *Checker) boundsName(s *ast.Slice) string {
	switch {
	case s.Lo != nil && s.Hi != nil:
		return "slice bounds " + c.indexName(s.Lo) + ":" + c.indexName(s.Hi)
	case s.Lo != nil:
		return "slice bound " + c.indexName(s.Lo)
	default:
		return "slice bound " + c.indexName(s.Hi)
	}
}

// boundsForm writes the bounds of s in brackets, as the checked form the
// error suggests writes them: [i:j], [i:] or [:j].
func (c *Checker) boundsForm(s *ast.Slice) string {
	var lo, hi string
	if s.Lo != nil {
		lo = c.indexName(s.Lo)
	}
	if s.Hi != nil {
		hi = c.indexName(s.Hi)
	}
	return "[" + lo + ":" + hi + "]"
}

// sliceChecks returns the comparisons that would prove what need is
// missing, in the order 0 <= i, i <= j, j <= len(x), with a bound left out
// standing as 0 or the length, and whether each can be written: every
// bound in one is a known value or a subject other than a mutable global,
// and the length, when one names it, is known or a subject's, len(x). A
// known value compared with a length is written as a check on the length,
// len(s) >= 1, as for an index.
func (c *Checker) sliceChecks(s *ast.Slice, need sliceNeed) ([]string, bool) {
	length := c.lengthText(s.X)
	var checks []string
	ok := true
	add := func(a, b ast.Expr) {
		left, right := "0", length
		if a != nil {
			left, ok = c.checkTerm(a, ok)
		}
		if b != nil {
			right, ok = c.checkTerm(b, ok)
		}
		if right == "" {
			ok = false
		}
		if a != nil && b == nil {
			if _, known := c.knownValue(a); known {
				checks = append(checks, right+" >= "+left)
				return
			}
		}
		checks = append(checks, left+" <= "+right)
	}
	if need.lo {
		add(nil, s.Lo)
	}
	if need.order {
		add(s.Lo, s.Hi)
	}
	if need.hi {
		add(s.Hi, nil)
	}
	return checks, ok
}

// checkTerm returns how a check names the bound b: its known value, or its
// name when it is a subject other than a mutable global, and ok as it was,
// or false when b is neither, and cannot be checked.
func (c *Checker) checkTerm(b ast.Expr, ok bool) (string, bool) {
	if v, known := c.knownValue(b); known {
		return v.String(), ok
	}
	if _, isGlobal := c.mutableGlobal(b); isGlobal || !c.isSubject(b) {
		return "", false
	}
	return c.nameOf(b), ok
}

// lengthText returns how a check names the length of x: a known length, or
// len(x) when x is a subject, other than a mutable global, whose length a
// check can bound, and "" otherwise.
func (c *Checker) lengthText(x ast.Expr) string {
	if n, ok := c.extentOf(x); ok {
		return fmt.Sprint(n)
	}
	return c.stringBound(x)
}

// sliceNotes returns the notes that explain why the bounds of s are not
// proven, as need says: for each bound that is a subject, what the guards
// in scope prove about it and why that falls short, as for an index; for
// one that is not, why a guard on a variable in it proves nothing; for a
// known bound, what the guards prove about the length of x; and for x,
// why its length cannot be proven, when it is a mutable global or a field
// of a mutable variable.
func (c *Checker) sliceNotes(s *ast.Slice, need sliceNeed) []diag.Diagnostic {
	var notes []diag.Diagnostic
	if need.lo {
		notes = append(notes, c.boundNotes(s, s.Lo, true)...)
	}
	switch {
	case !need.order:
	case s.Lo == nil:
		notes = append(notes, c.boundNotes(s, s.Hi, true)...)
	case s.Hi == nil:
		notes = append(notes, c.boundNotes(s, s.Lo, false)...)
	}
	if need.hi {
		notes = append(notes, c.boundNotes(s, s.Hi, false)...)
	}
	if ast.TypeOf(s.X) == types.String {
		notes = append(notes, c.indexedNotes(s.X)...)
	}
	return uniqueNotes(notes)
}

// boundNotes returns the notes that explain why the bound b of s is not
// proven at least 0, when lower is set, or at most the length of what s
// slices otherwise: for a subject, what the guards in scope prove about
// it, as for an index; for a known value past 0, what they prove about
// the length; and for anything else, why a guard on a variable in it
// proves nothing.
func (c *Checker) boundNotes(s *ast.Slice, b ast.Expr, lower bool) []diag.Diagnostic {
	if c.isSubject(b) {
		n, known := c.extentOf(s.X)
		unit := "byte"
		if ast.TypeOf(s.X) != types.String {
			unit = "element"
		}
		e := c.newExtent(s.X, unit, n, known)
		e.atEnd = true
		return c.explain(c.boundProof(b, e, lower, !lower))
	}
	if _, isKnown := c.knownValue(b); isKnown && !lower && c.isSubject(s.X) {
		return c.explain(c.lengthProof(s.X, func() bool { return c.withinLen(b, s.X) }))
	}
	return c.operandNotes(b)
}

// uniqueNotes returns notes without repeats, the first of each kept: a
// bound explained for two of the proofs it takes part in may get the same
// note from each.
func uniqueNotes(notes []diag.Diagnostic) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, n := range notes {
		if !slices.ContainsFunc(out, func(o diag.Diagnostic) bool {
			return o.Span == n.Span && o.Message == n.Message
		}) {
			out = append(out, n)
		}
	}
	return out
}

// boundProof returns the proof that the bound b, a subject, is at least 0,
// when lower is set, or at most the length of e, when upper is: e is a
// slice's extent, which a bound may reach.
func (c *Checker) boundProof(b ast.Expr, e extent, lower, upper bool) proof {
	about, _ := c.subjectKey(b)
	name := c.nameOf(b)
	proven := func() bool {
		return (!lower || c.notNegative(b)) && (!upper || c.withinLen(b, e.of))
	}
	return proof{
		about:     about,
		proven:    proven,
		relations: true,
		claim:     func(facts []fact) string { return describe(name, facts) },
		weak: func(facts []fact, would bool) []diag.Diagnostic {
			known := typeInterval(b)
			for _, f := range facts {
				known = known.narrow(f)
			}
			var notes []diag.Diagnostic
			if n, ok := lowerNote(name, facts, known, would); lower && ok {
				notes = append(notes, n)
			}
			if n, ok := c.upperNote(b, facts, known, e, would); upper && ok {
				notes = append(notes, n)
			}
			return notes
		},
	}
}

// notPlace reports target, a slice or an element of one, written where a
// place is needed, as what says: a slice is a value.
func (c *Checker) notPlace(span ast.Span, what string) {
	c.errorf(span, "cannot %s: a slice is not a place", what)
}
