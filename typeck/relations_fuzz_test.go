package typeck_test

import (
	"fmt"
	"math/big"
	"strings"
	"testing"
)

// The fuzz target here hunts for a program the relations accept whose
// index can still be out of range: it builds a function from the fuzzer's
// bytes, a guard of comparisons between its parameters, len(a) and
// constants, each plus or minus a constant, then perhaps a variable stored
// from one, then an index, and when the checker accepts it, runs the guard
// on every combination of values near the edges of each type, with Tack's
// semantics, arithmetic wrapping at its type's width and comparisons by
// value, asserting the index is in range wherever the guard holds.
//
// A program may also declare a mutable n holding a known value, which its
// terms and its index may name, so that a proof chains through a stored
// value's bounds, and write n another known value before the guard,
// after it, or both, the guard perhaps in a loop of two passes, where a
// write after it reaches the second pass's guard by the back edge. The
// oracle runs the passes as Tack does, n taking each value as written.

// fuzzVar is a parameter of the fuzzed function: its name, its type, and
// the values it is tried with.
type fuzzVar struct {
	name, typ string
	values    []*big.Int
}

// bigs returns vs as big integers.
func bigs(vs ...string) []*big.Int {
	out := make([]*big.Int, len(vs))
	for i, v := range vs {
		out[i], _ = new(big.Int).SetString(v, 10)
	}
	return out
}

// fuzzVars are the fuzzed function's integer parameters, and fuzzLens the
// lengths a is tried with: the largest int64 among them, which no real
// view reaches, but which the checker must allow for all the same.
var (
	fuzzVars = []fuzzVar{
		{
			"x",
			"int64",
			bigs(
				"-9223372036854775808",
				"-9223372036854775807",
				"-129",
				"-2",
				"-1",
				"0",
				"1",
				"2",
				"3",
				"127",
				"128",
				"9223372036854775806",
				"9223372036854775807",
			),
		},
		{
			"y",
			"int64",
			bigs("-9223372036854775808", "-1", "0", "1", "2", "4", "9223372036854775807"),
		},
		{"b", "int8", bigs("-128", "-127", "-1", "0", "1", "2", "126", "127")},
		{"u", "uint64", bigs("0", "1", "2", "3", "9223372036854775807", "9223372036854775808",
			"18446744073709551614", "18446744073709551615")},
	}
	fuzzLens  = bigs("0", "1", "2", "3", "128", "9223372036854775807")
	fuzzConst = []string{"0", "1", "2", "3", "126", "127", "128", "9223372036854775807"}
	fuzzOps   = []string{"<", "<=", ">", ">=", "==", "!="}
)

// fuzzStoredConst are the known values n is given.
var fuzzStoredConst = []string{"-1", "0", "1", "2", "3", "4", "128", "9223372036854775807"}

// fuzzStored is the value of v naming n, the mutable variable holding a
// stored value; its value is the last of the values a term is evaluated
// with (see valueAt).
const fuzzStored = -3

// fuzzTerm is a value of the fuzzed program: a parameter, len(a), n or a
// constant, plus or minus a constant.
type fuzzTerm struct {
	c     string
	v     int // index in fuzzVars, -1 for len(a), -2 for a constant alone, fuzzStored for n
	op    byte
	first bool // the constant written first, c + v
}

// fuzzReader hands out the fuzzer's bytes one at a time, 0 past the end.
type fuzzReader struct {
	data []byte
	at   int
}

// next returns the next byte, modulo n.
func (r *fuzzReader) next(n int) int {
	if r.at >= len(r.data) {
		return 0
	}
	b := r.data[r.at]
	r.at++
	return int(b) % n
}

// term reads a term.
func (r *fuzzReader) term() fuzzTerm {
	t := fuzzTerm{v: r.next(len(fuzzVars)+2) - 2}
	switch r.next(4) {
	case 1:
		t.op = '+'
	case 2:
		t.op = '-'
	case 3:
		t.op, t.first = '+', true
	}
	if t.op != 0 || t.v == -2 {
		t.c = fuzzConst[r.next(len(fuzzConst))]
	}
	if t.v == -2 {
		t.op = 0
	}
	return t
}

// text writes t as Tack.
func (t fuzzTerm) text() string {
	base := "len(a)"
	switch t.v {
	case -2:
		return t.c
	case -1:
	case fuzzStored:
		base = "n"
	default:
		base = fuzzVars[t.v].name
	}
	switch {
	case t.op == 0:
		return base
	case t.first:
		return t.c + " + " + base
	}
	return base + " " + string(t.op) + " " + t.c
}

// typ returns the type t's value has: its parameter's, int64 for len(a),
// and "" for a constant alone, which takes its type from its use.
func (t fuzzTerm) typ() string {
	switch t.v {
	case -2:
		return ""
	case -1, fuzzStored:
		return "int64"
	}
	return fuzzVars[t.v].typ
}

// wrap returns v wrapped to the integer type typ, as Tack's arithmetic does.
func wrap(v *big.Int, typ string) *big.Int {
	bits, signed := 64, true
	switch typ {
	case "int8":
		bits = 8
	case "uint64":
		signed = false
	}
	mod := new(big.Int).Lsh(big.NewInt(1), uint(bits))
	w := new(big.Int).Mod(v, mod)
	if signed && w.Cmp(new(big.Int).Rsh(mod, 1)) >= 0 {
		w.Sub(w, mod)
	}
	return w
}

// valueAt returns t's value given each parameter's value, vals, followed
// by n's, and len(a).
func (t fuzzTerm) valueAt(vals []*big.Int, n *big.Int) *big.Int {
	c, _ := new(big.Int).SetString(t.c, 10)
	if t.v == -2 {
		return c
	}
	base := n
	switch {
	case t.v == fuzzStored:
		base = vals[len(fuzzVars)]
	case t.v >= 0:
		base = vals[t.v]
	}
	switch t.op {
	case '+':
		return wrap(new(big.Int).Add(base, c), t.typ())
	case '-':
		return wrap(new(big.Int).Sub(base, c), t.typ())
	}
	return base
}

// fuzzCmp is a comparison of two terms.
type fuzzCmp struct {
	op   string
	l, r fuzzTerm
}

// holds reports whether c is true given vals and len(a), comparing by
// value.
func (c fuzzCmp) holds(vals []*big.Int, n *big.Int) bool {
	d := c.l.valueAt(vals, n).Cmp(c.r.valueAt(vals, n))
	switch c.op {
	case "<":
		return d < 0
	case "<=":
		return d <= 0
	case ">":
		return d > 0
	case ">=":
		return d >= 0
	case "==":
		return d == 0
	}
	return d != 0
}

// fuzzProgram is a fuzzed function: its guard, the conjunction of cmps,
// the term stored in k when stored is set, and the index, k or the
// parameter index, or n when indexN is set. When mut is set, n is declared
// holding start, written before when before is set and after when after
// is, and the guard is in a loop of two passes when loop is set.
type fuzzProgram struct {
	cmps   []fuzzCmp
	k      fuzzTerm
	stored bool
	index  int

	mut, loop, before, after, indexN bool
	start, beforeV, afterV           string
}

// readProgram reads a program from data.
func readProgram(data []byte) fuzzProgram {
	r := &fuzzReader{data: data}
	var p fuzzProgram
	for range 1 + r.next(4) {
		p.cmps = append(
			p.cmps,
			fuzzCmp{l: r.term(), op: fuzzOps[r.next(len(fuzzOps))], r: r.term()},
		)
	}
	p.stored = r.next(2) == 1
	if p.stored {
		p.k = r.term()
		if p.k.v == -2 {
			p.k.v = -1
		}
	}
	p.index = r.next(len(fuzzVars))
	// The bytes past those of a program with no n read as 0, which
	// leaves n out, so earlier seeds keep their programs.
	if p.mut = r.next(2) == 1; p.mut {
		p.readStored(r)
	}
	return p
}

// readStored reads what p does with n: its known values, where it is
// written, whether the guard is in a loop, and which terms, and whether
// the index, name n.
func (p *fuzzProgram) readStored(r *fuzzReader) {
	p.start = fuzzStoredConst[r.next(len(fuzzStoredConst))]
	shape := r.next(8)
	p.loop, p.before, p.after = shape&1 != 0, shape&2 != 0, shape&4 != 0
	p.beforeV = fuzzStoredConst[r.next(len(fuzzStoredConst))]
	p.afterV = fuzzStoredConst[r.next(len(fuzzStoredConst))]
	for i := range p.cmps {
		for _, t := range []*fuzzTerm{&p.cmps[i].l, &p.cmps[i].r} {
			if r.next(3) == 0 {
				t.v = fuzzStored
			}
		}
	}
	if p.stored && r.next(3) == 0 {
		p.k.v = fuzzStored
	}
	p.indexN = !p.stored && r.next(3) == 0
}

// source writes p as a Tack program.
func (p fuzzProgram) source() string {
	params := make([]string, 0, len(fuzzVars))
	conds := make([]string, 0, len(p.cmps))
	for _, v := range fuzzVars {
		params = append(params, v.name+" "+v.typ)
	}
	for _, c := range p.cmps {
		conds = append(conds, c.l.text()+" "+c.op+" "+c.r.text())
	}
	body := ""
	index := fuzzVars[p.index].name
	if p.stored {
		body = "        k := " + p.k.text() + "\n"
		index = "k"
	}
	if p.indexN {
		index = "n"
	}
	guard := "    if " + strings.Join(conds, " && ") + " {\n" + body +
		"        return a[" + index + "]\n    }\n"
	if !p.mut {
		return "package main\n\nfunc f(a []int64, " + strings.Join(params, ", ") + ") int64 {\n" +
			guard + "    return 0\n}\n\nfunc main() {\n}\n"
	}
	if p.before {
		guard = "    n = " + p.beforeV + "\n" + guard
	}
	if p.after {
		guard += "    n = " + p.afterV + "\n"
	}
	if p.loop {
		guard = "    for range 2 {\n" + guard + "    }\n"
	}
	return "package main\n\nfunc f(a []int64, " + strings.Join(params, ", ") + ") int64 {\n" +
		"    mut n int64 = " + p.start + "\n" + guard + "    return n\n}\n\nfunc main() {\n}\n"
}

// counterexample returns the values for which p's guard holds and its
// index is out of range, or nil when there are none among those tried.
func (p fuzzProgram) counterexample() []*big.Int {
	vals := make([]*big.Int, len(fuzzVars))
	var found []*big.Int
	var try func(i int) bool
	try = func(i int) bool {
		if i < len(vals) {
			for _, v := range fuzzVars[i].values {
				vals[i] = v
				if try(i + 1) {
					return true
				}
			}
			return false
		}
		for _, n := range fuzzLens {
			if p.outOfRange(vals, n) {
				found = append(append([]*big.Int{}, vals...), n)
				return true
			}
		}
		return false
	}
	try(0)
	return found
}

// outOfRange reports whether, for the parameters' values vals and len(a)
// n, p reaches its index with the guard holding and the index out of
// range, running each pass of its loop, n taking each value as written,
// until the guard holds and the function returns.
func (p fuzzProgram) outOfRange(vals []*big.Int, n *big.Int) bool {
	if !p.mut {
		return p.indexOut(append(vals[:len(vals):len(vals)], nil), n)
	}
	env := append(vals[:len(vals):len(vals)], bigs(p.start)[0])
	passes := 1
	if p.loop {
		passes = 2
	}
	for range passes {
		if p.before {
			env[len(fuzzVars)] = bigs(p.beforeV)[0]
		}
		if p.guardHolds(env, n) {
			return p.indexOut(env, n)
		}
		if p.after {
			env[len(fuzzVars)] = bigs(p.afterV)[0]
		}
	}
	return false
}

// guardHolds reports whether p's guard holds for env, the parameters'
// values followed by n's, and len(a) n.
func (p fuzzProgram) guardHolds(env []*big.Int, n *big.Int) bool {
	for _, c := range p.cmps {
		if !c.holds(env, n) {
			return false
		}
	}
	return true
}

// indexOut reports whether p's index is out of range, for env, the
// parameters' values followed by n's, and len(a) n, once its guard holds.
func (p fuzzProgram) indexOut(env []*big.Int, n *big.Int) bool {
	if !p.guardHolds(env, n) {
		return false
	}
	idx := env[p.index]
	switch {
	case p.stored:
		idx = p.k.valueAt(env, n)
	case p.indexN:
		idx = env[len(fuzzVars)]
	}
	return idx.Sign() < 0 || idx.Cmp(n) >= 0
}

// FuzzRelationsAreSound checks that no fuzzed function the checker
// accepts reads its view out of range for any of the values tried.
func FuzzRelationsAreSound(f *testing.F) {
	seeds := [][]byte{
		{0, 1, 0, 0, 1},       // 0 <= x
		{2, 1, 1, 0, 0, 0, 1}, // x + 1 < len(a), the wrap
		{2, 0, 1, 1, 0, 1, 2}, // x < len(a), stored x + 1
		{3, 2, 0, 3, 3, 0},    // y and x
		{4, 0, 3, 1, 1, 0, 0}, // the int8 wrap
		{5, 2, 0, 0, 1, 1, 1, 5, 2, 2, 3},
		{1, 3, 0, 2, 3, 1, 0, 0, 4, 1, 0, 3, 1, 5},
		// 0 <= x && x < n && 2 <= len(a), n stored as 2: written nowhere,
		// written 128 after the guard in a loop, which the back edge
		// brings to the second pass, written 128 before it, and written 2
		// before it and 128 after it in a loop.
		{2, 0, 0, 0, 1, 2, 0, 2, 0, 0, 2, 0, 0, 0, 2, 1, 1, 0, 0, 0, 1, 3, 0, 0, 0, 1, 1, 1, 0, 1, 1, 1},
		{2, 0, 0, 0, 1, 2, 0, 2, 0, 0, 2, 0, 0, 0, 2, 1, 1, 0, 0, 0, 1, 3, 5, 0, 6, 1, 1, 1, 0, 1, 1, 1},
		{2, 0, 0, 0, 1, 2, 0, 2, 0, 0, 2, 0, 0, 0, 2, 1, 1, 0, 0, 0, 1, 3, 2, 6, 0, 1, 1, 1, 0, 1, 1, 1},
		{2, 0, 0, 0, 1, 2, 0, 2, 0, 0, 2, 0, 0, 0, 2, 1, 1, 0, 0, 0, 1, 3, 7, 3, 6, 1, 1, 1, 0, 1, 1, 1},
		// a[n] under 3 <= x && x <= len(a), n stored as 2.
		{1, 0, 0, 3, 1, 2, 0, 2, 0, 1, 1, 0, 0, 0, 1, 3, 0, 0, 0, 1, 1, 1, 1, 0},
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		p := readProgram(data)
		src := p.source()
		if _, diags := checkTree(t, src); len(diags) > 0 {
			return
		}
		if bad := p.counterexample(); bad != nil {
			var named []string
			for i, v := range fuzzVars {
				named = append(named, fmt.Sprintf("%s = %s", v.name, bad[i]))
			}
			named = append(named, "len(a) = "+bad[len(bad)-1].String())
			t.Fatalf("accepted, but out of range for %s:\n%s", strings.Join(named, ", "), src)
		}
	})
}

// TestFuzzWrapMatchesTack checks wrap against the edges of each type, so
// the fuzz target's model of Tack's arithmetic is the one the language
// has.
func TestFuzzWrapMatchesTack(t *testing.T) {
	for _, tt := range []struct {
		v, typ, want string
	}{
		{"9223372036854775808", "int64", "-9223372036854775808"},
		{"-9223372036854775809", "int64", "9223372036854775807"},
		{"128", "int8", "-128"},
		{"-129", "int8", "127"},
		{"-1", "uint64", "18446744073709551615"},
		{"18446744073709551616", "uint64", "0"},
		{"5", "int8", "5"},
	} {
		v, _ := new(big.Int).SetString(tt.v, 10)
		if got := wrap(v, tt.typ).String(); got != tt.want {
			t.Errorf("wrap(%s, %s) = %s, want %s", tt.v, tt.typ, got, tt.want)
		}
	}
}
