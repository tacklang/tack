package resolver_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
)

// resolveProgram parses each of srcs as a file of one package main and
// resolves the program, returning it with what the resolver reported.
func resolveProgram(t *testing.T, srcs ...string) (*ast.Program, []diag.Diagnostic) {
	t.Helper()
	pkg := &ast.Package{Name: "main", Path: "main"}
	for i, src := range srcs {
		f := &source.File{ID: source.FileID(rune('a' + i)), Filename: "case.tck", Data: []byte(src)}
		lex := lexer.New(f)
		if diags := lex.Lex(); len(diags) > 0 {
			t.Fatalf("lex: %s", diag.RenderAllPretty(f, diags))
		}
		file, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
		if len(diags) > 0 {
			t.Fatalf("parse: %s", diag.RenderAllPretty(f, diags))
		}
		pkg.Files = append(pkg.Files, file)
	}
	prog := &ast.Program{Packages: []*ast.Package{pkg}}
	return prog, resolver.New(prog, stdtest.MustBuiltins()).Resolve()
}

// typeDecl returns the declaration of the type named name in prog.
func typeDecl(t *testing.T, prog *ast.Program, name string) *ast.TypeDecl {
	t.Helper()
	for _, d := range prog.Decls() {
		if td, ok := d.(*ast.TypeDecl); ok && td.Name == name {
			return td
		}
	}
	t.Fatalf("no type %s", name)
	return nil
}

// TestRecordsMethodsOnTheirType checks every method is recorded on its
// receiver's type, in program order, from any file, before or after the
// type, with RecvType set, and that methods of two types may share a name.
func TestRecordsMethodsOnTheirType(t *testing.T) {
	prog, diags := resolveProgram(
		t,
		"package main\n\nfunc (b B) len() int64 {\n    return 0\n}\n\ntype B struct {\n    n int64\n}\n",
		"package main\n\nfunc (mut b B) append(x int64) {\n}\n\nfunc (c C) len() int64 {\n    return 1\n}\n\n"+
			"type C struct {\n}\n\nfunc main() {\n}\n",
	)
	if len(diags) > 0 {
		t.Fatalf("resolve reported %v", diags)
	}
	b, c := typeDecl(t, prog, "B"), typeDecl(t, prog, "C")
	if len(b.Methods) != 2 || b.Methods[0].Name != "len" || b.Methods[1].Name != "append" {
		t.Fatalf("B's methods: %v", b.Methods)
	}
	if len(c.Methods) != 1 || c.Method("len") == b.Method("len") {
		t.Fatalf("C's methods: %v", c.Methods)
	}
	for _, m := range b.Methods {
		if m.RecvType != b {
			t.Errorf("%s's receiver type is %v, want B", m.Name, m.RecvType)
		}
	}
	if b.Method("nope") != nil {
		t.Error("B has a method nope")
	}
}

// TestMethodNamesAreNotPackageNames checks a method may share its name
// with a function, a global, a builtin or main, and that a function's
// name stays free to call.
func TestMethodNamesAreNotPackageNames(t *testing.T) {
	_, diags := resolveProgram(t, "package main\n\ntype B struct {\n}\n\n"+
		"func (b B) f() {\n}\n\nfunc (b B) print() {\n}\n\nfunc (b B) main() {\n}\n\n"+
		"func (b B) g() {\n}\n\nvar g = 1\n\nfunc f() {\n}\n\n"+
		"func main() {\n    f()\n    print(\"x\")\n    exit(int32(g))\n}\n")
	if len(diags) > 0 {
		t.Fatalf("resolve reported %v", diags)
	}
}

// TestReceiverIsAParameter checks a method's body sees its receiver as a
// parameter, bound like any other.
func TestReceiverIsAParameter(t *testing.T) {
	prog, diags := resolveProgram(t, "package main\n\ntype B struct {\n    n int64\n}\n\n"+
		"func (b B) f() int64 {\n    return b.n\n}\n\nfunc main() {\n}\n")
	if len(diags) > 0 {
		t.Fatalf("resolve reported %v", diags)
	}
	var fn *ast.Func
	for _, d := range prog.Decls() {
		if f, ok := d.(*ast.Func); ok && f.Name == "f" {
			fn = f
		}
	}
	ret := fn.Body[0].(*ast.Return)
	id := ret.Values[0].(*ast.Selector).X.(*ast.Ident)
	if id.Def != fn.Recv() {
		t.Errorf("b is bound to %v, want the receiver", id.Def)
	}
}
