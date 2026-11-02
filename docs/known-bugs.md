# Known bugs

Bugs found and not yet fixed. Each is fixed in its own change, with a failing
test first, and removed from this list when it lands.

## A length that is not proven gets no note

A length of a string, view or list, `len(s)`, used as a divisor, a
slice's bound, an index or a converted value, alone or in arithmetic,
gets its error with no note when it is not proven, whatever guards bound
it: `10 / len(s)` under `if len(s) < 10` and `t[:len(s)]` under
`if len(s) < 5` report the error alone. A length alone is also no index
or converted value the facts prove, though the same length in
arithmetic is: under `len(s) < len(t)`, `t[len(s)]` is rejected while
`t[len(s) - 0]` is accepted, and under `len(s) <= 255`,
`uint8(len(s))` is rejected while `uint8(len(s) + 0)` is accepted.

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

## A statement and a case on one line after a clause header fails to parse

A clause whose header ends its line cannot have a statement followed by the
next case on the following line. In a `switch`, `case 1:` on its own line
followed by `exit(3); case 2:` on the next is `expected 'case or default'
found 'exit'`, pointing at `exit`, and a `default:` header does the same.
The same clauses written on one line, `case 1: exit(3); case 2: exit(4)`,
parse.

## Checking writes before uses grows with writes times uses

Deciding whether a write reaches a use scans every recorded write for each
use, so a function with many writes and many uses of the same variable is
checked in time that grows with their product. A `switch` with N cases that
each write the variable and then return, followed by N uses, takes about 7
seconds at N = 16000. Since leaving branches stopped ending facts, shapes
the checker used to reject early pay this cost too: N leaving cases that
each write inside a loop, followed by N uses, take about 11 seconds at
N = 16000. Ordinary programs are far below these sizes.

## A note says a copied index may be negative when it is not

A guard on a variable copied from another one can get a note that says
the copy may be negative though its source was checked. In `if j >= 0 {
k := j; if k < len(t) { return s[k] } }`, with `s` a `[4]int64`, the error
on `s[k]` is right, since `k < len(t)` does not bound `k` by 4, but the
note `this guard bounds k only above; k may be negative` is false: `k >=
0` holds from `j >= 0`.

## A function with a syntax error hides a size error in a later function

When a function's body fails to parse, an array type in it that is too
large stops the same type being reported in a later function. A file
whose first function holds `y := )` and then `mut a
[1152921504606846976]int64`, and whose `main` declares `mut b
[1152921504606846976]int64`, reports only the syntax error, without
`array type ... is too large` for `b`. The file is still rejected, by the
syntax error, so no program runs with the type.
