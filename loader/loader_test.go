package loader_test

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tacklang/tack/loader"
)

// file returns a MapFile holding src.
func file(src string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(src)} }

// TestDirReadsTackFilesInNameOrder checks a directory's package is its .tck
// files in byte order of name, whatever order they were written in, with
// hidden files, other extensions, and directories, even one named like a
// Tack file, left out.
func TestDirReadsTackFilesInNameOrder(t *testing.T) {
	fsys := fstest.MapFS{
		"tack.mod":         file("module m\n"),
		"p/b.tck":          file("package main\n"),
		"p/a.tck":          file("package main\n"),
		"p/B.tck":          file("package main\n"),
		"p/.hidden.tck":    file("not tack at all"),
		"p/notes.txt":      file("not tack at all"),
		"p/main.tck.orig":  file("not tack at all"),
		"p/sub.tck/x.tck":  file("not tack at all"),
		"p/sub/nested.tck": file("not tack at all"),
	}
	res, err := (&loader.Loader{FS: loader.FromFS(fsys)}).Dir("p")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diags) > 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diags)
	}
	got := make([]string, 0, len(res.Files.Files()))
	for _, f := range res.Files.Files() {
		got = append(got, f.Filename)
	}
	want := "tack.mod p/B.tck p/a.tck p/b.tck"
	if strings.Join(got, " ") != want {
		t.Errorf("files = %v, want %s", got, want)
	}
	pkg := res.Program.Main()
	if pkg.Path != "m/p" || pkg.Name != "main" || len(pkg.Files) != 3 {
		t.Errorf("package = %s %s with %d files", pkg.Path, pkg.Name, len(pkg.Files))
	}
}

// TestDirErrors checks every way a directory target can fail before there
// is any source to point a diagnostic at.
func TestDirErrors(t *testing.T) {
	fsys := fstest.MapFS{
		"tack.mod":         file("module m\n"),
		"empty":            &fstest.MapFile{Mode: fs.ModeDir},
		"other/notes.txt":  file("x"),
		"hidden/.main.tck": file("package main\n"),
		"onlydirs/a.tck/b": file("x"),
		"file.tck":         file("package main\n"),
	}
	for _, tc := range []struct{ dir, want string }{
		{"empty", "no .tck files in empty"},
		{"other", "no .tck files in other"},
		{"hidden", "no .tck files in hidden"},
		{"onlydirs", "no .tck files in onlydirs"},
		{"missing", "cannot read directory missing"},
		{"file.tck", "cannot read directory file.tck"},
	} {
		t.Run(tc.dir, func(t *testing.T) {
			res, err := (&loader.Loader{FS: loader.FromFS(fsys)}).Dir(tc.dir)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Dir(%q) = %v, %v; want an error containing %q", tc.dir, res, err, tc.want)
			}
		})
	}
}

// failingFS lists a directory but fails to read one of its files.
type failingFS struct{ loader.FS }

// errUnreadable is what failingFS reports for bad.tck.
var errUnreadable = errors.New("unreadable")

// ReadFile fails for any file named bad.tck.
func (f failingFS) ReadFile(name string) ([]byte, error) {
	if strings.HasSuffix(name, "/bad.tck") {
		return nil, errUnreadable
	}
	data, err := f.FS.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}
	return data, nil
}

// TestDirWithUnreadableFile checks a file that is listed but cannot be
// read stops loading with an error naming the cause.
func TestDirWithUnreadableFile(t *testing.T) {
	fsys := failingFS{loader.FromFS(fstest.MapFS{
		"tack.mod":  file("module m\n"),
		"p/a.tck":   file("package main\n"),
		"p/bad.tck": file("package main\n"),
	})}
	_, err := (&loader.Loader{FS: fsys}).Dir("p")
	if !errors.Is(err, errUnreadable) {
		t.Errorf("err = %v, want it to wrap %v", err, errUnreadable)
	}
}

// TestDisplayNamesFiles checks Display gives each file both the name its
// diagnostics show and its FileID.
func TestDisplayNamesFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"abs/tack.mod": file("module m\n"),
		"abs/p/a.tck":  file("package main\nfunc main() {}\n"),
	}
	l := &loader.Loader{
		FS:      loader.FromFS(fsys),
		Display: func(name string) string { return strings.TrimPrefix(name, "abs/") },
	}
	res, err := l.Dir("abs/p")
	if err != nil {
		t.Fatal(err)
	}
	f := res.Files.Lookup("p/a.tck")
	if f == nil || f.Filename != "p/a.tck" || string(f.ID) != "p/a.tck" {
		t.Errorf("file shown as %q with ID %q, want p/a.tck for both", f.Filename, f.ID)
	}
	if mod := res.Files.Lookup("tack.mod"); mod == nil || mod.Filename != "tack.mod" {
		t.Errorf("tack.mod is not shown by its displayed name: %v", mod)
	}
}

// TestStopAfterLexAndParse checks a dump stops loading where it asks,
// having read every file of the target and nothing more.
func TestStopAfterLexAndParse(t *testing.T) {
	fsys := fstest.MapFS{
		"tack.mod": file("module m\n"),
		"p/a.tck":  file("package main\n"),
		"p/b.tck":  file("package other\n"),
	}
	lexed, err := (&loader.Loader{FS: loader.FromFS(fsys), Stop: loader.StageLex}).Dir("p")
	if err != nil {
		t.Fatal(err)
	}
	if len(lexed.Tokens) != 2 || len(lexed.Sources) != 2 || lexed.Program != nil ||
		len(lexed.Diags) > 0 {
		t.Errorf("lex: %d token lists, program %v, diags %v", len(lexed.Tokens), lexed.Program,
			lexed.Diags)
	}
	parsed, err := (&loader.Loader{FS: loader.FromFS(fsys), Stop: loader.StageParse}).Dir("p")
	if err != nil {
		t.Fatal(err)
	}
	// The clauses disagree, but that is found after parsing, where the
	// dump stops.
	if len(parsed.Trees) != 2 || parsed.Program != nil || len(parsed.Diags) > 0 {
		t.Errorf("parse: %d trees, program %v, diags %v", len(parsed.Trees), parsed.Program,
			parsed.Diags)
	}
}

// TestFileMissing checks a file that cannot be read is an error.
func TestFileMissing(t *testing.T) {
	_, err := (&loader.Loader{FS: loader.FromFS(fstest.MapFS{})}).File("nope.tck")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want it to wrap fs.ErrNotExist", err)
	}
}

// TestModuleRootAnywhere checks the module root is the nearest tack.mod at
// or above the target, at the root of the file system included, and gives
// the import path of the target's package.
func TestModuleRootAnywhere(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fsys  fstest.MapFS
		dir   string
		path  string
		files string
	}{
		{
			"at the file system's root",
			fstest.MapFS{"tack.mod": file("module calc\n"), "a/b/main.tck": file("package main\n")},
			"a/b", "calc/a/b", "tack.mod a/b/main.tck",
		},
		{
			"nearest wins",
			fstest.MapFS{
				"tack.mod":       file("module outer\n"),
				"in/tack.mod":    file("module inner\n"),
				"in/x/main.tck":  file("package main\n"),
				"in/x/other.txt": file(""),
			},
			"in/x", "inner/x", "in/tack.mod in/x/main.tck",
		},
		{
			"the root itself",
			fstest.MapFS{"m/tack.mod": file("module example.com/m\n"), "m/main.tck": file("package main\n")},
			"m", "example.com/m", "m/tack.mod m/main.tck",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := (&loader.Loader{FS: loader.FromFS(tc.fsys)}).Dir(tc.dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Diags) > 0 {
				t.Fatalf("diagnostics: %v", res.Diags)
			}
			if got := res.Program.Main().Path; got != tc.path {
				t.Errorf("path = %q, want %q", got, tc.path)
			}
			names := make([]string, 0, len(res.Files.Files()))
			for _, f := range res.Files.Files() {
				names = append(names, f.Filename)
			}
			if strings.Join(names, " ") != tc.files {
				t.Errorf("files read = %v, want %s", names, tc.files)
			}
		})
	}
}

// TestModuleFileUnreadable checks a tack.mod that exists but cannot be
// read stops the search with an error, rather than being skipped for one
// further up.
func TestModuleFileUnreadable(t *testing.T) {
	fsys := failingFS{loader.FromFS(fstest.MapFS{
		"tack.mod":     file("module outer\n"),
		"p/tack.mod":   &fstest.MapFile{Mode: fs.ModeDir},
		"p/main.tck":   file("package main\n"),
		"q/bad.tck":    file("package main\n"),
		"q/x/main.tck": file("package main\n"),
	})}
	_, err := (&loader.Loader{FS: fsys}).Dir("p")
	if err == nil || !strings.Contains(err.Error(), "cannot read p/tack.mod") {
		t.Errorf("err = %v, want one saying p/tack.mod cannot be read", err)
	}
}

// TestPathErrors checks every way an import path target names no package
// is an error naming the cause, and a relative or absolute one suggests
// the module-rooted path it probably means.
func TestPathErrors(t *testing.T) {
	fsys := fstest.MapFS{
		"tack.mod":        file("module calc\n"),
		"lexer/lexer.tck": file("package lexer\n"),
	}
	for _, tc := range []struct{ path, want string }{
		{"", "import path is empty"},
		{"../lexer", "import path must start with a module name, such as calc/lexer; relative paths are not allowed"},
		{"./lexer", "such as calc/lexer; relative paths are not allowed"},
		{"..", "such as calc/<package>; relative paths are not allowed"},
		{"/abs/lexer", "such as calc/lexer; absolute paths are not allowed"},
		{"calc/", "invalid import path calc/: a part between slashes is empty"},
		{"calc/./lexer", "invalid import path calc/./lexer: a part cannot be . or .."},
		{"calc/\x00", `cannot find package "calc/\x00"`},
		{"calcx/lexer", "unknown module calcx in import path calcx/lexer"},
		{"calc/lexer/deeper", "cannot find package calc/lexer/deeper: no directory calc/lexer/deeper in module calc"},
		{"calc/lexer\xff", `cannot find package "calc/lexer\xff"`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			_, err := (&loader.Loader{FS: loader.FromFS(fsys)}).Path(".", tc.path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Path(%q) = %v, want an error containing %q", tc.path, err, tc.want)
			}
		})
	}
}

// chain returns a module m whose main package imports p0, which imports
// p1, and so on down to p(n-1), which imports nothing, or, when back is
// non-negative, p(back), closing a cycle.
func chain(n, back int) fstest.MapFS {
	fsys := fstest.MapFS{
		"tack.mod": file("module m\n"),
		"main.tck": file("package main\n\nimport p from \"m/p0\"\n\nfunc main() {\n}\n"),
	}
	for i := range n {
		src := fmt.Sprintf("package p%d\n", i)
		switch {
		case i+1 < n:
			src += fmt.Sprintf("\nimport q from \"m/p%d\"\n", i+1)
		case back >= 0:
			src += fmt.Sprintf("\nimport q from \"m/p%d\"\n", back)
		}
		fsys[fmt.Sprintf("p%d/p.tck", i)] = file(src)
	}
	return fsys
}

// TestHugeImportGraphs checks a long chain of imports loads, every package
// once and each after what it imports.
func TestHugeImportGraphs(t *testing.T) {
	const n = 3000
	res, err := (&loader.Loader{FS: loader.FromFS(chain(n, -1))}).Dir(".")
	if err != nil || len(res.Diags) > 0 {
		t.Fatalf("chain of %d: %v %v", n, err, res.Diags)
	}
	pkgs := res.Program.Packages
	if len(pkgs) != n+1 || pkgs[0].Path != fmt.Sprintf("m/p%d", n-1) || pkgs[n].Path != "m" {
		t.Fatalf(
			"got %d packages, first %s, last %s",
			len(pkgs),
			pkgs[0].Path,
			pkgs[len(pkgs)-1].Path,
		)
	}
}

// TestLongImportCycle checks a long cycle is reported once, naming every
// package on it, without the loader running away.
func TestLongImportCycle(t *testing.T) {
	res, err := (&loader.Loader{FS: loader.FromFS(chain(50, 10))}).Dir(".")
	if err != nil || len(res.Diags) != 1 {
		t.Fatalf("cycle: %v, %d diagnostics", err, len(res.Diags))
	}
	msg := res.Diags[0].Message
	if !strings.HasPrefix(msg, "import cycle: m/p10 imports m/p11 imports") ||
		!strings.HasSuffix(msg, "m/p49 imports m/p10") || strings.Count(msg, " imports ") != 40 {
		t.Errorf("cycle reported as %q", msg)
	}
}

// TestWideDiamond checks many packages importing one shared package load
// it once.
func TestWideDiamond(t *testing.T) {
	const n = 500
	fsys := fstest.MapFS{
		"tack.mod":      file("module m\n"),
		"base/base.tck": file("package base\n"),
	}
	main := "package main\n\n"
	for i := range n {
		main += fmt.Sprintf("import p%d from \"m/p%d\"\n", i, i)
		fsys[fmt.Sprintf("p%d/p.tck", i)] = file(fmt.Sprintf(
			"package p%d\n\nimport base from \"m/base\"\n", i))
	}
	fsys["main.tck"] = file(main + "\nfunc main() {\n}\n")
	res, err := (&loader.Loader{FS: loader.FromFS(fsys)}).Dir(".")
	if err != nil || len(res.Diags) > 0 {
		t.Fatalf("%v %v", err, res.Diags)
	}
	if got := len(res.Program.Packages); got != n+2 {
		t.Errorf("got %d packages, want %d", got, n+2)
	}
	if got := len(res.Files.Files()); got != n+3 {
		t.Errorf("read %d files, want %d: the shared package once", got, n+3)
	}
}
