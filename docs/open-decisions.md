# Open language decisions

Questions the compiler currently answers by accident, and which need a
deliberate answer. Each entry says what happens today and why it matters.
When a decision is made, the entry is removed and the answer goes into the
README's "The language today".

## The `asm` keyword

`asm` is a keyword, and the lexer skips everything after it up to the next
`}` without tokenizing it; one with no `}` is an error. Nothing past the
lexer handles it. Is `asm` part of the language, and if so, what is its
syntax?

## Wording of the invalid character diagnostic

A byte the lexer does not accept is reported as `invalid character %c`
formatted from a single byte, so 0xFF prints as `invalid character ÿ` and a
UTF-8 letter such as `é` produces two diagnostics showing unrelated
characters. Showing the byte in hex, or decoding UTF-8 first, are options.

## Missing explanation notes

An error for a missing proof comes with notes when a guard in scope says
something about the operand (see [safety.md](safety.md#notes-on-a-missing-proof)).
Some causes still get the error alone, because the cause leaves no fact
behind, so explaining it means walking the condition again with logic that
mirrors how facts are derived. The wording for each was drafted and its
implementation deferred:

- `||` where one side says nothing about the variable, as in
  `if b < 0 || c > 0 { a / b }` (and `&&` when false): a note that the
  condition proves nothing about `b`, since the other side may be the one
  that holds.
- A comparison with a value that is not known, as in `if 0 <= i && i < n`
  or `if b > c`: a note that the comparison gives no fact about `i`, since
  `n` has no known value.
- `len(s)` in a form that gives no fact, as in `i <= len(s) - 1` or
  `i <= len(s)`, and `len` of a call, `i < len(name())`: a note that only
  `i < len(s)` of a string variable bounds `i` by its length.
- A string equality, as in `if s == "abc" { s[0] }`: a note that comparing
  strings says nothing about `len(s)`.

## Structs

Structs follow the decisions recorded in the README. One point was
settled by the implementation rather than decided, and could go another
way:

- A struct holding itself through an array of length 0 is rejected like
  any other cycle, as in Go.

A field of an immutable variable or parameter carries facts, as decided.
These points about it were settled by the implementation:

- A path's element counts only at a known index or an immutable
  variable's or plain parameter's, `p.a[1]` or `p.a[k]`, so that it names
  one element wherever it is written; `p.a[j]` with `j` mutable never
  carries a fact.
- A string field of an immutable variable has facts about its length,
  `len(p.s) > 0`, and bounds an index by it, `i < len(p.s)`, as a string
  variable does; a range over `p.s` proves `p.s[i]`.
- A field of an immutable global is still a known value first; a fact
  about it adds nothing a known value does not.
- The note for a guard that proves nothing about a field now says why, `a
  field of a mutable variable or a mut parameter never carries a fact`,
  and the note for an operand that is not proven says `only a name, or a
  field or element of an immutable variable, is proven`.

## Mut parameters

Mut parameters follow the decisions recorded in the README. These points
were settled by the implementation rather than decided, and could go
another way:

- A `mut` argument's type must be its parameter's exactly: a `mut int32`
  variable cannot be passed to `mut n int64`, since a reference to one
  cannot stand for a reference to the other. Widening still applies to
  plain arguments.
- `mut` is parsed before any operand, binding as a prefix operator does,
  so `mut a + 1` is `(mut a) + 1`; anywhere but a call's argument it is the
  type checker's error, `mut a can only be passed to a mut parameter`, and
  `mut mut x` is that error once.
- An immutable global passed as `mut` gets its own wording, `cannot pass
  mut limit: limit is immutable; copy it into a mutable local`, rather
  than the local's `declare it with mut`, which would lead to the mutable
  global error.
- Leaving `mut` out for a `mut` parameter suggests `pass mut b` when the
  argument is a variable, a field or an element, and otherwise says only
  a place can be passed as `mut`.
- `mut x` counts as reading `x` for the unused-variable check.
- Two indexes into one array are the same element when they are the same
  variable, or known values that are equal, or variables the facts pin to
  one equal value; then the call is `passed as mut twice`, not `may
  overlap`. They differ when known values differ or the facts give the
  two disjoint ranges. Relations between variables, such as `i != j` or
  `i < j`, give no fact, so `if i != j { swap(mut a[i], mut a[j]) }` is
  still an error.
- An exclusivity error is at the later argument, with a note at the
  first one it overlaps; each argument is reported once, against the
  first.
- A plain parameter is always passed by value in C, a copy, even when
  large; passing it by address where nothing overlaps, which the language
  allows, is not done.
- In the C backend, a variable a function passes as `mut` anywhere is
  treated as changeable by any call in that function when deciding where
  evaluation order could show, so its reads around such calls are held in
  temporaries; this is coarser than needed and costs only temporaries.

## Views

Views follow the decisions recorded in the README. These points were
settled by the implementation rather than decided, and could go another
way:

- `[]uint8(s)` takes any string: a literal, a variable, a field, an
  element or a call's result. A string's bytes are static today and never
  owned by its value, so no temporary is needed for any of them; a string
  that owns its bytes, if the memory model brings one, would reopen this
  for a call's result.
- `[]uint8(s)` of a mutable global string, or of a variable the same call
  passes as `mut`, views the bytes the string held when the argument was
  evaluated: assigning the string later changes which bytes it holds, not
  those bytes, so no copy is taken.
- `[]uint8(a)` of an array is an error, `cannot convert [2]uint8 to
  []uint8; pass the array itself to a view parameter`, so there is one way
  to pass an array; a conversion to any view but `[]uint8`, or of anything
  but a string, is `cannot convert ...`.
- A view's copy, for a mutable global or an overlapping `mut` argument, is
  taken when the argument is evaluated, left to right with the others, so
  `f(a, set(mut a))` views `a` as it was before `set` ran. Overlap is by
  path, but coarser than the exclusivity check's: any two elements of one
  array count as overlapping, which can only add a copy no program sees.
  Copying a large global array costs its size on every such call.
- `x := v` is `cannot declare x as []int64; a view can only be the type of
  a parameter`, at the name; a view written as any other type is `a view
  can only be the type of a parameter`, at the type.
- A switch on a view is `operator == is not defined on []int64`, even with
  no cases, since a switch compares with `==`.
- `len expects an array or a string` now reads `len expects an array, a
  string or a view`.
- Each view type is added to the type table when the checker first meets
  it, as an array is, which moves the IDs, and so the C names, of the
  types after it; only programs that use views see this.

## any and printf

`any` and `std/fmt`'s `printf` follow the decisions recorded in the
README. These points were settled by the implementation rather than
decided, and could go another way:

- A Tack function may declare `any` and `...any` parameters, not only an
  extern func, because `printf` must be a `pub` Tack function wrapping a
  private extern one, extern funcs never being `pub`. Inside it an `any`
  can be passed on, held in an immutable local, or asked which type it
  holds with a type assertion or a type switch (below).
- A view parameter may be `[]any`, as a variadic `...any` is inside its
  function; `[N]any` is an error, as is `any` anywhere but a parameter.
- `any` is a type's name, so it is reserved as one: a variable, function,
  field or package named `any` is an error.
- The fixed type ids follow the proposal's table: `bool` 1, `int8` to
  `int64` 2 to 5, `uint8` to `uint64` 6 to 9, `unsafe_size_t` 10,
  `unsafe_uintptr_t` 11, `string` 12.
- An `any` points at the caller's variable when nothing in the call can
  change it, a plain parameter, an immutable local or global, or a mutable
  local the function never passes as `mut` anywhere, and at a temporary
  copy otherwise, so a mutable local passed as `mut` in another call of
  the same function is copied though nothing in this call changes it.
- `printf`'s format is checked only for `std/fmt`'s `printf`, found by
  its package path and name, not by any marker a declaration could carry.
- A verb is the `%` and the UTF-8 sequence its next byte starts, so an
  unknown verb `%é` is shown and written whole; an unknown verb that is
  not graphic text is quoted in the diagnostic, `unknown printf verb
  "%\n"`.
- A count mismatch is reported at the values left over when there are
  more values than verbs, and at the format when there are fewer.
- A `tack_any` whose id is no type's, which only C can pass, is written as
  `invalid` in a marker, with no value: `%!d(invalid)`.
- `printf` writes with C's `fwrite` to `stdout`, the stream `print`
  writes to, in its own C file of `std/fmt`, rather than through
  `print`'s object, which is linked only when the program calls `print`.


## Type assertions and type switches

`v, ok := a.(T)` and `switch v := a.(type)` follow the decisions recorded
in the README. These points were settled by the implementation, by
consistency with the rules around them, and could go another way:

- `v, ok := a.(T)` follows every rule of the other checked forms, by
  sharing their code: `v, ok = a.(T)` assigns existing mutable variables,
  `T` widening to the target's type as a conversion's value does, `_`
  drops either value but not both, each name may have its own `mut`, and
  an assertion in a larger value bound to two names is `the checked form
  must be the whole value: v, ok := a.(int64), then compute v + 1`.
- A type switch's binding is reported unused once, at the binding, when
  no case reads its variable, as Go does; a case that does not read it is
  not reported, and declares no C variable.
- `switch mut v := a.(type)` is a parse error, `cannot use mut on a type
  switch variable`, worded as a range loop's `mut` is.
- A switch whose header binds a name to anything but `x.(type)` is a parse
  error, `a switch has no init statement; only a type switch binds a
  name, as in switch v := x.(type)`, which replaces `expected '{' found
  ':='` for `switch x := 1; x {`.
- The operand of `.(type)` that is not an `any` is reported with the
  assertion's wording, `cannot assert on s: it is string, not any`.
- A type an `any` never holds, written as a case, is reported with the
  assertion's wording, `an any never holds Point: only the builtin types
  can be passed as any for now`; a duplicate gets a `previous case` note,
  as an expression switch's does.
- `var b any = a`, with the type written, stays `any can only be the type
  of a parameter`: only a local whose type is left out may be an `any`.
- `v, ok := args[i]` out of range gives the empty `any`, a zeroed
  `tack_any` of type id 0, the checked form's zero value; it matches no
  assertion and no case, printf writes it `%!v(invalid)`, and it reaches
  C if passed on to an extern func, which the C helpers refuse.
- `a.(T) = 1` is parsed as an assignment to the assertion, which the
  resolver reports as `cannot assign to this expression`, and a statement
  that is an assertion alone is `expected 'statement'`.
## Errors

Errors follow the decisions recorded in the README: a returned value, a
code and a static message whose zero value is no error, `tack_error` in
C, and three rules, enforced by the compiler. These points were settled
by the implementation, for consistency with the rules already there,
rather than decided, and could go another way:

- `error` is a builtin type, reserved as a type's name like `string`, and
  a struct of two fields, `code`, an `int32` as `exit` takes, and `msg`, a
  `string`. Everything a struct does it does: a literal names both
  fields, `error{}` included, which is an error; its fields are written
  through a mutable variable, `e.code = 7`; `==` compares the code and
  the message's bytes; and it may be a local, a parameter, a field, an
  array's element, a mutable or immutable global, or a result anywhere a
  struct may.
- The error that is no error is `noerror`, a keyword like `true`: the
  struct literal, `error{code: 0, msg: ""}`, means the same but is long
  for every successful return, and Tack has no `nil`. It is a constant
  expression, and the zero value, so `mut e error` starts as it.
- The code alone says whether there is an error: `error{code: 0, msg:
  "x"}` is no error, and is not reported, though its message is never
  what a check reads.
- An error result cannot be dropped by `_` either, as the rule says, a
  binding of nothing but `_` included, which the resolver leaves to the
  type checker for a call whose last result is written `error`, rather
  than suggesting the call on its own. A call that returns an error
  alone is a value like any other: passing it, returning it or comparing
  it, `f() == noerror`, uses its error rather than dropping it.
- A check is a fact about `err.code` that bounds it to exactly 0, from
  the conditions every fact comes from, `if`, `else`, an early exit, a
  loop's condition, `&&`, `||`, `!` and `switch`, holding only in a
  region that never assigns `err`; `err == noerror` is not one. `exit`
  in a branch does not leave, as for any fact. `err.code` carries this
  fact even for a mutable `err`, though a field of a mutable variable
  carries none for a divisor or an index.
- A read is a use of the variable as a value, a `mut` argument, or the
  target of a compound assignment, `++` or `--`; assigning it whole, or a
  field or element of it, is not. The rule holds for every read in the
  variable's scope, before the binding that ties it to an error
  included, and a variable bound beside two errors, at two bindings,
  needs both checked.
- So that a check is about the error that came with the values, a
  binding that takes a result beside an error binds both to local
  variables, never to a parameter, a global, a field or an element, and
  an error variable that holds results is assigned only by a binding of
  a call that returns an error and assigns every variable it holds:
  plain assignment, a field's, `++`, a `mut` argument and a binding that
  leaves one out are errors. Without that, `err = noerror` before the
  check would let the failed call's values be read.
- An error must be the last result exactly; `[2]error` or a struct
  holding one anywhere in a result list is an ordinary value, to which
  none of the rules apply.
- printf takes no error, since `any` takes only the basic types.
- In C, `tack_error` and the helpers `TACK_ERROR(code, lit)`, whose
  message must be a literal, and `TACK_NO_ERROR` are in std's `tack.h`,
  reserved in a program's `tack.h`; `[]error` is `tack_view_error`.

## Lists

Lists follow the decisions recorded in the README: owned, move-only and
freed automatically, through `c_allocator`, the one allocator. These
points were settled by the implementation, for consistency with the
rules already there, rather than decided, and could go another way:

- `list` is a keyword, as `map` is, so `list[T]` parses wherever a type
  or a value may be written, `make`'s first argument included; a list
  type anywhere else a value is wanted is `cannot use list[int64] as a
  value; it is a type`. `c_allocator` is a keyword, a literal like
  `noerror`. `Allocator` is a builtin type's name, reserved as `error`
  is. `make` and `append` are generic builtins, like `len`, so they
  cannot be declared again.
- `Allocator` has no zero value, as a list has none, so neither do the
  structs and arrays holding one, and the checked form cannot read an
  element of one; the alternative, a zero handle that is no allocator,
  whose every append fails, would need no rule. It has no `==`. It is a
  constant expression, so an immutable global may hold `c_allocator`.
- A failed append's error is `error{code: 1, msg: "out of memory"}`.
- append is a variadic, `append(mut l list[T], elems ...T)`, under the
  variadic rules exactly; the name `elems`, which a diagnostic about a
  spread beside values names, is Go's. A mut value is `append does not
  take x as mut; pass x`, as for any function, where it said `v` before.
- A failed reserve's errors are `error{code: 1, msg: "out of memory"}`,
  as append's, when the allocator cannot grow the buffer or `len + n`
  overflows, and `error{code: 2, msg: "negative count"}` for a negative
  `n` at run time: a failure of its own, with the next code. The
  negative case is tested first, so a negative `n` never reaches the
  allocator. A negative `n` the checker knows, a constant or an
  immutable local holding one, is `reserve count must not be negative,
  got -1`, worded as an array length's is.
- reserve grows as append does, to at least double the capacity, from 4,
  and to exactly `len + n` when doubling falls short, rather than to
  exactly `len + n` always: reserving one element before each of many
  appends, as code building text does, then stays amortized constant
  time, where an exact reserve would move the buffer at every append.
  This is Rust's `Vec::reserve` and Go's `slices.Grow`; an exact
  reserve, Rust's `reserve_exact`, is not there.
- An append of several values, or of a spread, and a reserve, grow the
  buffer once, to the final length, and never call the allocator when
  there is room already, or when nothing is appended or reserved, so an
  empty list stays unallocated. A spread view's length is read once.
- A list's capacity grows from 4 by doubling, then to exactly what is
  needed when doubling would not fit, and never past what fits a `size_t`
  and a `ptrdiff_t`; `tack_list_grow_` fails, rather than wraps, beyond.
  Nothing shrinks a list.
- A move empties the variable it takes from, all of it, so its free
  later does nothing; the checker rejects every read of it, so this is
  never seen.
- Moving is from a whole variable only: a field, an element, a
  parameter, borrowed or `mut`, cannot be moved. `return l` moves `l`; a
  move inside a range loop over the same list is an error even where
  the loop then returns.
- A list, or a struct holding one, that no variable holds, a call's
  result or `make`'s, is allowed only where something takes it: the value
  of a declaration, an assignment, a return, a binding of several
  results or a struct literal's field. Read in place, `len(f())`,
  `total(f())`, `for x := range f()`, `f()[0]`, it is an error rather
  than a temporary freed after its statement; a call statement dropping
  one, and a binding putting `_` where one goes, are errors rather than
  frees.
- An argument that reads a list in place, borrowed or viewed, cannot
  share a call with a `mut` argument that may overlap it, a call nested
  in the arguments included: an array would be copied there (see views),
  but copying a list would allocate, at a call, with no error to give.
  Overlap is by path, two elements always overlapping, and a borrow of a
  struct holding a list counts whatever its `mut` overlap touches.
- A range over a list may write the list's elements in its body, which
  later iterations see, but not assign the list, or anything holding it,
  pass either as `mut`, or move it.
- A defer that reads a list moved later in its block is an error even
  when the list is assigned again before the block ends; deferred code
  cannot move a list declared outside it; a list moved before a defer
  cannot be read in it.
- A conditional move is tracked per path; a variable used after it may
  have been moved is reported once, at its first use, with a note at
  every move that reaches it.
- An extern func may take a list only as `mut` or as a view; by value,
  or as a result, a list or anything holding one is an error.
- A list, or a struct holding one, cannot be a global, mutable or not.
- `len(l)`'s facts end at an assignment of `l`, a `mut` argument of it,
  `append` and `reserve` included, though `reserve` changes no length, or
  a move; writing an element of a list ends no
  fact about any variable, since no element of a list is a subject.
- A variable that holds a list is never `const` in C. The C names are
  `tack_list_<id>` in the generated source and `tack_list_<element>` in
  `tack.h`, as for views.
- The driver links `std/builtin`'s `list.c` for a program with any list
  type, and std's objects defining a `tack_` symbol a program's own C
  object leaves undefined, so a C file calling std's list macros links
  what they call.

## Methods

Methods follow the decisions recorded in the README. These points were
settled by the implementation, for consistency with the rules already
there, rather than decided, and could go another way:

- A receiver may be followed by a comma, `func (b Builder,) len()`, as a
  parameter may. `func () m()` is `method has no receiver`, but `func ()
  {}`, with no name after the parentheses, keeps the missing function
  name's error. Two receivers are `method has multiple receivers`, and a
  variadic one `a receiver cannot be variadic`.
- A method's name follows a field's rule, so it may be `main`, a
  function's or a builtin's name. A method sharing a name with a field is
  `len is already declared as a field of Builder`, with a note at the
  field, and two methods of one type sharing one is `Builder.len is
  already declared`, with a note at the first; a method on another
  package's type, a basic type, `any`, `error`, an array, a view or a
  list is `cannot declare a method on X: only a struct type declared in
  this package can have methods`, X naming an array, view or list type by
  its kind. A receiver type bound to nothing is `unknown type: X`, as a
  parameter's is.
- A method's name in a diagnostic is its type's and its own, as the type
  is named there, `Builder.write`, or `lexer.Token.kind` from another
  package: in an argument count, an argument's type, a missing return, a
  dropped error and a pub method's exposure, `pub method Token.Get
  exposes hidden, which is not pub`. The wording for `mut` left out or
  written follows the parameter's, with the call to write as the fix:
  `Builder.write takes its receiver as mut; call mut b.write(s)` and
  `Builder.len does not take its receiver as mut; call b.len()`; for a
  receiver that is not a place, `Builder.write takes its receiver as mut;
  only a variable, a field or an element can be passed as mut`.
- A method named without a call is `write is a method of Builder, not a
  field; a method can only be called`, and a field called is `n is a
  field of Builder, not a method; a field cannot be called`. A method
  expression, `Builder.write(b, s)`, gets no message of its own: it is
  `cannot use Builder as a value; it is a type`, as any type name used
  as a value is.
- `mut` before a method call in parentheses, `mut (b.f())`, covers the
  whole call, not its receiver, and is reported as a mut argument that is
  not a place. A statement may start with a call whose result a method is
  called on, `makePoint().scale(2)` or `mut makePoint().scale(2)`, which
  the checker then reports as a temporary passed as `mut`.
- A `pub` method of a private or `lib` type is capped at its type's
  visibility without a diagnostic, as a `pub` field of one is.
- A method declared without a body is `method Builder.write has no body`,
  without the extern func suggestion a function's error makes, since a
  method cannot be extern.
- A binding of nothing but `_` from a method call that returns no error is
  reported by the type checker, not the resolver, with the resolver's
  wording, `every name is _, so nothing is bound; make the call on its
  own`, since which method is called is known only from the receiver's
  type.

## Interfaces and generic functions

Interfaces and generic functions follow the decisions recorded in the
README: interfaces only constrain type parameters, generic functions are
monomorphized, bodies are checked once against the constraints, type
arguments are inferred from the arguments and must be struct types, and
satisfaction is implicit. These points were settled by the
implementation, for consistency with the rules already there, rather
than decided, and could go another way:

- An interface's methods are separated as a struct's fields are, by a
  newline or `;`, and `interface {}` is allowed: it asks for nothing, so
  every struct type satisfies it as a constraint. Type parameters are
  separated as parameters are, the commas optional and a trailing one
  allowed, `[A I B J]` included.
- A method of an interface has no visibility of its own: it is as visible
  as its interface, and `pub` or `lib` written on one is `an interface's
  method cannot be pub; it is as visible as its interface`. Its
  parameters must be named, as a function's, no two alike and none `_`
  or a type's name, but may share a name with anything else, since no
  body can see them.
- Satisfaction across packages asks that the method be visible from the
  interface's package, which admits a `lib` method within one module as
  well as a `pub` one. Since nothing in package `main` can be `pub`, a
  type of `main` satisfies only `main`'s interfaces. A method satisfies
  an interface's only with the same parameters alike in being `mut` and
  variadic too, so `write(b ...uint8)` does not satisfy `write(b
  []uint8)`, though the parameter has one type, a view, in both. A type
  that does not satisfy an interface is reported for the first method,
  in the interface's order, it fails: `X has no method m`, `X.m takes its
  receiver as mut, and I's m does not` (or the other way round), `X.m is
  func(int32), and I's m is func(int64)`, `X.m is not pub, and I is an
  interface of another package`, or `X.m is lib, and I is an interface of
  module m`; a type parameter passed on is reported as `its constraint J
  has no method m`, or with the two interfaces' methods named, `J's m is
  ...`.
- A type parameter's name follows a parameter's rules: not reserved, not
  a package-level name or an import's, `duplicate type parameter name`
  for two, and no parameter or local may reuse it. Like a parameter, it
  may share a name with a function, in which case a call names the
  function and a type names the type parameter. A constraint is resolved
  where the type parameters are visible, so `[A I, B A]` reports `A is not
  an interface; the constraint of type parameter B must be one`, as `any`,
  a struct, a basic type or a view there does; a name of nothing is
  `unknown type: X`.
- `[W]` is `type parameter W has no constraint; write the interface it
  must satisfy after it, as in [W Writer]`, and `[]` is `empty type
  parameter list`, both parse errors. A method or an extern func with
  type parameters is a parse error too.
- A type parameter used as no parameter's type is an error at its
  declaration, `W appears in no parameter of f, so no call can infer it`,
  `main` included, which has no message of its own. A variadic `ws ...W`
  counts as a use, but is rejected by the view rules, `a variadic
  parameter cannot hold W: its elements are copied, and W, a type
  parameter, which may hold a list, cannot be`, since a type parameter is
  owned; the same holds for an array, a list or a view of one.
- Since a type parameter is owned, its parameters borrow and cannot be
  moved, and a local of one comes only from a call returning one; so a
  function can return a value of a type parameter only by passing on one
  a call gave it, and the only source is a call that never returns. A
  result of a type parameter is accepted, as decided, but no program can
  yet produce one usefully: something that makes values, or a consuming
  parameter, would.
- Only a struct type a package declares is a type argument: `error`, a
  builtin struct with no methods, is rejected even for an interface with
  none, `cannot use error as E in argument to f: a type argument must be
  a struct type a package declares`, and an untyped constant is an
  `int64` there. Two arguments for one type parameter that differ are
  reported at the later one, `cannot use B as W in argument to f: W is A
  in this call, from an earlier argument`, even when, in the body of
  another generic function, A and B might be one type at run time.
- A field of a value of a type parameter is `W has no field n: a type
  parameter has only its constraint's methods`, and a constraint's method
  named without a call `size is a method of W, not a field; a method can
  only be called`. A method call on one is named in a diagnostic by the
  type parameter and the method, `W.write expects 1 argument(s), got 2`.
- A call written with type arguments, `f[T](x)` or `p.f[A, B](x)`, is a
  parse error, `cannot write type arguments; they are inferred from the
  arguments, as in f(...)`, for any name followed by brackets and a
  parenthesis, which before was `expected ';' found '('`. A generic
  function used as a value is `undefined: f`, as any function is.
- `only struct types can be declared` now reads `only struct and
  interface types can be declared`, and a type declaration's missing
  kind `expected 'struct or interface'`.
- The instances are found from the bodies of every function that is not
  generic, methods and uncalled functions included, as every such
  function is emitted, then from each instance's body; a generic
  function no call reaches is checked but never emitted. Nothing is
  instantiated in a program with errors. Past 10000 instances the call
  that would add another is reported, `instantiating g with T6, T1, ...
  makes more than 10000 instances of generic functions, the most a
  program may have`, and the search stops: eight struct types passed to
  one function of eight type parameters that permutes them reaches it.
  The limit is a guess at what no hand-written program needs.
- An instance is emitted after every function that is not generic, its
  prototype after theirs, in the order the instances were found, under
  the C name of its function's name and type arguments, `f[pkg/path.T,
  main.U]`, mangled as one name in its function's package. A type
  parameter in the table takes an ID, which moves the IDs, and so the C
  names, of the array, view and list types after it, in programs with
  generic functions only.
- A value of a type parameter is moved where the checker sees a move;
  in an instance whose type argument holds no list the move is a copy
  and the variable is not emptied, which no program can observe, since
  the variable is not read again until it is assigned.
- The instances are recorded on the Program and on every file, as the
  type table is, so a program rebuilt from one checked file has them.

## Extern C

Extern C follows the rules recorded in [extern.md](extern.md). These
points were settled by the implementation rather than decided, and could
go another way:

- `extern` is a keyword reserved everywhere, as `from` is, rather than a
  word that means something only before `func`.
- An extern func whose C name C cannot define is an error: a C keyword
  (C23's, with `asm` and `typeof`), and `main`, which the generated C
  defines. A name a C header declares, such as `printf` or `int64_t`, is
  not checked; it clashes only with what a C file declares.
- A missing C definition is found when building, from the compiled
  objects' symbol tables; `--emit` does not compile C files, so it does
  not report one.
- One `tack.h` serves the whole program, its extern funcs package by
  package. `--emit-header` writes it into the target's directory and into
  every package directory of its module with C files or extern funcs; a
  std package with C gets none.
- C files are compiled with `-w`, as the generated C is, so clang's
  warnings about the user's own C are not shown; its errors are. There is
  no way yet to pass a C file other flags, or to link a library.
- tack.h's prototypes leave out the parameters' names, which could be C
  keywords or clash with a type's name; the Tack declaration is in a
  comment beside each.
- tack.h names an array type, which has no Tack name, `tack_array_`, its
  length and its element type's name (`tack_array_4_int64`,
  `tack_array_2_string`), and an extern func's results struct
  `tack_results_` and its C name, with members `r0`, `r1` and on.
- A struct's members in tack.h are its fields' Tack names, so a field
  named with a C keyword, or a type of package main named with one, is an
  error when an extern signature uses its struct, rather than being
  renamed. tack.h's own names, `tack_string` and the guard `TACK_H`, are
  errors for a type of package main too.
- A `mut` parameter is a plain `T *` in `tack.h`, not `T *restrict`,
  though Tack never passes two that overlap, so C compilers make no
  assumption Tack does not state elsewhere.
- A string C returns is taken as it is: nothing says who owns its bytes or
  how long they live, which the memory model will have to.
- The builtins' C bodies are C files of `std/builtin`, read from
  `$TACK_HOME/src/std`, compiled and linked like a package's, only the
  objects a program needs. Their C names are `tack_builtin_print` and
  `tack_builtin_exit`. `exit`'s body is not declared `_Noreturn`, as
  nothing depends on it.

## Standard library packages

`import fmt from "std/fmt"` reads `$TACK_HOME/src/std/fmt`, and std
packages follow the rules of any package. Some of what that implies was
decided by consistency and is open:

- A std package's C files are compiled as a program package's are, with
  the program's `tack.h` included first, not as `std/builtin`'s are, with
  `src/std/include` on the include path. So a std C file sees the
  program's `tack.h`, and std has two ways of writing C.
- std's own `tack.mod` is never read: the loader takes the name `std` for
  the directory `$TACK_HOME/src/std` whatever the file says, and only a
  directory at or below that path is in the module `std`. A path that
  reaches std through a symlink is another module, and its `module std`
  is the reserved-name error.
- `fmt.println`'s two `print` calls are two writes; nothing makes the
  line atomic, and neither reports an error.
- The wording for `lib` used from a single file is `x is lib in module
  std and cannot be used from a single file, which has no module`.
