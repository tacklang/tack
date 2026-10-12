package resolver_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
)

// Names inside ! && and || are bound like any other, and count as reads.
func TestBindsNamesInsideLogicalOperators(t *testing.T) {
	src := "package main\n\nfunc main() {\n    a := 1 < 2\n    b := 2 < 1\n    if !a && (b || !!a) {\n    }\n}\n"
	file, diags := resolveTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %q, want no diagnostics", messagesOf(diags))
	}

	body := file.Decls[0].(*ast.Func).Body
	a := body[0].(*ast.Var)
	cond := body[2].(*ast.If).Cond.(*ast.Binary)
	if id := cond.X.(*ast.Unary).X.(*ast.Ident); id.Def != a {
		t.Errorf("a under ! bound to %v, want its declaration", id.Def)
	}
}

// A name under ! that is not declared is reported.
func TestReportsUndefinedNameUnderNot(t *testing.T) {
	_, diags := resolveTree(t, "package main\n\nfunc main() {\n    if !missing {\n    }\n}\n")
	if got := messagesOf(diags); len(got) != 1 || got[0] != "undefined: missing" {
		t.Errorf("got %q, want undefined: missing", got)
	}
}
