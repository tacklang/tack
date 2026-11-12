package parser_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestParsesFieldsLoops checks for f := range fields(T) is a range loop
// whose X is fields(T) with T parsed as a type, plain, qualified or
// written with brackets, and that v.[f] is a field selection whose Field
// is the name f, as a value, a statement's target and a mut argument.
func TestParsesFieldsLoops(t *testing.T) {
	for _, tt := range []struct {
		name, typ string
	}{
		{"plain", "T"},
		{"qualified", "geo.Point"},
		{"list", "list[int64]"},
		{"map", "map[text.Text]int64"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n\nimport geo from \"m/geo\"\nimport text from \"std/text\"\n\n" +
				"func f() {\n    for f := range fields(" + tt.typ + ") {\n        g(v.[f])\n" +
				"        v.[f] = w.[f]\n        h(mut v.[f])\n    }\n}\n"
			file, diags := parse(t, src)
			if len(diags) > 0 {
				t.Fatalf("parse reported %+v", diags)
			}
			fn := file.Decls[0].(*ast.Func)
			loop, fo, ok := ast.FieldsLoop(fn.Body[0])
			if !ok {
				t.Fatalf("%T is no fields loop", fn.Body[0])
			}
			if got := src[fo.TypeExpr.Spanned().Start:fo.TypeExpr.Spanned().End]; got != tt.typ {
				t.Errorf("fields ranges over %q, want %q", got, tt.typ)
			}
			if got := src[fo.Span.Start:fo.Span.End]; got != "fields("+tt.typ+")" {
				t.Errorf("fields(T) covers %q", got)
			}
			if loop.Var == nil || loop.Var.Name != "f" {
				t.Errorf("loop variable %v, want f", loop.Var)
			}
			read := loop.Body[0].(*ast.ExprStmt).X.(*ast.Call).Args[0]
			assign := loop.Body[1].(*ast.Assign)
			mutArg := loop.Body[2].(*ast.ExprStmt).X.(*ast.Call).Args[0].(*ast.MutArg)
			for _, e := range []ast.Expr{read, assign.Target, assign.Value, mutArg.X} {
				checkFieldOf(t, src, e)
			}
		})
	}
}

// checkFieldOf checks e, an expression of src, is x.[f]: a Selector whose
// Field is the name f, whose Name is [f] and whose NameSpan covers it.
func checkFieldOf(t *testing.T, src string, e ast.Expr) {
	t.Helper()
	sel, ok := e.(*ast.Selector)
	if !ok || sel.Field == nil || sel.Field.Name != "f" || sel.Name != "[f]" {
		t.Errorf("%T %+v is no v.[f]", e, e)
		return
	}
	if got := src[sel.NameSpan.Start:sel.NameSpan.End]; got != "[f]" {
		t.Errorf("v.[f]'s name covers %q, want [f]", got)
	}
}

// TestParsesFieldsOfAnywhere checks fields(T) parses wherever an
// expression starts, the type checker being the one to say it can only
// be ranged over, and that x.[f] parses after any postfix chain.
func TestParsesFieldsOfAnywhere(t *testing.T) {
	src := "package main\n\nfunc f() {\n    x := fields(T)\n    fields(T)\n" +
		"    y := a[0].b.[f].c\n    z := g().[f]\n}\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	body := file.Decls[0].(*ast.Func).Body
	if _, ok := body[0].(*ast.Var).Value.(*ast.FieldsOf); !ok {
		t.Errorf("x := fields(T) holds %T", body[0].(*ast.Var).Value)
	}
	if _, ok := body[1].(*ast.ExprStmt).X.(*ast.FieldsOf); !ok {
		t.Errorf("fields(T) as a statement is %T", body[1].(*ast.ExprStmt).X)
	}
	outer := body[2].(*ast.Var).Value.(*ast.Selector)
	if inner, ok := outer.X.(*ast.Selector); !ok || inner.Field == nil || outer.Name != "c" {
		t.Errorf("a[0].b.[f].c is %+v", outer)
	}
	if sel := body[3].(*ast.Var).Value.(*ast.Selector); sel.Field == nil {
		t.Errorf("g().[f] is %+v", sel)
	}
}

// fieldsParseErrors are malformed fields loops and field selections, each
// with the message the parser must give.
var fieldsParseErrors = []struct{ name, src, msg string }{
	{
		"no type", "func f() {\n    for f := range fields() {\n    }\n}\n",
		"fields is a builtin, which takes a type, as in for f := range fields(T); " +
			"a call by that name always calls it",
	},
	{
		"value", "func f() {\n    for f := range fields(1) {\n    }\n}\n",
		"expected 'type name' found '1'",
	},
	{
		"unclosed fields", "func f() {\n    for f := range fields(T {\n    }\n}\n",
		"expected ')' found '{'",
	},
	{"number field", "func f() {\n    x := v.[0]\n}\n", "expected 'field variable' found '0'"},
	{"empty field", "func f() {\n    x := v.[]\n}\n", "expected 'field variable' found ']'"},
	{"unclosed field", "func f() {\n    x := v.[f\n}\n", "expected ']' found ';'"},
	{"two fields", "func f() {\n    x := v.[f, g]\n}\n", "expected ']' found ','"},
}

// TestRejectsMalformedFields checks each malformed fields loop and field
// selection is reported with its message.
func TestRejectsMalformedFields(t *testing.T) {
	for _, tt := range fieldsParseErrors {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := parse(t, "package main\n\n"+tt.src)
			if len(diags) == 0 {
				t.Fatal("parse reported nothing")
			}
			if diags[0].Message != tt.msg {
				t.Errorf("message = %q, want %q", diags[0].Message, tt.msg)
			}
		})
	}
}

// fieldsSeeds are sources about fields loops the fuzzer starts from.
var fieldsSeeds = []string{
	"package main\nfunc deep[T Data](v T) int64 {\nmut n := 0\nfor f := range fields(T) {\n" +
		"n = n +% 1 +% deep(v.[f])\nprint(f.name)\nm := f.index\n}\nreturn n\n}\n",
	"package main\nfunc g[T Data](mut v T, w T) {\nfor f := range fields(T) {\nfor e := range fields(T) {\n" +
		"v.[f] = w.[e]\nh(mut v.[f], w.[f].x)\n}\n}\n}\n",
	"package main\nfunc main() {\nfor range fields(geo.Point) {\n}\nfields(list[int64])\nx := p.[f].[g][0]\n}\n",
	"package main\nfunc main() {\nfor f := range fields(T {\n}\nx := v.[\n}\n",
}

// FuzzParseFields checks the parser never panics or hangs on sources
// near fields loops and field selections, and reports every bad node it
// recovers from.
func FuzzParseFields(f *testing.F) {
	for _, src := range fieldsSeeds {
		f.Add(src)
	}
	for _, tt := range fieldsParseErrors {
		f.Add("package main\n\n" + tt.src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		checkParse(t, src)
	})
}
