// Command tack is the Tack compiler driver.
//
// It reads the program its target names, runs it through the pipeline
// (loader, which lexes and parses, resolver, type checker, code generation
// backend), writes the generated source to a temporary directory, compiles
// the C files of the program's packages there, and runs the backend's
// toolchain to link them with it (clang, for the c backend) into a native
// binary at the path given by --out.
// The first stage that reports diagnostics ends the run; later stages never
// see a program an earlier one rejected.
//
// Usage:
//
//	tack --out <path> [flags] <target>
//	tack (--lex | --ast | --res | --emit | --emit-header) [flags] <target>
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
// file parsed, before the run stops, so a problem in each is reported.
//
// Flags must come before the target; "--" ends the flags, for a file name that
// starts with a dash. Each flag can be spelled with one dash or two.
//
//	--lex             print the token stream and stop
//	--ast             print the parsed syntax tree as JSON and stop
//	--res             print the resolved syntax tree as JSON and stop
//	--emit            print the generated code and stop
//	--emit-header     write tack.h into the package directories and stop
//	--backend <name>  use the code generation backend name (default c)
//	--backends        list the available backends, one per line, and stop
//	--out <path>      write the linked binary to path
//	--max-diags <n>   print at most n diagnostics; 0, the default, prints all
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
//   - 0: the dump, list, help, header, or binary was produced.
//   - 1: the program was rejected with diagnostics, or a file could not be
//     read or written, or the toolchain failed or could not be run, or the
//     run was interrupted.
//   - 2: the command line was wrong: an unknown or malformed flag, no input
//     file or more than one, an empty file name, an unknown backend, a
//     negative --max-diags, more than one dump flag (--emit-header counts
//     as one), --out missing when building, --out given together with a
//     dump flag, or --out naming the input file itself.
//
// Usage errors are all detected before the input is read. --help wins over
// everything else on the command line, and --backends ignores the rest of
// it apart from unknown flags.
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
// The generated source is written there as program.c. A program with C
// files, the .c files the loader found in each package's directory, gets
// the backend's header written there as tack.h, and each C file, in
// package order and then name order, is compiled to an object there with
// the backend's CompileCmd, which includes tack.h first: the file is named
// relative to the current directory when it is below it, as clang's
// messages then show it. A C file that does not compile ends the build,
// after clang's own errors, with "tack: cannot compile <file>: clang
// failed: ...". The driver then reads each object's symbol table, Mach-O
// or ELF, with debug/macho and debug/elf, so no tool beyond clang is
// needed, for the external symbols it defines. A C file that defines main,
// which the generated source does, or two that define one symbol are
// "tack:" errors naming the files; an extern func whose C name no C file
// of its own package defines is a diagnostic at its declaration, "extern
// func write has no C definition in package os", rendered and limited by
// --max-diags like any other. Last, BuildCmd compiles program.c and links
// the objects into --out.
//
// --emit-header runs the pipeline as --emit does, then writes the
// backend's header, tack.h, into the main package's directory and that of
// every other package with C files or extern funcs, replacing any there,
// and prints nothing. A build never reads those copies: CompileCmd's
// -include of the fresh one makes a stale one's include guard skip it.
//
// # Structure
//
// main only wires the process to run, which takes the arguments and the two
// output streams and returns the exit code. Nothing below main exits the
// process or touches package-level state, so tests drive run in process.
// Errors flow back to run, which turns them into output and an exit code in
// one place (report): a *usageError for exit 2, a *diagnosticsError whose
// rendered text is printed as is, and any other error as a "tack:" line.
package main
