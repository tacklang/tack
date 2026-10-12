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
		{"var x = true == false", false},
		{"var x = false == false", true},
		{"var x = true != (1 < 2)", false},
		{"var x = (2 > 1) == true", true},
		{"var x = (1 == 1) != (2 == 3)", true},
	}

	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			src := "package main\n\n" + tt.decl + "\n\nfunc main() {\n    if x {\n    }\n}\n"
			file, diags := checkTree(t, src)
			if len(diags) > 0 {
				t.Fatalf("got %v, want no diagnostics", diags)
			}
			v := globalX(t, file)
			if v.Type != types.Bool {
				t.Errorf("type = %v, want bool", v.Type)
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
		{"var x = true < false", "operator < is not defined on bool"},
		{"var x = true == 1", "mismatched types bool and int in =="},
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

// TestAcceptsBoolEquality lists comparisons of two bools with == and !=,
// which must check cleanly.
func TestAcceptsBoolEquality(t *testing.T) {
	tests := []string{
		"true == false",
		"true != false",
		"(1 < 2) == (3 < 4)",
		"(1 < 2) != true",
		"x == true",
		"x != x",
		"f() == x",
		"(x == true) == (x != false)",
	}

	for _, cond := range tests {
		t.Run(cond, func(t *testing.T) {
			src := "package main\n\nfunc f() bool {\n    return true\n}\n\n" +
				"func main() {\n    x := false\n    if " + cond + " {\n        exit(1)\n    }\n    if x {\n    }\n}\n"
			if _, diags := checkTree(t, src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// TestRejectsInvalidComparisons checks a comparison of two different types
// is one mismatch error, and an operator its operands' type does not support
// is one error naming the operator, each covering the whole comparison.
func TestRejectsInvalidComparisons(t *testing.T) {
	tests := []struct {
		cond string
		want string
	}{
		{"true == 1", "mismatched types bool and int in =="},
		{"1 != false", "mismatched types int and bool in !="},
		{"1 < true", "mismatched types int and bool in <"},
		{"\"a\" == true", "mismatched types string and bool in =="},
		{"\"a\" >= 1", "mismatched types string and int in >="},
		{"true < false", "operator < is not defined on bool"},
		{"true <= false", "operator <= is not defined on bool"},
		{"true > false", "operator > is not defined on bool"},
		{"1 < 2 > false", "operator > is not defined on bool"},
		{"true >= true", "operator >= is not defined on bool"},
		{"\"a\" == \"b\"", "operator == is not defined on string"},
		{"\"a\" < \"b\"", "operator < is not defined on string"},
	}

	for _, tt := range tests {
		t.Run(tt.cond, func(t *testing.T) {
			src := inMain("if " + tt.cond + " {\n    }")
			_, diags := checkTree(t, src)
			if len(diags) != 1 {
				t.Fatalf("got %v, want one diagnostic %q", diags, tt.want)
			}
			if diags[0].Message != tt.want {
				t.Errorf("message = %q, want %q", diags[0].Message, tt.want)
			}
			if got := src[diags[0].Span.Start:diags[0].Span.End]; got != tt.cond {
				t.Errorf("diagnostic covers %q, want the whole comparison %q", got, tt.cond)
			}
		})
	}
}

// An operand already reported is not reported again by the comparison it
// sits in.
func TestComparisonDoesNotRepeatOperandErrors(t *testing.T) {
	_, diags := checkTree(t, inMain("if (\"a\" + 1) == true {\n    }"))
	if len(diags) != 1 || diags[0].Message != "cannot use string as int in +" {
		t.Errorf("got %v, want only the error inside the operand", diags)
	}
}
