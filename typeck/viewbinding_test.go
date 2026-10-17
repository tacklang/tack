package typeck_test

import (
	"strings"
	"testing"
)

// bindingDecls declares the types the programs of the tests below use:
// Name, a struct holding a list, Expr, a union of a Name and a list, Cell,
// a union of an array and a list, and Arr, a union of an array alone,
// which holds no list and so is copied out of what holds it.
const bindingDecls = "package main\n\ntype Name struct {\n    bytes list[uint8]\n}\n\n" +
	"type Expr union {\n    name Name\n    word list[uint8]\n}\n\n" +
	"type Cell union {\n    arr [3]uint8\n    many list[uint8]\n}\n\n" +
	"type Arr union {\n    arr [3]uint8\n    none\n}\n\n" +
	"func nameBytes(n Name) []uint8 {\n    return n.bytes\n}\n\n"

// binding returns a program of bindingDecls and the function f, written
// as lines, with an empty main.
func binding(lines ...string) string {
	return bindingDecls + strings.Join(lines, "\n") + "\n\nfunc main() {\n}\n"
}

// TestAcceptsViewsThroughBindings checks views returned through variables
// that refer in place into a parameter that is not mut: a union pattern's
// binding, a range loop's element over owned elements, one inside the
// other, an array payload reached through a list's element, a binding
// given to a call returning a view, and a slice of one.
func TestAcceptsViewsThroughBindings(t *testing.T) {
	for _, tt := range []struct{ name, src string }{
		{"pattern binding", binding("func f(e Expr) []uint8 {", "    switch e {", "    case name(n):",
			"        return n.bytes", "    case word(w):", "        return w", "    }", "}")},
		{"range element", binding("func f(ws list[list[uint8]], or []uint8) []uint8 {",
			"    for _, w := range ws {", "        return w", "    }", "    return or", "}")},
		{"range element over a view", binding("func f(ws []list[uint8], or []uint8) []uint8 {",
			"    for _, w := range ws {", "        return w", "    }", "    return or", "}")},
		{"pattern binding inside a range element", binding("func f(es []Expr, or []uint8) []uint8 {",
			"    for _, e := range es {", "        switch e {", "        case word(w):", "            return w",
			"        default:", "        }", "    }", "    return or", "}")},
		{"array payload through a list's element", binding("func f(cs list[Cell], or []uint8) []uint8 {",
			"    for _, c := range cs {", "        switch c {", "        case arr(a):", "            return a[:]",
			"        default:", "        }", "    }", "    return or", "}")},
		{"binding given to a call", binding("func f(e Expr, or []uint8) []uint8 {", "    switch e {",
			"    case name(n):", "        return nameBytes(n)", "    default:", "        return or", "    }", "}")},
		{"slice of a binding", binding("func f(e Expr, or []uint8) []uint8 {", "    switch e {",
			"    case word(w):", "        return w[0:0]", "    default:", "        return or", "    }", "}")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if diags := check(t, tt.src); len(diags) > 0 {
				t.Fatalf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// TestRejectsViewsThroughBindings checks views through such variables when
// what they refer into is not what a returned view can point into: a mut
// parameter or receiver, a local, a call's result, and an array payload
// of a union the function holds its own copy of, and through a range
// element that is a copy, which is a local of its own.
func TestRejectsViewsThroughBindings(t *testing.T) {
	const rule = "; a returned view can only point into what a parameter that is not mut holds"
	for _, tt := range []struct{ name, src, want string }{
		{
			"mut receiver", binding("func (mut e Expr) f(or []uint8) []uint8 {", "    switch e {",
				"    case word(w):", "        return w", "    default:", "        return or", "    }", "}"),
			"cannot return a view of w: w refers into e, and e is a mut parameter" + rule,
		},
		{
			"mut list ranged over", binding("func f(mut ws list[list[uint8]], or []uint8) []uint8 {",
				"    for _, w := range ws {", "        return w", "    }", "    return or", "}"),
			"cannot return a view of w: w refers into ws, and ws is a mut parameter" + rule,
		},
		{
			"local switched on", binding("func f(e Expr, or []uint8) []uint8 {", "    mut x := Expr.word(make(list[uint8], c_allocator))",
				"    x = e", "    switch x {", "    case word(w):", "        return w", "    default:", "        return or", "    }", "}"),
			"cannot return a view of w: w refers into x, and x is a local variable" + rule,
		},
		{
			"call switched on", binding("func g() Expr {", "    return Expr.word(make(list[uint8], c_allocator))", "}", "",
				"func f(or []uint8) []uint8 {", "    switch g() {", "    case word(w):", "        return w", "    default:",
				"        return or", "    }", "}"),
			"cannot return a view of w: w refers into g(), and it is a temporary" + rule,
		},
		{
			"mut receiver's binding given to a call", binding("func (mut e Expr) f(or []uint8) []uint8 {",
				"    switch e {", "    case name(n):", "        return nameBytes(n)", "    default:", "        return or", "    }", "}"),
			"cannot return nameBytes(n): the view it returns may point into n, and n refers into e, and e is a mut parameter" + rule,
		},
		{
			"array payload of a copy", binding("func (u Arr) f(or []uint8) []uint8 {", "    switch u {",
				"    case arr(a):", "        return a[:]", "    default:", "        return or", "    }", "}"),
			"cannot return a view of a[:]: the function holds its own copy of u, and so of the array, which ends when it returns; " +
				"an array is returned as a view only when it is reached through an element of a list or a view",
		},
		{
			"copied range element", binding("func f(as [][3]uint8, or []uint8) []uint8 {", "    for _, a := range as {",
				"        return a[:]", "    }", "    return or", "}"),
			"cannot return a view of a[:]: a is a local variable" + rule,
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
