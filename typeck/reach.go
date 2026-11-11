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
// writeIndex.endings gives them, with the indexes of them a query builds
// the first time it needs one: byUse, for the positions each ending
// reaches, and byAt, for where each ending is. A fact copied shares its
// endings, and with them their indexes, since the endings never change.
type endings struct {
	list  []ending
	byUse *reachIndex
	byAt  *atIndex
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
// nil when none does: by asking reaches of each in turn when there are
// few, and otherwise through their indexes, built the first time they
// are needed (see reach.go).
func (c *Checker) firstReach(es *endings) *drop {
	if es == nil {
		return nil
	}
	var i int
	if len(es.list) <= reachScanLimit && !reachOracle {
		i = c.scanReach(es.list)
	} else {
		i = c.indexedReach(es)
		if reachOracle {
			if want := c.scanReach(es.list); i != want {
				panic(fmt.Sprintf("typeck: the reach index finds ending %d at %d, a scan finds %d", i, c.at, want))
			}
		}
	}
	if i < 0 {
		return nil
	}
	return es.list[i].drop
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

// indexedReach returns what scanReach does, through the indexes of es:
// the earlier of the first ending reaching c.at by c.at alone, and the
// first lying where one reaches it by where it is, when deferred code,
// an assignment's targets or a late default is being checked.
func (c *Checker) indexedReach(es *endings) int {
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
