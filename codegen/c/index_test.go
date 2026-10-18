package c_test

import (
	"testing"
)

// checkedIndexCases are programs indexing with variables, proven in range or
// bound with ok, with what each must print and the status it must exit with.
var checkedIndexCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"out of range gives zero and false",
		program(
			"a := [3]int64{7, 8, 9}",
			"mut i := 3",
			"mut j := -1",
			"v, ok := a[i]",
			"w, fine := a[j]",
			"if !ok && !fine && v == 0 && w == 0 {",
			"    exit(1)",
			"}",
		),
		"",
		1,
	},
	{
		"in range gives the element and true",
		program(
			"a := [3]int64{7, 8, 9}",
			"mut i := 2",
			"v, ok := a[i]",
			"if ok {",
			"    code, fits := int32(v)",
			"    if fits { exit(code) }",
			"}",
		),
		"",
		9,
	},
	{
		"array before index, each once, even out of range",
		"package main\n\nfunc arr() [2]int64 {\n    print(\"a\")\n    return [2]int64{1, 2}\n}\n\n" +
			"func idx(i int64) int64 {\n    print(\"i\")\n    return i\n}\n\n" +
			"func main() {\n    v, ok := arr()[idx(5)]\n    w, fine := arr()[idx(1)]\n    if !ok && v == 0 && fine {\n        code, fits := int32(w)\n        if fits { exit(code) }\n    }\n}\n",
		"aiai",
		2,
	},
	{
		"zero of a string element",
		program(
			"names := [2]string{\"x\", \"y\"}",
			"mut i := 4",
			"s, ok := names[i]",
			"print(s)",
			"if !ok && len(s) == 0 {",
			"    exit(1)",
			"}",
		),
		"",
		1,
	},
	{
		"zero of an array element",
		program(
			"g := [2][2]int64{{1, 2}, {3, 4}}",
			"mut i := 9",
			"row, ok := g[i]",
			"if !ok && row == [2]int64{0, 0} {",
			"    exit(1)",
			"}",
		),
		"",
		1,
	},
	{
		"narrow indexes",
		program(
			"a := [3]int64{7, 8, 9}",
			"mut u uint8 = 250",
			"mut s int8 = -2",
			"_, okU := a[u]",
			"_, okS := a[s]",
			"mut t uint8 = 1",
			"v, okT := a[t]",
			"if !okU && !okS && okT {",
			"    code, fits := int32(v)",
			"    if fits { exit(code) }",
			"}",
		),
		"",
		8,
	},
	{
		"proven indexes read and write",
		program(
			"mut a [5]int64",
			"for i := range len(a) {",
			"    a[i] = i * i",
			"}",
			"mut total := 0",
			"for i := range 5 {",
			"    if i >= 1 && i < 4 {",
			"        total += a[i]",
			"    }",
			"}",
			"code, fits := int32(total)",
			"if fits { exit(code) }",
		),
		"",
		14,
	},
	{
		"unsigned index within the type",
		program(
			"mut seen [256]bool",
			"mut count := 0",
			"for k := range 300 {",
			"    b, bFits := uint8(k)",
			"    if !bFits {",
			"        continue",
			"    }",
			"    if !seen[b] {",
			"        seen[b] = true",
			"        count++",
			"    }",
			"}",
			"code, fits := int32(count - 200)",
			"if fits { exit(code) }",
		),
		"",
		56,
	},
	{
		"checked index as a loop post",
		program(
			"a := [4]int64{1, 2, 3, 4}",
			"mut i := 0",
			"mut v := 0",
			"mut ok := true",
			"mut total := 0",
			"for ; ok; v, ok = a[i] {",
			"    total += v",
			"    i++",
			"}",
			"code, fits := int32(total)",
			"if fits { exit(code) }",
		),
		// The first pass adds the 0 v starts at, so a[0] is never added.
		"",
		9,
	},
}

// TestCheckedIndexesAtRuntime builds each program with clang's undefined
// behavior sanitizer set to stop it, so an index that got out of range in C
// fails the test.
func TestCheckedIndexesAtRuntime(t *testing.T) {
	for _, tt := range checkedIndexCases {
		t.Run(tt.name, func(t *testing.T) {
			out, status := runSanitized(t, tt.src)
			if out != tt.stdout || status != tt.status {
				t.Errorf("got stdout %q, status %d; want %q, %d", out, status, tt.stdout, tt.status)
			}
		})
	}
}
