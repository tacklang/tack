# Open language decisions

Questions the compiler currently answers by accident, and which need a
deliberate answer. Each entry says what happens today and why it matters.
When a decision is made, the entry is removed and the answer goes into the
README's "The language today".

## The `asm` keyword

`asm` is a keyword, and the lexer skips everything after it up to the next
`}` without tokenizing it; one with no `}` is an error. Nothing past the
lexer handles it. Is `asm` part of the language, and if so, what is its
syntax?

## Wording of the invalid character diagnostic

A byte the lexer does not accept is reported as `invalid character %c`
formatted from a single byte, so 0xFF prints as `invalid character ÿ` and a
UTF-8 letter such as `é` produces two diagnostics showing unrelated
characters. Showing the byte in hex, or decoding UTF-8 first, are options.

## Missing explanation notes

An error for a missing proof comes with notes when a guard in scope says
something about the operand (see [safety.md](safety.md#notes-on-a-missing-proof)).
Some causes still get the error alone, because the cause leaves no fact
behind, so explaining it means walking the condition again with logic that
mirrors how facts are derived. The wording for each was drafted and its
implementation deferred:

- `||` where one side says nothing about the variable, as in
  `if b < 0 || c > 0 { a / b }` (and `&&` when false): a note that the
  condition proves nothing about `b`, since the other side may be the one
  that holds.
- A comparison with a value that is not known, as in `if 0 <= i && i < n`
  or `if b > c`: a note that the comparison gives no fact about `i`, since
  `n` has no known value.
- `len(s)` in a form that gives no fact, as in `i <= len(s) - 1` or
  `i <= len(s)`, and `len` of a call, `i < len(name())`: a note that only
  `i < len(s)` of a string variable bounds `i` by its length.
- A string equality, as in `if s == "abc" { s[0] }`: a note that comparing
  strings says nothing about `len(s)`.

## Structs

Structs follow the decisions recorded in the README. One point was
settled by the implementation rather than decided, and could go another
way:

- A struct holding itself through an array of length 0 is rejected like
  any other cycle, as in Go.

Decided, not yet implemented: a field of an immutable variable is to
carry facts for proofs, so that `if p.d != 0 { n / p.d }` compiles when
`p` is immutable. It is to be implemented with pointers. Today a field
never carries a fact, mutable or not, so a guard on `p.x` proves nothing.

## Mut parameters

Mut parameters follow the decisions recorded in the README. These points
were settled by the implementation rather than decided, and could go
another way:

- A `mut` argument's type must be its parameter's exactly: a `mut int32`
  variable cannot be passed to `mut n int64`, since a reference to one
  cannot stand for a reference to the other. Widening still applies to
  plain arguments.
- `mut` is parsed before any operand, binding as a prefix operator does,
  so `mut a + 1` is `(mut a) + 1`; anywhere but a call's argument it is the
  type checker's error, `mut a can only be passed to a mut parameter`, and
  `mut mut x` is that error once.
- An immutable global passed as `mut` gets its own wording, `cannot pass
  mut limit: limit is immutable; copy it into a mutable local`, rather
  than the local's `declare it with mut`, which would lead to the mutable
  global error.
- Leaving `mut` out for a `mut` parameter suggests `pass mut b` when the
  argument is a variable, a field or an element, and otherwise says only
  a place can be passed as `mut`.
- `mut x` counts as reading `x` for the unused-variable check.
- A plain parameter is always passed by value in C, a copy, even when
  large; passing it by address where nothing overlaps, which the language
  allows, is not done.
- In the C backend, a variable a function passes as `mut` anywhere is
  treated as changeable by any call in that function when deciding where
  evaluation order could show, so its reads around such calls are held in
  temporaries; this is coarser than needed and costs only temporaries.

## Extern C

Extern C follows the rules recorded in [extern.md](extern.md). These
points were settled by the implementation rather than decided, and could
go another way:

- `extern` is a keyword reserved everywhere, as `from` is, rather than a
  word that means something only before `func`.
- An extern func whose C name C cannot define is an error: a C keyword
  (C23's, with `asm` and `typeof`), and `main`, which the generated C
  defines. A name a C header declares, such as `printf` or `int64_t`, is
  not checked; it clashes only with what a C file declares.
- A missing C definition is found when building, from the compiled
  objects' symbol tables; `--emit` does not compile C files, so it does
  not report one.
- One `tack.h` serves the whole program, its extern funcs package by
  package. `--emit-header` writes it into the target's directory and into
  every package directory with C files or extern funcs.
- C files are compiled with `-w`, as the generated C is, so clang's
  warnings about the user's own C are not shown; its errors are. There is
  no way yet to pass a C file other flags, or to link a library.
- tack.h's prototypes leave out the parameters' names, which could be C
  keywords or clash with a type's name; the Tack declaration is in a
  comment beside each.
- tack.h names an array type, which has no Tack name, `tack_array_`, its
  length and its element type's name (`tack_array_4_int64`,
  `tack_array_2_string`), and an extern func's results struct
  `tack_results_` and its C name, with members `r0`, `r1` and on.
- A struct's members in tack.h are its fields' Tack names, so a field
  named with a C keyword, or a type of package main named with one, is an
  error when an extern signature uses its struct, rather than being
  renamed. tack.h's own names, `tack_string` and the guard `TACK_H`, are
  errors for a type of package main too.
- A string C returns is taken as it is: nothing says who owns its bytes or
  how long they live, which the memory model will have to.
- The builtins' C bodies are part of the C backend, one embedded file per
  builtin in `codegen/c/builtin/`, written into the generated C as static
  functions when called, rather than C files of `src/std/builtin` compiled
  and linked like a package's: `go:embed` cannot reach `src/`, and the
  generated C stays one self-contained file. Their C names are
  `tack_builtin_print` and `tack_builtin_exit`. `exit`'s body is not
  declared `_Noreturn`, as nothing depends on it.
