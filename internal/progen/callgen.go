package progen

import "strconv"

// target is what a call can call: a function generated so far, a method
// of one on a receiver variable, or a variable or parameter of a function
// type, whose cost is the most any function it can hold costs.
type target struct {
	fn   *Func // the function or method; nil for a call through fv
	recv *Var  // a method's receiver
	fv   *Var  // a function value called through
	cost int
}

// result returns the type a call of tg gives, nil for none.
func (tg target) result() *Type {
	if tg.fv != nil {
		return tg.fv.T.Result
	}
	return tg.fn.Result
}

// params returns the types and kinds of tg's parameters.
func (tg target) params() []*Var {
	if tg.fv != nil {
		ps := make([]*Var, len(tg.fv.T.Params))
		for i, t := range tg.fv.T.Params {
			ps[i] = &Var{T: t}
		}
		return ps
	}
	return tg.fn.Params
}

// affordable reports whether the code being generated can pay for a call
// costing cost.
func (g *Gen) affordable(cost int) bool {
	return g.cost+g.mult*cost <= costBudget
}

// targets returns every target that keep allows and the code being
// generated can afford: functions and methods generated so far, each
// method on each variable that can be its receiver, and function values.
func (g *Gen) targets(keep func(target) bool) []target {
	var out []target
	add := func(tg target) {
		if g.affordable(tg.cost) && keep(tg) {
			out = append(out, tg)
		}
	}
	for _, fn := range g.funcs {
		if fn.Recv == nil {
			add(target{fn: fn, cost: fn.Cost})
			continue
		}
		for _, v := range g.visible(func(v *Var) bool { return sameType(v.T, fn.Recv.T) }) {
			if !fn.Recv.Mut || g.writable(v) {
				add(target{fn: fn, recv: v, cost: fn.Cost})
			}
		}
	}
	for _, v := range g.visible(func(v *Var) bool { return v.T.Kind == KFunc }) {
		add(target{fv: v, cost: g.costOf(v.T)})
	}
	return out
}

// costOf returns the most a function of the function type t generated so
// far costs: what a call through a value of t may cost.
func (g *Gen) costOf(t *Type) int {
	most := 1
	for _, fn := range g.funcs {
		if valueFunc(fn) && sameType(funcType(fn), t) {
			most = max(most, fn.Cost)
		}
	}
	return most
}

// plain reports whether a call of tg can be an expression: it gives one
// value, and takes nothing as mut or own, nor has a mut receiver.
func (tg target) plain() bool {
	if tg.fv != nil {
		return true
	}
	if tg.fn.ErrResult || tg.fn.Result == nil || tg.recv != nil && tg.fn.Recv.Mut {
		return false
	}
	for _, p := range tg.fn.Params {
		if p.Mut || p.Own {
			return false
		}
	}
	return true
}

// callExpr returns a call, as a value of t, of a target that can be an
// expression.
func (g *Gen) callExpr(t *Type, d int) (Expr, bool) {
	tgs := g.targets(func(tg target) bool {
		r := tg.result()
		return tg.plain() && r != nil && returnsAs(r, t)
	})
	if len(tgs) == 0 {
		return Expr{}, false
	}
	tg := tgs[g.pick(len(tgs))]
	text, eval, ok := g.buildCall(tg, d)
	if !ok {
		return Expr{}, false
	}
	return Expr{Text: text, T: tg.result(), Eval: func(f *frame) Value {
		r, _ := eval(f)
		return r
	}}, true
}

// returnsAs reports whether a result of type r can be used as a t.
func returnsAs(r, t *Type) bool {
	if t.Kind == KInt {
		return r.Kind == KInt && Widens(r.Int, t.Int)
	}
	return r.Kind == t.Kind
}

// buildCall returns the text of a call of tg, with its arguments, and how
// the interpreter runs it, giving its result and its error; and false
// when tg cannot be called here, its list or map argument having no
// variable to name. It counts the call's cost, and marks a variable
// passed as own moved.
func (g *Gen) buildCall(tg target, d int) (string, func(*frame) (Value, Value), bool) {
	callee := 1 << 30
	if tg.fn != nil {
		callee = tg.fn.Index
	}
	var hidden *Var
	if tg.recv != nil && tg.fn.Recv.Mut {
		// No argument may name what is passed as mut.
		hidden = tg.recv
		g.hidden[hidden] = true
	}
	args, owned, ok := g.argsFor(tg.params(), callee, d)
	if hidden != nil {
		delete(g.hidden, hidden)
	}
	if !ok {
		return "", nil, false
	}
	g.cost += g.mult * tg.cost
	if owned != nil {
		owned.Moved = true
	}
	return g.callText(tg, args), g.callEval(tg, args), true
}

// callText returns the text of tg called with args.
func (g *Gen) callText(tg target, args []Expr) string {
	switch {
	case tg.fv != nil:
		tg.fv.Used = true
		return tg.fv.Name + "(" + joinArgs(args) + ")"
	case tg.recv != nil:
		tg.recv.Used = true
		text := tg.recv.Name + "." + tg.fn.Name + "(" + joinArgs(args) + ")"
		if tg.fn.Recv.Mut {
			text = "mut " + text
		}
		return text
	}
	return tg.fn.Name + "(" + joinArgs(args) + ")"
}

// callEval returns how the interpreter runs tg called with args: the
// receiver first, then the arguments, left to right.
func (g *Gen) callEval(tg target, args []Expr) func(*frame) (Value, Value) {
	return func(f *frame) (Value, Value) {
		fn := tg.fn
		var recv *Value
		switch {
		case tg.fv != nil:
			fn = f.get(tg.fv).Fn
		case tg.recv != nil:
			recv = f.ref(tg.recv)
		}
		vals := make([]Value, len(args))
		for i, a := range args {
			vals[i] = a.Eval(f)
		}
		return call(f.m, fn, recv, vals)
	}
}

// argsFor returns arguments for params, the parameters of a function at
// callee among those generated, the variable passed as own, if any, and
// whether there are arguments for all of them: a list or a map is a
// variable of its type, passed as mut or own when its parameter is, and
// every other argument then names no list or map, so none overlaps it; a
// view is an array, a list or a slice of an array; a function value is a
// function generated before the callee, so calls never go round in a
// cycle.
func (g *Gen) argsFor(params []*Var, callee, d int) ([]Expr, *Var, bool) {
	owned, ok := g.ownedArg(params)
	if !ok {
		return nil, nil, false
	}
	saved := g.noLists
	g.noLists = g.noLists || owned != nil
	defer func() { g.noLists = saved }()
	args := make([]Expr, len(params))
	var moved *Var
	for i, p := range params {
		switch {
		case p.T.owned() || p.T.Kind == KView:
			args[i] = g.ownedArgExpr(owned, p)
			if p.Own {
				moved = owned
			}
		case p.T.Kind == KFunc:
			args[i] = g.funcArg(p.T, callee)
		default:
			args[i] = g.valueOf(p.T, d+1)
		}
	}
	return args, moved, true
}

// ownedArgExpr returns v passed for p, a list, map or view parameter: a
// view, now and then, a slice of an array, with constant bounds the
// compiler checks against its length.
func (g *Gen) ownedArgExpr(v, p *Var) Expr {
	if p.T.Kind == KView && v.T.Kind == KArray && g.pick(2) == 0 {
		lo := g.pick(v.T.Len + 1)
		v.Used = true
		return viewOf(v, lo, lo+g.pick(v.T.Len-lo+1))
	}
	return ownedExpr(v, p)
}

// ownedArg returns the variable to pass for the list, map or view
// parameter of params, nil when there is none, and whether there is one
// that may be passed so: as mut, a writable variable; as own, a local, or
// an own parameter, declared inside the innermost loop, so the move is
// never repeated.
func (g *Gen) ownedArg(params []*Var) (*Var, bool) {
	for _, p := range params {
		if !p.T.owned() && p.T.Kind != KView {
			continue
		}
		if g.noLists {
			return nil, false
		}
		v := g.choose(g.visible(func(v *Var) bool { return g.passable(v, p) }))
		return v, v != nil
	}
	return nil, true
}

// passable reports whether v can be passed for the parameter p.
func (g *Gen) passable(v, p *Var) bool {
	switch {
	case p.T.Kind == KView:
		return viewable(v, p.T)
	case !sameType(v.T, p.T):
		return false
	case p.Mut:
		return g.writable(v)
	case p.Own:
		return (!v.Param || v.Own) && !g.frozen[v] && v.Loops == g.loops && !g.inDefer
	}
	return true
}

// viewable reports whether v, an array or a list, can be passed to a view
// parameter of type t, of the same element type.
func viewable(v *Var, t *Type) bool {
	return (v.T.Kind == KArray || v.T.Kind == KList) && sameType(v.T.Elem, t.Elem)
}

// ownedExpr returns v passed for p: as mut or own when p is, and whole,
// or sliced, for a view.
func ownedExpr(v, p *Var) Expr {
	v.Used = true
	text := v.Name
	switch {
	case p.Mut:
		text = "mut " + v.Name
	case p.Own:
		text = "own " + v.Name
	}
	if p.T.Kind == KView {
		return viewOf(v, -1, -1)
	}
	return Expr{Text: text, T: v.T, Eval: func(f *frame) Value { return f.get(v) }}
}

// viewOf returns v, an array or a list, passed to a view parameter whole,
// or, for lo not -1, sliced, v[lo:hi], as the interpreter holds a view: a
// copy of its elements, which nothing changes while the call borrows
// them.
func viewOf(v *Var, lo, hi int) Expr {
	text := v.Name
	if lo >= 0 {
		text = v.Name + "[" + strconv.Itoa(lo) + ":" + strconv.Itoa(hi) + "]"
	}
	return Expr{Text: text, T: &Type{Kind: KView, Elem: v.T.Elem}, Eval: func(f *frame) Value {
		val := f.get(v)
		src := val.Elems
		if val.List != nil {
			src = *val.List
		}
		if lo >= 0 {
			src = src[lo:hi]
		}
		return Value{Elems: append([]Value{}, src...)}
	}}
}

// funcArg returns a function generated before callee, of the function
// type t, as a value.
func (g *Gen) funcArg(t *Type, callee int) Expr {
	var opts []*Func
	for _, fn := range g.funcs {
		if fn.Index < callee && valueFunc(fn) && sameType(funcType(fn), t) {
			opts = append(opts, fn)
		}
	}
	fn := opts[g.pick(len(opts))]
	return Expr{Text: fn.Name, T: t, Eval: func(*frame) Value { return Value{Fn: fn, Valid: true} }}
}

// valueFunc reports whether fn can be a value the generator passes and
// calls: a function, not a method, of integer and bool parameters, giving
// one integer or bool.
func valueFunc(fn *Func) bool {
	if fn.Recv != nil || fn.ErrResult || fn.Result == nil {
		return false
	}
	for _, p := range fn.Params {
		if p.T.Kind != KInt && p.T.Kind != KBool {
			return false
		}
	}
	return true
}

// funcType returns the function type of fn.
func funcType(fn *Func) *Type {
	t := &Type{Kind: KFunc, Result: fn.Result}
	for _, p := range fn.Params {
		t.Params = append(t.Params, p.T)
	}
	return t
}

// callStmt calls a target as a statement: one that gives no value, or one
// that takes something as mut or own, has a mut receiver or returns an
// error, whose results are observed.
func (g *Gen) callStmt() Stmt {
	tgs := g.targets(func(tg target) bool { return !tg.plain() || tg.result() == nil })
	if len(tgs) == 0 {
		return g.obsStmt()
	}
	tg := tgs[g.pick(len(tgs))]
	text, eval, ok := g.buildCall(tg, 0)
	if !ok {
		return g.obsStmt()
	}
	switch {
	case tg.fn != nil && tg.fn.ErrResult:
		return g.errCall(tg.fn.Result, text, eval)
	case tg.result() != nil:
		return g.observe(Expr{Text: text, T: tg.result(), Eval: func(f *frame) Value {
			r, _ := eval(f)
			return r
		}})
	}
	return simple(func(f *frame) { eval(f) }, text)
}

// errCall returns v, e := call, of a function returning a result of type
// t and an error, then observes e's code when it failed, or v when not,
// which the else proves may be read.
func (g *Gen) errCall(t *Type, text string, eval func(*frame) (Value, Value)) Stmt {
	v := &Var{Name: g.name("r"), T: t, Used: true}
	e := &Var{Name: g.name("e"), T: errType, Used: true}
	g.declare(v)
	g.declare(e)
	// v may be read only where e is proven checked, which the else below
	// is and nowhere after it is: no later expression names it.
	g.hidden[v] = true
	bind := simple(func(f *frame) {
		r, err := eval(f)
		f.set(v, r)
		f.set(e, err)
	}, v.Name+", "+e.Name+" := "+text)
	code := Expr{Text: e.Name + ".code", T: intTypeOf(Int32), Eval: func(f *frame) Value {
		return Value{Bits: f.get(e).Bits}
	}}
	cond := Expr{Text: e.Name + ".code != 0", T: boolType, Eval: func(f *frame) Value {
		return Value{Bool: f.get(e).Bits != 0}
	}}
	read := Expr{Text: v.Name, T: t, Eval: func(f *frame) Value { return f.get(v) }}
	check := ifOf(cond, []Stmt{g.observe(code)}, []Stmt{g.observe(read)}, true)
	return seq([]Stmt{bind, check})
}

// errReturn returns from a function returning an error with a failure:
// its result type's zero value and an error of code code.
func (g *Gen) errReturn(code int) Stmt {
	cs := strconv.Itoa(code)
	z := "0"
	if g.fn.Result.Kind == KBool {
		z = "false"
	}
	return Stmt{
		Lines: []string{"return " + z + ", error{code: " + cs + ", msg: \"e" + cs + "\"}"},
		Exec: func(f *frame) ctrl {
			f.ret, f.retErr = Value{}, Value{Bits: uint64(code)} //nolint:gosec // small
			return cReturn
		},
	}
}
