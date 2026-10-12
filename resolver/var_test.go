package resolver_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
)

// resolveTree parses src, resolves it, and returns the tree with the
// diagnostics. It fails the test if lexing or parsing reports anything.
func resolveTree(t *testing.T, src string) (*ast.File, []diag.Diagnostic) {
	t.Helper()

	f := &source.File{ID: "test", Filename: "case.tck", Data: []byte(src)}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatalf("lex: %s", diag.RenderAllPretty(f, diags))
	}
	file, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
	if len(diags) > 0 {
		t.Fatalf("parse: %s", diag.RenderAllPretty(f, diags))
	}
	return file, resolver.New(file).Resolve()
}

// inMain wraps statements in a main function.
func inMain(body string) string {
	return "package main\n\nfunc main() {\n" + body + "\n}\n"
}

// messagesOf returns the diagnostic messages, in order.
func messagesOf(diags []diag.Diagnostic) []string {
	out := make([]string, len(diags))
	for i, d := range diags {
		out[i] = d.Message
	}
	return out
}

// TestAcceptsValidVariableUse lists programs whose names must all resolve
// with no diagnostics.
func TestAcceptsValidVariableUse(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"read after declaration", "    x := 1\n    exit(x)"},
		{"read in a later declaration", "    x := 1\n    y := x + 1\n    exit(y)"},
		{"read in a condition", "    x := 1\n    if x < 2 {\n    }"},
		{"read in its own assignment", "    mut x := 1\n    x = x + 1"},
		{"read inside a branch", "    x := 1\n    if 1 < 2 {\n        exit(x)\n    }"},
		{
			"same name in sibling branches",
			"    if 1 < 2 {\n        x := 1\n        exit(x)\n    } else {\n        x := 2\n        exit(x)\n    }",
		},
		{
			"same name in two separate ifs",
			"    if 1 < 2 {\n        x := 1\n        exit(x)\n    }\n    if 2 < 3 {\n        x := 2\n        exit(x)\n    }",
		},
		{"named like a builtin", "    print := 1\n    exit(print)"},
		{"zero valued mutable read", "    mut n int64\n    exit(n)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := resolveTree(t, inMain(tt.body)); len(diags) > 0 {
				t.Errorf("got %q, want no diagnostics", messagesOf(diags))
			}
		})
	}
}

// A variable may share a function's name: calls resolve to functions and
// values to variables, as parameters already do.
func TestVariableMayShareAFunctionsName(t *testing.T) {
	src := "package main\n\nfunc f() int64 {\n    return 1\n}\n\nfunc main() {\n    f := f()\n    exit(f)\n}\n"
	if _, diags := resolveTree(t, src); len(diags) > 0 {
		t.Errorf("got %q, want no diagnostics", messagesOf(diags))
	}
}

// invalidVariableUseCases are programs that misuse a variable, with the diagnostics each must produce.
var invalidVariableUseCases = []struct {
	name string
	src  string
	want []string
}{
	{
		"read before declaration",
		inMain("    exit(x)\n    x := 1\n    exit(x)"),
		[]string{"undefined: x"},
	},
	{
		"read in its own initializer",
		inMain("    x := x\n    exit(x)"),
		[]string{"undefined: x"},
	},
	{
		"read after its block ends",
		inMain("    if 1 < 2 {\n        x := 1\n        exit(x)\n    }\n    exit(x)"),
		[]string{"undefined: x"},
	},
	{
		"declared twice in one block",
		inMain("    x := 1\n    x := 2\n    exit(x)"),
		[]string{"x is already declared"},
	},
	{
		"shadows an outer variable",
		inMain(
			"    x := 1\n    if 1 < 2 {\n        x := 2\n        exit(x)\n    }\n    exit(x)",
		),
		[]string{"x is already declared"},
	},
	{
		"shadows a parameter",
		"package main\n\nfunc f(x int64) {\n    x := 1\n    exit(x)\n}\n\nfunc main() {\n    f(1)\n}\n",
		[]string{"x is already declared"},
	},
	{
		"never read",
		inMain("    x := 1"),
		[]string{"declared and not used: x"},
	},
	{
		"only assigned",
		inMain("    mut x := 1\n    x = 2"),
		[]string{"declared and not used: x"},
	},
	{
		"zero valued and never read",
		inMain("    mut x int64"),
		[]string{"declared and not used: x"},
	},
	{
		"every unused variable is reported",
		inMain("    a := 1\n    b := 2\n    if 1 < 2 {\n        c := 3\n    }"),
		[]string{
			"declared and not used: c",
			"declared and not used: a",
			"declared and not used: b",
		},
	},
	{
		"assignment to an undefined name",
		inMain("    y = 1"),
		[]string{"undefined: y"},
	},
	{
		"assignment to a function",
		"package main\n\nfunc f() {\n}\n\nfunc main() {\n    f = 1\n}\n",
		[]string{"undefined: f"},
	},
	{
		"undefined name in an initializer",
		inMain("    x := y\n    exit(x)"),
		[]string{"undefined: y"},
	},
}

// TestRejectsInvalidVariableUse pins the diagnostics, in order, for each way
// of misusing a variable.
func TestRejectsInvalidVariableUse(t *testing.T) {
	tests := invalidVariableUseCases

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := resolveTree(t, tt.src)
			got := messagesOf(diags)
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// A redeclaration points back at the declaration it clashes with.
func TestRedeclarationNotesThePreviousDeclaration(t *testing.T) {
	src := inMain("    x := 1\n    x := 2\n    exit(x)")
	_, diags := resolveTree(t, src)
	if len(diags) == 0 || len(diags[0].Related) != 1 {
		t.Fatalf("got %+v, want a diagnostic with one related note", diags)
	}

	first := strings.Index(src, "x := 1")
	if got := diags[0].Related[0].Span.Start; got != first {
		t.Errorf("note points at %d, want %d", got, first)
	}
}

// Reads and assignments bind to the declaration they name.
func TestBindsVariableReferences(t *testing.T) {
	file, diags := resolveTree(t, inMain("    mut x := 1\n    x = x + 1\n    exit(x)"))
	if len(diags) > 0 {
		t.Fatalf("got %q", messagesOf(diags))
	}

	body := file.Decls[0].(*ast.Func).Body
	decl := body[0].(*ast.Var)
	assign := body[1].(*ast.Assign)

	if assign.Target.(*ast.Ident).Def != decl {
		t.Errorf(
			"assignment target bound to %v, want the declaration",
			assign.Target.(*ast.Ident).Def,
		)
	}
	if read := assign.Value.(*ast.Binary).X.(*ast.Ident); read.Def != decl {
		t.Errorf("read bound to %v, want the declaration", read.Def)
	}
}

// Parameters are not checked for use; only variables are.
func TestUnusedParameterIsNotReported(t *testing.T) {
	src := "package main\n\nfunc f(x int64) {\n}\n\nfunc main() {\n    f(1)\n}\n"
	if _, diags := resolveTree(t, src); len(diags) > 0 {
		t.Errorf("got %q, want no diagnostics", messagesOf(diags))
	}
}
