# Open language decisions

Questions the compiler currently answers by accident, and which need a
deliberate answer. Each entry says what happens today and why it matters.
When a decision is made, the entry is removed and the answer goes into the
README's "The language today".

## Integer semantics

The C backend emits `+ - * / %` as plain `int64_t` operations. Signed
overflow, division by zero, and `min / -1` are undefined behavior in C, so
what a Tack program does in those cases is whatever clang happens to do,
including at different optimization levels.

Options include wrapping (two's complement, as Go does for overflow), trapping
at runtime, or a mix (wrap on overflow, trap on division by zero, as Go does).
Every option except leaving it undefined needs code emitted around the
operators.

## Evaluation order

C does not specify the order in which a call's arguments, or a binary
expression's operands, are evaluated. Tack functions can print and also
return a value, so the order is observable:

```tack
func a() int {
    print("a")
    return 1
}

func b() int {
    print("b")
    return 2
}

func main() {
    exit(a() + b())
}
```

may print `a` then `b`, or `b` then `a`. Go fixes left to right. Fixing any
order means the backend evaluates operands into temporaries first.

## Escape sequences in raw strings

The lexer decodes escape sequences in backquoted strings as well as
double-quoted ones. In Go, a backquoted string is raw: a backslash is just a
backslash. Nothing documents which Tack intends.

## The `asm` keyword

`asm` is a keyword, and the lexer skips everything after it up to the next
`}` without tokenizing it. An `asm` with no `}` swallows the rest of the file
without a diagnostic. Nothing past the lexer handles it. Is `asm` part of the
language, and if so, what is its syntax?

## Block comments

`/* ... */` is reported as "block comments are not supported" and skipped.
Whether Tack has block comments is open.

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

## A result on `main`

`func main() int { return 1 }` is accepted. The generated C calls it and
discards the value, so the process still exits 0. Go rejects a result on
`main`. Options: reject it, or make it the exit status.

## Integer literals with leading zeros

`010` is the decimal number 10. In Go (and C) it is octal 8, and Go also
accepts `0o10`. Hex, octal and binary literals are not lexed at all.
