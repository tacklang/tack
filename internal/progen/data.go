package progen

import "strconv"

// dataKinds are the statements on arrays, lists, maps and strings.
var dataKinds []func(g *Gen) Stmt

// init fills dataKinds, which refers to functions that, through the
// statements they nest, refer back to it.
func init() {
	dataKinds = []func(g *Gen) Stmt{
		(*Gen).newCollection,
		(*Gen).guardedRead,
		(*Gen).guardedWrite,
		(*Gen).checkedRead,
		(*Gen).appendStmt,
		(*Gen).popStmt,
		(*Gen).putStmt,
		(*Gen).mapReadStmt,
		(*Gen).deleteStmt,
		(*Gen).hasStmt,
		(*Gen).rangeStmt,
		(*Gen).swapStmt,
	}
}

// dataNames name dataKinds, in order, for Program.Stats.
var dataNames = []string{
	"new-collection", "guarded-read", "guarded-write", "checked-read", "append", "pop", "put",
	"map-read", "delete", "has", "range", "swap",
}

// dataStmt returns a statement on a collection.
func (g *Gen) dataStmt() Stmt {
	k := g.pick(len(dataKinds))
	g.stats[dataNames[k]]++
	return dataKinds[k](g)
}

// listOfInt64 and mapOfInt64 are the list and map types the generator
// writes.
var (
	listOfInt64 = &Type{Kind: KList, Elem: int64Type}
	mapOfInt64  = &Type{Kind: KMap, Key: int64Type, Elem: int64Type}
)

// newCollection declares an empty list or map.
func (g *Gen) newCollection() Stmt {
	t := listOfInt64
	if g.pick(2) == 1 {
		t = mapOfInt64
	}
	v := &Var{Name: g.name("l"), T: t, Mut: true}
	g.declare(v)
	return simple(func(f *frame) { f.set(v, zero(t)) },
		"mut "+v.Name+" := make("+t.String()+", c_allocator)")
}

// indexed returns a variable that can be indexed, of a kind keep allows.
func (g *Gen) indexed(keep func(*Var) bool) *Var {
	return g.choose(g.visible(func(v *Var) bool {
		switch v.T.Kind {
		case KArray:
			return v.T.Elem.Kind != KStruct && keep(v)
		case KList, KString, KView:
			return keep(v)
		}
		return false
	}))
}

// indexVar returns an int64 variable to index with.
func (g *Gen) indexVar() *Var {
	return g.choose(g.visible(func(v *Var) bool { return v.T.Kind == KInt && v.T.Int == Int64 }))
}

// elemType returns the type of an element of v: an array's or a list's
// element type, or a string's byte.
func elemType(v *Var) *Type {
	if v.T.Kind == KString {
		return intTypeOf(Uint8)
	}
	return v.T.Elem
}

// elemAt returns v[i] for i the variable iv, which the caller proved in
// range.
func elemAt(v, iv *Var) Expr {
	return Expr{Text: v.Name + "[" + iv.Name + "]", T: elemType(v), Eval: func(f *frame) Value {
		return elemOf(f.get(v), int(asInt64(f.get(iv).Bits)))
	}}
}

// elemOf returns element i of an array, a list or a string.
func elemOf(v Value, i int) Value {
	switch {
	case v.List != nil:
		return (*v.List)[i]
	case v.Elems != nil:
		return v.Elems[i]
	}
	return Value{Bits: uint64(v.S[i])}
}

// guard returns the condition 0 <= i && i < len(v), which proves v[i] in
// range where it holds.
func guard(v, iv *Var) Expr {
	iv.Used, v.Used = true, true
	return Expr{
		Text: "0 <= " + iv.Name + " && " + iv.Name + " < len(" + v.Name + ")",
		T:    boolType,
		Eval: func(f *frame) Value {
			i := asInt64(f.get(iv).Bits)
			n := int64(lenOfAny(v, f.get(v)))
			return Value{Bool: 0 <= i && i < n}
		},
	}
}

// lenOfAny returns the length of v, a variable of an array, a list, a map
// or a string, holding val.
func lenOfAny(v *Var, val Value) int {
	if v.T.Kind == KArray {
		return v.T.Len
	}
	return lengthOf(val)
}

// guardedRead returns if 0 <= i && i < len(v) { obs(v[i]) }.
func (g *Gen) guardedRead() Stmt {
	v, iv := g.indexed(func(*Var) bool { return true }), g.indexVar()
	if v == nil || iv == nil {
		return g.newCollection()
	}
	return ifOf(guard(v, iv), []Stmt{g.observe(elemAt(v, iv))}, nil, false)
}

// guardedWrite returns if 0 <= i && i < len(v) { v[i] = x }, v a writable
// array or list.
func (g *Gen) guardedWrite() Stmt {
	v := g.indexed(func(v *Var) bool { return v.T.Kind != KString && g.writable(v) })
	iv := g.indexVar()
	if v == nil || iv == nil {
		return g.newCollection()
	}
	savedLists := g.noLists
	g.noLists = true
	x := g.valueOf(v.T.Elem, 1)
	g.noLists = savedLists
	xe := x.Eval
	write := simple(func(f *frame) {
		i := int(asInt64(f.get(iv).Bits))
		val := xe(f)
		if l := f.get(v).List; l != nil {
			(*l)[i] = val
			return
		}
		f.ref(v).Elems[i] = val
	}, v.Name+"["+iv.Name+"] = "+x.Text)
	return ifOf(guard(v, iv), []Stmt{write}, nil, false)
}

// checkedRead returns x, ok := v[i], for any i.
func (g *Gen) checkedRead() Stmt {
	v := g.indexed(func(*Var) bool { return true })
	if v == nil {
		return g.newCollection()
	}
	v.Used = true
	savedLists := g.noLists
	g.noLists = true
	i := g.intOperand(Int64, 1)
	g.noLists = savedLists
	if i.Konst {
		// A constant index is checked at compile time, in the checked form
		// too: it must not be negative, nor past an array's length.
		n := 4
		if v.T.Kind == KArray {
			n = v.T.Len
		}
		i = litExpr(Int64, uint64(g.pick(n))) //nolint:gosec // small
	}
	x := &Var{Name: g.name("x"), T: elemType(v)}
	okv := &Var{Name: g.name("ok"), T: boolType}
	g.declare(x)
	g.declare(okv)
	ie := i.Eval
	return simple(func(f *frame) {
		n := asInt64(ie(f).Bits)
		val := f.get(v)
		if 0 <= n && n < int64(lenOfAny(v, val)) {
			f.set(x, copyValue(elemOf(val, int(n))))
			f.set(okv, Value{Bool: true})
			return
		}
		f.set(x, zero(x.T))
		f.set(okv, Value{})
	}, x.Name+", "+okv.Name+" := "+v.Name+"["+i.Text+"]")
}

// failed returns if e.code != 0 { return }, a return when the call that
// gave e failed, which it does only when an allocation fails.
func (g *Gen) failed(e *Var) Stmt {
	cond := Expr{
		Text: e.Name + ".code != 0", T: boolType,
		Eval: func(*frame) Value { return Value{} },
	}
	return ifOf(cond, []Stmt{g.zeroReturn()}, nil, false)
}

// appendStmt appends one or two values to a writable list, and returns
// when the append fails.
func (g *Gen) appendStmt() Stmt {
	l := g.choose(g.visible(func(v *Var) bool { return v.T.Kind == KList && g.writable(v) }))
	if l == nil {
		return g.newCollection()
	}
	l.Used = true
	savedLists := g.noLists
	g.noLists = true
	vals := []Expr{g.intOperand(Int64, 1)}
	if g.pick(2) == 1 {
		vals = append(vals, g.intOperand(Int64, 1))
	}
	g.noLists = savedLists
	e := &Var{Name: g.name("e"), T: errType, Used: true}
	g.declare(e)
	app := simple(func(f *frame) {
		vs := make([]Value, len(vals))
		for i, x := range vals {
			vs[i] = x.Eval(f)
		}
		lst := f.get(l).List
		*lst = append(*lst, vs...)
	}, e.Name+" := append(mut "+l.Name+", "+joinArgs(vals)+")")
	return seq([]Stmt{app, g.failed(e)})
}

// errType is the type of error, which the interpreter never looks into.
var errType = &Type{Kind: KError}

// popStmt pops a writable list's last element, in the checked form, or
// as a statement, which drops it.
func (g *Gen) popStmt() Stmt {
	l := g.choose(g.visible(func(v *Var) bool { return v.T.Kind == KList && g.writable(v) }))
	if l == nil {
		return g.newCollection()
	}
	l.Used = true
	if g.pick(2) == 0 {
		return simple(func(f *frame) {
			lst := f.get(l).List
			if len(*lst) > 0 {
				*lst = (*lst)[:len(*lst)-1]
			}
		}, "pop(mut "+l.Name+")")
	}
	x := &Var{Name: g.name("p"), T: int64Type}
	okv := &Var{Name: g.name("ok"), T: boolType}
	g.declare(x)
	g.declare(okv)
	return simple(func(f *frame) {
		lst := f.get(l).List
		if len(*lst) == 0 {
			f.set(x, Value{})
			f.set(okv, Value{})
			return
		}
		f.set(x, (*lst)[len(*lst)-1])
		f.set(okv, Value{Bool: true})
		*lst = (*lst)[:len(*lst)-1]
	}, x.Name+", "+okv.Name+" := pop(mut "+l.Name+")")
}

// mapVar returns a map variable, a writable one when write is set.
func (g *Gen) mapVar(write bool) *Var {
	return g.choose(g.visible(func(v *Var) bool {
		return v.T.Kind == KMap && (!write || g.writable(v))
	}))
}

// putStmt puts a value under a key of a writable map, and returns when
// the put fails.
func (g *Gen) putStmt() Stmt {
	m := g.mapVar(true)
	if m == nil {
		return g.newCollection()
	}
	m.Used = true
	savedLists := g.noLists
	g.noLists = true
	k, val := g.mapKey(), g.intOperand(Int64, 1)
	g.noLists = savedLists
	e := &Var{Name: g.name("e"), T: errType, Used: true}
	g.declare(e)
	ke, ve := k.Eval, val.Eval
	put := simple(func(f *frame) {
		kb := ke(f).Bits
		vb := ve(f)
		f.get(m).Map.put(kb, vb)
	}, e.Name+" := put(mut "+m.Name+", "+k.Text+", "+val.Text+")")
	return seq([]Stmt{put, g.failed(e)})
}

// mapKey returns a key: most often a small constant, so keys repeat.
func (g *Gen) mapKey() Expr {
	if g.pick(3) != 0 {
		return litExpr(Int64, uint64(g.pick(6))) //nolint:gosec // small
	}
	return g.intOperand(Int64, 1)
}

// mapReadStmt reads a map in the checked form, v, ok := m[k].
func (g *Gen) mapReadStmt() Stmt {
	m := g.mapVar(false)
	if m == nil {
		return g.newCollection()
	}
	m.Used = true
	savedLists := g.noLists
	g.noLists = true
	k := g.mapKey()
	g.noLists = savedLists
	x := &Var{Name: g.name("g"), T: int64Type}
	okv := &Var{Name: g.name("ok"), T: boolType}
	g.declare(x)
	g.declare(okv)
	ke := k.Eval
	return simple(func(f *frame) {
		val, ok := f.get(m).Map.get(ke(f).Bits)
		f.set(x, val)
		f.set(okv, Value{Bool: ok})
	}, x.Name+", "+okv.Name+" := "+m.Name+"["+k.Text+"]")
}

// deleteStmt deletes a key of a writable map, as a statement or in the
// checked form.
func (g *Gen) deleteStmt() Stmt {
	m := g.mapVar(true)
	if m == nil {
		return g.newCollection()
	}
	m.Used = true
	savedLists := g.noLists
	g.noLists = true
	k := g.mapKey()
	g.noLists = savedLists
	ke := k.Eval
	if g.pick(2) == 0 {
		return simple(func(f *frame) { f.get(m).Map.remove(ke(f).Bits) },
			"delete(mut "+m.Name+", "+k.Text+")")
	}
	x := &Var{Name: g.name("d"), T: int64Type}
	okv := &Var{Name: g.name("ok"), T: boolType}
	g.declare(x)
	g.declare(okv)
	return simple(func(f *frame) {
		val, ok := f.get(m).Map.remove(ke(f).Bits)
		f.set(x, val)
		f.set(okv, Value{Bool: ok})
	}, x.Name+", "+okv.Name+" := delete(mut "+m.Name+", "+k.Text+")")
}

// hasStmt returns if has(m, c) { obs(m[c]) }, c a constant, which has
// proves in m where it holds.
func (g *Gen) hasStmt() Stmt {
	m := g.mapVar(false)
	if m == nil {
		return g.newCollection()
	}
	m.Used = true
	c := uint64(g.pick(6)) //nolint:gosec // small
	cs := strconv.FormatUint(c, 10)
	cond := Expr{Text: "has(" + m.Name + ", " + cs + ")", T: boolType, Eval: func(f *frame) Value {
		_, ok := f.get(m).Map.get(c)
		return Value{Bool: ok}
	}}
	read := Expr{Text: m.Name + "[" + cs + "]", T: int64Type, Eval: func(f *frame) Value {
		v, _ := f.get(m).Map.get(c)
		return v
	}}
	return ifOf(cond, []Stmt{g.observe(read)}, nil, false)
}
