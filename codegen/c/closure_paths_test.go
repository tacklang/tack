package c_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestClosurePathsExampleIsSanitizerClean builds examples/closure-paths
// with std's C at -O2, and with the address and undefined behavior
// sanitizers at -O0 and -O1, every finding fatal, and checks it writes
// exactly its output, every check ok: a defer and a ? in a closure, a
// closure of several results, closures with mut and scope parameters,
// plain function values of one and of several results passed to scope
// parameters through their adapters, a function with a scope parameter
// called through a value, captures of a payload bound in place and of a
// range element, recursion through a scope parameter, and a closure in a
// generic function passed to an interface's method.
func TestClosurePathsExampleIsSanitizerClean(t *testing.T) {
	src := readFile(t, filepath.Join("..", "..", "examples", "closure-paths", "main.tck"))
	want := readFile(t, filepath.Join("..", "..", "examples", "closure-paths", "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/closure-paths/output expects a check to fail")
	}
	for _, flags := range [][]string{
		{"-O2"},
		{"-O0", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"},
		{"-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"},
	} {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			out, code := runStatus(t, buildWithStd(t, src, flags...))
			if out != want || code != 0 {
				t.Errorf("wrote\n%s\nand exited %d, want\n%s\nand 0", out, code, want)
			}
		})
	}
}
