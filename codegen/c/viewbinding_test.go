package c_test

import (
	"path/filepath"
	"testing"
)

// TestViewBindingsExampleIsSanitizerClean builds examples/view-bindings
// with the address and undefined behavior sanitizers on, every finding
// fatal, at -O0 and -O2, and checks every check passes: views returned
// through a union pattern's binding, a range loop's element in place, both
// nested, an array payload reached through a list's element, and a call
// given a binding point into the caller's data, which is still alive
// where the views are read, and a call returning a view written as a
// statement drops it.
func TestViewBindingsExampleIsSanitizerClean(t *testing.T) {
	src := readFile(t, filepath.Join("..", "..", "examples", "view-bindings", "main.tck"))
	for _, level := range []string{"-O0", "-O2"} {
		t.Run(level, func(t *testing.T) {
			bin := buildWithStd(
				t,
				src,
				level,
				"-fsanitize=address,undefined", useAfterReturn,
				"-fno-sanitize-recover=all",
			)
			runChecks(t, bin, 6)
		})
	}
}
