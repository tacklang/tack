package loader

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/tacklang/tack/source"
)

// loadTests loads the package in dir, whose import path is importPath in
// mod, for its tests, as Loader.TestMain describes: its Tack files and
// its test files, then the file TestMain gives, then every package it
// imports. A directory with neither is an error, as for any package.
func (l *Loader) loadTests(res *Result, mod *module, dir, importPath string) (*Result, error) {
	entries, err := mod.fsys.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read directory %s: %w", l.display(dir), err)
	}
	names := testModeNames(entries)
	if len(names) == 0 {
		return nil, fmt.Errorf("%w in %s", errNoTackFiles, l.display(dir))
	}
	files, cFiles, err := l.readNamed(res, mod.fsys, dir, names, entries)
	if err != nil {
		return nil, err
	}
	pkg, ok := l.parsePackage(res, files)
	if !ok {
		return res, nil
	}
	for _, f := range pkg.Files {
		f.Test = IsTestFile(path.Base(string(f.Span.File)))
	}
	if diags := l.checkClauses(dir, importPath, pkg); len(diags) > 0 {
		res.report(StageLoad, diags)
		return res, nil
	}
	pkg.Path, pkg.Module, pkg.Dir, pkg.CFiles = importPath, mod.name, dir, cFiles
	name, src := l.TestMain(pkg, res.Files)
	gen := &source.File{ID: source.FileID(name), Filename: name, Data: src}
	res.Files.Add(gen)
	genPkg, ok := l.parsePackage(res, []*source.File{gen})
	if !ok {
		return res, nil
	}
	pkg.Files = append(pkg.Files, genPkg.Files...)
	g := newGraph(l, res, mod, pkg)
	return g.finish(), nil
}

// testModeNames returns the names of the Tack files among entries, the
// entries of one directory, that a load for its tests reads: its
// package's and its test files, each one whose name does not start with
// a dot.
func testModeNames(entries []fs.DirEntry) []string {
	var names []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && (isPackageFile(name) || IsTestFile(name) && !strings.HasPrefix(name, ".")) {
			names = append(names, name)
		}
	}
	return names
}
