package lexer

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/source"
)

// Lexer turns the bytes of one source file into tokens. A Lexer is used once:
// create it with New, call Lex, then read the result from Tokens.
type Lexer struct {
	file   *source.File
	tokens []Token
	diags  []diag.Diagnostic
	pos    int
}

// New returns a Lexer over file. The file's line starts are filled in as a
// side effect of Lex, so the file must not have been lexed before.
func New(file *source.File) *Lexer {
	return &Lexer{file: file, tokens: make([]Token, 0)}
}

// Tokens returns the tokens produced by Lex, ending with an EOF token.
func (l *Lexer) Tokens() []Token {
	return l.tokens
}

// operator is one spelling of a punctuation token.
type operator struct {
	text string
	kind TokenKind
}

// operators lists, for each byte that can start a punctuation token, the
// tokens it can start, longest first, so the first match is the longest one.
var operators = map[byte][]operator{
	'+': {{"++", Inc}, {"+=", AddAssign}, {"+", Add}},
	'-': {{"--", Dec}, {"-=", SubAssign}, {"-", Sub}},
	'*': {{"*=", MulAssign}, {"*", Mul}},
	'/': {{"/=", DivAssign}, {"/", Div}},
	'%': {{"%=", ModAssign}, {"%", Mod}},
	'&': {
		{"&^=", BitAndNotAssign},
		{"&=", BitAndAssign},
		{"&&", LogicalAnd},
		{"&^", BitAndNot},
		{"&", BitAnd},
	},
	'|': {{"|=", BitOrAssign}, {"||", LogicalOr}, {"|", BitOr}},
	'^': {{"^=", BitXorAssign}, {"^", BitXor}},
	'<': {
		{"<<=", ShiftLeftAssign},
		{"<=", LessEqual},
		{"<-", Arrow},
		{"<<", ShiftLeft},
		{"<", Less},
	},
	'>': {{">>=", ShiftRightAssign}, {">=", GreaterEqual}, {">>", ShiftRight}, {">", Greater}},
	'=': {{"==", Equal}, {"=", Assign}},
	'!': {{"!=", NotEqual}, {"!", LogicalNot}},
	'(': {{"(", LParen}},
	')': {{")", RParen}},
	'[': {{"[", LBrack}},
	']': {{"]", RBrack}},
	'{': {{"{", LCurl}},
	'}': {{"}", RCurl}},
	',': {{",", Comma}},
	'.': {{"...", Ellipsis}, {".", Period}},
	':': {{":=", Define}, {":", Colon}},
	';': {{";", Semi}},
	'~': {{"~", Tilde}},
}

// Lex tokenizes the whole file and returns the diagnostics it found. It keeps
// going after an error, so one run reports every lexical problem. Tokens are
// available from Tokens afterwards.
func (l *Lexer) Lex() []diag.Diagnostic {
	if l.file == nil {
		panic("file is nil")
	}

	l.file.AddLineStart(0)

	data := l.file.Data

	for l.pos < len(data) {
		c := data[l.pos]

		switch {
		case c == ' ' || c == '\t':
			l.handleSpace()

		case c == '\n':
			l.handleNewline()

		case l.handleToken():
			// handleToken consumed the input and reported any error.

		default:
			l.diag(fmt.Sprintf("invalid character %c", c), l.pos, l.pos+1)
			l.pos++
		}
	}

	l.addSemi(l.pos)
	l.addToken(EOF, nil, l.pos, l.pos)

	l.checkTokens()

	return l.diags
}

// handleToken lexes the token starting at the current position, and reports
// false, consuming nothing, if no token can start there. Each handler checks
// its own first byte and declines without side effects otherwise. The first
// bytes the handlers accept are disjoint except for a slash, which is tried
// as a comment before it is tried as an operator.
func (l *Lexer) handleToken() bool {
	return l.handleIdent() ||
		l.handleString() ||
		l.handleChar() ||
		l.handleDigit() ||
		l.handleComment() ||
		l.handleOperator()
}

// checkTokens panics if the token stream breaks the lexer's own invariants:
// tokens must not overlap and must not extend past the end of the file. A
// failure here is a bug in the lexer, not a user error.
func (l *Lexer) checkTokens() {
	data := l.file.Data
	for i := range l.tokens {
		if i > 0 && l.tokens[i].offStart < l.tokens[i-1].offEnd {
			panic(
				fmt.Sprintf(
					"tokens are overlapping: [%d:%d]",
					l.tokens[i].offStart,
					l.tokens[i-1].offEnd,
				),
			)
		}
		if l.tokens[i].offEnd > len(data) {
			panic(
				fmt.Sprintf(
					"token %q ends after EOF: [%d:%d]",
					l.tokens[i].kind,
					l.tokens[i].offStart,
					l.tokens[i].offEnd,
				),
			)
		}
	}
}

// handleOperator emits the longest punctuation token starting at the current
// position and moves past it. It reports false, consuming nothing, when no
// punctuation token starts there.
func (l *Lexer) handleOperator() bool {
	for _, op := range operators[l.file.Data[l.pos]] {
		if l.at(op.text) {
			l.addToken(op.kind, nil, l.pos, l.pos+len(op.text))
			l.pos += len(op.text)
			return true
		}
	}
	return false
}

// at reports whether the input at the current position begins with s.
func (l *Lexer) at(s string) bool {
	return bytes.HasPrefix(l.file.Data[l.pos:], []byte(s))
}

// isIdentStart reports whether c can begin an identifier.
func isIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

// isIdentByte reports whether c can appear in an identifier after its first
// byte.
func isIdentByte(c byte) bool {
	return isIdentStart(c) || isDigit(c)
}

// isDigit reports whether c is an ASCII decimal digit.
func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// diag records an error covering the bytes [start, end).
func (l *Lexer) diag(msg string, start, end int) {
	l.diags = append(l.diags, diag.Diagnostic{
		Severity: diag.Error,
		Message:  msg,
		Span:     ast.Span{Start: start, End: end},
	})
}

// addToken appends a token of kind k covering the bytes [start, end).
func (l *Lexer) addToken(k TokenKind, val any, start, end int) {
	l.tokens = append(l.tokens, Token{kind: k, val: val, offStart: start, offEnd: end})
}

// handleSpace skips a run of spaces and tabs.
func (l *Lexer) handleSpace() {
	data := l.file.Data
	for l.pos < len(data) && (data[l.pos] == ' ' || data[l.pos] == '\t') {
		l.pos++
	}
}

// handleNewline skips a run of newlines, recording where each new line
// starts, and inserts a semicolon at the first one if the preceding token can
// end a statement. Passing true suppresses the semicolon, for callers that
// insert their own.
func (l *Lexer) handleNewline(skipSemi ...bool) {
	nl, data := l.pos, l.file.Data
	if data[l.pos] != '\n' {
		return
	}
	for l.pos < len(data) && data[l.pos] == '\n' {
		l.file.AddLineStart(l.pos + 1)
		l.pos++
	}
	if len(skipSemi) > 0 && skipSemi[0] {
		return
	}
	l.addSemi(nl)
}

// handleIdent lexes an identifier or keyword. It reports false, consuming
// nothing, when the current byte cannot start an identifier. The asm keyword
// also swallows everything up to the next closing brace.
func (l *Lexer) handleIdent() bool {
	start, data := l.pos, l.file.Data

	if !isIdentStart(data[l.pos]) {
		return false
	}

	for l.pos < len(data) && isIdentByte(data[l.pos]) {
		l.pos++
	}

	v := string(data[start:l.pos])

	if kw, ok := LookupKeyword(v); ok {
		l.addToken(Keyword, kw, start, l.pos)
		if kw == ASM {
			l.handleASM()
		}
	} else {
		l.addToken(Ident, v, start, l.pos)
	}

	return true
}

// handleASM skips the body of an asm block, stopping at its closing brace so
// that the brace is lexed as an ordinary token.
func (l *Lexer) handleASM() {
	for l.pos < len(l.file.Data) {
		if l.file.Data[l.pos] == '}' {
			break
		}
		l.pos++
	}
}

// handleString lexes a double-quoted or backquoted string, decoding escape
// sequences. A double-quoted string may not span lines. An unterminated
// string still produces a token, along with a diagnostic.
func (l *Lexer) handleString() bool {
	start, data := l.pos, l.file.Data

	if l.pos >= len(data) || (data[l.pos] != '"' && data[l.pos] != '`') {
		return false
	}

	term, terminated := data[l.pos], false
	l.pos++

	var val strings.Builder
	for l.pos < len(data) {
		if data[l.pos] == term {
			terminated = true
			l.pos++
			break
		}
		if data[l.pos] == '\n' && term == '"' {
			break
		}
		if data[l.pos] == '\\' {
			c := l.unescapeRune('"')
			val.WriteByte(c)
			continue
		}
		val.WriteByte(data[l.pos])
		l.pos++
	}

	l.addToken(Str, val.String(), start, l.pos)

	if !terminated {
		l.diag("unterminated string", start, l.pos)
	}

	return true
}

// handleChar lexes a single-quoted character literal holding exactly one
// byte or escape sequence. A malformed literal produces a diagnostic and no
// token.
func (l *Lexer) handleChar() bool {
	start, data := l.pos, l.file.Data

	if l.pos >= len(data) || data[l.pos] != '\'' {
		return false
	}
	l.pos++

	if l.pos >= len(data) {
		l.diag("unterminated char literal", start, l.pos)
		return true
	}

	if data[l.pos] == '\'' {
		l.pos++
		l.diag("empty char literal", start, l.pos)
		return true
	}

	val := data[l.pos]
	if data[l.pos] == '\\' {
		val = l.unescapeRune('\'')
	} else {
		l.pos++
	}

	if l.pos >= len(data) {
		l.diag("unterminated char literal", start, start+1)
		return true
	}

	if data[l.pos] != '\'' {
		l.skipBadChar(start)
		return true
	}
	l.pos++

	l.addToken(Char, string(val), start, l.pos)

	return true
}

// skipBadChar recovers from a character literal starting at start that holds
// more than one character: it skips to the closing quote, or to the end of the
// line if there is none, and reports the whole literal.
func (l *Lexer) skipBadChar(start int) {
	data := l.file.Data
	l.pos++
	for l.pos < len(data) {
		if data[l.pos] == '\n' {
			l.handleNewline()
			break // we need to jump after the next l.pos++
		}
		if data[l.pos] == '\'' {
			break
		}
		l.pos++
	}
	l.pos++
	l.diag("too many characters in char literal", start, l.pos)
}

// simpleEscapes maps the byte after a backslash to the byte it denotes, for
// the escapes valid in every kind of literal.
var simpleEscapes = map[byte]byte{
	'a':  '\a',
	'b':  '\b',
	'f':  '\f',
	'v':  '\v',
	'n':  '\n',
	't':  '\t',
	'r':  '\r',
	'\\': '\\',
}

// unescapeRune decodes the escape sequence at the current position and moves
// past it. additionalSequences lists further bytes that may be escaped as
// themselves, such as the literal's own quote. An invalid sequence is
// reported and decodes to 0, as does a backslash at the end of the input.
func (l *Lexer) unescapeRune(additionalSequences ...byte) byte {
	start, data := l.pos, l.file.Data
	if l.pos >= len(data) || data[l.pos] != '\\' {
		return 0
	}
	l.pos++
	if l.pos >= len(data) {
		return 0
	}

	c := data[l.pos]
	l.pos++
	if r, ok := simpleEscapes[c]; ok {
		return r
	}
	if slices.Contains(additionalSequences, c) {
		return c
	}
	l.diag(fmt.Sprintf("Invalid escape sequence %q", data[start:l.pos]), start, l.pos)
	return 0
}

// handleDigit lexes an integer or a float. A float is a run of digits
// containing a single dot after at least one digit; a second dot ends the
// number.
func (l *Lexer) handleDigit() bool {
	start, k, data := l.pos, Int, l.file.Data

	var val strings.Builder
	for l.pos < len(data) && ((data[l.pos] >= '0' && data[l.pos] <= '9') || (data[l.pos] == '.' && val.Len() > 0 && k == Int)) {
		if data[l.pos] == '.' {
			k = Float
		}
		val.WriteByte(data[l.pos])
		l.pos++
	}

	if val.Len() > 0 {
		l.addToken(k, val.String(), start, l.pos)
	}

	return val.Len() > 0
}

// handleComment lexes a line comment. Consecutive lines that each start with
// "//" merge into one Comment token whose value joins them with newlines. A
// semicolon is inserted before the comment if the preceding token can end a
// statement. Block comments are not supported and panic.
func (l *Lexer) handleComment() bool {
	start, data := l.pos, l.file.Data

	if !l.at("//") && !l.at("/*") {
		return false
	}

	if l.at("/*") {
		panic("multi-line comment not supported")
	}
	l.pos += 2

	var val strings.Builder
	for l.pos < len(data) {
		if data[l.pos] == '\n' {
			l.handleNewline(true)

			if l.at("//") {
				val.WriteByte('\n')
				l.pos += 2
				continue
			}
			break
		}

		val.WriteByte(data[l.pos])
		l.pos++
	}

	l.addSemi(start)
	l.addToken(Comment, strings.TrimSpace(val.String()), start, l.pos-1)

	return true
}

// addSemi inserts an empty semicolon token at pos if the last token can end a
// statement. This is automatic semicolon insertion, following Go's rule.
func (l *Lexer) addSemi(pos int) {
	if len(l.tokens) > 0 && endsStatement(&l.tokens[len(l.tokens)-1]) {
		l.addToken(Semi, nil, pos, pos)
	}
}

// endsStatement reports whether a semicolon belongs after tk when a line ends
// there: identifiers, literals, ++ and --, closing brackets, and the keywords
// break, continue and return.
func endsStatement(tk *Token) bool {
	switch tk.kind {
	case Ident, Str, Int, Float, Char, Inc, Dec, RParen, RBrack, RCurl:
		return true
	case Keyword:
		return tk.val == Break || tk.val == Continue || tk.val == Return
	default:
		return false
	}
}
