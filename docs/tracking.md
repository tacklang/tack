# Tracking

What the compiler implements, stage by stage. A feature is usable only when
every stage it passes through handles it, so each one is tracked per stage:

- **Lexer**: tokenized.
- **Parser**: turned into a syntax tree.
- **Checker**: resolved and type-checked.
- **C**: lowered by the C backend.

Each cell is one of **yes**, **partial** (the notes say what is missing), **no**, or **n/a**. A stage that says no stops
the feature there with a
diagnostic, never a crash or silently wrong code.

Open questions about how a feature should behave are in
[open-decisions.md](open-decisions.md), and bugs are in
[known-bugs.md](known-bugs.md). The rules that keep division, indexing and
conversions from failing at runtime, proofs and the `v, ok :=` form, are in
[safety.md](safety.md). Checks for a future `tack vet`, for code that is
valid but says more than it needs to, are in [vet.md](vet.md).

## Today

### Lexical elements

| Feature                     | Lexer | Parser  | Checker | C       | Notes                                                                                                            |
|-----------------------------|-------|---------|---------|---------|------------------------------------------------------------------------------------------------------------------|
| Identifiers                 | yes   | yes     | yes     | yes     | ASCII letters, digits, `_`; no Unicode letters                                                                   |
| Keywords                    | yes   | partial | n/a     | n/a     | Tack's own set, inspired by Go rather than copied: 29 lexed; 19 parsed, see the statements and expressions below |
| Line endings                | yes   | n/a     | n/a     | n/a     | `\n` or `\r\n`, alike everywhere; a lone `\r` is an invalid character outside literals and comments              |
| Automatic semicolons        | yes   | yes     | n/a     | n/a     | Go's rule                                                                                                        |
| Line comments               | yes   | yes     | n/a     | n/a     | Anywhere a line can end, including before `package`; kept in the syntax tree, ignored by the grammar             |
| Block comments              | no    | no      | no      | no      | Not part of the language; `/*` is reserved and reported                                                          |
| Decimal integer literals    | yes   | yes     | yes     | yes     | Smallest `int64` to largest `uint64`; beyond is a parse error; no leading zeros: `007`, `00` are errors          |
| Hex, octal, binary literals | no    | no      | no      | no      |                                                                                                                  |
| Float literals              | yes   | no      | no      | no      |                                                                                                                  |
| Char literals               | yes   | yes     | yes     | yes     | A typed `uint8` constant; one byte, escapes as in strings; `'é'` is an error                                     |
| String literals             | yes   | yes     | yes     | yes     | Bytes, not runes, everywhere; escapes `\a \b \f \v \n \t \r \\ \"`; no newline                                   |
| Raw string literals         | yes   | yes     | yes     | yes     | Raw, as in Go: no escapes, may span lines, no backquote inside; carriage returns dropped                         |
| Operators                   | yes   | partial | partial | partial | All of Go's are lexed; see Expressions for what is parsed                                                        |

### Declarations

| Feature                  | Lexer | Parser | Checker | C   | Notes                                                                                       |
|--------------------------|-------|--------|---------|-----|---------------------------------------------------------------------------------------------|
| `package` clause         | yes   | yes    | partial | yes | Must be first and must be `main`                                                            |
| Imports                  | yes   | no     | no      | no  |                                                                                             |
| Functions                | yes   | yes    | yes     | yes | Top level only; any order; no duplicates; none named after a builtin                        |
| `main` function          | yes   | yes    | yes     | yes | Required; takes no parameters and declares no result                                        |
| Parameters               | yes   | yes    | yes     | yes | One type each, no grouping (`a, b int64`)                                                   |
| Variadic parameters      | yes   | no     | no      | no  | Rejected with a diagnostic                                                                  |
| Results                  | yes   | yes    | yes     | yes | None, one, or several in parentheses; unnamed; several are returned as one C struct         |
| Functions without a body | yes   | yes    | partial | yes | Accepted, and compiled as an empty body                                                     |
| Constants                | yes   | no     | no      | no  |                                                                                             |
| Struct type declarations | yes   | yes    | yes     | yes | `type T struct { ... }`, package level only; any order; no recursion by value; field array lengths name no variable |
| Other named types        | yes   | no     | no      | no  | `type C int64`, aliases and anonymous structs are rejected with a diagnostic                |
| Package-level variables  | yes   | yes    | yes     | yes | `var`/`mut` only; constant initializers, computed at compile time; usable from any function |
| Methods                  | yes   | no     | no      | no  | Rejected with a diagnostic, declared or called                                              |

### Statements

| Feature                    | Lexer | Parser | Checker | C   | Notes                                                                                                         |
|----------------------------|-------|--------|---------|-----|---------------------------------------------------------------------------------------------------------------|
| Call statement             | yes   | yes    | yes     | yes | A result may be discarded                                                                                     |
| `return`                   | yes   | yes    | yes     | yes | One value for each declared result; `return f()` cannot forward another call's results                        |
| Several results `a, b :=`  | yes   | yes    | yes     | yes | One call with exactly that many results; `mut` per name; no `var` form, no `x, y := 1, 2`                     |
| Several results `a, b =`   | yes   | yes    | yes     | yes | Mutable targets, each named once, no `mut`; allowed as a loop's post statement                                |
| Blank `_`                  | yes   | yes    | yes     | yes | Only discards a value in `a, _ :=`, `a, _ =` or a two-name range; not a name elsewhere; not all `_`           |
| `if` / `else` / `else if`  | yes   | yes    | yes     | yes | Condition must be a bool                                                                                      |
| Terminating statement rule | n/a   | n/a    | yes     | n/a | Syntactic, as in Go, including `for {}` without a `break`                                                     |
| Local variables            | yes   | yes    | yes     | yes | `x :=`, `var`, `mut`; immutable by default; zero values for `mut x T`; no shadowing; unused is an error       |
| Assignment `=`             | yes   | yes    | yes     | yes | Mutable variables only; parameters are immutable                                                              |
| Compound assignment        | yes   | yes    | yes     | yes | `+= -= *= /= %=` on any integer type; not a read of the target; bitwise forms come with the bitwise operators |
| `++` `--`                  | yes   | yes    | yes     | yes | Postfix statements only, never values; integer targets; not a read of the target                              |
| `for cond {}`, `for {}`    | yes   | yes    | yes     | yes | Condition must be a bool                                                                                      |
| Three-clause `for`         | yes   | yes    | yes     | yes | Go's rules; parts optional; init variable scoped to the loop; a counter is declared with `mut`; a counting loop proves its variable in the body |
| `range` over an integer    | yes   | yes    | yes     | yes | `for i := range n`, `for range n`; `n` evaluated once; `i` a fresh immutable variable of `n`'s type           |
| `range` over array/string  | yes   | yes    | yes     | yes | `for i, x := range a`, `_` index; evaluated once into a copy; `int64` index; string bytes `uint8`             |
| `switch`                   | yes   | yes    | yes     | yes | Tagged and tagless; no fallthrough; `break`/`continue` act on the loop; no init statement; lowered to C ifs   |
| `break` / `continue`       | yes   | yes    | yes     | yes | Innermost loop only; no labels                                                                                |
| Standalone blocks          | yes   | no     | no      | no  |                                                                                                               |
| `defer`                    | yes   | yes    | yes     | yes | `defer { ... }` only, not in a defer; block-scoped, as in Zig; runs at block exit, last first; `exit` skips   |
| `select`, `goto`           | yes   | no     | no      | no  |                                                                                                               |

### Expressions

| Feature                           | Lexer | Parser | Checker | C       | Notes                                                                                                                                                                   |
|-----------------------------------|-------|--------|---------|---------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Integer and string literals       | yes   | yes    | yes     | yes     |                                                                                                                                                                         |
| Parameter and variable references | yes   | yes    | yes     | yes     | Parameters and variables are the only names with values                                                                                                                 |
| Calls                             | yes   | yes    | yes     | yes     | To a function by name; arity and argument types checked                                                                                                                 |
| Parentheses                       | yes   | yes    | yes     | yes     |                                                                                                                                                                         |
| `+ - * / %`                       | yes   | yes    | yes     | yes     | Integer operands, widened to a common type; on known values, overflow is an error; otherwise it wraps at the type's width; divisor proven non-zero, or `q, ok := a / b` |
| `== != < <= > >=`                 | yes   | yes    | yes     | yes     | One type after widening: integers and strings all six, `bool`, arrays and structs `==` `!=` only; strings by unsigned bytes, never folded                             |
| `!`                               | yes   | yes    | yes     | yes     | `bool` operand only                                                                                                                                                     |
| Unary `-`                         | yes   | yes    | yes     | yes     | Signed integer operands only; the smallest known value is an overflow error; `-` before digits is a negative literal                                                    |
| Array literals `[n]T{...}`        | yes   | yes    | yes     | yes     | Exactly one element each; `[...]` from the elements; inner types may be left out                                                                                        |
| Struct literals `T{f: v}`         | yes   | yes    | yes     | yes     | Every field named once, any order; parenthesized in an `if`/`for`/range header; type left out only inside an array literal                                              |
| Field selection `x.f`             | yes   | yes    | yes     | yes     | On any struct value; written through a `mut` variable, mixed with indexes to any depth; no facts for proofs                                                             |
| Indexing `a[i]`                   | yes   | yes    | yes     | yes     | Proven in range, from constants, range loops (over a count or an array), guards and types, or `v, ok := a[i]`                                                           |
| Indexing a string `s[i]`          | yes   | yes    | yes     | yes     | A `uint8`; proven from a known string's length, `len(s)` guards and range loops over `len(s)` or `s`, or `v, ok := s[i]`; never assigned                               |
| Conversions `T(x)`                | yes   | yes    | yes     | yes     | Integer to integer; `T(x)` only where it cannot fail (a constant or known value that fits, or a widening type), otherwise `v, ok := T(x)`                                |
| Integer constants                 | yes   | yes    | yes     | yes     | Untyped, exact, between the smallest `int64` and the largest `uint64`; take their type from use, else `int64`                                                           |
| Unary `+` `^`                     | yes   | no     | no      | no      | Decided: no unary `+` for now                                                                                                                                           |
| `&&` `\|\|`                       | yes   | yes    | yes     | yes     | `bool` operands only; short-circuit                                                                                                                                     |
| Bitwise and shift operators       | yes   | no     | no      | no      |                                                                                                                                                                         |
| `true` / `false`                  | yes   | yes    | yes     | yes     | Keywords, so not usable as names; constant expressions                                                                                                                  |
| Evaluation order                  | n/a   | n/a    | n/a     | yes     | Left to right; the C backend holds operands in temporaries where the order could show                                                                                   |

### Types

| Feature                            | Checker | C   | Notes                                                             |
|------------------------------------|---------|-----|-------------------------------------------------------------------|
| `int8` `int16` `int32` `int64`     | yes     | yes | Signed, `int8_t` to `int64_t`                                     |
| `uint8` `uint16` `uint32` `uint64` | yes     | yes | Unsigned, `uint8_t` to `uint64_t`; no `byte`                      |
| Lossless widening                  | yes     | yes | Implicit wherever a value is used; no `int`                       |
| `string`                           | yes     | yes | Pointer and length, `tack_string` by value; immutable bytes; all six comparisons, by unsigned bytes |
| `bool`                             | yes     | yes | Compared with `==` `!=`; combined with `&&` `\|\|` `!`            |
| Float types                        | no      | no  |                                                                   |
| Arrays `[n]T`                      | yes     | yes | Values, a C struct each; `==` element by element; constant length |
| Structs                            | yes     | yes | Values, a C struct each; `==` field by field; no embedding        |
| Slices, maps                       | no      | no  |                                                                   |
| Pointers                           | no      | no  |                                                                   |
| Interfaces, generics               | no      | no  |                                                                   |

### Builtins

| Builtin         | Checker | C   | Notes                                                                                   |
|-----------------|---------|-----|-----------------------------------------------------------------------------------------|
| `print(string)` | yes     | yes | Any string expression; writes exactly it to stdout, no newline                          |
| `len(v)`        | yes     | yes | Arrays and strings; known for arrays, string literals and immutable variables holding a known string |
| `exit(int64)`   | yes     | yes | Terminates with that status; narrower integers widen; a stand-in for a future `os.Exit` |

Wanted builtins are listed in `src/std/builtin/PLANNED.md`.

### Runtime and memory

| Feature                     | Status | Notes                                                  |
|-----------------------------|--------|--------------------------------------------------------|
| Runtime                     | no     | Generated C calls libc directly                        |
| Memory model and allocation | no     | Nothing allocates; strings are static                  |
| Standard library            | no     | `src/std/fmt` is a sketch; nothing compiles against it |
| Packages beyond `main`      | no     |                                                        |
| Concurrency                 | no     |                                                        |

### Toolchain

| Feature                            | Status  | Notes                                                                      |
|------------------------------------|---------|----------------------------------------------------------------------------|
| C backend                          | yes     | Needs `clang` on `PATH`                                                    |
| Other backends                     | no      | The registry allows them; arm64 was dropped                                |
| `tack` driver                      | yes     | Build, stage dumps, `--max-diags`, exit codes 0/1/2                        |
| Diagnostics with source context    | yes     | Stops at the first stage that reports; reports every problem in that stage, each once, and nothing that follows from one reported |
| `make build/install/test/fmt/lint` | yes     |                                                                            |
| Golden tests                       | yes     | Every program under `examples/` and every case in `examples/invalid.txtar` |
| Fuzzing                            | partial | Lexer alone, and the whole pipeline through the driver                     |
| Self-hosting                       | no      | The goal: a Tack compiler written in Tack                                  |

## Timeline

To be added.
