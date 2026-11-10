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
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tacklang/tack/codegen"
	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/internal/invalid"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/source"
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
	optBackend     = "test-opt"     // "builds" by writing the level to --out
	objsBackend    = "test-objs"    // "builds" by writing the objects it links to --out
	includeBackend = "test-include" // "builds" by writing the include directory, and its tack.h, to --out
)

// fakeToolchain is a backend that generates C with the real c backend but
// builds with argv instead of clang. In argv, "{src}" and "{out}" are
// replaced by the build's source and output paths, "{opt}" by its
// optimization level, "{include}" by the build's include directory, and
// "{objs}", a whole element, by the paths of the
// objects it links, one element each. It compiles C files, the program's
// and std's, with clang, as the c backend does.
type fakeToolchain struct {
	codegen.Backend
	name string
	argv []string
}

// Name returns the name the fake is registered under.
func (f fakeToolchain) Name() string { return f.name }

// BuildCmd returns argv with the {src}, {out}, {opt}, {include} and
// {objs} placeholders filled in.
func (f fakeToolchain) BuildCmd(
	src, include string, objs []string, out string, opt codegen.Opt,
) []string {
	r := strings.NewReplacer(
		"{src}", src, "{out}", out, "{opt}", string(opt), "{include}", include)
	cmd := make([]string, 0, len(f.argv)+len(objs))
	for _, a := range f.argv {
		if a == "{objs}" {
			cmd = append(cmd, objs...)
			continue
		}
		cmd = append(cmd, r.Replace(a))
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
		{name: sleepBackend, argv: []string{"sleep", "300"}},
		{name: optBackend, argv: []string{"sh", "-c", `printf %s "$1" > "$2"`, "sh", "{opt}", "{out}"}},
		{name: includeBackend, argv: []string{
			"sh", "-c", `printf '%s\n' "$1" > "$2"; cat "$1/tack/tack.h" >> "$2"`, "sh", "{include}",
			"{out}",
		}},
		{name: objsBackend, argv: []string{
			"sh", "-c", `out=$1; shift; printf '%s\n' "$@" > "$out"`, "sh", "{out}", "{objs}",
		}},
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
	return runTackEnv(t, ctx, testEnv(t), args...)
}

// runTackEnv is runTackCtx in the environment env.
func runTackEnv(t *testing.T, ctx context.Context, env environment, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("run(%q) panicked: %v", args, r)
		}
	}()
	code := run(ctx, args, env, &stdout, &stderr)
	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

// testEnv is the environment every test runs the driver in unless it
// says otherwise: TACK_HOME is the repository's root, so std is the
// repository's own, whatever is installed; TACK_CACHE is a fresh
// directory, so no test reads or writes the user's cache or another
// test's; the toolchain is identified as the driver does it; and neither
// the home directory nor the user's cache directory is ever looked up,
// which fails the test if anything tries.
func testEnv(t testing.TB) environment {
	return environment{
		tackHome: stdtest.Home(),
		homeDir: func() (string, error) {
			t.Error("the driver looked up the home directory though TACK_HOME is set")
			return "", errors.New("no home directory in tests")
		},
		tackCache: t.TempDir(),
		userCacheDir: func() (string, error) {
			t.Error("the driver looked up the user cache directory though TACK_CACHE is set")
			return "", errors.New("no user cache directory in tests")
		},
		toolVersion: toolchainVersion,
	}
}

// example returns the path to examples/<name>/main.tck.
func example(name string) string {
	return filepath.Join(examplesDir, name, "main.tck")
}

// invalidCases returns the cases in examples/invalid.txtar.
func invalidCases(t testing.TB) []invalid.Case {
	t.Helper()
	cases, err := invalid.Load(invalid.Path)
	if err != nil {
		t.Fatal(err)
	}
	return cases
}

// seedCases returns the cases in examples/invalid.txtar and
// examples/warnings.txtar, the fuzzers' seeds besides the examples.
func seedCases(t testing.TB) []invalid.Case {
	t.Helper()
	warned, err := invalid.Load(invalid.WarningsPath)
	if err != nil {
		t.Fatal(err)
	}
	return append(invalidCases(t), warned...)
}

// invalidExample writes the program of the invalid case name to a fresh temp
// directory and returns its path.
func invalidExample(t *testing.T, name string) string {
	t.Helper()
	for _, tc := range invalidCases(t) {
		if tc.Name == name {
			return writeSource(t, tc.Source)
		}
	}
	t.Fatalf("no invalid case named %q", name)
	return ""
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
				name := "--" + f.Name
				if strings.HasPrefix(f.Name, "O") {
					name = "-" + f.Name
				}
				if !strings.Contains(r.stdout, name) {
					t.Errorf("help does not list %s:\n%s", name, r.stdout)
				}
			})
			for _, want := range []string{"--help", "Exit status", "(default c)", "(default -O3)"} {
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

// TestResOfRecursiveProgram covers a program whose resolved tree has a
// cycle: resolution links a recursive call back to its function. The dump
// must still succeed, as JSON, naming the callee by reference.
func TestResOfRecursiveProgram(t *testing.T) {
	r := runTack(t, "--res", example("fib"))
	assertOnlyStdout(t, r)
	if !json.Valid([]byte(r.stdout)) {
		t.Errorf("--res printed invalid JSON: %q", r.stdout)
	}
	if !strings.Contains(r.stdout, `"Kind": "Func"`) {
		t.Errorf("--res does not show the recursive call's Def as a reference:\n%s", r.stdout)
	}
}

func TestEmitIsExactlyTheGeneratedCode(t *testing.T) {
	path := example("fib")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	builtins := stdtest.Builtins(t)
	lib := &std{dir: stdtest.Dir()}
	res, single, err := load(path, dumpNone, lib, defaultMaxErrors)
	if err != nil {
		t.Fatal(err)
	}
	want, _, err := compile(res, single, c.New(), builtins, dumpNone, 0)
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

// TestEmitRedirectedToFileIsValidC checks --emit's output compiles on
// its own, given what any build of it needs: std's include directory on
// the include path, where it finds <tack/tack.h>.
func TestEmitRedirectedToFileIsValidC(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	r := runTack(t, "--emit", example("complete"))
	cFile := filepath.Join(t.TempDir(), "out.c")
	if err := os.WriteFile(cFile, []byte(r.stdout), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(
		t.Context(), "clang", "-fsyntax-only", "-I", stdtest.IncludeDir(), cFile,
	).CombinedOutput()
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
	for _, tc := range invalidCases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			path := writeSource(t, tc.Source)
			tmp := isolateTemp(t)
			out := filepath.Join(t.TempDir(), "bin")
			assertDiagnostics(t, runTack(t, "--out", out, path))
			assertDiagnostics(t, runTack(t, "--emit", path))
			if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("a rejected program produced %s", out)
			}
			assertEmptyDir(t, tmp)
		})
	}
}

func TestDumpStopsBeforeLaterStageErrors(t *testing.T) {
	// garbage fails in the lexer; no-main-func gets past the parser.
	noMain := invalidExample(t, "no-main-func")
	assertDiagnostics(t, runTack(t, "--lex", invalidExample(t, "garbage")))
	assertOnlyStdout(t, runTack(t, "--ast", noMain))
	assertDiagnostics(t, runTack(t, "--res", noMain))
}

func TestMaxDiags(t *testing.T) {
	path := invalidExample(t, "garbage")
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

// tooManyLine is the line the driver ends its diagnostics with when it
// left some out, for a limit of 10.
const tooManyLine = "too many errors: showing the first 10; --max-errors 0 shows them all\n"

// TestMaxErrors checks that the driver reports at most 10 errors by
// default, ending with a line saying it stopped, every one with
// --max-errors 0, n with --max-errors n, and that --max-diags is the same
// setting under its old name. The program has 15 independent syntax
// errors, each of which the parser reports once.
func TestMaxErrors(t *testing.T) {
	var b strings.Builder
	b.WriteString("package main\n\nfunc main() {\n")
	for range 15 {
		b.WriteString("    x := 1 1\n")
	}
	b.WriteString("}\n")
	path := writeSource(t, []byte(b.String()))
	for _, tc := range []struct {
		args []string
		want int
		more bool
	}{
		{nil, 10, true},
		{[]string{"--max-errors", "0"}, 15, false},
		{[]string{"--max-errors", "3"}, 3, true},
		{[]string{"--max-errors", "15"}, 15, false},
		{[]string{"--max-errors", "16"}, 15, false},
		{[]string{"--max-diags", "3"}, 3, true},
		{[]string{"--max-diags", "0"}, 15, false},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			r := runTack(t, append(tc.args, "--emit", path)...)
			assertDiagnostics(t, r)
			if got := strings.Count(r.stderr, ": error:"); got != tc.want {
				t.Errorf("printed %d errors, want %d:\n%s", got, tc.want, r.stderr)
			}
			more := strings.Contains(r.stderr, "too many errors")
			if more != tc.more {
				t.Errorf("too many errors line printed: %v, want %v:\n%s", more, tc.more, r.stderr)
			}
			if len(tc.args) == 0 && !strings.HasSuffix(r.stderr, tooManyLine) {
				t.Errorf("stderr ends %q, want it to end %q",
					r.stderr[max(0, len(r.stderr)-100):], tooManyLine)
			}
		})
	}
	bad := runTack(t, "--max-errors", "-1", "--emit", path)
	if bad.code != exitUsage ||
		!strings.Contains(bad.stderr, "--max-errors must be 0 or more, got -1") {
		t.Errorf("--max-errors -1: exit %d, stderr %q", bad.code, bad.stderr)
	}
}

// TestMaxErrorsCountsOnlyErrors checks that --max-errors counts errors
// alone: twelve warnings, past the default limit of 10, are all printed,
// with no line saying some were left out, and the run succeeds; and with
// errors among them, the report stops at the first error past the limit,
// the warnings before it printed, so no error is left out for a warning:
// with a limit of one, the first error and the three warnings before the
// second, which is left out.
func TestMaxErrorsCountsOnlyErrors(t *testing.T) {
	var b strings.Builder
	b.WriteString("package main\n\nfunc main() {\n")
	for i := range 12 {
		b.WriteString("    x" + strconv.Itoa(i) + " := 1\n")
	}
	b.WriteString("}\n")
	warned := runTack(t, "--emit", writeSource(t, []byte(b.String())))
	if warned.code != exitOK || warned.stdout == "" {
		t.Fatalf("12 warnings: exit %d, stdout %q, stderr %q",
			warned.code, warned.stdout, warned.stderr)
	}
	if got := strings.Count(warned.stderr, ": warning: declared and not used"); got != 12 {
		t.Errorf("12 warnings: printed %d warnings:\n%s", got, warned.stderr)
	}
	if strings.Contains(warned.stderr, "too many errors") {
		t.Errorf("12 warnings: the too many errors line printed:\n%s", warned.stderr)
	}

	// An unused local, a type error, two unused locals, and a type error.
	src := "package main\n\nfunc main() {\n" +
		"    a := 1\n    b := 1 + true\n    c := 1\n    d := 1\n    e := 1 + true\n}\n"
	r := runTack(t, "--max-errors", "1", "--emit", writeSource(t, []byte(src)))
	assertDiagnostics(t, r)
	if got := strings.Count(r.stderr, ": error:"); got != 1 {
		t.Errorf("printed %d errors, want 1:\n%s", got, r.stderr)
	}
	for _, w := range []string{"main.tck:4:5: warning", "main.tck:6:5: warning", "main.tck:7:5: warning"} {
		if !strings.Contains(r.stderr, w) {
			t.Errorf("stderr lacks %q:\n%s", w, r.stderr)
		}
	}
	more := "too many errors: showing the first 1; --max-errors 0 shows them all\n"
	if !strings.HasSuffix(r.stderr, more) {
		t.Errorf("stderr does not end %q:\n%s", more, r.stderr)
	}
}

// TestDumpsWithWarnings checks that a dump of a program with warnings is
// written, --res and --emit alone and with --check, the warnings going
// to stderr and the run succeeding.
func TestDumpsWithWarnings(t *testing.T) {
	path := writeSource(t, []byte("package main\n\nfunc main() {\n    x := 1\n}\n"))
	for _, args := range [][]string{
		{"--res"}, {"--emit"}, {"--check", "--res"}, {"--check", "--emit"}, {"--check"},
	} {
		r := runTack(t, append(args, path)...)
		warned := strings.Contains(r.stderr, "main.tck:4:5: warning: declared and not used: x")
		if r.code != exitOK || !warned {
			t.Errorf("%q: exit %d, stderr %q", args, r.code, r.stderr)
		}
		if (len(args) > 1 || args[0] != "--check") && r.stdout == "" {
			t.Errorf("%q: no dump", args)
		}
	}
}

// TestLaterStagesRunAfterErrors checks that an error one stage reports
// does not hide another stage's: a syntax error, a name that names
// nothing, which the resolver reports, and a type error, which the type
// checker does, in three functions of one file are all reported, once
// each, in the order they appear, with nothing generated.
func TestLaterStagesRunAfterErrors(t *testing.T) {
	src := "package main\n\n" +
		"func a() {\n    x := 1 1\n}\n\n" +
		"func b() {\n    print(nowhere)\n}\n\n" +
		"func c() {\n    var s string = 5\n    print(s)\n}\n\n" +
		"func main() {\n}\n"
	path := writeSource(t, []byte(src))
	for _, args := range [][]string{
		{"--emit", path},
		{"--out", filepath.Join(t.TempDir(), "bin"), path},
	} {
		r := runTack(t, args...)
		assertDiagnostics(t, r)
		want := []string{
			"main.tck:4:12: error:",
			"main.tck:8:11: error: undefined: nowhere",
			"main.tck:12:20: error:",
		}
		last := -1
		for _, w := range want {
			at := strings.Index(r.stderr, w)
			if at < 0 {
				t.Errorf("%q: stderr lacks %q:\n%s", args, w, r.stderr)
				continue
			}
			if at < last {
				t.Errorf("%q: %q is out of order:\n%s", args, w, r.stderr)
			}
			last = at
		}
		if got := strings.Count(r.stderr, ": error:"); got != len(want) {
			t.Errorf("%q: %d errors, want %d:\n%s", args, got, len(want), r.stderr)
		}
	}
}

// TestResDumpReportsEveryFrontEndError checks that --res, which stops
// after the resolver, reports the parser's errors and the resolver's
// together, and none of the type checker's.
func TestResDumpReportsEveryFrontEndError(t *testing.T) {
	src := "package main\n\nfunc a() {\n    x := 1 1\n}\n\n" +
		"func main() {\n    print(nowhere)\n    var s string = 5\n    print(s)\n}\n"
	r := runTack(t, "--res", writeSource(t, []byte(src)))
	assertDiagnostics(t, r)
	if !strings.Contains(r.stderr, "main.tck:4:12: error:") ||
		!strings.Contains(r.stderr, "error: undefined: nowhere") {
		t.Errorf("stderr lacks the parser's or the resolver's error:\n%s", r.stderr)
	}
	if got := strings.Count(r.stderr, ": error:"); got != 2 {
		t.Errorf("%d errors, want 2, the type checker's left out:\n%s", got, r.stderr)
	}
}

// TestErrorsSortedByPosition checks that the errors of one file are
// reported in the order they appear in it.
func TestErrorsSortedByPosition(t *testing.T) {
	src := "package main\n\nfunc a() {\n    x := 1 1\n}\n\nfunc (\n\nfunc b() {\n    y := 2 2\n}\n"
	r := runTack(t, "--max-errors", "0", "--emit", writeSource(t, []byte(src)))
	assertDiagnostics(t, r)
	last := 0
	for _, line := range strings.Split(r.stderr, "\n") {
		_, rest, ok := strings.Cut(line, "main.tck:")
		if !ok || !strings.Contains(rest, ": error:") {
			continue
		}
		n, _ := strconv.Atoi(strings.SplitN(rest, ":", 2)[0])
		if n < last {
			t.Errorf("error at line %d after one at line %d:\n%s", n, last, r.stderr)
		}
		last = n
	}
	if last == 0 {
		t.Fatalf("no errors found in %q", r.stderr)
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
		{"two levels", "-O0 and -O2 cannot be combined", []string{"-O0", "-O2", "--out", "x", hello}},
		{"level with a dump", "-O2 cannot be combined with a dump flag", []string{"-O2", "--emit", hello}},
		{"unknown level", "O4", []string{"-O4", "--out", "x", hello}},
		{"fast is not a level", "Ofast", []string{"-Ofast", "--out", "x", hello}},
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
	dir := writeDir(t, map[string]string{"tack.mod": "module m\n"})
	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{"missing file", filepath.Join(dir, "nope.tck"), "no such file"},
		{"missing directory", filepath.Join(dir, "nope", "main.tck"), "no such file"},
		{"directory with no Tack files", dir, "no .tck files in " + dir},
		{"directory with no tack.mod", t.TempDir(), "no tack.mod found in "},
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
		{"truncated range", "package main\nfunc main() {\n    for i, mut x, := range"},
		{
			"deep range nesting",
			"package main\nfunc main() {\n" + strings.Repeat("for range \"ab\" {\n", 500) +
				strings.Repeat("}\n", 501),
		},
		{"truncated switch", "package main\nfunc main() {\n    switch f() {\n    case 1, "},
		{
			"deep switch nesting",
			"package main\nfunc main() {\n    n := 1\n" +
				strings.Repeat("switch n {\ncase 1:\n", 500) + strings.Repeat("}\n", 501),
		},
		{"truncated defer", "package main\nfunc main() {\n    defer {\n        if f("},
		{"truncated extern", "package main\nextern func f(a int64"},
		{"extern alone", "package main\nextern"},
		{"truncated C name", "package main\nextern(\"abc"},
		{"C name with odd bytes", "package main\nextern(\"\xff\x00\") func f()\nfunc main() { f() }\n"},
		{"pub extern truncated", "package util\npub extern(\"x\")"},
		{"extern with a body", "package main\nextern func f() {"},
		{"many externs", "package main\n" + strings.Repeat("extern func f()\n", 500) + "func main() {}\n"},
		{"defer of nothing", "package main\nfunc main() {\n    defer\n"},
		{
			"deep defer nesting",
			"package main\nfunc main() {\n" + strings.Repeat("defer {\nprint(\"x\")\n", 500) +
				strings.Repeat("}\n", 501),
		},
		{
			"deep loops in a defer",
			"package main\nfunc main() {\ndefer {\n" + strings.Repeat("for range 2 {\nprint(\"x\")\n", 500) +
				strings.Repeat("break\n}\n", 500) + "}\n}\n",
		},
		{
			"deep defers with returns",
			"package main\nfunc f() int64 {\n" + strings.Repeat("if true {\ndefer { print(\"x\") }\nreturn 1\n", 300) +
				strings.Repeat("}\n", 300) + "return 0\n}\nfunc main() {\n    code, fits := int32(f())\n    if fits { exit(code) }\n}\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSource(t, []byte(tc.src))
			for _, flag := range []string{"--lex", "--ast", "--res", "--emit"} {
				checkCoherent(t, runTack(t, flag, path))
			}
		})
	}
}

// TestHostileFmtWriteSourceNeverPanics runs every dump and --emit on a
// truncated call to std/fmt's write, two thousand calls, and one call
// with the bytes of a 200 KB string, and checks each result is coherent,
// never a panic.
func TestHostileFmtWriteSourceNeverPanics(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"truncated fmt.write", "package main\nimport fmt from \"std/fmt\"\nfunc main() {\n    fmt.write([]uint8("},
		{
			"many fmt.writes",
			"package main\nimport fmt from \"std/fmt\"\nfunc main() {\n" +
				strings.Repeat("    fmt.write([]uint8(\"x\\x00\\xff\"))\n", 2000) + "}\n",
		},
		{
			"fmt.write of a huge string's bytes",
			"package main\nimport fmt from \"std/fmt\"\nfunc main() {\n    fmt.write([]uint8(\"" +
				strings.Repeat("ab", 100000) + "\"))\n}\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSource(t, []byte(tc.src))
			for _, flag := range []string{"--lex", "--ast", "--res", "--emit"} {
				checkCoherent(t, runTack(t, flag, path))
			}
		})
	}
}

// TestHostileVariadicSourceNeverPanics runs every dump and --emit on
// malformed and extreme uses of ..., truncated, repeated, with thousands
// of values and nested five hundred calls deep, and checks each result is
// coherent, never a panic.
func TestHostileVariadicSourceNeverPanics(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"truncated variadic", "package main\nfunc f(a ..."},
		{"truncated spread", "package main\nfunc f(a ...int64) {}\nfunc main() {\n    f(a..."},
		{"ellipses alone", "package main\nfunc f(" + strings.Repeat("...", 300) + ")"},
		{
			"many variadic values",
			"package main\nfunc f(a ...int64) {}\nfunc main() {\n    f(" +
				strings.Repeat("1, ", 5000) + "1)\n}\n",
		},
		{
			"deep variadic nesting",
			"package main\nfunc f(a ...int64) int64 {\n    return 0\n}\nfunc main() {\n    x := " +
				strings.Repeat("f(1, ", 500) + "1" + strings.Repeat(")", 500) + "\n    exit(int32(x))\n}\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSource(t, []byte(tc.src))
			for _, flag := range []string{"--lex", "--ast", "--res", "--emit"} {
				checkCoherent(t, runTack(t, flag, path))
			}
		})
	}
}

// TestHostileTypeSwitchSourceNeverPanics runs every dump and --emit on
// malformed and extreme type assertions and type switches, truncated,
// nested five hundred deep, with thousands of cases and a chain of
// thousands of assertions, and checks each result is coherent, never a
// panic.
func TestHostileTypeSwitchSourceNeverPanics(t *testing.T) {
	head := "package main\nfunc f(a any) {\n"
	var nested strings.Builder
	nested.WriteString(head + "    v0 := a\n")
	for i := range 500 {
		n, prev := strconv.Itoa(i+1), strconv.Itoa(i)
		nested.WriteString("switch v" + n + " := v" + prev + ".(type) {\ncase int64:\n    f(v" + n +
			")\ndefault:\n")
	}
	nested.WriteString(strings.Repeat("}\n", 501) + "func main() {\n    f(1)\n}\n")
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"truncated assertion", head + "    v, ok := a.("},
		{"truncated type", head + "    v, ok := a.(int64"},
		{"truncated .(type", head + "    switch a.(type"},
		{"truncated binding", head + "    switch v :="},
		{"truncated binding tag", head + "    switch v := a.("},
		{"truncated case", head + "    switch v := a.(type) {\n    case int64, "},
		{"type keyword alone", head + "    v, ok := a.type"},
		{"assertion of nothing", head + "    v, ok := .(int64)"},
		{"assertion of a literal", head + "    v, ok := 1.(int64)\n}\n"},
		{"assertion of a call", head + "    v, ok := f(a).(int64)\n}\n"},
		{"assertion as a statement", head + "    a.(int64)\n}\nfunc main() {}\n"},
		{"odd bytes in the type", head + "    v, ok := a.(\xff\x00)\n}\n"},
		{"deep type switch nesting", nested.String()},
		{
			"deep assertion chain",
			head + "    v, ok := a" + strings.Repeat(".(int64)", 3000) + "\n    f(v)\n    f(ok)\n}\n" +
				"func main() {}\n",
		},
		{
			"many cases",
			head + "    switch a.(type) {\n" + strings.Repeat("case int64, string:\n", 3000) +
				"    }\n}\nfunc main() {}\n",
		},
		{
			"many types in one case",
			head + "    switch v := a.(type) {\n    case " + strings.Repeat("bool, ", 3000) +
				"bool:\n        f(v)\n    }\n}\nfunc main() {}\n",
		},
		{
			"type switch in a defer in a loop",
			head + "    for range 3 {\n        defer {\n            switch v := a.(type) {\n" +
				"            case int64:\n                f(v)\n                break\n            }\n" +
				"        }\n    }\n}\nfunc main() {}\n",
		},
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
// success with output on stdout and nothing but warnings on stderr, or
// failure with output only on stderr.
func checkCoherent(t *testing.T, r result) {
	t.Helper()
	switch r.code {
	case exitOK:
		if !onlyWarnings(r.stderr) {
			t.Errorf("succeeded but wrote more than warnings to stderr: %q", r.stderr)
		}
	case exitFailure:
		if r.stdout != "" || r.stderr == "" {
			t.Errorf("failed with stdout %q, stderr %q", r.stdout, r.stderr)
		}
	default:
		t.Errorf("exit code = %d, want 0 or 1", r.code)
	}
}

// onlyWarnings reports whether stderr, what a run that succeeded wrote
// there, is nothing, or warnings and no error.
func onlyWarnings(stderr string) bool {
	if stderr == "" {
		return true
	}
	return strings.Contains(stderr, ": warning: ") && !strings.Contains(stderr, ": error: ")
}

// FuzzRunEmit runs the driver's --emit on arbitrary source as a single
// file and checks the result is coherent, never a panic. It is seeded with
// every Tack file under examples/, each alone, and every case of
// examples/invalid.txtar and examples/warnings.txtar.
func FuzzRunEmit(f *testing.F) {
	paths, _ := filepath.Glob(filepath.Join(examplesDir, "*", "*.tck"))
	for _, p := range paths {
		if src, err := os.ReadFile(p); err == nil {
			f.Add(src)
		}
	}
	for _, tc := range seedCases(f) {
		f.Add(tc.Source)
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
	code := run(t.Context(), []string{"--emit", example("hello")}, testEnv(t), errWriter{}, &stderr)
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

// TestOptLevelReachesTheToolchain checks the level the command line asks
// for, -O3 when it asks for none, is the one the build command gets.
func TestOptLevelReachesTheToolchain(t *testing.T) {
	for _, tc := range []struct {
		want  string
		flags []string
	}{
		{"3", nil},
		{"0", []string{"-O0"}},
		{"1", []string{"-O1"}},
		{"2", []string{"-O2"}},
		{"3", []string{"-O3"}},
		{"s", []string{"-Os"}},
		{"z", []string{"--Oz"}},
	} {
		t.Run(strings.Join(append([]string{"default"}, tc.flags...), " "), func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "bin")
			args := append(
				append([]string{}, tc.flags...),
				"--backend",
				optBackend,
				"--out",
				out,
				example("fib"),
			)
			if r := runTack(t, args...); r.code != exitOK || r.stdout != "" || r.stderr != "" {
				t.Fatalf(
					"exit %d, stdout %q, stderr %q; want a silent success",
					r.code,
					r.stdout,
					r.stderr,
				)
			}
			if got, err := os.ReadFile(out); err != nil {
				t.Fatal(err)
			} else if string(got) != tc.want {
				t.Errorf("toolchain got level %q, want %q", got, tc.want)
			}
		})
	}
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

// TestCancelledBuildKillsToolchainAndCleansUp cancels a build while its
// toolchain hangs and checks the toolchain is killed and the temp dir
// removed. The toolchain sleeps 300 seconds, so a run that ends within 60
// is one that killed it, however loaded the machine is. The program calls no builtin, so the hanging toolchain is the
// build's first command: with one that called a builtin, compiling std's C
// into an empty cache could outlast the 200ms deadline on a loaded
// machine, and the cancellation would kill clang instead.
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
		example("empty"),
	)
	if elapsed := time.Since(start); elapsed > 60*time.Second {
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
	assertFailure(t, r, "failed to create temp directory")
}

// buildWithClang compiles examples/<name> with the real c backend into a
// fresh directory and returns the binary's path. It skips the test when
// clang is not on PATH and checks the build left no temp files.
func buildWithClang(t *testing.T, name string) string {
	t.Helper()
	return buildTargetWithClang(t, name, example(name))
}

// buildTargetWithClang is buildWithClang for any target the driver takes,
// a file or a directory, writing the binary under the name name.
func buildTargetWithClang(t *testing.T, name, target string) string {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	tmp := isolateTemp(t)
	bin := filepath.Join(t.TempDir(), name)
	r := runTack(t, "--out", bin, target)
	if r.code != exitOK || r.stdout != "" || r.stderr != "" {
		t.Fatalf("build: exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	assertEmptyDir(t, tmp)
	return bin
}

// The unused-warnings example declares a local it never reads, imports
// a package it never uses, and drops the value of a call of len. Each is
// a warning: the build and --check exit 0, print nothing on stdout and
// the three warnings on stderr, and the program runs.
func TestBuiltUnusedWarningsRuns(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want := []string{
		"main.tck:7:8: warning: imported and not used: fmt",
		"main.tck:11:5: warning: declared and not used: count",
		"main.tck:12:5: warning: len(l) is not used",
	}
	checkWarnings := func(what string, r result) {
		t.Helper()
		if r.code != exitOK || r.stdout != "" {
			t.Fatalf("%s: exit %d, stdout %q, stderr %q", what, r.code, r.stdout, r.stderr)
		}
		for _, w := range want {
			if !strings.Contains(r.stderr, w) {
				t.Errorf("%s: stderr lacks %q:\n%s", what, w, r.stderr)
			}
		}
		if n := strings.Count(r.stderr, "warning:"); n != len(want) {
			t.Errorf("%s: %d warnings, want %d:\n%s", what, n, len(want), r.stderr)
		}
		if strings.Contains(r.stderr, "error") {
			t.Errorf("%s: an error among the warnings:\n%s", what, r.stderr)
		}
	}
	checkWarnings("--check", runTack(t, "--check", example("unused-warnings")))

	isolateTemp(t)
	bin := filepath.Join(t.TempDir(), "unused-warnings")
	checkWarnings("build", runTack(t, "--out", bin, example("unused-warnings")))
	out, err := exec.CommandContext(t.Context(), bin).Output()
	if err != nil {
		t.Errorf("unused-warnings exited with %v, want status 0", err)
	}
	if string(out) != "ran\n" {
		t.Errorf("unused-warnings printed %q, want %q", out, "ran\n")
	}
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

// The results example binds each function's results by position, with mut
// on one name only, and exits with a status built from all of them.
func TestBuiltResultsBindsEveryResultAndExits48(t *testing.T) {
	bin := buildWithClang(t, "results")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 48 {
		t.Errorf("results exited with %v, want status 48", err)
	}
	if string(out) != "negative\n" {
		t.Errorf("results printed %q, want %q", out, "negative\n")
	}
}

// The results-assign example reassigns several results at once, including
// in a loop's post statement, which has to run the call once per iteration.
func TestBuiltResultsAssignExits87(t *testing.T) {
	bin := buildWithClang(t, "results-assign")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 87 {
		t.Errorf("results-assign exited with %v, want status 87", err)
	}
}

// The blank example discards one result of each call with _.
func TestBuiltBlankExits62(t *testing.T) {
	bin := buildWithClang(t, "blank")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 62 {
		t.Errorf("blank exited with %v, want status 62", err)
	}
}

// The widths example wraps at 8, 16 and 32 bits, widens without loss, and
// uses the largest uint64, exiting with a status computed in uint16.
func TestBuiltWidthsExits54(t *testing.T) {
	bin := buildWithClang(t, "widths")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 54 {
		t.Errorf("widths exited with %v, want status 54", err)
	}
}

// The division example divides only where the divisor is proven non-zero,
// and uses q, ok := a / b where it cannot be.
func TestBuiltDivisionExits46(t *testing.T) {
	bin := buildWithClang(t, "division")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 46 {
		t.Errorf("division exited with %v, want status 46", err)
	}
}

// The arrays example copies arrays by value, compares them, indexes them
// with constants, and takes lengths of arrays and strings.
func TestBuiltArraysExits29(t *testing.T) {
	bin := buildWithClang(t, "arrays")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 29 {
		t.Errorf("arrays exited with %v, want status 29", err)
	}
}

// The indexing example indexes with variables proven in range by a range
// loop, a guard and an index's type, and uses v, ok := a[i] for indexes
// that are not.
func TestBuiltIndexingExits70(t *testing.T) {
	bin := buildWithClang(t, "indexing")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 70 {
		t.Errorf("indexing exited with %v, want status 70", err)
	}
}

// The chars example uses character literals as uint8 constants: in
// arithmetic, comparisons, indexes, array lengths and package-level
// initializers, and widened to int32 for exit; a mutable copy of one wraps
// at run time when tripled. Hex escapes give their bytes, in a character
// literal and in a string.
func TestBuiltCharsExits97(t *testing.T) {
	bin := buildWithClang(t, "chars")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 97 {
		t.Errorf("chars exited with %v, want status 97", err)
	}
}

// The string-index example reads bytes of strings at indexes proven in range
// by a range loop, guards on len and a constant index into a literal, and
// with v, ok := s[i] where they are not.
func TestBuiltStringIndexExits5(t *testing.T) {
	bin := buildWithClang(t, "string-index")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 5 {
		t.Errorf("string-index exited with %v, want status 5", err)
	}
}

// print writes exactly its argument, so lines in the print example are only
// broken where the program writes a newline itself.
func TestBuiltPrintWritesNoNewlineOfItsOwn(t *testing.T) {
	bin := buildWithClang(t, "print")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	if err != nil {
		t.Fatalf("print failed: %v", err)
	}
	want := "one line, three prints\n*\n**\n***\ntab\tand\nbreak\n"
	if string(out) != want {
		t.Errorf("print printed %q, want %q", out, want)
	}
}

// The logical example prints only the operands that ran, so its output pins
// short-circuit evaluation in a real build.
func TestBuiltLogicalShortCircuitsAndExits13(t *testing.T) {
	bin := buildWithClang(t, "logical")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 13 {
		t.Errorf("logical exited with %v, want status 13", err)
	}
	if string(out) != "first\nsecond\n" {
		t.Errorf("logical printed %q, want %q", out, "first\nsecond\n")
	}
}

// The loops example prints three ticks before breaking out of its loop, then
// exits with the tenth prime.
func TestBuiltLoopsTicksAndExits29(t *testing.T) {
	bin := buildWithClang(t, "loops")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 29 {
		t.Errorf("loops exited with %v, want status 29", err)
	}
	if string(out) != "tick\ntick\ntick\n" {
		t.Errorf("loops printed %q, want three ticks", out)
	}
}

// endlessCases are the cases of examples/endless-loops whose loops never
// end; see that example for what each one runs.
var endlessCases = []string{
	"clause", "clause-continue", "cond", "cond-call", "bare", "bare-break",
	"range-int", "range-array", "range-string", "range-view", "range-list",
	"range-map", "nested-inner", "nested-outer", "defer", "global",
	"exit-inside", "exit-before", "generic", "guarded",
}

// endlessGrace is how long a case of examples/endless-loops must still be
// running for. A loop the C compiler assumed to end is deleted, so the
// case returns, or reads out of range, at once.
const endlessGrace = 500 * time.Millisecond

// TestBuiltEndlessLoopsNeverEnd builds examples/endless-loops at every
// optimization level twice, and runs each case of both builds at once,
// all of them together: every case whose loop never ends must still be
// running, with nothing printed, when endlessGrace has passed, and the
// control case, whose loop ends, must print "ended" and exit 0. A loop the
// generated C lets the C compiler assume ends, one whose body does nothing
// observable and whose controlling expression is not a constant, is
// deleted at -O1 and above, and the code after it runs, or, when the loop
// was the only way on, whatever code comes next. The first build is the
// driver's own, with -fno-finite-loops. The second runs every clang
// through a shim that drops that flag, so the loop lowering, the first
// layer, is checked at run time on its own, and a regression in it is not
// hidden by the flag.
func TestBuiltEndlessLoopsNeverEnd(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			withFlag := buildEndless(t, opt)
			withoutFlag := buildEndlessWithoutFlag(t, opt)
			running := append(
				startEndlessAll(t, "with -fno-finite-loops", withFlag),
				startEndlessAll(t, "without -fno-finite-loops", withoutFlag)...)
			time.Sleep(endlessGrace)
			for _, run := range running {
				run.requireRunning(t)
			}
			for _, bin := range []string{withFlag, withoutFlag} {
				if out, code := runBuiltArgs(t, bin, "ends"); out != "ended\n" || code != 0 {
					t.Errorf("%s ends printed %q and exited %d, want %q and 0",
						bin, out, code, "ended\n")
				}
			}
		})
	}
}

// buildEndlessWithoutFlag is buildEndless with every clang run through a
// shim that drops -fno-finite-loops, for the rest of t, and checks clang
// ran through it, never with the flag, and that one of those runs was the
// link, the step that compiles the generated C.
func buildEndlessWithoutFlag(t *testing.T, opt codegen.Opt) string {
	t.Helper()
	ran := shimClang(t, "-fno-finite-loops")
	bin := buildEndless(t, opt)
	linked := false
	for _, argv := range ran() {
		if slices.Contains(argv, "-fno-finite-loops") {
			t.Fatalf("the shim left -fno-finite-loops in clang %q", argv)
		}
		linked = linked || clangRunKind(argv) == "link"
	}
	if !linked {
		t.Fatal("the build without -fno-finite-loops ran no link through the shim")
	}
	return bin
}

// startEndlessAll starts bin on every case of endlessCases, each named
// for messages by label and the case.
func startEndlessAll(t *testing.T, label, bin string) []*endlessRun {
	t.Helper()
	running := make([]*endlessRun, len(endlessCases))
	for i, name := range endlessCases {
		running[i] = startEndless(t, bin, name)
		running[i].name = label + ": " + name
	}
	return running
}

// buildEndless builds examples/endless-loops at opt into a fresh
// directory, with whatever clang is first on PATH, and returns the
// binary's path.
func buildEndless(t *testing.T, opt codegen.Opt) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "endless-loops")
	r := runTack(t, "-O"+string(opt), "--out", bin, example("endless-loops"))
	if r.code != exitOK || r.stdout != "" || r.stderr != "" {
		t.Fatalf("build: exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	return bin
}

// endlessRun is one case of examples/endless-loops started by
// startEndless: its name, the process, what it printed, and a channel
// that receives the process's end.
type endlessRun struct {
	cmd  *exec.Cmd
	out  *bytes.Buffer
	done chan error
	name string
}

// startEndless starts the binary bin on the case name, with its standard
// output and error collected together, and waits for its end in the
// background.
func startEndless(t *testing.T, bin, name string) *endlessRun {
	t.Helper()
	run := &endlessRun{
		name: name,
		cmd:  exec.CommandContext(t.Context(), bin, name),
		out:  &bytes.Buffer{},
		done: make(chan error, 1),
	}
	run.cmd.Stdout, run.cmd.Stderr = run.out, run.out
	if err := run.cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", name, err)
	}
	go func() { run.done <- run.cmd.Wait() }()
	return run
}

// requireRunning fails the test when the case has ended, and otherwise
// kills it, fails the test when it ended some other way than by that
// kill, in the moment between the check and the kill, and fails it when
// it printed anything, which only code after its loop does.
func (run *endlessRun) requireRunning(t *testing.T) {
	t.Helper()
	select {
	case err := <-run.done:
		t.Errorf("%s ended with %v and printed %q, want it still running", run.name, err, run.out)
		return
	default:
	}
	if err := run.cmd.Process.Kill(); err != nil {
		t.Fatalf("killing %s: %v", run.name, err)
	}
	var exitErr *exec.ExitError
	if err := <-run.done; !errors.As(err, &exitErr) || exitErr.ExitCode() != -1 {
		t.Errorf("%s ended with %v, want it killed", run.name, err)
	}
	if run.out.Len() != 0 {
		t.Errorf("%s printed %q, want nothing", run.name, run.out)
	}
}

// endedLimit bounds the run of a case of examples/endless-loops whose loop
// must end, so a loop that does not fails the test instead of hanging it.
const endedLimit = 5 * time.Second

// runBuiltArgs runs the binary bin with the arguments args and returns
// what it printed on standard output and its exit status. It kills the
// binary and fails the test when it is still running after endedLimit.
func runBuiltArgs(t *testing.T, bin string, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), endedLimit)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	var exitErr *exec.ExitError
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		t.Fatalf("%s %v was still running after %v, want it to end", bin, args, endedLimit)
		return "", 0
	case err == nil:
		return string(out), 0
	case errors.As(err, &exitErr):
		return string(out), exitErr.ExitCode()
	default:
		t.Fatalf("running %s: %v", bin, err)
		return "", 0
	}
}

// The compound example applies every compound assignment, then ++ and --,
// so its status pins what each one computes in a real build.
func TestBuiltCompoundExits14(t *testing.T) {
	bin := buildWithClang(t, "compound")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 14 {
		t.Errorf("compound exited with %v, want status 14", err)
	}
}

// The negate example uses the smallest int literal and negates through a
// function, the smallest int64 included, which wraps to itself, so its
// status pins negation in a real build.
func TestBuiltNegateExits19(t *testing.T) {
	bin := buildWithClang(t, "negate")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 19 {
		t.Errorf("negate exited with %v, want status 19", err)
	}
}

// The ranges example ranges over arrays and strings: the index proven in
// range for indexing, a body that writes and replaces the array it walks
// while later iterations still see the original elements, ranged calls that
// must run once, empty arrays and strings, nested ranges with break and
// continue, and an early return, so its status pins each of them in a real
// build.
func TestBuiltRangesExits42(t *testing.T) {
	bin := buildWithClang(t, "ranges")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
		t.Errorf("ranges exited with %v, want status 42", err)
	}
}

// The string-globals example computes every comparison of string
// constants at package level at compile time and checks each value, so
// its status is 42 only if all of them hold.
func TestBuiltStringGlobalsExits42(t *testing.T) {
	bin := buildWithClang(t, "string-globals")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
		t.Errorf("string-globals exited with %v, want status 42", err)
	}
}

// The computed-globals example initializes globals with fields and
// elements of immutable globals, len, conversions, comparisons and logical
// operators on known values, and checks each value, and uses fields and
// elements of immutable globals as known values in functions, so its
// status is 42 only if all of them hold.
func TestBuiltComputedGlobalsExits42(t *testing.T) {
	bin := buildWithClang(t, "computed-globals")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
		t.Errorf("computed-globals exited with %v, want status 42", err)
	}
}

// The comments example puts comments before the package clause, after
// every statement form, inside expressions, argument lists, result lists
// and array literals that span lines, and at the end of the file with no
// newline after it, so its status pins that none of them changes what the
// program does.
func TestBuiltCommentsExits37(t *testing.T) {
	bin := buildWithClang(t, "comments")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 37 {
		t.Errorf("comments exited with %v, want status 37", err)
	}
}

// intLiteralsWant is what examples/int-literals prints when every check
// passes.
var intLiteralsWant = "hexadecimal ok\n" +
	"binary ok\n" +
	"octal ok\n" +
	"separators ok\n" +
	"a typed global ok\n" +
	"the largest int64 ok\n" +
	"the smallest int64 ok\n" +
	"the largest uint64 ok\n" +
	"zero and a byte ok\n"

// The int-literals example writes integers in hexadecimal, binary and
// octal, with separators, at the edges of int64 and uint64, and checks
// each is the value its decimal form is.
func TestBuiltIntLiteralsPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "int-literals")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	if err != nil || string(out) != intLiteralsWant {
		t.Errorf("int-literals printed\n%s\nwith %v, want\n%s", out, err, intLiteralsWant)
	}
}

// blockCommentsWant is what examples/block-comments prints when every
// check passes.
var blockCommentsWant = "between operands ok\n" +
	"a comment spanning lines ends a statement ok\n" +
	"a comment on one line before a newline ok\n" +
	"a comment spanning lines acts as a newline ok\n" +
	"inside an argument list ok\n" +
	"spanning lines after a comma ok\n" +
	"after a division ok\n" +
	"slashes inside a block comment ok\n" +
	"slash-star inside a line comment ok\n" +
	"empty comments ok\n" +
	"nested three deep ok\n"

// The block-comments example puts block comments before the package
// clause, between operands, inside argument lists, after return, nested,
// spanning lines where they end a statement, on one line before a
// newline, around commented-out code, and at the end of the file with no
// newline after it, and checks each leaves the program as written.
func TestBuiltBlockCommentsPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "block-comments")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	if err != nil || string(out) != blockCommentsWant {
		t.Errorf("block-comments printed\n%s\nwith %v, want\n%s", out, err, blockCommentsWant)
	}
}

// The self-compare example compares a parameter with itself, which is valid
// Tack and makes clang warn about the generated C. buildWithClang requires
// a silent build, so this pins that no C warning reaches the user, and the
// status pins that the comparisons still come out right.
func TestBuiltSelfCompareIsSilentAndExits7(t *testing.T) {
	bin := buildWithClang(t, "self-compare")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Errorf("self-compare exited with %v, want status 7", err)
	}
}

// The structs example declares structs, one used before its declaration,
// and checks that copies are independent, that structs pass and return by
// value, several results included, that nested fields and elements of
// arrays of structs can be written, that a range over an array of structs
// sees each element, that zero values and == agree, and that arithmetic on
// a field wraps at its width, each failure exiting with its own status.
func TestBuiltStructsExits133(t *testing.T) {
	bin := buildWithClang(t, "structs")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 133 {
		t.Errorf("structs exited with %v, want status 133", err)
	}
}

// The equal-operands example compares structs, arrays and unions with
// operands that hold calls, a literal of calls on either side or both, a
// literal nested in one, a union built from one, and a switch case
// written as one, which the equality helpers read through addresses;
// the build must be silent, and each check exits with its own status
// unless the comparison holds and the calls ran once each, in order.
func TestBuiltEqualOperandsExits23(t *testing.T) {
	bin := buildWithClang(t, "equal-operands")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 23 {
		t.Errorf("equal-operands exited with %v, want status 23", err)
	}
}

// The condition-writes example writes a variable in a condition, or in a
// branch beside an early exit, where the proof the condition gives still
// holds: before the comparison that proves it, after the use it guards,
// or in a branch or case that leaves. Each check prints its name and
// "ok", or "FAIL".
func TestBuiltConditionWritesPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "condition-writes")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	if err != nil {
		t.Errorf("condition-writes exited with %v, want status 0", err)
	}
	want := "written before && proves ok\n" +
		"written before || proves ok\n" +
		"written after the use ok\n" +
		"branch that leaves not taken ok\n" +
		"branch that leaves taken ok\n" +
		"case that leaves not taken ok\n" +
		"case that leaves taken ok\n" +
		"loop condition writes then proves ok\n"
	if string(out) != want {
		t.Errorf("condition-writes printed\n%s\nwant\n%s", out, want)
	}
}

// The field-facts example guards fields of immutable variables and
// parameters and uses them as proven divisors and indexes: a field, an
// index after an early exit, a field of a field, elements of an array
// field at a known index and at an immutable parameter's, a string
// field's length guarding a constant index, a range over a string field,
// a switch on a field, a field of an immutable local and of an immutable
// global, a guard holding in deferred code that writes a mut parameter,
// and two mut elements kept apart by guards on fields. Each check prints
// its name and "ok", or "FAIL", on both sides of its guard.
func TestBuiltFieldFactsPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "field-facts")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	if err != nil {
		t.Errorf("field-facts exited with %v, want status 0", err)
	}
	want := "field divisor ok\n" +
		"field index after an early exit ok\n" +
		"field of a field ok\n" +
		"element of an array field ok\n" +
		"string field length ok\n" +
		"range over a string field ok\n" +
		"switch on a field ok\n" +
		"field of an immutable local ok\n" +
		"field of an immutable global ok\n" +
		"field guard around a defer ok\n" +
		"field guard around a defer, not taken ok\n" +
		"mut elements apart by field facts ok\n"
	if string(out) != want {
		t.Errorf("field-facts printed\n%s\nwant\n%s", out, want)
	}
}

// The exit-leaves example proves divisors, an index, a map's key and a
// result read beside an error from branches and cases that end in exit,
// in a loop beside break and continue too, and ends functions with
// results in exit, in an if and a switch and alone. Each check prints its
// name and "ok", or "FAIL", and the last call ends the program with
// status 7 from a function whose body ends in exit.
func TestBuiltExitLeavesPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "exit-leaves")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Errorf("exit-leaves exited with %v, want status 7", err)
	}
	want := "divide ok\n" +
		"dividing\n" +
		"an else ending in exit ok\n" +
		"an index proven by an exit ok\n" +
		"a key proven by an exit ok\n" +
		"a switch ending in exit ends the function ok\n" +
		"an if ending in exit and return ends the function ok\n" +
		"a result read after an exit on its error ok\n" +
		"a divisor proven by an exit in a loop ok\n" +
		"a case ending in exit ok\n" +
		"failing\n"
	if string(out) != want {
		t.Errorf("exit-leaves printed\n%s\nwant\n%s", out, want)
	}
}

// The leaving-writes example writes an index, or the list it indexes, in
// a branch or a case that leaves, by continue, break, return or exit, and
// reads with it after that branch, where the proof made before the branch
// still holds. Each check prints its name and "ok", or "FAIL", taking the
// leaving branch and not, and the last call takes the branch that exits,
// with status 3.
func TestBuiltLeavingWritesPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "leaving-writes")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Errorf("leaving-writes exited with %v, want status 3", err)
	}
	want := "a branch that writes and continues ok\n" +
		"a branch that writes and breaks ok\n" +
		"a branch that writes and returns ok\n" +
		"a branch that writes and exits ok\n" +
		"an else that writes and returns ok\n" +
		"a nested branch that writes and returns ok\n" +
		"cases that write and leave ok\n" +
		"a switch on a byte whose cases write and leave ok\n" +
		"a branch that passes mut and returns ok\n" +
		"a branch that appends and returns ok\n" +
		"a branch that writes and breaks a loop after the early exit ok\n" +
		"a branch that writes and breaks after the read ok\n" +
		"a branch in a loop that writes and returns ok\n" +
		"a return against a loop's brace past a branch that breaks ok\n" +
		"a body's end against the brace of an if whose branch exits ok\n" +
		"a return right before an if or a loop that writes ok\n" +
		"a use past an inner branch that returns ok\n" +
		"a use in a loop past a branch that breaks around one that continues ok\n" +
		"a use after a loop whose branch returns ok\n" +
		"a use past a branch ending in an if that leaves ok\n" +
		"a use past an inner branch ending in an if that leaves ok\n" +
		"a use past a branch ending in a switch with its default first ok\n"
	if string(out) != want {
		t.Errorf("leaving-writes printed\n%s\nwant\n%s", out, want)
	}
}

// The conversion-facts example converts integers in the plain form T(x)
// where the facts in force prove x a value of T: a guard, an early exit,
// a counting loop, a range, switch cases, a contract, a relation, a field
// of an immutable parameter, a branch that writes and continues, deferred
// code, and the target-width types, each tried at the edges of what the
// facts allow and just past them, and binds the checked form where the
// plain one is proven. Each check prints its name and "ok", or "FAIL", and
// the last call ends the program with status 4 from a contract's exit.
func TestBuiltConversionFactsPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "conversion-facts")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 4 {
		t.Errorf("conversion-facts exited with %v, want status 4", err)
	}
	want := "int64 to uint64 under v > 0 ok\n" +
		"int64 to uint64 past an early exit ok\n" +
		"uint64 to int64 ok\n" +
		"int64 to int32 ok\n" +
		"int64 to int8 ok\n" +
		"int64 to uint8 ok\n" +
		"uint64 to uint32 ok\n" +
		"int32 to uint32 ok\n" +
		"uint32 to int32 ok\n" +
		"int8 to uint8 and back ok\n" +
		"a counting loop ok\n" +
		"a range over 256 ok\n" +
		"a range over a string ok\n" +
		"switch cases ok\n" +
		"a default after a case that leaves ok\n" +
		"a contract ok\n" +
		"a relation ok\n" +
		"a field of an immutable parameter ok\n" +
		"a branch that writes and continues ok\n" +
		"deferred code ok\n" +
		"the checked form where the plain one is proven ok\n" +
		"int64 to unsafe_size_t ok\n" +
		"unsafe_size_t to uint32 and unsafe_uintptr_t ok\n"
	if string(out) != want {
		t.Errorf("conversion-facts printed\n%s\nwant\n%s", out, want)
	}
}

// The arithmetic-ranges example indexes, slices, divides and converts by
// arithmetic on bounded values, +, -, * and unary -, with no checked
// form: a conversion of -(v + 1), indexes i + 1, 2*i, i + j, u - 1 and
// -(i - 3), len(s) - 1, a slice s[i : i + 1], a divisor x - 5, and
// arithmetic on fields, each run at the edges of what its guard allows
// and just past them, the type's extremes included. Each check prints
// its name and "ok", or "FAIL".
func TestBuiltArithmeticRangesPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "arithmetic-ranges")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	if err != nil {
		t.Errorf("arithmetic-ranges exited with %v, want status 0", err)
	}
	want := "-(v + 1) to uint64 under v < 0 ok\n" +
		"a[i + 1] of an array ok\n" +
		"a[i + 1] of a view under i < len(a) - 1 ok\n" +
		"a[2*i] and a[2*i + 1] ok\n" +
		"a divisor x - 5 under x > 5 ok\n" +
		"a[i + j] under bounds on both ok\n" +
		"s[len(s) - 1] under len(s) > 0 ok\n" +
		"s[i : i + 1] under i < len(s) ok\n" +
		"fields of an immutable parameter ok\n" +
		"a[u - 1] of a uint64 ok\n" +
		"b * 2 - 1 to int8 ok\n" +
		"n * 4 to unsafe_size_t ok\n" +
		"a[-(i - 3)] ok\n" +
		"t[:len(s)] under len(s) <= len(t) ok\n" +
		"\"abc\"[:len(w)] under len(w) <= 3 ok\n"
	if string(out) != want {
		t.Errorf("arithmetic-ranges printed\n%s\nwant\n%s", out, want)
	}
}

// The loop-invariants example indexes by mutable counters and totals a
// loop writes, with no checked form, proven by the bound each keeps on
// every pass: counters stepping up from a start and down from one,
// stepped in a branch that continues, in an inner loop, in a range
// loop, in deferred code, set back to 0, a total grown by bounded
// amounts, a mut parameter, a counter kept at or above another
// variable, counters stepping up to the top of int64 and uint8, a
// counter stepping down by 2, a bound kept below that bounds 2*j, a
// level fallen by bounded amounts, and a counter stepping back under a
// guard that keeps it at or above 0. Each
// check runs its loop not at all, once, and to the ends of what it
// reads, and prints its name and "ok", or "FAIL".
func TestBuiltLoopInvariantsPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "loop-invariants")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	if err != nil {
		t.Errorf("loop-invariants exited with %v, want status 0", err)
	}
	want := "a counter stepping up from 0 ok\n" +
		"a counter stepping down from len(s) - 1 ok\n" +
		"a condition that reads s[i] ok\n" +
		"a counter stepped in a branch that continues and in an inner loop ok\n" +
		"a counter set back to 0 in the body ok\n" +
		"a total grown by bounded amounts ok\n" +
		"a counter kept at or above 1 ok\n" +
		"a counter kept at or above another variable ok\n" +
		"a counter kept at or below 9 ok\n" +
		"a uint64 counter stepping down to 0 ok\n" +
		"a counter in a range loop ok\n" +
		"a mut parameter ok\n" +
		"a counter set back to 0 between loops ok\n" +
		"a counter stepped in deferred code ok\n" +
		"a counter stepping up to the largest int64 ok\n" +
		"a uint8 counter stepping up to 255 ok\n" +
		"a bound kept below bounding arithmetic ok\n" +
		"a level fallen by bounded amounts ok\n" +
		"a counter stepping down by 2 ok\n" +
		"a counter stepping back under a guard ok\n"
	if string(out) != want {
		t.Errorf("loop-invariants printed\n%s\nwant\n%s", out, want)
	}
}

// The ensures example calls functions whose ensures clauses prove facts
// about their arguments: an error checked by a call, as a statement and
// as a value and in a loop, a divisor, through a plain and a mut
// argument, the same variable passed as both, and a parameter of the
// caller, a contract proven by another call's, indexes and a slice's bounds, alone and chained through
// relations, a list grown through mut, a map's key, and a recursive, a
// generic, a method's and a deferring callee, a return whose value is a
// contract's call, a function made generic by an interface parameter, and
// a mut receiver's method ensuring its other parameter. Each check prints its name
// and "ok", or "FAIL".
func TestBuiltEnsuresPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "ensures")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	if err != nil {
		t.Errorf("ensures exited with %v, want status 0", err)
	}
	want := "handle ok\n" +
		"checked as a value ok\n" +
		"handle in a loop ok\n" +
		"nonzero ok\n" +
		"fix through mut ok\n" +
		"fix leaves a nonzero value ok\n" +
		"a contract proven by a call's ok\n" +
		"the same variable as mut and as a copy ok\n" +
		"a parameter of the caller ok\n" +
		"inside a list ok\n" +
		"below a length stored ok\n" +
		"ordered slice bounds ok\n" +
		"room, chained through y := x + 1 ok\n" +
		"grow ok\n" +
		"grow then pop ok\n" +
		"need ok\n" +
		"recursive ok\n" +
		"generic ok\n" +
		"method on its receiver ok\n" +
		"deferred code ran, the fact held ok\n" +
		"a return's call proves the contract ok\n" +
		"an interface parameter's function ok\n" +
		"a mut receiver's method ok\n"
	if string(out) != want {
		t.Errorf("ensures printed\n%s\nwant\n%s", out, want)
	}
}

// The ensures-module example calls functions of another package whose
// contracts check an error and prove an index. Each check prints its name
// and "ok", or "FAIL".
func TestBuiltEnsuresModulePassesEveryCheck(t *testing.T) {
	bin := buildTargetWithClang(t, "ensures-module", filepath.Join(examplesDir, "ensures-module"))
	out, err := exec.CommandContext(t.Context(), bin).Output()
	if err != nil {
		t.Errorf("ensures-module exited with %v, want status 0", err)
	}
	want := "a result read after another package's check ok\n" +
		"an index another package proved ok\n"
	if string(out) != want {
		t.Errorf("ensures-module printed\n%s\nwant\n%s", out, want)
	}
}

// The mut-exclusive example passes several mut arguments to one call where
// their places cannot overlap: different fields of a struct and of a mut
// parameter, elements at known indexes and at indexes facts keep apart,
// nested paths, whole elements of an array field, elements two levels
// deep, and different fields of elements whose indexes are unknown; and a
// plain argument that reads a place passed as mut, which is a copy taken
// before the call, whether it is the whole place, a field of it, or a call
// that changes it first. Each check prints its name and "ok", or "FAIL".
func TestBuiltMutExclusivePassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "mut-exclusive")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	if err != nil {
		t.Errorf("mut-exclusive exited with %v, want status 0", err)
	}
	want := "fields of one struct ok\n" +
		"fields of a mut parameter ok\n" +
		"elements at known indexes ok\n" +
		"elements at indexes proven apart ok\n" +
		"nested paths ok\n" +
		"elements of a field ok\n" +
		"two-level elements ok\n" +
		"different fields of elements at unknown indexes ok\n" +
		"overlapping plain argument copied ok\n" +
		"field of the mut place as a plain argument ok\n" +
		"inner call changes the place first ok\n" +
		"two variables ok\n"
	if string(out) != want {
		t.Errorf("mut-exclusive printed\n%s\nwant\n%s", out, want)
	}
}

// The mut-params example passes mut arguments and checks what each call
// did to the caller's variable: a field write and a compound assignment
// through a reference, a whole value replaced, a swap, a reference passed
// on whole and by field, nested field and array element, a string field
// assigned, a defer reading and writing a mut parameter after the return
// value was taken, a mut argument in a defer, recursion a thousand deep, a reference passed through
// five calls, elements by known and proven indexes, arguments evaluated
// left to right around calls that change a variable another argument
// reads, a checked index reading the array before its index's call changes
// it, a bool, and a path four levels deep. Each check prints its name and
// "ok", or "FAIL".
func TestBuiltMutParamsPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "mut-params")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	if err != nil {
		t.Errorf("mut-params exited with %v, want status 0", err)
	}
	want := "in place ok\n" +
		"whole value ok\n" +
		"swap ok\n" +
		"passed on ok\n" +
		"array and string fields ok\n" +
		"defer reads and writes a mut parameter ok\n" +
		"mut argument in a defer ok\n" +
		"recursion ok\n" +
		"through five calls ok\n" +
		"element by known index ok\n" +
		"element by proven index ok\n" +
		"left to right ok\n" +
		"read before a later mut ok\n" +
		"checked index reads the array first ok\n" +
		"bool ok\n" +
		"deep path ok\n"
	if string(out) != want {
		t.Errorf("mut-params printed\n%s\nwant\n%s", out, want)
	}
}

// The unsafe-sizes example uses unsafe_size_t and unsafe_uintptr_t, C's
// size_t and uintptr_t: the narrow unsigned types widening into them and
// both widening into uint64, the common type of mixed operands, checked
// conversions that fit and that fail at the host's real width, arithmetic
// wrapping there, arithmetic on known values past every 32-bit value left
// to run time, division, struct fields, mut parameters, arrays,
// globals, a range count, results, a switch, and indexes of arrays,
// strings and views by either type, proven by a guard, a counting loop or
// a known value, and checked. Each check prints its name and "ok", or
// "FAIL". Some checks hold only on a 64-bit host, such as a uint64 above
// every 32-bit value converting to unsafe_size_t.
func TestBuiltUnsafeSizesPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "unsafe-sizes")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	if err != nil {
		t.Errorf("unsafe-sizes exited with %v, want status 0", err)
	}
	want := "widening in ok\n" +
		"widening out ok\n" +
		"common types ok\n" +
		"checked conversions that fit ok\n" +
		"checked conversions that fail ok\n" +
		"wrapping ok\n" +
		"known values past 32 bits ok\n" +
		"division ok\n" +
		"struct fields and mut parameters ok\n" +
		"arrays ok\n" +
		"globals and a range count ok\n" +
		"results ok\n" +
		"switch ok\n" +
		"index proven by a guard ok\n" +
		"index guard that fails ok\n" +
		"index proven by a counting loop ok\n" +
		"known index ok\n" +
		"checked index ok\n"
	if string(out) != want {
		t.Errorf("unsafe-sizes printed\n%s\nwant\n%s", out, want)
	}
}

// viewsWant is what examples/views prints when every check passes.
var viewsWant = "arrays of two lengths ok\n" +
	"range loops ok\n" +
	"range without variables ok\n" +
	"zero length ok\n" +
	"last element ok\n" +
	"passed on ok\n" +
	"checked index ok\n" +
	"guard on len ok\n" +
	"guarded index ok\n" +
	"field of structs ok\n" +
	"element of a field ok\n" +
	"view of arrays ok\n" +
	"string variable ok\n" +
	"string literal ok\n" +
	"empty string ok\n" +
	"zero string field ok\n" +
	"mut parameter's array ok\n" +
	"view alongside mut of the same array ok\n" +
	"view before a later mut ok\n" +
	"view after the change ok\n" +
	"mutable global array ok\n" +
	"mutable global string ok\n" +
	"view of bools ok\n" +
	"view of strings ok\n" +
	"view of unsafe_size_t ok\n"

// TestBuiltViewsPassesEveryCheck builds examples/views at every
// optimization level and checks each binary passes every check: arrays of
// two lengths and of none through one view parameter, every range form,
// a view passed on, the checked form in and out of range, a guard on
// len, views of struct fields, of array elements and of arrays of arrays,
// a string's bytes through []uint8(s), a mut parameter's array, a view
// alongside a mut argument of the same array and before a later one, which
// sees the array as it was, a mutable global array, which the callee
// changes while the view keeps the value it had, a mutable global string
// assigned during the call, and views of bools, strings and
// unsafe_size_t. Each check prints its name and "ok", or "FAIL".
func TestBuiltViewsPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "views")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("views"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != viewsWant || code != 0 {
				t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 0", out, code, viewsWant)
			}
		})
	}
}

// variadicsWant is what examples/variadics prints when every check
// passes.
var variadicsWant = "no values ok\n" +
	"one value ok\n" +
	"several values ok\n" +
	"many values ok\n" +
	"widened values ok\n" +
	"untyped constants ok\n" +
	"uint64 values ok\n" +
	"spread of an array ok\n" +
	"spread of a slice ok\n" +
	"spread of a variadic ok\n" +
	"spread passed on ok\n" +
	"spread of a variadic's slice ok\n" +
	"recursion ok\n" +
	"checked index ok\n" +
	"checked negative index ok\n" +
	"guard on len ok\n" +
	"range with index ok\n" +
	"strings ok\n" +
	"structs ok\n" +
	"arrays ok\n" +
	"bools ok\n" +
	"bytes ok\n" +
	"spread of a string's bytes ok\n" +
	"spread of the empty string's bytes ok\n" +
	"results ok\n" +
	"results with no values ok\n" +
	"evaluation order ok\n" +
	"reads between calls ok\n" +
	"nested calls ok\n" +
	"values alongside mut ok\n" +
	"spread alongside mut ok\n" +
	"spread of a mutable global ok\n" +
	"spread of a mut parameter's array ok\n" +
	"in a defer ok\n" +
	"in a loop condition ok\n"

// TestBuiltVariadicsPassesEveryCheck builds examples/variadics at every
// optimization level and checks each binary passes every check: zero,
// one, several and twenty values; values widened to the element type and
// untyped constants at int64's and uint64's edges; a spread of an array,
// an empty one, a slice, an empty slice, a variadic, a variadic's slice,
// recursively, and a string's bytes; len, the checked form, a guard,
// range and slicing inside; variadics of strings, structs, arrays, bools
// and bytes; several results; fixed and variadic arguments evaluated left
// to right, each once, with a mutable global read between calls; values
// and a spread alongside a mut argument of the same variable, and a
// spread of a mutable global, each a copy taken at the call; and calls in
// a defer and a loop's condition.
func TestBuiltVariadicsPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "variadics")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("variadics"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != variadicsWant || code != 0 {
				t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 0", out, code, variadicsWant)
			}
		})
	}
}

// TestBuiltPrintfWritesItsOutput builds examples/printf, which imports
// std/fmt, whose C the driver compiles against the program's tack.h, at
// every optimization level and checks each binary writes exactly
// examples/printf/output: every verb and every basic type at its edges,
// arguments from variables, constants, calls and globals, each a copy
// taken at the call, any parameters passed on, and the markers a format
// the compiler does not know gets for each kind of mistake.
func TestBuiltPrintfWritesItsOutput(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "printf", "output"))
	if err != nil {
		t.Fatal(err)
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "printf")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("printf"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != string(want) || code != 0 {
				t.Errorf("wrote\n%q\nand exited %d, want\n%q\nand 0", out, code, want)
			}
		})
	}
}

// fmtWriteBig is the number of bytes examples/fmt-write writes last, from
// one list[uint8]: 1 MiB, byte i being i % 251.
const fmtWriteBig = 1 << 20

// checkFmtWriteOutput reports whether out is exactly what examples/fmt-write
// writes: want, its output file, then fmtWriteBig bytes, byte i being
// i % 251. It reports the first difference rather than dumping a mebibyte.
func checkFmtWriteOutput(t *testing.T, out, want string) {
	t.Helper()
	if len(out) != len(want)+fmtWriteBig {
		t.Fatalf("wrote %d bytes, want %d", len(out), len(want)+fmtWriteBig)
	}
	if out[:len(want)] != want {
		t.Errorf("wrote\n%q\nwant\n%q", out[:len(want)], want)
	}
	for i := range fmtWriteBig {
		if got := out[len(want)+i]; got != byte(i%251) {
			t.Fatalf("byte %d of the large write is %#x, want %#x", i, got, byte(i%251))
		}
	}
}

// TestBuiltFmtWriteWritesItsOutput builds examples/fmt-write, which writes
// bytes with std/fmt's write, at every optimization level and checks each
// binary writes exactly examples/fmt-write/output, then the 1 MiB list:
// lists, arrays, slices and a string's bytes, empty views writing
// nothing, bytes 0x00 and 0xff as they are, views passed on, and writes
// interleaved with print and printf in call order.
func TestBuiltFmtWriteWritesItsOutput(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "fmt-write", "output"))
	if err != nil {
		t.Fatal(err)
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "fmt-write")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("fmt-write"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			out, code := runBuilt(t, bin)
			if code != 0 {
				t.Fatalf("exited %d, want 0", code)
			}
			checkFmtWriteOutput(t, out, string(want))
		})
	}
}

// TestBuiltBytesPassesEveryCheck builds examples/bytes, which imports
// std/bytes, a std package written in Tack alone, at every optimization
// level and checks each binary writes exactly examples/bytes/output, every
// check ok: each function of std/bytes at its edges, empty views, overlaps,
// bytes 0x00 and 0xff, int64's and uint64's extremes, repeat's negative
// and overflowing counts, and appends and repeats made to fail, which
// leave the list as it was and allocate nothing.
func TestBuiltBytesPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "bytes", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(want), "FAIL") {
		t.Fatal("examples/bytes/output expects a check to fail")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "bytes")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("bytes"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != string(want) || code != 0 {
				t.Errorf("wrote\n%q\nand exited %d, want\n%q\nand 0", out, code, want)
			}
		})
	}
}

// TestBuiltFloatsPassesEveryCheck builds examples/floats at every
// optimization level and checks each binary writes exactly
// examples/floats/output, every check ok: float32 and float64 constants,
// exact until used, arithmetic that rounds at run time, infinities and
// NaN from dividing by zero, negative zero, float32's rounding, the
// smallest subnormal, and floats in structs and arrays. Every level must
// print the same, so no level fuses or reorders float operations.
func TestBuiltFloatsPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "floats", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(want), "FAIL") {
		t.Fatal("examples/floats/output expects a check to fail")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "floats")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("floats"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != string(want) || code != 0 {
				t.Errorf("wrote\n%q\nand exited %d, want\n%q\nand 0", out, code, want)
			}
		})
	}
}

// TestBuiltFloatConversionsPassesEveryCheck builds examples/float-conversions
// at every optimization level and checks each binary writes exactly its
// output, every check ok: integers to floats rounding to nearest, float64
// to float32 rounding to an infinity past half an ulp beyond float32's
// largest, and the checked float to integer form at NaN, the infinities,
// zero of either sign, truncation toward zero, and each integer type's
// edges, 2^63 and 2^64 and the float values beside them included.
func TestBuiltFloatConversionsPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "float-conversions", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(want), "FAIL") {
		t.Fatal("examples/float-conversions/output expects a check to fail")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "float-conversions")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("float-conversions"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != string(want) || code != 0 {
				t.Errorf("wrote\n%q\nand exited %d, want\n%q\nand 0", out, code, want)
			}
		})
	}
}

// TestBuiltFloatFormatPassesEveryCheck builds examples/float-format at
// every optimization level and checks each binary writes exactly its
// output, which is what Go's fmt writes for the same floats: printf's %v,
// %g, %e and %f, std/conv's append_float and parse_float, NaN, the
// infinities, negative zero, subnormals and float32.
func TestBuiltFloatFormatPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "float-format", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(want), "FAIL") {
		t.Fatal("examples/float-format/output expects a check to fail")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "float-format")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("float-format"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != string(want) || code != 0 {
				t.Errorf("wrote\n%q\nand exited %d, want\n%q\nand 0", out, code, want)
			}
		})
	}
}

// TestBuiltTextPassesEveryCheck builds examples/text, which imports
// std/text, a std package written in Tack alone, at every optimization
// level and checks each binary writes exactly examples/text/output, every
// check ok: a Text built empty and from views, appended to, compared
// against literals, hashed against FNV-1a's values, its view passed to
// fmt.write and std/bytes, and appends and from_bytes made to fail, which
// leave the Text as it was and allocate nothing.
func TestBuiltTextPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "text", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(want), "FAIL") {
		t.Fatal("examples/text/output expects a check to fail")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "text")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("text"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != string(want) || code != 0 {
				t.Errorf("wrote\n%q\nand exited %d, want\n%q\nand 0", out, code, want)
			}
		})
	}
}

// osExampleArgs are the arguments examples/os is run with, and
// examples/os/output what it writes for them.
var osExampleArgs = []string{"one", "", "two words"}

// osExampleEnv returns the environment examples/os is run with: the
// test's own, with TACK_OS_EXAMPLE set to "set by the test",
// TACK_OS_EXAMPLE_EMPTY set to nothing, TACK_OS_EXAMPLE_UNSET not set,
// and TMPDIR set to tmp, where its temporary directory goes.
func osExampleEnv(tmp string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TACK_OS_EXAMPLE") && !strings.HasPrefix(kv, "TMPDIR=") {
			env = append(env, kv)
		}
	}
	return append(env, "TACK_OS_EXAMPLE=set by the test", "TACK_OS_EXAMPLE_EMPTY=", "TMPDIR="+tmp)
}

// osLimitedArgs returns the arguments to sh that run bin with
// osExampleArgs and at most 64 open files, so examples/os's 1000 rounds
// of opening files fail with "Too many open files" if std/os leaves one
// open each time.
func osLimitedArgs(bin string) []string {
	return append([]string{"-c", `ulimit -n 64 && exec "$0" "$@"`, bin}, osExampleArgs...)
}

// TestBuiltOsPassesEveryCheck builds examples/os, which imports std/os,
// at every optimization level and runs each binary with osExampleArgs
// and osExampleEnv, and at most 64 open files, checking it writes exactly examples/os/output, every
// check ok: its arguments and environment, files written, read, listed
// and removed in a temporary directory it makes, the OS's refusals, a 0
// byte in a path, and every allocating function made to fail. TMPDIR is
// a directory of the test's, which must be empty after each run: the
// example removes what it made.
func TestBuiltOsPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "os", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(want), "FAIL") {
		t.Fatal("examples/os/output expects a check to fail")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "os")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("os"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			tmp := t.TempDir()
			cmd := exec.CommandContext(t.Context(), "sh", osLimitedArgs(bin)...)
			cmd.Env = osExampleEnv(tmp)
			out, err := cmd.Output()
			if err != nil || string(out) != string(want) {
				t.Errorf("wrote\n%q\nand ended with %v, want\n%q\nand exit 0", out, err, want)
			}
			if left, _ := os.ReadDir(tmp); len(left) != 0 {
				t.Errorf("left %d entries in TMPDIR, the first %s", len(left), left[0].Name())
			}
		})
	}
}

// TestBuiltListEqualityPassesEveryCheck builds examples/list-equality at
// every optimization level and checks each binary writes exactly
// examples/list-equality/output, every check ok: == and != on lists of
// bytes, integers, strings, bools and arrays, empty, of different lengths
// and differing in their first or last element, lists of lists three
// deep, text.Text values, and structs and unions holding lists, with
// nothing allocated by a comparison and everything freed.
func TestBuiltListEqualityPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "list-equality", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(want), "FAIL") {
		t.Fatal("examples/list-equality/output expects a check to fail")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "list-equality")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("list-equality"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != string(want) || code != 0 {
				t.Errorf("wrote\n%q\nand exited %d, want\n%q\nand 0", out, code, want)
			}
		})
	}
}

// TestBuiltPathPassesEveryCheck builds examples/path, which imports
// std/path, a std package written in Tack alone, at every optimization
// level and checks each binary writes exactly examples/path/output, every
// check ok: clean, base, dir, ext, is_abs and join on Go's documented
// examples and edge cases of their own, each against what Go's path
// package gives, and allocations made to fail, which free what they made.
func TestBuiltPathPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "path", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(want), "FAIL") {
		t.Fatal("examples/path/output expects a check to fail")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "path")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("path"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != string(want) || code != 0 {
				t.Errorf("wrote\n%q\nand exited %d, want\n%q\nand 0", out, code, want)
			}
		})
	}
}

// TestBuiltConvPassesEveryCheck builds examples/conv, which imports
// std/conv, a std package written in Tack alone, at every optimization
// level and checks each binary writes exactly examples/conv/output, every
// check ok: parse_int and parse_uint on Go's own atoi test tables, every
// base and bit size at its limits and one past them, misplaced
// underscores, odd bytes and inputs thousands of bytes long, each against
// what Go's strconv gives.
func TestBuiltConvPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "conv", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(want), "FAIL") {
		t.Fatal("examples/conv/output expects a check to fail")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "conv")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("conv"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != string(want) || code != 0 {
				t.Errorf("wrote\n%q\nand exited %d, want\n%q\nand 0", out, code, want)
			}
		})
	}
}

// TestBuiltSortPassesEveryCheck builds examples/sort, which imports
// std/sort, at every optimization level and checks each binary writes
// exactly examples/sort/output, every check ok: lists of every shape and
// of sizes around the insertion sort's threshold sorted into sorted
// permutations of themselves, 100000 elements, less functions that order
// nothing, and lists of lists and of Texts sorted with no buffer left
// behind.
func TestBuiltSortPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "sort", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(want), "FAIL") {
		t.Fatal("examples/sort/output expects a check to fail")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "sort")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("sort"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != string(want) || code != 0 {
				t.Errorf("wrote\n%q\nand exited %d, want\n%q\nand 0", out, code, want)
			}
		})
	}
}

// TestBuiltRelationsPassesEveryCheck builds examples/relations, whose
// indexes and slices only relations between two values prove, at every
// optimization level and checks each binary writes exactly
// examples/relations/output, every check ok: each function at its first
// and last index, one past each end, on empty views, and at int64's and
// uint64's extremes, where a relation that ignored wrapping would read
// out of range.
func TestBuiltRelationsPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "relations", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(want), "FAIL") {
		t.Fatal("examples/relations/output expects a check to fail")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "relations")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("relations"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != string(want) || code != 0 {
				t.Errorf("wrote\n%q\nand exited %d, want\n%q\nand 0", out, code, want)
			}
		})
	}
}

// TestBuiltAnySwitchWritesItsOutput builds examples/any-switch, which asks
// anys which type they hold from Tack, at every optimization level, and
// checks each binary writes exactly examples/any-switch/output: every
// check ok, each of the twelve types in a type switch case of its own,
// cases naming several types and the default binding the any, a switch
// with no binding, exact matches, zero values on a mismatch, any locals
// from a range, an index and a binding, break and continue in a case,
// nested switches, defers, and values passed on to printf.
func TestBuiltAnySwitchWritesItsOutput(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "any-switch", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(want), "FAIL") {
		t.Fatal("examples/any-switch/output expects a check to fail")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "any-switch")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("any-switch"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != string(want) || code != 0 {
				t.Errorf("wrote\n%q\nand exited %d, want\n%q\nand 0", out, code, want)
			}
		})
	}
}

// TestBuiltAnyViewsWritesItsOutput builds examples/any-views, which
// passes views as any, at every optimization level, and checks each
// binary writes exactly examples/any-views/output: every check ok, a
// Text's bytes and views of a list, an array, slices, view results and a
// view parameter printed with %s and %v, empty views and bytes 0 and
// 0xff, a type switch binding each view, the markers printf writes for a
// view a verb does not take, and a view of an array copied when the call
// changes the array, through a mut argument or as a mutable global.
func TestBuiltAnyViewsWritesItsOutput(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "any-views", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(want), "FAIL") {
		t.Fatal("examples/any-views/output expects a check to fail")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "any-views")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("any-views"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != string(want) || code != 0 {
				t.Errorf("wrote\n%q\nand exited %d, want\n%q\nand 0", out, code, want)
			}
		})
	}
}

// slicesWant is what examples/slices prints when every check passes.
var slicesWant = "string slices ok\n" +
	"omitted and empty bounds ok\n" +
	"slices of slices ok\n" +
	"stored in a field, an array and a variable ok\n" +
	"results ok\n" +
	"bytes through a multi-byte character ok\n" +
	"zero strings ok\n" +
	"zero string field checked ok\n" +
	"known slice of a known string ok\n" +
	"globals ok\n" +
	"bytes of a slice ok\n" +
	"guard on the high bound ok\n" +
	"guard on both bounds ok\n" +
	"checked in range ok\n" +
	"checked out of range ok\n" +
	"checked uint64 bounds ok\n" +
	"checked omitted bounds ok\n" +
	"array slices ok\n" +
	"array slice ends ok\n" +
	"slices proven by loops ok\n" +
	"slice of a view passed on ok\n" +
	"unsafe_size_t bound ok\n" +
	"len of a view slice ok\n" +
	"field and element arrays ok\n" +
	"mut parameter's array ok\n" +
	"slice alongside mut of the same array ok\n" +
	"mutable global array ok\n" +
	"evaluation order ok\n"

// TestBuiltSlicesPassesEveryCheck builds examples/slices at every
// optimization level and checks each binary passes every check: string
// slices with every form of bounds, empty ones and slices of slices,
// stored in a field, an array, a variable and a result; bytes cut
// through a multi-byte character; slices of the zero string, a field of
// a zeroed struct included, whose pointer is null; a slice of a known
// string, itself known, as an array length and in a global; guards on
// one bound and on both; the checked form in and out of range, i > j, j
// past the end, a negative bound, uint64 bounds past every length and
// omitted bounds; array and view slices passed to view parameters,
// proven by constants, loops and guards, with an unsafe_size_t bound and
// under len; slices of a field's and an element's array and of a mut
// parameter's; a slice alongside a mut argument of the same array and
// one of a mutable global array, which see copies; and evaluation left
// to right, each part once. Each check prints its name and "ok", or
// "FAIL".
func TestBuiltSlicesPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "slices")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("slices"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != slicesWant || code != 0 {
				t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 0", out, code, slicesWant)
			}
		})
	}
}

// mixedCompareWant is what examples/mixed-compare prints when every check
// passes.
var mixedCompareWant = "int64 and uint64 ok\n" +
	"narrow pairs ok\n" +
	"target widths ok\n" +
	"known values ok\n" +
	"evaluation order ok\n" +
	"guard on len ok\n" +
	"facts ok\n" +
	"switches ok\n"

// TestBuiltMixedComparePassesEveryCheck builds examples/mixed-compare at
// every optimization level and checks each binary passes every check:
// comparisons between integers of different types, signed and unsigned,
// of the same width and of different ones, the target-width types
// included, give what the operands' values say, at the edges of each
// type, -1 against the largest uint64 and int8 -1 against uint8 255
// among them; known values and globals folded at compile time agree;
// both operands are evaluated exactly once, left to right; a uint64
// checked against len(s) indexes s; a mixed comparison with a known
// value bounds a variable by value; and a switch compares its tag with
// values of other integer types the same way. Each check prints its name
// and "ok", or "FAIL".
func TestBuiltMixedComparePassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "mixed-compare")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("mixed-compare"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != mixedCompareWant || code != 0 {
				t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 0", out, code, mixedCompareWant)
			}
		})
	}
}

// The strcmp example compares strings with all six operators: by content,
// a proper prefix less, the empty string least, embedded zero bytes and
// bytes from 0x80 up compared as unsigned bytes, strings returned by calls,
// string fields of structs and elements of arrays through ==, and in an if
// and a loop condition, each failure exiting with its own status.
func TestBuiltStrcmpExits42(t *testing.T) {
	bin := buildWithClang(t, "strcmp")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
		t.Errorf("strcmp exited with %v, want status 42", err)
	}
}

// The raw-strings example reads the bytes of backquoted strings: a
// backslash before n, a quote, another backslash or the closing backquote
// is a byte of its own, quotes and comment markers inside are bytes, a
// raw string spans lines, and bytes from 0x80 up pass through. It prints
// a three-line raw string as written and exits with the byte value of 't'.
func TestBuiltRawStringsPrintsAndExits116(t *testing.T) {
	bin := buildWithClang(t, "raw-strings")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 116 {
		t.Errorf("raw-strings exited with %v, want status 116", err)
	}
	want := "raw\n  strings\n\"keep\" \\every\\ byte // as written\n"
	if string(out) != want {
		t.Errorf("raw-strings printed %q, want %q", out, want)
	}
}

// The switch example runs both forms of switch: multi-value cases, a
// default in the middle tried last, string, struct, bool, array and narrow
// integer tags, a tag evaluated exactly once, case values evaluated left to
// right and no further than the first match, break and continue in a
// switch acting on the enclosing loop, returns from cases, nested
// switches, a switch in a range loop, and divisions and indexes proven by
// the cases before them, each failure exiting with its own status.
func TestBuiltSwitchExits42(t *testing.T) {
	bin := buildWithClang(t, "switch")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
		t.Errorf("switch exited with %v, want status 42", err)
	}
}

// The defer example runs defers last first when their block ends, an inner
// block's before the rest of the outer one, through early returns from
// several blocks, never one not reached, each reading variables as they
// are when it runs, after a return's values are computed, with several
// results, a division proven by the if around it, and prints in order.
// Its main defers a print that exit skips.
func TestBuiltDeferPrintsAndExits42(t *testing.T) {
	bin := buildWithClang(t, "defer")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
		t.Errorf("defer exited with %v, want status 42", err)
	}
	if want := "a\nb\nc\n"; string(out) != want {
		t.Errorf("defer printed %q, want %q", out, want)
	}
}

// The defer-loops example runs a loop body's defers at the end of every
// iteration, one ended by continue or break included, before the post
// statement; a case's defers when the case ends; only the innermost loop's
// on a break out of nested loops; a deferred block whose own loop breaks
// and continues; deferred updates that do not commute, run last first; a
// body left by break and by return; a continue out of a nested block whose
// next iterations run normally; a return out of nested loops with body
// defers; and one defer per range iteration.
func TestBuiltDeferLoopsExits42(t *testing.T) {
	bin := buildWithClang(t, "defer-loops")
	err := exec.CommandContext(t.Context(), bin).Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
		t.Errorf("defer-loops exited with %v, want status 42", err)
	}
}

// The safety example holds the guarded and checked examples from
// docs/safety.md and checks each one's result as the page states it: a
// guarded operation's value on both sides of its guard; a checked one's
// zero value and false on failure, for every element type, and its value
// and true on success; a known value converted without ok; a byte of an
// immutable string variable read by a known index; counting loops up and
// down indexing every element; a remainder guarded by ||, and an index by
// a case with two values, on both sides of the guard; the int8 minimum
// divided by -1 giving 0 and false, and its remainder by -1 0; the = form, parentheses, _, and
// both operands of a checked division being evaluated. Each check prints
// its name and "ok", or "FAIL".
func TestBuiltSafetyPassesEveryCheck(t *testing.T) {
	bin := buildWithClang(t, "safety")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	if err != nil {
		t.Errorf("safety exited with %v, want status 0", err)
	}
	want := "half guarded ok\n" +
		"division by zero ok\n" +
		"remainder ok\n" +
		"compound guarded ok\n" +
		"get guarded ok\n" +
		"index out of range ok\n" +
		"index in range ok\n" +
		"set guarded ok\n" +
		"at guarded ok\n" +
		"first guarded ok\n" +
		"byte out of range ok\n" +
		"byte in range ok\n" +
		"conversion fails ok\n" +
		"conversion fits ok\n" +
		"known value converts ok\n" +
		"known string length ok\n" +
		"counting loops ok\n" +
		"or proves ok\n" +
		"case proves ok\n" +
		"zero string ok\n" +
		"zero bool ok\n" +
		"zero struct ok\n" +
		"int8 min / -1 fails ok\n" +
		"int8 min % -1 ok\n" +
		"assign index ok\n" +
		"assign conversion ok\n" +
		"assign division in parentheses ok\n" +
		"blank discards ok\n" +
		"both operands evaluated ok\n" +
		"constant index into a literal ok\n" +
		"unneeded ok on a division ok\n" +
		"unneeded ok on an index ok\n" +
		"unneeded ok on a conversion ok\n" +
		"unneeded ok on proven operations ok\n"
	if string(out) != want {
		t.Errorf("safety printed\n%s\nwant\n%s", out, want)
	}
}

// TestDeepDeferNestingReportsOnce checks a long chain of nested defers is
// reported once, at the second defer, not once per nested defer.
func TestDeepDeferNestingReportsOnce(t *testing.T) {
	src := "package main\nfunc main() {\n" + strings.Repeat("defer {\nprint(\"x\")\n", 500) +
		strings.Repeat("}\n", 501)
	r := runTack(t, "--emit", writeSource(t, []byte(src)))
	if r.code != exitFailure {
		t.Fatalf("exit code = %d, want %d", r.code, exitFailure)
	}
	if n := strings.Count(r.stderr, "cannot defer inside a defer"); n != 1 {
		t.Errorf("got %d diagnostics, want 1:\n%s", n, r.stderr)
	}
	if !strings.Contains(r.stderr, "main.tck:5:1:") {
		t.Errorf("diagnostic is not at the second defer:\n%s", r.stderr)
	}
}

// TestBuiltOrderIsLeftToRight runs examples/order, whose operands each print
// their name, and checks they ran left to right in every position Tack
// evaluates several, and that the values they gave are right.
func TestBuiltOrderIsLeftToRight(t *testing.T) {
	bin := buildWithClang(t, "order")
	out, err := exec.CommandContext(t.Context(), bin).Output()
	if err != nil {
		t.Errorf("order exited with %v, want status 0", err)
	}
	want := "arithmetic: a b c ok\n" +
		"subtraction: a b ok\n" +
		"comparison: a b ok\n" +
		"strings: a b ok\n" +
		"structs: a b ok\n" +
		"arguments: a b c ok\n" +
		"nested arguments: a b c ok\n" +
		"results: a b ok\n" +
		"array elements: a b c ok\n" +
		"struct fields: b a ok\n" +
		"index: base index ok\n" +
		"division: a b ok\n" +
		"a global read before a call: ok\n" +
		"a call before a global read: ok\n" +
		"a global in arguments: ok\n" +
		"compound assignment: ok\n" +
		"switch cases: tag a b ok\n"
	if string(out) != want {
		t.Errorf("order printed\n%s\nwant\n%s", out, want)
	}
}

// TestBuiltMultiFileExits42 builds examples/multi-file, a package split
// across two files that use each other's functions, type and globals, and
// checks it prints the global from the other file and exits
// 24 + 14 + 1 + 3, the last the length of an array whose length is a
// global declared in the later file.
func TestBuiltMultiFileExits42(t *testing.T) {
	bin := buildTargetWithClang(t, "multi-file", filepath.Join(examplesDir, "multi-file"))
	out, err := exec.CommandContext(t.Context(), bin).Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 42 {
		t.Errorf("multi-file exited with %v, want status 42", err)
	}
	if string(out) != "split across files\n" {
		t.Errorf("multi-file printed %q, want %q", out, "split across files\n")
	}
}

// writeDir writes files, by name, into a fresh temp directory and returns
// its path.
func writeDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestDirectoryDumps checks each dump of a directory target: the tokens of
// every file under a header naming it, the trees as a JSON list, the
// resolved program as JSON, and the generated code.
func TestDirectoryDumps(t *testing.T) {
	dir := filepath.Join(examplesDir, "multi-file")

	lex := runTack(t, "--lex", dir)
	assertOnlyStdout(t, lex)
	// The directory is outside the current one, so its files are shown by
	// their absolute paths.
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	mainHeader := "-- " + filepath.ToSlash(filepath.Join(abs, "main.tck")) + " --\n"
	shapesHeader := "-- " + filepath.ToSlash(filepath.Join(abs, "shapes.tck")) + " --\n"
	if !strings.HasPrefix(lex.stdout, mainHeader) ||
		!strings.Contains(lex.stdout, "\n"+shapesHeader) {
		t.Errorf("--lex does not head each file's tokens with its name:\n%s", lex.stdout)
	}

	var trees []json.RawMessage
	r := runTack(t, "--ast", dir)
	assertOnlyStdout(t, r)
	if err := json.Unmarshal([]byte(r.stdout), &trees); err != nil || len(trees) != 2 {
		t.Errorf("--ast is not a JSON list of two trees (%v):\n%s", err, r.stdout)
	}

	var prog struct {
		Packages []struct{ Files []json.RawMessage }
	}
	r = runTack(t, "--res", dir)
	assertOnlyStdout(t, r)
	if err := json.Unmarshal([]byte(r.stdout), &prog); err != nil ||
		len(prog.Packages) != 1 || len(prog.Packages[0].Files) != 2 {
		t.Errorf("--res is not a program of one package of two files (%v):\n%s", err, r.stdout)
	}

	assertOnlyStdout(t, runTack(t, "--emit", dir))
}

// TestDirectoryDiagnosticsNameEachFile checks a problem spanning two files
// is shown in both, each by its own name.
func TestDirectoryDiagnosticsNameEachFile(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"tack.mod": "module m\n",
		"a.tck":    "package main\n\nfunc main() {\n}\n",
		"b.tck":    "package main\n\nfunc main() {\n}\n",
	})
	r := runTack(t, "--emit", dir)
	assertDiagnostics(t, r)
	for _, want := range []string{
		filepath.Join(dir, "b.tck") + ":3:6: error: main is already declared",
		filepath.Join(dir, "a.tck") + ":3:6: note: previous declaration of main",
	} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("stderr does not contain %q:\n%s", want, r.stderr)
		}
	}
}

// TestDirectoryFailures checks the ways a directory target fails before
// there is any source to point at: unreadable, holding a .tck name that is
// not a readable file, or a symbolic link loop.
func TestDirectoryFailures(t *testing.T) {
	t.Run("unreadable", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can read any directory")
		}
		root := writeDir(
			t,
			map[string]string{"tack.mod": "module m\n", "p/main.tck": "package main\n"},
		)
		dir := filepath.Join(root, "p")
		if err := os.Chmod(dir, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		assertFailure(t, runTack(t, "--emit", dir), "permission denied")
	})
	t.Run("tck link to a directory", func(t *testing.T) {
		dir := writeDir(t, map[string]string{
			"tack.mod": "module m\n", "main.tck": "package main\n", "sub/x": "",
		})
		if err := os.Symlink(filepath.Join(dir, "sub"), filepath.Join(dir, "sub.tck")); err != nil {
			t.Fatal(err)
		}
		assertFailure(t, runTack(t, "--emit", dir), "is a directory")
	})
	t.Run("symbolic link loop", func(t *testing.T) {
		loop := filepath.Join(t.TempDir(), "loop")
		if err := os.Symlink(loop, loop); err != nil {
			t.Fatal(err)
		}
		assertFailure(t, runTack(t, "--emit", loop), "too many levels of symbolic links")
	})
}

// runBuilt runs the binary at bin and returns what it printed and its exit
// status.
func runBuilt(t *testing.T, bin string) (string, int) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), bin).Output()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exitErr):
		return string(out), exitErr.ExitCode()
	default:
		t.Fatalf("running %s: %v", bin, err)
		return "", 0
	}
}

// TestBuiltModuleMainsByDirectoryAndPath builds both main packages of
// examples/module-mains, each from its directory and from its import path
// in the module found from the current directory, a subdirectory of the
// module included, and checks each prints its name and exits with its own
// status.
func TestBuiltModuleMainsByDirectoryAndPath(t *testing.T) {
	root, err := filepath.Abs(filepath.Join(examplesDir, "module-mains"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, cwd, target, out string
		code                   int
	}{
		{"root by directory", ".", root, "root\n", 3},
		{"tool by directory", ".", filepath.Join(root, "tools", "hello-world"), "hello-world\n", 4},
		{"root by path", root, "example.com/module-mains", "root\n", 3},
		{"tool by path", root, "example.com/module-mains/tools/hello-world", "hello-world\n", 4},
		{
			"tool by path from below the root", filepath.Join(root, "tools"),
			"example.com/module-mains/tools/hello-world", "hello-world\n", 4,
		},
		{"root by relative directory", filepath.Join(root, "tools"), "..", "root\n", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cwd != "." {
				t.Chdir(tc.cwd)
			}
			bin := buildTargetWithClang(t, "prog", tc.target)
			if out, code := runBuilt(t, bin); out != tc.out || code != tc.code {
				t.Errorf("printed %q and exited %d, want %q and %d", out, code, tc.out, tc.code)
			}
		})
	}
}

// TestImportPathTargetFailures checks an import path target that names no
// main package fails with an error saying why.
func TestImportPathTargetFailures(t *testing.T) {
	root := writeDir(t, map[string]string{
		"tack.mod":         "module calc\n",
		"lexer/lexer.tck":  "package lexer\n",
		"cmd/x/main.tck":   "package main\nfunc main() {}\n",
		"empty/notes.txt":  "",
		"cmd/y/main.tck":   "package main\nfunc main() { undefined() }\n",
		"bad/one.tck":      "package other\n",
		"cmd/z/broken.tck": "package main\nfunc main() {\n",
	})
	t.Chdir(root)
	for _, tc := range []struct{ target, want string }{
		{"calc/nothing", "cannot find package calc/nothing: no directory calc/nothing in module calc"},
		{"other/pkg", "unknown module other in import path other/pkg"},
		{"calc//x", "invalid import path calc//x: a part between slashes is empty"},
		{"calc/a/../x", "invalid import path calc/a/../x: a part cannot be . or .."},
		{"calc/empty", "no .tck files in empty"},
	} {
		t.Run(tc.target, func(t *testing.T) {
			assertFailure(t, runTack(t, "--emit", tc.target), tc.want)
		})
	}
	for _, tc := range []struct{ target, want string }{
		{"calc/lexer", "\nlexer/lexer.tck:1:9: error: calc/lexer is not a main package"},
		{"calc/cmd/y", "\ncmd/y/main.tck:2:15: error: undefined: undefined"},
		{"calc/bad", "\nbad/one.tck:1:9: error: package other does not match its directory"},
		{"calc/cmd/z", "\ncmd/z/broken.tck:2:14: error: expected '}' found 'EOF'"},
	} {
		t.Run(tc.target, func(t *testing.T) {
			r := runTack(t, "--emit", tc.target)
			assertDiagnostics(t, r)
			if !strings.Contains(r.stderr, tc.want) {
				t.Errorf("stderr does not contain %q:\n%s", tc.want, r.stderr)
			}
		})
	}
	assertOnlyStdout(t, runTack(t, "--emit", "calc/cmd/x"))
}

// TestImportPathTargetWithoutModule checks a target that names nothing on
// disk, outside any module, fails saying both that it is no file and that
// no tack.mod was found.
func TestImportPathTargetWithoutModule(t *testing.T) {
	t.Chdir(t.TempDir())
	assertFailure(
		t,
		runTack(t, "--emit", "calc/x"),
		"calc/x is not a file or a directory, nor an import path: no tack.mod found in . or any parent directory",
	)
}

// TestBuiltPackagesRunsBothMains builds both main packages of
// examples/packages, which import the module's libraries, a diamond and a
// chain of three among them, and checks each one's output and status:
// cmd/calc checks every qualified name it uses and exits 5 + 3 + 4 + 3,
// and cmd/count exits 3 * 10 + 4. cmd/calc is built by import path too.
func TestBuiltPackagesRunsBothMains(t *testing.T) {
	root, err := filepath.Abs(filepath.Join(examplesDir, "packages"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, target, out string
		code              int
		byPath            bool
	}{
		{"calc", filepath.Join(root, "cmd", "calc"), "packages ok\n", 15, false},
		{"count", filepath.Join(root, "cmd", "count"), "", 34, false},
		{"calc by path", "calc/cmd/calc", "packages ok\n", 15, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.byPath {
				t.Chdir(root)
			}
			bin := buildTargetWithClang(t, "prog", tc.target)
			if out, code := runBuilt(t, bin); out != tc.out || code != tc.code {
				t.Errorf("printed %q and exited %d, want %q and %d", out, code, tc.out, tc.code)
			}
		})
	}
}

// tackLexer is examples/tack-lexer/cmd/lex, a lexer for a subset of Tack
// written in Tack, which lexes its standard input and prints a line per
// token.
var tackLexer = filepath.Join(examplesDir, "tack-lexer", "cmd", "lex")

// runBuiltWithStdin is runBuilt with stdin as the binary's standard input.
func runBuiltWithStdin(t *testing.T, bin, stdin string) (string, int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), bin)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exitErr):
		return string(out), exitErr.ExitCode()
	default:
		t.Fatalf("running %s: %v", bin, err)
		return "", 0
	}
}

// tackLexerProgram is a small program for examples/tack-lexer, and
// tackLexerProgramOut what it prints for it: every kind of token in the
// subset, and semicolons inserted after an identifier, a literal, ++ and
// closing brackets, and before a comment.
const (
	tackLexerProgram = "package main\n\nfunc main() {\n\tx := \"a\\tb\" // hi\n\ty := '\\n'\n" +
		"\tif x <= 10 && y != 0 { x++ }\n}\n"
	tackLexerProgramOut = `keyword 0 7 "package"
ident 8 12 "main"
; 12 12 ""
keyword 14 18 "func"
ident 19 23 "main"
( 23 24 "("
) 24 25 ")"
{ 26 27 "{"
ident 29 30 "x"
:= 31 33 ":="
string 34 40 "\"a\\tb\""
; 41 41 ""
comment 41 46 "// hi"
ident 48 49 "y"
:= 50 52 ":="
char 53 57 "'\\n'"
; 57 57 ""
keyword 59 61 "if"
ident 62 63 "x"
<= 64 66 "<="
int 67 69 "10"
&& 70 72 "&&"
ident 73 74 "y"
!= 75 77 "!="
int 78 79 "0"
{ 80 81 "{"
ident 82 83 "x"
++ 83 85 "++"
} 86 87 "}"
; 87 87 ""
} 88 89 "}"
; 89 89 ""
EOF 90 90 ""
`
)

// tackLexerErrors is a source with one of each lexical error, and
// tackLexerErrorsOut what examples/tack-lexer prints for it: the tokens,
// then the errors.
const (
	tackLexerErrors    = "s := \"a\\q\" + 'ab' + 007 @ /* c */ \xff\n\"open\n`raw"
	tackLexerErrorsOut = `ident 0 1 "s"
:= 2 4 ":="
string 5 10 "\"a\\q\""
+ 11 12 "+"
+ 18 19 "+"
int 20 23 "007"
comment 26 33 "/* c */"
; 35 35 ""
string 36 41 "\"open"
; 41 41 ""
string 42 46 "` + "`raw" + `"
; 46 46 ""
EOF 46 46 ""
error 7 9 invalid escape sequence
error 13 17 too many characters in char literal
error 20 23 integer literal has a leading zero
error 24 25 invalid character
error 34 35 invalid character
error 36 41 unterminated string
error 42 46 unterminated string
`
)

// tackLexerFull returns a source of exactly 4096 tokens, examples/tack-lexer's
// capacity, and what it prints for it: 2047 lines of x, each an identifier
// and a semicolon, then a written semicolon and the EOF.
func tackLexerFull() (string, string) {
	var out strings.Builder
	for i := range 2047 {
		at, next := strconv.Itoa(2*i), strconv.Itoa(2*i+1)
		out.WriteString("ident " + at + " " + next + " \"x\"\n; " + next + " " + next + " \"\"\n")
	}
	out.WriteString("; 4094 4095 \";\"\nEOF 4095 4095 \"\"\n")
	return strings.Repeat("x\n", 2047) + ";", out.String()
}

// TestBuiltTackLexer builds examples/tack-lexer and checks what it prints
// and its exit status: the token count for a program, an empty one
// included; 254 with every lexical error listed; and, at its capacity of
// 4096 tokens, every token when they fit exactly and only an error, with
// status 255, when one more does not.
func TestBuiltTackLexer(t *testing.T) {
	bin := buildTargetWithClang(t, "lex", tackLexer)
	full, fullOut := tackLexerFull()
	for _, tc := range []struct {
		name, in, out string
		code          int
	}{
		{"program", tackLexerProgram, tackLexerProgramOut, 33},
		{"empty", "", "EOF 0 0 \"\"\n", 1},
		{"errors", tackLexerErrors, tackLexerErrorsOut, 254},
		{"exactly full", full, fullOut, 0},
		{"one token too many", full + ";", "error: more than 4096 tokens\n", 255},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, code := runBuiltWithStdin(t, bin, tc.in)
			if out != tc.out || code != tc.code {
				t.Errorf("printed\n%s\nand exited %d, want\n%s\nand %d", out, code, tc.out, tc.code)
			}
		})
	}
}

// TestBuiltTackLexerAgreesWithTheCompiler lexes every Tack file under
// examples/, each also with CRLF line endings, and every case of
// examples/invalid.txtar, with both the Tack lexer of examples/tack-lexer
// and the compiler's own, and checks they agree: the same tokens, with the
// same kinds, spans and text; errors over the same spans, in the same
// order, though the Tack lexer's messages are fixed strings, since Tack
// cannot format one; and a status of the token count, or 254 after an
// error. Inputs of more tokens than the Tack lexer holds are skipped.
func TestBuiltTackLexerAgreesWithTheCompiler(t *testing.T) {
	bin := buildTargetWithClang(t, "lex", tackLexer)
	inputs := tackLexerInputs(t)
	compared, withErrors := 0, 0
	for name, src := range inputs {
		want, code, ok := goLexerTokens(src)
		if !ok {
			continue
		}
		compared++
		if code == 254 {
			withErrors++
		}
		out, gotCode := runBuiltWithStdin(t, bin, string(src))
		if got := withoutErrorMessages(out); got != want || gotCode != code {
			t.Errorf(
				"%s: printed\n%s\nand exited %d, want\n%s\nand %d",
				name,
				got,
				gotCode,
				want,
				code,
			)
		}
	}
	t.Logf("compared %d of %d inputs, %d with errors", compared, len(inputs), withErrors)
	if compared < len(inputs)*9/10 || withErrors == 0 {
		t.Errorf("compared %d of %d inputs, %d with errors; want nearly all, some with errors",
			compared, len(inputs), withErrors)
	}
}

// tackLexerInputs returns the sources TestBuiltTackLexerAgreesWithTheCompiler
// lexes, by name: every Tack file under examples/, as it is and with CRLF
// line endings, every case of examples/invalid.txtar, and
// tackLexerHostileInputs.
func tackLexerInputs(t *testing.T) map[string][]byte {
	t.Helper()
	inputs := map[string][]byte{}
	err := filepath.WalkDir(examplesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".tck" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		inputs[path] = data
		inputs[path+" (CRLF)"] = bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range invalidCases(t) {
		inputs["invalid.txtar/"+tc.Name] = tc.Source
	}
	for i, src := range tackLexerHostileInputs {
		inputs["hostile "+strconv.Itoa(i)] = []byte(src)
	}
	return inputs
}

// tackLexerHostileInputs are sources at the edges of what a lexer
// handles: literals cut off by a line end or the end of the source, bad
// escapes and characters, comment runs, longest-match operators, and a
// nesting that nearly fills the Tack lexer.
var tackLexerHostileInputs = []string{
	`"abc`, `"a\`, "\"a\\\nb\"", `"\x4"`, `"\xZZ"`, `"\x41\x4a0"`, `"\'"`, `'\"'`,
	`'`, `''`, `'ab'`, `'ab`, "'\n", `'\x4'`, `'\''`, "'\u00e9'", "'\xff'",
	"0x1F 0b10 0o7 1_000 0X_f", "0x", "0b_", "0b102", "0o8", "0x1g", "1__0", "1_", "0x1_",
	"0_7", "12ab", "0xZ", "/* open", "/**/x", "a\rb", "a\x00b", "\xff\xfe", "@#$",
	"x // a\n  // b\n\n// c\ny", "x // a\r\n\t// b\r\ny", "// end", "x //",
	"a&^=b<<=c>>=d...e&^f<-g", "x++\ny--\nreturn\nbreak\ncontinue\ntrue\nfalse\n)\n]\n}\n",
	"00 007 0 10", "`a\nb\r\nc`", "`open\n", "x\n\n\r\n\n",
	"1.5 .5 1. 1e9 1.5e-3 1E+2 01.5 0e5", "1e", "1e+", "2.5E-", "1e_", "1_.5", "1._5", "1e_5",
	"1_e5", "1e1_0 1_000.000_5", "1.2.3", "1..2", "x.5", "0x1.8p3", "1ex", ".", "..5", "5.e",

	strings.Repeat("(", 2000) + strings.Repeat(")", 2000),
}

// goLexerTokens lexes src with the compiler's lexer and formats its tokens
// as examples/tack-lexer prints them, and its errors without their
// messages, and returns that lexer's exit status for src. It reports false
// for a source past that lexer's capacity.
func goLexerTokens(src []byte) (string, int, bool) {
	lex := lexer.New(&source.File{ID: "in.tck", Filename: "in.tck", Data: src})
	diags := lex.Lex()
	if len(lex.Tokens()) > 4096 {
		return "", 0, false
	}
	var b strings.Builder
	for _, tok := range lex.Tokens() {
		start, end := tok.OffsetStart(), tok.OffsetEnd()
		b.WriteString(
			tok.Kind().String() + " " + strconv.Itoa(start) + " " + strconv.Itoa(end) + " ",
		)
		b.WriteString(quoteBytes(src[start:end]) + "\n")
	}
	for _, d := range diags {
		b.WriteString("error " + strconv.Itoa(d.Span.Start) + " " + strconv.Itoa(d.Span.End) + "\n")
	}
	if len(diags) > 0 {
		return b.String(), 254, true
	}
	return b.String(), len(lex.Tokens()) % 256, true
}

// withoutErrorMessages drops the message from each error line the Tack
// lexer printed, keeping "error", its start and its end.
func withoutErrorMessages(out string) string {
	lines := strings.SplitAfter(out, "\n")
	for i, line := range lines {
		if f := strings.Fields(line); len(f) > 3 && f[0] == "error" {
			lines[i] = strings.Join(f[:3], " ") + "\n"
		}
	}
	return strings.Join(lines, "")
}

// quoteBytes quotes b as examples/tack-lexer does: a quote or backslash
// escaped, a printable ASCII byte as itself, any other byte as \x and two
// lowercase hex digits.
func quoteBytes(b []byte) string {
	const hex = "0123456789abcdef"
	var s strings.Builder
	s.WriteByte('"')
	for _, c := range b {
		switch {
		case c == '"' || c == '\\':
			s.WriteByte('\\')
			s.WriteByte(c)
		case c >= ' ' && c <= '~':
			s.WriteByte(c)
		default:
			s.WriteString(`\x`)
			s.WriteByte(hex[c>>4])
			s.WriteByte(hex[c&15])
		}
	}
	s.WriteByte('"')
	return s.String()
}

// TestBuiltFailurePassesEveryCheck builds examples/failure, which imports
// std/errors, at every optimization level and checks each binary writes
// exactly examples/failure/output, every check ok: Failures built from a
// literal and by from_bytes, read, printed, moved through calls, results,
// a list and a map, and made with every allocation refused in turn, the
// count of live buffers back to 0 after each part.
func TestBuiltFailurePassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want, err := os.ReadFile(filepath.Join(examplesDir, "failure", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(want), "FAIL") {
		t.Fatal("examples/failure/output expects a check to fail")
	}
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "failure")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("failure"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != string(want) || code != 0 {
				t.Errorf("wrote\n%q\nand exited %d, want\n%q\nand 0", out, code, want)
			}
		})
	}
}
