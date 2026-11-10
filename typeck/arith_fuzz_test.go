package typeck_test

import (
	"fmt"
	"math/big"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// The tests here hunt for arithmetic the checker bounds wrongly: an
// index, a divisor or a conversion's operand that is +, -, * or unary -
// on two parameters x and y and known values, nested up to three deep and
// naming at least one parameter, which the checker accepts although some
// values the guard allows put it out of range, or at zero. An index reads
// an array of a known length, or a view v whose length the guard bounds
// and may compare with x and y, perhaps less a known amount, so that the
// relations in force bound x + c against len(v); a view's index may also
// be len(v) - c, or len(w) - c for a second view w, alone, under a guard
// of comparisons of the two lengths and known values only, which names
// neither parameter. Both parameters have one
// integer type T, since arithmetic takes two operands of one type: int8,
// uint8, int16, uint16, int32, uint32, int64, uint64 or unsafe_size_t.
// The guard is an if around the use, or an early exit before it. Perhaps
// x is copied into a mutable m, which the guard and the use name in its
// place, and m is assigned, or passed as mut to a function that assigns
// it, before the use, ending its facts, or after it, ending nothing.
//
// The oracle is exact because it tries every value the guard can allow.
// The guard is a conjunction whose first part, the window, bounds each
// parameter of a type wider than 8 bits to at most 25 values near an edge
// of its type, near a power of two or near a square root of one, where
// arithmetic wraps; an 8-bit parameter may have a window or none, and has
// only 256 values anyway. For a view, x and y always have windows, and
// the guard bounds len(v) to at most 13 lengths below 21. The rest of the
// guard, comparisons of parameters and len(v), perhaps plus or minus a
// known value, with each other and with known values, joined by &&, ||
// and !, can only narrow that. So every value of x, of y and of len(v) the
// guard can hold for is in its window, or the 8-bit type, and the oracle
// runs the guard and the use on each of them, on a target whose
// target-width types are 32 bits wide and on one where they are 64,
// computing each operation exactly, then wrapped to T's width at that
// target, as +% would: a program the checker accepts has no operation
// that overflows, which it must prove or reject, so those are its values. A run the guard holds for whose use is out of
// range is a counterexample, and the checker must reject every use that
// has one. It may reject a use that has none, which is conservatism.

// arithTypes are the types T the parameters have.
var arithTypes = []string{
	"int8", "uint8", "int16", "uint16", "int32", "uint32", "int64", "uint64", "unsafe_size_t",
}

// arithCenters are the values a window is put around, those T holds on
// every target being used.
var arithCenters = bigs(
	"-9223372036854775808", "-4611686018427387904", "-3037000499", "-2147483648",
	"-65536", "-46341", "-32768", "-181", "-128", "-12", "0", "11", "127", "128",
	"181", "255", "256", "32767", "46340", "65535", "65536", "2147483647",
	"2147483648", "3037000499", "4294967295", "4611686018427387903",
	"9223372036854775807", "9223372036854775808", "18446744073709551615",
)

// arithSmallCenters are the centers of the windows of x and y for a
// view's index, near the lengths a view is given.
var arithSmallCenters = bigs("-12", "-1", "0", "1", "3", "11", "16", "20")

// arithConsts are the known values the arithmetic and the guard use,
// those T holds on every target being used.
var arithConsts = bigs(
	"0", "1", "2", "3", "5", "7", "12", "100", "127", "128", "255", "256",
	"32767", "65536", "2147483647", "2147483648", "4294967295",
	"9223372036854775807", "-1", "-2", "-100", "-128", "-32768", "-2147483648",
)

// arithLens are the lengths of the array an index reads.
var arithLens = []int64{1, 2, 3, 4, 8, 16, 100, 256}

// The uses of the arithmetic.
const (
	useIndex   = iota // a[e]
	useDivisor        // 1 / (e)
	useConvert        // sink(U(e))
	useView           // v[e], v a view whose length the guard bounds
	arithUses
)

// The kinds of arithmetic node.
const (
	arithVar = iota // x, or m in its place, or y
	arithConst
	arithAdd
	arithSub
	arithMul
	arithNeg
	arithLen // len(v), or len(w) for v 1, less c when c is not nil
)

// arithExpr is arithmetic: a parameter, v 0 for x and 1 for y, a known
// value c, len(v) less c, or op applied to a, and to b for a binary.
type arithExpr struct {
	a, b *arithExpr
	c    *big.Int
	op   int
	v    int
}

// arithCmp is a comparison in the guard, of l, a parameter or a known
// value, perhaps plus or minus k, with r, by op.
type arithCmp struct {
	l, r *arithExpr
	op   string
}

// arithCond is the guard's part past its windows: a comparison, or a and
// b joined by && or ||, or a negated.
type arithCond struct {
	a, b *arithCond
	cmp  arithCmp
	kind int
}

// arithProgram is a generated function: the parameters' type typ, the
// windows lo to hi of each parameter that has one, the rest of the guard,
// written as an early exit when exit is set, the arithmetic e, used as
// use says, into an array of n elements or converted to the type to, and
// a write to m, x's copy, of wval, before the use or, when after, after
// it.
type arithProgram struct {
	cond        *arithCond
	e           *arithExpr
	lo, hi      [2]*big.Int
	wval        *big.Int
	typ, to     string
	n           int64
	use, write  int
	nlo, nhi    int64 // the lengths of v the guard allows, for a view
	wlo, whi    int64 // the lengths of w the guard allows, when lens
	exit, after bool
	windowed    [2]bool
	copied      bool
	lens        bool // the index is a length less c, the guard on lengths only
}

// signedType reports whether typ is a signed integer type.
func signedType(typ string) bool {
	return typ[0] == 'i'
}

// portable reports whether v is a value typ holds on every target, as
// a constant of typ must be.
func portable(v *big.Int, typ string) bool {
	lo, hi := typeRange(typ, 32)
	if typ != "unsafe_size_t" {
		lo, hi = typeRange(typ, 64)
	}
	return v.Cmp(lo) >= 0 && v.Cmp(hi) <= 0
}

// pick returns a value of vals that typ holds on every target, chosen by
// r, or 0 when none is.
func (r *convReader) pick(vals []*big.Int, typ string) *big.Int {
	var fit []*big.Int
	for _, v := range vals {
		if portable(v, typ) {
			fit = append(fit, v)
		}
	}
	if len(fit) == 0 {
		return big.NewInt(0)
	}
	return fit[r.next(len(fit))]
}

// expr reads arithmetic at most depth deep, over the type typ, len(v)
// among its leaves when lens is set.
func (r *convReader) expr(typ string, depth int, lens bool) *arithExpr {
	choice := r.next(8)
	if depth == 0 || choice < 3 {
		switch {
		case choice == 2:
			return &arithExpr{op: arithConst, c: r.pick(arithConsts, typ)}
		case lens && choice == 1:
			return &arithExpr{op: arithLen}
		}
		return &arithExpr{op: arithVar, v: r.next(2)}
	}
	ops := []int{arithAdd, arithSub, arithMul, arithNeg}
	if !signedType(typ) {
		ops = ops[:3]
	}
	e := &arithExpr{op: ops[r.next(len(ops))], a: r.expr(typ, depth-1, lens)}
	if e.op != arithNeg {
		e.b = r.expr(typ, depth-1, lens)
	}
	return e
}

// guardTerm reads a side of a comparison in the guard: a parameter,
// perhaps plus or minus a known value that is not negative, or a known
// value, or, when lens is set, len(v), perhaps less a known value.
func (r *convReader) guardTerm(typ string, lens bool) *arithExpr {
	if lens && r.next(3) == 0 {
		e := &arithExpr{op: arithLen}
		if k := r.next(4); k > 0 {
			e.c = big.NewInt(int64(k))
		}
		return e
	}
	switch r.next(4) {
	case 0:
		return &arithExpr{op: arithConst, c: r.pick(arithConsts, typ)}
	case 1:
		k := r.pick(arithConsts[:17], typ)
		op := arithAdd
		if r.next(2) == 0 {
			op = arithSub
		}
		return &arithExpr{
			op: op, a: &arithExpr{op: arithVar, v: r.next(2)},
			b: &arithExpr{op: arithConst, c: k},
		}
	}
	return &arithExpr{op: arithVar, v: r.next(2)}
}

// guardCond reads n comparisons joined by &&, more often, or ||, each
// perhaps negated.
func (r *convReader) guardCond(typ string, n int, lens bool) *arithCond {
	c := &arithCond{kind: condLeaf, cmp: arithCmp{
		l: r.guardTerm(typ, lens), op: fuzzOps[r.next(len(fuzzOps))], r: r.guardTerm(typ, lens),
	}}
	if r.next(5) == 0 {
		c = &arithCond{kind: condNot, a: c}
	}
	if n == 1 {
		return c
	}
	kind := condAnd
	if r.next(3) == 0 {
		kind = condOr
	}
	return &arithCond{kind: kind, a: c, b: r.guardCond(typ, n-1, lens)}
}

// window reads the window of parameter v of p, around one of centers,
// within p's type on every target.
func (r *convReader) window(p *arithProgram, v int, centers []*big.Int) {
	p.windowed[v] = true
	c := r.pick(centers, p.typ)
	lo := new(big.Int).Sub(c, big.NewInt(int64(r.next(13))))
	hi := new(big.Int).Add(c, big.NewInt(int64(r.next(13))))
	tlo, thi := typeRange(p.typ, 64)
	if p.typ == "unsafe_size_t" {
		tlo, thi = typeRange(p.typ, 32)
	}
	p.lo[v], p.hi[v] = maxBig(lo, tlo), minBig(hi, thi)
}

// arithmetic reads the arithmetic a program over the type typ uses,
// naming a parameter, or len(v) when view is set.
func (r *convReader) arithmetic(typ string, view bool) *arithExpr {
	e := r.expr(typ, 1+r.next(3), view && typ == "int64")
	if !e.hasVar() {
		// Arithmetic on known values alone is a constant expression,
		// computed exactly, with no type to wrap in, which the oracle
		// does not model; and it is no arithmetic on a variable.
		e = &arithExpr{op: arithAdd, a: e, b: &arithExpr{op: arithVar}}
	}
	return e
}

// lenTerm reads a side of a comparison in a lengths-only guard: len(v) or
// len(w), perhaps less a known value from 1 to 3, or a known length.
func (r *convReader) lenTerm() *arithExpr {
	if r.next(4) == 0 {
		return &arithExpr{op: arithConst, c: big.NewInt(int64(r.next(21)))}
	}
	e := &arithExpr{op: arithLen, v: r.next(2)}
	if k := r.next(4); k > 0 {
		e.c = big.NewInt(int64(k))
	}
	return e
}

// lensCond reads n comparisons of lengths joined by &&, more often, or
// ||, each perhaps negated.
func (r *convReader) lensCond(n int) *arithCond {
	c := &arithCond{kind: condLeaf, cmp: arithCmp{
		l: r.lenTerm(), op: fuzzOps[r.next(len(fuzzOps))], r: r.lenTerm(),
	}}
	if r.next(5) == 0 {
		c = &arithCond{kind: condNot, a: c}
	}
	if n == 1 {
		return c
	}
	kind := condAnd
	if r.next(3) == 0 {
		kind = condOr
	}
	return &arithCond{kind: kind, a: c, b: r.lensCond(n - 1)}
}

// readLens fills in p, an index of a view, as a lengths-only program: w's
// lengths, a guard comparing lengths, and the index len(v) - c or
// len(w) - c, c from 0 to 3, which names neither parameter, so the
// bounds of the lengths alone, below and above, decide it. The index is
// an int64, which value wraps to T instead; that changes no verdict, as a
// length below 21 less at most 3 lies in every T, but for a negative
// value in an unsigned T, which wraps past every length, out of range
// either way.
func (r *convReader) readLens(p *arithProgram) {
	p.lens = true
	p.wlo = int64(r.next(8))
	p.whi = p.wlo + int64(r.next(13))
	if r.next(4) != 0 {
		p.cond = r.lensCond(1 + r.next(2))
	}
	p.e = &arithExpr{
		op: arithSub, a: &arithExpr{op: arithLen, v: r.next(2)},
		b: &arithExpr{op: arithConst, c: big.NewInt(int64(r.next(4)))},
	}
}

// readArithProgram reads a program from data (see the top of this file).
func readArithProgram(data []byte) arithProgram {
	r := &convReader{data: data}
	p := arithProgram{typ: arithTypes[r.next(len(arithTypes))], use: r.next(arithUses)}
	bits, _ := typeBits(p.typ, 64)
	view := p.use == useView
	centers := arithCenters
	if view {
		centers = arithSmallCenters
		p.nlo = int64(r.next(8))
		p.nhi = p.nlo + int64(r.next(13))
		if r.next(4) == 0 {
			r.readLens(&p)
			p.exit = r.next(3) == 0
			return p
		}
	}
	for v := range 2 {
		if bits == 8 && !view && r.next(2) == 0 {
			continue
		}
		r.window(&p, v, centers)
	}
	if r.next(3) != 0 {
		p.cond = r.guardCond(p.typ, 1+r.next(2), view)
	}
	p.e = r.arithmetic(p.typ, view)
	p.n = arithLens[r.next(len(arithLens))]
	p.to = convTargets[r.next(len(convTargets))]
	p.exit = r.next(3) == 0
	if r.next(3) == 0 {
		p.copied = true
		p.write = r.next(3)
		p.wval = r.pick(arithConsts, p.typ)
		p.after = r.next(2) == 0
	}
	return p
}

// hasVar reports whether e names a parameter.
func (e *arithExpr) hasVar() bool {
	if e == nil {
		return false
	}
	return e.op == arithVar || e.a.hasVar() || e.b.hasVar()
}

// maxBig returns the greater of a and b.
func maxBig(a, b *big.Int) *big.Int {
	if a.Cmp(b) >= 0 {
		return a
	}
	return b
}

// minBig returns the lesser of a and b.
func minBig(a, b *big.Int) *big.Int {
	if a.Cmp(b) <= 0 {
		return a
	}
	return b
}

// name returns how p writes the parameter v: x as m when p copies it.
func (p arithProgram) name(v int) string {
	if v == 0 && p.copied {
		return "m"
	}
	return [2]string{"x", "y"}[v]
}

// text writes e as Tack in p, every operation in parentheses.
func (p arithProgram) text(e *arithExpr) string {
	switch e.op {
	case arithVar:
		return p.name(e.v)
	case arithConst:
		if e.c.Sign() < 0 {
			return "(" + e.c.String() + ")"
		}
		return e.c.String()
	case arithNeg:
		return "-(" + p.text(e.a) + ")"
	case arithLen:
		name := [2]string{"len(v)", "len(w)"}[e.v]
		if e.c == nil {
			return name
		}
		return "(" + name + " - " + e.c.String() + ")"
	}
	op := map[int]string{arithAdd: " + ", arithSub: " - ", arithMul: " * "}[e.op]
	return "(" + p.text(e.a) + op + p.text(e.b) + ")"
}

// condText writes the whole guard: each window, then the rest.
func (p arithProgram) condText() string {
	var parts []string
	for v := range 2 {
		if p.windowed[v] {
			parts = append(parts, p.lo[v].String()+" <= "+p.name(v)+" && "+
				p.name(v)+" <= "+p.hi[v].String())
		}
	}
	if p.use == useView {
		parts = append(parts, fmt.Sprintf("%d <= len(v) && len(v) <= %d", p.nlo, p.nhi))
	}
	if p.lens {
		parts = append(parts, fmt.Sprintf("%d <= len(w) && len(w) <= %d", p.wlo, p.whi))
	}
	if p.cond != nil {
		parts = append(parts, "("+p.restText(p.cond)+")")
	}
	if len(parts) == 0 {
		return "true"
	}
	return strings.Join(parts, " && ")
}

// restText writes c as Tack.
func (p arithProgram) restText(c *arithCond) string {
	switch c.kind {
	case condLeaf:
		return p.text(c.cmp.l) + " " + c.cmp.op + " " + p.text(c.cmp.r)
	case condNot:
		return "!(" + p.restText(c.a) + ")"
	case condAnd:
		return "(" + p.restText(c.a) + ") && (" + p.restText(c.b) + ")"
	}
	return "(" + p.restText(c.a) + ") || (" + p.restText(c.b) + ")"
}

// topText writes e as Tack in p, as text does, but with no parentheses
// around the whole of it.
func (p arithProgram) topText(e *arithExpr) string {
	t := p.text(e)
	if e.op != arithVar && e.op != arithConst && e.op != arithNeg && e.op != arithLen {
		t = t[1 : len(t)-1]
	}
	return t
}

// useText writes the use of e, the text its error covers between @ and
// $.
func (p arithProgram) useText() string {
	switch p.use {
	case useIndex:
		return "if a[@" + p.topText(p.e) + "$] == 0 {\n}"
	case useDivisor:
		return "use(1 / (@" + p.topText(p.e) + "$))"
	case useView:
		return "if v[@" + p.topText(p.e) + "$] == 0 {\n}"
	}
	return "sink(@" + p.to + "(" + p.topText(p.e) + ")$)"
}

// source writes p as a Tack program, and returns the offsets its use's
// error must lie between.
func (p arithProgram) source() (string, int, int) {
	var src strings.Builder
	src.WriteString(p.header())
	opening, indent, closing := p.guardParts()
	src.WriteString(opening)
	for _, line := range p.bodyLines() {
		src.WriteString(indent + strings.ReplaceAll(line, "\n", "\n"+indent) + "\n")
	}
	src.WriteString(closing + "}\n\nfunc main() {\n}\n")
	out := src.String()
	at := strings.Index(out, "@")
	out = out[:at] + out[at+1:]
	end := strings.Index(out, "$")
	return out[:end] + out[end+1:], at, end
}

// header writes p up to its guard: the functions f calls, and f's
// start, which reads x and y, declares a when p indexes it, and copies x
// into m when p does.
func (p arithProgram) header() string {
	var src strings.Builder
	src.WriteString("package main\n\nfunc use(v " + p.typ + ") {\n}\n\n")
	if p.use == useConvert {
		src.WriteString("func sink(v " + p.to + ") {\n}\n\n")
	}
	if p.write == writeMut {
		src.WriteString(
			"func clobber(mut v " + p.typ + ") {\n    v = " + p.wval.String() + "\n}\n\n",
		)
	}
	params := "x " + p.typ + ", y " + p.typ
	if p.use == useView {
		params += ", v []int64"
	}
	if p.lens {
		params += ", w []int64"
	}
	src.WriteString("func f(" + params + ") {\n    use(x)\n    use(y)\n")
	if p.use == useIndex {
		src.WriteString("    a := [" + strconv.FormatInt(p.n, 10) + "]int64{" +
			strings.Repeat("0, ", int(p.n)-1) + "0}\n")
	}
	if p.copied {
		src.WriteString("    mut m := x\n    use(m)\n")
	}
	return src.String()
}

// guardParts returns p's guard as written before the statements it
// guards, the indent of those statements, and what closes the guard
// after them: an if around them, or an early exit before them.
func (p arithProgram) guardParts() (opening, indent, closing string) {
	if p.exit {
		return "    if !(" + p.condText() + ") {\n        return\n    }\n", "    ", ""
	}
	return "    if " + p.condText() + " {\n", "        ", "    }\n"
}

// bodyLines returns the statements the guard guards: the write to m
// before the use or after it, and the use.
func (p arithProgram) bodyLines() []string {
	write := ""
	switch p.write {
	case writeAssign:
		write = "m = " + p.wvalText()
	case writeMut:
		write = "clobber(mut m)"
	}
	if write == "" {
		return []string{p.useText()}
	}
	if p.after {
		return []string{p.useText(), write}
	}
	return []string{write, p.useText()}
}

// wvalText writes the value the write to m stores.
func (p arithProgram) wvalText() string {
	if p.wval.Sign() < 0 {
		return "(" + p.wval.String() + ")"
	}
	return p.wval.String()
}

// arithRun is one run of a program: the values of x and y, of m when x
// is copied, the length of v, and the width of a target-width type.
type arithRun struct {
	vals  [2]*big.Int
	m     *big.Int
	n     *big.Int // len(v)
	nw    *big.Int // len(w)
	width int
}

// value returns e's value in run, each operation computed exactly and
// wrapped to p's type, as Tack computes it.
func (p arithProgram) value(run *arithRun, e *arithExpr) *big.Int {
	switch e.op {
	case arithVar, arithConst, arithLen:
		return leafValue(run, e)
	case arithNeg:
		return wrapTo(new(big.Int).Neg(p.value(run, e.a)), p.typ, run.width)
	}
	a, b := p.value(run, e.a), p.value(run, e.b)
	v := new(big.Int)
	switch e.op {
	case arithAdd:
		v.Add(a, b)
	case arithSub:
		v.Sub(a, b)
	default:
		v.Mul(a, b)
	}
	return wrapTo(v, p.typ, run.width)
}

// leafValue returns the value in run of e, a parameter, a known value or
// len(v) or len(w) less a known value, which never wraps: a length below
// 21 less at most 3.
func leafValue(run *arithRun, e *arithExpr) *big.Int {
	switch {
	case e.op == arithVar && e.v == 0 && run.m != nil:
		return run.m
	case e.op == arithVar:
		return run.vals[e.v]
	case e.op == arithConst:
		return e.c
	}
	n := run.n
	if e.v == 1 {
		n = run.nw
	}
	if e.c == nil {
		return n
	}
	return new(big.Int).Sub(n, e.c)
}

// holds reports whether c holds in run.
func (p arithProgram) holds(run *arithRun, c *arithCond) bool {
	switch c.kind {
	case condLeaf:
		return compares(c.cmp.op, p.value(run, c.cmp.l).Cmp(p.value(run, c.cmp.r)))
	case condNot:
		return !p.holds(run, c.a)
	case condAnd:
		return p.holds(run, c.a) && p.holds(run, c.b)
	}
	return p.holds(run, c.a) || p.holds(run, c.b)
}

// guardHolds reports whether p's whole guard holds in run.
func (p arithProgram) guardHolds(run *arithRun) bool {
	if !p.inWindows(run) {
		return false
	}
	if p.use == useView && (run.n.Int64() < p.nlo || run.n.Int64() > p.nhi) {
		return false
	}
	if p.lens && (run.nw.Int64() < p.wlo || run.nw.Int64() > p.whi) {
		return false
	}
	return p.cond == nil || p.holds(run, p.cond)
}

// inWindows reports whether each parameter with a window lies in it in
// run, m in x's place when x is copied.
func (p arithProgram) inWindows(run *arithRun) bool {
	for v := range 2 {
		x := run.vals[v]
		if v == 0 && run.m != nil {
			x = run.m
		}
		if p.windowed[v] && (x.Cmp(p.lo[v]) < 0 || x.Cmp(p.hi[v]) > 0) {
			return false
		}
	}
	return true
}

// bad reports whether v, the value of p's arithmetic at its use on a
// target whose target-width types are width bits wide, is out of range
// for that use: an index outside the array or the view, whose length is
// n, a zero divisor, or a value the type converted to does not hold.
func (p arithProgram) bad(v, n *big.Int, width int) bool {
	switch p.use {
	case useIndex:
		return v.Sign() < 0 || v.Cmp(big.NewInt(p.n)) >= 0
	case useView:
		return v.Sign() < 0 || v.Cmp(n) >= 0
	case useDivisor:
		return v.Sign() == 0
	}
	lo, hi := typeRange(p.to, width)
	return v.Cmp(lo) < 0 || v.Cmp(hi) > 0
}

// domain returns every value parameter v can have where the guard holds
// on a target whose target-width types are width bits wide: its window,
// within its type there, or every value of its type, which has 8 bits.
func (p arithProgram) domain(v, width int) []*big.Int {
	lo, hi := typeRange(p.typ, width)
	if p.windowed[v] {
		lo, hi = maxBig(lo, p.lo[v]), minBig(hi, p.hi[v])
	}
	var vals []*big.Int
	for x := new(big.Int).Set(lo); x.Cmp(hi) <= 0; x = new(big.Int).Add(x, big.NewInt(1)) {
		vals = append(vals, x)
	}
	return vals
}

// usedVars reports which parameters p's guard or arithmetic names.
func (p arithProgram) usedVars() [2]bool {
	var used [2]bool
	var expr func(e *arithExpr)
	expr = func(e *arithExpr) {
		if e == nil {
			return
		}
		if e.op == arithVar {
			used[e.v] = true
		}
		expr(e.a)
		expr(e.b)
	}
	var cond func(c *arithCond)
	cond = func(c *arithCond) {
		if c == nil {
			return
		}
		if c.kind == condLeaf {
			expr(c.cmp.l)
			expr(c.cmp.r)
		}
		cond(c.a)
		cond(c.b)
	}
	expr(p.e)
	cond(p.cond)
	used[0] = used[0] || p.windowed[0]
	used[1] = used[1] || p.windowed[1]
	return used
}

// counterexample returns a run of p whose guard holds and whose use is
// out of range, described, or "" when there is none, trying every value
// the guard can hold for on each target (see the top of this file): a
// parameter nothing names is tried at 0 alone, since no value of it
// changes anything, and a program with no target-width type on a 64-bit
// target alone, since its every value is the same on both; a program
// with no view is tried at a length of 0 alone, which nothing reads. m,
// when x is copied, holds x's value where the guard is checked, and the
// value written to it at the use when the write comes before it.
func (p arithProgram) counterexample() string {
	widths := []int{64}
	if p.typ == "unsafe_size_t" || p.to == "unsafe_size_t" || p.to == "unsafe_uintptr_t" {
		widths = []int{32, 64}
	}
	used := p.usedVars()
	for _, width := range widths {
		if bad := p.searchWidth(width, used); bad != "" {
			return bad
		}
	}
	return ""
}

// searchWidth is counterexample on a target whose target-width types are
// width bits wide, used saying which parameters anything names.
func (p arithProgram) searchWidth(width int, used [2]bool) string {
	xs, ys := []*big.Int{big.NewInt(0)}, []*big.Int{big.NewInt(0)}
	if used[0] {
		xs = p.domain(0, width)
	}
	if used[1] {
		ys = p.domain(1, width)
	}
	for n := p.nlo; n <= p.nhi; n++ {
		for nw := p.wlo; nw <= p.whi; nw++ {
			lens := [2]*big.Int{big.NewInt(n), big.NewInt(nw)}
			if bad := p.searchLens(xs, ys, lens, width); bad != "" {
				return bad
			}
		}
	}
	return ""
}

// searchLens is searchWidth for the lengths of v and w that lens gives,
// trying each of xs for x and each of ys for y.
func (p arithProgram) searchLens(xs, ys []*big.Int, lens [2]*big.Int, width int) string {
	for _, x := range xs {
		for _, y := range ys {
			if bad := p.runBad(x, y, lens, width); bad != "" {
				return bad
			}
		}
	}
	return ""
}

// runBad returns a description of the run of p with x and y, and the
// lengths of v and w lens gives, on a target whose target-width types are
// width bits wide, when its guard holds and its use is out of range, and
// "" otherwise.
func (p arithProgram) runBad(x, y *big.Int, lens [2]*big.Int, width int) string {
	run := &arithRun{vals: [2]*big.Int{x, y}, n: lens[0], nw: lens[1], width: width}
	n := lens[0]
	if p.copied {
		run.m = x
	}
	if !p.guardHolds(run) {
		return ""
	}
	if p.copied && p.write != writeNone && !p.after {
		run.m = p.wval
	}
	v := p.value(run, p.e)
	if !p.bad(v, n, width) {
		return ""
	}
	return fmt.Sprintf("x = %s, y = %s, len(v) = %s, len(w) = %s, on a %d-bit target, gives %s",
		x, y, n, lens[1], width, v)
}

// mustArith returns p's source alone.
func mustArith(p arithProgram) string {
	src, _, _ := p.source()
	return src
}

// arithVerdict is what checking one program found: whether it was
// checked as a whole, with no error but its use's or an overflow's, and
// whether its use was accepted.
type arithVerdict struct {
	checked, accepted bool
}

// checkArith checks the program p and fails t when the checker accepts
// its use although the oracle finds a counterexample, or reports an error
// other than the one an unproven use gets, at the use, or one saying
// arithmetic in the guard or the use may overflow, which rejects the
// program too: a program accepted has no operation that wraps, so the
// oracle's wrapped values are its exact ones.
func checkArith(t *testing.T, p arithProgram) arithVerdict {
	t.Helper()
	src, at, end := p.source()
	_, diags := checkTree(t, src)
	prefix := map[int]string{
		useIndex: "index ", useDivisor: "divisor ", useConvert: "converting ", useView: "index ",
	}[p.use]
	v := arithVerdict{checked: true, accepted: true}
	for _, d := range diags {
		overflow := strings.Contains(d.Message, " may overflow ") || strings.Contains(d.Message, " may go below 0 in ")
		if !overflow && (d.Span.Start < at || d.Span.End > end || !strings.HasPrefix(d.Message, prefix)) {
			return arithVerdict{}
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

// TestArithmeticOracleFindsWraps checks the oracle on uses that wrap for
// a few values the guard allows, and that the checker rejects them.
func TestArithmeticOracleFindsWraps(t *testing.T) {
	x, y := &arithExpr{op: arithVar}, &arithExpr{op: arithVar, v: 1}
	k := func(s string) *arithExpr {
		v, _ := new(big.Int).SetString(s, 10)
		return &arithExpr{op: arithConst, c: v}
	}
	num := func(s string) *big.Int {
		v, _ := new(big.Int).SetString(s, 10)
		return v
	}
	for _, tt := range []struct {
		name string
		p    arithProgram
	}{
		{"int64 x + 1 at the largest", arithProgram{
			typ: "int64", use: useConvert, to: "uint64", windowed: [2]bool{true},
			lo: [2]*big.Int{num("9223372036854775800")},
			hi: [2]*big.Int{num("9223372036854775807")},
			e:  &arithExpr{op: arithAdd, a: x, b: k("1")},
		}},
		{"int8 -x at the least", arithProgram{
			typ: "int8", use: useConvert, to: "uint8",
			e:    &arithExpr{op: arithNeg, a: x},
			cond: &arithCond{kind: condLeaf, cmp: arithCmp{l: x, op: "<", r: k("0")}},
		}},
		{"unsafe_size_t x * 2 wrapping on 32 bits only", arithProgram{
			typ: "unsafe_size_t", use: useDivisor, windowed: [2]bool{true},
			lo: [2]*big.Int{num("2147483648")},
			hi: [2]*big.Int{num("2147483650")},
			e:  &arithExpr{op: arithMul, a: x, b: k("2")},
		}},
		{"uint8 x * y reaching 0", arithProgram{
			typ: "uint8", use: useDivisor,
			e: &arithExpr{op: arithMul, a: x, b: y},
			cond: &arithCond{
				kind: condAnd,
				a:    &arithCond{kind: condLeaf, cmp: arithCmp{l: x, op: ">", r: k("0")}},
				b:    &arithCond{kind: condLeaf, cmp: arithCmp{l: y, op: ">", r: k("0")}},
			},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if bad := tt.p.counterexample(); bad == "" {
				t.Fatalf("the oracle calls it safe:\n%s", mustArith(tt.p))
			}
			if v := checkArith(t, tt.p); !v.checked || v.accepted {
				t.Fatalf("checked %v, accepted %v:\n%s", v.checked, v.accepted, mustArith(tt.p))
			}
		})
	}
}

// TestArithmeticOracleCallsSafeUsesSafe checks that the oracle finds
// nothing for uses that stay in range, at the edges, and that the checker
// accepts them, so neither passes the tests above by calling everything
// unsafe.
func TestArithmeticOracleCallsSafeUsesSafe(t *testing.T) {
	x, y := &arithExpr{op: arithVar}, &arithExpr{op: arithVar, v: 1}
	k := func(s string) *arithExpr {
		v, _ := new(big.Int).SetString(s, 10)
		return &arithExpr{op: arithConst, c: v}
	}
	num := func(s string) *big.Int {
		v, _ := new(big.Int).SetString(s, 10)
		return v
	}
	for _, tt := range []struct {
		name string
		p    arithProgram
	}{
		{"int64 -(x + 1) for a negative x", arithProgram{
			typ: "int64", use: useConvert, to: "uint64", windowed: [2]bool{true},
			lo: [2]*big.Int{num("-9223372036854775808")}, hi: [2]*big.Int{num("-9223372036854775796")},
			e: &arithExpr{op: arithNeg, a: &arithExpr{op: arithAdd, a: x, b: k("1")}},
		}},
		{"int16 x * y + 9 into 19 elements", arithProgram{
			typ: "int16", use: useIndex, n: 19, windowed: [2]bool{true, true},
			lo: [2]*big.Int{num("-3"), num("-3")}, hi: [2]*big.Int{num("3"), num("3")},
			e: &arithExpr{op: arithAdd, a: &arithExpr{op: arithMul, a: x, b: y}, b: k("9")},
		}},
		{"unsafe_size_t x * 2 up to 2^32 - 2", arithProgram{
			typ: "unsafe_size_t", use: useDivisor, windowed: [2]bool{true},
			lo: [2]*big.Int{num("2147483640")}, hi: [2]*big.Int{num("2147483647")},
			e: &arithExpr{op: arithMul, a: x, b: k("2")},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if bad := tt.p.counterexample(); bad != "" {
				t.Fatalf("the oracle finds %s:\n%s", bad, mustArith(tt.p))
			}
			if v := checkArith(t, tt.p); !v.checked || !v.accepted {
				t.Fatalf("checked %v, accepted %v:\n%s", v.checked, v.accepted, mustArith(tt.p))
			}
		})
	}
}

// arithSeeds is how many programs TestArithmeticRangesAreSound checks,
// and arithRunSize how many of them one parallel subtest checks.
const (
	arithSeeds   = 6000
	arithRunSize = 250
)

// TestArithmeticRangesAreSound checks the programs of the first
// arithSeeds seeds (see checkArith), and that among the ones checked as a
// whole the checker accepts some uses of each kind that some values
// would put out of range without their guard, and rejects others, so the
// test does not pass by everything going one way.
func TestArithmeticRangesAreSound(t *testing.T) {
	var tally arithTally
	t.Run("seeds", func(t *testing.T) {
		for from := uint64(0); from < arithSeeds; from += arithRunSize {
			t.Run(strconv.FormatUint(from, 10), func(t *testing.T) {
				t.Parallel()
				for seed := from; seed < from+arithRunSize; seed++ {
					rng := rand.New(rand.NewPCG(seed, 0x61726974))
					data := make([]byte, 64)
					for i := range data {
						data[i] = byte(rng.Uint32())
					}
					p := readArithProgram(data)
					tally.add(p, checkArith(t, p))
				}
			})
		}
	})
	if !t.Failed() {
		tally.report(t)
	}
}

// arithTally counts what TestArithmeticRangesAreSound found: the programs
// checked as a whole, the uses accepted, and, of each kind, the uses of
// 8-bit arithmetic accepted that some values would put out of range
// without their guard, which only 8-bit types have few enough values to
// find, the view indexes accepted that some values would put out of
// range without the comparisons past the windows, and the indexes of
// lengths only accepted.
type arithTally struct {
	checked, accepted, lens atomic.Int64
	byUse                   [arithUses]atomic.Int64
}

// add counts p, of which checking found v.
func (c *arithTally) add(p arithProgram, v arithVerdict) {
	if v.checked {
		c.checked.Add(1)
	}
	if !v.accepted {
		return
	}
	c.accepted.Add(1)
	if p.lens {
		c.lens.Add(1)
	}
	unguarded := p
	if p.use == useView {
		// The windows and the lengths of v stay: what the rest of the
		// guard adds, the relations with len(v), is what counts.
		unguarded.cond = nil
	} else if bits, _ := typeBits(p.typ, 64); bits != 8 {
		return
	} else {
		unguarded.cond, unguarded.windowed, unguarded.exit = nil, [2]bool{}, false
	}
	if unguarded.counterexample() != "" {
		c.byUse[p.use].Add(1)
	}
}

// report fails t unless some uses were accepted and some rejected, and
// some of each kind accepted by their guard alone, and logs the counts.
func (c *arithTally) report(t *testing.T) {
	t.Helper()
	all, ok := c.checked.Load(), c.accepted.Load()
	uses := make([]int64, arithUses)
	for u := range c.byUse {
		uses[u] = c.byUse[u].Load()
	}
	lens := c.lens.Load()
	if ok == 0 || ok == all || slices.Contains(uses, 0) || lens == 0 {
		t.Fatalf("accepted %d of %d uses, by their guard %v, %d of lengths only; "+
			"the programs prove nothing either way", ok, all, uses, lens)
	}
	t.Logf("accepted %d of %d uses, by their guard (8-bit, and views by their comparisons) %v, "+
		"%d of lengths only", ok, all, uses, lens)
}

// FuzzArithmeticRangesAreSound checks the program its input makes (see
// checkArith).
func FuzzArithmeticRangesAreSound(f *testing.F) {
	for _, s := range [][]byte{
		{6, 2, 0, 3, 1, 1, 2, 0, 3, 0, 1, 0, 2},
		{1, 0, 0, 1, 2, 4, 0, 1, 0, 3, 5, 1},
		{8, 1, 1, 7, 5, 9, 2, 3, 0, 0, 2, 4, 1},
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		checkArith(t, readArithProgram(data))
	})
}
