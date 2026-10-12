package c_test

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// widthProgram is a program whose check function returns whether cond holds
// for the parameters params, given the arguments args, so neither Tack nor
// clang can compute it ahead of time. It exits 1 when cond holds.
func widthProgram(params, args, cond string) string {
	return "package main\n\nfunc check(" + params + ") bool {\n    " +
		strings.ReplaceAll(cond, "\n", "\n    ") + "\n}\n\n" +
		"func main() {\n    if check(" + args + ") {\n        exit(1)\n    }\n    exit(2)\n}\n"
}

// widthCases are identities that must hold at runtime for each integer
// width: arithmetic wraps at the type's own width, widening keeps values,
// and a checked conversion reports exactly whether the value fits.
var widthCases = []struct{ name, src string }{
	{
		"int8 wraps",
		widthProgram("a int8, b int8", "127, -128", "return a + 1 == b && b - 1 == a && -b == b"),
	},
	{
		"int8 multiplies in its own width",
		widthProgram("a int8, b int8", "100, 100", "return a * b == 16"),
	},
	{
		"int8 divides",
		widthProgram(
			"a int8, b int8",
			"-128, -1",
			"return a / b == a && a % b == 0 && a / 2 == -64",
		),
	},
	{
		"uint8 wraps",
		widthProgram("a uint8, b uint8", "0, 255", "return a - 1 == b && b + 1 == a && b * b == 1"),
	},
	{
		"uint8 divides",
		widthProgram("a uint8, b uint8", "200, 3", "return a / b == 66 && a % b == 2"),
	},
	{
		"int16 wraps",
		widthProgram("a int16, b int16", "32767, 2", "return a * b == -2 && a + a == -2"),
	},
	{
		"uint16 multiplies past int",
		widthProgram("a uint16, b uint16", "65535, 65535", "return a * b == 1"),
	},
	{
		"int32 wraps",
		widthProgram(
			"a int32, b int32",
			"-2147483648, -1",
			"return a / b == a && a * b == a && a - 1 == 2147483647",
		),
	},
	{
		"uint32 wraps",
		widthProgram(
			"a uint32, b uint32",
			"4294967295, 2",
			"return a + b == 1 && a * b == 4294967294",
		),
	},
	{
		"uint64 wraps",
		widthProgram(
			"a uint64, b uint64",
			"18446744073709551615, 1",
			"return a + b == 0 && 0 - a == 1",
		),
	},
	{
		"widening keeps values",
		widthProgram(
			"a int8, b uint8, c uint32",
			"-1, 255, 4294967295",
			"var x int64 = a\nvar y int16 = b\nvar z int64 = c\nreturn x == -1 && y == 255 && z == 4294967295 && a < y && b < c",
		),
	},
	{
		"mixed arithmetic in the wider type",
		widthProgram("a int8, b int32", "100, 100", "return a + b == 200 && a * b == 10000"),
	},
	{
		"checked conversions",
		widthProgram(
			"big int64, neg int64, max uint64",
			"300, -1, 18446744073709551615",
			"a, okA := int8(big)\nb, okB := uint64(neg)\nc, okC := int64(max)\nd, okD := uint8(big - 45)\n"+
				"e, okE := int8(neg)\nf, okF := uint64(big)\n"+
				"return !okA && a == 0 && !okB && b == 0 && !okC && c == 0 && okD && d == 255 && okE && e == -1 && okF && f == 300",
		),
	},
	{
		"checked conversion bounds",
		widthProgram(
			"a int64, b int64, c int64, d int64",
			"-128, 127, -129, 128",
			"_, okA := int8(a)\n_, okB := int8(b)\n_, okC := int8(c)\n_, okD := int8(d)\nreturn okA && okB && !okC && !okD",
		),
	},
	{
		"checked conversion from unsigned",
		widthProgram(
			"a uint8, b uint64",
			"200, 9223372036854775807",
			"_, okA := int8(a)\nx, okB := int64(b)\n_, okC := uint32(b)\nreturn !okA && okB && x == 9223372036854775807 && !okC",
		),
	},
	{
		"checked conversion assigned in a loop post",
		widthProgram("start int64", "250",
			"mut n int64 = start\nmut small uint8 = 0\nmut ok := true\nmut count := 0\n"+
				"for ; ok; small, ok = uint8(n) {\n    count++\n    n++\n}\nreturn count == 6 && small == 0 && n == 256"),
	},
	{
		"range over a sized count",
		widthProgram(
			"n uint8, m int8",
			"255, -3",
			"mut total uint64 = 0\nfor i := range n {\n    total += i\n}\nfor range m {\n    return false\n}\nreturn total == 32385",
		),
	},
	{
		"conversions that widen",
		widthProgram(
			"a uint8, b int16",
			"255, -300",
			"return int64(a) == 255 && int32(b) == -300 && uint64(a) == 255",
		),
	},
	{
		"compound assignment in narrow types",
		widthProgram(
			"a int8, b uint16",
			"120, 65000",
			"mut x int8 = a\nx += 10\nmut y uint16 = b\ny *= 2\nmut z int8 = -128\nz /= -1\nmut w uint8 = 0\nw--\n"+
				"return x == -126 && y == 64464 && z == -128 && w == 255",
		),
	},
}

// TestIntegerWidthsAtRuntime builds each program with clang's undefined
// behavior sanitizer set to stop the program, so arithmetic C leaves
// undefined, including overflow after C promotes a narrow type to int, fails
// the test rather than happening to work.
func TestIntegerWidthsAtRuntime(t *testing.T) {
	for _, tt := range widthCases {
		t.Run(tt.name, func(t *testing.T) {
			bin := buildWith(t, tt.src, "-O2", "-fsanitize=undefined", "-fno-sanitize-recover=all")
			out, err := exec.Command(bin).CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Errorf("got %v, want exit status 1; output:\n%s", err, out)
			}
		})
	}
}
