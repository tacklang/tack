# Known bugs

Bugs found and not yet fixed. Each is fixed in its own change, with a failing
test first, and removed from this list when it lands.

## Early exits that each relate new locals in quadratic time

A block of early exits, each relating two new locals, as `v := i`,
`w := n`, then `if v < 0 || w > len(a) || v >= w { return -1 }` before a
use of `a[v]`, is checked in time that grows with the square of the
exits: about 0.4 seconds for 1000 such exits and 6 for 4000. Each use
asks the relations in force, and the graph it builds reaches 0 through
the bound on `v`, where the bound every exit put on its own local
against 0 still holds, one for each exit, so the query judges every one
of them (`holdingAt` in `typeck/relindex.go`), though the graph keeps at
most `relationCap` nodes. The index of the relations is kept in step
with the facts in force, and a closure's body after each exit reads the
one kept for it, so only the edges a query looks at grow this way (see
`TestClosuresAfterRelatedExitsIndexLinearly`).
