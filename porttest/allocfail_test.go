package porttest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// allocFailVar is the environment variable that asks for TestAllocFail:
// the most points of failure each run is swept at, a whole number from 1
// up. Unset or empty, TestAllocFail skips.
const allocFailVar = "PORTTEST_ALLOC_FAIL"

// allocFailPerStage is how many cases of each stage allocFailStages names
// TestAllocFail runs, spread evenly over the stage's cases.
const allocFailPerStage = 12

// allocFailStages are the stages whose cases TestAllocFail samples: every
// kind of input, single files and packages, and every flag the stages
// pass but --emit-header, whose runs write into the tree they are given
// (see runHeaderCase).
var allocFailStages = []string{
	"lex-single", "ast-single", "res-single", "lex-packages", "ast-packages", "res-packages",
	"check-decls", "check-core", "check-calls", "check-facts", "check-arith", "check-loops",
	"check-own", "check-generic", "check-backend", "check-clean", "probes",
	emitSingleStage, emitPackagesStage, emitLibStage, emitCasesStage,
}

// sanitizerExit is the exit status every sanitizer's report ends a run
// with in TestAllocFail, which sets it, so that a report cannot pass for
// the port's own status. The address, leak and undefined behavior
// sanitizers linked together share one exit status, the one the options
// read last set, UBSAN_OPTIONS', so both options are given the same one.
const sanitizerExit = 86

// allocInject is the C TestAllocFail links into the port: malloc and
// realloc wrapped, with the linker's --wrap, to count the calls the
// runtime's c_allocator makes, the only C that calls them, written at
// exit to the file $TACK_ALLOC_COUNT names; and, at start, the runtime's
// own hook, tack_c_allocator_fail_after_, given $TACK_FAIL_AFTER, so
// that c_allocator grants that many requests and refuses every one
// after them.
const allocInject = `#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

void tack_c_allocator_fail_after_(int64_t n);
void *__real_malloc(size_t n);
void *__real_realloc(void *p, size_t n);

static int64_t calls;

void *__wrap_malloc(size_t n) {
    calls++;
    return __real_malloc(n);
}

void *__wrap_realloc(void *p, size_t n) {
    calls++;
    return __real_realloc(p, n);
}

static void report(void) {
    const char *path = getenv("TACK_ALLOC_COUNT");
    if (path == 0 || *path == 0) {
        return;
    }
    char buf[32];
    int len = snprintf(buf, sizeof buf, "%lld\n", (long long)calls);
    int fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if (fd < 0) {
        return;
    }
    // A short write leaves the count unreadable, which fails the sweep.
    ssize_t wrote = write(fd, buf, (size_t)len);
    (void)wrote;
    close(fd);
}

__attribute__((constructor)) static void inject(void) {
    const char *n = getenv("TACK_FAIL_AFTER");
    if (n != 0 && *n != 0) {
        tack_c_allocator_fail_after_(strtoll(n, 0, 10));
    }
    atexit(report);
}
`

// sanitizeFlags are the flags make port-sanitize's clang and
// TestAllocFail's add to every compile and link of the port: the address
// sanitizer, with the leak sanitizer, and the undefined behavior
// sanitizer, each ending the run at its first report.
// -fno-sanitize-address-globals-dead-stripping keeps the address
// sanitizer from defining the same symbol in every object, which the Go
// compiler's check of the C files' symbols rejects as defined twice.
const sanitizeFlags = "-g -fno-omit-frame-pointer -fsanitize=address,undefined " +
	"-fno-sanitize-recover=all -fno-sanitize-address-globals-dead-stripping"

// TestAllocFail builds the port with the sanitizers and allocInject, and
// runs it on a sample of the corpus: allocFailPerStage cases of each of
// allocFailStages, every program of testdata/emit-probes with --emit and
// --check, the port's driver with --check and --emit, and builds of an
// example using std and one with C files of its own, each first with
// every allocation granted, counting them, and then failing each
// allocation from the Nth on, for N at most $PORTTEST_ALLOC_FAIL points
// spread evenly from 0 to the count, every one when there are no more,
// each run with a fresh build cache of its own, but for a build of the
// example using std on a cache Go's build of it warmed, which every run
// shares and none writes to.
// A run with every allocation granted must allocate and end with no
// sanitizer report; one that fails them must exit 1, with standard error
// ending in the driver's "tack: out of memory", and with no sanitizer
// report. It checks the C-level allocations and the out-of-memory paths:
// the port ends through exit, whose tack_allocators_release_all_ frees
// every buffer c_allocator holds before the leak sanitizer looks, so the
// leaks it can see are those of C memory the runtime does not track;
// that Tack code frees what it owns, the checker enforces statically.
// What a compiler writes when memory runs out is not part of
// the port's contract: Go's runtime ends the process with a fatal error
// of its own, which no output of the port's matches, so nothing is
// compared with Go. It builds the port from its source tree, with a clang
// of its own on PATH that adds the sanitizers and allocInject, and skips
// without the tree. It runs only as TestPort does, and only with
// $PORTTEST_ALLOC_FAIL set.
func TestAllocFail(t *testing.T) {
	ready(t)
	v := os.Getenv(allocFailVar)
	if v == "" {
		t.Skip("the allocation failure sweep runs only with " + allocFailVar +
			" set to the points per run")
	}
	points, err := strconv.Atoi(v)
	if err != nil || points < 1 {
		t.Fatalf("%s is %q, not a whole number from 1 up", allocFailVar, v)
	}
	src, _ := portSource(os.Getenv("TACK_PORT"))
	if !dirExists(filepath.Join(src, "driver")) {
		t.Skip("no port source tree: the sweep builds the port with its allocations counted")
	}
	bin := buildFailingPort(t, src)
	cases, err := allocFailCases(t, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var before map[string]string
			if c.warm != "" {
				before = cacheSnapshot(t, c.warm)
			}
			sweepAllocFail(t, bin, c, points)
			if c.warm != "" {
				if diff := filesDiff(before, cacheSnapshot(t, c.warm)); diff != "" {
					t.Errorf("a run on the warm cache changed it, so it missed:%s", diff)
				}
			}
		})
	}
}

// allocFailCase is one run TestAllocFail sweeps: its subtest's name, its
// input and its command line, and whether it is a build, which must
// succeed with every allocation granted.
type allocFailCase struct {
	args []string
	name string
	// warm, when not empty, is the build cache every run of the case
	// shares, which a build filled first and none may change: a run that
	// missed would put what it compiled there.
	warm  string
	in    input
	build bool
}

// allocFailCases returns what TestAllocFail sweeps: allocFailPerStage
// cases of each of allocFailStages, spread evenly over its cases, each
// with the stage's command line; every emit probe, written to a
// temporary directory, with --emit and with --check; and the driver of
// the port's source tree src with --check and --emit; and the builds
// allocFailBuilds gives.
func allocFailCases(t *testing.T, src string) ([]allocFailCase, error) {
	var cases []allocFailCase
	for _, name := range allocFailStages {
		st := stageNamed(name)
		if st == nil {
			return nil, fmt.Errorf("no %s stage", name)
		}
		ins, err := st.inputs()
		if err != nil {
			return nil, err
		}
		step := max(len(ins)/allocFailPerStage, 1)
		for i := 0; i < len(ins); i += step {
			cases = append(
				cases,
				allocFailCase{name: name + "/" + ins[i].name, in: ins[i], args: st.args(ins[i])},
			)
		}
	}
	srcs, err := emitProbeSources()
	if err != nil {
		return nil, err
	}
	dir := t.TempDir()
	for file, text := range srcs {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0o600); err != nil {
			return nil, fmt.Errorf("writing an emit probe: %w", err)
		}
		in := input{name: file, dir: dir, file: file}
		cases = append(
			cases,
			allocFailCase{
				name: "emit-probes/" + file + "/emit",
				in:   in,
				args: []string{"--emit", file},
			},
			allocFailCase{
				name: "emit-probes/" + file + "/check",
				in:   in,
				args: []string{"--check", file},
			},
		)
	}
	driver := input{name: "driver", dir: src, file: "./driver"}
	cases = append(
		cases,
		allocFailCase{
			name: "driver/check",
			in:   driver,
			args: []string{"--check", "--max-errors", "0", "./driver"},
		},
		allocFailCase{name: "driver/emit", in: driver, args: []string{"--emit", "./driver"}},
	)
	return append(cases, allocFailBuilds(t)...), nil
}

// allocFailBuilds returns the builds TestAllocFail sweeps: examples/conv,
// which uses std, so its build compiles std's C and links objects of the
// runtime, and examples/extern, which has C files of its own, each built
// from its main.tck, as a single file, with --out into a temporary directory of t's, with a TMPDIR of its
// own there, so that the temporary directories a build refused memory
// leaves, as the driver documents it may, are removed with t's; and
// examples/conv again on a warm cache, which Go's build of it filled
// first, so the runtime's objects are read from the cache, every run's
// a hit, a refused one included, which writes nothing there: the cache
// must hold the same files, by path, mode and bytes, after the sweep as
// before it, as it would not had any run missed and put an object.
func allocFailBuilds(t *testing.T) []allocFailCase {
	t.Helper()
	cases := make([]allocFailCase, 0, 3)
	for _, name := range []string{"conv", "extern"} {
		dir := t.TempDir()
		in := input{
			name: "examples/" + name,
			dir:  filepath.Join(env.root, "examples", name),
			file: "main.tck",
			env:  []string{"TMPDIR=" + dir},
		}
		cases = append(cases, allocFailCase{
			name:  "build/examples/" + name,
			in:    in,
			build: true,
			args:  []string{"--out", filepath.Join(dir, "prog"), "main.tck"},
		})
	}
	dir, cache := t.TempDir(), t.TempDir()
	in := input{
		name: "examples/conv",
		dir:  filepath.Join(env.root, "examples", "conv"),
		file: "main.tck",
		env:  []string{"TMPDIR=" + dir, "TACK_CACHE=" + cache},
	}
	args := []string{"--out", filepath.Join(dir, "prog"), "main.tck"}
	goRun, err := goStackWrapper()
	if err != nil {
		t.Fatal(err)
	}
	if r := runCompiler(t, goRun, cache, in, args); r.code != 0 {
		t.Fatalf("Go's build to warm the cache: exit %d, stderr %q", r.code, r.stderr)
	}
	if err := os.Remove(filepath.Join(dir, "prog")); err != nil {
		t.Fatal(err)
	}
	if len(cacheSnapshot(t, cache)) == 0 {
		t.Fatal("Go's build to warm the cache left it empty")
	}
	return append(cases, allocFailCase{name: "build/warm/examples/conv", in: in, build: true, args: args, warm: cache})
}

// buildFailingPort builds the port's driver, in the source tree src,
// with the Go compiler and the port's own std (see portBuildEnv), with
// a clang on PATH that adds sanitizeFlags to
// every command but --version and, to the link, allocInject and the
// wrapping of malloc and realloc, and returns the binary's path.
func buildFailingPort(t *testing.T, src string) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Fatalf("finding clang: %v", err)
	}
	dir := t.TempDir()
	inject := filepath.Join(dir, "inject.c")
	if err := os.WriteFile(inject, []byte(allocInject), 0o600); err != nil {
		t.Fatal(err)
	}
	// The link is the one command without -c.
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in --version) exec %[1]q "$@";; esac
for a; do
	[ "$a" = -c ] && exec %[1]q "$@" %[2]s
done
exec %[1]q "$@" %[2]s %[3]q -Wl,--wrap=malloc,--wrap=realloc
`, clang, sanitizeFlags, inject)
	if err := os.WriteFile(
		filepath.Join(dir, "clang"),
		[]byte(script),
		0o700,
	); err != nil { //nolint:gosec // G306: a script it runs
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "tack-port-fail")
	cmd := exec.Command(
		env.goBin,
		"--out",
		bin,
		"./driver",
	) //nolint:gosec // G204: the Go compiler under test
	cmd.Dir = src
	cmd.Env = append(portBuildEnv(src, filepath.Join(dir, "cache")), "PATH="+dir+":"+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the port with its allocations counted: %v\n%s", err, out)
	}
	return bin
}

// sweepAllocFail runs bin, the port TestAllocFail builds, with c's args
// on its input with every allocation granted, counting them, and then
// once for each of at most points values of N spread from 0 to the
// count, failing every allocation from the Nth on, and fails the test
// for each run that does not end as TestAllocFail says: exit status 1
// with tack: out of memory last. A build's points lean to the build
// itself (see buildFailPoints). Each run has a build cache of its own,
// fresh, so a build compiles what the count did and allocates as it
// did, and no two runs share one.
func sweepAllocFail(t *testing.T, bin string, c allocFailCase, points int) {
	in, args := c.in, c.args
	base, total := countAllocs(t, bin, in, args)
	if c.build && base.code != 0 {
		t.Fatalf("with every allocation granted, %s %s exits %d, want 0, so a refused build cannot be told apart\nstderr:\n%s",
			bin, strings.Join(args, " "), base.code, tail(base.stderr))
	}
	ns := failPoints(total, points)
	if c.build {
		// Points spread over the whole run land mostly in compiling the
		// program, which the other cases sweep; half of them go to the
		// build itself, from where --emit of the same target ends.
		_, front := countAllocs(t, bin, in, []string{"--emit", in.file})
		ns = buildFailPoints(total, front, points)
		inBuild := 0
		for _, n := range ns {
			if n >= front {
				inBuild++
			}
		}
		t.Logf("%d of %d points fail an allocation of the build, from %d of %d on", inBuild, len(ns), front, total)
	}
	for _, n := range ns {
		checkRefused(t, n, total, runFailing(t, bin, in, args, "TACK_FAIL_AFTER="+strconv.Itoa(n)))
	}
}

// countAllocs runs bin with args on in with every allocation granted and
// returns how it ended and how many allocations it made, failing t when
// a sanitizer reports anything or it made none.
func countAllocs(t *testing.T, bin string, in input, args []string) (result, int) {
	t.Helper()
	count := filepath.Join(t.TempDir(), "count")
	base := runFailing(t, bin, in, args, "TACK_ALLOC_COUNT="+count)
	if report := sanitizerReport(base); report != "" {
		t.Fatalf("with every allocation granted, %s %s: %s", bin, strings.Join(args, " "), report)
	}
	data, err := os.ReadFile(count) //nolint:gosec // G304: the file the port wrote
	if err != nil {
		t.Fatalf("reading the allocation count: %v", err)
	}
	total, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("the allocation count is %q: %v", data, err)
	}
	if total == 0 {
		t.Fatalf(
			"%s %s allocated nothing, so no failure can be swept: the count is not the runtime's",
			bin,
			strings.Join(args, " "),
		)
	}
	return base, total
}

// buildFailPoints returns the values of N sweepAllocFail fails a build's
// allocations from, of total, the build's own starting about front:
// half of points spread over the whole count, as failPoints spreads
// them, and the other half spread from front to the end, each once, in
// order.
func buildFailPoints(total, front, points int) []int {
	front = min(max(front, 0), total-1)
	seen := map[int]bool{}
	var ns []int
	for _, n := range failPoints(total, max(points/2, 1)) {
		seen[n] = true
		ns = append(ns, n)
	}
	for _, n := range failPoints(total-front, max(points-points/2, 1)) {
		if !seen[front+n] {
			seen[front+n] = true
			ns = append(ns, front+n)
		}
	}
	slices.Sort(ns)
	return ns
}

// checkRefused fails t unless got, the run that was refused every
// allocation from the nth of total on, ended as sweepAllocFail says.
func checkRefused(t *testing.T, n, total int, got result) {
	t.Helper()
	if report := sanitizerReport(got); report != "" {
		t.Errorf("failing allocation %d of %d: %s", n, total, report)
		return
	}
	if got.code != 1 || !bytes.HasSuffix(got.stderr, []byte("tack: out of memory\n")) {
		t.Errorf(
			"failing allocation %d of %d: exit status %d, want 1 with tack: out of memory last\nstderr:\n%s",
			n,
			total,
			got.code,
			tail(got.stderr),
		)
	}
}

// failPoints returns the values of N sweepAllocFail fails allocations
// from, of total: every one from 0 to total-1 when there are at most
// points, and otherwise points of them spread evenly, 0 and total-1
// among them.
func failPoints(total, points int) []int {
	if total <= points {
		ns := make([]int, total)
		for i := range ns {
			ns[i] = i
		}
		return ns
	}
	ns := make([]int, 0, points)
	for i := range points {
		ns = append(ns, i*(total-1)/max(points-1, 1))
	}
	return ns
}

// runFailing runs bin with args on in, with a fresh build cache of its
// own, the sanitizers' exit status sanitizerExit and the variable extra,
// and returns
// how it ended. A run that is killed after compilerTimeout, or that
// cannot be run, fails the test.
func runFailing(t *testing.T, bin string, in input, args []string, extra string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), compilerTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // G204: the port under test
	cmd.Dir = in.dir
	cmd.Env = append(append(compilerEnv(t.TempDir()), in.env...),
		fmt.Sprintf("ASAN_OPTIONS=detect_leaks=1:exitcode=%d", sanitizerExit),
		fmt.Sprintf("UBSAN_OPTIONS=print_stacktrace=1:halt_on_error=1:exitcode=%d", sanitizerExit),
		extra)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		t.Fatalf(
			"running %s %s in %s with %s: killed after %v",
			bin,
			strings.Join(args, " "),
			in.dir,
			extra,
			compilerTimeout,
		)
	case err == nil:
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
	default:
		t.Fatalf(
			"running %s %s in %s with %s: %v\nstderr:\n%s",
			bin,
			strings.Join(args, " "),
			in.dir,
			extra,
			err,
			tail(stderr.Bytes()),
		)
	}
	return result{stdout: stdout.Bytes(), stderr: stderr.Bytes(), code: cmd.ProcessState.ExitCode()}
}

// sanitizerReport returns what a sanitizer reported of r, a run of a port
// built with sanitizeFlags, or "" when none did: the sanitizers' exit
// status, or a report on standard error.
func sanitizerReport(r result) string {
	if r.code == sanitizerExit ||
		bytes.Contains(
			r.stderr,
			[]byte("Sanitizer"),
		) || bytes.Contains(r.stderr, []byte("runtime error:")) {
		return fmt.Sprintf(
			"exit status %d, a sanitizer's report\nstderr:\n%s",
			r.code,
			tail(r.stderr),
		)
	}
	return ""
}

// tail returns the last 4 KB of b, as text, enough for a sanitizer's
// report.
func tail(b []byte) string {
	const most = 4096
	if len(b) > most {
		b = b[len(b)-most:]
	}
	return string(b)
}
