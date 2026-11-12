# Known bugs

Bugs found and not yet fixed. Each is fixed in its own change, with a failing
test first, and removed from this list when it lands.

## Early exits that each bound an unwritten variable, each use under a contradiction, in quadratic time

A block of early exits that each put the same bound on a variable no
code writes, as `v := i`, then
`if v < 0 || d > len(a) || v >= d { return -1 }`, with `a` an array of
4 and `d` a parameter, each followed by a use of `a[v]` under a guard
that contradicts that bound, `if d > 5 { t +%= a[v] }`, is checked in
time that grows with the square of the exits: about 0.46 seconds for
1000 such exits and 4.35 for 4000. Each exit's bound `d <= 4` is folded
into the first's, so a query's graph takes it once, but `d > 5` and
`d <= 4` make a cycle of negative weight, so the search for shortest
paths does not settle, and the bounds it finds in as many rounds as the
graph has nodes depend on how many edges alike it follows. To answer as
it would with every edge alike, the query builds its graph again keeping
them, which takes every one of the n bounds on `d`.

## Early exits that each bound a variable written only where code leaves, in quadratic time

A block of early exits that each put the same bound on a variable the
code writes only in branches that then leave, as `mut m := d`, then for
each exit `v := i`, `if v < 0 || m > len(a) || v >= m { return -1 }`,
`if v == 3 { m = 2; return -1 }` and a use of `a[v]`, is checked in time
that grows with the square of the exits: about 0.29 seconds for 1000
such exits and 3.1 for 4000. Each branch's write ends the bounds
before it only inside the branch, so every bound `m <= 4` still holds
at each use, but each has an ending the newer ones lack, so none holds
exactly where the one it shadows does and none is folded into another:
they are linked in one chain, every one of them holding. A query's
graph keeps only the first of them, but its walk of the chain still
judges every one.
