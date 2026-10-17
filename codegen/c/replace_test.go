package c_test

import (
	"path/filepath"
	"testing"
)

// TestReplaceExampleIsSanitizerClean builds examples/replace, its C file
// and std's C with clang's warnings as errors, at -O2 and with the address
// and undefined behavior sanitizers on at -O0 and -O1, every finding
// fatal, and checks every check passes: a list taken out of a struct and
// put back, an owned element and a copyable one replaced, a list of lists
// replaced whole and in part, replace written as a statement, and
// c_allocator holding nothing after each group, so every value replace
// moved out or in was freed exactly once.
func TestReplaceExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "replace", "live.c")
	for _, flags := range [][]string{
		{"-O2"},
		{"-O0", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"},
		{"-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"},
	} {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, csrc, flags...), 14)
		})
	}
}
