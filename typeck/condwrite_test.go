package typeck_test

import "testing"

// condWritePrelude declares the functions the programs of the tests below
// call: each changes what its mut argument holds, so a fact about that
// argument learned before the call no longer holds after it.
const condWritePrelude = `package main

func shrink(mut l list[int64]) bool {
    l = make(list[int64], c_allocator)
    return true
}

func zero(mut d int64) bool {
    d = 0
    return true
}

func neg(mut i int64) bool {
    i = -5
    return true
}

func five(mut i int64) int64 {
    i = 0
    return 5
}

func short(mut s string) bool {
    s = "a"
    return true
}

func keep(b bool) bool {
    return b
}

func use(v int64) {
}

func main() {
}

`

// condWriteProgram returns condWritePrelude followed by a function g
// taking params whose body is lines.
func condWriteProgram(params string, lines ...string) string {
	body := ""
	for _, l := range lines {
		body += "    " + l + "\n"
	}
	return condWritePrelude + "func g(" + params + ") {\n" + body + "}\n"
}

const (
	condIndex5  = "index 5 may be out of range; check len(l) > 5 first, or use v, ok := ...[5]"
	condIndex0  = "index 0 may be out of range; check len(l) > 0 first, or use v, ok := ...[0]"
	condStr5    = "index 5 may be out of range; check len(s) > 5 first, or use v, ok := ...[5]"
	condIndexI  = "index i may be out of range; check 0 <= i && i < 3 first, or use v, ok := ...[i]"
	condDivisor = "divisor d may be zero; check d != 0 first, or use q, ok := ... / d"
)

// TestWritesAfterAFactInItsConditionEndIt checks that a fact a condition
// proves ends at a write that runs after it and before the code it
// guards: later in the same condition, in a branch that falls through to
// the code after an early exit, in a later case's value, in any value of
// a tag switch tried since the tag was evaluated, or in a loop's
// condition on the next pass. Each program would compile if the fact
// still held, and gives exactly the one missing proof.
func TestWritesAfterAFactInItsConditionEndIt(t *testing.T) {
	for _, tt := range []struct{ name, src, msg string }{
		{"right of && in an if", condWriteProgram("mut l list[int64]",
			"if len(l) > 5 && shrink(mut l) {", "    use(l[5])", "}"), condIndex5},
		{"right of && within the condition", condWriteProgram("mut l list[int64]",
			"if len(l) > 0 && shrink(mut l) && l[0] == 0 {", "}"), condIndex0},
		{"right of && in parentheses", condWriteProgram("mut l list[int64]",
			"if len(l) > 5 && (shrink(mut l) && l[5] == 5) {", "}"), condIndex5},
		{"right of || before an early exit", condWriteProgram("mut l list[int64]",
			"if len(l) <= 5 || shrink(mut l) {", "    return", "}", "use(l[5])"), condIndex5},
		{"right of || in the else", condWriteProgram("mut l list[int64]",
			"if len(l) <= 5 || shrink(mut l) {", "} else {", "    use(l[5])", "}"), condIndex5},
		{"nested in a call's argument", condWriteProgram("mut l list[int64]",
			"if len(l) > 5 && keep(shrink(mut l)) {", "    use(l[5])", "}"), condIndex5},
		{"string", condWriteProgram("mut s string",
			"if len(s) > 5 && short(mut s) {", "    use(int64(s[5]))", "}"), condStr5},
		{"divisor", condWriteProgram("mut d int64",
			"if d != 0 && zero(mut d) {", "    use(10 / d)", "}"), condDivisor},
		{"loop condition", condWriteProgram("mut l list[int64]",
			"for len(l) > 5 && shrink(mut l) {", "    use(l[5])", "}"), condIndex5},
		{"counting loop condition after the bound", condWriteProgram("a [3]int64",
			"for mut i := 0; i < 3 && neg(mut i); i++ {", "    use(a[i])", "}"), condIndexI},
		{"counting loop condition before the bound", condWriteProgram("a [3]int64",
			"for mut i := 0; neg(mut i) && i < 3; i++ {", "    use(a[i])", "}"), condIndexI},
		{"then branch falling through past an early exit", condWriteProgram("mut l list[int64]",
			"if len(l) > 5 {", "    shrink(mut l)", "} else {", "    return", "}", "use(l[5])"), condIndex5},
		{"else branch falling through past an early exit", condWriteProgram("mut l list[int64]",
			"if len(l) <= 5 {", "    return", "} else {", "    shrink(mut l)", "}", "use(l[5])"), condIndex5},
		{"later case's value", condWriteProgram("mut l list[int64]",
			"switch {", "case len(l) <= 5:", "    return", "case shrink(mut l):", "    use(l[5])", "}"), condIndex5},
		{"later value of the same case", condWriteProgram("mut l list[int64]",
			"switch {", "case len(l) <= 5, shrink(mut l) && l[5] == 5:", "    return", "}"), condIndex5},
		{"later value of a case that exits", condWriteProgram("mut l list[int64]",
			"switch {", "case len(l) <= 5, shrink(mut l):", "    return", "}", "use(l[5])"), condIndex5},
		{"later case's value past an early exit", condWriteProgram("mut l list[int64]",
			"switch {", "case len(l) <= 5:", "    return", "case shrink(mut l):", "}", "use(l[5])"), condIndex5},
		{"later case's body past an early exit", condWriteProgram("mut l list[int64]",
			"switch {", "case len(l) <= 5:", "    return", "default:", "    shrink(mut l)", "}", "use(l[5])"), condIndex5},
		{"later case's value of a tag switch", condWriteProgram("mut d int64",
			"switch d {", "case 0:", "    return", "case five(mut d):", "default:", "    use(10 / d)", "}"), condDivisor},
		{"earlier case's value of a tag switch", condWriteProgram("mut d int64",
			"switch d {", "case five(mut d):", "case 0:", "    return", "default:", "    use(10 / d)", "}"), condDivisor},
		{"own value of a tag switch's case", condWriteProgram("mut d int64",
			"switch d {", "case 1, five(mut d):", "    use(10 / d)", "}"), condDivisor},
	} {
		t.Run(tt.name, func(t *testing.T) {
			diags := check(t, tt.src)
			if len(diags) != 1 || diags[0].Message != tt.msg {
				t.Fatalf("got %v, want only %q", diags, tt.msg)
			}
		})
	}
}

// TestWritesBeforeAFactKeepIt checks that a write that runs before a
// condition proves its fact, or only on a path that never reaches the
// guarded code, leaves the fact in force.
func TestWritesBeforeAFactKeepIt(t *testing.T) {
	for _, tt := range []struct{ name, src string }{
		{"left of &&", condWriteProgram("mut l list[int64]",
			"if shrink(mut l) && len(l) > 5 && l[5] == 5 {", "}",
			"if shrink(mut l) && len(l) > 5 {", "    use(l[5])", "}")},
		{"left of ||", condWriteProgram("mut l list[int64]",
			"if !shrink(mut l) || len(l) <= 5 {", "    return", "}", "use(l[5])")},
		{"after the use in the condition", condWriteProgram("mut l list[int64]",
			"if len(l) > 5 && l[5] == 5 && shrink(mut l) {", "}")},
		{"loop condition before the bound", condWriteProgram("mut l list[int64]",
			"for shrink(mut l) && len(l) > 5 {", "    use(l[5])", "}")},
		{"in the branch that exits", condWriteProgram("mut l list[int64]",
			"if len(l) <= 5 {", "    shrink(mut l)", "    return", "}", "use(l[5])")},
		{"in the case that exits", condWriteProgram("mut l list[int64]",
			"switch {", "case len(l) <= 5:", "    shrink(mut l)", "    return", "}", "use(l[5])")},
		{"in an earlier value of a case", condWriteProgram("mut l list[int64]",
			"switch {", "case !shrink(mut l), len(l) <= 5:", "    return", "}", "use(l[5])")},
		{"in a later case of a tag switch", condWriteProgram("mut d int64",
			"switch d {", "case 0:", "    return", "case 1:", "    use(10 / d)", "case five(mut d):", "}")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if diags := check(t, tt.src); len(diags) > 0 {
				t.Fatalf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// TestWriteInAConditionIsNamed checks that a fact a later part of its own
// condition ends is explained by a note at the guard that gave it and one
// at the mut argument that ended it.
func TestWriteInAConditionIsNamed(t *testing.T) {
	src := condWriteProgram("mut l list[int64]",
		"if len(l) > 5 && shrink(mut l) {", "    use(l[5])", "}")
	// The prelude takes 37 lines, so g is on line 38 and its first line is the 39th.
	expectNotes(t, src, condIndex5,
		wantNote{"this guard proves len(l) >= 6", "len(l) > 5 && shrink(mut l)", 39, 8},
		wantNote{"but l is passed as mut to shrink here, so the proof ends", "mut l", 39, 29},
	)
}
