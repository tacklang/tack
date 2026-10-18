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
// A failed construct that consumed the first token of the next
// declaration's line, finding its error there, as var a = 1 + before a
// type line does, is first moved back to that line (see rewindTo), so the
// declaration is parsed rather than skipped.
//
// A statement ends at the semicolon after it at its own depth of braces,
// counted from where it started, so a statement with blocks of its own is
// skipped whole, its blocks included; or at the closing brace of the
// block holding it, which is left for that block, but for one in the
// middle of its line, a typo for a comma or a parenthesis (see
// closesBlock). A semicolon inserted before a line that carries on the
// one before, or written in a header, for i := 0; i < n; i++ {, does not
// end it (see stmtGoesOn). An error inside a nested block is caught by
// that block, so only the innermost statement it damages is lost. A
// declaration ends where the next one starts (see declStarts): a line
// starting as only a declaration does (see atDeclLine), at any depth, or
// with var or mut outside every brace, or inside one when the damaged
// declaration cannot hold statements (see holdsNoStmts). A function's
// header line, or a type's first line, counts as opening only its body's
// brace (see declDepth).
//
// A block stops at the start of a line only a declaration starts too: a
// block left unclosed, the most common error that would swallow the rest
// of the file, then reports its missing closing brace there, once, and
// the declaration after it is parsed as one.
//
// A brace left out, or one too many, closes a block too soon, and what
// comes after is a cascade of errors that follow from it: the clauses of
// a switch closed too soon (see skipStrayClause), a function's body
// spilled where declarations go (see spill.go), and the closing brace it
// leaves over. Those are skipped without errors of their own. A cascade
// only indentation could tell apart is still reported: a brace left out
// before declarations that read as statements, var x int64 = 1, is found
// missing at the next line only a declaration starts, and a line edited
// into a declaration's inside a body, pub var x int64 = 1, ends it there.
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
		p.errAt[ast.Span{File: d.Span.File, Start: d.Span.Start}] = true
	}
}

// recorded reports whether an error starting where d starts has been
// recorded: a second one there, such as the missing closing brace of a
// block an earlier error left at the end of the file, follows from the
// first.
func (p *Parser) recorded(d diag.Diagnostic) bool {
	return p.errAt[ast.Span{File: d.Span.File, Start: d.Span.Start}]
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
	p.rewindTo(start, diags, func(depth int) bool { return p.declStarts(start, depth) })
	p.syncDecl(start)
	p.importNamesIn(start)
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
	p.rewindTo(start, diags, func(int) bool { return p.atDeclLine(true) })
	broken := p.syncStmt(start, inClause)
	return &ast.BadStmt{Span: p.skipped(start)}, broken
}

// importNamesIn records as an import's name each name followed by from
// and a string among the tokens from the token start to the parser's: the
// damaged declaration skipped was an import, its keyword mistyped, ! geo
// from "shapes/geo", and its name is known as a package's from here on,
// as parseImport records it, so qualified literals of it parse as ones.
// from starts nothing but an import's path.
func (p *Parser) importNamesIn(start int) {
	for i := start; i+2 < p.index; i++ {
		if p.tokens[i].Kind() == lexer.Ident && isKeyword(&p.tokens[i+1], lexer.From) &&
			p.tokens[i+2].Kind() == lexer.Str {
			p.imports[lexer.ValueChecked[string](&p.tokens[i])] = true
		}
	}
}

// rewindTo moves the parser back to the first token after the token start
// that starts says starts the next construct, given the depth of braces
// opened since start, when the construct that failed with diags consumed
// it and its first error is at it: the start of the line of the next
// declaration, which an error found at the first token of that line,
// expected 'value' found 'type', consumes. That line is then parsed as
// the declaration it is rather than skipped as part of the damage. A
// construct that went on into that line and failed further along it, var
// a = 6 * then mut b int64, read as 6 * mut b, or a struct's field pub
// type x, is not moved back: its error is in the line, which stays part
// of the damage, so the error stays in the bad node.
func (p *Parser) rewindTo(start int, diags []diag.Diagnostic, starts func(depth int) bool) {
	if len(diags) == 0 {
		return
	}
	end, depth := p.index, 0
	first := diags[0].Span.Start
	for _, d := range diags {
		first = min(first, d.Span.Start)
	}
	for k := start + 1; k < end && p.tokens[k].OffsetStart() <= first; k++ {
		depth = max(depth+braceStep(p.tokens[k-1].Kind()), 0)
		p.index = k
		if p.tokens[k].OffsetStart() == first && p.atLineStart() && starts(depth) {
			return
		}
	}
	p.index = end
}

// braceStep returns how a token of kind k changes the depth of braces: up
// one for {, down one for }, and not at all for anything else.
func braceStep(k lexer.TokenKind) int {
	switch k {
	case lexer.LCurl:
		return 1
	case lexer.RCurl:
		return -1
	default:
		return 0
	}
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
// failed construct have consumed one, so that block still ends there. A }
// that cannot close a block, with more of its line after it, is taken for
// part of the damage instead (see closesBlock).
func (p *Parser) braceDepth(start int) int {
	depth := 0
	for i := start; i < p.index; i++ {
		switch p.tokens[i].Kind() {
		case lexer.LCurl:
			depth++
		case lexer.RCurl:
			if depth > 0 {
				depth--
			} else if p.closesBlock(i) {
				p.index = i
				return 0
			}
		}
	}
	return depth
}

// closesBlock reports whether the } at token i can close the block a
// damaged construct is in: the rest of its line is nothing, a semicolon,
// another }, or else, as a block's closing brace's is (see endsItsLine),
// and it starts
// its line or closes a { opened earlier on it, a block written on one
// line, if a { b() }. One followed by more on its line, f(a } b) or a line
// } ), or ending a line that opened no block, x = }, is a typo for a
// comma, a parenthesis or a value, and is skipped with the damage around
// it, rather than ending the block, whose statements on the lines after
// would each be reported again outside it.
func (p *Parser) closesBlock(i int) bool {
	if !p.endsItsLine(i) {
		return false
	}
	return p.tokens[i].StartsLine() || p.opensOnLineBefore(i)
}

// endsItsLine reports whether what comes after the } at token i on its
// line is nothing, a semicolon, another }, or else.
func (p *Parser) endsItsLine(i int) bool {
	if i+1 >= len(p.tokens) {
		return true
	}
	next := &p.tokens[i+1]
	switch {
	case next.Kind() == lexer.EOF, next.StartsLine(), next.Kind() == lexer.RCurl,
		next.Kind() == lexer.Semi, isKeyword(next, lexer.Else):
		return true
	}
	return false
}

// opensOnLineBefore reports whether a { that no } closes comes before the
// token i on its line. A skip asks about each } of a line in turn, so the
// last } found to have none before it is remembered: none comes before
// one after it either, but for a { between them, and the scan stops
// there, which keeps a line of thousands of them linear.
func (p *Parser) opensOnLineBefore(i int) bool {
	depth := 0
	for j := i - 1; j >= 0; j-- {
		if j == p.noOpenBefore-1 && p.noOpenBefore > 0 {
			break // none before the remembered one, and none since
		}
		switch p.tokens[j].Kind() {
		case lexer.RCurl:
			depth++
		case lexer.LCurl:
			if depth == 0 {
				return true
			}
			depth--
		}
		if p.tokens[j].StartsLine() {
			break
		}
	}
	p.noOpenBefore = i
	return false
}

// inserted reports whether tk, a semicolon, was inserted at a line's end
// rather than written: an inserted one covers no bytes.
func inserted(tk *lexer.Token) bool {
	return tk.OffsetEnd() == tk.OffsetStart()
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
		tk := p.next()
		depth = max(depth+braceStep(tk.Kind()), 0)
		if tk.Kind() == lexer.Semi && depth == 0 && !p.stmtGoesOn(tk) {
			return false
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

// stmtGoesOn reports whether the damaged statement being skipped goes on
// past semi, a semicolon at its own depth: an inserted one, at a line's
// end, before a line that starts with a token no statement starts with,
// := or ) or an operator, which carries on the line before, as the second
// line of a header split in two does; or a written one before the rest of
// a line ending in an opening brace, which only a for's, an if's or a
// switch's header has, whose parts written semicolons separate. Each part
// would otherwise be reported as a statement of its own.
func (p *Parser) stmtGoesOn(semi *lexer.Token) bool {
	if inserted(semi) {
		return continuesLine(p.peek().Kind())
	}
	return p.lastOnLine(p.index) == lexer.LCurl
}

// continuesLine reports whether a line that starts with a token of kind k
// carries on the line before it: k is an operator other than ! or an
// assignment's, ) or ], or a comma, a period, a colon or an ellipsis.
func continuesLine(k lexer.TokenKind) bool {
	switch {
	case k >= lexer.Add && k <= lexer.ShiftRightAssign,
		k >= lexer.Equal && k <= lexer.GreaterEqual:
		return true
	}
	switch k {
	case lexer.LogicalAnd, lexer.LogicalOr, lexer.Inc, lexer.Dec, lexer.RParen, lexer.RBrack,
		lexer.Comma, lexer.Period, lexer.Colon, lexer.Ellipsis:
		return true
	default:
		return false
	}
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
		return p.closesBlock(p.index), false
	case inClause && (isKeyword(tk, lexer.Case) || isKeyword(tk, lexer.Default)):
		return p.index > start, false
	default:
		return p.index > start && p.atDeclLine(true), false
	}
}

// syncDecl skips the rest of the declaration that started at the token
// start, to the start of a line that starts the next one (see
// declStarts), or the end of the file, or, for a function's body spilled
// where declarations go, to the closing brace that starts a line outside
// the braces skipped, which skipStrayBraces then takes (see stmtLike). It
// always consumes at least one token, unless the file ends.
func (p *Parser) syncDecl(start int) {
	depth := p.declDepth(start)
	p.index = max(p.index, start)
	spill := p.stmtLike(start)
	for p.peek().Kind() != lexer.EOF && (p.index <= start || !p.declStarts(start, depth)) {
		if spill && p.index > start && depth == 0 && p.atBraceLine() {
			return
		}
		depth = max(depth+braceStep(p.next().Kind()), 0)
	}
}

// declDepth returns the depth of braces from the token start, where a
// damaged declaration starts, to the parser's token, as braceDepth does,
// but for a function's header line or a type's first line: that line
// opens one brace, its body's or its struct's, when it ends in one, and
// none when it does not, whatever other braces it holds, func f } else {
// a int64) { or type } else { struct {, which are its damage. Counting
// them would leave the skip inside braces the body never closes, past
// every declaration after it but a function's.
func (p *Parser) declDepth(start int) int {
	if !p.funcHeaderAt(start) && !p.typeAt(start) {
		return p.braceDepth(start)
	}
	next, depth := p.nextLine(start), p.headerDepth(start)
	if p.index <= next {
		// The failed header did not get past its own line, the rest of
		// which is skipped here, as its braces are counted.
		p.index = next
		return depth
	}
	for i := next; i < p.index; i++ {
		depth = max(depth+braceStep(p.tokens[i].Kind()), 0)
	}
	return depth
}

// funcHeaderAt reports whether the damaged declaration that starts at the
// token start is a function's: it starts with func, or its line is shaped
// like a function's header and it may hold statements (see holdsNoStmts),
// import func main() { or } else { check(c bool) {, func mistyped.
func (p *Parser) funcHeaderAt(start int) bool {
	return p.hasBody(start) || p.headerLineAt(start) && !p.holdsNoStmts(start)
}

// headerDepth returns how many braces the first line of the function or
// the type whose declaration starts at the token start opens: one, its
// body's, when it is shaped like a header (see headerLineAt) or, for one
// that starts with func or type, func f( { with its parameters damaged or
// type T struct {, ends in a brace, and none otherwise.
func (p *Parser) headerDepth(start int) int {
	if p.headerLineAt(start) ||
		(p.hasBody(start) || p.typeAt(start)) && p.lastOnLine(start) == lexer.LCurl {
		return 1
	}
	return 0
}

// typeAt reports whether the declaration that starts at the token start,
// pub or lib aside, starts with type.
func (p *Parser) typeAt(start int) bool {
	return isKeyword(p.declTok(start), lexer.Type)
}

// declStarts reports whether the next token starts the line a declaration
// does, with depth braces open since the damaged declaration that started
// at the token start: a var or a mut one outside every brace, or one only
// a declaration starts (see atDeclLine). Inside braces, a var line starts
// one too when the damaged declaration cannot hold statements (see
// holdsNoStmts), which a var line could be; so does a mut line when it is
// not a type's either, whose interface's methods may be mut ones.
func (p *Parser) declStarts(start, depth int) bool {
	if !p.atLineStart() {
		return false
	}
	tk := p.peek()
	switch {
	case isKeyword(tk, lexer.Var):
		return depth == 0 || p.holdsNoStmts(start)
	case isKeyword(tk, lexer.Mut):
		return depth == 0 || p.holdsNoStmts(start) && !isKeyword(p.declTok(start), lexer.Type)
	}
	return p.atDeclLine(depth > 0)
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
// declaration starts with (see isDeclKeyword), or the end of the file.
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
		case depth == 0 && tk.StartsLine() && isDeclKeyword(tk):
			return true
		}
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
		if !inClause && p.skipStrayClause() {
			continue
		}
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

// skipStrayClause skips a case or a default clause's header, through its
// colon or the end of its line, that starts a line where a statement is
// expected, outside any switch, after an error in the same top-level
// declaration, and reports whether it did. Such a clause is left over from
// a switch an earlier error closed too soon, a block's opening brace left
// out, whose closing brace then closed the switch: the error that did it
// is reported, and each clause after it would only repeat that. A clause
// with no earlier error is left to the statement to report.
func (p *Parser) skipStrayClause() bool {
	tk := p.peek()
	if !p.strayAfterError() || !isKeyword(tk, lexer.Case) && !isKeyword(tk, lexer.Default) {
		return false
	}
	for p.next(); ; p.next() {
		if tk := p.peek(); tk.Kind() == lexer.EOF || tk.Kind() == lexer.RCurl || tk.StartsLine() {
			return true
		}
		if p.peek().Kind() == lexer.Colon {
			p.next()
			return true
		}
	}
}

// strayAfterError reports whether the next token starts a line and an
// error has been recorded since the top-level declaration being parsed,
// or the one just parsed, began.
func (p *Parser) strayAfterError() bool {
	return p.atLineStart() && len(p.errs) > p.declErrs
}
