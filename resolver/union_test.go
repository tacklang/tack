package resolver_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// inUnionFunc wraps lines, one per statement, in a function f(e E) of a
// program that declares the union E, the struct Point and the function g.
func inUnionFunc(lines ...string) string {
	return "package main\n\ntype E union {\n    int int64\n    name string\n    eof\n}\n\n" +
		"type Point struct {\n    x int64\n}\n\nfunc g(x int64) int64 {\n    return x\n}\n\n" +
		"func f(e E) {\n    " + strings.Join(lines, "\n    ") + "\n}\n\nfunc main() {\n}\n"
}

// unionScopeCases are union declarations, constructions and switches whose
// names matter to the resolver, with the diagnostics each must produce.
// What depends on whether a switch's tag is a union is the type checker's
// to report, so a pattern whose names name nothing reports nothing here.
var unionScopeCases = []struct {
	name string
	src  string
	want []string
}{
	{"construction", inUnionFunc("x := E.int(1)", "y := E.eof", "g(x, y)"), []string{}},
	{
		"construction of a struct type",
		inUnionFunc("x := Point.x", "y := Point.f(1)", "g(x, y)"),
		[]string{
			"cannot use Point as a value; it is a type",
			"cannot use Point as a value; it is a type",
		},
	},
	{"construction of no case is the type checker's", inUnionFunc("x := E.nope", "g(x)"), nil},
	{
		"pattern binds a fresh name",
		inUnionFunc("switch e {", "case int(n):", "    g(n)", "case name, eof:", "}"),
		nil,
	},
	{
		"binding unused is the type checker's",
		inUnionFunc("switch e {", "case int(n):", "default:", "}"),
		nil,
	},
	{
		"binding used outside its clause",
		inUnionFunc("switch e {", "case int(n):", "    g(n)", "default:", "    g(n)", "}"),
		[]string{"undefined: n"},
	},
	{
		"binding used after the switch",
		inUnionFunc("switch e {", "case int(n):", "    g(n)", "default:", "}", "g(n)"),
		[]string{"undefined: n"},
	},
	{
		"binding in a clause of several values is not declared",
		inUnionFunc("switch e {", "case int(n), eof:", "    g(n)", "default:", "}"),
		nil,
	},
	{
		"binding visible as a variable is the type checker's",
		inUnionFunc("n := 1", "switch e {", "case int(n):", "default:", "}"),
		nil,
	},
	{
		"binding redeclared in its body",
		inUnionFunc("switch e {", "case int(n):", "    n := 2", "    g(n)", "default:", "}"),
		[]string{"n is already declared"},
	},
	{
		"pattern names nothing visible",
		inUnionFunc("switch e {", "case nope(x), Point(y), g(z):", "default:", "}"),
		nil,
	},
	{
		"case named like a variable",
		inUnionFunc(
			"eof := 1",
			"switch e {",
			"case int(n):",
			"    g(n + eof)",
			"case name, eof:",
			"}",
		),
		nil,
	},
	{
		"value of another shape is resolved as a value",
		inUnionFunc("switch e {", "case int(1), h(2):", "case name(m + 1):", "default:", "}"),
		[]string{"undefined: m"},
	},
	{
		"qualified callee is resolved as a value",
		inUnionFunc("switch e {", "case p.int(n):", "default:", "}"),
		[]string{"undefined: p", "undefined: n"},
	},
	{
		"tagless switch",
		inUnionFunc("switch {", "case eof:", "}"),
		[]string{"undefined: eof"},
	},
}

// TestUnionScope checks union constructions bind their type, and switch
// values bind as values or as patterns by whether their last name is a
// variable or a parameter visible at the switch.
func TestUnionScope(t *testing.T) {
	for _, tt := range unionScopeCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := resolveTree(t, tt.src)
			got := messagesOf(diags)
			if len(got) != len(tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			for i := range got {
				if !strings.Contains(got[i], tt.want[i]) {
					t.Errorf("got %q, want %q", got, tt.want)
				}
			}
		})
	}
}

// unionDeclCases are union declarations with the diagnostics each must
// produce: a case's name follows a field's rule, and a method is never a
// union's.
var unionDeclCases = []struct {
	name, decl string
	want       []string
}{
	{"cases", "type U union {\n    a int64\n    b\n}\n", nil},
	{"case named like a type", "type U union {\n    Point Point\n}\ntype Point struct {}\n", nil},
	{
		"case named after a basic type", "type U union {\n    int64 int64\n}\n",
		[]string{"cannot use int64 as a name; it is a type"},
	},
	{"case named _", "type U union {\n    _\n}\n", []string{"cannot use _ as a name"}},
	{"duplicate case", "type U union {\n    a int64\n    a\n}\n", []string{"duplicate case name"}},
	{"payload not a type", "var n = 1\ntype U union {\n    a n\n}\n", []string{"n is not a type"}},
	{
		"method on a union",
		"type U union {\n    a\n}\nfunc (u U) m() {\n}\n",
		[]string{
			"cannot declare a method on U: only a struct type declared in this package can have methods",
		},
	},
	{
		"union shares a name",
		"type U union {\n    a\n}\nvar U = 1\n",
		[]string{"U is already declared"},
	},
}

// TestUnionDecls checks a union's cases are named by a field's rule, once
// each, and its payloads' types resolved.
func TestUnionDecls(t *testing.T) {
	for _, tt := range unionDeclCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := resolveTree(t, "package main\n\n"+tt.decl+"\nfunc main() {\n}\n")
			got := messagesOf(diags)
			if len(got) != len(tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			for i := range got {
				if !strings.Contains(got[i], tt.want[i]) {
					t.Errorf("got %q, want %q", got, tt.want)
				}
			}
		})
	}
}

// TestBindsUnionPatterns checks a construction's type is bound to the
// union, a clause's binding is its own variable, which its body's uses
// bind to, and a bare case name visible as nothing is left unbound.
func TestBindsUnionPatterns(t *testing.T) {
	file, diags := resolveTree(t, inUnionFunc(
		"switch e {",
		"case int(n):",
		"    g(n)",
		"case name(s):",
		"    print(s)",
		"case eof:",
		"}",
		"x := E.int(1)",
		"g(x)",
	))
	if len(diags) > 0 {
		t.Fatalf("resolve reported %q", messagesOf(diags))
	}
	fn := funcNamed(file, "f")
	checkPatternBindings(t, fn.Body[0].(*ast.Switch))
	union := file.Decls[0].(*ast.TypeDecl)
	built := fn.Body[1].(*ast.Var).Value.(*ast.Call)
	td, sel, payload, ok := ast.Construction(built)
	if !ok || td != union || sel.Name != "int" || payload == nil || built.Receiver() != nil {
		t.Errorf("construction read as %v %v %v %v", td, sel, payload, ok)
	}
}

// funcNamed returns the function of file named name, or nil.
func funcNamed(file *ast.File, name string) *ast.Func {
	for _, decl := range file.Decls {
		if f, ok := decl.(*ast.Func); ok && f.Name == name {
			return f
		}
	}
	return nil
}

// checkPatternBindings fails the test unless the first two clauses of s
// declare n and s, which their bodies' uses bind to, and the third, eof,
// declares nothing and leaves its case's name unbound.
func checkPatternBindings(t *testing.T, s *ast.Switch) {
	t.Helper()
	for i, name := range []string{"n", "s"} {
		cl := s.Cases[i]
		if cl.Var == nil || cl.Var.Name != name {
			t.Fatalf("case %d declares %v, want %s", i, cl.Var, name)
		}
		if use := useOf(cl.Body, name); use == nil || use.Def != cl.Var {
			t.Errorf("case %d: %s bound to %v, want the clause's own variable", i, name, use)
		}
	}
	if s.Cases[2].Var != nil {
		t.Errorf("eof's clause declares %v", s.Cases[2].Var)
	}
	if eof := s.Cases[2].Values[0].(*ast.Ident); eof.Def != nil {
		t.Errorf("eof bound to %v", eof.Def)
	}
}
