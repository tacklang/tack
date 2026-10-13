package typeck

import "github.com/tacklang/tack/ast"

// checkDefer checks what a defer defers, as a block of its own, where the
// defer is written. No return may appear anywhere in it, and no break or
// continue aimed at a loop outside it: control never leaves deferred code
// except by reaching its end. A loop inside it may break and continue as
// usual. No defer may appear anywhere in it either: each outermost one
// that does is reported at its keyword, and its block is still checked, as
// deferred code, for errors of its own, except that a defer inside it, at
// any depth, is not reported again.
//
// It is checked with the facts in force at the defer, though it runs
// later, when the defer's block is left. Each of those facts holds there
// too. A fact is in force for a region, a whole block or the rest of one,
// only when nothing in the region assigns its variable, and the region
// holding the defer holds the whole rest of the defer's block: every path
// out of the block, every other defer that runs before this one, and this
// defer's own statements, since ast.Walk visits them. Nothing outside the
// region can change a variable a fact may be about, a parameter or local,
// or an immutable global. So a fact true at the defer is still true when
// the deferred code runs.
func (c *Checker) checkDefer(d *ast.Defer, fn *ast.Func) {
	outerRejected := c.inRejectedDefer
	if c.deferred > 0 && !c.inRejectedDefer {
		keyword := d.Span
		keyword.End = keyword.Start + len("defer")
		c.errorf(keyword, "cannot defer inside a defer")
		c.inRejectedDefer = true
	}
	outerLoops := c.deferLoops
	c.deferred++
	c.deferLoops = c.loops
	c.checkBlock(d.Body, fn)
	c.deferred--
	c.deferLoops = outerLoops
	c.inRejectedDefer = outerRejected
}
