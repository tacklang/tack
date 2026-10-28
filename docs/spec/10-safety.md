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
rule that an `ok` may not be dropped or left unread, and the operations
that have no checked form, what the prover does not accept, and the
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
   an array, a list, a view or a string holds.
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
  the divisor `b` is zero;
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
- a type assertion, `a.(T)`, when the `any` `a` does not hold a `T`.

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
`p.inner.d`, `p.a[1]` or `p.a[k]` with `k` immutable. An operand that is
not a subject, such as `b + 1`, `i + 1`, `-i`, a call's result, or a
field of a mutable variable or a `mut` parameter, is proven only when it
is a known value.

```tack run exit=4
package main

func half(a int64, b int64) int64 {
    if b == 0 {
        return 0
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
- the right side of `&&`, by the left being true, and of `||`, by the
  left being false;
- a `switch` case, as in the chain of ifs it stands for: a case proves in
  its body what `tag == v1 || tag == v2 ...` proves when true, and every
  later case and the default see the earlier values not matching; a type
  switch proves nothing;
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
from that bound cannot wrap past the end of the variable's type, and
neither its body nor its condition assigns the variable.

A fact about the length of a string, a view or a list proves that length
as a divisor too: where `len(l) > 0` holds, `10 / len(l)` needs nothing
more.

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

Arithmetic wraps, so a relation through `x + c` or `x - c` holds only
where the other facts in force keep it from wrapping in `x`'s type: `0 <=
i && i + 1 < len(a)` relates nothing, since `i + 1` wraps for the largest
`int64`, while `i < len(a) && i + 1 < len(a)` relates `i + 1` to
`len(a)`. `len(x) - c` never wraps. A product is no relation, `||` keeps
none, and a query follows at most 64 related values from the two it is
about.

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
  the `default`, which runs after every value is tried.

A write that runs only after the use does not end the fact. A call
assigns its `mut` arguments only once they are all evaluated, and an
assignment stores to its target only once its value is computed, so the
call or the assignment a fact is for may write the subject: `if len(l) >
0 { x := pop(mut l) }` and `if d != 0 { d = n / d }` compile. A proof
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
        x--
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
before it, since only the paths that did not take it run there. So a
loop's condition still proves `s[i]` below a branch that moves `i` on
and starts the next pass, where the condition proves it again:

```tack run exit=3
package main

func count(s string) int64 {
    mut n int64 = 0
    mut i int64 = 0
    for 0 <= i && i < len(s) {
        if s[i] == ' ' {
            i++
            continue
        }
        if s[i] == 'a' {
            n++
        }
        i++
    }
    return n
}

func main() {
    if count("a ba na") == 3 {
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
  in the loop's body ends it, in a branch that leaves too.

```tack error
package main

func sum(a [4]int64, k int64) int64 {
    mut s int64 = 0
    for mut i := 0; i < len(a); i++ {
        if k == 1 {
            i = 5
            continue
        }
        s += a[i]
    }
    return s
}

func main() {
}
```

```diagnostics
main.tck:10:16: error: index i may be out of range; check 0 <= i && i < 4 first, or use v, ok := ...[i]
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
- a guard on an operand that is not a subject, such as `a[i + 1]` after
  `if 0 <= i && i < 2`;
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
- a counting loop whose step could wrap, for what its start would prove;
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
says only that the divisor was not zero. Both operands of a checked
division are always evaluated.

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
  `q, ok := a / b + 1` is `the checked form must be the whole value: q,
  ok := a / b, then compute q + 1`.
- Only the outermost operation is checked: in `v, ok := m[i][j]`, the
  inner `m[i]` needs its own proof.
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

The `ok` is the check itself, and it may not be dropped. `_` may take the
value, `_, ok := m[k]`, but never the `ok`: `v, _ := m[1]` is `the ok
m[1] gives cannot be dropped; bind it to a variable instead of _`, and
so for every checked form, each named as written. Both names `_` is
`every name is _, so nothing is bound`.

An `ok` bound to a variable that nothing ever reads is dropped as surely,
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
- `swap(mut l, i, j)`.

Some operations have only the checked form. An index of a view a call
returns is never proven, since no fact is about a call, so it is read
only with `x, ok := t.bytes()[i]`; its slice is proven only when its
bounds are 0 and the view's own length, as in `[:]`, `[0:]`, `[:0]` and
`[0:0]`. A type assertion has no plain form either: `a type
assertion needs two results; write v, ok := a.(int64)`. A conversion of a
float to an integer has no plain form either, since a float carries no
facts: `converting float64 to int64 can fail; use v, ok := int64(...)`.
A float division cannot fail, so it has no checked form.

## Per operation

### Division and remainder

`a / b`, `a % b`, `x /= b` and `x %= b` need `b != 0`. A divisor is
proven by known values and by its own facts, never by a relation.
Unproven, the error names the divisor: `divisor b may be zero; check b
!= 0 first, or use q, ok := ... / b`, or `divisor may be zero; use q, ok
:= ... / ...` for a divisor that is not a subject.

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
index's bound is, with the relations `i <= j` and `j <= len(x)`. Unproven,
the error names only what is missing: `slice bounds i:j may be out of
range; check i <= j first, or use v, ok := ...[i:j]`. Only a string's
slice has the checked form, which gives `""` and `false` when the bounds
are out of range.

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
never dropped, are in [Errors](09-errors.md).

## Stack depth

No rule bounds the depth of the stack. Recursion is accepted, direct,
mutual, or through function values, whose callee is known only when the
call runs; so are large local arrays and large values passed or copied by
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
