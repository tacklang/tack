# Tack

Tack is a small compiled programming language. This repository holds `tack`, its
compiler, written from scratch in Go.

It is a learning project, built to understand how a compiler works end to end
rather than to be used for anything. There is no runtime, a standard library
of one package, `std/fmt`, and no intermediate representation.

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
loops. `print` takes strings only; `std/fmt`'s `printf` writes integers,
bools and strings, through `any` parameters (see the language today).

`exit` is a global builtin as a stand-in for a future `os.Exit`, after which
the global goes away. The language has packages now, but the standard
library has none yet.

## Requirements

- Go 1.27.1 or newer
- `clang` on `PATH`, used to compile and link

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
extern funcs use strings includes std's `<tack/tack.h>`, so the editor
needs `-I $TACK_HOME/src/std/include` in its compile flags, as the build
has.

| Flag               | Effect                                          |
|--------------------|-------------------------------------------------|
| `--lex`            | print the token stream and stop                 |
| `--ast`            | print the parsed syntax tree as JSON and stop   |
| `--res`            | print the resolved syntax tree as JSON and stop |
| `--emit`           | print the generated code and stop               |
| `--emit-header`    | write `tack.h` beside the C files and stop      |
| `--backend <name>` | select the code generation backend, default `c` |
| `--backends`       | list the available backends and stop            |
| `--out <path>`     | compile and link to `<path>`                    |
| `--max-diags <n>`  | print at most `n` diagnostics; 0 prints all     |
| `-O0` ... `-O3`, `-Os`, `-Oz` | optimize the build at that clang level, default `-O3` |
| `-h`, `--help`     | print usage and exit                            |

Diagnostics and errors go to stderr. The exit status is 0 on success, 1 when
the program has errors or the build fails, and 2 when the command line is
wrong.

```sh
tack --emit examples/hello/main.tck > hello.c   # the C the backend emits
```

## How it works

Each stage is a package that takes a data structure and returns one, along with
any diagnostics. The first stage to report a diagnostic ends the run. A stage
reports every problem it finds, each once: an error that follows from one
already reported, such as an unused variable whose declaration was wrong, is
not reported.

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
`uintptr_t`. A `bool` is emitted as C's `bool`.

## Builtins

`print` and `exit` have no declaration in a Tack program. The compiler holds
no list of them: it reads the `.tck` files of `std/builtin` in the
installed standard library, `$TACK_HOME/src/std/builtin`, whose source is
[`src/std/builtin/builtin.tck`](src/std/builtin/builtin.tck), and every
extern func declared there is a builtin, with the name, signature and C
name it is declared with:

```tack
extern("tack_builtin_print") func print(s string)
extern("tack_builtin_exit") func exit(code int32)
```

Every declaration there must be such a builtin: an extern func with its C
name given, starting `tack_`, returning nothing, with parameters of basic
types, none `mut` or variadic (`parameter xs of builtin p cannot be
variadic`) and none `any` (`parameter a of builtin p cannot be any`), and not named after a basic type, `len`, `main` or `_`. Anything
else, a syntax error included, is reported as a diagnostic in that file.

A call to `print` is a call to `tack_builtin_print`, and to `exit` one to
`tack_builtin_exit`; the generated C declares each builtin the program
calls with a prototype, `void tack_builtin_print(tack_string);`. Their
bodies are the `.c` files beside `builtin.tck`, one per builtin and named
after it, `print.c` and `exit.c`, which include `<tack/tack.h>` from
`src/std/include` for `tack_string`. That header is the one definition of
what every program's C shares, `tack_string` and the string helpers,
`tack_error` and the error helpers, and
`tack_any`, its type ids and its unpack helpers: the generated C includes
it too, and so does a program's `tack.h` when an extern func uses a string,
an error or `any`, so every C file is compiled with
`-I $TACK_HOME/src/std/include`, and `--emit` output needs that flag to
compile on its own. The driver compiles them as it
compiles a program's C files and links only the objects that define a
builtin the program calls, and those that define what those objects need
in turn, as a linker takes an archive's members. So the generated C
includes only `<stdbool.h>` and `<stdint.h>`, `<stddef.h>` if it uses
`unsafe_size_t`, which a freestanding C implementation has, and
`<tack/tack.h>`, which includes only those three, and a program
that calls neither builtin links neither body and none of the C library
they call. A target without a C library needs only different bodies.

`len` is generic, so no Tack declaration can describe it: the compiler
implements it itself, and `builtin.tck` describes it in a comment.
Declarations that are wanted but not implemented go in
`src/std/builtin/PLANNED.md`, which the compiler does not read.

## Code generation backends

A backend is anything that satisfies this interface:

```go
type Backend interface {
    Name() string                                    // "c"
    Ext() string                                     // ".c"
    Generate(prog *ast.Program, builtins *builtin.Set) ([]byte, []diag.Diagnostic)
    Header(prog *ast.Program) []byte                 // tack.h
    CompileCmd(src, header, include, obj string, opt Opt) []string // {"clang", "-w", "-O3", "-c", ..., src}
    CompileStdCmd(src, include, obj string, opt Opt) []string      // likewise, for std's C
    BuildCmd(src, include string, objs []string, out string, opt Opt) []string // {"clang", "-w", "-O3", "-I", include, "-o", out, src, objs...}
    VersionCmd() []string                            // {"clang", "--version"}
}
```

`Generate` receives the same resolved program every backend sees and returns target
source, or diagnostics if it meets something it cannot lower. `Ext`,
`CompileCmd`, `CompileStdCmd` and `BuildCmd` are what keep the driver from
knowing anything about the target: it writes the bytes to a file with that
extension in a temporary directory, writes `Header`'s output beside it as
`tack.h`, compiles each C file of the program with `CompileCmd` and std's
with `CompileStdCmd`, and links them with `BuildCmd`, each given std's
include directory, `include`, where `<tack/tack.h>` is. `VersionCmd`'s
output identifies the toolchain, so an object compiled from std's C is
reused from the build cache only by the same one.

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
generated `int main(void)` calls the Tack `main`. Each
part is read back by its length, so no two Tack names, whatever underscores
they hold, map to one C name, and none takes the shape of a name the backend
makes for itself, which has a letter after `tack_`.

## The language today

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
  itself`. Every file is lexed, and then every file parsed, before the
  first stage with errors stops the run, so each file's problems are
  reported. A file compiled on its own is the whole package.
- A directory is compiled in its module. A module is a directory tree
  whose root holds a `tack.mod`, which names it in one line, `module
  calc`, and may hold `//` comments, which run to the end of the line
  wherever they start, and blank lines. A module name is one or more parts
  joined by `/`, each lowercase ASCII letters, digits, `-`, `.` and `_`,
  not starting or ending with `.` or `-`, as in `example.com/my-tools`; it
  may not be `std` or start with `std/`, which are the standard library's.
  Anything else in `tack.mod` is an error at the line it is on: a second
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
  starting with a letter, and is not a keyword, a builtin type's or
  function's name, `builtin` or `std`: a directory holding a package of
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
  one `lib` or `pub` types: every struct type, or array of one, that a
  function's parameters or results, a global's type or a field's type
  name, the field's visibility capped by its struct's, must be at least as
  visible, or it is an error, `pub function Result exposes hidden, which is
  not pub` (`which is not lib or pub` for `lib`; `variable` and `field kind
  of Token` for the others). A `pub` or `lib` global is never reported as
  unused, since the packages that read it may not be in the program; a
  private one still is. `main` packages cannot be imported, so `pub` and
  `lib` mean nothing there and are errors wherever written, on a
  declaration or a field: `pub has no effect in package main, which no
  package can import`. Locals and parameters take neither keyword.
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
  Its one package today is [`fmt`](src/std/fmt/fmt.tck), whose `pub func
  println(s string)` writes `s` and then a newline, `fmt.println("hi")`,
  and whose `pub func printf(format string, args ...any)` writes formatted
  text (see printf).
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
  which declares the builtins, is not a package programs use: `std/builtin
  documents the builtins and cannot be imported`. No other module may be
  named `std` or start with `std/` (above); a directory inside std itself
  is in the module std.
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
  comments are not part of the language, and `/*` is reserved: anywhere
  outside a string or comment it is an error, `block comments are not
  supported`, and the lexer skips to the next `*/`, or the end of the file,
  before going on.
- `asm` is a keyword with no meaning yet: everything after it up to the
  next `}` is skipped unread, and an `asm` with no `}` after it is an error,
  `unterminated asm block`. No statement or declaration starts with `asm`,
  so any use of it is still rejected.
- A `main` package must declare a `func main()`, which takes no parameters
  and declares no result. No other package may declare anything named
  `main` (see imports).
- Top-level `func name(x int64, s string) int64 { ... }` declarations. Parameters
  are Go-style, one type each, no grouping. A function declares no result,
  one, or several in parentheses, `func divmod(a int64, b int64) (int64, int64)`, and
  `return a / b, a % b` gives one value for each. Results are unnamed.
- Mut parameters. A parameter written `mut p T`, as in `func scale(mut p
  Point, k int64)`, is a mutable reference to the caller's variable for the
  duration of the call: inside, `p`, `p.x` and `p.cells[i]` read and write
  the caller's variable directly, `p = Point{...}` replaces it, and
  `p.x++` changes it. Every other parameter is an immutable copy of its
  argument, as before. The call writes `mut` before the argument,
  `scale(mut b, 10)`, and only there: leaving it out for a `mut` parameter
  is an error, `scale takes p as mut; pass mut b`, and so is writing it
  for a plain one, `count does not take t as mut; pass t`. What follows
  `mut` must be a place the caller may change: a mutable local, a `mut`
  parameter, a field of one, `mut b.lo`, or an element of one whose index
  is proven in range (see arrays), `mut cells[i]`, nested in any way,
  `mut w.rows[1].pts[j].y`. Anything else is an error: an immutable
  variable or plain parameter, `cannot pass mut c: c is immutable; declare
  it with mut`, an immutable global, `cannot pass mut limit: limit is
  immutable; copy it into a mutable local`, a mutable global, `a mutable
  global cannot be passed as mut; copy it into a local`, since any function
  can reach one, a string's byte, `cannot pass mut s[0]: strings are
  immutable`, and a value that is not a place, such as a call's result or
  a literal, `only a variable, a field or an element can be passed as mut`.
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
  a fact about it holds in a region that never assigns it, and a call
  that passes a variable, or a part of it, as `mut` assigns it at that
  call (see division). An extern func may take a `mut` parameter, which C
  gets as a pointer (see extern C).
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
  argument to count; view its bytes with []uint8(s)`. A view parameter is
  passed on to another of the same type as it is, `sum(v)`. A view has
  `len(v)`, an `int64` never known, with the facts a string variable's
  length has; `v[i]`, with `i` proven in range against `len(v)` as for a
  string, or the checked form `x, ok := v[i]`; every range form, `for
  i, x := range v`, whose index is proven for `v`; and slicing, `v[i:j]`,
  passed on (see slicing). Nothing else: no `==`,
  `operator == is not defined on []int64`, no conversion, no printing. A
  view is the type of a parameter only: anywhere else, a local, a field, a
  result, a global or an array's element, it is `a view can only be the
  type of a parameter`, a variable declared from one, `x := v`, is `cannot
  declare x as []int64; a view can only be the type of a parameter`, and
  a view of views is `the element type of a view cannot be a view`. A view
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
  may not.
- `any`. A parameter `a any` takes a value of any basic type, an integer
  of any type, a `bool` or a `string`, which the function sees as a
  reference to the caller's value with its type known only at run time:
  in C, a type id and a pointer, `tack_any`. A value passes to it as it is,
  with nothing written, `show(5)`, `show("s")`; an untyped constant is an
  `int64` there, a character literal a `uint8`, and a constant no `int64`
  holds is `constant 18446744073709551615 overflows int64`. `any(x)` is no
  conversion, `cannot convert int64 to any`. An array, a struct or a view
  cannot be passed as one yet, `cannot pass Point as any: only the builtin
  types can be passed as any for now`, nor can a `mut` argument, `f does
  not take x as mut; pass x`. The value is a copy taken at the call, as a
  plain argument's is: `f(mut x, x)` gives the `any` `x` as it was before
  the call, and a mutable global is copied too. `args ...any` is a
  variadic of them, a read-only `[]any` under every view rule (see
  variadics), and a view parameter may be `[]any`. `any` is second-class,
  as a view is: anywhere but a parameter's type or a variadic's or view
  parameter's element type, a local, a field, a global, a result or an
  array's element, it is `any can only be the type of a parameter`, and a
  variable declared from one, `b := a`, `v, ok := args[0]` or `for _, v :=
  range args`, is `cannot declare b as any; any can only be the type of a
  parameter`. `mut a any` is `mut has no effect on an any parameter; take
  mut p T to change the caller's value`. Inside its function an `any` can
  only be passed on, to another `any` parameter, `g(a)`, or as a value of
  an `...any`, `many(a, args[0])`, and an `[]any` spread, `many(args...)`,
  `many(args[1:]...)`, or passed to an `[]any` parameter: it supports no
  operator, `operator == is not defined on any` (a `switch` on one too),
  no conversion, `cannot convert any to int64`, and no use as another type,
  `cannot use any as string in argument to print`. Asking which type an
  `any` holds, and reading its value, is for C only in this version (see
  extern C). `any` is a type's name, so it cannot name anything else,
  `cannot use any as a name; it is a type`.
- printf. `fmt.printf(format, args...)`, from `std/fmt`, writes `format`
  to standard output with each verb replaced by its argument: `%d` an
  integer of any type in decimal, `%x` one in lowercase hexadecimal, with
  a `-` before a negative value as Go writes it, `%s` a string's bytes,
  `%t` a bool, `%c` a `uint8` as its byte, `%v` any of them as its own
  verb would, and `%%` a `%`. There are no widths or flags. A verb is the
  `%` and the UTF-8 character after it. When the compiler knows the
  format, a literal or an immutable variable holding one, every verb is
  checked against its argument, and each mistake is an error: `printf
  verb %d does not take string`, `unknown printf verb %q` (a verb that is
  not graphic, such as a newline, is quoted, `unknown printf verb
  "%\n"`), `printf format ends in a lone %`, and `printf format has 2
  verbs but 1 argument` at the format, or at the arguments left over when
  there are more than verbs. An `any` passed on matches any verb, since
  its type is known only at run time, and a spread, `printf(f,
  args...)`, has its verbs checked but not its count. A format the
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
  - Its error cannot be dropped. A call statement is `the error f returns
    cannot be dropped; bind it, as in _, err := f(...)`, and `_` where the
    error goes, a binding of nothing but `_` included, is `the error f
    returns cannot be dropped; bind it to a variable instead of _`. A call
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
    branch, a `switch err.code` in its `case 0:`, and `err.code == 0 &&`
    on its right. Like any fact it holds only in a region that never
    assigns `err`, so binding a new error to it later in the same block
    ends it, and a branch ending in `exit` does not leave. Comparing the
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

  The rules are the compiler's: an error is a plain value at run time,
  in C a `tack_error` passed by value (see extern C).
- The statements are a call, a `return`, `if`/`else` (which chains with
  `else if`), `switch`, `for`, `break`, `continue`, `defer`, a variable declaration, a
  declaration of several results, an assignment, and `x++`/`x--`. A function that declares a result must end in a terminating
  statement: a return, an if whose branches all terminate, a `switch` with a
  `default` whose cases all terminate, or a `for` with no condition that no
  `break` leaves. The rule is syntactic, so a call that never comes back
  does not satisfy it.
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
  body runs: there is no fallthrough, and `fallthrough` is not a keyword.
  `break` and `continue` in a case act on the loop around the switch, not
  on the switch, unlike Go; outside a loop they are the usual error. Each
  case's body is a block of its own and may be empty, and so may the
  switch. Two cases with the same constant value, a known integer, a bool
  constant expression such as `true` or `1 > 2`, or a string literal, are
  an error. There is no init statement,
  `switch v := f(); v`, as `if` has none. A switch proves what the chain
  of ifs it stands for proves: `if c1 {} else if c2 || c3 {} else {}` for
  a tagless switch, and the same with `x == v` for each value `v` of an
  expression switch, so `switch d { case 0: return 0 }` proves `d != 0`
  after it, and a later case and the default see every earlier case's
  values not matching. A case with several values proves what their `||`
  does inside its body: `case 1, 2:` proves the tag is between 1 and 2,
  and so not zero.
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
  still hold when it runs: a fact holds only in a region that never
  assigns its variable, and that region takes in the rest of the defer's
  block, every other defer in it included.
- Loops. `for cond { ... }` runs while `cond`, a `bool`, holds, and
  `for { ... }` runs until something in its body leaves it. `break` leaves the
  innermost loop and `continue` starts its next iteration; either one outside
  a loop is an error. There are no labels.
- Three-clause loops, `for init; cond; post { ... }`, as in Go. `init` runs
  once and may be any simple statement: a declaration, an assignment,
  `x++`/`x--`, or a call. `post` runs after each iteration, `continue`
  included, and may be any of those except a declaration. Every part may be
  left out (`for ;; { ... }`). A variable `init` declares belongs to the
  loop, and needs `mut` to be updated: `for mut i := 0; i < n; i++ { ... }`.
  A counting loop proves its variable in its body: when `init` declares it
  with a known value `c`, `post` steps it by a known amount (`i++`,
  `i += k`, or `i--`, `i -= k`, with `k` above zero), the condition bounds
  it on the side it moves towards without the step wrapping past its
  type, and the body never assigns it, the body knows `i >= c` counting
  up, or `i <= c` counting down. So `for mut i := 0; i < len(a); i++`
  proves `a[i]`; see [docs/safety.md](docs/safety.md).
- Range over an integer. `for i := range n { ... }` runs once for each `i`
  from `0` to `n - 1`, and not at all when `n` is zero or less; `for range n`
  does the same without a variable. `n` is an integer of any type, a
  constant being an `int64`, evaluated once, before the first iteration.
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
  changes.
  Over an integer, only one name is allowed, and three names are an error
  anywhere. An array's index is proven in range for any array at least as
  long; a string's or a view's for the string or view it ranges over (see
  indexing a string, and views).
- Local variables. `x := v`, `var x T = v` and `var x = v` declare an
  immutable variable; `mut` in place of `var` (`mut x := v`, `mut x T = v`,
  `mut x = v`) declares a mutable one, and `mut x T` starts it at its zero
  value: `0`, `""` or `false`. An immutable variable must have a value.
- `x = v` assigns to a mutable variable a value its type accepts. Parameters
  are immutable, except a `mut` parameter (see mut parameters).
- `x += v`, `x -= v`, `x *= v`, `x /= v` and `x %= v` mean `x = x op v` for a
  mutable integer variable and a value its type accepts. Like a plain assignment, they do
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
  another of its fields, and `_` and the type names cannot name a field. An
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
  infinitely large and is an error. Named types other than structs
  (`type Celsius int64`), anonymous struct types, methods and embedded
  fields are not supported, and each is an error. A field may be `pub` or
  `lib`, as described under visibility.
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
  struct with no fields is always equal to another. Structs are not
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
  - arithmetic, comparisons, `&&`, `||` and `!` on constant expressions,
    strings, arrays and structs comparing as they do at run time.

  Anything else is `x must be initialized with a constant expression`: a
  call to a function, or to a builtin other than `len`, and a mutable
  global outside `len` of an array. Overflow and division by zero in a
  constant expression are compile errors, as for any known value (see
  arithmetic). A global can be used from any function, whatever the
  order of declarations, and shares one namespace with functions and
  builtins.
- A variable is in scope from its declaration to the end of its block. No
  variable or parameter may reuse the name of another variable or parameter
  in scope, package-level ones included, and a variable that is never read is
  an error. Reusing a function's name is allowed.
- Expressions are literals, parameter and variable names, calls, a call's
  `mut` arguments, parentheses,
  array and struct literals, indexes, slices, field selections, the infix
  operators `+ - * / %`, `== != < <= > >=` and `&& ||`, and the
  prefix `!` and `-`, with Go's precedence: the prefix operators bind
  tightest, then arithmetic, then comparisons, then `&&`, then `||`.
  Arithmetic takes two integers and gives their common type: the type of
  both, or the wider when one widens to the other, as `int8 + int64` is an
  `int64`; `int8 + uint8` and `uint64 + int64` have no common type and are
  errors, and so are `unsafe_size_t + int64` and
  `unsafe_size_t + unsafe_uintptr_t` (see target-width integers). `-x` means `0 - x` and takes a signed integer; on an unsigned one it
  is an error, and `0 - x` says the wrapping is meant. At runtime,
  arithmetic that overflows wraps around at its type's width, as in Go: the
  largest value plus one is the smallest, negating the smallest signed value
  gives itself, and the smallest signed value divided by -1 is itself, with
  remainder 0. Only a value the compiler does not know wraps: arithmetic
  whose operands are all known values (a constant, a character literal, an
  immutable variable holding one, `len` of an array or of a known string,
  a field or element of an immutable global, or arithmetic on those; see
  division) must give a value of its type at every step, in
  a function as at package level, or it is an error,
  `constant expression overflows uint8`, covering the step that overflowed.
  A target-width type is the exception above 4294967295, where it may or
  may not wrap depending on the target: there, arithmetic on known values
  is an error only in a constant expression (see target-width integers),
  and anywhere else is not a known value, and is computed at run time.
  So `x := 'a' * 3` is an error, and so are `-m`, `m - 1` and `m / -1`
  after `var m int8 = -128`, while `x++`, `x += 1` and arithmetic on a `mut`
  variable or a parameter wrap. This is intended, and it means changing
  `var` to `mut` can turn a compile error into wrapping at run time: after
  `var m int8 = 127`, `m + 1` is an error, while after `mut m int8 = 127`
  the same `m + 1` is `-128`, since a mutable variable's value is never
  known.
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
  the six, or two `bool`s, arrays or structs with `==` and `!=`, an array
  or a struct of integers needing the same element or field types, so
  `[2]int8` and `[2]uint8` do not compare. Strings compare byte by byte, each byte unsigned, as in Go: a
  proper prefix is less, so the empty string is least, a zero byte is a
  byte like any other, and a byte from 0x80 up sorts after every ASCII
  byte, so `"é" > "z"`. Nothing is allocated. Inside a function, a
  comparison of two string literals is made at run time; in a
  package-level variable's initializer, a comparison of known strings, or
  of arrays or structs holding them, is computed at compile time, so `var
  x = "a" == "a"` is `true`.
- Division never divides by zero. `a / b`, `a % b`, `x /= b` and `x %= b`
  compile only where the compiler can prove `b` is not zero; a constant zero
  is an error. Elsewhere, `q, ok := a / b` (or `%`, or `q, ok = a / b`)
  divides only when `b` is not zero: `ok` reports whether it was not, and `q`
  is the result, or `0`. The division must be the whole value:
  `q, ok := a / b + 1` is an error that suggests `q, ok := a / b`, then
  `q + 1`. Both operands are always evaluated. Where the
  divisor is proven not zero the two-value form is allowed too, and `ok` is
  always `true` (see [docs/vet.md](docs/vet.md)). A divisor is proven
  not zero when it is a non-zero constant, an immutable variable holding
  one, or a variable a condition covers where it is used:
  - inside `if c { ... }` when `c` being true rules out zero, and in its
    `else` when `c` being false does;
  - in the statements after an `if` whose branch ends in `return`, `break`
    or `continue`, by what got past it, as in `if b == 0 { return 0 }`;
  - in a loop's body, by its condition, and in a counting loop's body by
    the value its variable starts from (see three-clause loops);
  - in a `switch`, as in the chain of ifs it stands for (see switches);
  - on the right of `&&` by its left side being true, and of `||` by its left
    side being false.

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
  immutable variable or parameter (see structs), and the region must not assign
  it anywhere, a call passing it, or a part of it, as `mut` included, so a loop that counts its divisor up needs the check beside the
  division: `if d != 0 && n % d == 0`. A call to `exit` does not count as
  leaving. When a guard says something about a divisor or index it did not
  prove, the error comes with notes pointing at the guard and what it
  proves, at the assignment or `mut` argument that ended that proof, or at what the guard
  allows that the operation does not, such as `i == 3` for a `[3]T`, and
  at a branch ending in `exit`, a counting loop's step that could wrap, or
  a guard on a field of a mutable variable. A mutable global's error suggests only the two-value
  form, with a note at its declaration, since no check proves it. [docs/safety.md](docs/safety.md) gathers these rules, the
  two-value form and every operation they apply to in one place, and
  [docs/vet.md](docs/vet.md) the checks a future `tack vet` is to make,
  such as a two-value form whose `ok` is always `true`.
- Evaluation order is left to right wherever Tack evaluates several
  operands: the two sides of an operator, a call's arguments, a return's
  values, an array literal's elements, a struct literal's values in the
  order they are written, an indexed array before its index, a sliced
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
  `unsafe_size_t` and `unsafe_uintptr_t`, `string`, `bool` and `error`, arrays,
  views and `any`, which only a parameter may have, and the structs a
  program declares. There is no
  `int`, and no `byte`. A bool is `true`, `false`, or the result of a
  comparison.
- Target-width integers. `unsafe_size_t` and `unsafe_uintptr_t` are C's
  `size_t` and `uintptr_t`, for crossing to C (see Extern C): unsigned
  integers as wide as the target makes them, somewhere from 32 to 64 bits,
  and not necessarily as wide as each other. The `unsafe_` prefix is the
  only marker; they may be used anywhere any other integer type can,
  `pub` and `lib` signatures included, and have every operation an
  unsigned integer has, arithmetic wrapping at their real width. Their
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
  passes to a view parameter, `[]T`, without a copy (see views).
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
  `uint8` index into a `[256]T` needs nothing. Only a name is proven; an
  index like `i + 1` needs the two-value form. See
  [docs/safety.md](docs/safety.md) for every proof and diagnostic.
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
  array or view `x`, from `i` up to, but not including, `j`, as in Go;
  `x[:j]` starts at 0, `x[i:]` runs to `len(x)`, and `x[:]` is all of it.
  There is no three-index form: `x[i:j:k]` is `a slice has at most two
  bounds, x[i:j]; there is no three-index form`. A string's slice is a
  `string`, `{ptr + i, j - i}`, sharing the bytes it was cut from and never
  copying them, so it is a value like any other string: stored in a
  variable, a field, an array, a global's initializer when computable, or
  returned. A string slices by bytes, so a slice may cut through a
  multi-byte UTF-8 character. An array's or a view's slice is a view, `[]T`
  of its element type, under the rules for views: it can only be passed
  to a view parameter, `sum(a[1:3])`, an extern func's included, spread
  into a variadic one, `total(a[1:3]...)`, given to `len`, or sliced
  again; anywhere else, stored, returned, indexed, ranged
  over or compared, it is `a[1:3] is a view, []int64, which can only be
  passed to a view parameter or to len`. An array must be a place, as for a
  view: `f()[1:]` is `cannot slice a temporary array; store it in a variable
  first`. A slice of a mutable global array, or of an array the same call
  passes as `mut`, is of a copy taken when the argument is evaluated, as a
  view of it would be; a string's slice never copies. Anything else
  sliced is `cannot slice int64`, and a bound must be an integer, `slice
  bound must be an integer, got bool`, of any integer type, a target-width
  one included. The bounds must be proven, as an index is: `0 <= i`, `i <=
  j` and `j <= len(x)`, from known values, a variable's type, guards and
  loops, with two more relations a slice's guard may give, `j <= len(x)`
  and `i <= j`; a bound left out needs nothing of its own, so `x[:j]` needs
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
  division), and `len(v)` the number of elements of a view, never known
  but for a slice at known bounds; each is an `int64`.
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
  `int8('a')`, or `x` is of a type that widens to `T`. A constant or known
  value that is not a value of `T` is an error, as `uint8(x)` after
  `x := 300`. For a target-width `T`, a value of `T` is one from 0 to
  4294967295, a value on every target; a known value past that but
  within 64 bits fits on some targets only, so converting it is like
  converting a value not known. Every other conversion is written
  `v, ok := T(x)` (or `v, ok = T(x)`): `ok` reports whether `x` is a value of
  `T`, and `v` is that value, or `0` when it is not; for a target-width `T`,
  whether it is one on the target the program runs on. Where the conversion
  cannot fail the two-value form is allowed too, and `ok` is always `true`;
  a constant or known value that does not fit is an error in either form.
  A guard never makes `T(x)` compile; see
  [docs/safety.md](docs/safety.md). Strings and bools cannot be converted. `true` and `false` are keywords, so neither can be
  used as a name, and both are constant expressions, as is `noerror`, a
  keyword too (see errors).
- `print` takes one string and writes exactly its bytes to standard output,
  adding no newline; a line ends where the string has a `\n`.
- `exit` takes an `int32`, or an integer that widens to one
  (`int8`, `int16`, `uint8`, `uint16`), and terminates the process with that
  status, without running any pending defer. An `int64`, `uint32` or
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
  literal, so the smallest int64 is written `-9223372036854775808`. A
  literal is decimal and has no leading zeros: `0` is fine, but `007` and
  `00` are errors, unlike C and Go, where `010` is octal 8. There are no
  hex, octal or binary literals yet, so `0x10` is `0` followed by the name
  `x10`.
- Passing the wrong number or type of arguments is a type error, so a call
  cannot silently discard what it was given: `f expects 2 argument(s), got
  3`, or for a variadic function, which takes any number past its other
  parameters, `f expects at least 1 argument(s), got 0` (see variadics).
- Identifiers are ASCII letters, digits, and underscores, and may not start with
  a digit. The type names, `int8` through `uint64`, `unsafe_size_t`,
  `unsafe_uintptr_t`, `string`, `bool` and `error`, are
  reserved like keywords: they can be written only as types and in
  conversions, never declared, read or assigned as names, nor used as a
  struct's or a field's name. `_` on its own is
  not a name: it only discards a result in a
  binding of several, as in `_, r := divmod(7, 2)` or `q, _ = divmod(7, 2)`,
  and may appear there more than once, or one of a range loop's two
  variables, as in `for _, x := range a`. It cannot be declared, read or
  assigned anywhere else, cannot be `mut`, and a binding whose every name is
  `_` is an error, since the call can be made on its own, or the loop
  written without names; for a call that returns an error, it is the
  error dropped (see errors).
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
  `'a' * 3` overflows `uint8` and is an error, while `c * 3` wraps at
  runtime for a `mut` variable `c`. A character literal is exactly one byte, so `''`, `'ab'` and a
  character longer than one byte in UTF-8, such as `'é'`, are errors.

### Extern C

- `extern func name(params) results` declares a function whose body is
  written in C, and `extern("c_name") func ...` gives it a C name other
  than its Tack name. Every `.c` file in a package's directory, or beside
  a single-file target, is compiled and linked with the program, against
  a generated `tack.h` that declares each extern func and the types it
  uses, and includes std's `<tack/tack.h>`, with `tack_string` and helpers
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
  pointer or keeps it. An error is a `tack_error`, `{int32_t code;
  tack_string msg;}`, by value, which C builds with `TACK_ERROR(code,
  "literal")`, or `TACK_NO_ERROR` for none, from std's `<tack/tack.h>`;
  an extern func that returns one returns it last, in its results struct
  when it has several, and its callers check it as any function's. An
  extern func is never `pub` or `lib`, and one no C
  file of its package defines is an error at its declaration.
- Extern C is Tack's unsafe: an extern func's declaration is a promise
  about what its C body does, which the compiler cannot check beyond its
  types. [docs/extern.md](docs/extern.md) is the whole of it: the build,
  the ABI and `tack.h`'s names and helpers, the rules for C names, what the C side
  must promise, the missing-definition check, every diagnostic, and a
  worked example.

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
