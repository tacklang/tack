package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/types"
)

// withGlobal returns a program declaring decl at package level and reading
// the variable x from main, so x is always used.
func withGlobal(decl string) string {
	return "package main\n\n" + decl + "\n\nfunc main() {\n    if x == x {\n    }\n}\n"
}

// globalX returns the first package-level declaration of a checked program.
// machine returns a computed constant as Go's own value, so tests can
// compare it with an int64 literal: a constant.Int becomes its int64.
func machine(c any) any {
	if v, ok := c.(constant.Int); ok {
		return v.Int64()
	}
	return c
}

func globalX(t *testing.T, file *ast.File) *ast.Var {
	t.Helper()

	v, ok := file.Decls[0].(*ast.Var)
	if !ok {
		t.Fatalf("first declaration is %T, want *ast.Var", file.Decls[0])
	}
	return v
}

// TestFoldsGlobalInitializers checks each package-level variable's type and
// the value the checker computes for it at compile time.
func TestFoldsGlobalInitializers(t *testing.T) {
	tests := []struct {
		want any
		decl string
		typ  types.Type
	}{
		{int64(42), "var x = 42", types.Int64},
		{int64(42), "var x = a * 2\n\nvar a = 21", types.Int64},
		{int64(21), "var x = a + b\n\nvar a = b * 2\n\nvar b = 7", types.Int64},
		{int64(-5), "var x int8 = 0 - a\n\nvar a int8 = 5", types.Int8},
		{"tack", "var x = s\n\nvar s = \"tack\"", types.String},
		{true, "var x = b && 1 < 2\n\nvar b = 2 > 1", types.Bool},
		{int64(42), "var x int64 = 10 * 4 + 2", types.Int64},
		{int64(21), "var x = (1 + 2) * (3 + 4)", types.Int64},
		{int64(-7), "var x = 0 - 7", types.Int64},
		{int64(3), "var x = 7 / 2", types.Int64},
		{int64(-3), "var x = (0 - 7) / 2", types.Int64},
		{int64(1), "var x = 7 % 3", types.Int64},
		{int64(-1), "var x = (0 - 7) % 3", types.Int64},
		{int64(9223372036854775807), "var x = 9223372036854775807", types.Int64},
		{int64(-9223372036854775808), "var x = 0 - 9223372036854775807 - 1", types.Int64},
		{int64(0), "var x = (0 - 9223372036854775807 - 1) % (0 - 1)", types.Int64},
		{"tack", "var x = \"tack\"", types.String},
		{"", "var x string = \"\"", types.String},
		{true, "var x = 1 < 2", types.Bool},
		{false, "var x bool = 2 + 2 == 5", types.Bool},
		{true, "var x = 3 >= 3", types.Bool},
		{int64(0), "mut x int64", types.Int64},
		{"", "mut x string", types.String},
		{false, "mut x bool", types.Bool},
		{int64(5), "mut x = 5", types.Int64},
	}

	for _, tt := range append(tests, computedGlobals...) {
		t.Run(tt.decl, func(t *testing.T) {
			file, diags := checkTree(t, withGlobal(tt.decl))
			// Only the declaration is under test, not what the program
			// does with it.
			_ = diags
			v := globalX(t, file)
			if v.Type != tt.typ {
				t.Errorf("type = %v, want %v", v.Type, tt.typ)
			}
			if machine(v.Const) != tt.want {
				t.Errorf("value = %#v, want %#v", v.Const, tt.want)
			}
		})
	}
}

// computedGlobals are package-level declarations whose initializers use
// anything the compiler can compute: fields and elements of immutable
// globals, len, conversions, comparisons and logical operators on known
// values, and combinations of them, with the value each computes to.
var computedGlobals = []struct {
	want any
	decl string
	typ  types.Type
}{
	{int64(3), "var x = o.a\n\n" + oDecls, types.Int64},
	{int64(4), "var x = o.b\n\n" + oDecls, types.Int64},
	{int64(20), "var x = t[1]\n\nvar t = [3]int64{10, 20, 30}", types.Int64},
	{int64(30), "var x = t[k]\n\nvar k = 2\n\nvar t = [3]int64{10, 20, 30}", types.Int64},
	{int64(30), "var x = t[t[0] / 10 + 1]\n\nvar t = [3]int64{10, 20, 30}", types.Int64},
	{int64(3), "var x = len(t)\n\nvar t = [3]int64{10, 20, 30}", types.Int64},
	{int64(5), "var x = len(s)\n\nvar s = \"hello\"", types.Int64},
	{int64(4), "var x = len(m)\n\nmut m [4]int64", types.Int64},
	{int64(2), "var x = len(m[0])\n\nmut m [3][2]int64", types.Int64},
	{int64('e'), "var x = s[1]\n\nvar s = \"hello\"", types.Uint8},
	{int64(7), "var x = int64(int8(7))", types.Int64},
	{int64(-1), "var x = int64(n)\n\nvar n int8 = -1", types.Int64},
	{int64(200), "var x = uint8(n * 2)\n\nvar n int64 = 100", types.Uint8},
	{
		int64(4), "var x = g[1].b\n\nvar g = [2]P{P{a: 1, b: 2}, P{a: 3, b: 4}}\n\n" + pType,
		types.Int64,
	},
	{int64(2), "var x = n.p.b\n\nvar n = N{p: P{a: 1, b: 2}}\n\n" +
		"type N struct {\n    p P\n}\n\n" + pType, types.Int64},
	{"b", "var x = names[1]\n\nvar names = [2]string{\"a\", \"b\"}", types.String},
	{int64(2), "var x = len(names[1]) + 1\n\nvar names = [2]string{\"a\", \"b\"}", types.Int64},
	{true, "var x = o.a < o.b && t[0] != 0\n\nvar t = [1]int64{1}\n\n" + oDecls, types.Bool},
	{
		false,
		"var x = o.a > o.b || !(len(t) == 1)\n\nvar t = [1]int64{1}\n\n" + oDecls,
		types.Bool,
	},
	{true, "var x = o == P{a: 3, b: 4}\n\n" + oDecls, types.Bool},
	{false, "var x = o != o\n\n" + oDecls, types.Bool},
	{
		false,
		"var x = t == [2]string{\"a\", \"c\"}\n\nvar t = [2]string{\"a\", \"b\"}",
		types.Bool,
	},
	{true, "var x = b == true\n\nvar b = 1 < 2", types.Bool},
	{int64(1), "var x = P{a: 1, b: 2}.a\n\n" + pType, types.Int64},
	{int64(2), "var x = [2]int64{1, 2}[1]", types.Int64},
	{
		int64(8),
		"var x = c.a * c.b\n\nvar c = o\n\nvar o = P{a: 2, b: 4}\n\n" + pType,
		types.Int64,
	},
}

// pType declares the struct P, with two int64 fields a and b, and
// oDecls also declares the immutable global o, a P holding 3 and 4.
const (
	pType  = "type P struct {\n    a int64\n    b int64\n}"
	oDecls = "var o = P{a: 3, b: 4}\n\n" + pType
)

// invalidGlobalPrograms are package-level declarations the checker must
// reject, with the diagnostics each must produce.
var invalidGlobalPrograms = []struct {
	name string
	src  string
	want []string
}{
	{
		"immutable without a value", withGlobal("var x int64"),
		[]string{"immutable variable x needs a value"},
	},
	{
		"a mutable global",
		"package main\n\nmut a = 1\n\nvar x = a + 1\n\nfunc main() {\n    exit(x)\n}\n",
		[]string{"x must be initialized with a constant expression"},
	},
	{
		"a call",
		"package main\n\nfunc f() int64 {\n    return 1\n}\n\nvar x = f()\n\nfunc main() {\n    exit(x)\n}\n",
		[]string{"x must be initialized with a constant expression"},
	},
	{
		"a call deep inside",
		"package main\n\nfunc f() int64 {\n    return 1\n}\n\nvar x = 1 + 2 * f()\n\nfunc main() {\n    exit(x)\n}\n",
		[]string{"x must be initialized with a constant expression"},
	},
	{
		"a call to a function returning nothing",
		"package main\n\nfunc f() {\n}\n\nvar x = f()\n\nfunc main() {\n    exit(x)\n}\n",
		[]string{"x must be initialized with a constant expression"},
	},
	{
		"a field of a mutable global",
		withGlobal("var x = o.a\n\nmut o = P{a: 3, b: 4}\n\n" + pType),
		[]string{"x must be initialized with a constant expression"},
	},
	{
		"an element of a mutable global",
		withGlobal("var x = t[0]\n\nmut t = [1]int64{1}"),
		[]string{"x must be initialized with a constant expression"},
	},
	{
		"a mutable index",
		withGlobal("var x = t[i]\n\nmut i int64 = 0\n\nvar t = [1]int64{1}"),
		[]string{"x must be initialized with a constant expression"},
	},
	{
		"len of a call",
		"package main\n\nfunc f() string {\n    return \"a\"\n}\n\nvar x = len(f())\n\n" +
			"func main() {\n    exit(x)\n}\n",
		[]string{"x must be initialized with a constant expression"},
	},
	{
		"a conversion of a call",
		"package main\n\nfunc f() int64 {\n    return 1\n}\n\nvar x = int8(f())\n\n" +
			"func main() {\n    exit(x)\n}\n",
		[]string{"x must be initialized with a constant expression"},
	},
	{
		"a builtin that gives nothing",
		"package main\n\nvar x = exit(1)\n\nfunc main() {\n    exit(x)\n}\n",
		[]string{"x must be initialized with a constant expression"},
	},
	{
		"len of a mutable string", withGlobal("var x = len(s)\n\nmut s = \"abc\""),
		[]string{"x must be initialized with a constant expression"},
	},
	{
		"a field of a global already reported",
		withGlobal("var o = P{a: 1 / 0, b: 2}\n\nvar x = o.a\n\n" + pType),
		[]string{"division by zero in constant expression"},
	},
	{
		"an element of a global already reported",
		withGlobal("var t = [1]string{1}\n\nvar x = len(t[0]) +% 1"),
		[]string{"cannot use int64 as string in array literal"},
	},
	{
		"an index out of range", withGlobal("var x = t[3]\n\nvar t = [3]int64{1, 2, 3}"),
		[]string{"index 3 is out of range for [3]int64"},
	},
	{
		"a negative index", withGlobal("var x = t[0 - 1]\n\nvar t = [3]int64{1, 2, 3}"),
		[]string{"index -1 is out of range for [3]int64"},
	},
	{
		"a string index out of range", withGlobal("var x = s[2]\n\nvar s = \"ab\""),
		[]string{"index 2 is out of range for a string of 2 bytes"},
	},
	{
		"a conversion that overflows", withGlobal("var x = int8(n)\n\nvar n int64 = 300"),
		[]string{"constant 300 overflows int8"},
	},
	{
		"arithmetic on a field that overflows",
		withGlobal("var x = o.a * 2\n\nvar o = Q{a: 100}\n\ntype Q struct {\n    a int8\n}"),
		[]string{"constant expression overflows int8"},
	},
	{
		"division by a zero element", withGlobal("var x = 10 / t[1]\n\nvar t = [2]int64{1, 0}"),
		[]string{"division by zero"},
	},
	{
		"overflow on +", withGlobal("var x = 9223372036854775807 + 1"),
		[]string{"constant 9223372036854775808 overflows int64"},
	},
	{
		"overflow on -", withGlobal("var x = 0 - 9223372036854775807 - 2"),
		[]string{"constant expression is out of range"},
	},
	{
		"overflow on *", withGlobal("var x = 4611686018427387904 * 2"),
		[]string{"constant 9223372036854775808 overflows int64"},
	},
	{
		"overflow on /", withGlobal("var x = (0 - 9223372036854775807 - 1) / (0 - 1)"),
		[]string{"constant 9223372036854775808 overflows int64"},
	},
	{
		"division by zero", withGlobal("var x = 1 / 0"),
		[]string{"division by zero in constant expression"},
	},
	{
		"remainder by zero", withGlobal("var x = 1 % (2 - 2)"),
		[]string{"division by zero in constant expression"},
	},
	{
		"declared type mismatch", withGlobal("var x int64 = \"s\""),
		[]string{"cannot use string as int64 in declaration of x"},
	},
	{
		"string arithmetic",
		"package main\n\nvar x = \"a\" + 1\n\nfunc main() {\n    print(x)\n}\n",
		[]string{"operator + is not defined on string"},
	},
	{
		"assign to an immutable global",
		"package main\n\nvar x = 1\n\nfunc main() {\n    x = 2\n    exit(int32(x))\n}\n",
		[]string{"cannot assign to immutable variable x"},
	},
	{
		"assign the wrong type to a global",
		"package main\n\nmut x = 1\n\nfunc main() {\n    x = \"s\"\n    code, fits := int32(x)\n    if fits { exit(code) }\n}\n",
		[]string{"cannot use string as int64 in assignment to x"},
	},
}

func TestRejectsInvalidGlobals(t *testing.T) {
	for _, tt := range invalidGlobalPrograms {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
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

// A global's type is known in every function, whatever the order of
// declarations, since globals are checked before any body.
func TestGlobalTypeIsKnownInEarlierFunctions(t *testing.T) {
	src := "package main\n\nfunc main() {\n    exit(int32(limit + 1))\n}\n\nvar limit = 41\n"
	if _, diags := checkTree(t, src); len(diags) > 0 {
		t.Errorf("got %v, want no diagnostics", diags)
	}
}

// A mutable global can be assigned from any function.
func TestMutableGlobalIsAssignable(t *testing.T) {
	src := "package main\n\nmut count int64\n\nfunc bump() {\n    count = count +% 1\n}\n\nfunc main() {\n    bump()\n    code, fits := int32(count)\n    if fits { exit(code) }\n}\n"
	if _, diags := checkTree(t, src); len(diags) > 0 {
		t.Errorf("got %v, want no diagnostics", diags)
	}
}

// machineString renders a computed constant for comparison in a test.
func machineString(c any) string {
	if v, ok := c.(constant.Int); ok {
		return v.String()
	}
	return ""
}
