# 13. Builtins

This chapter describes every builtin function, with its signature,
what it does and when it fails: `print`, `exit`, `len`, `make`, the list
operations (`append`, its spread form, `reserve`, `pop`, `remove` and
the rest), the map operations, `drop`, the arena operations (`add` and
`add_all`), the pool operations (`insert`, and `remove` and `has` of a
pool), and the generic builtins. It follows
[builtin/builtin.tck](../../builtin/builtin.tck), which documents each
builtin in the compiler's own source. `true`, `false`, `noerror` and
`c_allocator` are keywords, not builtins: `noerror` belongs in
[Errors](09-errors.md), and the others in [Types](04-types.md). This
chapter does not repeat the ownership or
safety rules a builtin is subject to, which belong in [Ownership and
memory](08-ownership.md) (what `exit` frees among them) and [Safety and
proofs](10-safety.md); how a program ends, `exit` among the ways,
belongs in [Packages, modules and programs](12-packages.md). Builtin
types belong in [Types](04-types.md).

A builtin function is declared by no source file and needs no import:
every file of every package can call it by its bare name. Its signature
is given here as [builtin/builtin.tck](../../builtin/builtin.tck) gives
it. Some builtins take any list, map, arena, pool or type, which no Tack
declaration can say exactly; their signatures use stand-in types, which no program
can name:

- `Elem` stands for any type: a list's, an arena's or a pool's element,
  a map's value, a place.
- `Key` stands for any type a map key can have: one with `==`.
- `Sized` stands for any array, view, list, arena or pool, of any
  element type, a map, or a string.
- `ListType` stands for a list, a map, an arena or a pool type written
  where a value goes, as `make(list[int64], c_allocator)` does.

## Print

`func print(s string)`

`print` writes `s` to standard output exactly as given, adding no
newline. A line ends where the string has a `\n`.

```tack run exit=0
package main

func main() {
    print("a")
    print("b\n")
}
```

```output
ab
```

## Exit

`func exit(code int32)`

`exit` ends the process immediately with the given status. How a
program ends through it is in [Packages, modules and
programs](12-packages.md), what it frees in [Ownership and
memory](08-ownership.md), and how a call to it ends its block in
[Statements](07-statements.md).

The argument is an `int32`, or an integer that widens to one. Only the
low bits of the status reach the operating system, the low 8 on Linux
and macOS. This program prints a line with `print` and ends with status
3:

```tack run exit=3
package main

func main() {
    print("hi\n")
    exit(3)
}
```

```output
hi
```

and this one, whose status is 70000, ends with status 112:

```tack run exit=112
package main

func main() {
    exit(70000)
}
```

An integer constant outside `int32` is an error:

```tack error
package main

func main() {
    exit(3000000000)
}
```

```diagnostics
main.tck:4:10: error: constant 3000000000 overflows int32
```

## Length

`func len(v Sized) int64`

`len` returns the number of elements of the array, view, list or arena
`v`, an arena's sentinel among them, the number of live elements of the
pool `v`, the number of keys of the map `v`, or the number of bytes of
the string `v`. The length of an array, and of a string literal, is a
constant; a view's, a list's, an arena's or a pool's is never known.
Anything else is `len expects an array, a string, a view, a list, a map,
an arena or a pool, got int64`.

```tack run exit=8
package main

func main() {
    a := [3]int64{1, 2, 3}
    mut l := make(list[uint8], c_allocator)
    err := append(mut l, 'x', 'y')
    if err.code != 0 {
        exit(1)
    }
    if len(a) == 3 && len("abc") == 3 && len(l) == 2 {
        exit(8)
    }
}
```

## Make

`func make(t ListType, alloc Allocator) ListType`

`make` returns an empty list of elements of type `T`, written
`make(list[T], alloc)`, which grows through `alloc` and is freed
through it when its owner's block ends. It allocates nothing, so it
cannot fail. Written `make(map[K]V, alloc)`, it returns an empty map in
the same way. Written `make(arena[T], sentinel, alloc)`, it returns an
arena holding only `sentinel`, which it moves in as the element at id 0,
and allocates nothing either; without the sentinel it is `make expects 3
argument(s), got 2`. Written `make(pool[T], alloc)`, it returns an empty
pool with a tag of its own, and allocates nothing either; it never
fails, however many pools were made before it. Tags are 32 bits: past
2^32 - 2 pools made, later pools share the last tag and may accept each
other's handles, a logic limit, never a read of memory a pool does not
hold. Its first argument is the
type as written, `make takes a list, a map, an arena or a pool type, as
in make(list[int64], c_allocator)`, and it takes no capacity, `make
expects 2 argument(s), got 3`.

## List operations

Each list operation takes its list as `mut`, so the list is a place a
`mut` argument can name; `append(l, 1)` is `append takes l as mut; pass
mut l`.

### append

`func append[T Elem](mut l list[T], elems ...T) error`

`append` appends `elems` to the end of `l`, in order, growing its buffer
through `l`'s allocator when it has no room for them, and returns no
error; or, when the allocator cannot grow it, or `l`'s length would
overflow an `int64`, returns `error{code: 1, msg: "out of memory"}` and
leaves `l` as it was, all or nothing. It takes any number of values,
none included, `append(mut l, a, b, c)`, or a view spread whole,
`append(mut l, v...)`, never a view of `l` itself. Its error cannot be
discarded.

```tack run exit=4
package main

func main() {
    mut l := make(list[int64], c_allocator)
    a := [2]int64{3, 4}
    err := append(mut l, 1, 2)
    if err.code != 0 {
        exit(1)
    }
    err2 := append(mut l, a...)
    if err2.code != 0 {
        exit(2)
    }
    if len(l) == 4 && l[3] == 4 {
        exit(4)
    }
}
```

A call that discards the error is an error:

```tack error
package main

func main() {
    mut l := make(list[int64], c_allocator)
    append(mut l, 1)
}
```

```diagnostics
main.tck:5:5: error: the error append returns cannot be discarded; bind it, as in err := append(...)
```

### reserve

`func reserve[T Elem](mut l list[T], n int64) error`

`reserve` makes room in `l` for `n` more elements, so that its capacity
is at least its length plus `n`, and returns no error; its length and
elements never change, and `n` of 0 does nothing. A negative `n` returns
`error{code: 2, msg: "negative count"}`, and one the allocator cannot
make room for `error{code: 1, msg: "out of memory"}`, each leaving `l`
as it was. Its error cannot be discarded. `l` may be an arena,
`reserve(mut a, n)`, which also returns `error{code: 1, msg: "out of
memory"}` when `a` could not hold `n` more without passing 2^32 - 1
elements, its sentinel among them; anything else is `reserve expects a
list or an arena, got [3]int64`. A negative constant `n` is an error:

```tack error
package main

func main() {
    mut l := make(list[int64], c_allocator)
    err := reserve(mut l, -1)
    if err.code != 0 {
        exit(1)
    }
}
```

```diagnostics
main.tck:5:27: error: reserve count must not be negative, got -1
```

### pop

`func pop[T Elem](mut l list[T]) T`

`pop` moves the last element of `l` out of it and returns it, leaving
`l` one element shorter; nothing is copied, allocated or freed. Its
plain form, `x := pop(mut l)`, needs `len(l) > 0` proven where it is
written (see [Safety and proofs](10-safety.md)). Its checked form, `x,
ok := pop(mut l)`, may be written anywhere: `ok` is false for an empty
list, which is left as it was. Written as a statement, `pop(mut l)`
discards the last element when there is one.

### remove

`func remove[T Elem](mut l list[T], i int64) T`

`remove` moves element `i` of `l` out of it and returns it, moving every
element after it down one place, so the rest keep their order. Its
plain form needs `i` proven in range for `l`; its checked form, `x, ok
:= remove(mut l, i)`, may be written anywhere, `ok` being false for an
`i` out of range. Written as a statement it discards element `i` only when
`i` is in range. `l` may be a pool (see [remove of a
pool](#remove-of-a-pool)); anything else is `remove expects a list or a
pool, got arena[int64]`.

```tack run exit=6
package main

func main() {
    mut l := make(list[int64], c_allocator)
    err := append(mut l, 1, 2, 3, 4)
    if err.code != 0 {
        exit(1)
    }
    x, ok := pop(mut l)
    if !ok {
        exit(2)
    }
    y, found := remove(mut l, 0)
    if !found {
        exit(3)
    }
    if len(l) > 0 {
        z := pop(mut l)
        if x == 4 && y == 1 && z == 3 && len(l) == 1 {
            exit(6)
        }
    }
}
```

The plain form without its proof is an error:

```tack error
package main

func main() {
    mut l := make(list[int64], c_allocator)
    x := pop(mut l)
    if x == 0 {
        exit(1)
    }
}
```

```diagnostics
main.tck:5:10: error: pop may find l empty; check len(l) > 0 first, or use x, ok := pop(mut l)
```

### swap

`func swap[T Elem](mut l list[T], i int64, j int64)`

`swap` swaps elements `i` and `j` of `l` in place; `i` equal to `j`
leaves `l` as it was. It moves, copies and frees nothing, so it
reorders lists of owned elements. Both indexes need proofs, as `l[i]`
does, and a proof about `len(l)` holds across it. It returns no value.

### replace

`func replace[T Elem](mut place T, new T) T`

`replace` moves the value of `place` out, as its result, and moves `new`
into `place`, in one step, so the place never holds no value: `old :=
replace(mut s.items, make(list[T], alloc))` takes a list out of a
field, which no move can do. `place` is anything a `mut` argument can
be. Written as a statement, `replace(mut place, new)` is an assignment,
which frees the old value.

```tack run exit=9
package main

func main() {
    mut l := make(list[int64], c_allocator)
    err := append(mut l, 1, 2, 3)
    if err.code != 0 {
        exit(1)
    }
    if len(l) == 3 {
        swap(mut l, 0, 2)
    }
    mut x := 4
    old := replace(mut x, 5)
    if len(l) == 3 && l[0] == 3 && l[2] == 1 && old == 4 && x == 5 {
        exit(9)
    }
}
```

## Map operations

### put

`func put[K Key, V Elem](mut m map[K]V, k K, v V) error`

`put` puts `v` under `k` in `m`: in place of the value `k` has, which it
frees, keeping `k`'s position in `m`'s order; or at the end of the order
when `k` is new. When the allocator cannot grow `m` it returns
`error{code: 1, msg: "out of memory"}`, leaves `m` exactly as it was,
and frees `k` and `v`. `k` and `v` are moved in. Its error cannot be
discarded.

### delete

`func delete[K Key, V Elem](mut m map[K]V, k K) (V, bool)`

`delete` takes the value under `k` out of `m`, moving it, and frees the
key `m` held: `v, ok := delete(mut m, k)` gives the value and true, or,
when `k` is not in `m`, `V`'s zero value and false. Written as a
statement, `delete(mut m, k)` frees the value it takes.

### has

`func has[K Key, V Elem](m map[K]V, k K) bool`

`has` reports whether `k` is in `m`. What it proves when tested as a
condition is in [Safety and proofs](10-safety.md). `m` may be a pool
(see [has of a pool](#has-of-a-pool)).

```tack run exit=5
package main

func main() {
    mut m := make(map[string]int64, c_allocator)
    err := put(mut m, "a", 2)
    if err.code != 0 {
        exit(1)
    }
    err2 := put(mut m, "b", 3)
    if err2.code != 0 {
        exit(2)
    }
    v, ok := delete(mut m, "b")
    if !ok || has(m, "b") || len(m) != 1 {
        exit(3)
    }
    if has(m, "a") && m["a"] == 2 && v == 3 {
        exit(5)
    }
}
```

## Drop

`func drop[T Elem](own x T)`

`drop` frees `x` at once, exactly as the end of its block would, and
leaves it moved from, so its block's end frees nothing more. `x` is a
local variable or an own parameter whose type owns something, written
`own x`; what may be dropped, and why a field, an element, a borrowed
parameter or a value that owns nothing may not, is in [Ownership and
memory](08-ownership.md#drop). It returns no value, so it is only a
statement.

```tack run exit=3
package main

type Bag struct {
    items list[int64]
    more  list[int64]
}

func main() {
    mut b := Bag{items: make(list[int64], c_allocator), more: make(list[int64], c_allocator)}
    err := append(mut b.items, 1, 2, 3)
    if err.code != 0 {
        exit(1)
    }
    n := len(b.items)
    drop(own b)
    if n == 3 {
        exit(3)
    }
}
```

A value that owns nothing is an error, since there would be nothing to
free:

```tack error
package main

func main() {
    n := 4
    drop(own n)
}
```

```diagnostics
main.tck:5:14: error: cannot drop n: int64 owns no list or map, so there is nothing to free; remove the drop
```

## Arena operations

Each arena operation takes its arena as `mut`, as a list operation takes
its list; anything but an arena is `add expects an arena, got
list[int64]`.

### add

`func add[T Elem](mut a arena[T], v T) (id[T], error)`

`add` moves `v` into `a` as its new last element and returns its id,
which names it for as long as `a` lives, and no error. When the
allocator cannot grow `a`, or `a` would then hold more than 2^32 - 1
elements, its sentinel among them, it returns the zero id and
`error{code: 1, msg: "out of memory"}`, leaving `a` as it was and
freeing `v`. Its two results are bound together, `e, err := add(mut a,
v)` or `add(mut a, v)?`: the id cannot be read before the error is
checked, the error cannot be discarded, `the error add returns cannot
be discarded; bind it, as in _, err := add(...)`, and it gives no one
value, `add returns 2 values`.

### add_all

`func add_all[T Elem](mut a arena[T], elems ...T) (span[T], error)`

`add_all` moves its values into `a`, in order, as its new last elements,
all or none, and returns their span, from the first one's id, and no
error; with no values it adds nothing and returns the empty span. It
fails as `add` does, returning the empty span, leaving `a` as it was and
freeing every value it was given. It takes any number of values,
`add_all(mut a, x, y)`, or a view spread whole, `add_all(mut a, v...)`,
whose elements are copied, so not a view of an owned element type, and
never a span's view of `a` itself, which `add_all` may move.

```tack run exit=6
package main

func total(v []int64) int64 {
    mut t int64 = 0
    for _, x := range v {
        t = t +% x
    }
    return t
}

func main() {
    mut a := make(arena[int64], 0, c_allocator)
    s, err := add_all(mut a, 1, 2, 3)
    if err.code != 0 {
        exit(1)
    }
    if len(a[s]) == 3 && total(a[s]) == 6 {
        exit(6)
    }
}
```

## Pool operations

`insert` and a pool's `remove` take the pool as `mut`, as a list
operation takes its list, `remove takes p as mut; pass mut p`; `has`
reads it, as it reads a map. Anything but a pool is `insert expects a
pool, got list[int64]`. `remove` and `has` are the list's and the map's,
which take a pool and a handle too.

### insert

`func insert[T Elem](mut p pool[T], v T) (handle[T], error)`

`insert` moves `v` into `p` and returns its handle, which names it until
it is removed, and no error. It takes a slot an earlier `remove` freed
when there is one, and otherwise grows `p` through its allocator. When
it can do neither, the allocator failing or `p` holding 2^32 - 1 slots,
it returns the zero handle and `error{code: 1, msg: "out of memory"}`,
leaving `p` as it was and freeing `v`. Its two results are bound
together, `h, err := insert(mut p, v)` or `insert(mut p, v)?`: the
handle cannot be read before the error is checked, the error cannot be
discarded, `the error insert returns cannot be discarded; bind it, as in
h, err := insert(...)`, and it gives no one value, `insert returns 2
values`.

### remove of a pool

`remove(mut p, h)`, for a pool `p` and a handle `h` of its element type,
moves out the element `h` names. `v, ok := remove(mut p, h)` gives it and
true, or, when `h` names no live element of `p`, a stale handle, one of
another pool or the zero handle, the element type's zero value and
false, leaving `p` as it was; for an element type with no zero value,
`v` cannot be read before `ok` is checked, as `pop`'s cannot. Written as
a statement, `remove(mut p, h)` frees the element when there is one.
Neither form needs a proof, and there is no plain form, `remove gives
the element and whether the handle was live; use v, ok := remove(mut p,
h)`. The slot's generation moves on, so no handle to the element names
anything again.

### has of a pool

`has(p, h)` reports whether `h` names a live element of `p`; what it
proves when tested as a condition is in [Safety and
proofs](10-safety.md#pool-lookups). Anything but a map or a pool is `has
expects a map or a pool, got list[int64]`.

```tack run exit=7
package main

func main() {
    mut p := make(pool[int32], c_allocator)
    a, err := insert(mut p, 3)
    if err.code != 0 {
        exit(1)
    }
    b, err2 := insert(mut p, 4)
    if err2.code != 0 {
        exit(2)
    }
    remove(mut p, a)
    stale, found := p[a]
    v, ok := remove(mut p, b)
    if has(p, a) || found || stale != 0 || !ok || len(p) != 0 {
        exit(3)
    }
    c, err3 := insert(mut p, 7)
    if err3.code != 0 {
        exit(4)
    }
    if has(p, c) && c != a && v == 4 {
        exit(p[c])
    }
}
```

## Generic builtins

`len`, `make`, `append`, `reserve`, `pop`, `remove`, `swap`, `replace`,
`put`, `delete`, `has`, `drop`, `add`, `add_all` and `insert` are generic: each is checked by the compiler
itself, against the argument types of each call, as the signatures
above describe with their stand-in types. Their type arguments are
never written, as no generic function's are (see [Generics and
interfaces](11-generics.md)).

No builtin function is a value:

```tack error
package main

func main() {
    f := len
}
```

```diagnostics
main.tck:4:10: error: cannot use the builtin len as a value; only a declared function can be one
```

Whether a declaration may reuse a builtin function's name is in
[Declarations and scope](05-declarations.md).
