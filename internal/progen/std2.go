package progen

import (
	"errors"
	"strconv"
	"strings"
)

// parseStmt parses a literal with std/conv's parse_int, which follows Go's
// strconv.ParseInt to the bit, in a base and a bit size, and observes the
// error's code when it fails, or the value when not.
func (g *Gen) parseStmt() Stmt {
	g.imports["conv"] = true
	s := g.numeral()
	base := []int{0, 2, 8, 10, 16, 36}[g.pick(6)]
	bits := []int{0, 1, 8, 16, 32, 64}[g.pick(6)]
	text := "conv.parse_int([]uint8(" + strconv.Quote(s) + "), " + strconv.Itoa(base) + ", " +
		strconv.Itoa(bits) + ")"
	return g.errCall(int64Type, text, func(*frame) (Value, Value) {
		v, err := strconv.ParseInt(s, base, bits)
		switch {
		case errors.Is(err, strconv.ErrSyntax):
			return Value{}, Value{Bits: 6}
		case errors.Is(err, strconv.ErrRange):
			return Value{Bits: uint64(v)}, Value{Bits: 7} //nolint:gosec // bits
		}
		return Value{Bits: uint64(v)}, Value{} //nolint:gosec // bits
	})
}

// numeral returns a string that is most often a number: a sign, a prefix,
// digits, and now and then an underscore or a letter that is no digit.
func (g *Gen) numeral() string {
	var b strings.Builder
	b.WriteString([]string{"", "", "-", "+"}[g.pick(4)])
	b.WriteString([]string{"", "", "", "0x", "0b", "0o", "0"}[g.pick(7)])
	const digits = "0123456789abcdefz_"
	n := g.pick(22)
	for i := range n {
		if i > 0 && g.rare(6) {
			b.WriteByte(digits[g.pick(len(digits))])
			continue
		}
		b.WriteByte(digits[g.pick(10)])
	}
	return b.String()
}

// textStmt makes a Text of a string's bytes with std/text, and observes the
// error's code when it fails, or its length and whether it equals a word.
func (g *Gen) textStmt() Stmt {
	g.imports["text"] = true
	s := g.stringExpr()
	word := g.word()
	t := &Var{Name: g.name("t"), T: &Type{Kind: KError}, Used: true}
	e := &Var{Name: g.name("e"), T: errType, Used: true}
	g.declare(t)
	g.declare(e)
	g.hidden[t] = true
	se := s.Eval
	bind := simple(func(f *frame) {
		f.set(t, Value{S: se(f).S})
		f.set(e, Value{})
	}, t.Name+", "+e.Name+" := text.from_bytes([]uint8("+s.Text+"), c_allocator)")
	code := Expr{Text: e.Name + ".code", T: intTypeOf(Int32), Eval: func(f *frame) Value {
		return Value{Bits: f.get(e).Bits}
	}}
	cond := Expr{Text: e.Name + ".code != 0", T: boolType, Eval: func(f *frame) Value {
		return Value{Bool: f.get(e).Bits != 0}
	}}
	length := Expr{Text: t.Name + ".len()", T: int64Type, Eval: func(f *frame) Value {
		return Value{Bits: uint64(len(f.get(t).S))} //nolint:gosec // small
	}}
	same := Expr{
		Text: t.Name + ".equal([]uint8(" + strconv.Quote(word) + "))", T: boolType,
		Eval: func(f *frame) Value { return Value{Bool: f.get(t).S == word} },
	}
	check := ifOf(cond, []Stmt{g.observe(code)}, []Stmt{g.observe(length), g.observe(same)}, true)
	return seq([]Stmt{bind, check})
}

// failureStmt makes a std/errors Failure of an error and a string's bytes,
// and observes its error's code and its detail's length, which an
// allocation that fails leaves 0.
func (g *Gen) failureStmt() Stmt {
	g.imports["errors"] = true
	s := g.stringExpr()
	code := 3 + g.pick(9)
	fv := &Var{Name: g.name("fl"), T: &Type{Kind: KError}, Used: true}
	g.declare(fv)
	g.hidden[fv] = true
	se := s.Eval
	cs := strconv.Itoa(code)
	bind := simple(func(f *frame) { f.set(fv, Value{S: se(f).S}) },
		fv.Name+" := errors.from_bytes(error{code: "+cs+", msg: \"f"+cs+"\"}, []uint8("+s.Text+
			"), c_allocator)")
	errCode := Expr{Text: fv.Name + ".err.code", T: intTypeOf(Int32), Eval: func(*frame) Value {
		return Value{Bits: uint64(code)} //nolint:gosec // small
	}}
	detail := Expr{Text: fv.Name + ".detail.len()", T: int64Type, Eval: func(f *frame) Value {
		return Value{Bits: uint64(len(f.get(fv).S))} //nolint:gosec // small
	}}
	return seq([]Stmt{bind, g.observe(errCode), g.observe(detail)})
}

// genericCall returns a call, as a value of t, of a generic function whose
// type parameter an interface constrains, with a struct literal: the
// interface asks for a method of the struct that takes nothing, and the
// generic function calls it.
func (g *Gen) genericCall(t *Type, d int) (Expr, bool) {
	var opts []*Func
	for _, fn := range g.funcs {
		if fn.Recv != nil && !fn.Recv.Mut && len(fn.Params) == 0 && !fn.ErrResult &&
			fn.Result != nil && returnsAs(fn.Result, t) && g.affordable(fn.Cost) {
			opts = append(opts, fn)
		}
	}
	if len(opts) == 0 {
		return Expr{}, false
	}
	fn := opts[g.pick(len(opts))]
	iface, gen := "I"+fn.Name, "g"+fn.Name
	g.extra(iface, "type "+iface+" interface {\n    "+fn.Name+"() "+fn.Result.String()+"\n}\n")
	g.extra(gen, "func "+gen+"[T "+iface+"](x T) "+fn.Result.String()+" {\n    return x."+
		fn.Name+"()\n}\n")
	g.cost += g.mult * fn.Cost
	lit := g.structLit(fn.Recv.T, d+1)
	le := lit.Eval
	return Expr{Text: gen + "(" + lit.Text + ")", T: fn.Result, Eval: func(f *frame) Value {
		recv := le(f)
		r, _ := call(f.m, fn, &recv, nil)
		return r
	}}, true
}
