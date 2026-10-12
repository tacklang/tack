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

Functions have local variables, immutable unless declared with `mut`. There
are still no loops, so repetition means recursion. The process exit status
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
  are Go-style, one type each, no grouping. A function declares no result,
  one, or several in parentheses, `func divmod(a int, b int) (int, int)`, and
  `return a / b, a % b` gives one value for each. Results are unnamed.
- Several results are bound together, by position, with
  `q, r := divmod(7, 2)`. Each name is a new variable of its result's type,
  immutable unless that name has its own `mut`: `q, mut r := divmod(7, 2)`
  makes only `r` mutable. `q, r = divmod(7, 2)` assigns the results to
  existing mutable variables instead, each named once, and can be a loop's
  post statement. The value must be one call with exactly that many
  results. A call with several results cannot be used as a single value,
  passed straight to another call, or returned directly as another
  function's results, but it can be called as a statement to discard them
  all.
- The statements are a call, a `return`, `if`/`else` (which chains with
  `else if`), `for`, `break`, `continue`, a variable declaration, a
  declaration of several results, an assignment, and `x++`/`x--`. A function that declares a result must end in a terminating
  statement: a return, an if whose branches all terminate, or a `for` with no
  condition that no `break` leaves. The rule is syntactic, so a call that
  never comes back does not satisfy it.
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
- Range over an int. `for i := range n { ... }` runs once for each `i` from
  `0` to `n - 1`, and not at all when `n` is zero or less; `for range n`
  does the same without a variable. `n` is an `int` evaluated once, before
  the first iteration. `i` is a fresh immutable `int` each iteration.
- Local variables. `x := v`, `var x T = v` and `var x = v` declare an
  immutable variable; `mut` in place of `var` (`mut x := v`, `mut x T = v`,
  `mut x = v`) declares a mutable one, and `mut x T` starts it at its zero
  value: `0`, `""` or `false`. An immutable variable must have a value.
- `x = v` assigns to a mutable variable of the same type. Parameters are
  immutable.
- `x += v`, `x -= v`, `x *= v`, `x /= v` and `x %= v` mean `x = x op v` for a
  mutable `int` variable and an `int` value. Like a plain assignment, they do
  not count as reading `x`, so a variable that is only ever updated is still
  unused. `x++` and `x--` add and subtract one the same way. They are
  statements, never values, and there is no prefix form.
- Package-level variables. `var` and `mut` declarations may also appear
  outside any function, where `:=` is not allowed. A package-level variable is
  initialized with a constant expression, made only of literals and
  operators, which the compiler computes; overflow and division by zero in
  one are compile errors. It can be used from any function, whatever the
  order of declarations, and shares one namespace with functions and
  builtins.
- A variable is in scope from its declaration to the end of its block. No
  variable or parameter may reuse the name of another variable or parameter
  in scope, package-level ones included, and a variable that is never read is
  an error. Reusing a function's name is allowed.
- Expressions are literals, parameter and variable names, calls, parentheses,
  the infix operators `+ - * / %`, `== != < <= > >=` and `&& ||`, and the
  prefix `!` and `-`, with Go's precedence: the prefix operators bind
  tightest, then arithmetic, then comparisons, then `&&`, then `||`.
  Arithmetic and `-x` take `int` operands; `-x` means `0 - x`, so negating
  the smallest int overflows as subtracting does. A comparison takes two operands of the
  same type: two `int`s with any of the six, or two `bool`s with `==` and
  `!=`. Strings cannot be compared yet.
- `&&`, `||` and `!` take `bool` operands. `&&` and `||` short-circuit: the
  right side runs only when the left does not decide the result, so
  `n != 0 && 10 / n > 1` never divides by zero. A package-level initializer
  is computed in full, so an error on either side is reported.
- The types are `int`, `string`, and `bool`. A bool is `true`, `false`, or the
  result of a comparison. `true` and `false` are keywords, so neither can be
  used as a name, and both are constant expressions.
- `print` takes one string and writes exactly its bytes to standard output,
  adding no newline; a line ends where the string has a `\n`.
- `exit` takes one integer and terminates the process with that status.
- Integer literals. `int` is 64-bit signed, and a literal that does not fit is
  a parse error. A `-` right before the digits is part of the literal, so the
  smallest int is written `-9223372036854775808`.
- Passing the wrong number or type of arguments is a type error, so a call
  cannot silently discard what it was given.
- Identifiers are ASCII letters, digits, and underscores, and may not start with
  a digit. `_` on its own is not a name: it only discards a result in a
  binding of several, as in `_, r := divmod(7, 2)` or `q, _ = divmod(7, 2)`,
  and may appear there more than once. It cannot be declared, read or
  assigned anywhere else, cannot be `mut`, and a binding whose every name is
  `_` is an error, since the call can be made on its own.
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
