package typeck

import (
	"fmt"
	"iter"
	"slices"
	"sort"

	"github.com/tacklang/tack/ast"
)

// The region of the facts a statement gives the rest of its block (see
// checkStmts) is what an early exit falls through to first, if anything,
// then every statement after it in the block, and so is each region of
// an else if chain's links (see chainFacts). A block of n statements
// that each give facts has n such regions over the rest of it, so
// copying the rest into each, or walking it to index the writes of each
// (see assume), takes time that grows with n squared.
//
// So a region is a head, nodes of its own, then a rest: the statements
// of one block from some statement on (see blockRest), which every
// region over that block shares. The writes of a block's statements are
// found once, in one walk of all of them (see blockWrites), each kept
// with the position of the statement holding it, so the writes of a rest
// to one declaration are the end of that declaration's list, from the
// first a binary search finds held by a statement of the rest.
//
// What a walk of the block finds is what a walk of the region would. A
// region's nodes are each walked on their own, loops and leaving
// branches found within the node alone (see walkWrites), so a statement
// is walked the same in either. And all it finds is syntax: where each
// write is, the loops and leaving branches around it, and the
// declaration its root names, which the resolver binds and the checker
// unbinds only for a union pattern's name, never the root of a write.
// Whether a write writes that declaration also turns on types the
// checker may not have recorded yet (see throughList), and what a note
// on a call passing it as mut names, on the method the checker found
// (see factCallee). Those are asked when a region's writes are asked
// for, as indexWrites asks them when it walks a region, never when the
// block was walked (see regionIndex).
//
// An index that answered differently from a walk of the region could
// keep a fact in force past a write that ends it, so when the tests set
// regionOracle every query is checked against indexWrites over all of
// the region's nodes.

// regionOracle says to check every query of a region's writes against
// an index of a walk of all the region's nodes, panicking on a
// difference. It is set in a build with the tag tackfactoracle, and by
// the tests that compare the two, which set it only while no other test
// runs.
var regionOracle = factOracle

// region is where facts hold, as assume takes it: the nodes of head,
// then the statements of rest.
type region struct {
	head []ast.Node
	rest blockRest
}

// blockRest is the statements of the block blk from the one at from on.
// It is empty when blk is nil.
type blockRest struct {
	blk  *blockRegion
	from int
}

// blockRegion is the statements of one block as a region (see
// stmtNodes), and writes, every write in them by the declaration its
// root names, each declaration's in the order a walk of the statements
// meets them, found the first time a region over the block asks (see
// blockWrites). A block's checkStmts makes one, which every region over
// the rest of the block shares.
type blockRegion struct {
	nodes  []ast.Node
	writes map[ast.Decl][]blockWrite
}

// blockWrite is a write a walk of a block found, as indexNode would
// find it but for the checks asked only of a region's writes (see
// liveWrite), and node, the position in the block of the statement that
// holds it.
type blockWrite struct {
	write
	node int
}

// newBlockRegion returns the blockRegion of the statements of body, its
// writes not yet found. Each statement counts as copied (see proverWork).
func (c *Checker) newBlockRegion(body []ast.Stmt) *blockRegion {
	c.work.copied += len(body)
	return &blockRegion{nodes: stmtNodes(body)}
}

// restFrom returns the statements of b from the one at i on, empty when
// there are none.
func (b *blockRegion) restFrom(i int) blockRest {
	if i >= len(b.nodes) {
		return blockRest{}
	}
	return blockRest{blk: b, from: i}
}

// empty reports whether r holds no statement.
func (r blockRest) empty() bool {
	return r.blk == nil
}

// nodes returns the statements of r.
func (r blockRest) nodes() []ast.Node {
	if r.blk == nil {
		return nil
	}
	return r.blk.nodes[r.from:]
}

// regionOf returns the region of head, then rest. Each node of head
// counts as copied into a region (see proverWork); rest is shared, not
// copied.
func (c *Checker) regionOf(head []ast.Node, rest blockRest) region {
	c.work.copied += len(head)
	return region{head: head, rest: rest}
}

// nodes returns every node of r, head then rest, in a slice of their
// own, as a region was before a rest was shared, for the oracle to walk.
func (r region) nodes() []ast.Node {
	return append(slices.Clip(r.head), r.rest.nodes()...)
}

// blockWrites returns b's writes, by the declaration each one's root
// names, found in one walk of b's statements the first time it is asked
// and kept. A write's root is the name ast.Root gives of what it
// assigns, or of the argument it passes as mut, and only one bound to a
// declaration is kept, as writtenDecl keeps only those. Every mut
// argument of a call is kept, a call to swap's too, for liveWrite to
// judge when the writes are asked for.
func (c *Checker) blockWrites(b *blockRegion) map[ast.Decl][]blockWrite {
	if b.writes != nil {
		return b.writes
	}
	b.writes = make(map[ast.Decl][]blockWrite)
	add := func(i int, w write, root *ast.Ident) {
		if root != nil && root.Def != nil {
			b.writes[root.Def] = append(b.writes[root.Def], blockWrite{write: w, node: i})
		}
	}
	c.walkWrites(b.nodes, func(i int, n ast.Node, at write) {
		for _, target := range assignedTargets(n) {
			w := at
			w.shown = target
			add(i, w, ast.Root(target))
		}
		call, ok := n.(*ast.Call)
		if !ok {
			return
		}
		for _, arg := range call.Operands() {
			if m, ok := arg.(*ast.MutArg); ok {
				w := at
				w.shown, w.call = m, call
				add(i, w, ast.Root(m.X))
			}
		}
	})
	return b.writes
}

// liveWrite reports whether w, a write a walk of a block found for
// decl, writes decl, as indexNode would find it now: its target, or the
// argument it passes as mut, written as writtenDecl says, and not in a
// call to swap.
func (c *Checker) liveWrite(w write, decl ast.Decl) bool {
	if w.call != nil {
		return !isSwap(w.call) && c.writtenDecl(w.shown.(*ast.MutArg).X) == decl
	}
	return c.writtenDecl(w.shown.(ast.Expr)) == decl
}

// regionIndex holds every write in a region for one use, as a
// writeIndex does: head, the writeIndex of the region's head, from a
// walk of it, and rest, whose writes are found among the block's (see
// blockWrites) and checked as they are asked for (see restWrites). drops
// holds the drop made for each write of the rest found so far, so every
// fact that write ends shares one, as it would share the one a
// writeIndex holds. r is the region, for the oracle to walk.
type regionIndex struct {
	c     *Checker
	head  writeIndex
	rest  blockRest
	drops map[*blockWrite]*drop
	r     region
}

// indexRegion returns the regionIndex of r, walking r's head, but not
// its rest, whose block's writes are found once for every region over it.
func (c *Checker) indexRegion(r region) *regionIndex {
	ri := &regionIndex{c: c, rest: r.rest, r: r}
	if len(r.head) > 0 {
		ri.head = c.indexWrites(r.head)
	}
	return ri
}

// restWrites returns the writes ri's rest makes to decl, in the order a
// walk of it meets them: the end of the block's list for decl, from the
// first held by a statement of the rest, leaving out each that does not
// write decl now (see liveWrite), and, of a call's mut arguments passing
// decl, every one after the first that does, as indexNode leaves them
// out. A call's mut arguments are found together when the walk meets the
// call, so those passing decl are next to each other in the list.
func (ri *regionIndex) restWrites(decl ast.Decl) iter.Seq[*blockWrite] {
	return func(yield func(*blockWrite) bool) {
		if ri.rest.empty() {
			return
		}
		ws := ri.c.blockWrites(ri.rest.blk)[decl]
		from := sort.Search(len(ws), func(i int) bool { return ws[i].node >= ri.rest.from })
		var lastCall *ast.Call
		for i := from; i < len(ws); i++ {
			w := &ws[i]
			if !ri.c.liveWrite(w.write, decl) {
				continue
			}
			if w.call != nil {
				if w.call == lastCall {
					continue
				}
				lastCall = w.call
			}
			if !yield(w) {
				return
			}
		}
	}
}

// restDrop returns why w, a write of ri's rest to decl, ends a fact, as
// writeDrop says, made the first time it is asked for and kept.
func (ri *regionIndex) restDrop(w *blockWrite, decl ast.Decl) *drop {
	if d, ok := ri.drops[w]; ok {
		return d
	}
	if ri.drops == nil {
		ri.drops = make(map[*blockWrite]*drop)
	}
	d := ri.c.writeDrop(w.write, decl)
	ri.drops[w] = d
	return d
}

// endings returns what writeIndex.endings does for f over every node of
// ri's region, head then rest, checked against it when regionOracle is
// set.
func (ri *regionIndex) endings(f fact) *endings {
	got := ri.findEndings(f)
	if regionOracle {
		want := ri.c.indexWrites(ri.r.nodes()).endings(f)
		if why := differentEndings(got, want); why != "" {
			panic(fmt.Sprintf("typeck: a region's shared rest finds other writes than a walk of it: %s", why))
		}
	}
	return got
}

// findEndings is endings without the check: for each declaration f's
// keys name, once each, the writes to it in the head, then those in the
// rest, nil when there are none.
func (ri *regionIndex) findEndings(f fact) *endings {
	var ends []ending
	var buf [7]factKey
	var seenBuf [7]ast.Decl
	seen := seenBuf[:0]
	for _, k := range f.allKeys(&buf) {
		if k.isZero() || slices.Contains(seen, k.decl) {
			continue
		}
		seen = append(seen, k.decl)
		for _, w := range ri.head[k.decl] {
			ends = append(ends, endingOf(w))
		}
		for w := range ri.restWrites(k.decl) {
			e := w.write
			e.drop = ri.restDrop(w, k.decl)
			ends = append(ends, endingOf(e))
		}
	}
	if len(ends) == 0 {
		return nil
	}
	return &endings{list: ends}
}

// endingOf returns the ending of w, a write in a writeIndex, or of a
// region's rest with its drop set: its drop, the keeps holding it, the
// outermost loop holding it, or the zero span, and where it takes effect.
func endingOf(w write) ending {
	var loop ast.Span
	if len(w.loops) > 0 {
		loop = w.loops[0]
	}
	return ending{drop: w.drop, kept: w.kept, loop: loop, at: w.at}
}

// differentEndings returns how got and want, two fact's endings, differ,
// or "" when they hold the same endings in the same order: the same
// drop, the same loop, the same position, and keeps the same along the
// chain, each with the same skip.
func differentEndings(got, want *endings) string {
	var g, w []ending
	if got != nil {
		g = got.list
	}
	if want != nil {
		w = want.list
	}
	if (got == nil) != (want == nil) || len(g) != len(w) {
		return fmt.Sprintf("%d endings, a walk finds %d", len(g), len(w))
	}
	for i := range g {
		a, b := g[i], w[i]
		switch {
		case (a.drop == nil) != (b.drop == nil) || a.drop != nil && *a.drop != *b.drop:
			return fmt.Sprintf("ending %d is %v, a walk finds %v", i, a.drop, b.drop)
		case a.loop != b.loop || a.at != b.at:
			return fmt.Sprintf("ending %d is at %d in %v, a walk finds %d in %v", i, a.at, a.loop, b.at, b.loop)
		case !sameKeeps(a.kept, b.kept):
			return fmt.Sprintf("ending %d at %d is kept by other branches than a walk finds", i, a.at)
		}
	}
	return ""
}

// sameKeeps reports whether the chains of keeps a and b, innermost
// first, hold keeps alike in whole, end and resume, each with a skip
// alike in the same.
func sameKeeps(a, b *keep) bool {
	same := func(a, b *keep) bool {
		return a == nil && b == nil ||
			a != nil && b != nil && a.whole == b.whole && a.end == b.end && a.resume == b.resume
	}
	for ; a != nil || b != nil; a, b = a.parent, b.parent {
		if !same(a, b) || !same(a.skip, b.skip) {
			return false
		}
	}
	return true
}
