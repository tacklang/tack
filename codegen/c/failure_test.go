package c_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestFailureExampleIsSanitizerClean builds examples/failure, its C file
// and std's C with clang's warnings as errors and the address and
// undefined behavior sanitizers on, every finding fatal, and checks it
// writes exactly its output, every check ok: a std/errors Failure frees
// its detail exactly once, moved through calls, results, a list and a map,
// and when allocations fail, which the example's own count of live
// buffers checks.
func TestFailureExampleIsSanitizerClean(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "failure")
	src := readFile(t, filepath.Join(dir, "main.tck"))
	csrc := readFile(t, filepath.Join(dir, "live.c"))
	want := readFile(t, filepath.Join(dir, "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/failure/output expects a check to fail")
	}
	atO0Too(t, []string{"-O1", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"}, func(t *testing.T, flags []string) {
		bin := buildWithStdAndC(
			t,
			src,
			csrc,
			flags...,
		)
		var stderr strings.Builder
		cmd := exec.Command(bin)
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("failure failed: %v\n%s\nstderr:\n%s", err, out, stderr.String())
		}
		if string(out) != want {
			t.Errorf("failure wrote\n%q\nwant\n%q", out, want)
		}
	})
}
