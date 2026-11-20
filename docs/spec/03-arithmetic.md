# 3. Constants and arithmetic

This chapter describes values of the numeric and boolean kinds and what
the operators compute on them: constant expressions and how they are
evaluated at compile time, the type a constant takes, integer overflow,
wrapping arithmetic, division and remainder, shifts and bitwise
operators, what comparing two
integers or two floats computes, conversions `T(x)` from one numeric
type to another (integer to integer, integer to float, float to integer
and float to float) and what each computes, and float semantics. It
states what each operation yields and when it is an error. Which integer
type may be used where another is expected without a conversion belongs
in [Types](04-types.md), under assignability, as does what comparing
strings, `bool`s, arrays, lists, structs or unions computes; comparing
two errors belongs in [Errors](09-errors.md); the conversion of a
string to a view of its bytes, `[]uint8(s)`, belongs in [Ownership and
memory](08-ownership.md), with views. When an operation must be proven
safe or checked, what the proof needs (a divisor is not zero, a
conversion fits), and the checked forms such as `v, ok := T(x)`,
belong in [Safety and proofs](10-safety.md). The spelling of literals belongs
in [Source and lexical elements](02-lexical.md), and the operators'
syntax and precedence in [Expressions](06-expressions.md).

## Constant expressions

An integer literal, and any expression made only of integer literals and
operators, is an untyped integer constant. It has no type of its own and
is computed exactly, as a mathematical integer, at compile time. Every
literal and every step of the computation must lie between the smallest
`int64`, -9223372036854775808, and the largest `uint64`,
18446744073709551615; a step outside that range is an error, `constant
expression is out of range`, rather than a larger number. A step may
leave the range of every signed type, or of every unsigned type, so long
as it stays within those bounds.

A float literal, and any expression made only of float and integer
literals and operators that holds at least one float literal, is an
untyped float constant. It is computed exactly, as a rational number,
until it is given a type. Its numerator and its denominator are each held
in at most 8192 bits; a literal past that is an error, `float literal
1e3000 is too large or too precise to hold exactly`.

```tack run exit=0
package main

func main() {
    half := (18446744073709551615 - 1) / 2 - 9223372036854775807
    if half != 0 {
        exit(1)
    }
    if 0.1 + 0.2 != 0.3 {
        exit(2)
    }
    if 1 / 2 + 0.5 != 0.5 {
        exit(3)
    }
    exit(0)
}
```

A character literal is a constant too, but a typed one: its type is
`uint8` and its value the value of its one byte.

The keywords `true` and `false` are constant expressions of type `bool`,
`noerror` one of type `error` (see [Errors](09-errors.md)), and
`c_allocator` one of type `Allocator` (see [Types](04-types.md)).

A constant expression is an expression the compiler computes in full.
It is one built only from:

- integer, float, character, string and bool literals, and the keywords
  above;
- names of immutable package-level variables, of the package or of an
  import;
- array and struct literals of constant expressions, and a union built
  from a case whose payload is a constant expression, or that has none;
- a field of a constant expression, an element of one at a constant
  index, and a byte of a constant string at a constant index;
- a slice of a constant string at constant bounds, `s[1:3]`;
- `len` of a constant expression, and `len` of any array, a mutable one
  included, when computing it calls nothing;
- a conversion `T(x)` of a constant expression to an integer type;
- the arithmetic, wrapping, bitwise and shift operators, the
  comparisons, and `&&`, `||` and `!`, on constant expressions, with one
  exception: `^x`, and a wrapping operator, of a target-width type is
  not a constant expression, since its value depends on the target.

A value of a float type is constant only as an untyped float constant
given that type, a name of an immutable package-level variable, or a
field or an element of a constant expression. A conversion to a float
type, and an operator or a comparison with an operand of a float type
that is not an untyped constant, are not constant expressions: with `a`
a `float64` global and `f` a `float32` one, `-a`, `a + a`, `a < 2.0`,
`float64(3) + 1.0` and `float64(f)` are each rejected as a
package-level variable's initializer, `... must be initialized with a
constant expression`.

```tack error
package main

var s = "hello"
var t = s[1:3]
var a float64 = 1.5
var half float64 = 0.5 + 0.25
var b = -a

func main() {
    if t == "el" && half == 0.75 {
        exit(0)
    }
}
```

```diagnostics
main.tck:7:5: warning: declared and not used: b
main.tck:7:9: error: b must be initialized with a constant expression
```

A package-level variable's initializer must be a constant expression
(see [Declarations and scope](05-declarations.md)).

Inside a function, the compiler also computes every operation on known
values, which [Safety and proofs](10-safety.md#known-values) defines,
such as constants, immutable variables holding them, `len` of an array
or of a known string, and arithmetic on those. An operation on known values is computed under the rules for
constants that follow, in a function as at package level, with the
exception for target-width types given under [Integer
overflow](#integer-overflow).

## Types of constants

An untyped integer constant takes the type of where it is used: the
type of the variable it is assigned to, of the parameter it is passed
to, of the field or element it initializes, of the other operand of a
binary operator, or of the result it is returned as. Where nothing gives
it a type, as in `x := 5`, or when it is passed as `any`, it is an
`int64`. An untyped constant shifted by a typed count is an `int64`, so
`1 << n` in a `uint64` is written `uint64(1) << n`.

An untyped float constant becomes a `float64` where nothing gives it a
type, and is rounded to the nearest value of the float type it is given.
An untyped integer constant can be used as a float, as in `f * 2`; an
untyped float constant is never used as an integer, not even `2.0`,
though it may be converted to one (see [Conversions](#conversions)).
Each operation of a constant expression is computed on its own operands,
so in `1 / 2 + 0.5` the integer division `1 / 2` is computed first, as
integers, giving 0, and the whole is 0.5.

A constant must be a value of the type it takes. One that is not is an
error, `constant 256 overflows uint8`, and a float constant too large for
its float type is `constant 1e+39 overflows float32`. For the
target-width types, `unsafe_size_t` and `unsafe_uintptr_t`, a constant is
a value of the type only from 0 to 4294967295, the most every target
holds: `constant 4294967296 overflows unsafe_size_t: its width is
unknown, so a constant must be at most 4294967295`.

```tack error
package main

func small() uint8 {
    return 256
}

func large() int64 {
    return 18446744073709551615
}

func size() unsafe_size_t {
    return 4294967296
}

func single() float32 {
    return 1e39
}

func main() {
    exit(0)
}
```

```diagnostics
main.tck:4:12: error: constant 256 overflows uint8
main.tck:8:12: error: constant 18446744073709551615 overflows int64
main.tck:12:12: error: constant 4294967296 overflows unsafe_size_t: its width is unknown, so a constant must be at most 4294967295
main.tck:16:12: error: constant 1e+39 overflows float32
```

A character literal's type is `uint8`, so an untyped constant beside it
takes `uint8`: in `'a' + 1` the `1` is a `uint8`, and the sum is
computed in `uint8`. A character literal widens wherever a wider integer
is expected, as in `var n int64 = 'a'`.

## Integer overflow

The arithmetic operators `+`, `-`, `*`, `/` and `%` take two integers and
give a value of their common type: the type of both operands, or the
wider when one widens to the other (see [Implicit
widening](04-types.md#implicit-widening)), so `int8 + int64` is an
`int64`. Two operands with no common type, such as an `int8` and a
`uint8`, or a `uint64` and an `int64`, are an error, `mismatched types
int8 and uint8 in +`. Unary `-x` means `0 - x` and takes a signed
integer only; on an unsigned one it is an error, `operator - is not
defined on uint8`, and `-%x` is written when the wrapping is meant.

`+`, `-`, `*` and unary `-` on integers never wrap. Each computes the
exact result, and the program compiles only where that result is proven
a value of the operation's type, a result below 0 being outside an
unsigned type: by the operands' known values, or by the facts in force
where the operation is written (see [Safety and
proofs](10-safety.md#arithmetic)). `x++`, `x--`, `x += v`, `x -= v` and
`x *= v` compute `x + 1`, `x - 1`, `x + v`, `x - v` and `x * v` and
need the same proof. Anything else is an error that names the operation
and what fixes it: `a + b may overflow int64; bound a and b first, or
use v, ok := a + b, or a +% b to wrap`, `a - b may go below 0 in
uint64; check a >= b first, or use v, ok := a - b, or a -% b to wrap`,
`x++ may overflow int64; use x +%= 1 to wrap, or v, ok := x + 1 and x =
v when ok`. A signed `/` can overflow at one quotient, the smallest value
of its type divided by -1, and needs a proof of its own (see [Division
and remainder](#division-and-remainder)); `%` never overflows.

```tack run exit=0
package main

func next(x int8) int8 {
    if x < 127 {
        return x + 1
    }
    return x
}

func distance(a uint64, b uint64) uint64 {
    if a >= b {
        return a - b
    }
    return b - a
}

func main() {
    if next(126) != 127 || next(127) != 127 {
        exit(1)
    }
    if distance(3, 10) != 7 || distance(10, 3) != 7 {
        exit(2)
    }
    exit(0)
}
```

Without the guards, neither compiles:

```tack error
package main

func sum(a int64, b int64) int64 {
    return a + b
}

func down(x uint8) uint8 {
    mut y := x
    y--
    return y
}

func main() {
    exit(0)
}
```

```diagnostics
main.tck:4:12: error: a + b may overflow int64; bound a and b first, or use v, ok := a + b, or a +% b to wrap
main.tck:9:5: error: y-- may go below 0 in uint8; check y > 0 first, use y -%= 1 to wrap, or v, ok := y - 1 and y = v when ok
```

The checked form, `v, ok := a + b`, also `a - b`, `a * b` and `-a`,
computes the operation whether or not it fits: `ok` reports whether the
exact result is a value of the type, and `v` is that result, or 0 when
it is not. Only the outermost operation is checked, so in `v, ok := a +
b * c` the product needs its own proof. It is described with the other
checked forms in [Safety and proofs](10-safety.md#the-checked-form).

The wrapping operators `+%`, `-%` and `*%`, their compound forms `+%=`,
`-%=` and `*%=`, and unary `-%x` compute in two's complement at their
type's width and wrap around it: the largest value `+% 1` is the
smallest, the smallest signed value `-% 1` is the largest, `-%` of the
smallest signed value is itself, and, on an unsigned type, `0 -% 1` is
its largest value and `-%x` is `0 -% x`. They need no proof. They take
integers only, combined as `+` `-` and `*` combine them, so `int8 +%
uint8` is `mismatched types int8 and uint8 in +%`, and a float is
`operator +% is not defined on float64`. Two untyped constants have no
width to wrap at, and are an error that names a conversion of one, `200
+% 100 wraps at its type's width, but both operands are untyped
constants; give one a type, as in int64(200) +% 100`. They bind as `+`
`-` and `*` do, and `-%x` as `-x`.

```tack run exit=0
package main

func wrap8(x int8) int8 {
    return x +% 1
}

func main() {
    if wrap8(127) != -128 {
        exit(1)
    }
    mut u uint8 = 0
    u -%= 1
    if u != 255 || -%u != 1 {
        exit(2)
    }
    mut m int64 = -9223372036854775808
    if -%m != m || m *% -1 != m {
        exit(3)
    }
    exit(0)
}
```

Arithmetic whose operands are all known values is computed at compile
time. Plain `+`, `-` and `*` must give a value of its type at every
step, in a function as at package level, or it is an error, `constant
expression overflows int8`, at the step that overflowed; so must `-x`.
The wrapping operators wrap there as at run time, so with `var m int8 =
127`, `m + 1` is an error and `m +% 1` is -128, and a package-level
variable may be initialized with one, as in `var seed uint64 = a *% b`
with `a` and `b` known.

```tack error
package main

func main() {
    var m int8 = 127
    n := m + 1
    c := 'a' * 3
    exit(int32(n) + int32(c))
}
```

```diagnostics
main.tck:5:10: error: constant expression overflows int8
main.tck:6:10: error: constant expression overflows uint8
```

A target-width type is the exception above 4294967295, where whether
arithmetic overflows depends on the target. A step of a constant expression
of a target-width type must lie between 0 and 4294967295, as a constant
of the type must. Elsewhere a known value of a target-width type past
4294967295 is not an error, but it is not known either: after `s :=
unsafe_size_t(4294967295)`, `s + 1` needs a proof as any unknown value
does, which nothing gives it, since it overflows a 32-bit target, and
`s +% 1` is computed at run time. A wrapping operator on a target-width
type is never a known value, and so never a constant expression, as
`^x` is not, since its width depends on the target. A plain operation of
a target-width type is proven only where it fits a 32-bit target, unless
its result is no larger than an operand, as `a - b` with `b` at least 0
is.

## Division and remainder

Integer division truncates toward zero, and the remainder takes the sign
of the dividend, so that `a == (a / b) * b + a % b` holds for every `a`
and every non-zero `b`, as in Go and C: `-7 / 2` is -3, not -4, and
`-7 % 2` is -1; `7 / -2` is -3, and `7 % -2` is 1. Constants and known
values divide the same way: they truncate toward zero, and the remainder
takes the dividend's sign. The smallest value of a signed type divided
by -1 is one past its largest, so that division overflows, and no
division wraps: on known values it is an error, `constant expression
overflows int64`, and anywhere else a signed division must be proven not
to be that one, or written in its checked form (see below). Its
remainder by -1 is 0, and every remainder is a value of its type.

```tack run exit=0
package main

func quo(a int64, b int64) int64 {
    if b == 0 {
        return 0
    }
    if b == -1 {
        return -%a
    }
    return a / b
}

func rem(a int64, b int64) int64 {
    if b == 0 {
        return 0
    }
    return a % b
}

func main() {
    if quo(-7, 2) != -3 || rem(-7, 2) != -1 {
        exit(1)
    }
    if quo(7, -2) != -3 || rem(7, -2) != 1 {
        exit(2)
    }
    if -7 / 2 != -3 || -7 % 2 != -1 {
        exit(3)
    }
    var m int8 = -128
    if m % -1 != 0 {
        exit(4)
    }
    mut d int8 = -1
    q, ok := m / d
    if ok || q != 0 {
        exit(5)
    }
    exit(0)
}
```

An integer division or remainder never divides by zero. A divisor that
is a known zero is an error, `division by zero`, or `division by zero in
constant expression` between two untyped constants. Any other divisor
must be proven not zero, or the division written in its checked form,
`q, ok := a / b`, which gives 0 and `false` for a zero divisor; both are
described in [Safety and proofs](10-safety.md). A signed `a / b` must
also be proven not to be the smallest value of its type divided by -1:
by a fact, a known value or bounds that rule out `b` being -1 or `a`
being that value, as `b != -1`, `b > 0`, `a / 2`, or `a > -128` for an
`int8` `a`. Its checked form gives 0 and `false` there too. Anything
else is an error, `a / b may overflow int64; check b != -1 first, or use
q, ok := a / b`, reported once the divisor is proven not zero, and a
divisor that is a known -1 suggests a check on the dividend instead, `a
/ -1 may overflow int16; check a > -32768 first, or use q, ok := a /
-1`. An unsigned division never overflows. `x /= b` and `x %= b`
compute `x / b` and `x % b` and need the same proofs, of `b` and, for a
signed `x /= b`, of `x`, `x /= b may overflow int64; check b != -1
first, or use q, ok := x / b and x = q when ok`; they have no checked
form.

```tack error
package main

func quo(x int64) int64 {
    return x / 0
}

func rem() int64 {
    return 1 % 0
}

func least() int8 {
    var m int8 = -128
    return m / -1
}

func signed(a int64, b int64) int64 {
    if b != 0 {
        return a / b
    }
    return 0
}

func main() {
    exit(0)
}
```

```diagnostics
main.tck:4:16: error: division by zero
main.tck:8:12: error: division by zero in constant expression
main.tck:13:12: error: constant expression overflows int8
main.tck:18:16: error: a / b may overflow int64; check b != -1 first, or use q, ok := a / b
```

Float division is described under [Floats](#floats).

## Shifts and bitwise operators

The bitwise operators `&` (and), `|` (or), `^` (exclusive or) and `&^`
(and not: `a &^ b` clears in `a` every bit set in `b`) take two integers
under the rules for arithmetic and give their common type, so `int8 &
uint8` is an error, `mismatched types int8 and uint8 in &`. They work on
a signed value's two's complement bits. Unary `^x` flips every bit of an
integer of any type: `^int8(0)` is -1 and `^uint8(0)` is 255. None of
them is defined on anything but integers, `operator & is not defined on
bool`.

`x << n` and `x >> n` shift the integer `x` by the count `n`. The count
is an unsigned integer of any width, or an untyped constant that is not
negative; a count of a signed type is an error, `shift count must be an
unsigned integer, got int64`, and a negative constant count `shift count
must not be negative, got -1`. The result has `x`'s type. `<<` of a value
not known keeps the bits that fit the type's width, as `+%` does, and
needs no proof, so `shl(64, 1)` below is -128; on known values a shift
that loses a set bit is an error (see below).
`>>` of a signed value is arithmetic, copying the sign bit in, and of an
unsigned value logical. A count at or past `x`'s width shifts every bit
out: `x << n` is 0, and `x >> n` is 0, or -1 when `x` is negative.

```tack run exit=0
package main

func shl(x int8, n uint8) int8 {
    return x << n
}

func shr(x int8, n uint8) int8 {
    return x >> n
}

func main() {
    if shl(64, 1) != -128 || shl(1, 8) != 0 {
        exit(1)
    }
    if shr(-5, 1) != -3 || shr(-5, 200) != -1 || shr(5, 8) != 0 {
        exit(2)
    }
    if 6 &^ 3 != 4 || ^5 != -6 || -8 >> 1 != -4 {
        exit(3)
    }
    exit(0)
}
```

Untyped constants are shifted and combined exactly, on their infinitely
wide two's complement form, so `^5` is -6 and `-8 >> 1` is -4, under
the range rule for constants: `1 << 64` is `constant expression is out
of range`. On known values of a type, a shift that loses a set bit
overflows, as `uint8(1) << 8` does, `constant expression overflows
uint8`.

```tack error
package main

func big() int64 {
    return 1 << 64
}

func lost() uint8 {
    return uint8(1) << 8
}

func signed(x uint64, n int64) uint64 {
    return x << n
}

func negative(x uint64) uint64 {
    return x << -1
}

func main() {
    exit(0)
}
```

```diagnostics
main.tck:4:12: error: constant expression is out of range
main.tck:8:12: error: constant expression overflows uint8
main.tck:12:17: error: shift count must be an unsigned integer, got int64
main.tck:16:17: error: shift count must not be negative, got -1
```

Each operator has a compound form, `&=`, `|=`, `^=`, `&^=`, `<<=` and
`>>=`, which computes the operator with the variable as its left operand
and the right side as its right operand.

## Comparisons

The comparison operators `==`, `!=`, `<`, `<=`, `>` and `>=` give a
`bool`.

Two integers of any types compare, with any of the six operators, the
target-width types included, by their mathematical values: no common type
is needed, and neither operand is converted first. So `int64(-1) <
uint64(0)` and `uint64(18446744073709551615) > int64(9223372036854775807)`
hold, and `int8(-1) == uint8(255)` does not. C differs: there `-1 < 0u`
is false, since the signed operand is converted to the unsigned type
first. An untyped constant operand still takes the other operand's type
and must be a value of it, so, for a `uint64` `u`, `u < -1` is
`constant -1 overflows uint64`. Comparing across types does not give the
operands a common type for anything else: after `a < b` for an `int64`
`a` and a `uint64` `b`, `a + b` is still an error.

```tack run exit=0
package main

func less(a int64, b uint64) bool {
    return a < b
}

func same(a int8, b uint8) bool {
    return a == b
}

func main() {
    if !less(-1, 0) {
        exit(1)
    }
    if same(-1, 255) {
        exit(2)
    }
    exit(0)
}
```

Two floats compare when they have the same type, as IEEE 754 says: NaN is
equal to nothing, itself included, and is neither less than nor greater
than anything, and `-0.0` is equal to `0.0`. An untyped constant operand
takes the float's type, an integer constant included, so `f > 1` holds
for a `float64` `f` above one. A float never compares with a value of an
integer type, `mismatched types float64 and int64 in >`, nor a `float32`
with a `float64`.

## Conversions

A conversion `T(x)` changes the type of the numeric value `x` to the
numeric type `T`. It gives the value unchanged, or the nearest value of
`T` where that is stated below, or it does not compile. Strings and bools convert to and from
no number, `cannot convert string to int64`, and a bool converts to no
float and no float to a bool.

Integer to integer. `T(x)` gives `x`'s value unchanged, as a value of
`T`, and compiles only where it cannot fail, which is when one of these
holds:

- `x` is an untyped constant or a known value that is a value of `T`, as
  `uint8(x)` after `x := 5`, or `int8('a')`;
- `x` is of a type that widens to `T` (see [Implicit
  widening](04-types.md#implicit-widening)), as `int64(b)` for a `uint8`
  `b`;
- the facts in force prove `x` a value of `T`, as `uint64(v)` under `if
  v >= 0`, or `uint64(-(v + 1))` under `if v < 0`. What such a proof
  needs is described in [Safety and proofs](10-safety.md#facts).

A constant or known value that is not a value of `T` is an error, as
`uint8(x)` is after `x := 300`, `constant 300 overflows uint8`. Any other
integer conversion is an error, `converting int64 to uint8 can fail;
check 0 <= x && x <= 255 first, or use v, ok := uint8(...)`, and is
written in its checked form, `v, ok := T(x)`, whose `ok` reports whether
`x` is a value of `T` and whose `v` is that value, or 0 when it is not
(see [The checked form](10-safety.md#the-checked-form)).

```tack run exit=0
package main

func low(x int64) int64 {
    if 0 <= x && x < 256 {
        return int64(uint8(x))
    }
    return -1
}

func main() {
    if low(200) != 200 || low(300) != -1 {
        exit(1)
    }
    mut x int64 = 300
    v, ok := uint8(x)
    if ok || v != 0 {
        exit(2)
    }
    exit(0)
}
```

The same function without its guard does not compile:

```tack error
package main

func low(x int64) uint8 {
    return uint8(x)
}

func main() {
    exit(low(3))
}
```

```diagnostics
main.tck:4:12: error: converting int64 to uint8 can fail; check 0 <= x && x <= 255 first, or use v, ok := uint8(...)
```

For a target-width `T`, a value of `T` is one from 0 to 4294967295, for
a constant, a known value and a proof alike, since such a value is one
on every target. A known value past 4294967295, but within 64 bits, fits
on some targets only, so converting it needs the checked form. The
checked form to a target-width `T` reports whether `x` is a
value of `T` on the target the program runs on.

Integer to float, and float to float. An integer of any type converts to
either float type, and a float to the other float type, with the plain
form, `float64(i)`, `float32(d)`. The conversion never fails, but may
round: to the nearest value of `T`, ties to even, so
`float64(9007199254740993)` is `9007199254740992.0`, and a `float64` too
large for a `float32` becomes an infinity of its sign.

Float to integer. A float converts to an integer only in the checked
form, `v, ok := int64(f)`, which truncates toward zero, 2.9 giving 2 and
-2.9 giving -2, and gives 0 and `false` for NaN, an infinity, or a value
whose truncation `T` cannot hold. The test is exact at every edge:
`9223372036854775808.0` does not convert to `int64`, the largest
`float64` below it does, and `-9223372036854775808.0` does. Floats carry
no facts, so the plain form never compiles for a float that is not a
constant, `converting float64 to int64 can fail; use v, ok :=
int64(...)`. A float constant converts exactly or not at all: `int64(2.0)`
is 2, `int64(2.5)` is `cannot convert constant 2.5 to int64: it is not
an integer`, and `int8(300.0)` is `constant 300 overflows int8`.

```tack run exit=0
package main

func trunc(f float64) int64 {
    v, ok := int64(f)
    if !ok {
        return -100
    }
    return v
}

func main() {
    if trunc(2.9) != 2 || trunc(-2.9) != -2 {
        exit(1)
    }
    if trunc(9223372036854775808.0) != -100 {
        exit(2)
    }
    if float64(9007199254740993) != 9007199254740992.0 {
        exit(3)
    }
    exit(0)
}
```

Each of these is an error:

```tack error
package main

func whole(f float64) int64 {
    return int64(f)
}

func half() int64 {
    return int64(2.5)
}

func wide() int8 {
    return int8(300.0)
}

func main() {
    exit(0)
}
```

```diagnostics
main.tck:4:12: error: converting float64 to int64 can fail; use v, ok := int64(...)
main.tck:8:12: error: cannot convert constant 2.5 to int64: it is not an integer
main.tck:12:17: error: constant 300 overflows int8
```

## Floats

`float32` and `float64` are IEEE 754's binary32 and binary64. Every
operation rounds its exact result once, to the nearest value of its type,
ties to even, and no two operations are fused into one: `a * b + c`
rounds the product and then the sum, at every optimization level.

`+`, `-`, `*`, `/`, unary `-` and the six comparisons work on floats, and
`+=`, `-=`, `*=` and `/=` on a mutable float variable. `%`, the bitwise
operators, the shifts, `++` and `--` do not. Both operands of a float
operator have the same type: there is no implicit conversion between an
integer and a float, nor between `float32` and `float64`, so `f + i` is
`mismatched types float64 and int64 in +`. An untyped constant operand
takes the float's type, an integer constant included.

Dividing a float by zero needs no proof and never stops the program: it
gives an infinity of the quotient's sign, or NaN for `0.0 / 0.0`. A
constant division by zero is still an error, as for integers: `1.0 /
0.0` is `division by zero in constant expression`. A float division has no checked form, `dividing floats cannot
fail, so it has no checked form; use a / b`.

```tack run exit=0
package main

func div(a float64, b float64) float64 {
    return a / b
}

func main() {
    inf := div(1.0, 0.0)
    nan := div(0.0, 0.0)
    negZero := div(0.0, -1.0)
    if nan == nan || nan < 0.0 || nan >= 0.0 {
        exit(1)
    }
    if !(inf > 1e308) {
        exit(2)
    }
    if negZero != 0.0 {
        exit(3)
    }
    exit(0)
}
```

The operators a float lacks, and the checked form, are errors:

```tack error
package main

func mix(f float64, i int64) float64 {
    return f + i
}

func rem(f float64) float64 {
    return f % 2.0
}

func half(f float64) float64 {
    q, ok := f / 2.0
    if ok {
        return q
    }
    return 0.0
}

func main() {
    exit(0)
}
```

```diagnostics
main.tck:4:12: error: mismatched types float64 and int64 in +
main.tck:8:12: error: operator % is not defined on float64
main.tck:12:5: error: dividing floats cannot fail, so it has no checked form; use a / b
```

A float carries no facts for proofs and cannot index. Comparisons of
floats are described under [Comparisons](#comparisons), and conversions
to and from floats under [Conversions](#conversions).
