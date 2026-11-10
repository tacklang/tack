package progen

import (
	"math"
	"strconv"
)

// intLit returns a literal of type t: most often a small value, sometimes
// one at an edge of t's range.
func (g *Gen) intLit(t *IntType) Expr {
	var bits uint64
	switch g.pick(8) {
	case 0, 1, 2, 3:
		bits = uint64(g.pick(10)) //nolint:gosec // small
	case 4:
		bits = maxOf(t)
	case 5:
		bits = minOf(t)
	case 6:
		bits = norm(t, math.MaxUint64)
	default:
		bits = norm(t, uint64(g.pick(1<<16))*0x9e3779b97f4a7c15) //nolint:gosec // any bits
	}
	return litExpr(t, bits)
}

// litExpr returns the literal of t whose value is held in bits.
func litExpr(t *IntType, bits uint64) Expr {
	text := literal(t, bits)
	if text[0] == '-' {
		text = "(" + text + ")"
	}
	v := Value{Bits: bits}
	return Expr{Text: text, T: intTypeOf(t), Konst: true, Eval: func(*frame) Value { return v }}
}

// maxOf returns the largest value of t.
func maxOf(t *IntType) uint64 {
	if t.Signed {
		return 1<<(t.Bits-1) - 1
	}
	return norm(t, math.MaxUint64)
}

// minOf returns the smallest value of t.
func minOf(t *IntType) uint64 {
	if t.Signed {
		return norm(t, 1<<(t.Bits-1))
	}
	return 0
}

// varExpr returns a read of v, marking it used.
func varExpr(v *Var) Expr {
	v.Used = true
	return Expr{Text: v.Name, T: v.T, Eval: func(f *frame) Value { return copyValue(f.get(v)) }}
}

// intOperand returns an expression whose integer type widens to t.
func (g *Gen) intOperand(t *IntType, d int) Expr {
	if d >= maxExprDepth {
		return g.intLeaf(t)
	}
	switch g.pick(7) {
	case 0, 1:
		return g.intLeaf(t)
	case 2:
		return g.arithExpr(t, d)
	case 3:
		return g.unaryExpr(t, d)
	case 4:
		return g.shiftExpr(t, d)
	case 5:
		if g.pick(2) == 0 {
			if e, ok := g.genericCall(intTypeOf(t), d); ok {
				return e
			}
		}
		if e, ok := g.callExpr(intTypeOf(t), d); ok {
			return e
		}
	}
	return g.arithExpr(t, d)
}

// intLeaf returns a literal, or a name, field, element or length whose
// type widens to t.
func (g *Gen) intLeaf(t *IntType) Expr {
	if g.pick(3) != 0 {
		if e, ok := g.nonConst(t); ok {
			return e
		}
	}
	return g.intLit(t)
}

// nonConst returns an expression that is no constant, of a type that
// widens to t, and whether there is one: a variable, a struct's field, an
// array's element at a constant index, or a length.
func (g *Gen) nonConst(t *IntType) (Expr, bool) {
	for _, try := range []int{g.pick(4), 0, 1, 2, 3} {
		var e Expr
		var ok bool
		switch try {
		case 0:
			e, ok = g.intVar(t)
		case 1:
			e, ok = g.fieldExpr(t)
		case 2:
			e, ok = g.elemExpr(t)
		default:
			e, ok = g.lenExpr(t)
		}
		if ok {
			return e, true
		}
	}
	return Expr{}, false
}

// intVar returns a read of a variable whose integer type widens to t.
func (g *Gen) intVar(t *IntType) (Expr, bool) {
	v := g.choose(g.visible(func(v *Var) bool {
		return v.T.Kind == KInt && Widens(v.T.Int, t)
	}))
	if v == nil {
		return Expr{}, false
	}
	return varExpr(v), true
}

// common returns the type arithmetic on a and b gives, the wider of the
// two, and whether they have one.
func common(a, b *IntType) (*IntType, bool) {
	switch {
	case Widens(a, b):
		return b, true
	case Widens(b, a):
		return a, true
	}
	return nil, false
}

// arithOps are the arithmetic and bitwise operators an expression uses:
// the wrapping +% -% and *%, which need no proof, and the bitwise ones;
// division is made only where its divisor is proven (see stmt.go).
var arithOps = []string{"+%", "-%", "*%", "&", "|", "^", "&^"}

// arithExpr returns l op r, of a type that widens to t, with at least one
// operand no constant, so the compiler never computes it, and a constant
// one a value of the other's type, which it takes.
func (g *Gen) arithExpr(t *IntType, d int) Expr {
	opType := g.narrowerOrSame(t)
	l := g.intOperand(opType, d+1)
	if l.Konst {
		r, ok := g.nonConst(opType)
		if !ok {
			return l
		}
		return g.binary(g.intLit(r.T.Int), r, arithOps[g.pick(len(arithOps))])
	}
	// r's type widens to l's, or to opType when l's is opType, so the
	// two have a common type; an operand is never made and thrown away,
	// which would leave a variable marked read that no code reads.
	r := g.intOperand(l.T.Int, d+1)
	if r.Konst {
		r = g.intLit(l.T.Int)
	}
	return g.binary(l, r, arithOps[g.pick(len(arithOps))])
}

// narrowerOrSame returns t, most often, or a type that widens to it.
func (g *Gen) narrowerOrSame(t *IntType) *IntType {
	if g.pick(3) != 0 {
		return t
	}
	var opts []*IntType
	for _, it := range intTypes {
		if Widens(it, t) {
			opts = append(opts, it)
		}
	}
	return opts[g.pick(len(opts))]
}

// binary returns l op r, computed in their common type.
func (g *Gen) binary(l, r Expr, op string) Expr {
	ct, ok := common(l.T.Int, r.T.Int)
	if !ok {
		panic("progen: operands with no common type: " + l.T.String() + " " + r.T.String())
	}
	le, re := l.Eval, r.Eval
	return Expr{
		Text: "(" + l.Text + " " + op + " " + r.Text + ")",
		T:    intTypeOf(ct),
		Eval: func(f *frame) Value {
			a := le(f).Bits
			b := re(f).Bits
			return Value{Bits: arith(ct, op, a, b)}
		},
	}
}

// unaryExpr returns -%x, of a signed x, which wraps for the smallest
// value, or ^x, x no constant.
func (g *Gen) unaryExpr(t *IntType, d int) Expr {
	x, ok := g.nonConst(g.narrowerOrSame(t))
	if !ok {
		return g.intLeaf(t)
	}
	_ = d
	it := x.T.Int
	xe := x.Eval
	if it.Signed && g.pick(2) == 0 {
		return Expr{Text: "(-%" + x.Text + ")", T: x.T, Eval: func(f *frame) Value {
			return Value{Bits: arith(it, "-", 0, xe(f).Bits)}
		}}
	}
	return Expr{Text: "(^" + x.Text + ")", T: x.T, Eval: func(f *frame) Value {
		return Value{Bits: norm(it, ^xe(f).Bits)}
	}}
}

// shiftExpr returns x << n or x >> n, x no constant, n a constant count,
// sometimes past x's width, or an unsigned variable.
func (g *Gen) shiftExpr(t *IntType, d int) Expr {
	x, ok := g.nonConst(g.narrowerOrSame(t))
	if !ok {
		return g.arithExpr(t, d)
	}
	op := []string{"<<", ">>"}[g.pick(2)]
	n := g.shiftCount(x.T.Int)
	it, xe, ne := x.T.Int, x.Eval, n.Eval
	return Expr{
		Text: "(" + x.Text + " " + op + " " + n.Text + ")",
		T:    x.T,
		Eval: func(f *frame) Value {
			a := xe(f).Bits
			return Value{Bits: shift(it, op, a, ne(f).Bits)}
		},
	}
}

// shiftCount returns a shift's count for a value of t: an unsigned
// variable, or a constant from 0 to a little past t's width.
func (g *Gen) shiftCount(t *IntType) Expr {
	if g.rare(3) {
		v := g.choose(g.visible(func(v *Var) bool { return v.T.Kind == KInt && !v.T.Int.Signed }))
		if v != nil {
			return varExpr(v)
		}
	}
	n := uint64(g.pick(t.Bits + 3)) //nolint:gosec // small
	return Expr{
		Text: strconv.FormatUint(n, 10), T: intTypeOf(Uint64), Konst: true,
		Eval: func(*frame) Value { return Value{Bits: n} },
	}
}

// fieldExpr returns a read of an integer field, whose type widens to t,
// of a struct variable.
func (g *Gen) fieldExpr(t *IntType) (Expr, bool) {
	type pair struct {
		v *Var
		i int
	}
	var opts []pair
	for _, v := range g.visible(func(v *Var) bool { return v.T.Kind == KStruct }) {
		for i, fd := range v.T.Struct.Fields {
			if fd.T.Kind == KInt && Widens(fd.T.Int, t) {
				opts = append(opts, pair{v, i})
			}
		}
	}
	if len(opts) == 0 {
		return Expr{}, false
	}
	p := opts[g.pick(len(opts))]
	p.v.Used = true
	v, i := p.v, p.i
	return Expr{
		Text: v.Name + "." + v.T.Struct.Fields[i].Name,
		T:    v.T.Struct.Fields[i].T,
		Eval: func(f *frame) Value { return f.get(v).Elems[i] },
	}, true
}

// elemExpr returns a read of an array variable's element at a constant
// index, which the compiler proves in range, of a type that widens to t.
func (g *Gen) elemExpr(t *IntType) (Expr, bool) {
	v := g.choose(g.visible(func(v *Var) bool {
		return v.T.Kind == KArray && v.T.Len > 0 && v.T.Elem.Kind == KInt &&
			Widens(v.T.Elem.Int, t)
	}))
	if v == nil {
		return Expr{}, false
	}
	v.Used = true
	i := g.pick(v.T.Len)
	return Expr{
		Text: v.Name + "[" + strconv.Itoa(i) + "]",
		T:    v.T.Elem,
		Eval: func(f *frame) Value { return f.get(v).Elems[i] },
	}, true
}

// lenExpr returns the length of a list, a map or a string variable, an
// int64, when t is int64.
func (g *Gen) lenExpr(t *IntType) (Expr, bool) {
	if t != Int64 {
		return Expr{}, false
	}
	v := g.choose(g.visible(func(v *Var) bool {
		return v.T.Kind == KString || v.T.Kind == KView || v.T.owned() && !g.noLists
	}))
	if v == nil {
		return Expr{}, false
	}
	v.Used = true
	return Expr{Text: "len(" + v.Name + ")", T: int64Type, Eval: func(f *frame) Value {
		return Value{Bits: uint64(lengthOf(f.get(v)))} //nolint:gosec // lengths are small
	}}, true
}

// lengthOf returns the length of a list, a map or a string.
func lengthOf(v Value) int {
	switch {
	case v.List != nil:
		return len(*v.List)
	case v.Map != nil:
		return len(v.Map.keys)
	case v.Elems != nil:
		return len(v.Elems)
	}
	return len(v.S)
}
