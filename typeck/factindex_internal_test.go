package typeck

import (
	"math/rand/v2"
	"path/filepath"
	"slices"
	"testing"
)

// TestFactIndexMatchesScanOnExamples checks every program under
// examples/ with every query of the facts in force reading their index,
// kept in step with them, and checking it, and what the query of the
// facts not shadowed reads, against a scan of every fact in force (see
// indexOracle), which panics on a difference.
func TestFactIndexMatchesScanOnExamples(t *testing.T) {
	defer func(was bool) { indexOracle = was }(indexOracle)
	indexOracle = true
	dirs, err := filepath.Glob(filepath.Join("..", "examples", "*"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, dir := range dirs {
		if c, _ := checkedExample(t, dir); c != nil {
			checked++
		}
	}
	if checked < 50 {
		t.Fatalf("only %d examples checked cleanly", checked)
	}
}

// TestClosureViewMatchesCopyOnExamples checks every program under
// examples/ with every read of the facts or guards in force in a
// closure's body checked against those a copy of them made as the body
// is entered would hold (see closureOracle), which panics on a
// difference.
func TestClosureViewMatchesCopyOnExamples(t *testing.T) {
	defer func(was bool) { closureOracle = was }(closureOracle)
	closureOracle = true
	dirs, err := filepath.Glob(filepath.Join("..", "examples", "*"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, dir := range dirs {
		if c, _ := checkedExample(t, dir); c != nil {
			checked++
		}
	}
	if checked < 50 {
		t.Fatalf("only %d examples checked cleanly", checked)
	}
}

// TestRunsWithinMatchesWalk checks, on random chains of runs of endings,
// each shared from one of a few random lists from a random position in
// it, that whenever endsWithin answers yes without a look at the
// endings (see runsWithin), a walk of them answers yes too, and that it
// does answer so for a chain whose runs each start no earlier in the
// same list as another's.
func TestRunsWithinMatchesWalk(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	lists := make([]*sharedEnds, 3)
	for i := range lists {
		list := randomEndings(r)
		for len(list) == 0 {
			list = randomEndings(r)
		}
		lists[i] = &sharedEnds{list: list}
	}
	chain := func(picks []int, froms []int) *endings {
		var first, last *endings
		for i, p := range picks {
			sh := lists[p]
			run := &endings{list: sh.list[froms[i]:], shared: sh, from: froms[i]}
			if first == nil {
				first = run
			} else {
				last.next = run
			}
			last = run
		}
		return first
	}
	for trial := range 20000 {
		n := 1 + r.IntN(3)
		picks := make([]int, n)
		fFrom := make([]int, n)
		for i := range picks {
			picks[i] = r.IntN(len(lists))
			fFrom[i] = r.IntN(len(lists[picks[i]].list))
		}
		f := chain(picks, fFrom)
		gPicks := picks
		if r.IntN(4) == 0 {
			gPicks = slices.Clone(picks)
			gPicks[r.IntN(n)] = r.IntN(len(lists))
		}
		gFrom := make([]int, n)
		for i, p := range gPicks {
			gFrom[i] = r.IntN(len(lists[p].list))
		}
		g := chain(gPicks, gFrom)
		if runsWithin(g, f) && !walkWithin(g.all(), f.all(), new(int)) {
			t.Fatalf("trial %d: runs from %v of lists %v found within runs from %v of lists %v, and a walk says no",
				trial, gFrom, gPicks, fFrom, picks)
		}
		later := slices.Clone(fFrom)
		for i := range later {
			later[i] += r.IntN(len(lists[picks[i]].list) - fFrom[i])
		}
		if !runsWithin(chain(picks, later), f) {
			t.Fatalf("trial %d: runs from %v not found within runs from %v of the same lists %v", trial, later, fFrom, picks)
		}
	}
}
