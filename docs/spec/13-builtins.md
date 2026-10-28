# 13. Builtins

This chapter describes every builtin function, with its signature,
what it does and when it fails: `print`, `exit`, `len`, `make`, the list
operations (`append`, its spread form, `reserve`, `pop`, `remove` and
the rest), the map operations, and the generic builtins. It follows
[builtin/builtin.tck](../../builtin/builtin.tck), which documents each
builtin in the compiler's own source. `true`, `false`, `noerror` and
`c_allocator` are keywords, not builtins: `noerror` belongs in
[Errors](09-errors.md), and the others in [Types](04-types.md). This
chapter does not repeat the ownership or
safety rules a builtin is subject to, which belong in [Ownership and
memory](08-ownership.md) (what `exit` frees among them) and [Safety and
proofs](10-safety.md); how a program ends, `exit` among the ways,
belongs in [Packages, modules and programs](12-packages.md). Builtin
types belong in [Types](04-types.md).

Status: skeleton. The sections below are planned.

## Print

`print` writes exactly the bytes of its string to standard output.

## Exit

`exit` ends the process with the status it is given, of which only the
low bits reach the operating system, the low 8 on Linux and macOS. This
program prints a line with `print` and ends with status 3:

```tack run exit=3
package main

func main() {
    print("hi\n")
    exit(3)
}
```

```output
hi
```

## Length

## Make

## List operations

## Map operations

## Generic builtins
