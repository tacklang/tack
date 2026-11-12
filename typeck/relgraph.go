package typeck

import (
	"container/heap"
	"fmt"
	"math"
	"math/bits"
	"slices"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/types"
)

// This file holds the graph a query of the relations in force builds (see
// relations.go) from the index of the facts by the nodes they name (see
// relindex.go): the graph of at most
// relationCap nodes a query reaches through the facts that hold, and the
// shortest paths in it, whose weights bound the differences of its nodes.

// relEdge is a bound in a query's graph, from - to <= w, which counts only
// where conds hold. fact is the index in c.active of the fact it comes
// from, or -1 for a length's range, which always holds. In the index of
// the relations in force (see relindex.go), prev is the same edge of the
// fact that fact shadows, or -1, root the oldest edge of its chain, and
// headFrom and headTo the edge's slots, or the slots of the edge it
// stands for, in the heads of its nodes, or -1 for both when the edge is
// folded: on no chain, as its fact holds exactly where the fact giving
// prev, an edge alike that is on one, does (see relIndex.fold).
type relEdge struct {
	from, to         factKey
	conds            []noWrap
	w                int64
	fact             int
	prev             int
	root             int
	headFrom, headTo int
}

// folded reports whether e, an edge of the index of the relations in
// force, is folded into prev (see relIndex.fold).
func (e relEdge) folded() bool {
	return e.headFrom < 0
}

// weight returns v as an edge weight, when it fits an int64.
func weight(v constant.Int) (int64, bool) {
	if !v.Fits(64, true) {
		return 0, false
	}
	return v.Int64(), true
}

// storedBounds reports whether f is a stored value's bounds (see
// guardValue). The graph takes them as it takes any other fact's, as
// edges that count only where f holds (see holdsAt), so a write that ends
// f, in the code it reaches, ends them too, by the same endings. They are
// listed at the variable's node alone (see edgesAt and
// relScan.addStored), so a query reaches one only from that node: a
// query's own node, or one other edges lead to, never a node no other
// fact names that a query passes 0 to find. A query takes one only once
// it has reached that node, which it does on any path through the edge
// without passing 0: the path meets 0 once, so the part of it on the
// node's other side runs from the node to a query's node by other edges.
// So leaving them off 0's list loses no path, and keeps a query that
// reaches 0 from looking at every value its function stored.
func storedBounds(f fact) bool {
	return f.guard != nil && f.guard.kind == guardValue
}

// relGraph is the graph of a query: its nodes, in the order they were
// found, the edges between them that hold, admitted, the ones left out
// because the graph does not prove their conditions, pending, whether the
// cap left edges out, and the shortest paths found so far from a node,
// and to one. Unless alike says it keeps them, no two of its edges with
// no condition are alike; edges with conditions it keeps every one of
// (see graphBuilder.add).
type relGraph struct {
	index map[factKey]int
	from  map[int]relDist
	to    map[int]relDist

	// full, on a graph that drops edges alike, built from the index,
	// returns the graph of the same query keeping them, built once, the
	// first time it is asked for: the graph answers from it once a
	// shortest path in it does not settle (see diverged and dists). A
	// graph is asked about where it is built, before the facts in force
	// change, so the two are of the same facts.
	full func() *relGraph

	// diverged says a search for shortest paths in the graph did not
	// settle: it reached a cycle of negative weight, of facts that cannot
	// all be true, or found a bound past what an int64 holds (see
	// shortest).
	diverged bool

	// alike says the graph keeps every edge alike to one before it, as
	// every graph did before they were dropped: it was built to keep
	// them, or, built from the index, found none to leave out (see
	// graphOf).
	alike bool

	// old, when the tests set relationOracle, is the graph a scan of the
	// facts in force builds for the same query, every edge judged and
	// edges alike kept, as the graph was built before it dropped them:
	// every bound found in this graph is checked against the one found
	// in old (see checkGraph and sameDist).
	old *relGraph

	// steps, when set, counts the edges the query looks at, finding the
	// graph and following its paths (see proverWork).
	steps *int

	// taken is every edge the graph took, in the order it took them,
	// before it admitted them: what the oracle compares (see checkGraph).
	taken []relEdge

	nodes    []factKey
	admitted []relEdge
	pending  []relEdge
	capped   bool
}

// count adds n to the steps the graph's query took, when they are
// counted.
func (g *relGraph) count(n int) {
	if g.steps != nil {
		*g.steps += n
	}
}

// relDist is the bound found from, or to, one node on each node: dist[i]
// when known[i].
type relDist struct {
	dist  []int64
	known []bool
}

// relationGraph returns the graph for a query about the nodes x and y,
// xLen and yLen saying which are lengths, or nil when no relation is in
// force. It finds the nodes breadth first from x and y, through the
// edges that hold, up to relationCap of them, a length's range linking
// it to 0, then admits the edges between them (see admit). When the
// tests set relationOracle, the graph is checked against the one a scan
// of the facts in force builds (see checkGraph).
func (c *Checker) relationGraph(x, y factKey, xLen, yLen bool) *relGraph {
	g := c.buildGraph(c.relationIndex(), nil, x, y, xLen, yLen, &c.work.relations)
	if g != nil && relationOracle {
		c.checkGraph(g, x, y, xLen, yLen)
	}
	return g
}

// buildGraph returns the graph for a query about x and y, as
// relationGraph does, from idx, the index of the relations in force, or,
// when scan is set, from that index a scan built, every edge at a node
// judged and every edge alike kept, counting its steps in steps.
//
// Built from the index, the graph drops edges alike (see
// graphBuilder.add); one that dropped none is the graph keeping them,
// and answers as it does (see graphOf). Where a search reaches no cycle of negative weight
// and finds no bound past an int64, its bounds settle, and are the
// weights of the shortest paths of the edges admitted, which edges alike
// do not change, so it gives every answer the graph keeping them would.
// Where it reaches one, the facts cannot all be true, and the bounds a
// search finds in as many rounds as the graph has nodes depend on how
// many edges each round follows, edges alike included, and in what
// order. So a graph whose search does not settle while it admits its
// edges is built again keeping every edge alike, and
// one whose search does not settle later answers from that graph from
// then on (see relGraph.dists): each answers as the graph keeping them
// does, whatever the facts. That graph is built only when first needed,
// from the facts in force then, so it must be asked for where g was
// built, before the facts change: every query asks its graph at once,
// and keep panics, as a bug of the checker, when the facts, the index,
// or where and how holding is judged differ from what they were.
func (c *Checker) buildGraph(idx *relIndex, scan *relScan, x, y factKey, xLen, yLen bool, steps *int) *relGraph {
	typed, plain := c.typedRelations, c.plainRelations
	g := c.graphOf(idx, scan, x, y, xLen, yLen, steps, typed, plain, false)
	if g == nil || scan != nil || g.alike {
		return g
	}
	var full *relGraph
	built := c.stampOf(idx)
	keep := func() *relGraph {
		if full == nil {
			if now := c.stampOf(idx); now != built {
				panic(fmt.Sprintf("typeck: a query's graph keeping edges alike is asked for at %+v, and was built at %+v", now, built))
			}
			// The same index and facts give a graph that holds a relation,
			// as the one built from them did, so full is never nil.
			c.work.rebuilt++
			full = c.graphOf(idx, nil, x, y, xLen, yLen, steps, typed, plain, true)
		}
		return full
	}
	if g.diverged {
		return keep()
	}
	g.full = keep
	return g
}

// graphStamp is what a query's graph is built from beyond its own nodes:
// where checking is and how holding is judged there, the facts in force,
// and the index of their relations, as idx holds them (see buildGraph).
type graphStamp struct {
	at, active, edges, edited int
	idx, relIdx               *relIndex
	deferSpan, storing        *ast.Span
	lateDefaults              int
}

// stampOf returns the graphStamp of a query's graph built from idx
// now.
func (c *Checker) stampOf(idx *relIndex) graphStamp {
	return graphStamp{
		at: c.at, active: len(c.active), edges: len(idx.edges), edited: c.factsEdited,
		idx: idx, relIdx: c.relIdx, deferSpan: c.deferSpan, storing: c.storing,
		lateDefaults: len(c.lateDefaults),
	}
}

// graphOf returns the graph buildGraph builds, from idx or scan, with
// typed and plain as the graphBuilder's, keeping every edge alike when
// alike is set or it is built from scan: from idx, it then takes the
// edges folded into each edge of a chain it takes too (see
// graphBuilder.collectChain), so it takes every edge that holds, in the
// order of the index, as a graph did before edges were folded.
func (c *Checker) graphOf(idx *relIndex, scan *relScan, x, y factKey, xLen, yLen bool, steps *int, typed, plain, alike bool) *relGraph {
	// The maps are sized for a graph at the cap, so building one does not
	// grow them again and again.
	b := &graphBuilder{
		c: c, idx: idx, scan: scan,
		g:     &relGraph{index: make(map[factKey]int, relationCap), steps: steps, alike: alike || scan != nil},
		holds: make(map[int]bool, 4*relationCap), used: make(map[int]bool, 4*relationCap),
		ranged: make(map[factKey]bool), x: x, y: y, xLen: xLen, yLen: yLen,
		edges: make([]relEdge, 0, 4*relationCap), typed: typed,
		plain: plain,
	}
	if scan != nil {
		if !scan.related {
			return nil
		}
		b.lens = scan.lens
	} else {
		if idx.relations == 0 {
			return nil
		}
		b.lens = idx.lens
	}
	for _, q := range []factKey{x, y} {
		if b.isLen(q) && !b.usedAsLen(q) {
			b.lens = append(slices.Clip(b.lens), q)
		}
	}
	b.g.found(x)
	b.g.found(y)
	for next := 0; next < len(b.g.nodes); next++ {
		b.expand(b.g.nodes[next])
	}
	if b.plainEnds != nil {
		b.c.plainEnds = b.plainEnds
	}
	// A graph that left out no edge, and walked past no edge folded into
	// one it took, took every edge the graph keeping them would, in the
	// same order, so it is that graph, and answers as it does, with no
	// graph built again.
	if !b.dropped {
		b.g.alike = true
	}
	b.g.taken = b.edges
	b.g.admit(b.edges)
	return b.g
}

// graphBuilder is what buildGraph keeps while it finds a graph's nodes:
// the index it walks, or the one a scan built, the graph so far, which
// facts hold, which edges and lengths' ranges it has taken, the edges
// taken, once there are enough to need it (see add), the lengths, and
// the query's nodes.
type graphBuilder struct {
	c      *Checker
	idx    *relIndex
	scan   *relScan
	g      *relGraph
	holds  map[int]bool
	used   map[int]bool
	ranged map[factKey]bool
	x, y   factKey
	lens   []factKey
	edges  []relEdge
	xLen   bool
	yLen   bool

	// typed says the graph counts each variable's type's bounds as edges
	// to 0 (see typeRange).
	typed bool

	// plain says the graph leaves out a stored value's bounds (see
	// storedBounds and Checker.plainRelations).
	plain bool

	// plainEnds is the set of the ends and weights of the edges taken
	// with no condition, once the graph has taken alikeScan edges (see
	// taken).
	plainEnds map[edgeEnds]bool

	// dropped says the graph left out an edge alike to one it took, or
	// found that an edge whose fact holds has others folded into it,
	// which it does not take (see add and collectChain): either way, the
	// graph keeping edges alike would take edges this one does not.
	dropped bool
}

// usedAsLen reports whether some fact in force uses k as a length.
func (b *graphBuilder) usedAsLen(k factKey) bool {
	if b.scan != nil {
		return b.scan.isLen[k]
	}
	return b.idx.isLength(k)
}

// isLen reports whether k is a length: one the facts use as one, or a
// query's node that is.
func (b *graphBuilder) isLen(k factKey) bool {
	return b.usedAsLen(k) || k == b.x && b.xLen || k == b.y && b.yLen
}

// visit adds k to the graph, and reports whether it is in it: it is, or
// the cap leaves room for it.
func (b *graphBuilder) visit(k factKey) bool {
	if _, seen := b.g.index[k]; seen {
		return true
	}
	if len(b.g.nodes) == relationCap {
		b.g.capped = true
		return false
	}
	b.g.found(k)
	return true
}

// expand takes every edge that holds from u, a node found, to a node the
// graph has room for, and, for 0 and for a length, the lengths' ranges,
// each linking a length to 0.
func (b *graphBuilder) expand(u factKey) {
	b.takeEdges(u)
	switch {
	case u.isZero():
		for _, k := range b.lens {
			if b.visit(k) {
				b.lengthRange(k)
			}
		}
	case b.isLen(u):
		if b.visit(factKey{}) {
			b.lengthRange(u)
		}
	case b.typed:
		if b.visit(factKey{}) {
			b.typeRange(u)
		}
	}
}

// typeRange takes the bounds of the type of u, a variable or a parameter
// with no path below it, once, as edges to and from 0: every value it
// holds lies within them, so they always hold. An end past what an edge's
// weight holds is left out, which only proves less.
func (b *graphBuilder) typeRange(u factKey) {
	if b.ranged[u] || u.path != "" {
		return
	}
	b.ranged[u] = true
	var typ types.Type
	switch d := u.decl.(type) {
	case *ast.Var:
		typ = d.Type
	case *ast.Param:
		typ = d.Type
	}
	if !types.IsInteger(typ) || typ == types.UntypedInt {
		return
	}
	lo, hi := typeBounds(typ)
	zero := factKey{}
	if w, ok := weight(hi); ok {
		b.add(relEdge{from: u, to: zero, w: w, fact: -1})
	}
	if neg, ok := lo.Neg(); ok {
		if w, ok := weight(neg); ok {
			b.add(relEdge{from: zero, to: u, w: w, fact: -1})
		}
	}
}

// takeEdges takes every edge of the index that holds from u, a node found,
// to a node the graph has room for, each once, in the order the index
// lists them: from the index a scan built, judging every one, and
// otherwise those takeHeld takes, then the stored value's bounds the
// graph takes at u, unless it leaves them out.
func (b *graphBuilder) takeEdges(u factKey) {
	if b.scan != nil {
		for _, ei := range b.scan.adj[u] {
			b.g.count(1)
			e := b.scan.edges[ei]
			if b.used[ei] || b.plain && storedBounds(b.c.active[e.fact]) || !b.holdsAt(e.fact) {
				continue
			}
			b.take(u, ei, e)
		}
		return
	}
	b.takeHeld(u)
	if b.plain {
		return
	}
	from, end := b.idx.storedAt(u)
	for ei := from; ei < end; ei++ {
		b.g.count(1)
		e := b.idx.edges[ei]
		if !b.used[ei] && b.holdsAt(e.fact) {
			b.take(u, ei, e)
		}
	}
}

// take takes e, the edge at ei of the index, which holds, from u, when
// the graph has room for its other node.
func (b *graphBuilder) take(u factKey, ei int, e relEdge) {
	other := e.to
	if other == u {
		other = e.from
	}
	if b.visit(other) {
		b.used[ei] = true
		b.add(e)
	}
}

// edgeEnds is an edge's nodes and weight, the key of the set of the
// edges with no condition a graph has taken (see graphBuilder.add).
type edgeEnds struct {
	from, to factKey
	w        int64
}

// add takes e into the graph's edges, unless e has no condition and the
// graph has taken an edge alike already: one from the same node, to the
// same node, of the same weight, with no condition either, whatever fact
// gives each. Such an edge is admitted with the first, and changes no
// shortest path that settles (see shortest), so the graph keeps the first
// of them alone, which is the one any edge after it repeats, and its
// nodes, its edges' order and its cap are those it would have had keeping
// every one. An edge with conditions is always kept: admit judges each
// one's conditions in turn, as edges join, and where a search does not
// settle a later copy can fail what an earlier one passed and stay
// pending, which the notes on a wrapping step read (see wrapping). Built
// from a scan, for the oracle, or again for a query whose shortest paths
// do not settle (see buildGraph), the graph keeps every edge alike, as it
// was built before it dropped them.
func (b *graphBuilder) add(e relEdge) {
	if !b.g.alike && len(e.conds) == 0 {
		if b.taken(e) {
			b.dropped = true
			return
		}
		if b.plainEnds != nil {
			b.plainEnds[edgeEnds{e.from, e.to, e.w}] = true
		}
	}
	b.edges = append(b.edges, e)
}

// taken reports whether the graph has taken an edge alike to e, which has
// no condition. A graph takes few edges as a rule, so it looks among them
// one by one until it has taken alikeScan, and from then on through a set
// of the ends and weights of those taken with no condition, which it
// builds then, from the one the checker keeps for it, cleared.
func (b *graphBuilder) taken(e relEdge) bool {
	if b.plainEnds == nil {
		if len(b.edges) < alikeScan {
			return slices.ContainsFunc(b.edges, func(o relEdge) bool { return alikeEdges(o, e) })
		}
		b.plainEnds = b.c.plainEnds
		b.c.plainEnds = nil
		if b.plainEnds == nil {
			b.plainEnds = make(map[edgeEnds]bool, 2*len(b.edges))
		}
		clear(b.plainEnds)
		for _, o := range b.edges {
			if len(o.conds) == 0 {
				b.plainEnds[edgeEnds{o.from, o.to, o.w}] = true
			}
		}
	}
	return b.plainEnds[edgeEnds{e.from, e.to, e.w}]
}

// alikeScan is how many edges a graph takes before it keeps a set of
// them to find the ones alike (see graphBuilder.add).
const alikeScan = 16

// takeHeld takes the edges at u, of every fact but a stored value's,
// whose facts hold where the query is asked, each to a node the graph
// has room for and not taken before, in the order the index lists them,
// as taking every edge relIndex.holdingAt finds would, judging only the
// edges that can change what it takes (see relindex.go). While
// supposing or provenWithout is checking, it takes those holdingAt
// finds. Otherwise it walks u's chains in the order of their slots,
// each from its newest edge while the facts hold, keeping the edges
// found in pend, and takes each edge kept once it is lower than the
// oldest edge of every chain not yet walked: no edge of those can come
// before it. Once the graph is full, it stops walking, and the edges
// that hold of the chains left can only be taken when they lead to a
// node of the graph, or cap it when they lead elsewhere (see takeRest).
func (b *graphBuilder) takeHeld(u factKey) {
	idx := b.idx
	if b.c.factsEdited > 0 {
		for _, ei := range idx.holdingAt(b.c, u, b.holdsAt, b.g.steps) {
			if !b.used[ei] {
				b.take(u, ei, idx.edges[ei])
			}
		}
		return
	}
	heads := idx.heads[u]
	var pend edgeHeap
	s := 0
	for ; s < len(heads) && len(b.g.nodes) < relationCap; s++ {
		b.collectChain(heads[s], &pend)
		next := math.MaxInt
		if s+1 < len(heads) {
			next = idx.edges[heads[s+1]].root
		}
		for len(pend) > 0 && pend[0] < next {
			ei := heap.Pop(&pend).(int)
			if !b.used[ei] {
				b.take(u, ei, idx.edges[ei])
			}
		}
	}
	if s < len(heads) {
		b.takeRest(u, heads, s, pend)
	}
}

// collectChain adds to pend the edges of the chain whose newest edge is
// h, from it while their facts hold, counting a step for each edge
// judged, and, for a graph keeping edges alike, the edges folded into
// each edge added, which hold where it does (see relIndex.fold), each
// counting a step; for any other graph, it notes that it left them out
// (see graphBuilder.dropped).
func (b *graphBuilder) collectChain(h int, pend *edgeHeap) {
	for ei := h; ei >= 0; ei = b.idx.edges[ei].prev {
		b.g.count(1)
		if !b.holdsAt(b.idx.edges[ei].fact) {
			return
		}
		heap.Push(pend, ei)
		if !b.g.alike {
			b.dropped = b.dropped || len(b.idx.folds[ei]) > 0
			continue
		}
		for _, f := range b.idx.folds[ei] {
			b.g.count(1)
			heap.Push(pend, f)
		}
	}
}

// takeRest takes, for takeHeld, once the graph is full, the edges that
// hold at u of pend and of the chains at heads[s:], not walked, as
// taking every one of them in order would: those to a node of the graph,
// not taken before, in the order the index lists them, after marking the
// graph capped when any other holds. A full graph gains no node, so an
// edge to a node outside it is never taken, and only whether one holds
// matters. The chains to the graph's nodes are found from pairs, unless
// there are no more chains left than nodes, when each is looked at; a
// chain to another node is judged only from its newest edge, which holds
// when any of its edges does, and only until one holds, unless the graph
// is capped already. Each chain looked at counts a step.
func (b *graphBuilder) takeRest(u factKey, heads []int, s int, pend edgeHeap) {
	idx := b.idx
	out := []int(pend)
	if len(heads)-s <= len(b.g.nodes) {
		for _, h := range heads[s:] {
			b.g.count(1)
			if _, in := b.g.index[idx.edges[h].other(u)]; in {
				out = b.chainHeld(h, out)
			} else if !b.g.capped && b.holdsAt(idx.edges[h].fact) {
				b.g.capped = true
			}
		}
	} else {
		for _, w := range b.g.nodes {
			slots := idx.pairs[relPair{u, w}]
			first, _ := slices.BinarySearch(slots, s)
			for _, slot := range slots[first:] {
				b.g.count(1)
				out = b.chainHeld(heads[slot], out)
			}
		}
		for t := s; t < len(heads) && !b.g.capped; t++ {
			b.g.count(1)
			e := idx.edges[heads[t]]
			if _, in := b.g.index[e.other(u)]; !in && b.holdsAt(e.fact) {
				b.g.capped = true
			}
		}
	}
	slices.Sort(out)
	for _, ei := range out {
		if !b.used[ei] {
			b.take(u, ei, idx.edges[ei])
		}
	}
}

// chainHeld returns out with the edges of the chain whose newest edge is
// h added, from it while their facts hold, counting a step for each edge
// judged, and, for a graph keeping edges alike, the edges folded into
// each, as collectChain adds them, or notes that it left them out.
func (b *graphBuilder) chainHeld(h int, out []int) []int {
	for ei := h; ei >= 0; ei = b.idx.edges[ei].prev {
		b.g.count(1)
		if !b.holdsAt(b.idx.edges[ei].fact) {
			break
		}
		out = append(out, ei)
		if !b.g.alike {
			b.dropped = b.dropped || len(b.idx.folds[ei]) > 0
			continue
		}
		b.g.count(len(b.idx.folds[ei]))
		out = append(out, b.idx.folds[ei]...)
	}
	return out
}

// other returns the node of e that is not u, or u for an edge from u to
// itself.
func (e relEdge) other(u factKey) factKey {
	if e.to == u {
		return e.from
	}
	return e.to
}

// edgeHeap is a heap of edges, by their positions in the index, the
// lowest first (see container/heap).
type edgeHeap []int

// Len returns how many edges the heap holds.
func (h edgeHeap) Len() int { return len(h) }

// Less reports whether the edge at i comes before the one at j.
func (h edgeHeap) Less(i, j int) bool { return h[i] < h[j] }

// Swap swaps the edges at i and j.
func (h edgeHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

// Push adds x, an edge, at the end of the heap.
func (h *edgeHeap) Push(x any) { *h = append(*h, x.(int)) }

// Pop takes the edge at the end of the heap off it and returns it.
func (h *edgeHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

// holdsAt reports whether the fact at index i of the facts in force holds
// where the query is asked, remembering the answer.
func (b *graphBuilder) holdsAt(i int) bool {
	h, seen := b.holds[i]
	if !seen {
		h = b.c.holds(b.c.active[i])
		b.holds[i] = h
	}
	return h
}

// lengthRange takes the range of the length k, once: at most the largest
// int64, and at least 0, or the least length a successful append gives
// it (see appendedLen).
func (b *graphBuilder) lengthRange(k factKey) {
	if b.ranged[k] {
		return
	}
	b.ranged[k] = true
	zero := factKey{}
	b.add(relEdge{from: k, to: zero, w: math.MaxInt64, fact: -1})
	least := b.c.appendedLen(k, interval{})
	w := int64(0)
	if v, ok := least.lo.Neg(); least.hasLo && ok && v.Fits(64, true) {
		w = v.Int64()
	}
	b.add(relEdge{from: zero, to: k, w: w, fact: -1})
}

// found adds k to the graph's nodes, when it is not one already.
func (g *relGraph) found(k factKey) {
	if _, seen := g.index[k]; !seen {
		g.index[k] = len(g.nodes)
		g.nodes = append(g.nodes, k)
	}
}

// admit takes in edges between the graph's nodes: first those that need
// no condition, then, again and again, each conditional one whose
// conditions the graph proves, until none joins; the rest are pending.
func (g *relGraph) admit(edges []relEdge) {
	g.from, g.to = make(map[int]relDist), make(map[int]relDist)
	for _, e := range edges {
		if len(e.conds) > 0 {
			g.pending = append(g.pending, e)
		} else {
			g.admitted = append(g.admitted, e)
		}
	}
	for joined := true; joined; {
		joined = false
		for i := 0; i < len(g.pending); i++ {
			if !g.holdsAll(g.pending[i].conds) {
				continue
			}
			g.admitted = append(g.admitted, g.pending[i])
			g.pending = slices.Delete(g.pending, i, i+1)
			clear(g.from)
			clear(g.to)
			i--
			joined = true
		}
	}
}

// withPending returns g with every pending edge admitted, whatever its
// conditions.
func (g *relGraph) withPending() *relGraph {
	all := &relGraph{
		index:    g.index,
		nodes:    g.nodes,
		admitted: append(slices.Clip(g.admitted), g.pending...),
		from:     make(map[int]relDist),
		to:       make(map[int]relDist),
		steps:    g.steps,
		alike:    g.alike,
	}
	if g.old != nil {
		all.old = g.old.withPending()
	}
	if g.full != nil {
		full := g.full
		all.full = func() *relGraph { return full().withPending() }
	}
	return all
}

// sum returns a + b, and false when it is past what an int64 holds, which
// the graph then leaves out.
func sum(a, b int64) (int64, bool) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, false
	}
	return s, true
}

// shortest returns the bounds Bellman and Ford's shortest paths give from
// the node src on every node, or, when reverse is set, on every node less
// src, following the admitted edges backwards, and whether they settled.
// Every bound found is the weight of a walk of edges, so it holds whatever
// else the walks do: a cycle of negative weight, which only facts that
// cannot all be true give, is followed for at most as many rounds as the
// graph has nodes.
//
// A graph keeping edges alike finds them as every graph did before edges
// alike were dropped (see roundBounds). One that drops them finds them
// exactly (see exactBounds), and they settle only when no cycle of
// negative weight is reached from src and every bound fits an int64:
// then they are the shortest paths' weights, which neither edges alike
// nor the order of the edges change, and which roundBounds finds on the
// graph keeping them too. Whether they settle so depends only on which
// edges the graph admits, not on how many of each it holds, nor in what
// order, so a graph that drops edges alike falls back to the one keeping
// them (see dists) exactly where its answers could differ.
func (g *relGraph) shortest(src int, reverse bool) (relDist, bool) {
	// Each edge's nodes are looked up once, not once a round.
	ends := make([][2]int, len(g.admitted))
	for i, e := range g.admitted {
		a, b := g.index[e.from], g.index[e.to]
		if reverse {
			a, b = b, a
		}
		ends[i] = [2]int{a, b}
	}
	if g.alike {
		return g.roundBounds(src, ends)
	}
	return g.exactBounds(src, ends)
}

// roundBounds returns the bounds from the node src, as shortest does,
// following each admitted edge, from the node at ends[i][0] to the one
// at ends[i][1], in their order, for at most as many rounds as the graph
// has nodes, leaving out a sum past what an int64 holds, and whether a
// round changed none of them.
func (g *relGraph) roundBounds(src int, ends [][2]int) (relDist, bool) {
	n := len(g.nodes)
	d := relDist{dist: make([]int64, n), known: make([]bool, n)}
	d.known[src] = true
	for range n {
		changed := false
		g.count(len(g.admitted))
		for i, e := range g.admitted {
			a, b := ends[i][0], ends[i][1]
			if !d.known[a] {
				continue
			}
			s, ok := sum(d.dist[a], e.w)
			if ok && (!d.known[b] || s < d.dist[b]) {
				d.dist[b], d.known[b] = s, true
				changed = true
			}
		}
		if !changed {
			return d, true
		}
	}
	return d, false
}

// exactBounds returns the bounds from the node src, as shortest does,
// summed in 128 bits, so that no sum is left out, for at most as many
// rounds as the graph has nodes, and whether they settled: whether a
// round changed none of them, which, as the shortest paths of a graph
// with no cycle of negative weight reached have fewer edges than it has
// nodes, is so exactly when there is none, and every bound fits an int64.
// The walks followed have at most as many edges as rounds times edges,
// each of weight within an int64, so no sum of 128 bits wraps. When they
// do not settle, the bounds returned are those that fit an int64.
func (g *relGraph) exactBounds(src int, ends [][2]int) (relDist, bool) {
	n := len(g.nodes)
	dist := make([]wide, n)
	known := make([]bool, n)
	known[src] = true
	settled := false
	for range n {
		changed := false
		g.count(len(g.admitted))
		for i, e := range g.admitted {
			a, b := ends[i][0], ends[i][1]
			if !known[a] {
				continue
			}
			if s := dist[a].add(e.w); !known[b] || s.less(dist[b]) {
				dist[b], known[b] = s, true
				changed = true
			}
		}
		if !changed {
			settled = true
			break
		}
	}
	d := relDist{dist: make([]int64, n), known: make([]bool, n)}
	for i := range n {
		if !known[i] {
			continue
		}
		if v, ok := dist[i].int64(); ok {
			d.dist[i], d.known[i] = v, true
		} else {
			settled = false
		}
	}
	return d, settled
}

// wide is a 128-bit integer, hi the upper half, in two's complement, and
// lo the lower: the sums exactBounds keeps.
type wide struct {
	hi int64
	lo uint64
}

// add returns x + w.
func (x wide) add(w int64) wide {
	lo, carry := bits.Add64(x.lo, uint64(w), 0)
	return wide{hi: x.hi + w>>63 + int64(carry), lo: lo}
}

// less reports whether x is less than y.
func (x wide) less(y wide) bool {
	return x.hi < y.hi || x.hi == y.hi && x.lo < y.lo
}

// int64 returns x as an int64, when it fits one.
func (x wide) int64() (int64, bool) {
	v := int64(x.lo)
	return v, x.hi == v>>63
}

// bound returns what the graph knows of node x less node y, when it knows
// something.
func (g *relGraph) bound(x, y factKey) (int64, bool) {
	i, okX := g.index[x]
	j, okY := g.index[y]
	if !okX || !okY {
		return 0, false
	}
	d := g.dists(i, false)
	return d.dist[j], d.known[j]
}

// boundTo is bound, found from y backwards, which is quicker when y is
// fixed and x varies.
func (g *relGraph) boundTo(x, y factKey) (int64, bool) {
	i, okX := g.index[x]
	j, okY := g.index[y]
	if !okX || !okY {
		return 0, false
	}
	d := g.dists(j, true)
	return d.dist[i], d.known[i]
}

// dists returns the bounds the shortest paths give from the node at src
// on every node, or to it when reverse is set, found once (see shortest).
// When they do not settle, and g drops edges alike, g marks itself
// diverged, and, once it has admitted its edges, takes the graph keeping
// them in its place and finds them there, as it answers from then on
// (see buildGraph); while it admits them, buildGraph builds that graph
// once it is done. When the tests set relationOracle, the graph taken in
// g's place is checked against the one a scan builds, as checkGraph
// checks g, and the bounds kept against those of the scan's graph (see
// sameDist).
func (g *relGraph) dists(src int, reverse bool) relDist {
	cache := g.from
	if reverse {
		cache = g.to
	}
	if d, ok := cache[src]; ok {
		return d
	}
	d, settled := g.shortest(src, reverse)
	if !settled && !g.alike {
		g.diverged = true
		if g.full != nil {
			old := g.old
			*g = *g.full()
			g.old = old
			if old != nil && !g.sameGraph(old) {
				panic(fmt.Sprintf("typeck: a query's graph keeping edges alike is %v %v %v, and a scan's %v %v %v",
					g.nodes, g.admitted, g.pending, old.nodes, old.admitted, old.pending))
			}
			return g.dists(src, reverse)
		}
	}
	cache[src] = d
	g.sameDist(d, settled, src, reverse)
	return d
}

// sameDist panics when d, the bounds g found from its node at src, or to
// it when reverse is set, settled or not as settled says, are not those
// g.old, the graph a scan builds keeping every edge alike, finds on every
// node, when there is one: what is known of each node, the bound known,
// and whether they settled (see relGraph.old).
func (g *relGraph) sameDist(d relDist, settled bool, src int, reverse bool) {
	if g.old == nil {
		return
	}
	want, wantSettled := g.old.shortest(src, reverse)
	if settled != wantSettled || !slices.Equal(d.known, want.known) || !slices.Equal(d.dist, want.dist) {
		panic(fmt.Sprintf("typeck: a graph of %v finds the bounds %v %v, settled %v, from node %d, reversed %v, and the graph keeping edges alike %v %v, settled %v",
			g.nodes, d.dist, d.known, settled, src, reverse, want.dist, want.known, wantSettled))
	}
}

// proves reports whether the graph proves x - y <= w.
func (g *relGraph) proves(x, y factKey, w constant.Int) bool {
	d, ok := g.bound(x, y)
	return ok && constant.FromInt64(d).Cmp(w) <= 0
}

// holdsAll reports whether the graph proves every one of conds: the node
// at most hi, a bound on it less 0, and at least lo, a bound on 0 less it
// of at most -lo.
func (g *relGraph) holdsAll(conds []noWrap) bool {
	zero := factKey{}
	for _, cond := range conds {
		if cond.hasHi && !g.proves(cond.of, zero, cond.hi) {
			return false
		}
		if !cond.hasLo {
			continue
		}
		lo, ok := cond.lo.Neg()
		if !ok || !g.proves(zero, cond.of, lo) {
			return false
		}
	}
	return true
}
