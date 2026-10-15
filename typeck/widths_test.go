package typeck_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// sized declares a variable of every integer type, each read once so none
// is unused, for the programs below to combine. Each is mutable, so its
// value is not known and a conversion of it is checked by its type alone.
var sized = []string{
	"mut i8 int8 = 1",
	"mut i16 int16 = 1",
	"mut i32 int32 = 1",
	"mut i64 int64 = 1",
	"mut u8 uint8 = 1",
	"mut u16 uint16 = 1",
	"mut u32 uint32 = 1",
	"mut u64 uint64 = 1",
	"if i8 == 0 || i16 == 0 || i32 == 0 || i64 == 0 || u8 == 0 || u16 == 0 || u32 == 0 || u64 == 0 {",
	"}",
}

// withSized is a main holding sized and then lines.
func withSized(lines ...string) string {
	return mainLines(append(append([]string{}, sized...), lines...)...)
}

// integerWidthCases are uses of the sized types that must check cleanly:
// each constant at its type's bounds, and every lossless widening.
var integerWidthCases = []struct{ name, src string }{
	{"int8 bounds", mainLines("var a int8 = -128", "var b int8 = 127", "if a < b {", "}")},
	{"uint8 bounds", mainLines("var a uint8 = 0", "var b uint8 = 255", "if a < b {", "}")},
	{
		"int16 bounds",
		mainLines("var a int16 = -32768", "var b int16 = 32767", "if a < b {", "}"),
	},
	{"uint32 bound", mainLines("var a uint32 = 4294967295", "if a > 0 {", "}")},
	{
		"int64 bounds",
		mainLines(
			"var a int64 = -9223372036854775808",
			"var b int64 = 9223372036854775807",
			"if a < b {",
			"}",
		),
	},
	{"uint64 bound", mainLines("var a uint64 = 18446744073709551615", "if a > 0 {", "}")},
	{
		"constant arithmetic past int64",
		mainLines("var a uint64 = 9223372036854775807 * 2 + 1", "if a > 0 {", "}"),
	},
	{"negative intermediate", mainLines("var a uint8 = 10 - 20 + 265", "if a > 0 {", "}")},
	{
		"signed widens",
		withSized("var a int64 = i8", "var b int32 = i16", "if a + b > 0 {", "}"),
	},
	{
		"unsigned widens",
		withSized("var a uint64 = u8", "var b uint32 = u16", "if a + b > 0 {", "}"),
	},
	{
		"unsigned widens to wider signed",
		withSized("var a int16 = u8", "var b int64 = u32", "if a + b > 0 {", "}"),
	},
	{
		"mixed arithmetic widens",
		withSized("var a int64 = i8 + i64", "var b uint16 = u8 * u16", "if a + b > 0 {", "}"),
	},
	{"mixed comparison widens", withSized("if i8 < i32 && u8 == u64 && u16 < i32 {", "}")},
	{
		"constant takes the other operand's type",
		withSized("var a uint8 = u8 + 200", "if a > 0 {", "}"),
	},
	{
		"compound with a narrower value",
		withSized("mut a int64 = 0", "a += i8", "a -= u32", "exit(a)"),
	},
	{
		"increment any width",
		withSized(
			"mut a uint8 = 255",
			"a++",
			"mut b int8 = -128",
			"b--",
			"if a == 0 && b == 127 {",
			"}",
		),
	},
	{"exit with a narrower value", withSized("exit(i8)", "exit(u32)")},
	{"range over a sized count", withSized("for i := range u8 {", "    exit(i)", "}")},
	{"negate a signed value", withSized("var a int8 = -i8", "if a < 0 {", "}")},
	{
		"conversion that widens",
		withSized("a := int64(u8)", "b := uint16(u8)", "if a + b > 0 {", "}"),
	},
	{
		"constant conversion",
		mainLines(
			"a := int8(-128)",
			"b := uint64(18446744073709551615)",
			"if a < 0 && b > 0 {",
			"}",
		),
	},
	{"checked conversion", withSized("a, ok := int8(i64)", "if ok {", "    exit(a)", "}")},
	{
		"checked conversion to unsigned",
		withSized(
			"a, ok := uint64(i64)",
			"b, fits := int64(u64)",
			"if ok && fits && a > 0 {",
			"    exit(b)",
			"}",
		),
	},
	{
		"widening arguments and results",
		"package main\n\nfunc f(x int64) int32 {\n    var small int8 = 1\n    if x > 0 {\n        return small\n    }\n    return 0\n}\n\n" +
			"func main() {\n    var u uint8 = 7\n    exit(f(u))\n}\n",
	},
	{
		"checked constant conversion",
		mainLines("a, ok := int8(5)", "if ok {", "    exit(a)", "}"),
	},
	{
		"checked conversion that cannot fail",
		withSized("a, ok := int16(u8)", "if ok {", "    exit(a)", "}"),
	},
	{
		"checked conversion to the same type",
		withSized("a, ok := int8(i8)", "if ok {", "    exit(a)", "}"),
	},
	{
		"comparisons of every signed and unsigned pair",
		withSized(
			"if u32 == i32 || i8 < u8 || u64 >= i64 || i16 != u16 || u8 > i8 || i64 <= u64 {",
			"}",
		),
	},
	{
		"comparisons with known values of other types",
		withSized(
			"if u64 > int64(-1) && i8 < uint64(18446744073709551615) && u8 != int8(-1) {",
			"}",
		),
	},
}

// TestAcceptsIntegerWidths checks each of integerWidthCases has no
// diagnostics.
func TestAcceptsIntegerWidths(t *testing.T) {
	for _, tt := range integerWidthCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// widthErrorCases are misuses of the sized types and their constants, with
// the diagnostics each must produce.
var widthErrorCases = []struct {
	name string
	src  string
	want []spanned
}{
	{
		"uint8 too large",
		mainLines("var a uint8 = 256", "exit(a)"),
		[]spanned{{"constant 256 overflows uint8", "256"}},
	},
	{
		"int8 too small",
		mainLines("var a int8 = -129", "exit(a)"),
		[]spanned{{"constant -129 overflows int8", "-129"}},
	},
	{
		"negative unsigned",
		mainLines("var a uint8 = -1", "exit(a)"),
		[]spanned{{"constant -1 overflows uint8", "-1"}},
	},
	{
		"default int64 too large",
		mainLines("a := 18446744073709551615", "exit(a)"),
		[]spanned{{"constant 18446744073709551615 overflows int64", "18446744073709551615"}},
	},
	{
		"constant expression too large",
		mainLines("var a uint64 = 18446744073709551615 + 1", "if a > 0 {", "}"),
		[]spanned{{"constant expression is out of range", "18446744073709551615 + 1"}},
	},
	{
		"constant expression too small",
		mainLines("a := -9223372036854775808 - 1", "exit(a)"),
		[]spanned{{"constant expression is out of range", "-9223372036854775808 - 1"}},
	},
	{
		"constant division by zero",
		mainLines("a := 7 % 0 + 1", "exit(a)"),
		[]spanned{{"division by zero in constant expression", "7 % 0"}},
	},
	{
		"mismatched signs",
		withSized("exit(i8 + u8)"),
		[]spanned{{"mismatched types int8 and uint8 in +", "i8 + u8"}},
	},
	{
		"uint64 with int64",
		withSized("exit(u64 + i64)"),
		[]spanned{{"mismatched types uint64 and int64 in +", "u64 + i64"}},
	},
	{
		"narrowing declaration",
		withSized("var a int8 = i64", "exit(a)"),
		[]spanned{{"cannot use int64 as int8 in declaration of a", "i64"}},
	},
	{
		"narrowing argument",
		withSized("exit(u64)"),
		[]spanned{{"cannot use uint64 as int64 in argument to exit", "u64"}},
	},
	{
		"narrowing result",
		"package main\n\nfunc f(x int64) int8 {\n    return x\n}\n\nfunc main() {\n    exit(f(1))\n}\n",
		[]spanned{{"cannot use int64 as int8 in return from f", "x"}},
	},
	{
		"narrowing assignment",
		withSized("mut a uint8 = 0", "a = u16", "exit(a)"),
		[]spanned{{"cannot use uint16 as uint8 in assignment to a", "u16"}},
	},
	{
		"narrowing compound",
		withSized("mut a int8 = 0", "a += i16", "exit(a)"),
		[]spanned{{"cannot use int16 as int8 in +=", "i16"}},
	},
	{
		"compound constant too large",
		withSized("mut a uint8 = 0", "a += 300", "exit(a)"),
		[]spanned{{"constant 300 overflows uint8", "300"}},
	},
	{
		"compared constant too large",
		withSized("if u8 < 300 {", "}"),
		[]spanned{{"constant 300 overflows uint8", "300"}},
	},
	{
		"compared constant negative",
		withSized("if u8 == -1 {", "}"),
		[]spanned{{"constant -1 overflows uint8", "-1"}},
	},
	{
		"negate unsigned",
		withSized("exit(-u8)"),
		[]spanned{{"operator - is not defined on uint8", "u8"}},
	},
	{
		"conversion that can fail",
		withSized("a := int8(i64)", "exit(a)"),
		[]spanned{{"converting int64 to int8 can fail; use v, ok := int8(...)", "int8(i64)"}},
	},
	{
		"conversion across signs can fail",
		withSized("a := uint64(i8)", "if a > 0 {", "}"),
		[]spanned{{"converting int8 to uint64 can fail; use v, ok := uint64(...)", "uint64(i8)"}},
	},
	{
		"constant conversion too large",
		mainLines("a := int8(300)", "exit(a)"),
		[]spanned{{"constant 300 overflows int8", "300"}},
	},
	{
		"convert to string",
		mainLines("a := string(5)", "print(a)"),
		[]spanned{{"cannot convert int64 to string", "string(5)"}},
	},
	{
		"convert a bool",
		mainLines("a := int8(true)", "exit(a)"),
		[]spanned{{"cannot convert bool to int8", "int8(true)"}},
	},
	{
		"convert to bool",
		withSized("a := bool(i8)", "if a {", "}"),
		[]spanned{{"cannot convert int8 to bool", "bool(i8)"}},
	},
	{
		"conversion with two arguments",
		withSized("a := int8(i8, i8)", "exit(a)"),
		[]spanned{{"int8 expects 1 argument(s), got 2", "int8(i8, i8)"}},
	},
	{
		"checked conversion bound to three names",
		withSized("a, b, c := int8(i64)", "if b && c {", "    exit(a)", "}"),
		[]spanned{
			{
				"assignment mismatch: 3 variables but a conversion gives 2 values",
				"a, b, c := int8(i64)",
			},
		},
	},
	{
		"checked conversion assigned to the wrong type",
		withSized("mut a int8 = 0", "mut ok := 0", "a, ok = int8(i64)", "exit(a + ok)"),
		[]spanned{{"cannot use bool as int64 in assignment to ok", "ok"}},
	},
	{
		"global too large",
		"package main\n\nvar g uint16 = 65536\n\nfunc main() {\n    exit(g)\n}\n",
		[]spanned{{"constant 65536 overflows uint16", "65536"}},
	},
	{
		"range over a constant too large",
		mainLines("for i := range 18446744073709551615 {", "    exit(i)", "}"),
		[]spanned{{"constant 18446744073709551615 overflows int64", "18446744073709551615"}},
	},
}

// TestRejectsWidthErrors checks each misuse is one error, covering the right
// source.
func TestRejectsWidthErrors(t *testing.T) {
	for _, tt := range widthErrorCases {
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

// TestRecordsIntegerTypes checks the type each declaration and expression is
// given: a constant takes its declared type, or int64 with none, arithmetic
// takes its operands' common type, and a range variable takes its count's.
func TestRecordsIntegerTypes(t *testing.T) {
	file, diags := checkTree(t, mainLines(
		"var a uint8 = 5",
		"b := 5",
		"mut c int16 = 300",
		"d := a + c",
		"e := a + 1",
		"f, ok := int8(c)",
		"for i := range a {",
		"    exit(i)",
		"}",
		"if ok {",
		"    exit(b + d + e + f)",
		"}",
	))
	if len(diags) > 0 {
		t.Fatalf("got %v, want no diagnostics", diags)
	}

	body := file.Decls[0].(*ast.Func).Body
	want := []struct {
		name string
		typ  types.Type
	}{{"a", types.Uint8}, {"b", types.Int64}, {"c", types.Int16}, {"d", types.Int16}, {"e", types.Uint8}}
	for i, w := range want {
		v := body[i].(*ast.Var)
		if v.Name != w.name || v.Type != w.typ {
			t.Errorf("%s has type %v, want %v", v.Name, v.Type, w.typ)
		}
	}
	if lit := body[0].(*ast.Var).Value.(*ast.IntLit); lit.Type != types.Uint8 {
		t.Errorf("the literal 5 assigned to a uint8 has type %v", lit.Type)
	}
	m := body[5].(*ast.MultiVar)
	if m.Vars[0].Type != types.Int8 || m.Vars[1].Type != types.Bool {
		t.Errorf("f, ok have types %v, %v, want int8, bool", m.Vars[0].Type, m.Vars[1].Type)
	}
	if loop := body[6].(*ast.ForRange); loop.Var.Type != types.Uint8 {
		t.Errorf("range variable has type %v, want uint8", loop.Var.Type)
	}
}
