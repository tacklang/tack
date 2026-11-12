package c_test

import (
	"os/exec"
	"strings"
	"testing"
)

// methodExprsWant is how many checks examples/method-exprs prints.
const methodExprsWant = 41

// TestMethodExprsExampleIsSanitizerClean builds examples/method-exprs, its
// package shapes, its C file and std's C with clang's warnings as errors,
// at -O2 and with the address and undefined behavior sanitizers at -O0
// and -O1, every finding fatal, and checks every check it prints passed:
// methods named through their type, Pair.first, and through their
// package and type, shapes.Box.Wide, used as function values whose first
// parameter is the receiver, a mut one included, assigned, passed,
// returned, held by a struct, an array and a list, given to generic
// functions, of several results and of errors, of a struct holding a
// list and of a union, with c_allocator holding nothing after the checks
// that allocate.
func TestMethodExprsExampleIsSanitizerClean(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	prog := compileExampleDir(t, "method-exprs")
	for _, flags := range [][]string{
		{"-O2"},
		{"-O0", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
		{"-O1", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
	} {
		t.Run(strings.Join(flags, "/"), func(t *testing.T) {
			runChecks(t, buildProgram(t, prog, "", flags...), methodExprsWant)
		})
	}
}
