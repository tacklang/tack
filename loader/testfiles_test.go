package loader_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tacklang/tack/loader"
)

// broken is the source of a test file that would stop loading if it were
// read: it does not lex.
const broken = "@ never lexed\n"

// filenames returns the name of every file res read, in order.
func filenames(res *loader.Result) string {
	got := make([]string, 0, len(res.Files.Files()))
	for _, f := range res.Files.Files() {
		got = append(got, f.Filename)
	}
	return strings.Join(got, " ")
}

// sourceNames returns the name of every file in res.Sources, in order.
func sourceNames(res *loader.Result) string {
	got := make([]string, 0, len(res.Sources))
	for _, f := range res.Sources {
		got = append(got, f.Filename)
	}
	return strings.Join(got, " ")
}

// TestDirLeavesOutTestFiles checks a package's files leave out every
// file whose name ends in _test.tck, _test.tck itself included, and keep
// those whose name only looks like one: the match is exact, so _TEST.tck
// and _Test.tck are ordinary files, as are test.tck and a name holding
// _test.tck before its end. The same files are read whether loading goes
// on to a program or stops after lexing or parsing.
func TestDirLeavesOutTestFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"tack.mod":              file("module m\n"),
		"p/main.tck":            file("package main\n"),
		"p/main_test.tck":       file(broken),
		"p/_test.tck":           file(broken),
		"p/a__test.tck":         file(broken),
		"p/.hidden_test.tck":    file(broken),
		"p/UPPER_TEST.tck":      file("package main\n"),
		"p/mixed_Test.tck":      file("package main\n"),
		"p/test.tck":            file("package main\n"),
		"p/x_test.tck.tck":      file("package main\n"),
		"p/dir_test.tck/x.tck":  file(broken),
		"p/main_test.tck.orig":  file(broken),
		"p/main_test.tck~":      file(broken),
		"p/main_test.c":         file("int x;\n"),
		"p/sub/nested_test.tck": file(broken),
	}
	const want = "p/UPPER_TEST.tck p/main.tck p/mixed_Test.tck p/test.tck p/x_test.tck.tck"
	for _, stop := range []loader.Stage{"", loader.StageLex, loader.StageParse} {
		t.Run(string(stop), func(t *testing.T) {
			res, err := (&loader.Loader{FS: loader.FromFS(fsys), Stop: stop}).Dir("p")
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Diags) > 0 {
				t.Fatalf("unexpected diagnostics: %v", res.Diags)
			}
			if got := filenames(res); got != "tack.mod "+want {
				t.Errorf("files = %s, want tack.mod %s", got, want)
			}
			if stop == "" {
				if n := len(res.Program.Main().Files); n != 5 {
					t.Errorf("main package has %d files, want 5", n)
				}
				// A C file is not a Tack file, whatever its name.
				if c := res.Program.Main().CFiles; len(c) != 1 || c[0] != "p/main_test.c" {
					t.Errorf("C files = %q", c)
				}
				return
			}
			if got := sourceNames(res); got != want {
				t.Errorf("sources = %s, want %s", got, want)
			}
		})
	}
}

// TestOnlyTestFiles checks a directory whose only Tack files are test
// files is no package: as a target, and as an import, which is
// reported at the import's path, it is an error that says the files
// there are test files.
func TestOnlyTestFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"tack.mod":           file("module m\n"),
		"one/one_test.tck":   file(broken),
		"bare/_test.tck":     file(broken),
		"many/a_test.tck":    file(broken),
		"many/b_test.tck":    file(broken),
		"many/notes.txt":     file("x"),
		"hidden/.x.tck":      file(broken),
		"hidden/x_test.tck":  file(broken),
		"cmd/main.tck":       file("package main\n\nimport one from \"m/one\"\n\nfunc main() {}\n"),
		"lexonly/x_test.tck": file(broken),
	}
	for _, dir := range []string{"one", "bare", "many", "hidden"} {
		t.Run(dir, func(t *testing.T) {
			_, err := (&loader.Loader{FS: loader.FromFS(fsys)}).Dir(dir)
			want := "no .tck files other than test files in " + dir
			if err == nil || err.Error() != want {
				t.Errorf("Dir(%q) error = %v, want %q", dir, err, want)
			}
		})
	}
	t.Run("stopped", func(t *testing.T) {
		for _, stop := range []loader.Stage{loader.StageLex, loader.StageParse} {
			_, err := (&loader.Loader{FS: loader.FromFS(fsys), Stop: stop}).Dir("lexonly")
			if err == nil || !strings.Contains(err.Error(), "other than test files") {
				t.Errorf("Dir stopping at %s: error = %v", stop, err)
			}
		}
	})
	t.Run("import", func(t *testing.T) {
		res, err := (&loader.Loader{FS: loader.FromFS(fsys)}).Dir("cmd")
		if err != nil {
			t.Fatal(err)
		}
		const want = "cannot find package m/one: no .tck files other than test files in one"
		if len(res.Diags) != 1 || res.Diags[0].Message != want {
			t.Errorf("diagnostics = %v, want one: %s", res.Diags, want)
		}
	})
}

// TestLibLeavesOutTestFiles checks a library leaves out every package's
// test files, and a directory holding only test files, as it does one
// holding no Tack file: it is no package of the library.
func TestLibLeavesOutTestFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"tack.mod":             file("module m\n"),
		"a/a.tck":              file("package a\n"),
		"a/a_test.tck":         file(broken),
		"b/b_test.tck":         file(broken),
		"b/c/c.tck":            file("package c\n"),
		"b/c/c_test.tck":       file(broken),
		"tests/x_test.tck":     file(broken),
		"tests/deep/_test.tck": file(broken),
	}
	for _, stop := range []loader.Stage{"", loader.StageLex, loader.StageParse} {
		t.Run(string(stop), func(t *testing.T) {
			l := &loader.Loader{FS: loader.FromFS(fsys), Lib: true, Stop: stop}
			res, err := l.Dir(".")
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Diags) > 0 {
				t.Fatalf("unexpected diagnostics: %v", res.Diags)
			}
			if got, want := filenames(res), "tack.mod a/a.tck b/c/c.tck"; got != want {
				t.Errorf("files = %s, want %s", got, want)
			}
			if stop == "" {
				var paths []string
				for _, pkg := range res.Program.Packages {
					paths = append(paths, pkg.Path)
				}
				if got := strings.Join(paths, " "); got != "m/a m/b/c" {
					t.Errorf("packages = %s, want m/a m/b/c", got)
				}
			}
		})
	}
}

// TestLibOfOnlyTestFiles checks a library with no package to load says
// whether the Tack files it found at or below its root were all test
// files, and keeps the plain wording when it found none at all, a
// hidden test file not counting.
func TestLibOfOnlyTestFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"tack.mod":             file("module m\n"),
		"tests/x_test.tck":     file(broken),
		"tests/deep/_test.tck": file(broken),
	}
	t.Run("only tests", func(t *testing.T) {
		_, err := (&loader.Loader{FS: loader.FromFS(fsys), Lib: true}).Dir("tests")
		want := "no .tck files other than test files in tests or below"
		if err == nil || err.Error() != want {
			t.Errorf("error = %v, want %q", err, want)
		}
	})
	t.Run("nothing", func(t *testing.T) {
		empty := fstest.MapFS{
			"tack.mod":           file("module m\n"),
			"e/notes.txt":        file("x"),
			"e/.hidden_test.tck": file(broken),
		}
		_, err := (&loader.Loader{FS: loader.FromFS(empty), Lib: true}).Dir("e")
		if want := "no .tck files in e or below"; err == nil || err.Error() != want {
			t.Errorf("error = %v, want %q", err, want)
		}
	})
}

// TestStdLeavesOutTestFiles checks a std package's test files are left
// out as any package's are, whether std is imported or checked itself.
func TestStdLeavesOutTestFiles(t *testing.T) {
	fsys := stdFS()
	fsys["lib/std/fmt/fmt_test.tck"] = file(broken)
	fsys["lib/std/text/_test.tck"] = file(broken)
	res := loadWithStd(t, fsys, "lib/std", "calc/cmd")
	if len(res.Diags) > 0 {
		t.Fatalf("diagnostics: %v", res.Diags)
	}
	if got := filenames(res); strings.Contains(got, "_test.tck") {
		t.Errorf("files = %s, which names a test file", got)
	}
	l := &loader.Loader{FS: loader.FromFS(fsys), Std: &loader.Std{Dir: "lib/std"}, Lib: true}
	lib, err := l.Dir("lib/std")
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Diags) > 0 || lib.Program == nil {
		t.Fatalf("checking std: diagnostics %v", lib.Diags)
	}
	if got := filenames(lib); strings.Contains(got, "_test.tck") {
		t.Errorf("std's files = %s, which names a test file", got)
	}
}

// TestFileTargetTestFile checks a test file named as a single file
// target is rejected by its name, before it is read, so a missing one
// is too, in every mode; the error shows the name as Display gives it.
// A name ending in _TEST.tck is no test file and loads.
func TestFileTargetTestFile(t *testing.T) {
	fsys := fstest.MapFS{
		"x_test.tck":     file("package main\n\nfunc main() {}\n"),
		"d/_test.tck":    file("package main\n\nfunc main() {}\n"),
		"X_TEST.tck":     file("package main\n\nfunc main() {}\n"),
		"x_test.tck.tck": file("package main\n\nfunc main() {}\n"),
	}
	for _, name := range []string{"x_test.tck", "d/_test.tck", "missing_test.tck"} {
		for _, stop := range []loader.Stage{"", loader.StageLex, loader.StageParse} {
			t.Run(name+" "+string(stop), func(t *testing.T) {
				l := &loader.Loader{
					FS: loader.FromFS(fsys), Stop: stop, Lib: true,
					Display: func(n string) string { return "shown/" + n },
				}
				_, err := l.File(name)
				want := "shown/" + name + " is a test file, which is never loaded on its own"
				if err == nil || err.Error() != want {
					t.Errorf("File(%q) error = %v, want %q", name, err, want)
				}
			})
		}
	}
	for _, name := range []string{"X_TEST.tck", "x_test.tck.tck"} {
		res, err := (&loader.Loader{FS: loader.FromFS(fsys)}).File(name)
		if err != nil || len(res.Diags) > 0 || res.Program == nil {
			t.Errorf("File(%q) = %v, %v", name, res, err)
		}
	}
}
