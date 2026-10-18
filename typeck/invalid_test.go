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

// checkAfterErrors runs the front end on src as the driver does, every
// stage whatever an earlier one reported, on one program, and returns it
// with the type checker's diagnostics. When counted is false the errors
// the parser and the resolver reported are left off the program's count,
// as if the checker had been handed the tree with nothing said of them.
func checkAfterErrors(t *testing.T, src string, counted bool) (*ast.Program, []diag.Diagnostic) {
	t.Helper()
	f := &source.File{ID: source.FileID("test"), Filename: "case.tck", Data: []byte(src)}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatalf("lex: %s", diag.RenderAllPretty(f, diags))
	}
	file, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
	prog := ast.SingleFile(file)
	prog.Errors = len(diags)
	resolver.New(prog, stdtest.MustBuiltins()).Resolve()
	if !counted {
		prog.Errors = 0
	}
	return prog, typeck.New(prog, f, stdtest.MustBuiltins()).Check()
}

// messagesOf returns the messages of diags, one per line.
func messagesOf(diags []diag.Diagnostic) string {
	var b strings.Builder
	for _, d := range diags {
		b.WriteString(d.Message + "\n")
	}
	return b.String()
}

// TestUnboundNameIsSilentAfterAnError checks that a name the resolver
// bound to ast.Unbound, here lost to a bad declaration, is reported
// nothing about, nor is anything that uses it, and that the checker
// counts the errors on the program and on its type table.
func TestUnboundNameIsSilentAfterAnError(t *testing.T) {
	src := "package main\n\nfunc helper( {\n}\n\nfunc main() {\n" +
		"    x := helper() + 1\n    var s string = helper()\n    if x == s {\n    }\n}\n"
	prog, diags := checkAfterErrors(t, src, true)
	if len(diags) > 0 {
		t.Errorf("diagnostics:\n%swant none", messagesOf(diags))
	}
	if prog.Errors != 1 || prog.Types.Errors != 1 {
		t.Errorf("program counts %d errors, its table %d; want the syntax error, 1",
			prog.Errors, prog.Types.Errors)
	}
}

// TestUnboundNameIsReportedWithNoEarlierError checks the checker's guard:
// a name bound to ast.Unbound, or a bad node, in a program no earlier
// stage reported an error about is reported, so it can never pass as
// valid and have code generated from it.
func TestUnboundNameIsReportedWithNoEarlierError(t *testing.T) {
	src := "package main\n\nfunc helper( {\n}\n\nfunc main() {\n    helper()\n    x := 1 1\n}\n"
	_, diags := checkAfterErrors(t, src, false)
	got := messagesOf(diags)
	for _, want := range []string{
		"undefined: helper\n",
		"syntax error: this code could not be parsed\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("diagnostics:\n%swant %q among them", got, want)
		}
	}
}

// TestDamagedBodyKeepsWhatComesBefore checks a function with a bad
// statement: what is reported before the first one is kept, what is
// reported after it is dropped, since the bad statement may have been
// what would make it valid, and the function is not reported for not
// ending in a return, which the bad statement may have been.
func TestDamagedBodyKeepsWhatComesBefore(t *testing.T) {
	src := "package main\n\nfunc f(xs []int64, i int64) int64 {\n" +
		"    var s string = 1\n    print(s)\n    if i < 0 || i >= len(xs) + {\n        return 0\n    }\n" +
		"    var t string = 2\n    print(t)\n    n := xs[i]\n    print(n)\n}\n\nfunc main() {\n}\n"
	_, diags := checkAfterErrors(t, src, true)
	want := "cannot use int64 as string in declaration of s\n"
	if got := messagesOf(diags); got != want {
		t.Errorf("diagnostics:\n%swant\n%s", got, want)
	}
}
