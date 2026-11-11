package typeck

// proverWork counts the steps the checker's provers take: arithmetic is
// each known value of an arithmetic operator worked out, rather than
// remembered (see knownArithmetic); relations each edge a relation query
// looks at, finding its graph and following its shortest paths (see
// relgraph.go); and indexed each fact read into the index of the
// relations in force; and scanned each fact a query of the facts in
// force about one subject reads (see factsAbout); locals each known
// value of a local worked out, rather than remembered (see knownVar); and
// conditions each condition whose facts are worked out, rather than taken
// from those of the && or || chain kept (see factsFrom); and walked each
// node a region's writes are indexed from (see indexWrites); and leaving
// each statement looked at to find the branches in a region that leave
// and the breaks and continues out of them, and each keep a branch's
// keep passes as it is linked (see keepWalk and keep.link); and judged
// each such branch a write in one is judged against at a use (see
// leftBefore), or a keep a reach index's ending is built from (see
// reachSet); and reached each ending a fact's query asks reaches about,
// each ending, interval and piece a reach index of them is built from,
// and each step of a query of one (see reach.go); and ranged each operator or operand an arithmetic
// expression's bound reads (see rangeOf); and decided each if, switch,
// fallthrough or loop for which whether it leaves or terminates is
// worked out, rather than remembered (see leavememo.go); and copied each
// statement of a block put into a region facts hold in, once for the
// block, and each node put in a region's head, before the rest of a
// block every region over it shares (see region.go); and ended each
// ending put in a fact's own endings, or judged into the endings of a
// block's rest that the facts over it share (see sharedEnds), and each
// ending compared to find whether one fact's endings are among
// another's (see endsWithin). They count work,
// not time, so a test can bound how checking grows with the program
// checked, whatever else the machine is doing.
type proverWork struct {
	arithmetic int
	relations  int
	indexed    int
	scanned    int
	locals     int
	conditions int
	walked     int
	leaving    int
	judged     int
	reached    int
	ranged     int
	decided    int
	copied     int
	ended      int
}
