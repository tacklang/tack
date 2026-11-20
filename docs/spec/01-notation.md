# 1. Notation

This chapter defines how the rest of the specification is written: the
notation of its grammar productions, and the terms every other chapter
uses without defining them again, such as "valid program", "place",
"owned", "constant expression", "error" and "warning". It holds no rule
of the language itself; a term whose rules take more than a sentence is
defined here in a sentence and described in full in the chapter that
owns it, linked from here.

## Grammar notation

The syntax is specified using a variant of Extended Backus-Naur Form
(EBNF), the notation of the Go specification:

```text
Syntax      = { Production } .
Production  = production_name "=" [ Expression ] "." .
Expression  = Term { "|" Term } .
Term        = Factor { Factor } .
Factor      = production_name | token [ "..." token ] | Group | Option | Repetition .
Group       = "(" Expression ")" .
Option      = "[" Expression "]" .
Repetition  = "{" Expression "}" .
```

Productions are expressions constructed from terms and the following
operators, in increasing precedence:

```text
|   alternation
()  grouping
[]  option (0 or 1 times)
{}  repetition (0 to n times)
```

Lowercase production names identify lexical tokens, the units
[Source and lexical elements](02-lexical.md) defines. Names in
CamelCase identify syntactic constructs, made of tokens. A token is
written in double quotes, `"package"`, or, for a quote or a backslash
that would end or escape it, in backquotes, as Go writes them:
`` `"` ``. The form `a ... b`, an unquoted `...` between two quoted
single characters, stands for the set of characters from `a` through
`b`, inclusive, as alternatives; a quoted `"..."` is the three-period
token of that spelling. A comment in a production, `/* ... */`, is
informal prose that gives a set of bytes the notation cannot spell
compactly.

A production states which sequences are well formed. A rule stated in
prose alongside it may reject a sequence the production allows; the
prose names the diagnostic when it does.

## Examples in this specification

An example is a fenced block of Tack whose info string says what the
compiler must do with it: build and run it to a given exit status,
accept it, reject it with given diagnostics, accept it with given
warnings, or nothing at all, for a fragment. Every example but a
fragment is compiled by the specification's own test, so it shows what
the compiler does today. The [index](README.md) lists the tags and what
each requires.

A diagnostics block under an example shows the first line of each
diagnostic the compiler prints, in order: the file, line and column, the
severity and the message, as [Diagnostics](15-diagnostics.md)
describes. The source lines the compiler prints under each line are
left out.

## Terminology

### Valid program

A valid program is one the compiler accepts: every stage reports no
error about it, though it may report warnings. Every rule this
specification states about what a program does at run time is a rule
about a valid program. The compiler guarantees four things of every
valid program: it never accesses memory out of bounds, never leaks
memory, never uses memory after it is freed or moved, and never panics;
[Safety and proofs](10-safety.md) states them and how the compiler keeps
them. When the compiler cannot prove an operation safe, the program is
not valid.

### Place

A place is an expression that denotes a location holding a value,
rather than a value computed: a variable or a parameter, or a field or
an element of a place. A place that a `mut` argument can name is a
mutable place: a mutable local variable, a `mut` parameter, a field of
one, or an element of one whose index is proven in range; an immutable
variable, a union pattern's binding, a view's element and a string's
byte are not. [Ownership and memory](08-ownership.md) describes which
places each type has and what may be done with them.

### Owned value

An owned value is a value of an owned type: a list, a map, a value of a
type parameter, or a struct, union or array that holds an owned value,
directly or through another. An owned value moves rather than copies,
has no zero value, and is freed when its owner's block ends.
[Ownership and memory](08-ownership.md) describes ownership in full.

### Constant expression

A constant expression is an expression whose value the compiler
computes in full at compile time, with nothing run. Which expressions
are constant, and how each is computed, is stated in [Constants and
arithmetic](03-arithmetic.md); what a package-level variable's
initializer may hold is stated in [Declarations and
scope](05-declarations.md).

### Error, warning and note

A diagnostic is a problem the compiler reports, at a span of the
source. An error rejects the program: a program with one is not valid
and is never built. A warning reports something suspicious that rejects
nothing: a program with only warnings is valid and builds as one with
none does. A note adds an explanation to an error or a warning and
never stands alone. [Diagnostics](15-diagnostics.md) describes how
they are reported.

## Implementation-defined and target-dependent behavior

What a valid program computes does not depend on the target, with these
exceptions:

- The target-width integer types, `unsafe_size_t` and
  `unsafe_uintptr_t`, are as wide as the target makes C's `size_t` and
  `uintptr_t`, from 32 to 64 bits. Their rules are written to hold at
  every width, so a program valid for a 64-bit target is valid for a
  32-bit one; the value a wrapping operation on them gives depends on
  the width (see [Integer overflow](03-arithmetic.md#integer-overflow)). [Types](04-types.md) describes them.
- The sizes of values, against which the compiler checks the size
  limits, are those of the 64-bit targets the compiler builds for,
  arm64 and x86-64.
- Only the low bits of the status given to `exit` reach the operating
  system: the low 8 on Linux and macOS. [Builtins](13-builtins.md)
  describes `exit`.
- Code behind an extern C function does whatever its C does, outside
  every rule of this specification; the Tack side of the call is not.
  [Extern C](14-extern.md) describes the boundary.
