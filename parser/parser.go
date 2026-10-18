package parser

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/types"
)

// Parser turns one file's token stream into an AST. After a syntax error
// it recovers and goes on (see recover.go), so a file yields a tree and
// every error it has, the damage marked in the tree by bad nodes.
type Parser struct {
	file *ast.File

	// parens holds, for each expression written in parentheses, the span
	// of its outermost pair. A parenthesised expression leaves no node of
	// its own, so this is how an expression built around one, such as
	// (a) == (b), spans its parentheses too. See extent.
	parens map[ast.Expr]ast.Span

	// imports holds the names the file's imports declare. Imports come
	// before every other declaration, so they are all known by the time a
	// body is parsed, and an import's name can never be redeclared, so a
	// name of one followed by a period and a name is always a qualified
	// name, never a field. See atQualified.
	imports map[string]bool

	// tooDeep is the error for nesting past maxNesting, once a program
	// has, after which nothing nests further (see nest).
	tooDeep *diag.Diagnostic

	fileID   source.FileID
	tokens   []lexer.Token // every token but the comments
	comments []lexer.Token // the comment tokens, in source order

	// errs is every error recorded so far, and maxErrors how many the
	// parse goes past before it stops, or 0 for no limit (see Limit).
	errs      []diag.Diagnostic
	maxErrors int

	index int

	// noLit is set while parsing the header of an if, a for or a range
	// loop, where a name followed by an opening brace is the name and then
	// the body, not a struct literal, as in Go. Inside parentheses,
	// brackets and braces a literal is unambiguous again, so parsing them
	// clears it. See allowLits.
	noLit bool

	// depth is how many levels of nesting the construct being parsed is
	// inside (see nest).
	depth int
}

// maxNesting is how many levels a program may nest one construct inside
// another, counted together across expressions, statements and types:
// a parenthesized expression, a unary operator or mut and its operand, a
// call's arguments, an index's or a slice's brackets, a literal's braces,
// a block, a switch's cases, and an array, view or list type, each one
// level. A chain of binary operators binds left to right and nests no
// deeper for its length, and an else if is a branch of the chain it is
// in, not a level of its own. Every level is a frame of the parser's own
// recursion and of the later stages', and the C a program becomes nests
// with it: clang runs out of stack past about a thousand, so a program
// is held to that, here, where the recursion starts.
const maxNesting = 1000

// nest enters one more level of nesting, opened by tk, or, past
// maxNesting, reports the program as nested too deeply, at tk, and
// returns that error, which every later call returns too without going
// deeper. Each nest that returns nothing is matched by an unnest.
func (p *Parser) nest(tk *lexer.Token) []diag.Diagnostic {
	if p.tooDeep != nil {
		return []diag.Diagnostic{*p.tooDeep}
	}
	if p.depth == maxNesting {
		p.tooDeep = &diag.Diagnostic{
			Severity: diag.Error,
			Message:  fmt.Sprintf("expression nested too deeply: more than %d levels", maxNesting),
			Span:     p.span(tk),
		}
		return []diag.Diagnostic{*p.tooDeep}
	}
	p.depth++
	return nil
}

// nestAt is nest for a construct opened by a token of kind, which is
// next if the construct is written right: a token of another kind is
// left for the construct's own check to report, and nests nothing, but is
// still matched by an unnest.
func (p *Parser) nestAt(kind lexer.TokenKind) []diag.Diagnostic {
	if p.peek().Kind() != kind {
		if p.tooDeep != nil {
			return []diag.Diagnostic{*p.tooDeep}
		}
		p.depth++
		return nil
	}
	return p.nest(p.peek())
}

// unnest leaves the level of nesting the last nest entered.
func (p *Parser) unnest() {
	p.depth--
}

// NewParser returns a parser over tokens, which must come from a lexer that
// ended them with an EOF token. Spans it creates are tagged with fileID.
//
// The comment tokens are set apart here, so the grammar never sees them: a
// comment is accepted at every token boundary, and the parse is the same as
// without it. The lexer has already inserted any semicolon the comment's
// line end calls for.
func NewParser(fileID source.FileID, tokens []lexer.Token) *Parser {
	p := &Parser{
		fileID:  fileID,
		tokens:  make([]lexer.Token, 0, len(tokens)),
		parens:  make(map[ast.Expr]ast.Span),
		imports: make(map[string]bool),
	}
	for _, tk := range tokens {
		if tk.Kind() == lexer.Comment {
			p.comments = append(p.comments, tk)
		} else {
			p.tokens = append(p.tokens, tk)
		}
	}
	return p
}

func (p *Parser) next() *lexer.Token {
	if p.index >= len(p.tokens) {
		return nil
	}
	t := &p.tokens[p.index]
	p.index++
	return t
}

func (p *Parser) peek() *lexer.Token {
	if p.index >= len(p.tokens) {
		return nil
	}
	return &p.tokens[p.index]
}

func (p *Parser) peekNext() *lexer.Token {
	if p.index+1 >= len(p.tokens) {
		return nil
	}
	return &p.tokens[p.index+1]
}

// peekAt returns the token n places ahead without consuming anything, or
// the final EOF token if that runs past the end.
func (p *Parser) peekAt(n int) *lexer.Token {
	if p.index+n >= len(p.tokens) {
		return &p.tokens[len(p.tokens)-1]
	}
	return &p.tokens[p.index+n]
}

func (p *Parser) peekPrev() *lexer.Token {
	if p.index <= 0 {
		return nil
	}
	return &p.tokens[p.index-1]
}

// lastEnd returns the offset just past the last token consumed that is not
// a semicolon. No span covers the semicolon that ends a statement or
// declaration, whether it was written in the source or inserted at a line
// end. An inserted one covers no bytes but sits after any spaces before the
// newline or comment, so a span ending there would take in those spaces.
func (p *Parser) lastEnd() int {
	tk := p.peekPrev()
	if tk.Kind() == lexer.Semi && p.index >= 2 {
		tk = &p.tokens[p.index-2]
	}
	return tk.OffsetEnd()
}

// Parse parses the whole file: the package clause, then every top-level
// declaration up to EOF. It returns the file and every syntax error found,
// in the order they were found, recovering from each (see recover.go): a
// part that could not be parsed is a bad node in the tree, so the tree
// holds one exactly when there are errors. A program nested too deeply
// gives no tree, and the errors up to it.
func (p *Parser) Parse() (*ast.File, []diag.Diagnostic) {
	if len(p.tokens) == 0 {
		panic("token list is empty")
	}

	p.file = ast.NewFile()
	for i := range p.comments {
		p.file.Comments = append(p.file.Comments, ast.Comment{
			Value: lexer.ValueChecked[string](&p.comments[i]),
			Span:  p.span(&p.comments[i]),
		})
	}

	p.parseHeader()
	for p.peek().Kind() != lexer.EOF && !p.halted() {
		start := p.index
		dec, decDiags := p.parseDecl()
		if len(decDiags) > 0 {
			dec = p.badDecl(start, decDiags)
		}
		p.file.Decls = append(p.file.Decls, dec)
	}

	// A program nested too deeply has no tree, even where a parse that
	// went on past the error found nothing else wrong.
	if p.tooDeep != nil {
		if !p.recorded(*p.tooDeep) {
			p.errs = append(p.errs, *p.tooDeep)
		}
		return nil, p.errs
	}
	p.file.Span.End = p.peek().OffsetEnd()

	return p.file, p.errs
}

// parseHeader parses the package clause and the imports, recording each
// that fails as a bad declaration (see recover.go).
func (p *Parser) parseHeader() {
	keyword, pkg, pkgDiags := p.parsePkg()
	if len(pkgDiags) > 0 {
		p.file.Decls = append(p.file.Decls, p.badDecl(0, pkgDiags))
		p.file.Span = p.file.Decls[0].Spanned()
	} else {
		p.file.Package = pkg.Name
		p.file.PackageSpan = pkg.Span
		p.file.Span = keyword
	}
	for isKeyword(p.peek(), lexer.Import) && !p.halted() {
		start := p.index
		imp, diags := p.parseImport()
		if len(diags) > 0 {
			p.file.Decls = append(p.file.Decls, p.badDecl(start, diags))
			continue
		}
		p.file.Imports = append(p.file.Imports, imp)
		p.imports[imp.Name] = true
	}
}

// parsePkg parses the package clause, and returns the span of its package
// keyword and its name.
func (p *Parser) parsePkg() (ast.Span, *ast.Ident, []diag.Diagnostic) {
	tk := p.next()

	switch {
	case tk.Kind() == lexer.Keyword && tk.Value() == lexer.Package:
		pkg, pkgDiags := p.parseIdent("package name")
		if pkgDiags != nil {
			return ast.Span{}, nil, pkgDiags
		}

		if ok, semiDiags := p.expect(lexer.Semi); !ok {
			return ast.Span{}, nil, semiDiags
		}
		return p.span(tk), pkg, nil
	default:
		return ast.Span{}, nil, []diag.Diagnostic{p.diagInvalidToken("package", tk)}
	}
}

func (p *Parser) parseIdent(expected string) (*ast.Ident, []diag.Diagnostic) {
	if tk := p.peek(); tk.Kind() != lexer.Ident {
		return nil, []diag.Diagnostic{p.diagInvalidToken(expected, tk)}
	}
	tk := p.next()
	return &ast.Ident{Name: lexer.ValueChecked[string](tk), Span: p.span(tk)}, nil
}

func (p *Parser) expect(kind lexer.TokenKind) (bool, []diag.Diagnostic) {
	if tk := p.peek(); tk.Kind() != kind {
		return false, []diag.Diagnostic{p.diagInvalidToken(kind.String(), tk)}
	}
	p.next()
	return true, nil
}

func (p *Parser) optional(kind lexer.TokenKind) {
	if p.peek().Kind() == kind {
		p.next()
	}
}

func (p *Parser) expectSequence(kinds ...lexer.TokenKind) (bool, []diag.Diagnostic) {
	for _, kind := range kinds {
		if ok, diags := p.expect(kind); !ok {
			return false, diags
		}
	}
	return true, nil
}

// parseImport parses one import declaration, import x from "path", and
// the semicolon that ends it. from is not a keyword: it is a name that
// only an import gives a meaning, so it may still name anything else.
func (p *Parser) parseImport() (*ast.Import, []diag.Diagnostic) {
	kw := p.next()
	switch tk := p.peek(); tk.Kind() {
	case lexer.LParen:
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "grouped imports are not supported; write one import per line",
			Span:     p.span(tk),
		}}
	case lexer.Str:
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "an import needs a name: import x from \"path\"",
			Span:     p.span(tk),
		}}
	}
	name, diags := p.parseIdent("import name")
	if name == nil {
		return nil, diags
	}
	if tk := p.peek(); !isKeyword(tk, lexer.From) {
		return nil, []diag.Diagnostic{p.diagInvalidToken(lexer.From.String(), tk)}
	}
	p.next()
	path := p.peek()
	if path.Kind() != lexer.Str {
		return nil, []diag.Diagnostic{p.diagInvalidToken("import path", path)}
	}
	p.next()
	imp := &ast.Import{
		Name:     name.Name,
		NameSpan: name.Span,
		Path:     lexer.ValueChecked[string](path),
		PathSpan: p.span(path),
		Span:     ast.Span{File: p.fileID, Start: kw.OffsetStart(), End: path.OffsetEnd()},
	}
	if ok, diags := p.expect(lexer.Semi); !ok {
		return nil, diags
	}
	return imp, nil
}

// parseDecl parses one package-level declaration: a function, a variable or
// a type, with the visibility keyword that may come first.
func (p *Parser) parseDecl() (ast.Decl, []diag.Diagnostic) {
	tk := p.peek()
	if vis, ok := visibilityOf(tk); ok {
		return p.parseVisibleDecl(vis)
	}
	switch {
	case isKeyword(tk, lexer.Import):
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "imports must come before every other declaration",
			Span:     p.span(tk),
		}}
	case tk.Value() == lexer.Func:
		return p.parseFunc(false)
	case isKeyword(tk, lexer.Extern):
		return p.parseExternFunc()
	case tk.Value() == lexer.Var || tk.Value() == lexer.Mut:
		return p.parsePackageVar()
	case tk.Value() == lexer.Type:
		return p.parseTypeDecl()
	default:
		return nil, []diag.Diagnostic{p.diagInvalidToken("func", tk)}
	}
}

// visibilityOf returns the visibility tk gives, if it is pub or lib.
func visibilityOf(tk *lexer.Token) (ast.Visibility, bool) {
	switch {
	case isKeyword(tk, lexer.Pub):
		return ast.Pub, true
	case isKeyword(tk, lexer.Lib):
		return ast.Lib, true
	default:
		return ast.Private, false
	}
}

// parseVisibleDecl parses a package-level declaration that starts with the
// keyword pub or lib, giving vis: a function, a variable or a type, whose
// span then starts at the keyword. Anything else after the keyword, a
// second one included, is reported.
func (p *Parser) parseVisibleDecl(vis ast.Visibility) (ast.Decl, []diag.Diagnostic) {
	kw := p.next()
	visSpan := p.span(kw)
	tk := p.peek()
	switch {
	case isKeyword(tk, lexer.Func) || isKeyword(tk, lexer.Extern):
		fn, diags := p.parseAnyFunc(tk)
		if fn == nil {
			return nil, diags
		}
		fn.Vis, fn.VisSpan, fn.Span.Start = vis, visSpan, visSpan.Start
		return fn, diags
	case isKeyword(tk, lexer.Var) || isKeyword(tk, lexer.Mut):
		v, diags := p.parsePackageVar()
		if v == nil {
			return nil, diags
		}
		v.Vis, v.VisSpan, v.Span.Start = vis, visSpan, visSpan.Start
		return v, diags
	case isKeyword(tk, lexer.Type):
		td, diags := p.parseTypeDecl()
		if td == nil {
			return nil, diags
		}
		td.Vis, td.VisSpan, td.Span.Start = vis, visSpan, visSpan.Start
		return td, diags
	default:
		return nil, []diag.Diagnostic{p.diagInvalidToken("func, var, mut or type", tk)}
	}
}

// parseAnyFunc parses a function declaration starting at tk, an extern
// func if tk is extern. One after pub or lib is parsed either way, so that
// the resolver can say why an extern func cannot be either.
func (p *Parser) parseAnyFunc(tk *lexer.Token) (*ast.Func, []diag.Diagnostic) {
	if isKeyword(tk, lexer.Extern) {
		return p.parseExternFunc()
	}
	return p.parseFunc(false)
}

// parseExternFunc parses an extern func declaration, extern func name(...)
// with its results and no body, or extern("c_name") func name(...), whose
// string gives its C name, whose span then starts at extern.
func (p *Parser) parseExternFunc() (*ast.Func, []diag.Diagnostic) {
	kw := p.next()
	externSpan := p.span(kw)
	var cName *lexer.Token
	if p.peek().Kind() == lexer.LParen {
		p.next()
		if cName = p.peek(); cName.Kind() != lexer.Str {
			return nil, []diag.Diagnostic{p.diagInvalidToken("C name in quotes", cName)}
		}
		p.next()
		if ok, diags := p.expect(lexer.RParen); !ok {
			return nil, diags
		}
		externSpan.End = p.lastEnd()
	}
	fn, diags := p.parseFunc(true)
	if fn == nil {
		return nil, diags
	}
	fn.Extern, fn.CName, fn.ExternSpan = true, fn.Name, externSpan
	if cName != nil {
		fn.CName, fn.CNameSpan = lexer.ValueChecked[string](cName), p.span(cName)
	}
	fn.Span.Start = kw.OffsetStart()
	return fn, nil
}

// parseFunc parses a function declaration from its func keyword: its
// name, parameters, results and body, which an extern func, extern says,
// must not have. A parenthesis right after func starts a method's
// receiver, func (b Builder) len(), which becomes the method's first
// parameter; whether an extern func may be a method is the resolver's to
// judge.
func (p *Parser) parseFunc(extern bool) (*ast.Func, []diag.Diagnostic) {
	tk := p.peek()
	if tk.Value() != lexer.Func {
		return nil, []diag.Diagnostic{p.diagInvalidToken("func", tk)}
	}

	// advance past the func keyword
	p.next()

	var recv *ast.Param
	expected := "function name"
	if p.peek().Kind() == lexer.LParen {
		var diags []diag.Diagnostic
		if recv, diags = p.parseReceiver(); recv == nil {
			return nil, diags
		}
		expected = "method name"
	}

	ident, identDiags := p.parseIdent(expected)
	if ident == nil {
		return nil, identDiags
	}

	fn := &ast.Func{
		Name:     ident.Name,
		NameSpan: ident.Span,
		Span:     p.span(tk),
		Body:     make([]ast.Stmt, 0),
		Method:   recv != nil,
	}

	if diags := p.parseTypeParams(fn, extern); diags != nil {
		return nil, diags
	}

	params, paramsDiags := p.parseParams()
	if paramsDiags != nil {
		return nil, paramsDiags
	}
	if recv != nil {
		params = append([]*ast.Param{recv}, params...)
	}
	fn.Params = params

	results, resultDiags := p.parseResults()
	if resultDiags != nil {
		return nil, resultDiags
	}
	fn.Results = results

	if bodyDiags := p.parseFuncBody(fn, extern); bodyDiags != nil {
		return nil, bodyDiags
	}

	fn.Span.End = p.lastEnd()

	return fn, nil
}

// parseTypeParams parses the type parameters of fn, a generic function,
// in the brackets that come next, if any, into fn.TypeParams: each a name and its
// constraint, as a parameter is a name and a type, with commas between
// them optional, as between parameters. An empty list, a name with no
// constraint after it, and type parameters on a method or, extern says,
// an extern func are reported; whether a constraint names an interface is
// the type checker's to judge.
func (p *Parser) parseTypeParams(fn *ast.Func, extern bool) []diag.Diagnostic {
	if p.peek().Kind() != lexer.LBrack {
		return nil
	}
	open := p.next() // the opening bracket
	for p.peek().Kind() != lexer.RBrack {
		name, diags := p.parseIdent("type parameter name")
		if name == nil {
			return diags
		}
		if k := p.peek().Kind(); k == lexer.Comma || k == lexer.RBrack {
			return []diag.Diagnostic{{
				Severity: diag.Error,
				Message: fmt.Sprintf("type parameter %s has no constraint; "+
					"write the interface it must satisfy after it, as in [%s Writer]",
					name.Name, name.Name),
				Span: name.Span,
			}}
		}
		constraint, diags := p.parseType()
		if constraint == nil {
			return diags
		}
		fn.TypeParams = append(fn.TypeParams, &ast.TypeParam{
			Constraint: constraint,
			Name:       name.Name,
			NameSpan:   name.Span,
			Span: ast.Span{
				File:  p.fileID,
				Start: name.Span.Start,
				End:   constraint.Spanned().End,
			},
		})
		p.optional(lexer.Comma)
	}
	closing := p.next()
	fn.TypeParamsSpan = ast.Span{
		File:  p.fileID,
		Start: open.OffsetStart(),
		End:   closing.OffsetEnd(),
	}
	if msg := typeParamsProblem(fn, extern); msg != "" {
		return []diag.Diagnostic{{Severity: diag.Error, Message: msg, Span: fn.TypeParamsSpan}}
	}
	return nil
}

// typeParamsProblem returns what is wrong with fn's type parameters,
// parsed, as a whole, extern saying whether fn is an extern func, or ""
// when nothing is: there are none, or fn is a method or an extern func,
// neither of which can be generic.
func typeParamsProblem(fn *ast.Func, extern bool) string {
	switch {
	case len(fn.TypeParams) == 0:
		return "empty type parameter list"
	case fn.Method:
		return "a method cannot have type parameters; only a function can be generic"
	case extern:
		return fmt.Sprintf(
			"extern func %s cannot have type parameters; its body is written in C",
			fn.Name,
		)
	default:
		return ""
	}
}

// atTypeArgs reports whether the name that starts the next tokens, ending
// n places ahead, is followed by brackets and then an opening
// parenthesis, f[T](x): a call written with type arguments, which a call
// never has, since they are inferred from its arguments. Nothing else
// takes that shape, since a call's callee is a name.
func (p *Parser) atTypeArgs(n int) bool {
	if p.peek().Kind() != lexer.Ident || p.peekAt(n+1).Kind() != lexer.LBrack {
		return false
	}
	return p.peekAt(p.afterGroup(n+1, lexer.LBrack, lexer.RBrack)).Kind() == lexer.LParen
}

// typeArgsError reports the call that starts the next tokens, whose name
// ends n places ahead, written with type arguments (see atTypeArgs).
func (p *Parser) typeArgsError(n int) []diag.Diagnostic {
	first, last := p.peek(), p.peekAt(p.afterGroup(n+1, lexer.LBrack, lexer.RBrack)-1)
	name := lexer.ValueChecked[string](first)
	if n > 0 {
		name += "." + lexer.ValueChecked[string](p.peekAt(n))
	}
	return []diag.Diagnostic{{
		Severity: diag.Error,
		Message: fmt.Sprintf(
			"cannot write type arguments; they are inferred from the arguments, as in %s(...)",
			name,
		),
		Span: ast.Span{File: p.fileID, Start: first.OffsetStart(), End: last.OffsetEnd()},
	}}
}

// notFinal is the error for a ... at span that does not mark a function's
// final parameter: one before another parameter, or one in a type
// anywhere else.
func notFinal(span ast.Span) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: diag.Error,
		Message:  "can only use ... with the final parameter",
		Span:     span,
	}
}

// parseReceiver parses a method's receiver, in the parentheses that come
// next: one parameter, mut b Builder or b Builder, followed by a comma or
// not. No receiver, () before a method's name, several, and a variadic
// one are reported; () with no name after it is a function's name left
// out. Which types a receiver may have is the resolver's to judge.
func (p *Parser) parseReceiver() (*ast.Param, []diag.Diagnostic) {
	if p.peekNext().Kind() == lexer.RParen && p.peekAt(2).Kind() != lexer.Ident {
		return nil, []diag.Diagnostic{p.diagInvalidToken("function name", p.peek())}
	}
	open := p.next() // the opening parenthesis
	if tk := p.peek(); tk.Kind() == lexer.RParen {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "method has no receiver",
			Span:     ast.Span{File: p.fileID, Start: open.OffsetStart(), End: tk.OffsetEnd()},
		}}
	}
	recv, diags := p.parseParam("receiver name")
	if recv == nil {
		return nil, diags
	}
	if recv.Variadic {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "a receiver cannot be variadic",
			Span:     recv.EllipsisSpan,
		}}
	}
	return p.endReceiver(recv)
}

// endReceiver parses what follows recv, a method's receiver, up to and
// including the closing parenthesis: a comma before it may be written, as
// after a parameter, and another receiver is reported.
func (p *Parser) endReceiver(recv *ast.Param) (*ast.Param, []diag.Diagnostic) {
	if p.peek().Kind() == lexer.Comma && p.peekNext().Kind() == lexer.RParen {
		p.next()
	}
	switch tk := p.peek(); {
	case tk.Kind() == lexer.RParen:
		p.next()
		return recv, nil
	case tk.Kind() == lexer.Comma || tk.Kind() == lexer.Ident || isKeyword(tk, lexer.Mut) ||
		isKeyword(tk, lexer.Own):
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "method has multiple receivers",
			Span:     p.span(tk),
		}}
	default:
		return nil, []diag.Diagnostic{p.diagInvalidToken(")", tk)}
	}
}

// parseParams parses a parenthesized parameter list, parentheses included.
// Commas between parameters are optional.
func (p *Parser) parseParams() ([]*ast.Param, []diag.Diagnostic) {
	if ok, lparenDiags := p.expect(lexer.LParen); !ok {
		return nil, lparenDiags
	}

	var params []*ast.Param
	for p.peek().Kind() != lexer.RParen {
		// A variadic parameter takes every value left in a call, so
		// nothing can follow it, another variadic included.
		if n := len(params); n > 0 && params[n-1].Variadic {
			return nil, []diag.Diagnostic{notFinal(params[n-1].EllipsisSpan)}
		}
		param, paramDiags := p.parseParam("parameter name")
		if param == nil {
			return nil, paramDiags
		}
		params = append(params, param)

		p.optional(lexer.Comma)
	}

	if ok, rparenDiags := p.expect(lexer.RParen); !ok {
		return nil, rparenDiags
	}
	return params, nil
}

// parseResults parses a function's result types: one type name, or a
// parenthesized, comma-separated list of them. A brace or semicolon instead
// means the function returns nothing, and the result is empty.
func (p *Parser) parseResults() ([]ast.Expr, []diag.Diagnostic) {
	// A ... goes to parseType, which reports it, rather than being taken
	// for a missing body.
	if tk := p.peek(); p.startsType(tk) || tk.Kind() == lexer.Ellipsis {
		typ, diags := p.parseType()
		if typ == nil {
			return nil, diags
		}
		return []ast.Expr{typ}, nil
	}
	if p.peek().Kind() != lexer.LParen {
		return nil, nil
	}
	p.next()

	var results []ast.Expr
	for {
		typ, diags := p.parseType()
		if typ == nil {
			return nil, diags
		}
		results = append(results, typ)

		if p.peek().Kind() != lexer.Comma {
			break
		}
		p.next()
	}

	if ok, diags := p.expect(lexer.RParen); !ok {
		return nil, diags
	}
	return results, nil
}

// parseFuncBody parses what follows a function's signature into fn.Body: a
// brace-delimited body with its terminator, or a lone semicolon for a
// function declared without one, which sets fn.NoBody. An extern func,
// extern says, has no body to parse: its body is in C.
func (p *Parser) parseFuncBody(fn *ast.Func, extern bool) []diag.Diagnostic {
	if tk := p.peek(); extern && tk.Kind() == lexer.LCurl {
		return []diag.Diagnostic{
			{
				Severity: diag.Error,
				Message: fmt.Sprintf(
					"extern func %s cannot have a body; its body is written in C",
					fn.Name,
				),
				Span: p.span(tk),
			},
		}
	}
	switch p.next().Kind() {
	case lexer.LCurl:
		fn.Body = append(fn.Body, p.parseStmts(false)...)
		if p.tooDeep != nil {
			return []diag.Diagnostic{*p.tooDeep}
		}
		if ok, seqDiags := p.expectSequence(lexer.RCurl, lexer.Semi); !ok {
			return seqDiags
		}
		return nil
	case lexer.Semi:
		fn.NoBody = true
		return nil
	default:
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "missing function body",
			Span:     p.span(p.peek()),
		}}
	}
}

// parseParam parses one parameter: a name and its type, with mut before
// the name for a mut parameter, mut p T, or own for an own one, own p T,
// and ... before the type for a variadic one, args ...T, whose type is
// then its element type. expected names what the name is, for the error
// when there is none: a parameter's or a receiver's.
func (p *Parser) parseParam(expected string) (*ast.Param, []diag.Diagnostic) {
	param := &ast.Param{}
	if diags := p.parseParamMode(param); diags != nil {
		return nil, diags
	}
	name, nameDiags := p.parseIdent(expected)
	if name == nil {
		return nil, nameDiags
	}
	if tk := p.peek(); tk.Kind() == lexer.Ellipsis {
		p.next()
		param.Variadic, param.EllipsisSpan = true, p.span(tk)
	}

	typ, typDiags := p.parseType()
	if typ == nil {
		return nil, typDiags
	}

	param.Name, param.NameSpan, param.TypeExpr = name.Name, name.Span, typ
	param.Span = ast.Span{File: p.fileID, Start: name.Span.Start, End: typ.Spanned().End}
	startAtMode(param)
	return param, nil
}

// parseParamMode parses the mut or own a parameter may start with into
// param. A parameter is a reference to its caller's variable or the owner
// of its caller's value, never both, so mut and own together, in either
// order, are reported at the second; one written twice is left for the
// parameter's name to report.
func (p *Parser) parseParamMode(param *ast.Param) []diag.Diagnostic {
	if !p.takeParamMode(param) {
		return nil
	}
	if tk := p.peek(); isKeyword(tk, lexer.Mut) && param.Own ||
		isKeyword(tk, lexer.Own) && param.Mut {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message: "a parameter cannot be both mut and own; " +
				"an own parameter is moved into a mutable variable with mut x := p",
			Span: p.span(tk),
		}}
	}
	return nil
}

// takeParamMode takes the next token into param when it is mut or own,
// and reports whether it was.
func (p *Parser) takeParamMode(param *ast.Param) bool {
	tk := p.peek()
	switch {
	case isKeyword(tk, lexer.Mut):
		param.Mut, param.MutSpan = true, p.span(tk)
	case isKeyword(tk, lexer.Own):
		param.Own, param.OwnSpan = true, p.span(tk)
	default:
		return false
	}
	p.next()
	return true
}

// startAtMode moves the start of param's span back to its mut or own,
// when it has one.
func startAtMode(param *ast.Param) {
	switch {
	case param.Mut:
		param.Span.Start = param.MutSpan.Start
	case param.Own:
		param.Span.Start = param.OwnSpan.Start
	}
}

// startsType reports whether tk can start a type: a name, an array type's
// bracket, list, map, func, or struct, which parseType reports, since a
// struct type can only be declared.
func (p *Parser) startsType(tk *lexer.Token) bool {
	switch tk.Kind() {
	case lexer.Ident, lexer.LBrack:
		return true
	case lexer.Keyword:
		return tk.Value() == lexer.Struct || tk.Value() == lexer.List ||
			tk.Value() == lexer.Union ||
			tk.Value() == lexer.Map || tk.Value() == lexer.Func
	default:
		return false
	}
}

// parseType parses a type: a name, qualified or not, an array type, a
// view type, a list type, a map type or a function type. parseParam takes the ... of a variadic parameter before its
// type, so a ... here is one written anywhere else, a result's type, a
// variable's, a field's or an element's, and is reported rather than
// skipped, which would make it mean the type after it.
func (p *Parser) parseType() (ast.Expr, []diag.Diagnostic) {
	if tk := p.peek(); tk.Kind() == lexer.Ellipsis {
		return nil, []diag.Diagnostic{notFinal(p.span(tk))}
	}

	if parse := p.compositeType(); parse != nil {
		return parse()
	}
	if diags := p.anonymousType(); diags != nil {
		return nil, diags
	}
	// A type is never a field, so a name, a period and a name is always a
	// qualified type name, whether or not the first name is an import's;
	// the resolver reports one that is not.
	if p.peek().Kind() == lexer.Ident && p.peekAt(1).Kind() == lexer.Period &&
		p.peekAt(2).Kind() == lexer.Ident {
		return p.parseQualified(), nil
	}
	typ, typDiags := p.parseIdent("type name")
	if typ == nil {
		return nil, typDiags
	}
	return typ, nil
}

// compositeType returns the parser of the type that starts with the next
// token when it is an array or a view type's bracket, list, map or func,
// and nil for any other.
func (p *Parser) compositeType() func() (ast.Expr, []diag.Diagnostic) {
	switch tk := p.peek(); {
	case tk.Kind() == lexer.LBrack:
		return func() (ast.Expr, []diag.Diagnostic) { return p.parseArrayType(false) }
	case isKeyword(tk, lexer.List):
		return p.parseListType
	case isKeyword(tk, lexer.Map):
		return p.parseMapType
	case isKeyword(tk, lexer.Func):
		return p.parseFuncType
	default:
		return nil
	}
}

// anonymousType reports a struct or a union type written where a type is,
// rather than declared, when one comes next, which is not supported, and
// returns nil otherwise.
func (p *Parser) anonymousType() []diag.Diagnostic {
	var kind string
	switch tk := p.peek(); {
	case isKeyword(tk, lexer.Struct):
		kind = "structs"
	case isKeyword(tk, lexer.Union):
		kind = "unions"
	default:
		return nil
	}
	return []diag.Diagnostic{{
		Severity: diag.Error,
		Message:  "anonymous " + kind + " are not supported",
		Span:     p.span(p.peek()),
	}}
}

// atQualified reports whether the tokens n places ahead are a qualified
// name: the name of one of the file's imports, a period and a name.
func (p *Parser) atQualified(n int) bool {
	tk := p.peekAt(n)
	if tk.Kind() != lexer.Ident || !p.imports[lexer.ValueChecked[string](tk)] {
		return false
	}
	return p.peekAt(n+1).Kind() == lexer.Period && p.peekAt(n+2).Kind() == lexer.Ident
}

// parseQualified parses a name, a period and a name, which the caller has
// seen are next, as one qualified Ident spanning all three.
func (p *Parser) parseQualified() *ast.Ident {
	pkg := p.next()
	p.next() // the period
	name := p.next()
	return &ast.Ident{
		Pkg:     lexer.ValueChecked[string](pkg),
		PkgSpan: p.span(pkg),
		Name:    lexer.ValueChecked[string](name),
		Span:    ast.Span{File: p.fileID, Start: pkg.OffsetStart(), End: name.OffsetEnd()},
	}
}

// parseName parses a name, qualified by an import's name or not; expected
// says what it is, for the error when there is none.
func (p *Parser) parseName(expected string) (*ast.Ident, []diag.Diagnostic) {
	if p.atQualified(0) {
		return p.parseQualified(), nil
	}
	return p.parseIdent(expected)
}

// parseStmt parses one statement, choosing the form by its first token and,
// for one starting with a name, the token after it. On an error the statement
// is an untyped nil: the parsers it dispatches to return concrete pointer
// types, and a nil one of those would be a non-nil ast.Stmt that callers
// testing for nil take for a statement.
func (p *Parser) parseStmt() (ast.Stmt, []diag.Diagnostic) {
	var (
		stmt  ast.Stmt
		diags []diag.Diagnostic
	)

	tk := p.peek()
	switch {
	case tk.Kind() == lexer.EOF:
		return nil, nil
	case p.atSimpleStmt():
		stmt, diags = p.parseSimpleStmt()
		if len(diags) == 0 {
			diags = p.endStmt(simpleStmtSpan(stmt))
		}
	case tk.Kind() == lexer.Keyword:
		stmt, diags = p.parseKeywordStmt(tk)
	default:
		diags = []diag.Diagnostic{p.diagInvalidToken("statement", tk)}
	}

	if len(diags) > 0 {
		return nil, diags
	}
	return stmt, nil
}

// atSimpleStmt reports whether the next statement is a simple one: a
// declaration starting with var or mut, or anything starting with a name.
func (p *Parser) atSimpleStmt() bool {
	tk := p.peek()
	switch tk.Kind() {
	case lexer.Ident:
		return true
	case lexer.Keyword:
		return tk.Value() == lexer.Var || tk.Value() == lexer.Mut
	default:
		return false
	}
}

// parseSimpleStmt parses a simple statement, without the semicolon that ends
// it: a declaration, an assignment, an increment or decrement, or a call,
// a method call included, mut x.m() among them.
// These are the statements a three-clause loop's init and post may be, so
// the loop can end them with its own separators. On an error the statement
// is an untyped nil, as in parseStmt.
func (p *Parser) parseSimpleStmt() (ast.Stmt, []diag.Diagnostic) {
	var (
		stmt  ast.Stmt
		diags []diag.Diagnostic
	)

	tk := p.peek()
	switch {
	case p.atMulti():
		stmt, diags = p.parseMulti()
	case tk.Kind() == lexer.Ident:
		stmt, diags = p.parseIdentStmt(tk)
	case p.atMutCall():
		stmt, diags = p.parseCallStmt(tk)
	case p.atSimpleStmt():
		stmt, diags = p.parseVarDecl()
	default:
		diags = []diag.Diagnostic{p.diagInvalidToken("statement", tk)}
	}

	if len(diags) > 0 {
		return nil, diags
	}
	return stmt, nil
}

// simpleStmtSpan returns a pointer to the span of a statement parseSimpleStmt
// produced, so the semicolon that ends it can be added to it.
func simpleStmtSpan(stmt ast.Stmt) *ast.Span {
	switch s := stmt.(type) {
	case *ast.Var:
		return &s.Span
	case *ast.Assign:
		return &s.Span
	case *ast.IncDec:
		return &s.Span
	case *ast.ExprStmt:
		return &s.Span
	case *ast.MultiVar:
		return &s.Span
	case *ast.MultiAssign:
		return &s.Span
	default:
		panic(fmt.Sprintf("parser: %T is not a simple statement", stmt))
	}
}

// parseKeywordStmt parses a statement that starts with keyword tk. One
// that holds blocks is parsed by parseBlockStmt.
func (p *Parser) parseKeywordStmt(tk *lexer.Token) (ast.Stmt, []diag.Diagnostic) {
	switch tk.Value() {
	case lexer.If, lexer.For, lexer.Switch, lexer.Defer:
		return p.parseBlockStmt(tk)
	case lexer.Return:
		return p.parseReturn()
	case lexer.Break:
		return p.parseJump(&ast.Break{Span: p.span(tk)})
	case lexer.Continue:
		return p.parseJump(&ast.Continue{Span: p.span(tk)})
	case lexer.Type:
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "types can only be declared at package level",
			Span:     p.span(tk),
		}}
	default:
		return nil, []diag.Diagnostic{p.diagInvalidToken("statement", tk)}
	}
}

// parseBlockStmt parses a statement that starts with keyword tk and holds
// blocks: an if, a loop, a switch or a defer.
func (p *Parser) parseBlockStmt(tk *lexer.Token) (ast.Stmt, []diag.Diagnostic) {
	switch tk.Value() {
	case lexer.If:
		return p.parseIf()
	case lexer.For:
		if p.atRange() {
			return p.parseForRange()
		}
		return p.parseFor()
	case lexer.Switch:
		return p.parseSwitch()
	default: // lexer.Defer
		return p.parseDefer()
	}
}

// parseIdentStmt parses a statement that starts with the name tk: a short
// variable declaration, an assignment (plain or compound), an increment or
// decrement, or a call, a method call included. A name followed by indexes
// or fields can only be assigned, incremented or decremented, or have a
// method called on it; anything else after them is reported by parsing
// them first, so a malformed index or field gets its own error.
func (p *Parser) parseIdentStmt(tk *lexer.Token) (ast.Stmt, []diag.Diagnostic) {
	// A qualified name can be assigned or called but not declared.
	n := p.nameEnd()
	if p.atTypeArgs(n) {
		return nil, p.typeArgsError(n)
	}
	postfixed := p.peekAt(n+1).Kind() == lexer.LBrack || p.peekAt(n+1).Kind() == lexer.Period
	switch after := p.afterTarget(n); after.Kind() {
	case lexer.Define:
		return p.parseDefine(postfixed || n > 0, after)
	case lexer.Assign,
		lexer.AddAssign,
		lexer.SubAssign,
		lexer.MulAssign,
		lexer.DivAssign,
		lexer.ModAssign,
		lexer.BitAndAssign,
		lexer.BitOrAssign,
		lexer.BitXorAssign,
		lexer.BitAndNotAssign,
		lexer.ShiftLeftAssign,
		lexer.ShiftRightAssign:
		return p.parseAssign()
	case lexer.LParen:
		return p.parseCallLike(tk, n, postfixed)
	case lexer.Inc, lexer.Dec:
		return p.parseIncDec()
	default:
		if postfixed {
			return nil, p.badTarget(tk)
		}
		return nil, []diag.Diagnostic{p.diagInvalidToken("statement", tk)}
	}
}

// parseCallLike parses a statement that starts with the name tk, which
// ends n places ahead, and holds a call: a method call, x.m(), f().m() or
// a[i].m(), a call of the function the name names, or, after indexes or
// fields, postfixed says, anything else, which is reported.
func (p *Parser) parseCallLike(
	tk *lexer.Token,
	n int,
	postfixed bool,
) (ast.Stmt, []diag.Diagnostic) {
	if p.atMethodCall(n) {
		return p.parseCallStmt(tk)
	}
	if postfixed {
		return nil, p.badTarget(tk)
	}
	return p.parseFuncCall()
}

// nameEnd returns how many places ahead the name that starts the next
// tokens ends: 0 for a name, 2 for a qualified one.
func (p *Parser) nameEnd() int {
	if p.atQualified(0) {
		return 2
	}
	return 0
}

// parseDefine parses a short variable declaration, whose := is after, or
// reports one whose target is not a plain name, when notName says so.
func (p *Parser) parseDefine(notName bool, after *lexer.Token) (ast.Stmt, []diag.Diagnostic) {
	if notName {
		return nil, []diag.Diagnostic{p.diagInvalidToken("=", after)}
	}
	return p.parseShortVarDecl()
}

// badTarget reports a statement that starts with the name tk and indexes or
// fields but is not an assignment, increment or decrement: the error in
// the indexes or fields themselves, if there is one, and otherwise the
// statement.
func (p *Parser) badTarget(tk *lexer.Token) []diag.Diagnostic {
	if _, diags := p.parseTarget(); diags != nil {
		return diags
	}
	return []diag.Diagnostic{p.diagInvalidToken("statement", tk)}
}

// afterTarget returns the token after the name n places ahead and every
// bracketed index, field selection and type assertion that follows it,
// a[i].x[j].(T), so a statement that starts with an element or a field
// can be told apart by what comes after it. The brackets and parentheses
// are matched, so an index may hold brackets of its own. A period not
// followed by a name or an opening parenthesis ends the target, and is
// what is returned.
func (p *Parser) afterTarget(n int) *lexer.Token {
	k := n + 1
	for {
		switch p.peekAt(k).Kind() {
		case lexer.LBrack:
			k = p.afterGroup(k, lexer.LBrack, lexer.RBrack)
		case lexer.Period:
			switch p.peekAt(k + 1).Kind() {
			case lexer.Ident:
				k += 2
			case lexer.LParen:
				k = p.afterGroup(k+1, lexer.LParen, lexer.RParen)
			default:
				return p.peekAt(k)
			}
		default:
			return p.peekAt(k)
		}
	}
}

// atMethodCall reports whether the name n places ahead, and every index,
// field selection, call and type assertion after it, end in a method
// call: a field's name and its parenthesized arguments, x.m(), a[i].f.m(),
// f().m() or x.m().n(), which is a call statement. The brackets and
// parentheses are matched, so an index or an argument may hold its own,
// and each token is looked at once.
func (p *Parser) atMethodCall(n int) bool {
	k, method := n+1, false
	for {
		switch tk := p.peekAt(k); tk.Kind() {
		case lexer.LBrack:
			k, method = p.afterGroup(k, lexer.LBrack, lexer.RBrack), false
		case lexer.LParen:
			method = k >= 2 && p.peekAt(k-1).Kind() == lexer.Ident &&
				p.peekAt(k-2).Kind() == lexer.Period
			k = p.afterGroup(k, lexer.LParen, lexer.RParen)
		case lexer.Period:
			if k = p.afterSelection(k); k < 0 {
				return false
			}
			method = false
		default:
			return method
		}
	}
}

// afterSelection returns how many places ahead the token after the period
// k places ahead, and the name or the parenthesized type assertion after
// it, is, or -1 when neither follows the period.
func (p *Parser) afterSelection(k int) int {
	switch p.peekAt(k + 1).Kind() {
	case lexer.Ident:
		return k + 2
	case lexer.LParen:
		return p.afterGroup(k+1, lexer.LParen, lexer.RParen)
	default:
		return -1
	}
}

// atMutCall reports whether the next tokens are mut, then a name, and
// what follows it ends in a method call (see atMethodCall): mut x.m(),
// the call of a mut receiver, rather than the declaration mut x := v.
func (p *Parser) atMutCall() bool {
	return isKeyword(p.peek(), lexer.Mut) && p.peekNext().Kind() == lexer.Ident &&
		p.atMethodCall(1)
}

// parseCallStmt parses a statement that starts with tk and is a method
// call, x.m(args) or mut x.m(args), without the semicolon that ends it.
// What follows the call, an index or a field of its result, is not a
// statement, and is reported at tk.
func (p *Parser) parseCallStmt(tk *lexer.Token) (ast.Stmt, []diag.Diagnostic) {
	x, diags := p.parseOperand()
	if x == nil {
		return nil, diags
	}
	call, ok := x.(*ast.Call)
	if !ok {
		return nil, []diag.Diagnostic{p.diagInvalidToken("statement", tk)}
	}
	return &ast.ExprStmt{X: call, Span: call.Span}, nil
}

// afterGroup returns how many places ahead the token after the group that
// open opens k places ahead is, matching the opens and closes nested in
// it, or where EOF is if the group never closes.
func (p *Parser) afterGroup(k int, open, closing lexer.TokenKind) int {
	depth := 0
	for {
		switch p.peekAt(k).Kind() {
		case open:
			depth++
		case closing:
			depth--
		case lexer.EOF:
			return k
		}
		k++
		if depth == 0 {
			return k
		}
	}
}

// parseTarget parses what an assignment, increment or decrement writes: a
// name, followed by any number of indexes and fields, a[i].x[j].
func (p *Parser) parseTarget() (ast.Expr, []diag.Diagnostic) {
	name, diags := p.parseName("variable name")
	if name == nil {
		return nil, diags
	}
	return p.parsePostfix(name)
}

// atMulti reports whether the next tokens start a statement binding several
// results: a name, or mut and a name, followed by a comma.
func (p *Parser) atMulti() bool {
	tk := p.peek()
	if tk.Kind() == lexer.Keyword && tk.Value() == lexer.Mut {
		return p.peekNext().Kind() == lexer.Ident && p.afterTarget(1).Kind() == lexer.Comma
	}
	return tk.Kind() == lexer.Ident && p.afterTarget(0).Kind() == lexer.Comma
}

// parseMulti parses a statement binding each result of a call: a
// comma-separated list of names, then := and the call to declare them, or =
// and the call to assign to them. In a declaration each name is made mutable
// by its own mut; an assignment has no mut. Whether the value is a call with
// that many results is the type checker's to judge.
func (p *Parser) parseMulti() (ast.Stmt, []diag.Diagnostic) {
	start := p.span(p.peek())

	var items []multiItem
	for {
		item, diags := p.parseMultiItem()
		if item.target == nil {
			return nil, diags
		}
		items = append(items, item)

		if p.peek().Kind() != lexer.Comma {
			break
		}
		p.next()
	}

	op := p.peek()
	if op.Kind() != lexer.Define && op.Kind() != lexer.Assign {
		return nil, []diag.Diagnostic{p.diagInvalidToken(":= or =", op)}
	}
	p.next()

	value, diags := p.parseExpr(1)
	if value == nil {
		return nil, diags
	}
	span := ast.Span{File: start.File, Start: start.Start, End: value.Spanned().End}

	if op.Kind() == lexer.Define {
		return multiVar(items, value, span)
	}
	return multiAssign(items, value, span)
}

// multiItem is one entry in the list of a statement binding several values:
// a name, or an element of a variable, with whether mut was written before
// it and the span of both.
type multiItem struct {
	target  ast.Expr
	span    ast.Span
	mutable bool
}

// multiAssign turns the entries parsed for an assignment of several values
// into its targets, reporting one written with mut, which only a
// declaration may have.
func multiAssign(
	items []multiItem,
	value ast.Expr,
	span ast.Span,
) (*ast.MultiAssign, []diag.Diagnostic) {
	m := &ast.MultiAssign{Value: value, Span: span}
	for _, item := range items {
		if item.mutable {
			return nil, []diag.Diagnostic{{
				Severity: diag.Error,
				Message:  "cannot use mut in an assignment",
				Span:     item.span,
			}}
		}
		m.Targets = append(m.Targets, item.target)
	}
	return m, nil
}

// parseMultiItem parses one entry in the list of a statement binding
// several values: a name, or an element of a variable, with the mut before
// it if there is one. Whether either is allowed depends on whether the
// statement declares or assigns, which is not known yet.
func (p *Parser) parseMultiItem() (multiItem, []diag.Diagnostic) {
	start := p.peek()
	mutable := start.Kind() == lexer.Keyword && start.Value() == lexer.Mut
	if mutable {
		p.next()
	}

	target, diags := p.parseTarget()
	if target == nil {
		return multiItem{}, diags
	}
	return multiItem{
		target:  target,
		span:    ast.Span{File: p.fileID, Start: start.OffsetStart(), End: target.Spanned().End},
		mutable: mutable,
	}, nil
}

// multiVar turns the entries parsed for a declaration of several values
// into its variables, each with no value of its own, reporting an element,
// which only an assignment can have.
func multiVar(items []multiItem, value ast.Expr, span ast.Span) (*ast.MultiVar, []diag.Diagnostic) {
	m := &ast.MultiVar{Value: value, Span: span}
	for _, item := range items {
		name, ok := item.target.(*ast.Ident)
		if !ok {
			return nil, []diag.Diagnostic{{
				Severity: diag.Error,
				Message:  "only a name can be declared; use = to assign to an element",
				Span:     item.target.Spanned(),
			}}
		}
		m.Vars = append(m.Vars, &ast.Var{
			Name:     name.Name,
			NameSpan: name.Span,
			Span:     item.span,
			Mutable:  item.mutable,
		})
	}
	return m, nil
}

// parseIncDec parses name++ or name--.
func (p *Parser) parseIncDec() (*ast.IncDec, []diag.Diagnostic) {
	target, diags := p.parseTarget()
	if target == nil {
		return nil, diags
	}
	op := p.next() // ++ or --

	return &ast.IncDec{
		Target: target,
		Dec:    op.Kind() == lexer.Dec,
		Span:   ast.Span{File: p.fileID, Start: target.Spanned().Start, End: op.OffsetEnd()},
	}, nil
}

// parseVarDecl parses a declaration that starts with var or mut: a name,
// then a type, a value after =, or both. mut also allows the short form
// mut x := value. Whether a declaration without a value is allowed depends on
// its mutability, which is the type checker's to judge, not the parser's.
func (p *Parser) parseVarDecl() (*ast.Var, []diag.Diagnostic) {
	kw := p.next() // the var or mut keyword

	v := &ast.Var{Span: p.span(kw), Mutable: kw.Value() == lexer.Mut}

	name, nameDiags := p.parseIdent("variable name")
	if name == nil {
		return nil, nameDiags
	}
	v.Name, v.NameSpan = name.Name, name.Span

	if v.Mutable && p.peek().Kind() == lexer.Define {
		p.next()
		value, valueDiags := p.parseExpr(1)
		if value == nil {
			return nil, valueDiags
		}
		v.Value = value
		v.Span.End = value.Spanned().End
		return v, nil
	}

	if diags := p.parseVarTypeAndValue(v); diags != nil {
		return nil, diags
	}
	v.Span.End = p.peekPrev().OffsetEnd()
	return v, nil
}

// parseVarTypeAndValue parses what follows a declared name: a type, = and a
// value, or both, into v. Neither is a syntax error.
func (p *Parser) parseVarTypeAndValue(v *ast.Var) []diag.Diagnostic {
	if tk := p.peek(); tk.Kind() == lexer.Ellipsis || p.startsType(tk) {
		typ, typDiags := p.parseType()
		if typ == nil {
			return typDiags
		}
		v.TypeExpr = typ
	}

	if p.peek().Kind() == lexer.Assign {
		p.next()
		value, valueDiags := p.parseExpr(1)
		if value == nil {
			return valueDiags
		}
		v.Value = value
	}

	if v.TypeExpr == nil && v.Value == nil {
		return []diag.Diagnostic{p.diagInvalidToken("type or =", p.peek())}
	}
	return nil
}

// parseTypeDecl parses a type declaration, type Name struct { ... }, and
// the semicolon after it. Only struct types can be declared: a name
// declared as another type, type Celsius int64, is reported.
func (p *Parser) parseTypeDecl() (*ast.TypeDecl, []diag.Diagnostic) {
	kw := p.next() // the type keyword

	name, diags := p.parseIdent("type name")
	if name == nil {
		return nil, diags
	}

	typ, diags := p.parseDeclaredType()
	if typ == nil {
		return nil, diags
	}

	d := &ast.TypeDecl{
		TypeExpr: typ,
		Name:     name.Name,
		NameSpan: name.Span,
		Span:     ast.Span{File: p.fileID, Start: kw.OffsetStart(), End: typ.Spanned().End},
	}
	return d, p.endStmt(&d.Span)
}

// parseDeclaredType parses the type a type declaration declares, after
// its name: a struct, an interface or a union type. Anything else, a
// type's name or an array type, which would declare another named type,
// is reported. On an error the type is an untyped nil.
func (p *Parser) parseDeclaredType() (ast.Expr, []diag.Diagnostic) {
	switch tk := p.peek(); {
	case isKeyword(tk, lexer.Struct):
		st, diags := p.parseStructType()
		if st == nil {
			return nil, diags
		}
		return st, nil
	case isKeyword(tk, lexer.Interface):
		it, diags := p.parseInterfaceType()
		if it == nil {
			return nil, diags
		}
		return it, nil
	case isKeyword(tk, lexer.Union):
		ut, diags := p.parseUnionType()
		if ut == nil {
			return nil, diags
		}
		return ut, nil
	case tk.Kind() == lexer.Ident || tk.Kind() == lexer.LBrack:
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "only struct, interface and union types can be declared",
			Span:     p.span(tk),
		}}
	default:
		return nil, []diag.Diagnostic{p.diagInvalidToken("struct, interface or union", tk)}
	}
}

// parseInterfaceType parses interface { ... }: its methods, each ended by
// a semicolon, written or inserted at a line end, which may be left out
// before the closing brace, as a struct's fields are.
func (p *Parser) parseInterfaceType() (*ast.InterfaceType, []diag.Diagnostic) {
	kw := p.next() // the interface keyword
	if ok, diags := p.expect(lexer.LCurl); !ok {
		return nil, diags
	}

	typ := &ast.InterfaceType{Methods: make([]*ast.IfaceMethod, 0)}
	for p.peek().Kind() != lexer.RCurl {
		m, diags := p.parseIfaceMethod()
		if m == nil {
			return nil, diags
		}
		typ.Methods = append(typ.Methods, m)

		if p.peek().Kind() == lexer.RCurl {
			break
		}
		if ok, diags := p.expect(lexer.Semi); !ok {
			return nil, diags
		}
	}

	end := p.next() // the closing brace
	typ.Span = ast.Span{File: p.fileID, Start: kw.OffsetStart(), End: end.OffsetEnd()}
	return typ, nil
}

// notMethod reports name, the name an interface's member starts with,
// when what follows it is no method's parameters: nothing, or a period
// and a name, which would embed an interface, or a type, which would make
// it a field. It returns nil when parameters follow.
func (p *Parser) notMethod(name *ast.Ident) []diag.Diagnostic {
	msg := "an interface cannot have fields; it holds only methods"
	switch p.peek().Kind() {
	case lexer.LParen:
		return nil
	case lexer.Semi, lexer.RCurl, lexer.Period:
		msg = "embedded interfaces are not supported"
	}
	return []diag.Diagnostic{{Severity: diag.Error, Message: msg, Span: name.Span}}
}

// parseIfaceMethod parses one method of an interface type: mut, if its
// receiver is taken as mut, its name, its parameters and its results, as
// a function's signature is written, with no receiver and no body.
// Visibility written on it, a name alone, which would embed an interface,
// a field and a body are reported.
func (p *Parser) parseIfaceMethod() (*ast.IfaceMethod, []diag.Diagnostic) {
	m := &ast.IfaceMethod{}
	tk := p.peek()
	if _, visible := visibilityOf(tk); visible {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message: fmt.Sprintf(
				"an interface's method cannot be %s; it is as visible as its interface",
				tk.Value()),
			Span: p.span(tk),
		}}
	}
	if isKeyword(tk, lexer.Mut) {
		p.next()
		m.Mut, m.MutSpan = true, p.span(tk)
	}
	if tk := p.peek(); isKeyword(tk, lexer.Own) {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message: "a method cannot take its receiver as own; " +
				"take it as a plain or a mut receiver",
			Span: p.span(tk),
		}}
	}
	name, diags := p.parseIdent("method name")
	if name == nil {
		return nil, diags
	}
	m.Name, m.NameSpan = name.Name, name.Span
	if bad := p.notMethod(name); bad != nil {
		return nil, bad
	}

	params, diags := p.parseParams()
	if diags != nil {
		return nil, diags
	}
	m.Params = params
	results, diags := p.parseResults()
	if diags != nil {
		return nil, diags
	}
	m.Results = results
	if tk := p.peek(); tk.Kind() == lexer.LCurl {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("interface method %s cannot have a body", m.Name),
			Span:     p.span(tk),
		}}
	}
	m.Span = ast.Span{File: p.fileID, Start: name.Span.Start, End: p.lastEnd()}
	if m.Mut {
		m.Span.Start = m.MutSpan.Start
	}
	return m, nil
}

// parseUnionType parses union { ... }: its cases, each ended by a
// semicolon, written or inserted at a line end, which may be left out
// before the closing brace, as a struct's fields are.
func (p *Parser) parseUnionType() (*ast.UnionType, []diag.Diagnostic) {
	kw := p.next() // the union keyword
	if ok, diags := p.expect(lexer.LCurl); !ok {
		return nil, diags
	}

	typ := &ast.UnionType{Cases: make([]*ast.UnionCase, 0)}
	for p.peek().Kind() != lexer.RCurl {
		c, diags := p.parseUnionCase()
		if c == nil {
			return nil, diags
		}
		typ.Cases = append(typ.Cases, c)

		if p.peek().Kind() == lexer.RCurl {
			break
		}
		if ok, diags := p.expect(lexer.Semi); !ok {
			return nil, diags
		}
	}

	end := p.next() // the closing brace
	typ.Span = ast.Span{File: p.fileID, Start: kw.OffsetStart(), End: end.OffsetEnd()}
	return typ, nil
}

// parseUnionCase parses one case of a union type: its name, and the type
// of its payload unless the case ends there. Visibility written on it,
// which a case takes from its union, and a value given to it, which
// would be a tag C picks instead, are reported.
func (p *Parser) parseUnionCase() (*ast.UnionCase, []diag.Diagnostic) {
	tk := p.peek()
	if _, visible := visibilityOf(tk); visible {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message: fmt.Sprintf(
				"a union's case cannot be %s; it is as visible as its union",
				tk.Value()),
			Span: p.span(tk),
		}}
	}
	name, diags := p.parseIdent("case name")
	if name == nil {
		return nil, diags
	}
	c := &ast.UnionCase{Name: name.Name, NameSpan: name.Span, Span: name.Span}
	if kind := p.peek().Kind(); kind == lexer.Semi || kind == lexer.RCurl {
		return c, nil
	}
	if p.peek().Kind() != lexer.Assign {
		typ, diags := p.parseType()
		if typ == nil {
			return nil, diags
		}
		c.TypeExpr, c.Span.End = typ, typ.Spanned().End
	}
	if tk := p.peek(); tk.Kind() == lexer.Assign {
		return nil, []diag.Diagnostic{
			{
				Severity: diag.Error,
				Message:  "a union's case cannot have a tag value; it is a name and a payload's type, if any",
				Span:     p.span(tk),
			},
		}
	}
	return c, nil
}

// parseStructType parses struct { ... }: its fields, each ended by a
// semicolon, written or inserted at a line end, which may be left out
// before the closing brace.
func (p *Parser) parseStructType() (*ast.StructType, []diag.Diagnostic) {
	kw := p.next() // the struct keyword
	if ok, diags := p.expect(lexer.LCurl); !ok {
		return nil, diags
	}

	typ := &ast.StructType{Fields: make([]*ast.Field, 0)}
	for p.peek().Kind() != lexer.RCurl {
		f, diags := p.parseField()
		if f == nil {
			return nil, diags
		}
		typ.Fields = append(typ.Fields, f)

		if p.peek().Kind() == lexer.RCurl {
			break
		}
		if ok, diags := p.expect(lexer.Semi); !ok {
			return nil, diags
		}
	}

	end := p.next() // the closing brace
	typ.Span = ast.Span{File: p.fileID, Start: kw.OffsetStart(), End: end.OffsetEnd()}
	return typ, nil
}

// parseField parses one field of a struct type: pub or lib, if written,
// a name and a type. A type alone, which would embed it, is reported.
func (p *Parser) parseField() (*ast.Field, []diag.Diagnostic) {
	vis, visible := visibilityOf(p.peek())
	var visSpan ast.Span
	if visible {
		visSpan = p.span(p.next())
	}

	name, diags := p.parseIdent("field name")
	if name == nil {
		return nil, diags
	}
	if kind := p.peek().Kind(); kind == lexer.Semi || kind == lexer.RCurl {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "embedded fields are not supported",
			Span:     name.Span,
		}}
	}

	typ, diags := p.parseType()
	if typ == nil {
		return nil, diags
	}
	start := name.Span.Start
	if visible {
		start = visSpan.Start
	}
	return &ast.Field{
		TypeExpr: typ,
		Name:     name.Name,
		NameSpan: name.Span,
		Span:     ast.Span{File: p.fileID, Start: start, End: typ.Spanned().End},
		Vis:      vis,
		VisSpan:  visSpan,
	}, nil
}

// parsePackageVar parses a declaration outside any function. It is the same
// as one inside a function, except that the short form mut x := value is
// not allowed here.
func (p *Parser) parsePackageVar() (*ast.Var, []diag.Diagnostic) {
	if p.peekNext().Kind() == lexer.Ident && p.peekAt(2).Kind() == lexer.Define {
		p.next() // the mut keyword
		p.next() // the name
		return nil, []diag.Diagnostic{p.diagInvalidToken("type or =", p.peek())}
	}

	v, diags := p.parseVarDecl()
	if v == nil {
		return nil, diags
	}
	return v, p.endStmt(&v.Span)
}

// parseShortVarDecl parses name := value, which declares an immutable
// variable whose type is the value's.
func (p *Parser) parseShortVarDecl() (*ast.Var, []diag.Diagnostic) {
	name, _ := p.parseIdent("variable name")
	p.next() // the := token

	v := &ast.Var{Name: name.Name, NameSpan: name.Span, Span: name.Span}

	value, valueDiags := p.parseExpr(1)
	if value == nil {
		return nil, valueDiags
	}
	v.Value = value
	v.Span.End = value.Spanned().End

	return v, nil
}

// parseAssign parses name = value, or a compound assignment such as
// name += value.
func (p *Parser) parseAssign() (*ast.Assign, []diag.Diagnostic) {
	target, diags := p.parseTarget()
	if target == nil {
		return nil, diags
	}
	op := p.next() // = or a compound assignment operator

	a := &ast.Assign{Target: target, Op: compoundOps[op.Kind()], Span: target.Spanned()}

	value, valueDiags := p.parseExpr(1)
	if value == nil {
		return nil, valueDiags
	}
	a.Value = value
	a.Span.End = value.Spanned().End

	return a, nil
}

// compoundOps maps each compound assignment operator to the arithmetic,
// bitwise or shift operator it applies. A plain = is absent, so it maps to ast.InvalidOp.
var compoundOps = map[lexer.TokenKind]ast.BinaryOp{
	lexer.AddAssign: ast.Add,
	lexer.SubAssign: ast.Sub,
	lexer.MulAssign: ast.Mul,
	lexer.DivAssign: ast.Div,
	lexer.ModAssign: ast.Rem,

	lexer.BitAndAssign:     ast.BitAnd,
	lexer.BitOrAssign:      ast.BitOr,
	lexer.BitXorAssign:     ast.BitXor,
	lexer.BitAndNotAssign:  ast.AndNot,
	lexer.ShiftLeftAssign:  ast.Shl,
	lexer.ShiftRightAssign: ast.Shr,
}

// endStmt consumes the semicolon that ends a statement, which may be left out
// before a closing brace, and ends span at the last token before it, leaving
// the semicolon out. It returns the diagnostics for anything else found
// there.
func (p *Parser) endStmt(span *ast.Span) []diag.Diagnostic {
	if ok, semiDiags := p.expect(lexer.Semi); !ok && p.peek().Kind() != lexer.RCurl {
		return semiDiags
	}
	span.End = p.lastEnd()
	return nil
}

// parseFuncCall parses a call used as a statement, without the semicolon
// that ends it.
func (p *Parser) parseFuncCall() (*ast.ExprStmt, []diag.Diagnostic) {
	call, callDiags := p.parseCall()
	if call == nil {
		return nil, callDiags
	}

	return &ast.ExprStmt{X: call, Span: call.Span}, nil
}

// parseCall parses a call as an expression, without the statement terminator.
// The same syntax appears as a statement and as an operand.
func (p *Parser) parseCall() (*ast.Call, []diag.Diagnostic) {
	ident, identDiags := p.parseName("function name")
	if ident == nil {
		return nil, identDiags
	}
	return p.parseArgs(ident, ident.Span)
}

// parseArgs parses the parenthesized arguments of a call to fn, which
// spans fn's own span through the closing parenthesis.
func (p *Parser) parseArgs(fn ast.Expr, span ast.Span) (*ast.Call, []diag.Diagnostic) {
	if diags := p.nestAt(lexer.LParen); diags != nil {
		return nil, diags
	}
	defer p.unnest()
	return p.parseArgList(fn, span)
}

// parseArgList parses the parenthesized arguments of a call of fn, which
// starts at span, as parseArgs does once it has entered their level of
// nesting.
func (p *Parser) parseArgList(fn ast.Expr, span ast.Span) (*ast.Call, []diag.Diagnostic) {
	call := &ast.Call{
		Fn:   fn,
		Args: make([]ast.Expr, 0),
		Span: span,
	}

	if ok, lparenDiags := p.expect(lexer.LParen); !ok {
		return nil, lparenDiags
	}
	defer p.allowLits(true)()

	for p.peek().Kind() != lexer.RParen && p.peek().Kind() != lexer.EOF {
		// Every argument after the first is preceded by a comma. Requiring it
		// keeps f(1 2) from quietly becoming a two-argument call.
		if len(call.Args) > 0 {
			if ok, commaDiags := p.expect(lexer.Comma); !ok {
				return nil, commaDiags
			}
		}

		val, valDiags := p.parseExpr(1)
		if val == nil {
			return nil, valDiags
		}
		call.Args = append(call.Args, val)

		// A spread, v..., passes v as the whole of a variadic parameter,
		// the last, so it can only follow the last argument.
		if tk := p.peek(); tk.Kind() == lexer.Ellipsis {
			p.next()
			if p.peek().Kind() != lexer.RParen {
				return nil, []diag.Diagnostic{{
					Severity: diag.Error,
					Message:  "can only use ... with the final argument",
					Span:     p.span(tk),
				}}
			}
			call.Spread, call.EllipsisSpan = true, p.span(tk)
		}
	}

	if ok, rparenDiags := p.expect(lexer.RParen); !ok {
		return nil, rparenDiags
	}

	call.Span.End = p.peekPrev().OffsetEnd()

	return call, nil
}

// parseBlock parses a brace-delimited statement list.
func (p *Parser) parseBlock() ([]ast.Stmt, []diag.Diagnostic) {
	if diags := p.nestAt(lexer.LCurl); diags != nil {
		return nil, diags
	}
	defer p.unnest()
	if ok, lcurlDiags := p.expect(lexer.LCurl); !ok {
		return nil, lcurlDiags
	}

	body := p.parseStmts(false)
	if p.tooDeep != nil {
		return nil, []diag.Diagnostic{*p.tooDeep}
	}
	if ok, rcurlDiags := p.expect(lexer.RCurl); !ok {
		return nil, rcurlDiags
	}
	return body, nil
}

func (p *Parser) parseIf() (*ast.If, []diag.Diagnostic) {
	first, diags := p.parseIfBranch()
	if first == nil {
		return nil, diags
	}
	// An else if is the else branch of the if before it, holding a nested
	// if, but the chain is parsed in a loop, not by recursion, so its
	// length, a chain of a thousand branches flat in the source, costs no
	// stack.
	chain := []*ast.If{first}
	for last := first; ; {
		els, next, diags := p.parseElse()
		if diags != nil {
			return nil, diags
		}
		if next == nil {
			last.Else = els
			break
		}
		last.Else = []ast.Stmt{next}
		chain = append(chain, next)
		last = next
	}

	if ok, semiDiags := p.expect(lexer.Semi); !ok && p.peek().Kind() != lexer.RCurl {
		return nil, semiDiags
	}

	// Each if of the chain spans to the end of the chain, as the else
	// branch nested in it does.
	end := p.lastEnd()
	for _, stmt := range chain {
		stmt.Span.End = end
	}
	return first, nil
}

// parseIfBranch parses the if keyword, the condition and the block of
// one if, of a chain or alone, leaving its else branch to parseIf.
func (p *Parser) parseIfBranch() (*ast.If, []diag.Diagnostic) {
	tk := p.next() // the if keyword

	stmt := &ast.If{Span: p.span(tk)}

	restore := p.allowLits(false)
	cond, condDiags := p.parseExpr(1)
	restore()
	if cond == nil {
		return nil, condDiags
	}
	stmt.Cond = cond

	if diags := p.headerLit("an if, for or range"); diags != nil {
		return nil, diags
	}
	then, thenDiags := p.parseBlock()
	if then == nil {
		return nil, thenDiags
	}
	stmt.Then = then
	return stmt, nil
}

// parseElse parses an optional else branch, returning nil statements when
// there is none. An else if is parsed only as far as its own if, next,
// whose else branch parseIf goes on to parse.
func (p *Parser) parseElse() (els []ast.Stmt, next *ast.If, diags []diag.Diagnostic) {
	if p.peek().Kind() != lexer.Keyword || p.peek().Value() != lexer.Else {
		return nil, nil, nil
	}
	p.next()

	if p.peek().Kind() == lexer.Keyword && p.peek().Value() == lexer.If {
		nested, nestedDiags := p.parseIfBranch()
		if nested == nil {
			return nil, nil, nestedDiags
		}
		return nil, nested, nil
	}

	block, blockDiags := p.parseBlock()
	if block == nil {
		return nil, nil, blockDiags
	}
	return block, nil, nil
}

// parseSwitch parses a switch: the keyword, an optional tag, and the
// cases in braces. With no tag, the body's brace comes straight after the
// keyword. As in an if header, a name followed by a brace in the tag is
// the tag and then the body, never a struct literal. There is no init
// statement, as an if has none: a name and := after the keyword bind the
// value of a type switch's tag, x.(type), and nothing else (see
// parseBinding).
func (p *Parser) parseSwitch() (*ast.Switch, []diag.Diagnostic) {
	tk := p.next() // the switch keyword

	stmt := &ast.Switch{Span: p.span(tk), Cases: make([]*ast.CaseClause, 0)}

	if p.peek().Kind() != lexer.LCurl {
		bind, diags := p.parseBinding()
		if diags != nil {
			return nil, diags
		}
		restore := p.allowLits(false)
		tag, diags := p.parseExpr(1)
		restore()
		if tag == nil {
			return nil, diags
		}
		stmt.Tag, stmt.Bind = tag, bind
		if diags := p.checkBinding(stmt); diags != nil {
			return nil, diags
		}
		if diags := p.headerLit("a switch"); diags != nil {
			return nil, diags
		}
	}

	if diags := p.parseCases(stmt); diags != nil {
		return nil, diags
	}
	stmt.Span.End = p.peekPrev().OffsetEnd()

	if ok, semiDiags := p.expect(lexer.Semi); !ok && p.peek().Kind() != lexer.RCurl {
		return nil, semiDiags
	}
	return stmt, nil
}

// parseBinding parses the name and := of a type switch's binding, switch
// v := x.(type), when they come next, and returns the variable, or nil
// when the tag follows the keyword directly. The variable is immutable:
// a name written with mut is reported, as a range loop's is.
func (p *Parser) parseBinding() (*ast.Var, []diag.Diagnostic) {
	start := p.peek()
	mutable := isKeyword(start, lexer.Mut)
	k := 0
	if mutable {
		k = 1
	}
	if p.peekAt(k).Kind() != lexer.Ident || p.peekAt(k+1).Kind() != lexer.Define {
		return nil, nil
	}
	if mutable {
		p.next()
	}
	name, _ := p.parseIdent("variable name")
	p.next() // the :=
	span := ast.Span{File: p.fileID, Start: start.OffsetStart(), End: name.Span.End}
	if mutable {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "cannot use mut on a type switch variable",
			Span:     span,
		}}
	}
	return &ast.Var{Name: name.Name, NameSpan: name.Span, Span: span}, nil
}

// checkBinding reports a switch whose header binds a name to a tag that is
// not x.(type): a switch has no init statement, and only a type switch
// binds a name.
func (p *Parser) checkBinding(stmt *ast.Switch) []diag.Diagnostic {
	if stmt.Bind == nil || stmt.IsTypeSwitch() {
		return nil
	}
	return []diag.Diagnostic{
		{
			Severity: diag.Error,
			Message:  "a switch has no init statement; only a type switch binds a name, as in switch v := x.(type)",
			Span: ast.Span{
				File:  p.fileID,
				Start: stmt.Bind.Span.Start,
				End:   p.extent(stmt.Tag).End,
			},
		},
	}
}

// parseCases parses a switch's body into stmt.Cases: an opening brace,
// any number of case clauses, and the closing brace. A second default is
// reported, with a note pointing at the first.
func (p *Parser) parseCases(stmt *ast.Switch) []diag.Diagnostic {
	if diags := p.nestAt(lexer.LCurl); diags != nil {
		return diags
	}
	defer p.unnest()
	return p.parseCaseList(stmt)
}

// parseCaseList parses stmt's cases in braces, as parseCases does once it
// has entered their level of nesting.
func (p *Parser) parseCaseList(stmt *ast.Switch) []diag.Diagnostic {
	if ok, diags := p.expect(lexer.LCurl); !ok {
		return diags
	}

	var first *ast.CaseClause
	for p.peek().Kind() != lexer.RCurl && p.peek().Kind() != lexer.EOF {
		cl, diags := p.parseCaseClause(stmt.IsTypeSwitch())
		if cl == nil {
			return diags
		}
		if cl.Default && first != nil {
			return []diag.Diagnostic{{
				Severity: diag.Error,
				Message:  "multiple defaults in switch",
				Span:     cl.Span,
				Related: []diag.Diagnostic{{
					Severity: diag.Note,
					Message:  "first default here",
					Span:     first.Span,
				}},
			}}
		}
		if cl.Default {
			first = cl
		}
		if b := stmt.Bind; b != nil {
			cl.Var = &ast.Var{Name: b.Name, NameSpan: b.NameSpan, Span: b.Span}
		}
		stmt.Cases = append(stmt.Cases, cl)
	}

	if ok, diags := p.expect(lexer.RCurl); !ok {
		return diags
	}
	return nil
}

// parseCaseClause parses one clause of a switch: case, one or more values
// separated by commas, and a colon, or default and a colon; then the
// statements of its body, up to the next case, default or closing brace.
// In a type switch, typeCases says so, and each value is a type.
func (p *Parser) parseCaseClause(typeCases bool) (*ast.CaseClause, []diag.Diagnostic) {
	tk := p.peek()
	if !isKeyword(tk, lexer.Case) && !isKeyword(tk, lexer.Default) {
		return nil, []diag.Diagnostic{p.diagInvalidToken("case or default", tk)}
	}
	p.next()

	cl := &ast.CaseClause{
		Span:    p.span(tk),
		Default: tk.Value() == lexer.Default,
		Body:    make([]ast.Stmt, 0),
	}
	for !cl.Default {
		var value ast.Expr
		var diags []diag.Diagnostic
		if typeCases {
			value, diags = p.parseType()
		} else {
			value, diags = p.parseExpr(1)
		}
		if value == nil {
			return nil, diags
		}
		cl.Values = append(cl.Values, value)
		if p.peek().Kind() != lexer.Comma {
			break
		}
		p.next()
	}
	if ok, diags := p.expect(lexer.Colon); !ok {
		return nil, diags
	}
	cl.Span.End = p.peekPrev().OffsetEnd()

	cl.Body = p.parseStmts(true)
	if p.tooDeep != nil {
		return nil, []diag.Diagnostic{*p.tooDeep}
	}
	return cl, nil
}

// atClauseEnd reports whether the next token ends a case clause's body:
// the next case or default, the switch's closing brace, or the end of the
// file, which the closing brace's check then reports.
func (p *Parser) atClauseEnd() bool {
	tk := p.peek()
	return tk.Kind() == lexer.RCurl || tk.Kind() == lexer.EOF ||
		isKeyword(tk, lexer.Case) || isKeyword(tk, lexer.Default)
}

// isKeyword reports whether tk is the keyword kw.
func isKeyword(tk *lexer.Token, kw lexer.KeywordType) bool {
	return tk.Kind() == lexer.Keyword && tk.Value() == kw
}

// parseDefer parses a defer: the keyword, then a block, which may hold any
// statements. Anything else after the keyword is an error at that token,
// a single statement included: the braces show that the deferred code
// runs, and reads its variables, when the block holding the defer is left,
// not where the defer is written.
func (p *Parser) parseDefer() (*ast.Defer, []diag.Diagnostic) {
	tk := p.next() // the defer keyword

	if p.peek().Kind() != lexer.LCurl {
		return nil, []diag.Diagnostic{p.diagNoDeferBlock(p.peek())}
	}
	stmt := &ast.Defer{Span: p.span(tk)}
	body, diags := p.parseBlock()
	if body == nil {
		return nil, diags
	}
	stmt.Body = body
	if ok, semiDiags := p.expect(lexer.Semi); !ok && p.peek().Kind() != lexer.RCurl {
		return nil, semiDiags
	}
	stmt.Span.End = p.lastEnd()
	return stmt, nil
}

// diagNoDeferBlock reports tk, the token after a defer keyword, for not
// opening a block. It spans tk, or at the end of the file the point after
// the keyword.
func (p *Parser) diagNoDeferBlock(tk *lexer.Token) diag.Diagnostic {
	const msg = "defer must be followed by a block: defer { ... }"
	if tk.Kind() == lexer.EOF {
		end := p.lastEnd()
		return p.diag(diag.Error, msg, end, end)
	}
	return p.diag(diag.Error, msg, tk.OffsetStart(), tk.OffsetEnd())
}

// parseFor parses a loop: for, then its header, then the body. The header is
// empty when the body's opening brace comes straight after for, a
// condition, or three clauses separated by semicolons.
func (p *Parser) parseFor() (*ast.For, []diag.Diagnostic) {
	tk := p.next() // the for keyword

	loop := &ast.For{Span: p.span(tk)}

	if p.peek().Kind() != lexer.LCurl {
		restore := p.allowLits(false)
		diags := p.parseForHeader(loop)
		restore()
		if diags == nil {
			diags = p.headerLit("an if, for or range")
		}
		if diags != nil {
			return nil, diags
		}
	}

	body, bodyDiags := p.parseBlock()
	if body == nil {
		return nil, bodyDiags
	}
	loop.Body = body
	loop.Span.End = p.peekPrev().OffsetEnd()

	if ok, semiDiags := p.expect(lexer.Semi); !ok && p.peek().Kind() != lexer.RCurl {
		return nil, semiDiags
	}
	return loop, nil
}

// atRange reports whether the for keyword next is the start of a range
// loop: followed by range, or by a list of names, each perhaps after mut,
// then := and range. mut is not allowed there, but recognizing it lets
// parseForRange say so rather than report the range keyword as unexpected.
func (p *Parser) atRange() bool {
	isRange := func(tk *lexer.Token) bool {
		return tk.Kind() == lexer.Keyword && tk.Value() == lexer.Range
	}
	if isRange(p.peekAt(1)) {
		return true
	}
	k := 1
	for {
		if tk := p.peekAt(k); tk.Kind() == lexer.Keyword && tk.Value() == lexer.Mut {
			k++
		}
		if p.peekAt(k).Kind() != lexer.Ident {
			return false
		}
		k++
		if p.peekAt(k).Kind() != lexer.Comma {
			break
		}
		k++
	}
	return p.peekAt(k).Kind() == lexer.Define && isRange(p.peekAt(k+1))
}

// parseForRange parses for i := range x { ... }, for i, v := range x
// { ... } or for range x { ... }. The variables are immutable and have no
// type or value written; the type checker gives them their types. A name
// written with mut, or more than two names, is reported, after the ranged
// expression is parsed so the second error can cover it.
func (p *Parser) parseForRange() (*ast.ForRange, []diag.Diagnostic) {
	tk := p.next() // the for keyword

	loop := &ast.ForRange{Span: p.span(tk)}

	names, diags := p.parseRangeNames()
	if diags != nil {
		return nil, diags
	}
	p.next() // the range keyword

	restore := p.allowLits(false)
	x, diags := p.parseExpr(1)
	restore()
	if x == nil {
		return nil, diags
	}
	loop.X = x
	if varDiags := rangeVars(loop, names); varDiags != nil {
		return nil, varDiags
	}
	if litDiags := p.headerLit("an if, for or range"); litDiags != nil {
		return nil, litDiags
	}

	body, diags := p.parseBlock()
	if body == nil {
		return nil, diags
	}
	loop.Body = body
	loop.Span.End = p.peekPrev().OffsetEnd()

	if ok, semiDiags := p.expect(lexer.Semi); !ok && p.peek().Kind() != lexer.RCurl {
		return nil, semiDiags
	}
	return loop, nil
}

// parseRangeNames parses the names of a range loop up to and including its
// :=, each with the mut written before it, if any. atRange has seen that
// they are names, each perhaps after mut, separated by commas and ended by
// := and range.
func (p *Parser) parseRangeNames() ([]multiItem, []diag.Diagnostic) {
	var names []multiItem
	for p.peek().Kind() != lexer.Keyword || p.peek().Value() != lexer.Range {
		start := p.peek()
		mutable := start.Kind() == lexer.Keyword && start.Value() == lexer.Mut
		if mutable {
			p.next()
		}
		name, diags := p.parseIdent("variable name")
		if name == nil {
			return nil, diags
		}
		names = append(names, multiItem{
			target:  name,
			span:    ast.Span{File: p.fileID, Start: start.OffsetStart(), End: name.Span.End},
			mutable: mutable,
		})
		p.next() // the comma, or the := token
	}
	return names, nil
}

// rangeVars turns the names written before := in a range loop into its
// index and element variables, reporting a name written with mut, since
// both are immutable, and a third name, since range gives at most two
// values.
func rangeVars(loop *ast.ForRange, names []multiItem) []diag.Diagnostic {
	for _, item := range names {
		if item.mutable {
			return []diag.Diagnostic{{
				Severity: diag.Error,
				Message:  "cannot use mut on a range loop variable",
				Span:     item.span,
			}}
		}
	}
	if len(names) > 2 {
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message: fmt.Sprintf(
				"assignment mismatch: %d variables but range gives at most 2 values",
				len(names),
			),
			Span: ast.Span{
				File:  names[0].span.File,
				Start: names[0].span.Start,
				End:   loop.X.Spanned().End,
			},
		}}
	}
	vars := make([]*ast.Var, len(names))
	for i, item := range names {
		name := item.target.(*ast.Ident)
		vars[i] = &ast.Var{Name: name.Name, NameSpan: name.Span, Span: name.Span}
	}
	if len(vars) > 0 {
		loop.Var = vars[0]
	}
	if len(vars) > 1 {
		loop.Elem = vars[1]
	}
	return nil
}

// parseForHeader parses what comes between for and the body's opening brace
// into loop. A header that starts with a semicolon, a declaration, an
// assignment, or an increment or decrement has three clauses. One that starts with an expression is
// a condition, unless a semicolon follows it, in which case it was a call
// used as the init statement.
func (p *Parser) parseForHeader(loop *ast.For) []diag.Diagnostic {
	switch {
	case p.peek().Kind() == lexer.Semi:
		return p.parseForClauses(loop)
	case p.atDeclOrUpdate():
		init, diags := p.parseSimpleStmt()
		if init == nil {
			return diags
		}
		loop.Init = init
		return p.parseForClauses(loop)
	}

	cond, diags := p.parseExpr(1)
	if cond == nil {
		return diags
	}
	if call, ok := cond.(*ast.Call); ok && p.peek().Kind() == lexer.Semi {
		loop.Init = &ast.ExprStmt{X: call, Span: call.Span}
		return p.parseForClauses(loop)
	}
	loop.Cond = cond
	return nil
}

// atDeclOrUpdate reports whether the next tokens start a simple statement
// that cannot also be an expression: a declaration, an assignment, or an
// increment or decrement.
func (p *Parser) atDeclOrUpdate() bool {
	if p.peek().Kind() == lexer.Keyword {
		return p.atSimpleStmt() && !p.atMutCall()
	}
	if p.peek().Kind() != lexer.Ident {
		return false
	}
	switch after := p.afterTarget(0); after.Kind() {
	case lexer.Define, lexer.Inc, lexer.Dec, lexer.Comma:
		return true
	default:
		_, compound := compoundOps[after.Kind()]
		return compound || after.Kind() == lexer.Assign
	}
}

// parseForClauses parses the rest of a three-clause header once its init
// statement, if any, is in loop: the semicolon after it, an optional
// condition, a second semicolon, and an optional post statement, which may
// not declare a variable.
func (p *Parser) parseForClauses(loop *ast.For) []diag.Diagnostic {
	if ok, diags := p.expect(lexer.Semi); !ok {
		return diags
	}

	if p.peek().Kind() != lexer.Semi {
		cond, diags := p.parseExpr(1)
		if cond == nil {
			return diags
		}
		loop.Cond = cond
	}
	if ok, diags := p.expect(lexer.Semi); !ok {
		return diags
	}

	if p.peek().Kind() == lexer.LCurl {
		return nil
	}
	post, diags := p.parseSimpleStmt()
	if post == nil {
		return diags
	}
	switch post.(type) {
	case *ast.Var, *ast.MultiVar:
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "cannot declare a variable in a for loop's post statement",
			Span:     post.Spanned(),
		}}
	}
	loop.Post = post
	return nil
}

// parseJump parses break or continue, whose node the caller has built from
// the keyword. Nothing may follow the keyword: there are no labels.
func (p *Parser) parseJump(stmt ast.Stmt) (ast.Stmt, []diag.Diagnostic) {
	p.next() // the keyword

	if ok, semiDiags := p.expect(lexer.Semi); !ok && p.peek().Kind() != lexer.RCurl {
		return nil, semiDiags
	}
	return stmt, nil
}

func (p *Parser) parseReturn() (*ast.Return, []diag.Diagnostic) {
	tk := p.next() // the return keyword

	ret := &ast.Return{Span: p.span(tk)}

	// Semicolon insertion puts a Semi after a bare return, so anything else
	// here is the first of the returned expressions, separated by commas.
	if p.peek().Kind() != lexer.Semi && p.peek().Kind() != lexer.RCurl {
		for {
			val, valDiags := p.parseExpr(1)
			if val == nil {
				return nil, valDiags
			}
			ret.Values = append(ret.Values, val)

			if p.peek().Kind() != lexer.Comma {
				break
			}
			p.next()
		}
	}

	if ok, semiDiags := p.expect(lexer.Semi); !ok && p.peek().Kind() != lexer.RCurl {
		return nil, semiDiags
	}

	ret.Span.End = p.lastEnd()

	return ret, nil
}

// precedence returns the binding power of an infix operator, or 0 if the token
// is not one. Higher binds tighter, following Go's ordering.
func precedence(kind lexer.TokenKind) int {
	switch kind {
	case lexer.Mul, lexer.Div, lexer.Mod,
		lexer.ShiftLeft, lexer.ShiftRight, lexer.BitAnd, lexer.BitAndNot:
		return 5
	case lexer.Add, lexer.Sub, lexer.BitOr, lexer.BitXor:
		return 4
	case lexer.Equal,
		lexer.NotEqual,
		lexer.Less,
		lexer.LessEqual,
		lexer.Greater,
		lexer.GreaterEqual:
		return 3
	case lexer.LogicalAnd:
		return 2
	case lexer.LogicalOr:
		return 1
	default:
		return 0
	}
}

var binaryOps = map[lexer.TokenKind]ast.BinaryOp{
	lexer.Add:          ast.Add,
	lexer.Sub:          ast.Sub,
	lexer.Mul:          ast.Mul,
	lexer.Div:          ast.Div,
	lexer.Mod:          ast.Rem,
	lexer.Equal:        ast.Eq,
	lexer.NotEqual:     ast.Ne,
	lexer.Less:         ast.Lt,
	lexer.LessEqual:    ast.Le,
	lexer.Greater:      ast.Gt,
	lexer.GreaterEqual: ast.Ge,
	lexer.LogicalAnd:   ast.And,
	lexer.LogicalOr:    ast.Or,
	lexer.BitAnd:       ast.BitAnd,
	lexer.BitOr:        ast.BitOr,
	lexer.BitXor:       ast.BitXor,
	lexer.BitAndNot:    ast.AndNot,
	lexer.ShiftLeft:    ast.Shl,
	lexer.ShiftRight:   ast.Shr,
}

// parseExpr parses an expression whose operators bind at least as tightly as
// minPrec, by precedence climbing. Operators are left-associative, which is
// what parsing the right side at minPrec+1 achieves.
func (p *Parser) parseExpr(minPrec int) (ast.Expr, []diag.Diagnostic) {
	left, leftDiags := p.parseOperand()
	if left == nil {
		return nil, leftDiags
	}

	for {
		prec := precedence(p.peek().Kind())
		if prec == 0 || prec < minPrec {
			return left, nil
		}

		op := p.next()

		right, rightDiags := p.parseExpr(prec + 1)
		if right == nil {
			return nil, rightDiags
		}

		left = &ast.Binary{
			Op: binaryOps[op.Kind()],
			X:  left,
			Y:  right,
			Span: ast.Span{
				File:  p.fileID,
				Start: p.extent(left).Start,
				End:   p.extent(right).End,
			},
		}
	}
}

// parseOperand parses an expression with no infix operator at its root: a
// literal, a name, a call, a parenthesised expression, a prefix operator
// and its operand, or mut and an operand, which is only meaningful as a
// call's argument; the type checker reports it anywhere else; own and an
// operand, likewise; or a list
// type, which only make's first argument may be (see parseListOperand).
func (p *Parser) parseOperand() (ast.Expr, []diag.Diagnostic) {
	switch tk := p.peek(); {
	case tk.Kind() == lexer.LogicalNot:
		return p.parseUnary(ast.Not)
	case tk.Kind() == lexer.Sub:
		return p.parseUnary(ast.Neg)
	case tk.Kind() == lexer.BitXor:
		return p.parseUnary(ast.Complement)
	case isKeyword(tk, lexer.Mut):
		return p.parseMutArg()
	case isKeyword(tk, lexer.Own):
		return p.parseOwnArg()
	case isKeyword(tk, lexer.List):
		return p.parseListOperand()
	case isKeyword(tk, lexer.Map):
		return p.parseTypeOperand(p.parseMapType)
	case isKeyword(tk, lexer.Func):
		return nil, []diag.Diagnostic{
			{
				Severity: diag.Error,
				Message:  "function literals are not supported; declare the function and use its name as a value",
				Span:     p.span(tk),
			},
		}
	}

	x, diags := p.parsePrimary()
	if x == nil {
		return nil, diags
	}
	return p.parsePostfix(x)
}

// parseMutArg parses mut and the operand after it, a mut argument, or,
// when the operand is a method call, mut x.m(args), the call of a mut
// receiver: mut then covers the receiver, what comes before the last
// .m(, so mut w.out.write(s) passes w.out as mut. A call written in
// parentheses, mut (x.m()), is not one: mut covers the whole call, which
// is not a place.
func (p *Parser) parseMutArg() (ast.Expr, []diag.Diagnostic) {
	if diags := p.nest(p.peek()); diags != nil {
		return nil, diags
	}
	defer p.unnest()
	kw := p.next()
	x, diags := p.parseOperand()
	if x == nil {
		return nil, diags
	}
	if call, ok := x.(*ast.Call); ok && x.Spanned() == p.extent(x) {
		if sel, isMethod := call.Fn.(*ast.Selector); isMethod {
			if _, already := sel.X.(*ast.MutArg); !already {
				sel.X = &ast.MutArg{
					X: sel.X,
					Span: ast.Span{
						File:  p.fileID,
						Start: kw.OffsetStart(),
						End:   p.extent(sel.X).End,
					},
				}
				sel.Span.Start, call.Span.Start = kw.OffsetStart(), kw.OffsetStart()
				return call, nil
			}
		}
	}
	return &ast.MutArg{
		X:    x,
		Span: ast.Span{File: p.fileID, Start: kw.OffsetStart(), End: p.extent(x).End},
	}, nil
}

// parseOwnArg parses own and the operand after it, an own argument. Unlike
// mut, own never covers a method call's receiver: own x.m() is own before
// the call, which the type checker reports, as it reports own before
// anything but a variable's name.
func (p *Parser) parseOwnArg() (ast.Expr, []diag.Diagnostic) {
	if diags := p.nest(p.peek()); diags != nil {
		return nil, diags
	}
	defer p.unnest()
	kw := p.next()
	x, diags := p.parseOperand()
	if x == nil {
		return nil, diags
	}
	return &ast.OwnArg{
		X:    x,
		Span: ast.Span{File: p.fileID, Start: kw.OffsetStart(), End: p.extent(x).End},
	}, nil
}

// parsePrimary parses an operand that indexes and fields can follow: a
// parenthesized expression, a call, an array literal, a struct literal, a
// name, or a literal. A name followed by an opening brace starts a struct
// literal, except where noLit says the brace opens a body.
func (p *Parser) parsePrimary() (ast.Expr, []diag.Diagnostic) {
	switch p.peek().Kind() {
	case lexer.LParen:
		return p.parseParenExpr()
	case lexer.LBrack:
		return p.parseCompositeLit()
	}

	n := p.nameEnd()
	if p.peek().Kind() == lexer.Ident && p.peekAt(n+1).Kind() == lexer.LCurl && !p.noLit {
		return p.parseStructLit()
	}
	if p.atTypeArgs(n) {
		return nil, p.typeArgsError(n)
	}

	if p.peek().Kind() == lexer.Ident && p.peekAt(n+1).Kind() == lexer.LParen {
		return p.parseCallOperand()
	}

	if n > 0 {
		return p.parseQualified(), nil
	}
	return p.parseLiteral()
}

// parseCallOperand parses a call, which the caller has seen is next, as
// an operand. A nil *ast.Call would be a non-nil ast.Expr, so an error is
// returned as an untyped nil.
func (p *Parser) parseCallOperand() (ast.Expr, []diag.Diagnostic) {
	call, diags := p.parseCall()
	if call == nil {
		return nil, diags
	}
	return call, nil
}

// parseStructLit parses a struct literal: its type's name, qualified or
// not, which the caller has seen is next, and then its body.
func (p *Parser) parseStructLit() (ast.Expr, []diag.Diagnostic) {
	typ, _ := p.parseName("type name")
	lit, diags := p.parseLitBody(typ, typ.Span.Start)
	if lit == nil {
		return nil, diags
	}
	return lit, nil
}

// parsePostfix parses any number of indexes, slices, field selections and
// type assertions after x, x[i].f[j][k:], each an element, a slice, a field
// of what comes before it, or a question about which type it holds,
// x.(T) or x.(type).
func (p *Parser) parsePostfix(x ast.Expr) (ast.Expr, []diag.Diagnostic) {
	for {
		var diags []diag.Diagnostic
		switch p.peek().Kind() {
		case lexer.LBrack:
			x, diags = p.parseIndex(x)
		case lexer.Period:
			if p.peekNext().Kind() == lexer.LParen {
				x, diags = p.parseTypeAssert(x)
			} else {
				x, diags = p.parseSelector(x)
			}
		default:
			return x, nil
		}
		if diags != nil {
			return nil, diags
		}
	}
}

// parseIndex parses one index after x, x[i], or one slice, x[i:j], whose
// bounds may each be left out, x[:j], x[i:] or x[:]; see parseSlice.
func (p *Parser) parseIndex(x ast.Expr) (ast.Expr, []diag.Diagnostic) {
	if diags := p.nest(p.peek()); diags != nil {
		return nil, diags
	}
	defer p.unnest()
	p.next() // the opening bracket
	restore := p.allowLits(true)
	defer restore()
	var index ast.Expr
	if p.peek().Kind() != lexer.Colon {
		var diags []diag.Diagnostic
		if index, diags = p.parseExpr(1); index == nil {
			return nil, diags
		}
	}
	if p.peek().Kind() == lexer.Colon {
		return p.parseSlice(x, index)
	}
	if ok, diags := p.expect(lexer.RBrack); !ok {
		return nil, diags
	}
	return &ast.Index{
		X:     x,
		Index: index,
		Span: ast.Span{
			File:  p.fileID,
			Start: p.extent(x).Start,
			End:   p.peekPrev().OffsetEnd(),
		},
	}, nil
}

// parseSlice parses the rest of a slice after x, x[lo:hi], from the colon
// that follows lo, nil when it is left out. hi may be left out too. A
// second colon would start Go's three-index form, x[i:j:k], which Tack
// does not have, and is reported there.
func (p *Parser) parseSlice(x, lo ast.Expr) (ast.Expr, []diag.Diagnostic) {
	colon := p.span(p.next())
	var hi ast.Expr
	if k := p.peek().Kind(); k != lexer.RBrack && k != lexer.Colon {
		var diags []diag.Diagnostic
		if hi, diags = p.parseExpr(1); hi == nil {
			return nil, diags
		}
	}
	if tk := p.peek(); tk.Kind() == lexer.Colon {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "a slice has at most two bounds, x[i:j]; there is no three-index form",
			Span:     p.span(tk),
		}}
	}
	if ok, diags := p.expect(lexer.RBrack); !ok {
		return nil, diags
	}
	return &ast.Slice{
		X:         x,
		Lo:        lo,
		Hi:        hi,
		ColonSpan: colon,
		Span: ast.Span{
			File:  p.fileID,
			Start: p.extent(x).Start,
			End:   p.peekPrev().OffsetEnd(),
		},
	}, nil
}

// parseSelector parses one field selection after x, x.f, or, when an
// opening parenthesis follows the name, a method call on x, x.m(args),
// whose callee is the selection (see ast.Call).
func (p *Parser) parseSelector(x ast.Expr) (ast.Expr, []diag.Diagnostic) {
	p.next() // the period
	name, diags := p.parseIdent("field name")
	if name == nil {
		return nil, diags
	}
	sel := &ast.Selector{
		X:        x,
		Name:     name.Name,
		NameSpan: name.Span,
		Span:     ast.Span{File: p.fileID, Start: p.extent(x).Start, End: name.Span.End},
	}
	if p.peek().Kind() != lexer.LParen {
		return sel, nil
	}
	call, diags := p.parseArgs(sel, sel.Span)
	if call == nil {
		return nil, diags
	}
	return call, nil
}

// parseTypeAssert parses a type assertion after x, from the period: x.(T),
// with any type T, or x.(type). Which of them is allowed where, and which
// types, is the type checker's to judge.
func (p *Parser) parseTypeAssert(x ast.Expr) (ast.Expr, []diag.Diagnostic) {
	p.next() // the period
	p.next() // the opening parenthesis
	assert := &ast.TypeAssert{X: x}
	if isKeyword(p.peek(), lexer.Type) {
		p.next()
	} else {
		restore := p.allowLits(true)
		typ, diags := p.parseType()
		restore()
		if typ == nil {
			return nil, diags
		}
		assert.TypeExpr = typ
	}
	if ok, diags := p.expect(lexer.RParen); !ok {
		return nil, diags
	}
	assert.Span = ast.Span{File: p.fileID, Start: p.extent(x).Start, End: p.peekPrev().OffsetEnd()}
	return assert, nil
}

// parseCompositeLit parses an array literal: an array type, which may use
// [...] for its length, then its elements in braces. A view type, []T, is
// followed by its conversion's argument in parentheses, []uint8(s), or by
// braces, a literal of a view, which the type checker reports.
func (p *Parser) parseCompositeLit() (ast.Expr, []diag.Diagnostic) {
	typ, diags := p.parseArrayType(true)
	if typ == nil {
		return nil, diags
	}
	if view, ok := typ.(*ast.ViewType); ok && p.peek().Kind() == lexer.LParen {
		call, argDiags := p.parseArgs(view, view.Span)
		if call == nil {
			return nil, argDiags
		}
		return call, nil
	}
	lit, diags := p.parseLitBody(typ, typ.Spanned().Start)
	if lit == nil {
		return nil, diags
	}
	return lit, nil
}

// parseLitBody parses the braces of a composite literal and the elements in
// them, separated by commas, with a comma allowed after the last, as a
// literal written over several lines needs. An element may have a key,
// key: value, as a struct literal's do. An element or value that is itself
// only braces is a literal whose type is left out, typ is nil, and comes
// from the enclosing literal. The literal spans from start.
func (p *Parser) parseLitBody(typ ast.Expr, start int) (*ast.CompositeLit, []diag.Diagnostic) {
	if diags := p.nestAt(lexer.LCurl); diags != nil {
		return nil, diags
	}
	defer p.unnest()
	if ok, diags := p.expect(lexer.LCurl); !ok {
		return nil, diags
	}
	defer p.allowLits(true)()
	lit := &ast.CompositeLit{TypeExpr: typ, Elems: make([]ast.Expr, 0)}

	for p.peek().Kind() != lexer.RCurl {
		if len(lit.Elems) > 0 {
			if ok, diags := p.expect(lexer.Comma); !ok {
				return nil, diags
			}
			if p.peek().Kind() == lexer.RCurl {
				break
			}
		}

		elem, diags := p.parseLitElem()
		if elem == nil {
			return nil, diags
		}
		lit.Elems = append(lit.Elems, elem)
	}

	p.next() // the closing brace
	lit.Span = ast.Span{File: p.fileID, Start: start, End: p.peekPrev().OffsetEnd()}
	return lit, nil
}

// parseLitElem parses one element of a composite literal: a value, or a
// key, a colon and a value. A key in parentheses is marked as such, since
// the parentheses leave no node behind.
func (p *Parser) parseLitElem() (ast.Expr, []diag.Diagnostic) {
	key, diags := p.parseLitValue()
	if key == nil || p.peek().Kind() != lexer.Colon {
		return key, diags
	}
	p.next() // the colon
	value, diags := p.parseLitValue()
	if value == nil {
		return nil, diags
	}
	return &ast.KeyValue{
		Key:       key,
		Value:     value,
		Span:      ast.Span{File: p.fileID, Start: p.extent(key).Start, End: p.extent(value).End},
		KeyParens: p.extent(key) != key.Spanned(),
	}, nil
}

// parseLitValue parses a value in a composite literal: an expression, or
// braces alone, a literal whose type is left out. On an error the value is
// an untyped nil, as in parseStmt.
func (p *Parser) parseLitValue() (ast.Expr, []diag.Diagnostic) {
	tk := p.peek()
	if tk.Kind() != lexer.LCurl {
		return p.parseExpr(1)
	}
	nested, diags := p.parseLitBody(nil, tk.OffsetStart())
	if nested == nil {
		return nil, diags
	}
	return nested, nil
}

// parseArrayType parses [n]T. In an array literal, literal is true and the
// length may be [...], left for the elements to give. An empty length, [],
// makes it a view type, []T, which parseViewType parses.
func (p *Parser) parseArrayType(literal bool) (ast.Expr, []diag.Diagnostic) {
	if diags := p.nest(p.peek()); diags != nil {
		return nil, diags
	}
	defer p.unnest()
	open := p.next() // the opening bracket
	typ := &ast.ArrayType{Span: p.span(open)}

	switch tk := p.peek(); tk.Kind() {
	case lexer.RBrack:
		return p.parseViewType(open)
	case lexer.Ellipsis:
		p.next()
		if !literal {
			return nil, []diag.Diagnostic{{
				Severity: diag.Error,
				Message:  "[...] can only be used in an array literal",
				Span:     ast.Span{File: p.fileID, Start: open.OffsetStart(), End: tk.OffsetEnd()},
			}}
		}
	default:
		restore := p.allowLits(true)
		n, diags := p.parseExpr(1)
		restore()
		if n == nil {
			return nil, diags
		}
		typ.Len = n
	}
	if ok, diags := p.expect(lexer.RBrack); !ok {
		return nil, diags
	}

	elem, diags := p.parseType()
	if elem == nil {
		return nil, diags
	}
	typ.Elem = elem
	typ.Span.End = elem.Spanned().End
	return typ, nil
}

// parseViewType parses the rest of a view type, []T, whose opening bracket,
// open, has been read and whose closing one is next.
func (p *Parser) parseViewType(open *lexer.Token) (ast.Expr, []diag.Diagnostic) {
	p.next() // the closing bracket
	elem, diags := p.parseType()
	if elem == nil {
		return nil, diags
	}
	return &ast.ViewType{
		Elem: elem,
		Span: ast.Span{File: p.fileID, Start: open.OffsetStart(), End: elem.Spanned().End},
	}, nil
}

// parseListType parses a list type, list[T], whose keyword is next.
func (p *Parser) parseListType() (ast.Expr, []diag.Diagnostic) {
	if diags := p.nest(p.peek()); diags != nil {
		return nil, diags
	}
	defer p.unnest()
	kw := p.next()
	if ok, diags := p.expect(lexer.LBrack); !ok {
		return nil, diags
	}
	elem, diags := p.parseType()
	if elem == nil {
		return nil, diags
	}
	if ok, diags := p.expect(lexer.RBrack); !ok {
		return nil, diags
	}
	return &ast.ListType{
		Elem: elem,
		Span: ast.Span{File: p.fileID, Start: kw.OffsetStart(), End: p.peekPrev().OffsetEnd()},
	}, nil
}

// parseMapType parses a map type, map[K]V, whose keyword is next.
func (p *Parser) parseMapType() (ast.Expr, []diag.Diagnostic) {
	if diags := p.nest(p.peek()); diags != nil {
		return nil, diags
	}
	defer p.unnest()
	kw := p.next()
	if ok, diags := p.expect(lexer.LBrack); !ok {
		return nil, diags
	}
	key, diags := p.parseType()
	if key == nil {
		return nil, diags
	}
	if ok, closeDiags := p.expect(lexer.RBrack); !ok {
		return nil, closeDiags
	}
	value, diags := p.parseType()
	if value == nil {
		return nil, diags
	}
	return &ast.MapType{
		Key:   key,
		Value: value,
		Span:  ast.Span{File: p.fileID, Start: kw.OffsetStart(), End: value.Spanned().End},
	}, nil
}

// parseFuncType parses a function type, func(A, mut B) R, whose keyword
// is next: its parameters' types in parentheses, each with mut before it
// when it is mut, separated by commas, and its results as a function's
// are written (see parseResults), unless a name and a type follow, which
// start the next parameter of a list it is a parameter's type in. A
// parameter has no name, and no variadic parameter, ...T, is allowed;
// each is reported.
func (p *Parser) parseFuncType() (ast.Expr, []diag.Diagnostic) {
	if diags := p.nest(p.peek()); diags != nil {
		return nil, diags
	}
	defer p.unnest()
	kw := p.next()
	if ok, diags := p.expect(lexer.LParen); !ok {
		return nil, diags
	}
	var params []*ast.Param
	for p.peek().Kind() != lexer.RParen {
		param, diags := p.parseFuncTypeParam()
		if param == nil {
			return nil, diags
		}
		params = append(params, param)
		if p.peek().Kind() != lexer.Comma {
			break
		}
		p.next()
	}
	if ok, diags := p.expect(lexer.RParen); !ok {
		return nil, diags
	}
	var results []ast.Expr
	// A name followed by a type is the next parameter of a parameter
	// list, f func() n int64, its comma left out, as commas between
	// parameters may be, so the function type returns nothing.
	if p.peek().Kind() != lexer.Ident || !p.startsType(p.peekAt(1)) {
		var diags []diag.Diagnostic
		if results, diags = p.parseResults(); diags != nil {
			return nil, diags
		}
	}
	return &ast.FuncType{
		Params:  params,
		Results: results,
		Span:    ast.Span{File: p.fileID, Start: kw.OffsetStart(), End: p.lastEnd()},
	}, nil
}

// parseFuncTypeParam parses one parameter of a function type: its type,
// with mut before it for a mut parameter, or own for an own one. A name
// before the type, which a function type's parameters do not have, and a
// ..., which would make the parameter variadic, are reported.
func (p *Parser) parseFuncTypeParam() (*ast.Param, []diag.Diagnostic) {
	param := &ast.Param{}
	if diags := p.parseParamMode(param); diags != nil {
		return nil, diags
	}
	if tk := p.peek(); tk.Kind() == lexer.Ellipsis {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "a function type cannot have a variadic parameter",
			Span:     p.span(tk),
		}}
	}
	typ, diags := p.parseType()
	if typ == nil {
		return nil, diags
	}
	// A basic type's name is never a parameter's, so one followed by a
	// type is a comma left out, which expect reports.
	if name, isName := typ.(*ast.Ident); isName && name.Pkg == "" && !isBasicType(name.Name) {
		if tk := p.peek(); p.startsType(tk) || tk.Kind() == lexer.Ellipsis {
			return nil, []diag.Diagnostic{{
				Severity: diag.Error,
				Message: fmt.Sprintf(
					"a function type's parameters have no names; write only the type, not %s",
					name.Name,
				),
				Span: name.Span,
			}}
		}
	}
	param.TypeExpr = typ
	param.Span = ast.Span{File: p.fileID, Start: typ.Spanned().Start, End: typ.Spanned().End}
	startAtMode(param)
	return param, nil
}

// isBasicType reports whether name is a basic type's, such as int64,
// which no declaration may take as its name.
func isBasicType(name string) bool {
	_, ok := types.Lookup(name)
	return ok
}

// parseListOperand parses a list type written where an operand is, which
// make's first argument is, make(list[int64], c_allocator), or a literal
// of one, list[int64]{...}, which the type checker reports, as it does a
// list type anywhere else a value is wanted.
func (p *Parser) parseListOperand() (ast.Expr, []diag.Diagnostic) {
	return p.parseTypeOperand(p.parseListType)
}

// parseTypeOperand parses a list or a map type written where an operand
// is, with parse, and a literal of it, as parseListOperand describes.
func (p *Parser) parseTypeOperand(
	parse func() (ast.Expr, []diag.Diagnostic),
) (ast.Expr, []diag.Diagnostic) {
	typ, diags := parse()
	if typ == nil {
		return nil, diags
	}
	if p.peek().Kind() != lexer.LCurl || p.noLit {
		return typ, nil
	}
	lit, diags := p.parseLitBody(typ, typ.Spanned().Start)
	if lit == nil {
		return nil, diags
	}
	return lit, nil
}

// parseUnary parses a prefix operator, ! or -, and the operand after it. The
// operand is parsed as an operand, not a full expression, so a prefix
// operator binds tighter than any infix one: !a == b is (!a) == b. A - right
// before an integer literal makes one negative literal instead, so the
// smallest int, whose digits alone do not fit, can be written.
func (p *Parser) parseUnary(op ast.UnaryOp) (ast.Expr, []diag.Diagnostic) {
	if diags := p.nest(p.peek()); diags != nil {
		return nil, diags
	}
	defer p.unnest()
	tk := p.next()

	if op == ast.Neg && p.peek().Kind() == lexer.Int {
		return p.intLit("-", tk.OffsetStart(), p.next())
	}

	x, diags := p.parseOperand()
	if x == nil {
		return nil, diags
	}
	return &ast.Unary{
		Op:   op,
		X:    x,
		Span: ast.Span{File: p.fileID, Start: tk.OffsetStart(), End: p.extent(x).End},
	}, nil
}

// intLit decodes the integer literal digits, with sign ("" or "-") in front,
// into a literal spanning from start through the digits. The lexer hands
// over the digits undecoded, so this is the first place a literal too large
// to represent can be noticed.
func (p *Parser) intLit(sign string, start int, digits *lexer.Token) (ast.Expr, []diag.Diagnostic) {
	text := sign + lexer.ValueChecked[string](digits)
	span := ast.Span{File: p.fileID, Start: start, End: digits.OffsetEnd()}

	val, ok := constant.Parse(lexer.ValueChecked[string](digits), sign == "-")
	if !ok {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("integer literal %s is out of range", text),
			Span:     span,
		}}
	}
	return &ast.IntLit{Value: val, Span: span}, nil
}

// parseParenExpr parses a parenthesized expression and returns the inner
// expression; the parentheses leave no node of their own, and are recorded
// in p.parens instead, so the expression around them can span them.
func (p *Parser) parseParenExpr() (ast.Expr, []diag.Diagnostic) {
	if diags := p.nest(p.peek()); diags != nil {
		return nil, diags
	}
	defer p.unnest()
	lparen := p.next()

	restore := p.allowLits(true)
	inner, innerDiags := p.parseExpr(1)
	restore()
	if inner == nil {
		return nil, innerDiags
	}

	if ok, rparenDiags := p.expect(lexer.RParen); !ok {
		return nil, rparenDiags
	}
	// An enclosing pair is parsed after this one, so ((a)) ends up with
	// the outer pair's span.
	p.parens[inner] = ast.Span{
		File:  p.fileID,
		Start: lparen.OffsetStart(),
		End:   p.peekPrev().OffsetEnd(),
	}
	return inner, nil
}

// extent returns the source range x was written in as an operand: its span,
// widened to its outermost parentheses if it was written in some. An
// expression built from operands spans from its first operand's extent to
// its last's, so (a) == (b) covers both parentheses.
func (p *Parser) extent(x ast.Expr) ast.Span {
	if span, ok := p.parens[x]; ok {
		return span
	}
	return x.Spanned()
}

// parseLiteral parses a single-token operand: a string, integer, character
// or bool literal, noerror, c_allocator, or a reference to a parameter or
// variable.
func (p *Parser) parseLiteral() (ast.Expr, []diag.Diagnostic) {
	tk := p.next()
	switch tk.Kind() {
	case lexer.Str:
		return &ast.StrLit{Value: lexer.ValueChecked[string](tk), Span: p.span(tk)}, nil
	case lexer.Ident:
		// A parameter reference. The resolver binds it to its declaration.
		return &ast.Ident{Name: lexer.ValueChecked[string](tk), Span: p.span(tk)}, nil
	case lexer.Int:
		return p.intLit("", tk.OffsetStart(), tk)
	case lexer.Char:
		// The lexer gives a char literal only when it holds exactly one byte.
		return &ast.CharLit{Value: lexer.ValueChecked[string](tk)[0], Span: p.span(tk)}, nil
	case lexer.Keyword:
		if lit := p.keywordLiteral(tk); lit != nil {
			return lit, nil
		}
		return nil, []diag.Diagnostic{p.diagInvalidToken("value", tk)}
	default:
		return nil, []diag.Diagnostic{p.diagInvalidToken("value", tk)}
	}
}

// keywordLiteral returns the literal the keyword tk is, true, false,
// noerror or c_allocator, or nil for any other keyword.
func (p *Parser) keywordLiteral(tk *lexer.Token) ast.Expr {
	switch tk.Value() {
	case lexer.True:
		return &ast.BoolLit{Value: true, Span: p.span(tk)}
	case lexer.False:
		return &ast.BoolLit{Value: false, Span: p.span(tk)}
	case lexer.NoError:
		return &ast.NoErrorLit{Span: p.span(tk)}
	case lexer.CAllocator:
		return &ast.CAllocatorLit{Span: p.span(tk)}
	default:
		return nil
	}
}

func (p *Parser) diagInvalidToken(expected string, tk *lexer.Token) diag.Diagnostic {
	found := tk.Kind().String()
	if val, ok := tk.Value().(string); ok {
		found = val
	}

	if val, ok := tk.Value().(interface{ String() string }); ok {
		found = val.String()
	}

	msg := fmt.Sprintf("expected '%s' found '%s'", expected, found)

	at := tk.OffsetStart()
	if tk.Kind() == lexer.EOF || p.peek().Kind() == lexer.EOF {
		if pre := p.peekPrev(); pre != nil {
			at = pre.OffsetEnd()
		}
	}

	return p.diag(diag.Error, msg, at, at)
}

func (p *Parser) diag(sev diag.Severity, msg string, offStart, offEnd int) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: sev,
		Message:  msg,
		Span:     ast.Span{File: p.fileID, Start: offStart, End: offEnd},
	}
}

func (p *Parser) span(tk *lexer.Token) ast.Span {
	return ast.Span{File: p.fileID, Start: tk.OffsetStart(), End: tk.OffsetEnd()}
}

// allowLits sets whether a name followed by an opening brace starts a
// struct literal, and returns a function that puts back the setting it
// replaced.
func (p *Parser) allowLits(allowed bool) (restore func()) {
	old := p.noLit
	p.noLit = !allowed
	return func() { p.noLit = old }
}

// headerLit reports a struct literal written without parentheses in the
// header of an if, a for, a range loop or a switch, which ends the header
// at its type name, so that its braces are taken for the body. A body
// cannot start with a name and a colon, so that is how it is recognized.
// where names the kind of header, as the message words it.
func (p *Parser) headerLit(where string) []diag.Diagnostic {
	tk := p.peek()
	if tk.Kind() != lexer.LCurl || p.peekAt(1).Kind() != lexer.Ident ||
		p.peekAt(2).Kind() != lexer.Colon {
		return nil
	}
	return []diag.Diagnostic{{
		Severity: diag.Error,
		Message:  "a struct literal in " + where + " header must be in parentheses",
		Span:     p.span(tk),
	}}
}
