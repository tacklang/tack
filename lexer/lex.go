package lexer

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

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
//
// The token slice starts with room for one token per bytesPerToken bytes of
// source, which real code rarely outgrows, so a large file's tokens are not
// copied again and again as the slice grows.
func New(file *source.File) *Lexer {
	return &Lexer{file: file, tokens: make([]Token, 0, len(file.Data)/bytesPerToken+2)}
}

// bytesPerToken is the bytes of source New allows for each token when it
// sizes the token slice. The examples average five to six; four leaves room
// for denser code. A file with more tokens than that still lexes, its slice
// growing as it would otherwise.
const bytesPerToken = 4

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
	'+': {{"+%=", AddWrapAssign}, {"++", Inc}, {"+=", AddAssign}, {"+%", AddWrap}, {"+", Add}},
	'-': {{"-%=", SubWrapAssign}, {"--", Dec}, {"-=", SubAssign}, {"-%", SubWrap}, {"-", Sub}},
	'*': {{"*%=", MulWrapAssign}, {"*=", MulAssign}, {"*%", MulWrap}, {"*", Mul}},
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
	'?': {{"?", Question}},
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

		case l.newlineAt(l.pos) > 0:
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
		Span:     ast.Span{File: l.file.ID, Start: start, End: end},
	})
}

// addToken appends a token of kind k covering the bytes [start, end),
// starting a line when it is the first token or a newline lies between the
// last one and it (see Token.StartsLine).
func (l *Lexer) addToken(k TokenKind, val any, start, end int) {
	line := len(l.tokens) == 0
	if !line {
		prev := l.tokens[len(l.tokens)-1].offEnd
		line = prev <= start && bytes.IndexByte(l.file.Data[prev:start], '\n') >= 0
	}
	l.tokens = append(
		l.tokens,
		Token{kind: k, val: val, offStart: start, offEnd: end, startsLine: line},
	)
}

// handleSpace skips a run of spaces and tabs.
func (l *Lexer) handleSpace() {
	data := l.file.Data
	for l.pos < len(data) && (data[l.pos] == ' ' || data[l.pos] == '\t') {
		l.pos++
	}
}

// newlineAt returns the length of the newline starting at offset i: 1 for
// a line feed, 2 for a carriage return followed by one, and 0 if no newline
// starts there. A carriage return alone is not a newline.
func (l *Lexer) newlineAt(i int) int {
	data := l.file.Data
	switch {
	case i >= len(data):
		return 0
	case data[i] == '\n':
		return 1
	case data[i] == '\r' && i+1 < len(data) && data[i+1] == '\n':
		return 2
	default:
		return 0
	}
}

// handleNewline skips a run of newlines, each a line feed or a carriage
// return and line feed, recording where each new line starts, and inserts a
// semicolon at the first one if the preceding token can end a statement.
func (l *Lexer) handleNewline() {
	nl := l.pos
	for n := l.newlineAt(l.pos); n > 0; n = l.newlineAt(l.pos) {
		l.pos += n
		l.file.AddLineStart(l.pos)
	}
	l.addSemi(nl)
}

// handleIdent lexes an identifier or keyword. It reports false, consuming
// nothing, when the current byte cannot start an identifier.
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
	} else {
		l.addToken(Ident, v, start, l.pos)
	}

	return true
}

// handleString lexes a double-quoted or backquoted string. It reports
// false, consuming nothing, when the current byte starts neither.
func (l *Lexer) handleString() bool {
	switch l.file.Data[l.pos] {
	case '"':
		l.handleQuoted()
		return true
	case '`':
		l.handleRaw()
		return true
	default:
		return false
	}
}

// handleQuoted lexes a double-quoted string, decoding its escape sequences.
// It may not span lines: a newline, \n or \r\n, ends it unterminated and is
// left for the main loop to record. A carriage return not followed by a
// line feed is a byte of the string like any other. An unterminated string
// still produces a token, along with a diagnostic.
func (l *Lexer) handleQuoted() {
	start, data := l.pos, l.file.Data
	l.pos++

	terminated := false
	var val strings.Builder
	for l.pos < len(data) && l.newlineAt(l.pos) == 0 {
		if data[l.pos] == '"' {
			terminated = true
			l.pos++
			break
		}
		if data[l.pos] == '\\' {
			val.WriteByte(l.unescapeRune('"'))
			continue
		}
		val.WriteByte(data[l.pos])
		l.pos++
	}

	l.addToken(Str, val.String(), start, l.pos)

	if !terminated {
		l.diag("unterminated string", start, l.pos)
	}
}

// handleRaw lexes a backquoted string, which is raw, as in Go: every byte up
// to the next backquote is taken as it is, except carriage returns, which
// are dropped so a file's line endings do not change the value. A backslash
// is a byte like any other, so nothing is decoded and a backquote cannot
// appear inside. The string may span lines, whose starts are recorded, or
// every later position would be reported on the wrong line. One with no
// closing backquote runs to the end of the input and still produces a
// token, along with a diagnostic.
func (l *Lexer) handleRaw() {
	start, data := l.pos, l.file.Data
	l.pos++

	end := bytes.IndexByte(data[l.pos:], '`')
	terminated := end >= 0
	if !terminated {
		end = len(data) - l.pos
	}
	body := data[l.pos : l.pos+end]

	for i, c := range body {
		if c == '\n' {
			l.file.AddLineStart(l.pos + i + 1)
		}
	}
	l.pos += end
	if terminated {
		l.pos++
	}

	l.addToken(Str, string(bytes.ReplaceAll(body, []byte{'\r'}, nil)), start, l.pos)

	if !terminated {
		l.diag("unterminated string", start, l.pos)
	}
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

	// A char literal cannot span lines, so a newline ends it unterminated and
	// is left for the main loop to record.
	if l.pos >= len(data) || l.newlineAt(l.pos) > 0 {
		l.diag("unterminated char literal", start, l.pos)
		return true
	}

	if l.skipEmptyOrWideChar(start) {
		return true
	}

	val := data[l.pos]
	if data[l.pos] == '\\' {
		val = l.unescapeRune('\'')
	} else {
		l.pos++
	}

	if l.pos >= len(data) || l.newlineAt(l.pos) > 0 {
		l.diag("unterminated char literal", start, l.pos)
		return true
	}

	if data[l.pos] != '\'' {
		l.skipBadChar(start)
		return true
	}
	l.pos++

	// One byte, not the UTF-8 encoding of the code point the byte's value
	// would be, which string(val) would give for a byte from 0x80 up.
	l.addToken(Char, string([]byte{val}), start, l.pos)

	return true
}

// skipEmptyOrWideChar reports a character literal starting at start that is
// empty, ”, or holds one character encoded in more than one byte of UTF-8,
// such as 'é', since a character literal is one byte, and moves past it. It
// reports whether it found one. A byte that does not begin a valid
// multi-byte sequence is left for handleChar, as a byte of its own.
func (l *Lexer) skipEmptyOrWideChar(start int) bool {
	data := l.file.Data
	if data[l.pos] == '\'' {
		l.pos++
		l.diag("empty char literal", start, l.pos)
		return true
	}
	_, size := utf8.DecodeRune(data[l.pos:])
	if size < 2 || l.pos+size >= len(data) || data[l.pos+size] != '\'' {
		return false
	}
	l.pos += size + 1
	l.diag(fmt.Sprintf(
		"char literal %s is %d bytes in UTF-8; a char literal must be one byte",
		data[start:l.pos], size,
	), start, l.pos)
	return true
}

// skipBadChar recovers from a character literal starting at start that holds
// more than one character: it skips past the closing quote, or up to the end
// of the line or input if there is none, and reports the whole literal. A
// newline is left in place, so the main loop records the line and inserts any
// semicolon as usual.
func (l *Lexer) skipBadChar(start int) {
	data := l.file.Data
	for l.pos < len(data) && l.newlineAt(l.pos) == 0 {
		if data[l.pos] == '\'' {
			l.pos++
			break
		}
		l.pos++
	}
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
// past it: one of simpleEscapes, or \x and two hex digits, giving that byte. additionalSequences lists further bytes that may be escaped as
// themselves, such as the literal's own quote. An invalid sequence is
// reported and decodes to 0. A backslash at the end of the input decodes to 0
// unreported, since the literal it cut off reports itself as unterminated. A
// backslash before a newline, \n or \r\n, is reported and decodes to 0, and
// the newline is left in place for the caller to handle.
func (l *Lexer) unescapeRune(additionalSequences ...byte) byte {
	start, data := l.pos, l.file.Data
	if l.pos >= len(data) || data[l.pos] != '\\' {
		return 0
	}
	l.pos++
	if l.pos >= len(data) {
		return 0
	}
	if l.newlineAt(l.pos) > 0 {
		l.diag(fmt.Sprintf("Invalid escape sequence %q", data[start:l.pos]), start, l.pos)
		return 0
	}

	c := data[l.pos]
	l.pos++
	if r, ok := simpleEscapes[c]; ok {
		return r
	}
	if c == 'x' {
		return l.unescapeHex(start)
	}
	if slices.Contains(additionalSequences, c) {
		return c
	}
	l.diag(fmt.Sprintf("Invalid escape sequence %q", data[start:l.pos]), start, l.pos)
	return 0
}

// unescapeHex decodes the two hex digits of a \x escape, which starts at
// start, the current position being just past the x, and moves past them.
// Fewer than two hex digits, upper or lower case, is reported, covering the
// \x and any digit there is, and decodes to 0; whatever stopped the escape
// is left in place.
func (l *Lexer) unescapeHex(start int) byte {
	data := l.file.Data
	var v byte
	for range 2 {
		if l.pos >= len(data) {
			break
		}
		d, ok := hexDigit(data[l.pos])
		if !ok {
			break
		}
		v = v<<4 | d
		l.pos++
	}
	if l.pos-start < len(`\x00`) {
		l.diag(fmt.Sprintf("Invalid escape sequence %q", data[start:l.pos]), start, l.pos)
		return 0
	}
	return v
}

// hexDigit returns the value of the hex digit c, upper or lower case, and
// whether c is one.
func hexDigit(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

// handleDigit lexes a number: an integer literal, decimal or, after a 0x,
// 0b or 0o prefix in either case, hexadecimal, binary or octal, with _
// allowed between digits and after a prefix to group them; or a decimal
// float literal, as Go writes one, lexed as a Float: digits with a
// fraction point, 1.5 or 1., a point and digits, .5, or either or plain
// digits followed by an exponent, e or E, an optional sign and digits, as
// in 1e9 or 1.5e-3, with _ allowed between digits. The token's value is
// the literal as written, which the parser decodes (see constant.Parse,
// constant.ParseFloat). A prefix with no digit after it, a digit its base
// does not have, an exponent with no digits, and a _ that does not
// separate digits are reported, the literal still a token; a decimal
// integer starting with 0 is reported by checkLeadingZero. It reports
// false, consuming nothing, when the current byte is no digit, nor a
// point followed by one.
func (l *Lexer) handleDigit() bool {
	start, data := l.pos, l.file.Data
	if !l.numberStarts() {
		return false
	}
	if base, name := prefixBase(data[l.pos:]); base != 0 {
		l.pos += 2
		l.prefixedInt(start, base, name)
		return true
	}

	k := l.decimal()
	expOK := true
	if l.pos < len(data) && (data[l.pos] == 'e' || data[l.pos] == 'E') {
		k, expOK = Float, l.exponent()
	}
	text := string(data[start:l.pos])
	l.checkDecimal(k, expOK, text, start)
	l.addToken(k, text, start, l.pos)
	return true
}

// numberStarts reports whether a number starts at the current position:
// a digit, or a point followed by one, as in .5.
func (l *Lexer) numberStarts() bool {
	data := l.file.Data
	return isDigit(data[l.pos]) ||
		data[l.pos] == '.' && l.pos+1 < len(data) && isDigit(data[l.pos+1])
}

// checkDecimal reports what is wrong with text, a decimal literal of kind
// k starting at start, an integer or a float whose exponent has a digit
// when expOK: an exponent with no digits, a _ that separates none, or an
// integer's leading zero.
func (l *Lexer) checkDecimal(k TokenKind, expOK bool, text string, start int) {
	switch {
	case !expOK:
		l.diag(fmt.Sprintf("exponent has no digits in %s", text), start, l.pos)
	case k == Float:
		l.checkFloatSeparators(text, start)
	case l.checkSeparators(text, start, 0):
		l.checkLeadingZero(text, start)
	}
}

// decimal consumes a decimal number's digits and _ separators, with one
// fraction point among them, and returns its kind: Float when it had the
// point, Int otherwise.
func (l *Lexer) decimal() TokenKind {
	data, k := l.file.Data, Int
	for l.pos < len(data) && (isDigit(data[l.pos]) || data[l.pos] == '_' ||
		(data[l.pos] == '.' && k == Int)) {
		if data[l.pos] == '.' {
			k = Float
		}
		l.pos++
	}
	return k
}

// exponent consumes a float literal's exponent, at the current e or E:
// the letter, an optional + or -, and the digits and _ separators after
// it. It reports whether a digit was among them.
func (l *Lexer) exponent() bool {
	data := l.file.Data
	l.pos++
	if l.pos < len(data) && (data[l.pos] == '+' || data[l.pos] == '-') {
		l.pos++
	}
	digits := false
	for l.pos < len(data) && (isDigit(data[l.pos]) || data[l.pos] == '_') {
		digits = digits || isDigit(data[l.pos])
		l.pos++
	}
	return digits
}

// checkFloatSeparators reports text, a float literal starting at start,
// when a _ in it does not stand between two digits: next to the point,
// the exponent's letter or sign, another _, or either end.
func (l *Lexer) checkFloatSeparators(text string, start int) {
	for i := range len(text) {
		if text[i] == '_' &&
			(i == 0 || !isDigit(text[i-1]) || i+1 == len(text) || !isDigit(text[i+1])) {
			l.diag(fmt.Sprintf("_ must separate successive digits in %s", text), start, l.pos)
			return
		}
	}
}

// prefixBase returns the base and the name of the integer literal whose
// bytes start b when they start with a prefix, 0x, 0b or 0o in either
// case, and 0 otherwise.
func prefixBase(b []byte) (int, string) {
	if len(b) < 2 || b[0] != '0' {
		return 0, ""
	}
	switch b[1] {
	case 'x', 'X':
		return 16, "hexadecimal"
	case 'b', 'B':
		return 2, "binary"
	case 'o', 'O':
		return 8, "octal"
	}
	return 0, ""
}

// prefixedInt lexes the rest of an integer literal of the given base,
// named name, whose prefix starts at start and has been consumed: every
// letter, digit and _ that follows, so a digit the base does not have is
// part of the literal and reported in it, rather than starting a name.
func (l *Lexer) prefixedInt(start, base int, name string) {
	data := l.file.Data
	for l.pos < len(data) && isIdentByte(data[l.pos]) {
		l.pos++
	}
	text := string(data[start:l.pos])
	digits := text[2:]
	switch {
	case strings.Trim(digits, "_") == "":
		l.diag(fmt.Sprintf("%s literal %s has no digits", name, text), start, l.pos)
	default:
		if bad, at := badDigit(digits, base); bad != 0 {
			l.diag(fmt.Sprintf("invalid digit %c in %s literal %s", bad, name, text),
				start+2+at, start+3+at)
		} else {
			l.checkSeparators(text, start, 2)
		}
	}
	l.addToken(Int, text, start, l.pos)
}

// badDigit returns the first byte of digits, the digits of a literal of
// base, that is neither a digit of the base nor _, and its index, or 0.
func badDigit(digits string, base int) (byte, int) {
	for i := range len(digits) {
		c := digits[i]
		if c != '_' && digitValue(c) >= base {
			return c, i
		}
	}
	return 0, 0
}

// digitValue returns the value of c as a digit of a base up to 16, or 16
// when it is none.
func digitValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return 16
}

// checkSeparators reports text, an integer literal starting at start whose
// digits start at index from, after its prefix if it has one, when a _ in
// it does not stand between two digits, or, after a prefix, right after
// the prefix before a digit, and reports whether it found none.
func (l *Lexer) checkSeparators(text string, start, from int) bool {
	for i := from; i < len(text); i++ {
		if text[i] != '_' {
			continue
		}
		before := i > from && text[i-1] != '_' || i == from && from == 2
		after := i+1 < len(text) && text[i+1] != '_'
		if !before || !after {
			l.diag(fmt.Sprintf("_ must separate successive digits in %s", text), start, l.pos)
			return false
		}
	}
	return true
}

// checkLeadingZero reports the integer literal text, which starts at start
// and ends at the current position, if it has more than one digit and
// starts with 0, suggesting it without its leading zeros.
func (l *Lexer) checkLeadingZero(text string, start int) {
	if len(text) < 2 || text[0] != '0' {
		return
	}
	digits := strings.TrimLeft(strings.ReplaceAll(text, "_", ""), "0")
	if digits == "" {
		digits = "0"
	}
	l.diag(
		fmt.Sprintf("integer literal %s has a leading zero; write %s", text, digits),
		start,
		l.pos,
	)
}

// handleComment lexes a comment: a block comment (see blockComment) or a
// line comment. Consecutive lines whose first bytes
// other than spaces and tabs are "//" merge into one Comment token, however
// each line is indented, and its value joins them with newlines, each line
// with its surrounding space trimmed. A blank line, empty or holding only
// spaces and tabs, ends the comment, as it ends a comment group in Go, so
// comment lines separated by one become separate tokens. A semicolon is inserted before the comment if
// the preceding token can end a statement; the newlines after it are left to
// Lex, which inserts none since a comment cannot end a statement.
func (l *Lexer) handleComment() bool {
	start, data := l.pos, l.file.Data

	if l.at("/*") {
		l.blockComment()
		return true
	}

	if !l.at("//") {
		return false
	}
	l.pos += 2

	// A newline continues the comment only when the very next line, past
	// its indentation, starts with "//"; otherwise the comment ends before
	// the newline, which is left for Lex. Only a single newline is consumed
	// here, so a blank line always ends the comment. The indentation is not
	// written to the value, which trims it anyway.
	var val strings.Builder
	for l.pos < len(data) {
		if n := l.newlineAt(l.pos); n > 0 {
			next := l.pos + n
			for next < len(data) && (data[next] == ' ' || data[next] == '\t') {
				next++
			}
			if !bytes.HasPrefix(data[next:], []byte("//")) {
				break
			}
			l.file.AddLineStart(l.pos + n)
			val.WriteByte('\n')
			l.pos = next + 2
			continue
		}

		val.WriteByte(data[l.pos])
		l.pos++
	}

	l.addSemi(start)
	l.addToken(Comment, trimCommentLines(val.String()), start, l.pos)

	return true
}

// trimCommentLines trims the surrounding space from each line of a merged
// comment's text, so every line is treated as the first one is: "// a\n// b"
// has the value "a\nb". The lines are kept, empty ones included, so the
// value has as many lines as the comment.
func trimCommentLines(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.Join(lines, "\n")
}

// blockComment lexes a block comment, from "/*" to the "*/" that closes
// it, as one Comment token whose value is the text between them with its
// surrounding space trimmed and each \r\n made \n, as a line comment's
// value has no carriage return. Block comments nest: each "/*" inside opens
// one that needs its own "*/", so code holding one can be commented out
// whole. As in Go, a block comment that spans a line break acts as a
// newline, so a semicolon is inserted before it when the preceding token
// can end a statement, and one on a single line acts as a space, inserting
// nothing. One that is never closed runs to the end of the input and is
// reported. The lines it spans are recorded, so later positions stay
// correct.
func (l *Lexer) blockComment() {
	start, data := l.pos, l.file.Data
	l.pos += 2

	depth, spansLines := 1, false
	for l.pos < len(data) && depth > 0 {
		switch {
		case l.at("/*"):
			depth++
			l.pos += 2
		case l.at("*/"):
			depth--
			l.pos += 2
		default:
			if n := l.newlineAt(l.pos); n > 0 {
				l.pos += n
				l.file.AddLineStart(l.pos)
				spansLines = true
				continue
			}
			l.pos++
		}
	}

	text := data[start+2 : l.pos]
	if depth == 0 {
		text = data[start+2 : l.pos-2]
	}
	if spansLines {
		l.addSemi(start)
	}
	value := strings.ReplaceAll(string(text), "\r\n", "\n")
	l.addToken(Comment, strings.TrimSpace(value), start, l.pos)
	if depth > 0 {
		l.diag("block comment not terminated", start, l.pos)
	}
}

// addSemi inserts an empty semicolon token at pos if the last token other
// than a comment can end a statement, so a block comment on one line
// before a newline is passed over as a space is. This is automatic
// semicolon insertion, following Go's rule.
func (l *Lexer) addSemi(pos int) {
	last := len(l.tokens) - 1
	for last >= 0 && l.tokens[last].kind == Comment {
		last--
	}
	if last >= 0 && endsStatement(&l.tokens[last]) {
		l.addToken(Semi, nil, pos, pos)
	}
}

// endsStatement reports whether a semicolon belongs after tk when a line ends
// there: identifiers, literals (true, false, noerror and c_allocator among
// them), ++ and --, the postfix ?, closing brackets, and the keywords
// break, continue, return and unreachable.
func endsStatement(tk *Token) bool {
	switch tk.kind {
	case Ident, Str, Int, Float, Char, Inc, Dec, Question, RParen, RBrack, RCurl:
		return true
	case Keyword:
		switch tk.val {
		case Break, Continue, Return, Unreachable, True, False, NoError, CAllocator:
			return true
		}
		return false
	default:
		return false
	}
}
