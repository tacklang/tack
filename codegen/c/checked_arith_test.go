package c_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runArithmeticExample builds examples/<name> with clang's warnings as
// errors and the address and undefined behavior sanitizers on, every
// finding fatal, and checks it writes exactly its output, every check ok.
// The undefined behavior sanitizer is what makes this a test of the
// lowering: a checked or wrapping operation computed with C's signed
// arithmetic, which is undefined on overflow, stops the program.
func runArithmeticExample(t *testing.T, name string) {
	t.Helper()
	dir := filepath.Join("..", "..", "examples", name)
	src := readFile(t, filepath.Join(dir, "main.tck"))
	want := readFile(t, filepath.Join(dir, "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatalf("examples/%s/output expects a check to fail", name)
	}
	bin := buildWithStd(t, src, "-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all")
	var stderr strings.Builder
	cmd := exec.Command(bin)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s\nstderr:\n%s", name, err, out, stderr.String())
	}
	if string(out) != want {
		t.Errorf("%s wrote\n%q\nwant\n%q", name, out, want)
	}
}

// TestCheckedArithmeticExampleIsSanitizerClean runs examples/checked-arithmetic:
// v, ok := a + b, a - b, a * b and -a at the bounds of int8, uint8,
// int32, int64 and uint64, giving 0 and false on overflow, every int8 and
// uint8 pair against int64, and plain arithmetic, steps and loops the
// compiler proves cannot overflow.
func TestCheckedArithmeticExampleIsSanitizerClean(t *testing.T) {
	runArithmeticExample(t, "checked-arithmetic")
}

// TestWrappingArithmeticExampleIsSanitizerClean runs
// examples/wrapping-arithmetic: +% -% *%, their compound forms and -%x at
// the bounds of every integer width, and every int8 and uint8 pair
// against the low bits of the exact result.
func TestWrappingArithmeticExampleIsSanitizerClean(t *testing.T) {
	runArithmeticExample(t, "wrapping-arithmetic")
}

// TestOverflowFixesExampleIsSanitizerClean runs examples/overflow-fixes:
// each fix an overflow error suggests, applied, compiles and computes what
// it says, a wrapping product of known globals included.
func TestOverflowFixesExampleIsSanitizerClean(t *testing.T) {
	runArithmeticExample(t, "overflow-fixes")
}
