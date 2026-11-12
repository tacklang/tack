# Tack

Tack is a small compiled programming language. This repository holds `tack`, its
compiler, written from scratch in Go.

It is a learning project, built to understand how a compiler works end to end
rather than to be used for anything. There is no runtime, a standard library
of eight packages, `std/fmt`, `std/bytes`, `std/text`, `std/os`,
`std/path`, `std/conv`, `std/errors` and `std/sort`, and no intermediate
representation.

Every program the compiler accepts is held to four guarantees: no
out-of-bounds access, no memory leak, no use-after-free and no panic. They
are the project's hard rules; [docs/guarantees.md](docs/guarantees.md)
defines them and lists the violations still open.

Code generation is pluggable. The front end hands a resolved syntax tree to a
*backend*, and the backend decides what comes out and how it is built. One ships
today: `c`, which emits a self-contained C translation unit that `clang` turns
into an executable.

## Status

Early, and honest about it. The compiler runs a complete pipeline and produces
working native binaries, but the language it accepts is still small.

```tack
package main

func fib(n int32) int32 {
    if n < 2 {
        return n
    }
    return fib(n - 1) + fib(n - 2)
}

func main() {
    exit(fib(10))
}
```

That compiles, links, runs, and exits 55. Anything beyond what "The language
today" lists below does not.

The language computes. Functions take parameters and return values, calls are
expressions, arithmetic and comparison work, and `if` branches on the result.
`fib` is in `examples/fib`, and it runs.

Functions have local variables, immutable unless declared with `mut`, and
loops, and lists, owned and growable, which free themselves and may hold
lists of their own, and tagged unions, whose cases a switch matches. `print` takes strings only; `std/fmt`'s `printf` writes integers,
bools and strings, through `any` parameters, and its `write` writes bytes,
a view of `uint8` (see the language today). `std/bytes`, the first std
package written in Tack alone, searches byte views and formats integers
and repeats bytes into lists, and `std/text`, also in Tack alone, holds
growing bytes in a `Text`, compares and hashes them, and lends them out
as a view. `std/os` reads the program's arguments and environment,
reads, writes, lists, renames, stats and removes files and
directories, and runs other programs, through C that makes one system
call at a time, and
`std/path`, in Tack alone, cleans, splits and joins slash-separated
paths as Go's `path` package does, `std/conv`, in Tack alone,
parses integers as Go's `strconv.ParseInt` and `ParseUint` do, and
`std/errors`, in Tack alone, pairs an error with a message made at
run time in a `Failure`, and `std/sort`, in Tack alone, sorts a list
in place by a less function.

`exit` is a global builtin as a stand-in for a future `os.Exit`, after which
the global goes away. `std/os` does not have one yet, so `exit` stays.

## Requirements

- Go 1.27.1 or newer
- `clang` 21 or newer on `PATH`, used to compile and link; 21 is the
  version the project is tested with. On macOS this means LLVM clang 21
  or newer: Apple clang numbers its releases its own way, and its
  version number is not LLVM's

## Building

```sh
make build       # builds ./bin/tack
make install     # installs tack into $GOBIN, or $GOPATH/bin if unset, and std
make install-std # installs only the standard library, src/std
make test        # runs go test -race ./...
```

The compiler needs its standard library, `src/std`, installed beside it:
it reads the builtins from there and links their C bodies from there. It
looks in `$TACK_HOME/src/std`, or `~/.tack/src/std` when `TACK_HOME` is
unset. `make install` and `make install-std` copy `src/std` there,
replacing what is there, so a file deleted from the repository does not
linger. Without it, every command that compiles a program fails:

```
tack: cannot find std at /Users/you/.tack/src/std; set TACK_HOME or run make install
```

`--help`, `--backends`, `--lex` and `--ast` need no builtins and work
without it. The repository is laid out as an installation is, so
`TACK_HOME=$PWD` runs a freshly built compiler against the `src/std` beside
it; the tests do the same, and never read `~/.tack`.

The objects compiled from std's C, the bodies of the builtins, are kept
in a build cache, so only the first build after a change compiles them.
The cache is `$TACK_CACHE`, or `tack` in the user's cache directory when
`TACK_CACHE` is unset or empty: `~/Library/Caches/tack` on macOS, and
`$XDG_CACHE_HOME/tack` or `~/.cache/tack` on Linux. A relative
`TACK_CACHE` is taken from the current directory. An entry is keyed on
everything that decides the object: the C file's bytes, every file in
std's include directory and every `.h` file beside the C file, the whole
compile command (the optimization level and the absolute paths of the
file and of std's include directory among it), and the output of
`clang --version`; a change to any of them is a new entry, never a stale
one. An entry that does not hash to what its key says is compiled again.
Nothing removes old entries; deleting the directory clears the cache, and
is safe whenever no build is running:

```sh
rm -rf "${TACK_CACHE:-$HOME/Library/Caches/tack}"   # macOS
rm -rf "${TACK_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/tack}"   # Linux
```

A build that needs the cache and cannot find or create it fails, naming
it, rather than building without it:

```
tack: cannot find the build cache: TACK_CACHE is not set and the user cache directory is unknown: ...
```

Only std's own C is cached. A program's C files, and those of std
packages it imports, which are compiled against the program's own
`tack.h`, are compiled on every build.

## Usage

```sh
tack --out hello examples/hello/main.tck
./hello
tack --out shapes examples/multi-file    # every .tck file in the directory
cd examples/module-mains && tack --out hello example.com/module-mains/tools/hello-world
tack --out ext examples/extern/main.tck  # links helpers.c, which defines its extern funcs
```

The target is a file, compiled alone; a directory, compiled as the one
package its `.tck` files make, in the module whose `tack.mod` is in that
directory or the nearest one above it; or an import path, which names a
package of the module found from the current directory. A target that
names nothing on disk is taken for an import path unless it is absolute,
starts with `.`, or ends in `.tck`. Flags go before the target. The intermediate stages can be dumped instead of
linking. A dump prints only that stage's output to stdout, so it can be
redirected: `tack --emit x.tck > x.c` gives a C file that compiles. A dump
does not take `--out` or an `-O` flag, and only one dump can be given at
a time. A build compiles the generated C and the program's C files at one
clang optimization level, `-O3` unless `-O0`, `-O1`, `-O2`, `-Os` or `-Oz`
asks for another; `-Ofast` is not offered, since it gives up C semantics
the generated code relies on.
`--emit-header` is not a dump but stops where one does: it writes the
program's `tack.h` (see "Extern C" below) into the target's directory and
into every other package directory of its module with C files or extern
funcs, never a standard library package's, replacing
any `tack.h` there, for an editor such as clangd to find; it prints
nothing, takes no `--out`, and cannot be combined with a dump. Builds never
read that copy, so it can be left out of version control. A `tack.h` whose
extern funcs use strings includes `<tack/tack.h>`, so the editor needs
`-I $TACK_HOME/src/std/include` in its compile flags, where `make
install-std` writes a copy of the runtime header (see "Builtins").

| Flag               | Effect                                          |
|--------------------|-------------------------------------------------|
| `--lex`            | print the token stream and stop                 |
| `--ast`            | print the parsed syntax tree as JSON and stop   |
| `--res`            | print the resolved syntax tree as JSON and stop |
| `--emit`           | print the generated code and stop               |
| `--emit-header`    | write `tack.h` beside the C files and stop      |
| `--check`          | check without building; combines with a dump    |
| `--backend <name>` | select the code generation backend, default `c` |
| `--backends`       | list the available backends and stop            |
| `--out <path>`     | compile and link to `<path>`                    |
| `--max-errors <n>` | report at most `n` errors, default 10; 0 reports all |
| `--max-diags <n>`  | the old name of `--max-errors`                  |
| `-O0` ... `-O3`, `-Os`, `-Oz` | optimize the build at that clang level, default `-O3` |
| `-h`, `--help`     | print usage and exit                            |

`--check` runs every stage, code generation included, and builds
nothing: no clang, no `--out`, no `-O` flag. It exits 0 when it finds no
error and 1 when it finds any; warnings are printed and fail nothing. A target holding a main package is
checked as the program a build would compile, but a missing `func main`
is reported without stopping the stages after the resolver. A directory or
import path holding another package, or no `.tck` file, is checked as a
library: every package at or below it in its module, with what they
import, leaving out main packages (check one by naming its directory),
directories starting with `.`, and nested modules. A single file whose
package is not main is checked as a library too, and a library with a
syntax error is still one: the error is reported and its package checked
past it. `tack --check src/std` checks the standard library, from a
checkout as well as the installed copy, leaving out `std/builtin`, which
only holds a copy of the builtins' documentation. Named on its own,
`tack --check src/std/builtin/builtin.tck`, or by its directory, either
directly or through a symbolic link, that copy
is clean when it matches the builtins built into the compiler, and
otherwise an error saying to refresh it with `make std-copies`. With a dump flag,
`--check` prints that stage's output, when the stages up to it found no
error, and the diagnostics of any later stage: `--check --ast` prints
the trees on stdout and a type error on stderr, and exits 1; on syntax
errors it prints the tree the parser recovered, each damaged part a node
holding its span and, for a declaration, the name it was declaring. `--emit`
and `--emit-header` produce nothing for a program with any error;
a library's `--emit` is C with no `main`.

```sh
tack --check .                 # every library package at or below .
tack --check --res cmd/tool    # the resolved tree and every diagnostic
```

Diagnostics and errors go to stderr. The exit status is 0 on success, 1 when
the program has errors or the build fails, and 2 when the command line is
wrong. A warning, `warning:` where an error says `error:`, is something
suspicious that stops nothing: a variable declared and never read, an
import never used, a value computed and discarded. A program with only
warnings builds, dumps and runs as one with none does, its warnings
printed on stderr and its exit status 0.

```sh
tack --emit examples/hello/main.tck > hello.c   # the C the backend emits
```

## How it works

Each stage is a package that takes a data structure and returns one, along with
any diagnostics. Every stage runs, whatever an earlier one reported, so one
run reports the syntax errors, the unresolved names, the type errors and
the warnings of a program together: an error of one stage hides no other
stage's. Code is generated only from a program no stage reported an error
about, warnings aside; the backend checks that itself and refuses any
other. The one stop is a file that does not lex, or a package
whose files cannot be put together at all (a malformed `tack.mod`, a main
package whose package clauses are wrong), after which the resolver and the
type checker do not run.

Each error is reported once, at its cause: an error that follows from one
already reported is not reported. The parser reports every syntax error in a
file: after one, it skips to the end of the statement, the next line that
starts a declaration, or the brace that closes the block, puts a bad node in
the tree where the skipped text was, and goes on. A name the resolver could
not bind because of an error already known, its declaration reported, its
import not loaded, or a bad node that may have declared it, is invalid, and
nothing that uses it is reported: no `undefined`, no mismatch against its
type. A bad declaration hides only the name it was declaring, when the
parser got that far, and any name of its package when it did not; a bad
statement hides the names used after it in its function, and, since it may
have been a return, a guard or an assignment, everything the type checker
would report after it there. A variable, global or import damaged source
may have used is not reported unused. The driver reports the diagnostics
of a run sorted by file and by where each starts, at most 10 errors unless
`--max-errors` says otherwise, and then the line `too many errors: showing
the first 10; --max-errors 0 shows them all` when it left some out. Only
errors count toward the limit: the warnings among the errors shown are
shown, so a warning never takes an error's place, and the report stops
where the first error past the limit would be, leaving out what comes
after it, warnings too.

| Package    | Role                                                          |
|------------|---------------------------------------------------------------|
| `source`   | the source files, their bytes, and their line indexes         |
| `lexer`    | bytes into tokens, including Go-style semicolon insertion     |
| `parser`   | tokens into an AST                                            |
| `loader`   | finds a target's module and files, lexes and parses them      |
| `modfile`  | parses `tack.mod`, the root file naming a module              |
| `ast`      | node types, packages, programs, source spans, and `Walk`      |
| `resolver` | binds every name; checks there is a `main` package and `main` |
| `types`    | Tack's type representation                                    |
| `builtin`  | the builtins' names, signatures and C names                   |
| `typeck`   | checks types, proofs, `mut` arguments and field visibility    |
| `constant` | exact values of integer constant expressions                  |
| `codegen`  | the backend interface and its registry                        |
| `codegen/c`| AST into C, and `tack.h` for the C files of extern funcs      |
| `cname`    | which names C can declare: identifiers that are not keywords  |
| `diag`     | diagnostics and their rendering                               |
| `tack`     | the command line driver                                       |

Diagnostics carry a source span and render with the offending line and a caret:

```
garbage/main.tck:4:5: error: invalid character @
03 |	func main() {
04 |	    @#$%^ print("hi")
   |	    ^
05 |	}
```

Semicolons are inserted by the lexer following Go's rule: at a newline, if the
previous token can end a statement, a semicolon is inserted. This is why
function bodies and declarations do not need explicit terminators.

## Values

A `string` is one value that occupies two words, a pointer and a length. It is
one type rather than a pointer and a length passed separately, so that it can
be returned, stored, and nested later without changing every signature that
mentions it. The C backend emits it as a `tack_string` struct passed by value.

The integer types are emitted as C's fixed-width types, `int8_t` through
`uint64_t`, except the two whose width is the target's: `unsafe_size_t` is
emitted as C's `size_t`, from `<stddef.h>`, which the generated source
includes only when a program uses it, and `unsafe_uintptr_t` as
`uintptr_t`. A `bool` is emitted as C's `bool`. `float32` and `float64`
are emitted as C's `float` and `double`, IEEE 754's binary32 and binary64,
and a program that uses either turns off `FP_CONTRACT`, so C never fuses
`a * b + c` into one operation that rounds once where Tack rounds twice:
every optimization level computes the same result.

## Builtins

The builtins are built into the compiler: no file a user or the standard
library holds can add one, change one, or override how one works. Every
builtin type, value and function is documented in
[`builtin/builtin.tck`](builtin/builtin.tck), in the compiler's own tree,
which is embedded in the compiler as documentation only: it is never
compiled, and a test keeps it one to one with what the compiler
implements. `make install-std` writes a read-only copy to
`$TACK_HOME/src/std/builtin/builtin.tck`, and the repository keeps one at
[`src/std/builtin/builtin.tck`](src/std/builtin/builtin.tck), so it is
found where the standard library is browsed; `make std-copies` refreshes
the repository's copy, and a test fails while it differs. The compiler
reads neither copy, and `std/builtin` is no package: `std/builtin
documents the builtins and cannot be imported`, and `tack --check` leaves
it out of a library.

The file declares each builtin type as itself, `type int32 int32`, `error`
as its struct of `code` and `msg`, the values `true`, `false`, `noerror`
and `c_allocator` as `var`s, and each function without a body, the generic
ones in real syntax, `func pop[T Elem](mut l list[T]) T`, with stand-in
types, `Elem`, `Key`, `Sized` and `ListType`, for what no Tack type can
say. Those forms parse only in that file, which the compiler parses in a
builtin mode no program can turn on: anywhere else `var true bool` and
`type int32 int32` stay syntax errors, and a function without a body
stays `function f has no body; only an extern func is declared without
one`.

`print` and `exit` have a C body: a call to `print` is a call to
`tack_builtin_print`, and to `exit` one to `tack_builtin_exit`; the
generated C declares each builtin the program calls with a prototype,
`void tack_builtin_print(tack_string);`, and `exit`'s as never returning,
`_Noreturn void tack_builtin_exit(int32_t);`, as its body is, so a C
function with a result may end in a call to it. Their bodies, `print.c`
and `exit.c`, and `list.c`, which holds the functions that grow and free a
list's buffer, which the generated C calls, and `c_allocator`'s, are the
runtime's C files, in [`builtin/runtime`](builtin/runtime), embedded in
the compiler too, beside the runtime header, `<tack/tack.h>`. That header
is the one definition of what every program's C shares, `tack_string` and
the string helpers, `tack_error` and the error helpers, `tack_any`, its
type ids and its unpack helpers, and `tack_allocator`, the list functions
and the list macros: the generated C includes it, and so does a program's
`tack.h` when an extern func uses a string, an error, `any`, a list or an
`Allocator`. Each build writes the runtime to its own temporary directory
and compiles every C file, the program's, std packages' and the runtime's,
against that copy of the header, so editing an installed copy changes
nothing. `make install-std` writes a read-only copy to
`$TACK_HOME/src/std/include/tack/tack.h`, for extern C and editors, and
`--emit` output compiles on its own with `-I $TACK_HOME/src/std/include`.

The driver compiles the runtime's C files, or takes their objects from the
build cache, and links only those that define a builtin the program calls,
the list functions when it has a list type, the abort of an `unreachable`
when it holds one, `unreachable.c`, a function one of its C files
calls, and those that define what those objects need in turn, as a linker
takes an archive's members; `exit` needs `list.c` too, since it has every
allocator free what it still holds. So the generated C includes only
`<stdbool.h>` and `<stdint.h>`, `<stddef.h>` if it uses `unsafe_size_t`,
which a freestanding C implementation has, and `<tack/tack.h>`, which
includes only those three, and a program that calls neither builtin, and
has no list, links neither body and none of the C library they call.

`len`, `make`, `append`, `reserve`, `pop`, `remove`, `swap`, `replace`,
`put`, `delete`, `has`, `drop`, `add`, `add_all` and `insert` are generic: the type
checker checks each itself and the C backend lowers each in place. Builtins that are wanted
but not implemented go in [`builtin/PLANNED.md`](builtin/PLANNED.md),
beside `builtin.tck`.

## Code generation backends

A backend is anything that satisfies this interface:

```go
type Backend interface {
    Name() string                                    // "c"
    Ext() string                                     // ".c"
    Generate(prog *ast.Program, builtins *builtin.Set) ([]byte, []diag.Diagnostic)
    Header(prog *ast.Program) []byte                 // tack.h
    CompileCmd(src, header, include, obj string, opt Opt) []string // {"clang", "-w", "-fno-finite-loops", "-O3", "-c", ..., src}
    CompileStdCmd(src, include, obj string, opt Opt) []string      // likewise, for the runtime's C
    BuildCmd(src, include string, objs []string, out string, opt Opt) []string // {"clang", "-w", "-fno-finite-loops", "-O3", "-I", include, "-o", out, src, objs...}
    VersionCmd() []string                            // {"clang", "--version"}
}
```

`Generate` receives the same resolved program every backend sees and returns target
source, or diagnostics if it meets something it cannot lower. `Ext`,
`CompileCmd`, `CompileStdCmd` and `BuildCmd` are what keep the driver from
knowing anything about the target: it writes the bytes to a file with that
extension in a temporary directory, writes `Header`'s output beside it as
`tack.h`, compiles each C file of the program with `CompileCmd` and the
runtime's with `CompileStdCmd`, and links them with `BuildCmd`, each given
the include directory the build wrote the runtime header to, `include`,
where `<tack/tack.h>` is. `VersionCmd`'s output identifies the toolchain,
so an object compiled from the runtime's C is reused from the build cache
only by the same one.

Backends register themselves from `init`, so adding one means writing a package
under `codegen/` and importing it for its side effect in `tack/main.go` and
`test/golden_test.go`. Nothing else in the compiler changes, and the new backend
picks up golden coverage automatically.

The C backend translates directly, with no intermediate representation. Each
Tack function becomes a `static` C function named after its package's import
path and its name, each part written as its length and its bytes, with every
byte but an ASCII letter or digit escaped as `_` and two hex digits, so `First`
in `calc/lexer` is `tack_4_calc5_lexer5_First` and `main` in a single-file
program, whose path is `main`, is `tack_4_main4_main`;
an extern func is declared under its mangled name and bound to its C name
with an assembler label, so the C name never meets a name the generated C
declares;
string literals are pooled into `unsigned char` byte arrays, `tack_str0` and
on, so that arbitrary
bytes survive without an escaping scheme, a call to `print` is a call to the
static `tack_builtin_print`, whose body is one `fwrite` to `stdout`, and a
generated `int main(int argc, char **argv)` stores `argc` and `argv` in
two globals, `tack_argc_` and `tack_argv_`, for `std/os`, and calls the
Tack `main`. Each
part is read back by its length, so no two Tack names, whatever underscores
they hold, map to one C name, and none takes the shape of a name the backend
makes for itself, which has a letter after `tack_`.

## The language today

A topic-organized specification is being assembled in
[docs/spec/](docs/spec/README.md); until its chapters are filled in, this
section remains the description of the language.

- Every file starts with a `package` clause, before any declaration. A
  file compiled on its own must be `package main`; a package of a module
  is named as described below.
- A package may be split across files. Compiling a directory compiles every
  file directly in it whose name ends in `.tck`, in the byte order of their
  names, as one package; a name starting with `.` is skipped, as an
  editor's or a tool's, and so are subdirectories, even one named like a
  Tack file. A directory with no such file is an error, `no .tck files in
  dir`. Every file's package clause must name the same package, or the
  directory is reported as holding several, `directory dir holds two
  packages, a and b`, with a note at the first file of each other one.
  Every package-level name, function, type or global, of every file is
  visible in every file, in any order, exactly as within one file: a name
  declared in two files is `x is already declared`, with a note at the
  other, a parameter or local may not reuse a global of another file, and a
  global read only in another file is used. A global's type may name a
  global declared after it, in its file or a later one, as in `mut cells
  [size]int64`; one that needs itself is `initialization cycle: a refers to
  itself`. Every file is lexed, and then every file parsed, so each
  file's problems are reported. A file compiled on its own is the whole
  package.
- A directory is compiled in its module. A module is a directory tree
  whose root holds a `tack.mod`, which names it in one line, `module
  calc`, and may hold `//` comments, which run to the end of the line
  wherever they start, and blank lines. A module name is one or more parts
  joined by `/`, each lowercase ASCII letters, digits, `-`, `.` and `_`,
  not starting or ending with `.` or `-`, as in `example.com/my-tools`.
  `std` is a name like any other: the standard library's own `tack.mod`
  says `module std`, so a checkout of it is checked as that module, and in
  a module named `std` an import of `std/...` names the module's own
  package, never the installed standard library's. Anything else in `tack.mod` is an error at the line it is on: a second
  `module` line, a `module` without a name or with two, another directive,
  or a bad name. A directory with no `tack.mod` in it or above it is an
  error, `no tack.mod found in dir or any parent directory`; a file
  compiled on its own needs none.
- A package's import path is the module's name followed by its
  directory's path below the root, so `calc/lexer` is the directory
  `lexer` of module `calc`, and a package at the root has the module's
  name as its path. A package is named by the last part of its import path,
  and every file's package clause must say that name, `package lex does not
  match its directory; expected package lexer`, or `main`, which may be in
  any directory. A package name is lowercase ASCII letters and digits,
  starting with a letter, and is not a keyword or a builtin type's or
  function's name; `builtin` and `std` are names like any other: a directory holding a package of
  another name is an error, such as `my-tools is not a valid package name:
  use lowercase letters and digits`, though it may hold a `main` package,
  and directories below it may hold packages. A program is built from a
  `main` package; building any other is `calc/lexer is not a main
  package`. One module may hold several main packages, each built from its
  own directory or import path.
- Imports. `import lexer from "calc/lexer"` makes the package whose import
  path is `calc/lexer` usable in that file, and in no other, as `lexer`:
  `lexer.First(src)` calls its function `First`, `lexer.KindNumber` reads
  its global, and `lexer.Token` is its type, written anywhere a type may
  be, in a signature, a variable, an array's element or a field, and in a
  literal, `lexer.Token{kind: 1}`, which needs parentheses in an `if`,
  `for` or `switch` header like any struct literal. Imports come right
  after the package clause, one per line; `import (...)` and an import
  after another declaration are errors. The name is required and is the
  importer's choice. `from` is a keyword, reserved everywhere though only
  an import uses it, so it cannot name a variable, function, type, field,
  parameter, import or package.
  An import's name is declared in its file: it may not be a reserved name,
  a builtin's, any package-level name of the package, in any file, or
  another import's of the file, and no local or parameter may reuse it,
  `lexer is an imported package and cannot be redeclared`. The name alone
  is not a value, `lexer is a package, not a value`, nor a type, a function
  or a variable. An import no name of its file uses is `imported and not
  used: lexer`, and importing one package twice in a file is `calc/lexer is
  already imported as a`. A qualified name that names nothing is
  `undefined: lexer.Nothing`; a qualified global may be assigned as any
  global, under the same rules, though a mutable one can never be named
  from another package (below).
- Visibility. A package-level name is private to its package unless its
  declaration starts with `pub`, which makes it usable from every package,
  or `lib`, which makes it usable from every package of its module and from
  no other module: `pub func First(src string) Token`, `lib type Pos
  struct { ... }`, `pub var KindNumber = 1`. A name another package cannot
  see is an error where it is named, `lex is not pub in package
  calc/lexer`, or `isDigit is lib in module calc and cannot be used from
  module other`. A struct's fields are private by default too, each made
  `pub` or `lib` on its own, `pub kind int64`, and a field is never more
  visible than its struct. Reading or writing a field another package
  cannot see, by `=`, a compound assignment, `++` or `--`, is `end is not
  pub in lexer.Token`, and since a literal names every field, a literal of
  a struct with any such field is an error there too, `cannot write a
  lexer.Token literal outside package lexer: field end is not pub` (`fields
  start, end are not pub` for several): the package exports a function
  that builds one instead. Declaring a variable of the struct at its zero
  value, `mut t lexer.Token`, copying one, passing it and comparing two with
  `==`, which compares every field, private ones included, are allowed. A
  `mut` global can be neither `pub` nor `lib`, `a mut variable cannot be
  pub; export a function that reads or changes it`, so only its own package
  changes it. A `pub` declaration may expose only `pub` types, and a `lib`
  one `lib` or `pub` types: every struct or union type, or array of one,
  that a function's parameters or results, a global's type, a field's
  type or a union case's payload's type name, the field's visibility
  capped by its struct's, must be at least as visible, or it is an error,
  `pub function Result exposes hidden, which is not pub` (`which is not
  lib or pub` for `lib`; `variable`, `field kind of Token` and `case
  binary of Expr` for the others). A `pub` or `lib` global is never warned of as
  unused, since the packages that read it may not be in the program; a
  private one still is. `main` packages cannot be imported, so `pub` and
  `lib` mean nothing there and are errors wherever written, on a
  declaration or a field: `pub has no effect in package main, which no
  package can import`. Locals and parameters take neither keyword. A
  method is `pub` or `lib` as a function is, `pub func (t Token) Kind()
  int64`, and like a field is never more visible than its type; calling
  one another package cannot see is `hidden is not pub in lexer.Token`, or
  `Sum is lib in module std and cannot be used from module calc`. A `pub`
  method exposes only `pub` types, its receiver's included, as a function
  does, and a `lib` one, or a `pub` one capped at `lib` by its type, `lib`
  or `pub` types: `pub method Token.Get exposes hidden, which is not pub`
  (see methods).
- An import path starts with the module's name; a relative or absolute
  path is an error that suggests the module-rooted one, `import path must
  start with a module name, such as calc/lexer; relative paths are not
  allowed`, and so is a path with an empty, `.` or `..` part, one in
  another module, `unknown module other in import path other/pkg`, and one
  naming no directory, `cannot find package calc/nothing: no directory
  calc/nothing in module calc`. Imports may not form a cycle, `import
  cycle: calc/a imports calc/b imports calc/a`, a main package cannot be
  imported, `cannot import calc/cmd/calc: it is a main package`, and a
  package other than main may declare nothing named `main`, `func main is
  allowed only in package main`. An import path starting with `std` names
  the standard library (below), and a file compiled on its own can import
  only that, `cannot import calc/lexer: a single file can import only the
  standard library`. Every import is checked, so one run reports each bad
  one.
- The standard library is the module `std`: the directory the builtins
  come from, `$TACK_HOME/src/std`, or `~/.tack/src/std`, whose source is
  [`src/std`](src/std). An import path whose first part is `std` always
  names one of its packages, from any module and from a file compiled on
  its own: `import fmt from "std/fmt"` reads `$TACK_HOME/src/std/fmt`.
  Without std installed, such a program fails as every program does,
  `tack: cannot find std at <path>; set TACK_HOME or run make install`.
  Its packages today are [`fmt`](src/std/fmt/fmt.tck), whose `pub func
  println(s string)` writes `s` and then a newline, `fmt.println("hi")`,
  whose `pub func printf(format string, args ...any)` writes formatted
  text (see printf), and whose `pub func write(b []uint8)` writes bytes
  (see write), [`bytes`](src/std/bytes/bytes.tck), which reads byte
  views and builds byte lists (see bytes), and
  [`text`](src/std/text/text.tck), whose `Text` owns growing bytes (see
  text), [`os`](src/std/os/os.tck), which reaches the operating
  system and runs programs (see os), and
  [`path`](src/std/path/path.tck), which cleans, splits and joins paths
  (see path), [`conv`](src/std/conv/conv.tck), which parses
  integers and floats and writes floats (see conv), [`errors`](src/std/errors/errors.tck), whose
  `Failure` carries a message made at run time beside an error (see
  errors), and [`sort`](src/std/sort/sort.tck), which sorts lists
  (see sort).
  A std package follows every rule a package of the program's module
  does. Its `pub` names are usable from every package, its private names
  only from itself, `hidden is not pub in package std/util`, and its `lib`
  names only from std's own packages: `secret is lib in module std and
  cannot be used from module calc`, or `from a single file, which has no
  module`. Its `mut` globals and extern funcs are never `pub` or `lib`; its
  C files are compiled and linked as the program's are, and an extern func
  none of them defines is `extern func add has no C definition in package
  util`. A std package may import other std packages, and nothing else,
  `cannot import calc/lexer: a package of std can import only std
  packages`. A std path naming no directory is `cannot find package
  std/nosuch: no directory std/nosuch in module std`, and `std/builtin`,
  which holds a copy of the builtins' documentation, is not a package
  programs use: `std/builtin
  documents the builtins and cannot be imported`. A directory inside the
  installed std is in the module std, and so is a copy of std elsewhere,
  through its `tack.mod` (above).
- A package-level variable's initializer may name any immutable global, of
  its own package or an imported one, declared before or after it, and
  select its fields and index it: `var KindPlus = KindNumber + 1`, `var
  Limit = lexer.KindPlus * 10`, `var x = geom.Corners[1].x` (see
  package-level variables). A mutable global is never allowed, except
  under `len` of an array. Globals that need each other are an `initialization
  cycle: a refers to b refers to a`. A type of another package is named
  qualified in a diagnostic, `cannot use lexer.Token as int64`.
- A line ends at a newline, `\n`, or a carriage return and newline,
  `\r\n`, which means exactly the same everywhere: a file saved with
  either compiles to the same program, and a diagnostic gives the same
  line and column. A carriage return not followed by `\n` is not a line
  ending: outside a literal or comment it is an error, `invalid
  character`, and inside a string or character literal it is a byte like
  any other.
- Line comments, `// ...`, run to the end of the line and can be written
  anywhere a line can end: before `package`, between declarations, after a
  statement, or inside an expression, argument list or array literal that
  spans lines. They mean the same as the newline that ends them. Block
  comments, `/* ... */`, can be written anywhere a space can, and nest:
  each `/*` inside one needs its own `*/`, so code holding one can be
  commented out whole. As in Go, a block comment that spans a line break
  means the same as a newline, and one on a single line means the same as
  a space, so `x := 1 /* one */ + 2` is one statement. One never closed is
  an error, `block comment not terminated`. `//` inside a block comment,
  and `/*` inside a line comment, are text.
- `asm` is not a keyword: it is a name like any other, of a variable, a
  function, a type, a field or a method, and Tack has no inline assembly.
  C reserves it, which matters only for a name C sees as written: an
  extern func's C name, or a field of a struct `tack.h` defines (see
  [docs/extern.md](docs/extern.md)).
- A `main` package must declare a `func main()`, which takes no parameters
  and declares no result. No other package may declare anything named
  `main` (see imports).
- Top-level `func name(x int64, s string) int64 { ... }` declarations. Parameters
  are Go-style, one type each, no grouping. A function declares no result,
  one, or several in parentheses, `func divmod(a int64, b int64) (int64, int64)`, and
  `return a / b, a % b` gives one value for each. Results are unnamed. A
  function written with a receiver before its name, `func (b Builder)
  len() int64`, is a method of a struct type (see methods). A contract,
  `ensures` and a condition, may come after the results, before the body,
  `func must(err error) ensures err.code == 0 { ... }` (see contracts).
- Mut parameters. A parameter written `mut p T`, as in `func scale(mut p
  Point, k int64)`, is a mutable reference to the caller's variable for the
  duration of the call: inside, `p`, `p.x` and `p.cells[i]` read and write
  the caller's variable directly, `p = Point{...}` replaces it, and
  `p.x++` changes it. Every other parameter is an immutable copy of its
  argument, as before. The call writes `mut` before the argument,
  `scale(mut b, 10)`, and only there: leaving it out for a `mut` parameter
  is an error, `scale takes p as mut; pass mut b`, and so is writing it
  for a plain one, `count does not take t as mut; pass t`. When the
  argument could not be passed as `mut` either, the error says why and
  what fixes it instead: `scale takes p as mut,
  but b is immutable; declare it with mut, then pass mut b`, with a note
  at `b`'s declaration. The reason and the fix are the ones writing `mut`
  would get, below. A string's byte is told that `strings are immutable`,
  and a range loop variable, a type switch variable or the binding of a
  union's payload, none of which can be declared `mut`, gets no fix.
  What follows `mut` must be a place the caller may change: a mutable
  local, a `mut` parameter, a field of one, `mut b.lo`, or an element of
  one whose index is proven in range (see arrays), `mut cells[i]`, nested
  in any way, `mut w.rows[1].pts[j].y`. Anything else is an error: an immutable
  variable, `cannot pass mut c: c is immutable; declare it with mut`,
  with a note at its declaration, a plain parameter, `... declare it with
  mut in the signature`, or, for a closure's, whose signature the call
  fixes, `n is a closure parameter, whose type the call fixes; copy it
  into a mutable local`, an immutable global, `cannot pass mut limit:
  limit is immutable; copy it into a mutable local`, a mutable global,
  `cannot pass mut g: g is a mutable global, which any function can
  reach; copy it into a mutable local`, a range loop variable, `cannot
  pass mut x: x is a range loop variable, which cannot be mut`, and so a
  type switch variable and a union payload's binding, `n binds a union's
  payload, which cannot be mut`, a string's byte, whatever holds the
  string, `cannot pass mut s[0]: strings are immutable`, and a value
  that is not a place, such as a call's result or a literal, `only a
  variable, a field or an element can be passed as mut`.
  The argument's type must be the parameter's exactly, with no widening:
  passing a `mut int32` to `mut n int64` is `cannot use int32 as int64 in
  argument to bump`. A reference can never be stored or returned: there is
  no syntax for it, and `mut x` anywhere but as a call's argument is an
  error, `mut b can only be passed to a mut parameter`. Inside the callee,
  `q := p` copies the caller's value and `return p` returns a copy; passing
  it on, `bump(mut p.x)` or `scale(mut p, 2)`, is how a reference reaches
  further, through any number of calls, recursion included, and deferred
  code may read, write and pass on a `mut` parameter like any other
  statement. During a call, a place passed as `mut` is reachable only
  through its parameter, so no two `mut` arguments of one call may
  overlap. Places compare as paths: different fields of one struct never
  overlap, so `swap(mut p.x, mut p.y)` is allowed, and so is
  `swap(mut a[0], mut a[3])`, but the same place twice is an error, `x is
  passed as mut twice in this call; a mut argument must be the only way to
  reach x during the call`, and so is a place with a part of it, `mut p`
  and `mut p.x`, or `mut a` and `mut a[1]`: `p.x is part of p, and both
  are passed as mut in this call; ...`, each with a note at the first.
  Two elements of one array may be passed together only at indexes known
  to differ: known values that differ, or indexes the facts in force keep
  apart, as `i` in `[0, 1]` and `j` in `[2, 3]` after `if 0 <= i && i < 2
  && 2 <= j && j < 4`; otherwise `a[i] and a[j] may overlap, and both are
  passed as mut in this call; elements of one array can be passed as mut
  together only at indexes known to differ`. A plain argument that reads
  a place passed as `mut` in the same call is not an error: it is
  evaluated, left to right with the others, before the call begins, and
  passed as a copy, so in `scaleBy(mut b, b)` the callee's `q` keeps the
  value `b` had, however it changes `p`. A `mut` parameter counts as a mutable variable for proofs, so
  a fact about it holds up to the first write to it that can reach the
  use, and a call that passes a variable, or a part of it, as `mut`
  assigns it at that call, once its arguments are evaluated (see
  division). An extern func may take a `mut` parameter, which C
  gets as a pointer (see extern C). A method's `mut` receiver is a `mut`
  parameter too, and its call, `mut b.write(s)`, passes the receiver as a
  `mut` argument under every rule above (see methods).
- Own parameters. A parameter written `own p T`, as in `func failure(err
  error, own detail text.Text) errors.Failure`, takes its argument's value
  from the caller, which writes `own` before it, as `mut` is written at
  both ends: `f := failure(e, own t)`. The caller's variable is moved, so
  it cannot be used again until it is assigned a new value, `t is used
  after it was moved`, and the callee owns the value: it can read it,
  borrow it to a plain or a view parameter, return it, store it in a
  struct, a list or a map, move it on, `give_back(own p)`, or leave it,
  and it is freed when the function ends, at every exit, early returns
  and returns from loops included, after its defers. An own parameter is
  immutable, `cannot assign to parameter p`, `cannot pass mut p: p is an
  own parameter, which is immutable; move it into a mutable variable with
  mut x := p`, and `mut u := p` moves it into a variable that can change.
  Inside the callee it is followed as a local is: a use after it may have
  been moved, a move in a loop, a defer reading it before a later move,
  and a defer moving it are each the error a local's would be. What
  follows `own` at a call is a local variable or an own parameter of the
  caller: a plain parameter, `cannot move p: it is a parameter, which
  borrows its caller's value`, a `mut` one, a field, `cannot pass own s.l:
  a field cannot be moved on its own`, an element, `cannot pass own ls[0]:
  an element cannot be moved out of what holds it`, and a list being
  ranged over cannot be passed as `own`. A value no variable holds, a
  call's result, a literal or a union's case built through its type,
  `Expr.eof`, is passed with no `own`, `failure(e, text.new(alloc))`,
  and writing it is `cannot pass own g(): only a variable can be passed
  as own; pass g() without own`, which is not reported when the value
  has an error of its own. Leaving `own`
  out for a variable is `sink takes l as own; pass own t`, writing it for
  a parameter that is not own, the builtins' included, is `f does not
  take t as own; pass t`, and `own x` anywhere but as a call's argument is
  `own x can only be passed to an own parameter`. `append`, `put` and
  `replace` move what they store with no `own`, as before. An own
  parameter's type must own something, a list, a map, a struct, an array
  or a union holding one, or a type parameter: `int64 cannot be an own
  parameter: it owns no list or map, so it is copied; remove own`, `Allocator`
  and a function type alike, and a view, an `any`, a variadic parameter, a
  method's receiver, `a method cannot take its receiver as own; take it as
  a plain or a mut receiver`, and an extern func's parameter, `parameter l
  of extern func take cannot be own: C is not held to free what it is
  given`, are errors; `mut own` is `a parameter cannot be both mut and own;
  an own parameter is moved into a mutable variable with mut x := p`. A
  variable passed as `own` leaves the caller as the call begins, so
  nothing else in the call may name it, no other argument at any depth,
  no call nested in one, no receiver and no function value being called:
  `f(t, own t)`, `f(own t, t[0:])`, `f(own t, len(t))`, `f(t, g(own t))`,
  `f(mut t, own t)` and `s.take(own s)` are each `cannot use t alongside
  own t in this call: it is moved into the call, and nothing else in the
  call may reach it`, with a note at the own argument, and `f(own t, own
  t)` is reported at the second. Within one argument, which is
  evaluated left to right, a later name of the variable is a use after
  a move, as anywhere: `use(g(own t) + g(own t))` and `use(g(own t) +
  len(t))` are `t is used after it was moved`, while `use(len(t) + g(own
  t))` reads `t` before it moves. A view a function returns cannot point
  into an own parameter, which is freed as it returns, `cannot return a
  view of l[0:]: l is an own parameter, which is freed when the function
  returns; ...`. In a generic function, `own x T` takes a value of the
  type parameter, and its body passes one on as `own x`; a call whose
  type argument owns nothing passes a copy, `push(mut ints, n)`, and
  writing `own` for it is `cannot pass own n: int64 owns no list or map,
  so it is copied; pass n`. `own` is part of a function's type,
  `func(own list[int64]) int64`, and a method's signature when a type
  satisfies an interface. In C an own parameter is passed by value, as a
  plain one is; the caller's variable is emptied as the argument is
  evaluated, and the callee's block frees the parameter at every exit,
  which an emptied one, moved on, makes a no-op. `exit` in the callee
  frees it as it frees every buffer.
- Drop. `drop(own x)` frees `x` at once, exactly as the end of its block
  would, and leaves it moved from: it is a statement, `drop returns no
  value` anywhere else, and to the checker it is a move of `x` into a
  call that frees it. So a use after it is `x is used after it was
  moved`, with a note at the drop, and a use after a drop on some paths
  only is `x is used after it may have been moved`; a drop is allowed on
  some paths only, the block's end freeing `x` on the others. A drop
  twice is the use after a move, and a drop in a loop the next iteration
  reaches again, of a variable a defer reads later, `l is moved after this defer, which reads
  it when its block ends`, in deferred code of a variable declared
  outside it, in a closure of a capture, `a closure cannot move l, which
  it captures: the closure may run more than once`, and of a list being
  ranged over, or of a range's or a union pattern's binding into one, are
  each the error a move is. Assigning `x` a new value makes it usable
  again, and ends every fact about it, `len(x)` included, so a proof or
  a loop's bound that read it does not hold past the assignment. `x` is a
  local variable or an own parameter whose type owns something, written
  with `own`: `drop(l)` is `drop takes x as own; pass own l`, a plain
  parameter `cannot move l: it is a parameter, which borrows its caller's
  value`, a `mut` one `cannot move l: it is a mut parameter, whose value
  stays its caller's`, a view `cannot drop v: a view only borrows what it
  points into`, a type that owns nothing `cannot drop n: int64 owns no
  list or map, so there is nothing to free; remove the drop`, a field
  `cannot drop b.items: a field cannot be moved on its own; take it out
  with replace`, an element `cannot drop ll[0]: an element cannot be
  moved out of a list; take it out with pop or remove`, a map's value
  `... take it out with delete`, and anything but a variable `cannot drop
  numbers(): only a variable can be dropped`. In a generic function,
  `drop(own t)` of a type parameter's value is allowed, and an instance
  whose type argument owns nothing drops nothing. `drop` is a builtin's
  name, so no function may be declared with it. In C a drop is the free
  the block's end would emit, the same helpers in the same order, a
  struct's fields and a list's elements last first, then the variable
  emptied, so the block's end, a defer, `?`, `break` or `return` frees it
  no second time.
- Views. A parameter written `v []T`, as in `func sum(v []int64) int64`, is
  a read-only view of elements of type `T`: one function takes arrays of
  any length. An array `[N]T` of exactly the element type passes to it as
  it is, `sum(a)`, with no widening, so `[4]int32` is `cannot use [4]int32
  as []int64 in argument to sum`, and a `[]int32` is not a `[]int64`
  either. The array must be a place, a variable or a parameter, a `mut`
  one included, or a field or an element of one, `sum(p.items)` or
  `sum(rows[i])`; a call's result or a literal is `cannot view a temporary
  array; store it in a variable first`. A string's bytes pass through the
  conversion `[]uint8(s)`, which takes only a string, any string, a literal
  or a call's result included, and is allowed only as an argument to a
  `[]uint8` parameter, `[]uint8(...) can only be passed to a []uint8
  parameter`; a string without it is `cannot use string as []uint8 in
  argument to count; view its bytes with []uint8(s)`. A list of exactly
  the element type passes to one as it is, `sum(l)`, and is never copied
  for it (see lists). A view parameter is
  passed on to another of the same type as it is, `sum(v)`. A view has
  `len(v)`, an `int64` never known, with the facts a string variable's
  length has; `v[i]`, with `i` proven in range against `len(v)` as for a
  string, or the checked form `x, ok := v[i]`; every range form, `for
  i, x := range v`, whose index is proven for `v`; and slicing, `v[i:j]`,
  passed on (see slicing), and being passed as `any`, when its element
  is a basic type, which printf writes as a string's bytes for a `[]uint8`
  (see any). Nothing else: no `==`, `operator == is not defined on
  []int64`, no conversion, no printing with `print`. A
  view is the type of a parameter or of a result only (see view results):
  anywhere else, a local, a field, a global or an array's element, it is
  `a view can only be the type of a parameter or of a result`, a variable
  declared from one, `x := v`, is `cannot declare x as []int64; a view
  can only be the type of a parameter or of a result`, and a view of
  views is `the element type of a view cannot be a view`. Its
  element type may be owned, `[]list[uint8]`, whose elements it reads in
  place as a list does its own (see lists), though not a type
  parameter, `a view cannot hold S: a view's element cannot be a type
  parameter`. A view
  is read-only: `mut v []T` is `a view cannot be a mut parameter; views are
  read-only`, and assigning `v`, an element or a field of one, or passing
  either as `mut`, is `cannot assign to v[0]: v is a read-only view` or
  `cannot pass mut v[0]: v is a read-only view`. Like a plain parameter, a
  view shows the callee the array as it was when the argument was
  evaluated, for the whole call: when the same call passes an overlapping
  place as `mut`, `f(mut a, a)`, or a call nested in its arguments does,
  or the array is part of a mutable global, which the callee could change,
  the view is of a copy taken then. So facts about `len(v)` and the
  elements hold for the whole function. An extern func may take a view,
  which C gets as a pointer and a length (see extern C). A variadic
  parameter is a view too, and a view is passed on to one with a spread,
  `f(v...)` (see variadics).
- View results. A function or a method may return a view, when it is its
  only result: `pub func (t Text) bytes() []uint8 { return t.b }` lets
  another package read a `Text`'s bytes in place, with no `pub` field and
  no copy. It may return only what its caller passed it and it does not
  change: a view parameter, a list or an array that is a parameter that
  is not `mut`, or a field or an element of one, a slice of any of these,
  `return v[1:]`, or a call returning a view whose arguments are all such
  places, `return rest(v)` or `return t.bytes()`. A union pattern's
  binding and a range loop's element variable over owned elements refer
  in place into what they match or range over, so they are such places
  when that is one: in `func (e Expr) name_bytes() []uint8`, `case
  name(n): return n.bytes` returns a view into `e`, and `for _, w := range
  ws { return w }` one into the list parameter `ws`. Through a `mut`
  parameter or receiver, a local, a global or a call's result they are
  not: `cannot return a view of n.bytes: n refers into e, and e is a mut
  parameter; a returned view can only point into what a parameter that is
  not mut holds`. A range element of a copyable type is a copy, a local
  of its own. An array is returned
  only when it is reached through an element of a list or a view, which
  the caller holds: a plain parameter is its function's own copy, so
  `return a` of an array parameter `a`, or of an array field of a plain
  parameter, is `cannot return a view of a: the function holds its own
  copy of a, and so of the array, which ends when it returns; an array is
  returned as a view only when it is reached through an element of a list
  or a view`. Anything else is `cannot return a view of l: l is a local
  variable; a returned view can only point into what a parameter that is
  not mut holds`, and likewise `g is a global`, `t is a mut parameter`, a
  `mut` receiver included, and `it is a temporary`; a string's bytes are
  `cannot return []uint8(s): a returned view cannot point into a string's
  bytes`; and a call whose view may point into such a place is `cannot
  return whole(a): the view it returns may point into a, and a is a local
  variable; ...`, values a variadic call packs included. A view among
  several results is `a view can only be a function's only result`, an
  extern func's view result is `extern func f cannot return a view:
  nothing tells the checker what C's view points into`, and a view of a
  type parameter is reported as for a parameter. An interface's method may
  return a view, and a generic function may return the one a type
  parameter's method returns. The view a call returns is second-class: it
  is used only inside the expression that consumes it, passed to a view
  parameter, a variadic one by a spread and an extern func's included,
  `bytes.equal(t.bytes(), b)`, passed as `any` when its element is a
  basic type, `fmt.printf("%s", t.bytes())`, given to `len`, indexed with
  the checked form, `c, ok := t.bytes()[i]`, sliced, ranged over, `for i,
  c := range t.bytes()`, or returned on from a function returning a view.
  An index
  without `ok`, or a slice past `[:]` and `[0:0]`, is never proven, since
  no fact is about a call: `index 0 may be out of range; use v, ok :=
  ...[0]`. Anywhere else, stored, assigned, compared, converted, printed
  with `print`, or passed as a field's value, it is `t.bytes() is a view,
  []uint8, which can only be passed to a view parameter, given to len,
  indexed, sliced, ranged over or returned`. Written as a statement,
  `t.bytes()`, the call is made and its view discarded, as any call's result
  may be, borrowing nothing past the statement. The view may point into every argument of
  its call that is not `mut` and holds data, a list, an array, a struct,
  a union, a type parameter's value or a view, the receiver included, and
  reads them in place, as a list borrowed by a plain argument is: a call
  passing it alongside, at any depth, a `mut` argument that may overlap
  one of them is `cannot pass t.bytes() alongside mut t in this call: the
  view it returns may point into t, and a mut argument must be the only
  way to reach t during the call`, as is an index or a slice of it whose
  own index or bounds pass one, `cannot index t.bytes() alongside mut t
  in its index: the view it returns may point into t, ...`, and a range
  over it cannot change any
  of them while it runs, `t.bytes() is being ranged over, and the view it
  returns may point into t, so this loop cannot pass mut t`, nor assign,
  move or pass as `mut` anything inside them or holding them, a defer in
  the loop's body included. In C a view result is the view's struct,
  `tack_view_<T>`, returned by value: a pointer into the caller's data and
  a length, with nothing copied or freed.
- Variadics. A function's last parameter may be written `args ...T`, as
  in `func sum(label string, nums ...int64) int64`, and then takes zero or
  more values. Inside the function it is a read-only view, `[]T`, under
  every rule a view parameter follows (see views): `len(nums)`, with the
  facts a view's length has, a proven or checked index, range, slicing,
  and passing it on, to a `[]T` parameter, `total(nums)`, or to another
  variadic with a spread; it is never assigned, `mut` or stored. A call
  passes it any number of values after one for each other parameter,
  `sum("x")`, `sum("x", 5)`, `sum("x", 1, 2, 3)`, each used as a `T` as a
  plain argument to a `T` parameter is: an untyped constant takes `T` and
  must fit it, a narrower integer widens to it, and anything else is an
  error, `cannot use string as int64 in argument to sum`, a `mut` argument,
  an array or a view included. Too few values for the other parameters is
  `sum expects at least 1 argument(s), got 0`. A call may instead pass a
  view on whole, with a spread after its last argument, `sum("x", v...)`:
  what follows a view parameter, a variadic one included, an array that
  is a place, a slice, `sum("x", a[1:]...)`, or `[]uint8(s)` for a
  `...uint8`, exactly as a view argument may be, with the same errors,
  `cannot view a temporary array; store it in a variable first`, and the
  same copies. A spread is the whole variadic: `sum("x", 1, v...)` is
  `cannot use ... in a call with other values for nums`, `sum(v...)` is
  `sum expects 1 argument(s) before v..., got 0`, and a spread in a call
  to a function or builtin that is not variadic, or in a conversion, is
  `cannot use ... in a call to non-variadic f` or `cannot use ... in a
  conversion to int64`. The values are plain arguments: evaluated left to
  right with the call's other arguments, each once, and copied, so one
  that reads a place the same call passes as `mut`, `bump(mut n, n)`,
  keeps the value it had when it was evaluated. Nothing is known inside
  the function about how many values a call passed: its body is checked
  once, for every caller. `...` anywhere but before the last parameter's
  type, before another parameter or on a result's, a variable's, a
  field's or an element's type, is `can only use ... with the final
  parameter`, and after any argument but the last `can only use ... with
  the final argument`; `mut args ...T` is `a variadic parameter cannot be
  mut; it is a read-only view`, and `args ...[]T` is `the element type of
  a variadic parameter cannot be a view`. A function is only ever called,
  so a variadic is filled at a call and nowhere else. In C the values are
  an array on the caller's stack, built for the call, and the parameter
  the view of it, `tack_view_<T>`, with a null pointer and a length of 0
  for no values. An extern func may take one (see extern C); a builtin
  may not, but the generic `append` is one, `append(mut l list[T], elems
  ...T)`, under these rules exactly (see lists). An `...any`'s values are `any`s: `for _, v := range args` and
  `v, ok := args[i]` declare immutable `any` locals, and `switch v :=
  args[i].(type)` asks which type one holds (see any and type switches).
- `any`. A parameter `a any` takes a value of any basic type, an integer
  of any type, a `bool` or a `string`, which the function sees as a
  reference to the caller's value with its type known only at run time:
  in C, a type id and a pointer, `tack_any`. A value passes to it as it is,
  with nothing written, `show(5)`, `show("s")`; an untyped constant is an
  `int64` there, a character literal a `uint8`, and a constant no `int64`
  holds is `constant 18446744073709551615 overflows int64`. `any(x)` is no
  conversion, `cannot convert int64 to any`. A view of a basic type,
  `[]uint8`, `[]int64`, `[]string` and so on, passes to one as it is too:
  a view parameter, `show(v)`, a slice of a list, an array, a view or a
  string, `show(l[:])`, `show(a[1:])`, a call returning a view,
  `show(t.bytes())`, and an `any` holding one refers to the view, and so
  to the caller's elements, for the call. The elements are borrowed as a
  view argument's are, under the same rules: a list, or what a view a
  call returns may point into, passed as `mut` in the same call is
  `cannot pass l alongside mut l in this call: a list is not copied for a
  call, and a mut argument must be the only way to reach l during it`,
  and an array the call could change, through a `mut` argument or as a
  mutable global, is viewed as a copy taken at the call. A list or an
  array itself is not viewed for an `any`, since nothing asks for a view
  there: `show(l)` is `cannot pass list[uint8] as any: only the builtin
  types and views of them can be passed as any for now`, and `show(l[:])`
  views it. A string's bytes, `show([]uint8(s))`, are `[]uint8(...) can
  only be passed to a []uint8 parameter`; the string itself passes. An
  array, a struct, a union, a list, a view of any other type, `[]Point`
  or `[]any`, cannot be passed as one yet, `cannot pass Point as any:
  only the builtin types and views of them can be passed as any for now`,
  nor can a `mut` argument, `f does not take x as mut; pass x`. The value
  is a copy taken at the call, as a plain argument's is: `f(mut x, x)`
  gives the `any` `x` as it was before the call, and a mutable global is
  copied too. `args ...any` is a
  variadic of them, a read-only `[]any` under every view rule (see
  variadics), and a view parameter may be `[]any`. `any` is second-class,
  as a view is, and more: unlike a view it is never a result. Written as
  the type of anything but a parameter, or a variadic's or view
  parameter's element, a local's, a field's, a global's, a result's or an
  array's element's, it is `any can only be the
  type of a parameter`, `var b any = a` included. An immutable local may
  still hold one, declared from an `any` with its type left out: `b := a`,
  `v, ok := args[0]`, `for _, v := range args`, and a type switch's `v`
  (below). It refers to the caller's value just as the `any` it came from
  does, and for no longer, since nothing an `any` could be kept in
  outlives the call: no field, global, result, array, list or mutable
  variable holds one, and a `defer` runs before the call returns. A
  mutable one, `mut b := a` or `mut v, ok := args[0]`, is `cannot declare
  mut b as any; an any variable cannot be reassigned`. `v, ok := args[i]`
  with `i` out of range gives the empty `any`, which holds no type: no
  assertion matches it, a type switch runs its default, printf writes it
  `%!v(invalid)`, and C sees a zeroed `tack_any`, type id 0. `mut a any`
  is `mut has no effect on an any parameter; take mut p T to change the
  caller's value`. Inside its function an `any` is passed on, to another
  `any` parameter, `g(a)`, or as a value of an `...any`, `many(a,
  args[0])`, and an `[]any` spread, `many(args...)`, `many(args[1:]...)`,
  or passed to an `[]any` parameter, or asked which type it holds, with a
  type assertion or a type switch (below). It supports no operator,
  `operator == is not defined on any` (an expression `switch` on one too),
  no conversion, `cannot convert any to int64`, and no use as another type,
  `cannot use any as string in argument to print`. `any` is a type's name,
  so it cannot name anything else, `cannot use any as a name; it is a
  type`.
- Type assertions. `v, ok := a.(T)` asks whether the `any` `a` holds a
  `T`, exactly: `v` is the value when it does and `T`'s zero value when it
  does not, and `ok` says which. An `int8` passed as `any` is not an
  `int64`, so `a.(int64)` is false for it, and an untyped constant, passed
  as an `int64`, is one. It is a checked form, as `q, ok := a / b`, `v, ok
  := a[i]` and `v, ok := T(x)` are, under the same rules: the value may
  be `_`, `_, ok := a.(T)`, but not the `ok`, `the ok a.(T) gives cannot
  be discarded; bind it to a variable instead of _`, each may have its own `mut`, `v, ok
  = a.(T)` assigns existing mutable variables, `T` widening to `v`'s type,
  and it must be the whole value, `the checked form must be the whole
  value: v, ok := a.(int64), then compute v + 1`. There is no one-value
  form, since a mismatch would have no value to give: `a.(int64)` anywhere
  else, a declaration, an argument, a condition, is `a type assertion needs
  two results; write v, ok := a.(int64)`, and three names are `assignment
  mismatch: 3 variables but a type assertion gives 2 values`. `T` is one
  of the fourteen basic types an `any` can hold; any other, a struct, an
  array, a view of another type, a list, `error`, `Allocator` or `any`,
  is `an any never holds Point: only the builtin types and views of them
  can be passed as any for now`, and an operand that is not an `any` is
  `cannot assert on x: it is int64, not any`. A view an `any` can hold is
  asked for only with a type switch: `v, ok := a.([]uint8)` is `a type
  assertion cannot give []uint8, a view; ask with a type switch, as in
  switch v := a.(type)`, since `v` would be a view variable a mismatch
  left empty. Nothing about what an `any` holds is known at compile time, so an
  assertion proves nothing, but `ok` and `v` give facts as any variables
  do: `if ok && v != 0` proves a divisor. In C, `a` is held in a
  temporary, its type id compared with `T`'s, and `v` read as `*(const T
  *)ptr`, or `T`'s zero value.
- Type switches. `switch v := a.(type) { case int64: ... case int8, int16:
  ... default: ... }` runs the first case naming the type `a` holds,
  exactly, as an assertion matches. In a case naming one type, `v` is a
  value of that type, `a`'s value; in a case naming several, and in the
  default, `v` is the `any` itself. `v` is immutable in every case, and
  each case's `v` is a variable of its own. `v :=` is a binding, not an
  init statement, which a switch never has: `switch x := f() {` is `a
  switch has no init statement; only a type switch binds a name, as in
  switch v := x.(type)`, and `switch mut v := a.(type)` is `cannot use mut
  on a type switch variable`. `switch a.(type) { ... }` binds nothing. `v`
  is used if any case reads it, and otherwise is warned of, `declared and
  not used: v`, at the binding, since a case that does not need it need
  not read it.
  `v` may reuse no name visible at the switch, as no variable may, so
  `switch a := a.(type)` is `a is already declared`. A case's values are
  types: a type named twice, in one case or two, is `int64 appears twice
  in this type switch`, a type an `any` never holds is the assertion's
  error, and a value is a parse error, `expected 'type name' found '1'`.
  `default` is optional, and there is no check that the cases cover every
  type. A case may name a view of a basic type, `case []uint8:`, and its
  `v` is then that view, the caller's, under every rule a view parameter
  follows: `len(v)`, a proven or checked index, range, slicing, passing
  it on to a view parameter or as `any`, printing it with printf, and
  nothing that would keep it: `w := v` is `cannot declare w as []uint8; a
  view can only be the type of a parameter or of a result`, `return v` is
  `cannot return a view of v: v is a local variable; ...`, and `v[0] = 1`
  is `cannot assign to element of immutable variable v`. Otherwise it is a switch: `a` is evaluated once, the cases are
  tried top to bottom and the default last, wherever it is written, there
  is no fallthrough, `cannot fall through in a type switch`, and `break`
  and `continue` act on the loop around it. It gives no facts, since what
  `a` holds is never known at compile
  time. `.(type)` anywhere but a switch's tag is `.(type) can only be the
  tag of a switch`. A switch on a union matches its cases with the same
  shape of switch, each case naming one, and binds a payload in a
  clause's own value, `case int(n)`, rather than in the header (see
  unions). In C, a type switch is a chain of ifs on a
  temporary holding `a`, each comparing its type id, and `v` is `*(const
  T *)ptr` in a one-type case, or the `tack_any` itself, declared only in
  a case that reads it.
- printf. `fmt.printf(format, args...)`, from `std/fmt`, writes `format`
  to standard output with each verb replaced by its argument: `%d` an
  integer of any type in decimal, `%x` one in lowercase hexadecimal, with
  a `-` before a negative value as Go writes it, `%s` a string's bytes,
  or a `[]uint8`'s, `fmt.printf("undefined: %s\n", name.bytes())`, `%t`
  a bool, `%c` a `uint8` as its byte, `%v` any of them as its own verb
  would, and `%%` a `%`. No verb takes a view of any other type, which
  is `printf verb %v does not take []int64`, and written `%!v([]int64)`
  when the format is not known: printf writes no view's elements but
  bytes. There are no widths or flags. A verb is the
  `%` and the UTF-8 character after it. When the compiler knows the
  format, a literal or an immutable variable holding one, every verb is
  checked against its argument, and each mistake is an error: `printf
  verb %d does not take string`, `unknown printf verb %q` (a verb that is
  not graphic, such as a newline, is quoted, `unknown printf verb
  "%\n"`), `printf format ends in a lone %`, and `printf format has 2
  verbs but 1 argument` at the format, or at the arguments left over when
  there are more than verbs. An `any` passed on matches any verb, since
  its type is known only at run time, an `any` local and a type switch's
  `v` in a case naming several types included, while a type switch's `v`
  in a case naming one type is a value of that type, checked as one, and
  a spread, `printf(f, args...)`, has its verbs checked but not its
  count. A format the
  compiler does not know is accepted, and each mistake is written into the
  output as Go's `fmt` writes it, never undefined: `%!d(string=hi)` for a
  verb given an argument it does not take, or an unknown verb,
  `%!d(MISSING)` for a verb with none left, `%!(EXTRA int64=5, bool=true)`
  after the output for arguments no verb took, and `%!(NOVERB)` for a `%`
  ending the format. printf writes through the same C `stdout` as
  `print`, so the two interleave in call order, formats integers itself,
  never calling C's `printf`, and reports no error. Only `std/fmt`'s
  `printf` has its format checked: a function of any other package named
  `printf` is an ordinary function.
- write. `fmt.write(b)`, from `std/fmt`, writes exactly the bytes of `b`
  to standard output, each as it is, `0` and `0xff` included, and nothing
  else: no newline and no formatting. Its parameter is a view, `b []uint8`,
  so a `list[uint8]`, an array of `uint8`, a slice of either, or a
  string's bytes, `fmt.write([]uint8(s))`, can be passed, and a view of no
  bytes writes nothing. A string itself is not a view: `cannot use string
  as []uint8 in argument to fmt.write; view its bytes with []uint8(s)`.
  write goes through the same C `stdout` as `print` and `printf`, with one
  `fwrite`, so the three interleave in call order, and like them it
  reports no error.
- bytes. `std/bytes` is written in Tack alone, with no C: its views go
  in and its lists come out. Its read functions take `[]uint8` views, so
  a `list[uint8]`, an array or a slice of either, or a string's bytes,
  `[]uint8(s)`, never a string itself, and return an index, a count or a
  bool, allocating nothing: `equal(a, b)`, the same bytes in the same
  order; `index(s, sub)`, the first index of `sub` in `s`, or -1, and 0
  for an empty `sub`; `index_byte(s, c)`, the first index of the byte
  `c`, or -1; `has_prefix(s, prefix)` and `has_suffix(s, suffix)`, true
  for an empty one; and `count(s, sub)`, the non-overlapping occurrences
  of `sub` found from the start, so `"aa"` occurs twice in `"aaaa"`, and
  `len(s) + 1` for an empty `sub`. Its append functions format an integer
  into the end of the caller's list, `err := bytes.append_int(mut b, v)`
  for an `int64` in decimal, `-` before a negative one, the smallest
  `int64` included, `append_uint` for a `uint64` in decimal, and
  `append_hex` for a `uint64` in lowercase hexadecimal with no `0x`, as
  printf's `%x` writes it. Each adds all its bytes with one append or
  none: when `b` cannot grow, the error is the builtin append's,
  `error{code: 1, msg: "out of memory"}`, and `b` is left exactly as it
  was. `r, err := bytes.repeat(s, n, alloc)` makes a new list through
  `alloc` holding `n` copies of `s`, reserved once at its final length;
  `n` of 0, or an empty `s`, gives an empty list, which allocates
  nothing. Where Go's `bytes.Repeat` panics, it returns an error and
  allocates nothing, as reserve does: a negative `n` is `error{code: 2,
  msg: "negative count"}`, and a length that overflows an `int64`, or
  that `alloc` cannot give, `error{code: 1, msg: "out of memory"}`. Only
  a function that makes a new list takes an `Allocator`. The errors and
  the list follow the rules every call does: `bytes.append_int(mut b, 1)`
  alone is `the error bytes.append_int returns cannot be discarded; bind
  it, as in err := bytes.append_int(...)`, `_, err := bytes.repeat(...)`
  is `bytes.repeat returns list[uint8], which cannot be discarded; bind it
  to a variable instead of _`, and the list cannot be read before the
  error is checked. Bytes are bytes: nothing in it knows UTF-8.
- text. `std/text` is written in Tack alone, with no C. Its `pub type
  Text struct` owns a sequence of bytes in a private `list[uint8]`
  field, so no other package can reach the list, write a `text.Text`
  literal, `cannot write a text.Text literal outside package text: field
  b is not pub`, or read the field, `b is not pub in text.Text`; a Text
  frees its bytes when it goes out of scope, as a list does.
  `text.new(alloc)` returns an empty Text that grows through `alloc` and
  allocates nothing until bytes are appended. `t, err :=
  text.from_bytes(s, alloc)` returns a Text holding a copy of the bytes
  of the `[]uint8` view `s`; it is `from_bytes` because `from` is a
  keyword. When `alloc` cannot give the buffer, the error is
  `error{code: 1, msg: "out of memory"}` and the Text is empty, having
  allocated nothing; as for any result beside an error, `t` cannot be
  read before `err` is checked, and `_` cannot drop it, `text.from_bytes
  returns text.Text, which holds a list and cannot be discarded; bind it to
  a variable instead of _`. `t.len()` is the number of bytes,
  `t.equal(s)` whether `t` holds exactly the bytes of the view `s`, so
  `t.equal([]uint8("if"))` compares against a literal, and `t.hash()`
  the 64-bit FNV-1a hash of the bytes, from the offset basis
  `0xcbf29ce484222325` with the prime `0x100000001b3`, so no bytes hash
  to `0xcbf29ce484222325` and `"a"` to `0xaf63dc4c8601ec8c`.
  `err := mut t.append(s)` and `err := mut t.append_byte(c)` grow `t`,
  all or nothing as the builtin append is: when `t` cannot grow the
  error is `error{code: 1, msg: "out of memory"}` and `t` is left
  exactly as it was. `t.bytes()` returns a read-only `[]uint8` view of
  the bytes, in place, which can be passed to `fmt.write`, to std/bytes'
  functions or to any view parameter, ranged over, given to `len`, or
  indexed with the checked form, as any view a call returns. It borrows
  `t`: `mut t.append(t.bytes())` is `cannot pass t.bytes() alongside mut
  t in this call: the view it returns may point into t, and a mut
  argument must be the only way to reach t during the call`, and
  appending to `t` while ranging over `t.bytes()` is an error too. Only
  `new` and `from_bytes` take an `Allocator`. Bytes are bytes: nothing in
  it knows UTF-8, so `len` counts bytes and `equal` compares them
  exactly.
- os. `std/os` reaches the operating system through C, in
  [`os.c`](src/std/os/os.c), that makes one POSIX call at a time and
  decides nothing; every loop, retry and allocation is in Tack. Paths
  and names are `[]uint8` views, passed as they are; one holding a 0
  byte is refused before the OS sees it, `error{code: 4, msg: "path
  contains a NUL byte"}`, or `"name contains a NUL byte"` for `env`.
  What the OS gives back lands in lists Tack owns, appended by C through
  the list's own allocator: `a, err := os.args(alloc)`, a
  `list[text.Text]` of the program's name and its arguments, in order;
  `v, set, err := os.env(name, alloc)`, a variable's value and whether it
  is set; `data, err := os.read_file(path, alloc)`, every byte of a file;
  and `names, err := os.list_dir(path, alloc)`, a directory's entries
  without `.` and `..`, sorted bytewise so the order does not depend on
  the OS. `os.write_file(path, data)` creates a file with mode 0644 or
  truncates it and writes all of `data`; `os.make_dir(path)` makes one
  directory, mode 0755, and no parents; `os.remove_path(path)` removes a
  file or an empty directory, named `remove_path` because `remove` is a
  builtin; `os.rename(old_path, new_path)` renames a file or a
  directory with `rename(2)`, replacing a file, or an empty directory,
  at `new_path` at once, and moves nothing between file systems, which
  is `error{code: 3, msg: "Cross-device link"}`; `info, err :=
  os.stat(path)` reports a file with `stat(2)`, following symbolic
  links, as an `os.Info`, read through its methods: `info.size()`, the
  file's length in bytes, holes of a sparse file included, and
  `info.is_dir()`. Its fields are private, so no program outside std/os
  writes an `os.Info` literal, and fields added later break none. A
  link to nothing is `error{code: 3, msg: "No such file or
  directory"}`, and a size too wide for an `int64` cannot happen, since
  os.c asserts at compile time that an `int64_t` holds every `off_t`;
  and `d, err := os.make_temp_dir(alloc)` makes a new directory,
  mode 0700, in `$TMPDIR`, or `/tmp`, named `tack-` and six characters,
  which the caller removes. `status, err := os.run(argv, mut out, mut
  err_out, alloc)` runs the program `argv[0]` names, looked up on `PATH`
  through `posix_spawnp`, with `argv` as its arguments, this process's
  environment, and `/dev/null` as its standard input, and waits for it.
  It reads the child's standard output and standard error from two
  pipes at once, whichever has something, so neither fills and blocks
  the child, then appends each to `out` and `err_out`, and returns the
  exit status, 0 to 255, a status other than 0 being no error. `alloc`
  makes only run's own lists, all freed before it returns. On an error
  the status is -1, which cannot be read before the error is checked,
  and `out` and `err_out` keep their lengths and bytes: an empty `argv`
  is `error{code: 4, msg: "argv is empty"}`, a 0 byte in an argument
  `error{code: 4, msg: "argument contains a NUL byte"}`, a child killed
  by a signal `error{code: 5, msg: "child killed by a signal"}`, and a
  program not on `PATH` `error{code: 3, msg: "No such file or
  directory"}`. Out of memory while making room for the bytes read is
  the one path where `out`'s capacity may have grown, when only
  `err_out` could not. After an error while reading, run closes its
  ends of the pipes, so the child gets `EPIPE` or `SIGPIPE`, and waits
  for it, killing nothing; there is no timeout. An allocation that fails is `error{code: 1,
  msg: "out of memory"}`; the OS refusing is code 3 and the message
  macOS's strerror gives, the same on every target, `error{code: 3, msg:
  "No such file or directory"}`. A function that fails returns an empty
  list or Text and frees what it made; only `write_file` can leave work
  half done, a file created or truncated and partly written. Every file
  descriptor is opened close-on-exec and closed before the function
  returns, on every path, and every child run starts is waited for. The results follow the rules every call does:
  `_, err := os.read_file(...)` is `os.read_file returns list[uint8],
  which cannot be discarded; bind it to a variable instead of _`, and
  `os.write_file(p, d)` alone is `the error os.write_file returns cannot
  be discarded; bind it, as in err := os.write_file(...)`. The program's
  arguments reach std/os through the generated C main, which stores
  `argc` and `argv` in two globals (see
  [docs/extern.md](docs/extern.md#program-arguments)).
- path. `std/path` is written in Tack alone, with no C, and takes
  slash-separated paths apart and puts them together as Go's `path`
  package does, for `/` alone: no volume names or backslashes, and
  nothing that asks the OS whether a path names anything. Paths are
  `[]uint8` views of bytes, a 0 byte a byte like any other.
  `path.is_abs(p)` is whether `p` begins with a slash, and
  `path.ext(p)` the extension of its last element, from the last dot to
  the end, a view of `p`, empty when the last element has none.
  `path.clean(p, alloc)` is the shortest path naming what `p` does, by
  Go's rules: runs of slashes become one, `.` elements go, each `..`
  goes with the element before it, a `..` at the root goes, and a
  trailing slash goes, so `clean("a//b/./c/..")` is `a/b`,
  `clean("/../a")` is `/a`, and `clean("")` is `.`. `path.base(p,
  alloc)` is the last element, trailing slashes removed, `/` for a path
  of slashes and `.` for an empty one; `path.dir(p, alloc)` is everything
  before the last element, cleaned, so `dir("a")` is `.`; and
  `path.join(a, b, alloc)` is `a` and `b` joined by a slash and cleaned,
  an empty element left out, and an empty Text when both are empty, as
  Go's `path.Join` gives. Those four return a `text.Text`, made through
  `alloc`, since what they give is not always part of what they are
  given, `.` from `""` or `a/b` from `a/./b/c`; each fails only for want
  of memory, `error{code: 1, msg: "out of memory"}`, freeing what it
  made.
- conv. `std/conv` is written in Tack alone, with no C, allocates
  nothing, and parses integers exactly as Go's `strconv.ParseInt` and
  `strconv.ParseUint` do, ported from Go's own code. `v, err :=
  conv.parse_int(s, base, bit_size)` gives an `int64` and `u, err :=
  conv.parse_uint(s, base, bit_size)` a `uint64`, from `s`, a `[]uint8`
  view, so `[]uint8("42")`, a Text's `t.bytes()`, a list or an array can
  be passed; bytes are bytes, so no space is skipped. `base` is 2 to 36,
  the letters `a` to `z` in either case being the digits past 9, or 0,
  when the prefix decides: `0x` or `0X` 16, `0o` or `0O` 8, `0b` or
  `0B` 2, a leading `0` alone 8, and 10 otherwise; with base 0 alone,
  underscores may stand between digits, or after a prefix, as in a Go
  literal, so `1_000` and `0x_ff` parse and `_1`, `1__0` and `1_` do
  not. `bit_size` is 1 to 64, the width the value must fit, 0 meaning
  64, so `parse_int(s, 10, 8)` takes -128 to 127. `parse_int` takes one
  leading `+` or `-`, before any prefix, and `parse_uint` none. The
  errors are Go's: `error{code: 6, msg: "invalid syntax"}` for an empty
  input, a lone sign or prefix, a byte that is not a digit of the base,
  a 0 or 0xff byte among them, or an underscore out of place;
  `error{code: 7, msg: "value out of range"}` for a value past
  `bit_size`; `error{code: 8, msg: "invalid base"}` and `error{code: 9,
  msg: "invalid bit size"}` for a base or bit size Go refuses. They come in Go's order: an empty input is a
  syntax error whatever the base, the base is checked before the bit
  size, and digits from the first, so a value past range is a range
  error even with a bad byte after it. Beside a range error the value
  is the nearest `bit_size` holds, and 0 beside any other, as in Go,
  though, as for every result beside an error, it cannot be read
  before the error is checked. Go's own quirk comes too: in bit size 1,
  `parse_int("-2", 10, 1)` is -1 and no error. `v, err :=
  conv.parse_float(s, bit_size)` reads a float as Go's
  `strconv.ParseFloat` does, correctly rounded to the nearest `float32`
  for a `bit_size` of 32 and the nearest `float64` otherwise: a decimal or
  hexadecimal float as a Go literal writes one, `1.5e-3`, `0x1p-2`, with a
  sign and `_` between digits, or `inf`, `infinity` or `nan` in either
  case, `+Inf` and `-inf` included. Bad syntax is `error{code: 6, msg:
  "invalid syntax"}` and a value past the largest float `error{code: 7,
  msg: "value out of range"}`, beside an infinity, as in Go; a value too
  small rounds to zero or a subnormal with no error.
  `conv.append_float(mut b, f, format, prec, bit_size)` appends `f` to `b`
  as Go's `strconv.AppendFloat` does, `format` being `'e'`, `'E'`, `'f'`,
  `'g'` or `'G'` and `prec` the digits after the point, or in all for
  `g`, `-1` meaning the fewest that read back as `f`; it gives no error,
  or `error{code: 1, msg: "out of memory"}` leaving `b` as it was,
  making room for the whole result before writing any of it, so a
  precision too large for memory fails at once instead of growing `b`
  toward it. Both
  work exactly on a decimal of up to 800 digits, as Go's slow path does,
  in Tack alone, and printf's C uses the same algorithm; the tests check
  both against Go and against each other.
- errors. `std/errors` is written in Tack alone, with no C. An error's
  message is a literal, so what only the run knows, the name that was
  undefined or the path that could not be opened, cannot be in it;
  `pub type Failure struct { pub err error; pub detail text.Text }`
  holds the error beside a detail made at run time, a Text it owns. It
  is owned, as a struct holding a Text is: it moves, has no zero value,
  is borrowed by a plain parameter, taken by an own one, and moved out of
  a `mut` one only by `replace`, may be returned or held by a list or a map, and frees its
  detail when it goes out of scope. A literal, `errors.Failure{err: e,
  detail: t}`, and `errors.new(e, own t)`, whose detail is an own
  parameter (see own parameters), each move a Text built beforehand in
  and cannot fail, `new` taking a Text no variable holds with no `own`,
  `errors.new(e, text.new(alloc))`; `f :=
  errors.from_bytes(e, s, alloc)` copies the bytes of the `[]uint8`
  view `s` into a new detail made through `alloc`, and when `alloc`
  cannot give it, keeps `e` with an empty detail, having allocated
  nothing, so it returns no error of its own and a Failure never loses
  its code for want of memory. The fields are read as fields, `f.err.code`,
  and the detail grown through its own methods, `err := mut
  f.detail.append(s)`, or written with printf, `fmt.printf("%s: %s\n",
  f.err.msg, f.detail.bytes())`. Nothing asks what the error is: a
  Failure of `noerror` is a Failure like any other.
- sort. `std/sort` is written in Tack alone, with no C. `sort.by(mut l,
  less)` sorts the list `l` in place so that no element is less than the
  one before it, by `less`, a scope `func(T, T) bool` reporting whether its
  first argument goes before its second, a closure or a function, as Go's `slices.SortFunc` does
  with a less function in place of a comparison: `sort.by(mut words,
  shorter)`. It is generic over the element type, so a list of any type
  sorts, owned elements, lists and Texts, included. It is a heapsort,
  with an insertion sort for lists of at most 12 elements: it never
  recurses and its stack does not grow with the list, it allocates
  nothing and so returns no error, it moves elements only with `swap`,
  so an owned element is never copied or freed, and it makes O(n log n)
  comparisons in the worst case. It is not stable: equal elements may
  change order. `sort.is_sorted(v, less)`, whose `less` is scope too, reports whether no element of
  the view `v` is less than the one before it, so a list, an array or a
  slice of either may be passed, and an empty or one-element view is
  sorted. For a sorted result `less` must be a strict weak ordering, as
  Go asks; one that is not, always true, always false, or different on
  every call, still ends after O(n log n) calls, reads no element out of
  range and keeps every element, in an order left unspecified, since
  every loop is bounded by indexes, never by what `less` answers. The
  list is passed as `mut`, `sort.by takes l as mut; pass mut l`, and must
  be a list, `cannot use [3]int64 as list[T] in argument to sort.by`.
- Several results are bound together, by position, with
  `q, r := divmod(7, 2)`. Each name is a new variable of its result's type,
  immutable unless that name has its own `mut`: `q, mut r := divmod(7, 2)`
  makes only `r` mutable. `q, r = divmod(7, 2)` assigns the results to
  existing mutable variables instead, each named once, and can be a loop's
  post statement. The value must be one call with exactly that many
  results. A call with several results cannot be used as a single value,
  passed straight to another call, or returned directly as another
  function's results, but it can be called as a statement to discard them
  all, unless its last result is an error (see errors).
- Errors. A function that can fail returns an error, of the builtin type
  `error`, as its last result: `func parse(s string) (int64, error)`, or
  `func check() error` alone. An error anywhere else in a result list is
  `error must be the last result of f`. An error is a struct of two
  fields, `code`, an `int32`, and `msg`, a `string`: a code of 0 is no
  error, whatever the message, and any other code is an error. It is
  written as a struct literal naming both fields, `error{code: 2, msg:
  "not found"}`, any string being a message, since every string is
  static, and `noerror`, a keyword like `true`, is the error that is no
  error, code 0 and an empty message: the zero value of `error`, so `mut e
  error` starts as `noerror`, and a function that succeeds returns it,
  `return n, noerror`. An error does what a struct does: its fields are
  read from any error, `err.msg`, and written through a mutable variable,
  `e.code = 7`; two compare with `==` and `!=`, the code and the message's
  bytes, so a sentinel global, `var notFound = error{code: 2, msg: "not
  found"}`, is tested with `err == notFound`; and an error may be a local,
  a parameter, a result, a field, an array's element or a global, `mut`
  or not, `noerror` and a literal being constant expressions. It has no
  order, no conversion, and cannot be passed as `any`. Three rules hold
  for a call to a function that returns an error:
  - Its error cannot be discarded. A call statement is `the error f returns
    cannot be discarded; bind it, as in _, err := f(...)`, where a result
    that cannot be discarded either, a list or a value holding one, is named
    rather than `_`, `x`, `y` and `z` in order, then `x4` and on: `bind it,
    as in x, err := bytes.repeat(...)`. `_` where the
    error goes, a binding of nothing but `_` included, is `the error f
    returns cannot be discarded; bind it to a variable instead of _`, and a
    variable holding it that nothing ever reads discards it as surely: after
    `err := f()`, `v, err := f()` or `err = f()`, and the same for append,
    reserve and put, an `err` never read is `the error f returns is never
    checked: err is not read`, at its name, not the warning that it is
    unused. Reading it is what checks it: its `code` or its `msg`, or the
    error itself passed on or returned. Only a variable never read at all
    is reported, not one assigned again before its first value was read. A call
    returning an error alone is a value like any other, so it may be
    passed, returned or compared, `if check() == noerror`.
  - The results bound beside the error cannot be read until it is
    checked. After `v, err := f()`, or `v, err = f()`, reading `v`,
    passing `mut v`, or updating it with a compound assignment, `++` or
    `--`, is `v cannot be read before err is checked; check err.code == 0
    first` wherever the facts in force do not prove `err.code == 0`. The
    check is a fact like a divisor's (see division, and
    [docs/safety.md](docs/safety.md#errors)): `if err.code != 0 { return
    0, err }` proves it for the rest of the block, `if err.code == 0 {
    ... }` in its branch, the `else` of `if err.code != 0` in that
    branch, a `switch err.code` in its `case 0:`, `err.code == 0 &&`
    on its right, and a call to a function whose contract ensures it,
    `must(err)` after `func must(err error) ensures err.code == 0`
    (see contracts). Like any fact it holds up to the first write to `err`
    that can reach the read, so binding a new error to it ends it for
    every read after the binding, and a branch ending in `exit` leaves,
    as one ending in `return` does. A return that passes `v` on beside `err` unchecked, `return
    v, err` with `err` the error result, is no read: the caller holds
    `v` beside the same error and checks it there. `v` returned in an
    expression or beside another error is read. Comparing the
    whole error, `err == noerror`, is no check. So that a check is about
    the error that came with `v`, both are bound to local variables,
    never a parameter, a global, a field or an element, `p.x cannot take
    a result of f beside its error; bind it to a local variable`, and an
    error that holds variables is assigned only by a binding that assigns
    them all with it, `v, err = f()`: `err = noerror`, `err.code = 0`,
    `++`, `mut err` or `w, err = g()` are `err holds the error that came
    with v, so only a binding that also assigns v can assign it`. A
    variable bound beside two errors needs both checked.
  - The error is the last result, as above.

  The postfix `?` passes an error on: `x := parse(s)?` calls `parse`
  and, when its error's code is not 0, returns that error, unwrapped,
  from the function it is written in, which must itself return an error
  last; when the code is 0 it gives the call's other results. One result
  beside the error makes `?` a value, usable anywhere a value is,
  `return parse(s)? + 1, noerror` with the overflow proofs any `+`
  needs; none makes it a statement, `check(n)?`; and several are bound
  together, `q, r := divide(a, b)?`, but are not one value, `divide(a,
  b)? gives 2 values`. The results it gives come with no error, so
  nothing holds them and they need no check. A return through `?` runs
  the defers reached and frees every owned value the function holds, as
  a `return` there would, the list the call returned beside its error
  among them, and the other results of the function are empty values
  its caller can never read, since the error beside them is set. It
  applies to a call of a function, a method, `mut c.bump(n)?` included,
  a function value, or append, reserve and put; on anything else,
  `err?` or `noerror?`, it is `cannot use ? on err, which is not a
  call`, and on a call whose last result is not an error `cannot use ?
  on a call to lookup, whose last result is bool, not error`. It binds as
  tightly as a field or an index after it, so `pair(n)?.b`,
  `triple(n)?[2]` and a method call on what it gives, `box(n)?.get()?`,
  work, though a call's parentheses cannot follow it directly,
  `fv()?("x")`, as they cannot follow a call. `(f())?` and `f() ?` are
  the same as `f()?`, and a statement may start with a parenthesis when
  it is a call or a `?` on one, `(f())`, `(check(n))?` or `(check(n)?)`,
  as a loop's init or post statement too, `for check(n)?; i < 3; i++ {`;
  any other, `(x)` or `(a + b)`, is `expected 'statement' found '('`. An expression holding `?`s is evaluated left
  to right and stops at the first that returns: in `g(f()?, h()?)`, `h`
  is not called when `f` fails, and a value already made for an earlier
  operand, a list for an `own` parameter, is freed. In `main`, or in any
  function whose last result is not an error, `?` is `cannot use ? in
  main, which returns no error`, or `... whose last result is int64, not
  error`; in deferred code `cannot return from a defer with ?`; in a
  package-level variable's value `cannot use ? outside a function`. A
  `?` statement discards what it gives under a call statement's rules: a
  copyable value is discarded, a list is `numbers()? gives list[int64],
  which cannot be discarded; bind it to a variable`. A list a `?` gives is
  a temporary like a call's: it may be moved into a variable, a field or
  an `own` parameter, but not lent to a plain one, `cannot use a
  temporary list[int64] here; store it in a variable first`. Every fact
  the function's contract ensures must hold at each `?`, as at a return,
  `f ensures n > 0, which may not hold at this ?`.

  The rules are the compiler's: an error is a plain value at run time,
  in C a `tack_error` passed by value (see extern C).
- Lists. `list[T]` is an owned, growable list of elements of type `T`,
  stored by value, freed automatically: `list` is a keyword, as `map`
  is. `make(list[T], alloc)` makes an empty one, which allocates nothing
  and cannot fail, and grows through `alloc`, an `Allocator` (see
  allocators). `err := append(mut l, v)` adds `v`, of a type that is or
  widens to `T`, at the end, growing the buffer when it is full. append
  is variadic, `append(mut l list[T], elems ...T)`, under the variadic
  rules exactly (see variadics): it takes any number of values, `err :=
  append(mut l, a, b, c)`, each used as a `T`, an untyped constant taking
  `T`, or none, `append(mut l)`, which does nothing; or a view spread
  whole, `err := append(mut l, v...)`, of what a `[]T` parameter takes, a
  view, an array place, a slice, another list, or `[]uint8(s)` for a
  `list[uint8]`, never a string itself, `cannot use string as []uint8 in
  argument to append; view its bytes with []uint8(s)`, and never beside
  values, `cannot use ... in a call with other values for elems`. It
  grows the buffer once, to the final length, before writing anything,
  so it adds all of them or none: when the allocator cannot grow it, or
  the length would overflow an `int64`, the error is `error{code: 1,
  msg: "out of memory"}` and `l` is left as it was, its length, its
  capacity and its elements. Spreading `l` into its own append, `append(mut
  l, l...)` or `append(mut l, l[1:]...)`, is the borrow rule below,
  `cannot pass l alongside mut l in this call: ...`, as it is through a
  struct's field. `err := reserve(mut l, n)` makes room for `n` more
  elements, an `int64`, so the capacity is at least `len(l) + n`, without
  changing the length or the elements, growing as an append does, to at
  least double the capacity, so reserving one element before each append
  still appends in amortized constant time; `n` of 0 does nothing. A
  negative constant `n` is `reserve count must not be negative, got -1`;
  a negative `n` at run time is the error `error{code: 2, msg: "negative
  count"}`, and one the allocator cannot make room for, or that
  overflows the length, `error{code: 1, msg: "out of memory"}`, each
  leaving `l` as it was. Tack has no way to read the capacity. The
  errors of both follow the three error rules: `append(mut l, 1)` alone
  is `the error append returns cannot be discarded; bind it, as in err :=
  append(...)`, and `reserve(mut l, 1)` the same for reserve. The list is
  passed as `mut`, so it must be a mutable local, a `mut` parameter or a
  field of one: `append(l, 1)` is `append takes l as mut; pass mut l`,
  and anything but a list `append expects a list, got [3]int64`, and the
  same for reserve, which takes an arena too, `reserve expects a list or
  an arena, got [3]int64`. `make`
  takes the list type as written, `make takes a list, a map, an arena or
  a pool type, as in make(list[int64], c_allocator)`, and no capacity yet, `make expects 2
  argument(s), got 3`; a list type anywhere else a value is wanted is
  `cannot use list[int64] as a value; it is a type`, and
  `list[int64]{1, 2}` is `list[int64] cannot be written as a literal;
  only arrays and structs can`. A list reads as a view does: `len(l)`, an
  `int64` with the facts a string variable's length has (see indexing a
  string), which hold up to the first write to `l` that can reach the use,
  assigning it, passing it as `mut`, `append`, `reserve`, `pop` and
  `remove` included, or moving it, though writing an element ends none,
  and which a successful append adds to: after `err := append(mut l, a,
  b)` and a check proving `err.code == 0`, `len(l)` is at least 2, the
  number of values passed or the length of an array spread whole, until a
  write to `l` that can reach the use (see docs/safety.md, lists); a
  length proven not zero proves `len(l)` as a divisor too; `l[i]` with
  `i` proven in range against `len(l)`, or the checked form `v, ok := l[i]`; every range form, `for i, x := range l`, whose
  index is proven for `l`; slicing, `l[i:j]`, to a view, `[]T`; and
  passing it whole to a view parameter of its element type, `sum(l)`, or
  spreading it into a variadic one. Unlike a view it can be written: `l[i]
  = v`, `l[i].f = v`, `l[i] += v` and `l[i]++`, with `i` proven, in a
  list that is mutable, a `mut` local or a `mut` parameter, as an array's
  element is. A range over a list walks the list itself, not a copy, so
  element writes in the body show in later iterations, and the body
  cannot change the list: assigning it or anything holding it, passing
  either as `mut`, `append`, `reserve`, `pop` and `remove` included, or moving it is `l is being ranged
  over, so this loop cannot pass mut l`. Two lists of one type compare
  with `==` and `!=` when their element type does: equal when they have
  the same length and each element equals the one at the same index,
  compared as `==` compares the element type, whatever their capacities,
  so two empty lists are equal and a proper prefix is not. A list of
  lists, of strings, of arrays or of structs compares element by
  element, each through its own type's `==`; a list of elements without
  `==` has none, `operator == is not defined on list[Pool]` for a struct
  `Pool` holding an `Allocator`, and a view never has one. Comparing reads
  both lists where they are, copying nothing and allocating nothing, so
  each operand must be a variable or a place in one, `cannot use a
  temporary list[uint8] here; store it in a variable first`, and neither
  may be passed as `mut` in the other operand: `a == lists[first(mut
  a)]` is `cannot compare a alongside mut a in the other operand: a list
  is not copied to be compared, and a mut argument must be the only way
  to reach a during its call`. A switch cannot take a value holding a
  list as its tag, since it holds its tag while it evaluates each case:
  `switch l` is `cannot switch on list[int64], which holds a list;
  compare it with == in an if instead`. Lists have no order and no zero
  value: `mut l list[int64]` is `l needs a value: list[int64] has no zero
  value`. No package-level variable may hold a list,
  `package-level variable g cannot hold a list`, since any function could
  reach one.

  `x, ok := pop(mut l)` takes the last element out of `l` and gives it,
  and `x, ok := remove(mut l, i)` takes element `i`, an `int64`, moving
  every element after it down one place, so the rest keep their order,
  in time proportional to how many follow. Neither allocates or copies
  anything, so neither returns an error: the element moves out, and
  `ok` says whether there was one, `false` for an empty list or an `i` out
  of range, which leaves `l` as it was. `x` is then the element type's
  zero value, `0` from a `list[int64]`; an element type with none, a
  list, a union, an `Allocator` or anything holding one, has nothing to
  give, so
  `x` holds nothing to free and cannot be read until `ok` is checked
  true, as a result bound beside an error cannot be read before the
  error is checked: `x cannot be read before ok is checked; check that ok
  is true first` wherever the facts in force do not prove `ok`. `if ok`
  proves it in its branch, `if !ok { return }` for the rest of the block,
  and `ok &&` on its right, a fact like any other, which holds only in a
  region that never assigns `ok`. So that a check is about the `ok` that
  came with `x`, both are local variables, `s.x cannot take the element
  pop gives beside its ok; bind it to a local variable`, `ok` is assigned
  only by a binding that assigns `x` too, `ok says whether pop gave x, so
  only a binding that also assigns x can assign it`, `mut ok` included,
  and it is never `_`, `the ok pop returns cannot be discarded: x cannot be
  read until it is checked; bind it to a variable instead of _`. An owned
  element bound to `_` would never be freed, `pop returns list[int64],
  which cannot be discarded; bind it to a variable instead of _`; a copyable one
  may be discarded. The plain forms, `x := pop(mut l)` and `x := remove(mut
  l, i)`, are allowed only where `len(l) > 0`, or `i` in range for `l`,
  is proven, as an index is: `pop may find l empty; check len(l) > 0
  first, or use x, ok := pop(mut l)`, `index i may be out of range; check
  0 <= i && i < len(l) first, or use x, ok := remove(mut l, i)`. The
  call passes `l` as `mut`, which ends every fact about `len(l)` for what
  runs after it, but not for the call itself, which checks before it
  writes, so `if len(l) > 0 { x := pop(mut l) }` compiles. Written as a
  statement, `pop(mut l)` and `remove(mut l, i)` need no proof: they discard
  the element when there is one, freeing it when it is owned, and leave
  `l` as it was otherwise; this is the way to discard an element, since
  `_, _ = pop(mut l)` is `every name is _, so nothing is bound; make the
  call on its own`. A negative constant `i` is
  `index -1 is out of range for a list`, in every form; three
  names are `assignment mismatch: 3 variables but pop gives 2 values`.

  `swap(mut l, i, j)` swaps elements `i` and `j` of `l` in place, an
  `i` equal to `j` leaving it as it was; it moves neither out, copies
  neither and frees nothing, so it reorders a list of any element type,
  owned ones and a type parameter's included. Both indexes need proofs,
  `index j may be out of range; check 0 <= j && j < len(l) first`, as
  `l[i]` does, and there is no checked form; it changes no length, so a
  proof about `len(l)` holds across it, though it passes `l` as `mut`, so
  not inside a range over `l`, `l is being ranged over, so this loop
  cannot pass mut l`. It gives no value, `swap returns no value`, so it
  is only written as a statement. A function named `swap` is `duplicate
  declaration of builtin function`. In C it holds the list's address and
  both indexes in temporaries, then exchanges the two elements through a
  third.

  `old := replace(mut place, new)` moves the value of `place` out, as its
  result, and moves `new` in, in one step, so the place never holds no
  value. It is how a list kept in a struct is worked on as a local, `mut
  frames := replace(mut s.frames, make(list[int64], c_allocator))`, and
  put back, `empty := replace(mut s.frames, frames)`, which no move can
  do, since a field cannot be moved on its own; it replaces a list's
  element, owned or not, in place too. `place` is anything a `mut`
  argument can name: a mutable local, a `mut` parameter, a field of one,
  or an element with its index proven, and not an immutable variable, a
  union pattern's binding, a view's element or a string's byte. `new` is
  used as the place's type under assignment's rules, `cannot use string
  as int64 in argument to replace`, and moved in when owned. The
  arguments are evaluated left to right, as any call's: the place, with
  its indexes, first, then `new`, and only then is the old value read and
  `new` stored, so `replace(mut x, x * 2)` doubles `x` and gives its old
  value; an owned `new` that may overlap the place, `replace(mut l, l)`,
  is `cannot pass l alongside mut l in this call: ...`. Like any `mut`
  argument it ends the facts about the place for what runs after the
  call. A result holding a list must be bound, as any must; written as a
  statement, `replace(mut place, new)` is an assignment, which frees the
  old value. In C it is a comma expression over temporaries: the place's
  address, the new value, the old one read, the new one stored.

  A list may hold owned elements: lists, `list[list[uint8]]`, structs
  or unions holding lists, nested to any depth. An element of an owned
  type is never copied out of its list: it is a place inside the list,
  as a struct's list field is. `f(l[i])` borrows it, `f(mut l[i])` passes
  it, `append(mut l[i], x)` and `append(mut l[i].items, x)` grow it, and
  `l[i].m()` and `mut l[i].m()` call methods on it, all in place; `len(l[i])`,
  `l[i][j]`, `l[i].f`, `for _, b := range l[i]`, `switch l[i] { ... }`
  and passing it to a view parameter, `fmt.write(l[i])` for a
  `list[list[uint8]]`, read it where it is. It never leaves the list but
  through `pop` or `remove`: declaring a variable with it, assigning it,
  returning it, writing it as a struct literal's field, a union's payload
  or an appended value is `cannot move l[i] out of l: an element cannot
  be moved out of a list; take it out with pop or remove`, and the
  checked form, which would copy it, is `the checked form cannot read an
  element of type list[int64]: it would copy it out of l; check 0 <= i &&
  i < len(l) first and use l[i] in place`. `l[i] = v` moves `v` in once
  it is computed, then frees the element it replaces; `append` moves each
  value it is given into the list, `append(mut l, t)` leaving `t`
  unusable as `m := t` would, and when the buffer cannot grow it frees
  them, so nothing is lost; spreading a list or a view of owned elements
  into one, `append(mut l, other...)`, would copy them, `cannot spread
  other into append: its elements would be copied, and a list cannot
  be`. A range binding of an owned element, `for _, x := range l`,
  refers to it in place, as a union's binding does: `x` is read-only, is
  never moved, `cannot move x: it refers to an element of l in place, and
  an element cannot be moved out of a list`, and while it is bound the
  body changes no element of `l` either, since any may be the one it
  refers to, `l is being ranged over, and x refers into it, so this loop
  cannot assign l[i]`. Those are the only names that refer into a list.
  A view, `[]list[int64]`, holds owned elements under the same rules, and
  takes such a list, or a slice of one; an array's or a variadic's
  elements are copied, and stay copyable: `an array cannot hold
  list[int64]: its elements are copied, and a list cannot be`, and the
  same for a variadic parameter. Freeing a list frees each element it
  holds first, last first, as a struct's fields are, then its buffer, so
  reassigning a list frees every old element, and an element popped or
  removed is its new owner's to free. A type cannot hold itself through
  a list, `invalid recursive type Tree: a type cannot hold itself, even
  through a list`, so a tree is still a list of nodes naming each other
  by index. A list or a view whose elements hold lists cannot cross to C
  yet (see extern C).

  A list has one owner, the variable holding it, which frees it when its
  block ends, through the allocator it was made with. So a list is never
  copied: declaring a variable with one, `m := l`, assigning one, `m =
  l`, returning one or writing one as a struct literal's field moves it,
  and the variable it came from cannot be used again, `l is used after it
  was moved`, with a note at the move, until it is assigned a new list. A
  move on some paths only is allowed, and the variable is then unusable
  after them, `l is used after it may have been moved`; a move in a
  loop's body that the next iteration would see again is reported in the
  same way, unless the body assigns the variable a new list first. A
  plain parameter of a list type borrows its argument, read-only, and the
  caller keeps it; a `mut` parameter can grow it, and assign it whole.
  Neither can be moved, `cannot move l: it is a parameter, which borrows
  its caller's value`, nor can a field on its own, `cannot move s.items
  out of s: a field cannot be moved on its own`; `replace` takes one out
  with a value left in its place (see replace). A list no variable holds,
  a call's result or `make`'s, must be given an owner: anywhere but the
  value of a declaration, an assignment, a return, a binding of several
  results or a struct literal's field it is `cannot use a temporary
  list[int64] here; store it in a variable first`, or `cannot view a
  temporary list; store it in a variable first` for a view, and a call
  that discards one is `make returns list[int64], which cannot be discarded;
  bind it to a variable`, or with `instead of _` where a binding puts `_`.
  Assigning a variable that holds a list frees the old list once the new
  value is computed, which may read it. A list is freed where its block
  ends however it is left, by falling off its end, `return`, `break` or
  `continue`, each list declared in a loop's body at the end of every
  iteration, a list a three-clause loop's init declares once the loop is
  done, and a list that was moved, or a conditionally moved one on the
  paths that moved it, not at all. The frees run as if a defer freeing
  the list were written right after its declaration: frees and defers
  run last first, by position, so a defer written after `l` still reads
  it. A defer that reads a list moved later in its block would read a
  moved value: `l is moved after this defer, which reads it when its
  block ends`; deferred code cannot move a list declared outside it, `a
  defer cannot move l, which is declared outside it`; and `exit` runs no
  defer and frees no list itself, but before the process ends every
  allocator frees every buffer it still holds, so nothing outlives the
  program. During a call,
  an argument that reads a list in place, a list or a struct holding one
  passed to a plain parameter, or a list or a slice of one passed to a
  view parameter, cannot overlap a place the same call, or a call nested
  in its arguments, passes as `mut`, an element of a list as much as the
  list itself, since a list is not copied for a
  call and growing it could free what the other argument reads: `cannot
  pass l alongside mut l in this call: a list is not copied for a call,
  and a mut argument must be the only way to reach l during it`. Places
  compare as they do for two `mut` arguments, but two elements of one
  list always may be one, so `f(l[0], mut l[1])` is `cannot pass l[0]
  alongside mut l[1] in this call: ...`, whatever the indexes. An index
  reads its list in place in the same way, after the index is
  evaluated, so an index, checked or not, whose own expression passes as
  `mut`, at any depth, a place that may overlap the list it reads is
  `cannot index l alongside mut l in its index: a list is not copied to
  be indexed, and a mut argument must be the only way to reach l during
  its call`, `l[set(mut l[0])]` included, where an array's index reads
  the array as it was before; a slice's bounds the same, `cannot slice
  l alongside mut l in its bounds: a list is not copied to be sliced,
  ...`. An element of a list is assigned in place, so a value assigned
  to one cannot pass the list, or anything holding it, as `mut`: `l[0] =
  grow(mut l)` is `cannot assign to l[0] alongside mut l in its value: an
  element is assigned in place in its list, and a mut argument must be
  the only way to reach l during its call`, while `ll[0] = f(grow(mut
  ll[0]))`, a `mut` argument inside the list, is allowed.

  A struct with a field of a list type, directly or through another
  struct or a union, holds a list, and is owned as a list is: it moves, has no zero
  value, compares with `==` as a list does when every field does,
  cannot be a package-level variable, and frees its
  lists, last field first, when its owner's block ends. So does a union
  with a case whose payload holds a list, which frees what the case it
  holds holds (see unions). In C a list is
  `tack_list_<T>`, `{T *ptr; int64_t len; int64_t cap; tack_allocator
  alloc;}` (see extern C); one whose elements are owned is freed by a
  `tack_free_` function of its own, which frees each element it holds,
  last first, then its buffer; `pop` and `remove` are std's
  `TACK_LIST_POP` and `TACK_LIST_REMOVE`, the checked forms and the
  statements testing the length first, a statement then freeing an owned
  element it discards; and a range binding of an owned element is a pointer to
  it. [docs/safety.md](docs/safety.md#lists) has every rule and
  diagnostic.
- Maps. `map[K]V` holds values of type `V` by keys of type `K`, owned,
  as a list's elements are, in the order each key was first put: `map`
  is a keyword. `make(map[string]int64, c_allocator)` makes an empty one,
  which allocates nothing, and `len(m)` is how many keys it holds, never
  a known value and carrying no facts. `K` is any type `==` compares, an
  integer, a `bool`, a `string`, or an array, a struct, a union or a list
  of them, `text.Text` included, and keys are compared, and hashed, by
  their contents; not `error`, `cannot use error as a map key: an error
  is not a key`, `any`, an `Allocator`, a map, `cannot use
  map[int64]int64 as a map key: it has no ==`, or a type parameter. `V`
  is anything a list's element can be, owned included, but a type
  parameter. `err := put(mut m, k, v)` puts `v` under `k`: in place of
  the value `k` has, which it frees, keeping `k`'s position and the key
  `m` holds, the `k` given being freed; or at the end of the order when
  `k` is new, growing `m`'s buffers through its allocator. A put that
  cannot grow them returns `error{code: 1, msg: "out of memory"}`, leaves
  `m` as it was and frees `k` and `v`, as `append` frees what it cannot
  add; its error cannot be discarded, and `k` and `v` are moved in. `v, ok
  := m[k]` copies the value under `k`, or gives `V`'s zero value and
  `false` when `k` is not in `m`; it cannot read an owned value, which it
  would copy, `the checked form cannot read a value of type Bag: it would
  copy it out of m; check has(m, 1) first and use m[1] in place`, nor one
  of a type with no zero value. `has(m, k)` reports whether `k` is in
  `m`, and tested as a condition it proves so where the condition holds,
  as `i < len(l)` proves an index: `m[k]` is then a place, as a proven
  `l[i]` is, read in place, its fields read, passed as it is or as `mut`,
  appended to, given to a `mut` method or to `replace`, or assigned, `m[k]
  = v`, which frees the value it held. The proof ends at any write to `m`
  that can reach the use, a `put`, a `delete` of any key, an assignment or
  a `mut` argument, but not at one to a value `m` holds, which changes no
  key; `m` must be a variable or a parameter, or a field or element of one
  declared without `mut`, and `k` a constant, or a variable or parameter
  declared without `mut`, or a field of one, `has proves only a key that
  cannot change: a constant, a variable or parameter declared without mut,
  or a field of one`. A value cannot be moved out, `cannot move m[k] out
  of m: a value cannot be moved out of a map; take it out with delete`.
  `m[k]` unproven is `key k may not be in m; check has(m, k) first, or use
  v, ok := m[k]`, and assigning it `key k may not be in m; check has(m, k)
  first, or use put(mut m, k, ...)`. `v, ok := delete(mut m, k)` moves the value under
  `k` out, freeing the key `m` held; when `k` is not in `m`, `m` is left
  as it was and `v` is `V`'s zero value, or, for a type with none, cannot
  be read until `ok` is checked true, as `pop`'s `x`; written as a
  statement, `delete(mut m, k)` frees the value it takes, and bound to
  one name it is `delete gives the value and whether the key was there;
  use v, ok := delete(mut m, k)`. `for k, v := range m`, `for k := range
  m` and `for _, v := range m` walk the keys in insertion order, a key put
  again after a delete coming last. `k` is never assigned; an owned key
  or value is bound in place, any other copied, and the loop cannot
  change `m`, `m is being ranged over, so this loop cannot pass mut m`. A
  map has no zero value, no `==`, `operator == is not defined on
  map[int64]int64`, and no literal; it moves as a list does, nests in
  lists, structs, unions and other maps, and is freed with its keys and
  values when its owner's block ends. No package-level variable may hold
  one, `package-level variable g cannot hold a map`, no type may hold
  itself through one, and none crosses to C, `parameter m of extern func
  f cannot be map[int64]int64: a map does not cross to C`. In C a map is
  a struct of its own per map type: a buffer of entries in insertion
  order, each with its key, value, hash and whether it is live, and a
  power-of-two table of slots probed linearly from a key's hash; a
  delete marks its entry and slot deleted, which the next growth, all or
  nothing, compacts away. Keys hash deterministically, field by field and
  element by element.
- Arenas. `arena[T]` is an owned, append-only store of elements of type
  `T`: `arena` is a keyword, as `list` is, and `id` and `span` are
  builtin type names, reserved as `int64` is, so no variable, parameter,
  function, type or union case may take one, `cannot use span as a
  name; it is a type`, though a field or a method may, as it may be
  named like any builtin, and `id` alone is `id needs its element type, as
  in id[int64]`. `make(arena[T], sentinel, alloc)` makes one holding only
  `sentinel`, moved in as its element at id 0; it allocates nothing, so
  it cannot fail, and without the sentinel it is `make expects 3
  argument(s), got 2`. `e, err := add(mut a, v)` moves `v` in as the last
  element and gives its `id[T]`; `s, err := add_all(mut a, x, y)` or
  `add_all(mut a, l...)` adds its values in order, all or none, and gives
  their `span[T]`, the empty span for none. Each fails with
  `error{code: 1, msg: "out of memory"}` when the allocator cannot grow
  the arena or it would hold more than 2^32 - 1 elements, its sentinel
  among them, leaving it as it was and freeing what it was given; the
  error cannot be discarded, `the error add returns cannot be discarded;
  bind it, as in _, err := add(...)`, the id or span cannot be read
  before it is checked, and `add` used as one value is `add returns 2
  values`. `reserve(mut a, n)` makes room for `n` more, as a list's does,
  failing too past the limit. `a[e]` is the element `e` names, with no
  proof: a place, read in place, its fields read and written, assigned,
  which frees the element it replaces, passed as `mut` or to `replace`,
  but never moved out, `cannot move a[e] out of a: an element cannot be
  moved out of an arena`, nor dropped, nor read with the checked form,
  `the checked form cannot read an element of an arena: every id names
  one, the sentinel for an id the arena did not give; use a[e]`. An id at
  or past `len(a)`, which may come from another arena of the same `T`,
  and the zero id, name the sentinel: a logic error the program cannot
  detect, never a read or write of memory the arena does not own. `a[s]`
  is a read-only view of the span's elements, under every rule of a view
  a call returns, passed to a view parameter, given to `len`, indexed,
  sliced, ranged over or returned from a function that takes `a` as a
  plain parameter, `a[s] is a view, []int64, which can only be passed to
  a view parameter, given to len, indexed, sliced, ranged over or
  returned` anywhere else; a span not wholly in `a` is an empty view.
  `len(a)` counts the sentinel, and `for e, v := range a` gives every id,
  the sentinel's first, with its element, bound in place when it is
  owned; since the sentinel is inline, a view of an array reached
  through such a binding is returned only when the arena is itself an
  element of a list or a view, `cannot return a view of x.arr: the
  function holds its own copy of a, ...` for a plain arena parameter. An id and a span are copyable, 32-bit each, the span a start and
  a length, with `==` and `!=`, a zero value and a place as a map's key,
  but no order, `operator < is not defined on id[int64]`, no arithmetic,
  and no conversion to or from an integer, `cannot convert int64 to
  id[int64]`; an `int64` indexes no arena, `cannot index arena[int64]
  with int64; index it with an id[int64]`, nor does an id or a span of
  another element type, `cannot index arena[int64] with id[string]`. A
  type may hold an id or a span of itself, as an AST's nodes do, but not
  an arena of itself, `invalid recursive type Node: a type cannot hold
  itself, even through an arena`. An arena moves as a list does, nests in
  lists, maps, structs, unions, arenas and generic code as `arena[T]`, is
  borrowed by a plain parameter and grown through a `mut` one, and is
  freed with its owner, its elements last first, then its sentinel, at
  its block's end, a `drop`, a `?`, `exit` or an abort. Since `add` may
  move its buffer, nothing may read it in place while it grows: `a is
  being ranged over, so this loop cannot pass mut a`, over the arena or a
  span's view of it, and `cannot pass a[s] alongside mut a in this call:
  an arena is not copied for a call, and a mut argument must be the only
  way to reach a during it`, an element in place or a closure capturing
  `a` alike. A fact checked of an element, `a[e] != 0`, holds only while
  neither the arena nor the id can change, as for an array's element. An
  arena has no zero value, no `==`, no literal, no `pop`, `remove`,
  `swap`, `delete` or slice, `swap expects a list, got arena[int64]`, is
  no global, `package-level variable g cannot hold an arena`, no `any`,
  and neither it nor an id or a span crosses to C, `parameter a of extern
  func take cannot be arena[int64]: an arena does not cross to C`. `add`
  and `add_all` are builtins with no value, as every builtin is. In C an
  arena is a struct of its own per element type, a list of its elements
  but the sentinel and the sentinel inline; an id is a `uint32_t`, a span
  a `uint64_t`, its start in the low half and its length in the high,
  and every lookup clamps in 64 bits to the list or the sentinel, so an
  arena before its first `add` reads no buffer at all.
- Pools. `pool[T]` is an owned store of elements of type `T`, any one of
  which can be removed on its own: `pool` is a keyword, as `arena` is,
  and `handle` is a builtin type name, reserved as `id` is, `cannot use
  handle as a name; it is a type`, `handle needs its element type, as in
  handle[int64]`. `make(pool[T], alloc)` makes an empty one, which
  allocates nothing and cannot fail, however many pools were made before
  it. `h, err := insert(mut p, v)` moves `v` in and gives its
  `handle[T]`, taking a slot an earlier remove freed when there is one
  and growing the pool otherwise; it fails with `error{code: 1, msg: "out
  of memory"}` when the allocator cannot grow the pool or it would hold
  more than 2^32 - 1 slots, leaving it as it was and freeing `v`. The
  error cannot be discarded, `the error insert returns cannot be
  discarded; bind it, as in h, err := insert(...)`, the handle cannot be
  read before it is checked, `h cannot be read before err is checked;
  check err.code == 0 first`, and `insert` used as one value is `insert
  returns 2 values`. `has(p, h)` reports whether `h` names a live element
  of `p`, and tested as a condition proves it, as `has(m, k)` proves a
  map's key, with the same conditions on `p` and `h`; `p[h]` is then a
  place, read in place, its fields read and written, assigned, which
  frees the element it replaces, passed as `mut`, appended to or given to
  `replace`, never moved out, `cannot move p[h] out of p: an element
  cannot be moved out of a pool; take it out with remove`, nor dropped.
  The proof is the only one there is, and it ends at any `insert`, any
  `remove`, an assignment of `p`, a `mut` argument of it, a closure that
  writes it passed to a call, and a loop's back edge that reaches one,
  but not at a write to an element; a fact about a mutable pool does not
  enter a closure, one about an immutable pool does. Unproven, `p[h]` is
  `handle h may not be live in p; check has(p, h) first, or use v, ok :=
  p[h]`, for any element type, and assigned, `handle h may not be live
  in p; check has(p, h) first`. `v, ok := p[h]` copies the element, or
  gives the zero value and `false`, so it reads only a copyable element
  with a zero value, `the checked form cannot read an element of type
  list[int64]: it would copy it out of p; check has(p, h) first and use
  p[h] in place`, a type parameter's included. `v, ok := remove(mut p,
  h)` moves the element out, or gives the zero value, or nothing to read
  for a type with none, and `false`; written as a statement, `remove(mut
  p, h)` frees the element at once; neither needs a proof, a handle that
  names nothing removing nothing, and there is no one-name form, `remove
  gives the element and whether the handle was live; use v, ok :=
  remove(mut p, h)`. `len(p)` counts the live elements, and `for h, v :=
  range p` gives every live one's handle and element, bound in place
  when it is owned, the pool locked, `p is being ranged over, so this
  loop cannot pass mut p`; where `has(p, h)` could prove the handle, `p`
  a variable or a parameter, or a field or an element of one declared
  without `mut`, the body knows it for its handle, as a list range's body
  knows its index in range, so `p[h]` needs no check there, a closure
  over a mutable pool aside; a range over `w.p` with `w` a `mut`
  parameter gives no such fact. A removed slot's 32-bit generation moves on,
  so a handle to the element is stale from then on, even once the slot
  holds another, and a slot whose generation would wrap is retired,
  never used again, checked before the increment; each pool has a 32-bit
  tag, from a counter that starts at 1 and stops at its last value, so a
  handle from another pool, or the zero handle, names nothing either,
  except that past 2^32 - 2 pools made, later pools share the last tag
  and may accept each other's handles: a logic limit, never a read of
  memory a pool does not hold.
  A stale, foreign or zero handle is a value every lookup reports, never
  a read or write of memory: the pool never gives a slot back while it
  lives. A handle is copyable, three 32-bit parts, with `==` and `!=`, a
  zero value, a map's key and a switch's tag, but no order, `operator <
  is not defined on handle[int64]`, no arithmetic, and no conversion,
  `cannot convert int64 to handle[int64]`; a type may hold a handle of
  itself, but not a pool of itself, `invalid recursive type Node: a type
  cannot hold itself, even through a pool`. A pool moves as a list does,
  nests in lists, maps, structs, unions, arenas, pools and generic code as
  `pool[T]`, is borrowed by a plain parameter and changed through a `mut`
  one, `cannot pass p[h] alongside mut p in this call: a pool is not
  copied for a call, and a mut argument must be the only way to reach p
  during it`, and is freed with its owner, its live elements last first,
  then its slots, at its block's end, a `drop`, a `?` or `exit`. It has
  no zero value, no `==`, no literal, no `pop`, `swap`, `delete`,
  `reserve` or slice, is no global, `package-level variable g cannot hold
  a pool`, no `any`, and neither it nor a handle crosses to C, `parameter
  p of extern func f cannot be pool[int64]: a pool does not cross to C`,
  `... a handle does not cross to C`. `insert` is a builtin with no value,
  as every builtin is, and a pool has no methods, `pool[int64] has no
  method insert`. In C a pool is a struct of its own per element type, a
  buffer of slots, each its element, its generation, the next free slot
  and whether it is live, laid out first as a list's buffer is, then the
  count of live elements, the first free slot and the tag; a handle is
  the runtime's `tack_handle_`, compared with `tack_handle_eq_`; every
  lookup but a proven one compares the tag, the slot against the slots'
  count, liveness and the generation before it reads the slot, and a
  proven one reads it directly.
- Allocators. `Allocator` is a builtin type: an opaque handle on an
  allocator, which a list holds, grows through and is freed through. It
  is copyable, a local, a parameter, a result, a field, an array's or a
  list's element or a global, but has no zero value, `a needs a value:
  Allocator has no zero value`, so a struct or array holding one has none
  either, and the checked form cannot read an element of one, `the
  checked form cannot read an element of type Allocator: it has no zero
  value to give when the index is out of range`, while the one popped or
  removed by a checked `pop` or `remove` cannot be read until its `ok`
  is checked (see lists); it has no `==` and no
  order, and cannot be passed as `any`. `c_allocator`, a keyword like
  `noerror`, lowers to C's `malloc`, `realloc` and `free`, and is a
  constant expression, so `var a = c_allocator` is a global; the only
  other allocators are regions', which are second class (see regions).
  In C an `Allocator` is a `tack_allocator`, 8 bytes, the 16-bit index of
  an allocator std's C keeps and its 48-bit generation, and `c_allocator`
  is `TACK_C_ALLOCATOR`, index 0 and generation 0.
- Regions. `region r { ... }` opens an allocator, `r`, that lives for its
  block: `region` is a keyword, a statement of its own, `region is a
  keyword and cannot be a name; a region is written region r { ... }`,
  and `r` an immutable `Allocator` declared at the block's start, which
  may be `_` and is `declared and not used: r` when never read. What is
  made through `r`, by `make(list[T], r)` or a function given `r`, takes
  its memory from chunks of at least 64 KiB the region takes from
  `c_allocator`, bumping through the newest; the last buffer handed out
  grows in place, its chunk with it when it is the chunk's only one, and
  freeing it hands its bytes out again, while freeing any other does
  nothing until the region ends. The block is left as any block is, by
  falling off its end, `return`, `?`, `break` or `continue`, its defers
  and frees run, and then the region ends, giving every chunk back in one
  step; `exit` and an `unreachable` reached run no region's end, and
  `c_allocator`'s release frees the chunks as its own buffers. A region
  terminates when its body does, so `func f() int64 { region r { return
  1 } }` needs no return after it, and no `fallthrough` leaves one: one
  inside a region in a case is not its case's last statement,
  `fallthrough must be the last statement of a case`, while a switch
  written wholly inside a region falls through between its own cases as
  anywhere. Opening a region takes no chunk and cannot fail, though it
  may grow the region table through `c_allocator`, and an allocation
  through it that `c_allocator` refuses a chunk for fails with
  `error{code: 1, msg: "out of memory"}`, the list unchanged. The open
  regions are a stack, the region table, which starts with room for 16
  and doubles through `c_allocator`, made when the first region opens
  and freed when the last one ends. A region's handle is its depth plus 1
  in 16 bits and a generation of its own in 48, from one counter, and
  both saturate: a region opened when the table cannot grow, 65534 or
  more deep, or once the counter has reached its last value, so no
  generation is ever taken twice, gets the dead handle, which matches no
  allocator, so every allocation through it fails with code 1 and its end
  only counts it ended. `r` is second class, as a
  scope parameter of a function type is: it is only given to `make`,
  passed to a parameter written `scope a Allocator`, of a function, a
  method or a function type, `func(scope Allocator) int64`, or captured
  by a closure, which goes to a scope parameter itself; anywhere else,
  `a := r`, `g = r`, `return r`, `Holder{a: r}`, `[1]Allocator{r}`,
  `append(mut as, r)`, it is `r is a region's allocator, which can only
  be given to make or passed to a scope Allocator parameter`, with a note
  at the region, `region r is opened here, and everything made through r
  is given back when its block ends`; to a plain parameter, `take does
  not take a as scope, and a region's allocator can only be passed to a
  scope parameter`, and to an extern func's, `r is a region's allocator,
  which never crosses to C, and take is an extern func`. A scope
  `Allocator` parameter is under the same rule in its function, `a is a
  scope Allocator parameter, which can only be given to make or passed to
  another scope parameter`, takes any `Allocator`, `c_allocator`
  included, and is never an extern func's, `parameter a of extern func
  take cannot be scope: a region's allocator never crosses to C, and
  c_allocator crosses as a plain Allocator; remove scope`. std's
  functions that only allocate through the allocator they are given take
  it as scope: `text.new`, `text.from_bytes`, `errors.from_bytes`,
  `bytes.repeat`, `path.base`, `path.dir`, `path.clean`, `path.join`,
  `os.args`, `os.env`, `os.read_file`, `os.list_dir`, `os.make_temp_dir`
  and `os.run`. A value whose type owns memory is from `r` when it was
  made inside the block by an expression given `r`, by a call with `r` or
  a value from `r` among its operands, a closure capturing either among
  them, or is a variable such a value was declared with, assigned to,
  moved, appended, put, added or inserted into, or passed as `mut` to a
  call alongside; and it may not reach anything that outlives the block:
  `cannot return a value of type list[int64] holding memory from region
  r: the region gives its memory back when its block ends`, from a
  closure `... from a closure: a closure's results count as outside every
  region`, `cannot assign a value of type list[int64] holding memory from
  region r to outer: outer is declared outside the block of region r`,
  `cannot append ... to outer: ...`, `cannot put ... in m: ...`, `cannot
  add ... to a: ...`, `cannot replace the value of h.l with ...: h.l is
  part of h, which is declared outside the block of region r`, a `mut`
  parameter, a closure's included, `out is a mut parameter, which lives
  outside every region`, and a call taking one, or `r`, alongside a `mut`
  argument that owns memory declared outside, or a closure writing such
  a variable, `cannot pass mut outer to fill alongside r, the allocator
  of region r: the call could keep memory from region r in outer, and
  outer is declared outside the block of region r`, `cannot pass a
  closure that writes keep to run alongside ...`, each with the note.
  A scope parameter of a function type is the caller's closure, which
  may write anything it captures, so a call through one, or one passing
  it on, by name or captured in a closure, takes neither `r` nor a value
  from `r`, in any position: `cannot pass a value of type list[int64]
  holding memory from region r to g, a scope parameter: the closure
  behind it could keep memory from region r in anything it captures`,
  or `... to h alongside g, a scope parameter: ...`; values not from the
  region go to it freely.
  So `os.run(argv, mut out, mut err_out, r)` with lists declared outside
  the region is rejected, as any call given `r` beside an outer `mut`
  list is. Integers, bools, strings, ids, spans and handles computed
  from a region's values leave it freely, as does a list made through
  `c_allocator` inside it, and a closure that captures `r` may write the
  region's own variables. Regions nest: an inner region's values may not
  reach the outer region's variables declared outside it, `cannot assign
  a value of type list[int64] holding memory from region s to kept: kept
  is declared outside the block of region s`, and the outer one's are
  used freely inside it. The rule is checked within one function and is
  conservative: a variable ever given a value from `r` counts as from
  `r` throughout its scope, and an operand of a call counts whether or
  not the callee keeps it, but for one passed as a view, to a view
  parameter or spread, `append(mut out, l...)`, which owns nothing, so a
  region's elements are copied out that way. In C, `region r` is a block that opens with
  `const tack_allocator r = tack_region_push_();` and whose cleanup chain
  starts with `tack_region_pop_(r);`, so it runs after every other.
- Function values. A function's name used anywhere but as a call's callee,
  `double` or `bytes.equal`, is a value of its function type, written as
  a signature is, with types alone and commas between them,
  `func(int64) int64`, `func([]uint8, mut list[int64]) (int64, error)`,
  `func()`: its parameters' types, `mut` before a `mut` one, `own`
  before an `own` one and `scope` before a `scope` one, `func(own
  list[int64]) int64`, `func(scope func(int64))`, and its results, so
  a function taking a list as `own` is never a value of a type that
  borrows it, `cannot use func(own list[int64]) as func(list[int64]) in
  declaration of g`, nor the other way round. Two functions of one signature have one type, and nothing else
  widens to one, `cannot use func(int64) int64 as func(int64) bool in
  declaration of g`. A function value is copyable and owns nothing: a
  local, a parameter, a result, a field, an array's, a list's or a view's
  element, a map's value or a union's payload can hold one. It is called
  through a name, `f(x)`, a local or a parameter holding it, with every
  rule a call by name has for its arguments, `mut` ones, views, borrows,
  moves, owned and error results and their order; the value is read
  first, then the arguments, left to right, so `f(reassign(mut f))` calls
  the function `f` held before `reassign` changed it. Calling anything else is
  `cannot call n: it is int64, not a function`, and a field holding one
  is called through a local, `run is a field of Op holding a function
  value, not a method; call it through a local, as in f := op.run, then
  f(...)`. A call by a bare name calls the function or builtin of that
  name before any local, so a local or parameter holding a function value
  under a function's name is `step calls the function step, not the
  function value of that name; rename one of them`. A function type has
  no zero value, `f needs a value: func(int64) int64 has no zero value`,
  so a struct or array holding one has none either, and the checked forms
  that would give a zero value cannot read one; no `==`, `operator == is
  not defined on func() int64`, so it is no map's key; no conversion; and
  it cannot be passed as `any`. A function type's parameters have no
  names, `a function type's parameters have no names; write only the
  type, not x`, and none is variadic, `any` or a `mut` view, though in a
  generic function one may be a type parameter, `func(T, T) bool`, and it
  returns no view, so a function whose signature has
  one, a generic function, `cannot use measure as a value: it is generic,
  and a generic function is only called, its type arguments inferred from
  the call`, and an extern func are no values; neither is a
  builtin, `cannot use the builtin len as a value; only a declared
  function can be one`. A method is a value named through its type, a
  method expression (see methods), `Pair.first` or `lexer.Token.Kind`,
  whose first parameter is its receiver. A function literal is a closure, which goes only
  to a scope parameter (see closures). Nothing holding a function value crosses to C,
  `parameter f of extern func take cannot be func(): a function value does
  not cross to C`, and no package-level variable holds one,
  `package-level variable f cannot hold a function value`. In C every
  function value is a `tack_fnptr`, one function pointer type, which a
  call converts back to its function type's pointer type, `tack_fn_` and
  the type's ID, before calling it; a function of several results named as
  a value is a thunk that returns them in its function type's results
  struct.
- Closures. A function literal, written as Go writes one, `func(a int64,
  b int64) bool { return a < b }`, is a closure: its body reads and
  writes the variables and parameters of the functions and closures it
  is written in, its captures, by reference, never as a copy, so a write
  in it is seen by the caller after the call and by the closure's next
  run, and a read sees the value current when it runs. A closure is
  second class: it can only be passed to a parameter marked `scope`,
  written before the name as `mut` and `own` are, `func each(v []int64,
  scope f func(int64))`, with no marker at the call. Anywhere else,
  assigned, returned, stored in a field, an array, a list, a map or a
  global, passed to a plain, generic or `any` parameter, it is `a closure
  can only be passed to a scope parameter`, `run does not take f as
  scope, and a closure can only be passed to a scope parameter`, and
  called where it is written it is `a closure cannot be called where it
  is written; it can only be passed to a scope parameter`. A scope
  parameter is only called, `f(x)`, or passed to another scope
  parameter, a nested closure's call included; anything else, `g := f`,
  `return f`, `S{f: f}`, `append(mut l, f)`, `plain(f)`, is `f is a scope
  parameter, which can only be called or passed to another scope
  parameter`. So no value a closure makes outlives the call it is passed
  to. A plain function value, a function's name or a local holding one,
  may be passed to a scope parameter too, so `sort.by(mut l, less)` takes
  a named function as before. `scope` is part of a function type,
  `func(list[T], scope func(T, T) bool)`, and two function types are one
  only with the same scope marks, `cannot use func(func()) as
  func(scope func()) in assignment to g`, as an interface's method and a
  type's must match. `scope` on a parameter that is not of a function
  type or `Allocator` (see regions) is `parameter n cannot be scope:
  int64 is not a function type or Allocator, and only a function value or
  an Allocator can be scope; remove scope`, as in a function type;
  `scope mut f` is `a parameter cannot be both scope and mut: a scope
  parameter is only called or passed on to another scope parameter, never
  assigned`, and `scope own f` the same of own; a variadic one, a result, `a result cannot be
  scope; only a parameter can be scope`, and an extern func's parameter,
  `parameter f of extern func take cannot be scope: a function value
  does not cross to C`, are errors. `scope` is a keyword. A closure's
  parameters follow a function's rules, `mut`, `own` and `scope` ones
  included, but its signature must be a function type's: no variadic or
  `any` parameter and no view result, `this closure has no function type:
  its parameter xs is variadic, which no function type's can be`. Its type
  must be the parameter's exactly, `cannot use func(int64) bool as func()
  in argument to call`; a generic callee infers its type arguments from
  it as from any argument. A `return` in it returns from the closure,
  with the closure's results, `the closure declares no result, so it
  cannot return a value`, and one with results must end in one, `the
  closure must end in a return`; a `?` in it passes the error on as the
  closure's, `cannot use ? in the closure, which returns no error`; a
  `defer` in it runs when the closure returns; `break`, `continue` and
  `fallthrough` cannot leave it. Closures nest: an inner one may capture
  the outer one's parameters and captures, and a scope parameter of the
  function it is written in, which it may call. For a call's rules every
  capture is an argument of the call the closure is passed to, and a
  borrow, read in place however small its type: a closure that only reads
  `x` conflicts with `mut x` in the same call, unlike `f(mut x, x)` on a
  copyable `x`, `cannot capture x in a closure alongside mut x in this
  call: a closure reaches its captures in place, and a mut argument must
  be the only way to reach x during the call`, and with another closure
  of the call that writes it. A closure that writes a capture, assigning
  it or a part of it or passing one as `mut`, which needs it to be a
  mutable variable or a `mut` parameter as anywhere, counts as a mut
  argument of the call for every rule one is under: nothing else in the
  call may read it in place, `cannot pass l alongside a closure that
  writes l in this call: a list is not copied for a call, and a closure
  that writes l must be the only way to reach l during it`. A capture of
  `p.y` is a capture of the whole `p`. A capture moved before the call is
  a use after a move, one moved in the same call is `cannot use l
  alongside own l in this call: ...`, and a closure cannot move its
  captures, `a closure cannot move l, which it captures: the closure may
  run more than once`; it frees none of them, while what it makes is
  freed at each of its returns. A call that passes a closure writing `x`
  ends the facts about `x`, as `mut x` does, and inside a closure no
  fact from where it is written holds about a mutable variable, since the
  closure runs later, possibly many times; facts about immutable
  variables and plain parameters hold there (see docs/safety.md).
  Recursion through scope parameters is allowed, and is the parked
  stack-depth violation of docs/guarantees.md like any recursion. In C a
  scope parameter is a `tack_scope`, a function pointer and an
  environment pointer, which a call passes first,
  `((tack_sfn_ID)f.fn)(f.env, x)`; a closure is a static function of its
  own, `tack_closureN`, whose environment, `tack_envN`, a compound literal
  on the caller's stack, holds a pointer to each capture, read and written
  through it; a plain function value is passed through its type's adapter,
  `tack_adapt_ID`, with a pointer to a copy of the value as its
  environment.
- The statements are a call, a `return`, `if`/`else` (which chains with
  `else if`), `switch`, `fallthrough`, `for`, `break`, `continue`, `defer`, `unreachable`, a variable declaration, a
  declaration of several results, an assignment, and `x++`/`x--`. A call
  statement should do something: one that only gives a value, a
  conversion or `len`, as a union built does, is warned of, `int8(5) is
  not used` or `len(s) is not used`, even when an argument is a call,
  `int8(f())`, which Go makes an error. It still runs: it is evaluated,
  its arguments' calls with it, and its value discarded. One with an error
  of its own, a conversion that can fail among them, is reported for that
  alone, and one whose value holds a list, which nothing would free, is
  an error, `Value.items returns Value, which holds a list and cannot be
  discarded; bind it to a variable`. A function that declares a result must end in a terminating
  statement: a return, an if whose branches all terminate, a `switch` with a
  `default`, or on a union naming every one of its cases, whose cases all
  terminate, a case ending in `fallthrough` counting as one, as in Go, a
  `for` with no condition that no
  `break` leaves, a call to `exit`, which never returns: `func fail()
  int64 { exit(7) }` needs no `return`, or an `unreachable`. The rule is syntactic, so a call
  to a function that never comes back, one that calls `exit` included,
  does not satisfy it.
- Unreachable. `unreachable` is a statement, and a keyword reserved
  everywhere, so no local, parameter, field, function, type or global may
  be named by it; it takes no value and no parentheses. It marks a place
  the compiler must prove no run reaches, and a program it cannot prove
  is rejected: `unreachable may be reached; check a condition that rules
  this place out first, or handle the case here`. A place is proven when
  a statement before it in a block around it leaves or terminates, a
  `return`, `break`, `continue`, `exit`, another `unreachable`, or a
  `switch` on a union naming every case, each returning; when the facts
  in force there (see division) leave some variable no value, as inside
  `if x > 0 { if x < 0 { ... } }`, after `if x > 0 { return 1 }`, `if x <
  0 { return -1 }` and `if x == 0 { return 0 }`, or after an `else if`
  chain or a `switch` whose cases, with the variable's type and the
  bounds in force, cover every value: a `switch` on `d` in `[0, 2]` naming
  0, 1 and 2, or on a `bool` naming `true` and `false`, leaves its
  `default` no value, and, with no `default` and every case leaving, the
  code after it, which is reached only when no case matched; when every side of a true `||` or a false `&&` on
  the path to it, each taken alone, contradicts the facts there, those
  that arrive after the condition included: under `i` in `[0, 4]`, the
  body of `if i < 0 || i > 4` is unreachable, and so is `if x == 2`
  inside `if x == 1 || x == 3`; when a `bool` tested both ways, `if b`
  and `if !b`, leaves; and when relations contradict, `a < b && b < a`.
  As for every proof, a write that can reach the place ends a fact, so
  `if x > 0 { x = next(x); if x < 0 { unreachable } }` is rejected, with
  notes on the guard and the write. A field of a mutable variable and a
  mutable global never carry facts, and a call to a function that
  exits does not leave. It is a terminating statement, so a function
  with a result may end in it instead of a `return`, and a branch ending
  in it leaves, as one ending in `exit` does, so what got past it holds
  after it; statements after it are allowed, as after a `return`. It may
  be written in deferred code, proven by the facts in force at the
  `defer`. A union switch's `default` that full coverage forbids stays
  forbidden, even when it holds only `unreachable`. It lowers to
  `TACK_UNREACHABLE("main.tck:12")`, a macro of `<tack/tack.h>` calling
  the runtime's `tack_unreachable_`, in `unreachable.c`, which writes
  `tack: unreachable reached at main.tck:12 (compiler bug)` to stderr,
  frees what every allocator holds, as `exit` does, and aborts: reaching
  one means the compiler's proof was wrong. It is never C's
  `__builtin_unreachable`, which would make that undefined behavior.
- Switches. `switch x { case 1, 2: ... case y: ... default: ... }` compares
  the tag `x` with each case's values using `==`, under its rules: an
  untyped constant takes the tag's type, which it must fit, an integer
  value of any type compares with an integer tag by value, so
  `case int8(-1)` never matches a `uint8` tag of 255, other values have a
  common type with the tag, one widening to the other, and strings, bools,
  arrays and structs compare as `==` compares them. A tag that is an untyped constant
  is an `int64`. `switch { case c1, c2: ... }`, with no tag, takes each
  value as a `bool` condition, and a case matches when any of its
  conditions holds. The tag is evaluated once, before any case. Cases are
  tried top to bottom and a case's values left to right, and the first
  match wins: the values after it are not evaluated. Values need not be
  constants. `default` runs when nothing matches, and is tried last
  wherever it is written; there is at most one. Only the matching case's
  body runs, unless it ends in `fallthrough` (below).
  `break` and `continue` in a case act on the loop around the switch, not
  on the switch, unlike Go; outside a loop they are the usual error. Each
  case's body is a block of its own and may be empty, and so may the
  switch. Two cases with the same constant value, a known integer, a bool
  constant expression such as `true` or `1 > 2`, or a string literal, are
  an error. There is no init statement,
  `switch v := f(); v`, as `if` has none; the `v :=` of a type switch,
  `switch v := a.(type)`, is a binding, not one (see type switches). A
  switch on a union matches the case it holds instead of comparing it
  (see unions). A switch proves what the chain
  of ifs it stands for proves: `if c1 {} else if c2 || c3 {} else {}` for
  a tagless switch, and the same with `x == v` for each value `v` of an
  expression switch, so `switch d { case 0: return 0 }` proves `d != 0`
  after it, and so does every later case whose body leaves, while every
  case tried before it leaves too: `case 0: return 0; case -1: return 1`
  proves `d != 0` and `d != -1` after the switch, in either order. A
  later case and the default see every earlier case's values not
  matching. A case with several values proves what their `||`
  does inside its body: `case 1, 2:` proves the tag is between 1 and 2,
  and so not zero.
- Fallthrough. `fallthrough`, a keyword reserved everywhere, as the last
  statement of a case's body, goes on into the body of the case written
  next, as in Go, without testing that case's values, which are not
  evaluated: `switch n { case 1: print("1"); fallthrough; case 2:
  print("2") }` prints `12` for 1 and `2` for 2. The case written next is
  entered whatever the order cases are tried in, so a `default` written
  between cases is entered from the case before it, and enters the case
  after it. The case's body is a block, so it is left first: its defers
  run, and its locals are freed, before the next case's body starts, and
  a name it declared is not in scope there, `undefined: s`. Out of place
  it is an error: anywhere but the end of a case, `fallthrough must be
  the last statement of a case`, which covers one inside an `if` or a
  loop in a case; at the end of a switch's last case, `cannot fall
  through from the last case of a switch`; inside a `defer`, `cannot
  fall through out of a defer`; in a type switch, `cannot fall through in
  a type switch`; on a union, `cannot fall through in a switch on a
  union`; and anywhere outside a switch, `fallthrough is not in a
  switch`. A case's body that a fallthrough enters is reached two ways,
  by matching and by the fallthrough, and proves only what both give:
  what was known where the switch began, its own values matching and
  the cases tried before it not matching, joined with what is known at
  the fallthrough, after the falling case's defers, as `||` joins its
  sides. So in `switch { case d > 0 && n > 0: fallthrough; case d > 0:
  return n / d }` the second case proves `d > 0`, but in `switch d {
  case 0: fallthrough; case 1, 2: r = 10 / d }` it proves only that `d`
  is between 0 and 2, and the division is rejected, with no note, since
  what a join proves has no single cause to point at. A case ending in
  `fallthrough` adds no path to the code after the switch: it leaves when
  the case it enters does, so `switch { case d <= 0: fallthrough; case n
  < 0: return 0 }` proves `d > 0` after it, and a write in it reaches what
  the case it enters reaches. A `default` written before other cases runs
  after every case's values, and so does every case it falls into, so a
  write or a move in a value written after the `default` reaches them all. In C a switch with a `fallthrough` is a
  list of tests, each a `goto` to its case's label, in source order with
  the default's last, and then the cases' bodies in source order, each a
  C block after its label, one ending in `fallthrough` running into the
  next label, and any other jumping past the rest; every `goto` jumps
  forward, never into a block.
- Defers, block-scoped as in Zig rather than function-scoped as in Go.
  `defer { f(x) }` registers its block when execution reaches it, and the
  block runs when the block holding the defer is left, by any path:
  falling off its end, or a `return`, `break` or `continue` that leaves it,
  through any number of nested blocks. Each block runs the defers it
  reached last first, and a jump out of several blocks runs the innermost
  block's first. A defer that was never reached never runs. A defer in a
  loop's body runs at the end of each iteration that reached it, one ended
  by `continue` included, before a three-clause loop's post statement; one
  in a `switch` case runs when that case's body ends; one at the top of a
  function body runs when the function returns. `defer` is always followed
  by a block, even for a single call: `defer f(x)` is an error, since it
  looks like Go, which evaluates `x` at the defer, while a Tack defer
  evaluates everything when it runs, and the braces show that. The block
  may hold anything but a way out of it or another defer: a `return`
  anywhere in deferred code, a `break` or `continue` aimed at a loop
  outside it, and a `defer` at any depth in it are errors (only the
  outermost such `defer` is reported), while a loop inside the block
  breaks and continues as usual. The deferred block is
  evaluated when it runs, not when the defer is reached, unlike Go, which
  evaluates a deferred call's arguments at the defer: it reads every
  variable as it is then. It sees
  the names declared before the defer, not those after it. `return x`
  computes `x` before any defer runs, so a defer that changes `x` does not
  change what is returned, and the same holds for several results. `exit`
  ends the process without running any defer. Deferred code is checked
  with the facts in force where the defer is written (see division), which
  still hold when it runs: deferred code runs when its block is left, so
  every write in the fact's region outside the defer, the rest of the
  defer's block and every other defer in it included, ends a fact the
  deferred code relies on.
- Loops. `for cond { ... }` runs while `cond`, a `bool`, holds, and
  `for { ... }` runs until something in its body leaves it. `break` leaves the
  innermost loop and `continue` starts its next iteration; either one outside
  a loop is an error. There are no labels.
  A loop keeps a bound on a counter or a total it writes, declared before
  it, which then holds in the whole loop on every pass: the bound holds
  where the loop starts, by a fact or by the known value the variable was
  last given (`mut i := 0`, `i = 0`), and every write to it in the loop is
  `i++`, `i--`, `i += e`, `i -= e` or `i = e` storing a value the facts
  at that write, with the bound, prove within it. So `mut i := 0` then
  `for i < len(s) { use(s[i]); i++ }` proves `s[i]`: `i < len(s)` keeps
  `i++` from overflowing, so `i` stays at or above 0. The
  bound is not in force after the loop; see [docs/safety.md](docs/safety.md).
- Three-clause loops, `for init; cond; post { ... }`, as in Go. `init` runs
  once and may be any simple statement: a declaration, an assignment,
  `x++`/`x--`, or a call. `post` runs after each iteration, `continue`
  included, and may be any of those except a declaration. Every part may be
  left out (`for ;; { ... }`). A variable `init` declares belongs to the
  loop, and needs `mut` to be updated: `for mut i := 0; i < n; i++ { ... }`.
  A counting loop proves its variable in its body: when `init` declares it
  with a known value `c`, `post` steps it by a known amount (`i++`,
  `i += k`, or `i--`, `i -= k`, with `k` above zero), the condition bounds
  it on the side it moves towards without the step leaving its type, and
  the body never assigns it, the body knows `i >= c` counting
  up, or `i <= c` counting down. So `for mut i := 0; i < len(a); i++`
  proves `a[i]`. The post is proven not to overflow with the condition
  in force, less what the body writes, so that loop's `i++` needs
  nothing more, while `for mut i int8 = 0; i <= 127; i++` is an error;
  see [docs/safety.md](docs/safety.md).
- Range over an integer. `for i := range n { ... }` runs once for each `i`
  from `0` to `n - 1`, and not at all when `n` is zero or less; `for range n`
  does the same without a variable. `n` is an integer of any type, a
  constant being an `int64`, evaluated once, before the first iteration;
  over a variable `n` the body knows `i < n` while it does not assign
  `n`.
  `i` is a fresh immutable variable of `n`'s type each iteration.
- Range over an array or a string. `for i, x := range a { ... }` runs once
  for each element of the array `a`, in order, with `i` its index, an
  `int64`, and `x` the element; over a string, `x` is each byte, a
  `uint8`. `for i := range a` binds the index alone, `for _, x := range a`
  the element alone, and `for range a` neither. `a` is evaluated once,
  before the first iteration, and the loop walks a copy of it taken then:
  writing `a[j]` or assigning `a` in the body does not change what later
  iterations see. Both variables are fresh and immutable each iteration,
  so `x = v` and `mut x` are errors; change an element through `a[i]`.
  A view ranges the same way, its index an `int64` and its element of the
  view's element type, without copying its elements, since a view never
  changes, and so does a list, which the body cannot change (see lists).
  Over an integer, only one name is allowed, and three names are an error
  anywhere. An array's index is proven in range for any array at least as
  long; a string's or a view's for the string or view it ranges over (see
  indexing a string, and views).
- Local variables. `x := v`, `var x T = v` and `var x = v` declare an
  immutable variable; `mut` in place of `var` (`mut x := v`, `mut x T = v`,
  `mut x = v`) declares a mutable one, and `mut x T` starts it at its zero
  value: `0`, `""` or `false`. An immutable variable must have a value,
  and so must a variable of a type with no zero value, a list, an
  `Allocator`, a union, or a struct or array holding one.
- `x = v` assigns to a mutable variable a value its type accepts. Parameters
  are immutable, except a `mut` parameter (see mut parameters).
- `x += v`, `x -= v`, `x *= v`, `x /= v` and `x %= v` mean `x = x op v` for a
  mutable integer variable and a value its type accepts, and so do `x &= v`,
  `x |= v`, `x ^= v` and `x &^= v`; `x <<= n` and `x >>= n` shift `x` by `n`,
  a shift's count (see expressions). Like a plain assignment, they do
  not count as reading `x`, so a variable that is only ever updated is still
  unused. `x++` and `x--` add and subtract one the same way. They are
  statements, never values, and there is no prefix form.
- Structs. `type Point struct { x int64; y int64 }` declares a struct type,
  at package level only, with its fields one per line or separated by `;`;
  `struct {}` has none, and is allowed, as in Go. A type can be used anywhere in its package, before its
  declaration included, and its name shares one namespace with functions,
  package-level variables and builtins, so it cannot be declared again as
  any of them, nor as a local or a parameter; it cannot be used as a value,
  assigned or called, and a name that is not a type cannot be written as
  one. Each declaration is a type of its own, even with the same fields as
  another. Field names are a namespace of their own, one per struct: a
  field may share a name with anything outside its struct, but not with
  another of its fields. `_` and the keywords cannot name a field, but any
  builtin's name can, a builtin type's, `span`, `id`, `handle`, `int64`,
  `string`, or a builtin function's, `len`, `add`, `insert`: a field is
  written only after `.`, `t.span`, or as a literal's key, `Token{span:
  4}`, where no type or builtin can be, so `len(t.len)` calls the builtin
  on a field named `len`. In C a field's name is prefixed, `f_int64`, so
  none clashes; a field of a struct `tack.h` declares keeps its name
  there, so one named `bool`, a C keyword, is an error. An
  array length in a field's type is a constant expression that names no
  variable: literals, character literals, arithmetic on them and `len` of
  a string literal, as in `[len("abc") * 2]int64`. A length naming any
  variable, even an immutable global holding a constant, as in `[n]int64`
  or `[len(g.cells)]int64`, is an error, `array length in a struct field
  must not name variable n`, so a struct's layout never depends on a
  variable. A
  field's type may be any type, another struct or an array of one
  included, except one that holds the struct itself, directly, through
  other structs or through an array of any length, which would be
  infinitely large and is an error. Named types other than structs,
  interfaces and unions (`type Celsius int64`), `only struct, interface
  and union types can be declared`, anonymous struct types and embedded
  fields are not supported, and each is an error. A field may be `pub` or `lib`, as
  described under visibility. A struct type may have methods (see
  methods), and satisfy interfaces with them (see interfaces), as a union
  type may.
- Struct literals name every field exactly once, in any order:
  `Point{x: 1, y: 2}`. A literal without field names, `Point{1, 2}`, a
  field listed twice, a field the struct does not have, a field name in
  parentheses, `Point{(x): 1}`, and a field left out are each an error. Each value is used as its field's type, under the
  usual rules: a constant takes it, and a narrower integer widens to it.
  Literals nest, `Line{a: Point{x: 1, y: 2}, b: p}`, and inside an array
  literal a struct literal may leave out its type like an array literal
  can: `[2]Point{{x: 1, y: 2}, {x: 3, y: 4}}`. A field's value may not
  leave out its type. In the header of an `if`, a `for`, a range loop or a
  `switch`, a struct literal must be in parentheses, `if p == (Point{x: 1, y: 2})`, as
  in Go, since the brace would otherwise open the body; inside a call, an
  index or another literal it needs none. Written without them, as in
  `if p == Point{x: 1} {`, it is reported as needing parentheses when the
  brace is followed by a name and a colon; other forms, such as
  `if e == Empty{} {`, are a plain syntax error.
- A struct is a value, like an array: assigning, passing or returning one
  copies it, and a function may take and return structs, several results
  included. `p.x` reads a field of any struct value, a variable, an
  element, another field, or a call's result, `f().x`. A struct follows its
  variable, so `p.x = v`, `p.x += v` and `p.x++` need `p` to be `mut`;
  there is no `mut` per field. A target may mix fields and elements to any
  depth, `l.a.x = 1`, `a[i].x = 1` and `p.cells[i] = 1`, with every index
  proven in range as usual, and writing one does not count as reading the
  variable. `mut p Point` with no value starts with every field at its
  zero value, as `mut` does for any type. Two structs of one type compare
  with `==` and `!=`, field by field, a string field by its bytes, and a
  struct with no fields is always equal to another. A struct holding a
  list is owned, as a list is: it moves rather than copies, has no zero
  value, and compares with `==` as a list does, reading its lists in
  place (see lists); `==` compares every field, a private one included,
  so two `text.Text` values are equal when their bytes are. Structs are not
  ordered: `<` on two structs is an error. Structs may be package-level
  variables, initialized with a constant expression (see package-level
  variables). A field of an immutable global is a known value, so `n /
  origin.d` compiles when `origin.d` is not zero. A field of any
  immutable variable or parameter carries facts for proofs as a variable
  does, since nothing can change it: `if p.d != 0 { n / p.d }` compiles
  when `p` is immutable, and so do fields of fields, `p.inner.d`, elements
  at a known index or an immutable name's, `p.a[1]` or `p.a[k]`, and a
  string field's length, `if len(p.s) > 0 { p.s[0] }`. A field of a
  mutable variable or a `mut` parameter never carries a fact: there the
  guard proves nothing, and the division needs `q, ok := n / p.d`, or the
  field copied into a variable the guard then names, as in `d := p.d` and
  `if d != 0`.
- Unions. `type Expr union { int int64; name string; binary Binary; eof
  }`, at package level, written as a struct is with one case per line or
  separated by `;`, declares a union type: a value of it holds exactly one
  of its cases, each a name and the type of its payload, or no payload,
  as `eof`. `union` is a keyword. A case's name is in its union's
  namespace, as a field's is in its struct's: it may share a name with
  anything outside its union, a type, a variable or a function included,
  and is neither `_`, `cannot use _ as a name`, nor, unlike a field, a
  builtin type's name, `cannot use string as a name; it is a type`, since
  a switch's clause writes a case bare, `case len(n)`, where a switch on
  an `any` writes types; it is once in its union, `duplicate case
  name`, with a note at the first. A payload's type may be any a field
  may have, a struct, an array, a list or another union included, under
  a field's rules: an array length in it names no variable, `array length
  in a union's case must not name variable n`. `union {}` has no cases,
  and so no value: a function taking one can never be called, and a
  switch on one needs no clause. A union containing itself, directly or
  through a struct, an array or another union, would be infinitely large,
  `invalid recursive type Expr`, with a note for each step round, `Expr
  refers to Binary`, and through a list too, `invalid recursive type
  Expr: a type cannot hold itself, even through a list`, so a tree is a
  list of nodes naming each other by index, as `examples/unions` builds
  one. A case has no visibility of its
  own, `a union's case cannot be pub; it is as visible as its union`, and
  no value of its own, `a union's case cannot have a tag value; it is a
  name and a payload's type, if any`; `anonymous unions are not
  supported`. A union is `pub` or `lib` as a struct is, and exposes its
  payloads' types as a field does, `pub case binary of Expr exposes
  binary, which is not pub`. It may have methods, as a struct type may,
  none named like one of its cases (see methods), and so satisfy
  interfaces and be a type argument to a generic function (see interfaces
  and generic functions).

  `Expr.int(5)` builds a union holding the case `int` with the payload
  5, which is used as the case's type, as an argument to a parameter of
  it is: an untyped constant takes it, a narrower integer widens to it,
  and anything else is `cannot use string as int64 in argument to
  Expr.int`, with one payload exactly, `Expr.int expects 1 argument(s),
  got 2`, and no spread, `cannot use ... in a call to non-variadic
  Expr.int`. A payload holding a list is moved into the union, as into a
  struct literal's field. A case with no payload is named without
  parentheses, `Expr.eof`; `Expr.eof()` is `Expr.eof has no payload;
  write Expr.eof`, `Expr.int` alone `Expr.int needs its int64 payload, as
  in Expr.int(...)`, and a name the union has neither as a case nor as a
  method `Expr has no case or method nope`.
  From another package it is `ast.Expr.int(5)`, its cases exactly as
  visible as the union, `Expr is not pub in package calc/ast`. Nothing
  makes a union of a payload implicitly. A union built as a statement is
  warned of, `Expr.int(5) is not used`. A union has no literal, `Expr cannot be
  written as a literal; only arrays and structs can`, no field, `Expr has
  no field int`, and is no function, `cannot call Expr; it is a type`. A
  case is no method: `e.int(5)` on a value `e` is `Expr has no method
  int`, and a method named through the type, `Expr.pos(e)` or
  `Expr.pos`, is a method expression (see methods). It
  is a value, copied when assigned, passed or returned, unless it holds a
  list (below), and changed only whole, `e = Expr.int(3)`. A
  package-level union is a constant expression when its payload is, `var
  start = Token.plus`, `var keywords = [2]Token{Token.word("if"),
  Token.eof}`. There is no zero value: `mut e Expr` is `e needs a value:
  Expr has no zero value`, and so for a struct or an array holding one,
  and the checked form cannot read one, `the checked form cannot read an
  element of type Expr: it has no zero value to give when the index is
  out of range`.

  `switch e { case int(n): ... case binary(b): ... case name, eof: ...
  }` runs the clause naming the case `e` holds. Each value of a switch on
  a union names one of its cases: anything else is `a case of a switch on
  Expr names one of its cases, as in case int(v)`, a name the union lacks
  `Expr has no case nope`, and a case named twice `int appears twice in
  this switch`, with a note at the first. A clause naming one case with a
  payload may bind it, `case int(n)`: `n` is an immutable variable of the
  payload's type in that clause's body, and refers to the payload in
  place rather than copying it, so a payload holding a list is read as a
  range loop reads one. Like any variable it may reuse no name visible at
  the switch, `n is already declared`, `fmt is an imported package and
  cannot be redeclared`, `cannot use int64 as a name; it is a type`, and
  one its clause never reads is warned of, `declared and not used: n`. It cannot be
  assigned, `cannot assign to immutable variable n`, passed as `mut`,
  `cannot pass mut n: n binds a union's payload, which cannot be mut`, or moved,
  `cannot move l: it refers to a union's payload in place, and a payload
  cannot be moved out of a union`; and while it refers into the switched
  place, its clause cannot assign that place or anything holding it, pass
  either as `mut`, or move it, `e is being matched, and n refers into
  it, so this case cannot assign e` (`pass mut e`, `move e`), as a range
  loop over a list cannot change the list. A clause binding nothing may.
  A case with a payload named alone, `case int:`, binds nothing; `case
  int(_)` is `every name is _, so nothing is bound; write case int`; a
  case with no payload binds nothing, `eof has no payload to bind; write
  case eof`; and a clause naming several cases binds nothing, `a clause
  naming several cases binds nothing; write int, or give int(n) a clause
  of its own`. The clauses cover every case or the switch has a default,
  `switch on Expr misses cases name, eof; cover them or add a default`,
  but not both, `every case of Expr is already covered, so this default
  never runs`. A switch covering every case terminates when its every
  clause does, as one with a default does. Otherwise it is a switch: the
  tag is evaluated once, the clauses are tried in order with the default
  last, there is no fallthrough, `cannot fall through in a switch on a
  union`, and `break` and `continue` act on the loop around it. It gives
  no facts, since which case a union holds is
  known only at run time. The tag may be any union, a call's result
  included, but one holding a list must be a place, `cannot use a
  temporary Value here; store it in a variable first`. There is no other
  test of a case: no assertion, and no `is`. A switch's values are
  patterns only when its tag is a union; written in any other switch,
  they are values, and a name in one that names nothing is `undefined:
  n`, as anywhere.

  Two unions of one type compare with `==` and `!=`: equal when they hold
  the same case and, for a case with a payload, equal payloads, compared
  as `==` compares the payload's type, so `tok == Token.eof` tests for a
  case with no payload. A union with a payload of a type without `==`,
  a view or an `Allocator` among them, has none, `operator == is not
  defined on Value`; a payload holding a list compares as a list does. A union with a case whose payload holds a list,
  directly or through a struct, an array or another union, holds a list,
  and is owned as a struct holding one is (see lists): it moves, cannot be
  a package-level variable, `package-level variable g cannot hold a
  list`, nor an array's element, though a list's element it may be,
  matched in place (see lists), and frees the lists the case
  it holds holds when its owner's block ends, or when it is assigned,
  once the new value is computed. A union cannot be passed as `any`,
  `cannot pass Expr as any: only the builtin types and views of them can
  be passed as any for now`, so printf cannot print one, nor converted, `cannot convert
  Expr to int64`, nor cross to C (see extern C). In C a union is a
  struct of its tag, the position of its case, in the narrowest of
  `uint8_t`, `uint16_t` and `uint32_t` that holds every case's, and a C
  union of its payloads, `{uint8_t tag; union {int64_t c_int; ...} u;}`;
  a switch on one holds a pointer to the switched place, or to a copy of
  a value that is no place, or of a mutable global, which a call in a
  clause could assign, and a binding is a pointer to its payload.
- Methods. A function written with a receiver before its name, `func (b
  Builder) len() int64` or `func (mut b Builder) write(s string) error`, is
  a method of the receiver's type, which must be a struct or union type
  declared in the same package, in any file of it, before or after the
  method; any other type, one of another package, an interface, a basic
  type, `any`, `error`, an array, a view or a list, is `cannot declare a
  method on lexer.Token: only a struct or union type declared in this
  package can have methods`. A method
  is called on a value of its type, `b.len()`: a local, a parameter, a
  field, `w.out.len()`, an element, `rows[i].len()`, or a temporary,
  `makePoint(3, 4).norm()` or `Point{x: 1}.norm()`, and it is found from
  that value's type, of any package, as `t.kind()` for a `lexer.Token`
  `t`. A type with no such method is `Builder has no method nope`. The
  receiver is a parameter, written first, with a parameter's rules for its
  name: `_` and a type's name are errors, and no other parameter or local
  may reuse it. A plain receiver is a plain parameter, an immutable copy
  of any value of the type, and a struct holding a list is borrowed, not
  copied, so a temporary one is `cannot use a temporary Builder here;
  store it in a variable first`. A `mut` receiver is a `mut` parameter: the
  call writes `mut` before it, `mut b.write(s)`, as a statement or as a
  value, `err := mut b.write(s)`, and `mut` covers what comes before the
  last `.name(`, so `mut w.out.write(s)` changes `w.out`. The receiver is
  then a place the caller may change, under every rule of a `mut`
  argument: `cannot pass mut b: b is immutable; declare it with mut`,
  `cannot pass mut g: g is a mutable global, which any function can
  reach; copy it into a mutable local`, and, for
  a call's result, `only a variable, a field or an element can be passed
  as mut`. It is one of the call's `mut` arguments, which must not
  overlap, so `mut p.take(mut p)` is `p is passed as mut twice in this
  call; ...`, no plain argument may read a list it holds, `mut b.fill(b)`
  being `cannot pass b alongside mut b in this call: ...`, and like any
  `mut` argument it ends the facts about its place. Leaving `mut` out for
  a `mut` receiver is `Builder.write takes its receiver as mut; call mut
  b.write(s)`, and writing it for a plain one is `Builder.len does not
  take its receiver as mut; call b.len()`. A method may return a view of
  what a plain receiver holds, `func (t Text) bytes() []uint8 { return
  t.b }`, under the rules of view results, so another package reads it
  without a `pub` field; a `mut` receiver's is `cannot return a view of
  t.b: t is a mut parameter; ...`. The receiver is evaluated
  first, then the arguments, left to right. A method's name is in its
  type's namespace: it may share one with a function, a global, `main` or
  a builtin, as `func (b Builder) len() int64` and `func (mut b Builder)
  append(c uint8) error` do, since a method is only ever reached as
  `x.name(...)`, and inside a method a bare `len(x)` is still the
  builtin; it may not share one with a field of its struct, `len is
  already declared as a field of Builder`, a case of its union, `int is
  already declared as a case of Expr`, so `Expr.int(5)` always builds a
  case, `Expr.pos` always names a method, and `e.m()` always calls one,
  or with another method of it,
  `Builder.len is already declared`, and it follows a field's naming
  rule, so it may be named like a builtin type too, `func (s Span)
  int64() int64`, called `s.int64()` and named `Span.int64`. A method named through its type, `Builder.write` or
  `lexer.Token.Kind`, is a method expression: a function value whose
  first parameter is the receiver, so `func (p Pair) has(x int64) bool`
  gives a `func(Pair, int64) bool`, a `mut` receiver a `mut` first
  parameter, `func (mut b Builder) write(s string) error` giving a
  `func(mut Builder, string) error`, and a `scope` parameter keeps its
  mark, `func(Pair, scope func(int64) bool) bool`. A method and a
  function of one signature are values of one type. It is used wherever
  a function value is (see function values): assigned, passed, to a
  `scope` parameter too, returned, and held by a field, an array, a list,
  a map or a union, and called through a local, `f(mut p, 9)`, under every
  rule of a call through a value; it has no `==`. Called directly,
  `Pair.first(p)` or `Builder.write(mut b, s)`, it is a call of the method
  with the receiver passed first, as `p.first()` is, under every rule of a
  call by name, `Box.set takes b as mut; pass mut b`, with the method's
  contract in force after it; called through a value, no contract holds,
  since a function type states none. Naming one follows the method's
  visibility, capped by its type's, `hidden is not pub in lexer.Token`,
  `Sum is lib in module std and cannot be used from module calc`, and a
  type another package cannot see cannot be named, `secret is not pub in
  package calc/lexer`; the value goes anywhere, so a pub function may
  return a private method's. A name its type has no method of is
  `Builder has no method nope`, a field is `n is a field of Builder, not a
  method; only a method can be named through its type`, and a basic type
  has none, `int64 has no method abs`. A method whose signature no
  function type can say is no value, `cannot use Box.all as a value: its
  parameter xs is variadic, which no function type's can be`, as for a
  function. An interface is only a constraint, `cannot use Sizer.size as
  a value: Sizer is an interface; only a struct or union type's methods
  are values`, and a type parameter's methods are only called on a value
  of it, `cannot use W.size as a value: W is a type parameter, and a type
  parameter's methods can only be called on a value of it`. A method expression is a
  value, not a place: `Box.size = f` is `cannot assign to Box; it is a
  type`, and `mut Box.size` is `only a variable, a field or an element
  can be passed as mut`. There are no bound method values: `b.write`
  without a call is `write is a method of Builder, not a field; a method
  can only be called`; a field called as a method is `n is a field of
  Builder, not a method; a field cannot be called`. Inside a method
  the receiver is an ordinary parameter: fields are reached as `b.x`, with
  no implicit receiver, and methods call each other, and themselves,
  whatever the order of their declarations. A method follows every rule
  of a function: its results, an error returned last that its callers
  must check, `the error Builder.write returns cannot be discarded; bind
  it, as in err := mut b.write(...)`, defers, lists and moves, so a
  receiver is never moved, `any` and variadic parameters, a spread, and
  forwarding a variadic to `fmt.printf`; a diagnostic names a method by
  its type and its name, `Builder.write expects 1 argument(s), got 2`. A
  method may call an extern func but cannot be one, `a method cannot be
  extern; declare an extern func and call it from the method`, nor be
  declared without a body, `method Builder.write has no body`. Builtins
  stay functions: there is no `l.len()`. In C a method is a function of
  its own, named from its package's path, its type's name and its own,
  taking the receiver first, a `mut` one as a pointer, as a `mut`
  parameter is; a method expression is that function as a function
  pointer, or, for several results, its thunk, and called directly it
  calls the function. A union's methods are a struct's in every way: `func (e
  Expr) pos() int64` may switch on its receiver, and a `mut` one may
  reassign it, `e = Expr.eof`, under the rule for a switched place, so a
  clause binding a payload of the receiver cannot assign it or pass it
  as `mut`, `e is being matched, and n refers into it, so this case
  cannot assign e`, and a caller's `mut e.set()` in such a clause of a
  switch on `e` is `... cannot pass mut e`, while a plain call, `e.pos()`,
  reads it and may. A plain receiver of a union holding a list borrows
  it, and a `mut` one assigned frees the old payload's lists. A union
  built in place is no place: `mut Expr.eof.set()` is `only a variable, a
  field or an element can be passed as mut`, and `Expr.eof.set()` is
  `Expr.set takes its receiver as mut; only a variable, a field or an
  element can be passed as mut`. A struct or union type's methods
  satisfy interfaces (see interfaces); there are no method sets,
  embedding or generic methods.
- Interfaces. `type Writer interface { mut write(b []uint8) error; size()
  int64 }`, at package level, written as a struct is with one method per
  line or separated by `;`, declares an interface: the methods a type must
  have, each written as a function's signature without `func`, a receiver
  or a body, `mut` first for one that takes its receiver as `mut`. Its
  parameters and results follow a function's rules, a `mut`, a view, a
  variadic or an `any` parameter included, and an error result is last,
  `error must be the last result of Parser.parse`. `interface {}` asks for
  nothing, and every struct and union type satisfies it. A method's name
  is in its interface's namespace, follows a field's naming rule, a
  builtin type's name allowed, `span() int64`, and is declared
  once, `Writer.write is already declared`; its parameters are named, no
  two alike. An interface holds methods only: `an interface cannot have
  fields; it holds only methods`, `embedded interfaces are not
  supported`, `interface method write cannot have a body`, and `an
  interface's method cannot be pub; it is as visible as its interface`.
  The interface is `pub` or `lib` as a struct is, and exposes the types
  its methods name as a function does, `pub interface Maker exposes
  secret, which is not pub`. An interface is no type a value can have,
  since there are no interface values: it only constrains a type
  parameter (see generic functions). As the whole type of a function's
  parameter it is shorthand for one: `func emit(mut w Writer, s string)`
  is `func emit[W Writer](mut w W, s string)`, each such parameter a type
  parameter of its own, so `both(mut a Writer, mut b Writer)` may be
  passed two types, and the function is generic in every way, `cannot
  use emit as a value: it is generic, ...`. Written as any other type, a
  local's, a method's or an extern func's parameter's, a variadic
  parameter's elements, a result's, a field's, a global's, an element's,
  a function type's parameter, or in a literal, it is `Writer is an
  interface; it can only constrain a type parameter`, `sink.Sink is an
  interface; ...` from another package.
  It cannot have methods declared on it, `cannot declare a method on
  Writer: only a struct or union type declared in this package can have
  methods`, nor be called, `cannot call Writer; it is a type`. A struct or
  union type satisfies an interface, with nothing written to say so,
  when for each of the interface's methods it has a method of that name
  that takes its receiver as `mut` exactly when the interface's does,
  with the same parameters, each of the same type and alike in being
  `mut`, `own` or variadic, and the same results: `Expr does not satisfy Sized:
  Expr.size takes its receiver as mut, and Sized's size does not`. Each
  of those methods must also be visible where the type is chosen, from
  the package holding the call that binds a type parameter to the type,
  as if that code called the method itself: a private method within its
  own package, a `lib` one within its module, a `pub` one anywhere. The interface's package
  plays no part, so `main` passes a type of its own, with private
  methods, to another package's generic function, while a third package
  cannot pass a type whose method it cannot call, `words.Bag does not
  satisfy sink.Sink here: put is not pub in words.Bag`, `shapes.Tile
  does not satisfy shapes.Sized here: len is not pub in shapes.Tile` for
  a union, `util.Pair does
  not satisfy util.Counted here: count is lib in module std and cannot
  be used from module calc`. Interfaces exist only at compile time:
  nothing of them is in the C.
- Generic functions. `func render[W Writer](mut w W, s string) error`
  declares type parameters, in brackets after the name, each a name and
  its constraint, an interface, plain or qualified, `[A Writer, B
  sink.Sink]`, with commas between them optional as between parameters.
  Each is a type within its function, its signature and its body, which
  stands for whichever type a call passes. Every type
  parameter needs an interface for its constraint: `type parameter W has
  no constraint; write the interface it must satisfy after it, as in [W
  Writer]`, and anything else, a struct, a basic type, `any` among them,
  a view or another type parameter, is `Point is not an interface; the
  constraint of type parameter W must be one`. A type parameter must
  appear in some parameter's type, whole, `w W`, or as a part of it, a
  list's, a view's or a map value's element or a function type's
  parameter or result, `l list[T]`, `v []T`, `m map[string]T`, `less
  func(T, T) bool`, as a call infers it from the arguments, `W appears in
  no parameter of f, so no call can infer it`. A result may be one, or be
  made of one (see below). `[]` is `empty type parameter list`. A type
  parameter's name follows a parameter's rules: `duplicate type parameter
  name`, `Point is already declared` for a package-level name, and no
  parameter or local may reuse it. It is a type, never a value: `cannot
  use W as a value; it is a type`, `cannot call W; it is a type`, `cannot
  assign to W; it is a type`. Only a function is generic: `a method cannot
  have type parameters; only a function can be generic`, and `extern func
  f cannot have type parameters; its body is written in C`; there are no
  generic types. The body is checked once, against the constraints,
  never for each call, so what is reported never depends on which types a
  call passes. A value of a type parameter has its constraint's methods,
  called as a struct's are, `w.size()` and `mut w.write(b)`, under every
  rule of a method call, `W.write takes its receiver as mut; call mut
  w.write(b)`, `W has no method nope`, but not named through the type
  parameter, `cannot use W.size as a value: W is a type parameter, and a
  type parameter's methods can only be called on a value of it`, and nothing else: `W has no field
  n: a type parameter has only its constraint's methods`, `operator == is
  not defined on W`, `cannot pass W as any: only the builtin types and
  views of them can be passed as any for now`, `W cannot be written as a literal; only arrays
  and structs can`, and no conversion, `len` or range. Since a struct
  holding a list may be passed for it, a type parameter is owned as a
  list is (see lists): a value of one moves rather than copies, so a
  parameter of one, which borrows its argument, cannot be moved, `cannot
  move w: it is a parameter, which borrows its caller's value`, a moved
  local cannot be used, a temporary must be stored, `cannot use a
  temporary S here; store it in a variable first`, it has no zero value, `z needs a value: S has
  no zero value`, it is never borrowed alongside a `mut` argument of
  itself, and it cannot be an element of an array or a variadic, `a
  variadic parameter cannot hold S: its elements are copied, and S, a
  type parameter, which may hold a list, cannot be`, nor a map's key,
  `cannot use T as a map key: a map's key cannot be a type parameter`.
  It is owned whatever a call makes it, so an instance whose type
  argument is `int64` follows the same rules, a move of it being a copy.
  A list, a view, a map's value and a function type may be made of one,
  in a signature and in a body, `make(list[T], alloc)` among them: their
  elements are owned ones, read in place, moved in by `append` and `put`
  and out by `pop`, `remove` and `delete`, never copied out, `cannot move
  l[0] out of l: an element cannot be moved out of a list; take it out
  with pop or remove`, and `swap(mut l, i, j)` reorders them in place. So
  a generic function makes a value of a type parameter by moving one out
  of a list or getting one from a function value or a call returning one,
  and may return it: `func take[T Any](mut l list[T], mk func() T) T { if
  len(l) > 0 { return pop(mut l) }; return mk() }`, or by taking one as
  an own parameter, `func push[T Any](mut l list[T], own x T) error`
  (see own parameters). A plain parameter is never moved, `cannot move x:
  it is a parameter, which borrows its caller's value`, and a result of one cannot be discarded, `stuck returns S, which
  may hold a list and cannot be discarded; bind it to a variable`. A call is written as any call, `render(mut f,
  "x")`; type arguments are never written, `cannot write type arguments;
  they are inferred from the arguments, as in render(...)`. Each type
  parameter is given its type from the arguments, in order: the type of
  one passed to a parameter of that type, or the part of an argument's
  type where the parameter's type has it, the element of a list, an
  array or a view passed for a `list[T]` or a `[]T`, the value type of a
  map whose key type is the parameter's for a `map[K]T`, or a parameter's
  or a result's type of a function value passed for a function type,
  whose shape must match, `cannot use list[int64] as map[string]T in
  argument to size`, a function type's with its `mut`, `own` and `scope`
  parameters where the parameter's type has them, since a mark belongs
  to its parameter and no type argument supplies one, `cannot use
  func(scope func()) as func(T) in argument to g`; a type argument may
  itself be a function type taking a scope parameter, `func(scope
  func())`, whose values are plain function values. A type argument may
  be any type but a view, `cannot use []int64 as T in argument to keep: a type argument cannot be a
  view`, an `any`, `cannot use any as T in argument to one: a type
  argument cannot be an any`, and a type made of a type parameter,
  `cannot use list[T] as U in argument to count: a type argument cannot
  be made of a type parameter, only be one`, so a type parameter is
  passed on only whole; a type that is no struct or union has no
  method, so it satisfies only an interface asking for none, `int64 does
  not satisfy Sizer: int64 has no method size`, `type Any interface {}`;
  two arguments for one type parameter must be of one type, `cannot use
  Buffer as W in argument to both: W is Counter in this call, from an
  earlier argument`, and the type must satisfy the constraint: `Sized
  does not satisfy Writer: Sized has no method write`, `P does not
  satisfy Sizer: P.size takes its receiver as mut, and Sizer's size does
  not`, `P does not satisfy Sizer: P.size is func() int32, and Sizer's
  size is func() int64`, and each method must be visible from the
  calling package (see interfaces), `sink.Tally does not satisfy
  sink.Sink here: put is not pub in sink.Tally`. A type parameter passed
  on satisfies an interface when its own constraint has each method the
  interface asks for, alike in the same way, `S does not satisfy Writer:
  its constraint Sizer has no method write`, and is checked for no
  visibility: the call that bound a struct or union type checked every
  method of its constraint, and no generic function it is passed on to
  can ask for another. Arguments
  are otherwise checked as for any function: a `mut` parameter of a type
  parameter takes a `mut` place, `render takes w as mut; pass mut c`. A
  generic function may call itself and other generic functions, with its
  type parameters as type arguments; it is never a value, `cannot use
  measure as a value: it is generic, and a generic function is only
  called, its type arguments inferred from the call`. A `pub` or `lib` one exposes its constraints, `pub function
  Size exposes hidden, which is not pub`. In C each generic function is
  compiled once for every set of type arguments the program's calls need
  it with, an instance, found from the functions that are not generic
  and then from each instance, and emitted once however many calls need
  it; an uncalled one is not emitted. An instance is a function of its
  own, named from its function's name and its type arguments,
  `render[main.File]` or `sort[list[int64]]` mangled, its method calls
  on a type parameter call the type argument's method directly, and its
  `list[T]` is the C type of `list[int64]` its caller passes. Instances
  are finite, since every type argument is one code that is not generic
  writes, a type parameter being passed on only whole, but a program
  needing more than 10000 is an error,
  `instantiating g with ... makes more than 10000 instances of generic
  functions, the most a program may have`, so the compiler never runs
  away. A type parameter's value is passed and moved as its type
  argument's would be, so in an instance whose type argument holds no
  list a move is a copy, and nothing is freed.
- Package-level variables. `var` and `mut` declarations may also appear
  outside any function, where `:=` is not allowed. A package-level variable is
  initialized with a constant expression, which the compiler computes:
  nothing runs before `main`. A constant expression is one built only from
  - literals: integer, character, string and bool;
  - names of immutable globals, of the package or of an import, declared
    before or after;
  - array and struct literals of constant expressions;
  - a field of a constant expression, `origin.x`, an element of one at a
    constant index, `table[0]` or `table[len(table) - 1]`, and a byte of a
    constant string, `word[0]`; an index out of range is the usual error,
    `index 3 is out of range for [3]int64`;
  - `len` of a constant expression, and `len` of any array, even a mutable
    one, whose length is its type's, as long as computing it calls nothing;
  - a conversion `T(x)` of a constant expression, which, as anywhere, must
    give a value of `T`;
  - arithmetic, bitwise and shift operators, comparisons, `&&`, `||` and
    `!` on constant expressions, `^x` of a target-width type excepted,
    since its value is the target's,
    strings, arrays and structs comparing as they do at run time.

  A package-level variable cannot hold a list (see lists).
  Anything else is `x must be initialized with a constant expression`: a
  call to a function, or to a builtin other than `len`, and a mutable
  global outside `len` of an array. Overflow and division by zero in a
  constant expression are compile errors, as for any known value (see
  arithmetic). A global can be used from any function, whatever the
  order of declarations, and shares one namespace with functions and
  builtins. All of a program's globals together hold at most 2^30 bytes
  (see size limits).
- A variable is in scope from its declaration to the end of its block. No
  variable or parameter may reuse the name of another variable or parameter
  in scope, package-level ones included. A variable that is never read
  is a warning, `declared and not used: x`, but for one holding an error
  or an ok, which is an error (see errors and the checked forms); and so is an import no name
  uses, `imported and not used: fmt`; neither stops the program from
  building. So is, in a program with no error, a parameter its function
  never reads, `declared and not used: by`, and a private function,
  method or type nothing names, `declared and not used: helper`, `...:
  T.spare`, `...: Point`. A function is used when a name outside its own
  body names it, called or as a value; a method when a call outside its
  own body calls it, or when a generic call binds its type to a type
  parameter whose constraint asks for it; a type when a name outside its
  declaration and its methods names it, a field of another type, a
  literal, a signature or a constraint among them. Uses are not followed
  further: a function only an unused one calls is used. `pub` and `lib`
  declarations, which another package may use, are never warned of, nor
  is `func main`, nor a method's receiver, nor a struct's field; nor are
  the parameters of a function whose signature is not its own to change:
  an extern func's, a `pub` or `lib` function's or method's, a function
  used as a value, and a method a constraint asks for. An unused
  function's parameters are not warned of apart from it. A parameter
  cannot be named `_`, `cannot use _ as a name`, so one a function
  ignores on purpose is warned of. Reusing a function's name is allowed.
- Expressions are literals, parameter and variable names, functions'
  names as values, calls, a call's `mut` arguments, parentheses,
  array and struct literals, indexes, slices, field selections, the infix
  operators `+ - * / %`, `+% -% *%`, `& | ^ &^ << >>`, `== != < <= > >=`
  and `&& ||`, and the prefix `!`, `-`, `-%` and `^`, with Go's
  precedence: the prefix operators bind tightest, then `* / % *% << >> &
  &^`, then `+ - +% -% | ^`, then
  comparisons, then `&&`, then `||`, each level left-associative, so
  `1 + 2 << 3` is 17 and `6 & 3 * 2` is 4.
  Arithmetic takes two integers and gives their common type: the type of
  both, or the wider when one widens to the other, as `int8 + int64` is an
  `int64`; `int8 + uint8` and `uint64 + int64` have no common type and are
  errors, and so are `unsafe_size_t + int64` and
  `unsafe_size_t + unsafe_uintptr_t` (see target-width integers). `-x`
  means `0 - x` and takes a signed integer; on an unsigned one it is an
  error, and `-%x` says the wrapping is meant. `+ - *` and `-x` on
  integers never wrap: each, and `x++`, `x--`, `x += v`, `x -= v` and `x
  *= v`, compiles only where the checker proves the exact result a value
  of its type, below 0 included for an unsigned one, from the operands'
  known values, the facts in force, or the relations in force (see
  division); a target-width type's must fit a 32-bit target. Anything
  else is an error that suggests the fixes, `a + b may overflow int64;
  bound a and b first, or use v, ok := a + b, or a +% b to wrap`, `a - b
  may go below 0 in uint64; check a >= b first, ...`, `x++ may overflow
  int64; use x +%= 1 to wrap, or v, ok := x + 1 and x = v when ok`. The
  checked form `v, ok := a + b` (also `a - b`, `a * b` and `-a`, the
  outermost operation only, with `=` too) gives the result and `true`, or
  0 and `false` when it does not fit, both operands evaluated, left
  first; a float's has none, `adding floats cannot fail, so it has no
  checked form; use a + b`, nor a step's, `v, ok := x++` being a parse
  error. The wrapping operators `+% -% *%`, `+%= -%= *%=` and `-%x`, on
  integers only, compute in two's complement at the type's width and need
  no proof: the largest value `+% 1` is the smallest, `-%` of the
  smallest signed value is itself, and an unsigned `0 -% 1` is the
  largest; two untyped constants are an error, `200 +% 100 wraps at its
  type's width, but both operands are untyped constants; give one a type,
  as in int64(200) +% 100`. The smallest signed value divided by -1
  overflows, so a signed division needs a proof too (see division), and
  its remainder by -1 is 0. Arithmetic whose operands are all
  known values (a constant, a character literal, an immutable variable
  holding one, `len` of an array or of a known string, a field or element
  of an immutable global, or arithmetic on those; see division) is
  computed at compile time: plain arithmetic must give a value of its
  type at every step, in a function as at package level, or it is an
  error, `constant expression overflows uint8`, covering the step that
  overflowed, while a wrapping operator wraps, so `var seed uint64 = a *%
  b` is a constant for known `a` and `b`. A target-width type is the
  exception above 4294967295: there, arithmetic on known values is an
  error only in a constant expression (see target-width integers), and
  anywhere else is not a known value, and a wrapping operator on one is
  never known. So `x := 'a' * 3` is an error, and so are `-m`, `m - 1`
  and `m / -1` after `var m int8 = -128`, while after `mut m int8 = 127`,
  `m + 1` needs a proof, which a fact from the declaration, `m == 127`,
  rules out, and `m +% 1` is -128.
  The bitwise operators `& | ^ &^` (and, or, exclusive or, and and-not,
  `a &^ b` clearing in `a` every bit set in `b`) take two integers under
  the same rules as arithmetic, giving their common type, so
  `int8 & uint8` is `mismatched types int8 and uint8 in &`, and work on
  a signed value's two's complement bits. `^x` flips every bit of an
  integer of any type: `^int8(0)` is -1 and `^uint8(0)` 255. None is
  defined on anything but an integer, `operator & is not defined on
  bool`. `x << n` and `x >> n` shift `x`, an integer, by `n`, which must be
  an unsigned integer of any width, `shift count must be an unsigned
  integer, got int64`, or an untyped constant that is not negative,
  `shift count must not be negative, got -1`; the result has `x`'s type,
  and an untyped `x` shifted by a typed count is an `int64`, so `1 << n`
  in a `uint64` needs `uint64(1) << n`. A count at or past `x`'s width
  shifts every bit out: `x << n` is 0, and `x >> n` is 0, or -1 when `x`
  is negative, as in Go, never C's undefined behavior. `>>` of a signed
  value is arithmetic, copying the sign bit in, and of an unsigned one
  logical, and `<<` of a value not known keeps the bits that fit the
  width, as `+%` does, needing no proof, so after `mut x int8 = 64`, `x
  << 1` is -128. Untyped constants are computed exactly, as Go's
  are, on their infinitely wide two's complement form: `^5` is -6, `-8 >> 1`
  is -4, and `1 << 64` is `constant expression is out of range`; on
  known values a shift that loses a set bit is `constant expression
  overflows uint8`, as `uint8(1) << 8` is, under the overflow rule above.
  Every one has a compound form, `&= |= ^= &^= <<= >>=`, whose value is
  the right operand of its operator. They give no fact for a proof (see
  division), though a known value through them is known, as `k & 3` of
  a known `k` is.
  A comparison of two integers takes any two integer types, the
  target-width ones included, with any of the six operators, and compares
  their values, with no common type needed: `int64(-1) < uint64(0)` and
  `uint64(18446744073709551615) > int64(9223372036854775807)` hold, and
  `int8(-1) == uint8(255)` does not. C differs: there, `-1 < 0u` is
  false, since the signed operand is converted to the unsigned type
  first. An untyped constant still
  takes the other operand's type and must fit it, so `u < -1`, for a
  `uint64` `u`, is `constant -1 overflows uint64`, as `var u uint64 = -1`
  is. Only comparisons compare across types: arithmetic, assignments and
  arguments still need the widening rule, so `a + b` after `a < b`, for an
  `int64` `a` and a `uint64` `b`, is still
  `mismatched types int64 and uint64 in +`. Any other comparison takes two
  operands of one type, after the same widening: two strings with any of
  the six, or two `bool`s, arrays, lists, structs or unions with `==` and `!=`, an array
  or a struct of integers needing the same element or field types, so
  `[2]int8` and `[2]uint8` do not compare. Strings compare byte by byte, each byte unsigned, as in Go: a
  proper prefix is less, so the empty string is least, a zero byte is a
  byte like any other, and a byte from 0x80 up sorts after every ASCII
  byte, so `"é" > "z"`. Nothing is allocated. Inside a function, a
  comparison of two string literals is made at run time; in a
  package-level variable's initializer, a comparison of known strings, or
  of arrays or structs holding them, is computed at compile time, so `var
  x = "a" == "a"` is `true`.
- Division never divides by zero, and never overflows. `a / b`, `a % b`,
  `x /= b` and `x %= b` compile only where the compiler can prove `b` is
  not zero; a constant zero is an error. A signed `a / b` and `x /= b`
  also need a proof that the division is not the smallest value of its
  type divided by -1, one past its largest: a known value, the bounds, or
  a fact of either operand ruling out `b` being -1, as `b != -1` or `b >
  0`, or `a` being that value, as `a > -128` for an `int8`. `b != -1`, or
  `b !=` the smallest value of a signed width, such as `-128`, rules that
  one value out, and counts for this proof alone. Once `b` is proven not zero, an unproven
  one is `a / b may overflow int64; check b != -1 first, or use q, ok :=
  a / b`, or for `x /= b`, `x /= b may overflow int64; check b != -1
  first, or use q, ok := x / b and x = q when ok`; on known values it is
  `constant expression overflows int8`. An unsigned division, and any
  remainder, never overflows: the smallest value `% -1` is 0. Elsewhere,
  `q, ok := a / b` (or `%`, or `q, ok = a / b`) divides only when `b` is
  not zero and, for a signed division, the division does not overflow:
  `ok` reports whether it divided, and `q` is the result, or `0`. The division must be the whole value:
  `q, ok := a / b + 1` is an error that suggests `q, ok := a / b`, then
  `q + 1`. Both operands are always evaluated. Its `ok` cannot be
  discarded, as an error cannot: `q, _ := a / b`, or `= a / b`, is `the ok
  a / b gives cannot be discarded; bind it to a variable instead of _`, and
  so is `_` in the `ok` of every checked form, an index, `m[k]`, a
  string's slice, a conversion, a type assertion, `pop`, `remove` and
  `delete`, each named as written: `the ok m[1] gives cannot be discarded;
  ...`; an `ok` bound to a variable nothing ever reads is too, `the ok
  m[1] gives is never checked: ok is not read`, or for `pop`, `remove`
  and `delete`, `the ok pop returns is never checked: ok is not read`. The value may be discarded, `_, ok := m[k]`, and both cannot be,
  `every name is _, so nothing is bound`. Where the
  divisor is proven not zero the two-value form is allowed too, and `ok` is
  always `true` (see [docs/vet.md](docs/vet.md)). A divisor is proven
  not zero when it is a non-zero constant, an immutable variable holding
  one, arithmetic whose bound rules out zero, as `x - 5` under `x > 5`
  (see indexing), or a variable a condition covers where it is used:
  - inside `if c { ... }` when `c` being true rules out zero, and in its
    `else` when `c` being false does;
  - in the statements after an `if` whose branch ends in `return`, `break`,
    `continue`, a call to `exit` or a proven `unreachable`, by what got past it, as in `if b == 0 {
    return 0 }` or `if b == 0 { exit(1) }`;
  - in a loop's body, by its condition, in a counting loop's body by
    the value its variable starts from (see three-clause loops), and in a
    whole loop by a bound it keeps on a variable it writes (see loops);
  - in a `switch`, as in the chain of ifs it stands for (see switches),
    though a type switch, whose cases are types, proves nothing;
  - on the right of `&&` by its left side being true, and of `||` by its left
    side being false;
  - in the statements after a call whose callee's contract ensures it of
    the argument passed for it (see contracts).

  The conditions that count are `&&` (when true), `||` (when false), `!`, and
  a comparison of the variable with a known value that excludes zero, such
  as `b != 0`, `b > 0`, `b <= -1` or `b == 3`. `||` when true, and `&&`
  when false, count for what every side agrees on: `b < 0 || b > 0`
  proves `b != 0`, and `i == 0 || i == 1` proves `i` between 0 and 1, but
  `b < 0 || x` proves nothing about `b`. A known value is a constant,
  an immutable variable holding one, `len` of an array or of a known
  string (a string literal, an immutable variable holding a known string,
  or a string field or element of an immutable global), a field of an
  immutable global or an element of one at a known index, as `origin.x`
  or `table[0]`, a byte of a known string at a known index, a conversion
  of a known value that fits its type, or arithmetic on those. The
  variable must be a
  parameter, a local, or an immutable global, or a field or element of an
  immutable variable or parameter (see structs), and no write to it in the
  region, a call passing it, or a part of it, as `mut` included, may reach
  the use. A write reaches the use when it runs before it: written
  earlier, or in a loop inside the region that holds the use too, whose
  next pass runs the use again with no check in between, so a loop that
  counts its divisor up inside the guard needs the check beside the
  division: `if d != 0 && n % d == 0`. A write that runs only after the use
  does not end the fact. That includes the call or the assignment the fact
  is for, since a call assigns its `mut` arguments only once they are all
  evaluated, and an assignment stores to its target only once its value
  is computed: `if d > 0 { d = n / d }` and `if len(l) > 0 { x := pop(mut
  l) }` compile, while a second `pop` after the first needs a check of its
  own. A proof an assignment's target needs must hold through the whole
  statement, so a write in the value, or, in an assignment of several, to
  another target, ends it: `l[i] = bump(mut i)` and `i, l[i] = two()` are
  both `index i may be out of range; check 0 <= i && i < len(l) first`, as
  is `l[i], i = two()`, and such a value is computed into a variable
  before the index is proven. It includes a write later in a loop's body when the check runs
  again before the next use, as the loop's condition, or a guard in the
  body before the use, does: in `for 0 <= i && i < 3 { use(a[i]); i++ }`
  the condition proves `a[i]` on every pass. Deferred code is judged
  where it runs, at its block's end, and a `default` written above other
  cases runs after their values are tried. The region is everything that can
  run after the comparison and before the use, the rest of the condition
  included: in `if d != 0 && reset(mut d)` the body knows nothing about
  `d`, nor does the code after `if d == 0 || reset(mut d) { return }`,
  while `reset(mut d) && d != 0` proves `d != 0`, since the call runs
  first. After an early exit the region also takes in the branch that
  does not leave, and after a `switch` case that leaves, every later
  case's values and every later body that does not leave; a case's
  values tried after one count as well. A tag is evaluated once, before
  any value, so in `switch d` what a case proves about `d` also needs
  every value tried up to that case to leave `d` unassigned. A counting
  loop's start is not tried again before a later pass, so it holds only
  while neither the condition, which runs again before every pass, nor
  the body assigns the variable anywhere, and a range loop's index bound,
  which holds of what the loop copied when it began, only while the body
  never assigns what it ranges over. A write in a branch or a case that
  leaves (one ending in a `switch` leaves here only when the `switch` has
  a `default` and every case leaves: a `switch` on a union naming every
  case, which leaves as an early exit, does not count, since whether it
  names every case is known only once it is checked), anywhere in the
  region, never reaches code outside that `if`
  or `switch`, after it or before it, since only paths that did not take
  it run there: in `for 0 <= i && i < len(s)`, a branch that writes `i`
  and ends in `continue` leaves the condition's proof of `s[i]` standing
  below it. A `break` or `continue` in it that lands in a loop inside the
  region, though, lets the write reach everything from where it lands
  on: the code after that loop, for a `break` out of the outermost loop
  in the region, so `for range 3 { use(a[i]); if k == 1 { i = 99; break } }`
  keeps its proof of `a[i]` but an `a[i]` after the loop does not; and
  that outermost loop's next pass, and so all of it, otherwise. A
  `return` still runs deferred code, so a write before it ends a proof
  that deferred code relies on. This does not
  apply to a counting loop's start or a range loop's bound: a write in
  the body ends them wherever it is, in a branch that leaves too, as
  `if k == 1 { i = -5; continue }` in `for mut i := 0; i < len(a); i++`
  ends the proof of `i >= 0`.
  A call to `exit` counts as leaving,
  since it never returns, and so does a branch, a block or a case ending
  in one; a call to a function that calls `exit` does not. When a guard says something about a divisor, index or converted value it did not
  prove, the error comes with notes pointing at the guard and what it
  proves, at the assignment or `mut` argument that ended that proof, or at what the guard
  allows that the operation does not, such as `i == 3` for a `[3]T`, and
  at a counting loop's step that could leave its type, or
  a guard on a field of a mutable variable. A mutable global's error suggests only the two-value
  form, with a note at its declaration, since no check proves it. [docs/safety.md](docs/safety.md) gathers these rules, the
  two-value form and every operation they apply to in one place, and
  [docs/vet.md](docs/vet.md) the checks a future `tack vet` is to make,
  such as a two-value form whose `ok` is always `true`.
- Contracts. A function may state, after its results and before its
  body, facts it ensures about its parameters whenever it returns:
  `func must(err error) ensures err.code == 0`, `func nonzero(d int64)
  ensures d != 0`, `func inside(i int64, l []int64) ensures 0 <= i && i
  < len(l)`, `func need(m map[int64]int64, k int64) ensures has(m, k)`.
  `ensures` is a keyword. The clause states what an `if`'s condition
  proves, joined by `&&`, and nothing more: comparisons of the
  parameters, a method's receiver included, their fields and elements,
  constants, `len` and `has`, with `!`, `&&`, `||`, `+` and `-`, so a
  divisor not zero, bounds and the relations between values and lengths
  (see division and slicing), an error's code and a key in a map. Each
  operand of its `&&` must give a fact, or it is `d != 3 gives no fact;
  an ensures clause states facts an if's condition proves, such as d !=
  0, 0 <= i && i < len(a), err.code == 0 or has(m, k)`. A name that is not
  a parameter is `an ensures clause can name only the parameters of f,
  and limit is not one`, an own parameter, whose argument the call
  moves, `an ensures clause cannot name own parameter l: its argument
  moves into the call, so nothing it ensures about l reaches the
  caller`, a call `an ensures clause can call only len and has, not
  size`, and anything else, such as `*`, `an ensures clause cannot hold d
  * 2; it compares parameters, their fields and elements, constants, len
  and has, with !, &&, ||, + and -`; a clause that is no `bool` is
  `ensures clause must be a bool, got int64`, and an extern func's,
  whose body nothing proves, `extern func pick cannot have an ensures
  clause; its body is written in C, where nothing proves it`. As in an
  `if`, a struct literal in it must be in parentheses.

  The compiler proves the clause on every path that returns, from the
  facts in force there: at each `return`, once its values are computed,
  `f ensures d != 0, which may not hold at this return`, and at the end of
  the body when it can be reached, `f ensures d != 0, which may not hold
  at the end of its body`, each with a note at the clause. A branch
  ending in `exit` leaves, as everywhere, so `if d == 0 { exit(1) }`
  proves `d != 0` where the function returns, and code after a
  statement that leaves, or ends in `exit`, never runs. Deferred code runs after the
  return, so a defer written before it whose code assigns a parameter
  the clause names, or passes it as `mut`, fails the proof, with a note
  at the write, `but d is assigned here, in deferred code, which runs
  after f returns`. The proof follows writes as any fact does, so a `mut`
  parameter assigned, a list popped or a key deleted after the check,
  or a write on a loop's next pass, ends it.

  A call that is a statement, or the whole value of a declaration or an
  assignment, of one value or several, gives the statements after it in
  its block the facts the contract states, each parameter replaced by the
  argument passed for it, as an early exit's facts are given. A fact
  counts only when every parameter it names was passed a local variable
  or a parameter of the caller by name: as `mut`, which the callee
  changed in place, or as a plain argument nothing else in the call
  writes, so `f(mut i, i)` tells nothing of `i` through the plain one. A
  field, an element, an expression, a constant, a variadic parameter's
  values, an array passed as a view, a variable the statement assigns,
  `d = fixed(d)`, and a call through a function value, whose type says
  nothing of a contract, give none; a path below a variable counts only
  below an immutable one, or as an error's code. A write anywhere in the
  statement's targets drops its variable too, `arr[zero(mut d)] = fix(mut
  d)`. A field or an element of a `mut` parameter carries no facts, so a
  clause about one is `len(s.items) > 0 gives no fact; s.items is part of
  the mut parameter s, and a field or an element of a mut parameter
  carries no facts`: a method with a `mut` receiver ensures nothing about
  its own fields. A `return` whose one value is a call to a function with
  a contract, `return fix(mut d)`, proves with that call's facts, and a
  function made generic by an interface parameter, `func emit(mut w
  Writer, d int64) ensures d != 0`, has a contract as any other, though a
  clause calls none of its methods. The facts hold after the
  whole statement, never inside it, so `use(nonzero(d) + 10 / d)` is not
  proven, and, as any fact, until a write to the variable can reach the
  use. Recursion, generic functions and methods, and calls across
  packages work alike; a contract costs nothing at run time.
- Nesting limit. A program nests at most 1000 levels deep, counted
  together across expressions, statements and types: a parenthesized
  expression, a `!` or `-` and its operand, `mut` and its argument, a
  call's arguments, an index's or a slice's brackets, a literal's braces,
  the block of an `if`, a `for` or a `defer`, a `switch`'s cases, and an
  array, view or list type each open one level, inside the function body
  or declaration they are in. A level more is `expression nested too
  deeply: more than 1000 levels`, reported once, at the token that opens
  it, and parsing stops there. A chain of binary operators binds left to
  right and nests no deeper for its length, so `x + x + ... + x` has no
  limit, while one nested on the right, `x && (x && (...))`, opens a
  level per parenthesis; an `else if` is a branch of its chain, not a
  level, so a chain of any length is flat. The limit keeps every stage
  of the compiler, and the C it writes, within what clang compiles: past
  about a thousand levels of nesting, clang's parser runs out of stack.
- Evaluation order is left to right wherever Tack evaluates several
  operands: the two sides of an operator, a call's arguments, a return's
  values, an array literal's elements, a struct literal's values in the
  order they are written, a function value before the arguments of a
  call through it, an indexed array before its index, a sliced
  value before its bounds and the low bound before the high, and a
  switch's tag before its case values, which run one by one up to the
  first match. In an assignment the target's indexes come before the
  value, and `x op= v` reads `x` before it evaluates `v`. The order shows
  only through calls, which can print, exit, assign a mutable global
  another operand reads, or change a variable another operand reads
  through a `mut` argument: `f() + g()` always runs `f` first, in
  `g + bump()`, with `bump` adding one to the global `g`, `g` is read
  before the call, and in `three(next(mut k), k, next(mut k))`, with
  `next` returning `k` and adding one to it, the middle `k` is read
  between the two calls. A `mut` argument stands for its place, not a
  value read from it: the callee sees the variable as it is when the call
  begins, after every argument has been evaluated.
- `&&`, `||` and `!` take `bool` operands. `&&` and `||` short-circuit: the
  right side runs only when the left does not decide the result, so
  `n != 0 && 10 / n > 1` never divides by zero. A package-level initializer
  is computed in full, so an error on either side is reported.
- The types are the integers `int8`, `int16`, `int32`, `int64`, `uint8`,
  `uint16`, `uint32` and `uint64`, the target-width integers
  `unsafe_size_t` and `unsafe_uintptr_t`, the floats `float32` and
  `float64`, `string`, `bool` and `error`, arrays,
  views, which only a parameter or a result may have, and `any`, which
  only a parameter may have, the structs and
  unions a program declares, and, within a generic function, its type
  parameters.
  There is no
  `int`, and no `byte`. A bool is `true`, `false`, or the result of a
  comparison.
- Target-width integers. `unsafe_size_t` and `unsafe_uintptr_t` are C's
  `size_t` and `uintptr_t`, for crossing to C (see Extern C): unsigned
  integers as wide as the target makes them, somewhere from 32 to 64 bits,
  and not necessarily as wide as each other. The `unsafe_` prefix is the
  only marker; they may be used anywhere any other integer type can,
  `pub` and `lib` signatures included, and have every operation an
  unsigned integer has, the wrapping operators wrapping at their real
  width, and plain arithmetic proven to fit a 32-bit target. Their
  rules hold at every width, so a program valid on a 64-bit target stays
  valid on a 32-bit one. `uint8`, `uint16` and `uint32` widen into either
  of them, and either widens into `uint64`; nothing else widens into or
  out of them, no signed type, not `int64`, and not one into the other.
  They still compare with every integer type, each other included, by
  value, as every integer does.
  A constant of one, a literal or any step of a constant expression made
  of literals, conversions and operators, or a package-level variable's
  initializer, must lie between 0 and 4294967295, the most every target
  holds: `var s unsafe_size_t = 4294967296` and
  `unsafe_size_t(4294967295) + 1` are errors,
  `constant 4294967296 overflows unsafe_size_t: its width is unknown, so a constant must be at most 4294967295`.
  A known value past 4294967295 elsewhere, such as `s + 1` after
  `s := unsafe_size_t(4294967295)`, is not an error but is not known
  either, and is computed at run time. Either may index an array, a
  string or a view, proven or checked as a `uint64` index is (see
  indexing): `if s < len(a)` proves `a[s]` on every target. `len` stays
  an `int64`.
- Arrays. `[n]T` is an array of `n` elements of type `T`, where `n` is a
  known value (see division), such as a constant expression, an immutable
  variable holding one, `len` of an array or of a known string, or a field
  of an immutable global. In a struct field's type, `n`
  names no variable (see structs). `[3]int64{1, 2, 3}` is a literal,
  which lists exactly one element for each of the array's, and
  `[...]int64{1, 2, 3}` takes its length from its elements. A literal
  inside another may leave out its type: `[2][2]bool{{true, false},
  {false, true}}`, or a struct literal its own (see struct literals).
  Elements have no keys: `[3]int64{0: 1}` is an error. An array is a value: assigning, passing or returning one
  copies it, and `mut a [3]int64` starts with every element zero. An array
  follows its variable, so `a[i] = v` needs `a` to be `mut`, and assigning
  an element does not count as reading `a`. Two arrays of one type compare
  with `==` and `!=`, element by element, strings included; `<` on two
  arrays is an error. Arrays may be package-level variables, initialized
  with a constant expression (see package-level variables). An array
  passes to a view parameter, `[]T`, without a copy (see views). An
  array, like any value, holds at most 2^31 bytes (see size limits).
- Size limits. No value holds more than 2^31 bytes, whatever its type and
  wherever it is: a global, a local, a parameter, a result, a field, an
  element of an array or a list, a union's payload or a type argument.
  The size is that of the C object the backend makes of the value,
  nested arrays and structs included, with C's padding, and with its
  lowering: `[0]T` holds one element and an empty struct one byte. Sizes
  are those of the 64-bit targets the backend builds for, arm64 and
  x86-64, where a pointer, an `int64` and an `unsafe_size_t` are 8 bytes,
  aligned to 8; a 32-bit target would change them. `mut a
  [2147483648]uint8` is at the limit; `[2147483649]uint8` is `array type
  [2147483649]uint8 is too large: it holds 2147483649 bytes, more than the
  2^31 a value may hold`, and a struct or union made too large by its
  fields or payloads together, each within the limit, is `type S is too
  large: it holds 2147483656 bytes, more than the 2^31 a value may hold`.
  A size too large to count, as `[9223372036854775807][2147483648]uint8`'s
  is, holds `more than 18446744073709551615` bytes. A type is reported
  once, where it is made too large, and not at its uses, nor at a type
  holding it. All of a program's globals together, those of every package
  and of the standard library's Tack included, hold at most 2^30 bytes:
  the global that takes them past it is reported, `global c makes this
  program's globals hold 1610612736 bytes, more than the 2^30 a
  program's globals may hold`. Globals are counted package by package,
  every package after the ones it imports and the main package last,
  each package's files in the byte order of their names, and in source
  order within a file; only Tack globals count, not the C statics of
  std's C files. The limits keep a program within what clang's default
  code models link, and clang's own ceiling on an object is 2^60 bytes.
  On x86-64 code reaches static data RIP-relative, within 2 GiB: exactly
  2^31 bytes of statics fail to link there, `32-bit RIP-relative
  reference out of range`, so a program's globals get 2^30, half that,
  leaving room for the code and the C library's statics. arm64 reaches 4
  GiB, and a 4 GiB global beside any other static fails to link there at
  `-O0`. More data goes in a list. The C objects the backend
  makes of several values at once, the results of a function with
  several and the arguments a variadic call gathers, are not values and
  are not counted; they live on the stack.
- Indexing. `a[i]` is an element of the array `a`, and `i` an integer of any
  type, a target-width one included. An index is never out of range at runtime. `a[i]`, and an
  assignment to it, compile only where the compiler proves `i` in range: a
  constant, checked against the length at compile time, or a variable
  known to lie between 0 and the length less one. Elsewhere,
  `v, ok := a[i]` (or `v, ok = a[i]`) reads the element only when `i` is in
  range: `ok` reports whether it was, and `v` is the element, or its type's
  zero value; where `i` is proven in range it is allowed too, and `ok` is
  always `true`. An assignment to
  an element has no such form, so its index must be proven. What proves an
  index is what proves a divisor, below, with bounds as well as "not zero":
  a range loop's variable lies between 0 and its count less one, or the
  length of the array it ranges over less one, a
  comparison with a known value bounds a variable, as in
  `if 0 <= i && i < len(a)`, and a variable's type bounds it too, so a
  `uint8` index into a `[256]T` needs nothing. Relations between two
  values prove an index too, and a slice's bounds, though never a
  divisor: a comparison of two values that are not both known, each a
  variable, a length or a known value plus or minus a known amount,
  says how far apart they are, `i < n` that `i - n <= -1`, and
  relations chain, so `0 <= i && i < n && n <= len(a)` proves `a[i]`.
  A declaration or a plain assignment of such a value relates what it
  stores to it for the rest of its block, so `k := j - 1` is one less
  than `j` and `n := len(a)` is `len(a)`, until a write to either can
  reach the use. A statement storing a known value in a mutable
  variable, `mut n := 0` or `n = 5`, or a value of known bounds, `n++`
  included, puts the variable within them for the rest of its block,
  until a write to it can reach the use, and relations chain through
  those bounds: under `mut n int64 = 2`, `0 <= i && i < n` proves `a[i]`
  of a `[3]T`, and `3 <= j && j <= len(a)` proves `a[n]`. A chain counts
  the bounds only while the rest of the block holds at most 16 writes to
  the variable, a closure's included; with 17 or more, they prove only
  what they prove of the variable alone. A
  relation through `x + c` or `x - c` counts only where the other facts
  in force keep the step within `x`'s type, which the step needs to
  compile anyway: in `0 <= i && i + 1 < len(a)`, `i + 1` is an error, as
  it overflows for the largest `int64`, while `i < len(a) && i + 1 <
  len(a)` relates `i + 1` to `len(a)`; for an `int8` `i`, it takes a
  bound like `i < 100`. `len(a) - c` never overflows. `||` keeps no
  relation, a product such as `2 * i + 1` is none, and a query follows
  at most 64 related values. Arithmetic is proven too: an index, a
  divisor, a slice's bound or a conversion's operand that is `+`, `-` or
  `*` on two integers, or unary `-` on one, each operand a known value, a
  variable, a field or element of an immutable variable, `len` of a
  string, view or list variable, or such arithmetic itself, is bounded
  by its operands' bounds, worked out exactly: under `0 <= i && i < 4 &&
  0 <= j && j < 4`, `a[i + j]` of a `[7]T` and `a[2*i + 1]` of an `[8]T`
  compile. A length alone is bounded as it is in arithmetic, so it is
  proven wherever the same length plus 0 is: under `len(s) < len(t)`,
  `t[len(s)]` compiles, and under `len(s) < 3`, so does `a[len(s)]` of a
  `[3]T`; a write to the string, view or list ends its length's facts. An operation that could leave its type, for some values its
  operands' bounds allow, gives no bound, nor does any arithmetic holding
  it, and is an error itself: under `v >= 0`, `v + 1` overflows for the
  largest `int64`, and an operation of a target-width type counts only
  where it fits at 32 bits. Each operand is
  bounded on its own, so `x - x` is not known to be 0. Any other
  operand, a call, a conversion, `/`, `%`, a shift or a bitwise
  operator, gives no bound. Arithmetic that adds a known amount to one
  value, as `i + 1` or `len(a) - 1`, is related to other values too,
  where the facts keep it within its type: `0 <= i && i < len(a) - 1`
  proves `a[i + 1]` of a view, and `len(s) > 0` proves `s[len(s) - 1]`.
  See [docs/safety.md](docs/safety.md) for every proof and diagnostic.
- Indexing a string. `s[i]` is the byte of `s` at `i`, a `uint8`, under
  the same rules: it compiles only where `i` is proven in range, and
  `v, ok := s[i]` (or `v, ok = s[i]`) reads it otherwise, giving `0` and
  `false` out of range, and gives `true` where `i` is proven. A string's
  bytes cannot be assigned, even in a `mut` string: `s[0] = 65` is an
  error, since strings are immutable. A constant index into a known
  string, a literal or an immutable variable holding one, is checked at
  compile time, so `"tack"[1]` is `97`, `"tack"[4]` an error, and after
  `s := "abc"`, `s[0]` needs no proof and `s[5]` is an error; a negative
  constant is always an error. The length of any other string variable,
  a parameter or a `mut` one, is not a constant, so a proof names it:
  `for i := range len(s)` and `for i := range s` prove `s[i]` in the body,
  `if 0 <= i && i < len(s)` in the branch (or, as for arrays, after
  `if i < 0 || i >= len(s) { return }`), and a bound on the length proves a
  constant index, as `if len(s) > 0` proves `s[0]`, and so do
  `len(s) >= 1` and `len(s) != 0`. These hold only where neither the index
  nor the string is assigned; the string may be `mut`. A mutable
  package-level string never gets a proof. An unsigned index needs only
  the upper bound. Any integer index compares with `len(s)` or `len(a)`,
  an `int64`, by value (see comparisons), so for a `uint64` `u`,
  `if u < len(s)` proves `s[u]`, and the same holds for an
  `unsafe_size_t` or `unsafe_uintptr_t` `u`, whatever the target's width.
- Slicing. `x[i:j]` is the bytes of the string `x`, or the elements of the
  array, view or list `x`, from `i` up to, but not including, `j`, as in Go;
  `x[:j]` starts at 0, `x[i:]` runs to `len(x)`, and `x[:]` is all of it.
  There is no three-index form: `x[i:j:k]` is `a slice has at most two
  bounds, x[i:j]; there is no three-index form`. A string's slice is a
  `string`, `{ptr + i, j - i}`, sharing the bytes it was cut from and never
  copying them, so it is a value like any other string: stored in a
  variable, a field, an array, a global's initializer when computable, or
  returned. A string slices by bytes, so a slice may cut through a
  multi-byte UTF-8 character. An array's, a view's or a list's slice is a view, `[]T`
  of its element type, under the rules for views: it can only be passed
  to a view parameter, `sum(a[1:3])`, an extern func's included, spread
  into a variadic one, `total(a[1:3]...)`, given to `len`, or sliced
  again, or returned from a function returning a view (see view
  results); anywhere else, stored, indexed, ranged over or compared, it is
  `a[1:3] is a view, []int64, which can only be passed to a view parameter
  or to len`. An array must be a place, as for a
  view: `f()[1:]` is `cannot slice a temporary array; store it in a variable
  first`, and so is a list, `cannot slice a temporary list; store it in a
  variable first`; a known bound never fits a list whose length is
  unknown, so `l[-1:]` is `slice bound -1 is out of range for a list`. A slice of a mutable global array, or of an array the same call
  passes as `mut`, is of a copy taken when the argument is evaluated, as a
  view of it would be; a string's slice never copies. Anything else
  sliced is `cannot slice int64`, and a bound must be an integer, `slice
  bound must be an integer, got bool`, of any integer type, a target-width
  one included. The bounds must be proven, as an index is: `0 <= i`, `i <=
  j` and `j <= len(x)`, from known values, a variable's type, guards and
  loops, with two more relations a slice's guard may give, `j <= len(x)`
  and `i <= j`, and a bound may be arithmetic, as an index may, so
  `s[i : i + 1]` compiles under `0 <= i && i < len(s)`, or the length of
  another string, view or list, proven wherever that length plus 0 is,
  so `t[:len(s)]` compiles under `len(s) <= len(t)`; a bound left out
  needs nothing of its own, so `x[:j]` needs
  `0 <= j <= len(x)` and `x[i:]` `0 <= i <= len(x)`. Unproven, the error
  names what is missing, `slice bounds i:j may be out of range; check i <=
  j first, or use v, ok := ...[i:j]`. A string's slice may instead be
  bound with ok, `t, ok := s[i:j]` (or `=`), which gives the slice and
  `true`, or `""` and `false` when the bounds are out of range, comparing
  their values whatever their types; an array's or a view's cannot, `a[1:]
  is a view, []int64, which cannot be bound with ok; prove its bounds
  instead`. A known bound that no length makes valid is an error in either
  form: negative, past a known length, `slice bound 4 is out of range for
  [3]int64` or `for a string of 3 bytes`, or past the other known bound,
  `slice bounds 3:1 are out of order`. A slice is never a place: `s[0:1] =
  t`, `a[1:][0] = 3`, `a[:]++` and `mut a[1:]` are `cannot assign to
  s[0:1]: a slice is not a place` or `cannot pass mut a[1:]: a slice is
  not a place`. The slice of a known string at known bounds is a known
  string, so `k := "abcdef"[1:4]` makes `len(k)` 3 and `k[2]` `'d'`, and
  `len(x[i:j])` is known to be `j - i` wherever `i` and `j` are known. `x`
  is evaluated first, then `i`, then `j`, each once. See
  [docs/safety.md](docs/safety.md#slicing).
- `len(a)` is the number of elements of an array, a constant, `len(s)`
  the number of bytes of a string, a known value for a known string (see
  division), `len(v)` the number of elements of a view or a list, never
  known but for a slice at known bounds, and `len(m)` the number of keys
  of a map, never known and carrying no facts, `len(a)` of an arena its
  elements, its sentinel among them, and `len(p)` of a pool its live
  elements, neither known; each is an `int64`. Anything else is `len
  expects an array, a string, a view, a list, a map, an arena or a pool,
  got int64`.
- A value of one integer type can be used where another is expected, with
  no conversion, when every value of the first is a value of the second: a
  signed type in a wider signed one, and an unsigned type in a wider type of
  either kind. For a target-width type that must hold at every width it
  can have: `uint8`, `uint16` and `uint32` in either, and either in
  `uint64`, but neither in the other. That holds for assignments, arguments, results and the
  operands of an arithmetic operator alike. A comparison needs no such
  type: two integers of any types compare by their values (see
  comparisons). Any other change of type is a conversion.
- Conversions. `T(x)` converts the integer `x` to the integer type `T`, and is
  allowed only where it cannot fail: `x` is a constant, or a known value (see
  division), that is a value of `T`, as `uint8(x)` after `x := 5` or
  `int8('a')`, `x` is of a type that widens to `T`, or `x` is a name, a
  field or element of an immutable variable, `len` of a string, view or
  list one, or arithmetic on those (see indexing), that the facts in
  force keep within `T`'s range, as an index's are kept within a length:
  `uint64(v)` under `if v >= 0`, `int32(v)` under
  `if -2147483648 <= v && v <= 2147483647`, `int64(u)` for a `uint64` `u`
  under `if u <= 9223372036854775807`, `uint8(len(s))` under
  `if len(s) <= 255`, and `uint64(len(s))` always, a length never being
  negative. The facts come from the same guards,
  early exits, loops, cases, contracts and relations as an index's, and
  the same writes end them; a field of a mutable variable has none.
  Arithmetic is bounded as an index's is, so `uint64(-(v + 1))` is
  proven under `v < 0`, while `uint64(-v)` is not, as `-v` overflows for
  the least `int64`, an error of its own. Unproven, `T(x)` of a
  name is `converting int64 to uint64 can fail; check v >= 0 first, or
  use v, ok := uint64(...)`. A constant or known
  value that is not a value of `T` is an error, as `uint8(x)` after
  `x := 300`. For a target-width `T`, a value of `T` is one from 0 to
  4294967295, a value on every target, for a known value and for facts
  alike; a known value past that but
  within 64 bits fits on some targets only, so converting it needs the
  two-value form, which no fact changes. Every other conversion is written
  `v, ok := T(x)` (or `v, ok = T(x)`): `ok` reports whether `x` is a value of
  `T`, and `v` is that value, or `0` when it is not; for a target-width `T`,
  whether it is one on the target the program runs on. Where the conversion
  cannot fail the two-value form is allowed too, and `ok` is always `true`;
  a constant or known value that does not fit is an error in either form.
  See [docs/safety.md](docs/safety.md). Strings and bools cannot be converted to or from a number; a string's only conversion is `[]uint8(s)`, a view of its bytes (see views). `true` and `false` are keywords, so neither can be
  used as a name, and both are constant expressions, as is `noerror`, a
  keyword too (see errors).
- `print` takes one string and writes exactly its bytes to standard output,
  adding no newline; a line ends where the string has a `\n`.
- `exit` takes an `int32`, or an integer that widens to one
  (`int8`, `int16`, `uint8`, `uint16`), and terminates the process with that
  status, without running any pending defer. It never returns, so a call
  to it ends its block as a `return` does: the facts after an `if` whose
  branch ends in it, a function's last statement, and what a branch that
  ends in it moved, which the code after the branch never sees. Before the process ends,
  every allocator frees every buffer it still holds, so every list still
  live in any frame is freed, though no code of the program runs to do
  it. An `int64`, `uint32` or
  `uint64` status is converted first: `exit(int32(x))` where `x` is a
  constant or known value that fits, and otherwise the checked form,
  `v, ok := int32(x)`. A constant outside `int32` is an error, as
  `exit(3000000000)` is. Only the low bits of the status reach the
  operating system, the low 8 on Linux and macOS, so `exit(70000)` ends
  with status 112.
- Integer constants. A literal, and any expression made only of literals and
  operators, is a constant with no type of its own: it is computed exactly,
  and takes the type of where it is used, the type it is assigned, passed or
  compared to, or `int64` where nothing gives it one. It must be a value of
  that type: `var b uint8 = 256` and `x := 18446744073709551615` are errors,
  and so is `var s unsafe_size_t = 4294967296`, past the most a target-width
  type holds on every target.
  Every literal, and every step of a constant computation, must lie between
  the smallest `int64` and the largest `uint64`; beyond that is an error
  rather than a larger number. A `-` right before the digits is part of the
  literal, so the smallest int64 is written `-9223372036854775808`, or
  `-0x8000_0000_0000_0000`. A literal is decimal, `255`; hexadecimal after
  `0x` or `0X`, `0xFF`, either case of digit; binary after `0b` or `0B`,
  `0b1111_1111`; or octal after `0o` or `0O`, `0o377`. A `_` may stand
  between two digits, or right after a prefix, to group them,
  `1_000_000`, `0x_FF`; anywhere else it is `_ must separate successive
  digits in 1__0`. A prefix with no digit is `hexadecimal literal 0x has no
  digits`, and a digit its base lacks `invalid digit 2 in binary literal
  0b102`; a prefixed literal takes every letter and digit after it, so
  `0x1g` is one bad literal, not `0x1` and a name. A decimal literal has
  no leading zeros: `0` is fine, but `007`, `00` and `0_7` are errors,
  unlike C and Go, where `010` is octal 8, so `0o` is the one octal form.
- Floats. `float32` and `float64` are IEEE 754's binary32 and binary64,
  with no fast-math. A float literal is decimal, as in Go: `1.5`, `.5`,
  `1.`, `1e9`, `1.5e-3`, `1E+2`, with `_` between digits as in an integer,
  `1_000.000_5`; an exponent needs a digit, `exponent has no digits in 1e`.
  There are no hexadecimal floats. A float literal is an untyped constant,
  computed exactly, as a rational number, until it is used, so `0.1 + 0.2
  == 0.3` holds, and becomes a `float64` where nothing gives it a type,
  `x := 1.5`; given a type, it rounds to that type's nearest value, and one
  too large for it is an error, `constant 1e+39 overflows float32`. An
  untyped integer constant can be used as a float, `f * 2`, but an untyped
  float constant never as an integer, not even `2.0`. A constant is held
  in at most 8192 bits of numerator and of denominator: a literal past
  that, `1e3000`, is `float literal 1e3000 is too large or too precise to
  hold exactly`. `+ - * /`, unary `-`, and the six comparisons work on
  floats, and `+= -= *= /=` on a float variable; `%`, the bitwise operators,
  shifts, `++` and `--` do not. Both operands of a float operator must have
  the same type: there is no implicit conversion between integers and
  floats, nor between `float32` and `float64`, so `f + i` is `mismatched
  types float64 and int64 in +`. Dividing a float by zero needs no proof:
  it gives an infinity, or NaN for `0.0 / 0.0`, and never stops the
  program, though a constant division by zero is still an error, as for
  integers; so a float division has no checked form, `dividing floats
  cannot fail, so it has no checked form; use a / b`. Comparisons follow
  IEEE 754: NaN equals nothing, itself included, and `-0.0` equals `0.0`,
  in a struct's, an array's or a union's `==` too. A float carries no
  facts and cannot index, and a float, or anything holding one, cannot be a
  map key: `cannot use float64 as a map key: a float is not a key`. Floats
  may be fields, elements, payloads, list and map elements, parameters,
  results and globals; a global's initializer must be a constant
  expression. An integer of any type converts to a float, and a float to
  the other float type, in the plain form, `float64(i)`, `float32(d)`,
  which never fails but may round: to the nearest value, ties to even, as
  clang does, so `float64(9007199254740993)` is `9007199254740992.0`, and
  a `float64` too large for a `float32` becomes an infinity. A float
  converts to an integer only in the checked form, `v, ok := int64(f)`,
  which truncates toward zero, `2.9` giving `2` and `-2.9` giving `-2`,
  and gives `0` and `false` for NaN, an infinity, or a value whose
  truncation the integer type cannot hold; floats carry no facts that
  could prove the plain form, so `int64(f)` is `converting float64 to
  int64 can fail; use v, ok := int64(...)`. The test is exact at every
  edge: `int64(9223372036854775808.0)` fails, the float64 below it
  converts, and `-9223372036854775808.0` converts. A constant converts
  exactly or not at all, as in Go: `int64(2.0)` is `2`, `int64(2.5)` is
  `cannot convert constant 2.5 to int64: it is not an integer`, and
  `int8(300.0)` is `constant 300 overflows int8`. Neither a float nor a
  bool converts to the other. A float can be passed as `any`, with type
  ids of its own, 25 for `float32` and 26 for `float64`, and so can a view
  of one, 27 and 28, and a type switch or a type assertion on an `any`
  matches `float32` and `float64`. A float crosses to C as C's `float` or
  `double`, by value, alone or in a struct, an array or a view, and as a
  pointer for a `mut` parameter, and what C gives back may be NaN or an
  infinity, which Tack's floats hold as IEEE 754 says. `fmt.printf`
  writes a float as Go's `fmt` does: `%v` and `%g` as the shortest decimal
  that reads back as the same float, with an exponent when its decimal
  exponent is below -4 or 6 or more, `0.1`, `1e+06`, `5e-324`; `%e` with
  six digits after the point, `1.000000e-01`; `%f` with six, `0.100000`;
  NaN as `NaN`, the infinities as `+Inf` and `-Inf`, and negative zero as
  `-0`. The digits are worked out exactly, with no C library and no
  locale (see conv).
- Passing the wrong number or type of arguments is a type error, so a call
  cannot silently discard what it was given: `f expects 2 argument(s), got
  3`, or for a variadic function, which takes any number past its other
  parameters, `f expects at least 1 argument(s), got 0` (see variadics).
- Identifiers are ASCII letters, digits, and underscores, and may not start with
  a digit. The type names, `int8` through `uint64`, `unsafe_size_t`,
  `unsafe_uintptr_t`, `string`, `bool` and `error`, are
  reserved like keywords: they can be written only as types and in
  conversions, never declared, read or assigned as names, nor used as a
  struct's or a union case's name; a field or a method, written only
  after `.` or as a literal's key, may take one. `_` on its own is
  not a name: it only discards a result in a
  binding of several, as in `_, r := divmod(7, 2)` or `q, _ = divmod(7, 2)`,
  and may appear there more than once, or one of a range loop's two
  variables, as in `for _, x := range a`. It cannot be declared, read or
  assigned anywhere else, cannot be `mut`, and a binding whose every name is
  `_` is an error, since the call can be made on its own, or the loop
  written without names; for a call that returns an error, it is the
  error discarded (see errors).
- String literals may contain any bytes except a newline or a double quote,
  and are emitted as raw bytes, so UTF-8 passes through unchanged. A
  backslash starts an escape: `\a \b \f \v \n \t \r \\ \"`, as in Go, or
  `\x` and exactly two hex digits, upper or lower case, for the byte they
  give, so `"\x00\xff"` is a zero byte and a 0xff byte. Any other escape is
  an error, `Invalid escape sequence "\\q"`, and so is `\x` with fewer than
  two hex digits after it, `Invalid escape sequence "\\x4"`; `"\x410"` is
  `A` and then `0`.
- Raw string literals, written between backquotes, are raw, as in Go:
  every byte up to the closing backquote is taken as it is, so
  `` `\n` `` is a backslash and an `n`, and `` `\` `` is one backslash.
  Nothing is decoded, a raw string may span lines, and it cannot hold a
  backquote. Carriage returns are dropped from its value, as in Go, so
  the value of one spanning lines does not depend on the line endings it
  was saved with. A raw string with no closing backquote is an error.
- A string is a sequence of bytes, everywhere: `s[i]` is a `uint8`,
  `len(s)` counts bytes, a character literal is one byte, `range` over
  a string gives bytes, and `s[i:j]` slices bytes. Decoding UTF-8 into runes will
  be a facility of the standard library, not language syntax.
- Character literals. `'a'` is a constant of type `uint8`, the value of
  its one byte, with the escapes a string has (`'\n'`, `'\\'`, `'\xff'`) and `'\''`
  for the quote. Unlike an integer literal it has a type of its own: in
  `'a' + 1` the `1` becomes a `uint8`, the arithmetic is `uint8`
  arithmetic, and `'a'` widens where a wider
  integer is expected, as in `exit('a')` or `var n int64 = 'a'`. It is a
  known value for proofs, conversions and array lengths, and may appear in a
  package-level initializer. Arithmetic on it, being on known values, must
  stay a value of its type at every step, wherever it is written:
  `'a' * 3` overflows `uint8` and is an error, while for a `mut` variable
  `c`, `c * 3` needs a proof, and `c *% 3` wraps at runtime. A character literal is exactly one byte, so `''`, `'ab'` and a
  character longer than one byte in UTF-8, such as `'é'`, are errors.

### Extern C

- `extern func name(params) results` declares a function whose body is
  written in C, and `extern("c_name") func ...` gives it a C name other
  than its Tack name. Every `.c` file in a package's directory, or beside
  a single-file target, is compiled and linked with the program, against
  a generated `tack.h` that declares each extern func and the types it
  uses, and includes the runtime's `<tack/tack.h>`, with `tack_string` and helpers
  for building strings in C (`TACK_STR`, `TACK_STR_N`, `TACK_C_STR_LEN`),
  when a string crosses; `--emit-header` writes `tack.h` for editors. Every Tack type
  crosses by value, except a `mut` parameter, which C gets as a pointer to
  the caller's variable, `T *`, to use only during the call, and a view
  parameter, which C gets as `tack_view_<T>`, a pointer to `const`
  elements and a length, to read only during the call, as it gets a
  variadic one, `args ...T`, whose values the caller puts in an array for
  the call. An `any` parameter is a `tack_any`, `{uint32_t type; const
  void *ptr;}`, by value, and `args ...any` a `tack_view_any` of them: C
  checks the type id with `TACK_ANY_IS(a, INT64)` or reads the value with
  a checked unpack helper, `tack_any_int64(a, &v)`, from std's
  `<tack/tack.h>`, during the call only, and never writes through the
  pointer or keeps it. A view held by an `any` has an id of its own,
  `TACK_TYPE_VIEW_INT64` for `[]int64`, and points at the view, which C
  reads as `*(const tack_view_int64 *)a.ptr` once `TACK_ANY_IS(a,
  VIEW_INT64)` holds. An error is a `tack_error`, `{int32_t code;
  tack_string msg;}`, by value, which C builds with `TACK_ERROR(code,
  "literal")`, or `TACK_NO_ERROR` for none, from the runtime's `<tack/tack.h>`;
  an extern func that returns one returns it last, in its results struct
  when it has several, and its callers check it as any function's. A
  list crosses only as a `mut` parameter, `tack_list_<T> *`, `{T *ptr;
  int64_t len; int64_t cap; tack_allocator alloc;}`, which C reads with
  `TACK_LIST_LEN(l)` and `TACK_LIST_AT(l, i)` and grows with
  `TACK_LIST_APPEND(lp, v)`, true when it grew, `TACK_LIST_APPEND_N(lp,
  src, n)`, all of `n` elements or none, and `TACK_LIST_RESERVE(lp, n)`,
  and shrinks with `TACK_LIST_POP(lp)` and `TACK_LIST_REMOVE(lp, i)`,
  each giving the element it takes out, at a length or an index C has
  checked, from the runtime's `<tack/tack.h>`, or as a view of its elements; by value, a list, or a
  struct holding one, is `parameter l of extern func f cannot be
  list[int64]: a list crosses to C only as a mut parameter, or as a
  view`, and so is one returned, `extern func f cannot return
  list[int64]: ...`. A list or a view whose elements hold lists does
  not cross yet, directly or in a struct, as a `mut` parameter or a
  view, `parameter l of extern func f cannot be list[list[uint8]]: a
  list or a view whose elements hold lists cannot cross to C yet`, or
  `cannot be S, which holds one: ...`. An `Allocator` is a `tack_allocator`. A union does
  not cross to C yet: a parameter or a result of an extern func that is
  one, or holds one through a struct, an array, a view or a list, is
  `parameter e of extern func f cannot be Expr: a union cannot cross to C
  yet`, `extern func f cannot return Expr: ...`, or `parameter s of
  extern func f cannot be S, which holds a union: ...`. An
  extern func is never `pub` or `lib`, and one no C
  file of its package defines is an error at its declaration.
- Extern C is Tack's unsafe: an extern func's declaration is a promise
  about what its C body does, which the compiler cannot check beyond its
  types. [docs/extern.md](docs/extern.md) is the whole of it: the build,
  the ABI and `tack.h`'s names and helpers, the rules for C names, what the C side
  must promise, the missing-definition check, every diagnostic, and a
  worked example.

## Naming

Tack names functions, methods, variables, fields and packages in
snake_case, `index_byte`, `append_int`, and types and interfaces in
PascalCase, `Allocator`, `Writer`. The standard library follows this
convention; it is a convention, documented here, and the compiler does
not enforce it. Most of the examples predate it and use camelCase for
functions and variables; they are not a guide to it. The existing names
of std and the builtins, `println`, `printf` and `c_allocator` among
them, stay as they are.

## Tests

```sh
go test ./...
```

`test/golden_test.go` drives the whole pipeline in process, so no build step is
needed. It runs every program under `examples/` and compares the result against
a golden file in `test/testdata/`, and separately asserts intended behavior for
programs that should and should not compile. A program that reaches code
generation is pinned once per backend, in `<case>.<backend>.golden`; one rejected
before then produces the same diagnostic whatever the backend is, so it keeps a
single `<case>.golden`. Each case runs under a timeout with
panic recovery, so an infinite loop or a nil dereference is reported as a test
failure rather than hanging the suite.

Golden files record what the compiler does today. After an intentional change to
its output, regenerate them and read the diff:

```sh
go test ./test -update
```

Every directory under `examples/` is a test case, and so is every case in
`examples/invalid.txtar`, which exists to pin the diagnostic each malformed
program produces, and in `examples/invalid-packages.txtar`, for programs of
several files; `go test ./test -update` also rewrites their expected
sections. The programs whose problem shows only once their C files are
compiled, a C file that does not compile or defines what it must not, or
an extern func no C file defines, are in `examples/invalid-c.txtar`, in the
same format with the `.c` files among each case's files; the driver's
tests build each one with clang, and `go test ./tack -run TestInvalidC
-update` rewrites its expected sections. `TestSafetyDoc` keeps [docs/safety.md](docs/safety.md) honest: an
HTML comment before each of its examples and quoted diagnostics names the
case or example that holds it, and the test fails if they disagree.

The goldens only pin that output is stable, not that it is correct. The test
that checks correctness is in `codegen/c`: it compiles a generated translation
unit with a real `clang` and asserts what the resulting binary prints. It skips
if `clang` is not on `PATH`. Those builds use `-Wall -Wextra -Werror`, so C the
backend emits must compile without a warning; a user's build passes `-w`
instead, since a warning about generated C is not one they can act on.
