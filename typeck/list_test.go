package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// listing returns a program whose function f takes l, a list of int64 it
// borrows, m, a list of int64 it may grow, s, a Stack it may change, and
// i, an int64, and whose body is lines, each on a line of its own,
// followed by return 0. Stack holds a list, items, and an int64, n.
func listing(lines ...string) string {
	return "package main\n\ntype Stack struct {\n    items list[int64]\n    n     int64\n}\n\n" +
		"func g(v []int64) int64 {\n    return 0\n}\n\n" +
		"func use(n int64) {\n}\n\n" +
		"func h(l list[int64]) int64 {\n    return 0\n}\n\n" +
		"func make2() list[int64] {\n    return make(list[int64], c_allocator)\n}\n\n" +
		"func f(l list[int64], mut m list[int64], mut s Stack, i int64) int64 {\n    " +
		strings.Join(lines, "\n    ") + "\n    return 0\n}\n\nfunc main() {\n}\n"
}

// listAcceptCases are list programs the checker accepts: proofs of
// indexes, moves that leave nothing used after them, and the forms every
// operation takes.
var listAcceptCases = []struct{ name, src string }{
	{"guard proves an index", listing("if 0 <= i && i < len(l) {", "    use(l[i])", "}")},
	{"length bound proves a constant index", listing("if len(l) > 2 {", "    use(l[2])", "}")},
	{"range index", listing("for j := range l {", "    use(l[j])", "}")},
	{"checked form", listing("x, ok := l[i]", "if ok {", "    use(x)", "}")},
	{
		"element writes keep the length fact",
		listing("if len(m) > 1 {", "    m[0] = 1", "    m[1]++", "    m[0] += m[1]", "}"),
	},
	{
		"write through a mut parameter at a range index",
		listing("for j := range m {", "    m[j] = 0", "}"),
	},
	{"append", listing("err := append(mut m, i)", "if err.code != 0 {", "    return 1", "}")},
	{
		"append a widened value",
		listing("err := append(mut m, int8(1))", "if err.code != 0 {", "    return 1", "}"),
	},
	{
		"append to a field",
		listing("err := append(mut s.items, 1)", "if err.code != 0 {", "    return 1", "}"),
	},
	{"append as an error value", listing("if append(mut m, 1) != noerror {", "    return 1", "}")},
	{"view of a list", listing("return g(l)")},
	{"view of a slice of a list", listing("if len(l) > 1 {", "    return g(l[1:])", "}")},
	{"len of a slice", listing("return len(m[:])")},
	{"borrowed", listing("return h(l) + h(m) + h(s.items)")},
	{"move into a local", listing("x := make2()", "y := x", "return len(y)")},
	{
		"conditional move, then a new value",
		listing(
			"mut x := make2()",
			"if i > 0 {",
			"    y := x",
			"    use(len(y))",
			"}",
			"x = make2()",
			"return len(x)",
		),
	},
	{
		"move in a loop, assigned again before the next iteration",
		listing(
			"mut x := make2()",
			"for range 3 {",
			"    y := x",
			"    x = make2()",
			"    use(len(y))",
			"}",
		),
	},
	{
		"move on a path that returns",
		listing("x := make2()", "if i > 0 {", "    return h(x)", "}", "return len(x)"),
	},
	{"assigned to itself", listing("mut x := make2()", "x = x", "return len(x)")},
	{
		"struct literal takes a list",
		listing("x := make2()", "t := Stack{items: x, n: 1}", "return len(t.items)"),
	},
	{"assign a struct whole", listing("s = Stack{items: make2(), n: 0}")},
	{
		"defer reads a list moved in another block",
		listing(
			"x := make2()",
			"if i > 0 {",
			"    defer {",
			"        use(len(x))",
			"    }",
			"}",
			"y := x",
			"return len(y)",
		),
	},
	{
		"range over a field may write elements",
		listing("for j := range s.items {", "    s.n += j", "}"),
	},
	{
		"allocator parameter",
		"package main\n\nfunc f(a Allocator) list[int64] {\n    return make(list[int64], a)\n}\n\nfunc use(n int64) {\n}\n\nfunc main() {\n    l := f(c_allocator)\n    use(len(l))\n}\n",
	},
	{
		"allocator global",
		"package main\n\nvar a = c_allocator\n\nfunc use(n int64) {\n}\n\nfunc main() {\n    l := make(list[int64], a)\n    use(len(l))\n}\n",
	},
	{
		"list of arrays of allocators",
		"package main\n\nfunc use(n int64) {\n}\n\nfunc main() {\n    l := make(list[[2]Allocator], c_allocator)\n    use(len(l))\n}\n",
	},
	{
		"two results with a list",
		"package main\n\nfunc f() (list[int64], int64) {\n    return make(list[int64], c_allocator), 1\n}\n\nfunc use(n int64) {\n}\n\nfunc main() {\n    l, n := f()\n    use((len(l) + n))\n}\n",
	},
}

// TestAcceptsLists checks each of listAcceptCases has no diagnostics.
func TestAcceptsLists(t *testing.T) {
	for _, tt := range listAcceptCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// listErrorCases are uses of lists that are errors, each with the one
// diagnostic it gives.
var listErrorCases = []struct{ name, src, want string }{
	{
		"index unproven", listing("return l[i]"),
		"index i may be out of range; check 0 <= i && i < len(l) first, or use v, ok := ...[i]",
	},
	{
		"write unproven",
		listing("m[i] = 1"),
		"index i may be out of range; check 0 <= i && i < len(m) first",
	},
	{
		"write a borrowed list",
		listing("if len(l) > 0 {", "    l[0] = 1", "}"),
		"cannot assign to element of parameter l",
	},
	{
		"field of a mutable variable has no length fact",
		listing("if len(s.items) > 0 {", "    return s.items[0]", "}"),
		"index 0 may be out of range; use v, ok := ...[0]",
	},
	{
		"append to a borrowed list",
		listing("err := append(mut l, 1)", "if err.code != 0 {", "    return 1", "}"),
		"cannot pass mut l: l is immutable; declare it with mut",
	},
	{
		"use after move",
		listing("x := make2()", "y := x", "return len(x) + len(y)"),
		"x is used after it was moved",
	},
	{
		"move out of a borrowed parameter", listing("x := l", "return len(x)"),
		"cannot move l: it is a parameter, which borrows its caller's value",
	},
	{
		"move out of a mut parameter", listing("x := m", "return len(x)"),
		"cannot move m: it is a mut parameter, whose value stays its caller's",
	},
	{
		"move out of a field", listing("x := s.items", "return len(x)"),
		"cannot move s.items out of s: a field cannot be moved on its own",
	},
	{
		"borrow alongside mut",
		listing("return k(mut m, m)"),
		"cannot pass m alongside mut m in this call: a list is not copied for a call, and a mut argument must be the only way to reach m during it",
	},
	{
		"view of a field alongside mut of its struct",
		listing("return j(mut s, s.items)"),
		"cannot pass s.items alongside mut s in this call: a list is not copied for a call, and a mut argument must be the only way to reach s during it",
	},
	{
		"range then append",
		listing(
			"for _, x := range m {",
			"    err := append(mut m, x)",
			"    if err.code != 0 {",
			"        return 1",
			"    }",
			"}",
		),
		"m is being ranged over, so this loop cannot pass mut m",
	},
	{
		"compare lists",
		listing("if l == m {", "    return 1", "}"),
		"operator == is not defined on list[int64]",
	},
	{
		"compare structs holding lists",
		listing("t := Stack{items: make2(), n: 0}", "if s == t {", "    return 1", "}"),
		"operator == is not defined on Stack",
	},
}

// k takes a list it may grow and one it borrows, and j a Stack it may
// change and a view, for the exclusivity cases.
const listingHelpers = "\nfunc k(mut a list[int64], b list[int64]) int64 {\n    return 0\n}\n" +
	"\nfunc j(mut s Stack, v []int64) int64 {\n    return 0\n}\n"

// TestRejectsListMisuse checks each of listErrorCases gives exactly its
// one diagnostic.
func TestRejectsListMisuse(t *testing.T) {
	for _, tt := range listErrorCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src+listingHelpers)
			if len(diags) != 1 || diags[0].Message != tt.want {
				t.Errorf("got %v, want one diagnostic %q", diags, tt.want)
			}
		})
	}
}

// TestRecordsListTypes checks the table names a list type, says it is one
// and owned, has no zero value, and has its element, that make's call
// records its list type and append's error, and that c_allocator is an
// Allocator.
func TestRecordsListTypes(t *testing.T) {
	src := mainLines(
		"mut l := make(list[int64], c_allocator)",
		"err := append(mut l, 1)",
		"if err.code != 0 {",
		"    return",
		"}",
	)
	file, diags := checkTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %v, want no diagnostics", diags)
	}
	mk, ap, alloc := builtinCalls(t, file)
	table := file.Types
	if !table.IsList(mk.Type) || table.Name(mk.Type) != "list[int64]" {
		t.Errorf("make's type is %s, want list[int64]", table.Name(mk.Type))
	}
	if table.Elem(mk.Type) != types.Int64 || !table.Owned(mk.Type) || table.HasZero(mk.Type) {
		t.Errorf("list[int64] has element %s, owned %v, zero value %v",
			table.Name(table.Elem(mk.Type)), table.Owned(mk.Type), table.HasZero(mk.Type))
	}
	if ap.Type != types.Error || ast.TypeOf(ap) != types.Error {
		t.Errorf("append's type is %s, want error", table.Name(ap.Type))
	}
	if ast.TypeOf(alloc) != types.Allocator {
		t.Errorf("c_allocator's type is %s, want Allocator", table.Name(ast.TypeOf(alloc)))
	}
}

// builtinCalls returns the last call to make, the last call to append and
// the last c_allocator in file, failing t when one is missing.
func builtinCalls(t *testing.T, file *ast.File) (mk, ap *ast.Call, alloc ast.Expr) {
	t.Helper()
	ast.Walk(file, func(n ast.Node) bool {
		if lit, ok := n.(*ast.CAllocatorLit); ok {
			alloc = lit
		}
		call, ok := n.(*ast.Call)
		if !ok {
			return true
		}
		if id, isName := call.Fn.(*ast.Ident); isName && id.Name == "make" {
			mk = call
		} else if isName && id.Name == "append" {
			ap = call
		}
		return true
	})
	if mk == nil || ap == nil || alloc == nil {
		t.Fatal("make, append or c_allocator not found")
	}
	return mk, ap, alloc
}

// TestMarksMoves checks the checker marks every name whose use moves its
// variable's value, and no other: a declaration's, an assignment's, a
// return's and a struct literal field's, but not a borrow, a view, len or
// a mut argument.
func TestMarksMoves(t *testing.T) {
	src := "package main\n\ntype S struct {\n    items list[int64]\n}\n\n" +
		"func h(l list[int64], v []int64) int64 {\n    return 0\n}\n\n" +
		"func use(n int64) {\n}\n\n" +
		"func f() list[int64] {\n    a := make(list[int64], c_allocator)\n" +
		"    n := h(a, a) + len(a)\n    mut b := a\n    err := append(mut b, n)\n" +
		"    if err.code != 0 {\n        return b\n    }\n    c := S{items: b}\n" +
		"    mut d := make(list[int64], c_allocator)\n    d = make(list[int64], c_allocator)\n" +
		"    e := c\n    use(len(e.items))\n    return d\n}\n\nfunc main() {\n}\n"
	file, diags := checkTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %v, want no diagnostics", diags)
	}
	var moved, kept []string
	ast.Walk(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if _, isVar := id.Def.(*ast.Var); isVar && file.Types.Owned(ast.TypeOf(id)) {
				if id.Move {
					moved = append(moved, id.Name)
				} else {
					kept = append(kept, id.Name)
				}
			}
		}
		return true
	})
	if got, want := strings.Join(moved, " "), "a b b c d"; got != want {
		t.Errorf("moved %q, want %q", got, want)
	}
	if got, want := strings.Join(kept, " "), "a a a b d e"; got != want {
		t.Errorf("kept %q, want %q", got, want)
	}
}

// TestDeepLoopsCheckInLinearTime checks the move check follows loops
// nested 60 deep, each level's body moving a list and assigning it again,
// in about as many passes as there are loops: following each loop's body
// twice at every level would take 2^60 passes, and the test would never
// end.
func TestDeepLoopsCheckInLinearTime(t *testing.T) {
	const depth = 60
	var b strings.Builder
	b.WriteString("package main\n\nfunc main() {\n    mut l := make(list[int64], c_allocator)\n")
	for range depth {
		b.WriteString("for range 2 {\n")
	}
	b.WriteString("m := l\nl = make(list[int64], c_allocator)\nprint(\"\")\nif len(m) > 0 {\n}\n")
	for range depth {
		b.WriteString("}\n")
	}
	b.WriteString("}\n")
	if _, diags := checkTree(t, b.String()); len(diags) > 0 {
		t.Errorf("got %v, want no diagnostics", diags)
	}
}
