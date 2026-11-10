package c_test

import (
	"path/filepath"
	"testing"
)

// questionOwnedWant is how many checks examples/question-owned prints.
const questionOwnedWant = 36

// TestQuestionOwnedExampleIsSanitizerClean builds examples/question-owned
// under each of sanitizerFlags and checks every check it prints passed:
// every owned value an expression built before a ? that returns, a
// call's own argument, append's and put's values, a struct's field, a
// union's payload, a return's value, a variable moved into an argument,
// one held deep in an expression C cannot nest, and the list a failing
// call gave beside its error, is freed exactly once, and so is every
// owned local of every block the ? leaves, from a loop's condition and
// post statement, the right of &&, a range over a list and a switch's
// clause, and a ? as a statement in parentheses and as a loop's init
// and post, which c_allocator's count of live buffers and the sanitizers
// check.
func TestQuestionOwnedExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "question-owned", "live.c")
	for _, flags := range sanitizerFlags {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, csrc, flags...), questionOwnedWant)
		})
	}
}
