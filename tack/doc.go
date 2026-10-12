// Command tack is the Tack compiler.
//
// It reads one source file, runs it through the pipeline (lexer, parser,
// resolver, type checker, code generation backend), and hands the generated
// source to the backend's toolchain to produce a native binary at the path
// given by --out. The first stage that reports diagnostics prints them and
// ends the process with status 1; later stages never see a program an
// earlier one rejected.
//
// The --lex, --ast, --res, and --emit flags stop the pipeline after the
// matching stage and print what it produced instead of building a binary.
// --backend picks the code generation backend and --backends lists them.
//
// Usage:
//
//	tack --out <OUT> [OPTIONS...] <FILE>
package main
