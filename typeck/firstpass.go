package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// findCallees checks the body of fn once before checkBody checks it for
// good, to find what each call whose results a binding ties together
// calls: the method a method call's receiver selects, recorded as the
// call's Method, and the stand-in a call through a function value calls,
// recorded as its Dynamic (see dynamicFunc). collectHolds reads both.
//
// The first pass must leave no trace but what it exists to find. The
// second pass has to report exactly what checking the body once would,
// and whatever else the first pass changed that the second reads makes
// it see something a single pass never shows it. A name the first pass
// reported and marked as reported, its report then dropped, would be
// reported by neither, and an accepted program would break the rule the
// report enforces. So what the first pass reports is dropped, and every
// record of what checking has reported or found is handed to it fresh
// and put back after it (see bodyRecords).
//
// What the first pass is left to change, the second either needs or
// does again from the start:
//   - each call's Method and Dynamic, and the stand-ins of standIns and
//     dynamics they name, which are what it exists to find;
//   - the types it records on the tree, which the second pass records
//     again as it checks each node, and the types of c.table, which
//     depend on nothing checking reports. A search of the writes ahead
//     of where the second pass has reached reads a type the first pass
//     recorded there before the second records it (see throughList),
//     which a single pass finds unset and so takes as any write. A type
//     is the same in both passes, so the search sees what it would see
//     once checking got there: a write through a list's element, which
//     changes no variable a fact can be about;
//   - holding, which collectHolds makes anew for the second pass;
//   - defers, each function's defers, found from its syntax alone and
//     the same whichever pass finds them;
//   - factGen, which only grows, so that no index of relations built in
//     the first pass, relIdx, is taken as current in the second, and
//     factsBy, the index of the facts in force, which is kept in step
//     with them and so is as current after the pass as before it;
//   - work, the provers' steps, which the first pass did take;
//   - the state of the check in progress, the facts in force and the
//     guards among them, the loops and defers around the statement, dead
//     blocks, c.at and the rest, which checking a block leaves as it
//     found it.
//
// Every other part of the Checker is settled before any body is checked:
// declarations, signatures, struct fields, union cases, globals and the
// contracts of ensures clauses.
func (c *Checker) findCallees(fn *ast.Func) {
	c.discardedPass(fn)
}

// firstPasses checks the body of fn first, before checkBody checks it
// for good, as many times as it needs: once to find what each call whose
// results a binding ties together calls, when a binding does (see
// findCallees), and then once as a probe, when a loop writes a variable
// declared before it (see findInvariants), so the probe sees the callees
// the first found.
func (c *Checker) firstPasses(fn *ast.Func) {
	if c.everyBodyTwice || hasMethodBinding(fn) {
		c.findCallees(fn)
	}
	if !c.neverProbe && (c.probeEveryBody || hasCounterLoop(fn)) {
		c.findInvariants(fn)
	}
}

// findInvariants checks the body of fn once as a probe, before checkBody
// checks it for good, to find the bounds its loops keep (see
// invariant.go): each loop's, recorded in c.invariants, is what the probe
// exists to find, and what the check after it puts in force, as findCallees'
// pass leaves the callees it finds. The probe is a discarded pass like
// findCallees', and leaves no other trace, by the same argument; the
// probe's own records, invFrames and starts, are empty once it has checked
// the body, since every loop pops its frame and every block forgets the
// known values it remembered. A body findCallees checks first is probed
// after it, so the probe sees every call's callee as the check for good
// does.
//
// Trusting what the probe found is sound for a reason that does not
// depend on what the check after it proves. Each fact the probe uses at a
// loop's start and at each write in the loop is in force there under the
// same rules as in any single check, so it is true of every run of the
// program at that point, whichever pass worked it out. The probe has no
// fact the check for good lacks: it has every fact of a single check of
// the body, since nothing it proves puts a fact in force, and none of the
// bounds a loop keeps, which only add facts. So a bound the probe found a
// loop keeps holds on entry and through every write on every run (see
// invariant.go), and the check after it, which sees the same program,
// cannot find otherwise; and when the program has an error, it is
// rejected whatever the bound says.
func (c *Checker) findInvariants(fn *ast.Func) {
	c.probing = true
	c.discardedPass(fn)
	c.probing = false
}

// discardedPass checks the body of fn with what it reports dropped and
// with records of its own, put back as they were after it (see
// findCallees).
func (c *Checker) discardedPass(fn *ast.Func) {
	before, warned := len(c.diags), len(c.warnings)
	outer := c.swapBodyRecords(newBodyRecords())
	c.collectHolds(fn)
	c.checkBlock(fn.Body, fn)
	c.swapBodyRecords(outer)
	c.diags, c.warnings = c.diags[:before], c.warnings[:warned]
}

// bodyRecords are what the Checker records as it checks a body: what it
// has reported, which it does not report again, and what it has found,
// which it does not work out again. findCallees hands the first pass its
// own, so that none of it reaches the second pass.
type bodyRecords struct {
	// ownReported and sizeChecked say what has been reported: a name
	// passed alongside an own of it, and a type checked for its size,
	// with the site it was checked at.
	ownReported map[ast.Span]bool
	sizeChecked map[types.Type]ast.Span

	// knownVars, knownBinaries, tagWrites and writesByDecl remember what
	// working something out found.
	knownVars     map[*ast.Var]knownResult
	knownBinaries map[*ast.Binary]knownBinary
	tagWrites     map[*ast.Switch]tagWrite
	writesByDecl  map[ast.Decl][]funcWrite

	// holders, unionBindings, rangeBindings and rangeKeys say what the
	// body's variables hold or refer to in place.
	holders       map[*ast.Var]string
	unionBindings map[*ast.Var]ast.Expr
	rangeBindings map[*ast.Var]ast.Expr
	rangeKeys     map[*ast.Var]bool

	// appends holds the calls to append checked so far whose error
	// proves what they added.
	appends []appendRecord
}

// newBodyRecords returns records that hold nothing.
func newBodyRecords() bodyRecords {
	return bodyRecords{
		ownReported:   make(map[ast.Span]bool),
		sizeChecked:   make(map[types.Type]ast.Span),
		knownVars:     make(map[*ast.Var]knownResult),
		knownBinaries: make(map[*ast.Binary]knownBinary),
		tagWrites:     make(map[*ast.Switch]tagWrite),
		writesByDecl:  make(map[ast.Decl][]funcWrite),
		holders:       make(map[*ast.Var]string),
		unionBindings: make(map[*ast.Var]ast.Expr),
		rangeBindings: make(map[*ast.Var]ast.Expr),
		rangeKeys:     make(map[*ast.Var]bool),
	}
}

// swapBodyRecords makes r the Checker's records and returns the ones it
// replaced.
func (c *Checker) swapBodyRecords(r bodyRecords) bodyRecords {
	old := bodyRecords{
		ownReported:   c.ownReported,
		sizeChecked:   c.sizeChecked,
		knownVars:     c.knownVars,
		knownBinaries: c.knownBinaries,
		tagWrites:     c.tagWrites,
		writesByDecl:  c.writesByDecl,
		appends:       c.appends,
		holders:       c.holders,
		unionBindings: c.unionBindings,
		rangeBindings: c.rangeBindings,
		rangeKeys:     c.rangeKeys,
	}
	c.ownReported, c.sizeChecked = r.ownReported, r.sizeChecked
	c.knownVars, c.knownBinaries = r.knownVars, r.knownBinaries
	c.tagWrites, c.writesByDecl, c.appends = r.tagWrites, r.writesByDecl, r.appends
	c.holders = r.holders
	c.unionBindings, c.rangeBindings, c.rangeKeys = r.unionBindings, r.rangeBindings, r.rangeKeys
	return old
}
