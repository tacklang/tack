package resolver_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
)

func resolve(t *testing.T, src string) []diag.Diagnostic {
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

	return resolver.New(file).Resolve()
}

func TestResolvesEveryBuiltin(t *testing.T) {
	for _, body := range []string{`print("hi")`, `exit(0)`} {
		t.Run(body, func(t *testing.T) {
			src := "package main\n\nfunc main() {\n    " + body + "\n}\n"
			if diags := resolve(t, src); len(diags) > 0 {
				t.Errorf("%s did not resolve: %+v", body, diags)
			}
		})
	}
}

// Shadowing a builtin is already an error for print; exit joins it by being
// registered in the same table rather than by a second rule.
func TestRejectsDeclarationShadowingABuiltin(t *testing.T) {
	for _, name := range []string{"print", "exit"} {
		t.Run(name, func(t *testing.T) {
			src := "package main\n\nfunc " + name + "() {\n}\n\nfunc main() {\n}\n"

			diags := resolve(t, src)
			if len(diags) == 0 {
				t.Fatalf("declaring func %s() was accepted", name)
			}
			if !strings.Contains(diags[0].Message, "builtin") {
				t.Errorf("message = %q, want it to mention the builtin", diags[0].Message)
			}
		})
	}
}

// resolveFile runs the front end up to the resolver and returns the tree, so a
// test can inspect what the resolver bound.
func resolveFile(t *testing.T, src string) *ast.File {
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

	if diags := resolver.New(file).Resolve(); len(diags) > 0 {
		t.Fatalf("resolve: %s", diag.RenderAllPretty(f, diags))
	}

	return file
}

// An identifier argument must be bound to the parameter it names, since that
// binding is how every later stage learns the argument's type.
func TestBindsIdentifierArgumentToItsParameter(t *testing.T) {
	file := resolveFile(
		t,
		"package main\n\nfunc f(x int64) {\n    exit(x)\n}\n\nfunc main() {\n}\n",
	)

	fn := file.Decls[0].(*ast.Func)
	arg := fn.Body[0].(*ast.ExprStmt).X.(*ast.Call).Args[0].(*ast.Ident)

	if arg.Def == nil {
		t.Fatal("the identifier argument was left unbound")
	}
	param, ok := arg.Def.(*ast.Param)
	if !ok {
		t.Fatalf("bound to %T, want *ast.Param", arg.Def)
	}
	if param != fn.Params[0] {
		t.Error("bound to a different parameter than the one f declares")
	}
}

func TestRejectsUndefinedIdentifierArgument(t *testing.T) {
	diags := resolve(t, "package main\n\nfunc main() {\n    exit(nope)\n}\n")

	if len(diags) == 0 {
		t.Fatal("an undefined identifier argument was accepted")
	}
	if !strings.Contains(diags[0].Message, "undefined") {
		t.Errorf("message = %q, want it to say the name is undefined", diags[0].Message)
	}
}

// A parameter belongs to one function body, not to the file.
func TestParameterIsNotInScopeInAnotherFunction(t *testing.T) {
	src := "package main\n\nfunc f(x int64) {\n}\n\nfunc main() {\n    exit(x)\n}\n"

	diags := resolve(t, src)
	if len(diags) == 0 {
		t.Fatal("one function's parameter was visible inside another")
	}
	if !strings.Contains(diags[0].Message, "undefined") {
		t.Errorf("message = %q, want it to say the name is undefined", diags[0].Message)
	}
}

func TestRejectsDuplicateParameterName(t *testing.T) {
	diags := resolve(t, "package main\n\nfunc f(x int64, x int64) {\n}\n\nfunc main() {\n}\n")

	if len(diags) == 0 {
		t.Fatal("a repeated parameter name was accepted")
	}
	if !strings.Contains(diags[0].Message, "duplicate") {
		t.Errorf("message = %q, want it to mention the duplicate", diags[0].Message)
	}
}

func TestRejectsParametersOnMain(t *testing.T) {
	diags := resolve(t, "package main\n\nfunc main(x int64) {\n}\n")

	if len(diags) == 0 {
		t.Fatal("main was allowed to declare a parameter")
	}
	if !strings.Contains(diags[0].Message, "main") {
		t.Errorf("message = %q, want it to name main", diags[0].Message)
	}
}

// main takes nothing and returns nothing. A result of any type is rejected,
// whether or not the body returns a value.
func TestRejectsAResultOnMain(t *testing.T) {
	for _, src := range []string{
		"package main\n\nfunc main() int64 {\n    return 1\n}\n",
		"package main\n\nfunc main() string {\n    return \"x\"\n}\n",
		"package main\n\nfunc main() bool {\n    return 1 < 2\n}\n",
		"package main\n\nfunc main() int64 {\n}\n",
		"package main\n\nfunc main() nosuchtype {\n}\n",
	} {
		diags := resolve(t, src)
		if len(diags) != 1 {
			t.Errorf("%q: got %d diagnostics %v, want 1", src, len(diags), diags)
			continue
		}
		if diags[0].Message != "main function must not declare a result" {
			t.Errorf("%q: message = %q", src, diags[0].Message)
		}
	}
}

// A main with both parameters and a result gets both reported in one run.
func TestReportsParametersAndResultOnMainTogether(t *testing.T) {
	diags := resolve(t, "package main\n\nfunc main(x int64) int64 {\n    return x\n}\n")

	got := make([]string, 0, len(diags))
	for _, d := range diags {
		got = append(got, d.Message)
	}
	want := []string{
		"main function must not declare parameters",
		"main function must not declare a result",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("messages = %q, want %q", got, want)
	}
}

// Only the function named main is constrained; any other may return a value.
func TestOtherFunctionsMayDeclareAResult(t *testing.T) {
	src := "package main\n\nfunc mainly() int64 {\n    return 1\n}\n\nfunc main() {\n}\n"
	if diags := resolve(t, src); len(diags) > 0 {
		t.Errorf("got %v, want no diagnostics", diags)
	}
}

// Go allows a parameter to shadow a function name, and so does Tack; this pins
// that it is deliberate rather than untested.
func TestParameterMayShadowAFunctionName(t *testing.T) {
	src := "package main\n\nfunc f(g int64) {\n    exit(g)\n}\n\nfunc g() {\n}\n\nfunc main() {\n}\n"

	if diags := resolve(t, src); len(diags) > 0 {
		t.Errorf("a parameter shadowing a function name was rejected: %+v", diags)
	}
}

// A returned identifier is a value position the old call-only walk never
// visited, so it would have been left unbound.
func TestBindsIdentifierInAReturn(t *testing.T) {
	file := resolveFile(
		t,
		"package main\n\nfunc f(x int64) int64 {\n    return x\n}\n\nfunc main() {\n}\n",
	)

	fn := file.Decls[0].(*ast.Func)
	ret := fn.Body[0].(*ast.Return)

	id, ok := ret.Values[0].(*ast.Ident)
	if !ok {
		t.Fatalf("returned expression is %T, want *ast.Ident", ret.Values[0])
	}
	if id.Def == nil {
		t.Fatal("the returned identifier was left unbound")
	}
	if id.Def.(*ast.Param) != fn.Params[0] {
		t.Error("bound to something other than f's parameter")
	}
}

func TestRejectsUndefinedIdentifierInAReturn(t *testing.T) {
	diags := resolve(
		t,
		"package main\n\nfunc f() int64 {\n    return nope\n}\n\nfunc main() {\n}\n",
	)

	if len(diags) == 0 {
		t.Fatal("an undefined identifier in a return was accepted")
	}
	if !strings.Contains(diags[0].Message, "undefined") {
		t.Errorf("message = %q, want it to say the name is undefined", diags[0].Message)
	}
}

func TestBindsCalleeOfACallUsedAsAnArgument(t *testing.T) {
	src := "package main\n\nfunc seven() int64 {\n    return 7\n}\n\nfunc main() {\n    exit(seven())\n}\n"
	file := resolveFile(t, src)

	main := file.Decls[1].(*ast.Func)
	inner := main.Body[0].(*ast.ExprStmt).X.(*ast.Call).Args[0].(*ast.Call)

	if inner.Fn.(*ast.Ident).Def == nil {
		t.Fatal("the callee of a nested call was left unbound")
	}
}

func TestRejectsUndefinedCalleeInAnArgument(t *testing.T) {
	diags := resolve(t, "package main\n\nfunc main() {\n    exit(nosuchfunc())\n}\n")

	if len(diags) == 0 {
		t.Fatal("a call to an undefined function in argument position was accepted")
	}
	if !strings.Contains(diags[0].Message, "undefined") {
		t.Errorf("message = %q, want it to say the name is undefined", diags[0].Message)
	}
}

func TestBindsIdentifierInsideANestedCall(t *testing.T) {
	src := "package main\n\nfunc g(x int64) int64 {\n    return x\n}\n\n" +
		"func f(n int64) {\n    exit(g(n))\n}\n\nfunc main() {\n}\n"
	file := resolveFile(t, src)

	f := file.Decls[1].(*ast.Func)
	inner := f.Body[0].(*ast.ExprStmt).X.(*ast.Call).Args[0].(*ast.Call)

	id, ok := inner.Args[0].(*ast.Ident)
	if !ok {
		t.Fatalf("nested argument is %T, want *ast.Ident", inner.Args[0])
	}
	if id.Def == nil {
		t.Fatal("an identifier inside a nested call was left unbound")
	}
	if id.Def.(*ast.Param) != f.Params[0] {
		t.Error("bound to something other than f's parameter")
	}
}

func TestBindsIdentifiersInsideAnIf(t *testing.T) {
	src := "package main\n\nfunc f(n int64) {\n    if n < 2 {\n        exit(n)\n    }\n}\n\nfunc main() {\n}\n"
	file := resolveFile(t, src)

	fn := file.Decls[0].(*ast.Func)
	ifs := fn.Body[0].(*ast.If)

	cond := ifs.Cond.(*ast.Binary)
	if id, ok := cond.X.(*ast.Ident); !ok || id.Def == nil {
		t.Error("the identifier in the condition was left unbound")
	}

	call := ifs.Then[0].(*ast.ExprStmt).X.(*ast.Call)
	if id, ok := call.Args[0].(*ast.Ident); !ok || id.Def == nil {
		t.Error("the identifier inside the then block was left unbound")
	}
}

func TestBindsIdentifiersInsideAnElseBlock(t *testing.T) {
	src := "package main\n\nfunc f(n int64) {\n    if n < 2 {\n    } else {\n        exit(n)\n    }\n}\n\nfunc main() {\n}\n"
	file := resolveFile(t, src)

	ifs := file.Decls[0].(*ast.Func).Body[0].(*ast.If)
	call := ifs.Else[0].(*ast.ExprStmt).X.(*ast.Call)
	if id, ok := call.Args[0].(*ast.Ident); !ok || id.Def == nil {
		t.Error("the identifier inside the else block was left unbound")
	}
}

func TestBindsIdentifiersInBothOperandsOfABinary(t *testing.T) {
	src := "package main\n\nfunc f(a int64, b int64) int64 {\n    return a + b\n}\n\nfunc main() {\n}\n"
	file := resolveFile(t, src)

	bin := file.Decls[0].(*ast.Func).Body[0].(*ast.Return).Values[0].(*ast.Binary)
	for name, side := range map[string]ast.Expr{"left": bin.X, "right": bin.Y} {
		id, ok := side.(*ast.Ident)
		if !ok {
			t.Fatalf("%s operand is %T, want *ast.Ident", name, side)
		}
		if id.Def == nil {
			t.Errorf("the %s operand was left unbound", name)
		}
	}
}

func TestRejectsUndefinedIdentifierInACondition(t *testing.T) {
	diags := resolve(t, "package main\n\nfunc main() {\n    if nope < 2 {\n    }\n}\n")

	if len(diags) == 0 {
		t.Fatal("an undefined identifier in a condition was accepted")
	}
	if !strings.Contains(diags[0].Message, "undefined") {
		t.Errorf("message = %q, want it to say the name is undefined", diags[0].Message)
	}
}
