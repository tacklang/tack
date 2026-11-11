package typeck

import (
	"fmt"

	"github.com/tacklang/tack/ast"
)

// Whether a block leaves (see leaves) and whether a statement terminates
// (see terminates) are syntactic, but an else if is the last statement
// of the else of the link before it, so asking either of a link walks
// every link after it. The checker asks at every link of a chain, so
// asking afresh would take time that grows with the square of the
// chain. The checker remembers the answer
// for each if, switch, fallthrough and loop it works one out for, so each
// is worked out once and a chain of n links takes n steps in all.
//
// Both answers read one thing besides the syntax: Switch.Exhaustive,
// which checkCoverage sets on a switch on a union, once it is checked,
// when its clauses name every case. It is only ever set, never cleared,
// and setting it only ever turns a no into a yes, for leaves and
// terminates alike, since each is an and of the answers inside it. So a
// yes, once worked out, holds for good, and a no holds until the next
// switch is found to name every case, which exhaustGen counts: a no
// worked out at an earlier count is worked out again.
//
// An answer remembered that differed from one worked out afresh could
// count a branch as leaving that falls out, so when the tests set
// leaveOracle every answer is checked against leaves or terminates.

// leaveOracle says to check every answer the checker remembers for
// whether a block leaves or a statement terminates against one worked
// out afresh, panicking on a difference. It is set in a build with the
// tag tackfactoracle, and by the tests that compare the two, which set
// it only while no other test runs.
var leaveOracle = factOracle

// stmtAnswer is a remembered answer for a statement, yes or no, and the
// exhaustGen it was worked out at, which a no holds only while it stays
// the same.
type stmtAnswer struct {
	yes bool
	gen int
}

// holds reports whether a, remembered, still holds while exhaustGen is
// gen: a yes always does, and a no only when worked out at gen.
func (a stmtAnswer) holds(gen int) bool {
	return a.yes || a.gen == gen
}

// leaves reports what leaves does for body, remembering the answer for
// a body ending in an if, a switch or a fallthrough, the only ones that
// ask about other blocks.
func (c *Checker) leaves(body []ast.Stmt) bool {
	v := c.leavesRemembered(body)
	if leaveOracle && v != leaves(body) {
		panic(fmt.Sprintf("remembered that a block ending at %v leaves: %v, afresh: %v",
			body[len(body)-1].Spanned(), v, !v))
	}
	return v
}

// leavesRemembered is leaves without the check of its answer, each
// block inside body asked about through it in turn. Each answer worked
// out counts as work (see proverWork).
func (c *Checker) leavesRemembered(body []ast.Stmt) bool {
	if len(body) == 0 {
		return false
	}
	last := body[len(body)-1]
	switch last.(type) {
	case *ast.If, *ast.Switch, *ast.Fallthrough:
	default:
		return leaves(body)
	}
	if a, ok := c.leftBy[last]; ok && a.holds(c.exhaustGen) {
		return a.yes
	}
	c.work.decided++
	v := leavesBy(body, c.leavesRemembered)
	c.leftBy[last] = stmtAnswer{yes: v, gen: c.exhaustGen}
	return v
}

// terminates reports what terminates does for stmt, remembering the
// answer for an if, a switch or a loop, the only ones that ask about
// other statements.
func (c *Checker) terminates(stmt ast.Stmt) bool {
	v := c.terminatesRemembered(stmt)
	if leaveOracle && v != terminates(stmt) {
		panic(fmt.Sprintf("remembered that the statement at %v terminates: %v, afresh: %v",
			stmt.Spanned(), v, !v))
	}
	return v
}

// terminatesRemembered is terminates without the check of its answer,
// each statement inside stmt asked about through it in turn. Each answer
// worked out counts as work (see proverWork).
func (c *Checker) terminatesRemembered(stmt ast.Stmt) bool {
	switch stmt.(type) {
	case *ast.If, *ast.Switch, *ast.For:
	default:
		return terminates(stmt)
	}
	if a, ok := c.endedBy[stmt]; ok && a.holds(c.exhaustGen) {
		return a.yes
	}
	c.work.decided++
	v := terminatesBy(stmt, c.blockTerminatesRemembered)
	c.endedBy[stmt] = stmtAnswer{yes: v, gen: c.exhaustGen}
	return v
}

// blockTerminatesRemembered is blockTerminates, asking about body's last
// statement through terminatesRemembered.
func (c *Checker) blockTerminatesRemembered(body []ast.Stmt) bool {
	return len(body) > 0 && c.terminatesRemembered(body[len(body)-1])
}

// markExhaustive records on s, a switch on a union with no default,
// that its clauses name every case (see Switch.Exhaustive), and, the
// first time, counts it in exhaustGen, so no answer of leaves or
// terminates remembered before it is trusted.
func (c *Checker) markExhaustive(s *ast.Switch) {
	if !s.Exhaustive {
		s.Exhaustive = true
		c.exhaustGen++
	}
}
