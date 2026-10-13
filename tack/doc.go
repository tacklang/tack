// Command tack is the Tack compiler driver.
//
// It reads the program its target names, runs it through the pipeline
// (loader, which lexes and parses, resolver, type checker, code generation
// backend), writes the generated
// source to a temporary file, and runs the backend's toolchain on it (clang,
// for the c backend) to produce a native binary at the path given by --out.
// The first stage that reports diagnostics ends the run; later stages never
// see a program an earlier one rejected.
//
// Usage:
//
//	tack --out <path> [flags] <target>
//	tack (--lex | --ast | --res | --emit) [flags] <target>
//	tack --backends
//
// The target is a file or a directory. A file is compiled alone. A
// directory is compiled as the one package its .tck files make (see
// package loader); a directory with no .tck files is an error. For a
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
//   - 0: the dump, list, help, or binary was produced.
//   - 1: the program was rejected with diagnostics, or a file could not be
//     read or written, or the toolchain failed or could not be run, or the
//     run was interrupted.
//   - 2: the command line was wrong: an unknown or malformed flag, no input
//     file or more than one, an empty file name, an unknown backend, a
//     negative --max-diags, more than one dump flag, --out missing when
//     building, --out given together with a dump flag, or --out naming the
//     input file itself.
//
// Usage errors are all detected before the input is read. --help wins over
// everything else on the command line, and --backends ignores the rest of
// it apart from unknown flags.
//
// # Building
//
// Before compiling, the driver checks that --out can be written: its parent
// must be an existing directory and it must not itself be a directory. The
// generated source goes to a file created with os.CreateTemp (so $TMPDIR
// decides where), which is removed on every path out of the build: success,
// a toolchain that fails or cannot be started, and an interrupt. On SIGINT
// the toolchain is killed and the temporary file removed before tack exits;
// a second SIGINT kills tack outright.
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
