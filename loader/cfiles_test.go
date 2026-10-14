package loader_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tacklang/tack/loader"
)

// cFilesFS is a module whose main package, in p, and the package it
// imports, in p/lib, each have C files among other files, and a
// single-file program, in single, beside C files of its own.
func cFilesFS() fstest.MapFS {
	return fstest.MapFS{
		"tack.mod": file("module m\n"),
		"p/main.tck": file(
			"package main\n\nimport util from \"m/p/util\"\n\nfunc main() { util.F() }\n",
		),
		"p/b.c":             file("int b;\n"),
		"p/a.c":             file("int a;\n"),
		"p/B.c":             file("int B;\n"),
		"p/.hidden.c":       file("not linked"),
		"p/notes.h":         file("not linked"),
		"p/main.c.orig":     file("not linked"),
		"p/dir.c/x.c":       file("not linked"),
		"p/util/util.tck":   file("package util\n\npub func F() {}\n"),
		"p/util/util.c":     file("int util;\n"),
		"single/main.tck":   file("package main\n\nfunc main() {}\n"),
		"single/other.tck":  file("package main\n"),
		"single/helpers.c":  file("int h;\n"),
		"single/sub/deep.c": file("not linked"),
	}
}

// TestCFilesOfEveryPackage checks the loader records each package's
// directory and the C files directly in it, in byte order of name,
// leaving out hidden files, other extensions and directories, for a
// directory target and the package it imports.
func TestCFilesOfEveryPackage(t *testing.T) {
	res, err := (&loader.Loader{FS: loader.FromFS(cFilesFS())}).Dir("p")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diags) > 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diags)
	}
	want := map[string]string{
		"m/p/util": "p/util: p/util/util.c",
		"m/p":      "p: p/B.c p/a.c p/b.c",
	}
	for _, pkg := range res.Program.Packages {
		got := pkg.Dir + ": " + strings.Join(pkg.CFiles, " ")
		if got != want[pkg.Path] {
			t.Errorf("package %s: got %q, want %q", pkg.Path, got, want[pkg.Path])
		}
	}
}

// TestCFilesBesideASingleFile checks a single-file target gets every C file
// in its directory, as a package directory would.
func TestCFilesBesideASingleFile(t *testing.T) {
	res, err := (&loader.Loader{FS: loader.FromFS(cFilesFS())}).File("single/main.tck")
	if err != nil {
		t.Fatal(err)
	}
	pkg := res.Program.Main()
	if got := pkg.Dir + ": " + strings.Join(pkg.CFiles, " "); got != "single: single/helpers.c" {
		t.Errorf("got %q, want %q", got, "single: single/helpers.c")
	}
}

// TestCFilesOfAFileInTheCurrentDirectory checks a single file named with
// no directory looks for C files in the current one, ".".
func TestCFilesOfAFileInTheCurrentDirectory(t *testing.T) {
	fsys := fstest.MapFS{
		"main.tck": file("package main\n\nfunc main() {}\n"),
		"x.c":      file("int x;\n"),
	}
	res, err := (&loader.Loader{FS: loader.FromFS(fsys)}).File("main.tck")
	if err != nil {
		t.Fatal(err)
	}
	pkg := res.Program.Main()
	if got := pkg.Dir + ": " + strings.Join(pkg.CFiles, " "); got != ".: x.c" {
		t.Errorf("got %q, want %q", got, ".: x.c")
	}
}

// TestCFileNamedLikeAnOption checks a C file whose name starts with - is
// an error naming it, in a package directory and beside a single file:
// clang could read such a name as an option.
func TestCFileNamedLikeAnOption(t *testing.T) {
	fsys := fstest.MapFS{
		"tack.mod":          file("module m\n"),
		"p/main.tck":        file("package main\n\nfunc main() {}\n"),
		"p/-fplugin=evil.c": file("int x;\n"),
		"s/main.tck":        file("package main\n\nfunc main() {}\n"),
		"s/-include=/etc.c": file("not reached: a directory"),
		"s/-Dx.c":           file("int x;\n"),
	}
	l := &loader.Loader{FS: loader.FromFS(fsys)}
	if _, err := l.Dir("p"); err == nil || !strings.Contains(err.Error(), "-fplugin=evil.c") {
		t.Errorf("Dir(p) error = %v, want one naming -fplugin=evil.c", err)
	}
	if _, err := l.File("s/main.tck"); err == nil || !strings.Contains(err.Error(), "-Dx.c") {
		t.Errorf("File(s/main.tck) error = %v, want one naming -Dx.c", err)
	}
}
