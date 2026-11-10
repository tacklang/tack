# 7. Statements

This chapter describes every statement, its syntax and how it runs:
blocks, expression statements, assignments and compound assignments,
`++` and `--`, binding several results, `if` and `else`, `switch`, type
switches on `any`, switches on a union's cases, the loop forms (`for
cond`, three-clause loops, `range` over integers, arrays, strings,
views, lists and maps), `break` and `continue`, `return`, `defer` and
its block scope, and terminating statements: which statements
terminate, a call to `exit` among them, and that a function with
results must end in one. It also names which statements cannot stand
alone. Declarations inside a body belong in [Declarations and
scope](05-declarations.md); what an assignment or a `defer` frees or
moves belongs in [Ownership and memory](08-ownership.md); which facts a
condition gives the code it guards belongs in [Safety and
proofs](10-safety.md).

## Blocks

A block is a sequence of statements in braces, `{ ... }`. Statements in
a block are separated by semicolons, which the lexer inserts at the end
of a line ([Source and lexical elements](02-lexical.md)), so each
statement usually stands on a line of its own. Statements run in order,
each after the one before it ends.

A function's body is a block, and so is the body of each `if` and
`else` branch, of each case of a `switch`, of each `for` loop and of each
`defer`. Each opens a scope for the names declared in it ([Declarations
and scope](05-declarations.md)). A block cannot stand alone as a
statement: `{` where a statement is expected is `expected 'statement'
found '{'`.

A body holds these statements, each described below: an expression
statement, a local variable declaration ([Declarations and
scope](05-declarations.md)), a declaration or an assignment of several
results, an assignment, a compound assignment, `x++` and `x--`, `if`,
`switch`, `for`, `break`, `continue`, `return` and `defer`.

A statement leaves its block when control does not pass from it to the
statement after it: `return`, `break`, `continue`, a call to `exit`, and
a statement whose every path leaves, as [Safety and
proofs](10-safety.md) defines for the facts that follow. Statements
written after one that leaves never run. Whether such statements are
allowed is not yet specified; today the compiler accepts them without a
diagnostic.

## Expression statements

A call may stand alone as a statement: a call of a function, a function
value, a method or a builtin, `print("hi\n")`, `mut b.write(s)`. The
call is made and its results, if any, are dropped. A call whose results
cannot be dropped is an error: one whose last result is an error, which
[Errors](09-errors.md) describes, and one whose result holds a list or a
map, which nothing would free, `Value.items returns Value, which holds
a list and cannot be dropped; bind it to a variable`, `make` among them.
A call returning a view, as a statement, makes the call and drops the
view.

A call that only gives a value, a conversion, `len`, or a union's
construction with a payload, is warned of, `int8(5) is not used`,
`len(s) is not used` or `Expr.int(5) is not used`, even when an
argument is a call, as in `int8(f())`. It still runs: it is evaluated,
the calls in its arguments with it, and its value dropped. One with an
error of its own, such as a conversion that can fail, is reported for
that error alone. Today the compiler warns of these statements;
docs/open-decisions.md records them as rejected, and the two are not
yet reconciled.

No other expression can stand alone as a statement: an operator's
result, a name, a literal, a selector, an index, a slice, a type
assertion, a union's case with no payload and a conversion to a view,
`[]uint8(s)`, are each `expected 'statement'`.

```tack warn
package main

func main() {
    s := "tack"
    len(s)
    exit(0)
}
```

```diagnostics
main.tck:5:5: warning: len(s) is not used
```

## Assignments

An assignment `x = v` stores the value `v` in the place `x`. A place is:

- a mutable local variable, or a `mut` parameter;
- a field of a place, `p.x`, or an element of an array or a list that
  is a place, `a[i]`, under the proofs an index needs ([Safety and
  proofs](10-safety.md)), nested to any depth, `l.a.x` and `p.cells[i]`;
- a map's value, `m[k]`, where `has(m, k)` is proven ([Safety and
  proofs](10-safety.md));
- a mutable package-level variable, or a field or an element of one,
  qualified when it is another package's.

An immutable variable, `cannot assign to immutable variable x`, a
parameter that is not `mut`, `cannot assign to parameter p`, an
immutable global, a view or a view's element, a byte of a string, since
strings are immutable, a slice, `cannot assign to s[0:1]: a slice is
not a place`, and any value that is not a place are not assigned. A
field or an element follows its variable: there is no `mut` per field,
and `p.x = v` needs `p` to be mutable. Writing a field or an element
does not count as reading the variable ([Declarations and
scope](05-declarations.md)).

`v` must be assignable to the type of `x` ([Types](04-types.md)). The
indexes in `x` are evaluated before `v`, and `v` is computed before
anything is stored. What the assignment does to the value it replaces,
and to a moved value, is described in [Ownership and
memory](08-ownership.md).

An assignment has one target, or several targets assigned from one call
([Several results](#several-results)). `a, b = b, a` is not a statement
of Tack.

```tack run exit=9
package main

type Point struct {
    x int8
    y int8
}

func main() {
    mut p Point
    mut a [3]int8
    p.x = 4
    a[2] = 5
    p.y = a[2]
    exit(int32(p.x) + int32(p.y))
}
```

## Compound assignments

A compound assignment `x op= v` means `x = x op v`, with `x` read
before `v` is evaluated:

| Operand       | Operators                                     |
|---------------|-----------------------------------------------|
| an integer    | `+=` `-=` `*=` `/=` `%=` `&=` `\|=` `^=` `&^=` `<<=` `>>=` |
| a float       | `+=` `-=` `*=` `/=`                           |

`x` is a place, as for an assignment, and `v` a value its operator
accepts with `x` ([Constants and arithmetic](03-arithmetic.md)); for
`<<=` and `>>=`, `v` is a shift's count. `x /= v` and `x %= v` of
integers need the divisor `v` proven not zero, and a signed `x /= v`
needs `x / v` proven not the smallest value divided by -1 ([Safety and
proofs](10-safety.md)). Like a plain assignment, a compound assignment
does not count as reading `x`.

## Increment and decrement

`x++` adds one to `x`, and `x--` subtracts one, as `x += 1` and `x -=
1` do, for an integer place `x`. They are statements, never values, and
there is no prefix form. On any other type they are an error, `operator
++ is not defined on float64`. What they compute at the edges of the
type is described in [Constants and arithmetic](03-arithmetic.md).

```tack run exit=13
package main

func main() {
    mut n int32 = 3
    n += 4
    n *= 2
    n--
    n++
    n -= 1
    n |= 1
    exit(n)
}
```

## Several results

A call of a function with several results is bound by a declaration of
as many names, by position:

```text
q, r := divmod(7, 2)
q, mut r := divmod(7, 2)
_, r := divmod(7, 2)
```

Each name is a new variable of its result's type, immutable unless that
name has its own `mut`. None may already be declared in scope, `c is
already declared`. An assignment of several targets assigns the results
to existing places instead, each named once:

```text
q, r = divmod(7, 2)
```

The order in which the targets are stored is not yet specified.

The value is exactly one call, with exactly as many results as there
are names: `assignment mismatch: 2 variables but one returns 1 value`.
`_` discards the result in its position ([Declarations and
scope](05-declarations.md)), and a binding whose every name is `_` is an
error. A checked form, `v, ok := a[i]`, is bound with the same
statements ([Safety and proofs](10-safety.md)). A call with several
results cannot be used as one value, passed to another call or returned
whole as another function's results; it can be made as a statement,
dropping its results, unless its last result is an error
([Errors](09-errors.md)).

```tack run exit=6
package main

func divmod(a int32, b int32) (int32, int32) {
    if b == 0 {
        return 0, 0
    }
    if b == -1 {
        return -%a, 0
    }
    return a / b, a % b
}

func main() {
    q, mut r := divmod(17, 5)
    mut s int32 = 0
    r, s = divmod(r, 2)
    if q == 3 && r == 1 && s == 0 {
        exit(6)
    }
}
```

## If statements

```text
if cond { ... }
if cond { ... } else { ... }
if cond { ... } else if cond { ... } else { ... }
```

An `if` statement runs its first block when the condition, a `bool`,
is true, and otherwise its `else`, if it has one: a block, or another
`if` statement, so `else if` chains. A condition of any other type is
`condition must be a bool, got int64`. The braces are always written.
`else` follows the closing brace of the block before it on the same
line; at the start of a line it is `expected 'statement' found 'else'`.
An `if` has no init statement: `if x := f(); x > 0 {` is a syntax error.
In the condition, a struct literal is in parentheses ([Expressions](06-expressions.md)).

```tack run exit=2
package main

func sign(n int32) int8 {
    if n > 0 {
        return 1
    } else if n < 0 {
        return -1
    } else {
        return 0
    }
}

func main() {
    exit(int32(sign(5)) - int32(sign(-1)) + int32(sign(0)))
}
```

## Switch statements

```text
switch tag { case v1, v2: ... case v3: ... default: ... }
switch { case cond1, cond2: ... default: ... }
```

An expression switch compares its tag with each case's values using
`==`. The tag is evaluated once, before any case. The cases are tried
top to bottom, and a case's values left to right; the first value that
equals the tag selects its case, and the values after it are not
evaluated. Values need not be constants. A switch with no tag takes
each value as a `bool` condition, and a case is selected when any of
its conditions holds.

A case's value is compared with the tag under `==`'s rules: an untyped
constant takes the tag's type, which it must fit; an integer value of
any type compares with an integer tag by value, so `case int8(-1)`
never matches a `uint8` tag of 255; any other value has a common type
with the tag, one widening to the other; and strings, `bool`s, arrays
and structs compare as `==` compares them ([Types](04-types.md)). A tag
that is an untyped constant is an `int64`. An `any` tag is an error,
`operator == is not defined on any`; a switch on an `any` is a type
switch, below. A tag of a union type makes the switch a union switch,
below.

`default` is selected when no case is, and is tried last wherever it is
written; a switch has at most one. Only the selected case's body runs:
there is no fallthrough, and `fallthrough` is not a keyword. Each case's
body is a block of its own and may be empty, and so may the switch.
`break` and `continue` in a case act on the loop around the switch, not
on the switch; outside a loop they are the usual error. In a switch
with a tag, two cases with the same constant value, a known integer, a
`bool` constant expression or a string literal, are an error,
`duplicate case 2 in switch`, with a note at the first. A switch with
no tag is not checked for duplicates: two `case true:` are accepted,
though the README's description of switches states the rule without
that limit.

A switch has no init statement: `switch x := f(); x {` is `a switch has
no init statement; only a type switch binds a name, as in switch v :=
x.(type)`. In the tag, a struct literal is in parentheses. What each
case proves is described in [Safety and proofs](10-safety.md).

```tack run exit=6
package main

func kind(n int32) int8 {
    switch n {
    case 1, 2:
        return 10
    case 3:
        return 20
    default:
        return 0
    }
}

func main() {
    mut last int64 = -1
    for i := range 5 {
        switch {
        case i == 1:
            continue
        case i == 3:
            break
        }
        last = i
    }
    switch last {
    case 2:
        exit(int32(kind(3)) - int32(kind(1)) - 4)
    }
}
```

## Type switches

```text
switch v := a.(type) { case int64: ... case int8, int16: ... default: ... }
switch a.(type) { case string: ... }
```

A type switch selects the first case naming the type the `any` `a`
holds, exactly, as a type assertion matches ([Expressions](06-expressions.md)),
and `default` when none does. Its cases' values are types: a type an
`any` can hold ([Types](04-types.md)), including a view of a basic type,
`case []uint8:`. A type an `any` never holds is the assertion's error, a
type named twice in one switch, in one case or two, is `int64 appears
twice in this type switch`, and a value that is not a type is a syntax
error. `default` is optional, and the cases need not cover every type.

`v :=` binds a name, which is not an init statement. In a case naming
one type, `v` is a value of that type, `a`'s value; a case naming a view
type gives a `v` that is that view, under every rule of a view parameter
([Ownership and memory](08-ownership.md)). In a case naming several
types, and in the default, `v` is the `any` itself. `v` is immutable in
every case, `switch mut v := a.(type)` being `cannot use mut on a type
switch variable`, and each case's `v` is a variable of its own. `v` may
reuse no name visible at the switch. When no case reads `v`, it is
warned of once at the binding, `declared and not used: v`. `switch
a.(type)` binds nothing.

Otherwise a type switch is a switch: `a` is evaluated once, the cases
are tried top to bottom and the default last, there is no fallthrough,
and `break` and `continue` act on the loop around it. `.(type)` anywhere
but a switch's tag is `.(type) can only be the tag of a switch`. A type
switch gives no facts.

```tack run exit=5
package main

func size(a any) int32 {
    switch v := a.(type) {
    case int32:
        return v
    case string:
        return 2
    case int8, int16:
        return 1
    default:
        return 0
    }
}

func main() {
    var b int8 = 7
    var w int32 = 2
    if size(w) == 2 && size("x") == 2 && size(b) == 1 && size(true) == 0 {
        exit(5)
    }
}
```

## Union switches

```text
switch e { case int(n): ... case binary(b): ... case name, eof: ... default: ... }
```

A switch whose tag is a union runs the clause naming the case the union
holds. Each value of its cases names one of the union's cases: anything
else is `a case of a switch on Expr names one of its cases, as in case
int(v)`, a name the union lacks is `Expr has no case nope`, and a case
named twice is `int appears twice in this switch`, with a note at the
first.

A clause naming one case with a payload may bind the payload, `case
int(n)`: `n` is an immutable variable of the payload's type in that
clause's body, and refers to the payload in place rather than copying
it ([Ownership and memory](08-ownership.md)). It may reuse no name
visible at the switch, and is warned of when its clause never reads it.
It cannot be assigned, `cannot assign to immutable variable n`. What it
borrows from the switched place, and what its clause may then do to
that place, is described in [Ownership and memory](08-ownership.md). A
case named alone, `case
int:`, binds nothing; `case int(_)` is `every name is _, so nothing is
bound; write case int`; a case with no payload binds nothing, `eof has
no payload to bind; write case eof`; and a clause naming several cases
binds nothing, `a clause naming several cases binds nothing; write int,
or give int(n) a clause of its own`.

The clauses cover every case of the union, or the switch has a
`default`: `switch on Expr misses cases name, eof; cover them or add a
default`. Not both: `every case of Expr is already covered, so this
default never runs`. A switch on `union {}` needs no clause.

Otherwise a union switch is a switch: the tag is evaluated once, the
clauses are tried in order with the default last, there is no
fallthrough, and `break` and `continue` act on the loop around it. The
tag may be any value of a union type, a call's result included, but one
holding a list must be a place, `cannot use a temporary Value here;
store it in a variable first`. A union switch gives no facts. There is
no other test of a union's case. A switch's values are patterns only
when its tag is a union; in any other switch they are values.

```tack run exit=11
package main

type Shape union {
    circle int8
    square int8
    empty
}

func area(s Shape) int32 {
    switch s {
    case circle(r):
        return 3 * int32(r) * int32(r)
    case square(w):
        return int32(w) * int32(w)
    case empty:
        return 0
    }
}

func main() {
    if area(Shape.circle(1)) == 3 && area(Shape.square(2)) == 4 && area(Shape.empty) == 0 {
        exit(11)
    }
}
```

Without the clause for `empty`, and with no `default`, the switch is an
error:

```tack error
package main

type Shape union {
    circle int8
    square int8
    empty
}

func area(s Shape) int32 {
    switch s {
    case circle(r):
        return 3 * int32(r) * int32(r)
    case square(w):
        return int32(w) * int32(w)
    }
    return 0
}

func main() {
    exit(area(Shape.empty))
}
```

```diagnostics
main.tck:10:5: error: switch on Shape misses case empty; cover it or add a default
```

## For statements

```text
for cond { ... }
for { ... }
for init; cond; post { ... }
```

`for cond { ... }` runs its body while `cond`, a `bool`, is true,
testing it before each iteration. `for { ... }` runs its body until
something in it leaves the loop.

A three-clause loop, `for init; cond; post { ... }`, runs `init` once,
then runs the body while `cond` is true, testing it before each
iteration, and runs `post` after each iteration, one ended by `continue`
included. `init` is a simple statement: a local variable declaration, a
declaration of several results, an assignment, a compound assignment,
`x++` or `x--`, or a call. `post` is any of those but a declaration.
Every part may be left out, `for ;; { ... }`; without `cond` the loop
runs until something in its body leaves it. A variable `init` declares
belongs to the loop, and is mutable only when declared with `mut`: `for
mut i := 0; i < n; i++ { ... }`. What a counting loop proves about its
variable in its body is described in [Safety and proofs](10-safety.md).

A range clause is a loop's header too, below. A struct literal in a
loop's header is in parentheses.

A running total is written with `+%=` here and below, since nothing
bounds how far a loop adds to it; `+=` would need a proof that it fits
(see [Integer overflow](03-arithmetic.md#integer-overflow)).

```tack run exit=10
package main

func main() {
    mut total int32 = 0
    for mut i int32 = 1; i <= 4; i++ {
        total +%= i
    }
    mut n int32 = total
    for n > 5 {
        n -= 1
    }
    for {
        if n == 5 {
            break
        }
    }
    exit(total +% n -% 5)
}
```

## Range clauses

A range clause is the header of a `for` loop that runs once for each
value of a sequence:

```text
for i := range n { ... }
for range n { ... }
for i, x := range a { ... }
for i := range a { ... }
for _, x := range a { ... }
for range a { ... }
```

The ranged value is evaluated once, before the first iteration. The
names a range clause declares are fresh immutable variables in each
iteration: assigning them, `x = v`, is an error, and so is declaring
them with `mut`, `cannot use mut on a range loop variable`. Three names
are an error, `assignment mismatch: 3 variables but range gives at most
2 values`, and so is a binding whose every name is `_`.

| Ranged value      | First name             | Second name                |
|-------------------|------------------------|----------------------------|
| an integer `n`    | `0` to `n - 1`, of `n`'s type | none                |
| an array `[n]T`   | the index, an `int64`  | the element, a `T`         |
| a string          | the index, an `int64`  | the byte, a `uint8`        |
| a view `[]T`      | the index, an `int64`  | the element, a `T`         |
| a list `list[T]`  | the index, an `int64`  | the element, a `T`         |
| a map `map[K]V`   | the key, a `K`         | the value, a `V`           |

Over an integer, the loop runs once for each value from 0 to `n - 1`,
and not at all when `n` is zero or less; `n` is an integer of any type,
an untyped constant being an `int64`, and only one name is allowed.

Over an array or a string, the loop runs once for each element or byte,
in order. The loop walks a copy of the array taken when it begins, so
writing `a[j]` or assigning `a` in the body does not change what later
iterations see; change an element through `a[i]`. A view is walked in
place, since it never changes.

Over a list, the loop walks the list itself, not a copy, so writes to
its elements in the body show in later iterations. Over a map, the loop
walks its keys in the order each was first put, a key put again after
a delete coming last. What the body may not do to a list or a map it
ranges over, and how an element of an owned type is bound, in place
rather than copied, are described in [Ownership and
memory](08-ownership.md).

A slice is not ranged over. What a range loop proves about its index is
described in [Safety and proofs](10-safety.md).

```tack run exit=18
package main

func main() {
    mut total int32 = 0
    for i := range 3 {
        total +%= int32(i)
    }
    a := [3]int32{1, 2, 3}
    for _, x := range a {
        total +%= x
    }
    for i, c := range "ab" {
        if c == 'b' {
            total +%= int32(i)
        }
    }
    mut l := make(list[int32], c_allocator)
    err := append(mut l, 4, 4)
    if err.code != 0 {
        exit(1)
    }
    for _, x := range l {
        total +%= x
    }
    exit(total)
}
```

## Break and continue

`break` ends the innermost `for` loop around it, and execution continues
after that loop. `continue` ends the current iteration of the innermost
`for` loop around it: a three-clause loop then runs its `post`, and every
loop goes on to its next iteration, testing its condition first. Neither
takes a label; there are no labels. In a case of a `switch`, of either
kind, both act on the loop around the switch. Outside a loop, each is an
error, `break is not in a loop`, `continue is not in a loop`. A `break`
or `continue` in deferred code may act only on a loop inside that code
([Defer statements](#defer-statements)).

## Return statements

`return` ends the function and gives its results. In a function with no
result it is written alone; one with a value is `none declares no
result, so it cannot return a value`. In a function with results it
gives one value for each, by position, each assignable to its result's
type: `return a / b, a % b`. Too many or too few are errors, `too many
return values: one returns 1, got 2` and `not enough return values:
pair returns 2, got 1`. A call with several results is not returned
whole, `return two()`.

The values are evaluated left to right, then the function's pending
defers run ([Defer statements](#defer-statements)), then it returns. So
a defer that changes a variable named in `return x` does not change
what is returned. A `return` in deferred code is an error.

## Defer statements

```text
defer { ... }
```

A `defer` statement registers its block when execution reaches it, and
the block runs when the block holding the `defer` is left, by any path:
falling off its end, or a `return`, `break` or `continue` that leaves
it, through any number of nested blocks. A defer is scoped to its
block, not to its function, unlike Go's.

- A block runs the defers it reached in reverse order: the one reached
  last runs first. A jump out of several blocks runs the innermost
  block's defers first.
- A defer that was never reached never runs.
- A defer in a loop's body runs at the end of each iteration that
  reached it, one ended by `continue` included, before a three-clause
  loop's `post`. One in a case of a `switch` runs when that case's body
  ends. One at the top of a function's body runs when the function
  returns.
- `exit` ends the process without running any defer.

`defer` is always followed by a block, even for a single call: `defer
f(x)` is `defer must be followed by a block: defer { ... }`. The block
is evaluated when it runs, not when the defer is reached: it reads every
variable as it is then. It sees the names declared before the `defer`,
not those after it. The block may hold anything but a way out of it or
another defer: a `return` anywhere in deferred code is `cannot return
from a defer`, a `break` or `continue` aimed at a loop outside it is
`cannot break out of a defer` or `cannot continue out of a defer`, and a
`defer` at any depth in it is `cannot defer inside a defer`, of which
only the outermost is reported. A loop inside the block
breaks and continues as usual. Deferred code is checked with the facts
in force where the `defer` is written ([Safety and
proofs](10-safety.md)).

```tack run exit=0
package main

func main() {
    for i := range 3 {
        defer {
            print("end of pass\n")
        }
        if i == 1 {
            continue
        }
        print("body\n")
    }
    defer {
        print("first\n")
    }
    defer {
        print("second\n")
    }
    print("done\n")
}
```

```output
body
end of pass
end of pass
body
end of pass
done
second
first
```

## Terminating statements

A terminating statement is one of:

1. a `return` statement;
2. a call to the builtin `exit`, as a statement;
3. an `if` statement with an `else`, whose first block and `else` both
   end in a terminating statement, an `else if` counting as a block
   that ends in itself;
4. an expression switch or a type switch with a `default`, whose every
   case's body, the default's included, ends in a terminating statement;
5. a union switch that names every case of its union, or has a
   `default`, whose every clause's body ends in a terminating statement;
6. a `for` loop with no condition, `for { ... }` or a three-clause loop
   whose `cond` is left out, that no `break` leaves. A `break` inside a
   `switch` in the loop leaves it; one inside a nested loop does not.

A block ends in a terminating statement when its last statement is one.
A function that declares a result must end in a terminating statement,
or it is an error, `sign must end in a return`. The rule is syntactic: a
call to a function that never returns, one that calls `exit` included,
does not satisfy it, and neither does a `for` with a condition that is
always true. A function with no result may fall off the end of its body,
which returns.

```tack run exit=7
package main

func fail() int32 {
    exit(7)
}

func main() {
    exit(fail())
}
```

An `if` without an `else` does not terminate, whatever its branches do:

```tack error
package main

func sign(n int32) int8 {
    if n > 0 {
        return 1
    } else if n < 0 {
        return -1
    }
}

func main() {
    exit(sign(3))
}
```

```diagnostics
main.tck:3:6: error: sign must end in a return
```
