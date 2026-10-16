package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// structShape renders an expression with the parts structs add written
// out: a selector as X.f, a composite literal as T{...} with each element,
// a keyed one as k: v.
func structShape(t *testing.T, e ast.Expr) string {
	t.Helper()
	switch x := e.(type) {
	case *ast.Selector:
		return structShape(t, x.X) + "." + x.Name
	case *ast.Index:
		return structShape(t, x.X) + "[" + structShape(t, x.Index) + "]"
	case *ast.KeyValue:
		return structShape(t, x.Key) + ": " + structShape(t, x.Value)
	case *ast.CompositeLit:
		return compositeShape(t, x)
	case *ast.Binary:
		return "(" + structShape(t, x.X) + " " + x.Op.String() + " " + structShape(t, x.Y) + ")"
	case *ast.Call:
		args := make([]string, len(x.Args))
		for i, a := range x.Args {
			args[i] = structShape(t, a)
		}
		return structShape(t, x.Fn) + "(" + strings.Join(args, ", ") + ")"
	default:
		return shape(t, e)
	}
}

// compositeShape renders a composite literal as T{...}, with each element as
// structShape renders it.
func compositeShape(t *testing.T, lit *ast.CompositeLit) string {
	t.Helper()
	elems := make([]string, len(lit.Elems))
	for i, el := range lit.Elems {
		elems[i] = structShape(t, el)
	}
	typ := ""
	if lit.TypeExpr != nil {
		typ = typeShape(t, lit.TypeExpr)
	}
	return typ + "{" + strings.Join(elems, ", ") + "}"
}

// typeDecl parses src and returns its first declaration as a type
// declaration, failing on any diagnostic.
func typeDecl(t *testing.T, src string) *ast.TypeDecl {
	t.Helper()
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("%q: %v", src, diags)
	}
	d, ok := file.Decls[0].(*ast.TypeDecl)
	if !ok {
		t.Fatalf("declaration is %T, want *ast.TypeDecl", file.Decls[0])
	}
	return d
}

// TestParsesStructDeclarations checks the field lists a struct type can be
// written with: one field per line, fields separated by semicolons, a
// trailing semicolon, none at all, and array and struct field types.
func TestParsesStructDeclarations(t *testing.T) {
	tests := []struct{ src, want string }{
		{"type P struct {\n    x int64\n    y int64\n}\n", "x int64; y int64"},
		{"type P struct { x int64; y int64 }\n", "x int64; y int64"},
		{"type P struct { x int64; }\n", "x int64"},
		{"type P struct {}\n", ""},
		{"type P struct {\n}\n", ""},
		{"type P struct { a [3]Q; q Q }\n", "a [3]Q; q Q"},
		{
			"type P struct {\n    // a comment\n    x bool // after\n\n    y string\n}\n",
			"x bool; y string",
		},
	}
	for _, tt := range tests {
		src := "package main\n\n" + tt.src + "\nfunc main() {\n}\n"
		d := typeDecl(t, src)
		st, ok := d.TypeExpr.(*ast.StructType)
		if !ok {
			t.Fatalf("%q: type is %T", tt.src, d.TypeExpr)
		}
		fields := make([]string, len(st.Fields))
		for i, f := range st.Fields {
			fields[i] = f.Name + " " + typeShape(t, f.TypeExpr)
		}
		if got := strings.Join(fields, "; "); got != tt.want || d.Name != "P" {
			t.Errorf("%q: got %s %q, want P %q", tt.src, d.Name, got, tt.want)
		}
	}
}

// TestStructDeclarationSpans checks that a type declaration spans from type
// to the closing brace, without the semicolon after it, and that its name
// and each field span what they should.
func TestStructDeclarationSpans(t *testing.T) {
	src := "package main\n\ntype P struct { x int64; ab [2]bool }   \n\nfunc main() {\n}\n"
	d := typeDecl(t, src)
	st := d.TypeExpr.(*ast.StructType)
	checks := map[string]ast.Span{
		"type P struct { x int64; ab [2]bool }": d.Span,
		"P":                                     d.NameSpan,
		"struct { x int64; ab [2]bool }":        st.Span,
		"x int64":                               st.Fields[0].Span,
		"ab [2]bool":                            st.Fields[1].Span,
		"ab":                                    st.Fields[1].NameSpan,
	}
	for want, span := range checks {
		if got := src[span.Start:span.End]; got != want {
			t.Errorf("span covers %q, want %q", got, want)
		}
	}
}

// TestParsesStructExpressions checks struct literals, keyed elements and
// field selections wherever an expression can be, and how they combine with
// indexes, calls and operators.
func TestParsesStructExpressions(t *testing.T) {
	tests := []struct{ expr, want string }{
		{"P{x: 1, y: 2}", "P{x: 1, y: 2}"},
		{"P{}", "P{}"},
		{"P{\n        x: 1,\n        y: 2,\n    }", "P{x: 1, y: 2}"},
		{"L{a: P{x: 1}, b: {x: 2}}", "L{a: P{x: 1}, b: {x: 2}}"},
		{"[2]P{{x: 1}, P{x: 2}}", "[2]P{{x: 1}, P{x: 2}}"},
		{"p.x", "p.x"},
		{"p.a[1].b.c", "p.a[1].b.c"},
		{"f(p).x", "f(p).x"},
		{"P{x: 1}.x", "P{x: 1}.x"},
		{"(p).x", "p.x"},
		{"p.x + q.y * 2", "(p.x + (q.y * 2))"},
		{"p == P{x: 1}", "(p == P{x: 1})"},
		{"P{1, 2}", "P{1, 2}"},
		{"[2]int64{0: 1}", "[2]int64{0: 1}"},
	}
	for _, tt := range tests {
		if got := structShape(t, declared(t, tt.expr)); got != tt.want {
			t.Errorf("%s: got %s, want %s", tt.expr, got, tt.want)
		}
	}
}

// TestSelectorSpans checks that a field selection spans from the start of
// what it selects from, its parentheses included, through the field name.
func TestSelectorSpans(t *testing.T) {
	src := "package main\n\nfunc main() {\n    x := (p).a[0].b\n}\n"
	sel := mainBody(t, "    x := (p).a[0].b")[0].(*ast.Var).Value.(*ast.Selector)
	if got := src[sel.Span.Start:sel.Span.End]; got != "(p).a[0].b" {
		t.Errorf("selector covers %q", got)
	}
	if got := src[sel.NameSpan.Start:sel.NameSpan.End]; got != "b" {
		t.Errorf("field name covers %q", got)
	}
}

// TestParsesFieldTargets checks every statement that writes a field: plain
// and compound assignment, increment and decrement, and several results,
// through any mix of fields and elements.
func TestParsesFieldTargets(t *testing.T) {
	tests := []struct{ stmt, want string }{
		{"p.x = 1", "p.x"},
		{"p.a[i].x += 2", "p.a[i].x"},
		{"a[0].x++", "a[0].x"},
		{"l.a.y--", "l.a.y"},
	}
	for _, tt := range tests {
		var target ast.Expr
		switch s := mainBody(t, "    "+tt.stmt)[0].(type) {
		case *ast.Assign:
			target = s.Target
		case *ast.IncDec:
			target = s.Target
		default:
			t.Fatalf("%s: statement is %T", tt.stmt, s)
		}
		if got := structShape(t, target); got != tt.want {
			t.Errorf("%s: target %s, want %s", tt.stmt, got, tt.want)
		}
	}

	m, ok := mainBody(t, "    p.x, a[1].y = f()")[0].(*ast.MultiAssign)
	if !ok || len(m.Targets) != 2 || structShape(t, m.Targets[1]) != "a[1].y" {
		t.Errorf("several results to fields parsed as %v", m)
	}
}

// TestStructLiteralsInHeaders checks Go's rule for literals in the header
// of an if, a for or a range loop: a name and a brace there is the name and
// the body, so a struct literal must be in parentheses, or inside a call,
// index or other literal, where the brace cannot open the body. An array
// literal is never ambiguous.
func TestStructLiteralsInHeaders(t *testing.T) {
	for _, body := range []string{
		"    if p == (P{x: 1}) {\n    }",
		"    if eq(p, P{x: 1}) {\n    }",
		"    if a[P{x: 1}.x] {\n    }",
		"    for p != (P{}) {\n    }",
		"    for _, q := range [1]P{{x: 1}} {\n        exit(q.x)\n    }",
		"    for i := 0; i < (P{x: 3}).x; i++ {\n    }",
		"    if ok {\n        x := P{x: 1}\n        exit(x.x)\n    }",
		"    for mut i := 0; i < n; i++ {\n        p := P{x: i}\n        exit(p.x)\n    }",
	} {
		src := "package main\n\nfunc main() {\n" + body + "\n}\n"
		if _, diags := parse(t, src); len(diags) > 0 {
			t.Errorf("%q: %v", body, diags)
		}
	}
}

// structParseErrors are struct programs the parser must reject, each with
// the message it must give and the source it must cover.
var structParseErrors = []struct{ name, src, msg, at string }{
	{
		"anonymous variable type", "func main() {\n    mut p struct { x int64 }\n}\n",
		"anonymous structs are not supported", "struct",
	},
	{
		"anonymous field type", "type P struct {\n    q struct { x int64 }\n}\n",
		"anonymous structs are not supported", "struct",
	},
	{
		"anonymous parameter type", "func f(p struct{}) {\n}\n",
		"anonymous structs are not supported", "struct",
	},
	{
		"anonymous result type", "func f() struct{} {\n}\n",
		"anonymous structs are not supported", "struct",
	},
	{
		"anonymous element type", "var a [2]struct{}\n",
		"anonymous structs are not supported", "struct",
	},
	{"named integer", "type C int64\n", "only struct and interface types can be declared", "int64"},
	{"named array", "type A [2]int64\n", "only struct and interface types can be declared", "["},
	{"alias", "type A = P\n", "expected 'struct or interface' found '='", ""},
	{"grouped", "type (\n    P struct {}\n)\n", "expected 'type name' found '('", ""},
	{"pointer receiver", "func (p *P) f() {\n}\n", "expected 'type name' found '*'", ""},
	{"embedded", "type P struct {\n    Q\n}\n", "embedded fields are not supported", "Q"},
	{"embedded last", "type P struct { x int64; Q }\n", "embedded fields are not supported", "Q"},
	{
		"visibility twice",
		"type P struct {\n    pub lib x int64\n}\n",
		"expected 'field name' found 'lib'",
		"",
	},
	{
		"visibility without a field",
		"type P struct { pub }\n",
		"expected 'field name' found '}'",
		"",
	},
	{
		"visibility without a type",
		"type P struct { lib x }\n",
		"embedded fields are not supported",
		"x",
	},
	{
		"local type", "func main() {\n    type P struct {}\n}\n",
		"types can only be declared at package level", "type",
	},
	{
		"literal in if header", "func main() {\n    if p == P{x: 1} {\n    }\n}\n",
		"a struct literal in an if, for or range header must be in parentheses", "{",
	},
	{
		"literal in for header", "func main() {\n    for p != P{x: 1} {\n    }\n}\n",
		"a struct literal in an if, for or range header must be in parentheses", "{",
	},
	{
		"literal in range header", "func main() {\n    for i := range P{n: 1}.n {\n    }\n}\n",
		"a struct literal in an if, for or range header must be in parentheses", "{",
	},
	{
		"fields without separator",
		"type P struct { x int64 y int64 }\n",
		"expected ';' found 'y'",
		"",
	},
	{"grouped fields", "type P struct { x, y int64 }\n", "expected 'type name' found ','", ""},
	{
		"field name missing",
		"type P struct { int64 }\n",
		"embedded fields are not supported",
		"int64",
	},
	{"two semicolons", "type P struct { x int64;; }\n", "expected 'field name' found ';'", ""},
	{
		"selector without name",
		"func main() {\n    x := p.\n}\n",
		"expected 'field name' found '}'",
		"",
	},
	{
		"selector of a number",
		"func main() {\n    x := p.1\n}\n",
		"expected 'field name' found '1'",
		"",
	},
	{"field declared", "func main() {\n    p.x := 1\n}\n", "expected '=' found ':='", ""},
	{"field statement", "func main() {\n    p.x\n}\n", "expected 'statement' found 'p'", ""},
	{
		"malformed field target",
		"func main() {\n    p.() = 1\n}\n",
		"expected 'type name' found ')'",
		"",
	},
	{"field of a call written", "func main() {\n    f().x = 1\n}\n", "expected ';' found '.'", ""},
	{"key without value", "func main() {\n    x := P{x:}\n}\n", "expected 'value' found '}'", ""},
}

// TestRejectsMalformedStructs checks each malformed program is reported as
// listed, by the parser, with no tree.
func TestRejectsMalformedStructs(t *testing.T) {
	for _, tt := range structParseErrors {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n\n" + tt.src
			file, diags := parse(t, src)
			if file != nil || len(diags) != 1 {
				t.Fatalf("got %v, %v", file, diags)
			}
			if diags[0].Message != tt.msg {
				t.Errorf("message = %q, want %q", diags[0].Message, tt.msg)
			}
			if tt.at != "" {
				if got := src[diags[0].Span.Start:diags[0].Span.End]; got != tt.at {
					t.Errorf("covers %q, want %q", got, tt.at)
				}
			}
		})
	}
}

// TestTruncatedStructsNeverPanic cuts a struct program off after every byte
// and parses what is left, which must give a tree or diagnostics, never a
// panic or a hang.
func TestTruncatedStructsNeverPanic(t *testing.T) {
	src := "package main\n\ntype P struct {\n    x int64; a [2]Q\n}\n\n" +
		"func main() {\n    mut p := P{x: 1, a: [2]Q{{}, Q{}}}\n    p.a[1].y += p.x\n" +
		"    if p == (P{x: 2}) {\n        exit(f(p).x)\n    }\n}\n"
	for i := range len(src) + 1 {
		file, diags := parse(t, src[:i])
		if file == nil && len(diags) == 0 {
			t.Fatalf("cut at %d: no tree and no diagnostics", i)
		}
	}
}

// TestDeeplyNestedStructExpressions checks long chains of fields and deeply
// nested literals parse without trouble.
func TestDeeplyNestedStructExpressions(t *testing.T) {
	chain := "p" + strings.Repeat(".f", 500)
	if got := structShape(t, declared(t, chain)); got != chain {
		t.Errorf("chain of 500 fields parsed as %.40s...", got)
	}
	lit := strings.Repeat("N{n: ", 200) + "N{}" + strings.Repeat("}", 200)
	if _, ok := declared(t, lit).(*ast.CompositeLit); !ok {
		t.Error("200 nested literals did not parse as a literal")
	}
}
