package loader

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
)

// stdPrefix starts the import path of every package of the standard
// library, which is the module std.
const stdPrefix = "std/"

// builtinPath is the path of the package that documents the builtins.
const builtinPath = "std/builtin"

// progress says how far loading a package has got.
type progress int

// The zero progress means not looked at yet.
const (
	// loading means the package's imports are being loaded, further up
	// the stack, so importing it again closes a cycle.
	loading progress = iota + 1
	// loaded means the package and everything it imports are loaded.
	loaded
)

// graph loads the packages a program's main package imports, directly or
// not, depth first.
type graph struct {
	l    *Loader
	res  *Result
	mod  *module // nil for a program built from a single file
	main *ast.Package

	state    map[string]progress     // each package looked at, by import path
	packages map[string]*ast.Package // each package loaded, by import path
	stack    []string                // the import paths being loaded, outermost first
	order    []*ast.Package          // the packages loaded, each after what it imports
	diags    []diag.Diagnostic
	stopped  bool // a package failed to lex or parse; res holds why
}

// newGraph returns a graph for the program whose main package is main, in
// mod, recording what it reads in res.
func newGraph(l *Loader, res *Result, mod *module, main *ast.Package) *graph {
	return &graph{
		l:        l,
		res:      res,
		mod:      mod,
		main:     main,
		state:    make(map[string]progress),
		packages: make(map[string]*ast.Package),
	}
}

// finish loads everything the main package imports and returns res, with
// the program or the diagnostics that stopped it.
func (g *graph) finish() *Result {
	g.state[g.main.Path] = loading
	g.stack = append(g.stack, g.main.Path)
	g.loadImports(g.main)
	switch {
	case g.stopped:
	case len(g.diags) > 0:
		g.res.Stage, g.res.Diags = StageLoad, g.diags
	default:
		g.res.Program = &ast.Program{Packages: append(g.order, g.main)}
	}
	return g.res
}

// loadImports loads every package pkg's files import, in file order and
// source order, and sets each import's Package.
func (g *graph) loadImports(pkg *ast.Package) {
	for _, f := range pkg.Files {
		for _, imp := range f.Imports {
			if g.stopped {
				return
			}
			imp.Package = g.load(imp)
		}
	}
}

// load returns the package imp names, loading it and what it imports the
// first time, or nil, with the problem recorded, if it cannot be imported.
func (g *graph) load(imp *ast.Import) *ast.Package {
	p := imp.Path
	if msg := g.checkPath(p); msg != "" {
		g.errorAt(imp, msg)
		return nil
	}
	if p == g.main.Path {
		g.errorAt(imp, fmt.Sprintf("cannot import %s: it is a main package", show(p)))
		return nil
	}
	switch g.state[p] {
	case loaded:
		return g.packages[p]
	case loading:
		g.errorAt(imp, g.cycle(p))
		return nil
	}
	g.state[p] = loading
	pkg := g.loadPackage(imp)
	g.state[p] = loaded
	return pkg
}

// loadPackage loads the package imp names, which has not been looked at,
// and everything it imports.
func (g *graph) loadPackage(imp *ast.Import) *ast.Package {
	p := imp.Path
	dir, _ := g.mod.dirOf(p)
	files, err := g.l.readPackageFiles(g.res, dir)
	if err != nil {
		g.errorAt(imp, g.missing(p, err))
		return nil
	}
	pkg, ok := g.l.parsePackage(g.res, files)
	if !ok {
		g.stopped = true
		return nil
	}
	if diags := g.l.checkClauses(dir, p, pkg); len(diags) > 0 {
		g.diags = append(g.diags, diags...)
		return nil
	}
	if pkg.Name == mainPackage {
		g.errorAt(imp, fmt.Sprintf("cannot import %s: it is a main package", show(p)))
		return nil
	}
	pkg.Path = p
	g.stack = append(g.stack, p)
	g.loadImports(pkg)
	g.stack = g.stack[:len(g.stack)-1]
	g.packages[p] = pkg
	g.order = append(g.order, pkg)
	return pkg
}

// checkPath returns why p cannot be imported before looking for it, or "".
func (g *graph) checkPath(p string) string {
	switch {
	case p == builtinPath:
		return builtinPath + " documents the builtins and cannot be imported"
	case p == "std" || strings.HasPrefix(p, stdPrefix):
		return fmt.Sprintf("cannot find package %s: the standard library has no packages yet",
			show(p))
	case g.mod == nil:
		return fmt.Sprintf("cannot import %s: a single file can import only the standard library",
			show(p))
	}
	if msg := checkImportPath(p, g.mod.name); msg != "" {
		return msg
	}
	_, msg := g.mod.dirOf(p)
	return msg
}

// missing returns the message for p, whose directory could not be read as
// a package because of err.
func (g *graph) missing(p string, err error) string {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Sprintf("cannot find package %s: no directory %s in module %s",
			show(p), show(p), g.mod.name)
	}
	return fmt.Sprintf("cannot find package %s: %v", show(p), err)
}

// cycle returns the message for importing p while it is being loaded: the
// imports from p round to p again.
func (g *graph) cycle(p string) string {
	start := 0
	for i, q := range g.stack {
		if q == p {
			start = i
		}
	}
	chain := make([]string, 0, len(g.stack)-start+1)
	for _, q := range g.stack[start:] {
		chain = append(chain, show(q))
	}
	chain = append(chain, show(p))
	return "import cycle: " + strings.Join(chain, " imports ")
}

// errorAt records an error at imp's path.
func (g *graph) errorAt(imp *ast.Import, msg string) {
	g.diags = append(g.diags, diag.Diagnostic{
		Severity: diag.Error,
		Message:  msg,
		Span:     imp.PathSpan,
	})
}
