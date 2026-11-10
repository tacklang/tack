package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// specLoopTimeout is how long the binary of the looping example of
// TestSpecCheckerTimesOut may run, short so the test fails it fast. The
// other tests allow specRunTimeout, the suite's own, so a slow machine
// does not fail them.
const specLoopTimeout = 500 * time.Millisecond

// specManyErrors is a program with more errors than the driver reports
// by default, and specManyErrorsDiagnostics the first line of each.
var specManyErrors, specManyErrorsDiagnostics = func() (string, string) {
	var src, diags strings.Builder
	src.WriteString("package main\n\nfunc main() {\n")
	for i := 1; i <= 11; i++ {
		fmt.Fprintf(&src, "    f%d()\n", i)
		fmt.Fprintf(&diags, "main.tck:%d:5: error: undefined: f%d\n", i+3, i)
	}
	src.WriteString("}\n")
	return src.String(), diags.String()
}()

// needsClang reports whether the chapter doc holds a run example, which
// only clang can build.
func needsClang(doc string) bool {
	return strings.Contains(doc, "'''tack run")
}

// skipWithoutClang skips the test when doc needs clang and it is not on
// PATH.
func skipWithoutClang(t *testing.T, doc string) {
	t.Helper()
	if !needsClang(doc) {
		return
	}
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
}

// specWrongChapters are chapters checkSpecChapter must find fault with,
// each with text one of the problems it reports must hold.
var specWrongChapters = []struct {
	name, doc, want string
}{
	{
		"an error example with a wrong message",
		"'''tack error\npackage main\n\nfunc main() {\n    x := y\n}\n'''\n\n" +
			"'''diagnostics\nmain.tck:4:10: error: wrong\n'''\n",
		"diagnostics differ from the block after the example",
	},
	{
		"an error example that compiles",
		"'''tack error\npackage main\n\nfunc main() {\n}\n'''\n\n" +
			"'''diagnostics\nmain.tck:4:10: error: wrong\n'''\n",
		"--check exited 0, want 1",
	},
	{
		"a warn example whose program also errors",
		"'''tack warn\npackage main\n\nfunc main() {\n    x := 1\n    y := z\n}\n'''\n\n" +
			"'''diagnostics\nmain.tck:4:5: warning: declared and not used: x\n" +
			"main.tck:5:10: error: undefined: z\n'''\n",
		"--check exited 1, want 0",
	},
	{
		"a check example that warns",
		"'''tack check\npackage main\n\nfunc main() {\n    x := 1\n}\n'''\n",
		"want 0 and no diagnostic",
	},
	{
		"a check example that does not compile",
		"'''tack check\npackage main\n\nfunc main() {\n    nothing()\n}\n'''\n",
		"--check exited 1, want 0 and no diagnostic",
	},
	{
		"a run example that does not compile",
		"'''tack run exit=0\npackage main\n\nfunc main() {\n    nothing()\n}\n'''\n",
		"--check exited 1, want 0 and no diagnostic",
	},
	{"a parse problem", "'''tack\npackage main\n'''\n", "untagged tack block"},
	{
		"a run example with the wrong exit status",
		"'''tack run exit=4\npackage main\n\nfunc main() {\n    exit(3)\n}\n'''\n",
		"the program exited 3, want 4",
	},
	{
		"a run example with the wrong output",
		"'''tack run exit=0\npackage main\n\nfunc main() {\n    print(\"hi\\n\")\n}\n'''\n\n" +
			"'''output\nbye\n'''\n",
		"the program wrote",
	},
	{
		"a run example that writes to stderr",
		"'''tack run exit=0\npackage main\n\nimport os from \"std/os\"\n\nfunc main() {\n" +
			"    err := os.write_file([]uint8(\"/dev/stderr\"), []uint8(\"oops\\n\"))\n" +
			"    if err.code != 0 {\n        exit(1)\n    }\n}\n'''\n",
		"the program wrote to stderr",
	},
	{
		"a run example that passes --check but does not build",
		"'''tack run exit=0\npackage main\n\nextern(\"spec_nowhere\") func nowhere()\n\n" +
			"func main() {\n    nowhere()\n}\n'''\n",
		"build exited 1",
	},
	{
		"a run example with an extra newline",
		"'''tack run exit=0\npackage main\n\nfunc main() {\n    print(\"hi\\n\\n\")\n}\n'''\n\n" +
			"'''output\nhi\n'''\n",
		"the program wrote",
	},
	{
		"a run example with a leading space",
		"'''tack run exit=0\npackage main\n\nfunc main() {\n    print(\" hi\\n\")\n}\n'''\n\n" +
			"'''output\nhi\n'''\n",
		"the program wrote",
	},
	{
		"an error example with its notes swapped",
		"'''tack error\npackage main\n\nfunc main() {\n    mut d := len(make(list[int64], c_allocator))\n" +
			"    if d != 0 {\n        d -%= 1\n        code, fits := int32(8 / d)\n" +
			"        if fits { exit(code) }\n    }\n}\n'''\n\n'''diagnostics\n" +
			"main.tck:7:33: error: divisor d may be zero; check d != 0 first, or use q, ok := ... / d\n" +
			"main.tck:6:9: note: but d is assigned here, so the proof ends\n" +
			"main.tck:5:8: note: this guard proves d != 0\n'''\n",
		"diagnostics differ",
	},
}

// TestSpecCheckerCatches checks that checkSpecChapter reports each
// chapter of specWrongChapters with a problem holding its text. Chapters
// with a run example need clang, and are skipped without it.
func TestSpecCheckerCatches(t *testing.T) {
	for _, tc := range specWrongChapters {
		t.Run(tc.name, func(t *testing.T) {
			doc := md(tc.doc)
			skipWithoutClang(t, tc.doc)
			rep := checkSpecChapter(t, "t.md", doc, specRunnerFor(specRunTimeout))
			for _, p := range rep.problems {
				if strings.Contains(p, tc.want) {
					return
				}
			}
			t.Errorf("no problem holds %q; the problems are %q", tc.want, rep.problems)
		})
	}
}

// specRightChapters are chapters checkSpecChapter must find nothing
// wrong with, one per kind of example.
var specRightChapters = []struct {
	name, doc string
}{
	{
		"a run example with its output",
		"'''tack run exit=3\npackage main\n\nfunc main() {\n    print(\"hi\\n\")\n" +
			"    exit(3)\n}\n'''\n\n'''output\nhi\n'''\n",
	},
	{
		"a check example",
		"'''tack check\npackage main\n\nfunc main() {\n}\n'''\n",
	},
	{
		"an error example with a note",
		"'''tack error\npackage main\n\nfunc main() {\n    mut d := len(make(list[int64], c_allocator))\n" +
			"    if d != 0 {\n        d -%= 1\n        code, fits := int32(8 / d)\n" +
			"        if fits { exit(code) }\n    }\n}\n'''\n\n'''diagnostics\n" +
			"main.tck:7:33: error: divisor d may be zero; check d != 0 first, or use q, ok := ... / d\n" +
			"main.tck:5:8: note: this guard proves d != 0\n" +
			"main.tck:6:9: note: but d is assigned here, so the proof ends\n'''\n",
	},
	{
		"a warn example with two warnings",
		"'''tack warn\npackage main\n\nfunc main() {\n    x := 1\n    y := 2\n}\n'''\n\n" +
			"'''diagnostics\nmain.tck:4:5: warning: declared and not used: x\n" +
			"main.tck:5:5: warning: declared and not used: y\n'''\n",
	},
	{"a fragment", "'''tack fragment\nnot a program\n'''\n"},
	{
		"an error example with more errors than the default limit",
		"'''tack error\n" + specManyErrors + "'''\n\n'''diagnostics\n" +
			specManyErrorsDiagnostics + "'''\n",
	},
}

// TestSpecCheckerPasses checks that checkSpecChapter reports nothing for
// each chapter of specRightChapters. Chapters with a run example need
// clang, and are skipped without it.
func TestSpecCheckerPasses(t *testing.T) {
	for _, tc := range specRightChapters {
		t.Run(tc.name, func(t *testing.T) {
			skipWithoutClang(t, tc.doc)
			rep := checkSpecChapter(t, "t.md", md(tc.doc), specRunnerFor(specRunTimeout))
			if len(rep.problems) != 0 || len(rep.skipped) != 0 || rep.examples != 1 {
				t.Errorf("%d examples, problems %q, skipped %q; want 1 and none",
					rep.examples, rep.problems, rep.skipped)
			}
		})
	}
}

// TestSpecCheckerWithoutClang checks that, without clang, a run example
// is still checked with --check, and is reported as not built rather
// than passing in silence.
func TestSpecCheckerWithoutClang(t *testing.T) {
	noClang := specRunner{timeout: specRunTimeout}
	good := md("'''tack run exit=4\npackage main\n\nfunc main() {\n    exit(3)\n}\n'''\n")
	rep := checkSpecChapter(t, "t.md", good, noClang)
	if len(rep.problems) != 0 || len(rep.skipped) != 1 ||
		!strings.Contains(rep.skipped[0], "t.md:1: clang not on PATH") {
		t.Errorf(
			"problems %q, skipped %q; want none, and t.md:1 not built",
			rep.problems,
			rep.skipped,
		)
	}
	bad := md("'''tack run exit=0\npackage main\n\nfunc main() {\n    nothing()\n}\n'''\n")
	rep = checkSpecChapter(t, "t.md", bad, noClang)
	assertJudged(t, rep.problems, "--check exited 1, want 0 and no diagnostic")
}

// specLateProgram exits with status 3 after printing early, leaving a
// process that writes late to its stdout 2s later, through Linux's
// /proc, past any fixed wait for the pipe after the exit.
const specLateProgram = "" +
	"package main\n" +
	"\n" +
	"import os from \"std/os\"\n" +
	"import text from \"std/text\"\n" +
	"\n" +
	"func main() {\n" +
	"    mut argv := make(list[text.Text], c_allocator)\n" +
	"    parts := [3]string{\"sh\", \"-c\", \"exec 3>/proc/$PPID/fd/1; (sleep 2; echo late >&3) >/dev/null 2>&1 &\"}\n" +
	"    for _, p := range parts {\n" +
	"        t, e := text.from_bytes([]uint8(p), c_allocator)\n" +
	"        if e.code != 0 {\n" +
	"            exit(1)\n" +
	"        }\n" +
	"        e2 := append(mut argv, t)\n" +
	"        if e2.code != 0 {\n" +
	"            exit(1)\n" +
	"        }\n" +
	"    }\n" +
	"    mut out := make(list[uint8], c_allocator)\n" +
	"    mut errOut := make(list[uint8], c_allocator)\n" +
	"    _, e := os.run(argv, mut out, mut errOut, c_allocator)\n" +
	"    if e.code != 0 {\n" +
	"        exit(1)\n" +
	"    }\n" +
	"    print(\"early\\n\")\n" +
	"    exit(3)\n" +
	"}\n"

// TestSpecCheckerLateOutput checks that a run example's output block is
// compared with everything the program writes to stdout before its
// deadline, here a line a process it started writes 2s after it exits
// with status 3: an output block of the first line alone fails.
func TestSpecCheckerLateOutput(t *testing.T) {
	if _, err := os.Stat("/proc/self/fd/1"); err != nil {
		t.Skip("no /proc to write to a process's stdout through")
	}
	doc := "'''tack run exit=3\n" + specLateProgram + "'''\n\n'''output\nearly\n'''\n"
	skipWithoutClang(t, doc)
	rep := checkSpecChapter(t, "t.md", md(doc), specRunnerFor(specRunTimeout))
	assertJudged(t, rep.problems, "the program wrote\n\"early\\nlate\\n\"")
}

// TestSpecCheckerTimesOut checks that a run example whose program never
// ends fails once its binary has run for the runner's timeout.
func TestSpecCheckerTimesOut(t *testing.T) {
	doc := "'''tack run exit=0\npackage main\n\nfunc main() {\n    for {\n    }\n}\n'''\n"
	skipWithoutClang(t, doc)
	rep := checkSpecChapter(t, "t.md", md(doc), specRunnerFor(specLoopTimeout))
	assertJudged(t, rep.problems, "the program did not finish within 500ms")
}

// TestSpecRunLimits pins the suite's limit on a run example's binary, so
// changing it is a deliberate change to this test too.
func TestSpecRunLimits(t *testing.T) {
	if specRunTimeout != 10*time.Second {
		t.Errorf("specRunTimeout %s, want 10s", specRunTimeout)
	}
}

// writeScript writes script, a shell script, to an executable file in a
// fresh directory and returns its path. The script runs under set -e, so
// a command that fails ends it rather than letting it go on in the wrong
// place; a path in it is quoted with shellQuote. It skips the test
// without sh.
func writeScript(t *testing.T, script string) string {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "script")
	//nolint:gosec // G306: a script the test runs
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nset -e\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin
}

// shellQuote returns s quoted for a shell as one word, whatever it holds:
// in single quotes, each single quote in it closed, escaped and reopened.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// TestSpecScriptPaths checks that a path with a space and a quote in it
// reaches a script whole through shellQuote: the script writes to it,
// and to nothing else.
func TestSpecScriptPaths(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a b's")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(dir, "pid")
	r := runSpecBinary(
		t.Context(),
		writeScript(t, "echo $$ > "+shellQuote(pidFile)+"\n"),
		3*time.Second,
	)
	if r.timedOut || r.code != 0 || r.stderr != "" {
		t.Errorf("timed out %v, status %d, stderr %q; want none", r.timedOut, r.code, r.stderr)
	}
	if _, err := os.Stat(pidFile); err != nil {
		t.Error(err)
	}
}

// TestSpecRunBinaryLateOutput checks runSpecBinary on binaries that exit
// at once, leaving a process that writes to their stdout later. Output
// written before the deadline is all captured, whatever the exit status,
// even 2s after the binary exited; a pipe still open at the deadline,
// held 1s against a 200ms deadline, is a timeout. The scripts sleep whole
// seconds only, which every sleep accepts.
func TestSpecRunBinaryLateOutput(t *testing.T) {
	for _, code := range []int{0, 3} {
		t.Run(fmt.Sprintf("exit %d", code), func(t *testing.T) {
			bin := writeScript(
				t,
				fmt.Sprintf("(sleep 2; echo late) &\necho early\nexit %d\n", code),
			)
			r := runSpecBinary(t.Context(), bin, 4*time.Second)
			if r.timedOut || r.err != nil || r.code != code || r.stdout != "early\nlate\n" {
				t.Errorf("timed out %v, error %v, status %d, stdout %q; want %d and early, late",
					r.timedOut, r.err, r.code, r.stdout, code)
			}
		})
	}
	t.Run("past the deadline", func(t *testing.T) {
		bin := writeScript(t, "sleep 1 &\nexit 0\n")
		r := runSpecBinary(t.Context(), bin, 200*time.Millisecond)
		if !r.timedOut {
			t.Errorf("timed out %v, status %d, error %v; want a timeout", r.timedOut, r.code, r.err)
		}
	})
}

// TestSpecRunBinaryDeadline checks that runSpecBinary allows its binary
// its timeout and no more, as closely as a test stable under load can: a
// binary that finishes after 2s passes under a 3s limit, which any limit
// below 2/3 of it, with 1s of slack, would fail; one that finishes after
// 1s fails under a 750ms limit, which any limit past 4/3 of it would
// pass; and a binary that never ends is stopped no sooner than its limit.
// A limit between the timeout and 4/3 of it, say 1.1 times it, passes
// this test: catching it would need a binary to finish within 75ms of a
// limit, which a loaded machine overruns.
func TestSpecRunBinaryDeadline(t *testing.T) {
	r := runSpecBinary(t.Context(), writeScript(t, "sleep 2\necho done\n"), 3*time.Second)
	if r.timedOut || r.stdout != "done\n" {
		t.Errorf("2s under a 3s limit: timed out %v, stdout %q; want done", r.timedOut, r.stdout)
	}
	r = runSpecBinary(t.Context(), writeScript(t, "sleep 1\necho done\n"), 750*time.Millisecond)
	if !r.timedOut {
		t.Errorf(
			"1s under a 750ms limit: timed out %v, stdout %q; want a timeout",
			r.timedOut,
			r.stdout,
		)
	}
	start := time.Now()
	r = runSpecBinary(t.Context(), writeScript(t, "exec sleep 3\n"), 300*time.Millisecond)
	if took := time.Since(start); !r.timedOut || took < 300*time.Millisecond {
		t.Errorf(
			"never ending under a 300ms limit: timed out %v after %s; want a timeout, no sooner",
			r.timedOut,
			took,
		)
	}
}

// TestSpecRunBinaryHeldPipe checks that runSpecBinary returns at its
// deadline even when the binary, and a process it started, run on and
// hold its stdout open: past the 100ms deadline it kills the binary's
// process group and closes the pipe rather than waiting the 3s the sleeps
// run. The script writes its own process ID and the background sleep's,
// and the test checks the binary has been reaped and the sleep killed.
func TestSpecRunBinaryHeldPipe(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	leader := filepath.Join(t.TempDir(), "leader")
	bin := writeScript(
		t,
		"echo $$ > "+shellQuote(leader)+"\necho early\nsleep 3 &\necho $! > "+shellQuote(pidFile)+
			"\nexec sleep 3\n",
	)
	start := time.Now()
	r := runSpecBinary(t.Context(), bin, 100*time.Millisecond)
	if took := time.Since(start); !r.timedOut || took > time.Second || r.stdout != "early\n" {
		t.Errorf("timed out %v after %s, stdout %q; want a timeout within 1s, early kept",
			r.timedOut, took, r.stdout)
	}
	assertSpecProcessGone(t, leader)
	assertSpecProcessDead(t, pidFile)
}

// TestSpecRunBinaryEscapedPipe checks that runSpecBinary returns at its
// deadline even when a process outside the binary's process group, which
// killing the group does not reach, holds its stdout open: it closes the
// pipe itself. The escaped sleep, started by setsid, outlives the test by
// about 3s, and then ends on its own. It needs setsid, which Linux has.
func TestSpecRunBinaryEscapedPipe(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("setsid not on PATH")
	}
	bin := writeScript(t, "echo early\nsetsid sleep 3 &\nexec sleep 3\n")
	start := time.Now()
	r := runSpecBinary(t.Context(), bin, 100*time.Millisecond)
	if took := time.Since(start); !r.timedOut || took > time.Second || r.stdout != "early\n" {
		t.Errorf("timed out %v after %s, stdout %q; want a timeout within 1s, early kept",
			r.timedOut, took, r.stdout)
	}
}

// TestSpecRunBinaryLeavesNothing checks that a run that finishes in time
// leaves nothing it started running: a process the binary started with
// its output elsewhere, which does not hold the run open, is killed with
// the binary's process group once the run ends.
func TestSpecRunBinaryLeavesNothing(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	bin := writeScript(
		t,
		"sleep 3 >/dev/null 2>&1 &\necho $! > "+shellQuote(pidFile)+"\necho done\n",
	)
	r := runSpecBinary(t.Context(), bin, 3*time.Second)
	if r.timedOut || r.stdout != "done\n" {
		t.Errorf("timed out %v, stdout %q; want done", r.timedOut, r.stdout)
	}
	assertSpecProcessDead(t, pidFile)
}

// TestSpecRunBinaryKillsHard checks that the binary's process group is
// killed with a signal nothing can ignore: a process the binary started
// that ignores SIGTERM is gone after the run all the same.
func TestSpecRunBinaryKillsHard(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	bin := writeScript(
		t,
		"(trap '' TERM; sleep 3) >/dev/null 2>&1 &\necho $! > "+shellQuote(pidFile)+
			"\necho done\n",
	)
	r := runSpecBinary(t.Context(), bin, 3*time.Second)
	if r.timedOut || r.stdout != "done\n" {
		t.Errorf("timed out %v, stdout %q; want done", r.timedOut, r.stdout)
	}
	assertSpecProcessDead(t, pidFile)
}

// TestSpecRunBinaryReaped checks that a binary stopped at its deadline is
// reaped, not left a zombie of the test process.
func TestSpecRunBinaryReaped(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	r := runSpecBinary(
		t.Context(),
		writeScript(t, "echo $$ > "+shellQuote(pidFile)+"\nexec sleep 3\n"),
		100*time.Millisecond,
	)
	if !r.timedOut {
		t.Errorf("timed out %v; want a timeout", r.timedOut)
	}
	assertSpecProcessGone(t, pidFile)
}

// TestSpecRunBinaryCancelled checks that runSpecBinary stops a binary,
// as timed out, once its context is done, before the deadline.
func TestSpecRunBinaryCancelled(t *testing.T) {
	bin := writeScript(t, "exec sleep 3\n")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	r := runSpecBinary(ctx, bin, time.Hour)
	if took := time.Since(start); !r.timedOut || took > time.Second {
		t.Errorf("timed out %v after %s; want a timeout within 1s", r.timedOut, took)
	}
}

// TestSpecRunBinaryNotRun checks that runSpecBinary reports a binary it
// cannot start.
func TestSpecRunBinaryNotRun(t *testing.T) {
	r := runSpecBinary(t.Context(), filepath.Join(t.TempDir(), "none"), time.Second)
	if r.err == nil || r.timedOut {
		t.Errorf("error %v, timed out %v; want an error", r.err, r.timedOut)
	}
}

// specRecorder is a specReporter that records what it is given.
type specRecorder struct {
	errors, logs []string
}

// Error records args as a failure.
func (r *specRecorder) Error(args ...any) { r.errors = append(r.errors, fmt.Sprint(args...)) }

// Log records args as a log line.
func (r *specRecorder) Log(args ...any) { r.logs = append(r.logs, fmt.Sprint(args...)) }

// TestReportSpec checks that reportSpec reports failures as failures and
// log lines as log lines, every one, in order.
func TestReportSpec(t *testing.T) {
	var r specRecorder
	reportSpec(&r, specOutcome{failures: []string{"f1", "f2"}, logs: []string{"l1", "l2"}})
	if !slices.Equal(r.errors, []string{"f1", "f2"}) ||
		!slices.Equal(r.logs, []string{"l1", "l2"}) {
		t.Errorf("errors %q and logs %q, want f1, f2 and l1, l2", r.errors, r.logs)
	}
}

// specCheckOK, specCheckBad, specRun3 and specErrSrc are the sources of
// examples for the spec directories of TestSpecCheckDir: a check example
// that passes, one that does not compile, a run example that exits 3,
// and an error example that wants its diagnostics block after it.
const (
	specCheckOK  = "'''tack check\npackage main\n\nfunc main() {\n}\n'''\n"
	specCheckBad = "'''tack check\npackage main\n\nfunc main() {\n    nothing()\n}\n'''\n"
	specRun3     = "'''tack run exit=3\npackage main\n\nfunc main() {\n    exit(3)\n}\n'''\n"
	specErrSrc   = "'''tack error\npackage main\n\nfunc main() {\n    x := y\n    z := w\n}\n'''\n\n"
)

// specThree returns the chapter 01-a.md of three check examples,
// separated by text, whose example at each index in bad does not
// compile, and the positions of the examples, as 01-a.md:L (check).
func specThree(bad ...int) (string, []string) {
	var doc strings.Builder
	pos := make([]string, 0, 3)
	line := 1
	for i := range 3 {
		src := specCheckOK
		if slices.Contains(bad, i) {
			src = specCheckBad
		}
		pos = append(pos, fmt.Sprintf("01-a.md:%d (check)", line))
		doc.WriteString(src + "\nText.\n\n")
		line += strings.Count(src, "\n") + 3
	}
	return doc.String(), pos
}

// specDirCase is a spec directory checkSpecDir is run on, and the text
// each of its failures must hold, one want per failure, in order.
type specDirCase struct {
	name  string
	files map[string]string // file name to content, every ''' made backticks
	want  []string          // what each failure holds, in order; nil for none
}

// specDirCases are the spec directories of TestSpecCheckDir: those of
// specExampleDirCases, and directories of other faults.
func specDirCases() []specDirCase {
	return append(specExampleDirCases(), []specDirCase{
		{
			"a wrong example in the index",
			map[string]string{"01-a.md": specCheckOK, "README.md": specCheckBad},
			[]string{"README.md:1 (check): --check exited 1"},
		},
		{
			"a chapter with only parse problems",
			map[string]string{"01-a.md": "'''Tack check\nx\n'''\n"},
			[]string{"01-a.md:1: fence info string"},
		},
		{
			"several problems in one chapter and across chapters",
			map[string]string{
				"01-a.md": "<div>\n\n'''tak\nx\n'''\n\n" + specCheckBad,
				"02-b.md": specCheckBad + "\nText.\n\n" + specCheckBad,
			},
			[]string{
				"01-a.md:1: a < followed", "01-a.md:3: fence info word", "01-a.md:7 (check)",
				"02-b.md:1 (check)", "02-b.md:11 (check)",
			},
		},
		{"an empty directory", map[string]string{}, []string{"no Markdown files in "}},
		{
			"an unreadable chapter",
			map[string]string{"01-a.md": specCheckOK, "02-b.md": "-> missing"},
			[]string{"02-b.md"},
		},
	}...)
}

// specExampleDirCases are the spec directories of TestSpecCheckDir whose
// faults are in their examples, or that have none.
func specExampleDirCases() []specDirCase {
	first, firstPos := specThree(0)
	middle, middlePos := specThree(1)
	last, lastPos := specThree(2)
	all, allPos := specThree(0, 1, 2)
	errBlock := func(lines ...string) string {
		return specErrSrc + "'''diagnostics\n" + strings.Join(lines, "\n") + "\n'''\n"
	}
	x := "main.tck:4:10: error: undefined: y"
	z := "main.tck:5:10: error: undefined: w"
	return []specDirCase{
		{
			"right chapters",
			map[string]string{"01-a.md": specCheckOK, "02-b.md": errBlock(x, z)},
			nil,
		},
		{"the wrong example first", map[string]string{"01-a.md": first}, firstPos[:1]},
		{"the wrong example in the middle", map[string]string{"01-a.md": middle}, middlePos[1:2]},
		{"the wrong example last", map[string]string{"01-a.md": last}, lastPos[2:]},
		{"every example wrong", map[string]string{"01-a.md": all}, allPos},
		{
			"a diagnostics block with a line too many",
			map[string]string{"01-a.md": errBlock(x, z, "main.tck:6:1: error: extra")},
			[]string{"01-a.md:1 (error): diagnostics differ"},
		},
		{
			"a diagnostics block with a line missing",
			map[string]string{"01-a.md": errBlock(x)},
			[]string{"01-a.md:1 (error): diagnostics differ"},
		},
		{
			"a run example with the wrong status",
			map[string]string{"01-a.md": strings.Replace(specRun3, "exit=3", "exit=4", 1)},
			[]string{"01-a.md:1 (run): the program exited 3, want 4"},
		},
		{"a right run example", map[string]string{"01-a.md": specRun3}, nil},
	}
}

// writeSpecDir writes files into a fresh directory, each run of three
// single quotes made three backticks, a content of "-> target" made a
// symbolic link to target, and returns the directory.
func writeSpecDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if target, ok := strings.CutPrefix(content, "-> "); ok {
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(path, []byte(md(content)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// assertSpecFailures checks that failures are exactly as many as want,
// each holding the want at its index.
func assertSpecFailures(t *testing.T, failures, want []string) {
	t.Helper()
	ok := len(failures) == len(want)
	for i := 0; ok && i < len(want); i++ {
		ok = strings.Contains(failures[i], want[i])
	}
	if !ok {
		t.Errorf("failures\n%q\nwant one holding each of\n%q", failures, want)
	}
}

// TestSpecCheckDir runs checkSpecDir, end to end, on each directory of
// specDirCases and checks its failures, one by one. A right directory
// also logs how many tack blocks it holds. Cases with a run example need
// clang and are skipped without it; the others run everywhere.
func TestSpecCheckDir(t *testing.T) {
	for _, tc := range specDirCases() {
		t.Run(tc.name, func(t *testing.T) {
			for _, content := range tc.files {
				skipWithoutClang(t, content)
			}
			o := checkSpecDir(t, writeSpecDir(t, tc.files))
			assertSpecFailures(t, o.failures, tc.want)
			if tc.want == nil &&
				(len(o.logs) != 1 || !strings.Contains(o.logs[0], " tack blocks in ")) {
				t.Errorf("logs %q, want the count alone", o.logs)
			}
		})
	}
}

// TestSpecCheckDirLogs checks the log lines of checkSpecDir, exactly, on
// a directory of no fault: the count of tack blocks, summed over its
// chapters, and the fragments of every chapter, in order.
func TestSpecCheckDirLogs(t *testing.T) {
	frag := "'''tack fragment\nx\n'''\n\nText.\n\n"
	dir := writeSpecDir(t, map[string]string{
		"01-a.md": frag + specCheckOK,
		"02-b.md": frag + frag,
		"03-c.md": specCheckOK,
	})
	o := checkSpecDir(t, dir)
	assertSpecFailures(t, o.failures, nil)
	want := "5 tack blocks in " + dir +
		"; fragments, not compiled: 3 (01-a.md:1, 02-b.md:1, 02-b.md:7)"
	if !slices.Equal(o.logs, []string{want}) {
		t.Errorf("logs %q, want %q", o.logs, want)
	}
}

// TestSpecCheckDirTimesOut runs checkSpecDir on a directory whose run
// example never ends, with specRunTimeout lowered for the test, and
// checks it fails once the binary has run that long.
func TestSpecCheckDirTimesOut(t *testing.T) {
	loop := "'''tack run exit=0\npackage main\n\nfunc main() {\n    for {\n    }\n}\n'''\n"
	skipWithoutClang(t, loop)
	saved := specRunTimeout
	specRunTimeout = specLoopTimeout
	t.Cleanup(func() { specRunTimeout = saved })
	o := checkSpecDir(t, writeSpecDir(t, map[string]string{"01-a.md": loop}))
	assertSpecFailures(t, o.failures,
		[]string{"01-a.md:1 (run): the program did not finish within 500ms"})
}

// TestSpecCheckDirWithoutClang runs checkSpecDir, end to end, with an
// empty PATH, so clang is not found: a run example is still checked with
// --check, and is logged as not built rather than failing.
func TestSpecCheckDirWithoutClang(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	o := checkSpecDir(t, writeSpecDir(t, map[string]string{"01-a.md": specRun3}))
	assertSpecFailures(t, o.failures, nil)
	if len(o.logs) != 2 || o.logs[0] != "not built: 01-a.md:1: clang not on PATH" {
		t.Errorf("logs %q, want the example not built, then the count", o.logs)
	}
	bad := strings.Replace(specRun3, "exit(3)", "nothing()", 1)
	o = checkSpecDir(t, writeSpecDir(t, map[string]string{"01-a.md": bad}))
	assertSpecFailures(t, o.failures, []string{"01-a.md:1 (run): --check exited 1"})
}

// specErrorExample is an error example whose diagnostics block holds one
// error, for the tests of judgeCheck.
var specErrorExample = specExample{
	kind: specError, hasExp: true, expect: "main.tck:4:10: error: undefined: y\n",
}

// specErrorStderr is what the driver writes for specErrorExample.
const specErrorStderr = "\nmain.tck:4:10: error: undefined: y\n03 |\tfunc main() {\n" +
	"04 |\t    x := y\n   |\t         ^\n05 |\t}\n\n"

// TestSpecJudgeCheck checks judgeCheck on --check runs made up for it,
// including what the driver does not do today: write to stdout, or write
// a line that is not part of a diagnostic.
func TestSpecJudgeCheck(t *testing.T) {
	check := specExample{kind: specCheck}
	for _, tc := range []struct {
		name string
		want string // text a problem must hold, or "" for none
		r    result
		ex   specExample
	}{
		{"a clean check", "", result{}, check},
		{"a clean run", "", result{}, specExample{kind: specRun}},
		{"check output on stdout", "--check wrote to stdout", result{stdout: "x\n"}, check},
		{
			"error output on stdout", "--check wrote to stdout",
			result{code: exitFailure, stdout: "x\n", stderr: specErrorStderr},
			specErrorExample,
		},
		{"a warning on a check", "want 0 and no diagnostic", result{stderr: "x\n"}, check},
		{"a failed check", "want 0 and no diagnostic", result{code: exitFailure}, check},
		{
			"the diagnostics expected", "",
			result{code: exitFailure, stderr: specErrorStderr},
			specErrorExample,
		},
		{
			"a stray line among them", "not part of a diagnostic: \"garbage\"",
			result{code: exitFailure, stderr: specErrorStderr + "garbage\n"},
			specErrorExample,
		},
		{
			"a stray line like an excerpt", "not part of a diagnostic",
			result{code: exitFailure, stderr: specErrorStderr + "4 |\tx\n"},
			specErrorExample,
		},
		{
			"a header in the middle of a line", "not part of a diagnostic",
			result{code: exitFailure, stderr: specErrorStderr + "x main.tck:5:1: note: y\n"},
			specErrorExample,
		},
		{
			"diagnostics reordered", "diagnostics differ",
			result{code: exitFailure, stderr: "main.tck:1:1: error: a\nmain.tck:2:1: note: b\n"},
			specExample{
				kind: specError, hasExp: true,
				expect: "main.tck:2:1: note: b\nmain.tck:1:1: error: a\n",
			},
		},
		{
			"another diagnostic", "diagnostics differ",
			result{code: exitFailure, stderr: specErrorStderr + "main.tck:5:1: note: x\n"},
			specErrorExample,
		},
		{
			"the wrong exit status", "--check exited 0, want 1",
			result{code: exitOK, stderr: specErrorStderr},
			specErrorExample,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertJudged(t, judgeCheck(tc.ex, tc.r), tc.want)
		})
	}
}

// TestSpecJudgeBuildAndRun checks judgeBuild and judgeRun on builds and
// runs made up for them.
func TestSpecJudgeBuildAndRun(t *testing.T) {
	assertJudged(t, judgeBuild(result{}), "")
	assertJudged(t, judgeBuild(result{code: exitFailure, stderr: "x"}), "build exited 1")
	assertJudged(t, judgeBuild(result{stderr: "x"}), "build exited 0, want 0 and no diagnostic")
	assertJudged(t, judgeBuild(result{code: exitFailure}), "build exited 1")

	run := specExample{kind: specRun, exit: 3, expect: "hi\n", hasExp: true}
	assertJudged(t, judgeRun(run, specRunResult{stdout: "hi\n", code: 3}), "")
	unpinned := specExample{kind: specRun, exit: 3}
	assertJudged(t, judgeRun(unpinned, specRunResult{code: 3, stdout: "x"}), "")
	for _, tc := range []struct {
		name string
		want string
		r    specRunResult
	}{
		{"stderr", "wrote to stderr", specRunResult{stdout: "hi\n", code: 3, stderr: "x"}},
		{"status", "exited 4, want 3", specRunResult{stdout: "hi\n", code: 4}},
		{"stdout", "the program wrote", specRunResult{stdout: "ho\n", code: 3}},
		{"an extra newline", "the program wrote", specRunResult{stdout: "hi\n\n", code: 3}},
		{"no newline", "the program wrote", specRunResult{stdout: "hi", code: 3}},
		{"a leading space", "the program wrote", specRunResult{stdout: " hi\n", code: 3}},
		{"a carriage return", "the program wrote", specRunResult{stdout: "hi\r\n", code: 3}},
		{
			"timeout", "did not finish within 1s",
			specRunResult{timedOut: true, timeout: time.Second, stdout: "hi\n", code: 3},
		},
		{
			"no run", "could not be run: no such file",
			specRunResult{err: errors.New("no such file")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) { assertJudged(t, judgeRun(run, tc.r), tc.want) })
	}
	// A binary killed by a signal has status -1, which is no 255.
	assertJudged(t, judgeRun(specExample{kind: specRun, exit: 255}, specRunResult{code: -1}),
		"exited -1, want 255")
}

// assertJudged checks that problems, what a judge found, is empty when
// want is "", and otherwise holds a problem holding want.
func assertJudged(t *testing.T, problems []string, want string) {
	t.Helper()
	if want == "" {
		if len(problems) != 0 {
			t.Errorf("problems %q, want none", problems)
		}
		return
	}
	for _, p := range problems {
		if strings.Contains(p, want) {
			return
		}
	}
	t.Errorf("no problem holds %q; the problems are %q", want, problems)
}

// TestSpecIndexProblems checks specIndexProblems on directories made up
// for it: a good one, and one with each fault it reports.
func TestSpecIndexProblems(t *testing.T) {
	index := "# Index\n\n1. [One](01-one.md)\n2. [Two](02-two.md)\n"
	good := map[string]string{"README.md": index, "01-one.md": "x\n", "02-two.md": "x\n"}
	with := func(name, content string) map[string]string {
		return withFiles(good, map[string]string{name: content})
	}
	for _, tc := range []struct {
		name  string
		want  string // text a problem must hold, or "" for none
		files map[string]string
		dirs  []string
	}{
		{"a good directory", "", good, nil},
		{"a stray directory", "extra: docs/spec holds only", good, []string{"extra"}},
		{"a directory named .md", "extra.md: docs/spec holds only", good, []string{"extra.md"}},
		{"a file not .md", "notes.txt: docs/spec holds only", with("notes.txt", "x"), nil},
		{
			"a chapter not linked", "README.md does not link the chapter 03-three.md",
			with("03-three.md", "x\n"), nil,
		},
		{
			"a dangling link", "README.md links 03-three.md, which does not exist",
			with("README.md", index+"3. [Three](03-three.md)\n"), nil,
		},
		{
			"a badly named chapter", "three.md: a chapter's file is named NN-name.md",
			with("three.md", "x\n"), nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeDir(t, tc.files)
			for _, d := range tc.dirs {
				if err := os.Mkdir(filepath.Join(dir, d), 0o750); err != nil {
					t.Fatal(err)
				}
			}
			problems, err := specIndexProblems(dir)
			if err != nil {
				t.Fatal(err)
			}
			assertJudged(t, problems, tc.want)
		})
	}
}

// specLinkCases are indexes, each with whether specIndexLinks must count
// it as linking 02-two.md. Each one counted renders a link to 02-two.md
// under CommonMark, and each one that would render none is not counted;
// others render a link but are not of the one form that counts. The P
// and F cases are a review's probes of an earlier check, each first given
// as it was, then, where the form allows, as a line of the form in the
// same place.
var specLinkCases = []struct {
	name   string
	index  string
	linked bool
}{
	{"the form", "# Index\n\n1. [One](01-one.md)\n2. [Two](02-two.md)\n", true},
	{"the form alone", "2. [Two](02-two.md)", true},
	{"the form after a blank line", "Text.\n\n2. [Two](02-two.md)\n", true},
	{"the form after a line of spaces", "Text.\n   \n2. [Two](02-two.md)\n", true},
	{"a long number", "123456789. [Two](02-two.md)\n", true},
	{"a title of other punctuation", "1. [Two: the (big) one, & more](02-two.md)\n", true},
	{"the form after a fence", "```text\nx\n```\n\n2. [Two](02-two.md)\n", true},
	{"a ten-digit number", "1234567890. [Two](02-two.md)\n", false},
	{"no number", "[Two](02-two.md)\n", false},
	{"a bullet", "- [Two](02-two.md)\n", false},
	{"no space after the dot", "2.[Two](02-two.md)\n", false},
	{"two spaces after the dot", "2.  [Two](02-two.md)\n", false},
	{"a parenthesis marker", "2) [Two](02-two.md)\n", false},
	{"indented", " 2. [Two](02-two.md)\n", false},
	{"text after it", "2. [Two](02-two.md) and more\n", false},
	{"a space after it", "2. [Two](02-two.md) \n", false},
	{"text before the link", "2. See [Two](02-two.md)\n", false},
	{"an empty title", "2. [](02-two.md)\n", false},
	{"a bracket in the title", "2. [Two [draft]](02-two.md)\n", false},
	{"a backtick in the title", "2. [`Two`](02-two.md)\n", false},
	{"a backslash in the title", "2. [Two\\](02-two.md)\n", false},
	{"an angle bracket in the title", "2. [<b>](02-two.md)\n", false},
	{"a bang in the title", "2. [!Two](02-two.md)\n", false},
	{"an asterisk in the title", "2. [*Two*](02-two.md)\n", false},
	{"an underscore in the title", "2. [_Two_](02-two.md)\n", false},
	{"a split line", "2. [Two\nchapter](02-two.md)\n", false},
	{"a file not named NN-name.md", "2. [Two](2-two.md)\n", false},
	{"an uppercase file", "2. [Two](02-Two.md)\n", false},
	{"a file not .md", "2. [Two](02-two.txt)\n", false},
	{"an anchor", "2. [Two](02-two.md#x)\n", false},
	{"a title in the destination", "2. [Two](02-two.md \"t\")\n", false},
	{"an image", "2. ![Two](02-two.md)\n", false},
	{"in a fence", "```text\n2. [Two](02-two.md)\n```\n", false},
	{"in a tilde fence", "~~~text\n2. [Two](02-two.md)\n~~~\n", false},
	{"after a blank line in a fence", "```text\n\n2. [Two](02-two.md)\n```\n", false},
	{"after a blank line in a tilde fence", "~~~text\n\n2. [Two](02-two.md)\n~~~\n", false},
	{"between two fences", "```text\nx\n```\n2. [Two](02-two.md)\n```text\ny\n```\n", false},
	{"two links on one line", "1. [One](01-one.md)[Two](02-two.md)\n", false},
	{"two links, the form second", "1. [Two](02-two.md)[One](01-one.md)\n", false},
	{"after a paragraph line", "Text\n2. [Two](02-two.md)\n", false},
	{"in a code span from the line before", "a `b\n2. [Two](02-two.md)\nc` d\n", false},
	{"after a code span across a blank line", "a `b\n\n2. [Two](02-two.md)\n\nc` d\n", true},
	{"in image alt text from the line before", "![x\n2. [Two](02-two.md)](y.png)\n", false},
	{"in a link title from the line before", "[a](b \"\n2. [Two](02-two.md)\")\n", false},
	{"in a reference title from the line before", "[a]: /u \"\n2. [Two](02-two.md)\"\n", false},
	{"in a block quote", "> x\n2. [Two](02-two.md)\n", false},
	{"right after a bullet item, starting a new list", "- x\n2. [Two](02-two.md)\n", false},
	{
		"a form line after one with www in its title",
		"1. [See www.a.org](01-a.md)\n2. [Two](02-two.md)\n",
		false,
	},
	{
		"two form lines after a paragraph line",
		"Text\n2. [Two](02-two.md)\n3. [One](01-one.md)\n",
		false,
	},
	{
		"form lines in a code span from the line before",
		"a `b\n3. [One](01-one.md)\n2. [Two](02-two.md)\nc` d\n",
		false,
	},
	{"a footnote-like title", "1. [^Two](02-two.md)\n\n[^Two]: n\n", false},
	{"a caret later in the title", "1. [Two^](02-two.md)\n", false},
	{"P1 escaped backtick before a real code span", "\\` `[Two](02-two.md)`\n", false},
	{"P2 inside image alt text", "![x [Two](02-two.md)](y.png)\n", false},
	{"P3 inside another link's title", "[a](b \"[Two](02-two.md)\")\n", false},
	{"P4 in a link reference definition title", "[a]: /u \"[Two](02-two.md)\"\n", false},
	{"P5 inside an angle-bracket destination", "[a](<[Two](02-two.md)>)\n", false},
	{"P6 indented code in a block quote", ">     [Two](02-two.md)\n", false},
	{"P6 as the form", ">     2. [Two](02-two.md)\n", false},
	{"P7 indented code in a list item", "-     [Two](02-two.md)\n", false},
	{"P7 as the form", "-     2. [Two](02-two.md)\n", false},
	{"F1 escaped backtick then link then lone backtick", "\\` [Two](02-two.md) `\n", false},
	{"F2 adjacent links", "[One](01-one.md)[Two](02-two.md)\n", false},
	{"F3 escaped backslash before link", "\\\\[Two](02-two.md)\n", false},
	{"F4 escaped bang before link", "\\![Two](02-two.md)\n", false},
	{"F5 brackets in link text", "[Two [draft]](02-two.md)\n", false},
	{"F6 link text over two lines", "[Two\nchapter](02-two.md)\n", false},
	{"F7 lazy continuation indented 4", "See\n    [Two](02-two.md)\n", false},
	{"F7 as the form", "See\n    2. [Two](02-two.md)\n", false},
	{"F8 nested list item indented 4", "- a\n    - [Two](02-two.md)\n", false},
	{"F9 backticks paired across paragraphs", "a ` b\n\n[Two](02-two.md)\n\nc ` d\n", false},
	{"F9 as the form", "a ` b\n\n2. [Two](02-two.md)\n\nc ` d\n", true},
	{"F10 reference-style link", "[Two][t]\n\n[t]: 02-two.md\n", false},
	{"F11 link with a title", "[Two](02-two.md \"t\")\n", false},
}

// TestSpecIndexLinks checks specIndexLinks on each index of
// specLinkCases: whether it counts 02-two.md as linked, and that it
// counts nothing else.
func TestSpecIndexLinks(t *testing.T) {
	for _, tc := range specLinkCases {
		t.Run(tc.name, func(t *testing.T) {
			got := specIndexLinks(tc.index)
			if got["02-two.md"] != tc.linked {
				t.Errorf("02-two.md linked %v, want %v; links %v", got["02-two.md"], tc.linked, got)
			}
			delete(got, "02-two.md")
			delete(got, "01-one.md")
			if len(got) != 0 {
				t.Errorf("other links %v, want none", got)
			}
		})
	}
}

// TestSpecIndexTitles checks which characters a title of the index's
// form may hold: each allowed punctuation character counts, every other
// ASCII punctuation character does not, nor does a www. GitHub would make
// an autolink of. The pipe-table shapes are a title with a | over a
// delimiter row, which GitHub turns into a table.
func TestSpecIndexTitles(t *testing.T) {
	const allowed = ",.:;()'&-"
	for c := byte('!'); c <= '~'; c++ {
		if isASCIIAlnum(c) {
			continue
		}
		want := strings.IndexByte(allowed, c) >= 0
		index := "1. [Two " + string(c) + " more](02-two.md)\n"
		if got := specIndexLinks(index)["02-two.md"]; got != want {
			t.Errorf("%q: linked %v, want %v", index, got, want)
		}
	}
	for _, index := range []string{
		"1. [Two | more](02-two.md)\n    -|-\n",
		"1. [Two | more](02-two.md)\n   -|-\n",
		"1. [Two | more](02-two.md)\n   | - | - |\n",
		"1. [Two | more](02-two.md)\n   :-|-:\n",
		"1. [Two $x$ more](02-two.md)\n",
		"1. [See www.two.org](02-two.md)\n",
		"1. [See WWW.two.org](02-two.md)\n",
	} {
		if specIndexLinks(index)["02-two.md"] {
			t.Errorf("%q: linked, want not", index)
		}
	}
	if !specIndexLinks("1. [Two](02-www.md)\n")["02-www.md"] {
		t.Error("a file named with www: not linked, want linked")
	}
	if !specIndexLinks("1. [Two 2nd](02-two.md)\n")["02-two.md"] {
		t.Error("a digit in the title: not linked, want linked")
	}
	if !specIndexLinks("1. [Two, the www one](02-two.md)\n")["02-two.md"] {
		t.Error("www without a dot: not linked, want linked")
	}
}

// TestSpecIndexOutcome checks specIndexOutcome, which TestSpecIndex
// reports: the problems of a bad directory, and the error of one with no
// index.
func TestSpecIndexOutcome(t *testing.T) {
	badDir := writeDir(t, map[string]string{
		"README.md": "1. [Three](03-three.md)\n", "01-one.md": "x\n", "two.md": "x\n", "x.txt": "x",
	})
	bad := specIndexOutcome(badDir)
	assertSpecFailures(t, bad.failures, []string{
		"x.txt: docs/spec holds only", "README.md does not link the chapter 01-one.md",
		"two.md: a chapter's file is named", "README.md links 03-three.md, which does not exist",
	})
	if len(bad.logs) != 0 {
		t.Errorf("logs %q, want none: every index problem fails", bad.logs)
	}
	assertJudged(t, specIndexOutcome(t.TempDir()).failures, "reading the index")
}

// isASCIIAlnum reports whether c is an ASCII letter or digit.
func isASCIIAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}
