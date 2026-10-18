package builtin

import (
	_ "embed" // for the builtins' file
	"fmt"
	"sync"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
)

// docSource is the file that documents every builtin, built into the
// compiler. It is documentation only: the compiler never resolves,
// checks or compiles it, and every builtin's behavior is the compiler's.
//
//go:embed builtin.tck
var docSource []byte

// DocFilename is the name the builtins' file goes by, in spans and in the
// copy std holds at std/builtin/builtin.tck.
const DocFilename = "builtin.tck"

// DocFileID is the file ID the spans of the parsed builtins' file carry.
const DocFileID source.FileID = "<builtin>/builtin.tck"

// Source returns the builtins' file as built into the compiler. Callers
// must not change it.
func Source() []byte { return docSource }

// DocFile is the builtins' file parsed: its source, as spans into it are
// read, and its tree, whose declarations document the builtin types,
// values and functions, and the stand-in types their declarations use.
type DocFile struct {
	File *source.File
	Tree *ast.File
}

// Decl returns the declaration in d of the builtin named name, a type, a
// value or a function, or of a stand-in type, and whether there is one.
func (d *DocFile) Decl(name string) (ast.Decl, bool) {
	for _, decl := range d.Tree.Decls {
		if declName(decl) == name {
			return decl, true
		}
	}
	return nil, false
}

// declName returns the name decl declares: a function's, a var's or a
// type's, or "".
func declName(decl ast.Decl) string {
	switch x := decl.(type) {
	case *ast.Func:
		return x.Name
	case *ast.Var:
		return x.Name
	case *ast.TypeDecl:
		return x.Name
	}
	return ""
}

// parsed is the builtins' file, parsed once.
var parsed = sync.OnceValues(func() (*DocFile, error) { return parseDoc(docSource) })

// Doc returns the builtins' file, lexed and parsed in builtin mode (see
// parser.NewBuiltinParser), once; every call returns the same DocFile,
// which callers must not change. An editor maps a builtin's name to its
// declaration through it, for its documentation and to go to it. The
// error is never non-nil for the file built into the compiler, which a
// test parses.
func Doc() (*DocFile, error) { return parsed() }

// parseDoc lexes and parses src as the builtins' file, in builtin mode.
func parseDoc(src []byte) (*DocFile, error) {
	file := &source.File{ID: DocFileID, Filename: DocFilename, Data: src}
	lex := lexer.New(file)
	if diags := lex.Lex(); len(diags) > 0 {
		return nil, fmt.Errorf(
			"the builtins' file does not lex:\n%s",
			diag.RenderAllPretty(file, diags),
		)
	}
	tree, diags := parser.NewBuiltinParser(file.ID, lex.Tokens()).Parse()
	if len(diags) > 0 || tree == nil {
		return nil, fmt.Errorf("the builtins' file does not parse:\n%s",
			diag.RenderAllPretty(file, diags))
	}
	return &DocFile{File: file, Tree: tree}, nil
}
