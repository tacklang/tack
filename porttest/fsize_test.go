package porttest

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fsizeBlocks is the file-size limit, RLIMIT_FSIZE, TestFileSizeLimit
// runs its programs and the port under, in the blocks sh's ulimit -f
// counts: 512 bytes in the sh porttest runs, bash in its POSIX mode, and
// 1024 in a shell that counts kilobytes. Either way it is below every
// write the test makes.
const fsizeBlocks = "1"

// fsizeTimeout is how long one run of a built program under the limit
// may take.
const fsizeTimeout = time.Minute

// fsizeWrite is the program TestFileSizeLimit builds to write past the
// limit: 64 KiB, far past it, to the file big, through std/os, and
// printing the error the write gives, or ok.
const fsizeWrite = `package main

import os from "std/os"

// main writes 64 KiB to the file big, past the file-size limit it is
// run under, and prints the error the write gives, or ok.
func main() {
    mut data := make(list[uint8], c_allocator)
    mut i int64 = 0
    for i < 65536 {
        e := append(mut data, 120)
        if e.code != 0 {
            print("out of memory\n")
            return
        }
        i += 1
    }
    e := os.write_file([]uint8("big"), data)
    if e.code != 0 {
        print(e.msg)
        print("\n")
        return
    }
    print("ok\n")
}
`

// fsizeChild is the program TestFileSizeLimit builds to start a child
// under the limit: sh, which execs yes with its standard output the
// file out, through std/os's run, printing how the child ended, and
// whether the signal that killed it was SIGXFSZ, by its number here.
var fsizeChild = `package main

import os from "std/os"
import text from "std/text"

// main runs sh -c 'exec yes > out', a child that writes until the
// file-size limit it inherits stops it, and prints how the child ended.
func main() {
    mut argv := make(list[text.Text], c_allocator)
    for _, p := range [3]string{"sh", "-c", "exec yes > out"} {
        t, e := text.from_bytes([]uint8(p), c_allocator)
        if e.code != 0 {
            print("out of memory\n")
            return
        }
        e2 := append(mut argv, t)
        if e2.code != 0 {
            print("out of memory\n")
            return
        }
    }
    mut out := make(list[uint8], c_allocator)
    mut err_out := make(list[uint8], c_allocator)
    s, e := os.run([]uint8("sh"), argv, mut out, mut err_out, c_allocator)
    if e.code != 0 {
        print(e.msg)
        print("\n")
        return
    }
    if s.exited() {
        print("exited\n")
        return
    }
    if s.signal() == ` + strconv.Itoa(int(syscall.SIGXFSZ)) + ` {
        print("killed by SIGXFSZ\n")
        return
    }
    print("killed by another signal\n")
}
`

// limitedRun is how a run under the file-size limit ended: what it
// wrote to its standard output and standard error, both pipes, which
// the limit does not apply to, and its wait status.
type limitedRun struct {
	stdout, stderr []byte
	status         syscall.WaitStatus
}

// TestFileSizeLimit checks the runtime's handling of SIGXFSZ, which the
// port's runtime ignores, as it does SIGPIPE, so a write past the
// process's file-size limit, RLIMIT_FSIZE, fails with EFBIG rather than
// kill the process (per the owner's ruling, 2026-11-15). The frozen Go
// compiler's runtime does not ignore it, so this is a divergence of the
// port's runtime from the frozen one, and is recorded here, not in
// divergencesFile, whose entries are cases of a stage's runs of the two
// compilers. Each run is under sh's ulimit -f fsizeBlocks, with ulimit
// -c 0, SIGXFSZ's default action dumping core. It checks:
//
//   - write: fsizeWrite built by the port and by the reference Go
//     compiler, which compiles the port's runtime, prints std/os's
//     error for EFBIG, File too large, and exits 0, having written some
//     of the bytes but not all; built by the frozen Go compiler, it
//     dies of SIGXFSZ, the divergence.
//   - child: fsizeChild built by the port prints that the child died of
//     SIGXFSZ: std/os's run gives every child SIGXFSZ's default action,
//     as it gives SIGPIPE's, an ignored signal staying ignored across
//     exec, so yes there behaves as it does started from a shell.
//   - compiler: the port itself, as --emit writes fsizeWrite's C to its
//     standard output, a file under the limit, is not killed, and
//     leaves the same bytes in the file as the reference Go compiler,
//     whose Go runtime ignores SIGXFSZ too, and ends as Go does, exit 1
//     with "failed to write output" on standard error, byte for byte:
//     it writes its output through std/os's write_stdout, which
//     reports the failure (per the owner's ruling, 2026-11-18). Every
//     other output the driver reports a failed write of is checked by
//     TestOutputWriteFailure.
//
// It runs only as TestPort does, with TACK_PORT_TEST=1 and -count.
func TestFileSizeLimit(t *testing.T) {
	ready(t)
	frozen, err := frozenGo()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("write", func(t *testing.T) {
		t.Parallel()
		for _, c := range []struct{ name, bin, cache string }{
			{"port", env.portRun, env.portCache},
			{"go", env.goBin, env.goCache},
		} {
			dir := fsizeBuild(t, c.bin, c.cache, fsizeWrite, nil)
			r := runLimited(t, dir, filepath.Join(dir, "prog"))
			if !r.status.Exited() || r.status.ExitStatus() != 0 ||
				string(r.stdout) != "File too large\n" || len(r.stderr) != 0 {
				t.Errorf("built by the %s: %s, want exit 0 and the standard output %q",
					c.name, r, "File too large\n")
			}
			info, err := os.Stat(filepath.Join(dir, "big"))
			if err != nil {
				t.Fatalf("built by the %s: %v", c.name, err)
			}
			if info.Size() <= 0 || info.Size() >= 65536 {
				t.Errorf("built by the %s: big holds %d bytes, want some of the 65536 but not all",
					c.name, info.Size())
			}
		}
		// The frozen compiler builds with this module's frozen std, the
		// one it was frozen with, not the port's.
		dir := fsizeBuild(t, frozen, filepath.Join(t.TempDir(), "cache"), fsizeWrite,
			[]string{"TACK_HOME=" + env.root})
		r := runLimited(t, dir, filepath.Join(dir, "prog"))
		if !r.status.Signaled() || r.status.Signal() != syscall.SIGXFSZ {
			t.Errorf("built by the frozen Go compiler: %s, want death by SIGXFSZ, "+
				"the divergence the port's runtime makes", r)
		}
	})
	t.Run("child", func(t *testing.T) {
		t.Parallel()
		dir := fsizeBuild(t, env.portRun, env.portCache, fsizeChild, nil)
		r := runLimited(t, dir, filepath.Join(dir, "prog"))
		if !r.status.Exited() || r.status.ExitStatus() != 0 ||
			string(r.stdout) != "killed by SIGXFSZ\n" || len(r.stderr) != 0 {
			t.Errorf("%s, want exit 0 and the standard output %q", r, "killed by SIGXFSZ\n")
		}
	})
	t.Run("compiler", func(t *testing.T) {
		t.Parallel()
		var runs [2]limitedRun
		var outs [2][]byte
		for i, bin := range []string{env.goBin, env.portRun} {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.tck"), []byte(fsizeWrite), 0o600); err != nil {
				t.Fatal(err)
			}
			runs[i] = runLimited(t, dir, "sh", "-c", `exec "$0" --emit main.tck > out.c`, bin)
			var err error
			if outs[i], err = os.ReadFile(filepath.Join(dir, "out.c")); err != nil {
				t.Fatal(err)
			}
		}
		goRun, portRun := runs[0], runs[1]
		const goErr = "tack: failed to write output: write /dev/stdout: file too large\n"
		if !goRun.status.Exited() || goRun.status.ExitStatus() != 1 || string(goRun.stderr) != goErr {
			t.Fatalf("the Go compiler: %s, want exit 1 and the standard error %q", goRun, goErr)
		}
		// The port writes --emit's output through std/os's write_stdout,
		// which reports the failure, so it answers as Go does, byte for
		// byte (per the owner's ruling, 2026-11-18).
		if portRun.status != goRun.status || !bytes.Equal(portRun.stdout, goRun.stdout) ||
			!bytes.Equal(portRun.stderr, goRun.stderr) {
			t.Errorf("the port: %s, want the Go compiler's %s", portRun, goRun)
		}
		if !bytes.Equal(outs[1], outs[0]) {
			t.Errorf("the port wrote %d bytes to its standard output, the Go compiler %d, "+
				"or other bytes", len(outs[1]), len(outs[0]))
		}
	})
}

// fsizeBuild writes src as main.tck in a new directory of t's, builds
// it there with the compiler bin, whose TACK_CACHE is cache, into prog,
// with vars after the compilers' environment, and returns the
// directory. A build that fails, or that the port declines, fails t.
func fsizeBuild(t *testing.T, bin, cache, src string, vars []string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.tck"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	in := input{name: "main.tck", dir: dir, file: "main.tck", env: vars}
	if r := runCompiler(t, bin, cache, in, []string{"--out", "prog", "main.tck"}); r.code != 0 {
		t.Fatalf("building with %s: exit %d\n%s%s", bin, r.code, r.stdout, r.stderr)
	}
	return dir
}

// runLimited runs prog with args in dir, in the compilers' environment
// (see compilerEnv), under sh's ulimit -f fsizeBlocks and ulimit -c 0,
// sh then exec'ing prog, so the wait status is prog's own, and returns
// how it ended. A run past fsizeTimeout, or one that cannot be started,
// fails t.
func runLimited(t *testing.T, dir, prog string, args ...string) limitedRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), fsizeTimeout)
	defer cancel()
	script := "ulimit -c 0 && ulimit -f " + fsizeBlocks + ` && exec "$0" "$@"`
	cmd := exec.CommandContext(ctx, "sh", append([]string{"-c", script, prog}, args...)...)
	cmd.Dir = dir
	cmd.Env = compilerEnv(filepath.Join(t.TempDir(), "cache"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	outlived, err := runInGroup(cmd)
	running := "running " + prog + " " + strings.Join(args, " ") + " in " + dir
	failEnd(t, endOf(ctx.Err(), err, outlived, true), running, cmd, err, outlived,
		fsizeTimeout, stderr.Bytes())
	status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("%s: no wait status", running)
	}
	return limitedRun{stdout: stdout.Bytes(), stderr: stderr.Bytes(), status: status}
}

// String describes r for a failure: how it ended and what it wrote.
func (r limitedRun) String() string {
	end := "exit " + strconv.Itoa(r.status.ExitStatus())
	if r.status.Signaled() {
		end = "killed by " + r.status.Signal().String()
	}
	return end + ", standard output " + strconv.Quote(string(r.stdout)) +
		", standard error " + strconv.Quote(string(r.stderr))
}
