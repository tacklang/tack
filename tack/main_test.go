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
	includeBackend = "test-include" // "builds" by writing std's include directory to --out
)

// fakeToolchain is a backend that generates C with the real c backend but
// builds with argv instead of clang. In argv, "{src}" and "{out}" are
// replaced by the build's source and output paths, "{opt}" by its
// optimization level, "{include}" by std's include directory, and
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
		{name: sleepBackend, argv: []string{"sleep", "30"}},
		{name: optBackend, argv: []string{"sh", "-c", `printf %s "$1" > "$2"`, "sh", "{opt}", "{out}"}},
		{name: includeBackend, argv: []string{
			"sh", "-c", `printf %s "$1" > "$2"`, "sh", "{include}", "{out}",
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
	lib := &std{builtins: builtins, dir: stdtest.Dir()}
	res, single, err := load(path, dumpNone, lib)
	if err != nil {
		t.Fatal(err)
	}
	want, err := compile(res, single, c.New(), builtins, dumpNone, 0)
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
				strings.Repeat("}\n", 300) + "return 0\n}\nfunc main() {\n    code, _ := int32(f())\n    exit(code)\n}\n",
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

// FuzzRunEmit runs the driver's --emit on arbitrary source as a single
// file and checks the result is coherent, never a panic. It is seeded with
// every Tack file under examples/, each alone, and every invalid case.
func FuzzRunEmit(f *testing.F) {
	paths, _ := filepath.Glob(filepath.Join(examplesDir, "*", "*.tck"))
	for _, p := range paths {
		if src, err := os.ReadFile(p); err == nil {
			f.Add(src)
		}
	}
	for _, tc := range invalidCases(f) {
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
// down indexing every element; a division guarded by ||, and an index by
// a case with two values, on both sides of the guard; the int8 minimum
// divided by -1 giving itself and true; the = form, parentheses, _, and
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
		"int8 min / -1 wraps ok\n" +
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
error 26 33 block comments are not supported
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
// error. Inputs outside the Tack lexer's subset, with a float or an asm
// block, are skipped, and so are those of more tokens than it holds.
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
	"/* open", "/**/x", "a\rb", "a\x00b", "\xff\xfe", "@#$",
	"x // a\n  // b\n\n// c\ny", "x // a\r\n\t// b\r\ny", "// end", "x //",
	"a&^=b<<=c>>=d...e&^f<-g", "x++\ny--\nreturn\nbreak\ncontinue\ntrue\nfalse\n)\n]\n}\n",
	"00 007 0 10", "`a\nb\r\nc`", "`open\n", "x\n\n\r\n\n",
	strings.Repeat("(", 2000) + strings.Repeat(")", 2000),
}

// goLexerTokens lexes src with the compiler's lexer and formats its tokens
// as examples/tack-lexer prints them, and its errors without their
// messages, and returns that lexer's exit status for src. It reports false
// for a source outside that lexer's subset or capacity.
func goLexerTokens(src []byte) (string, int, bool) {
	lex := lexer.New(&source.File{ID: "in.tck", Filename: "in.tck", Data: src})
	diags := lex.Lex()
	if len(lex.Tokens()) > 4096 {
		return "", 0, false
	}
	var b strings.Builder
	for _, tok := range lex.Tokens() {
		if tok.Kind() == lexer.Float || tok.Kind() == lexer.Keyword && tok.Value() == lexer.ASM {
			return "", 0, false
		}
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
