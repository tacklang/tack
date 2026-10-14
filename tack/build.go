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
// and links them with the generated source.
type builder struct {
	ctx      context.Context
	backend  codegen.Backend
	stdout   io.Writer      // where the toolchain's output goes
	stderr   io.Writer      // likewise
	res      *loader.Result // the program, and the files its diagnostics render against
	maxDiags int
}

// cObject is a C file of the program and the object file it compiled to.
type cObject struct {
	pkg   *ast.Package // the package whose directory holds the file
	shown string       // the file's name as messages show it
	obj   string       // the object's path
}

// build writes the generated source, out, to a fresh temporary directory,
// compiles the program's C files there, checks they define every extern
// func, and links everything into outBin. The toolchain's own output is
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

	src := filepath.Join(dir, "program"+b.backend.Ext())
	if err = writeFile(src, out); err != nil {
		return err
	}
	objs, err := b.compileC(dir)
	if err != nil {
		return err
	}
	if err = b.checkDefinitions(objs); err != nil {
		return err
	}
	paths := make([]string, len(objs))
	for i, o := range objs {
		paths[i] = o.obj
	}
	return b.runTool(b.backend.BuildCmd(src, paths, outBin), "")
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
	show := func(name string) string { return name }
	if cwd, err := os.Getwd(); err == nil {
		show = relativeTo(cwd)
	}
	objs := make([]cObject, len(files))
	for i, f := range files {
		obj := filepath.Join(dir, fmt.Sprintf("c%d.o", i))
		// The name shown is relative to the current directory, where
		// clang runs, when the file is below it, so clang's own messages
		// show it that way too.
		src := filepath.FromSlash(show(f.path))
		if err := b.runTool(b.backend.CompileCmd(src, header, obj), src); err != nil {
			return nil, err
		}
		objs[i] = cObject{pkg: f.pkg, shown: src, obj: obj}
	}
	return objs, nil
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

// checkDefinitions reads the symbols each of objs defines and reports, as
// an error, a C file that defines main, which the generated source does,
// and two C files that define one symbol; and then, as diagnostics at
// their declarations, every extern func whose C name no C file of its own
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
		syms, err := definedSymbols(o.obj)
		if err != nil {
			return nil, fmt.Errorf("cannot read what %s defines: %w", o.shown, err)
		}
		if defined[o.pkg] == nil {
			defined[o.pkg] = make(map[string]bool)
		}
		for _, sym := range syms {
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
// main package and of every other package of prog with C files or extern
// funcs, for an editor to find beside the C files that include it. A
// tack.h already there is replaced.
func writeHeaders(prog *ast.Program, header []byte) error {
	for _, pkg := range prog.Packages {
		if pkg != prog.Main() && len(pkg.CFiles) == 0 && !hasExterns(pkg) {
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
	if err := os.WriteFile(name, data, 0o600); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	return nil
}
