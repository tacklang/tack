# 13. Builtins

This chapter describes every builtin function, with its signature,
what it does and when it fails: `print`, `exit`, `len`, `make`, the list
operations (`append`, its spread form, `reserve`, `pop`, `remove` and
the rest), the map operations, and the generic builtins. It follows
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
it. Some builtins take any list, map or type, which no Tack declaration
can say exactly; their signatures use stand-in types, which no program
can name:

- `Elem` stands for any type: a list's element, a map's value, a place.
- `Key` stands for any type a map key can have: one with `==`.
- `Sized` stands for any array, view or list, of any element type, or a
  string.
- `ListType` stands for a list type written where a value goes, as
  `make(list[int64], c_allocator)` does.

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

`len` returns the number of elements of the array, view or list `v`, or
the number of bytes of the string `v`. The length of an array, and of a
string literal, is a constant; a view's or a list's is never known.
`len(m)` of a map `m` is the number of keys it holds; `builtin.tck`'s
`len` and `Sized` do not yet say so, and are not yet aligned with
this. Anything else is
`len expects an array, a string, a view, a list or a map, got int64`.

```tack run exit=8
package main

func main() {
    a := [3]int64{1, 2, 3}
    mut l := make(list[uint8], c_allocator)
    err := append(mut l, 'x', 'y')
    if err.code != 0 {
        exit(1)
    }
    if len(a) + len("abc") + len(l) == 8 {
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
the same way. Its first argument is the type as written, `make takes a
list or a map type, as in make(list[int64], c_allocator)`, and it takes
no capacity, `make expects 2 argument(s), got 3`.

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
dropped.

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

A call that drops the error is an error:

```tack error
package main

func main() {
    mut l := make(list[int64], c_allocator)
    append(mut l, 1)
}
```

```diagnostics
main.tck:5:5: error: the error append returns cannot be dropped; bind it, as in err := append(...)
```

### reserve

`func reserve[T Elem](mut l list[T], n int64) error`

`reserve` makes room in `l` for `n` more elements, so that its capacity
is at least its length plus `n`, and returns no error; its length and
elements never change, and `n` of 0 does nothing. A negative `n` returns
`error{code: 2, msg: "negative count"}`, and one the allocator cannot
make room for `error{code: 1, msg: "out of memory"}`, each leaving `l`
as it was. Its error cannot be dropped. A negative constant `n` is an
error:

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
drops the last element when there is one.

### remove

`func remove[T Elem](mut l list[T], i int64) T`

`remove` moves element `i` of `l` out of it and returns it, moving every
element after it down one place, so the rest keep their order. Its
plain form needs `i` proven in range for `l`; its checked form, `x, ok
:= remove(mut l, i)`, may be written anywhere, `ok` being false for an
`i` out of range. Written as a statement it drops element `i` only when
`i` is in range.

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
        if x + y + z == 8 && len(l) == 1 {
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
    if len(l) == 3 && l[0] == 3 && l[2] == 1 && old + x == 9 {
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
dropped.

### delete

`func delete[K Key, V Elem](mut m map[K]V, k K) (V, bool)`

`delete` takes the value under `k` out of `m`, moving it, and frees the
key `m` held: `v, ok := delete(mut m, k)` gives the value and true, or,
when `k` is not in `m`, `V`'s zero value and false. Written as a
statement, `delete(mut m, k)` frees the value it takes.

### has

`func has[K Key, V Elem](m map[K]V, k K) bool`

`has` reports whether `k` is in `m`. What it proves when tested as a
condition is in [Safety and proofs](10-safety.md).

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
    if has(m, "a") && m["a"] + v == 5 {
        exit(5)
    }
}
```

## Generic builtins

`len`, `make`, `append`, `reserve`, `pop`, `remove`, `swap`, `replace`,
`put`, `delete` and `has` are generic: each is checked by the compiler
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
