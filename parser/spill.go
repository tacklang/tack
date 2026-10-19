package parser

import (
	"github.com/tacklang/tack/lexer"
)

// This file holds how error recovery (see recover.go) skips a function's
// body spilled where declarations go. A brace left out, if a > 0 with no
// {, or one too many, } }, closes a function's body before its end, and
// its statements then come where declarations go, each of which would be
// reported as a declaration gone wrong, after the one error that caused
// them, up to the function's own closing brace, which closes nothing. So
// do the statements of a body whose opening brace never came, func main()
// } with a typo for {. These lines are skipped without errors of their
// own, until a line only a declaration starts, or the brace that ends
// them. A var or a mut line among them is parsed as the declaration it may
// be, since it reads as one as much as a statement; one that reads only
// as a statement, var x := 1, is skipped.

// noteSpill records whether the lines after the top-level declaration
// just parsed may be a function's body spilled where declarations go:
// when it is a function whose body parsed as one, or a line that starts
// as a statement does right after one (see stmtLike), either told by
// suspect, and an error was recorded in it, and a brace closing nothing
// comes after it (see orphanBraceAhead). skipSpill and skipStrayBraces
// then skip them, until a line only a declaration starts. A function that
// failed in its header and was skipped whole, its body's braces matched,
// spills nothing, and neither does one whose body ended at its own
// closing brace, which no brace left over after it tells.
func (p *Parser) noteSpill(suspect bool) {
	if suspect && len(p.errs) > p.declErrs && p.orphanBraceAhead() {
		p.spilling = true
	}
}

// orphanBraceAhead reports whether a closing brace that closes nothing
// opened after the parser's position comes before the next line only a
// declaration starts, or the end of the file, with a line that starts as
// only a statement does before it: the closing brace of a body closed too
// soon, and statements of that body. A stray brace after declarations
// alone is no spilled body, and is reported.
func (p *Parser) orphanBraceAhead() bool {
	depth, sawStmt := 0, false
	for i := p.index; i < len(p.tokens); i++ {
		tk := p.tok(i)
		switch {
		case tk.Kind() == lexer.EOF || depth == 0 && p.declOnlyLineAt(i):
			return false
		case tk.Kind() == lexer.RCurl && depth == 0:
			return sawStmt
		}
		depth += braceStep(tk.Kind())
		if depth == 0 && tk.StartsLine() && p.stmtStartsAt(i) {
			sawStmt = true
		}
	}
	return false
}

// declOnlyLineAt reports whether the token at index i starts a line with
// a keyword only a declaration starts with, func, type, extern or import,
// pub and lib aside, which a damaged statement may start with too.
func (p *Parser) declOnlyLineAt(i int) bool {
	tk := p.tok(i)
	return tk.StartsLine() && isDeclKeyword(tk) && !isKeyword(tk, lexer.Pub) &&
		!isKeyword(tk, lexer.Lib)
}

// skipSpill skips, while the lines may be a spilled body (see noteSpill),
// lines that cannot start a declaration, up to the next line that can,
// that ends the spill (see endsSpill), or that starts with a closing
// brace outside the braces skipped, and reports whether it skipped any.
func (p *Parser) skipSpill() bool {
	if !p.spilling || !p.atLineStart() || p.atTopDeclLine() || p.peek().Kind() == lexer.RCurl {
		return false
	}
	p.skipToLine(func() bool {
		return p.peek().Kind() == lexer.RCurl || p.atTopDeclLine() || p.endsSpill()
	})
	return true
}

// endsSpill reports whether the next token, which starts a line, ends a
// spilled body (see noteSpill): it starts a line only a declaration
// starts, or one shaped like a function's header that does not start as a
// statement does, a damaged declaration, * f() bool { with func mistyped,
// which is reported rather than skipped.
func (p *Parser) endsSpill() bool {
	return p.atDeclLine(false) || p.headerLineAt(p.index) && !p.stmtStartsAt(p.index)
}

// atTopDeclLine reports whether the next token starts a line a top-level
// declaration starts: one only a declaration starts (see atDeclLine), or
// a var or a mut one that is not a statement's, var x := or mut x :=.
func (p *Parser) atTopDeclLine() bool {
	tk := p.peek()
	if isKeyword(tk, lexer.Var) || isKeyword(tk, lexer.Mut) {
		return p.atLineStart() && p.peekAt(2).Kind() != lexer.Define
	}
	return p.atDeclLine(false)
}

// skipUnopenedBody skips the body of the damaged declaration that started
// at the token start, when it is a function's, starting as one does (see
// funcShapeAt), and its opening brace never came, func main() } with a
// typo for {: the lines after it, up to and through a closing brace that
// starts a line outside the braces skipped, or up to a line only a
// declaration starts. They are its statements, var x int64 = 1 among
// them, which would otherwise each be reported, or taken for
// declarations; its error says what is wrong.
func (p *Parser) skipUnopenedBody(start int) {
	if !p.hasBody(start) || !p.funcShapeAt(start) || p.braceIn(start) {
		return
	}
	stop := func() bool { return p.peek().Kind() == lexer.RCurl || p.atDeclLine(false) }
	if p.peek().Kind() != lexer.EOF && (!p.atLineStart() || !stop()) {
		p.skipToLine(stop)
	}
	if p.atBraceLine() {
		p.skipBrace()
	}
}

// braceIn reports whether an opening brace is among the tokens from the
// token start to the parser's.
func (p *Parser) braceIn(start int) bool {
	for i := start; i < p.index; i++ {
		if p.tok(i).Kind() == lexer.LCurl {
			return true
		}
	}
	return false
}

// skipToLine consumes tokens, counting the braces they open and close, up
// to the end of the file, the first token that starts a line outside
// every brace consumed and for which stop reports true, or one that starts
// a line inside them where a block's statements stop (see atBlockBreak).
// A spilled line that opens braces it never closes, A}{{, would otherwise
// take every declaration after it, unreported; a block it opens stops
// there too. It consumes at least one token.
func (p *Parser) skipToLine(stop func() bool) {
	depth := 0
	for {
		depth = max(depth+braceStep(p.next().Kind()), 0)
		if p.peek().Kind() == lexer.EOF ||
			p.atLineStart() && (depth == 0 && stop() || depth > 0 && p.atBlockBreak()) {
			return
		}
	}
}

// skipBrace consumes the closing brace that is next, and the semicolon
// inserted after it.
func (p *Parser) skipBrace() {
	p.next()
	if p.peek().Kind() == lexer.Semi {
		p.next()
	}
}

// skipStrayBraces skips each closing brace that starts a line where a
// top-level declaration is expected, after one with an error, or while
// the lines may be a spilled body, which it then ends, and reports whether
// it skipped any: a brace left over from a block an earlier error closed
// too soon, as for a stray clause (see skipStrayClause). A brace after a
// declaration with no error is left to be reported.
func (p *Parser) skipStrayBraces() bool {
	skipped := false
	for p.peek().Kind() == lexer.RCurl && (p.strayAfterError() || p.spilling && p.atLineStart()) {
		p.skipBrace()
		skipped = true
		p.spilling = false
	}
	return skipped
}
