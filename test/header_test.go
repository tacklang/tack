package compilertest

import (
	"os"
	"path"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/typeck"
)

// TestHeaders checks that every example with C files carries the tack.h
// the compiler generates for it, in each directory tack --emit-header
// writes one to, and that no other example directory holds one. The
// copies are there to be read beside the C files; the build never uses
// them, so without this test they could go stale unnoticed. Under -update
// it rewrites them.
func TestHeaders(t *testing.T) {
	backend, ok := codegen.Get(frontEndBackend)
	if !ok {
		t.Fatalf("no backend registered under %q", frontEndBackend)
	}
	for _, ex := range findExamples(t) {
		t.Run(filepath.ToSlash(ex.dir), func(t *testing.T) {
			if got := run(t, frontEndBackend, ex.prog); got.stage != "ok" {
				return
			}
			prog := checkedProgram(t, ex.prog)
			want := headerDirs(prog)
			header := backend.Header(prog)
			for _, pkg := range prog.Packages {
				name := path.Join(pkg.Dir, "tack.h")
				if !want[pkg] {
					if ex.prog.files[name] != nil {
						t.Errorf("%s exists, but tack --emit-header writes none there", name)
					}
					continue
				}
				checkHeader(t, name, ex.prog.files[name], header)
			}
		})
	}
}

// checkedProgram loads and checks p, which run has already compiled
// without diagnostics, and returns the program.
func checkedProgram(t *testing.T, p program) *ast.Program {
	t.Helper()

	l := &loader.Loader{FS: loader.FromFS(p.files), Builtins: stdtest.MustBuiltins()}
	var res *loader.Result
	var err error
	if p.single {
		res, err = l.File(p.target)
	} else {
		res, err = l.Dir(p.target)
	}
	if err != nil {
		t.Fatal(err)
	}
	resolver.New(res.Program, stdtest.MustBuiltins()).Resolve()
	typeck.New(res.Program, res.Files, stdtest.MustBuiltins()).Check()
	return res.Program
}

// headerDirs returns the packages of prog that get a tack.h in their
// directory: none if prog has no C files, and otherwise those tack
// --emit-header writes to (writeHeaders in package main), the main package
// and each other package with C files or extern funcs.
func headerDirs(prog *ast.Program) map[*ast.Package]bool {
	dirs := make(map[*ast.Package]bool)
	hasC := false
	for _, pkg := range prog.Packages {
		hasC = hasC || len(pkg.CFiles) > 0
	}
	if !hasC {
		return dirs
	}
	for _, pkg := range prog.Packages {
		if pkg == prog.Main() || len(pkg.CFiles) > 0 || hasExterns(pkg) {
			dirs[pkg] = true
		}
	}
	return dirs
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

// checkHeader compares the example's copy of tack.h at name, nil if it has
// none, against header, or writes header there under -update. name is
// repo-relative, as the example's in-memory files are keyed.
func checkHeader(t *testing.T, name string, have *fstest.MapFile, header []byte) {
	t.Helper()

	if *update {
		if err := os.WriteFile(
			filepath.Join("..", filepath.FromSlash(name)),
			header,
			0o644,
		); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		return
	}
	switch {
	case have == nil:
		t.Errorf("%s is missing (run `go test ./test -update` to create it)", name)
	case string(have.Data) != string(header):
		t.Errorf("%s differs from the generated header\n\n--- want ---\n%s\n--- got ---\n%s",
			name, header, have.Data)
	}
}
