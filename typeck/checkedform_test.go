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
		"a division plus one",
		inF2("a int64, b int64", "q, ok := a / b + 1", "if ok {", "    return q", "}"),
		[]spanned{{
			"the checked form must be the whole value: q, ok := a / b, then compute q + 1",
			"a / b + 1",
		}},
	},
	{
		"a remainder times a variable",
		inF2("a int64, b int64, c int64", "q, ok := a % b * c", "if ok {", "    return q", "}"),
		[]spanned{{
			"the checked form must be the whole value: q, ok := a % b, then compute q * c",
			"a % b * c",
		}},
	},
	{
		"an index plus one",
		inF2("a [3]int64, i int64", "v, ok := a[i] + 1", "if ok {", "    return v", "}"),
		[]spanned{{
			"the checked form must be the whole value: v, ok := a[i], then compute v + 1",
			"a[i] + 1",
		}},
	},
	{
		"a string's byte in parentheses",
		inF2("s string, i int64", "v, ok := (s[i]) + 1", "if ok {", "    return int64(v)", "}"),
		[]spanned{{
			"the checked form must be the whole value: v, ok := s[i], then compute (v) + 1",
			"(s[i]) + 1",
		}},
	},
	{
		"an assignment",
		inF2("a int64, b int64",
			"mut q int64 = 0",
			"mut ok := false",
			"q, ok = 1 + a / b",
			"if ok {",
			"    return q",
			"}",
		),
		[]spanned{{
			"the checked form must be the whole value: q, ok = a / b, then compute 1 + q",
			"1 + a / b",
		}},
	},
	{
		"a blank first name",
		inF2("a int64, b int64", "_, ok := a / b + 1", "if ok {", "    return 1", "}"),
		[]spanned{{
			"the checked form must be the whole value: _, ok := a / b, then use its value",
			"a / b + 1",
		}},
	},
	{
		"two divisions, the second reported on its own",
		inF2(
			"a int64, b int64, c int64, d int64",
			"q, ok := a / b + c / d",
			"if ok {",
			"    return q",
			"}",
		),
		[]spanned{
			{
				"the checked form must be the whole value: q, ok := a / b, then compute q + c / d",
				"a / b + c / d",
			},
			{"divisor d may be zero; check d != 0 first, or use q, ok := ... / d", "d"},
		},
	},
	{
		"a known zero divisor",
		inF2("a int64", "q, ok := a / 0 + 1", "if ok {", "    return q", "}"),
		[]spanned{
			{
				"the checked form must be the whole value: q, ok := a / 0, then compute q + 1",
				"a / 0 + 1",
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

// TestMismatchStandsWithoutACheckedOperation checks that a value holding no
// division, remainder or index outside a call, or bound to other than two
// names, keeps the plain mismatch.
func TestMismatchStandsWithoutACheckedOperation(t *testing.T) {
	for _, tt := range []struct{ src, msg string }{
		{inF2("a int64", "q, ok := a + 1", "if ok {", "    return q", "}"), "assignment mismatch: 2 variables but 1 value"},
		{
			inF2("a int64, b int64", "q, ok, r := a / b + 1", "if ok {", "    return q + r", "}"),
			"assignment mismatch: 3 variables but 1 value",
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
