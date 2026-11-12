package typeck

import (
	"math"
	"slices"
	"strconv"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// An unreachable statement marks a place the checker must prove no run
// reaches; one it cannot prove is an error. A place is proven when:
//
//   - a statement before it in a block around it leaves that block or
//     terminates, a return, a break, a continue, a call to exit, an
//     unreachable, or an if or a switch that terminates (see c.dead);
//   - the facts there about some subject leave it no value: its type's
//     bounds and its facts' bounds cross, or every value between them is
//     ruled out, by != or by an == that was false, as the cases of a
//     switch or the early exits of an if chain rule out the values they
//     name (see subjectState.empty);
//   - a true || or a false && on the path to it has every side's facts,
//     each side taken alone with the other facts there, leaving some
//     subject no value, since one of its sides holds;
//   - the relations in force there hold a cycle of negative weight, as
//     a < b && b < a gives, which no values can satisfy (see
//     relationsContradict).
//
// Besides the facts in force, among them what the early exits of an
// else-if chain or the leaving cases of a switch give the code after it
// (see chainFacts), the proof counts facts only it asks about, each kept
// apart (see proofOnly): a value ruled out, which a long chain of cases or
// ifs gives one of for each value it names; and the sides of a true || or
// a false &&, beside the hull of them the facts in force hold. Each fact,
// of either kind, counts only where it holds, as for any other proof (see
// holds), so a write that can reach the place ends what it proved.

// proofOnly is facts kept apart from the facts in force, which only the
// proof of an unreachable place counts: with one side, facts that hold
// together, and with several, sides one of which holds, each side's facts
// together. region is where they hold, from their guard on, as assume
// takes it; the writes in it that could end each are found only when a
// proof first asks about them (see indexProven).
type proofOnly struct {
	sides  [][]fact
	region region
	// indexed is whether every fact's ends have been found.
	indexed bool
}

// eitherFacts is the sides of a true || or a false &&, of which one
// holds, carried beside the hull of them that union gives, by a fact that
// holds nothing else (see binaryFacts). assume keeps them apart from the
// facts in force (see proofOnly).
type eitherFacts struct {
	sides [][]fact
}

// eitherFact returns the fact carrying sides, the facts of each side of a
// true || or a false &&, leaving out any fact carrying sides of its own,
// which only says less.
func eitherFact(sides ...[]fact) fact {
	kept := make([][]fact, len(sides))
	for i, side := range sides {
		for _, f := range side {
			if f.either == nil {
				kept[i] = append(kept[i], f)
			}
		}
	}
	return fact{either: &eitherFacts{sides: kept}}
}

// dropSides returns a copy of e with each side's facts that region
// assigns the variable of marked dropped, as dropAssigned does.
func (c *Checker) dropSides(e *eitherFacts, region []ast.Node) *eitherFacts {
	out := &eitherFacts{sides: make([][]fact, len(e.sides))}
	for i, side := range e.sides {
		out.sides[i] = c.dropAssigned(slices.Clone(side), region)
	}
	return out
}

// keepApart records facts, the content of f, an empty fact put in force
// from g for region, as proof-only facts, when it has any: the sides it
// carries, or the value it rules out. It reports whether f had any.
func (c *Checker) keepApart(g *guard, f fact, r region) bool {
	switch {
	case f.drop != nil:
		return f.either != nil || f.out
	case f.either != nil:
		sides := make([][]fact, len(f.either.sides))
		for i, side := range f.either.sides {
			sides[i] = guarded(g, side)
		}
		c.proven = append(c.proven, proofOnly{sides: sides, region: r})
		return true
	case f.out:
		f.guard = g
		c.proven = append(c.proven, proofOnly{sides: [][]fact{{f}}, region: r})
		return true
	default:
		return false
	}
}

// chainFacts puts in force, for rest, the statements after stmt in its
// block, shared with the other regions over them (see region.go), what
// an if chain or a switch that stmt is gives the code after it past its
// first link, whose facts afterFacts gives. For an if whose then branch
// leaves and whose else is one if, the code after the outer if is
// reached only past that inner if,
// so whatever the inner if gives the code after it as an early exit (see
// afterFacts) holds there too, in a region from the inner if's branch that
// falls through on, and so on down the chain. For a switch see
// switchChainFacts.
//
// With no statement after stmt there is nowhere the facts hold, and the
// chain is not walked. So a link of a chain, alone in the else block of
// the link before it, never walks the rest of the chain, which in a chain
// of n links would take time n squared. Whether each link leaves is
// remembered (see leavememo.go), so the walk looks at each link once.
func (c *Checker) chainFacts(stmt ast.Stmt, rest blockRest) {
	if rest.empty() {
		return
	}
	if sw, ok := stmt.(*ast.Switch); ok {
		c.switchChainFacts(sw, rest)
		return
	}
	s, ok := stmt.(*ast.If)
	for ok && c.leaves(s.Then) && len(s.Else) == 1 {
		s, ok = s.Else[0].(*ast.If)
		if !ok {
			return
		}
		g, facts := c.afterFacts(s)
		if g == nil {
			continue
		}
		c.assumeIn(g, facts, c.regionOf(c.fallsThrough(s), rest))
	}
}

// switchChainFacts puts in force, for rest, the statements after s,
// shared with the other regions over them (see region.go), s an
// expression switch, tagged or with no tag, what the cases after its
// first, in the order tried, give them, as the
// else-if chain s means does; the first case's facts come from
// switchAfterFacts. While every case tried so far leaves, the code after s
// is reached only when none of them matched, so the next case gives the
// facts of its values not matching when its body leaves and the cases
// after it do not all leave (see chainLeaves), and those of its values
// matching when its body does not leave and the cases after it all do,
// which ends the walk. A case ending in a fallthrough leaves when the case
// it enters does (see leaves). The facts of a case not matching hold in a
// region of every value tried after it and every later body that does not
// leave, then rest; those of a case matching, in its body, then rest.
func (c *Checker) switchChainFacts(s *ast.Switch, rest blockRest) {
	if s.IsTypeSwitch() || (s.Tag != nil && c.table.IsUnion(ast.TypeOf(s.Tag))) {
		return
	}
	order := tryOrder(s)
	if len(order) < 2 {
		return
	}
	// bodyLeaves[i] is whether order[i]'s body leaves, and restLeaves[i]
	// whether the chain order[i:] does, which only one ending in a
	// default can.
	bodyLeaves := make([]bool, len(order))
	restLeaves := make([]bool, len(order)+1)
	all := order[len(order)-1].Default
	for i := len(order) - 1; i >= 0; i-- {
		bodyLeaves[i] = c.leaves(order[i].Body)
		all = all && bodyLeaves[i]
		restLeaves[i] = all
	}
	// The regions' heads are suffixes of one slice, each followed by
	// rest: from[i] is where the values of order[i] start in it, and
	// from[len(order)] its end.
	var nodes []ast.Node
	from := make([]int, len(order)+1)
	for i := 1; i < len(order); i++ {
		from[i] = len(nodes)
		nodes = append(nodes, valueNodes(order[i].Values)...)
		if !bodyLeaves[i] {
			nodes = append(nodes, stmtNodes(order[i].Body)...)
		}
	}
	from[len(order)] = len(nodes)
	c.work.copied += len(nodes)
	for i := 1; i < len(order) && bodyLeaves[i-1] && !restLeaves[i]; i++ {
		cl := order[i]
		switch {
		case bodyLeaves[i] && !restLeaves[i+1]:
			if facts := c.caseFacts(s, cl, false); len(facts) > 0 {
				g := &guard{span: caseSpan(cl), kind: guardExit}
				c.assumeIn(g, facts, region{head: nodes[from[i+1]:], rest: rest})
			}
		case !bodyLeaves[i] && restLeaves[i+1]:
			if facts := c.caseFacts(s, cl, true); len(facts) > 0 {
				g := &guard{span: caseSpan(cl), kind: guardMatched}
				c.assumeIn(g, facts, c.regionOf(stmtNodes(cl.Body), rest))
			}
		}
	}
}

// guarded returns a copy of facts with each one's guard set to g.
func guarded(g *guard, facts []fact) []fact {
	out := slices.Clone(facts)
	for i := range out {
		out[i].guard = g
	}
	return out
}

// indexProven finds, the first time it is asked, the writes in r's region that
// could end each of its facts.
func (c *Checker) indexProven(r *proofOnly) {
	if r.indexed {
		return
	}
	r.indexed = true
	idx := c.indexRegion(r.region)
	for _, side := range r.sides {
		for i := range side {
			if side[i].drop == nil {
				side[i].ends = idx.endings(side[i])
			}
		}
	}
}

// checkUnreachable checks u, which must be proven unreachable, recording
// the file and line its abort reports (see ast.Unreachable).
func (c *Checker) checkUnreachable(u *ast.Unreachable) {
	u.Where = c.position(u.Span)
	// A default that full coverage of a union forbids is already
	// reported, and the program rejected, so its unreachable is not.
	if c.inCoveredDefault > 0 || c.unreachableProven() {
		return
	}
	c.errorNotes(u.Span, c.unreachableNotes(),
		"unreachable may be reached; check a condition that rules this place out first, or handle the case here")
}

// position returns where span starts as file:line, the file named as
// diagnostics name it.
func (c *Checker) position(span ast.Span) string {
	f := c.files.Lookup(span.File)
	if f == nil {
		panic("typeck: a span outside the source given to New")
	}
	line, _ := f.Position(span.Start)
	return f.Filename + ":" + strconv.Itoa(line)
}

// unreachableProven reports whether the place checking has reached, c.at,
// is proven unreachable: a block around it has already left, or the facts
// there contradict. Every proof-only fact is first supposed to hold, a
// quick test of whether they could be enough before the writes that
// could end them are looked for.
func (c *Checker) unreachableProven() bool {
	if c.dead > 0 || c.relationsContradict() {
		return true
	}
	return c.factsContradict(false) && c.factsContradict(true)
}

// factsContradict reports whether the facts there leave some subject no
// value, those in force and the proof-only ones with one side together,
// or do so with each side of some proof-only facts with several taken
// in turn. A proof-only fact counts only where it holds when check is
// set, and wherever it was not dropped otherwise.
func (c *Checker) factsContradict(check bool) bool {
	// side returns the facts of side s of r that count, found once r's
	// writes are indexed, since each fact's ends are then on r itself.
	side := func(r *proofOnly, s int, into []fact) []fact {
		if check {
			c.indexProven(r)
		}
		for _, f := range r.sides[s] {
			if check && c.holds(f) || !check && f.drop == nil {
				into = append(into, f)
			}
		}
		return into
	}
	var base []fact
	for _, f := range c.inForce() {
		if c.holds(f) {
			base = append(base, f)
		}
	}
	for i := range c.proven {
		r := &c.proven[i]
		if len(r.sides) != 1 {
			continue
		}
		base = side(r, 0, base)
	}
	if c.leavesNoValue(base) {
		return true
	}
	for i := range c.proven {
		r := &c.proven[i]
		if len(r.sides) < 2 {
			continue
		}
		every := true
		for s := 0; s < len(r.sides) && every; s++ {
			every = c.leavesNoValue(side(r, s, slices.Clip(base)))
		}
		if every {
			return true
		}
	}
	return false
}

// subjectState is what facts say about one subject, for leavesNoValue:
// the interval they and its type allow, and the values they rule out.
type subjectState struct {
	known interval
	outs  []constant.Int
}

// leavesNoValue reports whether facts, every one of which holds, leave
// some subject no value (see subjectState.empty). A relation takes no
// part.
func (c *Checker) leavesNoValue(facts []fact) bool {
	states := make(map[factKey]*subjectState)
	var order []factKey
	for _, f := range facts {
		if f.relational() || f.about.isZero() {
			continue
		}
		s, ok := states[f.about]
		if !ok {
			s = &subjectState{known: c.keyInterval(f.about)}
			states[f.about] = s
			order = append(order, f.about)
		}
		s.known = s.known.narrow(f)
		if f.out {
			s.outs = append(s.outs, f.ne)
		}
	}
	for _, k := range order {
		if states[k].empty() {
			return true
		}
	}
	return false
}

// empty reports whether no value lies in s's interval once the values it
// rules out, and zero when the interval rules it out, are taken away: its
// bounds cross, or it has both and every value from its lower bound to its
// upper one is ruled out.
func (s *subjectState) empty() bool {
	known, outs := s.known, slices.Clone(s.outs)
	if known.nonzero {
		outs = append(outs, constant.Int{})
	}
	if known.hasLo && known.hasHi && known.lo.Cmp(known.hi) > 0 {
		return true
	}
	if !known.hasLo || !known.hasHi {
		return false
	}
	slices.SortFunc(outs, constant.Int.Cmp)
	lo := known.lo
	for _, v := range outs {
		if v.Cmp(lo) != 0 {
			continue
		}
		next, ok := lo.Add(constant.FromInt64(1))
		if !ok {
			// Every value up to the largest a constant holds is ruled
			// out, and the upper bound is no larger.
			return true
		}
		lo = next
	}
	return lo.Cmp(known.hi) > 0
}

// keyInterval returns the bounds the type of the subject k gives it: an
// integer's type bounds, 0 and 1 for a bool, which facts hold as 0 for
// false and 1 for true (see boolFact), and 0 up to the largest int64 for
// the length a fact about a string, a view or a list is about. A path
// below a variable, whose type the key does not carry, gets none.
func (c *Checker) keyInterval(k factKey) interval {
	var known interval
	typ := keyType(k)
	switch {
	case typ == types.Bool:
		known.lo, known.hi = constant.FromInt64(0), constant.FromInt64(1)
	case typ == types.String || c.table.IsView(typ) || c.table.IsList(typ):
		known.lo, known.hi = constant.FromInt64(0), constant.FromInt64(math.MaxInt64)
	case types.IsInteger(typ) && typ != types.UntypedInt:
		known.lo, known.hi = typeBounds(typ)
	default:
		return known
	}
	known.hasLo, known.hasHi = true, true
	return known
}

// relationsContradict reports whether the relations in force hold a
// cycle of negative weight through a subject one of them names: a walk
// from it back to itself bounds its value less itself below 0, which no
// value satisfies.
func (c *Checker) relationsContradict() bool {
	var seen []factKey
	for _, f := range c.inForce() {
		if !f.relational() || !c.holds(f) {
			continue
		}
		node, isLen := f.about, false
		if f.diff != nil {
			node, isLen = f.diff.x, f.diff.xLen
		}
		if node.isZero() || slices.Contains(seen, node) {
			continue
		}
		seen = append(seen, node)
		g := c.relationGraph(node, node, isLen, isLen)
		if g == nil {
			return false
		}
		if d, ok := g.bound(node, node); ok && d < 0 {
			return true
		}
	}
	return false
}

// unreachableNotes returns the notes for an unreachable place that is not
// proven: for each guard in scope that compares a field of a mutable
// variable or a mut parameter, or a mutable global, that it proves
// nothing about it; and, for each subject whose facts, dropped by a write,
// would have proven the place had they held, the guards that gave them
// and the write (see dropNotes). Any other way the proof falls short gets
// no note.
func (c *Checker) unreachableNotes() []diag.Diagnostic {
	var notes []diag.Diagnostic
	var globals []*ast.Var
	var guards []*guard
	for i, g := range c.guards {
		if g.cond == nil || !c.guardInForce(i) || slices.Contains(guards, g) {
			continue
		}
		guards = append(guards, g)
		for _, side := range comparedSides(g.cond) {
			if v, ok := c.mutableGlobal(side); ok && !slices.Contains(globals, v) {
				globals = append(globals, v)
				notes = append(notes, globalNote(v)...)
			}
		}
		for _, side := range comparedSides(g.cond) {
			if c.mutableField(side) {
				notes = append(notes, note(g.cond.Spanned(), g.subject()+
					" proves nothing: a field of a mutable variable or a mut parameter never carries a fact; copy "+
					c.text(side.Spanned())+" into a variable first"))
				break
			}
		}
	}
	return append(notes, c.droppedNotes()...)
}

// droppedNotes returns, for each integer subject, in the order its first
// fact was put in force, whose facts a write dropped and which would have
// proven the place on their own had they held, the notes dropNotes gives
// them. A stored value's facts, the bounds of a loop it keeps, a value
// ruled out and a relation take part in no note, as in explain.
func (c *Checker) droppedNotes() []diag.Diagnostic {
	var order []factKey
	byKey := make(map[factKey][]int)
	// The facts chosen here are cleared by provenWithout per subject. The
	// relation index's shadow chains stay sound under that only because
	// every fact of a subject is cleared together, invariant facts aside,
	// which have no endings; filtering on another guard kind here would
	// break that (see relindex.go).
	for i, f := range c.inForce() {
		if f.relational() || f.hasNe || f.guard == nil ||
			f.guard.silent() || f.guard.kind == guardInvariant ||
			!types.IsInteger(keyType(f.about)) || c.dropOf(f) == nil {
			continue
		}
		if _, ok := byKey[f.about]; !ok {
			order = append(order, f.about)
		}
		byKey[f.about] = append(byKey[f.about], i)
	}
	var notes []diag.Diagnostic
	for _, k := range order {
		at := byKey[k]
		if !c.provenWithout(at) {
			continue
		}
		dropped := make([]fact, len(at))
		parts := make(map[ast.Expr]*guard)
		for j, i := range at {
			dropped[j] = c.active[i]
			dropped[j].drop = c.dropOf(c.active[i])
			dropped[j].guard = partGuard(dropped[j], parts)
		}
		name := k.name()
		notes = append(notes, dropNotes(dropped, func(facts []fact) string {
			return describe(name, facts)
		})...)
	}
	return notes
}

// partGuard returns the guard a note on f names: f's own, but for a fact
// from one part of a condition, x > 0 of x > 0 && bump(mut x), where the
// condition as a whole may prove nothing of the kind, a guard for that
// part alone, made once for each part, which parts holds.
func partGuard(f fact, parts map[ast.Expr]*guard) *guard {
	g := f.guard
	if g.kind != guardCond || g.cond == nil || f.src == nil || f.src == g.cond {
		return g
	}
	if part, ok := parts[f.src]; ok {
		return part
	}
	part := &guard{cond: f.src, span: f.src.Spanned(), kind: guardCond}
	parts[f.src] = part
	return part
}

// provenWithout reports whether the place would be proven unreachable
// were the facts at the indexes at of c.active held, every write that
// ends them aside, putting the facts back as they were.
func (c *Checker) provenWithout(at []int) bool {
	saved := c.active
	c.active = slices.Clone(saved)
	for _, i := range at {
		c.active[i].drop, c.active[i].ends = nil, nil
	}
	c.factsEdited++
	defer func() {
		c.active = saved
		c.factsEdited--
	}()
	return c.unreachableProven()
}

// keyType returns the type of the variable or parameter the subject k
// is, or no type for a path below one, whose type the key does not carry.
func keyType(k factKey) types.Type {
	if k.path != "" {
		return types.Invalid
	}
	switch d := k.decl.(type) {
	case *ast.Var:
		return d.Type
	case *ast.Param:
		return d.Type
	default:
		return types.Invalid
	}
}

// comparedSides returns the operands of every comparison cond holds,
// through && || and !, in source order.
func comparedSides(cond ast.Expr) []ast.Expr {
	switch x := cond.(type) {
	case *ast.Unary:
		if x.Op == ast.Not {
			return comparedSides(x.X)
		}
	case *ast.Binary:
		if x.Op.IsLogical() {
			return append(comparedSides(x.X), comparedSides(x.Y)...)
		}
		if x.Op.IsComparison() {
			return []ast.Expr{x.X, x.Y}
		}
	}
	return nil
}

// mutableField reports whether e is a field, of an integer or a bool, of
// a mutable local variable or a mut parameter, through any fields and
// elements, which never carries a fact.
func (c *Checker) mutableField(e ast.Expr) bool {
	sel, ok := e.(*ast.Selector)
	if !ok {
		return false
	}
	if typ := ast.TypeOf(e); !types.IsInteger(typ) && typ != types.Bool {
		return false
	}
	var root ast.Expr = sel
	for {
		switch x := root.(type) {
		case *ast.Selector:
			root = x.X
			continue
		case *ast.Index:
			root = x.X
			continue
		case *ast.Ident:
			switch d := x.Def.(type) {
			case *ast.Var:
				return d.Mutable && !c.globals[d]
			case *ast.Param:
				return d.Mut
			}
		}
		return false
	}
}
