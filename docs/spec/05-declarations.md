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

Status: skeleton. The sections below are planned.

## Scopes

## The blank identifier

## Package-level variables

## Type declarations

A type declaration may declare a struct type, among other kinds; a
literal of a struct type names every field:

```tack check
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

## Function declarations

## Method declarations

## Local variable declarations

## Redeclaration and shadowing

## Visibility

## Unused names
