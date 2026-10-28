# 9. Errors

This chapter describes how a program reports and handles failure: the
`error` value's code and message, `noerror`, returning an error alone or
last among several results, the rule that an error must be checked
before the values beside it are used and may not be dropped, what drops
one (`_` where it goes, and a variable holding it that is never read,
which is reported as a drop, not as an unused name), checking it in
`if`, `switch` and loops, propagation, comparing two errors with `==`
and `!=`, sentinel errors, and errors in fields and arrays. The `error`
type's place among the types belongs in [Types](04-types.md); the other
unused-name rules belong in [Declarations and
scope](05-declarations.md); `std/errors` is part of the standard
library, not of this specification; the checked forms `v, ok := ...`,
which report failure as a `bool`, belong in [Safety and
proofs](10-safety.md).

Tack has no exceptions and no runtime error. A function that can fail
says so in its signature by returning an `error`, and the compiler holds
its callers to three rules: the error is the last result, it is never
dropped, and the results beside it are not read until it is checked.
The rules are the compiler's; at run time an error is a plain value.
Several points of these rules are recorded as open questions in
[open-decisions.md](../open-decisions.md#errors), and may change.

## Error values

`error` is a builtin struct type of two fields, `code`, an `int32`, and
`msg`, a `string`. A code of 0 is no error, whatever the message; any
other code is an error. An error is written as a struct literal naming
both fields, `error{code: 2, msg: "not found"}`.

`noerror` is a keyword, like `true`: the error with code 0 and an empty
message. It is the zero value of `error`, so `mut e error` starts as
`noerror`. `noerror` and an error literal are constant expressions.

An error does what a struct does. Its fields are read from any error,
`err.msg`, and written through a mutable variable, `e.code = 7`, except
where an error variable holds the results bound beside it (see
[Checking an error](#checking-an-error)). What else the `error` type
allows, as an operand, an `any` or a map key, is described in
[Types](04-types.md).

## Returning an error

A function that can fail returns an error as its last result, alone,
`func check() error`, or after the values it gives when it succeeds,
`func parse(s string) (int64, error)`. An error anywhere else in a result
list is `error must be the last result of f`. A function that succeeds
returns `noerror`, `return n, noerror`.

The rules of this chapter apply to a call whose last result is written
`error`. A builtin that can fail, `append`, `reserve` and `put`, returns
an error alone, under the same rules.

A call returning an error alone is a value like any other: it may be
passed, returned or compared, `if check() == noerror`, and doing so uses
the error rather than dropping it. A call with several results is bound
with `v, err := f()` or assigned with `v, err = f()`.

## Checking an error

After `v, err := f()`, or `v, err = f()`, `err` holds `v`. Reading `v`,
as a value, passing it as `mut`, or updating it with a compound
assignment, `++` or `--`, is `v cannot be read before err is checked;
check err.code == 0 first` wherever the facts in force do not prove
`err.code == 0`. Assigning `v` whole, or a field or element of it, is
not a read.

```tack error
package main

func parse(s string) (int64, error) {
    if len(s) == 0 {
        return 0, error{code: 1, msg: "empty"}
    }
    return 7, noerror
}

func main() {
    v, err := parse("x")
    print(err.msg)
    if v > 0 {
        exit(1)
    }
}
```

```diagnostics
main.tck:13:8: error: v cannot be read before err is checked; check err.code == 0 first
```

The check is a fact, given and ended as every fact is (see [Safety and
proofs](10-safety.md#facts)): a comparison of `err.code` with a known
value that bounds the code to exactly 0. So `if err.code != 0 { return
0, err }` proves it for the rest of the block, `if err.code == 0 { ... }`
in its branch, the `else` of `if err.code != 0` in that branch, `case 0:`
of a `switch err.code` in its body, `err.code == 0 &&` on its right, a
loop's condition in its body, and a call to a function whose contract
ensures `err.code == 0` after the call (see [Safety and
proofs](10-safety.md#contracts)). A branch ending in `exit` leaves, as
one ending in `return` does. `err.code` carries this fact whether `err`
is mutable or not.

```tack run exit=7
package main

func parse(s string) (int64, error) {
    if len(s) == 0 {
        return 0, error{code: 1, msg: "empty"}
    }
    return 7, noerror
}

func main() {
    v, err := parse("x")
    if err.code != 0 {
        exit(err.code)
    }
    if v == 7 {
        exit(7)
    }
}
```

A check that leaves an error possible is not one: after `if err.code > 0
{ return }`, the code may still be negative, and the read is reported
with the note `this early exit allows err.code == -1`. Comparing the
whole error, `err == noerror`, is not a check.

A check holds up to the first write to `err` that can reach the read, so
binding a new error to `err` ends it for every read after the binding,
and a rebinding later in a loop whose check is outside the loop reaches
the read on the next pass.

So that a check is always about the error that came with `v`, two rules
hold:

- A binding that takes a result beside an error binds both to local
  variables, never to a parameter, a global, a field or an element: `p.x
  cannot take a result of parse beside its error; bind it to a local
  variable`, `last cannot take the error of parse beside v; bind it to a
  local variable`.
- An error variable that holds results is assigned only by a binding of
  a call that returns an error and assigns every variable it holds. A
  plain assignment, to it or to a field of it, `++`, a `mut` argument, and
  a binding that leaves one of them out are each `err holds the error
  that came with v, so only a binding that also assigns v can assign
  it`, with the note `err is bound beside v here`.

A variable bound beside two errors, at two bindings, needs both checked.

## Dropping an error

An error cannot be dropped. A call statement whose callee returns an
error is `the error f returns cannot be dropped; bind it, as in err :=
f(...)` when the error is its only result, and `... bind it, as in _, err
:= f(...)` when results come before it. Where a result beside the error
cannot be dropped either, a list or a value holding one, the suggestion
names it rather than `_`, with `x`, `y` and `z` in order, then `x4` and
on: `bind it, as in x, err := bytes.repeat(...)`.

```tack error
package main

func check(n int64) error {
    if n < 0 {
        return error{code: 1, msg: "negative"}
    }
    return noerror
}

func main() {
    check(1)
}
```

```diagnostics
main.tck:11:5: error: the error check returns cannot be dropped; bind it, as in err := check(...)
```

`_` where the error goes, a binding of nothing but `_` included, is `the
error f returns cannot be dropped; bind it to a variable instead of _`.

A variable holding an error that nothing ever reads drops it as surely.
After `err := f()`, `v, err := f()` or `err = f()`, and the same for
`append`, `reserve` and `put`, an `err` never read is `the error f
returns is never checked: err is not read`, reported at its name, in
place of the warning that the variable is unused. Reading it is what
checks it: reading its `code` or its `msg`, or passing it on or
returning it. Only a variable never read at all is reported, not one
assigned again before its first value was read.

```tack error
package main

func main() {
    mut l := make(list[int64], c_allocator)
    err := append(mut l, 1)
}
```

```diagnostics
main.tck:5:5: error: the error append returns is never checked: err is not read
```

## Propagation

A function propagates an error by returning it. After a check,
`if err.code != 0 { return 0, err }` passes the error on with a value of
its own beside it.

A return that passes `v` on beside `err` unchecked, `return v, err`, is
not a read of `v`: the caller binds what it returns beside the same
error and checks it there. That holds when the function's error result
is `err`, a local variable, and each value returned beside it is a
variable `err` holds, returned whole as a result that is not a view.
Anything else that returns `v` reads it: `v` beside another error, as in
`return v, noerror`, or `v` in an expression, as in `return v + 1, err`.

```tack run exit=7
package main

func parse(s string) (int64, error) {
    if len(s) == 0 {
        return 0, error{code: 1, msg: "empty"}
    }
    return 7, noerror
}

func forward(s string) (int64, error) {
    v, err := parse(s)
    return v, err
}

func main() {
    v, err := forward("x")
    if err.code != 0 {
        exit(1)
    }
    if v == 7 {
        exit(7)
    }
}
```

## Sentinel errors

Two errors compare with `==` and `!=`: they are equal when their codes
are equal and their messages hold the same bytes. A package-level
variable holding an error, a sentinel, is compared with an error a call
returned to tell one failure from another. Such a comparison is not a
check of the error (see [Checking an error](#checking-an-error)).

```tack run exit=2
package main

var notFound = error{code: 2, msg: "not found"}

func find(k int64) error {
    if k != 1 {
        return notFound
    }
    return noerror
}

func main() {
    err := find(5)
    if err == notFound {
        exit(err.code)
    }
    exit(1)
}
```

## Errors in fields and arrays

An error may be a local, a parameter, a result, a struct's field, an
array's element or a package-level variable, mutable or not. The rules of
this chapter follow the error a call returns as its last result, and the
variables bound beside it. An error stored anywhere else, and an array or
a struct holding one, `[2]error` among them, wherever it appears in a
result list, is an ordinary value, to which none of these rules apply.
