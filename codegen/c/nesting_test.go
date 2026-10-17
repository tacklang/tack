package c_test

import (
	"fmt"
	"strings"
	"testing"
)

// limitDepth is the parser's nesting limit: every shape limitProgram
// writes nests exactly this deep, the deepest program the compiler takes.
const limitDepth = 1000

// limitProgram writes a function for each construct that nests, each
// nested limitDepth levels deep, counting from its body, and checks what
// each returns: parentheses, ! and -, calls, indexes, && and || nested on
// the right, ifs, fors and switches, an array type, a list type, an array
// literal and a struct literal, and half of the levels ifs around half
// of them a right-nested && in a condition, which count together.
func limitProgram() string {
	n := limitDepth
	var b strings.Builder
	b.WriteString("package main\n\nvar t = [1]int64{0}\n\n")
	b.WriteString("func f(v int64) int64 {\n    return v\n}\n\n")
	b.WriteString("func check(cond bool, name string) {\n    print(name)\n    if cond {\n" +
		"        print(\" ok\\n\")\n    } else {\n        print(\" FAIL\\n\")\n    }\n}\n\n")
	b.WriteString("type S0 struct {\n    v int64\n}\n\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&b, "type S%d struct {\n    s S%d\n}\n\n", i, i-1)
	}
	fns := limitFuncs(n)
	for _, fn := range fns {
		fmt.Fprintf(&b, "func %s%s {\n%s}\n\n", fn.name, fn.sig, fn.body)
	}
	b.WriteString("func main() {\n")
	for _, c := range limitCheckList {
		fmt.Fprintf(&b, "    check(%s, %q)\n", c.cond, c.name)
	}
	b.WriteString("}\n")
	return b.String()
}

// limitFuncs returns the name, signature and body of each function
// limitProgram writes, every one nested n levels deep.
func limitFuncs(n int) []struct{ name, sig, body string } {
	ret := func(open, mid, closing string) string {
		return "    return " + wrap(open, mid, closing, n) + "\n"
	}
	blocks := func(open, mid string) string {
		return "    mut r int64 = 0\n" + wrap(open, mid, "    }\n", n) + "    return r\n"
	}
	return []struct{ name, sig, body string }{
		{"parens", "(x int64) int64", ret("(", "x", ")")},
		{"nots", "(b bool) bool", ret("!", "b", "")},
		{"minuses", "(x int64) int64", ret("- ", "x", "")},
		{"calls", "(x int64) int64", ret("f(", "x", ")")},
		{"indexes", "() int64", ret("t[", "0", "]")},
		{"ands", "(b bool) bool", ret("b && (", "b", ")")},
		{"ors", "(b bool) bool", ret("b || (", "b", ")")},
		{"ifs", "(x int64) int64", blocks("    if x > 0 {\n", "    r = x\n")},
		{"fors", "() int64", blocks("    for range 1 {\n", "    r++\n")},
		{"switches", "(x int64) int64", blocks("    switch x {\n    case 1:\n", "    r = 7\n")},
		{
			"array_type",
			"() int64",
			"    mut a " + strings.Repeat("[1]", n) + "int64\n    return len(a)\n",
		},
		{
			"list_type",
			"() int64",
			"    l := make(" + wrap(
				"list[",
				"int64",
				"]",
				n-1,
			) + ", c_allocator)\n    return len(l)\n",
		},
		{
			"array_literal",
			"() int64",
			"    a := " + strings.Repeat("[1]", n) + "int64" + wrap("{", "5", "}", n) +
				"\n    return a" + strings.Repeat("[0]", n) + "\n",
		},
		{
			"struct_literal", "() int64",
			"    s := " + structLit(n) + "\n    return s" + strings.Repeat(".s", n-1) + ".v\n",
		},
		{"blocks_and_ands", "(b bool) int64", "    mut r int64 = 0\n" +
			wrap(
				"    if b {\n",
				"    if "+wrap("b && (", "b", ")", n/2-1)+" {\n        r = 3\n    }\n",
				"    }\n",
				n/2,
			) +
			"    return r\n"},
	}
}

// wrap returns mid inside n copies of open and n of closing.
func wrap(open, mid, closing string, n int) string {
	return strings.Repeat(open, n) + mid + strings.Repeat(closing, n)
}

// limitCheckList are the checks limitProgram's main makes, each a
// condition on what a function returns and its name.
var limitCheckList = []struct{ cond, name string }{
	{"parens(4) == 4", "parentheses"},
	{"nots(true)", "nots"},
	{"minuses(4) == 4", "minuses"},
	{"calls(4) == 4", "calls"},
	{"indexes() == 0", "indexes"},
	{"ands(true) && !ands(false)", "right-nested &&"},
	{"ors(true) && !ors(false)", "right-nested ||"},
	{"ifs(9) == 9 && ifs(-1) == 0", "ifs"},
	{"fors() == 1", "fors"},
	{"switches(1) == 7 && switches(2) == 0", "switches"},
	{"array_type() == 1", "an array type"},
	{"list_type() == 0", "a list type"},
	{"array_literal() == 5", "an array literal"},
	{"struct_literal() == 7", "a struct literal"},
	{"blocks_and_ands(true) == 3 && blocks_and_ands(false) == 0", "blocks around &&"},
}

// structLit returns a struct literal of S_{n-1}, holding S_{n-2} and so on
// to S0, n levels of braces.
func structLit(n int) string {
	lit := "S0{v: 7}"
	for i := 1; i < n; i++ {
		lit = fmt.Sprintf("S%d{s: %s}", i, lit)
	}
	return lit
}

// limitChecks is how many checks limitProgram prints.
const limitChecks = 15

// TestDeepestProgramsRun builds limitProgram's program, every construct
// nested as deep as the parser takes, at -O0 and at -O0 with the address
// and undefined behavior sanitizers, every finding fatal, and checks
// every check passes: whatever the parser accepts, clang compiles, and
// it runs.
func TestDeepestProgramsRun(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a large program twice")
	}
	prog := compileWithStd(t, limitProgram())
	for _, flags := range [][]string{
		{"-O0"},
		{"-O0", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"},
	} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			runChecks(t, buildProgram(t, prog, "", flags...), limitChecks)
		})
	}
}
