package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// owning returns a program whose function f takes l, a list of lists of
// int64 it borrows, m, one it may change, bags, a list of Bags it may
// change, vals, a list of Vals it borrows, v, a view of lists of int64,
// and i, an int64, and whose body is lines, each on a line of its own,
// followed by return 0. Bag holds a list and an int64; Val is a union
// holding a list.
func owning(lines ...string) string {
	return "package main\n\ntype Bag struct {\n    items list[int64]\n    n     int64\n}\n\n" +
		"type Val union {\n    items list[int64]\n    num   int64\n    none\n}\n\n" +
		"func (b Bag) total() int64 {\n    return b.n\n}\n\n" +
		"func (mut b Bag) add(x int64) error {\n    return append(mut b.items, x)\n}\n\n" +
		"func use(n int64) {\n}\n\n" +
		"func read(x list[int64]) int64 {\n    return len(x)\n}\n\n" +
		"func view(x []int64) int64 {\n    return len(x)\n}\n\n" +
		"func grow(mut x list[int64]) {\n}\n\n" +
		"func fresh() list[int64] {\n    return make(list[int64], c_allocator)\n}\n\n" +
		"func f(l list[list[int64]], mut m list[list[int64]], mut bags list[Bag], vals list[Val], " +
		"v []list[int64], i int64) int64 {\n    " +
		strings.Join(lines, "\n    ") + "\n    return 0\n}\n\nfunc main() {\n}\n"
}

// ownedAcceptCases are programs with lists of owned elements the checker
// accepts: elements read, grown, matched and passed in place, replaced,
// moved in by append, and taken out by the checked forms of pop and
// remove, read once their ok is checked.
var ownedAcceptCases = []struct{ name, src string }{
	{
		"borrowed in place",
		owning("if 0 <= i && i < len(l) {", "    use(read(l[i]) + len(l[i]))", "}"),
	},
	{"viewed in place", owning("if len(l) > 0 {", "    use(view(l[0]))", "}")},
	{
		"an element of an element",
		owning(
			"if len(l) > 0 {",
			"    x, ok := l[0][i]",
			"    if ok {",
			"        use(x)",
			"    }",
			"}",
		),
	},
	{
		"grown in place",
		owning(
			"if len(m) > 0 {",
			"    err := append(mut m[0], i)",
			"    use(int64(err.code))",
			"    grow(mut m[0])",
			"}",
		),
	},
	{
		"a field grown in place",
		owning(
			"if len(bags) > 0 {",
			"    err := append(mut bags[0].items, 1)",
			"    use(int64(err.code))",
			"}",
		),
	},
	{
		"methods on an element",
		owning(
			"if len(bags) > 0 {",
			"    err := mut bags[0].add(1)",
			"    use(bags[0].total() + int64(err.code))",
			"}",
		),
	},
	{
		"a copyable field read",
		owning("if len(bags) > 0 {", "    n := bags[0].n", "    use(n)", "}"),
	},
	{
		"matched in place",
		owning(
			"if len(vals) > 0 {",
			"    switch vals[0] {",
			"    case items(xs):",
			"        use(len(xs))",
			"    default:",
			"    }",
			"}",
		),
	},
	{
		"a range binding in place",
		owning("for _, x := range l {", "    if len(x) > 0 {", "        use(x[0])", "    }", "}"),
	},
	{"a range over a view", owning("for _, x := range v {", "    use(read(x))", "}")},
	{
		"a range over an element",
		owning("if len(l) > 0 {", "    for _, x := range l[0] {", "        use(x)", "    }", "}"),
	},
	{"an element replaced", owning("if len(m) > 0 {", "    m[0] = fresh()", "}")},
	{
		"a range index over a mut parameter",
		owning("for j := range len(m) {", "    use(len(m[j]))", "}"),
	},
	{
		"moved in by append",
		owning("x := fresh()", "err := append(mut m, x, fresh())", "use(int64(err.code))"),
	},
	{
		"moved in on one path",
		owning(
			"x := fresh()",
			"if i > 0 {",
			"    err := append(mut m, x)",
			"    use(int64(err.code))",
			"}",
		),
	},
	{
		"moved in every iteration",
		owning(
			"for j := range 3 {",
			"    mut x := fresh()",
			"    e := append(mut x, j)",
			"    err := append(mut m, x)",
			"    use(int64(err.code + e.code))",
			"}",
		),
	},
	{
		"a checked pop read where ok",
		owning("x, ok := pop(mut m)", "if ok {", "    use(len(x))", "}"),
	},
	{
		"a checked pop read after an early exit",
		owning("x, ok := pop(mut m)", "if !ok {", "    return 1", "}", "use(len(x))"),
	},
	{
		"a checked pop read on the right of &&",
		owning("x, ok := pop(mut m)", "if ok && len(x) > 0 {", "    use(x[0])", "}"),
	},
	{
		"a checked pop in a loop",
		owning(
			"for {",
			"    x, ok := pop(mut m)",
			"    if !ok {",
			"        break",
			"    }",
			"    use(len(x))",
			"}",
		),
	},
	{
		"a checked remove",
		owning("x, ok := remove(mut bags, i)", "if ok {", "    use(len(x.items))", "}"),
	},
	{
		"a checked pop assigned",
		owning(
			"mut x := fresh()",
			"mut ok := false",
			"x, ok = pop(mut m)",
			"if ok {",
			"    use(len(x))",
			"}",
		),
	},
	{
		"a copyable element read unchecked",
		owning(
			"if len(m) == 0 {",
			"    return 1",
			"}",
			"x, ok := pop(mut m[0])",
			"use(x)",
			"if ok {",
			"    use(1)",
			"}",
		),
	},
	{
		"a copyable element dropped",
		owning(
			"if len(m) == 0 {",
			"    return 1",
			"}",
			"_, ok := remove(mut m[0], i)",
			"if ok {",
			"    use(1)",
			"}",
		),
	},
}

// TestAcceptsOwnedElements checks each of ownedAcceptCases has no
// diagnostics.
func TestAcceptsOwnedElements(t *testing.T) {
	for _, tt := range ownedAcceptCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// TestRecordsTakes checks a call to pop or remove records the element type
// it gives, and that an owned value appended to a list is marked moved,
// while one passed in place, a mut argument or a borrowed element, is not.
func TestRecordsTakes(t *testing.T) {
	src := owning(
		"x := fresh()",
		"err := append(mut m, x)",
		"use(int64(err.code))",
		"y, ok := pop(mut m)",
		"z, ok2 := remove(mut bags, i)",
		"if ok && ok2 {",
		"    use(len(y) + z.n)",
		"}",
	)
	file, diags := checkTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %v, want no diagnostics", diags)
	}
	takes, moved := takesAndMoves(file)
	if got, want := strings.Join(takes, ", "), "pop list[int64], remove Bag"; got != want {
		t.Errorf("takes give %q, want %q", got, want)
	}
	if !moved["x"] || moved["m"] {
		t.Errorf("moved %v, want x appended and m, passed as mut, not", moved)
	}
}

// takesAndMoves returns each call to pop or remove in file, by its name
// and the element type recorded on it, and the names marked as moved.
func takesAndMoves(file *ast.File) (takes []string, moved map[string]bool) {
	moved = map[string]bool{}
	ast.Walk(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Call:
			if id, ok := x.Fn.(*ast.Ident); ok && (id.Name == "pop" || id.Name == "remove") {
				takes = append(takes, id.Name+" "+file.Types.Name(x.Type))
			}
		case *ast.Ident:
			if x.Move {
				moved[x.Name] = true
			}
		}
		return true
	})
	return takes, moved
}

// TestMethodsMayBeNamedPopAndRemove checks a method may share a name with
// pop or remove, as with append, since a method is only reached through a
// value, and that a bare pop inside one is still the builtin.
func TestMethodsMayBeNamedPopAndRemove(t *testing.T) {
	src := "package main\n\ntype Stack struct {\n    items list[int64]\n}\n\n" +
		"func (mut s Stack) pop() (int64, bool) {\n    x, ok := pop(mut s.items)\n    return x, ok\n}\n\n" +
		"func (s Stack) remove() int64 {\n    return len(s.items)\n}\n\n" +
		"func main() {\n    mut s := Stack{items: make(list[int64], c_allocator)}\n" +
		"    x, ok := mut s.pop()\n    if ok && x + s.remove() > 0 {\n        print(\"y\")\n    }\n}\n"
	if _, diags := checkTree(t, src); len(diags) > 0 {
		t.Errorf("got %v, want no diagnostics", diags)
	}
}
