package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// calling returns a program declaring f, which takes a string and then a
// variadic of int64, b, a variadic of uint8, ps, a variadic of P, a
// struct, v, a plain view parameter, and m, a mut parameter followed by
// a variadic, and whose main's body is lines, each on a line of its own.
func calling(lines ...string) string {
	return "package main\n\ntype P struct {\n    d int64\n}\n\n" +
		"func f(s string, n ...int64) int64 {\n    return len(s)\n}\n\n" +
		"func b(xs ...uint8) {\n}\n\n" +
		"func ps(xs ...P) {\n}\n\n" +
		"func v(xs []int64) {\n}\n\n" +
		"func m(mut x int64, xs ...int64) {\n    x++\n}\n\n" +
		"func mk() [2]int64 {\n    return [2]int64{1, 2}\n}\n\n" +
		"func main() {\n    " + strings.Join(lines, "\n    ") + "\n}\n"
}

// variadicCallCases are calls a variadic parameter accepts.
var variadicCallCases = []struct{ name, src string }{
	{"no values", calling("f(\"\")")},
	{"one value", calling("f(\"\", 1)")},
	{"several values", calling("f(\"\", 1, 2, 3)")},
	{"widened values", calling("var x int8 = 1", "var y uint32 = 2", "f(\"\", x, y, 'a')")},
	{"constants at the edges", calling("f(\"\", -9223372036854775808, 9223372036854775807)")},
	{"constant byte", calling("b(255, 0)")},
	{"struct values", calling("p := P{d: 1}", "ps(p, P{d: 2})")},
	{"spread of an array", calling("a := [2]int64{1, 2}", "f(\"\", a...)")},
	{"spread of an empty array", calling("a := [0]int64{}", "f(\"\", a...)")},
	{"spread of a slice", calling("a := [3]int64{1, 2, 3}", "f(\"\", a[1:]...)")},
	{"spread of a string's bytes", calling("b([]uint8(\"ab\")...)")},
	{"spread of a field", calling("a := [1]P{{d: 1}}", "ps(a...)")},
	{"value alongside mut", calling("mut x int64 = 1", "m(mut x, x, x)")},
	{"spread alongside mut", calling("mut x int64 = 1", "a := [1]int64{1}", "m(mut x, a...)")},
	{"result used", calling("x := f(\"\", 1)", "if x == 0 {", "    exit(1)", "}")},
	{
		"variadic passed on",
		"package main\n\nfunc g(xs []int64) {\n}\n\nfunc f(n ...int64) {\n    g(n)\n    f(n...)\n" +
			"    for i, x := range n {\n        f(i, x)\n    }\n    if len(n) > 1 {\n" +
			"        f(n[0], n[1])\n        f(n[1:]...)\n    }\n    y, ok := n[5]\n    if ok {\n        f(y)\n    }\n}\n\n" +
			"func main() {\n    f()\n}\n",
	},
	{
		"extern func",
		"package main\n\nextern func g(s string, n ...int64) int64\n\nfunc main() {\n" +
			"    a := [1]int64{1}\n    x := g(\"\", 1, 2) + g(\"\") + g(\"\", a...)\n" +
			"    if x == 0 {\n        exit(1)\n    }\n}\n",
	},
}

// TestAcceptsVariadicCalls checks each of variadicCallCases has no
// diagnostics.
func TestAcceptsVariadicCalls(t *testing.T) {
	for _, tt := range variadicCallCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// variadicErrorCases are declarations and calls of variadics that are
// errors, each with the one diagnostic it gives.
var variadicErrorCases = []struct{ name, src, want string }{
	{"too few", calling("f()"), "f expects at least 1 argument(s), got 0"},
	{
		"spread too few",
		calling("a := [2]int64{1, 2}", "f(a...)"),
		"f expects 1 argument(s) before a..., got 0",
	},
	{
		"spread with values", calling("a := [2]int64{1, 2}", "f(\"\", 1, a...)"),
		"cannot use ... in a call with other values for n",
	},
	{
		"spread to a view",
		calling("a := [2]int64{1, 2}", "v(a...)"),
		"cannot use ... in a call to non-variadic v",
	},
	{
		"spread to a builtin",
		calling("print(\"a\"...)"),
		"cannot use ... in a call to non-variadic print",
	},
	{
		"spread to len",
		calling("a := [2]int64{1, 2}", "exit(int32(len(a...)))"),
		"cannot use ... in a call to non-variadic len",
	},
	{
		"spread to a conversion",
		calling("x := 1", "exit(int32(x...))"),
		"cannot use ... in a conversion to int32",
	},
	{
		"spread to a checked conversion",
		calling("x := 1", "y, ok := int8(x...)", "if ok {", "    exit(y)", "}"),
		"cannot use ... in a conversion to int8",
	},
	{
		"spread to a view conversion",
		calling("b([]uint8(\"a\"...)...)"),
		"cannot use ... in a conversion to []uint8",
	},
	{
		"spread of a scalar",
		calling("x := 1", "f(\"\", x...)"),
		"cannot use int64 as []int64 in argument to f",
	},
	{
		"spread of another element type", calling("a := [2]int32{1, 2}", "f(\"\", a...)"),
		"cannot use [2]int32 as []int64 in argument to f",
	},
	{
		"spread of a temporary",
		calling("f(\"\", mk()...)"),
		"cannot view a temporary array; store it in a variable first",
	},
	{
		"spread of a string", calling("s := \"ab\"", "b(s...)"),
		"cannot use string as []uint8 in argument to b; view its bytes with []uint8(s)",
	},
	{
		"spread as mut",
		calling("mut a := [2]int64{1, 2}", "f(\"\", mut a...)"),
		"f does not take a as mut; pass a",
	},
	{
		"value of a wrong type",
		calling("f(\"\", 1, \"x\")"),
		"cannot use string as int64 in argument to f",
	},
	{
		"value narrowed",
		calling("var x int64 = 1", "b(x)"),
		"cannot use int64 as uint8 in argument to b",
	},
	{"constant overflows", calling("b(1, 256)"), "constant 256 overflows uint8"},
	{
		"value as mut",
		calling("mut x int64 = 1", "f(\"\", mut x)"),
		"f does not take x as mut; pass x",
	},
	{
		"array as a value",
		calling("a := [2]int64{1, 2}", "f(\"\", a)"),
		"cannot use [2]int64 as int64 in argument to f",
	},
	{
		"slice as a value", calling("a := [2]int64{1, 2}", "f(\"\", a[1:])"),
		"a[1:] is a view, []int64, which can only be passed to a view parameter or to len",
	},
	{"untyped constant as a struct", calling("ps(1)"), "cannot use int64 as P in argument to ps"},
	{
		"mut variadic",
		"package main\n\nfunc f(mut n ...int64) {\n}\n\nfunc main() {\n    f(1)\n}\n",
		"a variadic parameter cannot be mut; it is a read-only view",
	},
	{
		"variadic of views", "package main\n\nfunc f(n ...[]int64) {\n}\n\nfunc main() {\n}\n",
		"the element type of a variadic parameter cannot be a view",
	},
	{
		"variadic of an unknown type",
		"package main\n\nfunc f(n ...nothing) {\n}\n\nfunc main() {\n    f(1)\n}\n",
		"unknown type: nothing",
	},
	{
		"assigned", "package main\n\nfunc f(n ...int64) {\n    n = n\n}\n\nfunc main() {\n}\n",
		"cannot assign to n: n is a read-only view",
	},
	{
		"stored",
		"package main\n\nfunc f(n ...int64) {\n    x := n\n    f(x...)\n}\n\nfunc main() {\n}\n",
		"cannot declare x as []int64; a view can only be the type of a parameter or of a result",
	},
	{
		"index unproven",
		"package main\n\nfunc f(n ...int64) int64 {\n    return n[0]\n}\n\nfunc main() {\n}\n",
		"index 0 may be out of range; check len(n) > 0 first, or use v, ok := ...[0]",
	},
}

// TestRejectsVariadicMisuse checks each of variadicErrorCases gives
// exactly its diagnostic.
func TestRejectsVariadicMisuse(t *testing.T) {
	for _, tt := range variadicErrorCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != 1 || diags[0].Message != tt.want {
				t.Errorf("got %v, want one diagnostic %q", diags, tt.want)
			}
		})
	}
}

// TestRecordsVariadicTypes checks a variadic parameter's type is the view
// of its element type, the same type a view parameter of it has, and that
// an untyped constant passed to it takes the element type.
func TestRecordsVariadicTypes(t *testing.T) {
	src := "package main\n\nfunc f(n ...uint8) {\n}\n\nfunc g(v []uint8) {\n}\n\n" +
		"func main() {\n    f(1, 2)\n}\n"
	file, diags := checkTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %v", diags)
	}
	table := file.Types
	n := file.Decls[0].(*ast.Func).Params[0]
	if !table.IsView(n.Type) || table.Elem(n.Type) != types.Uint8 {
		t.Errorf("n is %s, want []uint8", table.Name(n.Type))
	}
	if v := file.Decls[1].(*ast.Func).Params[0]; v.Type != n.Type {
		t.Errorf("v is %s and n %s, want one type", table.Name(v.Type), table.Name(n.Type))
	}
	call := file.Decls[2].(*ast.Func).Body[0].(*ast.ExprStmt).X.(*ast.Call)
	for _, arg := range call.Args {
		if got := ast.TypeOf(arg); got != types.Uint8 {
			t.Errorf("a value is %s, want uint8", table.Name(got))
		}
	}
}
