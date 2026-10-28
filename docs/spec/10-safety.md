# 10. Safety and proofs

This chapter states the four guarantees every valid program keeps (no
out-of-bounds access, no leak, no use-after-free, no panic) and
describes how the compiler enforces those that rest on proofs: which
operations can fail (division and remainder, indexing arrays, strings,
views and lists, looking up a map's key, `m[k]`, slicing, conversions),
the guard rule and what must be proven for each, known values, facts
and what gives them, relations between variables, where a fact holds
and what ends it, contracts (`ensures`), the checked forms
`v, ok := ...` the language has, which turn a failure into a value, the
rule that an `ok` may not be dropped or left unread, and the operations
that have no checked form, what the prover does not accept, and the
depth of the stack, which no rule bounds yet. It also lists the known
violations of the guarantees that are parked, as
[guarantees.md](../guarantees.md) does. Leak and use-after-free freedom
are shown in [Ownership and memory](08-ownership.md), and the result of
each operation in [Constants and arithmetic](03-arithmetic.md); this
chapter says only when the operation is allowed.

Status: skeleton. The sections below are planned.

## The guarantees

## Operations that can fail

## The guard rule

## Known values

## Facts

## Relations

## Where a fact holds

## What does not count as a proof

## Contracts

## The checked form

The checked form gives a failure as a `bool` instead of a proof, as in a
division by a value nothing proves is not zero:

```tack fragment
q, ok := a / b
```

## Per operation

A divisor nothing proves is not zero is an error:

```tack error
package main

func half(a int64, b int64) int64 {
    return a / b
}

func main() {
    code, fits := int32(half(8, 2))
    if fits { exit(code) }
}
```

```diagnostics
main.tck:4:16: error: divisor b may be zero; check b != 0 first, or use q, ok := ... / b
```

## Stack depth

## Known violations
