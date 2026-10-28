# 4. Types

This chapter describes every kind of type and what values it holds: the
integer types, the target-width integers, the floats, `bool`, `string`,
`error`, `any`, `Allocator`, arrays, views, structs, unions, lists, maps
and function types, with each one's zero value (or lack of one), its
size and layout limits, and which types are comparable, ordered,
copyable and owned. It owns the value of a string or raw string
literal, and what comparing two strings, `bool`s, arrays, lists,
structs or unions computes; comparing numbers belongs in [Constants and
arithmetic](03-arithmetic.md), and comparing errors in
[Errors](09-errors.md). It also owns the values
of the keywords `true`, `false` and `c_allocator`; `noerror` belongs in
[Errors](09-errors.md). It says when two types are the same and when a
value of one may be used as another without a conversion, the implicit
widening of integers included; what a numeric conversion computes
belongs in [Constants and arithmetic](03-arithmetic.md), and the
conversion of a string to a view of its bytes, `[]uint8(s)`, in
[Ownership and memory](08-ownership.md). How a type is declared and
named belongs in [Declarations and scope](05-declarations.md); what
owning, moving and freeing a value of a type means belongs in
[Ownership and memory](08-ownership.md); interfaces as constraints
belong in [Generics and interfaces](11-generics.md); and the C layout of
each type belongs in [Extern C](14-extern.md).

Status: skeleton. The sections below are planned.

## Integer types

## Target-width integers

## Float types

## Booleans

## Strings

## The error type

## The any type

## Allocators

## Arrays

## Views

## Structs

## Unions

## Lists

## Maps

## Function types

## Zero values

## Size limits

## Type identity and assignability

## Implicit widening

## Comparable and ordered types
