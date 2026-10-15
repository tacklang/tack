package resolver_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// inAnyFunc wraps lines, one per statement, in a function f(a any) of a
// program that also declares the struct type Point.
func inAnyFunc(lines ...string) string {
	return "package main\n\ntype Point struct {\n    x int64\n}\n\nfunc f(a any) {\n    " +
		strings.Join(lines, "\n    ") + "\n}\n\nfunc main() {\n}\n"
}

// typeSwitchScopeCases are type switches and assertions whose names
// matter, with the diagnostics each must produce.
var typeSwitchScopeCases = []struct {
	name string
	src  string
	want []string
}{
	{
		"binding read in one case only",
		inAnyFunc(
			"switch v := a.(type) {",
			"case int64:",
			"    g(v)",
			"case string:",
			"default:",
			"}",
		),
		[]string{"undefined: g"},
	},
	{
		"binding read in no case",
		inAnyFunc("switch v := a.(type) {", "case int64:", "default:", "}"),
		[]string{"declared and not used: v"},
	},
	{
		"binding with no cases",
		inAnyFunc("switch v := a.(type) {", "}"),
		[]string{"declared and not used: v"},
	},
	{"no binding", inAnyFunc("switch a.(type) {", "case int64:", "}"), nil},
	{
		"binding shadows the operand",
		inAnyFunc("switch a := a.(type) {", "case int64:", "    exit(a)", "}"),
		[]string{"a is already declared"},
	},
	{
		"binding shadows an outer variable",
		inAnyFunc("v := 1", "switch v := a.(type) {", "case int64:", "    exit(v)", "}"),
		[]string{"v is already declared"},
	},
	{
		"binding named _",
		inAnyFunc("switch _ := a.(type) {", "case int64:", "}"),
		[]string{"cannot use _ as a name"},
	},
	{
		"binding named after a type",
		inAnyFunc("switch int64 := a.(type) {", "case int64:", "    exit(int64)", "}"),
		[]string{"cannot use int64 as a name; it is a type"},
	},
	{
		"case variable redeclared in its body",
		inAnyFunc("switch v := a.(type) {", "case int64:", "    v := 2", "    exit(v)", "}"),
		[]string{"v is already declared"},
	},
	{
		"binding used after the switch",
		inAnyFunc("switch v := a.(type) {", "case int64:", "    exit(v)", "}", "exit(v)"),
		[]string{"undefined: v"},
	},
	{
		"binding used in the tag",
		inAnyFunc("switch v := v.(type) {", "case int64:", "    exit(v)", "}"),
		[]string{"undefined: v"},
	},
	{
		"same name declared after the switch",
		inAnyFunc("switch v := a.(type) {", "case int64:", "    exit(v)", "}", "v := 1", "exit(v)"),
		nil,
	},
	{
		"case type not a type",
		inAnyFunc("n := 1", "switch a.(type) {", "case n:", "}", "exit(n)"),
		[]string{"n is not a type"},
	},
	{
		"case type undefined is left to the type checker",
		inAnyFunc("switch a.(type) {", "case Nope, Point, [2]int64, []bool, list[int8]:", "}"),
		nil,
	},
	{
		"asserted type not a type",
		inAnyFunc("n := 1", "v, ok := a.(n)", "exit(n)", "g(v, ok)"),
		[]string{"n is not a type", "undefined: g"},
	},
	{
		"assertion operand undefined",
		inAnyFunc("v, ok := b.(int64)", "g(v, ok)"),
		[]string{"undefined: b", "undefined: g"},
	},
	{
		"assigning to an assertion",
		inAnyFunc("a.(int64) = 1"),
		[]string{"cannot assign to this expression"},
	},
}

// TestTypeSwitchScope checks a type switch's binding is checked where the
// switch is, declared in each case, and reported unused only when no case
// reads it, and that an assertion resolves its operand and its type.
func TestTypeSwitchScope(t *testing.T) {
	for _, tt := range typeSwitchScopeCases {
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

// TestBindsTypeSwitchVariables checks each case's uses bind to that case's
// own variable, and the case types to the declarations they name.
func TestBindsTypeSwitchVariables(t *testing.T) {
	file, diags := resolveTree(t, inAnyFunc(
		"switch v := a.(type) {",
		"case Point:",
		"    exit(v.x)",
		"default:",
		"    h(v)",
		"}",
		"w, ok := a.(Point)",
		"h(w, ok)",
	))
	if got := messagesOf(diags); strings.Join(got, "\n") != "undefined: h\nundefined: h" {
		t.Fatalf("got %q, want only h reported", got)
	}
	s, assert := typeSwitchAndAssert(file)
	point := file.Decls[0].(*ast.TypeDecl)
	if s.Cases[0].Values[0].(*ast.Ident).Def != point {
		t.Errorf("case type bound to %v, want Point", s.Cases[0].Values[0].(*ast.Ident).Def)
	}
	if assert.TypeExpr.(*ast.Ident).Def != point {
		t.Errorf("asserted type bound to %v, want Point", assert.TypeExpr.(*ast.Ident).Def)
	}
	if assert.X.(*ast.Ident).Def == nil {
		t.Errorf("assertion operand not bound")
	}
	for i, cl := range s.Cases {
		if use := useOf(cl.Body, "v"); use == nil || use.Def != cl.Var {
			t.Errorf("case %d: v bound to %v, want the case's own variable", i, use)
		}
	}
}

// typeSwitchAndAssert returns the switch and the assertion that start the
// body of the function f in file.
func typeSwitchAndAssert(file *ast.File) (*ast.Switch, *ast.TypeAssert) {
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.Func); ok && fn.Name == "f" {
			return fn.Body[0].(*ast.Switch), fn.Body[1].(*ast.MultiVar).Value.(*ast.TypeAssert)
		}
	}
	return nil, nil
}

// useOf returns the last use of the name in body, or nil when there is
// none.
func useOf(body []ast.Stmt, name string) *ast.Ident {
	var use *ast.Ident
	for _, stmt := range body {
		ast.Walk(stmt, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == name {
				use = id
			}
			return true
		})
	}
	return use
}
