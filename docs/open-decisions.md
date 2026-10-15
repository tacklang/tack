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
  can only be passed on; every Tack-side operation is an error until the
  type assertion and type switch are designed.
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
