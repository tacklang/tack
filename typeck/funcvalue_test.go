package typeck_test

import (
	"strings"
	"testing"
)

// funcProgram returns a program declaring the functions and types the
// function value tests use, with a function f, taking params, whose body
// is lines.
func funcProgram(params string, lines ...string) string {
	return "package main\n\n" + funcHelpers + "func f(" + params + ") {\n    " +
		strings.Join(lines, "\n    ") + "\n}\n\nfunc main() {\n}\n"
}

// funcHelpers declares what funcProgram's bodies use: functions of
// several signatures, a struct holding a list, a struct holding a
// function that takes it, and a union with a function as a payload.
const funcHelpers = `type Bag struct {
    items list[int64]
}

type Node struct {
    n int64
    visit func(Node) int64
}

type Handler union {
    call func(int64) int64
    none
}

func double(x int64) int64 {
    return x *% 2
}

func bump(mut n int64) {
    n = n +% 1
}

func total(v []int64) int64 {
    return len(v)
}

func grow(mut l list[int64], x int64) error {
    return append(mut l, x)
}

func fresh() list[int64] {
    return make(list[int64], c_allocator)
}

func pair() (list[int64], error) {
    return make(list[int64], c_allocator), noerror
}

func sizeOf(b Bag) int64 {
    return len(b.items)
}

func visitor(n Node) int64 {
    return n.n
}

func higher(g func(int64) int64, x int64) int64 {
    return g(x)
}

`

// TestAcceptsFuncValues checks what the checker allows of function
// values: every place one can be held, calls through them with every
// kind of argument and result, a struct holding a function of itself,
// functions taking functions, and the checked forms with ok checked.
func TestAcceptsFuncValues(t *testing.T) {
	for _, tt := range []struct{ name, src string }{
		{"a local and a parameter", funcProgram("g func(int64) int64",
			"h := double", "if g(1) +% h(2) > 0 {", "}")},
		{"a mut parameter reassigned", funcProgram("mut g func(int64) int64",
			"g = double", "if g(1) > 0 {", "}")},
		{"mut arguments and views", funcProgram("mut l list[int64]",
			"inc := bump", "mut n := 0", "inc(mut n)", "sum := total", "push := grow",
			"err := push(mut l, n)", "if err.code != 0 || sum(l) > 0 {", "}")},
		{"owned results, several, with their error checked", funcProgram("",
			"mk := fresh", "l := mk()", "two := pair", "m, err := two()", "if err.code != 0 {", "    return", "}",
			"if len(l) +% len(m) > 0 {", "}")},
		{"a borrowed owned argument", funcProgram("b Bag", "size := sizeOf", "if size(b) > 0 {", "}")},
		{"a struct holding a function of itself", funcProgram("",
			"n := Node{n: 1, visit: visitor}", "v := n.visit", "if v(n) > 0 {", "}")},
		{"functions taking functions", funcProgram("",
			"h := higher", "if h(double, 1) > 0 {", "}")},
		{"an array, a list and a map", funcProgram("",
			"a := [2]func(int64) int64{double, double}", "mut l := make(list[func(int64) int64], c_allocator)",
			"e1 := append(mut l, double)", "mut m := make(map[string]func(int64) int64, c_allocator)",
			"e2 := put(mut m, \"d\", double)", "mut n := 0", "for _, g := range l {", "    n +%= g(1)", "}",
			"if has(m, \"d\") {", "    g := m[\"d\"]", "    n +%= g(1)", "}",
			"g := a[1]", "if e1.code != 0 || e2.code != 0 || n +% g(1) > 0 {", "}")},
		{"a union's payload", funcProgram("h Handler",
			"switch h {", "case call(g):", "    if g(1) > 0 {", "    }", "case none:", "}")},
		{"a checked pop, its value read once ok is", funcProgram("mut l list[func(int64) int64]",
			"g, ok := pop(mut l)", "if ok && g(1) > 0 {", "}")},
		{"a checked delete, its value read once ok is", funcProgram("mut m map[int64]func(int64) int64",
			"g, ok := delete(mut m, 1)", "if ok && g(1) > 0 {", "}")},
		{"deferred code", funcProgram("g func(int64) int64",
			"mut n := 0", "defer {", "    n = g(n)", "}", "n = 1")},
		{"a value passed to a generic function", "package main\n\ntype Sizer interface {\n    size() int64\n}\n\n" +
			"type Box struct {\n    n int64\n    scale func(int64) int64\n}\n\nfunc (b Box) size() int64 {\n" +
			"    s := b.scale\n    return s(b.n)\n}\n\nfunc measure[S Sizer](s S, g func(int64) int64) int64 {\n" +
			"    return g(s.size())\n}\n\nfunc double(x int64) int64 {\n    return x *% 2\n}\n\n" +
			"func main() {\n    if measure(Box{n: 1, scale: double}, double) > 0 {\n    }\n}\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if diags := check(t, tt.src); len(diags) > 0 {
				t.Fatalf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// TestRejectsFuncValues checks that a call through a function value
// keeps every rule a call by name keeps: a proof ends at a mut argument,
// a list cannot be read and passed as mut, ranged over and changed, or
// its element moved, an owned result cannot be discarded, a value held by
// an error or an ok cannot be read before it is checked, and a function
// type gives no zero value and no ==.
func TestRejectsFuncValues(t *testing.T) {
	for _, tt := range []struct{ name, src, want string }{
		{
			"a proof ended by a mut argument", funcProgram("mut l list[int64], i int64",
				"push := grow", "if 0 <= i && i < len(l) {", "    err := push(mut l, 1)", "    if err.code != 0 || l[i] > 0 {", "    }", "}"),
			"index i may be out of range; check 0 <= i && i < len(l) first, or use v, ok := ...[i]",
		},
		{"a list read and passed as mut", "package main\n\nfunc both(v []int64, mut l list[int64]) {\n}\n\n" +
			"func f(mut l list[int64]) {\n    g := both\n    g(l, mut l)\n}\n\nfunc main() {\n}\n", "cannot pass l alongside mut l in this call: a list is not copied for a call, and a mut argument " +
			"must be the only way to reach l during it"},
		{"a range changing its list", funcProgram("mut l list[int64]",
			"push := grow", "for _, x := range l {", "    err := push(mut l, x)", "    if err.code != 0 {", "    }", "}"), "l is being ranged over, so this loop cannot pass mut l"},
		{"an owned result dropped", funcProgram("", "mk := fresh", "mk()"), "mk returns list[int64], which cannot be discarded; bind it to a variable"},
		{"an owned result bound to _", funcProgram("", "two := pair", "_, err := two()", "if err.code != 0 {", "}"), "two returns list[int64], which cannot be discarded; bind it to a variable instead of _"},
		{"a result read before its error is checked", funcProgram("",
			"two := pair", "l, err := two()", "if len(l) > 0 {", "}", "if err.code != 0 {", "}"), "l cannot be read before err is checked; check err.code == 0 first"},
		{"a popped value read before ok", funcProgram("mut l list[func(int64) int64]",
			"g, ok := pop(mut l)", "if g(1) > 0 && ok {", "}"), "g cannot be read before ok is checked; check that ok is true first"},
		{"a temporary viewed", funcProgram("", "sum := total", "if sum(fresh()) > 0 {", "}"), "cannot view a temporary list; store it in a variable first"},
		{"a string where a byte view is", "package main\n\nfunc count(b []uint8) int64 {\n    return len(b)\n}\n\n" +
			"func main() {\n    c := count\n    if c(\"ab\") > 0 {\n    }\n}\n", "cannot use string as []uint8 in argument to c; view its bytes with []uint8(\"ab\")"},
		{"a function value as a type argument", "package main\n\ntype Sizer interface {\n    size() int64\n}\n\n" +
			"func measure[S Sizer](s S) int64 {\n    return s.size()\n}\n\nfunc double(x int64) int64 {\n    return x *% 2\n}\n\n" +
			"func main() {\n    if measure(double) > 0 {\n    }\n}\n", "func(int64) int64 does not satisfy Sizer: func(int64) int64 has no method size"},
		{"a conversion", funcProgram("", "g := double", "n := int64(g)", "if n > 0 {", "}"), "cannot convert func(int64) int64 to int64"},
		{"an operator", funcProgram("", "g := double", "h := double", "if g != h {", "}"), "operator != is not defined on func(int64) int64"},
		{"a type assertion target", funcProgram("v any", "g, ok := v.(func(int64) int64)", "if ok && g(1) > 0 {", "}"), "an any never holds func(int64) int64: only the builtin types and views of them can be passed " +
			"as any for now"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			diags := check(t, tt.src)
			if len(diags) == 0 || diags[0].Message != tt.want {
				t.Fatalf("got %v, want %q first", diags, tt.want)
			}
		})
	}
}
