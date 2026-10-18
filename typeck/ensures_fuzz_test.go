package typeck_test

import (
	"fmt"
	"math/big"
	"strings"
	"testing"
)

// The fuzz target here hunts for a contract the checker accepts that does
// not hold. It reads a program as FuzzRelationsAreSound does, a guard of
// comparisons and an index, and writes it one of two ways: the guard as
// an early exit ending in exit in a function whose contract is that the
// index is in range, so the function's own proof is on trial; or the
// guard as a function's contract, proven by the same early exit, and a
// caller that calls it and then indexes, perhaps through a stored
// variable, so the facts a call gives are on trial. When the checker
// accepts either, wherever the guard holds the index must be in range,
// for every combination of values tried. A third way, from an audit of
// contracts, puts both on trial at once: the clause and the guard that
// proves it are read apart, and the caller passes its x and y in another
// order, or one of them twice (see permutedProgram).

// contractSource writes p as a Tack program whose contract is on trial,
// the callee's own proof when callee is set, and the facts its caller
// gets from it otherwise.
func (p fuzzProgram) contractSource(callee bool) string {
	params := make([]string, 0, len(fuzzVars))
	args := make([]string, 0, len(fuzzVars))
	conds := make([]string, 0, len(p.cmps))
	for _, v := range fuzzVars {
		params = append(params, v.name+" "+v.typ)
		args = append(args, v.name)
	}
	for _, c := range p.cmps {
		conds = append(conds, c.l.text()+" "+c.op+" "+c.r.text())
	}
	guard := strings.Join(conds, " && ")
	sig := "(a []int64, " + strings.Join(params, ", ") + ")"
	exitUnless := "    if !(" + guard + ") {\n        exit(1)\n    }\n"
	index := fuzzVars[p.index].name
	if callee {
		return "package main\n\nfunc g" + sig + " ensures 0 <= " + index + " && " + index +
			" < len(a) {\n" + exitUnless + "}\n\nfunc main() {\n}\n"
	}
	body := ""
	if p.stored {
		body = "    k := " + p.k.text() + "\n"
		index = "k"
	}
	return "package main\n\nfunc g" + sig + " ensures " + guard + " {\n" + exitUnless + "}\n\n" +
		"func f" + sig + " int64 {\n    g(a, " + strings.Join(args, ", ") + ")\n" + body +
		"    return a[" + index + "]\n}\n\nfunc main() {\n}\n"
}

// permutedProgram is a contract program whose callee's clause and the
// guard its body proves it with are read apart, and whose caller passes,
// for the callee's x and y, which of its own x (0) and y (1) sel says,
// then indexes as the clause program says.
type permutedProgram struct {
	clause, proof fuzzProgram
	sel           [2]int
}

// readPermuted reads a permutedProgram from data: a byte for sel, then
// the clause from the first half of the rest and the proof from the
// second.
func readPermuted(data []byte) permutedProgram {
	var a permutedProgram
	if len(data) > 0 {
		a.sel = [2]int{int(data[0]) % 2, int(data[0]/2) % 2}
		data = data[1:]
	}
	half := len(data) / 2
	a.clause = readProgram(data[:half])
	a.proof = readProgram(data[half:])
	return a
}

// guardText writes p's guard as Tack.
func guardText(p fuzzProgram) string {
	conds := make([]string, 0, len(p.cmps))
	for _, c := range p.cmps {
		conds = append(conds, c.l.text()+" "+c.op+" "+c.r.text())
	}
	return strings.Join(conds, " && ")
}

// source writes a as Tack: g ensures the clause, proven by an early exit
// unless the proof guard holds, and f calls g with its arguments mapped
// and indexes as the clause program says.
func (a permutedProgram) source() string {
	params := make([]string, 0, len(fuzzVars))
	args := make([]string, 0, len(fuzzVars))
	for i, v := range fuzzVars {
		params = append(params, v.name+" "+v.typ)
		name := v.name
		if i < 2 {
			name = fuzzVars[a.sel[i]].name
		}
		args = append(args, name)
	}
	sig := "(a []int64, " + strings.Join(params, ", ") + ")"
	body := ""
	index := fuzzVars[a.clause.index].name
	if a.clause.stored {
		body = "    k := " + a.clause.k.text() + "\n"
		index = "k"
	}
	return "package main\n\nfunc g" + sig + " ensures " + guardText(a.clause) + " {\n" +
		"    if !(" + guardText(a.proof) + ") {\n        exit(1)\n    }\n}\n\n" +
		"func f" + sig + " int64 {\n    g(a, " + strings.Join(args, ", ") + ")\n" + body +
		"    return a[" + index + "]\n}\n\nfunc main() {\n}\n"
}

// counterexample returns caller values for which g returns, its proof
// guard holding for the arguments it is passed, and f's index is out of
// range, or nil.
func (a permutedProgram) counterexample() []*big.Int {
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
		callee := append([]*big.Int{}, vals...)
		callee[0], callee[1] = vals[a.sel[0]], vals[a.sel[1]]
		for _, n := range fuzzLens {
			if a.bad(vals, callee, n) {
				found = append(append([]*big.Int{}, vals...), n)
				return true
			}
		}
		return false
	}
	try(0)
	return found
}

// bad reports whether g returns for the callee's values and len(a) n
// while f's index, from the caller's values, is out of range.
func (a permutedProgram) bad(caller, callee []*big.Int, n *big.Int) bool {
	for _, c := range a.proof.cmps {
		if !c.holds(callee, n) {
			return false
		}
	}
	idx := caller[a.clause.index]
	if a.clause.stored {
		idx = a.clause.k.valueAt(caller, n)
	}
	return idx.Sign() < 0 || idx.Cmp(n) >= 0
}

// FuzzContractsAreSound checks that no fuzzed contract the checker
// accepts, proven in its function or given to a caller, fails to hold
// for any of the values tried. Its first byte picks the way the program
// is written: the callee's proof (0), the caller's facts (1), or both,
// with the caller's arguments permuted (2).
func FuzzContractsAreSound(f *testing.F) {
	seeds := [][]byte{
		{0, 0, 1, 0, 0, 1},       // the callee's 0 <= x
		{0, 1, 0, 0, 1, 0, 0, 1}, // two comparisons
		{1, 0, 1, 0, 0, 1},       // a caller of 0 <= x
		{1, 2, 1, 1, 0, 0, 0, 1}, // x + 1 < len(a), the wrap
		{1, 2, 0, 1, 1, 0, 1, 2}, // x < len(a), stored x + 1
		{1, 4, 0, 3, 1, 1, 0, 0}, // the int8 wrap
		{0, 3, 2, 0, 3, 3, 0},
		{1, 5, 2, 0, 0, 1, 1, 1, 5, 2, 2, 3},
		{2, 0, 0, 1, 0, 0, 1, 0, 1, 0, 0, 1},
		{2, 1, 1, 2, 1, 1, 0, 0, 0, 1, 1, 2, 1, 1, 0, 0, 0, 1},
		{2, 2, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1},
		{2, 3, 1, 2, 0, 1, 1, 0, 1, 2, 1, 2, 0, 1, 1, 0, 1, 2},
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		if data[0]%3 == 2 {
			checkPermuted(t, readPermuted(data[1:]))
			return
		}
		callee := data[0]%3 == 0
		p := readProgram(data[1:])
		if callee {
			p.stored = false
		}
		src := p.contractSource(callee)
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

// TestContractSourcesCheck checks that the fuzz target's programs are
// the ones it means: a guard that proves the index gives a program the
// checker accepts, both ways, so the target does not pass by having
// everything rejected.
func TestContractSourcesCheck(t *testing.T) {
	p := fuzzProgram{
		cmps: []fuzzCmp{
			{l: fuzzTerm{v: 0}, op: ">=", r: fuzzTerm{v: -2, c: "0"}},
			{l: fuzzTerm{v: 0}, op: "<", r: fuzzTerm{v: -1}},
		},
	}
	for _, callee := range []bool{true, false} {
		src := p.contractSource(callee)
		if _, diags := checkTree(t, src); len(diags) > 0 {
			t.Errorf("callee %v: rejected %v:\n%s", callee, diags, src)
		}
	}
	p.cmps = p.cmps[:1]
	for _, callee := range []bool{true, false} {
		src := p.contractSource(callee)
		if _, diags := checkTree(t, src); len(diags) == 0 {
			t.Errorf("callee %v: accepted an index the guard leaves unproven:\n%s", callee, src)
		}
	}
}

// FuzzPermutedContractsAreSound runs FuzzContractsAreSound's permuted
// mode alone, so a run spends all its time on it.
func FuzzPermutedContractsAreSound(f *testing.F) {
	for _, s := range [][]byte{
		{0, 0, 1, 0, 0, 1, 0, 1, 0, 0, 1},
		{1, 1, 2, 1, 1, 0, 0, 0, 1, 1, 2, 1, 1, 0, 0, 0, 1},
		{2, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1},
		{3, 1, 2, 0, 1, 1, 0, 1, 2, 1, 2, 0, 1, 1, 0, 1, 2},
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		checkPermuted(t, readPermuted(data))
	})
}

// checkPermuted fails t when the checker accepts a, a permuted contract
// program, and a tried value takes its caller's index out of range.
func checkPermuted(t *testing.T, a permutedProgram) {
	t.Helper()
	src := a.source()
	if _, diags := checkTree(t, src); len(diags) > 0 {
		return
	}
	if bad := a.counterexample(); bad != nil {
		var named []string
		for i, v := range fuzzVars {
			named = append(named, fmt.Sprintf("%s = %s", v.name, bad[i]))
		}
		named = append(named, "len(a) = "+bad[len(bad)-1].String())
		t.Fatalf("accepted, but out of range for %s:\n%s", strings.Join(named, ", "), src)
	}
}

// TestPermutedSourcesCheck checks that the permuted programs can be
// accepted at all, with the arguments swapped, and are rejected when the
// swap leaves the index unproven, so the mode does not pass by having
// everything rejected.
func TestPermutedSourcesCheck(t *testing.T) {
	inRange := fuzzProgram{cmps: []fuzzCmp{
		{l: fuzzTerm{v: 1}, op: ">=", r: fuzzTerm{v: -2, c: "0"}},
		{l: fuzzTerm{v: 1}, op: "<", r: fuzzTerm{v: -1}},
	}}
	// g's y is the caller's x: the clause speaks of g's y, which is f's x,
	// so f's index must be x for the program to be accepted.
	a := permutedProgram{clause: inRange, proof: inRange, sel: [2]int{1, 0}}
	a.clause.index = 0
	if _, diags := checkTree(t, a.source()); len(diags) > 0 {
		t.Errorf("rejected %v:\n%s", diags, a.source())
	}
	a.clause.index = 1
	if _, diags := checkTree(t, a.source()); len(diags) == 0 {
		t.Errorf("accepted an index the swapped clause leaves unproven:\n%s", a.source())
	}
}
