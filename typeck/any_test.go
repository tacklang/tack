package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// anyCalling returns a program declaring one, taking an any, many, taking
// ...any, view, taking []any, both, taking a mut int64 and an any, and
// P, a struct, whose main's body is lines.
func anyCalling(lines ...string) string {
	return "package main\n\ntype P struct {\n    d int64\n}\n\n" +
		"func one(a any) {\n}\n\n" +
		"func many(xs ...any) {\n}\n\n" +
		"func view(v []any) {\n}\n\n" +
		"func both(mut x int64, a any) {\n    x++\n}\n\n" +
		"func main() {\n    " + strings.Join(lines, "\n    ") + "\n}\n"
}

// anyAcceptedCases are uses of any the checker accepts.
var anyAcceptedCases = []struct{ name, src string }{
	{"every basic type", anyCalling(
		"var a int8 = 1", "var b int16 = 1", "var c int32 = 1", "var d uint8 = 1",
		"var e uint16 = 1", "var f uint32 = 1", "var g uint64 = 1", "var h unsafe_size_t = 1",
		"var i unsafe_uintptr_t = 1",
		"many(a, b, c, d, e, f, g, h, i, 1, \"s\", true, 'c')",
	)},
	{"one of each", anyCalling("one(1)", "one(\"s\")", "one(false)", "one(-9223372036854775808)")},
	{"no values", anyCalling("many()")},
	{"call results and expressions", anyCalling("x := 2", "many(x + 1, x < 2, len(\"ab\"))")},
	{"copy alongside mut", anyCalling("mut x int64 = 1", "both(mut x, x)")},
	{
		"passed on",
		"package main\n\nfunc one(a any) {\n}\n\nfunc many(xs ...any) {\n}\n\n" +
			"func view(v []any) {\n}\n\n" +
			"func on(a any, xs ...any) {\n    one(a)\n    many(a, a)\n    many(xs...)\n" +
			"    view(xs)\n    if len(xs) > 0 {\n        one(xs[0])\n        many(xs[1:]...)\n    }\n" +
			"    _, ok := xs[0]\n    if ok {\n        one(a)\n    }\n" +
			"    for i := range xs {\n        one(i)\n    }\n" +
			"    defer {\n        one(a)\n    }\n}\n\nfunc main() {\n    on(1, 2)\n}\n",
	},
	{
		"immutable locals",
		"package main\n\nfunc one(a any) {\n}\n\nfunc on(a any, xs ...any) {\n    b := a\n    one(b)\n" +
			"    v, ok := xs[0]\n    if ok {\n        one(v)\n    }\n" +
			"    for _, x := range xs {\n        one(x)\n    }\n    var c = b\n" +
			"    defer {\n        one(c)\n    }\n}\n\nfunc main() {\n    on(1, 2)\n}\n",
	},
	{
		"extern funcs",
		"package main\n\nextern func f(a any, xs ...any) bool\n\nfunc main() {\n" +
			"    if f(1, \"s\", true) {\n        exit(1)\n    }\n}\n",
	},
	{
		"recursion",
		"package main\n\nfunc down(n int64, a any) {\n    if n > 0 {\n        down(n - 1, a)\n    }\n}\n\n" +
			"func main() {\n    down(3, \"x\")\n}\n",
	},
}

// TestAcceptsAny checks each of anyAcceptedCases has no diagnostics.
func TestAcceptsAny(t *testing.T) {
	for _, tt := range anyAcceptedCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// anyErrorCases are misuses of any, each with the one diagnostic it gives.
var anyErrorCases = []struct{ name, src, want string }{
	{"local", anyCalling("var a any = 1", "one(a)"), "any can only be the type of a parameter"},
	{
		"field",
		"package main\n\ntype H struct {\n    a any\n}\n\nfunc main() {\n}\n",
		"any can only be the type of a parameter",
	},
	{
		"result",
		"package main\n\nfunc f() any {\n    return 1\n}\n\nfunc main() {\n}\n",
		"any can only be the type of a parameter",
	},
	{
		"array element of a parameter",
		"package main\n\nfunc f(a [1]any) {\n}\n\nfunc main() {\n}\n",
		"any can only be the type of a parameter",
	},
	{
		"view of arrays of any",
		"package main\n\nfunc f(a [][1]any) {\n}\n\nfunc main() {\n}\n",
		"any can only be the type of a parameter",
	},
	{
		"mut",
		"package main\n\nfunc f(mut a any) {\n}\n\nfunc main() {\n}\n",
		"mut has no effect on an any parameter; take mut p T to change the caller's value",
	},
	{
		"mut variadic",
		"package main\n\nfunc f(mut a ...any) {\n}\n\nfunc main() {\n}\n",
		"a variadic parameter cannot be mut; it is a read-only view",
	},
	{
		"mut inferred local",
		"package main\n\nfunc f(a any) {\n    mut b := a\n    f(b)\n}\n\nfunc main() {\n}\n",
		"cannot declare mut b as any; an any variable cannot be reassigned",
	},
	{
		"mut checked element",
		"package main\n\nfunc f(a ...any) {\n    mut v, ok := a[0]\n    if ok {\n        f(v)\n    }\n}\n\nfunc main() {\n}\n",
		"cannot declare mut v as any; an any variable cannot be reassigned",
	},
	{
		"explicitly typed local",
		"package main\n\nfunc f(a any) {\n    var b any = a\n    f(b)\n}\n\nfunc main() {\n}\n",
		"any can only be the type of a parameter",
	},
	{
		"struct", anyCalling("one(P{d: 1})"),
		"cannot pass P as any: only the builtin types can be passed as any for now",
	},
	{
		"array", anyCalling("a := [1]int64{1}", "many(a)"),
		"cannot pass [1]int64 as any: only the builtin types can be passed as any for now",
	},
	{
		"constant overflows int64", anyCalling("one(18446744073709551615)"),
		"constant 18446744073709551615 overflows int64",
	},
	{
		"mut argument",
		anyCalling("mut x int64 = 1", "one(mut x)"),
		"one does not take x as mut; pass x",
	},
	{"conversion to any", anyCalling("one(any(1))"), "cannot convert int64 to any"},
	{
		"spread of an array", anyCalling("a := [1]int64{1}", "many(a...)"),
		"cannot use [1]int64 as []any in argument to many",
	},
	{
		"comparison",
		"package main\n\nfunc f(a any) bool {\n    return a == a\n}\n\nfunc main() {\n}\n",
		"operator == is not defined on any",
	},
	{
		"switch",
		"package main\n\nfunc f(a any) {\n    switch a {\n    }\n}\n\nfunc main() {\n}\n",
		"operator == is not defined on any",
	},
	{
		"conversion from any",
		"package main\n\nfunc f(a any) int64 {\n    return int64(a)\n}\n\nfunc main() {\n}\n",
		"cannot convert any to int64",
	},
	{
		"to a concrete parameter",
		"package main\n\nfunc g(s string) {\n}\n\nfunc f(a any) {\n    g(a)\n}\n\nfunc main() {\n}\n",
		"cannot use any as string in argument to g",
	},
	{
		"negation",
		"package main\n\nfunc f(a any) {\n    f(-a)\n}\n\nfunc main() {\n}\n",
		"operator - is not defined on any",
	},
	{
		"len",
		"package main\n\nfunc f(a any) {\n    f(len(a))\n}\n\nfunc main() {\n}\n",
		"len expects an array, a string, a view or a list, got any",
	},
}

// TestRejectsAnyMisuse checks each of anyErrorCases gives exactly its
// diagnostic.
func TestRejectsAnyMisuse(t *testing.T) {
	for _, tt := range anyErrorCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != 1 || diags[0].Message != tt.want {
				t.Errorf("got %v, want one diagnostic %q", diags, tt.want)
			}
		})
	}
}

// TestRecordsAnyTypes checks an any parameter's type is types.Any, a
// variadic ...any's and a []any's are one view of it, and that the values
// passed as any keep their own types, an untyped constant made int64.
func TestRecordsAnyTypes(t *testing.T) {
	file, diags := checkTree(t, anyCalling("many(1, 'a', \"s\")"))
	if len(diags) > 0 {
		t.Fatalf("got %v", diags)
	}
	table := file.Types
	funcs := map[string]*ast.Func{}
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.Func); ok {
			funcs[fn.Name] = fn
		}
	}
	if got := funcs["one"].Params[0].Type; got != types.Any {
		t.Errorf("one's a is %s, want any", table.Name(got))
	}
	xs, v := funcs["many"].Params[0].Type, funcs["view"].Params[0].Type
	if !table.IsView(xs) || table.Elem(xs) != types.Any || xs != v {
		t.Errorf("xs is %s and v %s, want one type, []any", table.Name(xs), table.Name(v))
	}
	call := funcs["main"].Body[0].(*ast.ExprStmt).X.(*ast.Call)
	for i, want := range []types.Type{types.Int64, types.Uint8, types.String} {
		if got := ast.TypeOf(call.Args[i]); got != want {
			t.Errorf("value %d is %s, want %s", i, table.Name(got), table.Name(want))
		}
	}
}
