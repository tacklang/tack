# 3. Constants and arithmetic

This chapter describes values of the numeric and boolean kinds and what
the operators compute on them: constant expressions and how they are
evaluated at compile time, the type a constant takes, integer overflow,
division and remainder, shifts and bitwise operators, what comparing two
integers or two floats computes, conversions `T(x)` from one numeric
type to another (integer to integer, integer to float, float to integer
and float to float) and what each computes, and float semantics. It
states what each operation yields and when it is an error. Which integer
type may be used where another is expected without a conversion belongs
in [Types](04-types.md), under assignability, as does what comparing
strings, `bool`s, arrays, lists, structs or unions computes; comparing
two errors belongs in [Errors](09-errors.md); the conversion of a
string to a view of its bytes, `[]uint8(s)`, belongs in [Ownership and
memory](08-ownership.md), with views. When an operation must be proven
safe or checked, what the proof needs (a divisor is not zero, a
conversion fits), and the checked forms such as `v, ok := T(x)`,
belong in [Safety and proofs](10-safety.md). The spelling of literals belongs
in [Source and lexical elements](02-lexical.md), and the operators'
syntax and precedence in [Expressions](06-expressions.md).

Status: skeleton. The sections below are planned.

## Constant expressions

## Types of constants

## Integer overflow

## Division and remainder

## Shifts and bitwise operators

## Comparisons

## Conversions

## Floats
