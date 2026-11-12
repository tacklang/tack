package typeck_test

import (
	"fmt"
	"math/rand/v2"
	"strconv"
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
// with a closure after each exit, and with each pair of writes in a
// loop.
var relationShapes = []programShape{
	{"related writes", func(n int) string { return inF(relatedWrites(n)...) }},
	{"related writes with closures", func(n int) string { return withCall(relatedClosures(n)...) }},
	{"related writes in loops", func(n int) string { return inF(relatedLoops(n)...) }},
}

// TestRelatedExitsGrowLinearly checks that each of relationShapes, with
// thousands of early exits each proving j below k after writing both, is
// checked in steps linear in its exits (see checkGrowth): the facts read
// into the index of the relations in force, which is kept in step with
// the facts as they are pushed and popped, and the edges the queries look
// at, which, of the facts alike that each exit puts in force, judge only
// the newest that hold and the one before them. Building the index again
// from every fact in force after each exit, or judging every edge a
// query reaches, would make them quadratic. With the relation oracle on,
// every query scans every fact in force too, so the test is skipped.
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
		})
	}
}

// TestClosuresAfterRelatedExitsIndexLinearly checks that closedRelated,
// with thousands of early exits each relating two new locals, each
// followed by a closure whose body asks the relations in force about
// them, reads facts into the indexes of the relations linearly in its
// exits (see checkGrowth): each body reads the index of the facts that
// stay in force in it, kept in step with the facts in force, rather than
// one built from a scan of every one of them for each closure. The edges
// its queries look at still grow with the square of the exits (see
// docs/known-bugs.md), so they are not bounded here.
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
	for _, shape := range relationShapes {
		for _, n := range []int{1, 3, 9, 17, 40} {
			check(t, fmt.Sprintf("%s/%d", shape.name, n), []byte(shape.src(n)))
		}
	}
	for _, n := range []int{1, 3, 9, 17, 40, 70} {
		check(t, fmt.Sprintf("related locals read in closures/%d", n), []byte(withCall(closedRelated(n)...)))
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
