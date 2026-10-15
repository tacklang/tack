package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// resultNames parses a file whose first declaration is a function and
// returns the names of its result types, in order.
func resultNames(t *testing.T, src string) []string {
	t.Helper()

	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	results := file.Decls[0].(*ast.Func).Results
	names := make([]string, 0, len(results))
	for _, r := range results {
		names = append(names, r.(*ast.Ident).Name)
	}
	return names
}

// TestParsesResultLists checks each way of writing a function's results:
// none, one bare type, or a parenthesized list.
func TestParsesResultLists(t *testing.T) {
	tests := []struct {
		sig  string
		want []string
	}{
		{"func f() {", nil},
		{"func f() int64 {", []string{"int64"}},
		{"func f() (int64) {", []string{"int64"}},
		{"func f() (int64, string) {", []string{"int64", "string"}},
		{"func f() (int64, string, bool, int64) {", []string{"int64", "string", "bool", "int64"}},
		{"func f(a int64, b int64) (int64, int64) {", []string{"int64", "int64"}},
		{"func f() ( int64 ,string ) {", []string{"int64", "string"}},
	}

	for _, tt := range tests {
		t.Run(tt.sig, func(t *testing.T) {
			got := resultNames(t, "package main\n\n"+tt.sig+"\n}\n")
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("results = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRejectsMalformedResultLists checks every malformed list is a parse
// error, including named results and a list without its parentheses.
func TestRejectsMalformedResultLists(t *testing.T) {
	tests := []struct {
		sig  string
		want string
	}{
		{"func f() () {", "expected 'type name' found ')'"},
		{"func f() (int64,) {", "expected 'type name' found ')'"},
		{"func f() (, int64) {", "expected 'type name' found ','"},
		{"func f() (int64 string) {", "expected ')' found 'string'"},
		{"func f() (q int64, r int64) {", "expected ')' found 'int64'"},
		{"func f() (int64, int64 {", "expected ')' found '{'"},
		{"func f() int64, int64 {", "missing function body"},
		{"func f() (int64, 1) {", "expected 'type name' found '1'"},
		{"func f() ((int64, int64)) {", "expected 'type name' found '('"},
		{"func f() (...int64) {", "can only use ... with the final parameter"},
	}

	for _, tt := range tests {
		t.Run(tt.sig, func(t *testing.T) {
			_, diags := parse(t, "package main\n\n"+tt.sig+"\n}\n")
			if len(diags) == 0 {
				t.Fatalf("%q parsed without error", tt.sig)
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
		})
	}
}

// TestParsesReturnValueLists checks a return holds its values in order.
func TestParsesReturnValueLists(t *testing.T) {
	tests := []struct {
		stmt string
		want []string
	}{
		{"return", nil},
		{"return a", []string{"a"}},
		{"return a, b", []string{"a", "b"}},
		{"return a + 1, f(), !b", []string{"(a + 1)", "f()", "(!b)"}},
		{"return a,\n        b", []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.stmt, func(t *testing.T) {
			ret, ok := mainBody(t, "    "+tt.stmt)[0].(*ast.Return)
			if !ok {
				t.Fatal("statement is not a return")
			}
			var got []string
			for _, v := range ret.Values {
				got = append(got, shape(t, v))
			}
			if strings.Join(got, ";") != strings.Join(tt.want, ";") {
				t.Errorf("values = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRejectsMalformedReturnLists checks a missing value around a comma is a
// parse error.
func TestRejectsMalformedReturnLists(t *testing.T) {
	for _, stmt := range []string{"return a,", "return , a", "return a,, b", "return a b"} {
		t.Run(stmt, func(t *testing.T) {
			_, diags := parse(t, "package main\n\nfunc main() {\n    "+stmt+"\n}\n")
			if len(diags) == 0 {
				t.Fatalf("%q parsed without error", stmt)
			}
		})
	}
}

// multiVar parses a statement inside main and returns it as a MultiVar.
func multiVar(t *testing.T, stmt string) *ast.MultiVar {
	t.Helper()

	m, ok := mainBody(t, "    "+stmt)[0].(*ast.MultiVar)
	if !ok {
		t.Fatalf("%q did not parse to a MultiVar", stmt)
	}
	return m
}

// TestParsesMultiVar checks each name's mutability is its own: mut marks
// only the name it is written before.
func TestParsesMultiVar(t *testing.T) {
	tests := []struct {
		stmt string
		want string // each name, with a * after a mutable one
	}{
		{"a, b := f()", "a b"},
		{"a, mut b := f()", "a b*"},
		{"mut a, b := f()", "a* b"},
		{"mut a, mut b := f()", "a* b*"},
		{"a, b, mut c, d := f()", "a b c* d"},
		{"a,b:=f()", "a b"},
		{"a, b := g(1, x + 2)", "a b"},
	}

	for _, tt := range tests {
		t.Run(tt.stmt, func(t *testing.T) {
			m := multiVar(t, tt.stmt)
			var got []string
			for _, v := range m.Vars {
				name := v.Name
				if v.Mutable {
					name += "*"
				}
				if v.Value != nil || v.TypeExpr != nil {
					t.Errorf("%s has a value or type of its own", v.Name)
				}
				got = append(got, name)
			}
			if strings.Join(got, " ") != tt.want {
				t.Errorf("names = %q, want %q", strings.Join(got, " "), tt.want)
			}
			if _, ok := m.Value.(*ast.Call); !ok {
				t.Errorf("value is %T, want *ast.Call", m.Value)
			}
		})
	}
}

// A MultiVar spans from its first name, or the mut before it, through the
// end of its value, and each name's span includes its own mut.
func TestMultiVarSpans(t *testing.T) {
	src := "package main\n\nfunc main() {\n    mut a, mut b := f(1)\n}\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}

	m := file.Decls[0].(*ast.Func).Body[0].(*ast.MultiVar)
	if got := src[m.Span.Start:m.Span.End]; !strings.HasPrefix(got, "mut a, mut b := f(1)") {
		t.Errorf("statement span covers %q", got)
	}
	if got := src[m.Vars[1].Span.Start:m.Vars[1].Span.End]; got != "mut b" {
		t.Errorf("second name's span covers %q, want %q", got, "mut b")
	}
	if got := src[m.Vars[1].NameSpan.Start:m.Vars[1].NameSpan.End]; got != "b" {
		t.Errorf("second name's NameSpan covers %q, want %q", got, "b")
	}
}

// TestRejectsMalformedMultiVar checks the forms that are not part of the
// language: a var form, several values, assignment for now, and broken
// lists.
func TestRejectsMalformedMultiVar(t *testing.T) {
	tests := []struct {
		stmt string
		want string
	}{
		{"a, := f()", "expected 'variable name' found ':='"},
		{"a, 1 := f()", "expected 'variable name' found '1'"},
		{"a, b :=", "expected 'value' found '}'"},
		{"a, b := 1, 2", "expected ';' found ','"},
		{"var a, b = f()", "expected 'type or =' found ','"},
		{"a, mut := f()", "expected 'variable name' found ':='"},
		{"mut mut a, b := f()", "expected 'variable name' found 'mut'"},
		{"a, var b := f()", "expected 'variable name' found 'var'"},
		{"a, b int64 := f()", "expected ':= or =' found 'int64'"},
		{"a, b", "expected ':= or =' found ';'"},
		{"a, b := f(), g()", "expected ';' found ','"},
	}

	for _, tt := range tests {
		t.Run(tt.stmt, func(t *testing.T) {
			_, diags := parse(t, "package main\n\nfunc main() {\n    "+tt.stmt+"\n}\n")
			if len(diags) == 0 {
				t.Fatalf("%q parsed without error", tt.stmt)
			}
			if !strings.Contains(diags[0].Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", diags[0].Message, tt.want)
			}
		})
	}
}

// A MultiVar is a simple statement, so it can be a three-clause loop's init,
// but as a declaration it cannot be its post.
func TestMultiVarInLoopHeader(t *testing.T) {
	loop, ok := mainBody(t, "    for q, mut r := f(); r > 0; r-- {\n    }")[0].(*ast.For)
	if !ok {
		t.Fatal("statement is not a for loop")
	}
	if _, ok := loop.Init.(*ast.MultiVar); !ok {
		t.Errorf("init is %T, want *ast.MultiVar", loop.Init)
	}

	_, diags := parse(t, "package main\n\nfunc main() {\n    for ;; a, b := f() {\n    }\n}\n")
	if len(diags) == 0 ||
		diags[0].Message != "cannot declare a variable in a for loop's post statement" {
		t.Errorf("post declaration reported %+v", diags)
	}
}

// TestParsesMultiAssign checks = after a list of names makes an assignment
// to each of them, with no mut allowed.
func TestParsesMultiAssign(t *testing.T) {
	m, ok := mainBody(t, "    a, b, c = f(1)")[0].(*ast.MultiAssign)
	if !ok {
		t.Fatal("statement is not a MultiAssign")
	}
	names := make([]string, 0, len(m.Targets))
	for _, target := range m.Targets {
		names = append(names, target.(*ast.Ident).Name)
	}
	if strings.Join(names, " ") != "a b c" {
		t.Errorf("targets = %v, want [a b c]", names)
	}
	if _, ok := m.Value.(*ast.Call); !ok {
		t.Errorf("value is %T, want *ast.Call", m.Value)
	}

	loop := mainBody(t, "    for ;; a, b = f() {\n    }")[0].(*ast.For)
	if _, ok := loop.Post.(*ast.MultiAssign); !ok {
		t.Errorf("post is %T, want *ast.MultiAssign", loop.Post)
	}
}

// mut belongs to declarations; in an assignment it is an error covering the
// mut and its name.
func TestRejectsMutInMultiAssign(t *testing.T) {
	for _, stmt := range []string{"a, mut b = f()", "mut a, b = f()"} {
		t.Run(stmt, func(t *testing.T) {
			src := "package main\n\nfunc main() {\n    " + stmt + "\n}\n"
			_, diags := parse(t, src)
			if len(diags) != 1 || diags[0].Message != "cannot use mut in an assignment" {
				t.Fatalf("got %+v", diags)
			}
			if got := src[diags[0].Span.Start:diags[0].Span.End]; !strings.HasPrefix(got, "mut ") {
				t.Errorf("diagnostic covers %q, want the mut and its name", got)
			}
		})
	}
}
