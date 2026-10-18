package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/codegen"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/typeck"
)

// executeCheck carries out --check, in the environment env: it runs the
// whole pipeline on the target, through code generation, without building
// or running any toolchain, and reports every diagnostic it finds; with a
// dump flag, it also prints that dump, or writes tack.h for
// --emit-header, when the stages up to it found no error. The dump goes
// to stdout and the diagnostics to stderr, both when there is a dump and
// diagnostics from a later stage, and any error makes the run fail; a
// warning is written and does not.
//
// A directory or import path holding a package other than main, or no
// package, is checked as a library (see loader.Loader.Lib), and a single
// file whose package is not main as a library of one package. A main
// package with no main function is reported, and checked on all the
// same. The generated code and tack.h exist only for a program or
// library with no error.
func executeCheck(opts options, env environment, stdout, stderr io.Writer) error {
	// checkCombination has already confirmed the backend exists.
	backend, _ := codegen.Get(opts.backendName)
	lib, err := findStd(env)
	if err != nil {
		return err
	}

	if opts.dump == dumpNone {
		if doc, docErr := checkBuiltinDoc(opts.file, lib); doc {
			return docErr
		}
	}

	out, err := loadedCheckDump(opts, lib)
	if err != nil {
		return withDump(out, err, stdout)
	}

	res, single, err := loadCheck(opts.file, "", lib, opts.maxErrors)
	if err != nil {
		return err
	}
	staged, diags, err := checkProgram(res, single, backend, lib.builtinSet(), opts.dump)
	if err != nil {
		return err
	}
	if staged != nil {
		out = staged
	}
	if err := writeCheckOutput(opts.dump, res.Program, backend, out, diags, stdout); err != nil {
		return err
	}
	if diag.HasErrors(diags) {
		return diagnostics(res, diags, opts.maxErrors)
	}
	return writeWarnings(stderr, res, diags, opts.maxErrors)
}

// writeCheckOutput writes what --check with the dump d produced for prog,
// checked by backend: out, the dump's output, to stdout, when there is
// one, or tack.h into the package directories for --emit-header, when the
// check found no error among diags.
func writeCheckOutput(
	d dump,
	prog *ast.Program,
	backend codegen.Backend,
	out []byte,
	diags []diag.Diagnostic,
	stdout io.Writer,
) error {
	switch {
	case d == dumpHeader && !diag.HasErrors(diags):
		return writeHeaders(prog, backend.Header(prog))
	case out != nil:
		return writeAll(stdout, withTrailingNewline(out))
	}
	return nil
}

// withDump returns err, which stopped a run with --check, having written
// out, the dump of the trees the parser recovered, to stdout first when
// err is the syntax errors they were recovered from.
func withDump(out []byte, err error, stdout io.Writer) error {
	var diagErr *diagnosticsError
	if out != nil && errors.As(err, &diagErr) {
		if werr := writeAll(stdout, withTrailingNewline(out)); werr != nil {
			return werr
		}
	}
	return err
}

// loadedCheckDump returns the --lex or --ast dump opts asks for with
// --check, taken from a load that stops after it, as the dump is without
// --check, or nil for any other dump; the program is then loaded again in
// full to be checked. The load's diagnostics are returned as a
// *diagnosticsError, with the dump of the trees the parser recovered
// from syntax errors, bad nodes and all, when they are --ast's; a lex
// error leaves no dump.
func loadedCheckDump(opts options, lib *std) ([]byte, error) {
	stop := stopFor(opts.dump)
	if stop == "" {
		return nil, nil
	}
	res, single, err := loadCheck(opts.file, stop, lib, opts.maxErrors)
	if err != nil {
		return nil, err
	}
	if len(res.Diags) > 0 {
		var out []byte
		if res.Stage == loader.StageParse && opts.dump == dumpAST && len(res.Trees) > 0 {
			if out, err = loadedDump(res, single, opts.dump); err != nil {
				return nil, err
			}
		}
		return out, diagnostics(res, res.Diags, opts.maxErrors)
	}
	return loadedDump(res, single, opts.dump)
}

// loadCheck loads target as load does for a run with --check: a directory
// with no main package as a library, stopping after the stage stop when
// it is set, with a file's parse going past at most maxErrors syntax
// errors when maxErrors is positive. It reports whether target is a
// single file.
func loadCheck(
	target string,
	stop loader.Stage,
	lib *std,
	maxErrors int,
) (*loader.Result, bool, error) {
	l := newLoader(lib, maxErrors)
	l.Stop, l.Lib = stop, true
	return loadWith(l, target)
}

// checkProgram runs the pipeline on res, a program or library loaded in
// full, with builtins, through code generation, and returns the
// diagnostics it found with the output of d, the dump asked for, when
// the stages up to it found no error: the resolved tree for --res, the
// generated code for --emit, nil otherwise and for every other dump,
// which executeCheck takes care of. single says the program is a single
// file. As without --check, every stage runs whatever an earlier one
// reported, unless the loader built no program. A main package with no
// main function is reported too, which no stage depends on: it is not
// counted on the program as an error, so code generation still runs and
// reports what it finds when no other error was, but the generated code is
// dropped, since a program with any error has none. Any other error keeps
// code generation from running at all (see codegen.Refuse). Warnings stop
// nothing, and are returned with the rest.
func checkProgram(
	res *loader.Result,
	single bool,
	backend codegen.Backend,
	builtins *builtin.Set,
	d dump,
) ([]byte, []diag.Diagnostic, error) {
	diags := slices.Clone(res.Diags)
	prog := res.Program
	if prog == nil {
		return nil, diags, nil
	}
	missing := resolver.MissingMain(prog)

	r := resolver.New(prog, builtins)
	r.MainOptional = true
	diags = append(diags, r.Resolve()...)
	var out []byte
	if d == dumpRes && !diag.HasErrors(diags) {
		var err error
		if out, err = resolvedDump(prog, single); err != nil {
			return nil, nil, err
		}
	}

	diags = append(diags, typeck.New(prog, res.Files, builtins).Check()...)
	if diag.HasErrors(diags) {
		return out, append(missing, diags...), nil
	}
	generated, genDiags := backend.Generate(prog, builtins)
	switch {
	case len(genDiags) > 0 || len(missing) > 0:
		return out, append(append(missing, genDiags...), diags...), nil
	case d == dumpEmit:
		out = generated
	}
	return out, diags, nil
}

// isLibrary reports whether prog is a library, which --check loads: its
// last package, the one a program is built from, is not a main package.
func isLibrary(prog *ast.Program) bool {
	return prog.Main().Name != "main"
}

// builtinDocName is the name of the copy of the builtins' documentation
// in std/builtin.
const builtinDocName = "builtin.tck"

// checkBuiltinDoc checks target when it is std/builtin/builtin.tck, or
// the directory std/builtin, and reports whether it was: that file is a
// copy of the documentation of the builtins the compiler builds in (see
// builtin.Source), written in forms only the compiler's builtin mode
// parses, so it is never checked as code. It is clean when it matches
// the compiler's own, and an error saying how to refresh it when it does
// not. Any other target, another .tck file in std/builtin among them, is
// left to the loader.
func checkBuiltinDoc(target string, lib *std) (bool, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return false, nil //nolint:nilerr // the loader reports the target
	}
	info, err := os.Stat(abs)
	if err != nil {
		return false, nil //nolint:nilerr // the loader reports the target
	}
	file, dir := filepath.Join(abs, builtinDocName), abs
	if !info.IsDir() {
		file, dir = abs, filepath.Dir(abs)
		if filepath.Base(abs) != builtinDocName {
			return false, nil
		}
	}
	if !newLoader(lib, 0).IsStdBuiltinDir(filepath.ToSlash(dir)) {
		return false, nil
	}
	data, err := os.ReadFile(file) //nolint:gosec // G304: the target the user named
	if err != nil {
		return false, nil //nolint:nilerr // no copy: the loader reports the directory
	}
	return true, builtin.CopyError(target, data) //nolint:wrapcheck // the message is the driver's
}
