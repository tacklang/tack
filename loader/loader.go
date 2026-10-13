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

// mainPath is the path of the main package of a program built from a
// single file or from a directory alone.
const mainPath = "main"

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

	// Tokens holds each file's tokens, in Files order, when loading
	// stopped at StageLex.
	Tokens [][]lexer.Token

	// Trees holds each file's syntax tree, in Files order, when loading
	// stopped at StageParse.
	Trees []*ast.File

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

// Dir loads the program the Tack files directly in the directory dir make
// together: one package, whose path is main. A problem reading the
// directory or a file in it, and a directory with no Tack files, are
// returned as errors.
func (l *Loader) Dir(dir string) (*Result, error) {
	res := &Result{Files: source.NewSet()}
	files, err := l.readPackageFiles(res, dir)
	if err != nil {
		return nil, err
	}
	pkg, ok := l.parsePackage(res, files)
	if !ok {
		return res, nil
	}
	if !l.checkClauses(res, dir, pkg) {
		return res, nil
	}
	pkg.Path = mainPath
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
		res.Tokens = tokens
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
		res.Trees = pkg.Files
		return nil, false
	}
	pkg.Name = pkg.Files[0].Package
	return pkg, true
}

// checkClauses reports, in res, files of the directory dir whose package
// clauses disagree: a directory holds one package. The packages are named
// in sorted order, whatever the order of the files, and the error is at
// the first file of the second of them, with a note at the first file of
// each of the others. It returns whether the clauses all agree.
func (l *Loader) checkClauses(res *Result, dir string, pkg *ast.Package) bool {
	first := make(map[string]*ast.File) // the first file declaring each package
	var names []string
	for _, f := range pkg.Files {
		if _, seen := first[f.Package]; !seen {
			first[f.Package] = f
			names = append(names, f.Package)
		}
	}
	if len(names) == 1 {
		return true
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
	res.Stage, res.Diags = StageLoad, []diag.Diagnostic{d}
	return false
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
