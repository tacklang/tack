package c_test

import (
	"errors"
	"os/exec"
	"testing"
)

// wrapProgram checks one wrapping identity at runtime. The operands arrive
// as parameters, so neither Tack nor clang can compute the result ahead of
// time, and the program exits 1 when the identity holds. Both parameters are
// read, since the strict build rejects an unused one.
func wrapProgram(cond string) string {
	return "package main\n\nfunc check(max int, min int) bool {\n    return max != min && " + cond + "\n}\n\n" +
		"func main() {\n    if check(9223372036854775807, -9223372036854775808) {\n        exit(1)\n    }\n    exit(2)\n}\n"
}

// wrapCases are identities that hold when int arithmetic wraps on overflow,
// as Tack defines it.
var wrapCases = []struct{ name, src string }{
	{"max + 1", wrapProgram("max + 1 == min")},
	{"min - 1", wrapProgram("min - 1 == max")},
	{"max * 2", wrapProgram("max * 2 == -2")},
	{"min * -1", wrapProgram("min * -1 == min")},
	{"-min", wrapProgram("-min == min")},
	{"min / -1", wrapProgram("min / -1 == min")},
	{"min % -1", wrapProgram("min % -1 == 0")},
	{"max + max", wrapProgram("max + max == -2")},
	{"min + min", wrapProgram("min + min == 0")},
	{
		"compound and increment",
		"package main\n\nfunc check(max int, min int) bool {\n    mut a := max\n    a += 1\n    mut b := min\n    b--\n" +
			"    mut c := max\n    c++\n    mut d := min\n    d *= -1\n    mut e := min\n    e /= -1\n    mut f := min\n    f %= -1\n" +
			"    return a == min && b == max && c == min && d == min && e == min && f == 0\n}\n\n" +
			"func main() {\n    if check(9223372036854775807, -9223372036854775808) {\n        exit(1)\n    }\n    exit(2)\n}\n",
	},
	{
		"ordinary division still truncates",
		wrapProgram(
			"-7 / 2 == -3 && -7 % 2 == -1 && 7 / -2 == -3 && max / max == 1 && min / 2 == -4611686018427387904",
		),
	},
}

// TestArithmeticWrapsAtRuntime builds each program with clang's undefined
// behavior sanitizer set to stop the program, so arithmetic C leaves
// undefined, such as signed overflow, fails the test rather than happening
// to give the right answer.
func TestArithmeticWrapsAtRuntime(t *testing.T) {
	for _, tt := range wrapCases {
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
