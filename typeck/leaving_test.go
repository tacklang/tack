package typeck_test

import (
	"strconv"
	"testing"
)

// TestLongLeavingChainsCheckQuickly checks that a region holding thousands
// of branches or cases that each write the index and leave, after an
// early exit that proves it, then indexes past them all, is checked in
// steps linear in its branches (see checkGrowth): the nodes whose writes
// are indexed, and the statements looked at to find which branches leave
// and what breaks and continues leave them, and the branches each write
// is judged against. An else if is the last statement of the branch
// before it, so finding afresh at every link whether a branch leaves, or
// copying the branches holding each write, would make them quadratic. So
// would judging each write by walking every branch around it afresh, at
// each index: a branch ending in continue to a loop in the region does
// not settle it, so the walk goes on out to the branch that returns
// around the loop, past every link of a chain, or every level of a
// nesting, of such branches. Remembering the answer within one region's
// index would not be enough either: every guard proving the index builds
// an index of its own, so with uses growing with the chain, each use
// would still walk the whole chain once a guard.
//
// Nesting is bounded by the parser's limit, and each nested if's branch
// is a region of its own, whose writes are indexed apart from the
// region around it, so for nested loops only the branches judged are
// bounded: the nodes walked, and with them the statements looked at to
// find branches that leave, grow with the square of the depth. A switch
// whose cases end in return needs no statement looked at past each
// case's last, so it has no such steps to count.
func TestLongLeavingChainsCheckQuickly(t *testing.T) {
	for _, tc := range []struct {
		lines   func(n int) []string
		name    string
		n       int
		nested  bool
		looksAt bool
	}{
		{leavingElseIfs, "else if chain", 1000, false, true},
		{leavingCases, "switch", 1000, false, false},
		{continuingElseIfs, "else if chain continuing a loop", 1000, false, true},
		{continuingLevels, "nested loops continued", 100, true, false},
		{guardedUses, "uses growing with a continuing chain", 400, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := tc.n
			diags, small := checkWork(t, inF(tc.lines(n)...))
			if len(diags) > 0 {
				t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
			}
			diags, grown := checkWork(t, inF(tc.lines(4*n)...))
			if len(diags) > 0 {
				t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
			}
			checkGrowth(t, "branches writes are judged against", small.Judged, grown.Judged)
			if !tc.nested {
				checkGrowth(t, "nodes walked for writes", small.Walked, grown.Walked)
			}
			if tc.looksAt {
				checkGrowth(t, "steps finding branches that leave", small.Leaving, grown.Leaving)
			}
		})
	}
}

// leavingElseIfs returns the lines of an early exit proving j an index of
// a, then an if and n-1 else ifs on n, each assigning j and returning,
// then a[j].
func leavingElseIfs(n int) []string {
	lines := []string{"mut j := i", "if j < 0 || j >= len(a) {", "return -1", "}"}
	for k := range n {
		head := "} else if n == " + strconv.Itoa(k) + " {"
		if k == 0 {
			head = "if n == 0 {"
		}
		lines = append(lines, head, "j = "+strconv.Itoa(k), "return "+strconv.Itoa(k))
	}
	return append(lines, "}", "return a[j]")
}

// leavingCases returns the lines of an early exit proving j an index of
// a, then a switch on n with n cases, each assigning j and returning, then
// a[j].
func leavingCases(n int) []string {
	lines := []string{"mut j := i", "if j < 0 || j >= len(a) {", "return -1", "}", "switch n {"}
	for k := range n {
		lines = append(
			lines,
			"case "+strconv.Itoa(k)+":",
			"j = "+strconv.Itoa(k),
			"return "+strconv.Itoa(k),
		)
	}
	return append(lines, "}", "return a[j]")
}

// continuingElseIfs returns the lines of an early exit proving j an index
// of a, then an if on n that returns, holding a loop whose body is an if
// and n-1 else ifs on n and an else, each assigning j and continuing the
// loop, so every branch around a link leaves too, then ten uses of a[j].
func continuingElseIfs(n int) []string {
	lines := []string{
		"mut j := i", "if j < 0 || j >= len(a) {", "return -1", "}", "mut t int64 = 0",
		"if n == 0 {", "for range 3 {",
	}
	for k := range n {
		head := "} else if n == " + strconv.Itoa(k+1) + " {"
		if k == 0 {
			head = "if n == 1 {"
		}
		lines = append(lines, head, "j = "+strconv.Itoa(k), "continue")
	}
	lines = append(lines, "} else {", "j = 0", "continue", "}", "}", "return 0", "}")
	for range 10 {
		lines = append(lines, "t += a[j]")
	}
	return append(lines, "return t")
}

// continuingLevels returns the lines of an early exit proving j an index
// of a, then an if on n that returns, holding n loops, each in the branch
// of an if in the one around it, which assigns j and continues that loop
// once the loop inside it ends, then ten uses of a[j].
func continuingLevels(n int) []string {
	lines := []string{
		"mut j := i", "if j < 0 || j >= len(a) {", "return -1", "}", "mut t int64 = 0",
		"if n == 0 {",
	}
	for k := range n {
		lines = append(
			lines,
			"for range 3 {",
			"if n == "+strconv.Itoa(k+1)+" {",
			"j = "+strconv.Itoa(k),
		)
	}
	for range n {
		lines = append(lines, "continue", "}", "}")
	}
	lines = append(lines, "return 0", "}")
	for range 10 {
		lines = append(lines, "t += a[j]")
	}
	return append(lines, "return t")
}

// guardedUses returns the lines of three early exits, each proving j an
// index of a, then an if on n that returns, holding a loop whose body is
// an if and n-1 else ifs on n, each continuing the loop, and an else that
// assigns j and continues it, then n/10 uses of a[j].
func guardedUses(n int) []string {
	lines := []string{"mut j := i", "mut t int64 = 0"}
	for range 3 {
		lines = append(lines, "if j < 0 || j >= len(a) {", "return -1", "}")
	}
	lines = append(lines, "if n == 0 {", "for range 3 {")
	for k := range n {
		head := "} else if n == " + strconv.Itoa(k+1) + " {"
		if k == 0 {
			head = "if n == 1 {"
		}
		lines = append(lines, head, "report()", "continue")
	}
	lines = append(lines, "} else {", "j = 0", "continue", "}", "}", "return 0", "}")
	for range n / 10 {
		lines = append(lines, "t += a[j]")
	}
	return append(lines, "return t")
}
