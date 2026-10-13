// Package diag defines compiler diagnostics and renders them for people.
//
// Every stage of the pipeline (lexer, parser, resolver, type checker) reports
// problems in user input as []Diagnostic rather than by panicking or
// returning a Go error. A Diagnostic carries a Severity, a message, a Span
// into the source, and optionally related diagnostics, such as a note
// pointing at an earlier declaration a duplicate collides with.
//
// Render and RenderAll produce one compact line per diagnostic, which the
// golden tests pin. RenderPretty and RenderAllPretty add the offending source
// line with a marker under the span, for the command line: a caret under its
// first byte and a tilde under each of the rest, with nothing after. A
// source line is shown without its newline, \n or \r\n, so one saved with
// CRLF line endings looks the same as one saved with LF. Both
// take the source.File the spans refer to, which must already have its line
// starts recorded by the lexer.
package diag
