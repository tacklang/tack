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

## Functions without a body

The parser accepts `func f()` with no body, which is how
`src/std/builtin/builtin.tck` declares the builtins. The rest of the
pipeline then treats a missing body as an empty one, so `func f()` compiles
to a function that does nothing, and `func main()` alone compiles to a
program that does nothing. In Go a bodyless declaration means the body is
provided elsewhere, in assembly. Options: reject them outside declaration
files, or give them a meaning (external, `asm`, builtin).

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

## Packages and imports

Only `package main` exists today, and nothing can be imported. The
direction is chosen: a package is imported under a namespace, as in
`import x from "my/x/package"`, and its exported names are used through
that namespace, never brought in bare. A package's names therefore never
collide with a program's own; only the namespace name can, and it is
declared like any other name. This is why a variable may not shadow
another name visible where it is declared: with imports namespaced,
nothing a package adds can make that rule bite, so it stays.

Still open: the exact syntax, whether the namespace may be left out to
default to the package's own name, what makes a name exported, and how
a path maps to files.
