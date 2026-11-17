package progen

// The helper declarations the reflection and closure statements need:
// pg_sum, a generic function folding a Data value's integers and bools,
// at any depth, into an int64 with a switch on its type parameter and a
// fields loop; pg_bump, one adding 1 to its int64 and uint8 parts and
// flipping its bools, through mut; and pg_apply, which calls a closure
// passed to its scope parameter.
const (
	sumDecl = `func pg_sum[T Data](v T) int64 {
    mut n := 0
    switch T {
    case int64:
        n = v
    case int8:
        n = int64(v)
    case int16:
        n = int64(v)
    case int32:
        n = int64(v)
    case uint8:
        n = int64(v)
    case uint16:
        n = int64(v)
    case uint32:
        n = int64(v)
    case bool:
        if v {
            n = 1
        }
    default:
    }
    for f := range fields(T) {
        n = n *% 31 +% f.index +% len(f.name) +% pg_sum(v.[f])
    }
    return n
}
`
	bumpDecl = `func pg_bump[T Data](mut v T) {
    switch T {
    case int64:
        v = v +% 1
    case uint8:
        v = v +% 1
    case bool:
        v = !v
    default:
    }
    for f := range fields(T) {
        pg_bump(mut v.[f])
    }
}
`
	applyDecl = `func pg_apply(scope f func(int64) int64, x int64) int64 {
    return f(x)
}
`
)

// reflectStmt makes a Data value with make, folds one into the checksum
// with pg_sum, changes one in place with pg_bump, or runs a fields loop
// over a struct type outside any generic function.
func (g *Gen) reflectStmt() Stmt {
	switch g.pick(4) {
	case 0:
		return g.makeDataStmt()
	case 1:
		return g.sumStmt()
	case 2:
		return g.bumpStmt()
	}
	return g.fieldsLoopStmt()
}

// dataVarType reports whether a variable of t may be passed to pg_sum or
// pg_bump: an integer, a bool or a struct, each Data and copyable.
func dataVarType(t *Type) bool {
	return t.Kind == KInt || t.Kind == KBool || t.Kind == KStruct
}

// makeDataStmt declares a mutable variable of a struct, an integer or a
// bool type, given the type's empty value by make(T, c_allocator), its
// zero value, which allocates nothing; half the time it then changes
// the variable through pg_bump and folds it into the checksum, as
// bumpStmt does, so pg_bump meets every type make declares.
func (g *Gen) makeDataStmt() Stmt {
	t := intTypeOf(intTypes[g.pick(len(intTypes))])
	switch {
	case len(g.structs) > 0 && g.pick(2) == 0:
		t = &Type{Kind: KStruct, Struct: g.structs[g.pick(len(g.structs))]}
	case g.pick(4) == 0:
		t = boolType
	}
	v := &Var{Name: g.name("z"), T: t, Mut: true}
	g.declare(v)
	decl := simple(func(f *frame) { f.set(v, zero(t)) },
		"mut "+v.Name+" := make("+t.String()+", c_allocator)")
	if g.pick(2) == 0 {
		return decl
	}
	return seq([]Stmt{decl, g.bumpOf(v)})
}

// sumStmt folds a variable of an integer, a bool or a struct type into
// the checksum through pg_sum.
func (g *Gen) sumStmt() Stmt {
	v := g.choose(g.visible(func(v *Var) bool { return dataVarType(v.T) }))
	if v == nil {
		return g.makeDataStmt()
	}
	v.Used = true
	g.extra("pg_sum", sumDecl)
	return simple(func(f *frame) { f.m.observe(dataSum(v.T, f.get(v))) },
		"obs(pg_sum("+v.Name+"))")
}

// bumpStmt changes a writable variable of an integer, a bool or a
// struct type in place through pg_bump, then folds it into the checksum
// through pg_sum, so a change the interpreter gets wrong shows at once.
func (g *Gen) bumpStmt() Stmt {
	v := g.choose(g.visible(func(v *Var) bool { return dataVarType(v.T) && g.writable(v) }))
	if v == nil {
		return g.makeDataStmt()
	}
	return g.bumpOf(v)
}

// bumpOf changes v, a writable variable of an integer, a bool or a
// struct type, in place through pg_bump, then folds it into the checksum
// through pg_sum.
func (g *Gen) bumpOf(v *Var) Stmt {
	v.Used = true
	g.extra("pg_bump", bumpDecl)
	g.extra("pg_sum", sumDecl)
	return simple(func(f *frame) {
		dataBump(v.T, f.ref(v))
		f.m.observe(dataSum(v.T, f.get(v)))
	}, "pg_bump(mut "+v.Name+")", "obs(pg_sum("+v.Name+"))")
}

// fieldsLoopStmt runs a fields loop over a struct type, folding each
// field's index and name's length into the checksum and, given a
// variable of that type, the field itself through pg_sum. The body is
// fixed: a fields loop's body may not break or continue it.
func (g *Gen) fieldsLoopStmt() Stmt {
	if len(g.structs) == 0 {
		return g.makeDataStmt()
	}
	sd := g.structs[g.pick(len(g.structs))]
	t := &Type{Kind: KStruct, Struct: sd}
	f := g.name("f")
	v := g.choose(g.visible(func(v *Var) bool { return v.T.Kind == KStruct && v.T.Struct == sd }))
	val := f + ".index +% len(" + f + ".name)"
	if v != nil {
		v.Used = true
		g.extra("pg_sum", sumDecl)
		val += " +% pg_sum(" + v.Name + ".[" + f + "])"
	}
	g.cost += g.mult * len(sd.Fields)
	return simple(func(fr *frame) {
		var s Value
		if v != nil {
			s = fr.get(v)
		}
		for i, fd := range sd.Fields {
			n := int64(i) + int64(len(fd.Name))
			if v != nil {
				n += dataSum(fd.T, s.Elems[i])
			}
			fr.m.observe(n)
		}
	}, "for "+f+" := range fields("+t.String()+") {", "    obs("+val+")", "}")
}

// dataSum returns what pg_sum gives for val, a value of t: an integer of
// any type but uint64 as an int64, 1 for true and 0 for false and
// uint64, and for a struct its fields folded in order, each as n *% 31
// +% its index +% its name's length +% its own sum, from 0.
func dataSum(t *Type, val Value) int64 {
	var n int64
	switch t.Kind {
	case KInt:
		if t.Int != Uint64 {
			n = asInt64(val.Bits)
		}
	case KBool:
		if val.Bool {
			n = 1
		}
	case KStruct:
		for i, fd := range t.Struct.Fields {
			n = n*31 + int64(i) + int64(len(fd.Name)) + dataSum(fd.T, val.Elems[i])
		}
	}
	return n
}

// dataBump does what pg_bump does to val, a value of t: adds 1, wrapping,
// to an int64 or a uint8, flips a bool, and does the same to each field
// of a struct.
func dataBump(t *Type, val *Value) {
	switch t.Kind {
	case KInt:
		if t.Int == Int64 || t.Int == Uint8 {
			val.Bits = norm(t.Int, val.Bits+1)
		}
	case KBool:
		val.Bool = !val.Bool
	case KStruct:
		for i, fd := range t.Struct.Fields {
			dataBump(fd.T, &val.Elems[i])
		}
	}
}

// closureStmt folds into the checksum what pg_apply gives for a closure
// and an int64 operand: a closure that reads a captured integer
// variable, adding it to its parameter, or one that adds its parameter
// to a captured writable int64 variable and returns the sum, the
// capture then folded into the checksum too, so a write the interpreter
// gets wrong shows at once. The operand
// names no captured variable, so when the closure runs, at the call,
// after the operand is evaluated, the capture holds what it held before
// the statement.
func (g *Gen) closureStmt() Stmt {
	g.extra("pg_apply", applyDecl)
	a := g.name("a")
	head := "obs(pg_apply(func(" + a + " int64) int64 {"
	if c := g.choose(g.visible(func(v *Var) bool {
		return v.T.Kind == KInt && v.T.Int == Int64 && g.writable(v)
	})); c != nil && g.pick(2) == 0 {
		c.Used = true
		x := g.hiddenOperand(c)
		return simple(func(f *frame) {
			xv := x.Eval(f).Bits
			r := arith(Int64, "+%", f.get(c).Bits, xv)
			f.set(c, Value{Bits: r})
			f.m.observe(asInt64(r))
			f.m.observe(asInt64(f.get(c).Bits))
		}, head, "    "+c.Name+" = "+c.Name+" +% "+a, "    return "+c.Name, "}, "+x.Text+"))",
			"obs("+c.Name+")")
	}
	c := g.choose(g.visible(func(v *Var) bool {
		return v.T.Kind == KInt && Widens(v.T.Int, Int64)
	}))
	if c == nil {
		x := g.intOperand(Int64, 1)
		return simple(func(f *frame) { f.m.observe(asInt64(x.Eval(f).Bits)) },
			head+" return "+a+" }, "+x.Text+"))")
	}
	c.Used = true
	x := g.hiddenOperand(c)
	return simple(func(f *frame) {
		xv := x.Eval(f).Bits
		f.m.observe(asInt64(arith(Int64, "+%", xv, f.get(c).Bits)))
	}, head+" return "+a+" +% "+c.Name+" }, "+x.Text+"))")
}

// hiddenOperand returns an int64 operand that does not name c.
func (g *Gen) hiddenOperand(c *Var) Expr {
	g.hidden[c] = true
	x := g.intOperand(Int64, 1)
	delete(g.hidden, c)
	return x
}
