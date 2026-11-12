package progen_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/progen"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/typeck"
)

// stepBudget bounds the interpreter's run of a program; one that runs
// longer is skipped, as too slow to be worth building.
const stepBudget = 200000

// maxFailRuns bounds how many allocation failures a program is run with.
const maxFailRuns = 40

// clangTimeout bounds one clang call: compiling a shared object, or
// compiling and linking a generated program. It is generous, many times
// the second or two a build takes under the sanitizers, and still below
// the 60 seconds the fuzz engine lets one input run before it takes the
// worker for deadlocked and ends it, so that a hanging build fails its
// input here, with a message naming its source, instead.
const clangTimeout = 40 * time.Second

// runTimeout bounds one run of a built program. A generated program's
// interpreter run is held to stepBudget, so its binary finishes in well
// under a second; one still running after runTimeout is taken to hang.
// Like clangTimeout, it stays below the fuzz engine's 60 seconds per
// input, with room for the builds that come first.
const runTimeout = 30 * time.Second

// waitDelay is how long, after a command is killed for running past its
// timeout, its output is waited for before the command is given up on,
// in case a process it started still holds the output open.
const waitDelay = 5 * time.Second

// sanitize are the flags every build of a generated program takes: C11,
// every warning an error, and the address and undefined behavior
// sanitizers, stopping at the first report.
// -Wno-tautological-overlap-compare works around a finding, a condition
// always false, as x < 3 && x > 5, written as is in C (see
// TestFindingOverlapCompareWarns). -fno-finite-loops, which every build
// the driver runs passes, is left out on purpose, so these builds check
// the C backend's loop lowering on its own. -fstrict-flex-arrays=3 makes
// clang take only a struct member declared [] to be a flexible array, so
// the undefined behavior sanitizer checks every index into an array,
// which Tack lowers to a struct whose last member is a C array: without
// it a read past the array is not reported (see codegen/c's
// TestSanitizersCatchIndexPastAnArray).
// -fsanitize-address-use-after-return=always makes the address sanitizer
// report a read through a pointer into a function's frame after the
// function returns whatever ASAN_OPTIONS says (see codegen/c's
// TestSanitizersCatchUseAfterReturn).
var sanitize = []string{
	"-std=c11", "-Wall", "-Wextra", "-Werror",
	"-Wno-tautological-overlap-compare",
	"-fsanitize=address,undefined", "-fsanitize-address-use-after-return=always",
	"-fno-sanitize-recover=all",
	"-fstrict-flex-arrays=3",
}

// frontEnd compiles src, the program main.tck, through the loader, the
// resolver, the type checker and the C backend, and returns the program,
// the generated C and its header, or the diagnostics, an error among
// them, that rejected it,
// rendered.
func frontEnd(src string) (*ast.Program, []byte, []byte, string) {
	fsys := fstest.MapFS{"main.tck": {Data: []byte(src)}}
	l := &loader.Loader{
		FS:       loader.FromFS(fsys),
		Builtins: stdtest.MustBuiltins(),
		Std:      stdtest.Std(),
	}
	res, err := l.File("main.tck")
	if err != nil {
		return nil, nil, nil, err.Error()
	}
	if len(res.Diags) > 0 {
		return nil, nil, nil, diag.RenderAllPretty(res.Files, res.Diags)
	}
	// A generated program may leave a helper, a type or a parameter
	// unused, which is a warning, and stops nothing.
	if d := resolver.New(res.Program, stdtest.MustBuiltins()).Resolve(); diag.HasErrors(d) {
		return nil, nil, nil, diag.RenderAllPretty(res.Files, d)
	}
	if d := typeck.New(res.Program, res.Files, stdtest.MustBuiltins()).Check(); diag.HasErrors(d) {
		return nil, nil, nil, diag.RenderAllPretty(res.Files, d)
	}
	out, d := c.New().Generate(res.Program, stdtest.MustBuiltins())
	if len(d) > 0 {
		return nil, nil, nil, diag.RenderAllPretty(res.Files, d)
	}
	return res.Program, out, c.New().Header(res.Program), ""
}

// objects are the objects every build at one optimization level links:
// std/builtin's C, std/fmt's and the program's own C file, compiled once
// per level, since what they are compiled against, the header of a
// generated program, is the same for every one.
type objects struct {
	err  error
	objs []string
	once sync.Once
}

// shared holds the objects of each level.
var shared = map[string]*objects{"-O0": {}, "-O2": {}}

// sharedObjects returns the objects for level, compiled into dir the first
// time, against header.
func sharedObjects(dir, level string, prog *ast.Program, header []byte) ([]string, error) {
	o := shared[level]
	o.once.Do(func() { o.objs, o.err = compileShared(dir, level, prog, header) })
	return o.objs, o.err
}

// compileShared compiles the shared objects for level into dir.
func compileShared(dir, level string, prog *ast.Program, header []byte) ([]string, error) {
	hdr := filepath.Join(dir, c.HeaderName)
	pgc := filepath.Join(dir, "pg.c")
	if err := os.WriteFile(hdr, header, 0o600); err != nil {
		return nil, fmt.Errorf("writing %s: %w", hdr, err)
	}
	if err := os.WriteFile(pgc, []byte(progen.CSource), 0o600); err != nil {
		return nil, fmt.Errorf("writing %s: %w", pgc, err)
	}
	srcs := []string{pgc}
	for _, pkg := range prog.Packages {
		for _, f := range pkg.CFiles {
			srcs = append(srcs, filepath.Join(stdtest.Home(), f))
		}
	}
	var objs []string
	for i, src := range srcs {
		obj := filepath.Join(dir, fmt.Sprintf("shared%s-%d.o", level, i))
		args := append(append([]string{}, sanitize...), level, "-c", "-I", stdtest.IncludeDir(),
			"-I", dir, "-include", hdr, "-o", obj, src)
		if out, err := clang(src, args...); err != nil {
			return nil, fmt.Errorf("clang %s: %w\n%s", src, err, out)
		}
		objs = append(objs, obj)
	}
	for i, src := range stdtest.RuntimeCFiles() {
		obj := filepath.Join(dir, fmt.Sprintf("builtin%s-%d.o", level, i))
		args := append(append([]string{}, sanitize...), level, "-c", "-I", stdtest.IncludeDir(),
			"-o", obj, src)
		if out, err := clang(src, args...); err != nil {
			return nil, fmt.Errorf("clang %s: %w\n%s", src, err, out)
		}
		objs = append(objs, obj)
	}
	return objs, nil
}

// clang runs clang with args, which compile src, and returns its
// combined output; it is killed after clangTimeout (see runTool).
func clang(src string, args ...string) ([]byte, error) {
	return runTool(clangTimeout, src, "clang", args...)
}

// runTool runs name with args, a tool working on src, and returns its
// combined output. It is killed once it has run for timeout, and then
// fails with an error that says so and names src, whatever it printed.
func runTool(timeout time.Duration, src, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = waitDelay
	out, err := cmd.CombinedOutput()
	if err != nil && ctx.Err() != nil {
		return out, fmt.Errorf("%s on %s did not finish within %v and was killed", name, src,
			timeout)
	}
	return out, err
}

// sharedRootEnv names the environment variable that holds the directory
// every process's shared objects directory is made in. TestMain sets it
// in the process go test starts, so the fuzz workers that process
// starts, which inherit its environment, make theirs in the same one.
const sharedRootEnv = "PROGEN_SHARED_ROOT"

// sharedRoot is the directory sharedDir makes its directory in, set by
// TestMain before any test runs.
var sharedRoot string

// sharedDirMade is the directory sharedDir made, empty until it has
// made one, so that TestMain removes it.
var sharedDirMade string

// sharedDir is where the shared objects are compiled, made once per test
// process, in sharedRoot.
var sharedDir = sync.OnceValues(func() (string, error) {
	d, err := os.MkdirTemp(sharedRoot, "objects-")
	if err != nil {
		return "", err
	}
	sharedDirMade = d
	return d, nil
})

// TestMain runs the tests and then removes the directories the shared
// objects were compiled in. Each fuzz worker is a process of its own,
// with a directory of its own, and runs TestMain too, so it removes its
// directory when its fuzzing returns; but the fuzz engine stops a worker
// by closing its input, then interrupts it after a second, and kills it
// after another, so a worker in the middle of an input, whose builds and
// runs take seconds, ends before it can. So the process go test starts
// makes the root every directory goes in, passes it to its workers in
// sharedRootEnv, and removes it, with whatever a killed worker left, once
// its m.Run returns, by which time the engine has waited for every worker
// to end. A root that was already set in the environment belongs to
// whoever set it, and is left in place.
func TestMain(m *testing.M) {
	root, owned := os.Getenv(sharedRootEnv), false
	if root == "" {
		var err error
		if root, err = os.MkdirTemp("", "progen-shared-"); err != nil {
			fmt.Fprintln(os.Stderr, "making the shared objects' root:", err)
			os.Exit(1)
		}
		if err := os.Setenv(sharedRootEnv, root); err != nil {
			fmt.Fprintln(os.Stderr, "setting", sharedRootEnv+":", err)
			os.Exit(1)
		}
		owned = true
	}
	sharedRoot = root
	code := m.Run()
	remove := sharedDirMade
	if owned {
		remove = root
	}
	if remove != "" {
		if err := os.RemoveAll(remove); err != nil {
			fmt.Fprintln(os.Stderr, "removing the shared objects:", err)
			if code == 0 {
				code = 1
			}
		}
	}
	os.Exit(code)
}

// build compiles csrc, a generated program's C, at level, with the
// sanitizers and every warning an error, into dir, and returns the
// binary's path and the path of the C it was built from.
func build(dir, level string, prog *ast.Program, csrc, header []byte) (string, string, error) {
	sd, err := sharedDir()
	if err != nil {
		return "", "", err
	}
	objs, err := sharedObjects(sd, level, prog, header)
	if err != nil {
		return "", "", err
	}
	src := filepath.Join(dir, "program"+level+".c")
	if err := os.WriteFile(src, csrc, 0o600); err != nil {
		return "", "", fmt.Errorf("writing %s: %w", src, err)
	}
	bin := filepath.Join(dir, "prog"+level)
	args := append(append([]string{}, sanitize...), level, "-I", stdtest.IncludeDir(),
		"-o", bin, src)
	args = append(args, objs...)
	if out, err := clang(src, args...); err != nil {
		return "", "", fmt.Errorf(
			"the generated C does not compile cleanly at %s: %w\n%s",
			level,
			err,
			out,
		)
	}
	return bin, src, nil
}

// runBin runs bin, built from src, with PG_FAIL_AFTER set to fail when it
// is not empty, killed after runTimeout (see runBinWithin).
func runBin(bin, src, fail string) (string, string, int, error) {
	return runBinWithin(runTimeout, bin, src, fail)
}

// runBinWithin runs bin, built from src, with PG_FAIL_AFTER set to fail
// when it is not empty, and returns its standard output, its standard
// error and its exit status. The error is not nil when bin did not run
// to an exit of its own: it could not be started, or it was killed for
// running past timeout, and the error says which, naming src.
func runBinWithin(timeout time.Duration, bin, src, fail string) (string, string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.WaitDelay = waitDelay
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_leaks=0")
	if fail != "" {
		cmd.Env = append(cmd.Env, "PG_FAIL_AFTER="+fail)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return stdout.String(), stderr.String(), 0, nil
	case ctx.Err() != nil:
		return stdout.String(), stderr.String(), -1, fmt.Errorf(
			"%s, built from %s, did not finish within %v and was killed", bin, src, timeout)
	case errors.As(err, &exit):
		return stdout.String(), stderr.String(), exit.ExitCode(), nil
	}
	return stdout.String(), stderr.String(), -1, fmt.Errorf("running %s, built from %s: %w",
		bin, src, err)
}

// outcome is what checking one generated program found.
type outcome struct {
	rejected string // the diagnostics, when the compiler rejected it
	failure  string // what an oracle found wrong, empty when nothing
	slow     bool   // the interpreter ran past its budget, so it was not built
}

// check generates the program src decides and puts it through every
// oracle: the front end accepts it, its C builds cleanly at -O0 and -O2
// under the sanitizers, each binary exits 0 printing what the
// interpreter computed, and with every allocation failure up to the
// first that changes nothing it still exits 0, nothing leaked.
func check(t testing.TB, src progen.Source) (*progen.Program, outcome) {
	t.Helper()
	p := progen.Generate(src)
	want, ok := p.Expect(stepBudget)
	if !ok {
		return p, outcome{slow: true}
	}
	prog, csrc, header, rejected := frontEnd(p.Source)
	if rejected != "" {
		return p, outcome{rejected: rejected}
	}
	dir := t.TempDir()
	bins, srcs := map[string]string{}, map[string]string{}
	for _, level := range []string{"-O0", "-O2"} {
		bin, csrcPath, err := build(dir, level, prog, csrc, header)
		if err != nil {
			return p, outcome{failure: err.Error()}
		}
		bins[level], srcs[level] = bin, csrcPath
	}
	for level, bin := range bins {
		out, errOut, status, err := runBin(bin, srcs[level], "")
		if err != nil {
			return p, outcome{failure: fmt.Sprintf("at %s: %v\n%s", level, err, errOut)}
		}
		if status != 0 || out != want {
			return p, outcome{failure: fmt.Sprintf(
				"at %s: exit %d, printed %q, want exit 0 and %q\n%s",
				level,
				status,
				out,
				want,
				errOut,
			)}
		}
	}
	return p, outcome{failure: failRuns(bins["-O0"], srcs["-O0"], want)}
}

// failRuns runs bin, built from src, with its allocations failing after
// n, from 0 until the run prints want, as if no allocation failed, or
// maxFailRuns, and returns what went wrong: any run that does not exit
// 0, or that does not finish.
func failRuns(bin, src, want string) string {
	for n := range maxFailRuns {
		out, errOut, status, err := runBin(bin, src, fmt.Sprint(n))
		if err != nil {
			return fmt.Sprintf("with allocations failing after %d: %v\n%s", n, err, errOut)
		}
		if status != 0 {
			return fmt.Sprintf("with allocations failing after %d: exit %d, printed %q\n%s",
				n, status, out, errOut)
		}
		if out == want {
			return ""
		}
	}
	return ""
}
