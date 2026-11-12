package typeck_test

import (
	"strings"
	"testing"
)

// ownedProgram returns a program declaring Any, an interface every type
// satisfies, Sizer, one asking for size, and Box, a struct holding a list
// that satisfies it, then decls, with a main function whose body is lines.
func ownedProgram(decls string, lines ...string) string {
	return "package main\n\ntype Any interface {\n}\n\ntype Sizer interface {\n    size() int64\n}\n\n" +
		"type Box struct {\n    items list[int64]\n}\n\nfunc (b Box) size() int64 {\n    return len(b.items)\n}\n\n" +
		decls + "\n\nfunc main() {\n    " + strings.Join(
		lines,
		"\n    ",
	) + "\n}\n"
}

// TestAcceptsOwnedTypeParams checks what a type parameter, always owned,
// allows: lists, views, map values and function types of it in a
// signature and a body, a result of it, moves through pop, remove and
// append, swap, a has proof on a map of it, deferred code, and type
// arguments that are no struct, int64, a list, a function type and error
// among them, for a constraint that asks for no method.
func TestAcceptsOwnedTypeParams(t *testing.T) {
	for _, tt := range []struct{ name, src string }{
		{"a list of T moved between", ownedProgram(
			"func move[T Any](mut a list[T], mut b list[T]) error {\n    if len(a) > 0 {\n"+
				"        return append(mut b, pop(mut a))\n    }\n    return noerror\n}",
			"mut a := make(list[int64], c_allocator)", "mut b := make(list[int64], c_allocator)",
			"err := move(mut a, mut b)", "if err.code != 0 {", "}")},
		{"a view of T and a function of T", ownedProgram(
			"func count[T Any](v []T, f func(T) bool) int64 {\n    mut n := 0\n    for _, x := range v {\n"+
				"        if f(x) {\n            n +%= 1\n        }\n    }\n    return n\n}\n\n"+
				"func big(b Box) bool {\n    return b.size() > 1\n}",
			"l := make(list[Box], c_allocator)", "if count(l, big) > 0 {", "}")},
		{"a result of T made by a function value", ownedProgram(
			"func make1[T Any](mut l list[T], mk func() T) T {\n    if len(l) > 0 {\n        return pop(mut l)\n"+
				"    }\n    return mk()\n}\n\nfunc box() Box {\n    return Box{items: make(list[int64], c_allocator)}\n}",
			"mut l := make(list[Box], c_allocator)", "b := make1(mut l, box)", "if b.size() > 0 {", "}")},
		{"a map of T values under has", ownedProgram(
			"func sized[T Sizer](m map[string]T, k string) int64 {\n    if has(m, k) {\n        return m[k].size()\n"+
				"    }\n    return 0\n}",
			"m := make(map[string]Box, c_allocator)", "if sized(m, \"a\") > 0 {", "}")},
		{"swap keeping a proof", ownedProgram(
			"func turn[T Any](mut l list[T]) {\n    if len(l) > 2 {\n        swap(mut l, 0, 1)\n"+
				"        swap(mut l, 1, 2)\n        swap(mut l, 2, 2)\n    }\n}",
			"mut l := make(list[list[int64]], c_allocator)", "turn(mut l)")},
		{"deferred code dropping a T", ownedProgram(
			"func drain[T Any](mut l list[T]) {\n    defer {\n        pop(mut l)\n    }\n    remove(mut l, 0)\n}",
			"mut l := make(list[Box], c_allocator)", "drain(mut l)")},
		{"type arguments that are no struct", ownedProgram(
			"func turn[T Any](mut l list[T]) {\n    if len(l) > 1 {\n        swap(mut l, 0, 1)\n    }\n}\n\n"+
				"func one[E Any](e E) int64 {\n    return 1\n}\n\nfunc twice(x int64) int64 {\n    return x *% 2\n}",
			"mut a := make(list[int64], c_allocator)", "mut b := make(list[list[int64]], c_allocator)",
			"mut c := make(list[func(int64) int64], c_allocator)", "mut d := make(list[map[string]Box], c_allocator)",
			"turn(mut a)", "turn(mut b)", "turn(mut c)", "turn(mut d)",
			"if one(noerror) +% one(twice) +% one(c_allocator) > 0 {", "}")},
		{"a type parameter passed on whole", ownedProgram(
			"func inner[U Any](mut l list[U]) int64 {\n    return len(l)\n}\n\n"+
				"func outer[T Any](mut l list[T]) int64 {\n    return inner(mut l)\n}",
			"mut l := make(list[Box], c_allocator)", "if outer(mut l) > 0 {", "}")},
		{"a result of T among several", ownedProgram(
			"func split[T Any](mut l list[T], mk func() T) (T, int64) {\n    if len(l) > 0 {\n"+
				"        return pop(mut l), len(l)\n    }\n    return mk(), 0\n}\n\n"+
				"func box() Box {\n    return Box{items: make(list[int64], c_allocator)}\n}",
			"mut l := make(list[Box], c_allocator)", "b, n := split(mut l, box)", "if b.size() +% n > 0 {", "}")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if diags := check(t, tt.src); len(diags) > 0 {
				t.Fatalf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// TestRejectsOwnedTypeParams checks that a value of a type parameter is
// never copied, even when a call would make it a copyable type: it cannot
// be moved out of a parameter or a list's element, used after a move,
// dropped, compared, passed as any or read before the ok of the pop that
// gave it, and swap needs both of its indexes proven, and cannot run
// while its list is ranged over.
func TestRejectsOwnedTypeParams(t *testing.T) {
	for _, tt := range []struct{ name, decls, want string }{
		{
			"a parameter moved", "func same[T Any](x T) T {\n    return x\n}",
			"cannot move x: it is a parameter, which borrows its caller's value",
		},
		{
			"an element moved out", "func first[T Any](l list[T]) T {\n    if len(l) > 0 {\n        return l[0]\n    }\n    for {\n    }\n}",
			"cannot move l[0] out of l: an element cannot be moved out of a list; take it out with pop or remove",
		},
		{
			"a use after a move", "func twice[T Any](mut l list[T]) error {\n    if len(l) > 0 {\n        x := pop(mut l)\n" +
				"        e1 := append(mut l, x)\n        e2 := append(mut l, x)\n        if e1.code != 0 {\n            return e2\n" +
				"        }\n    }\n    return noerror\n}",
			"x is used after it was moved",
		},
		{
			"a dropped T", "func get[T Any](mut l list[T], mk func() T) T {\n    if len(l) > 0 {\n        return pop(mut l)\n" +
				"    }\n    return mk()\n}\n\nfunc toss[T Any](mut l list[T], mk func() T) {\n    get(mut l, mk)\n}",
			"get returns T, which may hold a list and cannot be discarded; bind it to a variable",
		},
		{"a T compared", "func same[T Any](a T, b T) bool {\n    return a == b\n}", "operator == is not defined on T"},
		{
			"a T as any", "func show(v any) {\n}\n\nfunc pass[T Any](x T) {\n    show(x)\n}",
			"cannot pass T as any: only the builtin types and views of them can be passed as any for now",
		},
		{
			"a popped T read before ok", "func last[T Any](mut l list[T], mut out list[T]) error {\n    x, ok := pop(mut l)\n" +
				"    err := append(mut out, x)\n    if ok {\n        return err\n    }\n    return err\n}",
			"x cannot be read before ok is checked; check that ok is true first",
		},
		{
			"swap of an unproven index", "func flip[T Any](mut l list[T], i int64) {\n    if len(l) > 1 {\n        swap(mut l, 0, i)\n    }\n}",
			"index i may be out of range; check 0 <= i && i < len(l) first",
		},
		{
			"swap in a range over its list", "func flip[T Any](mut l list[T]) {\n    for range l {\n        if len(l) > 1 {\n" +
				"            swap(mut l, 0, 1)\n        }\n    }\n}",
			"l is being ranged over, so this loop cannot pass mut l",
		},
		{
			"a map keyed by T", "func keys[T Any](m map[T]int64) int64 {\n    return len(m)\n}",
			"cannot use T as a map key: a map's key cannot be a type parameter",
		},
		{
			"an array of T", "func arr[T Any](a [2]T) int64 {\n    return 2\n}",
			"an array cannot hold T: its elements are copied, and T, a type parameter, which may hold a list, cannot be",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			diags := check(t, ownedProgram(tt.decls))
			if len(diags) == 0 || diags[0].Message != tt.want {
				t.Fatalf("got %v, want %q first", diags, tt.want)
			}
		})
	}
}
