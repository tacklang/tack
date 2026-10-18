package progen

// divStmt divides: in the checked form, q, ok := a / b, or with the
// divisor, a variable, proven not zero by an if around the division.
func (g *Gen) divStmt() Stmt {
	op := []string{"/", "%"}[g.pick(2)]
	if g.pick(2) == 0 {
		return g.checkedDiv(op)
	}
	return g.guardedDiv(op)
}

// checkedDiv returns q, ok := a op b, b no constant.
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
		if y == 0 {
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
// exit ending in exit that proves it, then the division (see exitGuard).
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
	if g.writable(d) && g.pick(3) == 0 {
		return seq([]Stmt{exitGuard(d), g.observe(div)})
	}
	cond := Expr{Text: d.Name + " != 0", T: boolType, Eval: func(f *frame) Value {
		return Value{Bool: de.Eval(f).Bits != 0}
	}}
	return ifOf(cond, []Stmt{g.observe(div)}, nil, false)
}

// exitGuard returns if d == 0 { d = 1 } and then if d == 0 { exit(9) }, d
// a writable integer variable: the second if, an early exit ending in
// exit, proves d not zero for the statements after it, and is never taken,
// since the first has made d 1 where it was 0, as the interpreter does.
func exitGuard(d *Var) Stmt {
	d.Used = true
	return simple(func(f *frame) {
		if v := f.ref(d); v.Bits == 0 {
			v.Bits = 1
		}
	},
		"if "+d.Name+" == 0 {", "    "+d.Name+" = 1", "}",
		"if "+d.Name+" == 0 {", "    exit(9)", "}")
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
