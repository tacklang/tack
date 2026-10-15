package parser_test

import (
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
			if lit.Value.Int64() != tt.want {
				t.Errorf("Value = %s, want %d", lit.Value, tt.want)
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
// that can notice a literal too large to represent: one past the largest
// uint64, the largest value any literal can have.
func TestRejectsIntegerLiteralOutOfRange(t *testing.T) {
	for _, lit := range []string{"18446744073709551616", "99999999999999999999999"} {
		_, diags := parse(t, "package main\n\nfunc main() {\n    exit("+lit+")\n}\n")
		want := "integer literal " + lit + " is out of range"
		if len(diags) != 1 || diags[0].Message != want {
			t.Errorf("%s: got %+v, want %q", lit, diags, want)
		}
	}
}

// Every literal from the smallest int64 to the largest uint64 parses, to its
// exact value; which type it can be is the type checker's to judge.
func TestParsesIntegerLiteralsAcrossTheRange(t *testing.T) {
	for _, lit := range []string{"0", "9223372036854775807", "9223372036854775808", "18446744073709551615", "-9223372036854775808"} {
		arg := firstArg(t, "package main\n\nfunc main() {\n    exit("+lit+")\n}\n")
		if got := arg.(*ast.IntLit).Value.String(); got != lit {
			t.Errorf("%s parsed as %s", lit, got)
		}
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
	if lit, ok := args[0].(*ast.IntLit); !ok || lit.Value.Int64() != 1 {
		t.Errorf("argument 0 = %#v, want IntLit 1", args[0])
	}
	if lit, ok := args[1].(*ast.StrLit); !ok || lit.Value != "hi" {
		t.Errorf("argument 1 = %#v, want StrLit \"hi\"", args[1])
	}
	if lit, ok := args[2].(*ast.IntLit); !ok || lit.Value.Int64() != 2 {
		t.Errorf("argument 2 = %#v, want IntLit 2", args[2])
	}
}

// An identifier argument is how a parameter gets used, so it has to reach the
// AST as an Ident for the resolver to bind.
func TestParsesIdentifierArgument(t *testing.T) {
	src := "package main\n\nfunc f(x int64) {\n    exit(x)\n}\n\nfunc main() {\n}\n"

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
	file, diags := parse(t, "package main\n\nfunc f(x int64, s string) {\n}\n\nfunc main() {\n}\n")
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags)
	}

	fn := file.Decls[0].(*ast.Func)
	if len(fn.Params) != 2 {
		t.Fatalf("f has %d parameters, want 2", len(fn.Params))
	}

	for i, want := range []struct{ name, typ string }{{"x", "int64"}, {"s", "string"}} {
		if fn.Params[i].Name != want.name {
			t.Errorf("parameter %d name = %q, want %q", i, fn.Params[i].Name, want.name)
		}
		typ, ok := fn.Params[i].TypeExpr.(*ast.Ident)
		if !ok {
			t.Fatalf("parameter %d type is %T, want *ast.Ident", i, fn.Params[i].TypeExpr)
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
	file, diags := parse(
		t,
		"package main\n\nfunc f() int64 {\n    return 1\n}\n\nfunc main() {\n}\n",
	)
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags)
	}

	fn := file.Decls[0].(*ast.Func)
	if len(fn.Results) != 1 {
		t.Fatalf("got %d result types, want 1", len(fn.Results))
	}
	id, ok := fn.Results[0].(*ast.Ident)
	if !ok {
		t.Fatalf("result type is %T, want *ast.Ident", fn.Results[0])
	}
	if id.Name != "int64" {
		t.Errorf("result type = %q, want %q", id.Name, "int64")
	}
}

func TestFunctionWithoutResultTypeHasNone(t *testing.T) {
	file, diags := parse(t, "package main\n\nfunc main() {\n}\n")
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags)
	}

	if fn := file.Decls[0].(*ast.Func); len(fn.Results) != 0 {
		t.Errorf("Results = %#v, want none", fn.Results)
	}
}

func TestParsesReturnWithAValue(t *testing.T) {
	file, diags := parse(
		t,
		"package main\n\nfunc f() int64 {\n    return 42\n}\n\nfunc main() {\n}\n",
	)
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags)
	}

	stmt := file.Decls[0].(*ast.Func).Body[0]
	ret, ok := stmt.(*ast.Return)
	if !ok {
		t.Fatalf("statement is %T, want *ast.Return", stmt)
	}
	lit, ok := ret.Values[0].(*ast.IntLit)
	if !ok {
		t.Fatalf("returned expression is %T, want *ast.IntLit", ret.Values[0])
	}
	if lit.Value.Int64() != 42 {
		t.Errorf("returned %s, want 42", lit.Value)
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
	if len(ret.Values) != 0 {
		t.Errorf("Values = %#v, want none for a bare return", ret.Values)
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
	if lit, ok := bin.Y.(*ast.IntLit); !ok || lit.Value.Int64() != 3 {
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
	src := "package main\n\nfunc fib(n int64) int64 {\n    return fib(n - 1) + fib(n - 2)\n}\n\nfunc main() {\n}\n"

	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %d diagnostic(s): %+v", len(diags), diags)
	}

	ret := file.Decls[0].(*ast.Func).Body[0].(*ast.Return)
	bin, ok := ret.Values[0].(*ast.Binary)
	if !ok {
		t.Fatalf("returned expression is %T, want *ast.Binary", ret.Values[0])
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
	src := "package main\n\nfunc f(n int64) int64 {\n    if n < 2 {\n        return n\n    }\n    return 0\n}\n\nfunc main() {\n}\n"

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

// TestErrorsInsideNestedConstructsAreNotLost checks that a syntax error
// inside a block, a condition, or a call argument is the one reported, and
// that the parser never hands back a tree with a hole where the bad part was.
// Before this was pinned, an error inside a block was dropped and a later,
// misleading one reported instead, or none at all.
func TestErrorsInsideNestedConstructsAreNotLost(t *testing.T) {
	tests := []struct {
		body string
		want string
	}{
		{"if 1 < 2 {\n        f(0}\n    }", "expected ',' found '}'"},
		{"if 1 < 2 {\n        return 1 1\n    }", "expected ';' found '1'"},
		{"if 1 < 2 {\n        if\n    }", "expected 'value' found '}'"},
		{"if 1 < 2 {\n    } else {\n        f(0}\n    }", "expected ',' found '}'"},
		{"if 1 < 2 {\n    } else if 2 < 3 {\n        f(0}\n    }", "expected ',' found '}'"},
		{"if 1 < 2 {\n        x := f(0}\n    }", "expected ',' found '}'"},
		{"if f(0 {\n    }", "expected ',' found '{'"},
		{"exit(f(0)", "expected ',' found ';'"},
		{"exit(1 + f(0)", "expected ',' found ';'"},
	}

	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			file, diags := parse(t, "package main\n\nfunc main() {\n    "+tt.body+"\n}\n")
			if len(diags) == 0 {
				t.Fatalf("no diagnostics; tree:\n%s", file)
			}
			if diags[0].Message != tt.want {
				t.Errorf("first diagnostic = %q, want %q", diags[0].Message, tt.want)
			}
			if file != nil {
				t.Errorf("got a tree alongside %d diagnostic(s)", len(diags))
			}
		})
	}
}
