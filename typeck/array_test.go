package typeck_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
)

// arrayCases are array programs that must check cleanly.
var arrayCases = []struct{ name, src string }{
	{"literal", mainLines("a := [3]int64{1, 2, 3}", "code, _ := int32(a[0])", "exit(code)")},
	{
		"inferred length",
		mainLines("a := [...]int64{1, 2, 3, 4}", "code, _ := int32(a[3] + len(a))", "exit(code)"),
	},
	{"empty", mainLines("a := [0]bool{}", "exit(int32(len(a)))")},
	{"zero value", mainLines("mut a [4]uint8", "a[3] = 255", "exit(a[3])")},
	{
		"elements widen",
		mainLines(
			"var small int8 = 1",
			"a := [2]int64{small, 300}",
			"code, _ := int32(a[1])",
			"exit(code)",
		),
	},
	{"constants take the element type", mainLines("a := [2]uint8{0, 255}", "exit(a[1])")},
	{
		"nested with types left out",
		mainLines("g := [2][2]bool{{true, false}, {false, true}}", "if g[1][1] {", "}"),
	},
	{
		"nested with types written",
		mainLines(
			"g := [2][1]int64{[1]int64{1}, [1]int64{2}}",
			"code, _ := int32(g[1][0])",
			"exit(code)",
		),
	},
	{
		"copied",
		mainLines(
			"a := [2]int64{1, 2}",
			"mut b := a",
			"b[0] = 5",
			"code, _ := int32(a[0] + b[0])",
			"exit(code)",
		),
	},
	{
		"compared",
		mainLines(
			"a := [2]int64{1, 2}",
			"if a == [2]int64{1, 2} && a != [...]int64{2, 1} {",
			"}",
		),
	},
	{
		"nested compared",
		mainLines("a := [2][2]bool{{true, false}, {false, true}}", "if a == a {", "}"),
	},
	{
		"constant length variable",
		mainLines(
			"n := 3",
			"mut a [n]int64",
			"a[2] = 1",
			"code, _ := int32(a[n - 1])",
			"exit(code)",
		),
	},
	{
		"length from len",
		mainLines(
			"a := [3]int64{1, 2, 3}",
			"mut b [len(a) * 2]int64",
			"b[5] = a[2]",
			"code, _ := int32(b[5])",
			"exit(code)",
		),
	},
	{"length of a string literal", mainLines("mut a [len(\"tack\")]bool", "if a[3] {", "}")},
	{"len of a string", mainLines("s := \"abc\"", "exit(int32(len(s) + len(\"de\")))")},
	{
		"index of any integer type",
		mainLines(
			"var i uint8 = 1",
			"a := [2]int64{1, 2}",
			"code, _ := int32(a[1] + len(a))",
			"exit(code)",
			"if i > 0 {",
			"}",
		),
	},
	{
		"element compound and increment",
		mainLines(
			"mut a [2]int64",
			"a[0] += 5",
			"a[1]++",
			"a[1] *= a[0]",
			"code, _ := int32(a[1])",
			"exit(code)",
		),
	},
	{
		"element assigned several values",
		"package main\n\nfunc p() (int64, bool) {\n    return 1, true\n}\n\n" +
			"func main() {\n    mut a [2]int64\n    mut b [1]bool\n    a[1], b[0] = p()\n    if b[0] {\n        code, _ := int32(a[1])\n        exit(code)\n    }\n}\n",
	},
	{
		"parameter and result",
		"package main\n\nfunc rev(v [2]int64) [2]int64 {\n    return [2]int64{v[1], v[0]}\n}\n\n" +
			"func main() {\n    r := rev([2]int64{1, 2})\n    code, _ := int32(r[0])\n    exit(code)\n}\n",
	},
	{
		"several results",
		"package main\n\nfunc pair() ([2]int64, bool) {\n    return [2]int64{1, 2}, true\n}\n\n" +
			"func main() {\n    a, ok := pair()\n    if ok {\n        code, _ := int32(a[1])\n        exit(code)\n    }\n}\n",
	},
	{
		"global",
		"package main\n\nvar primes = [3]int64{2, 3, 5}\n\nmut grid [2][2]bool\n\nfunc main() {\n    grid[0][1] = true\n    if grid[0][1] {\n        exit(int32(primes[2]))\n    }\n}\n",
	},
	{
		"global length",
		"package main\n\nvar n = 4\n\nmut buf [n]uint8\n\nfunc f(v [n]uint8) uint8 {\n    return v[3]\n}\n\nfunc main() {\n    exit(f(buf))\n}\n",
	},
	{"strings", mainLines("names := [2]string{\"a\", \"b\"}", "print(names[1])")},
	{
		"string arrays compared",
		mainLines(
			"a := [1]string{\"x\"}",
			"if a == a || a != [1]string{\"y\"} {",
			"    exit(1)",
			"}",
		),
	},
	{
		"index of a call",
		"package main\n\nfunc f() [2]int64 {\n    return [2]int64{1, 2}\n}\n\nfunc main() {\n    code, _ := int32(f()[1])\n    exit(code)\n}\n",
	},
}

// TestAcceptsArrays checks each of arrayCases has no diagnostics.
func TestAcceptsArrays(t *testing.T) {
	for _, tt := range arrayCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// arrayErrorCases are misuses of arrays, with the diagnostics each must
// produce.
var arrayErrorCases = []struct {
	name string
	src  string
	want []spanned
}{
	{
		"too many elements",
		mainLines("a := [2]int64{1, 2, 3}", "code, _ := int32(a[0])", "exit(code)"),
		[]spanned{{"[2]int64 literal has 3 elements, want 2", "[2]int64{1, 2, 3}"}},
	},
	{
		"too few elements",
		mainLines("a := [3]int64{1}", "code, _ := int32(a[0])", "exit(code)"),
		[]spanned{{"[3]int64 literal has 1 element, want 3", "[3]int64{1}"}},
	},
	{
		"empty literal of a longer type",
		mainLines("a := [2]int64{}", "code, _ := int32(a[0])", "exit(code)"),
		[]spanned{{"[2]int64 literal has 0 elements, want 2", "[2]int64{}"}},
	},
	{
		"element of the wrong type",
		mainLines("a := [2]int64{1, \"x\"}", "code, _ := int32(a[0])", "exit(code)"),
		[]spanned{{"cannot use string as int64 in array literal", "\"x\""}},
	},
	{
		"element too large",
		mainLines("a := [1]uint8{256}", "exit(a[0])"),
		[]spanned{{"constant 256 overflows uint8", "256"}},
	},
	{
		"nested literal of the wrong count",
		mainLines("g := [2][2]bool{{true}, {false, true}}", "if g[0][0] {", "}"),
		[]spanned{{"[2]bool literal has 1 element, want 2", "{true}"}},
	},
	{
		"literal without a type",
		mainLines("a := [2]int64{{1}, 2}", "code, _ := int32(a[1])", "exit(code)"),
		[]spanned{{"a literal without a type needs an array literal around it", "{1}"}},
	},
	{
		"constant index too large",
		mainLines("a := [3]int64{1, 2, 3}", "code, _ := int32(a[3])", "exit(code)"),
		[]spanned{{"index 3 is out of range for [3]int64", "3"}},
	},
	{
		"constant index negative",
		mainLines("a := [3]int64{1, 2, 3}", "code, _ := int32(a[-1])", "exit(code)"),
		[]spanned{{"index -1 is out of range for [3]int64", "-1"}},
	},
	{
		"constant variable index",
		mainLines("a := [3]int64{1, 2, 3}", "k := 5", "code, _ := int32(a[k])", "exit(code)"),
		[]spanned{{"index 5 is out of range for [3]int64", "k"}},
	},
	{
		"index of an empty array",
		mainLines("a := [0]int64{}", "code, _ := int32(a[0])", "exit(code)"),
		[]spanned{{"index 0 is out of range for [0]int64", "0"}},
	},
	{
		"written index too large",
		mainLines("mut a [2]int64", "a[2] = 1", "code, _ := int32(a[0])", "exit(code)"),
		[]spanned{{"index 2 is out of range for [2]int64", "2"}},
	},
	{
		"string index",
		mainLines("a := [2]int64{1, 2}", "code, _ := int32(a[\"0\"])", "exit(code)"),
		[]spanned{{"index must be an integer, got string", "\"0\""}},
	},
	{
		"index of a non-array",
		mainLines("n := 5", "exit(n[0])"),
		[]spanned{{"cannot index int64", "n"}},
	},
	{
		"index of a bool",
		mainLines("b := true", "exit(b[0])"),
		[]spanned{{"cannot index bool", "b"}},
	},
	{
		"immutable element",
		mainLines("a := [2]int64{1, 2}", "a[0] = 3", "code, _ := int32(a[0])", "exit(code)"),
		[]spanned{{"cannot assign to element of immutable variable a", "a"}},
	},
	{
		"immutable nested element",
		mainLines(
			"a := [1][1]int64{{1}}",
			"a[0][0] = 3",
			"code, _ := int32(a[0][0])",
			"exit(code)",
		),
		[]spanned{{"cannot assign to element of immutable variable a", "a"}},
	},
	{
		"parameter element",
		"package main\n\nfunc f(v [1]int64) {\n    v[0] = 1\n}\n\nfunc main() {\n    f([1]int64{0})\n}\n",
		[]spanned{{"cannot assign to element of parameter v", "v"}},
	},
	{
		"element of the wrong type",
		mainLines("mut a [2]int64", "a[0] = true", "code, _ := int32(a[0])", "exit(code)"),
		[]spanned{{"cannot use bool as int64 in assignment to an element of a", "true"}},
	},
	{
		"increment a bool element",
		mainLines("mut a [1]bool", "a[0]++", "if a[0] {", "}"),
		[]spanned{{"operator ++ is not defined on bool", "a[0]"}},
	},
	{
		"different lengths compared",
		mainLines("a := [2]int64{1, 2}", "if a == [3]int64{1, 2, 3} {", "}"),
		[]spanned{{"mismatched types [2]int64 and [3]int64 in ==", "a == [3]int64{1, 2, 3}"}},
	},
	{
		"arrays ordered",
		mainLines("a := [2]int64{1, 2}", "if a < a {", "}"),
		[]spanned{{"operator < is not defined on [2]int64", "a < a"}},
	},
	{
		"string arrays ordered",
		mainLines("a := [1]string{\"x\"}", "if a <= a {", "}"),
		[]spanned{{"operator <= is not defined on [1]string", "a <= a"}},
	},
	{
		"array assigned to another length",
		mainLines(
			"mut a [2]int64",
			"a = [3]int64{1, 2, 3}",
			"code, _ := int32(a[0])",
			"exit(code)",
		),
		[]spanned{{"cannot use [3]int64 as [2]int64 in assignment to a", "[3]int64{1, 2, 3}"}},
	},
	{
		"array widened",
		mainLines(
			"small := [1]int8{1}",
			"var big [1]int64 = small",
			"code, _ := int32(big[0])",
			"exit(code)",
		),
		[]spanned{{"cannot use [1]int8 as [1]int64 in declaration of big", "small"}},
	},
	{
		"variable length",
		mainLines("mut n := 3", "mut a [n]int64", "n = 4", "exit(a[0] + n)"),
		[]spanned{{"array length must be a constant integer", "n"}},
	},
	{
		"negative length",
		mainLines("mut a [-1]int64", "exit(len(a))"),
		[]spanned{{"array length must not be negative, got -1", "-1"}},
	},
	{
		"string length",
		mainLines("mut a [\"3\"]int64", "exit(len(a))"),
		[]spanned{{"array length must be a constant integer", "\"3\""}},
	},
	{
		"len of an integer",
		mainLines("n := 3", "exit(len(n))"),
		[]spanned{{"len expects an array, a string, a view or a list, got int64", "n"}},
	},
	{
		"len of two",
		mainLines("exit(len(\"a\", \"b\"))"),
		[]spanned{{"len expects 1 argument(s), got 2", "len(\"a\", \"b\")"}},
	},
	{"len as a statement", mainLines("len(\"a\")"), nil},
	{
		"global literal with a mutable variable",
		"package main\n\nmut k = 1\n\nvar a = [2]int64{k, 2}\n\nfunc main() {\n    exit(a[0])\n}\n",
		[]spanned{{"a must be initialized with a constant expression", "[2]int64{k, 2}"}},
	},
}

// TestRejectsArrayErrors checks each misuse is reported as listed, covering
// the right source.
func TestRejectsArrayErrors(t *testing.T) {
	for _, tt := range arrayErrorCases {
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

// The length of an array is a constant, recorded on the call, and so is
// that of a string literal; a string variable's is not.
func TestLenIsConstantWhereItCanBe(t *testing.T) {
	file, diags := checkTree(
		t,
		mainLines(
			"a := [3]int64{1, 2, 3}",
			"s := \"ab\"",
			"exit(int32(len(a) + len(\"xyz\") + len(s)))",
		),
	)
	if len(diags) > 0 {
		t.Fatalf("got %v", diags)
	}
	exit := file.Decls[0].(*ast.Func).Body[2].(*ast.ExprStmt).X.(*ast.Call)
	conv := exit.Args[0].(*ast.Call)
	sum := conv.Args[0].(*ast.Binary)
	inner := sum.X.(*ast.Binary)
	calls := []*ast.Call{inner.X.(*ast.Call), inner.Y.(*ast.Call), sum.Y.(*ast.Call)}
	want := []string{"3", "3", ""}
	for i, call := range calls {
		got := ""
		if call.Const != nil {
			got = machineString(call.Const)
		}
		if got != want[i] {
			t.Errorf("len %d has constant %q, want %q", i, got, want[i])
		}
	}
}
