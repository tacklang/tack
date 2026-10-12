package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestParsesIncDec checks x++ and x-- become IncDec statements recording
// their target and direction, with spans covering the whole statement.
func TestParsesIncDec(t *testing.T) {
	src := "package main\n\nfunc main() {\n    count++\n    count--\n    for { n++ }\n}\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}

	body := file.Decls[0].(*ast.Func).Body
	for i, wantDec := range []bool{false, true} {
		s, ok := body[i].(*ast.IncDec)
		if !ok {
			t.Fatalf("statement %d is %T, want *ast.IncDec", i, body[i])
		}
		if s.Target.(*ast.Ident).Name != "count" || s.Dec != wantDec {
			t.Errorf(
				"statement %d = %s, dec %v; want count, dec %v",
				i,
				s.Target.(*ast.Ident).Name,
				s.Dec,
				wantDec,
			)
		}
		want := "count++"
		if wantDec {
			want = "count--"
		}
		if got := src[s.Span.Start:s.Span.End]; got != want {
			t.Errorf("statement %d spans %q, want %q", i, got, want)
		}
	}
	if _, ok := body[2].(*ast.For).Body[0].(*ast.IncDec); !ok {
		t.Errorf("loop body holds %T, want *ast.IncDec", body[2].(*ast.For).Body[0])
	}
}

// TestRejectsMalformedIncDec checks ++ and -- are postfix statements only:
// never values, never prefix, and only on a name.
func TestRejectsMalformedIncDec(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{"++x", "expected 'statement' found '++'"},
		{"--x", "expected 'statement' found '--'"},
		{"y := x++", "expected ';' found '++'"},
		{"f(x++)", "expected ',' found '++'"},
		{"x++ + 1", "expected ';' found '+'"},
		{"x++++", "expected ';' found '++'"},
		{"x++ x", "expected ';' found 'x'"},
		{"1++", "expected 'statement' found '1'"},
		{"f()++", "expected ';' found '++'"},
		{"true++", "expected 'statement' found 'true'"},
		{"if x++ {\n    }", "expected '{' found '++'"},
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
