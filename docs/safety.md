# Safety rules

Some operations can fail: a division by zero, an index out of range, a
conversion to a type that cannot hold the value. Tack has no runtime error
for any of them. The compiler accepts such an operation only in one of two
ways:

1. **The guard rule.** The compiler proves, where the operation is written,
   that it cannot fail, and the plain form compiles: `a / b`, `a[i]`.
2. **The checked form.** The program binds the operation to two names,
   `v, ok := ...`, and handles the case where it failed: `ok` is `false` and
   `v` is a zero value.

Anything else is a compile error that names the variable and suggests both
fixes. This page lists every operation the rules apply to, what each rule
requires, and the exact diagnostics. The README's
["The language today"](../README.md#the-language-today) describes the same
rules feature by feature.

Every example here is tested: each failing one is a case in
[`examples/invalid.txtar`](../examples/invalid.txtar) whose expected output
holds the diagnostic quoted here, the guarded and checked ones run in
[`examples/safety/main.tck`](../examples/safety/main.tck), and
`test/safety_doc_test.go` follows the HTML comment in front of each example
and diagnostic to check that the two still agree.

## The operations

| Operation                          | Fails when                       | Plain form compiles when                        | Checked form          |
|------------------------------------|----------------------------------|-------------------------------------------------|-----------------------|
| `a / b`, `a % b`                   | `b` is zero                      | `b` is proven not zero                          | `q, ok := a / b`      |
| `x /= b`, `x %= b`                 | `b` is zero                      | `b` is proven not zero                          | none                  |
| `a[i]`, reading an array element   | `i < 0` or `i >= len(a)`         | `i` is proven in `[0, len(a))`                  | `v, ok := a[i]`       |
| `a[i] = v`, writing one            | as above                         | as above                                        | none                  |
| `s[i]`, reading a string's byte    | `i < 0` or `i >= len(s)`         | `i` is proven in `[0, len(s))`                  | `v, ok := s[i]`       |
| `s[i] = v`                         | always an error                  | never: strings are immutable                    | none                  |
| `T(x)`, integer conversion         | `x` is not a value of `T`        | `x` is a constant or known value that fits `T`, or its type widens to `T` | `v, ok := T(x)` |

A write to an element covers every way of writing one: `a[i] = v`,
`a[i] += v`, `a[i]++`, and targets that mix fields and indexes, such as
`p.cells[i] = v` or `a[i].x = v`. Every index in such a target needs a
proof.

Conversions are the odd one out: no guard or fact makes `T(x)` compile
when `x`'s type does not widen to `T`, only a known value that fits `T`
(see [Conversions](#conversions)).

## The checked form

The checked form is the operation, alone, as the value of a declaration or
assignment of exactly two names:

```tack
q, ok := a / b      // or a % b
v, ok := a[i]       // an array element
v, ok := s[i]       // a string's byte
v, ok := uint8(x)   // a conversion that can fail
```

The first value is the result when the operation succeeds, and the zero
value of its type when it fails: `0` for a division, a conversion or a
string's byte; the element type's zero value for an array, so `0`, `""`,
`false`, or a struct with every field zero. The second is a `bool`, `true`
when the operation succeeded. For a division `ok` says only that the
divisor was not zero: `m / d` with `m` an `int8` holding `-128` and `d`
holding `-1` gives `-128` and `true`, since that division wraps rather than
fails. Both operands of a checked division are always evaluated.

The details:

- **`=` works too.** `q, ok = a / b`, `v, ok = a[i]` and `v, ok = T(x)`
  assign to existing mutable variables, under the rules for assigning
  several results.
- **`_` discards either value**, as in `v, _ := a[i]` or
  `_, fits := uint8(v)`.
- **Exactly two names.** One name is the plain form, which then needs its
  proof. Three or more are an
  error: <!-- error: safety-checked-three-names-division -->`assignment mismatch: 3 variables but a division gives 2 values`,
  and
  likewise <!-- error: safety-checked-three-names-index -->`assignment mismatch: 3 variables but an index gives 2 values`
  and <!-- error: safety-checked-three-names-conversion -->`assignment mismatch: 3 variables but a conversion gives 2 values`.
- **The operation must be the whole value.** Parentheses around it are fine,
  `q, ok := (a / b)`,
  but <!-- case: safety-checked-not-whole -->`q, ok := a / b + 1` is not a
  checked division: it reports the unproven divisor
  and <!-- error: safety-checked-not-whole -->`assignment mismatch: 2 variables but 1 value`.
- **Only the outermost index is checked.**
  In <!-- case: safety-checked-outer-index-only -->`v, ok := m[i][j]` the
  inner `m[i]` is a plain index and needs its own proof.
- **An ok the compiler does not need is allowed.** Where the operation
  cannot fail, because the divisor or index is a known value or proven,
  both operands of a division are untyped constants, or a conversion's
  operand is a constant, a known value that fits, or of a type that widens
  to `T`, the checked form still compiles: the first value is the
  operation's, and `ok` is always `true`. A future `tack vet` is to point
  these out; [vet.md](vet.md) lists the checks.

<!-- example: safety -->
```tack
mut n int64 = 9
q, ok := n / 3              // q == 3, ok == true
r, fine := 7 / 2            // r == 3, fine == true
a := [3]int64{1, 2, 3}
v, inRange := a[1]          // v == 2, inRange == true
b, isByte := "tack"[1]      // b == 'a', isByte == true
c, fits := uint8(5)         // c == 5, fits == true
x := 5
d, known := uint8(x)        // d == 5, known == true
var small uint8 = 7
e, widens := int64(small)   // e == 7, widens == true
```

  The quotient of two untyped constants takes the type of the variable it
  is assigned to, as any constant does, and is an `int64` in a declaration.

- **A failure known at compile time is still an error.** The checked form
  does not accept an operation that always fails: `q, ok := a % 0`
  reports <!-- error: safety-checked-known-zero -->`division by zero`,
  `v, ok := a[5]` on a `[3]int64`
  reports <!-- error: safety-checked-array-index-known-out-of-range -->`index 5 is out of range for [3]int64`,
  `v, ok := "tack"[4]`
  reports <!-- error: safety-checked-string-literal-index-out-of-range -->`index 4 is out of range for a string of 4 bytes`,
  `v, ok := s[-1]`
  reports <!-- error: safety-checked-string-index-negative -->`index -1 is out of range for a string`,
  and `v, ok := uint8(256)`
  reports <!-- error: safety-checked-conversion-overflow -->`constant 256 overflows uint8`.
- **No checked form for writes.** `x /= b`, `x %= b` and a write to an
  array element have no two-value form, so their divisor or index must be
  proven. Their diagnostics leave out the `v, ok` suggestion.

## The guard rule

### What must be proven

| Operation        | Proof needed                                                                                     |
|------------------|--------------------------------------------------------------------------------------------------|
| `a / b`, `a % b`, `/=`, `%=` | `b` is not zero                                                                       |
| `a[i]` on `[n]T` | `0 <= i` and `i <= n - 1`; an unsigned `i` needs only the upper bound                           |
| `s[i]`           | `0 <= i` and `i` below the length of `s`: a known string's length, a bound the facts give `len(s)`, or `i < len(s)` itself |

A proof is about a **plain name**, the divisor or index written as a
variable or parameter. Anything else, such as `b + 1`, `i + 1`, `-i` or a
field `p.i`, is proven only when it is a known value.

### Known values

A known value proves itself: a divisor that is a known non-zero value, an
index that is a known value in range, or a conversion of a known value that
fits its type, needs nothing more. The compiler
knows the value of:

- a constant expression: literals and operators on them;
- a character literal, such as `'a'`;
- `len` of an array, and `len` of a known string: a string literal, or an
  immutable variable, local or package-level, initialized with a known
  string;
- an immutable variable, local or package-level, whose initializer is a
  known value, as in `last := len(a) - 1` or `var k int64 = 1`;
- arithmetic on known values, such as `len(a) - 1`.

A mutable variable, a parameter and a call never have a known value, and
neither does `len` of a mutable string variable, even one initialized with
a literal:
after <!-- case: safety-mutable-string-variable -->`mut s := "abc"`, `s[0]`
still needs a proof. An immutable one is known, so its bytes are indexed
as a literal's are:

<!-- example: safety -->
```tack
word := "tack"
same := word
last := same[len(same) - 1]     // last == 'k'
```

### Facts, and what gives them

Where the value is not known, the compiler uses *facts*: what a condition
that has just been tested says about a variable. A fact is about a
parameter, a local variable, or an immutable global. A variable's type also
bounds it, so an unsigned variable is never negative and a `uint8` index
into a `[256]T` needs nothing.

The conditions that give facts are:

- a comparison of the variable with a known value, either way round:
  `b != 0`, `b > 0`, `b <= -1`, `b == 4`, `0 <= i`, `i < len(a)`,
  `i <= len(a) - 1` (for an array, whose length is known);
- for a string variable `s`: `len(s)` compared with a known value, which
  bounds the length (`len(s) > 0`, `len(s) >= 2`, `len(s) != 0`, and
  `len(s) == 0` being false), and `i < len(s)`, which bounds `i` by it;
- `c1 && c2` when true gives the facts of both sides; `c1 || c2` when false
  gives the facts of both sides being false; `!c` swaps true and false;
- `c1 || c2` when true, and `c1 && c2` when false, give only what every
  side agrees on, since any one of them may be the side that decided: for
  a variable every side constrains, the smallest range holding what each
  side allows, and not zero when no side allows zero. So `b < 0 || b > 0`
  proves `b != 0`, `i == 0 || i == 1` proves `0 <= i && i <= 1`, and the
  `else` of `if b >= 0 && b <= 0` knows `b != 0`. A variable only some
  sides constrain gets nothing.

A fact holds in these places:

| Where                                      | Example                                                    |
|--------------------------------------------|------------------------------------------------------------|
| the `then` branch, by the condition being true | `if b != 0 { return a / b }`                           |
| the `else` branch, by it being false       | `if b == 0 { return 0 } else { return a / b }`             |
| the rest of the block after an `if` whose branch leaves by `return`, `break` or `continue` | `if b == 0 { return 0 }` then `return a / b` |
| a loop's body, by the loop's condition     | `for ; d != 0; d-- { sum += 12 / d }`                      |
| a range loop's body, for its index         | `for i := range a { a[i] }`, `for i := range s { s[i] }`   |
| a counting loop's body, for its variable   | `for mut i := 0; i < len(a); i++ { a[i] }`                 |
| the right side of `&&`, by the left being true | `b != 0 && a / b > 1`                                 |
| the right side of `\|\|`, by the left being false | `b == 0 \|\| a / b > 1`                            |
| a `switch` case, as in the chain of ifs it stands for | `switch b { case 0: return 0 }` then `a / b`    |
| deferred code, with the facts in force at its `defer` | `if b == 0 { return 0 }` then `defer { 12 / b }` |

A range loop's index is at least 0 and at most its count less one, when the
count is a known value, or the length of the array it ranges over less one,
so it proves an index into any array at least as long.
`for i := range len(s)` and `for i := range s` over a string variable bound
`i` by `len(s)`; over a known string, by its length.

A three-clause loop's body knows what its condition says, and a
*counting* loop's body knows one thing more: the value its variable
starts from. A loop counts when

- its init declares the variable with a known value, `mut i := c` (or
  `mut i T = c`);
- its post steps it by a known amount: up by `i++` or `i += k`, or down by
  `i--` or `i -= k`, with `k` a known value above zero;
- its condition bounds the variable on the side it moves towards, above
  when counting up (`i < n`, `i <= n`, `n > i`, `i < len(s)`), below when
  counting down (`i >= 0`), and the step from that bound cannot wrap past
  the end of the variable's type;
- its body never assigns the variable, as for any fact.

The body then knows that `i` is at least `c` counting up, or at most `c`
counting down, along with what the condition says:

<!-- example: safety -->
```tack
func total(a [3]int64) int64 {
    mut sum int64 = 0
    for mut i := 0; i < len(a); i++ {
        sum += a[i]
    }
    return sum
}

func reversed(a [3]int64) int64 {
    mut digits int64 = 0
    for mut i := len(a) - 1; i >= 0; i-- {
        digits = digits * 10 + a[i]
    }
    return digits
}
```

The fact holds in the body only: not in the condition, the post, or after
the loop. A body that assigns the variable gets no fact about it, so
in <!-- case: safety-counting-loop-assigns -->`for mut i := 0; i < 3; i++ { sum += a[i]; i++ }`
`a[i]` needs a proof. Nor does a step that could wrap: an `int8` at 2
plus 126 is -128, which is below 3 again,
so <!-- case: safety-counting-loop-may-wrap -->`for mut i int8 = 2; i < 3; i += 126 { sum += a[i] }`
proves nothing about `i`. An unsigned variable is never negative, so its
condition alone bounds it from both sides.

A `switch` case proves inside its body what `tag == v1 || tag == v2 ...`
proves when true, so `case 1, 2:` proves `1 <= tag && tag <= 2`, and
`tag != 0`. Every later case, and the default, sees the earlier cases'
values not matching.

<!-- example: safety -->
```tack
func split(a int64, b int64) int64 {
    if b < 0 || b > 0 {
        return a / b
    }
    return 0
}

func early(a [3]int64, i int64) int64 {
    switch i {
    case 0, 1:
        return a[i]
    }
    return -1
}
```

### Where a fact holds

A fact holds only in a region that never assigns its variable, anywhere,
before or after the use: an assignment, a compound assignment, `++` or
`--`, or one of several targets. If the region assigns it, the fact is
dropped for the whole region:

<!-- case: safety-region-assigned-later -->
```tack
mut d int64 = 2
mut x int64 = 0
if d != 0 {
    x = 10 / d      // error: d is assigned later in this branch
    d = 0
}
```

The division
reports <!-- error: safety-region-assigned-later -->`divisor d may be zero; check d != 0 first, or use q, ok := ... / d`.

The region is the branch for an `if`, the body for a loop (a three-clause
loop's post statement is not part of it), and the rest of the block for an
early exit. A fact that names a string through `len(s)` also needs the
region to leave `s` unassigned; `s` may be `mut` as long as the region does
not change it. Assigning a whole array does not affect proofs about its
indexes, since its length cannot change.

A **mutable global** never has a fact, since any call can change it. An
**immutable** global always has a known value, since its initializer is a
constant.

**Deferred code** is checked with the facts in force where the `defer` is
written. The region those facts hold in takes in the rest of the defer's
block, which is where the deferred code runs, so assigning the variable
after the `defer` drops the fact:

<!-- case: safety-defer-assigned-after -->
```tack
mut b int64 = 3
if b == 0 {
    return
}
defer {
    if 12 / b == 4 {    // error: b is assigned after the defer
        print("ok\n")
    }
}
b = 0
```

The division
reports <!-- error: safety-defer-assigned-after -->`divisor b may be zero; check b != 0 first, or use q, ok := ... / b`.

### What does not count

| Not a proof                                  | Why                                                                     |
|----------------------------------------------|-------------------------------------------------------------------------|
| <!-- case: safety-no-proof-field -->`if p.d != 0 { n / p.d }`                    | a field never carries a fact; copy it into a variable first             |
| <!-- case: safety-no-proof-not-a-name -->`a[i + 1]` after `if 0 <= i && i < 2`        | only a plain name is proven                                             |
| <!-- case: safety-no-proof-unknown-bound -->`if 0 <= i && i < n` with `n` not known      | a comparison with a variable that is not a known value gives no fact    |
| <!-- case: safety-no-proof-variable-comparison -->`if b > c { a / b }`                         | the same                                                                |
| <!-- case: safety-no-proof-string-len-minus-one -->`if 0 <= i && i <= len(s) - 1 { s[i] }`      | `len(s)` of a string variable is not known, and only `i < len(s)` bounds `i` by it |
| <!-- case: safety-no-proof-string-equality -->`if s == "abc" { s[0] }`                     | comparing strings says nothing about the length                         |
| <!-- case: safety-no-proof-unsigned-len -->`if u < len(s)` with `u` a `uint64`          | `uint64` and `int64` have no common type, so the comparison is an error; convert first with `i, ok := int64(u)` |
| <!-- case: safety-no-proof-exit -->`if b == 0 { exit(1) }` before `a / b`       | `exit` is a call, not a way out of the block; the rule is syntactic     |
| <!-- case: safety-no-proof-or-one-side -->`if b < 0 \|\| c > 0 { a / b }`              | only one side says anything about `b`                                   |
| <!-- case: safety-no-proof-multi-value-case-out-of-range -->`switch i { case 0, 3: a[i] }`               | the case allows `i == 3`                                                |
| <!-- case: safety-no-proof-loop-assigns -->`for i < 3 { a[i]; i++ }`                    | the body assigns `i`                                                    |
| <!-- case: safety-no-proof-mutable-global -->`mut g int64` (global), `if g != 0 { x / g }` | a mutable global never has a fact                                      |
| <!-- case: safety-no-proof-range-longer -->`for i := range b { a[i] }` with `b` longer than `a` | the index bound is `len(b) - 1`                                 |
| <!-- case: safety-no-proof-call-length -->`name()[i]` after `if 0 <= i && i < len(name())` | only a string variable's length can carry a fact                    |
| <!-- case: safety-no-proof-conversion-guard -->any guard around `uint8(x)`                  | conversions follow the type rule only                                  |

## Per operation

Each failing example below shows its diagnostic, as the compiler prints it
after `error: `.

### Division and remainder

<!-- case: divisor-may-be-zero -->
```tack
func half(a int64, b int64) int64 {
    return a / b
}
```

<!-- error: divisor-may-be-zero -->
`divisor b may be zero; check b != 0 first, or use q, ok := ... / b`

A divisor that is not a name
gets <!-- error: safety-div-not-a-name -->`divisor may be zero; use q, ok := ... / ...`,
as in `a / (b + 1)`.

Guarded:

<!-- example: safety -->
```tack
func half(a int64, b int64) int64 {
    if b == 0 {
        return 0
    }
    return a / b
}
```

Checked:

<!-- example: safety -->
```tack
mut b int64 = 0
q, ok := 8 / b      // q == 0, ok == false
b = 2
r, fine := 9 % b    // r == 1, fine == true
```

`%` follows the same rules as `/`. A known divisor is checked at compile
time: `a / 0` and `a % 0`
are <!-- error: safety-div-known-zero -->`division by zero`, and two untyped
constants, `1 / 0`,
give <!-- error: safety-div-constant-zero -->`division by zero in constant expression`,
in a function as at package level.

### Compound division

<!-- case: safety-compound-div-unguarded -->
```tack
func f(b int64) int64 {
    mut x int64 = 100
    x /= b
    return x
}
```

<!-- error: safety-compound-div-unguarded -->
`divisor b may be zero; check b != 0 first`, and for a divisor that is not a
name, as in
`x %= b + 1`, <!-- error: safety-compound-rem-not-a-name -->`divisor may be zero`.

Guarded:

<!-- example: safety -->
```tack
mut x int64 = 100
if b != 0 {
    x /= b
}
```

There is no checked form; write `q, ok := x / b` and assign `q`.

### Reading an array element

<!-- case: safety-index-read-unguarded -->
```tack
func get(a [3]int64, i int64) int64 {
    return a[i]
}
```

<!-- error: safety-index-read-unguarded -->
`index i may be out of range; check 0 <= i && i < 3 first, or use v, ok := ...[i]`

An index that is not a name
gets <!-- error: safety-index-read-not-a-name -->`index may be out of range; use v, ok := ...[...]`,
as in `a[i + 1]`.

Guarded:

<!-- example: safety -->
```tack
func get(a [3]int64, i int64) int64 {
    if 0 <= i && i < len(a) {
        return a[i]
    }
    return 0
}
```

Checked:

<!-- example: safety -->
```tack
a := [3]int64{10, 20, 30}
mut i int64 = 5
v, ok := a[i]       // v == 0, ok == false
```

A known index is checked at compile time: `a[3]` on a `[3]int64`
is <!-- error: array-index-out-of-range -->`index 3 is out of range for [3]int64`,
and `a[-1]`
is <!-- error: safety-index-read-negative -->`index -1 is out of range for [3]int64`.

### Writing an array element

<!-- case: safety-index-write-unguarded -->
```tack
func set(i int64) int64 {
    mut a [3]int64
    a[i] = 7
    return a[0]
}
```

<!-- error: safety-index-write-unguarded -->
`index i may be out of range; check 0 <= i && i < 3 first`, and for an index
that is not a name, as in
`a[i + 1] = 1`, <!-- error: safety-index-write-not-a-name -->`index may be out of range`.

Guarded:

<!-- example: safety -->
```tack
mut a [3]int64
if i < 0 || i >= 3 {
    return 0
}
a[i] = 7
```

There is no checked form.

### Reading a string's byte

<!-- case: safety-string-index-unguarded -->
```tack
func at(s string, i int64) uint8 {
    return s[i]
}
```

<!-- error: safety-string-index-unguarded -->
`index i may be out of range; check 0 <= i && i < len(s) first, or use v, ok := ...[i]`

A constant index into a string variable
gets <!-- error: string-constant-index-unguarded -->`index 0 may be out of range; check len(s) > 0 first, or use v, ok := ...[0]`.
An index into a string that is neither a variable nor a literal, such as a
call's result,
gets <!-- error: string-index-of-call -->`index i may be out of range; use v, ok := ...[i]`,
since only the checked form can help.

Guarded:

<!-- example: safety -->
```tack
func at(s string, i int64) uint8 {
    if 0 <= i && i < len(s) {
        return s[i]
    }
    return 0
}

func first(s string) uint8 {
    if len(s) > 0 {
        return s[0]
    }
    return 0
}
```

Checked:

<!-- example: safety -->
```tack
s := "abc"
mut i int64 = 9
v, ok := s[i]       // v == 0, ok == false
```

A constant index into a known string is checked at compile time:
`"tack"[1]` is `97`, `"tack"[4]`
is <!-- error: string-index-out-of-range -->`index 4 is out of range for a string of 4 bytes`,
and after `s := "abc"`, `s[5]`
is <!-- error: safety-known-string-variable-out-of-range -->`index 5 is out of range for a string of 3 bytes`.
A negative constant is always an
error, <!-- error: string-index-negative -->`index -1 is out of range for a string`.

Writing a byte, even of a `mut` string,
is <!-- error: string-element-assign -->`cannot assign to an element of a string; strings are immutable`.

### Conversions

<!-- case: safety-conversion-unchecked -->
```tack
func low(x int64) uint8 {
    return uint8(x)
}
```

<!-- error: safety-conversion-unchecked -->
`converting int64 to uint8 can fail; use v, ok := uint8(...)`

There is no guarded version. `T(x)` compiles only when `x` is an untyped
constant or a [known value](#known-values) that is a value of `T`, or `x`'s
type widens to `T`, as `int64(b)` for a `uint8` `b`. A guard does not help,
so this is the same error:

<!-- case: safety-conversion-guarded -->
```tack
func low(x int64) uint8 {
    if 0 <= x && x < 256 {
        return uint8(x)
    }
    return 0
}
```

A known value is converted as a constant is, whatever its type:

<!-- example: safety -->
```tack
x := 5
small := uint8(x)   // small == 5
letter := int8('a') // letter == 97, though 'a' is a uint8
```

and one that does not fit is an error: after `x := 300`, `uint8(x)`
is <!-- error: safety-conversion-known-overflow -->`constant 300 overflows uint8`.

Checked:

<!-- example: safety -->
```tack
mut x int64 = 300
v, ok := uint8(x)   // v == 0, ok == false
x = 44
w, fine := uint8(x) // w == 44, fine == true
```

An untyped constant that does not fit is a compile error in either form:
`uint8(256)`
is <!-- error: safety-conversion-constant-overflow -->`constant 256 overflows uint8`.
Strings and bools cannot be converted: `int64("7")`
is <!-- error: safety-conversion-string -->`cannot convert string to int64`.

## Compile-time errors

The compiler reports, rather than proves or checks, an operation whose
failure it can see from known values:

| Case                                      | Example                               | Diagnostic                                          |
|-------------------------------------------|---------------------------------------|-----------------------------------------------------|
| known zero divisor                        | `a / 0`                               | <!-- error: safety-div-known-zero -->`division by zero` |
| zero divisor between untyped constants    | `x := 1 / 0`                          | <!-- error: safety-div-constant-zero -->`division by zero in constant expression` |
| known index out of range, array           | `a[3]` on `[3]int64`                  | <!-- error: array-index-out-of-range -->`index 3 is out of range for [3]int64` |
| known index out of range, known string   | `"tack"[4]`, `s[5]` after `s := "abc"` | <!-- error: string-index-out-of-range -->`index 4 is out of range for a string of 4 bytes` |
| negative known index, any string          | `s[-1]`                               | <!-- error: string-index-negative -->`index -1 is out of range for a string` |
| constant that does not fit its type       | `var b uint8 = 256`, `uint8(256)`, `uint8(x)` after `x := 300` | <!-- error: safety-conversion-constant-overflow -->`constant 256 overflows uint8` |
| arithmetic on known values that overflows | `'a' * 3`; `-m`, `m - 1`, `m / -1` with `var m int8 = -128` | <!-- error: const-overflow-mul -->`constant expression overflows uint8`, or <!-- error: safety-const-overflow-int8 -->`constant expression overflows int8` for `m` |

Arithmetic on values the compiler does not know wraps at runtime instead:
for a `mut` `int8` `m` holding `-128`, `m / -1` is `-128` and `m % -1` is
`0`.
