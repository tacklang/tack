package typeck_test

import (
	"strings"
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
		{"1 && true", []want{{"operator && is not defined on int64", "1"}}},
		{"a || \"s\"", []want{{"operator || is not defined on string", "\"s\""}}},
		{"1 && 2", []want{{"operator && is not defined on int64", "1"}}},
		{"!5", []want{{"operator ! is not defined on int64", "5"}}},
		{"!\"s\"", []want{{"operator ! is not defined on string", "\"s\""}}},
		{"!(1 + 2)", []want{{"operator ! is not defined on int64", "1 + 2"}}},
		{"(1 + true) && a", []want{{"operator + is not defined on bool", "true"}}},
		{"!(1 + \"s\")", []want{{"operator + is not defined on string", "\"s\""}}},
		{"!5 == true", []want{{"operator ! is not defined on int64", "5"}}},
		{"!5 && a", []want{{"operator ! is not defined on int64", "5"}}},
		{"!!5", []want{{"operator ! is not defined on int64", "5"}}},
		{"a && 1 < true", []want{{"mismatched types int64 and bool in <", "1 < true"}}},
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
	if len(diags) != 1 || diags[0].Message != "cannot use bool as int32 in argument to exit" {
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
			if v.Type != types.Bool || v.Const != tt.want {
				t.Errorf("got %v %#v, want bool %v", v.Type, v.Const, tt.want)
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
		{"var x = true || 18446744073709551615 + 1 > 0", "constant expression is out of range"},
		{"var x = !1", "operator ! is not defined on int64"},
		{"var x = 1 && true", "operator && is not defined on int64"},
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

// chainProgram returns a program whose f tests an if condition of n terms
// joined by op: first d != 0 for &&, or d == 0 for ||, whose fact every
// later n / d needs, then, in turn, a comparison of n, whose fact holds to
// the end, so the facts in force grow with the chain, a comparison of k,
// a call passing k as mut, which ends every fact about k, and a division
// by d.
func chainProgram(op string, n int) string {
	first, kept, test, call, divide := "d != 0", "n >= 0", "k < 100", "bump(mut k)", "n / d >= 0"
	if op == "||" {
		first, kept, test, call, divide = "d == 0", "n < 0", "k > 100", "!bump(mut k)", "n / d < 0"
	}
	terms := make([]string, 0, n)
	terms = append(terms, first)
	for i := 1; i < n; i++ {
		// The comparison of n comes first, so the bound it gives n keeps
		// every n / d from being the smallest int64 divided by -1.
		terms = append(terms, []string{test, kept, call, divide}[i%4])
	}
	return "package main\n\nfunc bump(mut k int64) bool {\n    k +%= 1\n    return true\n}\n\n" +
		"func f(n int64, d int64, mut k int64) int64 {\n    if " + strings.Join(terms, " "+op+" ") +
		" {\n        return 1\n    }\n    return 0\n}\n\nfunc main() {\n}\n"
}

// TestLongLogicalChainsCheckQuickly checks that conditions of thousands
// of terms joined by && or ||, each carrying the facts of the terms before
// it into the next, some of them ended by a mut argument, are checked in
// steps linear in their terms (see checkGrowth): each term's facts are
// found once, not again at every operator above it, and each right side's
// writes are indexed once.
func TestLongLogicalChainsCheckQuickly(t *testing.T) {
	const n = 1000
	for _, op := range []string{"&&", "||"} {
		t.Run(op, func(t *testing.T) {
			diags, small := checkWork(t, chainProgram(op, n))
			if len(diags) > 0 {
				t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
			}
			_, grown := checkWork(t, chainProgram(op, 4*n))
			checkGrowth(t, "conditions worked out", small.Conditions, grown.Conditions)
			checkGrowth(t, "nodes walked for writes", small.Walked, grown.Walked)
		})
	}
}
