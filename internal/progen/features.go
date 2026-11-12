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
		return v.T.owned() && (!v.Param || v.Own) && !g.frozen[v] && v.Loops == g.loops &&
			!g.inDefer
	}))
	if v == nil {
		return g.newCollection()
	}
	v.Used = true
	v.Moved = true
	return simple(func(*frame) {}, "drop(own "+v.Name+")")
}

// arenaStmt makes an arena of int64s from a sentinel, adds a value to it
// with add and two with add_all, each an int64 operand naming no list or
// map, evaluated in order, and, once both errors are checked, observes
// the element its id names, the sentinel the zero id names, its length
// and the span's, and each element of the span's view, ranged over. Its
// variables are its own, which no other statement names; it allocates,
// so it is never in deferred code. No run the interpreter checks fails
// to allocate, so the interpreter always takes the branch.
func (g *Gen) arenaStmt() Stmt {
	if g.inDefer || g.noLists {
		return g.obsStmt()
	}
	g.noLists = true
	vals := make([]Expr, 4)
	for i := range vals {
		vals[i] = g.intOperand(Int64, 1)
	}
	g.noLists = false
	a, e, errE, s := g.name("a"), g.name("e"), g.name("err"), g.name("s")
	errS, z, v := g.name("err"), g.name("z"), g.name("v")
	lines := []string{
		"mut " + a + " := make(arena[int64], " + vals[0].Text + ", c_allocator)",
		e + ", " + errE + " := add(mut " + a + ", " + vals[1].Text + ")",
		s + ", " + errS + " := add_all(mut " + a + ", " + vals[2].Text + ", " + vals[3].Text + ")",
		"mut " + z + " id[int64]",
		"if " + errE + ".code == 0 && " + errS + ".code == 0 {",
		"    obs(" + a + "[" + e + "])",
		"    obs(" + a + "[" + z + "])",
		"    obs(len(" + a + ") +% len(" + a + "[" + s + "]))",
		"    for _, " + v + " := range " + a + "[" + s + "] {",
		"        obs(" + v + ")",
		"    }",
		"}",
	}
	return simple(func(f *frame) {
		vs := make([]int64, len(vals))
		for i, x := range vals {
			vs[i] = asInt64(x.Eval(f).Bits)
		}
		f.m.observe(vs[1])
		f.m.observe(vs[0])
		f.m.observe(6)
		f.m.observe(vs[2])
		f.m.observe(vs[3])
	}, lines...)
}

// poolStmt makes a pool of int64s and inserts two values with insert,
// each an int64 operand naming no list or map, evaluated in order, and,
// once both errors are checked, observes the first through its handle,
// which has proves live, removes it with the statement form, observes
// the checked form of its now stale handle, which gives 0, the pool's
// length, each element of a range over it, the second taken out with the
// checked remove, and the length again. Its variables are its own, which
// no other statement names; it allocates, so it is never in deferred
// code. No run the interpreter checks fails to allocate, so the
// interpreter always takes the branch.
func (g *Gen) poolStmt() Stmt {
	if g.inDefer || g.noLists {
		return g.obsStmt()
	}
	g.noLists = true
	vals := []Expr{g.intOperand(Int64, 1), g.intOperand(Int64, 1)}
	g.noLists = false
	p, a, errA, b, errB := g.name("p"), g.name("h"), g.name("err"), g.name("h"), g.name("err")
	x, found, w, y, ok := g.name("x"), g.name("ok"), g.name("w"), g.name("y"), g.name("ok")
	lines := []string{
		"mut " + p + " := make(pool[int64], c_allocator)",
		a + ", " + errA + " := insert(mut " + p + ", " + vals[0].Text + ")",
		b + ", " + errB + " := insert(mut " + p + ", " + vals[1].Text + ")",
		"if " + errA + ".code == 0 && " + errB + ".code == 0 {",
		"    if has(" + p + ", " + a + ") {",
		"        obs(" + p + "[" + a + "])",
		"    }",
		"    remove(mut " + p + ", " + a + ")",
		"    " + x + ", " + found + " := " + p + "[" + a + "]",
		"    if !" + found + " {",
		"        obs(" + x + ")",
		"    }",
		"    obs(len(" + p + "))",
		"    for _, " + w + " := range " + p + " {",
		"        obs(" + w + ")",
		"    }",
		"    " + y + ", " + ok + " := remove(mut " + p + ", " + b + ")",
		"    if " + ok + " {",
		"        obs(" + y + ")",
		"    }",
		"    obs(len(" + p + "))",
		"}",
	}
	return simple(func(f *frame) {
		v0, v1 := asInt64(vals[0].Eval(f).Bits), asInt64(vals[1].Eval(f).Bits)
		f.m.observe(v0)
		f.m.observe(0)
		f.m.observe(1)
		f.m.observe(v1)
		f.m.observe(v1)
		f.m.observe(0)
	}, lines...)
}

// regionStmt opens a region around a block of any statements, so every
// way out of that block, a return, a break, a continue, a ? and falling
// off its end, the block's defers and frees on the way, passes the
// region's end; then, if the block falls through, makes a list of int64s
// through the region, appends two values to it, each an int64 operand
// naming no list or map, evaluated in order, and, once both errors are
// checked, sums them by a range into a local of the region, then opens a
// region inside it holding a list of lists through the inner region, to
// which it appends a list made through the outer one, observes that
// list's length, 1, and, after the inner region, the sum. Its variables
// are its own, which no other statement names, so nothing the block does
// can move a value from the region out of it; it allocates, so it is
// never in deferred code. No run the interpreter checks fails to
// allocate, so the interpreter always takes the branches.
func (g *Gen) regionStmt() Stmt {
	if g.inDefer || g.noLists {
		return g.obsStmt()
	}
	r, q := g.name("r"), g.name("q")
	inner := g.block()
	g.noLists = true
	vals := []Expr{g.intOperand(Int64, 1), g.intOperand(Int64, 1)}
	g.noLists = false
	s, l, errA, errB, w := g.name("s"), g.name("l"), g.name("err"), g.name("err"), g.name("w")
	k, errK := g.name("k"), g.name("err")
	lines := []string{
		"mut " + s + " int64 = 0",
		"mut " + l + " := make(list[int64], " + r + ")",
		errA + " := append(mut " + l + ", " + vals[0].Text + ")",
		errB + " := append(mut " + l + ", " + vals[1].Text + ")",
		"if " + errA + ".code == 0 && " + errB + ".code == 0 {",
		"    for _, " + w + " := range " + l + " {",
		"        " + s + " = " + s + " +% " + w,
		"    }",
		"}",
		"region " + q + " {",
		"    mut " + k + " := make(list[list[int64]], " + q + ")",
		"    " + errK + " := append(mut " + k + ", make(list[int64], " + r + "))",
		"    if " + errK + ".code == 0 {",
		"        obs(len(" + k + "))",
		"    }",
		"}",
		"obs(" + s + ")",
	}
	tail := simple(func(f *frame) {
		v0, v1 := asInt64(vals[0].Eval(f).Bits), asInt64(vals[1].Eval(f).Bits)
		f.m.observe(1)
		f.m.observe(v0 + v1)
	}, lines...)
	inner = append(inner, tail)
	return Stmt{Lines: indent("region "+r+" {", inner), Exec: func(f *frame) ctrl {
		return execBlock(f, inner)
	}}
}
