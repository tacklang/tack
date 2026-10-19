package parser_test

import (
	"fmt"
	"strings"
	"testing"
)

// tooDeep is the error for a program nested past the parser's limit.
const tooDeep = "expression nested too deeply: more than 1000 levels"

// nestingShape writes, for a depth n, a program that nests one construct
// n levels deep, and marks the opening token of its last level with a
// NUL byte just before it, which nested strips: the token the error
// points at when n is one past the limit.
type nestingShape struct {
	gen  func(n int) string
	name string
}

// repeatMarked returns open repeated n times, the last one marked.
func repeatMarked(open string, n int) string {
	return strings.Repeat(open, n-1) + "\x00" + open
}

// nestingShapes are the constructs that nest: parentheses, the unary
// operators, mut, a call's arguments, an index's brackets, a slice's,
// array, struct and nested literals, blocks of if, for, switch, defer and
// a plain block's statements, and array, view, list and function types; and
// shapes that mix them, which count against one limit together.
var nestingShapes = []nestingShape{
	{name: "parens", gen: func(n int) string {
		return inFunc("    x := " + repeatMarked("(", n) + "1" + strings.Repeat(")", n) + "\n")
	}},
	{name: "not", gen: func(n int) string {
		return inFunc("    x := " + repeatMarked("!", n) + "true\n")
	}},
	{name: "negation", gen: func(n int) string {
		return inFunc("    x := " + repeatMarked("- ", n) + "y\n")
	}},
	{name: "right-nested and", gen: func(n int) string {
		return inFunc(
			"    x := " + strings.Repeat(
				"b && (",
				n-1,
			) + "b && \x00(" + "b" + strings.Repeat(
				")",
				n,
			) + "\n",
		)
	}},
	{name: "calls", gen: func(n int) string {
		return inFunc(
			"    x := " + strings.Repeat(
				"f(",
				n-1,
			) + "f\x00(" + "1" + strings.Repeat(
				")",
				n,
			) + "\n",
		)
	}},
	{name: "indexes", gen: func(n int) string {
		return inFunc(
			"    x := " + strings.Repeat(
				"t[",
				n-1,
			) + "t\x00[" + "0" + strings.Repeat(
				"]",
				n,
			) + "\n",
		)
	}},
	{name: "slices", gen: func(n int) string {
		return inFunc(
			"    x := " + strings.Repeat(
				"s[",
				n-1,
			) + "s\x00[" + "0" + strings.Repeat(
				":]",
				n,
			) + "\n",
		)
	}},
	{name: "array literals", gen: func(n int) string {
		return inFunc(
			"    x := [1]int64" + repeatMarked("{", n) + "1" + strings.Repeat("}", n) + "\n",
		)
	}},
	{name: "struct literals", gen: func(n int) string {
		return inFunc(
			"    x := " + strings.Repeat(
				"S{s: ",
				n-1,
			) + "S\x00{" + "v: 1" + strings.Repeat(
				"}",
				n,
			) + "\n",
		)
	}},
	{name: "ifs", gen: func(n int) string {
		return inFunc(
			strings.Repeat("    if b {\n", n-1) + "    if b \x00{\n" + strings.Repeat("    }\n", n),
		)
	}},
	{name: "fors", gen: func(n int) string {
		return inFunc(
			strings.Repeat(
				"    for b {\n",
				n-1,
			) + "    for b \x00{\n" + strings.Repeat(
				"    }\n",
				n,
			),
		)
	}},
	{name: "switches", gen: func(n int) string {
		return inFunc(
			strings.Repeat(
				"    switch x {\n    case 1:\n",
				n-1,
			) + "    switch x \x00{\n    case 1:\n" +
				strings.Repeat(
					"    }\n",
					n,
				),
		)
	}},
	{name: "defers", gen: func(n int) string {
		return inFunc(
			strings.Repeat(
				"    defer {\n",
				n-1,
			) + "    defer \x00{\n" + strings.Repeat(
				"    }\n",
				n,
			),
		)
	}},
	{name: "array types", gen: func(n int) string {
		return "package main\n\nfunc f(a " + repeatMarked("[1]", n) + "int64) {\n}\n"
	}},
	{name: "view types", gen: func(n int) string {
		return "package main\n\nfunc f(a " + repeatMarked("[]", n) + "int64) {\n}\n"
	}},
	{name: "list types", gen: func(n int) string {
		return "package main\n\nfunc f(a " + repeatMarked(
			"list[",
			n,
		) + "int64" + strings.Repeat(
			"]",
			n,
		) + ") {\n}\n"
	}},
	{name: "function types", gen: func(n int) string {
		return "package main\n\nfunc f(a " + repeatMarked("func(", n) + "int64" +
			strings.Repeat(")", n) + ") {\n}\n"
	}},
	{name: "blocks around parens", gen: func(n int) string {
		half := n / 2
		return inFunc(strings.Repeat("    if b {\n", half) +
			"    x := " + repeatMarked("(", n-half) + "1" + strings.Repeat(")", n-half) + "\n" +
			strings.Repeat("    }\n", half))
	}},
	{name: "a call of a literal of a type", gen: func(n int) string {
		third := n / 3
		return inFunc("    x := " + strings.Repeat("f(", third) +
			strings.Repeat("[1]", n-2*third) + "int64" + repeatMarked("{", n-2*third+third) + "1" +
			strings.Repeat("}", n-2*third+third) + strings.Repeat(")", third) + "\n")
	}},
}

// inFunc returns a program whose one function has body.
func inFunc(body string) string {
	return "package main\n\nfunc main() {\n" + body + "}\n"
}

// nested strips the mark from src and returns it and the offset the mark
// was at, where the marked token now starts.
func nested(src string) (string, int) {
	at := strings.IndexByte(src, 0)
	return strings.Replace(src, "\x00", "", 1), at
}

// TestNestingAtTheLimitParses parses every shape nested exactly 1000
// levels deep, the most the parser takes, and checks it parses.
func TestNestingAtTheLimitParses(t *testing.T) {
	for _, shape := range nestingShapes {
		t.Run(shape.name, func(t *testing.T) {
			src, _ := nested(shape.gen(1000))
			if _, diags := parse(t, src); len(diags) > 0 {
				t.Fatalf("at 1000 levels: %s", diags[0].Message)
			}
		})
	}
}

// TestNestingPastTheLimitIsReported parses every shape nested 1001
// levels deep and checks it is reported once, as too deep, at the token
// that opens its 1001st level.
func TestNestingPastTheLimitIsReported(t *testing.T) {
	for _, shape := range nestingShapes {
		t.Run(shape.name, func(t *testing.T) {
			src, at := nested(shape.gen(1001))
			_, diags := parse(t, src)
			if len(diags) != 1 {
				t.Fatalf("got %d diagnostics, want 1: %v", len(diags), diags)
			}
			d := diags[0]
			if d.Message != tooDeep {
				t.Errorf("message %q, want %q", d.Message, tooDeep)
			}
			if d.Span.Start != at {
				t.Errorf(
					"reported at %d (%q), want %d (%q)",
					d.Span.Start,
					src[d.Span.Start:d.Span.End],
					at,
					src[at:at+1],
				)
			}
		})
	}
}

// TestFlatChainsDoNotNest checks the forms that are long but not deep
// parse past the limit: a chain of 2000 + operators, which binds left to
// right and nests no deeper for its length, and a chain of 10000 else
// ifs, each the else branch of the one before but flat in the source.
func TestFlatChainsDoNotNest(t *testing.T) {
	sum := inFunc("    x := 1" + strings.Repeat(" + 1", 2000) + "\n")
	if _, diags := parse(t, sum); len(diags) > 0 {
		t.Errorf("a chain of 2000 + operators: %s", diags[0].Message)
	}
	if _, diags := parse(t, elseIfChain(10000)); len(diags) > 0 {
		t.Errorf("a chain of 10000 else ifs: %s", diags[0].Message)
	}
}

// TestHugeNestingFailsFast parses nested parentheses past the parser's
// limit and checks the error comes in steps in proportion to them (see
// checkStepGrowth), 5000 and 20000 of them, and that for a million the
// parser goes no deeper than its limit, once.
func TestHugeNestingFailsFast(t *testing.T) {
	src := func(n int) string {
		open, closing := strings.Repeat("(", n), strings.Repeat(")", n)
		return inFunc(fmt.Sprintf("    x := %s1%s\n", open, closing))
	}
	_, small := parseSteps(t, src(5000))
	_, grown := parseSteps(t, src(20000))
	checkStepGrowth(t, "nested parentheses", small, grown)
	diags, _ := parseSteps(t, src(1000000))
	if len(diags) != 1 || diags[0].Message != tooDeep {
		t.Fatalf("got %v, want the one error %q", diags, tooDeep)
	}
	want := strings.Index(src(1000000), "x := (") + len("x := ") + 1000
	if diags[0].Span.Start != want {
		t.Errorf("reported at %d, want the 1001st parenthesis, at %d", diags[0].Span.Start, want)
	}
}
