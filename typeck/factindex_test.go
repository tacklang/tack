package typeck_test

import (
	"fmt"
	"maps"
	"strconv"
	"testing"

	"github.com/tacklang/tack/internal/invalid"
	"github.com/tacklang/tack/typeck"
)

// localExits returns j proven, then n times a new local copied from i,
// an early exit proving it an index of a, and a use of a through it:
// each exit puts facts about a subject no other exit names in force for
// the rest of the block.
func localExits(n int) []string {
	lines := provenJ()
	for k := range n {
		v := "v" + strconv.Itoa(k)
		lines = append(lines, v+" := i", "if "+v+" < 0 || "+v+" >= len(a) {", "return -1", "}", "t +%= a["+v+"]")
	}
	return append(lines, "return t")
}

// factShapes are the programs with many early exits, each changing the
// facts in force for the rest of the block, measured for how a query
// reads them: exits bounding one variable, exits each bounding a new
// local, and straight-line code that writes j and proves it again before
// each use, each exit's fact keeping every write to j after it, so it is
// measured only up to 4000.
var factShapes = []reachShape{
	{"exits bounding n", guardedExits, 16000},
	{"exits bounding new locals", localExits, 16000},
	{"straight line", straightWrites, 4000},
}

// closedLocals returns localExits with each local also read by a closure
// passed to call after its exit, whose body keeps the facts about it, the
// local being immutable (see keepsInClosure). A program holding it needs
// call (see withCall).
func closedLocals(n int) []string {
	lines := provenJ()
	for k := range n {
		v := "v" + strconv.Itoa(k)
		lines = append(lines, v+" := i", "if "+v+" < 0 || "+v+" >= len(a) {", "return -1", "}",
			"call(func() { if a["+v+"] > 0 { report() } })", "t +%= a["+v+"]")
	}
	return append(lines, "return t")
}

// withCall returns the program inF makes of body, with call, which calls
// the closure it is passed, after it.
func withCall(body ...string) string {
	return inF(body...) + "\nfunc call(scope body func()) {\n    body()\n}\n"
}

// loopedWrites returns straightWrites with each write to j in a loop, so
// that every exit's fact keeps endings in loops of its region.
func loopedWrites(n int) []string {
	lines := provenJ()
	for range n {
		lines = append(lines, "for range 2 {", "j = i", "}", "if j < 0 || j >= len(a) {", "return -1", "}", "t +%= a[j]")
	}
	return append(lines, "return t")
}

// leavingWrites returns straightWrites with each exit followed by an if
// that writes j and leaves, which ends none of the facts after it.
func leavingWrites(n int) []string {
	lines := provenJ()
	for k := range n {
		lines = append(lines, "j = i", "if j < 0 || j >= len(a) {", "return -1", "}",
			"if n == "+strconv.Itoa(k)+" {", "j = 9", "return -1", "}", "t +%= a[j]")
	}
	return append(lines, "return t")
}

// unprovenWrites returns straightWrites followed by a use of a[j + 1],
// which nothing proves, so the notes that explain it suppose the facts
// in force held (see supposing).
func unprovenWrites(n int) []string {
	lines := straightWrites(n)
	return append(lines[:len(lines)-1], "t +%= a[j + 1]", "return t")
}

// supposedShadows are programs in which a fact a guard gives, j == 3, is
// followed by a stored value's fact saying the same, which a write to j
// then drops with it, before a place nothing proves: the notes on it
// suppose the guard's fact held, its write aside (see supposing and
// provenWithout), and that fact must then be read though the stored
// value's shadows it.
var supposedShadows = map[string]string{
	"supposed index": inF("mut j := i", "mut t int64 = 0", "switch j {", "case 3:",
		"t +%= a[j]", "j = 3", "j = n", "t +%= a[j]", "}", "return t"),
	"supposed unreachable": inF("mut j := i", "mut t int64 = 0", "switch j {", "case 3:",
		"t +%= a[j]", "j = 3", "j = n", "if j != 3 {", "unreachable", "}", "}", "return t"),
}

// relatedExits is a program whose exits bound j by k + 1 by two amounts,
// one after the other, each a difference bound about j with the same
// condition on k + 1: neither may be taken as saying what the other does.
var relatedExits = inF("j := i", "k := n % 100", "mut t int64 = 0", "if j < 0 || j >= len(a) {", "return -1", "}",
	"t +%= a[j]", "if j > k + 1 {", "return -1", "}", "if j >= k + 1 {", "return -1", "}", "t +%= a[j]", "return t")

// TestFactIndexMatchesScanOnPrograms checks every case of
// examples/invalid.txtar and examples/warnings.txtar, and the fact,
// region and reach shapes, and shapes with closures, loops, leaving
// writes and an unproven use, each at a few sizes, with every query of
// the facts in force reading their index and checking it, and what the
// query of the facts not shadowed reads, against a scan of every fact
// (see typeck.CheckFactIndex), which panics on a difference. Whether a
// program is accepted does not matter here, only that the index and a
// scan agree.
func TestFactIndexMatchesScanOnPrograms(t *testing.T) {
	defer typeck.CheckFactIndex()()
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
	shapes := append(factShapes[:len(factShapes):len(factShapes)], regionShapes...)
	shapes = append(shapes, reachShapes...)
	shapes = append(shapes,
		reachShape{"closed locals", closedLocals, 0},
		reachShape{"looped writes", loopedWrites, 0},
		reachShape{"leaving writes", leavingWrites, 0},
		reachShape{"unproven writes", unprovenWrites, 0},
	)
	for _, shape := range shapes {
		for _, n := range []int{1, 3, 9, 17, 40} {
			check(t, fmt.Sprintf("%s/%d", shape.name, n), []byte(withCall(shape.lines(n)...)))
		}
	}
}

// TestFactShapesAreChecked checks that the shapes the oracle test adds
// check as expected: all accepted but the unproven use, so the oracle
// test reads facts that prove things, and an error's notes, and that
// each of supposedShadows has its one error explained by notes, and
// relatedExits none.
func TestFactShapesAreChecked(t *testing.T) {
	for _, shape := range []reachShape{
		{"closed locals", closedLocals, 0},
		{"looped writes", loopedWrites, 0},
		{"leaving writes", leavingWrites, 0},
	} {
		diags, _ := checkWork(t, withCall(shape.lines(40)...))
		if len(diags) > 0 {
			t.Errorf("%s: got %d diagnostics, first %v", shape.name, len(diags), diags[0])
		}
	}
	srcs := map[string]string{"unproven writes": inF(unprovenWrites(40)...)}
	maps.Copy(srcs, supposedShadows)
	if diags, _ := checkWork(t, relatedExits); len(diags) > 0 {
		t.Errorf("related exits: got %v", diags)
	}
	for name, src := range srcs {
		diags, _ := checkWork(t, src)
		if len(diags) != 1 || len(diags[0].Related) == 0 {
			t.Errorf("%s: got %v, want one error with notes", name, diags)
		}
	}
}

// TestFactQueriesGrowLinearly checks that blocks of thousands of early
// exits, each putting facts in force for the rest of the block, are
// checked reading facts linearly in the exits (see checkGrowth): each
// query reads only the facts about its subject, from an index kept in
// step with the facts in force rather than built again after each exit,
// and, of facts alike that the code between the exits writes, only the
// newest, which holds wherever the older ones do. Building the index
// after each exit, or reading every fact about j, would make the facts
// read quadratic.
func TestFactQueriesGrowLinearly(t *testing.T) {
	for _, shape := range factShapes {
		t.Run(shape.name, func(t *testing.T) {
			diags, small := checkWork(t, inF(shape.lines(500)...))
			if len(diags) > 0 {
				t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
			}
			diags, grown := checkWork(t, inF(shape.lines(2000)...))
			if len(diags) > 0 {
				t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
			}
			checkGrowth(t, "facts read", small.Scanned, grown.Scanned)
		})
	}
}

// BenchmarkFactsInForce times checking each fact shape at 1000, 4000 and
// 16000 exits, up to the most it is measured at.
func BenchmarkFactsInForce(b *testing.B) {
	for _, shape := range factShapes {
		for _, n := range []int{1000, 4000, 16000} {
			if n > shape.most {
				continue
			}
			src := inF(shape.lines(n)...)
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
