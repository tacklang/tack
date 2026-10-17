package c_test

import (
	"path/filepath"
	"testing"
)

// TestProofRulesExampleIsSanitizerClean builds examples/proof-rules, its
// C file and std's C with clang's warnings as errors, alone and with the
// address and undefined behavior sanitizers on, every finding fatal, and
// checks every check passes: a pop, a remove and a division guarded by
// a check that their own write follows, proofs that a write later in a
// loop's pass leaves standing, the lengths a successful append proves,
// and pops and removes written as statements, which drop an element only
// when there is one and free it when it is owned, which the example's
// count of live buffers checks.
func TestProofRulesExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "proof-rules", "live.c")
	for _, flags := range [][]string{
		{"-O2"},
		{"-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"},
	} {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, csrc, flags...), 15)
		})
	}
}
