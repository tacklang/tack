package resolver_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// inMainOf wraps lines, one per statement, in a main function.
func inMainOf(lines ...string) string {
	return "package main\n\nfunc main() {\n    " + strings.Join(lines, "\n    ") + "\n}\n"
}

// switchScopeCases are switches whose names' scopes matter, with the
// diagnostics each must produce: each case's body is a block of its own,
// and the tag and values are resolved where the switch is.
var switchScopeCases = []struct {
	name string
	src  string
	want []string
}{
	{
		"same name in two cases",
		inMainOf(
			"n := 1",
			"switch n {",
			"case 1:",
			"    x := 1",
			"    exit(x)",
			"default:",
			"    x := 2",
			"    exit(x)",
			"}",
		),
		nil,
	},
	{
		"declared in one case, used in the next",
		inMainOf(
			"n := 1",
			"switch n {",
			"case 1:",
			"    x := 1",
			"    exit(x)",
			"case 2:",
			"    exit(x)",
			"}",
		),
		[]string{"undefined: x"},
	},
	{
		"declared in a case, used after the switch",
		inMainOf("switch {", "case true:", "    x := 1", "    exit(x)", "}", "exit(x)"),
		[]string{"undefined: x"},
	},
	{
		"unused in a case",
		inMainOf("switch {", "default:", "    x := 1", "}"),
		[]string{"declared and not used: x"},
	},
	{
		"shadows an outer variable",
		inMainOf("x := 1", "switch x {", "case 1:", "    x := 2", "    exit(x)", "}"),
		[]string{"x is already declared"},
	},
	{
		"tag and value read variables",
		inMainOf("n := 1", "m := 2", "switch n {", "case m:", "}"),
		nil,
	},
	{
		"undefined tag",
		inMainOf("switch n {", "}"),
		[]string{"undefined: n"},
	},
	{
		"undefined value",
		inMainOf("switch {", "case b:", "}"),
		[]string{"undefined: b"},
	},
	{
		"undefined in the default",
		inMainOf("switch {", "default:", "    exit(y)", "}"),
		[]string{"undefined: y"},
	},
	{
		"a fallthrough carries no name into the next case",
		inMainOf(
			"switch {",
			"case true:",
			"    x := 1",
			"    exit(x)",
			"    fallthrough",
			"default:",
			"    exit(x)",
			"}",
		),
		[]string{"undefined: x"},
	},
}

// TestSwitchScope checks each case's body is a scope of its own.
func TestSwitchScope(t *testing.T) {
	for _, tt := range switchScopeCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := resolveTree(t, tt.src)
			if got := messagesOf(diags); strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// The tag and a case value bind to the variables they name.
func TestBindsSwitchTagAndValues(t *testing.T) {
	file, diags := resolveTree(t, inMainOf("n := 1", "m := 2", "switch n {", "case m:", "}"))
	if len(diags) > 0 {
		t.Fatalf("got %q, want no diagnostics", messagesOf(diags))
	}
	body := file.Decls[0].(*ast.Func).Body
	s := body[2].(*ast.Switch)
	if s.Tag.(*ast.Ident).Def != body[0].(*ast.Var) {
		t.Errorf("tag bound to %v, want n", s.Tag.(*ast.Ident).Def)
	}
	if s.Cases[0].Values[0].(*ast.Ident).Def != body[1].(*ast.Var) {
		t.Errorf("value bound to %v, want m", s.Cases[0].Values[0].(*ast.Ident).Def)
	}
}
