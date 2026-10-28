# 8. Ownership and memory

This chapter describes the memory model: which values own memory, how a
value is copied or moved, when a moved-from variable may no longer be
used, `own` parameters and arguments, `mut` parameters and receivers and
the rule that a `mut` argument is the only way to reach its place during
the call, views and the borrows they make (view parameters and view
results, and the conversion of a string to a view of its bytes,
`[]uint8(s)`), lists and maps as owners, allocators and what a list or
map holds of one, when memory is freed (end of scope, reassignment,
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

A value is *owned* when its type holds a list or a map: a list, a map, a
struct with a field that holds one, directly or through another struct
or a union, a union with a case whose payload holds one, and, inside a
generic function, a value of a type parameter, whatever type a call
gives it. Every other value is *copyable*: the integers, the floats,
`bool`, `string`, `error`, `Allocator`, function values, and arrays and
structs of copyable values.

A copyable value is copied wherever it is declared, assigned, passed,
returned or stored, and the copy is independent of the original. A
string is copyable and its bytes are static: a string, and a slice of
one, which shares the bytes it is cut from, never allocates and is never
freed.

An owned value has exactly one owner at a time, a variable, a parameter
that took it with `own`, or the list, map, struct or union that holds it.
The owner frees it, and nothing else does. An owned value is never
copied; where a copyable value would be copied, it is moved (see
[Moves](#moves)).

An owned type has no zero value, so a variable of one must be declared
with a value: `mut l list[int64]` is `l needs a value: list[int64] has no
zero value`. An array cannot hold an owned element, since its elements
are copied: `an array cannot hold list[int64]: its elements are copied,
and a list cannot be`; the same holds for a variadic parameter. No
package-level variable may hold a list or a map, `package-level variable
g cannot hold a list`, `package-level variable g cannot hold a map`, so a
function reaches a list or a map only through its parameters and its own
locals. No type may hold itself through a list or a map: `invalid
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
here; store it in a variable first`, and a call statement that drops one
is `make returns list[int64], which cannot be dropped; bind it to a
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
        sum += x
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
parameter: it owns no list or map, so it is copied; drop own`. A type
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

## Mut parameters and receivers

A parameter written `mut p T` refers to the caller's place for the
duration of the call: reading and writing `p`, its fields and its
elements reads and writes the caller's place directly. Every other
parameter, an own parameter aside, is an immutable copy of its argument,
or, for an owned or a view type, a read-only borrow of it (see [Views
and borrows](#views-and-borrows)).

The caller writes `mut` before the argument, `scale(mut b, 3)`, and only
there. Leaving it out for a `mut` parameter is `scale takes p as mut;
pass mut b`, and writing it for a plain one is `count does not take t as
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
    p.x *= k
    p.y *= k
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
- a value assigned to an element of a list, or to a value in a map, that
  passes the list or map, or anything holding it, as `mut`: `cannot
  assign to l[0] alongside mut l in its value: an element is assigned in
  place in its list, ...`; a `mut` argument inside the list, one of its
  elements, may be passed;
- a comparison of two lists, or of values holding them, where one operand
  passes the other as `mut`: `cannot compare a alongside mut a in the
  other operand: ...`;
- a view a call returns, which may point into every argument of its call
  that is not `mut` and holds data, the receiver included: `cannot pass
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
    mut n int64 = 0
    for _, b := range v {
        if b == c {
            n++
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
element of a list or a view, since a plain array parameter is the
function's own copy.

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
        n += x
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
the view is dropped, borrowing nothing past the statement. It borrows
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
owner, and copy and allocate nothing. Written as a statement, each drops
and frees the element it takes when there is one. A checked `pop` or
`remove` that finds no element leaves the list as it was; when the
element type has no zero value, the element variable then holds nothing
to free and cannot be read until `ok` is checked true. An owned element
bound to `_` would never be freed: `pop returns list[int64], which cannot
be dropped; bind it to a variable instead of _`. `swap(mut l, i, j)`
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

## Allocators

An `Allocator` is an opaque handle on an allocator: a list or a map holds
the one it was made with, grows through it, and is freed through it. An
`Allocator` is copyable; copying it copies the handle, not the
allocator. The `Allocator` type's other properties, and `c_allocator`,
are described in [Types](04-types.md).

## When memory is freed

An owned local variable is freed where its block ends, however the block is left: by
falling off its end, `return`, `break` or `continue`. The free runs as if
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
- A list frees its elements, and a map its keys and values, when it is
  freed, as a struct frees its fields, last first, and a union what its
  case holds.
- Written as a statement, `pop`, `remove` and `delete` free what they
  take, and `replace(mut place, new)` is an assignment, which frees the
  old value.
- `append` and `put` free the values they were given when they cannot
  grow, and `put` frees a replaced value and a duplicate key.

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
