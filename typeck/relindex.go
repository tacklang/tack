package typeck

import (
	"fmt"
	"iter"
	"maps"
	"reflect"
	"slices"

	"github.com/tacklang/tack/constant"
)

// The index of the relations in force (relIndex) is built from one scan
// of the facts in force the first time a query of them needs it, and from
// then on kept in step with them: assume adds the edges of each fact it
// pushes (see push), and release takes off the edges of each fact it pops
// (see pop), each costing a step for the fact. So a block of n early
// exits, each changing the facts in force and asking about the relations
// in them, takes time growing with n for the index, not with n times the
// facts in force, as it would were the index built again whenever the
// facts change.
//
// Each node's edges are listed in the order of the facts that give them,
// as a scan of every fact in force lists them, and a stored value's
// bounds are kept apart, one list of the facts giving them for each
// node: the graph takes only the last one's bounds, and only when that
// fact has at most reachScanLimit endings (see storedBounds and
// edgesAt).
//
// A block of n early exits that each prove the same relation, between
// variables the code between them writes, puts n facts alike in force,
// and a query at each use would judge every one of them against the
// writes that end it, though only the newest holds. So a fact g pushed
// links each of its edges to the same edge of the fact f it shadows (see
// shadows), the newest among the last shadowScan facts about its subject
// that no newer one shadows: g holds wherever f does, so of a chain of
// such links, the facts that hold where a query is asked are the newest
// ones, up to the first that does not. heads lists, for each node, the
// newest edge of each chain, and a query walks each chain from it only
// while the facts hold (see holdingAt), and then takes the edges found in
// the order every edge of the node is listed in, so it takes the same
// edges, in the same order, as one that judged every edge. supposing and
// provenWithout clear what ends some facts, so a fact shadowed could
// hold where its shadow does not: while either is checking, a query
// judges every edge, and no fact pushed is linked.
//
// A block of n early exits that each put the same bound on a variable
// nothing writes puts n facts alike in force, each shadowing the one
// before it and every one holding wherever the first does, so a chain of
// them would hold n edges alike, all held, and a query would walk every
// one, though its graph keeps only the first of edges alike (see
// graphBuilder.add). So a fact g pushed that shadows f, where every
// ending of f is also one of g, which then hold at the same places, and
// whose edges have no condition, has its edges folded into f's (see
// fold): they head no chain and are on none, a query walking the chain
// finds f's, the first of the two, and a graph keeping edges alike takes
// g's after f's (see folds).
//
// A block of n early exits that each relate new locals puts n facts in
// force that no other shadows, one bound on each local against 0, so 0
// has n chains, every one holding. A query whose graph reaches 0 can
// take only relationCap nodes, so it walks 0's chains in the order of
// their oldest edges, which is the order of their slots in heads and
// lower than any edge of a later chain, taking each edge found once no
// chain not yet walked can hold a lower one, and stops walking once the
// graph is full (see graphBuilder.takeHeld). From then on it can take
// only edges to the nodes it holds, which pairs lists, for each node and
// another, as the slots in heads of the chains between the two, and it
// judges, of the other chains, only as many as it needs to know whether
// one holds, which caps the graph. So it takes the same edges, in the
// same order, and is capped alike, as one that walked every chain.
//
// A closure's body reads, of the facts in force where it is written, only
// those keepsInClosure keeps, then its own (see enterClosure). So the
// first closure entered where there is an index gives that index a
// second, kept, of the facts in force keepsInClosure keeps, built from a
// scan of them, and from then on kept in step with it, each fact kept
// pushed to both and popped from both. The body reads kept as its index
// and pushes its own facts to it, popping them before it is left, when
// the index of the code around it is put back; a closure in the body
// does the same with kept's own second index. Where there is no index as
// a closure is entered, the body builds its own the first time a query
// in it needs one, from a scan of the facts in force in it. So a block of
// n early exits, each followed by a closure whose body asks about the
// relations, reads the facts each exit puts in force a few times, not
// once for each closure after it.
//
// An index that gave a query an edge a scan would not, or left one out,
// or took them in another order, could prove what the facts do not, or
// change which nodes a capped graph holds. So when the tests set
// relationOracle, every query checks the index against one a scan of
// every fact in force builds (see relScan), and the graph it builds
// against the one that index and a judgment of every edge give.

// relationOracle says to check, at every query of the relations in
// force, the index of them against a scan of every fact in force, and the
// graph the query builds against the one built from that scan (see
// relindex.go). It is set in a build with the tag tackfactoracle, and by
// the tests that compare the two, which set it only while no other test
// runs.
var relationOracle = factOracle

// relIndex is the index of the edges the facts in force give, whether or
// not each holds where a query is asked, kept in step with them (see
// relindex.go): edges, every one, in the order of the facts that give
// them; adj, for each node, the edges that touch it of every fact but a
// stored value's, in that order; heads, for each node, the newest edge of
// each chain of links among those (see relEdge.prev); pairs, for each
// node and another, the slots in the first's heads of the chains between
// the two, in order; folds, for each edge on a chain, the edges folded
// into it, in order (see fold); stored, for each
// node, the stored values' facts about it, as positions in facts; about,
// for each subject, the positions in facts of the facts about it no newer
// one shadows, the ones a fact pushed may shadow; facts, every fact
// indexed, in order; lens, the nodes the facts use as lengths, in the
// order they first name them, and lenCount, how many facts name each;
// relations, how many facts are relations, without which no edge says
// more than the facts' own bounds; and kept, when a closure has been
// entered, the index of the facts in this one's that keepsInClosure keeps,
// which a closure's body reads (see relindex.go).
type relIndex struct {
	kept      *relIndex
	adj       map[factKey][]int
	heads     map[factKey][]int
	pairs     map[relPair][]int
	stored    map[factKey][]int
	about     map[factKey][]int
	folds     map[int][]int
	lenCount  map[factKey]int
	edges     []relEdge
	facts     []relFact
	lens      []factKey
	relations int
}

// relPair is a node and another, or itself, a key of relIndex.pairs.
type relPair struct {
	at, other factKey
}

// relFact is a fact the index holds: at, its position in c.active, and
// about, its subject; from,
// the first of its edges in the index's edges, which run to the next
// fact's; named, the lengths it names, each once, of which the last
// newLens were named by no fact before it; relation, whether it is one;
// stored, whether it is a stored value's bounds, and within, whether such
// a fact has at most reachScanLimit endings; and hid, the fact it
// shadows, as a position in facts and its slot in its subject's list in
// about, or -1 for both when it shadows none.
type relFact struct {
	about    factKey
	at, from int
	named    []factKey
	newLens  int
	relation bool
	stored   bool
	within   bool
	hid      hidden
}

// newRelIndex returns an index holding no fact.
func newRelIndex() *relIndex {
	return &relIndex{
		adj: make(map[factKey][]int), heads: make(map[factKey][]int), pairs: make(map[relPair][]int),
		stored: make(map[factKey][]int), about: make(map[factKey][]int), lenCount: make(map[factKey]int),
		folds: make(map[int][]int),
	}
}

// relationIndex returns the index of the relations in force, building it
// from a scan of the facts in force when there is none: from then on,
// assume and release keep it in step with them (see relindex.go). When
// the tests set relationOracle, it is checked against one a scan builds
// (see checkRelationIndex).
func (c *Checker) relationIndex() *relIndex {
	if c.relIdx == nil {
		idx := newRelIndex()
		for i := range c.inForce() {
			idx.push(c, i)
		}
		c.relIdx = idx
	}
	if relationOracle {
		c.checkRelationIndex()
	}
	return c.relIdx
}

// bodyRelations returns the index of the relations in force for the body
// of a closure being entered, outer being the index of the code around
// it, once enterClosure has set outerFacts: outer's index of the facts
// keepsInClosure keeps, built the first time a body needs it from the
// facts in force in the body, which are those, and kept in step with
// outer from then on (see relindex.go); or nil when there is no outer
// index, for the body to build its own when a query in it needs one.
func (c *Checker) bodyRelations(outer *relIndex) *relIndex {
	if outer == nil {
		return nil
	}
	if outer.kept == nil {
		kept := newRelIndex()
		for i := range c.inForce() {
			kept.push(c, i)
		}
		outer.kept = kept
	}
	return outer.kept
}

// indexRelation adds c.active's last fact, just pushed, to c.relIdx,
// when there is an index to keep.
func (c *Checker) indexRelation() {
	if c.relIdx != nil {
		c.relIdx.push(c, len(c.active)-1)
	}
}

// unindexRelations takes the facts of c.active from the one at n on,
// about to be popped, out of c.relIdx, when there is an index to keep,
// last first.
func (c *Checker) unindexRelations(n int) {
	if c.relIdx == nil {
		return
	}
	for i := len(c.active) - 1; i >= n; i-- {
		c.relIdx.pop(i)
	}
}

// factEdges returns the edges of f, the fact at index i of c.active,
// each with no link, in the order a scan lists them (see relScan): the
// bounds f puts on its value against 0, then, for any fact but a stored
// value's, its relation, with the lengths that names, each once, in
// order, and whether it is one. An edge whose weight is past what an
// int64 holds is left out, which only proves less.
func factEdges(i int, f fact) (es []relEdge, named []factKey, relation bool) {
	add := func(e relEdge, w constant.Int) {
		if v, ok := weight(w); ok {
			e.w, e.fact, e.prev = v, i, -1
			es = append(es, e)
		}
	}
	length := func(k factKey, yes bool) {
		if yes && !k.isZero() && !slices.Contains(named, k) {
			named = append(named, k)
		}
	}
	zero := factKey{}
	if f.hasHi {
		add(relEdge{from: f.about, to: zero}, f.hi)
	}
	if lo, ok := f.lo.Neg(); f.hasLo && ok {
		add(relEdge{from: zero, to: f.about}, lo)
	}
	if storedBounds(f) {
		return es, nil, false
	}
	minusOne := constant.FromInt64(-1)
	switch {
	case !f.lenOf.isZero():
		add(relEdge{from: f.about, to: f.lenOf}, minusOne)
		length(f.lenOf, true)
	case !f.atMostLen.isZero():
		add(relEdge{from: f.about, to: f.atMostLen}, constant.Int{})
		length(f.atMostLen, true)
	case !f.atMost.isZero():
		w := constant.Int{}
		if f.strict {
			w = minusOne
		}
		add(relEdge{from: f.about, to: f.atMost}, w)
	case f.diff != nil:
		d := f.diff
		add(relEdge{from: d.x, to: d.y, conds: d.conds}, d.w)
		length(d.x, d.xLen)
		length(d.y, d.yLen)
		for _, cond := range d.conds {
			length(cond.of, cond.isLen)
		}
	default:
		return es, nil, false
	}
	return es, named, true
}

// push adds the fact at position i of c.active to idx, counting a step,
// and to idx.kept, when there is one and keepsInClosure keeps it:
// nothing for a key being in a map, which bounds no difference, or for a
// silent guard's fact but a stored value's; a stored value's bounds to
// the stored facts about its node; and any other fact's edges to the
// lists of the nodes they touch, each linked to the same edge of the fact
// it shadows, if any, which it then stands for in heads (see
// relindex.go).
func (idx *relIndex) push(c *Checker, i int) {
	if idx.kept != nil && c.keptAt[i] {
		idx.kept.push(c, i)
	}
	c.work.indexed++
	f := c.active[i]
	stored := storedBounds(f)
	if !f.inMap.isZero() || !stored && f.guard.silent() {
		return
	}
	es, named, relation := factEdges(i, f)
	rf := relFact{about: f.about, at: i, from: len(idx.edges), named: named, relation: relation, stored: stored, hid: hidden{-1, -1}}
	if stored {
		rf.within = f.ends == nil || f.ends.count() <= reachScanLimit
		idx.stored[f.about] = append(idx.stored[f.about], len(idx.facts))
		idx.facts = append(idx.facts, rf)
		idx.edges = append(idx.edges, es...)
		return
	}
	for _, k := range named {
		if idx.lenCount[k] == 0 {
			idx.lens = append(idx.lens, k)
			rf.newLens++
		}
		idx.lenCount[k]++
	}
	if relation {
		idx.relations++
	}
	held := idx.about[f.about]
	// No link is made while supposing or provenWithout edits the facts:
	// an index first built then would link facts by their cleared ends
	// and drops, and the links would outlive the edit.
	if c.factsEdited == 0 {
		for s := len(held) - 1; s >= 0 && s >= len(held)-shadowScan; s-- {
			c.work.relations++
			if g := idx.facts[held[s]]; shadows(f, c.active[g.at], &c.work.ended) {
				rf.hid = hidden{held[s], s}
				held = slices.Delete(held, s, s+1)
				break
			}
		}
	}
	// A fact that shadows another whose endings are all its own holds
	// exactly where that one does, so its edges are folded into those of
	// the other's chains rather than heading them (see fold). Its endings
	// are among the other's, so the two have the same only when they have
	// as many, which is checked first: a newer fact usually has fewer, and
	// walking the older one's at each push would take time growing with
	// the square of the facts.
	// A fact giving an edge with conditions is never folded: a graph
	// keeps every edge with conditions (see graphBuilder.add).
	same := false
	if rf.hid.at >= 0 && !slices.ContainsFunc(es, func(e relEdge) bool { return len(e.conds) > 0 }) {
		hid := c.active[idx.facts[rf.hid.at].at].ends
		same = hid.count() == f.ends.count() && endsWithin(hid, f.ends, &c.work.ended)
	}
	idx.about[f.about] = append(held, len(idx.facts))
	idx.facts = append(idx.facts, rf)
	for k, e := range es {
		ei := len(idx.edges)
		idx.adj[e.from] = append(idx.adj[e.from], ei)
		idx.adj[e.to] = append(idx.adj[e.to], ei)
		if same {
			idx.fold(&e, ei, idx.facts[rf.hid.at].from+k)
		} else if rf.hid.at >= 0 {
			idx.link(&e, ei, idx.facts[rf.hid.at].from+k)
		} else {
			e.root = ei
			e.headFrom = len(idx.heads[e.from])
			idx.heads[e.from] = append(idx.heads[e.from], ei)
			fromPair := relPair{e.from, e.to}
			idx.pairs[fromPair] = append(idx.pairs[fromPair], e.headFrom)
			if e.to != e.from {
				e.headTo = len(idx.heads[e.to])
				idx.heads[e.to] = append(idx.heads[e.to], ei)
				toPair := relPair{e.to, e.from}
				idx.pairs[toPair] = append(idx.pairs[toPair], e.headTo)
			}
		}
		idx.edges = append(idx.edges, e)
	}
}

// link makes e, the edge to be put at ei, stand for prev, the same edge
// of the fact e's shadows, in heads: e takes prev's slots there, and its
// chain's oldest edge, and prev becomes the one before it in its chain.
// A fact shadowed says the same as the fact shadowing it, so the two
// give the same edges in the same order, the k-th of one the k-th of the
// other. When prev is folded,
// e stands for the edge prev is folded into, whose fact holds exactly
// where prev's does, so e's holds wherever that one's does too.
func (idx *relIndex) link(e *relEdge, ei, prev int) {
	prev = idx.unfolded(prev)
	p := idx.edges[prev]
	e.prev, e.root, e.headFrom, e.headTo = prev, p.root, p.headFrom, p.headTo
	idx.heads[e.from][e.headFrom] = ei
	if e.to != e.from {
		idx.heads[e.to][e.headTo] = ei
	}
}

// fold makes e, the edge to be put at ei, folded into prev, the same
// edge of the fact e's shadows, or the edge prev is folded into: e's
// fact shadows prev's, and every ending of prev's fact is one of e's, so
// the two hold at the same places, and prev, alike and before e in the
// order the index lists edges, is the one a query takes of the two (see
// graphBuilder.add). So e heads no chain and is on none, and a query
// walking a chain of n facts alike that hold at the same places, as n
// early exits that each bound a variable nothing writes give, judges one
// of them, not n. e stays on the lists of its nodes in adj, as the
// edges a query judging every one, and a scan, find, and is listed under
// prev in folds, for a graph keeping edges alike to take after prev.
func (idx *relIndex) fold(e *relEdge, ei, prev int) {
	prev = idx.unfolded(prev)
	p := idx.edges[prev]
	e.prev, e.root, e.headFrom, e.headTo = prev, p.root, -1, -1
	idx.folds[prev] = append(idx.folds[prev], ei)
}

// unfolded returns ei, an edge of the index, or, when it is folded, the
// edge on a chain it is folded into, which is never folded itself.
func (idx *relIndex) unfolded(ei int) int {
	if idx.edges[ei].folded() {
		return idx.edges[ei].prev
	}
	return ei
}

// pop takes the fact at position i of c.active, the last, out of idx,
// and out of idx.kept, when each holds it, undoing what push did: every
// fact pushed after it has been popped, so its edges are the last of
// every list they are on, and each edge it linked stands for the one
// before it in heads.
func (idx *relIndex) pop(i int) {
	if idx.kept != nil {
		idx.kept.pop(i)
	}
	n := len(idx.facts) - 1
	if n < 0 || idx.facts[n].at != i {
		return
	}
	rf := idx.facts[n]
	if rf.stored {
		idx.stored[rf.about] = idx.stored[rf.about][:len(idx.stored[rf.about])-1]
	} else {
		for ei := len(idx.edges) - 1; ei >= rf.from; ei-- {
			e := idx.edges[ei]
			idx.adj[e.to] = idx.adj[e.to][:len(idx.adj[e.to])-1]
			idx.adj[e.from] = idx.adj[e.from][:len(idx.adj[e.from])-1]
			if e.folded() {
				if f := idx.folds[e.prev]; len(f) == 1 {
					delete(idx.folds, e.prev)
				} else {
					idx.folds[e.prev] = f[:len(f)-1]
				}
				continue
			}
			if e.prev >= 0 {
				idx.heads[e.from][e.headFrom] = e.prev
				if e.to != e.from {
					idx.heads[e.to][e.headTo] = e.prev
				}
				continue
			}
			if e.to != e.from {
				idx.heads[e.to] = idx.heads[e.to][:len(idx.heads[e.to])-1]
				toPair := relPair{e.to, e.from}
				idx.pairs[toPair] = idx.pairs[toPair][:len(idx.pairs[toPair])-1]
			}
			idx.heads[e.from] = idx.heads[e.from][:len(idx.heads[e.from])-1]
			fromPair := relPair{e.from, e.to}
			idx.pairs[fromPair] = idx.pairs[fromPair][:len(idx.pairs[fromPair])-1]
		}
		held := idx.about[rf.about]
		held = held[:len(held)-1]
		if rf.hid.at >= 0 {
			held = slices.Insert(held, rf.hid.slot, rf.hid.at)
		}
		idx.about[rf.about] = held
		for _, k := range rf.named {
			idx.lenCount[k]--
			if idx.lenCount[k] == 0 {
				delete(idx.lenCount, k)
			}
		}
		idx.lens = idx.lens[:len(idx.lens)-rf.newLens]
		if rf.relation {
			idx.relations--
		}
	}
	idx.edges = idx.edges[:rf.from]
	idx.facts = idx.facts[:n]
}

// isLength reports whether some fact in force uses k as a length.
func (idx *relIndex) isLength(k factKey) bool {
	return idx.lenCount[k] > 0
}

// factEnd returns where the edges of the fact at position p of facts end
// in edges: where the next fact's start, or the end of edges.
func (idx *relIndex) factEnd(p int) int {
	if p+1 < len(idx.facts) {
		return idx.facts[p+1].from
	}
	return len(idx.edges)
}

// storedAt returns the first and the end of the edges, in edges, of the
// stored value's bounds the graph takes at k: those of the last stored
// value's fact about k, when it has at most reachScanLimit endings, and
// none otherwise. Each such fact put in force later lies in the region
// of an earlier one, after a write that ends the earlier one there, and
// asking whether a fact with more endings holds builds an index of them
// (see reach.go), which, for every value a variable is given in a deep
// nest of blocks each writing it again, would take time growing with the
// square of the nest. Leaving a fact out only proves less, so neither
// rests on anything for soundness.
func (idx *relIndex) storedAt(k factKey) (from, end int) {
	at := idx.stored[k]
	if len(at) == 0 {
		return 0, 0
	}
	p := at[len(at)-1]
	if !idx.facts[p].within {
		return 0, 0
	}
	return idx.facts[p].from, idx.factEnd(p)
}

// edgesAt returns every edge the index gives the node k, in the order a
// scan of the facts in force lists them (see relScan): the edges of
// every fact but a stored value's that touch k, in the order of the
// facts, then the stored value's bounds the graph takes at k (see
// storedAt).
func (idx *relIndex) edgesAt(k factKey) iter.Seq[relEdge] {
	return func(yield func(relEdge) bool) {
		for _, ei := range idx.adj[k] {
			if !yield(idx.edges[ei]) {
				return
			}
		}
		from, end := idx.storedAt(k)
		for _, e := range idx.edges[from:end] {
			if !yield(e) {
				return
			}
		}
	}
}

// relScan is the index of the relations in force a scan of every fact in
// force builds, against which the tests check relIndex (see
// relationOracle): every edge the facts give, and, for each node, the
// edges that touch it, a stored value's bounds listed after the others
// and at the node alone; lens, the nodes the facts use as lengths, in
// the order they name them, and isLen, the same as a set; and related,
// whether any fact is a relation.
type relScan struct {
	adj     map[factKey][]int
	isLen   map[factKey]bool
	edges   []relEdge
	lens    []factKey
	related bool
}

// scanRelations returns the index of the relations in force a scan of
// every fact in force builds.
func (c *Checker) scanRelations() *relScan {
	idx := &relScan{adj: make(map[factKey][]int), isLen: make(map[factKey]bool)}
	var stored []int
	for i, f := range c.inForce() {
		switch {
		case !f.inMap.isZero():
			// A key being in a map bounds no difference.
		case storedBounds(f):
			stored = append(stored, i)
		case !f.guard.silent():
			idx.addFact(i, f)
		}
	}
	// Only the last stored value's fact about a node is added, and only
	// one with at most reachScanLimit writes that may end it (see
	// relIndex.storedAt).
	last := make(map[factKey]bool, len(stored))
	for _, i := range slices.Backward(stored) {
		f := c.active[i]
		if last[f.about] {
			continue
		}
		last[f.about] = true
		if f.ends == nil || f.ends.count() <= reachScanLimit {
			idx.addStored(i, f)
		}
	}
	return idx
}

// addFact adds the edges of f, the fact at index i of the facts in force:
// a bound on its value, against 0, and its relation, if it has one, which
// also marks the index as holding a relation.
func (idx *relScan) addFact(i int, f fact) {
	idx.addBounds(i, f)
	minusOne := constant.FromInt64(-1)
	switch {
	case !f.lenOf.isZero():
		idx.add(relEdge{from: f.about, to: f.lenOf, fact: i}, minusOne)
		idx.length(f.lenOf, true)
	case !f.atMostLen.isZero():
		idx.add(relEdge{from: f.about, to: f.atMostLen, fact: i}, constant.Int{})
		idx.length(f.atMostLen, true)
	case !f.atMost.isZero():
		w := constant.Int{}
		if f.strict {
			w = minusOne
		}
		idx.add(relEdge{from: f.about, to: f.atMost, fact: i}, w)
	case f.diff != nil:
		d := f.diff
		idx.add(relEdge{from: d.x, to: d.y, conds: d.conds, fact: i}, d.w)
		idx.length(d.x, d.xLen)
		idx.length(d.y, d.yLen)
		for _, cond := range d.conds {
			idx.length(cond.of, cond.isLen)
		}
	default:
		return
	}
	idx.related = true
}

// addBounds adds the edges of the bounds f, the fact at index i of the
// facts in force, puts on its value, against 0.
func (idx *relScan) addBounds(i int, f fact) {
	zero := factKey{}
	if f.hasHi {
		idx.add(relEdge{from: f.about, to: zero, fact: i}, f.hi)
	}
	if lo, ok := f.lo.Neg(); f.hasLo && ok {
		idx.add(relEdge{from: zero, to: f.about, fact: i}, lo)
	}
}

// add adds the edge e, of weight w, when w fits an int64.
func (idx *relScan) add(e relEdge, w constant.Int) {
	v, ok := weight(w)
	if !ok {
		return
	}
	e.w = v
	idx.adj[e.from] = append(idx.adj[e.from], len(idx.edges))
	idx.adj[e.to] = append(idx.adj[e.to], len(idx.edges))
	idx.edges = append(idx.edges, e)
}

// addStored adds the edges of the bounds f, a stored value's fact at
// index i of the facts in force, puts on its value, against 0, each
// listed at f's node alone, not at 0's (see storedBounds).
func (idx *relScan) addStored(i int, f fact) {
	zero := factKey{}
	if f.hasHi {
		idx.addAt(f.about, relEdge{from: f.about, to: zero, fact: i}, f.hi)
	}
	if lo, ok := f.lo.Neg(); f.hasLo && ok {
		idx.addAt(f.about, relEdge{from: zero, to: f.about, fact: i}, lo)
	}
}

// addAt adds the edge e, of weight w, when w fits an int64, listed at the
// node k alone.
func (idx *relScan) addAt(k factKey, e relEdge, w constant.Int) {
	v, ok := weight(w)
	if !ok {
		return
	}
	e.w = v
	idx.adj[k] = append(idx.adj[k], len(idx.edges))
	idx.edges = append(idx.edges, e)
}

// length records k as a length, when yes and k is not the constant.
func (idx *relScan) length(k factKey, yes bool) {
	if yes && !k.isZero() && !idx.isLen[k] {
		idx.isLen[k] = true
		idx.lens = append(idx.lens, k)
	}
}

// edgeSaid is what an edge says, and the fact that gives it, without the
// links an index keeps among edges: what two indexes compare.
type edgeSaid struct {
	from, to factKey
	conds    []noWrap
	w        int64
	fact     int
}

// said returns what e says.
func (e relEdge) said() edgeSaid {
	return edgeSaid{e.from, e.to, e.conds, e.w, e.fact}
}

// checkRelationIndex panics when c.relIdx, kept in step with the facts in
// force, does not give what the index a scan of them builds gives: the
// same edges at every node, in the same order, the same lengths, in the
// same order, and a relation or none alike.
func (c *Checker) checkRelationIndex() {
	idx, scan := c.relIdx, c.scanRelations()
	if idx.relations > 0 != scan.related {
		panic(fmt.Sprintf("typeck: the relation index holds %d relations, a scan finds related %v", idx.relations, scan.related))
	}
	if !slices.Equal(idx.lens, scan.lens) || len(idx.lenCount) != len(scan.isLen) {
		panic(fmt.Sprintf("typeck: the relation index holds lengths %v, a scan finds %v", idx.lens, scan.lens))
	}
	nodes := make(map[factKey]bool)
	for k := range scan.adj {
		nodes[k] = true
	}
	for k := range idx.adj {
		nodes[k] = true
	}
	for k := range idx.stored {
		nodes[k] = true
	}
	idx.checkPairs()
	for k := range nodes {
		var got, want []edgeSaid
		for e := range idx.edgesAt(k) {
			got = append(got, e.said())
		}
		for _, ei := range scan.adj[k] {
			want = append(want, scan.edges[ei].said())
		}
		if !reflect.DeepEqual(got, want) {
			panic(fmt.Sprintf("typeck: the relation index gives %v the edges %v, a scan %v", k, got, want))
		}
	}
}

// checkPairs panics when idx.pairs does not list, for each node and
// another, the slots in the first's heads of the chains between the two,
// in order, and nothing else, when an edge of a chain does not have its
// head's nodes and oldest edge, when a folded edge is not alike to an
// older edge on a chain, with no condition, or when idx.folds does not
// list, under each edge, the edges folded into it, in order.
func (idx *relIndex) checkPairs() {
	want := make(map[relPair][]int)
	for k, heads := range idx.heads {
		for slot, h := range heads {
			head := idx.edges[h]
			other := head.to
			if other == k {
				other = head.from
			}
			p := relPair{k, other}
			want[p] = append(want[p], slot)
			for ei := h; ei >= 0; ei = idx.edges[ei].prev {
				e := idx.edges[ei]
				if e.from != head.from || e.to != head.to || e.root != head.root || e.prev < 0 && e.root != ei {
					panic(fmt.Sprintf("typeck: the relation index chains the edge %d, %v, at %v under the head %v", ei, e, k, head))
				}
			}
		}
	}
	folds := make(map[int][]int)
	for ei, e := range idx.edges {
		if !e.folded() {
			continue
		}
		if p := idx.edges[e.prev]; e.prev >= ei || p.folded() || !alikeEdges(p, e) || len(e.conds) > 0 {
			panic(fmt.Sprintf("typeck: the relation index folds the edge %d, %v, into %d, %v", ei, e, e.prev, p))
		}
		folds[e.prev] = append(folds[e.prev], ei)
	}
	if !maps.EqualFunc(folds, idx.folds, slices.Equal) {
		panic(fmt.Sprintf("typeck: the relation index lists the folds %v, its edges %v", idx.folds, folds))
	}
	for p, slots := range idx.pairs {
		if !slices.Equal(slots, want[p]) {
			panic(fmt.Sprintf("typeck: the relation index pairs %v at the slots %v, its heads at %v", p, slots, want[p]))
		}
	}
	for p, slots := range want {
		if len(idx.pairs[p]) != len(slots) {
			panic(fmt.Sprintf("typeck: the relation index pairs %v at the slots %v, its heads at %v", p, idx.pairs[p], slots))
		}
	}
}

// checkGraph panics when g, the graph a query about x and y built from
// the relation index, is not the one a scan of the facts in force and a
// judgment of every edge at every node build, which keeps edges alike,
// with every edge with no condition alike to one before it left out,
// unless g keeps them too (see firstAlike): the same nodes, in the same
// order, the same edges taken, admitted and pending, in the same order,
// and capped alike. A graph keeps every edge with conditions, so its
// pending ones are the scan's, and admit judges them alike, in the same
// order, wherever the two find the same bounds. checkGraph then keeps
// the scan's graph as g.old, against which every bound found in g is
// checked (see relGraph.sameDist), so that dropping edges alike is seen
// to change no answer, not only the graph.
func (c *Checker) checkGraph(g *relGraph, x, y factKey, xLen, yLen bool) {
	want := c.buildGraph(nil, c.scanRelations(), x, y, xLen, yLen, new(int))
	if want == nil {
		panic(fmt.Sprintf("typeck: a query about %v and %v built a graph from the relation index, and a scan finds no relation", x, y))
	}
	if !g.sameGraph(want) {
		panic(fmt.Sprintf("typeck: a query about %v and %v built the graph %v %v %v %v from the relation index, and %v %v %v %v from a scan",
			x, y, g.nodes, g.admitted, g.pending, g.capped, want.nodes, want.admitted, want.pending, want.capped))
	}
	g.old = want
}

// sameGraph reports whether g is want, a graph keeping every edge alike,
// as checkGraph compares them: the same nodes, in the same order, the
// same edges taken, admitted and pending, in the same order, with every
// edge with no condition alike to one before it left out of want's
// unless g keeps them too, and capped alike.
func (g *relGraph) sameGraph(want *relGraph) bool {
	saids := func(es []relEdge) []edgeSaid {
		out := make([]edgeSaid, len(es))
		for i, e := range es {
			out[i] = e.said()
		}
		return out
	}
	taken, admitted, pending := want.taken, want.admitted, want.pending
	if !g.alike {
		taken, admitted, pending = firstAlike(taken), firstAlike(admitted), firstAlike(pending)
	}
	return slices.Equal(g.nodes, want.nodes) && g.capped == want.capped &&
		reflect.DeepEqual(saids(g.admitted), saids(admitted)) &&
		reflect.DeepEqual(saids(g.taken), saids(taken)) &&
		reflect.DeepEqual(saids(g.pending), saids(pending))
}

// alikeEdges reports whether a and b are alike: from the same node, to
// the same node, of the same weight, under the same conditions, which
// for the edges a graph leaves out are none (see graphBuilder.add).
func alikeEdges(a, b relEdge) bool {
	return a.from == b.from && a.to == b.to && a.w == b.w && slices.Equal(a.conds, b.conds)
}

// firstAlike returns es with every edge with no condition alike to one
// before it left out: from the same node, to the same node, of the same
// weight, whatever fact gives each, as a graph leaves them out (see
// graphBuilder.add).
func firstAlike(es []relEdge) []relEdge {
	var out []relEdge
	for _, e := range es {
		if len(e.conds) > 0 || !slices.ContainsFunc(out, func(o relEdge) bool { return alikeEdges(o, e) }) {
			out = append(out, e)
		}
	}
	return out
}

// holdingAt returns the edges at u, of every fact but a stored value's,
// whose facts hold where a query is asked, as holds judges each by its
// position in c.active, in the order the index lists them. Of each chain
// of linked edges in u's heads, the facts that hold are the newest, up
// to the first that does not (see relindex.go), so it walks each chain
// from its newest edge only while they hold, which leaves out the edges
// folded into those it finds (see fold). While supposing or
// provenWithout is checking, what ends the facts is not what it was, so
// it judges every edge at u instead, folded edges among them. Each edge
// looked at counts a step in steps, when it is set.
func (idx *relIndex) holdingAt(c *Checker, u factKey, holds func(int) bool, steps *int) []int {
	var out []int
	looked := 0
	if c.factsEdited > 0 {
		for _, ei := range idx.adj[u] {
			looked++
			if holds(idx.edges[ei].fact) {
				out = append(out, ei)
			}
		}
	} else {
		for _, h := range idx.heads[u] {
			for ei := h; ei >= 0; ei = idx.edges[ei].prev {
				looked++
				if !holds(idx.edges[ei].fact) {
					break
				}
				out = append(out, ei)
			}
		}
		slices.Sort(out)
	}
	if steps != nil {
		*steps += looked
	}
	return out
}

// heldAt returns the edges the index gives the node k whose facts hold
// where a query is asked, as holds judges each, in the order edgesAt
// lists them, but for the edges folded into another (see fold): those
// holdingAt finds, then the stored value's bounds the graph takes at k.
// An edge folded into another holds exactly where that one does and is
// alike to it, with no condition, so it narrows no bound the other does
// not, and edgeBounds, which takes the tightest bound of each kind, finds
// the same bounds without it, as its oracle checks against every edge
// at k. Each edge looked at counts a step in steps, when it is set.
func (idx *relIndex) heldAt(c *Checker, k factKey, holds func(int) bool, steps *int) []relEdge {
	var out []relEdge
	for _, ei := range idx.holdingAt(c, k, holds, steps) {
		out = append(out, idx.edges[ei])
	}
	from, end := idx.storedAt(k)
	for _, e := range idx.edges[from:end] {
		if steps != nil {
			*steps++
		}
		if holds(e.fact) {
			out = append(out, e)
		}
	}
	return out
}
