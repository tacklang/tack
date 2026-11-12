# 8. Ownership and memory

This chapter describes the memory model: which values own memory, how a
value is copied or moved, when a moved-from variable may no longer be
used, `own` parameters and arguments, `drop`, `mut` parameters and receivers and
the rule that a `mut` argument is the only way to reach its place during
the call, views and the borrows they make (view parameters and view
results, and the conversion of a string to a view of its bytes,
`[]uint8(s)`), lists and maps as owners, allocators and what a list or
map holds of one, regions and the escape rule that keeps what is made
through one inside its block, when memory is freed (end of scope, reassignment,
`defer`, early return), and what `exit` frees before the process ends.
It is the chapter that shows guarantees 2 and 3 (no leak, no
use-after-free) hold. The types themselves belong in
[Types](04-types.md); the operations on lists and maps, and `exit`'s
signature, belong in [Builtins](13-builtins.md); how a program ends
belongs in [Packages, modules and programs](12-packages.md); bounds
checks belong in [Safety and proofs](10-safety.md); how memory crosses
to C belongs in [Extern C](14-extern.md).

## Values, copies and owners

Every value is either copyable or owned.

A value is *owned* when its type holds a list, a map, an arena or a
pool: a list, a map, an arena, a pool, a struct with a field that holds
one, directly or through another struct
or a union, a union with a case whose payload holds one, and, inside a
generic function, a value of a type parameter, whatever type a call
gives it. Every other value is *copyable*: the integers, the floats,
`bool`, `string`, `error`, `Allocator`, function values, an arena's ids
and spans, a pool's handles, and arrays and structs of copyable values.

A copyable value is copied wherever it is declared, assigned, passed,
returned or stored, and the copy is independent of the original. A
string is copyable and its bytes are static: a string, and a slice of
one, which shares the bytes it is cut from, never allocates and is never
freed.

An owned value has exactly one owner at a time, a variable, a parameter
that took it with `own`, or the list, map, arena, pool, struct or union that
holds it.
The owner frees it, and nothing else does. An owned value is never
copied; where a copyable value would be copied, it is moved (see
[Moves](#moves)).

An owned type has no zero value, so a variable of one must be declared
with a value: `mut l list[int64]` is `l needs a value: list[int64] has no
zero value`. An array cannot hold an owned element, since its elements
are copied: `an array cannot hold list[int64]: its elements are copied,
and a list cannot be`; the same holds for a variadic parameter. No
package-level variable may hold a list, a map, an arena or a pool,
`package-level variable g cannot hold a list`, `package-level variable g
cannot hold a map`, `package-level variable g cannot hold an arena`,
`package-level variable g cannot hold a pool`, so a function reaches one
only through its parameters and its own locals. No type may hold itself
through a list, a map, an arena or a pool: `invalid
recursive type Tree: a type cannot hold itself, even through a list`.

## Moves

Declaring a variable with an owned value, `m := l`, assigning one,
`m = l`, returning one, writing one as a struct literal's field or a
union case's payload, passing a variable as an `own` argument, and the
builtins that store a value, `append`, `put` and `replace`, move it. A
move gives the value a new owner and empties the variable it came from.

A variable that was moved from cannot be used until it is assigned a new
value: `l is used after it was moved`, with a note at the move. A
variable moved on some paths only is unusable after them, `l is used
after it may have been moved`. A move in a loop's body that the next
iteration would reach again is reported in the same way, with the note
`l is moved here, in an earlier iteration of the loop`, unless the body
assigns the variable a new value before the move.
Moves are followed in the order code runs: a `switch`'s `default` runs
after every case's values are tried, wherever it is written, so a case
value written after the default that moves a variable moves it before
the default's body, which cannot use it, nor can any case the default
falls into by `fallthrough`. An assignment evaluates the indexes in its
target before its value, and stores after it ([Statements](07-statements.md)),
so a move in an index comes before the value, and a move in the value
comes before the store: `l[0] = take(own l)` and `l[0] +%= take(own l)`
both use `l` after it was moved.

```tack error
package main

func main() {
    l := make(list[int64], c_allocator)
    m := l
    if len(l) == len(m) {
        exit(1)
    }
}
```

```diagnostics
main.tck:6:12: error: l is used after it was moved
main.tck:5:10: note: l is moved here
```

Only a local variable or an own parameter may be moved from. A plain parameter borrows its
caller's value and a `mut` parameter refers to its caller's place, so
neither is moved: `cannot move l: it is a parameter, which borrows its
caller's value`. A field is never moved on its own, `cannot move s.items
out of s: a field cannot be moved on its own`, nor an element of a list,
a view or a map, nor a union pattern's binding or a range loop's element
variable that refers into what it matches or ranges over (see
[Lists](#lists) and [Maps](#maps)). `replace(mut place, new)` takes a
value out of any place a `mut` argument can name, moving `new` into it in
the same step, so the place always holds a value: `mut frames :=
replace(mut s.frames, make(list[int64], c_allocator))`.

An owned value that no variable holds, a call's result or `make`'s, must
be given an owner where it is made: as the value of a declaration, an
assignment, a return, a binding of several results, or a struct
literal's field. Anywhere else it is `cannot use a temporary list[int64]
here; store it in a variable first`, and a call statement that discards one
is `make returns list[int64], which cannot be discarded; bind it to a
variable`, or `... bind it to a variable instead of _` where a binding
gives it `_`.

A defer runs when its block is left, so deferred code that reads a
variable moved later in its block is an error, `l is moved after this
defer, which reads it when its block ends`, and deferred code cannot move
a variable declared outside it, `a defer cannot move l, which is
declared outside it`.

## Own parameters and arguments

A parameter written `own p T` takes its argument's value from the
caller. The caller writes `own` before the argument, `f(own t)`, which
moves `t` into the call. The callee owns the value: it may read it,
borrow it to a plain or a view parameter, return it, store it, move it
on, or leave it, and it frees the value when the function ends, at every
exit, after the function's defers, unless it moved it on first. An own
parameter is immutable; `mut u := p` moves it into a mutable variable.

```tack run exit=7
package main

func total(own l list[int64]) int64 {
    mut sum int64 = 0
    for _, x := range l {
        s, ok := sum + x
        if !ok {
            return 0
        }
        sum = s
    }
    return sum
}

func main() {
    mut l := make(list[int64], c_allocator)
    err := append(mut l, 1, 2, 4)
    if err.code != 0 {
        exit(1)
    }
    if total(own l) == 7 {
        exit(7)
    }
    exit(2)
}
```

What follows `own` at a call is a local variable or an own parameter of
the caller. A plain or a `mut` parameter, a field, `cannot pass own s.l:
a field cannot be moved on its own`, an element, `cannot pass own ls[0]:
an element cannot be moved out of what holds it`, and a list being
ranged over cannot be passed as `own`. A value no variable holds is
passed with no `own`, and writing it is `cannot pass own g(): only a
variable can be passed as own; pass g() without own`. Leaving `own` out
for a variable is `sink takes l as own; pass own t`; writing it for a
parameter that is not own is `f does not take t as own; pass t`; and
`own x` anywhere but as a call's argument is `own x can only be passed to
an own parameter`.

An own parameter's type must own something: `int64 cannot be an own
parameter: it owns no list or map, so it is copied; remove own`. A type
parameter may be one; a call whose type argument owns nothing passes a
copy, with no `own`. A view, an `any`, a variadic parameter, a method's
receiver and an extern func's parameter cannot be own, and `mut own` is
an error. `own` is part of a function's type, `func(own list[int64])
int64`, and of a method's signature when a type satisfies an interface.

A variable passed as `own` leaves the caller as the call begins, so
nothing else in the call may name it: no other argument at any depth, no
call nested in one, no receiver and no function value being called.
`f(t, own t)` is `cannot use t alongside own t in this call: it is moved
into the call, and nothing else in the call may reach it`. Within one
argument, which is evaluated left to right, a later name of the variable
is a use after a move.

An `ensures` clause cannot name an own parameter (see [Safety and
proofs](10-safety.md#contracts)), and a view a function returns cannot
point into one, since it is freed as the function returns (see [View
results](#view-results)).

## Drop

`drop(own x)` frees the value of `x` where it is written, exactly as the
end of its block would, and leaves `x` moved from. It is a statement and
gives no value, `drop returns no value` anywhere else, and to every rule
of this chapter it is a move of `x` into a call that frees it: a later
use is `x is used after it was moved`, a second drop is the same, and a
drop is subject to every rule of a move and of an own argument, in
loops, defers, closures and ranges alike. A drop is no write, so the
facts about `x` need nothing more: `x` is read again only once it is
assigned a new value, and the assignment ends every fact about it,
`len(x)` included.

```tack run exit=0
package main

func main() {
    mut l := make(list[int64], c_allocator)
    err := append(mut l, 1, 2, 3)
    if err.code != 0 {
        exit(1)
    }
    drop(own l)
    l = make(list[int64], c_allocator)
    if len(l) != 0 {
        exit(2)
    }
}
```

What follows `own` is a local variable or an own parameter whose type
owns something. `own` is required: `drop(l)` is `drop takes x as own;
pass own l`. A plain or a `mut` parameter does not own its value and is
not moved, `cannot move l: it is a parameter, which borrows its caller's
value`. A view only borrows, `cannot drop v: a view only borrows what it
points into`, and a value whose type owns nothing would have nothing to
free: `cannot drop n: int64 owns no list or map, so there is nothing to
free; remove the drop`. A field or an element is never moved out of what
holds it, so neither is dropped, `cannot drop b.items: a field cannot be
moved on its own; take it out with replace`, `cannot drop ll[0]: an
element cannot be moved out of a list; take it out with pop or remove`,
and `pop`, `remove` and `delete` written as a statement, or `replace`,
free one instead. Anything but a variable is `cannot drop numbers():
only a variable can be dropped`.

```tack error
package main

func main() {
    l := make(list[int64], c_allocator)
    drop(own l)
    if len(l) == 0 {
        exit(1)
    }
}
```

```diagnostics
main.tck:6:12: error: l is used after it was moved
main.tck:5:14: note: l is moved here
```

Inside a generic function, `drop(own t)` of a value of a type parameter
is allowed, since its type argument may own something, and an instance
whose type argument owns nothing drops nothing.

## Mut parameters and receivers

A parameter written `mut p T` refers to the caller's place for the
duration of the call: reading and writing `p`, its fields and its
elements reads and writes the caller's place directly. Every other
parameter, an own parameter aside, is an immutable copy of its argument,
or, for an owned or a view type, a read-only borrow of it (see [Views
and borrows](#views-and-borrows)).

The caller writes `mut` before the argument, `scale(mut b, 3)`, and only
there. Leaving it out for a `mut` parameter is `scale takes p as mut;
pass mut b`, or, when `b` could not be passed as `mut` either, `scale
takes p as mut, but b is immutable; declare it with mut, then pass mut
b`, and writing it for a plain one is `count does not take t as
mut; pass t`. What follows `mut` must be a place the caller may change: a
mutable local, a `mut` parameter, a field of one, or an element of one
whose index is proven in range, nested in any way. An immutable variable,
a plain parameter, an own parameter, a global, a string's byte, a slice
and a value that is not a place are errors. The argument's type must be
the parameter's exactly, with no widening.

```tack run exit=9
package main

type Point struct {
    x int64
    y int64
}

func scale(mut p Point, k int64) {
    x, okX := p.x * k
    y, okY := p.y * k
    if okX && okY {
        p.x = x
        p.y = y
    }
}

func main() {
    mut b := Point{x: 1, y: 2}
    scale(mut b, 3)
    if b.x == 3 && b.y == 6 {
        exit(9)
    }
    exit(1)
}
```

A reference is never stored or returned: there is no syntax for one, and
`mut x` anywhere but as a call's argument is `mut b can only be passed to
a mut parameter`. Inside the callee, `q := p` copies the caller's value
and `return p` returns a copy, and passing `p` on as `mut` is the only way
the reference reaches further.

A method's `mut` receiver is a `mut` parameter, and a call to it, `mut
b.write(s)`, passes the receiver as a `mut` argument under every rule of
this section and the next.

A `mut` argument stands for its place, not a value read from it: the
callee sees the place as it is when the call begins, after every argument
has been evaluated, and the call assigns it once they all are.

## Exclusive access

During a call, a place passed as `mut` is reachable only through its
parameter. So no two `mut` arguments of one call may overlap. Places
compare as paths: different fields of one struct never overlap, and two
elements of one array overlap unless their indexes are known values that
differ or the facts in force keep them apart. The same place twice is `x
is passed as mut twice in this call; a mut argument must be the only way
to reach x during the call`, and a place with a part of it, `mut p` and
`mut p.x`, is `p.x is part of p, and both are passed as mut in this call;
...`, each with a note at the first.

```tack error
package main

func swap2(mut a int64, mut b int64) {
    t := a
    a = b
    b = t
}

func main() {
    mut x int64 = 1
    swap2(mut x, mut x)
    if x == 1 {
        exit(1)
    }
}
```

```diagnostics
main.tck:11:18: error: x is passed as mut twice in this call; a mut argument must be the only way to reach x during the call
main.tck:11:11: note: first passed as mut here
```

A plain argument of a copyable type that reads a place passed as `mut`
in the same call is a copy taken before the call begins, so it is
allowed. A list is never copied for a call, so an argument that reads one
in place, a list or a value holding one passed to a plain parameter, or a
list or a slice of one passed to a view parameter, cannot
overlap a place the same call, or a call nested in its arguments, passes
as `mut`: `cannot pass l alongside mut l in this call: a list is not
copied for a call, and a mut argument must be the only way to reach l
during it`. Two elements of one list may always be one, so `f(l[0], mut
l[1])` is an error whatever the indexes.

```tack error
package main

func grow(mut l list[int64], v []int64) error {
    return append(mut l, v...)
}

func main() {
    mut k := make(list[int64], c_allocator)
    err := grow(mut k, k)
    if err.code != 0 {
        exit(2)
    }
}
```

```diagnostics
main.tck:9:24: error: cannot pass k alongside mut k in this call: a list is not copied for a call, and a mut argument must be the only way to reach k during it
main.tck:9:17: note: passed as mut here
```

The same rule covers every other read of a list, a map or a returned view
in place:

- an index whose own expression passes, at any depth, a place that may
  overlap the list or map it reads as `mut`: `cannot index l alongside
  mut l in its index: a list is not copied to be indexed, ...`;
- a slice whose bounds do: `cannot slice l alongside mut l in its bounds:
  ...`;
- a value assigned to an element of a list, an arena or a pool, or to a
  value in a map, that passes the list, arena, pool or map, or anything
  holding it, as `mut`: `cannot assign to l[0] alongside mut l in its
  value: an element is assigned in place in its list, ...`, `... in its
  arena, ...` for an arena's, `... in its pool, ...` for a pool's; a
  `mut` argument inside the list, one of its elements, may be passed;
- an element of an arena, or a part of one, passed as `mut` while
  another operand passes the arena, or anything holding it, as `mut`, at
  any depth: `cannot pass mut a[e] alongside mut a in this call: an arena
  is not copied for a call, ...`, since the element's address is taken
  first and growing the arena may move it; a list's element so passed
  loses the proof of its index instead;
- a comparison of two lists, or of values holding them, where one operand
  passes the other as `mut`: `cannot compare a alongside mut a in the
  other operand: ...`;
- a view a call returns, which may point into every argument of its call
  that is not `mut` and holds data, a list, a map, an arena, a pool, an array, a
  struct, a union, a view or a type parameter's value, the receiver
  included: `cannot pass
  t.bytes() alongside mut t in this call: the view it returns may point
  into t, ...`.

A range over a list or a map walks it in place, so its body cannot
assign it or anything holding it, pass either as `mut`, or move it, a
defer in the body included: `l is being ranged over, so this loop cannot
pass mut l`. While a range binding refers to an owned element in place,
the body assigns no element of the list either: `l is being ranged over,
and x refers into it, so this loop cannot assign l[i]`. A switch on a
union that binds a payload refers into the switched place the same way,
so that clause cannot assign the place, pass it as `mut` or move it: `u
is being matched, and l refers into it, so this case cannot pass mut u`.

No global holds a list or a map and no reference is ever stored, so a
callee reaches a caller's list or map only through its arguments, and
these rules cover every way a buffer could be moved or freed while
something reads it.

## Closures

A closure passed to a call is, for the call's rules, its captures, each
an argument of the call and a borrow, read in place whatever its type: a
plain argument may be a copy, but a closure reaches the variable itself
each time it runs. So a capture cannot be passed as `mut` in the same
call, even one the closure only reads, `cannot capture x in a closure
alongside mut x in this call: a closure reaches its captures in place,
and a mut argument must be the only way to reach x during the call`, nor
written by another closure of the call. A closure that writes a capture,
assigning it or a part of it, or passing one as `mut`, counts as a `mut`
argument of the call for every rule one is under: no other argument may
read it in place, `cannot pass l alongside a closure that writes l in
this call: a list is not copied for a call, and a closure that writes l
must be the only way to reach l during it`, as no index or slice around
the call may. A capture of a field or an element is a capture of the
whole variable, and a nested closure's captures are its outer closure's
too. Writing a capture needs a place that may be written, as anywhere.

A closure cannot move a capture, `a closure cannot move l, which it
captures: the closure may run more than once`, and a capture moved
before the call, or in it, is a use after a move, or `cannot use l
alongside own l in this call: ...`. A closure frees none of its
captures; what it declares is freed at each of its returns, as a
function's locals are, its `own` parameters included.

## Views and borrows

A parameter written `v []T` is a read-only view of elements of type `T`.
An array place `[N]T` of exactly the element type, a list of exactly the
element type, a slice of either, and another view of the same type pass
to it as they are, with no copy. A string passes to a `[]uint8`
parameter through the conversion `[]uint8(s)`, a view of its bytes, which
takes any string and is allowed only as an argument to a `[]uint8`
parameter: `[]uint8(...) can only be passed to a []uint8 parameter`.

```tack run exit=5
package main

func count(v []uint8, c uint8) int64 {
    // n is at most len(v), so +%= never wraps.
    mut n int64 = 0
    for _, b := range v {
        if b == c {
            n +%= 1
        }
    }
    return n
}

func main() {
    a := [4]uint8{1, 2, 1, 1}
    if count(a, 1) == 3 && count([]uint8("banana"), 'a') == 3 {
        exit(5)
    }
    exit(1)
}
```

An array passed to a view must be a place: a call's result or a literal
is `cannot view a temporary array; store it in a variable first`, and a
list no variable holds `cannot view a temporary list; store it in a
variable first`.

A view is read-only: `mut v []T` is `a view cannot be a mut parameter;
views are read-only`, and assigning `v`, an element of it or a field of
one, or passing either as `mut`, is an error. A view is the type of a
parameter or of a result only: a local, a field, a global or an array's
element of a view type is `a view can only be the type of a parameter or
of a result`. Its element type may be owned, `[]list[uint8]`, whose
elements it reads in place, and never moves out: `cannot move v[0] out of
v: an element cannot be moved out of a view, which only borrows it`.

A view shows the callee the array as it was when the argument was
evaluated, for the whole call. When the same call passes an overlapping
place as `mut`, `f(mut a, a)`, or a call nested in its arguments does, or
the array is part of a mutable global, which the callee could change,
the view is of a copy taken then. A list is never copied for this; such a
call is an error instead (see [Exclusive access](#exclusive-access)).

A plain parameter of an owned type borrows its argument in the same way:
read-only, never copied, and never moved by the callee. A variadic
parameter is a view too.

## View results

A function or a method may return a view when it is its only result,
`a view can only be a function's only result` otherwise. It may return
only a view into what its caller passed it and it does not change: a view
parameter, a list or an array that is a parameter that is not `mut`, or a
field or an element of one, the receiver included; a slice of any of
these; a union pattern's binding or a range loop's element variable that
refers into one of these; or a call returning a view whose arguments are
all such places. An array is returned only when it is reached through an
element of a list, a pool or a view, since a plain array parameter is the
function's own copy. A range over an arena gives its sentinel first,
which the arena holds inline, so an array reached through a range
loop's element over an arena, or through a union pattern's binding
inside one, counts as reached through an element of a list or a view
only when the arena itself is: a plain arena parameter, a field of a
plain parameter holding one, and an element of another arena are the
function's own copy, `cannot return a view of x.arr: the function holds
its own copy of a, ...`. An arena's element, `a[e]`, is never one, since
an id may name the sentinel.

```tack run exit=6
package main

type Bag struct {
    items list[int64]
}

func (b Bag) all() []int64 {
    return b.items
}

func sum(v []int64) int64 {
    mut n int64 = 0
    for _, x := range v {
        s, ok := n + x
        if !ok {
            return 0
        }
        n = s
    }
    return n
}

func main() {
    mut items := make(list[int64], c_allocator)
    err := append(mut items, 1, 2, 3)
    if err.code != 0 {
        exit(1)
    }
    b := Bag{items: items}
    if sum(b.all()) == 6 {
        exit(6)
    }
    exit(2)
}
```

Anything else is an error that names what the view would point into: a
local, a global, a `mut` parameter or receiver, an own parameter, which
is freed as the function returns, a temporary, or a string's bytes.

```tack error
package main

func bad() []int64 {
    l := make(list[int64], c_allocator)
    return l
}

func main() {
}
```

```diagnostics
main.tck:5:12: error: cannot return a view of l: l is a local variable; a returned view can only point into what a parameter that is not mut holds
```

An extern func cannot return a view. The view a call returns is
second-class: it is used only inside the expression that consumes it,
passed to a view parameter, passed as `any` when its element is a basic
type, given to `len`, indexed with the checked form, sliced, ranged over,
or returned on from a function returning a view. Anywhere else, stored,
assigned, compared, converted or printed with `print`, it is `t.bytes()
is a view, []uint8, which can only be passed to a view parameter, given
to len, indexed, sliced, ranged over or returned`. As a call statement
the view is discarded, borrowing nothing past the statement. It borrows
every argument of its call that is not `mut` and holds data, under the
rules of [Exclusive access](#exclusive-access).

## Lists

A `list[T]` owns a buffer of elements, which it grows and frees through
the allocator it was made with, `make(list[T], alloc)`. `make` allocates
nothing. `append` and `reserve` grow the buffer all at once, before
writing anything, so when the allocator cannot grow it they return an
error and leave the list's length, capacity and elements as they were.

A list owns each element it holds, and may hold owned elements, lists,
maps, and structs and unions holding them, nested to any depth. An
element of an owned type is never copied out of its list: it is a place
inside the list, read, borrowed, passed as `mut`, grown and matched where
it is. It never leaves the list but through `pop` or `remove`: declaring
a variable with it, assigning it, returning it, or storing it is `cannot
move l[i] out of l: an element cannot be moved out of a list; take it out
with pop or remove`, and the checked form, which would copy it, is `the
checked form cannot read an element of type list[int64]: it would copy it
out of l; check 0 <= i && i < len(l) first and use l[i] in place`.
Spreading a list or a view of owned elements into `append` would copy
them, and is an error.

`l[i] = v` moves `v` in once it is computed, then frees the element it
replaces. `append` moves each value it is given into the list, and when
the buffer cannot grow it frees those values, so nothing is lost.
`pop(mut l)` and `remove(mut l, i)` move an element out, to its new
owner, and copy and allocate nothing. Written as a statement, each discards
and frees the element it takes when there is one. A checked `pop` or
`remove` that finds no element leaves the list as it was; when the
element type has no zero value, the element variable then holds nothing
to free and cannot be read until `ok` is checked true. An owned element
bound to `_` would never be freed: `pop returns list[int64], which cannot
be discarded; bind it to a variable instead of _`. `swap(mut l, i, j)`
exchanges two elements in place, moving, copying and freeing nothing.
`old := replace(mut l[i], new)` takes an element out with `new` left in
its place.

A range binding of an owned element, `for _, x := range l`, refers to the
element in place: it is read-only and is never moved, `cannot move x: it
refers to an element of l in place, and an element cannot be moved out
of a list`. A range binding of a copyable element is a copy.

Freeing a list frees each element it holds, last first, then its
buffer, through its allocator. Assigning a list frees every old element
and the old buffer.

## Maps

A `map[K]V` owns its buffers, its keys and its values, and frees them all
when it is freed. It moves as a list does, and nests in lists, structs,
unions and other maps.

`put(mut m, k, v)` moves `k` and `v` in. When `k` is already there, the
new value replaces the old one, which is freed, and the key given is
freed, the one the map holds being kept; when the allocator cannot grow
the map, `put` returns an error, leaves the map as it was, and frees `k`
and `v`. `v, ok := delete(mut m, k)` moves the value out, to its new
owner, and frees the key the map held; written as a statement,
`delete(mut m, k)` frees the value too.

A value is never moved out of a map but through `delete`: `cannot move
m[k] out of m: a value cannot be moved out of a map; take it out with
delete`. Where `has(m, k)` proves the key is there, `m[k]` is a place,
read and written in the map's entries; assigning it frees the value it
held. The checked form, `v, ok := m[k]`, copies, so it cannot read an
owned value: `the checked form cannot read a value of type Bag: it would
copy it out of m; check has(m, 1) first and use m[1] in place`. A range
over a map binds an owned key or value in place, and copies any other.

```tack run exit=4
package main

func main() {
    mut m := make(map[string]list[int64], c_allocator)
    err := put(mut m, "a", make(list[int64], c_allocator))
    if err.code != 0 {
        exit(1)
    }
    if has(m, "a") {
        e := append(mut m["a"], 4)
        if e.code != 0 {
            exit(2)
        }
        if len(m["a"]) == 1 {
            exit(4)
        }
    }
    exit(3)
}
```

## Arenas

An `arena[T]` owns a list of its elements and its sentinel, the element
at id 0, held in the arena itself, and frees them all when it is freed:
its elements last first, then its sentinel. `make(arena[T], sentinel,
alloc)` moves the sentinel in and allocates nothing. It moves as a list
does, and nests in lists, maps, structs, unions and other arenas.

`add(mut a, v)` and `add_all(mut a, ...)` move their values in, growing
the list all at once, before writing anything: when the allocator cannot
grow it, or the arena would hold more than 2^32 - 1 elements, its
sentinel among them, each returns an error, leaves the arena as it was
and frees the values it was given, so nothing is lost. Nothing takes an
element out of an arena while it lives: `cannot move a[e] out of a: an
element cannot be moved out of an arena`, and no element is dropped or
popped. `a[e] = v` moves `v` in, then frees the element it replaces,
and `old := replace(mut a[e], new)` takes one out with `new` left in its
place. A range binding of an owned element refers to it in place.

An id holds no memory, so an id outlives nothing: it stays valid for as
long as its arena lives, and names the sentinel in any other. What reads
an arena in place, an element, a range over it or a span's view of it,
`a[s]`, is a borrow of the arena, and `add`, which may move its list's
buffer, takes it as `mut`: the exclusive access rules stop both in one
call, `cannot pass a[s] alongside mut a in this call: an arena is not
copied for a call, and a mut argument must be the only way to reach a
during it`, and a range from growing what it walks, `a is being ranged
over, so this loop cannot pass mut a`. A closure that reads an arena it
captures borrows it as any capture is borrowed.

```tack run exit=0
package main

func numbers(n int64) list[int64] {
    mut l := make(list[int64], c_allocator)
    for i := range n {
        err := append(mut l, i)
        if err.code != 0 {
            return l
        }
    }
    return l
}

func main() {
    mut a := make(arena[list[int64]], numbers(0), c_allocator)
    e, err := add(mut a, numbers(2))
    if err.code != 0 {
        exit(1)
    }
    grown := append(mut a[e], 7)
    old := replace(mut a[e], numbers(5))
    if grown.code != 0 || len(old) != 3 || len(a[e]) != 5 {
        exit(2)
    }
    drop(own a)
    exit(0)
}
```

## Pools

A `pool[T]` owns a buffer of slots and the live elements in them, and
frees them all when it is freed: its live elements last first, then the
buffer. `make(pool[T], alloc)` allocates nothing. It moves as a list
does, and nests in lists, maps, structs, unions, arenas and other pools.

`insert(mut p, v)` moves `v` in, into a slot an earlier remove freed or a
new one: when there is none and the allocator cannot grow the buffer, or
the pool would hold more than 2^32 - 1 slots, it returns an error,
leaves the pool as it was and frees `v`, so nothing is lost. `v, ok :=
remove(mut p, h)` moves the element out, to its new owner, and written
as a statement `remove(mut p, h)` frees it at once; either way the slot
stays in the buffer, which a pool never gives back while it lives, so a
stale handle never reaches freed memory. Nothing else takes an element
out of a pool: `cannot move p[h] out of p: an element cannot be moved out
of a pool; take it out with remove`, and no element is dropped, `cannot
drop p[h]: ...`. Where `has(p, h)` proves `h` live, `p[h] = v` moves `v`
in, then frees the element it replaces, and `old := replace(mut p[h],
new)` takes one out with `new` left in its place. The checked form, `v,
ok := p[h]`, copies, so it cannot read an owned element. A range binding
of an owned element refers to it in place.

A handle holds no memory, so a handle outlives nothing: it names its
element while the element is in its pool, and nothing once it is
removed or the pool is gone. What reads a pool in place, an element or a
range over it, is a borrow of the pool, and `insert` and `remove`, which
may move its buffer or free an element, take it as `mut`: the exclusive
access rules stop both in one call, `cannot pass p[h] alongside mut p in
this call: a pool is not copied for a call, and a mut argument must be
the only way to reach p during it`, and a range from changing what it
walks, `p is being ranged over, so this loop cannot pass mut p`. A
closure that reads a pool it captures borrows it as any capture is
borrowed.

```tack run exit=0
package main

func numbers(n int64) list[int64] {
    mut l := make(list[int64], c_allocator)
    for i := range n {
        err := append(mut l, i)
        if err.code != 0 {
            return l
        }
    }
    return l
}

func main() {
    mut p := make(pool[list[int64]], c_allocator)
    h, err := insert(mut p, numbers(2))
    if err.code != 0 {
        exit(1)
    }
    if has(p, h) {
        grown := append(mut p[h], 7)
        old := replace(mut p[h], numbers(5))
        if grown.code != 0 || len(old) != 3 || len(p[h]) != 5 {
            exit(2)
        }
    }
    remove(mut p, h)
    v, ok := remove(mut p, h)
    if ok && len(v) >= 0 {
        exit(3)
    }
    if len(p) != 0 {
        exit(4)
    }
    drop(own p)
    exit(0)
}
```

## Allocators

An `Allocator` is an opaque handle on an allocator: a list, a map, an
arena or a pool holds the one it was made with, grows through it, and is freed through it. An
`Allocator` is copyable; copying it copies the handle, not the
allocator. The `Allocator` type's other properties, and `c_allocator`,
are described in [Types](04-types.md); a region's allocator, which is
second class, below.

## Regions

A region statement, `region r { ... }`, opens an allocator, `r`, that
lives for its block (see [Statements](07-statements.md)): what is made
through `r`, by `make` given `r` or by a function given `r` for a scope
`Allocator` parameter, takes its memory from chunks the region takes from
`c_allocator`, and the end of the block gives every chunk back in one
step, however the block is left. Each owned value made through `r` is
still freed by its owner, as any is, which costs almost nothing, so a
value from a region is freed exactly once, as any value is.

`r` is second class, as a scope parameter of a function type is: it can
only be given to `make` as its allocator, passed to a parameter written
`scope a Allocator`, or captured by a closure, which can only be passed
to a scope parameter. Anywhere else, copied into a local, assigned,
returned, put in a field, an array, a list, a map or a global, it is `r
is a region's allocator, which can only be given to make or passed to a
scope Allocator parameter`; passed to a plain `Allocator` parameter it is
`take does not take a as scope, and a region's allocator can only be
passed to a scope parameter`, and to an extern func's, `r is a region's
allocator, which never crosses to C, and take is an extern func`. A
scope `Allocator` parameter is under the same rule in its function, `a
is a scope Allocator parameter, which can only be given to make or
passed to another scope parameter`, so no callee keeps a region's
allocator past the call. Any `Allocator` may be passed to one,
`c_allocator` included.

A value is from `r` when its type owns memory and it may hold memory `r`
handed out: it was made inside `r`'s block by an expression given `r`,
a call with `r`, or a value from `r`, among its operands, a method
call's receiver and a closure's captures included, or it is a variable a
value from `r` was declared with, assigned to, moved, appended, put,
added or inserted into, or passed as `mut` to a call alongside. A value
from `r` may not reach anything that lives longer than `r`'s block:

- it is not returned, from the function or from a closure, whose results
  live outside every region, `cannot return a value of type list[int64]
  holding memory from region r: the region gives its memory back when
  its block ends`;
- it is not assigned to a variable declared outside the block, or to a
  field or an element of one, a parameter's included, `cannot assign a
  value of type list[int64] holding memory from region r to outer: outer
  is declared outside the block of region r`;
- it is not appended, put, added, inserted or given to `replace` into a
  place declared outside the block, `cannot append a value of type
  list[int64] holding memory from region r to outer: outer is declared
  outside the block of region r`;
- no call takes it, or `r`, together with a `mut` argument whose type
  owns memory declared outside the block, or a closure that writes such a
  variable, since the callee could store one into the other, `cannot pass
  mut outer to fill alongside r, the allocator of region r: the call
  could keep memory from region r in outer, and outer is declared outside
  the block of region r`;
- no call through a scope parameter of a function type takes it, or `r`,
  in any position, nor does a call that passes such a parameter on, by
  name or captured in a closure: the closure behind it is the caller's
  and may write anything it captures, `cannot pass a value of type
  list[int64] holding memory from region r to g, a scope parameter: the
  closure behind it could keep memory from region r in anything it
  captures`. Named functions, methods and plain function values capture
  nothing, and keep the rule above.

Each of these has a note at the region, `region r is opened here, and
everything made through r is given back when its block ends`. Values
that own nothing are never from a region, so an integer, a bool, a
string, an id, a span or a handle computed from one leaves freely, and a
list made through `c_allocator` inside a region is not from it. Regions
nest: inside a region `s` in `r`, values from `r` may be used freely,
and values from `s` may not reach a place of `r`'s block declared
outside `s`'s. The rule is checked within one function, and needs no
annotation on any other: a callee given `r` sees a scope `Allocator`,
and what it makes through it leaves it only through its results and its
`mut` arguments, which the call's rules cover. An operand passed as a
view, to a view parameter or spread, `append(mut out, l...)`, owns
nothing, as any view, so the elements of a list from `r` are copied out
that way. The rule is conservative: a variable that is ever given a
value from `r` counts as from `r` everywhere in its scope, before that
point included, and any other operand counts whether or not the callee
keeps it.

```tack run exit=0
package main

func fill(n int64, scope a Allocator) list[int64] {
    mut l := make(list[int64], a)
    for i := range n {
        err := append(mut l, i)
        if err.code != 0 {
            return l
        }
    }
    return l
}

func main() {
    mut total int64 = 0
    region r {
        l := fill(10, r)
        for _, v := range l {
            total = total +% v
        }
    }
    if total != 45 {
        exit(1)
    }
}
```

A list made through a region appended to one declared outside it is
the escape the rule stops:

```tack error
package main

func main() {
    mut outer := make(list[list[int64]], c_allocator)
    region r {
        err := append(mut outer, make(list[int64], r))
        if err.code != 0 {
            exit(1)
        }
    }
    if len(outer) != 1 {
        exit(1)
    }
}
```

```diagnostics
main.tck:6:34: error: cannot append a value of type list[int64] holding memory from region r to outer: outer is declared outside the block of region r
main.tck:5:5: note: region r is opened here, and everything made through r is given back when its block ends
```

`exit` inside a region, or an `unreachable` reached in one, runs no
region's end: the chunks are `c_allocator`'s buffers, which it frees
before the process ends, as it frees every buffer it holds.

## When memory is freed

An owned local variable is freed where its block ends, however the block is left: by
falling off its end, `return`, `break` or `continue`. A region's end runs
after every free and defer of its block, so they all still see what was
made through it. The free runs as if
a defer freeing the variable were written right after its declaration, so
frees and defers run together, last first, by position, and a defer
written after the declaration still reads the variable.

- A variable declared in a loop's body is freed at the end of every
  iteration; one a three-clause loop's init declares is freed once the
  loop is done.
- An own parameter is freed when its function returns, at every exit,
  after the function's defers.
- A variable that was moved from is empty, and freeing it does nothing;
  one moved on some paths is freed on the others only.
- Assigning a variable, an element of a list or a value in a map frees
  the old value once the new one is computed, so the new value may read
  the old.
- A list frees its elements, a map its keys and values, and a pool its
  live elements, when it is freed, as a struct frees its fields, last
  first, and a union what its case holds.
- `drop(own x)` frees `x` where it is written, and its block's end then
  frees nothing more, as for any variable moved from.
- Written as a statement, `pop`, `remove` and `delete` free what they
  take, a pool's `remove` included, and `replace(mut place, new)` is an
  assignment, which frees the old value.
- `append`, `put`, `add`, `add_all` and `insert` free the values they were
  given when they cannot grow, and `put` frees a replaced value and a
  duplicate key.

So every owned value is freed exactly once, by its one owner, and no
value is read after it is freed or moved.

## Exit

`exit` ends the process without running any pending defer, in the
function that calls it or in any other, and without freeing any variable
itself. Before the process ends, every allocator frees every buffer it
still holds, so every list and map still live in any frame is freed,
though no code of the program runs to do it. So nothing a program
allocates outlives it when it ends by `exit`.

```tack run exit=3
package main

func work(own l list[int64]) {
    mut keep := make(list[list[int64]], c_allocator)
    err := append(mut keep, l)
    if err.code != 0 {
        exit(1)
    }
    exit(3)
}

func main() {
    mut l := make(list[int64], c_allocator)
    err := append(mut l, 1, 2)
    if err.code != 0 {
        exit(2)
    }
    work(own l)
}
```

A call to `exit` never returns, so it ends its block as a `return` does:
a move in a branch that ends in `exit` is not seen by the code after the
branch, and a branch ending in `exit` leaves for facts (see [Safety and
proofs](10-safety.md#where-a-fact-holds)).
