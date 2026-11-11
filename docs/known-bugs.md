# Known bugs

Bugs found and not yet fixed. Each is fixed in its own change, with a failing
test first, and removed from this list when it lands.

## Early exits that each prove a relation in quadratic time

A block of early exits, each proving again a relation between variables
the code between them writes, as `j = i`, `k = n`, then
`if j < 0 || k > len(a) || j >= k { return -1 }` before each use of
`a[j]` does, is checked in time that grows with the square of the exits:
about 1 second for 1000 such exits and 18 for 4000. Each exit changes the
facts in force, so the next query of the relations builds their index
again from every fact in force (`relationIndex` in `typeck/relgraph.go`),
and the graph a query builds judges every relation in force about the
variables it reaches, one for each exit, though only the newest holds
(`holdsAt` in `typeck/relgraph.go`), where a query of one subject reads
only facts a newer one does not shadow (`heldFactsAbout` in
`typeck/factindex.go`).
