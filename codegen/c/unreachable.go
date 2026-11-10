package c

import (
	"strconv"
	"strings"
)

// cStringLiteral returns s as a C string literal: printable ASCII as it
// is, but for a double quote, a backslash and a question mark, which
// could start a trigraph, and every other byte as a three-digit octal
// escape, which no digit after it can extend.
func cStringLiteral(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '"' || ch == '\\' || ch == '?':
			b.WriteByte('\\')
			b.WriteByte(ch)
		case ch >= ' ' && ch <= '~':
			b.WriteByte(ch)
		default:
			oct := strconv.FormatUint(uint64(ch), 8)
			b.WriteString(`\` + strings.Repeat("0", 3-len(oct)) + oct)
		}
	}
	b.WriteByte('"')
	return b.String()
}
