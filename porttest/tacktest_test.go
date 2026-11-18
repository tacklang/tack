package porttest

import (
	"bytes"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// testExamplesStage is the stage of tack test: each case runs tack test,
// with its own flags and target, with both compilers (see runTestCase).
const testExamplesStage = "test-examples"

// init adds test-examples to stages.
func init() {
	stages = append(stages, stage{
		name:   testExamplesStage,
		args:   func(in input) []string { return testArgs(in) },
		inputs: testInputs,
		stdout: compareBytes,
		run:    runTestCase,
		done:   checkSavedCompared,
	})
}

// testRun is how one case of test-examples runs tack test: the
// arguments after test, the target among them or none, and whether the
// compilers' standard output is a pipe whose reader is closed, as the
// Go compiler's TestTackTestBrokenPipe runs examples/tack-test/pipe.
type testRun struct {
	args         []string
	env          []string
	linkHook     string
	closedStdout bool
	leaves       bool
}

// testRuns holds every case's run, by its input's name, which
// testInputs fills once a run.
var testRuns = map[string]testRun{}

// testArgs returns the command line of in, a case of test-examples:
// test, then its arguments.
func testArgs(in input) []string {
	return append([]string{"test"}, testRuns[in.name].args...)
}

// testCorpusDirs are the corpus's packages tack test runs on, by their
// paths from this module's root, or from env.home for std's (see
// corpusDir): every package of examples/tack-test,
// examples/test-files, whose test file _test.tck does not lex, and
// std/hex, the one package of std with a test file.
var testCorpusDirs = []string{
	"examples/tack-test/cexit",
	"examples/tack-test/cwd",
	"examples/tack-test/exit141",
	"examples/tack-test/exits",
	"examples/tack-test/exitzero",
	"examples/tack-test/fail",
	"examples/tack-test/imports",
	"examples/tack-test/notests",
	"examples/tack-test/pass",
	"examples/tack-test/pipe",
	"examples/tack-test/question",
	"examples/tack-test/withmain",
	"examples/test-files",
	"examples/test-files/onlytests",
	"src/std/hex",
}

// testFlagSets are the flags each corpus package is run with, each a
// case named by its package's path and the suffix: none, -v, the -run
// patterns the Go compiler's tests run the corpus with, each of which
// some tests' names hold and others' do not, and -O2.
var testFlagSets = []struct {
	suffix string
	flags  []string
}{
	{"", nil},
	{":v", []string{"-v"}},
	{":run", []string{"-run", "_p"}},
	{":run-v", []string{"-v", "-run", "i"}},
	{":run-exit", []string{"-run", "exit"}},
	{":run-too", []string{"-v", "-run", "_too"}},
	{":O2", []string{"-O2"}},
}

// testTargets are corpus cases that name their package other than as
// ., run from another directory: by import path from the module's
// root, by a relative path from above, and std/hex from std/testing, as
// the Go compiler's TestTackTestPackages runs them.
var testTargets = []struct {
	name, dir string
	args      []string
}{
	{"examples/tack-test:tt/imports", "examples/tack-test", []string{"tt/imports"}},
	{"examples/tack-test:tt/pass", "examples/tack-test", []string{"-v", "tt/pass"}},
	{"examples/tack-test:./fail", "examples/tack-test", []string{"-run", "_p", "./fail"}},
	{"examples/tack-test:tt/nowhere", "examples/tack-test", []string{"tt/nowhere"}},
	{"examples/tack-test:./nowhere", "examples/tack-test", []string{"./nowhere"}},
	{"examples:tack-test/cwd", "examples", []string{"tack-test/cwd"}},
	{"src/std/testing:../hex", "src/std/testing", []string{"-v", "../hex"}},
}

// testInputsOnce makes test-examples' inputs once a run.
var testInputsOnce struct {
	once sync.Once
	ins  []input
	err  error
}

// testInputs returns test-examples' inputs: each of testCorpusDirs run
// from its own directory with . as its target and each of
// testFlagSets, examples/tack-test/pipe with -v and its standard output
// a closed pipe too, each of testTargets, and each of testProbes,
// written once a run below the run's temporary directory.
func testInputs() ([]input, error) {
	testInputsOnce.once.Do(func() { testInputsOnce.ins, testInputsOnce.err = makeTestInputs() })
	return testInputsOnce.ins, testInputsOnce.err
}

// corpusDir returns the directory of the corpus at rel, a path with
// slashes: below env.home for a path in src/std, the std the comparisons
// read, and below this module's root for any other.
func corpusDir(rel string) string {
	if rel == "src/std" || strings.HasPrefix(rel, "src/std/") {
		return filepath.Join(env.home, filepath.FromSlash(rel))
	}
	return filepath.Join(env.root, filepath.FromSlash(rel))
}

// makeTestInputs makes testInputs' inputs and fills testRuns.
func makeTestInputs() ([]input, error) {
	var ins []input
	add := func(name, dir string, r testRun) {
		target := "."
		for i := len(r.args) - 1; i >= 0; i-- {
			if !strings.HasPrefix(r.args[i], "-") && (i == 0 || r.args[i-1] != "-run") {
				target = r.args[i]
				break
			}
		}
		testRuns[name] = r
		ins = append(ins, input{name: name, dir: dir, file: target})
	}
	for _, d := range testCorpusDirs {
		for _, fs := range testFlagSets {
			add(d+fs.suffix, corpusDir(d), testRun{args: fs.flags})
		}
	}
	add("examples/tack-test/pipe:closed", filepath.Join(env.root, "examples", "tack-test", "pipe"),
		testRun{args: []string{"-v"}, closedStdout: true})
	for _, tt := range testTargets {
		add(tt.name, corpusDir(tt.dir), testRun{args: tt.args})
	}
	root := filepath.Join(env.tmp, "test-probes")
	for _, p := range testProbes {
		dir := filepath.Join(root, p.name)
		files := maps.Clone(p.files)
		if files == nil {
			files = map[string]string{}
		}
		if p.from != "" {
			if err := probeFiles(p.from, files); err != nil {
				return nil, err
			}
		}
		files["tack.mod"] = "module m\n"
		for name, data := range files {
			f := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
				return nil, fmt.Errorf("writing test probe %s: %w", p.name, err)
			}
			if err := os.WriteFile(f, []byte(data), 0o644); err != nil { //nolint:gosec // a probe's source
				return nil, fmt.Errorf("writing test probe %s: %w", p.name, err)
			}
		}
		add("probe/"+p.name, dir, testRun{args: p.args, env: p.env, linkHook: p.linkHook, leaves: p.leaves})
	}
	slices.SortFunc(ins, func(a, b input) int { return strings.Compare(a.name, b.name) })
	return ins, nil
}

// TestTestLateDecline checks lateDeclineMismatch for tack test: a decline after
// what Go wrote before its tack: line matches, and one where Go wrote
// no such line, as after a test binary's run, does not.
func TestTestLateDecline(t *testing.T) {
	const line = "tack: not implemented in the port: test with a temp directory that cannot be removed\n"
	ran := []byte("FAIL m/p: 1 of 1 test failed\n")
	if why := lateDeclineMismatch(result{stderr: ran, code: 1},
		result{stderr: append(slices.Clone(ran), line...), code: 3}, line); why == "" {
		t.Error("a late decline where Go wrote no tack: line matches, want a mismatch")
	}
	goErr := append(slices.Clone(ran), "tack: failed to delete temp directory: x\n"...)
	if why := lateDeclineMismatch(result{stderr: goErr, code: 1},
		result{stderr: append(slices.Clone(ran), line...), code: 3}, line); why != "" {
		t.Errorf("a late decline in place of Go's tack: line: %s", why)
	}
}

// tmpHidden returns r with the random part of each directory a build
// makes in tmp hidden (see tempNamesHidden), and tmp itself, a directory
// of the case's own, written $TMPDIR, so that an entry of the divergence
// list can record a message naming a path below it.
func tmpHidden(r result, tmp string) result {
	r = tempNamesHidden(r, tmp)
	r.stdout = bytes.ReplaceAll(r.stdout, []byte(tmp), []byte("$TMPDIR"))
	r.stderr = bytes.ReplaceAll(r.stderr, []byte(tmp), []byte("$TMPDIR"))
	return r
}

// probeFiles adds to files every file of the directory of
// testdata/probes named from, by its slash path there, but its target,
// which the probes stage reads.
func probeFiles(from string, files map[string]string) error {
	root := filepath.Join(env.root, "porttest", "testdata", "probes", from)
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == "target" {
			return err
		}
		data, err := os.ReadFile(p) //nolint:gosec // G304: a probe's own file
		if err != nil {
			return fmt.Errorf("reading probe %s: %w", from, err)
		}
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
}

// runTestCase runs the case in of test-examples, st, with portBin as the
// port: each compiler runs tack test, the port first, in a fresh copy of
// the case's tree at the same path, with a read-only TACK_HOME and a
// fresh TACK_CACHE and TMPDIR at the same paths, as a build stage's case
// builds (see prepareBuild), the Go compiler through goStackWrapper. It
// compares the standard output, standard error and exit status byte for
// byte, but for the random part of a directory a build makes in TMPDIR
// (see tempNamesHidden), and the files each copy holds afterwards; and
// it fails a run that leaves anything in TMPDIR. A decline before
// anything is written skips, or fails in a required stage, and a late
// one is held to the Protocol, as a build's is.
func runTestCase(t *testing.T, st stage, in input, portBin string) {
	t.Helper()
	base := t.TempDir()
	d := newBuildDirs(base)
	readOnlyHome(t, d.home)
	port := testOnce(t, portBin, d, in, filepath.Join(base, "port.c"))
	if port.res.earlyDecline() {
		declined(t, st, in, port.res)
	}
	goRun, err := goStackWrapper()
	if err != nil {
		t.Fatal(err)
	}
	want := testOnce(t, goRun, d, in, filepath.Join(base, "go.c"))
	port.res, want.res = tmpHidden(port.res, d.tmp), tmpHidden(want.res, d.tmp)
	if line, late := port.res.lateDecline(); late {
		if why := lateDeclineMismatch(want.res, port.res, line); why != "" {
			t.Fatalf("the port declines after writing what Go does not: %s", why)
		}
		declined(t, st, in, result{stderr: []byte(line), code: notImplementedCode})
	}
	compareResults(t, st, want.in, testArgs(in), want.res, port.res)
	if diff := filesDiff(want.files, port.files); diff != "" {
		t.Errorf("the files the runs leave differ:%s", diff)
	}
	_, listed := divergenceFor(t, st, in)
	compareSavedC(t, want.bin, port.bin, listed)
}

// savedCompared counts the cases of test-examples whose generated C
// compareSavedC compared, in a run, which checkSavedCompared checks.
var savedCompared atomic.Int64

// checkSavedCompared is test-examples' done: it fails t, the stage's
// test, when no case of it compared the generated C of its test binary,
// as when the saving clang saves nothing.
func checkSavedCompared(t *testing.T) {
	if n := savedCompared.Load(); n == 0 {
		t.Error("no case compared the generated C of its test binary")
	} else {
		t.Logf("%d cases compared the generated C of their test binary", n)
	}
}

// compareSavedC fails t when goC and portC, the generated C each
// compiler's build of the test binary linked, as savingClangDir's clang
// saved it, differ: the port must generate Go's C for a package's tests,
// the store of each ? in a test and the entry point among it. A build
// that linked nothing, as one that stopped before the link, saved none;
// one compiler saving C and the other not fails, but for a case the
// divergence list holds, listed, where the Go compiler's build fails
// before its link.
func compareSavedC(t *testing.T, goC, portC string, listed bool) {
	t.Helper()
	want, errGo := os.ReadFile(goC)
	got, errPort := os.ReadFile(portC)
	switch {
	case errGo != nil && errPort != nil:
		return
	case (errGo != nil) != (errPort != nil) && listed:
		return
	case errGo != nil:
		t.Error("the port linked a test binary and Go did not")
		return
	case errPort != nil:
		t.Error("Go linked a test binary and the port did not")
		return
	}
	savedCompared.Add(1)
	if d := firstDiff(want, got); d != "" {
		t.Errorf("the generated C of the test binary differs: %s", d)
	}
}

// savedC is the saving clang, written once a run (see savingClangDir).
var savedC struct {
	err  error
	dir  string
	once sync.Once
}

// savingClangDir returns the directory holding the saving clang, a
// shell script named clang, written once a run in the run's temporary
// directory: it copies an argument naming a file program.c, the
// generated C a build links, to $PORTTEST_SAVE_C when that is set, and
// runs the real clang, the one first on the PATH the test inherits, by
// its absolute path, with the arguments it is given. test-examples puts
// it first on PATH for both compilers.
func savingClangDir() (string, error) {
	savedC.once.Do(func() {
		clang, err := exec.LookPath("clang")
		if err == nil {
			clang, err = filepath.Abs(clang)
		}
		if err != nil {
			savedC.err = fmt.Errorf("finding clang for the saving clang: %w", err)
			return
		}
		dir := filepath.Join(env.tmp, "saving-clang")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			savedC.err = fmt.Errorf("writing the saving clang: %w", err)
			return
		}
		script := fmt.Sprintf(`#!/bin/sh
for a; do
    case $a in
    */program.c) [ -z "$%[1]s" ] || cp "$a" "$%[1]s" || exit 125 ;;
    esac
done
exec %[2]q "$@"
`, saveCVar, clang)
		//nolint:gosec // G306: a script the builds run
		if err := os.WriteFile(filepath.Join(dir, "clang"), []byte(script), 0o755); err != nil {
			savedC.err = fmt.Errorf("writing the saving clang: %w", err)
			return
		}
		savedC.dir = dir
	})
	return savedC.dir, savedC.err
}

// saveCVar is the variable naming where the saving clang copies the
// generated C to.
const saveCVar = "PORTTEST_SAVE_C"

// testOnce runs tack test on in with bin, in d's directories, each made
// anew but home, with the saving clang first on PATH, saving the
// generated C to saveC, and returns how it ended, what the copy holds
// afterwards, and saveC as its bin. It fails t when TMPDIR is not empty
// afterwards. A decline is returned as it is, nothing checked.
func testOnce(t *testing.T, bin string, d buildDirs, in input, saveC string) built {
	t.Helper()
	copied, tree, err := prepareTest(d, in)
	if err != nil {
		t.Fatal(err)
	}
	clangDir, err := savingClangDir()
	if err != nil {
		t.Fatal(err)
	}
	copied.env = append(copied.env, "PATH="+clangDir+string(filepath.ListSeparator)+os.Getenv("PATH"),
		saveCVar+"="+saveC)
	run := testRuns[in.name]
	if run.linkHook != "" {
		hookDir, err := linkHookDir(filepath.Dir(saveC), filepath.Base(saveC), clangDir, run.linkHook)
		if err != nil {
			t.Fatal(err)
		}
		copied.env = append(copied.env, "PATH="+hookDir+string(filepath.ListSeparator)+clangDir+
			string(filepath.ListSeparator)+os.Getenv("PATH"))
	}
	copied.env = append(copied.env, run.env...)
	copied.closedStdout = run.closedStdout
	r := runCompiler(t, bin, d.cache, copied, testArgs(in))
	who := "the port"
	if isGo(bin) {
		who = "Go"
	}
	left := entries(t, d.tmp)
	if run.leaves {
		// What the probe's test leaves, which no one may read, is made
		// removable for the next run; that something is left is the
		// point of the probe.
		if len(left) == 0 {
			t.Errorf("%s's tack test left nothing in TMPDIR, which the probe's test must make it leave", who)
		}
		makeRemovable(t, d.tmp)
		if err := fresh(d.tmp); err != nil {
			t.Fatal(err)
		}
	} else if len(left) > 0 {
		t.Errorf("%s's tack test left in TMPDIR: %s", who, strings.Join(left, ", "))
	}
	if _, late := r.lateDecline(); r.earlyDecline() || late {
		return built{res: r, in: copied}
	}
	files, err := snapshot(tree)
	if err != nil {
		t.Fatal(err)
	}
	return built{res: r, files: files, in: copied, bin: saveC}
}

// linkHookDir writes, in a directory beside where the saved C goes, a
// clang that runs the saving clang in clangDir and then, after a link
// of the test binary, a command whose -o ends in /tests, the shell
// commands hook with $o the binary's path, and returns the directory.
func linkHookDir(base, name, clangDir, hook string) (string, error) {
	dir := filepath.Join(base, "hook-"+name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("writing the link hook: %w", err)
	}
	script := fmt.Sprintf(`#!/bin/sh
%q "$@" || exit
o=; prev=
for a; do
    [ "$prev" = -o ] && o=$a
    prev=$a
done
case $o in
*/tests) %s ;;
esac
`, filepath.Join(clangDir, "clang"), hook)
	//nolint:gosec // G306: a script the builds run
	if err := os.WriteFile(filepath.Join(dir, "clang"), []byte(script), 0o755); err != nil {
		return "", fmt.Errorf("writing the link hook: %w", err)
	}
	return dir, nil
}

// prepareModule makes d's directories anew, as prepareBuild does, but
// copies the whole module in's directory is in, the nearest directory at
// or above it holding a tack.mod, below d.work by its path from this
// module's root, or from the module's own root for one outside it, so
// that a case run from a package's directory, or naming another package
// of its module, finds what it needs. It returns in as it runs in the
// copy, with TMPDIR d.tmp and TACK_HOME d.home.
func prepareModule(d buildDirs, in input) (input, error) {
	if err := os.RemoveAll(d.work); err != nil {
		return input{}, fmt.Errorf("emptying %s: %w", d.work, err)
	}
	if err := fresh(d.cache, d.tmp, filepath.Dir(d.out)); err != nil {
		return input{}, err
	}
	scope := in.dir
	for dir := in.dir; ; dir = filepath.Dir(dir) {
		if fileExists(filepath.Join(dir, "tack.mod")) {
			scope = dir
			break
		}
		if dir == env.root || dir == filepath.Dir(dir) {
			break
		}
	}
	base := env.root
	if rel, err := filepath.Rel(env.root, scope); err != nil || strings.HasPrefix(rel, "..") {
		base = scope
	}
	below, err := filepath.Rel(base, scope)
	if err != nil {
		return input{}, fmt.Errorf("copying %s: %w", in.name, err)
	}
	if err := copyTree(scope, filepath.Join(d.work, below)); err != nil {
		return input{}, err
	}
	cwd, err := filepath.Rel(base, in.dir)
	if err != nil {
		return input{}, fmt.Errorf("copying %s: %w", in.name, err)
	}
	copied := in
	copied.dir = filepath.Join(d.work, cwd)
	copied.env = append(slices.Clone(copied.env), "TMPDIR="+d.tmp, "TACK_HOME="+d.home)
	return copied, nil
}

// prepareTest makes d's directories anew for a run of tack test on in,
// as prepareBuild does, and returns in as it runs there and the tree
// whose files are compared afterwards: the copy of in's tree; or, for
// an input in std, which the loader finds std's only at or below
// TACK_HOME's, in as it runs in d's read-only TACK_HOME, and std there.
func prepareTest(d buildDirs, in input) (input, string, error) {
	std := homeStd()
	rel, err := filepath.Rel(std, in.dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		copied, err := prepareModule(d, in)
		return copied, d.work, err
	}
	if err := fresh(d.cache, d.tmp); err != nil {
		return input{}, "", err
	}
	copied := in
	caseStd := filepath.Join(d.home, "src", "std")
	copied.dir = filepath.Join(caseStd, rel)
	copied.env = append(slices.Clone(copied.env), "TMPDIR="+d.tmp, "TACK_HOME="+d.home)
	return copied, caseStd, nil
}

// testProbe is one probe of test-examples: a module named m, its files
// by path, tack.mod aside, written below the run's temporary directory
// and run from its root with args after test and env's variables after
// every other; with linkHook's shell commands run on the test binary,
// $o, once clang has linked it (see linkHookDir); and leaves set when its
// test leaves in TMPDIR what cannot be removed. from, when not "", names
// a directory of testdata/probes whose files the module holds too.
type testProbe struct {
	from     string
	name     string
	files    map[string]string
	args     []string
	env      []string
	linkHook string
	leaves   bool
}

// testingImport is the import of std/testing every test file starts
// with, after its package clause.
const testingImport = "\nimport testing from \"std/testing\"\n"

// passingTest is a test file of package p holding one test that passes.
const passingTest = "package p\n" + testingImport + "\nfunc test_ok(mut t testing.T) error {\n" +
	"    return noerror\n}\n"

// testProbes are what the corpus never runs tack test with: each
// command-line mistake and the help, every way a flag can be written,
// targets that name nothing or a file, a package of test files alone,
// with no Tack file, or with tests declared wrongly, a syntax error, a
// name the generated main declares, an import cycle, a warning, a test
// binary a signal kills or that exits with a status the runner never
// gives, 2000 tests, names and a pattern of odd bytes, the PWD a test
// sees, a test that leaves in its binary's directory what may not be
// read, failing and passing, a binary a hook on clang's link makes one
// exec cannot run, the types of testdata/probes whose tack.h names the
// header ruling changes, a relative TMPDIR, every mistake in the format of T.errorf and T.fatalf, called
// as methods and through testing.T, and formats that are right, a main
// package with no main function, and a package whose C uses a type of
// the package, which the Go compiler names wrongly in tack.h.
var testProbes = []testProbe{
	{name: "usage-two-targets", args: []string{"p", "q"}, files: map[string]string{"p/p_test.tck": passingTest}},
	{name: "usage-three-opts", args: []string{"-O0", "-O2", "-Os", "p"}},
	{name: "usage-two-opts", args: []string{"-O1", "-O1"}},
	{name: "usage-unknown-flag", args: []string{"--out", "x"}},
	{name: "usage-empty-target", args: []string{""}},
	{name: "usage-empty-and-more", args: []string{"", " ", "a\tb"}},
	{name: "usage-file", args: []string{"p/p_test.tck"}, files: map[string]string{"p/p_test.tck": passingTest}},
	{name: "usage-run-no-value", args: []string{"-run"}},
	{name: "usage-bad-bool", args: []string{"-v=x"}},
	{name: "usage-bad-opt-bool", args: []string{"-O2=yes"}},
	{name: "usage-bad-syntax", args: []string{"---v"}},
	{name: "usage-bad-syntax-equals", args: []string{"-=v"}},
	{name: "usage-bad-syntax-dashes", args: []string{"--=", "p"}},
	{name: "help-h", args: []string{"-h"}},
	{name: "help-help", args: []string{"--help"}},
	{name: "help-single-dash", args: []string{"-help", "p", "q"}},
	{name: "help-with-value", args: []string{"-h=1"}},
	{name: "help-after-mistake", args: []string{"-O1", "-O2", "-h"}},
	{name: "flags-equals", args: []string{"-run=ok", "-v=true", "-O2=false", "--", "p"},
		files: map[string]string{"p/p_test.tck": passingTest}},
	{name: "flags-double-dash", args: []string{"--run", "nothing", "--v", "p"},
		files: map[string]string{"p/p_test.tck": passingTest}},
	{name: "flags-last-wins", args: []string{"-run", "nothing", "-run", "ok", "-v=false", "p"},
		files: map[string]string{"p/p_test.tck": passingTest}},
	{name: "flags-after-target", args: []string{"p", "-v"}, files: map[string]string{"p/p_test.tck": passingTest}},
	{name: "target-default", args: nil, files: map[string]string{"p_test.tck": strings.Replace(passingTest,
		"package p", "package m", 1)}},
	{name: "target-dash", args: []string{"-"}},
	{name: "target-import-path", args: []string{"m/p"}, files: map[string]string{"p/p_test.tck": passingTest}},
	{name: "target-missing-path", args: []string{"m/nowhere"}},
	{name: "target-missing-dir", args: []string{"./nowhere"}},
	{name: "target-other-module", args: []string{"other/p"}},
	{name: "target-absolute-missing", args: []string{"/nonexistent/tack-test-probe"}},
	{name: "target-no-tack-files", args: []string{"p"}, files: map[string]string{"p/p.c": "int p_x;\n"}},
	{name: "target-empty-dir", args: []string{"p"}, files: map[string]string{"p/sub/keep.txt": "\n"}},
	{name: "only-tests", args: []string{"-v", "p"}, files: map[string]string{
		"p/a_test.tck": passingTest,
		"p/b_test.tck": "package p\n" + testingImport + "\nfunc test_b(mut t testing.T) error {\n" +
			"    mut t.errorf(\"b %d\", 2)\n    return noerror\n}\n",
	}},
	{name: "files-in-name-order", args: []string{"-v", "p"}, files: map[string]string{
		"p/z.tck":       "package p\n\npub func z() int64 {\n    return 26\n}\n",
		"p/b_test.tck":  "package p\n" + testingImport + "\nfunc test_b2(mut t testing.T) error {\n    return noerror\n}\n\nfunc test_b1(mut t testing.T) error {\n    return noerror\n}\n",
		"p/A_test.tck":  "package p\n" + testingImport + "\nfunc test_a(mut t testing.T) error {\n    if z() != 26 {\n        mut t.errorf(\"z\")\n    }\n    return noerror\n}\n",
		"p/.h_test.tck": "@ never read\n",
		"p/_test.tck":   "package p\n" + testingImport + "\nfunc test_under(mut t testing.T) error {\n    return noerror\n}\n",
		"p/x_TEST.tck":  "package p\n\nfunc test_not_a_test() {\n}\n",
	}},
	{name: "no-tests-run-pattern", args: []string{"-run", "x", "p"}, files: map[string]string{
		"p/p.tck": "package p\n\npub func one() int64 {\n    return 1\n}\n",
	}},
	{name: "run-matches-none", args: []string{"-run", "OK", "p"}, files: map[string]string{"p/p_test.tck": passingTest}},
	{name: "shape-errors", args: []string{"p"}, files: map[string]string{
		"p/p.tck": "package p\n\n// test_plain is no test.\npub func test_plain(n int64) int64 {\n    return n\n}\n",
		"p/p_test.tck": "package p\n" + testingImport + "\nfunc test_no_params() error {\n    return noerror\n}\n\n" +
			"func test_not_mut(t testing.T) error {\n    return noerror\n}\n\n" +
			"func test_no_result(mut t testing.T) {\n}\n\n" +
			"func test_two(mut t testing.T, n int64) error {\n    return noerror\n}\n\n" +
			"func test_generic[T any](mut t testing.T) error {\n    return noerror\n}\n\n" +
			"func test_own(own t testing.T) error {\n    return noerror\n}\n\n" +
			"func test_int(mut t testing.T) int64 {\n    return 0\n}\n\n" +
			"extern(\"p_c\") func test_extern(mut t testing.T) error\n\n" +
			"type S struct {\n    n int64\n}\n\n" +
			"func (s S) test_method(mut t testing.T) error {\n    return noerror\n}\n\n" +
			"func test_ok(mut t testing.T) error {\n    return noerror\n}\n",
	}},
	{name: "shape-unknown-type", args: []string{"p"}, files: map[string]string{
		"p/p_test.tck": "package p\n\nfunc test_x(mut t testing.T) error {\n    return noerror\n}\n",
	}},
	{name: "syntax-error", args: []string{"p"}, files: map[string]string{
		"p/p.tck":      "package p\n\npub func one() int64 {\n    return 1\n}\n",
		"p/p_test.tck": "package p\n" + testingImport + "\nfunc test_x(mut t testing.T) error {\n    return\n",
	}},
	{name: "lex-error", args: []string{"p"}, files: map[string]string{
		"p/p_test.tck": "package p\n\n@\n",
	}},
	{name: "clause-mismatch", args: []string{"p"}, files: map[string]string{
		"p/p.tck":      "package p\n\npub func one() int64 {\n    return 1\n}\n",
		"p/p_test.tck": "package q\n" + testingImport + "\nfunc test_x(mut t testing.T) error {\n    return noerror\n}\n",
	}},
	{name: "clash-func", args: []string{"p"}, files: map[string]string{
		"p/p.tck": "package p\n\npub func tack_test_main_() {\n}\n",
	}},
	{name: "clash-all", args: []string{"p"}, files: map[string]string{
		"p/p.tck": "package p\n\nimport tack_test_names_ from \"std/fmt\"\nimport tack_testing_ from \"std/os\"\n\n" +
			"var tack_testing_ int64 = 1\n\ntype tack_test_main_ struct {\n    n int64\n}\n",
		"p/p_test.tck": "package p\n\nimport tack_test_main_ from \"std/testing\"\n",
	}},
	{name: "import-cycle", args: []string{"./a"}, files: map[string]string{
		"a/a.tck": "package a\n\npub func one() int64 {\n    return 1\n}\n",
		"a/a_test.tck": "package a\n\nimport b from \"m/b\"\n" + testingImport[1:] +
			"\nfunc test_b(mut t testing.T) error {\n    if b.two() != 2 {\n        mut t.errorf(\"no\")\n" +
			"    }\n    return noerror\n}\n",
		"b/b.tck": "package b\n\nimport a from \"m/a\"\n\npub func two() int64 {\n    return a.one() + 1\n}\n",
	}},
	{name: "warning", args: []string{"-v", "p"}, files: map[string]string{
		"p/p_test.tck": "package p\n" + testingImport + "\nfunc test_x(mut t testing.T) error {\n" +
			"    n := 1\n    return noerror\n}\n",
	}},
	{name: "signal-term", args: []string{"-v", "p"}, files: signalProbe("SIGTERM")},
	{name: "signal-kill", args: []string{"p"}, files: signalProbe("SIGKILL")},
	{name: "signal-segv", args: []string{"p"}, files: signalProbe("SIGSEGV")},
	{name: "exit-status-7", args: []string{"-v", "p"}, files: map[string]string{
		"p/p.c": "#include <unistd.h>\n\n#include \"tack.h\"\n\nvoid p_quit(int32_t code) {\n    _exit(code);\n}\n",
		"p/p_test.tck": "package p\n" + testingImport + "\nextern(\"p_quit\") func quit(code int32)\n\n" +
			"func test_first(mut t testing.T) error {\n    return noerror\n}\n\n" +
			"func test_quit(mut t testing.T) error {\n    quit(7)\n    return noerror\n}\n",
	}},
	{name: "many-tests", args: []string{"-run", "test_19", "p"}, files: map[string]string{"p/p_test.tck": manyTests(2000)}},
	{name: "many-tests-all", args: []string{"p"}, files: map[string]string{"p/p_test.tck": manyTests(2000)}},
	{name: "odd-bytes", args: []string{"p"}, files: map[string]string{
		"p/odd \"q\" \\ \xc3\xa9\x01_test.tck": "package p\n" + testingImport +
			"\nfunc test_odd(mut t testing.T) error {\n    mut t.errorf(\"in %s\", \"odd\")\n" +
			"    return noerror\n}\n",
	}},
	{name: "odd-pattern", args: []string{"-run", "\"\\\xff", "p"}, files: map[string]string{"p/p_test.tck": passingTest}},
	{name: "relative-tmpdir", args: []string{"p"}, env: []string{"TMPDIR=tmp"},
		files: map[string]string{"p/p_test.tck": passingTest, "tmp/keep": "\n"}},
	{name: "format-errors", args: []string{"p"}, files: map[string]string{
		"p/p_test.tck": "package p\n" + testingImport + "\nfunc test_formats(mut t testing.T) error {\n" +
			"    mut t.errorf(\"%d\", \"s\")\n" +
			"    mut t.errorf(\"%d %s\", 1)\n" +
			"    mut t.errorf(\"%d\", 1, 2, 3)\n" +
			"    mut t.errorf(\"%q and %\", 1)\n" +
			"    mut t.errorf(\"%t\", 1)\n" +
			"    return mut t.fatalf(\"%c %v\", \"x\")\n}\n",
	}},
	{name: "format-method-expr", args: []string{"p"}, files: map[string]string{
		"p/p_test.tck": "package p\n" + testingImport + "\nfunc pass(mut t testing.T, args ...any) {\n" +
			"    mut t.errorf(\"%d %z %s\", args...)\n}\n\n" +
			"func test_spread(mut t testing.T) error {\n" +
			"    pass(mut t, 1, \"a\")\n" +
			"    testing.T.errorf(mut t, \"%s\", 1)\n" +
			"    testing.T.errorf(mut t, \"%d\", 1, 2)\n" +
			"    testing.T.errorf(mut t, \"%d %d\", 1)\n" +
			"    return testing.T.fatalf(mut t, \"%\")\n}\n",
	}},
	{name: "format-ok", args: []string{"-v", "p"}, files: map[string]string{
		"p/p_test.tck": "package p\n" + testingImport + "\nfunc test_formats(mut t testing.T) error {\n" +
			"    mut t.errorf(\"%d %s %t %c %v %% %x\", 1, \"s\", true, 'c', 2.5, 255)\n" +
			"    testing.T.errorf(mut t, \"%s\", \"through the type\")\n" +
			"    f := \"%d\"\n" +
			"    mut t.errorf(f, \"not checked\")\n" +
			"    return noerror\n}\n",
	}},
	{name: "pwd", args: []string{"-v", "./pwd"}, files: map[string]string{
		"pwd/pwd_test.tck": "package pwd\n\nimport fmt from \"std/fmt\"\nimport os from \"std/os\"\n" + testingImport[1:] +
			"\nfunc test_pwd(mut t testing.T) error {\n" +
			"    v, set := os.env([]uint8(\"PWD\"), c_allocator)?\n" +
			"    if !set {\n        mut t.errorf(\"no PWD\")\n    }\n" +
			"    fmt.printf(\"PWD %s\\n\", v.bytes())\n" +
			"    return noerror\n}\n",
	}},
	{name: "temp-not-removable", args: []string{"p"}, leaves: true, files: map[string]string{"p/p_test.tck": lockTest(true)}},
	{name: "temp-not-removable-pass", args: []string{"p"}, leaves: true,
		files: map[string]string{"p/p_test.tck": lockTest(false)}},
	{name: "temp-deep-tree", args: []string{"-v", "p"}, files: map[string]string{"p/p_test.tck": passingTest},
		linkHook: `cd "${o%/*}" && i=0 && while [ $i -lt 25 ]; do ` +
			`n=dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd` +
			`dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd; ` +
			`mkdir "$n" && cd "$n" && : > f || exit; i=$((i+1)); done`},
	{name: "link-fails-not-removable", args: []string{"p"}, leaves: true,
		files:    map[string]string{"p/p_test.tck": passingTest},
		linkHook: `d="${o%/*}" && mkdir "$d/sub" && : > "$d/sub/f" && chmod 300 "$d/sub" && exit 1`},
	{name: "binary-exec-format", args: []string{"p"}, linkHook: `printf 'not a program\n' > "$o"`,
		files: map[string]string{"p/p_test.tck": passingTest}},
	{name: "binary-not-executable", args: []string{"p"}, linkHook: `chmod 644 "$o"`,
		files: map[string]string{"p/p_test.tck": passingTest}},
	{name: "header-keyword-type", args: []string{"-v", "./p"}, from: "header-keyword-type"},
	{name: "header-prefix-reverse", args: []string{"-v", "./p"}, from: "header-prefix-reverse"},
	{name: "header-two-util", args: []string{"-v", "./a/util"}, from: "header-two-util"},
	{name: "main-no-main", args: []string{"p"}, files: map[string]string{
		"p/p_test.tck": strings.Replace(passingTest, "package p", "package main", 1),
	}},
	{name: "main-with-c-type", args: []string{"-v", "p"}, files: cTypeProbe("main", "Info")},
	{name: "c-uses-package-type", args: []string{"-v", "p"}, files: cTypeProbe("p", "p_Info")},
}

// lockTest returns a test file of package p whose test makes, in the
// directory of its binary, os.args' first, a directory no one may read,
// mode 0o300, holding a file, so the binary's temporary directory cannot
// be removed, and then fails, when fails is set, or passes.
func lockTest(fails bool) string {
	fail := ""
	if fails {
		fail = "    mut t.errorf(\"locked\")\n"
	}
	return "package p\n\nimport os from \"std/os\"\nimport path from \"std/path\"\n" + testingImport[1:] +
		"\nfunc test_lock(mut t testing.T) error {\n" +
		"    a := os.args(c_allocator)?\n" +
		"    mut dir := path.dir([]uint8(\".\"), c_allocator)?\n" +
		"    for i, x := range a {\n        if i == 0 {\n            dir = path.dir(x.bytes(), c_allocator)?\n        }\n    }\n" +
		"    sub := path.join(dir.bytes(), []uint8(\"sub\"), c_allocator)?\n" +
		"    os.make_dir_mode(sub.bytes(), 0o300)?\n" +
		"    f := path.join(sub.bytes(), []uint8(\"f\"), c_allocator)?\n" +
		"    os.write_file(f.bytes(), []uint8(\"x\"))?\n" + fail +
		"    return noerror\n}\n"
}

// signalProbe returns the files of a package whose second test sends
// itself sig through C, ending the test binary, a way no valid Tack
// program ends but one code behind an extern func may.
func signalProbe(sig string) map[string]string {
	return map[string]string{
		"p/p.c": "#include <signal.h>\n\n#include \"tack.h\"\n\nvoid p_die(void) {\n    raise(" + sig + ");\n}\n",
		"p/p_test.tck": "package p\n" + testingImport + "\nextern(\"p_die\") func die()\n\n" +
			"func test_first(mut t testing.T) error {\n    return noerror\n}\n\n" +
			"func test_die(mut t testing.T) error {\n    die()\n    return noerror\n}\n",
	}
}

// cTypeProbe returns the files of package pkg, in directory p, whose C
// file fills in a struct of the package, Info, which the C names cname,
// the name tack.h must give it: Info in package main, p_Info in p.
func cTypeProbe(pkg, cname string) map[string]string {
	pub := "pub "
	if pkg == "main" {
		pub = ""
	}
	files := map[string]string{
		"p/p.tck": "package " + pkg + "\n\n// Info is filled in by C.\n" + pub + "type Info struct {\n    " + pub + "n int64\n}\n\n" +
			"// fill sets i's n, in C.\nextern(\"p_fill\") func fill(mut i Info)\n\n" +
			"// filled returns an Info C filled in.\n" + pub + "func filled() int64 {\n    mut i := Info{n: 0}\n" +
			"    fill(mut i)\n    return i.n\n}\n",
		"p/p.c": "#include \"tack.h\"\n\nvoid p_fill(" + cname + " *i) {\n    i->n = 7;\n}\n",
		"p/p_test.tck": "package " + pkg + "\n" + testingImport + "\nfunc test_fill(mut t testing.T) error {\n" +
			"    if filled() != 7 {\n        mut t.errorf(\"filled %d\", filled())\n    }\n    return noerror\n}\n",
	}
	if pkg == "main" {
		files["p/main.tck"] = "package main\n\nfunc main() {\n    print(\"never runs\\n\")\n}\n"
	}
	return files
}

// manyTests returns a test file of package p declaring n tests, the last
// of which fails.
func manyTests(n int) string {
	var b strings.Builder
	b.WriteString("package p\n" + testingImport)
	for i := range n {
		fmt.Fprintf(&b, "\nfunc test_%04d(mut t testing.T) error {\n", i)
		if i == n-1 {
			fmt.Fprintf(&b, "    mut t.errorf(\"last of %%d\", %d)\n", n)
		}
		b.WriteString("    return noerror\n}\n")
	}
	return b.String()
}

// TestTestInputs checks test-examples' inputs without running a
// compiler: every corpus directory exists, as corpusDir finds it, and
// every name is unique. Run without TACK_PORT_TEST, with no env.home,
// it finds std's below this module's frozen src/std, whose files
// TestPortFiles keeps the port's, path for path.
func TestTestInputs(t *testing.T) {
	for _, d := range testCorpusDirs {
		dir := corpusDir(d)
		if env.home == "" {
			dir = filepath.Join(env.root, filepath.FromSlash(d))
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Errorf("corpus directory %s: %v", d, err)
		}
	}
	seen := map[string]bool{}
	for _, p := range testProbes {
		if seen[p.name] {
			t.Errorf("probe %s twice", p.name)
		}
		seen[p.name] = true
	}
}
