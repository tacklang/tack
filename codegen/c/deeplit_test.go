package c_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/internal/stdtest"
)

// deepLiteralLevels is how deeply deepLiteralProgram nests its literals,
// the parser's limit: past where clang ran out of stack on the C the
// backend wrote before it held deep literals in temporaries, about 950
// levels of array literals in a function and 400 in a global's
// initializer. A literal whose innermost level holds a call, which is a
// level too, has one literal level fewer, and one alternating struct and
// array literals, two levels each, about half as many of each.
const deepLiteralLevels = 1000

// deepLiteralProgram writes a program that builds literals nested n
// levels deep, in a function and as globals' initializers, and checks
// what they hold, each check printing its name and ok or FAIL:
//
//   - an array literal of arrays of arrays, read back at its innermost
//     element;
//   - a struct literal of structs, each a level deeper, with a field
//     beside each, whose values are taken from a counter as they are
//     evaluated, so the checks show Tack's order, left to right, is kept
//     through every temporary;
//   - a literal alternating struct and array levels;
//   - a union holding a union, built case by case;
//   - as a global, a struct of structs whose innermost holds a string, a
//     bool, a zero int64 left unset and a union of its second case.
func deepLiteralProgram(n int) string {
	var b strings.Builder
	b.WriteString("package main\n\n")
	b.WriteString("mut counter int64\n\n")
	b.WriteString("func tick() int64 {\n    counter +%= 1\n    return counter\n}\n\n")
	b.WriteString("func check(cond bool, name string) {\n    print(name)\n    if cond {\n" +
		"        print(\" ok\\n\")\n    } else {\n        print(\" FAIL\\n\")\n    }\n}\n\n")
	writeDeepTypes(&b, n)
	v := deepValues(n)
	fmt.Fprintf(&b, "var garray = %s\n\n", v.array)
	fmt.Fprintf(&b, "var gmixed = %s\n\n", v.mixed)
	fmt.Fprintf(&b, "var gunion = %s\n\n", v.union)
	fmt.Fprintf(&b, "var gdeep = %s\n\n", v.global)
	writeDeepMain(&b, n, v)
	return b.String()
}

// writeDeepTypes writes the types deepLiteralProgram's literals have, n
// levels of each: T_i holding a, T_{i-1} and b, in that order; M_i
// holding an array of one M_{i-1}; U_i whose second case holds U_{i-1};
// and G_i holding G_{i-1}, the innermost a string, a bool, an int64 and
// a union.
func writeDeepTypes(b *strings.Builder, n int) {
	b.WriteString("type T0 struct {\n    v int64\n}\n\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(b, "type T%d struct {\n    a int64\n    s T%d\n    b int64\n}\n\n", i, i-1)
	}
	b.WriteString("type M0 struct {\n    v int64\n}\n\n")
	for i := 1; i <= n/2; i++ {
		fmt.Fprintf(b, "type M%d struct {\n    a [1]M%d\n}\n\n", i, i-1)
	}
	b.WriteString("type U0 union {\n    v int64\n    none\n}\n\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(b, "type U%d union {\n    none\n    u U%d\n}\n\n", i, i-1)
	}
	b.WriteString("type Pick union {\n    none\n    word string\n}\n\n")
	b.WriteString("type G0 struct {\n    name string\n    on bool\n" +
		"    zero int64\n    pick Pick\n}\n\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(b, "type G%d struct {\n    g G%d\n}\n\n", i, i-1)
	}
}

// deepLiterals are deepLiteralProgram's literals, each n levels deep, and
// the expressions that read back their innermost values.
type deepLiterals struct {
	array, mixed, union, global, ordered string
	arrayRead, mixedRead, globalRead     string
	orderedRead                          string
}

// deepValues writes deepLiteralProgram's literals, n levels deep.
func deepValues(n int) deepLiterals {
	v := deepLiterals{
		array: strings.Repeat(
			"[1]",
			n,
		) + "int64" + strings.Repeat(
			"{",
			n,
		) + "7" + strings.Repeat(
			"}",
			n,
		),
		arrayRead:   strings.Repeat("[0]", n),
		mixed:       "M0{v: 9}",
		mixedRead:   strings.Repeat(".a[0]", (n-1)/2) + ".v",
		union:       "U0.v(5)",
		global:      `G0{name: "deep", on: true, zero: 0, pick: Pick.word("leaf")}`,
		globalRead:  strings.Repeat(".g", n-2),
		ordered:     "T0{v: tick()}",
		orderedRead: strings.Repeat(".s", n-2) + ".v",
	}
	for i := 1; i <= (n-1)/2; i++ {
		v.mixed = fmt.Sprintf("M%d{a: [1]M%d{%s}}", i, i-1, v.mixed)
	}
	for i := 1; i < n; i++ {
		v.union = fmt.Sprintf("U%d.u(%s)", i, v.union)
	}
	for i := 1; i < n-1; i++ {
		v.global = fmt.Sprintf("G%d{g: %s}", i, v.global)
		v.ordered = fmt.Sprintf("T%d{a: tick(), s: %s, b: tick()}", i, v.ordered)
	}
	return v
}

// writeDeepMain writes deepLiteralProgram's main, which builds the
// literals of v in a function and checks them and the globals.
func writeDeepMain(b *strings.Builder, n int, v deepLiterals) {
	b.WriteString("func main() {\n")
	fmt.Fprintf(b, "    a := %s\n", v.array)
	fmt.Fprintf(b, "    check(a%s == 7, \"an array literal\")\n", v.arrayRead)
	fmt.Fprintf(b, "    t := %s\n", v.ordered)
	fmt.Fprintf(
		b,
		"    check(t.a == 1 && t%s == %d && t.b == %d, \"a struct literal, in order\")\n",
		v.orderedRead,
		n-1,
		2*(n-1)-1,
	)
	fmt.Fprintf(b, "    m := %s\n", v.mixed)
	fmt.Fprintf(b, "    check(m%s == 9, \"structs and arrays\")\n", v.mixedRead)
	fmt.Fprintf(b, "    u := %s\n", v.union)
	b.WriteString("    switch u {\n    case u:\n        check(true, \"a union literal\")\n" +
		"    default:\n        check(false, \"a union literal\")\n    }\n")
	fmt.Fprintf(b, "    check(garray%s == 7, \"a global array\")\n", v.arrayRead)
	fmt.Fprintf(b, "    check(gmixed%s == 9, \"a global of structs and arrays\")\n", v.mixedRead)
	b.WriteString("    switch gunion {\n    case u:\n        check(true, \"a global union\")\n" +
		"    default:\n        check(false, \"a global union\")\n    }\n")
	fmt.Fprintf(b, "    leaf := gdeep%s\n", v.globalRead)
	b.WriteString(
		"    check(leaf.name == \"deep\" && leaf.on && leaf.zero == 0, \"a global's leaves\")\n",
	)
	b.WriteString(
		"    switch leaf.pick {\n    case word(w):\n        check(w == \"leaf\", \"a global's union leaf\")\n" +
			"    default:\n        check(false, \"a global's union leaf\")\n    }\n",
	)
	b.WriteString("}\n")
}

// deepLiteralChecks is how many checks deepLiteralProgram prints.
const deepLiteralChecks = 9

// TestDeepLiteralsRun builds deepLiteralProgram's program at
// deepLiteralLevels at -O0, and at -O0 with the address and undefined
// behavior sanitizers, every finding fatal, and checks every check
// passes: clang takes every literal and every global's value however
// deep, and they hold what Tack wrote, evaluated in Tack's order. It is
// not built optimized, which takes clang minutes on a program this
// size, though no longer than the same shallow code would.
func TestDeepLiteralsRun(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a large program three times")
	}
	prog := compileWithStd(t, deepLiteralProgram(deepLiteralLevels))
	for _, flags := range [][]string{
		{"-O0"},
		{"-O0", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
	} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			runChecks(t, buildProgram(t, prog, "", flags...), deepLiteralChecks)
		})
	}
}

// TestDeepLiteralsStayShallow checks the C written for
// deepLiteralProgram's program nests no expression and no initializer
// deeper than a bound well within what clang takes, braces included,
// while its literals nest a thousand levels deep.
func TestDeepLiteralsStayShallow(t *testing.T) {
	prog := compileWithStd(t, deepLiteralProgram(deepLiteralLevels))
	out, diags := c.New().Generate(prog, stdtest.MustBuiltins())
	if len(diags) > 0 {
		t.Fatalf("generate reported %+v", diags)
	}
	if depth, line := deepestBraces(string(out)); depth > 64 {
		t.Errorf("C nests %d levels deep, braces included, at line %d", depth, line)
	}
}

// deepestBraces returns how deeply src, C, nests its parentheses,
// brackets and braces, the deepest, and the line it is on.
func deepestBraces(src string) (deepest, line int) {
	depth, at := 0, 1
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '(', '[', '{':
			depth++
			if depth > deepest {
				deepest, line = depth, at
			}
		case ')', ']', '}':
			depth--
		case '\n':
			at++
		case '"', '\'':
			q := src[i]
			for i++; i < len(src) && src[i] != q; i++ {
				if src[i] == '\\' {
					i++
				}
			}
		}
	}
	return deepest, line
}
