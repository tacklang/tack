package parser_test

import (
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
)

// FuzzBuiltinModeOff parses arbitrary source as every file but the
// builtins' is parsed, with NewParser, and checks no tree it gives, even
// one recovered from errors, holds a form only builtin mode parses: a var
// named by a keyword, or a type declared as itself.
func FuzzBuiltinModeOff(f *testing.F) {
	for _, seed := range []string{
		"package builtin\n\nvar true bool\n",
		"package builtin\n\nvar noerror error\nvar c_allocator Allocator\n",
		"package builtin\n\ntype int32 int32\n",
		"package main\n\ntype T T\nvar false bool\n",
		"package main\n\nfunc f()\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		file := &source.File{ID: "f.tck", Filename: "f.tck", Data: []byte(src)}
		lex := lexer.New(file)
		if diags := lex.Lex(); len(diags) > 0 {
			return
		}
		tree, _ := parser.NewParser(file.ID, lex.Tokens()).Parse()
		if tree == nil {
			return
		}
		for _, decl := range tree.Decls {
			switch x := decl.(type) {
			case *ast.Var:
				if _, kw := lexer.LookupKeyword(x.Name); kw {
					t.Fatalf("parsed var %s, a keyword, outside builtin mode:\n%s", x.Name, src)
				}
			case *ast.TypeDecl:
				if id, ok := x.TypeExpr.(*ast.Ident); ok && id.Name == x.Name {
					t.Fatalf("parsed type %s %s outside builtin mode:\n%s", x.Name, x.Name, src)
				}
			}
		}
	})
}
