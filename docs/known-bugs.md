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

## A proof does not follow a relation through a stored value

The relations a proof chains leave out a stored value's bounds, so a
bound that reaches a variable only through one proves nothing. Under
`mut n int64 = 2; if 0 <= i && i < 50 && i < n { return a[i] }`, with
`a` a `[3]int64`, the index is rejected, though `i <= 1` makes it safe.
The rejection is safe.
