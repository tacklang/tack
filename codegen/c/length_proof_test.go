package c_test

import (
	"path/filepath"
	"testing"
)

// lengthProofsWant is how many checks examples/length-proofs prints.
const lengthProofsWant = 12

// TestLengthProofsExampleIsSanitizerClean builds examples/length-proofs
// and std's C with clang's warnings as errors, alone and with the address
// and undefined behavior sanitizers on, every finding fatal, and checks
// every check passes: a length alone, proven as an index into a string, a
// view, an array and a list, as a converted value, as a slice's bound and
// as a divisor, run at the edges of what its guard allows and past them,
// never reads out of range, converts a value that does not fit, or
// divides by zero.
func TestLengthProofsExampleIsSanitizerClean(t *testing.T) {
	src := readFile(t, filepath.Join("..", "..", "examples", "length-proofs", "main.tck"))
	for _, flags := range [][]string{
		{"-O2"},
		{"-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"},
	} {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStd(t, src, flags...), lengthProofsWant)
		})
	}
}
