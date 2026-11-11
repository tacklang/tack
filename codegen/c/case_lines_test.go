package c_test

import (
	"path/filepath"
	"testing"
)

// TestCaseLinesExampleRuns builds examples/case-lines each way
// fallthroughBuilds says and checks it exits 42, printing nothing: every
// clause whose header ends its line, followed by a line holding its last
// statement, a ; and the next case or default, runs as its own clause, in
// tagged, tagless, union and type switches, through fallthroughs, and
// with a closure literal or a ? in the clause.
func TestCaseLinesExampleRuns(t *testing.T) {
	src := readFile(t, filepath.Join("..", "..", "examples", "case-lines", "main.tck"))
	for _, flags := range fallthroughBuilds {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			out, code := runStatus(t, buildWith(t, src, flags...))
			if out != "" || code != 42 {
				t.Errorf("printed %q and exited %d, want nothing and 42", out, code)
			}
		})
	}
}
