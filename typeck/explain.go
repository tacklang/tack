package typeck

import (
	"slices"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// An error for a missing proof comes with notes that explain it, when a
// guard in scope says something about the operand: the guard gave a fact
// that its region dropped, because the region assigns the variable, or a
// fact that holds but is too weak, such as an upper bound without a lower
// one. Each note's span is the code it is about, the guard or the
// assignment, so its text never prints a position. An operand no guard
// mentions keeps its error alone.
//
// A dropped fact is not thrown away: assume puts it in force marked with
// its drop, which every query that proves something skips, so it is there
// only for these notes. To say whether a dropped fact would have been
// enough, the proof is run again with every fact counted (supposing); when
// it still fails, the notes go on to say what the facts would have lacked.

// guard is where facts came from: a condition, a loop, a case or an early
// exit, whose span a note underlines. cond is the condition tested, when
// there is one.
type guard struct {
	cond ast.Expr
	span ast.Span
	kind guardKind
}

// guardKind says what a guard is, which decides how a note names it, and
// what its facts are true of.
type guardKind int

const (
	// guardCond is a condition being true: an if's, for its then branch, a
	// loop's, for its body, or the left side of && or ||, for the right.
	guardCond guardKind = iota
	// guardElse is an if's condition being false, in its else branch.
	guardElse
	// guardRange is a range loop, for its body.
	guardRange
	// guardCount is a counting loop's start, for its body.
	guardCount
	// guardCase is a switch case matching, in its body, or a case value not
	// matching, for the values after it.
	guardCase
	// guardLaterCase is a switch case not matching, in the cases after it.
	guardLaterCase
	// guardExit is an if or a switch whose branch leaves the block, for the
	// rest of the block.
	guardExit
	// guardMatched is a switch case whose body does not leave, every
	// other case that could reach past the switch leaving, for the rest
	// of the block: the code after the switch is reached only through
	// that case matching (see switchAfterFacts and switchChainFacts).
	guardMatched
	// guardStore is a declaration or an assignment relating what it stores
	// to what it was computed from, for the rest of the block (see
	// storedFacts).
	guardStore
	// guardEnsures is a call whose callee's contract gives facts about
	// its arguments, for the rest of the block (see ensures.go).
	guardEnsures
	// guardInvariant is a loop keeping a bound on a variable it writes,
	// for the whole loop (see invariant.go).
	guardInvariant
	// guardValue is a statement storing in a variable a known value, or a
	// value of known bounds, for the rest of the block (see storedFacts
	// and steppedFacts). Notes never describe its facts, and a subject
	// one holds about gets no note on what the facts allow, such as that
	// it may be negative (see heldValue).
	guardValue
	// guardFallthrough is a case a fallthrough enters, for its body: what
	// matching it and the fallthrough both give (see checkFallenInto).
	// Its facts are a join no note can point at a single cause of, so
	// notes treat them as a stored value's (see silent).
	guardFallthrough
)

// silent reports whether g's facts are ones no note describes, and that
// keep a subject they hold about from a note on what the facts allow: a
// stored value's, and a join's at a case a fallthrough enters.
func (g *guard) silent() bool {
	return g != nil && (g.kind == guardValue || g.kind == guardFallthrough)
}

// subject returns how a note names g.
func (g *guard) subject() string {
	switch g.kind {
	case guardRange, guardInvariant:
		return "this loop"
	case guardCount:
		return "counting from this start"
	case guardCase, guardLaterCase, guardMatched:
		return "this case"
	case guardExit:
		return "this early exit"
	case guardStore:
		return "this assignment"
	case guardEnsures:
		return "this call"
	default:
		return "this guard"
	}
}

// where returns what a note adds after what g's facts prove, to say where
// they are true, when that is not simply inside the region g starts.
func (g *guard) where() string {
	switch g.kind {
	case guardElse:
		return " in its else branch"
	case guardLaterCase:
		return " in the cases after it"
	case guardMatched:
		return " after the switch"
	case guardExit, guardStore, guardEnsures:
		return " after it"
	default:
		return ""
	}
}

// drop is why a fact does not hold where it is put in force, which a note
// points at, at: the region assigns name there, or passes it as mut to the
// function text there, or the loop's step there could wrap, which text then explains.
type drop struct {
	name string
	text string
	at   ast.Span
	kind dropKind
}

// dropKind says why a fact was dropped.
type dropKind int

const (
	// dropAssigned is a region assigning the fact's variable, or the
	// string whose length bounds it.
	dropAssigned dropKind = iota
	// dropWraps is a counting loop whose step could wrap past the end of
	// its variable's type, so the bound it counts from does not hold.
	dropWraps
	// dropMut is a region passing the fact's variable, or part of it, as
	// mut to a call, text, which may assign it.
	dropMut
)

// same reports whether d and other are the same reason, the same
// assignment.
func (d *drop) same(other *drop) bool {
	return d.kind == other.kind && d.at == other.at
}

// note returns the note that points at d's cause.
func (d *drop) note() diag.Diagnostic {
	switch d.kind {
	case dropWraps:
		return note(d.at, d.text)
	case dropMut:
		return note(d.at, "but "+d.name+" is passed as mut to "+d.text+" here, so the proof ends")
	default:
		return note(d.at, "but "+d.name+" is assigned here, so the proof ends")
	}
}

// note returns a note with message msg covering span.
func note(span ast.Span, msg string) diag.Diagnostic {
	return diag.Diagnostic{Severity: diag.Note, Message: msg, Span: span}
}

// proof is a missing proof to explain: about, the subject the facts that
// could prove it are about; proven, which runs the proof again; claim,
// which says what some facts about the variable prove; weak, which
// explains why facts that do not prove it fall short, phrased as what they
// would do when would is set; and, when set, relevant, which picks out
// the facts that could take part in the proof, so that no note explains
// a dropped fact that could not. relations is set for a slice bound's
// proof, the only one a relation (see fact.relational) can take part in.
type proof struct {
	proven    func() bool
	claim     func(facts []fact) string
	weak      func(facts []fact, would bool) []diag.Diagnostic
	relevant  func(f fact) bool
	about     factKey
	relations bool
}

// explain returns the notes for the missing proof p: for each fact about
// its variable that was dropped, unless a bound a loop keeps still says
// all it said (see implies), the guard that gave it and what dropped
// it, and then, if those facts would not have been enough, or none was
// dropped, why the facts fall short. A guard's facts that one reason
// dropped are explained only when one of them is relevant to p. A
// difference bound is left out: what relations fall short of is a note of
// its own (see relationNotes).
func (c *Checker) explain(p proof) []diag.Diagnostic {
	// A stored value's fact is never described, so where one holds about
	// the subject, no note says what the facts allow, which would have to
	// count it; the notes on which guard's proof a write ended stay. The
	// same goes for a subject that took part in a relation, or in the
	// sides of a || (see noteRelated), which a claim cannot follow.
	held := c.heldValue(p.about) || c.relatedSubjects[p.about]
	var facts, dropped []fact
	kept := c.keptBounds(p.about)
	for f := range c.factsAbout(p.about) {
		// A value ruled out, b != -1, takes part in no proof a note
		// explains, so no note describes it.
		if f.relational() && !p.relations || f.diff != nil || f.hasNe || f.guard.silent() {
			continue
		}
		f.drop = c.dropOf(f)
		if f.drop != nil && kept.implies(f) {
			continue
		}
		facts = append(facts, f)
		if f.drop != nil {
			dropped = append(dropped, f)
		}
	}
	dropped = relevantDrops(dropped, p.relevant)
	if len(dropped) == 0 {
		if held {
			return nil
		}
		return p.weak(facts, false)
	}
	notes := dropNotes(dropped, p.claim)
	if !c.supposing(p.proven) && !held {
		// What the facts would fall short of is said with the dropped
		// facts counted, so whatever a note checks against the facts in
		// force, such as a relation that refutes it, is checked under the
		// same supposition.
		c.supposing(func() bool {
			notes = append(notes, p.weak(facts, true)...)
			return false
		})
	}
	return notes
}

// keptBounds returns what the bounds a loop keeps on the subject k (see
// invariant.go) say of it together. Such a bound never ends, and a
// difference bound says nothing of k's own value.
func (c *Checker) keptBounds(k factKey) interval {
	var known interval
	for f := range c.factsAbout(k) {
		if f.guard != nil && f.guard.kind == guardInvariant {
			known = known.narrow(f)
		}
	}
	return known
}

// implies reports whether known says everything f does: f bounds its
// subject only by values, and known bounds it at least as tightly on
// each side f does, and keeps it from zero when f does. Such a fact, once
// dropped, ends no proof: what it said still holds, so no note says it
// ends, and no note describes it beside the facts in force.
func (known interval) implies(f fact) bool {
	if !f.lenOf.isZero() || f.relational() {
		return false
	}
	return known.covers(f) && (!f.nonzero || known.excludesZero())
}

// covers reports whether known bounds a value at least as tightly as f
// does, on each side f bounds it.
func (known interval) covers(f fact) bool {
	lo := !f.hasLo || known.hasLo && known.lo.Cmp(f.lo) >= 0
	return lo && (!f.hasHi || known.hasHi && known.hi.Cmp(f.hi) <= 0)
}

// relevantDrops returns the dropped facts that came from a guard, and were
// dropped for a reason, that dropped at least one fact relevant reports
// true for, all of them when relevant is nil, so a note about a guard
// still says all that it proves.
func relevantDrops(dropped []fact, relevant func(f fact) bool) []fact {
	if relevant == nil {
		return dropped
	}
	var kept []fact
	for _, f := range dropped {
		if slices.ContainsFunc(dropped, func(o fact) bool {
			return o.guard == f.guard && o.drop.same(f.drop) && relevant(o)
		}) {
			kept = append(kept, f)
		}
	}
	return kept
}

// heldValue reports whether a fact a stored value gives, its bounds (see
// guardValue) or its relation to what it was computed from (guardStore),
// or a join's at a case a fallthrough enters (guardFallthrough), holds
// about k where checking has reached: a note on what the facts allow k,
// which counts none of them, is then left out.
func (c *Checker) heldValue(k factKey) bool {
	for f := range c.factsAbout(k) {
		if f.guard != nil && (f.guard.silent() || f.guard.kind == guardStore) && c.holds(f) {
			return true
		}
	}
	return false
}

// supposing runs proven with every fact in force counted, dropped ones
// included, and puts the facts back as they were before returning what it
// reported.
func (c *Checker) supposing(proven func() bool) bool {
	saved := c.active
	c.active = slices.Clone(saved)
	for i := range c.active {
		// A stored value's bounds, and a join's, stay as they are: notes
		// never name them, so a note must not rest on one held where it
		// is not.
		if g := c.active[i].guard; !g.silent() {
			c.active[i].drop, c.active[i].ends = nil, nil
		}
	}
	defer func() { c.active = saved }()
	return proven()
}

// dropNotes returns, for each reason some of the dropped facts were
// dropped, in the order the facts were put in force, a note at each guard
// whose facts it dropped, in source order, saying what they prove, and
// then one at the reason. claim says what some facts prove.
func dropNotes(dropped []fact, claim func(facts []fact) string) []diag.Diagnostic {
	var notes []diag.Diagnostic
	var done []*drop
	for _, f := range dropped {
		if slices.ContainsFunc(done, f.drop.same) {
			continue
		}
		done = append(done, f.drop)
		if f.drop.kind == dropWraps {
			notes = append(notes, f.drop.note())
			continue
		}
		for _, g := range guardsDroppedBy(dropped, f.drop) {
			var own []fact
			for _, o := range dropped {
				if o.guard == g && o.drop.same(f.drop) {
					own = append(own, o)
				}
			}
			notes = append(notes, note(g.span, g.subject()+" proves "+claim(own)+g.where()))
		}
		notes = append(notes, f.drop.note())
	}
	return notes
}

// guardsDroppedBy returns the guards of the facts d dropped, each once, in
// source order.
func guardsDroppedBy(dropped []fact, d *drop) []*guard {
	var guards []*guard
	for _, f := range dropped {
		if f.drop.same(d) && !slices.Contains(guards, f.guard) {
			guards = append(guards, f.guard)
		}
	}
	slices.SortStableFunc(guards, func(a, b *guard) int {
		return a.span.Start - b.span.Start
	})
	return guards
}

// span returns what a note about f underlines: the comparison, or the &&
// or ||, that gave it, or else its guard.
func (f fact) span() ast.Span {
	if f.src != nil {
		return f.src.Spanned()
	}
	return f.guard.span
}

// verb returns present, or its conditional form, would followed by the
// verb's base form, when would is set.
func verb(present, base string, would bool) string {
	if would {
		return "would " + base
	}
	return present
}

// describe says what facts, all about the subject called name, prove
// together: its bounds, a bound by a string's length, a relation, and that
// it is not zero, when its bounds do not already say so. An upper bound is
// written with <, as checks usually are.
func describe(name string, facts []fact) string {
	var known interval
	var lenOf factKey
	for _, f := range facts {
		known = known.narrow(f)
		if !f.lenOf.isZero() {
			lenOf = f.lenOf
		}
	}
	relations := relationParts(name, facts)
	parts := bounds(name, known, !lenOf.isZero() || len(relations) > 0)
	if !lenOf.isZero() {
		parts = append(parts, name+" < len("+lenOf.name()+")")
	}
	parts = append(parts, relations...)
	bounded := known
	bounded.nonzero = false
	if known.nonzero && !bounded.excludesZero() {
		parts = append(parts, name+" != 0")
	}
	return strings.Join(parts, " && ")
}

// relationParts writes the relations facts give the subject called name,
// in order: at most a length, name <= len(s), or at most another subject,
// name <= j.
func relationParts(name string, facts []fact) []string {
	var parts []string
	for _, f := range facts {
		switch {
		case !f.atMostLen.isZero():
			parts = append(parts, name+" <= len("+f.atMostLen.name()+")")
		case !f.atMost.isZero():
			parts = append(parts, name+" <= "+f.atMost.name())
		}
	}
	return parts
}

// bounds writes the bounds known gives the variable called name: one value,
// or a lower bound, written lo <= name when an upper bound follows, which a
// bound by a length does when byLen is set, and an upper one.
func bounds(name string, known interval, byLen bool) []string {
	if known.hasLo && known.hasHi && known.lo.Cmp(known.hi) == 0 {
		return []string{name + " == " + known.lo.String()}
	}
	var parts []string
	switch {
	case known.hasLo && (known.hasHi || byLen):
		parts = append(parts, known.lo.String()+" <= "+name)
	case known.hasLo:
		parts = append(parts, name+" >= "+known.lo.String())
	}
	if known.hasHi {
		parts = append(parts, below(name, known.hi))
	}
	return parts
}

// below writes the upper bound hi on the variable called name: name < hi+1,
// or name <= hi when hi+1 is out of range.
func below(name string, hi constant.Int) string {
	if next, ok := hi.Add(constant.FromInt64(1)); ok {
		return name + " < " + next.String()
	}
	return name + " <= " + hi.String()
}

// divisorProof returns the proof that the divisor e, a subject, is not
// zero.
func (c *Checker) divisorProof(e ast.Expr) proof {
	about, _ := c.subjectKey(e)
	name := c.nameOf(e)
	return proof{
		about:  about,
		proven: func() bool { return c.nonzero(e) },
		claim: func(facts []fact) string {
			var known interval
			for _, f := range facts {
				known = known.narrow(f)
			}
			if known.excludesZero() {
				return name + " != 0"
			}
			return describe(name, facts)
		},
		weak: func(facts []fact, would bool) []diag.Diagnostic {
			zero := constant.Int{}
			if len(facts) == 0 || c.rulesOut(e, zero) {
				return nil
			}
			f := facts[len(facts)-1]
			msg := f.guard.subject() + " " + verb(
				"allows",
				"allow",
				would,
			) + " " + name + " == 0"
			return []diag.Diagnostic{note(f.span(), msg)}
		},
	}
}

// extent is what an index must stay below: the length n of an array or a
// string, when known, and the array or string itself, of, whose items are
// called unit, and which notes call label. A slice's bound may also equal
// the length, which atEnd says.
type extent struct {
	of    ast.Expr
	label string
	unit  string
	n     int64
	known bool
	atEnd bool
}

// newExtent returns the extent of of, an array or a string whose items are
// called unit, with n of them when known, labeled by its name when it is
// a subject, and as the array or the string otherwise.
func (c *Checker) newExtent(of ast.Expr, unit string, n int64, known bool) extent {
	label := "the array"
	switch {
	case c.isSubject(of):
		label = c.nameOf(of)
	case unit == "byte":
		label = "the string"
	}
	return extent{of: of, label: label, unit: unit, n: n, known: known}
}

// indexProof returns the proof that the index i, a subject, is in range
// for e, which proven checks. An unsigned i is never below 0, and one the
// facts in force keep at or above 0 is not either, so only an upper bound,
// by a value or by a length, can be missing, and a fact that gives none,
// such as the lower bound a counting loop counts from, is not relevant to
// it.
func (c *Checker) indexProof(i ast.Expr, e extent, proven func() bool) proof {
	about, _ := c.subjectKey(i)
	name := c.nameOf(i)
	var relevant func(f fact) bool
	if types.IsUnsigned(ast.TypeOf(i)) || c.atLeastZero(i) {
		relevant = func(f fact) bool { return f.hasHi || !f.lenOf.isZero() }
	}
	return proof{
		about:    about,
		proven:   proven,
		relevant: relevant,
		claim:    func(facts []fact) string { return describe(name, facts) },
		weak: func(facts []fact, would bool) []diag.Diagnostic {
			known := typeInterval(i)
			for _, f := range facts {
				known = known.narrow(f)
			}
			var notes []diag.Diagnostic
			if n, ok := c.lowerNote(i, facts, known, would); ok {
				notes = append(notes, n)
			}
			if n, ok := c.upperNote(i, facts, known, e, would); ok {
				notes = append(notes, n)
			}
			return notes
		},
	}
}

// atLeastZero reports whether the facts in force keep i, an index that
// is a subject, at or above 0 where it is used.
func (c *Checker) atLeastZero(i ast.Expr) bool {
	known := c.intervalOf(i)
	return known.hasLo && !known.lo.IsNegative() || c.atLeast(i)
}

// typeInterval returns the bounds the type of e, a subject, gives it, none
// for a type without them. A target-width type's are the widest it has on
// any target (see typeBounds).
func typeInterval(e ast.Expr) interval {
	var known interval
	if typ := ast.TypeOf(e); types.MaxBits(typ) > 0 {
		known.lo, known.hi = typeBounds(typ)
		known.hasLo, known.hasHi = true, true
	}
	return known
}

// lowerNote explains why the facts about the index i, which with its
// type's bounds give known, do not keep it at or above 0: the fact that
// set the lower bound allows a negative value, or no fact gives a lower
// bound while one gives an upper. A value a != rules out is not one a
// note names (see rulesOut).
func (c *Checker) lowerNote(i ast.Expr, facts []fact, known interval, would bool) (diag.Diagnostic, bool) {
	if known.hasLo && !known.lo.IsNegative() {
		return diag.Diagnostic{}, false
	}
	name := c.nameOf(i)
	if f, ok := setting(
		facts,
		func(f fact) bool { return f.hasLo && f.lo.Cmp(known.lo) == 0 },
	); ok {
		if c.rulesOut(i, known.lo) {
			return diag.Diagnostic{}, false
		}
		msg := f.guard.subject() + " " + verb(
			"allows",
			"allow",
			would,
		) + " " + name + " == " + known.lo.String()
		return note(f.span(), msg), true
	}
	if f, ok := setting(facts, func(f fact) bool {
		return f.hasHi || !f.lenOf.isZero() || f.relational()
	}); ok {
		msg := f.guard.subject() + " " + verb("bounds", "bound", would) + " " + name +
			" only above; " + name + " may be negative"
		return note(f.span(), msg), true
	}
	return diag.Diagnostic{}, false
}

// upperNote explains why the facts about the index i, which with its
// type's bounds give known, do not keep it below the length of e: the fact
// that set the upper bound allows a value past the end, or bounds i by the
// length of something else. A value a != rules out is not one a note
// names (see rulesOut).
func (c *Checker) upperNote(
	i ast.Expr,
	facts []fact,
	known interval,
	e extent,
	would bool,
) (diag.Diagnostic, bool) {
	name := c.nameOf(i)
	if !e.known {
		return c.otherLenNote(name, facts, e, would)
	}
	last := e.n - 1
	if e.atEnd {
		last = e.n
	}
	if known.hasHi && known.hi.Cmp(constant.FromInt64(last)) <= 0 {
		return diag.Diagnostic{}, false
	}
	f, ok := setting(facts, func(f fact) bool { return f.hasHi && f.hi.Cmp(known.hi) == 0 })
	if !ok {
		return c.otherLenNote(name, facts, e, would)
	}
	if other, ok := c.lenOfOther(f.src, i, e.of); ok {
		return lengthNote(f, name, other, e, would), true
	}
	if c.rulesOut(i, known.hi) {
		return diag.Diagnostic{}, false
	}
	msg := f.guard.subject() + " " + verb(
		"allows",
		"allow",
		would,
	) + " " + name + " == " +
		known.hi.String() + ", and " + e.label + " has " + plural64(
		e.n,
		e.unit,
	)
	return note(f.span(), msg), true
}

// otherLenNote explains a bound on the index called name by the length of
// a string subject other than e's, when no fact bounds it by e's own.
func (c *Checker) otherLenNote(
	name string,
	facts []fact,
	e extent,
	would bool,
) (diag.Diagnostic, bool) {
	s, isSubject := c.subjectKey(e.of)
	if !isSubject {
		return diag.Diagnostic{}, false
	}
	if slices.ContainsFunc(facts, func(f fact) bool {
		return f.lenOf == s || e.atEnd && f.atMostLen == s
	}) {
		return diag.Diagnostic{}, false
	}
	f, ok := setting(facts, func(f fact) bool { return !f.lenOf.isZero() || !f.atMostLen.isZero() })
	if !ok {
		return diag.Diagnostic{}, false
	}
	other := f.lenOf
	if other.isZero() {
		other = f.atMostLen
	}
	return lengthNote(f, name, other.name(), e, would), true
}

// lengthNote returns the note that f bounds the index called name by the
// length of other, not of e.
func lengthNote(f fact, name, other string, e extent, would bool) diag.Diagnostic {
	msg := f.guard.subject() + " " + verb("bounds", "bound", would) + " " + name +
		" by the length of " + other + ", not " + e.label
	return note(f.span(), msg)
}

// lenOfOther returns the name of x when src is a comparison of the index
// i with len(x), x being a subject other than of.
func (c *Checker) lenOfOther(src ast.Expr, i, of ast.Expr) (string, bool) {
	cmp, ok := src.(*ast.Binary)
	if !ok || !cmp.Op.IsComparison() {
		return "", false
	}
	ik, _ := c.subjectKey(i)
	side := cmp.Y
	if k, isSubject := c.subjectKey(cmp.Y); isSubject && k == ik {
		side = cmp.X
	}
	x, ok := lenArg(side)
	if !ok {
		return "", false
	}
	xk, isSubject := c.subjectKey(x)
	if ofKey, ofIsSubject := c.subjectKey(of); !isSubject || ofIsSubject && ofKey == xk {
		return "", false
	}
	return c.nameOf(x), true
}

// lenArg returns x when e is len(x).
func lenArg(e ast.Expr) (ast.Expr, bool) {
	call, ok := e.(*ast.Call)
	if !ok || !isLen(call) || len(call.Args) != 1 {
		return nil, false
	}
	return call.Args[0], true
}

// noteRelated records in c.relatedSubjects each subject f, a fact being put in
// force, relates to another variable, a length aside, and each subject of
// every side f carries when it carries several: no claim about such a
// subject is literally true unless it follows the relation or the sides,
// which notes do not.
func (c *Checker) noteRelated(f fact) {
	mark := func(k factKey) {
		if k.isZero() {
			return
		}
		if c.relatedSubjects == nil {
			c.relatedSubjects = make(map[factKey]bool)
		}
		c.relatedSubjects[k] = true
	}
	switch {
	case f.either != nil && len(f.either.sides) > 1:
		for _, side := range f.either.sides {
			for _, s := range side {
				mark(s.about)
				c.noteRelated(s)
			}
		}
	case !f.atMost.isZero():
		mark(f.about)
		mark(f.atMost)
	case f.diff != nil && !f.diff.xLen && !f.diff.yLen && !f.diff.x.isZero() && !f.diff.y.isZero():
		mark(f.diff.x)
		mark(f.diff.y)
	}
}

// rulesOut reports whether a fact in force that holds rules the value v
// out for the subject e, e != v, or e != 0 for 0: a note that says e may
// be v would then be false. Notes describe no such fact, and a b != k
// that gives nothing else is a proof-only fact (see keepApart), so those
// are looked at too.
func (c *Checker) rulesOut(e ast.Expr, v constant.Int) bool {
	k, ok := c.subjectKey(e)
	if !ok {
		return false
	}
	out := func(f fact) bool {
		return f.about == k && (f.out && f.ne.Cmp(v) == 0 || f.nonzero && v.IsZero()) && c.holds(f)
	}
	for f := range c.factsAbout(k) {
		if out(f) {
			return true
		}
	}
	for i := range c.proven {
		r := &c.proven[i]
		if len(r.sides) != 1 {
			continue
		}
		c.indexProven(r)
		if slices.ContainsFunc(r.sides[0], out) {
			return true
		}
	}
	return false
}

// setting returns the innermost of facts for which match holds: the one
// that set a bound, when match picks out the facts with that bound.
func setting(facts []fact, match func(fact) bool) (fact, bool) {
	for i := len(facts) - 1; i >= 0; i-- {
		if match(facts[i]) {
			return facts[i], true
		}
	}
	return fact{}, false
}

// plural64 writes n of unit, with an s added unless n is one.
func plural64(n int64, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return constant.FromInt64(n).String() + " " + unit + "s"
}

// lengthProof returns the proof that the string subject s is long enough
// for a constant index, which proven checks. Its facts are about s's
// length.
func (c *Checker) lengthProof(s ast.Expr, proven func() bool) proof {
	about, _ := c.subjectKey(s)
	name := about.name()
	return proof{
		about:  about,
		proven: proven,
		claim:  func(facts []fact) string { return describe("len("+name+")", facts) },
		weak:   func([]fact, bool) []diag.Diagnostic { return nil },
	}
}

// text returns the source span covers, which notes quote.
func (c *Checker) text(span ast.Span) string {
	f := c.files.Lookup(span.File)
	if f == nil || span.Start < 0 || span.End > len(f.Data) || span.Start > span.End {
		panic("typeck: a span outside the source given to New")
	}
	return string(f.Data[span.Start:span.End])
}

// mutableGlobal returns the declaration of the variable e names when it is
// a mutable global, which never has a fact.
func (c *Checker) mutableGlobal(e ast.Expr) (*ast.Var, bool) {
	id, ok := e.(*ast.Ident)
	if !ok {
		return nil, false
	}
	v, ok := id.Def.(*ast.Var)
	return v, ok && v.Mutable && c.globals[v]
}

// globalNote returns the note, at its declaration, that the mutable global
// v can never be proven anything about.
func globalNote(v *ast.Var) []diag.Diagnostic {
	return []diag.Diagnostic{note(v.NameSpan,
		v.Name+" is a mutable global, so no check proves it; copy it into a local first")}
}

// operandNotes explains why operand, a divisor, an index, a slice's bound
// or a converted value that is not a subject, is not proven, lenAlone
// saying whether its use proves the length of a string, view or list
// subject on its own, as a divisor's and a slice bound's does: a field of
// a mutable variable a guard in scope compares, which never carries a
// fact, or an expression using a variable a guard bounds. Arithmetic the
// checker reads through (see arithmeticShape) gets a note that its
// operands' bounds fall short or let a step overflow; anything else gets one
// that lists what is proven for its use (see provenList). A length gets
// no note, alone or in arithmetic (see nameNote), and a stored value's
// fact (see guardValue) is never the one a note names.
func (c *Checker) operandNotes(operand ast.Expr, lenAlone bool) []diag.Diagnostic {
	if _, ok := operand.(*ast.Selector); ok {
		return c.fieldNotes(operand)
	}
	if _, known := c.knownValue(operand); known {
		return nil
	}
	readable := c.arithmeticShape(operand)
	var notes []diag.Diagnostic
	var seen []ast.Decl
	ast.Walk(operand, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || slices.Contains(seen, id.Def) {
			return true
		}
		seen = append(seen, id.Def)
		f, bounded := setting(
			c.active,
			func(f fact) bool {
				return f.about == declKey(id.Def) && c.holds(f) && !f.relational() &&
					!f.guard.silent()
			},
		)
		if bounded {
			notes = append(notes, c.nameNote(f, id, operand, readable, lenAlone)...)
		}
		return true
	})
	return notes
}

// nameNote returns the note at f's guard, which bounds id, a name in
// operand, saying that it bounds id and not operand: operand's bounds
// fall short or let a step wrap, when readable says operand is arithmetic
// the checker reads through, or else only what provenList lists for its
// use is proven. A fact about a string, a view or a list is about its
// length, which gets no note.
func (c *Checker) nameNote(
	f fact,
	id *ast.Ident,
	operand ast.Expr,
	readable, lenAlone bool,
) []diag.Diagnostic {
	if _, isLen := c.stringVar(id); isLen {
		return nil
	}
	expr := c.text(operand.Spanned())
	why := "; only " + provenList(lenAlone) + ", is proven, so copy "
	if readable {
		why = "; the bounds of its operands do not prove " + expr +
			", or let a step of it overflow, so copy "
	}
	return []diag.Diagnostic{note(f.guard.span, f.guard.subject()+" bounds "+id.Qualified()+
		", not "+expr+why+expr+" into a variable and check it")}
}

// provenList returns what a note lists as proven for an operand: a known
// value, a name, a path below an immutable one, and arithmetic on those
// or on the length of a string, view or list one, and, when lenAlone says
// the use proves such a length on its own, as a divisor and a slice's
// bound do, that length alone too.
func provenList(lenAlone bool) string {
	if lenAlone {
		return "a known value, a name, a field or element of an immutable variable, " +
			"len of a string, view or list one, or +, -, * or unary - on those"
	}
	return "a known value, a name, a field or element of an immutable variable, " +
		"or +, -, * or unary - on those or on len of a string, view or list one"
}

// fieldNotes returns the note for the field path, such as p.d, of a
// mutable variable or a mut parameter, that the innermost guard in scope
// comparing it, or its length, proves nothing about it.
func (c *Checker) fieldNotes(path ast.Expr) []diag.Diagnostic {
	for i := len(c.guards) - 1; i >= 0; i-- {
		g := c.guards[i]
		if g.cond == nil || !compares(g.cond, path) {
			continue
		}
		return []diag.Diagnostic{note(g.cond.Spanned(), g.subject()+
			" proves nothing: a field of a mutable variable or a mut parameter never carries a fact; copy "+
			c.text(path.Spanned())+
			" into a variable first")}
	}
	return nil
}

// compares reports whether cond, through && || and !, holds a comparison
// one side of which is path, or len of path.
func compares(cond, path ast.Expr) bool {
	switch x := cond.(type) {
	case *ast.Unary:
		return x.Op == ast.Not && compares(x.X, path)
	case *ast.Binary:
		if x.Op.IsLogical() {
			return compares(x.X, path) || compares(x.Y, path)
		}
		return x.Op.IsComparison() && (pathOrLen(x.X, path) || pathOrLen(x.Y, path))
	default:
		return false
	}
}

// pathOrLen reports whether e is path, or a call to len of path.
func pathOrLen(e, path ast.Expr) bool {
	if call, ok := e.(*ast.Call); ok && isLen(call) && len(call.Args) == 1 {
		return samePath(call.Args[0], path)
	}
	return samePath(e, path)
}

// samePath reports whether a and b are the same variable, or the same field
// of the same path, p.d and p.d.
func samePath(a, b ast.Expr) bool {
	switch x := a.(type) {
	case *ast.Ident:
		y, ok := b.(*ast.Ident)
		return ok && x.Def == y.Def
	case *ast.Selector:
		y, ok := b.(*ast.Selector)
		return ok && x.Name == y.Name && samePath(x.X, y.X)
	default:
		return false
	}
}

// outerIndexNote returns the note that only the outer index of the element
// being bound with ok is checked, when ix is an index inside it, which
// still needs a proof of its own.
func (c *Checker) outerIndexNote(ix *ast.Index) []diag.Diagnostic {
	outer := c.checkedIndex
	if outer == nil || outer == ix {
		return nil
	}
	inside := false
	ast.Walk(outer, func(n ast.Node) bool {
		inside = inside || n == ast.Node(ix)
		return !inside
	})
	if !inside {
		return nil
	}
	return []diag.Diagnostic{note(outer.Span, "only the outer index of "+c.text(outer.Span)+
		" is checked; "+c.text(ix.Span)+" needs its own proof")}
}

// isLenCall reports whether e is a call to the builtin len.
func isLenCall(e ast.Expr) bool {
	call, ok := e.(*ast.Call)
	return ok && isLen(call)
}

// isExit reports whether call calls the builtin exit, which has a nil
// declaration like every builtin.
func isExit(call *ast.Call) bool {
	id, ok := call.Fn.(*ast.Ident)
	if !ok || id.Name != "exit" {
		return false
	}
	fn, ok := id.Def.(*ast.Func)
	return ok && fn == nil
}
