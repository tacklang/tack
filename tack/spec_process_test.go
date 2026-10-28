//go:build linux

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The environment variables by which TestSpecTestDies tells
// TestSpecHelperLoop, in a test binary of its own, what to run.
const (
	// specHelperDirEnv names the directory the helper writes its binary
	// and the process IDs to: the parent's, which the parent removes, as
	// the helper, killed, cannot.
	specHelperDirEnv = "TACK_SPEC_HELPER_DIR"
	// specHelperScriptEnv names the kind of binary the helper runs, one of
	// the keys of specHelperScripts.
	specHelperScriptEnv = "TACK_SPEC_HELPER_SCRIPT"
	// specHelperSignalsEnv, when set, makes the helper run its binary
	// without the parent-death signal, so that only the SIGINT and
	// SIGTERM handling of watchSpecSignals can kill it, as on macOS.
	specHelperSignalsEnv = "TACK_SPEC_HELPER_SIGNALS_ONLY"
)

// specHelperScripts are the binaries TestSpecHelperLoop can run, by
// kind. Each runs in its directory, which it finds through the
// environment, never spliced into its text, and writes its process ID to
// the file pid there and loops forever; child also leaves a sleep running
// in its group, its ID in the file child; trap ignores SIGTERM and
// SIGINT; and late sleeps 5s first, so its test binary's 2s timeout ends
// that before it writes anything.
var specHelperScripts = map[string]string{
	"loop":  "echo $$ > pid\nwhile :; do :; done\n",
	"child": "sleep 300 &\necho $! > child\necho $$ > pid\nwhile :; do :; done\n",
	"trap":  "trap '' TERM INT\necho $$ > pid\nwhile :; do :; done\n",
	"late":  "sleep 5\necho $$ > pid\nwhile :; do :; done\n",
}

// TestSpecHelperLoop is the helper TestSpecTestDies runs in a test binary
// of its own: it runs the binary of specHelperScripts its environment
// names, in the directory it names, under a limit far past the test's,
// until the test binary dies. Run any other way, it does nothing.
func TestSpecHelperLoop(t *testing.T) {
	dir := os.Getenv(specHelperDirEnv)
	if dir == "" {
		return
	}
	specNoParentDeath = os.Getenv(specHelperSignalsEnv) != ""
	bin := filepath.Join(dir, "script")
	// set -e and a quoted variable: a script that cannot reach its
	// directory ends, rather than writing where it was started.
	script := "#!/bin/sh\nset -e\ncd \"$" + specHelperDirEnv + "\"\n" +
		specHelperScripts[os.Getenv(specHelperScriptEnv)]
	//nolint:gosec // G306: a script the test runs
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	runSpecBinary(t.Context(), bin, time.Hour)
}

// specDiesCase is one way TestSpecTestDies ends a test binary running a
// spec binary.
type specDiesCase struct {
	name    string
	script  string         // the kind of binary, a key of specHelperScripts
	sig     syscall.Signal // the signal, or 0 to let the test's timeout end it
	only    bool           // whether the binary runs without the parent-death signal
	ignored bool           // whether the test binary inherits SIGINT ignored
}

// TestSpecTestDies checks that when a test running a spec binary dies,
// the binary, and what it started, dies with it. It starts this
// package's test binary running TestSpecHelperLoop, as the leader of a
// process group as a terminal starts a foreground job, waits for the
// spec binary to start, and then sends the test binary's group SIGINT, as
// Ctrl-C does, or SIGTERM, or SIGKILL to the test binary alone, or lets
// the test binary's own 2s timeout end it with a panic. The spec binary,
// in a group of its own that none of these reaches, and a process it
// started must be gone too, and the test binary must have ended as the
// signal, or the timeout's panic, ends it.
//
// A test binary that inherits SIGINT ignored must go on ignoring it, and
// still end, with all it ran, on the SIGTERM that follows.
func TestSpecTestDies(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a test binary")
	}
	for _, tc := range []specDiesCase{
		{"interrupt, signals only", "loop", syscall.SIGINT, true, false},
		{"terminate, signals only", "loop", syscall.SIGTERM, true, false},
		{"interrupt", "loop", syscall.SIGINT, false, false},
		{"kill", "loop", syscall.SIGKILL, false, false},
		{"test timeout", "loop", 0, false, false},
		{"a child, interrupt, signals only", "child", syscall.SIGINT, true, false},
		{"ignoring TERM and INT, interrupt, signals only", "trap", syscall.SIGINT, true, false},
		{"ignoring TERM and INT, kill", "trap", syscall.SIGKILL, false, false},
		{"interrupt ignored, then terminate", "loop", syscall.SIGTERM, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) { checkSpecTestDies(t, tc) })
	}
}

// checkSpecTestDies runs one case of TestSpecTestDies, in a fresh
// directory.
func checkSpecTestDies(t *testing.T, tc specDiesCase) {
	t.Helper()
	checkSpecTestDiesIn(t, t.TempDir(), tc)
}

// checkSpecTestDiesIn runs one case of TestSpecTestDies in dir.
func checkSpecTestDiesIn(t *testing.T, dir string, tc specDiesCase) {
	t.Helper()
	helper, done := startSpecHelper(t, dir, tc)
	waitForPidFile(t, filepath.Join(dir, "pid"), done)
	if tc.ignored {
		// Ignored, SIGINT ends nothing: the test binary and its spec
		// binary run on, and the SIGTERM after it ends them.
		if err := syscall.Kill(-helper.Process.Pid, syscall.SIGINT); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			t.Fatalf("the test binary ended on an ignored SIGINT: %v", err)
		case <-time.After(300 * time.Millisecond):
		}
		if state := specProcessState(
			readSpecPidOrFail(t, filepath.Join(dir, "pid")),
		); state == "" ||
			state == "Z" {
			t.Fatal("the spec binary ended on an ignored SIGINT")
		}
	}
	if err := signalSpecHelper(helper, tc.sig); err != nil {
		t.Fatal(err)
	}
	awaitSpecHelper(t, helper, done)
	assertSpecProcessDead(t, filepath.Join(dir, "pid"))
	if tc.script == "child" {
		assertSpecProcessDead(t, filepath.Join(dir, "child"))
	}
	assertSpecHelperEnd(t, helper, tc.sig)
}

// readSpecPidOrFail returns the process ID pidFile holds.
func readSpecPidOrFail(t *testing.T, pidFile string) string {
	t.Helper()
	data, err := os.ReadFile(pidFile) //nolint:gosec // G304: a file the test made
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

// startSpecHelper starts this test binary running TestSpecHelperLoop,
// with a 2s timeout, as the leader of a session of its own, and so of a
// process group, as a terminal's foreground job leads one; its binary is
// the one tc names, written to dir; it inherits SIGINT ignored when tc
// says so, started through a shell that ignores it. It returns the
// helper and a channel that receives its Wait's result once it ends.
//
// Every process the helper starts stays in its session, whatever group
// it makes, so the cleanup kills the whole session (see
// killSpecSession): what a test that failed early, or a helper that died
// before its binary said its process ID, leaves running.
func startSpecHelper(t *testing.T, dir string, tc specDiesCase) (*exec.Cmd, <-chan error) {
	t.Helper()
	args := []string{
		os.Args[0],
		"-test.run=^TestSpecHelperLoop$",
		"-test.count=1",
		"-test.timeout=2s",
	}
	if tc.ignored {
		args = append([]string{"sh", "-c", "trap '' INT; exec \"$@\"", "sh"}, args...)
	}
	helper := exec.Command(args[0], args[1:]...) //nolint:gosec // G204: this test binary
	helper.Env = append(os.Environ(), specHelperDirEnv+"="+dir, specHelperScriptEnv+"="+tc.script)
	if tc.only {
		helper.Env = append(helper.Env, specHelperSignalsEnv+"=1")
	}
	helper.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { killSpecSession(helper.Process.Pid) })
	done := make(chan error, 1)
	go func() { done <- helper.Wait() }()
	return helper, done
}

// signalSpecHelper sends sig to helper as it would reach a test: SIGKILL
// to the test binary alone, as an out-of-memory killer sends it, and any
// other signal to its process group, as a terminal sends Ctrl-C. Signal 0
// sends nothing.
func signalSpecHelper(helper *exec.Cmd, sig syscall.Signal) error {
	switch sig {
	case 0:
		return nil
	case syscall.SIGKILL:
		return syscall.Kill(helper.Process.Pid, sig) //nolint:wrapcheck // the test reports it as is
	}
	return syscall.Kill(-helper.Process.Pid, sig) //nolint:wrapcheck // the test reports it as is
}

// waitForPidFile waits for the spec binary of a helper to write its
// process ID to pidFile. It fails the test at once if the helper, whose
// Wait's result done receives, ends first, and after 30s otherwise.
func waitForPidFile(t *testing.T, pidFile string, done <-chan error) {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		data, err := os.ReadFile(pidFile) //nolint:gosec // G304: a file the test made
		if err == nil && strings.TrimSpace(string(data)) != "" {
			return
		}
		select {
		case err := <-done:
			t.Fatalf("the test binary ended before its spec binary started: %v", err)
		case <-deadline:
			t.Fatal("the spec binary did not start within 30s")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// awaitSpecHelper waits up to 10s for helper, whose Wait's result done
// receives, to end, and kills it and fails the test if it does not.
func awaitSpecHelper(t *testing.T, helper *exec.Cmd, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		_ = helper.Process.Kill()
		<-done //nolint:errcheck // killed, it errs; the test fails either way
		t.Fatal("the test binary did not end within 10s")
	}
}

// assertSpecHelperEnd checks that helper, ended, ended as sig ends a
// process, or, for signal 0, as a test timeout's panic does, with status
// 2: the handler of watchSpecSignals passes the signal on.
func assertSpecHelperEnd(t *testing.T, helper *exec.Cmd, sig syscall.Signal) {
	t.Helper()
	ws, ok := helper.ProcessState.Sys().(syscall.WaitStatus)
	switch {
	case !ok:
		t.Fatalf("no wait status: %v", helper.ProcessState)
	case sig != 0 && (!ws.Signaled() || ws.Signal() != sig):
		t.Errorf("the test binary ended with %v, want killed by %s", helper.ProcessState, sig)
	case sig == 0 && ws.ExitStatus() != 2:
		t.Errorf("the test binary ended with %v, want status 2", helper.ProcessState)
	}
}

// specSessionProcesses returns the IDs of the processes of the session
// sid, from /proc, the ones that have ended but are not yet reaped among
// them.
func specSessionProcesses(sid int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat") //nolint:gosec // G304: a /proc path
		if err != nil {
			continue
		}
		// After the command, in parentheses: state, parent, group, session.
		i := strings.LastIndexByte(string(b), ')')
		f := strings.Fields(string(b[i+1:]))
		if len(f) > 3 && f[3] == strconv.Itoa(sid) && f[0] != "Z" {
			pids = append(pids, pid)
		}
	}
	return pids
}

// killSpecSession kills every process of the session sid, which a helper
// leads, and goes on until none runs or 2s pass: a process may start
// another while it is being killed.
func killSpecSession(sid int) {
	for range 200 {
		pids := specSessionProcesses(sid)
		if len(pids) == 0 {
			return
		}
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGKILL) // ESRCH, already gone, is no matter
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSpecHelperDiesFirst checks the cleanup of a helper whose 2s test
// timeout ends it before its binary has written a process ID: the binary,
// run without the parent-death signal, outlives the helper, as on macOS,
// and killing the helper's session ends it all the same. Run in a
// directory with a space in its name, it also checks the binary writes
// nothing outside it.
func TestSpecHelperDiesFirst(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a test binary")
	}
	before := specPackageFiles(t)
	dir := filepath.Join(t.TempDir(), "t sp")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	helper, done := startSpecHelper(t, dir, specDiesCase{script: "late", only: true})
	awaitSpecHelper(t, helper, done)
	assertSpecHelperEnd(t, helper, 0)
	if left := specSessionProcesses(helper.Process.Pid); len(left) == 0 {
		t.Error("nothing of the helper's session outlived it: the test checks no cleanup")
	}
	killSpecSession(helper.Process.Pid)
	if left := specSessionProcesses(helper.Process.Pid); len(left) != 0 {
		t.Errorf("processes %v of the helper's session still run", left)
	}
	if _, err := os.Stat(filepath.Join(dir, "pid")); err == nil {
		t.Error("the binary wrote its process ID before the helper's timeout")
	}
	if after := specPackageFiles(t); !slices.Equal(before, after) {
		t.Errorf("the package directory changed from %q to %q", before, after)
	}
}

// TestSpecHelperSpacedDir runs TestSpecTestDies's first case with the
// helper's directory named with a space, which a script splicing it in
// unquoted would miss, writing into the package directory instead and
// leaving its binary unkilled.
func TestSpecHelperSpacedDir(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a test binary")
	}
	before := specPackageFiles(t)
	dir := filepath.Join(t.TempDir(), "t sp")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	tc := specDiesCase{
		name:   "interrupt, signals only",
		script: "loop",
		sig:    syscall.SIGINT,
		only:   true,
	}
	checkSpecTestDiesIn(t, dir, tc)
	if after := specPackageFiles(t); !slices.Equal(before, after) {
		t.Errorf("the package directory changed from %q to %q", before, after)
	}
}

// specPackageFiles returns the names of the files of the package
// directory, where the tests run, sorted.
func specPackageFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
