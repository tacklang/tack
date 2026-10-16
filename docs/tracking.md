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
[safety.md](safety.md), and extern C, Tack's unsafe, is in
[extern.md](extern.md). Checks for a future `tack vet`, for code that is
valid but says more than it needs to, are in [vet.md](vet.md).

## Today

### Lexical elements

| Feature                     | Lexer | Parser  | Checker | C       | Notes                                                                                                            |
|-----------------------------|-------|---------|---------|---------|------------------------------------------------------------------------------------------------------------------|
| Identifiers                 | yes   | yes     | yes     | yes     | ASCII letters, digits, `_`; no Unicode letters                                                                   |
| Keywords                    | yes   | partial | n/a     | n/a     | Tack's own set, inspired by Go rather than copied: 35 lexed; 28 parsed, see the statements and expressions below |
| Line endings                | yes   | n/a     | n/a     | n/a     | `\n` or `\r\n`, alike everywhere; a lone `\r` is an invalid character outside literals and comments              |
| Automatic semicolons        | yes   | yes     | n/a     | n/a     | Go's rule                                                                                                        |
| Line comments               | yes   | yes     | n/a     | n/a     | Anywhere a line can end, including before `package`; kept in the syntax tree, ignored by the grammar             |
| Block comments              | no    | no      | no      | no      | Not part of the language; `/*` is reserved and reported                                                          |
| Decimal integer literals    | yes   | yes     | yes     | yes     | Smallest `int64` to largest `uint64`; beyond is a parse error; no leading zeros: `007`, `00` are errors          |
| Hex, octal, binary literals | no    | no      | no      | no      |                                                                                                                  |
| Float literals              | yes   | no      | no      | no      |                                                                                                                  |
| Char literals               | yes   | yes     | yes     | yes     | A typed `uint8` constant; one byte, escapes as in strings; `'é'` is an error                                     |
| String literals             | yes   | yes     | yes     | yes     | Bytes, not runes, everywhere; escapes `\a \b \f \v \n \t \r \\ \"` and `\xHH`; no newline                          |
| Raw string literals         | yes   | yes     | yes     | yes     | Raw, as in Go: no escapes, may span lines, no backquote inside; carriage returns dropped                         |
| Operators                   | yes   | partial | partial | partial | All of Go's are lexed; see Expressions for what is parsed                                                        |

### Declarations

| Feature                  | Lexer | Parser | Checker | C   | Notes                                                                                       |
|--------------------------|-------|--------|---------|-----|---------------------------------------------------------------------------------------------|
| `package` clause         | yes   | yes    | yes     | yes | Must be first; `main`, or in a module the import path's last part; every file of a directory names the same package; a single file must be `main` |
| Several files per package | n/a  | yes    | yes     | yes | A directory is one package: its `.tck` files in name order, package-level names shared by all, any order |
| Modules and `tack.mod`   | n/a   | yes    | yes     | n/a | `module <name>` only; a directory builds in the module found at or above it; package name is the import path's last part, or `main` |
| Imports                  | yes   | yes    | yes     | yes | `import x from "path"`, per file, one per line after the package clause; cycles, unused and duplicate imports are errors |
| `pub` / `lib`            | yes   | yes    | yes     | n/a | Functions, types, immutable globals and fields; private by default; `lib` within the module; not on `mut` globals, nor anywhere in `main`; exposed types must be as visible |
| Functions                | yes   | yes    | yes     | yes | Top level only; any order; no duplicates; none named after a builtin                        |
| `main` function          | yes   | yes    | yes     | yes | Required in package `main`, not allowed in any other; takes no parameters and declares no result; not extern |
| Parameters               | yes   | yes    | yes     | yes | One type each, no grouping (`a, b int64`); immutable copies unless `mut`                    |
| `mut` parameters         | yes   | yes    | yes     | yes | `mut p T`, a reference to the caller's variable for the call, a `T *` in C; passed as `mut x`, a mutable local, `mut` parameter, field or proven element; no two `mut` arguments of a call overlap, by path, elements only at indexes known to differ; a plain argument overlapping one is a copy; never stored or returned; a `T *` in an extern func's C too |
| View parameters `[]T`    | yes   | yes    | yes     | yes | Read-only, parameters only, never `mut`; an array place of exactly the element type passes as it is, a temporary array is an error, a string through `[]uint8(s)` only; `len`, proven or checked index, range, passed on; no `==` or conversion; copied first when overlapping a `mut` argument or part of a mutable global; `tack_view_<T>`, `{const T *ptr; int64_t len;}`, in C |
| Variadic parameters      | yes   | yes    | yes     | yes | `args ...T`, the last parameter only, a read-only view `[]T` inside under every view rule, never `mut`, no view element; a call passes zero or more values, each used as a `T`, evaluated left to right and copied, or spreads a view on, `f(a, v...)`, alone after the fixed arguments; no spread to a non-variadic call or a conversion; the caller packs the values in an array on its stack, a `tack_view_<T>` in C, `{0, 0}` for none; allowed on extern funcs, not on builtins; `...any` takes values of every basic type |
| Results                  | yes   | yes    | yes     | yes | None, one, or several in parentheses; unnamed; several are returned as one C struct; an `error` only as the last |
| Functions without a body | yes   | yes    | yes     | n/a | Only an `extern func`; any other is an error                                                |
| `extern func`            | yes   | yes    | yes     | yes | Body in C, linked from the package directory's `.c` files; every type crosses by value but a list, several results as one struct; a `mut` parameter is a `T *`, a list's a `tack_list_<T> *` C grows with std's `TACK_LIST_APPEND`; a view parameter a `tack_view_<T>`, read during the call only, and a variadic one too; an `any` a `tack_any`, read through std's `tack.h` helpers during the call only; an `error` a `tack_error`, returned last; C name is the Tack name or `extern("name")`'s, unique in the program, not `tack_` outside std; never `pub` or `lib` |
| C files and `tack.h`     | n/a   | n/a    | n/a     | yes | Every `.c` in a package directory, or beside a single file, compiled with `clang -w -c`, at the build's `-O` level, against the generated `tack.h`, which defines the types the extern funcs use (`Point`, `lexer_Token`, `tack_array_4_int64`, `tack_view_int64`, `tack_results_f`); a missing definition is a diagnostic at the extern func; `--emit-header` writes `tack.h` for editors; when a signature uses a string it includes std's `<tack/tack.h>`, the one definition of `tack_string` and the string helpers `TACK_STR`, `TACK_STR_N` and `TACK_C_STR_LEN`, which the generated C includes too, so every C file is compiled with `-I $TACK_HOME/src/std/include`; all of extern C is in [extern.md](extern.md) |
| Constants                | yes   | no     | no      | no  |                                                                                             |
| Struct type declarations | yes   | yes    | yes     | yes | `type T struct { ... }`, package level only; any order; no recursion by value; field array lengths name no variable |
| Union type declarations  | yes   | yes    | yes     | yes | `type T union { a int64; b }`, package level only; any order; cases named by a field's rule, each with one payload type or none, as visible as the union; no recursion by value; no tag values or extern crossing; may have methods, none named like a case, and be a type argument |
| Other named types        | yes   | no     | no      | no  | `type C int64`, aliases and anonymous structs are rejected with a diagnostic                |
| Package-level variables  | yes   | yes    | yes     | yes | `var`/`mut` only; initializers are constant expressions computed at compile time: immutable globals of the package or an import, their fields and elements, `len`, conversions, operators and comparisons; no calls; usable from any function; never a list, or a struct holding one |
| Methods                  | yes   | yes    | yes     | yes | `func (b T) m()` and `func (mut b T) m()`, `pub`/`lib` capped by `T`'s visibility, on a struct or union type `T` of the same package, in any file and order; the receiver is the first parameter, under every parameter rule; called `x.m(args)`, on any value of `T`, a temporary included, or `mut x.m(args)` on a place, as a statement or a value, `mut` covering what comes before the last `.m(`; the receiver is a mut argument for exclusivity and list borrows; named in `T`'s namespace, not a field's, a case's or another method's name, a builtin's allowed; no method values or expressions, no extern methods; a C function `tack_<path><T.m>` taking the receiver first, a pointer for `mut`, evaluated first |
| Interface declarations   | yes   | yes    | yes     | n/a | `type I interface { mut m(p T) R; n() }`, package level only; methods only, no fields, embedding, bodies or per-method visibility; satisfied implicitly by a struct or union type with each method, same signature and receiver `mut`, visible from the package of the call choosing the type; used only as a constraint |
| Generic functions        | yes   | yes    | yes     | yes | `func f[W I, V J](...)`, functions only, not methods or extern funcs; every type parameter constrained by an interface and the type of some parameter; body checked once against the constraints; type arguments inferred from the arguments, struct or union types or the caller's type parameters, never written; monomorphized, one C function per instance, `f[pkg.T]` mangled, each emitted once; at most 10000 instances |

### Statements

| Feature                    | Lexer | Parser | Checker | C   | Notes                                                                                                         |
|----------------------------|-------|--------|---------|-----|---------------------------------------------------------------------------------------------------------------|
| Call statement             | yes   | yes    | yes     | yes | A function's or a method's, `x.m()` or `mut x.m()`; a result may be discarded, but not an error, nor a list or a struct holding one |
| `return`                   | yes   | yes    | yes     | yes | One value for each declared result; `return f()` cannot forward another call's results                        |
| Several results `a, b :=`  | yes   | yes    | yes     | yes | One call with exactly that many results; `mut` per name; no `var` form, no `x, y := 1, 2`                     |
| Several results `a, b =`   | yes   | yes    | yes     | yes | Mutable targets, each named once, no `mut`; allowed as a loop's post statement                                |
| Blank `_`                  | yes   | yes    | yes     | yes | Only discards a value in `a, _ :=`, `a, _ =` or a two-name range; not a name elsewhere; not all `_`; never an error |
| `if` / `else` / `else if`  | yes   | yes    | yes     | yes | Condition must be a bool                                                                                      |
| Terminating statement rule | n/a   | n/a    | yes     | n/a | Syntactic, as in Go, including `for {}` without a `break`                                                     |
| Local variables            | yes   | yes    | yes     | yes | `x :=`, `var`, `mut`; immutable by default; zero values for `mut x T`, but none for a list, an `Allocator` or what holds one; no shadowing; unused is an error |
| Assignment `=`             | yes   | yes    | yes     | yes | Mutable variables and `mut` parameters only; other parameters are immutable                                   |
| Compound assignment        | yes   | yes    | yes     | yes | `+= -= *= /= %=` on any integer type; not a read of the target; bitwise forms come with the bitwise operators |
| `++` `--`                  | yes   | yes    | yes     | yes | Postfix statements only, never values; integer targets; not a read of the target                              |
| `for cond {}`, `for {}`    | yes   | yes    | yes     | yes | Condition must be a bool                                                                                      |
| Three-clause `for`         | yes   | yes    | yes     | yes | Go's rules; parts optional; init variable scoped to the loop; a counter is declared with `mut`; a counting loop proves its variable in the body |
| `range` over an integer    | yes   | yes    | yes     | yes | `for i := range n`, `for range n`; `n` evaluated once; `i` a fresh immutable variable of `n`'s type           |
| `range` over array/string/view/list | yes | yes | yes   | yes | `for i, x := range a`, `_` index; evaluated once into a copy; `int64` index; string bytes `uint8`; a view's and a list's elements read in place, the list unchangeable in the body |
| `switch`                   | yes   | yes    | yes     | yes | Tagged and tagless; no fallthrough; `break`/`continue` act on the loop; no init statement; integer case values of any type, by value; lowered to C ifs   |
| Type switch                | yes   | yes    | yes     | yes | `switch v := a.(type)` on an `any`; a case's types exact, none twice; `v` the case's one type or the `any`; no facts; lowered to C ifs on the type id |
| Union switch               | yes   | yes    | yes     | yes | `switch e { case int(n): ... case a, b: ... }` on a union; every case covered or a default, never both; none twice; a one-case clause binds its payload in place, immutable, and cannot change the switched place; no facts; lowered to C ifs on the tag, the last clause the final else when every case is covered |
| Type assertion             | yes   | yes    | yes     | yes | `v, ok := a.(T)` only, a checked form; exact type; `T`'s zero value on a mismatch; `T` one of the twelve types an `any` holds |
| `break` / `continue`       | yes   | yes    | yes     | yes | Innermost loop only; no labels                                                                                |
| Standalone blocks          | yes   | no     | no      | no  |                                                                                                               |
| `defer`                    | yes   | yes    | yes     | yes | `defer { ... }` only, not in a defer; block-scoped, as in Zig; runs at block exit, last first, interleaved with the frees of lists by position; `exit` skips; reads no list moved after it; moves no list declared outside it |
| `select`, `goto`           | yes   | no     | no      | no  |                                                                                                               |

### Expressions

| Feature                           | Lexer | Parser | Checker | C       | Notes                                                                                                                                                                   |
|-----------------------------------|-------|--------|---------|---------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Integer and string literals       | yes   | yes    | yes     | yes     |                                                                                                                                                                         |
| Parameter and variable references | yes   | yes    | yes     | yes     | Parameters and variables are the only names with values                                                                                                                 |
| Qualified names `x.name`          | yes   | yes    | yes     | yes     | Through an import, for functions, globals and types, in calls, values, targets, types and literals; C names by package path and name, length-prefixed so none collide |
| Calls                             | yes   | yes    | yes     | yes     | To a function by name; arity and argument types checked; any number of values for a variadic parameter, or a spread `v...` after the last argument                    |
| `mut` arguments `mut x`           | yes   | yes    | yes     | yes     | Only to a `mut` parameter, of exactly its type; a place the caller may change; `&place` in C; ends the facts about the variable for its region                          |
| Parentheses                       | yes   | yes    | yes     | yes     |                                                                                                                                                                         |
| `+ - * / %`                       | yes   | yes    | yes     | yes     | Integer operands, widened to a common type; on known values, overflow is an error; otherwise it wraps at the type's width; divisor proven non-zero, or `q, ok := a / b` |
| `== != < <= > >=`                 | yes   | yes    | yes     | yes     | Two integers of any types, all six, by value (`int64(-1) < uint64(0)`); otherwise one type after widening: strings all six, `bool`, arrays, structs and unions `==` `!=` only; strings by unsigned bytes, folded only in a global's initializer |
| `!`                               | yes   | yes    | yes     | yes     | `bool` operand only                                                                                                                                                     |
| Unary `-`                         | yes   | yes    | yes     | yes     | Signed integer operands only; the smallest known value is an overflow error; `-` before digits is a negative literal                                                    |
| Array literals `[n]T{...}`        | yes   | yes    | yes     | yes     | Exactly one element each; `[...]` from the elements; inner types may be left out                                                                                        |
| Struct literals `T{f: v}`         | yes   | yes    | yes     | yes     | Every field named once, any order; parenthesized in an `if`/`for`/range/`switch` header; type left out only inside an array literal                                              |
| Field selection `x.f`             | yes   | yes    | yes     | yes     | On any struct value; written through a `mut` variable, mixed with indexes to any depth; facts for proofs below an immutable variable only                                  |
| Indexing `a[i]`                   | yes   | yes    | yes     | yes     | Proven in range, from constants, range loops (over a count or an array), guards and types, or `v, ok := a[i]`                                                           |
| Indexing a string `s[i]`          | yes   | yes    | yes     | yes     | A `uint8`; proven from a known string's length, `len(s)` guards and range loops over `len(s)` or `s`, or `v, ok := s[i]`; never assigned                               |
| Slicing `x[i:j]`                  | yes   | yes    | yes     | yes     | Strings, arrays, views and lists; `0 <= i <= j <= len(x)` proven, with `j <= len(x)` and `i <= j` relations, or `t, ok := s[i:j]` for a string; constant mistakes are errors; never a place; a helper per type in C that never moves a null pointer |
| Conversions `T(x)`                | yes   | yes    | yes     | yes     | Integer to integer; `T(x)` only where it cannot fail (a constant or known value that fits, or a widening type), otherwise `v, ok := T(x)`                                |
| Integer constants                 | yes   | yes    | yes     | yes     | Untyped, exact, between the smallest `int64` and the largest `uint64`; take their type from use, else `int64`                                                           |
| Unary `+` `^`                     | yes   | no     | no      | no      | Decided: no unary `+` for now                                                                                                                                           |
| `&&` `\|\|`                       | yes   | yes    | yes     | yes     | `bool` operands only; short-circuit                                                                                                                                     |
| Bitwise and shift operators       | yes   | no     | no      | no      |                                                                                                                                                                         |
| `true` / `false`                  | yes   | yes    | yes     | yes     | Keywords, so not usable as names; constant expressions                                                                                                                  |
| `noerror`                         | yes   | yes    | yes     | yes     | Keyword, the `error` with code 0 and an empty message; a constant expression; a zeroed `tack_error` in C                                                                |
| `c_allocator`                     | yes   | yes    | yes     | yes     | Keyword, the `Allocator` lowering to `malloc`, `realloc` and `free`; a constant expression; `TACK_C_ALLOCATOR` in C                                                     |
| `make(list[T], alloc)`            | yes   | yes    | yes     | yes     | An empty list, which allocates nothing; the list type as written; no capacity yet                                                                                       |
| `append(mut l, v)`                | yes   | yes    | yes     | yes     | Variadic: any number of values, none included, or a spread view, `append(mut l, v...)`; grows `l` once through its allocator; an `error`, code 1 "out of memory", the list unchanged, all of it, when it cannot; never dropped; std's `TACK_LIST_APPEND` and `TACK_LIST_APPEND_N` in C |
| `reserve(mut l, n)`               | yes   | yes    | yes     | yes     | Room for `n` more elements, capacity at least `len + n`, growing as append does; a negative constant `n` is a compile error; at run time code 2 "negative count", or code 1 "out of memory", the list unchanged; never dropped; std's `TACK_LIST_RESERVE` in C |
| Evaluation order                  | n/a   | n/a    | n/a     | yes     | Left to right; the C backend holds operands in temporaries where the order could show, a variable passed as `mut` anywhere in the function counting as changeable       |

### Types

| Feature                            | Checker | C   | Notes                                                             |
|------------------------------------|---------|-----|-------------------------------------------------------------------|
| `int8` `int16` `int32` `int64`     | yes     | yes | Signed, `int8_t` to `int64_t`                                     |
| `uint8` `uint16` `uint32` `uint64` | yes     | yes | Unsigned, `uint8_t` to `uint64_t`; no `byte`                      |
| `unsafe_size_t` `unsafe_uintptr_t` | yes     | yes | Unsigned, the target's width (32 to 64 bits), `size_t` and `uintptr_t`; `uint8` to `uint32` widen in, they widen to `uint64`, not to each other; constants at most 4294967295; compared with any integer by value; an index, proven or checked as a `uint64` one is |
| Lossless widening                  | yes     | yes | Implicit wherever a value is used, when it holds on every target; not needed to compare two integers; no `int` |
| `string`                           | yes     | yes | Pointer and length, `tack_string` by value; immutable bytes; all six comparisons, by unsigned bytes |
| `bool`                             | yes     | yes | Compared with `==` `!=`; combined with `&&` `\|\|` `!`            |
| Float types                        | no      | no  |                                                                   |
| Arrays `[n]T`                      | yes     | yes | Values, a C struct each; `==` element by element; constant length |
| Structs                            | yes     | yes | Values, a C struct each; `==` field by field; no embedding        |
| Views `[]T`                        | yes     | yes | Parameters only, read-only; a pointer to const elements and a length by value; no `==` |
| `any`                              | yes     | yes | Parameters, `...any` and `[]any` only, and immutable locals declared from one; takes a value of a basic type, an untyped constant as `int64`, a copy taken at the call; no arrays, structs or views in it yet; never `mut`; in Tack passed on, or asked with `v, ok := a.(T)` and `switch v := a.(type)`, no `==`, expression switch or conversion; `tack_any`, `{uint32_t type; const void *ptr;}` by value, with fixed type ids 1 to 12, in C |
| `error`                            | yes     | yes | A struct of `code int32` and `msg string`, code 0 being no error; a literal names both, `noerror` is none; `==` as a struct's; the last result of a function that returns one, never dropped, and the results beside it read only where a check proves `err.code == 0`, which only a binding of them all can undo; `tack_error`, defined in std's `tack.h`, by value in C, with `TACK_ERROR` and `TACK_NO_ERROR` |
| Slices                             | yes     | yes | `x[i:j]`, either bound left out; a string's slice is a string sharing its bytes; an array's or a view's a view, passed to a view parameter or `len` only; no three-index form |
| Unions                             | yes     | yes | Values, a C struct each of a tag and a C union of payloads; built as `T.c(payload)` or `T.c`; `==` case and payload; no zero value; owned when a payload holds a list, freed by the case held; a global when constant and holding no list; methods and type arguments as a struct's; no `any`, conversion or C crossing |
| Lists `list[T]`                    | yes     | yes | Owned and growable, through an `Allocator`; elements copyable; move on assignment, declaration, return and struct field, use after move an error, conditional moves allowed; borrowed by plain parameters, grown through `mut` ones, viewed by view parameters, never alongside a `mut` argument of the same list; freed when the owner's block ends, as a defer, and when assigned; no `==`, zero value or global; structs holding them likewise; `tack_list_<T>`, `{T *ptr; int64_t len; int64_t cap; tack_allocator alloc;}`, in C |
| `Allocator`                        | yes     | yes | An opaque handle, copyable, with no zero value or `==`; `c_allocator` only; `tack_allocator`, `{uint32_t index; uint32_t gen;}`, in C |
| Maps                               | no      | no  |                                                                   |
| Pointers                           | no      | no  | None; a `mut` parameter is a reference that cannot be stored      |
| Interfaces                         | yes     | n/a | Constraints of type parameters only, never a value's type; nothing of them in C |
| Type parameters                    | yes     | yes | Within a generic function; owned as a list is, no `==`, zero value, field, element or `any`; the constraint's methods only; bound to its type argument in each instance |

### Builtins

| Builtin         | Checker | C   | Notes                                                                                   |
|-----------------|---------|-----|-----------------------------------------------------------------------------------------|
| `print(string)` | yes     | yes | Any string expression; writes exactly it to stdout, no newline; extern C, `tack_builtin_print`, declared in `src/std/builtin/builtin.tck`, body in `src/std/builtin/print.c`, linked only when called |
| `len(v)`        | yes     | yes | Arrays, strings, views and lists; known for arrays, string literals and immutable variables holding a known string, never for a view or a list, but for a slice at known bounds |
| `make`, `append`, `reserve` | yes    | yes | Generic, in the compiler, described in `builtin.tck`; a list's C is `src/std/builtin/list.c`, linked for any program with a list type |
| `exit(int32)`   | yes     | yes | Terminates with that status; narrower integers widen, and an `int64`, `uint32` or `uint64` needs a conversion; a stand-in for a future `os.Exit`; extern C, `tack_builtin_exit`, declared in `src/std/builtin/builtin.tck`, body in `src/std/builtin/exit.c`, linked only when called |

The compiler reads the builtins from `$TACK_HOME/src/std/builtin`
(`~/.tack` without `TACK_HOME`), installed by `make install`; it holds no
list of its own. Wanted builtins are listed in `src/std/builtin/PLANNED.md`.

### Runtime and memory

| Feature                     | Status | Notes                                                  |
|-----------------------------|--------|--------------------------------------------------------|
| Runtime                     | no     | Generated C calls libc directly                        |
| Memory model and allocation | partial | Lists own heap memory, through `c_allocator`, the one allocator, and free it automatically; strings are static; no other allocators yet |
| Standard library            | partial | `src/std`, installed to `$TACK_HOME/src/std`, the module `std`: `std/builtin`, the builtins and their C bodies, and `std/fmt`, importable, with `println`, `write`, which writes a `[]uint8` view's bytes exactly, and `printf`, whose verbs `%d %x %s %t %c %v %%` are checked against a known format at compile time and written as Go-style markers otherwise; no `sprintf` or `appendf`; and `std/bytes`, written in Tack alone, with `equal`, `index`, `index_byte`, `has_prefix`, `has_suffix` and `count` over `[]uint8` views, `append_int`, `append_uint` and `append_hex` into a `list[uint8]`, all or nothing, and `repeat`, which makes a list through a given `Allocator` |
| Packages beyond `main`      | partial | Modules, imports, qualified names and `pub`/`lib` visibility; std packages imported from any module or a single file, `lib` hiding std's internals |
| Concurrency                 | no     |                                                        |

### Toolchain

| Feature                            | Status  | Notes                                                                      |
|------------------------------------|---------|----------------------------------------------------------------------------|
| C backend                          | yes     | Needs `clang` on `PATH`                                                    |
| Other backends                     | no      | The registry allows them; arm64 was dropped                                |
| `tack` driver                      | yes     | Build, stage dumps, `--max-diags`, exit codes 0/1/2; a file, a directory or an import path |
| Diagnostics with source context    | yes     | Stops at the first stage that reports; reports every problem in that stage, each once, and nothing that follows from one reported |
| Build cache                        | partial | std/builtin's objects, keyed on their inputs and the toolchain, in `$TACK_CACHE` or the user cache directory; the program's C, and std packages' C, are compiled every build; nothing trims it |
| `make build/install/test/fmt/lint` | yes     |                                                                            |
| Golden tests                       | yes     | Every program under `examples/`, every case in `examples/invalid.txtar` and `examples/invalid-packages.txtar` |
| Fuzzing                            | partial | Lexer alone, the parser with a comment at any token boundary and on generics, methods and unions, the whole pipeline through the driver, a package's file order, `tack.mod`, the loader on random file trees, C name mangling, constant arithmetic, and printf's format check |
| Self-hosting                       | no      | The goal: a Tack compiler written in Tack                                  |

## Timeline

To be added.
