package typeck_test

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/tacklang/tack/internal/invalid"
	"github.com/tacklang/tack/typeck"
)

// relatedClosures returns relatedWrites with a closure passed to call
// after each exit, whose body proves nothing about j or k, which are
// mutable, so none of the facts about them are in force in it. A program
// holding it needs call (see withCall).
func relatedClosures(n int) []string {
	lines := append(provenJ(), "mut k := n")
	for range n {
		lines = append(lines, "j = i", "k = n", "if j < 0 || k > len(a) || j >= k {", "return -1", "}",
			"call(func() { if a[0] > 0 { report() } })", "t +%= a[j]")
	}
	return append(lines, "return t")
}

// closedRelated returns j proven, then n times two new locals copied from
// i and n, an early exit proving the first below the second and the
// second at most len(a), a closure passed to call whose body uses a
// through the first, and a use of a through it: the locals are
// immutable, so every exit's facts stay in force in every closure after
// it (see keepsInClosure), and each body's use asks the relations in
// force in it. A program holding it needs call (see withCall).
func closedRelated(n int) []string {
	lines := provenJ()
	for k := range n {
		v, w := "v"+strconv.Itoa(k), "w"+strconv.Itoa(k)
		lines = append(lines, v+" := i", w+" := n", "if "+v+" < 0 || "+w+" > len(a) || "+v+" >= "+w+" {", "return -1", "}",
			"call(func() { if a["+v+"] > 0 { report() } })", "t +%= a["+v+"]")
	}
	return append(lines, "return t")
}

// relatedLocals returns j proven, then n times two new locals copied from
// i and n, an early exit proving the first below the second and the
// second at most len(a), and a use of a through the first: every exit's
// bounds on its own locals against 0 stay in force after it, so the node
// 0 of each use's graph has edges to every local before it.
func relatedLocals(n int) []string {
	lines := provenJ()
	for k := range n {
		v, w := "v"+strconv.Itoa(k), "w"+strconv.Itoa(k)
		lines = append(lines, v+" := i", w+" := n", "if "+v+" < 0 || "+w+" > len(a) || "+v+" >= "+w+" {", "return -1", "}",
			"t +%= a["+v+"]")
	}
	return append(lines, "return t")
}

// boundParam returns j proven, then n times a new local copied from i, an
// early exit proving it below the parameter d and d at most len(a), and a
// use of a through the local: d is never written, so every exit's bound
// d <= 4 holds wherever the first does, each linked to the one before it,
// and each use's graph reaches d and every one of those bounds, alike.
func boundParam(n int) []string {
	lines := provenJ()
	for k := range n {
		v := "v" + strconv.Itoa(k)
		lines = append(lines, v+" := i", "if "+v+" < 0 || d > len(a) || "+v+" >= d {", "return -1", "}", "t +%= a["+v+"]")
	}
	return append(lines, "return t")
}

// contradictedUses returns boundLocals' 70 locals, each proven at least
// 0, then boundParam with each use under a guard that contradicts the
// bound every exit puts on d, d <= 4: each use's graph holds a cycle of
// negative weight, so its search does not settle, and the query answers
// from the graph keeping every edge alike, which takes each exit's bound
// on d, folded into the first's (see relIndex.fold), and, as 0's chains
// to the locals fill the graph before it reaches d, takes them once it
// is full (see graphBuilder.takeRest).
func contradictedUses(n int) []string {
	lines := provenJ()
	for k := range 70 {
		u := "u" + strconv.Itoa(k)
		lines = append(lines, u+", _ := pair()", "if "+u+" < 0 {", "return -1", "}")
	}
	for k := range n {
		v := "v" + strconv.Itoa(k)
		lines = append(lines, v+" := i", "if "+v+" < 0 || d > len(a) || "+v+" >= d {", "return -1", "}",
			"if d > 5 { t +%= a["+v+"] }")
	}
	return append(lines, "return t")
}

// leftBound returns boundParam on m, a mutable copy of d, each exit
// followed by a branch that writes m and leaves: every exit's bound
// m <= 4 holds after the branch, which ends only the older ones, and only
// in the branch, so each shadows the one before it without holding at
// the same places, is linked to it rather than folded, and each use's
// graph takes every one of those bounds, alike, and drops all but the
// first (see graphBuilder.add).
func leftBound(n int) []string {
	lines := append(provenJ(), "mut m := d")
	for k := range n {
		v := "v" + strconv.Itoa(k)
		lines = append(lines, v+" := i", "if "+v+" < 0 || m > len(a) || "+v+" >= m {", "return -1", "}",
			"if "+v+" == 3 {", "m = 2", "return -1", "}", "t +%= a["+v+"]")
	}
	return append(lines, "return t")
}

// repeatedConditions is a program whose graph for a query about w takes
// the relation x + 3 <= v twice, an edge with the condition that x + 3
// does not wrap, among bounds near the least int64: the graph keeping
// every edge admits the first copy and leaves the second pending, judged
// once the first has joined. A graph that kept one copy of an edge with
// conditions would have no edge pending, and so no note naming one (see
// relGraph.wrapping).
const repeatedConditions = `package main

func f(w int64, v int64, x int64, a [4]int64) int64 {
    if w > -9223372036854775807 {
        return -1
    }
    if v > w {
        return -1
    }
    if x > v {
        return -1
    }
    if x + 3 > v {
        return -1
    }
    if x + 3 > v {
        return -1
    }
    return a[w]
}

func main() {
    code, fits := int32(f(1, 1, 1, [4]int64{1, 2, 3, 4}))
    if fits { exit(code) }
}
`

// relatedUse returns a new local copied from i, an early exit proving it
// below d and d at most len(a), and a use of a through it, which only a
// query of the relations in force proves, its graph reaching 0 through
// the local's bound. Every node the graph finds before 0 is related only
// to nodes found before it, so only 0's other edges can cap it.
func relatedUse() []string {
	return []string{"v := i", "if v < 0 || d > len(a) || v >= d {", "return -1", "}", "t +%= a[v]"}
}

// boundLocals returns j proven, then n new locals given by pair, each
// proven at least 0 by an early exit, and relatedUse: 0 has a chain for
// each local, which no other fact names, so a query whose graph fills
// before it has walked them all is capped only by those it has not
// walked.
func boundLocals(n int) []string {
	lines := provenJ()
	for k := range n {
		v := "u" + strconv.Itoa(k)
		lines = append(lines, v+", _ := pair()", "if "+v+" < 0 {", "return -1", "}")
	}
	return append(append(lines, relatedUse()...), "return t")
}

// interleavedChains returns j proven, then p and k read from a, each
// proven at least 0, then n times p written and proven at least 0 again,
// then relatedUse: 0's chain of p's bounds against it starts before k's,
// and its newest edge, the one that holds, comes after k's, so the
// query, which reaches p and k through 0 alone, takes k's edge first.
func interleavedChains(n int) []string {
	lines := append(provenJ(), "mut p := a[0]", "if p < 0 {", "return -1", "}", "k := a[1]", "if k < 0 {", "return -1", "}")
	for range n {
		lines = append(lines, "p = a[0]", "if p < 0 {", "return -1", "}")
	}
	return append(append(lines, relatedUse()...), "t +%= k + p", "return t")
}

// relatedLoops returns relatedWrites with each pair of writes in a loop,
// so that every exit's relation keeps endings in loops of its region.
func relatedLoops(n int) []string {
	lines := append(provenJ(), "mut k := n")
	for range n {
		lines = append(lines, "for range 2 {", "j = i", "k = n", "}",
			"if j < 0 || k > len(a) || j >= k {", "return -1", "}", "t +%= a[j]")
	}
	return append(lines, "return t")
}

// relationShapes are the programs with many early exits, each proving a
// relation between variables the code before it writes, measured for
// how the queries of the relations grow: straight-line code, the same
// with a closure after each exit, with each pair of writes in a loop,
// with new locals related at each exit in place of the writes, and with
// a new local at each exit related to a parameter each exit bounds alike.
var relationShapes = []programShape{
	{"related writes", func(n int) string { return inF(relatedWrites(n)...) }},
	{"related locals", func(n int) string { return inF(relatedLocals(n)...) }},
	{"related writes with closures", func(n int) string { return withCall(relatedClosures(n)...) }},
	{"related writes in loops", func(n int) string { return inF(relatedLoops(n)...) }},
	{"bound parameter", func(n int) string { return inF(boundParam(n)...) }},
}

// TestRelatedExitsGrowLinearly checks that each of relationShapes, with
// thousands of early exits each proving one variable below another after
// writing or declaring both, is checked in steps linear in its exits (see
// checkGrowth): the facts read into the index of the relations in force,
// which is kept in step with the facts as they are pushed and popped, and
// the edges the queries look at, which, of the facts alike that each exit
// puts in force, judge only the newest that hold and the one before them,
// of those that hold exactly where an older one does only that one (see
// relIndex.fold), and, once a query's graph is full, only the edges to
// its nodes and as many others as tell whether one caps it; and the
// endings compared,
// which a fact pushed compares with those of the one it shadows, to
// fold it, only when they are as many. Building the index again from
// every fact in force after each exit, judging every edge a query
// reaches, or walking the endings of every fact shadowed would make them
// quadratic. With the relation oracle on, every
// query scans every fact in force too, so the test is skipped.
func TestRelatedExitsGrowLinearly(t *testing.T) {
	if typeck.RelationOracle() || typeck.ReachOracle() {
		t.Skip("the oracles scan every fact in force and its endings at every query")
	}
	for _, shape := range relationShapes {
		t.Run(shape.name, func(t *testing.T) {
			diags, small := checkWork(t, shape.src(500))
			if len(diags) > 0 {
				t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
			}
			diags, grown := checkWork(t, shape.src(2000))
			if len(diags) > 0 {
				t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
			}
			checkGrowth(t, "facts indexed", small.Indexed, grown.Indexed)
			checkGrowth(t, "relation steps", small.Relations, grown.Relations)
			checkGrowth(t, "facts read", small.Scanned, grown.Scanned)
			checkGrowth(t, "endings compared", small.Ended, grown.Ended)
		})
	}
}

// TestClosuresAfterRelatedExitsIndexLinearly checks that closedRelated,
// with thousands of early exits each relating two new locals, each
// followed by a closure whose body asks the relations in force about
// them, reads facts into the indexes of the relations, and takes steps
// of the relation queries, linearly in its exits (see checkGrowth): each
// body reads the index of the facts that stay in force in it, kept in
// step with the facts in force, rather than one built from a scan of
// every one of them for each closure, and each query, whose graph
// reaches 0 and so the bound of every exit on its own local against 0,
// stops judging them once its graph is full (see graphBuilder.takeHeld).
func TestClosuresAfterRelatedExitsIndexLinearly(t *testing.T) {
	if typeck.RelationOracle() || typeck.ReachOracle() {
		t.Skip("the oracles scan every fact in force and its endings at every query")
	}
	diags, small := checkWork(t, withCall(closedRelated(500)...))
	if len(diags) > 0 {
		t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
	}
	diags, grown := checkWork(t, withCall(closedRelated(2000)...))
	if len(diags) > 0 {
		t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
	}
	checkGrowth(t, "facts indexed", small.Indexed, grown.Indexed)
	checkGrowth(t, "relation steps", small.Relations, grown.Relations)
}

// lowBounds returns, in g, which takes a list a, n locals copied from a
// parameter, each proven above a negative bound of its own, then i
// proven below n, at most len(a), and n uses of a[i]: each use's graph
// reaches 0, the length's range, from len(a) to 0 of the largest int64,
// and the locals' bounds, edges of positive weight out of 0, so a search
// sums past an int64 though the facts can all be true. A program holding
// it needs g's call (see withList).
func lowBounds(n int) []string {
	lines := []string{"mut t int64 = 0"}
	for k := range n {
		v := "k" + strconv.Itoa(k)
		lines = append(lines, v+" := d", "if "+v+" < "+strconv.Itoa(-1-k)+" || "+v+" > 100 { return -1 }")
	}
	lines = append(lines, "if i < 0 || i >= n || n > len(a) { return -1 }")
	for range n {
		lines = append(lines, "t +%= a[i]")
	}
	return append(lines, "return t")
}

// withList returns body as the body of g, which takes a list a and the
// parameters inF's f takes but for its array, and a call of it from f.
func withList(body ...string) string {
	return inF("return g(a[:], n, d, i)") + "\nfunc g(a []int64, n int64, d int64, i int64) int64 {\n    " +
		strings.Join(body, "\n    ") + "\n}\n"
}

// TestConsistentGraphsAreNotRebuilt checks that no query builds its graph
// again keeping edges alike (see buildGraph) where the facts can all be
// true, in lowBounds, whose searches sum past an int64, and in each of
// relationShapes, while contradictedUses, whose searches each reach a
// cycle of negative weight, builds them again, so the count is seen to
// count: a query that left out no edge answers from its own graph, which
// is the one keeping them.
func TestConsistentGraphsAreNotRebuilt(t *testing.T) {
	srcs := map[string]string{"low bounds": withList(lowBounds(300)...)}
	for _, shape := range relationShapes {
		srcs[shape.name] = shape.src(300)
	}
	for name, src := range srcs {
		diags, work := checkWork(t, src)
		if len(diags) > 0 {
			t.Fatalf("%s: got %d diagnostics, first %v", name, len(diags), diags[0])
		}
		if work.Rebuilt != 0 {
			t.Errorf("%s: %d graphs built again", name, work.Rebuilt)
		}
	}
	if _, work := checkWork(t, inF(contradictedUses(30)...)); work.Rebuilt == 0 {
		t.Errorf("contradicted uses: no graph built again")
	}
}

// BenchmarkRelatedExits times checking each of relationShapes at 1000,
// 4000 and 16000 exits.
func BenchmarkRelatedExits(b *testing.B) {
	for _, shape := range relationShapes {
		for _, n := range []int{1000, 4000, 16000} {
			src := shape.src(n)
			b.Run(shape.name+"/"+strconv.Itoa(n), func(b *testing.B) {
				for range b.N {
					_, c := checkerFor(b, src)
					if diags := errorsOf(c.Check()); len(diags) > 0 {
						b.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
					}
				}
			})
		}
	}
}

// relationOracleSeeds is how many programs of each generator
// TestRelationIndexMatchesScanOnPrograms checks.
const relationOracleSeeds = 300

// TestRelationIndexMatchesScanOnPrograms checks every case of
// examples/invalid.txtar and examples/warnings.txtar, the relation,
// fact, region, reach and closure shapes, the shared endings shapes, each
// at a few sizes, and programs the relations', closures' and leaving
// writes' generators make, with every query of the relations in force
// checking their index, kept in step with the facts, against one a scan
// of every fact in force builds, and the graph it builds against the one
// from that scan, every edge judged (see typeck.CheckRelationIndex),
// which panics on a difference. Whether a program is accepted does not
// matter here, only that the two agree.
func TestRelationIndexMatchesScanOnPrograms(t *testing.T) {
	defer typeck.CheckRelationIndex()()
	check := func(t *testing.T, name string, src []byte) {
		t.Helper()
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("%s: %v", name, r)
			}
		}()
		checkWith(src, nil)
	}
	for _, path := range []string{invalid.Path, invalid.WarningsPath} {
		cases, err := invalid.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			check(t, c.Name, c.Source)
		}
	}
	for name, src := range supposedShadows {
		check(t, name, []byte(src))
	}
	check(t, "related exits", []byte(relatedExits))
	check(t, "repeated conditions", []byte(repeatedConditions))
	for _, shape := range relationShapes {
		for _, n := range []int{1, 3, 9, 17, 40} {
			check(t, fmt.Sprintf("%s/%d", shape.name, n), []byte(shape.src(n)))
		}
	}
	for _, n := range []int{1, 3, 9, 17, 40, 70} {
		check(t, fmt.Sprintf("related locals read in closures/%d", n), []byte(withCall(closedRelated(n)...)))
	}
	for _, n := range []int{1, 3, 9, 17, 40, 70, 140, 200} {
		check(t, fmt.Sprintf("related locals/%d", n), []byte(inF(relatedLocals(n)...)))
		check(t, fmt.Sprintf("bound locals/%d", n), []byte(inF(boundLocals(n)...)))
		check(t, fmt.Sprintf("interleaved chains/%d", n), []byte(inF(interleavedChains(n)...)))
		check(t, fmt.Sprintf("left bound/%d", n), []byte(inF(leftBound(n)...)))
		check(t, fmt.Sprintf("contradicted uses/%d", n), []byte(inF(contradictedUses(n)...)))
	}
	shapes := append(factShapes[:len(factShapes):len(factShapes)], regionShapes...)
	shapes = append(shapes, reachShapes...)
	shapes = append(shapes,
		reachShape{"closed locals", closedLocals, 0},
		reachShape{"closed writes", closedWrites, 0},
		reachShape{"nested closures", nestedClosures, 0},
		reachShape{"looped writes", loopedWrites, 0},
		reachShape{"leaving writes", leavingWrites, 0},
		reachShape{"unproven writes", unprovenWrites, 0},
	)
	for _, shape := range shapes {
		for _, n := range []int{1, 3, 17} {
			check(t, fmt.Sprintf("%s/%d", shape.name, n), []byte(withCall(shape.lines(n)...)))
		}
	}
	checkSharedShapes(t, check)
	rng := rand.New(rand.NewPCG(1, 0x72656c73))
	for seed := range relationOracleSeeds {
		data := make([]byte, 8+rng.IntN(64))
		for i := range data {
			data[i] = byte(rng.UintN(256))
		}
		check(t, fmt.Sprintf("relations seed %d", seed), []byte(readProgram(data).source()))
	}
	for seed := range uint64(relationOracleSeeds) {
		g := &cGen{rng: rand.New(rand.NewPCG(seed, 0x636c6f73))}
		src, _ := renderClosureProgram(g.block(4, 4, false))
		check(t, fmt.Sprintf("closure seed %d", seed), []byte(src))
	}
	for seed := range uint64(relationOracleSeeds) {
		body := leavingProgram(seedRand(seed))
		for _, style := range leavingStyles {
			check(t, fmt.Sprintf("leaving seed %d %+v", seed, style), []byte(render(body, style).src.String()))
		}
	}
}
