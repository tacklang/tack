package typeck_test

import (
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"strings"
	"testing"
)

// The tests here hunt for arithmetic the checker accepts although it can
// overflow: a function of two parameters x and y of one integer type T
// guards, with an if, either a value computed from them, + - * on two
// operands and unary - on a signed one, nested up to three deep over x, y
// and known values, and sometimes divided by, or dividing, more of it, or
// a step of a mutable copy of x, m++, m--, or m +=, -=, *= or /= of y or
// a known value. The guard is a conjunction of comparisons of x and y
// with known values and with each other. A division fails as overflowing
// does when its divisor is zero, and when it is T's smallest value
// divided by -1.
//
// The oracle is exact because it tries every value the guard can allow.
// The guard begins with a window on each parameter of a type wider than
// 8 bits, at most 25 values around an edge of its type, a power of two or
// a square root of one, where arithmetic overflows; an 8-bit parameter
// may have one or none, and has 256 values anyway. The rest of the guard
// can only narrow that. For each pair the guard holds for, the oracle
// computes each operation exactly and asks whether it is a value of T, on
// a target whose target-width type is 32 bits wide and on one where it
// is 64. An accepted program with a pair that leaves T at any step is a
// counterexample. The checker may reject a program that has none, which
// is conservatism, but every error it gives must be the overflow error.

// overflowTypes are the types T the parameters have.
var overflowTypes = []string{
	"int8", "uint8", "int16", "uint16", "int32", "uint32", "int64", "uint64", "unsafe_size_t",
}

// overflowCenters are values a window is put around.
var overflowCenters = bigs(
	"-9223372036854775808", "-3037000499", "-2147483648", "-46341", "-32768", "-182", "-128",
	"-1", "0", "1", "11", "127", "128", "181", "255", "256", "32767", "46340", "65535",
	"65536", "2147483647", "2147483648", "3037000499", "4294967295", "9223372036854775807",
	"9223372036854775808", "18446744073709551615",
)

// overflowConsts are the known values the arithmetic and the guard use.
var overflowConsts = bigs(
	"0", "1", "2", "3", "7", "100", "127", "128", "255", "256", "32767", "65535", "65536",
	"2147483647", "4294967295", "9223372036854775807", "-1", "-2", "-100", "-128", "-32768",
)

// ovExpr is arithmetic over x and y: a parameter, v 0 or 1, when op is
// 'x'; the known value c when op is 'k'; a and b combined by op, + - * or
// /; or a negated when op is 'n'.
type ovExpr struct {
	a, b *ovExpr
	c    *big.Int
	v    int
	op   byte
}

// ovCmp is a guard's comparison of a parameter, l, with the other one,
// when r is -1, or with the known value k.
type ovCmp struct {
	k  *big.Int
	op string
	l  int
	r  int
}

// ovProgram is one program the tests check: T, the window of each
// parameter that has one, the rest of the guard, and the use, e, or, for
// a step, the operator step applies to m, x's copy, by amount, y or a
// known value.
//
// Choices that make a division are read after every other choice, and
// only a byte that is not 0 makes one, so input that ran out before them
// makes the program it made before division was among them.
type ovProgram struct {
	typ      string
	windowed [2]bool
	lo, hi   [2]*big.Int
	cmps     []ovCmp
	e        *ovExpr
	step     string
	amount   *ovExpr
	loop     *ovLoop
}

// ovLoop is a loop over a mutable m of type T, started at start, which
// runs while m < limit, or m <= limit when inclusive, steps m by m++ or,
// when by is set, m += by at the top of its body, and then computes use
// from m, standing for parameter 0, and y. The facts the loop's condition
// and m's start give hold at the step only on the first pass: m's step
// reaches every later one.
//
// When post is set, the loop is a three-clause one instead, for mut m T =
// start; cond; post, whose post is m = post, or m += post when compound,
// and whose body only computes use. Its condition compares m with y, not
// limit, when toY is set.
type ovLoop struct {
	start, limit, by *big.Int
	use, post        *ovExpr
	inclusive        bool
	compound, toY    bool
}

// ovUnsigned reports whether typ is unsigned.
func ovUnsigned(typ string) bool {
	return strings.HasPrefix(typ, "u")
}

// ovRange returns the smallest and largest values of typ at width, the
// bits a target-width type has.
func ovRange(typ string, width int) (*big.Int, *big.Int) {
	bits := width
	if typ != "unsafe_size_t" {
		fmt.Sscanf(strings.TrimLeft(typ, "uint"), "%d", &bits)
	}
	one := big.NewInt(1)
	if ovUnsigned(typ) {
		return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits)), one)
	}
	top := new(big.Int).Lsh(one, uint(bits-1))
	return new(big.Int).Neg(top), new(big.Int).Sub(top, one)
}

// ovFits reports whether v is a value of typ at every width a target
// gives it, 32 bits for a target-width type, the narrower.
func ovFits(v *big.Int, typ string) bool {
	lo, hi := ovRange(typ, 32)
	if typ != "unsafe_size_t" {
		lo, hi = ovRange(typ, 64)
	}
	return v.Cmp(lo) >= 0 && v.Cmp(hi) <= 0
}

// ovPick returns a value of vals that typ holds on every target.
func ovPick(r *convReader, vals []*big.Int, typ string) *big.Int {
	var ok []*big.Int
	for _, v := range vals {
		if ovFits(v, typ) {
			ok = append(ok, v)
		}
	}
	return ok[r.next(len(ok))]
}

// ovExprOf reads arithmetic of at most depth levels, naming a parameter.
func ovExprOf(r *convReader, typ string, depth int) *ovExpr {
	if depth == 0 || r.next(3) == 0 {
		return &ovExpr{op: 'x', v: r.next(2)}
	}
	if !ovUnsigned(typ) && r.next(5) == 0 {
		return &ovExpr{op: 'n', a: ovExprOf(r, typ, depth-1)}
	}
	a := ovExprOf(r, typ, depth-1)
	var b *ovExpr
	if r.next(2) == 0 {
		b = &ovExpr{op: 'k', c: ovPick(r, overflowConsts, typ)}
	} else {
		b = ovExprOf(r, typ, depth-1)
	}
	if r.next(2) == 0 {
		a, b = b, a
	}
	return &ovExpr{op: "+-*"[r.next(3)], a: a, b: b}
}

// readOverflowProgram makes the program data's choices describe.
func readOverflowProgram(data []byte) ovProgram {
	r := &convReader{data: data}
	p := ovProgram{typ: overflowTypes[r.next(len(overflowTypes))]}
	narrow := p.typ == "int8" || p.typ == "uint8"
	for v := range 2 {
		if narrow && r.next(2) == 0 {
			continue
		}
		c := ovPick(r, overflowCenters, p.typ)
		lo := new(big.Int).Sub(c, big.NewInt(int64(r.next(13))))
		hi := new(big.Int).Add(c, big.NewInt(int64(r.next(13))))
		tlo, thi := ovRange(p.typ, 32)
		if p.typ != "unsafe_size_t" {
			tlo, thi = ovRange(p.typ, 64)
		}
		if lo.Cmp(tlo) < 0 {
			lo = tlo
		}
		if hi.Cmp(thi) > 0 {
			hi = thi
		}
		p.windowed[v], p.lo[v], p.hi[v] = true, lo, hi
	}
	for range r.next(3) {
		cmp := ovCmp{l: r.next(2), r: -1, op: []string{"<", "<=", ">", ">=", "!="}[r.next(5)]}
		if r.next(3) == 0 {
			cmp.r = 1 - cmp.l
		} else {
			cmp.k = ovPick(r, overflowConsts, p.typ)
		}
		p.cmps = append(p.cmps, cmp)
	}
	if r.next(3) == 0 {
		p.step = []string{"++", "--", "+=", "-=", "*="}[r.next(5)]
		if p.step[1] == '=' {
			p.amount = &ovExpr{op: 'x', v: 1}
			if r.next(2) == 0 {
				p.amount = &ovExpr{op: 'k', c: ovPick(r, overflowConsts, p.typ)}
			}
			if r.next(4) == 1 {
				p.step = "/="
			}
		}
		return p
	}
	if r.next(3) == 0 {
		l := &ovLoop{start: ovPick(r, overflowConsts, p.typ), limit: ovPick(r, overflowCenters, p.typ),
			inclusive: r.next(2) == 0}
		if r.next(2) == 0 {
			l.by = ovPick(r, overflowConsts, p.typ)
			if l.by.Sign() <= 0 {
				l.by = big.NewInt(1)
			}
		}
		l.use = ovExprOf(r, p.typ, 2)
		if r.next(2) == 0 {
			l.post = ovExprOf(r, p.typ, 2)
			l.compound = r.next(2) == 0
			l.toY = r.next(2) == 0
		}
		p.loop = l
		return p
	}
	p.e = ovExprOf(r, p.typ, 3)
	switch r.next(4) {
	case 1:
		p.e = &ovExpr{op: '/', a: p.e, b: ovDivisor(r, p.typ)}
	case 2:
		p.e = &ovExpr{op: '/', a: ovDivisor(r, p.typ), b: p.e}
	}
	return p
}

// ovDivisor reads the other operand of a division: a parameter, a known
// value, or arithmetic one level deep.
func ovDivisor(r *convReader, typ string) *ovExpr {
	if r.next(3) == 1 {
		return &ovExpr{op: 'k', c: ovPick(r, overflowConsts, typ)}
	}
	return ovExprOf(r, typ, 1)
}

// ovName names parameter v.
func ovName(v int) string {
	return []string{"x", "y"}[v]
}

// text writes e as Tack, each operation parenthesised.
func (e *ovExpr) text() string {
	switch e.op {
	case 'x':
		return ovName(e.v)
	case 'k':
		return e.c.String()
	case 'n':
		return "-(" + e.a.text() + ")"
	default:
		return "(" + e.a.text() + " " + string(e.op) + " " + e.b.text() + ")"
	}
}

// guard writes the guard, true when it is empty.
func (p ovProgram) guard() string {
	var parts []string
	for v := range 2 {
		if p.windowed[v] {
			parts = append(parts, fmt.Sprintf("%s <= %s && %s <= %s", p.lo[v], ovName(v), ovName(v), p.hi[v]))
		}
	}
	for _, c := range p.cmps {
		r := ""
		if c.r >= 0 {
			r = ovName(c.r)
		} else {
			r = c.k.String()
		}
		parts = append(parts, ovName(c.l)+" "+c.op+" "+r)
	}
	if len(parts) == 0 {
		return "true"
	}
	return strings.Join(parts, " && ")
}

// source returns the program's text, and where its use starts and ends,
// within which every error must be.
func (p ovProgram) source() (string, int, int) {
	head := fmt.Sprintf("package main\n\nfunc f(x %s, y %s) %s {\n    if %s {\n", p.typ, p.typ, p.typ, p.guard())
	var use string
	switch {
	case p.loop != nil:
		l := p.loop
		cmp, step := "<", "m++"
		if l.inclusive {
			cmp = "<="
		}
		if l.by != nil {
			step = "m += " + l.by.String()
		}
		bound := l.limit.String()
		if l.toY {
			bound = "y"
		}
		body := "            r := " + strings.ReplaceAll(l.use.text(), "x", "m") +
			"\n            if r == 0 {\n                return r\n            }\n"
		if l.post == nil {
			use = fmt.Sprintf("mut m %s = %s\n        for m %s %s {\n            %s\n%s        }\n        return m",
				p.typ, l.start, cmp, bound, step, body)
			break
		}
		post := "m = " + strings.ReplaceAll(l.post.text(), "x", "m")
		if l.compound {
			post = "m += " + strings.ReplaceAll(l.post.text(), "x", "m")
		}
		use = fmt.Sprintf("for mut m %s = %s; m %s %s; %s {\n%s        }\n        return 0",
			p.typ, l.start, cmp, bound, post, body)
	case p.e != nil:
		use = "return " + p.e.text()
	default:
		use = "mut m := x\n        m" + p.step
		if p.amount != nil {
			use = "mut m := x\n        m " + p.step + " " + p.amount.text()
		}
	}
	tail := "\n"
	if p.e == nil && p.loop == nil {
		tail = "\n        return m\n"
	}
	src := head + "        " + use + tail + "    }\n    return 0\n}\n\nfunc main() {\n}\n"
	at := len(head)
	return src, at, at + len("        ") + len(use)
}

// ovDomain returns every value parameter v may have under its window, or
// every value of T for an 8-bit T with none.
func (p ovProgram) domain(v int) []*big.Int {
	lo, hi := p.lo[v], p.hi[v]
	if !p.windowed[v] {
		lo, hi = ovRange(p.typ, 8)
	}
	var out []*big.Int
	for x := new(big.Int).Set(lo); x.Cmp(hi) <= 0; x.Add(x, big.NewInt(1)) {
		out = append(out, new(big.Int).Set(x))
	}
	return out
}

// holds reports whether the guard holds for the values xy.
func (p ovProgram) holds(xy [2]*big.Int) bool {
	for _, c := range p.cmps {
		r := c.k
		if c.r >= 0 {
			r = xy[c.r]
		}
		d := xy[c.l].Cmp(r)
		ok := map[string]bool{"<": d < 0, "<=": d <= 0, ">": d > 0, ">=": d >= 0, "!=": d != 0}[c.op]
		if !ok {
			return false
		}
	}
	return true
}

// eval computes e exactly on xy, and reports false when a step is no
// value of T at width.
func (p ovProgram) eval(e *ovExpr, xy [2]*big.Int, width int) (*big.Int, bool) {
	var v *big.Int
	switch e.op {
	case 'x':
		return xy[e.v], true
	case 'k':
		return e.c, true
	case 'n':
		a, ok := p.eval(e.a, xy, width)
		if !ok {
			return nil, false
		}
		v = new(big.Int).Neg(a)
	default:
		a, okA := p.eval(e.a, xy, width)
		b, okB := p.eval(e.b, xy, width)
		if !okA || !okB {
			return nil, false
		}
		switch e.op {
		case '+':
			v = new(big.Int).Add(a, b)
		case '-':
			v = new(big.Int).Sub(a, b)
		case '/':
			if b.Sign() == 0 {
				return nil, false
			}
			v = new(big.Int).Quo(a, b)
		default:
			v = new(big.Int).Mul(a, b)
		}
	}
	lo, hi := ovRange(p.typ, width)
	return v, v.Cmp(lo) >= 0 && v.Cmp(hi) <= 0
}

// use returns the arithmetic the use does: e, or m's step written as m op
// amount, with m standing for x.
func (p ovProgram) use() *ovExpr {
	if p.e != nil {
		return p.e
	}
	x := &ovExpr{op: 'x'}
	switch p.step {
	case "++":
		return &ovExpr{op: '+', a: x, b: &ovExpr{op: 'k', c: big.NewInt(1)}}
	case "--":
		return &ovExpr{op: '-', a: x, b: &ovExpr{op: 'k', c: big.NewInt(1)}}
	default:
		return &ovExpr{op: p.step[0], a: x, b: p.amount}
	}
}

// counterexample returns a pair the guard holds for at which the use
// leaves T, on either target, or "" when there is none.
func (p ovProgram) counterexample() string {
	widths := []int{64}
	if p.typ == "unsafe_size_t" {
		widths = []int{32, 64}
	}
	if p.loop != nil {
		return p.loopCounterexample(widths)
	}
	e := p.use()
	for _, x := range p.domain(0) {
		for _, y := range p.domain(1) {
			xy := [2]*big.Int{x, y}
			if !p.holds(xy) {
				continue
			}
			for _, w := range widths {
				if _, ok := p.eval(e, xy, w); !ok {
					return fmt.Sprintf("x = %s, y = %s at %d bits", x, y, w)
				}
			}
		}
	}
	return ""
}

// ovLoopPasses is the most passes the oracle runs a loop for: a step or
// a use that leaves T in one of them is a counterexample, and one past
// them is not looked for, which can only miss one.
const ovLoopPasses = 300

// loopCounterexample runs the loop for each pair the guard holds for, at
// each width, and returns the first at which a step or a use leaves T,
// or "" when none does within ovLoopPasses passes.
func (p ovProgram) loopCounterexample(widths []int) string {
	l := p.loop
	by := l.by
	if by == nil {
		by = big.NewInt(1)
	}
	for _, x := range p.domain(0) {
		for _, y := range p.domain(1) {
			if !p.holds([2]*big.Int{x, y}) {
				continue
			}
			for _, w := range widths {
				lo, hi := ovRange(p.typ, w)
				m := new(big.Int).Set(l.start)
				limit := l.limit
				if l.toY {
					limit = y
				}
				for pass := 0; pass < ovLoopPasses; pass++ {
					d := m.Cmp(limit)
					if d > 0 || d == 0 && !l.inclusive {
						break
					}
					if l.post != nil {
						if _, ok := p.eval(l.use, [2]*big.Int{m, y}, w); !ok {
							return fmt.Sprintf("y = %s, m = %s on pass %d at %d bits", y, m, pass, w)
						}
						v, ok := p.eval(l.post, [2]*big.Int{m, y}, w)
						if ok && l.compound {
							v = new(big.Int).Add(m, v)
							ok = v.Cmp(lo) >= 0 && v.Cmp(hi) <= 0
						}
						if !ok {
							return fmt.Sprintf("y = %s, m's post from %s on pass %d at %d bits", y, m, pass, w)
						}
						m = v
						continue
					}
					m = new(big.Int).Add(m, by)
					if m.Cmp(lo) < 0 || m.Cmp(hi) > 0 {
						return fmt.Sprintf("y = %s, m's step on pass %d at %d bits", y, pass, w)
					}
					if _, ok := p.eval(l.use, [2]*big.Int{m, y}, w); !ok {
						return fmt.Sprintf("y = %s, m = %s on pass %d at %d bits", y, m, pass, w)
					}
				}
			}
		}
	}
	return ""
}

// ovError reports whether msg is an error an unproven use may get: an
// overflow, or a constant one between known values, or a divisor that may
// be zero, or is.
func ovError(msg string) bool {
	return strings.Contains(msg, " may overflow ") || strings.Contains(msg, " may go below 0 in ") ||
		strings.HasPrefix(msg, "constant expression overflows") ||
		strings.HasPrefix(msg, "constant ") && strings.Contains(msg, " overflows ") ||
		strings.HasPrefix(msg, "divisor ") || strings.HasPrefix(msg, "division by zero")
}

// checkOverflowProgram checks p and fails t when the checker accepts it
// although the oracle finds a counterexample, or gives an error that is
// not an overflow error at the use. It reports whether p was accepted.
func checkOverflowProgram(t *testing.T, p ovProgram) bool {
	t.Helper()
	src, at, end := p.source()
	_, diags := checkTree(t, src)
	for _, d := range diags {
		if d.Span.Start < at || d.Span.End > end || !ovError(d.Message) {
			t.Fatalf("unexpected error %q at %d:%d:\n%s", d.Message, d.Span.Start, d.Span.End, src)
		}
	}
	if len(diags) > 0 {
		return false
	}
	if bad := p.counterexample(); bad != "" {
		t.Fatalf("accepted, but overflows for %s:\n%s", bad, src)
	}
	return true
}

// TestOverflowOracleFindsOverflows checks the oracle on uses that overflow
// for some value the guard allows, and that the checker rejects them.
func TestOverflowOracleFindsOverflows(t *testing.T) {
	x, y := &ovExpr{op: 'x'}, &ovExpr{op: 'x', v: 1}
	k := func(v int64) *ovExpr { return &ovExpr{op: 'k', c: big.NewInt(v)} }
	for _, tt := range []struct {
		name string
		p    ovProgram
	}{
		{"int8 x + 1 at the largest", ovProgram{typ: "int8", e: &ovExpr{op: '+', a: x, b: k(1)}}},
		{"uint8 x - y", ovProgram{typ: "uint8", e: &ovExpr{op: '-', a: x, b: y}}},
		{"int8 -x", ovProgram{typ: "int8", e: &ovExpr{op: 'n', a: x}}},
		{"unsafe_size_t x * 2 past 32 bits", ovProgram{
			typ: "unsafe_size_t", windowed: [2]bool{true, true},
			lo: [2]*big.Int{big.NewInt(2147483640), big.NewInt(0)},
			hi: [2]*big.Int{big.NewInt(2147483650), big.NewInt(0)},
			e:  &ovExpr{op: '*', a: x, b: k(2)},
		}},
		{"uint8 m-- at 0", ovProgram{typ: "uint8", step: "--"}},
		{"int8 x / y at min / -1", ovProgram{
			typ: "int8", cmps: []ovCmp{{op: "!=", l: 1, r: -1, k: big.NewInt(0)}},
			e: &ovExpr{op: '/', a: x, b: y},
		}},
		{"int16 x / y with y only negative", ovProgram{
			typ: "int16", windowed: [2]bool{true, true},
			lo: [2]*big.Int{big.NewInt(-32768), big.NewInt(-12)},
			hi: [2]*big.Int{big.NewInt(-32760), big.NewInt(-1)},
			e:  &ovExpr{op: '/', a: x, b: y},
		}},
		{"int64 m /= -1 near the smallest", ovProgram{
			typ: "int64", windowed: [2]bool{true, true},
			lo:   [2]*big.Int{big.NewInt(math.MinInt64), big.NewInt(0)},
			hi:   [2]*big.Int{big.NewInt(math.MinInt64 + 3), big.NewInt(0)},
			step: "/=", amount: k(-1),
		}},
		{"uint8 x / y at y == 0", ovProgram{typ: "uint8", e: &ovExpr{op: '/', a: x, b: y}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.p.counterexample() == "" {
				src, _, _ := tt.p.source()
				t.Fatalf("the oracle calls it safe:\n%s", src)
			}
			if checkOverflowProgram(t, tt.p) {
				t.Fatal("accepted")
			}
		})
	}
}

// TestOverflowOracleCallsSafeUsesSafe checks that the oracle finds nothing
// for uses that stay in T at their edges, and that the checker accepts
// them, so neither passes the tests above by calling everything unsafe.
func TestOverflowOracleCallsSafeUsesSafe(t *testing.T) {
	x, y := &ovExpr{op: 'x'}, &ovExpr{op: 'x', v: 1}
	k := func(v int64) *ovExpr { return &ovExpr{op: 'k', c: big.NewInt(v)} }
	for _, tt := range []struct {
		name string
		p    ovProgram
	}{
		{"int8 x + 1 below the largest", ovProgram{
			typ: "int8", cmps: []ovCmp{{op: "<", l: 0, r: -1, k: big.NewInt(127)}},
			e: &ovExpr{op: '+', a: x, b: k(1)},
		}},
		{"uint8 x - y under y <= x", ovProgram{
			typ: "uint8", cmps: []ovCmp{{op: "<=", l: 1, r: 0}}, e: &ovExpr{op: '-', a: x, b: y},
		}},
		{"int8 m++ under x < y", ovProgram{
			typ: "int8", cmps: []ovCmp{{op: "<", l: 0, r: 1}}, step: "++",
		}},
		{"int64 x * y in windows", ovProgram{
			typ: "int64", windowed: [2]bool{true, true},
			lo: [2]*big.Int{big.NewInt(3037000490), big.NewInt(-3037000499)},
			hi: [2]*big.Int{big.NewInt(3037000499), big.NewInt(-3037000490)},
			e:  &ovExpr{op: '*', a: x, b: y},
		}},
		{"int8 x / y under y > 0", ovProgram{
			typ: "int8", cmps: []ovCmp{{op: ">", l: 1, r: -1, k: big.NewInt(0)}},
			e: &ovExpr{op: '/', a: x, b: y},
		}},
		{"int8 x / y under y != 0 && y != -1", ovProgram{
			typ: "int8", cmps: []ovCmp{
				{op: "!=", l: 1, r: -1, k: big.NewInt(0)}, {op: "!=", l: 1, r: -1, k: big.NewInt(-1)},
			},
			e: &ovExpr{op: '/', a: x, b: y},
		}},
		{"int8 x / y under x > -128 && y != 0", ovProgram{
			typ: "int8", cmps: []ovCmp{
				{op: ">", l: 0, r: -1, k: big.NewInt(-128)}, {op: "!=", l: 1, r: -1, k: big.NewInt(0)},
			},
			e: &ovExpr{op: '/', a: x, b: y},
		}},
		{"int8 (x - 1) / y under 0 <= x && y < 0", ovProgram{
			typ: "int8", cmps: []ovCmp{
				{op: ">=", l: 0, r: -1, k: big.NewInt(0)}, {op: "<", l: 1, r: -1, k: big.NewInt(0)},
			},
			e: &ovExpr{op: '/', a: &ovExpr{op: '-', a: x, b: k(1)}, b: y},
		}},
		{"int8 m /= -2", ovProgram{typ: "int8", step: "/=", amount: k(-2)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if bad := tt.p.counterexample(); bad != "" {
				src, _, _ := tt.p.source()
				t.Fatalf("the oracle finds %s:\n%s", bad, src)
			}
			if !checkOverflowProgram(t, tt.p) {
				src, _, _ := tt.p.source()
				t.Fatalf("rejected:\n%s", src)
			}
		})
	}
}

// TestOverflowProofsAreSound checks programs made from fixed seeds, and
// that enough of them are accepted for the checks to mean something.
func TestOverflowProofsAreSound(t *testing.T) {
	accepted, n := 0, 3000
	if testing.Short() {
		n = 300
	}
	for seed := range n {
		rng := rand.New(rand.NewPCG(uint64(seed), 7))
		data := make([]byte, 48)
		for i := range data {
			data[i] = byte(rng.UintN(256))
		}
		if checkOverflowProgram(t, readOverflowProgram(data)) {
			accepted++
		}
	}
	t.Logf("accepted %d of %d", accepted, n)
	if accepted < n/20 {
		t.Fatalf("accepted %d of %d programs; the programs prove too little to test anything", accepted, n)
	}
}

// FuzzOverflowProofsAreSound checks the program its input makes (see
// checkOverflowProgram).
func FuzzOverflowProofsAreSound(f *testing.F) {
	for _, s := range [][]byte{
		{6, 2, 0, 3, 1, 1, 2, 0, 3, 0, 1, 0, 2},
		{1, 0, 0, 1, 2, 4, 0, 1, 0, 3, 5, 1},
		{8, 1, 1, 7, 5, 9, 2, 3, 0, 0, 2, 4, 1},
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		checkOverflowProgram(t, readOverflowProgram(data))
	})
}
