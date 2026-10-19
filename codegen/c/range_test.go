package c_test

import (
	"strings"
	"testing"
)

// rangeRuntimeCases are programs with range loops, and what each must print
// and exit with.
var rangeRuntimeCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"counts from zero",
		"package main\n\nfunc main() {\n    mut sum = 0\n    for i := range 10 {\n        sum += i\n    }\n    code, fits := int32(sum)\n    if fits { exit(code) }\n}\n",
		"",
		45,
	},
	{
		"no variable",
		"package main\n\nfunc main() {\n    for range 3 {\n        print(\"x\")\n    }\n}\n",
		"xxx", 0,
	},
	{
		"zero and negative counts",
		"package main\n\nfunc main() {\n    for range 0 {\n        exit(1)\n    }\n    for range -5 {\n        exit(2)\n    }\n    exit(3)\n}\n",
		"",
		3,
	},
	{
		"count evaluated once",
		"package main\n\nfunc three() int64 {\n    print(\"count\")\n    return 3\n}\n\nfunc main() {\n    mut n = 0\n    for range three() {\n        n++\n    }\n    code, fits := int32(n)\n    if fits { exit(code) }\n}\n",
		"count",
		3,
	},
	{
		"changing the count's variable changes nothing",
		"package main\n\nfunc main() {\n    mut n = 2\n    mut iterations = 0\n    for range n {\n        n += 10\n        iterations++\n    }\n    code, fits := int32(iterations)\n    if fits { exit(code) }\n}\n",
		"",
		2,
	},
	{
		"break and continue",
		"package main\n\nfunc main() {\n    mut sum = 0\n    for i := range 100 {\n        if i % 2 == 1 {\n            continue\n        }\n        if i > 8 {\n            break\n        }\n        sum += i\n    }\n    code, fits := int32(sum)\n    if fits { exit(code) }\n}\n",
		"",
		20,
	},
	{
		"nested",
		"package main\n\nfunc main() {\n    mut n = 0\n    for i := range 4 {\n        for j := range i {\n            n += j + 1\n        }\n    }\n    code, fits := int32(n)\n    if fits { exit(code) }\n}\n",
		"",
		10,
	},
	{
		"siblings reuse the name",
		"package main\n\nfunc main() {\n    mut n = 0\n    for i := range 3 {\n        n += i\n    }\n    for i := range 3 {\n        n += i * 10\n    }\n    code, fits := int32(n)\n    if fits { exit(code) }\n}\n",
		"",
		33,
	},
	{
		"returns from inside",
		"package main\n\nfunc find(want int64) int64 {\n    for i := range 50 {\n        if i * i == want {\n            return i\n        }\n    }\n    return -1\n}\n\nfunc main() {\n    code, fits := int32(find(49) + find(50) + 1)\n    if fits { exit(code) }\n}\n",
		"",
		7,
	},
}

// Each of rangeRuntimeCases, built with clang's warnings as errors (see
// run), prints and exits as the case says.
func TestRangeLoopsAtRuntime(t *testing.T) {
	for _, tt := range rangeRuntimeCases {
		t.Run(tt.name, func(t *testing.T) {
			stdout, status := run(t, tt.src)
			if stdout != tt.stdout || status != tt.status {
				t.Errorf(
					"got stdout %q, status %d; want %q, %d",
					stdout,
					status,
					tt.stdout,
					tt.status,
				)
			}
		})
	}
}

// collectionRangeCases are programs ranging over arrays and strings, and
// what each must print and exit with.
var collectionRangeCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"sums an array",
		program(
			"mut sum := 0",
			"for _, x := range [4]int64{1, 2, 3, 4} {",
			"    sum += x",
			"}",
			"code, fits := int32(sum)",
			"if fits { exit(code) }",
		),
		"",
		10,
	},
	{
		"index and element agree",
		program(
			"a := [3]int64{7, 8, 9}",
			"for i, x := range a {",
			"    if a[i] != x {",
			"        exit(1)",
			"    }",
			"}",
			"exit(2)",
		),
		"",
		2,
	},
	{
		"walks a copy of the array",
		program(
			"mut a := [3]int64{1, 2, 3}",
			"mut got := 0",
			"for i, x := range a {",
			"    a[2] = 9",
			"    a = [3]int64{4, 5, 6}",
			"    a[i] = 0",
			"    got = got * 10 + x",
			"}",
			"code, fits := int32(got + a[0] + a[1] + a[2] - 100)",
			"if fits { exit(code) }",
		),
		"",
		32,
	},
	{
		"index alone over an array the body writes",
		program(
			"mut a := [3]int64{1, 2, 3}",
			"for i := range a {",
			"    a[i] = a[i] * 2",
			"}",
			"code, fits := int32(a[0] + a[1] + a[2])",
			"if fits { exit(code) }",
		),
		"",
		12,
	},
	{
		"ranged call runs once",
		"package main\n\nfunc arr() [3]int64 {\n    print(\"a\")\n    return [3]int64{1, 2, 3}\n}\n\nfunc str() string {\n    print(\"s\")\n    return \"xyz\"\n}\n\n" +
			"func main() {\n    mut n := 0\n    for range arr() {\n        n++\n    }\n    for range str() {\n        n++\n    }\n    for i := range arr() {\n        n += i\n    }\n    code, fits := int32(n)\n    if fits { exit(code) }\n}\n",
		"asa",
		9,
	},
	{
		"bytes of a string",
		program(
			"mut sum := 0",
			"for i, b := range \"abc\" {",
			"    sum += i * 1000 + int64(b)",
			"}",
			"code, fits := int32(sum - 3000 - 97 - 98)",
			"if fits { exit(code) }",
		),
		"",
		99,
	},
	{
		"walks the string as it was",
		program(
			"mut s := \"hello\"",
			"mut n := 0",
			"for _, b := range s {",
			"    s = \"\"",
			"    if b == 'l' {",
			"        n++",
			"    }",
			"}",
			"code, fits := int32(n * 10 + len(s))",
			"if fits { exit(code) }",
		),
		"",
		20,
	},
	{
		"bytes above 127 stay unsigned",
		program(
			"mut n := 0",
			"for _, b := range \"\u00e9\" {",
			"    n += int64(b)",
			"}",
			"code, fits := int32(n - 300)",
			"if fits { exit(code) }",
		),
		"",
		64,
	},
	{
		"empty array and string",
		program(
			"for range [0]int64{} {",
			"    exit(1)",
			"}",
			"for _, b := range \"\" {",
			"    exit(b)",
			"}",
			"exit(3)",
		),
		"",
		3,
	},
	{
		"array of arrays",
		program(
			"g := [2][3]int64{{1, 2, 3}, {4, 5, 6}}",
			"mut n := 0",
			"for r, row := range g {",
			"    for c, v := range row {",
			"        if g[r][c] != v {",
			"            exit(1)",
			"        }",
			"        n += v",
			"    }",
			"}",
			"code, fits := int32(n)",
			"if fits { exit(code) }",
		),
		"",
		21,
	},
	{
		"array of strings",
		program(
			"mut n := 0",
			"for _, s := range [2]string{\"ab\", \"cde\"} {",
			"    for range s {",
			"        n++",
			"    }",
			"    print(s)",
			"}",
			"code, fits := int32(n)",
			"if fits { exit(code) }",
		),
		"abcde",
		5,
	},
	{
		"break, continue and return",
		"package main\n\nfunc find(s string, c uint8) int64 {\n    for i, b := range s {\n        if b == c {\n            return i\n        }\n    }\n    return -1\n}\n\n" +
			"func main() {\n    mut n := 0\n    for i := range \"abcdef\" {\n        if i == 1 {\n            continue\n        }\n        if i == 4 {\n            break\n        }\n        n += i\n    }\n    code, fits := int32(n * 10 + find(\"tack\", 'c') + find(\"tack\", 'z') + 1)\n    if fits { exit(code) }\n}\n",
		"",
		52,
	},
	{
		"ranged parameter",
		"package main\n\nfunc total(a [3]uint8) int64 {\n    mut n := 0\n    for i := range a {\n        n += int64(a[i])\n    }\n    return n\n}\n\nfunc main() {\n    code, fits := int32(total([3]uint8{255, 255, 1}) - 500)\n    if fits { exit(code) }\n}\n",
		"",
		11,
	},
}

// TestCollectionRangesAtRuntime builds each of collectionRangeCases with
// clang's warnings as errors and its undefined behavior sanitizer set to
// stop the program, and checks what it prints and its exit status.
func TestCollectionRangesAtRuntime(t *testing.T) {
	for _, tt := range collectionRangeCases {
		t.Run(tt.name, func(t *testing.T) {
			stdout, status := runSanitized(t, tt.src)
			if stdout != tt.stdout || status != tt.status {
				t.Errorf(
					"got stdout %q, status %d; want %q, %d",
					stdout,
					status,
					tt.stdout,
					tt.status,
				)
			}
		})
	}
}

// A range over an array or a string copies it into a const temporary that
// every iteration reads, and an array's bound is its constant length; one
// with no element variable only evaluates the array.
func TestCollectionRangeLowering(t *testing.T) {
	got := generate(t, program(
		"mut a := [3]int64{1, 2, 3}",
		"for i, x := range a {",
		"    a[i] = x",
		"}",
		"for _, b := range \"ab\" {",
		"    exit(b)",
		"}",
		"for range a {",
		"}",
	))
	for _, want := range []string{
		" tack_r0 = v_a;",
		"tack_k1 < 3;",
		"const int64_t v_i = tack_k1;",
		"const int64_t v_x = tack_r0.e[tack_k1];",
		" tack_r2 = (tack_string){",
		"tack_k3 < tack_r2.len;",
		"const uint8_t v_b = tack_r2.ptr[tack_k3];",
		"(void)v_a;",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated C is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "v__") {
		t.Errorf("_ got a C variable:\n%s", got)
	}
}
