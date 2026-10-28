# 11. Generics and interfaces

This chapter describes interface declarations, how a struct or union
type satisfies an interface (its methods, signatures and `mut`
receivers, and their visibility from the call), generic functions with
type parameters constrained by interfaces, interface parameters, how a
call chooses its type arguments, and what a generic body may do with a
value of a type parameter. Only a function is generic: a method, an
`extern func` and a type cannot have type parameters, as "The language
today" in the [README](../../README.md) says under generic functions.
Methods themselves are declared as in [Declarations and
scope](05-declarations.md), and `any`, which is not an interface,
belongs in [Types](04-types.md).

Status: skeleton. The sections below are planned.

## Interface declarations

## Satisfying an interface

## Generic functions

## Interface parameters

## Type arguments and inference

## What a generic body may do
