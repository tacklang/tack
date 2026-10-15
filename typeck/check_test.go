package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/typeck"
	"github.com/tacklang/tack/types"
)

// check runs the front end up to and including type checking, and returns
// only the type checker's diagnostics. Earlier stages failing is a broken
// test case, not a result, so it aborts.
func check(t *testing.T, src string) []diag.Diagnostic {
	t.Helper()

	f := &source.File{ID: source.FileID("test"), Filename: "case.tck", Data: []byte(src)}

	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatalf("lex: %s", diag.RenderAllPretty(f, diags))
	}

	file, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
	if len(diags) > 0 {
		t.Fatalf("parse: %s", diag.RenderAllPretty(f, diags))
	}

	if diags := resolver.New(ast.SingleFile(file), stdtest.MustBuiltins()).
		Resolve(); len(
		diags,
	) > 0 {
		t.Fatalf("resolve: %s", diag.RenderAllPretty(f, diags))
	}

	return typeck.New(ast.SingleFile(file), f, stdtest.MustBuiltins()).Check()
}

func inMain(body string) string {
	return "package main\n\nfunc main() {\n    " + body + "\n}\n"
}

func TestAcceptsWellTypedBuiltinCalls(t *testing.T) {
	for _, body := range []string{`print("hi")`, `exit(0)`, `exit(42)`} {
		t.Run(body, func(t *testing.T) {
			if diags := check(t, inMain(body)); len(diags) > 0 {
				t.Errorf("%s was rejected: %+v", body, diags)
			}
		})
	}
}

func TestRejectsBuiltinArgumentOfTheWrongType(t *testing.T) {
	tests := []struct {
		body string
		want string
	}{
		{`print(42)`, "cannot use int64 as string"},
		{`exit("boom")`, "cannot use string as int64"},
	}

	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			diags := check(t, inMain(tt.body))
			if len(diags) == 0 {
				t.Fatalf("%s was accepted", tt.body)
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
		})
	}
}

func TestRejectsBuiltinCallWithWrongArgumentCount(t *testing.T) {
	for _, body := range []string{`print()`, `exit()`} {
		t.Run(body, func(t *testing.T) {
			diags := check(t, inMain(body))
			if len(diags) == 0 {
				t.Fatalf("%s was accepted", body)
			}
			if !strings.Contains(diags[0].Message, "argument") {
				t.Errorf("message = %q, want it to mention the argument count", diags[0].Message)
			}
		})
	}
}

// A call passing arguments to a function that declares no parameters used to
// compile, with the argument silently discarded by the backend.
func TestRejectsArgumentsToAFunctionThatTakesNone(t *testing.T) {
	src := "package main\n\nfunc f() {\n}\n\nfunc main() {\n    f(\"dropped\")\n}\n"

	diags := check(t, src)
	if len(diags) == 0 {
		t.Fatal(
			"an argument passed to a parameterless function was accepted and would be discarded",
		)
	}
	if !strings.Contains(diags[0].Message, "argument") {
		t.Errorf("message = %q, want it to mention the argument count", diags[0].Message)
	}
}

func TestAcceptsCallToAFunctionThatTakesNone(t *testing.T) {
	src := "package main\n\nfunc f() {\n}\n\nfunc main() {\n    f()\n}\n"

	if diags := check(t, src); len(diags) > 0 {
		t.Errorf("a correct call was rejected: %+v", diags)
	}
}

// The caret has to land on the offending argument, not the whole call.
func TestPointsAtTheOffendingArgument(t *testing.T) {
	src := inMain(`print(42)`)

	diags := check(t, src)
	if len(diags) == 0 {
		t.Fatal("print(42) was accepted")
	}
	if got := src[diags[0].Span.Start:diags[0].Span.End]; got != "42" {
		t.Errorf("diagnostic spans %q, want %q", got, "42")
	}
}

func TestReportsEveryBadCallNotJustTheFirst(t *testing.T) {
	src := "package main\n\nfunc main() {\n    print(1)\n    print(2)\n}\n"

	if diags := check(t, src); len(diags) != 2 {
		t.Errorf("got %d diagnostics, want 2 (one per bad call)", len(diags))
	}
}

func TestSeverityIsError(t *testing.T) {
	diags := check(t, inMain(`print(42)`))
	if len(diags) == 0 {
		t.Fatal("print(42) was accepted")
	}
	if diags[0].Severity != diag.Error {
		t.Errorf("severity = %v, want %v", diags[0].Severity, diag.Error)
	}
}

// Type checking must not depend on the resolver having run, beyond names being
// defined: a hand-built tree with an unsupported argument is still rejected.
func TestRejectsUnknownArgumentExpression(t *testing.T) {
	file := &ast.File{
		Package: "main",
		Decls: []ast.Decl{
			&ast.Func{Name: "main", Body: []ast.Stmt{
				&ast.ExprStmt{X: &ast.Call{
					Fn:   &ast.Ident{Name: "print"},
					Args: []ast.Expr{&ast.Ident{Name: "x"}},
				}},
			}},
		},
	}

	if diags := typeck.New(ast.SingleFile(file), nil, stdtest.MustBuiltins()).
		Check(); len(
		diags,
	) == 0 {
		t.Error("an argument of an unsupported expression kind was accepted")
	}
}

// checkFile is check, but it hands back the tree so a test can inspect what
// the checker annotated.
func checkFile(t *testing.T, src string) (*ast.File, []diag.Diagnostic) {
	t.Helper()

	f := &source.File{ID: source.FileID("test"), Filename: "case.tck", Data: []byte(src)}

	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatalf("lex: %s", diag.RenderAllPretty(f, diags))
	}

	file, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
	if len(diags) > 0 {
		t.Fatalf("parse: %s", diag.RenderAllPretty(f, diags))
	}

	if diags := resolver.New(ast.SingleFile(file), stdtest.MustBuiltins()).
		Resolve(); len(
		diags,
	) > 0 {
		t.Fatalf("resolve: %s", diag.RenderAllPretty(f, diags))
	}

	return file, typeck.New(ast.SingleFile(file), f, stdtest.MustBuiltins()).Check()
}

// The parameter's type is what the backend reads to size and place it, so the
// checker has to record it rather than leave the type as syntax.
func TestAnnotatesParameterTypes(t *testing.T) {
	file, diags := checkFile(
		t,
		"package main\n\nfunc f(x int64, s string) {\n}\n\nfunc main() {\n}\n",
	)
	if len(diags) > 0 {
		t.Fatalf("check reported %d diagnostic(s): %+v", len(diags), diags)
	}

	params := file.Decls[0].(*ast.Func).Params
	for i, want := range []types.Type{types.Int64, types.String} {
		if params[i].Type != want {
			t.Errorf("parameter %d typ = %v, want %v", i, params[i].Type, want)
		}
	}
}

func TestRejectsUnknownParameterType(t *testing.T) {
	_, diags := checkFile(t, "package main\n\nfunc f(x nosuchtype) {\n}\n\nfunc main() {\n}\n")

	if len(diags) == 0 {
		t.Fatal("a parameter of an unknown type was accepted")
	}
	if !strings.Contains(diags[0].Message, "nosuchtype") {
		t.Errorf("message = %q, want it to name the unknown type", diags[0].Message)
	}
}

func TestTypesAParameterReferenceFromItsDeclaration(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
		ok   bool
	}{
		{
			name: "int64 parameter to exit",
			src:  "func f(x int64) {\n    exit(x)\n}",
			ok:   true,
		},
		{
			name: "string parameter to print",
			src:  "func f(s string) {\n    print(s)\n}",
			ok:   true,
		},
		{
			name: "string parameter to exit",
			src:  "func f(s string) {\n    exit(s)\n}",
			want: "cannot use string as int64",
		},
		{
			name: "int64 parameter to print",
			src:  "func f(x int64) {\n    print(x)\n}",
			want: "cannot use int64 as string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkFile(t, "package main\n\n"+tt.src+"\n\nfunc main() {\n}\n")

			if tt.ok {
				if len(diags) > 0 {
					t.Fatalf("rejected: %+v", diags)
				}
				return
			}
			if len(diags) == 0 {
				t.Fatal("accepted")
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
		})
	}
}

func TestChecksArgumentTypesAgainstDeclaredParameters(t *testing.T) {
	tests := []struct {
		call string
		want string
	}{
		{"f(1, \"hi\")", ""},
		{"f(\"hi\", 1)", "cannot use string as int64"},
		{"f(1)", "argument"},
		{"f(1, \"hi\", 2)", "argument"},
	}

	for _, tt := range tests {
		t.Run(tt.call, func(t *testing.T) {
			src := "package main\n\nfunc f(x int64, s string) {\n}\n\nfunc main() {\n    " + tt.call + "\n}\n"
			_, diags := checkFile(t, src)

			if tt.want == "" {
				if len(diags) > 0 {
					t.Fatalf("%s was rejected: %+v", tt.call, diags)
				}
				return
			}
			if len(diags) == 0 {
				t.Fatalf("%s was accepted", tt.call)
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
		})
	}
}

// Passing a parameter straight through to another function is the smallest
// case where an argument's type comes from a binding rather than a literal.
func TestAcceptsForwardingAParameterToAnotherFunction(t *testing.T) {
	src := "package main\n\nfunc g(s string) {\n    print(s)\n}\n\n" +
		"func f(s string) {\n    g(s)\n}\n\nfunc main() {\n    f(\"hi\")\n}\n"

	if _, diags := checkFile(t, src); len(diags) > 0 {
		t.Errorf("forwarding a parameter was rejected: %+v", diags)
	}
}

func TestAnnotatesResultType(t *testing.T) {
	file, diags := checkFile(
		t,
		"package main\n\nfunc f() int64 {\n    return 1\n}\n\nfunc main() {\n}\n",
	)
	if len(diags) > 0 {
		t.Fatalf("check reported %d diagnostic(s): %+v", len(diags), diags)
	}

	if got := file.Decls[0].(*ast.Func).ResultTypes; len(got) != 1 || got[0] != types.Int64 {
		t.Errorf("ResultType = %v, want %v", got, types.Int64)
	}
}

// Check records its Table on the file, so the backend names types through
// the same one the checker used.
func TestRecordsTheTypeTableOnTheFile(t *testing.T) {
	file, diags := checkFile(t, "package main\n\nfunc main() {\n}\n")
	if len(diags) > 0 {
		t.Fatalf("check reported %d diagnostic(s): %+v", len(diags), diags)
	}
	if file.Types == nil {
		t.Fatal("Check left File.Types nil")
	}
	if got := file.Types.Name(types.Int64); got != "int64" {
		t.Errorf("File.Types.Name(Int) = %q, want %q", got, "int64")
	}
}

func TestRejectsUnknownResultType(t *testing.T) {
	_, diags := checkFile(
		t,
		"package main\n\nfunc f() nosuchtype {\n    return 1\n}\n\nfunc main() {\n}\n",
	)

	if len(diags) == 0 {
		t.Fatal("an unknown result type was accepted")
	}
	if !strings.Contains(diags[0].Message, "nosuchtype") {
		t.Errorf("message = %q, want it to name the unknown type", diags[0].Message)
	}
}

func TestChecksReturnedValueAgainstTheDeclaredResult(t *testing.T) {
	tests := []struct {
		name string
		decl string
		want string
	}{
		{"int64 returned as int64", "func f() int64 {\n    return 1\n}", ""},
		{"string returned as string", "func f() string {\n    return \"hi\"\n}", ""},
		{
			"string returned as int64",
			"func f() int64 {\n    return \"hi\"\n}",
			"cannot use string as int64",
		},
		{
			"int64 returned as string",
			"func f() string {\n    return 1\n}",
			"cannot use int64 as string",
		},
		{
			"value returned from a function with no result",
			"func f() {\n    return 1\n}",
			"no result",
		},
		{
			"bare return from a function with a result",
			"func f() int64 {\n    return\n}",
			"must return",
		},
		{"no return at all", "func f() int64 {\n}", "must end"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkFile(t, "package main\n\n"+tt.decl+"\n\nfunc main() {\n}\n")

			if tt.want == "" {
				if len(diags) > 0 {
					t.Fatalf("rejected: %+v", diags)
				}
				return
			}
			if len(diags) == 0 {
				t.Fatal("accepted")
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
		})
	}
}

func TestBareReturnIsFineWhenNothingIsDeclared(t *testing.T) {
	if _, diags := checkFile(
		t,
		"package main\n\nfunc f() {\n    return\n}\n\nfunc main() {\n}\n",
	); len(
		diags,
	) > 0 {
		t.Errorf("a bare return was rejected: %+v", diags)
	}
}

func TestTypesACallByWhatItReturns(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "int64 result into exit",
			src:  "func seven() int64 {\n    return 7\n}\n\nfunc main() {\n    exit(seven())\n}",
		},
		{
			name: "string result into print",
			src:  "func hi() string {\n    return \"hi\"\n}\n\nfunc main() {\n    print(hi())\n}",
		},
		{
			name: "string result into exit",
			src:  "func hi() string {\n    return \"hi\"\n}\n\nfunc main() {\n    exit(hi())\n}",
			want: "cannot use string as int64",
		},
		{
			name: "call to a function returning nothing",
			src:  "func nothing() {\n}\n\nfunc main() {\n    exit(nothing())\n}",
			want: "no value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkFile(t, "package main\n\n"+tt.src+"\n")

			if tt.want == "" {
				if len(diags) > 0 {
					t.Fatalf("rejected: %+v", diags)
				}
				return
			}
			if len(diags) == 0 {
				t.Fatal("accepted")
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
		})
	}
}

func TestAcceptsReturningTheResultOfAnotherCall(t *testing.T) {
	src := "package main\n\nfunc g() int64 {\n    return 1\n}\n\n" +
		"func f() int64 {\n    return g()\n}\n\nfunc main() {\n    exit(f())\n}\n"

	if _, diags := checkFile(t, src); len(diags) > 0 {
		t.Errorf("returning a call's result was rejected: %+v", diags)
	}
}

func TestChecksArgumentsOfANestedCall(t *testing.T) {
	src := "package main\n\nfunc g(x int64) int64 {\n    return x\n}\n\n" +
		"func main() {\n    exit(g(\"hi\"))\n}\n"

	_, diags := checkFile(t, src)
	if len(diags) == 0 {
		t.Fatal("a badly typed argument to a nested call was accepted")
	}
	if !strings.Contains(diags[0].Message, "cannot use string as int64") {
		t.Errorf("message = %q, want it to report the nested argument", diags[0].Message)
	}
}

func TestChecksBinaryOperands(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"1 + 2", ""},
		{"1 - 2", ""},
		{"1 * 2", ""},
		{"1 / 2", ""},
		{"1 % 2", ""},
		{"1 + \"hi\"", "operator + is not defined on string"},
		{"\"a\" + \"b\"", "operator + is not defined on string"},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			_, diags := checkFile(t, "package main\n\nfunc main() {\n    exit("+tt.expr+")\n}\n")

			if tt.want == "" {
				if len(diags) > 0 {
					t.Fatalf("rejected: %+v", diags)
				}
				return
			}
			if len(diags) == 0 {
				t.Fatal("accepted")
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
		})
	}
}

func TestComparisonYieldsBool(t *testing.T) {
	src := "package main\n\nfunc less() bool {\n    return 1 < 2\n}\n\nfunc main() {\n}\n"

	if _, diags := checkFile(t, src); len(diags) > 0 {
		t.Errorf("a comparison returned as a bool was rejected: %+v", diags)
	}
}

func TestRejectsBoolWhereAnIntIsWanted(t *testing.T) {
	_, diags := checkFile(t, "package main\n\nfunc main() {\n    exit(1 < 2)\n}\n")

	if len(diags) == 0 {
		t.Fatal("a bool was accepted as an exit status")
	}
	if !strings.Contains(diags[0].Message, "cannot use bool as int64") {
		t.Errorf(
			"message = %q, want it to contain %q",
			diags[0].Message,
			"cannot use bool as int64",
		)
	}
}

func TestConditionMustBeBool(t *testing.T) {
	_, diags := checkFile(t, "package main\n\nfunc main() {\n    if 1 {\n    }\n}\n")

	if len(diags) == 0 {
		t.Fatal("an int64 was accepted as a condition")
	}
	if !strings.Contains(diags[0].Message, "condition") {
		t.Errorf("message = %q, want it to mention the condition", diags[0].Message)
	}
}

func TestAcceptsBoolCondition(t *testing.T) {
	src := "package main\n\nfunc main() {\n    if 1 < 2 {\n        print(\"yes\")\n    }\n}\n"

	if _, diags := checkFile(t, src); len(diags) > 0 {
		t.Errorf("a comparison condition was rejected: %+v", diags)
	}
}

func TestChecksStatementsInsideBothBranches(t *testing.T) {
	src := "package main\n\nfunc main() {\n    if 1 < 2 {\n        print(42)\n    } else {\n        exit(\"no\")\n    }\n}\n"

	_, diags := checkFile(t, src)
	if len(diags) != 2 {
		t.Errorf("got %d diagnostics, want 2 (one per branch)", len(diags))
	}
}

// An if is a terminating statement when both branches terminate, which is what
// lets fib end in an if rather than a bare return.
func TestIfWithBothBranchesReturningTerminates(t *testing.T) {
	src := "package main\n\nfunc f(n int64) int64 {\n    if n < 2 {\n        return 1\n    } else {\n        return 2\n    }\n}\n\nfunc main() {\n}\n"

	if _, diags := checkFile(t, src); len(diags) > 0 {
		t.Errorf("a function ending in a fully returning if was rejected: %+v", diags)
	}
}

func TestIfWithoutElseDoesNotTerminate(t *testing.T) {
	src := "package main\n\nfunc f(n int64) int64 {\n    if n < 2 {\n        return 1\n    }\n}\n\nfunc main() {\n}\n"

	_, diags := checkFile(t, src)
	if len(diags) == 0 {
		t.Fatal("a function whose last statement is an if with no else was accepted")
	}
	if !strings.Contains(diags[0].Message, "must end") {
		t.Errorf("message = %q, want it to say the function must end in a return", diags[0].Message)
	}
}

func TestAcceptsBoolAsAParameterAndResult(t *testing.T) {
	src := "package main\n\nfunc pick(b bool) bool {\n    return b\n}\n\nfunc main() {\n}\n"

	if _, diags := checkFile(t, src); len(diags) > 0 {
		t.Errorf("bool as a parameter and result was rejected: %+v", diags)
	}
}

func TestFibTypechecks(t *testing.T) {
	src := "package main\n\n" +
		"func fib(n int64) int64 {\n" +
		"    if n < 2 {\n        return n\n    }\n" +
		"    return fib(n - 1) + fib(n - 2)\n" +
		"}\n\n" +
		"func main() {\n    exit(fib(10))\n}\n"

	if _, diags := checkFile(t, src); len(diags) > 0 {
		t.Errorf("fib was rejected: %+v", diags)
	}
}

// A builtin returns nothing, so using one as a value is reported like any
// other function that returns nothing, wherever the value is wanted.
func TestBuiltinUsedAsAValueIsReported(t *testing.T) {
	tests := []string{
		inMain("exit(print(\"x\"))"),
		inMain("if print(\"x\") == 1 {\n    }"),
		"package main\n\nfunc f() int64 {\n    return exit(1)\n}\n\nfunc main() {\n    exit(f())\n}\n",
	}
	for _, src := range tests {
		diags := check(t, src)
		if len(diags) == 0 {
			t.Errorf("%q: no diagnostics, want the builtin reported", src)
		}
	}
}

// An unknown type is reported once, where it is written. Everything that then
// has no type because of it is not reported again, and nothing leaks the
// compiler's own type names.
func TestUnknownTypeIsReportedOnce(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			"parameter read as an argument",
			"package main\n\nfunc f(x foo) {\n    exit(x)\n}\n\nfunc main() {\n    f(1)\n}\n",
		},
		{
			"parameter in arithmetic",
			"package main\n\nfunc f(x foo) int64 {\n    return x + 1\n}\n\nfunc main() {\n    exit(f(1))\n}\n",
		},
		{
			"parameter as a condition",
			"package main\n\nfunc f(x foo) {\n    if x {\n    }\n}\n\nfunc main() {\n    f(1)\n}\n",
		},
		{
			"parameter returned",
			"package main\n\nfunc f(x foo) int64 {\n    return x\n}\n\nfunc main() {\n    exit(f(1))\n}\n",
		},
		{
			"result type",
			"package main\n\nfunc f() foo {\n    return 1\n}\n\nfunc main() {\n    f()\n}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diags := check(t, tt.src)
			got := make([]string, len(diags))
			for i, d := range diags {
				got[i] = d.Message
			}
			if strings.Join(got, "\n") != "unknown type: foo" {
				t.Errorf("got %q, want only [\"unknown type: foo\"]", got)
			}
		})
	}
}

// operatorErrorCases are binary expressions an operator is not defined on,
// with the one diagnostic each must produce: an expression is reported
// once, at the first operand the operator is not defined on, however many
// of its operands are wrong.
var operatorErrorCases = []struct {
	name string
	src  string
	want spanned
}{
	{
		"two strings added",
		inMain("print(\"a\" + \"b\")"),
		spanned{"operator + is not defined on string", "\"a\""},
	},
	{
		"two strings subtracted",
		inMain("print(\"a\" - \"b\")"),
		spanned{"operator - is not defined on string", "\"a\""},
	},
	{
		"two strings multiplied",
		inMain("print(\"a\" * \"b\")"),
		spanned{"operator * is not defined on string", "\"a\""},
	},
	{
		"two strings divided",
		inMain("print(\"a\" / \"b\")"),
		spanned{"operator / is not defined on string", "\"a\""},
	},
	{
		"two strings remainder",
		inMain("print(\"a\" % \"b\")"),
		spanned{"operator % is not defined on string", "\"a\""},
	},
	{
		"two bools added",
		inMain("exit(true + false)"),
		spanned{"operator + is not defined on bool", "true"},
	},
	{
		"string and bool",
		inMain("exit(\"a\" - true)"),
		spanned{"operator - is not defined on string", "\"a\""},
	},
	{
		"only the right operand",
		inMain("exit(1 + \"b\")"),
		spanned{"operator + is not defined on string", "\"b\""},
	},
	{
		"two arrays",
		inMain("exit([1]int64{1} + [1]int64{2})"),
		spanned{"operator + is not defined on [1]int64", "[1]int64{1}"},
	},
	{
		"two integers and",
		inMain("if 1 && 2 {\n    }"),
		spanned{"operator && is not defined on int64", "1"},
	},
	{
		"two strings or",
		inMain("if \"a\" || \"b\" {\n    }"),
		spanned{"operator || is not defined on string", "\"a\""},
	},
	{
		"only the right of and",
		inMain("if true && 2 {\n    }"),
		spanned{"operator && is not defined on int64", "2"},
	},
	{
		"nested",
		inMain("print((\"a\" + \"b\") + \"c\")"),
		spanned{"operator + is not defined on string", "\"a\""},
	},
	{
		"global",
		"package main\n\nvar g = \"a\" + \"b\"\n\nfunc main() {\n    print(g)\n}\n",
		spanned{"operator + is not defined on string", "\"a\""},
	},
}

// TestReportsAnOperatorErrorOncePerExpression checks each of
// operatorErrorCases gives exactly its one diagnostic.
func TestReportsAnOperatorErrorOncePerExpression(t *testing.T) {
	for _, tt := range operatorErrorCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != 1 {
				t.Fatalf("got %v, want one diagnostic", diags)
			}
			if diags[0].Message != tt.want.msg {
				t.Errorf("message = %q, want %q", diags[0].Message, tt.want.msg)
			}
			if got := tt.src[diags[0].Span.Start:diags[0].Span.End]; got != tt.want.at {
				t.Errorf("diagnostic covers %q, want %q", got, tt.want.at)
			}
		})
	}
}
