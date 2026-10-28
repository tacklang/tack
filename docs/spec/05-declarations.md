# 5. Declarations and scope

This chapter describes every declaration and the names it introduces:
package-level variables (`var`, `mut`) and their initializers (what an
initializer may name and compute, and initialization cycles), type
declarations, function and method declarations with their parameters
and results (the forms `mut p`, `own p`, views and variadics, as
syntax), local variable declarations, the blank identifier, scopes and
shadowing, which names may not be redeclared, and visibility (`pub`,
`lib` and private) for package-level names and struct fields. It owns
the unused-name rules: which declared names, imports among them, are
reported when never used, and whether each report is a warning or an
error. A variable holding an error that is never read is not one of
them: it drops the error, which [Errors](09-errors.md) describes, and a
variable holding an `ok` that is never read leaves its checked form
unchecked, which [Safety and proofs](10-safety.md) describes. What a
parameter form means for ownership and aliasing belongs in [Ownership
and memory](08-ownership.md); contracts (`ensures`) belong in [Safety
and proofs](10-safety.md); interface and generic declarations belong in
[Generics and interfaces](11-generics.md); `extern func` belongs in
[Extern C](14-extern.md); imports belong in [Packages, modules and
programs](12-packages.md).

## Scopes

A declaration binds a name to a variable, a function, a type or an
imported package. The scope of a name is the part of the source in
which the name refers to what it binds. Tack has these scopes:

- The builtin types and the builtin functions ([Builtins](13-builtins.md))
  are in scope in every file of every package. `true`, `false`,
  `noerror` and `c_allocator` are keywords, not names, and mean the same
  everywhere.
- A function's type parameters are in scope in its signature and body
  ([Generics and interfaces](11-generics.md)).
- A package-level name, a function, a type or a package-level
  variable, is in scope in every file of its package, before and after
  its declaration alike. A package may span several files, as
  [Packages, modules and programs](12-packages.md) describes; the order
  of its files and of the declarations in them does not change what a
  name refers to.
- An import's name is in scope in the file that imports it, and in no
  other file of the package.
- A parameter, a method's receiver included, is in scope in its
  function's body.
- A local variable is in scope from the end of its declaration to the
  end of the innermost block that holds it. A variable declared in the
  header of a `for` statement is in scope in the header after its
  declaration and in the loop's body. A name a type switch or a union
  switch binds is in scope in the body of the case that binds it.

Field names, union case names and method names are not in any of these
scopes: each struct has a namespace of its fields and methods, and each
union one of its cases and methods. A field, a case or a method is
reached only through a value or a type, as `p.x`, `Expr.int(5)` or
`b.len()`, and may share its name with anything outside its type.

A global may be used in a function written before it, and a type in a
signature written before it:

```tack run exit=10
package main

func double(p Pair) int32 {
    return (p.a + p.b) * scale
}

type Pair struct {
    a int32
    b int32
}

var scale int32 = 2

func main() {
    exit(double(Pair{a: 1, b: 4}))
}
```

A local variable cannot be used before its declaration, even in the
same block:

```tack error
package main

func main() {
    y := z + 1
    z := 2
    exit(y + z)
}
```

```diagnostics
main.tck:4:10: error: undefined: z
```

## The blank identifier

`_` is not a name. It may be written only in these places:

- as one of the names on the left of a declaration or an assignment of
  several results, `_, r := divmod(7, 2)` or `q, _ = divmod(7, 2)`,
  where it discards the result in its position, and may be written more
  than once;
- as the value of a checked form, `_, ok := a[i]`, which discards
  the value; never as its `ok`, which `v, _ := a[i]` would drop, `the ok
  a[i] gives cannot be dropped; bind it to a variable instead of _`
  ([Safety and proofs](10-safety.md));
- as the first of a range clause's two names, `for _, x := range a`.

A declaration or an assignment whose every name is `_` is an error,
since the call can be made on its own or the loop written without
names. `_` cannot be declared on its own, read, assigned on its own,
written with `mut`, or name a parameter, a field, a union case, a
type, a function or a package-level variable: each is an error,
`cannot use _ as a name`, `cannot use _ as a value` where it is read,
as in `print(_)`, or `cannot assign to _`. `_` where a call's error result
goes drops that error, which [Errors](09-errors.md) describes.

```tack run exit=1
package main

func divmod(a int32, b int32) (int32, int32) {
    if b == 0 {
        return 0, 0
    }
    return a / b, a % b
}

func main() {
    _, r := divmod(7, 2)
    exit(r)
}
```

A parameter named `_` is an error; a parameter a function does not
read is named, and warned of (see [Unused names](#unused-names)):

```tack error
package main

func ignore(_ int64) {
}

func main() {
    ignore(1)
}
```

```diagnostics
main.tck:3:13: error: cannot use _ as a name
```

## Package-level variables

A package-level variable is declared outside any function with `var`,
which declares an immutable variable, or `mut`, which declares a mutable
one:

```text
var name = value
var name T = value
mut name = value
mut name T = value
mut name T
```

The short form `name := value` is not allowed at package level. A `mut`
variable declared with a type and no value starts at its type's zero
value ([Types](04-types.md)); a `var` variable always has a value. A
global's type, written or taken from its initializer, may not be or
hold a list, a map or a function value: `package-level variable g
cannot hold a list`, `... cannot hold a map`, `... cannot hold a
function value`. All of a program's globals together hold at most 2^30
bytes ([Types](04-types.md)).

A package-level variable is initialized with a constant expression,
which the compiler computes: no code of the program runs before `main`.
A constant expression is built only from:

- literals: integer, float, character, string and `bool`, and the
  keywords `true`, `false`, `noerror` and `c_allocator`;
- names of immutable globals, of the package or of an imported
  package, declared before or after;
- array and struct literals of constant expressions, and a union built
  from a constant payload, `Token.word("if")`, or with none,
  `Token.eof`;
- a field of a constant expression, `origin.x`, an element of one at a
  constant index, `table[0]` or `table[len(table) - 1]`, and a byte of a
  constant string, `word[0]`;
- `len` of a constant expression, and `len` of any array, even a
  mutable global one, whose length is its type's, as long as computing
  it calls nothing;
- a conversion `T(x)` of a constant expression, which must give a value
  of `T`;
- the arithmetic, bitwise and shift operators, comparisons, `&&`, `||`
  and `!` on constant expressions, except `^x` of a target-width type,
  whose value depends on the target.

What each operator computes, and that overflow and division by zero in
a constant expression are compile-time errors, is described in
[Constants and arithmetic](03-arithmetic.md). An index out of range in a
constant expression is an error, `index 3 is out of range for
[3]int64`. Both sides of `&&` and `||` are computed in an initializer,
so an error on either side is reported.

Anything else is an error, `x must be initialized with a constant
expression`: a call to a function, or to a builtin other than `len`,
and a mutable global anywhere but under `len` of an array.

```tack run exit=7
package main

var limit = base * 2 + 1

var base = 3

var table = [3]int64{limit, base, len("tack")}

func main() {
    exit(int32(table[0]))
}
```

A call is not a constant expression:

```tack error
package main

var start = now()

func now() int64 {
    return 1
}

func main() {
    exit(int32(start))
}
```

```diagnostics
main.tck:3:13: error: start must be initialized with a constant expression
```

### Initialization cycles

The initializer of a package-level variable, and its type, may name
other globals, but no global may need itself, directly or through
others. A global that does is an error, `initialization cycle: a
refers to itself`, or, through others, naming each global round the
cycle:

```tack error
package main

var a = b + 1

var b = a + 1

func main() {
    exit(int32(a))
}
```

```diagnostics
main.tck:5:9: error: initialization cycle: a refers to b refers to a
```

A global's type may name a global declared after it, as in `mut cells
[size]int64` before `var size = 4`.

## Type declarations

A type declaration binds a name to a new type. It is written at
package level only:

```text
type Name struct { fields }
type Name union { cases }
type Name interface { methods }
```

Each declaration is a type of its own, distinct from every other, even
one with the same fields or cases. Struct, union and interface types
are the only types a program may declare: any other, `type Celsius
int64`, is `only struct, interface and union types can be declared`.
Type aliases, anonymous struct and union types and embedded fields are
not supported, and each is an error. Interface declarations are
described in [Generics and interfaces](11-generics.md), and what values
each kind of type holds in [Types](04-types.md).

A type's name shares one namespace with the package's functions and
package-level variables and with the builtins: it cannot be declared
again as any of them, nor as a local, a parameter or a binding. A
type's name is not a value: it cannot be assigned, called or used as an
operand, `cannot use Point as a value; it is a type`. A name that is
not a type cannot be written as one.

### Struct types

A struct declaration lists the struct's fields, one per line or
separated by `;`, each a name and a type. `struct {}` has no fields and
is allowed. A field's name is unique within its struct and is neither
`_` nor the name of a builtin type. A field may be `pub` or `lib`
([Visibility](#visibility)).

A field may have any type, another struct or an array of one included,
except a type that holds the struct itself, directly, through other
structs or unions, through an array of any length or through a list,
which is an error. An array length in a field's type is a constant
expression that names no variable: literals, character literals,
arithmetic on them and `len` of a string literal, `[len("abc") *
2]int64`. A length naming any variable, even an immutable global, is
`array length in a struct field must not name variable n`, so a
struct's layout never depends on a variable.

A literal of a struct type names every field:

```tack run exit=3
package main

type Point struct {
    x int64
    y int64
}

func main() {
    p := Point{x: 1, y: 2}
    code, fits := int32(p.x + p.y)
    if fits { exit(code) }
}
```

### Union types

A union declaration lists the union's cases, one per line or separated
by `;`, each a name and the type of its payload, or a name alone for a
case with no payload. `union {}` has no cases and so no value. A case's
name is unique within its union, `duplicate case name`, follows a
field's naming rule, and has no visibility and no tag value of its
own. A payload may have any type a field may, under a field's rules:
an array length in it names no variable, `array length in a union's
case must not name variable n`. A union that holds itself, directly or
through a struct, an array or another union, is `invalid recursive
type Expr`, and through a list `invalid recursive type Expr: a type
cannot hold itself, even through a list`.

```tack run exit=4
package main

type Token union {
    number int32
    plus
    eof
}

func weight(t Token) int32 {
    switch t {
    case number(n):
        return n
    case plus:
        return 1
    case eof:
        return 0
    }
}

func main() {
    exit(weight(Token.number(3)) + weight(Token.plus))
}
```

A type that is not a struct, a union or an interface cannot be
declared:

```tack error
package main

type Celsius int64

func main() {
}
```

```diagnostics
main.tck:3:14: error: only struct, interface and union types can be declared
```

## Function declarations

A function declaration binds a name to a function. It is written at
package level only:

```text
func name(parameters) results contract { body }
```

The parameters are a list, separated by commas, possibly empty and
possibly ending in a comma, of which each is a name and one type: `func
f(a int64, b int64)`. Names
are not grouped under one type. The results are nothing, one type, or
several types in parentheses, `(int64, error)`; results are unnamed. A
function with results must end in a terminating statement
([Statements](07-statements.md)). An `error` result may only be the
last ([Errors](09-errors.md)). A contract, `ensures` and a condition,
may stand between the results and the body; [Safety and
proofs](10-safety.md) describes it. A function written with type
parameters in brackets, `func pick[T Elem](...)`, is generic, as
[Generics and interfaces](11-generics.md) describes.

A function has a body, except an `extern func`, which [Extern
C](14-extern.md) describes. Any other function without a body is
`function f has no body; only an extern func is declared without one`.

A parameter is written in one of these forms:

| Form         | Example                    | The parameter is                                      |
|--------------|----------------------------|-------------------------------------------------------|
| `p T`        | `n int64`                  | an immutable copy of its argument                     |
| `mut p T`    | `mut p Point`              | the caller's place, which the function may change     |
| `own p T`    | `own t text.Text`          | the caller's value, moved into the function           |
| `p []T`      | `v []int64`                | a read-only view of the caller's elements             |
| `p ...T`     | `nums ...int64`            | zero or more values, seen as a view `[]T`             |
| `p any`      | `a any`                    | a value of a basic type or a view, as `any`           |

What `mut`, `own` and views mean for the caller's value is described in
[Ownership and memory](08-ownership.md), and `any` in
[Types](04-types.md). As syntax: `mut` and `own` stand before the
parameter's name, and a parameter is not both; a view's or a
variadic's element is not itself a view; only the last parameter may be
variadic, `can only use ... with the final parameter`, and it is not
`mut`. A view may be a function's result only when it is the only one.
A function's parameters are immutable, except a `mut` one.

A function's name is unique among its package's package-level names,
`f is already declared`, and is not a builtin's. A package other than
`main` declares nothing named `main`; the `main` package declares `func
main()`, with no parameters and no result ([Packages, modules and
programs](12-packages.md)).

```tack run exit=5
package main

func divmod(a int32, b int32) (int32, int32) {
    if b == 0 {
        return 0, 0
    }
    return a / b, a % b
}

func scale(mut n int32, k int32) {
    n = n * k
}

func main() {
    q, r := divmod(17, 5)
    mut total := q
    scale(mut total, r)
    exit(total - 1)
}
```

A function without a body is an error:

```tack error
package main

func half(n int32) int32

func main() {
    exit(half(4))
}
```

```diagnostics
main.tck:3:6: error: function half has no body; only an extern func is declared without one
```

## Method declarations

A method is a function declared with a receiver, a parameter written in
parentheses before its name:

```text
func (r T) name(parameters) results { body }
func (mut r T) name(parameters) results { body }
```

The receiver's type is a struct or union type declared in the same
package, in any file of it, before or after the method. A method on any
other type, a type of another package, an interface, a builtin type,
`any`, `error`, an array, a view or a list, is `cannot declare a method
on T: only a struct or union type declared in this package can have
methods`. A receiver is a single parameter; it is neither variadic nor `own`, and a method has no type parameters of its own. A
receiver follows a parameter's rules for its name. A plain receiver is
a plain parameter; a `mut` receiver is a `mut` parameter.

A method's name is in its type's namespace. It may share a name with a
function, a global, `main` or a builtin, but not with a field of its
struct, `len is already declared as a field of Builder`, a case of its
union, `int is already declared as a case of Expr`, or another method
of its type, `Builder.len is already declared`. A method has a body:
it cannot be extern, `a method cannot be extern; declare an extern
func and call it from the method`, nor be declared without one, `method
Builder.write has no body`. In every other way a method is a function:
its parameters, results and body follow the rules above. How a method
is called is described in [Expressions](06-expressions.md).

```tack run exit=9
package main

type Counter struct {
    n int32
}

func (c Counter) get() int32 {
    return c.n
}

func (mut c Counter) add(k int32) {
    c.n = c.n + k
}

func main() {
    mut c := Counter{n: 4}
    mut c.add(5)
    exit(c.get())
}
```

## Local variable declarations

A local variable is declared inside a function body in one of these
forms:

```text
name := value
var name = value
var name T = value
mut name := value
mut name = value
mut name T = value
mut name T
```

The forms with `var`, or with neither keyword, declare an immutable
variable, which must have a value: `immutable variable x needs a
value`. The forms with `mut` declare a mutable one. `mut name T`, with
no value, starts the variable at its type's zero value; a type with no
zero value, a list, a map, an `Allocator`, a union, a function type, or
a struct or array holding one, needs a value, `l needs a value:
list[int64] has no zero value`. Without a type, the variable takes the
value's type, and an untyped constant its default type
([Constants and arithmetic](03-arithmetic.md)). With one, the value
must be assignable to it ([Types](04-types.md)). A view can be the type
of a parameter or a result only, and `any` of a parameter only; an
immutable local may still hold an `any` declared from one with its type
left out, as [Types](04-types.md) describes.

Several results of one call are bound by one declaration, `q, r :=
divmod(7, 2)`, in which each name may have its own `mut`, `q, mut r :=
divmod(7, 2)`; the checked forms are bound the same way, `v, ok :=
a[i]`. [Statements](07-statements.md) describes this declaration.

A variable declared in a `for` statement's header, a range clause's
names, a type switch's binding and a union switch's binding are
declarations too, described with their statements.

```tack run exit=12
package main

func main() {
    a := 2
    var b = 3
    var c int32 = 4
    mut d int64
    mut e int32 = 1
    d = a + b + c
    e = e + c
    if d == 9 {
        exit(e + 7)
    }
}
```

An immutable variable with no value is an error:

```tack error
package main

func main() {
    var w int32
    exit(w)
}
```

```diagnostics
main.tck:4:9: error: immutable variable w needs a value
```

## Redeclaration and shadowing

Tack has no shadowing. A local variable or a parameter may not reuse
the name of a variable, a parameter or a package-level variable in
scope where it is declared, in an inner block included; nor the name
of a type, `cannot use int64 as a name; it is a type` for a builtin
type and `Point is already declared` for a declared one; nor an
import's, `fmt is an imported package and cannot be redeclared`.
Each is an error, with a note at the earlier declaration:

```tack error
package main

var limit = 10

func main() {
    total := 1
    if total > 0 {
        limit := 3
        total := 4
        exit(limit + total)
    }
}
```

```diagnostics
main.tck:8:9: error: limit is already declared
main.tck:3:5: note: previous declaration of limit
main.tck:9:9: error: total is already declared
main.tck:6:5: note: previous declaration of total
```

A local variable or a parameter may have the name of a function. A
call by that bare name still calls the function, so a variable holding
a function value under a function's name cannot be called by it, as
[Expressions](06-expressions.md) describes.

At package level, each name is declared once in its package, in any of
its files: two functions, types or globals of one name, or one of each,
are `x is already declared`, with a note at the other. A package-level
name may not be a builtin's or a keyword's: a global is `print is a
builtin and cannot be redeclared`, and a function `duplicate
declaration of builtin function`. The builtin types' names, `true`, `false`,
`noerror` and `c_allocator` can be written only as what they are.

## Visibility

A package-level name is private to its package unless its declaration
starts with `pub`, which makes it usable from every package, or `lib`,
which makes it usable from every package of its module and from no
other module. `pub` and `lib` may start a function, a type, an immutable
global or a method declaration, and may stand before a struct field.

```text
pub func First(src string) Token
lib type Pos struct { ... }
pub var KindNumber = 1
pub func (t Token) Kind() int64
```

Naming a name another package cannot see is an error where it is
named: `lex is not pub in package calc/lexer`, or `isDigit is lib in
module calc and cannot be used from module other` (`from a single file,
which has no module`, for a file compiled on its own). The following
rules hold:

- A struct's fields are private by default, each made `pub` or `lib`
  on its own. A field is never more visible than its struct. Reading or
  writing a field another package cannot see, by `=`, a compound
  assignment, `++` or `--`, is `end is not pub in lexer.Token`.
- Since a struct literal names every field, a literal of a struct with
  a field the package cannot see is `cannot write a lexer.Token literal
  outside package lexer: field end is not pub` (`fields start, end are
  not pub` for several). Declaring a variable of such a struct at its
  zero value, copying one, passing it and comparing two with `==` are
  allowed.
- A union's cases are exactly as visible as the union, and cannot be
  marked themselves, `a union's case cannot be pub; it is as visible as
  its union`.
- A method is `pub` or `lib` as a function is, and is never more
  visible than its type. Calling one another package cannot see is
  `hidden is not pub in lexer.Token`.
- A `mut` global can be neither `pub` nor `lib`, `a mut variable cannot
  be pub; export a function that reads or changes it`, so only its own
  package changes it. Nor can an extern func, `an extern func cannot be
  pub; export a Tack function that calls it` ([Extern C](14-extern.md)).
- A `pub` declaration may expose only `pub` types, and a `lib` one only
  `lib` or `pub` types: every struct or union type, or array of one,
  named by a function's or method's parameters and results, its
  receiver included, by a global's type, by a field's type or by a
  union case's payload must be at least as visible as the declaration,
  a field's visibility capped by its struct's. Otherwise it is an
  error, `pub function Result exposes hidden, which is not pub` (`which
  is not lib or pub` for `lib`; `variable`, `method Token.Get`, `field
  kind of Token` and `case binary of Expr` for the others).
- A `main` package cannot be imported, so `pub` and `lib` are errors
  there, on a declaration or a field: `pub has no effect in package
  main, which no package can import`.
- Locals, parameters and union cases take neither keyword.

The standard library's packages follow these rules as any package does:

```tack error
package main

import conv from "std/conv"
import text from "std/text"

func main() {
    t := text.new(c_allocator)
    e := conv.syntax
    print(e.msg)
    print(t.b)
}
```

```diagnostics
main.tck:8:10: error: syntax is not pub in package std/conv
main.tck:10:13: error: b is not pub in text.Text
```

In a package other than `main`, a `mut` global marked `pub` and a `pub`
function exposing a private type are errors:

```tack error
package shapes

type hidden struct {
    n int64
}

pub mut count int64

pub func Make() hidden {
    return hidden{n: 1}
}
```

```diagnostics
main.tck:7:1: error: a mut variable cannot be pub; export a function that reads or changes it
main.tck:9:17: error: pub function Make exposes hidden, which is not pub
```

## Unused names

A declared name that is never used is reported. Today the compiler
reports each case below as a warning, which does not stop the program
from building; docs/tracking.md records unused imports and unused
locals as errors, and the two are not yet reconciled. A variable
holding an error that is never read is described in
[Errors](09-errors.md), and one holding an `ok` that is never read in
[Safety and proofs](10-safety.md); neither is an unused name.

- A local variable that is never read is `declared and not used: x`.
  Assigning it, with `=`, a compound assignment, `++` or `--`, does not
  count as reading it, nor does writing a field or an element of it;
  passing it as `mut` does.
- An import that no name of its file uses is `imported and not used:
  fmt`.
- A private package-level variable that is never read is `declared and
  not used: x`. A `pub` or `lib` one is never reported, since the
  packages that read it may not be in the program.
- In a program with no error, a parameter its function never reads is
  `declared and not used: by`, and a private function, method or type
  that nothing names is `declared and not used: helper`, `declared and
  not used: T.spare` and `declared and not used: Point`.
- A type switch's binding that no case reads, and a union switch's
  binding its clause never reads, are `declared and not used: v`, at
  the binding.

A function is used when a name outside its own body names it, called or
as a value. A method is used when a call outside its own body calls it,
or when a generic call binds its type to a type parameter whose
constraint asks for it. A type is used when a name outside its
declaration and its methods names it: a field of another type, a
literal, a signature or a constraint among them. Uses are not followed
further, so a function that only an unused function calls is used.

These are never reported: a `pub` or `lib` declaration; `func main`; a
method's receiver; a struct's field; the parameters of a function whose
signature is not its own to change, an extern func's, a `pub` or `lib`
function's or method's, a function used as a value's, and a method's
that a constraint asks for; and the parameters of an unused function,
apart from the function itself. A parameter cannot be named `_`, so a
parameter a function ignores on purpose is reported.

```tack warn
package main

func spare() {
}

func first(a int32, b int32) int32 {
    return a
}

func main() {
    mut n int64
    n = 4
    exit(first(3, 4))
}
```

```diagnostics
main.tck:3:6: warning: declared and not used: spare
main.tck:6:21: warning: declared and not used: b
main.tck:11:9: warning: declared and not used: n
```

An import no name of its file uses:

```tack warn
package main

import fmt from "std/fmt"

func main() {
}
```

```diagnostics
main.tck:3:8: warning: imported and not used: fmt
```
