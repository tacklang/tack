package parser

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
)

// Error recovery. A construct that hits a syntax error returns its
// diagnostics, as it always has, and stops where it is. Three places catch
// them and go on: the package clause and each import, each top-level
// declaration, and each statement of a block, a function's body, an if's
// or a loop's block, a defer's or a case clause's body. There the error is
// recorded (see record), the damaged text skipped (syncDecl, syncStmt),
// and a bad node, ast.BadDecl or ast.BadStmt, put in the tree where it
// was, so the rest of the file is parsed as if the damaged part were
// absent and each of its own errors is found too.
//
// A statement ends at the semicolon after it at its own depth of braces,
// counted from where it started, so a statement with blocks of its own is
// skipped whole, its blocks included; or at the closing brace of the
// block holding it, which is left for that block. An error inside a
// nested block is caught by that block, so only the innermost statement
// it damages is lost. A declaration ends where the next one starts: a
// line starting with func, type, pub, lib,
// extern or import, at any depth, since none of them starts a statement,
// or with var or mut outside every brace.
//
// A block stops at the start of such a line too, before func, pub, lib,
// extern or import, which no statement starts with: a block left
// unclosed, the most common error that would swallow the rest of the
// file, then reports its missing closing brace there, once, and the
// declaration after it is parsed as one.
//
// Each error is reported once. A construct's error is usually one
// diagnostic, and of the errors that start at one place, as two blocks
// left unclosed before one line give, only the first is kept. Past the
// limit (see Limit) the parser stops, after recording one error more than
// the limit, so a caller knows some were left out. Nesting past maxNesting
// ends the parse at once, with no tree, as it always has.

// Limit makes the parser stop once it has recorded more than n errors,
// the one past n saying there are more, or never, for n of 0, the default.
func (p *Parser) Limit(n int) {
	p.maxErrors = n
}

// record records diags, the errors of a construct that failed, leaving
// out any starting where one already recorded does (see recorded), and
// all of them once the parse has halted.
func (p *Parser) record(diags []diag.Diagnostic) {
	for _, d := range diags {
		if p.halted() || p.recorded(d) {
			continue
		}
		p.errs = append(p.errs, d)
	}
}

// recorded reports whether an error starting where d starts has been
// recorded: a second one there, such as the missing closing brace of a
// block an earlier error left at the end of the file, follows from the
// first.
func (p *Parser) recorded(d diag.Diagnostic) bool {
	for _, e := range p.errs {
		if e.Span.File == d.Span.File && e.Span.Start == d.Span.Start {
			return true
		}
	}
	return false
}

// halted reports whether the parse stops: it has recorded more errors than
// its limit, or the program nests too deeply.
func (p *Parser) halted() bool {
	return p.tooDeep != nil || p.maxErrors > 0 && len(p.errs) > p.maxErrors
}

// badDecl records diags, the errors of the declaration that started at
// the token start, skips to the start of the next one (see syncDecl), and
// returns the BadDecl standing for the text skipped.
func (p *Parser) badDecl(start int, diags []diag.Diagnostic) *ast.BadDecl {
	p.record(diags)
	p.unconsumeEOF()
	p.syncDecl(start)
	bad := &ast.BadDecl{Span: p.skipped(start)}
	bad.Name, bad.Method = p.declName(start)
	return bad
}

// declName returns the name the declaration skipped from the token start
// to the parser's was declaring, when it got that far, and whether it is
// a method's: the name after func, after a method's receiver, after type,
// var or mut, or after import, past any pub, lib or extern before them
// and an extern's C name. It returns "" when the tokens skipped hold no
// such name.
func (p *Parser) declName(start int) (string, bool) {
	i := p.pastModifiers(start)
	if i >= p.index || p.tokens[i].Kind() != lexer.Keyword {
		return "", false
	}
	method := false
	switch p.tokens[i].Value() {
	case lexer.Func:
		i++
		if i < p.index && p.tokens[i].Kind() == lexer.LParen {
			i, method = p.pastParens(i), true
		}
	case lexer.Type, lexer.Var, lexer.Mut, lexer.Import:
		i++
	default:
		return "", false
	}
	if i >= p.index || p.tokens[i].Kind() != lexer.Ident {
		return "", false
	}
	return lexer.ValueChecked[string](&p.tokens[i]), method
}

// pastModifiers returns the index of the first token from start, below the
// parser's index, that is none of the words a declaration may start with
// before its keyword: pub, lib, and extern with its C name, if written.
func (p *Parser) pastModifiers(start int) int {
	i := start
	for i < p.index && (isKeyword(&p.tokens[i], lexer.Pub) || isKeyword(&p.tokens[i], lexer.Lib)) {
		i++
	}
	if i < p.index && isKeyword(&p.tokens[i], lexer.Extern) {
		i++
		if i < p.index && p.tokens[i].Kind() == lexer.LParen {
			i = p.pastParens(i)
		}
	}
	return i
}

// pastParens returns the index of the token after the parenthesis that
// closes the one at the token open, or the parser's index when it is
// not closed before it.
func (p *Parser) pastParens(open int) int {
	depth := 0
	for i := open; i < p.index; i++ {
		switch p.tokens[i].Kind() {
		case lexer.LParen:
			depth++
		case lexer.RParen:
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return p.index
}

// badStmt records diags, the errors of the statement that started at the
// token start, skips to its end (see syncStmt), and returns the BadStmt
// standing for the text skipped, and whether the block holding it is
// broken (see syncStmt). inClause says the statement is in a case
// clause's body, which a case or a default also ends.
func (p *Parser) badStmt(start int, diags []diag.Diagnostic, inClause bool) (*ast.BadStmt, bool) {
	p.record(diags)
	p.unconsumeEOF()
	broken := p.syncStmt(start, inClause)
	return &ast.BadStmt{Span: p.skipped(start)}, broken
}

// unconsumeEOF moves the parser back to the EOF token when the construct
// that failed consumed it, so every loop still finds the end of the file.
func (p *Parser) unconsumeEOF() {
	p.index = min(p.index, len(p.tokens)-1)
}

// skipped returns the span from the token start through the last token
// consumed since, or the empty span at start's token when none was.
func (p *Parser) skipped(start int) ast.Span {
	first := &p.tokens[min(start, len(p.tokens)-1)]
	if p.index <= start {
		return ast.Span{File: p.fileID, Start: first.OffsetStart(), End: first.OffsetStart()}
	}
	return ast.Span{File: p.fileID, Start: first.OffsetStart(), End: p.lastEnd()}
}

// braceDepth returns the depth of braces from the token start to the
// token before the parser's, counting each { in and each } out, and moves
// the parser back to a } that closes the block start is in, should the
// failed construct have consumed one, so that block still ends there.
func (p *Parser) braceDepth(start int) int {
	depth := 0
	for i := start; i < p.index; i++ {
		switch p.tokens[i].Kind() {
		case lexer.LCurl:
			depth++
		case lexer.RCurl:
			if depth == 0 {
				p.index = i
				return 0
			}
			depth--
		}
	}
	return depth
}

// syncStmt skips the rest of the statement that started at the token
// start, as the comment at the top of the file says: through the
// semicolon that ends it at its own depth, or up to the closing brace of
// the block holding it, the start of a line that starts a declaration, a
// case or a default, for a statement of a case clause, or the end of the
// file. It always consumes at least one token, unless one of those is
// next, so a block's loop never parses the same token twice. It returns
// whether the block holding the statement is broken: it stopped at a line
// that starts a declaration with braces the statement opened still open,
// so that block's own closing brace is missing too, and its statements
// end there.
func (p *Parser) syncStmt(start int, inClause bool) bool {
	if p.semiInParens(start) {
		return false
	}
	depth := p.braceDepth(start)
	for {
		if stop, broken := p.stmtEnds(start, depth, inClause); stop {
			return broken
		}
		switch p.next().Kind() {
		case lexer.LCurl:
			depth++
		case lexer.RCurl:
			depth--
		case lexer.Semi:
			if depth == 0 {
				return false
			}
		}
	}
}

// semiInParens reports whether the statement that started at the token
// start ends at a semicolon the failed construct consumed with a
// parenthesis or a bracket it opened still open, outside any brace, as in
// print(n +; whose operand's error consumed the semicolon. No expression
// holds one there, so the statement ends at it, and the parser is moved
// to the token after it. A closing brace that ends the block before such
// a semicolon is left for braceDepth.
func (p *Parser) semiInParens(start int) bool {
	parens, braces := 0, 0
	for i := start; i < p.index; i++ {
		switch p.tokens[i].Kind() {
		case lexer.LParen, lexer.LBrack:
			parens++
		case lexer.RParen, lexer.RBrack:
			parens = max(parens-1, 0)
		case lexer.LCurl:
			braces++
		case lexer.RCurl:
			if braces == 0 {
				return false
			}
			braces--
		case lexer.Semi:
			if parens > 0 && braces == 0 {
				p.index = i + 1
				return true
			}
		}
	}
	return false
}

// stmtEnds reports whether the statement that started at the token start,
// being skipped with depth braces open, ends before the next token, and
// whether that leaves the block holding it broken (see syncStmt).
func (p *Parser) stmtEnds(start, depth int, inClause bool) (bool, bool) {
	tk := p.peek()
	switch {
	case tk.Kind() == lexer.EOF:
		return true, false
	case depth > 0:
		return p.index > start && p.atDeclLine(true), true
	case tk.Kind() == lexer.RCurl:
		return true, false
	case inClause && (isKeyword(tk, lexer.Case) || isKeyword(tk, lexer.Default)):
		return p.index > start, false
	default:
		return p.index > start && p.atDeclLine(true), false
	}
}

// syncDecl skips the rest of the declaration that started at the token
// start, to the start of a line that starts the next one, or the end of
// the file. It always consumes at least one token, unless the file ends.
func (p *Parser) syncDecl(start int) {
	depth := p.braceDepth(start)
	p.index = max(p.index, start)
	for p.peek().Kind() != lexer.EOF && (p.index <= start || !p.declStarts(depth)) {
		switch p.next().Kind() {
		case lexer.LCurl:
			depth++
		case lexer.RCurl:
			depth = max(depth-1, 0)
		}
	}
}

// declStarts reports whether the next token starts the line a declaration
// does, with depth braces open: a var or a mut one outside every brace, or
// one only a declaration starts (see atDeclLine).
func (p *Parser) declStarts(depth int) bool {
	if !p.atLineStart() {
		return false
	}
	tk := p.peek()
	if depth == 0 && (isKeyword(tk, lexer.Var) || isKeyword(tk, lexer.Mut)) {
		return true
	}
	return p.atDeclLine(depth > 0)
}

// atLineStart reports whether the next token starts a line: no token
// comes before it on its line (see lexer.Token.StartsLine).
func (p *Parser) atLineStart() bool {
	return p.peek().StartsLine()
}

// atDeclLine reports whether the next tokens start a line the way only a
// top-level declaration does: func and a name or a receiver's
// parenthesis, type and a name, pub or lib and func, var, mut, type or
// extern, extern and func or the parenthesis of its C name, or import and
// a name. A keyword misused as a name, extern := 1, starts no such line,
// and is left to the statement it is in to report. inside says the line
// is inside braces, where a func line counts only when it opens a body on
// the same line, as a function's declaration does and an interface's
// method, func f(), written by mistake, does not.
func (p *Parser) atDeclLine(inside bool) bool {
	tk, next := p.peek(), p.peekAt(1)
	if !p.atLineStart() || tk.Kind() != lexer.Keyword {
		return false
	}
	switch tk.Value() {
	case lexer.Func:
		return p.funcDeclLine(next, inside)
	case lexer.Type, lexer.Import:
		return next.Kind() == lexer.Ident
	case lexer.Pub, lexer.Lib:
		return visibleDeclKeyword(next)
	case lexer.Extern:
		return isKeyword(next, lexer.Func) || next.Kind() == lexer.LParen
	default:
		return false
	}
}

// funcDeclLine reports whether func, with next after it, starts a line as
// a function's declaration does: a name or a receiver's parenthesis next,
// and, inside braces, a body opened on the same line (see atDeclLine).
func (p *Parser) funcDeclLine(next *lexer.Token, inside bool) bool {
	if next.Kind() != lexer.Ident && next.Kind() != lexer.LParen {
		return false
	}
	return !inside || p.opensBodyOnLine()
}

// visibleDeclKeyword reports whether tk, after pub or lib, is a keyword a
// declaration they make visible starts with: func, var, mut, type or
// extern.
func visibleDeclKeyword(tk *lexer.Token) bool {
	if tk.Kind() != lexer.Keyword {
		return false
	}
	switch tk.Value() {
	case lexer.Func, lexer.Var, lexer.Mut, lexer.Type, lexer.Extern:
		return true
	default:
		return false
	}
}

// opensBodyOnLine reports whether an opening brace comes after the next
// token before the line it starts ends.
func (p *Parser) opensBodyOnLine() bool {
	for k := 1; ; k++ {
		tk := p.peekAt(k)
		switch {
		case tk.Kind() == lexer.EOF || tk.StartsLine():
			return false
		case tk.Kind() == lexer.LCurl:
			return true
		}
	}
}

// atBlockBreak reports whether a block's statements stop before the next
// token without its closing brace: it starts a line with func, pub, lib,
// extern or import, which no statement starts with, so the brace is
// missing; or with type, when no brace closes the block before the next
// declaration's line (see unclosedBefore), which says the type was meant
// for the package and the brace is missing. A type in a block that is
// closed after it is left to the statement, which says types are declared
// at package level only.
func (p *Parser) atBlockBreak() bool {
	if !p.atDeclLine(true) {
		return false
	}
	return !isKeyword(p.peek(), lexer.Type) || p.unclosedBefore()
}

// unclosedBefore reports whether, from the next token, the start of a
// declaration's line, no closing brace that would close the block holding
// it comes before the next line that starts with a keyword only a
// declaration starts with (see declKeywordAt), or the end of the file.
func (p *Parser) unclosedBefore() bool {
	depth := 0
	for k := 1; ; k++ {
		tk := p.peekAt(k)
		switch {
		case tk.Kind() == lexer.EOF:
			return true
		case tk.Kind() == lexer.LCurl:
			depth++
		case tk.Kind() == lexer.RCurl:
			if depth == 0 {
				return false
			}
			depth--
		case depth == 0 && tk.StartsLine() && p.declKeywordAt(k):
			return true
		}
	}
}

// declKeywordAt reports whether the token k places ahead is one only a
// package-level declaration starts with: func, type, pub, lib, extern or
// import, not var or mut, which a statement starts with too.
func (p *Parser) declKeywordAt(k int) bool {
	tk := p.peekAt(k)
	if tk.Kind() != lexer.Keyword {
		return false
	}
	switch tk.Value() {
	case lexer.Func, lexer.Type, lexer.Pub, lexer.Lib, lexer.Extern, lexer.Import:
		return true
	default:
		return false
	}
}

// parseStmts parses the statements of a block up to its closing brace, or
// the end of the file, or a line that starts a declaration (see
// atBlockBreak), or, when inClause, a case or a default, recovering from
// each that fails (see badStmt), up to one that leaves the block broken
// (see syncStmt). It stops when the parse halts.
func (p *Parser) parseStmts(inClause bool) []ast.Stmt {
	body := make([]ast.Stmt, 0)
	for !p.halted() && !p.atBlockBreak() && !p.atStmtsEnd(inClause) {
		start := p.index
		stmt, diags := p.parseStmt()
		if len(diags) == 0 {
			if stmt == nil {
				break
			}
			body = append(body, stmt)
			continue
		}
		bad, broken := p.badStmt(start, diags, inClause)
		body = append(body, bad)
		if broken {
			break
		}
	}
	return body
}

// atStmtsEnd reports whether the next token ends a block's statements:
// its closing brace or the end of the file, or, in a case clause's body,
// the next case or default too (see atClauseEnd).
func (p *Parser) atStmtsEnd(inClause bool) bool {
	if inClause {
		return p.atClauseEnd()
	}
	kind := p.peek().Kind()
	return kind == lexer.RCurl || kind == lexer.EOF
}
