package testmain

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/source"
)

// FileName is the name, and the source.FileID, of the generated main: no
// file on disk can be shown by it, since the loader shows every file it
// reads by its path, and no path is wrapped in angle brackets.
const FileName = "<tack test main>"

// testPrefix starts the name of every test.
const testPrefix = "test_"

// stdTesting is the import path of std/testing, which the generated main
// calls.
const stdTesting = "std/testing"

// The names the generated main declares: its function, the global array
// of every test's name, and the name it imports std/testing by. Each
// ends in an underscore, and starts with tack_, which no other name in
// a package is likely to; one the package declares too is reported at
// its own declaration (see Clashes).
const (
	mainName    = "tack_test_main_"
	namesName   = "tack_test_names_"
	testingName = "tack_testing_"
)

// Clashes reports every name of pkg, a package loaded for its tests, its
// generated main aside, that a name the generated main declares would
// clash with, at the declaration: a package-level function, variable or
// type named like any of them, and an import named like the generated
// main's function or array, which are package-level and so share a
// scope with every file's imports. An import named like the generated
// main's import of std/testing does not clash, as imports are each
// file's own. It returns nil for a nil package.
func Clashes(pkg *ast.Package) []diag.Diagnostic {
	if pkg == nil {
		return nil
	}
	packageLevel := map[string]bool{mainName: true, namesName: true, testingName: true}
	imported := map[string]bool{mainName: true, namesName: true}
	var diags []diag.Diagnostic
	clash := func(name string, span ast.Span) {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Message:  name + " is declared by the main tack test generates; rename it",
			Span:     span,
		})
	}
	for _, f := range pkg.Files {
		if f.Span.File == FileName {
			continue
		}
		for _, imp := range f.Imports {
			if imported[imp.Name] {
				clash(imp.Name, imp.NameSpan)
			}
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.Func:
				if !d.Method && packageLevel[d.Name] {
					clash(d.Name, d.NameSpan)
				}
			case *ast.Var:
				if packageLevel[d.Name] {
					clash(d.Name, d.NameSpan)
				}
			case *ast.TypeDecl:
				if packageLevel[d.Name] {
					clash(d.Name, d.NameSpan)
				}
			}
		}
	}
	return diags
}

// Options are what the generated main is told of the run, which it passes
// to std/testing's start as literals.
type Options struct {
	// Pkg is the name the last line of the run gives the package.
	Pkg string
	// Pattern is -run's: only the tests whose names hold it run, every
	// one when it is empty.
	Pattern string
	// Verbose is -v: a line for each test run.
	Verbose bool
}

// Tests returns the tests of pkg, a package loaded for its tests: every
// function, not a method, of a test file of pkg whose name starts with
// test_, in file order and then source order, the order they run in.
// One not declared as a test must be is among them; the type checker
// reports it (see package typeck).
func Tests(pkg *ast.Package) []*ast.Func {
	var tests []*ast.Func
	for _, f := range pkg.Files {
		if !f.Test {
			continue
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.Func); ok && !fn.Method && strings.HasPrefix(fn.Name, testPrefix) {
				tests = append(tests, fn)
			}
		}
	}
	return tests
}

// Source returns the generated main of pkg, a package loaded for its
// tests whose files files holds, as Go's go test generates one: a file
// of package pkg that declares the global array of every test's name and
// a function that starts std/testing's runner, runs each test of Tests
// through it, and exits with the status its finish gives. Each test's
// place is its name's: the file diagnostics show it by, its line and
// column, and the excerpt a diagnostic there prints. Every string is a
// literal, as start and run require, written in printable ASCII with
// any other byte, and every " and \, as \xHH, so any name, path or
// pattern reads back exactly.
func Source(pkg *ast.Package, files source.Files, opts Options) []byte {
	tests := Tests(pkg)
	qual := testingName + "."
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", pkg.Name)
	if pkg.Path == stdTesting {
		qual = ""
	} else {
		fmt.Fprintf(&b, "import %s from %s\n\n", testingName, quote(stdTesting))
	}
	fmt.Fprintf(&b, "var %s = [%d]string{", namesName, len(tests))
	for i, fn := range tests {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(quote(fn.Name))
	}
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "func %s() {\n", mainName)
	fmt.Fprintf(&b, "    mut r := %sstart(%s, %s, %t, %s)\n",
		qual, quote(opts.Pkg), quote(opts.Pattern), opts.Verbose, namesName)
	for _, fn := range tests {
		file, line, col, excerpt := place(files, fn.NameSpan)
		fmt.Fprintf(&b, "    mut r.run(%s, %s, %d, %d, %s, %s)\n",
			quote(fn.Name), quote(file), line, col, quote(excerpt), fn.Name)
	}
	b.WriteString("    exit(mut r.finish())\n")
	b.WriteString("}\n")
	return []byte(b.String())
}

// place returns where span starts, as a diagnostic there shows it: its
// file's name, line and column, and the excerpt below its header.
func place(files source.Files, span ast.Span) (string, int, int, string) {
	f := files.Lookup(span.File)
	if f == nil {
		panic("testmain: a test outside the files given")
	}
	line, col := f.Position(span.Start)
	return f.Filename, line, col, diag.Excerpt(f, span)
}

// quote returns s as a Tack string literal: printable ASCII as it is,
// and any other byte, " and \ among them, as \xHH.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c < 0x7f && c != '"' && c != '\\' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, `\x%02x`, c)
	}
	b.WriteByte('"')
	return b.String()
}

// withoutGenerated returns notes without those located in the generated
// main, which is never shown.
func withoutGenerated(notes []diag.Diagnostic) []diag.Diagnostic {
	var kept []diag.Diagnostic
	for _, n := range notes {
		if n.Span.File != FileName {
			kept = append(kept, n)
		}
	}
	return kept
}

// Entry returns the generated main's function in prog, a program loaded
// for its tests, or nil when its last package has no generated main, as
// when a problem stopped the load before it was parsed, or the file did
// not parse into it.
func Entry(prog *ast.Program) *ast.Func {
	pkg := prog.Main()
	if pkg == nil {
		return nil
	}
	for _, f := range pkg.Files {
		if f.Span.File != FileName {
			continue
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.Func); ok && fn.Name == mainName && !fn.Method {
				return fn
			}
		}
	}
	return nil
}

// Diagnostics returns diags, what the stages reported of a program
// loaded for its tests, with those located in the generated main dealt
// with: dropped when any other is an error, since a mistake in a test,
// a test declared wrongly among them, is what makes the generated main's
// call of it wrong, and otherwise each turned into an error saying the
// compiler is wrong, at the start of the first file of the package,
// pkg, since the generated file is not shown, with what it said. A
// note of another diagnostic located there is dropped with it.
func Diagnostics(diags []diag.Diagnostic, pkg *ast.Package) []diag.Diagnostic {
	var kept, generated []diag.Diagnostic
	otherErrors := false
	for _, d := range diags {
		if d.Span.File == FileName {
			generated = append(generated, d)
			continue
		}
		d.Related = withoutGenerated(d.Related)
		kept = append(kept, d)
		otherErrors = otherErrors || d.Severity == diag.Error
	}
	if otherErrors || len(generated) == 0 {
		return kept
	}
	at := ast.Span{}
	if pkg != nil && len(pkg.Files) > 0 {
		at.File = pkg.Files[0].Span.File
	}
	for _, d := range generated {
		kept = append(kept, diag.Diagnostic{
			Severity: diag.Error,
			Message:  "internal error: tack test generated a main that does not compile: " + d.Message,
			Span:     at,
		})
	}
	return kept
}
