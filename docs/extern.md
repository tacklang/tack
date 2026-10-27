# Extern C, Tack's unsafe

An `extern func` is a Tack function whose body is written in C. It is the
one place a Tack program can do what Tack itself cannot, and the one place
the compiler stops checking: the declaration is a promise about what the C
body does, and nothing holds the C side to it beyond its types. This page is
the whole of extern C: how to declare one, how its C is built, how each type
crosses, what the C side must promise, and what is checked.

The rules in [safety.md](safety.md), the proofs that keep Tack code from
dividing by zero, indexing out of range or converting to a type a value does
not fit, cover Tack code only. They do not reach into C, and nothing they
prove survives a trip through it.

## Declaring one

```tack
extern func add(a int64, b int64) int64
extern("c_twice") func twice(x int64) int64
```

`extern func name(params) results` declares the function and gives it no
body; its body is the C function of the same name, or of the name
`extern("...")` gives. It is called like any other function, and, like any
call, not in a package-level initializer, which is a constant.

- `extern` is a keyword, reserved everywhere, and is written only before
  `func` or a C name in parentheses: `extern var` is `expected 'func' found
  'var'`, and `extern(add)` is `expected 'C name in quotes' found 'add'`.
- An extern func has no body in Tack: `extern func f cannot have a body; its
  body is written in C`.
- An extern func is never generic: `extern func f cannot have type
  parameters; its body is written in C`.
- A function without a body that is not extern is an error: `function f has
  no body; only an extern func is declared without one`.
- `main` cannot be extern: `main function must not be extern`.

## Names

An extern func's C name is its Tack name unchanged, or the string
`extern("c_name")` gives, so `extern("c_twice") func twice(x int64) int64`
is defined in C as `c_twice`. A C name C cannot define is an error at the
declaration, at the string if one is given:

- one that is not a C identifier: `extern func f cannot be named "two words"
  in C: it is not a C identifier`;
- a C keyword, C23's set plus `asm` and `typeof`: `extern func while cannot
  be named while in C: it is a C keyword`. A Tack name that is a C keyword
  needs `extern("name")`;
- `main`, which the generated C defines: `extern func f cannot be named main
  in C: the program's C main is the one that runs Tack's`;
- outside the standard library, one starting `tack_`, the prefix of the
  generated C's own names: `extern func f cannot be named tack_f in C: names
  starting tack_ are reserved for the standard library`. The standard
  library names its own `tack_<package>_<name>`, as the builtins below are.

Two extern funcs of one program, in any packages, with one C name are an
error at the second, with a note at the first: `extern funcs add and
util.plus both have the C name add`. A name a C header declares, such as
`printf` or `int64_t`, is not checked; it clashes only with what a C file
declares. A macro of the headers `tack.h` includes is the exception, since
`tack.h` itself would break: see the names `tack.h` cannot give, below.

The generated C declares each extern func under its own mangled name and
binds that to the C name with an assembler label, so a C name never clashes
with a name the generated C uses.

## Visibility

An extern func is never `pub` or `lib`: `an extern func cannot be pub;
export a Tack function that calls it`, and likewise for `lib`. A package that binds C wraps each
extern func in a Tack function and exports that, so the Tack side of the
promise, such as keeping a divisor nonzero, is kept in one place, and the
package's users never call C directly. The worked example below does this
with `divmod`.

## The build

- Every file whose name ends in `.c` directly in a package's directory is
  compiled and linked with the program. A single-file target, one built
  without a `tack.mod`, has the `.c` files of its own directory. Hidden
  files and directories are skipped, as for `.tck` files. No rule ties a
  `.c` file's name to a `.tck` file's: `os.tck` beside `os.c` is a
  convention only, and a `.c` file is compiled and linked even if no extern
  func of its package uses it.
- A C file whose name starts with `-` is an error: `tack: file -x.c in dir
  starts with -, which clang could read as an option; rename it`. Every
  path the driver hands clang is absolute, so none is read as an option.
- Each C file is compiled on its own with `clang -w -O3 -c`, the generated
  C's flags: at the build's optimization level, `-O3` unless an `-O` flag
  says otherwise, and with warnings off, so clang's warnings about the C
  are not shown; its errors are, and then `tack: cannot compile helpers.c: clang failed:
  exit status 1`. There is no way yet to pass a C file other flags, or to
  link a library.
- The compiler generates `tack.h` for the program: the types the extern
  funcs' signatures use and a prototype for each extern func, under its C
  name, with its Tack declaration in a comment beside it. A build writes it
  into its temporary build directory, never into the source tree, and
  compiles every C file with that directory on the include path, then
  the directory it writes the runtime header to, and `tack.h` included before the
  file's first line. So clang checks every
  definition against its extern func, whether the file includes `tack.h` or
  not; a definition that does not match is clang's `conflicting types`.
- A file may still `#include "tack.h"`, and needs to for an editor to know
  the types. `tack --emit-header <target>` writes `tack.h` into the
  target's directory and into every package directory of its module with
  C files or extern funcs, never a standard library package's, and stops; add it to `.gitignore`. The programs under
  `examples/` keep theirs, for reading beside the C files, and a test
  checks each matches what the compiler generates. A stale copy beside a C
  file is skipped by the include guard of the one the build includes first.
  One `tack.h` serves the whole program, its extern funcs package by
  package.
- `tack.h` holds only what is the program's own. What every program
  shares, `tack_string` and the string helpers below, `tack_error` and
  the error helpers, and `tack_any`, its
  type ids and its unpack helpers, is defined once, in
  the runtime header, [`tack/tack.h`](../builtin/runtime/tack/tack.h),
  built into the compiler, which `tack.h` includes as `<tack/tack.h>` when
  a signature uses a string, an error or `any`, and which the generated C
  and std's own C files include too. An editor that reads such a `tack.h`
  needs a copy of it on its include path: `make install-std` writes one to
  std's include directory, so `-I $TACK_HOME/src/std/include`, or
  `~/.tack/src/std/include` without `TACK_HOME`, in whatever compile flags
  the editor reads. A build never reads that copy.

## The ABI

Every Tack type crosses the boundary, in both directions, by value, with
the layout the generated C gives it, which is what `tack.h` defines. A
`mut` parameter is the one exception: it crosses as a pointer to the
caller's variable. A view, `[]T`, crosses only into C, as a struct of a
pointer to its first element and its length: a Tack function may return
one, of data its caller passed it, but an extern func cannot, `extern
func f cannot return a view: nothing tells the checker what C's view
points into`. A variadic
parameter, `args ...T`, is a view inside a Tack function, and crosses as
one: C gets a `tack_view_<T>` of the values the call passed, which the
caller puts in an array on its stack for the call, or of the view a
spread, `f(v...)`, passes on. An `any` is a parameter only too, or the
element of a variadic or view parameter: C gets a `tack_any`, a type id
and a pointer to the caller's value, a view of a basic type included,
and `args ...any` a `tack_view_any` of them. A list, `list[T]`, crosses only as a `mut` parameter, a pointer
to the caller's list, which C may read and grow during the call (see
[Lists](#lists)), or as a view of its elements; by value, as a
parameter or a result, it, or a struct or an array holding one, is an
error, since a list owns its buffer and passing one to C, or getting one
back, would move it into or out of code no rule follows. A list or a
view whose elements hold lists does not cross at all yet, as a `mut`
parameter, a view, or inside a struct: `parameter l of extern func f
cannot be list[list[uint8]]: a list or a view whose elements hold lists
cannot cross to C yet`, or `parameter s of extern func f cannot be S,
which holds one: ...`, since how C would free or take its elements is
not settled. A function value, or anything holding one, does not cross
at all, `parameter f of extern func take cannot be func(): a function
value does not cross to C`, since nothing would check what C's pointer
points to, and an extern func is no value, `cannot use now as a value:
it is an extern func, and a function value does not cross to C`.

| Tack                        | C                                                                                                                                                                                    |
|-----------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `int8` ... `int64`          | `int8_t` ... `int64_t`                                                                                                                                                               |
| `uint8` ... `uint64`        | `uint8_t` ... `uint64_t`                                                                                                                                                             |
| `unsafe_size_t`             | `size_t`, from `<stddef.h>`, as wide as the target's                                                                                                                                 |
| `unsafe_uintptr_t`          | `uintptr_t`, from `<stdint.h>`, as wide as the target's                                                                                                                              |
| `bool`                      | `bool`, from `<stdbool.h>`                                                                                                                                                           |
| `float32`, `float64`        | `float`, `double`: IEEE 754's binary32 and binary64; NaN and infinities cross too                                                                                                    |
| `string`                    | `tack_string`: `const unsigned char *ptr; int64_t len;`, by value                                                                                                                    |
| `error`                     | `tack_error`: `int32_t code; tack_string msg;`, by value; code 0 is no error                                                                                                         |
| a struct                    | a C struct with a member per field, under the field's Tack name, in order; `char unused;` if it has none                                                                             |
| `[N]T`                      | a struct holding one C array, `T e[N]`, of at least one element                                                                                                                      |
| no result                   | `void`                                                                                                                                                                               |
| one result                  | that result's C type                                                                                                                                                                 |
| several results             | one returned struct, `tack_results_<C name>`, with members `r0`, `r1`, and on; an error, always the last result, in the last member                                                  |
| a `mut p T` parameter       | `T *`, a pointer to the caller's variable, valid only during the call; never null                                                                                                    |
| a `v []T` parameter         | `tack_view_<T>`: `const T *ptr; int64_t len;`, by value; `ptr` valid only during the call, for `len` elements                                                                        |
| an `args ...T` parameter    | `tack_view_<T>`, as for `[]T`: the values the call passed, in order, or the view it spread; `ptr` null and `len` 0 for no values                                                     |
| an `a any` parameter        | `tack_any`: `uint32_t type; const void *ptr;`, by value; `type` one of the ids below, `ptr` pointing at a value of that type, a view's struct for a view, valid only during the call |
| an `args ...any` parameter  | `tack_view_any`: `const tack_any *ptr; int64_t len;`, the view of anys a variadic is                                                                                                 |
| `Allocator`                 | `tack_allocator`: `uint32_t index; uint32_t gen;`, by value, an opaque handle                                                                                                        |
| a `mut l list[T]` parameter | `tack_list_<T> *`: `T *ptr; int64_t len; int64_t cap; tack_allocator alloc;`, a pointer to the caller's list, valid only during the call                                             |

`tack.h` includes `<stdbool.h>` and `<stdint.h>`, `<stddef.h>` when a
signature uses `size_t`, and the runtime's `<tack/tack.h>` when one uses a string,
an error, `any`, a list or an `Allocator`, and defines every type an extern func's signature uses and every type
those hold, each before the types that hold it. Its names:

- `tack_string` for `string`, `tack_error` for `error`, and `tack_any`
  for `any`, which the runtime's `<tack/tack.h>` defines. `[]any`, and so
  `...any`, is `tack_view_any`; `[]error` is `tack_view_error`, and
  `[2]error` `tack_array_2_error`, after error's Tack name, as for any
  basic type.
- A struct of package `main` keeps its Tack name: `Point`. One of any other
  package is its package's name, `_`, and its name: `Token` of
  `calc/lexer` is `lexer_Token`.
- An array is `tack_array_`, its length, `_`, and its element type's name,
  a basic type's being its Tack name: `[4]int64` is `tack_array_4_int64`,
  `[2]unsafe_size_t` is `tack_array_2_unsafe_size_t`,
  `[2]lexer.Token` is `tack_array_2_lexer_Token`, and `[2][3]Point` is
  `tack_array_2_tack_array_3_Point`.
- A view is `tack_view_` and its element type's name, named as an array's
  is: `[]int64` is `tack_view_int64`, `[]uint8` is `tack_view_uint8`,
  `[]string` is `tack_view_string`, `[]Point` is `tack_view_Point`, and
  `[][2]int64` is `tack_view_tack_array_2_int64`. Its members are `ptr`, a
  pointer to `const` elements, and `len`, an `int64_t`, the layout of
  `tack_string`, whose bytes a `[]uint8` views. Like every other name, it
  is defined only when an extern signature uses it, and is reserved then: a
  struct named `tack_view_int64` in a program whose extern func takes a
  `[]int64` is `type tack_view_int64 of package main and view type []int64
  are both named tack_view_int64 in tack.h`.
- A list is `tack_list_` and its element type's name, named as an
  array's is: `list[int64]` is `tack_list_int64` and `list[Point]`
  `tack_list_Point`, reserved as a view's name is: `list type list[int64]
  and type tack_list_int64 of package main are both named tack_list_int64
  in tack.h`. `Allocator` is `tack_allocator`, which the runtime's `<tack/tack.h>`
  defines.
- The results of extern func `f` are `tack_results_f`, after its C name.
- The prototypes name each parameter in a comment after its type,
  `int64_t /* n */`, not as a C name, which could be a C keyword or clash
  with a type's name.

A name `tack.h` cannot give is an error whatever is built:

- two things given one name, at the second with a note at the first: `type
  Token of package calc/b/lexer and type Token of package calc/a/lexer are
  both named lexer_Token in tack.h`;
- a type named like one of `tack.h`'s own, or of the std header it
  includes: `type tack_string of package main cannot be named tack_string
  in tack.h: that is tack.h's string type`, and likewise `TACK_H`, its
  include guard, `TACK_STD_TACK_H`, the include guard of std's `tack.h`,
  and `TACK_C_STR_LEN`, `TACK_STR` and `TACK_STR_N`, the string helpers,
  for a type or an extern func's C name: `extern func wrap of package main
  cannot be named TACK_STR_N in tack.h: that is a tack.h string helper`.
  So are `tack_any`, `tack.h's any type`, `TACK_TYPE_INVALID` through
  `TACK_TYPE_VIEW_FLOAT64`, each `a tack.h any type id`, and `TACK_ANY_IS`, the
  fourteen `tack_any_<type>` unpack helpers, `tack_any_signed` and
  `tack_any_unsigned`, each `a tack.h any helper`: `type tack_any of
  package main cannot be named tack_any in tack.h: that is tack.h's any
  type`. So are `tack_error`, `tack.h's error type`, and `TACK_ERROR`
  and `TACK_NO_ERROR`, each `a tack.h error helper`: `type tack_error of
  package main cannot be named tack_error in tack.h: that is tack.h's
  error type`. So are `tack_allocator`, `tack.h's allocator type`,
  `TACK_C_ALLOCATOR`, `a tack.h allocator helper`, `TACK_LIST_LEN`,
  `TACK_LIST_AT`, `TACK_LIST_APPEND`, `TACK_LIST_APPEND_N`,
  `TACK_LIST_RESERVE`, `TACK_LIST_POP` and `TACK_LIST_REMOVE`, each `a
  tack.h list helper`,
  `tack_list_grow_` and `tack_list_free_`, each `a tack.h list
  function`, and `tack_c_allocator_live_`,
  `tack_c_allocator_fail_after_` and `tack_allocators_release_all_`, each
  `a tack.h allocator function`:
  `type tack_allocator of package main cannot be named tack_allocator in
  tack.h: that is tack.h's allocator type`. So are `tack_argc_` and
  `tack_argv_`, each `a tack.h program global` (see [Program
  arguments](#program-arguments)): `type tack_argv_ of package main
  cannot be named tack_argv_ in tack.h: that is a tack.h program global`.
  Each is reserved whether or not the program uses strings, errors, `any`
  or lists;
- a type or an extern func's C name named like a macro the C standard
  specifies one of the headers `tack.h` includes defines, whether or not
  this `tack.h` includes it: `type NULL of package main cannot be named
  NULL in tack.h: that is a macro of <stddef.h>`. The macros are
  `__bool_true_false_are_defined` of `<stdbool.h>` (`bool`, `true` and
  `false` are C keywords); `NULL`, `offsetof` and `unreachable` of
  `<stddef.h>`; and of `<stdint.h>`, for N of 8, 16, 32 and 64,
  `INTN_MIN`, `INTN_MAX`, `INTN_WIDTH`, `UINTN_MAX`, `UINTN_WIDTH`, the
  same with `INT_LEASTN` and `UINT_LEASTN`, and with `INT_FASTN` and
  `UINT_FASTN`, `INTN_C` and `UINTN_C`, then `INTPTR_MIN`, `INTPTR_MAX`,
  `INTPTR_WIDTH`, `UINTPTR_MAX`, `UINTPTR_WIDTH`, `INTMAX_MIN`,
  `INTMAX_MAX`, `INTMAX_WIDTH`, `UINTMAX_MAX`, `UINTMAX_WIDTH`,
  `INTMAX_C`, `UINTMAX_C`, `PTRDIFF_MIN`, `PTRDIFF_MAX`, `PTRDIFF_WIDTH`,
  `SIG_ATOMIC_MIN`, `SIG_ATOMIC_MAX`, `SIG_ATOMIC_WIDTH`, `SIZE_MAX`,
  `SIZE_WIDTH`, `RSIZE_MAX`, `WCHAR_MIN`, `WCHAR_MAX`, `WCHAR_WIDTH`,
  `WINT_MIN`, `WINT_MAX` and `WINT_WIDTH`;
- a type or field named with a C keyword, once an extern signature uses
  it: `type int cannot be named int in C: it is a C keyword`, `field while
  of type Shape cannot be named while in C: it is a C keyword`;
- a field named like an object-like macro of the two lists above, once an
  extern signature uses its type: the macro would replace the member's
  name in C, leaving a member with no name, which changes the struct's
  layout, or one C cannot parse. `field TACK_H of type Shape cannot be
  named TACK_H in tack.h: that is tack.h's include guard`, `field NULL of
  type Limits cannot be named NULL in tack.h: that is a macro of
  <stddef.h>`, `field TACK_TYPE_STRING of type P cannot be named
  TACK_TYPE_STRING in tack.h: that is a tack.h any type id`, `field
  TACK_NO_ERROR of type P cannot be named TACK_NO_ERROR in tack.h: that
  is a tack.h error helper`. A field may
  be named like a function-like macro, such as `TACK_STR`, `TACK_ANY_IS`,
  `offsetof` or `INT64_C`, which C expands only before a `(`, or like
  `tack_string`, `tack_any` or an unpack helper, whose names a member's
  name does not clash with.

A type no extern func uses is not in `tack.h`, whatever its names. C never
declares a type for Tack: every type is declared in Tack, and C structs a
library owns, such as `struct stat`, are copied field by field into a Tack
struct by a C wrapper.

So `extern func grow(mut p Point, k int64)` is `void grow(Point * /* p */, int64_t /* k */);`
in `tack.h`, and the C side reads and writes the caller's point through
`p->x`. `extern func total(v []int64) int64` is
`int64_t total(tack_view_int64 /* v */);`, and the C side reads
`v.ptr[0]` through `v.ptr[v.len - 1]`; `examples/extern-views` passes
views of integers, a string's bytes, structs, arrays and strings.
`extern func sum(base int64, nums ...int64) int64` is
`int64_t sum(int64_t /* base */, tack_view_int64 /* nums */);`, with
the Tack declaration, `...` and all, in the comment beside it, so
`sum(1, 2, 3)` gives C a view of the two values 2 and 3, `sum(1)` one of
none, and `sum(1, a...)` the view of `a`; `examples/extern-variadics`
passes integers, strings, none and spreads.
`extern func kind(a any) uint32` is `uint32_t kind(tack_any /* a */);`,
and `extern func sum(args ...any) int64` is
`int64_t sum(tack_view_any /* args */);`, whose C reads `args.ptr[i]`
for `i` below `args.len`; `examples/extern-any` passes every basic type
at its edges and unpacks each, and passes views, whose ids it checks and
one of which it reads as its view struct.
`extern func half(n int64) (int64, error)` is
`tack_results_half half(int64_t /* n */);`, whose C returns
`(tack_results_half){n / 2, TACK_NO_ERROR}`, or a value and
`TACK_ERROR(9, "odd")`; its Tack callers bind and check the error as
they would any function's. `examples/extern-errors` returns errors alone
and last, passes them to C, as a struct's field through a `mut`
parameter and as `...error`.

## Lists

A list reaches C only as a `mut` parameter, `tack_list_<T> *`, a pointer
to the caller's list, or as a view of its elements, `tack_view_<T>`. Its
struct is the generated source's, in the same layout:

```c
typedef struct {
    int64_t *ptr;          // the buffer, null while the list is empty
    int64_t len;           // the number of elements
    int64_t cap;           // how many the buffer has room for
    tack_allocator alloc;  // what it grows and is freed through
} tack_list_int64;
```

the runtime's `<tack/tack.h>` gives C five macros for it:

- `TACK_LIST_LEN(l)`, the number of elements of the list `l`, a
  `tack_list_<T>`, not a pointer to one: `TACK_LIST_LEN(*lp)`.
- `TACK_LIST_AT(l, i)`, element `i`, an lvalue C reads and writes as any
  other, `TACK_LIST_AT(*lp, i) *= 2`. Nothing checks `i`: C keeps it at
  least 0 and below the length, as it must every index.
- `TACK_LIST_APPEND(lp, v)`, which appends `v` to the list `lp` points
  to, growing its buffer through its own allocator when it is full, and
  is `true`; or `false` when the allocator cannot grow it, the list left
  as it was. It evaluates `lp` once and then `v` once, before the buffer
  can move, so `v` may read an element of the list, and converts `v` to
  the element type as an assignment would, so clang checks it as one.
  `v` is the macro's variadic argument, so a compound literal, whose
  commas would otherwise split it, can be one: `TACK_LIST_APPEND(lp,
  (Point){1, 2})`. It is a GNU statement expression, which clang and gcc
  accept in C11 with no warning, as are the two below.
- `TACK_LIST_RESERVE(lp, n)`, which makes room in the list `lp` points to
  for `n` more elements, an `int64_t`, so that its capacity is at least
  its length plus `n`, growing its buffer as an append does, to at least
  double its capacity, and is `true`; or `false`, the list left as it
  was, when `n` is negative, the length plus `n` overflows an `int64_t`,
  or the allocator cannot grow it. It never changes the length or the
  elements, and `n` of 0 never allocates. It evaluates `lp` once and then
  `n` once.
- `TACK_LIST_APPEND_N(lp, src, n)`, which appends the `n` elements `src`
  points to, a pointer to the element type, all or none: it makes room
  for all of them first, as `TACK_LIST_RESERVE(lp, n)` does, then copies
  them, and is `true`; or `false`, the list's length, capacity and
  elements left as they were. `n` of 0 appends nothing, allocates nothing
  and is `true`, and `src` may then be null. It evaluates `lp`, `src` and
  `n` once each, in that order. `src` must not point into the list's own
  buffer, which making room may move: a view of the list's elements is
  exactly what Tack never passes alongside the list as `mut`. Several
  values are appended from an array, `const int64_t v[3] = {a, b, c};
  TACK_LIST_APPEND_N(lp, v, 3)`, and a view's elements from its pointer
  and length, `TACK_LIST_APPEND_N(lp, v.ptr, v.len)`.
- `TACK_LIST_POP(lp)`, the last element of the list `lp` points to,
  which it takes out, leaving the list one element shorter, and
  `TACK_LIST_REMOVE(lp, i)`, element `i`, an `int64_t`, which it takes
  out, moving every element after it down one place, in order. Neither
  checks anything: C checks `TACK_LIST_LEN(*lp) > 0`, or `0 <= i && i <
  TACK_LIST_LEN(*lp)`, first. Neither allocates or frees, so the buffer
  keeps its capacity. Each evaluates `lp` once, and `TACK_LIST_REMOVE`
  then `i` once, as Tack's `pop` and `remove` do, which lower to them.

Growing goes through `tack_list_grow_`, and a list's owner frees it
through `tack_list_free_`, both the runtime's, in its `list.c`, which
the driver links for any program with a list type. `tack_list_grow_`
fails rather than wraps when the buffer's size would not fit a `size_t`
or a `ptrdiff_t`, when the alignment is more than the allocator gives,
or when the handle is no allocator's; C never calls either directly.

An `Allocator` is `tack_allocator`, `{uint32_t index; uint32_t gen;}`,
an opaque handle: the index of one of the allocators the runtime's C keeps and
that entry's generation, so a handle to an allocator that has ended can
be told from one to its successor. `c_allocator` is `TACK_C_ALLOCATOR`,
index 0, generation 0, which lowers to `malloc`, `realloc` and `free`.
For tests, `tack_c_allocator_live_()` returns how many buffers
`c_allocator` has given out and not had back, and
`tack_c_allocator_fail_after_(n)` makes it grant the next `n` requests
and refuse every one after them, or none again for a negative `n`;
`examples/lists`, `examples/list-bulk` and `examples/extern-lists` check
every list is freed through the first, and appends and reserves that
fail through the second. Every allocator keeps track of each buffer it
has given out and not had back, `c_allocator` in a header in front of the
buffer, sized so the buffer keeps `max_align_t`'s alignment, and
`tack_allocators_release_all_()` has each free all of them: Tack's
`exit` calls it before it ends the process, and C must not, since every
list still holding one of those buffers is left pointing at freed
memory.

C must keep a list's rules, which Tack cannot check in C: it reads,
writes and appends to the list only during the call; it never keeps the
pointer, or a pointer into the buffer, past the call, since an append
may move the buffer and the caller frees it; it never frees the buffer,
shrinks the list, sets `len` past the elements it holds, or changes
`ptr`, `cap` or `alloc` itself; and it grows the list only through
`TACK_LIST_APPEND`, `TACK_LIST_APPEND_N` and `TACK_LIST_RESERVE`, so the
buffer always comes from the list's own allocator, which is what frees
it. It may read `cap`. `examples/extern-lists` appends integers and
structs from C, one at a time and several at once, reserves room, doubles
elements in place, reads a view of the list, and makes appends and a
reserve fail.

## Program arguments

The generated C's `main` is `int main(int argc, char **argv)`. Before it
calls the Tack `main`, it stores `argc` and `argv` in two globals the
generated source defines, `int tack_argc_` and `char **tack_argv_`,
which the runtime's `<tack/tack.h>` declares, so every program has them, whether
or not it reads them. `std/os`'s C reads its arguments from them, and
nothing writes them after `main` has: C reads them, never writes them,
and never writes through `tack_argv_`. No extern func may take either
name, as no program's extern may take any name starting `tack_`.

## any

A value passed as `any` reaches C as a `tack_any`, by value: `type`, the
id of the value's type, and `ptr`, a pointer to a copy of the value made
for the call, or to the caller's variable when nothing in the call can
change it. The ids are fixed for good, since C is written against them,
and the runtime's `<tack/tack.h>` names each:

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
| 13 to 24 | `TACK_TYPE_VIEW_BOOL` through `TACK_TYPE_VIEW_STRING` | `[]bool` through `[]string` | `tack_view_<T>`     |
| 25       | `TACK_TYPE_FLOAT32`                                   | `float32`                   | `float`             |
| 26       | `TACK_TYPE_FLOAT64`                                   | `float64`                   | `double`            |
| 27       | `TACK_TYPE_VIEW_FLOAT32`                              | `[]float32`                 | `tack_view_float32` |
| 28       | `TACK_TYPE_VIEW_FLOAT64`                              | `[]float64`                 | `tack_view_float64` |

0 is no type, so a zeroed `tack_any` is recognisably empty: it is the
`any` an index out of range gives, `v, ok := args[i]`, which Tack code
can pass on, so C may get one, with a null `ptr`, and every helper below
refuses it. A view of each of the fourteen basic types has an id of its
own, named `TACK_TYPE_VIEW_` and the end of its element's macro: for the
first twelve, its element's plus 12, 13 being `TACK_TYPE_VIEW_BOOL`,
`[]bool`, 18 `TACK_TYPE_VIEW_UINT8`, `[]uint8`, and 24
`TACK_TYPE_VIEW_STRING`, `[]string`; for the floats, which came later,
27, `TACK_TYPE_VIEW_FLOAT32`, and 28, `TACK_TYPE_VIEW_FLOAT64`. `ptr` points at the view itself, a struct of a `const`
pointer to its first element, `ptr`, and their number, `len`, an
`int64_t`, the layout of every view (see the table above), whose
elements are the caller's, as `const`, and for as long, as the view.
Std's `<tack/tack.h>` declares no view type, so no helper unpacks one:
C checks the id, then reads the view as the struct the program's
`tack.h` declares for it, `tack_view_int64` for `[]int64`, which it
does when an extern func's signature uses that view:

```c
if (TACK_ANY_IS(a, VIEW_INT64)) {
    tack_view_int64 v = *(const tack_view_int64 *)a.ptr;
    // v.ptr[i] for i below v.len, during the call only
}
```

A program that uses no such signature has no such type in its
`tack.h`, and C declares its own with the same members, a `const T *ptr`
and an `int64_t len`. `examples/extern-any` reads a `[]int64` this
way. 29 to 255 are kept for types to come. Arrays, structs, lists and
views of any other type cannot be passed as `any` yet. C reads the
basic types with these, from the runtime's `<tack/tack.h>`:

- `TACK_ANY_IS(a, T)` reports whether `a` holds a `T`, where `T` is the
  end of an id's macro: `TACK_ANY_IS(a, STRING)`. Any other `T` does not
  compile.
- `tack_any_<type>(a, &v)`, one for each type, named after it,
  `tack_any_int64`, `tack_any_unsafe_size_t`, `tack_any_string`, and so
  on, stores the value in `v` and returns `true` when `a` holds that type,
  and returns `false`, leaving `v` alone, otherwise. It is a function, not
  a macro, so clang checks `v`'s type.
- `tack_any_signed(a, &v)` stores any signed integer, widened to
  `int64_t`, and `tack_any_unsigned(a, &v)` any unsigned one,
  `unsafe_size_t` and `unsafe_uintptr_t` included, widened to `uint64_t`,
  each returning whether `a` held one.

None of them returns `ptr`; reading through it any other way is C's own
risk, under the trust boundary below. `std/fmt`'s `printf` is written in
C against these, in [`printf.c`](../src/std/fmt/printf.c).

## Helpers for C

Std's `<tack/tack.h>` also defines helpers that make the C side shorter
and harder to get wrong, beside `tack_string`, and beside `tack_any` the
ones under any above. A program's `tack.h` includes it only when the
program uses what they help with, because some extern signature uses a
string, an error or `any`, and then three macros for building a string come with
it:

- `TACK_STR(lit)` is a string literal as a `tack_string`:
  `return TACK_STR("hello");`. Its length is the literal's size less the
  zero C ends it with, so a zero byte inside the literal counts. Anything
  but a literal is a compile error.
- `TACK_STR_N(p, n)` is the `n` bytes at `p`, for bytes in a buffer or
  behind a pointer, whose length only the caller knows. `p` must be a
  `const unsigned char *` or an `unsigned char *`, or an array of either;
  a `char *`, `NULL` or any other pointer is a compile error, not a silent
  conversion, which under the build's `-w` it would otherwise be. Convert
  a `char *` explicitly: `TACK_STR_N((const unsigned char *)s, len)`.
- `TACK_C_STR_LEN(a)` is the length of the string the byte array `a`
  holds: its size less one, for the zero that ends it. A pointer is a
  compile error, since its size says nothing about its string, but an
  array is taken at its full size, so `unsigned char buf[64]` holding
  `"hi"` gives 63, not 2; use `TACK_STR_N` with the real length there.

`examples/extern-strings` uses each. Beside `tack_error`, two build an
error:

- `TACK_ERROR(code, lit)` is an error with the code `code`, converted to
  `int32_t`, and the string literal `lit` as its message, built by
  `TACK_STR`: `return TACK_ERROR(2, "not found");`. Anything but a
  literal message is a compile error; an error whose message is not a
  literal is a compound literal, `(tack_error){code, TACK_STR_N(p, n)}`,
  whose bytes must outlive the program's use of them, as below.
- `TACK_NO_ERROR` is no error, Tack's `noerror`: code 0 and an empty
  message.

`examples/extern-errors` uses both. They make a string, not its bytes'
lifetime: the bytes must still stay valid as the trust boundary below
says, which a literal's and a static array's do. A future helper follows
the same rule as these: it is defined once, in the runtime's `<tack/tack.h>` or a
std header beside it, reaches a program's C through `tack.h` only when the
program uses the type it serves, and its names are reserved in `tack.h`.

## The trust boundary

`tack.h` makes clang check that each C definition has the types its extern
func declares. Nothing checks more than that. Everything else below is a
promise the C side makes, and a broken one is undefined behavior in the
whole program, Tack code included, which no Tack proof can catch.

- **The signature.** The C function does what its Tack declaration and doc
  comment say, for every argument the Tack side can pass, and returns the
  declared results. The Tack side sees the result as a value nothing is
  known about: a proof about an argument says nothing about the result.
- **Integer semantics.** Tack's `+`, `-` and `*` wrap and its division is
  proved safe; C's signed overflow, and its division by zero or of the
  smallest value by -1, are undefined. A C body that does arithmetic either
  wraps it itself, through unsigned types as `point_sum` below does, or
  states the range it accepts, which its Tack callers then keep.
- **Bounds.** C reads a string's bytes only below `len` and an array's
  elements only below its length. A string's bytes do not end in a zero,
  and a string that is a zeroed struct field or array element has a null
  `ptr` with a `len` of 0, so C must not touch `ptr` when `len` is 0.
- **No writes, no keeping.** C does not write through a `tack_string`'s
  `ptr`, which is `const`, and keeps no pointer into Tack data beyond the
  call: Tack promises nothing about how long the bytes it passes live.
- **Views.** A view's `ptr` points at `len` elements C may read during the
  call, and only below `len`: a view of an empty array has a `len` of 0
  and a `ptr` that must not be read, and so does an empty slice, `a[2:2]`,
  whose `ptr` may be one past the last element, or null for the bytes of
  the zero string, and so does a variadic given no values, whose `ptr` is
  null. A slice, `a[1:3]`, passes as any view does, its `ptr`
  at its first element. C does not write through `ptr`, which
  is `const`, since Tack proves facts about a view's elements, its length
  included, for the whole call; and it does not keep `ptr` after
  returning, since the array may be a local that is gone, a copy the
  caller made for the call, or the array of a variadic's values, which
  lives only for the call. The elements do not change during the call
  unless C changes them, so C may read them more than once.
- **`any`.** C checks a `tack_any`'s `type` before it reads through
  `ptr`, and reads it as that type only, with `TACK_ANY_IS` or an unpack
  helper. `ptr` is valid only during the call: C must not keep it, return
  it, or use it after returning, as for a `mut` parameter's pointer, since
  it may point at a copy the caller made for the call. C never writes
  through it: it is `const`, the value may be a constant in read-only
  memory, and writing it would change a value Tack passed as a copy. A
  `tack_view_any` is a view, under the rule above, and so is a view an
  `any` holds, once read through `ptr`: its elements are the caller's,
  valid and unchanged for the call only.
- **`mut` parameters.** C may read and write the caller's variable through
  a `mut` parameter's pointer, but only during the call: it must not store
  the pointer, return it, or use it after returning, since in Tack a
  reference exists only for the call and the variable it points at may
  be gone, or be changed by Tack code, the moment the call ends. A value C
  writes there must be one of the type: a `bool` that is `true` or `false`,
  and a string whose bytes stay valid as a returned string's must. Tack
  passes two `mut` parameters of one call to places that do not overlap,
  so C may treat them as distinct, but C must not reach the same variable
  any other way during the call.
- **Lists.** C reads, writes and appends to a list it gets as a `mut`
  parameter only during the call, and through `TACK_LIST_LEN`,
  `TACK_LIST_AT`, `TACK_LIST_APPEND`, `TACK_LIST_APPEND_N` and
  `TACK_LIST_RESERVE` or as their definitions do, never appending from
  its own buffer with `TACK_LIST_APPEND_N`: it
  keeps no pointer to the list or into its buffer past the call, since
  an append may move the buffer and the caller frees it; it never frees
  the buffer, sets `len` past the elements written or above `cap`, or
  changes `ptr`, `cap` or `alloc` itself, so the list's own allocator is
  always the one that frees what it gave. An element it writes must be a
  value of the element type. Tack never passes a list C gets as `mut`
  alongside a view or a borrow of it in the same call, so growing it
  frees nothing another argument reads.
- **Strings from C.** A string C returns is taken as it is: its bytes must
  stay valid, and unchanged, for as long as Tack might use the string,
  which today means for the rest of the program. Nothing says who owns them
  or frees them; the memory model has not decided it yet. An error's
  message is such a string, whatever the code.
- **Errors from C.** Only the code says whether there is an error: 0 is
  none, and Tack reads the other results only then, so C returns the
  results that go with a code of 0 as it would any result. Tack does not
  check that C sets a code other than 0 when it fails: an extern func
  that fails with code 0 returns its results as if it succeeded.
- **Control.** C can end the process, with `exit` or `abort`, or never
  return, and either way no Tack defer runs, and no list still live is
  freed: only Tack's `exit` has the allocators free what they hold. There is no way for C to call
  back into Tack: every Tack function is static in the generated C.
- **Symbols.** C may define any symbol, including one the C library uses;
  only the clashes below are caught.

## The missing-definition check

After compiling a program's C files, the driver reads which functions and
variables each object file defines and exports, from its symbol table, and
reports:

- an extern func that no C file of its own package defines, as an error at
  its declaration: `extern func write has no C definition in package os`.
  A definition in another package's C files does not count. A `static`
  one would not count either, since it is not exported, but it never gets
  that far: `tack.h` has already declared the name without `static`, so
  clang rejects the file, as `cannot compile`;
- a C file that defines `main`: `tack: helpers.c defines main; a Tack
  program's main is its Tack main, so no C file may define one`;
- two C files, of any packages, that define one symbol, function or
  variable: `tack: two C files define add: a.c and b.c`.

The check runs only when building. `--emit` does not compile C files, so it
reports none of these.

Platform notes: the symbol tables are read in Go, with `debug/macho` and
`debug/elf`, so the build needs no tool but clang. Mach-O, what clang
writes on macOS, is the format the tests run against. ELF, Linux's, is read
by the same code but has not been tested: no test has run on Linux yet. Any
other object format, such as Windows' COFF, fails the build with `not a
Mach-O or ELF object file`.

## The builtins

The builtins are built into the compiler: `print` and `exit`, available in
every file without an import, with their C names, `tack_builtin_print`
and `tack_builtin_exit`, and the runtime's C files that define their
bodies, [`print.c`](../builtin/runtime/print.c) and
[`exit.c`](../builtin/runtime/exit.c), ordinary non-static functions under
those names, beside [`list.c`](../builtin/runtime/list.c) and the runtime
header, [`tack/tack.h`](../builtin/runtime/tack/tack.h). Every builtin is
documented in [`builtin/builtin.tck`](../builtin/builtin.tck), embedded in
the compiler as documentation only; `make install-std` writes read-only
copies of it and of the header into `$TACK_HOME/src/std`, which no build
reads, so editing them changes nothing.

The runtime's C files include `<tack/tack.h>`, the one definition of
`tack_string`, which the generated C includes too, and nothing
program-specific. The generated C declares each builtin the program calls
with a prototype, `void tack_builtin_print(tack_string);`, and includes no
hosted header for it, so it includes only `<stdbool.h>` and `<stdint.h>`,
`<stddef.h>` for `size_t`, which every freestanding C implementation has,
and `<tack/tack.h>`, which includes only those three. Each build writes
the runtime to its own temporary directory and compiles the generated C,
and every other C file, with that directory's copy of the header on the
include path; `tack --emit` output compiles on its own with `-I
$TACK_HOME/src/std/include`, where the installed copy is.

A build compiles every C file of the runtime, when the program calls any
builtin, has a list type, or has a C file whose object leaves a `tack_`
symbol undefined, as it compiles a package's: with clang, `-w`, at the
build's `-O` level, with the runtime header's directory on the include
path but without the program's `tack.h`, or takes the object from the
build cache. It reads each object's symbols, as it reads a package's, and
links only the objects that define a builtin the program calls, the list
functions, `tack_list_grow_` and `tack_list_free_`, for a program with
lists, and a `tack_` symbol one of the program's C files leaves undefined,
as a C file using the list macros does, and, in turn, those that define a
symbol a linked one leaves undefined, the way a linker takes members from
an archive (`exit.c`'s object leaves `tack_allocators_release_all_`
undefined, so a program calling `exit` links `list.c` too); there is no
archive, and no tool but clang. A program that calls neither builtin and
has no list links no runtime object, and none of the C library they call.
A C file of the program may not define a builtin's C name the program
calls: with the runtime's linked, that is `tack: two C files define
tack_builtin_print: helpers.c and <runtime>/print.c`.

## A worked example

[`examples/extern-guide`](../examples/extern-guide) is a single-file
program with one C file beside it. Its tests build it and run it, and check
that this page quotes it, and the `tack.h` the compiler writes for it,
exactly.

`main.tck`:

```tack
package main

// The worked example of docs/extern.md. Each extern func is defined in
// guide.c, beside this file.

type Point struct {
    x int64
    y int64
}

// count returns how many bytes of s are c.
extern func count(s string, c uint8) int64

// sum returns p.x + p.y, wrapping as Tack's + does. It is defined in C as
// point_sum.
extern("point_sum") func sum(p Point) int64

// c_divmod returns a / b and a % b. C's division is undefined when b is 0,
// or when a is the smallest int64 and b is -1, so its callers must rule
// both out: nothing checks that they do.
extern func c_divmod(a int64, b int64) (int64, int64)

// divmod is the Tack side of the promise: it calls C only with operands C
// can divide, and reports whether it did.
func divmod(a int64, b int64) (int64, int64, bool) {
    if b == 0 || b == -1 {
        return 0, 0, false
    }
    q, r := c_divmod(a, b)
    return q, r, true
}

func main() {
    if count("banana", 'a') != 3 || count("", 'a') != 0 {
        exit(1)
    }
    if sum(Point{x: -3, y: 10}) != 7 {
        exit(2)
    }
    q, r, ok := divmod(17, 5)
    if !ok || q != 3 || r != 2 {
        exit(3)
    }
    _, _, divided := divmod(1, 0)
    if divided {
        exit(4)
    }
    print("ok\n")
}
```

`guide.c`:

```c
// The C half of examples/extern-guide, the worked example of
// docs/extern.md.
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

int64_t point_sum(Point p) {
    return (int64_t)((uint64_t)p.x + (uint64_t)p.y);
}

tack_results_c_divmod c_divmod(int64_t a, int64_t b) {
    return (tack_results_c_divmod){a / b, a % b};
}
```

`tack --out guide examples/extern-guide/main.tck` builds it, and `./guide`
prints `ok`. `tack --emit-header examples/extern-guide/main.tck` writes the
`tack.h` the build uses, beside `guide.c`:

```c
// Generated by the Tack compiler. Do not edit.
//
// It declares every extern func of the program, for the C files that
// define them, and the types their signatures use.

#ifndef TACK_H
#define TACK_H

#include <stdbool.h>
#include <stdint.h>
#include <tack/tack.h>

typedef struct {
    int64_t x;
    int64_t y;
} Point;

typedef struct {
    int64_t r0;
    int64_t r1;
} tack_results_c_divmod;

// package main
int64_t count(tack_string /* s */, uint8_t /* c */); // extern func count(s string, c uint8) int64
int64_t point_sum(Point /* p */); // extern func sum(p Point) int64
tack_results_c_divmod c_divmod(int64_t /* a */, int64_t /* b */); // extern func c_divmod(a int64, b int64) (int64, int64)

#endif
```

`c_divmod` shows the trust boundary at its smallest: its C body divides
without a check, its doc comment states the promise its callers must keep,
and `divmod`, the Tack function a package would export, keeps it. Were
`divmod` to pass a zero, the compiler would not notice; the program would
be undefined.

## Not yet

- Raw C pointer types usable only in extern signatures, so a C library
  function taking a pointer can be bound without a C wrapper.
- Writable views, and slicing a view, across the boundary.
- Passing a C file flags, or linking a library.
- An ownership story for strings C returns.
- Lists passed to C, or returned from it, by value, which would move a
  list across the boundary; and allocators C defines.

Open questions about how extern C behaves are under "Extern C" in
[open-decisions.md](open-decisions.md).
