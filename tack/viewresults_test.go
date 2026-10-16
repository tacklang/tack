package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/codegen"
)

// TestBuiltViewResultsWritesItsOutput builds examples/view-results at
// every optimization level and checks each binary writes exactly
// examples/view-results/output, every check ok: views returned by
// functions and by methods, a pub method of another package among them,
// read by len, the checked index, slicing, range, a spread and a view
// parameter, returned on through nested calls, a generic function and a
// union's method, at the empty edges, with c_allocator's count of live
// buffers unchanged by reading them and back to zero after each group.
func TestBuiltViewResultsWritesItsOutput(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "view-results", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(want), "FAIL") {
		t.Fatal("examples/view-results/output expects a check to fail")
	}
	target := filepath.Join(examplesDir, "view-results")
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "view-results")
			r := runTack(t, "-O"+string(opt), "--out", bin, target)
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != string(want) || code != 0 {
				t.Errorf("wrote\n%s\nand exited %d, want\n%s\nand 0", out, code, want)
			}
		})
	}
}
