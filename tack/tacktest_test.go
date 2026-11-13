package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// tackTestDir is examples/tack-test, the module of packages tack test is
// run on.
var tackTestDir = filepath.Join(examplesDir, "tack-test")

// runTackTest runs tack test with args in process, skipping the test
// when clang, which every run builds with, is not on PATH.
func runTackTest(t *testing.T, args ...string) result {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	return runTackEnv(t, t.Context(), testEnv(t), append([]string{testCommand}, args...)...)
}

// tackTestPkg returns the directory of the package name of
// examples/tack-test.
func tackTestPkg(name string) string {
	return filepath.Join(tackTestDir, name)
}

// shownDir returns the name diagnostics and reports give the directory
// examples/tack-test: its absolute path, as it is above the current
// directory, with a slash after it.
func shownDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(tackTestDir)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(abs) + "/"
}

// assertRun checks r exited with code and wrote exactly stdout and
// stderr, $DIR/ in either standing for shownDir.
func assertRun(t *testing.T, r result, code int, stdout, stderr string) {
	t.Helper()
	dir := shownDir(t)
	stdout = strings.ReplaceAll(stdout, "$DIR/", dir)
	stderr = strings.ReplaceAll(stderr, "$DIR/", dir)
	if r.code != code {
		t.Errorf("exit code %d, want %d", r.code, code)
	}
	if r.stdout != stdout {
		t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, stdout)
	}
	if r.stderr != stderr {
		t.Errorf("stderr:\n%s\nwant:\n%s", r.stderr, stderr)
	}
}

// TestTackTestPasses runs the tests of tt/pass, which all pass, in the
// order of their files' names and then of the source, with -v, and
// without, which writes the last line alone.
func TestTackTestPasses(t *testing.T) {
	r := runTackTest(t, "-v", tackTestPkg("pass"))
	assertRun(t, r, exitOK, "", "ok test_first\nok test_\nok test_sum\nok test_parse\n"+
		"ok test_lists\nok test_with_blank\nok tt/pass: 6 tests\n")
	r = runTackTest(t, tackTestPkg("pass"))
	assertRun(t, r, exitOK, "", "ok tt/pass: 6 tests\n")
}

// failStderr is what the tests of tt/fail write to standard error: a
// report of each failure, at the place it was written, or at the test
// for an error returned plainly and a leak, and the last line.
const failStderr = `$DIR/fail/fail_test.tck:28:5: fail: test_errorf: first 1
27 |	func test_errorf(mut t testing.T) error {
28 |	    mut t.errorf("first %d", 1)
   |	    ^~~~~~~~~~~~~~~~~~~~~~~~~~~
29 |	    mut t.errorf("second %s %t", "and", true)

$DIR/fail/fail_test.tck:29:5: fail: test_errorf: second and true
28 |	    mut t.errorf("first %d", 1)
29 |	    mut t.errorf("second %s %t", "and", true)
   |	    ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
30 |	    fmt.println("errorf went on")

$DIR/fail/fail_test.tck:50:16: fail: test_fatalf: stopped at 1
49 |	    if lookup("a")? == 1 {
50 |	        return mut t.fatalf("stopped at %v", 1)
   |	               ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
51 |	    }

$DIR/fail/fail_test.tck:59:10: fail: test_question: returned error 3: no such key
58 |	    n := lookup("a")?
59 |	    m := lookup("b")?
   |	         ^~~~~~~~~~~~
60 |	    mut t.errorf("the ? did not return: %d %d", n, m)

$DIR/fail/fail_test.tck:65:6: fail: test_plain_return: returned error 3: no such key
64 |	// test_plain_return returns an error with no ?, reported at the test.
65 |	func test_plain_return(mut t testing.T) error {
   |	     ^~~~~~~~~~~~~~~~~
66 |	    _, err := lookup("z")

$DIR/fail/fail_test.tck:71:6: fail: test_leak: 1 buffer allocated during the test was not freed
70 |	// test_leak keeps a buffer of c_allocator's past its end.
71 |	func test_leak(mut t testing.T) error {
   |	     ^~~~~~~~~
72 |	    hold()

$DIR/fail/fail_test.tck:100:5: fail: test_errorf_then_fatalf: before
99 |	func test_errorf_then_fatalf(mut t testing.T) error {
100 |	    mut t.errorf("before")
   |	    ^~~~~~~~~~~~~~~~~~~~~~
101 |	    return mut t.fatalf("then %s", "stop")

$DIR/fail/fail_test.tck:101:12: fail: test_errorf_then_fatalf: then stop
100 |	    mut t.errorf("before")
101 |	    return mut t.fatalf("then %s", "stop")
   |	           ^~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
102 |	}

FAIL tt/fail: 6 of 10 tests failed
`

// TestTackTestFailures runs the tests of tt/fail, which fail every way
// but exit, and checks every report, what the tests print, and that the
// run exits 1. errorf lets its test go on, which prints; fatalf, a ? and
// a plain return stop theirs; the tests between them pass, the one after
// the leak freeing it, and the one after test_refuse allocating, since
// the runner makes c_allocator grant every request again.
func TestTackTestFailures(t *testing.T) {
	r := runTackTest(t, tackTestPkg("fail"))
	assertRun(t, r, exitFailure, "errorf went on\n", failStderr)
}

// TestTackTestFailuresVerbose checks -v's line for each test, after its
// reports, FAIL for a failed one.
func TestTackTestFailuresVerbose(t *testing.T) {
	r := runTackTest(t, "-v", "-run", "_p", tackTestPkg("fail"))
	assertRun(t, r, exitFailure, "", "ok test_passes\n"+
		`$DIR/fail/fail_test.tck:65:6: fail: test_plain_return: returned error 3: no such key
64 |	// test_plain_return returns an error with no ?, reported at the test.
65 |	func test_plain_return(mut t testing.T) error {
   |	     ^~~~~~~~~~~~~~~~~
66 |	    _, err := lookup("z")

FAIL test_plain_return
FAIL tt/fail: 1 of 2 tests failed
`)
}

// TestTackTestRun checks -run: a substring of several names runs those,
// in order; one of a single name runs it; the pattern is matched case
// and all; and one that matches none says so and exits 1.
func TestTackTestRun(t *testing.T) {
	pass := tackTestPkg("pass")
	r := runTackTest(t, "-v", "-run", "i", pass)
	assertRun(t, r, exitOK, "", "ok test_first\nok test_lists\nok test_with_blank\n"+
		"ok tt/pass: 3 tests\n")
	r = runTackTest(t, "-run", "test_sum", pass)
	assertRun(t, r, exitOK, "", "ok tt/pass: 1 test\n")
	r = runTackTest(t, "-run", "SUM", pass)
	assertRun(t, r, exitFailure, "", "tack test: -run \"SUM\" matched no test in tt/pass\n")
	r = runTackTest(t, "-run", "", pass)
	assertRun(t, r, exitOK, "", "ok tt/pass: 6 tests\n")
}

// TestTackTestExit checks a test that calls exit, with 3 and with 0: the
// run ends there, the test is reported at its declaration with the
// number of the tests after it that would have run, -run counted, the
// run's last line follows, counting the test with the failures before it
// of every test that would have run, and tack test exits 1. What the
// tests printed before is all there.
func TestTackTestExit(t *testing.T) {
	exit3 := `$DIR/exits/exits_test.tck:14:6: fail: test_exit called exit(3); %s did not run
13 |	// freeing the list, and fails the test.
14 |	func test_exit(mut t testing.T) error {
   |	     ^~~~~~~~~
15 |	    mut l := make(list[int64], c_allocator)

`
	r := runTackTest(t, "-v", tackTestPkg("exits"))
	assertRun(t, r, exitFailure, "before ran\nexiting\n", "ok test_before\n"+fmt.Sprintf(exit3, "2 tests")+
		"FAIL tt/exits: 1 of 4 tests failed\n")
	r = runTackTest(t, "-run", "exit", tackTestPkg("exits"))
	assertRun(t, r, exitFailure, "exiting\n", fmt.Sprintf(exit3, "0 tests")+
		"FAIL tt/exits: 1 of 1 test failed\n")
	r = runTackTest(t, "-run", "_too", tackTestPkg("exits"))
	assertRun(t, r, exitOK, "after_too ran\n", "ok tt/exits: 1 test\n")

	r = runTackTest(t, tackTestPkg("exitzero"))
	assertRun(t, r, exitFailure, "",
		`$DIR/exitzero/exitzero_test.tck:16:6: fail: test_exit_zero called exit(0); 1 test did not run
15 |	// test_exit_zero ends the process with status 0.
16 |	func test_exit_zero(mut t testing.T) error {
   |	     ^~~~~~~~~~~~~~
17 |	    exit(0)

FAIL tt/exitzero: 1 of 3 tests failed
`)
}

// TestTackTestExitFromC checks a test whose C ends the process with C's
// own exit, status 0, is reported as ending it from C, the run's last
// line after it, and fails the run; and that a test calling exit(141),
// the status of a broken pipe, is taken for one: nothing more is
// written, and tack test says how the binary ended.
func TestTackTestExitFromC(t *testing.T) {
	r := runTackTest(t, tackTestPkg("cexit"))
	assertRun(t, r, exitFailure, "",
		`$DIR/cexit/cexit_test.tck:18:6: fail: test_quits ended the process from C; 1 test did not run
17 |	// test_quits ends the process from C, with a status of 0.
18 |	func test_quits(mut t testing.T) error {
   |	     ^~~~~~~~~~
19 |	    quit(0)

FAIL tt/cexit: 1 of 3 tests failed
`)
	r = runTackTest(t, "-v", tackTestPkg("exit141"))
	assertRun(t, r, exitFailure, "",
		"ok test_first\nFAIL tt/exit141: the test binary exited with status 141\n")
}

// TestTackTestQuestionPlaces checks where a returned error is reported:
// a ? in a closure in a test stores no place, so the test's plain return
// of its error is reported at the test; a ? in a test whose T is named _
// is reported at the ?; and a plain return after a function the test
// called with its T returned through a ? reports that ?, the last place
// stored in the T.
func TestTackTestQuestionPlaces(t *testing.T) {
	r := runTackTest(t, tackTestPkg("question"))
	assertRun(t, r, exitFailure, "",
		`$DIR/question/question_test.tck:24:6: fail: test_closure: returned error 4: failed
23 |	// test_closure returns the error of a ? in a closure plainly.
24 |	func test_closure(mut t testing.T) error {
   |	     ^~~~~~~~~~~~
25 |	    err := apply(func() error {

$DIR/question/question_test.tck:37:10: fail: test_blank_t: returned error 4: failed
36 |	func test_blank_t(mut _ testing.T) error {
37 |	    n := fails()?
   |	         ^~~~~~~~
38 |	    if n != 0 {

$DIR/question/question_test.tck:37:10: fail: test_inner_question: returned error 4: failed
36 |	func test_blank_t(mut _ testing.T) error {
37 |	    n := fails()?
   |	         ^~~~~~~~
38 |	    if n != 0 {

FAIL tt/question: 3 of 3 tests failed
`)
}

// TestTackTestRelativeTMPDIR checks a relative TMPDIR, which names
// another place from the package's directory the binary runs in, still
// finds the binary.
func TestTackTestRelativeTMPDIR(t *testing.T) {
	pass, err := filepath.Abs(tackTestPkg("pass"))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	if err := os.Mkdir("tmp", 0o750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", "tmp")
	r := runTackTest(t, pass)
	assertRun(t, r, exitOK, "", "ok tt/pass: 6 tests\n")
	entries, err := os.ReadDir("tmp")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("left behind in TMPDIR: %s", e.Name())
	}
}

// TestTackTestPackages checks tack test on packages of other kinds: a
// main package, whose main never runs; one with no test file, whose run
// has 0 tests, and whose -run matches none; one whose tests import other
// packages, named by its import path from the module's root; one whose
// test reads a file beside it, by a relative path; and std/hex, a std
// package, whose tests import std/testing with no cycle.
func TestTackTestPackages(t *testing.T) {
	assertRun(t, runTackTest(t, tackTestPkg("withmain")), exitOK, "", "ok tt/withmain: 1 test\n")
	assertRun(t, runTackTest(t, tackTestPkg("notests")), exitOK, "", "ok tt/notests: 0 tests\n")
	assertRun(t, runTackTest(t, "-run", "x", tackTestPkg("notests")), exitFailure, "",
		"tack test: -run \"x\" matched no test in tt/notests\n")
	assertRun(t, runTackTest(t, tackTestPkg("cwd")), exitOK, "", "ok tt/cwd: 1 test\n")
	assertRun(t, runTackTest(t, "-v", filepath.Join("..", "src", "std", "hex")), exitOK, "",
		"ok test_digit\nok test_append_encode\nok test_append_encode_empty\nok std/hex: 3 tests\n")

	t.Chdir(tackTestDir)
	assertRun(t, runTackTest(t, "tt/imports"), exitOK, "", "ok tt/imports: 2 tests\n")
}

// TestTackTestOptLevels checks every -O level builds the tests alike.
func TestTackTestOptLevels(t *testing.T) {
	for _, level := range []string{"-O0", "-O2", "-Os"} {
		r := runTackTest(t, level, "-run", "_p", tackTestPkg("fail"))
		if r.code != exitFailure || !strings.HasSuffix(r.stderr, "FAIL tt/fail: 1 of 2 tests failed\n") {
			t.Errorf("%s: exit %d, stderr:\n%s", level, r.code, r.stderr)
		}
	}
}

// writeModule writes files, by path, into a fresh module named m and
// returns its root.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files["tack.mod"] = "module m\n"
	for name, data := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestTackTestManyTests runs 2000 tests, each named and declared apart,
// every one of them passing but the last, whose report names it.
func TestTackTestManyTests(t *testing.T) {
	const n = 2000
	var b strings.Builder
	b.WriteString("package m\n\nimport testing from \"std/testing\"\n")
	for i := range n {
		fmt.Fprintf(&b, "\nfunc test_%04d(mut t testing.T) error {\n", i)
		if i == n-1 {
			fmt.Fprintf(&b, "    mut t.errorf(\"last of %%d\", %d)\n", n)
		}
		b.WriteString("    return noerror\n}\n")
	}
	root := writeModule(t, map[string]string{"many_test.tck": b.String()})
	t.Chdir(root)
	r := runTackTest(t, ".")
	line := fmt.Sprintf("many_test.tck:%d:5: fail: test_1999: last of 2000\n", 4+(n-1)*4+2)
	if r.code != exitFailure || !strings.HasPrefix(r.stderr, line) ||
		!strings.HasSuffix(r.stderr, "FAIL m: 1 of 2000 tests failed\n") {
		t.Errorf("exit %d, stderr:\n%s", r.code, r.stderr)
	}
	r = runTackTest(t, "-run", "test_00", ".")
	assertRun(t, r, exitOK, "", "ok m: 100 tests\n")
}

// TestTackTestOddBytes checks names and a pattern with bytes a string
// literal must escape reach the reports exactly: a test file whose name
// holds a quote, a backslash, a space and bytes that are not ASCII, and
// a -run pattern of such bytes, which matches nothing.
func TestTackTestOddBytes(t *testing.T) {
	name := "odd \"q\" \\ \xc3\xa9\x01_test.tck"
	root := writeModule(t, map[string]string{
		name: "package m\n\nimport testing from \"std/testing\"\n\n" +
			"func test_odd(mut t testing.T) error {\n    mut t.errorf(\"in %s\", \"odd\")\n" +
			"    return noerror\n}\n",
	})
	t.Chdir(root)
	r := runTackTest(t, ".")
	want := name + ":6:5: fail: test_odd: in odd\n"
	if r.code != exitFailure || !strings.HasPrefix(r.stderr, want) {
		t.Errorf("exit %d, stderr:\n%q\nwant it to start %q", r.code, r.stderr, want)
	}
	pattern := "\"\\\xff\x00"
	r = runTackTest(t, "-run", pattern, ".")
	assertRun(t, r, exitFailure, "", "tack test: -run \""+pattern+"\" matched no test in m\n")
}

// TestTackTestShapeErrors checks every way a test can be declared wrong
// is an error at its name, which stops the run before anything is
// built, with nothing of the generated main reported, while a function
// named test_ in a file that is not a test file is no test.
func TestTackTestShapeErrors(t *testing.T) {
	root := writeModule(t, map[string]string{
		"m.tck": "package m\n\n// test_plain is no test.\npub func test_plain(n int64) int64 {\n" +
			"    return n\n}\n",
		"m_test.tck": "package m\n\nimport testing from \"std/testing\"\n\n" +
			"func test_no_params() error {\n    return noerror\n}\n\n" +
			"func test_not_mut(t testing.T) error {\n    return noerror\n}\n\n" +
			"func test_no_result(mut t testing.T) {\n}\n",
	})
	t.Chdir(root)
	r := runTackTest(t, ".")
	assertDiagnostics(t, r)
	for _, name := range []string{"test_no_params", "test_not_mut", "test_no_result"} {
		want := fmt.Sprintf("error: %s is a test, so it must be declared func %s(mut t testing.T) error, "+
			"not generic and not a method\n", name, name)
		if !strings.Contains(r.stderr, want) {
			t.Errorf("stderr does not report %s:\n%s", name, r.stderr)
		}
	}
	if strings.Contains(r.stderr, "tack test main") || strings.Contains(r.stderr, "internal error") ||
		strings.Contains(r.stderr, "test_plain") {
		t.Errorf("stderr reports more than the three tests:\n%s", r.stderr)
	}
}

// TestTackTestSyntaxError checks a test file with a syntax error is
// reported as any file's is, and nothing runs.
func TestTackTestSyntaxError(t *testing.T) {
	root := writeModule(t, map[string]string{
		"m.tck":      "package m\n\npub func One() int64 {\n    return 1\n}\n",
		"m_test.tck": "package m\n\nimport testing from \"std/testing\"\n\nfunc test_x(mut t testing.T) error {\n    return\n",
	})
	t.Chdir(root)
	r := runTackTest(t, ".")
	assertDiagnostics(t, r)
	if !strings.Contains(r.stderr, "m_test.tck:") || strings.Contains(r.stderr, "internal error") {
		t.Errorf("stderr:\n%s", r.stderr)
	}
}

// TestTackTestNameClash checks a package declaring a name the generated
// main declares is told so at its declaration, and nothing more.
func TestTackTestNameClash(t *testing.T) {
	root := writeModule(t, map[string]string{
		"m.tck": "package m\n\npub func tack_test_main_() {\n}\n",
	})
	t.Chdir(root)
	r := runTackTest(t, ".")
	assertDiagnostics(t, r)
	want := "m.tck:3:10: error: tack_test_main_ is declared by the main tack test generates; rename it\n"
	if !strings.HasPrefix(r.stderr, "\n"+want) || strings.Count(r.stderr, "error:") != 1 {
		t.Errorf("stderr:\n%s\nwant it to start with:\n%s", r.stderr, want)
	}
}

// TestTackTestImportCycle checks a test importing a package that imports
// the package being tested is an import cycle.
func TestTackTestImportCycle(t *testing.T) {
	root := writeModule(t, map[string]string{
		"a/a.tck":      "package a\n\npub func One() int64 {\n    return 1\n}\n",
		"a/a_test.tck": "package a\n\nimport b from \"m/b\"\nimport testing from \"std/testing\"\n\nfunc test_b(mut t testing.T) error {\n    if b.Two() != 2 {\n        mut t.errorf(\"no\")\n    }\n    return noerror\n}\n",
		"b/b.tck":      "package b\n\nimport a from \"m/a\"\n\npub func Two() int64 {\n    return a.One() + 1\n}\n",
	})
	t.Chdir(root)
	r := runTackTest(t, "./a")
	assertDiagnostics(t, r)
	if !strings.Contains(r.stderr, "error: import cycle: m/a imports m/b imports m/a\n") {
		t.Errorf("stderr:\n%s", r.stderr)
	}
}

// TestTackTestUsage checks tack test's command-line mistakes exit 2 and
// point at its help, and that the help lists its flags.
func TestTackTestUsage(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"a", "b"}, "expected one directory or import path, got 2 arguments"},
		{[]string{"-O0", "-O2"}, "-O0 and -O2 cannot be combined"},
		{[]string{"--out", "x"}, "flag provided but not defined: -out"},
		{[]string{""}, "the directory or import path is empty"},
		{[]string{example("hello")}, "is a file; tack test takes the directory of a package"},
	} {
		r := runTackEnv(t, t.Context(), testEnv(t), append([]string{testCommand}, tc.args...)...)
		if r.code != exitUsage || r.stdout != "" ||
			!strings.HasPrefix(r.stderr, "tack test: ") || !strings.Contains(r.stderr, tc.want) ||
			!strings.HasSuffix(r.stderr, "Run 'tack test --help' for usage.\n") {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", tc.args, r.code, r.stdout, r.stderr)
		}
	}
	r := runTackEnv(t, t.Context(), testEnv(t), testCommand, "--help")
	if r.code != exitOK || r.stderr != "" {
		t.Fatalf("--help: exit %d, stderr %q", r.code, r.stderr)
	}
	for _, flag := range []string{"-run <text>", "-v\n", "-O0, -O1", "-h, --help"} {
		if !strings.Contains(r.stdout, flag) {
			t.Errorf("help does not list %q:\n%s", flag, r.stdout)
		}
	}
	main := runTackEnv(t, t.Context(), testEnv(t), "--help")
	if !strings.Contains(main.stdout, "tack test [-run text]") {
		t.Errorf("tack --help does not mention tack test:\n%s", main.stdout)
	}
}

// TestTackTestMissingTarget checks a target that is neither a directory
// nor an import path of the module is an error.
func TestTackTestMissingTarget(t *testing.T) {
	t.Chdir(tackTestDir)
	r := runTackTest(t, "tt/nowhere")
	if r.code != exitFailure || !strings.Contains(r.stderr, "cannot find package tt/nowhere") {
		t.Errorf("exit %d, stderr %q", r.code, r.stderr)
	}
	r = runTackTest(t, "./nowhere")
	if r.code != exitFailure || !strings.HasPrefix(r.stderr, "tack: cannot find ./nowhere") {
		t.Errorf("exit %d, stderr %q", r.code, r.stderr)
	}
}
