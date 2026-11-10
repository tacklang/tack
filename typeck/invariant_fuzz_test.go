package typeck_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// The tests here hunt for a bound a loop keeps that does not hold: an
// index a[i] by a counter i of an 8-bit type T, int8 or uint8, that a
// loop writes, which the checker accepts although some run reads past
// either end of a. The program is
//
//	func f(p T) int64 {
//	    mut a [L]int64
//	    start
//	    mut n := 0
//	    mut s int64 = 0
//	    for ; n < N && cond; post {
//	        n++
//	        body
//	    }
//	    return s
//	}
//
// where start declares i with a known value, mut i T = c, or declares it
// from p and then gives it one, i = c, or declares it from p and leaves
// it there past an early exit comparing it with a known value, so the
// loop starts from a fact; cond, perhaps left out, compares i with a
// known value; post, perhaps left out, steps i as a statement of the
// body may; and body holds statements that step i up or down by a known
// value or by p, set it to a known value, an if whose condition compares
// i or p with a known value, holding more of them and perhaps ending in
// continue or break, or an inner loop of two passes holding more of
// them, whose continue and break act on it, with s +%= a[i] once anywhere
// among them, which needs no bound on s. n bounds every run to N passes,
// and n++ comes first, so a continue cannot skip it.
//
// The oracle is exact because the program has only p for input, and T
// only 256 values: it runs f for each of them, computing every step
// wrapped to T's width, as +% would, and a run that reads a[i] with i
// outside 0 to L - 1 is a counterexample. The checker must reject every
// program that has one. It may reject one that has none, which is
// conservatism. A step of i it does not prove keeps within T is an error
// of its own, which rejects the program too, so in a program it accepts
// no step wraps and the oracle's values are its exact ones.

// loopKind is what a statement of a loop's body does.
type loopKind int

const (
	loopUse   loopKind = iota // s +%= a[i]
	loopInc                   // i++
	loopDec                   // i--
	loopAdd                   // i += k
	loopSub                   // i -= k
	loopSet                   // i = k
	loopAddP                  // i += p
	loopSubP                  // i -= p
	loopIf                    // if guard { body }, perhaps ending in continue or break
	loopInner                 // mut m := 0; for m < 2 { m++; body }
	invKinds
)

// invGuard is a comparison of i, or of p when onP, with the known
// value k.
type invGuard struct {
	op   string
	k    int64
	onP  bool
	none bool
}

// loopStmt is a statement of a loop's body: of kind, with k its known
// value, and, for an if, its guard, its body, and leave, 1 for a continue
// at its end and 2 for a break.
type loopStmt struct {
	body  []loopStmt
	guard invGuard
	kind  loopKind
	k     int64
	leave int
}

// loopProgram is the program the tests check (see above).
type loopProgram struct {
	typ    string
	body   []loopStmt
	post   []loopStmt
	cond   invGuard
	exit   invGuard
	start  int64
	length int64
	passes int64
	from   int
}

// How a program starts i: with a known value, from p and then set to a
// known value, or from p past an early exit.
const (
	fromKnown = iota
	fromSet
	fromFact
)

// loopConsts are the known values of each type the programs use, near
// its ends and near 0.
var loopConsts = map[string][]int64{
	"int8":  {-128, -127, -2, -1, 0, 1, 2, 3, 5, 7, 100, 126, 127},
	"uint8": {0, 1, 2, 3, 5, 7, 100, 200, 254, 255},
}

// loopSteps are the known amounts the programs step i by.
var loopSteps = []int64{1, 2, 3, 5, 100, 127}

// loopOps are the comparisons a guard makes.
var loopOps = []string{"<", "<=", ">", ">=", "!="}

// loopLengths are the lengths of a.
var loopLengths = []int64{1, 3, 8, 128, 256}

// readLoopProgram reads a program from data (see convReader.next), with
// at most one inner loop, whose counter m only one may declare.
func readLoopProgram(data []byte) loopProgram {
	r := &convReader{data: data}
	p := loopProgram{typ: []string{"int8", "uint8"}[r.next(2)]}
	consts := loopConsts[p.typ]
	p.start = consts[r.next(len(consts))]
	p.from = r.next(3)
	if p.from == fromFact {
		p.exit = invGuard{op: loopOps[r.next(len(loopOps))], k: consts[r.next(len(consts))]}
	}
	p.length = loopLengths[r.next(len(loopLengths))]
	p.passes = []int64{3, 300}[r.next(2)]
	p.cond = invGuard{none: true}
	if r.next(3) > 0 {
		p.cond = invGuard{op: loopOps[r.next(len(loopOps))], k: consts[r.next(len(consts))]}
	}
	p.body = readLoopStmts(r, consts, 0)
	inner := false
	for i, s := range p.body {
		if s.kind == loopInner && inner {
			p.body[i] = loopStmt{kind: loopInc}
		}
		inner = inner || s.kind == loopInner
	}
	p.body = insertUse(r, p.body)
	if r.next(3) == 0 {
		post := readLoopStmts(r, consts, 2)
		p.post = post[:1]
	}
	return p
}

// readLoopStmts reads from one to three statements, none a use: an inner
// loop only at depth 0, and an if only at depth 0 or directly in an inner
// loop's body, which is read at depth 1, as an if's body is read one
// deeper than the if.
func readLoopStmts(r *convReader, consts []int64, depth int) []loopStmt {
	n := 1 + r.next(3)
	stmts := make([]loopStmt, 0, n)
	for range n {
		kind := loopKind(1 + r.next(int(invKinds)-1))
		if kind == loopInner && depth > 0 || kind == loopIf && depth > 1 {
			kind = loopInc
		}
		s := loopStmt{kind: kind}
		switch kind {
		case loopAdd, loopSub:
			s.k = loopSteps[r.next(len(loopSteps))]
		case loopSet:
			s.k = consts[r.next(len(consts))]
		case loopIf:
			s.guard = invGuard{
				op:  loopOps[r.next(len(loopOps))],
				k:   consts[r.next(len(consts))],
				onP: r.next(2) == 1,
			}
			s.body = readLoopStmts(r, consts, depth+1)
			s.leave = r.next(3)
		case loopInner:
			s.body = readLoopStmts(r, consts, depth+1)
		}
		stmts = append(stmts, s)
	}
	return stmts
}

// insertUse puts the use at a place r picks: among stmts, or inside the
// body of one of its ifs or inner loops.
func insertUse(r *convReader, stmts []loopStmt) []loopStmt {
	var ifs []int
	for i, s := range stmts {
		if s.kind == loopIf || s.kind == loopInner {
			ifs = append(ifs, i)
		}
	}
	use := loopStmt{kind: loopUse}
	if len(ifs) > 0 && r.next(2) == 1 {
		i := ifs[r.next(len(ifs))]
		at := r.next(len(stmts[i].body) + 1)
		stmts[i].body = slices.Insert(slices.Clone(stmts[i].body), at, use)
		return stmts
	}
	return slices.Insert(stmts, r.next(len(stmts)+1), use)
}

// source returns p's text.
func (p loopProgram) source() string {
	var b strings.Builder
	fmt.Fprintf(&b, "package main\n\nfunc f(p %s) int64 {\n", p.typ)
	fmt.Fprintf(&b, "    mut a [%d]int64\n", p.length)
	switch p.from {
	case fromKnown:
		fmt.Fprintf(&b, "    mut i %s = %d\n", p.typ, p.start)
	case fromSet:
		fmt.Fprintf(&b, "    mut i %s = p\n    i = %d\n", p.typ, p.start)
	default:
		fmt.Fprintf(&b, "    mut i %s = p\n    if %s {\n        return 0\n    }\n",
			p.typ, p.exit.text())
	}
	b.WriteString("    mut n := 0\n    mut s int64 = 0\n")
	cond := "n < " + strconv.FormatInt(p.passes, 10)
	if !p.cond.none {
		cond += " && " + p.cond.text()
	}
	if len(p.post) > 0 {
		cond = "; " + cond + "; " + p.post[0].text()
	}
	fmt.Fprintf(&b, "    for %s {\n        n++\n", cond)
	writeLoopStmts(&b, p.body, "        ")
	b.WriteString("    }\n    a[0] = s\n    return a[0]\n}\n\nfunc main() {\n    print(\"\")\n}\n")
	return b.String()
}

// text returns g's comparison.
func (g invGuard) text() string {
	name := "i"
	if g.onP {
		name = "p"
	}
	return fmt.Sprintf("%s %s %d", name, g.op, g.k)
}

// writeLoopStmts writes stmts to b, each line indented by indent.
func writeLoopStmts(b *strings.Builder, stmts []loopStmt, indent string) {
	for _, s := range stmts {
		if s.kind == loopInner {
			b.WriteString(indent + "mut m := 0\n" + indent + "for m < 2 {\n" +
				indent + "    m++\n")
			writeLoopStmts(b, s.body, indent+"    ")
			b.WriteString(indent + "}\n")
			continue
		}
		if s.kind != loopIf {
			b.WriteString(indent + s.text() + "\n")
			continue
		}
		fmt.Fprintf(b, "%sif %s {\n", indent, s.guard.text())
		writeLoopStmts(b, s.body, indent+"    ")
		switch s.leave {
		case 1:
			b.WriteString(indent + "    continue\n")
		case 2:
			b.WriteString(indent + "    break\n")
		}
		b.WriteString(indent + "}\n")
	}
}

// text returns s's text, for a statement other than an if.
func (s loopStmt) text() string {
	switch s.kind {
	case loopUse:
		return "s +%= a[i]"
	case loopInc:
		return "i++"
	case loopDec:
		return "i--"
	case loopAdd:
		return fmt.Sprintf("i += %d", s.k)
	case loopSub:
		return fmt.Sprintf("i -= %d", s.k)
	case loopSet:
		return fmt.Sprintf("i = %d", s.k)
	case loopAddP:
		return "i += p"
	default:
		return "i -= p"
	}
}

// loopRun is one run of a program, for p: where i is, and how the
// statements run so far ended.
type loopRun struct {
	p, i int64
	bad  bool
}

// wrap8 returns v wrapped to the width of typ, as Tack computes it.
func wrap8(v int64, typ string) int64 {
	v &= 0xff
	if typ == "int8" && v >= 128 {
		v -= 256
	}
	return v
}

// holds reports whether g holds in run.
func (g invGuard) holds(run *loopRun) bool {
	v := run.i
	if g.onP {
		v = run.p
	}
	switch g.op {
	case "<":
		return v < g.k
	case "<=":
		return v <= g.k
	case ">":
		return v > g.k
	case ">=":
		return v >= g.k
	default:
		return v != g.k
	}
}

// exec runs stmts and returns 0 when they fall through, 1 for a continue
// and 2 for a break, setting run.bad and stopping at a read out of range.
func (p loopProgram) exec(run *loopRun, stmts []loopStmt) int {
	for _, s := range stmts {
		switch {
		case s.kind == loopUse:
			if run.i < 0 || run.i >= p.length {
				run.bad = true
				return 2
			}
		case s.kind == loopInner:
			if p.execInner(run, s); run.bad {
				return 2
			}
		case s.kind != loopIf:
			run.i = p.step(run, s)
		case s.guard.holds(run):
			if how := p.execIf(run, s); how != 0 {
				return how
			}
		}
	}
	return 0
}

// execInner runs s, an inner loop, for its two passes, a continue in its
// body ending a pass and a break ending the loop, stopping at a read out
// of range.
func (p loopProgram) execInner(run *loopRun, s loopStmt) {
	for range 2 {
		if p.exec(run, s.body) == 2 || run.bad {
			return
		}
	}
}

// execIf runs the body of s, an if whose guard holds in run, and
// returns how it ended, as exec does, or its leave when it falls through.
func (p loopProgram) execIf(run *loopRun, s loopStmt) int {
	if how := p.exec(run, s.body); how != 0 || run.bad {
		return how
	}
	return s.leave
}

// step returns the value s, a write to i, stores in run.
func (p loopProgram) step(run *loopRun, s loopStmt) int64 {
	switch s.kind {
	case loopInc:
		return wrap8(run.i+1, p.typ)
	case loopDec:
		return wrap8(run.i-1, p.typ)
	case loopAdd:
		return wrap8(run.i+s.k, p.typ)
	case loopSub:
		return wrap8(run.i-s.k, p.typ)
	case loopSet:
		return s.k
	case loopAddP:
		return wrap8(run.i+run.p, p.typ)
	default:
		return wrap8(run.i-run.p, p.typ)
	}
}

// counterexample returns a p for which f reads a[i] out of range, or ""
// when none does.
func (p loopProgram) counterexample() string {
	lo, hi := int64(-128), int64(127)
	if p.typ == "uint8" {
		lo, hi = 0, 255
	}
	for pv := lo; pv <= hi; pv++ {
		if p.runBad(pv) {
			return "p = " + strconv.FormatInt(pv, 10)
		}
	}
	return ""
}

// runBad reports whether f, run for pv, reads a[i] out of range.
func (p loopProgram) runBad(pv int64) bool {
	run := &loopRun{p: pv, i: p.start}
	if p.from == fromFact {
		run.i = pv
		if p.exit.holds(run) {
			return false
		}
	}
	for n := int64(0); n < p.passes && (p.cond.none || p.cond.holds(run)); n++ {
		if p.exec(run, p.body) == 2 || run.bad {
			break
		}
		p.exec(run, p.post)
	}
	return run.bad
}

// loopVerdict is what checking one program found: whether it was checked
// as a whole, with no error but its use's, and whether its use was
// accepted.
type loopVerdict struct {
	checked, accepted bool
}

// stepOverflows reports whether msg is the error for a step of i not
// proven to stay within its type.
func stepOverflows(msg string) bool {
	for _, step := range []string{"i++ ", "i-- ", "i += ", "i -= "} {
		if strings.HasPrefix(msg, step) &&
			(strings.Contains(msg, " may overflow ") || strings.Contains(msg, " may go below 0 in ")) {
			return true
		}
	}
	return false
}

// checkLoop checks the program p and fails t when the checker accepts
// its use although the oracle finds a counterexample. A step of i not
// proven to stay within its type rejects the program as an unproven use
// does.
func checkLoop(t *testing.T, p loopProgram) loopVerdict {
	t.Helper()
	src := p.source()
	_, diags := checkTree(t, src)
	at := strings.Index(src, "a[i]")
	v := loopVerdict{checked: true, accepted: true}
	for _, d := range diags {
		if stepOverflows(d.Message) {
			v.accepted = false
			continue
		}
		if d.Span.Start < at || d.Span.End > at+len("a[i]") ||
			!strings.HasPrefix(d.Message, "index ") {
			t.Logf("unexpected %s:\n%s", d.Message, src)
			return loopVerdict{}
		}
		v.accepted = false
	}
	if v.accepted {
		if bad := p.counterexample(); bad != "" {
			t.Fatalf("accepted, but out of range for %s:\n%s", bad, src)
		}
	}
	return v
}

// TestLoopOracleFindsBreaks checks the oracle on loops whose counter
// leaves a's range for some p, and that the checker rejects them.
func TestLoopOracleFindsBreaks(t *testing.T) {
	use := loopStmt{kind: loopUse}
	for _, tt := range []struct {
		name string
		p    loopProgram
	}{
		{"int8 i++ wrapping past 127", loopProgram{
			typ: "int8", start: 120, length: 128, passes: 300, cond: invGuard{none: true},
			body: []loopStmt{use, {kind: loopInc}},
		}},
		{"i -= p for a negative p", loopProgram{
			typ: "int8", start: 0, length: 8, passes: 3, cond: invGuard{op: "<", k: 7},
			body: []loopStmt{use, {kind: loopSubP}},
		}},
		{"uint8 i-- wrapping past 0", loopProgram{
			typ: "uint8", start: 2, length: 8, passes: 300, cond: invGuard{none: true},
			body: []loopStmt{use, {kind: loopDec}},
		}},
		{"a post stepping i back past 0", loopProgram{
			typ: "int8", start: 0, length: 8, passes: 300, cond: invGuard{op: "<", k: 7},
			body: []loopStmt{use, {kind: loopInc}}, post: []loopStmt{{kind: loopSub, k: 3}},
		}},
		{"a start from p, an early exit allowing -1", loopProgram{
			typ: "int8", from: fromFact, exit: invGuard{op: "<", k: -1}, length: 8, passes: 3,
			cond: invGuard{op: "<", k: 7}, body: []loopStmt{use, {kind: loopInc}},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if bad := tt.p.counterexample(); bad == "" {
				t.Fatalf("the oracle calls it safe:\n%s", tt.p.source())
			}
			if v := checkLoop(t, tt.p); !v.checked || v.accepted {
				t.Fatalf("checked %v, accepted %v:\n%s", v.checked, v.accepted, tt.p.source())
			}
		})
	}
}

// TestLoopOracleCallsSafeLoopsSafe checks that the oracle finds nothing
// for loops whose counter stays in range, at the edges, and that the
// checker accepts them, so neither passes the tests above by calling
// everything unsafe.
func TestLoopOracleCallsSafeLoopsSafe(t *testing.T) {
	use := loopStmt{kind: loopUse}
	for _, tt := range []struct {
		name string
		p    loopProgram
	}{
		{"int8 i++ from 0 under i < 8", loopProgram{
			typ: "int8", start: 0, length: 8, passes: 300, cond: invGuard{op: "<", k: 8},
			body: []loopStmt{use, {kind: loopInc}},
		}},
		{"int8 i -= 2 from 7 under i >= 2", loopProgram{
			typ: "int8", start: 7, length: 8, passes: 300, cond: invGuard{op: ">=", k: 2},
			body: []loopStmt{use, {kind: loopSub, k: 2}},
		}},
		{"int8 i += p under p in a guard, reset at the end", loopProgram{
			typ: "int8", start: 0, length: 128, passes: 300, cond: invGuard{none: true},
			body: []loopStmt{use, {
				kind: loopIf, guard: invGuard{op: "<", k: 100},
				body: []loopStmt{{kind: loopInc}}, leave: 1,
			}, {kind: loopSet, k: 0}},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if bad := tt.p.counterexample(); bad != "" {
				t.Fatalf("the oracle finds %s:\n%s", bad, tt.p.source())
			}
			if v := checkLoop(t, tt.p); !v.checked || !v.accepted {
				t.Fatalf("checked %v, accepted %v:\n%s", v.checked, v.accepted, tt.p.source())
			}
		})
	}
}

// loopSeeds is how many programs TestLoopInvariantsAreSound checks, and
// loopRunSize how many of them one parallel subtest checks.
const (
	loopSeeds   = 20000
	loopRunSize = 250
)

// TestLoopInvariantsAreSound checks the programs of the first loopSeeds
// seeds (see checkLoop), and that among the ones checked as a whole the
// checker accepts some whose index needs a bound the loop keeps, an
// int8 i into fewer than 128 elements, and rejects others, so the test
// does not pass by everything going one way.
func TestLoopInvariantsAreSound(t *testing.T) {
	var tally loopTally
	t.Run("seeds", func(t *testing.T) {
		for from := uint64(0); from < loopSeeds; from += loopRunSize {
			t.Run(strconv.FormatUint(from, 10), func(t *testing.T) {
				t.Parallel()
				for seed := from; seed < from+loopRunSize; seed++ {
					p := seededLoopProgram(seed)
					tally.add(p, checkLoop(t, p))
				}
			})
		}
	})
	if t.Failed() {
		return
	}
	all, ok, k := tally.checked.Load(), tally.accepted.Load(), tally.kept.Load()
	shapes := make([]int64, len(tally.shapes))
	for i := range tally.shapes {
		shapes[i] = tally.shapes[i].Load()
	}
	if ok == 0 || ok == all || k == 0 || slices.Contains(shapes, 0) {
		t.Fatalf("accepted %d of %d uses, %d needing a kept bound, by post, set start, fact "+
			"start and inner loop %v; the programs prove nothing either way", ok, all, k, shapes)
	}
	t.Logf("accepted %d of %d uses, %d needing a kept bound, by post, set start, fact start "+
		"and inner loop %v", ok, all, k, shapes)
}

// loopTally counts what TestLoopInvariantsAreSound found: the programs
// checked as a whole, the uses accepted, and those accepted that need a
// bound the loop keeps, an int8 i into fewer than 128 elements, of which
// shapes counts those with a post, starting from a set value, starting
// from a fact, and with an inner loop.
type loopTally struct {
	checked, accepted, kept atomic.Int64
	shapes                  [4]atomic.Int64
}

// add counts p, of which checking found v.
func (c *loopTally) add(p loopProgram, v loopVerdict) {
	if v.checked {
		c.checked.Add(1)
	}
	if !v.accepted {
		return
	}
	c.accepted.Add(1)
	if p.typ != "int8" || p.length >= 128 {
		return
	}
	c.kept.Add(1)
	inner := slices.ContainsFunc(p.body, func(s loopStmt) bool { return s.kind == loopInner })
	for i, has := range []bool{len(p.post) > 0, p.from == fromSet, p.from == fromFact, inner} {
		if has {
			c.shapes[i].Add(1)
		}
	}
}

// seededLoopProgram returns the program of seed: one read from 48 bytes
// the seed's generator makes.
func seededLoopProgram(seed uint64) loopProgram {
	rng := rand.New(rand.NewPCG(seed, 0x6c6f6f70))
	data := make([]byte, 48)
	for i := range data {
		data[i] = byte(rng.Uint32())
	}
	return readLoopProgram(data)
}

// FuzzLoopInvariantsAreSound checks the program its input makes (see
// checkLoop).
func FuzzLoopInvariantsAreSound(f *testing.F) {
	for _, s := range [][]byte{
		{0, 4, 2, 1, 0, 0, 1, 0, 1, 2},
		{1, 0, 1, 1, 1, 3, 2, 8, 0, 2, 4, 1, 1, 3},
		{0, 7, 0, 0, 2, 0, 8, 1, 3, 4, 0, 2, 1},
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		checkLoop(t, readLoopProgram(data))
	})
}
