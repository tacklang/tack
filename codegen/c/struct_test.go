package c_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// structProgram puts declarations before a main function made of lines.
func structProgram(decls string, lines ...string) string {
	return "package main\n\n" + decls + "\n\n" + strings.TrimPrefix(
		program(lines...),
		"package main\n\n",
	)
}

// structDecls declares the structs most cases below use.
const structDecls = "type Point struct {\n    x int64\n    y int64\n}\n\n" +
	"type Line struct {\n    a Point\n    b Point\n}\n\n" +
	"type Bag struct {\n    items [3]Point\n    name string\n    n uint8\n}"

// structCases are struct programs with what each must print and the status
// it must exit with.
var structCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"assignment copies",
		structProgram(
			structDecls,
			"mut p := Point{x: 1, y: 2}",
			"mut q := p",
			"q.x = 9",
			"p.y = 7",
			"code, fits := int32(p.x * 1000 + p.y * 100 + q.x * 10 + q.y)",
			"if fits { exit(code) }",
		),
		"",
		1792 % 256,
	},
	{
		"passing and returning copy",
		structProgram(
			structDecls+"\n\nfunc moved(p Point) Point {\n    mut c := p\n    c.x += 10\n    return c\n}",
			"p := Point{x: 1, y: 2}",
			"q := moved(p)",
			"code, fits := int32(p.x * 100 + q.x)",
			"if fits { exit(code) }",
		),
		"",
		111,
	},
	{
		"nested fields written",
		structProgram(
			structDecls,
			"mut l := Line{a: Point{x: 1, y: 2}, b: Point{x: 3, y: 4}}",
			"l.a.x = 5",
			"l.b.y *= 10",
			"l.b = l.a",
			"l.a.y--",
			"code, fits := int32(l.a.x * 10 + l.a.y + l.b.y * 3)",
			"if fits { exit(code) }",
		),
		"",
		57,
	},
	{
		"arrays of structs",
		structProgram(
			structDecls,
			"mut a := [3]Point{{x: 1, y: 1}, Point{x: 2, y: 2}, {y: 3, x: 3}}",
			"a[2].y = 30",
			"for i := range a {",
			"    a[i].x *= 2",
			"}",
			"mut sum := 0",
			"for _, p := range a {",
			"    sum += p.x + p.y",
			"}",
			"code, fits := int32(sum)",
			"if fits { exit(code) }",
		),
		"",
		2 + 1 + 4 + 2 + 6 + 30,
	},
	{
		"range walks a copy",
		structProgram(
			structDecls,
			"mut a := [2]Point{{x: 1, y: 0}, {x: 2, y: 0}}",
			"mut sum := 0",
			"for i, p := range a {",
			"    a[1].x = 100",
			"    sum += p.x + i",
			"}",
			"code, fits := int32(sum)",
			"if fits { exit(code) }",
		),
		"",
		4,
	},
	{
		"struct of arrays and strings",
		structProgram(
			structDecls,
			"mut b Bag",
			"b.items[1].y = 4",
			"b.name = \"bag\"",
			"b.n = 255",
			"b.n++",
			"print(b.name)",
			"for i := range b.items {",
			"    b.items[i].x = i",
			"}",
			"code, fits := int32(b.items[2].x * 10 + b.items[1].y + b.n + len(b.name))",
			"if fits { exit(code) }",
		),
		"bag",
		27,
	},
	{
		"checked element of a field out of range",
		structProgram(
			structDecls,
			"b := Bag{items: [3]Point{{x: 1, y: 2}, {x: 3, y: 4}, {x: 5, y: 6}}, name: \"\", n: 0}",
			"mut i := 1",
			"i = 7",
			"p, ok := b.items[i]",
			"if !ok && p == (Point{x: 0, y: 0}) {",
			"    exit(9)",
			"}",
		),
		"",
		9,
	},
	{
		"checked element of a field in range",
		structProgram(
			structDecls,
			"b := Bag{items: [3]Point{{x: 1, y: 2}, {x: 3, y: 4}, {x: 5, y: 6}}, name: \"\", n: 0}",
			"mut i := 7",
			"i = 2",
			"p, ok := b.items[i]",
			"if ok {",
			"    code, fits := int32(p.y)",
			"    if fits { exit(code) }",
			"}",
		),
		"",
		6,
	},
	{
		"equality",
		structProgram(
			structDecls+"\n\ntype E struct {}\n\ntype W struct {\n    e E\n    l [2]Line\n    ok bool\n}",
			"p := Point{x: 1, y: 2}",
			"l := Line{a: p, b: p}",
			"w := W{e: E{}, l: [2]Line{l, l}, ok: true}",
			"mut v := w",
			"mut n := 0",
			"if p == l.b {",
			"    n += 1",
			"}",
			"if w == v {",
			"    n += 2",
			"}",
			"v.l[1].b.y = 3",
			"if w != v {",
			"    n += 4",
			"}",
			"v.l[1].b.y = 2",
			"v.ok = false",
			"if w != v {",
			"    n += 8",
			"}",
			"if (E{}) == (E{}) && w.e == v.e {",
			"    n += 16",
			"}",
			"code, fits := int32(n)",
			"if fits { exit(code) }",
		),
		"",
		31,
	},
	{
		"zero values",
		structProgram(
			structDecls,
			"mut b Bag",
			"mut l Line",
			"print(b.name)",
			"if b.items[2] == l.a && b.n == 0 && len(b.name) == 0 && l == (Line{a: l.b, b: l.a}) {",
			"    exit(3)",
			"}",
		),
		"",
		3,
	},
	{
		"fields wrap",
		structProgram(
			"type S struct {\n    i int8\n    u uint16\n    w int64\n}",
			"mut s := S{i: 127, u: 0, w: -9223372036854775808}",
			"s.i++",
			"s.u--",
			"s.w -= 1",
			"s.i *= 3",
			"if s.i == -128 && s.u == 65535 && s.w == 9223372036854775807 {",
			"    exit(7)",
			"}",
		),
		"",
		7,
	},
	{
		"global structs",
		structProgram(
			structDecls+"\n\nvar home = Line{a: Point{x: 1, y: 2}, b: Point{y: 4, x: 3}}\n\n"+
				"var named = Bag{name: \"g\", n: 7, items: [3]Point{{x: 1, y: 1}, {x: 2, y: 2}, {x: 3, y: 3}}}\n\nmut cursor Point",
			"cursor.x = home.b.x",
			"cursor.y += named.items[2].y",
			"print(named.name)",
			"code, fits := int32(cursor.x * 100 + cursor.y * 10 + named.n + home.b.y)",
			"if fits { exit(code) }",
		),
		"g",
		(300 + 30 + 7 + 4) % 256,
	},
	{
		"field names that are C keywords",
		structProgram(
			"type C struct {\n    int int64\n    while bool\n    static uint8\n    unused int8\n}\n\ntype E struct {}",
			"mut c := C{int: 1, while: true, static: 2, unused: 3}",
			"c.int += c.static",
			"e := E{}",
			"if c.while && e == e {",
			"    code, fits := int32(c.int + c.unused)",
			"    if fits { exit(code) }",
			"}",
		),
		"",
		6,
	},
	{
		"field names that are macros tack.h defines or includes",
		structProgram(
			"type M struct {\n    TACK_H int64\n    TACK_STD_TACK_H int64\n    NULL int64\n    INT64_MAX int64\n}",
			"mut m := M{TACK_H: 1, TACK_STD_TACK_H: 2, NULL: 3, INT64_MAX: 0}",
			"m.INT64_MAX = m.TACK_H + m.TACK_STD_TACK_H + m.NULL",
			"code, fits := int32(m.INT64_MAX)",
			"if fits { exit(code) }",
		),
		"",
		6,
	},
	{
		"several results",
		structProgram(
			structDecls+"\n\nfunc ends(l Line) (Point, Point) {\n    return l.a, l.b\n}",
			"l := Line{a: Point{x: 1, y: 2}, b: Point{x: 3, y: 4}}",
			"a, b := ends(l)",
			"mut m Line",
			"m.b, m.a = ends(l)",
			"code, fits := int32(a.x + b.y * 10 + m.a.y * 100)",
			"if fits { exit(code) }",
		),
		"",
		(41 + 400) % 256,
	},
	{
		"several results into fields in a loop's post statement",
		structProgram(
			structDecls+"\n\nfunc step(p Point) (int64, int64) {\n    return p.x + 1, p.y + p.x\n}",
			"mut p Point",
			"for ; p.x < 4; p.x, p.y = step(p) {",
			"}",
			"code, fits := int32(p.y)",
			"if fits { exit(code) }",
		),
		"",
		6,
	},
	{
		"checked division of fields",
		structProgram(
			structDecls,
			"p := Point{x: 7, y: 0}",
			"q, mut ok := p.x / p.y",
			"mut r := Point{x: 7, y: 2}",
			"r.x, ok = r.x % r.y",
			"if !ok {",
			"    exit(1)",
			"}",
			"code, fits := int32(q * 10 + r.x)",
			"if fits { exit(code) }",
		),
		"",
		1,
	},
	{
		"field of a call and of a literal",
		structProgram(
			structDecls+"\n\nfunc mk(n int64) Line {\n    return Line{a: Point{x: n, y: n + 1}, b: Point{x: 0, y: 0}}\n}",
			"code, fits := int32(mk(4).a.y + Point{x: 1, y: 2}.y + [1]Point{{x: 3, y: 0}}[0].x)",
			"if fits { exit(code) }",
		),
		"",
		10,
	},
	{
		"struct declared after the one holding it",
		"package main\n\ntype Outer struct {\n    in [2]Inner\n    last Inner\n}\n\ntype Inner struct {\n    v uint8\n}\n\n" +
			"func main() {\n    mut o Outer\n    o.in[1].v = 4\n    o.last = o.in[1]\n    if o.last == o.in[1] {\n        exit(o.last.v)\n    }\n}\n",
		"",
		4,
	},
}

// TestStructsAtRuntime builds each program with clang's undefined behavior
// sanitizer set to stop it, and checks what it prints and its exit status.
func TestStructsAtRuntime(t *testing.T) {
	for _, tt := range structCases {
		t.Run(tt.name, func(t *testing.T) {
			out, status := runSanitized(t, tt.src)
			if out != tt.stdout || status != tt.status {
				t.Errorf("got stdout %q, status %d; want %q, %d", out, status, tt.stdout, tt.status)
			}
		})
	}
}

// TestEqualOperandsExampleIsSanitizerClean builds examples/equal-operands
// with clang's warnings as errors and the address and undefined behavior
// sanitizers on, every finding fatal, and checks it exits 23, every check
// passed: an operand of a struct's, an array's or a union's equality that
// holds calls, which C cannot take the address of as rendered, is read
// through a temporary, compared rightly, and its calls run once each, in
// order.
func TestEqualOperandsExampleIsSanitizerClean(t *testing.T) {
	src := readFile(t, filepath.Join("..", "..", "examples", "equal-operands", "main.tck"))
	if _, status := runStatus(t, buildWith(t, src, sanitizers...)); status != 23 {
		t.Errorf("equal-operands exited %d, want 23", status)
	}
}

// A chain of structs hundreds deep, each holding the next, lowers to C
// that defines each before the one that holds it, and runs.
func TestDeeplyNestedStructsRun(t *testing.T) {
	const depth = 200
	var decls strings.Builder
	for i := range depth {
		fmt.Fprintf(&decls, "type S%d struct {\n    next S%d\n}\n\n", i, i+1)
	}
	fmt.Fprintf(&decls, "type S%d struct {\n    n int32\n}", depth)
	chain := "s" + strings.Repeat(".next", depth) + ".n"
	out, status := runSanitized(
		t,
		structProgram(
			decls.String(),
			"mut s S0",
			chain+" = 42",
			"t := s",
			"if t == s {",
			"    exit("+chain+")",
			"}",
		),
	)
	if out != "" || status != 42 {
		t.Errorf("got stdout %q, status %d; want 42", out, status)
	}
}

// A struct lowers to a typedef'd C struct named after it, with a member
// per field, defined after the types of its fields whatever the order they
// were declared in, and == calls a helper that compares field by field.
func TestStructLowering(t *testing.T) {
	got := generate(
		t,
		"package main\n\ntype A struct {\n    b B\n    c [2]B\n}\n\ntype B struct {\n    x int64\n}\n\n"+
			"func main() {\n    a := A{b: B{x: 1}, c: [2]B{{x: 2}, {x: 3}}}\n    if a == a {\n        code, fits := int32(a.c[1].x)\n        if fits { exit(code) }\n    }\n}\n",
	)
	for _, want := range []string{
		"typedef struct {\n    int64_t f_x;\n} tack_4_main1_B;",
		"    tack_4_main1_B f_b;\n",
		"} tack_4_main1_A;",
		"(tack_4_main1_A){.f_b = (tack_4_main1_B){.f_x = 1}, .f_c = ",
		"v_a.f_c.e[1].f_x",
		"return tack_eq_",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated C is missing %q:\n%s", want, got)
		}
	}
	b, arr, a := strings.Index(
		got,
		"} tack_4_main1_B;",
	), strings.Index(
		got,
		"tack_4_main1_B e[2];",
	), strings.Index(
		got,
		"} tack_4_main1_A;",
	)
	if b < 0 || arr < b || a < arr {
		t.Errorf("types not defined before their use: B at %d, [2]B at %d, A at %d", b, arr, a)
	}
}
