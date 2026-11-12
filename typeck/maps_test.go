package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// mapProgram returns a program declaring Bag, a struct holding a list,
// with a function f, taking params, whose body is lines.
func mapProgram(params string, lines ...string) string {
	return "package main\n\ntype Bag struct {\n    items list[int64]\n}\n\nfunc f(" + params + ") {\n    " +
		strings.Join(
			lines,
			"\n    ",
		) + "\n}\n\nfunc main() {\n}\n"
}

// TestAcceptsMaps checks every map operation the checker allows: make and
// len, put of copyable and owned keys and values, the checked lookup of a
// copyable value, delete checked and as a statement, every range form,
// maps nested in maps, lists and structs, and comparable composite keys.
func TestAcceptsMaps(t *testing.T) {
	for _, tt := range []struct{ name, src string }{
		{"make, put, len and lookup", mapProgram("",
			"mut m := make(map[string]int64, c_allocator)", "err := put(mut m, \"a\", 1)",
			"v, ok := m[\"a\"]", "if err.code != 0 || !ok || v != 1 || len(m) != 1 {", "}")},
		{"owned values and delete", mapProgram("",
			"mut m := make(map[int64]Bag, c_allocator)", "err := put(mut m, 1, Bag{items: make(list[int64], c_allocator)})",
			"b, ok := delete(mut m, 1)", "if err.code != 0 || ok && len(b.items) > 0 {", "}", "delete(mut m, 2)")},
		{"every range form", mapProgram("m map[int64]list[int64]",
			"mut n := 0", "for k, v := range m {", "    n +%= k +% len(v)", "}", "for k := range m {", "    n +%= k", "}",
			"for _, v := range m {", "    n +%= len(v)", "}", "if n > 0 {", "}")},
		{"owned keys in place", mapProgram("m map[list[int64]]int64",
			"mut n := 0", "for k, v := range m {", "    n +%= len(k) +% v", "}", "if n > 0 {", "}")},
		{"composite keys", mapProgram("m map[[2]int64]bool, n map[Bag]int64",
			"v, ok := m[[2]int64{1, 2}]", "if ok && v && len(n) == 0 {", "}")},
		{"maps nested", mapProgram("",
			"mut outer := make(map[string]map[string]int64, c_allocator)",
			"inner := make(map[string]int64, c_allocator)", "err := put(mut outer, \"x\", inner)",
			"mut ms := make(list[map[int64]int64], c_allocator)", "e := append(mut ms, make(map[int64]int64, c_allocator))",
			"if err.code != 0 || e.code != 0 {", "}")},
		{"a put of another map in a range", mapProgram("m map[int64]int64, mut n map[int64]int64",
			"for k, v := range m {", "    err := put(mut n, k, v)", "    if err.code != 0 {", "    }", "}")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if diags := check(t, tt.src); len(diags) > 0 {
				t.Fatalf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// TestRejectsMaps checks the map errors: a key without ==, an error key,
// a lookup or a write without the checked form, a checked lookup of an
// owned value, a delete bound to one name, a dropped put, a range that
// changes its map, a map that holds itself, and a map crossing to C.
func TestRejectsMaps(t *testing.T) {
	for _, tt := range []struct{ name, src, want string }{
		{
			"a key without ==", mapProgram("m map[map[int64]int64]int64"),
			"cannot use map[int64]int64 as a map key: it has no ==",
		},
		{"an error key", mapProgram("m map[error]int64"), "cannot use error as a map key: an error is not a key"},
		{
			"a plain lookup", mapProgram("m map[int64]int64, k int64", "x := m[k]", "if x == 0 {", "}"),
			"key k may not be in m; check has(m, k) first, or use v, ok := m[k]",
		},
		{"a write", mapProgram("mut m map[int64]int64, k int64", "m[k] = 1"), "key k may not be in m; check has(m, k) first, or use put(mut m, k, ...)"},
		{
			"an owned value looked up", mapProgram("m map[int64]Bag", "b, ok := m[1]", "if ok && len(b.items) > 0 {", "}"),
			"the checked form cannot read a value of type Bag: it would copy it out of m; " +
				"check has(m, 1) first and use m[1] in place",
		},
		{
			"delete bound to one name", mapProgram("mut m map[int64]int64", "x := delete(mut m, 1)", "if x == 0 {", "}"),
			"delete gives the value and whether the key was there; use v, ok := delete(mut m, 1)",
		},
		{
			"a dropped put", mapProgram("mut m map[int64]int64", "put(mut m, 1, 2)"),
			"the error put returns cannot be discarded; bind it, as in err := put(...)",
		},
		{
			"a range changing its map", mapProgram("mut m map[int64]int64", "for k := range m {", "    delete(mut m, k)", "}"),
			"m is being ranged over, so this loop cannot pass mut m",
		},
		{
			"a map holding itself", "package main\n\ntype Node struct {\n    kids map[string]Node\n}\n\nfunc main() {\n}\n",
			"invalid recursive type Node: a type cannot hold itself, even through a map",
		},
		{
			"a map crossing to C", "package main\n\nextern(\"f\") func f(m map[int64]int64)\n\nfunc main() {\n}\n",
			"parameter m of extern func f cannot be map[int64]int64: a map does not cross to C",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			diags := check(t, tt.src)
			if len(diags) != 1 || diags[0].Message != tt.want {
				t.Fatalf("got %v, want only %q", diags, tt.want)
			}
		})
	}
}

// TestMapTypes checks the type a range over a map gives its key: the
// map's key type.
func TestMapTypes(t *testing.T) {
	file, diags := checkTree(
		t,
		mapProgram(
			"m map[string]list[int64]",
			"for k := range m {",
			"    if len(k) > 0 {",
			"    }",
			"}",
		),
	)
	if len(diags) > 0 {
		t.Fatalf("got %v", diags)
	}
	fn := file.Decls[1].(*ast.Func)
	loop := fn.Body[0].(*ast.ForRange)
	if loop.Var.Type != types.String {
		t.Errorf("key type = %v, want string", loop.Var.Type)
	}
}

// TestAcceptsHas checks what a has guard proves: a read, a field read, an
// assignment, a compound one, a mut argument, an append, a mut method and
// a replace on m[k], under an if, an early exit, the right of && and ||,
// and a loop's condition; a constant key proven by the same constant or
// an immutable name holding it; a key that is a field of a parameter; a
// write to another key's value, which changes no key; and a proof again
// after a put.
func TestAcceptsHas(t *testing.T) {
	for _, tt := range []struct{ name, src string }{
		{"an if", mapProgram("mut m map[int64]int64, k int64",
			"if has(m, k) {", "    m[k] = m[k] +% 1", "    m[k] +%= 1", "}")},
		{"an early exit", mapProgram("m map[int64]Bag, k int64",
			"if !has(m, k) {", "    return", "}", "if len(m[k].items) > 0 {", "}")},
		{"&& and ||", mapProgram("m map[string]int64, k string",
			"if has(m, k) && m[k] > 0 || !has(m, k) || m[k] < 0 {", "}")},
		{"a loop's condition", mapProgram("mut m map[int64]int64, k int64",
			"for has(m, k) {", "    m[k] -%= 1", "    delete(mut m, k)", "}")},
		{"owned values in place", mapProgram("mut m map[int64]list[int64], k int64",
			"if has(m, k) {", "    err := append(mut m[k], 1)", "    old := replace(mut m[k], make(list[int64], c_allocator))",
			"    m[k] = old", "    if err.code != 0 || len(m[k]) > 0 {", "    }", "}")},
		{"a mut method", mapProgram("mut m map[int64]Bag",
			"if has(m, 1) {", "    err := mut m[1].add(2)", "    if err.code != 0 {", "    }", "}")},
		{"a constant key by an immutable name", mapProgram("mut m map[int64]int64",
			"one := 1", "if has(m, one) {", "    m[1] = m[one]", "}")},
		{"a field of a parameter as the key", mapProgram("m map[int64]int64, p Pair",
			"if has(m, p.n) && m[p.n] > 0 {", "}")},
		{"another key's value written", mapProgram("mut m map[int64]int64, k int64, j int64",
			"if has(m, k) && has(m, j) {", "    m[j] = 1", "    m[k] = 2", "}")},
		{"a proof again after a put", mapProgram("mut m map[int64]int64, k int64",
			"err := put(mut m, k, 1)", "if err.code == 0 && has(m, k) {", "    m[k] +%= 1", "}")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := strings.Replace(tt.src, "func main()", hasHelpers+"func main()", 1)
			if diags := check(t, src); len(diags) > 0 {
				t.Fatalf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// hasHelpers declares what TestAcceptsHas and TestRejectsHas use beside
// mapProgram's Bag: a mut method on Bag, a struct with an int64 field, and
// a function that changes a map.
const hasHelpers = `func (mut b Bag) add(x int64) error {
    return append(mut b.items, x)
}

type Pair struct {
    n int64
}

func gone(mut m map[int64]int64) int64 {
    delete(mut m, 1)
    return 0
}

`

// TestRejectsHas checks what a has guard does not prove: a key used after
// a write to its map: a put, a delete of any key, a mut argument or an
// assignment of the map, between them, or after them through a loop's back
// edge or in deferred code; a key that can change; a map that is not a
// variable or a field of an immutable one; an assignment whose value
// changes the map; and a has stored in a variable. It checks has's own
// errors too.
func TestRejectsHas(t *testing.T) {
	const unproven = "key k may not be in m; check has(m, k) first, or use v, ok := m[k]"
	for _, tt := range []struct{ name, src, want string }{
		{"a put between", mapProgram("mut m map[int64]int64, k int64",
			"if has(m, k) {", "    err := put(mut m, 2, 2)", "    if err.code != 0 || m[k] > 0 {", "    }", "}"), unproven},
		{"a delete of another key", mapProgram("mut m map[int64]int64, k int64, j int64",
			"if has(m, k) {", "    delete(mut m, j)", "    if m[k] > 0 {", "    }", "}"), unproven},
		{"a mut argument", mapProgram("mut m map[int64]int64, k int64",
			"if has(m, k) {", "    x := gone(mut m)", "    if m[k] > x {", "    }", "}"), unproven},
		{"the map assigned", mapProgram("mut m map[int64]int64, k int64, n map[int64]int64",
			"if has(m, k) {", "    m = n", "    if m[k] > 0 {", "    }", "}"), unproven},
		{
			"a back edge", mapProgram("mut m map[int64]int64, k int64",
				"if has(m, k) {", "    for i := range 2 {", "        if m[k] > i {", "        }", "        delete(mut m, k)", "    }", "}"),
			unproven,
		},
		{"a defer", mapProgram("mut m map[int64]int64, k int64",
			"if has(m, k) {", "    defer {", "        if m[k] > 0 {", "        }", "    }", "    delete(mut m, k)", "}"), unproven},
		{"a mutable key", mapProgram("m map[int64]int64, mut k int64",
			"if has(m, k) && m[k] > 0 {", "}"), unproven},
		{"the key outside the guard", mapProgram("m map[int64]int64, k int64",
			"if !has(m, k) {", "    if m[k] > 0 {", "    }", "}"), unproven},
		{"a has stored", mapProgram("m map[int64]int64, k int64",
			"ok := has(m, k)", "if ok && m[k] > 0 {", "}"), unproven},
		{
			"a map in a mutable map", mapProgram("mut m map[int64]map[int64]int64",
				"if has(m, 1) && has(m[1], 2) && m[1][2] > 0 {", "}"),
			"key 2 may not be in m[1]; check has(m[1], 2) first, or use v, ok := m[1][2]",
		},
		{"an owned value unproven", mapProgram("m map[int64]Bag, k int64",
			"if len(m[k].items) > 0 {", "}"), "key k may not be in m; check has(m, k) first"},
		{
			"a value changing its map", mapProgram("mut m map[int64]int64, k int64",
				"if has(m, k) {", "    m[k] = gone(mut m)", "}"),
			"cannot assign to m[k] alongside mut m in its value: a value is assigned in place in its map, " +
				"and a mut argument must be the only way to reach m during its call",
		},
		{
			"a value moved out", mapProgram("m map[int64]Bag, k int64",
				"if has(m, k) {", "    b := m[k]", "    if len(b.items) > 0 {", "    }", "}"),
			"cannot move m[k] out of m: a value cannot be moved out of a map; take it out with delete",
		},
		{"has of a list", mapProgram("l list[int64]", "if has(l, 0) {", "}"), "has expects a map or a pool, got list[int64]"},
		{"has of one argument", mapProgram("m map[int64]int64", "if has(m) {", "}"), "has expects 2 argument(s), got 1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := strings.Replace(tt.src, "func main()", hasHelpers+"func main()", 1)
			diags := check(t, src)
			if len(diags) == 0 || diags[0].Message != tt.want {
				t.Fatalf("got %v, want %q first", diags, tt.want)
			}
		})
	}
}
