# 6. Expressions

This chapter describes every form of expression, its syntax, its type
and the value it yields: operands and names, qualified names, literals of
composite types (arrays, structs, unions), selectors, method calls,
indexing and slicing, calls and their arguments, function values,
operators and their precedence, logical operators and short-circuiting,
type assertions, and the order in which operands are evaluated. What an
arithmetic operator computes belongs in [Constants and
arithmetic](03-arithmetic.md); when an index, slice or division is
proven safe, and the checked forms `v, ok := ...`, belong in [Safety and
proofs](10-safety.md); what passing `mut` or `own` does to the argument
belongs in [Ownership and memory](08-ownership.md).

## Operands

An operand is one of:

- a literal: an integer, float, character, string or raw string literal
  ([Source and lexical elements](02-lexical.md));
- one of the keywords `true`, `false`, `noerror` and `c_allocator`;
- the name of a local variable, a parameter or a package-level
  variable, which gives its value;
- the name of a function, which gives the function as a value
  ([Function values](#function-values));
- a method named through its type, `Pair.first`, which gives the method
  as a value ([Method expressions](#method-expressions));
- a qualified name ([Qualified names](#qualified-names));
- a composite literal ([Composite literals](#composite-literals));
- an expression in parentheses, `(x)`, whose value is `x`'s.

What a name refers to is decided by the scope rules of [Declarations and
scope](05-declarations.md). A name that refers to nothing is `undefined:
z`. A type's name is not an operand, `cannot use Point as a value; it is
a type`, outside a conversion `T(x)`, a union's construction
`Expr.int(5)` and a method expression `Pair.first`; an import's name is not one either, `fmt is a package,
not a value`, outside a qualified name. `_` is not an operand.

There is no unary `+`: `+x` is a syntax error.

## Qualified names

A qualified name, `pkg.Name`, names the package-level name `Name` of the
package imported as `pkg` in the file ([Packages, modules and
programs](12-packages.md)). It may name a function, `lexer.First`, which
is called or is a value as an unqualified function's name is; a global,
`lexer.KindNumber`, which is read, and may be assigned under the rules
for any global, though a mutable global is never visible to another
package; or a type, `lexer.Token`, written wherever a type may be, and
in a struct literal, `lexer.Token{kind: 1}`, or a union's construction,
`ast.Expr.int(5)`. A qualified name that names nothing is
`undefined: lexer.Nothing`, and one the importing package cannot see is
an error under the visibility rules of [Declarations and
scope](05-declarations.md).

```tack run exit=0
package main

import fmt from "std/fmt"

func main() {
    fmt.println("hi")
}
```

```output
hi
```

## Composite literals

A composite literal builds a value of an array or a struct type from
its parts. Unions, lists and maps have no literals: a union is built
from one of its cases, below, a list or a map by `make`
([Builtins](13-builtins.md)), and `Expr{}` is `Expr cannot be written as
a literal; only arrays and structs can`.

An array literal, `[3]int64{1, 2, 3}`, gives the array's type and lists
its elements in order, exactly one for each element of the array.
`[...]int64{1, 2, 3}` takes the array's length from the number of its
elements. Elements have no keys: `[3]int64{0: 1}` is an error.

A struct literal, `Point{x: 1, y: 2}`, names every field of the struct
exactly once, each with its value, in any order. A literal without field
names, `Point{1, 2}`, a field listed twice, a field the struct does not
have, a field name in parentheses and a field left out are each an
error. A struct with a field the literal's package cannot see cannot be
written as a literal there ([Declarations and
scope](05-declarations.md)).

Each element's or field's value is used as a value of the element's or
field's type, under the assignability rules of [Types](04-types.md): an
untyped constant takes that type and must fit it, and a narrower integer
widens to it. Inside an array literal, an element that is itself an
array or struct literal may leave out its type, `[2][2]bool{{true,
false}, {false, true}}` and `[2]Point{{x: 1, y: 2}, {x: 3, y: 4}}`; a
field's value may not.

In the header of an `if`, a `for`, a range clause or a `switch`, a
struct literal must be in parentheses, `if p == (Point{x: 1, y: 2}) {`,
since its brace would otherwise open the body. Inside a call, an index
or another literal it needs none.

```tack run exit=10
package main

type Point struct {
    x int8
    y int8
}

func main() {
    ps := [...]Point{{x: 1, y: 2}, {y: 4, x: 3}}
    if ps[1] == (Point{x: 3, y: 4}) {
        exit(int32(ps[0].x) + int32(ps[0].y) + int32(ps[1].x) + int32(ps[1].y))
    }
}
```

A union value is built from one of its cases, named through the union's
type: `Expr.int(5)` builds the case `int` with the payload 5, and
`Expr.eof` the case `eof`, which has no payload. The payload is used as
an argument to a parameter of the case's type: an untyped constant
takes the type, a narrower integer widens to it, and exactly one payload
is given. A case with no payload is named without parentheses:
`Expr.eof()` is `Expr.eof has no payload; write Expr.eof`. A case with
one is not named alone, `Expr.int needs its int64 payload, as in
Expr.int(...)`, and a name the union has as neither a case nor a method
is `Expr has no case or method nope`. A case and a method of one union
never share a name, so any other name written through a union is a
method expression ([Method expressions](#method-expressions)).
Nothing makes a union of a payload implicitly. A union of another
package is built through its qualified name, `ast.Expr.int(5)`.

## Selectors

A selector `x.f` reads the field `f` of `x`, a value of a struct type: a
variable, a parameter, an element, another field or a call's result,
`f().x`. An `error` is a struct of the fields `code` and `msg`
([Errors](09-errors.md)). A selector of a field is a place when `x` is
one, so a field may be assigned when its variable is mutable
([Statements](07-statements.md)). Selecting a field the struct does not
have, or any field of a union, `Expr has no field int`, is an error, as
is selecting a field another package cannot see.

A selector followed by arguments, `x.m(args)`, calls the method `m` of
`x`'s type, found from that type, whatever its package. `x` may be any
value of the type: a variable, a parameter, a field, `w.out.len()`, an
element, `rows[i].len()`, or a temporary, `makePoint(3, 4).norm()`. A
type with no such method is `Builder has no method nope`.

A method whose receiver is `mut` is called with `mut` before the
selector, `mut b.write(s)`, as a statement or as a value, `err := mut
b.write(s)`. The `mut` covers what comes before the last `.name(`, so
`mut w.out.write(s)` passes `w.out` as the receiver. The receiver is then
a `mut` argument of the call, under every rule of one, below. Leaving
`mut` out for a `mut` receiver is `Builder.write takes its receiver as
mut; call mut b.write(s)`, and writing it for a plain receiver is
`Builder.len does not take its receiver as mut; call b.len()`.

A method is named as a value only through its type, `Builder.write`
([Method expressions](#method-expressions)). There are no bound method
values: `b.write` without a call is `write is a method of Builder, not a
field; a method can only be called`. A field called as a method is `n is a field of Builder,
not a method; a field cannot be called`, and a field holding a function
value is called through a local, `run is a field of Op holding a
function value, not a method; call it through a local, as in f :=
op.run, then f(...)`. The builtins are functions, not methods: there is
no `l.len()`.

## Index expressions

An index expression `x[i]` reads one element of `x`:

| `x` is         | `i` is               | `x[i]` is                              |
|----------------|----------------------|----------------------------------------|
| an array `[n]T` | an integer of any type | the element at `i`, a `T`            |
| a string       | an integer of any type | the byte at `i`, a `uint8`            |
| a view `[]T`   | an integer of any type | the element at `i`, a `T`             |
| a list `list[T]` | an integer of any type | the element at `i`, a `T`           |
| a map `map[K]V` | a value of type `K`  | the value under the key `i`, a `V`     |

Elements are numbered from 0. Anything else indexed is an error. An
index into an array, a string, a view or a list compiles only where the
compiler proves it in range, and a map's key only where it proves the
key is in the map; elsewhere the checked form `v, ok := x[i]` reads it.
Both are described in [Safety and proofs](10-safety.md). An index
expression of an array, a list, or a map's value is a place when `x` is
one; a string's byte is never a place, since strings are immutable, and
a view's element is not one either, since a view is read-only.

A constant index into a known string is computed at compile time, so
`"tack"[1]` is 97.

```tack run exit=7
package main

func main() {
    a := [3]int32{5, 6, 7}
    s := "tack"
    if s[1] == 'a' {
        exit(a[2])
    }
}
```

## Slice expressions

A slice expression `x[i:j]` is the bytes of the string `x`, or the
elements of the array, view or list `x`, from index `i` up to but not
including `j`. `x[:j]` starts at 0, `x[i:]` runs to `len(x)`, and `x[:]`
is all of it. There is no three-index form: `x[i:j:k]` is `a slice has
at most two bounds, x[i:j]; there is no three-index form`. A bound is
an integer of any type, `slice bound must be an integer, got bool`, and
anything else sliced is `cannot slice int64`.

A string's slice is a `string` that shares the bytes it was cut from,
never copying them, and is a value like any other string. A string
slices by bytes, so a slice may cut through a multi-byte UTF-8 sequence.
An array's, a view's or a list's slice is a view, `[]T` of its element
type, under the rules for views of [Ownership and
memory](08-ownership.md): it may be passed to a view parameter, spread
into a variadic one, given to `len`, sliced again, or returned from a
function returning a view, and nothing else. An array or a list that is
sliced must be a place: `cannot slice a temporary array; store it in a
variable first`. A slice is never a place: `cannot assign to s[0:1]: a
slice is not a place`.

The bounds must be proven, as an index is, or a string's slice bound
with ok, `t, ok := s[i:j]`; [Safety and proofs](10-safety.md) describes
both.

```tack run exit=3
package main

func count(v []int64) int64 {
    return len(v)
}

func main() {
    a := [5]int64{1, 2, 3, 4, 5}
    s := "abcdef"[1:4]
    if count(a[1:4]) == 3 && s == "bcd" {
        exit(3)
    }
}
```

## Calls

A call `f(args)` calls a function with a list of arguments, separated by
commas, with no comma after the last. `f` is the name of a function or a
builtin, a qualified name, a local variable or a parameter holding a
function value, or a selector naming a method. Calling anything else is
`cannot call n: it is int64, not a function`. The same form, with a type
in place of the function, is a conversion `T(x)`, which [Constants and
arithmetic](03-arithmetic.md) describes for numbers and [Ownership and
memory](08-ownership.md) for `[]uint8(s)`, and with a union's case, a
union's construction (above).

A call passes one argument for each parameter, and for a variadic
parameter, the last, any number of values, or a view spread whole with
`...` after the last argument. The wrong number is `f expects 2
argument(s), got 3`, or for a variadic function `f expects at least 1
argument(s), got 0`. Each argument is used as a value of its parameter's
type under the assignability rules of [Types](04-types.md), or it is an
error, `cannot use string as int64 in argument to f`.

An argument to a `mut` parameter is written `mut x`, and `x` must be a
place the caller may change. An argument to an `own` parameter that is a
variable is written `own x`. `mut` and `own` are written only there:
leaving `mut` out is `scale takes p as mut; pass mut b`, or, when `b`
could not be passed as `mut` either, says why and what fixes it, `scale
takes p as mut, but b is immutable; declare it with mut, then pass mut
b`; writing it for a plain parameter is `count does not take t as mut;
pass t`, and `mut x` anywhere but as a call's argument is `mut x can
only be passed to a mut parameter`; the same holds for `own`. What such
an argument may be and what passing it does is described in [Ownership
and memory](08-ownership.md). A spread, `f(v...)`, is the whole of the
variadic parameter: `cannot use ... in a call with other values for
nums`, and in a call to a function that is not variadic `cannot use ...
in a call to non-variadic f`.

A call's value is its function's result. A call of a function with no
result has no value, and is a statement only. A call of a function with
several results is not a value: it is bound by a declaration or an
assignment of as many names, `q, r := divmod(7, 2)`, or made as a
statement ([Statements](07-statements.md)); used as one value, passed
to another call or returned whole, it is an error, `two returns 2
values`. A call returning a view gives a view that may be used only
inside the expression that consumes it ([Ownership and
memory](08-ownership.md)).

A call by a bare name calls the function or builtin of that name before
any local of the name ([Function values](#function-values)).

```tack error
package main

func id(n int32) int32 {
    return n
}

func main() {
    n := 3
    exit(n(2) + id(1, 2))
}
```

```diagnostics
main.tck:9:10: error: cannot call n: it is int64, not a function
main.tck:9:17: error: id expects 1 argument(s), got 2
```

## Function values

A function's name used anywhere but as a call's callee, `double` or
`bytes.equal`, is a value of its function type, written as a signature
is, with the parameters' types alone, `mut` and `own` before the
parameters that have them, and the results: `func(int64) int64`,
`func([]uint8, mut list[int64]) (int64, error)`, `func()`. Two functions
of one signature have one type; nothing else is assignable to it. A
function value is called through a local or a parameter holding it,
`f(x)`, under every rule a call by name has for its arguments.

A method is a value named through its type ([Method
expressions](#method-expressions)). A generic function, an extern func
and a builtin are not values: `cannot use measure as a value: it is generic, and a generic
function is only called, its type arguments inferred from the call`,
`cannot use now as a value: it is an extern func, and a function value
does not cross to C`, `cannot use the builtin len as a value; only a
declared function can be one`. Nor is a function whose signature no
function type can have: one with a variadic parameter, `cannot use sum
as a value: its parameter xs is variadic, which no function type's can
be`, one with an `any` parameter, `cannot use show as a value: its
parameter v is an any, which no function type's can be`, and one that
returns a view, `cannot use head as a value: it returns a view, which
no function type can`. A function literal is a closure, described
below. A call by a bare name calls the function or builtin of that
name, never a local holding a function value under that name, so such a
local is `step calls the function step, not the function value of that
name; rename one of them` where it is called. What a function type
holds, and that it has no zero value and no `==`, is described in
[Types](04-types.md).

```tack run exit=8
package main

func half(n int32) int32 {
    return n / 2
}

func twice(f func(int32) int32, n int32) int32 {
    return f(f(n))
}

func main() {
    g := half
    exit(twice(g, 32))
}
```

## Method expressions

A method named through its type, `T.m`, or through its package and its
type, `pkg.T.m`, is a method expression: the method as a function value
whose first parameter is its receiver. `func (p Pair) has(x int64) bool`
gives a `func(Pair, int64) bool`; a `mut` receiver gives a `mut` first
parameter, `func (mut b Builder) write(s string) error` a `func(mut
Builder, string) error`; and every other parameter keeps its marks,
`scope` included. A method and a function of one signature are values of
one type, and a method expression is used wherever a function value is
([Function values](#function-values)): assigned, passed, to a `scope`
parameter too, returned, held by a field or an element, and called
through a local or a parameter under every rule of a call through a
value. It has no `==`, and is no place: `mut Pair.first` is `only a
variable, a field or an element can be passed as mut`, and `Pair.first =
f` is `cannot assign to Pair; it is a type`.

Called directly, `Pair.has(p, 3)` or `Builder.write(mut b, s)`, a method
expression calls the method with its receiver passed first, as
`p.has(3)` does, under every rule of a call by name: `Box.set takes b as
mut; pass mut b`, `Box.size does not take b as mut; pass b`, `P.norm
expects 1 argument(s), got 0`, and the method's contract holds after
the call ([Declarations](05-declarations.md)). A call through a value
holding the method gives no contract, as a function type states none.

The type is a struct or a union type. A name it has no method of is
`Box has no method nope`, a union's `Expr has no case or method nope`,
and a field named through its type is `n is a field of Box, not a
method; only a method can be named through its type`. A builtin type has
no methods, `int64 has no method abs`. An interface's methods are not
values, `cannot use Sizer.size as a value: Sizer is an interface; only a
struct or union type's methods are values`, nor a type parameter's,
which are only called on a value of it, `cannot use W.size as a value: W
is a type parameter, and a type parameter's methods can only be called on a value of it`.
A method whose signature no function type can have is no value, as for
a function: `cannot use Box.all as a value: its parameter xs is
variadic, which no function type's can be`.

Naming a method of another package's type follows the method's
visibility, capped by its type's: `hidden is not pub in lexer.Token`,
`Sum is lib in module std and cannot be used from module calc`; a type
the package cannot see is not named at all, `secret is not pub in
package calc/lexer`. The rule is on the name, not on the value: a `pub`
function may return a private method's value to another package.

```tack run exit=7
package main

type Pair struct {
    a int32
    b int32
}

func (p Pair) first() int32 {
    return p.a
}

func (mut p Pair) swap() {
    t := p.a
    p.a = p.b
    p.b = t
}

func second(p Pair) int32 {
    return p.b
}

func main() {
    mut p := Pair{a: 3, b: 7}
    mut get := Pair.first
    sw := Pair.swap
    sw(mut p)
    if get(p) == 7 && Pair.first(p) == 7 {
        get = second
        exit(get(p) +% 4)
    }
}
```

A method expression is checked as any call or function value is:

```tack error
package main

type Box struct {
    n int32
}

func (mut b Box) set(x int32) {
    b.n = x
}

func main() {
    mut b := Box{n: 0}
    f := Box.set
    f(b, 1)
    Box.set(b, 2)
    exit(Box.nope(b))
}
```

```diagnostics
main.tck:14:7: error: f takes argument 1 as mut; pass mut b
main.tck:15:13: error: Box.set takes b as mut; pass mut b
main.tck:16:14: error: Box has no method nope
```

## Function literals

A function literal is written as a function declaration is, without a
name: `func(a int64, b int64) bool { return a < b }`. It is a closure:
its body may name the variables and parameters of the functions and
closures it is written in, its captures, and reads and writes them by
reference, never as a copy. Its parameters follow a function's, `mut`,
`own` and `scope` ones included, and are declared in its body, so none
may reuse a name visible where it is written; its signature must be one
a function type can say, `this closure has no function type: its
parameter xs is variadic, which no function type's can be`. A `return`
in it returns from the closure with the closure's results, one with
results must end in a terminating statement, `the closure must end in a
return`, a `?` in it passes an error on as the closure's, a `defer` in
it runs when the closure returns, and no `break`, `continue` or
`fallthrough` leaves it.

A closure can only be an argument to a parameter marked `scope`; anywhere
else it is `a closure can only be passed to a scope parameter`, or, for
a parameter that is not `scope`, `run does not take f as scope, and a
closure can only be passed to a scope parameter`, and called where it is
written, `a closure cannot be called where it is written; it can only be
passed to a scope parameter`. Its type, which its signature gives, must
be the parameter's exactly, `cannot use func(int64) bool as func() in
argument to call`; in a call to a generic function it gives the type
arguments it mentions as any argument does ([Generics and
interfaces](11-generics.md)). A `scope` parameter is used only as a
call's callee or as an argument to another `scope` parameter, `f is a
scope parameter, which can only be called or passed to another scope
parameter`, so no value a closure makes outlives the call it is passed
to. A plain function value may be passed to a `scope` parameter too. A
closure runs only when the function it is passed to calls it, as many
times as it does, and each run reads its captures as they are then;
what the call's rules say of its captures is in [Ownership and
memory](08-ownership.md#closures), and what a proof may assume in it in
[Safety and proofs](10-safety.md).

```tack run exit=14
package main

func each(v []int32, scope f func(int32)) {
    for _, x := range v {
        f(x)
    }
}

func main() {
    v := [4]int32{3, 4, 5, 9}
    limit := 4
    mut sum int32 = 0
    each(v, func(x int32) {
        if x > limit {
            sum = sum +% x
        }
    })
    exit(sum)
}
```

## Operators and precedence

Tack has these operators:

| Kind       | Operators                               |
|------------|-----------------------------------------|
| postfix    | `?`                                     |
| prefix     | `!` `-` `^` `-%`                        |
| arithmetic | `+` `-` `*` `/` `%`                     |
| wrapping   | `+%` `-%` `*%`                          |
| bitwise    | `&` `\|` `^` `&^` `<<` `>>`             |
| comparison | `==` `!=` `<` `<=` `>` `>=`             |
| logical    | `&&` `\|\|`                             |

The postfix `?` binds tightest, as tightly as a selector or an index
written after it, so `f()?.b` is the field `b` of what `f()?` gives,
`f()?[2]` its element, `f()?.m()?` a `?` on a method call of it, and
`-f()?` negates it (see [The ? operator](#the--operator)). A call's
parentheses cannot follow a `?` directly, `fv()?("x")`, as they cannot
follow a call, `g()("x")`.
The prefix operators bind next. The binary operators have five
levels of precedence, from tightest to loosest:

| Precedence | Operators                           |
|------------|-------------------------------------|
| 5          | `*` `/` `%` `*%` `<<` `>>` `&` `&^` |
| 4          | `+` `-` `+%` `-%` `\|` `^`          |
| 3          | `==` `!=` `<` `<=` `>` `>=`         |
| 2          | `&&`                                |
| 1          | `\|\|`                              |

Binary operators of one level associate to the left, so `1 + 2 << 3` is
17, `6 & 3 * 2` is 4 and `a - b - c` is `(a - b) - c`. Parentheses group
explicitly.

`-x` negates a signed integer or a float, `-%x` negates an integer of
any type, wrapping, and `^x` flips every bit of an integer; `!x` is
described below. The arithmetic, wrapping and bitwise operators take two
integers, or for `+ - * /` two floats, and give their common type; what
each computes, which operand types it accepts, and the shifts' rules for
their count are described in [Constants and
arithmetic](03-arithmetic.md). `+`, `-`, `*` and `-x` on integers must
be proven not to overflow, or be written in their checked form, `v, ok
:= a + b`; `+%`, `-%`, `*%` and `-%x` wrap at the type's width instead
(see [Integer overflow](03-arithmetic.md#integer-overflow)). `+%` and
`-%` bind as `+` and `-` do, `*%` as `*`, and `-%x` as `-x`. A comparison gives a `bool`. Two
integers of any types compare by their values, as [Constants and
arithmetic](03-arithmetic.md) describes, as do two floats of one type;
which other types compare, and what `==` computes on them, is described
in [Types](04-types.md), and comparing errors in [Errors](09-errors.md).
`/` and `%` on integers need a divisor proven not zero, and a signed `/`
a proof that it is not the smallest value divided by -1 ([Safety and
proofs](10-safety.md)).

```tack run exit=21
package main

func main() {
    a := 1 + 2 << 3
    b := 6 & 3 * 2
    exit(int32(a + b))
}
```

## Logical operators

`&&`, `||` and `!` take `bool` operands and give a `bool`. `!x` is true
when `x` is false. `a && b` is true when both are, and `a || b` when
either is. `&&` and `||` short-circuit: the right operand is evaluated
only when the left does not decide the result, so `n != 0 && 10 / n > 1`
never divides by zero. In a package-level variable's initializer both
operands are computed, so an error on either side is reported
([Declarations and scope](05-declarations.md)). What a condition built
with these operators proves is described in [Safety and
proofs](10-safety.md).

```tack run exit=4
package main

func touch() bool {
    print("touched\n")
    return true
}

func main() {
    a := false && touch()
    b := true || touch()
    c := true && touch()
    if !a && b && c {
        exit(4)
    }
}
```

```output
touched
```

## Type assertions

A type assertion `a.(T)` asks whether the `any` `a` holds a value of
type `T`, exactly: an `int8` passed as `any` is not an `int64`. It has
only the checked form, `v, ok := a.(T)`, or `v, ok = a.(T)`, in which
`v` is the value when `a` holds a `T` and `T`'s zero value when it does
not, and `ok` says which. The rules every checked form follows, that it
is the whole value bound and that `ok` is not discarded, are described in
[Safety and proofs](10-safety.md). Anywhere else, an assertion is `a
type assertion needs two results; write v, ok := a.(int64)`.

`T` is one of the basic types an `any` can hold ([Types](04-types.md));
any other is `an any never holds Point: only the builtin types and views
of them can be passed as any for now`, and a view an `any` holds is asked
for only with a type switch ([Statements](07-statements.md)). An operand
that is not an `any` is `cannot assert on x: it is int64, not any`.

```tack run exit=6
package main

func small(a any) int32 {
    v, ok := a.(int8)
    if ok {
        return int32(v)
    }
    return 0
}

func main() {
    var x int8 = 6
    if small(5) != 0 {
        exit(1)
    }
    exit(small(x))
}
```

## The ? operator

`f(...)?` calls `f`, whose last result must be an error, and passes that
error on: when its code is not 0, the function the `?` is written in
returns it, unwrapped, as a `return` written there would, running the
defers reached and freeing every owned value the function holds; when
the code is 0, `f(...)?` gives the call's other results. With one, it is
a value of that result's type, usable wherever a value is; with none,
it is a statement only; with several, they are bound together,
`q, r := divide(a, b)?` ([Statements](07-statements.md#several-results)),
and are not one value, `divide(a, b)? gives 2 values`. The error is not
among them, so nothing holds the values and they need no check before
they are read ([Errors](09-errors.md#propagation)).

What `?` follows must be a call of a function, a method, a function
value, or `append`, `reserve` or `put`, written in parentheses or not:
`(f())?` and `f() ?` are `f()?`. Anything else, a variable, a stored
error, a literal, a conversion, a union's construction or another `?`,
is `cannot use ? on err, which is not a call`, and a call whose last
result is not an error `cannot use ? on a call to lookup, whose last
result is bool, not error`, or, returning nothing, `... which returns no
error`. The function the `?` is in must return an error last: in `main`
or any function that does not, it is `cannot use ? in main, which
returns no error`, or `cannot use ? in f, whose last result is int64,
not error`. In deferred code, which cannot return, it is `cannot return
from a defer with ?`, and in a package-level variable's value `cannot
use ? outside a function`. The other results of the function it returns
from are left empty, which its caller never reads, since the error
beside them is set.

A `?` may sit anywhere in an expression. The expression is evaluated in
order (see [Order of evaluation](#order-of-evaluation)), and a `?` that
returns stops it there: no operand to its right is evaluated, and a
value an operand to its left made that nothing holds yet, a list made
for an `own` parameter, is freed.

```tack run exit=7
package main

func parse(s string) (int64, error) {
    if len(s) == 0 {
        return 0, error{code: 7, msg: "empty"}
    }
    return len(s), noerror
}

func add(a int64, b int64) int64 {
    print("add\n")
    return a +% b
}

func both(a string, b string) (int64, error) {
    return add(parse(a)?, parse(b)?), noerror
}

func main() {
    n, err := both("ab", "c")
    if err.code != 0 || n != 3 {
        exit(1)
    }
    _, e := both("", "c")
    exit(e.code)
}
```

```output
add
```

## Order of evaluation

Wherever Tack evaluates several operands, it evaluates them left to
right, each once: the two sides of a binary operator, subject to
short-circuiting; a call's arguments; a method call's receiver before
its arguments; a function value before the arguments of a call through
it; a return's values; an array literal's elements; a struct literal's
values, in the order they are written; an indexed value before its
index; a sliced value before its low bound, and the low bound before the
high; and a switch's tag before its case values. The statements that
evaluate several operands, and the order an assignment follows, are
described in [Statements](07-statements.md). A `?` that returns ends the
evaluation where it is ([The ? operator](#the--operator)).

The order shows only through calls, which can print, exit, assign a
mutable global that another operand reads, or change a variable that
another operand reads through a `mut` argument. A plain argument is
evaluated before the call begins and passed as a copy, so it keeps the
value it had then. A `mut` argument stands for its place, not a value
read from it: the callee sees the variable as it is when the call
begins, after every argument has been evaluated.

```tack run exit=3
package main

func next(mut k int8) int8 {
    if k < 9 {
        k++
    }
    return k
}

func three(a int8, b int8, c int8) int32 {
    print("called\n")
    return int32(a) * 100 + int32(b) * 10 + int32(c)
}

func say(s string, v int32) int32 {
    print(s)
    return v
}

func main() {
    mut k int8 = 0
    n := three(next(mut k), k, next(mut k))
    m := say("left\n", 1) < say("right\n", 2)
    if n == 112 && m {
        exit(3)
    }
}
```

```output
called
left
right
```
