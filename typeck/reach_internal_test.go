package typeck

import (
	"fmt"
	"math"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestReachIndexMatchesScanOnRandomEndings checks, on random lists of
// endings asked about at random positions, in random states of the
// checker, that the reach index finds the ending a scan asking reaches
// of each in turn finds (see reach.go). The keeps are random too, their
// wholes not nested and their resumes in any order along the skips, so
// the index is held to what reaches and leftBefore do, not to what the
// walk that builds keeps happens to give them. Positions are drawn from
// a small range, so that ends, starts and resumes meet and pass each
// other, at math.MaxInt too.
func TestReachIndexMatchesScanOnRandomEndings(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for trial := range 20000 {
		list := randomEndings(r)
		es := &endings{list: list}
		for range 20 {
			c := &Checker{}
			randomReachState(r, c)
			got, want := c.indexedReach(es), c.scanReach(list)
			if got != want {
				t.Fatalf("trial %d: at %d, defer %v, storing %v, late %v: the index finds %d, a scan %d\nendings %s",
					trial, c.at, c.deferSpan, c.storing, c.lateDefaults, got, want, showEndings(list))
			}
		}
	}
}

// TestSuffixIndexesMatchScanOnRandomEndings checks, on random lists of
// endings asked about from random positions in them, at random positions
// in random states of the checker, as TestReachIndexMatchesScanOnRandomEndings
// does, that the indexes of a list shared by the facts over the rest of a
// block find, among the endings from that position on, the one a scan of
// them finds (see sharedReach).
func TestSuffixIndexesMatchScanOnRandomEndings(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for trial := range 20000 {
		list := randomEndings(r)
		sh := &sharedEnds{list: list}
		for range 20 {
			c := &Checker{}
			randomReachState(r, c)
			from := r.IntN(len(list) + 1)
			got, want := c.sharedReach(sh, from), c.scanReach(list[from:])
			if got != want {
				t.Fatalf("trial %d: from %d, at %d, defer %v, storing %v, late %v: the index finds %d, a scan %d\nendings %s",
					trial, from, c.at, c.deferSpan, c.storing, c.lateDefaults, got, want, showEndings(list))
			}
		}
	}
}

// reachRange is how far the random positions of
// TestReachIndexMatchesScanOnRandomEndings run.
const reachRange = 40

// randomPos returns a random position in [0, reachRange].
func randomPos(r *rand.Rand) int {
	return r.IntN(reachRange + 1)
}

// randomSpan returns a random span within [0, reachRange], sometimes the
// zero span and sometimes an empty one, its end at or before its start.
func randomSpan(r *rand.Rand) ast.Span {
	switch r.IntN(6) {
	case 0:
		return ast.Span{}
	case 1:
		p := randomPos(r)
		return ast.Span{Start: p, End: p - r.IntN(2)}
	default:
		a, b := randomPos(r), randomPos(r)
		return ast.Span{Start: min(a, b), End: max(a, b)}
	}
}

// randomEndings returns up to 30 random endings, each with a chain of up
// to four keeps linked through skip, some of them shared between
// endings, as the keeps around one branch are.
func randomEndings(r *rand.Rand) []ending {
	var pool []*keep
	n := r.IntN(31)
	list := make([]ending, n)
	for i := range list {
		var kept *keep
		switch {
		case len(pool) > 0 && r.IntN(3) == 0:
			kept = pool[r.IntN(len(pool))]
		case r.IntN(4) != 0:
			for range 1 + r.IntN(4) {
				resume := randomPos(r)
				if r.IntN(3) == 0 {
					resume = math.MaxInt
				}
				kept = &keep{skip: kept, whole: randomSpan(r), resume: resume}
				pool = append(pool, kept)
			}
		}
		var loop ast.Span
		if r.IntN(2) == 0 {
			loop = randomSpan(r)
		}
		list[i] = ending{drop: &drop{name: fmt.Sprint(i)}, kept: kept, loop: loop, at: randomPos(r)}
	}
	return list
}

// randomReachState puts c at a random position, perhaps in deferred
// code, an assignment's targets and the late defaults of up to two
// switches.
func randomReachState(r *rand.Rand, c *Checker) {
	c.at = randomPos(r) + r.IntN(2)
	if r.IntN(4) == 0 {
		s := randomSpan(r)
		c.deferSpan = &s
	}
	if r.IntN(4) == 0 {
		s := randomSpan(r)
		c.storing = &s
	}
	for range r.IntN(3) {
		if r.IntN(2) == 0 {
			c.lateDefaults = append(c.lateDefaults, lateDefault{whole: randomSpan(r), body: randomSpan(r)})
		}
	}
}

// showEndings returns list written out, each ending's position, loop
// and keeps along their skips.
func showEndings(list []ending) string {
	s := ""
	for i, e := range list {
		s += fmt.Sprintf("\n  %d: at %d loop %v", i, e.at, e.loop)
		for k := e.kept; k != nil; k = k.skip {
			s += fmt.Sprintf(" keep{whole %v resume %d}", k.whole, k.resume)
		}
	}
	return s
}

// TestReachIndexMatchesScanOnExamples checks every program under
// examples/ with every query of a fact's endings read through their
// index, however few there are, and checked against a scan of them (see
// reachOracle), which panics on a difference.
func TestReachIndexMatchesScanOnExamples(t *testing.T) {
	defer func(was bool) { reachOracle = was }(reachOracle)
	reachOracle = true
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
