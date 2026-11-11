# Known bugs

Bugs found and not yet fixed. Each is fixed in its own change, with a failing
test first, and removed from this list when it lands.

## Early exits that each keep every later write check in quadratic time

A block of early exits, each proving again what the code between them
writes, as straight-line code that assigns `j` and proves it again
before each use does, is checked in time that grows with the square of
the exits. Each exit's fact keeps every write to `j` after it, so its
endings are gathered from the rest of the block and, at its first
query, indexed for reach (`findEndings` in `typeck/region.go`,
`newReachIndex` in `typeck/reach.go`), once for each exit: about 0.3
seconds for 1000 such exits and 5.4 seconds for 4000. A query about `j`
reads only the newest of the exits' facts (`heldFactsAbout` in
`typeck/factindex.go`), so judging them is no longer the cost; sharing
the rest's endings between the facts over it would be. A closure after
each of many exits is quadratic too, since entering one copies every
fact in force that stays in force in its body (`enterClosure` in
`typeck/closure.go`).
