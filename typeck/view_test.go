package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// viewing returns a program whose function f takes v, a view of int64,
// b, a view of bytes, ps, a view of P, a struct with a field d, i, an
// int64, and returns an int64, and whose body is lines, each on a line of
// its own, followed by return 0.
func viewing(lines ...string) string {
	return "package main\n\ntype P struct {\n    d int64\n    a [2]int64\n}\n\n" +
		"func g(v []int64) int64 {\n    return 0\n}\n\n" +
		"func f(v []int64, b []uint8, ps []P, i int64) int64 {\n    " +
		strings.Join(lines, "\n    ") + "\n    return 0\n}\n\nfunc main() {\n}\n"
}

// viewProofCases prove an index into a view as one into a string of
// unknown length is proven, and use the facts a view's elements carry.
var viewProofCases = []struct{ name, src string }{
	{
		"counting loop",
		viewing(
			"for mut j int64 = 0; j < len(v); j++ {",
			"    code, fits := int32(v[j])",
			"    if fits { exit(code) }",
			"}",
		),
	},
	{
		"range index",
		viewing(
			"for j := range v {",
			"    code, fits := int32(v[j])",
			"    if fits { exit(code) }",
			"}",
		),
	},
	{
		"range over a view indexes another by its own length",
		viewing(
			"for j := range b {",
			"    if j < len(v) {",
			"        code, fits := int32(v[j])",
			"        if fits { exit(code) }",
			"    }",
			"}",
		),
	},
	{
		"guard",
		viewing(
			"if 0 <= i && i < len(v) {",
			"    code, fits := int32(v[i])",
			"    if fits { exit(code) }",
			"}",
		),
	},
	{
		"early exit",
		viewing(
			"if i < 0 || i >= len(v) {",
			"    return 0",
			"}",
			"code, fits := int32(v[i])",
			"if fits { exit(code) }",
		),
	},
	{
		"length bound proves a constant index",
		viewing(
			"if len(v) > 2 {",
			"    code, fits := int32(v[2])",
			"    if fits { exit(code) }",
			"}",
		),
	},
	{
		"length fact holds after a call",
		viewing(
			"if len(v) < 1 {",
			"    return 0",
			"}",
			"g(v)",
			"code, fits := int32(v[0])",
			"if fits { exit(code) }",
		),
	},
	{
		"checked form",
		viewing(
			"x, ok := v[i]",
			"if ok {",
			"    code, fits := int32(x)",
			"    if fits { exit(code) }",
			"}",
		),
	},
	{
		"field of an element carries facts",
		viewing(
			"if len(ps) > 0 && ps[0].d != 0 {",
			"    code, fits := int32(10 / ps[0].d)",
			"    if fits { exit(code) }",
			"}",
		),
	},
	{
		"element of an element's array",
		viewing(
			"if len(ps) > 0 {",
			"    code, fits := int32(ps[0].a[1])",
			"    if fits { exit(code) }",
			"}",
		),
	},
	{
		"len is an int64",
		viewing("var n int64 = len(b)", "code, fits := int32(n)", "if fits { exit(code) }"),
	},
	{"passed on", viewing("code, fits := int32(g(v))", "if fits { exit(code) }")},
	{"byte element", viewing("for j := range b {", "    exit(b[j])", "}")},
}

// TestAcceptsViewProofs checks each of viewProofCases has no diagnostics.
func TestAcceptsViewProofs(t *testing.T) {
	for _, tt := range viewProofCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// viewErrorCases are uses of a view that are errors, each with the one
// diagnostic it gives.
var viewErrorCases = []struct{ name, src, want string }{
	{
		"index past a length bound",
		viewing(
			"if len(v) > 2 {",
			"    code, fits := int32(v[3])",
			"    if fits { exit(code) }",
			"}",
		),
		"index 3 may be out of range; check len(v) > 3 first, or use v, ok := ...[3]",
	},
	{
		"negative constant index",
		viewing("code, fits := int32(v[-1])", "if fits { exit(code) }"),
		"index -1 is out of range for a view",
	},
	{
		"negative constant checked index", viewing("x, ok := v[-1]", "if ok {", "    exit(x)", "}"),
		"index -1 is out of range for a view",
	},
	{
		"bound by another view's length",
		viewing(
			"if 0 <= i && i < len(b) {",
			"    code, fits := int32(v[i])",
			"    if fits { exit(code) }",
			"}",
		),
		"index i may be out of range; check 0 <= i && i < len(v) first, or use v, ok := ...[i]",
	},
	{"index into an element", viewing("v[i]++"), "cannot assign to v[i]: v is a read-only view"},
	{"compound assignment", viewing("v[0] += 1"), "cannot assign to v[0]: v is a read-only view"},
	{
		"element of an element", viewing("ps[0].a[1] = 2"),
		"cannot assign to ps[0].a[1]: ps is a read-only view",
	},
	{
		"multi assign",
		viewing(
			"mut k int64 = 0",
			"k, v = 10 / 2",
			"code, fits := int32(k)",
			"if fits { exit(code) }",
		),
		"cannot assign to v: v is a read-only view",
	},
	{"conversion of a view", viewing("exit(int64(v))"), "cannot convert []int64 to int64"},
	{"arithmetic", viewing("exit(len(v) + v)"), "operator + is not defined on []int64"},
	{"not a bool", viewing("if b {", "}"), "condition must be a bool, got []uint8"},
	{
		"view of another element type",
		viewing("code, fits := int32(g(b))", "if fits { exit(code) }"),
		"cannot use []uint8 as []int64 in argument to g",
	},
}

// TestRejectsViewMisuse checks each of viewErrorCases gives exactly its
// diagnostic.
func TestRejectsViewMisuse(t *testing.T) {
	for _, tt := range viewErrorCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != 1 || diags[0].Message != tt.want {
				t.Errorf("got %v, want one diagnostic %q", diags, tt.want)
			}
		})
	}
}

// TestRecordsViewTypes checks a view parameter's type is a view of its
// element type, the same type wherever it is written, and that the
// conversion []uint8(s) records the []uint8 view on its call.
func TestRecordsViewTypes(t *testing.T) {
	src := "package main\n\nfunc f(a []int64, b []int64, c []uint8) {\n}\n\n" +
		"func main() {\n    x := [2]int64{1, 2}\n    f(x, x, []uint8(\"s\"))\n}\n"
	file, diags := checkTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %v", diags)
	}
	table := file.Types
	params := file.Decls[0].(*ast.Func).Params
	if !table.IsView(params[0].Type) || table.Elem(params[0].Type) != types.Int64 {
		t.Errorf("a is %s, want []int64", table.Name(params[0].Type))
	}
	if params[0].Type != params[1].Type {
		t.Error("[]int64 written twice is two types")
	}
	main := file.Decls[1].(*ast.Func)
	call := main.Body[1].(*ast.ExprStmt).X.(*ast.Call)
	if conv := call.Args[2].(*ast.Call); conv.Conv != params[2].Type {
		t.Errorf("[]uint8(\"s\") converts to %s, want []uint8", table.Name(conv.Conv))
	}
}
