package typeck_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// multiFuncs declares functions with several results, and some without, for
// the programs below to call.
const multiFuncs = "func divmod(a int64, b int64) (int64, int64) {\n    return a / b, a % b\n}\n\n" +
	"func three() (string, bool, int64) {\n    return \"s\", true, 3\n}\n\n" +
	"func one() int64 {\n    return 1\n}\n\n" +
	"func none() {\n}\n\n"

// withMulti is a program holding multiFuncs and a main of the given lines.
func withMulti(lines ...string) string {
	return "package main\n\n" + multiFuncs + mainLines(lines...)[len("package main\n\n"):]
}

// TestRecordsMultiVarTypes checks each variable takes the type of the result
// in its position.
func TestRecordsMultiVarTypes(t *testing.T) {
	file, diags := checkTree(
		t,
		withMulti("s, b, mut n := three()", "n = 4", "if b {", "    print(s)", "}", "exit(n)"),
	)
	if len(diags) > 0 {
		t.Fatalf("check reported %v", diags)
	}

	main := file.Decls[len(file.Decls)-1].(*ast.Func)
	m := main.Body[0].(*ast.MultiVar)
	want := []types.Type{types.String, types.Bool, types.Int64}
	for i, v := range m.Vars {
		if v.Type != want[i] {
			t.Errorf("variable %s has type %v, want %v", v.Name, v.Type, want[i])
		}
	}
}

// TestAcceptsMultipleResults lists programs using several results that must
// check cleanly.
func TestAcceptsMultipleResults(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"bind both", withMulti("q, r := divmod(7, 2)", "exit(q + r)")},
		{"discard all as a statement", withMulti("divmod(7, 2)")},
		{"mixed types", withMulti("s, b, n := three()", "if b {", "    print(s)", "}", "exit(n)")},
		{"mutable one", withMulti("q, mut r := divmod(7, 2)", "r += q", "exit(r)")},
		{
			"in a loop init",
			withMulti("for q, mut r := divmod(9, 4); r > 0; r-- {", "    exit(q)", "}"),
		},
		{
			"results as arguments",
			withMulti("q, r := divmod(7, 2)", "a, b := divmod(q, r)", "exit(a + b)"),
		},
		{
			"recursive",
			"package main\n\nfunc fib(n int64) (int64, int64) {\n    if n == 0 {\n        return 0, 1\n    }\n" +
				"    a, b := fib(n - 1)\n    return b, a + b\n}\n\nfunc main() {\n    x, y := fib(10)\n    exit(x + y)\n}\n",
		},
		{
			"declared after its caller",
			"package main\n\nfunc main() {\n    a, b := later()\n    exit(a + b)\n}\n\n" +
				"func later() (int64, int64) {\n    return 1, 2\n}\n",
		},
		{
			"ends in a loop",
			"package main\n\nfunc f() (int64, int64) {\n    for {\n    }\n}\n\nfunc main() {\n    f()\n}\n",
		},
		{
			"ends in an if with an else",
			"package main\n\nfunc f(n int64) (int64, int64) {\n    if n > 0 {\n        return 1, 2\n    } else {\n        return 3, 4\n    }\n}\n\n" +
				"func main() {\n    f(1)\n}\n",
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

// multipleResultMisuseCases are wrong uses of several results, with the
// diagnostics each must produce.
var multipleResultMisuseCases = []struct {
	name string
	src  string
	want []spanned
}{
	{
		"too few variables",
		withMulti("a, b := three()", "exit(a + b)"),
		[]spanned{
			{"assignment mismatch: 2 variables but three returns 3 values", "a, b := three()"},
		},
	},
	{
		"too many variables",
		withMulti("a, b, c := divmod(1, 2)", "exit(a + b + c)"),
		[]spanned{
			{
				"assignment mismatch: 3 variables but divmod returns 2 values",
				"a, b, c := divmod(1, 2)",
			},
		},
	},
	{
		"one result",
		withMulti("a, b := one()", "exit(a + b)"),
		[]spanned{
			{"assignment mismatch: 2 variables but one returns 1 value", "a, b := one()"},
		},
	},
	{
		"no result",
		withMulti("a, b := none()", "exit(a + b)"),
		[]spanned{
			{"assignment mismatch: 2 variables but none returns no value", "a, b := none()"},
		},
	},
	{
		"builtin",
		withMulti("a, b := exit(1)", "exit(a + b)"),
		[]spanned{
			{"assignment mismatch: 2 variables but exit returns no value", "a, b := exit(1)"},
		},
	},
	{
		"not a call",
		withMulti("a, b := 1", "exit(a + b)"),
		[]spanned{{"assignment mismatch: 2 variables but 1 value", "a, b := 1"}},
	},
	{
		"not a call, an expression",
		withMulti("a, b := 1 + 2", "exit(a + b)"),
		[]spanned{{"assignment mismatch: 2 variables but 1 value", "a, b := 1 + 2"}},
	},
	{
		"several results as one value",
		withMulti("x := divmod(1, 2)", "exit(x)"),
		[]spanned{{"divmod returns 2 values", "divmod(1, 2)"}},
	},
	{
		"several results as an argument",
		withMulti("exit(divmod(1, 2))"),
		[]spanned{{"divmod returns 2 values", "divmod(1, 2)"}},
	},
	{
		"several results as a nested argument",
		withMulti("q, r := divmod(divmod(1, 2), 3)", "exit(q + r)"),
		[]spanned{{"divmod returns 2 values", "divmod(1, 2)"}},
	},
	{
		"bad argument to the call",
		withMulti("q, r := divmod(\"x\", 3)", "exit(q + r)"),
		[]spanned{{"cannot use string as int64 in argument to divmod", "\"x\""}},
	},
	{
		"result type flows into uses",
		withMulti("s, b, n := three()", "exit(s)", "if b {", "    exit(n)", "}"),
		[]spanned{{"cannot use string as int64 in argument to exit", "s"}},
	},
	{
		"immutable result variable",
		withMulti("q, mut r := divmod(1, 2)", "q = r", "exit(q)"),
		[]spanned{{"cannot assign to immutable variable q", "q"}},
	},
	{
		"mutable result variable gets the wrong type",
		withMulti("s, b, mut n := three()", "n = s", "if b {", "    exit(n)", "}"),
		[]spanned{{"cannot use string as int64 in assignment to n", "s"}},
	},
	{
		"mismatched variables are not reported again",
		withMulti("a, b := one()", "exit(a + b)", "print(a)"),
		[]spanned{
			{"assignment mismatch: 2 variables but one returns 1 value", "a, b := one()"},
		},
	},
}

// TestRejectsMultipleResultMisuse checks each wrong use of several results
// is one error, covering the right source.
func TestRejectsMultipleResultMisuse(t *testing.T) {
	for _, tt := range multipleResultMisuseCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != len(tt.want) {
				t.Fatalf("got %v, want %v", diags, tt.want)
			}
			for i, d := range diags {
				if d.Message != tt.want[i].msg {
					t.Errorf("message %d = %q, want %q", i, d.Message, tt.want[i].msg)
				}
				if got := tt.src[d.Span.Start:d.Span.End]; got != tt.want[i].at {
					t.Errorf("diagnostic %d covers %q, want %q", i, got, tt.want[i].at)
				}
			}
		})
	}
}

// withResults is a program whose function f declares the results sig and has
// the body body, and whose main calls it.
func withResults(sig, body string) string {
	return "package main\n\nfunc f() " + sig + " {\n    " + body + "\n}\n\nfunc main() {\n    f()\n}\n"
}

// wrongReturnCases are returns that do not match their function's results,
// with the diagnostics each must produce.
var wrongReturnCases = []struct {
	name string
	src  string
	want []spanned
}{
	{
		"bare return, two results",
		withResults("(int64, int64)", "return"),
		[]spanned{{"not enough return values: f returns 2, got 0", "return"}},
	},
	{
		"one value, two results",
		withResults("(int64, int64)", "return 1"),
		[]spanned{{"not enough return values: f returns 2, got 1", "return 1"}},
	},
	{
		"three values, two results",
		withResults("(int64, int64)", "return 1, 2, 3"),
		[]spanned{{"too many return values: f returns 2, got 3", "1, 2, 3"}},
	},
	{
		"two values, one result",
		withResults("int64", "return 1, 2"),
		[]spanned{{"too many return values: f returns 1, got 2", "1, 2"}},
	},
	{
		"two values, no result",
		withResults("", "return 1, 2"),
		[]spanned{{"f declares no result, so it cannot return a value", "1, 2"}},
	},
	{
		"bare return, one result",
		withResults("int64", "return"),
		[]spanned{{"f must return a value of type int64", "return"}},
	},
	{
		"wrong type in second place",
		withResults("(int64, string)", "return 1, 2"),
		[]spanned{{"cannot use int64 as string in return from f", "2"}},
	},
	{
		"wrong type in both places",
		withResults("(int64, string)", "return \"a\", true"),
		[]spanned{
			{"cannot use string as int64 in return from f", "\"a\""},
			{"cannot use bool as string in return from f", "true"},
		},
	},
	{
		"forwarding another call's results",
		"package main\n\nfunc g() (int64, int64) {\n    return 1, 2\n}\n\n" +
			"func f() (int64, int64) {\n    return g()\n}\n\nfunc main() {\n    f()\n}\n",
		[]spanned{{"not enough return values: f returns 2, got 1", "return g()"}},
	},
	{
		"several results as a single return value",
		"package main\n\nfunc g() (int64, int64) {\n    return 1, 2\n}\n\n" +
			"func f() int64 {\n    return g()\n}\n\nfunc main() {\n    f()\n}\n",
		[]spanned{{"g returns 2 values", "g()"}},
	},
	{
		"unknown result type is reported once",
		withResults("(int64, nosuch)", "return 1, 2"),
		[]spanned{{"unknown type: nosuch", "nosuch"}},
	},
	{
		"missing return",
		withResults("(int64, int64)", "f()"),
		[]spanned{{"f must end in a return", "f"}},
	},
}

// TestRejectsWrongReturns checks a return carries exactly one value of the
// right type for each declared result.
func TestRejectsWrongReturns(t *testing.T) {
	for _, tt := range wrongReturnCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != len(tt.want) {
				t.Fatalf("got %v, want %v", diags, tt.want)
			}
			for i, d := range diags {
				if d.Message != tt.want[i].msg {
					t.Errorf("message %d = %q, want %q", i, d.Message, tt.want[i].msg)
				}
				if got := tt.src[d.Span.Start:d.Span.End]; got != tt.want[i].at {
					t.Errorf("diagnostic %d covers %q, want %q", i, got, tt.want[i].at)
				}
			}
		})
	}
}

// multiAssignCases are assignments of several results, with the diagnostics
// each must produce; none means it must check cleanly.
var multiAssignCases = []struct {
	name string
	src  string
	want []spanned
}{
	{
		"both mutable",
		withMulti("mut q, mut r := divmod(7, 2)", "q, r = divmod(q, r)", "exit(q + r)"),
		nil,
	},
	{
		"loop post",
		withMulti(
			"for mut a, mut b := divmod(9, 1); a > 0; a, b = divmod(a, 2) {",
			"    exit(b)",
			"}",
		),
		nil,
	},
	{
		"mutable global",
		"package main\n\nmut g int64\n\n" + multiFuncs + mainLines("mut r := 0", "g, r = divmod(7, 2)", "exit(g + r)")[len("package main\n\n"):],
		nil,
	},
	{
		"immutable target",
		withMulti("q, mut r := divmod(7, 2)", "q, r = divmod(1, 1)", "exit(q + r)"),
		[]spanned{{"cannot assign to immutable variable q", "q"}},
	},
	{
		"parameter target",
		"package main\n\n" + multiFuncs + "func f(p int64) {\n    mut r := 0\n    p, r = divmod(1, 1)\n    exit(r)\n}\n\nfunc main() {\n    f(1)\n}\n",
		[]spanned{{"cannot assign to parameter p", "p"}},
	},
	{
		"wrong type",
		withMulti(
			"mut s := 0",
			"mut b := true",
			"mut n := 0",
			"s, b, n = three()",
			"exit(s + n)",
			"if b {",
			"}",
		),
		[]spanned{{"cannot use string as int64 in assignment to s", "s"}},
	},
	{
		"same target twice",
		withMulti("mut q := 0", "q, q = divmod(7, 2)", "exit(q)"),
		[]spanned{{"q is assigned more than once", "q"}},
	},
	{
		"count mismatch",
		withMulti("mut a := 0", "mut b := 0", "a, b = three()", "exit(a + b)"),
		[]spanned{
			{"assignment mismatch: 2 variables but three returns 3 values", "a, b = three()"},
		},
	},
	{
		"count mismatch and an immutable target",
		withMulti("a := 0", "mut b := 0", "a, b = one()", "exit(a + b)"),
		[]spanned{
			{"assignment mismatch: 2 variables but one returns 1 value", "a, b = one()"},
			{"cannot assign to immutable variable a", "a"},
		},
	},
	{
		"not a call",
		withMulti("mut a := 0", "mut b := 0", "a, b = 5", "exit(a + b)"),
		[]spanned{{"assignment mismatch: 2 variables but 1 value", "a, b = 5"}},
	},
}

// TestMultiAssign checks an assignment of several results against each rule
// a single assignment follows, plus the count and naming each target once.
func TestMultiAssign(t *testing.T) {
	for _, tt := range multiAssignCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != len(tt.want) {
				t.Fatalf("got %v, want %v", diags, tt.want)
			}
			for i, d := range diags {
				if d.Message != tt.want[i].msg {
					t.Errorf("message %d = %q, want %q", i, d.Message, tt.want[i].msg)
				}
				if got := tt.src[d.Span.Start:d.Span.End]; got != tt.want[i].at {
					t.Errorf("diagnostic %d covers %q, want %q", i, got, tt.want[i].at)
				}
			}
		})
	}
}

// _ discards a result of any type, still counts toward the number of
// results, and may repeat in an assignment.
func TestBlankInMultiBindings(t *testing.T) {
	file, diags := checkTree(
		t,
		withMulti(
			"_, b, _ := three()",
			"mut n := 0",
			"_, _, n = three()",
			"if b {",
			"    exit(n)",
			"}",
		),
	)
	if len(diags) > 0 {
		t.Fatalf("got %v, want no diagnostics", diags)
	}
	main := file.Decls[len(file.Decls)-1].(*ast.Func)
	if got := main.Body[0].(*ast.MultiVar).Vars[1].Type; got != types.Bool {
		t.Errorf("b has type %v, want bool", got)
	}

	_, diags = checkTree(t, withMulti("_, _, _, x := three()", "exit(x)"))
	if len(diags) != 1 ||
		diags[0].Message != "assignment mismatch: 4 variables but three returns 3 values" {
		t.Errorf("got %v", diags)
	}
}
