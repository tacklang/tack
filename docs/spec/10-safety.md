# 10. Safety and proofs

This chapter states the four guarantees every valid program keeps (no
out-of-bounds access, no leak, no use-after-free, no panic) and
describes how the compiler enforces those that rest on proofs: which
operations can fail (division and remainder, indexing arrays, strings,
views and lists, looking up a map's key, `m[k]`, slicing, conversions),
the guard rule and what must be proven for each, known values, facts
and what gives them, relations between variables, where a fact holds
and what ends it, contracts (`ensures`), the checked forms
`v, ok := ...` the language has, which turn a failure into a value, the
rule that an `ok` may not be discarded or left unread, and the operations
that have no checked form, what the prover does not accept, what proves
an `unreachable` place, and the
depth of the stack, which no rule bounds yet. It also lists the known
violations of the guarantees that are parked, as
[guarantees.md](../guarantees.md) does. Leak and use-after-free freedom
are shown in [Ownership and memory](08-ownership.md), and the result of
each operation in [Constants and arithmetic](03-arithmetic.md); this
chapter says only when the operation is allowed.

[safety.md](../safety.md) gathers the same rules with every diagnostic
and many more examples.

## The guarantees

Every program the compiler accepts keeps four guarantees:

1. No out-of-bounds access: it never reads or writes outside the memory
   an array, a list, an arena, a view or a string holds.
2. No memory leak: everything it allocates is freed before the process
   ends, however it ends, `exit` included. The stack and static storage
   are excluded.
3. No use-after-free: it never reads or writes memory after that memory
   has been freed or moved.
4. No panic: it never ends the process abnormally by itself, by a trap,
   a fault, an abort, or running out of stack. An end the program asks
   for, `exit`, is not a panic.

Two things are outside the guarantees: code behind an extern C function,
though the Tack side of the call is not, and ends that come from outside
the process, such as a signal or kill from another process or the
operating system, the operating system killing the process for memory,
and hardware failure.

When the compiler cannot prove an operation safe, it rejects the
program. No check is weakened, skipped or made optional for a program to
compile. Where a proof is not possible, the program uses a checked form,
`v, ok := ...`, or handles an `error` result (see [Errors](09-errors.md)),
so a failure is a value the program sees. A known way to break a
guarantee in accepted code is a bug; those not yet fixed are listed under
[Known violations](#known-violations).

## Operations that can fail

These operations can fail, and Tack has no runtime error for any of
them:

- a division or remainder, `a / b`, `a % b`, `x /= b` and `x %= b`, when
  the divisor `b` is zero, and a signed division, `a / b` and `x /= b`,
  when `a` is its type's smallest value and `b` is -1, whose quotient
  overflows (see [Constants and
  arithmetic](03-arithmetic.md#division-and-remainder));
- an index, read or written, into an array, `a[i]`, a string, `s[i]`, a
  view, `v[i]`, or a list, `l[i]`, when `i < 0` or `i >= len(x)`;
- a slice, `x[i:j]`, of a string, an array, a view or a list, when
  `i < 0`, `j < i` or `j > len(x)`;
- a lookup of a map's key, `m[k]`, read or written, when `k` is not in
  `m`;
- `pop(mut l)`, when `l` is empty, and `remove(mut l, i)` and `swap(mut l,
  i, j)`, when an index is out of range for `l`;
- an integer conversion, `T(x)`, when `x` is not a value of `T`, and a
  conversion of a float to an integer, when the float is NaN, an
  infinity, or its truncation is not a value of the integer type;
- a type assertion, `a.(T)`, when the `any` `a` does not hold a `T`;
- `+`, `-` and `*` on two integers and unary `-` on one, and `x++`,
  `x--`, `x += v`, `x -= v` and `x *= v` on an integer, when the exact
  result is not a value of the type, below 0 included for an unsigned
  one (see [Constants and arithmetic](03-arithmetic.md#integer-overflow)).
  The wrapping operators, `+%`, `-%`, `*%`, their compound forms and
  `-%x`, never fail, and need no proof.

Each is allowed in one of two ways. Under [the guard rule](#the-guard-rule),
the compiler proves where the operation is written that it cannot fail,
and the plain form compiles. Under [the checked form](#the-checked-form),
the program binds the operation to two names, `v, ok := ...`, and `ok`
says whether it failed. Anything else is a compile error that names what
is not proven and suggests the fixes the operation has.

What every other operation yields, and which failures the compiler
reports from known values, is in [Constants and
arithmetic](03-arithmetic.md); a failure it reports that way is an error
in the checked form too (see [The checked form](#the-checked-form)).

## The guard rule

An operation's plain form compiles only where its operands are proven,
from known values, the types of the operands, and the facts in force
where it is written, to be ones for which it cannot fail:

| Operation                    | Proof needed                                                                 |
|------------------------------|------------------------------------------------------------------------------|
| `a / b`, `a % b`, `/=`, `%=` | `b != 0`                                                                     |
| signed `a / b`, `x /= b`     | also `b != -1`, or `a` (`x`) above its type's smallest value                 |
| `a[i]` on `[n]T`             | `0 <= i` and `i <= n - 1`                                                    |
| `s[i]`, `v[i]`, `l[i]`       | `0 <= i` and `i < len(x)`                                                    |
| `x[i:j]`                     | `0 <= i`, `i <= j` and `j <= len(x)`                                         |
| `m[k]`                       | `has(m, k)`                                                                  |
| `pop(mut l)`                 | `len(l) > 0`                                                                 |
| `remove(mut l, i)`           | `0 <= i` and `i < len(l)`                                                    |
| `swap(mut l, i, j)`          | `0 <= i`, `i < len(l)`, `0 <= j` and `j < len(l)`                            |
| `T(x)`, `x` an integer       | `x` at or above `T`'s smallest value and at or below its largest              |

A bound that the operand's type already keeps needs no proof: an
unsigned index needs only its upper bound, and a `uint8` index into a
`[256]T` needs nothing. A bound left out of a slice stands for 0 or
`len(x)`, so `x[:j]` needs `0 <= j` and `j <= len(x)`, `x[i:]` needs `0 <=
i` and `i <= len(x)`, and `x[:]` needs nothing. A target-width type,
`unsafe_size_t` or `unsafe_uintptr_t`, has the range 0 to 4294967295 for
a conversion's proof, the range it has on every target.

A proof is about a *subject*: a name, a parameter or a variable, or a
field or element of an immutable variable or parameter, such as `p.d`,
`p.inner.d`, `p.a[1]` or `p.a[k]` with `k` immutable, or about
arithmetic on subjects, `+`, `-` and `*` on two integers and unary `-` on
one (see [Arithmetic](#arithmetic)). Any other operand, such as a call's
result, `i / 2`, `^i`, or a field of a mutable variable or a `mut`
parameter, is proven only when it is a known value.

```tack run exit=4
package main

func half(a int64, b int64) int64 {
    if b == 0 {
        return 0
    }
    if b == -1 {
        return -%a
    }
    return a / b
}

func main() {
    if half(8, 2) == 4 {
        exit(4)
    }
    exit(1)
}
```

## Known values

A known value proves itself: a known divisor that is not zero, a known
index in range, and a known value converted to a type that holds it need
nothing more. The compiler knows the value of:

- a constant expression, character literals included;
- `len` of an array, and `len` of a known string: a string literal, an
  immutable variable initialized with a known string, or a string field
  or element of an immutable global;
- an immutable variable, local or package-level, whose initializer is a
  known value;
- a field of an immutable global, an element of one at a known index, a
  byte of a known string at a known index, and the same of a literal of
  known values;
- a slice of a known string at known bounds, and `len(x[i:j])` where `i`
  and `j` are known;
- a conversion of a known value that fits its type;
- arithmetic on known values.

A mutable variable, a parameter and a call never have a known value, and
neither does `len` of a mutable string variable. A value of a
target-width type is known only up to 4294967295.

## Facts

Where a value is not known, the compiler uses facts: what a condition
that has just been tested says about a subject. A subject's type also
bounds it, so an unsigned subject is never negative. The conditions that
give facts are:

- a comparison of a subject with a known value, either way round: `b !=
  0`, `b > 0`, `0 <= i`, `i < len(a)` for an array `a`. Two integers of
  any types compare by their values, so the known value may be of any
  integer type;
- for a string, view or list `x` that is a subject, `len(x)` compared
  with a known value, which bounds the length, and `i < len(x)`, which
  bounds `i` by it;
- `has(m, k)`, which proves `k` is in `m`, when `m` is a variable or a
  parameter, or a field or element of one declared without `mut`, and `k`
  is a constant, or a variable or parameter declared without `mut`, or a
  field of one;
- a comparison of two values that are not both known, which gives a
  relation (see [Relations](#relations));
- `c1 && c2` when true, which gives the facts of both sides; `c1 || c2`
  when false, which gives the facts of both sides being false; and `!c`,
  which swaps true and false;
- `c1 || c2` when true, and `c1 && c2` when false, which give only what
  every side agrees on: for a subject every side constrains, the smallest
  range holding what each side allows, and not zero when no side allows
  zero. So `b < 0 || b > 0` proves `b != 0`, and `i == 0 || i == 1`
  proves `0 <= i && i <= 1`. A subject only some sides constrain gets
  nothing.

A fact is in force in these places:

- an `if`'s branch, by its condition being true, and its `else`, by its
  condition being false;
- the rest of the block after an `if` whose branch leaves, by `return`,
  `break`, `continue` or a call to `exit`, or by a `switch` with a
  `default` whose every clause leaves, by its condition being false;
- a loop's body, by its condition;
- a range loop's body, for its index, which is at least 0 and at most its
  count less one, or the length of what it ranges over less one;
- a counting loop's body, for its variable, which is at least the value
  it starts from counting up, or at most it counting down;
- a whole loop, its condition, post and body, for a bound the loop keeps
  on a variable it writes (see below);
- the right side of `&&`, by the left being true, and of `||`, by the
  left being false;
- a `switch` case, as in the chain of ifs it stands for: a case proves in
  its body what `tag == v1 || tag == v2 ...` proves when true, and every
  later case and the default see the earlier values not matching; after
  the switch, each case whose body leaves, tried while every case before
  it leaves too, gives what its values not matching give, and when the
  first case that does not leave is followed only by cases that leave, a
  `default` among them, it gives what its values matching give; a type
  switch proves nothing;
- a case's body that a `fallthrough` enters, which is reached both by
  matching and by the fallthrough: only what both give, joined as for a
  true `||`, each side's range per subject widened to hold the other's.
  Matching gives what held where the switch began, the case's own values
  matching and the cases tried before it not matching; the fallthrough
  gives what holds at it, once the falling case's defers have run, a
  fact about a variable one of them writes ending there. Neither the
  case's values nor the earlier cases not matching prove anything on
  the fallthrough's path, where none was tested, and a value ruled out,
  and the sides of a condition, are not carried through the join. So
  after `switch d { case 0: fallthrough; case 1, 2: ... }` the second
  case's body knows only that `d` is between 0 and 2;
- deferred code, with the facts in force where the `defer` is written;
- the statements after a call whose callee's contract ensures the fact
  (see [Contracts](#contracts));
- after `err := append(mut l, ...)` and a check proving `err.code == 0`,
  `len(l)` is at least the number of values passed, or the length of an
  array spread whole; a spread of anything else, `reserve`, and an error
  bound to a `mut` variable give no such fact, and the fact is the
  append's own, not added to one a guard gave before it.

A three-clause loop counts when its init declares the variable with a
known value, its post steps it by a known amount above zero, up or down,
its condition bounds the variable on the side it moves towards, the step
from that bound stays within the variable's type, and
neither its body nor its condition assigns the variable.

A loop keeps a bound on a variable it writes, a local or a parameter of
an integer type declared before the loop, when:

- the bound holds where the loop starts, by a fact in force there or by
  the known value the variable was last given, by its declaration, a
  declaration without a value giving 0, or by a plain assignment, with no
  write that can reach the loop in between. The bound is a known value
  the variable is at or above, or at or below, or a difference with
  another value a fact in force there relates it to, with no condition
  that its step stay within its type;
- the loop's init does not write the variable: the init runs after the
  loop's start, and a loop whose init writes the variable keeps no bound
  on it;
- every write to the variable in the loop's condition, post and body,
  inner loops and deferred code included, is `x++`, `x--`, `x += e`, `x
  -= e` or `x = e`, and the facts in force at the write, with the bound
  itself, prove
  the value it stores within the bound. A write
  of any other form, a `mut` argument or an assignment of several values
  keeps no bound;
- nothing in the loop writes the other value of a difference, or the
  string, view or list whose length it is.

The bound then holds in the whole loop, on every pass, before and after
every write, and is not in force after it. A bound one loop keeps does
not count as a fact when deciding whether another bound is kept, and a
loop inside eight others, each with a bound that held where it started
on a variable it writes, keeps none.

```tack run exit=3
package main

func last(s string) int64 {
    mut n int64 = -1
    mut i := 0
    for i < len(s) && s[i] != '.' {
        if s[i] == 'a' {
            n = i
        }
        i++
    }
    return n
}

func main() {
    if last("banana.aa") == 5 && last("") == -1 {
        exit(3)
    }
    exit(1)
}
```

A fact about the length of a string, a view or a list proves that length
as a divisor too: where `len(l) > 0` holds, `10 / len(l)` needs nothing
more.

### Arithmetic

Plain `+`, `-` and `*` on integers, and unary `-`, never wrap in a
program that compiles: each is proven to give a value of its type, or
written in its checked form, or the program is rejected. An operation is
proven when the bounds of its exact result, worked out from its
operands' bounds as below, lie within its type on every target, which
for a target-width type is a 32-bit one's, or when its result is no
larger than an operand: `a - b` with `b` at least 0, `a + b` with one
operand at most 0. Each operand's bounds are its known value, a subject's
from its type and the facts in force, a length's, arithmetic's own, a
conversion's operand's, within the conversion's type, and, for `a / b`,
`a % b`, `a & b` and `a >> k`, what their operands give them: a quotient
is never larger in size than `a`, and by a known positive `k` its bounds
are `a`'s divided; a remainder lies between 0 and `a` and is smaller in
size than a known `b`; `a & b` lies from 0 to the operand that is never
negative; `a >> k` has `a`'s bounds shifted. Any other operand, a call,
a wrapping operator, `<<`, is anywhere in its type. Where those bounds
fall short, the relations in force may still prove it: a known amount
added to a subject or a length, `i + 1` under `i < n`, `n`'s type
keeping it at most the largest `int64`, and `a - b` under `b <= a`,
which is at least 0. A statement storing a known value in a mutable
variable, `mut n := 0`, `mut n int64` or `n = 5`, puts the variable at
that value, and one storing arithmetic, or stepping the variable, `n++`
or `n += k`, within the bounds of what it stored, for the rest of its
block up to the next write to it. A three-clause loop's post statement
runs after a pass of the body, so it is proven with what the loop's
condition says in force too, except about what the body writes.

```tack run exit=0
package main

func steps(s string) int64 {
    mut n := 0
    n++
    n += 2
    for mut i := 0; i < len(s); i++ {
        n = i + 1
    }
    return n
}

func half(a int64, b int64) int64 {
    if a >= 0 && b >= 0 {
        return a / 2 + b / 2
    }
    return 0
}

func main() {
    if steps("abc") != 3 || steps("") != 3 {
        exit(1)
    }
    if half(9223372036854775807, 9223372036854775807) != 9223372036854775806 {
        exit(2)
    }
    exit(0)
}
```

A divisor, an index, a slice's bound or a converted value may be
arithmetic: `+`, `-` or `*` on two integers, or unary `-` on one, each
operand a known value, a subject, the length of a string, view or list
subject, or such arithmetic itself. Its bound is worked out exactly from
the bounds of its operands: a subject's from its type and the facts in
force, a length's from the least length the facts give it to the
greatest, or the largest `int64` when they give none. A sum runs from the sum of the two lowest values to the sum of
the two highest, a difference from the lowest of one less the highest of
the other to the highest of one less the lowest of the other, a product
from the least to the greatest product of the operands' ends, and a
negation from the highest negated to the lowest negated. A product of two
values the facts keep from zero, and a negation of one, is not zero
either. So under `0 <= i && i < 4 && 0 <= j && j < 4`, `i + j` lies from
0 to 6, and under `x > 5`, `x - 5` is never zero.

An operation is bounded for such a use only when every value its
operands' bounds allow gives a result within its type; otherwise it has
no bound, and nor does any arithmetic holding it, though the operation
itself is then also an error unless something else proves it. An operation of a target-width type is bounded
only when its result is within the type on a 32-bit target. Each operand
is bounded on its own, so the same subject read twice is two values,
each anywhere in its bound: `x - x` is not known to be 0. Any other
operand, a call, a conversion, `/`, `%`, a shift or a bitwise operator,
gives no bound to the arithmetic holding it. Arithmetic with no bound of
its own still lies within its type's range, which proves
what that range proves, as a `uint8`'s does for a `[256]T`.

```tack run exit=7
package main

func negated(v int64) uint64 {
    if v < 0 {
        return uint64(-(v + 1))
    }
    return 0
}

func pair(i int64, j int64) int64 {
    a := [7]int64{0, 1, 2, 3, 4, 5, 6}
    if 0 <= i && i < 4 && 0 <= j && j < 4 {
        return a[i + j]
    }
    return -1
}

func over(x int64) int64 {
    if x > 5 {
        return 100 / (x - 5)
    }
    return -1
}

func main() {
    if negated(-9223372036854775808) == 9223372036854775807 && pair(3, 3) == 6 &&
        pair(4, 0) == -1 && over(6) == 100 && over(5) == -1 {
        exit(7)
    }
    exit(1)
}
```

## Relations

A comparison of two values that are not both known, each a subject, a
length or a known value, plus or minus a known amount, gives a relation:
how far apart the two are. `i < n` says `i - n <= -1`, and `n <= len(a)`
says `n - len(a) <= 0`. Relations chain, through any number of values,
so `0 <= i && i < n && n <= len(a)` proves `a[i]`. A declaration or a
plain assignment of such a value plus or minus a known amount relates
what it stores to it for the rest of its block: after `k := j - 1`, `k` is
one less than `j`. An index, a slice's bounds and a conversion use
relations, so `0 <= x && x < n && n <= 255` proves `uint8(x)`; a divisor
uses only facts about its own subject.

```tack run exit=3
package main

func at(a []int64, i int64, n int64) int64 {
    if 0 <= i && i < n && n <= len(a) {
        return a[i]
    }
    return -1
}

func main() {
    a := [4]int64{1, 2, 3, 4}
    if at(a, 2, 3) == 3 {
        exit(3)
    }
    exit(1)
}
```

A relation through `x + c` or `x - c` holds only where the other facts
in force keep the step within `x`'s type: in `0 <= i && i + 1 < len(a)`,
`i + 1` is an error, since it overflows for the largest `int64`, while
`i < len(a) && i + 1 < len(a)` proves it and relates `i + 1` to
`len(a)`. `len(x) - c` never leaves an `int64`. A product is no relation, `||` keeps
none, and a query follows at most 64 related values from the two it is
about.

Arithmetic that adds or subtracts known amounts to one subject or
length, `i + 1` or `len(a) - 1`, is that value plus an offset, and the
relations in force bound it as they bound the value itself, where none
of its steps can leave its type: its own bound (see [Arithmetic](#arithmetic))
shows that, or else the relations in force do. So `0 <= i && i < len(a) -
1` proves `a[i + 1]`, `len(s) > 0` proves `s[len(s) - 1]`, and `0 <= i &&
i < len(s)` proves `s[i : i + 1]`, while under `0 <= i && i + 1 < len(a)`
the `i + 1` itself is not proven.

## Where a fact holds

A fact's region is the `if` branch, the loop's body (a three-clause
loop's post statement is not in it), or the rest of the block after an
early exit, and whatever of the condition still runs after the comparison
that gave it. A fact holds in its region up to the first write to its
subject that can reach the use. A write is an assignment, a compound
assignment, `++` or `--`, one of several targets, or a call that passes
the subject, or a part of it, as `mut`, a method's `mut` receiver
included. For a fact about `len(x)`, it is also a write to `x`: assigning
it, passing it as `mut`, `append`, `reserve`, `pop` and `remove`, or
moving it; writing an element of a list ends no fact about its length,
and assigning a whole array ends none about its indexes. For a fact from
`has(m, k)` it is a write to `m`: `put`, `delete` of any key, an
assignment, or a `mut` argument; writing a value `m` holds ends none.

A write reaches the use when it can run before it:

- a write earlier on the path to the use;
- a write later in a loop that lies inside the region and holds the use,
  since the next pass runs the use again with no check between;
- a write anywhere in the rest of a defer's block, for deferred code,
  which runs when the block is left;
- a write in a value of a switch's case written below a `default`, for
  the `default`, which runs after every value is tried, and for every
  case the `default` falls into, directly or down a chain of
  `fallthrough`s, which runs after it;
- a write in a case whose body ends in `fallthrough`, for the cases it
  falls into, as for any code after it;
- a write in a closure, for code after the call it is passed to, since
  the call may run it, and, for code in the closure before the write, the
  closure counting as a loop around its body, since a later run follows
  an earlier one. A `return` in a closure leaves only the closure, so a
  write before it reaches the code after the call.

No fact in force where a closure is written holds in its body about a
mutable variable or a `mut` parameter: the closure runs during the call
it is passed to, later and possibly many times, after its own writes on
earlier runs. A fact about an immutable variable or a plain parameter,
which nothing writes, holds there, and the closure's own conditions give
facts in it as anywhere.

A write that runs only after the use does not end the fact. A call
assigns its `mut` arguments only once they are all evaluated, and an
assignment stores to its target only once its value is computed, so the
call or the assignment a fact is for may write the subject: `if len(l) >
0 { x := pop(mut l) }` and `if d > 0 { d = n / d }` compile. A proof
that an assignment's target needs holds through the whole statement, so
a write in the value, or, in an assignment of several, to another
target, ends it. A write later in a loop's body that reaches the next
pass through the loop's back edge does not end a fact from the loop's
condition, or from a guard inside the body before the use, since the
check runs again on the way.

```tack error
package main

func f(d int64) int64 {
    mut x = d
    if x != 0 {
        x = x / 2
        return 10 / x
    }
    return 0
}

func main() {
}
```

```diagnostics
main.tck:7:21: error: divisor x may be zero; check x != 0 first, or use q, ok := ... / x
main.tck:5:8: note: this guard proves x != 0
main.tck:6:9: note: but x is assigned here, so the proof ends
```

### Leaving branches

A write in a branch or a case that leaves, by `return`, `break`,
`continue` or `exit`, or by a `switch` with a `default` whose every case
leaves, never reaches code outside its `if` or `switch`, after it or
before it, since only the paths that did not take it run there. A case
ending in `fallthrough` leaves exactly when the case it falls into
does, and the `break` or `continue` that case leaves by counts as the
falling case's own. So a
loop's condition still proves `s[i]` below a branch that moves `i` on
and starts the next pass, where the condition proves it again:

```tack run exit=3
package main

func last(s string) int64 {
    mut n int64 = -1
    mut i int64 = 0
    for 0 <= i && i < len(s) {
        if s[i] == ' ' {
            i++
            continue
        }
        if s[i] == 'a' {
            n = i
        }
        i++
    }
    return n
}

func main() {
    if last("a ba na") == 6 {
        exit(3)
    }
    exit(1)
}
```

The rule has these exceptions:

- A branch that falls through is in the region, whatever its neighbors
  do.
- A `break` or `continue` in the leaving branch that lands in a loop
  inside the region lets the write reach whatever that loop reaches from
  where it lands: the code after the loop, for a `break` out of the
  outermost loop in the region, and that outermost loop's next pass, and
  so all of it, for a `continue`, or for a `break` out of a loop inside
  another loop in the region.
- A `return` still runs deferred code, so a write before it reaches what
  that deferred code relies on.
- A branch ending in a `switch` on a union that names every case, with no
  `default`, does not leave for this rule, even when every case leaves; a
  `default` that leaves makes such a branch leave.
- A call to a function that calls `exit` does not leave; only a call to
  `exit` itself does.
- What a counting loop's start or a range loop's bound proves is not
  proven again before the next pass, so a write to the subject anywhere
  in the loop's body ends it, in a branch that leaves too. Such a write
  ends what the condition proves for the loop's post statement as well,
  so its step needs a proof of its own.

```tack error
package main

func sum(a [4]int64, k int64) int64 {
    mut s int64 = 0
    for mut i := 0; i < len(a); i++ {
        if k == 1 {
            i = 5
            continue
        }
        s = a[i]
    }
    return s
}

func main() {
}
```

```diagnostics
main.tck:5:33: error: i++ may overflow int64; use i +%= 1 to wrap, or v, ok := i + 1 and i = v when ok
main.tck:10:15: error: index i may be out of range; check 0 <= i && i < 4 first, or use v, ok := ...[i]
main.tck:5:9: note: counting from this start proves i >= 0
main.tck:7:13: note: but i is assigned here, so the proof ends
```

### Mutable globals

A mutable global never has a fact, since any call can change it, so an
operation whose operand is one is never proven: it is allowed only in a
checked form, where the operation has one, and its error suggests only
that, with a note at the global's declaration: `g is a
mutable global, so no check proves it; copy it into a local first`. An
immutable global always has a known value.

## What does not count as a proof

None of these proves anything:

- a guard on a field of a mutable variable or a `mut` parameter, or on
  the length of one;
- a guard on an operand that is neither a subject nor arithmetic on
  subjects, such as `a[i / 2]` after `if 0 <= i && i < 4`;
- a guard whose bounds let arithmetic reach past what its use allows,
  such as `a[i + 1]` on a `[3]T` after `if 0 <= i && i < 3`, or past its
  type, such as `uint64(-v)` after `if v < 0`, whose `-v` overflows for
  the smallest `int64`, an error of its own;
- two reads of one subject taken as one value: `a[x - x]` after `if 0 <=
  x && x < 3` is not proven, since `x - x` is bounded from -2 to 2;
- an element in a path at an index that is not known and not an
  immutable name's;
- a comparison with a value that is not known, for a divisor, which
  gives only a relation, and a divisor uses none: `if b > c { a / b }`;
- a comparison of `i` with a `uint64` limit, for an `int64` `i`, which
  bounds `i` only above;
- a string equality, `if s == "abc" { s[0] }`, which says nothing about
  the length;
- `len` of a call, `name()[i]` after `if 0 <= i && i < len(name())`, and
  any index of a view a call returns other than through the checked form;
- a guard that allows an edge value the operation does not, such as
  `switch i { case 0, 3: a[i] }` for a `[3]T`, or `int32(v)` after `if
  -2147483648 <= v && v <= 2147483648`;
- `||` where only one side says anything about the subject;
- a call to a function that calls `exit`, standing in for an early exit;
- a counting loop whose step could leave its variable's type, for what
  its start would prove; the step is an error of its own;
- a comparison of a whole error, `err == noerror`, for the results beside
  it.

When a guard in scope says something about an operand it did not prove,
the error comes with notes that say what the guard proves or allows, and
point at the write that ended its proof.

```tack error
package main

func f(a int64, b int64, c int64) int64 {
    if b > c {
        return a / b
    }
    return 0
}

func main() {
}
```

```diagnostics
main.tck:5:20: error: divisor b may be zero; check b != 0 first, or use q, ok := ... / b
```

## Contracts

A function may state, after its results and before its body, facts it
ensures about its parameters whenever it returns: `func nonzero(d int64)
ensures d != 0`. `ensures` is a keyword. The clause holds what an `if`'s
condition gives facts through, joined by `&&`: comparisons of the
parameters, a method's receiver included, their fields and elements,
constants, `len` and `has`, with `!`, `&&`, `||`, `+` and `-`. Each
operand of its `&&` must give a fact, `d != 3 gives no fact; an ensures
clause states facts an if's condition proves, such as d != 0, 0 <= i &&
i < len(a), err.code == 0 or has(m, k)`. The clause names only the
function's parameters, never an own parameter, whose argument the call
moves, and calls only `len` and `has`; a clause that is not a `bool` is
an error, and an extern func has none. As in an `if`, a struct literal in
it is in parentheses. A field or an element of a `mut` parameter carries
no facts, so a clause about one is an error.

The compiler proves the clause on every path that returns, from the facts
in force there: at each `return`, once its values are computed, `f
ensures d != 0, which may not hold at this return`, and at the end of the
body when it can be reached, `f ensures d != 0, which may not hold at the
end of its body`. Deferred code runs after the return, so a defer written
before it that writes a parameter the clause names fails the proof.

A call that is a statement, or the whole value of a declaration or an
assignment, gives the statements after it in its block the facts the
contract states, each parameter replaced by the argument passed for it.
A fact counts only when every parameter it names was passed a local
variable or a parameter of the caller by name: as `mut`, which the
callee changed in place, or as a plain argument nothing else in the call
writes. A field, an element, an expression, a constant, a variadic
parameter's values, an array passed as a view, a variable the statement
assigns, and a call through a function value give none. The facts hold
after the whole statement, never inside it, and, as any fact, until a
write to the variable can reach the use. A `return` whose one value is a
call to a function with a contract proves with that call's facts. A
contract costs nothing at run time.

```tack run exit=5
package main

func nonzero(d int64) ensures d != 0 {
    if d == 0 {
        exit(1)
    }
}

func main() {
    d := 2
    mut n = d
    nonzero(n)
    q := 10 / n
    if q == 5 {
        exit(5)
    }
    exit(2)
}
```

## The checked form

The checked form is the operation, alone, as the value of a declaration
or an assignment of exactly two names:

- `q, ok := a / b`, and `a % b`;
- `v, ok := a + b`, `a - b`, `a * b` and `-a`, on integers;
- `v, ok := a[i]`, `s[i]`, `v[i]` and `l[i]`;
- `t, ok := s[i:j]`, a string's slice only;
- `v, ok := m[k]`;
- `v, ok := T(x)`, an integer or float converted to an integer;
- `v, ok := a.(T)`, a type assertion;
- `x, ok := pop(mut l)`, `x, ok := remove(mut l, i)` and `v, ok :=
  delete(mut m, k)`.

The first value is the result when the operation succeeds, and when it
fails, the zero value of its type: `0`, `""`, `false`, or a struct or
array of zero values. For a type with no zero value, the checked index and
lookup are errors, and the value `pop`, `remove` or `delete`
gives cannot be read until `ok` is checked true. The second value, `ok`,
is a `bool`, `true` when the operation succeeded. For a division, `ok`
says that the divisor was not zero and, for a signed one, that the
division was not the smallest value of its type divided by -1, which
overflows; for a remainder, only that the divisor was not zero. For
arithmetic, `ok` says
whether the exact result is a value of the type, and the first value is
that result, or 0. Both operands of a checked division, and of checked
arithmetic, are always evaluated, the left first.

```tack run exit=3
package main

func div(a int64, b int64) int64 {
    q, ok := a / b
    if !ok {
        return -1
    }
    return q
}

func main() {
    if div(8, 0) == -1 && div(9, 3) == 3 {
        exit(3)
    }
    exit(1)
}
```

The rules of the form:

- `=` works too, `q, ok = a / b`, under the rules for assigning several
  results.
- The operation is the whole value; parentheses around it are allowed.
  `q, ok := a / b & 1` is `the checked form must be the whole value: q,
  ok := a / b, then compute q & 1`.
- Only the outermost operation is checked: in `v, ok := m[i][j]`, the
  inner `m[i]` needs its own proof, and `q, ok := a / b + 1` is a checked
  addition whose `a / b` needs one.
- One name is the plain form, which needs its proof; three or more are
  `assignment mismatch: 3 variables but a division gives 2 values`.
- An operation that always fails is still an error, the checked form
  included: a division by a known zero, a known index out of range, a
  constant that does not fit.
- Where the operation cannot fail, the checked form is still allowed, and
  `ok` is always `true`.

A checked form whose element or value is owned would copy it out of what
holds it, so it is an error; the element is used in place under a proof
instead (see [Ownership and memory](08-ownership.md#lists)).

### The ok

The `ok` is the check itself, and it may not be discarded. `_` may take the
value, `_, ok := m[k]`, but never the `ok`: `v, _ := m[1]` is `the ok
m[1] gives cannot be discarded; bind it to a variable instead of _`, and
so for every checked form, each named as written. Both names `_` is
`every name is _, so nothing is bound`.

An `ok` bound to a variable that nothing ever reads is discarded as surely,
and is an error at its name: `the ok m[1] gives is never checked: ok is
not read`, or, for `pop`, `remove` and `delete`, `the ok pop returns is
never checked: ok is not read`. Only a variable never read at all is
reported, not one assigned again before its first value was read.

```tack error
package main

func main() {
    m := make(map[int64]int64, c_allocator)
    v, ok := m[1]
    if v == 0 {
        exit(1)
    }
}
```

```diagnostics
main.tck:5:8: error: the ok m[1] gives is never checked: ok is not read
```

### Operations with no checked form

These operations have no checked form, so their operands must be
proven, and their errors suggest only the check:

- `x /= b` and `x %= b`; `q, ok := x / b` and assigning `q` is the
  checked way;
- a write to an element of an array or a list, in every form: `a[i] =
  v`, `a[i] += v`, `a[i]++`, and targets that mix fields and indexes;
- a slice of an array, a view or a list, which is a view and cannot be
  bound with `ok`: `a[1:] is a view, []int64, which cannot be bound with
  ok; prove its bounds instead`;
- assigning `m[k]`, for which `put(mut m, k, v)` is the unproven way;
- `swap(mut l, i, j)`;
- `x++`, `x--`, `x += v`, `x -= v` and `x *= v`, which are statements,
  so `v, ok := x++` does not parse; `w, ok := x + v`, then `x = w` when
  `ok`, is the checked way, and the wrapping compound assignment, `x +%=
  v`, the wrapping one.

Some operations have only the checked form. An index of a view a call
returns is never proven, since no fact is about a call, so it is read
only with `x, ok := t.bytes()[i]`; its slice is proven only when its
bounds are 0 and the view's own length, as in `[:]`, `[0:]`, `[:0]` and
`[0:0]`. A type assertion has no plain form either: `a type
assertion needs two results; write v, ok := a.(int64)`. A conversion of a
float to an integer has no plain form either, since a float carries no
facts: `converting float64 to int64 can fail; use v, ok := int64(...)`.
A float division cannot fail, so it has no checked form, and nor does
float arithmetic, `adding floats cannot fail, so it has no checked form;
use a + b`, nor a wrapping operator, `a +% b wraps and cannot fail, so it
has no checked form; use a +% b, or v, ok := a + b`.

## Per operation

### Division and remainder

`a / b`, `a % b`, `x /= b` and `x %= b` need `b != 0`. A divisor is
proven by known values, by its own facts, and, for arithmetic, by the
bounds of its operands, never by a relation.
Unproven, the error names the divisor: `divisor b may be zero; check b
!= 0 first, or use q, ok := ... / b`, or `divisor may be zero; use q, ok
:= ... / ...` for a divisor that is not a subject.

A signed `a / b` and `x /= b` need, besides, that the division is not
the smallest value of its type divided by -1: a known value, the bounds,
or a fact of either operand rules out `b` being -1, as `b != -1` or `b >
0` does, or `a` being that value, as `a > -128` does for an `int8`. `b
!= -1`, or `b !=` the smallest value of a signed width, such as `-128`,
only rules that value out, counts for this proof alone, and no note
describes it. Once
the divisor is proven not zero, an unproven one is `a / b may overflow
int64; check b != -1 first, or use q, ok := a / b`, with a check on the
dividend, `a > -9223372036854775808`, where the divisor is not a subject
or is known, and none where neither is; for `x /= b` it is `x /= b may
overflow int64; check b != -1 first, or use q, ok := x / b and x = q
when ok`. An unsigned division, and any remainder, never overflows.

```tack error
package main

func ratio(a int64, b int64) int64 {
    if b != 0 {
        return a / b
    }
    return 0
}

func main() {
    code, fits := int32(ratio(8, 2))
    if fits { exit(code) }
}
```

```diagnostics
main.tck:5:16: error: a / b may overflow int64; check b != -1 first, or use q, ok := a / b
```

```tack error
package main

func half(a int64, b int64) int64 {
    return a / b
}

func main() {
    code, fits := int32(half(8, 2))
    if fits { exit(code) }
}
```

```diagnostics
main.tck:4:16: error: divisor b may be zero; check b != 0 first, or use q, ok := ... / b
```

### Indexing

`a[i]` into an array needs `0 <= i && i < len(a)`, against the array's
known length; `s[i]`, `v[i]` and `l[i]` need it against `len(s)`, `len(v)`
or `len(l)`, from a known length or a fact about it. The index is an
integer of any type. A read of an array's, a string's, a view's or a
list's element has the checked form; a write has none. A string's byte is
never written, and a view's element never is: both are errors in any
form. The index of a list is read in place, after the index is
evaluated (see [Ownership and memory](08-ownership.md#exclusive-access)).

### Slicing

`x[i:j]` needs `0 <= i`, `i <= j` and `j <= len(x)`, each proven as an
index's bound is, with the relations `i <= j` and `j <= len(x)`. A bound
may also be the length of another string, view or list, bounded as an
operand of arithmetic is (see [Arithmetic](#arithmetic)), so
`t[:len(s)]` is proven under `len(s) <= len(t)`, as `t[:len(s) + 0]`
is. Unproven,
the error names only what is missing: `slice bounds i:j may be out of
range; check i <= j first, or use v, ok := ...[i:j]`. Only a string's
slice has the checked form, which gives `""` and `false` when the bounds
are out of range.

### Arena lookups

An arena's element, `a[e]`, and a span's view of one, `a[s]`, cannot
fail, so they need no proof and have no checked form, `the checked form
cannot read an element of an arena: every id names one, the sentinel for
an id the arena did not give; use a[e]`. An arena only grows, so an id
it gave stays below its length for as long as it lives; an id that is
not, at or past `len(a)`, the zero id, or one of another arena past this
one's end, names the sentinel, at id 0, which the arena holds from
`make` on, before its first `add` too. A span not wholly in `a` is an
empty view. Both are logic errors the program cannot detect, and
neither reads or writes memory the arena does not own: every lookup is
clamped, in 64 bits, to an element the arena holds. An id has no
conversion from an integer, so nothing forges one. A fact about an
element, `a[e] != 0`, holds as one about an array's element does, only
while neither `a` nor `e` can change.

```tack run exit=0
package main

func main() {
    mut small := make(arena[int64], -1, c_allocator)
    mut big := make(arena[int64], 0, c_allocator)
    mut far id[int64]
    for i := range 10 {
        e, err := add(mut big, i)
        if err.code != 0 {
            exit(1)
        }
        far = e
    }
    small[far] = 7
    mut z id[int64]
    if small[far] != 7 || small[z] != 7 || len(small) != 1 {
        exit(2)
    }
    exit(0)
}
```

### Map keys

`m[k]` reads or writes the value under `k` only where `has(m, k)` proves
`k` is in `m`: `key k may not be in m; check has(m, k) first, or use v,
ok := m[k]`, and for an assignment `... or use put(mut m, k, ...)`. The
checked form reads a copy, so it cannot read an owned value or a value of
a type with no zero value.

### Taking an element out of a list

`x := pop(mut l)` needs `len(l) > 0`, `pop may find l empty; check len(l)
> 0 first, or use x, ok := pop(mut l)`, and `x := remove(mut l, i)` needs
`i` in range for `l`. The call passes `l` as `mut`, which ends the facts
about `len(l)` for what runs after it, but not for the call itself.
Written as a statement, `pop(mut l)` and `remove(mut l, i)` need no proof:
they take the element when there is one and do nothing otherwise.
`swap(mut l, i, j)` needs both indexes proven, and a fact about `len(l)`
holds across it.

### Conversions

`T(x)` of an integer `x` to an integer type `T` compiles when `x` is a
constant or a known value that is a value of `T`, when `x`'s type widens
to `T`, and when the facts in force keep `x` within `T`'s range. Only the
side `x`'s type leaves open needs a fact: a `uint64` `u` needs only `u <=
9223372036854775807` for `int64(u)`. The facts are the ones an index's
proof uses, from guards, early exits, loops, cases, contracts and
relations, and the same writes end them. For a target-width `T`, a value
of `T` is one from 0 to 4294967295; a known value past that, within 64
bits, fits on some targets only, so its conversion needs the checked
form, which tests it against the target's real range at run time.

```tack run exit=200
package main

func low(x int64) int64 {
    if 0 <= x && x < 256 {
        return int64(uint8(x))
    }
    return -1
}

func main() {
    if low(200) == 200 && low(256) == -1 {
        exit(200)
    }
    exit(1)
}
```

Unproven, the error says which check would prove it:

```tack error
package main

func low(x int64) uint8 {
    return uint8(x)
}

func main() {
}
```

```diagnostics
main.tck:4:12: error: converting int64 to uint8 can fail; check 0 <= x && x <= 255 first, or use v, ok := uint8(...)
```

### Errors

The values bound beside an error are read only where the facts in force
prove the error's code is 0; that rule, and the rule that an error is
never discarded, are in [Errors](09-errors.md).

## Unreachable places

An `unreachable` statement ([Statements](07-statements.md#unreachable-statements))
must be proven unreachable, or the program is an error, `unreachable may
be reached; check a condition that rules this place out first, or handle
the case here`. It is proven when a statement before it in a block
around it leaves the block or terminates; when the facts in force there,
with each variable's type bounds, leave some variable no value, its
bounds crossing or every value between them ruled out by `!=`, a false
`==` or the cases of a `switch`, those facts including what the early
exits of an `else if` chain and the cases of a `switch` give the code
after it: each link or case that leaves, while every one tried before it
leaves too, its condition or values being false, a case ending in
`fallthrough` leaving when the case it falls into does, and the first
that does not leave, when every one after it leaves and a `default` or
an `else` is among them, its condition or values being true; when
every side of a true `||` or a false `&&` on the path to it, each taken
alone, leaves some
variable no value with the facts there, those arriving after the
condition included; or when the relations in force contradict. The
values ruled out and the sides of such a condition count only for this
proof. Each fact counts only where it holds:
a write that can reach the place ends it.

```tack run exit=0
package main

func band(c int64) int64 {
    if c < 3 {
        return 0
    }
    if c > 5 {
        return 2
    }
    if 3 <= c && c <= 5 {
        return 1
    }
    unreachable
}

func main() {
    if band(4) != 1 {
        exit(1)
    }
    exit(0)
}
```

## Stack depth

No rule bounds the depth of the stack. Recursion is accepted, direct,
mutual, through function values, whose callee is known only when the
call runs, or through scope parameters and the closures passed to them; so are large local arrays and large values passed or copied by
value. Each can run out of stack, which guarantee 4 forbids, and each is
a known violation, parked (see [Known violations](#known-violations)).

The compiler bounds only how deeply source nests: at most 1000 levels,
counted together across expressions, statements and types, past which a
program is `expression nested too deeply: more than 1000 levels`. That
limit keeps the compiler and the C it writes within what they can
handle; it does not bound the depth of the program's stack at run time.

## Known violations

These are open bugs against the guarantees. Each is fixed before new
features unless it is parked. The list here is the one in
[guarantees.md](../guarantees.md#known-violations):

- Panic (4), parked. Unbounded recursion is accepted and can run out of
  stack. The fix is not decided yet. A call through a function value
  calls whichever function the value names when it runs, so recursion
  through values, a function passed itself or one that calls it, cannot
  be found from the source by following names; the fix has to bound it
  too.
- Panic (4), parked. A large local array, or a large value passed or
  copied by value, can overflow the stack with no recursion at all: a
  function with a 16 MB local array crashes. The same compile-time bound
  on call chains is the fix.
- Panic (4), parked. Comparing or freeing a value whose type nests very
  deeply uses one frame per level of the type. Each frame is a few
  words, so 1000 levels run, but the depth is bounded only once the
  compile-time stack bound above exists.
