package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/typeck"
)

// parseFile lexes and parses src as the file name, failing the test on any
// diagnostic.
func parseFile(t *testing.T, set *source.Set, name, src string) *ast.File {
	t.Helper()
	f := &source.File{ID: source.FileID(name), Filename: name, Data: []byte(src)}
	set.Add(f)
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatalf("lex: %s", diag.RenderAllPretty(f, diags))
	}
	file, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
	if len(diags) > 0 {
		t.Fatalf("parse: %s", diag.RenderAllPretty(f, diags))
	}
	return file
}

// checkWithLibrary type-checks a program of two packages: one with the
// import path libPath and the source lib, and a main package that does
// not import it. It returns the checker's diagnostics.
func checkWithLibrary(t *testing.T, libPath, lib string) []diag.Diagnostic {
	t.Helper()
	set := source.NewSet()
	libFile := parseFile(t, set, "lib.tck", lib)
	mainFile := parseFile(t, set, "main.tck", "package main\n\nfunc main() {\n}\n")
	prog := &ast.Program{Packages: []*ast.Package{
		{Name: libFile.Package, Path: libPath, Files: []*ast.File{libFile}},
		{Name: "main", Path: "main", Files: []*ast.File{mainFile}},
	}}
	if diags := resolver.New(prog, stdtest.MustBuiltins()).Resolve(); len(diags) > 0 {
		t.Fatalf("resolve: %s", diag.RenderAllPretty(set, diags))
	}
	return typeck.New(prog, set, stdtest.MustBuiltins()).Check()
}

// TestTackPrefixIsTheStandardLibrarys checks a package of the standard
// library may give an extern func a C name starting tack_, by its
// convention tack_<package>_<name>, and any other package may not.
func TestTackPrefixIsTheStandardLibrarys(t *testing.T) {
	lib := "package os\n\nextern func tack_os_write(fd int32) int64\n\n" +
		"extern(\"tack_os_exit\") func quit(code int32)\n\n" +
		"pub func Write(fd int32) int64 {\n    quit(0)\n    return tack_os_write(fd)\n}\n"
	for _, path := range []string{"std/os", "std"} {
		if diags := checkWithLibrary(t, path, lib); len(diags) > 0 {
			t.Errorf("in %s: %+v, want none", path, diags)
		}
	}
	for _, path := range []string{"calc/os", "stdx/os", "os"} {
		diags := checkWithLibrary(t, path, lib)
		if len(diags) != 2 ||
			!strings.Contains(diags[0].Message, "reserved for the standard library") {
			t.Errorf("in %s: %+v, want two tack_ errors", path, diags)
		}
	}
}
