// Command unicodegen writes std/unicode's tables, src/std/unicode/tables.tck,
// from Go's own unicode package, so std/unicode classifies every rune
// exactly as the Go that built the generator does:
//
//	go run ./internal/unicodegen src/std/unicode/tables.tck
//
// or `make unicode-tables`. It takes the output path, relative to the
// working directory (the module root under go run), and exits 1 if the
// file cannot be written and 2 on a usage error.
//
// For each class std/unicode answers, print, graphic, letter, digit and
// space, it asks Go's predicate, unicode.IsPrint, IsGraphic, IsLetter,
// IsDigit or IsSpace, of every rune from 0 to 0x10FFFF, and writes the
// runs of consecutive runes the predicate holds for as one array of
// int32 pairs, each run's first rune and its last, in ascending order,
// with no two runs touching. Asking the predicate, rather than copying
// Go's RangeTables, folds in what Go decides outside its tables, Latin-1's
// own properties table and IsPrint's exclusion of every space but U+0020,
// and gives every class the same shape: std/unicode answers a class by a
// binary search of its runs. Go's tables also have strides, which pack
// runs such as every other rune of a script into one entry; runs of
// consecutive runes alone are simpler to search and fit in under 20 KB.
//
// The file names the Unicode version of the Go that wrote it,
// unicode.Version, and TestTablesAreCurrent fails while the committed
// file differs from what this Go would write, so a Go upgrade that
// changes Unicode, or a hand edit, is caught until the tables are
// regenerated.
package main
