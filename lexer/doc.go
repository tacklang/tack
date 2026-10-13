// Package lexer turns Tack source bytes into tokens. It is the first stage of
// the pipeline: it takes a *source.File and produces the []Token the parser
// consumes, plus diagnostics for anything it could not tokenize.
//
// The lexer works on bytes, not runes. Identifiers are ASCII letters, digits
// and underscores; any other byte outside a literal or comment is reported as
// an invalid character and skipped. Lexing continues past every error, so a
// single run reports all lexical problems in the file.
//
// A newline is a line feed, or a carriage return followed by one, and the
// two mean the same everywhere: they end a line, a comment, and an
// unterminated string or char literal, and imply a semicolon, placed at
// the newline's first byte. A carriage return not followed by a line feed
// is not a newline: outside a literal or comment it is an invalid
// character, and in a double-quoted string or a char literal it is a byte
// of the value.
//
// Along the way the lexer records where each line starts in the source.File,
// one past each line feed, which is what later lets a diagnostic's byte
// offset be shown as a line and column. A file must therefore be lexed
// exactly once.
//
// Semicolons are inserted automatically, following Go's rule: at a newline,
// before a comment, and at the end of input, a Semi token is added if the
// preceding token is an identifier, a literal, ++ or --, a closing bracket, or
// one of the keywords break, continue, return, true and false. An inserted
// Semi covers no bytes. The token stream always ends with an EOF token.
//
// Literals carry decoded values: double-quoted string and char literals
// have their escape sequences resolved, Go's single-letter ones and \x with
// exactly two hex digits, any other being reported and decoding to 0, and keywords carry their
// KeywordType. A backquoted string is raw, as in Go: its value is every byte
// up to the closing backquote, with no escapes decoded and carriage returns
// dropped, so `\n` is a backslash and an n and a backquote cannot appear
// inside. It may span lines, and one with no closing backquote runs to the
// end of the input and is reported as an unterminated string. A char literal
// holds exactly one byte, written as itself or as an escape sequence, and
// its value is a one-byte string; a character whose UTF-8 encoding is longer,
// such as 'é', is reported, as are an empty literal and one with several
// characters. Numbers are kept
// as their source text; converting them, and rejecting out-of-range values, is
// left to the parser. An integer literal is decimal, and one of more than
// one digit that starts with 0, such as 007 or 00, is reported here; 0
// alone is fine. A 0 followed by a letter, as in 0x10, is a 0 and then a
// name, since hex, octal and binary literals are not lexed.
//
// The lexer guarantees that tokens appear in source order, never overlap, and
// never extend past the end of the input. It checks this before returning and
// panics if it fails, since a violation is a bug in the lexer, not in the
// program being compiled.
//
// A line comment runs from "//" to the end of its line. Lines that each
// start with "//" after any spaces and tabs, with no line between them,
// become one Comment token however each is indented; a blank line, empty or
// holding only spaces and tabs, ends the token, as it ends a comment group
// in Go. A Comment token runs from its first "//" to the end of its last
// line, so it covers the indentation of every line after the first, but
// never the newline after it, a carriage return before the line feed
// included. Its value trims each line alike.
//
// Block comments ("/* ... */") are not part of the language, and "/*" is
// reserved: one is reported and skipped, up to its "*/" or the end of the
// input. The body of an asm block is skipped up to the next "}" without
// being tokenized, and an asm block with no "}" runs to the end of the input
// and is reported as unterminated, from its keyword to the end.
package lexer
