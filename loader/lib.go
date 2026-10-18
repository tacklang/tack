package loader

import (
	"fmt"
	"path"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/modfile"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
)

// loadLib loads, into res, the library at dir in mod: every package in a
// directory at or below dir, in the byte order of their paths, that is
// not a main package, with everything each imports, as one Program whose
// packages are each listed after those it imports. A directory whose name
// starts with a dot is left out, with everything below it, as is one
// holding a tack.mod of its own, another module's root. A main package is
// left out too: a library has none, and each one is checked by naming its
// own directory. files and cFiles are dir's own, already read into res,
// or nil when dir holds no Tack file.
//
// No package to load is an error, saying whether there were only main
// packages. With l.Stop set, res gets the tokens or trees of every package
// loaded as the library's own, and none of what they import.
func (l *Loader) loadLib(
	res *Result,
	mod *module,
	dir string,
	files []*source.File,
	cFiles []string,
) (*Result, error) {
	dirs, err := l.packageDirs(mod.fsys, dir)
	if err != nil {
		return nil, err
	}
	g := newGraph(l, res, mod, nil)
	loadedAny, sawMain := false, false
	for _, d := range dirs {
		var read *packageFiles
		if d == dir && files != nil {
			read = &packageFiles{files: files, cFiles: cFiles}
		}
		isMain, err := g.addDir(d, read)
		if err != nil {
			return nil, err
		}
		sawMain = sawMain || isMain
		loadedAny = loadedAny || !isMain
		if g.stopped {
			// The package, or one it imports, failed to lex, or left
			// nothing to load, which is reported; there is no program.
			g.res.report(StageLoad, g.diags)
			return res, nil
		}
	}
	if !loadedAny {
		return nil, l.noLibrary(dir, sawMain)
	}
	return g.finishLib(), nil
}

// finishLib returns g's Result once every package of the library it loads
// is added: with the program, the diagnostics found loading it, or both,
// as finish gives a program's, the program counting the diagnostics in
// its Errors; or with only the diagnostics when no package could be
// loaded at all; or with neither when loading stopped at l.Stop, having
// recorded what was asked for.
func (g *graph) finishLib() *Result {
	g.res.report(StageLoad, g.diags)
	if g.l.Stop == "" && len(g.order) > 0 {
		g.res.Program = &ast.Program{Packages: g.order, Errors: len(g.res.Diags)}
	}
	return g.res
}

// packageFiles are the Tack files of a package's directory, and the paths
// of its C files.
type packageFiles struct {
	files  []*source.File
	cFiles []string
}

// addDir adds the package in the directory d, of g's module, to the
// library g loads, as addRoot does, unless an earlier package imports it
// and so loaded it already, or it is a main package, which it reports.
// read holds d's files when they were read already, and is nil otherwise.
func (g *graph) addDir(d string, read *packageFiles) (bool, error) {
	p := g.mod.pathOf(d)
	if g.state[p] == loaded {
		return false, nil
	}
	if read == nil {
		files, cFiles, err := g.l.readPackageFiles(g.res, g.mod.fsys, d)
		if err != nil {
			return false, err
		}
		read = &packageFiles{files: files, cFiles: cFiles}
	}
	if name, ok := clauseName(read.files); ok && name == mainPackage {
		return true, nil
	}
	g.addRoot(d, p, read.files, read.cFiles)
	return false, nil
}

// noLibrary returns the error for a library at dir with no package to
// load: none at all, or, when sawMain says so, only main packages.
func (l *Loader) noLibrary(dir string, sawMain bool) error {
	if sawMain {
		return fmt.Errorf(
			"no library package in %s or below; check a main package by naming its directory",
			l.display(dir))
	}
	return fmt.Errorf("%w in %s or below", errNoTackFiles, l.display(dir))
}

// addRoot loads the package whose files, read from the directory dir, are
// files, with the C files cFiles, as one of a library's own, with import
// path p, and everything it imports. Syntax errors in its files are
// recorded, and it is added with bad nodes where they were. It is not
// added when its files fail to lex, or leave nothing to load (see
// parsePackage), which sets g.stopped, as a package it imports failing to
// does; when loading stops at l.Stop, having recorded them; or when its
// package clauses are wrong, which is recorded in g.diags.
func (g *graph) addRoot(dir, p string, files []*source.File, cFiles []string) {
	pkg, ok := g.l.parsePackage(g.res, files)
	if !ok {
		// Stopping at l.Stop goes on to the next package; a failure to
		// load stops loading, as it does for an import.
		g.stopped = g.l.Stop == ""
		return
	}
	if diags := g.l.checkClauses(dir, p, pkg); len(diags) > 0 {
		g.diags = append(g.diags, diags...)
		return
	}
	pkg.Path, pkg.Module, pkg.Dir, pkg.CFiles = p, g.mod.name, dir, cFiles
	g.state[p] = loading
	g.stack = append(g.stack, p)
	g.loadImports(pkg)
	g.stack = g.stack[:len(g.stack)-1]
	g.state[p] = loaded
	g.packages[p] = pkg
	g.order = append(g.order, pkg)
}

// packageDirs returns dir and every directory below it, through fsys, in
// the byte order of their paths, that holds a Tack file, leaving out a
// directory whose name starts with a dot and one below dir holding a
// tack.mod, with everything below either, and std's root, which is a
// module of its own.
func (l *Loader) packageDirs(fsys FS, dir string) ([]string, error) {
	hasTack, subs, err := l.scanDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var dirs []string
	if hasTack {
		dirs = append(dirs, dir)
	}
	for _, sub := range subs {
		below, err := l.packageDirs(fsys, sub)
		if err != nil {
			return nil, err
		}
		dirs = append(dirs, below...)
	}
	return dirs, nil
}

// scanDir reads the directory dir through fsys and reports whether it
// holds a Tack file, and returns the paths of the directories in it to
// look in for more, in byte order: those whose names do not start with a
// dot and that are not a module's root.
func (l *Loader) scanDir(fsys FS, dir string) (bool, []string, error) {
	entries, err := fsys.ReadDir(dir)
	if err != nil {
		return false, nil, fmt.Errorf("cannot read directory %s: %w", l.display(dir), err)
	}
	hasTack := false
	var subs []string
	// ReadDir returns entries sorted by name, so subs is in byte order.
	for _, e := range entries {
		name := e.Name()
		if name[0] == '.' {
			continue
		}
		if !e.IsDir() {
			hasTack = hasTack || path.Ext(name) == sourceExt
			continue
		}
		sub := path.Join(dir, name)
		nested, err := l.isModuleRoot(fsys, sub)
		if err != nil {
			return false, nil, err
		}
		if !nested {
			subs = append(subs, sub)
		}
	}
	return hasTack, subs, nil
}

// isModuleRoot reports whether the directory d, read through fsys, is the
// root of a module: it holds a tack.mod, or it is std's root.
func (l *Loader) isModuleRoot(fsys FS, d string) (bool, error) {
	if l.isStdRoot(d) {
		return true, nil
	}
	entries, err := fsys.ReadDir(d)
	if err != nil {
		return false, fmt.Errorf("cannot read directory %s: %w", l.display(d), err)
	}
	for _, e := range entries {
		if e.Name() == modfile.Name {
			return true, nil
		}
	}
	return false, nil
}

// clauseName returns the name the first of files, the files of one
// package, gives in its package clause, lexing and parsing it apart from
// any Result. It returns false when the file does not lex or parse, which
// loading the package reports. It lexes a copy of the file, since lexing
// records the file's line starts, which the real lexing records again.
func clauseName(files []*source.File) (string, bool) {
	if len(files) == 0 {
		return "", false
	}
	f := files[0]
	lex := lexer.New(&source.File{ID: f.ID, Filename: f.Filename, Data: f.Data})
	if len(lex.Lex()) > 0 {
		return "", false
	}
	tree, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
	if len(diags) > 0 || tree == nil {
		return "", false
	}
	return tree.Package, true
}
