# tack

This repository is the Tack compiler written in Tack, on its way to
compiling itself. Its Tack module is `tack`, and its driver, the `tack`
command, is the package in `driver/`.

It is a port of the compiler written in Go, in the tackc repository, which
is frozen at its tag `go-frozen` while the port is written. The Go compiler
is the reference: the port is checked against it differentially, stage by
stage, by the tests in tackc's `porttest` package, which run both compilers
on the same inputs and compare what each writes and how it exits.

## Status

The port has the compiler's first two stages: packages `source`, `diag`
and `lexer`, and `ast`, `parser` and `constant`, ported from the Go
compiler's packages of those names, `constant` as far as the parser
needs it: integer constants in full, and whether a float literal can be
held exactly, without the float constant's value yet. The driver answers
`tack --backends` as the Go compiler does, `tack --lex <file>` for a
single file, the token dump, and `tack --ast <file>`, the syntax tree as
JSON that decodes to the value the Go compiler writes; or the file's
lexical or syntax errors, its usage errors and its errors reading the
file, byte for byte as the Go compiler writes them. To any other
command line, and to a dump it cannot answer yet, it writes one line

    tack: not implemented in the port: <what>

to standard error, naming the first argument, an unknown flag, or what
it cannot do, as in `--lex of a directory`, and exits with status 3,
which the harness reads as a stage the port does not implement yet. A
file whose read fails with an error the port cannot word exactly as the
Go compiler would is answered the same way.

### Known differences

These are known, and not fixed yet:

- A failed write of the output is not reported. std's `fmt.write` and
  `fmt.ewrite` report no error, so when standard output or standard
  error cannot be written, to a full disk for one, the port exits 0
  where the Go compiler reports the failure and exits 1. Fixing it needs
  a std write that returns an error, planned with the driver at M8.
- The parser recurses, as the Go compiler's does, to the nesting limit
  of a thousand levels, on the C stack, which does not grow; the JSON
  dump walks the tree with a stack of its own. Binary operators are
  parsed with stacks of the parser's own, so precedence adds no frames.
  Built as the harness builds it, with the Go compiler's default -O3, the
  costliest shapes at the limit take, measured: switches 2.9 MB, ifs 2.8
  MB, closures and blocks inside ifs 2.6 MB, nested struct literals and
  call ladders through every precedence 2.3 MB, plain nested calls 2.1
  MB, parentheses 1.6 MB, of the 8 MB stack Linux gives a program by
  default. tackc's porttest runs these shapes, at the limit and past it,
  with the stack held to 4 MB. An -O0 build takes far more, 10 MB
  for a thousand nested parentheses and 13 MB for a thousand nested
  calls, and crashes on both at the default stack. The recursion rule planned
  for the port bounds this for every build.
- Neither compiler limits the size of a source file. A file that never
  ends, such as `/proc/self/pagemap`, is read until memory runs out, by
  both. A limit for the port is a follow-up.

## Building

The port is built with the Go compiler. With tackc checked out next to this
repository, as `../tackc`, and its compiler built there with `make build`:

    TACK_HOME=../tackc ../tackc/bin/tack --out bin/tack ./driver

run from this repository's root. `TACK_HOME` points the Go compiler at
tackc, whose `src/std` is the standard library the port is written against
until the standard library moves here. `TACK_CACHE` may also be set, to
keep the Go compiler's build cache somewhere other than the default cache
directory.

## Checking against the Go compiler

From tackc:

    make port-test

builds the Go compiler and this port, and runs every stage of `porttest`
over its corpus: the programs under tackc's `examples/` and the cases of
its txtar archives. It runs

    TACK_PORT_TEST=1 go test -count=1 ./porttest

since the check runs only with `TACK_PORT_TEST=1`, and only uncached:
`go test` would otherwise replay a result that does not see a change to
this repository. A stage the port does not implement yet skips, unless
it is listed in tackc's `porttest/required.txt`, where it then fails. The
harness reads:

- `TACK_PORT`, this repository's path, `../tack` by default;
- `TACK_PORT_BIN`, a built port to use instead of building one;
- `TACK_GO`, a built Go compiler to use instead of building one.

A relative path in any of them is taken from tackc's root, wherever the
command is run from, so `TACK_PORT=../tack` names this repository when it
sits next to tackc. Once `TACK_PORT_TEST=1` asks for the check, a port
that is not there fails it, whether `TACK_PORT` names the directory or
it is the default: a source tree without `driver/` is never skipped.

`TACK_PORT_BIN` set to the Go compiler's binary runs the Go compiler
against itself, which passes every stage, and checks the harness rather
than the port. The package documentation of `porttest` describes the
protocol, the stages and the corpus in full.

From tackc,

    TACK_PORT=<this repository> make port-fuzz FUZZTIME=60s

fuzzes `--lex`: it builds both compilers once and runs tackc's
FuzzLexDiff, which compares their output on generated inputs.
`PORT_FUZZ=FuzzParseDiff` fuzzes `--ast` the same way.
