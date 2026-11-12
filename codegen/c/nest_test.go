package c_test

import (
	"fmt"
	"strings"
	"testing"
)

// nestLive is the C a nested types program reads c_allocator's count of
// live buffers through.
const nestLive = `#include <tack/tack.h>

#include "tack.h"

int64_t nest_live(void) {
    return tack_c_allocator_live_();
}
`

// nestTypes writes n types, each nesting the one before, named prefix and
// a number: the first is first, and each after it is made by next from
// its own name and the name of the one before.
func nestTypes(
	b *strings.Builder,
	prefix string,
	n int,
	first string,
	next func(name, inner string) string,
) {
	fmt.Fprintf(b, "type %s0 %s\n\n", prefix, first)
	for i := 1; i < n; i++ {
		name, inner := fmt.Sprintf("%s%d", prefix, i), fmt.Sprintf("%s%d", prefix, i-1)
		fmt.Fprintf(b, "type %s %s\n\n", name, next(name, inner))
	}
}

// nestStep is how many levels of a nested value each statement nestValue
// writes nests: far fewer than the parser's limit of a thousand levels,
// and enough that a value takes few statements.
const nestStep = 100

// nestValue returns statements, each indented by indent, that build a
// value of the outermost of n nested types, from first, a value of the
// innermost, each level i after it wrapped by wrap: nestStep levels a
// statement, each into a local of its own named name and a number, the
// next statement wrapping the local before; and the name of the last
// local, which holds the value. One literal nested a thousand levels deep
// would be past the parser's limit, though the types, each declared on
// its own, are not.
func nestValue(
	n int,
	name, indent, first string,
	wrap func(i int, inner string) string,
) (stmts, last string) {
	var b strings.Builder
	v, local := first, ""
	for i := 1; i <= n; i++ {
		if i == n || i%nestStep == 0 {
			local = fmt.Sprintf("%s%d", name, i/nestStep)
			fmt.Fprintf(&b, "%s%s := %s\n", indent, local, v)
			v = local
		}
		if i < n {
			v = wrap(i, v)
		}
	}
	return b.String(), local
}

// nestProgram returns a program whose types nest n levels deep, each in the
// one after: unions holding the union before as a payload, structs holding
// the struct before beside an int64, so each level is larger, structs
// holding an array of the struct before, and unions holding the union
// before where the innermost holds a list. It compares values of the
// outermost of each with == and !=, and lets values holding lists go out of
// scope, alone and as a list of them, checking c_allocator has nothing
// live after each. Each check prints its name and "ok", or "FAIL".
func nestProgram(n int) string {
	var b strings.Builder
	nestDecls(&b, n)
	nestFuncs(&b, n)
	return b.String()
}

// nestDecls writes nestProgram's package clause, its extern func and its
// check, and its four chains of n nested types: U, S, A and L.
func nestDecls(b *strings.Builder, n int) {
	b.WriteString("package main\n\nextern(\"nest_live\") func live() int64\n\n")
	b.WriteString("func check(cond bool, name string) {\n    print(name)\n    if cond {\n" +
		"        print(\" ok\\n\")\n    } else {\n        print(\" FAIL\\n\")\n    }\n}\n\n")
	nestTypes(b, "U", n, "union {\n    v int64\n    none\n}", func(_, inner string) string {
		return "union {\n    none\n    u " + inner + "\n}"
	})
	nestTypes(b, "S", n, "struct {\n    v int64\n}", func(_, inner string) string {
		return "struct {\n    s " + inner + "\n    x int64\n}"
	})
	nestTypes(b, "A", n, "struct {\n    v int64\n}", func(_, inner string) string {
		return "struct {\n    a [1]" + inner + "\n}"
	})
	nestTypes(b, "L", n, "union {\n    l list[int64]\n    none\n}", func(_, inner string) string {
		return "union {\n    none\n    u " + inner + "\n}"
	})
}

// nestFuncs writes nestProgram's functions: one per check, and main.
func nestFuncs(b *strings.Builder, n int) {
	last := n - 1
	pair := func(first func(v int) string, wrap func(i int, inner string) string) string {
		a, x := nestValue(n, "a", "    ", first(7), wrap)
		bs, y := nestValue(n, "b", "    ", first(8), wrap)
		return a + bs + fmt.Sprintf("    a := %s\n    b := %s\n", x, y)
	}
	unionPair := pair(func(v int) string { return fmt.Sprintf("U0.v(%d)", v) },
		func(i int, inner string) string { return fmt.Sprintf("U%d.u(%s)", i, inner) })
	structPair := pair(func(v int) string { return fmt.Sprintf("S0{v: %d}", v) },
		func(i int, inner string) string { return fmt.Sprintf("S%d{s: %s, x: 1}", i, inner) })
	arrayPair := pair(
		func(v int) string { return fmt.Sprintf("A0{v: %d}", v) },
		func(i int, inner string) string { return fmt.Sprintf("A%d{a: [1]A%d{%s}}", i, i-1, inner) },
	)
	holdingWrap := func(i int, inner string) string { return fmt.Sprintf("L%d.u(%s)", i, inner) }
	oneStmts, oneLast := nestValue(n, "h", "    ", "L0.l(l)", holdingWrap)
	manyStmts, manyLast := nestValue(n, "h", "        ", "L0.l(l)", holdingWrap)

	fmt.Fprintf(b, "func unions() {\n%s", unionPair)
	fmt.Fprintf(
		b,
		"    check(a == a && a != b && a != U%d.none, \"%d nested unions compare\")\n}\n\n",
		last,
		n,
	)
	fmt.Fprintf(b, "func structs() {\n%s", structPair)
	fmt.Fprintf(
		b,
		"    check(a == a && a != b, \"%d nested structs, each larger, compare\")\n}\n\n",
		n,
	)
	fmt.Fprintf(b, "func arrays() {\n%s", arrayPair)
	fmt.Fprintf(
		b,
		"    check(a == a && a != b, \"%d nested arrays of structs compare\")\n}\n\n",
		n,
	)
	fmt.Fprintf(b, "func one() {\n    mut l := make(list[int64], c_allocator)\n"+
		"    err := append(mut l, 1, 2, 3)\n    if err.code != 0 {\n        return\n    }\n"+
		"%s    x := %s\n    switch x {\n    case u:\n        print(\"\")\n    default:\n    }\n}\n\n",
		oneStmts, oneLast)
	fmt.Fprintf(
		b,
		"func many() {\n    mut all := make(list[L%d], c_allocator)\n    for range 3 {\n"+
			"        mut l := make(list[int64], c_allocator)\n        e1 := append(mut l, 1)\n"+
			"        if e1.code != 0 {\n            return\n        }\n"+
			"%s        e2 := append(mut all, %s)\n        if e2.code != 0 {\n            return\n        }\n"+
			"    }\n    check(len(all) == 3, \"a list of %d nested unions holding lists\")\n}\n\n",
		last,
		manyStmts,
		manyLast,
		n,
	)
	b.WriteString("func main() {\n    unions()\n    structs()\n    arrays()\n" +
		"    one()\n    check(live() == 0, \"a union nested holding a list is freed\")\n" +
		"    many()\n    check(live() == 0, \"a list of them is freed, with every list they hold\")\n}\n")
}

// TestThousandNestedTypes builds nestProgram's program, its types nested a
// thousand levels deep, at -O0, alone and with the address and undefined
// behavior sanitizers, every finding fatal, and checks every check
// passes: the helpers that compare and free a value of the outermost type
// call the helper of the type inside in a frame of one small size, so
// none runs out of stack, and every list a value held is freed. -O0 keeps
// every helper a call of its own, as deep as the types nest; clang also
// takes minutes to optimize values this deeply nested.
func TestThousandNestedTypes(t *testing.T) {
	src := nestProgram(1000)
	for _, flags := range [][]string{
		{"-O0"},
		{"-O0", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
	} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, nestLive, flags...), 6)
		})
	}
}
