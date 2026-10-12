package lexer

import (
	"fmt"
)

// Token is one lexical unit: its kind, its decoded value if it has one, and
// the byte offsets [offStart, offEnd) it covers in the source. An inserted
// semicolon covers no bytes.
type Token struct {
	val  any
	kind TokenKind

	offStart, offEnd int
}

// Kind returns the token's kind.
func (t *Token) Kind() TokenKind {
	return t.kind
}

// Value returns the token's value: the text of an identifier or number, the
// decoded contents of a string or char literal, a KeywordType for a keyword,
// the text of a comment, and nil for punctuation.
func (t *Token) Value() any {
	return t.val
}

// OffsetStart returns the offset of the token's first byte.
func (t *Token) OffsetStart() int {
	return t.offStart
}

// OffsetEnd returns the offset just past the token's last byte.
func (t *Token) OffsetEnd() int {
	return t.offEnd
}

// String formats the token for debugging, with offsets padded to two digits.
func (t *Token) String() string {
	return t.StringPadded(2)
}

// StringPadded formats the token for debugging as "[start:end] kind(value)",
// zero-padding the offsets to minW digits so a token dump lines up.
func (t *Token) StringPadded(minW int) string {
	minWFmt := fmt.Sprintf("%%0%dd", minW)
	posFmt := fmt.Sprintf("[%s:%s]", minWFmt, minWFmt)
	if s, ok := t.val.(string); ok {
		return fmt.Sprintf(posFmt+" %s(%q)", t.offStart, t.offEnd, t.kind.String(), s)
	}
	if s, ok := t.val.(interface{ String() string }); ok {
		return fmt.Sprintf(posFmt+" %s(%s)", t.offStart, t.offEnd, t.kind.String(), s.String())
	}
	return fmt.Sprintf(posFmt+" %s", t.offStart, t.offEnd, t.kind.String())
}
