# Known bugs

Bugs found and not yet fixed. Each is fixed in its own change, with a failing
test first, and removed from this list when it lands.

## A diagnostic at an implied semicolon points at a trailing comment

When a line ends in a comment, a parse error about the semicolon that the
line end implies is placed at the comment rather than at the newline. In
`exit(42 // c` followed by a newline and `)`, the error points at the `//`,
while the same source without the comment points at the newline. The
comment-as-newline sweep in `parser/comment_test.go` compares diagnostics by
message only because of this.

## A union built in place passed to a mut parameter without mut

`f(Expr.eof)`, where `f` takes a `mut` parameter, is `f takes e as mut;
pass mut Expr.eof`, suggesting a fix that is itself rejected, since a
union built in place is no place. The check reads `Expr.eof` as a field
of a name. A `mut` receiver written without `mut`, `Expr.eof.set()`,
already says `only a variable, a field or an element can be passed as
mut` instead; the parameter's wording should too.

## A function must end in a return even when no path reaches its end

A function with a result must end in a `return`, even when every path
before its end already returns. `func sign(x int64) int64` whose body is
`if x > 0 { return 1 }`, `if x < 0 { return -1 }` and `if x == 0 { return
0 }` is `sign must end in a return`, though the three conditions cover
every value. Tack has no way to say a place is unreachable, and a
`panic("unreachable")`, as Go writes it, would break the no-panic
guarantee. Today such a function ends in a `return` that never runs, as
`append_digits` in `std/bytes` does, or covers the last case with `else`.
The fix is to prove more paths end, or an `unreachable` statement the
compiler must prove unreachable, rejecting it otherwise; both wait for a
stronger prover.
