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

// nestValue returns a value of the outermost of n nested types, built from
// first, a value of the innermost, each level i after it wrapped by wrap.
func nestValue(n int, first string, wrap func(i int, inner string) string) string {
	v := first
	for i := 1; i < n; i++ {
		v = wrap(i, v)
	}
	return v
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
	union := func(v int) string {
		return nestValue(n, fmt.Sprintf("U0.v(%d)", v), func(i int, inner string) string {
			return fmt.Sprintf("U%d.u(%s)", i, inner)
		})
	}
	strct := func(v int) string {
		return nestValue(n, fmt.Sprintf("S0{v: %d}", v), func(i int, inner string) string {
			return fmt.Sprintf("S%d{s: %s, x: 1}", i, inner)
		})
	}
	array := func(v int) string {
		return nestValue(n, fmt.Sprintf("A0{v: %d}", v), func(i int, inner string) string {
			return fmt.Sprintf("A%d{a: [1]A%d{%s}}", i, i-1, inner)
		})
	}
	holding := nestValue(n, "L0.l(l)", func(i int, inner string) string {
		return fmt.Sprintf("L%d.u(%s)", i, inner)
	})

	fmt.Fprintf(b, "func unions() {\n    a := %s\n    b := %s\n", union(7), union(8))
	fmt.Fprintf(
		b,
		"    check(a == a && a != b && a != U%d.none, \"%d nested unions compare\")\n}\n\n",
		last,
		n,
	)
	fmt.Fprintf(b, "func structs() {\n    a := %s\n    b := %s\n", strct(7), strct(8))
	fmt.Fprintf(
		b,
		"    check(a == a && a != b, \"%d nested structs, each larger, compare\")\n}\n\n",
		n,
	)
	fmt.Fprintf(b, "func arrays() {\n    a := %s\n    b := %s\n", array(7), array(8))
	fmt.Fprintf(
		b,
		"    check(a == a && a != b, \"%d nested arrays of structs compare\")\n}\n\n",
		n,
	)
	fmt.Fprintf(b, "func one() {\n    mut l := make(list[int64], c_allocator)\n"+
		"    err := append(mut l, 1, 2, 3)\n    if err.code != 0 {\n        return\n    }\n"+
		"    x := %s\n    switch x {\n    case u:\n        print(\"\")\n    default:\n    }\n}\n\n", holding)
	fmt.Fprintf(
		b,
		"func many() {\n    mut all := make(list[L%d], c_allocator)\n    for range 3 {\n"+
			"        mut l := make(list[int64], c_allocator)\n        e1 := append(mut l, 1)\n"+
			"        if e1.code != 0 {\n            return\n        }\n"+
			"        e2 := append(mut all, %s)\n        if e2.code != 0 {\n            return\n        }\n"+
			"    }\n    check(len(all) == 3, \"a list of %d nested unions holding lists\")\n}\n\n",
		last,
		holding,
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
		{"-O0", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"},
	} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, nestLive, flags...), 6)
		})
	}
}
