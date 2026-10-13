package typeck_test

import (
	"slices"
	"strings"
	"testing"
)

// acceptedDefers must check cleanly: deferred statements of every kind,
// loops inside deferred blocks that break and continue on their own, and
// divisions and indexes proven by facts that hold wherever the deferred
// code can run.
var acceptedDefers = []struct {
	name string
	src  string
}{
	{"call", inF("defer { report() }", "return 0")},
	{"call with results discarded", inF("defer { pair() }", "return 0")},
	{"assignment", inF("mut x := 1", "defer { x = 2 }", "return x")},
	{"compound assignment", inF("mut x := 1", "defer { x *= n }", "return x")},
	{"increment", inF("mut x := 1", "defer { x-- }", "return x")},
	{
		"element and field",
		inF(
			"mut b := a",
			"mut p := P{x: 1}",
			"defer { b[0] = 2 }",
			"defer { p.x++ }",
			"return b[1] + p.x",
		),
	},
	{"multi-assign", inF("mut x := 1", "mut y := 2", "defer { x, y = pair() }", "return x + y")},
	{"empty block", inF("defer {", "}", "return 0")},
	{
		"every statement in a block",
		inF(
			"defer {",
			"    x := 1",
			"    mut y := x",
			"    if y > 0 {",
			"        y = 2",
			"    }",
			"    switch y {",
			"    case 1:",
			"        report()",
			"    }",
			"    for range y {",
			"        break",
			"    }",
			"    for {",
			"        if y > 3 {",
			"            break",
			"        }",
			"        y++",
			"        continue",
			"    }",
			"}",
			"return 0",
		),
	},
	{
		"loop in a block in a loop",
		inF(
			"for range 3 {",
			"    defer {",
			"        for range 2 {",
			"            continue",
			"        }",
			"    }",
			"}",
			"return 0",
		),
	},
	{
		"proven by an enclosing if",
		inF(
			"if d != 0 {",
			"    defer { report() }",
			"    defer { f(n / d, d, i, s, a) }",
			"}",
			"return 0",
		),
	},
	{
		"proven by an if before it",
		inF("if d == 0 {", "    return 0", "}", "defer { f(n / d, d, i, s, a) }", "return 1"),
	},
	{
		"proven by a range loop",
		inF(
			"for j := range 4 {",
			"    mut b := a",
			"    defer { b[j] = 1 }",
			"    b[0] = b[j]",
			"}",
			"return 0",
		),
	},
	{
		"proven inside the block",
		inF(
			"mut e := d",
			"defer {",
			"    if e != 0 {",
			"        f(n / e, d, i, s, a)",
			"    }",
			"}",
			"e = 0",
			"return e",
		),
	},
	{
		"known value of an immutable variable",
		inF("k := 4", "defer { f(n / k, k, i, s, a) }", "return 0"),
	},
	{
		"reads a mutable variable at exit",
		inF("mut x := 1", "defer { report() }", "defer { x = x + n }", "return x"),
	},
}

// TestAcceptsDefers checks well-formed defers check cleanly.
func TestAcceptsDefers(t *testing.T) {
	for _, tt := range acceptedDefers {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// deferInDefer is the message for a defer in deferred code.
const deferInDefer = "cannot defer inside a defer"

// divisorMayBeZero is the message for an unproven divisor d.
const divisorMayBeZero = "divisor d may be zero; check d != 0 first, or use q, ok := ... / d"

// rejectedDefers pair defers with every diagnostic each must produce, and
// where: control leaving deferred code, and facts the deferred code cannot
// rely on when it runs.
var rejectedDefers = []struct {
	name string
	src  string
	want []switchError
}{
	{
		"return in a block",
		inF("defer {", "    return 1", "}", "return 0"),
		[]switchError{{"cannot return from a defer", "return 1"}},
	},
	{
		"bare return in a nested block",
		inF("defer {", "    if n > 0 {", "        return", "    }", "}", "return 0"),
		[]switchError{{"cannot return from a defer", "return"}},
	},
	{
		"return in a loop in a block",
		inF("defer {", "    for {", "        return 1", "    }", "}", "return 0"),
		[]switchError{{"cannot return from a defer", "return 1"}},
	},
	{
		"return's values are checked for what is inside them",
		inF("defer {", "    return n / d", "}", "return 0"),
		[]switchError{{"cannot return from a defer", "return n / d"}, {divisorMayBeZero, "d"}},
	},
	{
		"break out of a defer",
		inF("for {", "    defer {", "        break", "    }", "}"),
		[]switchError{{"cannot break out of a defer", "break"}},
	},
	{
		"continue out of a defer",
		inF(
			"for range 2 {",
			"    defer {",
			"        if n > 0 {",
			"            continue",
			"        }",
			"    }",
			"}",
			"return 0",
		),
		[]switchError{{"cannot continue out of a defer", "continue"}},
	},
	{
		"break in a switch in a defer",
		inF(
			"for range 2 {",
			"    defer {",
			"        switch n {",
			"        case 1:",
			"            break",
			"        }",
			"    }",
			"}",
			"return 0",
		),
		[]switchError{{"cannot break out of a defer", "break"}},
	},
	{
		"defer in a deferred block",
		inF("defer {", "    defer {", "    }", "}", "return 0"),
		[]switchError{{deferInDefer, "defer"}},
	},
	{
		"defer deep in a deferred block",
		inF(
			"defer {",
			"    for {",
			"        if n > 0 {",
			"            switch n {",
			"            case 1:",
			"                defer {",
			"                    report()",
			"                }",
			"            }",
			"        }",
			"        break",
			"    }",
			"}",
			"return 0",
		),
		[]switchError{{deferInDefer, "defer"}},
	},
	{
		"every outermost defer in a deferred block",
		inF(
			"defer {",
			"    defer {",
			"        defer {",
			"        }",
			"    }",
			"    defer {",
			"    }",
			"}",
			"return 0",
		),
		[]switchError{{deferInDefer, "defer"}, {deferInDefer, "defer"}},
	},
	{
		"a rejected defer's block is still checked",
		inF(
			"defer {",
			"    for {",
			"        defer {",
			"            break",
			"        }",
			"        break",
			"    }",
			"}",
			"return 0",
		),
		[]switchError{{deferInDefer, "defer"}, {"cannot break out of a defer", "break"}},
	},
	{
		"break with no loop at all",
		inF("defer {", "    break", "}", "return 0"),
		[]switchError{{"break is not in a loop", "break"}},
	},
	{
		"a later defer changes the divisor",
		inF(
			"mut e := d",
			"if e != 0 {",
			"    defer { f(n / e, d, i, s, a) }",
			"    defer { e = 0 }",
			"}",
			"return 0",
		),
		[]switchError{{"divisor e may be zero; check e != 0 first, or use q, ok := ... / e", "e"}},
	},
	{
		"the rest of the block changes the divisor",
		inF(
			"mut e := d",
			"if e == 0 {",
			"    return 0",
			"}",
			"defer { f(n / e, d, i, s, a) }",
			"e = 0",
			"return 1",
		),
		[]switchError{{"divisor e may be zero; check e != 0 first, or use q, ok := ... / e", "e"}},
	},
	{
		"the defer itself changes the divisor",
		inF(
			"mut e := d",
			"if e != 0 {",
			"    defer {",
			"        report()",
			"        e = n / e",
			"    }",
			"}",
			"return 0",
		),
		[]switchError{{"divisor e may be zero; check e != 0 first, or use q, ok := ... / e", "e"}},
	},
	{
		"no fact at all",
		inF("defer { f(n / d, d, i, s, a) }", "return 0"),
		[]switchError{{divisorMayBeZero, "d"}},
	},
	{
		"wrong type in a deferred assignment",
		inF("mut x := 1", "defer { x = s }", "return x"),
		[]switchError{{"cannot use string as int64 in assignment to x", "s"}},
	},
	{
		"assignment to an immutable variable",
		inF("x := 1", "defer { x++ }", "return x"),
		[]switchError{{"cannot assign to immutable variable x", "x"}},
	},
}

// TestRejectsInvalidDefers checks each problem with a defer is reported
// once, at the right place.
func TestRejectsInvalidDefers(t *testing.T) {
	for _, tt := range rejectedDefers {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != len(tt.want) {
				t.Fatalf("got %v, want %v", diags, tt.want)
			}
			for i, d := range diags {
				if d.Message != tt.want[i].msg {
					t.Errorf("message %d = %q, want %q", i, d.Message, tt.want[i].msg)
				}
				if got := tt.src[d.Span.Start:d.Span.End]; got != tt.want[i].at {
					t.Errorf("diagnostic %d covers %q, want %q", i, got, tt.want[i].at)
				}
			}
		})
	}
}

// TestDeferIsNotATerminatingStatement checks a function ending in a defer
// that would return is still missing its return: a defer never ends one.
func TestDeferIsNotATerminatingStatement(t *testing.T) {
	_, diags := checkTree(t, inF("defer { report() }"))
	if len(diags) != 1 || diags[0].Message != "f must end in a return" {
		t.Errorf("got %v, want only %q", diags, "f must end in a return")
	}
}

// outermostDefers pair sources with the defers, by their order among every
// defer keyword in the source counting from 0, that must each be reported
// as a defer inside a defer, and with any other messages expected too.
var outermostDefers = []struct {
	name   string
	src    string
	defers []int
	others []string
}{
	{
		"three deep reports only the middle",
		inF("defer {", "    defer {", "        defer {", "        }", "    }", "}", "return 0"),
		[]int{1},
		nil,
	},
	{
		"siblings are each reported",
		inF("defer {", "    defer { report() }", "    defer { report() }", "}", "return 0"),
		[]int{1, 2},
		nil,
	},
	{
		"a chain in a loop in a defer reports one",
		inF(
			"defer {",
			"    for range 2 {",
			"        defer {",
			"            defer {",
			"                defer {",
			"                }",
			"            }",
			"        }",
			"    }",
			"}",
			"return 0",
		),
		[]int{1},
		nil,
	},
	{
		"other errors in a suppressed region are still reported",
		inF(
			"mut x := 1",
			"defer {",
			"    defer {",
			"        defer {",
			"            x = s",
			"        }",
			"    }",
			"}",
			"return x",
		),
		[]int{1},
		[]string{"cannot use string as int64 in assignment to x"},
	},
}

// TestReportsOnlyOutermostDeferInDefer checks a defer inside deferred code
// is reported, but no defer inside the reported one's block is.
func TestReportsOnlyOutermostDeferInDefer(t *testing.T) {
	for _, tt := range outermostDefers {
		t.Run(tt.name, func(t *testing.T) {
			var starts []int
			for off := 0; ; {
				i := strings.Index(tt.src[off:], "defer")
				if i < 0 {
					break
				}
				starts = append(starts, off+i)
				off += i + len("defer")
			}
			_, diags := checkTree(t, tt.src)
			var got []int
			var others []string
			for _, d := range diags {
				if d.Message != deferInDefer {
					others = append(others, d.Message)
					continue
				}
				got = append(got, slices.Index(starts, d.Span.Start))
			}
			if !slices.Equal(got, tt.defers) || !slices.Equal(others, tt.others) {
				t.Errorf(
					"got defers %v and %q, want %v and %q (%v)",
					got,
					others,
					tt.defers,
					tt.others,
					diags,
				)
			}
		})
	}
}
