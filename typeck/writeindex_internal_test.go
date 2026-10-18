package typeck

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/resolver"
)

// TestWriteIndexMatchesEachWrite checks, on every program under
// examples/, that a writeIndex gives exactly the writes eachWrite finds,
// for every declaration, in every region assume or dropAssigned could be
// given: each function's body, and each node in it on its own. The index
// replaced a walk of the region for each fact with one walk for all of
// them, and must find the same writes in the same order, with the same
// loops, ends and drops.
func TestWriteIndexMatchesEachWrite(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join("..", "examples", "*"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, dir := range dirs {
		c, prog := checkedExample(t, dir)
		if c == nil {
			continue
		}
		checked++
		for _, decl := range prog.Decls() {
			fn, ok := decl.(*ast.Func)
			if !ok || len(fn.Body) == 0 {
				continue
			}
			compareRegion(t, c, dir, stmtNodes(fn.Body))
			for _, stmt := range fn.Body {
				ast.Walk(stmt, func(n ast.Node) bool {
					compareRegion(t, c, dir, []ast.Node{n})
					return true
				})
			}
		}
	}
	if checked < 50 {
		t.Fatalf("only %d examples were checked", checked)
	}
}

// checkedExample loads and checks the example program in dir, and returns
// its checker and program, or nil when it is not one that checks cleanly.
func checkedExample(t *testing.T, dir string) (*Checker, *ast.Program) {
	t.Helper()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	l := &loader.Loader{FS: loader.OS{}, Builtins: stdtest.Builtins(t), Std: stdtest.Std()}
	var res *loader.Result
	switch {
	case fileExists(filepath.Join(abs, "tack.mod")):
		res, err = l.Dir(filepath.ToSlash(abs))
	case fileExists(filepath.Join(abs, "main.tck")):
		res, err = l.File(filepath.ToSlash(filepath.Join(abs, "main.tck")))
	default:
		return nil, nil
	}
	if err != nil || res.Program == nil {
		return nil, nil
	}
	if diags := resolver.New(res.Program, stdtest.Builtins(t)).Resolve(); len(diags) > 0 {
		return nil, nil
	}
	c := New(res.Program, res.Files, stdtest.Builtins(t))
	if diags := c.Check(); len(diags) > 0 {
		return nil, nil
	}
	return c, res.Program
}

// fileExists reports whether a file is at name.
func fileExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

// compareRegion checks that the writeIndex of region holds, for every
// declaration region names, the writes eachWrite finds for it, and that
// a declaration it holds no writes for has none.
func compareRegion(t *testing.T, c *Checker, dir string, region []ast.Node) {
	t.Helper()
	idx := c.indexWrites(region)
	decls := namedDecls(region)
	for decl := range idx {
		if !slices.Contains(decls, decl) {
			t.Errorf("%s: the index holds writes to %s, which the region does not name",
				dir, declName(decl))
		}
	}
	for _, decl := range decls {
		var want []write
		c.eachWrite(region, decl, func(w write) { want = append(want, w) })
		compareWrites(t, c, dir, decl, idx[decl], want)
	}
}

// namedDecls returns every declaration a name in region refers to, each
// once.
func namedDecls(region []ast.Node) []ast.Decl {
	var decls []ast.Decl
	for _, n := range region {
		ast.Walk(n, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Def != nil && !slices.Contains(decls, id.Def) {
				decls = append(decls, id.Def)
			}
			return true
		})
	}
	return decls
}

// compareWrites checks that got, the index's writes to decl, are want,
// eachWrite's, with the drop writeDrop gives each.
func compareWrites(t *testing.T, c *Checker, dir string, decl ast.Decl, got, want []write) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: %s: the index has %d writes, eachWrite finds %d",
			dir, declName(decl), len(got), len(want))
		return
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.shown != w.shown || g.call != w.call || g.at != w.at ||
			!slices.Equal(g.loops, w.loops) {
			t.Errorf("%s: %s: write %d differs: index %+v, eachWrite %+v",
				dir, declName(decl), i, g, w)
		}
		if want := c.writeDrop(w, decl); *g.drop != *want {
			t.Errorf("%s: %s: write %d's drop differs: %+v, want %+v",
				dir, declName(decl), i, *g.drop, *want)
		}
	}
}
