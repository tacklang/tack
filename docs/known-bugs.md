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

## A proof does not follow a relation through a stored value

The relations a proof chains leave out a stored value's bounds, so a
bound that reaches a variable only through one proves nothing. Under
`mut n int64 = 2; if 0 <= i && i < 50 && i < n { return a[i] }`, with
`a` a `[3]int64`, the index is rejected, though `i <= 1` makes it safe.
The rejection is safe.

## Many early exits, or a long else-if chain with many uses after it, check in quadratic time

A block with many early exits, each an `if` that leaves, followed by more
statements, is checked in time that grows with the square of its
length, since indexing each exit's writes walks the rest of the block:
about a second for 1000 exits and 30 seconds for 4000. A long chain of
`else if` links followed by as many statements is quadratic for the same
reason: the region each link's facts hold in is the next link followed by
the rest of the block, so the rest is copied once a link: about 2.1
seconds for 16000 links with 16000 uses after them. Both come from each
exit or link getting a region of its own over the rest of the block,
copied or walked once for each.
