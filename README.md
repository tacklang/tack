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

`--port-case` and `--port-test-case` are test-only (per the owner's
ruling, 2026-11-19): each writes one case of the test corpus exactly as
tackc's in-process harness renders it, so that `tools/corpus` checks the
corpus without Go (see Testing). `--port-case` builds its target as
`--emit` does, and `--port-test-case` a package as `tack test` does.
Each writes to standard output

    stage: <stage>
    ---
    <output>

and exits 0 whatever the stage, 1 when the driver fails and 3 when it
declines. The stage is `ok` for a program that compiled, `codegen` when
the backend reported anything, `load` for a loader that failed with an
error, or a name `tack test`'s generated main clashes with, and
otherwise the stages that reported errors, in order, joined by `+`: the
loader's first stage that reported anything, `lex`, `parse` or `load`,
then `resolve`, then `typeck`. The output is the generated C, as it is,
for `ok`; the backend's diagnostics, in its order, for `codegen`; the
loader's error and a newline, with no `tack: `, for its failure; and
every diagnostic otherwise, sorted, with no limit on how many. For `ok`
the warnings go to standard error. Every file is named relative to the
current directory, the standard library's and a single file's target
included. `--port-std <dir>` gives either flag the standard library
in place of `$TACK_HOME`'s, as a case of the corpus with a `.std`
directory of its own needs; it is a usage error with any other flag,
and with an empty name.

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

Decided on 2026-11-19, following the owner's ruling of that day that
std/unicode is at Unicode 18.0.0, which covers every place the compiler
quotes or classifies a rune, diagnostic wording included:

- A rune Unicode 18.0.0 assigns is quoted and classified as 18.0.0
  has it, where Go's unicode package is at 17.0.0 and has it
  unassigned: Go writes U+0558 as `\u0558` where the port writes `՘`
  itself. tackc's porttest records each such place with a probe:
  - `--lex`'s dump of a string, char or comment, `string("՘")`;
  - a printf verb, `unknown printf verb %՘`, where Go writes
    `"%\u0558"`;
  - a duplicate string case, `duplicate case "՘" in switch`;
  - an extern func's C name that is no C identifier, `cannot be named
    "՘" in C`;
  - a module name in `tack.mod`, `invalid module name "m՘"`;
  - a package name from a directory, `"՘" is not a valid package name`;
  - the driver's usage errors that quote an argument: a flag's value it
    cannot parse, `invalid value "՘" for flag -max-errors`, arguments
    past the one file, and an unknown backend, `unknown backend "՘"`;
  - `tack test`'s: a boolean flag's value, `invalid boolean value "՘"
    for -v`, and arguments past the one target.

  The lexer's invalid escape sequence quotes at most one byte past the
  backslash, never a whole rune, and a map fact's key is compared and
  never written, so neither differs; nor does white space, which is the
  same at both versions. tackc's porttest fuzz targets skip every input
  holding a rune the two versions classify apart.

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

### Bootstrapping

The port built by the Go compiler is stage 1. From this repository's
root,

    make bootstrap TACK=bin/tack

builds stage 2, the port built by stage 1, and stage 3, the port built
by stage 2, both into `bin/bootstrap/`. Each is built from `./driver`
at `-O3`, the level the compiler is built at and the Go compiler's
default, with `TACK_HOME` this repository and a fresh build cache of
its own under `bin/bootstrap/`, so every object is compiled anew. It
prints both SHA-256 sums and fails unless stage 2 and stage 3 are the
same bytes, `cmp` naming the first byte that differs. It needs clang,
`sha256sum` and `cmp`, and no Go. Stage 1 is also the same bytes as
stage 2, since the C the port writes is the Go compiler's; tackc's
porttest checks all three with its `bootstrap` stage, and tackc's

    make port-test-stage2

builds stage 2 the same way and runs the whole of porttest with it as
the port.

### Installing

From this repository's root,

    make install

builds the compiler in three stages, each from `./driver` at `-O3`:
`TACK`, an existing Tack compiler (`tack` on `PATH` by default, the
port built by the Go compiler, or a previous install), builds stage 1
into `bin/install-stage1/`, and stage 1 runs the bootstrap, as `make
bootstrap` does but into `bin/install/`, so it fails unless stage 2 and
stage 3 are the same bytes, even when `TACK` is an older compiler whose
output has changed since. It installs stage 3 to `~/.local/bin/tack`.
`BINDIR` names another directory to install it to, and `PREFIX` one
whose `bin/` it is; with neither set, `HOME` must be. That directory
needs to be on `PATH` for `tack` to be runnable by name. It installs
std too, as

    make install-std

does: it copies `src/std` to `$TACK_HOME/src/std`, or to
`~/.tack/src/std` when `TACK_HOME` is unset or empty, where the
compiler looks for it, with the builtins' documentation and the runtime
header written from `runtime/`, read-only, for reading: no build reads
them. The copy is made beside the std there and renamed into place, the
old one removed after, so a file deleted from `src/std` does not
linger, and a failed copy leaves the old one as it was. With neither
`TACK_HOME` nor `HOME` set it fails, saying so. It also refuses, before
it writes anything, a `TACK_HOME` (or a `HOME`) that is not absolute,
and a destination inside this repository or holding it, symbolic links
resolved, so `TACK_HOME=.`, as the build commands above set it, cannot
remove this repository's own `src/std`.

`make install` checks every destination before it builds anything: that
it is allowed, can be written, and that `$BINDIR/tack` is not a
directory. Once stage 3 is built, it copies std beside the old one and
the compiler into `BINDIR` under a name of its own, and only then
renames both into place, so a reinstall replaces both or neither, never
leaves a half-written `tack`, and leaves a process already running the
old one unaffected. Either target ended by SIGHUP, SIGINT or SIGTERM
undoes what it did; one ended by SIGKILL leaves its temporary files,
named for its PID, beside std and the compiler, and the next run
removes those of a run no longer running, putting back an old std left
aside with nothing in its place. `BINDIR`, `PREFIX`, `TACK_HOME` and
`HOME` are read from the environment, where make puts them when they are given on its
command line, never pasted into a command, so a path holding quotes or
a backtick is taken as it is. `tools/install.sh` does the work of both.
Neither needs Go: `make install` needs clang, coreutils and `cmp`, and
`make install-std` coreutils alone.

    make test-install TACK=bin/tack

checks both, with no Go: it installs from copies of this repository
into directories of its own below `$TMPDIR`, with a `HOME` of its own,
twice with the port, the second time with the compiler the first
installed as `TACK`, over itself, and more times with stand-ins for a
compiler, small scripts. It checks that the installed compiler builds
and runs a program that imports std/fmt and passes `tack test` of
std/hex with nothing but `HOME` pointing at its std; that a file deleted
from `src/std` does not linger; that every destination that would
delete part of this repository is refused, deleting nothing; that an
install whose stages differ, or into a `BINDIR` that cannot be written,
fails, keeping the previous install; and how both fail without `HOME`
or `TACK_HOME`. `tools/test-install.sh` lists what it checks. tackc's
porttest `install` stage runs it with the port.

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

std/unicode classifies runes as Go's unicode package defines its
classes, at Unicode 18.0.0 (owner ruling 2026-11-19), where the Go
compiler's unicode package, and tackc's frozen copy of the tables, are
at 17.0.0; the two differ only on the 13007 runes Unicode 18.0.0
assigns. Its tables, `src/std/unicode/tables.tck`, are written by
`tools/unicodegen`, a Tack program, from this repository's copy of the
Unicode Character Database, `tools/unicodegen/ucd/18.0.0`:
`UnicodeData.txt` and `PropList.txt` as unicode.org publishes them,
with `license.txt`, the Unicode License V3 they are under, and
`SHA256SUMS`, the SHA-256 of each of the three as unicode.org
publishes them. After a change to the generator or to that copy,

    make unicode-tables TACK=../tackc/bin/port-go/tack

rewrites the tables, and

    make check-unicode-tables TACK=bin/tack

fails while they differ from what the generator writes. Both first
check the copy: every file of it must have the SHA-256 `SHA256SUMS`
pins, and `PropList.txt` must name the version the directory is named
for. And

    make test-unicodegen TACK=bin/tack

runs the generator on the cases of `tools/unicodegen/testdata`, small
UCDs, most of them missing or malformed, each with the standard error
and exit status it must give, and the tables for those it accepts. None
of them needs Go. tackc's porttest `unicode-tables` stage makes the
same checks with the port. A new Unicode version replaces the copy's
directory, which must be the only one in `tools/unicodegen/ucd`, its
`SHA256SUMS` and `UCD` in the Makefile. std/unicode's tests,
`src/std/unicode/unicode_test.tck`, check a rune of each class the
upgrade to 18.0.0 changed and the bounds of the tables' runs.

## Testing

`corpus/` is the test corpus: `examples/`, the example programs with
their six txtar archives of invalid, warned, test-build and C cases and
their committed `tack.h` copies, and `golden/`, the goldens, each the
stage line and the generated C of a program of `examples/`. It started
as a copy of tackc's `examples/` and `test/testdata`, and tackc's
porttest `TestPortFiles` keeps it equal to tackc's but for the cases
and files it lists with their reasons: the four cases of
`invalid.txtar` where the port diverges from Go on purpose (see Known
divergences), the goldens and `tack.h` copies of the programs that
import std/os, whose externs this repository's std adds to, and the
goldens of the programs that import std/unicode, through std/conv,
std/fmt or themselves, whose tables this repository's std has at
Unicode 18.0.0. Its
`tack.mod` makes it a module root only so that `tack --check .` skips
it.

    make test-corpus TACK=bin/tack

builds `tools/corpus` with the compiler `TACK` names and runs it over
the corpus with that compiler and this repository's std. It needs clang
and coreutils, and no Go. It checks every case of every archive, every
golden and every committed `tack.h`, as tackc's `go test ./test` checks
its own (see `tools/corpus`'s package comment), each case one run of
the compiler in a temporary directory, through coreutils' `timeout`,
two minutes at most, and `env`, which sets `TACK_HOME`, `TACK_CACHE`
and `TMPDIR`, since std/os's `run` can neither bound a child's time nor
set its environment. A failure says which case failed and the first line
that differs; a decline fails too. It also checks that each case of
`invalid.txtar` and `invalid-packages.txtar`, and each program, gives
the same output with CRLF line endings, as tackc's `TestCRLFMatchesLF`
does. The run ends with a count, and exits 1 when anything failed. The
temporary directory, in `$TMPDIR`, must not be below a directory that
holds a `tack.mod`, or the cases with no module of their own would load
as part of that one: the runner refuses such a `$TMPDIR` before it runs
anything.

    make update-corpus TACK=bin/tack

rewrites, in place, each expected section, golden and `tack.h` copy
the compiler's output differs from, as `go test ./test -update` does in
tackc; a run that changes nothing writes nothing. Read the diff it
leaves, line by line, before keeping it. `tack.h` is written from a
scratch copy where `--emit-header` ran, never by running it in
`corpus/`. tackc's porttest runs the same check, its `corpus-runner`
stage, with the port.

    make fuzz-build TACK=bin/tack
    make fuzz FUZZ=parse FUZZTIME=600 TACK=bin/tack
    make fuzz-regress TACK=bin/tack

fuzz the compiler's front end in process, with clang's libFuzzer and the
address, leak and undefined behavior sanitizers, and no Go (decided
2026-11-19: a third-party fuzzer until Tack has its own). Each target of `tools/fuzz` is a package main whose main
libFuzzer runs once for each input: `lex` lexes the input as a file,
and `parse` lexes and parses it, whatever the lexer reported, three
times, the third with an error limit. Each holds what it gets to
invariants of its own besides the sanitizers' (see package
`tools/fuzz/check`): the tokens in order within the input, every byte
between them white space or inside a diagnostic's span, the line
starts, every diagnostic's span within the file, a parse with no tree
having reported an error, a tree's `--ast` dump one JSON value, a
second parse giving the same diagnostics and dump, and a limited parse
stopping at its limit. It renders every diagnostic too. A broken
invariant aborts, so libFuzzer saves the input as a crash. Every input
must free what it allocated, checked two ways: `tools/fuzz/entry.c`
aborts after any input that leaves a buffer of `c_allocator` held, by
the runtime's count of them, which catches a leak of Tack's memory,
since the runtime keeps every buffer it holds on a list LeakSanitizer
sees as reachable; and LeakSanitizer finds memory C allocates with
`malloc` and loses, libFuzzer checking after each input whose mallocs
and frees do not balance, and at exit. `fuzz.sh` sets the sanitizers'
options itself, so an inherited `LSAN_OPTIONS` cannot turn that off.

`fuzz-build` builds every target into `bin/fuzz/<target>/fuzz`: the C
`--emit` writes for it, with the program's `tack.h` from
`--port-header`, the C files `--port-loaded` lists and the runtime's,
compiled with the driver's flags at `-O1` and the sanitizers.
`tools/fuzz/entry.c` includes the emitted C, its `main` renamed, since
every Tack function there is `static`, and calls that `main` for each
input; the target reads the input through package `tools/fuzz/input`,
whose C half holds it, so no Tack name or type is spelt in C. `fuzz`
fuzzes `FUZZ`, `lex` by default, for `FUZZTIME` seconds, 60 by
default, with at most 2048 MB of memory and 60 seconds an input, from
the inputs it kept before in `bin/fuzz/corpus/<target>`, the target's
committed inputs in `tools/fuzz/regress/<target>`, and every `.tck`
file of `corpus/examples` and every `.tck` section of its txtar
archives as seeds, cut to 64 KiB, which cuts two of them, the `conv`
and `os` programs; each finding is saved into
`bin/fuzz/crashes/<target>/`. `fuzz-regress` replays the committed
inputs and the seeds, whole, through every target, once each, and fails
on any finding, or when a target has no committed input; tackc's porttest
`fuzz-regress` stage runs it with the port. A finding, once fixed, is
committed to `tools/fuzz/regress/<target>`. Every run's stack is 40 MB:
the parser recurses on the C stack (see Known differences), and the
stack gives the sanitized target about the headroom the default 8 MB
gives the compiler built at `-O3`. On a thousand nested ifs, switches
or struct literals, measured, the compiler needs more than 2 MB and at
most 3 MB, and the sanitized parse target more than 12 MB and at most
16 MB, so 8 MB is 2.7 to 4 times the one's need and 40 MB 2.5 to 3.3
times the other's. It needs clang with libFuzzer, and no Go.

    make test-std TACK=bin/tack

runs `tack test` on every package of std that holds a test file,
`std/hex` and `std/unicode` today, and fails when any of them fails. It
needs no Go. tackc's porttest `test-examples` stage runs the same
packages' tests with both compilers, and its `unicode-tables` stage
requires std/unicode's to pass.

`corpus/runs/` holds run records: programs built with `--out` and run,
at each optimization level a record names, or runs of the compiler
itself, each record saying what the run must do: its exit status,
signal or that it is still running after a while, its whole standard
output and error, descriptors 3 and 4, and the files it leaves. They are
the runtime assertions of tackc's Go tests, moved into data, and
`invalid.txtar` there holds records every reader must reject, each with
the class of its problem. `tools/corpus/runs.tck` describes the format.
Each run is on copies of its own, so no record sees what another wrote,
its TACK_CACHE a copy of the warm cache of its clang flags, and every
program's `--emit` output is held to clang's strict warnings,
as `warnings.txtar`'s are. A program runs through `env -i`, `sh`,
coreutils' `timeout`, the runner itself as a launcher (`corpus
--launch`, in `tools/corpus/launch.c`) and `env -i` again, so its
environment is PATH, TMPDIR and the record's own alone; `sh` sets its
standard input and descriptors (closed, a file, or a broken pipe made
from a FIFO with `mkfifo`), and `timeout`'s own lines go to a file the
program never has. The launcher is a child subreaper: it starts the
program in a session of its own, with its limit of open files, judges
its end and the deadline with `waitpid`, and fails the record when
anything the program started is still alive as the program ends,
killing what is left at the deadline by the pids
`/proc/self/task/<pid>/children` lists, the one file under `/proc` the
runner reads. Each run has a private root holding its `$SRC`, `$TMP`,
`$RUN` and `$PROG`, which must hold exactly those afterwards, and every
tree it checks is walked without following symbolic links, which a
`symlink` line pins. The runner closes every
descriptor it inherited, with a small C file of its own, so a program
sees 0, 1, 2 and what its record opens alone, and it refuses to run the
records with a `timeout` that is not coreutils', with `-v` and `-k`. The runner checks the
records after the rest, or alone with `--runs`, which needs no flag of
the port's own, so any compiler can be checked against them; with
`--update` it rewrites each record's expectations from the runs, which
`tackc`'s `porttest/rungen` leaves for it to fill: read that diff line by
line. tackc's porttest `TestRunRecords` checks each record against the
reference Go compiler, with no shell, and `TestRunRecordsRejects`
checks its reader rejects `invalid.txtar`'s records as this one does.
`must-fail.txtar` holds records that must fail, each with a `must-fail`
line naming the class of the first thing that must fail, from one list
both runners share (`failure_classes` in `tools/corpus/runs.tck`): a
record of it that passes, or fails another way, fails the check, on
both sides.

    bin/corpus --parse-runs corpus/runs/prototype.txtar

only reads a run archive and writes what it read, for tackc's
`FuzzRunRecordReaders` to compare with its own reader's: `archive ok`
or `archive error`; then, for each case, `case` and its name, a line
`file <path> <length>` for each of its files, and either `error
<class>` or `record` and its fields, one line each in a fixed order:
`replaces <count>`, `flags selfcheck=... partial=... cold=... strict=...
cwd-empty=... stdin=... stdout=... stderr=...`, `dir`, `target`,
`stdout-file`, `stderr-file`, `must-fail`, a line per `opt`, `cflags`, `invoke`,
`arg` and `env`, `stdin`, `nofile <n>`, `stream` and the four ways,
`end exit=<n> signal=<n> running=<text> ends=<n>`, `stdout`, `stderr`,
`fd3`, `fd4`, a `want <path> <bytes>` line per file line, a `lacks <stream>
<bytes>` line per lacks line, and a `link <path> <target>` line per
symlink line. Every
string is quoted as Go's `strconv.Quote` quotes it, and every flag is
`true` or `false`; `tools/corpus/dump.tck` defines it.

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
against itself, which passes every stage but `corpus-runner`,
`bootstrap` and `fuzz-regress`, which it skips, since Go has no
`--port-case`, `--port-header` or `--port-loaded` and the port it builds
is not Go, and checks the harness rather than the port. The
package documentation of `porttest` describes the protocol, the stages
and the corpus in full.

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
