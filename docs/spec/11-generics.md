# 11. Generics and interfaces

This chapter describes interface declarations, how a struct or union
type satisfies an interface (its methods, signatures and `mut`
receivers, and their visibility from the call), generic functions with
type parameters constrained by interfaces, interface parameters, how a
call chooses its type arguments, and what a generic body may do with a
value of a type parameter. Only a function is generic: a method, an
`extern func` and a type cannot have type parameters, as "The language
today" in the [README](../../README.md) says under generic functions.
Methods themselves are declared as in [Declarations and
scope](05-declarations.md), and `any`, which is not an interface,
belongs in [Types](04-types.md).

## Interface declarations

An interface declaration names a set of methods:

```tack check
package sink

pub type Writer interface {
    mut write(b []uint8) error
    size() int64
}
```

It is written at package level only, as a struct is, with one method
per line or methods separated by `;`. Each method is written as a
function's signature without `func`, a receiver or a body, with `mut`
first for a method that takes its receiver as `mut`. A method's
parameters and results follow a function's rules: a `mut`, a view, a
variadic or an `any` parameter may appear, and an error result is last.
Its parameters are named, no two alike and none `_` or a type's name;
since no body can see them, they may otherwise share a name with
anything. A method's name is in its interface's namespace, follows a
field's naming rule, and is declared once in it.

An interface holds methods only. It has no fields, `an interface cannot
have fields; it holds only methods`, embeds no other interface,
`embedded interfaces are not supported`, and gives no method a body,
`interface method write cannot have a body`. A method has no visibility
of its own: it is as visible as its interface, and `pub` or `lib` on a
method is `an interface's method cannot be pub; it is as visible as its
interface`. The interface itself is `pub`, `lib` or private as a struct
is, and exposes the types its methods name as a function does, `pub
interface Maker exposes secret, which is not pub`.

`interface {}` asks for nothing.

An interface is not a type a value can have. There are no interface
values: an interface only constrains a type parameter. Written as the
whole type of a function's parameter it is shorthand for a type
parameter (see [Interface parameters](#interface-parameters)); written
anywhere else a type goes, a local's, a result's, a field's, a
global's, an element's, a method's or an extern func's parameter's, a
variadic parameter's elements, a function type's parameter, or in a
literal, it is an error:

```tack error
package main

type Sizer interface {
    size() int64
}

type Pair struct {
    a Sizer
}

func main() {
}
```

```diagnostics
main.tck:8:7: error: Sizer is an interface; it can only constrain a type parameter
```

No method can be declared on an interface, `cannot declare a method on
Writer: only a struct or union type declared in this package can have
methods`, and an interface cannot be called, `cannot call Writer; it is
a type`. Nothing of an interface exists at run time.

## Satisfying an interface

A type satisfies an interface when, for each of the interface's
methods, the type has a method of the same name that:

- takes its receiver as `mut` exactly when the interface's method does;
- has the same parameters, each of the same type and alike in being
  `mut`, `own` or variadic, so `write(b ...uint8)` does not satisfy
  `write(b []uint8)`, though the parameter is a view in both;
- has the same results.

Nothing is written to say that a type satisfies an interface. Only a
struct or union type has methods, so any other type satisfies only an
interface that asks for none. Every type that may be a type argument
(see [Type arguments and inference](#type-arguments-and-inference))
satisfies `interface {}`.

A type that does not satisfy an interface is reported for the first
method, in the interface's order, that it fails:

```tack error
package main

type Sizer interface {
    size() int64
}

type Box struct {
    n int64
}

func (mut b Box) size() int64 {
    return b.n
}

func measure[S Sizer](s S) int64 {
    return s.size()
}

func main() {
    b := Box{n: 2}
    if measure(b) != 2 || measure(7) != 7 {
        exit(1)
    }
}
```

```diagnostics
main.tck:21:16: error: Box does not satisfy Sizer: Box.size takes its receiver as mut, and Sizer's size does not
main.tck:21:35: error: int64 does not satisfy Sizer: int64 has no method size
```

The other reasons read `X has no method m` and `X.m is func(int32), and
I's m is func(int64)`.

Each method the interface asks for must also be visible where the type
is chosen: from the package holding the call that binds a type
parameter to the type, as if that code called the method itself. A
private method is visible within its own package, a `lib` one within
its module, and a `pub` one anywhere. The interface's package plays no
part, so a `main` package may pass a type of its own, with private
methods, to another package's generic function, while a package cannot
pass a type whose method it could not call. The error names the
method's visibility and marks the type with `here`, since the method
may be visible from another package:

```text
words.Bag does not satisfy sink.Sink here: put is not pub in words.Bag
util.Pair does not satisfy util.Counted here: count is lib in module std and cannot be used from module calc
```

A type parameter that is passed on as a type argument (see [Type
arguments and inference](#type-arguments-and-inference)) satisfies an
interface when its own constraint has each method the interface asks
for, alike in the same way: `S does not satisfy Writer: its constraint
Sizer has no method write`. No visibility is checked for it: the call
that bound a struct or union type checked every method of its
constraint, and no generic function it is passed on to can ask for
another.

## Generic functions

A generic function declares type parameters in brackets after its name,
each a name and its constraint:

```tack fragment
func render[W Writer](mut w W, s string) error
func pair[A Writer, B sink.Sink](mut a A, mut b B)
```

The constraint is an interface, plain or qualified. Type parameters are
separated as parameters are: the commas between them are optional and a
trailing one is allowed. A type parameter is a type within its function,
its signature and its body, and stands for whichever type a call passes.

- Every type parameter has a constraint: `[W]` is `type parameter W has
  no constraint; write the interface it must satisfy after it, as in [W
  Writer]`.
- The constraint is an interface. Anything else, a struct, a basic type,
  `any`, a view or another type parameter, is `Point is not an
  interface; the constraint of type parameter W must be one`, and a name
  of nothing is `unknown type: X`.
- `[]` is `empty type parameter list`.
- A type parameter's name follows a parameter's rules: not reserved, not
  a package-level name or an import's, `duplicate type parameter name`
  for two alike, and no parameter or local may reuse it.
- Every type parameter appears in some parameter's type, so a call can
  infer it: whole, `w W`, or as a part of it, a list's, a view's or a map
  value's element, or a function type's parameter or result, `l
  list[T]`, `v []T`, `m map[string]T`, `less func(T, T) bool`. Otherwise
  it is `W appears in no parameter of f, so no call can infer it`.
- A type parameter is a type, never a value: `cannot use W as a value;
  it is a type`, `cannot call W; it is a type`, `cannot assign to W; it
  is a type`.
- A method cannot have type parameters, `a method cannot have type
  parameters; only a function can be generic`, nor can an extern func,
  `extern func f cannot have type parameters; its body is written in C`.
  There are no generic types.
- A `pub` or `lib` generic function exposes its constraints as it
  exposes its parameters' types, `pub function Size exposes hidden,
  which is not pub`.

A generic function is never a value: `cannot use measure as a value: it
is generic, and a generic function is only called, its type arguments
inferred from the call`. It may call itself and other generic
functions, passing its type parameters as type arguments.

The body of a generic function is checked once, against the
constraints, never for each call, so the diagnostics a program gets
never depend on which types its calls pass. The function is compiled
once for each set of type arguments the program's calls need, an
instance. A program that needs more than 10000 instances is an error
at the call that would add another: `instantiating g with T6, T1, ...
makes more than 10000 instances of generic functions, the most a
program may have`.

This program passes two values of one struct type to a generic
function:

```tack run exit=5
package main

type Sizer interface {
    size() int32
}

type Box struct {
    n int32
}

func (b Box) size() int32 {
    return b.n
}

func total[S Sizer](a S, b S) int64 {
    return int64(a.size()) + int64(b.size())
}

func main() {
    a := Box{n: 2}
    b := Box{n: 3}
    if total(a, b) == 5 {
        exit(5)
    }
}
```

## Interface parameters

An interface written as the whole type of a function's parameter
declares an implicit type parameter of that function, constrained by
the interface. So

```tack fragment
func emit(mut w Writer, s string)
```

is

```tack fragment
func emit[W Writer](mut w W, s string)
```

Each such parameter is a type parameter of its own, so `both(mut a
Writer, mut b Writer)` may be passed two different types, and the
function is generic in every way: it cannot be used as a value, `cannot
use emit as a value: it is generic, ...`, and its body is checked as
any generic body is. Only a function's parameter declares one; an
interface as a method's or an extern func's parameter type is an error,
as in [Interface declarations](#interface-declarations).

```tack run exit=7
package main

type Sizer interface {
    size() int32
}

type Box struct {
    n int32
}

func (b Box) size() int32 {
    return b.n
}

type Line struct {
    a int16
    b int16
}

func (l Line) size() int32 {
    return int32(l.b) - int32(l.a)
}

func both(x Sizer, y Sizer) int64 {
    return int64(x.size()) + int64(y.size())
}

func main() {
    if both(Box{n: 3}, Line{a: 2, b: 6}) == 7 {
        exit(7)
    }
}
```

## Type arguments and inference

A call to a generic function is written as any call, `render(mut f,
"x")`. Type arguments are never written: `f[T](x)` is `cannot write
type arguments; they are inferred from the arguments, as in f(...)`.

Each type parameter takes its type from the arguments, in order:

- the type of an argument passed to a parameter of that type;
- or the part of an argument's type where the parameter's type has the
  type parameter: the element of a list, an array or a view passed for a
  `list[T]` or a `[]T`, the value type of a map whose key type is the
  parameter's for a `map[K]T`, or a parameter's or a result's type of a
  function value, or of a closure, passed for a function type, scope
  marks included. The argument's shape must
  match: `cannot use list[int64] as map[string]T in argument to size`.

An untyped constant passed for a type parameter is an `int64`.

A type argument may be any type except:

- a view, `cannot use []int64 as T in argument to keep: a type argument
  cannot be a view`;
- an `any`, `cannot use any as T in argument to one: a type argument
  cannot be an any`;
- a type made of a type parameter, `cannot use list[T] as U in argument
  to count: a type argument cannot be made of a type parameter, only be
  one`. A type parameter is passed on only whole.

Two arguments for one type parameter must be of one type, reported at
the later one: `cannot use Buffer as W in argument to both: W is Counter
in this call, from an earlier argument`. The type argument must satisfy
the constraint, as in [Satisfying an
interface](#satisfying-an-interface). The arguments are otherwise
checked as for any function: a `mut` parameter of a type parameter
takes a `mut` place, `render takes w as mut; pass mut c`.

## What a generic body may do

A value of a type parameter has its constraint's methods, called as a
struct's are, `w.size()` and `mut w.write(b)`, under every rule of a
method call: `W.write takes its receiver as mut; call mut w.write(b)`,
`W has no method nope`. A constraint's method named without a call is
`size is a method of W, not a field; a method can only be called`.

It has nothing else:

- no field, `W has no field n: a type parameter has only its
  constraint's methods`;
- no operator, `operator == is not defined on W`;
- no conversion, no `len` and no range;
- no literal, `W cannot be written as a literal; only arrays and structs
  can`;
- it cannot be passed as `any`, `cannot pass W as any: only the builtin
  types and views of them can be passed as any for now`.

A value of a type parameter is owned as a list is, whatever a call makes
the type parameter, since a struct holding a list may be passed for it
(see [Ownership and memory](08-ownership.md)). So it moves rather than
copies; a parameter of one borrows its argument and cannot be moved,
`cannot move w: it is a parameter, which borrows its caller's value`; a
moved local cannot be used; a temporary must be stored, `cannot use a
temporary S here; store it in a variable first`; it has no zero value,
`z needs a value: S has no zero value`; and a result of one cannot be
dropped, `stuck returns S, which may hold a list and cannot be dropped;
bind it to a variable`. An instance whose type argument is `int64`
follows the same rules; there a move is a copy.

A type parameter cannot be the element of an array or a variadic
parameter, `a variadic parameter cannot hold S: its elements are
copied, and S, a type parameter, which may hold a list, cannot be`, nor
a map's key, `cannot use T as a map key: a map's key cannot be a type
parameter`. A list, a view, a map's value and a function type may be
made of one, in a signature and in a body, `make(list[T], alloc)`
included. Their elements are owned: read in place, moved in by `append`
and `put`, moved out by `pop`, `remove` and `delete`, reordered by
`swap`, and never copied out, `cannot move l[0] out of l: an element
cannot be moved out of a list; take it out with pop or remove`.

A generic function gets a value of a type parameter by moving one out
of a list with `pop` or `remove`, or out of a map with `delete`, by
calling a function value or a function that returns one,
or by taking one as an `own` parameter, and may return it. Here the
type argument is `int64`, which satisfies the empty interface `Any`:

```tack run exit=3
package main

type Any interface {}

func take[T Any](mut l list[T], mk func() T) T {
    if len(l) > 0 {
        return pop(mut l)
    }
    return mk()
}

func seven() int64 {
    return 7
}

func main() {
    mut l := make(list[int64], c_allocator)
    x := take(mut l, seven)
    err := append(mut l, 4)
    if err.code != 0 {
        exit(1)
    }
    y := take(mut l, seven)
    if x == 7 && y == 4 {
        exit(3)
    }
}
```
