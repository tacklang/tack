package porttest

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildDecline is one build TestBuildDeclines runs the port on, which it
// must decline, saying why.
type buildDecline struct {
	// env returns the variables the build adds, given the case's base
	// directory and its work directory, the build's current directory.
	env func(base, work string) []string
	// setup makes what the build needs in base or work first.
	setup func(t *testing.T, base, work string)
	// after checks what the build left, beyond what every case checks.
	after func(t *testing.T, d declineDirs)
	// name names the case; reason is what its not implemented line says
	// after --out.
	name, reason string
	// leaves says its clang leaves, in TMPDIR, what the port cannot
	// remove.
	leaves bool
	// keepsOut says something is at the --out path before the build,
	// which a declined build must leave, so after checks it rather than
	// that nothing is there.
	keepsOut bool
	// cwdGone runs the build from a current directory removed before it
	// starts, with --out relative to it, prog, so that nothing it can
	// find takes it.
	cwdGone bool
}

// The reasons the port's declines give, after --out.
const (
	declineTempNotMade    = " with a temp directory that cannot be made"
	declineTempNotRemoved = " with a temp directory that cannot be removed"
	declineRuntimeDir     = " with a runtime directory that cannot be made"
	declineRelativeClang  = " with a clang on a PATH element that is not absolute"
	declineClangKilled    = " with a clang killed by a signal"
	declineClangNotRun    = " with a clang that cannot be started"
	declineNoCwd          = " in a current directory that cannot be found"
)

// buildDeclines are TestBuildDeclines' builds, each of a program that
// warns, so that a warning written before the decline is seen: a TMPDIR
// that is missing and one that is a file, where the temporary directory
// cannot be made; a TMPDIR so long that the runtime's directories below
// it cannot be made, whose Go error names the temporary directory; a
// clang on a relative element of PATH, which Go runs or skips by its
// mode; a clang killed by a signal; a first clang on PATH whose
// interpreter is missing, one with no #! line and one that is not
// executable, each before a real one, which the port must not run in
// its place; a clang that writes the binary and leaves in the temporary
// directory what the port cannot remove; a file and a dangling symbolic
// link at the --out path before the build, which a decline leaves as
// they were; and a current directory that is gone, where Go's message
// names an empty --out path.
var buildDeclines = []buildDecline{
	{name: "tmpdir-missing", reason: declineTempNotMade, env: func(base, _ string) []string {
		return []string{"TMPDIR=" + filepath.Join(base, "missing")}
	}},
	{
		name: "tmpdir-file", reason: declineTempNotMade,
		env: func(base, _ string) []string {
			return []string{"TMPDIR=" + filepath.Join(base, "file")}
		},
		setup: func(t *testing.T, base, _ string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(base, "file"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	},
	{
		name: "tmpdir-too-long", reason: declineRuntimeDir,
		env: func(base, _ string) []string { return []string{"TMPDIR=" + longTmp(base)} },
		setup: func(t *testing.T, base, _ string) {
			t.Helper()
			if err := os.MkdirAll(longTmp(base), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		after: func(t *testing.T, d declineDirs) {
			t.Helper()
			if left := entries(t, longTmp(filepath.Dir(d.tmp))); len(left) > 0 {
				t.Errorf("the long TMPDIR holds %q, want nothing", left)
			}
		},
	},
	{
		name: "clang-on-relative-path", reason: declineRelativeClang,
		env: func(_, _ string) []string {
			return []string{"PATH=rel" + string(filepath.ListSeparator) + os.Getenv("PATH")}
		},
		setup: func(t *testing.T, _, work string) {
			t.Helper()
			if err := copyTree(fakeClangDir("clang-fails"), filepath.Join(work, "rel")); err != nil {
				t.Fatal(err)
			}
		},
	},
	{name: "clang-killed", reason: declineClangKilled, env: fakeClangFirst("clang-killed")},
	{name: "clang-bad-interpreter", reason: declineClangNotRun, env: fakeClangFirst("clang-bad-interpreter")},
	{name: "clang-no-shebang", reason: declineClangNotRun, env: fakeClangFirst("clang-no-shebang")},
	{name: "clang-not-executable", reason: declineClangNotRun, env: fakeClangFirst("clang-not-executable")},
	{
		name: "temp-not-removable", reason: declineTempNotRemoved, leaves: true,
		env: fakeClangFirst("clang-locks"),
	},
	{
		name: "out-file-before", reason: declineClangKilled, keepsOut: true,
		env: fakeClangFirst("clang-killed"),
		setup: func(t *testing.T, base, _ string) {
			t.Helper()
			if err := os.WriteFile(outOf(base), []byte("before\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		after: func(t *testing.T, d declineDirs) {
			t.Helper()
			data, err := os.ReadFile(d.out)
			if err != nil || string(data) != "before\n" {
				t.Errorf("the --out file holds %q (%v), want what it held before", data, err)
			}
		},
	},
	{
		name: "out-link-before", reason: declineClangKilled, keepsOut: true,
		env: fakeClangFirst("clang-killed"),
		setup: func(t *testing.T, base, _ string) {
			t.Helper()
			if err := os.Symlink(filepath.Join(base, "nowhere"), outOf(base)); err != nil {
				t.Fatal(err)
			}
		},
		after: func(t *testing.T, d declineDirs) {
			t.Helper()
			to, err := os.Readlink(d.out)
			if err != nil || to != filepath.Join(filepath.Dir(d.tmp), "nowhere") {
				t.Errorf("the --out link points to %q (%v), want where it pointed before", to, err)
			}
		},
	},
	{name: "cwd-gone", reason: declineNoCwd, cwdGone: true},
}

// outOf returns the --out path of the build of TestBuildDeclines whose
// base directory is base, but for cwd-gone's.
func outOf(base string) string { return filepath.Join(base, "out", "prog") }

// longTmp returns a TMPDIR below base whose path is 4068 bytes long:
// make_temp_dir can make its directory there, but not the runtime's
// directories below that, whose paths pass PATH_MAX, 4096 bytes with
// the 0 that ends them.
func longTmp(base string) string {
	p := filepath.Join(base, "long")
	for len(p) < 4068 {
		n := min(4068-len(p)-1, 200)
		if n < 1 {
			n = 1
		}
		p = filepath.Join(p, strings.Repeat("d", n))
	}
	return p
}

// fakeClangDir returns the directory of testdata/build-probes named name,
// which holds a fake clang.
func fakeClangDir(name string) string {
	return filepath.Join(env.root, "porttest", "testdata", "build-probes", name)
}

// fakeClangFirst returns an env for buildDecline that puts the fake clang
// of the directory named name before the PATH the test inherits, which
// the fake's own commands, and a real clang, are found on.
func fakeClangFirst(name string) func(base, work string) []string {
	return func(_, _ string) []string {
		return []string{"PATH=" + fakeClangDir(name) + string(filepath.ListSeparator) +
			os.Getenv("PATH")}
	}
}

// TestBuildDeclines runs the port's build of testdata/build-probes/warn
// in each of buildDeclines, where the port cannot word what Go writes,
// and checks it declines cleanly, as a case of a build stage it declines
// must to skip: exit status 3, the one not implemented line, giving the
// case's reason, on standard error, nothing on standard output, no
// warning, nothing left at the --out path, or what was there left as it
// was, and nothing in TMPDIR but what a clang that leaves it left there.
// These are no cases of build-probes, which is required, and one leaves
// what the harness must make removable first. Only the port runs: Go
// words each of these. It runs only as TestPort does.
func TestBuildDeclines(t *testing.T) {
	ready(t)
	prog, err := os.ReadFile(filepath.Join(fakeClangDir("warn"), "main.tck"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range buildDeclines {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			runBuildDecline(t, c, prog)
		})
	}
}

// runBuildDecline runs c, the program prog its target, and checks the
// port declines it as TestBuildDeclines says.
func runBuildDecline(t *testing.T, c buildDecline, prog []byte) {
	t.Helper()
	d := prepareBuildDecline(t, c, prog)
	ctx, cancel := context.WithTimeout(t.Context(), compilerTimeout)
	defer cancel()
	var cmd *exec.Cmd
	if c.cwdGone {
		// The shell leaves work, its current directory, removed; the
		// relative --out path then has nothing to be taken from. The port
		// runs itself, not through its stack wrapper, a shell that would
		// complain of the missing directory before it runs the port.
		cmd = exec.CommandContext(ctx, "sh", "-c", `rmdir "$1" && shift && exec "$@"`, "sh",
			d.work, env.portBin, "--out", filepath.Base(d.out), d.target)
	} else {
		cmd = exec.CommandContext(ctx, env.portRun, "--out", d.out, d.target) //nolint:gosec // G204: the port
	}
	cmd.Dir, cmd.Env = d.work, d.vars
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	outlived, err := runInGroup(cmd)
	failEnd(t, endOf(ctx.Err(), err, outlived, false), "running the port's build", cmd, err,
		outlived, compilerTimeout, stderr.Bytes())
	if code := cmd.ProcessState.ExitCode(); code != 3 {
		t.Errorf("exit status %d, want 3", code)
	}
	if want := "tack: not implemented in the port: --out" + c.reason + "\n"; stderr.String() != want {
		t.Errorf("standard error %q, want %q", stderr.Bytes(), want)
	}
	if stdout.Len() > 0 {
		t.Errorf("standard output %q, want nothing", stdout.Bytes())
	}
	checkDeclineLeft(t, c, d)
	if c.after != nil {
		c.after(t, d)
	}
}

// declineDirs are the paths of one build of TestBuildDeclines: the
// current directory, the target, the --out path, the TMPDIR and the
// variables the port runs with.
type declineDirs struct {
	work, target, out, tmp string
	vars                   []string
}

// prepareBuildDecline makes the directories of c's build below a
// temporary directory of t's, prog its target, runs c's setup and
// returns them, with the variables it runs with. cwd-gone's --out path
// is prog in its current directory, the one the build is given.
func prepareBuildDecline(t *testing.T, c buildDecline, prog []byte) declineDirs {
	t.Helper()
	base := t.TempDir()
	src := filepath.Join(base, "src")
	d := declineDirs{
		work: filepath.Join(base, "work"), target: filepath.Join(src, "main.tck"),
		out: outOf(base), tmp: filepath.Join(base, "tmp"),
	}
	if c.cwdGone {
		d.out = filepath.Join(d.work, "prog")
	}
	for _, dir := range []string{d.work, src, d.tmp, filepath.Dir(d.out)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Whatever a clang left that no one may list is made removable again
	// before t's directory is removed.
	t.Cleanup(func() { makeRemovable(t, d.tmp) })
	if err := os.WriteFile(d.target, prog, 0o644); err != nil { //nolint:gosec // G306: a source file
		t.Fatal(err)
	}
	if c.setup != nil {
		c.setup(t, base, d.work)
	}
	d.vars = append(compilerEnv(filepath.Join(base, "cache")), "TMPDIR="+d.tmp)
	if c.env != nil {
		d.vars = append(d.vars, c.env(base, d.work)...)
	}
	return d
}

// checkDeclineLeft fails t when c's declined build, in d, left anything
// at its --out path, but for a case that had something there before,
// or in its TMPDIR, but the one directory a clang that leaves what
// cannot be removed left there.
func checkDeclineLeft(t *testing.T, c buildDecline, d declineDirs) {
	t.Helper()
	if _, err := os.Lstat(d.out); !c.keepsOut && !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the --out path is there after the decline (%v)", err)
	}
	left := entries(t, d.tmp)
	switch {
	case c.leaves && len(left) != 1:
		t.Errorf("TMPDIR holds %q, want the one directory the port could not remove", left)
	case !c.leaves && len(left) > 0:
		t.Errorf("TMPDIR holds %q, want nothing", left)
	}
}

// makeRemovable gives its owner every permission on each directory at
// or below root, each before it is listed, so that it can be listed and
// removed.
func makeRemovable(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.Chmod(p, 0o700) //nolint:gosec // G302: a test's own directory
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}
