package parser

import (
	"fmt"
	"strconv"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/source"
)

// Parser turns one file's token stream into an AST. It stops at the first
// syntax error, so a file yields either a tree or diagnostics, never both.
type Parser struct {
	fileID source.FileID
	file   *ast.File
	tokens []lexer.Token
	index  int
}

// NewParser returns a parser over tokens, which must come from a lexer that
// ended them with an EOF token. Spans it creates are tagged with fileID.
func NewParser(fileID source.FileID, tokens []lexer.Token) *Parser {
	return &Parser{fileID: fileID, tokens: tokens}
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

// Parse parses the whole file: the package clause, then every top-level
// declaration up to EOF. On the first syntax error it returns a nil file and
// that error's diagnostics.
func (p *Parser) Parse() (*ast.File, []diag.Diagnostic) {
	if len(p.tokens) == 0 {
		panic("token list is empty")
	}

	p.file = ast.NewFile()

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
	for tk.Kind() != lexer.EOF {
		switch {
		case tk.Value() == lexer.Func:
			return p.parseFunc()
		case tk.Value() == lexer.Var || tk.Value() == lexer.Mut:
			return p.parsePackageVar()
		case tk.Kind() == lexer.Comment:
			p.parseComment()
			tk = p.peek()
		default:
			return nil, []diag.Diagnostic{p.diagInvalidToken("func", tk)}
		}
	}
	return nil, nil
}

func (p *Parser) parseComment() {
	for {
		if tk := p.peek(); tk.Kind() == lexer.Comment {
			p.file.Comments = append(p.file.Comments, ast.Comment{
				Value: lexer.ValueChecked[string](tk),
				Span:  p.span(tk),
			})
			p.next()
		} else {
			break
		}
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

	// An identifier here is the declared result type; a brace or semicolon
	// means the function returns nothing.
	if p.peek().Kind() == lexer.Ident {
		result, resultDiags := p.parseType()
		if result == nil {
			return nil, resultDiags
		}
		fn.Result = result
	}

	if bodyDiags := p.parseFuncBody(fn); bodyDiags != nil {
		return nil, bodyDiags
	}

	fn.Span.End = p.peekPrev().OffsetEnd()

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
		Type:     typ,
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
	switch tk.Kind() {
	case lexer.EOF:
		return nil, nil
	case lexer.Keyword:
		stmt, diags = p.parseKeywordStmt(tk)
	case lexer.Ident:
		stmt, diags = p.parseIdentStmt(tk)
	default:
		diags = []diag.Diagnostic{p.diagInvalidToken("statement", tk)}
	}

	if len(diags) > 0 {
		return nil, diags
	}
	return stmt, nil
}

// parseKeywordStmt parses a statement that starts with keyword tk.
func (p *Parser) parseKeywordStmt(tk *lexer.Token) (ast.Stmt, []diag.Diagnostic) {
	switch tk.Value() {
	case lexer.If:
		return p.parseIf()
	case lexer.Return:
		return p.parseReturn()
	case lexer.For:
		return p.parseFor()
	case lexer.Break:
		return p.parseJump(&ast.Break{Span: p.span(tk)})
	case lexer.Continue:
		return p.parseJump(&ast.Continue{Span: p.span(tk)})
	case lexer.Var, lexer.Mut:
		return p.parseVarDecl()
	default:
		return nil, []diag.Diagnostic{p.diagInvalidToken("statement", tk)}
	}
}

// parseIdentStmt parses a statement that starts with the name tk: a short
// variable declaration, an assignment (plain or compound), an increment or
// decrement, or a call.
func (p *Parser) parseIdentStmt(tk *lexer.Token) (ast.Stmt, []diag.Diagnostic) {
	switch p.peekNext().Kind() {
	case lexer.Define:
		return p.parseShortVarDecl()
	case lexer.Assign,
		lexer.AddAssign,
		lexer.SubAssign,
		lexer.MulAssign,
		lexer.DivAssign,
		lexer.ModAssign:
		return p.parseAssign()
	case lexer.LParen:
		return p.parseFuncCall()
	case lexer.Inc, lexer.Dec:
		return p.parseIncDec()
	default:
		return nil, []diag.Diagnostic{p.diagInvalidToken("statement", tk)}
	}
}

// parseIncDec parses name++ or name--.
func (p *Parser) parseIncDec() (*ast.IncDec, []diag.Diagnostic) {
	target, _ := p.parseIdent("variable name")
	op := p.next() // ++ or --

	s := &ast.IncDec{
		Target: target,
		Dec:    op.Kind() == lexer.Dec,
		Span:   ast.Span{File: p.fileID, Start: target.Span.Start, End: op.OffsetEnd()},
	}
	return s, p.endStmt(&s.Span)
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
		return v, p.endStmt(&v.Span)
	}

	if diags := p.parseVarTypeAndValue(v); diags != nil {
		return nil, diags
	}
	return v, p.endStmt(&v.Span)
}

// parseVarTypeAndValue parses what follows a declared name: a type, = and a
// value, or both, into v. Neither is a syntax error.
func (p *Parser) parseVarTypeAndValue(v *ast.Var) []diag.Diagnostic {
	if kind := p.peek().Kind(); kind == lexer.Ident || kind == lexer.Ellipsis {
		typ, typDiags := p.parseType()
		if typ == nil {
			return typDiags
		}
		v.Type = typ
	}

	if p.peek().Kind() == lexer.Assign {
		p.next()
		value, valueDiags := p.parseExpr(1)
		if value == nil {
			return valueDiags
		}
		v.Value = value
	}

	if v.Type == nil && v.Value == nil {
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
	return v, nil
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

	return v, p.endStmt(&v.Span)
}

// parseAssign parses name = value, or a compound assignment such as
// name += value.
func (p *Parser) parseAssign() (*ast.Assign, []diag.Diagnostic) {
	target, _ := p.parseIdent("variable name")
	op := p.next() // = or a compound assignment operator

	a := &ast.Assign{Target: target, Op: compoundOps[op.Kind()], Span: target.Span}

	value, valueDiags := p.parseExpr(1)
	if value == nil {
		return nil, valueDiags
	}
	a.Value = value

	return a, p.endStmt(&a.Span)
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
// before a closing brace, and extends span to cover it. It returns the
// diagnostics for anything else found there.
func (p *Parser) endStmt(span *ast.Span) []diag.Diagnostic {
	if ok, semiDiags := p.expect(lexer.Semi); !ok && p.peek().Kind() != lexer.RCurl {
		return semiDiags
	}
	span.End = p.peekPrev().OffsetEnd()
	return nil
}

func (p *Parser) parseFuncCall() (*ast.ExprStmt, []diag.Diagnostic) {
	call, callDiags := p.parseCall()
	if call == nil {
		return nil, callDiags
	}

	stmt := &ast.ExprStmt{X: call, Span: call.Span}

	if ok, semiDiags := p.expect(lexer.Semi); !ok && p.peek().Kind() != lexer.RCurl {
		return nil, semiDiags
	}

	stmt.Span.End = p.peekPrev().OffsetEnd()

	return stmt, nil
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

	stmt.Span.End = p.peekPrev().OffsetEnd()

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

// parseFor parses a loop: for, then a condition unless the body's opening
// brace comes straight after, then the body.
func (p *Parser) parseFor() (*ast.For, []diag.Diagnostic) {
	tk := p.next() // the for keyword

	loop := &ast.For{Span: p.span(tk)}

	if p.peek().Kind() != lexer.LCurl {
		cond, condDiags := p.parseExpr(1)
		if cond == nil {
			return nil, condDiags
		}
		loop.Cond = cond
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
	// here is the returned expression.
	if p.peek().Kind() != lexer.Semi && p.peek().Kind() != lexer.RCurl {
		val, valDiags := p.parseExpr(1)
		if val == nil {
			return nil, valDiags
		}
		ret.Value = val
	}

	if ok, semiDiags := p.expect(lexer.Semi); !ok && p.peek().Kind() != lexer.RCurl {
		return nil, semiDiags
	}

	ret.Span.End = p.peekPrev().OffsetEnd()

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
				Start: left.Spanned().Start,
				End:   right.Spanned().End,
			},
		}
	}
}

// parseOperand parses an expression with no infix operator at its root: a
// literal, a name, a call, a parenthesised expression, or a prefix operator
// and its operand.
func (p *Parser) parseOperand() (ast.Expr, []diag.Diagnostic) {
	switch p.peek().Kind() {
	case lexer.LParen:
		return p.parseParenExpr()
	case lexer.LogicalNot:
		return p.parseUnary(ast.Not)
	case lexer.Sub:
		return p.parseUnary(ast.Neg)
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
		Span: ast.Span{File: p.fileID, Start: tk.OffsetStart(), End: x.Spanned().End},
	}, nil
}

// intLit decodes the integer literal digits, with sign ("" or "-") in front,
// into a literal spanning from start through the digits. The lexer hands
// over the digits undecoded, so this is the first place a literal too large
// to represent can be noticed.
func (p *Parser) intLit(sign string, start int, digits *lexer.Token) (ast.Expr, []diag.Diagnostic) {
	text := sign + lexer.ValueChecked[string](digits)
	span := ast.Span{File: p.fileID, Start: start, End: digits.OffsetEnd()}

	val, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return nil, []diag.Diagnostic{{
			Severity: diag.Error,
			Message:  fmt.Sprintf("integer literal %s out of range for int", text),
			Span:     span,
		}}
	}
	return &ast.IntLit{Value: val, Span: span}, nil
}

// parseParenExpr parses a parenthesized expression and returns the inner
// expression; the parentheses leave no node of their own.
func (p *Parser) parseParenExpr() (ast.Expr, []diag.Diagnostic) {
	p.next() // the opening parenthesis

	inner, innerDiags := p.parseExpr(1)
	if inner == nil {
		return nil, innerDiags
	}

	if ok, rparenDiags := p.expect(lexer.RParen); !ok {
		return nil, rparenDiags
	}
	return inner, nil
}

// parseLiteral parses a single-token operand: a string, integer or bool
// literal, or a reference to a parameter or variable.
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
	if val, ok := tk.Value().(string); ok && tk.Kind() != lexer.Comment {
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
