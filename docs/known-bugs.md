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

## A switch case that leaves on -1 proves nothing after the switch

A switch whose `case -1:` returns does not prove `d != -1` in the code
after it, though a returning `case 0:` proves `d != 0` there, and the
later cases and the default of the same switch do see `d != -1`. So
`switch d { case 0: return 0; case -1: return -%n }` followed by `n / d`
is rejected as a division that may overflow; an `if d == -1 { return
-%n }` before the division is accepted.

## A note ignores a bound that comes through a relation

A note about what a guard allows names the guard's own bound and misses
a tighter one that reaches the variable through a relation to another
variable with a known value. `mut k int64 = 1; if d >= k && d < 100 {
return x / d }` notes `this guard allows d == 0`, though `d >= 1`, and
`mut lo int64 = 0; if lo <= i && i < 50 { return a[i] }` notes that `i`
may be negative. Under `mut n int64 = 2; if 0 <= i && i < 50 && i < n {
return a[i] }`, with `a` a `[3]int64`, the note `this guard allows i ==
49` is false and the index is rejected, though `i <= 1` makes it safe.

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

## A closure with a scope parameter cannot go to a generic scope parameter

A closure that takes a scope parameter of its own cannot be passed to a
generic function's `scope f func(T)`: inferring `T` from the closure's
parameter type loses the parameter's scope mark, so `T` is the plain
function type and the closure's type no longer matches. Given `func
g[T Any](scope f func(T))`, `g(func(scope h func()) { h() })` is `cannot
use func(scope func()) as func(func()) in argument to g`. It is too
strict, never unsound: the call is rejected, and a closure whose
parameters are not scope, or a non-generic callee, is unaffected.

## A missing mut suggests passing an immutable variable as mut

A call that passes a variable to a `mut` parameter without `mut` is
told to `pass mut b`, even when `b` is immutable, where applying the
fix fails because an immutable variable cannot be passed as `mut`.
`Box.set(b, 1)` with `b := Box{}` shows it, and so does the same call
to a plain function. The message should say that `b` must be declared
`mut` as well.

## Moving a union case built through its type says a field cannot move

`own Expr.eof`, a union case built in place and passed to an `own`
parameter, reports `a field cannot be moved on its own`, which is not
what the expression is.

## The facts prove an assignment's target index after its value

The fact checker proves a target index as if the assignment's value had
already run, while the C backend evaluates the index first. So `if
len(l) > 0 { a[l[0]] = take(own l) }`, with `l` a `list[uint8]` and `a`
a `[256]int64`, is rejected as an index that may be out of range,
though `l[0]` is read before the move. The rejection is safe, but it
rejects a program whose order makes it valid.
