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

The driver is a stub. It answers `tack --backends` as the Go compiler does,
and to any other command line it writes

    tack: not implemented in the port: <first argument>

to standard error and exits with status 3, which the harness reads as a
stage the port does not implement yet.

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
