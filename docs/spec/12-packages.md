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

Status: skeleton. The sections below are planned.

## Source files and the package clause

## Packages of several files

## Modules and tack.mod

## Import paths and package names

## Import declarations

## The standard library

## Program execution and termination
