package porttest

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// perfRuns is how many times TestResPerf runs each compiler, keeping the
// fastest run's time and the largest peak memory.
const perfRuns = 3

// perfTimeFactor and perfMemoryFactor bound the port against the Go
// compiler on TestResPerf's program: the port may take at most this many
// times Go's time, plus perfTimeSlack, and this many times its peak
// memory. Measured at M5, the port took a quarter of Go's time and a
// fifth of its memory, so either bound tripping means the port has lost
// that margin many times over.
const (
	perfTimeFactor   = 2
	perfMemoryFactor = 2
	perfTimeSlack    = 100 * time.Millisecond
)

// TestResPerf is the performance tripwire for --res: it runs both
// compilers, perfRuns times each, on the largest program the corpus has,
// the port's own driver, which imports the whole port and most of std,
// and fails when the port is slower or larger than perfTimeFactor and
// perfMemoryFactor allow, logging both compilers' fastest time and peak
// memory. It needs the port's source tree, and skips without one. It
// runs only as TestPort does, with TACK_PORT_TEST=1 and -count, and is
// not parallel, so the timings are of one compiler at a time.
func TestResPerf(t *testing.T) {
	ready(t)
	src, _ := portSource(os.Getenv("TACK_PORT"))
	if !dirExists(filepath.Join(src, "driver")) {
		t.Skip("no port source tree: the program is the port's driver")
	}
	args := []string{"--res", "./driver"}
	goTime, goMem := measure(t, env.goBin, env.goCache, src, args)
	portTime, portMem := measure(t, env.portRun, env.portCache, src, args)
	t.Logf("--res ./driver of the port: Go %v, %d KB; port %v, %d KB", goTime, goMem, portTime, portMem)
	if portTime > perfTimeFactor*goTime+perfTimeSlack {
		t.Errorf("the port took %v, more than %d times Go's %v", portTime, perfTimeFactor, goTime)
	}
	if portMem > perfMemoryFactor*goMem {
		t.Errorf("the port's peak memory was %d KB, more than %d times Go's %d KB", portMem, perfMemoryFactor, goMem)
	}
}

// measureVar, when set in the environment, makes the test binary a
// launcher for TestResPerf instead of running tests (see TestMain): it
// runs the command its value holds, each argument followed by a newline,
// with its standard output discarded and its standard error passed on,
// and writes to standard output the command's peak resident memory, in
// KB, from wait4. Linux keeps, as a process's peak, the peak of the
// process it was forked from, carried across exec, so a compiler started
// by the test binary itself, holding the whole corpus, would report the
// test binary's; started by the launcher, a fresh process, it reports
// its own.
const measureVar = "PORTTEST_MEASURE"

// launchMeasured is the launcher measureVar asks for, given the command
// cmdline, its arguments each followed by a newline. It returns the exit
// status, the command's, or 1 when it could not be run.
func launchMeasured(cmdline string) int {
	argv := strings.Split(strings.TrimSuffix(cmdline, "\n"), "\n")
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // G204: a compiler under test
	cmd.Stdout, cmd.Stderr = nil, os.Stderr
	err := cmd.Run()
	ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage)
	if !ok {
		fmt.Fprintln(os.Stderr, "porttest: no rusage")
		return 1
	}
	fmt.Println(ru.Maxrss)
	if err != nil {
		fmt.Fprintln(os.Stderr, "porttest:", err)
		return max(cmd.ProcessState.ExitCode(), 1)
	}
	return 0
}

// measure runs bin with args in dir perfRuns times, with cache as
// TACK_CACHE, each run through the launcher measureVar makes of this
// test binary, each to succeed, and returns the fastest run's wall time,
// the launcher's start included, the same for both compilers, and the
// largest peak resident memory, in KB.
func measure(t *testing.T, bin, cache, dir string, args []string) (time.Duration, int64) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// An environment variable cannot hold a 0 byte; no argument here
	// holds a newline.
	cmdline := bin + "\n" + strings.Join(args, "\n") + "\n"
	var best time.Duration
	var peak int64
	for i := range perfRuns {
		cmd := exec.Command(self) //nolint:gosec // G204: this test binary
		cmd.Dir = dir
		cmd.Env = append(compilerEnv(cache), measureVar+"="+cmdline)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		start := time.Now()
		if err := cmd.Run(); err != nil {
			t.Fatalf("running %s %v in %s: %v\n%s", bin, args, dir, err, stderr.Bytes())
		}
		took := time.Since(start)
		if i == 0 || took < best {
			best = took
		}
		kb, err := strconv.ParseInt(strings.TrimSpace(stdout.String()), 10, 64)
		if err != nil {
			t.Fatalf("the launcher wrote no peak memory: %q", stdout.String())
		}
		peak = max(peak, kb)
	}
	return best, peak
}
