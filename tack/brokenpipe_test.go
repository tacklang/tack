//go:build unix

package main

import (
	"bufio"
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

	"github.com/tacklang/tack/codegen"
)

// brokenPipeLive is the line examples/broken-pipe's C writes to its report
// pipe, descriptor 3, as the process ends with no buffer live.
const brokenPipeLive = "nothing live at process end ok\n"

// brokenPipeRun is one run of examples/broken-pipe: the case, the stream
// it breaks, 1 or 2, and what it must do.
type brokenPipeRun struct {
	name   string
	stream int    // the descriptor a pipe with no reader is given
	other  string // what the other of standard output and standard error gets
	report string // what reaches descriptor 3
	status int    // the exit status
}

// brokenPipeRuns are the cases of examples/broken-pipe that end through a
// broken pipe: every write to standard output or standard error, from
// print, std/fmt's stdout functions and its stderr twins, ends the
// process with status 141, every buffer freed and no defer run. return
// and exit write from a function that returns, whose defer runs, before
// the broken pipe is found: by the flush after main for return, and for
// exit by exit's own flush, so its status is 141, not the 3 it asked for.
var brokenPipeRuns = []brokenPipeRun{
	{"print", 1, "", brokenPipeLive, 141},
	{"println", 1, "", brokenPipeLive, 141},
	{"printf", 1, "", brokenPipeLive, 141},
	{"write", 1, "", brokenPipeLive, 141},
	{"big", 1, "", brokenPipeLive, 141},
	{"region", 1, "", brokenPipeLive, 141},
	{"return", 1, "", "buffered's defer ran\n" + brokenPipeLive, 141},
	{"exit", 1, "", "buffered's defer ran\n" + brokenPipeLive, 141},
	{"eprintln", 2, "start\n", brokenPipeLive, 141},
	{"eprintf", 2, "start\n", brokenPipeLive, 141},
	{"ewrite", 2, "start\n", brokenPipeLive, 141},
}

// brokenPipeEnd is how one run of examples/broken-pipe ended: its exit
// status, or the signal that killed it, what it wrote to the stream not
// broken, and what reached descriptor 3.
type brokenPipeEnd struct {
	status int
	signal syscall.Signal
	other  string
	report string
}

// startBrokenPipe starts bin with the case name, its descriptor stream a
// pipe's writing end, the other of 1 and 2 a pipe whose bytes wait
// returns, descriptor 3 the report pipe and descriptor 4 a pipe whose
// reader is closed before the start. With closed, the stream's reader is
// closed before the start too, so the program's first write to it fails,
// and nil is returned; otherwise its reading end is returned, for the
// caller to read from and close. The returned wait ends the run within
// 20 seconds, killing it past that, and returns how it ended.
func startBrokenPipe(t *testing.T, bin, name string, stream int, closed bool) (*os.File, func() brokenPipeEnd) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, bin, name)
	pipe := func() (*os.File, *os.File) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { r.Close(); w.Close() })
		return r, w
	}
	brokenR, brokenW := pipe()
	if closed {
		brokenR.Close()
		brokenR = nil
	}
	var other bytes.Buffer
	if stream == 1 {
		cmd.Stdout, cmd.Stderr = brokenW, &other
	} else {
		cmd.Stdout, cmd.Stderr = &other, brokenW
	}
	reportR, reportW := pipe()
	fd4R, fd4W := pipe()
	fd4R.Close()
	cmd.ExtraFiles = []*os.File{reportW, fd4W}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	brokenW.Close()
	reportW.Close()
	fd4W.Close()
	report := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(reportR)
		report <- string(b)
	}()
	return brokenR, func() brokenPipeEnd {
		err := cmd.Wait()
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			t.Fatalf("running %s %s: %v", bin, name, err)
		}
		if ctx.Err() != nil {
			t.Fatalf("%s %s still running after 20s", bin, name)
		}
		end := brokenPipeEnd{other: other.String(), report: <-report}
		ws := cmd.ProcessState.Sys().(syscall.WaitStatus)
		if ws.Signaled() {
			end.signal = ws.Signal()
		} else {
			end.status = ws.ExitStatus()
		}
		return end
	}
}

// buildBrokenPipe builds examples/broken-pipe at the level opt.
func buildBrokenPipe(t *testing.T, opt codegen.Opt) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "broken-pipe")
	r := runTack(t, "-O"+string(opt), "--out", bin, example("broken-pipe"))
	if r.code != exitOK || r.stderr != "" {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	return bin
}

// checkBrokenPipeEnd checks the run tc ended as it must.
func checkBrokenPipeEnd(t *testing.T, tc brokenPipeRun, got brokenPipeEnd) {
	t.Helper()
	if got.signal != 0 {
		t.Errorf("killed by %v, want exit status %d", got.signal, tc.status)
	} else if got.status != tc.status {
		t.Errorf("exit status %d, want %d", got.status, tc.status)
	}
	if got.other != tc.other {
		t.Errorf("the other stream got %q, want %q", got.other, tc.other)
	}
	if got.report != tc.report {
		t.Errorf("descriptor 3 got %q, want %q", got.report, tc.report)
	}
}

// TestBuiltBrokenPipeEndsLikeExit builds examples/broken-pipe at every
// optimization level and runs each case that writes to a stream with
// that stream a pipe whose reader closed before the program started, and
// checks it ends with status 141, every buffer freed and no defer run, as
// exit(141) ends it, where the process used to be killed by SIGPIPE with
// nothing freed. Each case that writes forever is then run again with a
// reader that reads one line and closes, as head -1 does, and must end
// within the run's limit just the same.
func TestBuiltBrokenPipeEndsLikeExit(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := buildBrokenPipe(t, opt)
			for _, tc := range brokenPipeRuns {
				t.Run(tc.name, func(t *testing.T) {
					_, wait := startBrokenPipe(t, bin, tc.name, tc.stream, true)
					checkBrokenPipeEnd(t, tc, wait())
				})
				if tc.name == "big" || tc.name == "return" || tc.name == "exit" {
					continue
				}
				t.Run(tc.name+" after a line", func(t *testing.T) {
					r, wait := startBrokenPipe(t, bin, tc.name, tc.stream, false)
					start := time.Now()
					line, err := bufio.NewReader(r).ReadString('\n')
					if err != nil || line == "" {
						t.Errorf("reading a line: %q, %v", line, err)
					}
					r.Close()
					end := wait()
					t.Logf("ended %v after the first line was read", time.Since(start))
					checkBrokenPipeEnd(t, tc, end)
				})
			}
		})
	}
}

// TestBuiltBrokenPipeOtherDescriptors builds examples/broken-pipe at
// every optimization level and checks that a write to any descriptor but
// standard output and standard error still fails with EPIPE as an error
// the program sees and goes on from: os.write_file to /dev/fd/4, a pipe
// whose reader closed before the start, returns error{code: 3, msg:
// "Broken pipe"}. It then checks that a child os.run starts has SIGPIPE's
// default action, though the program ignores SIGPIPE: yes, writing into
// a pipe head has closed, dies of it, so the shell reports status 141,
// where a yes that inherited SIGPIPE ignored would report its write's
// error and status 1. Last, a standard output made wide-oriented, whose
// every write of bytes fails with errno untouched, after errno was left
// EPIPE: the writes lose their bytes and the program exits 0, since each
// checked write clears errno first.
func TestBuiltBrokenPipeOtherDescriptors(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	for _, tool := range []string{"sh", "yes", "head"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := buildBrokenPipe(t, opt)
			for _, tc := range []struct{ name, out string }{
				{"write-file", "write_file 3 Broken pipe\n"},
				{"child", "child 0 0 141\n"},
				{"wide", ""},
			} {
				t.Run(tc.name, func(t *testing.T) {
					// The stream broken is standard error, which neither
					// case writes to, so its standard output is read whole.
					_, wait := startBrokenPipe(t, bin, tc.name, 2, true)
					checkBrokenPipeEnd(t, brokenPipeRun{tc.name, 2, tc.out, brokenPipeLive, 0}, wait())
				})
			}
		})
	}
}
