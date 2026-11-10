package c_test

import (
	"path/filepath"
	"testing"
)

// fallthroughOwnedWant is how many checks examples/fallthrough-owned
// prints.
const fallthroughOwnedWant = 12

// TestFallthroughOwnedExampleIsSanitizerClean builds
// examples/fallthrough-owned under each of sanitizerFlags and checks
// every check it prints passed: a fallthrough frees its case's lists,
// after that case's defer, before the next case's body runs; a ? that
// fails before a fallthrough, or in the case one entered, frees every
// list the blocks it leaves hold; and a list moved and given anew before
// a fallthrough is freed once, which c_allocator's count of live buffers
// and the sanitizers check.
func TestFallthroughOwnedExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "fallthrough-owned", "live.c")
	for _, flags := range sanitizerFlags {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, csrc, flags...), fallthroughOwnedWant)
		})
	}
}
