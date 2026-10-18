package progen

import "strconv"

// stmtKinds are the statement generators, the simplest first, which is
// what a Source that has run out picks.
var stmtKinds []func(g *Gen) Stmt

// init fills stmtKinds, which refers to functions that, through the
// statements they nest, refer back to it.
func init() {
	stmtKinds = []func(g *Gen) Stmt{
		(*Gen).obsStmt,
		(*Gen).declStmt,
		(*Gen).assignStmt,
		(*Gen).compoundStmt,
		(*Gen).incStmt,
		(*Gen).ifStmt,
		(*Gen).loopStmt,
		(*Gen).divStmt,
		(*Gen).convStmt,
		(*Gen).dataStmt,
		(*Gen).callStmt,
		(*Gen).controlStmt,
		(*Gen).unionStmt,
		(*Gen).funcValueStmt,
		(*Gen).deferStmt,
		(*Gen).stdStmt,
	}
}

// stmtNames name stmtKinds, in order, for Program.Stats.
var stmtNames = []string{
	"obs", "decl", "assign", "compound", "incdec", "if", "loop", "div", "conv", "data", "call",
	"control", "union", "funcval", "defer", "std",
}

// dataKind is the index of dataStmt in stmtKinds.
const dataKind = 9

// stmt returns one statement, counting its cost.
func (g *Gen) stmt() Stmt {
	g.cost += g.mult
	k := g.pick(len(stmtKinds))
	if g.rare(4) {
		k = dataKind // collections are worth more than one kind's share
	}
	if g.inDefer && k > deferSafe {
		k = g.pick(deferSafe + 1)
	}
	g.stats[stmtNames[k]]++
	return stmtKinds[k](g)
}

// simple returns a statement of lines that the interpreter runs as exec.
func simple(exec func(*frame), lines ...string) Stmt {
	return Stmt{Lines: lines, Exec: func(f *frame) ctrl {
		exec(f)
		return cNormal
	}}
}

// obsStmt folds a value into the checksum.
func (g *Gen) obsStmt() Stmt {
	if g.pick(4) == 3 {
		return g.observe(g.boolExpr(0))
	}
	return g.observe(g.intOperand(intTypes[g.pick(len(intTypes))], 0))
}

// observe folds e, an integer or a bool, into the checksum, with obs, or
// obsu for a uint64, which may not fit an int64, or obsb for a bool.
func (g *Gen) observe(e Expr) Stmt {
	ee := e.Eval
	switch {
	case e.T.Kind == KBool:
		return simple(func(f *frame) {
			if ee(f).Bool {
				f.m.observe(1)
			} else {
				f.m.observe(2)
			}
		}, "obsb("+e.Text+")")
	case e.T.Int == Uint64:
		return simple(func(f *frame) {
			b := ee(f).Bits
			if b > 1<<63-1 {
				f.m.observe(7)
			} else {
				f.m.observe(asInt64(b))
			}
		}, "obsu("+e.Text+")")
	}
	return simple(func(f *frame) { f.m.observe(asInt64(ee(f).Bits)) }, "obs("+e.Text+")")
}

// observeVar reads v into the checksum, as fits its type.
func (g *Gen) observeVar(v *Var) Stmt {
	switch v.T.Kind {
	case KInt, KBool:
		return g.observe(varExpr(v))
	case KStruct:
		return g.observeFields(v)
	case KArray:
		if v.T.Len > 0 && v.T.Elem.Kind != KStruct {
			v.Used = true
			return g.observe(Expr{
				Text: v.Name + "[0]", T: v.T.Elem,
				Eval: func(f *frame) Value { return f.get(v).Elems[0] },
			})
		}
	case KUnion:
		return g.switchOn(v, true)
	case KFunc:
		return g.observeFunc(v)
	}
	v.Used = true
	return g.observe(Expr{Text: "len(" + v.Name + ")", T: int64Type, Eval: func(f *frame) Value {
		return Value{Bits: uint64(lenOfAny(v, f.get(v)))} //nolint:gosec // small
	}})
}

// observeFields reads every field of v, a struct, into the checksum.
func (g *Gen) observeFields(v *Var) Stmt {
	v.Used = true
	stmts := make([]Stmt, 0, len(v.T.Struct.Fields))
	for i, fd := range v.T.Struct.Fields {
		e := Expr{
			Text: v.Name + "." + fd.Name, T: fd.T,
			Eval: func(f *frame) Value { return f.get(v).Elems[i] },
		}
		stmts = append(stmts, g.observe(e))
	}
	return seq(stmts)
}

// seq returns stmts run in order as one statement.
func seq(stmts []Stmt) Stmt {
	var lines []string
	for _, s := range stmts {
		lines = append(lines, s.Lines...)
	}
	return Stmt{Lines: lines, Exec: func(f *frame) ctrl { return runSeq(f, stmts) }}
}

// scalarType returns a copyable type for a new variable.
func (g *Gen) scalarType() *Type {
	switch g.pick(8) {
	case 0, 1, 2:
		return intTypeOf(intTypes[g.pick(len(intTypes))])
	case 3:
		return boolType
	case 4:
		return stringType
	case 5:
		if len(g.structs) > 0 {
			return &Type{Kind: KStruct, Struct: g.structs[g.pick(len(g.structs))]}
		}
	case 6:
		if len(g.unions) > 0 {
			return &Type{Kind: KUnion, Union: g.unions[g.pick(len(g.unions))]}
		}
	}
	elem := int64Type // what a view parameter takes, most often
	if g.pick(2) == 0 {
		elem = intTypeOf(intTypes[g.pick(len(intTypes))])
	}
	return &Type{Kind: KArray, Len: 1 + g.pick(4), Elem: elem}
}

// declStmt declares a mutable variable of a copyable type with a value,
// or, one time in four, an empty list or map.
func (g *Gen) declStmt() Stmt {
	if g.rare(4) {
		return g.newCollection()
	}
	t := g.scalarType()
	e := g.valueOf(t, 0)
	v := &Var{Name: g.name("v"), T: t, Mut: true}
	g.declare(v)
	ee := e.Eval
	return simple(func(f *frame) { f.set(v, ee(f)) },
		"mut "+v.Name+" "+t.String()+" = "+e.Text)
}

// assignStmt assigns a writable variable, or a field of one, a new value.
func (g *Gen) assignStmt() Stmt {
	v := g.choose(g.visible(func(v *Var) bool {
		return g.writable(v) && !v.T.owned() &&
			v.T.Kind != KFunc
	}))
	if v == nil {
		return g.declStmt()
	}
	if v.T.Kind == KStruct && g.pick(2) == 0 {
		i := g.pick(len(v.T.Struct.Fields))
		fd := v.T.Struct.Fields[i]
		e := g.valueOf(fd.T, 0)
		ee := e.Eval
		return simple(func(f *frame) {
			val := ee(f)
			f.ref(v).Elems[i] = val
		}, v.Name+"."+fd.Name+" = "+e.Text)
	}
	e := g.valueOf(v.T, 0)
	if e.Text == v.Name {
		// x = x lowers to a C self-assignment, which clang warns of
		// (finding: see TestFindingSelfAssignWarns).
		return g.observeVar(v)
	}
	ee := e.Eval
	return simple(func(f *frame) { f.set(v, ee(f)) }, v.Name+" = "+e.Text)
}

// compoundOps are the compound assignments of an integer by a value.
var compoundOps = []string{"+", "-", "*", "&", "|", "^", "&^"}

// compoundStmt changes a writable integer variable with a compound
// assignment, a shift's by a count.
func (g *Gen) compoundStmt() Stmt {
	v := g.choose(g.visible(func(v *Var) bool { return g.writable(v) && v.T.Kind == KInt }))
	if v == nil {
		return g.declStmt()
	}
	it := v.T.Int
	if g.pick(4) == 0 {
		op := []string{"<<", ">>"}[g.pick(2)]
		n := g.shiftCount(it)
		ne := n.Eval
		return simple(func(f *frame) {
			c := ne(f).Bits
			p := f.ref(v)
			p.Bits = shift(it, op, p.Bits, c)
		}, v.Name+" "+op+"= "+n.Text)
	}
	op := compoundOps[g.pick(len(compoundOps))]
	e := g.intOperand(it, 1)
	if e.Konst {
		e = g.intLit(it)
	}
	ee := e.Eval
	return simple(func(f *frame) {
		x := ee(f).Bits
		p := f.ref(v)
		p.Bits = arith(it, op, p.Bits, x)
	}, v.Name+" "+op+"= "+e.Text)
}

// incStmt increments or decrements a writable integer variable.
func (g *Gen) incStmt() Stmt {
	v := g.choose(g.visible(func(v *Var) bool { return g.writable(v) && v.T.Kind == KInt }))
	if v == nil {
		return g.declStmt()
	}
	op, aop := "++", "+"
	if g.pick(2) == 0 {
		op, aop = "--", "-"
	}
	it := v.T.Int
	return simple(func(f *frame) {
		p := f.ref(v)
		p.Bits = arith(it, aop, p.Bits, 1)
	}, v.Name+op)
}

// ifStmt returns an if, with an else branch or not.
func (g *Gen) ifStmt() Stmt {
	cond := g.boolExpr(0)
	then := g.block()
	var els []Stmt
	hasElse := g.pick(2) == 1
	if hasElse {
		els = g.block()
	}
	return ifOf(cond, then, els, hasElse)
}

// ifOf returns if cond { then } else { els }, the else left out unless
// hasElse.
func ifOf(cond Expr, then, els []Stmt, hasElse bool) Stmt {
	ce := cond.Eval
	lines := indent("if "+cond.Text+" {", then)
	if hasElse {
		lines = append(lines[:len(lines)-1], indent("} else {", els)...)
	}
	return Stmt{Lines: lines, Exec: func(f *frame) ctrl {
		if ce(f).Bool {
			return execBlock(f, then)
		}
		return execBlock(f, els)
	}}
}

// loopStmt returns a loop of a constant number of trips: a range over a
// count, a three-clause for, or a for with a condition on a counter.
func (g *Gen) loopStmt() Stmt {
	trips := g.pick(maxTrips + 1)
	switch g.pick(3) {
	case 0:
		return g.rangeLoop(trips)
	case 1:
		return g.clauseLoop(trips)
	}
	return g.whileLoop(trips)
}

// enterLoop accounts for a loop of trips around the code generated until
// the returned function is called.
func (g *Gen) enterLoop(trips int) func() {
	old := g.mult
	g.mult *= max(trips, 1)
	g.loops++
	inSwitch := g.inSwitch
	g.inSwitch = false
	return func() {
		g.mult = old
		g.loops--
		g.inSwitch = inSwitch
	}
}

// loopBody runs body once, as a loop's trip does, and reports whether
// the loop goes on, and how it ended when it does not.
func loopBody(f *frame, body []Stmt) (bool, ctrl) {
	switch c := execBlock(f, body); c {
	case cBreak:
		return false, cNormal
	case cReturn:
		return false, cReturn
	}
	return true, cNormal
}

// rangeLoop returns for k := range trips { body }.
func (g *Gen) rangeLoop(trips int) Stmt {
	done := g.enterLoop(trips)
	g.push()
	k := &Var{Name: g.name("k"), T: int64Type}
	g.declare(k)
	body := g.blockIn()
	body = append(body, g.pop()...)
	done()
	return Stmt{
		Lines: indent("for "+k.Name+" := range "+strconv.Itoa(trips)+" {", body),
		Exec: func(f *frame) ctrl {
			for i := range trips {
				f.set(k, Value{Bits: uint64(i)}) //nolint:gosec // small
				if more, c := loopBody(f, body); !more {
					return c
				}
			}
			return cNormal
		},
	}
}

// blockIn generates statements into the innermost scope, which the caller
// opened and closes: up to maxTopStmts for a function's body, maxStmts
// for any block nested in it.
func (g *Gen) blockIn() []Stmt {
	limit := maxStmts
	if g.depth == 1 {
		limit = maxTopStmts
	}
	var stmts []Stmt
	for g.depth <= maxDepth && g.more(len(stmts), limit) {
		stmts = append(stmts, g.stmt())
	}
	return stmts
}

// clauseLoop returns for mut i := 0; i < trips; i++ { body }, which never
// writes i.
func (g *Gen) clauseLoop(trips int) Stmt {
	done := g.enterLoop(trips)
	g.push()
	i := &Var{Name: g.name("i"), T: int64Type, Mut: true}
	g.declare(i)
	g.frozen[i] = true
	body := g.blockIn()
	body = append(body, g.pop()...)
	done()
	head := "for mut " + i.Name + " := 0; " + i.Name + " < " + strconv.Itoa(trips) + "; " +
		i.Name + "++ {"
	return Stmt{
		Lines: indent(head, body),
		Exec: func(f *frame) ctrl {
			for n := range trips {
				f.set(i, Value{Bits: uint64(n)}) //nolint:gosec // small
				if more, c := loopBody(f, body); !more {
					return c
				}
			}
			return cNormal
		},
	}
}

// whileLoop returns mut w := 0, then for w < trips { w++; body }, which
// counts first, so a continue cannot skip it, and never writes w after.
func (g *Gen) whileLoop(trips int) Stmt {
	w := &Var{Name: g.name("w"), T: int64Type, Mut: true, Used: true}
	g.declare(w)
	g.frozen[w] = true
	done := g.enterLoop(trips)
	body := g.block()
	done()
	inner := append([]Stmt{{Lines: []string{w.Name + "++"}}}, body...)
	lines := append([]string{"mut " + w.Name + " := 0"},
		indent("for "+w.Name+" < "+strconv.Itoa(trips)+" {", inner)...)
	return Stmt{Lines: lines, Exec: func(f *frame) ctrl {
		f.set(w, Value{})
		for n := range trips {
			f.set(w, Value{Bits: uint64(n + 1)}) //nolint:gosec // small
			if more, c := loopBody(f, body); !more {
				return c
			}
		}
		return cNormal
	}}
}
