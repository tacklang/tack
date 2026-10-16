package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// returning returns a program declaring Text, a struct holding a list of
// bytes, with a method bytes returning a view of it and a mut method
// push; whole, which returns its view parameter; use, which takes a view;
// and touch, which takes a Text as mut; then decls, and a function f whose
// body is lines, each on a line of its own, taking t, a Text it borrows,
// m, one it may change, v, a view of bytes, and a, an array of bytes it
// may change.
func returning(decls string, lines ...string) string {
	return "package main\n\ntype Text struct {\n    b list[uint8]\n}\n\n" +
		"func (t Text) bytes() []uint8 {\n    return t.b\n}\n\n" +
		"func (mut t Text) push(c uint8) error {\n    return append(mut t.b, c)\n}\n\n" +
		"func whole(v []uint8) []uint8 {\n    return v\n}\n\n" +
		"func use(v []uint8) int64 {\n    return len(v)\n}\n\n" +
		"func touch(mut t Text) int64 {\n    return 0\n}\n\n" +
		decls + "\n\n" +
		"func f(t Text, mut m Text, v []uint8, mut a [3]uint8) {\n    " +
		strings.Join(lines, "\n    ") + "\n}\n\nfunc main() {\n}\n"
}

// viewResultAcceptCases are programs with views returned by functions and
// methods that the checker accepts.
var viewResultAcceptCases = []struct{ name, src string }{
	{
		"passed to a view parameter",
		returning("", "n := use(t.bytes()) + use(whole(v))", "if n == 0 {", "}"),
	},
	{"len", returning("", "if len(t.bytes()) == len(whole(a)) {", "}")},
	{
		"index while another place changes",
		returning("", "c, ok := t.bytes()[touch(mut m)]", "if ok && c == 0 {", "}"),
	},
	{"checked index", returning("", "c, ok := t.bytes()[0]", "if ok && c == 0 {", "}")},
	{"whole slice", returning("", "if use(t.bytes()[:]) + use(whole(v)[0:0]) == 0 {", "}")},
	{
		"range",
		returning("", "for i, c := range t.bytes() {", "    if i == 0 && c == 0 {", "    }", "}"),
	},
	{
		"range then change",
		returning(
			"",
			"for range m.bytes() {",
			"}",
			"err := mut m.push(1)",
			"if err.code == 0 {",
			"}",
		),
	},
	{
		"another place changed while ranging",
		returning(
			"",
			"for _, c := range t.bytes() {",
			"    err := mut m.push(c)",
			"    if err.code == 0 {",
			"    }",
			"}",
		),
	},
	{
		"a mut argument inside the view's own call",
		returning(
			"func g(x int64, v []uint8) []uint8 {\n    return v\n}",
			"if use(g(touch(mut m), t.bytes())) == 0 {",
			"}",
		),
	},
	{
		"a disjoint field alongside",
		returning(
			"type Pair struct {\n    p Text\n    q Text\n}\n\nfunc both(v []uint8, mut t Text) {\n}",
			"mut p := Pair{p: Text{b: make(list[uint8], c_allocator)}, q: Text{b: make(list[uint8], c_allocator)}}",
			"both(p.p.bytes(), mut p.q)",
		),
	},
	{
		"spread",
		returning(
			"func n(xs ...uint8) int64 {\n    return len(xs)\n}",
			"if n(t.bytes()...) == 0 {",
			"}",
		),
	},
	{
		"returned on",
		returning(
			"func g(t Text, v []uint8) []uint8 {\n    if len(v) > 0 {\n        return whole(v[1:])\n    }\n    return t.bytes()\n}",
		),
	},
	{
		"an element of a list parameter",
		returning(
			"func g(l list[list[uint8]], none []uint8) []uint8 {\n    if len(l) > 0 {\n        return l[0]\n    }\n    return none\n}",
		),
	},
	{
		"an array through a view's element",
		returning(
			"type Cell struct {\n    px [2][4]uint8\n}\n\nfunc g(c []Cell, none []uint8) []uint8 {\n    if len(c) > 0 {\n        return c[0].px[1]\n    }\n    return none\n}",
		),
	},
	{
		"an interface's method",
		returning(
			"type Viewer interface {\n    bytes() []uint8\n}\n\nfunc g[T Viewer](x T) []uint8 {\n    return x.bytes()\n}",
			"if use(g(t)) == 0 {",
			"}",
		),
	},
	{
		"a union's method",
		returning(
			"type U union {\n    a int64\n    b\n}\n\nfunc (u U) pick(x []uint8, y []uint8) []uint8 {\n    switch u {\n    case a:\n        return x\n    case b:\n        return y\n    }\n}",
		),
	},
}

// TestAcceptsViewResults checks each of viewResultAcceptCases has no
// diagnostics.
func TestAcceptsViewResults(t *testing.T) {
	for _, tt := range viewResultAcceptCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// viewResultRejectCases are programs the checker rejects for how they
// declare, return or use a view result, each with the one diagnostic it
// gives.
var viewResultRejectCases = []struct{ name, src, want string }{
	{
		"stored",
		returning("", "x := t.bytes()", "if len(x) == 0 {", "}"),
		"t.bytes() is a view, []uint8, which can only be passed to a view parameter, given to len, indexed, sliced, ranged over or returned",
	},
	{
		"printed",
		returning("", "print(whole(v))"),
		"whole(v) is a view, []uint8, which can only be passed",
	},
	{"dropped", returning("", "t.bytes()"), "t.bytes() is not used"},
	{
		"unchecked index",
		returning("", "if t.bytes()[0] == 0 {", "}"),
		"index 0 may be out of range; use v, ok := ...[0]",
	},
	{
		"several results",
		returning("func g(v []uint8) ([]uint8, int64) {\n    return v, 0\n}"),
		"a view can only be a function's only result",
	},
	{"extern", returning("extern func g(v []uint8) []uint8"), "extern func g cannot return a view"},
	{
		"local",
		returning("func g() []uint8 {\n    l := make(list[uint8], c_allocator)\n    return l\n}"),
		"cannot return a view of l: l is a local variable; a returned view can only point into what a parameter that is not mut holds",
	},
	{
		"mut parameter",
		returning("func g(mut l list[uint8]) []uint8 {\n    return l[0:]\n}"),
		"cannot return a view of l[0:]: l is a mut parameter",
	},
	{
		"array parameter",
		returning("func g(x [3]uint8) []uint8 {\n    return x\n}"),
		"cannot return a view of x: the function holds its own copy of x",
	},
	{
		"string bytes",
		returning("func g(s string) []uint8 {\n    return []uint8(s)\n}"),
		"cannot return []uint8(s): a returned view cannot point into a string's bytes",
	},
	{
		"call of a local",
		returning("func g() []uint8 {\n    x := [2]uint8{1, 2}\n    return whole(x)\n}"),
		"cannot return whole(x): the view it returns may point into x, and x is a local variable",
	},
	{
		"packed",
		returning(
			"func all(xs ...uint8) []uint8 {\n    return xs\n}\n\nfunc g() []uint8 {\n    return all(1)\n}",
		),
		"the view it returns may point into the values for xs, and they are packed into a temporary",
	},
	{
		"overlap",
		returning("func g(v []uint8, mut t Text) {\n}", "g(m.bytes(), mut m)"),
		"cannot pass m.bytes() alongside mut m in this call: the view it returns may point into m",
	},
	{
		"nested overlap",
		returning("func g(v []uint8, n int64) {\n}", "g(whole(m.bytes()[0:]), touch(mut m))"),
		"cannot pass whole(m.bytes()[0:]) alongside mut m in this call",
	},
	{
		"array overlap",
		returning("func g(v []uint8, mut a [3]uint8) {\n}", "g(whole(a), mut a)"),
		"cannot pass whole(a) alongside mut a in this call",
	},
	{
		"index overlap",
		returning("", "c, ok := m.bytes()[touch(mut m)]", "if ok && c == 0 {", "}"),
		"cannot index m.bytes() alongside mut m in its index: the view it returns may point into m, " +
			"and a mut argument must be the only way to reach m during its call",
	},
	{
		"slice overlap",
		returning("", "if use(m.bytes()[touch(mut m):]) == 0 {", "}"),
		"cannot slice m.bytes() alongside mut m in its bounds: the view it returns may point into m",
	},
	{
		"array index overlap",
		returning(
			"func cell(mut a [3]uint8) int64 {\n    return 0\n}",
			"c, ok := whole(a)[cell(mut a)]",
			"if ok && c == 0 {",
			"}",
		),
		"cannot index whole(a) alongside mut a in its index: the view it returns may point into a",
	},
	{
		"range then push",
		returning(
			"",
			"for _, c := range m.bytes() {",
			"    err := mut m.push(c)",
			"    if err.code == 0 {",
			"    }",
			"}",
		),
		"m.bytes() is being ranged over, and the view it returns may point into m, so this loop cannot pass mut m",
	},
	{
		"range then assign a field",
		returning("", "for range m.bytes() {", "    m.b = make(list[uint8], c_allocator)", "}"),
		"so this loop cannot assign m.b",
	},
	{
		"range then assign an element",
		returning("", "for range whole(a) {", "    a[1] = 0", "}"),
		"whole(a) is being ranged over, and the view it returns may point into a, so this loop cannot assign a[1]",
	},
}

// TestRejectsViewResults checks each of viewResultRejectCases gives
// exactly one diagnostic, holding its message.
func TestRejectsViewResults(t *testing.T) {
	for _, tt := range viewResultRejectCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != 1 || !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("got %v, want one diagnostic holding %q", diags, tt.want)
			}
		})
	}
}

// TestRecordsViewResultTypes checks a view result's type is recorded on
// its function, as the view of its element type, and on a call to it.
func TestRecordsViewResultTypes(t *testing.T) {
	file, diags := checkTree(t, returning("", "if len(t.bytes()) == 0 {", "}"))
	if len(diags) > 0 {
		t.Fatalf("got %v, want no diagnostics", diags)
	}
	table := file.Types
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.Func); ok && fn.Name == "bytes" &&
			(len(fn.ResultTypes) != 1 || !table.IsView(fn.ResultTypes[0])) {
			t.Fatalf("bytes returns %v, want one view", fn.ResultTypes)
		}
	}
	calls := callsOf(file, "bytes")
	if len(calls) != 1 {
		t.Fatalf("found %d calls of bytes, want 1", len(calls))
	}
	if !table.IsView(ast.TypeOf(calls[0])) {
		t.Errorf("t.bytes() has type %v, want a view", ast.TypeOf(calls[0]))
	}
}

// callsOf returns every call in file of a function or method named name.
func callsOf(file *ast.File, name string) []*ast.Call {
	var calls []*ast.Call
	ast.Walk(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.Call); ok {
			if fn := ast.Callee(call); fn != nil && fn.Name == name {
				calls = append(calls, call)
			}
		}
		return true
	})
	return calls
}
