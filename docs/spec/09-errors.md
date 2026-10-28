# 9. Errors

This chapter describes how a program reports and handles failure: the
`error` value's code and message, `noerror`, returning an error alone or
last among several results, the rule that an error must be checked
before the values beside it are used and may not be dropped, what drops
one (`_` where it goes, and a variable holding it that is never read,
which is reported as a drop, not as an unused name), checking it in
`if`, `switch` and loops, propagation, comparing two errors with `==`
and `!=`, sentinel errors, and errors in fields and arrays. The `error`
type's place among the types belongs in [Types](04-types.md); the other
unused-name rules belong in [Declarations and
scope](05-declarations.md); `std/errors` is part of the standard
library, not of this specification; the checked forms `v, ok := ...`,
which report failure as a `bool`, belong in [Safety and
proofs](10-safety.md).

Status: skeleton. The sections below are planned.

## Error values

## Returning an error

## Checking an error

## Dropping an error

## Propagation

## Sentinel errors

## Errors in fields and arrays
