package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
)

// unionCase is one case a union declaration must parse to: its name and
// its payload's type as written, "" for none.
type unionCase struct {
	name, payload string
}

// unionDecls are union declarations, each with the cases it declares and
// its visibility.
var unionDecls = []struct {
	name, decl string
	cases      []unionCase
	vis        ast.Visibility
}{
	{
		"one per line",
		"type Expr union {\n    int    int64\n    name   string\n    binary Binary\n    eof\n}\n",
		[]unionCase{{"int", "int64"}, {"name", "string"}, {"binary", "Binary"}, {"eof", ""}},
		ast.Private,
	},
	{"empty", "type Never union {}\n", nil, ast.Private},
	{"empty on lines", "type Never union {\n}\n", nil, ast.Private},
	{
		"semicolons",
		"pub type T union { a int64; b; c [3]uint8 }\n",
		[]unionCase{{"a", "int64"}, {"b", ""}, {"c", "[3]uint8"}},
		ast.Pub,
	},
	{
		"lists and qualified types",
		"lib type T union {\n    l list[int64]\n    q lexer.Token\n    v [2]lexer.Token\n}\n",
		[]unionCase{{"l", "list[int64]"}, {"q", "lexer.Token"}, {"v", "[2]lexer.Token"}},
		ast.Lib,
	},
	{
		"names that are keywords elsewhere",
		"type T union {\n    print int64\n    len\n    main string\n}\n",
		[]unionCase{{"print", "int64"}, {"len", ""}, {"main", "string"}},
		ast.Private,
	},
	{
		"trailing semicolon",
		"type T union { a; }\n",
		[]unionCase{{"a", ""}},
		ast.Private,
	},
}

// TestParsesUnionDecls checks that a union declaration's cases are each a
// name and the type of a payload, or none, in order, with their spans.
func TestParsesUnionDecls(t *testing.T) {
	for _, tt := range unionDecls {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n\n" + tt.decl
			file, diags := parse(t, src)
			if len(diags) > 0 {
				t.Fatalf("parse reported %+v", diags)
			}
			td := file.Decls[0].(*ast.TypeDecl)
			ut := td.Union()
			if ut == nil || td.Interface() != nil || td.Vis != tt.vis {
				t.Fatalf("got %T, vis %s, want a %s union", td.TypeExpr, td.Vis, tt.vis)
			}
			checkUnionCases(t, src, ut, tt.cases)
		})
	}
}

// checkUnionCases fails the test unless ut, a union parsed from src, has
// exactly the cases want, in order, each found by its name, and spans its
// keyword through its closing brace.
func checkUnionCases(t *testing.T, src string, ut *ast.UnionType, want []unionCase) {
	t.Helper()
	if len(ut.Cases) != len(want) {
		t.Fatalf("got %d cases, want %d", len(ut.Cases), len(want))
	}
	for i, w := range want {
		checkUnionCase(t, src, ut.Cases[i], w)
		if c, at := ut.Case(w.name); c != ut.Cases[i] || at != i {
			t.Errorf("Case(%s) = %v at %d", w.name, c, at)
		}
	}
	if c, at := ut.Case("nope"); c != nil || at != -1 {
		t.Errorf("Case(nope) = %v at %d, want nil at -1", c, at)
	}
	if got := src[ut.Span.Start:ut.Span.End]; !strings.HasPrefix(got, "union") ||
		!strings.HasSuffix(got, "}") {
		t.Errorf("union spans %q", got)
	}
}

// checkUnionCase fails the test unless c, a case parsed from src, is
// named and typed as want, and spans its name and its type.
func checkUnionCase(t *testing.T, src string, c *ast.UnionCase, want unionCase) {
	t.Helper()
	if c.Name != want.name || src[c.NameSpan.Start:c.NameSpan.End] != want.name {
		t.Errorf("case %s, name spans %q, want %s", c.Name, src[c.NameSpan.Start:c.NameSpan.End],
			want.name)
	}
	payload := ""
	if c.TypeExpr != nil {
		span := c.TypeExpr.Spanned()
		payload = src[span.Start:span.End]
	}
	if payload != want.payload {
		t.Errorf("case %s has payload %q, want %q", c.Name, payload, want.payload)
	}
	text := src[c.Span.Start:c.Span.End]
	if !strings.HasPrefix(text, want.name) || !strings.HasSuffix(text, want.payload) {
		t.Errorf("case %s spans %q", c.Name, text)
	}
}

// unionParseErrors are union declarations the parser rejects, each with
// the message and the text the error must point at, "" for an unexpected
// token, which is reported where it starts.
var unionParseErrors = []struct {
	name, src, msg, at string
}{
	{
		"pub case",
		"type T union {\n    pub a int64\n}\n",
		"a union's case cannot be pub; it is as visible as its union",
		"pub",
	},
	{
		"lib case",
		"type T union {\n    lib a\n}\n",
		"a union's case cannot be lib; it is as visible as its union",
		"lib",
	},
	{
		"tag value",
		"type T union {\n    a int64 = 1\n}\n",
		"a union's case cannot have a tag value; it is a name and a payload's type, if any",
		"=",
	},
	{
		"tag value without payload",
		"type T union {\n    a = 1\n}\n",
		"a union's case cannot have a tag value; it is a name and a payload's type, if any",
		"=",
	},
	{
		"anonymous union field",
		"type S struct {\n    u union { a int64 }\n}\n",
		"anonymous unions are not supported",
		"union",
	},
	{
		"anonymous union result",
		"func f() union { a int64 } {\n}\n",
		"anonymous unions are not supported",
		"union",
	},
	{
		"anonymous union parameter",
		"func f(u union { a int64 }) {\n}\n",
		"anonymous unions are not supported",
		"union",
	},
	{
		"anonymous union local",
		"func f() {\n    var u union { a int64 }\n}\n",
		"anonymous unions are not supported",
		"union",
	},
	{
		"two cases on a line",
		"type T union {\n    a b c\n}\n",
		"expected ';' found 'c'",
		"",
	},
	{
		"parenthesised payload",
		"type T union {\n    a(int64)\n}\n",
		"expected 'type name' found '('",
		"",
	},
	{
		"no brace",
		"type T union a int64\n",
		"expected '{' found 'a'",
		"",
	},
	{
		"unterminated",
		"type T union {\n    a int64\n",
		"expected 'case name' found 'EOF'",
		"",
	},
	{
		"union as a name",
		"func f() {\n    union := 1\n}\n",
		"expected",
		"",
	},
}

// TestRejectsMalformedUnions checks each malformed union declaration is
// reported, with its message, at the text the error is about.
func TestRejectsMalformedUnions(t *testing.T) {
	for _, tt := range unionParseErrors {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n\n" + tt.src
			_, diags := parse(t, src)
			if len(diags) == 0 {
				t.Fatal("parsed without errors")
			}
			d := diags[0]
			if !strings.HasPrefix(d.Message, tt.msg) {
				t.Errorf("message = %q, want %q", d.Message, tt.msg)
			}
			if got := src[d.Span.Start:d.Span.End]; got != tt.at {
				t.Errorf("error points at %q, want %q", got, tt.at)
			}
		})
	}
}

// casePatternCases are case values of a switch, each with the case name
// and the binding ast.CasePattern reads from it, or none when it is not a
// pattern.
var casePatternCases = []struct {
	value, name, bind string
	ok                bool
}{
	{"eof", "eof", "", true},
	{"int(n)", "int", "n", true},
	{"int(_)", "int", "_", true},
	{"_", "", "", false},
	{"int(1)", "", "", false},
	{"int(n, m)", "", "", false},
	{"int()", "", "", false},
	{"int(n...)", "", "", false},
	{"lexer.eof", "", "", false},
	{"int(lexer.n)", "", "", false},
	{"T.int(n)", "", "", false},
	{"a.b", "", "", false},
	{"1", "", "", false},
}

// TestCasePattern checks which case values of an expression switch read
// as a union's patterns, and what case and binding each names.
func TestCasePattern(t *testing.T) {
	for _, tt := range casePatternCases {
		t.Run(tt.value, func(t *testing.T) {
			src := "package main\n\nimport lexer from \"m/lexer\"\n\nfunc main() {\n    switch x {\n    case " +
				tt.value + ":\n    }\n}\n"
			file, diags := parse(t, src)
			if len(diags) > 0 {
				t.Fatalf("parse reported %+v", diags)
			}
			fn := file.Decls[0].(*ast.Func)
			v := fn.Body[0].(*ast.Switch).Cases[0].Values[0]
			name, bind, ok := ast.CasePattern(v)
			if ok != tt.ok {
				t.Fatalf("CasePattern ok = %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			gotBind := ""
			if bind != nil {
				gotBind = bind.Name
			}
			if name.Name != tt.name || gotBind != tt.bind {
				t.Errorf("got %s(%s), want %s(%s)", name.Name, gotBind, tt.name, tt.bind)
			}
		})
	}
}

// unionSeeds are programs FuzzParseUnions starts from: union declarations
// of every shape, constructions, and switches whose values are patterns.
var unionSeeds = []string{
	"package main\ntype E union {\n    int int64\n    name string\n    eof\n}\n" +
		"func main() {\n    e := E.int(5)\n    switch e {\n    case int(n):\n        print(\"x\")\n" +
		"    case name, eof:\n    }\n}\n",
	"package main\ntype N union {}\nfunc main() {}\n",
	"package main\npub type T union { a int64; b; c [3]uint8 }\n",
	"package main\nfunc f(t T) bool {\n    return t == T.b && t != T.a(1)\n}\n",
	"package main\nfunc f() {\n    switch g() {\n    case a(_), b(x):\n    default:\n    }\n}\n",
	"package main\nimport a from \"m/a\"\nfunc f() {\n    x := a.T.c(a.T.b)\n}\n",
	"package main\ntype T union {\n    pub a int64\n    b = 2\n}\n",
	"package main\ntype S struct { u union { a } }\n",
}

// FuzzParseUnions parses random programs starting from union declarations,
// constructions and pattern switches, and checks the parser never panics,
// gives a tree exactly when it gives no diagnostic, and points every
// diagnostic inside the source.
func FuzzParseUnions(f *testing.F) {
	for _, src := range unionSeeds {
		f.Add(src)
	}
	for _, tt := range unionParseErrors {
		f.Add("package main\n\n" + tt.src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		file := &source.File{ID: "fuzz", Filename: "fuzz.tck", Data: []byte(src)}
		lex := lexer.New(file)
		if diags := lex.Lex(); len(diags) > 0 {
			return
		}
		tree, diags := parser.NewParser(file.ID, lex.Tokens()).Parse()
		if (tree == nil) == (len(diags) == 0) {
			t.Fatalf("tree %v with %d diagnostics", tree != nil, len(diags))
		}
		for _, d := range diags {
			checkSpan(t, src, d)
		}
	})
}
