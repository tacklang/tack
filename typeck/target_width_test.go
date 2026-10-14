package typeck_test

import (
	"testing"
)

// targetWidth wraps statements in a function taking a value of every
// integer type the target-width types meet: s an unsafe_size_t, p an
// unsafe_uintptr_t, and u8, u32, u64, i32 and i64 of their types. None is
// known, so a conversion of one is checked by its type alone.
func targetWidth(lines ...string) string {
	body := ""
	for _, l := range lines {
		body += "    " + l + "\n"
	}
	return "package main\n\n" +
		"func f(s unsafe_size_t, p unsafe_uintptr_t, u8 uint8, u32 uint32, u64 uint64, i32 int32, i64 int64) {\n" +
		body +
		"    if s == 0 || p == 0 || u8 == 0 || u32 == 0 || u64 == 0 || i32 == 0 || i64 == 0 {\n        return\n    }\n}\n\n" +
		"func main() {\n    f(1, 2, 3, 4, 5, 6, 7)\n}\n"
}

// acceptedTargetWidthCases are uses of the target-width types that must
// check cleanly: widening into and out of them, their common types,
// constants up to the largest every target holds, known values, and the
// checked conversions that can fail.
var acceptedTargetWidthCases = []struct{ name, src string }{
	{"narrow unsigned widen in", targetWidth(
		"var a unsafe_size_t = u8", "var b unsafe_uintptr_t = u32", "if a == s && b == p {", "}")},
	{
		"widen out to uint64",
		targetWidth("var a uint64 = s", "var b uint64 = p", "if a == b {", "}"),
	},
	{
		"common type with a narrower operand",
		targetWidth("var a unsafe_size_t = u32 + s", "if a > u8 {", "}"),
	},
	{"common type with uint64", targetWidth("var a uint64 = s * u64", "if a > s {", "}")},
	{
		"largest portable constant",
		targetWidth("var a unsafe_size_t = 4294967295", "if a > s && p < 4294967295 {", "}"),
	},
	{
		"constant arithmetic in range",
		targetWidth("a := unsafe_size_t(4294967294) + 1", "if a > s {", "}"),
	},
	{"known value past 32 bits is not known", targetWidth(
		"a := unsafe_size_t(4294967295)", "b := a + 1", "if b > s {", "}")},
	{
		"known value converts",
		targetWidth("a := unsafe_size_t(300)", "b := uint16(a)", "if b > u8 {", "}"),
	},
	{"known value converts to the other", targetWidth(
		"a := unsafe_size_t(300)", "b := unsafe_uintptr_t(a)", "if b > p {", "}")},
	{"constant converts", targetWidth("a := unsafe_uintptr_t(4294967295)", "if a > p {", "}")},
	{
		"checked conversions",
		targetWidth(
			"a, okA := unsafe_size_t(i64)",
			"b, okB := unsafe_uintptr_t(s)",
			"c, okC := uint32(p)",
			"d, okD := int64(s)",
			"e, okE := unsafe_size_t(u64)",
			"g, okG := int8(s)",
			"if okA && okB && okC && okD && okE && okG && a == s && b == p && c == u32 && d == i64 && e == s && g == 0 {",
			"}",
		),
	},
	{"checked known value past 32 bits", targetWidth(
		"x := uint64(5000000000)", "a, ok := unsafe_size_t(x)", "if ok && a == s {", "}")},
	{
		"range over a count",
		targetWidth(
			"mut t unsafe_size_t = 0",
			"for i := range s {",
			"    t += i",
			"}",
			"if t == 0 {",
			"}",
		),
	},
	{"guarded division", targetWidth("if s != 0 {", "    if u32 / s == 0 {", "    }", "}")},
	{
		"compound with a narrower value",
		targetWidth("mut a unsafe_size_t = s", "a += u32", "a *= u8", "if a == 0 {", "}"),
	},
	{
		"array length from a known value",
		targetWidth("n := unsafe_size_t(3)", "a := [n]int64{1, 2, 3}", "exit(a[0])"),
	},
}

// TestAcceptsTargetWidthUses checks each of acceptedTargetWidthCases has no
// diagnostics.
func TestAcceptsTargetWidthUses(t *testing.T) {
	for _, tt := range acceptedTargetWidthCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) != 0 {
				t.Errorf("got %v, want none", diags)
			}
		})
	}
}

// unknownWidth is what an error about a constant too large for a
// target-width type adds.
const unknownWidth = ": its width is unknown, so a constant must be at most 4294967295"

// rejectedTargetWidthCases are uses of the target-width types that must be
// one error, with its message and the source it covers: a widening that
// does not hold at every width, operands with no common type, a
// conversion that can fail, a constant past the largest every target
// holds, and an index.
var rejectedTargetWidthCases = []struct {
	name, src string
	want      spanned
}{
	{
		"signed into size", targetWidth("var a unsafe_size_t = i32", "if a == s {", "}"),
		spanned{"cannot use int32 as unsafe_size_t in declaration of a", "i32"},
	},
	{
		"uint64 into size", targetWidth("var a unsafe_size_t = u64", "if a == s {", "}"),
		spanned{"cannot use uint64 as unsafe_size_t in declaration of a", "u64"},
	},
	{
		"size into int64", targetWidth("var a int64 = s", "if a == i64 {", "}"),
		spanned{"cannot use unsafe_size_t as int64 in declaration of a", "s"},
	},
	{
		"size into uintptr", targetWidth("var a unsafe_uintptr_t = s", "if a == p {", "}"),
		spanned{"cannot use unsafe_size_t as unsafe_uintptr_t in declaration of a", "s"},
	},
	{
		"common type is size, not uint32",
		targetWidth("var a uint32 = u32 + s", "if a == u32 {", "}"),
		spanned{"cannot use unsafe_size_t as uint32 in declaration of a", "u32 + s"},
	},
	{
		"common type is uint64, not size",
		targetWidth("var a unsafe_size_t = s + u64", "if a == s {", "}"),
		spanned{"cannot use uint64 as unsafe_size_t in declaration of a", "s + u64"},
	},
	{
		"size and int64", targetWidth("if s < i64 {", "}"),
		spanned{"mismatched types unsafe_size_t and int64 in <", "s < i64"},
	},
	{
		"size and uintptr", targetWidth("if s * p == 0 {", "}"),
		spanned{"mismatched types unsafe_size_t and unsafe_uintptr_t in *", "s * p"},
	},
	{
		"size to uint32",
		targetWidth("a := uint32(s)", "if a == u32 {", "}"),
		spanned{
			"converting unsafe_size_t to uint32 can fail; use v, ok := uint32(...)",
			"uint32(s)",
		},
	},
	{
		"uintptr to size",
		targetWidth("a := unsafe_size_t(p)", "if a == s {", "}"),
		spanned{
			"converting unsafe_uintptr_t to unsafe_size_t can fail; use v, ok := unsafe_size_t(...)",
			"unsafe_size_t(p)",
		},
	},
	{
		"uint32 constant past 32 bits",
		targetWidth("var a unsafe_size_t = 4294967296", "if a == s {", "}"),
		spanned{"constant 4294967296 overflows unsafe_size_t" + unknownWidth, "4294967296"},
	},
	{
		"negative constant", targetWidth("var a unsafe_uintptr_t = -1", "if a == p {", "}"),
		spanned{"constant -1 overflows unsafe_uintptr_t", "-1"},
	},
	{
		"largest uint64 constant",
		targetWidth("if s == 18446744073709551615 {", "}"),
		spanned{
			"constant 18446744073709551615 overflows unsafe_size_t" + unknownWidth,
			"18446744073709551615",
		},
	},
	{
		"constant step past 32 bits",
		targetWidth("a := unsafe_size_t(65536) * 65536", "if a == s {", "}"),
		spanned{
			"constant expression overflows unsafe_size_t" + unknownWidth,
			"unsafe_size_t(65536) * 65536",
		},
	},
	{
		"constant step below zero", targetWidth("a := unsafe_size_t(1) - 2", "if a == s {", "}"),
		spanned{"constant expression overflows unsafe_size_t", "unsafe_size_t(1) - 2"},
	},
	{
		"known value step below zero",
		targetWidth("a := unsafe_size_t(1)", "b := a - 2", "if b == s {", "}"),
		spanned{"constant expression overflows unsafe_size_t", "a - 2"},
	},
	{
		"known value past 32 bits converted",
		targetWidth(
			"a := unsafe_size_t(4294967295)",
			"b := uint64(a + 1)",
			"c := uint32(a + 1)",
			"if b == u64 && c == u32 {",
			"}",
		),
		spanned{
			"converting unsafe_size_t to uint32 can fail; use v, ok := uint32(...)",
			"uint32(a + 1)",
		},
	},
	{
		"known uint64 past 32 bits",
		targetWidth("x := uint64(4294967296)", "a := unsafe_size_t(x)", "if a == s {", "}"),
		spanned{
			"converting uint64 to unsafe_size_t can fail; use v, ok := unsafe_size_t(...)",
			"unsafe_size_t(x)",
		},
	},
	{
		"checked known value that never fits",
		targetWidth("x := -1", "a, ok := unsafe_size_t(x)", "if ok && a == s {", "}"),
		spanned{"constant -1 overflows unsafe_size_t", "x"},
	},
	{
		"checked constant past 32 bits",
		targetWidth("a, ok := unsafe_size_t(4294967296)", "if ok && a == s {", "}"),
		spanned{"constant 4294967296 overflows unsafe_size_t" + unknownWidth, "4294967296"},
	},
	{
		"index with size", targetWidth("a := [2]int64{1, 2}", "exit(a[s])"),
		spanned{"index cannot be unsafe_size_t; convert it first with i, ok := int64(s)", "s"},
	},
	{
		"checked index with uintptr",
		targetWidth("a := [2]int64{1, 2}", "v, ok := a[p - 1]", "if ok {", "    exit(v)", "}"),
		spanned{
			"index cannot be unsafe_uintptr_t; convert it first with i, ok := int64(p - 1)",
			"p - 1",
		},
	},
	{
		"string index with a known size", targetWidth("n := unsafe_size_t(0)", "exit(\"tack\"[n])"),
		spanned{"index cannot be unsafe_size_t; convert it first with i, ok := int64(n)", "n"},
	},
	{
		"negation", targetWidth("a := -s", "if a == s {", "}"),
		spanned{"operator - is not defined on unsafe_size_t", "s"},
	},
}

// TestRejectsTargetWidthMisuses checks each of rejectedTargetWidthCases is
// one error, covering the right source.
func TestRejectsTargetWidthMisuses(t *testing.T) {
	for _, tt := range rejectedTargetWidthCases {
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
