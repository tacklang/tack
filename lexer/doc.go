// Package lexer turns Tack source bytes into tokens. It is the first stage of
// the pipeline: it takes a *source.File and produces the []Token the parser
// consumes, plus diagnostics for anything it could not tokenize.
//
// own is a keyword like mut, reserved everywhere, though only a parameter
// and a call's argument use it. ensures is one too, reserved everywhere,
// though only a function's contract, after its results, uses it.
// unreachable is a keyword that is a statement of its own, so it is never
// a name, and so is fallthrough. scope is reserved everywhere too, though
// only a parameter, in a function's signature or in a function type, uses
// it. arena is a keyword like list, starting an arena type, arena[T],
// and so is pool, starting a pool type, pool[T].
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
// at a line comment, and at the end of input, a Semi token is added if the
// preceding token is an identifier, a literal, ++ or --, the postfix ?, a
// closing bracket, or one of the keywords break, continue, return,
// unreachable, fallthrough, true, false, noerror and c_allocator, so a
// line may end in f()?. An inserted Semi covers no bytes. One a line
// comment calls for comes right after the comment token and sits where the
// comment's first line ends, at its newline or at the end of the input, the
// place it has when the line holds no comment, so a diagnostic about it does
// not move with a comment. The token stream always ends with an EOF token.
//
// ? is a token of its own, Question, the postfix operator that passes a
// call's error on; it starts no longer token.
//
// Each token records whether it starts a line, no token before it on its
// line (Token.StartsLine), which the parser's error recovery uses to find
// where the next declaration starts.
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
// left to the parser (see constant.Parse). An integer literal is decimal,
// or hexadecimal, binary or octal after a 0x, 0b or 0o prefix in either
// case, which takes every letter, digit and _ after it, so 0x1g is one
// literal with a bad digit, reported here. A _ must stand between two
// digits, or right after a prefix; a prefix needs a digit; and a decimal
// literal of more than one digit that starts with 0, such as 007, 00 or
// 0_7, is reported here, 0 alone being fine. A float literal is decimal,
// as Go writes one: digits with a fraction point, 1.5 or 1., a point and
// digits, .5, either of them or plain digits with an exponent, e or E,
// an optional sign and digits, 1e9 or 1.5e-3, and _ only between two
// digits. It may start with 0, 01.5, as in Go. An exponent with no
// digits, 1e, is reported here. There are no hexadecimal floats: 0x1.8
// is an integer and then a float, .8, which the parser reports.
//
// The lexer guarantees that tokens appear in source order, never overlap, and
// never extend past the end of the input, with one exception: the Semi after
// a line comment of several merged lines sits at the newline ending the
// first, inside the comment's span. It covers no bytes, and the token after
// it starts past the comment. The lexer checks this before returning and
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
// A block comment, "/* ... */", is one Comment token, never merged with
// line comments, whose value is the text between its markers with the
// surrounding space trimmed. Block comments nest: each "/*" inside one
// needs its own "*/". As in Go, one that spans a line break acts as a
// newline, a semicolon going before it when the token before can end a
// statement, and one on a single line acts as a space; semicolon insertion
// looks past comments to the last token that is not one. One never closed
// runs to the end of the input and is reported, "block comment not
// terminated".
package lexer
