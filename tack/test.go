package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/codegen"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/testmain"
	"github.com/tacklang/tack/typeck"
)

// testCommand is the first argument that makes the command line tack
// test's (see runTest).
const testCommand = "test"

// testOptions is tack test's validated command line.
type testOptions struct {
	target  string
	pattern string
	verbose bool
	opt     codegen.Opt
}

// runTest is tack test, args being what follows test on the command
// line: it builds the package the target names with its tests and the
// main that runs them (see package testmain), runs the binary in the
// package's directory, its standard output and error those of tack, and
// returns its exit status, 0 when every test passed and 1 when one
// failed, as the binary's last line says. The program's diagnostics
// end the run with 1 before anything runs, as a build's do, and a
// command-line mistake with 2. A binary that ends any other way, killed
// by a signal or with another status, as a broken pipe ends it, is
// reported as a failure of the package, and the run ends with 1.
func runTest(ctx context.Context, args []string, env environment, stdout, stderr io.Writer) int {
	fs, values := newTestFlagSet()
	opts, err := parseTestArgs(fs, values, args)
	if errors.Is(err, flag.ErrHelp) {
		printTestUsage(fs, stdout)
		return exitOK
	}
	if err == nil {
		err = executeTest(ctx, opts, env, stdout, stderr)
	}
	return reportTest(err, stderr)
}

// reportTest is report for tack test, whose usage errors point at tack
// test --help, and whose run of the binary, when it fails, has reported
// itself (see testFailure).
func reportTest(err error, stderr io.Writer) int {
	var usageErr *usageError
	var failed *testFailure
	switch {
	case errors.As(err, &usageErr):
		_, _ = fmt.Fprintf(stderr, "tack test: %v\nRun 'tack test --help' for usage.\n", usageErr)
		return exitUsage
	case errors.As(err, &failed):
		if failed.msg != "" {
			_, _ = io.WriteString(stderr, failed.msg)
		}
		return exitFailure
	}
	return report(err, stderr)
}

// testFailure is a run of the tests that failed, with what the driver
// has to add to what the binary wrote: nothing when the binary reported
// it, ending with status 1, or the line saying how it ended otherwise.
type testFailure struct{ msg string }

// Error returns what the driver adds to the binary's report.
func (e *testFailure) Error() string { return e.msg }

// testFlagValues holds the raw values tack test's flags write into.
type testFlagValues struct {
	opts    map[codegen.Opt]*bool
	pattern string
	verbose bool
}

// newTestFlagSet declares every flag tack test accepts on a fresh
// FlagSet, which reports errors and prints nothing, and returns it with
// the values it writes into.
func newTestFlagSet() (*flag.FlagSet, *testFlagValues) {
	v := &testFlagValues{opts: make(map[codegen.Opt]*bool)}
	fs := flag.NewFlagSet("tack test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.StringVar(&v.pattern, "run", "",
		"run only the tests whose names hold `text`, byte for byte, case and all")
	fs.BoolVar(&v.verbose, "v", false, "write a line for each test as it ends: ok or FAIL, and its name")
	for _, opt := range codegen.Opts {
		v.opts[opt] = fs.Bool("O"+string(opt), false, "optimize at level "+string(opt))
	}
	return fs, v
}

// parseTestArgs parses args with fs into v and validates them: at most
// one target, . by default, and at most one -O flag.
func parseTestArgs(fs *flag.FlagSet, v *testFlagValues, args []string) (testOptions, error) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return testOptions{}, flag.ErrHelp
		}
		return testOptions{}, &usageError{msg: err.Error()}
	}
	opts := testOptions{target: ".", pattern: v.pattern, verbose: v.verbose, opt: codegen.DefaultOpt}
	var names []string
	for _, o := range codegen.Opts {
		if *v.opts[o] {
			opts.opt = o
			names = append(names, "-O"+string(o))
		}
	}
	if len(names) > 1 {
		return testOptions{}, usagef("%s cannot be combined; pick one", strings.Join(names, " and "))
	}
	switch rest := fs.Args(); {
	case len(rest) > 1:
		return testOptions{}, usagef(
			"expected one directory or import path, got %d arguments: %s (flags must come before it)",
			len(rest), strings.Join(quoteAll(rest), " "))
	case len(rest) == 1 && rest[0] == "":
		return testOptions{}, usagef("the directory or import path is empty")
	case len(rest) == 1:
		opts.target = rest[0]
	}
	return opts, nil
}

// printTestUsage writes tack test's help, listing every flag in fs, to w.
func printTestUsage(fs *flag.FlagSet, w io.Writer) {
	var b strings.Builder
	b.WriteString("usage: tack test [-run text] [-v] [-O level] [directory or import path]\n\n")
	b.WriteString("Builds the package in the directory, . by default, with its test files,\n")
	b.WriteString("those whose names end in _test.tck, and runs every test in them: each\n")
	b.WriteString("func test_x(mut t testing.T) error, in the order of their files' names\n")
	b.WriteString("and then of the source. The binary runs in the package's directory.\n\nFlags:\n")
	fs.VisitAll(func(f *flag.Flag) {
		if isOptFlag(f.Name) {
			return
		}
		arg, usage := flag.UnquoteUsage(f)
		b.WriteString("  -" + f.Name)
		if arg != "" {
			b.WriteString(" <" + arg + ">")
		}
		b.WriteString("\n        " + usage + "\n")
	})
	b.WriteString(optUsage())
	b.WriteString("  -h, --help\n        print this help and exit\n\n")
	b.WriteString("Exit status: 0 when every test passed, 1 if one failed, the package has\n")
	b.WriteString("errors or the build fails, 2 if the command line is wrong.\n")
	_, _ = io.WriteString(w, b.String())
}

// executeTest carries out a validated tack test command line, in the
// environment env.
func executeTest(
	ctx context.Context,
	opts testOptions,
	env environment,
	stdout, stderr io.Writer,
) (err error) {
	backend, _ := codegen.Get(defaultBackend)
	lib, err := findStd(env)
	if err != nil {
		return err
	}
	res, err := loadTests(opts, lib)
	if err != nil {
		return err
	}
	out, warnings, err := compileTests(res, backend, lib.builtinSet())
	if err != nil {
		return err
	}
	if err := writeWarnings(stderr, res, warnings, defaultMaxErrors); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "tk-test-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to delete temp directory: %w", rmErr))
		}
	}()
	// The binary runs in the package's directory, so its path is made
	// absolute: a relative TMPDIR names another place from there.
	if dir, err = filepath.Abs(dir); err != nil {
		return fmt.Errorf("cannot find the temp directory: %w", err)
	}
	bin := filepath.Join(dir, "tests")
	b := &builder{
		ctx: ctx, backend: backend, res: res, std: lib, maxErrors: defaultMaxErrors, opt: opts.opt,
		env: env, stdout: stdout, stderr: stderr,
	}
	if err := b.build(out, bin); err != nil {
		return err
	}
	pkg := res.Program.Main()
	return runTests(ctx, bin, filepath.FromSlash(pkg.Dir), pkg.Path, stdout, stderr)
}

// loadTests loads the package opts names for its tests (see
// loader.Loader.TestMain), a directory or an import path in the module
// found from the current directory, with the generated main told what
// opts says of the run. A file is not a package to test.
func loadTests(opts testOptions, lib *std) (*loader.Result, error) {
	l := newLoader(lib, defaultMaxErrors)
	l.TestMain = func(pkg *ast.Package, files source.Files) (string, []byte) {
		return testmain.FileName, testmain.Source(pkg, files, testmain.Options{
			Pkg: pkg.Path, Pattern: opts.pattern, Verbose: opts.verbose,
		})
	}
	info, statErr := os.Stat(opts.target)
	switch {
	case statErr == nil && info.IsDir():
		return loadDir(l, opts.target)
	case statErr == nil:
		return nil, usagef("%s is a file; tack test takes the directory of a package, or its import path",
			opts.target)
	case errors.Is(statErr, fs.ErrNotExist) && looksLikeImportPath(opts.target):
		return loadPath(l, opts.target)
	}
	return nil, fmt.Errorf("cannot find %s: %w", opts.target, statErr)
}

// compileTests runs the pipeline on res, a package loaded for its tests,
// as compile does for a build, and returns the generated source and the
// warnings, or the diagnostics as a *diagnosticsError: the resolver
// asks for no main function, as the generated main is the program's
// entry; the type checker checks the tests (see typeck.Checker.Tests);
// and what is reported in the generated main is dropped or made an
// internal error (see testmain.Diagnostics). A name of the package's that
// the generated main declares too is reported at the package's own
// declaration before any stage runs, and ends the run (see
// testmain.Clashes). A main package with no main
// function is reported, as --check reports it.
func compileTests(
	res *loader.Result,
	backend codegen.Backend,
	builtins *builtin.Set,
) ([]byte, []diag.Diagnostic, error) {
	diags := slices.Clone(res.Diags)
	prog := res.Program
	var pkg *ast.Package
	if prog != nil {
		pkg = prog.Main()
	}
	// A clash stops the run before the resolver, which would report it
	// again, in the generated main's words.
	if clashes := testmain.Clashes(pkg); len(clashes) > 0 {
		return nil, nil, diagnostics(res, append(diags, clashes...), defaultMaxErrors)
	}
	var out []byte
	if prog != nil {
		prog.Entry = testmain.Entry(prog)
		diags = append(diags, resolver.MissingMain(prog)...)
		r := resolver.New(prog, builtins)
		r.MainOptional = true
		diags = append(diags, r.Resolve()...)
		c := typeck.New(prog, res.Files, builtins)
		c.Tests = true
		diags = append(diags, c.Check()...)
		if !diag.HasErrors(diags) {
			if prog.Entry == nil {
				return nil, nil, errors.New("internal error: tack test generated no main")
			}
			var genDiags []diag.Diagnostic
			out, genDiags = backend.Generate(prog, builtins)
			diags = append(diags, genDiags...)
		}
	}
	diags = testmain.Diagnostics(diags, pkg)
	if diag.HasErrors(diags) {
		return nil, nil, diagnostics(res, diags, defaultMaxErrors)
	}
	if prog == nil {
		return nil, nil, errors.New("internal error: the loader built no program and reported no error")
	}
	return out, diags, nil
}

// runTests runs bin, the tests of the package with import path pkg, in
// dir, the package's directory, its output going to stdout and stderr,
// and returns nil when it exits 0, or a *testFailure: with nothing to
// add for 1, the status of a run whose last line says it failed, and
// with a line saying how it ended for any other status or a signal.
func runTests(ctx context.Context, bin, dir, pkg string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, bin)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return fmt.Errorf("the tests of %s were interrupted: %w", pkg, ctx.Err())
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return fmt.Errorf("cannot run the tests of %s: %w", pkg, err)
	}
	return &testFailure{msg: endMessage(pkg, exitErr.ProcessState)}
}

// endMessage returns what the driver says of a test binary of the
// package pkg that ended as state says, which is not with status 0:
// nothing for 1, which the binary has reported itself, and a line for
// any other end.
func endMessage(pkg string, state *os.ProcessState) string {
	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return fmt.Sprintf("FAIL %s: the test binary was killed by signal %d (%v)\n",
			pkg, int(ws.Signal()), ws.Signal())
	}
	if code := state.ExitCode(); code != exitFailure {
		return fmt.Sprintf("FAIL %s: the test binary exited with status %d\n", pkg, code)
	}
	return ""
}
