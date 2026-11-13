package testmain

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
)

// decoded returns the value of lit, a Tack string literal, as the lexer
// reads it, failing the test when it does not lex as one string.
func decoded(t *testing.T, lit string) string {
	t.Helper()
	f := &source.File{ID: "q.tck", Filename: "q.tck", Data: []byte(lit)}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatalf("%s does not lex: %v", lit, diags)
	}
	toks := lex.Tokens()
	if len(toks) < 1 || toks[0].Kind() != lexer.Str {
		t.Fatalf("%s lexes as %v", lit, toks)
	}
	v, ok := toks[0].Value().(string)
	if !ok {
		t.Fatalf("%s lexes to %v", lit, toks[0].Value())
	}
	return v
}

// TestQuoteEveryByte checks a string of every byte, and the empty one,
// is quoted in printable ASCII, every other byte and " and \ escaped,
// and reads back exactly.
func TestQuoteEveryByte(t *testing.T) {
	var all strings.Builder
	for c := range 256 {
		all.WriteByte(byte(c))
	}
	for _, s := range []string{"", all.String(), `a"b\c`, "\xc3\xa9"} {
		lit := quote(s)
		for i := 0; i < len(lit); i++ {
			if lit[i] < 0x20 || lit[i] >= 0x7f {
				t.Fatalf("quote(%q) holds byte %#x", s, lit[i])
			}
		}
		if got := decoded(t, lit); got != s {
			t.Errorf("quote(%q) = %s reads back as %q", s, lit, got)
		}
	}
}

// FuzzQuote checks any string quotes to a literal that reads back as it.
func FuzzQuote(f *testing.F) {
	f.Add("")
	f.Add("\"\\\x00\xff")
	f.Fuzz(func(t *testing.T, s string) {
		if got := decoded(t, quote(s)); got != s {
			t.Errorf("quote(%q) reads back as %q", s, got)
		}
	})
}

// parsed returns the package of the files srcs, by name, parsed, each
// named _test.tck marked a test file, and the files.
func parsed(t *testing.T, srcs map[string]string, order ...string) (*ast.Package, *source.Set) {
	t.Helper()
	files := source.NewSet()
	pkg := &ast.Package{Path: "m/p"}
	for _, name := range order {
		f := &source.File{ID: source.FileID(name), Filename: name, Data: []byte(srcs[name])}
		files.Add(f)
		lex := lexer.New(f)
		if diags := lex.Lex(); len(diags) > 0 {
			t.Fatal(diags)
		}
		tree, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
		if len(diags) > 0 {
			t.Fatal(diags)
		}
		tree.Test = strings.HasSuffix(name, "_test.tck")
		pkg.Files = append(pkg.Files, tree)
		pkg.Name = tree.Package
	}
	return pkg, files
}

// TestSourceOfTests checks the generated main names every test, a
// function of a test file named test_, in file and then source order,
// with its place, and passes the options as literals; a method, a
// function of another file, and one with another name are no tests.
func TestSourceOfTests(t *testing.T) {
	pkg, files := parsed(t, map[string]string{
		"p/a_test.tck": "package p\n\nfunc test_b(mut t T) error {\n    return noerror\n}\n\n" +
			"func (p P) test_m(mut t T) error {\n    return noerror\n}\n\nfunc helper() {\n}\n",
		"p/b.tck":      "package p\n\nfunc test_not(mut t T) error {\n    return noerror\n}\n",
		"p/c_test.tck": "package p\n\nfunc test_a(mut t T) error {\n    return noerror\n}\n",
	}, "p/a_test.tck", "p/b.tck", "p/c_test.tck")
	var names []string
	for _, fn := range Tests(pkg) {
		names = append(names, fn.Name)
	}
	if got := strings.Join(names, " "); got != "test_b test_a" {
		t.Errorf("Tests = %s, want test_b test_a", got)
	}
	src := string(Source(pkg, files, Options{Pkg: "m/p", Pattern: "x\"", Verbose: true}))
	for _, want := range []string{
		"package p\n\nimport tack_testing_ from \"std/testing\"\n\n",
		"var tack_test_names_ = [2]string{\"test_b\", \"test_a\"}\n",
		"    mut r := tack_testing_.start(\"m/p\", \"x\\x22\", true, tack_test_names_)\n",
		"    mut r.run(\"test_b\", \"p/a_test.tck\", 3, 6, \"02 |\\x09\\x0a03 |\\x09func test_b",
		"    mut r.run(\"test_a\", \"p/c_test.tck\", 3, 6, ",
		"    exit(mut r.finish())\n}\n",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("source does not hold %q:\n%s", want, src)
		}
	}
	if strings.Contains(src, `"test_m"`) || strings.Contains(src, `"test_not"`) {
		t.Errorf("source names a method or a function of a file that is no test file:\n%s", src)
	}
}

// TestSourceOfStdTesting checks std/testing's own tests call start
// unqualified, importing nothing, and a package of no tests gets an
// empty array.
func TestSourceOfStdTesting(t *testing.T) {
	pkg, files := parsed(t, map[string]string{"t.tck": "package testing\n"}, "t.tck")
	pkg.Path = "std/testing"
	src := string(Source(pkg, files, Options{Pkg: "std/testing"}))
	if strings.Contains(src, "import") || !strings.Contains(src, "mut r := start(") ||
		!strings.Contains(src, "= [0]string{}\n") {
		t.Errorf("source:\n%s", src)
	}
}

// TestEntry checks Entry finds the generated main's function in the
// generated file only, and is nil without one.
func TestEntry(t *testing.T) {
	pkg, _ := parsed(t, map[string]string{
		"a.tck":  "package p\n\nfunc tack_test_main_() {\n}\n",
		FileName: "package p\n\nfunc other() {\n}\n\nfunc tack_test_main_() {\n}\n",
	}, "a.tck", FileName)
	prog := &ast.Program{Packages: []*ast.Package{pkg}}
	fn := Entry(prog)
	if fn == nil || fn.Span.File != FileName {
		t.Errorf("Entry = %v", fn)
	}
	prog.Packages[0].Files = prog.Packages[0].Files[:1]
	if fn := Entry(prog); fn != nil {
		t.Errorf("Entry without a generated file = %v", fn)
	}
	if fn := Entry(&ast.Program{}); fn != nil {
		t.Errorf("Entry of no package = %v", fn)
	}
}

// TestDiagnostics checks what is reported in the generated file is
// dropped when another diagnostic is an error, kept, warnings with it,
// and made internal errors at the package's first file otherwise, a
// warning there included.
func TestDiagnostics(t *testing.T) {
	pkg := &ast.Package{Files: []*ast.File{{Span: ast.Span{File: "p/a.tck"}}}}
	gen := diag.Diagnostic{Severity: diag.Error, Message: "x is undefined", Span: ast.Span{File: FileName}}
	genWarning := diag.Diagnostic{Severity: diag.Warning, Message: "unused", Span: ast.Span{File: FileName}}
	user := diag.Diagnostic{Severity: diag.Error, Message: "bad", Span: ast.Span{File: "p/a.tck", Start: 3}}
	warning := diag.Diagnostic{Severity: diag.Warning, Message: "meh", Span: ast.Span{File: "p/a.tck"}}

	got := Diagnostics([]diag.Diagnostic{gen, user, genWarning, warning}, pkg)
	if len(got) != 2 || got[0].Message != "bad" || got[1].Message != "meh" {
		t.Errorf("with an error elsewhere: %v", got)
	}
	got = Diagnostics([]diag.Diagnostic{warning, gen, genWarning}, pkg)
	want := "internal error: tack test generated a main that does not compile: "
	if len(got) != 3 || got[0].Message != warning.Message ||
		got[1].Message != want+"x is undefined" || got[1].Severity != diag.Error ||
		got[1].Span != (ast.Span{File: "p/a.tck"}) || got[2].Message != want+"unused" ||
		got[2].Severity != diag.Error {
		t.Errorf("with no error elsewhere: %v", got)
	}
	if got := Diagnostics([]diag.Diagnostic{warning}, pkg); len(got) != 1 || got[0].Message != warning.Message {
		t.Errorf("with nothing generated: %v", got)
	}
	if got := Diagnostics([]diag.Diagnostic{gen}, nil); len(got) != 1 || got[0].Span != (ast.Span{}) {
		t.Errorf("with no package: %v", got)
	}
}

// TestClashes checks every package-level function, variable and type
// named like a name the generated main declares, and an import named
// like its function or array, is reported at its name, while a method,
// an import named like its import of std/testing, and the generated file
// itself are not.
func TestClashes(t *testing.T) {
	pkg, _ := parsed(t, map[string]string{
		"a.tck": "package p\n\nimport tack_test_names_ from \"std/a\"\nimport tack_testing_ from \"std/b\"\n\n" +
			"var tack_test_main_ = 1\n\ntype tack_testing_ struct {\n    n int64\n}\n\n" +
			"func (x X) tack_test_main_() {\n}\n\nfunc tack_test_names_() {\n}\n",
		FileName: "package p\n\nfunc tack_test_main_() {\n}\n",
	}, "a.tck", FileName)
	var got []string
	for _, d := range Clashes(pkg) {
		if d.Severity != diag.Error || d.Span.File != "a.tck" {
			t.Errorf("%v", d)
		}
		got = append(got, d.Message)
	}
	const tail = " is declared by the main tack test generates; rename it"
	want := []string{"tack_test_names_" + tail, "tack_test_main_" + tail, "tack_testing_" + tail,
		"tack_test_names_" + tail}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("Clashes = %q, want %q", got, want)
	}
	if Clashes(nil) != nil {
		t.Error("Clashes(nil) is not nil")
	}
}

// TestDiagnosticsDropGeneratedNotes checks a note located in the
// generated file is dropped from a diagnostic that is kept.
func TestDiagnosticsDropGeneratedNotes(t *testing.T) {
	d := diag.Diagnostic{
		Severity: diag.Error, Message: "x", Span: ast.Span{File: "a.tck"},
		Related: []diag.Diagnostic{
			{Severity: diag.Note, Message: "gen", Span: ast.Span{File: FileName}},
			{Severity: diag.Note, Message: "user", Span: ast.Span{File: "b.tck"}},
		},
	}
	got := Diagnostics([]diag.Diagnostic{d}, nil)
	if len(got) != 1 || len(got[0].Related) != 1 || got[0].Related[0].Message != "user" {
		t.Errorf("Diagnostics = %v", got)
	}
}
