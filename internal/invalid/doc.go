// Package invalid reads and writes examples/invalid.txtar, the file that holds
// every invalid example program of one file together with the output the
// compiler must produce for it, examples/invalid-packages.txtar, which
// does the same for the invalid programs of several files, and
// examples/invalid-c.txtar, for those with C files. examples/warnings.txtar
// and examples/warnings-packages.txtar, at WarningsPath and
// WarningsTreePath, are in the formats of invalid.txtar and
// invalid-packages.txtar, for programs the compiler accepts with
// warnings, each case's expected section the warnings alone.
//
// It is test infrastructure, not part of the compiler: the golden harness in
// test/ checks each case's expected output and rewrites it under -update, and
// the lexer, parser, and driver tests use the sources as inputs and fuzz
// seeds.
//
// # Format
//
// The file is a txtar archive: an optional free-form comment, then a sequence
// of files, each introduced by a header line of the form
//
//	-- <file name> --
//
// whose content runs up to the next header or the end of the file. A case
// named n is two files, in this order:
//
//	-- n/main.tck --   the program, byte for byte
//	-- n/expected --   what the compiler reports for it
//
// optionally followed by an empty marker file
//
//	-- n/no-final-newline --
//
// which says the program does not end with a newline. txtar ends every
// non-empty file with one, so without the marker a program whose last line
// is unterminated could not be stored exactly; with it, Parse drops the
// newline the format added.
//
// # Invariants
//
// Parse is strict, so a hand edit that breaks the structure fails loudly
// instead of quietly changing what a case tests. It rejects a header with
// stray spaces or an empty name, a file that does not belong to the case
// before it, a case without an expected output, case names that are not in
// strictly increasing byte order (which also rules out duplicates), a
// non-empty marker or one after a program whose last line is empty, and an
// archive that does not end with a newline. So each list of cases has
// exactly one archive.
//
// Format refuses what txtar cannot hold: content with a line that would read
// as a header, an empty or newline-free expected output, and names that are
// empty, contain a slash or a space, or are out of order. For every list of
// cases Format accepts, Parse returns that list unchanged.
//
// # Tree cases
//
// examples/invalid-packages.txtar is a txtar archive too, read by ParseTree
// and written by FormatTree. A case named n is any number of files, at
// least one, each n/<path> with a clean relative path below n, followed by
//
//	-- n/expected --   what the compiler reports for it
//
// The case is built from the directory n, or from the directory that an
// optional file n/.target names, relative to n; its name starts with a dot,
// so the loader never reads it as source. A .target naming a Tack file
// builds that file alone, as a single-file program. Diagnostics name each
// file n/<path>.
//
// A case is built with the repository's standard library, src/std, unless
// it has one of its own: its files under n/.std, which then stands in for
// all of std, so a case can give std the packages a diagnostic needs,
// such as one with lib declarations, without adding them to the real one.
// StdDir names that directory. A file may not repeat in a case, and every file must end with a
// newline, since this format has no no-final-newline marker; a program
// whose last line has none belongs in invalid.txtar. ParseTree is as strict
// as Parse, and FormatTree as Format: for every list of tree cases
// FormatTree accepts, ParseTree returns that list unchanged.
//
// # Cases with C files
//
// examples/invalid-c.txtar holds the invalid programs whose problem shows
// only once their C files are compiled: a C file that does not compile, or
// that defines what it must not, and an extern func that no C file
// defines. It is an archive of tree cases, in the same format, read by
// ParseTree, and a case's files include its .c files. A single-file
// program names main.tck in its .target file. The driver's tests build
// each case with clang, from a directory holding the case's files, and
// compare what tack writes to standard error with the expected output.
// clang's own report of a C file it cannot compile varies with its
// version, so for such a case the expected output holds only what tack
// writes after it, from its first line starting tack:, and the test checks
// that clang reported an error before it.
package invalid
