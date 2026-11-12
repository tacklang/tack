package typeck_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/tacklang/tack/typeck"
)

// programShape is a program measured at a size: name, and src, which
// returns the program for n.
type programShape struct {
	name string
	src  func(n int) string
}

// methodWrites returns straightWrites with each write to j a call of a
// method passing it as mut, whose callee, and so the note on what ends a
// fact, the checker knows only once it has checked the call. A program
// holding it needs the method (see withMethod).
func methodWrites(n int) []string {
	lines := append(provenJ(), "p := P{x: 1}")
	for range n {
		lines = append(lines, "p.set(mut j)", "if j < 0 || j >= len(a) {", "return -1", "}", "t +%= a[j]")
	}
	return append(lines, "return t")
}

// withMethod returns the program inF makes of body, with P's method set,
// which assigns the variable it is passed, after it.
func withMethod(body ...string) string {
	return inF(body...) + "\nfunc (p P) set(mut v int64) {\n    v = p.x\n}\n"
}

// relatedWrites returns j proven, then n times an assignment to j and to
// k, an early exit proving j below k and k at most len(a), and a use of
// a[j]: each exit's relation of j to k keeps every write to either after
// it, so its endings are two runs, each the end of the writes to one of
// them.
func relatedWrites(n int) []string {
	lines := append(provenJ(), "mut k := n")
	for range n {
		lines = append(lines, "j = i", "k = n", "if j < 0 || k > len(a) || j >= k {", "return -1", "}", "t +%= a[j]")
	}
	return append(lines, "return t")
}

// unprovenMethodWrites returns methodWrites followed by a use of
// a[j + 1], which nothing proves, so the notes on it name the call that
// wrote j last.
func unprovenMethodWrites(n int) []string {
	lines := methodWrites(n)
	return append(lines[:len(lines)-1], "t +%= a[j + 1]", "return t")
}

// loopExits returns j proven, then n loops, each proving j again with an
// early exit, using a[j] and then writing j: the exit's fact in the loop
// and the one before the loop say the same, but the write in the loop
// ends the one before it from inside a loop of its region, and the one
// in it from no loop of its own, so neither holds wherever the other
// does.
func loopExits(n int) []string {
	lines := provenJ()
	for range n {
		lines = append(lines, "for range 2 {", "if j < 0 || j >= len(a) {", "return -1", "}", "t +%= a[j]", "j = i", "}")
	}
	return append(lines, "return t")
}

// deferredStraight returns straightWrites with a defer using a[j] last,
// checked with every write outside it reaching it, while every exit's
// fact is in force, so a query of the newest fact's endings there also
// asks which lies outside the deferred code (see placed).
func deferredStraight(n int) []string {
	lines := straightWrites(n)
	return append(lines[:len(lines)-1], "defer {", "report()", "_ = a[j]", "}", "return t")
}

// lateMethodWrites returns j proven, then n times an early exit that
// both bounds j and rules out 3, a call of a method writing j, and a
// place proven unreachable where j is 3: the value ruled out is kept
// apart for that proof, and its endings found only when the proof needs
// them, once the call has been checked and its callee is known, while
// the bound's are found as the exit puts it in force, before (see
// region.go). A program holding it needs the method (see withMethod).
func lateMethodWrites(n int) []string {
	lines := append(provenJ(), "p := P{x: 1}")
	for range n {
		lines = append(lines, "if j == 3 || j < 0 || j >= len(a) {", "return -1", "}", "t +%= a[j]",
			"p.set(mut j)", "if j == 3 {", "unreachable", "}")
	}
	return append(lines, "return t")
}

// sharedShapes are the programs whose early exits' facts share the
// endings of the rest of a block in another way than straight line code
// does: endings judged with a callee the checker finds as it checks each
// call.
var sharedShapes = []programShape{
	{"method writes", func(n int) string { return withMethod(methodWrites(n)...) }},
}

// sharedOracleShapes are sharedShapes, a program whose facts' endings are
// two runs each, one for each variable a relation names, a program with
// an error whose notes name what ended a shared fact, one whose endings
// of a value ruled out are found later than the exit's other facts',
// one whose facts alike share the endings of different blocks, and one
// whose uses are in deferred code, for the oracle tests to check. The
// relation's program is measured for how its checking grows with the
// other programs whose exits each prove a relation (see relationShapes).
var sharedOracleShapes = append(sharedShapes[:len(sharedShapes):len(sharedShapes)],
	programShape{"related writes", func(n int) string { return inF(relatedWrites(n)...) }},
	programShape{"unproven method writes", func(n int) string { return withMethod(unprovenMethodWrites(n)...) }},
	programShape{"late method writes", func(n int) string { return withMethod(lateMethodWrites(n)...) }},
	programShape{"exits in loops", func(n int) string { return inF(loopExits(n)...) }},
	programShape{"deferred straight line", func(n int) string { return inF(deferredStraight(n)...) }},
)

// checkSharedShapes calls check with every one of sharedOracleShapes at
// sizes on both sides of the count of endings a scan answers for.
func checkSharedShapes(t *testing.T, check func(t *testing.T, name string, src []byte)) {
	for _, shape := range sharedOracleShapes {
		for _, n := range []int{1, 3, 17, 40} {
			check(t, fmt.Sprintf("%s/%d", shape.name, n), []byte(shape.src(n)))
		}
	}
}

// TestSharedShapesAreChecked checks that the shapes sharing endings check
// as expected: the method's and the relation's accepted, so the facts
// they share prove things, and the unproven use one error explained by
// notes.
func TestSharedShapesAreChecked(t *testing.T) {
	for _, shape := range sharedOracleShapes[:2] {
		if diags, _ := checkWork(t, shape.src(40)); len(diags) > 0 {
			t.Errorf("%s: got %d diagnostics, first %v", shape.name, len(diags), diags[0])
		}
	}
	diags, _ := checkWork(t, withMethod(unprovenMethodWrites(40)...))
	if len(diags) != 1 || len(diags[0].Related) == 0 {
		t.Errorf("unproven method writes: got %v, want one error with notes", diags)
	}
}

// TestSharedEndingsGrowLinearly checks that each of sharedShapes, with
// thousands of early exits each keeping every later write, is checked in
// steps linear in its exits (see checkGrowth): the endings put in facts'
// endings, and the steps finding the writes that reach a use. Giving each
// exit's fact endings of its own, or indexing them for each, would make
// them quadratic. With the oracle on, every query scans the endings too,
// so the test is skipped.
func TestSharedEndingsGrowLinearly(t *testing.T) {
	if typeck.ReachOracle() {
		t.Skip("the reach oracle scans every fact's endings at every query")
	}
	for _, shape := range sharedShapes {
		t.Run(shape.name, func(t *testing.T) {
			diags, small := checkWork(t, shape.src(500))
			if len(diags) > 0 {
				t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
			}
			diags, grown := checkWork(t, shape.src(2000))
			if len(diags) > 0 {
				t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
			}
			checkGrowth(t, "endings put in facts' endings", small.Ended, grown.Ended)
			checkGrowth(t, "steps finding the writes that reach a use", small.Reached, grown.Reached)
			checkGrowth(t, "facts read", small.Scanned, grown.Scanned)
		})
	}
}

// BenchmarkSharedEndings times checking each of sharedShapes at 1000,
// 4000 and 16000 exits.
func BenchmarkSharedEndings(b *testing.B) {
	for _, shape := range sharedShapes {
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
