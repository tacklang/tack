package porttest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
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
	// program, when not empty, is the directory below
	// testdata/build-probes whose files the build copies and whose
	// main.tck it builds, in place of the program that warns.
	program string
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
	// goRemoves says Go's build removes what the port's clang leaves,
	// which the port cannot, so the trees left are not compared.
	goRemoves bool
	// sameCache says the build cache, in the case's base directory, must
	// hold afterwards what Go's build left in its own, every file and
	// directory by its path, mode and bytes.
	sameCache bool
	// late says the port declines once clang has run, after the warning
	// and what clang wrote, so the Go compiler runs the case too, and the
	// port's output must be Go's before its last error, then the decline
	// (see lateDeclineMismatch).
	late bool
}

// The reasons the port's declines give, after --out.
const (
	declineTempNotMade    = " with a temp directory that cannot be made"
	declineTempNotRemoved = " with a temp directory that cannot be removed"
	declineRuntimeDir     = " with a runtime directory that cannot be made"
	declineNoCwd          = " in a current directory that cannot be found"
	declineBadObject      = " with an object file that cannot be read"
	declineCacheNotWrite  = " with a build cache that cannot be written"
)

// buildDeclines are TestBuildDeclines' builds, each of a program that
// warns but the last, so that a warning written before the decline is
// seen: a TMPDIR that is missing and one that is a file, where the
// temporary directory cannot be made; a TMPDIR so long that the
// runtime's directories below it cannot be made, whose Go error names
// the temporary directory; a clang that writes the binary and leaves in
// the temporary directory what the port cannot remove; a file and a
// dangling symbolic link at the --out path before a build whose clang
// leaves that too but writes no binary, which a decline leaves as they
// were; and a current directory that is gone, where Go's message names
// an empty --out path; and a clang that writes no object where it
// compiles a file, which the port cannot read, where Go's error is in
// debug/elf's words, building a clang that leaves several things that
// cannot be removed, at several depths, beside what can; a program that
// links objects of the runtime, which writes nothing before, and one
// with a C file of its own and a warning, which writes the warning
// first; a build cache whose objects or keys directory no one may write
// to, and one Go filled whose every key, or every object, was then made
// a directory, which Go's rename cannot replace, where Go's cache.put
// fails naming a temporary file of a random name; one Go filled whose
// every key then names an ELF32, a big-endian or a Mach-O object
// defining foo, which Go reads, failing on foo defined twice, and the
// port's ELF reader does not take; and a C file clang rejects, whose
// clang leaves in the temporary directory what cannot be removed, where
// Go's error is two joined, the compile's and the removal's, over two
// lines; and one that leaves a path past PATH_MAX, which Go's RemoveAll
// removes and the port's cannot, after diagnostics, which Go writes
// alone.
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
		name: "temp-not-removable", reason: declineTempNotRemoved, leaves: true, late: true,
		env: fakeClangFirst("clang-locks"),
	},
	{
		name: "out-file-before", reason: declineTempNotRemoved, keepsOut: true, leaves: true, late: true,
		env: fakeClangFirst("clang-locks-only"),
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
		name: "out-link-before", reason: declineTempNotRemoved, keepsOut: true, leaves: true, late: true,
		env: fakeClangFirst("clang-locks-only"),
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
	{
		name: "object-unreadable", reason: declineBadObject, program: "lists",
		env: fakeClangFirst("clang-junk-object"),
	},
	{
		name: "c-object-unreadable", reason: declineBadObject, program: "missing-def", late: true,
		env: fakeClangFirst("clang-junk-object"),
	},
	{
		name: "cache-objects-unwritable", reason: declineCacheNotWrite, program: "missing-def", late: true,
		setup: readOnlyCacheDir("objects"),
	},
	{
		name: "cache-keys-unwritable", reason: declineCacheNotWrite, program: "missing-def", late: true,
		setup: readOnlyCacheDir("keys"),
	},
	{
		name: "cache-key-is-directory", reason: declineCacheNotWrite, program: "missing-def", late: true,
		sameCache: true, setup: filledCache(entriesDirs("keys")),
	},
	{
		name: "cache-object-is-directory", reason: declineCacheNotWrite, program: "missing-def", late: true,
		sameCache: true, setup: filledCache(entriesDirs("objects")),
	},
	{
		name: "cache-hit-elf32", reason: declineBadObject, program: "missing-def", late: true,
		sameCache: true, setup: filledCache(foreignObjects("i386-linux-gnu")),
	},
	{
		name: "cache-hit-big-endian", reason: declineBadObject, program: "missing-def", late: true,
		sameCache: true, setup: filledCache(foreignObjects("powerpc64-linux-gnu")),
	},
	{
		name: "cache-hit-macho", reason: declineBadObject, program: "missing-def", late: true,
		sameCache: true, setup: filledCache(foreignObjects("x86_64-apple-macos11")),
	},
	{
		name: "temp-many-unremovable", reason: declineTempNotRemoved, leaves: true, late: true,
		env: fakeClangFirst("clang-locks-many"),
	},
	{
		name: "compile-fails-not-removable", reason: declineTempNotRemoved, program: "bad-c",
		leaves: true, late: true, env: fakeLeaves("locked"),
	},
	{
		name: "temp-path-too-long", reason: declineTempNotRemoved, program: "missing-def",
		leaves: true, goRemoves: true, late: true, env: fakeLeaves("deep"),
	},
}

// fakeLeaves returns an env for buildDecline that puts the fake clang of
// testdata/build-probes/clang-leaves-compile before the PATH the test
// inherits, leaving in the temporary directory what shape names (see
// the fake).
func fakeLeaves(shape string) func(base, work string) []string {
	return func(base, work string) []string {
		return append(fakeClangFirst("clang-leaves-compile")(base, work), "FAKE_LEAVES="+shape)
	}
}

// readOnlyCacheDir returns a setup for buildDecline that makes the build
// cache's std/keys and std/objects, in the case's base directory, where
// prepareBuildDecline puts TACK_CACHE, and leaves the one named sub with
// mode 0555, so that no file can be written in it: Go's cache.put fails
// there, naming a temporary file of a random name, once the first of the
// runtime's C files is compiled.
func readOnlyCacheDir(sub string) func(t *testing.T, base, work string) {
	return func(t *testing.T, base, _ string) {
		t.Helper()
		std := filepath.Join(base, "cache", "std")
		for _, d := range []string{"keys", "objects"} {
			if err := os.MkdirAll(filepath.Join(std, d), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Chmod(filepath.Join(std, sub), 0o555); err != nil { //nolint:gosec // G302: a test's own directory
			t.Fatal(err)
		}
	}
}

// filledCache returns a setup for buildDecline that fills the build
// cache, in the case's base directory, where prepareBuildDecline puts
// TACK_CACHE, with Go's build of testdata/build-probes/lists, which
// must succeed, through runCompiler as every compiler run of the
// harness, and then applies damage to the cache's std directory. The
// keys depend on the runtime's C files, the -O level and the toolchain
// alone, so they are those of the case's own program's build.
func filledCache(damage func(t *testing.T, std string)) func(t *testing.T, base, work string) {
	return func(t *testing.T, base, _ string) {
		t.Helper()
		cache := filepath.Join(base, "cache")
		tmp := filepath.Join(base, "fill-tmp")
		if err := fresh(tmp); err != nil {
			t.Fatal(err)
		}
		goRun, err := goStackWrapper()
		if err != nil {
			t.Fatal(err)
		}
		in := input{name: "fill", dir: fakeClangDir("lists"), file: "main.tck", env: []string{"TMPDIR=" + tmp}}
		r := runCompiler(t, goRun, cache, in, []string{"--out", filepath.Join(tmp, "prog"), in.file})
		if r.code != 0 {
			t.Fatalf("Go's build to fill the cache: exit %d, stderr %q", r.code, r.stderr)
		}
		damage(t, filepath.Join(cache, "std"))
		if err := os.RemoveAll(tmp); err != nil {
			t.Fatal(err)
		}
	}
}

// entriesDirs puts an empty directory in place of every file of the
// cache's std/<sub>, a key or an object that is a directory: each is a
// miss, and Go's cache.put, which cannot rename a file onto a
// directory, fails naming its temporary file, of a random name, once
// the first of the runtime's C files is compiled again.
func entriesDirs(sub string) func(t *testing.T, std string) {
	return eachEntry(sub, func(t *testing.T, p string) {
		t.Helper()
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(p, 0o750); err != nil {
			t.Fatal(err)
		}
	})
}

// foreignObjects returns a damage that points every key of the cache at
// an object clang compiles for target, from a C file defining foo: one
// the Go compiler's readSymbols reads, an ELF32, a big-endian ELF or a
// Mach-O object, and the port's ELF reader does not take. Go then links the cached object
// for every runtime file, and fails on foo defined twice; the port
// cannot tell what Go reads there, so it declines.
func foreignObjects(target string) func(t *testing.T, std string) {
	return func(t *testing.T, std string) {
		t.Helper()
		dir := t.TempDir()
		src, obj := filepath.Join(dir, "foo.c"), filepath.Join(dir, "foo.o")
		if err := os.WriteFile(src, []byte("int foo(void) { return 1; }\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), compilerTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, "clang", "--target="+target, "-c", "-o", obj, src) //nolint:gosec // G204: the harness's own object
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		outlived, err := runInGroup(cmd)
		failEnd(t, endOf(ctx.Err(), err, outlived, false), "compiling a "+target+" object", cmd, err,
			outlived, compilerTimeout, stderr.Bytes())
		if cmd.ProcessState.ExitCode() != 0 {
			t.Fatalf("clang --target=%s: exit %d: %s", target, cmd.ProcessState.ExitCode(), stderr.Bytes())
		}
		data, err := os.ReadFile(obj) //nolint:gosec // G304: the object just compiled
		if err != nil {
			t.Fatal(err)
		}
		sum := fmt.Sprintf("%x", sha256.Sum256(data))
		if err := os.WriteFile(filepath.Join(std, "objects", sum+".o"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		eachEntry("keys", func(t *testing.T, p string) {
			t.Helper()
			rewrite(t, p, func([]byte) []byte { return []byte(sum + "\n") })
		})(t, std)
	}
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

// TestBuildDeclines runs the port's build of testdata/build-probes/warn,
// or of the program a case names, in each of buildDeclines, where the port cannot word what Go writes,
// and checks it declines as a case of a build stage it declines must to
// skip: exit status 3 and the not implemented line, giving the case's
// reason, last on standard error; for a decline before clang runs, that
// line alone, nothing on standard output and no warning; for one after,
// a late one, the Go compiler's build of the case run too, and the
// port's output Go's before its last error, the warning among it, then
// the line; nothing left at the --out path, or what was there left as
// it was, and nothing in TMPDIR but what a clang that leaves it left
// there.
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
	line := "tack: not implemented in the port: --out" + c.reason + "\n"
	switch {
	case c.late:
		port := declineHidden(result{stdout: stdout.Bytes(), stderr: stderr.Bytes(),
			code: cmd.ProcessState.ExitCode()}, d)
		if got, ok := port.lateDecline(); !ok || got != line {
			t.Errorf("standard error %q, want it to end in %q, after what came before", stderr.Bytes(), line)
		} else {
			checkLateAgainstGo(t, c, prog, d, port, line)
		}
	case stderr.String() != line:
		t.Errorf("standard error %q, want %q", stderr.Bytes(), line)
	case stdout.Len() > 0:
		t.Errorf("standard output %q, want nothing", stdout.Bytes())
	}
	checkDeclineLeft(t, c, d)
	if c.after != nil {
		c.after(t, d)
	}
}

// checkLateAgainstGo runs Go's build of c, prog its target, and fails t
// unless port, the port's late decline in d with the line line, wrote
// what Go wrote before its final error, and, for a case whose clang
// leaves what cannot be removed, left in TMPDIR what Go's left.
func checkLateAgainstGo(t *testing.T, c buildDecline, prog []byte, d declineDirs, port result, line string) {
	t.Helper()
	goRes, goDirs := goDeclineRun(t, c, prog)
	if why := lateDeclineMismatch(goRes, port, line); why != "" {
		t.Errorf("the port declines after writing what Go does not: %s", why)
	}
	if c.leaves && !c.goRemoves {
		checkSameLeft(t, d, goDirs)
	}
	if c.sameCache {
		port := cacheSnapshot(t, filepath.Join(filepath.Dir(d.tmp), "cache"))
		goCache := cacheSnapshot(t, filepath.Join(filepath.Dir(goDirs.tmp), "cache"))
		if diff := filesDiff(goCache, port); diff != "" {
			t.Errorf("the build caches differ:%s", diff)
		}
	}
}

// goDeclineRun runs the Go compiler on c's build, prog its target, in
// directories of its own laid out as the port's are, and returns what
// it wrote, hidden as declineHidden says, and the directories.
func goDeclineRun(t *testing.T, c buildDecline, prog []byte) (result, declineDirs) {
	t.Helper()
	d := prepareBuildDecline(t, c, prog)
	goRun, err := goStackWrapper()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), compilerTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, goRun, "--out", d.out, d.target) //nolint:gosec // G204: the Go compiler
	cmd.Dir, cmd.Env = d.work, d.vars
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	outlived, err := runInGroup(cmd)
	failEnd(t, endOf(ctx.Err(), err, outlived, false), "running Go's build", cmd, err,
		outlived, compilerTimeout, stderr.Bytes())
	return declineHidden(result{stdout: stdout.Bytes(), stderr: stderr.Bytes(),
		code: cmd.ProcessState.ExitCode()}, d), d
}

// checkSameLeft fails t unless the port's build in d left in its TMPDIR
// what Go's in goDirs left in its own: every file and directory, at
// every depth, by its path below TMPDIR, the build directory's name
// tk-*, each made removable first (see makeRemovable).
func checkSameLeft(t *testing.T, d, goDirs declineDirs) {
	t.Helper()
	port, goLeft := leftTree(t, d.tmp), leftTree(t, goDirs.tmp)
	if !slices.Equal(port, goLeft) {
		t.Errorf("the port leaves in TMPDIR %q, Go %q", port, goLeft)
	}
}

// leftTree returns every path below tmp, a build's TMPDIR, made
// removable first, relative to it, the first component tk-*, the build
// directory's name hidden, and a directory's with a slash after it, in
// the order filepath.WalkDir gives them.
func leftTree(t *testing.T, tmp string) []string {
	t.Helper()
	makeRemovable(t, tmp)
	var out []string
	err := filepath.WalkDir(tmp, func(p string, e fs.DirEntry, err error) error {
		if err != nil || p == tmp {
			return err
		}
		rel, err := filepath.Rel(tmp, p)
		if err != nil {
			return fmt.Errorf("naming %s: %w", p, err)
		}
		parts := strings.SplitN(filepath.ToSlash(rel), "/", 2)
		if strings.HasPrefix(parts[0], "tk-") {
			parts[0] = "tk-*"
		}
		name := strings.Join(parts, "/")
		if e.IsDir() {
			name += "/"
		}
		out = append(out, name)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// declineHidden returns r, the output of a build in d, with the
// characters after tk- of the build directory's name hidden, as
// tempNamesHidden hides them, and d's base directory, which holds every
// path the build is given, written BASE, so that two builds in two
// such directories compare.
func declineHidden(r result, d declineDirs) result {
	r = tempNamesHidden(r, d.tmp)
	base := []byte(filepath.Dir(d.tmp))
	r.stdout = bytes.ReplaceAll(r.stdout, base, []byte("BASE"))
	r.stderr = bytes.ReplaceAll(r.stderr, base, []byte("BASE"))
	return r
}

// declineDirs are the paths of one build of TestBuildDeclines: the
// current directory, the target, the --out path, the TMPDIR and the
// variables the port runs with.
type declineDirs struct {
	work, target, out, tmp string
	vars                   []string
}

// prepareBuildDecline makes the directories of c's build below a
// temporary directory of t's, prog its target, or the files of c's
// program when it names one, runs c's setup and
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
	if c.program != "" {
		if err := copyTree(fakeClangDir(c.program), src); err != nil {
			t.Fatal(err)
		}
	} else if err := os.WriteFile(d.target, prog, 0o644); err != nil { //nolint:gosec // G306: a source file
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
		// A path past PATH_MAX cannot be walked by path; t's own
		// removal, relative to each directory, removes it.
		if errors.Is(err, syscall.ENAMETOOLONG) {
			return filepath.SkipDir
		}
		if err != nil {
			return err
		}
		if d.IsDir() {
			err := os.Chmod(p, 0o700) //nolint:gosec // G302: a test's own directory
			if errors.Is(err, syscall.ENAMETOOLONG) {
				return filepath.SkipDir
			}
			return err
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}

// TestBuildRemovalReported builds testdata/build-probes/missing-def,
// whose extern func no C file defines, with a clang that leaves in the
// temporary directory what cannot be removed, and checks the port
// reports both, where Go drops the removal's error (see the divergence
// in the port's driver, build.tck's report_removal): exit status 1,
// nothing on standard output, and on standard error Go's, the warning
// and the diagnostic, then the line failed to delete temp directory:
// with the error Go's os.RemoveAll gives for the same tree, made here
// and removed with it (see removalReported); and that it leaves in
// TMPDIR what Go's build leaves. Its clangs leave a directory no one may
// list, holding a file, with TMPDIR absolute and relative; one that may
// be listed but not searched, holding a file; a read-only directory
// holding a symbolic link; and an empty directory no one may list,
// which can be removed, beside the first. These builds are no cases of
// a stage: the clang leaves what the harness must make removable first.
// It runs only as TestPort does.
func TestBuildRemovalReported(t *testing.T) {
	ready(t)
	for _, rc := range removalReported {
		t.Run(rc.name, func(t *testing.T) {
			t.Parallel()
			runRemovalReported(t, rc)
		})
	}
}

// removalCase is a build of TestBuildRemovalReported: its name, what
// testdata/build-probes/clang-leaves-compile leaves (see the fake),
// whether TMPDIR is relative, ../tmp from the build's current
// directory, and leave, which makes what the fake leaves in a directory
// standing for the temporary one, so that os.RemoveAll can give its
// error.
type removalCase struct {
	leave  func(t *testing.T, dir string)
	name   string
	shape  string
	relTmp bool
}

// removalReported are TestBuildRemovalReported's builds.
var removalReported = []removalCase{
	{name: "locked", shape: "locked", leave: leaveLocked},
	{name: "locked-relative-tmpdir", shape: "locked", relTmp: true, leave: leaveLocked},
	{name: "unsearchable", shape: "unsearchable", leave: func(t *testing.T, dir string) {
		t.Helper()
		mustLeave(t, dir, "b", "f1", 0o600)
	}},
	{name: "link", shape: "link", leave: func(t *testing.T, dir string) {
		t.Helper()
		ro := filepath.Join(dir, "ro")
		if err := os.Mkdir(ro, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("nowhere", filepath.Join(ro, "ln")); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(ro, 0o500); err != nil {
			t.Fatal(err)
		}
	}},
	{name: "empty", shape: "empty", leave: func(t *testing.T, dir string) {
		t.Helper()
		if err := os.Mkdir(filepath.Join(dir, "empty"), 0); err != nil {
			t.Fatal(err)
		}
		leaveLocked(t, dir)
	}},
}

// leaveLocked makes, in dir, the directory locked, holding a file, that
// no one may list.
func leaveLocked(t *testing.T, dir string) {
	t.Helper()
	mustLeave(t, dir, "locked", "file", 0)
}

// mustLeave makes, in dir, the directory sub holding the empty file
// file, and gives the directory mode, as a fake clang does.
func mustLeave(t *testing.T, dir, sub, file string, mode os.FileMode) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(dir, sub), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sub, file), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, sub), mode); err != nil {
		t.Fatal(err)
	}
}

// goRemoveAllError returns the error Go's os.RemoveAll gives for a
// temporary directory holding what rc's clang leaves, a program.c beside
// it, with the directory written BASE/tmp/tk-*, as declineHidden hides
// the port's.
func goRemoveAllError(t *testing.T, rc removalCase) string {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "tk-x")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "program.c"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	rc.leave(t, dir)
	err := os.RemoveAll(dir)
	makeRemovable(t, base)
	if err == nil {
		t.Fatal("os.RemoveAll removed what the clang leaves")
	}
	return strings.ReplaceAll(err.Error(), dir, "BASE/tmp/tk-*")
}

// runRemovalReported runs rc's build with the port and with Go and
// checks them as TestBuildRemovalReported says.
func runRemovalReported(t *testing.T, rc removalCase) {
	t.Helper()
	c := buildDecline{name: "removal-reported", program: "missing-def", leaves: true,
		env: func(base, work string) []string {
			vars := fakeLeaves(rc.shape)(base, work)
			if rc.relTmp {
				vars = append(vars, "TMPDIR=../tmp")
			}
			return vars
		}}
	d := prepareBuildDecline(t, c, nil)
	ctx, cancel := context.WithTimeout(t.Context(), compilerTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, env.portRun, "--out", d.out, d.target) //nolint:gosec // G204: the port
	cmd.Dir, cmd.Env = d.work, d.vars
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	outlived, err := runInGroup(cmd)
	failEnd(t, endOf(ctx.Err(), err, outlived, false), "running the port's build", cmd, err,
		outlived, compilerTimeout, stderr.Bytes())
	port := declineHidden(result{stdout: stdout.Bytes(), stderr: stderr.Bytes(),
		code: cmd.ProcessState.ExitCode()}, d)
	goRun, goDirs := goDeclineRun(t, c, nil)
	want := string(goRun.stderr) + "tack: failed to delete temp directory: " + goRemoveAllError(t, rc) + "\n"
	switch {
	case goRun.code != 1 || len(goRun.stdout) > 0:
		t.Errorf("Go's build exited %d, writing %q, want 1 and nothing", goRun.code, goRun.stdout)
	case port.code != 1:
		t.Errorf("exit status %d, want 1", port.code)
	case len(port.stdout) > 0:
		t.Errorf("standard output %q, want nothing", port.stdout)
	case string(port.stderr) != want:
		t.Errorf("standard error differs: %s", firstDiff([]byte(want), port.stderr))
	}
	if !strings.Contains(string(goRun.stderr), "has no C definition") {
		t.Errorf("Go's standard error %q holds no diagnostic of the definitions check", goRun.stderr)
	}
	checkDeclineLeft(t, c, d)
	checkSameLeft(t, d, goDirs)
}
