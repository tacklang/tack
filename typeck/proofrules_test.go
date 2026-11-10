package typeck_test

import (
	"strings"
	"testing"
)

// proofPrelude adds to condWritePrelude what the programs of the tests
// below need beyond it: a call that drops what it is given, one that
// takes two values, and a struct whose mut method passes a list on as
// mut.
const proofPrelude = condWritePrelude + `func two(a int64, b int64) int64 {
    return a +% b
}

func shrunk(mut l list[int64]) int64 {
    l = make(list[int64], c_allocator)
    return 0
}

func zeroed(mut d int64) int64 {
    d = 0
    return 1
}

type Box struct {
    n int64
}

func (mut b Box) drain(mut l list[int64]) bool {
    l = make(list[int64], c_allocator)
    b.n = 0
    return true
}

func (mut b Box) bump() bool {
    b.n = b.n +% 1
    return true
}

`

// proofProgram returns proofPrelude followed by a function g taking params
// whose body is lines.
func proofProgram(params string, lines ...string) string {
	return proofPrelude + "func g(" + params + ") {\n    " + strings.Join(lines, "\n    ") + "\n}\n"
}

const (
	popEmpty   = "pop may find l empty; check len(l) > 0 first, or use x, ok := pop(mut l)"
	removeI    = "index i may be out of range; check 0 <= i && i < len(l) first, or use x, ok := remove(mut l, i)"
	divisorD   = "divisor d may be zero; check d != 0 first, or use q, ok := ... / d"
	index0     = "index 0 may be out of range; check len(l) > 0 first, or use v, ok := ...[0]"
	index1     = "index 1 may be out of range; check len(l) > 1 first, or use v, ok := ...[1]"
	indexIArr  = "index i may be out of range; check 0 <= i && i < 3 first, or use v, ok := ...[i]"
	indexIStr  = "index i may be out of range; check 0 <= i && i < len(s) first, or use v, ok := ...[i]"
	lenDivisor = "divisor may be zero; use q, ok := ... / ..."
	// The target of an assignment is told to check, and never offered the
	// checked form, which cannot be assigned.
	targetI    = "index i may be out of range; check 0 <= i && i < len(l) first"
	targetIArr = "index i may be out of range; check 0 <= i && i < 3 first"
)

// assignPrelude adds to proofPrelude functions of two results, one taking
// a mut argument, for the targets of an assignment of several values.
const assignPrelude = proofPrelude + `func pair(mut i int64) (int64, int64) {
    i = 0
    return 1, 2
}

func both() (int64, int64) {
    return 1, 2
}

`

// assignProgram returns assignPrelude followed by a function g taking
// params whose body is lines.
func assignProgram(params string, lines ...string) string {
	return assignPrelude + "func g(" + params + ") {\n    " + strings.Join(
		lines,
		"\n    ",
	) + "\n}\n"
}

// TestWritesThatReachTheUseEndAProof checks that a proof ends at a write
// that can run between the check and the use: before the use in the same
// pass, on a loop's back edge inside the proof's region, in an earlier
// part of the same expression, in code a defer runs after, in a switch's
// case value tried before a default written above it, and through a mut
// argument of a method, and that a successful append proves only what it
// added, only until a later write to its list. A proof an assignment's
// target needs is held to the whole statement, so a write in its value or
// another target ends it, a mut argument that ends where the statement
// does included. Each program would compile if the proof held.
func TestWritesThatReachTheUseEndAProof(t *testing.T) {
	for _, tt := range endedProofs {
		t.Run(tt.name, func(t *testing.T) {
			diags := check(t, tt.src)
			for _, d := range diags {
				if d.Message == tt.msg {
					return
				}
			}
			t.Fatalf("got %v, want %q", diags, tt.msg)
		})
	}
}

// endedProofs are programs where a write reaches a use and ends the proof
// it needs, each with the error that use must get.
var endedProofs = []struct{ name, src, msg string }{
	{
		"a second pop after the first",
		proofProgram(
			"mut l list[int64]",
			"if len(l) > 0 {",
			"    x := pop(mut l)",
			"    y := pop(mut l)",
			"    use(x +% y)",
			"}",
		),
		popEmpty,
	},
	{"a second pop in the same condition", proofProgram("mut l list[int64]",
		"if len(l) > 0 && pop(mut l) > 0 && pop(mut l) > 0 {", "}"), popEmpty},
	{"a second pop in the same expression", proofProgram("mut l list[int64]",
		"if len(l) > 0 {", "    use(pop(mut l) + pop(mut l))", "}"), popEmpty},
	{"a write in an earlier argument", proofProgram("mut l list[int64]",
		"if len(l) > 0 {", "    use(two(shrunk(mut l), pop(mut l)))", "}"), popEmpty},
	{"a write in an earlier operand", proofProgram("mut d int64",
		"if d != 0 {", "    use(zeroed(mut d) + 10 / d)", "}"), divisorD},
	{"a write nested in the assignment's own value", proofProgram("mut d int64",
		"if d != 0 {", "    d = zeroed(mut d) + 10 / d", "}"), divisorD},
	{"an index read after a pop in the same call", proofProgram("mut l list[int64]",
		"if len(l) > 0 {", "    use(two(pop(mut l), l[0]))", "}"), index0},
	{
		"a pop on the next pass of a loop in the branch",
		proofProgram(
			"mut l list[int64]",
			"if len(l) > 0 {",
			"    for range 2 {",
			"        x := pop(mut l)",
			"        use(x)",
			"    }",
			"}",
		),
		popEmpty,
	},
	{
		"a write later in a loop in the branch",
		proofProgram(
			"a [3]int64, mut i int64",
			"if 0 <= i && i < 3 {",
			"    for range 2 {",
			"        use(a[i])",
			"        i +%= 1",
			"    }",
			"}",
		),
		indexIArr,
	},
	{
		"a write later in a loop after an early exit",
		proofProgram(
			"mut l list[int64]",
			"if len(l) == 0 {",
			"    return",
			"}",
			"for {",
			"    x := pop(mut l)",
			"    use(x)",
			"}",
		),
		popEmpty,
	},
	{
		"a write later in a loop's pass, the guard outside it",
		proofProgram(
			"mut d int64",
			"if d == 0 {",
			"    return",
			"}",
			"for range 3 {",
			"    use(10 / d)",
			"    d = 0",
			"}",
		),
		divisorD,
	},
	{"a counting loop's start ended by a later write", proofProgram("a [3]int64",
		"for mut i := 0; i < 3; i++ {", "    use(a[i])", "    i = -5", "}"), indexIArr},
	{"a range over a string ended by a later write", proofProgram("mut s string",
		"for i := range s {", "    use(int64(s[i]))", "    s = \"a\"", "}"), indexIStr},
	{"a range over a string's length ended by a later write", proofProgram("mut s string",
		"for i := range len(s) {", "    use(int64(s[i]))", "    s = \"\"", "}"), indexIStr},
	{
		"a pop after a defer that pops",
		proofProgram(
			"mut l list[int64]",
			"if len(l) > 0 {",
			"    defer {",
			"        x := pop(mut l)",
			"        use(x)",
			"    }",
			"    pop(mut l)",
			"}",
		),
		popEmpty,
	},
	{
		"a second pop inside a defer",
		proofProgram(
			"mut l list[int64]",
			"if len(l) > 0 {",
			"    defer {",
			"        x := pop(mut l)",
			"        y := pop(mut l)",
			"        use(x +% y)",
			"    }",
			"}",
		),
		popEmpty,
	},
	{"a later defer that runs first", proofProgram("mut d int64",
		"if d != 0 {", "    defer { use(10 / d) }", "    defer { d = 0 }", "}"), divisorD},
	{
		"a defer in a loop, run each pass",
		proofProgram(
			"mut l list[int64]",
			"if len(l) > 0 {",
			"    for range 2 {",
			"        defer {",
			"            x := pop(mut l)",
			"            use(x)",
			"        }",
			"    }",
			"}",
		),
		popEmpty,
	},
	{
		"a default written before a case whose value writes",
		proofProgram(
			"mut d int64",
			"if d != 0 {",
			"    switch {",
			"    default:",
			"        use(10 / d)",
			"    case zero(mut d):",
			"    }",
			"}",
		),
		divisorD,
	},
	{
		"a default of a tag switch written before a value that writes",
		proofProgram(
			"mut d int64",
			"switch d {",
			"case 0:",
			"    return",
			"default:",
			"    use(10 / d)",
			"case five(mut d):",
			"}",
		),
		divisorD,
	},
	{
		"a case value that writes before the body",
		proofProgram(
			"mut l list[int64]",
			"if len(l) > 0 {",
			"    switch {",
			"    case shrink(mut l):",
			"        x := pop(mut l)",
			"        use(x)",
			"    }",
			"}",
		),
		popEmpty,
	},
	{
		"a method's mut argument before the pop",
		proofProgram(
			"mut l list[int64], mut b Box",
			"if len(l) > 0 {",
			"    if mut b.drain(mut l) {",
			"        x := pop(mut l)",
			"        use(x)",
			"    }",
			"}",
		),
		popEmpty,
	},
	{"a method's mut argument in the same condition", proofProgram("mut l list[int64], mut b Box",
		"if len(l) > 0 && mut b.drain(mut l) && l[0] == 0 {", "}"), index0},
	{
		"append proves only what it added",
		proofProgram(
			"mut l list[int64]",
			"if len(l) > 0 {",
			"    err := append(mut l, 1)",
			"    if err.code != 0 {",
			"        return",
			"    }",
			"    use(l[1])",
			"}",
		),
		index1,
	},
	{
		"a pop after a successful append",
		proofProgram(
			"mut l list[int64]",
			"err := append(mut l, 1)",
			"if err.code != 0 {",
			"    return",
			"}",
			"x := pop(mut l)",
			"use(x + l[0])",
		),
		index0,
	},
	{
		"a pop on the next pass, the append outside the loop",
		proofProgram(
			"mut l list[int64]",
			"err := append(mut l, 1)",
			"if err.code != 0 {",
			"    return",
			"}",
			"for range 2 {",
			"    x := pop(mut l)",
			"    use(x)",
			"}",
		),
		popEmpty,
	},
	{
		"a pop after a defer that reads the append's proof",
		proofProgram(
			"mut l list[int64]",
			"err := append(mut l, 1)",
			"if err.code != 0 {",
			"    return",
			"}",
			"defer { use(l[0]) }",
			"pop(mut l)",
		),
		index0,
	},
	{
		"an assignment between the append and its check",
		proofProgram(
			"mut l list[int64]",
			"err := append(mut l, 1)",
			"l = make(list[int64], c_allocator)",
			"if err.code != 0 {",
			"    return",
			"}",
			"use(l[0])",
		),
		index0,
	},
	{
		"an append whose error is mutable",
		proofProgram(
			"mut l list[int64]",
			"mut err := append(mut l, 1)",
			"if err.code != 0 {",
			"    return",
			"}",
			"use(l[0])",
		),
		index0,
	},
	{"an append to another list", proofProgram("mut l list[int64], mut k list[int64]",
		"err := append(mut k, 1)", "if err.code != 0 {", "    return", "}", "use(l[0])"), index0},
	{"an append's error not checked", proofProgram("mut l list[int64]",
		"err := append(mut l, 1)", "if err.code > 0 {", "    return", "}", "use(l[0])"), index0},
	{
		"a spread of a view",
		proofProgram(
			"mut l list[int64], v []int64",
			"err := append(mut l, v...)",
			"if err.code != 0 {",
			"    return",
			"}",
			"use(l[0])",
		),
		index0,
	},
	{"a reserve", proofProgram("mut l list[int64]",
		"err := reserve(mut l, 3)", "if err.code != 0 {", "    return", "}", "use(l[0])"), index0},
	{"a default written early, after an append", proofProgram("mut l list[int64]",
		"err := append(mut l, 1)", "if err.code != 0 {", "    return", "}",
		"switch {", "default:", "    use(l[0])", "case shrink(mut l):", "}"), index0},
	{"a length divisor after a pop", proofProgram("mut l list[int64]",
		"if len(l) > 0 {", "    x := pop(mut l)", "    use(10 / len(l) + x)", "}"), lenDivisor},
	{"a plain pop that is not a statement", proofProgram("mut l list[int64]",
		"use(pop(mut l))"), popEmpty},
	{"a plain remove not a statement", proofProgram("mut l list[int64], i int64",
		"use(remove(mut l, i))"), removeI},
	{
		"a mut argument that is the whole of the target's value",
		proofProgram("mut l list[int64], mut i int64",
			"if 0 <= i && i < len(l) {", "    l[i] = five(mut i)", "}"),
		targetI,
	},
	{"a mut argument ending a binary value", proofProgram("mut l list[int64], mut i int64",
		"if 0 <= i && i < len(l) {", "    l[i] = 1 + five(mut i)", "}"), targetI},
	{
		"a mut argument in a compound assignment's value",
		proofProgram("mut l list[int64], mut i int64",
			"if 0 <= i && i < len(l) {", "    l[i] += five(mut i)", "}"),
		targetI,
	},
	{"a mut argument assigning an element's field", proofProgram("mut l list[Box], mut i int64",
		"if 0 <= i && i < len(l) {", "    l[i].n = five(mut i)", "}"), targetI},
	{"a mut argument assigning an array's element", proofProgram("mut a [3]int64, mut i int64",
		"if 0 <= i && i < 3 {", "    a[i] = five(mut i)", "}"), targetIArr},
	{
		"a mut argument assigning a nested array's element",
		proofProgram("mut a [3][3]int64, mut i int64",
			"if 0 <= i && i < 3 {", "    a[1][i] = five(mut i)", "}"),
		targetIArr,
	},
	{"a mut argument through a function value", proofProgram("mut l list[int64], mut i int64",
		"f := five", "if 0 <= i && i < len(l) {", "    l[i] = f(mut i)", "}"), targetI},
	{
		"a mut argument in the value of several targets",
		assignProgram("mut l list[int64], mut i int64, mut x int64",
			"if 0 <= i && i < len(l) {", "    l[i], x = pair(mut i)", "}"),
		targetI,
	},
	{"an earlier target of several", assignProgram("mut l list[int64], mut i int64",
		"if 0 <= i && i < len(l) {", "    i, l[i] = both()", "}"), targetI},
	{"a later target of several", assignProgram("mut l list[int64], mut i int64",
		"if 0 <= i && i < len(l) {", "    l[i], i = both()", "}"), targetI},
}

// TestWritesAfterTheUseKeepAProof checks that a proof holds through a
// write that runs only after the use on every path: the mut argument of
// the call the proof is for, the target of the assignment, a write later
// in the same pass of a loop whose condition, or a guard in whose body,
// proves it again, a write after a defer's own use inside it, a write in
// another case of a switch, and a successful append's proof, and that
// pop and remove written as statements need no proof.
func TestWritesAfterTheUseKeepAProof(t *testing.T) {
	for _, tt := range []struct{ name, src string }{
		{"a pop after a length check", proofProgram("mut l list[int64]",
			"if len(l) > 0 {", "    x := pop(mut l)", "    use(x)", "}")},
		{"a remove after a range check", proofProgram("mut l list[int64], i int64",
			"if 0 <= i && i < len(l) {", "    x := remove(mut l, i)", "    use(x)", "}")},
		{"a divisor's own assignment", proofProgram("mut d int64",
			"if d != 0 {", "    d = 10 / d", "}")},
		{"a compound assignment's own divisor", proofProgram("mut d int64",
			"if d != 0 {", "    d /= d", "}")},
		{"a pop in its own condition", proofProgram("mut l list[int64]",
			"if len(l) > 0 && pop(mut l) > 0 {", "}")},
		{"an index read before a pop in the same call", proofProgram("mut l list[int64]",
			"if len(l) > 0 {", "    use(two(l[0], pop(mut l)))", "}")},
		{"a write later in a loop's pass", proofProgram("a [3]int64, mut i int64",
			"for 0 <= i && i < 3 {", "    use(a[i])", "    i++", "}")},
		{"a pop later in a loop's pass", proofProgram("mut l list[int64]",
			"for len(l) > 0 {", "    use(l[0])", "    x := pop(mut l)", "    use(x)", "}")},
		{"a write after a guard inside a loop", proofProgram("mut l list[int64], mut at int64",
			"for {", "    if at < 0 || at >= len(l) {", "        break", "    }", "    at = l[at]", "}")},
		{"a write in an inner loop after the use", proofProgram("a [3]int64, mut i int64",
			"if 0 <= i && i < 3 {", "    use(a[i])", "    for ; i < 2; i++ {", "    }", "}")},
		{"a write after the use inside a defer", proofProgram("mut d int64",
			"if d != 0 {", "    defer {", "        use(10 / d)", "        d = 0", "    }", "}")},
		{"a write in another case's body", proofProgram("mut l list[int64], c bool",
			"if len(l) > 0 {", "    switch {", "    case c:", "        x := pop(mut l)", "        use(x)", "    default:",
			"        shrink(mut l)", "    }", "}")},
		{"a default written last after a case value that writes", proofProgram("mut d int64",
			"if d != 0 {", "    switch {", "    case keep(true):", "    default:", "        use(10 / d)", "    }", "    d = 0", "}")},
		{"a method's mut argument after the use", proofProgram("mut l list[int64], mut b Box",
			"if len(l) > 0 {", "    x := pop(mut l)", "    if mut b.drain(mut l) {", "        use(x)", "    }", "}")},
		{"a method's own mut receiver", proofProgram("mut l list[int64], mut b Box",
			"if len(l) > 0 && mut b.bump() {", "    x := pop(mut l)", "    use(x)", "}")},
		{"a successful append", proofProgram("mut l list[int64]",
			"err := append(mut l, 1, 2)", "if err.code != 0 {", "    return", "}", "use(l[1] +% 10 / len(l))")},
		{"a successful append checked in a branch", proofProgram("mut l list[int64]",
			"err := append(mut l, 1)", "if err.code == 0 {", "    x := pop(mut l)", "    use(x)", "}")},
		{"a successful append of an array", proofProgram("mut l list[int64]",
			"a := [2]int64{1, 2}", "err := append(mut l, a...)", "if err.code != 0 {", "    return", "}", "use(l[1])")},
		{"a successful append in each pass", proofProgram("mut l list[int64]",
			"for range 3 {", "    err := append(mut l, 1)", "    if err.code != 0 {", "        return", "    }",
			"    x := pop(mut l)", "    use(x)", "}")},
		{"a write to the list before the append", proofProgram("mut l list[int64]",
			"shrink(mut l)", "err := append(mut l, 1)", "if err.code != 0 {", "    return", "}", "use(l[0])")},
		{"a length divisor after a length check", proofProgram("mut l list[int64]",
			"if len(l) > 0 {", "    use(10 / len(l))", "}")},
		{"pop and remove as statements", proofProgram("mut l list[int64], i int64",
			"pop(mut l)", "remove(mut l, i)", "remove(mut l, 7)")},
		{"pop and remove of owned elements as statements", proofProgram("mut l list[list[int64]], i int64",
			"pop(mut l)", "remove(mut l, i)")},
		{"a pop statement in a defer", proofProgram("mut l list[int64]",
			"defer { pop(mut l) }", "pop(mut l)")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if diags := check(t, tt.src); len(diags) > 0 {
				t.Fatalf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// TestAssignmentsKeepAProofOfTheirTarget checks that an assignment's
// target keeps its proof through every write in the statement that does
// not touch what the proof is about: a value's mut argument of another
// variable, a plain or compound assignment, a value computed into a
// variable before the index is proven, and a target of several beside
// one that writes nothing the index needs.
func TestAssignmentsKeepAProofOfTheirTarget(t *testing.T) {
	for _, tt := range []struct{ name, src string }{
		{"a value's mut argument of another variable", proofProgram("mut l list[int64], mut i int64, mut j int64",
			"if 0 <= i && i < len(l) {", "    l[i] = five(mut j)", "    l[i] +%= five(mut j)", "}")},
		{"a value computed before the target is proven", proofProgram("mut l list[int64], mut i int64",
			"v := five(mut i)", "if 0 <= i && i < len(l) {", "    l[i] = v", "}")},
		{"a target of several that writes no index", assignProgram("mut l list[int64], i int64, mut x int64",
			"if 0 <= i && i < len(l) {", "    l[i], x = both()", "}")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if diags := check(t, tt.src); len(diags) > 0 {
				t.Fatalf("got %v, want no diagnostics", diags)
			}
		})
	}
}
