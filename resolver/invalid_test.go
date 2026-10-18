package resolver_test

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
)

// resolveRecovered parses src, which must have syntax errors, and resolves
// the tree the parser recovered, as the driver does, returning the
// program and the resolver's diagnostics.
func resolveRecovered(t *testing.T, src string) (*ast.Program, []diag.Diagnostic) {
	t.Helper()
	f := &source.File{ID: source.FileID("test"), Filename: "case.tck", Data: []byte(src)}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatalf("lex: %s", diag.RenderAllPretty(f, diags))
	}
	file, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
	if len(diags) == 0 {
		t.Fatalf("parsed with no error; the case needs one")
	}
	prog := ast.SingleFile(file)
	prog.Errors = len(diags)
	return prog, resolver.New(prog, stdtest.MustBuiltins()).Resolve()
}

// identsNamed returns every name in prog written as name.
func identsNamed(prog *ast.Program, name string) []*ast.Ident {
	var ids []*ast.Ident
	for _, f := range prog.Files() {
		ast.Walk(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == name {
				ids = append(ids, id)
			}
			return true
		})
	}
	return ids
}

// messages returns the messages of diags, one per line.
func diagMessages(diags []diag.Diagnostic) string {
	var b strings.Builder
	for _, d := range diags {
		b.WriteString(d.Message + "\n")
	}
	return b.String()
}

// TestNamesLostToABadDeclaration checks that a use of the name a bad
// declaration was declaring is bound to ast.Unbound and not reported,
// while a name no damaged source could have declared still is.
func TestNamesLostToABadDeclaration(t *testing.T) {
	src := "package main\n\nfunc helper( {\n}\n\nfunc main() {\n    helper()\n    other()\n}\n"
	prog, diags := resolveRecovered(t, src)
	if got := diagMessages(diags); got != "undefined: other\n" {
		t.Errorf("diagnostics:\n%swant only undefined: other", got)
	}
	for _, id := range identsNamed(prog, "helper") {
		if id.Def != ast.Unbound {
			t.Errorf("helper at %v is bound to %T, want ast.Unbound", id.Span, id.Def)
		}
	}
}

// TestUnnamedBadDeclarationHidesEveryName checks that a bad declaration
// that got to no name could have declared any name of the package, so no
// undefined name and no main missing is reported.
func TestUnnamedBadDeclarationHidesEveryName(t *testing.T) {
	src := "package main\n\nfunc (\n\nfunc f() {\n    other()\n}\n"
	_, diags := resolveRecovered(t, src)
	if len(diags) > 0 {
		t.Errorf("diagnostics:\n%swant none", diagMessages(diags))
	}
}

// TestBadStatementHidesWhatFollows checks a function with a bad statement:
// a name that names nothing before it is reported, and one after it is
// not, since the bad statement may have declared it; a variable declared
// before it is not reported unused, since it may have read it, and one
// declared after the last one is.
func TestBadStatementHidesWhatFollows(t *testing.T) {
	src := "package main\n\nfunc main() {\n    early()\n    n := 1\n    x := 1 1\n" +
		"    later()\n    y := 2\n}\n"
	_, diags := resolveRecovered(t, src)
	want := "undefined: early\ndeclared and not used: y\n"
	if got := diagMessages(diags); got != want {
		t.Errorf("diagnostics:\n%swant\n%s", got, want)
	}
}

// TestUseOfRejectedNameIsUnbound checks that a name whose declaration
// was reported, shadowing a parameter, is bound to ast.Unbound where it is
// used rather than to the parameter, which the use may not mean, and that
// the parameter is not then reported unused.
func TestUseOfRejectedNameIsUnbound(t *testing.T) {
	src := "package main\n\nfunc f(n int64) int64 {\n    n := \"two\"\n    return n\n}\n\n" +
		"func main() {\n    x := 1 1\n}\n"
	prog, diags := resolveRecovered(t, src)
	if got := diagMessages(diags); got != "n is already declared\n" {
		t.Errorf("diagnostics:\n%swant only n is already declared", got)
	}
	ids := identsNamed(prog, "n")
	if len(ids) != 1 || ids[0].Def != ast.Unbound {
		t.Errorf("the use of n is not bound to ast.Unbound: %v", ids)
	}
}

// TestCallToShadowedBuiltinIsUnbound checks that a call by the name of a
// builtin the package tried to redeclare, which was reported, is bound to
// ast.Unbound, not to the builtin.
func TestCallToShadowedBuiltinIsUnbound(t *testing.T) {
	src := "package main\n\nfunc print() {\n}\n\nfunc main() {\n    print()\n    x := 1 1\n}\n"
	prog, diags := resolveRecovered(t, src)
	if got := diagMessages(diags); got != "duplicate declaration of builtin function\n" {
		t.Errorf("diagnostics:\n%s", got)
	}
	for _, id := range identsNamed(prog, "print") {
		if id.Def != ast.Unbound {
			t.Errorf("the call to print is bound to %T, want ast.Unbound", id.Def)
		}
	}
}

// TestResolveCountsItsErrors checks that Resolve adds the errors it
// reports to the program's count, which keeps code from being generated.
func TestResolveCountsItsErrors(t *testing.T) {
	prog, diags := resolveRecovered(t, "package main\n\nfunc main() {\n    b()\n    x := 1 1\n}\n")
	if len(diags) == 0 || prog.Errors != 1+len(diags) {
		t.Errorf("program counts %d errors; 1 syntax error and %d reported",
			prog.Errors, len(diags))
	}
}
