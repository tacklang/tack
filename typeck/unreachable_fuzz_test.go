package typeck_test

import (
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
)

// The tests here hunt for an unreachable the checker accepts although a
// run reaches it. They build a function f(x int64, b int8, u uint64, z
// bool) int64, with a mutable local m that starts as x, from a tree of
// statements: ifs with and without an else, early returns, switches on
// x, b, u, m and z with cases of one or two values, some of which set m
// as they are tried, pin(mut m, k), and a default,
// written last or among the cases, or, with no default, every case
// ending in a return or a fallthrough, so that the code after the switch
// is reached only when no case matched, cases but the last ending in
// fallthrough, which enters the next case's body without testing it,
// loops
// over 2 with break and continue, defers, writes to m, and unreachable
// statements, under conditions that compare x, b, u and m with small
// constants, test z, and combine those with &&, || and !. When the
// checker accepts the function, the oracle runs it, with Tack's
// semantics, on every combination of values at and near the edges of
// each parameter's type, and fails if any run reaches an unreachable.
// The checker may reject a function no run reaches the unreachable of:
// that is conservatism, not unsoundness.

// uConsts are the constants the conditions, writes and cases use: small
// ones, near the bounds a guard often sets, which u compares with only
// when they are not negative, since an unsigned value never is.
var uConsts = []int64{-1, 0, 1, 2, 3}

// uVar is a variable a condition reads: x, b, u or m, indexed by the
// order uVarNames gives, and z, a bool, at uZ.
const (
	uX = iota
	uB
	uU
	uM
	uZ
	uVarCount
)

// uVarNames are the variables' names, in the order of their indexes.
var uVarNames = [uVarCount]string{"x", "b", "u", "m", "z"}

// uKind is the kind of a generated statement.
type uKind int

// The kinds of generated statement.
const (
	uUnreachable uKind = iota // unreachable
	uIf                       // if cond { then } else { els }
	uExit                     // if cond { return 1 }
	uReturn                   // return 2
	uWrite                    // m = k, or m = x
	uSwitch                   // switch v { case ...: ... default: ... }
	uLoop                     // for range 2 { body }
	uBreak                    // break, inside a loop
	uContinue                 // continue, inside a loop
	uDefer                    // defer { body }
	uKinds
)

// uCond is a generated condition: a comparison of v with k, z or !z, or
// && or || of two conditions, or ! of one.
type uCond struct {
	op   string // a comparison's operator, "z", "!", "&&" or "||"
	v    int
	k    int64
	l, r *uCond
}

// uCase is a case of a generated switch: its values, or none for the
// default, its body, and whether the body ends in a fallthrough. A value
// pins says is written pin(mut m, k), which sets m to k as it is tried,
// and gives k.
type uCase struct {
	values []int64
	pins   []bool
	body   []*uStmt
	falls  bool
}

// uStmt is a generated statement of kind kind: its condition, its
// branches, a loop's or a defer's body in then, the variable a switch is
// on, its cases, and what a write stores, k, or x when fromX.
type uStmt struct {
	kind      uKind
	cond      *uCond
	then, els []*uStmt
	hasElse   bool
	v         int
	cases     []uCase
	k         int64
	fromX     bool
}

// uGen builds a function from a source of choices.
type uGen struct {
	next func(n int) int
}

// cond returns a condition nested at most depth deep.
func (g *uGen) cond(depth int) *uCond {
	if depth > 0 {
		switch g.next(6) {
		case 0:
			return &uCond{op: "&&", l: g.cond(depth - 1), r: g.cond(depth - 1)}
		case 1:
			return &uCond{op: "||", l: g.cond(depth - 1), r: g.cond(depth - 1)}
		case 2:
			return &uCond{op: "!", l: g.cond(depth - 1)}
		}
	}
	v := g.next(uVarCount)
	if v == uZ {
		return &uCond{op: "z"}
	}
	return &uCond{
		op: []string{"<", "<=", ">", ">=", "==", "!="}[g.next(6)],
		v:  v,
		k:  g.konst(v),
	}
}

// konst returns a constant v may be compared with or switched on: one of
// uConsts, not a negative one for u.
func (g *uGen) konst(v int) int64 {
	if v == uU {
		return uConsts[1+g.next(len(uConsts)-1)]
	}
	return uConsts[g.next(len(uConsts))]
}

// block returns up to four statements nested at most depth deep, loops
// saying how many loops are around them and deferred whether they are in
// deferred code, which may not return, break out of it or defer again.
func (g *uGen) block(depth, loops int, deferred bool) []*uStmt {
	n := 1 + g.next(4)
	out := make([]*uStmt, 0, n)
	for range n {
		out = append(out, g.stmt(depth, loops, deferred))
	}
	return out
}

// stmt returns one statement nested at most depth deep.
func (g *uGen) stmt(depth, loops int, deferred bool) *uStmt {
	kind := uKind(g.next(int(uKinds)))
	if depth == 0 && (kind == uIf || kind == uSwitch || kind == uLoop || kind == uDefer) {
		kind = uExit
	}
	if (kind == uBreak || kind == uContinue) && loops == 0 {
		kind = uWrite
	}
	if deferred && (kind == uExit || kind == uReturn || kind == uDefer ||
		kind == uBreak || kind == uContinue || kind == uLoop) {
		kind = uWrite
	}
	s := &uStmt{kind: kind}
	switch kind {
	case uIf:
		s.cond = g.cond(2)
		s.then = g.block(depth-1, loops, deferred)
		if g.next(2) == 0 {
			s.hasElse, s.els = true, g.block(depth-1, loops, deferred)
		}
	case uExit:
		s.cond = g.cond(2)
	case uWrite:
		s.k, s.fromX = uConsts[g.next(len(uConsts))], g.next(4) == 0
	case uSwitch:
		s.v = g.next(uVarCount)
		s.cases = g.cases(s.v, depth, loops, deferred)
	case uLoop:
		s.then = g.block(depth-1, loops+1, deferred)
	case uDefer:
		s.then = g.block(depth-1, 0, true)
	}
	return s
}

// cases returns the cases of a switch on v: up to three with values,
// each value named once, and a default or none, written last or, one
// time in two, among them. Each case but the last may end in a
// fallthrough, which, in a switch whose every case leaves, takes the
// place of its return.
func (g *uGen) cases(v, depth, loops int, deferred bool) []uCase {
	pool := []int64{0, 1}
	if v != uZ {
		pool = nil
		for _, k := range uConsts {
			if v != uU || k >= 0 {
				pool = append(pool, k)
			}
		}
	}
	// Half the switches end every case in a return, so the place after
	// one is reached only past every case, as the proof of an
	// unreachable there asks.
	leave := g.next(2) == 0 && !deferred
	var cases []uCase
	for range 1 + g.next(3) {
		if len(pool) == 0 {
			break
		}
		var values []int64
		for range 1 + g.next(2) {
			if len(pool) == 0 {
				break
			}
			i := g.next(len(pool))
			values = append(values, pool[i])
			pool = append(pool[:i:i], pool[i+1:]...)
		}
		// A value that writes m, on a tag that is not a bool, so that a
		// default written before it, and every case that default falls
		// into, runs after the write.
		pins := make([]bool, len(values))
		for j := range pins {
			pins[j] = v != uZ && g.next(4) == 0
		}
		cases = append(cases, uCase{values: values, pins: pins, body: g.block(depth-1, loops, deferred)})
	}
	if !leave && g.next(2) == 0 {
		def := uCase{body: g.block(depth-1, loops, deferred)}
		at := len(cases)
		if g.next(2) == 0 {
			at = g.next(len(cases) + 1)
		}
		cases = append(cases[:at:at], append([]uCase{def}, cases[at:]...)...)
	}
	for i := range cases {
		cases[i].falls = i < len(cases)-1 && g.next(3) == 0
		if leave && !cases[i].falls {
			cases[i].body = append(cases[i].body, &uStmt{kind: uReturn})
		}
	}
	return cases
}

// text writes c as Tack.
func (c *uCond) text() string {
	switch c.op {
	case "z":
		return "z"
	case "!":
		return "!(" + c.l.text() + ")"
	case "&&", "||":
		return "(" + c.l.text() + " " + c.op + " " + c.r.text() + ")"
	}
	return uVarNames[c.v] + " " + c.op + " " + strconv.FormatInt(c.k, 10)
}

// write appends s's lines, indented by in, to b.
func (s *uStmt) write(b *strings.Builder, in string) {
	switch s.kind {
	case uUnreachable:
		b.WriteString(in + "unreachable\n")
	case uIf:
		b.WriteString(in + "if " + s.cond.text() + " {\n")
		writeAll(b, s.then, in+"    ")
		if s.hasElse {
			b.WriteString(in + "} else {\n")
			writeAll(b, s.els, in+"    ")
		}
		b.WriteString(in + "}\n")
	case uExit:
		b.WriteString(in + "if " + s.cond.text() + " {\n" + in + "    return 1\n" + in + "}\n")
	case uReturn:
		b.WriteString(in + "return 2\n")
	case uWrite:
		value := strconv.FormatInt(s.k, 10)
		if s.fromX {
			value = "x"
		}
		b.WriteString(in + "m = " + value + "\n")
	case uSwitch:
		b.WriteString(in + "switch " + uVarNames[s.v] + " {\n")
		for _, cl := range s.cases {
			if cl.values == nil {
				b.WriteString(in + "default:\n")
			} else {
				var vs []string
				for j, k := range cl.values {
					if cl.pins[j] {
						vs = append(vs, "pin(mut m, "+strconv.FormatInt(k, 10)+")")
						continue
					}
					vs = append(vs, caseText(s.v, k))
				}
				b.WriteString(in + "case " + strings.Join(vs, ", ") + ":\n")
			}
			writeAll(b, cl.body, in+"    ")
			if cl.falls {
				b.WriteString(in + "    fallthrough\n")
			}
		}
		b.WriteString(in + "}\n")
	case uLoop:
		b.WriteString(in + "for range 2 {\n")
		writeAll(b, s.then, in+"    ")
		b.WriteString(in + "}\n")
	case uBreak:
		b.WriteString(in + "break\n")
	case uContinue:
		b.WriteString(in + "continue\n")
	case uDefer:
		b.WriteString(in + "defer {\n")
		writeAll(b, s.then, in+"    ")
		b.WriteString(in + "}\n")
	}
}

// caseText writes a case value k of a switch on v: true or false for z.
func caseText(v int, k int64) string {
	if v == uZ {
		return strconv.FormatBool(k == 1)
	}
	return strconv.FormatInt(k, 10)
}

// writeAll appends the lines of each of body, indented by in, to b.
func writeAll(b *strings.Builder, body []*uStmt, in string) {
	for _, s := range body {
		s.write(b, in)
	}
}

// uSource returns the function body makes, with a main that calls it.
func uSource(body []*uStmt) string {
	var b strings.Builder
	b.WriteString("package main\n\nfunc f(x int64, b int8, u uint64, z bool) int64 {\n" +
		"    mut m := x\n")
	writeAll(&b, body, "    ")
	b.WriteString("    return m\n}\n\nfunc main() {\n    if f(1, 1, 1, true) == 0 {\n        exit(1)\n    }\n}\n")
	b.WriteString("\n// pin sets m to k and gives k.\nfunc pin(mut m int64, k int64) int64 {\n    m = k\n    return k\n}\n")
	return b.String()
}

// uCtrl is how a statement ended: normally, by a return, a break or a
// continue.
type uCtrl int

// The ways a statement ends.
const (
	uNormal uCtrl = iota
	uReturned
	uBroke
	uContinued
)

// uRun is one run of a generated function: the parameters' values, m's,
// and whether an unreachable was reached.
type uRun struct {
	x, b, m int64
	u       uint64
	z       bool
	reached bool
}

// holds reports whether c is true in r.
func (r *uRun) holds(c *uCond) bool {
	switch c.op {
	case "z":
		return r.z
	case "!":
		return !r.holds(c.l)
	case "&&":
		return r.holds(c.l) && r.holds(c.r)
	case "||":
		return r.holds(c.l) || r.holds(c.r)
	}
	return uCompare(r.value(c.v), c.op, c.k)
}

// value returns v's value in r, which compare reads by value: u, which
// the generator compares only with constants that are not negative, is
// at most the largest int64 among the values tried or else far above
// every constant, so it compares as the largest int64 does.
func (r *uRun) value(v int) int64 {
	switch v {
	case uX:
		return r.x
	case uB:
		return r.b
	case uU:
		if r.u > math.MaxInt64 {
			return math.MaxInt64
		}
		return int64(r.u)
	case uZ:
		if r.z {
			return 1
		}
		return 0
	}
	return r.m
}

// uCompare reports whether a op k.
func uCompare(a int64, op string, k int64) bool {
	switch op {
	case "<":
		return a < k
	case "<=":
		return a <= k
	case ">":
		return a > k
	case ">=":
		return a >= k
	case "==":
		return a == k
	}
	return a != k
}

// block runs body, then its defers, last first, however it ends.
func (r *uRun) block(body []*uStmt) uCtrl {
	var defers [][]*uStmt
	ctrl := uNormal
	for _, s := range body {
		if s.kind == uDefer {
			defers = append(defers, s.then)
			continue
		}
		if ctrl = r.stmt(s); ctrl != uNormal {
			break
		}
	}
	for i := len(defers) - 1; i >= 0; i-- {
		r.block(defers[i])
	}
	return ctrl
}

// stmt runs s.
func (r *uRun) stmt(s *uStmt) uCtrl {
	switch s.kind {
	case uUnreachable:
		r.reached = true
	case uIf:
		if r.holds(s.cond) {
			return r.block(s.then)
		}
		return r.block(s.els)
	case uExit:
		if r.holds(s.cond) {
			return uReturned
		}
	case uReturn:
		return uReturned
	case uWrite:
		r.m = s.k
		if s.fromX {
			r.m = r.x
		}
	case uSwitch:
		return r.cases(s)
	case uLoop:
		for range 2 {
			ctrl := r.block(s.then)
			if ctrl == uReturned {
				return ctrl
			}
			if ctrl == uBroke {
				break
			}
		}
	case uBreak:
		return uBroke
	case uContinue:
		return uContinued
	}
	return uNormal
}

// cases runs the case of s that matches its tag, or its default, or none,
// and then, while the body run ends in a fallthrough, the body of the case
// written after it.
func (r *uRun) cases(s *uStmt) uCtrl {
	tag := r.value(s.v)
	for i, cl := range s.cases {
		for j, k := range cl.values {
			if cl.pins[j] {
				r.m = k
			}
			if tag == k {
				return r.fall(s.cases[i:])
			}
		}
	}
	for i, cl := range s.cases {
		if cl.values == nil {
			return r.fall(s.cases[i:])
		}
	}
	return uNormal
}

// fall runs the body of the first of cases, and of each after it while
// the one before ends normally in a fallthrough.
func (r *uRun) fall(cases []uCase) uCtrl {
	for _, cl := range cases {
		if ctrl := r.block(cl.body); ctrl != uNormal || !cl.falls {
			return ctrl
		}
	}
	return uNormal
}

// reachesUnreachable reports whether any run of body, for the values
// tried, reaches an unreachable.
func reachesUnreachable(body []*uStmt) bool {
	xs := []int64{math.MinInt64, -2, -1, 0, 1, 2, 3, 4, math.MaxInt64}
	bs := []int64{-128, -1, 0, 1, 2, 3, 4, 127}
	us := []uint64{0, 1, 2, 3, 4, math.MaxInt64, math.MaxUint64}
	for _, x := range xs {
		for _, b := range bs {
			for _, u := range us {
				for _, z := range []bool{false, true} {
					r := &uRun{x: x, b: b, m: x, u: u, z: z}
					r.block(body)
					if r.reached {
						return true
					}
				}
			}
		}
	}
	return false
}

// hasUnreachable reports whether body holds an unreachable anywhere.
func hasUnreachable(body []*uStmt) bool {
	for _, s := range body {
		if s.kind == uUnreachable || hasUnreachable(s.then) || hasUnreachable(s.els) {
			return true
		}
		for _, cl := range s.cases {
			if hasUnreachable(cl.body) {
				return true
			}
		}
	}
	return false
}

// hasFallthrough reports whether body holds a case ending in fallthrough
// anywhere.
func hasFallthrough(body []*uStmt) bool {
	for _, s := range body {
		if hasFallthrough(s.then) || hasFallthrough(s.els) {
			return true
		}
		for _, cl := range s.cases {
			if cl.falls || hasFallthrough(cl.body) {
				return true
			}
		}
	}
	return false
}

// checkUnreachableSound checks the function the choices next makes: when
// the checker accepts it, no run may reach an unreachable. It reports
// whether the checker accepted a function holding one, and whether that
// function holds a fallthrough too.
func checkUnreachableSound(t *testing.T, next func(n int) int) (accepted, fell bool) {
	t.Helper()
	g := &uGen{next: next}
	body := g.block(3, 0, false)
	src := uSource(body)
	if _, diags := checkTree(t, src); len(diags) > 0 {
		return false, false
	}
	if reachesUnreachable(body) {
		t.Fatalf("accepted, but a run reaches an unreachable:\n%s", src)
	}
	accepted = hasUnreachable(body)
	return accepted, accepted && hasFallthrough(body)
}

// bytesChoice returns a source of choices that reads data a byte at a
// time, 0 past its end.
func bytesChoice(data []byte) func(n int) int {
	at := 0
	return func(n int) int {
		if at >= len(data) {
			return 0
		}
		b := data[at]
		at++
		return int(b) % n
	}
}

// FuzzUnreachableIsSound checks that no function built from the fuzzer's
// bytes that the checker accepts reaches an unreachable on any of the
// values tried.
func FuzzUnreachableIsSound(f *testing.F) {
	for _, s := range [][]byte{
		{0},
		{1, 1, 0, 4, 0, 1, 0},
		{2, 1, 2, 0, 0, 1, 0, 3, 1, 0, 0},
		{1, 5, 3, 0, 1, 2, 1, 4, 0},
		{3, 6, 1, 2, 0, 0, 9, 4, 2, 0, 1, 0, 0},
		{2, 2, 1, 1, 0, 2, 3, 0, 3, 1, 1, 0, 0, 1},
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		checkUnreachableSound(t, bytesChoice(data))
	})
}

// TestUnreachableIsSound runs the oracle of FuzzUnreachableIsSound on
// functions built from a fixed sequence of random choices, so every run
// of the tests tries them, and checks that the checker accepts some
// function holding an unreachable among them, and some holding a
// fallthrough as well, so the oracle is not vacuous.
func TestUnreachableIsSound(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 29))
	accepted, fell := 0, 0
	for range 3000 {
		a, f := checkUnreachableSound(t, func(n int) int { return rng.IntN(n) })
		if a {
			accepted++
		}
		if f {
			fell++
		}
	}
	if accepted == 0 || fell == 0 {
		t.Fatalf("the checker accepted %d generated functions holding an unreachable, %d of them "+
			"with a fallthrough, want some of each", accepted, fell)
	}
	t.Logf("%d accepted functions hold an unreachable, %d of them a fallthrough too", accepted, fell)
}

// TestUnreachableAfterLateValueIsSound puts through the oracle of
// FuzzUnreachableIsSound a switch whose default, written first, falls
// into a case holding an unreachable that a fact from before the switch,
// m >= 2, would prove, while a case written after both sets m to 0 as its
// value is tried. The default runs only after that value, and so does the
// case it falls into, where m is then 0: the checker must not accept it.
func TestUnreachableAfterLateValueIsSound(t *testing.T) {
	sw := &uStmt{kind: uSwitch, v: uX, cases: []uCase{
		{falls: true},
		{
			values: []int64{1}, pins: []bool{false},
			body: []*uStmt{{
				kind: uIf,
				cond: &uCond{op: "<", v: uM, k: 2},
				then: []*uStmt{{kind: uUnreachable}},
			}},
		},
		{values: []int64{0}, pins: []bool{true}, body: []*uStmt{{kind: uReturn}}},
	}}
	body := []*uStmt{{kind: uIf, cond: &uCond{op: ">=", v: uM, k: 2}, then: []*uStmt{sw}}}
	src := uSource(body)
	if !reachesUnreachable(body) {
		t.Fatalf("no run reaches the unreachable, so the test proves nothing:\n%s", src)
	}
	if _, diags := checkTree(t, src); len(diags) == 0 {
		t.Fatalf("accepted, but a run reaches an unreachable:\n%s", src)
	}
}
