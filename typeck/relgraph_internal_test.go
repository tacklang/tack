package typeck

import (
	"math"
	"math/rand/v2"
	"reflect"
	"strconv"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
)

// alikeGraphs returns, for edges between nodes, the graph keeping every
// edge, as one was built before edges alike were dropped, and the graph
// a query builds dropping them, as graphBuilder.add drops them: in place
// of it the first, when its search does not settle while it admits its
// edges, and otherwise one that answers from the first once a search in
// it does not settle (see buildGraph).
func alikeGraphs(nodes []factKey, edges []relEdge) (full, g *relGraph) {
	graph := func(es []relEdge, alike bool) *relGraph {
		g := &relGraph{index: make(map[factKey]int), alike: alike}
		for _, k := range nodes {
			g.found(k)
		}
		g.taken = es
		g.admit(es)
		return g
	}
	full = graph(edges, true)
	g = graph(firstAlike(edges), false)
	if g.diverged {
		return full, graph(edges, true)
	}
	g.full = func() *relGraph { return graph(edges, true) }
	return full, g
}

// randomEdges returns between 1 and 14 edges among nodes, many of them
// repeated, each of a weight near 0 or near either end of an int64, a
// third of them with the condition that a node lies within bounds near
// either end, so that searches reach cycles of negative weight and sums
// past an int64, and conditions fail where they did not.
func randomEdges(rng *rand.Rand, nodes []factKey) []relEdge {
	weights := []int64{-3, -2, -1, 0, 1, 2, 5, math.MaxInt64, math.MaxInt64 - 2, math.MinInt64 + 1, math.MinInt64 + 3, 1 << 62, -(1 << 62)}
	bounds := []int64{0, 3, math.MaxInt64 - 3, math.MinInt64 + 3, math.MaxInt64}
	var edges []relEdge
	for range 1 + rng.IntN(14) {
		if len(edges) > 0 && rng.IntN(3) == 0 {
			edges = append(edges, edges[rng.IntN(len(edges))])
			continue
		}
		e := relEdge{
			from: nodes[rng.IntN(len(nodes))], to: nodes[rng.IntN(len(nodes))],
			w: weights[rng.IntN(len(weights))], fact: len(edges),
		}
		if rng.IntN(3) == 0 {
			cond := noWrap{of: nodes[1+rng.IntN(len(nodes)-1)]}
			if rng.IntN(2) == 0 {
				cond.hi, cond.hasHi = constant.FromInt64(bounds[rng.IntN(len(bounds))]), true
			} else {
				cond.lo, cond.hasLo = constant.FromInt64(-bounds[rng.IntN(len(bounds))]), true
			}
			e.conds = []noWrap{cond}
		}
		edges = append(edges, e)
	}
	return edges
}

// TestDroppingAlikeEdgesKeepsAnswers checks, on random graphs of 0 and
// three nodes whose edges repeat, with and without conditions, that the
// graph a query builds dropping edges alike admits the same edges as the
// one keeping them, with each edge alike with no condition after the
// first left out, leaves the same edges pending, in the same order, and
// gives the same bound of every node less every other, the same proof of
// each bound tried, and the same note on a wrapping step (see wrapping),
// each asked of a graph of its own, as each query builds its own.
func TestDroppingAlikeEdgesKeepsAnswers(t *testing.T) {
	nodes := []factKey{{}}
	for i := range 3 {
		nodes = append(nodes, factKey{decl: &ast.Param{Name: "p" + strconv.Itoa(i)}})
	}
	saids := func(es []relEdge) []edgeSaid {
		out := make([]edgeSaid, len(es))
		for i, e := range es {
			out[i] = e.said()
		}
		return out
	}
	tries := []int64{-4, -1, 0, 2, math.MaxInt64}
	rng := rand.New(rand.NewPCG(3, 0x616c696b))
	for seed := range 3000 {
		edges := randomEdges(rng, nodes)
		full, g := alikeGraphs(nodes, edges)
		if !reflect.DeepEqual(saids(g.pending), saids(full.pending)) ||
			!reflect.DeepEqual(saids(g.admitted), saids(firstAlike(full.admitted))) && !g.alike {
			t.Fatalf("seed %d, edges %v: admitted %v pending %v, keeping edges alike %v %v",
				seed, edges, g.admitted, g.pending, full.admitted, full.pending)
		}
		for _, x := range nodes {
			for _, y := range nodes {
				_, g := alikeGraphs(nodes, edges)
				gd, gk := g.bound(x, y)
				fd, fk := full.bound(x, y)
				if gd != fd || gk != fk {
					t.Fatalf("seed %d, edges %v: %v less %v is %d %v, keeping edges alike %d %v", seed, edges, x, y, gd, gk, fd, fk)
				}
				for _, w := range tries {
					_, g := alikeGraphs(nodes, edges)
					c := constant.FromInt64(w)
					if gp, fp := g.proves(x, y, c), full.proves(x, y, c); gp != fp {
						t.Fatalf("seed %d, edges %v: %v - %v <= %d proved %v, keeping edges alike %v", seed, edges, x, y, w, gp, fp)
					}
					_, g = alikeGraphs(nodes, edges)
					gn, gok := g.wrapping(x, y, c)
					fn, fok := full.wrapping(x, y, c)
					if gn != fn || gok != fok {
						t.Fatalf("seed %d, edges %v: the wrapping note on %v - %v <= %d is %q %v, keeping edges alike %q %v",
							seed, edges, x, y, w, gn, gok, fn, fok)
					}
				}
			}
		}
	}
}
