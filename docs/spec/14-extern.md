# 14. Extern C

This chapter describes Tack's unsafe: the `extern func` declaration, a
function's C name and which names C can declare, extern visibility, the
C files of a package and how they are built and linked, the ABI (how
each Tack type is passed to and returned from C, `tack.h` and
`--emit-header`), lists, views, strings, `any` and errors across the
boundary, program arguments, the helpers C may call, the trust boundary
and what the guarantees do and do not cover behind it, and the
missing-definition check. It follows [extern.md](../extern.md). The
guarantees themselves belong in [Safety and proofs](10-safety.md).

An extern func is a Tack function whose body is written in C. Its
declaration is a promise about what the C body does, which the compiler
cannot check beyond its types. A program whose C side breaks such a
promise is outside the guarantees of [Safety and
proofs](10-safety.md), which cover Tack code only; nothing they prove
survives a trip through C.

Examples in this specification are single files with no C beside them,
so an example that needs a C file is a fragment, and C code is shown as
text. A C header is named by its path, `stddef.h`, without the angle
brackets C writes around it. `tack --check` builds nothing, so it does not look for C
definitions; the error examples below are rejected before any C is
needed.

## Declaring an extern function

```tack fragment
extern func add(a int64, b int64) int64
extern("c_twice") func twice(x int64) int64
```

`extern func name(params) results` declares a function and gives it no
body; its body is the C function of the same name, or of the name
`extern("...")` gives (see [C names](#c-names)). It is called as any
other function is.

- `extern` is a keyword, reserved everywhere, and is written only before
  `func` or before a C name in parentheses: `extern var` is `expected
  'func' found 'var'`, and `extern(add)` is `expected 'C name in quotes'
  found 'add'`.
- An extern func has no body in Tack: `extern func f cannot have a body;
  its body is written in C`.
- An extern func has no type parameters: `extern func f cannot have type
  parameters; its body is written in C`.
- A function without a body that is not extern is `function f has no
  body; only an extern func is declared without one`.
- `main` cannot be extern (see [Packages, modules and
  programs](12-packages.md)).
- A method cannot be extern: `a method cannot be extern; declare an
  extern func and call it from the method`.
- An extern func is not a value: `cannot use now as a value: it is an
  extern func, and a function value does not cross to C`.

```tack error
package main

extern func f(x int64) int64 {
    return x
}

func g(x int64) int64

extern func now() int64

func main() {
    h := now
}
```

```diagnostics
main.tck:3:30: error: extern func f cannot have a body; its body is written in C
main.tck:7:6: error: function g has no body; only an extern func is declared without one
main.tck:12:5: warning: declared and not used: h
main.tck:12:10: error: cannot use now as a value: it is an extern func, and a function value does not cross to C
```

## C names

An extern func's C name is its Tack name unchanged, or the string
`extern("c_name")` gives. A C name C cannot define is an error at the
declaration, at the string when one is given:

- one that is not a C identifier: `extern func f cannot be named "two
  words" in C: it is not a C identifier`;
- a C keyword, of C23's set or `asm` or `typeof`: `extern func while
  cannot be named while in C: it is a C keyword`. A Tack name that is a C
  keyword needs `extern("name")`;
- `main`: `extern func f cannot be named main in C: the program's C main
  is the one that runs Tack's`;
- outside the standard library, a name starting `tack_`: `extern func f
  cannot be named tack_f in C: names starting tack_ are reserved for the
  standard library`;
- a name that `tack.h` reserves (see [The ABI](#the-abi)).

```tack error
package main

extern func tack_d(x int64) int64
extern func while(x int64) int64
extern("two words") func e(x int64) int64

func main() {
}
```

```diagnostics
main.tck:3:13: error: extern func tack_d cannot be named tack_d in C: names starting tack_ are reserved for the standard library
main.tck:4:13: error: extern func while cannot be named while in C: it is a C keyword
main.tck:5:8: error: extern func e cannot be named "two words" in C: it is not a C identifier
```

Two extern funcs of one program, in any packages, with one C name are
an error at the second, with a note at the first: `extern funcs add and
util.plus both have the C name add`. A name a C header declares, such
as `printf` or `int64_t`, is not checked; it clashes only with what a C
file declares.

A C name never clashes with a name the generated C declares: the
generated C declares each extern func under a name of its own and binds
it to the C name.

## Visibility

An extern func is never `pub` or `lib`: `an extern func cannot be pub;
export a Tack function that calls it`, and likewise for `lib`. In a
`main` package, where neither keyword may be written, the error is the
one for any declaration there (see [Declarations and
scope](05-declarations.md)). A package that binds C exports Tack
functions that call its extern funcs, so the Tack side of each promise
is kept in one place.

## The build

- Every file whose name ends in `.c` directly in a package's directory
  is compiled and linked with the program. A file compiled on its own
  has the `.c` files of its own directory. Files and directories whose
  names start with `.` are skipped. A `.c` file is compiled and linked
  whether or not an extern func of its package uses it.
- A C file whose name starts with `-` is an error: `tack: file -x.c in
  dir starts with -, which clang could read as an option; rename it`.
- Each C file is compiled on its own with clang, at the build's
  optimization level, with `-w`, so clang's warnings are not shown, and
  with `-fno-finite-loops`, so clang never assumes a loop ends. Its
  errors are shown, followed by `tack: cannot compile helpers.c: clang
  failed: exit status 1`. There is no way yet to pass a C file other
  flags, or to link a library.
- The compiler generates one `tack.h` for the program, which declares
  the types the extern funcs' signatures use and a prototype for each
  extern func under its C name, with its Tack declaration in a comment
  beside it. A build writes it into its own temporary directory, never
  into the source tree, and compiles every C file with `tack.h`
  included before the file's first line, so clang checks every
  definition against its extern func whether or not the file includes
  `tack.h`. A definition that does not match is clang's `conflicting
  types`.
- `tack --emit-header` writes `tack.h` into the target's directory and
  into every other package directory of its module with C files or
  extern funcs, never a standard library package's, replacing any
  `tack.h` there, and stops. Builds never read that copy.
- `tack.h` holds only what is the program's own. What every program
  shares is the runtime header, `tack/tack.h`, built into the
  compiler, which `tack.h` includes when a signature uses a string, an
  error, `any`, a list or an `Allocator`. A build compiles against its
  own copy of the runtime header; `make install-std` writes a copy to
  `$TACK_HOME/src/std/include/tack/tack.h` for editors, which no build
  reads.

`print` and `exit` have C bodies in the runtime's C files, built into the
compiler, under the C names `tack_builtin_print` and
`tack_builtin_exit`. A C file of the program that defines a builtin's C
name the program calls is the error of two C files defining one symbol
(see [The missing-definition check](#the-missing-definition-check)).

## The ABI

Every Tack type crosses the boundary, in both directions, by value, with
the layout `tack.h` defines for it, except as this section and the next
two say.

| Tack                        | C                                                                                                                                      |
|-----------------------------|----------------------------------------------------------------------------------------------------------------------------------------|
| `int8` ... `int64`          | `int8_t` ... `int64_t`                                                                                                                 |
| `uint8` ... `uint64`        | `uint8_t` ... `uint64_t`                                                                                                               |
| `unsafe_size_t`             | `size_t`, from `stddef.h`                                                                                                              |
| `unsafe_uintptr_t`          | `uintptr_t`, from `stdint.h`                                                                                                           |
| `bool`                      | `bool`, from `stdbool.h`                                                                                                               |
| `float32`, `float64`        | `float`, `double`; NaN and infinities cross too                                                                                        |
| `string`                    | `tack_string`: `const unsigned char *ptr; int64_t len;`, by value                                                                      |
| `error`                     | `tack_error`: `int32_t code; tack_string msg;`, by value; code 0 is no error                                                           |
| a struct                    | a C struct with a member per field, under the field's Tack name, in order; `char unused;` if it has none                               |
| `[N]T`                      | a struct holding one C array, `T e[N]`, of at least one element: `T e[1]` for `[0]T`                                                   |
| no result                   | `void`                                                                                                                                 |
| one result                  | that result's C type                                                                                                                   |
| several results             | one struct, `tack_results_` and the C name, with members `r0`, `r1`, and on; an error, always the last result, in the last member      |
| a `mut p T` parameter       | `T *`, a pointer to the caller's variable, valid only during the call; never null                                                      |
| a `v []T` parameter         | `tack_view_T`: `const T *ptr; int64_t len;`, by value; `ptr` valid only during the call, for `len` elements                            |
| an `args ...T` parameter    | `tack_view_T`, as for `[]T`: the values the call passed, in order, or the view it spread; `ptr` null and `len` 0 for no values         |
| an `a any` parameter        | `tack_any`: `uint32_t type; const void *ptr;`, by value                                                                                |
| an `args ...any` parameter  | `tack_view_any`: `const tack_any *ptr; int64_t len;`                                                                                   |
| `Allocator`                 | `tack_allocator`: `uint32_t index; uint32_t gen;`, by value, an opaque handle                                                          |
| a `mut l list[T]` parameter | `tack_list_T *`: `T *ptr; int64_t len; int64_t cap; tack_allocator alloc;`, a pointer to the caller's list, valid only during the call |

A view crosses only into C: an extern func cannot return one, `extern
func f cannot return a view: nothing tells the checker what C's view
points into`. An `any` is only a parameter, or the element of a
variadic or view parameter. These do not cross at all:

- a function value, or anything holding one, `parameter f of extern
  func take cannot be func(): a function value does not cross to C`;
- a map, `parameter m of extern func f cannot be map[int64]int64: a map
  does not cross to C`;
- a union, or anything holding one through a struct, an array, a view
  or a list, `parameter e of extern func f cannot be Expr: a union
  cannot cross to C yet`, `extern func f cannot return Expr: ...`;
- a list by value, as a parameter or a result, or a struct or array
  holding one (see [Lists across the boundary](#lists-across-the-boundary)).

```tack error
package main

extern func a(l list[int64]) int64
extern func b(x int64) []int64
extern func c(f func()) int64
extern func d(m map[int64]int64) int64

func main() {
}
```

```diagnostics
main.tck:3:17: error: parameter l of extern func a cannot be list[int64]: a list crosses to C only as a mut parameter, or as a view
main.tck:4:24: error: extern func b cannot return a view: nothing tells the checker what C's view points into
main.tck:5:17: error: parameter f of extern func c cannot be func(): a function value does not cross to C
main.tck:6:17: error: parameter m of extern func d cannot be map[int64]int64: a map does not cross to C
```

### tack.h

`tack.h` includes `stdbool.h` and `stdint.h`, `stddef.h` when a
signature uses `size_t`, and `tack/tack.h` when one uses a string, an
error, `any`, a list or an `Allocator`. It defines every type an extern
func's signature uses and every type those hold, each before the types
that hold it, and no other type. Its names are:

- `tack_string`, `tack_error`, `tack_any` and `tack_allocator`, which
  `tack/tack.h` defines, for `string`, `error`, `any` and `Allocator`.
- For a struct of package `main`, its Tack name, `Point`; for one of any
  other package, its package's name, `_`, and its name, so `Token` of
  `calc/lexer` is `lexer_Token`.
- For an array, `tack_array_`, its length, `_`, and its element type's
  name, a basic type's being its Tack name: `[4]int64` is
  `tack_array_4_int64`, and `[2][3]Point` is
  `tack_array_2_tack_array_3_Point`.
- For a view, `tack_view_` and its element type's name, named as an
  array's is: `[]int64` is `tack_view_int64`, and `[]any` is
  `tack_view_any`.
- For a list, `tack_list_` and its element type's name: `list[Point]` is
  `tack_list_Point`.
- For the results of extern func `f`, `tack_results_f`, after its C
  name.
- Each prototype names each parameter in a comment after its type,
  `int64_t /* n */`, not as a C name.

So `extern func grow(mut p Point, k int64)` is `void grow(Point * /* p
*/, int64_t /* k */);` in `tack.h`, and `extern func sum(base int64,
nums ...int64) int64` is `int64_t sum(int64_t /* base */,
tack_view_int64 /* nums */);`.

A name `tack.h` cannot give is an error, whatever is built. An extern
func's C name is always checked; a type, and its fields, only once an
extern signature uses the type, since `tack.h` defines no other:

- two things given one name, at the second with a note at the first:
  `type Token of package calc/b/lexer and type Token of package
  calc/a/lexer are both named lexer_Token in tack.h`;
- an extern func's C name, or a type an extern signature uses, named
  like a name of `tack.h` or of `tack/tack.h`: its include guards `TACK_H` and `TACK_STD_TACK_H`;
  its types `tack_string`, `tack_error`, `tack_any` and
  `tack_allocator`; its string helpers `TACK_STR`, `TACK_STR_N` and
  `TACK_C_STR_LEN`; its error helpers `TACK_ERROR` and `TACK_NO_ERROR`;
  its any type ids `TACK_TYPE_INVALID` through `TACK_TYPE_VIEW_FLOAT64`;
  its any helpers `TACK_ANY_IS`, the fourteen `tack_any_T` unpack
  helpers, `tack_any_signed` and `tack_any_unsigned`; its allocator
  helper `TACK_C_ALLOCATOR`; its list helpers `TACK_LIST_LEN`,
  `TACK_LIST_AT`, `TACK_LIST_APPEND`, `TACK_LIST_APPEND_N`,
  `TACK_LIST_RESERVE`, `TACK_LIST_POP` and `TACK_LIST_REMOVE`; its list
  functions `tack_list_grow_` and `tack_list_free_`; its allocator
  functions `tack_c_allocator_live_`, `tack_c_allocator_fail_after_`
  and `tack_allocators_release_all_`; and its program globals
  `tack_argc_` and `tack_argv_`. For example, `type tack_string of
  package main cannot be named tack_string in tack.h: that is tack.h's
  string type`. Each is reserved whether or not the program uses what it
  serves;
- an extern func's C name, or a type an extern signature uses, named
  like a macro that the C standard specifies for one of the headers
  `tack.h` may include,
  whether or not this `tack.h` includes it (the first diagnostic of the
  block below). The macros are `__bool_true_false_are_defined` of
  `stdbool.h`; `NULL`, `offsetof` and `unreachable` of `stddef.h`; and
  of `stdint.h`, for N of 8, 16, 32 and 64, `INTN_MIN`, `INTN_MAX`,
  `INTN_WIDTH`, `UINTN_MAX`, `UINTN_WIDTH`, the same with `INT_LEASTN`
  and `UINT_LEASTN`, and with `INT_FASTN` and `UINT_FASTN`, `INTN_C` and
  `UINTN_C`, then `INTPTR_MIN`, `INTPTR_MAX`, `INTPTR_WIDTH`,
  `UINTPTR_MAX`, `UINTPTR_WIDTH`, `INTMAX_MIN`, `INTMAX_MAX`,
  `INTMAX_WIDTH`, `UINTMAX_MAX`, `UINTMAX_WIDTH`, `INTMAX_C`,
  `UINTMAX_C`, `PTRDIFF_MIN`, `PTRDIFF_MAX`, `PTRDIFF_WIDTH`,
  `SIG_ATOMIC_MIN`, `SIG_ATOMIC_MAX`, `SIG_ATOMIC_WIDTH`, `SIZE_MAX`,
  `SIZE_WIDTH`, `RSIZE_MAX`, `WCHAR_MIN`, `WCHAR_MAX`, `WCHAR_WIDTH`,
  `WINT_MIN`, `WINT_MAX` and `WINT_WIDTH`;
- a type or field named with a C keyword, once an extern signature uses
  it: `type int cannot be named int in C: it is a C keyword`, `field
  while of type Shape cannot be named while in C: it is a C keyword`;
- a field named like an object-like macro of the two lists above, once
  an extern signature uses its type (the second diagnostic below),
  since the macro would replace the member's name. A field may be named
  like a function-like macro, or like a type or function of
  `tack/tack.h`.

```text
type NULL of package main cannot be named NULL in tack.h: that is a macro of <stddef.h>
field NULL of type Limits cannot be named NULL in tack.h: that is a macro of <stddef.h>
```

A view's or a list's name is reserved once an extern signature uses
that view or list type: `type tack_view_int64 of package main and view
type []int64 are both named tack_view_int64 in tack.h`.

## Lists across the boundary

A list crosses only as a `mut` parameter, `tack_list_T *`, a pointer
to the caller's list, or as a view of its elements, `tack_view_T`. By
value, as a parameter or a result, a list, or a struct or array holding
one, is `parameter l of extern func f cannot be list[int64]: a list
crosses to C only as a mut parameter, or as a view`, or `extern func f
cannot return list[int64]: ...`. A list or a view whose elements hold
lists does not cross at all, `parameter l of extern func f cannot be
list[list[uint8]]: a list or a view whose elements hold lists cannot
cross to C yet`, nor does a struct holding one, `parameter s of extern
func f cannot be S, which holds one: ...`.

`tack/tack.h` gives C these macros for a list:

- `TACK_LIST_LEN(l)`, the number of elements of the list `l`, a
  `tack_list_T`, not a pointer to one.
- `TACK_LIST_AT(l, i)`, element `i`, an lvalue. Nothing checks `i`.
- `TACK_LIST_APPEND(lp, v)` appends `v` to the list `lp` points to,
  growing its buffer through its own allocator when it is full, and is
  `true`; or `false` when the allocator cannot grow it, the list left as
  it was. It evaluates `lp` once and then `v` once, before the buffer
  can move, and converts `v` to the element type as an assignment
  would.
- `TACK_LIST_RESERVE(lp, n)` makes room for `n` more elements, an
  `int64_t`, so the capacity is at least the length plus `n`, and is
  `true`; or `false`, the list left as it was, when `n` is negative, the
  length plus `n` overflows an `int64_t`, or the allocator cannot grow
  it. It never changes the length or the elements, and `n` of 0 never
  allocates.
- `TACK_LIST_APPEND_N(lp, src, n)` appends the `n` elements `src` points
  to, all or none, and is `true`; or `false`, the list left as it was.
  `n` of 0 appends nothing and is `true`, and `src` may then be null.
  `src` must not point into the list's own buffer.
- `TACK_LIST_POP(lp)` takes the last element out and gives it, and
  `TACK_LIST_REMOVE(lp, i)` takes element `i` out, moving every element
  after it down one place. Neither checks anything: C checks
  `TACK_LIST_LEN(*lp) > 0`, or `0 <= i && i < TACK_LIST_LEN(*lp)`,
  first. Neither allocates or frees.

An `Allocator` is a `tack_allocator`, an opaque handle; `c_allocator` is
`TACK_C_ALLOCATOR`.

The rules C must keep for a list are under [The trust
boundary](#the-trust-boundary).

## any across the boundary

A value passed as `any` reaches C as a `tack_any`: `type`, the id of
the value's type, and `ptr`, a pointer to the value. The ids are fixed,
and `tack/tack.h` names each:

| id       | macro                                                 | Tack                        | `ptr` points at     |
|----------|-------------------------------------------------------|-----------------------------|---------------------|
| 0        | `TACK_TYPE_INVALID`                                   | none                        | nothing             |
| 1        | `TACK_TYPE_BOOL`                                      | `bool`                      | `bool`              |
| 2        | `TACK_TYPE_INT8`                                      | `int8`                      | `int8_t`            |
| 3        | `TACK_TYPE_INT16`                                     | `int16`                     | `int16_t`           |
| 4        | `TACK_TYPE_INT32`                                     | `int32`                     | `int32_t`           |
| 5        | `TACK_TYPE_INT64`                                     | `int64`                     | `int64_t`           |
| 6        | `TACK_TYPE_UINT8`                                     | `uint8`                     | `uint8_t`           |
| 7        | `TACK_TYPE_UINT16`                                    | `uint16`                    | `uint16_t`          |
| 8        | `TACK_TYPE_UINT32`                                    | `uint32`                    | `uint32_t`          |
| 9        | `TACK_TYPE_UINT64`                                    | `uint64`                    | `uint64_t`          |
| 10       | `TACK_TYPE_UNSAFE_SIZE_T`                             | `unsafe_size_t`             | `size_t`            |
| 11       | `TACK_TYPE_UNSAFE_UINTPTR_T`                          | `unsafe_uintptr_t`          | `uintptr_t`         |
| 12       | `TACK_TYPE_STRING`                                    | `string`                    | `tack_string`       |
| 13 to 24 | `TACK_TYPE_VIEW_BOOL` through `TACK_TYPE_VIEW_STRING` | `[]bool` through `[]string` | `tack_view_T`       |
| 25       | `TACK_TYPE_FLOAT32`                                   | `float32`                   | `float`             |
| 26       | `TACK_TYPE_FLOAT64`                                   | `float64`                   | `double`            |
| 27       | `TACK_TYPE_VIEW_FLOAT32`                              | `[]float32`                 | `tack_view_float32` |
| 28       | `TACK_TYPE_VIEW_FLOAT64`                              | `[]float64`                 | `tack_view_float64` |

The view of each of the first twelve basic types has its element's id
plus 12. 29 to 255 are kept for types to come. An `any` C gets may have
id 0, with a null `ptr`: it is the `any` an index out of range gives,
`v, ok := args[i]`, which Tack code can pass on.

C reads an `any` with these, from `tack/tack.h`:

- `TACK_ANY_IS(a, T)` reports whether `a` holds a `T`, where `T` is the
  end of an id's macro: `TACK_ANY_IS(a, STRING)`.
- `tack_any_T(a, &v)`, one for each basic type, `tack_any_int64`,
  `tack_any_string` and so on, stores the value in `v` and returns
  `true` when `a` holds that type, and otherwise returns `false`,
  leaving `v` alone.
- `tack_any_signed(a, &v)` stores any signed integer, widened to
  `int64_t`, and `tack_any_unsigned(a, &v)` any unsigned one, widened to
  `uint64_t`, each returning whether `a` held one.

No helper unpacks a view: C checks the id, then reads `ptr` as a
pointer to the view's struct:

```text
if (TACK_ANY_IS(a, VIEW_INT64)) {
    tack_view_int64 v = *(const tack_view_int64 *)a.ptr;
}
```

## Program arguments

A Tack program's `main` takes no parameters (see [Packages, modules and
programs](12-packages.md)). The generated C's `main` is `int main(int
argc, char **argv)`, and before it calls the Tack `main` it stores
`argc` and `argv` in two globals, `int tack_argc_` and `char
**tack_argv_`, which `tack/tack.h` declares and every program has. C
may read them; it never writes them, and never writes through
`tack_argv_`. No
extern func may take either name, as no program's extern func may take
a name starting `tack_`.

## Helpers for C

`tack/tack.h` defines these macros for building strings and errors in
C:

- `TACK_STR(lit)` is a string literal as a `tack_string`. Its length is
  the literal's size less the zero C ends it with. Anything but a
  literal is a compile error.
- `TACK_STR_N(p, n)` is the `n` bytes at `p`, which is a `const unsigned
  char *` or an `unsigned char *`, or an array of either; any other
  pointer is a compile error.
- `TACK_C_STR_LEN(a)` is the length of the string the byte array `a`
  holds: its size less one. A pointer is a compile error.
- `TACK_ERROR(code, lit)` is an error with the code `code`, converted to
  `int32_t`, and the string literal `lit` as its message. Anything but a
  literal message is a compile error.
- `TACK_NO_ERROR` is no error, Tack's `noerror`: code 0 and an empty
  message.

These make a string, not its bytes' lifetime: the bytes must stay valid
as [The trust boundary](#the-trust-boundary) says.

```text
tack_results_half half(int64_t n) {
    if (n % 2 != 0) {
        return (tack_results_half){0, TACK_ERROR(9, "odd")};
    }
    return (tack_results_half){n / 2, TACK_NO_ERROR};
}
```

is a C body for `extern func half(n int64) (int64, error)`, whose Tack
callers check its error as any function's.

## The trust boundary

`tack.h` makes clang check that each C definition has the types its
extern func declares. Nothing checks more than that. Each rule below is
a promise the C side makes; a broken one is undefined behavior in the
whole program, Tack code included, which no Tack proof catches.

- **The signature.** The C function does what its Tack declaration says,
  for every argument the Tack side can pass, and returns the declared
  results. Tack knows nothing about a result: a proof about an argument
  says nothing about it.
- **Integer semantics.** Tack's `+`, `-` and `*` are proven not to
  overflow, or written in the checked form, and `+%`, `-%` and `*%`
  wrap (see [Integer overflow](03-arithmetic.md#integer-overflow)); C's
  signed overflow, and its division by zero or of the smallest value by
  -1, are undefined. A C body that does arithmetic wraps it itself, or
  states the range it accepts, which its Tack callers then keep.
- **Bounds.** C reads a string's bytes only below `len`, and an array's
  elements only below its length. A string's bytes do not end in a
  zero, and an empty string may have a null `ptr`, so C does not touch
  `ptr` when `len` is 0.
- **No writes, no keeping.** C does not write through a `tack_string`'s
  `ptr`, and keeps no pointer into Tack data past the call.
- **Views.** C reads a view's elements only below `len`, only during the
  call, and does not write through its `ptr`. An empty view's `ptr` may
  be null or one past the last element, and is not read. The elements
  do not change during the call unless C changes them.
- **`any`.** C checks a `tack_any`'s `type` before reading through
  `ptr`, reads it as that type only, during the call only, and never
  writes through it.
- **`mut` parameters.** C reads and writes the caller's variable only
  during the call, keeps and returns no pointer to it, and writes there
  only values of its type: a `bool` that is `true` or `false`, a string
  whose bytes stay valid as a returned string's must. Two `mut`
  parameters of one call never overlap, and C reaches the variable no
  other way during the call.
- **Lists.** C reads, writes, grows and shrinks a list only during the
  call, and only through the list macros or as their definitions do,
  never appending from the list's own buffer with
  `TACK_LIST_APPEND_N`. It keeps no pointer to the list or into its
  buffer past the call; it never frees the buffer, sets `len` past the
  elements written or above `cap`, or changes `ptr`, `cap` or `alloc`.
  An element it writes is a value of the element type. Tack never
  passes a list C gets as `mut` alongside a view or a borrow of it in
  the same call.
- **Strings from C.** A string C returns is taken as it is: its bytes
  stay valid, and unchanged, for as long as Tack might use the string,
  which today means for the rest of the program. Who owns those bytes,
  and who frees them, is not yet specified. An error's message is such
  a string.
- **Errors from C.** Only the code says whether there is an error: 0 is
  none, and Tack reads the other results only then. An extern func that
  fails with code 0 returns its results as if it succeeded.
- **Control.** C may end the process, with C's `exit` or `abort`, or
  never return; either way no Tack defer runs and no list still live is
  freed. Only Tack's `exit` has the allocators free what they hold. C
  cannot call back into Tack.
- **Symbols.** C may define any symbol, one the C library uses
  included; only the clashes in [The missing-definition
  check](#the-missing-definition-check) are caught.
- **Allocator functions.** C never calls `tack_allocators_release_all_`,
  which would leave every list still holding a buffer pointing at freed
  memory, nor `tack_list_grow_` or `tack_list_free_` directly.

## The missing-definition check

When building, after compiling a program's C files, the compiler reads
which functions and variables each object file defines and exports, and
reports:

- an extern func that no C file of its own package defines, as an error
  at its declaration: `extern func write has no C definition in package
  os`. A definition in another package's C files does not count;
- a C file that defines `main`: `tack: helpers.c defines main; a Tack
  program's main is its Tack main, so no C file may define one`;
- two C files, of any packages or the runtime's, that define one
  symbol: `tack: two C files define add: a.c and b.c`.

The check runs only when building: `tack --check` and `tack --emit`
compile no C file and report none of these.

A program with one extern func whose C file defines it, beside its
`main.tck`:

```tack fragment
package main

// count returns how many bytes of s are c.
extern func count(s string, c uint8) int64

func main() {
    if count("banana", 'a') != 3 {
        exit(1)
    }
}
```

and `count.c`:

```text
#include "tack.h"

int64_t count(tack_string s, uint8_t c) {
    int64_t n = 0;
    for (int64_t i = 0; i < s.len; i++) {
        if (s.ptr[i] == c) {
            n++;
        }
    }
    return n;
}
```
