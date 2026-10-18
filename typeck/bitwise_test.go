package typeck_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/types"
)

// bitwiseFunc returns a program whose function f, taking an int8 a, a
// uint8 b, a uint64 u and a uint8 n, declares x with the value expr first.
func bitwiseFunc(expr string) string {
	return "package main\n\nfunc f(a int8, b uint8, u uint64, n uint8) {\n    x := " + expr +
		"\n    if x == x {\n    }\n}\n\nfunc main() {\n}\n"
}

// declared returns the variable f, in a program bitwiseFunc built, declares
// first, failing the test on any diagnostic.
func declared(t *testing.T, src string) *ast.Var {
	t.Helper()
	file, diags := checkTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %v, want no diagnostics", diags)
	}
	return file.Decls[0].(*ast.Func).Body[0].(*ast.Var)
}

// TestBitwiseResultTypes checks the type each bitwise and shift operator
// gives: & | ^ &^ their operands' common type, an untyped constant taking
// the other's; a shift its left operand's, whatever its count's type; an
// untyped constant shifted by a typed count an int64; and ^x its
// operand's.
func TestBitwiseResultTypes(t *testing.T) {
	for _, tt := range []struct {
		expr string
		want types.Type
	}{
		{"a & 3", types.Int8},
		{"b | 128", types.Uint8},
		{"a ^ a", types.Int8},
		{"b &^ b", types.Uint8},
		{"u << n", types.Uint64},
		{"a >> u", types.Int8},
		{"b << 3", types.Uint8},
		{"1 << n", types.Int64},
		{"^a", types.Int8},
		{"^b", types.Uint8},
		{"b & uint16(7)", types.Uint16},
		{"1 << 3", types.Int64},
	} {
		t.Run(tt.expr, func(t *testing.T) {
			if got := declared(t, bitwiseFunc(tt.expr)).Type; got != tt.want {
				t.Errorf("type = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBitwiseConstantsFoldExactly checks that a bitwise or shift operator
// on untyped constants, and ^ on one, is computed exactly, on the
// infinitely wide two's complement form, as Go's untyped constants are.
func TestBitwiseConstantsFoldExactly(t *testing.T) {
	for _, tt := range []struct {
		expr string
		want int64
	}{
		{"6 & 3", 2},
		{"6 | 3", 7},
		{"6 ^ 3", 5},
		{"6 &^ 3", 4},
		{"-1 & 255", 255},
		{"-8 >> 1", -4},
		{"-1 >> 100", -1},
		{"7 >> 100", 0},
		{"1 << 62", 1 << 62},
		{"0 << 1000", 0},
		{"^5", -6},
		{"^-1", 0},
		{"1 + 2 << 3", 17},
		{"(5 | 2) ^ 1", 6},
	} {
		t.Run(tt.expr, func(t *testing.T) {
			v := declared(t, bitwiseFunc("int64("+tt.expr+")"))
			arg := v.Value.(*ast.Call).Args[0]
			got, ok := constOf(arg)
			if !ok || got.Cmp(constant.FromInt64(tt.want)) != 0 {
				t.Errorf("value = %v (%v), want %d", got, ok, tt.want)
			}
		})
	}
}

// constOf returns the constant the checker recorded on e, an operator.
func constOf(e ast.Expr) (constant.Int, bool) {
	switch x := e.(type) {
	case *ast.Binary:
		v, ok := x.Const.(constant.Int)
		return v, ok
	case *ast.Unary:
		v, ok := x.Const.(constant.Int)
		return v, ok
	case *ast.IntLit:
		return x.Value, true
	default:
		return constant.Int{}, false
	}
}

// TestKnownBitwiseValuesProve checks that bitwise and shift operators on
// known values have known values, as arithmetic does, which prove an index
// in range or a divisor not zero: an immutable variable's & a mask, a
// shift of one, and ^ of an unsigned one, every bit of its width flipped.
// A target-width type's ^ has no known value, its width being the
// target's, and proves nothing.
func TestKnownBitwiseValuesProve(t *testing.T) {
	accepted := []string{
		mainLines(
			"a := [4]int64{1, 2, 3, 4}",
			"k := uint8(7)",
			"code, fits := int32(a[k & 3])",
			"if fits { exit(code) }",
		),
		mainLines(
			"a := [9]int64{0, 0, 0, 0, 0, 0, 0, 0, 1}",
			"k := uint8(1)",
			"code, fits := int32(a[k << 3])",
			"if fits { exit(code) }",
		),
		mainLines(
			"a := [9]int64{0, 0, 0, 0, 0, 0, 0, 0, 1}",
			"k := int64(-16)",
			"code, fits := int32(a[-(k >> 1)])",
			"if fits { exit(code) }",
		),
		mainLines("k := uint8(0)", "x := 255 / ^k", "exit(int32(x))"),
		mainLines("k := int16(-1)", "x := 7 % (^k | 1)", "exit(int32(x))"),
	}
	for _, src := range accepted {
		if diags := check(t, src); len(diags) > 0 {
			t.Errorf("%s: got %v, want no diagnostics", src, diags)
		}
	}
	rejected := []struct{ src, msg string }{
		{
			mainLines("a := [4]int64{1, 2, 3, 4}", "k := uint8(7)", "exit(int32(a[k & 4]))"),
			"index 4 is out of range for [4]int64",
		},
		{
			mainLines(
				"k := unsafe_size_t(0)",
				"x := 6 / ^k",
				"code, fits := int32(x)",
				"if fits { exit(code) }",
			),
			"divisor may be zero; use q, ok := ... / ...",
		},
		{
			mainLines("k := uint8(200)", "x := k << 2", "exit(int32(x))"),
			"constant expression overflows uint8",
		},
		{
			mainLines(
				"x := unsafe_size_t(1) << 40",
				"code, fits := int32(x)",
				"if fits { exit(code) }",
			),
			"constant expression overflows unsafe_size_t",
		},
	}
	for _, tt := range rejected {
		diags := check(t, tt.src)
		if len(diags) == 0 || diags[0].Message[:min(len(diags[0].Message), len(tt.msg))] != tt.msg {
			t.Errorf("%s: got %v, want %q", tt.src, diags, tt.msg)
		}
	}
}

// TestShiftCountTypes checks that a count of every unsigned type, the
// target-width ones included, and an untyped constant that is not
// negative, are accepted, beside every compound form.
func TestShiftCountTypes(t *testing.T) {
	for _, count := range []string{"uint8", "uint16", "uint32", "uint64", "unsafe_size_t", "unsafe_uintptr_t"} {
		src := "package main\n\nfunc f(x int32, n " + count + ") int32 {\n    mut y := x << n\n" +
			"    y >>= n\n    y <<= 3\n    y &= x\n    y |= 1\n    y ^= x\n    y &^= 2\n    return y >> 31\n}\n\n" +
			"func main() {\n}\n"
		if diags := check(t, src); len(diags) > 0 {
			t.Errorf("count %s: got %v, want no diagnostics", count, diags)
		}
	}
}
