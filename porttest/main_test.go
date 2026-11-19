package porttest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/loader"
)

// env is what TestMain sets up for TestPort: the two compilers, the
// directories they run in, and why TestPort cannot run, if it cannot.
// TestMain writes it before any test runs and nothing writes it after.
var env struct {
	// root is this module's root, absolute.
	root string
	// home is the TACK_HOME every comparison runs both compilers with,
	// the port's source tree, absolute, whose src/std is the std they
	// read: the port owns std and the runtime (per the owner's ruling,
	// 2026-11-18), so both compilers are compared on the port's.
	home string
	// tmp is the run's temporary directory, which TestMain removes, for
	// what the tests write once a run, such as the instrumented Go
	// compiler and the harvested programs.
	tmp string
	// goBin is the Go compiler's binary, the reference: this module's
	// compiler embedding the port's runtime (see buildReference).
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
// TestResPerf's launcher instead, with groupHelperVar the harness the
// group tests stop, and with buildGoVar it builds the reference Go
// compiler for the Makefile (see buildGoMain); each runs no test.
// Otherwise it first makes a
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
	if out := os.Getenv(buildGoVar); out != "" {
		os.Exit(buildGoMain(out))
	}
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
	src, explicit := portSource(os.Getenv("TACK_PORT"))
	if !dirExists(filepath.Join(src, "src", "std")) {
		return missingStd(src, explicit)
	}
	env.home = src
	if env.goBin, err = goCompiler(tmp, src); err != nil {
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
	if err := writeScripts(); err != nil {
		return err
	}
	env.archives, err = writeArchives(filepath.Join(tmp, "corpus"))
	return err
}

// writeScripts writes, before any test runs, the scripts the tests run
// that are written once a run: the Go compiler's stack wrapper, the
// counting clang and the saving clang. Written lazily, as a case first
// needed one, a script could be written while parallel cases fork, and a
// child forked then holds it open for writing until it execs, so that
// running the script fails with ETXTBSY, text file busy. A clang that
// cannot be found is no failure here: the clang scripts keep the error,
// and the tests that need them report it, as before.
func writeScripts() error {
	if _, err := goStackWrapper(); err != nil {
		return err
	}
	_, _ = countingClangDir()
	_, _ = savingClangDir()
	return nil
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

// goCompiler returns $TACK_GO, which must name a reference compiler, one
// built as buildReference builds it from src, the port's source tree,
// and is checked to be one (see checkReference), or, when it is unset or
// empty, the path of the reference built into tmp from src.
func goCompiler(tmp, src string) (string, error) {
	if bin := os.Getenv("TACK_GO"); bin != "" {
		bin = fromRoot(bin)
		return bin, checkReference(bin, src)
	}
	bin := filepath.Join(tmp, "tack-go")
	return bin, buildReference(bin, env.root, src)
}

// buildGoVar is the environment variable that makes TestMain build the
// reference Go compiler at the path it names, a relative one taken from
// the module's root, and exit, as the Makefile's port targets run it
// (see buildGoMain).
const buildGoVar = "PORTTEST_BUILD_GO"

// buildGoMain builds the reference Go compiler at out, made absolute by
// fromRoot, from the port's source tree $TACK_PORT names, ../tack by
// default, and returns the exit status: 0, or 1 with the error on
// standard error.
func buildGoMain(out string) int {
	src, explicit := portSource(os.Getenv("TACK_PORT"))
	if !dirExists(filepath.Join(src, portRuntimeDir)) {
		fmt.Fprintln(os.Stderr, "porttest:", missingRuntime(src, explicit))
		return 1
	}
	if err := buildReference(fromRoot(out), env.root, src); err != nil {
		fmt.Fprintln(os.Stderr, "porttest:", err)
		return 1
	}
	return 0
}

// buildReference builds the reference Go compiler at out: ./tack of the
// module at root, whose Go code is the frozen compiler's, with the files
// its package builtin embeds, builtin.tck, the runtime header and the
// runtime's C files, replaced by the port's own in src's runtime
// directory, through go build's -overlay (see referenceOverlay). The
// port owns the runtime (per the owner's ruling, 2026-11-18), and a
// program either compiler builds links it, so the comparisons need a Go
// compiler that compiles the port's: one whose binaries, cache keys and
// clang runs are the port's when the port is right. The overlay is
// written beside out and removed once the build ends.
func buildReference(out, root, src string) error {
	overlay, err := referenceOverlay(root, src)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return fmt.Errorf("building the reference Go compiler: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(out), "overlay-*.json")
	if err != nil {
		return fmt.Errorf("building the reference Go compiler: %w", err)
	}
	_, writeErr := f.Write(overlay)
	if err = errors.Join(writeErr, f.Close()); err == nil {
		cmd := exec.Command("go", "build", "-overlay", f.Name(), "-o", out, "./tack")
		cmd.Dir = root
		if output, runErr := cmd.CombinedOutput(); runErr != nil {
			err = fmt.Errorf("building the reference Go compiler: %w\n%s", runErr, output)
		}
	}
	return errors.Join(err, os.Remove(f.Name()))
}

// portEmbedded returns the paths, relative to src's runtime directory
// and slash-separated, of the port's files the reference embeds:
// builtin.tck, tack/tack.h and every *.c directly in it, sorted. Those
// are all builtin/'s go:embed patterns name, so any other regular file
// below the runtime directory, but those portOnlyFiles lists, is an
// error: the reference would not carry it, and the comparisons would
// never see it. builtin.tck and tack/tack.h must be there.
func portEmbedded(src string) ([]string, error) {
	all, err := treeFiles(src, portRuntimeDir)
	if err != nil {
		return nil, err
	}
	var out []string
	found := map[string]bool{}
	for _, p := range all {
		rel := strings.TrimPrefix(p, portRuntimeDir+"/")
		switch {
		case portOnlyFiles[p] != "":
		case rel == "builtin.tck" || rel == "tack/tack.h" ||
			!strings.Contains(rel, "/") && path.Ext(rel) == ".c":
			out = append(out, rel)
			found[rel] = true
		default:
			return nil, fmt.Errorf("the port's runtime holds %s, which the reference "+
				"would not embed: only builtin.tck, tack/tack.h and *.c are", p)
		}
	}
	for _, need := range []string{"builtin.tck", "tack/tack.h"} {
		if !found[need] {
			return nil, fmt.Errorf("the port's runtime has no %s/%s as a regular file",
				portRuntimeDir, need)
		}
	}
	sort.Strings(out)
	return out, nil
}

// checkReference returns an error unless bin, the binary $TACK_GO
// names, holds every file portEmbedded lists for src byte for byte, as a
// reference built from src embeds them, naming the first it lacks. A
// frozen build of this module lacks the port's tack.h and process.c.
func checkReference(bin, src string) error {
	files, err := portEmbedded(src)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(bin) //nolint:gosec // G304: the compiler TACK_GO names
	if err != nil {
		return fmt.Errorf("reading TACK_GO: %w", err)
	}
	for _, rel := range files {
		want, err := os.ReadFile(filepath.Join(src, portRuntimeDir, filepath.FromSlash(rel)))
		if err != nil {
			return fmt.Errorf("reading the port's runtime: %w", err)
		}
		if !bytes.Contains(data, want) {
			return fmt.Errorf("TACK_GO, %s, does not hold the port's %s/%s byte for byte: "+
				"it is not a reference built from this port (build one with make port-go-bin)",
				bin, portRuntimeDir, rel)
		}
	}
	return nil
}

// referenceOverlay returns go build's -overlay file, as JSON, that makes
// root's builtin/ embed src's runtime: builtin/builtin.tck is the port's
// runtime/builtin.tck, builtin/runtime/tack/tack.h its runtime/tack/tack.h,
// and builtin/runtime/<name>.c its runtime/<name>.c for every C file
// either has, one root has and src has not deleted, as portEmbedded
// lists the port's files, failing as it fails.
func referenceOverlay(root, src string) ([]byte, error) {
	portDir := filepath.Join(src, portRuntimeDir)
	frozenDir := filepath.Join(root, "builtin", "runtime")
	files, err := portEmbedded(src)
	if err != nil {
		return nil, err
	}
	replace := map[string]string{}
	for _, rel := range files {
		frozen := filepath.Join(frozenDir, filepath.FromSlash(rel))
		if rel == "builtin.tck" {
			frozen = filepath.Join(root, "builtin", "builtin.tck")
		}
		replace[frozen] = filepath.Join(portDir, filepath.FromSlash(rel))
	}
	cs, err := filepath.Glob(filepath.Join(frozenDir, "*.c"))
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", frozenDir, err)
	}
	for _, c := range cs {
		if _, ok := replace[c]; !ok {
			replace[c] = ""
		}
	}
	data, err := json.Marshal(struct{ Replace map[string]string }{replace})
	if err != nil {
		return nil, fmt.Errorf("writing the reference's overlay: %w", err)
	}
	return data, nil
}

// frozenGo returns this module's compiler as it is, the frozen Go
// compiler with its own runtime, built into the run's temporary
// directory the first time it is called. Only the checks of where the
// port's runtime differs from it on purpose run it (see TestFileSizeLimit);
// $TACK_GO, the reference, never stands for it.
var frozenGo = sync.OnceValues(func() (string, error) {
	if env.tmp == "" {
		return "", errors.New("the frozen Go compiler is built only as TestPort runs, with " +
			gateVar + "=" + gateOn)
	}
	bin := filepath.Join(env.tmp, "tack-frozen")
	cmd := exec.Command("go", "build", "-o", bin, "./tack")
	cmd.Dir = env.root
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("building the frozen Go compiler: %w\n%s", err, out)
	}
	return bin, nil
})

// portCompiler returns $TACK_PORT_BIN, or, when it is unset or empty, the
// path of the port's driver built into tmp with the Go compiler, with
// the port's own std (see portBuildEnv). A source
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
	cmd.Env = portBuildEnv(src, env.goCache)
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

// missingStd returns the error for src, the port's source tree, having
// no src/std, which every comparison reads std from, saying whether
// $TACK_PORT named src, explicit, or it is the default.
func missingStd(src string, explicit bool) error {
	if explicit {
		return fmt.Errorf("TACK_PORT is %s, which has no src/std for the comparisons to read", src)
	}
	return fmt.Errorf("no port's std to compare with: %s, the default, has no src/std "+
		"(set TACK_PORT to the port's source tree)", src)
}

// missingRuntime returns the error for src, the port's source tree,
// having no runtime directory, portRuntimeDir, to build the reference
// from, saying whether $TACK_PORT named src, explicit, or it is the
// default.
func missingRuntime(src string, explicit bool) error {
	if explicit {
		return fmt.Errorf("TACK_PORT is %s, which has no %s directory", src, portRuntimeDir)
	}
	return fmt.Errorf("no port's runtime to build the reference with: %s, the default, has no %s "+
		"directory (set TACK_PORT to the port's source tree)", src, portRuntimeDir)
}

// compilerEnv returns the environment a compiler runs in: this
// process's, with TACK_HOME set to env.home, the port's source tree, and
// TACK_CACHE to cache.
func compilerEnv(cache string) []string {
	return append(os.Environ(), "TACK_HOME="+env.home, "TACK_CACHE="+cache)
}

// homeStd returns the std every comparison reads, env.home's src/std.
func homeStd() string { return filepath.Join(env.home, "src", "std") }

// inProcessStd returns the std Go's loader reads when the harness runs
// it in process on a program held in memory, as the classification of
// the archives' cases and the backend's baseline do: env.home's src/std,
// the port's, which the comparisons read, once TestMain has set env.home
// up for TestPort. The harness's own tests that classify without
// TACK_PORT_TEST, TestClassifyCounts and the baseline's feature tests
// among them, run with no port tree, and read this module's frozen
// src/std (stdtest.Std); they check the classifier, which reads std's
// declarations alone, and TestPortFiles keeps std's files the same on
// both sides but for the paths portFileDivergences lists.
func inProcessStd() *loader.Std {
	if env.home == "" {
		return stdtest.Std()
	}
	return &loader.Std{FS: loader.FromFS(os.DirFS(env.home)), Dir: stdtest.StdPath}
}

// portBuildEnv returns the environment the Go compiler builds the port,
// or one of its tools, in: this process's, with TACK_HOME set to src,
// the port's source tree, so the build reads the port's own std, and
// TACK_CACHE to cache. It is compilerEnv's environment when src is
// env.home, as it is whenever the harness builds the port itself.
func portBuildEnv(src, cache string) []string {
	return append(os.Environ(), "TACK_HOME="+src, "TACK_CACHE="+cache)
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
