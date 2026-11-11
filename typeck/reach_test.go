package typeck_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/tacklang/tack/internal/invalid"
	"github.com/tacklang/tack/typeck"
)

// The shapes below each hold many writes to j in a fact's region and
// many uses of a[j] the fact proves, so whether each write reaches each
// use is asked writes times uses times. They are the programs the
// reach query is measured and bounded on (see BenchmarkWriteReach and
// TestManyWritesAndUsesCheckQuickly).

// reachShape is one such program, made with n writes and n uses, which
// BenchmarkWriteReach measures at sizes up to most.
type reachShape struct {
	name  string
	lines func(n int) []string
	most  int
}

// reachShapes are the shapes measured: a switch whose cases each write
// j and return, the same with each write in a loop in its case, the same
// switch inside a loop, an else if chain, a loop holding ifs that write
// and return, and straight-line code that writes j and proves it again
// before each use, each early exit's fact keeping every write to j after
// it, so it is measured only up to 4000.
var reachShapes = []reachShape{
	{"switch", switchWrites, 16000},
	{"switch writing in loops", switchLoopWrites, 16000},
	{"switch in a loop", loopedSwitchWrites, 16000},
	{"else if chain", elseIfWrites, 16000},
	{"loop of leaving ifs", loopIfWrites, 16000},
	{"straight line", straightWrites, 4000},
}

// provenJ returns the lines that start every shape: j, a copy of i, and
// an early exit proving it an index of a, and t, which the uses add to.
func provenJ() []string {
	return []string{"mut j := i", "mut t int64 = 0", "if j < 0 || j >= len(a) {", "return -1", "}"}
}

// uses returns n lines, each adding a[j] to t, then the return of t.
func uses(n int) []string {
	var lines []string
	for range n {
		lines = append(lines, "t +%= a[j]")
	}
	return append(lines, "return t")
}

// switchWrites returns j proven, a switch on n with n cases, each
// assigning j and returning, then n uses of a[j].
func switchWrites(n int) []string {
	lines := append(provenJ(), "switch n {")
	for k := range n {
		s := strconv.Itoa(k)
		lines = append(lines, "case "+s+":", "j = "+s, "return "+s)
	}
	return append(append(lines, "}"), uses(n)...)
}

// switchLoopWrites returns j proven, a switch on n with n cases, each
// assigning j in a loop and returning, then n uses of a[j].
func switchLoopWrites(n int) []string {
	lines := append(provenJ(), "switch n {")
	for k := range n {
		s := strconv.Itoa(k)
		lines = append(lines, "case "+s+":", "for range 2 {", "j = "+s, "}", "return "+s)
	}
	return append(append(lines, "}"), uses(n)...)
}

// loopedSwitchWrites returns j proven, a loop holding a switch on n with
// n cases, each assigning j and returning, then n uses of a[j] after the
// loop.
func loopedSwitchWrites(n int) []string {
	lines := append(provenJ(), "for range 2 {", "switch n {")
	for k := range n {
		s := strconv.Itoa(k)
		lines = append(lines, "case "+s+":", "j = "+s, "return "+s)
	}
	return append(append(lines, "}", "}"), uses(n)...)
}

// elseIfWrites returns j proven, an if and n-1 else ifs on n, each
// assigning j and returning, then n uses of a[j].
func elseIfWrites(n int) []string {
	lines := provenJ()
	for k := range n {
		s := strconv.Itoa(k)
		head := "} else if n == " + s + " {"
		if k == 0 {
			head = "if n == 0 {"
		}
		lines = append(lines, head, "j = "+s, "return "+s)
	}
	return append(append(lines, "}"), uses(n)...)
}

// loopIfWrites returns j proven, a loop whose body holds n ifs on n, each
// assigning j and returning, then n uses of a[j], all in the loop.
func loopIfWrites(n int) []string {
	lines := append(provenJ(), "for range 2 {")
	for k := range n {
		s := strconv.Itoa(k)
		lines = append(lines, "if n == "+s+" {", "j = "+s, "return "+s, "}")
	}
	for range n {
		lines = append(lines, "t +%= a[j]")
	}
	return append(lines, "}", "return t")
}

// straightWrites returns j proven, then n times an assignment of i to j,
// an early exit proving j again, and a use of a[j].
func straightWrites(n int) []string {
	lines := provenJ()
	for range n {
		lines = append(lines, "j = i", "if j < 0 || j >= len(a) {", "return -1", "}", "t +%= a[j]")
	}
	return append(lines, "return t")
}

// BenchmarkWriteReach times checking each shape at 1000, 4000 and 16000
// writes and uses, up to the most it is measured at.
func BenchmarkWriteReach(b *testing.B) {
	for _, shape := range reachShapes {
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

// TestManyWritesAndUsesCheckQuickly checks that each shape but straight
// line code, with thousands of writes to j in leaving branches or cases
// and as many uses of a[j] after them, is checked in steps linear in its
// writes and uses (see checkGrowth): the endings asked about, the steps
// of the reach index built over them, and the keeps they are judged
// against. Asking whether every write reaches every use, as a scan of a
// fact's endings at each use does, would make them quadratic. Straight
// line code proves j again before each use, and each early exit's fact
// keeps every write to j after it and is asked about at every use after
// it (see docs/known-bugs.md), so only the nodes walked for its writes
// are bounded (see TestRegionsShareTheRestOfABlock). With
// the oracle on, every query scans the endings too, so the test is
// skipped.
func TestManyWritesAndUsesCheckQuickly(t *testing.T) {
	if typeck.ReachOracle() {
		t.Skip("the reach oracle scans every fact's endings at every query")
	}
	for _, shape := range reachShapes {
		if shape.name == "straight line" {
			continue
		}
		t.Run(shape.name, func(t *testing.T) {
			diags, small := checkWork(t, inF(shape.lines(500)...))
			if len(diags) > 0 {
				t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
			}
			diags, grown := checkWork(t, inF(shape.lines(2000)...))
			if len(diags) > 0 {
				t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
			}
			checkGrowth(t, "steps finding the writes that reach a use", small.Reached, grown.Reached)
			checkGrowth(t, "branches writes are judged against", small.Judged, grown.Judged)
		})
	}
}

// deferredUses returns j proven, a switch on n with n cases, each
// assigning j and returning, then a defer holding n uses of a[j], each of
// which every write outside the defer reaches.
func deferredUses(n int) []string {
	lines := append(provenJ(), "switch n {")
	for k := range n {
		s := strconv.Itoa(k)
		lines = append(lines, "case "+s+":", "j = "+s, "return "+s)
	}
	lines = append(lines, "}", "defer {")
	for range n {
		lines = append(lines, "report()", "_ = a[j]")
	}
	return append(lines, "}", "return t")
}

// lateDefaultUses returns j proven, then a switch on n whose default,
// written first, holds n uses of a[j], followed by n cases, each assigning
// j and returning, every one of which reaches the default's uses.
func lateDefaultUses(n int) []string {
	lines := append(provenJ(), "switch n {", "default:")
	lines = append(lines, uses(n)...)
	for k := range n {
		s := strconv.Itoa(k)
		lines = append(lines, "case "+s+":", "j = "+s, "return "+s)
	}
	return append(lines, "}", "return t")
}

// storedUses returns j proven, a switch on n with n cases, each assigning
// j and returning, then n assignments to b[j], whose targets are checked
// with the whole assignment's writes reaching them, the last one
// assigning j too.
func storedUses(n int) []string {
	lines := append(provenJ(), "mut b := a", "switch n {")
	for k := range n {
		s := strconv.Itoa(k)
		lines = append(lines, "case "+s+":", "j = "+s, "return "+s)
	}
	lines = append(lines, "}")
	for range n {
		lines = append(lines, "b[j] = t")
	}
	return append(lines, "b[j], j = 1, 2", "return b[0]")
}

// TestReachIndexMatchesScanOnPrograms checks every case of
// examples/invalid.txtar and examples/warnings.txtar, every shape, and
// shapes whose uses are in deferred code, a late default and an
// assignment's targets, each at sizes on both sides of the count of
// endings a scan answers for, with every query of a fact's endings read
// through their index and checked against a scan of them (see
// typeck.CheckReachIndex), which panics on a difference. Whether a
// program is accepted does not matter here, only that both find the
// same write.
func TestReachIndexMatchesScanOnPrograms(t *testing.T) {
	defer typeck.CheckReachIndex()()
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
	shapes := append(reachShapes[:len(reachShapes):len(reachShapes)],
		reachShape{"deferred uses", deferredUses, 0},
		reachShape{"late default uses", lateDefaultUses, 0},
		reachShape{"stored uses", storedUses, 0},
	)
	for _, shape := range shapes {
		for _, n := range []int{1, 3, 17, 40} {
			check(t, fmt.Sprintf("%s/%d", shape.name, n), []byte(inF(shape.lines(n)...)))
		}
	}
}
