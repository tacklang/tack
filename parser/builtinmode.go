package parser

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/source"
)

// NewBuiltinParser returns a parser over tokens, as NewParser does, in
// builtin mode: the mode in which package builtin parses the file that
// documents the builtins, built into the compiler, and nothing else. In
// it two forms parse that are errors everywhere else, since only that
// file declares what the language itself provides:
//
//   - a var named by a keyword that is a builtin value, var true bool,
//     var noerror error (see builtinValueName);
//   - a type declared as itself, type int32 int32 (see selfNamedType).
//
// A func without a body parses in every mode, as an extern func's
// declaration does, and the resolver reports one in any file it resolves;
// the builtins' file is never resolved. No Tack source can turn the mode
// on: the loader parses every file with NewParser.
func NewBuiltinParser(fileID source.FileID, tokens []lexer.Token) *Parser {
	p := NewParser(fileID, tokens)
	p.builtinMode = true
	return p
}

// builtinValues are the keywords that name builtin values, which the
// builtins' file declares as vars in builtin mode.
var builtinValues = []lexer.KeywordType{lexer.True, lexer.False, lexer.NoError, lexer.CAllocator}

// parseVarName parses the name a var or a mut declares: a name, or, in
// builtin mode, a keyword that names a builtin value.
func (p *Parser) parseVarName() (*ast.Ident, []diag.Diagnostic) {
	if name, ok := p.builtinValueName(); ok {
		return name, nil
	}
	return p.parseIdent("variable name")
}

// builtinValueName consumes and returns the next token as a name when
// the parser is in builtin mode and it is a keyword naming a builtin
// value, true, false, noerror or c_allocator.
func (p *Parser) builtinValueName() (*ast.Ident, bool) {
	tk := p.peek()
	if !p.builtinMode || tk.Kind() != lexer.Keyword {
		return nil, false
	}
	for _, kw := range builtinValues {
		if tk.Value() == kw {
			p.next()
			return &ast.Ident{Name: kw.String(), Span: p.span(tk)}, true
		}
	}
	return nil, false
}

// selfNamedType consumes and returns the type a type declaration named
// name declares when the parser is in builtin mode and it is that name
// itself, type int32 int32: a builtin type, which no other declaration
// can describe.
func (p *Parser) selfNamedType(name *ast.Ident) (ast.Expr, bool) {
	tk := p.peek()
	if !p.builtinMode || tk.Kind() != lexer.Ident || lexer.ValueChecked[string](tk) != name.Name {
		return nil, false
	}
	p.next()
	return &ast.Ident{Name: name.Name, Span: p.span(tk)}, true
}
