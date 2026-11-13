//go:build unix

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestTackTestBrokenPipe runs the tests of tt/pipe with standard output
// a pipe whose reader is closed: the test's writes end the binary with
// status 141, as they end any program, freeing its buffers and writing
// nothing more, and tack test says how it ended and exits 1.
func TestTackTestBrokenPipe(t *testing.T) {
	skipUnlessClang(t)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var stderr bytes.Buffer
	code := run(t.Context(), []string{testCommand, "-v", tackTestPkg("pipe")}, testEnv(t), w, &stderr)
	want := "FAIL tt/pipe: the test binary exited with status 141\n"
	if code != exitFailure || stderr.String() != want {
		t.Errorf("exit %d, stderr %q, want 1 and %q", code, stderr.String(), want)
	}
}

// skipUnlessClang skips the test when clang is not on PATH.
func skipUnlessClang(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
}

// TestTackTestSignalDeath checks a test binary killed by a signal, which
// no valid Tack program is, is reported as a failure of the package, as
// is one that ends with a status the runner never gives: runTests is
// given a binary that does each, a script.
func TestTackTestSignalDeath(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		script string
		want   string
	}{
		{"#!/bin/sh\nkill -SEGV $$\n", "FAIL p: the test binary was killed by signal 11 (segmentation fault)\n"},
		{"#!/bin/sh\nkill -KILL $$\n", "FAIL p: the test binary was killed by signal 9 (killed)\n"},
		{"#!/bin/sh\nexit 7\n", "FAIL p: the test binary exited with status 7\n"},
		{"#!/bin/sh\necho reported >&2\nexit 1\n", ""},
	} {
		bin := filepath.Join(dir, "tests")
		if err := os.WriteFile(bin, []byte(tc.script), 0o700); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		err := runTests(t.Context(), bin, dir, "p", &stdout, &stderr)
		if code := reportTest(err, &stderr); code != exitFailure {
			t.Errorf("%q: exit %d, want 1", tc.script, code)
		}
		want := tc.want
		if want == "" {
			want = "reported\n"
		}
		if stderr.String() != want {
			t.Errorf("%q: stderr %q, want %q", tc.script, stderr.String(), want)
		}
	}
}
