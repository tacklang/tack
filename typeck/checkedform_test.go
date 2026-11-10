package typeck_test

import (
	"testing"
)

// notWholeCases bind two names to a value that holds a division, a
// remainder or an index but is not one, with the errors each must give and
// the source each covers, in order.
var notWholeCases = []struct {
	name string
	src  string
	want []spanned
}{
	{
		"a division shifted",
		inF2("a int64, b int64", "q, ok := a / b << 1", "if ok {", "    return q", "}"),
		[]spanned{{
			"the checked form must be the whole value: q, ok := a / b, then compute q << 1",
			"a / b << 1",
		}},
	},
	{
		"a remainder and a variable",
		inF2("a int64, b int64, c int64", "q, ok := a % b & c", "if ok {", "    return q", "}"),
		[]spanned{{
			"the checked form must be the whole value: q, ok := a % b, then compute q & c",
			"a % b & c",
		}},
	},
	{
		"an index shifted",
		inF2("a [3]int64, i int64", "v, ok := a[i] << 1", "if ok {", "    return v", "}"),
		[]spanned{{
			"the checked form must be the whole value: v, ok := a[i], then compute v << 1",
			"a[i] << 1",
		}},
	},
	{
		"a string's byte in parentheses",
		inF2("s string, i int64", "v, ok := (s[i]) << 1", "if ok {", "    return int64(v)", "}"),
		[]spanned{{
			"the checked form must be the whole value: v, ok := s[i], then compute (v) << 1",
			"(s[i]) << 1",
		}},
	},
	{
		"an assignment",
		inF2("a int64, b int64",
			"mut q int64 = 0",
			"mut ok := false",
			"q, ok = 1 | a / b",
			"if ok {",
			"    return q",
			"}",
		),
		[]spanned{{
			"the checked form must be the whole value: q, ok = a / b, then compute 1 | q",
			"1 | a / b",
		}},
	},
	{
		"a blank first name",
		inF2("a int64, b int64", "_, ok := a / b << 1", "if ok {", "    return 1", "}"),
		[]spanned{{
			"the checked form must be the whole value: _, ok := a / b, then use its value",
			"a / b << 1",
		}},
	},
	{
		"two divisions, the second reported on its own",
		inF2(
			"a int64, b int64, c int64, d int64",
			"q, ok := a / b ^ c / d",
			"if ok {",
			"    return q",
			"}",
		),
		[]spanned{
			{
				"the checked form must be the whole value: q, ok := a / b, then compute q ^ c / d",
				"a / b ^ c / d",
			},
			{"divisor d may be zero; check d != 0 first, or use q, ok := ... / d", "d"},
		},
	},
	{
		"a known zero divisor",
		inF2("a int64", "q, ok := a / 0 << 1", "if ok {", "    return q", "}"),
		[]spanned{
			{
				"the checked form must be the whole value: q, ok := a / 0, then compute q << 1",
				"a / 0 << 1",
			},
			{"division by zero", "0"},
		},
	},
}

// TestCheckedFormMustBeTheWholeValue checks that two names bound to a value
// that holds an operation the checked form is for, but is more than it,
// give one error for the form, naming the operation and the rest of the
// value, and no unproven proof for that operation.
func TestCheckedFormMustBeTheWholeValue(t *testing.T) {
	for _, tt := range notWholeCases {
		t.Run(tt.name, func(t *testing.T) {
			diags := check(t, tt.src)
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

// outermostCheckedCases bind two names to + - * or unary - on integers
// that hold a division or an index: the outermost operation is the
// checked form, of arithmetic, so the operation inside it is a plain one,
// which needs its own proof, with the errors each must give and the
// source each covers, in order.
var outermostCheckedCases = []struct {
	name string
	src  string
	want []spanned
}{
	{
		"a division plus one",
		inF2("a int64, b int64", "q, ok := a / b + 1", "if ok {", "    return q", "}"),
		[]spanned{{"divisor b may be zero; check b != 0 first, or use q, ok := ... / b", "b"}},
	},
	{
		"a proven division plus one",
		inF2("a int64, b int64", "if b != 0 {", "    q, ok := a / b + 1", "    if ok {", "        return q", "    }", "}"),
		nil,
	},
	{
		"an index times a variable",
		inF2("a [3]int64, i int64, c int64", "v, ok := a[i] * c", "if ok {", "    return v", "}"),
		[]spanned{{"index i may be out of range; check 0 <= i && i < 3 first, or use v, ok := ...[i]", "i"}},
	},
	{
		"a known zero divisor",
		inF2("a int64", "q, ok := 1 - a / 0", "if ok {", "    return q", "}"),
		[]spanned{{"division by zero", "0"}},
	},
	{
		"a negated division",
		inF2("a int64, b int64", "q, ok := -(a / b)", "if ok {", "    return q", "}"),
		[]spanned{{"divisor b may be zero; check b != 0 first, or use q, ok := ... / b", "b"}},
	},
}

// TestOutermostArithmeticIsChecked checks that two names bound to + - *
// or unary - holding a division or an index check the arithmetic, and
// leave the operation inside it to its own proof, with no error for the
// form.
func TestOutermostArithmeticIsChecked(t *testing.T) {
	for _, tt := range outermostCheckedCases {
		t.Run(tt.name, func(t *testing.T) {
			diags := check(t, tt.src)
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

// TestMismatchStandsWithoutACheckedOperation checks that a value holding no
// division, remainder or index outside a call, or bound to other than two
// names, keeps the plain mismatch, and that arithmetic bound to three says
// its checked form gives two.
func TestMismatchStandsWithoutACheckedOperation(t *testing.T) {
	for _, tt := range []struct{ src, msg string }{
		{inF2("a int64", "q, ok := a << 1", "if ok {", "    return q", "}"), "assignment mismatch: 2 variables but 1 value"},
		{
			inF2("a int64, b int64", "q, ok, r := a / b << 1", "if ok {", "    return q +% r", "}"),
			"assignment mismatch: 3 variables but 1 value",
		},
		{
			inF2("a int64, b int64", "q, ok, r := a + b", "if ok {", "    return q +% r", "}"),
			"assignment mismatch: 3 variables but an addition gives 2 values",
		},
	} {
		diags := check(t, tt.src)
		found := false
		for _, d := range diags {
			found = found || d.Message == tt.msg
		}
		if !found {
			t.Errorf("%q: got %v, want %q", tt.src, diags, tt.msg)
		}
	}
}
