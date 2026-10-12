package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/types"
)

// TestAcceptsBoolLiterals lists programs that use true and false where a
// bool is wanted, and must check cleanly.
func TestAcceptsBoolLiterals(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"if condition", inMain("if true {\n    }")},
		{"declared with an inferred type", mainLines("x := false", "if x {", "}")},
		{"declared with a type", mainLines("var x bool = true", "if x {", "}")},
		{"assigned", mainLines("mut x = true", "x = false", "if x {", "}")},
		{
			"returned",
			"package main\n\nfunc f() bool {\n    return true\n}\n\nfunc main() {\n    if f() {\n    }\n}\n",
		},
		{
			"passed",
			"package main\n\nfunc f(b bool) {\n    if b {\n    }\n}\n\nfunc main() {\n    f(false)\n}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// TestRejectsBoolLiteralsWhereOtherTypesAreWanted checks a bool literal is
// held to the same rules as any other bool.
func TestRejectsBoolLiteralsWhereOtherTypesAreWanted(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"exit status", inMain("exit(true)"), []string{"cannot use bool as int"}},
		{"printed", inMain("print(false)"), []string{"cannot use bool as string"}},
		{
			// An arithmetic expression takes its left operand's type, so the
			// argument is reported too, as for "a" + "b".
			"arithmetic",
			inMain("exit(true + 1)"),
			[]string{"cannot use bool as int in argument to exit", "cannot use bool as int in +"},
		},
		{
			// Comparing bools is not defined yet; == takes ints only.
			"compared",
			inMain("if true == false {\n    }"),
			[]string{"cannot use bool as int in ==", "cannot use bool as int in =="},
		},
		{
			"int variable",
			mainLines("var x int = true", "exit(x)"),
			[]string{"cannot use bool as int in declaration of x"},
		},
		{
			"assigned to an int",
			mainLines("mut x = 1", "x = false", "exit(x)"),
			[]string{"cannot use bool as int in assignment to x"},
		},
		{
			"returned as an int",
			"package main\n\nfunc f() int {\n    return true\n}\n\nfunc main() {\n    exit(f())\n}\n",
			[]string{"cannot use bool as int"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != len(tt.want) {
				t.Fatalf("got %v, want %q", diags, tt.want)
			}
			for i, d := range diags {
				if !strings.Contains(d.Message, tt.want[i]) {
					t.Errorf("message %d = %q, want it to contain %q", i, d.Message, tt.want[i])
				}
			}
		})
	}
}

// A bool literal is a constant expression, so it can initialize a
// package-level variable, and its value is computed like any other.
func TestFoldsBoolLiteralGlobals(t *testing.T) {
	tests := []struct {
		decl string
		want bool
	}{
		{"var x = true", true},
		{"var x bool = false", false},
		{"mut x = true", true},
	}

	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			src := "package main\n\n" + tt.decl + "\n\nfunc main() {\n    if x {\n    }\n}\n"
			file, diags := checkTree(t, src)
			if len(diags) > 0 {
				t.Fatalf("got %v, want no diagnostics", diags)
			}
			v := globalX(t, file)
			if v.Kind != types.Bool {
				t.Errorf("kind = %s, want bool", v.Kind)
			}
			if v.Const != tt.want {
				t.Errorf("value = %#v, want %v", v.Const, tt.want)
			}
		})
	}
}

// Folding a global whose initializer applies an operator to a bool literal
// must report the operand's type, not crash on a value that is not an int.
func TestRejectsOperatorsOnBoolLiteralGlobals(t *testing.T) {
	tests := []struct {
		decl string
		want string
	}{
		{"var x = true + 1", "cannot use bool as int in +"},
		{"var x = 1 * false", "cannot use bool as int in *"},
		{"var x = true < false", "cannot use bool as int in <"},
	}

	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			_, diags := checkTree(t, withGlobal(tt.decl))
			if len(diags) == 0 {
				t.Fatalf("%q was accepted", tt.decl)
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
		})
	}
}
