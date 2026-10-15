package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// slicing returns a program whose function f takes s, a string, v, a view
// of int64, a, an array of five int64, i and j, int64s, u, a uint64, z, an
// unsafe_size_t, and k, an int8, and returns an int64, and whose body is
// lines, each on a line of its own, followed by return 0. g takes a view
// of int64 and h a string.
func slicing(lines ...string) string {
	return "package main\n\nfunc g(v []int64) int64 {\n    return 0\n}\n\n" +
		"func h(s string) int64 {\n    return 0\n}\n\n" +
		"func f(s string, v []int64, a [5]int64, i int64, j int64, u uint64, " +
		"z unsafe_size_t, k int8) int64 {\n    " +
		strings.Join(lines, "\n    ") + "\n    return 0\n}\n\nfunc main() {\n}\n"
}

// sliceProofCases are slices whose bounds are proven, each one way a proof
// can be had, and the places a slice may be used.
var sliceProofCases = []struct{ name, src string }{
	{"every form of a known string", slicing(
		`t := "hello"`, "exit(h(t[1:3]) + h(t[:2]) + h(t[3:]) + h(t[:]) + h(t[5:]) + h(t[0:0]))")},
	{
		"every form of an array",
		slicing("exit(g(a[1:3]) + g(a[:2]) + g(a[3:]) + g(a[:]) + g(a[5:5]))"),
	},
	{"omitted bounds need nothing", slicing("exit(h(s[:]) + g(v[:]))")},
	{"zero bounds of any string", slicing("exit(h(s[0:0]) + h(s[:0]) + g(v[0:0]))")},
	{"len of the string", slicing("exit(h(s[len(s):]) + h(s[:len(s)]) + h(s[0:len(s)]))")},
	{
		"guard on both bounds",
		slicing("if 0 <= i && i <= j && j <= len(s) {", "    exit(h(s[i:j]))", "}"),
	},
	{"guard written the other way", slicing(
		"if i >= 0 && j >= i && len(s) >= j {", "    exit(h(s[i:j]))", "}")},
	{"strict guard", slicing("if 0 <= i && i < j && j < len(s) {", "    exit(h(s[i:j]))", "}")},
	{"early exit", slicing("if i < 0 || i > len(s) {", "    return 0", "}", "exit(h(s[i:]))")},
	{"negated guard", slicing("if !(j > len(v)) && j >= 0 {", "    exit(g(v[:j]))", "}")},
	{"unsigned high bound", slicing("if u <= len(s) {", "    exit(h(s[:u]))", "}")},
	{"unsafe_size_t bound", slicing("if z <= len(v) {", "    exit(g(v[:z]) + g(v[z:]))", "}")},
	{"int8 bound", slicing("if 0 <= k && k <= len(s) {", "    exit(h(s[k:]))", "}")},
	{"mixed types in order", slicing(
		"if 0 <= k && u <= len(s) && k <= u {", "    exit(h(s[k:u]))", "}")},
	{"same bound twice", slicing("if 0 <= i && i <= len(s) {", "    exit(h(s[i:i]))", "}")},
	{"array bound by a constant", slicing("if 0 <= i && i <= 3 && 3 <= j && j <= 5 {",
		"    exit(g(a[i:j]))", "}")},
	{
		"array bound by its len",
		slicing("if 0 <= i && i <= len(a) {", "    exit(g(a[i:]) + g(a[:i]))", "}"),
	},
	{
		"length bound proves a constant",
		slicing("if len(s) >= 2 {", "    exit(h(s[2:]) + h(s[1:2]))", "}"),
	},
	{
		"counting loop",
		slicing("for mut n := 0; n < len(s); n++ {", "    exit(h(s[n:]) + h(s[:n]))", "}"),
	},
	{"range loop", slicing("for n := range v {", "    exit(g(v[n:]) + g(v[:n]))", "}")},
	{"slice of a slice", slicing("exit(g(a[1:][1:3]) + g(a[:4][2:]))")},
	{"len of a view slice", slicing("exit(len(a[1:]) + len(v[:]))")},
	{"checked form needs no proof", slicing("t, ok := s[i:j]", "if ok {", "    exit(h(t))", "}")},
	{"checked form assigned", slicing(
		`mut t string`, "mut ok bool", "t, ok = s[u:]", "if ok {", "    exit(h(t))", "}")},
	{"string slice stored anywhere", slicing(
		`t := "abc"[1:]`, "var w [2]string = [2]string{t, s[:]}", "exit(h(w[0]))")},
	{
		"slice of a string's slice indexed",
		slicing(`t := s[:]`, "if len(t) > 0 {", "    exit(t[0])", "}"),
	},
	{"byte of a known slice", slicing(`exit("abcdef"[2:4][1])`)},
}

// TestAcceptsSliceProofs checks each of sliceProofCases has no diagnostics.
func TestAcceptsSliceProofs(t *testing.T) {
	for _, tt := range sliceProofCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// sliceErrorCases are slices that are errors, each with the one
// diagnostic it gives.
var sliceErrorCases = []struct{ name, src, want string }{
	{
		"no proof", slicing("exit(h(s[i:j]))"),
		"slice bounds i:j may be out of range; check 0 <= i && i <= j && j <= len(s) first, " +
			"or use v, ok := ...[i:j]",
	},
	{
		"lower only", slicing("if i <= len(s) {", "    exit(h(s[i:]))", "}"),
		"slice bound i may be out of range; check 0 <= i first, or use v, ok := ...[i:]",
	},
	{
		"high only from the start",
		slicing("exit(h(s[:j]))"),
		"slice bound j may be out of range; check 0 <= j && j <= len(s) first, or use v, ok := ...[:j]",
	},
	{
		"unsigned high bound needs only the length", slicing("exit(h(s[:u]))"),
		"slice bound u may be out of range; check u <= len(s) first, or use v, ok := ...[:u]",
	},
	{
		"order", slicing("if 0 <= i && j <= len(s) {", "    exit(h(s[i:j]))", "}"),
		"slice bounds i:j may be out of range; check i <= j first, or use v, ok := ...[i:j]",
	},
	{
		"guard by another relation",
		slicing("if 0 <= i && j <= len(s) && j <= i {", "    exit(h(s[i:j]))", "}"),
		"slice bounds i:j may be out of range; check i <= j first, or use v, ok := ...[i:j]",
	},
	{
		"view has no checked form", slicing("exit(g(v[i:]))"),
		"slice bound i may be out of range; check 0 <= i && i <= len(v) first",
	},
	{
		"array bound past its length", slicing("if 0 <= i && i <= 6 {", "    exit(g(a[i:]))", "}"),
		"slice bound i may be out of range; check i <= 5 first",
	},
	{
		"constant needs the length", slicing("exit(h(s[2:]))"),
		"slice bound 2 may be out of range; check len(s) >= 2 first, or use v, ok := ...[2:]",
	},
	{
		"not a name", slicing("exit(h(s[i - 1:]))"),
		"slice bound ... may be out of range; use v, ok := ...[...:]",
	},
	{"out of order", slicing("exit(h(s[3:1]))"), "slice bounds 3:1 are out of order"},
	{
		"out of order in the checked form",
		slicing("t, ok := s[3:1]", "if ok {", "    exit(h(t))", "}"),
		"slice bounds 3:1 are out of order",
	},
	{"negative", slicing("exit(h(s[-1:]))"), "slice bound -1 is out of range for a string"},
	{"past an array", slicing("exit(g(a[:6]))"), "slice bound 6 is out of range for [5]int64"},
	{
		"past a known string",
		slicing(`exit(h("ab"[3:]))`),
		"slice bound 3 is out of range for a string of 2 bytes",
	},
	{
		"past a known view slice",
		slicing("exit(g(a[1:3][:3]))"),
		"slice bound 3 is out of range for a view of 2 elements",
	},
	{
		"not an integer bound",
		slicing(`exit(h(s["a":]))`),
		"slice bound must be an integer, got string",
	},
	{"not sliceable", slicing("exit(h(i[1:]))"), "cannot slice int64"},
	{
		"temporary array", slicing("exit(g([2]int64{1, 2}[1:]))"),
		"cannot slice a temporary array; store it in a variable first",
	},
	{
		"view stored", slicing("w := a[1:]", "exit(g(w))"),
		"a[1:] is a view, []int64, which can only be passed to a view parameter or to len",
	},
	{
		"view compared", slicing("if v[1:] == 1 {", "}"),
		"v[1:] is a view, []int64, which can only be passed to a view parameter or to len",
	},
	{
		"view printed", slicing("print(a[:])"),
		"a[:] is a view, []int64, which can only be passed to a view parameter or to len",
	},
	{
		"view checked", slicing("w, ok := a[i:]", "if ok {", "    exit(g(w))", "}"),
		"a[i:] is a view, []int64, which cannot be bound with ok; prove its bounds instead",
	},
	{
		"view passed to a string", slicing("exit(h(a[1:]))"),
		"a[1:] is a view, []int64, which can only be passed to a view parameter or to len",
	},
	{
		"assigned",
		slicing("mut t := s", "t[1:] = s", "exit(h(t))"),
		"cannot assign to t[1:]: a slice is not a place",
	},
	{
		"element assigned", slicing("mut b := a", "b[1:][0] = 1", "exit(g(b))"),
		"cannot assign to b[1:][0]: a slice is not a place",
	},
	{
		"string's byte of a slice assigned", slicing("mut t := s", "t[1:][0] = 1", "exit(h(t))"),
		"cannot assign to t[1:][0]: a slice is not a place",
	},
	{
		"incremented",
		slicing("mut b := a", "b[1:]++", "exit(g(b))"),
		"cannot assign to b[1:]: a slice is not a place",
	},
	{
		"wrong count",
		slicing("t, _, _ := s[:]", "exit(h(t))"),
		"assignment mismatch: 3 variables but a slice gives 2 values",
	},
}

// TestRejectsSliceMisuse checks each of sliceErrorCases gives exactly its
// diagnostic.
func TestRejectsSliceMisuse(t *testing.T) {
	for _, tt := range sliceErrorCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != 1 || diags[0].Message != tt.want {
				t.Errorf("got %v, want one diagnostic %q", diags, tt.want)
			}
		})
	}
}

// TestRecordsSliceTypes checks a string's slice is a string and an
// array's or a view's a view of its elements, the same type a view
// parameter of that element has.
func TestRecordsSliceTypes(t *testing.T) {
	file, diags := checkTree(t, slicing("exit(h(s[:]) + g(a[1:]) + g(v[:]))"))
	if len(diags) > 0 {
		t.Fatalf("got %v", diags)
	}
	table := file.Types
	f := file.Decls[2].(*ast.Func)
	var got []types.Type
	ast.Walk(f, func(n ast.Node) bool {
		if s, ok := n.(*ast.Slice); ok {
			got = append(got, s.Type)
		}
		return true
	})
	want := []types.Type{types.String, f.Params[1].Type, f.Params[1].Type}
	if len(got) != len(want) {
		t.Fatalf("found %d slices, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("slice %d is %s, want %s", i, table.Name(got[i]), table.Name(want[i]))
		}
	}
}

// TestKnownSliceValues checks a slice of a known string at known bounds is
// a known string, whose length is a known value usable as an array's
// length, and a view slice's length at known bounds is known too.
func TestKnownSliceValues(t *testing.T) {
	src := slicing(`t := "abcdef"[1:4]`, "mut w [len(t)]int64", "mut x [len(a[1:4])]int64",
		"w[2] = 1", "x[2] = 1", "exit(w[2] + x[2] + 10 / (len(t) - 2))")
	if _, diags := checkTree(t, src); len(diags) > 0 {
		t.Errorf("got %v, want no diagnostics", diags)
	}
	src = slicing(`t := "abcdef"[1:4]`, "exit(10 / (len(t) - 3))")
	if _, diags := checkTree(t, src); len(diags) != 1 || diags[0].Message != "division by zero" {
		t.Errorf("got %v, want division by zero", diags)
	}
}

// TestSliceNotes checks a slice bound's error explains what the guards in
// scope prove about it, as an index's does.
func TestSliceNotes(t *testing.T) {
	src := "package main\n\nfunc f(s string, n int64) string {\n    mut i := n\n" +
		"    if 0 <= i && i <= len(s) {\n        i++\n        return s[i:]\n    }\n    return s\n}\n\n" +
		"func main() {\n}\n"
	expectNotes(
		t,
		src,
		"slice bound i may be out of range; check 0 <= i && i <= len(s) first, or use v, ok := ...[i:]",
		wantNote{"this guard proves 0 <= i && i <= len(s)", "0 <= i && i <= len(s)", 5, 8},
		wantNote{"but i is assigned here, so the proof ends", "i", 6, 9},
	)
	src = "package main\n\nfunc f(a [3]int64, j int64) int64 {\n    if 0 <= j && j <= 4 {\n" +
		"        return g(a[:j])\n    }\n    return 0\n}\n\nfunc g(v []int64) int64 {\n    return 0\n}\n\n" +
		"func main() {\n}\n"
	expectNotes(t, src, "slice bound j may be out of range; check j <= 3 first",
		wantNote{"this guard allows j == 4, and a has 3 elements", "j <= 4", 4, 18},
	)
}
