package parser

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/source"
)

// Parser turns one file's token stream into an AST. It stops at the first
// syntax error, so a file yields either a tree or diagnostics, never both.
type Parser struct {
	file *ast.File

	// parens holds, for each expression written in parentheses, the span
	// of its outermost pair. A parenthesised expression leaves no node of
	// its own, so this is how an expression built around one, such as
	// (a) == (b), spans its parentheses too. See extent.
	parens map[ast.Expr]ast.Span

	fileID   source.FileID
	tokens   []lexer.Token // every token but the comments
	comments []lexer.Token // the comment tokens, in source order
	index    int
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
		fileID: fileID,
		tokens: make([]lexer.Token, 0, len(tokens)),
		parens: make(map[ast.Expr]ast.Span),
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
// declaration up to EOF. On the first syntax error it returns a nil file and
// that error's diagnostics.
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

	pkg, pkgDiags := p.parsePkg()
	if len(pkgDiags) > 0 {
		return nil, pkgDiags
	}

	p.file.Package = pkg.Name
	p.file.Span = pkg.Span

	for p.peek().Kind() != lexer.EOF {
		dec, decDiags := p.parseDecl()
		if len(decDiags) > 0 {
			return nil, decDiags
		}
		p.file.Decls = append(p.file.Decls, dec)
	}

	p.file.Span.End = p.peek().OffsetEnd()

	return p.file, nil
}

func (p *Parser) parsePkg() (*ast.Ident, []diag.Diagnostic) {
	tk := p.next()

	switch {
	case tk.Kind() == lexer.Keyword && tk.Value() == lexer.Package:
		pkg, pkgDiags := p.parseIdent("package name")
		if pkgDiags != nil {
			return nil, pkgDiags
		}
		pkg.Span = p.span(tk)

		if ok, semiDiags := p.expect(lexer.Semi); !ok {
			return nil, semiDiags
		}
		return pkg, nil
	default:
		return nil, []diag.Diagnostic{p.diagInvalidToken("package", tk)}
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

func (p *Parser) parseDecl() (ast.Decl, []diag.Diagnostic) {
	tk := p.peek()
	switch {
	case tk.Value() == lexer.Func:
		return p.parseFunc()
	case tk.Value() == lexer.Var || tk.Value() == lexer.Mut:
		return p.parsePackageVar()
	default:
		return nil, []diag.Diagnostic{p.diagInvalidToken("func", tk)}
	}
}

func (p *Parser) parseFunc() (*ast.Func, []diag.Diagnostic) {
	tk := p.peek()
	if tk.Value() != lexer.Func {
		return nil, []diag.Diagnostic{p.diagInvalidToken("func", tk)}
	}

	// advance past the func keyword
	p.next()

	ident, identDiags := p.parseIdent("function name")
	if ident == nil {
		return nil, identDiags
	}

	fn := &ast.Func{
		Name:     ident.Name,
		NameSpan: ident.Span,
		Span:     p.span(tk),
		Body:     make([]ast.Stmt, 0),
	}

	params, paramsDiags := p.parseParams()
	if paramsDiags != nil {
		return nil, paramsDiags
	}
	fn.Params = params

	results, resultDiags := p.parseResults()
	if resultDiags != nil {
		return nil, resultDiags
	}
	fn.Results = results

	if bodyDiags := p.parseFuncBody(fn); bodyDiags != nil {
		return nil, bodyDiags
	}

	fn.Span.End = p.lastEnd()

	return fn, nil
}

// parseParams parses a parenthesized parameter list, parentheses included.
// Commas between parameters are optional.
func (p *Parser) parseParams() ([]*ast.Param, []diag.Diagnostic) {
	if ok, lparenDiags := p.expect(lexer.LParen); !ok {
		return nil, lparenDiags
	}

	var params []*ast.Param
	for p.peek().Kind() != lexer.RParen {
		param, paramDiags := p.parseParam()
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
	switch p.peek().Kind() {
	case lexer.Ident, lexer.LBrack:
		typ, diags := p.parseType()
		if typ == nil {
			return nil, diags
		}
		return []ast.Expr{typ}, nil
	case lexer.LParen:
		p.next()
	default:
		return nil, nil
	}

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
// function declared without one.
func (p *Parser) parseFuncBody(fn *ast.Func) []diag.Diagnostic {
	switch p.next().Kind() {
	case lexer.LCurl:
		for p.peek().Kind() != lexer.RCurl && p.peek().Kind() != lexer.EOF {
			stmt, stmtDiags := p.parseStmt()
			if len(stmtDiags) > 0 {
				return stmtDiags
			}
			fn.Body = append(fn.Body, stmt)
		}
		if ok, seqDiags := p.expectSequence(lexer.RCurl, lexer.Semi); !ok {
			return seqDiags
		}
		return nil
	case lexer.Semi:
		return nil
	default:
		return []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "missing function body",
			Span:     p.span(p.peek()),
		}}
	}
}

func (p *Parser) parseParam() (*ast.Param, []diag.Diagnostic) {
	name, nameDiags := p.parseIdent("parameter name")
	if name == nil {
		return nil, nameDiags
	}

	typ, typDiags := p.parseType()
	if typ == nil {
		return nil, typDiags
	}

	return &ast.Param{
		Name:     name.Name,
		NameSpan: name.Span,
		TypeExpr: typ,
		Span: ast.Span{
			File:  p.fileID,
			Start: name.Span.Start,
			End:   typ.Spanned().End,
		},
	}, nil
}

func (p *Parser) parseType() (ast.Expr, []diag.Diagnostic) {
	// Consuming the ellipsis and carrying on would make f(x ...int) and
	// f(x int) the same tree, so nothing downstream could tell that variadics
	// had been asked for and quietly not delivered.
	if tk := p.peek(); tk.Kind() == lexer.Ellipsis {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "variadic parameters are not supported",
			Span:     p.span(tk),
		}}
	}

	if p.peek().Kind() == lexer.LBrack {
		return p.parseArrayType(false)
	}
	typ, typDiags := p.parseIdent("type name")
	if typ == nil {
		return nil, typDiags
	}
	return typ, nil
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
// it: a declaration, an assignment, an increment or decrement, or a call.
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

// parseKeywordStmt parses a statement that starts with keyword tk.
func (p *Parser) parseKeywordStmt(tk *lexer.Token) (ast.Stmt, []diag.Diagnostic) {
	switch tk.Value() {
	case lexer.If:
		return p.parseIf()
	case lexer.Return:
		return p.parseReturn()
	case lexer.For:
		if p.atRange() {
			return p.parseForRange()
		}
		return p.parseFor()
	case lexer.Break:
		return p.parseJump(&ast.Break{Span: p.span(tk)})
	case lexer.Continue:
		return p.parseJump(&ast.Continue{Span: p.span(tk)})
	default:
		return nil, []diag.Diagnostic{p.diagInvalidToken("statement", tk)}
	}
}

// parseIdentStmt parses a statement that starts with the name tk: a short
// variable declaration, an assignment (plain or compound), an increment or
// decrement, or a call.
func (p *Parser) parseIdentStmt(tk *lexer.Token) (ast.Stmt, []diag.Diagnostic) {
	indexed := p.peekNext().Kind() == lexer.LBrack
	switch after := p.afterIndexes(0); after.Kind() {
	case lexer.Define:
		if indexed {
			return nil, []diag.Diagnostic{p.diagInvalidToken("=", after)}
		}
		return p.parseShortVarDecl()
	case lexer.Assign,
		lexer.AddAssign,
		lexer.SubAssign,
		lexer.MulAssign,
		lexer.DivAssign,
		lexer.ModAssign:
		return p.parseAssign()
	case lexer.LParen:
		if indexed {
			return nil, []diag.Diagnostic{p.diagInvalidToken("statement", tk)}
		}
		return p.parseFuncCall()
	case lexer.Inc, lexer.Dec:
		return p.parseIncDec()
	default:
		return nil, []diag.Diagnostic{p.diagInvalidToken("statement", tk)}
	}
}

// afterIndexes returns the token after the name n places ahead and every
// bracketed index that follows it, a[i][j], so a statement that starts with
// an element can be told apart by what comes after it. The brackets are
// matched, so an index may hold brackets of its own.
func (p *Parser) afterIndexes(n int) *lexer.Token {
	k := n + 1
	for p.peekAt(k).Kind() == lexer.LBrack {
		depth := 0
		for {
			switch p.peekAt(k).Kind() {
			case lexer.LBrack:
				depth++
			case lexer.RBrack:
				depth--
			case lexer.EOF:
				return p.peekAt(k)
			}
			k++
			if depth == 0 {
				break
			}
		}
	}
	return p.peekAt(k)
}

// parseTarget parses what an assignment, increment or decrement writes: a
// name, followed by any number of indexes, a[i][j].
func (p *Parser) parseTarget() (ast.Expr, []diag.Diagnostic) {
	name, diags := p.parseIdent("variable name")
	if name == nil {
		return nil, diags
	}
	return p.parseIndexes(name)
}

// atMulti reports whether the next tokens start a statement binding several
// results: a name, or mut and a name, followed by a comma.
func (p *Parser) atMulti() bool {
	tk := p.peek()
	if tk.Kind() == lexer.Keyword && tk.Value() == lexer.Mut {
		return p.peekNext().Kind() == lexer.Ident && p.afterIndexes(1).Kind() == lexer.Comma
	}
	return tk.Kind() == lexer.Ident && p.afterIndexes(0).Kind() == lexer.Comma
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
	if kind := p.peek().
		Kind(); kind == lexer.Ident || kind == lexer.Ellipsis ||
		kind == lexer.LBrack {
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

// compoundOps maps each compound assignment operator to the arithmetic
// operator it applies. A plain = is absent, so it maps to ast.InvalidOp.
var compoundOps = map[lexer.TokenKind]ast.BinaryOp{
	lexer.AddAssign: ast.Add,
	lexer.SubAssign: ast.Sub,
	lexer.MulAssign: ast.Mul,
	lexer.DivAssign: ast.Div,
	lexer.ModAssign: ast.Rem,
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
	ident, identDiags := p.parseIdent("function name")
	if ident == nil {
		return nil, identDiags
	}

	call := &ast.Call{
		Fn:   ident,
		Args: make([]ast.Expr, 0),
		Span: ident.Span,
	}

	if ok, lparenDiags := p.expect(lexer.LParen); !ok {
		return nil, lparenDiags
	}

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
	}

	if ok, rparenDiags := p.expect(lexer.RParen); !ok {
		return nil, rparenDiags
	}

	call.Span.End = p.peekPrev().OffsetEnd()

	return call, nil
}

// parseBlock parses a brace-delimited statement list.
func (p *Parser) parseBlock() ([]ast.Stmt, []diag.Diagnostic) {
	if ok, lcurlDiags := p.expect(lexer.LCurl); !ok {
		return nil, lcurlDiags
	}

	body := make([]ast.Stmt, 0)
	for p.peek().Kind() != lexer.RCurl && p.peek().Kind() != lexer.EOF {
		stmt, stmtDiags := p.parseStmt()
		if stmt == nil {
			return nil, stmtDiags
		}
		body = append(body, stmt)
	}

	if ok, rcurlDiags := p.expect(lexer.RCurl); !ok {
		return nil, rcurlDiags
	}
	return body, nil
}

func (p *Parser) parseIf() (*ast.If, []diag.Diagnostic) {
	tk := p.next() // the if keyword

	stmt := &ast.If{Span: p.span(tk)}

	cond, condDiags := p.parseExpr(1)
	if cond == nil {
		return nil, condDiags
	}
	stmt.Cond = cond

	then, thenDiags := p.parseBlock()
	if then == nil {
		return nil, thenDiags
	}
	stmt.Then = then

	els, terminated, elseDiags := p.parseElse()
	if elseDiags != nil {
		return nil, elseDiags
	}
	stmt.Else = els

	if !terminated {
		if ok, semiDiags := p.expect(lexer.Semi); !ok && p.peek().Kind() != lexer.RCurl {
			return nil, semiDiags
		}
	}

	stmt.Span.End = p.lastEnd()

	return stmt, nil
}

// parseElse parses an optional else branch, returning nil statements when
// there is none. A chained "else if" is parsed as a nested if, which consumes
// the statement terminator for the whole chain; terminated reports that case.
func (p *Parser) parseElse() (els []ast.Stmt, terminated bool, diags []diag.Diagnostic) {
	if p.peek().Kind() != lexer.Keyword || p.peek().Value() != lexer.Else {
		return nil, false, nil
	}
	p.next()

	if p.peek().Kind() == lexer.Keyword && p.peek().Value() == lexer.If {
		nested, nestedDiags := p.parseIf()
		if nested == nil {
			return nil, false, nestedDiags
		}
		return []ast.Stmt{nested}, true, nil
	}

	block, blockDiags := p.parseBlock()
	if block == nil {
		return nil, false, blockDiags
	}
	return block, false, nil
}

// parseFor parses a loop: for, then its header, then the body. The header is
// empty when the body's opening brace comes straight after for, a
// condition, or three clauses separated by semicolons.
func (p *Parser) parseFor() (*ast.For, []diag.Diagnostic) {
	tk := p.next() // the for keyword

	loop := &ast.For{Span: p.span(tk)}

	if p.peek().Kind() != lexer.LCurl {
		if diags := p.parseForHeader(loop); diags != nil {
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

	x, diags := p.parseExpr(1)
	if x == nil {
		return nil, diags
	}
	loop.X = x
	if varDiags := rangeVars(loop, names); varDiags != nil {
		return nil, varDiags
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
		return p.atSimpleStmt()
	}
	if p.peek().Kind() != lexer.Ident {
		return false
	}
	switch after := p.afterIndexes(0); after.Kind() {
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
	case lexer.Mul, lexer.Div, lexer.Mod:
		return 5
	case lexer.Add, lexer.Sub:
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
// literal, a name, a call, a parenthesised expression, or a prefix operator
// and its operand.
func (p *Parser) parseOperand() (ast.Expr, []diag.Diagnostic) {
	switch p.peek().Kind() {
	case lexer.LogicalNot:
		return p.parseUnary(ast.Not)
	case lexer.Sub:
		return p.parseUnary(ast.Neg)
	}

	x, diags := p.parsePrimary()
	if x == nil {
		return nil, diags
	}
	return p.parseIndexes(x)
}

// parsePrimary parses an operand that indexes can follow: a parenthesized
// expression, a call, an array literal, a name, or a literal.
func (p *Parser) parsePrimary() (ast.Expr, []diag.Diagnostic) {
	switch p.peek().Kind() {
	case lexer.LParen:
		return p.parseParenExpr()
	case lexer.LBrack:
		return p.parseCompositeLit()
	}

	if p.peek().Kind() == lexer.Ident && p.peekNext().Kind() == lexer.LParen {
		// A nil *ast.Call would be a non-nil ast.Expr, so an error is
		// returned as an untyped nil.
		call, callDiags := p.parseCall()
		if call == nil {
			return nil, callDiags
		}
		return call, nil
	}

	return p.parseLiteral()
}

// parseIndexes parses any number of indexes after x, x[i][j], each an
// element of what comes before it.
func (p *Parser) parseIndexes(x ast.Expr) (ast.Expr, []diag.Diagnostic) {
	for p.peek().Kind() == lexer.LBrack {
		p.next()
		index, diags := p.parseExpr(1)
		if index == nil {
			return nil, diags
		}
		if ok, diags := p.expect(lexer.RBrack); !ok {
			return nil, diags
		}
		x = &ast.Index{
			X:     x,
			Index: index,
			Span: ast.Span{
				File:  p.fileID,
				Start: p.extent(x).Start,
				End:   p.peekPrev().OffsetEnd(),
			},
		}
	}
	return x, nil
}

// parseCompositeLit parses an array literal: an array type, which may use
// [...] for its length, then its elements in braces.
func (p *Parser) parseCompositeLit() (ast.Expr, []diag.Diagnostic) {
	typ, diags := p.parseArrayType(true)
	if typ == nil {
		return nil, diags
	}
	lit, diags := p.parseLitBody(typ, typ.Spanned().Start)
	if lit == nil {
		return nil, diags
	}
	return lit, nil
}

// parseLitBody parses the braces of an array literal and the elements in
// them, separated by commas, with a comma allowed after the last, as a
// literal written over several lines needs. An element that is itself only
// braces is a literal whose type is left out, typ is nil, and comes from the
// enclosing literal. The literal spans from start.
func (p *Parser) parseLitBody(typ ast.Expr, start int) (*ast.CompositeLit, []diag.Diagnostic) {
	if ok, diags := p.expect(lexer.LCurl); !ok {
		return nil, diags
	}
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

		var (
			elem  ast.Expr
			diags []diag.Diagnostic
		)
		if tk := p.peek(); tk.Kind() == lexer.LCurl {
			nested, nestedDiags := p.parseLitBody(nil, tk.OffsetStart())
			if nested != nil {
				elem = nested
			}
			diags = nestedDiags
		} else {
			elem, diags = p.parseExpr(1)
		}
		if elem == nil {
			return nil, diags
		}
		lit.Elems = append(lit.Elems, elem)
	}

	p.next() // the closing brace
	lit.Span = ast.Span{File: p.fileID, Start: start, End: p.peekPrev().OffsetEnd()}
	return lit, nil
}

// parseArrayType parses [n]T. In an array literal, literal is true and the
// length may be [...], left for the elements to give. An empty length, [],
// would be a slice, which Tack does not have yet.
func (p *Parser) parseArrayType(literal bool) (ast.Expr, []diag.Diagnostic) {
	open := p.next() // the opening bracket
	typ := &ast.ArrayType{Span: p.span(open)}

	switch tk := p.peek(); tk.Kind() {
	case lexer.Ellipsis:
		p.next()
		if !literal {
			return nil, []diag.Diagnostic{{
				Severity: diag.Error,
				Message:  "[...] can only be used in an array literal",
				Span:     ast.Span{File: p.fileID, Start: open.OffsetStart(), End: tk.OffsetEnd()},
			}}
		}
	case lexer.RBrack:
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  "slices are not supported yet",
			Span:     ast.Span{File: p.fileID, Start: open.OffsetStart(), End: tk.OffsetEnd()},
		}}
	default:
		n, diags := p.parseExpr(1)
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

// parseUnary parses a prefix operator, ! or -, and the operand after it. The
// operand is parsed as an operand, not a full expression, so a prefix
// operator binds tighter than any infix one: !a == b is (!a) == b. A - right
// before an integer literal makes one negative literal instead, so the
// smallest int, whose digits alone do not fit, can be written.
func (p *Parser) parseUnary(op ast.UnaryOp) (ast.Expr, []diag.Diagnostic) {
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
	lparen := p.next()

	inner, innerDiags := p.parseExpr(1)
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
// or bool literal, or a reference to a parameter or variable.
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
		switch tk.Value() {
		case lexer.True:
			return &ast.BoolLit{Value: true, Span: p.span(tk)}, nil
		case lexer.False:
			return &ast.BoolLit{Value: false, Span: p.span(tk)}, nil
		}
		return nil, []diag.Diagnostic{p.diagInvalidToken("value", tk)}
	default:
		return nil, []diag.Diagnostic{p.diagInvalidToken("value", tk)}
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
