package loader_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/source"
)

// genName is the name the test main of these tests is given.
const genName = "<gen>"

// testLoader returns a Loader of fsys for tests, whose TestMain records
// the package it was given in *got and gives a main of that package that
// imports imp, when it is not empty, and declares gen.
func testLoader(fsys fstest.MapFS, imp string, got **ast.Package) *loader.Loader {
	return &loader.Loader{
		FS: loader.FromFS(fsys),
		TestMain: func(pkg *ast.Package, files source.Files) (string, []byte) {
			*got = pkg
			src := "package " + pkg.Name + "\n\n"
			if imp != "" {
				src += "import x from \"" + imp + "\"\n\n"
			}
			return genName, []byte(src + "func gen() {\n}\n")
		},
	}
}

// fileNames returns the names of pkg's files, in order, each marked * when
// it is a test file.
func fileNames(pkg *ast.Package) string {
	names := make([]string, 0, len(pkg.Files))
	for _, f := range pkg.Files {
		name := string(f.Span.File)
		if f.Test {
			name += "*"
		}
		names = append(names, name)
	}
	return strings.Join(names, " ")
}

// TestTestMainLoadsTheRootsTestFiles checks a load for tests reads the
// package's test files with its others, in byte order, marks them, and
// adds the generated main last, whose import is loaded; a package it
// imports keeps its test files out, broken as they are, and so does the
// directory below. Lib, set, changes nothing.
func TestTestMainLoadsTheRootsTestFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"tack.mod":           file("module m\n"),
		"p/b.tck":            file("package p\n"),
		"p/a_test.tck":       file("package p\n\nimport q from \"m/q\"\n"),
		"p/c_test.tck":       file("package p\n"),
		"p/.hidden_test.tck": file(broken),
		"p/sub/s_test.tck":   file(broken),
		"q/q.tck":            file("package q\n"),
		"q/q_test.tck":       file(broken),
		"r/r.tck":            file("package r\n"),
		"r/r_test.tck":       file(broken),
	}
	var given *ast.Package
	l := testLoader(fsys, "m/r", &given)
	l.Lib = true
	res, err := l.Dir("p")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diags) > 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diags)
	}
	main := res.Program.Main()
	if got, want := fileNames(main), "p/a_test.tck* p/b.tck p/c_test.tck* "+genName; got != want {
		t.Errorf("files = %s, want %s", got, want)
	}
	if given != main || main.Path != "m/p" || main.Name != "p" {
		t.Errorf("TestMain was given %v, the package loaded is %s %s", given, main.Path, main.Name)
	}
	if n := len(res.Program.Packages); n != 3 {
		t.Errorf("the program has %d packages, want 3: q, r and p", n)
	}
	for _, pkg := range res.Program.Packages[:2] {
		for _, f := range pkg.Files {
			if f.Test {
				t.Errorf("%s is marked a test file", f.Span.File)
			}
		}
	}
}

// TestTestMainOfAMainPackage checks a main package loads for its tests
// as it does for a build, its test files added, and a directory holding
// only test files loads too, as a package of them alone.
func TestTestMainOfAMainPackage(t *testing.T) {
	fsys := fstest.MapFS{
		"tack.mod":           file("module m\n"),
		"main.tck":           file("package main\n\nfunc main() {\n}\n"),
		"main_test.tck":      file("package main\n"),
		"only/only_test.tck": file("package only\n"),
	}
	var given *ast.Package
	res, err := testLoader(fsys, "", &given).Dir(".")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fileNames(res.Program.Main()), "main.tck main_test.tck* "+genName; got != want {
		t.Errorf("files = %s, want %s", got, want)
	}
	res, err = testLoader(fsys, "", &given).Dir("only")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fileNames(res.Program.Main()), "only/only_test.tck* "+genName; got != want {
		t.Errorf("files = %s, want %s", got, want)
	}
}

// TestTestMainNotCalledForABrokenPackage checks TestMain is not called
// when a test file does not lex, which stops the load, nor when the
// package clauses are wrong, and that an empty directory is an error.
func TestTestMainNotCalledForABrokenPackage(t *testing.T) {
	fsys := fstest.MapFS{
		"tack.mod":         file("module m\n"),
		"lex/lex.tck":      file("package lex\n"),
		"lex/lex_test.tck": file(broken),
		"mixed/a.tck":      file("package mixed\n"),
		"mixed/a_test.tck": file("package other\n"),
		"empty/notes.txt":  file("nothing\n"),
	}
	for _, dir := range []string{"lex", "mixed"} {
		var given *ast.Package
		res, err := testLoader(fsys, "", &given).Dir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if given != nil || res.Program != nil || len(res.Diags) == 0 {
			t.Errorf("%s: TestMain given %v, program %v, diagnostics %v", dir, given, res.Program, res.Diags)
		}
	}
	var given *ast.Package
	if _, err := testLoader(fsys, "", &given).Dir("empty"); err == nil ||
		!strings.Contains(err.Error(), "no .tck files in empty") {
		t.Errorf("empty: err = %v", err)
	}
}

// TestTestMainImportBackIsACycle checks a test file importing a package
// that imports the package being tested, a library, is reported as an
// import cycle, not as an import of a main package.
func TestTestMainImportBackIsACycle(t *testing.T) {
	fsys := fstest.MapFS{
		"tack.mod":     file("module m\n"),
		"a/a.tck":      file("package a\n"),
		"a/a_test.tck": file("package a\n\nimport b from \"m/b\"\n"),
		"b/b.tck":      file("package b\n\nimport a from \"m/a\"\n"),
	}
	var given *ast.Package
	res, err := testLoader(fsys, "", &given).Dir("a")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diags) != 1 || res.Diags[0].Message != "import cycle: m/a imports m/b imports m/a" {
		t.Errorf("diagnostics = %v", res.Diags)
	}
}

// TestTestMainOfAGeneratedFileThatDoesNotParse checks a generated main
// with a syntax error is reported in that file, and one that does not
// lex stops the load, as any file does.
func TestTestMainOfAGeneratedFileThatDoesNotParse(t *testing.T) {
	fsys := fstest.MapFS{
		"tack.mod": file("module m\n"),
		"p/p.tck":  file("package p\n"),
	}
	for _, src := range []string{"package p\n\nfunc (\n", "@\n"} {
		l := &loader.Loader{
			FS: loader.FromFS(fsys),
			TestMain: func(*ast.Package, source.Files) (string, []byte) {
				return genName, []byte(src)
			},
		}
		res, err := l.Dir("p")
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Diags) == 0 || res.Diags[0].Span.File != genName {
			t.Errorf("%q: diagnostics = %v", src, res.Diags)
		}
	}
}
