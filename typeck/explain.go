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
// points at, at: the region assigns name there, the branch that would have
// left ends in exit there, or the loop's step there could wrap, which text
// then explains.
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

// text returns the source span covers, which notes quote.
func (c *Checker) text(span ast.Span) string {
	if span.Start < 0 || span.End > len(c.src) || span.Start > span.End {
		panic("typeck: a span outside the source given to New")
	}
	return string(c.src[span.Start:span.End])
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
// variable, is not proven: a field a guard in scope compares, which never
// carries a fact, or an expression using a variable a guard bounds, when
// only a plain name is proven.
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
			func(f fact) bool { return f.decl == id.Def && f.inForce() },
		)
		if bounded {
			expr := c.text(operand.Spanned())
			notes = append(
				notes,
				note(f.guard.span, f.guard.subject()+" bounds "+id.Name+", not "+expr+
					"; only a plain name is proven, so copy "+expr+" into a variable and check it"),
			)
		}
		return true
	})
	return notes
}

// fieldNotes returns the note for the field path, such as p.d, that the
// innermost guard in scope comparing it proves nothing about it.
func (c *Checker) fieldNotes(path ast.Expr) []diag.Diagnostic {
	for i := len(c.guards) - 1; i >= 0; i-- {
		g := c.guards[i]
		if g.cond == nil || !compares(g.cond, path) {
			continue
		}
		return []diag.Diagnostic{note(g.cond.Spanned(), g.subject()+
			" proves nothing: a field never carries a fact; copy "+c.text(path.Spanned())+
			" into a variable first")}
	}
	return nil
}

// compares reports whether cond, through && || and !, holds a comparison
// one side of which is path.
func compares(cond, path ast.Expr) bool {
	switch x := cond.(type) {
	case *ast.Unary:
		return x.Op == ast.Not && compares(x.X, path)
	case *ast.Binary:
		if x.Op.IsLogical() {
			return compares(x.X, path) || compares(x.Y, path)
		}
		return x.Op.IsComparison() && (samePath(x.X, path) || samePath(x.Y, path))
	default:
		return false
	}
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

// lenNote returns the note on cmp, a comparison of operands of types x and
// y that have no common type, when one side is len, an int64, and the
// other's type is unsigned: it says how to convert that side.
func (c *Checker) lenNote(cmp *ast.Binary, x, y types.Type) []diag.Diagnostic {
	operand := cmp.X
	switch {
	case isLenCall(cmp.Y) && types.IsUnsigned(x):
	case isLenCall(cmp.X) && types.IsUnsigned(y):
		operand = cmp.Y
	default:
		return nil
	}
	text := c.text(operand.Spanned())
	return []diag.Diagnostic{note(operand.Spanned(),
		"len gives an int64; convert "+text+" first with i, ok := int64("+text+")")}
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
	call := exitCall(stmt)
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
// switch, at any depth, or nil when there is none.
func exitCall(stmt ast.Stmt) *ast.Call {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		if call, ok := s.X.(*ast.Call); ok && isExit(call) {
			return call
		}
	case *ast.If:
		if call := lastExit(s.Then); call != nil {
			return call
		}
		return lastExit(s.Else)
	case *ast.Switch:
		for _, cl := range s.Cases {
			if call := lastExit(cl.Body); call != nil {
				return call
			}
		}
	}
	return nil
}

// lastExit returns the call to exit that ends body, at any depth, or nil.
func lastExit(body []ast.Stmt) *ast.Call {
	if len(body) == 0 {
		return nil
	}
	return exitCall(body[len(body)-1])
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
