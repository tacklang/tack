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
)

// subject returns how a note names g.
func (g *guard) subject() string {
	switch g.kind {
	case guardRange:
		return "this loop"
	case guardCount:
		return "counting from this start"
	case guardCase, guardLaterCase:
		return "this case"
	case guardExit:
		return "this early exit"
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
	case guardExit:
		return " after it"
	default:
		return ""
	}
}

// drop is why a fact does not hold where it is put in force: the region
// assigns name at the span at.
type drop struct {
	name string
	at   ast.Span
	kind dropKind
}

// dropKind says why a fact was dropped.
type dropKind int

const (
	// dropAssigned is a region assigning the fact's variable, or the
	// string whose length bounds it.
	dropAssigned dropKind = iota
)

// same reports whether d and other are the same reason, the same
// assignment.
func (d *drop) same(other *drop) bool {
	return d.kind == other.kind && d.at == other.at
}

// note returns the note that points at d's cause.
func (d *drop) note() diag.Diagnostic {
	return note(d.at, "but "+d.name+" is assigned here, so the proof ends")
}

// note returns a note with message msg covering span.
func note(span ast.Span, msg string) diag.Diagnostic {
	return diag.Diagnostic{Severity: diag.Note, Message: msg, Span: span}
}

// proof is a missing proof to explain: about, the variable the facts that
// could prove it are about; proven, which runs the proof again; claim,
// which says what some facts about the variable prove; and weak, which
// explains why facts that do not prove it fall short, phrased as what they
// would do when would is set.
type proof struct {
	about  ast.Decl
	proven func() bool
	claim  func(facts []fact) string
	weak   func(facts []fact, would bool) []diag.Diagnostic
}

// explain returns the notes for the missing proof p: for each fact about
// its variable that was dropped, the guard that gave it and what dropped
// it, and then, if those facts would not have been enough, or none was
// dropped, why the facts fall short.
func (c *Checker) explain(p proof) []diag.Diagnostic {
	var facts, dropped []fact
	for _, f := range c.active {
		if f.decl != p.about {
			continue
		}
		facts = append(facts, f)
		if !f.inForce() {
			dropped = append(dropped, f)
		}
	}
	if len(dropped) == 0 {
		return p.weak(facts, false)
	}
	notes := dropNotes(dropped, p.claim)
	if !c.supposing(p.proven) {
		notes = append(notes, p.weak(facts, true)...)
	}
	return notes
}

// supposing runs proven with every fact in force counted, dropped ones
// included, and puts the facts back as they were before returning what it
// reported.
func (c *Checker) supposing(proven func() bool) bool {
	saved := c.active
	c.active = slices.Clone(saved)
	for i := range c.active {
		c.active[i].drop = nil
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

// describe says what facts, all about the variable called name, prove
// together: its bounds, a bound by a string's length, and that it is not
// zero, when its bounds do not already say so. An upper bound is written
// with <, as checks usually are.
func describe(name string, facts []fact) string {
	var known interval
	var lenOf ast.Decl
	for _, f := range facts {
		known = known.narrow(f)
		if f.lenOf != nil {
			lenOf = f.lenOf
		}
	}
	parts := bounds(name, known, lenOf != nil)
	if lenOf != nil {
		parts = append(parts, name+" < len("+declName(lenOf)+")")
	}
	bounded := known
	bounded.nonzero = false
	if known.nonzero && !bounded.excludesZero() {
		parts = append(parts, name+" != 0")
	}
	return strings.Join(parts, " && ")
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

// divisorProof returns the proof that the divisor id, a variable, is not
// zero.
func (c *Checker) divisorProof(id *ast.Ident) proof {
	return proof{
		about:  id.Def,
		proven: func() bool { return c.nonzero(id) },
		claim: func(facts []fact) string {
			var known interval
			for _, f := range facts {
				known = known.narrow(f)
			}
			if known.excludesZero() {
				return id.Name + " != 0"
			}
			return describe(id.Name, facts)
		},
		weak: func(facts []fact, would bool) []diag.Diagnostic {
			if len(facts) == 0 {
				return nil
			}
			f := facts[len(facts)-1]
			msg := f.guard.subject() + " " + verb(
				"allows",
				"allow",
				would,
			) + " " + id.Name + " == 0"
			return []diag.Diagnostic{note(f.span(), msg)}
		},
	}
}

// extent is what an index must stay below: the length n of an array or a
// string, when known, and the array or string itself, of, whose items are
// called unit.
type extent struct {
	of    ast.Expr
	unit  string
	n     int64
	known bool
}

// name returns how a note names the array or string e is the extent of.
func (e extent) name() string {
	if id, ok := e.of.(*ast.Ident); ok {
		return id.Name
	}
	if e.unit == "byte" {
		return "the string"
	}
	return "the array"
}

// indexProof returns the proof that the index id, a variable, is in range
// for e, which proven checks.
func (c *Checker) indexProof(id *ast.Ident, e extent, proven func() bool) proof {
	return proof{
		about:  id.Def,
		proven: proven,
		claim:  func(facts []fact) string { return describe(id.Name, facts) },
		weak: func(facts []fact, would bool) []diag.Diagnostic {
			known := typeInterval(id)
			for _, f := range facts {
				known = known.narrow(f)
			}
			var notes []diag.Diagnostic
			if n, ok := lowerNote(id, facts, known, would); ok {
				notes = append(notes, n)
			}
			if n, ok := upperNote(id, facts, known, e, would); ok {
				notes = append(notes, n)
			}
			return notes
		},
	}
}

// typeInterval returns the bounds the type of the variable id names gives
// it, none for a type without them.
func typeInterval(id *ast.Ident) interval {
	var known interval
	if typ := ast.TypeOf(id); types.Bits(typ) > 0 {
		known.lo, known.hi = typeBounds(typ)
		known.hasLo, known.hasHi = true, true
	}
	return known
}

// lowerNote explains why the facts about the index id, which with its
// type's bounds give known, do not keep it at or above 0: the fact that set
// the lower bound allows a negative value, or no fact gives a lower bound
// while one gives an upper.
func lowerNote(id *ast.Ident, facts []fact, known interval, would bool) (diag.Diagnostic, bool) {
	if known.hasLo && !known.lo.IsNegative() {
		return diag.Diagnostic{}, false
	}
	if f, ok := setting(
		facts,
		func(f fact) bool { return f.hasLo && f.lo.Cmp(known.lo) == 0 },
	); ok {
		msg := f.guard.subject() + " " + verb(
			"allows",
			"allow",
			would,
		) + " " + id.Name + " == " + known.lo.String()
		return note(f.span(), msg), true
	}
	if f, ok := setting(facts, func(f fact) bool { return f.hasHi || f.lenOf != nil }); ok {
		msg := f.guard.subject() + " " + verb("bounds", "bound", would) + " " + id.Name +
			" only above; " + id.Name + " may be negative"
		return note(f.span(), msg), true
	}
	return diag.Diagnostic{}, false
}

// upperNote explains why the facts about the index id, which with its
// type's bounds give known, do not keep it below the length of e: the fact
// that set the upper bound allows a value past the end, or bounds id by the
// length of something else.
func upperNote(
	id *ast.Ident,
	facts []fact,
	known interval,
	e extent,
	would bool,
) (diag.Diagnostic, bool) {
	if !e.known {
		return otherLenNote(id, facts, e, would)
	}
	if known.hasHi && known.hi.Cmp(constant.FromInt64(e.n-1)) <= 0 {
		return diag.Diagnostic{}, false
	}
	f, ok := setting(facts, func(f fact) bool { return f.hasHi && f.hi.Cmp(known.hi) == 0 })
	if !ok {
		return otherLenNote(id, facts, e, would)
	}
	if other, ok := lenOfOther(f.src, id, e.of); ok {
		return lengthNote(f, id, other, e, would), true
	}
	msg := f.guard.subject() + " " + verb("allows", "allow", would) + " " + id.Name + " == " +
		known.hi.String() + ", and " + e.name() + " has " + plural64(e.n, e.unit)
	return note(f.span(), msg), true
}

// otherLenNote explains a bound on the index id by the length of a string
// variable other than e's, when no fact bounds it by e's own.
func otherLenNote(id *ast.Ident, facts []fact, e extent, would bool) (diag.Diagnostic, bool) {
	s, isName := e.of.(*ast.Ident)
	if !isName {
		return diag.Diagnostic{}, false
	}
	if slices.ContainsFunc(facts, func(f fact) bool { return f.lenOf == s.Def }) {
		return diag.Diagnostic{}, false
	}
	f, ok := setting(facts, func(f fact) bool { return f.lenOf != nil })
	if !ok {
		return diag.Diagnostic{}, false
	}
	return lengthNote(f, id, declName(f.lenOf), e, would), true
}

// lengthNote returns the note that f bounds the index id by the length of
// other, not of e.
func lengthNote(f fact, id *ast.Ident, other string, e extent, would bool) diag.Diagnostic {
	msg := f.guard.subject() + " " + verb("bounds", "bound", would) + " " + id.Name +
		" by the length of " + other + ", not " + e.name()
	return note(f.span(), msg)
}

// lenOfOther returns the name x when src is a comparison of the variable
// id with len(x), x being a variable other than of.
func lenOfOther(src ast.Expr, id *ast.Ident, of ast.Expr) (string, bool) {
	cmp, ok := src.(*ast.Binary)
	if !ok || !cmp.Op.IsComparison() {
		return "", false
	}
	side := cmp.Y
	if name, isName := cmp.Y.(*ast.Ident); isName && name.Def == id.Def {
		side = cmp.X
	}
	x, ok := lenArg(side)
	if !ok {
		return "", false
	}
	if indexed, isName := of.(*ast.Ident); isName && indexed.Def == x.Def {
		return "", false
	}
	return x.Name, true
}

// lenArg returns x when e is len(x), x a name.
func lenArg(e ast.Expr) (*ast.Ident, bool) {
	call, ok := e.(*ast.Call)
	if !ok || !isLen(call) || len(call.Args) != 1 {
		return nil, false
	}
	x, ok := call.Args[0].(*ast.Ident)
	return x, ok
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

// lengthProof returns the proof that the string variable s is long enough
// for a constant index, which proven checks. Its facts are about s's
// length.
func lengthProof(s *ast.Ident, proven func() bool) proof {
	return proof{
		about:  s.Def,
		proven: proven,
		claim:  func(facts []fact) string { return describe("len("+s.Name+")", facts) },
		weak:   func([]fact, bool) []diag.Diagnostic { return nil },
	}
}
