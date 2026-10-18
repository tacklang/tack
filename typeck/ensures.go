package typeck

import (
	"slices"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// A function's contract, func f(d int64) ensures d != 0, states facts
// about its parameters that hold whenever it returns, which the checker
// proves in its body and gives to its callers.
//
// The clause is checked with the signature (checkContract), before any
// body, so a call can use the contract of a function declared after it,
// in its own package or another. It may hold only what an if's condition
// gives facts through: comparisons of the function's parameters, their
// fields and elements, constants, len and has, with !, &&, ||, + and -,
// and each operand of its top-level && must give at least one fact. An
// own parameter cannot be named, since its argument moves into the call,
// nor anything but a parameter: a global, a function or a type. Its facts
// are those factsFrom gives for the clause being true, each about a
// parameter or a path below one.
//
// In the body, every path that returns must prove each of those facts
// (proveContract): each return, and the end of the body when it can be
// reached, with the facts in force there; a branch ending in exit leaves,
// as everywhere, so an early exit ending in one proves what got past it.
// Code after a statement that leaves, or ends in exit, never runs, and
// proves anything. A return whose one value is a call with a contract
// proves with the facts that call gives. No fact is about a field or an
// element of a mut parameter, so a clause about one, a mut receiver's
// field included, gives no fact, and its error says why (mutPart). Deferred code runs after
// the return, so a defer written before the return whose code writes a
// variable a fact names, through an assignment or a mut argument, fails
// the proof, however the deferred code runs.
//
// At a call, the facts are put in force after the statement the call is,
// or is the whole value of, a declaration or an assignment, of one value
// or several, for the rest of the block, as an early exit's are, each
// with every parameter it names replaced by the argument passed for it
// (contractFacts). A fact counts only when every parameter it names was
// passed a local variable or a parameter of the caller by name, as a
// plain argument that nothing else in the call writes, so it still holds
// the value the callee saw, or as mut, which the callee changed in
// place; a field, an element, an expression, an own argument, a variadic
// parameter's values, an array passed for a view parameter, whose length
// its type already knows, a variable the statement assigns once the
// call is done, and one written anywhere in the statement's targets, as
// a mut argument in an index, drop every fact naming that parameter. The call's own writes,
// to its mut arguments, end the facts in force before it as they would
// anywhere, and the ensured facts, true after it, are put in force after
// that. A call through a function value has no contract: its type says
// nothing of one.

// contract is a function's ensures clause as the checker uses it: the
// facts it gives, each with the operand of the clause's top-level && it
// came from.
type contract struct {
	facts []clauseFact
}

// clauseFact is one fact a contract gives, about the function's
// parameters: the fact, leaf, the operand of the clause's top-level &&
// that gives it, for an error to quote, base, what the fact's subject's
// type, or a length's range, says of it without any fact, and isLen,
// whether that subject is a string, view or list, whose length the fact
// is about.
type clauseFact struct {
	leaf  ast.Expr
	fact  fact
	base  interval
	isLen bool
}

// checkContract checks fn's ensures clause, once its parameters' types
// are known, and records the contract it gives in c.contracts. An extern
// func's is reported: its body is in C, where nothing proves it.
func (c *Checker) checkContract(fn *ast.Func) {
	if fn.Ensures == nil {
		return
	}
	if fn.Extern {
		span := fn.EnsuresSpan
		span.End = fn.Ensures.Spanned().End
		c.errorf(span, "extern func %s cannot have an ensures clause; its body is written in C, "+
			"where nothing proves it", fn.Name)
		return
	}
	before := len(c.diags)
	c.clauseShape(fn, fn.Ensures)
	if len(c.diags) == before {
		c.checkClauseType(fn.Ensures)
	}
	if len(c.diags) > before {
		return
	}
	k := &contract{}
	for _, leaf := range conjuncts(fn.Ensures) {
		k.facts = c.leafFacts(leaf, k.facts)
	}
	if len(c.diags) == before {
		c.contracts[fn] = k
	}
}

// checkClauseType checks clause, an ensures clause of the right shape, as
// an expression, which must be a bool.
func (c *Checker) checkClauseType(clause ast.Expr) {
	outer := c.at
	c.at = clause.Spanned().End
	got := c.valueOf(clause, c.expr(clause))
	c.at = outer
	if got != types.Invalid && got != types.Bool {
		c.errorf(clause.Spanned(), "ensures clause must be a bool, got %s", c.typeName(got))
	}
}

// leafFacts returns facts with the facts leaf, an operand of an ensures
// clause's top-level &&, gives when true added, and reports a leaf that
// gives none.
func (c *Checker) leafFacts(leaf ast.Expr, facts []clauseFact) []clauseFact {
	n := len(facts)
	for _, f := range c.factsFrom(leaf, true) {
		if !f.empty() || !f.inMap.isZero() {
			f.src = nil
			base, isLen := c.baseOf(leaf, f.about)
			facts = append(facts, clauseFact{leaf: leaf, fact: f, base: base, isLen: isLen})
		}
	}
	switch part, p := mutPart(leaf); {
	case len(facts) > n:
	case part != nil:
		c.errorf(leaf.Spanned(), "%s gives no fact; %s is part of the mut parameter %s, and a "+
			"field or an element of a mut parameter carries no facts",
			c.text(leaf.Spanned()), c.text(part.Spanned()), p.Name)
	default:
		c.errorf(leaf.Spanned(), "%s gives no fact; an ensures clause states facts an if's "+
			"condition proves, such as d != 0, 0 <= i && i < len(a), err.code == 0 or has(m, k)",
			c.text(leaf.Spanned()))
	}
	return facts
}

// mutPart returns the first field or element in leaf, an operand of an
// ensures clause, below a mut parameter, with that parameter, or nil: no
// fact is about one (see subject.go), so a clause naming one, a mut
// receiver's field included, ensures nothing of it. An error's code is
// left out, since it carries facts whatever its variable.
func mutPart(leaf ast.Expr) (ast.Expr, *ast.Param) {
	var part ast.Expr
	var param *ast.Param
	ast.Walk(leaf, func(n ast.Node) bool {
		if part != nil {
			return false
		}
		e, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		if p := mutRoot(e); p != nil {
			part, param = e, p
			return false
		}
		return true
	})
	return part, param
}

// mutRoot returns the mut parameter e, a field or an element, is part of,
// or nil for anything else, an error's code included.
func mutRoot(e ast.Expr) *ast.Param {
	switch x := e.(type) {
	case *ast.Selector:
		if x.Name == "code" && ast.TypeOf(x.X) == types.Error {
			return nil
		}
	case *ast.Index:
	default:
		return nil
	}
	if root := ast.Root(e); root != nil {
		if p, ok := root.Def.(*ast.Param); ok && p.Mut {
			return p
		}
	}
	return nil
}

// conjuncts returns the operands of e's top-level &&, in order, or e
// itself when it is no &&. && binds left to right, so a chain nests on
// its left, and is walked down that side in a loop, so that its length
// costs no stack.
func conjuncts(e ast.Expr) []ast.Expr {
	var out []ast.Expr
	for {
		bin, ok := e.(*ast.Binary)
		if !ok || bin.Op != ast.And {
			out = append(out, e)
			break
		}
		out = append(out, conjuncts(bin.Y)...)
		e = bin.X
	}
	// The right operands were found last first, each in order.
	slices.Reverse(out)
	return out
}

// clauseShape reports each part of e, fn's ensures clause or a part of
// it, that a clause cannot hold: a name of anything but one of fn's
// parameters, an own parameter, a call of anything but len and has, and
// any expression but a comparison, !, &&, ||, + and - on operands of
// those, a field, an element, or a literal. A name the resolver could
// not bind was reported there.
func (c *Checker) clauseShape(fn *ast.Func, e ast.Expr) {
	switch x := e.(type) {
	case *ast.Ident:
		c.clauseName(fn, x)
	case *ast.IntLit, *ast.FloatLit, *ast.CharLit, *ast.StrLit, *ast.BoolLit:
	case *ast.Selector:
		c.clauseShape(fn, x.X)
	case *ast.Index:
		c.clauseShape(fn, x.X)
		c.clauseShape(fn, x.Index)
	case *ast.Unary:
		c.clauseOperands(fn, x, x.Op == ast.Not || x.Op == ast.Neg, x.X)
	case *ast.Binary:
		c.clauseOperands(fn, x, clauseOp(x.Op), x.X, x.Y)
	case *ast.Call:
		c.clauseCall(fn, x)
	default:
		c.clauseCannotHold(e)
	}
}

// clauseOp reports whether an ensures clause may hold the binary operator
// op: a comparison, &&, ||, + or -.
func clauseOp(op ast.BinaryOp) bool {
	switch op {
	case ast.And, ast.Or, ast.Add, ast.Sub:
		return true
	default:
		return op.IsComparison()
	}
}

// clauseCall checks the shape of call, a part of fn's ensures clause,
// which may call only len and has.
func (c *Checker) clauseCall(fn *ast.Func, call *ast.Call) {
	if !isLen(call) && !isHas(call) {
		name := c.text(call.Fn.Spanned())
		c.errorf(call.Fn.Spanned(), "an ensures clause can call only len and has, not %s", name)
		return
	}
	c.clauseOperands(fn, call, true, call.Args...)
}

// clauseOperands checks the shape of operands, those of e, a part of fn's
// ensures clause, when ok says e itself may be in a clause, and reports e
// when it may not.
func (c *Checker) clauseOperands(fn *ast.Func, e ast.Expr, ok bool, operands ...ast.Expr) {
	if !ok {
		c.clauseCannotHold(e)
		return
	}
	for _, x := range operands {
		c.clauseShape(fn, x)
	}
}

// clauseName reports id, a name in fn's ensures clause, unless it names
// one of fn's parameters that is not own, or names nothing because of an
// error already reported.
func (c *Checker) clauseName(fn *ast.Func, id *ast.Ident) {
	p, isParam := id.Def.(*ast.Param)
	switch {
	case id.Def == ast.Unbound:
	case isParam && slices.Contains(fn.Params, p) && p.Own:
		c.errorf(id.Span, "an ensures clause cannot name own parameter %s: its argument moves "+
			"into the call, so nothing it ensures about %s reaches the caller", p.Name, p.Name)
	case isParam && slices.Contains(fn.Params, p):
	default:
		c.errorf(id.Span, "an ensures clause can name only the parameters of %s, and %s is not one",
			c.displayName(fn), id.Qualified())
	}
}

// clauseCannotHold reports e, a part of an ensures clause no fact can be
// learned through.
func (c *Checker) clauseCannotHold(e ast.Expr) {
	c.errorf(e.Spanned(), "an ensures clause cannot hold %s; it compares parameters, their "+
		"fields and elements, constants, len and has, with !, &&, ||, + and -", c.text(e.Spanned()))
}

// baseOf returns what is known of the subject k, named in leaf, without
// any fact: the range of a length, of an error's code, or of the
// subject's integer type, or nothing for any other, and whether k is a
// length.
func (c *Checker) baseOf(leaf ast.Expr, k factKey) (interval, bool) {
	var base interval
	found, length := false, false
	ast.Walk(leaf, func(n ast.Node) bool {
		e, ok := n.(ast.Expr)
		if found || !ok {
			return !found
		}
		got, ok := c.subject(e)
		if !ok || got != k {
			return true
		}
		found = true
		_, isLen := c.stringVar(e)
		_, isLenCall := c.stringLen(e)
		_, isCode := c.errorCodeKey(e)
		switch {
		case isLen || isLenCall:
			base, length = lengthRange(), true
		case isCode:
			base = codeInterval()
		default:
			base = typeInterval(e)
		}
		return false
	})
	return base, length
}

// proveContract reports each fact fn's contract gives that the facts in
// force do not prove where fn returns, at a return, at, whose keyword is
// span, or at the end of its body, its closing brace, for a nil at, or a
// defer written before it writes a variable it names. Only one error is
// reported for each operand of the clause's &&. Code that never runs, after a
// statement that leaves its block or ends in exit, is not asked about.
func (c *Checker) proveContract(fn *ast.Func, span ast.Span, at *ast.Return) {
	k := c.contracts[fn]
	if k == nil || c.dead > 0 || c.deferred > 0 {
		return
	}
	defer c.release(c.mark())
	c.assumeReturnCall(at)
	var reported []ast.Expr
	for _, cf := range k.facts {
		if slices.Contains(reported, cf.leaf) {
			continue
		}
		notes, ok := c.contractHolds(fn, cf, span)
		if ok {
			continue
		}
		reported = append(reported, cf.leaf)
		notes = append(notes, note(cf.leaf.Spanned(), "ensured here"))
		where := "at this return"
		if at == nil {
			where = "at the end of its body"
		}
		c.errorNotes(span, notes, "%s ensures %s, which may not hold %s",
			c.displayName(fn), c.text(cf.leaf.Spanned()), where)
	}
}

// assumeReturnCall puts in force the facts at's one value gives, when at
// is a return of a call to a function with a contract, return fix(mut
// d): they hold once the call is done, and nothing runs between it and
// the return but the defers, whose writes proveContract judges.
func (c *Checker) assumeReturnCall(at *ast.Return) {
	if at == nil || len(at.Values) != 1 {
		return
	}
	if call, ok := at.Values[0].(*ast.Call); ok {
		if g, facts := c.callContractFacts(call, nil); g != nil {
			c.assume(g, facts, nil)
		}
	}
}

// contractHolds reports whether cf holds where fn returns at span, with
// a note on the deferred code that ends it when that is why it does not.
func (c *Checker) contractHolds(
	fn *ast.Func,
	cf clauseFact,
	span ast.Span,
) ([]diag.Diagnostic, bool) {
	if d := c.deferWrite(fn, cf.fact, span); d != nil {
		return []diag.Diagnostic{d.deferNote(c.displayName(fn))}, false
	}
	return nil, c.proves(cf)
}

// deferWrite returns the first write to a variable f names in deferred
// code of fn written before span, which runs after a return at span, or
// nil when there is none.
func (c *Checker) deferWrite(fn *ast.Func, f fact, span ast.Span) *drop {
	for _, d := range c.defersOf(fn) {
		if d.Span.End > span.Start {
			continue
		}
		idx := c.indexWrites(stmtNodes(d.Body))
		if why := idx.assigned(f); why != nil {
			return why
		}
	}
	return nil
}

// defersOf returns every defer in fn's body, at any depth, found once and
// kept.
func (c *Checker) defersOf(fn *ast.Func) []*ast.Defer {
	if ds, ok := c.defers[fn]; ok {
		return ds
	}
	var ds []*ast.Defer
	for _, stmt := range fn.Body {
		ast.Walk(stmt, func(n ast.Node) bool {
			if d, ok := n.(*ast.Defer); ok {
				ds = append(ds, d)
			}
			return true
		})
	}
	c.defers[fn] = ds
	return ds
}

// deferNote returns the note on d, a write in deferred code, that ends a
// fact fn ensures, since the code runs after fn returns.
func (d *drop) deferNote(fn string) diag.Diagnostic {
	if d.kind == dropMut {
		return note(d.at, "but "+d.name+" is passed as mut to "+d.text+
			" here, in deferred code, which runs after "+fn+" returns")
	}
	return note(d.at, "but "+d.name+" is assigned here, in deferred code, which runs after "+
		fn+" returns")
}

// proves reports whether the facts in force prove cf's fact: a key in a
// map by a fact that says so, a relation by the relations in force, or a
// bound by what is known of its subject, or else by the relations.
func (c *Checker) proves(cf clauseFact) bool {
	f := cf.fact
	about := linTerm{node: f.about}
	minusOne := constant.FromInt64(-1)
	switch {
	case !f.inMap.isZero():
		return c.provesMember(f)
	case !f.lenOf.isZero():
		return c.relates(about, linTerm{node: f.lenOf, isLen: true}, minusOne)
	case !f.atMostLen.isZero():
		return c.relates(about, linTerm{node: f.atMostLen, isLen: true}, constant.Int{})
	case !f.atMost.isZero():
		w := constant.Int{}
		if f.strict {
			w = minusOne
		}
		return c.relates(about, linTerm{node: f.atMost}, w)
	case f.diff != nil:
		return c.provesDiff(f.diff)
	}
	return c.provesBounds(cf)
}

// provesDiff reports whether the facts in force prove the difference
// bound d: the relations in force do, or a fact in force is the same
// bound, or a tighter one, holding under no more conditions, as the
// clause's own comparison tested in an early exit gives.
func (c *Checker) provesDiff(d *diffBound) bool {
	x := linTerm{node: d.x, isLen: d.xLen}
	y := linTerm{node: d.y, isLen: d.yLen}
	if c.relates(x, y, d.w) {
		return true
	}
	return slices.ContainsFunc(c.active, func(g fact) bool {
		o := g.diff
		return o != nil && o.x == d.x && o.y == d.y && o.xLen == d.xLen && o.yLen == d.yLen &&
			o.w.Cmp(d.w) <= 0 && everyCond(o.conds, d.conds) && c.holds(g)
	})
}

// everyCond reports whether each of conds is one of within.
func everyCond(conds, within []noWrap) bool {
	for _, cond := range conds {
		if !slices.ContainsFunc(within, func(o noWrap) bool { return sameCond(cond, o) }) {
			return false
		}
	}
	return true
}

// sameCond reports whether a and b are the same condition.
func sameCond(a, b noWrap) bool {
	return a.of == b.of && a.isLen == b.isLen && a.hasLo == b.hasLo && a.hasHi == b.hasHi &&
		(!a.hasLo || a.lo.Cmp(b.lo) == 0) && (!a.hasHi || a.hi.Cmp(b.hi) == 0)
}

// provesBounds reports whether the facts in force keep the subject of
// cf's fact, of which cf.base is known without any, within the bounds the
// fact puts on it, and away from zero when it does.
func (c *Checker) provesBounds(cf clauseFact) bool {
	f, known := cf.fact, c.knownOf(cf)
	about := linTerm{node: f.about, isLen: cf.isLen}
	return (!f.nonzero || known.excludesZero()) &&
		(!f.hasLo || c.atLeastBy(known, about, f.lo)) &&
		(!f.hasHi || c.atMostOf(known, about, f.hi))
}

// provesMember reports whether a fact in force says what f, a key's
// being in a map, does: the same key in the same map.
func (c *Checker) provesMember(f fact) bool {
	for g := range c.factsAbout(f.about) {
		if g.inMap == f.inMap && g.known == f.known && c.holds(g) {
			return true
		}
	}
	return false
}

// knownOf returns what the facts in force say of the subject of cf's
// fact, starting from what cf.base knows of it, and, for a length, what a
// successful append proves, with a bound at zero moved past it where zero
// is ruled out.
func (c *Checker) knownOf(cf clauseFact) interval {
	known := cf.base
	for g := range c.factsAbout(cf.fact.about) {
		if g.inMap.isZero() && c.holds(g) {
			known = known.narrow(g)
		}
	}
	if cf.isLen {
		known = c.appendedLen(cf.fact.about, known)
	}
	return known.tightened()
}

// atLeastBy reports whether a subject, the query's term about, of which
// known is known, is at least lo: known says so, or the relations in
// force do.
func (c *Checker) atLeastBy(known interval, about linTerm, lo constant.Int) bool {
	if known.hasLo && known.lo.Cmp(lo) >= 0 {
		return true
	}
	w, ok := lo.Neg()
	return ok && c.relates(zeroTerm(), about, w)
}

// atMostOf reports whether a subject, the query's term about, of which
// known is known, is at most hi: known says so, or the relations in force
// do.
func (c *Checker) atMostOf(known interval, about linTerm, hi constant.Int) bool {
	return known.hasHi && known.hi.Cmp(hi) <= 0 || c.relates(about, zeroTerm(), hi)
}

// contractFacts returns the facts the call stmt is, or is the whole value
// of, gives the statements after it in its block by its callee's
// contract, with the guard they come from, or nil when it gives none (see
// the top of this file).
func (c *Checker) contractFacts(stmt ast.Stmt) (*guard, []fact) {
	call, targets := contractCall(stmt)
	if call == nil {
		return nil, nil
	}
	return c.callContractFacts(call, targets)
}

// callContractFacts returns the facts call gives by its callee's
// contract once it is done, with the guard they come from, or nil when
// it gives none: targets are what the statement holding the call assigns
// after it, whose variables carry nothing.
func (c *Checker) callContractFacts(call *ast.Call, targets []ast.Expr) (*guard, []fact) {
	fn := ast.Callee(call)
	k := c.contracts[fn]
	ops := call.Operands()
	if k == nil || len(ops) != len(fn.Params) {
		return nil, nil
	}
	args := c.contractArgs(fn, call, targets)
	var facts []fact
	for _, cf := range k.facts {
		if f, ok := c.carryFact(cf.fact, args); ok {
			facts = append(facts, f)
		}
	}
	if len(facts) == 0 {
		return nil, nil
	}
	return &guard{span: call.Span, kind: guardEnsures}, facts
}

// contractCall returns the call stmt is, or is the whole value of, a
// declaration or an assignment, of one value or several, with every
// target the statement assigns once the call is done, or nil.
func contractCall(stmt ast.Stmt) (*ast.Call, []ast.Expr) {
	var value ast.Expr
	var targets []ast.Expr
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		value = s.X
	case *ast.Var:
		value = s.Value
	case *ast.MultiVar:
		value = s.Value
	case *ast.Assign:
		value, targets = s.Value, []ast.Expr{s.Target}
	case *ast.MultiAssign:
		value, targets = s.Value, s.Targets
	}
	call, _ := value.(*ast.Call)
	return call, targets
}

// contractArg is what a call passes for one parameter, as a contract's
// facts are carried over: the caller's variable, and the argument naming
// it, or a nil decl when no fact about the parameter can be.
type contractArg struct {
	decl ast.Decl
	arg  *ast.Ident
}

// contractArgs returns, for each of fn's parameters, what call passes for
// it, as contractArg says: a local variable or a parameter of the caller
// by name, passed as mut, or as a plain argument nothing else in the call
// writes, that the statement holding the call does not assign once it is
// done, in targets. A view parameter passed an array, whose length is its
// type's, carries nothing.
func (c *Checker) contractArgs(
	fn *ast.Func,
	call *ast.Call,
	targets []ast.Expr,
) map[ast.Decl]contractArg {
	ops := call.Operands()
	assigned := make(map[ast.Decl]bool)
	nodes := make([]ast.Node, len(targets))
	for i, t := range targets {
		nodes[i] = t
		if decl := c.writtenDecl(t); decl != nil {
			assigned[decl] = true
		}
	}
	args := make(map[ast.Decl]contractArg, len(fn.Params))
	for i, p := range fn.Params {
		id, ok := c.carrier(p, ops[i], call)
		// A write anywhere in the targets, a mut argument in an index,
		// arr[zero(mut d)] = fix(mut d), runs in the same statement, so it
		// carries nothing either.
		if ok && !assigned[id.Def] && !c.writtenIn(nodes, id.Def) {
			args[p] = contractArg{decl: id.Def, arg: id}
		}
	}
	return args
}

// carrier returns the variable arg, an argument of call passed for p, is
// when facts about p can be about it: a local variable or a parameter of
// the caller by name, passed as mut, or as a plain argument nothing else
// in the call writes; a view parameter passed an array carries nothing.
func (c *Checker) carrier(p *ast.Param, arg ast.Expr, call *ast.Call) (*ast.Ident, bool) {
	m, mut := arg.(*ast.MutArg)
	if mut {
		arg = m.X
	}
	id, ok := arg.(*ast.Ident)
	if !ok || p.Variadic || !c.callerVar(id.Def) ||
		c.table.IsView(p.Type) && c.table.IsArray(ast.TypeOf(id)) {
		return nil, false
	}
	// The call itself is walked, so that its own mut arguments count:
	// f(mut i, i) changes i after the callee was given its value.
	return id, mut || !c.writtenIn([]ast.Node{call}, id.Def)
}

// callerVar reports whether decl is a local variable or a parameter, of
// the function being checked.
func (c *Checker) callerVar(decl ast.Decl) bool {
	switch d := decl.(type) {
	case *ast.Param:
		return true
	case *ast.Var:
		return !c.globals[d]
	default:
		return false
	}
}

// writtenIn reports whether anything in nodes writes decl, as eachWrite
// finds writes, a call passing it as mut included.
func (c *Checker) writtenIn(nodes []ast.Node, decl ast.Decl) bool {
	found := false
	c.eachWrite(nodes, decl, func(write) { found = true })
	return found
}

// carryFact returns f, a fact about a callee's parameters, with each
// replaced by the caller's variable args holds for it, and whether every
// one it names has one that can carry it: a path below a variable needs
// an immutable one, or an error's code; a path whose index is a name
// cannot be carried, since the name is the callee's; and a key in a map
// is known as memberOf knows the argument.
func (c *Checker) carryFact(f fact, args map[ast.Decl]contractArg) (fact, bool) {
	ok := true
	sub := func(k factKey) factKey {
		if k.isZero() || !ok {
			return k
		}
		a, found := args[k.decl]
		if !found || !carriable(k, a.decl) {
			ok = false
			return k
		}
		return factKey{decl: a.decl, path: k.path}
	}
	out := f
	if f.inMap.isZero() {
		out.about = sub(f.about)
	} else if !c.carryKey(&out, args, sub) {
		return f, false
	}
	out.lenOf, out.atMostLen, out.atMost = sub(f.lenOf), sub(f.atMostLen), sub(f.atMost)
	out.inMap = sub(f.inMap)
	if f.diff != nil {
		out.diff = carryDiff(f.diff, sub)
	}
	out.ends, out.drop, out.guard, out.src = nil, nil, nil, nil
	return out, ok
}

// carryKey carries the key of out, a fact that a key is in a map, to the
// caller, through sub for a path below a parameter, which must be below
// an immutable variable, or as memberOf knows the argument passed for the
// parameter itself, and reports whether it can.
func (c *Checker) carryKey(
	out *fact,
	args map[ast.Decl]contractArg,
	sub func(factKey) factKey,
) bool {
	k := out.about
	if k.isZero() {
		return true
	}
	if k.path != "" {
		out.about = sub(k)
		return immutableDecl(out.about.decl)
	}
	a, found := args[k.decl]
	if !found {
		return false
	}
	var ok bool
	out.about, out.known, ok = c.memberOf(a.arg)
	return ok
}

// carryDiff returns d with its nodes, and those of its conditions, passed
// through sub.
func carryDiff(d *diffBound, sub func(factKey) factKey) *diffBound {
	out := *d
	out.x, out.y = sub(d.x), sub(d.y)
	out.conds = make([]noWrap, len(d.conds))
	for i, cond := range d.conds {
		cond.of = sub(cond.of)
		out.conds[i] = cond
	}
	return &out
}

// carriable reports whether a fact about k, a subject below a callee's
// parameter, can be about the same path below decl, the caller's
// variable passed for it: the parameter itself always can; a path only
// below an immutable variable or parameter, as subject.go has it, or as
// an error's code, which errorCodeKey allows of any, and only when every
// index in it is a known value.
func carriable(k factKey, decl ast.Decl) bool {
	if k.path == "" {
		return true
	}
	if namedIndex(k.path) {
		return false
	}
	return immutableDecl(decl) || k.path == ".code" && declType(decl) == types.Error
}

// namedIndex reports whether path, a subject's path below its variable,
// has an element whose index is written as a name rather than a value.
func namedIndex(path string) bool {
	for {
		i := strings.IndexByte(path, '[')
		if i < 0 {
			return false
		}
		path = path[i+1:]
		if path == "" || path[0] != '-' && (path[0] < '0' || path[0] > '9') {
			return true
		}
	}
}

// immutableDecl reports whether decl is a variable, or a parameter, that
// nothing can assign.
func immutableDecl(decl ast.Decl) bool {
	switch d := decl.(type) {
	case *ast.Var:
		return !d.Mutable
	case *ast.Param:
		return !d.Mut
	default:
		return false
	}
}

// tightened returns known with a bound at zero moved past it when known
// rules zero out, so a length not zero is at least 1, as minLen has it.
func (known interval) tightened() interval {
	if !known.nonzero {
		return known
	}
	one := constant.FromInt64(1)
	if known.hasLo && known.lo.IsZero() {
		known.lo = one
	}
	if known.hasHi && known.hi.IsZero() {
		known.hi = constant.FromInt64(-1)
	}
	return known
}
