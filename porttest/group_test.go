package porttest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// processWaitDelay is how long Wait waits, once a process the harness
// runs in a group of its own has ended or been killed, for its output
// pipes to close, which a process it started may hold open, before
// closing them itself.
const processWaitDelay = 5 * time.Second

// outlivedGrace is how long reapGroup waits, once a group's leader has
// ended, for the rest of the group to end on its own before it counts
// the group as outliving it: a process that ends at its input's end, as
// a sanitizer's symbolizer does, or a child the leader never waited
// for, a zombie until the process that inherits it reaps it.
const outlivedGrace = 2 * time.Second

// outlivedPoll is how often reapGroup looks at the group in that time.
const outlivedPoll = 10 * time.Millisecond

// groups are the process groups runInGroup's commands run in, by their
// ids, which are their leaders' process ids, while they run: the groups
// killGroups kills when the harness is signaled.
var groups = struct {
	live map[int]bool
	sync.Mutex
}{live: map[int]bool{}}

// runInGroup runs cmd, which must not be started yet, in a process group
// of its own, and returns whether a process it started outlived it, and
// Run's error. The cancel of its context kills the whole group, so that
// what the process started dies with it rather than hold its output
// open; Wait waits for that output at most processWaitDelay after the
// process ends. The process gets SIGKILL should this one die first
// (Pdeathsig), and the group is registered, for killGroups, while it
// runs. Pdeathsig is sent when the thread that started the process ends,
// not the process, so the goroutine keeps to its thread from the start
// to the end of Wait.
func runInGroup(cmd *exec.Cmd) (bool, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = processWaitDelay
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := cmd.Start(); err != nil {
		return false, err //nolint:wrapcheck // Run's error, as Run returns it
	}
	pgid := cmd.Process.Pid
	groups.Lock()
	groups.live[pgid] = true
	groups.Unlock()
	err := cmd.Wait()
	outlived := reapGroup(pgid)
	groups.Lock()
	delete(groups.live, pgid)
	groups.Unlock()
	return outlived, err //nolint:wrapcheck // Run's error, as Run returns it
}

// reapGroup waits up to outlivedGrace for the process group pgid, whose
// leader has ended, to empty, and reports whether it did not: then it
// kills every process left in it and returns true, a process the leader
// started having outlived it.
func reapGroup(pgid int) bool {
	for deadline := time.Now().Add(outlivedGrace); ; time.Sleep(outlivedPoll) {
		if syscall.Kill(-pgid, 0) != nil {
			return false
		}
		if time.Now().After(deadline) {
			break
		}
	}
	// The group may empty between the last look and the kill, which is
	// the end the kill is for.
	_ = syscall.Kill(-pgid, syscall.SIGKILL) //nolint:errcheck // see above
	return true
}

// handleSignals makes SIGINT, SIGTERM and SIGHUP, as Ctrl-C and timeout
// send, kill every group runInGroup runs, which are not in the group a
// terminal or timeout signals, and then end this process with status 1.
// Without it a signaled harness would leave its runs' descendants
// running: Pdeathsig reaches only the processes it starts itself.
func handleSignals() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		sig := <-ch
		n := killGroups()
		fmt.Fprintf(os.Stderr, "porttest: %v: killed %d process groups\n", sig, n)
		os.Exit(1)
	}()
}

// killGroups kills every registered group and returns how many there
// were. It keeps the registry locked until the process ends, so that no
// group registers after it; a process started but not yet registered
// is ended only by its Pdeathsig when the harness exits, and what it
// started may outlive the harness.
func killGroups() int {
	groups.Lock()
	for pgid := range groups.live {
		_ = syscall.Kill(-pgid, syscall.SIGKILL) //nolint:errcheck // a group may have just ended
	}
	return len(groups.live)
}

// runEnd is how a run of a process the harness started ended.
type runEnd int

const (
	// endExited is an exit, with any status, or, where the run allows
	// it, the end a signal the process got itself gave it.
	endExited runEnd = iota
	// endTimeout is a kill at the run's time limit.
	endTimeout
	// endFailed is a process that could not be started or waited for,
	// or one a signal ended where the run allows none.
	endFailed
	// endOutlived is an end that would be endExited but that left a
	// process it started running.
	endOutlived
)

// endOf returns how a run ended, from the error of its context, ctxErr,
// Run's error, err, and whether a process it started outlived it;
// signals says whether an end by a signal counts as an exit. A time
// limit's kill comes first, and a failed end before an outliving
// process, which the caller notes beside it (see outlivedNote).
func endOf(ctxErr, err error, outlived, signals bool) runEnd {
	var exit *exec.ExitError
	exitedAsAllowed := errors.As(err, &exit) && (signals || exit.ExitCode() >= 0)
	switch {
	case errors.Is(ctxErr, context.DeadlineExceeded):
		return endTimeout
	case err != nil && !exitedAsAllowed:
		return endFailed
	case outlived:
		return endOutlived
	}
	return endExited
}

// failEnd fails t, a run named running having ended end with Run's
// error err, as cmd, whose standard error was stderr, after limit when
// it timed out: each message names how it ended and holds its standard
// error. It does nothing for endExited.
func failEnd(
	t *testing.T, end runEnd, running string, cmd *exec.Cmd, err error, outlived bool,
	limit time.Duration, stderr []byte,
) {
	t.Helper()
	switch end {
	case endTimeout:
		t.Fatalf("%s: killed after %v, with every process it started\nstderr:\n%s",
			running, limit, stderr)
	case endFailed:
		t.Fatalf("%s: %v%s\nstderr:\n%s", running, err, outlivedNote(outlived), stderr)
	case endOutlived:
		t.Fatalf("%s: %s, and a process it started outlived it, killed\nstderr:\n%s",
			running, cmd.ProcessState, stderr)
	case endExited:
	}
}

// outlivedNote returns the note a failure's message carries when a
// process the run started outlived it too, or "".
func outlivedNote(outlived bool) string {
	if outlived {
		return "; a process it started outlived it too, killed"
	}
	return ""
}

// TestEndOf checks endOf's precedence: the time limit first, then a
// failed end, then an outliving process, and that a signal's end is an
// exit only where signals are allowed.
func TestEndOf(t *testing.T) {
	exited := exec.Command("sh", "-c", "exit 3").Run()
	signaled := exec.Command("sh", "-c", "kill -KILL $$").Run()
	notRun := exec.Command(filepath.Join(t.TempDir(), "missing")).Run()
	if exited == nil || signaled == nil || notRun == nil {
		t.Fatalf("the runs this test needs ended %v, %v, %v; want errors", exited, signaled, notRun)
	}
	deadline := context.DeadlineExceeded
	cases := []struct {
		ctxErr, err       error
		name              string
		want              runEnd
		outlived, signals bool
	}{
		{name: "exit 0", want: endExited},
		{name: "exit 3", err: exited, want: endExited},
		{name: "signal, allowed", err: signaled, signals: true, want: endExited},
		{name: "signal, not allowed", err: signaled, want: endFailed},
		{name: "not started", err: notRun, want: endFailed},
		{name: "outlived", outlived: true, want: endOutlived},
		{name: "exit 3, outlived", err: exited, outlived: true, want: endOutlived},
		{name: "signal not allowed, outlived", err: signaled, outlived: true, want: endFailed},
		{
			name: "signal allowed, outlived", err: signaled, signals: true, outlived: true,
			want: endOutlived,
		},
		{name: "timeout", ctxErr: deadline, err: signaled, outlived: true, want: endTimeout},
		{name: "canceled, not timed out", ctxErr: context.Canceled, want: endExited},
	}
	for _, tc := range cases {
		if got := endOf(tc.ctxErr, tc.err, tc.outlived, tc.signals); got != tc.want {
			t.Errorf("%s: endOf = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// gone waits up to outlivedGrace for the process pid to end and be
// reaped, and reports whether it did.
func gone(pid int) bool {
	for deadline := time.Now().Add(outlivedGrace); time.Now().Before(deadline); {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
		time.Sleep(outlivedPoll)
	}
	return false
}

// readPid returns the process id the file at p holds, waiting up to
// outlivedGrace for it to be written, failing t when it is not.
func readPid(t *testing.T, p string) int {
	t.Helper()
	for deadline := time.Now().Add(outlivedGrace); time.Now().Before(deadline); {
		data, err := os.ReadFile(p) //nolint:gosec // G304: the test's own file
		if pid, perr := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && perr == nil {
			return pid
		}
		time.Sleep(outlivedPoll)
	}
	t.Fatalf("%s holds no process id", p)
	return 0
}

// TestGroupTimeoutKillsGrandchild checks that a run killed at its time
// limit takes every process it started with it, a grandchild holding
// none of its output among them, and is no outliving one.
func TestGroupTimeoutKillsGrandchild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c",
		`sleep 30 >/dev/null 2>&1 & echo $! > "$0"; exec sleep 30`, pidFile)
	outlived, err := runInGroup(cmd)
	if got := endOf(ctx.Err(), err, outlived, true); got != endTimeout || outlived {
		t.Errorf("end %d, outlived %v; want endTimeout, not outlived", got, outlived)
	}
	if pid := readPid(t, pidFile); !gone(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL) //nolint:errcheck // cleanup
		t.Errorf("the grandchild, %d, still runs after the timeout", pid)
	}
}

// TestGroupLingeringGrandchild checks that a grandchild still running
// outlivedGrace after its parent exits is reported, and killed.
func TestGroupLingeringGrandchild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	cmd := exec.CommandContext(t.Context(), "sh", "-c",
		`sleep 30 >/dev/null 2>&1 & echo $! > "$0"; exit 0`, pidFile)
	outlived, err := runInGroup(cmd)
	if got := endOf(nil, err, outlived, true); got != endOutlived {
		t.Errorf("end %d, want endOutlived", got)
	}
	if pid := readPid(t, pidFile); !gone(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL) //nolint:errcheck // cleanup
		t.Errorf("the grandchild, %d, still runs", pid)
	}
}

// TestGroupQuickEndsNotOutlived checks that what ends on its own soon
// after its parent is no outliving process: a grandchild that ends at
// its input's end, as a symbolizer does, and a child the parent never
// waits for, a zombie until it is reaped; each run many times, four at
// once, as a loaded machine runs them.
func TestGroupQuickEndsNotOutlived(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash on PATH")
	}
	fixtures := []struct {
		shell, script string
		code          int
	}{
		{"bash", `exec 4> >(cat >/dev/null); exit 86`, 86},
		{"sh", `true >/dev/null 2>&1 & exec sleep 0.05`, 0},
	}
	for _, f := range fixtures {
		t.Run(f.shell, func(t *testing.T) {
			var wg sync.WaitGroup
			for range 4 {
				wg.Go(func() {
					for range 10 {
						cmd := exec.CommandContext(t.Context(), f.shell, "-c", f.script)
						outlived, err := runInGroup(cmd)
						if outlived || cmd.ProcessState == nil ||
							cmd.ProcessState.ExitCode() != f.code {
							t.Errorf("%s: outlived %v, %v; want exit %d, not outlived",
								f.script, outlived, err, f.code)
							return
						}
					}
				})
			}
			wg.Wait()
		})
	}
}

// groupHelperVar, set in the environment, makes the test binary the
// harness TestGroupHarnessSignalled and TestGroupHarnessKilled stop: it
// runs its value as a shell script through runInGroup, with signals
// handled as TestMain handles them, and waits.
const groupHelperVar = "PORTTEST_GROUP_HELPER"

// groupHelper is the test binary as groupHelperVar makes it: it runs
// script and returns 0 when the script ends.
func groupHelper(script string) int {
	handleSignals()
	cmd := exec.CommandContext(context.Background(), "sh", "-c", script)
	if _, err := runInGroup(cmd); err != nil {
		fmt.Fprintln(os.Stderr, "porttest:", err)
		return 1
	}
	return 0
}

// stopHarness runs the test binary as a harness, through timeout with
// the signal sig after 2 seconds, running a script that starts a
// grandchild holding none of the output and then sleeps, and returns
// the grandchild's and the child's process ids, once timeout ends.
func stopHarness(t *testing.T, sig string) (grandchild, child int) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := fmt.Sprintf(`sleep 30 >/dev/null 2>&1 & echo $! > %q; echo $$ > %q; exec sleep 30`,
		filepath.Join(dir, "grandchild"), filepath.Join(dir, "child"))
	cmd := exec.CommandContext(t.Context(), "timeout", "-s", sig, "2", self) //nolint:gosec // G204: this test binary
	cmd.Env = append(os.Environ(), groupHelperVar+"="+script)
	out, err := cmd.CombinedOutput()
	// timeout ends 124 when it stopped its command with SIGTERM; SIGKILL,
	// which it sends to its whole process group, ends it too.
	var exit *exec.ExitError
	if !errors.As(err, &exit) || (exit.ExitCode() != 124 && sig != "KILL") {
		t.Fatalf("the harness under timeout ended %v, want it stopped by timeout\n%s", err, out)
	}
	t.Logf("the harness, stopped by SIG%s, ended %v: %s", sig, err, out)
	return readPid(t, filepath.Join(dir, "grandchild")), readPid(t, filepath.Join(dir, "child"))
}

// TestGroupHarnessSignalled checks that a harness that timeout stops with
// SIGTERM leaves nothing running: its handler kills its run's whole
// group, the grandchild included.
func TestGroupHarnessSignalled(t *testing.T) {
	grandchild, child := stopHarness(t, "TERM")
	for _, pid := range []int{grandchild, child} {
		if !gone(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL) //nolint:errcheck // cleanup
			t.Errorf("process %d still runs after the harness was stopped", pid)
		}
	}
}

// TestGroupHarnessKilled checks that a harness that timeout kills with
// SIGKILL, which no handler sees, still takes the process it started
// with it, by Pdeathsig. A grandchild then outlives it, which this test
// shows and then kills: Pdeathsig reaches only the harness's own
// children.
func TestGroupHarnessKilled(t *testing.T) {
	grandchild, child := stopHarness(t, "KILL")
	if !gone(child) {
		_ = syscall.Kill(child, syscall.SIGKILL) //nolint:errcheck // cleanup
		t.Errorf("the harness's child, %d, still runs after the harness was killed", child)
	}
	if gone(grandchild) {
		t.Logf("the grandchild, %d, ended too", grandchild)
		return
	}
	t.Logf("the grandchild, %d, outlives a killed harness, as documented; killing it", grandchild)
	_ = syscall.Kill(grandchild, syscall.SIGKILL) //nolint:errcheck // cleanup
}
