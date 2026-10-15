package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/loader"
)

// headerName is the name tack.h is written under, in the build directory
// and by --emit-header.
const headerName = "tack.h"

// builder turns a compiled program into a binary: it compiles the C files
// of the program's packages, checks they define the program's extern funcs,
// compiles std's C files, or takes their objects from the build cache, and
// picks the ones the program needs, and links them with the generated
// source.
type builder struct {
	ctx      context.Context
	backend  codegen.Backend
	stdout   io.Writer      // where the toolchain's output goes
	stderr   io.Writer      // likewise
	res      *loader.Result // the program, and the files its diagnostics render against
	std      *std           // the standard library the program was compiled with
	env      environment    // where the build cache is, and how to identify the toolchain
	opt      codegen.Opt    // the optimization level every C file is compiled at
	maxDiags int
}

// cObject is a C file of the program, or of std, and the object file it
// compiled to.
type cObject struct {
	pkg   *ast.Package // the package whose directory holds the file, nil for std's
	shown string       // the file's name as messages show it
	obj   string       // the object's path
}

// build writes the generated source, out, to a fresh temporary directory,
// compiles the program's C files and std's there, std's only when the
// build cache does not hold their objects already, checks they define every
// extern func and every builtin the program calls, and links the
// program's objects and the std objects it needs into outBin. The toolchain's own output is
// passed through to stdout and stderr. The directory is removed on every
// path, including cancellation of ctx, which kills the toolchain.
func (b *builder) build(out []byte, outBin string) (err error) {
	dir, err := os.MkdirTemp("", "tk-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to delete temp directory: %w", rmErr))
		}
	}()

	// Every path handed to the toolchain is absolute, so none, not even a
	// C file named -fplugin=x.c or an --out starting with -, can be read as
	// an option.
	if dir, err = filepath.Abs(dir); err != nil {
		return fmt.Errorf("cannot find the temp directory: %w", err)
	}
	if outBin, err = filepath.Abs(outBin); err != nil {
		return fmt.Errorf("cannot find --out %s: %w", outBin, err)
	}
	src := filepath.Join(dir, "program"+b.backend.Ext())
	if err = writeFile(src, out); err != nil {
		return err
	}
	objs, err := b.compileC(dir)
	if err != nil {
		return err
	}
	stdObjs, err := b.linkStd(dir)
	if err != nil {
		return err
	}
	objs = append(objs, stdObjs...)
	if err = b.checkDefinitions(objs); err != nil {
		return err
	}
	paths := make([]string, len(objs))
	for i, o := range objs {
		paths[i] = o.obj
	}
	return b.runTool(b.backend.BuildCmd(src, b.std.includeDir(), paths, outBin, b.opt), "")
}

// compileC writes tack.h to dir and compiles every C file of every package
// of the program against it, in package order and then name order, each
// to an object in dir. It does nothing when the program has no C files.
func (b *builder) compileC(dir string) ([]cObject, error) {
	files := cFiles(b.res.Program)
	if len(files) == 0 {
		return nil, nil
	}
	header := filepath.Join(dir, headerName)
	if err := writeFile(header, b.backend.Header(b.res.Program)); err != nil {
		return nil, err
	}
	show := shower()
	objs := make([]cObject, len(files))
	for i, f := range files {
		obj := filepath.Join(dir, fmt.Sprintf("c%d.o", i))
		// The name tack's messages show is relative to the current
		// directory when the file is below it; clang is given it absolute.
		shown := filepath.FromSlash(show(f.path))
		src, err := filepath.Abs(filepath.FromSlash(f.path))
		if err != nil {
			return nil, fmt.Errorf("cannot find %s: %w", shown, err)
		}
		if err := b.runTool(
			b.backend.CompileCmd(src, header, b.std.includeDir(), obj, b.opt),
			shown,
		); err != nil {
			return nil, err
		}
		objs[i] = cObject{pkg: f.pkg, shown: shown, obj: obj}
	}
	return objs, nil
}

// shower returns the function that gives the name messages show for a C
// file: relative to the current directory when the file is below it, as
// clang's messages then show it, and as it is otherwise.
func shower() func(string) string {
	if cwd, err := os.Getwd(); err == nil {
		return relativeTo(cwd)
	}
	return func(name string) string { return name }
}

// cFile is one C file of a program: its slash-separated path, and the
// package whose directory holds it.
type cFile struct {
	pkg  *ast.Package
	path string
}

// cFiles returns every C file of every package of prog, in package order
// and then name order.
func cFiles(prog *ast.Program) []cFile {
	var files []cFile
	for _, pkg := range prog.Packages {
		for _, p := range pkg.CFiles {
			files = append(files, cFile{pkg: pkg, path: p})
		}
	}
	return files
}

// runTool runs argv, a toolchain command, passing its output through. A
// failure names the program and, for a C file of the program's, file, the
// file; the toolchain has already written its own errors to stderr.
func (b *builder) runTool(argv []string, file string) error {
	// The command is the backend's own toolchain invocation; only the paths
	// come from the user, so G204 does not apply.
	cmd := exec.CommandContext(b.ctx, argv[0], argv[1:]...) //nolint:gosec // see above
	cmd.Stdout = b.stdout
	cmd.Stderr = b.stderr
	if err := cmd.Run(); err != nil {
		if file != "" {
			return fmt.Errorf("cannot compile %s: %s failed: %w", file, argv[0], err)
		}
		return fmt.Errorf("%s failed: %w", argv[0], err)
	}
	return nil
}

// checkDefinitions reads the symbols each of objs, the program's and the
// std objects it links, defines and reports, as an error, a C file that
// defines main, which the generated source does, and two C files that
// define one symbol, std's included; and then, as diagnostics at their
// declarations, every extern func whose C name no C file of its own
// package defines.
func (b *builder) checkDefinitions(objs []cObject) error {
	defined, err := definitions(objs)
	if err != nil {
		return err
	}
	var diags []diag.Diagnostic
	for _, pkg := range b.res.Program.Packages {
		for _, decl := range pkg.Decls() {
			if fn, ok := decl.(*ast.Func); ok && fn.Extern && !defined[pkg][fn.CName] {
				diags = append(diags, diag.Diagnostic{
					Severity: diag.Error,
					Message: fmt.Sprintf("extern func %s has no C definition in package %s",
						fn.Name, pkg.Name),
					Span: fn.NameSpan,
				})
			}
		}
	}
	if len(diags) > 0 {
		return diagnostics(b.res, diags, b.maxDiags)
	}
	return nil
}

// definitions returns, for each package, the symbols the objects of its C
// files, among objs, define. It returns an error if a C file defines main,
// or two define one symbol.
func definitions(objs []cObject) (map[*ast.Package]map[string]bool, error) {
	defined := make(map[*ast.Package]map[string]bool)
	first := make(map[string]string) // the file that first defines each symbol
	for _, o := range objs {
		syms, err := readSymbols(o.obj)
		if err != nil {
			return nil, fmt.Errorf("cannot read what %s defines: %w", o.shown, err)
		}
		if defined[o.pkg] == nil {
			defined[o.pkg] = make(map[string]bool)
		}
		for _, sym := range syms.defined {
			if err := checkSymbol(sym, o.shown, first); err != nil {
				return nil, err
			}
			defined[o.pkg][sym] = true
		}
	}
	return defined, nil
}

// checkSymbol records sym, a symbol the C file shown defines, in first,
// and returns an error if the file may not define it: it is main, or
// another file defined it already.
func checkSymbol(sym, shown string, first map[string]string) error {
	if sym == "main" {
		return fmt.Errorf(
			"%s defines main; a Tack program's main is its Tack main, so no C file may define one",
			shown)
	}
	if prev, dup := first[sym]; dup {
		return fmt.Errorf("two C files define %s: %s and %s", sym, prev, shown)
	}
	first[sym] = shown
	return nil
}

// writeHeaders writes header, prog's tack.h, into the directory of prog's
// main package and of every other package of its module with C files or
// extern funcs, for an editor to find beside the C files that include it.
// A tack.h already there is replaced. A package of another module, such
// as one of std, is not the program's to write into.
func writeHeaders(prog *ast.Program, header []byte) error {
	main := prog.Main()
	for _, pkg := range prog.Packages {
		if pkg != main &&
			(pkg.Module != main.Module || len(pkg.CFiles) == 0 && !hasExterns(pkg)) {
			continue
		}
		name := filepath.Join(filepath.FromSlash(pkg.Dir), headerName)
		// The header is source for an editor to read, like the C files
		// beside it, so it is readable by all, as they are.
		if err := os.WriteFile(name, header, 0o644); err != nil { //nolint:gosec // see above
			return fmt.Errorf("cannot write %s: %w", name, err)
		}
	}
	return nil
}

// hasExterns reports whether pkg declares an extern func.
func hasExterns(pkg *ast.Package) bool {
	for _, decl := range pkg.Decls() {
		if fn, ok := decl.(*ast.Func); ok && fn.Extern {
			return true
		}
	}
	return false
}

// writeFile writes data to the file at name, creating it.
func writeFile(name string, data []byte) error {
	// name is always in the build's own temporary directory.
	if err := os.WriteFile(name, data, 0o600); err != nil { //nolint:gosec // see above
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	return nil
}
