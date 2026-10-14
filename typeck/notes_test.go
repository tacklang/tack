package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/diag"
)

// wantNote is a note expected on an error: its message, and the source its
// span covers, which starts at line and col, both 1-based.
type wantNote struct {
	msg       string
	text      string
	line, col int
}

// offsetOf returns the byte offset of line and col, both 1-based, in src.
func offsetOf(t *testing.T, src string, line, col int) int {
	t.Helper()
	off := 0
	for range line - 1 {
		next := strings.IndexByte(src[off:], '\n')
		if next < 0 {
			t.Fatalf("source has no line %d", line)
		}
		off += next + 1
	}
	return off + col - 1
}

// expectNotes checks that src gives an error whose message is msg, and that
// its notes are exactly notes, in order, each covering the source it says.
func expectNotes(t *testing.T, src, msg string, notes ...wantNote) {
	t.Helper()
	diags := check(t, src)
	var found *diag.Diagnostic
	for i := range diags {
		if diags[i].Message == msg {
			found = &diags[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no error %q; got %v", msg, diags)
	}
	if len(found.Related) != len(notes) {
		t.Fatalf("%d notes, want %d: %v", len(found.Related), len(notes), found.Related)
	}
	for i, n := range found.Related {
		checkNote(t, src, i, n, notes[i])
	}
}

// checkNote checks that n, the i-th note on an error in src, is w.
func checkNote(t *testing.T, src string, i int, n diag.Diagnostic, w wantNote) {
	t.Helper()
	if n.Severity != diag.Note {
		t.Errorf("note %d has severity %v", i, n.Severity)
	}
	if n.Message != w.msg {
		t.Errorf("note %d = %q, want %q", i, n.Message, w.msg)
	}
	start := offsetOf(t, src, w.line, w.col)
	if n.Span.Start != start || n.Span.End != start+len(w.text) {
		t.Errorf("note %d covers %q at %d, want %q at %d",
			i, src[n.Span.Start:n.Span.End], n.Span.Start, w.text, start)
	}
}

// program joins lines into a source file, one per line, ending in a
// newline. Line 1 is the first.
func program(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

const (
	divD       = "divisor d may be zero; check d != 0 first, or use q, ok := ... / d"
	divB       = "divisor b may be zero; check b != 0 first, or use q, ok := ... / b"
	noteIndexI = "index i may be out of range; check 0 <= i && i < 3 first, or use v, ok := ...[i]"
	strI       = "index i may be out of range; check 0 <= i && i < len(s) first, or use v, ok := ...[i]"
)

// assignedHere is the note that points at the assignment that ended a
// proof of the variable name.
func assignedHere(name string, line, col int) wantNote {
	return wantNote{"but " + name + " is assigned here, so the proof ends", name, line, col}
}

// noteCase is a program, the error it gives, and the notes on that error.
type noteCase struct {
	name  string
	src   string
	msg   string
	notes []wantNote
}

// runNoteCases checks each of cases in a subtest.
func runNoteCases(t *testing.T, cases []noteCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			expectNotes(t, tt.src, tt.msg, tt.notes...)
		})
	}
}

// TestDroppedFactNotes checks that a fact a region drops, because the
// region assigns its variable, is explained by a note at the guard that
// gave it and one at the assignment, wherever the assignment is.
func TestDroppedFactNotes(t *testing.T) {
	runNoteCases(t, droppedFactCases)
}

// droppedFactCases are programs where a region drops a fact.
var droppedFactCases = []noteCase{
	{
		"assigned later in the branch",
		program(
			"package main",
			"",
			"func main() {",
			"    mut d int64 = 2",
			"    mut x int64 = 0",
			"    if d != 0 {",
			"        x = 10 / d",
			"        d = 0",
			"    }",
			"    exit(x + d)",
			"}",
		),
		divD,
		[]wantNote{
			{"this guard proves d != 0", "d != 0", 6, 8},
			assignedHere("d", 8, 9),
		},
	},
	{
		"decremented before the use",
		program(
			"package main",
			"",
			"func main() {",
			"    mut d int64 = 4",
			"    if d != 0 {",
			"        d--",
			"        exit(8 / d)",
			"    }",
			"}",
		),
		divD,
		[]wantNote{
			{"this guard proves d != 0", "d != 0", 5, 8},
			assignedHere("d", 6, 9),
		},
	},
	{
		"assigned in a nested block",
		program(
			"package main",
			"",
			"func f(n int64, x int64) int64 {",
			"    mut d := n",
			"    if d != 0 {",
			"        if x > 0 {",
			"            d = 0",
			"        }",
			"        return 10 / d",
			"    }",
			"    return 0",
			"}",
			"",
			"func main() {",
			"    exit(f(2, 1))",
			"}",
		),
		divD,
		[]wantNote{
			{"this guard proves d != 0", "d != 0", 5, 8},
			assignedHere("d", 7, 13),
		},
	},
	{
		"assigned in a defer",
		program(
			"package main",
			"",
			"func main() {",
			"    mut d := 2",
			"    if d != 0 {",
			"        defer { exit(10 / d) }",
			"        defer { d = 0 }",
			"    }",
			"}",
		),
		divD,
		[]wantNote{
			{"this guard proves d != 0", "d != 0", 5, 8},
			assignedHere("d", 7, 17),
		},
	},
	{
		"one of several targets",
		program(
			"package main",
			"",
			"func g() (int64, int64) {",
			"    return 1, 0",
			"}",
			"",
			"func main() {",
			"    mut d int64 = 2",
			"    mut x int64 = 0",
			"    if d != 0 {",
			"        x = 10 / d",
			"        x, d = g()",
			"    }",
			"    exit(x)",
			"}",
		),
		divD,
		[]wantNote{
			{"this guard proves d != 0", "d != 0", 10, 8},
			assignedHere("d", 12, 12),
		},
	},
	{
		"the first of two assignments",
		program(
			"package main",
			"",
			"func main() {",
			"    mut d int64 = 2",
			"    if d != 0 {",
			"        d++",
			"        d--",
			"        exit(10 / d)",
			"    }",
			"}",
		),
		divD,
		[]wantNote{
			{"this guard proves d != 0", "d != 0", 5, 8},
			assignedHere("d", 6, 9),
		},
	},
	{
		"two guards, one assignment",
		program(
			"package main",
			"",
			"func main() {",
			"    mut d int64 = 2",
			"    if d != 0 {",
			"        if d > 0 {",
			"            d = 1",
			"            exit(10 / d)",
			"        }",
			"    }",
			"}",
		),
		divD,
		[]wantNote{
			{"this guard proves d != 0", "d != 0", 5, 8},
			{"this guard proves d != 0", "d > 0", 6, 12},
			assignedHere("d", 7, 13),
		},
	},
	{
		"an inner loop's post",
		program(
			"package main",
			"",
			"func main() {",
			"    a := [3]int64{1, 2, 3}",
			"    mut i int64 = 1",
			"    mut sum int64 = 0",
			"    if 0 <= i && i < 3 {",
			"        sum += a[i]",
			"        for ; i < 2; i++ {",
			"        }",
			"    }",
			"    exit(sum)",
			"}",
		),
		noteIndexI,
		[]wantNote{
			{"this guard proves 0 <= i && i < 3", "0 <= i && i < 3", 7, 8},
			assignedHere("i", 9, 22),
		},
	},
	{
		"the else branch",
		program(
			"package main",
			"",
			"func main() {",
			"    mut d int64 = 2",
			"    if d == 0 {",
			"        exit(1)",
			"    } else {",
			"        exit(10 / d)",
			"        d = 1",
			"    }",
			"}",
		),
		divD,
		[]wantNote{
			{"this guard proves d != 0 in its else branch", "d == 0", 5, 8},
			assignedHere("d", 9, 9),
		},
	},
	{
		"after an early exit",
		program(
			"package main",
			"",
			"func f(n int64) int64 {",
			"    mut d := n",
			"    if d == 0 {",
			"        return 0",
			"    }",
			"    q := 10 / d",
			"    d = 0",
			"    return q + d",
			"}",
			"",
			"func main() {",
			"    exit(f(2))",
			"}",
		),
		divD,
		[]wantNote{
			{"this early exit proves d != 0 after it", "if d == 0", 5, 5},
			assignedHere("d", 9, 5),
		},
	},
	{
		"a switch case",
		program(
			"package main",
			"",
			"func f(n int64) int64 {",
			"    mut d := n",
			"    switch d {",
			"    case 1, 2:",
			"        d = 0",
			"        return 10 / d",
			"    }",
			"    return 0",
			"}",
			"",
			"func main() {",
			"    exit(f(2))",
			"}",
		),
		divD,
		[]wantNote{
			{"this case proves d != 0", "case 1, 2", 6, 5},
			assignedHere("d", 7, 9),
		},
	},
	{
		"a later switch case",
		program(
			"package main",
			"",
			"func f(n int64) int64 {",
			"    mut d := n",
			"    switch d {",
			"    case 0:",
			"        return 0",
			"    default:",
			"        d = 0",
			"        return 10 / d",
			"    }",
			"}",
			"",
			"func main() {",
			"    exit(f(2))",
			"}",
		),
		divD,
		[]wantNote{
			{"this case proves d != 0 in the cases after it", "case 0", 6, 5},
			assignedHere("d", 9, 9),
		},
	},
	{
		"the string a length names",
		program(
			"package main",
			"",
			"func main() {",
			"    mut s := \"tack\"",
			"    if len(s) > 0 {",
			"        s = \"\"",
			"        exit(s[0])",
			"    }",
			"}",
		),
		"index 0 may be out of range; check len(s) > 0 first, or use v, ok := ...[0]",
		[]wantNote{
			{"this guard proves len(s) >= 1", "len(s) > 0", 5, 8},
			assignedHere("s", 6, 9),
		},
	},
	{
		"a range over a string",
		program(
			"package main",
			"",
			"func main() {",
			"    mut s := \"tack\"",
			"    for i := range s {",
			"        s = \"\"",
			"        exit(s[i])",
			"    }",
			"}",
		),
		strI,
		[]wantNote{
			{"this loop proves 0 <= i && i < len(s)", "for i := range s", 5, 5},
			assignedHere("s", 6, 9),
		},
	},
	{
		"only the fact that names the assigned string",
		program(
			"package main",
			"",
			"func f(s string, i int64) uint8 {",
			"    mut t := s",
			"    if 0 <= i && i < len(t) {",
			"        t = \"x\"",
			"        return t[i]",
			"    }",
			"    return 0",
			"}",
			"",
			"func main() {",
			"    exit(f(\"tack\", 1))",
			"}",
		),
		"index i may be out of range; check 0 <= i && i < len(t) first, or use v, ok := ...[i]",
		[]wantNote{
			{"this guard proves i < len(t)", "0 <= i && i < len(t)", 5, 8},
			assignedHere("t", 6, 9),
		},
	},
	{
		"a counting loop whose body assigns its variable",
		program(
			"package main",
			"",
			"func main() {",
			"    a := [3]int64{10, 20, 30}",
			"    mut sum int64 = 0",
			"    for mut i := 0; i < 3; i++ {",
			"        sum += a[i]",
			"        i++",
			"    }",
			"    exit(sum)",
			"}",
		),
		noteIndexI,
		[]wantNote{
			{"counting from this start proves i >= 0", "mut i := 0", 6, 9},
			{"this guard proves i < 3", "i < 3", 6, 21},
			assignedHere("i", 8, 9),
		},
	},
	{
		"a dropped fact that would not have been enough",
		program(
			"package main",
			"",
			"func main() {",
			"    a := [3]int64{10, 20, 30}",
			"    mut i int64 = 0",
			"    mut sum int64 = 0",
			"    for i < 3 { sum += a[i]; i++ }",
			"    exit(sum)",
			"}",
		),
		noteIndexI,
		[]wantNote{
			{"this guard proves i < 3", "i < 3", 7, 9},
			assignedHere("i", 7, 30),
			{"this guard would bound i only above; i may be negative", "i < 3", 7, 9},
		},
	},
	{
		"deferred code, with a fact from an early exit",
		program(
			"package main",
			"",
			"func main() {",
			"    mut b int64 = 3",
			"    if b == 0 {",
			"        return",
			"    }",
			"    defer {",
			"        exit(12 / b)",
			"    }",
			"    b = 0",
			"}",
		),
		divB,
		[]wantNote{
			{"this early exit proves b != 0 after it", "if b == 0", 5, 5},
			assignedHere("b", 11, 5),
		},
	},
}

// TestWeakFactNotes checks that a fact in force that does not prove the
// operation is explained by a note at the guard that gave it.
func TestWeakFactNotes(t *testing.T) {
	runNoteCases(t, weakFactCases)
}

// weakFactCases are programs whose facts in force fall short of a proof.
var weakFactCases = []noteCase{
	{
		"an array index bounded only above",
		inF2("a [3]int64, i int64", "if i < 3 {", "    return a[i]", "}"),
		noteIndexI,
		[]wantNote{{"this guard bounds i only above; i may be negative", "i < 3", 4, 8}},
	},
	{
		"a string index bounded only above",
		inF8("s string, i int64", "if i < len(s) {", "    return s[i]", "}"),
		strI,
		[]wantNote{{"this guard bounds i only above; i may be negative", "i < len(s)", 4, 8}},
	},
	{
		"an off by one guard",
		inF2("a [3]int64, i int64", "if 0 <= i && i <= len(a) {", "    return a[i]", "}"),
		noteIndexI,
		[]wantNote{{"this guard allows i == 3, and a has 3 elements", "i <= len(a)", 4, 18}},
	},
	{
		"a lower bound below zero",
		inF2("a [3]int64, i int64", "if -1 <= i && i < 3 {", "    return a[i]", "}"),
		noteIndexI,
		[]wantNote{{"this guard allows i == -1", "-1 <= i", 4, 8}},
	},
	{
		"a case that allows a value out of range",
		inF2("a [3]int64, i int64", "switch i { case 0, 3: return a[i] }"),
		noteIndexI,
		[]wantNote{{"this case allows i == 3, and a has 3 elements", "case 0, 3", 4, 16}},
	},
	{
		"a range over a longer array",
		inF2("a [3]int64, b [4]int64", "for i := range b { return a[i] }"),
		noteIndexI,
		[]wantNote{{"this loop allows i == 3, and a has 3 elements", "for i := range b", 4, 5}},
	},
	{
		"a case that allows zero",
		inF2("d int64", "switch d {", "case 1, 0:", "    return 10 / d", "}"),
		divD,
		[]wantNote{{"this case allows d == 0", "case 1, 0", 5, 5}},
	},
	{
		"a guard that allows zero",
		inF2("b int64", "if b >= 0 {", "    return 8 / b", "}"),
		divB,
		[]wantNote{{"this guard allows b == 0", "b >= 0", 4, 8}},
	},
	{
		"a guard on another string",
		inF8(
			"s string, t string, i int64",
			"if 0 <= i && i < len(t) {",
			"    return s[i]",
			"}",
		),
		strI,
		[]wantNote{{"this guard bounds i by the length of t, not s", "i < len(t)", 4, 18}},
	},
	{
		"a guard on another array",
		inF2(
			"a [3]int64, b [4]int64, i int64",
			"if 0 <= i && i < len(b) {",
			"    return a[i]",
			"}",
		),
		noteIndexI,
		[]wantNote{{"this guard bounds i by the length of b, not a", "i < len(b)", 4, 18}},
	},
	{
		"a known string's length",
		inF8("i int64", "w := \"tack\"", "if 0 <= i && i <= 4 {", "    return w[i]", "}"),
		"index i may be out of range; check 0 <= i && i < len(w) first, or use v, ok := ...[i]",
		[]wantNote{{"this guard allows i == 4, and w has 4 bytes", "i <= 4", 5, 18}},
	},
	{
		"an early exit that bounds only above",
		inF2("a [3]int64, i int64", "if i >= 3 {", "    return 0", "}", "return a[i]"),
		noteIndexI,
		[]wantNote{{"this early exit bounds i only above; i may be negative", "i >= 3", 4, 8}},
	},
}

// inF2 wraps lines, each indented one level, in f, which takes params and
// returns an int64, and a main that calls nothing. The first line is line 4.
func inF2(params string, lines ...string) string {
	return inFunc(params, "int64", lines)
}

// inF8 is inF2 for an f that returns a uint8.
func inF8(params string, lines ...string) string {
	return inFunc(params, "uint8", lines)
}

// inFunc wraps lines in f, which takes params and returns result, with the
// first line on line 4, then a return of 0 and an empty main.
func inFunc(params, result string, lines []string) string {
	body := ""
	for _, l := range lines {
		body += "    " + l + "\n"
	}
	return "package main\n\nfunc f(" + params + ") " + result + " {\n" + body +
		"    return 0\n}\n\nfunc main() {\n}\n"
}

// TestNoNoteWithoutAGuard checks that an operation no guard says anything
// about keeps its error alone.
func TestNoNoteWithoutAGuard(t *testing.T) {
	for _, src := range []string{
		inF2("a [3]int64, i int64", "return a[i]"),
		inF2("b int64", "return 8 / b"),
		inF8("s string, i int64", "return s[i]"),
		inF2("b int64, c int64", "if c != 0 {", "    return 8 / b", "}"),
	} {
		diags := check(t, src)
		if len(diags) != 1 {
			t.Fatalf("got %v, want one error", diags)
		}
		if len(diags[0].Related) != 0 {
			t.Errorf("%q: got notes %v, want none", src, diags[0].Related)
		}
	}
}

// TestDroppedFactsProveNothing checks that a fact kept only to explain an
// error never proves anything, through each query a proof makes: the
// interval of a divisor and of an index, the least length of a string, and
// a bound by a string's length. Each program would compile if the dropped
// fact counted.
func TestDroppedFactsProveNothing(t *testing.T) {
	for _, tt := range []struct{ name, src string }{
		{"divisor", inMain("mut d int64 = 2\n    if d != 0 {\n        d = 1\n        exit(8 / d)\n    }")},
		{
			"compound divisor",
			inMain("mut d int64 = 2\n    mut x int64 = 8\n    if d != 0 {\n        d = 1\n        x /= d\n    }\n    exit(x)"),
		},
		{
			"array index",
			inF2("a [3]int64, n int64", "mut i := n", "if 0 <= i && i < 3 {", "    i = 0", "    return a[i]", "}"),
		},
		{
			"array write",
			inF2("n int64", "mut a [3]int64", "mut i := n", "if 0 <= i && i < 3 {", "    i = 0", "    a[i] = 1", "}", "return a[0]"),
		},
		{
			"least length",
			inF8("t string", "mut s := t", "if len(s) > 0 {", "    s = \"x\"", "    return s[0]", "}"),
		},
		{
			"length bound",
			inF8("t string, i int64", "mut s := t", "if 0 <= i && i < len(s) {", "    s = \"x\"", "    return s[i]", "}"),
		},
		{
			"length bound by a range",
			inF8("t string", "mut s := t", "for i := range s {", "    s = \"x\"", "    return s[i]", "}"),
		},
		{
			"lower bound by a counting loop",
			inF2("a [3]int64", "for mut i := 0; i < 3; i++ {", "    i += 0", "    return a[i]", "}"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			diags := check(t, tt.src)
			if len(diags) != 1 {
				t.Fatalf("got %v, want the one missing proof", diags)
			}
		})
	}
}

// TestExplainingLeavesTheFactsAlone checks that working out whether a
// dropped fact would have been enough does not leave it proving anything:
// every use after the first error is still reported.
func TestExplainingLeavesTheFactsAlone(t *testing.T) {
	src := inF2("a [3]int64, n int64",
		"mut i := n",
		"if 0 <= i && i < 3 {",
		"    mut s := a[i]",
		"    s += a[i]",
		"    i = 0",
		"    return s",
		"}",
		"return a[i]",
	)
	diags := check(t, src)
	if len(diags) != 3 {
		t.Fatalf("got %d errors, want 3: %v", len(diags), diags)
	}
	for _, d := range diags[:2] {
		if len(d.Related) != 2 {
			t.Errorf("got notes %v, want the guard and the assignment", d.Related)
		}
	}
	if len(diags[2].Related) != 0 {
		t.Errorf("an unguarded index got notes %v", diags[2].Related)
	}
}

// TestOtherCauseNotes checks the notes that explain a missing proof by
// something other than a fact: an early exit through exit, a counting loop
// whose step could wrap, a mutable global, a field, an operand that is not
// a plain name, and an index the checked form does not cover.
func TestOtherCauseNotes(t *testing.T) {
	runNoteCases(t, otherCauseCases)
}

// globalNote is the note at the declaration of the mutable global g.
var globalNote = wantNote{
	"g is a mutable global, so no check proves it; copy it into a local first", "g", 3, 5,
}

// inGlobalFile wraps lines in f, which takes params and returns result, in a
// file that declares the mutable global g, an int64, on line 3. The first
// line is line 6.
func inGlobalFile(params, result string, lines ...string) string {
	return "package main\n\nmut g int64\n\n" + inFunc(params, result, lines)[len("package main\n\n"):]
}

// otherCauseCases are programs whose missing proof has a cause other than a
// fact.
var otherCauseCases = []noteCase{
	{
		"exit as an early exit",
		inF2("a int64, b int64", "if b == 0 { exit(1) }", "return a / b"),
		divB,
		[]wantNote{
			{"this early exit would prove b != 0 after it", "if b == 0", 4, 5},
			{"but its branch ends in exit, not in return, break or continue", "exit(1)", 4, 17},
		},
	},
	{
		"exit in an else",
		inF2("a int64, b int64", "if b != 0 {", "} else {", "    exit(1)", "}", "return a / b"),
		divB,
		[]wantNote{
			{"this early exit would prove b != 0 after it", "if b != 0", 4, 5},
			{"but its branch ends in exit, not in return, break or continue", "exit(1)", 6, 9},
		},
	},
	{
		"a step that could wrap upwards",
		inF2("a [3]int64",
			"mut sum int64 = 0",
			"for mut i int8 = 2; i < 3; i += 126 {",
			"    sum += a[i]",
			"}",
			"return sum",
		),
		noteIndexI,
		[]wantNote{{
			"this step could wrap i past 127, so the loop does not keep i at or above 2",
			"i += 126", 5, 32,
		}},
	},
	{
		"a step that could wrap downwards",
		inF2("a int64", "for mut i int8 = -2; i > -3; i -= 127 {", "    return a / i", "}"),
		"divisor i may be zero; check i != 0 first, or use q, ok := ... / i",
		[]wantNote{{
			"this step could wrap i past -128, so the loop does not keep i at or below -2",
			"i -= 127", 4, 34,
		}},
	},
	{
		"a mutable global divisor",
		inGlobalFile("x int64", "int64", "if g != 0 { return x / g }"),
		"divisor g may be zero; use q, ok := ... / g",
		[]wantNote{globalNote},
	},
	{
		"a mutable global divisor with no guard",
		inGlobalFile("x int64", "int64", "mut y := x", "y /= g", "return y"),
		"divisor g may be zero",
		[]wantNote{globalNote},
	},
	{
		"a mutable global index",
		inGlobalFile("a [3]int64", "int64", "return a[g]"),
		"index g may be out of range; use v, ok := ...[g]",
		[]wantNote{globalNote},
	},
	{
		"a mutable global index written",
		inGlobalFile("", "int64", "mut a [3]int64", "a[g] = 1", "return a[0]"),
		"index g may be out of range",
		[]wantNote{globalNote},
	},
	{
		"a mutable global string index",
		inGlobalFile("s string", "uint8", "return s[g]"),
		"index g may be out of range; use v, ok := ...[g]",
		[]wantNote{globalNote},
	},
	{
		"a mutable global string",
		"package main\n\nmut g = \"tack\"\n\nfunc f() uint8 {\n    if len(g) > 0 {\n        return g[0]\n    }\n" +
			"    return 0\n}\n\nfunc main() {\n}\n",
		"index 0 may be out of range; use v, ok := ...[0]",
		[]wantNote{globalNote},
	},
	{
		"the length of a field",
		"package main\n\ntype L struct {\n    src string\n    pos int64\n}\n\n" +
			"func f(mut l L) uint8 {\n    i := l.pos\n    if i >= 0 && i < len(l.src) {\n        return l.src[i]\n    }\n" +
			"    return 0\n}\n\nfunc main() {\n}\n",
		"index i may be out of range; use v, ok := ...[i]",
		[]wantNote{
			{
				"this guard proves nothing: a field of a mutable variable or a mut parameter " +
					"never carries a fact; copy l.src into a variable first",
				"i >= 0 && i < len(l.src)",
				10,
				8,
			},
		},
	},
	{
		"the length of a field, for a constant",
		"package main\n\ntype L struct {\n    src string\n}\n\n" +
			"func f(mut l L) uint8 {\n    if len(l.src) > 0 {\n        return l.src[0]\n    }\n" +
			"    return 0\n}\n\nfunc main() {\n}\n",
		"index 0 may be out of range; use v, ok := ...[0]",
		[]wantNote{
			{
				"this guard proves nothing: a field of a mutable variable or a mut parameter " +
					"never carries a fact; copy l.src into a variable first",
				"len(l.src) > 0",
				8,
				8,
			},
		},
	},
	{
		"a field divisor",
		"package main\n\ntype P struct {\n    d int64\n}\n\n" +
			"func f(n int64, mut p P) int64 {\n    if p.d != 0 { return n / p.d }\n    return 0\n}\n\n" +
			"func main() {\n}\n",
		"divisor may be zero; use q, ok := ... / ...",
		[]wantNote{
			{
				"this guard proves nothing: a field of a mutable variable or a mut parameter " +
					"never carries a fact; copy p.d into a variable first",
				"p.d != 0",
				8,
				8,
			},
		},
	},
	{
		"a field index after an early exit",
		"package main\n\ntype P struct {\n    i int64\n}\n\n" +
			"func f(a [3]int64, mut p P) int64 {\n    if p.i < 0 || p.i >= 3 {\n        return 0\n    }\n" +
			"    return a[p.i]\n}\n\nfunc main() {\n}\n",
		"index may be out of range; use v, ok := ...[...]",
		[]wantNote{
			{
				"this early exit proves nothing: a field of a mutable variable or a mut parameter " +
					"never carries a fact; copy p.i into a variable first",
				"p.i < 0 || p.i >= 3",
				8,
				8,
			},
		},
	},
	{
		"an index that is not a name",
		inF2("a [3]int64, i int64", "if 0 <= i && i < 2 {", "    return a[i + 1]", "}"),
		"index may be out of range; use v, ok := ...[...]",
		[]wantNote{
			{
				"this guard bounds i, not i + 1; only a name, or a field or element of an immutable variable, is proven, " +
					"so copy i + 1 into a variable and check it",
				"0 <= i && i < 2",
				4,
				8,
			},
		},
	},
	{
		"a divisor that is not a name",
		inF2("a int64, b int64", "if b > 1 {", "    return a / (b - 1)", "}"),
		"divisor may be zero; use q, ok := ... / ...",
		[]wantNote{
			{
				"this guard bounds b, not b - 1; only a name, or a field or element of an immutable variable, is proven, " +
					"so copy b - 1 into a variable and check it",
				"b > 1",
				4,
				8,
			},
		},
	},
	{
		"a written index that is not a name",
		inF2(
			"i int64",
			"mut a [3]int64",
			"if 0 <= i && i < 2 {",
			"    a[i + 1] = 1",
			"}",
			"return a[0]",
		),
		"index may be out of range",
		[]wantNote{
			{
				"this guard bounds i, not i + 1; only a name, or a field or element of an immutable variable, is proven, " +
					"so copy i + 1 into a variable and check it",
				"0 <= i && i < 2",
				5,
				8,
			},
		},
	},
	{
		"only the outer index is checked",
		inF2("m [2][3]int64, i int64, j int64", "v, ok := m[i][j]", "if ok {", "    return v", "}"),
		"index i may be out of range; check 0 <= i && i < 2 first, or use v, ok := ...[i]",
		[]wantNote{{
			"only the outer index of m[i][j] is checked; m[i] needs its own proof",
			"m[i][j]", 4, 14,
		}},
	},
}

// TestUint64ComparedWithLen checks the note on comparing a uint64 with
// len, an int64, which says how to convert it.
func TestUint64ComparedWithLen(t *testing.T) {
	for _, tt := range []struct {
		name, cond string
		col        int
	}{
		{"on the left", "u < len(s)", 8},
		{"on the right", "len(s) > u", 17},
	} {
		t.Run(tt.name, func(t *testing.T) {
			expectNotes(
				t,
				inF8("s string, u uint64", "if "+tt.cond+" {", "    return 1", "}"),
				"mismatched types "+map[bool]string{true: "uint64 and int64", false: "int64 and uint64"}[tt.col == 8]+
					" in "+strings.Fields(tt.cond)[1],
				wantNote{
					"len gives an int64; convert u first with i, ok := int64(u)",
					"u",
					4,
					tt.col,
				},
			)
		})
	}
}

// TestNoOtherNoteWithoutAGuard checks that a field, or an operand that is
// not a name, that no guard mentions keeps its error alone.
func TestNoOtherNoteWithoutAGuard(t *testing.T) {
	for _, src := range []string{
		inF2("a [3]int64, i int64", "return a[i + 1]"),
		inF2("a int64, b int64, c int64", "if c > 1 {", "    return a / (b - 1)", "}"),
		"package main\n\ntype P struct {\n    d int64\n}\n\n" +
			"func f(n int64, p P, q P) int64 {\n    if q.d != 0 { return n / p.d }\n    return 0\n}\n\n" +
			"func main() {\n}\n",
	} {
		diags := check(t, src)
		if len(diags) != 1 || len(diags[0].Related) != 0 {
			t.Errorf("%q: got %v, want one error with no notes", src, diags)
		}
	}
}

// TestPhantomFactsProveNothing checks that a fact that would hold but for
// how its region is written, after an if whose branch ends in exit or in a
// counting loop whose step could wrap, proves nothing.
func TestPhantomFactsProveNothing(t *testing.T) {
	for _, src := range []string{
		inF2("a int64, b int64", "if b == 0 { exit(1) }", "return a / b"),
		inF2("a [3]int64", "for mut i int8 = 2; i < 3; i += 126 {", "    return a[i]", "}"),
	} {
		if diags := check(t, src); len(diags) != 1 {
			t.Errorf("%q: got %v, want the one missing proof", src, diags)
		}
	}
}
