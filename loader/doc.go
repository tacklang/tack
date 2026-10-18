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
// A target is a single file, a directory, or an import path. A file is
// compiled on its own, as the one file of a package whose path is main,
// with no tack.mod. A directory is compiled in its module, as the main
// package its Tack files make together, and an import path names such a
// directory in the module found from a given directory (Path). A package's
// files are every file directly in its directory
// whose name ends in .tck, in the byte order of their names, without
// recursing, leaving out a name that starts with a dot, as an editor's or a
// tool's, and a directory, even one whose name ends in .tck. A directory
// with no such file is an error, not a diagnostic, since there is no source
// to point into; so is a file or directory that cannot be read.
//
// # Libraries
//
// With Loader.Lib set, which the driver's --check does, a directory or
// import path holding a package other than main, or no Tack file, is
// loaded as a library: every package in a directory at or below it, in
// the byte order of the directories' paths, with everything each imports,
// in one Program that has no main package. A directory whose name starts
// with a dot is left out, with everything below it, as is one holding a
// tack.mod, another module's root, and std's root. A main package is left
// out too, decided by the first file's package clause; it is loaded by
// naming its own directory. Each package is loaded as an import is, so one
// imported by another package of the library is loaded once, and a cycle
// between them is an import cycle. A package that fails to lex or parse
// stops loading, as an import does. With Stop set, the Result gets the
// tokens or trees of every package of the library, and of none it
// imports. No package to load is an error, which says so when only main
// packages were found. A directory holding a main package loads as it
// does without Lib.
//
// # C files
//
// Every package records the directory it was read from, Dir, and the path
// of every C file directly in it, CFiles: each file whose name ends in .c,
// in the byte order of their names, leaving out a name that starts with a
// dot and a directory, as for Tack files; a C file whose name starts with -
// is an error, since clang could read it as an option. They define the package's extern
// funcs, and the driver compiles and links them; the loader does not read
// them. A single file's package has the directory the file is in, and the
// C files there, whatever other Tack files the directory holds. A single
// file's directory is not listed when loading stops after lexing or
// parsing, and one that cannot be listed is an error.
//
// # Modules
//
// A directory belongs to the module whose root is the nearest directory at
// or above it holding a tack.mod, parsed by package modfile. None found is
// an error; a tack.mod that exists but cannot be read stops the search
// with an error rather than being skipped. A malformed tack.mod is
// reported as diagnostics, at StageLoad, against the tack.mod itself,
// which is added to the Result's files like any source.
//
// Every package of a program records its module's name in
// ast.Package.Module, empty for a program built from a single file. A
// package's import path is the module's name, then its directory's path
// below the root; the root's own is the module's name. Its name is the
// last part of its import path, and every file's package clause must give
// that name or main, which may be in any directory: one giving another
// does not match its directory, and both in one directory are two
// packages. A name other than main must be a valid package name: lowercase
// ASCII letters and digits starting with a letter, not a keyword, a
// builtin type's or function's name, builtin or std. The main package of a
// program must be a main package.
//
// An import path starts with a module's name, never with . or /, and its
// parts are neither empty nor . or ..; a relative or absolute one is
// reported with the module-rooted path it probably means. A path outside
// the module is an unknown module, and one naming no directory in it a
// package that cannot be found.
//
// # Imports
//
// The main package's imports are loaded depth first, in file order and
// source order, each package once, by its import path: a package imported
// again is the one already loaded, so a diamond loads its shared package
// once, and one imported while its own imports are being loaded closes an
// import cycle, reported at the import that closes it with every package on
// the cycle, calc/a imports calc/b imports calc/a. Each import's Package is
// set to the package it names, and the program lists every package after
// those it imports, the main package last. An import of the main package,
// or of any package main, is an error, as is one of std/builtin, which
// documents the builtins. A program built from a single file has no
// module, so it can import only the standard library. An
// import whose path is malformed, is in another module, names no
// directory, or names one holding no Tack files or a package whose clauses
// are wrong, is reported at the import's path, and loading goes on with
// the next, so one run reports every such import; its Package is left nil,
// and the program is built without it, for the resolver, which reports
// nothing about what the import would have named. A package that fails to
// lex, has a file nested too deeply to be parsed, or has a package clause
// that could not be parsed stops loading there, with its diagnostics and
// no program; one with syntax errors is loaded, its trees holding bad
// nodes where the errors were (see package parser).
//
// # The standard library
//
// The standard library is the module std, whose root is the directory
// Loader.Std names, read through its own FS or the Loader's. An import
// path whose first part is std always names a package of std, whichever
// package imports it: std/fmt is the directory fmt below the root, loaded
// as any package is, with Module std, its Dir and CFiles paths in std's
// FS, and its files added to the Result like the program's, named by
// Display. A package of std may import only std packages, and an import
// of std with no Loader.Std is an error. std is never read as a module of
// the program: its tack.mod is not read, and its name, std, is one modfile
// gives no other module. A directory to build that is at or below std's
// root, read through the Loader's own FS, is in the module std; one
// anywhere else whose tack.mod says module std is an error at that name.
// What std's packages may use of each other, lib included, is the
// resolver's to check, by Module, as for any two packages.
//
// # Stages
//
// Every file of a package is lexed before any is parsed, and every file is
// parsed before the package is checked as a whole, so one run reports the
// problems of every file. Loading goes on past syntax errors and imports
// that cannot be loaded, so that the resolver and the type checker can
// report their own problems with the rest of the program; it stops, with
// no program, after a lexing error, since the lexer drops what it cannot
// read and parsing what is left would only report errors that follow from
// it, and after a malformed tack.mod or a main package whose package
// clauses are wrong. A Result carries every diagnostic found, with the
// first Stage that found any: StageLex, StageParse, or StageLoad for
// problems with how files fit together, such as files of one directory
// whose package clauses disagree. Loader.Stop asks for a dump of the
// tokens or the syntax trees of the target's own files instead of a
// program.
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
// A Result has a Program, or diagnostics, or both, unless loading was
// stopped early at Loader.Stop. A Program's Errors is the number of the
// Result's diagnostics, which the resolver and the type checker add theirs
// to; code is generated only from a program with none. Every package of
// the Program has at least one file, and every file of a package has a
// package clause that parsed. An import's Package is nil exactly when it
// was reported. The main package, the one being built, is the last; a
// library, loaded with Lib, has no main package, and its last package is
// any of its own.
package loader
