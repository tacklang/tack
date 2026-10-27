// Command tack is the Tack compiler driver.
//
// It reads the program its target names, runs it through the pipeline
// (loader, which lexes and parses, resolver, type checker, code generation
// backend), writes the generated source to a temporary directory, compiles
// the C files of the program's packages there, and runs the backend's
// toolchain to link them with it (clang, for the c backend) into a native
// binary at the path given by --out.
// Every stage up to the one a dump asks for runs whatever an earlier one
// reported, and the diagnostics of all of them are reported together: the
// loader's syntax and import errors, the resolver's and, through code
// generation, the type checker's. A name or a type an earlier error leaves
// invalid is reported nothing more about (see packages resolver and
// typeck). The loader alone can end the run before the resolver, when it
// builds no program: a file that does not lex, for one (see package
// loader). Nothing is dumped, generated, compiled or linked when any stage
// reported an error, and the backend refuses such a program itself (see
// codegen.Refuse). A warning (diag.Warning) stops nothing: a run whose
// stages reported only warnings dumps, generates, builds and exits 0 as
// one with no diagnostic does, writing the warnings to stderr first. The
// diagnostics are reported sorted by file and then by where each starts,
// up to where the first error past --max-errors would be, 10 by default,
// followed, when some were left out, by the line "too many errors:
// showing the first 10; --max-errors 0 shows them all". Only errors count
// toward the limit, so a warning never takes an error's place; those
// among the errors shown are shown, and those after the last are left out
// with the errors (see renderDiagnostics). The parser reports every syntax
// error of a file, recovering after each (see package parser), and stops
// parsing once it has found more than --max-errors.
//
// Usage:
//
//	tack --out <path> [flags] <target>
//	tack (--lex | --ast | --res | --emit | --emit-header) [flags] <target>
//	tack --check [dump flag] [flags] <target>
//	tack --backends
//
// The target is a file, a directory, or an import path. A file is compiled
// alone. A directory is compiled as the one package its .tck files make,
// in the module whose tack.mod is in it or the nearest directory above it
// (see package loader); a directory with no .tck files, or no tack.mod at
// or above it, is an error. A target that names nothing on disk, is not
// absolute, does not start with a dot and does not end in .tck is an
// import path, looked up in the module found from the current directory.
// Files are shown relative to the current directory when they are in it or
// below it, and by their absolute paths otherwise. For a
// directory, --lex prints each file's tokens after a line naming it,
// -- <file> --, --ast prints the list of the files' trees, and --res the
// whole program; for a file, both print that file's tree, as before
// directories were targets. Every file of a directory is lexed, and every
// file parsed, so a problem in each is reported.
//
// Flags must come before the target; "--" ends the flags, for a file name that
// starts with a dash. Each flag can be spelled with one dash or two.
//
//	--lex             print the token stream and stop
//	--ast             print the parsed syntax tree as JSON and stop
//	--res             print the resolved syntax tree as JSON and stop
//	--emit            print the generated code and stop
//	--emit-header     write tack.h into the package directories and stop
//	--check           check without building, reporting every diagnostic
//	--backend <name>  use the code generation backend name (default c)
//	--backends        list the available backends, one per line, and stop
//	--out <path>      write the linked binary to path
//	--max-errors <n>  report at most n errors, then a line saying there are
//	                  more (default 10); 0 reports them all
//	--max-diags <n>   the old name of --max-errors
//	-O0 ... -O3, -Os, -Oz
//	                  optimize the build at that level (default -O3)
//	-h, --help        print the help text to stdout and exit 0
//
// # Output streams
//
// Standard output carries only what was asked for: the dump of a dump flag,
// the backend list for --backends, or the help text. A dump is the stage's
// output and nothing else, ending in a newline, so `tack --emit x.tck > x.c`
// writes a compilable C file. A build writes nothing of its own to stdout;
// the toolchain's stdout and stderr are passed through.
//
// Everything else goes to standard error: diagnostics, rendered by the diag
// package exactly as it formats them, and driver errors, which start with
// "tack: ". A usage error is followed by a pointer to --help.
//
// # Exit status
//
//   - 0: the dump, list, help, header, or binary was produced, or --check
//     found no diagnostic.
//   - 1: the program was rejected with diagnostics, or a file could not be
//     read or written, or the toolchain failed or could not be run, or the
//     run was interrupted.
//   - 2: the command line was wrong: an unknown or malformed flag, no input
//     file or more than one, an empty file name, an unknown backend, a
//     negative --max-errors or --max-diags, more than one dump flag (--emit-header counts
//     as one), --out missing when building, --out given together with a
//     dump flag or --check, two -O flags, an -O flag given together with a
//     dump flag or --check, or --out naming the input file itself.
//
// Usage errors are all detected before the input is read. --help wins over
// everything else on the command line, and --backends ignores the rest of
// it apart from unknown flags.
//
// # The standard library
//
// The builtins and the runtime are built into the compiler (see package
// builtin): print and exit, with their C names, the generic builtins, the
// runtime's C files, which define the bodies of print, exit and lists, and
// the runtime header, <tack/tack.h>. Nothing on disk changes them: std's
// std/builtin/builtin.tck and std/include/tack/tack.h are copies, for
// people and editors to read, which no build reads. The standard library is
// $TACK_HOME/src/std, or ~/.tack/src/std when TACK_HOME is unset or
// empty; `make install` copies the repository's src/std there. A relative
// TACK_HOME is taken from the current directory and made absolute. Every
// command that runs the resolver needs it, --res, --emit and
// --emit-header included, and finds it after the command line is checked
// and before the program is read; one that is not there, or is not a
// directory, ends the run with exit 1 and "tack: cannot find std at
// <path>; set TACK_HOME or run make install". --help, --backends, --lex and
// --ast need no builtins and do not look for it.
//
// The same directory is the module std, whose packages a program imports:
// the driver hands it to the loader as loader.Std, so import fmt from
// "std/fmt" reads $TACK_HOME/src/std/fmt, from a module or from a single
// file. A std package's files are named in diagnostics as the program's
// are, relative to the current directory when below it and absolute
// otherwise, and its C files are compiled with CompileCmd and checked and
// linked exactly as a program package's are (Building), not as the
// runtime's are.
//
// # Building
//
// Before compiling, the driver checks that --out can be written: its parent
// must be an existing directory and it must not itself be a directory. The
// build happens in a directory created with os.MkdirTemp (so $TMPDIR
// decides where), which is removed with everything in it on every path out
// of the build: success, a toolchain that fails or cannot be started, and
// an interrupt. On SIGINT the toolchain is killed and the temporary
// directory removed before tack exits; a second SIGINT kills tack outright.
//
// Every path given to the toolchain, the C files, the temporary directory's
// files and --out, is made absolute first, so that no file named like an
// option, -fplugin=x.c, is read as one.
//
// The runtime built into the compiler is written there first: its header
// as runtime/include/tack/tack.h, and its C files in runtime/builtin.
// runtime/include is the include directory every C file of the build is
// compiled against, the generated source, the program's C files, std
// packages' and the runtime's own, so <tack/tack.h> is always the
// compiler's, whatever $TACK_HOME/src/std/include holds.
//
// The generated source is written there as program.c. A program with C
// files, the .c files the loader found in each package's directory, a std
// package's included, gets
// the backend's header written there as tack.h, and each C file, in
// package order and then name order, is compiled to an object there with
// the backend's CompileCmd, which includes tack.h first and is given the
// runtime's include directory, absolute, where the <tack/tack.h> that
// tack.h includes when a signature uses a string is: the file is named
// relative to the current directory when it is below it, as clang's
// messages then show it. A C file that does not compile ends the build,
// after clang's own errors, with "tack: cannot compile <file>: clang
// failed: ...". The driver then reads each object's symbol table, Mach-O
// or ELF, with debug/macho and debug/elf, so no tool beyond clang is
// needed, for the external symbols it defines.
//
// When the program calls a builtin, has a list type, whose generated C
// grows and frees lists through the runtime's list.c, or has a C file
// whose object leaves undefined a symbol starting tack_, which only the
// runtime can define, every C file of the runtime, exit.c, list.c and
// print.c, is compiled there too, to std-<name>.o, with the backend's
// CompileStdCmd: CompileCmd's flags and level, with the runtime's include
// directory on the include path in place of the program's tack.h. Messages
// name a runtime file <runtime>/<name>, such as <runtime>/print.c, since
// the path it is written to is a temporary one. Each std
// object's symbols, defined and undefined, are read the same way, and the
// objects linked are chosen as a linker takes an archive's members: those
// defining a builtin the program calls, the list functions of a program
// with lists, and a tack_ symbol a C file of the program leaves undefined,
// and in turn those defining a symbol
// an object already chosen leaves undefined; a symbol no std object
// defines, the C library's, is left to the linker. No ar is run and no
// archive written. A program that needs none of these compiles no std file and
// links no std object, so it does not link the C library code print and
// exit call. A builtin the program calls that no runtime object defines,
// which only a broken compiler can have, is "tack: builtin print has no C
// definition: the runtime defines no tack_builtin_print".
//
// # The build cache
//
// A std object is compiled only when the build cache does not already
// hold it; the selection above runs on its symbols either way. The cache
// is $TACK_CACHE, or tack in os.UserCacheDir (~/Library/Caches/tack on
// macOS, $XDG_CACHE_HOME/tack or ~/.cache/tack on Linux) when TACK_CACHE is
// unset or empty, as Go's GOCACHE is found; a relative TACK_CACHE is taken
// from the current directory. std's entries are in its std directory,
// created when missing: std/keys/<key> holds the hex SHA-256 of an
// object, which is std/objects/<that hash>.o. A key is the SHA-256 of
// everything that decides the object: the cache's format, the backend's
// name, the whole CompileStdCmd with the paths of the C file, of the
// include directory and of the object left out, since they are temporary
// ones (the optimization level and every flag, -fno-finite-loops
// included, are in it, so an object compiled with other flags is never
// reused), the standard output of the
// backend's VersionCmd, clang --version, run once per build, the runtime
// header's bytes, and the C file's name and bytes. Every input but the
// toolchain is built into the compiler, so nothing on disk, $TACK_HOME
// included, changes what a key names. The C library's
// headers are not hashed; the toolchain's identity stands for them. A
// change to any input is a new key, never a stale hit. On a hit the object
// is copied into the temporary directory as std-<name>.o, as a compiled
// one would be there, so the link never reads the cache. An entry is not
// trusted when its key does not hold a hash, its object is missing or does
// not hash to it, or the object's symbols cannot be read: the file is
// compiled again and the entry replaced. Every cache file is written to a
// temporary file beside it and renamed into place, the object before the
// key naming it, so builds running at once never read half an entry; two
// that miss together both compile, and either's rename wins.
//
// The cache is needed only by a build that links a std object: a dump, or a
// program that calls no builtin, never looks for it. A build that needs it
// and cannot find it, because TACK_CACHE is unset and os.UserCacheDir
// fails, ends with "tack: cannot find the build cache: TACK_CACHE is not
// set and the user cache directory is unknown: ..."; one that cannot
// create its directories with "tack: cannot use the build cache <dir>:
// ..."; one that cannot write an entry with "tack: cannot write to the
// build cache <dir>: ..."; and one whose toolchain's VersionCmd cannot be
// run or fails with "tack: cannot identify the toolchain: clang --version
// failed: ...". None of them builds without the cache instead. Nothing
// trims the cache: deleting its directory clears it. The C files of the
// program and of std packages are compiled against the program's own
// tack.h, and are compiled on every build, never cached.
//
// A C file, the program's or a chosen std object's, that defines main,
// which the generated source does, or two that define one symbol, a C file
// of the program defining a builtin the program calls included, are
// "tack:" errors naming the files; two std files defining one symbol are
// an error whether or not either is chosen; an extern func whose C name no C file
// of its own package defines is a diagnostic at its declaration, "extern
// func write has no C definition in package os", rendered and limited by
// --max-errors like any other. Last, BuildCmd compiles program.c, with
// the runtime's include directory, for the <tack/tack.h> every generated source
// includes, and links the program's objects, then the chosen std objects,
// into --out.
//
// Both commands build at one optimization level, -O3 unless an -O flag
// gives another: -O0 to -O3, -Os or -Oz, clang's levels. -Ofast is not
// one, since it gives up the C standard's semantics that the generated
// source relies on. Help lists the -O flags as one entry, with one dash,
// as clang spells them; two dashes work too, as for every flag.
//
// --emit-header runs the pipeline as --emit does, then writes the
// backend's header, tack.h, into the main package's directory and that of
// every other package of its module with C files or extern funcs,
// replacing any there, and prints nothing. A tack.h whose signatures use a
// string includes <tack/tack.h>, which an editor finds with an installed
// std's include directory on its include path, where make install-std
// writes a copy of the runtime header. A package of another module, a
// std package, is not written into: it is not the program's. A build never reads those copies: CompileCmd's
// -include of the fresh one makes a stale one's include guard skip it.
//
// # Checking
//
// --check runs the whole pipeline, through code generation, and builds
// nothing: no toolchain is run, nothing is written to the temporary
// directory or the build cache, and it takes no --out or -O flag. It is
// for a library, an editor or a language server, which want every problem
// a target has rather than a binary. Its exit status is 0 when it finds no
// error and 1 when it finds any; warnings are written to stderr either
// way and change nothing.
//
// A target holding a main package is checked as the program a build would
// compile, with one difference: a main package that declares no main
// function gets the same diagnostic a build gives, main function not
// found, and the stages after the resolver run all the same, since none
// depends on main. A directory or import path holding another package, or
// no Tack file, is checked as a library: every package at or below it in
// its module, with everything those import, in one program (see
// loader.Loader.Lib). A main package below it is left out, and is checked
// by naming its directory; so is a directory starting with a dot and
// another module below it. A target with no package to check is a "tack:"
// error: "no .tck files in <dir> or below", or, when it holds only main
// packages, "no library package in <dir> or below; check a main package by
// naming its directory". A single file whose package is not main is
// checked as a library of that one package.
//
// Every stage runs whatever an earlier one reported, as without --check,
// and the diagnostics of all of them are sorted and capped by
// --max-errors as any run's are, the missing main function among them.
//
// --check combines with one dump flag, which prints its output for the
// packages checked, or writes tack.h for --emit-header, when the stages up
// to it found no error, together with the diagnostics of any later
// stage: --check --ast on a program with a type error prints the trees on
// stdout and the type error on stderr, and exits 1. --lex and --ast print
// what a dump without --check prints, from a load that stops after them,
// and the program is then loaded in full to be checked. On syntax errors,
// --check --ast prints the trees the parser recovered, each damaged
// declaration or statement a bad node holding its Span and, for a
// declaration, the name it was declaring when the parser got to it, with
// the errors; --ast without --check prints only the errors, and a lex error
// leaves no tree either way. For a library,
// --lex and --ast cover the files of every package checked, under one
// header per file as for a directory, and --res the whole program. The
// generated code and tack.h exist only for a program or library with no
// diagnostic at all, a missing main function included. A library's C has
// no C main, since it has no Tack main; the statements a C main would run
// first, giving a deep global its value, are the body of an unused static
// function, tack_init_. tack.h is written into every package of the
// library's module with C files or extern funcs, as for a program's
// packages other than main.
//
// # Structure
//
// main only wires the process to run, which takes the arguments, the
// environment (TACK_HOME and a way to find the home directory, TACK_CACHE
// and a way to find the user's cache directory, and a way to run the
// toolchain's VersionCmd) and the two output streams and returns the exit
// code. Nothing below main exits the
// process, reads the environment or touches package-level state, so tests
// drive run in process, each with the std and the cache it names.
// Errors flow back to run, which turns them into output and an exit code in
// one place (report): a *usageError for exit 2, a *diagnosticsError whose
// rendered text is printed as is, and any other error as a "tack:" line.
package main
