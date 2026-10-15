package typeck_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// withTypes puts type declarations, and any other declarations, before a
// main function made of lines.
func withTypes(decls string, lines ...string) string {
	return "package main\n\n" + decls + "\n\n" + strings.TrimPrefix(
		mainLines(lines...),
		"package main\n\n",
	)
}

// pointDecls declares the structs most cases below use.
const pointDecls = "type Point struct {\n    x int64\n    y int64\n}\n\n" +
	"type Line struct {\n    a Point\n    b Point\n}\n\n" +
	"type Grid struct {\n    cells [3]int8\n    name string\n}"

// structCases are struct programs that must check cleanly.
var structCases = []struct{ name, src string }{
	{
		"literal",
		withTypes(pointDecls, "p := Point{x: 1, y: 2}", "code, _ := int32(p.x)", "exit(code)"),
	},
	{
		"fields in any order",
		withTypes(pointDecls, "p := Point{y: 2, x: 1}", "code, _ := int32(p.y)", "exit(code)"),
	},
	{
		"multi-line literal",
		withTypes(
			pointDecls,
			"p := Point{",
			"    x: 1,",
			"    y: 2,",
			"}",
			"code, _ := int32(p.x)",
			"exit(code)",
		),
	},
	{
		"constants take the field type",
		withTypes(
			pointDecls,
			"g := Grid{cells: [3]int8{-128, 0, 127}, name: \"\"}",
			"exit(g.cells[0])",
		),
	},
	{
		"values widen",
		withTypes(
			pointDecls,
			"var small int8 = 3",
			"p := Point{x: small, y: 'a'}",
			"code, _ := int32(p.y)",
			"exit(code)",
		),
	},
	{
		"nested literal",
		withTypes(
			pointDecls,
			"l := Line{a: Point{x: 1, y: 2}, b: Point{x: 3, y: 4}}",
			"code, _ := int32(l.b.y)",
			"exit(code)",
		),
	},
	{
		"array of structs, element types left out",
		withTypes(
			pointDecls,
			"a := [2]Point{{x: 1, y: 2}, Point{x: 3, y: 4}}",
			"code, _ := int32(a[1].x)",
			"exit(code)",
		),
	},
	{
		"array of arrays of structs",
		withTypes(
			pointDecls,
			"a := [1][1]Point{{{x: 1, y: 2}}}",
			"code, _ := int32(a[0][0].y)",
			"exit(code)",
		),
	},
	{
		"zero value",
		withTypes(
			pointDecls,
			"mut p Point",
			"mut l Line",
			"mut g Grid",
			"code, _ := int32(p.x + l.a.y + g.cells[2] + len(g.name))",
			"exit(code)",
		),
	},
	{
		"copied",
		withTypes(
			pointDecls,
			"p := Point{x: 1, y: 2}",
			"mut q := p",
			"q.x = 5",
			"code, _ := int32(p.x + q.x)",
			"exit(code)",
		),
	},
	{
		"compared",
		withTypes(
			pointDecls,
			"p := Point{x: 1, y: 2}",
			"l := Line{a: p, b: p}",
			"if p == l.a && l != (Line{a: p, b: l.b}) {",
			"}",
		),
	},
	{
		"array of structs compared",
		withTypes(pointDecls, "a := [1]Point{{x: 1, y: 1}}", "if a == a {", "}"),
	},
	{
		"field of a field written",
		withTypes(
			pointDecls,
			"mut l Line",
			"l.a.x = 1",
			"l.b.y += l.a.x",
			"l.b.y++",
			"code, _ := int32(l.b.y)",
			"exit(code)",
		),
	},
	{
		"field of an element written",
		withTypes(
			pointDecls,
			"mut a [2]Point",
			"a[1].x = 3",
			"a[0].y--",
			"code, _ := int32(a[1].x)",
			"exit(code)",
		),
	},
	{
		"element of a field written",
		withTypes(
			pointDecls,
			"mut g Grid",
			"g.cells[2] = 1",
			"g.cells[1] *= 2",
			"exit(g.cells[2])",
		),
	},
	{
		"whole field written",
		withTypes(
			pointDecls,
			"mut l Line",
			"l.a = Point{x: 1, y: 1}",
			"code, _ := int32(l.a.x)",
			"exit(code)",
		),
	},
	{
		"string field",
		withTypes(
			pointDecls,
			"mut g Grid",
			"g.name = \"ab\"",
			"print(g.name)",
			"code, _ := int32(len(g.name))",
			"exit(code)",
		),
	},
	{
		"string field indexed",
		withTypes(
			pointDecls,
			"g := Grid{cells: [3]int8{1, 2, 3}, name: \"ab\"}",
			"c, ok := g.name[1]",
			"if ok {",
			"    exit(c)",
			"}",
		),
	},
	{
		"field of a call",
		withTypes(
			pointDecls+"\n\nfunc f() Point {\n    return Point{x: 1, y: 2}\n}",
			"code, _ := int32(f().y)",
			"exit(code)",
		),
	},
	{"field of a literal", withTypes(pointDecls, "exit(int32(Point{x: 1, y: 2}.y))")},
	{
		"field of an element",
		withTypes(
			pointDecls,
			"a := [2]Point{{x: 1, y: 2}, {x: 3, y: 4}}",
			"code, _ := int32(a[1].y)",
			"exit(code)",
		),
	},
	{
		"struct parameter and result",
		withTypes(
			pointDecls+"\n\nfunc swap(p Point) Point {\n    return Point{x: p.y, y: p.x}\n}",
			"code, _ := int32(swap(Point{x: 1, y: 2}).x)",
			"exit(code)",
		),
	},
	{
		"structs in several results",
		withTypes(
			pointDecls+"\n\nfunc two() (Point, Line) {\n    p := Point{x: 1, y: 2}\n    return p, Line{a: p, b: p}\n}",
			"p, l := two()",
			"code, _ := int32(p.x + l.b.y)",
			"exit(code)",
		),
	},
	{
		"several results assigned to fields",
		withTypes(
			pointDecls+"\n\nfunc two() (int64, Point) {\n    return 1, Point{x: 2, y: 3}\n}",
			"mut l Line",
			"l.a.x, l.b = two()",
			"code, _ := int32(l.a.x + l.b.y)",
			"exit(code)",
		),
	},
	{
		"range over an array of structs",
		withTypes(
			pointDecls,
			"a := [2]Point{{x: 1, y: 2}, {x: 3, y: 4}}",
			"mut sum := 0",
			"for _, p := range a {",
			"    sum += p.x",
			"}",
			"code, _ := int32(sum)",
			"exit(code)",
		),
	},
	{
		"range over a field",
		withTypes(
			pointDecls,
			"mut g Grid",
			"for i := range g.cells {",
			"    g.cells[i] = 1",
			"}",
			"exit(g.cells[0])",
		),
	},
	{
		"len of a field",
		withTypes(
			pointDecls,
			"mut g Grid",
			"mut a [len(g.cells)]bool",
			"a[2] = true",
			"if a[2] {",
			"}",
		),
	},
	{"empty struct", withTypes("type E struct {}", "e := E{}", "mut f E", "if e == f {", "}")},
	{
		"struct of an empty struct",
		withTypes(
			"type E struct {}\n\ntype S struct {\n    e E\n    n int64\n}",
			"s := S{e: E{}, n: 1}",
			"code, _ := int32(s.n)",
			"exit(code)",
		),
	},
	{"array of zero structs", withTypes("type E struct {}", "a := [0]E{}", "exit(int32(len(a)))")},
	{
		"global struct",
		withTypes(
			pointDecls+"\n\nvar origin = Point{x: 0, y: 0}\n\nmut cursor Point",
			"cursor.x = origin.y + 1",
			"code, _ := int32(cursor.x)",
			"exit(code)",
		),
	},
	{
		"global nested literal",
		withTypes(
			pointDecls+"\n\nvar box = [2]Line{{a: Point{x: 1, y: 2}, b: Point{x: 3, y: 4}}, {a: Point{x: 0, y: 0}, b: Point{x: 0, y: 0}}}",
			"exit(int32(box[0].b.x))",
		),
	},
	{
		"field lengths from constant expressions",
		withTypes(
			"type B struct {\n    a [2 * 3 - 2]int64\n    c ['c' - 'a'][len(\"abc\")]int8\n}",
			"mut b B",
			"b.a[3] = 1",
			"b.c[1][2] = 1",
			"code, _ := int32(b.a[3] + len(b.c))",
			"exit(code)",
		),
	},
	{
		"global literal of a struct with a constant length",
		withTypes(
			"var g = B{a: [2]int64{1, 2}}\n\ntype B struct {\n    a [len(\"ab\")]int64\n}",
			"exit(int32(g.a[1]))",
		),
	},
	{
		"field shares a variable's name",
		withTypes(
			pointDecls,
			"x := 1",
			"p := Point{x: x, y: x}",
			"code, _ := int32(p.x)",
			"exit(code)",
		),
	},
	{
		"checked division by a field",
		withTypes(
			pointDecls,
			"p := Point{x: 6, y: 0}",
			"q, ok := p.x / p.y",
			"if ok {",
			"    code, _ := int32(q)",
			"    exit(code)",
			"}",
		),
	},
	{
		"checked index by a field",
		withTypes(
			pointDecls,
			"p := Point{x: 1, y: 9}",
			"a := [2]int64{1, 2}",
			"v, ok := a[p.y]",
			"if ok {",
			"    code, _ := int32(v)",
			"    exit(code)",
			"}",
		),
	},
	{
		"field proved through a variable",
		withTypes(
			pointDecls,
			"p := Point{x: 6, y: 2}",
			"d := p.y",
			"if d != 0 {",
			"    code, _ := int32(p.x / d)",
			"    exit(code)",
			"}",
		),
	},
	{
		"constant index into a field",
		withTypes(pointDecls, "g := Grid{cells: [3]int8{1, 2, 3}, name: \"\"}", "exit(g.cells[2])"),
	},
	{
		"checked element of a field",
		withTypes(
			pointDecls,
			"g := Grid{cells: [3]int8{1, 2, 3}, name: \"\"}",
			"mut i := 1",
			"i = 5",
			"v, ok := g.cells[i]",
			"if ok {",
			"    exit(v)",
			"}",
		),
	},
	{
		"struct compared with a string field",
		withTypes(
			pointDecls,
			"g := Grid{cells: [3]int8{0, 0, 0}, name: \"\"}",
			"mut z Grid",
			"if g == z || g != (Grid{cells: g.cells, name: \"g\"}) || g.name < z.name {",
			"    exit(1)",
			"}",
		),
	},
}

// TestAcceptsStructs checks each program above has no type errors.
func TestAcceptsStructs(t *testing.T) {
	for _, tt := range structCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v", diags)
			}
		})
	}
}

// structErrorCases are struct programs the checker must reject, with the
// diagnostics each must produce and the source each must cover.
var structErrorCases = []struct {
	name string
	src  string
	want []spanned
}{
	{
		"positional",
		withTypes(pointDecls, "p := Point{1, 2}", "code, _ := int32(p.x)", "exit(code)"),
		[]spanned{
			{"missing field name in Point literal", "1"},
			{"missing field name in Point literal", "2"},
		},
	},
	{
		"positional and keyed",
		withTypes(pointDecls, "p := Point{x: 1, 2}", "code, _ := int32(p.x)", "exit(code)"),
		[]spanned{{"missing field name in Point literal", "2"}},
	},
	{
		"missing field",
		withTypes(pointDecls, "p := Point{x: 1}", "code, _ := int32(p.x)", "exit(code)"),
		[]spanned{{"Point literal is missing field y", "Point{x: 1}"}},
	},
	{
		"missing fields",
		withTypes(pointDecls, "p := Point{}", "code, _ := int32(p.x)", "exit(code)"),
		[]spanned{{"Point literal is missing fields x, y", "Point{}"}},
	},
	{
		"duplicate field",
		withTypes(
			pointDecls,
			"p := Point{x: 1, y: 2, x: 3}",
			"code, _ := int32(p.x)",
			"exit(code)",
		),
		[]spanned{{"duplicate field x in Point literal", "x"}},
	},
	{
		"unknown field",
		withTypes(
			pointDecls,
			"p := Point{x: 1, y: 2, z: 3}",
			"code, _ := int32(p.x)",
			"exit(code)",
		),
		[]spanned{{"Point has no field z", "z"}},
	},
	{
		"unknown field and missing",
		withTypes(pointDecls, "p := Point{x: 1, z: 3}", "code, _ := int32(p.x)", "exit(code)"),
		[]spanned{
			{"Point has no field z", "z"},
			{"Point literal is missing field y", "Point{x: 1, z: 3}"},
		},
	},
	{
		"key not a name",
		withTypes(pointDecls, "p := Point{\"x\": 1, y: 2}", "code, _ := int32(p.x)", "exit(code)"),
		[]spanned{{"invalid field name in Point literal", "\"x\""}},
	},
	{
		"key in parentheses",
		withTypes(pointDecls, "p := Point{(x): 1, y: 2}", "code, _ := int32(p.x)", "exit(code)"),
		[]spanned{{"invalid field name in Point literal", "x"}},
	},
	{
		"key in two pairs of parentheses",
		withTypes(pointDecls, "p := Point{x: 1, ((y)): 2}", "code, _ := int32(p.x)", "exit(code)"),
		[]spanned{{"invalid field name in Point literal", "y"}},
	},
	{
		"key in parentheses in an array of structs",
		withTypes(
			pointDecls,
			"a := [1]Point{{(x): 1, y: 2}}",
			"code, _ := int32(a[0].x)",
			"exit(code)",
		),
		[]spanned{{"invalid field name in Point literal", "x"}},
	},
	{
		"field of the wrong type",
		withTypes(pointDecls, "p := Point{x: true, y: 2}", "code, _ := int32(p.x)", "exit(code)"),
		[]spanned{{"cannot use bool as int64 in field x of Point literal", "true"}},
	},
	{
		"field constant overflows",
		withTypes(
			pointDecls,
			"g := Grid{cells: [3]int8{128, 0, 0}, name: \"\"}",
			"exit(g.cells[0])",
		),
		[]spanned{{"constant 128 overflows int8", "128"}},
	},
	{
		"struct of another type",
		withTypes(
			pointDecls,
			"l := Line{a: Grid{cells: [3]int8{0, 0, 0}, name: \"\"}, b: Point{x: 0, y: 0}}",
			"code, _ := int32(l.a.x)",
			"exit(code)",
		),
		[]spanned{
			{
				"cannot use Grid as Point in field a of Line literal",
				"Grid{cells: [3]int8{0, 0, 0}, name: \"\"}",
			},
		},
	},
	{
		"inner literal without its type",
		withTypes(
			pointDecls,
			"l := Line{a: {x: 1, y: 2}, b: Point{x: 0, y: 0}}",
			"code, _ := int32(l.a.x)",
			"exit(code)",
		),
		[]spanned{{"a literal without a type needs an array literal around it", "{x: 1, y: 2}"}},
	},
	{
		"inner literal without its type, missing a field",
		withTypes(pointDecls, "a := [1]Point{{x: 1}}", "code, _ := int32(a[0].x)", "exit(code)"),
		[]spanned{{"Point literal is missing field y", "{x: 1}"}},
	},
	{
		"inner positional literal",
		withTypes(pointDecls, "a := [1]Point{{1, 2}}", "code, _ := int32(a[0].x)", "exit(code)"),
		[]spanned{
			{"missing field name in Point literal", "1"},
			{"missing field name in Point literal", "2"},
		},
	},
	{
		"key in an array literal",
		mainLines("a := [2]int64{0: 1, 1: 2}", "code, _ := int32(a[0])", "exit(code)"),
		[]spanned{
			{"keys are not allowed in an array literal", "0: 1"},
			{"keys are not allowed in an array literal", "1: 2"},
		},
	},
	{
		"key in an inner array literal",
		mainLines("a := [1][1]int64{{0: 1}}", "code, _ := int32(a[0][0])", "exit(code)"),
		[]spanned{{"keys are not allowed in an array literal", "0: 1"}},
	},
	{
		"literal of a basic type",
		mainLines("n := int64{1}", "exit(n)"),
		[]spanned{
			{"int64 cannot be written as a literal; only arrays and structs can", "int64{1}"},
		},
	},
	{
		"unknown type literal", mainLines("n := Nope{x: 1}", "exit(n)"),
		[]spanned{{"unknown type: Nope", "Nope"}},
	},
	{
		"no such field read", withTypes(pointDecls, "p := Point{x: 1, y: 2}", "exit(p.z)"),
		[]spanned{{"Point has no field z", "z"}},
	},
	{
		"no such field written",
		withTypes(pointDecls, "mut p Point", "p.z = 1", "code, _ := int32(p.x)", "exit(code)"),
		[]spanned{{"Point has no field z", "z"}},
	},
	{
		"field of an integer", mainLines("n := 1", "exit(n.x)"),
		[]spanned{{"int64 has no field x", "x"}},
	},
	{
		"field of a constant", mainLines("exit((1).x)"),
		[]spanned{{"int64 has no field x", "x"}},
	},
	{
		"field of an array", mainLines("a := [1]int64{1}", "exit(a.x)"),
		[]spanned{{"[1]int64 has no field x", "x"}},
	},
	{
		"field of a string", mainLines("s := \"ab\"", "exit(s.len)"),
		[]spanned{{"string has no field len", "len"}},
	},
	{
		"immutable field",
		withTypes(
			pointDecls,
			"p := Point{x: 1, y: 2}",
			"p.x = 3",
			"code, _ := int32(p.x)",
			"exit(code)",
		),
		[]spanned{{"cannot assign to field of immutable variable p", "p"}},
	},
	{
		"immutable nested field",
		withTypes(
			pointDecls,
			"l := Line{a: Point{x: 1, y: 2}, b: Point{x: 1, y: 2}}",
			"l.a.x++",
			"code, _ := int32(l.a.x)",
			"exit(code)",
		),
		[]spanned{{"cannot assign to field of immutable variable l", "l"}},
	},
	{
		"immutable field of an element",
		withTypes(
			pointDecls,
			"a := [1]Point{{x: 1, y: 2}}",
			"a[0].x += 1",
			"code, _ := int32(a[0].x)",
			"exit(code)",
		),
		[]spanned{{"cannot assign to element of immutable variable a", "a"}},
	},
	{
		"immutable element of a field",
		withTypes(
			pointDecls,
			"g := Grid{cells: [3]int8{0, 0, 0}, name: \"\"}",
			"g.cells[0] = 1",
			"exit(g.cells[0])",
		),
		[]spanned{{"cannot assign to field of immutable variable g", "g"}},
	},
	{
		"field of a parameter",
		withTypes(pointDecls+"\n\nfunc f(p Point) {\n    p.x = 1\n}", "f(Point{x: 1, y: 2})"),
		[]spanned{{"cannot assign to field of parameter p", "p"}},
	},
	{
		"field of a range element",
		withTypes(
			pointDecls,
			"a := [1]Point{{x: 1, y: 2}}",
			"for _, p := range a {",
			"    p.x = 2",
			"    code, _ := int32(p.y)",
			"    exit(code)",
			"}",
		),
		[]spanned{{"cannot assign to field of immutable variable p", "p"}},
	},
	{
		"field of an immutable global",
		withTypes(
			pointDecls+"\n\nvar origin Point = Point{x: 0, y: 0}",
			"origin.x = 1",
			"exit(int32(origin.y))",
		),
		[]spanned{{"cannot assign to field of immutable variable origin", "origin"}},
	},
	{
		"field assigned the wrong type",
		withTypes(
			pointDecls,
			"mut p Point",
			"p.x = \"one\"",
			"code, _ := int32(p.x)",
			"exit(code)",
		),
		[]spanned{{"cannot use string as int64 in assignment to field x of p", "\"one\""}},
	},
	{
		"increment of a struct field",
		withTypes(pointDecls, "mut l Line", "l.a++", "code, _ := int32(l.a.x)", "exit(code)"),
		[]spanned{{"operator ++ is not defined on Point", "l.a"}},
	},
	{
		"compound on a string field",
		withTypes(pointDecls, "mut g Grid", "g.name += \"x\"", "print(g.name)"),
		[]spanned{{"operator += is not defined on string", "g.name"}},
	},
	{
		"byte of a string field written",
		withTypes(pointDecls, "mut g Grid", "g.name[0] = 65", "print(g.name)"),
		[]spanned{{"cannot assign to an element of a string; strings are immutable", "g.name[0]"}},
	},
	{
		"struct with a string field ordered",
		withTypes(pointDecls, "g := Grid{cells: [3]int8{0, 0, 0}, name: \"\"}", "if g > g {", "}"),
		[]spanned{{"operator > is not defined on Grid", "g > g"}},
	},
	{
		"struct ordered", withTypes(pointDecls, "p := Point{x: 1, y: 2}", "if p < p {", "}"),
		[]spanned{{"operator < is not defined on Point", "p < p"}},
	},
	{
		"different structs compared",
		withTypes(
			pointDecls+"\n\ntype Other struct {\n    x int64\n    y int64\n}",
			"p := Point{x: 1, y: 2}",
			"o := Other{x: 1, y: 2}",
			"if p == o {",
			"}",
		),
		[]spanned{{"mismatched types Point and Other in ==", "p == o"}},
	},
	{
		"struct assigned to another struct",
		withTypes(
			pointDecls+"\n\ntype Other struct {\n    x int64\n    y int64\n}",
			"mut p Point",
			"p = Other{x: 1, y: 2}",
			"code, _ := int32(p.x)",
			"exit(code)",
		),
		[]spanned{{"cannot use Other as Point in assignment to p", "Other{x: 1, y: 2}"}},
	},
	{
		"struct compared with a constant",
		withTypes(pointDecls, "p := Point{x: 1, y: 2}", "if p == 1 {", "}"),
		[]spanned{{"mismatched types Point and int64 in ==", "p == 1"}},
	},
	{
		"struct in arithmetic",
		withTypes(pointDecls, "p := Point{x: 1, y: 2}", "q := p + p", "exit(q.x)"),
		[]spanned{{"operator + is not defined on Point", "p"}},
	},
	{
		"struct as a condition", withTypes(pointDecls, "p := Point{x: 1, y: 2}", "if p {", "}"),
		[]spanned{{"condition must be a bool, got Point", "p"}},
	},
	{
		"struct ranged over",
		withTypes(pointDecls, "p := Point{x: 1, y: 2}", "for i := range p {", "    exit(i)", "}"),
		[]spanned{{"cannot range over Point", "p"}},
	},
	{
		"struct indexed", withTypes(pointDecls, "p := Point{x: 1, y: 2}", "exit(p[0])"),
		[]spanned{{"cannot index Point", "p"}},
	},
	{
		"len of a struct", withTypes(pointDecls, "p := Point{x: 1, y: 2}", "exit(len(p))"),
		[]spanned{{"len expects an array, a string or a view, got Point", "p"}},
	},
	{
		"struct converted", withTypes(pointDecls, "p := Point{x: 1, y: 2}", "exit(int64(p))"),
		[]spanned{{"cannot convert Point to int64", "int64(p)"}},
	},
	{
		"struct passed to exit", withTypes(pointDecls, "p := Point{x: 1, y: 2}", "exit(p)"),
		[]spanned{{"cannot use Point as int32 in argument to exit", "p"}},
	},
	{
		"global literal with a mutable variable",
		withTypes(pointDecls+"\n\nmut k = 1\n\nvar p = Point{x: k, y: 1}", "exit(p.x)"),
		[]spanned{{"p must be initialized with a constant expression", "Point{x: k, y: 1}"}},
	},
	{
		"immutable global without a value",
		withTypes(pointDecls+"\n\nvar p Point", "code, _ := int32(p.x)", "exit(code)"),
		[]spanned{{"immutable variable p needs a value", "p"}},
	},
	{
		"field length naming a mutable global",
		withTypes("type B struct {\n    a [n]int64\n}\n\nmut n = 2", "mut b B", "exit(len(b.a))"),
		[]spanned{{"array length in a struct field must not name variable n", "n"}},
	},
	{
		"field length not constant",
		withTypes("type B struct {\n    a [true]int64\n}", "mut b B", "exit(len(b.a))"),
		[]spanned{{"array length must be a constant integer", "true"}},
	},
	{
		"unknown field type",
		withTypes("type B struct {\n    a nope\n}", "mut b B", "exit(len(b.a))"),
		[]spanned{{"unknown type: nope", "nope"}},
	},
	{
		"unproven divisor field",
		withTypes(
			pointDecls,
			"p := Point{x: 6, y: 2}",
			"code, _ := int32(p.x / p.y)",
			"exit(code)",
		),
		[]spanned{
			{"divisor p.y may be zero; check p.y != 0 first, or use q, ok := ... / p.y", "p.y"},
		},
	},
	{
		"guarded divisor field of a mutable variable",
		withTypes(
			pointDecls,
			"mut p := Point{x: 6, y: 2}",
			"if p.y != 0 {",
			"    code, _ := int32(p.x / p.y)",
			"    exit(code)",
			"}",
		),
		[]spanned{{"divisor may be zero; use q, ok := ... / ...", "p.y"}},
	},
	{
		"guarded divisor field in a compound",
		withTypes(
			pointDecls,
			"mut p Point",
			"if p.y > 0 {",
			"    p.x /= p.y",
			"}",
			"code, _ := int32(p.x)",
			"exit(code)",
		),
		[]spanned{{"divisor may be zero", "p.y"}},
	},
	{
		"unproven index field of a mutable variable",
		withTypes(
			pointDecls,
			"mut p := Point{x: 1, y: 1}",
			"a := [2]int64{1, 2}",
			"if p.y >= 0 && p.y < 2 {",
			"    code, _ := int32(a[p.y])",
			"    exit(code)",
			"}",
		),
		[]spanned{{"index may be out of range; use v, ok := ...[...]", "p.y"}},
	},
	{
		"unproven index written into a field",
		withTypes(pointDecls, "mut g Grid", "i := 3", "g.cells[i] = 1", "exit(g.cells[0])"),
		[]spanned{{"index 3 is out of range for [3]int8", "i"}},
	},
	{
		"constant field index out of range",
		withTypes(pointDecls, "g := Grid{cells: [3]int8{1, 2, 3}, name: \"\"}", "exit(g.cells[3])"),
		[]spanned{{"index 3 is out of range for [3]int8", "3"}},
	},
	{
		"field length naming an immutable global",
		withTypes(
			"type B struct {\n    a [n * 2]int64\n}\n\nvar n = 2",
			"mut b B",
			"exit(len(b.a))",
		),
		[]spanned{{"array length in a struct field must not name variable n", "n"}},
	},
	{
		"field length naming a field of a global",
		withTypes(
			"type B struct {\n    a [len(g.cells)]int64\n}\n\nvar g = Grid{cells: [3]int8{1, 2, 3}, name: \"\"}\n\n"+pointDecls,
			"mut b B",
			"exit(len(b.a))",
		),
		[]spanned{{"array length in a struct field must not name variable g", "g"}},
	},
	{
		"inner field length naming a mutable global",
		withTypes(
			"type B struct {\n    a [2][1 + n]int64\n}\n\nmut n int64",
			"mut b B",
			"exit(len(b.a))",
		),
		[]spanned{{"array length in a struct field must not name variable n", "n"}},
	},
	{
		"global literal of a struct whose length names a global",
		withTypes(
			"var g = B{a: [2]int64{1, 2}}\n\nvar n = 2\n\ntype B struct {\n    a [n]int64\n}",
			"exit(g.a[1])",
		),
		[]spanned{{"array length in a struct field must not name variable n", "n"}},
	},
	{
		"checked division of a known field divisor",
		withTypes(
			pointDecls,
			"p := Point{x: 6, y: 0}",
			"q, ok := p.x / 0",
			"if ok {",
			"    exit(q)",
			"}",
		),
		[]spanned{{"division by zero", "0"}},
	},
}

// TestRejectsStructErrors checks each misuse is reported as listed, covering
// the right source.
func TestRejectsStructErrors(t *testing.T) {
	for _, tt := range structErrorCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != len(tt.want) {
				t.Fatalf("got %v, want %v", diags, tt.want)
			}
			for i, d := range diags {
				if d.Message != tt.want[i].msg {
					t.Errorf("message %d = %q, want %q", i, d.Message, tt.want[i].msg)
				}
				if got := tt.src[d.Span.Start:d.Span.End]; got != tt.want[i].at {
					t.Errorf("diagnostic %d covers %q, want %q", i, got, tt.want[i].at)
				}
			}
		})
	}
}

// cycleCases are structs that contain themselves by value, with the type the
// cycle is reported at and the notes it must carry, in order.
var cycleCases = []struct {
	name, decls, at string
	notes           []string
}{
	{"itself", "type A struct {\n    a A\n}", "A", []string{"A refers to A"}},
	{
		"itself after a field",
		"type A struct {\n    n int64\n    a A\n}",
		"A",
		[]string{"A refers to A"},
	},
	{"through an array", "type A struct {\n    a [2]A\n}", "A", []string{"A refers to A"}},
	{"through an empty array", "type A struct {\n    a [0]A\n}", "A", []string{"A refers to A"}},
	{
		"through nested arrays",
		"type A struct {\n    a [1][2][3]A\n}",
		"A",
		[]string{"A refers to A"},
	},
	{
		"two types",
		"type A struct {\n    b B\n}\n\ntype B struct {\n    a A\n}",
		"A",
		[]string{"A refers to B", "B refers to A"},
	},
	{
		"three types, found from the middle",
		"type C struct {\n    a A\n}\n\ntype A struct {\n    b [1]B\n}\n\ntype B struct {\n    c C\n}",
		"C",
		[]string{"C refers to A", "A refers to B", "B refers to C"},
	},
	{
		"a cycle reached from outside it",
		"type O struct {\n    a A\n}\n\ntype A struct {\n    b B\n}\n\ntype B struct {\n    a A\n}",
		"A",
		[]string{"A refers to B", "B refers to A"},
	},
}

// TestRejectsStructsContainingThemselves checks each cycle is reported once,
// at the struct it is found from, with a note for each field on the way
// round, and that checking ends.
func TestRejectsStructsContainingThemselves(t *testing.T) {
	for _, tt := range cycleCases {
		t.Run(tt.name, func(t *testing.T) {
			src := withTypes(tt.decls)
			_, diags := checkTree(t, src)
			if len(diags) != 1 {
				t.Fatalf("got %v, want one cycle", diags)
			}
			d := diags[0]
			if want := "invalid recursive type " + tt.at; d.Message != want {
				t.Errorf("message = %q, want %q", d.Message, want)
			}
			if got := src[d.Span.Start:d.Span.End]; got != tt.at {
				t.Errorf("covers %q, want %q", got, tt.at)
			}
			if len(d.Related) != len(tt.notes) {
				t.Fatalf("notes %v, want %v", d.Related, tt.notes)
			}
			for i, n := range d.Related {
				if n.Message != tt.notes[i] {
					t.Errorf("note %d = %q, want %q", i, n.Message, tt.notes[i])
				}
			}
		})
	}
}

// A struct that holds itself and is then compared, ranged over and written
// still checks to an end, reporting the cycle and nothing more.
func TestCyclicStructUsedEverywhereStillEnds(t *testing.T) {
	src := withTypes(
		"type A struct {\n    b [1]B\n    n int64\n}\n\ntype B struct {\n    a A\n}",
		"mut a A", "a.n = 1", "if a == a {", "}", "code, _ := int32(a.b[0].a.n)",
		"exit(code)",
	)
	_, diags := checkTree(t, src)
	if len(diags) != 1 || diags[0].Message != "invalid recursive type A" {
		t.Errorf("got %v", diags)
	}
}

// A struct whose array length depends on itself, through a literal of its
// own type, is reported rather than looped on, once: a length that cannot
// be worked out because of the cycle follows from it and is not reported.
// A length can no longer reach itself through a global, since it names no
// variable.
func TestRejectsStructsDependingOnThemselvesThroughLengths(t *testing.T) {
	tests := []struct{ name, src, first string }{
		{
			"through len of a call with the wrong arguments",
			withTypes(
				"type Box struct {\n    items [len()]int64\n}",
				"mut b Box",
				"exit(len(b.items))",
			),
			"len expects 1 argument(s), got 0",
		},
		{
			"through a literal of its own type",
			withTypes(
				"type Box struct {\n    items [len(Box{}.items)]int64\n}",
				"mut b Box",
				"exit(len(b.items))",
			),
			"invalid recursive type Box",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != 1 || diags[0].Message != tt.first {
				t.Errorf("got %v, want only %q", diags, tt.first)
			}
		})
	}
}

// TestDeepAndWideStructs checks structs nested hundreds deep, a struct with
// hundreds of fields, and a chain of structs each holding an array of the
// next, all check cleanly and record the right types.
func TestDeepAndWideStructs(t *testing.T) {
	const depth = 300
	var decls strings.Builder
	for i := range depth {
		fmt.Fprintf(&decls, "type S%d struct {\n    next S%d\n    n int64\n}\n\n", i, i+1)
	}
	fmt.Fprintf(&decls, "type S%d struct {\n    n int32\n}", depth)
	chain := "s" + strings.Repeat(".next", depth) + ".n"
	file, diags := checkTree(
		t,
		withTypes(decls.String(), "mut s S0", chain+" = 7", "exit("+chain+")"),
	)
	if len(diags) > 0 {
		t.Fatalf("deep: got %v", diags[0])
	}
	exit := file.Decls[len(file.Decls)-1].(*ast.Func).Body[2].(*ast.ExprStmt).X.(*ast.Call)
	if got := ast.TypeOf(exit.Args[0]); got != types.Int32 {
		t.Errorf("a field %d deep has type %d", depth, got)
	}

	const width = 500
	fields, inits := make([]string, 0, width), make([]string, 0, width)
	for i := range width {
		fields = append(fields, fmt.Sprintf("f%d uint8", i))
		inits = append(inits, fmt.Sprintf("f%d: %d", i, i%256))
	}
	wide := "type W struct {\n    " + strings.Join(fields, "\n    ") + "\n}"
	if _, diags := checkTree(
		t,
		withTypes(wide, "w := W{"+strings.Join(inits, ", ")+"}", "exit(w.f499)"),
	); len(
		diags,
	) > 0 {
		t.Errorf("wide: got %v", diags[0])
	}
	missing := withTypes(wide, "w := W{"+strings.Join(inits[1:], ", ")+"}", "exit(w.f1)")
	if _, diags := checkTree(t, missing); len(diags) != 1 ||
		diags[0].Message != "W literal is missing field f0" {
		t.Errorf("wide, missing f0: got %v", diags)
	}
}

// The literal and field types the checker records are the struct and field
// types, and a struct's ID is the one its declaration records.
func TestRecordsStructTypes(t *testing.T) {
	file, diags := checkTree(
		t,
		withTypes(
			pointDecls,
			"l := Line{a: Point{x: 1, y: 2}, b: Point{x: 3, y: 4}}",
			"y := l.b.y",
			"if y == 4 {",
			"}",
		),
	)
	if len(diags) > 0 {
		t.Fatalf("got %v", diags)
	}
	point := file.Decls[0].(*ast.TypeDecl).Type
	line := file.Decls[1].(*ast.TypeDecl).Type
	body := file.Decls[3].(*ast.Func).Body
	lit := body[0].(*ast.Var).Value.(*ast.CompositeLit)
	inner := lit.Elems[0].(*ast.KeyValue).Value
	sel := body[1].(*ast.Var).Value.(*ast.Selector)
	if lit.Type != line || ast.TypeOf(inner) != point || ast.TypeOf(sel.X) != point ||
		sel.Type != types.Int64 {
		t.Errorf(
			"types: literal %d, inner %d, l.b %d, l.b.y %d",
			lit.Type,
			ast.TypeOf(inner),
			ast.TypeOf(sel.X),
			sel.Type,
		)
	}
	if file.Types.Name(point) != "Point" || !file.Types.IsStruct(line) {
		t.Error("the table does not describe the declared structs")
	}
}
