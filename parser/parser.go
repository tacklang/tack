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

func (p *Parser) parseStmt() (ast.Stmt, []diag.Diagnostic) {
	tk := p.peek()
	for tk.Kind() != lexer.EOF {
		switch {
		case tk.Kind() == lexer.Keyword && tk.Value() == lexer.If:
			return p.parseIf()
		case tk.Kind() == lexer.Keyword && tk.Value() == lexer.Return:
			return p.parseReturn()
		case tk.Kind() == lexer.Ident && p.peekNext().Kind() == lexer.LParen:
			return p.parseFuncCall()
		default:
			return nil, []diag.Diagnostic{p.diagInvalidToken("statement", tk)}
		}
	}
	return nil, nil
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
		return 3
	case lexer.Add, lexer.Sub:
		return 2
	case lexer.Equal,
		lexer.NotEqual,
		lexer.Less,
		lexer.LessEqual,
		lexer.Greater,
		lexer.GreaterEqual:
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
// literal, a parameter reference, a call, or a parenthesised expression.
func (p *Parser) parseOperand() (ast.Expr, []diag.Diagnostic) {
	if p.peek().Kind() == lexer.LParen {
		return p.parseParenExpr()
	}

	if p.peek().Kind() == lexer.Ident && p.peekNext().Kind() == lexer.LParen {
		return p.parseCall()
	}

	return p.parseLiteral()
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

// parseLiteral parses a single-token operand: a string or integer literal, or
// a parameter reference.
func (p *Parser) parseLiteral() (ast.Expr, []diag.Diagnostic) {
	tk := p.next()
	switch tk.Kind() {
	case lexer.Str:
		return &ast.StrLit{Value: lexer.ValueChecked[string](tk), Span: p.span(tk)}, nil
	case lexer.Ident:
		// A parameter reference. The resolver binds it to its declaration.
		return &ast.Ident{Name: lexer.ValueChecked[string](tk), Span: p.span(tk)}, nil
	case lexer.Int:
		// The lexer hands over the digits undecoded, so this is the first
		// place a literal too large to represent can be noticed.
		text := lexer.ValueChecked[string](tk)
		val, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, []diag.Diagnostic{{
				Severity: diag.Error,
				Message:  fmt.Sprintf("integer literal %s out of range for int", text),
				Span:     p.span(tk),
			}}
		}
		return &ast.IntLit{Value: val, Span: p.span(tk)}, nil
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
