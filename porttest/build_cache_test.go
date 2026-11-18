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
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

// The build cache's stages, build-warm and build-cache (see the package
// doc, Build cache stages).
const (
	buildWarmStage  = "build-warm"
	buildCacheStage = "build-cache"
)

// init adds the build cache's stages to stages: build-warm builds each
// input of build-examples cold and then warm, the two compilers on each
// other's caches (see runWarmCase), and build-cache builds each of
// cacheProbes, a damaged cache, a build that must miss or two builds at
// once (see runCacheProbe).
func init() {
	stages = append(stages,
		stage{
			name:   buildWarmStage,
			args:   func(in input) []string { return buildArgs(buildOutShown, in) },
			inputs: buildExampleInputs,
			stdout: compareBytes,
			run:    runWarmCase,
		},
		stage{
			name:   buildCacheStage,
			args:   func(in input) []string { return []string{"--out", buildOutShown, in.file} },
			inputs: cacheProbeInputs,
			stdout: compareBytes,
			run:    runCacheProbe,
		},
	)
}

// clangLogVar names the file the counting clang appends the name of each
// object of the runtime's it compiles to, and clangVersionVar, when set
// and not empty, a line it writes after the real clang's --version
// output, so that the toolchain a key hashes differs.
const (
	clangLogVar     = "PORTTEST_CLANG_LOG"
	clangVersionVar = "PORTTEST_CLANG_VERSION"
)

// countingClang is the counting clang, written once a run (see
// countingClangDir).
var countingClang struct {
	err  error
	dir  string
	once sync.Once
}

// countingClangDir returns the directory holding the counting clang, a
// shell script named clang, written once a run in the run's temporary
// directory: it runs the real clang, the one first on the PATH the test
// inherits, by its absolute path, with the arguments it is given, and
// first appends to $PORTTEST_CLANG_LOG the base name of the -o argument
// of a compile, a command with -c, whose object is a runtime's,
// std-<name>.o, which only compileStd's commands write. Given --version
// with $PORTTEST_CLANG_VERSION set, it writes that line after the real
// clang's output. The build cache's stages put it first on PATH for
// both compilers, so that a build that compiles a runtime object it
// should have found in the cache is seen to.
func countingClangDir() (string, error) {
	countingClang.once.Do(func() {
		clang, err := exec.LookPath("clang")
		if err != nil {
			countingClang.err = fmt.Errorf("finding clang for the counting clang: %w", err)
			return
		}
		if clang, err = filepath.Abs(clang); err != nil {
			countingClang.err = fmt.Errorf("finding clang for the counting clang: %w", err)
			return
		}
		dir := filepath.Join(env.tmp, "counting-clang")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			countingClang.err = fmt.Errorf("writing the counting clang: %w", err)
			return
		}
		script := fmt.Sprintf(`#!/bin/sh
c=; o=; prev=
for a; do
    [ "$a" = -c ] && c=1
    [ "$prev" = -o ] && o=$a
    prev=$a
done
if [ -n "$c" ] && [ -n "$%[1]s" ]; then
    case $o in
    */std-*.o) printf '%%s\n' "${o##*/}" >> "$%[1]s" || exit 125 ;;
    esac
fi
if [ "$1" = --version ] && [ -n "$%[2]s" ]; then
    %[3]q --version || exit
    printf '%%s\n' "$%[2]s"
    exit 0
fi
exec %[3]q "$@"
`, clangLogVar, clangVersionVar, clang)
		//nolint:gosec // G306: a script the builds run
		if err := os.WriteFile(filepath.Join(dir, "clang"), []byte(script), 0o755); err != nil {
			countingClang.err = fmt.Errorf("writing the counting clang: %w", err)
			return
		}
		countingClang.dir = dir
	})
	return countingClang.dir, countingClang.err
}

// cacheBuilt is how one build of a build cache stage ended: its result,
// temporary names hidden, the SHA-256 of the binary it wrote and its
// mode, or "" when it wrote none, and the runtime's objects it compiled,
// in order, as the counting clang logged them.
type cacheBuilt struct {
	compiled []string
	sum      string
	res      result
	mode     os.FileMode
}

// cacheJob is one build of a build cache stage, ready to run: the
// command and where the counting clang logs, with d's directories.
type cacheJob struct {
	cmd    *exec.Cmd
	ctx    context.Context //nolint:containedctx // the build's own deadline, which finish reads
	cancel context.CancelFunc
	d      buildDirs
	log    string
	who    string
	stdout bytes.Buffer
	stderr bytes.Buffer
}

// prepareCacheBuild makes d's work, TMPDIR and --out directories anew,
// leaving its cache as it is, and returns the build of in with bin,
// args its command line, through the counting clang, with TACK_CACHE
// d.cache, TACK_HOME d.home, TMPDIR d.tmp and vars after every other
// variable.
func prepareCacheBuild(t *testing.T, bin string, d buildDirs, in input, args []string, vars ...string) *cacheJob {
	t.Helper()
	if err := os.RemoveAll(d.work); err != nil {
		t.Fatal(err)
	}
	if err := fresh(d.tmp, filepath.Dir(d.out)); err != nil {
		t.Fatal(err)
	}
	copied, err := buildCopy(in, d.work)
	if err != nil {
		t.Fatal(err)
	}
	clang, err := countingClangDir()
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(filepath.Dir(d.work), "clang.log")
	if err := os.Remove(log); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	who := "the port"
	if isGo(bin) {
		who = "Go"
	}
	ctx, cancel := context.WithTimeout(t.Context(), compilerTimeout)
	j := &cacheJob{ctx: ctx, cancel: cancel, d: d, log: log, who: who}
	j.cmd = exec.CommandContext(ctx, bin, args...) //nolint:gosec // G204: a compiler under test
	j.cmd.Dir = copied.dir
	j.cmd.Env = append(append(compilerEnv(d.cache), copied.env...),
		"TMPDIR="+d.tmp, "TACK_HOME="+d.home,
		"PATH="+clang+string(filepath.ListSeparator)+os.Getenv("PATH"), clangLogVar+"="+log)
	j.cmd.Env = append(j.cmd.Env, vars...)
	j.cmd.Stdout, j.cmd.Stderr = &j.stdout, &j.stderr
	return j
}

// run runs j's build, in a process group of its own (see runInGroup),
// and returns what finish needs; it may run on any goroutine.
func (j *cacheJob) run() (bool, error) {
	return runInGroup(j.cmd)
}

// finish fails t when j's build, which run ran and which ended with
// outlived and err, ended in a way no compiler's answer does, and
// returns how it ended: for a decline, the result alone; otherwise its
// result with temporary names hidden, the binary's SHA-256 and mode,
// the binary removed, and what it compiled. It fails t when TMPDIR is
// not empty afterwards, when a failed build left its --out path and
// when one that exited 0 did not.
func (j *cacheJob) finish(t *testing.T, outlived bool, err error) cacheBuilt {
	t.Helper()
	defer j.cancel()
	ctx := j.ctx
	running := fmt.Sprintf("running %s in %s", strings.Join(j.cmd.Args, " "), j.cmd.Dir)
	failEnd(t, endOf(ctx.Err(), err, outlived, false), running, j.cmd, err, outlived,
		compilerTimeout, j.stderr.Bytes())
	r := result{stdout: j.stdout.Bytes(), stderr: j.stderr.Bytes(), code: j.cmd.ProcessState.ExitCode()}
	if _, late := r.lateDecline(); r.earlyDecline() || late {
		return cacheBuilt{res: tempNamesHidden(r, j.d.tmp)}
	}
	if left := entries(t, j.d.tmp); len(left) > 0 {
		t.Errorf("%s's build left in TMPDIR: %s", j.who, strings.Join(left, ", "))
	}
	b := cacheBuilt{res: tempNamesHidden(r, j.d.tmp)}
	b.sum, b.mode = j.takeBinary(t, r.code)
	data, err := os.ReadFile(j.log)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	b.compiled = strings.Fields(string(data))
	return b
}

// takeBinary returns the SHA-256 and the mode of the binary j's build,
// which exited code, wrote at its --out path, and removes it, or "" when
// it wrote none, failing t when a failed build left the path and when
// one that exited 0 did not write it.
func (j *cacheJob) takeBinary(t *testing.T, code int) (string, os.FileMode) {
	t.Helper()
	info, err := os.Stat(j.d.out)
	switch {
	case errors.Is(err, fs.ErrNotExist) && code == 0:
		t.Errorf("%s's build exited 0 and wrote nothing at its --out path", j.who)
		return "", 0
	case errors.Is(err, fs.ErrNotExist):
		return "", 0
	case err != nil:
		t.Fatal(err)
	case code != 0:
		t.Errorf("%s's build failed, exit %d, and left its --out path", j.who, code)
		return "", 0
	}
	sum := fileSHA256(t, j.d.out)
	if err := os.Remove(j.d.out); err != nil {
		t.Fatal(err)
	}
	return sum, info.Mode()
}

// cacheBuild runs one build of a build cache stage: in built with bin,
// args its command line, in d's directories, the cache d.cache as it
// is, through the counting clang, vars set after every other variable
// (see prepareCacheBuild and finish).
func cacheBuild(t *testing.T, bin string, d buildDirs, in input, args []string, vars ...string) cacheBuilt {
	t.Helper()
	j := prepareCacheBuild(t, bin, d, in, args, vars...)
	outlived, err := j.run()
	return j.finish(t, outlived, err)
}

// cacheSnapshot returns what the cache dir holds, as snapshot gives it,
// every file and directory by its path, its mode and a file's bytes, or
// nothing when dir is not there.
func cacheSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}
	}
	files, err := snapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// cacheKeys returns how many keys the cache dir holds.
func cacheKeys(t *testing.T, dir string) int {
	t.Helper()
	es, err := os.ReadDir(filepath.Join(dir, "std", "keys"))
	if errors.Is(err, fs.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(es)
}

// swapCache moves the cache at from to d.cache, where every build of a
// case runs with it, so that both compilers see one path, and returns a
// function that moves it back. A rename keeps every file's mode and
// bytes, which a copy need not.
func swapCache(t *testing.T, d buildDirs, from string) func() {
	t.Helper()
	if err := os.Rename(from, d.cache); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if err := os.Rename(d.cache, from); err != nil {
			t.Fatal(err)
		}
	}
}

// buildIn runs one build of in, args its command line, with bin in d,
// on the cache at cache, moved to d.cache for it, and returns how it
// ended and what the cache holds afterwards.
func buildIn(t *testing.T, bin string, d buildDirs, cache string, in input, args []string, vars ...string) (cacheBuilt, map[string]string) {
	t.Helper()
	back := swapCache(t, d, cache)
	b := cacheBuild(t, bin, d, in, args, vars...)
	snap := cacheSnapshot(t, d.cache)
	back()
	return b, snap
}

// sameBuild reports how port, the port's build at the step named step of
// the case in of the stage st, differs from want, Go's: a decline skips,
// or fails, as a case of st declining does (see declined), a late one
// only after what it wrote is held to the protocol; otherwise the
// driver's output and exit status, as compareResults compares them, the
// binary, by its SHA-256, and its mode, and the runtime's objects it
// compiled. The caches are compared by the caller.
func sameBuild(t *testing.T, st stage, in input, step string, args []string, want, port cacheBuilt) {
	t.Helper()
	if port.res.earlyDecline() {
		t.Logf("step %s", step)
		declined(t, st, in, port.res)
	}
	if line, late := port.res.lateDecline(); late {
		t.Logf("step %s", step)
		if why := lateDeclineMismatch(want.res, port.res, line); why != "" {
			t.Fatalf("the port declines after writing what Go does not: %s", why)
		}
		declined(t, st, in, result{stderr: []byte(line), code: notImplementedCode})
	}
	failed := t.Failed()
	compareResults(t, st, in, args, want.res, port.res)
	if port.sum != want.sum {
		t.Errorf("binary: port's sha256 %q, Go's %q", port.sum, want.sum)
	}
	if port.mode != want.mode {
		t.Errorf("binary mode: port's %v, Go's %v", port.mode, want.mode)
	}
	if !slices.Equal(port.compiled, want.compiled) {
		t.Errorf("runtime objects compiled: port %q, Go %q", port.compiled, want.compiled)
	}
	if !failed && t.Failed() {
		t.Logf("at step %s", step)
	}
}

// sameGo fails t when got, a build of Go's at the step named step, did
// not give what want, Go's build of the same case, gave: the driver's
// output and exit status, byte for byte, and the binary.
func sameGo(t *testing.T, step string, want, got cacheBuilt) {
	t.Helper()
	if got.res.code != want.res.code {
		t.Errorf("step %s: Go's exit status %d, before %d", step, got.res.code, want.res.code)
	}
	if d := firstDiff(want.res.stderr, got.res.stderr); d != "" {
		t.Errorf("step %s: Go's standard error differs: %s", step, d)
	}
	if d := firstDiff(want.res.stdout, got.res.stdout); d != "" {
		t.Errorf("step %s: Go's standard output differs: %s", step, d)
	}
	if got.sum != want.sum {
		t.Errorf("step %s: Go's binary sha256 %q, before %q", step, got.sum, want.sum)
	}
}

// sameCache fails t when port, what the port's build left in its cache
// at the step named step, is not want, what Go's left in its own.
func sameCache(t *testing.T, step string, want, port map[string]string) {
	t.Helper()
	if diff := filesDiff(want, port); diff != "" {
		t.Errorf("step %s: the caches differ:%s", step, diff)
	}
}

// warm fails t when b, a build at the step named step on a warm cache,
// compiled any of the runtime's objects, or when after, what the cache
// held after it, is not before, what it held before: a hit writes
// nothing to the cache.
func warm(t *testing.T, step string, b cacheBuilt, before, after map[string]string) {
	t.Helper()
	if len(b.compiled) > 0 {
		t.Errorf("step %s: a build on a warm cache compiled %q", step, b.compiled)
	}
	if diff := filesDiff(before, after); diff != "" {
		t.Errorf("step %s: a build on a warm cache changed it:%s", step, diff)
	}
}

// runWarmCase runs the case in of build-warm, st, with portBin as the
// port, each build in the same directories, at the same paths: Go
// builds it on an empty cache, A, and the port on another, B, and the
// two caches must then hold the same files, by path, mode and bytes;
// then Go builds it again on A, and the port on A, Go on B and the port
// on B, each of which must give what Go's build on A gave, the driver's
// output, exit status and binary, compile none of the runtime's objects,
// as the counting clang logs them, and leave its cache as it was. A
// program that needs no object of the runtime leaves both caches empty.
// Its cases are build-examples', built as that stage builds them, so the
// port's output is compared with Go's through build-examples' entries of
// the divergence list, which record what the port writes in place of
// Go's; a step of Go's is compared with Go's, byte for byte.
func runWarmCase(t *testing.T, st stage, in input, portBin string) {
	t.Helper()
	in.suite = buildExamplesStage
	base := t.TempDir()
	d := newBuildDirs(base)
	readOnlyHome(t, d.home)
	goRun, err := goStackWrapper()
	if err != nil {
		t.Fatal(err)
	}
	a, b := filepath.Join(base, "cache-a"), filepath.Join(base, "cache-b")
	if err := fresh(a, b); err != nil {
		t.Fatal(err)
	}
	args := buildArgs(d.out, in)
	portCold, portB := buildIn(t, portBin, d, b, in, args)
	goCold, goA := buildIn(t, goRun, d, a, in, args)
	sameBuild(t, st, in, "cold", args, goCold, portCold)
	sameCache(t, "cold", goA, portB)
	goWarm, goA2 := buildIn(t, goRun, d, a, in, args)
	warm(t, "Go on A", goWarm, goA, goA2)
	sameGo(t, "Go on A", goCold, goWarm)
	for _, step := range []struct {
		snap  map[string]string
		name  string
		bin   string
		cache string
	}{
		{goA, "port on A", portBin, a},
		{portB, "Go on B", goRun, b},
		{portB, "port on B", portBin, b},
	} {
		got, after := buildIn(t, step.bin, d, step.cache, in, args)
		if step.bin == portBin {
			sameBuild(t, st, in, step.name, args, goWarm, got)
		} else {
			sameGo(t, step.name, goWarm, got)
		}
		warm(t, step.name, got, step.snap, after)
	}
}

// cacheProbe is one case of build-cache, on the program of
// testdata/build-probes named program, built from its directory: kind
// says what the case does, with damage, for a damaged cache, applied to
// the cache's std directory.
type cacheProbe struct {
	damage  func(t *testing.T, std string)
	program string
	kind    cacheProbeKind
}

// cacheProbeKind is what a case of build-cache does.
type cacheProbeKind int

const (
	// probeDamaged builds on a cache Go filled, damaged (see
	// runDamagedProbe).
	probeDamaged cacheProbeKind = iota
	// probeMissOpt builds at the default level and then at -O2 (see
	// runMissProbe).
	probeMissOpt
	// probeMissToolchain builds with clang and then with a clang whose
	// --version output differs (see runMissProbe).
	probeMissToolchain
	// probeConcurrent runs two of the port's builds at once on one empty
	// cache (see runConcurrentProbe).
	probeConcurrent
)

// cacheProbes are build-cache's cases, by name, each on the program that
// links objects of the runtime and has no C file of its own, lists:
// every key of a cache Go filled cut short, in upper case, of bytes that
// are no hex, empty, unreadable, naming no object, or naming bytes that
// hash right but are no object, and every object with a byte changed or
// unreadable, each a miss that is compiled again and put back, the
// cache repaired; every other key gone, a build of hits and misses; a
// build at -O2 and one with a clang whose --version
// differs, after one at the default level with clang, which must miss;
// and two of the port's builds at once on one empty cache. A key or an
// object that is a directory, which Go's put cannot replace, failing
// with an error naming its temporary file, is TestBuildDeclines' (see
// buildDeclines), and so is a key naming an object Go reads and the
// port's ELF reader does not take, ELF32, big-endian or Mach-O.
var cacheProbes = map[string]cacheProbe{
	"build-cache/key-truncated": {program: "lists", damage: eachEntry("keys", func(t *testing.T, p string) {
		rewrite(t, p, func(b []byte) []byte { return b[:len(b)/2] })
	})},
	"build-cache/key-no-newline": {program: "lists", damage: eachEntry("keys", func(t *testing.T, p string) {
		rewrite(t, p, func(b []byte) []byte { return bytes.TrimSuffix(b, []byte("\n")) })
	})},
	"build-cache/key-upper": {program: "lists", damage: eachEntry("keys", func(t *testing.T, p string) {
		rewrite(t, p, bytes.ToUpper)
	})},
	"build-cache/key-not-hex": {program: "lists", damage: eachEntry("keys", func(t *testing.T, p string) {
		rewrite(t, p, func([]byte) []byte { return []byte(strings.Repeat("g", 64) + "\n") })
	})},
	"build-cache/key-names-path": {program: "lists", damage: eachEntry("keys", func(t *testing.T, p string) {
		rewrite(t, p, func([]byte) []byte { return []byte("../../../../../../../../etc/passwd\n") })
	})},
	"build-cache/key-empty": {program: "lists", damage: eachEntry("keys", func(t *testing.T, p string) {
		rewrite(t, p, func([]byte) []byte { return nil })
	})},
	"build-cache/key-unreadable": {program: "lists", damage: eachEntry("keys", func(t *testing.T, p string) {
		if err := os.Chmod(p, 0); err != nil {
			t.Fatal(err)
		}
	})},
	"build-cache/key-no-object": {program: "lists", damage: eachEntry("objects", func(t *testing.T, p string) {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	})},
	"build-cache/key-not-elf": {program: "lists", damage: notObjects},
	"build-cache/object-mismatch": {program: "lists", damage: eachEntry("objects", func(t *testing.T, p string) {
		rewrite(t, p, func(b []byte) []byte {
			b = slices.Clone(b)
			b[len(b)/2] ^= 0xff
			return b
		})
	})},
	"build-cache/object-unreadable": {program: "lists", damage: eachEntry("objects", func(t *testing.T, p string) {
		if err := os.Chmod(p, 0); err != nil {
			t.Fatal(err)
		}
	})},
	"build-cache/some-hits":      {program: "lists", damage: someKeysGone},
	"build-cache/miss-opt":       {program: "lists", kind: probeMissOpt},
	"build-cache/miss-toolchain": {program: "lists", kind: probeMissToolchain},
	"build-cache/concurrent":     {program: "lists", kind: probeConcurrent},
}

// eachEntry returns a damage for cacheProbe that applies damage to the
// path of every file in the cache's std/<sub>, in name order, failing
// when there is none.
func eachEntry(sub string, damage func(t *testing.T, p string)) func(t *testing.T, std string) {
	return func(t *testing.T, std string) {
		t.Helper()
		names := entries(t, filepath.Join(std, sub))
		if len(names) == 0 {
			t.Fatalf("the cache's %s is empty", sub)
		}
		slices.Sort(names)
		for _, n := range names {
			damage(t, filepath.Join(std, sub, n))
		}
	}
}

// rewrite replaces the bytes of the file at p with what change makes of
// them, keeping its mode.
func rewrite(t *testing.T, p string, change func([]byte) []byte) {
	t.Helper()
	data, err := os.ReadFile(p) //nolint:gosec // G304: a test's own cache
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, change(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// someKeysGone removes every other key of the cache at std, the first,
// the third and so on in name order, so a build hits on the objects of
// the keys left and compiles the rest, each a miss of its own: a build
// that mixes hits and misses.
func someKeysGone(t *testing.T, std string) {
	t.Helper()
	dir := filepath.Join(std, "keys")
	names := entries(t, dir)
	if len(names) < 2 {
		t.Fatalf("the cache holds %d keys, want two or more to keep some", len(names))
	}
	slices.Sort(names)
	for i, n := range names {
		if i%2 == 0 {
			if err := os.Remove(filepath.Join(dir, n)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// notObjects points every key of the cache at std at bytes that hash
// right but are no object, a line naming the key, written where the
// object of their hash belongs, as only something other than tack would
// cache them.
func notObjects(t *testing.T, std string) {
	t.Helper()
	eachEntry("keys", func(t *testing.T, p string) {
		t.Helper()
		junk := []byte("hashes right, links wrong: " + filepath.Base(p) + "\n")
		sum := fmt.Sprintf("%x", sha256.Sum256(junk))
		if err := os.WriteFile(filepath.Join(std, "objects", sum+".o"), junk, 0o600); err != nil {
			t.Fatal(err)
		}
		rewrite(t, p, func([]byte) []byte { return []byte(sum + "\n") })
	})(t, std)
}

// cacheProbeInputs returns build-cache's inputs, each of cacheProbes run
// from its program's directory with main.tck as its target, in the
// order of their names.
func cacheProbeInputs() ([]input, error) {
	dir := filepath.Join(env.root, "porttest", "testdata", "build-probes")
	names := slices.Sorted(func(yield func(string) bool) {
		for name := range cacheProbes {
			if !yield(name) {
				return
			}
		}
	})
	ins := make([]input, 0, len(names))
	for _, name := range names {
		in := input{name: name, dir: filepath.Join(dir, cacheProbes[name].program), file: "main.tck"}
		if !fileExists(filepath.Join(in.dir, in.file)) {
			return nil, fmt.Errorf("cache probe %s: %s has no main.tck", name, cacheProbes[name].program)
		}
		ins = append(ins, in)
	}
	return ins, nil
}

// runCacheProbe runs the case in of build-cache, st, with portBin as the
// port, as its kind says.
func runCacheProbe(t *testing.T, st stage, in input, portBin string) {
	t.Helper()
	p, ok := cacheProbes[in.name]
	if !ok {
		t.Fatalf("%s is no cache probe", in.name)
	}
	base := t.TempDir()
	d := newBuildDirs(base)
	readOnlyHome(t, d.home)
	goRun, err := goStackWrapper()
	if err != nil {
		t.Fatal(err)
	}
	switch p.kind {
	case probeDamaged:
		runDamagedProbe(t, st, in, p, d, goRun, portBin)
	case probeMissOpt:
		runMissProbe(t, st, in, d, goRun, portBin, []string{"-O2"}, nil)
	case probeMissToolchain:
		runMissProbe(t, st, in, d, goRun, portBin, nil, []string{clangVersionVar + "=a toolchain of another build"})
	case probeConcurrent:
		runConcurrentProbe(t, st, in, d, goRun, portBin)
	}
}

// runDamagedProbe runs p, a damaged cache's case in of st: Go fills two
// empty caches, each of which p's damage then damages; Go builds on one
// and the port on the other, which must give the same output and
// binary, compile the same objects of the runtime and leave the same
// cache; then each builds again on its own, which must compile nothing
// and leave its cache as it was, repaired.
func runDamagedProbe(t *testing.T, st stage, in input, p cacheProbe, d buildDirs, goRun, portBin string) {
	t.Helper()
	base := filepath.Dir(d.work)
	g, q := filepath.Join(base, "cache-go"), filepath.Join(base, "cache-port")
	if err := fresh(g, q); err != nil {
		t.Fatal(err)
	}
	args := []string{"--out", d.out, in.file}
	for _, c := range []string{g, q} {
		filled, _ := buildIn(t, goRun, d, c, in, args)
		if filled.res.code != 0 || cacheKeys(t, c) == 0 {
			t.Fatalf("Go's build to fill the cache: exit %d, %d keys, stderr %q",
				filled.res.code, cacheKeys(t, c), filled.res.stderr)
		}
		p.damage(t, filepath.Join(c, "std"))
	}
	t.Cleanup(func() {
		// An unreadable entry left by a damage is made readable for the
		// removal of the case's directory, which needs no more than that.
		makeRemovable(t, base)
	})
	goRes, goSnap := buildIn(t, goRun, d, g, in, args)
	if len(goRes.compiled) == 0 {
		t.Fatalf("Go's build on the damaged cache compiled nothing; the damage misses no entry")
	}
	portRes, portSnap := buildIn(t, portBin, d, q, in, args)
	sameBuild(t, st, in, "damaged", args, goRes, portRes)
	sameCache(t, "damaged", goSnap, portSnap)
	goAgain, goAfter := buildIn(t, goRun, d, g, in, args)
	warm(t, "Go repaired", goAgain, goSnap, goAfter)
	portAgain, portAfter := buildIn(t, portBin, d, q, in, args)
	sameBuild(t, st, in, "repaired", args, goAgain, portAgain)
	warm(t, "port repaired", portAgain, portSnap, portAfter)
}

// runMissProbe runs a case in of st whose second build must miss: each
// compiler, on a cache of its own, empty, builds in at the default level
// with clang, then with flags before --out and vars set, which changes
// every key, then so again; the second must compile every object the
// first did, adding as many keys again, and the third none, and the
// port's every build must give Go's output and binary, compile the
// objects Go's does, and leave the cache Go's leaves.
func runMissProbe(t *testing.T, st stage, in input, d buildDirs, goRun, portBin string, flags, vars []string) {
	t.Helper()
	base := filepath.Dir(d.work)
	g, q := filepath.Join(base, "cache-go"), filepath.Join(base, "cache-port")
	if err := fresh(g, q); err != nil {
		t.Fatal(err)
	}
	first := []string{"--out", d.out, in.file}
	changed := append(slices.Clone(flags), first...)
	steps := []struct {
		name string
		args []string
		vars []string
	}{
		{"first", first, nil},
		{"changed", changed, vars},
		{"changed again", changed, vars},
	}
	var goFirst []string
	for i, step := range steps {
		want, goSnap := buildIn(t, goRun, d, g, in, step.args, step.vars...)
		got, portSnap := buildIn(t, portBin, d, q, in, step.args, step.vars...)
		sameBuild(t, st, in, step.name, step.args, want, got)
		sameCache(t, step.name, goSnap, portSnap)
		switch i {
		case 0:
			goFirst = want.compiled
			if len(goFirst) == 0 {
				t.Fatalf("Go's first build compiled nothing")
			}
		case 1:
			if !slices.Equal(got.compiled, goFirst) {
				t.Errorf("the changed build compiled %q, want %q, a miss for each", got.compiled, goFirst)
			}
			if n := cacheKeys(t, q); n != 2*len(goFirst) {
				t.Errorf("the cache holds %d keys after the changed build, want %d", n, 2*len(goFirst))
			}
		case 2:
			if len(got.compiled) > 0 {
				t.Errorf("the build after the changed one compiled %q, want nothing", got.compiled)
			}
		}
	}
}

// cacheEntry matches the name of an entry of the cache's std directory,
// a key or an object, or one of its two directories: no temporary file.
var cacheEntry = regexp.MustCompile(`\A(?:keys/|objects/|keys/[0-9a-f]{64}|objects/[0-9a-f]{64}\.o)\z`)

// runConcurrentProbe runs the case in of st, two of the port's builds at
// once on one empty cache, each with a TMPDIR, a copy and an --out path
// of its own: each must give what Go's build on an empty cache of its
// own gives, the driver's output and binary, and the cache afterwards
// must be what Go's left, every file and directory by its path, mode and
// bytes, no temporary file among them.
func runConcurrentProbe(t *testing.T, st stage, in input, d buildDirs, goRun, portBin string) {
	t.Helper()
	base := filepath.Dir(d.work)
	g := filepath.Join(base, "cache-go")
	if err := fresh(g); err != nil {
		t.Fatal(err)
	}
	args := []string{"--out", d.out, in.file}
	want, goSnap := buildIn(t, goRun, d, g, in, args)
	second := newBuildDirs(filepath.Join(base, "second"))
	second.home, second.cache = d.home, d.cache
	jobs := []*cacheJob{
		prepareCacheBuild(t, portBin, d, in, args),
		prepareCacheBuild(t, portBin, second, in, []string{"--out", second.out, in.file}),
	}
	type ended struct {
		err      error
		outlived bool
	}
	ends := make([]ended, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Go(func() {
			outlived, err := j.run()
			ends[i] = ended{err: err, outlived: outlived}
		})
	}
	wg.Wait()
	for i, j := range jobs {
		got := j.finish(t, ends[i].outlived, ends[i].err)
		// Which objects each compiled depends on which found the other's
		// entries first.
		got.compiled = want.compiled
		sameBuild(t, st, in, fmt.Sprintf("concurrent %d", i), args, want, got)
	}
	snap := cacheSnapshot(t, d.cache)
	sameCache(t, "concurrent", goSnap, snap)
	for p := range snap {
		if rel, ok := strings.CutPrefix(p, "std/"); !ok || !cacheEntry.MatchString(rel) {
			if p != "std/" {
				t.Errorf("the cache holds %s, which is no entry", p)
			}
		}
	}
}
