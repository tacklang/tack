package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The fallthrough example, built by the driver with no warning, prints
// exactly examples/fallthrough/output, the path each switch took, and
// exits 42 once every check by value holds.
func TestBuiltFallthroughPrintsAndExits42(t *testing.T) {
	bin := buildWithClang(t, "fallthrough")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
		t.Errorf("fallthrough exited with %v, want status 42", err)
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "fallthrough", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(want) {
		t.Errorf("fallthrough printed\n%s\nwant\n%s", out, want)
	}
}

// The fallthrough-facts example, built by the driver with no warning,
// exits 42: each division in it is proven on every path into it, a
// fallthrough among them, and gives the value main checks.
func TestBuiltFallthroughFactsExits42(t *testing.T) {
	bin := buildWithClang(t, "fallthrough-facts")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
		t.Errorf("fallthrough-facts exited with %v, want status 42", err)
	}
}
