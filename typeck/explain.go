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

// drop is why a fact does not hold where it is put in force, which a note
// points at, at: the region assigns name there, or passes it as mut to the
// function text there, the branch that would have left ends in exit there,
// or the loop's step there could wrap, which text then explains.
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
	// dropExit is an if or a switch whose branch ends in a call to exit,
	// which does not count as leaving the block, so the facts of getting
	// past it would hold after it only if the branch left.
	dropExit
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
	case dropExit:
		return note(d.at, "but its branch ends in exit, not in return, break or continue")
	case dropWraps:
		return note(d.at, d.text)
	case dropMut:
		return note(d.at, "but "+d.name+" is passed as mut to "+d.text+" here, so the proof ends")
	default:
		return note(d.at, "but "+d.name+" is assigned here, so the proof ends")
	}
}

// proves returns how a note says what a guard's facts, dropped by d, prove:
// they do, until the assignment, or they would, had the branch left.
func (d *drop) proves() string {
	if d.kind == dropExit {
		return " would prove "
	}
	return " proves "
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
// its variable that was dropped, the guard that gave it and what dropped
// it, and then, if those facts would not have been enough, or none was
// dropped, why the facts fall short. A guard's facts that one reason
// dropped are explained only when one of them is relevant to p.
func (c *Checker) explain(p proof) []diag.Diagnostic {
	var facts, dropped []fact
	for _, f := range c.active {
		if f.about != p.about || f.relational() && !p.relations {
			continue
		}
		f.drop = c.dropOf(f)
		facts = append(facts, f)
		if f.drop != nil {
			dropped = append(dropped, f)
		}
	}
	dropped = relevantDrops(dropped, p.relevant)
	if len(dropped) == 0 {
		return p.weak(facts, false)
	}
	notes := dropNotes(dropped, p.claim)
	if !c.supposing(p.proven) {
		notes = append(notes, p.weak(facts, true)...)
	}
	return notes
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

// supposing runs proven with every fact in force counted, dropped ones
// included, and puts the facts back as they were before returning what it
// reported.
func (c *Checker) supposing(proven func() bool) bool {
	saved := c.active
	c.active = slices.Clone(saved)
	for i := range c.active {
		c.active[i].drop, c.active[i].ends = nil, nil
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
			notes = append(notes, note(g.span, g.subject()+f.drop.proves()+claim(own)+g.where()))
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
			if len(facts) == 0 {
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
// for e, which proven checks. An unsigned i is never below 0, so only an
// upper bound, by a value or by a length, can be missing, and a fact that
// gives none, such as the lower bound a counting loop counts from, is not
// relevant to it.
func (c *Checker) indexProof(i ast.Expr, e extent, proven func() bool) proof {
	about, _ := c.subjectKey(i)
	name := c.nameOf(i)
	var relevant func(f fact) bool
	if types.IsUnsigned(ast.TypeOf(i)) {
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
			if n, ok := lowerNote(name, facts, known, would); ok {
				notes = append(notes, n)
			}
			if n, ok := c.upperNote(i, facts, known, e, would); ok {
				notes = append(notes, n)
			}
			return notes
		},
	}
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

// lowerNote explains why the facts about the index called name, which with
// its type's bounds give known, do not keep it at or above 0: the fact
// that set the lower bound allows a negative value, or no fact gives a
// lower bound while one gives an upper.
func lowerNote(name string, facts []fact, known interval, would bool) (diag.Diagnostic, bool) {
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
// length of something else.
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

// operandNotes explains why operand, a divisor or an index that is not a
// subject, is not proven: a field of a mutable variable a guard in scope
// compares, which never carries a fact, or an expression using a variable
// a guard bounds, when only a plain name or a path below an immutable one
// is proven.
func (c *Checker) operandNotes(operand ast.Expr) []diag.Diagnostic {
	if _, ok := operand.(*ast.Selector); ok {
		return c.fieldNotes(operand)
	}
	if _, known := c.knownValue(operand); known {
		return nil
	}
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
				return f.about == declKey(id.Def) && c.holds(f) && !f.relational()
			},
		)
		if bounded {
			expr := c.text(operand.Spanned())
			notes = append(
				notes,
				note(f.guard.span, f.guard.subject()+" bounds "+id.Qualified()+", not "+expr+
					"; only a name, or a field or element of an immutable variable, is proven, so copy "+expr+" into a variable and check it"),
			)
		}
		return true
	})
	return notes
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

// exitAfterFacts returns the facts an if or a switch would give the
// statements after it if a branch ending in a call to exit left the block,
// with the guard they come from, each marked as dropped by that call. It
// returns none when the statement gives the same facts either way.
func (c *Checker) exitAfterFacts(stmt ast.Stmt) (*guard, []fact) {
	call := c.exitCall(stmt)
	if call == nil {
		return nil, nil
	}
	var g *guard
	var facts []fact
	switch s := stmt.(type) {
	case *ast.If:
		facts = exitFacts(leavesBy(s.Then, true), leavesBy(s.Else, true), func(holds bool) []fact {
			return c.factsFrom(s.Cond, holds)
		})
		g = ifGuard(s)
	case *ast.Switch:
		g, facts = c.switchAfterFactsBy(s, true)
	}
	why := &drop{kind: dropExit, at: call.Span}
	for i := range facts {
		facts[i].drop = why
	}
	return g, facts
}

// exitCall returns the call to exit that ends a branch of stmt, an if or a
// switch, at any depth, or nil when there is none. Each statement's is
// found once and kept: every block of a chain of else ifs asks it of the
// if that block holds, whose answer takes in the rest of the chain, which
// would make a long chain cost the square of its length.
func (c *Checker) exitCall(stmt ast.Stmt) *ast.Call {
	if call, ok := c.exitCalls[stmt]; ok {
		return call
	}
	var call *ast.Call
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		if x, ok := s.X.(*ast.Call); ok && isExit(x) {
			call = x
		}
	case *ast.If:
		if call = c.lastExit(s.Then); call == nil {
			call = c.lastExit(s.Else)
		}
	case *ast.Switch:
		for _, cl := range s.Cases {
			if call = c.lastExit(cl.Body); call != nil {
				break
			}
		}
	}
	c.exitCalls[stmt] = call
	return call
}

// lastExit returns the call to exit that ends body, at any depth, or nil.
func (c *Checker) lastExit(body []ast.Stmt) *ast.Call {
	if len(body) == 0 {
		return nil
	}
	return c.exitCall(body[len(body)-1])
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
