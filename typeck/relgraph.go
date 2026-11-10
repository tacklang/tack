package typeck

import (
	"math"
	"slices"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/types"
)

// This file holds the graph a query of the relations in force builds (see
// relations.go): an index of the facts by the nodes they name, kept for
// as long as the facts in force do not change, the graph of at most
// relationCap nodes a query reaches through the facts that hold, and the
// shortest paths in it, whose weights bound the differences of its nodes.

// relEdge is a bound in a query's graph, from - to <= w, which counts only
// where conds hold. fact is the index in c.active of the fact it comes
// from, or -1 for a length's range, which always holds.
type relEdge struct {
	from, to factKey
	conds    []noWrap
	w        int64
	fact     int
}

// relIndex is every edge the facts in force give, whether or not each holds
// where a query is asked, and, for each node, the edges that touch it, so
// that a query visits only the facts about the nodes it reaches. lens are
// the nodes the facts use as lengths, in the order they name them, and
// related whether any fact is a relation, without which no edge says more
// than the facts' own bounds. gen is the generation of the facts in force
// it was built for (see Checker.factGen).
type relIndex struct {
	adj     map[factKey][]int
	isLen   map[factKey]bool
	edges   []relEdge
	lens    []factKey
	gen     int
	related bool
}

// weight returns v as an edge weight, when it fits an int64.
func weight(v constant.Int) (int64, bool) {
	if !v.Fits(64, true) {
		return 0, false
	}
	return v.Int64(), true
}

// relationIndex returns the index of the facts in force, building it again
// when they have changed since it was last built.
func (c *Checker) relationIndex() *relIndex {
	if c.relIdx != nil && c.relIdx.gen == c.factGen {
		return c.relIdx
	}
	idx := &relIndex{gen: c.factGen, adj: make(map[factKey][]int), isLen: make(map[factKey]bool)}
	for i, f := range c.active {
		c.work.indexed++
		// A stored value's bounds (see guardValue) stay out of the
		// graph: they prove what they prove through the variable's own
		// interval, and a note on a missing link would name them.
		if f.inMap.isZero() && (f.guard == nil || f.guard.kind != guardValue) {
			idx.addFact(i, f)
		}
	}
	c.relIdx = idx
	return idx
}

// addFact adds the edges of f, the fact at index i of the facts in force:
// a bound on its value, against 0, and its relation, if it has one, which
// also marks the index as holding a relation.
func (idx *relIndex) addFact(i int, f fact) {
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
func (idx *relIndex) addBounds(i int, f fact) {
	zero := factKey{}
	if f.hasHi {
		idx.add(relEdge{from: f.about, to: zero, fact: i}, f.hi)
	}
	if lo, ok := f.lo.Neg(); f.hasLo && ok {
		idx.add(relEdge{from: zero, to: f.about, fact: i}, lo)
	}
}

// add adds the edge e, of weight w, when w fits an int64.
func (idx *relIndex) add(e relEdge, w constant.Int) {
	v, ok := weight(w)
	if !ok {
		return
	}
	e.w = v
	idx.adj[e.from] = append(idx.adj[e.from], len(idx.edges))
	idx.adj[e.to] = append(idx.adj[e.to], len(idx.edges))
	idx.edges = append(idx.edges, e)
}

// length records k as a length, when yes and k is not the constant.
func (idx *relIndex) length(k factKey, yes bool) {
	if yes && !k.isZero() && !idx.isLen[k] {
		idx.isLen[k] = true
		idx.lens = append(idx.lens, k)
	}
}

// relGraph is the graph of a query: its nodes, in the order they were
// found, the edges between them that hold, admitted, the ones left out
// because the graph does not prove their conditions, pending, whether the
// cap left edges out, and the shortest paths found so far from a node,
// and to one.
type relGraph struct {
	index map[factKey]int
	from  map[int]relDist
	to    map[int]relDist

	// steps, when set, counts the edges the query looks at, finding the
	// graph and following its paths (see proverWork).
	steps *int

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
// it to 0, then admits the edges between them (see admit).
func (c *Checker) relationGraph(x, y factKey, xLen, yLen bool) *relGraph {
	idx := c.relationIndex()
	if !idx.related {
		return nil
	}
	// The maps are sized for a graph at the cap, so building one does not
	// grow them again and again.
	b := &graphBuilder{
		c: c, idx: idx,
		g:     &relGraph{index: make(map[factKey]int, relationCap), steps: &c.work.relations},
		holds: make(map[int]bool, 4*relationCap), used: make(map[int]bool, 4*relationCap),
		ranged: make(map[factKey]bool), lens: idx.lens, x: x, y: y, xLen: xLen, yLen: yLen,
		edges: make([]relEdge, 0, 4*relationCap), typed: c.typedRelations,
	}
	for _, q := range []factKey{x, y} {
		if b.isLen(q) && !idx.isLen[q] {
			b.lens = append(slices.Clip(b.lens), q)
		}
	}
	b.g.found(x)
	b.g.found(y)
	for next := 0; next < len(b.g.nodes); next++ {
		b.expand(b.g.nodes[next])
	}
	b.g.admit(b.edges)
	return b.g
}

// graphBuilder is what relationGraph keeps while it finds a graph's
// nodes: the index it walks, the graph so far, which facts hold, which
// edges and lengths' ranges it has taken, the lengths, and the query's
// nodes.
type graphBuilder struct {
	c      *Checker
	idx    *relIndex
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
}

// isLen reports whether k is a length: one the facts use as one, or a
// query's node that is.
func (b *graphBuilder) isLen(k factKey) bool {
	return b.idx.isLen[k] || k == b.x && b.xLen || k == b.y && b.yLen
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
		b.edges = append(b.edges, relEdge{from: u, to: zero, w: w, fact: -1})
	}
	if neg, ok := lo.Neg(); ok {
		if w, ok := weight(neg); ok {
			b.edges = append(b.edges, relEdge{from: zero, to: u, w: w, fact: -1})
		}
	}
}

// takeEdges takes every edge of the index that holds from u, a node found,
// to a node the graph has room for, each once.
func (b *graphBuilder) takeEdges(u factKey) {
	for _, ei := range b.idx.adj[u] {
		b.g.count(1)
		e := b.idx.edges[ei]
		if b.used[ei] || !b.holdsAt(e.fact) {
			continue
		}
		other := e.to
		if other == u {
			other = e.from
		}
		if b.visit(other) {
			b.used[ei] = true
			b.edges = append(b.edges, e)
		}
	}
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
	b.edges = append(b.edges, relEdge{from: k, to: zero, w: math.MaxInt64, fact: -1})
	least := b.c.appendedLen(k, interval{})
	w := int64(0)
	if v, ok := least.lo.Neg(); least.hasLo && ok && v.Fits(64, true) {
		w = v.Int64()
	}
	b.edges = append(b.edges, relEdge{from: zero, to: k, w: w, fact: -1})
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
// src, following the admitted edges backwards. Every bound found is the
// weight of a walk of edges, so it holds whatever else the walks do: a
// cycle of negative weight, which only facts that cannot all be true give,
// is followed for at most as many rounds as the graph has nodes.
func (g *relGraph) shortest(src int, reverse bool) relDist {
	n := len(g.nodes)
	d := relDist{dist: make([]int64, n), known: make([]bool, n)}
	d.known[src] = true
	// Each edge's nodes are looked up once, not once a round.
	ends := make([][2]int, len(g.admitted))
	for i, e := range g.admitted {
		a, b := g.index[e.from], g.index[e.to]
		if reverse {
			a, b = b, a
		}
		ends[i] = [2]int{a, b}
	}
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
			break
		}
	}
	return d
}

// bound returns what the graph knows of node x less node y, when it knows
// something.
func (g *relGraph) bound(x, y factKey) (int64, bool) {
	i, okX := g.index[x]
	j, okY := g.index[y]
	if !okX || !okY {
		return 0, false
	}
	d, ok := g.from[i]
	if !ok {
		d = g.shortest(i, false)
		g.from[i] = d
	}
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
	d, ok := g.to[j]
	if !ok {
		d = g.shortest(j, true)
		g.to[j] = d
	}
	return d.dist[i], d.known[i]
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
