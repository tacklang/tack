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

## Checking writes before uses grows with writes times uses

Deciding whether a write reaches a use scans every recorded write for each
use, so a function with many writes and many uses of the same variable is
checked in time that grows with their product. A `switch` with N cases that
each write the variable and then return, followed by N uses, takes about 7
seconds at N = 16000. Since leaving branches stopped ending facts, shapes
the checker used to reject early pay this cost too: N leaving cases that
each write inside a loop, followed by N uses, take about 11 seconds at
N = 16000. Ordinary programs are far below these sizes.

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

## A closure with a scope parameter cannot go to a generic scope parameter

A closure that takes a scope parameter of its own cannot be passed to a
generic function's `scope f func(T)`: inferring `T` from the closure's
parameter type loses the parameter's scope mark, so `T` is the plain
function type and the closure's type no longer matches. Given `func
g[T Any](scope f func(T))`, `g(func(scope h func()) { h() })` is `cannot
use func(scope func()) as func(func()) in argument to g`. It is too
strict, never unsound: the call is rejected, and a closure whose
parameters are not scope, or a non-generic callee, is unaffected.

## The facts prove an assignment's target index after its value

The fact checker proves a target index as if the assignment's value had
already run, while the C backend evaluates the index first. So `if
len(l) > 0 { a[l[0]] = take(own l) }`, with `l` a `list[uint8]` and `a`
a `[256]int64`, is rejected as an index that may be out of range,
though `l[0]` is read before the move. The rejection is safe, but it
rejects a program whose order makes it valid.
