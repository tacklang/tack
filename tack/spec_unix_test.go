//go:build unix

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// specOwnGroup makes cmd start in a process group of its own, so that
// killSpecGroup reaches every process it starts too, and, where the
// system has it, has it killed when the test process dies (see
// specParentDeath).
func specOwnGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	specParentDeath(cmd.SysProcAttr)
}

// killSpecGroup kills every process of the group cmd, started by
// specOwnGroup, leads, and cmd's process itself, should it have left the
// group.
func killSpecGroup(cmd *exec.Cmd) {
	// ESRCH, a group with no process left, is the one error possible here,
	// and leaves nothing to kill.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill() // it may have exited already, which is no matter
}

// specGroups holds the process groups of the spec binaries running now,
// so that a SIGINT or SIGTERM to the test process can kill them first.
var specGroups = struct {
	pids map[int]bool
	sync.Mutex
}{pids: make(map[int]bool)}

// specSignalsOnce starts the watch on SIGINT and SIGTERM once.
var specSignalsOnce sync.Once

// startSpecTracked starts cmd, a spec binary, and records its group as
// running, with the registry locked from before the start until after
// the record, so that a SIGINT or SIGTERM that arrives meanwhile is
// handled only once the group is recorded, and kills it. It starts the
// watch on those signals first, if no run has: the binary's own group
// puts it out of reach of a terminal's Ctrl-C, so the test process kills
// it itself before it ends.
func startSpecTracked(cmd *exec.Cmd) error {
	specSignalsOnce.Do(watchSpecSignals)
	specGroups.Lock()
	defer specGroups.Unlock()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting %s: %w", cmd.Path, err)
	}
	specGroups.pids[cmd.Process.Pid] = true
	return nil
}

// untrackSpecGroup records that the group of cmd has been killed.
func untrackSpecGroup(cmd *exec.Cmd) {
	specGroups.Lock()
	defer specGroups.Unlock()
	delete(specGroups.pids, cmd.Process.Pid)
}

// watchSpecSignals catches SIGINT and SIGTERM to the test process, from
// the first run on until the process ends, each unless signal.Ignored
// reports it ignored. That is Go's view, not the disposition the process
// inherited: the runtime keeps SIGINT ignored when the process inherits it
// so, as a background job of a non-interactive shell does, and Ignored
// then reports it; catching it would let it end the process, which it
// must not. SIGTERM inherited ignored is not kept so, Ignored reports it
// false, and the watcher catches it. On a signal caught, the
// watcher kills every recorded group, holding the registry so no binary
// starts or is recorded meanwhile, and then ends the process as the
// signal would have ended it, by resetting its handling and sending it
// again. Should the process outlive that for a second, it exits with
// status 2 rather than wait on with the registry held.
func watchSpecSignals() {
	var sigs []os.Signal
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		if !signal.Ignored(sig) {
			sigs = append(sigs, sig)
		}
	}
	if len(sigs) == 0 {
		return
	}
	c := make(chan os.Signal, 1)
	signal.Notify(c, sigs...)
	go func() {
		sig, ok := (<-c).(syscall.Signal)
		if !ok {
			return
		}
		specGroups.Lock()
		for pid := range specGroups.pids {
			_ = syscall.Kill(-pid, syscall.SIGKILL) // the process is ending; ESRCH is no matter
		}
		signal.Reset(sig)
		_ = syscall.Kill(os.Getpid(), sig) // its default action ends the process
		time.Sleep(time.Second)
		os.Exit(2)
	}()
}

// readSpecPid returns the process ID a test's script wrote to pidFile,
// and false where there is no /proc to look a process up in.
func readSpecPid(t *testing.T, pidFile string) (string, bool) {
	t.Helper()
	if _, err := os.Stat("/proc/self"); err != nil {
		return "", false
	}
	data, err := os.ReadFile(pidFile) //nolint:gosec // G304: a file the test's script wrote
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data)), true
}

// specProcessState returns the state of the process pid, from
// /proc/pid/stat, as "R", "S" or "Z", or "" when there is no such
// process.
func specProcessState(pid string) string {
	b, err := os.ReadFile("/proc/" + pid + "/stat") //nolint:gosec // G304: a path the test made
	if err != nil {
		return ""
	}
	// The state follows the command, which is in parentheses.
	if i := strings.LastIndexByte(string(b), ')'); i >= 0 && i+3 <= len(b) {
		return string(b[i+2 : i+3])
	}
	return "?"
}

// assertSpecProcessGone checks that the process whose ID pidFile holds,
// a binary runSpecBinary ran, has been killed and reaped within 1s: no
// process is left, not even a zombie, which only its parent, the test
// process, could reap. It does nothing where there is no /proc.
func assertSpecProcessGone(t *testing.T, pidFile string) {
	t.Helper()
	pid, ok := readSpecPid(t, pidFile)
	if !ok {
		return
	}
	state := "?"
	for range 100 {
		if state = specProcessState(pid); state == "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("the binary, process %s, is left in state %s", pid, state)
}

// assertSpecProcessDead checks that the process whose ID pidFile holds,
// one a binary started, runs no more within 2s: gone, or a zombie, which
// the process it was handed to when its parent died reaps when it will.
// Where it still runs, it is killed, so the test leaves nothing. It does
// nothing where there is no /proc.
func assertSpecProcessDead(t *testing.T, pidFile string) {
	t.Helper()
	pid, ok := readSpecPid(t, pidFile)
	if !ok {
		return
	}
	state := "?"
	for range 200 {
		if state = specProcessState(pid); state == "" || state == "Z" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	var n int
	if _, err := fmt.Sscan(pid, &n); err == nil {
		_ = syscall.Kill(n, syscall.SIGKILL) // the test fails either way
	}
	t.Errorf("process %s, which a binary started, still runs in state %s", pid, state)
}

// TestSpecGroupsUntracked checks that a run leaves no group tracked once
// it has ended, finished or timed out, so that a later SIGINT kills no
// group whose ID may since have been given to another.
func TestSpecGroupsUntracked(t *testing.T) {
	runSpecBinary(t.Context(), writeScript(t, "echo done\n"), time.Second)
	runSpecBinary(t.Context(), writeScript(t, "exec sleep 3\n"), 100*time.Millisecond)
	specGroups.Lock()
	defer specGroups.Unlock()
	if len(specGroups.pids) != 0 {
		t.Errorf("groups tracked after their runs: %v", specGroups.pids)
	}
}

// TestSpecRunBinaryLeaderLeftGroup checks that a binary that leaves its
// process group, which killing the group then misses, is still killed at
// its deadline: here a Python process that joins its parent's group and
// sleeps 6s, under a 300ms limit, must not hold the run for its 6s. It
// needs python3, and skips without it.
func TestSpecRunBinaryLeaderLeftGroup(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	bin := writeScript(t, "exec python3 -c 'import os,time; "+
		"os.setpgid(0, os.getpgid(os.getppid())); time.sleep(6)'\n")
	start := time.Now()
	r := runSpecBinary(t.Context(), bin, 300*time.Millisecond)
	if took := time.Since(start); !r.timedOut || took > 3*time.Second {
		t.Errorf("timed out %v after %s; want a timeout within 3s", r.timedOut, took)
	}
}

// TestSpecNoStartWhileHeld checks that no spec binary starts while the
// registry is held, as the watcher holds it while it kills every
// recorded group: a run begun then starts its binary only once the
// registry is released, so it is recorded before the watcher could miss
// it. The binary writes its process ID as it starts; 500ms with the
// registry held must pass without it.
func TestSpecNoStartWhileHeld(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	bin := writeScript(t, "echo $$ > "+shellQuote(pidFile)+"\n")
	specGroups.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runSpecBinary(t.Context(), bin, 5*time.Second)
	}()
	time.Sleep(500 * time.Millisecond)
	_, err := os.Stat(pidFile)
	specGroups.Unlock()
	<-done
	if err == nil {
		t.Error("a spec binary started while the registry was held")
	}
}

// TestSpecKilledBeforeUntracked checks that a run kills its binary's group
// before it stops recording it, so that the watcher, which kills only
// recorded groups, never misses one still running: with the registry
// held as a run ends, the process the binary left running must already
// be dead. The binary starts a sleep, writes its ID and ends after 1s;
// the registry is taken once the ID is written, and held 2s.
func TestSpecKilledBeforeUntracked(t *testing.T) {
	childFile := filepath.Join(t.TempDir(), "child")
	bin := writeScript(t, "sleep 30 >/dev/null 2>&1 &\necho $! > "+shellQuote(childFile)+
		"\nsleep 1\n")
	done := make(chan struct{})
	go func() {
		defer close(done)
		runSpecBinary(t.Context(), bin, 10*time.Second)
	}()
	waitForFile(t, childFile)
	specGroups.Lock()
	time.Sleep(2 * time.Second)
	pid, ok := readSpecPid(t, childFile)
	state := ""
	if ok {
		state = specProcessState(pid)
	}
	specGroups.Unlock()
	<-done
	if state != "" && state != "Z" {
		t.Errorf("the binary's sleep, process %s, still ran in state %s when the run "+
			"stopped recording its group", pid, state)
		assertSpecProcessDead(t, childFile) // kills it
	}
}

// waitForFile waits up to 5s for file to hold something.
func waitForFile(t *testing.T, file string) {
	t.Helper()
	for range 500 {
		data, err := os.ReadFile(file) //nolint:gosec // G304: the test's file
		if err == nil && strings.TrimSpace(string(data)) != "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s was not written within 5s", file)
}
