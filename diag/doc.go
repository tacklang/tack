// Package diag defines compiler diagnostics and renders them for people.
//
// Every stage of the pipeline (loader, lexer, parser, resolver, type
// checker, code generation backend) reports problems in user input as
// []Diagnostic rather than by panicking or returning a Go error. A Diagnostic carries a Severity, a message, a Span
// into the source, and optionally related diagnostics, such as a note
// pointing at an earlier declaration a duplicate collides with.
//
// A Severity is Error, Warning or Note. An error stops compilation: a
// program with one is never generated, built or dumped. A warning reports
// something suspicious that stops nothing, a variable declared and never
// read, an import never used, a value computed and dropped, and is
// printed as one; a program with only warnings compiles as one with none
// does. A note only adds to another diagnostic, among its Related.
// ErrorCount and HasErrors count the errors of a list, which is what
// every stage's and the driver's decisions count, warnings aside. A
// warning about code never used, every one the compiler gives today, sets
// Unused, its span the code an editor may dim; nothing rendered shows it.
//
// Render and RenderAll produce one compact line per diagnostic, which the
// golden tests pin. RenderPretty and RenderAllPretty add the offending source
// line with a marker under the span, for the command line: a caret under its
// first byte and a tilde under each of the rest, with nothing after, the
// carriage return of a \r\n inside the span not counted. A
// source line is shown without its newline, \n or \r\n, so one saved with
// CRLF line endings looks the same as one saved with LF. Both
// take a source.Files, which finds the file each span points into by the
// source.FileID the span carries, so a diagnostic and its notes may be in
// different files of one package. A lone source.File stands for every span,
// as when one file is compiled; a source.Set holds the files of a program.
// Every file must already have its line starts recorded by the lexer. A span
// in no file the source.Files knows is a compiler bug, and is rendered with
// <unknown file> in place of a position rather than panicked on.
//
// Excerpt gives the lines RenderPretty writes below a diagnostic's header
// for a span alone, which the type checker gives a testing.Site at
// compile time, so a place a program reports at run time reads exactly
// as a diagnostic there does.
package diag
