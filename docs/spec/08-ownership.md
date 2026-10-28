# 8. Ownership and memory

This chapter describes the memory model: which values own memory, how a
value is copied or moved, when a moved-from variable may no longer be
used, `own` parameters and arguments, `mut` parameters and receivers and
the rule that a `mut` argument is the only way to reach its place during
the call, views and the borrows they make (view parameters and view
results, and the conversion of a string to a view of its bytes,
`[]uint8(s)`), lists and maps as owners, allocators and what a list or
map holds of one, when memory is freed (end of scope, reassignment,
`defer`, early return), and what `exit` frees before the process ends.
It is the chapter that shows guarantees 2 and 3 (no leak, no
use-after-free) hold. The types themselves belong in
[Types](04-types.md); the operations on lists and maps, and `exit`'s
signature, belong in [Builtins](13-builtins.md); how a program ends
belongs in [Packages, modules and programs](12-packages.md); bounds
checks belong in [Safety and proofs](10-safety.md); how memory crosses
to C belongs in [Extern C](14-extern.md).

Status: skeleton. The sections below are planned.

## Values, copies and owners

## Moves

## Own parameters and arguments

## Mut parameters and receivers

## Exclusive access

## Views and borrows

## View results

## Lists

## Maps

## Allocators

## When memory is freed

## Exit
