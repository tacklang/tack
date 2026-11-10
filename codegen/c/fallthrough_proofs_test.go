package c_test

import (
	"path/filepath"
	"testing"
)

// TestFallthroughProofsExampleRuns builds examples/fallthrough-proofs
// each way fallthroughBuilds says and checks it exits 42, printing
// nothing: a division by a value both ways into a case keep from zero, a
// stored value's and an early return's among them, an unreachable after
// cases that cover every value and leave through a fallthrough, a switch
// with a fallthrough in deferred code, a chain of five fallthroughs, one
// in a loop proving an index, and a ? in the case one entered, each
// giving the value main checks.
func TestFallthroughProofsExampleRuns(t *testing.T) {
	src := readFile(t, filepath.Join("..", "..", "examples", "fallthrough-proofs", "main.tck"))
	for _, flags := range fallthroughBuilds {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			out, code := runStatus(t, buildWith(t, src, flags...))
			if out != "" || code != 42 {
				t.Errorf("printed %q and exited %d, want nothing and 42", out, code)
			}
		})
	}
}
