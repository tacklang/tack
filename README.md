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
them together, as the Go compiler's do, its libraries, which `--check`
loads, and its test mode, which `tack test` uses, among it; `testmain`,
which writes the main a package's tests are built with, byte for byte
the Go compiler's; and
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
swap and regions; and generics and functions, E4, are here too: each
instance of a generic function lowered under its binding and named by
its type arguments, interface methods called through their stand-ins,
switches on type parameters, closures with their environments and
captures, scope parameters and the adapters plain function values are
passed to them through, function values and the thunks of functions of
several results, fields loops, make of a Data type, calls through
function values, and the testing.Site arguments a call passes. The
whole of the lowering is ported. The C the Go compiler writes for a few
shapes of accepted programs does not compile; the port writes C that
does instead of copying it, and tackc's porttest lists each such case
as a known divergence (see Known divergences from the Go compiler
below, and package codegen). The
program is lowered into buffers, so a decline
writes nothing but its one line, neither C nor the warnings, which the
driver writes only once the lowering, and any `tack.h`, is done. `types` has the
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
implemented for a shape whose Go output the port does not copy.
`--emit-header` reports a `tack.h` it cannot open as the Go compiler
does, `cannot write` and the file, and declines any other failed
write; as in Go, the files it wrote before stay written. `--check` of
`std/builtin` or its `builtin.tck` compares the copy with the
compiler's own, as the Go compiler does; one that may be that copy
reached through a symbolic link, which the port cannot resolve, is
answered as not implemented. The flags
`-O0` to `-Oz` and `--backend` are read, and their usage errors
written, as the Go compiler writes them.

`tack test [-run text] [-v] [-O level] [directory or import path]`
runs a package's tests as the Go compiler does: the package loaded with
its test files and the generated main (see package testmain), checked
with every test's declaration, built as `--out` builds a program into a
temporary directory of its own, with the build cache, and run in the
package's directory with std/os's `run_inherit_in`, its outputs the
driver's own; the exit status is the binary's, 0 or 1, and a binary a
signal kills or that exits with another status is reported as Go
reports it, `FAIL <package>: the test binary ...`, exit status 1. Its
flags are read as Go's flag package reads tack test's, `-h` and
`--help` write its help, and every usage error, diagnostic and failure
is written as the Go compiler writes it; a failure the port cannot word
so is declined, naming `test`, and always before the line Go ends in,
which the decline stands for: the line `tack:` and an error a build or
a run ends in is held until the temporary directory is removed, and a
removal error with no Go words, EHWPOISON alone, is declined after a
run that passed or such an error, and written in std/os's words after
failed tests or diagnostics, where Go writes nothing of it. tackc's
porttest runs it, stage
test-examples, on every package of `examples/tack-test`,
`examples/test-files` and std/hex, with and without each flag, and on
probes of every message and path, comparing the generated C of each
test binary with Go's too.

`tack --out <path> <target>` builds a binary as the Go compiler does,
byte for byte the Go compiler's. The `--out` path is checked first, as
Go's `checkOutPath` does, the program compiled as `--emit` compiles it,
and the runtime and the C written to a temporary directory. Every C
file of every package, std's included, is compiled against the
program's `tack.h`, in package order and then name order; when the
program calls a builtin, declares an extern func, has a list, map,
arena or pool type, holds an `unreachable` or a C file of its needs a
`tack_` symbol, the build cache's directories are made, `clang
--version` run, and the runtime's C files compiled, and those the
program needs picked as a linker picks archive members. What each
object defines and leaves undefined is read by an ELF reader written
in Tack, with no tool but clang, so a C file that defines `main`, two
that define one symbol and an extern func no C file of its package
defines are reported as Go reports them; then clang's link turns the
generated C and the objects into the binary. Every command is the Go
compiler's at the `-O` level asked for. Each runtime object is taken
from the build cache when it holds one compiled from the same inputs,
and compiled and put there otherwise: its keys, its layout, its modes
and the bytes it writes are the Go compiler's, so the two share one
cache, a cache either filled giving the other hits, and an entry that
is damaged, a key that is not a hash or an object whose bytes do not
hash to it, is compiled again and replaced, as Go's are. Every file is
written whole to a temporary file beside it, created only where nothing
is, as Go's `os.CreateTemp` creates its own, and renamed into place, so
builds running at once, the port's or Go's, never share a temporary
file. A cached object the ELF reader does not take but Go's may read,
an ELF32, big-endian or Mach-O one, is declined.

clang is looked up on `PATH` as Go's `exec.LookPath` looks, skipping a
file it may not run and refusing one found relative to the current
directory, and the path found is the one run, given `clang` as its
`argv[0]`, as Go gives it. A clang that exits other than 0, is killed
by a signal or cannot be started is worded as Go words it, `clang
failed:` and the exit status, the signal's name or `fork/exec`, its
path and the errno, after `cannot compile` and the file for a C file's
compile. clang writes to the driver's own standard output and
standard error as it runs, as the Go compiler's does, so a terminal
shows its colour and both streams arrive as it writes them; `clang
--version`'s output is read, and its standard error, trimmed, ends
Go's error when it fails. The warnings are written just before the
first clang that writes to the driver's outputs starts, or with the
build's error when none does, and the failure after, as the Go
compiler writes them. Anything else is answered as not implemented,
the line saying so written last, and a file clang made at the `--out`
path removed: a decline decided before clang starts writes nothing but
that line, and one after writes it after the warnings and clang's
output, what the Go compiler writes before its own error. What was at
`--out` before the build is not restored: a decline after clang ran can
leave a file clang overwrote, and a file clang wrote through a dangling
symbolic link. The declines are these: a clang whose run fails in a way
Go's exec does not, `/dev/null` that cannot be opened, a pipe, read,
poll or wait that fails; an errno Go's words for are not known; an
object the ELF reader does not take, where Go's error is debug/elf's; a
build cache that cannot be written, an object or a key that is a
directory among it, where Go's error names a temporary file of a
random name; a
temporary directory or file that cannot be made or written, which Go
words with a path or an operation the port cannot give; and a current
directory that is gone, where Go's message names an empty path or Go
finds it by walking up. The temporary directory is removed with
std/os's `remove_all`, which removes and fails as Go's `RemoveAll`
does, relative to each directory, so one that cannot be removed is
reported as Go reports it, joined to the build's error; after the
diagnostics of the check of the definitions, where the Go compiler
drops it, the port reports it, `tack: failed to delete temp
directory:` and the error, a known divergence. An errno the port has no
Go words for there, EHWPOISON alone on Linux, is declined where Go's
output ends in a `tack:` line, after a build that succeeded or failed
with an error of one line, nothing of that line written first, and
written in std/os's words after diagnostics, where Go writes nothing
of the removal. The temporary directory is std/os's `tk-XXXXXX`, six
letters and digits, where Go's is `tk-<digits>`, made with std/os's
file modes, and every failure Go words with a path in it, as a runtime
directory that cannot be made, is declined. No build the porttest
stages compare shows these differences. Ctrl-C does not stop clang, and
leaves the temporary directory behind.

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
`--port-symbols` writes what an object file defines and leaves
undefined as the build's ELF reader reads it, which porttest compares
with the Go driver's `readSymbols`, damaged objects among them.

### Known divergences from the Go compiler

The port does not copy the Go compiler's bugs. Where an accepted
program reaches one, the port does what was ruled right, and tackc's
porttest lists the case in `porttest/testdata/divergences.txtar` with
the port's reviewed output, which it compares the port with in place of
Go's. The language owner decided these on 2026-11-15:

- `has(m, k)` written as a statement is warned of as not used, as
  `len(x)` is, and its value dropped; Go says nothing and lowers it to a
  call of a C function that does not exist. `make(T, alloc)` written as
  a statement, for a `T` that owns nothing, is warned of the same way,
  where Go says nothing.
- An extern func whose C name clashes with a type's name in `tack.h`, or
  is a name `tack.h` reserves, is reported at its `extern("...")` C
  name, not at the func's name.
- A struct whose `tack.h` name is a C keyword is reported as `type T of
  package P`, as the other `tack.h` messages name a type, and its name
  is claimed, so a later clash with it is reported.

The language owner decided this on 2026-11-16:

- A package that is not main, given to `--emit` or `--emit-header`
  without `--check`, is reported as Go reports it, `main package not
  found`, and the port stops there. The resolver binds nothing of such
  a program; Go's driver type-checks it on and reports errors the
  program does not have, its parameters undefined and `len` taking no
  arguments among them.

Decided on 2026-11-18:

- A build whose check of the definitions gives diagnostics, and whose
  temporary directory then cannot be removed for want of permission,
  EACCES or EPERM, writes the diagnostics
  and then `tack: failed to delete temp directory:` and the error Go's
  `RemoveAll` would give, `openfdat` or `unlinkat`, the entry's path
  and the errno, exit status 1; Go joins that error to the diagnostics
  and reports only the diagnostics. The error is the one std/os's
  `remove_all` gives, Go's `RemoveAll`'s.
- Where the port reports a removal error that Go drops, after a build's
  diagnostics and after `tack test`'s failed tests, an errno the port
  has no Go words for, EHWPOISON alone on Linux, is written in std/os's
  own words, `failed to delete temp directory: <op> <path>: <text>`
  after `tack:`; nothing declines there, since Go writes no line a
  decline could stand for. Decided 2026-11-18. The same holds for a
  failed write of `tack.h`, which may follow the warnings: EHWPOISON is
  written in std/os's words, `cannot write <path>: <op> <path>: <text>`.

- `tack.h` names a struct type of a package by its package's name and
  an underscore first unless the package is named main, so the package's
  own C, written against the names a program importing it gets,
  compiles against it. Go leaves the prefix off the types of the
  program's last package, which is main only in a program: in `tack
  test` of a package whose C uses a type of the package, std/os among
  them, its C does not compile, and the `tack.h` `--check
  --emit-header` writes for a library names its types otherwise than
  its C does.
  Under this naming, a package loaded for its tests or checked as a
  library is accepted and rejected exactly as a program importing it
  is, where Go differs three ways: a type `auto` of package `p` is
  `p_auto`, which Go rejects as `auto`, a C keyword; a type `q_X` of
  `p`, which imports `q` and its `X`, is `p_q_X`, which Go rejects as
  sharing `q_X`; and types `X` of `a/util` and of `b/util` are both
  `util_X`, which the port rejects and Go accepts, naming the tested
  one `X`.
- A `tack test` whose temporary directory cannot be removed after
  tests that failed, a test having left in it what may not be read,
  writes the tests' failure and then `tack: failed to delete temp
  directory:` and the error, exit status 1; Go drops that error then.
  Decided 2026-11-18 for `tack test`, extending the ruling for builds
  above.

These follow the lead's ruling of 2026-11-15 that the language already
defines the zero values, which the language owner may still overrule:

- The zero value of a type parameter standing for a string, an error or
  an Allocator is what the same function written for that type gets,
  where Go writes `0`: a defer's result variable of a string zeroed in
  full, an error or an Allocator zeroed, and a checked pop, remove or
  delete's string the pooled empty string.
- The zero value of a string where nothing else pools the empty string,
  a checked pop, remove or delete's among them, is the pooled empty
  string, where Go writes an empty label; and a `pool[string]`'s take
  helper leaves a zeroed string in its slot, where Go names the empty
  string before the pool declares it.

### Known differences

These are known, and not fixed yet:

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

The port is built with the reference Go compiler: tackc's compiler
with this repository's `runtime/` embedded in place of its frozen one.
With tackc checked out next to this repository, as `../tackc`, build
it there with

    make port-go-bin

which writes `bin/port-go/tack` in tackc from this repository, or the
one `TACK_PORT` names, and then, from this repository's root:

    TACK_HOME=. ../tackc/bin/port-go/tack --out bin/tack ./driver

`TACK_HOME` points the Go compiler at this repository, whose `src/std`
is the standard library. `TACK_CACHE` may also be set, to keep the Go
compiler's build cache somewhere other than the default cache
directory. The port built so links its own runtime, and is the same
binary it then builds of itself. A compiler from tackc's `make build`
embeds tackc's frozen runtime instead, so a port built with it links
that runtime, not `runtime/`'s; build `port-go-bin` again after a
change to `runtime/`.

## Std and the runtime

This repository owns the standard library, the C runtime, its header
and the builtins' documentation (per the owner's ruling, 2026-11-18):

- `src/std`, the module `std`;
- `runtime/builtin.tck`, the builtins' documentation;
- `runtime/tack/tack.h`, the runtime header;
- `runtime/*.c`, the runtime's C files.

`runtime/` is no Tack package: its `tack.mod` makes it a module root
only so that a library check of this repository, `tack --check .`,
skips `builtin.tck`, which is documentation and no code. A change to
std, a builtin or the runtime is made here. tackc's `src/std` and
`builtin/` are frozen copies, as they were when this repository took
them over. Its `porttest` runs both compilers on this repository's std,
and compares the port with a reference Go compiler that embeds this
repository's runtime. Its `TestPortFiles` fails while this repository's
std and runtime differ from the frozen copies, except for the paths it
lists, each with its reason.

The runtime ignores SIGPIPE and SIGXFSZ (per the owner's ruling,
2026-11-15, for SIGXFSZ). A write to a pipe with no reader fails with
EPIPE, and a write past the file-size limit, RLIMIT_FSIZE, fails with
EFBIG, instead of killing the process. std/os returns the error, `File
too large` for EFBIG. A broken pipe on standard output or standard
error ends the process with status 141, whatever wrote to it. Any other
failed write of `print`, `exit` or std/fmt, EFBIG and ENOSPC among them,
loses the bytes; std/os's `write_stdout` and `write_stderr` return it
as an error instead (per the owner's ruling, 2026-11-18), and the
driver writes its output and its warnings through them, so a failed
write is reported as the Go compiler reports it, `tack: failed to write
output: write /dev/stdout: file too large` past the limit, exit status
1. A standard stream set non-blocking is waited for with poll(2) while
it is full, as Go's runtime waits for one, so every byte is written.
std/os's `write_file_result` writes a file as `write_file` does and
says which call failed, open, write or close, so the driver words a
failed write of `tack.h` as Go does, `cannot write <path>: write
<path>: file too large` past the limit. std/os's `run` gives every child SIGPIPE's and SIGXFSZ's default
actions, so a child behaves as it does when a shell or Go starts it.
The frozen Go compiler's runtime does not ignore SIGXFSZ, so this is
one of the paths `TestPortFiles` lists.

`builtin/embedded.tck` holds the runtime and the builtins'
documentation as string literals. It is written by `tools/embed`, from
`runtime/`, the C files in the byte order of their names, by

    make embed TACK=../tackc/bin/port-go/tack

`TACK` is the compiler that builds `tools/embed`, `tack` on `PATH` by
default; it builds with this repository's std. std keeps copies of
`builtin.tck` and `tack.h`, `src/std/builtin/builtin.tck` and
`src/std/include/tack/tack.h`, which

    make std-copies

refreshes after a change to either. tackc's porttest fails while
`embedded.tck` differs from what `make embed` writes, or while std's
copies differ from `runtime/`'s.

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
- `TACK_GO`, a built reference Go compiler to use instead of building
  one: tackc's Go code with this repository's `runtime/` embedded, as
  tackc's `make port-go-bin` builds it, at `bin/port-go/tack`. The
  harness checks that it holds every file of `runtime/` it embeds, byte
  for byte, and fails otherwise; a compiler from tackc's plain `make
  build` is not one.

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

From tackc,

    TACK_PORT=<this repository> make port-embed

builds the Go compiler and runs this repository's `make embed` with it
(see Std and the runtime).
