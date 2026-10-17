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
			"mut n := 0", "for k, v := range m {", "    n += k + len(v)", "}", "for k := range m {", "    n += k", "}",
			"for _, v := range m {", "    n += len(v)", "}", "if n > 0 {", "}")},
		{"owned keys in place", mapProgram("m map[list[int64]]int64",
			"mut n := 0", "for k, v := range m {", "    n += len(k) + v", "}", "if n > 0 {", "}")},
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
			"key k may not be in m; use v, ok := m[k]",
		},
		{"a write", mapProgram("mut m map[int64]int64, k int64", "m[k] = 1"), "key k may not be in m; use put(mut m, k, ...)"},
		{
			"an owned value looked up", mapProgram("m map[int64]Bag", "b, ok := m[1]", "if ok && len(b.items) > 0 {", "}"),
			"the checked form cannot read a value of type Bag: it would copy it out of m; range over m to use its values in place",
		},
		{
			"delete bound to one name", mapProgram("mut m map[int64]int64", "x := delete(mut m, 1)", "if x == 0 {", "}"),
			"delete gives the value and whether the key was there; use v, ok := delete(mut m, 1)",
		},
		{
			"a dropped put", mapProgram("mut m map[int64]int64", "put(mut m, 1, 2)"),
			"the error put returns cannot be dropped; bind it, as in err := put(...)",
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
