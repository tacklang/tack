package typeck_test

import (
	"strconv"
	"strings"
	"testing"
)

// nestedCounters returns a program of depth nested loops, each counting
// its own counter, declared just before it, up from 0 below 1, the
// innermost reading a[i<depth>] of a [1]int64: only the bound i >= 0 the
// innermost loop keeps proves the index.
func nestedCounters(depth int) string {
	var b strings.Builder
	b.WriteString("package main\n\nfunc f() int64 {\n    mut a [1]int64\n    a[0] = 1\n" +
		"    mut s int64 = 0\n")
	for k := 1; k <= depth; k++ {
		i := "i" + strconv.Itoa(k)
		b.WriteString("    mut " + i + " := 0\n    for " + i + " < 1 {\n")
	}
	b.WriteString("    s +%= a[i" + strconv.Itoa(depth) + "]\n")
	for k := depth; k >= 1; k-- {
		b.WriteString("    i" + strconv.Itoa(k) + "++\n    }\n")
	}
	b.WriteString("    return s\n}\n\nfunc main() {\n    print(\"\")\n}\n")
	return b.String()
}

// TestLoopInvariantNestingCap checks that a loop nested inside seven
// others that may keep bounds still keeps one, and that a loop inside
// eight keeps none, its index then unproven: the probe judges writes for
// at most eight loops at once.
func TestLoopInvariantNestingCap(t *testing.T) {
	if _, diags := checkTree(t, nestedCounters(8)); len(diags) > 0 {
		t.Errorf("eight loops deep, got %d errors, first %s", len(diags), diags[0].Message)
	}
	_, diags := checkTree(t, nestedCounters(9))
	if len(diags) != 1 || !strings.HasPrefix(diags[0].Message, "index i9 may be out of range") {
		t.Errorf("nine loops deep, got %v, want the index i9 unproven", diags)
	}
}

// int64Step returns loop-invariant-int64-wraps's program with step as the
// loop's write to i: i starts at the largest int64 less 1, and a[i -
// 9223372036854775806] is proven only while the loop keeps i at or above
// that start.
func int64Step(step string) string {
	return "package main\n\nfunc f() int64 {\n    a := [2]int64{1, 2}\n" +
		"    mut i := 9223372036854775806\n    mut sum int64 = 0\n" +
		"    for i < 9223372036854775807 {\n        sum +%= a[i - 9223372036854775806]\n" +
		"        " + step + "\n    }\n    return sum\n}\n\nfunc main() {\n    print(\"\")\n}\n"
}

// TestWrappingStepKeepsNoBound checks that the step is what decides
// loop-invariant-int64-wraps: a plain i += 1, which its condition proves,
// keeps i at or above its start and proves the index, and so does no
// write at all, while i +%= 2, which wraps, keeps nothing.
func TestWrappingStepKeepsNoBound(t *testing.T) {
	for _, step := range []string{"i += 1", "print(\"\")"} {
		if _, diags := checkTree(t, int64Step(step)); len(diags) != 0 {
			t.Errorf("with %s: %v", step, diags)
		}
	}
	if _, diags := checkTree(t, int64Step("i +%= 2")); len(diags) == 0 {
		t.Error("with i +%= 2: accepted")
	}
}
