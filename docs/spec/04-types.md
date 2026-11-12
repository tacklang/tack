# 4. Types

This chapter describes every kind of type and what values it holds: the
integer types, the target-width integers, the floats, `bool`, `string`,
`error`, `any`, `Allocator`, arrays, views, structs, unions, lists, maps
and function types, with each one's zero value (or lack of one), its
size and layout limits, and which types are comparable, ordered,
copyable and owned. It owns the value of a string or raw string
literal, and what comparing two strings, `bool`s, arrays, lists,
structs or unions computes; comparing numbers belongs in [Constants and
arithmetic](03-arithmetic.md), and comparing errors in
[Errors](09-errors.md). It also owns the values
of the keywords `true`, `false` and `c_allocator`; `noerror` belongs in
[Errors](09-errors.md). It says when two types are the same and when a
value of one may be used as another without a conversion, the implicit
widening of integers included; what a numeric conversion computes
belongs in [Constants and arithmetic](03-arithmetic.md), and the
conversion of a string to a view of its bytes, `[]uint8(s)`, in
[Ownership and memory](08-ownership.md). How a type is declared and
named belongs in [Declarations and scope](05-declarations.md); what
owning, moving and freeing a value of a type means belongs in
[Ownership and memory](08-ownership.md); interfaces as constraints
belong in [Generics and interfaces](11-generics.md); and the C layout of
each type belongs in [Extern C](14-extern.md).

The types are the integer types, the target-width integers, the float
types, `bool`, `string`, `error`, `any`, `Allocator`, arrays, views,
lists, maps, function types, the structs and unions a program declares,
and, within a generic function, its type parameters (see [Generics and
interfaces](11-generics.md)). There is no `int` and no `byte`.

## Integer types

The integer types and their values are:

| Type     | Values                                             |
|----------|----------------------------------------------------|
| `int8`   | -128 to 127                                        |
| `int16`  | -32768 to 32767                                    |
| `int32`  | -2147483648 to 2147483647                          |
| `int64`  | -9223372036854775808 to 9223372036854775807        |
| `uint8`  | 0 to 255                                           |
| `uint16` | 0 to 65535                                         |
| `uint32` | 0 to 4294967295                                    |
| `uint64` | 0 to 18446744073709551615                          |

A signed integer is held in two's complement, which the bitwise
operators and the wrapping operators `+%`, `-%` and `*%` show (see
[Integer overflow](03-arithmetic.md#integer-overflow)); plain `+`, `-`
and `*` never wrap. An integer constant that nothing gives a
type is an `int64`, and a character literal is a `uint8`.

## Target-width integers

`unsafe_size_t` and `unsafe_uintptr_t` are C's `size_t` and `uintptr_t`,
for crossing to C (see [Extern C](14-extern.md)). Each is an unsigned
integer as wide as the target makes it, from 32 to 64 bits, and the two
are not necessarily as wide as each other. The `unsafe_` prefix is their
only marker: they may be used anywhere any other integer type can, and
have every operation an unsigned integer has, the wrapping operators
wrapping at their real width; plain arithmetic on them must be proven
to fit on every target (see [Integer overflow](03-arithmetic.md#integer-overflow)).

Their rules hold at every width, so a program valid on a 64-bit target is
valid on a 32-bit one. A constant of either is a value of it only from 0
to 4294967295 (see [Types of
constants](03-arithmetic.md#types-of-constants)). `uint8`, `uint16` and
`uint32` widen into either, and either widens into `uint64`; nothing else
widens into or out of them, no signed type, not `int64`, and neither into
the other (see [Implicit widening](#implicit-widening)). Like every
integer, they compare by value with every integer type, each other
included.

## Float types

`float32` and `float64` are IEEE 754's binary32 and binary64. A float
constant that nothing gives a type is a `float64`. No integer converts
to a float, nor one float type to the other, without a conversion. What
the operators and conversions compute on floats is described in
[Constants and arithmetic](03-arithmetic.md#floats).

## Booleans

`bool` has two values, `true` and `false`. `true` and `false` are
keywords, so neither can name anything, and each is a constant
expression of type `bool`. A comparison and the operators `&&`, `||` and
`!` give a `bool`. A `bool` converts to and from no number.

## Strings

A `string` is an immutable sequence of bytes. Nothing in the language
decodes them: `len(s)` is the number of bytes of `s`, `s[i]` is the byte
at `i`, a `uint8`, a range over a string gives its bytes, and `s[i:j]`
holds the bytes from `i` up to `j`. A string's bytes cannot be assigned,
in a mutable string variable too.

The value of a string literal is the bytes between its quotes, with each
escape replaced by the byte it stands for: `\a`, `\b`, `\f`, `\v`, `\n`,
`\t`, `\r`, `\\` and `\"` by the byte Go gives each, and `\x` and two hex
digits by the byte they spell. Every other byte stands for itself, so
UTF-8 passes through unchanged. The value of a raw string literal is
every byte between its backquotes as written, with nothing decoded,
except that each carriage return is dropped, so the value of a raw
string spanning lines does not depend on the line endings its file was
saved with. How each literal is spelled is described in [Source and
lexical elements](02-lexical.md).

```tack run exit=0
package main

func main() {
    if len("\x41\x00\xff") != 3 || "\x41" != "A" {
        exit(1)
    }
    if len(`a\nb`) != 4 || `\` != "\\" {
        exit(2)
    }
    exit(0)
}
```

Two strings compare with any of the six comparison operators. They are
compared byte by byte, each byte as an unsigned value, as in Go: the first
byte that differs decides, a proper prefix is less than the string it
starts, so the empty string is the least, and a zero byte is a byte like
any other. A byte from 0x80 up is greater than every ASCII byte, so
`"\xc3\xa9"`, the UTF-8 of an accented e, is greater than `"z"`.
Comparing strings allocates nothing.

```tack run exit=0
package main

func less(a string, b string) bool {
    return a < b
}

func main() {
    if !less("", "a") || !less("ab", "abc") || !less("abc", "abd") {
        exit(1)
    }
    if !less("z", "\xc3\xa9") || !less("a", "a\x00") {
        exit(2)
    }
    exit(0)
}
```

## The error type

`error` is a builtin struct type of two fields, `code`, an `int32`, and
`msg`, a `string`. What an error means, its zero value `noerror`, and
what comparing two errors computes are described in
[Errors](09-errors.md). An error has no order, no conversion, and cannot
be passed as `any`.

## The any type

A value of type `any` refers to a value of one of the fourteen basic
types, the ten integer types, the two float types, `bool` and `string`,
or to a view of one of them, with which type known only at run time.
`any` is the type of a parameter only, or the element type of a
variadic or view parameter; anywhere else, a local, a field, a global, a
result or an array's element, it is an error, `any can only be the type
of a parameter`. An immutable local may still hold one, declared from an
`any` with its type left out. A value is passed to an `any` parameter as
it is, with no conversion; `any(x)` is an error.

An `any` supports no operator, no conversion, and no use as another
type. It is asked which type it holds with a type assertion (see
[Expressions](06-expressions.md)) or a type switch (see
[Statements](07-statements.md)).

## Allocators

`Allocator` is a builtin type: an opaque handle on an allocator, through
which a list or a map grows and is freed (see [Ownership and
memory](08-ownership.md)). It is copyable, and may be the type of a
local, a parameter, a result, a field, an array's or a list's element or
a global. It has no zero value, no `==` and no order, and cannot be
passed as `any`.

`c_allocator` is a keyword, the allocator that lowers to C's `malloc`,
`realloc` and `free`. It is a constant expression, so a package-level
variable may be initialized with it. The only other allocators are
regions' (see [Statements](07-statements.md)): a region's allocator is
an `Allocator` too, but second class, given only to `make` or passed to
a parameter written `scope a Allocator`, which is under the same rule in
its function (see [Ownership and memory](08-ownership.md)). Any
`Allocator` may be passed to a scope one. An `Allocator` is 8 bytes, a
16-bit index and a 48-bit generation.

## Arrays

`[n]T` is an array of `n` elements of type `T`. `n` is a known value
that is not negative (see [Safety and
proofs](10-safety.md#known-values)), and, in a struct field's type or a
union case's payload type, a constant expression that names no variable.
Arrays of different lengths, or of different element types, are
different types. `[0]T` is a type, with no elements.

An array is a value: assigning, passing or returning one copies it, and
it follows its variable, so an element is assigned only through a
mutable variable. An array's elements are copied with it, so they are
copyable: an array cannot hold an owned type, `an array cannot hold
list[int64]: its elements are copied, and a list cannot be`.

Two arrays of one type compare with `==` and `!=` when their element type
does, element by element; `<` and the other orderings are errors.

## Views

`[]T` is a view: a read-only reference to a sequence of elements of type
`T`, held by an array, a list, another view, or a string's bytes. A view
is the type of a parameter or of a result only; anywhere else, a local, a
field, a global or an array's element, it is an error, `a view can only
be the type of a parameter or of a result`. A view's element type is
not a view, `the element type of a view cannot be a view`. A view has no `==`, no order and no conversion. What may be
passed to a view, what a view refers to, and for how long, are described
in [Ownership and memory](08-ownership.md).

## Structs

A struct type is a sequence of named fields, each of its own type. Each
struct declaration is a type of its own, distinct from every other, even
one with the same fields (see [Declarations and
scope](05-declarations.md)). A field's type is any type but a view or
`any`, and a struct cannot hold itself, directly, through other structs
or arrays, or through a list.

A struct is a value, like an array: assigning, passing or returning one
copies it, unless it holds an owned value (see [Lists](#lists)), and it
follows its variable, so a field is assigned only through a mutable
variable.

Two structs of one type compare with `==` and `!=` when every field's
type does, field by field, a private field included, each through its own
type's `==`; a struct with no fields is equal to every other value of
its type. Structs have no order: `<` on two structs is an error.

## Unions

A union type is a set of named cases, each with the type of its payload
or with none. A value of a union holds exactly one of its cases, and
that case's payload if it has one. Each union declaration is a type of
its own. A union cannot hold itself, directly or through a struct, an
array, another union or a list. A union with no cases, `union {}`, has no
values.

A union is a value: assigning, passing or returning one copies it, unless
it holds an owned value, and it is changed only whole.

Two unions of one type compare with `==` and `!=` when every payload's
type does: they are equal when they hold the same case and, for a case
with a payload, equal payloads, compared through the payload type's
`==`. A union whose case has a payload of a type with no `==`, an
`Allocator` or a map among them, has none. Unions have no order.

```tack run exit=0
package main

type Point struct {
    x int64
    y int64
}

type Token union {
    num int64
    word string
    eof
}

func main() {
    a := [2]Point{{x: 1, y: 2}, {x: 3, y: 4}}
    b := [2]Point{{x: 1, y: 2}, {x: 3, y: 4}}
    if a != b {
        exit(1)
    }
    if Token.num(1) == Token.num(2) || Token.num(1) != Token.num(1) {
        exit(2)
    }
    if Token.word("eof") == Token.eof {
        exit(3)
    }
    exit(0)
}
```

## Lists

`list[T]` is an owned, growable list of elements of type `T`, stored by
value. `list` is a keyword. A list is made with `make` (see
[Builtins](13-builtins.md)), has one owner, moves rather than copies, and
is freed when its owner's block ends (see [Ownership and
memory](08-ownership.md)). Its element type may itself be owned. A list
cannot be held by a package-level variable, nor by an array.

A struct with a field of an owned type, directly or through another
struct or a union, is owned as a list is, and so is a union with a case
whose payload is owned.

Two lists of one type compare with `==` and `!=` when their element type
does: they are equal when they have the same length and each element
equals the one at the same index, compared through the element type's
`==`, whatever their capacities. So two empty lists are equal, and a
proper prefix is not equal to the list it starts. Lists have no order.

## Maps

`map[K]V` holds values of type `V` by keys of type `K`, both owned by the
map. `map` is a keyword. `K` is a type with `==`: an integer, a `bool`, a
`string`, or an array, a struct, a union or a list of such types. A float,
or a type holding one, is not a key, though floats compare, and neither
is `error`, `any`, `Allocator`, a map, a type with no `==`, or a type
parameter. `V` is any type a list's element may be, a type parameter
included.

A map is owned, as a list is, and nests in lists, structs, unions and
other maps. It has no `==` and no order, and no package-level variable
may hold one.

## Arenas, ids and spans

`arena[T]` is an owned, append-only store of elements of type `T`, which
may be any type, an owned one or a type parameter included. `arena` is a
keyword. An arena is made with `make(arena[T], sentinel, alloc)`, which
places `sentinel` at id 0 and allocates nothing, and grows only, through
`add` and `add_all` (see [Builtins](13-builtins.md#arena-operations)):
nothing removes, pops, swaps, slices or shrinks its elements while it
lives. It is owned as a list is, has no zero value, no `==` and no
literal, is no global, cannot be passed as `any` and does not cross to C.
A type cannot hold an arena of itself, since an arena holds its sentinel
by value.

`id[T]` names one element of an `arena[T]`, and `span[T]` a run of
consecutive ones, a start and a length. `id` and `span` are builtin type
names, written only with an element type. Each is 32 bits, a span's
start and length each, copyable, comparable with `==` and `!=`, and may
be a map's key; neither has an order or arithmetic, and neither converts
to or from an integer, so no id names a slot but one `add` gave or the
sentinel's. An id's zero value is 0, the sentinel's id, and a span's the
empty span. A type may hold an id or a span of itself. Ids and spans of
two element types are two types.

`a[e]` is the element the id `e` names, a place (see
[Expressions](06-expressions.md)), read with no proof: an id at or past
`len(a)`, the zero id, and an id of another arena past this one's end
all name the sentinel. `a[s]` is a read-only view of the span's elements,
empty when the span is not wholly in `a`.

```tack run exit=0
package main

type Node struct {
    value int64
    next  id[Node]
}

func main() {
    mut none id[Node]
    mut nodes := make(arena[Node], Node{value: -1, next: none}, c_allocator)
    first, err := add(mut nodes, Node{value: 1, next: none})
    if err.code != 0 {
        exit(1)
    }
    second, err2 := add(mut nodes, Node{value: 2, next: first})
    if err2.code != 0 {
        exit(1)
    }
    mut sum int64 = 0
    mut at := second
    for at != none {
        sum = sum +% nodes[at].value
        at = nodes[at].next
    }
    if sum != 3 || len(nodes) != 3 || nodes[none].value != -1 {
        exit(2)
    }
    exit(0)
}
```

An integer indexes no arena, and an id converts from no integer:

```tack error
package main

func main() {
    a := make(arena[int64], 0, c_allocator)
    n := 3
    e := id[int64](n)
    if a[0] == a[e] {
        exit(1)
    }
}
```

```diagnostics
main.tck:6:10: error: cannot convert int64 to id[int64]
main.tck:7:10: error: cannot index arena[int64] with int64; index it with an id[int64]
```

## Pools and handles

`pool[T]` is an owned store of elements of type `T`, which may be any
type, an owned one or a type parameter included, any one of which can be
removed on its own. `pool` is a keyword. A pool is made empty with
`make(pool[T], alloc)`, which allocates nothing, and changes through
`insert` and `remove` (see [Builtins](13-builtins.md#pool-operations)).
It is owned as a list is, has no zero value, no `==` and no literal, is
no global, cannot be passed as `any` and does not cross to C. A type
cannot hold a pool of itself.

`handle[T]` names one element of a `pool[T]`. `handle` is a builtin type
name, written only with an element type. A handle is a slot, the slot's
generation and the pool's tag, 32 bits each: copyable, comparable with
`==` and `!=`, and a map's key, with no order or arithmetic and no
conversion to or from an integer, so no handle names a slot but one
`insert` gave. Its zero value names no element of any pool. A type may
hold a handle of itself. Handles of two element types are two types.

Removing an element moves its slot's generation on, so every handle to
it is stale from then on: it names no element, even once the slot holds
another. A slot whose generation would wrap past 2^32 - 1 is never used
again. Every pool has a tag, so a handle from another pool names no
element of it either, except that past 2^32 - 2 pools made, later pools
share the last tag and may accept each other's handles, a logic limit,
never a read of memory a pool does not hold. `has(p, h)` reports whether `h` names a live
element of `p`, and proves it as `has(m, k)` proves a map's key (see
[Safety](10-safety.md#pool-lookups)); `p[h]` is then a place, and
anywhere else a read needs the checked form, `v, ok := p[h]`; a range
over `p` proves each handle it gives live in its body. `len(p)` is how
many live elements `p` holds.

```tack run exit=0
package main

type Node struct {
    value int64
    next  handle[Node]
}

func main() {
    mut none handle[Node]
    mut nodes := make(pool[Node], c_allocator)
    first, err := insert(mut nodes, Node{value: 1, next: none})
    if err.code != 0 {
        exit(1)
    }
    second, err2 := insert(mut nodes, Node{value: 2, next: first})
    if err2.code != 0 {
        exit(1)
    }
    mut sum int64 = 0
    if has(nodes, second) {
        sum = nodes[second].value
    }
    remove(mut nodes, first)
    if has(nodes, first) || len(nodes) != 1 || sum != 2 {
        exit(2)
    }
    exit(0)
}
```

A handle converts from no integer, and needs `has` before it is read:

```tack error
package main

func main() {
    mut p := make(pool[int64], c_allocator)
    h, err := insert(mut p, 1)
    if err.code != 0 {
        exit(1)
    }
    n := 3
    g := handle[int64](n)
    if p[h] == 1 && has(p, g) {
        exit(1)
    }
}
```

```diagnostics
main.tck:10:10: error: cannot convert int64 to handle[int64]
main.tck:11:10: error: handle h may not be live in p; check has(p, h) first, or use v, ok := p[h]
```

## Function types

A function type is written as a function's signature is, with the types
of its parameters alone and its results: `func(int64) int64`,
`func([]uint8, mut list[int64]) (int64, error)`, `func()`. A `mut`
parameter is written `mut T`, an `own` parameter `own T` and a `scope`
parameter, which takes a function value that does not outlive the call,
`scope F`, `F` a function type: `func(list[int64], scope func(int64,
int64) bool)`, or an `Allocator` that does not, `scope Allocator`. A function
type's parameters have no names; none is variadic, `any`, or a `mut`
view; and it has no view result.

A value of a function type is a declared function of that signature,
named anywhere but as a call's callee, or a method named through its
type, `Pair.first`, whose receiver is the first parameter; a generic
function, an extern func and a builtin are not values (see
[Expressions](06-expressions.md)). A function
value is copyable and owns nothing. It has no `==`, no order and no
conversion, and cannot be passed as `any`, held by a package-level
variable, or passed to C. A closure, a function literal, is not a value
of a function type that can be held: it can only be passed to a `scope`
parameter (see [Expressions](06-expressions.md#function-literals)).

## Zero values

A mutable variable declared without a value, `mut x T`, starts at `T`'s
zero value. The zero values are:

- 0 for every integer type and 0.0 for both float types;
- `false` for `bool`;
- the empty string, `""`, for `string`;
- `noerror` for `error`;
- 0, the sentinel's id, for an `id[T]`, and the empty span for a
  `span[T]`;
- a handle no pool gives, which names no element, for a `handle[T]`;
- for an array, every element at its type's zero value, and for a
  struct, every field at its type's zero value.

A list, a map, an arena, a pool, an `Allocator`, a union and a function type have
no zero value, and neither does a struct or an array that holds one of
them: a
variable of such a type must be given a value, `l needs a value:
list[int64] has no zero value`. A view and an `any` have no zero value
either; no variable of either type is declared without one.

```tack run exit=0
package main

type Point struct {
    x int64
    s string
}

func main() {
    mut n int64
    mut f float64
    mut b bool
    mut p Point
    mut a [3]uint8
    mut e error
    n = n + 1
    f = f + 1.0
    b = !b
    if n != 1 || f != 1.0 || !b || p.x != 0 || p.s != "" || a[1] != 0 || e.code != 0 {
        exit(1)
    }
    exit(0)
}
```

A variable of a type with no zero value cannot be declared without one:

```tack error
package main

type Expr union {
    num int64
    eof
}

func empty() list[int64] {
    mut l list[int64]
    return l
}

func alloc() Allocator {
    mut a Allocator
    return a
}

func expr() Expr {
    mut e Expr
    return e
}

func main() {
    exit(0)
}
```

```diagnostics
main.tck:9:9: error: l needs a value: list[int64] has no zero value
main.tck:14:9: error: a needs a value: Allocator has no zero value
main.tck:19:9: error: e needs a value: Expr has no zero value
```

## Size limits

No value holds more than 2^31 bytes, whatever its type and wherever it
is: a global, a local, a parameter, a result, a field, an element of an
array or a list, a union's payload or a type argument. A value's size is
that of the C object the backend makes of it on the 64-bit targets it
builds for, arm64 and x86-64, nested arrays and structs included, with
C's padding: there a pointer, an `int64` and an `unsafe_size_t` are 8
bytes, aligned to 8, `[0]T` holds one element, and a struct with no
fields one byte. A type that holds more is an error where it is made too
large, and not at its uses: `array type [2147483649]uint8 is too large:
it holds 2147483649 bytes, more than the 2^31 a value may hold`, or, for
a struct or a union whose fields or payloads together exceed the limit,
`type S is too large: it holds 2147483656 bytes, more than the 2^31 a
value may hold`.

All of a program's globals together, those of every package included,
hold at most 2^30 bytes. The global that takes them past it is an error,
`global c makes this program's globals hold 1610612736 bytes, more than
the 2^30 a program's globals may hold`. Globals are counted package by
package, every package after the ones it imports and the main package
last, each package's files in the byte order of their names, and in
source order within a file.

```tack error
package main

type Huge struct {
    bytes [2147483649]uint8
}

func main() {
    exit(0)
}
```

```diagnostics
main.tck:4:11: error: array type [2147483649]uint8 is too large: it holds 2147483649 bytes, more than the 2^31 a value may hold
```

## Type identity and assignability

Two types are the same type when:

- they are the same builtin type;
- they are the same declared struct or union: each declaration is a type
  of its own;
- they are arrays of the same length and the same element type;
- they are views, lists, or maps of the same element, key and value
  types;
- they are function types with the same parameter types, each with the
  same `mut`, `own` or `scope` marker, and the same result types.

A value of type `V` may be used where a value of type `T` is expected,
in an assignment, an argument, a result, a field or element of a
literal, or an operand of an arithmetic or bitwise operator, when `V`
and `T` are the same type, or when both are integer types and `V`
widens to `T` (see [Implicit widening](#implicit-widening)). An untyped
constant may be used as any type of which it is a value (see [Types of
constants](03-arithmetic.md#types-of-constants)). Nothing else changes
type without a conversion: `[4]int32` is not a `[4]int64`, a `[]int32`
is not a `[]int64`, and a function of one signature is not a value of a
function type of another. Passing an array, a list or a string's bytes
to a view parameter is described in [Ownership and
memory](08-ownership.md).

## Implicit widening

An integer type widens to another when every value of the first is a
value of the second:

- a signed type widens to a wider signed type;
- an unsigned type widens to a wider type, signed or unsigned;
- `uint8`, `uint16` and `uint32` widen to `unsafe_size_t` and to
  `unsafe_uintptr_t`, and each of those widens to `uint64`.

No signed type widens to an unsigned one, and the two target-width types
do not widen to each other, nor to or from `int64`. Widening applies to
integers alone: no float widens, and no array, view, list or struct of
integers widens to one of wider integers.

```tack run exit=0
package main

func total(a int64, b uint64) uint64 {
    if a < 0 || b > 9223372036854775807 {
        return b
    }
    return b + uint64(a)
}

func main() {
    var small int8 = -3
    var byte uint8 = 200
    var count uint32 = 7
    var wide int64 = small
    var half int16 = byte
    var size unsafe_size_t = count
    var big uint64 = size
    if wide != -3 || half != 200 || big != 7 || total(wide, big) != 7 {
        exit(1)
    }
    exit(0)
}
```

Any other change of integer type is an error:

```tack error
package main

func main() {
    var s int8 = -1
    var u uint8 = s
    var n int64 = 4
    var z unsafe_size_t = n
    var w unsafe_size_t = 1
    var p unsafe_uintptr_t = w
    exit(int32(u) + int32(z) + int32(p))
}
```

```diagnostics
main.tck:5:19: error: cannot use int8 as uint8 in declaration of u
main.tck:7:27: error: cannot use int64 as unsafe_size_t in declaration of z
main.tck:9:30: error: cannot use unsafe_size_t as unsafe_uintptr_t in declaration of p
```

## Comparable and ordered types

A type is comparable when `==` and `!=` are defined on two of its values,
and ordered when `<`, `<=`, `>` and `>=` are too. Both operands of a
comparison have one type, after widening, except that two integers of
any types compare by value (see [Comparisons](03-arithmetic.md#comparisons)).

| Type                      | `==` `!=`                          | Ordered |
|---------------------------|------------------------------------|---------|
| integer types             | yes, by value                      | yes     |
| float types               | yes, as IEEE 754 says              | yes     |
| `bool`                    | yes                                | no      |
| `string`                  | yes, byte by byte                  | yes     |
| `error`                   | yes (see [Errors](09-errors.md))   | no      |
| arrays                    | when the element type is           | no      |
| structs                   | when every field's type is         | no      |
| unions                    | when every payload's type is       | no      |
| lists                     | when the element type is           | no      |
| ids, spans and handles    | yes                                | no      |
| views, maps, `any`        | no                                 | no      |
| arenas and pools          | no                                 | no      |
| `Allocator`               | no                                 | no      |
| function types            | no                                 | no      |

An operator a type lacks is an error, such as `operator == is not
defined on []int64`, or, for an ordering of two structs, `operator < is
not defined on Point`.

```tack error
package main

type Point struct {
    x int64
}

func before(a Point, b Point) bool {
    return a < b
}

func same(a []int64, b []int64) bool {
    return a == b
}

func main() {
    exit(0)
}
```

```diagnostics
main.tck:8:12: error: operator < is not defined on Point
main.tck:12:12: error: operator == is not defined on []int64
```

A value is copyable when assigning, passing or returning it copies it,
and owned when doing so moves it (see [Ownership and
memory](08-ownership.md)). The owned types are lists, maps, arenas, pools, and
structs and unions that hold one; every other type is copyable.
