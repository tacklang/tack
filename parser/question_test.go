package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// questionShape renders e with each ?, field, index and call it holds
// written out, and every binary operator parenthesized, so a test can see
// what a ? applies to.
func questionShape(t *testing.T, e ast.Expr) string {
	t.Helper()
	switch x := e.(type) {
	case *ast.Question:
		return "[" + questionShape(t, x.X) + "]?"
	case *ast.Selector:
		return questionShape(t, x.X) + "." + x.Name
	case *ast.Index:
		return questionShape(t, x.X) + "[" + questionShape(t, x.Index) + "]"
	case *ast.Call:
		args := make([]string, len(x.Args))
		for i, a := range x.Args {
			args[i] = questionShape(t, a)
		}
		return questionShape(t, x.Fn) + "(" + strings.Join(args, ", ") + ")"
	case *ast.MutArg:
		return "mut " + questionShape(t, x.X)
	case *ast.Binary:
		return "(" + questionShape(t, x.X) + " " + x.Op.String() + " " + questionShape(t, x.Y) + ")"
	case *ast.Unary:
		return "(" + x.Op.String() + questionShape(t, x.X) + ")"
	case *ast.Ident:
		return x.Name
	case *ast.IntLit:
		return x.Value.String()
	default:
		t.Fatalf("unexpected expression %T", e)
		return ""
	}
}

// TestQuestionBinding pins where ? binds: as tightly as a field, an index
// or a call after it, so each applies to what the ? gives, and tighter
// than every prefix and binary operator. A ? written with a space before
// it, or after parentheses, is the same ?, and two in a row each apply to
// what is before them, which the type checker judges.
func TestQuestionBinding(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"f()?", "[f()]?"},
		{"f() ?", "[f()]?"},
		{"(f())?", "[f()]?"},
		{"f()?.b", "[f()]?.b"},
		{"f()?[2]", "[f()]?[2]"},
		{"f()?.get()?", "[[f()]?.get()]?"},
		{"f(g()?, h()?)?", "[f([g()]?, [h()]?)]?"},
		{"a + f()? * b", "(a + ([f()]? * b))"},
		{"-f()?", "(-[f()]?)"},
		{"!f()? && g()?", "((![f()]?) && [g()]?)"},
		{"x?", "[x]?"},
		{"f()??", "[[f()]?]?"},
		{"x.m(1)?", "[x.m(1)]?"},
		{"a[0].m()?.n", "[a[0].m()]?.n"},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			if got := questionShape(t, declared(t, tt.expr)); got != tt.want {
				t.Errorf("%s parsed as %s, want %s", tt.expr, got, tt.want)
			}
		})
	}
}

// TestQuestionSpan checks a ? spans from the start of what it follows,
// its parentheses included, through the ?, and Mark is the ? alone.
func TestQuestionSpan(t *testing.T) {
	tests := []struct {
		expr, span string
	}{
		{"f(1)?", "f(1)?"},
		{"(f(1)) ?", "(f(1)) ?"},
		{"x.m()?", "x.m()?"},
	}
	for _, tt := range tests {
		src := "package main\n\nfunc main() {\n    x := " + tt.expr + "\n}\n"
		file, diags := parse(t, src)
		if len(diags) > 0 {
			t.Fatalf("%s: parse reported %+v", tt.expr, diags)
		}
		q, ok := file.Decls[0].(*ast.Func).Body[0].(*ast.Var).Value.(*ast.Question)
		if !ok {
			t.Fatalf("%s did not parse to a ?", tt.expr)
		}
		if got := src[q.Span.Start:q.Span.End]; got != tt.span {
			t.Errorf("%s spans %q, want %q", tt.expr, got, tt.span)
		}
		if got := src[q.Mark().Start:q.Mark().End]; got != "?" {
			t.Errorf("%s marks %q, want the ?", tt.expr, got)
		}
	}
}

// TestQuestionStatements checks the statements a ? makes: a call's, a
// method call's, a mut receiver's, which stays a call of a mut receiver,
// a ? on a method of what a ? gives, a ? on a name, which the type
// checker reports, and the bindings of what a ? gives.
func TestQuestionStatements(t *testing.T) {
	tests := []struct {
		stmt string
		want string
	}{
		{"f(1)?", "[f(1)]?"},
		{"f() ?", "[f()]?"},
		{"x.m()?", "[x.m()]?"},
		{"mut c.bump(1)?", "[mut c.bump(1)]?"},
		{"mut c.a.bump(1)??", "[[mut c.a.bump(1)]?]?"},
		{"f()?.get()?", "[[f()]?.get()]?"},
		{"a[0].m()?", "[a[0].m()]?"},
		{"err?", "[err]?"},
		{"a.b?", "[a.b]?"},
	}
	for _, tt := range tests {
		t.Run(tt.stmt, func(t *testing.T) {
			body := mainBody(t, "    "+tt.stmt)
			s, ok := body[0].(*ast.ExprStmt)
			if !ok {
				t.Fatalf("%s parsed as %T, want an expression statement", tt.stmt, body[0])
			}
			if got := questionShape(t, s.X); got != tt.want {
				t.Errorf("%s parsed as %s, want %s", tt.stmt, got, tt.want)
			}
		})
	}
	for _, src := range []string{"q, r := divide(7, 2)?", "q, r = divide(7, 2)?"} {
		body := mainBody(t, "    "+src)
		var value ast.Expr
		switch s := body[0].(type) {
		case *ast.MultiVar:
			value = s.Value
		case *ast.MultiAssign:
			value = s.Value
		}
		if _, ok := value.(*ast.Question); !ok {
			t.Errorf("%s binds %T, want a ?", src, value)
		}
	}
}

// TestQuestionMutReceiverSpans checks that mut before a method call with
// ?s after it makes the receiver the mut argument, and each ? spans from
// mut.
func TestQuestionMutReceiverSpans(t *testing.T) {
	src := "package main\n\nfunc main() {\n    mut c.bump(1)??\n}\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	outer := file.Decls[0].(*ast.Func).Body[0].(*ast.ExprStmt).X.(*ast.Question)
	inner := outer.X.(*ast.Question)
	for _, q := range []*ast.Question{outer, inner} {
		if got := src[q.Span.Start : q.Span.Start+3]; got != "mut" {
			t.Errorf("a ? spans from %q, want mut", src[q.Span.Start:q.Span.End])
		}
	}
	call := inner.X.(*ast.Call)
	if _, ok := call.Fn.(*ast.Selector).X.(*ast.MutArg); !ok {
		t.Error("mut c.bump(1)?? does not pass c as a mut receiver")
	}
}

// TestQuestionMalformed checks the ? that follows nothing, and what
// follows a ? that is not a statement, are each reported where they are.
func TestQuestionMalformed(t *testing.T) {
	tests := []struct {
		body, want string
	}{
		{"    ?", "expected 'statement' found '?'"},
		{"    ?f()", "expected 'statement' found '?'"},
		{"    f()\n    ?", "expected 'statement' found '?'"},
		{"    f()?.x", "expected 'statement' found 'f'"},
		{"    x := ?", "expected 'value' found '?'"},
	}
	for _, tt := range tests {
		src := "package main\n\nfunc main() {\n" + tt.body + "\n}\n"
		_, diags := parse(t, src)
		if len(diags) == 0 || diags[0].Message != tt.want {
			t.Errorf("%q reported %+v, want %q", tt.body, diags, tt.want)
		}
	}
}

// TestParenthesizedCallStatements checks a statement may start with a
// parenthesis when it is a call or a ? on one, spanning from the
// parenthesis, and that a loop takes one as its init and post statements,
// a ? on a call as its init too.
func TestParenthesizedCallStatements(t *testing.T) {
	tests := []struct {
		stmt, want string
	}{
		{"(f())", "f()"},
		{"(check(n))?", "[check(n)]?"},
		{"(check(n)?)", "[check(n)]?"},
		{"((check(n)))??", "[[check(n)]?]?"},
		{"(x.m())?", "[x.m()]?"},
	}
	for _, tt := range tests {
		t.Run(tt.stmt, func(t *testing.T) {
			src := "package main\n\nfunc main() {\n    " + tt.stmt + "\n}\n"
			file, diags := parse(t, src)
			if len(diags) > 0 {
				t.Fatalf("parse reported %+v", diags)
			}
			s, ok := file.Decls[0].(*ast.Func).Body[0].(*ast.ExprStmt)
			if !ok {
				t.Fatalf("%s parsed as %T, want an expression statement", tt.stmt,
					file.Decls[0].(*ast.Func).Body[0])
			}
			if got := questionShape(t, s.X); got != tt.want {
				t.Errorf("%s parsed as %s, want %s", tt.stmt, got, tt.want)
			}
			if got := src[s.Span.Start:s.Span.End]; got != tt.stmt {
				t.Errorf("%s spans %q", tt.stmt, got)
			}
		})
	}
	for _, header := range []string{
		"check(n)?; i < 3; check(i)?",
		"(check(n))?; i < 3; (check(i))?",
		"(f()); i < 3; (f())",
		"check(n)?; ; check(i)?",
	} {
		body := mainBody(t, "    for "+header+" {\n    }")
		loop, ok := body[0].(*ast.For)
		if !ok {
			t.Fatalf("for %s parsed as %T", header, body[0])
		}
		if _, ok := loop.Init.(*ast.ExprStmt); !ok {
			t.Errorf("for %s has init %T, want a call statement", header, loop.Init)
		}
		if _, ok := loop.Post.(*ast.ExprStmt); !ok {
			t.Errorf("for %s has post %T, want a call statement", header, loop.Post)
		}
	}
}

// TestParenthesizedNonCallStatements checks a statement starting with a
// parenthesis that is no call, nor a ? on one, as a statement, a loop's
// init or its post, gives one diagnostic, at the parenthesis, and that
// the statement after it is parsed as it would be. A malformed one is
// reported so too, but as a loop's init, where a malformed expression is
// read as the loop's condition and reported once where it goes wrong, as
// one not in parentheses is.
func TestParenthesizedNonCallStatements(t *testing.T) {
	lines := []string{"(a +", "(", "for x := 0; x < 3; (a + {\n    }"}
	for _, stmt := range []string{"(x)", "(a + b)", "(x)?", "(f()).x", "(f())?.y"} {
		lines = append(lines, stmt,
			"for "+stmt+"; x < 3; x++ {\n    }",
			"for x := 0; x < 3; "+stmt+" {\n    }")
	}
	for _, line := range lines {
		src := "package main\n\nfunc main() {\n    " + line + "\n    f()\n}\n"
		_, diags := parse(t, src)
		at := strings.Index(src, "(") // past main's own
		at = strings.Index(src[at+1:], "(") + at + 1
		if strings.HasPrefix(line, "for x") {
			at = strings.LastIndex(src, "; ") + 2
		}
		if len(diags) != 1 || diags[0].Message != "expected 'statement' found '('" ||
			diags[0].Span.Start != at {
			t.Errorf("%q reported %+v, want only expected 'statement' found '(' at %d",
				line, diags, at)
		}
	}
	for _, init := range []string{"(a +", "(", "a +"} {
		src := "package main\n\nfunc main() {\n    for " + init + "; x < 3; x++ {\n    }\n    f()\n}\n"
		_, diags := parse(t, src)
		if len(diags) != 1 || diags[0].Message != "expected 'value' found ';'" {
			t.Errorf("for %s; reported %+v, want only expected 'value' found ';'", init, diags)
		}
	}
}
