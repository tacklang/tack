# Known bugs

Bugs found and not yet fixed. Each is fixed in its own change, with a failing
test first, and removed from this list when it lands.

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
