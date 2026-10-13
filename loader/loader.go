package loader

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/tacklang/tack/ast"
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

// mainPath is the path of the package of a program built from a single
// file.
const mainPath = "main"

// mainPackage is the name of the package a program is built from.
const mainPackage = "main"

// Loader reads the files of a program and parses them. Its fields are
// set by the caller; the zero value is not usable, since FS is required.
type Loader struct {
	// FS is where files are read from.
	FS FS

	// Display returns the name a diagnostic shows for the file at name,
	// which is also the file's source.FileID. Nil shows name as it is.
	Display func(name string) string

	// Stop, if StageLex or StageParse, stops after that stage, having
	// read only the target's own files, for a dump of the tokens or the
	// syntax trees.
	Stop Stage
}

// Result is a loaded program, or the diagnostics that stopped it loading.
type Result struct {
	// Program is the program loaded. It is nil when Diags is not empty,
	// and when loading stopped early at Stop.
	Program *ast.Program

	// Files holds every file read, for rendering diagnostics.
	Files *source.Set

	// Sources holds the target's own Tack files when loading stopped at
	// StageLex or StageParse, and Tokens and Trees what was asked for, in
	// the same order: each file's tokens when loading stopped at StageLex,
	// and its syntax tree when it stopped at StageParse.
	Sources []*source.File
	Tokens  [][]lexer.Token
	Trees   []*ast.File

	// Stage is the stage that reported Diags, empty when there are none.
	Stage Stage

	// Diags are the problems found in the source. Loading stops at the
	// first stage that reports any.
	Diags []diag.Diagnostic
}

// File loads the program the one file name makes on its own: a package
// whose path is main. A problem reading it is returned as an error.
func (l *Loader) File(name string) (*Result, error) {
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
	pkg.Path = mainPath
	res.Program = &ast.Program{Packages: []*ast.Package{pkg}}
	return res, nil
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
	return l.loadMain(res, dir, mod.pathOf(dir))
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
	if _, err := l.FS.ReadDir(dir); err != nil {
		return nil, fmt.Errorf("cannot find package %s: no directory %s in module %s",
			show(importPath), show(importPath), mod.name)
	}
	return l.loadMain(res, dir, importPath)
}

// loadMain loads the package in dir, whose import path is importPath, as
// the main package of a program, into res.
func (l *Loader) loadMain(res *Result, dir, importPath string) (*Result, error) {
	files, err := l.readPackageFiles(res, dir)
	if err != nil {
		return nil, err
	}
	pkg, ok := l.parsePackage(res, files)
	if !ok {
		return res, nil
	}
	if !l.checkClauses(res, dir, importPath, pkg) {
		return res, nil
	}
	if pkg.Name != mainPackage {
		res.Stage = StageLoad
		res.Diags = []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("%s is not a main package", importPath),
			Span:     pkg.Files[0].PackageSpan,
		}}
		return res, nil
	}
	pkg.Path = importPath
	res.Program = &ast.Program{Packages: []*ast.Package{pkg}}
	return res, nil
}

// readPackageFiles reads every Tack file directly in dir, in the byte order
// of their names, and adds each to res. A name starting with a dot is left
// out, as an editor's or a tool's, and so is a directory.
func (l *Loader) readPackageFiles(res *Result, dir string) ([]*source.File, error) {
	entries, err := l.FS.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read directory %s: %w", l.display(dir), err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, sourceExt) {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no %s files in %s", sourceExt, l.display(dir))
	}
	sort.Strings(names)

	files := make([]*source.File, 0, len(names))
	for _, name := range names {
		full := path.Join(dir, name)
		data, err := l.FS.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("failed to open file: %w", err)
		}
		files = append(files, l.addFile(res, full, data))
	}
	return files, nil
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
// parsed, so one run reports the problems of every file at the first stage
// that has any; it then records them in res and returns false. It also
// returns false, having recorded what was asked for, when l.Stop says to
// stop after lexing or parsing.
func (l *Loader) parsePackage(res *Result, files []*source.File) (*ast.Package, bool) {
	tokens := make([][]lexer.Token, len(files))
	var diags []diag.Diagnostic
	for i, f := range files {
		lex := lexer.New(f)
		diags = append(diags, lex.Lex()...)
		tokens[i] = lex.Tokens()
	}
	if len(diags) > 0 {
		res.Stage, res.Diags = StageLex, diags
		return nil, false
	}
	if l.Stop == StageLex {
		res.Sources, res.Tokens = files, tokens
		return nil, false
	}

	pkg := &ast.Package{Files: make([]*ast.File, 0, len(files))}
	for i, f := range files {
		tree, fileDiags := parser.NewParser(f.ID, tokens[i]).Parse()
		diags = append(diags, fileDiags...)
		if tree != nil {
			pkg.Files = append(pkg.Files, tree)
		}
	}
	if len(diags) > 0 {
		res.Stage, res.Diags = StageParse, diags
		return nil, false
	}
	if l.Stop == StageParse {
		res.Sources, res.Trees = files, pkg.Files
		return nil, false
	}
	pkg.Name = pkg.Files[0].Package
	return pkg, true
}

// checkClauses reports, in res, files of the directory dir, the package
// whose import path is importPath, whose package clauses are wrong, and
// returns whether they are all right. A package's name is the last part of
// its import path, and every file's clause names it, or main, which may be
// any directory's: a clause naming anything else does not match its
// directory, and both names in one directory are two packages. A name
// other than main must be a valid package name.
func (l *Loader) checkClauses(res *Result, dir, importPath string, pkg *ast.Package) bool {
	want := path.Base(importPath)
	var diags []diag.Diagnostic
	for _, f := range pkg.Files {
		if f.Package == mainPackage || f.Package == want {
			continue
		}
		if msg := checkPackageName(want); msg != "" {
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
		if msg := checkPackageName(want); msg != "" {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error, Message: msg, Span: pkg.Files[0].PackageSpan,
			})
		}
	}
	if len(diags) > 0 {
		res.Stage, res.Diags = StageLoad, diags
		return false
	}
	return true
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
