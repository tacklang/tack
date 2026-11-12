package c_test

import (
	"path/filepath"
	"testing"
)

// fallthroughBuilds are the clang flags each fallthrough example is built
// with: optimized, and with the address and undefined behavior sanitizers
// on, every finding fatal, so a jump into the next case that skips or
// repeats a defer, or reads past a proof, is caught at run time.
var fallthroughBuilds = [][]string{
	{"-O2"},
	{"-O0", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
	sanitizers,
}

// TestFallthroughExampleRuns builds examples/fallthrough with clang's
// warnings as errors, each way fallthroughBuilds says, and checks it
// prints exactly its output file and exits 42: chains of fallthrough
// across tagged, tagless and string switches, into and out of a default
// written in the middle and one written last, into cases whose values are
// never evaluated, out of cases holding defers, in a loop with break and
// continue, in nested switches, and with each case's own facts used up to
// its fallthrough.
func TestFallthroughExampleRuns(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "fallthrough")
	src := readFile(t, filepath.Join(dir, "main.tck"))
	want := readFile(t, filepath.Join(dir, "output"))
	for _, flags := range fallthroughBuilds {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			out, code := runStatus(t, buildWith(t, src, flags...))
			if out != want || code != 42 {
				t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 42", out, code, want)
			}
		})
	}
}

// TestFallthroughFactsExampleRuns builds examples/fallthrough-facts each
// way fallthroughBuilds says and checks it exits 42, printing nothing:
// every division it holds is proven on every path into it, a fallthrough
// path among them, and gives the value the check in main expects.
func TestFallthroughFactsExampleRuns(t *testing.T) {
	src := readFile(t, filepath.Join("..", "..", "examples", "fallthrough-facts", "main.tck"))
	for _, flags := range fallthroughBuilds {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			out, code := runStatus(t, buildWith(t, src, flags...))
			if out != "" || code != 42 {
				t.Errorf("printed %q and exited %d, want nothing and 42", out, code)
			}
		})
	}
}
