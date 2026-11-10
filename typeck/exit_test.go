package typeck_test

import (
	"strings"
	"testing"
)

// noTail is src, made by inF2, without the return 0 inF2 ends f in, so
// that what ends f is the last line given.
func noTail(src string) string {
	return strings.Replace(src, "    return 0\n}\n\nfunc main", "}\n\nfunc main", 1)
}

// TestExitLeaves checks that a branch, a block or a case ending in a call
// to exit leaves, as one ending in return does: an early exit proves what
// got past it, and a function with a result may end in exit, alone or in
// an if or a switch whose every branch leaves. A dividend is an int32,
// never the smallest int64, so only the divisor needs the exit's proof.
func TestExitLeaves(t *testing.T) {
	for _, src := range []string{
		inF2("a int32, b int64", "if b == 0 { exit(1) }", "return a / b"),
		inF2("a int32, b int64", "if b != 0 {", "} else {", "    exit(1)", "}", "return a / b"),
		inF2("a int32, b int64", "switch {", "case b == 0:", "    exit(1)", "}", "return a / b"),
		noTail(inF2("a int64", "exit(1)")),
		noTail(inF2("a int64", "if a > 0 {", "    return a", "} else {", "    exit(1)", "}")),
		noTail(inF2("a int64", "switch a {", "case 0:", "    exit(1)", "default:", "    return a", "}")),
		inF2("a [3]int64, i int64", "if i < 0 || i >= 3 {", "    exit(1)", "}", "return a[i]"),
	} {
		if diags := check(t, src); len(diags) != 0 {
			t.Errorf("%q: got %v, want none", src, diags)
		}
	}
}

// TestOnlyExitItselfLeaves checks that what is not a call to exit, ending
// a branch, leaves nothing: a call to a function that exits, by name or
// through a value, and a defer whose code ends in exit, which runs only
// when its block ends.
func TestOnlyExitItselfLeaves(t *testing.T) {
	die := "\n\nfunc die() {\n    exit(1)\n}\n"
	for _, src := range []string{
		inF2("a int64, b int64", "if b == 0 { die() }", "return a / b") + die,
		inF2("a int64, b int64", "g := die", "if b == 0 { g() }", "return a / b") + die,
		inF2("a int64, b int64", "if b == 0 {", "    defer {", "        exit(1)", "    }", "}", "return a / b"),
		noTail(inF2("a int64", "die()")) + die,
	} {
		if diags := check(t, src); len(diags) != 1 {
			t.Errorf("%q: got %v, want one error", src, diags)
		}
	}
}
