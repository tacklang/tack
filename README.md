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

The port has the compiler's first four stages: packages `source`,
`diag` and `lexer`; `ast`, `parser` and `constant`, ported from the Go
compiler's packages of those names, `constant` as far as the parser
needs it: integer constants in full, and whether a float literal can be
held exactly, without the float constant's value yet; `modfile` and
`loader`, which find a program's files, in a directory, a module
(`tack.mod`) and the packages it imports, std's among them, and put
them together, as the Go compiler's do, but for its libraries, which
`--check` loads, and its test mode, which `tack test` uses; and
`resolver`, which binds every name of the program, with `types`, the
basic types and the `Table` that gives every other type its ID, `cname`,
whole, and
`builtin`, the builtins' names and the files the compiler carries
inside it: the builtins' documentation and the C runtime with its
header, which `tools/embed` writes into `builtin/embedded.tck` as
string literals, since Tack has no `go:embed` (see package builtin).
`typeck`, the type checker, has its declaration phase, every struct
and union type, every global with its constant value, and every
signature, and `constant` float constants with it, exact rationals
rounded to float32 and float64; and the bodies, their statements and
expressions, calls, facts, arithmetic and relations, loops and
contracts, ownership, generics and closures, with the instances of
generic functions a program needs, as the Go compiler checks them. A
judgment the port cannot make yet, as a declaration's constant after
an error may need, is declined (see package typeck).
The type checker hands what it found to code generation as one
record, `typeck.Typed`: the program, its files, its type table and
every annotation the Go compiler's checker writes on the tree that its
C backend reads (see package typeck). `codegen` is the backend's
contract, ported from Go's package codegen: the backends there are, the
optimization levels, and Refuse, which keeps a program with errors from
being lowered; of the C backend, the header, `tack.h`, with the names
it may not give: a program whose types, fields or extern funcs `tack.h`
could not name is rejected as the Go compiler rejects it, before
anything is lowered, and the header is rendered as Go's backend renders
it. The lowering is ported in stages, as tackc's porttest stages its
cases: the core, E1, is here, a program of scalars, strings, globals,
functions, mut parameters, every loop and if, the operators, floats,
conversions that cannot fail, extern funcs, print and exit lowered to
the C the Go compiler writes, byte for byte, with every temporary and
pooled string numbered as Go numbers them; and values, E2, are here
too: structs, arrays and unions with their equality helpers, methods,
switches of every kind, slices, views, any, the division and shift
helpers, several values and the checked forms, and globals too deep for
a C initializer; and owned values and containers, E3, are here too:
lists, maps, arenas and pools with their helpers, hashes and frees,
defers and the cleanup chains every exit runs, ?, moves, drop, replace,
swap and regions. What a later stage lowers, generics, closures and
function values, is answered as not implemented where the Go compiler
would start lowering it: the program is lowered into buffers, so a decline writes nothing
but its one line, neither C nor the warnings, which the driver writes
only once the lowering, and any `tack.h`, is done (see package
codegen). `types` has the
bindings the backend lowers a generic function's instances under, Go's
`Bind` and `Bound` among them, which add the types they make in Go's
order, since C names carry type IDs, with a twin of every query of the
type table that answers, and adds, as Go's does under a binding, and
the package that declares a struct or a union, which names its C
struct; `builtin` what a program needs of
the runtime, Go's `Used`, `RunsC`, `UsesUnreachable` and `UsesLists`;
and `constant` the hexadecimal float literals the backend writes.
The driver answers `tack --backends` as the Go compiler does; `tack
--lex` and `tack --ast` of a single file, a directory or an import
path, the token dump, a header line before each file's when there are
several, and the syntax trees as JSON that decodes to the value the Go
compiler writes, a list of them for a package; and `tack --res`, for
which it loads the program in full, with the standard library, and
resolves it: the resolved tree, a single file's taken as Go's driver
takes it, the first tree of the program's first package, and a
package's as the whole program, with the warnings, or the diagnostics
of loading and resolving together. Its usage errors, its errors
reading files and directories, and every diagnostic are written byte
for byte as the Go compiler writes them. To any other command line,
and to what it cannot answer yet, it writes one line

    tack: not implemented in the port: <what>

to standard error, naming the first argument, an unknown flag, or what
it cannot do, and exits with status 3, which the harness reads as a
stage the port does not implement yet. A file or directory whose
reading fails with an error the port cannot word exactly as the Go
compiler would is answered the same way (see package loader). `--emit`
and `--emit-header`, with `--check` or without, run every stage before
the backend as the Go compiler does and report their diagnostics, then
lower the program: `--emit` writes its C, `--emit-header` writes
`tack.h` into the directories of the packages that need one, as the Go
compiler's writeHeaders does, and `--check` alone shows what the
backend reports, with the warnings; each is answered as not
implemented for a program the backend's port does not lower yet.
`--emit-header` reports a `tack.h` it cannot open as the Go compiler
does, `cannot write` and the file, and declines any other failed
write; as in Go, the files it wrote before stay written. `--check` of
`std/builtin` or its `builtin.tck` compares the copy with the
compiler's own, as the Go compiler does; one that may be that copy
reached through a symbolic link, which the port cannot resolve, is
answered as not implemented. The flags
`-O0` to `-Oz` and `--backend` are read, and their usage errors
written, as the Go compiler writes them.

The driver has flags the Go compiler has not. `--port-loaded`
writes what a load in full gives: the program's packages, in order,
their directories, files and C files, what each import is bound to, the
error count, and every diagnostic in the order it was reported.
tackc's porttest compares it with what the Go loader gives, since
`--res` shows none of the directories, C files, the count or the
bindings of imports no qualified name uses, and dumps nothing for a
program with an error; it stays until the builds check those.
`--port-types` writes what the type checker finds, as flat JSON: every
type of the table with every query it answers, every declaration with
the types and the value the checker recorded on it, the instances of
generic functions, the errors, every node with what the checker
recorded on it once every body is checked, and
the kinds of judgment the port took as holding without proving them,
which tackc's porttest compares with what the Go checker finds, on
every program of the corpus the Go compiler accepts. It is written
from the checker's handover, so it holds everything the backend will
read: the method and the stand-in each call calls, a `?`'s values, an
unreachable's place, the testing.Site arguments the checker passes and
what each holds, among the rest. `--port-floats` writes the C literal
the backend writes for each float a file lists by its bits, and
`--port-bind` what the type table's bindings give a program's types
under each instance's binding; porttest compares both with the Go
compiler's. `--port-header` writes `tack.h` for a checked program as the
backend renders it, with nothing lowered, and `--port-reserved` the
names `tack.h` may not give and the type ids an `any` holds; porttest
compares them with the Go backend's `Header` and its own tables.

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
- An entry of a directory that can be listed but not searched, a
  directory with read but no execute permission, cannot be told a file
  or a directory: std/os lists names alone, and the port lstats each
  entry, which fails there, where Go reads the type from the listing
  and goes on to fail opening the package's files. The port answers
  not implemented for it, wherever it is met: as a target, an import,
  or an import path named on the command line.
- A file that cannot be read, a `.tck` file with no read permission,
  gives `permission denied` from its open in Go; std/os's read_file
  does not say whether its open or its read failed, so the port answers
  not implemented, unless a directory on the way cannot be searched,
  which an lstat shows.
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
`PORT_FUZZ=FuzzParseDiff` fuzzes `--ast` the same way,
`PORT_FUZZ=FuzzResDiff` `--res`, and `PORT_FUZZ=FuzzLoadDiff` both
dumps of generated file trees.

`builtin/embedded.tck` is written by `tools/embed` from tackc's
`builtin/` directory, which, from tackc,

    TACK_PORT=<this repository> make port-embed

runs; tackc's porttest fails while the file differs from what that
writes, or from the files it holds.
