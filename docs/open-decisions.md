# Open language decisions

Questions the compiler currently answers by accident, and which need a
deliberate answer. Each entry says what happens today and why it matters.
When a decision is made, the entry is removed and the answer goes into the
README's "The language today".

## Evaluation order

C does not specify the order in which a call's arguments, or a binary
expression's operands, are evaluated. Tack functions can print and also
return a value, so the order is observable:

```tack
func a() int64 {
    print("a")
    return 1
}

func b() int64 {
    print("b")
    return 2
}

func main() {
    exit(a() + b())
}
```

may print `a` then `b`, or `b` then `a`. Go fixes left to right. Fixing any
order means the backend evaluates operands into temporaries first.

## The `asm` keyword

`asm` is a keyword, and the lexer skips everything after it up to the next
`}` without tokenizing it; one with no `}` is an error. Nothing past the
lexer handles it. Is `asm` part of the language, and if so, what is its
syntax?

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

## Integer literals with leading zeros

`010` is the decimal number 10. In Go (and C) it is octal 8, and Go also
accepts `0o10`. Hex, octal and binary literals are not lexed at all.

## Structs

Structs follow the decisions recorded in the README. These points were
settled by the implementation rather than decided, and each could go
another way:

- A struct literal in the header of an `if`, a `for` or a range loop must
  be in parentheses, as in Go, since `if p == Point{x: 1} {` would
  otherwise be ambiguous. `if p == Point{x: 1} {` is reported as needing
  parentheses when the body would start with a name and a colon; other
  forms, such as `if e == Empty{} {`, give a plain syntax error.
- Inside an array literal, a struct literal may leave out its type,
  `[2]Point{{x: 1, y: 2}, {x: 3, y: 4}}`, extending the rule that let an
  inner array literal do so. A field's value may not.
- `struct {}` is allowed, as in Go: its values are all equal, and the C
  backend gives it one unused member, since C has no empty struct.
- A struct holding itself through an array of length 0 is rejected like
  any other cycle, as in Go.
- `_` cannot name a field, following the rule that `_` is not a name;
  Go allows blank fields, as padding.
- A key written in parentheses, `Point{(x): 1}`, is taken as the field
  name `x`, since parentheses leave no node behind. Go rejects it.
- The fields of a struct literal are evaluated in the order C chooses, as
  a call's arguments are (see evaluation order).
- A field never carries a fact for proving a divisor non-zero or an index
  in range, so a guard on `p.x` proves nothing; whether a field of an
  immutable variable should is open.
