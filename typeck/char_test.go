package typeck_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// charTypeCases are declarations whose value involves a character literal,
// with the type the declared variable takes.
var charTypeCases = []struct {
	decl string
	want types.Type
}{
	{"x := 'a'", types.Uint8},
	{"x := '\\n'", types.Uint8},
	{"x := 'a' + 1", types.Uint8},
	{"x := 1 + 'a'", types.Uint8},
	{"x := 'a' + 'b'", types.Uint8},
	{"x := 'a' * 2", types.Uint8},
	{"x := ('a')", types.Uint8},
	{"x := 'a' == 97", types.Bool},
	{"x := 'a' < 'b'", types.Bool},
	{"var x int64 = 'a'", types.Int64},
	{"var x int16 = 'a'", types.Int16},
	{"var x uint64 = 'a' + 1", types.Uint64},
	{"x := int64('a')", types.Int64},
	{"x := uint8('a')", types.Uint8},
}

// TestCharLiteralsAreUint8 checks a character literal is a typed uint8
// constant: an untyped constant beside it settles to uint8, and it widens
// where a wider type is wanted.
func TestCharLiteralsAreUint8(t *testing.T) {
	for _, tt := range charTypeCases {
		t.Run(tt.decl, func(t *testing.T) {
			file, diags := checkTree(t, mainLines(tt.decl, "if x == x {", "}"))
			if len(diags) > 0 {
				t.Fatalf("got %v, want no diagnostics", diags)
			}
			v := file.Decls[0].(*ast.Func).Body[0].(*ast.Var)
			if v.Type != tt.want {
				t.Errorf("type = %v, want %v", v.Type, tt.want)
			}
		})
	}
}

// validCharPrograms use character literals as known values: as a divisor,
// an array length, an index and a range count, and wherever a uint8 or a
// wider integer goes.
var validCharPrograms = []struct{ name, src string }{
	{"exit widens", mainLines("exit('a')")},
	{"known divisor", mainLines("x := 10 / ('b' - 'a')", "exit(x)")},
	{"known divisor variable", mainLines("d := 'b' - 'a'", "x := 10 / d", "exit(x)")},
	{"array length", mainLines("mut a ['c' - 'a']int64", "a[1] = 5", "exit(a[1])")},
	{"constant index", mainLines("mut seen [256]bool", "seen['z'] = true", "if seen['z'] {", "}")},
	{"constant index at the end", mainLines("mut a [98]int64", "exit(a['a'])")},
	{
		"range over a character count",
		mainLines(
			"mut a [2]int64",
			"for i := range 'c' - 'a' {",
			"    a[i] = 1",
			"}",
			"exit(a[0])",
		),
	},
	{
		"uint8 variable index",
		mainLines("mut seen [256]bool", "c := 'q'", "seen[c] = true", "if seen[c] {", "}"),
	},
	{"a mutable variable wraps", mainLines("mut x = 'a'", "x *= 3", "exit(x)")},
	{"compared with a uint8", mainLines("var b uint8 = 97", "if b == 'a' {", "}")},
	{"compared with an int64", mainLines("n := 97", "if n == 'a' {", "}")},
	{
		"checked conversion",
		mainLines("mut c := 'a'", "v, ok := int8(c)", "if ok {", "    exit(v)", "}"),
	},
	{"known conversion", mainLines("v := int8('a')", "exit(v)")},
	{
		"parameter and result",
		"package main\n\nfunc up(c uint8) uint8 {\n    return c - 'a' + 'A'\n}\n\nfunc main() {\n    exit(up('b'))\n}\n",
	},
}

// TestAcceptsCharPrograms checks each of validCharPrograms has no
// diagnostics.
func TestAcceptsCharPrograms(t *testing.T) {
	for _, tt := range validCharPrograms {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// invalidCharPrograms misuse character literals, with the one diagnostic
// each must produce.
var invalidCharPrograms = []struct {
	name string
	src  string
	want spanned
}{
	{
		"narrowing",
		mainLines("var c int8 = 'a'", "exit(c)"),
		spanned{"cannot use uint8 as int8 in declaration of c", "'a'"},
	},
	{
		"constant beside it overflows",
		mainLines("c := 'a' + 300", "exit(c)"),
		spanned{"constant 300 overflows uint8", "300"},
	},
	{
		"negated",
		mainLines("c := -'a'", "exit(c)"),
		spanned{"operator - is not defined on uint8", "'a'"},
	},
	{
		"mixed with a signed type",
		mainLines("var s int8 = 1", "c := 'a' + s", "exit(c)"),
		spanned{"mismatched types uint8 and int8 in +", "'a' + s"},
	},
	{
		"printed",
		mainLines("print('a')"),
		spanned{"cannot use uint8 as string in argument to print", "'a'"},
	},
	{
		"as a condition",
		mainLines("if 'a' {", "}"),
		spanned{"condition must be a bool, got uint8", "'a'"},
	},
	{
		"known zero divisor",
		mainLines("x := 10 / ('a' - 'a')", "exit(x)"),
		spanned{"division by zero", "'a' - 'a'"},
	},
	{
		"indexed",
		mainLines("x := 'a'[0]", "exit(x)"),
		spanned{"cannot index uint8", "'a'"},
	},
	{
		"constant index out of range",
		mainLines("mut a [97]int64", "exit(a['a'])"),
		spanned{"index 97 is out of range for [97]int64", "'a'"},
	},
	{
		"assigned",
		mainLines("mut s string", "s = 'a'", "print(s)"),
		spanned{"cannot use uint8 as string in assignment to s", "'a'"},
	},
	{
		"conversion that cannot fail with ok",
		mainLines("v, ok := int64('a')", "if ok {", "    exit(v)", "}"),
		spanned{"converting uint8 to int64 cannot fail; use int64(...)", "int64('a')"},
	},
	{
		"global product overflows",
		withGlobal("var x = 'a' * 3"),
		spanned{"constant expression overflows uint8", "'a' * 3"},
	},
	{
		"global difference below zero",
		withGlobal("var x = 'a' - 'b'"),
		spanned{"constant expression overflows uint8", "'a' - 'b'"},
	},
	{
		"global overflow in a step",
		withGlobal("var x = 'a' * 3 / 3"),
		spanned{"constant expression overflows uint8", "'a' * 3"},
	},
	{
		"global overflow in an element",
		withGlobal("var x = [2]uint8{'a', 'a' + 'a' + 'a'}"),
		spanned{"constant expression overflows uint8", "'a' + 'a' + 'a'"},
	},
	{
		"global overflow in a comparison",
		withGlobal("var x = 'a' * 3 > 0"),
		spanned{"constant expression overflows uint8", "'a' * 3"},
	},
	{
		"global division by zero",
		withGlobal("var x = 'a' / ('a' - 'a')"),
		spanned{"division by zero", "'a' - 'a'"},
	},
}

// TestRejectsCharMisuse checks each of invalidCharPrograms is one error,
// covering the code it is about.
func TestRejectsCharMisuse(t *testing.T) {
	for _, tt := range invalidCharPrograms {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != 1 {
				t.Fatalf("got %v, want only %q", diags, tt.want.msg)
			}
			if diags[0].Message != tt.want.msg {
				t.Errorf("message = %q, want %q", diags[0].Message, tt.want.msg)
			}
			if got := tt.src[diags[0].Span.Start:diags[0].Span.End]; got != tt.want.at {
				t.Errorf("diagnostic covers %q, want %q", got, tt.want.at)
			}
		})
	}
}

// charGlobals are package-level variables initialized with character
// literals, with the type and value each must have.
var charGlobals = []struct {
	want any
	decl string
	typ  types.Type
}{
	{int64(97), "var x = 'a'", types.Uint8},
	{int64(255), "var x = '\xff'", types.Uint8},
	{int64(0), "var x = '\\\\' - '\\\\'", types.Uint8},
	{int64(122), "var x = 'a' + 25", types.Uint8},
	{int64(255), "var x = 'a' + 158", types.Uint8},
	{int64(97), "var x int64 = 'a'", types.Int64},
	{int64(32), "var x = 'a' - 'A'", types.Uint8},
	{int64(48), "var x = 'a' / 2", types.Uint8},
	{true, "var x = 'a' < 'b'", types.Bool},
	{true, "var x = 'a' == 97", types.Bool},
	{false, "var x = 'a' != 97 || 'b' < 'a'", types.Bool},
	{int64(10), "mut x = '\\n'", types.Uint8},
}

// TestFoldsCharGlobals checks a package-level initializer made of character
// literals is computed at compile time, typed arithmetic and comparisons
// included.
func TestFoldsCharGlobals(t *testing.T) {
	for _, tt := range charGlobals {
		t.Run(tt.decl, func(t *testing.T) {
			file, diags := checkTree(t, withGlobal(tt.decl))
			if len(diags) > 0 {
				t.Fatalf("got %v, want no diagnostics", diags)
			}
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

// TestFoldsCharArrayGlobals checks an array literal of character literals
// at package level is computed element by element.
func TestFoldsCharArrayGlobals(t *testing.T) {
	file, diags := checkTree(t, withGlobal("var x = [3]uint8{'a', 'b' + 1, '\\t'}"))
	if len(diags) > 0 {
		t.Fatalf("got %v, want no diagnostics", diags)
	}
	elems, ok := globalX(t, file).Const.([]any)
	if !ok || len(elems) != 3 {
		t.Fatalf("value = %#v, want three elements", globalX(t, file).Const)
	}
	for i, want := range []int64{97, 99, 9} {
		if got := machine(elems[i]); got != want {
			t.Errorf("element %d = %#v, want %d", i, got, want)
		}
	}
}
