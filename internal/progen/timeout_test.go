package progen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hangLimit is how long a test of a timeout lets a hanging command run
// before it must have been killed and reported: far above the short
// timeout the test gives it, far below the minute the hang would last.
const hangLimit = 10 * time.Second

// TestRunBinKillsAHangingBinary runs a stand-in for a built binary that
// sleeps for a minute, with a short timeout, and checks that it is killed
// within hangLimit and that the run fails with an error that names the
// program's source, rather than passing or blocking.
func TestRunBinKillsAHangingBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "prog-O0")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 60\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "program-O0.c")
	start := time.Now()
	_, _, _, err := runBinWithin(200*time.Millisecond, bin, src, "")
	if elapsed := time.Since(start); elapsed > hangLimit {
		t.Fatalf("the hanging binary ran for %v, want it killed after 200ms", elapsed)
	}
	if err == nil {
		t.Fatal("a binary killed for running too long was reported as a run that finished")
	}
	for _, want := range []string{src, "did not finish within 200ms"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestRunToolKillsAHangingCompiler runs sleep for a minute in place of
// clang, with a short timeout, and checks that it is killed within
// hangLimit and that the call fails with an error that names the source
// being compiled.
func TestRunToolKillsAHangingCompiler(t *testing.T) {
	src := filepath.Join(t.TempDir(), "program-O0.c")
	start := time.Now()
	_, err := runTool(200*time.Millisecond, src, "sleep", "60")
	if elapsed := time.Since(start); elapsed > hangLimit {
		t.Fatalf("the hanging compiler ran for %v, want it killed after 200ms", elapsed)
	}
	if err == nil {
		t.Fatal("a compiler killed for running too long was reported as a call that finished")
	}
	for _, want := range []string{src, "did not finish within 200ms"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestRunBinReportsExitStatus checks that a binary that finishes in time,
// exiting non-zero, is reported with its status and no error, so the
// timeout does not change how an ordinary failing run is seen.
func TestRunBinReportsExitStatus(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "prog-O0")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho out\necho err >&2\nexit 3\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	out, errOut, status, err := runBinWithin(runTimeout, bin, filepath.Join(dir, "program-O0.c"), "")
	if err != nil || status != 3 || out != "out\n" || errOut != "err\n" {
		t.Fatalf("got %q, %q, exit %d, error %v; want \"out\\n\", \"err\\n\", exit 3, no error",
			out, errOut, status, err)
	}
}
