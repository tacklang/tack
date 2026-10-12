package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
)

// parse runs the lexer and parser over src. It fails the test if lexing
// reports anything, so a case only ever exercises the parser.
func parse(t *testing.T, src string) (*ast.File, []diag.Diagnostic) {
	t.Helper()

	f := &source.File{ID: source.FileID("test"), Filename: "case.tck", Data: []byte(src)}

	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatalf("lex: %s", diag.RenderAllPretty(f, diags))
	}

	return parser.NewParser(f.ID, lex.Tokens()).Parse()
}

// callArgs returns the arguments of the sole call in main's body.
func callArgs(t *testing.T, src string) []ast.Expr {
	t.Helper()

	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags)
	}

	fn, ok := file.Decls[len(file.Decls)-1].(*ast.Func)
	if !ok {
		t.Fatalf("last declaration is %T, want *ast.Func", file.Decls[len(file.Decls)-1])
	}
	stmt, ok := fn.Body[0].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("first statement is %T, want *ast.ExprStmt", fn.Body[0])
	}
	call, ok := stmt.X.(*ast.Call)
	if !ok {
		t.Fatalf("statement expression is %T, want *ast.Call", stmt.X)
	}
	return call.Args
}

// firstArg returns the sole argument of the sole call in main's body.
func firstArg(t *testing.T, src string) ast.Expr {
	t.Helper()

	args := callArgs(t, src)
	if len(args) != 1 {
		t.Fatalf("call has %d arguments, want 1", len(args))
	}
	return args[0]
}

func TestParsesIntegerLiteralArgument(t *testing.T) {
	tests := []struct {
		src  string
		want int64
	}{
		{"exit(0)", 0},
		{"exit(42)", 42},
		{"exit(9223372036854775807)", 9223372036854775807},
	}

	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			arg := firstArg(t, "package main\n\nfunc main() {\n    "+tt.src+"\n}\n")

			lit, ok := arg.(*ast.IntLit)
			if !ok {
				t.Fatalf("argument is %T, want *ast.IntLit", arg)
			}
			if lit.Value != tt.want {
				t.Errorf("Value = %d, want %d", lit.Value, tt.want)
			}
		})
	}
}

// The literal's span is what a later diagnostic points its caret at, so it has
// to cover the digits and nothing else.
func TestIntegerLiteralSpansOnlyTheDigits(t *testing.T) {
	src := "package main\n\nfunc main() {\n    exit(42)\n}\n"

	lit, ok := firstArg(t, src).(*ast.IntLit)
	if !ok {
		t.Fatalf("argument is not an *ast.IntLit")
	}

	if got := src[lit.Span.Start:lit.Span.End]; got != "42" {
		t.Errorf("span covers %q, want %q", got, "42")
	}
}

// The lexer hands over the digits undecoded, so the parser is the first place
// that can notice a literal too large to represent.
func TestRejectsIntegerLiteralOutOfRange(t *testing.T) {
	_, diags := parse(t, "package main\n\nfunc main() {\n    exit(9223372036854775808)\n}\n")

	if len(diags) == 0 {
		t.Fatal("an out-of-range integer literal parsed without a diagnostic")
	}
	if !strings.Contains(diags[0].Message, "range") {
		t.Errorf("message = %q, want it to mention the value being out of range", diags[0].Message)
	}
}

func TestStillParsesStringLiteralArgument(t *testing.T) {
	arg := firstArg(t, "package main\n\nfunc main() {\n    print(\"hi\")\n}\n")

	lit, ok := arg.(*ast.StrLit)
	if !ok {
		t.Fatalf("argument is %T, want *ast.StrLit", arg)
	}
	if lit.Value != "hi" {
		t.Errorf("Value = %q, want %q", lit.Value, "hi")
	}
}

func TestParsesCommaSeparatedArguments(t *testing.T) {
	args := callArgs(t, "package main\n\nfunc main() {\n    f(1, \"hi\", 2)\n}\n")

	if len(args) != 3 {
		t.Fatalf("call has %d arguments, want 3", len(args))
	}
	if lit, ok := args[0].(*ast.IntLit); !ok || lit.Value != 1 {
		t.Errorf("argument 0 = %#v, want IntLit 1", args[0])
	}
	if lit, ok := args[1].(*ast.StrLit); !ok || lit.Value != "hi" {
		t.Errorf("argument 1 = %#v, want StrLit \"hi\"", args[1])
	}
	if lit, ok := args[2].(*ast.IntLit); !ok || lit.Value != 2 {
		t.Errorf("argument 2 = %#v, want IntLit 2", args[2])
	}
}

// An identifier argument is how a parameter gets used, so it has to reach the
// AST as an Ident for the resolver to bind.
func TestParsesIdentifierArgument(t *testing.T) {
	src := "package main\n\nfunc f(x int) {\n    exit(x)\n}\n\nfunc main() {\n}\n"

	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags)
	}

	fn := file.Decls[0].(*ast.Func)
	call := fn.Body[0].(*ast.ExprStmt).X.(*ast.Call)
	if len(call.Args) != 1 {
		t.Fatalf("call has %d arguments, want 1", len(call.Args))
	}

	id, ok := call.Args[0].(*ast.Ident)
	if !ok {
		t.Fatalf("argument is %T, want *ast.Ident", call.Args[0])
	}
	if id.Name != "x" {
		t.Errorf("Name = %q, want %q", id.Name, "x")
	}
}

func TestRejectsArgumentsWithoutASeparatingComma(t *testing.T) {
	_, diags := parse(t, "package main\n\nfunc main() {\n    f(1 2)\n}\n")

	if len(diags) == 0 {
		t.Fatal("two arguments with no comma between them parsed without a diagnostic")
	}
}

// Parameter lists already parsed before this slice; these pin that, since the
// resolver and the backend are about to depend on the shape.
func TestParsesParameterList(t *testing.T) {
	file, diags := parse(t, "package main\n\nfunc f(x int, s string) {\n}\n\nfunc main() {\n}\n")
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags)
	}

	fn := file.Decls[0].(*ast.Func)
	if len(fn.Params) != 2 {
		t.Fatalf("f has %d parameters, want 2", len(fn.Params))
	}

	for i, want := range []struct{ name, typ string }{{"x", "int"}, {"s", "string"}} {
		if fn.Params[i].Name != want.name {
			t.Errorf("parameter %d name = %q, want %q", i, fn.Params[i].Name, want.name)
		}
		typ, ok := fn.Params[i].Type.(*ast.Ident)
		if !ok {
			t.Fatalf("parameter %d type is %T, want *ast.Ident", i, fn.Params[i].Type)
		}
		if typ.Name != want.typ {
			t.Errorf("parameter %d type = %q, want %q", i, typ.Name, want.typ)
		}
	}
}

func TestParsesEmptyParameterList(t *testing.T) {
	file, diags := parse(t, "package main\n\nfunc main() {\n}\n")
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags)
	}

	if got := len(file.Decls[0].(*ast.Func).Params); got != 0 {
		t.Errorf("main has %d parameters, want 0", got)
	}
}

func TestParsesResultType(t *testing.T) {
	file, diags := parse(t, "package main\n\nfunc f() int {\n    return 1\n}\n\nfunc main() {\n}\n")
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags)
	}

	fn := file.Decls[0].(*ast.Func)
	if fn.Result == nil {
		t.Fatal("the declared result type was dropped")
	}
	id, ok := fn.Result.(*ast.Ident)
	if !ok {
		t.Fatalf("result type is %T, want *ast.Ident", fn.Result)
	}
	if id.Name != "int" {
		t.Errorf("result type = %q, want %q", id.Name, "int")
	}
}

func TestFunctionWithoutResultTypeHasNone(t *testing.T) {
	file, diags := parse(t, "package main\n\nfunc main() {\n}\n")
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags)
	}

	if fn := file.Decls[0].(*ast.Func); fn.Result != nil {
		t.Errorf("Result = %#v, want nil", fn.Result)
	}
}

func TestParsesReturnWithAValue(t *testing.T) {
	file, diags := parse(
		t,
		"package main\n\nfunc f() int {\n    return 42\n}\n\nfunc main() {\n}\n",
	)
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags)
	}

	stmt := file.Decls[0].(*ast.Func).Body[0]
	ret, ok := stmt.(*ast.Return)
	if !ok {
		t.Fatalf("statement is %T, want *ast.Return", stmt)
	}
	lit, ok := ret.Value.(*ast.IntLit)
	if !ok {
		t.Fatalf("returned expression is %T, want *ast.IntLit", ret.Value)
	}
	if lit.Value != 42 {
		t.Errorf("returned %d, want 42", lit.Value)
	}
}

func TestParsesBareReturn(t *testing.T) {
	file, diags := parse(t, "package main\n\nfunc f() {\n    return\n}\n\nfunc main() {\n}\n")
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags)
	}

	stmt := file.Decls[0].(*ast.Func).Body[0]
	ret, ok := stmt.(*ast.Return)
	if !ok {
		t.Fatalf("statement is %T, want *ast.Return", stmt)
	}
	if ret.Value != nil {
		t.Errorf("Value = %#v, want nil for a bare return", ret.Value)
	}
}

// A call has to be usable where a value is expected, or nothing could ever
// consume what a function returns.
func TestParsesCallAsAnArgument(t *testing.T) {
	arg := firstArg(t, "package main\n\nfunc main() {\n    exit(seven())\n}\n")

	call, ok := arg.(*ast.Call)
	if !ok {
		t.Fatalf("argument is %T, want *ast.Call", arg)
	}
	if id, ok := call.Fn.(*ast.Ident); !ok || id.Name != "seven" {
		t.Errorf("callee = %#v, want an Ident named seven", call.Fn)
	}
}

func TestParsesNestedCallArguments(t *testing.T) {
	arg := firstArg(t, "package main\n\nfunc main() {\n    f(g(h()))\n}\n")

	outer, ok := arg.(*ast.Call)
	if !ok {
		t.Fatalf("argument is %T, want *ast.Call", arg)
	}
	if len(outer.Args) != 1 {
		t.Fatalf("g has %d arguments, want 1", len(outer.Args))
	}
	if _, ok := outer.Args[0].(*ast.Call); !ok {
		t.Errorf("innermost argument is %T, want *ast.Call", outer.Args[0])
	}
}

func TestParsesCallMixedWithOtherArguments(t *testing.T) {
	args := callArgs(t, "package main\n\nfunc main() {\n    f(1, g(), \"hi\")\n}\n")

	if len(args) != 3 {
		t.Fatalf("call has %d arguments, want 3", len(args))
	}
	if _, ok := args[1].(*ast.Call); !ok {
		t.Errorf("argument 1 is %T, want *ast.Call", args[1])
	}
}

// binOp returns the sole argument of main's sole call as a binary expression.
func binArg(t *testing.T, expr string) *ast.Binary {
	t.Helper()

	arg := firstArg(t, "package main\n\nfunc main() {\n    exit("+expr+")\n}\n")
	bin, ok := arg.(*ast.Binary)
	if !ok {
		t.Fatalf("argument is %T, want *ast.Binary", arg)
	}
	return bin
}

func TestParsesBinaryOperators(t *testing.T) {
	tests := []struct {
		expr string
		want ast.BinaryOp
	}{
		{"1 + 2", ast.Add},
		{"1 - 2", ast.Sub},
		{"1 * 2", ast.Mul},
		{"1 / 2", ast.Div},
		{"1 % 2", ast.Rem},
		{"1 == 2", ast.Eq},
		{"1 != 2", ast.Ne},
		{"1 < 2", ast.Lt},
		{"1 <= 2", ast.Le},
		{"1 > 2", ast.Gt},
		{"1 >= 2", ast.Ge},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			if got := binArg(t, tt.expr).Op; got != tt.want {
				t.Errorf("Op = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMultiplicationBindsTighterThanAddition(t *testing.T) {
	bin := binArg(t, "1 + 2 * 3")

	if bin.Op != ast.Add {
		t.Fatalf("root operator = %v, want Add", bin.Op)
	}
	right, ok := bin.Y.(*ast.Binary)
	if !ok || right.Op != ast.Mul {
		t.Errorf("right operand = %#v, want a Mul", bin.Y)
	}
}

func TestComparisonBindsLooserThanArithmetic(t *testing.T) {
	bin := binArg(t, "1 + 2 < 4")

	if bin.Op != ast.Lt {
		t.Fatalf("root operator = %v, want Lt", bin.Op)
	}
	if left, ok := bin.X.(*ast.Binary); !ok || left.Op != ast.Add {
		t.Errorf("left operand = %#v, want an Add", bin.X)
	}
}

func TestSubtractionIsLeftAssociative(t *testing.T) {
	bin := binArg(t, "1 - 2 - 3")

	if bin.Op != ast.Sub {
		t.Fatalf("root operator = %v, want Sub", bin.Op)
	}
	left, ok := bin.X.(*ast.Binary)
	if !ok || left.Op != ast.Sub {
		t.Fatalf("left operand = %#v, want a Sub, so that 1-2-3 means (1-2)-3", bin.X)
	}
	if lit, ok := bin.Y.(*ast.IntLit); !ok || lit.Value != 3 {
		t.Errorf("right operand = %#v, want the literal 3", bin.Y)
	}
}

func TestParenthesesOverridePrecedence(t *testing.T) {
	bin := binArg(t, "(1 + 2) * 3")

	if bin.Op != ast.Mul {
		t.Fatalf("root operator = %v, want Mul", bin.Op)
	}
	if left, ok := bin.X.(*ast.Binary); !ok || left.Op != ast.Add {
		t.Errorf("left operand = %#v, want the parenthesised Add", bin.X)
	}
}

func TestParsesCallsWithinArithmetic(t *testing.T) {
	src := "package main\n\nfunc fib(n int) int {\n    return fib(n - 1) + fib(n - 2)\n}\n\nfunc main() {\n}\n"

	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags)
	}

	ret := file.Decls[0].(*ast.Func).Body[0].(*ast.Return)
	bin, ok := ret.Value.(*ast.Binary)
	if !ok {
		t.Fatalf("returned expression is %T, want *ast.Binary", ret.Value)
	}
	if bin.Op != ast.Add {
		t.Errorf("root operator = %v, want Add", bin.Op)
	}
	if _, ok := bin.X.(*ast.Call); !ok {
		t.Errorf("left operand is %T, want *ast.Call", bin.X)
	}
	if _, ok := bin.Y.(*ast.Call); !ok {
		t.Errorf("right operand is %T, want *ast.Call", bin.Y)
	}
}

func ifStmt(t *testing.T, body string) *ast.If {
	t.Helper()

	file, diags := parse(t, "package main\n\nfunc main() {\n"+body+"\n}\n")
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags)
	}

	stmt := file.Decls[0].(*ast.Func).Body[0]
	got, ok := stmt.(*ast.If)
	if !ok {
		t.Fatalf("statement is %T, want *ast.If", stmt)
	}
	return got
}

func TestParsesIfWithoutElse(t *testing.T) {
	got := ifStmt(t, "    if 1 < 2 {\n        print(\"yes\")\n    }")

	if _, ok := got.Cond.(*ast.Binary); !ok {
		t.Errorf("condition is %T, want *ast.Binary", got.Cond)
	}
	if len(got.Then) != 1 {
		t.Errorf("then block has %d statements, want 1", len(got.Then))
	}
	if got.Else != nil {
		t.Errorf("Else = %#v, want nil", got.Else)
	}
}

func TestParsesIfElse(t *testing.T) {
	got := ifStmt(
		t,
		"    if 1 < 2 {\n        print(\"yes\")\n    } else {\n        print(\"no\")\n    }",
	)

	if len(got.Then) != 1 {
		t.Errorf("then block has %d statements, want 1", len(got.Then))
	}
	if len(got.Else) != 1 {
		t.Errorf("else block has %d statements, want 1", len(got.Else))
	}
}

// An else-if chain is an if whose else block holds one more if.
func TestParsesElseIfChain(t *testing.T) {
	got := ifStmt(
		t,
		"    if 1 < 2 {\n        print(\"a\")\n    } else if 2 < 3 {\n        print(\"b\")\n    } else {\n        print(\"c\")\n    }",
	)

	if len(got.Else) != 1 {
		t.Fatalf("else block has %d statements, want 1", len(got.Else))
	}
	nested, ok := got.Else[0].(*ast.If)
	if !ok {
		t.Fatalf("else statement is %T, want *ast.If", got.Else[0])
	}
	if len(nested.Else) != 1 {
		t.Errorf("the chained else has %d statements, want 1", len(nested.Else))
	}
}

func TestParsesReturnInsideAnIf(t *testing.T) {
	src := "package main\n\nfunc f(n int) int {\n    if n < 2 {\n        return n\n    }\n    return 0\n}\n\nfunc main() {\n}\n"

	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags)
	}

	body := file.Decls[0].(*ast.Func).Body
	if len(body) != 2 {
		t.Fatalf("body has %d statements, want 2", len(body))
	}
	ifs, ok := body[0].(*ast.If)
	if !ok {
		t.Fatalf("first statement is %T, want *ast.If", body[0])
	}
	if _, ok := ifs.Then[0].(*ast.Return); !ok {
		t.Errorf("then block holds %T, want *ast.Return", ifs.Then[0])
	}
}

// A variadic marker used to be consumed and discarded, so f(x ...int) and
// f(x int) produced identical trees and nothing downstream could tell that
// something had been asked for and silently not delivered.
func TestRejectsVariadicParameter(t *testing.T) {
	_, diags := parse(t, "package main\n\nfunc f(x ...int) {\n}\n\nfunc main() {\n}\n")

	if len(diags) == 0 {
		t.Fatal("a variadic parameter parsed without a diagnostic")
	}
	if !strings.Contains(diags[0].Message, "variadic") {
		t.Errorf("message = %q, want it to name the unsupported feature", diags[0].Message)
	}
}

func TestRejectsVariadicResultType(t *testing.T) {
	_, diags := parse(t, "package main\n\nfunc f() ...int {\n    return 1\n}\n\nfunc main() {\n}\n")

	if len(diags) == 0 {
		t.Fatal("a variadic result type parsed without a diagnostic")
	}
}
