package parser

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
)

// Recovery inside a switch. A clause whose header fails, case or default
// and its values through the colon, does not fail the switch: its error is
// recorded, the header skipped (see skipHeader), and the statements after
// it parsed as its body, which the next case, default or closing brace
// ends as any clause's does. A bad clause, an ast.CaseClause with Bad set,
// stands for it in the switch, so every other clause is in the tree,
// parsed as it would be with no damage. A second default, which parses
// but is rejected, becomes a bad clause the same way (see rejectClause).
//
// A clause's body ends where the next clause starts, at a case or a
// default, and also at a line that can only be a damaged header (see
// atDamagedHeader): one ending in a colon, as csae 2: or type string:
// does. Its statements then go to a bad clause of their own rather than
// into the body of the clause before, where a type switch's variable
// would have that clause's type and each use of it be reported again.
//
// The header's skip covers its first error, or the token after the skip
// does, as for every bad node. A header that went on past the switch to
// find its error, which no skip inside the switch covers, fails the
// switch as before, and the block holding it recovers.

// badClause records diags, the errors of the clause header that started
// at the token start and failed, skips the header, parses the statements
// after it as the clause's body, and returns the bad clause standing for
// it (see the comment at the top of the file). It returns nil, recording
// nothing and leaving the parser where it was, when the nesting limit was
// passed, which ends the parse, or when the header's skip does not cover
// its first error.
func (p *Parser) badClause(start int, diags []diag.Diagnostic) *ast.CaseClause {
	if p.tooDeep != nil || len(diags) == 0 {
		return nil
	}
	failed := p.index
	p.unconsumeEOF()
	p.index = start
	if !p.skipHeader(start, firstErrorAt(diags)) {
		p.index = failed
		return nil
	}
	p.record(diags)
	header := &ast.BadStmt{Span: p.skipped(start)}
	body := append([]ast.Stmt{header}, p.parseStmts(true)...)
	return &ast.CaseClause{Span: header.Span, Body: body, Bad: true}
}

// rejectClause records diags, the errors of cl, a clause that parsed but
// is rejected, and makes it a bad clause: its header, which cl's span
// covers, becomes the bad statement its body starts with, and its values
// are dropped.
func (p *Parser) rejectClause(cl *ast.CaseClause, diags []diag.Diagnostic) {
	p.record(diags)
	header := &ast.BadStmt{Span: cl.Span}
	cl.Body = append([]ast.Stmt{header}, cl.Body...)
	cl.Values, cl.Default, cl.Bad = nil, false, true
}

// firstErrorAt returns where the first of diags, which are never none,
// starts in the source.
func firstErrorAt(diags []diag.Diagnostic) int {
	first := diags[0].Span.Start
	for _, d := range diags[1:] {
		first = min(first, d.Span.Start)
	}
	return first
}

// skipHeader skips a damaged clause header from the token start, the
// parser's, whose first error starts at the offset first, and reports
// whether the skip covers that error: it starts before the token after
// the skip, or at it. The header ends after a colon outside every
// parenthesis, bracket and brace opened since start, at or past the
// error, or before a line, after the first, that starts at or past the
// error; and, short of the error, before a line starting with case or
// default, which starts the next clause, or a closing brace that closes
// the switch, or the end of the file, which a skip then fails to cover.
func (p *Parser) skipHeader(start, first int) bool {
	depth := 0
	for !p.headerStops(start, first, depth) {
		tk := p.next()
		depth = max(depth+nestStep(tk.Kind()), 0)
		if tk.Kind() == lexer.Colon && depth == 0 && tk.OffsetStart() >= first {
			return true
		}
	}
	return first <= p.peek().OffsetStart()
}

// headerStops reports whether a damaged clause header's skip, from the
// token start, with depth parentheses, brackets and braces open, stops
// before the next token, the header's first error starting at the offset
// first (see skipHeader).
func (p *Parser) headerStops(start, first, depth int) bool {
	tk := p.peek()
	switch {
	case tk.Kind() == lexer.EOF:
		return true
	case p.index > start && tk.StartsLine():
		return tk.OffsetStart() >= first || p.atClauseEnd()
	default:
		return tk.Kind() == lexer.RCurl && depth == 0 && p.closesBlock(p.index)
	}
}

// nestStep returns how a token of kind k changes the depth of
// parentheses, brackets and braces: up one for an opening one, down one
// for a closing one, and not at all for anything else.
func nestStep(k lexer.TokenKind) int {
	switch k {
	case lexer.LParen, lexer.LBrack, lexer.LCurl:
		return 1
	case lexer.RParen, lexer.RBrack, lexer.RCurl:
		return -1
	default:
		return 0
	}
}

// atDamagedHeader reports whether the next token, in a case clause's
// body, starts a line that can only be a clause's header, damaged: its
// last token is a colon outside every parenthesis, bracket and brace
// opened on the line, which ends no statement's line, and it does not
// start with case or default, which end the body anyway. The line is read
// once, at the statement that starts it.
func (p *Parser) atDamagedHeader() bool {
	if !p.atLineStart() || p.atClauseEnd() {
		return false
	}
	end := p.nextLine(p.index)
	depth, last, lastDepth := 0, lexer.EOF, 0
	for i := p.index; i < end; i++ {
		tk := p.tok(i)
		if tk.Kind() == lexer.Semi && inserted(tk) {
			continue
		}
		depth = max(depth+nestStep(tk.Kind()), 0)
		last, lastDepth = tk.Kind(), depth
	}
	return last == lexer.Colon && lastDepth == 0
}
