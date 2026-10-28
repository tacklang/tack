# 12. Packages, modules and programs

This chapter describes how source files make a program: the package
clause, packages of several files and the order their files are read,
modules and `tack.mod`, import paths and package names, the `import`
declaration and what an imported name may be used for, the standard
library's place as the module `std` (whose `std/builtin` documents the
builtins and is no package: it cannot be imported), the `main` package
and `func main`, and how a program ends: returning from `main`,
or calling `exit`. Package-level variables' initializers, and
initialization cycles, belong in [Declarations and
scope](05-declarations.md); `exit`'s signature and status belong in
[Builtins](13-builtins.md), and what it frees in [Ownership and
memory](08-ownership.md). Visibility of names across packages belongs in
[Declarations and scope](05-declarations.md); the contents of std's
packages are not part of this specification; how C files of a package
are built belongs in [Extern C](14-extern.md).

Examples in this specification are single files, so the examples of
modules, of packages of several files and of imports of a module's own
packages below are fragments, with their file trees shown as text.

## Source files and the package clause

Every source file starts with a package clause, before any import or
declaration:

```tack fragment
package lexer
```

A package name is lowercase ASCII letters and digits, starting with a
letter, and is not a keyword or the name of a builtin type or function.
`builtin` and `std` are names like any other.

A file compiled on its own is the whole of its package, and is built as
a program only when its package is `main`.

## Packages of several files

A package may be split across files. A directory is compiled as one
package made of every file directly in it whose name ends in `.tck`. A
file whose name starts with `.` is skipped, and so is every
subdirectory, even one named like a Tack file. A directory with no such
file is an error, `no .tck files in dir`.

The files are read in the byte order of their names. Every file is
lexed, and then every file parsed, so the problems of each are
reported.

Every file's package clause names the same package; otherwise the
directory is reported as holding several, `directory dir holds two
packages, a and b`, with a note at the first file of each other one.

Every package-level name of every file, a function, a type or a global,
is visible in every file of the package, in any order, exactly as within
one file. A name declared in two files is `x is already declared`, with
a note at the other declaration. A parameter or local may not reuse a
global of another file, and a global read only in another file is used.

## Modules and tack.mod

A directory is compiled in its module. A module is a directory tree
whose root holds a file named `tack.mod`, which names the module in one
line:

```text
module calc
```

`tack.mod` may also hold blank lines and `//` comments, which run to the
end of the line wherever they start. Anything else in it is an error at
the line it is on: a second `module` line, a `module` without a name or
with two, another directive, or a bad name.

A module name is one or more parts joined by `/`. Each part is lowercase
ASCII letters, digits, `-`, `.` and `_`, and does not start or end with
`.` or `-`, as in `example.com/my-tools`. `std` is a module name like
any other (see [The standard library](#the-standard-library)).

A directory's module is the one whose `tack.mod` is in that directory or
the nearest directory above it. A directory with no `tack.mod` in it or
above it is an error, `no tack.mod found in dir or any parent
directory`. A file compiled on its own needs no `tack.mod` and belongs
to no module.

## Import paths and package names

A package's import path is its module's name followed by its
directory's path below the module's root. In this module

```text
calc/
    tack.mod          module calc
    lexer/
        lexer.tck     package lexer
        token.tck     package lexer
    cmd/
        calc/
            main.tck  package main
```

the directory `lexer` is the package `calc/lexer`, and a package at the
root has the module's name as its import path.

A package is named by the last part of its import path, and every
file's package clause says that name, or `main`, which may be in any
directory. A clause naming another package is `package lex does not
match its directory; expected package lexer`. A directory whose name is
not a valid package name may hold only a `main` package, `my-tools is
not a valid package name: use lowercase letters and digits`, though
directories below it may hold packages.

One module may hold several `main` packages, each built from its own
directory or import path. A program is built from a `main` package;
building any other package is `calc/lexer is not a main package`.

## Import declarations

An import declaration makes a package usable in the file that holds
it, under a name the importer chooses:

```tack fragment
import lexer from "calc/lexer"
```

The name is required. Imports come right after the package clause, one
per line; `import (...)` is an error, and so is an import after another
declaration. `from` is a keyword, reserved everywhere though only an
import uses it.

An import's name is declared in its file, and in no other file of the
package. It may not be a reserved name, a builtin's name, a
package-level name of its package in any file, or another import's
name in the file, and no local or parameter may reuse it, `lexer is an
imported package and cannot be redeclared`.

Through the name, a qualified name `lexer.X` names a package-level name
`X` of the imported package that is visible to the importer (see
[Declarations and scope](05-declarations.md)):

- a function, which may be called, `lexer.First(src)`, or used as a
  function value;
- a global, which may be read, `lexer.KindNumber`, and never assigned:
  a mutable global is never `pub` or `lib`, so an imported global is
  always immutable, and assigning one is `cannot assign to immutable
  variable lexer.KindNumber`;
- a type, written anywhere a type may be, in a signature, a variable,
  an array's element or a field, and in a literal, `lexer.Token{kind:
  1}`.

The name alone is nothing: `lexer is a package, not a value`. A
qualified name that names nothing is `undefined: lexer.Nothing`.

An import path starts with a module's name. These are errors:

- a relative or absolute path, `import path must start with a module
  name, such as calc/lexer; relative paths are not allowed`, and a path
  with an empty, `.` or `..` part;
- a path in another module, `unknown module other in import path
  other/pkg`;
- a path that names no directory, `cannot find package calc/nothing: no
  directory calc/nothing in module calc`;
- an import cycle, `import cycle: calc/a imports calc/b imports calc/a`;
- importing a `main` package, `cannot import calc/cmd/calc: it is a main
  package`;
- importing one package twice in a file, `calc/lexer is already imported
  as a`;
- from a file compiled on its own, importing any package outside the
  standard library.

An import no name of its file uses is covered by the unused-name rules
of [Declarations and scope](05-declarations.md). Every import is
checked, so one run reports each bad one.

```tack error
package main

import lexer from "calc/lexer"
import fmt from "std/fmt"

func main() {
    fmt.println("hi")
}
```

```diagnostics
main.tck:3:19: error: cannot import calc/lexer: a single file can import only the standard library
```

## The standard library

The standard library is the module `std`. An import path is resolved
in this order:

- `std/builtin` is an error (below);
- in a package of the module being built, a path that is the module's
  name or starts with it and `/` names that module's own package, even
  when it starts with `std`, so in a module named `std`, or one whose
  name starts with `std/`, such a path never names the installed
  standard library's package;
- any other path whose first part is `std` names a package of the
  standard library, from any module and from a file compiled on its
  own;
- any other path names a package of the module being built.

The contents of std's packages are outside this specification.

```tack run exit=0
package main

import fmt from "std/fmt"

func main() {
    fmt.println("hello")
}
```

```output
hello
```

A std package follows every rule a package of a program's module does.
Its `lib` names are usable only from std's own packages, `secret is lib
in module std and cannot be used from module calc`, or `from a single
file, which has no module`. A std package imports only std packages,
`cannot import calc/lexer: a package of std can import only std
packages`. A std path naming no directory is `cannot find package
std/nosuch: no directory std/nosuch in module std`.

`std/builtin` holds a copy of the builtins' documentation and is no
package; it cannot be imported:

```tack error
package main

import b from "std/builtin"

func main() {
}
```

```diagnostics
main.tck:3:15: error: std/builtin documents the builtins and cannot be imported
```

## Program execution and termination

A program is a `main` package and every package it imports, directly or
through other imports. A `main` package declares a function `main`,
which takes no parameters and declares no result; a `main` package
without one is `main function not found`. No package other than `main`
declares anything named `main`, `func main is allowed only in package
main`, and `main` is not extern, `main function must not be extern`.

```tack error
package main

func main(n int64) int64 {
    return n
}
```

```diagnostics
main.tck:3:11: error: main function must not declare parameters
main.tck:3:20: error: main function must not declare a result
```

A program starts by calling `main`. Package-level variables hold their
values before it starts: their initializers are computed at compile
time (see [Declarations and scope](05-declarations.md)). A program's
arguments reach it only through C (see [Extern C](14-extern.md)).

A program ends in one of two ways:

- `main` returns. Its block ends as any function's does, running its
  defers and freeing what it owns (see [Ownership and
  memory](08-ownership.md)), and the process ends with status 0.
- The program calls `exit`, from any function. The process ends at once
  with the status given, running no pending defer in any function (see
  [Builtins](13-builtins.md)).

Code behind an extern func can also end the process; what follows then
is described in [Extern C](14-extern.md).

This program returns from `main`, which runs its defer after its last
statement, and ends with status 0:

```tack run exit=0
package main

func main() {
    defer {
        print("deferred\n")
    }
    print("returning\n")
}
```

```output
returning
deferred
```
