package typeck_test

import (
	"fmt"
	"math/big"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// The tests here hunt for a plain conversion T(x) the checker accepts
// although x can lie outside T's range there. Each program is a function
// of five integer parameters, of types int64, int32, int8, uint64 and
// unsafe_size_t. A guard, a condition built of comparisons with &&, ||
// and !, each comparison between parameters and known values, each perhaps
// plus or minus a known amount, gives the facts, written one of five ways:
// an if around the conversion, an early exit, a tagless switch whose
// first case leaves, a switch on the converted parameter, or a call to a
// function whose ensures clause is the guard. Then perhaps a variable k is
// stored from the converted parameter plus or minus a known value, and
// converted in its place, or that arithmetic is converted itself, inline,
// with no k, whose value is k's; or the parameter is copied into a mutable m,
// which the guard is about, and m is assigned, or passed as mut to a
// function that assigns it, before the conversion, ending its facts, or
// after it, ending nothing. The conversion is of the parameter, k or m,
// to one of the integer types, passed to a function taking that type.
//
// The oracle is exact: it decides whether any values of the parameters,
// each a value of its type, make the guard hold and the converted value
// fall outside T's range, on a target whose target-width types are 32
// bits wide and on one where they are 64. Every comparison is by value,
// and every term is a parameter, k or 0, plus a known offset, less a
// multiple of its type's modulus for the wrap: exactly one multiple puts
// the value in its type's range, so trying each multiple that can, with
// that range as a constraint, is exact. The guard, with ! pushed down to
// the comparisons, becomes a disjunction of conjunctions, and a != one of
// two cases, < or >, so each case is a conjunction of difference bounds,
// a - b <= w, over integers, satisfiable exactly when the graph of the
// bounds has no negative cycle (see convFeasible). The out-of-range
// condition is two more cases, below T's smallest value or above its
// largest. Each case found satisfiable gives values, which the program is
// then run on, concretely, to confirm the guard holds and the value is out
// of range there, so a wrong encoding cannot pass unseen as a false
// counterexample; and a search over values near the edges of each type
// checks that it finds nothing the exact oracle misses (see
// convEnumerate). The checker may reject a conversion the oracle finds
// safe, which is conservatism; it must reject every one the oracle does
// not.

// convVar is a parameter of a generated function: its name, its type,
// and values near the edges of its type, which the cross-check tries on
// a 64-bit target, and those its type holds on a 32-bit one.
type convVar struct {
	name, typ string
	values    []*big.Int
}

// convVars are the generated function's parameters, convTargets the types
// a conversion converts to, and convConsts the known values the guard
// compares with and adds or subtracts, the last ones only when not
// negative.
var (
	convVars = []convVar{
		{"x", "int64", bigs(
			"-9223372036854775808", "-9223372036854775807", "-2147483649", "-2147483648",
			"-129", "-128", "-1", "0", "1", "127", "128", "255", "256",
			"2147483647", "2147483648", "4294967295", "4294967296",
			"9223372036854775806", "9223372036854775807",
		)},
		{"w", "int32", bigs(
			"-2147483648", "-2147483647", "-129", "-128", "-1", "0", "1", "127", "128",
			"255", "256", "2147483646", "2147483647",
		)},
		{"b", "int8", bigs("-128", "-127", "-1", "0", "1", "126", "127")},
		{"u", "uint64", bigs(
			"0", "1", "127", "128", "255", "256", "2147483647", "2147483648",
			"4294967295", "4294967296", "9223372036854775807", "9223372036854775808",
			"18446744073709551615",
		)},
		{"s", "unsafe_size_t", bigs(
			"0", "1", "255", "256", "4294967294", "4294967295", "4294967296",
			"9223372036854775808", "18446744073709551615",
		)},
	}
	convTargets = []string{
		"int8", "uint8", "int16", "uint16", "int32", "uint32", "int64", "uint64",
		"unsafe_size_t", "unsafe_uintptr_t",
	}
	convConsts = []string{
		"0", "1", "127", "128", "255", "256", "2147483647", "2147483648",
		"4294967295", "4294967296", "9223372036854775807",
		"-1", "-128", "-129", "-2147483648", "-2147483649",
	}
)

// The nodes of the oracle's graph: 0, the constant zero, then each
// parameter, then k.
const (
	convZero  = 0
	convK     = len(convVarsNames) + 1
	convNodes = convK + 1
)

// convVarsNames names the parameters, in the order of convVars, so that
// convK is a constant.
var convVarsNames = [5]string{"x", "w", "b", "u", "s"}

// convTerm is a value of a generated program: a parameter, or k, plus or
// minus a known value, or a known value alone.
type convTerm struct {
	c     string
	v     int // index in convVars, -1 for k, -2 for a known value alone
	op    byte
	first bool // the known value written first, c + v
}

// convReader hands out choices one at a time from the fuzzer's bytes, 0
// past their end.
type convReader struct {
	data []byte
	at   int
}

// next returns the next choice, below n.
func (r *convReader) next(n int) int {
	if r.at >= len(r.data) {
		return 0
	}
	b := r.data[r.at]
	r.at++
	return int(b) % n
}

// term reads a term about a parameter or a known value; k is never one.
func (r *convReader) term() convTerm {
	t := convTerm{v: r.next(len(convVars)+1) - 1}
	if t.v == -1 {
		t.v = -2
	}
	switch r.next(4) {
	case 1:
		t.op = '+'
	case 2:
		t.op = '-'
	case 3:
		t.op, t.first = '+', true
	}
	if t.v == -2 {
		t.op, t.first = 0, false
		t.c = convConsts[r.next(len(convConsts))]
		return t
	}
	if t.op != 0 {
		nonneg := convConsts[:11]
		t.c = nonneg[r.next(len(nonneg))]
	}
	return t
}

// typeBits returns the width of the integer type typ on a target whose
// target-width types are width bits wide, and whether it is signed.
func typeBits(typ string, width int) (int, bool) {
	switch typ {
	case "unsafe_size_t", "unsafe_uintptr_t":
		return width, false
	}
	signed := typ[0] == 'i'
	n, _ := strconv.Atoi(strings.TrimLeft(typ, "uint"))
	return n, signed
}

// typeRange returns the smallest and largest values of typ on a target
// whose target-width types are width bits wide.
func typeRange(typ string, width int) (lo, hi *big.Int) {
	n, signed := typeBits(typ, width)
	one := big.NewInt(1)
	if signed {
		hi = new(big.Int).Sub(new(big.Int).Lsh(one, uint(n-1)), one)
		return new(big.Int).Neg(new(big.Int).Lsh(one, uint(n-1))), hi
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, uint(n)), one)
}

// typeModulus returns 2 to the width of typ on a target whose
// target-width types are width bits wide.
func typeModulus(typ string, width int) *big.Int {
	n, _ := typeBits(typ, width)
	return new(big.Int).Lsh(big.NewInt(1), uint(n))
}

// wrapTo returns v wrapped to typ on a target whose target-width types
// are width bits wide, as Tack's arithmetic does.
func wrapTo(v *big.Int, typ string, width int) *big.Int {
	_, signed := typeBits(typ, width)
	mod := typeModulus(typ, width)
	w := new(big.Int).Mod(v, mod)
	if signed && w.Cmp(new(big.Int).Rsh(mod, 1)) >= 0 {
		w.Sub(w, mod)
	}
	return w
}

// The kinds of a condition.
const (
	condLeaf = iota
	condAnd
	condOr
	condNot
)

// convCond is a condition: a comparison, leaf, of two terms, l op r, or
// a, b joined by && or ||, or a negated.
type convCond struct {
	a, b *convCond
	op   string
	l, r convTerm
	kind int
}

// The ways a guard is written.
const (
	formIf          = iota // if G { body }
	formExit               // if !(G) { return } body
	formSwitch             // switch { case !first: return; case rest: body }
	formValueSwitch        // switch x { case c1, c2: body }
	formEnsures            // need(...) body, need ensuring G
	convForms
)

// The writes to m.
const (
	writeNone   = iota
	writeAssign // m = c
	writeMut    // clobber(mut m), which sets m to c
)

// convProgram is a generated function: its guard, cond, written as form,
// the conversion of the parameter subject to the type to, or of k when
// stored, k being the term k, written in k's place when inline, or of m
// when write is set, a copy of the parameter that cond is about in its
// place, which the write sets to wval, before the conversion or, when
// after, after it.
type convProgram struct {
	cond    *convCond
	wval    *big.Int
	to      string
	k       convTerm
	subject int
	form    int
	write   int
	stored  bool
	inline  bool
	after   bool
}

// leaf reads a comparison. Most compare the converted parameter, perhaps
// plus or minus 1, with a value at or next to an edge of the type it
// converts to, so that guards that just prove the conversion, and ones
// that just fail to, are common; the rest compare any two terms.
func (r *convReader) leaf(p *convProgram) *convCond {
	c := &convCond{kind: condLeaf, l: r.term(), op: fuzzOps[r.next(len(fuzzOps))], r: r.term()}
	if r.next(4) == 0 {
		return c
	}
	c.l = convTerm{v: p.subject}
	switch r.next(4) {
	case 1:
		c.l.op, c.l.c = '+', "1"
	case 2:
		c.l.op, c.l.c = '-', "1"
	}
	edges := convEdges(p.to)
	c.r = convTerm{v: -2, c: edges[r.next(len(edges))]}
	if r.next(2) == 0 {
		c.l, c.r = c.r, c.l
	}
	return c
}

// cond reads a condition of n comparisons: one, perhaps negated, or one
// joined to the rest by &&, more often, or ||, the join perhaps negated
// once, !(A && B), or twice, !(!(A || B)) (see negateJoin).
func (r *convReader) cond(p *convProgram, n int) *convCond {
	c := r.leaf(p)
	if r.next(6) == 0 {
		c = &convCond{kind: condNot, a: c}
	}
	if n == 1 {
		return c
	}
	kind := condAnd
	if r.next(3) == 0 {
		kind = condOr
	}
	return r.negateJoin(&convCond{kind: kind, a: c, b: r.cond(p, n-1)})
}

// negateJoin returns c, a join by && or ||, negated once when the reader
// has handed out a number of choices that is 0 modulo 5, twice when it is
// 1, and as it is otherwise. Choosing by the reader's position, not by a
// choice of its own, leaves every input's program as it was apart from
// the negations, so the seeds keep their meaning.
func (r *convReader) negateJoin(c *convCond) *convCond {
	switch r.at % 5 {
	case 0:
		return &convCond{kind: condNot, a: c}
	case 1:
		return &convCond{kind: condNot, a: &convCond{kind: condNot, a: c}}
	}
	return c
}

// readConvProgram reads a program from data (see the top of this file).
func readConvProgram(data []byte) convProgram {
	r := &convReader{data: data}
	p := convProgram{
		to:      convTargets[r.next(len(convTargets))],
		subject: r.next(len(convVars)),
		form:    r.next(convForms),
	}
	if p.form == formValueSwitch {
		edges := append(convEdges(p.to), convEdges(convVars[p.subject].typ)...)
		for range 1 + r.next(2) {
			c := &convCond{
				kind: condLeaf, l: convTerm{v: p.subject}, op: "==",
				r: convTerm{v: -2, c: edges[r.next(len(edges))]},
			}
			if p.cond == nil {
				p.cond = c
			} else {
				p.cond = &convCond{kind: condOr, a: p.cond, b: c}
			}
		}
	} else {
		p.cond = r.cond(&p, 1+r.next(3))
	}
	switch {
	case r.next(4) == 0:
		p.stored = true
		p.k = r.term()
		p.k.v = p.subject
		// Chosen by the reader's position, as negateJoin chooses, so the
		// seeds keep their meaning.
		p.inline = r.at%2 == 0
	case (p.form == formIf || p.form == formExit) && r.next(2) == 0:
		p.write = writeAssign + r.next(2)
		edges := convEdges(p.to)
		p.wval, _ = new(big.Int).SetString(edges[r.next(len(edges))], 10)
		if p.write == writeMut {
			_, p.wval = typeRange(convVars[p.subject].typ, 32)
		}
		p.after = r.next(3) == 0
	}
	return p
}

// convEdges returns the values at and next to the edges of typ's range,
// on either target for a target-width type, as Tack constants, leaving
// out the ones past what an integer literal can be: below the smallest
// int64, or above the largest uint64.
func convEdges(typ string) []string {
	var edges []string
	one := big.NewInt(1)
	smallest, _ := typeRange("int64", 64)
	_, largest := typeRange("uint64", 64)
	for _, width := range []int{32, 64} {
		lo, hi := typeRange(typ, width)
		for _, v := range []*big.Int{lo, hi} {
			for _, e := range []*big.Int{new(big.Int).Sub(v, one), v, new(big.Int).Add(v, one)} {
				if e.Cmp(smallest) >= 0 && e.Cmp(largest) <= 0 {
					edges = append(edges, e.String())
				}
			}
		}
	}
	return edges
}

// name returns how p writes the parameter v: as m, the copy the guard is
// about, when p writes one, and by its own name otherwise.
func (p convProgram) name(v int) string {
	if p.write != writeNone && v == p.subject {
		return "m"
	}
	return convVars[v].name
}

// text writes t as Tack in p.
func (p convProgram) text(t convTerm) string {
	if t.v == -2 {
		return t.c
	}
	base := "k"
	if t.v >= 0 {
		base = p.name(t.v)
	}
	switch {
	case t.op == 0:
		return base
	case t.first:
		return t.c + " + " + base
	}
	return base + " " + string(t.op) + " " + t.c
}

// condText writes c as Tack in p.
func (p convProgram) condText(c *convCond) string {
	switch c.kind {
	case condLeaf:
		return p.text(c.l) + " " + c.op + " " + p.text(c.r)
	case condNot:
		return "!(" + p.condText(c.a) + ")"
	case condAnd:
		return "(" + p.condText(c.a) + ") && (" + p.condText(c.b) + ")"
	}
	return "(" + p.condText(c.a) + ") || (" + p.condText(c.b) + ")"
}

// convertedName returns what p converts: k, the arithmetic k would hold
// when inline, m or the parameter.
func (p convProgram) convertedName() string {
	switch {
	case p.inline:
		return p.text(p.k)
	case p.stored:
		return "k"
	case p.write != writeNone:
		return "m"
	}
	return convVars[p.subject].name
}

// convParams returns the generated function's parameters, written as
// declared, and as arguments.
func convParams() (params, args string) {
	ps := make([]string, 0, len(convVars))
	as := make([]string, 0, len(convVars))
	for _, v := range convVars {
		ps = append(ps, v.name+" "+v.typ)
		as = append(as, v.name)
	}
	return strings.Join(ps, ", "), strings.Join(as, ", ")
}

// source writes p as a Tack program, and returns the offset of its
// conversion.
func (p convProgram) source() (string, int) {
	params, _ := convParams()
	var src strings.Builder
	src.WriteString("package main\n\nfunc sink(v " + p.to + ") {\n}\n\n")
	if p.write == writeMut {
		src.WriteString("func clobber(mut v " + convVars[p.subject].typ + ") {\n    v = " +
			p.wval.String() + "\n}\n\n")
	}
	if p.form == formEnsures {
		src.WriteString("func need(" + params + ") ensures " + p.condText(p.cond) +
			" {\n    if !(" + p.condText(p.cond) + ") {\n        exit(1)\n    }\n}\n\n")
	}
	src.WriteString("func f(" + params + ") {\n")
	if p.write != writeNone {
		src.WriteString("    mut m := " + convVars[p.subject].name + "\n")
	}
	opening, nested := p.guardText()
	src.WriteString(opening)
	indent := "    "
	if nested {
		indent = "        "
	}
	for _, line := range p.bodyLines() {
		src.WriteString(indent + line + "\n")
	}
	if nested {
		src.WriteString("    }\n")
	}
	src.WriteString("}\n\nfunc main() {\n}\n")
	out := src.String()
	at := strings.Index(out, "@")
	return out[:at] + out[at+1:], at
}

// bodyLines returns the statements the guard guards: k stored, unless
// its arithmetic is converted inline, the write
// to m before or after the conversion, and the conversion, its type
// marked with @, where source finds its offset.
func (p convProgram) bodyLines() []string {
	var body []string
	if p.stored && !p.inline {
		body = append(body, "k := "+p.text(p.k))
	}
	write := ""
	switch p.write {
	case writeAssign:
		write = "m = " + p.wval.String()
	case writeMut:
		write = "clobber(mut m)"
	}
	if write != "" && !p.after {
		body = append(body, write)
	}
	body = append(body, "sink(@"+p.to+"("+p.convertedName()+"))")
	if write != "" && p.after {
		body = append(body, write)
	}
	return body
}

// guardText writes p's guard as its form has it, up to the statements it
// guards, and reports whether they go in a block it opens.
func (p convProgram) guardText() (string, bool) {
	switch p.form {
	case formIf:
		return "    if " + p.condText(p.cond) + " {\n", true
	case formExit:
		return "    if !(" + p.condText(p.cond) + ") {\n        return\n    }\n", false
	case formSwitch:
		if p.cond.kind == condAnd {
			return "    switch {\n    case !(" + p.condText(p.cond.a) + "):\n        return\n" +
				"    case " + p.condText(p.cond.b) + ":\n", true
		}
		return "    switch {\n    case " + p.condText(p.cond) + ":\n", true
	case formValueSwitch:
		var vals []string
		for c := p.cond; ; c = c.a {
			if c.kind == condLeaf {
				vals = append(vals, c.r.c)
				break
			}
			vals = append(vals, c.b.r.c)
		}
		return "    switch " + convVars[p.subject].name + " {\n    case " +
			strings.Join(vals, ", ") + ":\n", true
	}
	_, args := convParams()
	return "    need(" + args + ")\n", false
}

// typeOf returns the type of the parameter or k that t names.
func (p convProgram) typeOf(t convTerm) string {
	if t.v == -1 {
		return convVars[p.k.v].typ
	}
	return convVars[t.v].typ
}

// convRun is the values one run of a program has: k's once stored, each
// parameter's, and the width of a target-width type.
type convRun struct {
	k     *big.Int
	vals  []*big.Int
	width int
}

// value returns t's value in run.
func (p convProgram) value(run *convRun, t convTerm) *big.Int {
	c, _ := new(big.Int).SetString(t.c, 10)
	if t.v == -2 {
		return c
	}
	base := run.k
	if t.v >= 0 {
		base = run.vals[t.v]
	}
	switch t.op {
	case '+':
		return wrapTo(new(big.Int).Add(base, c), p.typeOf(t), run.width)
	case '-':
		return wrapTo(new(big.Int).Sub(base, c), p.typeOf(t), run.width)
	}
	return base
}

// compares reports whether d, the sign of a comparison's left side less
// its right, makes op hold.
func compares(op string, d int) bool {
	switch op {
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

// holds reports whether c is true in run, comparing by value; a nil c is
// no guard, always true.
func (p convProgram) holds(run *convRun, c *convCond) bool {
	if c == nil {
		return true
	}
	switch c.kind {
	case condLeaf:
		return compares(c.op, p.value(run, c.l).Cmp(p.value(run, c.r)))
	case condNot:
		return !p.holds(run, c.a)
	case condAnd:
		return p.holds(run, c.a) && p.holds(run, c.b)
	}
	return p.holds(run, c.a) || p.holds(run, c.b)
}

// converted returns the value p converts in run.
func (p convProgram) converted(run *convRun) *big.Int {
	switch {
	case p.stored:
		run.k = p.value(run, p.k)
		return run.k
	case p.write != writeNone && !p.after:
		return p.wval
	}
	return run.vals[p.subject]
}

// outOfRange returns a description of run when p's guard holds in it and
// its conversion is out of range, and "" otherwise.
func (p convProgram) outOfRange(run *convRun) string {
	x := p.converted(run)
	if !p.holds(run, p.cond) {
		return ""
	}
	if lo, hi := typeRange(p.to, run.width); x.Cmp(lo) >= 0 && x.Cmp(hi) <= 0 {
		return ""
	}
	var named []string
	for i, v := range run.vals {
		if v != nil {
			named = append(named, fmt.Sprintf("%s = %s", convVars[i].name, v))
		}
	}
	return fmt.Sprintf("%s, on a %d-bit target, converts %s",
		strings.Join(named, ", "), run.width, x)
}

// usedVars returns the parameters c names, and the one p converts.
func (p convProgram) usedVars() map[int]bool {
	used := map[int]bool{p.subject: true}
	var note func(c *convCond)
	note = func(c *convCond) {
		if c == nil {
			return
		}
		if c.kind == condLeaf {
			for _, t := range []convTerm{c.l, c.r} {
				if t.v >= 0 {
					used[t.v] = true
				}
			}
		}
		note(c.a)
		note(c.b)
	}
	note(p.cond)
	return used
}

// convEnumerate is the cross-check: it runs p on every combination of the
// edge values of each parameter it names, each that its type holds on
// the target, and returns a description of the first run whose guard
// holds and whose conversion is out of range, or "" when none is.
func (p convProgram) convEnumerate() string {
	used := p.usedVars()
	for _, width := range []int{32, 64} {
		run := &convRun{vals: make([]*big.Int, len(convVars)), width: width}
		if bad := p.enumerateFrom(run, used, 0); bad != "" {
			return bad
		}
	}
	return ""
}

// enumerateFrom is convEnumerate for the parameters from i on, the ones
// before i holding the values run gives them.
func (p convProgram) enumerateFrom(run *convRun, used map[int]bool, i int) string {
	if i == len(convVars) {
		return p.outOfRange(run)
	}
	if !used[i] {
		run.vals[i] = big.NewInt(0)
		return p.enumerateFrom(run, used, i+1)
	}
	lo, hi := typeRange(convVars[i].typ, run.width)
	for _, v := range convVars[i].values {
		if v.Cmp(lo) < 0 || v.Cmp(hi) > 0 {
			continue
		}
		run.vals[i] = v
		if bad := p.enumerateFrom(run, used, i+1); bad != "" {
			return bad
		}
	}
	return ""
}

// convLit is a comparison in a case of the oracle: l op r, op one of <,
// <=, ==, >= and >, a != having been split into < and >.
type convLit struct {
	op   string
	l, r convTerm
}

// negateOp returns the comparison that holds exactly when op does not.
func negateOp(op string) string {
	return map[string]string{"<": ">=", "<=": ">", ">": "<=", ">=": "<", "==": "!=", "!=": "=="}[op]
}

// dnf returns c, negated when neg is set, as a disjunction of
// conjunctions of comparisons, with every != split into < or >. A nil c
// is true: one empty conjunction.
func dnf(c *convCond, neg bool) [][]convLit {
	if c == nil {
		return [][]convLit{nil}
	}
	switch c.kind {
	case condLeaf:
		op := c.op
		if neg {
			op = negateOp(op)
		}
		if op == "!=" {
			return [][]convLit{{{op: "<", l: c.l, r: c.r}}, {{op: ">", l: c.l, r: c.r}}}
		}
		return [][]convLit{{{op: op, l: c.l, r: c.r}}}
	case condNot:
		return dnf(c.a, !neg)
	}
	and := c.kind == condAnd
	if neg {
		and = !and
	}
	a, b := dnf(c.a, neg), dnf(c.b, neg)
	if !and {
		return append(a, b...)
	}
	var out [][]convLit
	for _, x := range a {
		for _, y := range b {
			out = append(out, append(append([]convLit{}, x...), y...))
		}
	}
	return out
}

// convEdge is a difference bound: the value of node v less that of node u
// is at most w.
type convEdge struct {
	w    *big.Int
	u, v int
}

// convSystem is a conjunction of difference bounds being built.
type convSystem struct {
	edges []convEdge
}

// atMost adds the bound v - u <= w.
func (s *convSystem) atMost(v, u int, w *big.Int) {
	s.edges = append(s.edges, convEdge{u: u, v: v, w: new(big.Int).Set(w)})
}

// within adds lo <= node <= hi.
func (s *convSystem) within(node int, lo, hi *big.Int) {
	s.atMost(node, convZero, hi)
	s.atMost(convZero, node, new(big.Int).Neg(lo))
}

// convFeasible reports whether the bounds of s hold together for some
// integer values of the nodes, and returns such values, node 0 being 0:
// Bellman and Ford's shortest paths from a source joined to every node at
// distance 0 are one, unless the graph has a negative cycle, in which
// case none exists.
func convFeasible(s *convSystem) ([]*big.Int, bool) {
	dist := make([]*big.Int, convNodes)
	for i := range dist {
		dist[i] = new(big.Int)
	}
	for range convNodes + 1 {
		changed := false
		for _, e := range s.edges {
			if d := new(big.Int).Add(dist[e.u], e.w); d.Cmp(dist[e.v]) < 0 {
				dist[e.v], changed = d, true
			}
		}
		if !changed {
			zero := dist[convZero]
			for i := range dist {
				dist[i] = new(big.Int).Sub(dist[i], zero)
			}
			return dist, true
		}
	}
	return nil, false
}

// convPart is a term as the oracle reads it: its node, and its offset,
// the known value it adds, less the wrap's multiple of its modulus, which
// each case chooses.
type convPart struct {
	off  *big.Int
	node int
}

// termNode returns t's node and the known value it adds.
func termNode(t convTerm) (int, *big.Int) {
	c, _ := new(big.Int).SetString(t.c, 10)
	if c == nil {
		c = new(big.Int)
	}
	switch {
	case t.v == -2:
		return convZero, c
	case t.op == '-':
		c.Neg(c)
	case t.op == 0:
		c.SetInt64(0)
	}
	if t.v == -1 {
		return convK, c
	}
	return 1 + t.v, c
}

// solver holds what deciding one case of a program needs (see
// solveCase): the program, the terms whose wrap the case chooses, the
// conjunction of comparisons that must hold, the target's width, and
// which side of the range converted to the converted value must fall.
type solver struct {
	p      *convProgram
	terms  []convTerm
	clause []convLit
	width  int
	below  bool
}

// wrapChoices returns the multiples k of t's modulus for which t's node
// plus its known value less k times the modulus can be a value of t's
// type, for some value of the node in that type.
func (sv *solver) wrapChoices(t convTerm) []int64 {
	if t.v == -2 || t.op == 0 {
		return []int64{0}
	}
	_, off := termNode(t)
	typ := sv.p.typeOf(t)
	lo, hi := typeRange(typ, sv.width)
	m := typeModulus(typ, sv.width)
	// lo <= node + off - k*m <= hi for some node in [lo, hi] exactly when
	// (lo + off - hi) / m <= k <= (hi + off - lo) / m, the first rounded
	// up and the second down.
	least := new(big.Int).Sub(new(big.Int).Add(lo, off), hi)
	least.Neg(least).Div(least, m).Neg(least) // ceiling: -floor(-x / m)
	most := new(big.Int).Sub(new(big.Int).Add(hi, off), lo)
	most.Div(most, m) // big.Int.Div rounds toward negative infinity for m > 0
	var ks []int64
	for k := least.Int64(); k <= most.Int64(); k++ {
		ks = append(ks, k)
	}
	return ks
}

// part returns t's part in a case choosing the wrap multiple k, and adds
// to s the bound keeping t's value in its type for that k.
func (sv *solver) part(s *convSystem, t convTerm, k int64) convPart {
	node, off := termNode(t)
	if node == convZero {
		return convPart{node: node, off: off}
	}
	typ := sv.p.typeOf(t)
	km := new(big.Int).Mul(big.NewInt(k), typeModulus(typ, sv.width))
	off.Sub(off, km)
	if t.op != 0 {
		lo, hi := typeRange(typ, sv.width)
		s.within(node, new(big.Int).Sub(lo, off), new(big.Int).Sub(hi, off))
	}
	return convPart{node: node, off: off}
}

// compare adds l op r to s.
func compare(s *convSystem, op string, l, r convPart) {
	// l.node + l.off op r.node + r.off: l.node - r.node op r.off - l.off.
	d := new(big.Int).Sub(r.off, l.off)
	one := big.NewInt(1)
	switch op {
	case "<":
		s.atMost(l.node, r.node, new(big.Int).Sub(d, one))
	case "<=":
		s.atMost(l.node, r.node, d)
	case ">":
		s.atMost(r.node, l.node, new(big.Int).Sub(new(big.Int).Neg(d), one))
	case ">=":
		s.atMost(r.node, l.node, new(big.Int).Neg(d))
	case "==":
		s.atMost(l.node, r.node, d)
		s.atMost(r.node, l.node, new(big.Int).Neg(d))
	}
}

// search tries every choice of wrap for the terms from i on, ks holding
// the choices for the ones before, then decides the case, and returns the
// values of a case that holds, or nil.
func (sv *solver) search(i int, ks []int64) []*big.Int {
	if i == len(sv.terms) {
		if vals, ok := convFeasible(sv.system(ks)); ok {
			return vals
		}
		return nil
	}
	for _, k := range sv.wrapChoices(sv.terms[i]) {
		if vals := sv.search(i+1, append(ks, k)); vals != nil {
			return vals
		}
	}
	return nil
}

// counterexample returns a description of a run of p whose guard holds
// and whose conversion is out of range, or "" when there is none, on
// either target, deciding it exactly (see the top of this file), and
// confirming any run it finds by running p on it.
func (p convProgram) counterexample(t *testing.T) string {
	t.Helper()
	for _, width := range []int{32, 64} {
		for _, clause := range dnf(p.cond, false) {
			for _, below := range []bool{true, false} {
				if vals := p.solveCase(width, clause, below); vals != nil {
					run := &convRun{vals: make([]*big.Int, len(convVars)), width: width}
					for i := range convVars {
						run.vals[i] = vals[1+i]
					}
					bad := p.outOfRange(run)
					if bad == "" {
						t.Fatalf(
							"the oracle's values %v do not make p fail:\n%s",
							vals,
							must(p.source()),
						)
					}
					return bad
				}
			}
		}
	}
	return ""
}

// must returns the program text of source's results.
func must(src string, _ int) string {
	return src
}

// solveCase decides one case of p on the target of the given width: the
// conjunction clause holds, every parameter is a value of its type, k is
// what p stores, and the converted value is below T's range, when below
// is set, or above it. It returns the nodes' values when it holds, and
// nil otherwise.
func (p convProgram) solveCase(width int, clause []convLit, below bool) []*big.Int {
	sv := &solver{p: &p, clause: clause, width: width, below: below}
	for _, l := range clause {
		sv.terms = append(sv.terms, l.l, l.r)
	}
	if p.stored {
		sv.terms = append(sv.terms, p.k)
	}
	if p.write != writeNone && !p.after {
		tlo, thi := typeRange(p.to, width)
		if below && p.wval.Cmp(tlo) >= 0 || !below && p.wval.Cmp(thi) <= 0 {
			return nil
		}
	}
	return sv.search(0, nil)
}

// system returns the bounds of the case with the wrap choices ks, one for
// each of sv.terms: every parameter within its type, each comparison of
// the clause, k equal to what p stores, and, unless a write before the
// conversion fixes the converted value, which solveCase has judged, that
// value on the side of T's range the case asks for.
func (sv *solver) system(ks []int64) *convSystem {
	p := sv.p
	s := &convSystem{}
	for i, v := range convVars {
		lo, hi := typeRange(v.typ, sv.width)
		s.within(1+i, lo, hi)
	}
	for i, l := range sv.clause {
		compare(s, l.op, sv.part(s, l.l, ks[2*i]), sv.part(s, l.r, ks[2*i+1]))
	}
	converted := 1 + p.subject
	if p.stored {
		k := sv.part(s, p.k, ks[len(ks)-1])
		compare(s, "==", convPart{node: convK, off: new(big.Int)}, k)
		converted = convK
	}
	if p.write != writeNone && !p.after {
		return s
	}
	one := big.NewInt(1)
	tlo, thi := typeRange(p.to, sv.width)
	if sv.below {
		s.atMost(converted, convZero, new(big.Int).Sub(tlo, one))
	} else {
		s.atMost(convZero, converted, new(big.Int).Neg(new(big.Int).Add(thi, one)))
	}
	return s
}

// convVerdict is what checkConversion found of one program: whether it
// was checked as a whole, with no error but its conversion's, whether
// the conversion was accepted, and whether it was accepted by the guard,
// the oracle finding it out of range for some values without it.
type convVerdict struct {
	checked, accepted, byGuard bool
}

// checkConversion checks the program data makes and fails t when the
// checker accepts a conversion the oracle finds out of range, reports an
// error at the conversion other than its can-fail one, or the edge-value
// search finds a run the exact oracle does not.
func checkConversion(t *testing.T, data []byte) convVerdict {
	t.Helper()
	return checkProgram(t, readConvProgram(data))
}

// checkProgram is checkConversion for the program p.
func checkProgram(t *testing.T, p convProgram) convVerdict {
	t.Helper()
	src, at := p.source()
	_, diags := checkTree(t, src)
	v := convVerdict{checked: true, accepted: true}
	for _, d := range diags {
		if d.Span.Start != at || !strings.HasPrefix(d.Message, "converting ") {
			return convVerdict{}
		}
		v.accepted = false
	}
	bad := p.counterexample(t)
	if found := p.convEnumerate(); found != "" && bad == "" {
		t.Fatalf("the edge values find %s, which the oracle misses:\n%s", found, src)
	}
	if v.accepted {
		if bad != "" {
			t.Fatalf("accepted, but out of range for %s:\n%s", bad, src)
		}
		unguarded := p
		unguarded.cond = nil
		v.byGuard = unguarded.counterexample(t) != ""
	}
	return v
}

// TestConversionOracleFindsLoneValues checks the oracle on guards that
// allow values no edge of any type is near, which only solving the
// comparisons finds, and that the checker rejects them.
func TestConversionOracleFindsLoneValues(t *testing.T) {
	x := convTerm{v: 0}
	leaf := func(l convTerm, op string, c string) *convCond {
		return &convCond{kind: condLeaf, l: l, op: op, r: convTerm{v: -2, c: c}}
	}
	shifted := convTerm{v: 0, op: '-', c: "2147483648"}
	for _, tt := range []struct {
		name string
		p    convProgram
	}{
		{"x == 300 to uint8", convProgram{to: "uint8", cond: leaf(x, "==", "300")}},
		{"a two-value window to int8", convProgram{to: "int8", cond: &convCond{
			kind: condAnd,
			a:    leaf(shifted, ">=", "127"),
			b:    leaf(shifted, "<=", "128"),
		}}},
		{"a value only a wrap reaches", convProgram{to: "uint8", cond: leaf(
			convTerm{v: 0, op: '+', c: "9223372036854775807"}, "==", "-9223372036854775508")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if bad := tt.p.counterexample(t); bad == "" {
				t.Fatalf("the oracle calls it safe:\n%s", must(tt.p.source()))
			}
			if v := checkProgram(t, tt.p); !v.checked || v.accepted {
				t.Fatalf("checked %v, accepted %v:\n%s", v.checked, v.accepted, must(tt.p.source()))
			}
		})
	}
}

// TestConversionOracleCallsSafeGuardsSafe checks that the oracle finds
// nothing for guards that keep the conversion in range, at the edges
// and through a wrap, so it does not pass the tests above by calling
// everything unsafe.
func TestConversionOracleCallsSafeGuardsSafe(t *testing.T) {
	x := convTerm{v: 0}
	leaf := func(l convTerm, op string, c string) *convCond {
		return &convCond{kind: condLeaf, l: l, op: op, r: convTerm{v: -2, c: c}}
	}
	for _, tt := range []struct {
		name string
		p    convProgram
	}{
		{"0 <= x <= 255 to uint8", convProgram{to: "uint8", cond: &convCond{
			kind: condAnd, a: leaf(x, ">=", "0"), b: leaf(x, "<=", "255"),
		}}},
		{"x == 300 or x == 5, not 300, to uint8", convProgram{to: "uint8", cond: &convCond{
			kind: condAnd,
			a:    &convCond{kind: condOr, a: leaf(x, "==", "300"), b: leaf(x, "==", "5")},
			b:    &convCond{kind: condNot, a: leaf(x, ">", "255")},
		}}},
		{"s to uint32 on every target", convProgram{
			to: "uint32", subject: 4,
			cond: leaf(convTerm{v: 4}, "<=", "4294967295"),
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if bad := tt.p.counterexample(t); bad != "" {
				t.Fatalf("the oracle finds %s:\n%s", bad, must(tt.p.source()))
			}
		})
	}
}

// convSeeds is how many programs TestConversionFactsAreSound checks, and
// convRunSize how many of them one parallel subtest checks.
const (
	convSeeds   = 12000
	convRunSize = 250
)

// TestConversionFactsAreSound checks the programs of the first convSeeds
// seeds (see checkConversion), and that among the ones checked as a
// whole the checker accepts some conversions only their guard proves, in
// every form a guard is written in, and rejects others, so the test does
// not pass by everything going one way.
func TestConversionFactsAreSound(t *testing.T) {
	var tally convTally
	t.Run("seeds", func(t *testing.T) {
		for from := uint64(0); from < convSeeds; from += convRunSize {
			t.Run(strconv.FormatUint(from, 10), func(t *testing.T) {
				t.Parallel()
				for seed := from; seed < from+convRunSize; seed++ {
					rng := rand.New(rand.NewPCG(seed, 0x636f6e76))
					data := make([]byte, 48)
					for i := range data {
						data[i] = byte(rng.Uint32())
					}
					tally.add(readConvProgram(data), checkConversion(t, data))
				}
			})
		}
	})
	if !t.Failed() {
		tally.report(t)
	}
}

// convTally counts what TestConversionFactsAreSound found: the programs
// checked as a whole, the conversions accepted, those accepted by their
// guard, by the form it is written in, those with a write, and those of
// inline arithmetic.
type convTally struct {
	checked, accepted, byGuard, byWrite, byInline atomic.Int64
	byForm                                        [convForms]atomic.Int64
}

// add counts p, of which checking found v.
func (c *convTally) add(p convProgram, v convVerdict) {
	if v.checked {
		c.checked.Add(1)
	}
	if v.accepted {
		c.accepted.Add(1)
	}
	if !v.byGuard {
		return
	}
	c.byGuard.Add(1)
	c.byForm[p.form].Add(1)
	if p.write != writeNone {
		c.byWrite.Add(1)
	}
	if p.inline {
		c.byInline.Add(1)
	}
}

// report fails t unless some conversion was accepted by its guard in
// every form, one with a write among them and one of inline arithmetic,
// and some rejected, and logs the counts.
func (c *convTally) report(t *testing.T) {
	t.Helper()
	all, ok, guarded, writes := c.checked.Load(), c.accepted.Load(), c.byGuard.Load(), c.byWrite.Load()
	inline := c.byInline.Load()
	if guarded == 0 || ok == all || writes == 0 || inline == 0 {
		t.Fatalf("accepted %d of %d conversions, %d by their guard, %d with a write, %d inline; "+
			"the programs prove nothing either way", ok, all, guarded, writes, inline)
	}
	forms := make([]int64, convForms)
	for form := range c.byForm {
		forms[form] = c.byForm[form].Load()
		if forms[form] == 0 {
			t.Fatalf("no conversion is accepted by a guard written as form %d", form)
		}
	}
	t.Logf(
		"accepted %d of %d conversions, %d by their guard, by form %v, %d with a write, %d inline",
		ok,
		all,
		guarded,
		forms,
		writes,
		inline,
	)
}

// FuzzConversionFactsAreSound checks the program its input makes (see
// checkConversion).
func FuzzConversionFactsAreSound(f *testing.F) {
	seeds := [][]byte{
		{0, 1, 0, 4, 7, 1, 7, 0, 0},
		{1, 4, 1, 0, 1, 0, 0, 7, 1, 1, 4},
		{0, 4, 2, 1, 4, 0, 4, 0, 0, 3},
		{2, 1, 3, 1, 0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 2, 0, 5},
		{4, 0, 4, 1, 1, 0, 1, 2, 3},
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		checkConversion(t, data)
	})
}
