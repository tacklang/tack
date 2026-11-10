package typeck_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// inF wraps body in f, which takes a number n, a divisor d, an index i, a
// string s and an array a, and returns an int64, and is called from main.
func inF(body ...string) string {
	return "package main\n\nfunc report() {\n}\n\n" +
		"func pair() (int64, int64) {\n    return 1, 2\n}\n\n" +
		"type P struct {\n    x int64\n}\n\ntype Q struct {\n    x int64\n}\n\n" +
		"func f(n int64, d int64, i int64, s string, a [4]int64) int64 {\n    " +
		strings.Join(body, "\n    ") +
		"\n}\n\nfunc main() {\n    code, fits := int32(f(1, 1, 1, \"x\", [4]int64{1, 2, 3, 4}))\n    if fits { exit(code) }\n}\n"
}

// acceptedSwitches must check cleanly: well-typed tags and values, and
// divisions and indexes the cases prove.
var acceptedSwitches = []struct {
	name string
	src  string
}{
	{"empty", inF("switch n {", "}", "switch {", "}", "return 0")},
	{"empty cases", inF("switch n {", "case 1:", "case 2, 3:", "default:", "}", "return 0")},
	{"untyped tag", inF("switch 3 {", "case n:", "}", "return 0")},
	{
		"bool values not known are never duplicates",
		inF("b := n > 0", "switch b {", "case n > 0, n > 0, true:", "case false:", "}", "return 0"),
	},
	{
		"narrow value widens to the tag",
		inF("var b int8 = 1", "switch n {", "case b:", "}", "return 0"),
	},
	{
		"narrow tag widens to the value",
		inF("var b int8 = 1", "switch b {", "case n:", "}", "return 0"),
	},
	{
		"char value on a uint8 tag",
		inF("var c uint8 = 1", "switch c {", "case 'x', 255:", "}", "return 0"),
	},
	{"strings", inF("switch s {", "case \"a\", `b`, s:", "}", "return 0")},
	{"bools", inF("switch n > 0 {", "case true:", "case n < 9:", "}", "return 0")},
	{"arrays", inF("switch a {", "case [4]int64{1, 2, 3, 4}, a:", "}", "return 0")},
	{"structs", inF("switch (P{x: 1}) {", "case P{x: n}:", "}", "return 0")},
	{"calls as values", inF("switch n {", "case f(1, 2, 3, s, a), n +% 1:", "}", "return 0")},
	{"tagless conditions", inF("switch {", "case n > 0, s == \"\", true:", "}", "return 0")},
	{
		"break in a loop",
		inF("for {", "    switch {", "    case n > 0:", "        break", "    }", "}", "return 0"),
	},
	{
		"continue in a loop",
		inF(
			"for range 3 {",
			"    switch n {",
			"    case 1:",
			"        continue",
			"    }",
			"}",
			"return 0",
		),
	},
	{
		"case sees its condition",
		inF("switch {", "case d != 0:", "    return n / d", "}", "return 0"),
	},
	{
		"default sees every condition false",
		inF("switch {", "case d == 0:", "    return 0", "default:", "    return n / d", "}"),
	},
	{
		"default in the middle",
		inF("switch {", "default:", "    return n / d", "case d == 0:", "    return 0", "}"),
	},
	{
		"later case sees earlier false",
		inF(
			"switch {",
			"case d == 0:",
			"    return 0",
			"case n > 0:",
			"    return n / d",
			"}",
			"return 0",
		),
	},
	{
		"a list false gives all",
		inF(
			"switch {",
			"case d == 0, n == 0:",
			"    return 0",
			"default:",
			"    return n / d +% 1 / n",
			"}",
		),
	},
	{
		"a value sees those before it false",
		inF("switch {", "case d == 0, n / d > 1:", "}", "return 0"),
	},
	{
		"expression switch default",
		inF("switch d {", "case 0:", "    return 0", "default:", "    return n / d", "}"),
	},
	{"expression switch case", inF("switch d {", "case 3:", "    return n / d", "}", "return 0")},
	{"expression switch value", inF("switch d {", "case 0, n / d:", "}", "return 0")},
	{"after a switch", inF("switch d {", "case 0:", "    return 0", "}", "return n / d")},
	{
		"after a tagless switch",
		inF("switch {", "case d == 0:", "    return 0", "case n > 0:", "}", "return n / d"),
	},
	{
		"after a break in a loop",
		inF(
			"for {",
			"    switch {",
			"    case d == 0:",
			"        break",
			"    }",
			"    return n / d",
			"}",
			"return 0",
		),
	},
	{
		"index",
		inF("switch {", "case i < 0, i >= 4:", "    return 0", "default:", "    return a[i]", "}"),
	},
	{
		"length of a string",
		inF("switch len(s) {", "case 0:", "    return 0", "default:", "    return s[0]", "}"),
	},
	{
		"nested",
		inF(
			"switch {",
			"case d != 0:",
			"    switch n {",
			"    case 1:",
			"        return n / d",
			"    }",
			"}",
			"return 0",
		),
	},
	{
		"same value in two switches",
		inF("switch n {", "case 1:", "}", "switch n {", "case 1:", "}", "return 0"),
	},
	{
		"same value in a nested switch",
		inF("switch n {", "case 1:", "    switch n {", "    case 1:", "    }", "}", "return 0"),
	},
	{
		"same mutable variable twice",
		inF("mut m := 1", "m = 2", "switch n {", "case m, m:", "}", "return 0"),
	},
	{
		"same call twice",
		inF("switch n {", "case f(1, 2, 3, s, a), f(1, 2, 3, s, a):", "}", "return 0"),
	},
	{
		"integer values of a type with no common type",
		inF("var u uint64 = 1", "switch n {", "case u, uint8(2), int8(-1):", "}", "return 0"),
	},
	{
		"signed and unsigned of one width",
		inF("var b int8 = 1", "var c uint8 = 1", "switch b {", "case c:", "}", "return 0"),
	},
	{
		"target-width values on a signed tag",
		inF(
			"var z unsafe_size_t = 1",
			"var p unsafe_uintptr_t = 2",
			"switch n {",
			"case z, p:",
			"}",
			"return 0",
		),
	},
	{
		"same value of two types",
		inF("switch n {", "case uint8(255):", "case int16(-255):", "}", "return 0"),
	},
}

// TestAcceptsSwitches checks well-formed switches, and what their cases
// prove, check cleanly.
func TestAcceptsSwitches(t *testing.T) {
	for _, tt := range acceptedSwitches {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// switchError is one diagnostic a rejected switch must give: its message
// and the source it covers.
type switchError struct{ msg, at string }

// rejectedSwitches must give exactly the listed diagnostics, in order.
var rejectedSwitches = []struct {
	name string
	src  string
	want []switchError
}{
	{
		"int tag, string value",
		inF("switch n {", "case \"1\":", "}", "return 0"),
		[]switchError{{"mismatched types int64 and string in switch case", "\"1\""}},
	},
	{
		"string tag, int value",
		inF("switch s {", "case 1:", "}", "return 0"),
		[]switchError{{"mismatched types string and int64 in switch case", "1"}},
	},
	{
		"bool tag, int value",
		inF("switch n > 0 {", "case 0:", "}", "return 0"),
		[]switchError{{"mismatched types bool and int64 in switch case", "0"}},
	},
	{
		"different structs",
		inF("switch (P{x: 1}) {", "case Q{x: 1}:", "}", "return 0"),
		[]switchError{{"mismatched types P and Q in switch case", "Q{x: 1}"}},
	},
	{
		"different array lengths",
		inF("switch a {", "case [3]int64{1, 2, 3}:", "}", "return 0"),
		[]switchError{
			{"mismatched types [4]int64 and [3]int64 in switch case", "[3]int64{1, 2, 3}"},
		},
	},
	{
		"constant overflows the tag",
		inF("var c uint8 = 1", "switch c {", "case 256:", "}", "return 0"),
		[]switchError{{"constant 256 overflows uint8", "256"}},
	},
	{
		"negative constant on an unsigned tag",
		inF("var c uint8 = 1", "switch c {", "case -1:", "}", "return 0"),
		[]switchError{{"constant -1 overflows uint8", "-1"}},
	},
	{
		"tagless int condition",
		inF("switch {", "case n > 0, n:", "}", "return 0"),
		[]switchError{{"condition must be a bool, got int64", "n"}},
	},
	{
		"tagless string condition",
		inF("switch {", "case s:", "}", "return 0"),
		[]switchError{{"condition must be a bool, got string", "s"}},
	},
	{
		"tag with no value",
		inF("switch report() {", "case 1:", "}", "return 0"),
		[]switchError{{"report returns no value", "report()"}},
	},
	{
		"tag with two values",
		inF("switch pair() {", "}", "return 0"),
		[]switchError{{"pair returns 2 values", "pair()"}},
	},
	{
		"values checked when the tag is bad",
		inF("switch report() {", "case s + 1:", "}", "return 0"),
		[]switchError{
			{"report returns no value", "report()"},
			{"operator + is not defined on string", "s"},
		},
	},
	{
		"duplicate integers",
		inF("switch n {", "case 1, 2:", "case 3, 1:", "}", "return 0"),
		[]switchError{{"duplicate case 1 in switch", "1"}},
	},
	{
		"every repeat reported against the first",
		inF("switch n {", "case 1, 1:", "case 1:", "}", "return 0"),
		[]switchError{{"duplicate case 1 in switch", "1"}, {"duplicate case 1 in switch", "1"}},
	},
	{
		"duplicate through arithmetic",
		inF("switch n {", "case 2, 1 + 1:", "}", "return 0"),
		[]switchError{{"duplicate case 2 in switch", "1 + 1"}},
	},
	{
		"duplicate through a known variable and len",
		inF("k := 3", "switch n {", "case k, len(\"abc\"):", "}", "return 0"),
		[]switchError{{"duplicate case 3 in switch", "len(\"abc\")"}},
	},
	{
		"duplicate character and integer",
		inF("var c uint8 = 1", "switch c {", "case 'a', 97:", "}", "return 0"),
		[]switchError{{"duplicate case 97 in switch", "97"}},
	},
	{
		"duplicate strings, raw and interpreted",
		inF("switch s {", "case \"a\\n\", `b`:", "case `a`, \"b\":", "}", "return 0"),
		[]switchError{{"duplicate case \"b\" in switch", "\"b\""}},
	},
	{
		"duplicate bools",
		inF("b := n > 0", "switch b {", "case true, false:", "case true:", "}", "return 0"),
		[]switchError{{"duplicate case true in switch", "true"}},
	},
	{
		"duplicate bool through constant expressions",
		inF("b := n > 0", "switch b {", "case 1 < 2:", "case !false:", "}", "return 0"),
		[]switchError{{"duplicate case true in switch", "!false"}},
	},
	{
		"duplicate bool through a known variable",
		inF(
			"k := 3",
			"b := n > 0",
			"switch b {",
			"case false, k == 3:",
			"case 1 == 1 && false:",
			"}",
			"return 0",
		),
		[]switchError{{"duplicate case false in switch", "1 == 1 && false"}},
	},
	{
		"duplicate after the default",
		inF("switch n {", "case 1:", "default:", "case 1:", "}", "return 0"),
		[]switchError{{"duplicate case 1 in switch", "1"}},
	},
	{
		"break outside a loop",
		inF("switch n {", "case 1:", "    break", "}", "return 0"),
		[]switchError{{"break is not in a loop", "break"}},
	},
	{
		"continue outside a loop",
		inF("switch {", "default:", "    continue", "}", "return 0"),
		[]switchError{{"continue is not in a loop", "continue"}},
	},
	{
		"a list true gives what every value agrees on",
		inF("switch {", "case d != 0, n > 0:", "    return n / d", "}", "return 0"),
		[]switchError{{"divisor d may be zero; check d != 0 first, or use q, ok := ... / d", "d"}},
	},
	{
		"an expression list true allows any of its values",
		inF("switch d {", "case 1, 0:", "    return n / d", "}", "return 0"),
		[]switchError{{"divisor d may be zero; check d != 0 first, or use q, ok := ... / d", "d"}},
	},
	{
		"default does not see a case true",
		inF("switch {", "case d != 0:", "default:", "    return n / d", "}", "return 0"),
		[]switchError{{"divisor d may be zero; check d != 0 first, or use q, ok := ... / d", "d"}},
	},
	{
		"later case sees earlier false",
		inF("switch {", "case d != 0:", "case n > 0:", "    return n / d", "}", "return 0"),
		[]switchError{{"divisor d may be zero; check d != 0 first, or use q, ok := ... / d", "d"}},
	},
	{
		"assigned in the case body",
		inF(
			"mut e := d",
			"switch {",
			"case e != 0:",
			"    e = 0",
			"    return n / e",
			"}",
			"return 0",
		),
		[]switchError{{"divisor e may be zero; check e != 0 first, or use q, ok := ... / e", "e"}},
	},
	{
		"assigned in a later case",
		inF(
			"mut e := d",
			"switch {",
			"case e == 0:",
			"    return 0",
			"case n > 0:",
			"    e = 0",
			"default:",
			"    return n / e",
			"}",
			"return 0",
		),
		[]switchError{{"divisor e may be zero; check e != 0 first, or use q, ok := ... / e", "e"}},
	},
	{
		"first case does not leave",
		inF("switch {", "case d == 0:", "    report()", "}", "return n / d"),
		[]switchError{{"divisor d may be zero; check d != 0 first, or use q, ok := ... / d", "d"}},
	},
	{
		"tag that is not a variable",
		inF("switch d + 0 {", "case 0:", "    return 0", "}", "return n / d"),
		[]switchError{{"divisor d may be zero; check d != 0 first, or use q, ok := ... / d", "d"}},
	},
	{
		"index beyond what the cases prove",
		inF("switch {", "case i < 0:", "    return 0", "default:", "    return a[i]", "}"),
		[]switchError{
			{
				"index i may be out of range; check 0 <= i && i < 4 first, or use v, ok := ...[i]",
				"i",
			},
		},
	},
}

// TestRejectsInvalidSwitches checks each problem with a switch is reported
// once, at the right place, and that a case proves no more than the if it
// stands for.
func TestRejectsInvalidSwitches(t *testing.T) {
	for _, tt := range rejectedSwitches {
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

// TestDuplicateCasePointsAtTheFirst checks a duplicate case's note points
// at the value it repeats.
func TestDuplicateCasePointsAtTheFirst(t *testing.T) {
	src := inF("switch n {", "case 7:", "case 8, 7:", "}", "return 0")
	_, diags := checkTree(t, src)
	if len(diags) != 1 || len(diags[0].Related) != 1 {
		t.Fatalf("got %v, want one diagnostic with one note", diags)
	}
	note := diags[0].Related[0]
	if note.Message != "previous case" || note.Span.Start != strings.Index(src, "7:") {
		t.Errorf("note = %q at %d, want %q at %d",
			note.Message, note.Span.Start, "previous case", strings.Index(src, "7:"))
	}
}

// switchTermination pairs bodies of f, which returns an int64, with
// whether they end in a terminating statement.
var switchTermination = []struct {
	name       string
	body       string
	terminates bool
}{
	{
		"every case returns, with a default",
		"switch n {\n    case 1:\n        return 1\n    default:\n        return 2\n    }",
		true,
	},
	{
		"default in the middle",
		"switch {\n    default:\n        return 2\n    case n > 0:\n        return 1\n    }",
		true,
	},
	{"only a default", "switch n {\n    default:\n        return 2\n    }", true},
	{"no default", "switch n {\n    case 1:\n        return 1\n    }", false},
	{"empty", "switch n {\n    }", false},
	{"an empty case", "switch n {\n    case 1:\n    default:\n        return 2\n    }", false},
	{"an empty default", "switch n {\n    case 1:\n        return 1\n    default:\n    }", false},
	{
		"nested",
		"switch n {\n    case 1:\n        switch {\n        default:\n            return 1\n        }\n    default:\n        return 2\n    }",
		true,
	},
	{
		"a case ending in a loop",
		"switch n {\n    case 1:\n        for {\n        }\n    default:\n        return 2\n    }",
		true,
	},
	{
		"break in a loop",
		"for {\n        switch {\n        case n > 0:\n            break\n        }\n    }",
		false,
	},
	{
		"break in a loop's default",
		"for {\n        switch n {\n        default:\n            break\n        }\n    }",
		false,
	},
	{
		"return in a loop",
		"for {\n        switch {\n        case n > 0:\n            return 1\n        }\n    }",
		true,
	},
	{
		"break in an inner loop",
		"for {\n        for {\n            switch {\n            case true:\n                break\n            }\n        }\n    }",
		true,
	},
	{"switch then return", "switch n {\n    }\n    return 0", true},
}

// TestSwitchTermination checks a switch terminates as the if chain it
// stands for does, and that a break in one leaves the enclosing loop.
func TestSwitchTermination(t *testing.T) {
	for _, tt := range switchTermination {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, inF(tt.body))
			if tt.terminates && len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
			if !tt.terminates && (len(diags) != 1 || diags[0].Message != "f must end in a return") {
				t.Errorf("got %v, want only %q", diags, "f must end in a return")
			}
		})
	}
}

// TestSettlesCaseValuesToTheTagsType checks an untyped constant case value
// takes the tag's type, and an untyped constant tag becomes an int64.
func TestSettlesCaseValuesToTheTagsType(t *testing.T) {
	file, diags := checkTree(
		t,
		inF(
			"var c uint8 = 1",
			"switch c {",
			"case 7:",
			"}",
			"switch 3 {",
			"case n:",
			"}",
			"return 0",
		),
	)
	if len(diags) > 0 {
		t.Fatalf("got %v", diags)
	}
	body := file.Decls[len(file.Decls)-2].(*ast.Func).Body
	if got := ast.TypeOf(body[1].(*ast.Switch).Cases[0].Values[0]); got != types.Uint8 {
		t.Errorf("case value type = %v, want uint8", got)
	}
	if got := ast.TypeOf(body[2].(*ast.Switch).Tag); got != types.Int64 {
		t.Errorf("tag type = %v, want int64", got)
	}
}

// TestHugeSwitchChecksQuickly checks a switch on a variable with thousands
// of cases, each giving a fact the rest must carry, is checked in steps
// linear in its cases (see checkGrowth): the facts read, the conditions
// whose facts are worked out, and the nodes whose writes are indexed,
// which a region of every later case walked for each case would make
// quadratic.
func TestHugeSwitchChecksQuickly(t *testing.T) {
	const n = 1250
	diags, small := checkWork(t, inF(hugeSwitch(n)...))
	if len(diags) > 0 {
		t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
	}
	_, grown := checkWork(t, inF(hugeSwitch(4*n)...))
	checkGrowth(t, "facts read", small.Scanned, grown.Scanned)
	checkGrowth(t, "conditions worked out", small.Conditions, grown.Conditions)
	checkGrowth(t, "nodes walked for writes", small.Walked, grown.Walked)
}

// hugeSwitch returns the lines of a switch on d with n cases, each
// dividing by d, which every case proves not zero.
func hugeSwitch(n int) []string {
	lines := make([]string, 0, 2*n+3)
	lines = append(lines, "switch d {")
	for i := range n {
		lines = append(lines, "case "+strconv.Itoa(i+1)+":", "    return n / d")
	}
	return append(lines, "}", "return 0")
}
