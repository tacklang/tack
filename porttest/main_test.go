package porttest

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// env is what TestMain sets up for TestPort: the two compilers, the
// directories they run in, and why TestPort cannot run, if it cannot.
// TestMain writes it before any test runs and nothing writes it after.
var env struct {
	// root is this module's root, absolute.
	root string
	// tmp is the run's temporary directory, which TestMain removes, for
	// what the tests write once a run, such as the instrumented Go
	// compiler and the harvested programs.
	tmp string
	// goBin is the Go compiler's binary.
	goBin string
	// portBin is the port's binary.
	portBin string
	// portRun is what TestPort runs as the port: a wrapper around portBin
	// that holds its stack to deepStack KB (see stackWrapper).
	portRun string
	// portPairs is portRun with its stack held to pairStack KB, which
	// TestCheckDeepPairs runs as well.
	portPairs string
	// portSmall is portRun with its stack held to smallStack KB, which
	// TestCheckSliceChains runs as well.
	portSmall string
	// goCache is the TACK_CACHE the Go compiler runs with, the port's
	// build among its runs.
	goCache string
	// portCache is the TACK_CACHE the port runs with.
	portCache string
	// skip, when not empty, is why TestPort skips: it was not asked for.
	skip string
	// setupErr, when not nil, is why TestPort fails: it was asked for
	// without -count, there is no port, a binary could not be built, or
	// the corpus could not be written.
	setupErr error
	// required are the stages a not implemented answer fails, from
	// required.txt.
	required map[string]bool
	// archives are the txtar archives' cases, written to disk.
	archives []archive
}

// gateVar is the environment variable that asks for TestPort; it runs
// only when its value is gateOn.
const (
	gateVar = "TACK_PORT_TEST"
	gateOn  = "1"
)

// TestMain sets TestPort up, when it is asked for, once for every test
// of the run: it builds the binaries and writes the corpus in a
// temporary directory it removes afterwards. The harness's own tests
// need none of it and run either way. With measureVar set, the binary is
// TestResPerf's launcher instead, and with groupHelperVar the harness
// the group tests stop, and runs no test. Otherwise it first makes a
// signal that stops the harness kill the process groups it runs (see
// handleSignals).
func TestMain(m *testing.M) {
	if cmdline := os.Getenv(measureVar); cmdline != "" {
		os.Exit(launchMeasured(cmdline))
	}
	if script := os.Getenv(groupHelperVar); script != "" {
		os.Exit(groupHelper(script))
	}
	handleSignals()
	flag.Parse()
	root, err := filepath.Abs("..")
	if err != nil {
		fmt.Fprintln(os.Stderr, "porttest: finding the module root:", err)
		os.Exit(1)
	}
	env.root = root
	switch {
	case os.Getenv(gateVar) != gateOn:
		env.skip = "the port is checked only with " + gateVar + "=" + gateOn +
			" and -count=1, as make port-test runs it"
		os.Exit(m.Run())
	case !countGiven(flag.CommandLine):
		env.setupErr = errors.New(gateVar + "=" + gateOn + " needs -count=1: " +
			"go test would otherwise replay a cached result that does not see " +
			"a change to the port or to the Go compiler")
		os.Exit(m.Run())
	}
	tmp, err := os.MkdirTemp("", "porttest")
	if err != nil {
		fmt.Fprintln(os.Stderr, "porttest:", err)
		os.Exit(1)
	}
	env.setupErr = setup(tmp)
	code := m.Run()
	if err := os.RemoveAll(tmp); err != nil {
		fmt.Fprintln(os.Stderr, "porttest:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

// countGiven reports whether fs's command line set -count, as go test
// passes it on, -test.count. go test never caches a run given -count.
func countGiven(fs *flag.FlagSet) bool {
	given := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "test.count" {
			given = true
		}
	})
	return given
}

// setup fills env, putting what it makes in tmp. It returns why TestPort
// cannot run, or nil.
func setup(tmp string) error {
	var err error
	env.tmp = tmp
	env.goCache = filepath.Join(tmp, "cache-go")
	env.portCache = filepath.Join(tmp, "cache-port")
	if env.required, err = readRequired("required.txt"); err != nil {
		return err
	}
	scale, err := scaleFrom(timeoutScaleVar)
	if err != nil {
		return err
	}
	compilerTimeout = baseCompilerTimeout * time.Duration(scale)
	if env.goBin, err = goCompiler(tmp); err != nil {
		return err
	}
	if env.portBin, err = portCompiler(tmp); err != nil {
		return err
	}
	if env.portRun, err = writeStackWrapper(tmp, "port-stack", deepStack); err != nil {
		return err
	}
	if env.portPairs, err = writeStackWrapper(tmp, "port-pairs", pairStack); err != nil {
		return err
	}
	if env.portSmall, err = writeStackWrapper(tmp, "port-small", smallStack); err != nil {
		return err
	}
	env.archives, err = writeArchives(filepath.Join(tmp, "corpus"))
	return err
}

// fromRoot returns p, a path from an environment variable, made absolute:
// a relative one is taken from the module's root, since go test runs the
// tests in porttest/ and cannot know where it was run from.
func fromRoot(p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(env.root, p)
}

// goCompiler returns $TACK_GO, or, when it is unset or empty, the path of
// ./tack built into tmp.
func goCompiler(tmp string) (string, error) {
	if bin := os.Getenv("TACK_GO"); bin != "" {
		return fromRoot(bin), nil
	}
	bin := filepath.Join(tmp, "tack-go")
	cmd := exec.Command("go", "build", "-o", bin, "./tack")
	cmd.Dir = env.root
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("building the Go compiler: %w\n%s", err, out)
	}
	return bin, nil
}

// portCompiler returns $TACK_PORT_BIN, or, when it is unset or empty, the
// path of the port's driver built into tmp with the Go compiler. A source
// tree with no driver directory is an error, whether $TACK_PORT names it
// or it is the default.
func portCompiler(tmp string) (string, error) {
	if given := os.Getenv("TACK_PORT_BIN"); given != "" {
		return fromRoot(given), nil
	}
	src, explicit := portSource(os.Getenv("TACK_PORT"))
	driver := filepath.Join(src, "driver")
	info, err := os.Stat(driver)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "", missingPort(src, explicit)
	case err != nil:
		return "", fmt.Errorf("finding the port's driver: %w", err)
	case !info.IsDir():
		return "", fmt.Errorf("the port's driver, %s, is not a directory", driver)
	}
	bin := filepath.Join(tmp, "tack-port")
	cmd := exec.Command(env.goBin, "--out", bin, "./driver")
	cmd.Dir = src
	cmd.Env = compilerEnv(env.goCache)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("building the port in %s: %w\n%s", src, err, out)
	}
	return bin, nil
}

// portSource returns the port's source tree for tackPort, the value of
// $TACK_PORT, made absolute by fromRoot, and whether tackPort named it:
// an empty tackPort stands for the default, ../tack from the module's
// root.
func portSource(tackPort string) (src string, explicit bool) {
	if tackPort == "" {
		return fromRoot(filepath.Join("..", "tack")), false
	}
	return fromRoot(tackPort), true
}

// missingPort returns the error for src, the port's source tree, having
// no driver directory, saying whether $TACK_PORT named src, explicit, or
// it is the default.
func missingPort(src string, explicit bool) error {
	if explicit {
		return fmt.Errorf("TACK_PORT is %s, which has no driver directory", src)
	}
	return fmt.Errorf("no port to test: %s, the default, has no driver directory "+
		"(set TACK_PORT to the port's source tree or TACK_PORT_BIN to its binary)", src)
}

// compilerEnv returns the environment a compiler runs in: this
// process's, with TACK_HOME set to the module's root and TACK_CACHE to
// cache.
func compilerEnv(cache string) []string {
	return append(os.Environ(), "TACK_HOME="+env.root, "TACK_CACHE="+cache)
}

// requirableChecks are the names, besides the stages', that required.txt
// may hold: checks outside TestPort that a port can answer not
// implemented to, which then fail rather than skip once required.
// loaded is TestLoadOrder's, types-decls and types-bodies TestTypes'.
// The emit stages, emit-E0 to emit-E4 and emit-H, are added by init (see
// emitStageNames): a case of a stage that classifies its cases is
// required once every one it needs is named. build-identical, the check
// that the two compilers build the same binary for a case of
// build-examples, is added by init too (see buildIdenticalCheck).
var requirableChecks = map[string]bool{loadedCheck: true, typesCheck: true, bodiesCheck: true}

// readRequired reads the stage names in the file at path, one a line,
// skipping blank lines and lines starting with #. A name that is no stage
// is an error, so a typo cannot leave a stage unrequired.
func readRequired(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: a fixed file of this package
	if err != nil {
		return nil, fmt.Errorf("reading the required stages: %w", err)
	}
	required := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		name := string(bytes.TrimSpace(sc.Bytes()))
		if name == "" || name[0] == '#' {
			continue
		}
		if stageNamed(name) == nil && !requirableChecks[name] {
			return nil, fmt.Errorf("%s:%d: %q is no stage", path, n, name)
		}
		required[name] = true
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return required, nil
}

// ready skips or fails t, a test or a fuzz target, when TestMain found
// the tests cannot run.
func ready(t testing.TB) {
	t.Helper()
	if env.setupErr != nil {
		t.Fatal(env.setupErr)
	}
	if env.skip != "" {
		t.Skip(env.skip)
	}
}
