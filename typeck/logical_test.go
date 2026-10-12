package typeck_test

import (
	"testing"

	"github.com/tacklang/tack/types"
)

// logicalProgram returns a program with bools a and b, a function f
// returning a bool, and cond as the condition of an if in main.
func logicalProgram(cond string) string {
	return "package main\n\nfunc f() bool {\n    return true\n}\n\nfunc main() {\n" +
		"    a := 1 < 2\n    b := f()\n    if " + cond + " {\n        exit(1)\n    }\n    if a == b {\n    }\n}\n"
}

// TestAcceptsLogicalOperators lists conditions built with && || and ! that
// must check cleanly.
func TestAcceptsLogicalOperators(t *testing.T) {
	tests := []string{
		"true && false",
		"a || b",
		"!a",
		"!!a",
		"!(1 < 2) && a",
		"f() || !f()",
		"(a && b) == (a || b)",
		"a != !b",
		"1 < 2 && 3 != 4 || !a",
	}

	for _, cond := range tests {
		t.Run(cond, func(t *testing.T) {
			if _, diags := checkTree(t, logicalProgram(cond)); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// A logical expression is a bool wherever one is wanted.
func TestLogicalExpressionsAreBools(t *testing.T) {
	src := "package main\n\nfunc both(a bool, b bool) bool {\n    return a && b\n}\n\n" +
		"func main() {\n    mut x = !both(true, false)\n    x = x || false\n    if x {\n    }\n}\n"
	if _, diags := checkTree(t, src); len(diags) > 0 {
		t.Errorf("got %v, want no diagnostics", diags)
	}
}

// TestRejectsNonBoolLogicalOperands checks each operand that is not a bool
// is reported on its own, naming the operator and the operand's type, and
// that an operand already reported is not reported again.
func TestRejectsNonBoolLogicalOperands(t *testing.T) {
	type want struct{ msg, at string }
	tests := []struct {
		cond string
		want []want
	}{
		{"1 && true", []want{{"operator && is not defined on int", "1"}}},
		{"a || \"s\"", []want{{"operator || is not defined on string", "\"s\""}}},
		{"1 && 2", []want{
			{"operator && is not defined on int", "1"},
			{"operator && is not defined on int", "2"},
		}},
		{"!5", []want{{"operator ! is not defined on int", "5"}}},
		{"!\"s\"", []want{{"operator ! is not defined on string", "\"s\""}}},
		{"!(1 + 2)", []want{{"operator ! is not defined on int", "1 + 2"}}},
		{"(1 + true) && a", []want{{"cannot use bool as int in +", "true"}}},
		{"!(1 + \"s\")", []want{{"cannot use string as int in +", "\"s\""}}},
		{"!5 == true", []want{{"operator ! is not defined on int", "5"}}},
		{"!5 && a", []want{{"operator ! is not defined on int", "5"}}},
		{"!!5", []want{{"operator ! is not defined on int", "5"}}},
		{"a && 1 < true", []want{{"mismatched types int and bool in <", "1 < true"}}},
	}

	for _, tt := range tests {
		t.Run(tt.cond, func(t *testing.T) {
			src := logicalProgram(tt.cond)
			_, diags := checkTree(t, src)
			if len(diags) != len(tt.want) {
				t.Fatalf("got %v, want %v", diags, tt.want)
			}
			for i, d := range diags {
				if d.Message != tt.want[i].msg {
					t.Errorf("message %d = %q, want %q", i, d.Message, tt.want[i].msg)
				}
				if got := src[d.Span.Start:d.Span.End]; got != tt.want[i].at {
					t.Errorf("diagnostic %d covers %q, want %q", i, got, tt.want[i].at)
				}
			}
		})
	}
}

// A logical expression is a bool, so it is reported where an int is wanted.
func TestLogicalExpressionWhereAnIntIsWanted(t *testing.T) {
	_, diags := checkTree(t, inMain("exit(true && false)"))
	if len(diags) != 1 || diags[0].Message != "cannot use bool as int in argument to exit" {
		t.Errorf("got %v, want one argument error", diags)
	}
}

// TestFoldsLogicalGlobals checks the value computed for a package-level bool
// initialized with logical operators.
func TestFoldsLogicalGlobals(t *testing.T) {
	tests := []struct {
		decl string
		want bool
	}{
		{"var x = true && false", false},
		{"var x = true && true", true},
		{"var x = false || false", false},
		{"var x = false || true", true},
		{"var x = !true", false},
		{"var x = !!true", true},
		{"var x = !(1 < 2) || 3 == 3", true},
		{"var x = 1 < 2 && 2 < 1", false},
		{"var x = !false && !(true == false)", true},
	}

	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			src := "package main\n\n" + tt.decl + "\n\nfunc main() {\n    if x {\n    }\n}\n"
			file, diags := checkTree(t, src)
			if len(diags) > 0 {
				t.Fatalf("got %v, want no diagnostics", diags)
			}
			v := globalX(t, file)
			if v.Kind != types.Bool || v.Const != tt.want {
				t.Errorf("got %s %#v, want bool %v", v.Kind, v.Const, tt.want)
			}
		})
	}
}

// TestRejectsInvalidLogicalGlobals checks a package-level initializer is
// computed in full, the right side of && and || included, and an operand of
// the wrong type is reported rather than folded.
func TestRejectsInvalidLogicalGlobals(t *testing.T) {
	tests := []struct {
		decl string
		want string
	}{
		{"var x = false && 1 / 0 == 1", "division by zero in constant expression"},
		{"var x = true || 9223372036854775807 + 1 > 0", "constant expression overflows int"},
		{"var x = !1", "operator ! is not defined on int"},
		{"var x = 1 && true", "operator && is not defined on int"},
	}

	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			src := "package main\n\n" + tt.decl + "\n\nfunc main() {\n    if x {\n    }\n}\n"
			_, diags := checkTree(t, src)
			if len(diags) != 1 || diags[0].Message != tt.want {
				t.Errorf("got %v, want %q", diags, tt.want)
			}
		})
	}
}
