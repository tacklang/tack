package progen

import "strconv"

// rangeStmt ranges over an array, a list or a map, which the body never
// writes.
func (g *Gen) rangeStmt() Stmt {
	v := g.choose(g.visible(func(v *Var) bool {
		return v.T.Kind == KArray && v.T.Elem.Kind != KStruct || v.T.owned() || v.T.Kind == KView
	}))
	if v == nil {
		return g.newCollection()
	}
	v.Used = true
	trips := assumedLen
	if v.T.Kind == KArray {
		trips = v.T.Len
	}
	done := g.enterLoop(trips)
	g.push()
	keyT, valT := int64Type, v.T.Elem
	k := &Var{Name: g.name("k"), T: keyT}
	x := &Var{Name: g.name("x"), T: valT}
	g.declare(k)
	g.declare(x)
	wasFrozen := g.frozen[v]
	g.frozen[v] = true
	body := g.blockIn()
	body = append(body, g.pop()...)
	g.frozen[v] = wasFrozen
	done()
	head := "for " + k.Name + ", " + x.Name + " := range " + v.Name + " {"
	return Stmt{Lines: indent(head, body), Exec: func(f *frame) ctrl {
		return rangeExec(f, v, k, x, body)
	}}
}

// rangeExec runs a range over v, giving k and x each key or index and
// element, in order: a map's in insertion order.
func rangeExec(f *frame, v, k, x *Var, body []Stmt) ctrl {
	val := f.get(v)
	var keys, elems []Value
	switch {
	case val.Map != nil:
		for _, key := range val.Map.keys {
			e, _ := val.Map.get(key)
			keys, elems = append(keys, Value{Bits: key}), append(elems, e)
		}
	case val.List != nil:
		elems = append(elems, *val.List...)
	default:
		elems = val.Elems
	}
	for i, e := range elems {
		key := Value{Bits: uint64(i)} //nolint:gosec // small
		if keys != nil {
			key = keys[i]
		}
		f.set(k, key)
		f.set(x, copyValue(e))
		if more, c := loopBody(f, body); !more {
			return c
		}
	}
	return cNormal
}

// unionExpr returns a value of the union type t: a variable of it, or one
// built of a case.
func (g *Gen) unionExpr(t *Type, d int) Expr {
	if v := g.choose(g.visible(func(v *Var) bool { return sameType(v.T, t) })); v != nil &&
		g.pick(2) == 0 {
		return varExpr(v)
	}
	u := t.Union
	tag := g.pick(len(u.Cases))
	c := u.Cases[tag]
	if c.Payload == nil {
		return Expr{Text: u.Name + "." + c.Name, T: t, Eval: func(*frame) Value {
			return Value{Tag: tag, Valid: true}
		}}
	}
	p := g.valueOf(c.Payload, d+1)
	pe := p.Eval
	return Expr{
		Text: "(" + u.Name + "." + c.Name + "(" + p.Text + "))", T: t,
		Eval: func(f *frame) Value {
			pv := pe(f)
			return Value{Tag: tag, Pay: &pv, Valid: true}
		},
	}
}

// unionStmt switches on a union variable, or declares one when there is
// none.
func (g *Gen) unionStmt() Stmt {
	v := g.choose(g.visible(func(v *Var) bool { return v.T.Kind == KUnion }))
	if v == nil {
		return g.declStmt()
	}
	return g.switchOn(v, false)
}

// switchOn returns a switch on v, a union variable, with a clause for
// every case, binding the payload of each that has one, or a default for
// the rest. With plain set, each clause only observes which case it is.
func (g *Gen) switchOn(v *Var, plain bool) Stmt {
	v.Used = true
	u := v.T.Union
	wasFrozen, inSwitch := g.frozen[v], g.inSwitch
	g.frozen[v], g.inSwitch = true, true
	defer func() { g.frozen[v], g.inSwitch = wasFrozen, inSwitch }()
	withDefault := !plain && g.pick(3) == 0
	n := len(u.Cases)
	if withDefault {
		n = 1 + g.pick(len(u.Cases))
	}
	clauses := make([]clause, 0, n+1)
	for tag := range n {
		clauses = append(clauses, g.unionClause(u, tag, plain))
	}
	if withDefault && n < len(u.Cases) {
		g.push()
		body := g.clauseBody(plain, -1)
		clauses = append(clauses, clause{tag: -1, head: "default:", body: append(body, g.pop()...)})
	}
	return switchStmt(v, clauses)
}

// clause is one clause of a switch on a union: the case it names, -1 for
// the default, its head, the variable bound to the payload, if any, and
// its body.
type clause struct {
	bind *Var
	head string
	body []Stmt
	tag  int
}

// unionClause returns the clause for case tag of u.
func (g *Gen) unionClause(u *UnionDecl, tag int, plain bool) clause {
	c := u.Cases[tag]
	g.push()
	cl := clause{tag: tag, head: "case " + c.Name + ":"}
	if c.Payload != nil && g.pick(4) != 0 {
		cl.bind = &Var{Name: g.name("b"), T: c.Payload}
		g.declare(cl.bind)
		cl.head = "case " + c.Name + "(" + cl.bind.Name + "):"
	}
	cl.body = append(g.clauseBody(plain, tag), g.pop()...)
	return cl
}

// clauseBody returns a clause's statements: an observation of which case
// it is, then, unless plain, a block's.
func (g *Gen) clauseBody(plain bool, tag int) []Stmt {
	mark := int64(100 + tag)
	first := simple(func(f *frame) { f.m.observe(mark) }, "obs("+strconv.FormatInt(mark, 10)+")")
	if plain {
		return []Stmt{first}
	}
	return append([]Stmt{first}, g.blockIn()...)
}

// switchStmt returns the switch on v with clauses.
func switchStmt(v *Var, clauses []clause) Stmt {
	lines := []string{"switch " + v.Name + " {"}
	for _, cl := range clauses {
		lines = append(lines, cl.head)
		for _, s := range cl.body {
			for _, l := range s.Lines {
				lines = append(lines, "    "+l)
			}
		}
	}
	lines = append(lines, "}")
	return Stmt{Lines: lines, Exec: func(f *frame) ctrl {
		val := f.get(v)
		for _, cl := range clauses {
			if cl.tag == val.Tag || cl.tag == -1 {
				if cl.bind != nil {
					f.set(cl.bind, copyValue(*val.Pay))
				}
				return execBlock(f, cl.body)
			}
		}
		return cNormal
	}}
}
