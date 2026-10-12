package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"uuid"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen"
	_ "github.com/tacklang/tack/codegen/c" // backends register from init; importing plugs one in
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
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
	dumpNone dump = iota // build a binary
	dumpLex              // --lex: the token stream
	dumpAST              // --ast: the parsed syntax tree
	dumpRes              // --res: the resolved syntax tree
	dumpEmit             // --emit: the generated target source
)

// options is the validated command line.
type options struct {
	file         string
	backendName  string
	outBin       string
	maxDiags     int
	dump         dump
	listBackends bool
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

// main runs the compiler with the process's arguments and standard streams
// and exits with the code run returns. An interrupt cancels the run so that
// temporary files are still removed; a second interrupt kills the process.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run is the whole driver: it parses args, compiles the named file, and
// either prints the requested intermediate form to stdout or builds a
// binary. Diagnostics and errors go to stderr. It returns the process exit
// code (exitOK, exitFailure, or exitUsage) and never exits itself, which is
// what lets tests call it in process.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs, values := newFlagSet()
	opts, err := parseArgs(fs, values, args)
	if errors.Is(err, flag.ErrHelp) {
		printUsage(fs, stdout)
		return exitOK
	}
	if err == nil {
		err = execute(ctx, opts, stdout, stderr)
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
	backendName  string
	outBin       string
	maxDiags     int
	lex          bool
	ast          bool
	res          bool
	emit         bool
	listBackends bool
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
	fs.StringVar(&v.backendName, "backend", defaultBackend,
		"use the code generation backend `name`")
	fs.BoolVar(&v.listBackends, "backends", false,
		"list the available code generation backends and stop")
	fs.StringVar(&v.outBin, "out", "",
		"write the linked binary to `path` (required unless a dump flag is given)")
	fs.IntVar(&v.maxDiags, "max-diags", 0,
		"print at most `n` diagnostics; 0 prints them all")
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
		maxDiags:     v.maxDiags,
		listBackends: v.listBackends,
	}
	if opts.listBackends {
		// Listing backends needs nothing else from the command line.
		return opts, nil
	}

	var err error
	if opts.dump, err = dumpOf(*v); err != nil {
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
	case opts.maxDiags < 0:
		return usagef("--max-diags must be 0 or more, got %d", opts.maxDiags)
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
	b.WriteString("usage: tack --out <path> [flags] <file>\n")
	b.WriteString("       tack (--lex | --ast | --res | --emit) [flags] <file>\n")
	b.WriteString("       tack --backends\n\nFlags:\n")
	fs.VisitAll(func(f *flag.Flag) {
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
	b.WriteString("  -h, --help\n        print this help and exit\n\n")
	b.WriteString("Exit status: 0 on success, 1 if the program has errors or the build\n")
	b.WriteString("fails, 2 if the command line is wrong.\n")
	// Help that cannot be written has no one to report the failure to.
	_, _ = io.WriteString(w, b.String())
}

// execute carries out a validated command line.
func execute(ctx context.Context, opts options, stdout, stderr io.Writer) error {
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

	srcFile, err := readSource(opts.file)
	if err != nil {
		return err
	}

	out, err := compile(srcFile, backend, opts.dump, opts.maxDiags)
	if err != nil {
		return err
	}

	if opts.dump != dumpNone {
		return writeAll(stdout, withTrailingNewline(out))
	}
	return build(ctx, backend, out, opts.outBin, stdout, stderr)
}

// readSource reads the named file into a source.File.
func readSource(fname string) (*source.File, error) {
	// Reading the file the user named is the point of a compiler.
	content, err := os.ReadFile(fname) //nolint:gosec // G304: path comes from the user by design
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	return &source.File{
		ID:       source.FileID(uuid.New().String()),
		Filename: fname,
		Data:     content,
	}, nil
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

// compile runs the pipeline from lexing up to the stage d names, or through
// code generation when d is dumpNone, and returns what that stage produced:
// the formatted dump for a dump, the generated source otherwise. The first
// stage to report diagnostics ends the run with a *diagnosticsError holding
// at most maxDiags of them when maxDiags is positive.
func compile(srcFile *source.File, backend codegen.Backend, d dump, maxDiags int) ([]byte, error) {
	fail := func(diags []diag.Diagnostic) error {
		if maxDiags > 0 && len(diags) > maxDiags {
			diags = diags[:maxDiags]
		}
		return &diagnosticsError{rendered: diag.RenderAllPretty(srcFile, diags)}
	}

	lex := lexer.New(srcFile)
	if diags := lex.Lex(); len(diags) > 0 {
		return nil, fail(diags)
	}
	if d == dumpLex {
		return []byte(formatTokens(lex.Tokens())), nil
	}

	file, err := parseAndResolve(srcFile.ID, lex.Tokens(), d, fail)
	if err != nil {
		return nil, err
	}
	if d == dumpAST || d == dumpRes {
		return marshalTree(file)
	}

	if diags := typeck.New(file).Check(); len(diags) > 0 {
		return nil, fail(diags)
	}
	out, genDiags := backend.Generate(file)
	if len(genDiags) > 0 {
		return nil, fail(genDiags)
	}
	return out, nil
}

// parseAndResolve parses tokens into a syntax tree and, unless d is dumpAST,
// resolves it. Diagnostics from either stage are turned into the returned
// error by fail.
func parseAndResolve(
	id source.FileID,
	tokens []lexer.Token,
	d dump,
	fail func([]diag.Diagnostic) error,
) (*ast.File, error) {
	file, diags := parser.NewParser(id, tokens).Parse()
	if len(diags) > 0 {
		return nil, fail(diags)
	}
	if d == dumpAST {
		return file, nil
	}
	if diags := resolver.New(file).Resolve(); len(diags) > 0 {
		return nil, fail(diags)
	}
	return file, nil
}

// marshalTree renders file as indented JSON, the same rendering as
// ast.File.String. It marshals directly rather than calling String because
// String discards the marshaling error and returns "", which would make a
// failed dump look like a successful empty one.
func marshalTree(file *ast.File) ([]byte, error) {
	js, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to render syntax tree: %w", err)
	}
	return js, nil
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

// build writes the generated source to a temporary file and runs the
// backend's build command on it to produce outBin. The toolchain's own
// output is passed through to stdout and stderr. The temporary file is
// removed on every path, including cancellation of ctx, which kills the
// toolchain.
func build(
	ctx context.Context,
	backend codegen.Backend,
	out []byte,
	outBin string,
	stdout, stderr io.Writer,
) (err error) {
	tmpFile, err := os.CreateTemp("", "tk-*"+backend.Ext())
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		if rmErr := os.Remove(tmpName); rmErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to delete temp file: %w", rmErr))
		}
	}()

	_, writeErr := tmpFile.Write(out)
	closeErr := tmpFile.Close()
	if writeErr != nil {
		return fmt.Errorf("failed to write temp file: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close temp file: %w", closeErr)
	}

	argv := backend.BuildCmd(tmpName, outBin)
	// The command is the backend's own toolchain invocation; only the paths
	// come from the user, so G204 does not apply.
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // see above
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		// The toolchain has already written its own errors to stderr.
		return fmt.Errorf("%s failed: %w", argv[0], err)
	}
	return nil
}
