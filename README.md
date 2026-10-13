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

func fib(n int64) int64 {
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
garbage/main.tck:4:5: error: invalid character '@'
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

The integer types are emitted as C's fixed-width types, `int8_t` through
`uint64_t`, and a `bool` is emitted as C's `bool`.

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
    BuildCmd(src, out string) []string               // {"clang", "-w", "-o", out, src}
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
- Line comments, `// ...`, run to the end of the line and can be written
  anywhere a line can end: before `package`, between declarations, after a
  statement, or inside an expression, argument list or array literal that
  spans lines. They mean the same as the newline that ends them. Block
  comments, `/* ... */`, are not supported.
- A `func main()` is required. It takes no parameters and declares no result.
- Top-level `func name(x int64, s string) int64 { ... }` declarations. Parameters
  are Go-style, one type each, no grouping. A function declares no result,
  one, or several in parentheses, `func divmod(a int64, b int64) (int64, int64)`, and
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
  `else if`), `switch`, `for`, `break`, `continue`, `defer`, a variable declaration, a
  declaration of several results, an assignment, and `x++`/`x--`. A function that declares a result must end in a terminating
  statement: a return, an if whose branches all terminate, a `switch` with a
  `default` whose cases all terminate, or a `for` with no condition that no
  `break` leaves. The rule is syntactic, so a call that never comes back
  does not satisfy it.
- Switches. `switch x { case 1, 2: ... case y: ... default: ... }` compares
  the tag `x` with each case's values using `==`, under its rules: the
  values have a common type with the tag, one widening to the other, an
  untyped constant takes the tag's type, and strings, bools, arrays and
  structs compare as `==` compares them. A tag that is an untyped constant
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
  switch. Two cases with the same constant value, a known integer or a
  string literal, are an error. There is no init statement,
  `switch v := f(); v`, as `if` has none. A switch proves what the chain
  of ifs it stands for proves: `if c1 {} else if c2 || c3 {} else {}` for
  a tagless switch, and the same with `x == v` for each value `v` of an
  expression switch, so `switch d { case 0: return 0 }` proves `d != 0`
  after it, and a later case and the default see every earlier case's
  values not matching. A case with several values proves nothing about
  them inside its body, as `||` gives nothing when true.
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
  Over an integer, only one name is allowed, and three names are an error
  anywhere. An array's index is proven in range for any array at least as
  long; a string's for the string it ranges over (see indexing a string).
- Local variables. `x := v`, `var x T = v` and `var x = v` declare an
  immutable variable; `mut` in place of `var` (`mut x := v`, `mut x T = v`,
  `mut x = v`) declares a mutable one, and `mut x T` starts it at its zero
  value: `0`, `""` or `false`. An immutable variable must have a value.
- `x = v` assigns to a mutable variable a value its type accepts. Parameters
  are immutable.
- `x += v`, `x -= v`, `x *= v`, `x /= v` and `x %= v` mean `x = x op v` for a
  mutable integer variable and a value its type accepts. Like a plain assignment, they do
  not count as reading `x`, so a variable that is only ever updated is still
  unused. `x++` and `x--` add and subtract one the same way. They are
  statements, never values, and there is no prefix form.
- Structs. `type Point struct { x int64; y int64 }` declares a struct type,
  at package level only, with its fields one per line or separated by `;`;
  `struct {}` has none. A type can be used anywhere in the file, before its
  declaration included, and its name shares one namespace with functions,
  package-level variables and builtins, so it cannot be declared again as
  any of them, nor as a local or a parameter; it cannot be used as a value,
  assigned or called, and a name that is not a type cannot be written as
  one. Each declaration is a type of its own, even with the same fields as
  another. Field names are a namespace of their own, one per struct: a
  field may share a name with anything outside its struct, but not with
  another of its fields, and `_` and the type names cannot name a field. A
  field's type may be any type, another struct or an array of one
  included, except one that holds the struct itself, directly, through
  other structs or through an array of any length, which would be
  infinitely large and is an error. Named types other than structs
  (`type Celsius int64`), anonymous struct types, methods, embedded fields
  and field visibility are not supported, and each is an error.
- Struct literals name every field exactly once, in any order:
  `Point{x: 1, y: 2}`. A literal without field names, `Point{1, 2}`, a
  field listed twice, a field the struct does not have, and a field left
  out are each an error. Each value is used as its field's type, under the
  usual rules: a constant takes it, and a narrower integer widens to it.
  Literals nest, `Line{a: Point{x: 1, y: 2}, b: p}`, and inside an array
  literal a struct literal may leave out its type like an array literal
  can: `[2]Point{{x: 1, y: 2}, {x: 3, y: 4}}`. A field's value may not
  leave out its type. In the header of an `if`, a `for`, a range loop or a
  `switch`, a struct literal must be in parentheses, `if p == (Point{x: 1, y: 2})`, as
  in Go, since the brace would otherwise open the body; inside a call, an
  index or another literal it needs none.
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
  ordered: `<` on two structs is an error. Structs may be package-level variables,
  initialized with a literal of constants. A field never carries a fact
  for a proof: `if p.d != 0 { n / p.d }` is still an error, and needs
  `q, ok := n / p.d`, or the field copied into a variable the guard then
  names, as in `d := p.d` and `if d != 0`.
- Package-level variables. `var` and `mut` declarations may also appear
  outside any function, where `:=` is not allowed. A package-level variable is
  initialized with a constant expression, made only of literals and
  operators, array and struct literals of them included, which the compiler
  computes; overflow and division by zero in
  one are compile errors. It can be used from any function, whatever the
  order of declarations, and shares one namespace with functions and
  builtins.
- A variable is in scope from its declaration to the end of its block. No
  variable or parameter may reuse the name of another variable or parameter
  in scope, package-level ones included, and a variable that is never read is
  an error. Reusing a function's name is allowed.
- Expressions are literals, parameter and variable names, calls, parentheses,
  array and struct literals, indexes, field selections, the infix
  operators `+ - * / %`, `== != < <= > >=` and `&& ||`, and the
  prefix `!` and `-`, with Go's precedence: the prefix operators bind
  tightest, then arithmetic, then comparisons, then `&&`, then `||`.
  Arithmetic takes two integers and gives their common type: the type of
  both, or the wider when one widens to the other, as `int8 + int64` is an
  `int64`; `int8 + uint8` and `uint64 + int64` have no common type and are
  errors. `-x` means `0 - x` and takes a signed integer; on an unsigned one it
  is an error, and `0 - x` says the wrapping is meant. At runtime,
  arithmetic that overflows wraps around at its type's width, as in Go: the
  largest value plus one is the smallest, negating the smallest signed value
  gives itself, and the smallest signed value divided by -1 is itself, with
  remainder 0. Only a value the compiler does not know wraps: arithmetic
  whose operands are all known values (a constant, a character literal, an
  immutable variable holding one, `len` of an array or of a known string,
  or arithmetic on those) must give a value of its type at every step, in
  a function as at package level, or it is an error,
  `constant expression overflows uint8`, covering the step that overflowed.
  So `x := 'a' * 3` is an error, and so are `-m`, `m - 1` and `m / -1`
  after `var m int8 = -128`, while `x++`, `x += 1` and arithmetic on a `mut`
  variable or a parameter wrap.
  A comparison takes two
  operands of one type, after the same widening: two integers or two
  strings with any of the six, or two `bool`s, arrays or structs with `==`
  and `!=`. Strings compare byte by byte, each byte unsigned, as in Go: a
  proper prefix is less, so the empty string is least, a zero byte is a
  byte like any other, and a byte from 0x80 up sorts after every ASCII
  byte, so `"é" > "z"`. Nothing is allocated, and a comparison of two
  string literals is made at run time.
- Division never divides by zero. `a / b`, `a % b`, `x /= b` and `x %= b`
  compile only where the compiler can prove `b` is not zero; a constant zero
  is an error. Elsewhere, `q, ok := a / b` (or `%`, or `q, ok = a / b`)
  divides only when `b` is not zero: `ok` reports whether it was not, and `q`
  is the result, or `0`. Both operands are always evaluated. The two-value
  form is an error where the divisor is proven not zero. A divisor is proven
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
  as `b != 0`, `b > 0`, `b <= -1` or `b == 3`. A known value is a constant,
  an immutable variable holding one, `len` of an array or of a known
  string (a string literal, or an immutable variable holding a known
  string), or arithmetic on those. The variable must be a
  parameter, a local, or an immutable global, and the region must not assign
  it anywhere, so a loop that counts its divisor up needs the check beside the
  division: `if d != 0 && n % d == 0`. A call to `exit` does not count as
  leaving. [docs/safety.md](docs/safety.md) gathers these rules, the
  two-value form and every operation they apply to in one place.
- `&&`, `||` and `!` take `bool` operands. `&&` and `||` short-circuit: the
  right side runs only when the left does not decide the result, so
  `n != 0 && 10 / n > 1` never divides by zero. A package-level initializer
  is computed in full, so an error on either side is reported.
- The types are the integers `int8`, `int16`, `int32`, `int64`, `uint8`,
  `uint16`, `uint32` and `uint64`, `string` and `bool`, arrays, and the
  structs a program declares. There is no
  `int`, and no `byte`. A bool is `true`, `false`, or the result of a
  comparison.
- Arrays. `[n]T` is an array of `n` elements of type `T`, where `n` is a
  constant: a constant expression, an immutable variable holding one, or
  `len` of an array or of a known string. `[3]int64{1, 2, 3}` is a literal,
  which lists exactly one element for each of the array's, and
  `[...]int64{1, 2, 3}` takes its length from its elements. A literal
  inside another may leave out its type: `[2][2]bool{{true, false},
  {false, true}}`, or a struct literal its own (see struct literals).
  Elements have no keys: `[3]int64{0: 1}` is an error. An array is a value: assigning, passing or returning one
  copies it, and `mut a [3]int64` starts with every element zero. An array
  follows its variable, so `a[i] = v` needs `a` to be `mut`, and assigning
  an element does not count as reading `a`. Two arrays of one type compare
  with `==` and `!=`, element by element, strings included; `<` on two
  arrays is an error. Arrays may be package-level variables, initialized with a
  literal of constants. There are no slices yet.
- Indexing. `a[i]` is an element of the array `a`, and `i` an integer of any
  type. An index is never out of range at runtime. `a[i]`, and an
  assignment to it, compile only where the compiler proves `i` in range: a
  constant, checked against the length at compile time, or a variable
  known to lie between 0 and the length less one. Elsewhere,
  `v, ok := a[i]` (or `v, ok = a[i]`) reads the element only when `i` is in
  range: `ok` reports whether it was, and `v` is the element, or its type's
  zero value; it is an error where `i` is proven in range. An assignment to
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
  `false` out of range, and is an error where `i` is proven. A string's
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
  the upper bound. A `uint64` index cannot be compared with `len(s)` or
  `len(a)`, an `int64`, since `uint64` and `int64` have no common type; this
  is intended, not a gap. Convert it first, `i, ok := int64(u)`, and prove
  `i`, or use the two-value form.
- `len(a)` is the number of elements of an array, a constant, and `len(s)`
  the number of bytes of a string, a known value for a string literal or an
  immutable variable holding a known string; either
  is an `int64`.
- A value of one integer type can be used where another is expected, with
  no conversion, when every value of the first is a value of the second: a
  signed type in a wider signed one, and an unsigned type in a wider type of
  either kind. That holds for assignments, arguments, results and the
  operands of an operator alike. Any other change of type is a conversion.
- Conversions. `T(x)` converts the integer `x` to the integer type `T`, and is
  allowed only where it cannot fail: `x` is a constant, or a known value (see
  division), that is a value of `T`, as `uint8(x)` after `x := 5` or
  `int8('a')`, or `x` is of a type that widens to `T`. A constant or known
  value that is not a value of `T` is an error, as `uint8(x)` after
  `x := 300`. Every other conversion is written
  `v, ok := T(x)` (or `v, ok = T(x)`): `ok` reports whether `x` is a value of
  `T`, and `v` is that value, or `0` when it is not. The two-value form is an
  error where the conversion cannot fail, so each conversion has one
  spelling. A guard never makes `T(x)` compile; see
  [docs/safety.md](docs/safety.md). Strings and bools cannot be converted. `true` and `false` are keywords, so neither can be
  used as a name, and both are constant expressions.
- `print` takes one string and writes exactly its bytes to standard output,
  adding no newline; a line ends where the string has a `\n`.
- `exit` takes an `int64`, or an integer that widens to one, and terminates
  the process with that status, without running any pending defer.
- Integer constants. A literal, and any expression made only of literals and
  operators, is a constant with no type of its own: it is computed exactly,
  and takes the type of where it is used, the type it is assigned, passed or
  compared to, or `int64` where nothing gives it one. It must be a value of
  that type: `var b uint8 = 256` and `x := 18446744073709551615` are errors.
  Every literal, and every step of a constant computation, must lie between
  the smallest `int64` and the largest `uint64`; beyond that is an error
  rather than a larger number. A `-` right before the digits is part of the
  literal, so the smallest int64 is written `-9223372036854775808`.
- Passing the wrong number or type of arguments is a type error, so a call
  cannot silently discard what it was given.
- Identifiers are ASCII letters, digits, and underscores, and may not start with
  a digit. The type names, `int8` through `uint64`, `string` and `bool`, are
  reserved like keywords: they can be written only as types and in
  conversions, never declared, read or assigned as names, nor used as a
  struct's or a field's name. `_` on its own is
  not a name: it only discards a result in a
  binding of several, as in `_, r := divmod(7, 2)` or `q, _ = divmod(7, 2)`,
  and may appear there more than once, or one of a range loop's two
  variables, as in `for _, x := range a`. It cannot be declared, read or
  assigned anywhere else, cannot be `mut`, and a binding whose every name is
  `_` is an error, since the call can be made on its own, or the loop
  written without names.
- String literals may contain any bytes except a newline or a double quote,
  and are emitted as raw bytes, so UTF-8 passes through unchanged.
- Raw string literals, written between backquotes, are raw, as in Go:
  every byte up to the closing backquote is taken as it is, so
  `` `\n` `` is a backslash and an `n`, and `` `\` `` is one backslash.
  Nothing is decoded, a raw string may span lines, and it cannot hold a
  backquote. Carriage returns are dropped from its value, as in Go, so
  the value of one spanning lines does not depend on the line endings it
  was saved with. A raw string with no closing backquote is an error.
- A string is a sequence of bytes, everywhere: `s[i]` is a `uint8`,
  `len(s)` counts bytes, a character literal is one byte, and `range` over
  a string gives bytes. Decoding UTF-8 into runes will
  be a facility of the standard library, not language syntax.
- Character literals. `'a'` is a constant of type `uint8`, the value of
  its one byte, with the escapes a string has (`'\n'`, `'\\'`) and `'\''`
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
program produces; `go test ./test -update` also rewrites its expected
sections. `TestSafetyDoc` keeps [docs/safety.md](docs/safety.md) honest: an
HTML comment before each of its examples and quoted diagnostics names the
case or example that holds it, and the test fails if they disagree.

The goldens only pin that output is stable, not that it is correct. The test
that checks correctness is in `codegen/c`: it compiles a generated translation
unit with a real `clang` and asserts what the resulting binary prints. It skips
if `clang` is not on `PATH`. Those builds use `-Wall -Wextra -Werror`, so C the
backend emits must compile without a warning; a user's build passes `-w`
instead, since a warning about generated C is not one they can act on.
