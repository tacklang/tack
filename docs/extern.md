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
declares.

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
  compiles every C file with that directory on the include path and
  `tack.h` included before the file's first line. So clang checks every
  definition against its extern func, whether the file includes `tack.h` or
  not; a definition that does not match is clang's `conflicting types`.
- A file may still `#include "tack.h"`, and needs to for an editor to know
  the types. `tack --emit-header <target>` writes `tack.h` into the
  target's directory and into every package directory with C files or
  extern funcs, and stops; add it to `.gitignore`. The programs under
  `examples/` keep theirs, for reading beside the C files, and a test
  checks each matches what the compiler generates. A stale copy beside a C
  file is skipped by the include guard of the one the build includes first.
  One `tack.h` serves the whole program, its extern funcs package by
  package.

## The ABI

Every Tack type crosses the boundary, in both directions, by value, with
the layout the generated C gives it, which is what `tack.h` defines. A
`mut` parameter is the one exception: it crosses as a pointer to the
caller's variable.

| Tack                         | C                                                                                   |
|------------------------------|-------------------------------------------------------------------------------------|
| `int8` ... `int64`           | `int8_t` ... `int64_t`                                                              |
| `uint8` ... `uint64`         | `uint8_t` ... `uint64_t`                                                            |
| `unsafe_size_t`              | `size_t`, from `<stddef.h>`, as wide as the target's                                |
| `unsafe_uintptr_t`           | `uintptr_t`, from `<stdint.h>`, as wide as the target's                             |
| `bool`                       | `bool`, from `<stdbool.h>`                                                          |
| `string`                     | `tack_string`: `const unsigned char *ptr; int64_t len;`, by value                   |
| a struct                     | a C struct with a member per field, under the field's Tack name, in order; `char unused;` if it has none |
| `[N]T`                       | a struct holding one C array, `T e[N]`, of at least one element                     |
| no result                    | `void`                                                                              |
| one result                   | that result's C type                                                                |
| several results              | one returned struct, `tack_results_<C name>`, with members `r0`, `r1`, and on       |
| a `mut p T` parameter        | `T *`, a pointer to the caller's variable, valid only during the call; never null   |

`tack.h` includes `<stdbool.h>` and `<stdint.h>`, and `<stddef.h>` when a
signature uses `size_t`, and defines every type an extern func's signature
uses and every type those hold, each before the types that hold it. Its
names:

- `tack_string` for `string`.
- A struct of package `main` keeps its Tack name: `Point`. One of any other
  package is its package's name, `_`, and its name: `Token` of
  `calc/lexer` is `lexer_Token`.
- An array is `tack_array_`, its length, `_`, and its element type's name,
  a basic type's being its Tack name: `[4]int64` is `tack_array_4_int64`,
  `[2]unsafe_size_t` is `tack_array_2_unsafe_size_t`,
  `[2]lexer.Token` is `tack_array_2_lexer_Token`, and `[2][3]Point` is
  `tack_array_2_tack_array_3_Point`.
- The results of extern func `f` are `tack_results_f`, after its C name.
- The prototypes name each parameter in a comment after its type,
  `int64_t /* n */`, not as a C name, which could be a C keyword or clash
  with a type's name.

A name `tack.h` cannot give is an error whatever is built:

- two things given one name, at the second with a note at the first: `type
  Token of package calc/b/lexer and type Token of package calc/a/lexer are
  both named lexer_Token in tack.h`;
- a type named like one of `tack.h`'s own: `type tack_string of package
  main cannot be named tack_string in tack.h: that is tack.h's string type`,
  and likewise `TACK_H`, its include guard, and `TACK_C_STR_LEN`,
  `TACK_STR` and `TACK_STR_N`, its string helpers, for a type or an extern
  func's C name: `extern func wrap of package main cannot be named
  TACK_STR_N in tack.h: that is a tack.h string helper`;
- a type or field named with a C keyword, once an extern signature uses
  it: `type int cannot be named int in C: it is a C keyword`, `field while
  of type Shape cannot be named while in C: it is a C keyword`.

A type no extern func uses is not in `tack.h`, whatever its names. C never
declares a type for Tack: every type is declared in Tack, and C structs a
library owns, such as `struct stat`, are copied field by field into a Tack
struct by a C wrapper.

So `extern func grow(mut p Point, k int64)` is `void grow(Point * /* p */, int64_t /* k */);`
in `tack.h`, and the C side reads and writes the caller's point through
`p->x`. Slices, which C would get as a `{ptr, len}` struct, do not exist in
Tack yet.

## Helpers for C

`tack.h` also defines helpers that make the C side shorter and harder to
get wrong, each only when the program uses what it helps with. When
`tack_string` is defined, because some extern signature uses a string,
so are three macros for building one:

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

`examples/extern-strings` uses each. They make a string, not its bytes'
lifetime: the bytes must still stay valid as the trust boundary below
says, which a literal's and a static array's do. A future helper follows
the same rule as these: it is defined in `tack.h`, only when the program
uses the type it serves, and its names are reserved there.

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
- **Strings from C.** A string C returns is taken as it is: its bytes must
  stay valid, and unchanged, for as long as Tack might use the string,
  which today means for the rest of the program. Nothing says who owns them
  or frees them; the memory model has not decided it yet.
- **Control.** C can end the process, with `exit` or `abort`, or never
  return, and either way no Tack defer runs. There is no way for C to call
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

`print` and `exit` are extern funcs too, declared in
[`src/std/builtin/builtin.tck`](../src/std/builtin/builtin.tck) as
`extern("tack_builtin_print") func print(s string)` and
`extern("tack_builtin_exit") func exit(code int64)`, and available in every
file without an import. The compiler ships their C bodies, one file per
builtin in [`codegen/c/builtin/`](../codegen/c/builtin), and writes each
into the generated C, as a `static` function, only when the program calls
it, with the header it needs: `<stdio.h>` for `print`, `<stdlib.h>` for
`exit`. A program that calls neither includes only `<stdbool.h>` and
`<stdint.h>`, which every freestanding C implementation has, so a target
without a C library needs only different bodies. Being static, they are not
symbols a C file can call or clash with.

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

typedef struct {
    const unsigned char *ptr;
    int64_t len;
} tack_string;

// TACK_C_STR_LEN(a) is the length of the string the byte array a holds,
// its size less the zero that ends it. A pointer does not compile.
#define TACK_C_STR_LEN(x) (sizeof (x) - 1 + 0 * sizeof (char[1 - 2 * __builtin_types_compatible_p(__typeof__(x), __typeof__(&(x)[0]))]))
// TACK_STR(lit) is the string literal lit as a tack_string. Only a
// literal compiles.
#define TACK_STR(lit) ((tack_string){(const unsigned char *)("" lit ""), sizeof ("" lit "") - 1})
// TACK_STR_N(p, n) is the n bytes at p, a const unsigned char * or an
// unsigned char *, as a tack_string. Any other pointer does not compile.
#define TACK_STR_N(p, n) ((tack_string){_Generic((p), const unsigned char *: (p), unsigned char *: (p)), (int64_t)(n)})

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
- Slices across the boundary.
- Passing a C file flags, or linking a library.
- An ownership story for strings C returns.

Open questions about how extern C behaves are under "Extern C" in
[open-decisions.md](open-decisions.md).
