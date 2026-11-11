# Known bugs

Bugs found and not yet fixed. Each is fixed in its own change, with a failing
test first, and removed from this list when it lands.

## A proof does not follow a relation through a stored value

The relations a proof chains leave out a stored value's bounds, so a
bound that reaches a variable only through one proves nothing. Under
`mut n int64 = 2; if 0 <= i && i < 50 && i < n { return a[i] }`, with
`a` a `[3]int64`, the index is rejected, though `i <= 1` makes it safe.
The rejection is safe.

## Many early exits check in quadratic time

A block with many early exits, each an `if` that leaves and puts a fact
in force for the rest of the block, followed by
more statements, is checked in time that grows with the square of the
exits. The exits' regions share the rest of the block, walked once for
its writes (`typeck/region.go`), but each exit changes the facts in
force, so the next query rebuilds the index of them by subject from
every fact in force (`factIndex` in `typeck/factindex.go`), and each
query about a variable reads every fact about it: about 0.2 seconds
for 4000 exits bounding `n` and 2.8 seconds for 16000, and, through the
driver, about 5 seconds for 4000 exits that each bound a new local and
a minute for 16000, since the rebuild does not depend on the variable. When the
statements after the exits also write the variable, as straight-line
code that assigns `j` and proves it again before each use does, each
exit's fact also keeps every write to `j` after it, and each use judges
every earlier exit's fact against them, so 4000 such exits take about 10
seconds.
