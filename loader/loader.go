package loader

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
)

// Stage names the part of loading that reported a Result's diagnostics.
type Stage string

// The stages of loading, in the order they run.
const (
	// StageLex is lexing a file.
	StageLex Stage = "lex"
	// StageParse is parsing a file.
	StageParse Stage = "parse"
	// StageLoad is putting files together into packages, and packages
	// into a program.
	StageLoad Stage = "load"
)

// sourceExt is the extension of a Tack source file.
const sourceExt = ".tck"

// testSuffix ends the name of a test file, a Tack file a build never
// loads (see IsTestFile).
const testSuffix = "_test" + sourceExt

// cExt is the extension of a C source file, which defines extern funcs.
const cExt = ".c"

// mainPath is the path of the package of a program built from a single
// file.
const mainPath = "main"

// mainPackage is the name of the package a program is built from.
const mainPackage = "main"

// errNoTackFiles is wrapped by the error for a package directory that
// holds no Tack file, test files left out.
var errNoTackFiles = errors.New("no " + sourceExt + " files")

// IsTestFile reports whether the file named name, a base name, is a test
// file: its name ends in _test.tck, matched byte for byte, so _test.tck
// itself is one and X_TEST.tck is not. No build loads a test file, as
// part of a package or on its own. The driver asks it of a file target
// before anything else looks at the file.
func IsTestFile(name string) bool {
	return strings.HasSuffix(name, testSuffix)
}

// isPackageFile reports whether the file named name, a base name, of a
// directory entry that is not itself a directory, is one of its
// package's Tack files: its name ends in .tck, does not start with a dot,
// as an editor's or a tool's, and is not a test file.
func isPackageFile(name string) bool {
	return strings.HasSuffix(name, sourceExt) && !strings.HasPrefix(name, ".") &&
		!IsTestFile(name)
}

// Loader reads the files of a program and parses them. Its fields are
// set by the caller; the zero value is not usable, since FS is required.
type Loader struct {
	// FS is where files are read from.
	FS FS

	// Display returns the name a diagnostic shows for the file at name,
	// which is also the file's source.FileID. Nil shows name as it is.
	Display func(name string) string

	// Builtins are the builtins of the program, whose names no package
	// may have. Loading that stops at Stop never needs them; nil has only
	// the generic ones, such as len.
	Builtins *builtin.Set

	// Std is where the standard library is, the module std, which an
	// import path starting with std names. Nil means there is none, and
	// importing from it is an error.
	Std *Std

	// Stop, if StageLex or StageParse, stops after that stage, having
	// read only the target's own files, for a dump of the tokens or the
	// syntax trees.
	Stop Stage

	// MaxErrors, when positive, is how many syntax errors a file's parse
	// goes past before it stops (see parser.Parser.Limit).
	MaxErrors int

	// Lib, if set, makes Dir and Path load a directory that holds no
	// main package as a library instead of rejecting it: every package
	// at or below it in its module that is not a main package, with
	// everything they import, in one Program with no main package (see
	// loadLib). A directory holding a main package loads as it does
	// without Lib. The driver sets it for --check.
	Lib bool

	// TestMain, if set, makes Dir and Path load the package in the
	// directory for its tests, tack test's mode, whatever its name: its
	// test files are read with its other files, in the byte order of
	// all their names, each marked as one (see ast.File.Test), and
	// TestMain is called with the package parsed, its path set, and
	// every file read so far, to give one file more: the main that runs
	// the tests, its name, which is its source.FileID and must be no
	// file's on disk, and its source. That file is parsed into the
	// package, its last file, before anything the package imports is
	// loaded, so what it imports is loaded too, and no other package's
	// test files are read. The package becomes the program's last, as a
	// main package is, though it may be any. TestMain is not called when
	// the package's files do not parse into a whole package. Lib is
	// ignored then.
	TestMain func(pkg *ast.Package, files source.Files) (name string, src []byte)
}

// Std is where a Loader finds the standard library: the directory Dir, the
// root of the module std, read through FS.
type Std struct {
	// FS is what std's files are read through. Nil means the Loader's
	// own FS, which is also what lets a directory to build be in std: std
	// is recognized as the module of a directory only when it is read
	// through the same FS.
	FS FS

	// Dir is the slash-separated path of std's root directory in FS,
	// $TACK_HOME/src/std for the driver. A package's import path below
	// std is its directory's path below Dir: std/fmt is Dir/fmt.
	Dir string
}

// Result is a loaded program, or the diagnostics that stopped it loading,
// or both: a program loaded with syntax errors or imports that could not
// be loaded, with the diagnostics saying so.
type Result struct {
	// Program is the program loaded, which may hold bad nodes and imports
	// bound to no package when Diags is not empty (see Diags). It is nil
	// when loading could not go on, and when it stopped early at Stop. A
	// library's has no main package (see Loader.Lib).
	Program *ast.Program

	// Files holds every file read, for rendering diagnostics.
	Files *source.Set

	// Sources holds the target's own Tack files when loading stopped at
	// StageLex or StageParse, a library's those of every package loaded
	// as its own, and Tokens and Trees what was asked for, in
	// the same order: each file's tokens when loading stopped at StageLex,
	// and its syntax tree when it stopped at StageParse. Loading that
	// stops at StageParse keeps the trees the parser recovered, with
	// their bad nodes, when the package's files had syntax errors, along
	// with those errors in Diags; a file too deeply nested to have a tree
	// leaves its package's out.
	Sources []*source.File
	Tokens  [][]lexer.Token
	Trees   []*ast.File

	// Stage is the first stage that reported any of Diags, empty when
	// there are none.
	Stage Stage

	// Diags are the problems found in the source. Loading goes on past a
	// syntax error, which leaves a bad node in the tree (see package
	// parser), and past an import that cannot be loaded, which is bound
	// to no package, so the program is still built and later stages can
	// report their own problems with it. It stops, with no program, at a
	// file that does not lex, or that nests too deeply to be parsed, and
	// at a malformed tack.mod or a main package whose package clauses are
	// wrong, after which nothing more can be known about the program.
	Diags []diag.Diagnostic
}

// report records diags, found at stage, in res.
func (res *Result) report(stage Stage, diags []diag.Diagnostic) {
	if len(diags) == 0 {
		return
	}
	if res.Stage == "" {
		res.Stage = stage
	}
	res.Diags = append(res.Diags, diags...)
}

// File loads the program the one file name makes on its own: a package
// whose path is main. A problem reading it is returned as an error, and
// so is a name that makes it a test file, which is never loaded on its
// own, before it is read.
func (l *Loader) File(name string) (*Result, error) {
	if IsTestFile(path.Base(name)) {
		return nil, fmt.Errorf("%s is a test file, which is never loaded on its own",
			l.display(name))
	}
	data, err := l.FS.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	res := &Result{Files: source.NewSet()}
	f := l.addFile(res, name, data)
	pkg, ok := l.parsePackage(res, []*source.File{f})
	if !ok {
		return res, nil
	}
	pkg.Path, pkg.Dir = mainPath, path.Dir(name)
	if pkg.CFiles, err = l.readCFiles(pkg.Dir); err != nil {
		return nil, err
	}
	g := newGraph(l, res, nil, pkg)
	return g.finish(), nil
}

// readCFiles returns the path of every C file directly in dir, in the
// byte order of their names, leaving out a name that starts with a dot
// and a directory, as readPackageFiles does for Tack files.
func (l *Loader) readCFiles(dir string) ([]string, error) {
	entries, err := l.FS.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read directory %s: %w", l.display(dir), err)
	}
	return l.cFilesIn(dir, entries)
}

// cFilesIn returns the path of every C file among entries, the entries of
// the directory dir, in the byte order of their names. A C file whose name
// starts with - is an error: the driver hands C files to clang, which could
// take such a name for an option.
func (l *Loader) cFilesIn(dir string, entries []fs.DirEntry) ([]string, error) {
	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, cExt) {
			continue
		}
		if strings.HasPrefix(name, "-") {
			return nil, fmt.Errorf(
				"file %s in %s starts with -, which clang could read as an option; rename it",
				name,
				l.display(dir),
			)
		}
		names = append(names, path.Join(dir, name))
	}
	sort.Strings(names)
	return names, nil
}

// Dir loads the program whose main package is the directory dir: the
// package the Tack files directly in it make together, in the module
// whose root is the nearest directory at or above dir holding a tack.mod.
// A problem reading a directory or a file, a directory with no Tack files,
// and no tack.mod at or above dir are returned as errors; a malformed
// tack.mod, as diagnostics.
func (l *Loader) Dir(dir string) (*Result, error) {
	res := &Result{Files: source.NewSet()}
	mod, err := l.findModule(res, dir)
	if err != nil || mod == nil {
		return res, err
	}
	return l.loadMain(res, mod, dir, mod.pathOf(dir))
}

// Path loads the program whose main package has the import path
// importPath, in the module whose root is the nearest directory at or
// above cwd holding a tack.mod. Problems with the path are returned as
// errors, as Dir's are.
func (l *Loader) Path(cwd, importPath string) (*Result, error) {
	res := &Result{Files: source.NewSet()}
	mod, err := l.findModule(res, cwd)
	if err != nil || mod == nil {
		return res, err
	}
	if msg := checkImportPath(importPath, mod.name); msg != "" {
		return nil, fmt.Errorf("%s", msg)
	}
	dir, msg := mod.dirOf(importPath)
	if msg != "" {
		return nil, fmt.Errorf("%s", msg)
	}
	if _, err := mod.fsys.ReadDir(dir); err != nil {
		return nil, fmt.Errorf("cannot find package %s: no directory %s in module %s",
			show(importPath), show(importPath), mod.name)
	}
	return l.loadMain(res, mod, dir, importPath)
}

// loadMain loads the package in dir, whose import path is importPath in
// mod, as the main package of a program, into res, with every package it
// imports.
// With l.Lib set, a directory with no Tack files, or whose files make a
// package other than main, is loaded as a library instead (see loadLib).
func (l *Loader) loadMain(res *Result, mod *module, dir, importPath string) (*Result, error) {
	if l.TestMain != nil {
		return l.loadTests(res, mod, dir, importPath)
	}
	files, cFiles, err := l.readPackageFiles(res, mod.fsys, dir)
	if l.isLib(files, err) {
		return l.loadLib(res, mod, dir, files, cFiles)
	}
	if err != nil {
		return nil, err
	}
	pkg, ok := l.parsePackage(res, files)
	if !ok {
		return res, nil
	}
	diags := l.checkClauses(dir, importPath, pkg)
	if len(diags) == 0 && pkg.Name != mainPackage {
		diags = []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("%s is not a main package", importPath),
			Span:     pkg.Files[0].PackageSpan,
		}}
	}
	if len(diags) > 0 {
		res.report(StageLoad, diags)
		return res, nil
	}
	pkg.Path, pkg.Module, pkg.Dir, pkg.CFiles = importPath, mod.name, dir, cFiles
	g := newGraph(l, res, mod, pkg)
	return g.finish(), nil
}

// isLib reports whether a directory is to be loaded as a library: l.Lib
// is set, and reading the directory's Tack files, files, found none, err,
// or found a package other than main.
func (l *Loader) isLib(files []*source.File, err error) bool {
	if !l.Lib {
		return false
	}
	if err != nil {
		return errors.Is(err, errNoTackFiles)
	}
	name, ok := clauseName(files)
	return ok && name != mainPackage
}

// readPackageFiles reads every Tack file directly in dir, in the byte order
// of their names, through fsys, and adds each to res. A name starting with
// a dot is left out, as an editor's or a tool's, and so is a test file and
// a directory (see isPackageFile). A directory with no such file is an
// error wrapping errNoTackFiles, which says so when it holds test files.
// It also returns the path of every C file in dir, which it does not read.
func (l *Loader) readPackageFiles(
	res *Result,
	fsys FS,
	dir string,
) ([]*source.File, []string, error) {
	entries, err := fsys.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read directory %s: %w", l.display(dir), err)
	}
	names, tests := packageFileNames(entries)
	if len(names) == 0 {
		return nil, nil, l.noTackFiles(dir, tests)
	}
	return l.readNamed(res, fsys, dir, names, entries)
}

// readNamed reads the Tack files of dir named names, through fsys, in the
// byte order of their names, adding each to res, and returns them with
// the path of every C file among entries, dir's entries.
func (l *Loader) readNamed(
	res *Result,
	fsys FS,
	dir string,
	names []string,
	entries []fs.DirEntry,
) ([]*source.File, []string, error) {
	sort.Strings(names)

	files := make([]*source.File, 0, len(names))
	for _, name := range names {
		full := path.Join(dir, name)
		data, err := fsys.ReadFile(full)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open file: %w", err)
		}
		files = append(files, l.addFile(res, full, data))
	}
	cFiles, cErr := l.cFilesIn(dir, entries)
	if cErr != nil {
		return nil, nil, cErr
	}
	return files, cFiles, nil
}

// packageFileNames returns the names of the package's Tack files among
// entries, the entries of one directory, in their order (see
// isPackageFile), and whether any other entry is a test file whose name
// does not start with a dot.
func packageFileNames(entries []fs.DirEntry) ([]string, bool) {
	names := make([]string, 0, len(entries))
	tests := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if isPackageFile(name) {
			names = append(names, name)
		}
		tests = tests || (IsTestFile(name) && !strings.HasPrefix(name, "."))
	}
	return names, tests
}

// noTackFiles returns the error, wrapping errNoTackFiles, for the
// directory dir, which holds no Tack file of a package; tests says it
// holds test files, which the error then says, since a directory of
// test files would otherwise seem to hold Tack files it does not count.
func (l *Loader) noTackFiles(dir string, tests bool) error {
	if tests {
		return fmt.Errorf("%w other than test files in %s", errNoTackFiles, l.display(dir))
	}
	return fmt.Errorf("%w in %s", errNoTackFiles, l.display(dir))
}

// addFile records the file read from name, with contents data, in res and
// returns it.
func (l *Loader) addFile(res *Result, name string, data []byte) *source.File {
	shown := l.display(name)
	f := &source.File{ID: source.FileID(shown), Filename: shown, Data: data}
	res.Files.Add(f)
	return f
}

// display returns the name shown for name.
func (l *Loader) display(name string) string {
	if l.Display == nil {
		return name
	}
	return l.Display(name)
}

// parsePackage lexes and then parses files, the files of one package, and
// returns the package they make. Every file is lexed, and then every file
// parsed, so one run reports the problems of every file. A lexing error
// stops it before parsing, recorded in res, and it returns false: the
// lexer drops what it cannot read, so the tokens left would only make
// syntax errors that follow from it. Syntax errors are recorded in res
// and the package returned all the same, its files holding bad nodes
// where the errors were, unless a file nests too deeply to have a tree,
// or has a package clause that could not be parsed, which leaves the
// package unknown: either stops it as a lexing error does. It also returns false, having
// recorded what was asked for, when l.Stop says to stop after lexing or
// parsing.
func (l *Loader) parsePackage(res *Result, files []*source.File) (*ast.Package, bool) {
	tokens := make([][]lexer.Token, len(files))
	var diags []diag.Diagnostic
	for i, f := range files {
		lex := lexer.New(f)
		diags = append(diags, lex.Lex()...)
		tokens[i] = lex.Tokens()
	}
	if len(diags) > 0 {
		res.report(StageLex, diags)
		return nil, false
	}
	if l.Stop == StageLex {
		res.Sources, res.Tokens = append(res.Sources, files...), append(res.Tokens, tokens...)
		return nil, false
	}

	pkg, whole, diags := l.parseFiles(files, tokens)
	res.report(StageParse, diags)
	if l.Stop == StageParse {
		if len(pkg.Files) == len(files) {
			// The trees, bad nodes and all when the parser recovered
			// from errors, are kept for a dump that asks for them.
			res.Sources, res.Trees = append(res.Sources, files...), append(res.Trees, pkg.Files...)
		}
		return nil, false
	}
	if !whole || len(pkg.Files) == 0 {
		return nil, false
	}
	pkg.Name = pkg.Files[0].Package
	return pkg, true
}

// parseFiles parses files, the files of one package, from their tokens,
// and returns the package of the trees it got, whether it got a tree with
// a package clause for every file, and every syntax error.
func (l *Loader) parseFiles(
	files []*source.File,
	tokens [][]lexer.Token,
) (*ast.Package, bool, []diag.Diagnostic) {
	pkg := &ast.Package{Files: make([]*ast.File, 0, len(files))}
	whole := true
	var diags []diag.Diagnostic
	for i, f := range files {
		p := parser.NewParser(f.ID, tokens[i])
		p.Limit(l.MaxErrors)
		tree, fileDiags := p.Parse()
		diags = append(diags, fileDiags...)
		if tree != nil {
			pkg.Files = append(pkg.Files, tree)
		}
		whole = whole && tree != nil && tree.Package != ""
	}
	return pkg, whole, diags
}

// checkClauses reports files of the directory dir, the package whose
// import path is importPath, whose package clauses are wrong. A package's name is the last part of
// its import path, and every file's clause names it, or main, which may be
// any directory's: a clause naming anything else does not match its
// directory, and both names in one directory are two packages. A name
// other than main must be a valid package name.
func (l *Loader) checkClauses(dir, importPath string, pkg *ast.Package) []diag.Diagnostic {
	want := path.Base(importPath)
	var diags []diag.Diagnostic
	for _, f := range pkg.Files {
		if f.Package == mainPackage || f.Package == want {
			continue
		}
		if msg := checkPackageName(want, l.Builtins); msg != "" {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error, Message: msg, Span: f.PackageSpan,
			})
			break
		}
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Message: fmt.Sprintf("package %s does not match its directory; expected package %s",
				show(f.Package), want),
			Span: f.PackageSpan,
		})
	}
	if len(diags) == 0 {
		diags = l.mixedPackages(dir, pkg)
	}
	if len(diags) == 0 && pkg.Name != mainPackage {
		if msg := checkPackageName(want, l.Builtins); msg != "" {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error, Message: msg, Span: pkg.Files[0].PackageSpan,
			})
		}
	}
	return diags
}

// mixedPackages reports files of the directory dir whose package clauses
// disagree, though each is allowed there: a directory holds one package.
// The packages are named in sorted order, whatever the order of the files,
// and the error is at the first file of the second of them, with a note at
// the first file of each of the others.
func (l *Loader) mixedPackages(dir string, pkg *ast.Package) []diag.Diagnostic {
	first := make(map[string]*ast.File) // the first file declaring each package
	var names []string
	for _, f := range pkg.Files {
		if _, seen := first[f.Package]; !seen {
			first[f.Package] = f
			names = append(names, f.Package)
		}
	}
	if len(names) == 1 {
		return nil
	}
	sort.Strings(names)

	d := diag.Diagnostic{
		Severity: diag.Error,
		Message:  fmt.Sprintf("directory %s holds %s", l.display(dir), packageList(names)),
		Span:     first[names[1]].PackageSpan,
	}
	for i, name := range names {
		if i == 1 {
			continue
		}
		d.Related = append(d.Related, diag.Diagnostic{
			Severity: diag.Note,
			Message:  fmt.Sprintf("package %s is declared here", name),
			Span:     first[name].PackageSpan,
		})
	}
	return []diag.Diagnostic{d}
}

// packageList names the packages a directory holds, given in sorted
// order: "two packages, a and b", or "3 packages, a, b and c".
func packageList(names []string) string {
	count := "two"
	if len(names) > 2 {
		count = fmt.Sprintf("%d", len(names))
	}
	return fmt.Sprintf("%s packages, %s and %s",
		count, strings.Join(names[:len(names)-1], ", "), names[len(names)-1])
}
