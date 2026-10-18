package progen

import (
	"strconv"
	"strings"
)

// boolExpr returns an expression of type bool.
func (g *Gen) boolExpr(d int) Expr {
	if d >= maxExprDepth {
		return g.boolLeaf()
	}
	switch g.pick(7) {
	case 0:
		return g.boolLeaf()
	case 1, 2:
		return g.compareExpr(d)
	case 3:
		return g.logicExpr(d)
	case 4:
		x := g.boolExpr(d + 1)
		xe := x.Eval
		return Expr{
			Text: "(!" + x.Text + ")", T: boolType, Konst: x.Konst,
			Eval: func(f *frame) Value { return Value{Bool: !xe(f).Bool} },
		}
	case 5:
		return g.equalExpr(d)
	default:
		if e, ok := g.callExpr(boolType, d); ok {
			return e
		}
		return g.compareExpr(d)
	}
}

// boolLeaf returns a bool variable, or a literal.
func (g *Gen) boolLeaf() Expr {
	if v := g.choose(g.visible(func(v *Var) bool { return v.T.Kind == KBool })); v != nil &&
		g.pick(3) != 0 {
		return varExpr(v)
	}
	b := g.pick(2) == 1
	return Expr{
		Text: strconv.FormatBool(b), T: boolType, Konst: true,
		Eval: func(*frame) Value { return Value{Bool: b} },
	}
}

// cmpOps are the comparison operators.
var cmpOps = []string{"==", "!=", "<", "<=", ">", ">="}

// compareExpr compares two integers, of any two types, as Tack compares
// their values; a constant operand is a value of the other's type.
func (g *Gen) compareExpr(d int) Expr {
	l := g.intOperand(intTypes[g.pick(len(intTypes))], d+1)
	r := g.intOperand(intTypes[g.pick(len(intTypes))], d+1)
	switch {
	case l.Konst && r.Konst:
		r = g.intLit(l.T.Int)
	case l.Konst:
		l = g.intLit(r.T.Int)
	case r.Konst:
		r = g.intLit(l.T.Int)
	}
	if l.Text == r.Text {
		// x < x lowers to a C self-comparison, which clang warns of
		// (finding: see TestFindingSelfCompareWarns).
		r = g.intLit(l.T.Int)
	}
	op := cmpOps[g.pick(len(cmpOps))]
	lt, rt, le, re := l.T.Int, r.T.Int, l.Eval, r.Eval
	return Expr{
		Text:  "(" + l.Text + " " + op + " " + r.Text + ")",
		T:     boolType,
		Konst: l.Konst && r.Konst,
		Eval: func(f *frame) Value {
			a := le(f).Bits
			b := re(f).Bits
			return Value{Bool: holds(op, compare(lt, a, rt, b))}
		},
	}
}

// holds reports whether the comparison op holds of two values that
// compare as c, -1, 0 or 1.
func holds(op string, c int) bool {
	switch op {
	case "==":
		return c == 0
	case "!=":
		return c != 0
	case "<":
		return c < 0
	case "<=":
		return c <= 0
	case ">":
		return c > 0
	}
	return c >= 0
}

// logicExpr returns l && r or l || r, which evaluates r only when it
// decides the result.
func (g *Gen) logicExpr(d int) Expr {
	l, r := g.boolExpr(d+1), g.boolExpr(d+1)
	and := g.pick(2) == 0
	op := "||"
	if and {
		op = "&&"
	}
	le, re := l.Eval, r.Eval
	return Expr{
		Text:  "(" + l.Text + " " + op + " " + r.Text + ")",
		T:     boolType,
		Konst: l.Konst && r.Konst,
		Eval: func(f *frame) Value {
			a := le(f).Bool
			if a != and {
				return Value{Bool: a}
			}
			return Value{Bool: re(f).Bool}
		},
	}
}

// equalExpr compares two values of a struct, a string, a bool or a union
// type with == or !=, or two integers when there is none.
func (g *Gen) equalExpr(d int) Expr {
	v := g.choose(g.visible(func(v *Var) bool {
		k := v.T.Kind
		return k == KStruct || k == KString || k == KUnion || k == KArray && v.T.Elem.Kind == KInt
	}))
	if v == nil {
		return g.compareExpr(d)
	}
	if v.T.Kind == KString && g.pick(2) == 0 {
		return g.stringOrder(v)
	}
	l := varExpr(v)
	// No call in the other side: a struct or array literal holding calls,
	// compared, lowers to C that does not compile (finding: see
	// TestFindingEqualLiteralWithCallsDoesNotCompile).
	saved := g.noCalls
	g.noCalls = true
	r := g.valueOf(v.T, d+1)
	g.noCalls = saved
	op := "=="
	if g.pick(2) == 0 {
		op = "!="
	}
	t, le, re := v.T, l.Eval, r.Eval
	return Expr{
		Text: "(" + l.Text + " " + op + " " + r.Text + ")",
		T:    boolType,
		Eval: func(f *frame) Value {
			a := le(f)
			eq := equal(t, a, re(f))
			return Value{Bool: eq == (op == "==")}
		},
	}
}

// stringOrder compares a string variable with a string by order, byte
// by byte, each byte unsigned.
func (g *Gen) stringOrder(v *Var) Expr {
	l, r := varExpr(v), g.stringExpr()
	op := cmpOps[2+g.pick(4)]
	le, re := l.Eval, r.Eval
	return Expr{
		Text: "(" + l.Text + " " + op + " " + r.Text + ")", T: boolType,
		Eval: func(f *frame) Value {
			a := le(f).S
			return Value{Bool: holds(op, strings.Compare(a, re(f).S))}
		},
	}
}

// valueOf returns an expression of type t, a copyable type: an integer,
// a bool, a string, a struct, an array or a union.
func (g *Gen) valueOf(t *Type, d int) Expr {
	switch t.Kind {
	case KInt:
		e := g.intOperand(t.Int, d)
		return e
	case KBool:
		return g.boolExpr(d)
	case KString:
		return g.stringExpr()
	case KStruct:
		return g.structExpr(t, d)
	case KArray:
		return g.arrayExpr(t, d)
	case KUnion:
		return g.unionExpr(t, d)
	}
	panic("progen: no value of " + t.String())
}

// stringExpr returns a string variable or literal.
func (g *Gen) stringExpr() Expr {
	if v := g.choose(g.visible(func(v *Var) bool { return v.T.Kind == KString })); v != nil &&
		g.pick(2) == 0 {
		return varExpr(v)
	}
	return stringLit(g.word())
}

// word returns a short string of lowercase letters, maybe empty.
func (g *Gen) word() string {
	n := g.pick(5)
	var b strings.Builder
	for range n {
		b.WriteByte(byte('a' + g.pick(4))) //nolint:gosec // a to d
	}
	return b.String()
}

// stringLit returns the literal s.
func stringLit(s string) Expr {
	v := Value{S: s}
	return Expr{
		Text: strconv.Quote(s), T: stringType, Konst: true,
		Eval: func(*frame) Value { return v },
	}
}

// structExpr returns a value of the struct type t: a variable of it, or a
// literal naming every field.
func (g *Gen) structExpr(t *Type, d int) Expr {
	if v := g.choose(g.visible(func(v *Var) bool { return sameType(v.T, t) })); v != nil &&
		g.pick(2) == 0 {
		return varExpr(v)
	}
	return g.structLit(t, d)
}

// structLit returns a literal of the struct type t naming every field.
func (g *Gen) structLit(t *Type, d int) Expr {
	sd := t.Struct
	parts := make([]string, len(sd.Fields))
	evals := make([]func(*frame) Value, len(sd.Fields))
	for i, fd := range sd.Fields {
		e := g.valueOf(fd.T, d+1)
		parts[i] = fd.Name + ": " + e.Text
		evals[i] = e.Eval
	}
	return Expr{
		Text: "(" + sd.Name + "{" + strings.Join(parts, ", ") + "})",
		T:    t,
		Eval: func(f *frame) Value { return Value{Elems: evalAll(f, evals)} },
	}
}

// arrayExpr returns a value of the array type t: a variable of it, or a
// literal of every element.
func (g *Gen) arrayExpr(t *Type, d int) Expr {
	if v := g.choose(g.visible(func(v *Var) bool { return sameType(v.T, t) })); v != nil &&
		g.pick(2) == 0 {
		return varExpr(v)
	}
	parts := make([]string, t.Len)
	evals := make([]func(*frame) Value, t.Len)
	for i := range t.Len {
		e := g.valueOf(t.Elem, d+1)
		parts[i], evals[i] = e.Text, e.Eval
	}
	return Expr{
		Text: "(" + t.String() + "{" + strings.Join(parts, ", ") + "})",
		T:    t,
		Eval: func(f *frame) Value { return Value{Elems: evalAll(f, evals)} },
	}
}

// evalAll evaluates evals in order, left to right.
func evalAll(f *frame, evals []func(*frame) Value) []Value {
	out := make([]Value, len(evals))
	for i, e := range evals {
		out[i] = e(f)
	}
	return out
}
