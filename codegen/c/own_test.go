package c_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestOwnParamsExampleIsSanitizerClean builds examples/own-params, its C
// file and std's C at -O2, and with the address and undefined behavior
// sanitizers at -O0 and -O1, every finding fatal, and checks every check
// it prints passed: values moved into functions by own parameters are
// returned, stored in lists and maps, moved on, rebound as mut, borrowed,
// read by a defer and freed at every exit, through generic functions, an
// interface's method and function values, and through the case values
// of switches whose default is written first or last, with every
// allocation refused in turn, and c_allocator holds nothing after each
// group.
func TestOwnParamsExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "own-params", "live.c")
	for _, flags := range [][]string{
		{"-O2"},
		{"-O0", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
		{"-O1", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
	} {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, csrc, flags...), 31)
		})
	}
}

// TestOwnExitExampleIsSanitizerClean builds examples/own-exit with the
// sanitizers on and checks it writes exactly its output and exits with
// status 3: exit, called in a function owning two lists given to it by
// own parameters, runs no defer, and every buffer is freed before the
// process ends, which live.c's destructor checks.
func TestOwnExitExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "own-exit", "live.c")
	want := readFile(t, filepath.Join("..", "..", "examples", "own-exit", "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/own-exit/output expects a check to fail")
	}
	atO0Too(t, sanitizers, func(t *testing.T, flags []string) {
		out, code := runStatus(t, buildWithStdAndC(t, src, csrc, flags...))
		if out != want || code != 3 {
			t.Errorf("wrote\n%s\nand exited %d, want\n%s\nand 3", out, code, want)
		}
	})
}
