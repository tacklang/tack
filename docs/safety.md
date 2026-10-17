# Safety rules

These rules serve Tack's four guarantees, set out in
[guarantees.md](guarantees.md).

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
requires, and the exact diagnostics. The same proofs keep the results of
a call that returned an error from being read before the error is
checked (see [Errors](#errors)). The README's
["The language today"](../README.md#the-language-today) describes the same
rules feature by feature.

These rules cover Tack code only. An extern func's body is C, which they
do not reach: what it does, and what it returns, is a promise its
declaration makes and nothing checks, as
[extern.md](extern.md) says.

Lists are memory a program owns, and their rules, moves and frees, keep
every buffer freed exactly once and never read after it is (see
[Lists](#lists)). `exit` ends the process before any owner frees its
list, so every allocator frees each buffer it still holds first.

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
| `v[i]`, reading a view's element   | `i < 0` or `i >= len(v)`         | `i` is proven in `[0, len(v))`                  | `x, ok := v[i]`       |
| `v[i] = x`                         | always an error                  | never: views are read-only                      | none                  |
| `l[i]`, reading a list's element   | `i < 0` or `i >= len(l)`         | `i` is proven in `[0, len(l))`                  | `x, ok := l[i]`       |
| `l[i] = x`, writing one            | as above                         | as above, and `l` is mutable                    | none                  |
| `x[i:j]`, a slice of a string, an array, a view or a list | `i < 0`, `j < i` or `j > len(x)` | `0 <= i`, `i <= j` and `j <= len(x)` are proven | `t, ok := s[i:j]`, a string's only |
| `x[i:j] = v`                       | always an error                  | never: a slice is not a place                   | none                  |
| `T(x)`, integer conversion         | `x` is not a value of `T`        | `x` is a constant or known value that fits `T`, or its type widens to `T` | `v, ok := T(x)` |

A write to an element covers every way of writing one: `a[i] = v`,
`a[i] += v`, `a[i]++`, and targets that mix fields and indexes, such as
`p.cells[i] = v` or `a[i].x = v`. Every index in such a target needs a
proof.

Conversions are the odd one out: no guard or fact makes `T(x)` compile
when `x`'s type does not widen to `T`, only a known value that fits `T`
(see [Conversions](#conversions)).

A shift, `x << n` or `x >> n`, never fails, so it is not in the table: a
count at or past `x`'s width shifts every bit out, giving 0, or -1 for
`>>` of a negative value, where C leaves it undefined, and the backend
tests the count before shifting. What a count cannot be is negative, which
its type rules out instead of a proof: it must be an unsigned integer,
<!-- error: shift-signed-count -->`shift count must be an unsigned integer, got int64`,
or an untyped constant that is not
negative, <!-- error: shift-negative-count -->`shift count must not be negative, got -1`.

## The checked form

The checked form is the operation, alone, as the value of a declaration or
assignment of exactly two names:

```tack
q, ok := a / b      // or a % b
v, ok := a[i]       // an array element
v, ok := s[i]       // a string's byte
x, ok := v[i]       // a view's element
t, ok := s[i:j]     // a string's slice, s[i:] or s[:j] too
v, ok := uint8(x)   // a conversion that can fail
x, ok := pop(mut l)       // a list's last element, taken out
x, ok := remove(mut l, i) // a list's element i, taken out
```

The first value is the result when the operation succeeds, and the zero
value of its type when it fails: `0` for a division, a conversion or a
string's byte; the element type's zero value for an array or a view, so `0`, `""`,
`false`, or a struct with every field zero; `""` for a string's slice;
and for `pop` and `remove`, the element type's zero value, or, for a
type with none, nothing that can be read until the second value is
checked true (see [lists](#lists)). The second is a `bool`, `true`
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
  and <!-- error: safety-checked-three-names-conversion -->`assignment mismatch: 3 variables but a conversion gives 2 values`,
  and <!-- error: slice-checked-mismatch -->`assignment mismatch: 3 variables but a slice gives 2 values`.
- **The operation must be the whole value.** Parentheses around it are fine,
  `q, ok := (a / b)`,
  but <!-- case: safety-checked-not-whole -->`q, ok := a / b + 1` is not a
  checked division. It is one
  error, <!-- error: safety-checked-not-whole -->`the checked form must be the whole value: q, ok := a / b, then compute q + 1`,
  covering the whole value, which names the first division, remainder or
  index in it outside a call, and the rest of the value computed from the
  first name. That operation needs no proof of its own; any other in the
  value still does.
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
  `t, ok := s[1:5]` after `s := "abc"`
  reports <!-- error: slice-checked-bound-past-string -->`slice bound 5 is out of range for a string of 3 bytes`,
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
| `x[i:j]`         | `0 <= i`, `i <= j` and `j <= len(x)`, each as an index's bound is proven, `len(x)` itself or `j <= len(x)` included; `x[:j]` needs `0 <= j <= len(x)` and `x[i:]` needs `0 <= i <= len(x)` |

A proof is about a **name**, the divisor or index written as a variable or
parameter, or a **field or element of an immutable variable or
parameter**, such as `p.d`, `p.inner.d`, `p.a[1]` or `p.a[k]` with `k`
immutable. Anything else, such as `b + 1`, `i + 1`, `-i`, or a field of a
mutable variable or a `mut` parameter, is proven only when it is a known
value.

### Known values

A known value proves itself: a divisor that is a known non-zero value, an
index that is a known value in range, or a conversion of a known value that
fits its type, needs nothing more. The compiler
knows the value of:

- a constant expression: literals and operators on them;
- a character literal, such as `'a'`;
- `len` of an array, and `len` of a known string: a string literal, an
  immutable variable, local or package-level, initialized with a known
  string, or a string field or element of an immutable global;
- an immutable variable, local or package-level, whose initializer is a
  known value, as in `last := len(a) - 1` or `var k int64 = 1`;
- a field of an immutable global, `origin.x`, an element of one at a known
  index, `table[0]`, and a byte of a known string at a known index, and
  the same of a literal of known values;
- a conversion of a known value that fits its type, such as `int8(k)`;
- arithmetic on known values, such as `len(a) - 1`.

A target-width type, `unsafe_size_t` or `unsafe_uintptr_t`, is as wide as
the target makes it, from 32 to 64 bits, so where it wraps above
4294967295 depends on the target. A value of one is known only up to
4294967295: after `s := unsafe_size_t(4294967295)`, `s + 1` is not a
known value, and is computed at run time. Below 0 every target wraps, so
after `z := unsafe_size_t(0)`, `z - 1` is an error, as it is for any
unsigned type.

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
parameter, a local variable, an immutable global, or a field or element of
an immutable variable or parameter, which nothing can change while it is in
scope: a field, `p.d`, a field of a field, `p.inner.d`, an element at a
known index or at an immutable variable's, `p.a[1]` or `p.a[k]`, and a
string field's length, `len(p.s)`. A variable's type also bounds it, so an
unsigned variable is never negative and a `uint8` index into a `[256]T`
needs nothing.

<!-- example: field-facts -->
```tack
func divide(n int64, p P) int64 {
    if p.d != 0 {
        return n / p.d
    }
    return -1
}
```

The conditions that give facts are:

- a comparison of the variable with a known value, either way round:
  `b != 0`, `b > 0`, `b <= -1`, `b == 4`, `0 <= i`, `i < len(a)`,
  `i <= len(a) - 1` (for an array, whose length is known). The known
  value may be of any integer type, since two integers compare by their
  values: `i < uint64(3)` bounds an `int64` `i` at 2, and says nothing of
  its lower bound, so it may still be negative;
- for a string variable `s`: `len(s)` compared with a known value, which
  bounds the length (`len(s) > 0`, `len(s) >= 2`, `len(s) != 0`, and
  `len(s) == 0` being false), and `i < len(s)`, which bounds `i` by it,
  whatever `i`'s integer type, a `uint64` or a target-width type included;
  the same for a view `v`, `len(v) > 0` and `i < len(v)`;
- for a slice's bounds only, two relations: `j <= len(s)`, for a string
  or a view, bounds `j` by the length itself, and `i <= j`, or `i < j`,
  puts `i` at or below `j`, for integer variables of any types. An index
  or a divisor never uses them;
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
`i` by `len(s)`; over a known string, by its length. Over a view, `i` is
bounded by `len(v)`.

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
- its body never assigns the variable, as for any fact, and neither does
  its condition, which runs again before every pass.

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
the loop. The start is not tried again before a later pass, so a body or
a condition that assigns the variable anywhere, even after the use, gets
no fact from the start about it, and
in <!-- case: safety-counting-loop-assigns -->`for mut i := 0; i < 3; i++ { sum += a[i]; i++ }`
`a[i]` needs a proof that `i >= 0`, though the condition, tried again
before every pass, still proves `i < 3`. Nor does a step that could wrap: an `int8` at 2
plus 126 is -128, which is below 3 again,
so <!-- case: safety-counting-loop-may-wrap -->`for mut i int8 = 2; i < 3; i += 126 { sum += a[i] }`
proves nothing about `i`. A target-width variable could wrap past
4294967295 on a 32-bit target, so its step must stay at or below that:
<!-- case: unsafe-counting-loop-may-wrap -->`for mut i unsafe_size_t = 1; i <= 4294967295; i++ { q += 10 / i }`
proves nothing about `i` either. An unsigned variable is never negative, so its
condition alone bounds it from both sides.

A `switch` case proves inside its body what `tag == v1 || tag == v2 ...`
proves when true, so `case 1, 2:` proves `1 <= tag && tag <= 2`, and
`tag != 0`. Every later case, and the default, sees the earlier cases'
values not matching. A type switch, `switch v := a.(type)`, proves
nothing, since what an `any` holds is never known at compile time; a
case's `v` is a variable like any other, so a check on it inside the case
proves what that check proves.

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

A fact holds in its region up to the first write to its variable that can
reach the use: an assignment, a compound assignment, `++` or `--`, one of
several targets, or a call that passes the variable, or a part of it, as
`mut`, which may assign it. A write that runs before the use ends the
fact there:

<!-- case: safety-region-assigned-before -->
```tack
mut d int64 = 2
mut x int64 = 0
if d != 0 {
    d--
    x = 10 / d      // error: d is assigned before the use
}
```

The division
reports <!-- error: safety-region-assigned-before -->`divisor d may be zero; check d != 0 first, or use q, ok := ... / d`.

A write that runs after the use, on every path, does not end it. A call
assigns its `mut` arguments only after every argument is evaluated and
the call has begun, and an assignment stores to its target only after
its value is computed, so the call or the assignment a fact is for may
write the variable itself: `if len(l) > 0 { x := pop(mut l) }`,
`if 0 <= i && i < len(l) { x := remove(mut l, i) }` and
`if d != 0 { d = n / d }` compile. A second use after that write needs a
proof of its own: in <!-- case: cond-write-pop-twice -->`if len(l) > 0 && pop(mut l) > 0 && pop(mut l) > 0`
the second `pop` reports <!-- error: cond-write-pop-twice -->`pop may find l empty; check len(l) > 0 first, or use x, ok := pop(mut l)`,
with <!-- note: cond-write-pop-twice -->`but l is passed as mut to pop here, so the proof ends`
at the first. A write later in a loop's body reaches the next pass's use
only through the loop's back edge, and when the fact comes from the
loop's condition, or from a guard inside the body before the use, that
edge runs the check again, so the write does not end it. A write in a
loop that lies inside the fact's region does reach every use in that
loop, before or after it, since the next pass starts with no check:

<!-- case: proof-ends-on-back-edge -->
```tack
if len(l) > 0 {
    for range 2 {
        x := pop(mut l)
```

reports the pop, <!-- note: proof-ends-on-back-edge -->`but l is passed as mut to pop here, so the proof ends`,
pointing at the pop itself. Code that runs out of the order it is
written in is judged by when it runs: deferred code runs when its block
is left, after every write in the block outside it (see below), and a
switch's `default` written before other cases runs after every case's
values are tried, so a value that writes, written below it, ends what
the default relies
on: <!-- case: proof-ends-before-early-default -->`if d != 0 { switch { default: exit(10 / d); case zero(mut d): } }`
reports <!-- note: proof-ends-before-early-default -->`but d is passed as mut to zero here, so the proof ends`.

A call passing the variable as `mut` ends the proof the same way, and the
note names the
call: <!-- case: mut-proof-ends-at-call -->`set(mut i, 7)` before `a[i]`
inside `if 0 <= i && i < 3`
adds <!-- note: mut-proof-ends-at-call -->`but i is passed as mut to set here, so the proof ends`.
A method call's `mut` receiver, `mut p.reset()`, is a `mut` argument
too, and ends a proof about its place the same way.
Inside a function, a `mut` parameter is a variable like any other: a fact
about it holds in a region that never assigns it.

The region is the branch for an `if`, the body for a loop (a three-clause
loop's post statement is not part of it), and the rest of the block for an
early exit. It also takes in whatever of the condition still runs after
the comparison that gave the fact, so a call later in the same condition
ends it:

<!-- case: cond-write-and-ends-fact -->
```tack
mut l := six()
if len(l) > 5 && shrink(mut l) {
    use(l[5])
}
```

reports <!-- error: cond-write-and-ends-fact -->`index 5 may be out of range; check len(l) > 5 first, or use v, ok := ...[5]`,
with <!-- note: cond-write-and-ends-fact -->`but l is passed as mut to shrink here, so the proof ends`.
The same holds on the right of `&&` or `||` inside the condition, as
in <!-- case: cond-write-and-chain -->`len(l) > 0 && shrink(mut l) && l[0] == 0`,
and after an early exit whose condition writes on the
right: <!-- case: cond-write-or-early-exit -->`if len(l) <= 5 || shrink(mut l) { return }`
proves nothing about `l` after it. A call on the left runs before the
comparison, so `shrink(mut l) && len(l) > 5` still proves `len(l) > 5`.
After an early exit the region starts with the branch that does not
leave: <!-- case: cond-write-else-falls-through -->`if len(l) <= 5 { return } else { shrink(mut l) }`
proves nothing after it, while a write in the branch that leaves never
matters. A `switch` is the chain of ifs it stands for: a case's values are
tried in turn, so a later value, and a later case's values, are in the
region of what an earlier one not matching
proves: <!-- case: cond-write-case-later-value -->`case len(l) <= 5: return` then
`case shrink(mut l): use(l[5])` reports the index. A switch's tag is
evaluated once, before any value, and compared as it was then, so what a
case proves about the tag's variable also needs every value tried up to
that case to leave it
unassigned: <!-- case: cond-write-tag-switch-earlier-value -->`switch d { case five(mut d): ... case 0: return; default: use(100 / d) }`
reports the division. A counting loop's
start holds only while its condition assigns nothing
either: <!-- case: cond-write-counting-loop -->`for mut i := 0; neg(mut i) && i < 3; i++ { use(a[i]) }`
proves only `i < 3`.

A fact that names a string through `len(s)` also needs the
region to leave `s` unassigned; `s` may be `mut` as long as the region does
not change it. Assigning a whole array does not affect proofs about its
indexes, since its length cannot change.

A **mutable global** never has a fact, since any call can change it, so
its error suggests only the checked
form, <!-- error: safety-no-proof-mutable-global -->`divisor g may be zero; use q, ok := ... / g`,
with a note at its
declaration: <!-- note: safety-no-proof-mutable-global -->`g is a mutable global, so no check proves it; copy it into a local first`.
So does an index into a mutable global string, whose length no check
proves: <!-- error: note-global-string-indexed -->`index 1 may be out of range; use v, ok := ...[1]`.
An **immutable** global always has a known value, since its initializer
is a constant expression, and so do its fields and elements.

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
| <!-- case: safety-no-proof-field -->`if p.d != 0 { n / p.d }` with `p` a `mut` parameter or variable | a field of a mutable variable or a `mut` parameter never carries a fact; copy it into a variable first |
| <!-- case: safety-no-proof-not-a-name -->`a[i + 1]` after `if 0 <= i && i < 2`        | only a name, or a field or element of an immutable variable, is proven  |
| <!-- case: field-fact-mutable-index -->`if p.a[j] != 0 { n / p.a[j] }` with `j` mutable | an element in a path is proven only at a known index or an immutable name's |
| <!-- case: safety-no-proof-unknown-bound -->`if 0 <= i && i < n` with `n` not known      | a comparison with a variable that is not a known value gives no fact    |
| <!-- case: safety-no-proof-variable-comparison -->`if b > c { a / b }`                         | the same                                                                |
| <!-- case: mixed-compare-no-lower-bound -->`if i < limit { a[i] }` with `limit := uint64(3)` and `i` an `int64` | a comparison bounds `i` by value, here only above; `i` may be negative |
| <!-- case: safety-no-proof-string-len-minus-one -->`if 0 <= i && i <= len(s) - 1 { s[i] }`      | `len(s)` of a string variable is not known, and only `i < len(s)` bounds `i` by it |
| <!-- case: safety-no-proof-string-equality -->`if s == "abc" { s[0] }`                     | comparing strings says nothing about the length                         |
| <!-- case: safety-no-proof-exit -->`if b == 0 { exit(1) }` before `a / b`       | `exit` is a call, not a way out of the block; the rule is syntactic     |
| <!-- case: safety-no-proof-or-one-side -->`if b < 0 \|\| c > 0 { a / b }`              | only one side says anything about `b`                                   |
| <!-- case: safety-no-proof-multi-value-case-out-of-range -->`switch i { case 0, 3: a[i] }`               | the case allows `i == 3`                                                |
| <!-- case: safety-no-proof-loop-assigns -->`for i < 3 { i++; a[i] }`                    | the body assigns `i` before the use                                     |
| <!-- case: safety-no-proof-mutable-global -->`mut g int64` (global), `if g != 0 { x / g }` | a mutable global never has a fact                                      |
| <!-- case: safety-no-proof-range-longer -->`for i := range b { a[i] }` with `b` longer than `a` | the index bound is `len(b) - 1`                                 |
| <!-- case: safety-no-proof-call-length -->`name()[i]` after `if 0 <= i && i < len(name())` | only a string variable's length can carry a fact                    |
| <!-- case: safety-no-proof-conversion-guard -->any guard around `uint8(x)`                  | conversions follow the type rule only                                  |

### Notes on a missing proof

When a guard in scope says something about a divisor or index it did not
prove, the error comes with notes that say why. Each note points at the
code it is about, so its text never repeats a position:

- a guard whose fact the region dropped says what it proves, and the next
  note points at the assignment that dropped it;
- when that fact would not have been enough anyway, a note says what it
  would have lacked;
- a guard whose fact holds but is too weak says what it allows, such as
  `i == 3` for a `[3]T`, or that it bounds the index only above.

For `for i < 3 { i++; sum += a[i] }` the error is followed
by <!-- note: safety-no-proof-loop-assigns -->`this guard proves i < 3`,
under `i < 3`,
then <!-- note: safety-no-proof-loop-assigns -->`but i is assigned here, so the proof ends`,
under the `i` of `i++`,
and <!-- note: safety-no-proof-loop-assigns -->`this guard would bound i only above; i may be negative`.
An operation no guard mentions keeps its error alone.

An unsigned index is never negative, so no note explains a lower bound on
it: not that a guard bounds it only above, and not that a lower bound it
was given does not hold. For
<!-- case: unsafe-index-loop-unbounded -->`for mut i unsafe_size_t = 2; i <= n; i++ { total += a[i] }`
the error stands alone, though the loop's step could wrap `i` on a 32-bit
target, because what is missing is a bound below `len(a)`.

Other notes point at a branch that ends in `exit`, which would have left
the block had it been a `return`; at a counting loop's step that could
wrap; at a guard on a field of a mutable variable, or on the length of
one, <!-- note: note-field-len-guard -->`this guard proves nothing: a field of a mutable variable or a mut parameter never carries a fact; copy l.src into a variable first`,
or on a variable used inside a larger divisor or index; and, in `v, ok := m[i][j]`, at the element whose outer index
alone the checked form covers. One does not need a guard: the declaration of
a mutable global. An unproven unsigned index into a string is told to
check only the upper bound, whatever its width, since any integer
compares with `len(s)` by
value: <!-- error: string-index-uint64 -->`index u may be out of range; check u < len(s) first, or use v, ok := ...[u]`.
A target-width index, `unsafe_size_t` or `unsafe_uintptr_t`, is told the
same, whatever the target's width, here into a
`[4]int64`: <!-- error: unsafe-index-array -->`index s may be out of range; check s < 4 first, or use v, ok := ...[s]`.

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

An unsigned index needs only the upper bound, and a `uint64` one compares
with `len(s)` by value like any other:

<!-- example: mixed-compare -->
```tack
func at(s string, u uint64) uint8 {
    if u < len(s) {
        return s[u]
    }
    return 0
}
```

So does a target-width one, `unsafe_size_t` or `unsafe_uintptr_t`, whose
values the compiler bounds by its widest, 64 bits, whatever the target's
width. A counting loop over one proves its index by its condition, here
`i < len(str)`, even though its step is not sure not to wrap on a 32-bit
target, since the condition is tested on every entry to the body:

<!-- example: unsafe-sizes -->
```tack
mut bytes int64 = 0
for mut i unsafe_size_t = 0; i < len(str); i++ {
    bytes += str[i]
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

### Reading a view's element

A view, `[]T`, is a parameter whose length is never known, so its index is
proven exactly as one into a string parameter is, against `len(v)`:

<!-- case: view-unproven-index -->
```tack
func at(v []int64, i int64) int64 {
    return v[i]
}
```

<!-- error: view-unproven-index -->
`index i may be out of range; check 0 <= i && i < len(v) first, or use v, ok := ...[i]`

A constant index
gets <!-- error: view-unproven-constant -->`index 0 may be out of range; check len(v) > 0 first, or use v, ok := ...[0]`,
and a negative one is always an
error, <!-- error: view-index-negative -->`index -1 is out of range for a view`.

Guarded, by a check, a counting loop or a range loop:

<!-- example: views -->
```tack
func guarded(v []int64, i int64) int64 {
    if 0 <= i && i < len(v) {
        return v[i]
    }
    return -1
}
```

<!-- example: views -->
```tack
for mut i int64 = 0; i < len(v); i++ {
    total += v[i]
}
```

<!-- example: views -->
```tack
for i := range v {
    total += v[i]
}
```

A view cannot change during its function's call: nothing in it can assign
the view or pass it as `mut`, and the caller passes a copy of an array the
call could otherwise change. So a fact about `len(v)` holds for the rest of
the function, calls and all:

<!-- example: views -->
```tack
func firstTwo(v []int64) int64 {
    if len(v) < 2 {
        return -1
    }
    return v[0] + v[1]
}
```

Checked:

<!-- example: views -->
```tack
func at(v []int64, i int64) int64 {
    x, ok := v[i]
    if !ok {
        return -1
    }
    return x
}
```

A view is read-only: writing an element, in any form,
is <!-- error: view-assign-element -->`cannot assign to v[0]: v is a read-only view`.

A view a call returns, `t.bytes()`, has a length no fact is about, since
a call is no subject, so its index is never proven, even under a guard
that calls it too:
<!-- error: view-result-unchecked-index -->`index 0 may be out of range; use v, ok := ...[0]`.
It is read checked:

<!-- example: view-results -->
```tack
c, ok := t.bytes()[1]
check(ok && c == 'e', "checked index in range")
```

and its slice is proven only at bounds no length can fail, `[:]`, `[0:]`
and `[:0]`, so `t.bytes()[1:]` is
<!-- error: view-result-unproven-slice -->`slice bound 1 may be out of range`.

A variadic parameter, `nums ...int64`, is a view, `[]int64`, inside its
function, so its index is proven or checked the same way, whatever values a
call passes, since the body is checked once for every caller. Unproven,
`nums[0]`
is <!-- error: variadic-index-unproven -->`index 0 may be out of range; check len(nums) > 0 first, or use v, ok := ...[0]`;
guarded:

<!-- example: variadics -->
```tack
func firstTwo(nums ...int64) int64 {
    if len(nums) < 2 {
        return -1
    }
    return nums[0] + nums[1]
}
```

### Slicing

A slice, `x[i:j]`, needs three things proven: `0 <= i`, `i <= j` and
`j <= len(x)`. A bound left out stands for 0 or `len(x)`, so `x[:j]` needs
`0 <= j <= len(x)` and `x[i:]` needs `0 <= i <= len(x)`; `x[:]` needs
nothing. Each of the three is proven as an index's bounds are, by known
values, a variable's type, and the facts in force, with the two relations
only a slice uses, `j <= len(x)` and `i <= j`. Unproven, the error names
what is missing, and only that:

<!-- case: slice-unproven-all -->
```tack
func cut(s string, i int64, j int64) string {
    return s[i:j]
}
```

<!-- error: slice-unproven-all -->
`slice bounds i:j may be out of range; check 0 <= i && i <= j && j <= len(s) first, or use v, ok := ...[i:j]`

With `0 <= i && j <= len(s)` checked, what is left
is <!-- error: slice-unproven-order -->`slice bounds i:j may be out of range; check i <= j first, or use v, ok := ...[i:j]`;
with `i <= len(s)` alone, before `s[i:]`, it
is <!-- error: slice-unproven-lower -->`slice bound i may be out of range; check 0 <= i first, or use v, ok := ...[i:]`,
with the note <!-- note: slice-unproven-lower -->`this guard bounds i only above; i may be negative`.
A known bound needs the length checked, as a known index
does, <!-- error: slice-unproven-known-bound -->`slice bound 1 may be out of range; check len(s) >= 1 first, or use v, ok := ...[1:]`.
An array's length is known, so the check names it, here for a `[3]int64`
after `if 0 <= j && j <= 4`, <!-- error: slice-unproven-high-past-array -->`slice bound j may be out of range; check j <= 3 first`,
with <!-- note: slice-unproven-high-past-array -->`this guard allows j == 4, and a has 3 elements`.
A view's slice, and an array's, have no checked form, so their error
suggests only the check, <!-- error: slice-unproven-view -->`slice bound i may be out of range; check 0 <= i && i <= len(v) first`.

Guarded, by a check, a counting loop or a range loop:

<!-- example: slices -->
```tack
func between(s string, i int64, j int64) string {
    if 0 <= i && i <= j && j <= len(s) {
        return s[i:j]
    }
    return "?"
}
```

<!-- example: slices -->
```tack
for mut i := 0; i < len(v); i++ {
    total += sum(v[:i])
}
```

<!-- example: slices -->
```tack
for i := range v {
    total += sum(v[i:])
}
```

Checked, for a string's slice only, which gives `""` and `false` when the
bounds are out of range, whatever their types:

<!-- example: slices -->
```tack
func checked(s string, i int64, j int64) (string, bool) {
    t, ok := s[i:j]
    return t, ok
}
```

An array's or a view's slice is a view, which no variable may hold,
so it cannot be bound with ok:
<!-- error: slice-view-checked -->`a[1:] is a view, []int64, which cannot be bound with ok; prove its bounds instead`.
A slice is never a place:
assigning one is <!-- error: slice-assign -->`cannot assign to s[0:1]: a slice is not a place`.

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

A value fits a target-width type, `unsafe_size_t` or `unsafe_uintptr_t`,
on every target only from 0 to 4294967295, and that is what `T(x)` takes
from a constant or known value. A constant past it is an error in either
form, <!-- error: unsafe-const-overflow -->`constant 4294967296 overflows unsafe_size_t: its width is unknown, so a constant must be at most 4294967295`.
A known value past it, but within 64 bits, fits on some targets only, so
`T(x)` is the error a value not known gets, as for `unsafe_size_t(x)`
after `x := uint64(5000000000)`, <!-- error: unsafe-conversion-known-past-32-bits -->`converting uint64 to unsafe_size_t can fail; use v, ok := unsafe_size_t(...)`,
and the checked form tests it against the target's real largest value at
run time, as it does a value not known. The two target-width types do not
widen to each other, since one may be narrower than the other, so
`unsafe_uintptr_t(s)` for an `unsafe_size_t` `s` needs the checked form
too.

## Errors

A function that can fail returns an error, a value of type `error`, as
its last result, and the results before it are what it gives when it
succeeds. The error is `code` and `msg`, an `int32` and a `string`; a
code of 0 is no error, whatever the message, and `noerror` is that value.
The proof system covers the rest: the results that come with an error
are not read until the facts in force prove the error's code is 0, just
as a divisor is not used until they prove it is not zero.

After `v, err := f()`, or `v, err = f()`, `err` holds `v`, and each read
of `v`, as a value, a `mut` argument, or the target of a compound
assignment, `++` or `--`, needs a check of `err`:

<!-- case: error-read-unchecked -->
```tack
v, err := parse("1")
print(err.msg)
if v > 0 {
```

reports <!-- error: error-read-unchecked -->`v cannot be read before err is checked; check err.code == 0 first`.
Checked, by an early exit:

<!-- example: errors -->
```tack
func double(s string) (int64, error) {
    v, err := parse(s)
    if err.code != 0 {
        return 0, err
    }
    return v * 2, noerror
}
```

A check is a comparison of `err.code` with a known value whose facts
bound the code to exactly 0, under every rule a fact follows: `err.code
== 0` being true, `err.code != 0` being false, `!`, `&&` and `||`
combining them, and a `switch` on `err.code`, whose `case 0:` proves it
in its body. It holds where any fact holds: an `if`'s branch, the rest
of a block after an early exit, a loop's body, the right of `&&` or
`||`, a case, and deferred code, with the facts in force at its
`defer`. `err.code` carries this fact whether `err` is mutable or not,
though a field of a mutable variable carries none for any other proof.
Comparing `err` itself, `err == noerror`, is not a check.

<!-- example: errors -->
```tack
func describe(s string) string {
    v, err := parse(s)
    switch err.code {
    case 0:
        if v > 9 {
            return "big"
        }
        return "small"
    case 1:
        return "empty"
    default:
        return err.msg
    }
}
```

A check that leaves an error possible is not one, and its note says
what it allows: after `if err.code > 0 { return }`, a code may still be
negative, <!-- note: error-read-weak-check -->`this early exit allows err.code == -1`.
A branch ending in `exit` does not leave, as for any fact,
so <!-- case: error-read-exit -->`if err.code != 0 { exit(1) }` checks
nothing, <!-- note: error-read-exit -->`but its branch ends in exit, not in return, break or continue`.

A check holds up to the first binding of a new error to `err` that can
reach the read, as a fact about a divisor does, so a read after the
rebinding needs a check of its own:

<!-- case: error-read-after-rebinding -->
```tack
mut v, mut err := parse("1")
if err.code != 0 {
    return
}
v, err = parse("")
if v > 0 {
    print("parsed\n")
}
```

reports the read with the notes <!-- note: error-read-after-rebinding -->`this early exit proves err.code == 0 after it`
and <!-- note: error-read-after-rebinding -->`but err is assigned here, so the proof ends`.
A read before the rebinding keeps the check, and so does a read in a loop
whose body checks `err` again before it, while a rebinding later in a
loop whose check is outside it reaches the read on the next
pass: <!-- case: error-read-loop-rebinding -->`for i := range 3 { if v > i { ... }; v, err = parse("2") }`
after the check reports the read.

The proof is about `err`'s value, and `v` is read on its strength, so
`err` must still hold the error of the call that gave `v`. Two rules
keep it so. A binding that takes a result beside an error binds both to
local variables, never to a parameter, a global, a field or an
element: <!-- error: error-bind-field -->`p.x cannot take a result of parse beside its error; bind it to a local variable`,
and <!-- error: error-bind-global-error -->`last cannot take the error of parse beside v; bind it to a local variable`.
And an error that holds variables is assigned only by a binding that
assigns all of them with it. Anything else, an assignment, to it or to
a field of it, `++`, a `mut` argument, or a binding that leaves one of
them out, could make it no error while `v` keeps the failed call's
value:

<!-- case: error-assign-alone -->
```tack
v, mut err := parse("")
err = noerror
if err.code != 0 {
    return
}
```

reports <!-- error: error-assign-alone -->`err holds the error that came with v, so only a binding that also assigns v can assign it`,
with <!-- note: error-assign-alone -->`err is bound beside v here`.
A variable bound beside two errors, at two bindings, needs both checked.

An error cannot be dropped either. A call statement whose callee returns
one
is <!-- error: error-dropped-call -->`the error parse returns cannot be dropped; bind it, as in _, err := parse(...)`,
naming a result that cannot be dropped either, as a list cannot, in place of its `_`,
<!-- error: bytes-repeat-dropped -->`the error bytes.repeat returns cannot be dropped; bind it, as in x, err := bytes.repeat(...)`,
and `_` in its
place <!-- error: error-dropped-blank -->`the error parse returns cannot be dropped; bind it to a variable instead of _`.
A function's error is its last
result: <!-- error: error-not-last -->`error must be the last result of f`.

## Lists

A list, `list[T]`, owns memory: its buffer, which its allocator gives
it as `append` grows it, and takes back when the list is freed; a map
owns memory the same way (see [Maps](#maps)). Three properties make that safe, and the compiler checks each.

**Indexes stay in range.** A list's length is never known, so an index
into one is proven as one into a string parameter is, against `len(l)`,
by a guard, a range loop or an early exit, or read with the checked
form. A fact about `len(l)` holds up to the first write to `l` that can
reach the use: an assignment, a `mut` argument, or a move;
`append(mut l, v)` passes it as `mut`, so it ends the proof, as
`reserve(mut l, n)` does, though it changes no length. A successful
append proves a length of its own instead: it adds all of its values or
none, and adding none is an error, so once a check proves
`err.code == 0` for `err := append(mut l, a, b)`, `len(l)` is at least
2, and `l[1]`, `pop(mut l)` and `10 / len(l)` need no other proof. The
count is the number of values passed, or the length of an array spread
whole; a spread of anything else, `reserve`, an error bound to a `mut`
variable and an append to another list prove nothing. The proof ends at
a write to `l` that can run between the append and the use, as a fact
does, so <!-- case: proof-append-then-pop -->`err := append(mut l, 1)`,
checked, then two pops, reports the
second, <!-- error: proof-append-then-pop -->`pop may find l empty; check len(l) > 0 first, or use x, ok := pop(mut l)`,
and a defer reading `l[0]` that runs after a pop
reports <!-- error: proof-ends-before-defer-runs -->`index 0 may be out of range; check len(l) > 0 first, or use v, ok := ...[0]`.
The append's proof is its own, and does not add to a guard's:

<!-- case: list-len-dropped-by-append -->
```tack
if len(l) > 0 {
    err := append(mut l, 1)
```

proves `len(l) >= 1` after the check, not 2, and reports the read of `l[1]` after it with <!-- note: list-len-dropped-by-append -->`but l is passed as mut to append here, so the proof ends`.
Writing an element, `l[i] = v`, changes no list's length, so it ends no
fact, and a write needs its index proven as a read does, with no checked
form: <!-- error: list-index-write-unproven -->`index i may be out of range; check 0 <= i && i < len(l) first`.
A known index or slice bound is never in range when negative:
<!-- error: list-checked-negative -->`index -1 is out of range for a list`,
<!-- error: list-slice-bound-negative -->`slice bound -1 is out of range for a list`.

**One owner, one free.** The variable holding a list owns it and frees
it when its block ends, so a list is never copied: declaring a variable
with one, assigning one, returning one or writing one as a struct
literal's field moves it, and the variable it came from cannot be used
again until it is assigned. Every use of a moved list is an error,

<!-- case: move-declare-then-use -->
```tack
m := l
print("x")
if len(l) > 0 && len(m) > 0 {
```

reports <!-- error: move-declare-then-use -->`l is used after it was moved`,
with <!-- note: move-declare-then-use -->`l is moved here`, and a list
moved on some paths only is unusable after them,
<!-- error: move-conditional-then-use -->`l is used after it may have been moved`.
A move in a loop's body is seen by the next iteration, unless the body
assigns the variable first: <!-- note: move-in-loop -->`l is moved here, in an earlier iteration of the loop`.
A parameter borrows its list, and a `mut` parameter refers to its
caller's, so neither is moved, <!-- error: move-borrowed-param -->`cannot move l: it is a parameter, which borrows its caller's value`,
and a field is never moved on its own,
<!-- error: move-field -->`cannot move s.items out of s: a field cannot be moved on its own`;
`old := replace(mut s.items, new)` takes it out with `new` left in its
place, so the struct always holds a list. Its `new` is moved in, and an
owned one that may overlap the place is
<!-- error: replace-owned-from-itself-local -->`cannot pass l alongside mut l in this call: a list is not copied for a call, and a mut argument must be the only way to reach l during it`.
A list that no variable holds, a call's result or `make`'s, must be given
one, or it would never be freed:
<!-- error: move-temporary-len -->`cannot use a temporary list[int64] here; store it in a variable first`,
<!-- error: move-dropped-make -->`make returns list[int64], which cannot be dropped; bind it to a variable`.
No package-level variable may hold a list, <!-- error: list-global -->`package-level variable g cannot hold a list`.

The frees are where the compiler puts them, at the end of the owner's
block, by every way out of it, as if a defer freeing the list were
written after its declaration, and they are tested, not assumed:
`examples/lists` counts the buffers its allocator has given out and not
had back, and checks the count is back to zero after every check,
returns, breaks, continues, conditional moves and reassignments
included.

<!-- example: lists -->
```tack
func conditional(move bool) {
    mut l := make(list[int64], c_allocator)
    err := fill(mut l, 2)
    check(err.code == 0, "fill for conditional")
    if move {
        m := l
        check(len(m) == 2, "moved on one path")
    }
}
```

A move empties its variable, so the free at the end of `conditional`
frees the list on the path that kept it and nothing on the one that
moved it. A defer runs when its block ends, so one that reads a list
moved later in its block is an error,
<!-- error: move-defer-reads-later-moved -->`l is moved after this defer, which reads it when its block ends`,
and so is deferred code moving a list declared outside it,
<!-- error: move-defer-moves-outer -->`a defer cannot move l, which is declared outside it`.

**No buffer read after it moves.** Growing a list may move its buffer
and free the old one, so nothing may read the old one then. A list is
never copied for a call, so an argument that reads one in place, a list
or a struct holding one borrowed by a plain parameter, or viewed by a
view parameter, cannot overlap a place the same call passes as `mut`:
<!-- error: list-borrow-alongside-mut -->`cannot pass l alongside mut l in this call: a list is not copied for a call, and a mut argument must be the only way to reach l during it`,
a call nested in the arguments included. A list spread into an append
to itself, or a slice of it, is such a view, through a struct's field
too:
<!-- error: list-append-spread-self-field -->`cannot pass s.items alongside mut s.items in this call: a list is not copied for a call, and a mut argument must be the only way to reach s.items during it`.
An append or a reserve that cannot grow the buffer changes nothing: it
grows once, to the final length, before writing anything, so its error
leaves the list's length, capacity and elements as they were, which
`examples/list-bulk` checks through its C. A range loop walks the list
itself, so its body cannot grow, assign or move it:
<!-- error: move-range-append -->`l is being ranged over, so this loop cannot pass mut l`.
A view a function returns may point into any argument of its call that
is not `mut` and holds data, the receiver included, and is read in
place, so the same holds for each of them: passed alongside a `mut`
argument that may overlap one,
<!-- error: view-result-overlap-after -->`cannot pass t.bytes() alongside mut t in this call: the view it returns may point into t, and a mut argument must be the only way to reach t during the call`,
and changed, anything inside it included, by a range over the view,
<!-- error: view-result-range-mut -->`t.bytes() is being ranged over, and the view it returns may point into t, so this loop cannot pass mut t`.
An index reads its list in place too, after its index is evaluated, so
an index whose own expression may change that list, through a `mut`
argument at any depth, is an error, the checked form and an element
being assigned included,
<!-- error: index-borrow-list -->`cannot index l alongside mut l in its index: a list is not copied to be indexed, and a mut argument must be the only way to reach l during its call`,
and the same for a view a call returns,
<!-- error: index-borrow-view -->`cannot index first(l) alongside mut l in its index: the view it returns may point into l, and a mut argument must be the only way to reach l during its call`,
and for a slice's bounds,
<!-- error: slice-borrow-bound -->`cannot slice l alongside mut l in its bounds: a list is not copied to be sliced, and a mut argument must be the only way to reach l during its call`.
An element is assigned in place as well, so the value assigned to an
element of a list cannot change that list, or anything holding it,
<!-- error: assign-borrow-element -->`cannot assign to l[0] alongside mut l in its value: an element is assigned in place in its list, and a mut argument must be the only way to reach l during its call`;
a `mut` argument inside the list, one of its elements, may be passed,
since it cannot move the list's own buffer.
No global holds a list and no reference is ever stored, so a callee
reaches a caller's list only through its arguments, and these rules
cover every way a buffer could move while something reads it. An
element of a list is part of it for both: two elements of one list may
always be one, so
<!-- error: owned-element-borrowed-alongside-mut-element -->`cannot pass l[0] alongside mut l[1] in this call: a list is not copied for a call, and a mut argument must be the only way to reach l[1] during it`.

**Owned elements.** A list may hold lists, and structs and unions
holding them, to any depth, and owns each: freeing a list frees every
element it holds, last first, then its buffer, and reassigning it frees
every old element. An element of an owned type is never copied, since a
copy would be a second owner. It is a place inside its list, read,
grown, matched and borrowed where it is, as a struct's list field is,
and never moved out of it, which would leave the list holding what it
no longer owns:
<!-- error: owned-element-moved-by-declaration -->`cannot move l[0] out of l: an element cannot be moved out of a list; take it out with pop or remove`,
through a view too,
<!-- error: owned-element-of-view-moved -->`cannot move v[0] out of v: an element cannot be moved out of a view, which only borrows it`.
The checked form would copy one, so it is
<!-- error: owned-element-checked-index -->`the checked form cannot read an element of type list[int64]: it would copy it out of l; check 0 <= i && i < len(l) first and use l[i] in place`.
`l[i] = v` moves `v` in, then frees the element it replaces, once `v`
is computed, as assigning a variable does. `append` moves each value
into the list, so the variable it came from is used up,
<!-- error: owned-element-used-after-append -->`t is used after it was moved`,
and when the buffer cannot grow, the append frees the values it was
given, which no variable holds any longer. Spreading a list of owned
elements would copy them,
<!-- error: owned-element-spread-into-append -->`cannot spread other into append: its elements would be copied, and a list cannot be`.
A range loop's element variable refers to an owned element in place, as
a union's binding refers to its payload: it is never moved,
<!-- error: owned-range-binding-moved -->`cannot move x: it refers to an element of l in place, and an element cannot be moved out of a list`,
and since it may be any element, the loop's body changes none of them,

<!-- case: owned-range-element-assigned -->
```tack
for i, x := range l {
    l[i] = make(list[int64], c_allocator)
```

reports <!-- error: owned-range-element-assigned -->`l is being ranged over, and x refers into it, so this loop cannot assign l[i]`,
so a fact about `len(x)` holds for the body as a fact about an
immutable variable does. An array's and a variadic's elements are
copied when read, so they stay copyable,
<!-- error: list-array-of-lists -->`an array cannot hold list[int64]: its elements are copied, and a list cannot be`,
and no type holds itself through a list,
<!-- error: owned-recursive-through-list -->`invalid recursive type Tree: a type cannot hold itself, even through a list`.

**Taking an element out.** `pop(mut l)` takes a list's last element
and `remove(mut l, i)` its element `i`, moving the rest down; neither
allocates, and neither frees but when written as a statement (below).
The plain forms need the element proven there, as
an index does,
<!-- error: pop-unproven -->`pop may find l empty; check len(l) > 0 first, or use x, ok := pop(mut l)`,
<!-- error: remove-unproven -->`index i may be out of range; check 0 <= i && i < len(l) first, or use x, ok := remove(mut l, i)`,
and each passes `l` as `mut`, which ends the facts about its length for
what runs after it, though not for its own call, which checks before it
writes: `if len(l) > 0 { x := pop(mut l) }` compiles, while a second pop
in the same branch

<!-- case: pop-guard-ends-at-pop -->
```tack
if len(l) > 0 {
    x := pop(mut l)
    y := pop(mut l)
```

reports the second with <!-- note: pop-guard-ends-at-pop -->`but l is passed as mut to pop here, so the proof ends`.
A `pop` or a `remove` written as a statement, `pop(mut l)` or
`remove(mut l, i)`, needs no proof: it drops the element when there is
one, freeing it when it is owned, and does nothing otherwise, so it is
how a program drops an element; binding both results to `_`,
`_, _ = pop(mut l)`,
is <!-- error: pop-results-all-blank -->`every name is _, so nothing is bound; make the call on its own`.
The checked forms, `x, ok := pop(mut l)` and `x, ok := remove(mut
l, i)`, need no proof either. `ok` is false for an empty
list or an index out of range, which leaves the list as it was, and `x`
is then the element type's zero value; a type with none, owned, a union
or an `Allocator`, has no value to give, so `x` cannot be read until `ok` is
checked true, as a result beside an error cannot be until the error is
checked:
<!-- error: pop-read-before-ok -->`x cannot be read before ok is checked; check that ok is true first`.
The check is a fact, `if ok`, `if !ok { return }` or `ok &&`, that
holds in a region never assigning `ok`, and `ok` is assigned only with
`x`,
<!-- error: pop-ok-assigned -->`ok says whether pop gave x, so only a binding that also assigns x can assign it`,
so a check is always about the element `x` holds. An owned element is
never dropped,
<!-- error: pop-owned-dropped -->`pop returns list[int64], which cannot be dropped; bind it to a variable instead of _`,
and neither is the `ok` that guards one,
<!-- error: pop-ok-dropped -->`the ok pop returns cannot be dropped: x cannot be read until it is checked; bind it to a variable instead of _`.
`examples/owned-lists` counts the buffers its allocator holds after
lists of lists, of structs and of unions are built, read and matched in
place, grown, replaced, popped and removed at every edge, nested three
deep, reassigned and made to fail part way, and checks the count is
back to zero after each.

<!-- example: owned-lists -->
```tack
    w, ok := pop(mut words)
    if ok {
        check(is(w, "dddd") && len(words) == 3, "the last word popped")
```

**Type parameters.** A generic function's body is checked once, for
every type a call may pass, and a struct holding a list is one, so a
value of a type parameter is owned as a list is, under every rule
above. It moves rather than copies, and a parameter of one borrows its
argument, <!-- error: generic-copy-param -->`cannot move s: it is a parameter, which borrows its caller's value`;
a moved one cannot be used, <!-- error: generic-use-after-move -->`x is used after it was moved`;
one no variable holds must be given one,
<!-- error: generic-temporary -->`cannot use a temporary S here; store it in a variable first`,
<!-- error: generic-dropped-result -->`stuck returns S, which may hold a list and cannot be dropped; bind it to a variable`;
it has no zero value, <!-- error: generic-zero-value -->`z needs a value: S has no zero value`;
it is never borrowed alongside a `mut` argument of itself,
<!-- error: generic-arg-borrow-alongside-mut -->`cannot pass w alongside mut w in this call: a list is not copied for a call, and a mut argument must be the only way to reach w during it`;
and it is no element,
<!-- error: generic-array -->`an array cannot hold S: its elements are copied, and S, a type parameter, which may hold a list, cannot be`.
Each instance of the function is compiled with its type arguments, so
its moves and frees are those of a struct holding a list where the type
argument holds one, and plain copies, with nothing to free, where it
does not. `examples/generics` passes a struct holding a list through
generic functions that borrow it, change it and move it, and checks the
count of live buffers is back to zero after each check.

**Unions.** A union any of whose cases' payloads holds a list, directly
or through a struct, an array or another union, holds a list, and is
owned as a struct holding one is, under every rule above. It moves
rather than copies, <!-- error: union-moved -->`u is used after it was moved`;
one no variable holds must be given one, a switch's tag included,
<!-- error: union-temporary-owned -->`cannot use a temporary U here; store it in a variable first`;
no global holds one, <!-- error: union-global-list -->`package-level variable g cannot hold a list`;
it is no array's element, though a list's it may be, matched in place;
and it compares with `==` as a list does, reading both in place, so
neither operand may be passed as `mut` in the other,
<!-- error: list-eq-alongside-mut -->`cannot compare a alongside mut a in the other operand: a list is not copied to be compared, and a mut argument must be the only way to reach a during its call`.
Freeing one frees what the case it holds holds, and a moved one is
empty, so freeing it does nothing; assigning one frees the old value
once the new one is computed. A payload is built into a union by
moving it there, as into a struct literal's field, and is never moved
out of one: a switch on a union binds a payload by referring to it in
place, as a range loop walks a list, so the binding cannot be moved,
<!-- error: union-move-binding -->`cannot move l: it refers to a union's payload in place, and a payload cannot be moved out of a union`,
and while it refers into the switched place its clause cannot assign
that place, or anything holding it, pass either as `mut`, or move it:

<!-- case: union-mut-matched -->
```tack
switch u {
case items(l):
    reset(mut u)
```

reports <!-- error: union-mut-matched -->`u is being matched, and l refers into it, so this case cannot pass mut u`,
and assigning it is <!-- error: union-assign-matched -->`u is being matched, and n refers into it, so this case cannot assign u`.
A `mut` method called on the place is a `mut` argument of it too, so in
such a clause of a switch on `e`, binding `n`, `mut e.set()`
reports <!-- error: union-method-mut-matched -->`e is being matched, and n refers into it, so this case cannot pass mut e`,
while a plain method call, which only reads the place, may be made,
the binding passed to it included. Inside a `mut` method, the receiver
is a place like any other, so a switch on it binding a payload keeps
the method from reassigning it in that clause,
<!-- error: union-method-receiver-matched -->`e is being matched, and n refers into it, so this case cannot assign e`.
A clause that binds nothing may change the place, since nothing reads
the payload after its case is tested. `examples/unions` counts the
buffers its allocator holds after unions holding lists are built,
reassigned in loops, moved on some paths, read from defers and nested
in other unions, and checks the count is back to zero after each;
`examples/union-methods` does the same after `mut` methods replace and
free a union's list and unions holding lists pass through generic
functions.

<!-- example: unions -->
```tack
func testLoops() {
    mut v := Value.none
    mut sum := 0
    for i := range 10 {
        v = Value.items(listOf(i))
        sum += total(v)
    }
```

## Maps

A map, `map[K]V`, owns its buffers, its keys and its values, which it
frees when its owner's block ends, as a list frees its elements; it
moves rather than copies, as a list does, under the same rules. The rest
of its safety is the same three properties a list's is.

**A key is never assumed to be there.** A value is read only with the
checked form, `v, ok := m[k]`, which gives the value type's zero value
and `false` for a key that is not, or where `has(m, k)` proves the key is
there, and `m[k]` anywhere else
is <!-- error: map-lookup-plain -->`key k may not be in m; check has(m, k) first, or use v, ok := m[k]`.
The checked form copies, so it cannot read an owned value,
<!-- error: map-lookup-owned -->`the checked form cannot read a value of type list[int64]: it would copy it out of m; check has(m, 1) first and use m[1] in place`,
nor one of a type with no zero value to give,
<!-- error: map-lookup-no-zero -->`the checked form cannot read a value of type Allocator: it has no zero value to give when the key is not in the map; check has(m, 1) first`.
`delete` gives its value only with `ok`, which holds it as `pop`'s does,
<!-- error: map-delete-plain -->`delete gives the value and whether the key was there; use v, ok := delete(mut m, k)`.

**A proven key stays there while its value is used.** `has(m, k)`
tested as a condition proves `k` is in `m` as `i < len(l)` proves an
index: by the same proof rules, it holds where the condition does, up to
any write that can reach the use. A write to `m` is a `put`, a `delete`
of any key, an assignment, or `m` passed as `mut`; each can take the key
out, or move the entries a value lives in, so each ends the proof,
<!-- error: map-has-write-between -->`key k may not be in m; check has(m, k) first, or use v, ok := m[k]`,
<!-- note: map-has-write-between -->`but m is passed as mut to put here, so the proof ends`,
a delete of another key included
(<!-- case: map-has-other-key -->), and so does one that reaches the use
through a loop's back edge (<!-- case: map-has-back-edge -->) or runs
before deferred code (<!-- case: map-has-defer -->). Writing a value `m`
holds, `m[k] = v` or `append(mut m[k], x)` for a proven key, changes no
key and moves no entry, so it ends nothing. The key must be one nothing
can change, a constant, or a variable or parameter declared without
`mut`, or a field of one,
<!-- note: map-has-key-reassigned -->`has proves only a key that cannot change: a constant, a variable or parameter declared without mut, or a field of one`;
and the map one whose every write is seen, a variable or a parameter, or
a field or element of one declared without `mut`, not a map held in a
mutable map, whose keys a `mut` argument of its value would change,
<!-- note: map-has-map-not-variable -->`has proves a key only in a map that is a variable or a parameter, or a field or an element of one declared without mut`.
Under the proof `m[k]` is a place, its value in the map's entries, read
and written there; a value cannot be moved out,
<!-- error: map-value-moved -->`cannot move m[k] out of m: a value cannot be moved out of a map; take it out with delete`,
and one assigned cannot be computed by a call that changes the map,
<!-- error: map-assign-borrow -->`cannot assign to m[k] alongside mut m in its value: a value is assigned in place in its map, and a mut argument must be the only way to reach m during its call`.

**Nothing is freed twice or kept after it is freed.** `put` moves its key
and value in; replacing a value frees the old one, and the key given is
freed in favour of the one the map holds; a `put` that cannot grow the
map frees both and leaves the map as it was, and its error cannot be
dropped,
<!-- error: map-put-dropped -->`the error put returns cannot be dropped; bind it, as in err := put(...)`.
`delete` moves the value out and frees the key, or, as a statement,
frees the value too. A key moved into a map cannot be used after,
<!-- error: map-key-moved -->`k is used after it was moved`.

**A walk never sees its map change.** A range over a map binds owned
keys and values in place, in its buffers, so the loop cannot change the
map, <!-- error: map-range-put -->`m is being ranged over, so this loop cannot pass mut m`,
nor assign a value a binding may refer to,
<!-- error: map-has-range-assign -->`m is being ranged over, and v refers into it, so this loop cannot assign m[k]`,
and a lookup whose key passes the map as mut
is <!-- error: map-index-borrow -->`cannot index m alongside mut m in its index: a map is not copied to be indexed, and a mut argument must be the only way to reach m during its call`.

Keys are compared with `==`, so a key type must have one,
<!-- error: map-key-map -->`cannot use map[int64]int64 as a map key: it has no ==`,
and `error` is no key,
<!-- error: map-key-error -->`cannot use error as a map key: an error is not a key`.
`examples/maps` puts, reads, replaces, deletes and ranges over maps of
string, Text and integer keys and of list and struct values, 100000
entries among them, makes every allocation of a `put` fail in turn, and
checks after every group that its allocator holds nothing.
`examples/map-has` reads, assigns, appends to, replaces and calls mut
methods on values under `has`, in maps held by variables, fields, union
payloads, range loops and other maps, makes every allocation of an append
under `has` fail in turn, and checks the same.

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
| known slice bound past a known length     | `a[1:4]` on `[3]int64`, `"abc"[4:]`  | <!-- error: slice-bound-past-array -->`slice bound 4 is out of range for [3]int64` |
| negative known slice bound                | `a[-1:]`                              | <!-- error: slice-bound-negative -->`slice bound -1 is out of range for [3]int64` |
| known slice bounds out of order           | `s[3:1]`                              | <!-- error: slice-bounds-out-of-order -->`slice bounds 3:1 are out of order` |
| constant that does not fit its type       | `var b uint8 = 256`, `uint8(256)`, `uint8(x)` after `x := 300` | <!-- error: safety-conversion-constant-overflow -->`constant 256 overflows uint8` |
| arithmetic on known values that overflows | `'a' * 3`; `-m`, `m - 1`, `m / -1` with `var m int8 = -128` | <!-- error: const-overflow-mul -->`constant expression overflows uint8`, or <!-- error: safety-const-overflow-int8 -->`constant expression overflows int8` for `m` |
| a value of more than 2^31 bytes           | `[2147483649]uint8`, a struct whose fields add up past it | <!-- error: size-array-boundary -->`array type [2147483649]uint8 is too large: it holds 2147483649 bytes, more than the 2^31 a value may hold`, or <!-- error: size-struct -->`type Halves is too large: it holds 2147483656 bytes, more than the 2^31 a value may hold` |
| globals of more than 2^30 bytes together  | three `[536870912]uint8` globals      | <!-- error: size-globals-cap -->`global c makes this program's globals hold 1610612736 bytes, more than the 2^30 a program's globals may hold` |
| nesting deeper than 1000 levels            | 1001 parentheses, or a mix of blocks, calls, literals and types that adds up to it | <!-- error: nesting-too-deep -->`expression nested too deeply: more than 1000 levels` |

Arithmetic on values the compiler does not know wraps at runtime instead:
for a `mut` `int8` `m` holding `-128`, `m / -1` is `-128` and `m % -1` is
`0`.
