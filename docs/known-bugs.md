# Known bugs

Bugs found and not yet fixed. Each is fixed in its own change, with a failing
test first, and removed from this list when it lands.

## Early exits that each bound the same unwritten variable in quadratic time

A block of early exits that each put the same bound on a variable no
code writes, as `v := i`, then
`if v < 0 || d > len(a) || v >= d { return -1 }` before a use of `a[v]`,
with `a` an array of 4 and `d` a parameter, is checked in time that
grows with the square of the exits: about 0.3 seconds for 1000 such
exits and 3.3 for 4000. Each exit puts `d <= 4` in force again, and as
`d` is never written, every one of them holds wherever the first does,
so each links to the one before it and the whole chain holds. Each use
asks the relations in force, its graph reaches `d`, and it takes every
one of those bounds as its own edge from `d` to 0, then follows each in
its shortest paths. The edges a graph takes are what the relation
oracle checks, so leaving out a bound that says what another already
says changes the graph, not only the time it takes to build it.
