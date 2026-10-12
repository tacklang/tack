package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// withGlobal returns a program declaring decl at package level and reading
// the variable x from main, so x is always used.
func withGlobal(decl string) string {
	return "package main\n\n" + decl + "\n\nfunc main() {\n    if x == x {\n    }\n}\n"
}

// globalX returns the first package-level declaration of a checked program.
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
		{int64(42), "var x = 42", types.Int},
		{int64(42), "var x int = 10 * 4 + 2", types.Int},
		{int64(21), "var x = (1 + 2) * (3 + 4)", types.Int},
		{int64(-7), "var x = 0 - 7", types.Int},
		{int64(3), "var x = 7 / 2", types.Int},
		{int64(-3), "var x = (0 - 7) / 2", types.Int},
		{int64(1), "var x = 7 % 3", types.Int},
		{int64(-1), "var x = (0 - 7) % 3", types.Int},
		{int64(9223372036854775807), "var x = 9223372036854775807", types.Int},
		{int64(-9223372036854775808), "var x = 0 - 9223372036854775807 - 1", types.Int},
		{int64(0), "var x = (0 - 9223372036854775807 - 1) % (0 - 1)", types.Int},
		{"tack", "var x = \"tack\"", types.String},
		{"", "var x string = \"\"", types.String},
		{true, "var x = 1 < 2", types.Bool},
		{false, "var x bool = 2 + 2 == 5", types.Bool},
		{true, "var x = 3 >= 3", types.Bool},
		{int64(0), "mut x int", types.Int},
		{"", "mut x string", types.String},
		{false, "mut x bool", types.Bool},
		{int64(5), "mut x = 5", types.Int},
	}

	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			file, diags := checkTree(t, withGlobal(tt.decl))
			// Comparing a string or bool with == is an error of its own; only
			// the declaration is under test.
			_ = diags
			v := globalX(t, file)
			if v.Type != tt.typ {
				t.Errorf("type = %v, want %v", v.Type, tt.typ)
			}
			if v.Const != tt.want {
				t.Errorf("value = %#v, want %#v", v.Const, tt.want)
			}
		})
	}
}

// invalidGlobalPrograms are package-level declarations the checker must
// reject, with the diagnostics each must produce.
var invalidGlobalPrograms = []struct {
	name string
	src  string
	want []string
}{
	{
		"immutable without a value", withGlobal("var x int"),
		[]string{"immutable variable x needs a value"},
	},
	{
		"another global",
		"package main\n\nvar a = 1\n\nvar x = a + 1\n\nfunc main() {\n    exit(x)\n}\n",
		[]string{"x must be initialized with a constant expression"},
	},
	{
		"a call",
		"package main\n\nfunc f() int {\n    return 1\n}\n\nvar x = f()\n\nfunc main() {\n    exit(x)\n}\n",
		[]string{"x must be initialized with a constant expression"},
	},
	{
		"a call deep inside",
		"package main\n\nfunc f() int {\n    return 1\n}\n\nvar x = 1 + 2 * f()\n\nfunc main() {\n    exit(x)\n}\n",
		[]string{"x must be initialized with a constant expression"},
	},
	{
		"a call to a function returning nothing",
		"package main\n\nfunc f() {\n}\n\nvar x = f()\n\nfunc main() {\n    exit(x)\n}\n",
		[]string{"x must be initialized with a constant expression"},
	},
	{
		"overflow on +", withGlobal("var x = 9223372036854775807 + 1"),
		[]string{"constant expression overflows int"},
	},
	{
		"overflow on -", withGlobal("var x = 0 - 9223372036854775807 - 2"),
		[]string{"constant expression overflows int"},
	},
	{
		"overflow on *", withGlobal("var x = 4611686018427387904 * 2"),
		[]string{"constant expression overflows int"},
	},
	{
		"overflow on /", withGlobal("var x = (0 - 9223372036854775807 - 1) / (0 - 1)"),
		[]string{"constant expression overflows int"},
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
		"declared type mismatch", withGlobal("var x int = \"s\""),
		[]string{"cannot use string as int in declaration of x"},
	},
	{
		"string arithmetic",
		"package main\n\nvar x = \"a\" + 1\n\nfunc main() {\n    print(x)\n}\n",
		[]string{"cannot use string as int in +"},
	},
	{
		"assign to an immutable global",
		"package main\n\nvar x = 1\n\nfunc main() {\n    x = 2\n    exit(x)\n}\n",
		[]string{"cannot assign to immutable variable x"},
	},
	{
		"assign the wrong type to a global",
		"package main\n\nmut x = 1\n\nfunc main() {\n    x = \"s\"\n    exit(x)\n}\n",
		[]string{"cannot use string as int in assignment to x"},
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
	src := "package main\n\nfunc main() {\n    exit(limit + 1)\n}\n\nvar limit = 41\n"
	if _, diags := checkTree(t, src); len(diags) > 0 {
		t.Errorf("got %v, want no diagnostics", diags)
	}
}

// A mutable global can be assigned from any function.
func TestMutableGlobalIsAssignable(t *testing.T) {
	src := "package main\n\nmut count int\n\nfunc bump() {\n    count = count + 1\n}\n\nfunc main() {\n    bump()\n    exit(count)\n}\n"
	if _, diags := checkTree(t, src); len(diags) > 0 {
		t.Errorf("got %v, want no diagnostics", diags)
	}
}
