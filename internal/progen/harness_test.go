package progen_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

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

// sanitize are the flags every build of a generated program takes: C11,
// every warning an error, and the address and undefined behavior
// sanitizers, stopping at the first report.
// -Wno-tautological-overlap-compare works around a finding, a condition
// always false, as x < 3 && x > 5, written as is in C (see
// TestFindingOverlapCompareWarns).
var sanitize = []string{
	"-std=c11", "-Wall", "-Wextra", "-Werror",
	"-Wno-tautological-overlap-compare",
	"-fsanitize=address,undefined", "-fno-sanitize-recover=all",
}

// frontEnd compiles src, the program main.tck, through the loader, the
// resolver, the type checker and the C backend, and returns the program,
// the generated C and its header, or the diagnostics that rejected it,
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
	if d := resolver.New(res.Program, stdtest.MustBuiltins()).Resolve(); len(d) > 0 {
		return nil, nil, nil, diag.RenderAllPretty(res.Files, d)
	}
	if d := typeck.New(res.Program, res.Files, stdtest.MustBuiltins()).Check(); len(d) > 0 {
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
		if out, err := exec.Command("clang", args...).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("clang %s: %w\n%s", src, err, out)
		}
		objs = append(objs, obj)
	}
	for i, src := range stdtest.MustBuiltins().CFiles() {
		obj := filepath.Join(dir, fmt.Sprintf("builtin%s-%d.o", level, i))
		args := append(append([]string{}, sanitize...), level, "-c", "-I", stdtest.IncludeDir(),
			"-o", obj, src)
		if out, err := exec.Command("clang", args...).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("clang %s: %w\n%s", src, err, out)
		}
		objs = append(objs, obj)
	}
	return objs, nil
}

// sharedDir is where the shared objects are compiled, made once per test
// binary.
var sharedDir = sync.OnceValues(func() (string, error) {
	return os.MkdirTemp("", "progen-shared-")
})

// build compiles csrc, a generated program's C, at level, with the
// sanitizers and every warning an error, into dir, and returns the
// binary's path.
func build(dir, level string, prog *ast.Program, csrc, header []byte) (string, error) {
	sd, err := sharedDir()
	if err != nil {
		return "", err
	}
	objs, err := sharedObjects(sd, level, prog, header)
	if err != nil {
		return "", err
	}
	src := filepath.Join(dir, "program"+level+".c")
	if err := os.WriteFile(src, csrc, 0o600); err != nil {
		return "", fmt.Errorf("writing %s: %w", src, err)
	}
	bin := filepath.Join(dir, "prog"+level)
	args := append(append([]string{}, sanitize...), level, "-I", stdtest.IncludeDir(),
		"-o", bin, src)
	args = append(args, objs...)
	if out, err := exec.Command("clang", args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf(
			"the generated C does not compile cleanly at %s: %w\n%s",
			level,
			err,
			out,
		)
	}
	return bin, nil
}

// runBin runs bin, with PG_FAIL_AFTER set to fail when it is not empty,
// and returns its standard output, its standard error and its exit
// status.
func runBin(bin, fail string) (string, string, int) {
	cmd := exec.Command(bin)
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
		return stdout.String(), stderr.String(), 0
	case errors.As(err, &exit):
		return stdout.String(), stderr.String(), exit.ExitCode()
	}
	return stdout.String(), err.Error(), -1
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
	bins := map[string]string{}
	for _, level := range []string{"-O0", "-O2"} {
		bin, err := build(dir, level, prog, csrc, header)
		if err != nil {
			return p, outcome{failure: err.Error()}
		}
		bins[level] = bin
	}
	for level, bin := range bins {
		out, errOut, status := runBin(bin, "")
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
	return p, outcome{failure: failRuns(bins["-O0"], want)}
}

// failRuns runs bin with its allocations failing after n, from 0 until
// the run prints want, as if no allocation failed, or maxFailRuns, and
// returns what went wrong: any run that does not exit 0.
func failRuns(bin, want string) string {
	for n := range maxFailRuns {
		out, errOut, status := runBin(bin, fmt.Sprint(n))
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
