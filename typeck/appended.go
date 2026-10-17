package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/types"
)

// A successful append proves the list holds at least what it added:
// append adds all of its values or none, and when it adds none its error's
// code is not zero (see README, lists). So after
//
//	err := append(mut l, a, b)
//	if err.code != 0 {
//		return
//	}
//
// len(l) is at least 2, until a write to l can reach the code asking. The
// rule applies when the error is bound to an immutable variable declared
// by the call, so its code always tells of that call, and the list is a
// variable that can have facts (see stringVar). The count is the number
// of values passed, or the length of an array spread whole; a spread of
// anything else, whose length is not known, proves nothing. reserve adds
// no element, and proves nothing either.
//
// The proof does not come from a guard, so it has no region: a write to
// l ends it when it runs after the append and before the code asking,
// which the whole function's writes to l are searched for (see
// writtenSince).

// appendRecord is a call to append whose error was bound to err, an
// immutable variable, adding count values to the list list, which takes
// effect at at, the end of the call.
type appendRecord struct {
	err   *ast.Var
	list  factKey
	count int64
	at    int
}

// funcWrite is a write to a variable anywhere in the function being
// checked: where it takes effect, as an ending's at, and every loop that
// holds it.
type funcWrite struct {
	loops []ast.Span
	at    int
}

// recordAppend records v, a variable declared with the value of a call,
// when it is an immutable error bound to a call to append that adds a
// known number of values to a list that can have facts (see
// appendRecord).
func (c *Checker) recordAppend(v *ast.Var) {
	call, ok := v.Value.(*ast.Call)
	if !ok || v.Mutable || v.Type != types.Error || !isAppend(call) || len(call.Args) < 2 {
		return
	}
	m, ok := call.Args[0].(*ast.MutArg)
	if !ok {
		return
	}
	list, ok := c.stringVar(m.X)
	if count := c.appendCount(call); ok && count > 0 {
		r := appendRecord{err: v, list: list, count: count, at: call.Span.End}
		c.appends = append(c.appends, r)
	}
}

// appendCount returns how many values call, a call to append, adds when
// it succeeds, if that is known: the number of values passed, or the
// length of an array spread whole, and 0 for a spread of anything else.
func (c *Checker) appendCount(call *ast.Call) int64 {
	if !call.Spread {
		return int64(len(call.Args) - 1)
	}
	if typ := ast.TypeOf(call.Args[1]); c.table.IsArray(typ) {
		return c.table.Len(typ)
	}
	return 0
}

// appendedLen returns least, what the facts say of the length of the
// list k, narrowed by every append to it whose error the facts in force
// prove is no error, and that no write to it since can have undone (see
// writtenSince).
func (c *Checker) appendedLen(k factKey, least interval) interval {
	for _, r := range c.appends {
		if r.list != k || !c.noError(r.err) || c.writtenSince(r) {
			continue
		}
		least = least.narrow(fact{lo: constant.FromInt64(r.count), hasLo: true})
	}
	return least
}

// writtenSince reports whether a write to r's list can run after r's
// append and before c.at, the code asking, without the append running
// again: it takes effect after the append and before c.at; or it is in a
// loop holding c.at but not the append, whose next pass reaches c.at with
// no append in between; or c.at is in deferred code or a default written
// early, and the write is one that may run first (see reaches). The
// append's own write takes effect at its end, which is not after it.
func (c *Checker) writtenSince(r appendRecord) bool {
	for _, w := range c.funcWrites(r.list.decl) {
		if w.at <= r.at {
			continue
		}
		if c.reaches(ending{at: w.at}) {
			return true
		}
		for _, loop := range w.loops {
			if inSpan(c.at, loop) && !inSpan(r.at, loop) {
				return true
			}
		}
	}
	return false
}

// funcWrites returns every write to decl in the body of the function
// being checked (see eachWrite), found once per function and kept.
func (c *Checker) funcWrites(decl ast.Decl) []funcWrite {
	if ws, ok := c.writesByDecl[decl]; ok {
		return ws
	}
	var ws []funcWrite
	c.eachWrite(stmtNodes(c.body), decl, func(w write) {
		ws = append(ws, funcWrite{loops: w.loops, at: w.at})
	})
	c.writesByDecl[decl] = ws
	return ws
}
