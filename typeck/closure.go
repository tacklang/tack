package typeck

import (
	"fmt"
	"iter"
	"slices"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// Closures. A function literal, func(a T) R { ... }, is a closure: its
// body reads and writes the variables and parameters of the functions and
// closures it is written in, its captures, by reference, never as a copy.
// It is second class: it never outlives the call it is passed to, so its
// captures, which live in its enclosing function's frame, outlive it.
//
// A parameter written scope f T, of a function type T, is the only place a
// closure can go: a closure is allowed only as an argument to one, and
// anywhere else, assigned, returned, stored in a field, an element or a
// global, passed to any other parameter, a generic one or an any
// included, or called where it is written, it is reported (see
// funcLitElsewhere). A scope parameter is used only as a call's callee or
// as an argument to another scope parameter, and anywhere else it is
// reported too (see scopeElsewhere), so no value a closure makes can be
// stored, returned or passed anywhere it could outlive the call: nothing
// holding a function value can hold a scope one, since no expression
// gives one but the closure and the scope parameter themselves. A plain
// function value may be passed to a scope parameter. scope is part of a
// function type, func(scope func()), and two types are one only with the
// same scope marks; a parameter whose type is neither a function type nor
// Allocator, a mut or an own one, a variadic one, a result and an extern
// func's parameter cannot be scope (see scopeParamType). A scope
// Allocator follows rules of its own (see scopealloc.go).
//
// A closure's body is checked where it is written, as a block of the
// function holding it, with these differences (see checkFuncLit):
//   - a return in it returns from the closure, with its own results, and
//     a ? in it passes an error on as the closure's, so its results are
//     checked as a function's are, and one with results must end in a
//     return;
//   - break and continue cannot leave it, nor a fallthrough, and a defer
//     in it runs when the closure returns, so the loops, defers and switch
//     cases around it are not around its statements;
//   - it runs later than it is written, during the call it is passed to,
//     zero times, once or many, and the callee and the closure itself may
//     have changed any variable the closure writes since the last run. So
//     no fact in force where it is written about a mutable variable holds
//     in it: a fact is put in force there only when every subject it
//     names is an immutable variable, an immutable global or a parameter
//     that is not mut, none of which anything can write (see keepsInClosure),
//     and no known value a mutable variable was last given carries in
//     either. Facts its own conditions give hold in it as they would in a
//     function, up to any write in it that can reach the use: a closure
//     counts as a loop around its body for whether a write reaches a use
//     (see isLoop), since a later run of it follows an earlier one, and a
//     write in a nested closure is a write where that closure is passed.
//     A call it makes to a scope parameter of the function it is written
//     in cannot write any of its captures, which that function's caller
//     cannot reach, nor can a call to any other function, which reaches
//     a variable only through a mut argument the call shows.
//
// Each closure records its captures (see ast.FuncLit.Captures), found in
// its body and the bodies of the closures nested in it: every name of a
// variable or a parameter declared outside it, and whether it may write
// one (see captures). For a call's rules a capture is an argument of the
// call the closure is passed to, and a borrow, read in place however
// small its type (see checkCaptures): a capture cannot be passed as mut,
// or written by another closure, in the same call, whether the closure
// only reads it or writes it, unlike a plain argument, which is a copy;
// and a capture the closure may write counts as a mut argument of the call
// for every rule a mut argument is under: nothing else in the call may
// read it in place, a list passed plainly or viewed included (see
// writersIn), nor may two of its closures write one variable. A capture
// of a field or an element counts as one of the whole variable. Writing a
// capture needs it to be a place that may be written, as anywhere: a
// mutable variable or a mut parameter. A capture moved before the call,
// or in it, is a use after a move, or a use alongside an own argument, as
// any other name is, and a closure cannot move a capture, since it may
// run again (see move.go). A call that passes a closure writing x ends
// the facts about x, as a call passing x as mut does, since a write in a
// closure is in the call that receives it.
//
// Recursion through scope parameters is allowed: a function may pass its
// scope parameter, or a closure calling it, to itself. Like any
// recursion, it is the parked stack-depth violation docs/guarantees.md
// lists.

// scopeParamType returns typ, the type param, a parameter marked scope,
// is declared with, once it has checked that scope fits it: its type is
// a function type or Allocator, and it is not variadic. A misfit is
// reported, and param is then taken as a plain parameter (see
// scopeFits), so its uses are not reported again.
func (c *Checker) scopeParamType(param *ast.Param, typ types.Type) types.Type {
	switch {
	case typ == types.Invalid:
	case param.Variadic:
		c.errorf(
			param.ScopeSpan,
			"parameter %s cannot be both variadic and scope: a scope parameter takes one function value or Allocator",
			param.Name,
		)
	case !c.table.IsFunc(typ) && typ != types.Allocator:
		c.errorf(param.ScopeSpan,
			"parameter %s cannot be scope: %s is not a function type or Allocator, "+
				"and only a function value or an Allocator can be scope; remove scope",
			param.Name, c.typeName(typ))
	}
	return typ
}

// scopeFits reports whether p is a scope parameter scope fits, as
// scopeParamType checks: one marked scope that is not variadic, whose
// type is a function type or Allocator. A parameter it does not fit was
// reported, and is taken as a plain one.
func (c *Checker) scopeFits(p *ast.Param) bool {
	return p.Scope && !p.Variadic && (c.table.IsFunc(p.Type) || p.Type == types.Allocator)
}

// funcTypeScope reports whether p, a parameter of a function type marked
// scope, whose type is typ, may be: typ is a function type or Allocator.
// A parameter that may not is reported.
func (c *Checker) funcTypeScope(p *ast.Param, typ types.Type) bool {
	if typ == types.Invalid || c.table.IsFunc(typ) || typ == types.Allocator {
		return true
	}
	c.errorf(p.ScopeSpan,
		"a function type's parameter cannot be scope unless it is a function type or Allocator, "+
			"and %s is not; remove scope",
		c.typeName(typ))
	return false
}

// checkExternScope reports each parameter of fn, an extern func, that is
// marked scope, and returns whether there was none: a function value does
// not cross to C, a closure least of all, and a scope Allocator would be
// how a region's allocator crossed, which it never does.
func (c *Checker) checkExternScope(fn *ast.Func) bool {
	clean := true
	for _, p := range fn.Params {
		if !p.Scope {
			continue
		}
		clean = false
		if p.Type == types.Allocator {
			c.errorf(
				p.ScopeSpan,
				"parameter %s of extern func %s cannot be scope: a region's allocator never crosses to C, "+
					"and c_allocator crosses as a plain Allocator; remove scope",
				p.Name,
				fn.Name,
			)
			continue
		}
		c.errorf(p.ScopeSpan,
			"parameter %s of extern func %s cannot be scope: a function value does not cross to C",
			p.Name, fn.Name)
	}
	return clean
}

// scopeArg checks arg, passed to p, a scope parameter of callee: a
// closure, whose type is its signature's, a scope parameter passed on, or
// any other value of a function type. Its type must be p's, scope marks
// included, and when p's type mentions a type parameter of a generic
// callee whose call is being inferred, arg's type gives it its type
// arguments first, as any argument's does. A closure's body is checked
// once its type is known. A scope Allocator parameter takes any
// Allocator, a region's and a scope one among them (see scopeAllocArg).
// A parameter whose type was reported, or is no function type, which its
// scope mark was reported for, takes anything without a further report.
func (c *Checker) scopeArg(arg ast.Expr, p param, callee string) {
	if p.typ == types.Allocator {
		c.scopeAllocArg(arg, p, callee)
		return
	}
	if p.typ == types.Invalid || !c.table.IsFunc(p.typ) {
		// The parameter's scope or its type was reported where it is
		// declared, and nothing that follows from it is reported here: a
		// closure is not checked, and anything else only for itself.
		if _, isLit := arg.(*ast.FuncLit); !isLit {
			c.argExpr(arg)
		}
		return
	}
	if _, isMut := arg.(*ast.MutArg); isMut {
		c.checkArg(arg, param{name: p.name, typ: p.typ}, callee)
		return
	}
	if _, isOwn := arg.(*ast.OwnArg); isOwn {
		c.checkArg(arg, param{name: p.name, typ: p.typ}, callee)
		return
	}
	lit, isLit := arg.(*ast.FuncLit)
	var got types.Type
	switch {
	case isLit:
		got = c.funcLitType(lit)
	default:
		if id, ok := arg.(*ast.Ident); ok {
			c.scopeUse = id
		}
		got = c.expr(arg)
		c.scopeUse = nil
	}
	want := p.typ
	if got != types.Invalid && c.inference.mentionsAny(c, want) {
		if !c.unify(arg, want, got, callee) {
			got = types.Invalid
		} else {
			want = c.substitute(want, c.inference.args)
		}
	}
	if got != types.Invalid {
		c.use(arg, got, want, "in argument to "+callee)
	}
	if isLit && lit.Type != types.Invalid {
		c.checkFuncLit(lit)
	}
}

// funcLitType returns the function type of lit, a closure, which its
// signature gives, recording it on lit and on its Func as its Value: its
// parameters' types, each mut, own or scope where it is, and its results'.
// Its parameters follow a function's rules, but its signature must be one
// a function type can say, as a function used as a value must (see
// notValue): no variadic or any parameter, and no view result. A
// signature that cannot, or with a part that was reported, gives
// types.Invalid.
func (c *Checker) funcLitType(lit *ast.FuncLit) types.Type {
	fn := lit.Func
	lit.Type = types.Invalid
	fn.Value = types.Invalid
	ok := true
	params := make([]types.FuncParam, len(fn.Params))
	for i, p := range fn.Params {
		p.Type = c.paramType(p)
		ok = ok && p.Type != types.Invalid
		params[i] = types.FuncParam{Type: p.Type, Mut: p.Mut, Own: p.Own, Scope: c.scopeFits(p)}
	}
	fn.ResultTypes = make([]types.Type, len(fn.Results))
	for i, r := range fn.Results {
		fn.ResultTypes[i] = c.resultType(r, len(fn.Results), "")
		ok = ok && fn.ResultTypes[i] != types.Invalid
	}
	c.checkErrorLast(fn)
	if !ok {
		return types.Invalid
	}
	if why := c.notValue(fn); why != "" {
		c.errorf(fn.NameSpan, "this closure has no function type: %s", why)
		return types.Invalid
	}
	lit.Type = c.table.Func(params, fn.ResultTypes)
	fn.Value = lit.Type
	return lit.Type
}

// closureState is the part of the Checker that checking a closure's body
// sets apart from the function it is written in, and checkFuncLit puts
// back once the body is checked.
type closureState struct {
	fn                                    *ast.Func
	loops, deferred, deferLoops, dead, at int
	fieldLoop                             int
	inCoveredDefault                      int
	inRejectedDefer                       bool
	clauses                               []clauseAt
	active                                []fact
	guards                                []*guard
	outerFacts, outerGuards               int
	relIdx                                *relIndex
	proven                                []proofOnly
	starts                                []start
	invFrames                             []*invFrame
	appends                               []appendRecord
	chain                                 chainFacts
	inference                             *inference
	viewSlice, checkedSlice               *ast.Slice
	viewCall, droppedCall, checkedTake    *ast.Call
	checkedIndex, viewSpan                *ast.Index
	checkedDivision                       *ast.Binary
	checkedArith                          ast.Expr
	checkedAssert                         *ast.TypeAssert
	assigning                             *pendingAssign
	passedOn                              map[*ast.Ident]ast.Decl
	deferredOverflow                      *[]overflowCheck
	scopeUse                              *ast.Ident
}

// enterClosure sets the Checker up to check the body of lit, a closure,
// and returns what it replaced: lit's function is the one a return and a
// ? leave, no loop, defer or switch case is around its statements, no
// block around it has left, and only the facts keepsInClosure keeps are
// in force, none of them a known value a mutable variable was last given
// or a bound a loop around the closure keeps, with only the guards that
// gave them. What the call it is an argument of is checking for itself,
// an inference among it, is nothing the body's own expressions are part
// of.
//
// The facts and guards in force where the closure is written stay where
// they are, in active and guards, with the index of the facts by subject,
// and those the body may not read are passed over as they are read (see
// inForce), so entering a closure copies none of them: a block of n early
// exits, each followed by a closure, would otherwise copy the facts in
// force n times. The facts before outerFacts are the outer ones, and of
// those, only the ones keepsInClosure keeps are in force in the body,
// which kept lists as facts are put in force, and keptAt marks; the body
// puts its own facts in force after them, and releases them before it
// ends. So the facts in force in the body are those that were copied
// into it before, in the same order, and the index, which holds the
// outer facts the body may not read too, is kept in step with all of
// them, and so is as current after the body as before it. The index of
// the relations in force is set aside instead, and put back as the body
// is left: the body reads the one it keeps of the facts keepsInClosure
// keeps (see relindex.go).
func (c *Checker) enterClosure(lit *ast.FuncLit) closureState {
	saved := closureState{
		fn: c.fn, loops: c.loops, deferred: c.deferred, deferLoops: c.deferLoops,
		fieldLoop: c.fieldLoop, dead: c.dead, at: c.at, inCoveredDefault: c.inCoveredDefault,
		inRejectedDefer: c.inRejectedDefer, clauses: c.clauses,
		active: c.active, guards: c.guards, outerFacts: c.outerFacts, outerGuards: c.outerGuards,
		relIdx: c.relIdx, proven: c.proven, starts: c.starts,
		invFrames: c.invFrames, appends: c.appends, chain: c.chain, inference: c.inference,
		viewSlice: c.viewSlice, checkedSlice: c.checkedSlice, viewCall: c.viewCall,
		droppedCall: c.droppedCall, checkedTake: c.checkedTake, checkedIndex: c.checkedIndex,
		checkedDivision: c.checkedDivision, checkedArith: c.checkedArith,
		checkedAssert: c.checkedAssert, assigning: c.assigning, passedOn: c.passedOn,
		deferredOverflow: c.deferredOverflow, scopeUse: c.scopeUse, viewSpan: c.viewSpan,
	}
	c.fn = lit.Func
	c.loops, c.deferred, c.deferLoops, c.dead, c.inCoveredDefault = 0, 0, 0, 0, 0
	c.fieldLoop = 0
	c.inRejectedDefer = false
	c.clauses = nil
	c.outerFacts, c.outerGuards = len(c.active), len(c.guards)
	c.relIdx = c.bodyRelations(saved.relIdx)
	if closureOracle {
		c.checkClosureView()
	}
	c.proven, c.starts, c.invFrames, c.appends = nil, nil, nil, nil
	c.chain = chainFacts{}
	c.inference = nil
	c.viewSlice, c.checkedSlice, c.viewCall, c.droppedCall, c.checkedTake = nil, nil, nil, nil, nil
	c.checkedIndex, c.checkedDivision, c.checkedArith, c.checkedAssert = nil, nil, nil, nil
	c.assigning, c.passedOn, c.deferredOverflow, c.scopeUse = nil, nil, nil, nil
	c.viewSpan = nil
	c.factGen++
	return saved
}

// leaveClosure puts back what enterClosure replaced.
func (c *Checker) leaveClosure(s closureState) {
	c.fn = s.fn
	c.loops, c.deferred, c.deferLoops, c.dead, c.at = s.loops, s.deferred, s.deferLoops, s.dead, s.at
	c.fieldLoop = s.fieldLoop
	c.inCoveredDefault, c.inRejectedDefer, c.clauses = s.inCoveredDefault, s.inRejectedDefer, s.clauses
	c.unindexFacts(len(s.active))
	c.relIdx = s.relIdx
	c.active, c.guards, c.proven, c.starts = s.active, s.guards, s.proven, s.starts
	c.popKept(len(s.active), len(s.guards))
	c.outerFacts, c.outerGuards = s.outerFacts, s.outerGuards
	c.invFrames, c.appends, c.chain, c.inference = s.invFrames, s.appends, s.chain, s.inference
	c.viewSlice, c.checkedSlice, c.viewCall = s.viewSlice, s.checkedSlice, s.viewCall
	c.droppedCall, c.checkedTake, c.checkedIndex = s.droppedCall, s.checkedTake, s.checkedIndex
	c.checkedDivision, c.checkedArith, c.checkedAssert = s.checkedDivision, s.checkedArith, s.checkedAssert
	c.assigning, c.passedOn, c.deferredOverflow, c.scopeUse = s.assigning, s.passedOn, s.deferredOverflow, s.scopeUse
	c.viewSpan = s.viewSpan
	c.factGen++
}

// pushKept records whether keepsInClosure keeps f, the fact just put in
// force, last in active: in keptAt, in kept when it does, and for its
// guard, the last, in guardKept.
func (c *Checker) pushKept(f fact) {
	keep := c.keepsInClosure(f)
	c.keptAt = append(c.keptAt, keep)
	if keep {
		c.kept = append(c.kept, len(c.active)-1)
		c.guardKept[len(c.guardKept)-1] = true
	}
}

// popKept forgets what pushKept recorded for the facts from position
// facts of active on, and for the guards from position guards on, as
// release takes them out of force.
func (c *Checker) popKept(facts, guards int) {
	c.keptAt = c.keptAt[:facts]
	for len(c.kept) > 0 && c.kept[len(c.kept)-1] >= facts {
		c.kept = c.kept[:len(c.kept)-1]
	}
	c.guardKept = c.guardKept[:guards]
}

// inForce returns the facts in force, in order, with their positions in
// active: every one, outside a closure's body, and in one, the outer
// facts keepsInClosure keeps, found through kept, then the body's own
// (see enterClosure). So reading the facts in force in a body reads no
// more of them than a copy of them would have held. When the tests set
// closureOracle, what a body reads is checked first (see
// checkClosureView).
func (c *Checker) inForce() iter.Seq2[int, fact] {
	if closureOracle {
		c.checkClosureView()
	}
	return c.viewFacts()
}

// viewFacts is inForce without the check.
func (c *Checker) viewFacts() iter.Seq2[int, fact] {
	return func(yield func(int, fact) bool) {
		from := 0
		if c.outerFacts > 0 {
			for _, i := range c.kept {
				if i >= c.outerFacts {
					break
				}
				if !yield(i, c.active[i]) {
					return
				}
			}
			from = c.outerFacts
		}
		for i := from; i < len(c.active); i++ {
			if !yield(i, c.active[i]) {
				return
			}
		}
	}
}

// factsInForce returns the facts in force, as inForce gives them, in a
// slice: active itself outside a closure's body, and a copy in one.
func (c *Checker) factsInForce() []fact {
	if c.outerFacts == 0 {
		return c.active
	}
	var out []fact
	for _, f := range c.inForce() {
		out = append(out, f)
	}
	return out
}

// factInForce reports whether the fact at position i of active is in
// force: a fact of the closure's body being checked, or of none, or an
// outer one keepsInClosure keeps (see enterClosure).
func (c *Checker) factInForce(i int) bool {
	return i >= c.outerFacts || c.keptAt[i]
}

// guardInForce reports whether the guard at position i of guards is in
// force: one of the closure's body being checked, or of none, or an
// outer one that gave a fact keepsInClosure keeps (see enterClosure).
// When the tests set closureOracle, what a body reads is checked first
// (see checkClosureView), once for each guard read.
func (c *Checker) guardInForce(i int) bool {
	if closureOracle {
		c.checkClosureView()
	}
	return c.guardInView(i)
}

// guardInView is guardInForce without the check.
func (c *Checker) guardInView(i int) bool {
	return i >= c.outerGuards || c.guardKept[i]
}

// closureOracle says to check, whenever a closure's body reads the facts
// or guards in force, that they are those a copy of them made as the
// body is entered would hold, panicking on a difference (see
// checkClosureView). It is set in a build with the tag tackfactoracle,
// and by the tests that compare the two, which set it only while no
// other test runs.
var closureOracle = factOracle

// checkClosureView panics when the facts or guards in force, as inForce,
// factInForce and guardInForce read them, are not those entering the
// closure being checked copied into its body before they were left in
// place (see enterClosure): of the facts before outerFacts, those
// keepsInClosure keeps, judged afresh, in order, then the body's own;
// and of the guards before outerGuards, the guards of those facts, each
// once, in the order the facts give them, then the body's own. The
// facts of a closure nested in another are those keepsInClosure keeps
// of the facts in force in the other, which are those it keeps of the
// facts before the other's body and the other's own, so the same rule
// holds for it with its own outerFacts and outerGuards. Outside a
// closure's body every fact and guard is in force, and there is nothing
// to check.
func (c *Checker) checkClosureView() {
	if c.outerFacts == 0 && c.outerGuards == 0 {
		return
	}
	var want []int
	var wantGuards []*guard
	for i, f := range c.active {
		if i >= c.outerFacts || c.keepsInClosure(f) {
			want = append(want, i)
		}
		if i < c.outerFacts && c.keepsInClosure(f) && !slices.Contains(wantGuards, f.guard) {
			wantGuards = append(wantGuards, f.guard)
		}
	}
	var got []int
	for i := range c.viewFacts() {
		got = append(got, i)
	}
	if !slices.Equal(got, want) {
		panic(fmt.Sprintf("typeck: a closure's body reads the facts at %v, a copy holds those at %v", got, want))
	}
	for i := range c.active {
		if in := c.factInForce(i); in != slices.Contains(want, i) {
			panic(fmt.Sprintf("typeck: a closure's body takes the fact at %d as in force: %v, a copy: %v", i, in, !in))
		}
	}
	var gotGuards []*guard
	for i, g := range c.guards[:c.outerGuards] {
		if c.guardInView(i) {
			gotGuards = append(gotGuards, g)
		}
	}
	if !slices.Equal(gotGuards, wantGuards) {
		panic(fmt.Sprintf("typeck: a closure's body reads %d outer guards, a copy holds %d", len(gotGuards), len(wantGuards)))
	}
}

// keepsInClosure reports whether f, a fact in force where a closure is
// written, stays in force in its body: every subject it names, and every
// subject of each side it carries, is a variable or a parameter nothing
// can write, an immutable local or global or a parameter that is not mut
// (see immutableDecl). A fact about anything else may not hold when the
// closure runs.
func (c *Checker) keepsInClosure(f fact) bool {
	var buf [7]factKey
	for _, k := range f.allKeys(&buf) {
		if !k.isZero() && !immutableDecl(k.decl) {
			return false
		}
	}
	if f.either != nil {
		for _, side := range f.either.sides {
			for _, g := range side {
				if !c.keepsInClosure(g) {
					return false
				}
			}
		}
	}
	return true
}

// checkFuncLit checks the body of lit, a closure whose type is known,
// where it is written (see enterClosure), that one with results ends in
// a return, and then records its captures (see captures). Its
// parameters are recorded in closureParams first.
func (c *Checker) checkFuncLit(lit *ast.FuncLit) {
	fn := lit.Func
	for _, p := range fn.Params {
		c.closureParams[p] = true
	}
	saved := c.enterClosure(lit)
	c.checkBlock(fn.Body, fn)
	if fn.Results != nil && !blockTerminates(fn.Body) && len(ast.BadStmts(fn.Body)) == 0 {
		c.errorf(fn.NameSpan, "%s must end in a return", c.displayName(fn))
	}
	c.leaveClosure(saved)
	lit.Captures = c.captures(lit)
}

// captures returns the captures of lit, a closure: each variable or
// parameter its body names, at any depth, that is not declared inside
// lit, its own parameters and those of the closures nested in it
// included, a package-level variable aside, in the order of their first
// names, with whether the body, or a closure nested in it, may write it
// (see ast.Capture).
func (c *Checker) captures(lit *ast.FuncLit) []*ast.Capture {
	inside := make(map[ast.Decl]bool)
	for _, p := range lit.Func.Params {
		inside[p] = true
	}
	for _, stmt := range lit.Func.Body {
		ast.Walk(stmt, func(n ast.Node) bool {
			switch d := n.(type) {
			case *ast.Var:
				inside[d] = true
			case *ast.Param:
				inside[d] = true
			}
			return true
		})
	}
	var caps []*ast.Capture
	captured := func(id *ast.Ident) *ast.Capture {
		if id == nil || inside[id.Def] {
			return nil
		}
		switch d := id.Def.(type) {
		case *ast.Param:
		case *ast.Var:
			// A fields loop's variable is no variable the closure can
			// point to: the backend lowers each use of it in place.
			if c.globals[d] || c.fieldLoops[d] != nil {
				return nil
			}
		default:
			return nil
		}
		for _, cp := range caps {
			if cp.Decl == id.Def {
				return cp
			}
		}
		cp := &ast.Capture{Decl: id.Def, At: id.Span}
		caps = append(caps, cp)
		return cp
	}
	write := func(target ast.Expr) {
		if cp := captured(ast.Root(target)); cp != nil && !cp.Write {
			cp.Write, cp.WriteAt = true, target.Spanned()
		}
	}
	for _, stmt := range lit.Func.Body {
		ast.Walk(stmt, func(n ast.Node) bool {
			for _, target := range assignedTargets(n) {
				write(target)
			}
			switch x := n.(type) {
			case *ast.MutArg:
				write(x.X)
			case *ast.Ident:
				captured(x)
			}
			return true
		})
	}
	return caps
}

// funcLitElsewhere reports lit, a closure written anywhere but as an
// argument to a scope parameter, the one place a closure can go. Its body
// is not checked.
func (c *Checker) funcLitElsewhere(lit *ast.FuncLit) types.Type {
	c.errorf(lit.Func.NameSpan, "a closure can only be passed to a scope parameter")
	return types.Invalid
}

// funcLitToPlain reports lit, a closure passed to p, a parameter of
// callee that is not scope.
func (c *Checker) funcLitToPlain(lit *ast.FuncLit, p param, callee string) {
	c.errorf(lit.Func.NameSpan,
		"%s does not take %s as scope, and a closure can only be passed to a scope parameter",
		callee, p.name)
}

// isScopeParam reports whether id names a scope parameter of a function
// type that scope fits (see scopeFits); a scope Allocator parameter is
// a scope allocator instead (see isScopeAlloc).
func (c *Checker) isScopeParam(id *ast.Ident) bool {
	p, ok := id.Def.(*ast.Param)
	return ok && c.scopeFits(p) && c.table.IsFunc(p.Type)
}

// scopeElsewhere reports id, a name of a scope parameter, used anywhere
// but as a call's callee or an argument to another scope parameter, where
// its value could outlive the call.
func (c *Checker) scopeElsewhere(id *ast.Ident) {
	c.errorf(id.Span,
		"%s is a scope parameter, which can only be called or passed to another scope parameter",
		id.Name)
}

// writer is a place an operand of a call may change while the call runs
// or its operands are evaluated: the place of a mut argument, mut, in it
// at any depth outside a closure, or a variable a closure in it writes,
// at any depth, named by place, with what wrote it, the closure lit and
// the capture.
type writer struct {
	place ast.Expr
	mut   *ast.MutArg
	lit   *ast.FuncLit
	cap   *ast.Capture
}

// writersIn returns the writers in operands, at any depth, in order (see
// writer). An operand may be nil.
func writersIn(operands []ast.Expr) []writer {
	var ws []writer
	for _, e := range operands {
		if e == nil {
			continue
		}
		ast.Walk(e, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.MutArg:
				ws = append(ws, writer{place: x.X, mut: x})
			case *ast.FuncLit:
				for _, cp := range x.Captures {
					if cp.Write {
						id := &ast.Ident{Name: declName(cp.Decl), Def: cp.Decl, Span: cp.WriteAt}
						ws = append(ws, writer{place: id, lit: x, cap: cp})
					}
				}
				return false
			}
			return true
		})
	}
	return ws
}

// writersOutside returns the writers in every operand but the one at
// skip, at any depth, in order.
func writersOutside(operands []ast.Expr, skip int) []writer {
	var ws []writer
	for j, arg := range operands {
		if j != skip {
			ws = append(ws, writersIn([]ast.Expr{arg})...)
		}
	}
	return ws
}

// what names w in a message: mut and its place's text, or a closure that
// writes and the variable's name.
func (w writer) what(c *Checker) string {
	if w.mut != nil {
		return "mut " + c.text(w.place.Spanned())
	}
	return "a closure that writes " + declName(w.cap.Decl)
}

// name returns the text of what w changes, its place's or its variable's
// name.
func (w writer) name(c *Checker) string {
	if w.mut != nil {
		return c.text(w.place.Spanned())
	}
	return declName(w.cap.Decl)
}

// only says, in a message, that w must be the only way to reach what it
// changes.
func (w writer) only(c *Checker) string {
	if w.mut != nil {
		return "a mut argument must be the only way to reach " + w.name(c)
	}
	return "a closure that writes " + w.name(c) + " must be the only way to reach " + w.name(c)
}

// note returns the note pointing at w: its mut argument, or the closure's
// first write.
func (w writer) note() diag.Diagnostic {
	if w.mut != nil {
		return note(w.mut.Span, "passed as mut here")
	}
	return note(w.cap.WriteAt, fmt.Sprintf("%s is written here, in a closure", declName(w.cap.Decl)))
}

// checkCaptures reports each capture of each closure among call's
// operands, a method call's receiver among them, that a writer in another
// operand may change (see writersOutside): a mut argument of the same
// variable, or another closure that writes it, at any depth, with a note
// at that writer. A closure reaches its captures in place, however small
// their type, while the call runs, so a capture the call passes as mut or
// another closure in it writes would be reachable two ways at once, as a
// place passed as mut twice would. A capture of a field or an element of
// a variable is one of the whole variable. Each variable is reported once
// for the call.
func (c *Checker) checkCaptures(call *ast.Call) {
	operands := call.Operands()
	var reported []ast.Decl
	for i, arg := range operands {
		lit, ok := arg.(*ast.FuncLit)
		if !ok || len(lit.Captures) == 0 {
			continue
		}
		ws := writersOutside(operands, i)
		for _, cp := range lit.Captures {
			if slices.Contains(reported, cp.Decl) {
				continue
			}
			for _, w := range ws {
				if root := ast.Root(w.place); root == nil || root.Def != cp.Decl {
					continue
				}
				reported = append(reported, cp.Decl)
				name := declName(cp.Decl)
				c.errorNotes(cp.At, []diag.Diagnostic{w.note()},
					"cannot capture %s in a closure alongside %s in this call: a closure reaches its captures in place, "+
						"and %s during the call",
					name, w.what(c), w.only(c))
				break
			}
		}
	}
}
