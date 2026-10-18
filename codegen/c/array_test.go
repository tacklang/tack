package c_test

import (
	"strings"
	"testing"
)

// arrayCases are array programs with what each must print and the status it
// must exit with.
var arrayCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"assignment copies",
		program(
			"mut a := [3]int64{1, 2, 3}",
			"mut b := a",
			"b[0] = 9",
			"a[2] = 7",
			"code, fits := int32(a[0] * 100 + b[0] * 10 + b[2])",
			"if fits { exit(code) }",
		),
		"",
		193,
	},
	{
		"passing copies",
		"package main\n\nfunc clobber(v [2]int64) int64 {\n    mut w := v\n    w[0] = 50\n    return w[0] + v[0]\n}\n\n" +
			"func main() {\n    a := [2]int64{1, 2}\n    code, fits := int32(clobber(a) + a[0])\n    if fits { exit(code) }\n}\n",
		"",
		52,
	},
	{
		"returned",
		"package main\n\nfunc pair(x int64) [2]int64 {\n    return [2]int64{x, x * 2}\n}\n\nfunc main() {\n    code, fits := int32(pair(4)[1] + pair(1)[0])\n    if fits { exit(code) }\n}\n",
		"",
		9,
	},
	{
		"zero value",
		program(
			"mut a [3]int64",
			"mut b [2]bool",
			"mut s [2]string",
			"print(s[1])",
			"if a[2] == 0 && !b[1] && len(s[0]) == 0 {",
			"    exit(1)",
			"}",
		),
		"",
		1,
	},
	{
		"nested",
		program(
			"mut g [3][2]int64",
			"g[2][1] = 5",
			"g[0][0] = g[2][1] + 1",
			"row := g[2]",
			"code, fits := int32(g[0][0] * 10 + row[1])",
			"if fits { exit(code) }",
		),
		"",
		65,
	},
	{
		"equality",
		program(
			"a := [3]int64{1, 2, 3}",
			"b := [...]int64{1, 2, 3}",
			"c := [3]int64{1, 2, 4}",
			"g := [2][2]bool{{true, false}, {false, true}}",
			"mut h := g",
			"h[1][0] = true",
			"if a == b && a != c && !(a == c) && g == g && g != h && [0]int64{} == [0]int64{} {",
			"    exit(1)",
			"}",
		),
		"",
		1,
	},
	{
		"strings",
		program(
			"names := [3]string{\"a\", \"bc\", \"def\"}",
			"print(names[2])",
			"print(names[0])",
			"code, fits := int32(len(names[1]) + len(names))",
			"if fits { exit(code) }",
		),
		"defa",
		5,
	},
	{
		"narrow elements wrap",
		program("mut a := [2]uint8{255, 0}", "a[0]++", "a[1]--", "exit(a[1] - a[0])"),
		"", 255,
	},
	{
		"globals",
		"package main\n\nvar primes = [4]int64{2, 3, 5, 7}\n\nmut counts [3]int64\n\nvar grid = [2][2]bool{{false, true}, {true, false}}\n\n" +
			"func bump() {\n    counts[1] += 2\n}\n\nfunc main() {\n    bump()\n    bump()\n    if grid[0][1] && !grid[1][1] {\n        code, fits := int32(counts[1] + primes[3])\n        if fits { exit(code) }\n    }\n}\n",
		"",
		11,
	},
	{
		"len",
		"package main\n\nfunc noisy() [4]int64 {\n    print(\"n\")\n    return [4]int64{1, 2, 3, 4}\n}\n\n" +
			"func main() {\n    s := \"tack\"\n    a := [3]bool{true, true, true}\n    code, fits := int32(len(s) * 100 + len(a) * 10 + len(noisy()))\n    if fits { exit(code) }\n}\n",
		"n",
		434 % 256,
	},
	{
		"constant indexes from constants",
		program(
			"a := [5]int64{1, 2, 3, 4, 5}",
			"last := len(a) - 1",
			"code, fits := int32(a[last] * 10 + a[len(a) / 2])",
			"if fits { exit(code) }",
		),
		"",
		53,
	},
	{
		"element assigned several values",
		"package main\n\nfunc p() (int64, int64) {\n    return 3, 4\n}\n\n" +
			"func main() {\n    mut a [2]int64\n    a[1], a[0] = p()\n    code, fits := int32(a[0] * 10 + a[1])\n    if fits { exit(code) }\n}\n",
		"", 43,
	},
}

// TestArraysAtRuntime builds each program with clang's undefined behavior
// sanitizer set to stop it, and checks what it prints and its exit status.
func TestArraysAtRuntime(t *testing.T) {
	for _, tt := range arrayCases {
		t.Run(tt.name, func(t *testing.T) {
			out, status := runSanitized(t, tt.src)
			if out != tt.stdout || status != tt.status {
				t.Errorf("got stdout %q, status %d; want %q, %d", out, status, tt.stdout, tt.status)
			}
		})
	}
}

// An array lowers to a struct holding a C array, so it is a value C can
// assign, pass and return, and each array type gets one struct.
func TestArrayLowering(t *testing.T) {
	got := generate(
		t,
		program(
			"a := [3]int64{1, 2, 3}",
			"b := [3]int64{4, 5, 6}",
			"if a == b {",
			"    code, fits := int32(a[0])",
			"    if fits { exit(code) }",
			"}",
		),
	)
	for _, want := range []string{"    int64_t e[3];\n} tack_array_", "(tack_array_", "){{1, 2, 3}}", "tack_eq_", ".e[0]"} {
		if !strings.Contains(got, want) {
			t.Errorf("generated C is missing %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "int64_t e[3];"); n != 1 {
		t.Errorf("[3]int64 got %d structs, want 1:\n%s", n, got)
	}
}
