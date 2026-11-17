package porttest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
// --check, and the port's driver with --check and --emit, each first with
// every allocation granted, counting them, and then failing each
// allocation from the Nth on, for N at most $PORTTEST_ALLOC_FAIL points
// spread evenly from 0 to the count, every one when there are no more.
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
			sweepAllocFail(t, bin, c.in, c.args, points)
		})
	}
}

// allocFailCase is one run TestAllocFail sweeps: its subtest's name, its
// input and its command line.
type allocFailCase struct {
	args []string
	name string
	in   input
}

// allocFailCases returns what TestAllocFail sweeps: allocFailPerStage
// cases of each of allocFailStages, spread evenly over its cases, each
// with the stage's command line; every emit probe, written to a
// temporary directory, with --emit and with --check; and the driver of
// the port's source tree src with --check and --emit.
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
	return cases, nil
}

// buildFailingPort builds the port's driver, in the source tree src,
// with the Go compiler, with a clang on PATH that adds sanitizeFlags to
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
	cmd.Env = append(compilerEnv(filepath.Join(dir, "cache")), "PATH="+dir+":"+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the port with its allocations counted: %v\n%s", err, out)
	}
	return bin
}

// sweepAllocFail runs bin, the port TestAllocFail builds, with args on in
// with every allocation granted, counting them, and then once for each of
// at most points values of N spread from 0 to the count, failing every
// allocation from the Nth on, and fails the test for each run that does
// not end as TestAllocFail says.
func sweepAllocFail(t *testing.T, bin string, in input, args []string, points int) {
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
	for _, n := range failPoints(total, points) {
		got := runFailing(t, bin, in, args, "TACK_FAIL_AFTER="+strconv.Itoa(n))
		if report := sanitizerReport(got); report != "" {
			t.Errorf("failing allocation %d of %d: %s", n, total, report)
			continue
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

// runFailing runs bin with args on in, with the sanitizers' exit status
// sanitizerExit and the variable extra, and returns
// how it ended. A run that is killed after compilerTimeout, or that
// cannot be run, fails the test.
func runFailing(t *testing.T, bin string, in input, args []string, extra string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), compilerTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // G204: the port under test
	cmd.Dir = in.dir
	cmd.Env = append(append(compilerEnv(env.portCache), in.env...),
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
