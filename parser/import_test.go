package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// mustParse parses src and fails the test on any diagnostic.
func mustParse(t *testing.T, src string) *ast.File {
	t.Helper()
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v for:\n%s", diags, src)
	}
	return file
}

// TestParsesImports checks each import's name, path and spans, in source
// order, raw string paths and escapes included, and that from stays an
// ordinary name everywhere else.
func TestParsesImports(t *testing.T) {
	src := "package main\n\nimport lexer from \"calc/lexer\"\nimport p from `calc/parser`\n" +
		"import x from \"calc/\\x41\"\n\nfunc main() {\n    from := 1\n    exit(from)\n}\n"
	file := mustParse(t, src)
	want := []struct{ name, path, nameText, pathText string }{
		{"lexer", "calc/lexer", "lexer", `"calc/lexer"`},
		{"p", "calc/parser", "p", "`calc/parser`"},
		{"x", "calc/A", "x", `"calc/\x41"`},
	}
	if len(file.Imports) != len(want) {
		t.Fatalf("got %d imports, want %d", len(file.Imports), len(want))
	}
	for i, w := range want {
		imp := file.Imports[i]
		if imp.Name != w.name || imp.Path != w.path ||
			src[imp.NameSpan.Start:imp.NameSpan.End] != w.nameText ||
			src[imp.PathSpan.Start:imp.PathSpan.End] != w.pathText ||
			!strings.HasPrefix(src[imp.Span.Start:imp.Span.End], "import "+w.name) {
			t.Errorf("import %d = %+v, want %+v", i, imp, w)
		}
	}
}

// qualifiedIn returns every qualified Ident in file, in walk order.
func qualifiedIn(file *ast.File) []*ast.Ident {
	var found []*ast.Ident
	ast.Walk(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Pkg != "" {
			found = append(found, id)
		}
		return true
	})
	return found
}

// TestParsesQualifiedNames checks an import's name, a period and a name is
// one qualified Ident spanning all three wherever a name may be: a callee,
// a value, a type in a signature, a variable and an array, a literal's
// type, an assignment target, followed by fields and indexes, and in an
// if header, where a literal still needs parentheses.
func TestParsesQualifiedNames(t *testing.T) {
	src := `package main

import lx from "calc/lexer"

var g [lx.Max]lx.Token

func f(t lx.Token) (lx.Token, int64) {
    return t, lx.Count
}

func main() {
    lx.Reset()
    lx.Count = lx.First("1").kind
    lx.Count++
    lx.Table[0].kind = 2
    t := lx.Token{kind: lx.KindNumber}
    if t == (lx.Token{kind: 1}) {
        exit(lx.Width(t))
    }
}
`
	file := mustParse(t, src)
	ids := qualifiedIn(file)
	got := make([]string, 0, len(ids))
	for _, id := range ids {
		if text := src[id.Span.Start:id.Span.End]; text != id.Pkg+"."+id.Name ||
			src[id.PkgSpan.Start:id.PkgSpan.End] != "lx" {
			t.Errorf("%s spans %q", id.Qualified(), text)
		}
		got = append(got, id.Qualified())
	}
	want := "lx.Max lx.Token lx.Token lx.Token lx.Count lx.Reset lx.Count lx.First lx.Count " +
		"lx.Table lx.Token lx.KindNumber lx.Token lx.Width"
	if strings.Join(got, " ") != want {
		t.Errorf("qualified names:\n%s\nwant\n%s", strings.Join(got, " "), want)
	}
}

// TestFieldOfNonImportIsASelector checks a name that is not an import's,
// followed by a period and a name, is still a field selection in an
// expression, and is a qualified name in a type, where a field cannot be.
func TestFieldOfNonImportIsASelector(t *testing.T) {
	src := "package main\n\nfunc main() {\n    var v p.T\n    exit(p.x)\n}\n"
	file := mustParse(t, src)
	ids := qualifiedIn(file)
	if len(ids) != 1 || ids[0].Qualified() != "p.T" {
		t.Errorf("qualified names = %v, want only the type p.T", ids)
	}
	sels := 0
	ast.Walk(file, func(n ast.Node) bool {
		if _, ok := n.(*ast.Selector); ok {
			sels++
		}
		return true
	})
	if sels != 1 {
		t.Errorf("got %d selectors, want p.x", sels)
	}
}

// TestRejectsMalformedImports checks every malformed import is a parse
// error naming the problem, starting at the right token.
func TestRejectsMalformedImports(t *testing.T) {
	for _, tt := range []struct{ name, src, want, at string }{
		{"grouped", "import (\n\tx from \"a\"\n)\n", "grouped imports are not supported", "("},
		{"no name", "import \"a\"\n", "an import needs a name", `"a"`},
		{"no from", "import x \"a\"\n", "expected 'from'", `"a"`},
		{"wrong word", "import x of \"a\"\n", "expected 'from'", "of"},
		{"no path", "import x from\n", "expected 'import path'", ""},
		{"path not a string", "import x from a\n", "expected 'import path'", "a"},
		{"number name", "import 1 from \"a\"\n", "expected 'import name'", "1"},
		{"two on a line", "import x from \"a\" import y from \"b\"\n", "expected ';'", "import"},
		{"after a declaration", "func f() {}\nimport x from \"a\"\n", "imports must come before", "import"},
		{"keyword name", "import func from \"a\"\n", "expected 'import name'", "func"},
		{"truncated", "import x from", "expected 'import path'", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n" + tt.src
			_, diags := parse(t, src)
			if len(diags) != 1 || !strings.Contains(diags[0].Message, tt.want) {
				t.Fatalf("got %+v, want one error containing %q", diags, tt.want)
			}
			if got := src[diags[0].Span.Start:]; !strings.HasPrefix(got, tt.at) {
				t.Errorf("error at %q, want it at %q", got, tt.at)
			}
		})
	}
}
