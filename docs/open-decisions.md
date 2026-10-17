# Open language decisions

Questions the compiler currently answers by accident, and which need a
deliberate answer. Each entry says what happens today and why it matters.
When a decision is made, the entry is removed and the answer goes into the
README's "The language today".

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
- An index or a slice of a string holds the string by value, two words,
  while its index or bounds are evaluated. That is sound only while no
  string owns heap memory: once one can, an index or a bound that can
  free the bytes a string holds, by assigning it through a `mut`
  argument, needs the rule lists have (see Lists).
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
  a parameter or of a result`, at the name; a view written as any other
  type is `a view can only be the type of a parameter or of a result`, at
  the type.
- A switch on a view is `operator == is not defined on []int64`, even with
  no cases, since a switch compares with `==`.
- `len expects an array or a string` now reads `len expects an array, a
  string or a view`.
- Each view type is added to the type table when the checker first meets
  it, as an array is, which moves the IDs, and so the C names, of the
  types after it; only programs that use views see this.

## View results

Ahmed decided that a function's or a method's single result may be a
view, second-class like a view parameter: a call returning one is used
only inside the expression that consumes it, passed to a view parameter,
given to `len`, indexed with the checked form, sliced, ranged over, or
returned from a function returning a view. It borrows every argument of
its call that is not `mut` and holds data, the receiver included, under
the rule that keeps a list read in place from a `mut` argument that may
overlap it, and a range over it locks those places for the loop. A callee
returns only a view rooted in a parameter that is not `mut`. These points
were settled by the implementation rather than decided, and could go
another way:

- A plain parameter is passed by value in C, so an array parameter, or an
  array field or element of a plain struct parameter, is the callee's own
  copy, and a view of it would dangle once the call returns. Such an
  array is rejected, `cannot return a view of a: the function holds its
  own copy of a, and so of the array, which ends when it returns; ...`;
  an array is returned only through an element of a list or a view, which
  the caller holds. Returning a parameter's own array would need such a
  parameter passed by pointer, an ABI change.
- A union pattern's binding and a range's element variable that refer to
  a parameter's payload or element in place are rooted, as Ahmed decided,
  in what they refer into, so a union method can return a view of its
  payload's list; through a `mut` parameter or receiver, a local, a global
  or a call's result they are not, `cannot return a view of n.bytes: n
  refers into e, and e is a mut parameter; ...`. A range element of a
  copyable type is a copy, and stays a local.
- An index without `ok`, or a slice past `[:]`, `[0:]` and `[:0]`, of a
  call's view is never proven, since no fact is about a call; the error
  is the usual one for an unproven index or bound.
- A call's view is not allowed as what a range ranges over when sliced,
  `range t.bytes()[1:]`, as no view slice is.
- A call returning a view written as a statement drops the view, as
  Ahmed decided, as any call's result may be dropped.
- A view among several results is `a view can only be a function's only
  result`, rather than letting a multiple binding hold one.
- An extern func cannot return a view: `extern func f cannot return a view:
  nothing tells the checker what C's view points into`.
- Borrowing is conservative: every argument that is not `mut` and holds
  data, a struct with no list included, is borrowed, whether or not the
  callee could return a view into it, and a range over the view locks the
  whole of each borrowed place, so assigning an element of an array
  viewed through it is an error, where a range over a list allows one of
  copyable elements. A `mut` argument inside the view's own call does not
  count against it at the call outside, since that call runs before the
  view exists, and is checked at its own call.
- A call returning a view of values a variadic call packs, `return
  all(1, 2)`, is rejected: the values live on the callee's stack.
- `[]uint8(s)` passed to a view-returning call is not borrowed: a string's
  bytes are static and assigning the string never frees them.
- The checked form of an owned element of a call's view, `x, ok :=
  w.view()[0]`, suggests a range, `range over w.view() to use its elements
  in place`, rather than a guard no fact can satisfy.

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
  loop's condition, `&&`, `||`, `!` and `switch`, holding up to the
  first write to `err` that can reach the read; `err == noerror` is not one. `exit`
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
  `append`, `reserve`, `pop` and `remove` included, though `reserve`
  changes no length, or a move, each when it can reach the use; a
  successful `append`, its error proven 0, proves `len(l)` at least the
  count it added, as Ahmed decided; writing an element of a list ends no
  fact about any variable, since no element of a list is a subject.
- A variable that holds a list is never `const` in C. The C names are
  `tack_list_<id>` in the generated source and `tack_list_<element>` in
  `tack.h`, as for views.
- A list is read in place while its index is evaluated, so an index, or
  a slice bound, that passes as `mut` the list it reads, a place holding
  it, or a part of it, at any depth, is an error, as an argument read
  alongside an overlapping `mut` argument is: `cannot index l alongside
  mut l in its index: a list is not copied to be indexed, and a mut
  argument must be the only way to reach l during its call`, `cannot
  slice l alongside mut l in its bounds: ...`, and, for a view a call
  returns, `cannot index first(l) alongside mut l in its index: the
  view it returns may point into l, ...`. A part counts, `l[set(mut
  l[0])]` included, though writing an element moves no buffer: an
  array's index reads the array as it was before the index, and a list
  cannot be copied to do the same. An array is unaffected. The unchecked
  index, an element being assigned, and a slice bound are covered as
  well as the checked index, each reported once, instead of any proof
  the index needs.
- An element of a list being assigned, `l[i] = v` or `l[i] += v`, at any
  depth of the target, is an error when `v` passes as `mut` the list
  indexed or a place holding it, `cannot assign to l[0] alongside mut l
  in its value: an element is assigned in place in its list, and a mut
  argument must be the only way to reach l during its call`. A `mut`
  argument inside that list, `ll[0] = make_list(grow(mut ll[0]))`, is
  allowed: it cannot move the list's own buffer.
- The driver links `std/builtin`'s `list.c` for a program with any list
  type, and std's objects defining a `tack_` symbol a program's own C
  object leaves undefined, so a C file calling std's list macros links
  what they call.

## Lists of owned elements

Ahmed decided that a list may hold owned elements, lists and structs and
unions holding them, nested to any depth, each read in place as a
struct's list field is, never copied out, moved in by `append` and by
`l[i] = v`, and taken out only by the new builtins `pop` and `remove`,
whose plain forms need a proof and whose checked forms do not; that a
range binds such an element in place; that a view may hold them while
arrays and variadics may not yet; and that spreading such a list into
`append` is an error. These points were settled by the implementation,
for consistency with the rules already there, rather than decided, and
could go another way:

- The plain forms, `x := pop(mut l)` and `x := remove(mut l, i)`, are
  proven as an index is. Ahmed decided the proof rules that make them
  usable: a fact holds up to the first write to its variable that can
  reach the use, so the call's own `mut` argument, which takes effect
  after the call's check, does not end the fact it relies on, and `if
  len(l) > 0 { x := pop(mut l) }` and `if d != 0 { d = n / d }` compile.
- In the checked form, `x` is the element type's zero value when `ok` is
  false, as for the other checked forms. A type with no zero value, any
  owned type, a union or an `Allocator`, gives nothing: `x` is held by `ok` as a
  result is by the error beside it, and cannot be read until `ok` is
  checked true, `x cannot be read before ok is checked; check that ok
  is true first`. In C, `x` is then zeroed, so freeing it does nothing.
  The rules that keep an error's check about its results keep `ok`'s
  about `x`: both are locals, `s.x cannot take the element pop gives
  beside its ok; bind it to a local variable`, `ok` is assigned only by
  a binding that assigns `x` too, or passed as `mut`, `ok says whether
  pop gave x, so only a binding that also assigns x can assign it`, and
  `ok` is never `_` when it holds `x`, `the ok pop returns cannot be
  dropped: x cannot be read until it is checked; bind it to a variable
  instead of _`. An `x` of a type with a zero value is read freely.
- The check is a fact about a bool variable, which facts did not have:
  a bool variable written alone as a condition gives one, `if ok` that
  it is true in its branch, `!ok` that it is false, combined with `&&`,
  `||` and early exits as every fact is. `ok == true` gives none, as
  only comparisons with a known integer do. Only an `ok`'s check asks
  about such a fact.
- Dropping an element: Ahmed decided that `pop(mut l)` and `remove(mut
  l, i)` written as a statement need no proof, drop the element when
  there is one, and free it when it is owned. Bound to `_`, an owned one
  is `pop returns list[int64], which cannot be dropped; bind it to a
  variable instead of _`; `_, ok := pop(mut l)` is allowed for a
  copyable one. `_, _ = pop(mut l)` stays the resolver's `every name is
  _, so nothing is bound; make the call on its own`, the statement being
  the way to drop.
- `pop` and `remove` are generic builtins, as `append` and `reserve` are:
  a declaration of either is `duplicate declaration of builtin
  function`, `builtin.tck` cannot declare them, and a method may share
  either name. `remove`'s index is an `int64`, as `reserve`'s count is,
  so a narrower integer widens to it and a `uint64` is `cannot use
  uint64 as int64 in argument to remove`. A negative constant index is
  `index -1 is out of range for a list` in the checked form too, as an
  index into a list is: a failure known at compile time is still an
  error. Three names are `assignment mismatch: 3 variables but pop
  gives 2 values`.
- A type parameter still cannot be a list's or a view's element, as
  before: a type parameter cannot appear in any composite type, so
  `list[W]` was and is an error, now worded `a list cannot hold S: a
  list's element cannot be a type parameter` (`a view's` for a view),
  since its elements are no longer copied. An array's and a variadic's
  keep their old wording.
- A type holding itself through a list is reported as one holding
  itself by value is, `invalid recursive type Tree: a type cannot hold
  itself, even through a list`, with the same notes, though such a type
  would be finite: how it would be built, freed and laid out in C, and
  whether trees should be written that way, is not settled. Before, the
  owned element was the error.
- A range binding of an owned element refers to it in place, as a
  pointer in C, so while the loop runs nothing may change it: the body
  changes no element of the list, or any part of one, since any element
  may be the one bound, `l is being ranged over, and x refers into it,
  so this loop cannot assign l[i]` (`pass mut`, `move`), a compound
  assignment or `++` included. A binding of `_` refers to nothing, and
  the body may then write elements, as before. The binding cannot be
  moved, `cannot move x: it refers to an element of l in place, and an
  element cannot be moved out of a list`.
- A borrowed element overlaps a `mut` argument at the list's level, as
  any two elements may: `f(l[0], mut l[1])` is `cannot pass l[0]
  alongside mut l[1] in this call: ...`, whatever the indexes, which the
  borrow rule already said. `append(mut l, l[0])` is the index proof
  error, the `mut l` ending the guard's fact, before it can be the
  move-out error, which an append to another list gives.
- When an append of owned values cannot grow the buffer, it frees the
  values, last first, which no variable holds any more after the move,
  and gives its error; the list is left as it was. The values are held
  in C temporaries for that.
- A list frees its elements last first, then its buffer, through one
  `tack_free_` function per list type of owned elements, as a struct
  frees its fields last first.
- `pop` and `remove` lower to new macros in std's `tack.h`,
  `TACK_LIST_POP(lp)` and `TACK_LIST_REMOVE(lp, i)`, which C may call
  too; `remove` moves the rest down with a loop rather than `memmove`, so
  `tack.h` still includes only `<stdbool.h>`, `<stddef.h>` and
  `<stdint.h>`.
- A list or a view whose elements are owned does not cross to C, as a
  `mut` parameter, a view, or inside a struct, `parameter l of extern
  func f cannot be list[list[uint8]]: a list or a view whose elements
  hold lists cannot cross to C yet`: how C would name such a type in
  `tack.h`, and free or take its elements, is not settled.
- The wording of the new errors follows the nearest existing ones: the
  move out of an element follows the field's, `cannot move l[0] out of
  l: an element cannot be moved out of a list; take it out with pop or
  remove` (`... out of a view, which only borrows it`); the checked form
  of an owned element, `the checked form cannot read an element of type
  list[int64]: it would copy it out of l; check 0 <= i && i < len(l)
  first and use l[i] in place`; a spread follows the old element error,
  `cannot spread other into append: its elements would be copied, and a
  list cannot be`; and pop's missing proof follows an index's, `pop may
  find l empty; check len(l) > 0 first, or use x, ok := pop(mut l)`.

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
  package's type, an interface, a basic type, `any`, `error`, an array, a
  view or a list is `cannot declare a method on X: only a struct or union
  type declared in this package can have methods`, X naming an array,
  view or list type by its kind. A receiver type bound to nothing is `unknown type: X`, as a
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
satisfaction is implicit.

Ahmed decided where a method's visibility is checked for satisfaction:
where the type argument is chosen. At a call that binds a type parameter
to a struct type, each method the constraint asks for must be visible
from the package holding the call, under the usual rules (private within
its package, `lib` within its module, `pub` anywhere), as if that code
called the method itself; the interface's package plays no part, and the
interface need only be nameable where it is written. So `main` passes
its own types, private methods and all, to a library's generic
functions, and a package cannot pass another's type whose method it
could not call. Where the type argument is the enclosing generic
function's own type parameter, passed on, nothing is checked for
visibility: its constraint must hold every method the callee's asks
for, so the outermost call binding a struct type checked them all,
through any chain of generic functions. This replaces the rule first
implemented, visibility from the interface's package, under which a type
of `main` satisfied only `main`'s interfaces. The wording follows the
method-call diagnostics, with `here` marking that the method might be
visible from another package: `X does not satisfy I here: m is not pub
in pkg.X`, `X does not satisfy I here: m is lib in module M and cannot
be used from module N`.

These points were settled by the implementation, for consistency with
the rules already there, rather than decided, and could go another way:

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
- A method satisfies an interface's only with the same parameters alike
  in being `mut` and variadic too, so `write(b ...uint8)` does not
  satisfy `write(b []uint8)`, though the parameter has one type, a view,
  in both. A type that does not satisfy an interface is reported for the
  first method, in the interface's order, it fails: `X has no method m`,
  `X.m takes its receiver as mut, and I's m does not` (or the other way
  round), `X.m is func(int32), and I's m is func(int64)`, or one of the
  visibility messages above; a type parameter passed on is reported as
  `its constraint J has no method m`, or with the two interfaces' methods
  named, `J's m is ...`.
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
- A result of a type parameter was decided against: since a type
  parameter is owned, its parameters borrow and cannot be moved, so a
  function could return a value of one only by passing on what another
  such call returned, and no call could start the chain. `f's result
  cannot be its type parameter S: a generic function has no way to
  produce a value of S`. Something that makes values of a type
  parameter, or a parameter that consumes its argument, would make such
  results useful, and would reopen the rule.
- Only a struct or union type a package declares is a type argument:
  `error`, a builtin struct with no methods, is rejected even for an
  interface with none, `cannot use error as E in argument to f: a type
  argument must be a struct or union type a package declares`, and an
  untyped constant is an `int64` there. Two arguments for one type parameter that differ are
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

## Unions

Unions follow the decisions recorded in the README: named cases with one
payload or none, `Expr.int(5)` and `Expr.eof` to build one, a switch
whose clauses name cases and bind a payload in place, every case covered
or a default, `==` when every payload has it, owned when a payload holds
a list, no recursion by value, no zero value, cases as visible as their
union, and no extern crossing, `any`, conversion or tag values. These
points were settled by the implementation, for consistency with the
rules already there, rather than decided, and could go another way:

- A union with no cases, `union {}`, is allowed, as `struct {}` and
  `interface {}` are. It has no value, so a function taking one can never
  be called; a switch on one needs no clause and covers every case, and
  its C tells the compiler it is never reached,
  `__builtin_unreachable()`.
- A case's name follows a field's rule: it may share a name with a type,
  a function, a variable, a builtin or a method, `len` or `print`, and
  with a case of another union, but not be `_` or a basic type's name,
  and is declared once, `duplicate case name`, with the field's note.
  `type X int64` now reads `only struct, interface and union types can
  be declared`, and a missing kind `expected 'struct, interface or
  union'`; a union written where a type is, `anonymous unions are not
  supported`, as a struct's is.
- Building takes a call's wording where it is one: `Expr.int expects 1
  argument(s), got 2`, `cannot use string as int64 in argument to
  Expr.int`, `cannot use ... in a call to non-variadic Expr.int`. The
  wrong form of a case is `Expr.eof has no payload; write Expr.eof` and
  `Expr.int needs its int64 payload, as in Expr.int(...)`; a case the
  union lacks is `Expr has no case nope`, in a construction and in a
  switch. A union built as a statement is `Expr.int(5) is not used`: no
  wording for a value left unused existed. A conversion or `len` written
  as a statement now takes the same wording; see the next section.
- A payload case named alone, `case int:`, binds nothing, as a clause
  naming several cases does, and is allowed in a clause of its own.
  `case int(_)` is `every name is _, so nothing is bound; write case
  int`, as a binding of nothing but `_` is reported elsewhere. A binding
  on a case with no payload, `case eof(x)` or `eof(_)`, is `eof has no
  payload to bind; write case eof`, and one in a clause naming several
  cases `a clause naming several cases binds nothing; write int, or give
  int(n) a clause of its own`; the name is not declared in the body, so a
  use of it is not reported as well.
- A binding is a variable as a type switch's is: it may reuse no name
  visible at the switch, `n is already declared` with a note at it, `fmt
  is an imported package and cannot be redeclared`, `cannot use int64 as
  a name; it is a type`; and one its clause never reads is `declared and
  not used: n`, at the binding. Both are reported by the type checker,
  not the resolver, since only it knows the switch's tag is a union.
- A value of a switch's case is a pattern or a value by its names, since
  the resolver cannot know the tag's type: written as `name` or
  `name(x)`, its last name a variable or a parameter visible at the
  switch, it is a value, and in a union switch it is then a pattern whose
  binding is already declared; any other is a pattern, and in a switch on
  anything else it is reported as a value whose names name nothing,
  `undefined: n`, or name a type, a package or no function, `cannot use P
  as a value; it is a type`, `undefined: g`. One consequence: a case
  named alone like a variable visible at the switch, `case name:` with a
  local `name`, counts as a read of that variable, so a variable used
  nowhere else is not reported as unused. A value of any other shape in
  a union switch is `a case of a switch on Expr names one of its cases,
  as in case int(v)`, the example naming the union's first case.
- The rule against changing a matched place holds only in a clause that
  binds a payload, for the whole of its body, deferred code included;
  its wording follows the range loop's, `e is being matched, and n
  refers into it, so this case cannot assign e`, `... cannot pass mut e`,
  `... cannot move e`, and holds for anything holding the place, `s` for
  `s.u` and `us[1]` for `us[0]`, elements comparing as a range's do. A
  binding cannot be moved, `cannot move l: it refers to a union's
  payload in place, and a payload cannot be moved out of a union`.
- The tag of a switch on a union holding no list may be any value, a
  call's result included, which C holds in a temporary. One holding a
  list must be a place, as a range over a list must: a temporary is
  `cannot use a temporary Value here; store it in a variable first`. A
  mutable global is matched through a copy taken when the switch begins,
  since a call in a clause can assign a global without naming it: its
  binding keeps the payload the switch matched.
- A switch on a union covering every case is a terminating statement, and
  leaves its block when every clause does, as one with a default does.
  The error for cases it misses is at its header, before anything in its
  clauses, `switch on Expr misses cases name, eof; cover them or add a
  default` (`misses case eof; cover it` for one), and the error for a
  default that never runs at the default, `every case of Expr is already
  covered, so this default never runs`. A case named twice is `int
  appears twice in this switch`, with a note at the first, as a type
  switch's type is.
- Like a struct, a union may be a field, an array's element when it
  holds no list, a list's element, a result, and a package-level variable
  when its initializer is a constant expression and it holds no list. It
  may hold another union, and a switch may be on a clause's binding.
- A union crossing to C is reported for every parameter or result that
  is one or holds one, through a struct, an array, a view or a list, a
  `mut` parameter included, `parameter e of extern func f cannot be Expr:
  a union cannot cross to C yet`, and that error replaces the list one
  for a union holding a list.
- printf given a union reports it twice, as it does a struct: `cannot
  pass U as any: ...` and `printf verb %v does not take U`.

## Methods on unions

Ahmed decided that a union type may have methods under exactly the rules
for a struct type's, and so satisfy interfaces and be a type argument.
These points were settled by the implementation, for consistency with
the rules already there, rather than decided, and could go another way:

- A method may not share a name with a case of its union, as a struct's
  may not with a field: `int is already declared as a case of Expr`,
  with a note at the case, a case without a payload included. It may
  share one with a case of another union. So nothing a program writes
  is ambiguous: `Expr.int(5)` and `Expr.eof`, through the type, always
  build a case, and `e.m()`, through a value, always calls a method.
- A case reached through a value gets no wording of its own: `e.int(5)`
  is `Expr has no method int`, and `e.int` stays `Expr has no field
  int`. A method named through the type, `Expr.pos(e)` or `Expr.pos`, is
  a construction of a case the union lacks, `Expr has no case pos`,
  where a struct's method expression is `cannot use Builder as a value;
  it is a type`.
- The rejection of other receivers now reads `cannot declare a method on
  X: only a struct or union type declared in this package can have
  methods`, and of other type arguments `cannot use T as S in argument
  to f: a type argument must be a struct or union type a package
  declares`. A union passed for a type parameter is then checked for
  satisfaction like a struct: `U does not satisfy Sized: U has no method
  size`.
- A union built in place is no place. `mut Expr.eof.set()` is `only a
  variable, a field or an element can be passed as mut`, as `mut
  Expr.int(1).set()` is, and `Expr.eof.set()` for a `mut` receiver is
  `Expr.set takes its receiver as mut; only a variable, a field or an
  element can be passed as mut`, rather than suggesting `mut` before
  it. Before this change the first reached code generation unreported,
  since `Expr.eof` reads as a field of a name, and no union had a method
  to call.
- A `mut` method called on the place a switch matches, in a clause
  binding a payload of it, is that place passed as `mut`: `e is being
  matched, and n refers into it, so this case cannot pass mut e`, also
  when the binding is passed alongside, `mut e.take(n)`, which is
  reported once, for the receiver. A plain method call there reads the
  place, and is allowed with the binding passed to it, `e.read(n)`.
- Inside a `mut` method the receiver is a `mut` parameter, a place like
  any other: a switch on it binding a payload keeps that clause from
  assigning it, `e is being matched, and n refers into it, so this case
  cannot assign e`, while a clause binding nothing may, and the new
  value is otherwise assigned after the switch.
- A plain receiver of a union holding a list borrows it, as a plain
  parameter does: in C it is passed by value, as a struct holding a list
  is, and never freed by the method. A `mut` receiver assigned frees
  the old payload's lists once the new value is computed, as any
  assignment does.
- In a generic function's body a value of a type parameter still has
  only its constraint's methods: a switch on one is an expression
  switch, `operator == is not defined on S`, even where every call
  passes a union.
- An instance's C name writes a union type argument as it writes a
  struct, its package's path, a period and its name, `measure[main.E]`;
  the two kinds share one namespace per package, so no two type
  arguments are written alike.

## Statements that only give a value

A conversion or a call of `len` written as a statement, `int8(5)` or
`len(s)`, was accepted and lowered to a call of a C function that does
not exist, so clang rejected the generated C. It is now rejected as a
union built as a statement is, `int8(5) is not used` and `len(s) is not
used`, quoting the statement as written. These points were settled by
the fix, for consistency with the union wording, and could go another
way:

- The wording is Tack's `X is not used`, not Go's `X (value of type
  int8) is not used`; the type is left out, as the union's wording
  leaves it out.
- A conversion whose operand does something, `int8(f())`, is still `not
  used`, as Go reports it, rather than allowed for the call's effect;
  `f()` alone is the statement that keeps it. The same holds for
  `len(f())`.
- A statement with an error of its own is reported for that alone:
  `int8(x)` of an `int64` x is `converting int64 to int8 can fail; use v,
  ok := int8(...)`, and `len(5)` is len's error, with no `not used`
  after either.
- `make` as a statement keeps `make returns list[int64], which cannot be
  dropped; bind it to a variable`; `print`, `exit`, `append` and
  `reserve` keep their rules as calls, `append` and `reserve` having an
  error that cannot be dropped. A view conversion, `[]uint8(s)`, cannot
  start a statement, which the parser reports as `expected 'statement'
  found '['`.

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

## std/bytes

`std/bytes`'s shape, its set of functions and the naming convention were
decided. These points of its edges were settled by consistency, with Go's
`bytes` package where it applies and with the builtins where Go panics,
and could go another way:

- `index` of an empty sub is 0, an empty s included, and `has_prefix`
  and `has_suffix` of an empty one are true, as in Go.
- `count` of an empty sub is `len(s) + 1`. Go's `bytes.Count` returns the
  number of UTF-8 characters plus one; Tack's bytes are bytes, so the
  consistent count is of bytes.
- `count` counts non-overlapping occurrences, found from the start, as
  Go's does: `"aa"` occurs twice in `"aaaa"` and twice in `"aaaaa"`.
- `append_hex` writes lowercase digits with no `0x` prefix, as printf's
  `%x` does, and takes a `uint64`; there is no signed hex form.
- A failed append function returns the builtin append's error,
  `error{code: 1, msg: "out of memory"}`, and leaves the list as it was.
- `repeat` with `n` of 0, or an empty `s`, returns an empty list and no
  error, allocating nothing; an empty `s` with any non-negative `n`,
  `int64`'s largest included, returns at once.
- Where Go's `bytes.Repeat` panics, `repeat` returns the builtin
  reserve's errors with an empty list, having allocated nothing: a
  negative `n` is `error{code: 2, msg: "negative count"}`, checked before
  anything else, so an empty `s` with a negative `n` is an error too, and
  a length `len(s) * n` that overflows an `int64`, or that the allocator
  cannot give, is `error{code: 1, msg: "out of memory"}`.
- The list `repeat` returns beside an error is the empty list `make`
  gives, not the absence of one, since a list has no zero value; the
  caller must still bind it, and cannot read it before checking the
  error.

## std/text

`std/text`'s shape was decided: a `Text` with a private `list[uint8]`,
`new`, a constructor from a view, `len`, `equal`, `hash` as 64-bit FNV-1a,
`append`, `append_byte` and `bytes`, a read-only view. These points were
settled by the coordinator or by consistency with `std/bytes` and the
builtins, and could go another way:

- The constructor is `from_bytes`, not `from`: `from` is a keyword,
  reserved everywhere, so `text.from` is `expected 'field name' found
  'from'`. Making `from` a keyword only in an import would free the name;
  `from_bytes` leaves room for a later `from_string`.
- `hash` computes FNV-1a's XOR with `^`. It computed it with `%`, `/` and
  `+`, eight steps a byte, until Tack had bitwise operators, with
  identical results.
- A failed `from_bytes` returns the empty Text it was building, which
  allocated nothing, beside `error{code: 1, msg: "out of memory"}`, as
  `bytes.repeat` returns an empty list; the caller must still bind it,
  and cannot read it before checking the error.
- `new` allocates nothing and cannot fail, as `make` of a list does.
- A failed `append` or `append_byte` returns the builtin append's error
  and leaves the Text as it was.
- `equal` compares bytes exactly, with no case folding or normalization,
  and `len` counts bytes, as nothing in std knows UTF-8.

## std/os

`std/os`'s shape was decided: thin C, one system call per extern, with
every byte in lists Tack owns; paths and names as `[]uint8` views; text
results as `text.Text`; the generated `main` taking `argc` and `argv`
and storing them in `tack_argc_` and `tack_argv_`; code 3 for an OS
failure with a literal message per errno; code 4 for a 0 byte in a path
or name; `list_dir` as one extern, so no `DIR *` reaches Tack; and, for `run`,
an `Allocator` as its last parameter, for its own lists only, code 5 for
a child killed by a signal, the outputs' lengths and bytes left as they
were on any error, an empty `argv` and a 0 byte in an argument as code
4, a status other than 0 as no error, waiting for the child after an
error without killing it, at most 8192 arguments, and no timeout.
These points were settled by consistency and could go another way:

- The file and directory remover is `remove_path`, since `remove` is a
  builtin and a package-level function cannot take a builtin's name.
- An OS error's message is macOS's strerror text, the same on every
  target, so a program's output does not depend on the C library. An
  errno the table does not list is `"unknown OS error"`, and the errno
  itself is not kept. `ENOMEM` from the OS is `error{code: 1, msg: "out
  of memory"}`, as an allocation failure is.
- A 0 byte in an environment variable's name is `"name contains a NUL
  byte"`; in a path, `"path contains a NUL byte"`.
- A path or a name too long for the C library's `PATH_MAX` is code 3,
  `"File name too long"`, without asking the OS.
- `env` of an empty name is unset, not an error, as `getenv` gives.
  `env` copies the value twice, into a list and then into a Text, since
  std/text has no constructor that takes a list over.
- `read_file` makes room for 65536 more bytes before every read, so its
  list's capacity may be up to that much over its length.
- A failed `close` after reading is not reported; after writing it is,
  since it can mean the bytes did not reach the file.
- `write_file` is not atomic: an error after the file is opened leaves it
  created or truncated, holding some of the data.
- `remove_path` is C's `remove`, so whether a directory is tried with
  `rmdir` after `unlink` fails is the C library's choice.
- `make_temp_dir` trims the slashes `$TMPDIR` ends in, and treats an
  empty `TMPDIR` as unset; its directory's mode is mkdtemp's, 0700. When
  making its Text fails, it removes the directory it made.
- `list_dir`'s sort is bytewise, with no locale or case folding, so
  capitals sort before lowercase and a name before every longer name it
  begins.
- `run`'s status on an error is -1, though no caller can read it before
  checking the error, so it is never seen.
- `run` makes room in `out` before `err_out`, so when only `err_out`
  cannot grow, `out`'s capacity has grown, though its length and bytes
  have not: two lists cannot grow atomically together.
- `run` gives the child the process's whole environment; there is no way
  to set or remove a variable for one child.
- A follow-up: `text.from_list(l list[uint8]) Text`, taking a list over
  with no copy, would let `env`, `args`, `list_dir` and `make_temp_dir`
  skip their second copy of each value. std/text has no such
  constructor today.

## std/path

`std/path`'s shape was decided: pure Tack, `/` only, after Go's `path`
package, with `dir`, `base` and `ext` returning views where they can,
`is_abs`, and `join` and `clean` returning a `text.Text` through an
`Allocator`. These points were settled by consistency and could go
another way:

- `base` and `dir` return a `text.Text` and take an `Allocator`, not a
  view: `base("")` is `.` and `dir("a")` is `.`, which `p` does not
  hold, and `dir` cleans what it returns, so `dir("a/./b/c")` is `a/b`,
  not a part of `p`. Only `ext` returns a view.
- `join` takes two elements, not a variadic list as Go's does; joining
  more is `join` applied again, which cleans each time.
- `join` of two empty elements is an empty Text, not `.`, as Go's
  `path.Join` gives.
- `clean` and the functions built on it copy their result twice, into a
  list and then into a Text, since std/text has no constructor that takes
  a list over (see std/os's `text.from_list` follow-up).
- Every behavior is Go's, checked against Go's `path` package on its
  documented examples and on random paths.
