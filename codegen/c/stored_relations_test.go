package c_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestStoredRelationsExampleIsSanitizerClean builds
// examples/stored-relations with clang's warnings as errors, optimized,
// and with the address and undefined behavior sanitizers on at -O0 and
// -O1, every finding fatal, and checks each binary prints exactly its
// output file, every check ok, and exits 0: indexes only a relation
// chained through a stored value's bounds proves, read one below, at and
// one past every bound and at int64's extremes, where a bound kept past
// the write that ended it would read out of range.
func TestStoredRelationsExampleIsSanitizerClean(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "stored-relations")
	src := readFile(t, filepath.Join(dir, "main.tck"))
	want := readFile(t, filepath.Join(dir, "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/stored-relations/output expects a check to fail")
	}
	for _, flags := range fallthroughBuilds {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			out, code := runStatus(t, buildWith(t, src, flags...))
			if out != want || code != 0 {
				t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 0", out, code, want)
			}
		})
	}
}
