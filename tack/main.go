package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/codegen"
	_ "github.com/tacklang/tack/codegen/c" // backends register from init; importing plugs one in
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/typeck"
)

// Exit codes returned by run and passed to os.Exit by main.
const (
	// exitOK means the requested work was done.
	exitOK = 0
	// exitFailure means the program was rejected with diagnostics, or reading,
	// writing, or building failed.
	exitFailure = 1
	// exitUsage means the command line itself was wrong.
	exitUsage = 2
)

// defaultBackend is the backend used when --backend is not given.
const defaultBackend = "c"

// dump names the pipeline stage whose output is printed instead of building
// a binary.
type dump int

// The stages a dump flag can stop the pipeline after, in pipeline order.
const (
	dumpNone   dump = iota // build a binary
	dumpLex                // --lex: the token stream
	dumpAST                // --ast: the parsed syntax tree
	dumpRes                // --res: the resolved syntax tree
	dumpEmit               // --emit: the generated target source
	dumpHeader             // --emit-header: tack.h, written to the package directories
)

// options is the validated command line.
type options struct {
	file         string
	backendName  string
	outBin       string
	opt          codegen.Opt
	maxErrors    int
	dump         dump
	listBackends bool
	// check is --check: report every diagnostic, and the dump asked for,
	// without building (see executeCheck).
	check bool
}

// usageError is a mistake on the command line. run reports it together with
// a pointer to --help and exits with exitUsage.
type usageError struct{ msg string }

// Error returns the description of the command-line mistake.
func (e *usageError) Error() string { return e.msg }

// usagef builds a usageError from a format string.
func usagef(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

// diagnosticsError carries the diagnostics a pipeline stage reported. Its
// rendering is the compiler's user-facing output, so run prints it as is,
// without the "tack:" prefix other errors get.
type diagnosticsError struct{ rendered string }

// Error returns the rendered diagnostics.
func (e *diagnosticsError) Error() string { return e.rendered }

// environment is what the driver takes from the process's environment,
// read by main and passed down, so nothing below main reads it and tests
// can give their own.
type environment struct {
	// homeDir returns the user's home directory, as os.UserHomeDir does.
	// It is called only when tackHome is empty, to find ~/.tack.
	homeDir func() (string, error)
	// userCacheDir returns the user's cache directory, as os.UserCacheDir
	// does. It is called only when tackCache is empty and a build needs
	// the cache, to find <user cache>/tack.
	userCacheDir func() (string, error)
	// toolVersion runs argv, a backend's VersionCmd, and returns its
	// standard output, which identifies the toolchain std's C is compiled
	// with. It is called once per build that links std objects.
	toolVersion func(ctx context.Context, argv []string) ([]byte, error)
	// tackHome is $TACK_HOME, the Tack installation, empty when it is
	// unset or empty.
	tackHome string
	// tackCache is $TACK_CACHE, the build cache, empty when it is unset or
	// empty.
	tackCache string
}

// main runs the compiler with the process's arguments, environment and
// standard streams and exits with the code run returns. An interrupt
// cancels the run so that temporary files are still removed; a second
// interrupt kills the process.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	env := environment{
		tackHome:     os.Getenv("TACK_HOME"),
		homeDir:      os.UserHomeDir,
		tackCache:    os.Getenv("TACK_CACHE"),
		userCacheDir: os.UserCacheDir,
		toolVersion:  toolchainVersion,
	}
	code := run(ctx, os.Args[1:], env, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run is the whole driver: it parses args, compiles the named file, and
// either prints the requested intermediate form to stdout, builds a
// binary, or, for --check, checks it. The standard library is found from env. Diagnostics and errors
// go to stderr. It returns the process exit code (exitOK, exitFailure, or
// exitUsage) and never exits itself, which is what lets tests call it in
// process.
func run(ctx context.Context, args []string, env environment, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == testCommand {
		return runTest(ctx, args[1:], env, stdout, stderr)
	}
	fs, values := newFlagSet()
	opts, err := parseArgs(fs, values, args)
	if errors.Is(err, flag.ErrHelp) {
		printUsage(fs, stdout)
		return exitOK
	}
	switch {
	case err != nil:
	case opts.check:
		err = executeCheck(opts, env, stdout, stderr)
	default:
		err = execute(ctx, opts, env, stdout, stderr)
	}
	return report(err, stderr)
}

// report writes err to stderr in the form its kind calls for and returns the
// matching exit code. It is the one place driver errors are turned into
// output and exit codes.
func report(err error, stderr io.Writer) int {
	var usageErr *usageError
	var diagErr *diagnosticsError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &diagErr):
		// Nothing useful can be done if stderr itself is unwritable.
		_, _ = io.WriteString(stderr, diagErr.rendered)
		return exitFailure
	case errors.As(err, &usageErr):
		_, _ = fmt.Fprintf(stderr, "tack: %v\nRun 'tack --help' for usage.\n", usageErr)
		return exitUsage
	default:
		_, _ = fmt.Fprintf(stderr, "tack: %v\n", err)
		return exitFailure
	}
}

// flagValues holds the raw values a flag.FlagSet writes into, before
// parseArgs validates and folds them into an options.
type flagValues struct {
	opts         map[codegen.Opt]*bool // -O0 and on, by level
	backendName  string
	outBin       string
	maxErrors    int
	maxDiags     int
	lex          bool
	ast          bool
	res          bool
	emit         bool
	emitHeader   bool
	listBackends bool
	check        bool
}

// newFlagSet declares every flag tack accepts on a fresh FlagSet and returns
// it with the flagValues it writes into. The set reports errors instead of
// exiting and prints nothing itself; run decides what reaches the user.
func newFlagSet() (*flag.FlagSet, *flagValues) {
	v := &flagValues{}
	fs := flag.NewFlagSet("tack", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.BoolVar(&v.lex, "lex", false, "print the token stream and stop")
	fs.BoolVar(&v.ast, "ast", false, "print the parsed syntax tree as JSON and stop")
	fs.BoolVar(&v.res, "res", false, "print the resolved syntax tree as JSON and stop")
	fs.BoolVar(&v.emit, "emit", false, "print the generated code and stop")
	fs.BoolVar(&v.emitHeader, "emit-header", false,
		"write tack.h, which declares the extern funcs, into the target's directory and\n"+
			"        every package directory with C files or extern funcs, and stop")
	fs.BoolVar(&v.check, "check", false,
		"check the program, or the library packages at or below a directory, and\n"+
			"        report every diagnostic without building; a dump flag adds its output")
	fs.StringVar(&v.backendName, "backend", defaultBackend,
		"use the code generation backend `name`")
	fs.BoolVar(&v.listBackends, "backends", false,
		"list the available code generation backends and stop")
	fs.StringVar(&v.outBin, "out", "",
		"write the linked binary to `path` (required unless --check or a dump flag is given)")
	fs.IntVar(&v.maxErrors, "max-errors", defaultMaxErrors,
		"report at most `n` errors, then say there are more; 0 reports them all")
	fs.IntVar(&v.maxDiags, "max-diags", defaultMaxErrors,
		"the old name of --max-errors")
	v.opts = make(map[codegen.Opt]*bool)
	for _, opt := range codegen.Opts {
		v.opts[opt] = fs.Bool("O"+string(opt), false, "optimize at level "+string(opt))
	}
	return fs, v
}

// parseArgs parses args with fs into v, both from newFlagSet, and validates
// the result. It returns flag.ErrHelp when help was asked for and a
// *usageError for any other command-line mistake.
func parseArgs(fs *flag.FlagSet, v *flagValues, args []string) (options, error) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return options{}, flag.ErrHelp
		}
		return options{}, &usageError{msg: err.Error()}
	}

	opts := options{
		backendName:  v.backendName,
		outBin:       v.outBin,
		maxErrors:    v.maxErrors,
		listBackends: v.listBackends,
		check:        v.check,
	}
	limitName := "--max-errors"
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "max-diags" {
			opts.maxErrors, limitName = v.maxDiags, "--max-diags"
		}
	})
	if opts.maxErrors < 0 {
		return options{}, usagef("%s must be 0 or more, got %d", limitName, opts.maxErrors)
	}
	if opts.listBackends {
		// Listing backends needs nothing else from the command line.
		return opts, nil
	}

	var err error
	if opts.dump, err = dumpOf(*v); err != nil {
		return options{}, err
	}
	if opts.opt, err = optOf(*v, opts.dump, opts.check); err != nil {
		return options{}, err
	}
	if opts.file, err = fileArg(fs.Args()); err != nil {
		return options{}, err
	}
	if err := checkCombination(opts); err != nil {
		return options{}, err
	}
	return opts, nil
}

// dumpOf returns the single dump the flags in v ask for. Asking for more
// than one is a usage error, since only one stage's output can be printed.
func dumpOf(v flagValues) (dump, error) {
	set := []struct {
		name string
		d    dump
		on   bool
	}{
		{"--lex", dumpLex, v.lex},
		{"--ast", dumpAST, v.ast},
		{"--res", dumpRes, v.res},
		{"--emit", dumpEmit, v.emit},
		{"--emit-header", dumpHeader, v.emitHeader},
	}
	d := dumpNone
	var names []string
	for _, s := range set {
		if s.on {
			d = s.d
			names = append(names, s.name)
		}
	}
	if len(names) > 1 {
		return dumpNone, usagef("%s cannot be combined; pick one", strings.Join(names, " and "))
	}
	return d, nil
}

// optOf returns the optimization level the flags in v ask for, the
// default if none. Asking for two is a usage error, as is asking for one
// with --check, check set, or d, a dump, which build nothing to optimize.
func optOf(v flagValues, d dump, check bool) (codegen.Opt, error) {
	var names []string
	opt := codegen.DefaultOpt
	for _, o := range codegen.Opts {
		if *v.opts[o] {
			opt = o
			names = append(names, "-O"+string(o))
		}
	}
	switch {
	case len(names) > 1:
		return "", usagef("%s cannot be combined; pick one", strings.Join(names, " and "))
	case len(names) == 1 && check:
		return "", usagef("%s cannot be combined with --check, which builds nothing", names[0])
	case len(names) == 1 && d != dumpNone:
		return "", usagef(
			"%s cannot be combined with a dump flag, which prints instead of building",
			names[0],
		)
	}
	return opt, nil
}

// fileArg returns the one input file named by the positional arguments.
func fileArg(rest []string) (string, error) {
	switch {
	case len(rest) == 0:
		return "", usagef("no input file")
	case len(rest) > 1:
		return "", usagef(
			"expected one input file, got %d arguments: %s (flags must come before the file)",
			len(rest), strings.Join(quoteAll(rest), " "),
		)
	case rest[0] == "":
		return "", usagef("input file name is empty")
	}
	return rest[0], nil
}

// checkCombination rejects flag values that are each valid alone but
// meaningless or contradictory together.
func checkCombination(opts options) error {
	switch {
	case opts.check && opts.outBin != "":
		return usagef("--out cannot be combined with --check, which builds nothing")
	case opts.check:
	case opts.dump == dumpNone && opts.outBin == "":
		return usagef("--out is required to build a binary")
	case opts.dump != dumpNone && opts.outBin != "":
		return usagef("--out cannot be combined with a dump flag, which prints instead of building")
	}
	if _, ok := codegen.Get(opts.backendName); !ok {
		return usagef(
			"unknown backend %q; available: %s",
			opts.backendName, strings.Join(codegen.Names(), ", "),
		)
	}
	return nil
}

// quoteAll returns each of ss in Go quoted form, so that empty and
// whitespace-only arguments are visible in an error message.
func quoteAll(ss []string) []string {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = strconv.Quote(s)
	}
	return q
}

// printUsage writes the help text, listing every flag in fs, to w.
func printUsage(fs *flag.FlagSet, w io.Writer) {
	var b strings.Builder
	b.WriteString("usage: tack --out <path> [flags] <file or directory>\n")
	b.WriteString(
		"       tack (--lex | --ast | --res | --emit | --emit-header) [flags] <file or directory>\n",
	)
	b.WriteString("       tack --check [dump flag] [flags] <file or directory>\n")
	b.WriteString("       tack --backends\n")
	b.WriteString("       tack test [-run text] [-v] [-O level] [directory or import path]\n\nFlags:\n")
	fs.VisitAll(func(f *flag.Flag) {
		if isOptFlag(f.Name) {
			return
		}
		arg, usage := flag.UnquoteUsage(f)
		b.WriteString("  --" + f.Name)
		if arg != "" {
			b.WriteString(" <" + arg + ">")
		}
		b.WriteString("\n        " + usage)
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" {
			b.WriteString(" (default " + f.DefValue + ")")
		}
		b.WriteString("\n")
	})
	b.WriteString(optUsage())
	b.WriteString("  -h, --help\n        print this help and exit\n\n")
	b.WriteString("Exit status: 0 on success, 1 if the program has errors or the build\n")
	b.WriteString("fails, 2 if the command line is wrong.\n")
	// Help that cannot be written has no one to report the failure to.
	_, _ = io.WriteString(w, b.String())
}

// isOptFlag reports whether name is an -O flag's.
func isOptFlag(name string) bool {
	for _, o := range codegen.Opts {
		if name == "O"+string(o) {
			return true
		}
	}
	return false
}

// optUsage is the help's one entry for every -O flag, written as clang
// writes them, with one dash.
func optUsage() string {
	names := make([]string, len(codegen.Opts))
	for i, o := range codegen.Opts {
		names[i] = "-O" + string(o)
	}
	return "  " + strings.Join(names, ", ") + "\n" +
		"        optimize the generated C and the C files at that level, as clang's\n" +
		"        -O flags do: 0 for none, 1 to 3 for faster code, s and z for smaller\n" +
		"        (default -O" + string(codegen.DefaultOpt) + ")\n"
}

// execute carries out a validated command line, in the environment env.
func execute(ctx context.Context, opts options, env environment, stdout, stderr io.Writer) error {
	if opts.listBackends {
		return writeAll(stdout, strings.Join(codegen.Names(), "\n")+"\n")
	}

	// checkCombination has already confirmed the backend exists.
	backend, _ := codegen.Get(opts.backendName)

	if opts.dump == dumpNone {
		if err := checkOutPath(opts.outBin, opts.file); err != nil {
			return err
		}
	}

	lib, err := stdFor(opts.dump, env)
	if err != nil {
		return err
	}

	res, single, err := load(opts.file, opts.dump, lib, opts.maxErrors)
	if err != nil {
		return err
	}

	out, err := compileWarned(res, single, backend, lib.builtinSet(), opts, stderr)
	if err != nil {
		return err
	}

	switch opts.dump {
	case dumpNone:
	case dumpHeader:
		return writeHeaders(res.Program, backend.Header(res.Program))
	default:
		return writeAll(stdout, withTrailingNewline(out))
	}
	b := &builder{
		ctx: ctx, backend: backend, res: res, std: lib, maxErrors: opts.maxErrors, opt: opts.opt,
		env: env, stdout: stdout, stderr: stderr,
	}
	return b.build(out, opts.outBin)
}

// load reads the program target names: the package in it if it is a
// directory, the package with that import path in the module found from
// the current directory if it names nothing on disk and looks like one,
// and otherwise the one file it names. A dump of the tokens or the syntax
// trees stops loading there, having read only the target's own files. No
// package may be named after one of lib's builtins, and an import of a
// std package is read from lib, which is nil only for such a dump. It also
// reports whether the target is a single file.
func load(target string, d dump, lib *std, maxErrors int) (*loader.Result, bool, error) {
	l := newLoader(lib, maxErrors)
	l.Stop = stopFor(d)
	return loadWith(l, target)
}

// newLoader returns a Loader that reads from the operating system, with
// lib's builtins and lib as the standard library, whose parse of a file
// goes past at most maxErrors syntax errors when maxErrors is positive.
func newLoader(lib *std, maxErrors int) *loader.Loader {
	return &loader.Loader{
		FS: loader.OS{}, Builtins: lib.builtinSet(), Std: lib.loaderStd(), MaxErrors: maxErrors,
	}
}

// stopFor returns the loader stage the dump d stops after: lexing for
// --lex, parsing for --ast, and none for any other.
func stopFor(d dump) loader.Stage {
	switch d {
	case dumpLex:
		return loader.StageLex
	case dumpAST:
		return loader.StageParse
	}
	return ""
}

// loadWith loads target with l, as load describes, and reports whether it
// is a single file.
func loadWith(l *loader.Loader, target string) (*loader.Result, bool, error) {
	info, statErr := os.Stat(target)
	switch {
	case statErr == nil && info.IsDir():
		res, err := loadDir(l, target)
		return res, false, err
	case errors.Is(statErr, fs.ErrNotExist) && looksLikeImportPath(target):
		res, err := loadPath(l, target)
		return res, false, err
	}
	res, err := l.File(filepath.ToSlash(target))
	return res, true, err //nolint:wrapcheck // the loader's errors are the driver's
}

// loadDir loads the directory target with l. Its module is found from the
// directory's absolute path, so a tack.mod above the current directory is
// found too, and files are shown relative to the current directory.
func loadDir(l *loader.Loader, target string) (*loader.Result, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return nil, fmt.Errorf("cannot find %s: %w", target, err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("cannot find the current directory: %w", err)
	}
	l.Display = relativeTo(cwd)
	return l.Dir(filepath.ToSlash(abs)) //nolint:wrapcheck // the loader's errors are the driver's
}

// loadPath loads the import path target with l, in the module found from
// the current directory, showing files relative to it.
func loadPath(l *loader.Loader, target string) (*loader.Result, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("cannot find the current directory: %w", err)
	}
	l.Display = relativeTo(cwd)
	res, err := l.Path(filepath.ToSlash(cwd), target)
	if err != nil {
		return nil, fmt.Errorf(
			"%s is not a file or a directory, nor an import path: %w",
			target,
			err,
		)
	}
	return res, nil
}

// looksLikeImportPath reports whether target, which names nothing on disk,
// could be an import path rather than a mistyped file: it is not absolute,
// does not start with a dot, and does not end in .tck.
func looksLikeImportPath(target string) bool {
	return target != "" && !filepath.IsAbs(target) && !strings.HasPrefix(target, ".") &&
		!strings.HasSuffix(target, ".tck")
}

// relativeTo returns a loader Display function that shows a slash path
// relative to dir when it is in dir or below it, and as it is otherwise.
func relativeTo(dir string) func(string) string {
	return func(name string) string {
		rel, err := filepath.Rel(dir, filepath.FromSlash(name))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return name
		}
		return filepath.ToSlash(rel)
	}
}

// checkOutPath fails early, before any compiling, when the binary could not
// be written to outBin: its directory is missing or not a directory, outBin
// is itself a directory, or outBin is the input file, which linking would
// overwrite.
func checkOutPath(outBin, input string) error {
	dir := filepath.Dir(outBin)
	if info, err := os.Stat(dir); err != nil {
		return fmt.Errorf("cannot write --out %s: %w", outBin, err)
	} else if !info.IsDir() {
		return fmt.Errorf("cannot write --out %s: %s is not a directory", outBin, dir)
	}

	// A missing output is the normal case. Any other Stat failure is left
	// for the toolchain to report when it tries to write there.
	if outInfo, err := os.Stat(outBin); err == nil {
		if outInfo.IsDir() {
			return fmt.Errorf("cannot write --out %s: it is a directory", outBin)
		}
		if inInfo, err := os.Stat(input); err == nil && os.SameFile(inInfo, outInfo) {
			return usagef("--out %s is the input file; linking would overwrite it", outBin)
		}
	}
	return nil
}

// compile runs the pipeline on a loaded program, with builtins, up to the
// stage d names, or through code generation when d is dumpNone, and
// returns what that stage produced: the formatted dump for a dump, the
// generated source otherwise. single says the program is a single file, whose trees are
// dumped as that file rather than as a list or a program.
//
// Every stage up to the one asked for runs, whatever an earlier one
// reported, and the problems all of them found are reported together: the
// loader's, the resolver's and, through code generation, the type
// checker's. A name or a type that failed is left invalid, which later
// stages report nothing more about (see packages resolver and typeck). Only
// a loader that could not build a program at all, after a file that does
// not lex for one, ends the run before the resolver. A dump, or code
// generation, happens only when no stage reported an error, and it
// returns the warnings the stages reported, which do not stop it;
// otherwise the run ends with a *diagnosticsError holding the warnings
// and at most maxErrors errors when maxErrors is positive (see
// diagnostics). The backend checks the same itself, refusing a program
// with errors (see codegen.Refuse).
func compile(
	res *loader.Result,
	single bool,
	backend codegen.Backend,
	builtins *builtin.Set,
	d dump,
	maxErrors int,
) ([]byte, []diag.Diagnostic, error) {
	diags := slices.Clone(res.Diags)
	var out []byte
	var err error
	if d == dumpLex || d == dumpAST {
		if !diag.HasErrors(diags) {
			out, err = loadedDump(res, single, d)
		}
	} else {
		out, diags, err = runStages(res, single, backend, builtins, d, diags)
	}
	switch {
	case err != nil:
		return nil, nil, err
	case diag.HasErrors(diags):
		return nil, nil, diagnostics(res, diags, maxErrors)
	}
	return out, diags, nil
}

// runStages is compile past the loader, for any dump but --lex and
// --ast: it runs the resolver, the type checker and the backend on the
// program res holds, up to the stage d names, diags being what the loader
// reported, and returns that stage's output, when no stage reported an
// error, with every diagnostic so far.
func runStages(
	res *loader.Result,
	single bool,
	backend codegen.Backend,
	builtins *builtin.Set,
	d dump,
	diags []diag.Diagnostic,
) ([]byte, []diag.Diagnostic, error) {
	prog := res.Program
	if prog == nil {
		if !diag.HasErrors(diags) {
			return nil, nil, errors.New(
				"internal error: the loader built no program and reported no error",
			)
		}
		return nil, diags, nil
	}
	diags = append(diags, resolver.New(prog, builtins).Resolve()...)
	if d == dumpRes {
		if diag.HasErrors(diags) {
			return nil, diags, nil
		}
		out, err := resolvedDump(prog, single)
		return out, diags, err
	}
	diags = append(diags, typeck.New(prog, res.Files, builtins).Check()...)
	if diag.HasErrors(diags) {
		return nil, diags, nil
	}
	out, genDiags := backend.Generate(prog, builtins)
	return out, append(diags, genDiags...), nil
}

// compileWarned is compile, for the dump opts asks for or a build, with
// the warnings of a run that succeeded written to stderr (see
// writeWarnings).
func compileWarned(
	res *loader.Result,
	single bool,
	backend codegen.Backend,
	builtins *builtin.Set,
	opts options,
	stderr io.Writer,
) ([]byte, error) {
	out, warnings, err := compile(res, single, backend, builtins, opts.dump, opts.maxErrors)
	if err != nil {
		return nil, err
	}
	return out, writeWarnings(stderr, res, warnings, opts.maxErrors)
}

// writeWarnings writes warnings, what the stages of a run that succeeded
// reported, to stderr, rendered as diagnostics are (see
// renderDiagnostics), or nothing when there are none.
func writeWarnings(
	stderr io.Writer,
	res *loader.Result,
	warnings []diag.Diagnostic,
	maxErrors int,
) error {
	if len(warnings) == 0 {
		return nil
	}
	return writeAll(stderr, renderDiagnostics(res, warnings, maxErrors))
}

// defaultMaxErrors is how many errors the driver reports when
// --max-errors is not given.
const defaultMaxErrors = 10

// diagnostics returns the *diagnosticsError that reports diags, problems
// in the program res holds (see renderDiagnostics).
func diagnostics(res *loader.Result, diags []diag.Diagnostic, maxErrors int) error {
	return &diagnosticsError{rendered: renderDiagnostics(res, diags, maxErrors)}
}

// renderDiagnostics renders diags, problems in the program res holds,
// sorted by file and then by where each starts, up to the error past the
// first maxErrors when maxErrors is positive, followed, when that left
// some out, by a line saying so (see tooManyErrors). Only errors count
// toward maxErrors: the warnings among the errors shown are shown too, so
// warnings never take an error's place, and those after the last error
// shown are left out with it, as the errors after it are.
func renderDiagnostics(res *loader.Result, diags []diag.Diagnostic, maxErrors int) string {
	diags = slices.Clone(diags)
	slices.SortStableFunc(diags, func(a, b diag.Diagnostic) int {
		return cmp.Or(
			cmp.Compare(a.Span.File, b.Span.File),
			cmp.Compare(a.Span.Start, b.Span.Start),
		)
	})
	more := false
	if maxErrors > 0 {
		errs := 0
		for i, d := range diags {
			if d.Severity != diag.Error {
				continue
			}
			if errs++; errs > maxErrors {
				diags, more = diags[:i], true
				break
			}
		}
	}
	rendered := diag.RenderAllPretty(res.Files, diags)
	if more {
		rendered += tooManyErrors(maxErrors)
	}
	return rendered
}

// tooManyErrors is the line that ends the report of the first n errors
// when there were more.
func tooManyErrors(n int) string {
	return fmt.Sprintf("too many errors: showing the first %d; --max-errors 0 shows them all\n", n)
}

// loadedDump returns the dump d, --lex or --ast, of what res, loaded to
// that stage, holds: a single file's tokens or tree as they are, several
// files' tokens each under a header, and several files' trees as a list.
func loadedDump(res *loader.Result, single bool, d dump) ([]byte, error) {
	switch {
	case d == dumpLex:
		return []byte(formatFileTokens(res, single)), nil
	case single:
		return marshalTree(res.Trees[0])
	default:
		return marshalTree(res.Trees)
	}
}

// resolvedDump returns the --res dump of prog: its one file's tree when it
// is a single file, the whole program otherwise.
func resolvedDump(prog *ast.Program, single bool) ([]byte, error) {
	if single {
		return marshalTree(prog.Packages[0].Files[0])
	}
	return marshalTree(prog)
}

// marshalTree renders v, a syntax tree or several, as indented JSON, the
// same rendering as ast.File.String. It marshals directly rather than
// calling String so that a marshaling failure is an error, reported as
// such, rather than text printed as if it were the dump.
func marshalTree(v any) ([]byte, error) {
	js, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to render syntax tree: %w", err)
	}
	return js, nil
}

// formatFileTokens renders the tokens of every file res holds: a single
// file's alone, as formatTokens does, and each of several after a header
// line naming its file, -- name --.
func formatFileTokens(res *loader.Result, single bool) string {
	files := res.Sources
	if single {
		return formatTokens(res.Tokens[0])
	}
	var b strings.Builder
	for i, f := range files {
		fmt.Fprintf(&b, "-- %s --\n", f.Filename)
		b.WriteString(formatTokens(res.Tokens[i]))
	}
	return b.String()
}

// formatTokens renders tokens one per line, with every offset zero-padded to
// the width of the largest one so the columns line up. The largest offset is
// the end of the last token, since tokens are in source order. An empty
// slice renders as the empty string.
func formatTokens(tokens []lexer.Token) string {
	if len(tokens) == 0 {
		return ""
	}
	width := len(strconv.Itoa(max(tokens[len(tokens)-1].OffsetEnd(), 0)))

	var b strings.Builder
	for i := range tokens {
		b.WriteString(tokens[i].StringPadded(width))
		b.WriteByte('\n')
	}
	return b.String()
}

// withTrailingNewline returns b as a string ending in a newline, adding one
// only if b is non-empty and lacks it, so a dump ends cleanly on a terminal
// without doubling the newline generated code already ends with.
func withTrailingNewline(b []byte) string {
	s := string(b)
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}

// writeAll writes s to w, reporting a failed or short write.
func writeAll(w io.Writer, s string) error {
	if _, err := io.WriteString(w, s); err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}
	return nil
}
