package typeck_test

import (
	"encoding/binary"
	"hash/fnv"
	"maps"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tacklang/tack/diag"
)

// The tests here hunt for an index or a contract the checker accepts
// although a write can reach it, among generated programs built from the
// shapes where a write in a branch that leaves is set aside (see keep in
// facts.go): branches ending in continue, break, return or exit at
// several depths, regions around such branches, whose bodies run once, in
// place, branches ending in further ifs and switches, loops
// inside such branches inside loops, a break and a continue in one
// branch, defers, exit in deferred code, chains of else ifs, switches on
// an integer, on a union with and without a default, on an index, and
// with no tag, cases with several values, indexes in conditions and in
// switch tags, conditions of cases that write, a default written before
// them that falls into later cases, cases ending in fallthrough, whose
// writes reach the code
// the cases they fall into reach, uses before and after writes, early
// exits inside loops
// that prove the index again, loops whose condition proves it again, and
// the two kinds of loop whose facts any write in them ends: a counting
// loop over len(a), whose variable a branch may set, and a range over a
// string a branch may assign. Some uses are written as a conversion
// instead of an index: uint64(i), which needs i's lower bound, and
// int8(q), for a uint64 q its type keeps at or above 0, which needs only
// the upper bound q <= 3 the early exits prove; and some writes set q to
// 200 rather than i to -1, so the same shapes check the facts a
// conversion is proven by, from below and from above (see lConv).
//
// The oracle runs each program on concrete values. a is [0, 1, 2, 3];
// i starts at 0, 1, 2 or 3, in range, and a write to it puts it out of
// range, at -1, where nothing puts it back, and so does a write to q,
// which starts at or below 3, at 200; s, a local string, starts as "abcd"
// and a write to it empties it; a counting loop's variable may be set to
// -5. The inputs take every combination of i's start, k in 0 to 3, so
// that a k some test of k == 0, 1 or 2 matches and one none matches are
// both tried, z false and true, and u p, q and r. A loop
// over 3 runs its passes as Tack does, a counting loop over len(a) until
// its condition fails, and a range over s over the length s had when the
// loop began; every run stops after a fixed number of steps, so a loop
// whose variable keeps being set back cannot run forever. The oracle
// records every check (an index, a return's contract, the end of the
// body's contract) it runs with its index out of range, or its contract
// false. An index out of range in a condition or a switch's tag ends the
// run there, as the program would. The checker must reject every check
// the oracle records. It may reject more: that is conservatism, not
// unsoundness; a run the step limit ends only records less.
//
// Each program is written out several ways, with newlines, with
// semicolons and braces packed against each other, and with a call
// through a function value first, which has the body checked twice (see
// checkBody). The checker must reject the same checks in all of them, so
// no verdict depends on where a brace goes or on the order checking runs
// in.

// leavingMaxDepth bounds how deeply the generator nests statements.
const leavingMaxDepth = 5

// leavingSteps bounds how many statements one run of a program runs.
const leavingSteps = 4000

// lKind is the kind of a generated statement.
type lKind int

// The kinds of generated statement.
const (
	lUse      lKind = iota // use(a[i]), useU(uint64(i)) or useI8(int8(q)) (see lConv), a check
	lUseN                  // use(a[n]), n a counting loop's variable over len(a), a check
	lUseS                  // use(s[n]), n a range over s's variable, a check
	lWrite                 // a write putting i out of range
	lWriteN                // n = -5, n a counting loop's variable over len(a)
	lWriteS                // s = "", emptying s
	lProve                 // if i < 0 || i >= len(a) || q > 3 { continue } or { exit(5) }
	lReturn                // return, a check of the contract
	lExit                  // exit(3)
	lBreak                 // break
	lContinue              // continue
	lDefer                 // defer { body }
	lIf                    // if c1 { } else if c2 { } ... else { }
	lSwitch                // switch, on k, u, a[i], or with no tag
	lLoop                  // a loop
	lRegion                // region _ { body }, which runs body once, in place
	lRaw                   // text written out as it is, which the oracle does not run
)

// lLoopKind is the kind of a generated loop. Each kind fixes both how the
// loop is written (see lText.loop) and what values its variable takes
// (see lRun.loop), and loopShapes says both, so the two cannot drift
// apart.
type lLoopKind int

// The kinds of generated loop.
const (
	loopRange    lLoopKind = iota // for range 3
	loopRangeVar                  // for n := range 3
	loopCount                     // for mut n := 0; n < 3; n++
	loopCountUse                  // for mut n := 0; n < 3; use(a[i]), stepping n first in its body
	loopGuard                     // for mut n := 0; 0 <= i && i < len(a) && n < 3; n++
	loopCountLen                  // for mut n := 0; n < len(a); n++, whose n a branch may set
	loopRangeS                    // for n := range s
	loopKinds
)

// loopShape is what a loop kind means: the header written before its
// body, with %v for the variable, %c for the post statement's check when
// it has one; whether the body starts by stepping the variable, so it
// sees the values from 1 on; whether it has a variable; whether it stops
// once i is out of range; and what the variable runs to: three, len(a),
// or the length s has when the loop begins.
type loopShape struct {
	header    string
	stepFirst bool
	hasVar    bool
	guard     bool
	bound     loopBound
}

// loopBound is what a loop's variable runs to.
type loopBound int

// The bounds of a loop's variable.
const (
	boundThree loopBound = iota
	boundLenA
	boundLenS
)

// loopShapes describes each loop kind (see loopShape).
var loopShapes = [loopKinds]loopShape{
	loopRange:    {header: "for range 3 ", bound: boundThree},
	loopRangeVar: {header: "for %v := range 3 ", hasVar: true, bound: boundThree},
	loopCount:    {header: "for mut %v := 0; %v < 3; %v++ ", hasVar: true, bound: boundThree},
	loopCountUse: {
		header: "for mut %v := 0; %v < 3; %c ",
		hasVar: true, stepFirst: true, bound: boundThree,
	},
	loopGuard: {
		header: "for mut %v := 0; 0 <= i && i < len(a) && %v < 3; %v++ ",
		hasVar: true, guard: true, bound: boundThree,
	},
	loopCountLen: {header: "for mut %v := 0; %v < len(a); %v++ ", hasVar: true, bound: boundLenA},
	loopRangeS:   {header: "for %v := range s ", hasVar: true, bound: boundLenS},
}

// lCondKind is the kind of a generated condition.
type lCondKind int

// The kinds of generated condition.
const (
	condZ     lCondKind = iota // z
	condNotZ                   // !z
	condK                      // k == c
	condVar                    // v == c
	condVarK                   // v == k
	condIndex                  // a[i] == c, a check
	condWrite                  // gb(mut i), which puts i out of range and is false
)

// lCond is a generated condition of kind kind about c, v, or, for
// condIndex, the check id.
type lCond struct {
	v    string
	kind lCondKind
	c    int
	id   int
}

// lTag is what a generated switch switches on.
type lTag int

// The tags of a generated switch.
const (
	tagK     lTag = iota // switch k
	tagU                 // switch u, the union
	tagIndex             // switch a[i], a check
	tagNone              // switch { case cond: }
)

// lCase is a case of a generated switch: its values, or its conditions
// for a switch with no tag, or the default when def; its body; and
// whether the body ends in a fallthrough into the next case, which body
// then ends with.
type lCase struct {
	vals  []string
	conds []lCond
	body  []lStmt
	def   bool
	falls bool
}

// lLink is one link of a chain of ifs: its condition and its body.
type lLink struct {
	body []lStmt
	cond lCond
}

// lStmt is a generated statement of kind kind. id numbers a check: a
// use, a return, a counting loop's post statement that indexes, a
// switch's tag that indexes. v names the loop variable a statement uses
// or sets, or the loop's own; write is a write's text, or raw text;
// links and els make a chain of ifs; tag, cases a switch; loop and body
// a loop, body a defer; inLoop says a proving early exit continues
// rather than exits.
type lStmt struct {
	write  string
	v      string
	links  []lLink
	els    []lStmt
	cases  []lCase
	body   []lStmt
	kind   lKind
	loop   lLoopKind
	tag    lTag
	id     int
	conv   lConv
	inLoop bool
	hasEls bool
	high   bool
}

// lConv is how a use is written: as an index, a[i], the default, or as
// a conversion, uint64(i), which needs only i >= 0, or int8(q), which
// needs only q <= 127, q being a uint64. A write is to i, setting it to
// -1, or, when high, to q, setting it to 200. The generator picks a use's
// form by its id, and a write's target by how many writes came before
// it, so neither takes a value from the generator's source, and every
// seed's program is the one it was apart from how its uses and writes
// are written; a program written by hand keeps indexes and writes to i.
type lConv int

// The ways a use is written.
const (
	convIndex lConv = iota // a[i]
	convU64                // uint64(i)
	convI8                 // int8(q)
)

// lScope is what a generated statement may refer to: the variables of
// the loops around it, those of counting loops over len(a) and of ranges
// over s among them, and whether it is in a loop or in deferred code.
type lScope struct {
	vars, lenVars, sVars []string
	inLoop, inDefer      bool
}

// lGen generates a program from rng, numbering its checks, until budget
// statements are spent.
type lGen struct {
	rng    *rand.Rand
	checks int
	budget int
	writes int
}

// check returns the number of a new check.
func (g *lGen) check() int {
	g.checks++
	return g.checks
}

// chance reports true with probability p.
func (g *lGen) chance(p float64) bool {
	return g.rng.Float64() < p
}

// block returns up to n statements at depth, ending perhaps in a way out.
func (g *lGen) block(depth int, sc lScope, n int) []lStmt {
	var out []lStmt
	for range n {
		if g.budget <= 0 {
			break
		}
		out = append(out, g.stmt(depth, sc))
	}
	if g.chance(0.4) && (sc.inLoop || !sc.inDefer || g.chance(0.3)) {
		out = append(out, g.leave(sc))
	}
	return out
}

// leave returns a way out of the code around it: a return, or an exit,
// the only way out of deferred code, and a break or a continue in a loop,
// or an if choosing between a break and a continue.
func (g *lGen) leave(sc lScope) lStmt {
	if sc.inLoop && g.chance(0.2) {
		brk, cont := []lStmt{{kind: lBreak}}, []lStmt{{kind: lContinue}}
		if g.chance(0.5) {
			brk, cont = cont, brk
		}
		return lStmt{
			kind:   lIf,
			links:  []lLink{{cond: g.cond(sc), body: brk}},
			els:    cont,
			hasEls: true,
		}
	}
	opts := []lKind{lExit}
	if !sc.inDefer {
		opts = append(opts, lReturn, lReturn)
	}
	if sc.inLoop {
		opts = append(opts, lBreak, lContinue, lBreak, lContinue)
	}
	k := opts[g.rng.IntN(len(opts))]
	if k == lReturn {
		return lStmt{kind: lReturn, id: g.check()}
	}
	return lStmt{kind: k}
}

// cond returns a condition on the inputs, on a loop variable, or on a[i].
func (g *lGen) cond(sc lScope) lCond {
	switch r := g.rng.IntN(10); {
	case r == 0:
		return lCond{kind: condZ}
	case r == 1:
		return lCond{kind: condNotZ}
	case r < 5:
		return lCond{kind: condK, c: g.rng.IntN(3)}
	case r == 5:
		return lCond{kind: condIndex, c: g.rng.IntN(3), id: g.check()}
	case len(sc.vars) == 0:
		return lCond{kind: condK, c: g.rng.IntN(3)}
	case r < 8:
		return lCond{kind: condVar, v: g.pickVar(sc.vars), c: g.rng.IntN(3)}
	default:
		return lCond{kind: condVarK, v: g.pickVar(sc.vars)}
	}
}

// pickVar returns one of vars, the innermost loop's more often.
func (g *lGen) pickVar(vars []string) string {
	if g.chance(0.5) {
		return vars[len(vars)-1]
	}
	return vars[g.rng.IntN(len(vars))]
}

// write returns a write: most often one putting i out of range, by
// assignment or by a call passing it as mut, or one setting a counting
// loop's variable or emptying s.
func (g *lGen) write(sc lScope) lStmt {
	var s lStmt
	switch r := g.rng.IntN(10); {
	case r < 4:
		s = lStmt{kind: lWrite, write: "i = -1"}
	case r < 7:
		s = lStmt{kind: lWrite, write: "g(mut i)"}
	case r < 9 && len(sc.lenVars) > 0:
		return lStmt{kind: lWriteN, v: g.pickVar(sc.lenVars)}
	case r == 9:
		return lStmt{kind: lWriteS}
	default:
		s = lStmt{kind: lWrite, write: "i = -1"}
	}
	g.writes++
	s.high = g.writes%2 == 0
	return s
}

// use returns a check of an index: a[i], or a[n] or s[n] for the
// variable of a loop that proves it.
func (g *lGen) use(sc lScope) lStmt {
	switch r := g.rng.IntN(6); {
	case r == 0 && len(sc.lenVars) > 0:
		return lStmt{kind: lUseN, v: g.pickVar(sc.lenVars), id: g.check()}
	case r == 1 && len(sc.sVars) > 0:
		return lStmt{kind: lUseS, v: g.pickVar(sc.sVars), id: g.check()}
	default:
		id := g.check()
		return lStmt{kind: lUse, id: id, conv: []lConv{convU64, convIndex, convI8, convIndex}[id%4]}
	}
}

// stmt returns one statement at depth.
func (g *lGen) stmt(depth int, sc lScope) lStmt {
	g.budget--
	r := g.pick(depth)
	switch {
	case r < 0.15:
		return g.use(sc)
	case r < 0.28:
		return g.write(sc)
	case r < 0.33:
		return lStmt{kind: lProve, inLoop: sc.inLoop}
	case r < 0.40:
		return g.leave(sc)
	case r < 0.58:
		return g.ifStmt(depth, sc)
	case r < 0.70:
		return g.switchStmt(depth, sc)
	case r < 0.77 && !sc.inDefer:
		inner := sc
		inner.inLoop, inner.inDefer = false, true
		return lStmt{kind: lDefer, body: g.block(depth+1, inner, 1+g.rng.IntN(2))}
	case r < 0.82:
		// A region's body is a block run once, in place, so a break,
		// continue or return in it acts as one in a branch does, and a
		// write in it reaches what one in a branch would.
		return lStmt{kind: lRegion, body: g.branch(depth, sc)}
	default:
		return g.loopStmt(depth, sc, 1+g.rng.IntN(3))
	}
}

// pick returns a number in [0, 1) that chooses a statement at depth,
// below 0.40, which chooses one that holds no others, once the depth or
// the budget runs out.
func (g *lGen) pick(depth int) float64 {
	r := g.rng.Float64()
	if depth >= leavingMaxDepth || g.budget <= 0 {
		r *= 0.40
	}
	return r
}

// ifStmt returns a chain of one to four ifs, with an else or not.
func (g *lGen) ifStmt(depth int, sc lScope) lStmt {
	s := lStmt{kind: lIf}
	n := 1 + g.rng.IntN(2)
	if g.chance(0.25) {
		n = 3 + g.rng.IntN(2)
	}
	for range n {
		s.links = append(s.links, lLink{cond: g.cond(sc), body: g.branch(depth, sc)})
	}
	if g.chance(0.5) {
		s.els, s.hasEls = g.branch(depth, sc), true
	}
	return s
}

// switchStmt returns a switch on k, on u, the union, on a[i], or with no
// tag, its cases holding one value or several. A switch on u names every
// case of U, or has a default; one naming every case may not have one.
func (g *lGen) switchStmt(depth int, sc lScope) lStmt {
	s := lStmt{kind: lSwitch, tag: lTag(g.rng.IntN(4))}
	if s.tag == tagIndex {
		s.id = g.check()
	}
	var def bool
	if s.tag == tagNone {
		s.cases, def = g.condCases(depth, sc)
	} else {
		s.cases, def = g.valueCases(depth, sc, s.tag == tagU)
	}
	if def {
		at := g.rng.IntN(len(s.cases) + 1)
		s.cases = slices.Insert(s.cases, at, lCase{def: true, body: g.branch(depth, sc)})
	}
	if s.tag == tagU {
		return s
	}
	for n := range s.cases[:len(s.cases)-1] {
		if g.chance(0.3) {
			c := &s.cases[n]
			c.falls = true
			c.body = append(c.body, lStmt{kind: lRaw, write: "fallthrough"})
		}
	}
	return s
}

// condCases returns the cases of a switch with no tag, each with one
// condition or two, and whether it gets a default. A condition is
// sometimes one that writes, gb(mut i), so a default written before it,
// and every case that default falls into, runs after the write.
func (g *lGen) condCases(depth int, sc lScope) ([]lCase, bool) {
	cases := make([]lCase, 0, 3)
	for range 1 + g.rng.IntN(3) {
		c := lCase{body: g.branch(depth, sc)}
		for range 1 + g.rng.IntN(2) {
			if g.chance(0.2) {
				c.conds = append(c.conds, lCond{kind: condWrite})
				continue
			}
			c.conds = append(c.conds, g.cond(sc))
		}
		cases = append(cases, c)
	}
	return cases, !g.chance(0.5)
}

// valueCases returns the cases of a switch on k or a[i], or on u when
// union, each with one value or two, and whether it gets a default: a
// switch on u gets one exactly when its cases leave a case of U unnamed.
func (g *lGen) valueCases(depth int, sc lScope, union bool) ([]lCase, bool) {
	vals := []string{"0", "1", "2"}
	if union {
		vals = []string{"p", "q", "r"}
	}
	g.rng.Shuffle(len(vals), func(i, j int) { vals[i], vals[j] = vals[j], vals[i] })
	var cases []lCase
	used := 0
	for used < len(vals) && (used == 0 || g.chance(0.6)) {
		n := 1
		if used+1 < len(vals) && g.chance(0.3) {
			n = 2
		}
		cases = append(cases, lCase{vals: vals[used : used+n], body: g.branch(depth, sc)})
		used += n
	}
	if union {
		return cases, used < len(vals)
	}
	return cases, g.chance(0.6)
}

// loopStmt returns a loop at depth with up to n statements in its body.
func (g *lGen) loopStmt(depth int, sc lScope, n int) lStmt {
	s := lStmt{
		kind: lLoop,
		loop: lLoopKind(g.rng.IntN(int(loopKinds))),
		v:    "n" + strconv.Itoa(depth),
	}
	shape := loopShapes[s.loop]
	inner := sc
	inner.inLoop = true
	if shape.hasVar {
		inner.vars = append(slices.Clip(sc.vars), s.v)
	}
	switch shape.bound {
	case boundLenA:
		inner.lenVars = append(slices.Clip(sc.lenVars), s.v)
	case boundLenS:
		inner.sVars = append(slices.Clip(sc.sVars), s.v)
	}
	if s.loop == loopCountUse {
		s.id = g.check()
	}
	s.body = g.block(depth+1, inner, n)
	return s
}

// branch returns the body of a branch or a case: a loop with perhaps a use
// after it, or a short block, then perhaps a write and a way out, or a
// further if or switch, so that branches that leave nest in branches
// that leave. Half the loops get a branch that writes and leaves put in
// their body, so that a branch that leaves its loop sits in a branch that
// leaves an outer one, with a use between the two.
func (g *lGen) branch(depth int, sc lScope) []lStmt {
	var b []lStmt
	if g.chance(0.3) && depth+1 < leavingMaxDepth {
		b = g.loopFirst(depth, sc)
	} else {
		b = g.block(depth+1, sc, g.rng.IntN(3))
	}
	switch r := g.rng.Float64(); {
	case r < 0.4:
		b = append(b, g.write(sc), g.leave(sc))
	case r < 0.55:
		b = append(b, g.leave(sc))
	case r < 0.8 && depth+1 < leavingMaxDepth && g.budget > 0:
		if g.chance(0.5) {
			b = append(b, g.write(sc))
		}
		g.budget--
		if g.chance(0.6) {
			b = append(b, g.ifStmt(depth+1, sc))
		} else {
			b = append(b, g.switchStmt(depth+1, sc))
		}
	}
	return b
}

// loopFirst returns the start of a branch at depth that is a loop, half
// the time with a branch that writes and leaves put in its body, and
// perhaps a use after it.
func (g *lGen) loopFirst(depth int, sc lScope) []lStmt {
	l := g.loopStmt(depth+1, sc, 1+g.rng.IntN(2))
	if g.chance(0.5) {
		in := sc
		in.inLoop = true
		inner := lStmt{
			kind:  lIf,
			links: []lLink{{cond: g.cond(sc), body: []lStmt{g.write(sc), g.leave(in)}}},
		}
		l.body = slices.Insert(l.body, g.rng.IntN(len(l.body)+1), inner)
	}
	b := []lStmt{l}
	if g.chance(0.6) {
		b = append(b, g.use(sc))
	}
	return b
}

// lFlow is how running statements ended: normally, by a break, a
// continue, a return, an exit, or by stopping the run, at an index out of
// range in a condition or a tag, or at the step limit.
type lFlow int

// The ways running statements end.
const (
	flowNormal lFlow = iota
	flowBreak
	flowContinue
	flowReturn
	flowExit
	flowStop
)

// lenA is the length of a in every run, whose elements are 0 to lenA-1.
const lenA = 4

// lRun runs a generated program on one combination of inputs, recording
// in bad every check that runs out of range.
type lRun struct {
	env   map[string]int
	bad   map[int]bool
	u     string
	k     int
	i     int // i's value while in range
	retID int
	steps int
	sLen  int
	iOK   bool
	qOK   bool
	z     bool
}

// stopped reports whether f ends the whole run, not just the statements
// it came from.
func stopped(f lFlow) bool {
	return f == flowExit || f == flowStop
}

// holds reports whether c holds now, and whether the run goes on: an
// index out of range in c stops it.
func (r *lRun) holds(c lCond) (holds, ok bool) {
	switch c.kind {
	case condZ:
		return r.z, true
	case condNotZ:
		return !r.z, true
	case condK:
		return r.k == c.c, true
	case condVar:
		return r.env[c.v] == c.c, true
	case condVarK:
		return r.env[c.v] == r.k, true
	case condWrite:
		r.iOK = false
		return false, true
	default:
		if !r.iOK {
			r.bad[c.id] = true
			return false, false
		}
		return r.i == c.c, true
	}
}

// block runs body as a block, then its defers, last first, unless it
// ended the whole run.
func (r *lRun) block(body []lStmt) lFlow {
	var defers [][]lStmt
	f := r.stmts(body, &defers)
	if stopped(f) {
		return f
	}
	for _, d := range slices.Backward(defers) {
		if df := r.block(d); stopped(df) {
			return df
		}
	}
	return f
}

// stmts runs body's statements in order, adding each defer it reaches to
// defers, until one ends other than normally.
func (r *lRun) stmts(body []lStmt, defers *[][]lStmt) lFlow {
	for _, s := range body {
		r.steps++
		if r.steps > leavingSteps {
			return flowStop
		}
		if f := r.stmt(s, defers); f != flowNormal {
			return f
		}
	}
	return flowNormal
}

// jumps maps each statement that only jumps to how it ends what runs.
var jumps = map[lKind]lFlow{lExit: flowExit, lBreak: flowBreak, lContinue: flowContinue}

// stmt runs s.
func (r *lRun) stmt(s lStmt, defers *[][]lStmt) lFlow {
	switch s.kind {
	case lUse, lUseN, lUseS:
		r.use(s)
	case lWrite, lWriteN, lWriteS:
		r.write(s)
	case lProve:
		return r.prove(s)
	case lReturn:
		r.retID = s.id
		return flowReturn
	case lExit, lBreak, lContinue:
		return jumps[s.kind]
	case lDefer:
		*defers = append(*defers, s.body)
	case lIf:
		return r.ifStmt(s)
	case lSwitch:
		return r.switchStmt(s)
	case lLoop:
		return r.loop(s)
	case lRegion:
		return r.block(s.body)
	}
	return flowNormal
}

// use records s, an index, as bad when its index is out of range.
func (r *lRun) use(s lStmt) {
	var ok bool
	switch s.kind {
	case lUse:
		ok = r.iOK
		if s.conv == convI8 {
			ok = r.qOK
		}
	case lUseN:
		ok = 0 <= r.env[s.v] && r.env[s.v] < lenA
	default:
		ok = 0 <= r.env[s.v] && r.env[s.v] < r.sLen
	}
	if !ok {
		r.bad[s.id] = true
	}
}

// write runs s, a write: i out of range, n set to -5, or s emptied.
func (r *lRun) write(s lStmt) {
	switch s.kind {
	case lWrite:
		if s.high {
			r.qOK = false
		} else {
			r.iOK = false
		}
	case lWriteN:
		r.env[s.v] = -5
	default:
		r.sLen = 0
	}
}

// prove runs s, an early exit unless i is in range: a continue in a loop,
// an exit outside one.
func (r *lRun) prove(s lStmt) lFlow {
	switch {
	case r.iOK && r.qOK:
		return flowNormal
	case s.inLoop:
		return flowContinue
	default:
		return flowExit
	}
}

// ifStmt runs the body of the first link of s, a chain of ifs, whose
// condition holds, or else its else.
func (r *lRun) ifStmt(s lStmt) lFlow {
	for _, l := range s.links {
		h, ok := r.holds(l.cond)
		if !ok {
			return flowStop
		}
		if h {
			return r.block(l.body)
		}
	}
	if s.hasEls {
		return r.block(s.els)
	}
	return flowNormal
}

// switchStmt runs the case of s that matches, trying its cases in order
// and the default last, as Tack does, and then, while the body run ends
// in a fallthrough, the body of the case written after it.
func (r *lRun) switchStmt(s lStmt) lFlow {
	tag, ok := r.switchTag(s)
	if !ok {
		return flowStop
	}
	for n, c := range s.cases {
		if c.def {
			continue
		}
		hit, ok := r.matches(c, tag)
		if !ok {
			return flowStop
		}
		if hit {
			return r.fall(s.cases[n:])
		}
	}
	for n, c := range s.cases {
		if c.def {
			return r.fall(s.cases[n:])
		}
	}
	return flowNormal
}

// fall runs the body of the first of cases, and of each after it while
// the one before ends normally, in its fallthrough.
func (r *lRun) fall(cases []lCase) lFlow {
	for _, c := range cases {
		if f := r.block(c.body); f != flowNormal || !c.falls {
			return f
		}
	}
	return flowNormal
}

// switchTag returns the value s, a switch, compares its cases with, and
// whether the run goes on: an index out of range in the tag stops it.
func (r *lRun) switchTag(s lStmt) (string, bool) {
	switch s.tag {
	case tagK:
		return strconv.Itoa(r.k), true
	case tagU:
		return r.u, true
	case tagIndex:
		if !r.iOK {
			r.bad[s.id] = true
			return "", false
		}
		return strconv.Itoa(r.i), true
	default:
		return "", true
	}
}

// matches reports whether c, a case that is not the default, matches
// tag, or, with no tag, has a condition that holds, tried in order, and
// whether the run goes on.
func (r *lRun) matches(c lCase, tag string) (hit, ok bool) {
	if slices.Contains(c.vals, tag) {
		return true, true
	}
	for _, cond := range c.conds {
		h, ok := r.holds(cond)
		if !ok || h {
			return h, ok
		}
	}
	return false, true
}

// loop runs s, a loop, as its shape says (see loopShape): its variable
// from 0 while it is under its bound, the body seeing it one higher when
// the body steps it first, and, for a loop indexing in its post
// statement, that index after every pass a break does not end.
func (r *lRun) loop(s lStmt) lFlow {
	shape := loopShapes[s.loop]
	limit := r.limit(shape)
	for n := 0; n < limit; n++ {
		if shape.guard && !r.iOK {
			break
		}
		r.env[s.v] = n + shape.seen()
		f := r.block(s.body)
		switch {
		case f == flowBreak:
			return flowNormal
		case f == flowReturn || stopped(f):
			return f
		}
		if s.id != 0 && !r.iOK {
			r.bad[s.id] = true
		}
		n = r.env[s.v] - shape.seen()
		if r.steps > leavingSteps {
			return flowStop
		}
	}
	return flowNormal
}

// limit returns what a loop of shape counts its variable up to, from
// when it begins.
func (r *lRun) limit(shape loopShape) int {
	switch shape.bound {
	case boundLenA:
		return lenA
	case boundLenS:
		return r.sLen
	default:
		return 3
	}
}

// seen returns how far above the count of passes so far the value of a
// loop of shape's variable is when its body sees it: 1 when the body
// steps it first, and 0 otherwise.
func (shape loopShape) seen() int {
	if shape.stepFirst {
		return 1
	}
	return 0
}

// lEnd is the check number of the contract at the end of the body.
const lEnd = 0

// leavingViolations returns every check of body that runs out of range,
// or with its contract false, on some combination of inputs.
func leavingViolations(body []lStmt) map[int]bool {
	bad := map[int]bool{}
	for i := range lenA {
		for k := range 4 {
			for _, z := range []bool{false, true} {
				for _, u := range []string{"p", "q", "r"} {
					r := &lRun{
						env:  map[string]int{},
						bad:  bad,
						i:    i,
						k:    k,
						z:    z,
						u:    u,
						sLen: 4,
						iOK:  true,
						qOK:  true,
					}
					switch r.block(body) {
					case flowReturn:
						if !r.iOK {
							bad[r.retID] = true
						}
					case flowNormal:
						if !r.iOK {
							bad[lEnd] = true
						}
					}
				}
			}
		}
	}
	return bad
}

// lStyle is how a program is written out: tight packs statements with
// semicolons and braces against each other, and bound adds a call
// through a function value first, so the body is checked twice.
type lStyle struct {
	tight, bound bool
}

// lText is a program written out, with the offset of each check in it.
type lText struct {
	at  map[int]int
	src strings.Builder
}

// render writes body out as a program in style. i comes in through a
// parameter, so the oracle's 0 and 1 are both values it may have.
func render(body []lStmt, style lStyle) *lText {
	t := &lText{at: map[int]int{}}
	t.src.WriteString("package main\n\ntype U union {\n    p\n    q\n    r\n}\n\n" +
		"func use(v int64) {\n}\n\nfunc useU(v uint64) {\n}\n\nfunc useI8(v int8) {\n}\n\n" +
		"func g(mut i int64) {\n    i = -1\n}\n\nfunc h(mut q uint64) {\n    q = 200\n}\n\n" +
		"func gb(mut i int64) bool {\n    i = -1\n    return false\n}\n\n" +
		"func two() (int64, int64) {\n    return 1, 2\n}\n\n" +
		"func f(mut i int64, mut q uint64, a []int64, k int64, z bool, u U) ensures 0 <= i && i < len(a) {\n")
	if style.bound {
		t.src.WriteString("    fv := two\n    x, y := fv()\n    use(x +% y)\n")
	}
	t.src.WriteString("    if i < 0 || i >= len(a) || q > 3 {\n        exit(1)\n    }\n" +
		"    mut s := \"abcd\"\n    use(len(s))\n    ")
	t.stmts(body, 1, style)
	if !style.tight {
		t.src.WriteString("\n")
	}
	t.at[lEnd] = t.src.Len()
	t.src.WriteString("}\n\nfunc main() {\n}\n")
	return t
}

// sep returns what goes between two statements at indent ind.
func sep(ind int, style lStyle) string {
	if style.tight {
		return ";"
	}
	return "\n" + strings.Repeat("    ", ind)
}

// stmts writes body's statements at indent ind.
func (t *lText) stmts(body []lStmt, ind int, style lStyle) {
	for n, s := range body {
		if n > 0 {
			t.src.WriteString(sep(ind, style))
		}
		t.stmt(s, ind, style)
	}
}

// block writes body as a block at indent ind, braces included; packed, the
// first statement comes right after the brace, the last right against
// the closing one.
func (t *lText) block(body []lStmt, ind int, style lStyle) {
	switch {
	case len(body) == 0:
		t.src.WriteString("{}")
	case style.tight:
		t.src.WriteString("{")
		t.stmts(body, ind+1, style)
		t.src.WriteString("}")
	default:
		t.src.WriteString("{" + sep(ind+1, style))
		t.stmts(body, ind+1, style)
		t.src.WriteString(sep(ind, style) + "}")
	}
}

// index writes the index check id of the slice or string named on: the
// index text, recording where it is.
func (t *lText) index(on, index string, id int) {
	t.src.WriteString(on + "[")
	t.at[id] = t.src.Len()
	t.src.WriteString(index + "]")
}

// cond writes c as Tack.
func (t *lText) cond(c lCond) {
	switch c.kind {
	case condZ:
		t.src.WriteString("z")
	case condNotZ:
		t.src.WriteString("!z")
	case condK:
		t.src.WriteString("k == " + strconv.Itoa(c.c))
	case condVar:
		t.src.WriteString(c.v + " == " + strconv.Itoa(c.c))
	case condVarK:
		t.src.WriteString(c.v + " == k")
	case condWrite:
		t.src.WriteString("gb(mut i)")
	default:
		t.index("a", "i", c.id)
		t.src.WriteString(" == " + strconv.Itoa(c.c))
	}
}

// jumpText is how each statement that only jumps is written.
var jumpText = map[lKind]string{lExit: "exit(3)", lBreak: "break", lContinue: "continue"}

// stmt writes s at indent ind.
func (t *lText) stmt(s lStmt, ind int, style lStyle) {
	switch s.kind {
	case lUse, lUseN, lUseS:
		t.use(s)
	case lWrite, lRaw, lWriteN, lWriteS:
		t.write(s)
	case lProve:
		t.prove(s, ind, style)
	case lReturn:
		t.at[s.id] = t.src.Len()
		t.src.WriteString("return")
	case lExit, lBreak, lContinue:
		t.src.WriteString(jumpText[s.kind])
	case lDefer:
		t.src.WriteString("defer ")
		t.block(s.body, ind, style)
	case lIf:
		t.ifStmt(s, ind, style)
	case lSwitch:
		t.switchStmt(s, ind, style)
	case lLoop:
		t.loop(s, ind, style)
	case lRegion:
		t.src.WriteString("region _ ")
		t.block(s.body, ind, style)
	}
}

// write writes s, a write or raw text.
func (t *lText) write(s lStmt) {
	switch s.kind {
	case lWriteN:
		t.src.WriteString(s.v + " = -5")
	case lWriteS:
		t.src.WriteString(`s = ""`)
	case lWrite:
		w := s.write
		if s.high {
			w = strings.NewReplacer("i = -1", "q = 200", "g(mut i)", "h(mut q)").Replace(w)
		}
		t.src.WriteString(w)
	default:
		t.src.WriteString(s.write)
	}
}

// use writes s, an index or a conversion of i (see lConv), as an
// argument of use, useU or useI8.
func (t *lText) use(s lStmt) {
	if s.kind == lUse && s.conv != convIndex {
		sink, conv := "useU(", "uint64(i))"
		if s.conv == convI8 {
			sink, conv = "useI8(", "int8(q))"
		}
		t.src.WriteString(sink)
		t.at[s.id] = t.src.Len()
		t.src.WriteString(conv)
		return
	}
	t.src.WriteString("use(")
	switch s.kind {
	case lUse:
		t.index("a", "i", s.id)
	case lUseN:
		t.index("a", s.v, s.id)
	default:
		t.index("s", s.v, s.id)
	}
	t.src.WriteString(")")
}

// prove writes s, an early exit unless i is in range.
func (t *lText) prove(s lStmt, ind int, style lStyle) {
	t.src.WriteString("if i < 0 || i >= len(a) || q > 3 ")
	out := lStmt{kind: lRaw, write: "exit(5)"}
	if s.inLoop {
		out = lStmt{kind: lContinue}
	}
	t.block([]lStmt{out}, ind, style)
}

// ifStmt writes s, a chain of ifs, at indent ind.
func (t *lText) ifStmt(s lStmt, ind int, style lStyle) {
	for n, l := range s.links {
		if n > 0 {
			t.src.WriteString(" else ")
		}
		t.src.WriteString("if ")
		t.cond(l.cond)
		t.src.WriteString(" ")
		t.block(l.body, ind, style)
	}
	if s.hasEls {
		t.src.WriteString(" else ")
		t.block(s.els, ind, style)
	}
}

// switchStmt writes s, a switch, at indent ind, each case on a line of
// its own.
func (t *lText) switchStmt(s lStmt, ind int, style lStyle) {
	switch s.tag {
	case tagK:
		t.src.WriteString("switch k {")
	case tagU:
		t.src.WriteString("switch u {")
	case tagIndex:
		t.src.WriteString("switch ")
		t.index("a", "i", s.id)
		t.src.WriteString(" {")
	default:
		t.src.WriteString("switch {")
	}
	for _, c := range s.cases {
		t.src.WriteString("\n" + strings.Repeat("    ", ind))
		t.caseHead(c)
		if len(c.body) > 0 {
			if style.tight {
				t.src.WriteString(" ")
			} else {
				t.src.WriteString(sep(ind+1, style))
			}
			t.stmts(c.body, ind+1, style)
		}
	}
	if style.tight {
		t.src.WriteString("}")
	} else {
		t.src.WriteString("\n" + strings.Repeat("    ", ind) + "}")
	}
}

// caseHead writes c's keyword and its values or conditions.
func (t *lText) caseHead(c lCase) {
	if c.def {
		t.src.WriteString("default:")
		return
	}
	t.src.WriteString("case ")
	if c.conds == nil {
		t.src.WriteString(strings.Join(c.vals, ", "))
	}
	for n, cond := range c.conds {
		if n > 0 {
			t.src.WriteString(", ")
		}
		t.cond(cond)
	}
	t.src.WriteString(":")
}

// loop writes s, a loop, at indent ind, as its shape says (see
// loopShape): a loop indexing in its post statement steps its variable
// first thing in its body, and a loop's variable that the body might
// leave unused is used first thing, so it never is.
func (t *lText) loop(s lStmt, ind int, style lStyle) {
	shape := loopShapes[s.loop]
	head := strings.ReplaceAll(shape.header, "%v", s.v)
	before, after, post := strings.Cut(head, "%c")
	t.src.WriteString(before)
	if post {
		t.src.WriteString("use(")
		t.index("a", "i", s.id)
		t.src.WriteString(")" + after)
	}
	var first []lStmt
	if shape.stepFirst {
		first = append(first, lStmt{kind: lRaw, write: s.v + "++"})
	}
	if shape.hasVar {
		first = append(first, lStmt{kind: lRaw, write: "use(" + s.v + ")"})
	}
	t.block(append(first, s.body...), ind, style)
}

// leavingStyles are the ways checkLeaving writes each program out.
var leavingStyles = []lStyle{{}, {tight: true}, {bound: true}, {tight: true, bound: true}}

// bytesSource is a rand.Source that reads its numbers from data, eight
// bytes at a time, and from a generator seeded by data once they run out,
// so every byte of a fuzz input steers the program it makes.
type bytesSource struct {
	rest rand.Source
	data []byte
}

// Uint64 returns the next number.
func (s *bytesSource) Uint64() uint64 {
	if len(s.data) == 0 {
		return s.rest.Uint64()
	}
	var b [8]byte
	n := copy(b[:], s.data)
	s.data = s.data[n:]
	return binary.LittleEndian.Uint64(b[:])
}

// leavingProgram generates the program rng gives: a chain (see chain)
// four times in ten, and otherwise a block of statements of any kind.
func leavingProgram(rng *rand.Rand) []lStmt {
	// Small programs as well as large ones: in a large one, some other
	// write usually reaches a check already, which hides whether the
	// write the test is about would.
	g := &lGen{rng: rng, budget: 6 + rng.IntN(40)}
	if g.chance(0.4) {
		var b []lStmt
		if g.chance(0.3) {
			b = append(b, g.use(lScope{}))
		}
		levels := 2 + g.rng.IntN(4)
		tail := g.chance(0.35)
		b = append(b, g.chain(0, lScope{}, levels, g.rng.IntN(levels), tail))
		for range g.rng.IntN(3) {
			b = append(b, g.use(lScope{}))
		}
		return b
	}
	return g.block(0, lScope{}, 1+g.rng.IntN(4))
}

// chain returns a statement nesting levels loops and branches that leave,
// or may not, in any order, with uses before and after each level and one
// write, at the level writeAt counts down to, before or after the levels
// inside it: the shapes whose verdict turns on how the branches around a
// write, and the loops their breaks and continues land in, combine (see
// keep). The write is the program's only one, so no other write hides
// what it reaches. A tail chain is ifs alone, each but the innermost the
// last statement of the branch around it, the write first in its level,
// and the branches leave only half the time: the shape in which whether a
// branch leaves turns on whether a further if, its last statement, does,
// which keepWalk remembers once found (see leavesBody).
func (g *lGen) chain(depth int, sc lScope, levels, writeAt int, tail bool) lStmt {
	inner, writeFirst := g.chainStart(sc, writeAt == 0, tail)
	write := lStmt{kind: lWrite, write: "i = -1"}
	s, in := g.chainLoop(depth, sc, tail)
	if levels > 1 {
		inner = append(inner, g.chain(depth+1, in, levels-1, writeAt-1, tail))
	}
	if writeAt == 0 && !writeFirst {
		inner = append(inner, write)
	}
	if !tail && g.chance(0.4) {
		inner = append(inner, g.use(in))
	}
	if s.kind == lLoop {
		s.body = inner
		return s
	}
	return g.chainIf(sc, inner, tail)
}

// chainStart returns what a level of a chain starts with: perhaps a use,
// unless in a tail chain, then the write, when the level holds it and it
// comes first, which it always does in a tail chain; and whether it did.
func (g *lGen) chainStart(sc lScope, writes, tail bool) ([]lStmt, bool) {
	var start []lStmt
	if !tail && g.chance(0.4) {
		start = append(start, g.use(sc))
	}
	first := writes && (tail || g.chance(0.5))
	if first {
		start = append(start, lStmt{kind: lWrite, write: "i = -1"})
	}
	return start, first
}

// chainLoop returns the loop a level of a chain at depth is, and the scope
// inside it, or, when the level is to be a branch, which a tail chain's
// always is, a statement that is no loop and sc itself.
func (g *lGen) chainLoop(depth int, sc lScope, tail bool) (lStmt, lScope) {
	if tail || !g.chance(0.45) {
		return lStmt{}, sc
	}
	s := lStmt{
		kind: lLoop,
		loop: lLoopKind(g.rng.IntN(int(loopKinds))),
		v:    "n" + strconv.Itoa(depth),
	}
	in := sc
	in.inLoop = true
	if loopShapes[s.loop].hasVar {
		in.vars = append(slices.Clip(sc.vars), s.v)
	}
	if s.loop == loopCountUse {
		s.id = g.check()
	}
	return s, in
}

// chainIf returns the if a level of a chain is, inner its branch, which
// leaves, and an else that leaves, each with a chance that is even in a
// tail chain.
func (g *lGen) chainIf(sc lScope, inner []lStmt, tail bool) lStmt {
	leave, els := 0.65, 0.3
	if tail {
		leave, els = 0.5, 0.5
	}
	if g.chance(leave) {
		inner = append(inner, g.leave(sc))
	}
	s := lStmt{kind: lIf, links: []lLink{{cond: g.cond(sc), body: inner}}}
	if g.chance(els) {
		s.els, s.hasEls = []lStmt{g.leave(sc)}, true
	}
	return s
}

// seedRand returns the generator seed gives.
func seedRand(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, 0x6c656176))
}

// checkLeaving checks body written out every way, and fails t when the
// checker accepts a check the oracle sees run out of range, reports an
// error at no check, or rejects different checks for different ways of
// writing it. It returns how many checks the program has and how many of
// them the checker accepted, the first way it is written.
func checkLeaving(t *testing.T, name string, body []lStmt) (checks, accepted int) {
	t.Helper()
	bad := leavingViolations(body)
	var first map[int]bool
	for _, style := range leavingStyles {
		text := render(body, style)
		src := text.src.String()
		_, diags := checkTree(t, src)
		rejected := rejectedChecks(t, text, diags, src)
		for id := range bad {
			if !rejected[id] {
				t.Fatalf("%s: check %d runs out of range but is accepted, written %+v:\n%s",
					name, id, style, src)
			}
		}
		if first == nil {
			first = rejected
			checks, accepted = len(text.at), len(text.at)-len(rejected)
			continue
		}
		if !maps.Equal(first, rejected) {
			t.Fatalf(
				"%s: written %+v, rejects checks %v, not %v:\n%s",
				name,
				style,
				slices.Sorted(maps.Keys(rejected)),
				slices.Sorted(maps.Keys(first)),
				src,
			)
		}
	}
	return checks, accepted
}

// counterStepError reports whether msg is the error for a loop counter's
// post, n0++ say, not proven not to overflow: a body that assigns its
// counter, as the generator's may, leaves the post no bound. It is about
// no check, and the checks are judged without it.
func counterStepError(msg string) bool {
	name, rest, ok := strings.Cut(msg, "++ may overflow ")
	if !ok || len(name) < 2 || name[0] != 'n' || rest == "" {
		return false
	}
	_, err := strconv.Atoi(name[1:])
	return err == nil
}

// rejectedChecks returns the checks of text that diags report an error
// at, failing t on an error at no check, or on any error but an index or
// a contract not proven, which a program the generator got wrong gives,
// or a loop counter's post not proven not to overflow (see
// counterStepError), which it skips.
func rejectedChecks(t *testing.T, text *lText, diags []diag.Diagnostic, src string) map[int]bool {
	t.Helper()
	byOffset := map[int]int{}
	for id, at := range text.at {
		byOffset[at] = id
	}
	rejected := map[int]bool{}
	for _, d := range diags {
		if counterStepError(d.Message) {
			continue
		}
		id, ok := byOffset[d.Span.Start]
		proof := strings.HasPrefix(d.Message, "index ") ||
			strings.HasPrefix(d.Message, "converting ") ||
			strings.HasPrefix(d.Message, "f ensures ")
		if !ok || !proof {
			t.Fatalf(
				"an error that is not a check's, %q at offset %d:\n%s",
				d.Message,
				d.Span.Start,
				src,
			)
		}
		rejected[id] = true
	}
	return rejected
}

// leavingSeeds is how many programs TestLeavingWritesAreSound checks,
// from the first seeds, and leavingRun how many of them one parallel
// subtest checks.
const (
	leavingSeeds = 1500
	leavingRun   = 50
)

// leavingCorpus holds seeds past leavingSeeds that a longer run found
// failing a checker made wrong on purpose, which the plain test checks
// too. A seed names a program only for this generator: when it changes,
// these have to be found again.
//
//   - 8136, 8574, 11628: written packed, the checker, popping a branch
//     only once a node starts past the branch's end plus one, rejects
//     fewer checks than written with newlines.
var leavingCorpus = []uint64{8136, 8574, 11628}

// TestLeavingWritesAreSound checks the programs of the first
// leavingSeeds seeds and of leavingCorpus (see checkLeaving), and that
// the checker accepts some of their checks, so the test does not pass by
// everything being rejected. A seed names a program only for this
// generator: changing it changes what every seed makes. The seeds are
// checked in parallel, in runs of leavingRun.
func TestLeavingWritesAreSound(t *testing.T) {
	var checks, accepted atomic.Int64
	t.Run("seeds", func(t *testing.T) {
		t.Run("corpus", func(t *testing.T) {
			t.Parallel()
			for _, seed := range leavingCorpus {
				c, a := checkLeaving(
					t,
					"seed "+strconv.FormatUint(seed, 10),
					leavingProgram(seedRand(seed)),
				)
				checks.Add(int64(c))
				accepted.Add(int64(a))
			}
		})
		for from := uint64(0); from < leavingSeeds; from += leavingRun {
			t.Run(strconv.FormatUint(from, 10), func(t *testing.T) {
				t.Parallel()
				for seed := from; seed < min(from+leavingRun, leavingSeeds); seed++ {
					c, a := checkLeaving(
						t,
						"seed "+strconv.FormatUint(seed, 10),
						leavingProgram(seedRand(seed)),
					)
					checks.Add(int64(c))
					accepted.Add(int64(a))
				}
			})
		}
	})
	if t.Failed() {
		return
	}
	all, ok := checks.Load(), accepted.Load()
	if ok == 0 || ok == all {
		t.Fatalf("accepted %d of %d checks; the programs prove nothing either way", ok, all)
	}
	t.Logf("accepted %d of %d checks", ok, all)
}

// FuzzLeavingWritesAreSound checks the program its input makes, every
// byte of it steering the generator (see bytesSource).
func FuzzLeavingWritesAreSound(f *testing.F) {
	for _, s := range []uint64{0, 1, 7, 42, 999} {
		f.Add(binary.LittleEndian.AppendUint64(nil, s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		h := fnv.New64a()
		h.Write(data)
		src := &bytesSource{data: data, rest: rand.NewPCG(h.Sum64(), 0x6c656176)}
		checkLeaving(t, "input", leavingProgram(rand.New(src)))
	})
}

// TestLeavingNestedKeepsAreSound checks (see checkLeaving) every program
// of one shape, too rare among random ones to be met reliably: a branch
// that writes and leaves, perhaps in a loop, inside a branch that leaves,
// perhaps in a loop, with one use, before the inner loop, after it, after
// the outer branch, or after everything. Each way out is tried at each
// level, as the loops around allow: break, continue, both, return and
// exit, and, for the outer branch only, none, since an inner branch that
// does not leave is not the shape. How the two branches' ways out
// combine, into what the
// write can reach, is what the checker's walk out from a write decides
// (see leftBefore).
func TestLeavingNestedKeepsAreSound(t *testing.T) {
	ways := func(inLoop, none bool) []lStmt {
		var w []lStmt
		if none {
			w = append(w, lStmt{kind: lRaw})
		}
		w = append(w, lStmt{kind: lReturn}, lStmt{kind: lExit})
		if inLoop {
			mixed := lStmt{kind: lIf, links: []lLink{{
				cond: lCond{kind: condK, c: 2},
				body: []lStmt{{kind: lBreak}},
			}}, els: []lStmt{{kind: lContinue}}, hasEls: true}
			w = append(w, lStmt{kind: lBreak}, lStmt{kind: lContinue}, mixed)
		}
		return w
	}
	programs := 0
	for _, outerLoop := range []bool{false, true} {
		for _, outer := range ways(outerLoop, true) {
			for _, innerLoop := range []bool{false, true} {
				for _, inner := range ways(outerLoop || innerLoop, false) {
					for use := range 4 {
						body := nestedKeeps(outerLoop, innerLoop, outer, inner, use)
						checkLeaving(t, "nested "+strconv.Itoa(programs), body)
						programs++
					}
				}
			}
		}
	}
	t.Logf("checked %d programs", programs)
}

// nestedKeeps returns the program TestLeavingNestedKeepsAreSound checks
// for its choices: the outer and inner branches' ways out, outer a lRaw
// for none; whether each is in a loop of its own; and where the one use
// is, 0 to 3.
func nestedKeeps(outerLoop, innerLoop bool, outer, inner lStmt, use int) []lStmt {
	checks := 0
	way := func(s lStmt) []lStmt {
		switch s.kind {
		case lRaw:
			return nil
		case lReturn:
			checks++
			s.id = checks
		}
		return []lStmt{s}
	}
	useAt := func(at int) []lStmt {
		if at != use {
			return nil
		}
		checks++
		return []lStmt{{kind: lUse, id: checks}}
	}
	innerIf := lStmt{kind: lIf, links: []lLink{{
		cond: lCond{kind: condZ},
		body: append([]lStmt{{kind: lWrite, write: "i = -1"}}, way(inner)...),
	}}}
	mid := []lStmt{innerIf}
	if innerLoop {
		mid = []lStmt{{kind: lLoop, loop: loopRange, v: "n1", body: mid}}
	}
	outerBody := slices.Concat(useAt(0), mid, useAt(1), way(outer))
	outerIf := lStmt{kind: lIf, links: []lLink{{cond: lCond{kind: condK, c: 1}, body: outerBody}}}
	top := slices.Concat([]lStmt{outerIf}, useAt(2))
	if outerLoop {
		top = []lStmt{{kind: lLoop, loop: loopRange, v: "n0", body: top}}
	}
	return slices.Concat(top, useAt(3))
}

// leavingOracleCase is a program TestLeavingOracle runs, with the checks
// that run out of range in it.
type leavingOracleCase struct {
	name string
	body []lStmt
	want []int
}

// The statements TestLeavingOracle's programs are built from: a use, a
// write, a return, an exit, a defer, an if, a loop over n, and n == c.
var (
	write = lStmt{kind: lWrite, write: "i = -1"}
	exit  = lStmt{kind: lExit}
)

// use returns use(a[i]), check id.
func use(id int) lStmt { return lStmt{kind: lUse, id: id} }

// ret returns a return, check id.
func ret(id int) lStmt { return lStmt{kind: lReturn, id: id} }

// deferred returns a defer of body.
func deferred(body ...lStmt) lStmt { return lStmt{kind: lDefer, body: body} }

// when returns an if on c around body.
func when(c lCond, body ...lStmt) lStmt {
	return lStmt{kind: lIf, links: []lLink{{cond: c, body: body}}}
}

// loop returns a loop of kind k over n around body, whose post statement
// is check id when it has one.
func loop(k lLoopKind, id int, body ...lStmt) lStmt {
	return lStmt{kind: lLoop, loop: k, v: "n", id: id, body: body}
}

// nIs returns n == c.
func nIs(c int) lCond { return lCond{kind: condVar, v: "n", c: c} }

// leavingOracleCases returns the programs TestLeavingOracle runs: those
// about how control flows, then those about the values a run takes.
func leavingOracleCases() []leavingOracleCase {
	return slices.Concat(leavingFlowCases(), leavingValueCases(), leavingDetailCases())
}

// leavingFlowCases returns the programs about defers and loops.
func leavingFlowCases() []leavingOracleCase {
	return []leavingOracleCase{
		{"a defer runs at the return", []lStmt{deferred(use(1)), write, ret(2)}, []int{1, 2}},
		{
			"every defer runs",
			[]lStmt{deferred(use(1)), deferred(use(2)), write, ret(3)},
			[]int{1, 2, 3},
		},
		{"exit runs no defer", []lStmt{deferred(use(1)), write, exit}, nil},
		{"exit in a defer ends the run", []lStmt{deferred(exit), write}, nil},
		{
			"a loop runs three passes",
			[]lStmt{loop(loopRangeVar, 0, when(nIs(2), use(1)), when(nIs(1), write))},
			[]int{1, lEnd},
		},
		{
			"a continue goes on to the next pass",
			[]lStmt{write, loop(loopRangeVar, 0, when(nIs(0), lStmt{kind: lContinue}), use(1))},
			[]int{1, lEnd},
		},
		{
			"a break ends the loop",
			[]lStmt{loop(loopRangeVar, 0, when(nIs(0), lStmt{kind: lBreak}), write), use(1), exit},
			nil,
		},
		{
			"a post statement indexes after each pass",
			[]lStmt{loop(loopCountUse, 1, write)},
			[]int{1, lEnd},
		},
		{
			"a body stepping first sees 1 to 3",
			[]lStmt{loop(loopCountUse, 0, when(nIs(0), write)), use(1), exit},
			nil,
		},
		{
			"a body stepping first sees 3 on its last pass",
			[]lStmt{loop(loopCountUse, 1,
				when(lCond{kind: condK, c: 2}, use(2)),
				when(lCond{kind: condVarK, v: "n"}, write))},
			[]int{1, 2, lEnd},
		},
		{
			"a guard loop stops once i is out of range",
			[]lStmt{loop(loopGuard, 0, use(1), write)},
			[]int{lEnd},
		},
	}
}

// leavingValueCases returns the programs about the inputs, a switch's
// default, a loop's variable and s, and an index in a condition.
func leavingValueCases() []leavingOracleCase {
	return []leavingOracleCase{
		{"every k", []lStmt{when(lCond{kind: condK, c: 1}, write, use(1), exit)}, []int{1}},
		{"every z", []lStmt{when(lCond{kind: condZ}, write, use(1), exit)}, []int{1}},
		{
			"every i",
			[]lStmt{when(lCond{kind: condIndex, c: 1, id: 2}, write, use(1), exit)},
			[]int{1},
		},
		{
			"every u, and a switch's default",
			[]lStmt{{kind: lSwitch, tag: tagU, cases: []lCase{
				{vals: []string{"p"}},
				{def: true, body: []lStmt{write, use(1), exit}},
			}}},
			[]int{1},
		},
		{
			"a counting loop's variable set back",
			[]lStmt{{kind: lLoop, loop: loopCountLen, v: "n", body: []lStmt{
				when(nIs(1), lStmt{kind: lWriteN, v: "n"}, lStmt{kind: lContinue}),
				{kind: lUseN, v: "n", id: 1},
			}}},
			[]int{1},
		},
		{
			"s emptied in a range over it",
			[]lStmt{{kind: lLoop, loop: loopRangeS, v: "n", body: []lStmt{
				{kind: lWriteS},
				{kind: lUseS, v: "n", id: 1},
			}}},
			[]int{1},
		},
		{
			"a range over s emptied before it runs no pass",
			[]lStmt{{kind: lWriteS}, {kind: lLoop, loop: loopRangeS, v: "n", body: []lStmt{
				{kind: lUseS, v: "n", id: 1},
			}}},
			nil,
		},
		{
			"an index out of range in a condition stops the run",
			[]lStmt{write, when(lCond{kind: condIndex, c: 0, id: 1}), use(2)},
			[]int{1},
		},
	}
}

// never is a condition no input meets: k is never 5.
var never = lCond{kind: condK, c: 5}

// leavingDetailCases returns the programs about the finer points of a
// run: how far loops and s go, which case and link run (see
// leavingChoiceCases), the order defers run in, a proving early exit in a
// loop, the step limit, and what an index out of range in a tag does.
func leavingDetailCases() []leavingOracleCase {
	return append(leavingLengthCases(), leavingChoiceCases()...)
}

// leavingLengthCases returns the programs about how far a loop over
// len(a) and a range over s go, and the edge of s[n].
func leavingLengthCases() []leavingOracleCase {
	countLen := func(body ...lStmt) lStmt {
		return lStmt{kind: lLoop, loop: loopCountLen, v: "n", body: body}
	}
	rangeS := func(body ...lStmt) lStmt {
		return lStmt{kind: lLoop, loop: loopRangeS, v: "n", body: body}
	}
	return []leavingOracleCase{
		{
			"a counting loop over len(a) runs four passes",
			[]lStmt{countLen(when(nIs(3), write)), use(1), exit},
			[]int{1},
		},
		{
			"a range over s keeps the length it began with",
			[]lStmt{rangeS(lStmt{kind: lWriteS}, when(nIs(1), write)), use(1), exit},
			[]int{1},
		},
		{"s starts four long", []lStmt{rangeS(when(nIs(3), write)), use(1), exit}, []int{1}},
		{
			"s[n] is out of range at its length",
			[]lStmt{rangeS(when(nIs(0), lStmt{kind: lWriteS}, lStmt{
				kind: lUseS, v: "n", id: 1,
			}))},
			[]int{1},
		},
	}
}

// leavingChoiceCases returns the programs about which case, value and
// link run, and every z, then those of leavingControlCases.
func leavingChoiceCases() []leavingOracleCase {
	return append([]leavingOracleCase{
		{
			"a default written first runs only when no case matches",
			[]lStmt{{kind: lSwitch, tag: tagK, cases: []lCase{
				{def: true},
				{vals: []string{"1"}, body: []lStmt{write}},
			}}, use(1), exit},
			[]int{1},
		},
		{
			"a case's second condition",
			[]lStmt{{kind: lSwitch, tag: tagNone, cases: []lCase{
				{conds: []lCond{never, {kind: condZ}}, body: []lStmt{write}},
			}}, use(1), exit},
			[]int{1},
		},
		{
			"an else if after the first link",
			[]lStmt{
				{
					kind:  lIf,
					links: []lLink{{cond: never}, {cond: lCond{kind: condZ}, body: []lStmt{write}}},
				},
				use(1),
				exit,
			},
			[]int{1},
		},
		{"every z, false too", []lStmt{when(lCond{kind: condNotZ}, write, use(1), exit)}, []int{1}},
		{
			"a case's second value",
			[]lStmt{{kind: lSwitch, tag: tagK, cases: []lCase{
				{vals: []string{"5", "1"}, body: []lStmt{write}},
			}}, use(1), exit},
			[]int{1},
		},
		{
			"an else",
			[]lStmt{
				{kind: lIf, links: []lLink{{cond: never}}, els: []lStmt{write}, hasEls: true},
				use(1),
				exit,
			},
			[]int{1},
		},
	}, leavingControlCases()...)
}

// leavingControlCases returns the programs about a guard loop, a return
// in a loop, defers, a proving early exit in a loop, a post statement
// after a continue, the step limit, and an index out of range in a tag.
func leavingControlCases() []leavingOracleCase {
	// Four loops of three passes nested, each with a variable of its own,
	// as the generator names them: 3 + 9 + 27 + 81 statements run.
	deep := use(2)
	for _, v := range []string{"n4", "n3", "n2", "n1"} {
		deep = lStmt{kind: lLoop, loop: loopRange, v: v, body: []lStmt{deep}}
	}
	return []leavingOracleCase{
		{
			"a guard loop runs no pass once i is out of range",
			[]lStmt{write, loop(loopGuard, 0, use(1))},
			[]int{lEnd},
		},
		{
			"a return in a loop ends the run",
			[]lStmt{loop(loopRangeVar, 0, when(nIs(0), write, ret(1))), use(2)},
			[]int{1},
		},
		{"defers run last first", []lStmt{deferred(use(1)), deferred(write)}, []int{1, lEnd}},
		{
			"a defer in a loop runs at a continue",
			[]lStmt{
				loop(
					loopRangeVar,
					0,
					deferred(use(1)),
					when(nIs(2), write, lStmt{kind: lContinue}),
				),
			},
			[]int{1, lEnd},
		},
		{
			"a proving early exit in a loop continues",
			[]lStmt{
				loop(
					loopRangeVar,
					0,
					when(nIs(0), write),
					lStmt{kind: lProve, inLoop: true},
					use(1),
				),
			},
			[]int{lEnd},
		},
		{
			"a post statement indexes after a continue",
			[]lStmt{loop(loopCountUse, 1, write, lStmt{kind: lContinue})},
			[]int{1, lEnd},
		},
		{"a run takes many steps before the limit", []lStmt{deep, write, use(1), exit}, []int{1}},
		{
			"an index out of range in a tag stops the run",
			[]lStmt{write, {kind: lSwitch, tag: tagIndex, id: 1, cases: []lCase{
				{vals: []string{"0"}, body: []lStmt{use(2)}},
			}}},
			[]int{1},
		},
	}
}

// TestLeavingOracle checks the oracle on programs whose checks out of
// range are known, at least one for each thing it does in running a
// program (see leavingFlowCases, leavingValueCases and
// leavingDetailCases), so that getting any of them wrong fails here, and
// not only, by chance, in a program the generator makes.
func TestLeavingOracle(t *testing.T) {
	for _, tc := range leavingOracleCases() {
		got := slices.Sorted(maps.Keys(leavingViolations(tc.body)))
		want := slices.Sorted(slices.Values(tc.want))
		if !slices.Equal(got, want) {
			t.Errorf("%s: checks out of range %v, want %v", tc.name, got, want)
		}
	}
}
