package c_test

import (
	"testing"
)

// checkedDivisionCases are programs using q, ok := a / b, with what each
// must print and the status it must exit with.
var checkedDivisionCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"zero divisor gives 0 and false",
		program(
			"mut z int64 = 0",
			"q, ok := 7 / z",
			"r, okR := 7 % z",
			"if !ok && !okR && q == 0 && r == 0 {",
			"    exit(1)",
			"}",
		),
		"",
		1,
	},
	{
		"non-zero divisor gives the result and true",
		program(
			"mut d int64 = -2",
			"q, ok := -7 / d",
			"r, okR := -7 % d",
			"if ok && okR && q == 3 && r == -1 {",
			"    exit(1)",
			"}",
		),
		"",
		1,
	},
	{
		"smallest value by -1 wraps",
		program(
			"mut m int8 = -128",
			"mut d int8 = -1",
			"q, ok := m / d",
			"r, okR := m % d",
			"if ok && okR && q == -128 && r == 0 {",
			"    exit(1)",
			"}",
		),
		"",
		1,
	},
	{
		"both operands evaluated, dividend first, even when the divisor is zero",
		"package main\n\nfunc say(s string, n int64) int64 {\n    print(s)\n    return n\n}\n\n" +
			"func main() {\n    q, ok := say(\"a\", 6) / say(\"b\", 0)\n    if !ok && q == 0 {\n        exit(1)\n    }\n}\n",
		"ab", 1,
	},
	{
		"narrow operands",
		program(
			"mut a uint8 = 200",
			"mut b uint16 = 7",
			"q, ok := a / b",
			"if ok && q == 28 {",
			"    exit(1)",
			"}",
		),
		"",
		1,
	},
	{
		"as a loop post",
		program(
			"mut n int64 = 100",
			"mut d int64 = 3",
			"mut steps := 0",
			"mut ok := true",
			"for ; ok && n > 0; n, ok = n / d {",
			"    steps +%= 1",
			"    d -%= 1",
			"}",
			"code, fits := int32(steps)",
			"if fits { exit(code) }",
		),
		"",
		3,
	},
}

// TestCheckedDivisionAtRuntime builds each program with clang's undefined
// behavior sanitizer set to stop the program, so a division by zero that got
// through, or the smallest value divided by -1, fails the test.
func TestCheckedDivisionAtRuntime(t *testing.T) {
	for _, tt := range checkedDivisionCases {
		t.Run(tt.name, func(t *testing.T) {
			out, status := runSanitized(t, tt.src)
			if out != tt.stdout || status != tt.status {
				t.Errorf("got stdout %q, status %d; want %q, %d", out, status, tt.stdout, tt.status)
			}
		})
	}
}
