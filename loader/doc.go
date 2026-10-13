// Package loader finds the files of a program, lexes and parses them, and
// puts them together into packages.
//
// It is the front of the pipeline: it takes what the command line names
// and produces an *ast.Program for the resolver, along with the
// source.Set every later stage renders its diagnostics against. Lexing and
// parsing happen here because what a program is depends on what its files
// say: a file's package clause decides which package it belongs to.
//
// # Targets
//
// A target is a single file or a directory. A file is compiled on its own,
// as the one file of a package whose path is main. A directory is compiled
// as the package its Tack files make together: every file directly in it
// whose name ends in .tck, in the byte order of their names, without
// recursing, leaving out a name that starts with a dot, as an editor's or a
// tool's, and a directory, even one whose name ends in .tck. A directory
// with no such file is an error, not a diagnostic, since there is no source
// to point into; so is a file or directory that cannot be read.
//
// # Stages
//
// Every file of a package is lexed before any is parsed, and every file is
// parsed before the package is checked as a whole, so one run reports the
// problems of every file at the first stage that finds any. A Result
// carries those diagnostics with the Stage that found them: StageLex,
// StageParse, or StageLoad for problems with how files fit together, such
// as files of one directory whose package clauses disagree. Loader.Stop
// asks for a dump of the tokens or the syntax trees of the target's own
// files instead of a program.
//
// # Names
//
// Paths are slash-separated, whatever the host. Every file read gets a
// source.FileID equal to the name its diagnostics show, which
// Loader.Display gives, so IDs are unique and the same from run to run.
//
// # Files
//
// The loader reads through FS, a two-method interface, rather than the
// operating system directly: OS is the host's file system, and FromFS
// adapts any io/fs.FS, which is how tests load programs from an in-memory
// one. A self-hosted compiler needs only an equivalent of those two
// methods.
//
// # Invariants
//
// A Result has a Program exactly when it has no diagnostics and loading
// was not stopped early. Every package of the Program has at least one
// file, and every file of a package has the package's name in its package
// clause. The main package, the one being built, is the last.
package loader
