// Package lexer turns Tack source bytes into tokens. It is the first stage of
// the pipeline: it takes a *source.File and produces the []Token the parser
// consumes, plus diagnostics for anything it could not tokenize.
//
// The lexer works on bytes, not runes. Identifiers are ASCII letters, digits
// and underscores; any other byte outside a literal or comment is reported as
// an invalid character and skipped. Lexing continues past every error, so a
// single run reports all lexical problems in the file.
//
// Along the way the lexer records where each line starts in the source.File,
// which is what later lets a diagnostic's byte offset be shown as a line and
// column. A file must therefore be lexed exactly once.
//
// Semicolons are inserted automatically, following Go's rule: at a newline,
// before a comment, and at the end of input, a Semi token is added if the
// preceding token is an identifier, a literal, ++ or --, a closing bracket, or
// one of the keywords break, continue, return, true and false. An inserted
// Semi covers no bytes. The token stream always ends with an EOF token.
//
// Literals carry decoded values: string and char literals have their escape
// sequences resolved, and keywords carry their KeywordType. A char literal
// holds exactly one byte, written as itself or as an escape sequence, and
// its value is a one-byte string; a character whose UTF-8 encoding is longer,
// such as 'é', is reported, as are an empty literal and one with several
// characters. Numbers are kept
// as their source text; converting them, and rejecting out-of-range values, is
// left to the parser.
//
// The lexer guarantees that tokens appear in source order, never overlap, and
// never extend past the end of the input. It checks this before returning and
// panics if it fails, since a violation is a bug in the lexer, not in the
// program being compiled.
//
// Block comments ("/* ... */") are not supported: one is reported and
// skipped. The body of an asm block is skipped up to the next "}" without
// being tokenized, and an asm block with no "}" runs to the end of the input
// without a diagnostic. Escape sequences are decoded in backquoted strings as
// well as double-quoted ones.
package lexer
