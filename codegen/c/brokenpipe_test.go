//go:build unix

package c_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestBrokenPipeExampleIsSanitizerClean builds examples/broken-pipe, its
// C file and std's C with clang's warnings as errors and the address,
// leak and undefined behavior sanitizers on, every finding fatal, and
// runs each case that writes to standard output or standard error with
// that stream a pipe whose reader closed before the start. Each must end
// with status 141 and its C's line on descriptor 3 that nothing is live:
// the end a broken pipe makes frees every buffer c_allocator holds, which
// the line checks, since a buffer still on c_allocator's list is
// reachable and so no leak to the leak sanitizer; the leak sanitizer,
// which runs as exit ends the process and would make the status 23,
// finds no allocation left unreachable, std's and the C library's
// included; and nothing reads or writes memory it should not. Before the
// end went through exit, SIGPIPE killed the process and no check ran.
func TestBrokenPipeExampleIsSanitizerClean(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "broken-pipe")
	src := readFile(t, filepath.Join(dir, "main.tck"))
	csrc := readFile(t, filepath.Join(dir, "live.c"))
	const live = "nothing live at process end ok\n"
	cases := []struct {
		name   string
		stream int
		report string
	}{
		{"print", 1, live},
		{"println", 1, live},
		{"printf", 1, live},
		{"write", 1, live},
		{"big", 1, live},
		{"region", 1, live},
		{"return", 1, "buffered's defer ran\n" + live},
		{"exit", 1, "buffered's defer ran\n" + live},
		{"eprintln", 2, live},
		{"eprintf", 2, live},
		{"ewrite", 2, live},
	}
	atO0Too(t, []string{"-O1", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"}, func(t *testing.T, flags []string) {
		bin := buildWithStdAndC(t, src, csrc, flags...)
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				brokenR, brokenW, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				brokenR.Close()
				defer brokenW.Close()
				reportR, reportW, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				defer reportR.Close()
				var other bytes.Buffer
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				cmd := exec.CommandContext(ctx, bin, tc.name)
				cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_leaks=1")
				if tc.stream == 1 {
					cmd.Stdout, cmd.Stderr = brokenW, &other
				} else {
					cmd.Stdout, cmd.Stderr = &other, brokenW
				}
				cmd.ExtraFiles = []*os.File{reportW}
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				reportW.Close()
				report, readErr := io.ReadAll(reportR)
				if readErr != nil {
					t.Fatal(readErr)
				}
				var exitErr *exec.ExitError
				if err := cmd.Wait(); err != nil && !errors.As(err, &exitErr) {
					t.Fatalf("running %s %s: %v", bin, tc.name, err)
				}
				ws := cmd.ProcessState.Sys().(syscall.WaitStatus)
				if ws.Signaled() || ws.ExitStatus() != 141 {
					t.Errorf("ended %v, want exit status 141\nother stream:\n%s", ws, other.String())
				}
				if string(report) != tc.report {
					t.Errorf("descriptor 3 got %q, want %q", report, tc.report)
				}
			})
		}
	})
}
