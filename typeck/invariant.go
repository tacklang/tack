package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/types"
)

// A fact about a variable ends at the first write to it that can reach
// the use, so a counter a loop steps carries nothing into the loop's next
// pass but what the loop's condition says again. A loop invariant is a
// bound on such a variable that holds everywhere in the loop, its
// condition, its post statement and its body, however many passes it has
// made: a constant bound, i >= 0 or j <= 9, or a difference bound
// against another node, j - len(s) <= -1 or lo - i <= 0 (see
// relations.go). A loop keeps a bound, and the checker puts it in force
// for the whole loop, under these rules:
//
//   - the variable is a local or a parameter of an integer type that can
//     have facts (canHoldFact), declared before the loop;
//   - the bound holds where the loop is entered: a fact in force at the
//     loop's start gives it, or the known value the variable was last
//     given, by its declaration, mut i := 0 or mut i int64, or by a plain
//     assignment, i = 0, with no write between that can reach the loop
//     (see startFact);
//   - the loop's init, which runs after the loop's start, does not write
//     the variable (see loopNodes);
//   - every write to the variable in the loop's condition, post and body
//     is i++, i--, i += e, i -= e or i = e, and keeps the bound: from the
//     facts in force where it is checked, and the bound itself, the value
//     it stores is proven within the bound, every step of it unwrapped
//     (see keeps). A call passing the variable as mut, an assignment of
//     several values and any other operator keep nothing;
//   - nothing in the loop writes the other node of a difference bound,
//     nor the string, view or list whose length it is.
//
// The bound then holds on every path through the loop, at every point of
// it: it holds on entry, nothing else changes either of its nodes, and
// each write that changes the variable runs where the bound holds, by
// induction on the writes run so far, and where the facts its proof used
// hold, so it stores a value within the bound. A continue, a break and a
// return change nothing, and deferred code in the loop runs inside it.
// So the bound needs no ending: no write in the loop can end it. It is
// not in force after the loop, where any write may follow.
//
// Whether a write keeps a bound depends on the facts in force where it
// is, which are known only once checking reaches it, while a use earlier
// in the loop, in a pass after the write, needs the answer first. So a
// function whose body has a loop that writes a variable declared outside
// it (see hasCounterLoop) is checked once more first, with what that
// check reports dropped: a probe. The probe takes the candidate bounds at
// each loop's start and judges each write against them, with no loop
// keeping anything, and keeps, for each loop, the bounds every write in it
// kept (see invariants); the check that counts puts those in force. The
// probe's facts are all true where they are, so what it proves holds,
// whatever the check after it proves besides.

// invFrame is a loop the probe is checking, with the bounds it may keep,
// each judged against every write to its variable checked in the loop.
type invFrame struct {
	loop  ast.Stmt
	nodes []ast.Node
	cands []*invCand
}

// invCand is a bound a loop may keep: f, the fact it is, about the
// variable v, and kept, the targets of the writes checked so far that
// keep it.
type invCand struct {
	v    ast.Decl
	kept map[ast.Expr]bool
	f    fact
}

// start is a known value a mutable variable was last given, as the fact
// f, holding in region, the rest of the block that gave it, and, once a
// loop's start has asked about it, ended, f with every write in region
// that may end it (see startFact).
type start struct {
	region []ast.Node
	ended  *fact
	f      fact
}

// invFrameCap is the most loops the probe judges writes for at once. A
// loop nested inside that many others that may keep bounds takes none
// of its own, which only proves less, so that a write judged against
// every loop around it costs a time bounded by the cap however deep the
// nesting.
const invFrameCap = 8

// hasCounterLoop reports whether fn's body has a loop that assigns, with
// an assignment, an increment or a decrement, a variable declared outside
// it: a parameter, or a variable declared before the loop starts.
func hasCounterLoop(fn *ast.Func) bool {
	found := false
	var loops []ast.Span
	for _, stmt := range fn.Body {
		loops = loops[:0]
		ast.Walk(stmt, func(n ast.Node) bool {
			span := n.Spanned()
			for len(loops) > 0 && span.Start >= loops[len(loops)-1].End {
				loops = loops[:len(loops)-1]
			}
			if isLoop(n) {
				loops = append(loops, span)
			}
			for _, target := range assignedTargets(n) {
				id, ok := target.(*ast.Ident)
				if ok && len(loops) > 0 && declaredBefore(id.Def, loops[len(loops)-1].Start) {
					found = true
				}
			}
			return !found
		})
	}
	return found
}

// declaredBefore reports whether decl is a parameter, or a variable
// declared before pos.
func declaredBefore(decl ast.Decl, pos int) bool {
	switch d := decl.(type) {
	case *ast.Param:
		return true
	case *ast.Var:
		return d.Span.Start < pos
	default:
		return false
	}
}

// loopNodes returns what of loop can run while it runs, for the writes
// that may break a bound it keeps: a three-clause loop's init, condition,
// post and body, and a range loop's ranged value and body. The init and
// the ranged value run once, before the loop, but a write in them is
// never one a bound is judged against, so any keeps the loop from keeping
// a bound on what it writes.
func loopNodes(loop ast.Stmt) []ast.Node {
	var nodes []ast.Node
	var body []ast.Stmt
	switch l := loop.(type) {
	case *ast.For:
		if l.Init != nil {
			nodes = append(nodes, l.Init)
		}
		if l.Post != nil {
			nodes = append(nodes, l.Post)
		}
		if l.Cond != nil {
			nodes = append(nodes, l.Cond)
		}
		body = l.Body
	case *ast.ForRange:
		nodes = append(nodes, l.X)
		body = l.Body
	}
	return append(nodes, stmtNodes(body)...)
}

// loopHeader returns the span a note about a bound loop keeps underlines:
// its for keyword through its condition or ranged value, or the keyword
// alone for a loop with neither.
func loopHeader(loop ast.Stmt) ast.Span {
	span := loop.Spanned()
	end := span.Start + len("for")
	switch l := loop.(type) {
	case *ast.For:
		if l.Cond != nil {
			end = l.Cond.Spanned().End
		}
	case *ast.ForRange:
		end = l.X.Spanned().End
	}
	span.End = end
	return span
}

// enterLoop is called as loop's header has been checked, as far as what
// runs before its condition: in the probe, it takes the bounds the loop
// may keep and judges the writes checked in it against them, until the
// returned function, called once the loop is checked, keeps those every
// write kept (see invariants); in the check that counts, it puts in force
// the bounds the probe found the loop keeps, which the caller releases,
// unless a test has every body probed to show the probe leaves no other
// trace (see probeEveryBody).
func (c *Checker) enterLoop(loop ast.Stmt) func() {
	if !c.probing {
		if facts := c.invariants[loop]; len(facts) > 0 && !c.probeEveryBody {
			g := &guard{span: loopHeader(loop), kind: guardInvariant}
			c.assume(g, facts, nil)
		}
		return func() {}
	}
	if len(c.invFrames) >= invFrameCap {
		return func() {}
	}
	nodes := loopNodes(loop)
	frame := &invFrame{loop: loop, nodes: nodes, cands: c.candidates(loop, nodes)}
	if len(frame.cands) == 0 {
		return func() {}
	}
	c.invFrames = append(c.invFrames, frame)
	return func() {
		c.invFrames = c.invFrames[:len(c.invFrames)-1]
		var kept []fact
		for _, cand := range frame.cands {
			if c.keptThrough(frame, cand) {
				kept = append(kept, cand.f)
			}
		}
		c.invariants[loop] = kept
	}
}

// candidates returns the bounds loop may keep on each variable it writes
// that can have one (see counterVar), each holding at the loop's start:
// the tightest constant bound above and below that a fact in force there
// or a known value the variable was last given says, and, for each other
// node a fact in force relates it to without conditions, the tightest
// difference bound each way.
func (c *Checker) candidates(loop ast.Stmt, nodes []ast.Node) []*invCand {
	outer := c.at
	c.at = loop.Spanned().Start
	defer func() { c.at = outer }()
	var cands []*invCand
	var seen []ast.Decl
	for _, n := range nodes {
		ast.Walk(n, func(n ast.Node) bool {
			for _, target := range assignedTargets(n) {
				id, ok := target.(*ast.Ident)
				if !ok || !c.counterVar(id.Def, loop) || containsDecl(seen, id.Def) {
					continue
				}
				seen = append(seen, id.Def)
				cands = append(cands, c.boundsOf(id.Def)...)
			}
			return true
		})
	}
	return cands
}

// counterVar reports whether decl is a variable a loop may keep a bound
// on: a parameter, or a variable declared before loop, of an integer
// type, that can have facts.
func (c *Checker) counterVar(decl ast.Decl, loop ast.Stmt) bool {
	if !declaredBefore(decl, loop.Spanned().Start) || !c.canHoldFact(decl) {
		return false
	}
	return types.IsInteger(declType(decl))
}

// boundsOf returns the bounds on the variable v that hold at c.at, as
// candidates: the tightest constant bound each way, from the facts in
// force and the known values v was last given (see starts), and, for
// each other node a fact in force relates v to without conditions, the
// tightest difference bound each way (see edgeBounds). A constant bound
// its type already gives is left out.
func (c *Checker) boundsOf(v ast.Decl) []*invCand {
	k := declKey(v)
	typeLo, typeHi := typeBounds(declType(v))
	var known interval
	for i := range c.starts {
		if s := &c.starts[i]; s.f.about == k && c.holds(c.startFact(s)) {
			known = known.narrow(s.f)
		}
	}
	known, diffs := c.edgeBounds(k, known)
	var cands []*invCand
	add := func(f fact) {
		cands = append(cands, &invCand{v: v, f: f, kept: map[ast.Expr]bool{}})
	}
	if known.hasLo && known.lo.Cmp(typeLo) > 0 {
		add(fact{about: k, lo: known.lo, hasLo: true})
	}
	if known.hasHi && known.hi.Cmp(typeHi) < 0 {
		add(fact{about: k, hi: known.hi, hasHi: true})
	}
	for _, d := range diffs {
		about := d.x
		if about.isZero() {
			about = d.y
		}
		add(fact{about: about, diff: d})
	}
	return cands
}

// edgeBounds returns known narrowed by the bounds on the node k that the
// relations in force at c.at give against the constant 0, and, for each
// other node they relate k to with no condition that a step not wrap, the
// tightest difference bound from k to it and from it to k, in the order
// the relations first name them.
func (c *Checker) edgeBounds(k factKey, known interval) (interval, []*diffBound) {
	at := map[[2]factKey]int{}
	var diffs []*diffBound
	idx := c.relationIndex()
	for _, ei := range idx.adj[k] {
		e := idx.edges[ei]
		if e.from == e.to || len(e.conds) > 0 || !c.holds(c.active[e.fact]) {
			continue
		}
		w := constant.FromInt64(e.w)
		if e.from.isZero() || e.to.isZero() {
			known = known.narrow(constantBound(e, w))
			continue
		}
		d := &diffBound{x: e.from, y: e.to, w: w, xLen: idx.isLen[e.from], yLen: idx.isLen[e.to]}
		s := [2]factKey{e.from, e.to}
		if i, seen := at[s]; !seen {
			at[s] = len(diffs)
			diffs = append(diffs, d)
		} else if w.Cmp(diffs[i].w) < 0 {
			diffs[i] = d
		}
	}
	return known, diffs
}

// constantBound returns the bound e, of weight w, between a node and the
// constant 0 puts on the node: at most w when e runs from the node to 0,
// at least -w when it runs from 0, and none when -w is past what a
// constant holds.
func constantBound(e relEdge, w constant.Int) fact {
	if e.to.isZero() {
		return fact{hi: w, hasHi: true}
	}
	lo, ok := w.Neg()
	return fact{lo: lo, hasLo: ok}
}

// recordStart remembers, for a loop's start, the known value stmt gives
// a mutable variable of an integer type (see storedValue); a loop takes
// a bound only on one that can have facts (see counterVar). rest is the rest of the block after stmt, which a write
// in ends it. Only the probe takes candidate bounds, so only the probe
// remembers.
func (c *Checker) recordStart(stmt ast.Stmt, rest []ast.Stmt) {
	if !c.probing {
		return
	}
	decl, value, ok := storedValue(stmt)
	if !ok || !types.IsInteger(declType(decl)) {
		return
	}
	v := constant.Int{}
	if value != nil {
		if v, ok = c.knownValue(value); !ok {
			return
		}
	}
	f := fact{about: declKey(decl), lo: v, hi: v, hasLo: true, hasHi: true}
	c.starts = append(c.starts, start{f: f, region: stmtNodes(rest)})
}

// storedValue returns the variable stmt gives a value whole and that
// value: a mutable variable's declaration, whose value is nil when it has
// none and starts at its zero value, or a plain assignment to a name.
func storedValue(stmt ast.Stmt) (ast.Decl, ast.Expr, bool) {
	switch s := stmt.(type) {
	case *ast.Var:
		return s, s.Value, s.Mutable
	case *ast.Assign:
		id, ok := s.Target.(*ast.Ident)
		if !ok || s.Op != ast.InvalidOp {
			return nil, nil, false
		}
		return id.Def, s.Value, true
	default:
		return nil, nil, false
	}
}

// startFact returns s's fact with every write in its region that may end
// it, for holds to judge, found once. A target whose type is not yet
// recorded, ahead of where checking has reached, counts as a write (see
// throughList), which can only end the fact sooner.
func (c *Checker) startFact(s *start) fact {
	if s.ended == nil {
		f := s.f
		f.ends = c.indexWrites(s.region).endings(f)
		s.ended = &f
	}
	return *s.ended
}

// invariantWrite judges stmt, an assignment, an increment or a
// decrement just checked, in the probe, against every bound a loop being
// checked may keep on the variable it writes (see keeps).
func (c *Checker) invariantWrite(stmt ast.Stmt) {
	if len(c.invFrames) == 0 {
		return
	}
	targets := assignedTargets(stmt)
	if len(targets) != 1 {
		return
	}
	target := targets[0]
	id, ok := target.(*ast.Ident)
	if !ok {
		return
	}
	for _, frame := range c.invFrames {
		for _, cand := range frame.cands {
			if cand.v == id.Def && c.keeps(stmt, id, cand.f) {
				cand.kept[target] = true
			}
		}
	}
}

// keeps reports whether stmt, which writes the variable target names,
// keeps the bound f on it: with f in force besides the facts in force,
// the value stmt stores is proven within f, as a query's term by the
// relations in force (see relates), or, for a constant bound, by its
// range (see rangeOf). Either proof takes in that no step of the value
// wraps.
func (c *Checker) keeps(stmt ast.Stmt, target *ast.Ident, f fact) bool {
	m := c.mark()
	c.assume(&guard{span: stmt.Spanned(), kind: guardInvariant}, []fact{f}, nil)
	defer c.release(m)
	if t, ok := c.storedTerm(stmt, target); ok && c.termKeeps(t, f, target.Def) {
		return true
	}
	if f.diff != nil {
		return false
	}
	known, ok := c.storedRange(stmt, target)
	if !ok {
		return false
	}
	if f.hasLo {
		return known.lo.Cmp(f.lo) >= 0
	}
	return known.hi.Cmp(f.hi) <= 0
}

// termKeeps reports whether the relations in force prove f, a bound on
// the variable v, of t, the value stored to v: t in v's place in a
// difference bound, or t within a constant one.
func (c *Checker) termKeeps(t linTerm, f fact, v ast.Decl) bool {
	switch {
	case f.diff != nil:
		d := f.diff
		x := linTerm{node: d.x, isLen: d.xLen}
		y := linTerm{node: d.y, isLen: d.yLen}
		if d.x == declKey(v) {
			x = t
		} else {
			y = t
		}
		return c.relates(x, y, d.w)
	case f.hasLo:
		lo, ok := f.lo.Neg()
		return ok && c.relates(zeroTerm(), t, lo)
	default:
		return c.relates(t, zeroTerm(), f.hi)
	}
}

// storedTerm returns the value stmt stores to the variable target names,
// as a query's term: the variable plus or minus one for an increment or
// a decrement, plus or minus a known value for += or -= of one, or the
// value of a plain assignment, when queryTerm reads it.
func (c *Checker) storedTerm(stmt ast.Stmt, target *ast.Ident) (linTerm, bool) {
	typ := ast.TypeOf(target)
	self := linTerm{node: declKey(target.Def), own: typeInterval(target)}
	switch s := stmt.(type) {
	case *ast.IncDec:
		if s.Dec {
			return self.step(constant.FromInt64(-1), typ)
		}
		return self.step(constant.FromInt64(1), typ)
	case *ast.Assign:
		switch s.Op {
		case ast.InvalidOp:
			return c.queryTerm(s.Value)
		case ast.Add, ast.Sub:
			k, ok := c.knownValue(s.Value)
			if !ok {
				return linTerm{}, false
			}
			if s.Op == ast.Sub {
				if k, ok = k.Neg(); !ok {
					return linTerm{}, false
				}
			}
			return self.step(k, typ)
		}
	}
	return linTerm{}, false
}

// storedRange returns the range of the value stmt stores to the variable
// target names, from the ranges of what it is computed from (see
// rangeOf): the variable's own plus or minus one, or the value's, for an
// increment or a decrement, += or -=, or the value's own for a plain
// assignment. It is false when a step of it could wrap.
func (c *Checker) storedRange(stmt ast.Stmt, target *ast.Ident) (interval, bool) {
	delta, sub, plain, ok := storedStep(stmt)
	switch {
	case !ok:
		return interval{}, false
	case plain:
		return c.rangeOf(delta)
	}
	a, ok := c.rangeOf(target)
	if !ok {
		return interval{}, false
	}
	b := single(constant.FromInt64(1))
	if delta != nil {
		if b, ok = c.rangeOf(delta); !ok {
			return interval{}, false
		}
	}
	var known interval
	var okLo, okHi bool
	if sub {
		known.lo, okLo = a.lo.Sub(b.hi)
		known.hi, okHi = a.hi.Sub(b.lo)
	} else {
		known.lo, okLo = a.lo.Add(b.lo)
		known.hi, okHi = a.hi.Add(b.hi)
	}
	if !okLo || !okHi {
		return interval{}, false
	}
	return fitted(known, ast.TypeOf(target))
}

// storedStep returns how stmt changes the variable it writes: by adding
// delta to it, or one when delta is nil, or subtracting it when sub, for
// an increment, a decrement, += and -=; or, when plain, by storing delta
// itself, for a plain assignment. It is false for any other statement.
func storedStep(stmt ast.Stmt) (delta ast.Expr, sub, plain, ok bool) {
	switch s := stmt.(type) {
	case *ast.IncDec:
		return nil, s.Dec, false, true
	case *ast.Assign:
		switch s.Op {
		case ast.InvalidOp:
			return s.Value, false, true, true
		case ast.Add, ast.Sub:
			return s.Value, s.Op == ast.Sub, false, true
		}
	}
	return nil, false, false, false
}

// keptThrough reports whether the loop of frame keeps cand: every write
// checked in it kept it, every write to its variable anywhere in the loop
// is one of those, so none went unjudged, and nothing in the loop writes
// the other node of a difference bound. A call's mut argument and an
// assignment of several values are never judged (see invariantWrite), so
// one keeps nothing.
func (c *Checker) keptThrough(frame *invFrame, cand *invCand) bool {
	ok := true
	c.eachWrite(frame.nodes, cand.v, func(w write) {
		if target, _ := w.shown.(ast.Expr); !cand.kept[target] {
			ok = false
		}
	})
	if !ok || cand.f.diff == nil {
		return ok
	}
	for _, k := range []factKey{cand.f.diff.x, cand.f.diff.y} {
		if k.isZero() || k.decl == cand.v {
			continue
		}
		c.eachWrite(frame.nodes, k.decl, func(write) { ok = false })
	}
	return ok
}
