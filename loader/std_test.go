package loader_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/loader"
)

// stdFS is a module, calc, beside a standard library in lib/std: fmt,
// which imports std/text, and text, which has a C file. The main package
// at calc/cmd imports std/fmt, and a single file, single.tck, does too.
func stdFS() fstest.MapFS {
	return fstest.MapFS{
		"calc/tack.mod": file("module calc\n"),
		"calc/cmd/main.tck": file("package main\n\nimport fmt from \"std/fmt\"\n" +
			"import lexer from \"calc/lexer\"\n\nfunc main() { fmt.P(lexer.F()) }\n"),
		"calc/lexer/lexer.tck": file("package lexer\n\nimport fmt from \"std/fmt\"\n\n" +
			"pub func F() string { fmt.P(\"x\")\nreturn \"y\" }\n"),
		"single.tck": file(
			"package main\n\nimport fmt from \"std/fmt\"\n\nfunc main() { fmt.P(\"x\") }\n",
		),
		"lib/std/tack.mod": file("module std\n"),
		"lib/std/fmt/fmt.tck": file("package fmt\n\nimport text from \"std/text\"\n\n" +
			"pub func P(s string) { text.W(s) }\n"),
		"lib/std/text/text.tck": file("package text\n\nlib func W(s string) { print(s) }\n"),
		"lib/std/text/text.c":   file("int text;\n"),
	}
}

// loadWithStd loads target, a directory, or a file when it ends in .tck, from
// fsys with std at dir in it, and fails the test on an error.
func loadWithStd(t *testing.T, fsys fstest.MapFS, dir, target string) *loader.Result {
	t.Helper()
	l := &loader.Loader{FS: loader.FromFS(fsys), Std: &loader.Std{Dir: dir}}
	var res *loader.Result
	var err error
	if strings.HasSuffix(target, ".tck") {
		res, err = l.File(target)
	} else {
		res, err = l.Dir(target)
	}
	if err != nil {
		t.Fatalf("loading %s: %v", target, err)
	}
	return res
}

// packages describes each package of res's program as path:module:dir,
// in program order.
func packages(res *loader.Result) string {
	got := make([]string, 0, len(res.Program.Packages))
	for _, pkg := range res.Program.Packages {
		got = append(got, pkg.Path+":"+pkg.Module+":"+pkg.Dir)
	}
	return strings.Join(got, " ")
}

// TestStdImportsResolveInStd checks an import path starting std names a
// directory below std's, in the module std, whichever module imports it,
// that a std package imports another, that a package imported twice is
// loaded once, and that a std package's C files are recorded as a user
// package's are.
func TestStdImportsResolveInStd(t *testing.T) {
	res := loadWithStd(t, stdFS(), "lib/std", "calc/cmd")
	if len(res.Diags) > 0 {
		t.Fatalf("diagnostics: %v", res.Diags)
	}
	want := "std/text:std:lib/std/text std/fmt:std:lib/std/fmt " +
		"calc/lexer:calc:calc/lexer calc/cmd:calc:calc/cmd"
	if got := packages(res); got != want {
		t.Errorf("packages = %s\nwant       %s", got, want)
	}
	text := res.Program.Packages[0]
	if strings.Join(text.CFiles, " ") != "lib/std/text/text.c" {
		t.Errorf("std/text's C files = %q", text.CFiles)
	}
}

// TestSingleFileImportsStd checks a single file, which has no module, can
// import std, and its std packages are in the module std.
func TestSingleFileImportsStd(t *testing.T) {
	res := loadWithStd(t, stdFS(), "lib/std", "single.tck")
	if len(res.Diags) > 0 {
		t.Fatalf("diagnostics: %v", res.Diags)
	}
	if got, want := packages(
		res,
	), "std/text:std:lib/std/text std/fmt:std:lib/std/fmt main::."; got != want {
		t.Errorf("packages = %s, want %s", got, want)
	}
}

// TestStdFromItsOwnFS checks std read through an FS of its own, apart
// from the program's: its files are named by their paths in that FS.
func TestStdFromItsOwnFS(t *testing.T) {
	prog := fstest.MapFS{
		"tack.mod": file("module calc\n"),
		"main.tck": file(
			"package main\n\nimport fmt from \"std/fmt\"\n\nfunc main() { fmt.P(\"x\") }\n",
		),
	}
	lib := fstest.MapFS{
		"std/fmt/fmt.tck": file("package fmt\n\npub func P(s string) { print(s) }\n"),
	}
	l := &loader.Loader{
		FS:  loader.FromFS(prog),
		Std: &loader.Std{FS: loader.FromFS(lib), Dir: "std"},
	}
	res, err := l.Dir(".")
	if err != nil || len(res.Diags) > 0 {
		t.Fatalf("loading: %v %v", err, res.Diags)
	}
	if got, want := packages(res), "std/fmt:std:std/fmt calc:calc:."; got != want {
		t.Errorf("packages = %s, want %s", got, want)
	}
	if res.Files.Lookup("std/fmt/fmt.tck") == nil {
		t.Errorf("std/fmt's file is not recorded under its path")
	}
}

// messages returns the message of each of diags.
func messages(diags []diag.Diagnostic) string {
	got := make([]string, 0, len(diags))
	for _, d := range diags {
		got = append(got, d.Message)
	}
	return strings.Join(got, "\n")
}

// stdImportCase is a program whose one bad import, of or from std, is
// reported with the message want.
type stdImportCase struct {
	files fstest.MapFS
	std   *loader.Std
	name  string
	want  string
}

// stdImportCases are TestStdImportErrors' cases.
var stdImportCases = []stdImportCase{
	{
		name: "unknown std package",
		files: fstest.MapFS{
			"tack.mod":    file("module calc\n"),
			"main.tck":    file("package main\nimport x from \"std/nosuch\"\nfunc main() {}\n"),
			"std/fmt/a.c": file("int a;\n"),
		},
		std:  &loader.Std{Dir: "std"},
		want: "cannot find package std/nosuch: no directory std/nosuch in module std",
	},
	{
		name: "std package with no Tack files",
		files: fstest.MapFS{
			"tack.mod":    file("module calc\n"),
			"main.tck":    file("package main\nimport x from \"std/fmt\"\nfunc main() {}\n"),
			"std/fmt/a.c": file("int a;\n"),
		},
		std:  &loader.Std{Dir: "std"},
		want: "cannot find package std/fmt: no .tck files in std/fmt",
	},
	{
		name: "std/builtin",
		files: fstest.MapFS{
			"tack.mod": file("module calc\n"),
			"main.tck": file(
				"package main\nimport b from \"std/builtin\"\nfunc main() {}\n",
			),
			"std/builtin/builtin.tck": file("package builtin\n"),
		},
		std:  &loader.Std{Dir: "std"},
		want: "std/builtin documents the builtins and cannot be imported",
	},
	{
		name: "no std given",
		files: fstest.MapFS{
			"tack.mod": file("module calc\n"),
			"main.tck": file("package main\nimport f from \"std/fmt\"\nfunc main() {}\n"),
		},
		want: "cannot import std/fmt: there is no standard library",
	},
	{
		name: "std imports the user's module",
		files: fstest.MapFS{
			"tack.mod": file("module calc\n"),
			"main.tck": file(
				"package main\nimport f from \"std/fmt\"\nfunc main() {}\n",
			),
			"lexer/lexer.tck": file("package lexer\n"),
			"std/fmt/fmt.tck": file("package fmt\nimport l from \"calc/lexer\"\n"),
		},
		std:  &loader.Std{Dir: "std"},
		want: "cannot import calc/lexer: a package of std can import only std packages",
	},
	{
		name: "std imports a package of no module",
		files: fstest.MapFS{
			"main.tck": file(
				"package main\nimport f from \"std/fmt\"\nfunc main() {}\n",
			),
			"std/fmt/fmt.tck": file("package fmt\nimport o from \"other/x\"\n"),
		},
		std:  &loader.Std{Dir: "std"},
		want: "cannot import other/x: a package of std can import only std packages",
	},
	{
		name: "malformed std path",
		files: fstest.MapFS{
			"tack.mod": file("module calc\n"),
			"main.tck": file("package main\nimport f from \"std/./fmt\"\nfunc main() {}\n"),
		},
		std:  &loader.Std{Dir: "std"},
		want: "invalid import path std/./fmt: a part cannot be . or ..",
	},
	{
		name: "std package named wrong",
		files: fstest.MapFS{
			"tack.mod": file("module calc\n"),
			"main.tck": file(
				"package main\nimport f from \"std/fmt\"\nfunc main() {}\n",
			),
			"std/fmt/fmt.tck": file("package other\n"),
		},
		std:  &loader.Std{Dir: "std"},
		want: "package other does not match its directory; expected package fmt",
	},
}

// TestStdImportErrors checks every way an import of, or from, std fails,
// each reported at the import's path, with the program still built, the
// import bound to no package and the error counted on it.
func TestStdImportErrors(t *testing.T) {
	for _, tc := range stdImportCases {
		t.Run(tc.name, func(t *testing.T) {
			target := "."
			if tc.files["tack.mod"] == nil {
				target = "main.tck"
			}
			l := &loader.Loader{FS: loader.FromFS(tc.files), Std: tc.std}
			var res *loader.Result
			var err error
			if target == "main.tck" {
				res, err = l.File(target)
			} else {
				res, err = l.Dir(target)
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.Program == nil || res.Program.Errors != len(res.Diags) ||
				res.Stage != loader.StageLoad || messages(res.Diags) != tc.want {
				t.Errorf(
					"stage %q, diagnostics\n%s\nwant\n%s",
					res.Stage,
					messages(res.Diags),
					tc.want,
				)
			}
		})
	}
}

// TestStdRootIsTheModuleStd checks a directory in std, read through the
// loader's own FS, is in the module std, whose tack.mod, module std, no
// other module may have, is not read; its imports of std resolve in it,
// and its imports of anything else are errors. A copy of std elsewhere is
// another module, whose tack.mod is rejected.
func TestStdRootIsTheModuleStd(t *testing.T) {
	fsys := fstest.MapFS{
		"std/tack.mod": file("module std\n"),
		"std/tool/main.tck": file(
			"package main\nimport f from \"std/fmt\"\nfunc main() { f.P() }\n",
		),
		"std/fmt/fmt.tck":    file("package fmt\npub func P() {}\n"),
		"std/bad/main.tck":   file("package main\nimport l from \"calc/lexer\"\nfunc main() {}\n"),
		"copy/tack.mod":      file("module std\n"),
		"copy/tool/main.tck": file("package main\nfunc main() {}\n"),
	}
	res := loadWithStd(t, fsys, "std", "std/tool")
	if len(res.Diags) > 0 {
		t.Fatalf("diagnostics: %v", res.Diags)
	}
	if got, want := packages(res), "std/fmt:std:std/fmt std/tool:std:std/tool"; got != want {
		t.Errorf("packages = %s, want %s", got, want)
	}

	res = loadWithStd(t, fsys, "std", "std/bad")
	if want := "cannot import calc/lexer: a package of std can import only std packages"; messages(
		res.Diags,
	) != want {
		t.Errorf("diagnostics\n%s\nwant\n%s", messages(res.Diags), want)
	}

	res = loadWithStd(t, fsys, "std", "copy/tool")
	if want := "module name std is reserved for the standard library"; messages(res.Diags) != want {
		t.Errorf("diagnostics\n%s\nwant\n%s", messages(res.Diags), want)
	}

	// Read through an FS of its own, std is never the module of a
	// directory to build, even one at the same path.
	l := &loader.Loader{
		FS:  loader.FromFS(fsys),
		Std: &loader.Std{FS: loader.FromFS(fsys), Dir: "std"},
	}
	res, err := l.Dir("std/tool")
	if err != nil {
		t.Fatal(err)
	}
	if want := "module name std is reserved for the standard library"; messages(res.Diags) != want {
		t.Errorf("diagnostics\n%s\nwant\n%s", messages(res.Diags), want)
	}
}

// TestStdPathTarget checks an import path target in std, from a directory
// in std, names a package of std.
func TestStdPathTarget(t *testing.T) {
	fsys := fstest.MapFS{
		"std/tool/main.tck": file("package main\nfunc main() {}\n"),
	}
	l := &loader.Loader{FS: loader.FromFS(fsys), Std: &loader.Std{Dir: "std"}}
	res, err := l.Path("std/tool", "std/tool")
	if err != nil || len(res.Diags) > 0 {
		t.Fatalf("loading: %v %v", err, res.Diags)
	}
	if main := res.Program.Main(); main.Path != "std/tool" || main.Module != "std" {
		t.Errorf("main package %s in module %s", main.Path, main.Module)
	}
}
