package typeck

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"sort"

	"github.com/tacklang/tack/ast"
)

// A fact holds at c.at unless one of the writes in its region that may
// end it reaches c.at (see dropOf and reaches). A function with n writes
// to a variable in a fact's region and n uses of it the fact proves would
// ask reaches about every write at every use, n times n. So a fact with
// more than reachScanLimit endings answers through an index of them
// instead, built the first time it is asked and kept with the endings,
// which never change.
//
// Whether an ending e reaches c.at depends, but for three things the
// checker may be in the middle of, only on c.at: e.at < c.at, or c.at in
// e's loop, and not leftBefore, which walks e's keeps along their skips
// asking about c.at alone. So the positions e reaches are a union of a
// few intervals, worked out once (see reachSet), and the first ending
// that reaches c.at is the one with the least position in the list among
// those whose intervals hold c.at: a stabbing query, answered by a
// binary search over the cuts between the intervals, each piece between
// two cuts labeled with the first ending whose intervals cover it (see
// reachIndex).
//
// The three other things each make an ending reach c.at by where the
// ending is, whatever c.at is: c.at in deferred code and e outside it,
// c.at in an assignment's targets and e in that assignment, and c.at in
// a switch's default written before some of its cases and e in that
// switch outside the case being checked (see reaches). So while any of
// them is in force, the first such ending is found too, as the one with
// the least position in the list among those whose own position lies in
// a few intervals: a range query over the endings sorted by position
// (see atIndex). The first ending that reaches c.at is the earlier of the
// two answers.
//
// The endings of the facts over the rest of one block are, for each
// declaration, suffixes of one list, the writes to it in the rest from
// the first statement any fact's region holds on (see sharedEnds). So a
// run of a fact's endings may be such a suffix, from some position in
// the list on, answered by indexes of the whole list built once for
// every fact over the rest: the first ending at or after from that
// reaches c.at. For the positions each ending reaches, each interval of
// its reachSet is put in the few nodes of a segment tree over the pieces
// between the cuts that cover it, as the position of the ending in the
// list, and since the endings are taken in order each node's positions
// are in increasing order. The first ending at or after from reaching
// c.at is then the least, of the first at or after from in each node on
// the path from c.at's piece to the root, each found by a binary search
// (see suffixReach). For the endings whose own position lies in an
// interval, each node of a segment tree over the endings sorted by
// position holds the positions in the list of those it covers, in
// increasing order, and the first at or after from in the interval is
// the least of the first at or after from in each node the interval
// splits into (see suffixAt).
//
// An index that answered differently from reaches could keep a fact in
// force past a write that ends it, so when the tests set reachOracle
// every query reads the index, however few endings there are, and checks
// it against a scan of them all.

// reachScanLimit is how many endings a fact's query asks reaches about
// one by one before it reads them through an index, which costs a sort
// to build.
const reachScanLimit = 16

// reachOracle says to answer every query of a fact's endings through
// their index and check it against a scan of them (see reach.go). It is
// set in a build with the tag tackfactoracle, and by the tests that
// compare the two, which set it only while no other test runs.
var reachOracle = factOracle

// endings is every write in a fact's region that may end it, in the order
// writeIndex.endings gives them, as a chain of runs of them through next,
// each run's list in order and every ending of a run before those of the
// next. A run is the fact's own, with the indexes of it a query builds
// the first time it needs one: byUse, for the positions each ending
// reaches, and byAt, for where each ending is. Or it is shared: list is
// shared.list from the position from on, and shared's indexes answer for
// it (see sharedEnds). A run's list is never empty. A fact copied shares
// its endings, and with them their indexes, since the endings never
// change.
type endings struct {
	list   []ending
	byUse  *reachIndex
	byAt   *atIndex
	shared *sharedEnds
	from   int
	next   *endings
}

// count returns how many endings es holds, in all its runs.
func (es *endings) count() int {
	n := 0
	for run := es; run != nil; run = run.next {
		n += len(run.list)
	}
	return n
}

// all returns every ending of es, its runs one after the other, in a
// slice of their own when there is more than one run.
func (es *endings) all() []ending {
	if es == nil {
		return nil
	}
	if es.next == nil {
		return es.list
	}
	var out []ending
	for run := es; run != nil; run = run.next {
		out = append(out, run.list...)
	}
	return out
}

// ival is the positions lo through hi, both included; it is empty when lo
// is past hi.
type ival struct {
	lo, hi int
}

// spanIval returns the positions inSpan says lie in span: after its start
// and at or before its end.
func spanIval(span ast.Span) ival {
	if span.Start == math.MaxInt {
		return ival{1, 0}
	}
	return ival{span.Start + 1, span.End}
}

// above returns the positions past at, or none when at is math.MaxInt.
func above(at int) ival {
	if at == math.MaxInt {
		return ival{1, 0}
	}
	return ival{at + 1, math.MaxInt}
}

// every is every position.
var every = ival{math.MinInt, math.MaxInt}

// cut returns the positions of set outside v, set being any intervals,
// overlapping or not.
func cut(set []ival, v ival) []ival {
	if v.lo > v.hi {
		return set
	}
	var out []ival
	for _, s := range set {
		if s.hi < v.lo || s.lo > v.hi {
			out = append(out, s)
			continue
		}
		if s.lo < v.lo {
			out = append(out, ival{s.lo, v.lo - 1})
		}
		if s.hi > v.hi {
			out = append(out, ival{v.hi + 1, s.hi})
		}
	}
	return out
}

// clip returns the positions of set inside v.
func clip(set []ival, v ival) []ival {
	var out []ival
	for _, s := range set {
		if lo, hi := max(s.lo, v.lo), min(s.hi, v.hi); lo <= hi {
			out = append(out, ival{lo, hi})
		}
	}
	return out
}

// reachSet returns the positions c.at may be at for e to reach it when no
// deferred code, assignment's targets or late default is being checked:
// those reaches answers true at then, as intervals. It is the positions
// past e.at, or in e's loop, less those leftBefore answers true at, found
// by asking leftBefore's question of every position at once, keep by
// keep along the skips: the positions a keep's whole holds are answered
// no, those left at or before its resume yes, and the rest go on to the
// next keep. Each keep looked at counts as work, as leftBefore's do.
func (c *Checker) reachSet(e ending) []ival {
	set := []ival{above(e.at)}
	if loop := spanIval(e.loop); loop.lo <= loop.hi {
		set = append(set, loop)
	}
	rest := []ival{every}
	for k := e.kept; k != nil && len(rest) > 0; k = k.skip {
		c.work.judged++
		rest = cut(rest, spanIval(k.whole))
		for _, left := range clip(rest, ival{math.MinInt, k.resume}) {
			set = cut(set, left)
		}
		rest = clip(rest, above(k.resume))
	}
	return set
}

// reachIndex answers which ending of a list is the first to reach a
// position, when no deferred code, assignment's targets or late default
// is being checked: cuts are the positions, in increasing order, where
// the answer may change, and first[i] the position in the list of the
// first ending reaching every position from cuts[i] up to the next cut,
// or -1 for none. No ending reaches a position before the first cut.
type reachIndex struct {
	cuts  []int
	first []int
}

// newReachIndex returns the reachIndex of list, from the reachSet of each
// of its endings. Each piece between two cuts is labeled by the first
// ending covering it, so the endings are taken in order and each labels
// only pieces none before it did, skipping the labeled ones through
// next, which points each piece at the first piece from it not yet
// labeled. Each ending, and each interval and piece labeled, counts as
// work.
func (c *Checker) newReachIndex(list []ending) *reachIndex {
	sets := make([][]ival, len(list))
	var cuts []int
	for i, e := range list {
		c.work.reached++
		sets[i] = c.reachSet(e)
		for _, v := range sets[i] {
			cuts = append(cuts, v.lo)
			if v.hi < math.MaxInt {
				cuts = append(cuts, v.hi+1)
			}
		}
	}
	slices.Sort(cuts)
	cuts = slices.Compact(cuts)
	first := make([]int, len(cuts))
	next := make([]int, len(cuts)+1)
	for i := range first {
		first[i] = -1
		next[i] = i
	}
	next[len(cuts)] = len(cuts)
	free := func(j int) int {
		root := j
		for next[root] != root {
			root = next[root]
		}
		for next[j] != root {
			next[j], j = root, next[j]
		}
		return root
	}
	for i, set := range sets {
		for _, v := range set {
			c.work.reached++
			from, _ := slices.BinarySearch(cuts, v.lo)
			to := len(cuts)
			if v.hi < math.MaxInt {
				to, _ = slices.BinarySearch(cuts, v.hi+1)
			}
			for j := free(from); j < to; j = free(j) {
				c.work.reached++
				first[j] = i
				next[j] = j + 1
			}
		}
	}
	return &reachIndex{cuts: cuts, first: first}
}

// at returns the position in the list of the first ending reaching the
// position at, or -1 for none. Each step of its search counts as work.
func (r *reachIndex) at(at int, work *int) int {
	i := sort.Search(len(r.cuts), func(i int) bool {
		*work++
		return r.cuts[i] > at
	})
	if i == 0 {
		return -1
	}
	return r.first[i-1]
}

// atIndex finds the first ending of a list whose own position lies in an
// interval: order holds the positions in the list of the endings, sorted
// by where each is, ats where each of them is, in the same order, and
// least a tree of the least of order over each run of it: least[n+i] is
// order[i], for n endings, and least[i] the lesser of least[2i] and
// least[2i+1].
type atIndex struct {
	order, ats, least []int
}

// newAtIndex returns the atIndex of list. Each ending counts as work.
func (c *Checker) newAtIndex(list []ending) *atIndex {
	n := len(list)
	order := make([]int, n)
	for i := range order {
		c.work.reached++
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(list[a].at, list[b].at) })
	ats := make([]int, n)
	least := make([]int, 2*n)
	for i, j := range order {
		ats[i] = list[j].at
		least[n+i] = j
	}
	for i := n - 1; i > 0; i-- {
		least[i] = min(least[2*i], least[2*i+1])
	}
	return &atIndex{order: order, ats: ats, least: least}
}

// first returns the least position in the list of an ending whose own
// position lies in v, or -1 for none. Each step of its search counts as
// work.
func (x *atIndex) first(v ival, work *int) int {
	if v.lo > v.hi {
		return -1
	}
	n := len(x.ats)
	lo := sort.Search(n, func(i int) bool { *work++; return x.ats[i] >= v.lo })
	hi := sort.Search(n, func(i int) bool { *work++; return x.ats[i] > v.hi })
	best := -1
	take := func(j int) {
		if best < 0 || j < best {
			best = j
		}
	}
	for lo, hi = lo+n, hi+n; lo < hi; lo, hi = lo/2, hi/2 {
		*work++
		if lo&1 == 1 {
			take(x.least[lo])
			lo++
		}
		if hi&1 == 1 {
			hi--
			take(x.least[hi])
		}
	}
	return best
}

// placed returns the intervals where an ending reaches c.at whatever
// c.at is, by where it is alone (see reaches): outside the deferred code
// being checked, in the assignment whose targets are, and in a switch
// whose late default, or a case it falls into, is, outside that case.
// It returns nil when none of them is being checked.
func (c *Checker) placed() []ival {
	var out []ival
	if c.deferSpan != nil {
		out = append(out, cut([]ival{every}, spanIval(*c.deferSpan))...)
	}
	if c.storing != nil {
		out = append(out, spanIval(*c.storing))
	}
	for _, d := range c.lateDefaults {
		out = append(out, cut([]ival{spanIval(d.whole)}, spanIval(d.body))...)
	}
	return out
}

// firstReach returns the drop of the first of es that reaches c.at, or
// nil when none does: the first that reaches it in the first of es's runs
// one does, every ending of a run coming before those of the next.
func (c *Checker) firstReach(es *endings) *drop {
	for run := es; run != nil; run = run.next {
		if i := c.firstInRun(run); i >= 0 {
			return run.list[i].drop
		}
	}
	return nil
}

// firstInRun returns the position in run's list of the first ending that
// reaches c.at, or -1 for none: by asking reaches of each in turn when
// there are few, and otherwise through their indexes, built the first
// time they are needed (see reach.go).
func (c *Checker) firstInRun(run *endings) int {
	if len(run.list) <= reachScanLimit && !reachOracle {
		return c.scanReach(run.list)
	}
	i := c.indexedReach(run)
	if reachOracle {
		if want := c.scanReach(run.list); i != want {
			panic(fmt.Sprintf("typeck: the reach index finds ending %d at %d, a scan finds %d", i, c.at, want))
		}
	}
	return i
}

// scanReach returns the position in list of the first ending that
// reaches c.at, asking reaches of each in turn, or -1 for none. Each
// ending asked about counts as work.
func (c *Checker) scanReach(list []ending) int {
	for i, e := range list {
		c.work.reached++
		if c.reaches(e) {
			return i
		}
	}
	return -1
}

// indexedReach returns what scanReach does, through the indexes of es,
// a run of endings, or of the list it is shared from: the earlier of the
// first ending reaching c.at by c.at alone, and the first lying where
// one reaches it by where it is, when deferred code, an assignment's
// targets or a late default is being checked.
func (c *Checker) indexedReach(es *endings) int {
	if es.shared != nil {
		return c.sharedReach(es.shared, es.from)
	}
	if es.byUse == nil {
		es.byUse = c.newReachIndex(es.list)
	}
	best := es.byUse.at(c.at, &c.work.reached)
	for _, v := range c.placed() {
		if es.byAt == nil {
			es.byAt = c.newAtIndex(es.list)
		}
		if i := es.byAt.first(v, &c.work.reached); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	return best
}

// sharedReach returns what indexedReach does for the endings of sh from
// the position from on, as a position among them: through sh's indexes,
// built the first time they are needed, the first ending at or after
// from that reaches c.at by c.at alone, or that lies where one reaches
// it by where it is.
func (c *Checker) sharedReach(sh *sharedEnds, from int) int {
	if sh.byUse == nil {
		sh.byUse = c.newSuffixReach(sh.list)
	}
	best := sh.byUse.at(c.at, from, &c.work.reached)
	for _, v := range c.placed() {
		if sh.byAt == nil {
			sh.byAt = c.newSuffixAt(sh.list)
		}
		if i := sh.byAt.first(v, from, &c.work.reached); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	if best < 0 {
		return -1
	}
	return best - from
}

// suffixReach answers which ending of a list, at or after some position
// in it, is the first to reach a position, when no deferred code,
// assignment's targets or late default is being checked: cuts are the
// positions, in increasing order, where the answer may change, as a
// reachIndex's are, and tree a segment tree over the pieces between
// them, tree[len(cuts)+j] piece j's leaf and tree[i] the parent of
// tree[2i] and tree[2i+1], each node holding, in increasing order, the
// position in the list of every ending one of whose intervals covers the
// pieces under it, and is put there, not in a node above or below.
type suffixReach struct {
	cuts []int
	tree [][]int
}

// newSuffixReach returns the suffixReach of list, from the reachSet of
// each of its endings, each interval put in the nodes that cover its
// pieces and no more, the fewest whose pieces make it up. Each ending,
// each interval and each node an interval is put in counts as work.
func (c *Checker) newSuffixReach(list []ending) *suffixReach {
	sets := make([][]ival, len(list))
	var cuts []int
	for i, e := range list {
		c.work.reached++
		sets[i] = c.reachSet(e)
		for _, v := range sets[i] {
			cuts = append(cuts, v.lo)
			if v.hi < math.MaxInt {
				cuts = append(cuts, v.hi+1)
			}
		}
	}
	slices.Sort(cuts)
	cuts = slices.Compact(cuts)
	n := len(cuts)
	tree := make([][]int, 2*n)
	for i, set := range sets {
		for _, v := range set {
			c.work.reached++
			from, _ := slices.BinarySearch(cuts, v.lo)
			to := n
			if v.hi < math.MaxInt {
				to, _ = slices.BinarySearch(cuts, v.hi+1)
			}
			for lo, hi := from+n, to+n; lo < hi; lo, hi = lo/2, hi/2 {
				c.work.reached++
				if lo&1 == 1 {
					tree[lo] = append(tree[lo], i)
					lo++
				}
				if hi&1 == 1 {
					hi--
					tree[hi] = append(tree[hi], i)
				}
			}
		}
	}
	return &suffixReach{cuts: cuts, tree: tree}
}

// at returns the position in the list of the first ending at or after
// from that reaches the position at, or -1 for none: the least, over the
// nodes from at's piece up to the root, of the first position at or
// after from each holds. Each step of its searches counts as work.
func (r *suffixReach) at(at, from int, work *int) int {
	n := len(r.cuts)
	j := sort.Search(n, func(i int) bool {
		*work++
		return r.cuts[i] > at
	})
	if j == 0 {
		return -1
	}
	best := -1
	for node := j - 1 + n; node >= 1; node /= 2 {
		*work++
		if i := firstFrom(r.tree[node], from, work); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	return best
}

// firstFrom returns the first of list, in increasing order, at or after
// from, or -1 for none. Each step of its search counts as work.
func firstFrom(list []int, from int, work *int) int {
	k := sort.Search(len(list), func(i int) bool {
		*work++
		return list[i] >= from
	})
	if k == len(list) {
		return -1
	}
	return list[k]
}

// suffixAt finds the first ending of a list, at or after some position
// in it, whose own position lies in an interval: ats is where each
// ending is, in increasing order, and tree a segment tree over them,
// tree[len(ats)+k] the leaf of the k-th, and tree[i] the parent of
// tree[2i] and tree[2i+1], each node holding the positions in the list
// of the endings under it, in increasing order.
type suffixAt struct {
	ats  []int
	tree [][]int
}

// newSuffixAt returns the suffixAt of list, each node's positions merged
// from its children's. Each ending, and each position merged into a
// node, counts as work.
func (c *Checker) newSuffixAt(list []ending) *suffixAt {
	n := len(list)
	order := make([]int, n)
	for i := range order {
		c.work.reached++
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(list[a].at, list[b].at) })
	ats := make([]int, n)
	tree := make([][]int, 2*n)
	for k, i := range order {
		ats[k] = list[i].at
		tree[n+k] = []int{i}
	}
	for i := n - 1; i > 0; i-- {
		a, b := tree[2*i], tree[2*i+1]
		merged := make([]int, 0, len(a)+len(b))
		for len(a) > 0 || len(b) > 0 {
			c.work.reached++
			if len(b) == 0 || len(a) > 0 && a[0] < b[0] {
				merged, a = append(merged, a[0]), a[1:]
			} else {
				merged, b = append(merged, b[0]), b[1:]
			}
		}
		tree[i] = merged
	}
	return &suffixAt{ats: ats, tree: tree}
}

// first returns the least position in the list, at or after from, of an
// ending whose own position lies in v, or -1 for none. Each step of its
// searches counts as work.
func (x *suffixAt) first(v ival, from int, work *int) int {
	if v.lo > v.hi {
		return -1
	}
	n := len(x.ats)
	lo := sort.Search(n, func(i int) bool { *work++; return x.ats[i] >= v.lo })
	hi := sort.Search(n, func(i int) bool { *work++; return x.ats[i] > v.hi })
	best := -1
	take := func(node int) {
		if i := firstFrom(x.tree[node], from, work); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	for lo, hi = lo+n, hi+n; lo < hi; lo, hi = lo/2, hi/2 {
		*work++
		if lo&1 == 1 {
			take(lo)
			lo++
		}
		if hi&1 == 1 {
			hi--
			take(hi)
		}
	}
	return best
}
