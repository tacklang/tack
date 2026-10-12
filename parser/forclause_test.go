package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// clauseKind names the statement in a loop clause, or "" for none.
func clauseKind(s ast.Stmt) string {
	switch s.(type) {
	case nil:
		return ""
	case *ast.Var:
		return "var"
	case *ast.Assign:
		return "assign"
	case *ast.IncDec:
		return "incdec"
	case *ast.ExprStmt:
		return "call"
	default:
		return "other"
	}
}

// TestParsesThreeClauseLoops checks each part of for init; cond; post is
// recorded, and that each part may be left out.
func TestParsesThreeClauseLoops(t *testing.T) {
	tests := []struct {
		header string
		init   string
		cond   string
		post   string
	}{
		{"mut i := 0; i < 3; i++", "var", "(i < 3)", "incdec"},
		{"mut i = 0; i < 3; i += 1", "var", "(i < 3)", "assign"},
		{"var i int64 = 0; i < 3; i = i + 1", "var", "(i < 3)", "assign"},
		{"i := 0; i < 3; f()", "var", "(i < 3)", "call"},
		{"i = 0; i < 3; i--", "assign", "(i < 3)", "incdec"},
		{"f(); g(); h()", "call", "g()", "call"},
		{"; i < 3; i++", "", "(i < 3)", "incdec"},
		{"mut i := 0; ; i++", "var", "", "incdec"},
		{"mut i := 0; i < 3;", "var", "(i < 3)", ""},
		{";;", "", "", ""},
		{"; ;", "", "", ""},
		{"i++; ; ", "incdec", "", ""},
		{"; f() && !done; ", "", "(f() && (!done))", ""},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			body := mainBody(t, "    for "+tt.header+" {\n        g()\n    }")
			loop, ok := body[0].(*ast.For)
			if !ok {
				t.Fatalf("statement is %T, want *ast.For", body[0])
			}
			if got := clauseKind(loop.Init); got != tt.init {
				t.Errorf("init = %q, want %q", got, tt.init)
			}
			cond := ""
			if loop.Cond != nil {
				cond = shape(t, loop.Cond)
			}
			if cond != tt.cond {
				t.Errorf("cond = %q, want %q", cond, tt.cond)
			}
			if got := clauseKind(loop.Post); got != tt.post {
				t.Errorf("post = %q, want %q", got, tt.post)
			}
			if len(loop.Body) != 1 {
				t.Errorf("body has %d statements, want 1", len(loop.Body))
			}
		})
	}
}

// The plain forms keep working, and record no init or post.
func TestPlainLoopsHaveNoClauses(t *testing.T) {
	for _, src := range []string{"for {\n    }", "for a {\n    }", "for f() {\n    }"} {
		loop := mainBody(t, "    "+src)[0].(*ast.For)
		if loop.Init != nil || loop.Post != nil {
			t.Errorf("%q: init %T, post %T; want neither", src, loop.Init, loop.Post)
		}
	}
}

// Spans of the clauses cover only their own text.
func TestThreeClauseSpans(t *testing.T) {
	src := "package main\n\nfunc main() {\n    for mut i := 0; i < 3; i += 2 {\n    }\n}\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	loop := file.Decls[0].(*ast.Func).Body[0].(*ast.For)
	if got := src[loop.Init.Spanned().Start:loop.Init.Spanned().End]; got != "mut i := 0" {
		t.Errorf("init spans %q", got)
	}
	if got := src[loop.Post.Spanned().Start:loop.Post.Spanned().End]; got != "i += 2" {
		t.Errorf("post spans %q", got)
	}
}

// TestRejectsMalformedThreeClauseLoops checks malformed headers are parse
// errors, a declaration in the post statement among them.
func TestRejectsMalformedThreeClauseLoops(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{
			"for mut i := 0; i < 3; j := 1 {\n    }",
			"cannot declare a variable in a for loop's post statement",
		},
		{"for ;; var j = 1 {\n    }", "cannot declare a variable in a for loop's post statement"},
		{"for ;; mut j int64 {\n    }", "cannot declare a variable in a for loop's post statement"},
		{"for mut i := 0 {\n    }", "expected ';' found '{'"},
		{"for mut i := 0; i < 3 {\n    }", "expected ';' found '{'"},
		{"for mut i := 0; i < 3; i++; {\n    }", "expected '{' found ';'"},
		{"for i < 3; i++ {\n    }", "expected '{' found ';'"},
		{"for ; ; ; {\n    }", "expected 'statement' found ';'"},
		{"for ;; i++ ", "expected '{' found ';'"},
		{"for ;; break {\n    }", "expected 'statement' found 'break'"},
		{"for return; ; {\n    }", "expected 'value' found 'return'"},
		{"for if a {} ; ; {\n    }", "expected 'value' found 'if'"},
		{"for ; i := 0; {\n    }", "expected ';' found ':='"},
		{"for x; ; {\n    }", "expected '{' found ';'"},
		{"for 1 + 2; ; {\n    }", "expected '{' found ';'"},
	}

	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			_, diags := parse(t, "package main\n\nfunc main() {\n    "+tt.src+"\n}\n")
			if len(diags) == 0 {
				t.Fatalf("%q parsed without error", tt.src)
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
		})
	}
}

// The post-statement declaration error points at the declaration.
func TestPostDeclarationErrorSpan(t *testing.T) {
	src := "package main\n\nfunc main() {\n    for ;; j := 1 {\n    }\n}\n"
	_, diags := parse(t, src)
	if len(diags) != 1 {
		t.Fatalf("got %+v, want one diagnostic", diags)
	}
	if got := src[diags[0].Span.Start:diags[0].Span.End]; got != "j := 1" {
		t.Errorf("diagnostic covers %q, want %q", got, "j := 1")
	}
}
