package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tacklang/tack/codegen"
	"github.com/tacklang/tack/codegen/c"
)

// examplesDir is where the example programs live, relative to this package.
const examplesDir = "../examples"

// Names of the stand-in backends registered below. They generate C with the
// real backend but replace the toolchain, so the build step's error and
// cleanup paths can be exercised without clang.
const (
	copyBackend    = "test-copy"    // "builds" by copying the source to --out
	failBackend    = "test-fail"    // toolchain writes to stderr and exits 3
	missingBackend = "test-missing" // toolchain does not exist
	sleepBackend   = "test-sleep"   // toolchain hangs until killed
)

// fakeToolchain is a backend that generates C with the real c backend but
// builds with argv instead of clang. In argv, "{src}" and "{out}" are
// replaced by the build's source and output paths.
type fakeToolchain struct {
	codegen.Backend
	name string
	argv []string
}

// Name returns the name the fake is registered under.
func (f fakeToolchain) Name() string { return f.name }

// BuildCmd returns argv with the {src} and {out} placeholders filled in.
func (f fakeToolchain) BuildCmd(src, out string) []string {
	r := strings.NewReplacer("{src}", src, "{out}", out)
	cmd := make([]string, len(f.argv))
	for i, a := range f.argv {
		cmd[i] = r.Replace(a)
	}
	return cmd
}

// init registers the stand-in backends.
func init() {
	for _, f := range []fakeToolchain{
		{name: copyBackend, argv: []string{"cp", "{src}", "{out}"}},
		{name: failBackend, argv: []string{
			"sh", "-c", "echo toolchain-says-no >&2; exit 3", "sh", "{src}", "{out}",
		}},
		{name: missingBackend, argv: []string{"/nonexistent/tack-test-cc", "{src}", "{out}"}},
		{name: sleepBackend, argv: []string{"sleep", "30"}},
	} {
		f.Backend = c.New()
		codegen.Register(f)
	}
}

// result is what one call to run produced.
type result struct {
	stdout string
	stderr string
	code   int
}

// runTack calls run in process with args and captures its output. A panic
// fails the test rather than crashing the suite.
func runTack(t *testing.T, args ...string) result {
	t.Helper()
	return runTackCtx(t, t.Context(), args...)
}

// runTackCtx is runTack with an explicit context.
func runTackCtx(t *testing.T, ctx context.Context, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("run(%q) panicked: %v", args, r)
		}
	}()
	code := run(ctx, args, &stdout, &stderr)
	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

// example returns the path to examples/<name>/main.tck.
func example(name string) string {
	return filepath.Join(examplesDir, name, "main.tck")
}

// writeSource writes src to a file in a fresh temp directory and returns its
// path.
func writeSource(t *testing.T, src []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.tck")
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// isolateTemp points TMPDIR at a fresh directory, so the test can check
// that the build leaves nothing behind there, and returns it.
func isolateTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	return dir
}

// assertEmptyDir fails the test if dir has any entries.
func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("left behind in temp dir: %s", e.Name())
	}
}

// assertUsageError checks r is a usage error: exit 2, nothing on stdout,
// and a "tack:" message pointing at --help on stderr containing want.
func assertUsageError(t *testing.T, r result, want string) {
	t.Helper()
	if r.code != exitUsage {
		t.Errorf("exit code = %d, want %d; stderr:\n%s", r.code, exitUsage, r.stderr)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
	if !strings.HasPrefix(r.stderr, "tack: ") || !strings.Contains(r.stderr, "--help") {
		t.Errorf("stderr = %q, want a tack: message pointing at --help", r.stderr)
	}
	if !strings.Contains(r.stderr, want) {
		t.Errorf("stderr = %q, want it to mention %q", r.stderr, want)
	}
}

// assertFailure checks r is a non-diagnostic failure: exit 1, nothing on
// stdout, and a "tack:" message on stderr containing want.
func assertFailure(t *testing.T, r result, want string) {
	t.Helper()
	if r.code != exitFailure {
		t.Errorf("exit code = %d, want %d; stderr:\n%s", r.code, exitFailure, r.stderr)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
	if !strings.HasPrefix(r.stderr, "tack: ") || !strings.Contains(r.stderr, want) {
		t.Errorf("stderr = %q, want a tack: message mentioning %q", r.stderr, want)
	}
}

// assertDiagnostics checks r is a rejected program: exit 1, nothing on
// stdout, and rendered diagnostics, not a driver message, on stderr.
func assertDiagnostics(t *testing.T, r result) {
	t.Helper()
	if r.code != exitFailure {
		t.Errorf("exit code = %d, want %d", r.code, exitFailure)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty: diagnostics belong on stderr", r.stdout)
	}
	if !strings.Contains(r.stderr, "error:") || strings.HasPrefix(r.stderr, "tack:") {
		t.Errorf("stderr = %q, want rendered diagnostics", r.stderr)
	}
}

// assertOnlyStdout checks r succeeded, wrote nothing to stderr, and wrote
// something other than a banner to stdout.
func assertOnlyStdout(t *testing.T, r result) {
	t.Helper()
	if r.code != exitOK {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", r.code, r.stderr)
	}
	if r.stderr != "" {
		t.Errorf("stderr = %q, want empty", r.stderr)
	}
	if r.stdout == "" {
		t.Error("stdout is empty")
	}
	if strings.Contains(r.stdout, "=====") {
		t.Errorf("stdout contains a banner line:\n%s", r.stdout)
	}
	if !strings.HasSuffix(r.stdout, "\n") || strings.HasSuffix(r.stdout, "\n\n") {
		t.Errorf(
			"stdout should end in exactly one newline: %q",
			r.stdout[max(0, len(r.stdout)-20):],
		)
	}
}

func TestHelpListsEveryFlagAndExitsZero(t *testing.T) {
	fs, _ := newFlagSet()
	for _, arg := range []string{"-h", "--help", "-help", "--h"} {
		t.Run(arg, func(t *testing.T) {
			r := runTack(t, arg)
			if r.code != exitOK {
				t.Errorf("exit code = %d, want 0", r.code)
			}
			if r.stderr != "" {
				t.Errorf("stderr = %q, want empty", r.stderr)
			}
			fs.VisitAll(func(f *flag.Flag) {
				if !strings.Contains(r.stdout, "--"+f.Name) {
					t.Errorf("help does not list --%s:\n%s", f.Name, r.stdout)
				}
			})
			for _, want := range []string{"--help", "Exit status", "(default c)"} {
				if !strings.Contains(r.stdout, want) {
					t.Errorf("help does not mention %q", want)
				}
			}
		})
	}
}

func TestHelpWinsOverOtherArguments(t *testing.T) {
	r := runTack(t, "--emit", "--help", example("hello"))
	if r.code != exitOK || !strings.HasPrefix(r.stdout, "usage:") {
		t.Errorf("got exit %d, stdout %q; want help and exit 0", r.code, r.stdout)
	}
}

func TestBackendsListsRegistry(t *testing.T) {
	for _, args := range [][]string{
		{"--backends"},
		{"--backends", "--backend", "no-such-backend"},
		{"--backends", "--lex", "--ast", "ignored.tck", "extra"},
	} {
		r := runTack(t, args...)
		want := strings.Join(codegen.Names(), "\n") + "\n"
		if r.code != exitOK || r.stdout != want || r.stderr != "" {
			t.Errorf("run(%q) = %d, stdout %q, stderr %q; want 0, %q, \"\"",
				args, r.code, r.stdout, r.stderr, want)
		}
		if !strings.Contains(r.stdout, "c\n") {
			t.Errorf("--backends does not list c: %q", r.stdout)
		}
	}
}

func TestDumpsWriteOnlyContentToStdout(t *testing.T) {
	for _, flag := range []string{"--lex", "--ast", "--res", "--emit"} {
		for _, ex := range []string{"hello", "empty", "complete", "no-trailing-newline", "params"} {
			t.Run(flag+"/"+ex, func(t *testing.T) {
				assertOnlyStdout(t, runTack(t, flag, example(ex)))
			})
		}
	}
}

func TestASTAndResDumpsAreJSON(t *testing.T) {
	for _, flag := range []string{"--ast", "--res"} {
		r := runTack(t, flag, example("complete"))
		if !json.Valid([]byte(r.stdout)) {
			t.Errorf("%s output is not valid JSON:\n%s", flag, r.stdout)
		}
	}
}

// TestResOfRecursiveProgramIsNotSilentlyEmpty covers a program whose
// resolved tree the JSON encoder cannot render (resolution links a recursive
// call back to its function, a cycle). Whatever the dump does about that, it
// must not report success with empty output.
func TestResOfRecursiveProgramIsNotSilentlyEmpty(t *testing.T) {
	r := runTack(t, "--res", example("fib"))
	checkCoherent(t, r)
	if r.code == exitOK && !json.Valid([]byte(r.stdout)) {
		t.Errorf("--res succeeded with invalid JSON: %q", r.stdout)
	}
	if r.code != exitOK && !strings.HasPrefix(r.stderr, "tack: ") {
		t.Errorf("--res failed without a tack: message: %q", r.stderr)
	}
}

func TestEmitIsExactlyTheGeneratedCode(t *testing.T) {
	path := example("fib")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := readSource(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := compile(f, c.New(), dumpNone, 0)
	if err != nil {
		t.Fatal(err)
	}

	r := runTack(t, "--emit", path)
	if r.stdout != string(want) {
		t.Errorf(
			"--emit stdout differs from the generated code:\ngot:\n%s\nwant:\n%s",
			r.stdout,
			want,
		)
	}
	if len(src) == 0 || r.code != exitOK || r.stderr != "" {
		t.Errorf("exit %d, stderr %q, source %d bytes", r.code, r.stderr, len(src))
	}
}

func TestEmitRedirectedToFileIsValidC(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	r := runTack(t, "--emit", example("complete"))
	cFile := filepath.Join(t.TempDir(), "out.c")
	if err := os.WriteFile(cFile, []byte(r.stdout), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(t.Context(), "clang", "-fsyntax-only", cFile).CombinedOutput()
	if err != nil {
		t.Errorf("clang rejected --emit output: %v\n%s", err, out)
	}
}

func TestLexDumpPadsEveryOffsetToTheSameWidth(t *testing.T) {
	// Long enough that the last offset has three digits while the first
	// tokens have one, which is where per-token width bugs would show.
	src := "package main\n\nfunc main() {\n" +
		strings.Repeat("    print(\"padding\")\n", 6) + "}\n"
	r := runTack(t, "--lex", writeSource(t, []byte(src)))
	assertOnlyStdout(t, r)

	lines := strings.Split(strings.TrimSuffix(r.stdout, "\n"), "\n")
	width := len(strconv.Itoa(len(src)))
	if width != 3 {
		t.Fatalf("test source is %d bytes; want a three-digit length", len(src))
	}
	for _, line := range lines {
		start, end, ok := strings.Cut(strings.TrimPrefix(line, "["), ":")
		end, _, _ = strings.Cut(end, "]")
		if !ok || len(start) != width || len(end) != width {
			t.Errorf("line %q: offsets not padded to %d digits", line, width)
		}
	}
	if last := lines[len(lines)-1]; !strings.HasSuffix(last, "EOF") {
		t.Errorf("last token = %q, want EOF", last)
	}
}

func TestLexDumpOfEmptyFile(t *testing.T) {
	r := runTack(t, "--lex", writeSource(t, nil))
	if r.code != exitOK || r.stdout != "[0:0] EOF\n" || r.stderr != "" {
		t.Errorf("got exit %d, stdout %q, stderr %q; want 0, \"[0:0] EOF\\n\", \"\"",
			r.code, r.stdout, r.stderr)
	}
}

func TestFormatTokensEmpty(t *testing.T) {
	if got := formatTokens(nil); got != "" {
		t.Errorf("formatTokens(nil) = %q, want \"\"", got)
	}
}

func TestInvalidProgramsReportToStderrOnly(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join(examplesDir, "invalid", "*"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no invalid examples found: %v", err)
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			tmp := isolateTemp(t)
			out := filepath.Join(t.TempDir(), "bin")
			assertDiagnostics(t, runTack(t, "--out", out, filepath.Join(dir, "main.tck")))
			assertDiagnostics(t, runTack(t, "--emit", filepath.Join(dir, "main.tck")))
			if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("a rejected program produced %s", out)
			}
			assertEmptyDir(t, tmp)
		})
	}
}

func TestDumpStopsBeforeLaterStageErrors(t *testing.T) {
	// garbage fails in the lexer; no-main-func gets past the parser.
	assertDiagnostics(t, runTack(t, "--lex", example("invalid/garbage")))
	assertOnlyStdout(t, runTack(t, "--ast", example("invalid/no-main-func")))
	assertDiagnostics(t, runTack(t, "--res", example("invalid/no-main-func")))
}

func TestMaxDiags(t *testing.T) {
	path := example("invalid/garbage")
	all := strings.Count(runTack(t, "--emit", path).stderr, "error:")
	if all < 2 {
		t.Fatalf("garbage reports %d diagnostics; the test needs at least 2", all)
	}
	for _, tc := range []struct {
		arg  string
		want int
	}{
		{"0", all},
		{"1", 1},
		{strconv.Itoa(all), all},
		{strconv.Itoa(all + 100), all},
		{"9223372036854775807", all},
	} {
		r := runTack(t, "--max-diags", tc.arg, "--emit", path)
		assertDiagnostics(t, r)
		if got := strings.Count(r.stderr, "error:"); got != tc.want {
			t.Errorf("--max-diags %s printed %d diagnostics, want %d", tc.arg, got, tc.want)
		}
	}
}

func TestUsageErrors(t *testing.T) {
	hello := example("hello")
	for _, tc := range []struct {
		name string
		want string
		args []string
	}{
		{"no arguments", "no input file", nil},
		{"only flags", "no input file", []string{"--emit"}},
		{"empty file name", "empty", []string{"--emit", ""}},
		{"extra positional", "expected one input file", []string{"--emit", hello, hello}},
		{"flag after file", "flags must come before the file", []string{hello, "--emit"}},
		{"unknown flag", "bogus", []string{"--bogus", hello}},
		{"unknown single-dash flag", "-x", []string{"-x", hello}},
		{"unknown backend", `unknown backend "llvm"`, []string{"--backend", "llvm", "--emit", hello}},
		{"empty backend", `unknown backend ""`, []string{"--backend", "", "--emit", hello}},
		{"backend missing value", "backend", []string{"--backend"}},
		{"missing --out", "--out is required", []string{hello}},
		{"empty --out", "--out is required", []string{"--out", "", hello}},
		{"--out missing value", "expected one input file", []string{hello, "--out"}},
		{"two dumps", "--lex and --emit cannot be combined", []string{"--lex", "--emit", hello}},
		{"all dumps", "cannot be combined", []string{"--lex", "--ast", "--res", "--emit", hello}},
		{"dump with --out", "--out cannot be combined", []string{"--emit", "--out", "x", hello}},
		{"negative --max-diags", "--max-diags", []string{"--max-diags", "-1", "--emit", hello}},
		{"non-numeric --max-diags", "max-diags", []string{"--max-diags", "many", "--emit", hello}},
		{"overflowing --max-diags", "max-diags", []string{"--max-diags", "99999999999999999999", "--emit", hello}},
		{"bool flag with value", "emit", []string{"--emit=maybe", hello}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertUsageError(t, runTack(t, tc.args...), tc.want)
		})
	}
}

func TestUsageErrorsAreCheckedBeforeReadingTheFile(t *testing.T) {
	// The file does not exist, but the command line is wrong first.
	r := runTack(t, "--backend", "llvm", "--emit", "/nonexistent/main.tck")
	assertUsageError(t, r, "unknown backend")
}

func TestDoubleDashAllowsFileNamesStartingWithDash(t *testing.T) {
	src, err := os.ReadFile(example("hello"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "-hello.tck"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	assertOnlyStdout(t, runTack(t, "--emit", "--", "-hello.tck"))
}

func TestInputFileFailures(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{"missing file", filepath.Join(dir, "nope.tck"), "no such file"},
		{"missing directory", filepath.Join(dir, "nope", "main.tck"), "no such file"},
		{"directory", dir, "is a directory"},
		{"file as directory", filepath.Join(example("hello"), "main.tck"), "not a directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertFailure(t, runTack(t, "--emit", tc.path), tc.want)
		})
	}
}

func TestUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read any file")
	}
	path := writeSource(t, []byte("package main\n"))
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	assertFailure(t, runTack(t, "--emit", path), "permission denied")
}

func TestHostileSourceNeverPanics(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"empty", ""},
		{"single newline", "\n"},
		{"only whitespace", " \t \n\n\t"},
		{"invalid UTF-8", "package main\n\xff\xfe\x80"},
		{"invalid UTF-8 in string", "package main\nfunc main() { print(\"\xff\") }\n"},
		{"NUL bytes", "package main\x00\x00\nfunc main() {}\n"},
		{"lone continuation byte", "\x80"},
		{"BOM", "\xef\xbb\xbfpackage main\nfunc main() {}\n"},
		{"CRLF", "package main\r\nfunc main() {\r\n}\r\n"},
		{"truncated", "package main\nfunc main() { print(\"hi\""},
		{"deep nesting", "package main\nfunc main() {" + strings.Repeat("if true {", 2000) + "}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSource(t, []byte(tc.src))
			for _, flag := range []string{"--lex", "--ast", "--res", "--emit"} {
				checkCoherent(t, runTack(t, flag, path))
			}
		})
	}
}

func TestEmptyFileIsRejectedWithDiagnostics(t *testing.T) {
	assertDiagnostics(t, runTack(t, "--emit", writeSource(t, nil)))
}

func TestNonUTF8IsRejectedWithDiagnostics(t *testing.T) {
	path := writeSource(t, []byte("package main\n\xff\xfe\x80"))
	assertDiagnostics(t, runTack(t, "--emit", path))
}

// checkCoherent checks that r is internally consistent for a dump:
// success with output only on stdout, or failure with output only on
// stderr.
func checkCoherent(t *testing.T, r result) {
	t.Helper()
	switch r.code {
	case exitOK:
		if r.stderr != "" {
			t.Errorf("succeeded but wrote to stderr: %q", r.stderr)
		}
	case exitFailure:
		if r.stdout != "" || r.stderr == "" {
			t.Errorf("failed with stdout %q, stderr %q", r.stdout, r.stderr)
		}
	default:
		t.Errorf("exit code = %d, want 0 or 1", r.code)
	}
}

func FuzzRunEmit(f *testing.F) {
	paths, _ := filepath.Glob(filepath.Join(examplesDir, "*", "main.tck"))
	invalid, _ := filepath.Glob(filepath.Join(examplesDir, "invalid", "*", "main.tck"))
	for _, p := range append(paths, invalid...) {
		if src, err := os.ReadFile(p); err == nil {
			f.Add(src)
		}
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		path := writeSource(t, src)
		checkCoherent(t, runTack(t, "--emit", path))
	})
}

// errWriter is a writer that always fails.
type errWriter struct{}

// Write fails without writing anything.
func (errWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestStdoutWriteFailureIsReported(t *testing.T) {
	var stderr bytes.Buffer
	code := run(t.Context(), []string{"--emit", example("hello")}, errWriter{}, &stderr)
	if code != exitFailure || !strings.Contains(stderr.String(), "disk full") {
		t.Errorf("got exit %d, stderr %q; want 1 and the write error", code, stderr.String())
	}
}

func TestOutPathFailures(t *testing.T) {
	dir := t.TempDir()
	notDir := filepath.Join(dir, "file")
	if err := os.WriteFile(notDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	hello := example("hello")
	for _, tc := range []struct {
		name string
		out  string
		want string
	}{
		{"nonexistent directory", filepath.Join(dir, "missing", "bin"), "no such file"},
		{"parent is a file", filepath.Join(notDir, "bin"), "not a directory"},
		{"out is a directory", dir, "is a directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := isolateTemp(t)
			assertFailure(t, runTack(t, "--backend", copyBackend, "--out", tc.out, hello), tc.want)
			assertEmptyDir(t, tmp)
		})
	}
}

func TestOutCannotOverwriteInput(t *testing.T) {
	path := writeSource(t, []byte("package main\nfunc main() {}\n"))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assertUsageError(t, runTack(t, "--backend", copyBackend, "--out", path, path), "input file")
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Error("input file was modified")
	}
}

func TestBuildPassesGeneratedCodeToToolchainAndCleansUp(t *testing.T) {
	tmp := isolateTemp(t)
	out := filepath.Join(t.TempDir(), "bin")
	r := runTack(t, "--backend", copyBackend, "--out", out, example("fib"))
	if r.code != exitOK || r.stdout != "" || r.stderr != "" {
		t.Fatalf(
			"got exit %d, stdout %q, stderr %q; want a silent success",
			r.code,
			r.stdout,
			r.stderr,
		)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if want := runTack(t, "--emit", example("fib")).stdout; string(got) != want {
		t.Errorf("toolchain received:\n%s\nwant the --emit output:\n%s", got, want)
	}
	assertEmptyDir(t, tmp)
}

func TestFailingToolchainSurfacesStderrAndCleansUp(t *testing.T) {
	tmp := isolateTemp(t)
	r := runTack(
		t,
		"--backend",
		failBackend,
		"--out",
		filepath.Join(t.TempDir(), "bin"),
		example("hello"),
	)
	if r.code != exitFailure || r.stdout != "" {
		t.Errorf("got exit %d, stdout %q; want 1 and nothing on stdout", r.code, r.stdout)
	}
	if r.stderr != "toolchain-says-no\ntack: sh failed: exit status 3\n" {
		t.Errorf("stderr = %q, want the toolchain's failure reported", r.stderr)
	}
	assertEmptyDir(t, tmp)
}

func TestMissingToolchainFailsAndCleansUp(t *testing.T) {
	tmp := isolateTemp(t)
	r := runTack(
		t,
		"--backend",
		missingBackend,
		"--out",
		filepath.Join(t.TempDir(), "bin"),
		example("hello"),
	)
	assertFailure(t, r, "tack-test-cc")
	assertEmptyDir(t, tmp)
}

func TestCancelledBuildKillsToolchainAndCleansUp(t *testing.T) {
	tmp := isolateTemp(t)
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	r := runTackCtx(
		t,
		ctx,
		"--backend",
		sleepBackend,
		"--out",
		filepath.Join(t.TempDir(), "bin"),
		example("hello"),
	)
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("run took %s after cancellation; the toolchain was not killed", elapsed)
	}
	assertFailure(t, r, "sleep failed")
	assertEmptyDir(t, tmp)
}

func TestTempDirUnusableFails(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	r := runTack(
		t,
		"--backend",
		copyBackend,
		"--out",
		filepath.Join(t.TempDir(), "bin"),
		example("hello"),
	)
	assertFailure(t, r, "failed to create temp file")
}

// buildWithClang compiles examples/<name> with the real c backend into a
// fresh directory and returns the binary's path. It skips the test when
// clang is not on PATH and checks the build left no temp files.
func buildWithClang(t *testing.T, name string) string {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	tmp := isolateTemp(t)
	bin := filepath.Join(t.TempDir(), name)
	r := runTack(t, "--out", bin, example(name))
	if r.code != exitOK || r.stdout != "" || r.stderr != "" {
		t.Fatalf("build: exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	assertEmptyDir(t, tmp)
	return bin
}

func TestBuiltFibExits55(t *testing.T) {
	bin := buildWithClang(t, "fib")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 55 {
		t.Errorf("fib exited with %v, want status 55", err)
	}
}

func TestBuiltHelloPrints(t *testing.T) {
	bin := buildWithClang(t, "hello")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	if err != nil {
		t.Fatalf("hello failed: %v", err)
	}
	if string(out) != "Hello, World!\n" {
		t.Errorf("hello printed %q, want %q", out, "Hello, World!\n")
	}
}

func TestBuildOverwritesExistingOutput(t *testing.T) {
	bin := buildWithClang(t, "fib")
	r := runTack(t, "--out", bin, example("hello"))
	if r.code != exitOK {
		t.Fatalf("rebuild over existing output: exit %d, stderr %q", r.code, r.stderr)
	}
	if out, _ := exec.CommandContext(t.Context(), bin).Output(); string(out) != "Hello, World!\n" {
		t.Errorf("rebuilt binary printed %q, want hello's output", out)
	}
}

func TestBuiltBoolsPrintsAndExits9(t *testing.T) {
	bin := buildWithClang(t, "bools")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 9 {
		t.Errorf("bools exited with %v, want status 9", err)
	}
	if string(out) != "bools\n" {
		t.Errorf("bools printed %q, want %q", out, "bools\n")
	}
}
