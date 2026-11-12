package progen

// deferSafe is the last of stmtKinds deferred code may hold: none past it
// can leave the deferred block, nor allocate, whose failure returns.
const deferSafe = 8

// observeFunc reads v, a function value, into the checksum, by calling it.
func (g *Gen) observeFunc(v *Var) Stmt {
	args := make([]Expr, len(v.T.Params))
	for i, t := range v.T.Params {
		args[i] = g.valueOf(t, 2)
	}
	g.cost += g.mult * g.costOf(v.T)
	tg := target{fv: v}
	eval := g.callEval(tg, args)
	return g.observe(Expr{Text: g.callText(tg, args), T: v.T.Result, Eval: func(f *frame) Value {
		r, _ := eval(f)
		return r
	}})
}

// funcValueStmt declares a variable of a function type holding a function
// generated so far, or assigns one of the same type another.
func (g *Gen) funcValueStmt() Stmt {
	var opts []*Func
	for _, fn := range g.funcs {
		if valueFunc(fn) {
			opts = append(opts, fn)
		}
	}
	if len(opts) == 0 {
		return g.obsStmt()
	}
	fn := opts[g.pick(len(opts))]
	t := funcType(fn)
	val := Value{Fn: fn, Valid: true}
	if v := g.choose(g.visible(func(v *Var) bool {
		return v.T.Kind == KFunc && g.writable(v) && sameType(v.T, t)
	})); v != nil && g.pick(2) == 0 {
		return simple(func(f *frame) { f.set(v, val) }, v.Name+" = "+funcValueName(fn))
	}
	v := &Var{Name: g.name("fv"), T: t, Mut: true}
	g.declare(v)
	return simple(func(f *frame) { f.set(v, val) },
		"mut "+v.Name+" "+t.String()+" = "+funcValueName(fn))
}

// deferStmt returns defer { ... }, which runs when the block holding it is
// left: statements that cannot leave it, allocate, or name a list or a
// map, which a later statement of the block may move.
func (g *Gen) deferStmt() Stmt {
	if g.inDefer {
		return g.obsStmt()
	}
	g.inDefer, g.noLists = true, true
	body := g.block()
	if len(body) == 0 {
		body = []Stmt{g.obsStmt()}
	}
	g.inDefer, g.noLists = false, false
	return Stmt{Lines: indent("defer {", body), Exec: func(f *frame) ctrl {
		f.defers = append(f.defers, func(f *frame) { execBlock(f, body) })
		return cNormal
	}}
}

// swapStmt swaps two elements of a writable list, with both indexes
// proven in range: if 0 <= i && i < len(l) && 0 <= j && j < len(l).
func (g *Gen) swapStmt() Stmt {
	l := g.choose(g.visible(func(v *Var) bool { return v.T.Kind == KList && g.writable(v) }))
	i, j := g.indexVar(), g.indexVar()
	if l == nil || i == nil {
		return g.newCollection()
	}
	gi, gj := guard(l, i), guard(l, j)
	cond := Expr{Text: gi.Text + " && " + gj.Text, T: boolType, Eval: func(f *frame) Value {
		return Value{Bool: gi.Eval(f).Bool && gj.Eval(f).Bool}
	}}
	sw := simple(func(f *frame) {
		lst := *f.get(l).List
		a, b := asInt64(f.get(i).Bits), asInt64(f.get(j).Bits)
		lst[a], lst[b] = lst[b], lst[a]
	}, "swap(mut "+l.Name+", "+i.Name+", "+j.Name+")")
	return ifOf(cond, []Stmt{sw}, nil, false)
}

// dropStmt returns drop(own v), for a list or a map v that a move may
// take where generation is, as an own argument's (see passable): no plain
// parameter, nothing frozen, ranged over or declared outside a loop
// around the drop, and not in deferred code. It frees v at once, which
// live's check at the end of main and the sanitizers watch, so v is moved
// from then on and named by no later statement; the interpreter has
// nothing to do.
func (g *Gen) dropStmt() Stmt {
	v := g.choose(g.visible(func(v *Var) bool {
		return v.T.owned() && (!v.Param || v.Own) && !g.frozen[v] && v.Loops == g.loops && !g.inDefer
	}))
	if v == nil {
		return g.newCollection()
	}
	v.Used = true
	v.Moved = true
	return simple(func(*frame) {}, "drop(own "+v.Name+")")
}
