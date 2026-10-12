// Package source holds the contents of a file being compiled and maps byte
// offsets in it to line and column positions.
//
// The driver creates a File from the bytes it read. The lexer records the
// offset at which each line starts as it scans, starting with 0, so a File
// can answer Position and Line queries only once lexing has run. Spans
// elsewhere in the compiler refer to a File by its FileID and to positions in
// it by byte offset; this package is what turns those offsets into the
// line:column a diagnostic shows.
//
// It is a leaf package and depends on nothing else in the compiler.
package source
