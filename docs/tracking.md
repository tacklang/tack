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
[known-bugs.md](known-bugs.md).

## Today

### Lexical elements

| Feature                     | Lexer | Parser  | Checker | C       | Notes                                                                                                                                |
|-----------------------------|-------|---------|---------|---------|--------------------------------------------------------------------------------------------------------------------------------------|
| Identifiers                 | yes   | yes     | yes     | yes     | ASCII letters, digits, `_`; no Unicode letters                                                                                       |
| Keywords                    | yes   | partial | n/a     | n/a     | Tack's own set, inspired by Go rather than copied: 27 lexed; only `package`, `func`, `if`, `else`, `return`, `var`, `mut` are parsed |
| Automatic semicolons        | yes   | yes     | n/a     | n/a     | Go's rule                                                                                                                            |
| Line comments               | yes   | partial | n/a     | n/a     | Only between top-level declarations; anywhere else is a parse error                                                                  |
| Block comments              | no    | no      | no      | no      | Reported as unsupported                                                                                                              |
| Decimal integer literals    | yes   | yes     | yes     | yes     | 64-bit; out of range is a parse error; leading zeros are decimal                                                                     |
| Hex, octal, binary literals | no    | no      | no      | no      |                                                                                                                                      |
| Float literals              | yes   | no      | no      | no      |                                                                                                                                      |
| Char literals               | yes   | no      | no      | no      |                                                                                                                                      |
| String literals             | yes   | yes     | yes     | yes     | Bytes, not runes; escapes `\a \b \f \v \n \t \r \\ \"`; no newline                                                                   |
| Raw string literals         | yes   | yes     | yes     | yes     | Escapes are decoded too, unlike Go                                                                                                   |
| Operators                   | yes   | partial | partial | partial | All of Go's are lexed; see Expressions for what is parsed                                                                            |

### Declarations

| Feature                  | Lexer | Parser  | Checker | C   | Notes                                                                                                |
|--------------------------|-------|---------|---------|-----|------------------------------------------------------------------------------------------------------|
| `package` clause         | yes   | yes     | partial | yes | Must be first and must be `main`                                                                     |
| Imports                  | yes   | no      | no      | no  |                                                                                                      |
| Functions                | yes   | yes     | yes     | yes | Top level only; any order; no duplicates; none named after a builtin                                 |
| `main` function          | yes   | yes     | yes     | yes | Required; takes no parameters and declares no result                                                 |
| Parameters               | yes   | yes     | yes     | yes | One type each, no grouping (`a, b int`)                                                              |
| Variadic parameters      | yes   | no      | no      | no  | Rejected with a diagnostic                                                                           |
| Results                  | yes   | partial | yes     | yes | At most one, unnamed; multiple results (tuples) are planned, and today give a misleading parse error |
| Functions without a body | yes   | yes     | partial | yes | Accepted, and compiled as an empty body                                                              |
| Constants and types      | yes   | no      | no      | no  |                                                                                                      |
| Package-level variables  | yes   | yes     | yes     | yes | `var`/`mut` only; constant initializers, computed at compile time; usable from any function          |
| Methods                  | yes   | no      | no      | no  |                                                                                                      |

### Statements

| Feature                        | Lexer | Parser | Checker | C   | Notes                                                                                                   |
|--------------------------------|-------|--------|---------|-----|---------------------------------------------------------------------------------------------------------|
| Call statement                 | yes   | yes    | yes     | yes | A result may be discarded                                                                               |
| `return`                       | yes   | yes    | yes     | yes | Value required exactly when the function declares a result                                              |
| `if` / `else` / `else if`      | yes   | yes    | yes     | yes | Condition must be a bool                                                                                |
| Terminating statement rule     | n/a   | n/a    | yes     | n/a | Syntactic, as in Go                                                                                     |
| Local variables                | yes   | yes    | yes     | yes | `x :=`, `var`, `mut`; immutable by default; zero values for `mut x T`; no shadowing; unused is an error |
| Assignment `=`                 | yes   | yes    | yes     | yes | Mutable variables only; parameters are immutable                                                        |
| Compound assignment, `++` `--` | yes   | no     | no      | no  | Decided: later                                                                                          |
| `for` loops                    | yes   | no     | no      | no  |                                                                                                         |
| `switch`                       | yes   | no     | no      | no  |                                                                                                         |
| `break` / `continue`           | yes   | no     | no      | no  |                                                                                                         |
| Standalone blocks              | yes   | no     | no      | no  |                                                                                                         |
| `defer`, `select`, `goto`      | yes   | no     | no      | no  |                                                                                                         |

### Expressions

| Feature                           | Lexer | Parser | Checker | C       | Notes                                                       |
|-----------------------------------|-------|--------|---------|---------|-------------------------------------------------------------|
| Integer and string literals       | yes   | yes    | yes     | yes     |                                                             |
| Parameter and variable references | yes   | yes    | yes     | yes     | Parameters and variables are the only names with values     |
| Calls                             | yes   | yes    | yes     | yes     | To a function by name; arity and argument types checked     |
| Parentheses                       | yes   | yes    | yes     | yes     |                                                             |
| `+ - * / %`                       | yes   | yes    | yes     | yes     | `int` operands only; overflow and division by zero follow C |
| `== != < <= > >=`                 | yes   | yes    | yes     | yes     | Same-type operands: `int` all six, `bool` `==` `!=` only    |
| `!`                               | yes   | yes    | yes     | yes     | `bool` operand only                                         |
| Unary `-` `+` `^`                 | yes   | no     | no      | no      | `-1` cannot be written                                      |
| `&&` `\|\|`                        | yes   | yes    | yes     | yes     | `bool` operands only; short-circuit                         |
| Bitwise and shift operators       | yes   | no     | no      | no      |                                                             |
| `true` / `false`                  | yes   | yes    | yes     | yes     | Keywords, so not usable as names; constant expressions      |
| Evaluation order                  | n/a   | n/a    | n/a     | partial | Unspecified, inherited from C                               |

### Types

| Feature                       | Checker | C   | Notes                                                         |
|-------------------------------|---------|-----|---------------------------------------------------------------|
| `int`                         | yes     | yes | 64-bit signed, `int64_t`                                      |
| `string`                      | yes     | yes | Pointer and length, `tack_string` by value                    |
| `bool`                        | yes     | yes | Compared with `==` `!=`; combined with `&&` `\|\|` `!`          |
| Other integer and float types | no      | no  |                                                               |
| Arrays, slices, maps, structs | no      | no  |                                                               |
| Pointers                      | no      | no  |                                                               |
| Interfaces, generics          | no      | no  |                                                               |

### Builtins

| Builtin         | Checker | C   | Notes                                                          |
|-----------------|---------|-----|----------------------------------------------------------------|
| `print(string)` | yes     | yes | Any string expression; writes it and a newline to stdout       |
| `exit(int)`     | yes     | yes | Terminates with that status; a stand-in for a future `os.Exit` |

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
| Diagnostics with source context    | yes     | Stops at the first stage that reports; reports every problem in that stage |
| `make build/install/test/fmt/lint` | yes     |                                                                            |
| Golden tests                       | yes     | Every program under `examples/`                                            |
| Fuzzing                            | partial | Lexer alone, and the whole pipeline through the driver                     |
| Self-hosting                       | no      | The goal: a Tack compiler written in Tack                                  |

## Timeline

To be added.
