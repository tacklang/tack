package c_test

import (
	"strings"
	"testing"
)

// rawStringBytes is every byte value in order, except the backquote, which
// would end the raw string, and the carriage return, which a raw string
// drops. It holds a zero byte, a newline, a double quote, a backslash and
// every byte from 0x80 up, the bytes a C string literal would have to
// escape.
func rawStringBytes() string {
	var b strings.Builder
	for c := range 256 {
		if c != '`' && c != '\r' {
			b.WriteByte(byte(c))
		}
	}
	return b.String()
}

// TestRawStringHoldsEveryByteAtRuntime writes every byte a raw string can
// hold between backquotes in Tack source, and checks at run time, under the
// undefined behavior sanitizer, that the string has each one, in order,
// with nothing added or lost on the way through the C backend.
func TestRawStringHoldsEveryByteAtRuntime(t *testing.T) {
	src := program(
		"s := `"+rawStringBytes()+"`",
		"if len(s) != 254 {",
		"    exit(1)",
		"}",
		"mut j := 0",
		"for b := range 256 {",
		"    if b == 96 || b == 13 {",
		"        continue",
		"    }",
		"    v, ok := s[j]",
		"    if !ok || int64(v) != b {",
		"        exit(2)",
		"    }",
		"    j++",
		"}",
		"code, fits := int32(j - 200)",
		"if fits { exit(code) }",
	)
	if out, status := runSanitized(t, src); out != "" || status != 54 {
		t.Errorf("got stdout %q, status %d; want no output, status 54", out, status)
	}
}

// TestRawStringDropsCarriageReturnsAtRuntime checks that a raw string written
// with CRLF line endings, or holding a lone carriage return, has none in its
// value, as in Go, so a file's line endings do not change what it means.
func TestRawStringDropsCarriageReturnsAtRuntime(t *testing.T) {
	src := program(
		"s := `a\r\nb\r\n`",
		"t := `\r`",
		"u := `x\ry`",
		"if len(s) != 4 || s != \"a\\nb\\n\" || len(t) != 0 || u != \"xy\" {",
		"    exit(1)",
		"}",
		"print(s)",
		"exit(7)",
	)
	if out, status := runSanitized(t, src); out != "a\nb\n" || status != 7 {
		t.Errorf("got stdout %q, status %d; want %q, status 7", out, status, "a\nb\n")
	}
}
