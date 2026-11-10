package progen

import "strconv"

// divStmt divides: in the checked form, q, ok := a / b, or with the
// divisor, a variable, proven not zero by an if around the division, and,
// for a signed /, not -1, so the division is never the smallest value
// divided by -1.
func (g *Gen) divStmt() Stmt {
	op := []string{"/", "%"}[g.pick(2)]
	if g.pick(2) == 0 {
		return g.checkedDiv(op)
	}
	return g.guardedDiv(op)
}

// checkedDiv returns q, ok := a op b, b no constant, whose ok is false for
// a zero b, and for a signed / of the smallest value by -1.
func (g *Gen) checkedDiv(op string) Stmt {
	b, ok := g.nonConst(intTypes[g.pick(len(intTypes))])
	if !ok {
		return g.obsStmt()
	}
	a := g.intOperand(b.T.Int, 1)
	if a.Konst {
		a = g.intLit(b.T.Int)
	}
	ct, _ := common(a.T.Int, b.T.Int)
	q := &Var{Name: g.name("q"), T: intTypeOf(ct), Mut: false}
	okv := &Var{Name: g.name("ok"), T: boolType}
	g.declare(q)
	g.declare(okv)
	ae, be := a.Eval, b.Eval
	return simple(func(f *frame) {
		x := ae(f).Bits
		y := be(f).Bits
		if y == 0 || op == "/" && overflowsDiv(ct, x, y) {
			f.set(q, Value{})
			f.set(okv, Value{Bool: false})
			return
		}
		f.set(q, Value{Bits: arith(ct, op, x, y)})
		f.set(okv, Value{Bool: true})
	}, q.Name+", "+okv.Name+" := "+a.Text+" "+op+" "+b.Text)
}

// guardedDiv returns if d != 0 { obs(a op d) }, d a variable, which the
// if proves not zero where the division is, or, for a writable d, an early
// exit ending in exit that proves it, or a call whose contract proves it,
// then the division (see exitGuard and fixStmt). A signed / needs d not
// -1 too: the if is d != 0 && d != -1, and the early exit rules out both,
// while no contract can say d != -1, so the call is not used for one.
func (g *Gen) guardedDiv(op string) Stmt {
	d := g.choose(g.visible(func(v *Var) bool { return v.T.Kind == KInt }))
	if d == nil {
		return g.checkedDiv(op)
	}
	dt := d.T.Int
	a := g.intOperand(dt, 1)
	if a.Konst {
		a = g.intLit(dt)
	}
	ct, _ := common(a.T.Int, dt)
	de := varExpr(d)
	ae := a.Eval
	div := Expr{
		Text: "(" + a.Text + " " + op + " " + d.Name + ")", T: intTypeOf(ct),
		Eval: func(f *frame) Value {
			x := ae(f).Bits
			return Value{Bits: arith(ct, op, x, f.get(d).Bits)}
		},
	}
	minusOne := op == "/" && ct.Signed
	if g.writable(d) && g.pick(3) == 0 {
		if g.pick(2) == 0 || minusOne {
			return seq([]Stmt{exitGuard(d, minusOne), g.observe(div)})
		}
		return seq([]Stmt{g.fixStmt(d), g.observe(div)})
	}
	cond := Expr{Text: d.Name + " != 0", T: boolType, Eval: func(f *frame) Value {
		return Value{Bool: de.Eval(f).Bits != 0}
	}}
	if minusOne {
		cond = Expr{Text: d.Name + " != 0 && " + d.Name + " != -1", T: boolType, Eval: func(f *frame) Value {
			b := de.Eval(f).Bits
			return Value{Bool: b != 0 && int64(b) != -1} //nolint:gosec // two's complement on purpose
		}}
	}
	return ifOf(cond, []Stmt{g.observe(div)}, nil, false)
}

// exitGuard returns if d == 0 { d = 1 } and then if d == 0 { exit(9) }, d
// a writable integer variable: the second if, an early exit ending in
// exit, proves d not zero for the statements after it, and is never taken,
// since the first has made d 1 where it was 0, as the interpreter does.
// With minusOne, each if tests d == 0 || d == -1 instead, which a signed
// d of a signed division must not be either.
func exitGuard(d *Var, minusOne bool) Stmt {
	d.Used = true
	test := d.Name + " == 0"
	if minusOne {
		test += " || " + d.Name + " == -1"
	}
	return simple(func(f *frame) {
		if v := f.ref(d); v.Bits == 0 || minusOne && int64(v.Bits) == -1 { //nolint:gosec // two's complement
			v.Bits = 1
		}
	},
		"if "+test+" {", "    "+d.Name+" = 1", "}",
		"if "+test+" {", "    exit(9)", "}")
}

// convStmt converts in the checked form, v, ok := T(x), x no constant.
func (g *Gen) convStmt() Stmt {
	x, ok := g.nonConst(intTypes[g.pick(len(intTypes))])
	if !ok {
		return g.obsStmt()
	}
	to := intTypes[g.pick(len(intTypes))]
	c := &Var{Name: g.name("c"), T: intTypeOf(to)}
	okv := &Var{Name: g.name("ok"), T: boolType}
	g.declare(c)
	g.declare(okv)
	from, xe := x.T.Int, x.Eval
	return simple(func(f *frame) {
		b := xe(f).Bits
		if fits(from, b, to) {
			f.set(c, Value{Bits: b})
			f.set(okv, Value{Bool: true})
			return
		}
		f.set(c, Value{})
		f.set(okv, Value{Bool: false})
	}, c.Name+", "+okv.Name+" := "+to.Name+"("+x.Text+")")
}

// controlStmt returns if cond { break }, or continue, inside a loop, or
// if cond { return ... } anywhere a return may be.
func (g *Gen) controlStmt() Stmt {
	if g.rare(4) {
		if s, ok := g.unreachableStmt(); ok {
			return s
		}
	}
	cond := g.boolExpr(0)
	if g.loops > 0 && !g.inSwitch && !g.inDefer && g.pick(2) == 0 {
		word, c := "break", cBreak
		if g.pick(2) == 0 {
			word, c = "continue", cContinue
		}
		return ifOf(cond, []Stmt{{Lines: []string{word}, Exec: func(*frame) ctrl { return c }}},
			nil, false)
	}
	if g.inDefer {
		return g.obsStmt()
	}
	return ifOf(cond, []Stmt{g.returnStmt()}, nil, false)
}

// returnStmt returns from the function being generated, with a value of
// its result type when it has one, and no error, or, now and then, a
// failure, when it returns one.
func (g *Gen) returnStmt() Stmt {
	if g.fn.Result == nil {
		return Stmt{Lines: []string{"return"}, Exec: func(*frame) ctrl { return cReturn }}
	}
	if g.fn.ErrResult && g.rare(3) {
		return g.errReturn(3 + g.pick(5))
	}
	e := g.valueOf(g.fn.Result, 1)
	ee := e.Eval
	text := "return " + e.Text
	if g.fn.ErrResult {
		text += ", noerror"
	}
	return Stmt{Lines: []string{text}, Exec: func(f *frame) ctrl {
		f.ret, f.retErr = ee(f), Value{}
		return cReturn
	}}
}

// zeroReturn returns from the function being generated with its result
// type's zero value, a return that names nothing a failed call left, and
// a failure when it returns an error.
func (g *Gen) zeroReturn() Stmt {
	r := g.fn.Result
	switch {
	case g.fn.ErrResult:
		return g.errReturn(1)
	case r == nil:
		return Stmt{Lines: []string{"return"}, Exec: func(*frame) ctrl { return cReturn }}
	case r.Kind == KBool:
		return Stmt{Lines: []string{"return false"}, Exec: func(f *frame) ctrl {
			f.ret = Value{}
			return cReturn
		}}
	}
	return Stmt{Lines: []string{"return 0"}, Exec: func(f *frame) ctrl {
		f.ret = Value{}
		return cReturn
	}}
}

// fixStmt returns pg_fix_T(mut d), d a writable integer variable of type
// T, a call to a helper whose contract, ensures d != 0, proves d not zero
// in the statements after it: it makes a zero d 1, as the interpreter
// does.
func (g *Gen) fixStmt(d *Var) Stmt {
	t := d.T.String()
	name := "pg_fix_" + t
	g.extra(name, "// "+name+" makes d 1 where it is zero, which its contract says.\n"+
		"func "+name+"(mut d "+t+") ensures d != 0 {\n"+
		"    if d != 0 {\n        return\n    }\n"+
		"    d = 1\n"+
		"    if d == 0 {\n        exit(9)\n    }\n}\n")
	d.Used = true
	return simple(func(f *frame) {
		if v := f.ref(d); v.Bits == 0 {
			v.Bits = 1
		}
	}, name+"(mut "+d.Name+")")
}

// unreachableStmt returns an unreachable the compiler must prove, behind
// guards on an integer variable v that contradict, in one of three
// shapes: if v < k { if v >= k { unreachable } }, if v < k && v > k {
// unreachable }, and if v == k { if v < k || v > k { unreachable } },
// each side of whose true || contradicts v == k. k is from 1 to 100, a
// value of every integer type. The guards only read v, so the
// interpreter runs the whole statement as nothing; should the compiled
// program reach the unreachable, it aborts, which the oracles that run it
// catch. It reports false when no integer variable is in scope.
func (g *Gen) unreachableStmt() (Stmt, bool) {
	v := g.choose(g.visible(func(v *Var) bool { return v.T.Kind == KInt }))
	if v == nil {
		return Stmt{}, false
	}
	e := varExpr(v)
	k := strconv.Itoa(1 + g.pick(100))
	never := Stmt{Lines: []string{"unreachable"}}
	none := func(*frame) ctrl { return cNormal }
	inner := [][]string{
		indent("if "+e.Text+" >= "+k+" {", []Stmt{never}),
		indent("if "+e.Text+" < "+k+" || "+e.Text+" > "+k+" {", []Stmt{never}),
	}
	var lines []string
	switch g.pick(3) {
	case 0:
		lines = indent("if "+e.Text+" < "+k+" {", []Stmt{{Lines: inner[0]}})
	case 1:
		lines = indent("if "+e.Text+" < "+k+" && "+e.Text+" > "+k+" {", []Stmt{never})
	default:
		lines = indent("if "+e.Text+" == "+k+" {", []Stmt{{Lines: inner[1]}})
	}
	return Stmt{Lines: lines, Exec: none}, true
}
