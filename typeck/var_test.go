package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/typeck"
	"github.com/tacklang/tack/types"
)

// checkTree runs the front end through type checking and returns the checked
// tree with the checker's diagnostics. Earlier stages failing aborts the test.
func checkTree(t *testing.T, src string) (*ast.File, []diag.Diagnostic) {
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
	if diags := resolver.New(file).Resolve(); len(diags) > 0 {
		t.Fatalf("resolve: %s", diag.RenderAllPretty(f, diags))
	}
	return file, typeck.New(file).Check()
}

// mainLines wraps statements, one per line, in a main function.
func mainLines(lines ...string) string {
	return "package main\n\nfunc main() {\n    " + strings.Join(lines, "\n    ") + "\n}\n"
}

// TestRecordsEachVariablesType checks the type recorded for the first
// variable main declares, whether written or inferred.
func TestRecordsEachVariablesType(t *testing.T) {
	tests := []struct {
		decl string
		want types.Type
	}{
		{"x := 1", types.Int},
		{"x := \"s\"", types.String},
		{"x := 1 < 2", types.Bool},
		{"x := 1 + 2 * 3", types.Int},
		{"var x int = 1", types.Int},
		{"var x = \"s\"", types.String},
		{"var x bool = 1 < 2", types.Bool},
		{"mut x int", types.Int},
		{"mut x string", types.String},
		{"mut x bool", types.Bool},
		{"mut x = 1", types.Int},
	}

	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			file, diags := checkTree(t, mainLines(tt.decl, "if x == x {", "}"))
			// A bool or string compared with == is a checker error of its
			// own; only the declaration's type is under test here.
			_ = diags
			v := file.Decls[0].(*ast.Func).Body[0].(*ast.Var)
			if v.Type != tt.want {
				t.Errorf("type = %v, want %v", v.Type, tt.want)
			}
		})
	}
}

// A variable's type flows into everything that reads it.
func TestVariableTypeFlowsIntoUses(t *testing.T) {
	src := "package main\n\nfunc show(s string) {\n    print(s)\n}\n\nfunc main() {\n" +
		"    s := \"hi\"\n    show(s)\n    n := 2\n    m := n * 3\n    exit(m)\n}\n"
	if _, diags := checkTree(t, src); len(diags) > 0 {
		t.Errorf("got %v, want no diagnostics", diags)
	}
}

// TestAcceptsValidVariablePrograms lists programs the checker must accept.
func TestAcceptsValidVariablePrograms(t *testing.T) {
	tests := []string{
		mainLines("mut x := 1", "x = 2", "exit(x)"),
		mainLines("mut x int", "x = x + 1", "exit(x)"),
		mainLines("mut s string", "s = \"x\"", "print(s)"),
		mainLines("mut b bool", "b = 1 < 2", "if b {", "}"),
		mainLines("x := 1", "mut y = x", "y = x * 2", "exit(y)"),
	}
	for _, src := range tests {
		if _, diags := checkTree(t, src); len(diags) > 0 {
			t.Errorf("%q: got %v, want no diagnostics", src, diags)
		}
	}
}

// invalidVariablePrograms are programs that misuse a variable's type or mutability, with the diagnostics each must produce.
var invalidVariablePrograms = []struct {
	name string
	src  string
	want []string
}{
	{
		"immutable without a value", mainLines("var x int", "exit(x)"),
		[]string{"immutable variable x needs a value"},
	},
	{
		"declared type mismatch", mainLines("var x int = \"s\"", "exit(x)"),
		[]string{"cannot use string as int in declaration of x"},
	},
	{
		"declared bool from int", mainLines("var b bool = 1", "if b {", "}"),
		[]string{"cannot use int as bool in declaration of b"},
	},
	{
		"unknown declared type", mainLines("var x float = 1", "exit(x)"),
		[]string{"unknown type: float"},
	},
	{
		"value from a function returning nothing",
		"package main\n\nfunc f() {\n}\n\nfunc main() {\n    x := f()\n    exit(x)\n}\n",
		[]string{"f returns no value"},
	},
	{
		"value from a builtin", mainLines("x := print(\"s\")", "exit(x)"),
		[]string{"print returns no value"},
	},
	{
		"assign to an immutable variable", mainLines("x := 1", "x = 2", "exit(x)"),
		[]string{"cannot assign to immutable variable x"},
	},
	{
		"assign to an immutable var declaration", mainLines("var x = 1", "x = 2", "exit(x)"),
		[]string{"cannot assign to immutable variable x"},
	},
	{
		"assign a different type", mainLines("mut x := 1", "x = \"s\"", "exit(x)"),
		[]string{"cannot use string as int in assignment to x"},
	},
	{
		"assign nothing",
		"package main\n\nfunc f() {\n}\n\nfunc main() {\n    mut x := 1\n    x = f()\n    exit(x)\n}\n",
		[]string{"f returns no value"},
	},
	{
		"assign to a parameter",
		"package main\n\nfunc f(n int) {\n    n = 2\n}\n\nfunc main() {\n    f(1)\n}\n",
		[]string{"cannot assign to parameter n"},
	},
	{
		"string arithmetic through a variable",
		mainLines("s := \"a\"", "t := s + s", "print(t)"),
		[]string{"cannot use string as int in +", "cannot use string as int in +"},
	},
}

// TestRejectsInvalidVariablePrograms pins the checker's diagnostics for each
// way to misuse a variable's type or mutability.
func TestRejectsInvalidVariablePrograms(t *testing.T) {
	tests := invalidVariablePrograms

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			got := make([]string, len(diags))
			for i, d := range diags {
				got[i] = d.Message
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
