package typeck_test

import (
	"strings"
	"testing"
)

// viewsAsAny returns a program importing std/fmt and std/text, declaring
// P, a struct, B, a struct holding a list with a method bytes returning
// a view of it, one, taking an any, many, taking ...any, both, taking a
// mut list and an any, and bothB, taking a mut B and an any, then decls,
// and a main whose body is lines.
func viewsAsAny(decls string, lines ...string) string {
	return "package main\n\nimport fmt from \"std/fmt\"\nimport text from \"std/text\"\n\n" +
		"type P struct {\n    d int64\n}\n\n" +
		"type B struct {\n    l list[uint8]\n}\n\n" +
		"func (b B) bytes() []uint8 {\n    return b.l\n}\n\n" +
		"func one(a any) {\n}\n\n" +
		"func many(xs ...any) {\n}\n\n" +
		"func both(mut l list[uint8], a any) {\n}\n\n" +
		"func bothB(mut b B, a any) {\n}\n\n" +
		"func keep(t text.Text) {\n    fmt.printf(\"%s\", t.bytes())\n}\n\n" +
		decls +
		"func main() {\n    " + strings.Join(lines, "\n    ") + "\n}\n"
}

// anyViewAcceptedCases are views passed as any, and views a type switch
// binds, that the checker accepts.
var anyViewAcceptedCases = []struct{ name, src string }{
	{"a view parameter", viewsAsAny(
		"func on(v []uint8, w []string) {\n    one(v)\n    many(v, w, 1)\n}\n\n",
		"w := [1]string{\"x\"}",
		"on([]uint8(\"s\"), w)",
	)},
	{"slices of a list, an array, a view and a string", viewsAsAny(
		"func on(v []int64) {\n    many(v[:], v[0:0])\n}\n\n",
		"mut l := make(list[uint8], c_allocator)",
		"a := [2]int64{1, 2}",
		"s := \"str\"",
		"many(l[:], l[0:0], a[:], a[1:], s[1:])",
		"on(a)",
	)},
	{"views of every basic type", viewsAsAny("",
		"a := [1]bool{true}", "b := [1]int8{1}", "c := [1]int16{1}", "d := [1]int32{1}",
		"e := [1]int64{1}", "f := [1]uint8{1}", "g := [1]uint16{1}", "h := [1]uint32{1}",
		"i := [1]uint64{1}", "j := [1]unsafe_size_t{1}", "k := [1]unsafe_uintptr_t{1}",
		"m := [1]string{\"s\"}",
		"many(a[:], b[:], c[:], d[:], e[:], f[:], g[:], h[:], i[:], j[:], k[:], m[:])",
	)},
	{"calls returning a view", viewsAsAny(
		"func tail(v []uint8) []uint8 {\n    return v[len(v):]\n}\n\n",
		"b := B{l: make(list[uint8], c_allocator)}",
		"one(b.bytes())",
		"many(tail(b.l), tail(tail(b.bytes())), b.bytes()[0:0])",
	)},
	{"a list borrowed beside a mut argument of another", viewsAsAny("",
		"mut l := make(list[uint8], c_allocator)",
		"m := make(list[uint8], c_allocator)",
		"both(mut l, m[:])",
		"b := B{l: make(list[uint8], c_allocator)}",
		"mut c := B{l: make(list[uint8], c_allocator)}",
		"bothB(mut c, b.bytes())",
	)},
	{"an array beside a mut argument of itself, copied", viewsAsAny(
		"func set(mut a [2]uint8, v any) {\n    a[0] = 1\n}\n\n",
		"mut a := [2]uint8{0, 0}",
		"set(mut a, a[:])",
	)},
	{"printf takes a []uint8 for %s and %v", viewsAsAny("",
		"a := [2]uint8{'o', 'k'}",
		"t := text.new(c_allocator)",
		"fmt.printf(\"%s %v %s\\n\", a[:], a[:], t.bytes())",
	)},
	{"a type switch binding each view", viewsAsAny(
		"func on(a any) int64 {\n"+
			"    switch v := a.(type) {\n"+
			"    case []uint8:\n"+
			"        if len(v) > 0 && v[0] == 'x' {\n            return 1\n        }\n"+
			"        c, ok := v[1]\n"+
			"        if ok {\n            one(c)\n        }\n"+
			"        for i, x := range v {\n            one(v[i])\n            one(x)\n        }\n"+
			"        one(v)\n        if len(v) > 1 {\n            many(v, v[1:])\n        }\n"+
			"        fmt.write(v)\n        fmt.printf(\"%s %v\", v, v)\n"+
			"        defer {\n            one(v)\n        }\n"+
			"        return len(v)\n"+
			"    case []string, []int64:\n        one(v)\n        return 2\n"+
			"    case []unsafe_uintptr_t:\n        return len(v)\n"+
			"    case string:\n        return len(v)\n"+
			"    }\n"+
			"    return 0\n}\n\n",
		"a := [1]uint8{'x'}",
		"one(on(a[:]))",
	)},
}

// TestAcceptsViewsAsAny checks each of anyViewAcceptedCases has no
// diagnostics.
func TestAcceptsViewsAsAny(t *testing.T) {
	for _, tt := range anyViewAcceptedCases {
		t.Run(tt.name, func(t *testing.T) {
			if diags := checkWithStd(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// anyViewErrorCases are misuses of views passed as any, or bound by a
// type switch, each with the diagnostics it gives, in order.
var anyViewErrorCases = []struct {
	name string
	src  string
	want []string
}{
	{
		"a view of structs",
		viewsAsAny("", "ps := [1]P{P{d: 1}}", "one(ps[:])"),
		[]string{
			"cannot pass []P as any: only the builtin types and views of them can be passed as any for now",
		},
	},
	{
		"a view of anys",
		viewsAsAny("func on(xs ...any) {\n    one(xs)\n}\n\n"),
		[]string{
			"cannot pass []any as any: only the builtin types and views of them can be passed as any for now",
		},
	},
	{
		"a list not sliced",
		viewsAsAny("", "l := make(list[uint8], c_allocator)", "one(l)"),
		[]string{
			"cannot pass list[uint8] as any: only the builtin types and views of them can be passed as any for now",
		},
	},
	{
		"an array not sliced",
		viewsAsAny("", "a := [1]uint8{1}", "many(a)"),
		[]string{
			"cannot pass [1]uint8 as any: only the builtin types and views of them can be passed as any for now",
		},
	},
	{
		"a string's bytes",
		viewsAsAny("", "one([]uint8(\"s\"))"),
		[]string{"[]uint8(...) can only be passed to a []uint8 parameter"},
	},
	{
		"a slice of a list beside mut of it",
		viewsAsAny("", "mut l := make(list[uint8], c_allocator)", "both(mut l, l[:])"),
		[]string{
			"cannot pass l alongside mut l in this call: a list is not copied for a call, " +
				"and a mut argument must be the only way to reach l during it",
		},
	},
	{
		"a view result beside mut of what it views",
		viewsAsAny("", "mut b := B{l: make(list[uint8], c_allocator)}", "bothB(mut b, b.bytes())"),
		[]string{
			"cannot pass b.bytes() alongside mut b in this call: the view it returns may point into b, " +
				"and a mut argument must be the only way to reach b during the call",
		},
	},
	{
		"a slice of a temporary list",
		viewsAsAny(
			"func mk() list[uint8] {\n    return make(list[uint8], c_allocator)\n}\n\n",
			"one(mk()[:])",
		),
		[]string{"cannot slice a temporary list; store it in a variable first"},
	},
	{
		"printf %d of a []uint8",
		viewsAsAny("", "a := [1]uint8{1}", "fmt.printf(\"%d\", a[:])"),
		[]string{"printf verb %d does not take []uint8"},
	},
	{
		"printf %v of a []int64",
		viewsAsAny("", "a := [1]int64{1}", "fmt.printf(\"%v\", a[:])"),
		[]string{"printf verb %v does not take []int64"},
	},
	{
		"printf %s of a []string",
		viewsAsAny("", "a := [1]string{\"s\"}", "fmt.printf(\"%s\", a[:])"),
		[]string{"printf verb %s does not take []string"},
	},
	{
		"a case naming a view of structs",
		viewsAsAny("func on(a any) {\n    switch a.(type) {\n    case []P:\n    }\n}\n\n"),
		[]string{
			"an any never holds []P: only the builtin types and views of them can be passed as any for now",
		},
	},
	{
		"a view named twice",
		viewsAsAny(
			"func on(a any) {\n    switch a.(type) {\n    case []uint8:\n    case string, []uint8:\n    }\n}\n\n",
		),
		[]string{"[]uint8 appears twice in this type switch"},
	},
	{
		"an assertion to a view",
		viewsAsAny("func on(a any) {\n    v, ok := a.([]uint8)\n    one(v)\n    one(ok)\n}\n\n"),
		[]string{
			"a type assertion cannot give []uint8, a view; ask with a type switch, as in switch v := a.(type)",
		},
	},
	{
		"a case's view declared into a local",
		viewsAsAny("func on(a any) {\n    switch v := a.(type) {\n    case []uint8:\n" +
			"        w := v\n        one(w)\n    }\n}\n\n"),
		[]string{
			"cannot declare w as []uint8; a view can only be the type of a parameter or of a result",
		},
	},
	{
		"a case's view returned",
		viewsAsAny(
			"func on(a any, d []uint8) []uint8 {\n    switch v := a.(type) {\n    case []uint8:\n" +
				"        return v\n    }\n    return d\n}\n\n",
		),
		[]string{
			"cannot return a view of v: v is a local variable; " +
				"a returned view can only point into what a parameter that is not mut holds",
		},
	},
	{
		"a case's view assigned through",
		viewsAsAny("func on(a any) {\n    switch v := a.(type) {\n    case []uint8:\n" +
			"        v[0] = 1\n        one(v)\n    }\n}\n\n"),
		[]string{"cannot assign to element of immutable variable v"},
	},
	{
		"a case's view indexed unproven",
		viewsAsAny("func on(a any) {\n    switch v := a.(type) {\n    case []uint8:\n" +
			"        one(v[0])\n    }\n}\n\n"),
		[]string{"index 0 may be out of range; check len(v) > 0 first, or use v, ok := ...[0]"},
	},
}

// TestRejectsViewsAsAnyMisuse checks each of anyViewErrorCases gives
// exactly its diagnostics.
func TestRejectsViewsAsAnyMisuse(t *testing.T) {
	for _, tt := range anyViewErrorCases {
		t.Run(tt.name, func(t *testing.T) {
			diags := checkWithStd(t, tt.src)
			got := make([]string, len(diags))
			for i, d := range diags {
				got[i] = d.Message
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
