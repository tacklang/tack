package typeck_test

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
)

// The test here hunts for an index the checker accepts although a
// closure can put it out of range, among generated programs of one
// mutable index i into a = [0, 1, 2, 3], an immutable one k, start - 1,
// out of range for start 0 unless a guard keeps it out of use, whose
// facts from outside a closure hold in it, uses of a[i], writes putting i
// out of range or back in, guards proving it in range, early returns,
// loops, and closures, nested to any depth, passed to run, which calls
// its scope parameter zero, one or two times, to down, which passes its
// own scope parameter on to run, and two at a time to run2, which calls
// both in turn, the second naming only k, since it cannot reach an i the
// first may write. The shapes are those where
// a closure's runs, coming later than where it is written and repeating,
// change what holds: a closure that writes i and a use after its call
// that relies on a fact from before it, which the write ends; a closure
// whose body relies on a fact from outside it, or from earlier in itself,
// that its own write, on an earlier run, ends; a closure that writes i in
// a branch ending in a return, which leaves only the closure; and the
// same inside loops and inside other closures.
//
// The oracle runs each program for every start of i in 0 to 3, a run of
// a closure running its body as many times as run's count says, a return
// in it leaving that run of the closure alone, and records every use it
// runs with i out of range. The checker must reject every use the oracle
// records, with an index error at it, and report no other error. It may
// reject more.

// cKind is the kind of a generated statement.
type cKind int

// The kinds of generated statement.
const (
	cUse    cKind = iota // if a[i] == 9 { print("x") }, a check
	cOut                 // i = -1
	cIn                  // i = 2
	cGuard               // if 0 <= i && i < 4 { body }
	cLeave               // if i < 0 || i >= 4 { return }
	cWhen                // if i == 0 { body }, which may hold a return
	cReturn              // return
	cLoop                // for range 2 { body }
	cRun                 // run(n, func() { body }), or down(n, ...), which passes its scope parameter on to run
	cUseK                // if a[k] == 9 { print("x") }, a check of the immutable k
	cGuardK              // if 0 <= k && k < 4 { body }
	cLeaveK              // if k < 0 || k >= 4 { return }
	cRun2                // run2(n, func() { body }, func() { other }), other naming no i
	cKinds
)

// cStmt is a generated statement: its kind, its check's number for a
// use, its count for a run, whether a run goes down a level of scope
// parameters, and the statements it holds, the second closure's for a
// cRun2 in other.
type cStmt struct {
	kind  cKind
	id    int
	n     int
	down  bool
	body  []cStmt
	other []cStmt
}

// cGen generates programs, numbering their checks. noI, while set, keeps
// i out of what it generates: the second closure of a cRun2 cannot
// capture i, which the first may write, since two closures of one call
// cannot both reach a variable one of them writes.
type cGen struct {
	rng *rand.Rand
	ids int
	noI bool
}

// block returns up to n statements nested at most depth deep, ending in a
// return now and then, inClosure saying whether they are in a closure's
// body, where a return leaves the closure.
func (g *cGen) block(depth, n int, inClosure bool) []cStmt {
	var out []cStmt
	for range 1 + g.rng.IntN(n) {
		out = append(out, g.stmt(depth, inClosure))
	}
	if g.rng.IntN(5) == 0 {
		out = append(out, cStmt{kind: cReturn})
	}
	return out
}

// stmt returns a statement nested at most depth deep.
func (g *cGen) stmt(depth int, inClosure bool) cStmt {
	k := cKind(g.rng.IntN(int(cKinds)))
	if g.noI {
		k = []cKind{cUseK, cGuardK, cLeaveK, cLoop, cRun}[g.rng.IntN(5)]
	}
	if depth == 0 && k != cUse && k != cOut && k != cIn && k != cUseK && k != cLeaveK {
		k = []cKind{cUse, cOut, cIn, cUseK}[g.rng.IntN(4)]
		if g.noI {
			k = cUseK
		}
	}
	switch k {
	case cUseK:
		g.ids++
		return cStmt{kind: cUseK, id: g.ids}
	case cGuardK:
		return cStmt{kind: k, body: g.block(depth-1, 3, inClosure)}
	case cRun2:
		body := g.block(depth-1, 3, true)
		outer := g.noI
		g.noI = true
		other := g.block(depth-1, 3, true)
		g.noI = outer
		return cStmt{kind: cRun2, n: g.rng.IntN(3), body: body, other: other}
	case cUse:
		g.ids++
		return cStmt{kind: cUse, id: g.ids}
	case cGuard, cWhen, cLoop:
		return cStmt{kind: k, body: g.block(depth-1, 3, inClosure)}
	case cReturn:
		return cStmt{kind: cLeave}
	case cRun:
		return cStmt{
			kind: cRun, n: g.rng.IntN(3), down: g.rng.IntN(3) == 0, body: g.block(depth-1, 3, true),
		}
	default:
		return cStmt{kind: k}
	}
}

// cRender writes a program out, recording the offset of each check's i.
type cRender struct {
	b  strings.Builder
	at map[int]int
}

// line writes s on a line of its own at indentation ind.
func (r *cRender) line(ind int, s string) {
	r.b.WriteString(strings.Repeat("    ", ind))
	r.b.WriteString(s)
	r.b.WriteString("\n")
}

// block writes body at indentation ind.
func (r *cRender) block(body []cStmt, ind int) {
	for _, s := range body {
		r.stmt(s, ind)
	}
}

// stmt writes s at indentation ind.
func (r *cRender) stmt(s cStmt, ind int) {
	switch s.kind {
	case cUse:
		r.b.WriteString(strings.Repeat("    ", ind) + "if a[")
		r.at[s.id] = r.b.Len()
		r.b.WriteString("i] == 9 {\n")
		r.line(ind+1, `print("x")`)
		r.line(ind, "}")
	case cUseK:
		r.b.WriteString(strings.Repeat("    ", ind) + "if a[")
		r.at[s.id] = r.b.Len()
		r.b.WriteString("k] == 9 {\n")
		r.line(ind+1, `print("x")`)
		r.line(ind, "}")
	case cGuardK:
		r.line(ind, "if 0 <= k && k < 4 {")
		r.block(s.body, ind+1)
		r.line(ind, "}")
	case cLeaveK:
		r.line(ind, "if k < 0 || k >= 4 {")
		r.line(ind+1, "return")
		r.line(ind, "}")
	case cRun2:
		r.line(ind, fmt.Sprintf("run2(%d, func() {", s.n))
		r.block(s.body, ind+1)
		r.line(ind, "}, func() {")
		r.block(s.other, ind+1)
		r.line(ind, "})")
	case cOut:
		r.line(ind, "i = -1")
	case cIn:
		r.line(ind, "i = 2")
	case cReturn:
		r.line(ind, "return")
	case cLeave:
		r.line(ind, "if i < 0 || i >= 4 {")
		r.line(ind+1, "return")
		r.line(ind, "}")
	case cGuard, cWhen, cLoop:
		head := map[cKind]string{
			cGuard: "if 0 <= i && i < 4 {", cWhen: "if i == 0 {", cLoop: "for range 2 {",
		}[s.kind]
		r.line(ind, head)
		r.block(s.body, ind+1)
		r.line(ind, "}")
	case cRun:
		callee := "run"
		if s.down {
			callee = "down"
		}
		r.line(ind, fmt.Sprintf("%s(%d, func() {", callee, s.n))
		r.block(s.body, ind+1)
		r.line(ind, "})")
	}
}

// renderClosureProgram returns the program body is the body of g in,
// and where each check's i is.
func renderClosureProgram(body []cStmt) (string, map[int]int) {
	r := &cRender{at: map[int]int{}}
	r.b.WriteString("package main\n\n")
	r.b.WriteString("func run(n int64, scope f func()) {\n    for range n {\n        f()\n    }\n}\n\n")
	r.b.WriteString("func down(n int64, scope f func()) {\n    run(n, f)\n}\n\n")
	r.b.WriteString("func run2(n int64, scope f func(), scope h func()) {\n" +
		"    for range n {\n        f()\n        h()\n    }\n}\n\n")
	r.b.WriteString("func g(start int64) {\n")
	r.line(1, "a := [4]int64{0, 1, 2, 3}")
	r.line(1, "mut i := start")
	r.line(1, "k := start -% 1")
	r.block(body, 1)
	r.line(1, "if i == 7 && k == 7 && len(a) == 4 {")
	r.line(2, `print("x")`)
	r.line(1, "}")
	r.b.WriteString("}\n\nfunc main() {\n    for s := range 4 {\n        g(s)\n    }\n}\n")
	return r.b.String(), r.at
}

// cOracle is the oracle's state running a program.
type cOracle struct {
	i, k int
	bad  map[int]bool
}

// block runs body and reports whether a return ended it.
func (o *cOracle) block(body []cStmt) bool {
	for _, s := range body {
		if o.stmt(s) {
			return true
		}
	}
	return false
}

// stmt runs s and reports whether a return ended the function or the run
// of the closure it is in.
func (o *cOracle) stmt(s cStmt) bool {
	switch s.kind {
	case cUse:
		if o.i < 0 || o.i >= 4 {
			o.bad[s.id] = true
		}
	case cUseK:
		if o.k < 0 || o.k >= 4 {
			o.bad[s.id] = true
		}
	case cGuardK:
		if 0 <= o.k && o.k < 4 {
			return o.block(s.body)
		}
	case cLeaveK:
		return o.k < 0 || o.k >= 4
	case cRun2:
		for range s.n {
			o.block(s.body)
			o.block(s.other)
		}
	case cOut:
		o.i = -1
	case cIn:
		o.i = 2
	case cReturn:
		return true
	case cLeave:
		return o.i < 0 || o.i >= 4
	case cGuard:
		if 0 <= o.i && o.i < 4 {
			return o.block(s.body)
		}
	case cWhen:
		if o.i == 0 {
			return o.block(s.body)
		}
	case cLoop:
		for range 2 {
			if o.block(s.body) {
				return true
			}
		}
	case cRun:
		for range s.n {
			o.block(s.body)
		}
	}
	return false
}

// closureViolations returns the checks body runs with i out of range, for
// some start of i.
func closureViolations(body []cStmt) map[int]bool {
	o := &cOracle{bad: map[int]bool{}}
	for start := range 4 {
		o.i, o.k = start, start-1
		o.block(body)
	}
	return o.bad
}

// checkClosureProgram checks body's program, failing t when the checker
// accepts a check the oracle runs out of range, or reports an error that
// is not an index's at a check, and returns the checks it rejected.
func checkClosureProgram(t *testing.T, name string, body []cStmt) map[int]bool {
	t.Helper()
	src, at := renderClosureProgram(body)
	_, diags := checkTree(t, src)
	byOffset := map[int]int{}
	for id, off := range at {
		byOffset[off] = id
	}
	rejected := map[int]bool{}
	for _, d := range diags {
		id, ok := byOffset[d.Span.Start]
		if !ok || !strings.HasPrefix(d.Message, "index ") {
			t.Fatalf("%s: an error that is not a check's, %q at offset %d:\n%s",
				name, d.Message, d.Span.Start, src)
		}
		rejected[id] = true
	}
	for id := range closureViolations(body) {
		if !rejected[id] {
			t.Fatalf("%s: check %d runs out of range but is accepted:\n%s", name, id, src)
		}
	}
	return rejected
}

// closureSeeds is how many generated programs TestClosureWritesAreSound
// checks.
const closureSeeds = 2000

// closureCases are programs of the shapes the comment at the top of the
// file names, written out by hand, each of which a checker that let a
// closure's write miss a use would accept.
var closureCases = map[string][]cStmt{
	"a write in a closure ends the fact after its call": {
		{kind: cGuard, body: []cStmt{
			{kind: cRun, n: 1, body: []cStmt{{kind: cOut}}},
			{kind: cUse, id: 1},
		}},
	},
	"a closure's write ends an outer fact on its next run": {
		{kind: cGuard, body: []cStmt{
			{kind: cRun, n: 2, body: []cStmt{{kind: cUse, id: 1}, {kind: cOut}}},
		}},
	},
	"a closure's write ends its own fact on its next run": {
		{kind: cRun, n: 2, body: []cStmt{
			{kind: cGuard, body: []cStmt{{kind: cUse, id: 1}}},
			{kind: cOut},
			{kind: cUse, id: 2},
		}},
	},
	"a write before a closure's return reaches the use after the call": {
		{kind: cGuard, body: []cStmt{
			{kind: cRun, n: 1, body: []cStmt{{kind: cWhen, body: []cStmt{{kind: cOut}, {kind: cReturn}}}}},
			{kind: cUse, id: 1},
		}},
	},
	"a nested closure's write ends the outer closure's fact": {
		{kind: cRun, n: 1, body: []cStmt{
			{kind: cGuard, body: []cStmt{
				{kind: cRun, n: 1, body: []cStmt{{kind: cOut}}},
				{kind: cUse, id: 1},
			}},
		}},
	},
	"a closure's write ends an outer fact through two closures of one call": {
		{kind: cGuard, body: []cStmt{
			{kind: cRun2, n: 2, body: []cStmt{{kind: cUse, id: 1}, {kind: cOut}}, other: []cStmt{
				{kind: cGuardK, body: []cStmt{{kind: cUseK, id: 2}}},
			}},
		}},
	},
	"a closure's write ends an outer fact through a scope parameter passed down": {
		{kind: cGuard, body: []cStmt{
			{kind: cRun, n: 2, down: true, body: []cStmt{{kind: cUse, id: 1}, {kind: cOut}}},
		}},
	},
	"an early exit's fact ends at a closure's write": {
		{kind: cLeave},
		{kind: cRun, n: 2, body: []cStmt{{kind: cUse, id: 1}, {kind: cOut}}},
	},
}

// closureKept are programs whose every check is in range, by a fact
// about the immutable k from outside a closure, which nothing can write
// and so holds in it, alone, beside a closure that writes i in the same
// call, and through a scope parameter passed down: the checker must
// accept each.
var closureKept = map[string][]cStmt{
	"an immutable variable's fact enters a closure": {
		{kind: cGuardK, body: []cStmt{{kind: cRun, n: 2, body: []cStmt{{kind: cUseK, id: 1}}}}},
	},
	"an immutable variable's fact enters the second closure of a call": {
		{kind: cGuardK, body: []cStmt{{kind: cRun2, n: 2, body: []cStmt{{kind: cOut}}, other: []cStmt{
			{kind: cUseK, id: 1},
		}}}},
	},
	"an early exit's fact about k enters a closure passed down": {
		{kind: cLeaveK},
		{kind: cRun, n: 2, down: true, body: []cStmt{{kind: cUseK, id: 1}, {kind: cOut}}},
	},
}

// TestClosureWritesAreSound checks the hand-written programs of
// closureCases, those of closureKept, which the checker must accept
// whole, and then closureSeeds generated ones (see checkClosureProgram).
func TestClosureWritesAreSound(t *testing.T) {
	for name, body := range closureCases {
		if len(closureViolations(body)) == 0 {
			t.Fatalf("%s: the oracle finds no check out of range", name)
		}
		checkClosureProgram(t, name, body)
	}
	for name, body := range closureKept {
		if len(closureViolations(body)) != 0 {
			t.Fatalf("%s: the oracle finds a check out of range", name)
		}
		if rejected := checkClosureProgram(t, name, body); len(rejected) > 0 {
			t.Errorf("%s: checks %v rejected, though a fact about k proves them", name, rejected)
		}
	}
	for seed := range uint64(closureSeeds) {
		g := &cGen{rng: rand.New(rand.NewPCG(seed, 0x636c6f73))}
		checkClosureProgram(t, fmt.Sprintf("seed %d", seed), g.block(4, 4, false))
	}
}

// FuzzClosureWritesAreSound checks programs the generator makes from the
// fuzzer's seeds, as TestClosureWritesAreSound does.
func FuzzClosureWritesAreSound(f *testing.F) {
	for seed := range uint64(8) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, seed uint64) {
		g := &cGen{rng: rand.New(rand.NewPCG(seed, 0x636c6f73))}
		checkClosureProgram(t, fmt.Sprintf("seed %d", seed), g.block(4, 4, false))
	})
}
