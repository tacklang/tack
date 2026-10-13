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
// take a source.Files, which finds the file each span points into by the
// source.FileID the span carries, so a diagnostic and its notes may be in
// different files of one package. A lone source.File stands for every span,
// as when one file is compiled; a source.Set holds the files of a program.
// Every file must already have its line starts recorded by the lexer. A span
// in no file the source.Files knows is a compiler bug, and is rendered with
// <unknown file> in place of a position rather than panicked on.
package diag
