# Tack

Tack is a small compiled programming language. This repository holds `tack`, its
compiler, written from scratch in Go.

It is a learning project, built to understand how a compiler works end to end
rather than to be used for anything. There is no runtime, no standard library,
and no intermediate representation.

Code generation is pluggable. The front end hands a resolved syntax tree to a
*backend*, and the backend decides what comes out and how it is built. One ships
today: `c`, which emits a self-contained C translation unit that `clang` turns
into an executable.

## Status

Early, and honest about it. The compiler runs a complete pipeline and produces
working native binaries, but the language it accepts is still small.

```tack
package main

func fib(n int) int {
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

There are still no variables and no loops, so every intermediate value is an
argument or a return, and repetition means recursion. The process exit status
is still the only way to observe an integer, since `print` takes strings only.

`exit` is a global builtin as a stand-in. Once the language has packages it
should become `os.Exit`, and the global goes away.

## Requirements

- Go 1.27.1 or newer
- `clang` on `PATH`, used to compile and link

## Building

```sh
make build       # builds ./bin/tack
make install     # installs tack into $GOBIN, or $GOPATH/bin if unset
make test        # runs go test ./...
```

## Usage

```sh
tack --out hello examples/hello/main.tck
./hello
```

Flags go before the file. The intermediate stages can be dumped instead of
linking. A dump prints only that stage's output to stdout, so it can be
redirected: `tack --emit x.tck > x.c` gives a C file that compiles. A dump
does not take `--out`, and only one dump can be given at a time.

| Flag               | Effect                                          |
|--------------------|-------------------------------------------------|
| `--lex`            | print the token stream and stop                 |
| `--ast`            | print the parsed syntax tree as JSON and stop   |
| `--res`            | print the resolved syntax tree as JSON and stop |
| `--emit`           | print the generated code and stop               |
| `--backend <name>` | select the code generation backend, default `c` |
| `--backends`       | list the available backends and stop            |
| `--out <path>`     | compile and link to `<path>`                    |
| `--max-diags <n>`  | print at most `n` diagnostics; 0 prints all     |
| `-h`, `--help`     | print usage and exit                            |

Diagnostics and errors go to stderr. The exit status is 0 on success, 1 when
the program has errors or the build fails, and 2 when the command line is
wrong.

```sh
tack --emit examples/hello/main.tck > hello.c   # the C the backend emits
```

## How it works

Each stage is a package that takes a data structure and returns one, along with
any diagnostics. The first stage to report a diagnostic ends the run.

| Package    | Role                                                          |
|------------|---------------------------------------------------------------|
| `source`   | the source file, its bytes, and its line index                |
| `lexer`    | bytes into tokens, including Go-style semicolon insertion     |
| `parser`   | tokens into an AST                                            |
| `ast`      | node types, source spans, and `Walk`                          |
| `resolver` | checks the program has a `main` package and a `main` function |
| `types`    | Tack's type representation                                    |
| `builtin`  | the builtins' names and signatures                            |
| `typeck`   | checks calls pass the right number and type of arguments      |
| `codegen`  | the backend interface and its registry                        |
| `codegen/c`| AST into C                                                    |
| `diag`     | diagnostics and their rendering                               |
| `tack`     | the command line driver                                       |

Diagnostics carry a source span and render with the offending line and a caret:

```
examples/invalid/garbage/main.tck:4:5: error: invalid character '@'
02 |
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

An `int` is a 64-bit signed integer, emitted as C's `int64_t`, and a `bool` is
emitted as C's `bool`.

## Builtins

`print` and `exit` have no declaration in a Tack program: the resolver knows
their names and the type checker knows their signatures, both from the
`builtin` package, and each backend lowers them for its own target.

`src/std/builtin/builtin.tck` documents them as Tack declarations with no
bodies, the way Go's `src/builtin/builtin.go` does. It is documentation rather
than input, since `go:embed` cannot reach out of a package directory and
moving the file into the Go tree would defeat the point of having a `src/std`.
What keeps it honest is a test in the `builtin` package that parses it with
the compiler's own front end and fails if it names a builtin the compiler does
not implement, gives one the wrong signature, or stops parsing. Declarations
that are wanted but not implemented go in `src/std/builtin/PLANNED.md`, which
nothing checks and nothing claims is true.

## Code generation backends

A backend is anything that satisfies this interface:

```go
type Backend interface {
    Name() string                                    // "c"
    Ext() string                                     // ".c"
    Generate(file *ast.File) ([]byte, []diag.Diagnostic)
    BuildCmd(src, out string) []string               // {"clang", "-o", out, src}
}
```

`Generate` receives the same resolved tree every backend sees and returns target
source, or diagnostics if it meets something it cannot lower. `Ext` and
`BuildCmd` are what keep the driver from knowing anything about the target: it
writes the bytes to a temporary file with that extension and runs that command.

Backends register themselves from `init`, so adding one means writing a package
under `codegen/` and importing it for its side effect in `tack/main.go` and
`test/golden_test.go`. Nothing else in the compiler changes, and the new backend
picks up golden coverage automatically.

The C backend translates directly, with no intermediate representation. Each
Tack function becomes a `static void` C function named `tack_<package>_<name>`,
string literals are pooled into `unsigned char` byte arrays so that arbitrary
bytes survive without an escaping scheme, `print` lowers to a pair of `fwrite`
calls on `stdout`, and a generated `int main(void)` calls the Tack `main`. The
mangling is unambiguous only while package names cannot contain an underscore,
which is true today because `main` is the only package there is.

## The language today

- `package main` is required, and must be the first declaration.
- A `func main()` is required. It takes no parameters and declares no result.
- Top-level `func name(x int, s string) int { ... }` declarations. Parameters
  are Go-style, one type each, no grouping. At most one result.
- The statements are a call, a `return`, and `if`/`else`, which chains with
  `else if`. A function that declares a result must end in a terminating
  statement: a return, or an if whose branches all terminate. The rule is
  syntactic, so a call that never comes back does not satisfy it.
- Expressions are literals, parameter names, calls, parentheses, and the infix
  operators `+ - * / %` and `== != < <= > >=`, with Go's precedence. Every
  operand must be an `int`.
- The types are `int`, `string`, and `bool`. A bool comes only from a
  comparison; there are no `true` and `false` literals yet.
- `print` takes one string and writes it followed by a newline.
- `exit` takes one integer and terminates the process with that status.
- Integer literals. `int` is 64-bit signed, and a literal that does not fit is
  a parse error.
- Passing the wrong number or type of arguments is a type error, so a call
  cannot silently discard what it was given.
- Identifiers are ASCII letters, digits, and underscores, and may not start with
  a digit.
- String literals may contain any bytes except a newline or a double quote,
  and are emitted as raw bytes, so UTF-8 passes through unchanged.

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

Every directory under `examples/` is a test case, including the ones under
`examples/invalid/`, which exist to pin the diagnostic each malformed program
produces.

The goldens only pin that output is stable, not that it is correct. The test
that checks correctness is in `codegen/c`: it compiles a generated translation
unit with a real `clang` and asserts what the resulting binary prints. It skips
if `clang` is not on `PATH`.
