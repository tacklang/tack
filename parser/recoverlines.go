package parser

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/lexer"
)

// This file holds what error recovery (see recover.go) reads of a line:
// whether it starts as only a declaration does, as a function's header
// does, or as only a statement does. Each looks at the tokens of one line
// at most, so recovery never reads past the line it is deciding about.

// atLineStart reports whether the next token starts a line: no token
// comes before it on its line (see lexer.Token.StartsLine).
func (p *Parser) atLineStart() bool {
	return p.peek().StartsLine()
}

// atBraceLine reports whether the next token is a closing brace that
// starts its line.
func (p *Parser) atBraceLine() bool {
	return p.atLineStart() && p.peek().Kind() == lexer.RCurl
}

// nextLine returns the index of the first token after the token k that
// starts a line, or of the EOF token.
func (p *Parser) nextLine(k int) int {
	next := k + 1
	for next < len(p.tokens) && !p.tokens[next].StartsLine() && p.tokens[next].Kind() != lexer.EOF {
		next++
	}
	return min(next, len(p.tokens)-1)
}

// lastOnLine returns the kind of the last token of the line the token k
// is on, from k on, the semicolon inserted at its end aside, or EOF when
// there is none. A skip asks about each written semicolon of a line in
// turn (see stmtGoesOn), so the line last scanned is remembered, which
// keeps a line of thousands of them linear.
func (p *Parser) lastOnLine(k int) lexer.TokenKind {
	m := &p.lineMemo
	if !m.ok || k < m.from || k >= m.end {
		*m = lineMemo{from: k, end: p.nextLine(k), lastAt: -1, ok: true}
		for i := k; i < m.end; i++ {
			if tk := &p.tokens[i]; tk.Kind() != lexer.Semi || !inserted(tk) {
				m.lastAt = i
			}
		}
	}
	if m.lastAt < k {
		return lexer.EOF
	}
	return p.tokens[m.lastAt].Kind()
}

// lineMemo is the line lastOnLine last scanned: from the token from to
// the one before end, whose last token, the inserted semicolon aside, is
// at lastAt, or -1 for none.
type lineMemo struct {
	from, end, lastAt int
	ok                bool
}

// headerLineAt reports whether the line the token k starts ends in an
// opening brace with a closing parenthesis before it, as a function's
// header does.
func (p *Parser) headerLineAt(k int) bool {
	return p.onLine(k, func(tk *lexer.Token) bool { return tk.Kind() == lexer.RParen }) &&
		p.lastOnLine(k) == lexer.LCurl
}

// funcOnLine reports whether func is among the tokens of the line the
// token k is on, from k on.
func (p *Parser) funcOnLine(k int) bool {
	return p.onLine(k, func(tk *lexer.Token) bool { return isKeyword(tk, lexer.Func) })
}

// onLine reports whether match holds for a token of the line the token k
// is on, from k on.
func (p *Parser) onLine(k int, match func(*lexer.Token) bool) bool {
	for i, end := k, p.nextLine(k); i < end; i++ {
		if match(&p.tokens[i]) {
			return true
		}
	}
	return false
}

// atDeclLine reports whether the next tokens start a line the way only a
// top-level declaration does: func and a name or a receiver's
// parenthesis, type and a name, pub or lib and func, var, mut, type or
// extern, extern and func or the parenthesis of its C name, or import, a
// name and from, all on one line. A keyword misused as a name, extern :=
// 1, starts no such line, and is left to the statement it is in to report.
// inside says the line is inside braces, where a func line counts only
// when it has a declaration's shape with its body's brace after its
// parameters (see funcDeclLine), as a function's declaration does and an
// interface's method, func f(), written by mistake, does not.
func (p *Parser) atDeclLine(inside bool) bool {
	if !p.keywordLine() {
		return false
	}
	tk, next := p.peek(), p.peekAt(1)
	switch tk.Value() {
	case lexer.Func:
		return p.funcDeclLine(next, inside)
	case lexer.Type:
		return next.Kind() == lexer.Ident
	case lexer.Import:
		return p.importLine()
	case lexer.Pub, lexer.Lib:
		// pub mut x := is a statement's form, never a declaration's.
		return visibleDeclKeyword(next) && p.peekAt(3).Kind() != lexer.Define
	case lexer.Extern:
		return isKeyword(next, lexer.Func) || next.Kind() == lexer.LParen
	default:
		return false
	}
}

// importLine reports whether the import that is next reads as every
// import does, import x from; import x bool, a var's keyword mistyped, is
// a statement's damage.
func (p *Parser) importLine() bool {
	return p.peekAt(1).Kind() == lexer.Ident && isKeyword(p.peekAt(2), lexer.From)
}

// keywordLine reports whether the next token starts a line and is a
// keyword with more of its line after it: import alone, with a statement
// on the line after, starts no declaration.
func (p *Parser) keywordLine() bool {
	return p.atLineStart() && p.peek().Kind() == lexer.Keyword && !p.peekAt(1).StartsLine()
}

// funcDeclLine reports whether func, with next after it, starts a line as
// a function's declaration does: a name or a receiver's parenthesis next,
// and, inside braces, the shape of a declaration, a name and then its
// parameters, func f( or func f[, or a receiver closed on the line and
// then those, func (p T) m(, with the brace that opens its body after the
// parameters close, on the same line. A line inside braces that starts
// with func but goes on otherwise, func check(a == T{...}, "x"), whose
// brace opens a composite literal inside the parentheses, is left to the
// statement it is in to report (see atDeclLine).
func (p *Parser) funcDeclLine(next *lexer.Token, inside bool) bool {
	if next.Kind() != lexer.Ident && next.Kind() != lexer.LParen {
		return false
	}
	return !inside || p.declShapeAfterFunc() && p.bodyAfterParams()
}

// declShapeAfterFunc reports whether the tokens after the func that is
// next start a declaration: a name followed by ( or [, or a parenthesized
// receiver closed on the same line followed by such a name.
func (p *Parser) declShapeAfterFunc() bool {
	k := p.funcNameAt()
	if k < 0 {
		return false
	}
	after := p.peekAt(k + 1).Kind()
	return after == lexer.LParen || after == lexer.LBrack
}

// funcNameAt returns how many places ahead of the func that is next its
// name is, past a receiver closed on the same line, or -1 when no name
// comes there.
func (p *Parser) funcNameAt() int {
	k := 1
	if p.peekAt(k).Kind() == lexer.LParen {
		end, ok := p.closeOnLine(k, lexer.LParen, lexer.RParen)
		if !ok {
			return -1
		}
		k = end + 1
	}
	if p.peekAt(k).Kind() != lexer.Ident {
		return -1
	}
	return k
}

// closeOnLine returns how many places ahead the token closing the group
// that open opens k places ahead is, and whether it closes on the line
// the group starts on.
func (p *Parser) closeOnLine(k int, open, closing lexer.TokenKind) (int, bool) {
	depth := 0
	for ; ; k++ {
		tk := p.peekAt(k)
		if tk.Kind() == lexer.EOF || depth > 0 && tk.StartsLine() {
			return k, false
		}
		switch tk.Kind() {
		case open:
			depth++
		case closing:
			if depth--; depth == 0 {
				return k, true
			}
		}
	}
}

// bodyAfterParams reports whether, on the line of the func that is next,
// whose name and its parameters' parenthesis follow it (see
// declShapeAfterFunc), an opening brace comes after the parameters close:
// the brace of its body, after its results if any, which hold no operator
// and no literal.
func (p *Parser) bodyAfterParams() bool {
	end, ok := p.paramsEnd()
	if !ok {
		return false
	}
	for k := end + 1; ; k++ {
		tk := p.peekAt(k)
		switch {
		case tk.Kind() == lexer.EOF || tk.StartsLine() || !resultToken(tk):
			return false
		case tk.Kind() == lexer.LCurl:
			return true
		}
	}
}

// paramsEnd returns how many places ahead of the func that is next the
// parenthesis closing its parameters is, past its type parameters, and
// whether they close on its line.
func (p *Parser) paramsEnd() (int, bool) {
	k, ok := p.paramsAt()
	if !ok {
		return 0, false
	}
	return p.closeOnLine(k, lexer.LParen, lexer.RParen)
}

// paramsAt returns how many places ahead of the func that is next the
// parenthesis opening its parameters is, after its name or after type
// parameters closed on its line, and whether it is there.
func (p *Parser) paramsAt() (int, bool) {
	k := p.funcNameAt() + 1
	if p.peekAt(k).Kind() == lexer.LBrack {
		end, ok := p.closeOnLine(k, lexer.LBrack, lexer.RBrack)
		if !ok {
			return 0, false
		}
		k = end + 1
	}
	return k, p.peekAt(k).Kind() == lexer.LParen
}

// resultToken reports whether tk can be part of a function's results
// written before its body: no operator but the punctuation of a type, and
// no literal. f(true) != "x" {, an if with its keyword mistyped, has both.
func resultToken(tk *lexer.Token) bool {
	switch tk.Kind() {
	case lexer.Comma, lexer.Period, lexer.RParen, lexer.RBrack, lexer.Ellipsis:
		return true
	case lexer.LogicalNot, lexer.Str, lexer.Char:
		return false
	}
	return !continuesLine(tk.Kind())
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

// declTok returns the token that says what the declaration starting at
// the token start declares: its first, or, after pub or lib, its second.
func (p *Parser) declTok(start int) *lexer.Token {
	k := p.declKeywordIndex(start)
	return &p.tokens[k]
}

// declKeywordIndex returns the index of the token declTok returns.
func (p *Parser) declKeywordIndex(start int) int {
	k := min(start, len(p.tokens)-1)
	if isKeyword(&p.tokens[k], lexer.Pub) || isKeyword(&p.tokens[k], lexer.Lib) {
		k = min(k+1, len(p.tokens)-1)
	}
	return k
}

// hasBody reports whether the declaration that starts at the token start,
// pub or lib aside, is a function's.
func (p *Parser) hasBody(start int) bool {
	return isKeyword(p.declTok(start), lexer.Func)
}

// holdsNoStmts reports whether the declaration that starts at the token
// start, pub or lib aside, is one whose braces cannot hold statements. A
// function's can: one that starts with func, or whose line holds func and
// is shaped like its header (see headerLineAt), import func main() {, or
// that starts with a keyword and a name and is so shaped, mut main() {, a
// function with func mistyped, or a brace after a function cut off from
// its body (see afterBodilessFunc). An extern func's can only when it is
// written with a body (see externBodyAt). A declaration that starts with
// var, mut, type, import or package otherwise cannot. One that starts
// with anything else can when it starts as a statement does (see
// stmtLike), or is on a header-shaped line, main() { or 1 main() {.
func (p *Parser) holdsNoStmts(start int) bool {
	// A skip asks about each var or mut line it passes, about the same
	// declaration, whose line is read once.
	if m := &p.noStmtsMemo; m.ok && m.start == start {
		return m.val
	}
	val := p.holdsNoStmtsAt(start)
	p.noStmtsMemo = startMemo{start: start, val: val, ok: true}
	return val
}

// startMemo is what holdsNoStmts answered for the declaration that starts
// at the token start.
type startMemo struct {
	start   int
	val, ok bool
}

// holdsNoStmtsAt is holdsNoStmts, worked out.
func (p *Parser) holdsNoStmtsAt(start int) bool {
	k := p.declKeywordIndex(start)
	tk := &p.tokens[k]
	switch {
	case isKeyword(tk, lexer.Extern):
		return !p.externBodyAt(k)
	case p.funcOnLine(k) && p.headerLineAt(k), isKeyword(tk, lexer.Func):
		return false
	case tk.Kind() == lexer.LCurl && p.afterBodilessFunc():
		return false
	case noStmtKeyword(tk):
		return !p.funcMistyped(k)
	}
	return !p.headerLineAt(k) && !p.stmtLike(k)
}

// externBodyAt reports whether the extern func whose line starts at the
// token k is written with a body, an error of its own: its line is shaped
// like a function's header (see headerLineAt), extern func f() {, with no
// closing brace on it. An extern has no body to follow its line, so one
// with a closing brace, extern func f() } else {, is that line's damage,
// and the lines after it are declarations, not statements.
func (p *Parser) externBodyAt(k int) bool {
	return p.headerLineAt(k) &&
		!p.onLine(k, func(tk *lexer.Token) bool { return tk.Kind() == lexer.RCurl })
}

// noStmtKeyword reports whether tk is a keyword that starts a declaration
// that holds no statements: var, mut, type, import, extern or package.
func noStmtKeyword(tk *lexer.Token) bool {
	if tk.Kind() != lexer.Keyword {
		return false
	}
	switch tk.Value() {
	case lexer.Var, lexer.Mut, lexer.Extern, lexer.Type, lexer.Import, lexer.Package:
		return true
	default:
		return false
	}
}

// funcMistyped reports whether the line the keyword at token k starts is
// a function's header with its func mistyped as that keyword: a name and
// a parenthesis after it, and the shape of a header, mut main() {, not
// var x = f(a) {, a damaged var.
func (p *Parser) funcMistyped(k int) bool {
	return p.tokens[min(k+1, len(p.tokens)-1)].Kind() == lexer.Ident &&
		p.tokens[min(k+2, len(p.tokens)-1)].Kind() == lexer.LParen && p.headerLineAt(k)
}

// stmtLike reports whether the damaged top-level declaration that starts
// at the token start starts as a statement does (see stmtStartsAt), right
// after a function. It is then that function's body spilled where
// declarations go, by an opening brace left out, func main() with its
// body after it, or by an extra closing brace: the lines after it may be
// more of the body (see noteSpill), and its skip ends at the function's
// own closing brace (see syncDecl). Such a line after anything else, a
// var's, is a stray line of its own.
func (p *Parser) stmtLike(start int) bool {
	return p.afterFunc() && p.stmtStartsAt(min(start, len(p.tokens)-1))
}

// stmtStartsAt reports whether the token at index i starts as only a
// statement does: a keyword only a statement starts with, var x := or mut
// x :=, or a name not on a line shaped like a function's header.
func (p *Parser) stmtStartsAt(i int) bool {
	tk := &p.tokens[i]
	switch {
	case tk.Kind() == lexer.Ident:
		return !p.headerLineAt(i)
	case isKeyword(tk, lexer.Var) || isKeyword(tk, lexer.Mut):
		// var x := and mut x := declare only in a body.
		return p.tokens[min(i+2, len(p.tokens)-1)].Kind() == lexer.Define
	case tk.Kind() == lexer.Keyword:
		switch tk.Value() {
		case lexer.For, lexer.If, lexer.Return, lexer.Switch, lexer.Defer, lexer.Break,
			lexer.Continue, lexer.Else:
			return true
		}
	}
	return false
}

// afterFunc reports whether the last top-level declaration parsed is a
// function's.
func (p *Parser) afterFunc() bool {
	_, ok := p.lastDecl().(*ast.Func)
	return ok
}

// afterBodilessFunc reports whether the last top-level declaration parsed
// is a function's with no body that is not an extern func, which only an
// extern func may be: its body was cut off from it, func main() ; { or
// with its brace on the next line.
func (p *Parser) afterBodilessFunc() bool {
	fn, ok := p.lastDecl().(*ast.Func)
	return ok && fn.NoBody && !fn.Extern
}

// lastDecl returns the last top-level declaration parsed, or nil.
func (p *Parser) lastDecl() ast.Decl {
	if n := len(p.file.Decls); n > 0 {
		return p.file.Decls[n-1]
	}
	return nil
}

// funcShapeAt reports whether the declaration that starts at the token
// start, pub or lib aside, starts as a function's does, func f( or func
// (p T) m( (see declShapeAfterFunc), or func f[T Any]( with its type
// parameters closed on its line (see paramsAt), rather than func
// followed by anything else, which says nothing about where a body would
// be. A bracket that no parameters' parenthesis follows, func counts
// [3]int64, a var's keyword mistyped, starts no function.
func (p *Parser) funcShapeAt(start int) bool {
	saved := p.index
	defer func() { p.index = saved }()
	p.index = p.declKeywordIndex(start)
	if !p.declShapeAfterFunc() {
		return false
	}
	_, ok := p.paramsAt()
	return ok
}
