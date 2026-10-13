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
	want := "p/B.tck p/a.tck p/b.tck"
	if strings.Join(got, " ") != want {
		t.Errorf("files = %v, want %s", got, want)
	}
	pkg := res.Program.Main()
	if pkg.Path != "main" || pkg.Name != "main" || len(pkg.Files) != 3 {
		t.Errorf("package = %s %s with %d files", pkg.Path, pkg.Name, len(pkg.Files))
	}
}

// TestDirErrors checks every way a directory target can fail before there
// is any source to point a diagnostic at.
func TestDirErrors(t *testing.T) {
	fsys := fstest.MapFS{
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
	fsys := fstest.MapFS{"abs/p/a.tck": file("package main\nfunc main() {}\n")}
	l := &loader.Loader{
		FS:      loader.FromFS(fsys),
		Display: func(name string) string { return strings.TrimPrefix(name, "abs/") },
	}
	res, err := l.Dir("abs/p")
	if err != nil {
		t.Fatal(err)
	}
	f := res.Files.Files()[0]
	if f.Filename != "p/a.tck" || string(f.ID) != "p/a.tck" {
		t.Errorf("file shown as %q with ID %q, want p/a.tck for both", f.Filename, f.ID)
	}
	if res.Files.Lookup("p/a.tck") != f {
		t.Errorf("the file is not found by its ID")
	}
}

// TestStopAfterLexAndParse checks a dump stops loading where it asks,
// having read every file of the target and nothing more.
func TestStopAfterLexAndParse(t *testing.T) {
	fsys := fstest.MapFS{
		"p/a.tck": file("package main\n"),
		"p/b.tck": file("package other\n"),
	}
	lexed, err := (&loader.Loader{FS: loader.FromFS(fsys), Stop: loader.StageLex}).Dir("p")
	if err != nil {
		t.Fatal(err)
	}
	if len(lexed.Tokens) != 2 || lexed.Program != nil || len(lexed.Diags) > 0 {
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
