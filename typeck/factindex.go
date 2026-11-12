package typeck

import (
	"fmt"
	"iter"
	"maps"
	"reflect"
	"slices"
	"sort"
)

// The facts in force, c.active, change only when assume pushes facts or
// release pops them; supposing swaps in a copy that differs only in what
// each fact's drop and ends say, and a closure's body reads, of the
// outer facts, only those that stay in force in it, and pushes and pops
// its own after them (see enterClosure). Most queries of them ask about
// one subject, and a function with n facts in force and n such queries
// would read n facts for each of them. So when more than scanLimit facts
// are in force, a query reads c.factsBy instead: for each subject, the
// positions in c.active of the facts about it, in increasing order.
//
// The index is built from one scan of c.active the first time a query
// needs it, and from then on kept in step with c.active: assume adds the
// position of each fact it pushes to its subject's list (see push), and
// release takes the positions it pops off the end of theirs (see pop).
// Both cost a step for each fact pushed or popped, so a block of n early
// exits, each pushing facts and asking about them, takes time growing
// with n, not with n times the facts in force, as it would were the index
// built again whenever the facts change. supposing keeps every fact where
// it was, about what it was, so the index holds for its copy as it is; a
// closure's body shares the index with the code around it, which holds
// the outer facts not in force in the body too, and a query in the body
// passes over those (see factsAt). A fact a newer one shadows says the
// same of the same subjects, so the two stay in force in a body or not
// together (see keepsInClosure), and the body reads the shadowing one
// whenever it would have read the one shadowed.
//
// A block of n early exits that each prove the same of a variable, which
// the code between them writes, puts n facts about it in force, all alike
// but for where they hold, and a query about it at each use would judge
// every one against the writes that end it (see holds). Most queries ask
// only what the facts that hold say, not which guard gave them: the
// bounds of an interval, or whether some fact says one thing. For those,
// a fact f says nothing a newer fact g about the same subject does not
// when the two say the same and g holds wherever f does: neither dropped
// where it is put in force, and every write that can end g one that can
// end f, the same write at the same place in the same loops and leaving
// branches, so whatever reaches c.at and ends g ends f too. A fact so
// shadowed is left out of held, the list such a query reads (see
// heldFactsAbout), for as long as the fact shadowing it is in force: push
// takes it out and pop puts it back where it was, the lists changing
// like a stack between the two. push looks for a fact to shadow only
// among the last few held about the subject, which is all a block of
// such exits needs; missing one costs time, never an answer. supposing,
// which clears what ends the facts it holds, and provenWithout, which
// clears it for some of them, could make a shadowed fact hold where its
// shadow does not, so while either is checking, queries read every fact
// (see factsEdited).
//
// A fact the index missed could leave a proof unmade, or, through what
// a missing fact would have said to the contrary, made where it should
// not be, and a fact left out of held that said more than those kept
// could do the same. So when the tests set indexOracle every query reads
// the index, however few facts are in force, and checks it, every
// subject's list, against one built afresh from a scan of every fact in
// force, and each query of held checks that the facts it reads that hold
// say all that every fact about the subject that holds says.

// indexOracle says to read the index of the facts in force on every
// query and check it, and what a query of held reads, against a scan of
// every fact (see factindex.go). It is set in a build with the tag
// tackfactoracle, and by the tests that compare the two, which set it
// only while no other test runs.
var indexOracle = factOracle

// scanLimit is how many facts in force a query reads one by one before
// it reads them through c.factsBy, which costs a map to build.
const scanLimit = 32

// shadowScan is how many of the last facts held about a subject push
// looks among for one the fact pushed shadows.
const shadowScan = 4

// subjectIndex is the index of the facts in force by subject: by, every
// fact's position in c.active under its subject, in increasing order;
// held, the same less the positions of facts a newer one shadows; and
// hid, for each position in c.active, the fact pushing the one there took
// out of held.
type subjectIndex struct {
	by   map[factKey][]int
	held map[factKey][]int
	hid  []hidden
}

// hidden is the fact a push took out of its subject's held list: at, its
// position in c.active, and slot, where it was in the list, or at -1
// when the push shadowed none.
type hidden struct {
	at, slot int
}

// factsAbout returns the facts in force, held or dropped, whose subject
// is k, in the order they were put in force, which is the order of
// c.active. Each fact read counts as one step (see proverWork).
func (c *Checker) factsAbout(k factKey) iter.Seq[fact] {
	if len(c.active) <= scanLimit && !indexOracle {
		return c.scanFactsAbout(k)
	}
	idx := c.factIndex()
	if indexOracle {
		c.checkFactIndex()
	}
	return c.factsAt(k, idx.by[k])
}

// heldFactsAbout returns facts in force whose subject is k, as
// factsAbout does, but for those a newer one shadows, which say nothing
// it does not where they hold (see factindex.go). Only a query that asks
// what the facts that hold say may read it, never one that asks which
// guards gave them or why one does not hold.
func (c *Checker) heldFactsAbout(k factKey) iter.Seq[fact] {
	if len(c.active) <= scanLimit && !indexOracle {
		return c.scanFactsAbout(k)
	}
	idx := c.factIndex()
	if indexOracle {
		c.checkFactIndex()
	}
	if c.factsEdited > 0 {
		return c.factsAt(k, idx.by[k])
	}
	if indexOracle {
		c.checkHeld(k)
	}
	return c.factsAt(k, idx.held[k])
}

// scanFactsAbout returns the facts in force whose subject is k, read by
// a scan of every one.
func (c *Checker) scanFactsAbout(k factKey) iter.Seq[fact] {
	return func(yield func(fact) bool) {
		for _, f := range c.inForce() {
			c.work.scanned++
			if f.about == k && !yield(f) {
				return
			}
		}
	}
}

// factsAt returns the facts in force at the positions at of c.active,
// in order, those positions being some of the facts about k. In a
// closure's body, the outer facts not in force there are passed over
// (see enterClosure): every one of them when k names a variable some
// code can write, since no fact about it is kept, found by a binary
// search for the body's own, and otherwise each as it is read. When the
// tests set closureOracle, what a body reads is checked first (see
// checkClosureView).
func (c *Checker) factsAt(k factKey, at []int) iter.Seq[fact] {
	all := at
	if c.outerFacts > 0 && !k.isZero() && !immutableDecl(k.decl) {
		at = at[sort.SearchInts(at, c.outerFacts):]
	}
	if closureOracle {
		c.checkClosureView()
		for _, i := range all[:len(all)-len(at)] {
			if c.factInForce(i) {
				panic(fmt.Sprintf("typeck: a query about %v in a closure's body passes over the fact at %d, which is in force", k, i))
			}
		}
	}
	return func(yield func(fact) bool) {
		for _, i := range at {
			c.work.scanned++
			if !c.factInForce(i) {
				continue
			}
			if !yield(c.active[i]) {
				return
			}
		}
	}
}

// factIndex returns c.factsBy, building it from c.active when there is
// none yet, with no fact shadowed; once built, assume and release keep
// it in step with c.active.
func (c *Checker) factIndex() *subjectIndex {
	if c.factsBy == nil {
		by := c.scanFactIndex(true)
		held := make(map[factKey][]int, len(by))
		for k, at := range by {
			held[k] = slices.Clone(at)
		}
		hid := make([]hidden, len(c.active))
		for i := range hid {
			hid[i] = hidden{-1, -1}
		}
		c.factsBy = &subjectIndex{by: by, held: held, hid: hid}
	}
	return c.factsBy
}

// scanFactIndex returns the positions of c.active's facts by subject,
// found by a scan of every fact in force, each fact read counting as a
// step when counted is set.
func (c *Checker) scanFactIndex(counted bool) map[factKey][]int {
	idx := make(map[factKey][]int)
	for i, f := range c.active {
		if counted {
			c.work.scanned++
		}
		idx[f.about] = append(idx[f.about], i)
	}
	return idx
}

// indexFact adds c.active's last fact, just pushed, to c.factsBy, and to
// the index of the relations in force, c.relIdx, when there is an index
// to keep.
func (c *Checker) indexFact() {
	if c.factsBy != nil {
		c.factsBy.push(c, len(c.active)-1)
	}
	c.indexRelation()
}

// unindexFacts takes the facts of c.active from the one at n on, about
// to be popped, out of c.factsBy, and out of the index of the relations
// in force, c.relIdx, when there is an index to keep, last first.
func (c *Checker) unindexFacts(n int) {
	c.unindexRelations(n)
	if c.factsBy == nil {
		return
	}
	for i := len(c.active) - 1; i >= n; i-- {
		c.factsBy.pop(c, i)
	}
}

// push adds the fact at position i of c.active, the last, to idx: to its
// subject's list in by, and to its list in held, taking out of held the
// fact it shadows among the last shadowScan there, if any, and
// recording it in hid. While supposing or provenWithout is checking, no
// fact is taken out, since what ends the facts is not what it was.
func (idx *subjectIndex) push(c *Checker, i int) {
	g := c.active[i]
	k := g.about
	idx.by[k] = append(idx.by[k], i)
	h := hidden{-1, -1}
	if c.factsEdited == 0 {
		held := idx.held[k]
		for s := len(held) - 1; s >= 0 && s >= len(held)-shadowScan; s-- {
			c.work.scanned++
			if shadows(g, c.active[held[s]], &c.work.ended) {
				h = hidden{held[s], s}
				idx.held[k] = slices.Delete(held, s, s+1)
				break
			}
		}
	}
	idx.held[k] = append(idx.held[k], i)
	idx.hid = append(idx.hid, h)
}

// pop takes the fact at position i of c.active, the last, out of idx,
// putting the fact it shadowed back in held where it was. Every fact
// pushed after it has been popped, so its position is the last of both
// its subject's lists, and held is as push left it. A subject left with
// no facts is taken out, as a fresh scan would have no list for it.
func (idx *subjectIndex) pop(c *Checker, i int) {
	k := c.active[i].about
	if at := idx.by[k]; len(at) == 1 {
		delete(idx.by, k)
	} else {
		idx.by[k] = at[:len(at)-1]
	}
	held := idx.held[k]
	held = held[:len(held)-1]
	if h := idx.hid[i]; h.at >= 0 {
		held = slices.Insert(held, h.slot, h.at)
	}
	if len(held) == 0 {
		delete(idx.held, k)
	} else {
		idx.held[k] = held
	}
	idx.hid = idx.hid[:i]
}

// shadows reports whether g, a fact pushed after f about the same
// subject, says all f says wherever f holds: the two say the same (see
// sameSaid), the sides of a condition neither, neither was dropped where
// it was put in force, and every write that can end g can end f (see
// endsWithin), each ending compared counting as work.
func shadows(g, f fact, work *int) bool {
	return g.drop == nil && f.drop == nil && g.either == nil && f.either == nil &&
		sameSaid(g, f) && endsWithin(g.ends, f.ends, work)
}

// endsWithin reports whether every one of the endings g is also one of
// f, at the same place, in the same outermost loop and held by the same
// leaving branches, which reaches c.at exactly when the other does (see
// reaches), and the endings of g come in the order f's do: so g holds
// wherever f does. When the runs of both are shared, run by run, each of
// g's the end of the same list as f's, no shorter, the answer is yes
// without a look at them (see runsWithin); otherwise it walks each list
// once, each ending compared counting as work. When the tests set
// indexOracle, the answer without a look is checked against the walk.
func endsWithin(g, f *endings, work *int) bool {
	if g == nil || g == f {
		return true
	}
	if f == nil {
		return false
	}
	if runsWithin(g, f) {
		if indexOracle && !walkWithin(g.all(), f.all(), new(int)) {
			panic("typeck: shared endings found within others are not, by a walk of them")
		}
		return true
	}
	return walkWithin(g.all(), f.all(), work)
}

// runsWithin reports whether g and f, endings, have as many runs, and
// each run of g is shared from the same list as f's, from the same
// position in it or a later one: each run of g is then the end of f's,
// so the endings of g are among f's, in f's order.
func runsWithin(g, f *endings) bool {
	for ; g != nil && f != nil; g, f = g.next, f.next {
		if g.shared == nil || g.shared != f.shared || g.from < f.from {
			return false
		}
	}
	return g == nil && f == nil
}

// walkWithin reports whether every ending of g is one of f, as
// sameEnding says, in f's order, walking each once, each ending of f
// compared counting as work.
func walkWithin(g, f []ending, work *int) bool {
	j := 0
	for _, e := range g {
		for j < len(f) && !sameEnding(e, f[j]) {
			*work++
			j++
		}
		if j == len(f) {
			return false
		}
		*work++
		j++
	}
	return true
}

// sameEnding reports whether a and b end facts at the same places: the
// same position, loop and keeps, whatever their drops say.
func sameEnding(a, b ending) bool {
	return a.at == b.at && a.loop == b.loop && a.kept == b.kept
}

// checkFactIndex panics when c.factsBy, kept in step with c.active, is
// not the index a fresh scan of every fact in force builds: the same
// subjects, each with the same positions in the same order, or when a
// subject's held list is not those positions in order less some, or
// does not record one fact pushed for each in force.
func (c *Checker) checkFactIndex() {
	idx := c.factsBy
	want := c.scanFactIndex(false)
	if !maps.EqualFunc(idx.by, want, slices.Equal) {
		panic(fmt.Sprintf("typeck: the fact index holds %v, a scan finds %v", idx.by, want))
	}
	if len(idx.hid) != len(c.active) || len(idx.held) != len(idx.by) {
		panic(fmt.Sprintf("typeck: the fact index records %d pushes and %d held subjects for %d facts and %d subjects",
			len(idx.hid), len(idx.held), len(c.active), len(idx.by)))
	}
	for k, held := range idx.held {
		by := idx.by[k]
		if len(held) == 0 || held[len(held)-1] != by[len(by)-1] || !subsequence(held, by) {
			panic(fmt.Sprintf("typeck: the fact index holds %v about %v, of %v", held, k, by))
		}
	}
}

// subsequence reports whether a is b with some elements left out.
func subsequence(a, b []int) bool {
	j := 0
	for _, x := range a {
		for j < len(b) && b[j] != x {
			j++
		}
		if j == len(b) {
			return false
		}
		j++
	}
	return true
}

// checkHeld panics when a fact about k that holds at c.at says something
// that no fact held about k that holds there says: the facts a query of
// held reads must give it the answer every fact would. What two facts
// say is compared field by field, through any pointer, apart from
// sameSaid, so a mistake there is found and not repeated.
func (c *Checker) checkHeld(k factKey) {
	idx := c.factsBy
	said := func(f fact) fact {
		f.src, f.guard, f.drop, f.ends = nil, nil, nil, nil
		return f
	}
	for _, i := range idx.by[k] {
		f := c.active[i]
		if !c.factInForce(i) || !c.holds(f) {
			continue
		}
		found := slices.ContainsFunc(idx.held[k], func(j int) bool {
			g := c.active[j]
			return c.factInForce(j) && c.holds(g) && reflect.DeepEqual(said(g), said(f))
		})
		if !found {
			panic(fmt.Sprintf("typeck: the fact at %d about %v holds at %d, and no fact held about it says the same", i, k, c.at))
		}
	}
}

// sameSaid reports whether f and g say the same of their subject,
// wherever they came from and whatever ends them: the same bounds and
// relations, a difference bound alike in all it says (see sameDiff), and
// the same sides of a condition.
func sameSaid(f, g fact) bool {
	if !sameDiff(f.diff, g.diff) {
		return false
	}
	f.src, f.guard, f.drop, f.ends, f.diff = nil, nil, nil, nil, nil
	g.src, g.guard, g.drop, g.ends, g.diff = nil, nil, nil, nil, nil
	return f == g
}

// sameDiff reports whether a and b, difference bounds or nil, are both
// nil or bound the same nodes by the same amount where the same
// conditions hold.
func sameDiff(a, b *diffBound) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.x == b.x && a.y == b.y && a.w == b.w && a.xLen == b.xLen && a.yLen == b.yLen &&
		slices.Equal(a.conds, b.conds)
}
