package typeck

import (
	"slices"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// Moves. A value of an owned type, a list or a struct or array holding
// one (types.Table.Owned), has one owner, the variable holding it, which
// frees it when its block ends. So it is never copied: assigning it,
// declaring a variable with it, returning it or writing it as a struct
// literal's field, or passing it to an own parameter, moves it, and the
// variable it came from cannot be used again until it is assigned a new
// value. An own parameter owns its value as a local variable does, and is
// followed as one, though it is never assigned. A plain parameter of an
// owned type borrows its argument, which its caller keeps, and a mut
// parameter refers to its caller's variable, so neither can be moved, and
// nor can a field, alone, out of the struct holding it.
//
// checkMoves follows each function body in the order it runs, keeping for
// each local variable and own parameter of an owned type whether it has
// been moved, on every
// path that reaches the point being checked or on some of them, and where.
// Control flow is structured: an if and a switch join what their branches
// leave, a branch that returns, breaks, continues or calls exit, which
// never returns, leaving nothing to join; a loop's body is followed twice, the second time from what the
// first pass left joined with what it started from, so a move late in the
// body is seen by a use early in it, and only the second pass reports.
// Using a variable that may have been moved is an error: reading it,
// passing it as mut, moving it, or assigning one of its fields or
// elements. Assigning it whole makes it owned again; its old value, moved
// or not, is freed first (see codegen/c), which a moved value makes a no-op.
//
// A value of an owned type that no variable holds, a call's result, a
// make or a struct literal, must be given an owner: it is only allowed as
// the value of a declaration, an assignment, a return, a binding of
// several results or a struct literal's field. Anywhere else, read in
// place or dropped, nothing would free it, and it is reported.
//
// A defer runs when its block ends, so it reads every variable as it is
// then. One that reads a variable moved later in its block, at any depth,
// would read a moved value, and is reported; and deferred code cannot move
// a variable declared outside it, since the statements after the defer
// run first, and could assign it again. A range loop over a list walks
// the list itself, not a copy, so its body cannot assign the list, or
// anything holding it, pass either as mut, or move it. A range loop over
// a view a call returns walks what the view points into, which may be
// any place the call borrows (viewBorrows), so its body cannot assign,
// pass as mut or move anything that may overlap one of them, inside it or
// holding it. Likewise a clause
// of a switch on a union whose binding refers to the payload of the
// switched place in place cannot assign that place, or anything holding
// it, pass either as mut, or move it, and the binding itself is never
// moved: a payload cannot be moved out of a union.
//
// Each function is checked only if checking its body reported nothing, so
// every expression in it has its type.

// moveFact is what a flow knows about a moved variable: whether it was
// moved on only some of the paths that reach the point (maybe), and the
// places it was moved, in the order they were first met.
type moveFact struct {
	at    []ast.Span
	maybe bool
}

// flow is what is known at one point of a function body about its owned
// variables: those moved on some path that reaches it, and how. A dead
// flow is a point no path reaches, after a return, a break or a continue.
type flow struct {
	moved map[ast.Decl]moveFact
	dead  bool
}

// live returns a flow at a reachable point where nothing has been moved.
func live() flow {
	return flow{moved: make(map[ast.Decl]moveFact)}
}

// dead returns a flow at a point no path reaches.
func dead() flow {
	return flow{dead: true, moved: make(map[ast.Decl]moveFact)}
}

// clone returns a copy of f that can be changed without changing f.
func (f flow) clone() flow {
	moved := make(map[ast.Decl]moveFact, len(f.moved))
	for v, mf := range f.moved {
		moved[v] = moveFact{maybe: mf.maybe, at: slices.Clone(mf.at)}
	}
	return flow{dead: f.dead, moved: moved}
}

// join returns what is known where the paths reaching a and b meet: a
// variable moved on both is moved, maybe moved if either says so, and one
// moved on only one of them is maybe moved. A dead flow adds nothing.
func join(a, b flow) flow {
	switch {
	case a.dead:
		return b.clone()
	case b.dead:
		return a.clone()
	}
	out := live()
	for v, fa := range a.moved {
		fb, both := b.moved[v]
		out.moved[v] = moveFact{maybe: fa.maybe || !both || fb.maybe, at: unionSpans(fa.at, fb.at)}
	}
	for v, fb := range b.moved {
		if _, both := a.moved[v]; !both {
			out.moved[v] = moveFact{maybe: true, at: slices.Clone(fb.at)}
		}
	}
	return out
}

// unionSpans returns a followed by every span of b not in a.
func unionSpans(a, b []ast.Span) []ast.Span {
	out := slices.Clone(a)
	for _, s := range b {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// loopFlow collects what the breaks and continues of one loop leave.
type loopFlow struct {
	breaks, continues flow
}

// deferRead is a read, at, of an owned variable or own parameter v
// declared outside the defer that reads it.
type deferRead struct {
	v  ast.Decl
	at ast.Span
}

// blockFlow is what the mover knows about a block whose statements it is
// following: the reads its defers make, so far, of variables a later move
// would take from under them.
type blockFlow struct {
	reads []deferRead
}

// mover follows one function body for checkMoves.
type mover struct {
	c  *Checker
	fn *ast.Func // the function whose body it follows

	// deferLocals holds the variables the deferred code being followed
	// declares, which it may move.
	deferLocals map[ast.Decl]bool

	// reported holds the defer reads already reported, so a read of a
	// variable moved twice later is reported once.
	reported map[ast.Span]bool

	loops   []*loopFlow  // the loops enclosing the point, innermost last
	blocks  []*blockFlow // the blocks enclosing it, innermost last
	ranging []ranged     // the lists being ranged over around it, innermost last

	// matching holds the places switched on around the point whose
	// clause binds a payload of them in place, innermost last.
	matching []matched

	// report says whether errors are reported: not while following a
	// loop's body for the first time, which the second pass repeats.
	report bool

	// deferring says whether deferred code is being followed.
	deferring bool
}

// ranged is a list a range loop around the point walks, place, and the
// loop's element variable when it refers to the element in place, an
// element of an owned type, or nil. A range over a view a call returns
// gives one for each place the view may point into (viewBorrows), with
// view the call: nothing that may overlap the place can change while the
// loop runs.
type ranged struct {
	place ast.Expr
	bind  *ast.Var
	view  *ast.Call
}

// matched is a place a switch on a union matches, whose clause being
// followed binds bind to the payload it holds, in place.
type matched struct {
	place ast.Expr
	bind  *ast.Var
}

// expression contexts: how the value of an expression is used.
type useCtx int

const (
	// readCtx reads the value where it is, or copies a copyable one.
	readCtx useCtx = iota
	// moveCtx takes the value: the value of a declaration, an
	// assignment, a return or a struct literal's field.
	moveCtx
	// viewCtx reads the value through a view parameter; a temporary list
	// there has been reported by viewArg.
	viewCtx
)

// checkMoves checks the moves of fn's body, as the comment above
// describes.
func (c *Checker) checkMoves(fn *ast.Func) {
	m := &mover{c: c, fn: fn, report: true, reported: make(map[ast.Span]bool)}
	m.block(fn.Body, live())
}

// ownedLocal returns the local variable or the own parameter id names
// when it holds an owned value, and nil otherwise.
func (m *mover) ownedLocal(id *ast.Ident) ast.Decl {
	switch d := id.Def.(type) {
	case *ast.Var:
		if !m.c.globals[d] && m.c.table.Owned(d.Type) {
			return d
		}
	case *ast.Param:
		if d.Own && m.c.table.Owned(d.Type) {
			return d
		}
	}
	return nil
}

// errorNotes reports an error, unless the loop pass being followed is not
// the one that reports.
func (m *mover) errorNotes(span ast.Span, notes []diag.Diagnostic, format string, args ...any) {
	if m.report {
		m.c.errorNotes(span, notes, format, args...)
	}
}

// block follows the statements of a block from in, and returns what is
// known at its end.
func (m *mover) block(body []ast.Stmt, in flow) flow {
	m.blocks = append(m.blocks, &blockFlow{})
	defer func() { m.blocks = m.blocks[:len(m.blocks)-1] }()
	cur := in
	for _, stmt := range body {
		cur = m.stmt(stmt, cur)
	}
	return cur
}

// stmt follows one statement from cur, and returns what is known after
// it.
func (m *mover) stmt(stmt ast.Stmt, cur flow) flow {
	switch s := stmt.(type) {
	case *ast.Return:
		for i, v := range s.Values {
			m.value(v, m.resultCtx(i, v), &cur)
		}
		return dead()
	case *ast.If:
		return m.ifStmt(s, cur)
	case *ast.Switch:
		return m.switchStmt(s, cur)
	case *ast.For:
		return m.forStmt(s, cur)
	case *ast.ForRange:
		return m.rangeStmt(s, cur)
	case *ast.Defer:
		m.deferStmt(s, cur)
		return cur
	case *ast.Break, *ast.Continue:
		return m.jump(s, cur)
	default:
		return m.simpleFlow(stmt, cur)
	}
}

// simpleFlow follows a statement that holds no other from cur, and
// returns what is known after it: nothing, after a call to exit, which
// never returns, as after a return.
func (m *mover) simpleFlow(stmt ast.Stmt, cur flow) flow {
	m.simpleStmt(stmt, &cur)
	if s, ok := stmt.(*ast.ExprStmt); ok && isExitStmt(s) {
		return dead()
	}
	return cur
}

// ifStmt follows an if from cur: its condition, then each branch from
// what is known after it, and returns their join, an if without an else
// joining what its condition left.
func (m *mover) ifStmt(s *ast.If, cur flow) flow {
	m.expr(s.Cond, readCtx, &cur)
	then := m.block(s.Then, cur.clone())
	els := cur
	if s.Else != nil {
		els = m.block(s.Else, cur.clone())
	}
	return join(then, els)
}

// jump follows a break or a continue, s, from cur: what is known there
// joins its loop's breaks or continues, and nothing reaches past it.
func (m *mover) jump(s ast.Stmt, cur flow) flow {
	n := len(m.loops)
	if n == 0 {
		return dead()
	}
	lf := m.loops[n-1]
	if _, isBreak := s.(*ast.Break); isBreak {
		lf.breaks = join(lf.breaks, cur)
	} else {
		lf.continues = join(lf.continues, cur)
	}
	return dead()
}

// simpleStmt follows a statement that holds no other, changing cur.
func (m *mover) simpleStmt(stmt ast.Stmt, cur *flow) {
	switch s := stmt.(type) {
	case *ast.Var:
		if s.Value != nil {
			m.value(s.Value, m.ctxFor(s.Type), cur)
		}
		m.declare(s, cur)
	case *ast.MultiVar:
		m.multiVar(s, cur)
	case *ast.Assign:
		m.assign(s, cur)
	case *ast.MultiAssign:
		m.multiAssign(s, cur)
	case *ast.IncDec:
		m.rangeChange(s.Target, "assign")
		m.expr(s.Target, readCtx, cur)
	case *ast.ExprStmt:
		m.exprStmt(s.X, cur)
	}
}

// multiVar follows a declaration of several variables: its call, whose
// results each variable owns, and a result of an owned type bound to _.
func (m *mover) multiVar(s *ast.MultiVar, cur *flow) {
	m.binding(s.Value, cur)
	blank := make([]bool, len(s.Vars))
	for i, v := range s.Vars {
		blank[i] = v.Name == ast.Blank
		m.declare(v, cur)
	}
	m.droppedResults(s.Value, blank, varSpans(s.Vars))
}

// assign follows an assignment: a compound one reads its target and its
// value, and a plain one gives its value to its target (see
// assignTarget), moving it when it is owned.
func (m *mover) assign(s *ast.Assign, cur *flow) {
	if s.Op != ast.InvalidOp {
		m.rangeChange(s.Target, "assign")
		m.expr(s.Target, readCtx, cur)
		m.expr(s.Value, readCtx, cur)
		return
	}
	m.value(s.Value, m.ctxFor(ast.TypeOf(s.Target)), cur)
	m.assignTarget(s.Target, cur)
}

// multiAssign follows an assignment of several values: its call, then
// each target but _, which a result of an owned type cannot be.
func (m *mover) multiAssign(s *ast.MultiAssign, cur *flow) {
	m.binding(s.Value, cur)
	blank := make([]bool, len(s.Targets))
	spans := make([]ast.Span, len(s.Targets))
	for i, t := range s.Targets {
		blank[i], spans[i] = ast.IsBlank(t), t.Spanned()
		if !blank[i] {
			m.assignTarget(t, cur)
		}
	}
	m.droppedResults(s.Value, blank, spans)
}

// varSpans returns where each of vars is named.
func varSpans(vars []*ast.Var) []ast.Span {
	spans := make([]ast.Span, len(vars))
	for i, v := range vars {
		spans[i] = v.NameSpan
	}
	return spans
}

// ctxFor returns the context a value of type typ is given to a variable,
// a result or a field in: moved, for an owned type, and read otherwise.
func (m *mover) ctxFor(typ types.Type) useCtx {
	if m.c.table.Owned(typ) {
		return moveCtx
	}
	return readCtx
}

// resultCtx returns the context v, the value a return gives as the
// function's result at i, is used in: read through a view, when the
// result is a view, which points into the value in place, and otherwise
// as ctxFor says for v's type.
func (m *mover) resultCtx(i int, v ast.Expr) useCtx {
	if i < len(m.fn.ResultTypes) && m.c.table.IsView(m.fn.ResultTypes[i]) {
		return viewCtx
	}
	return m.ctxFor(ast.TypeOf(v))
}

// declare records v, a variable just declared, as owning its value, and,
// in deferred code, as one the deferred code may move.
func (m *mover) declare(v *ast.Var, cur *flow) {
	delete(cur.moved, v)
	if m.deferring {
		m.deferLocals[v] = true
	}
}

// value follows e, the whole value given to a variable, a result or a
// field, in ctx.
func (m *mover) value(e ast.Expr, ctx useCtx, cur *flow) {
	m.expr(e, ctx, cur)
}

// binding follows the value of a binding of several results, a call,
// whose results the binding takes, owned ones included.
func (m *mover) binding(value ast.Expr, cur *flow) {
	if call, ok := value.(*ast.Call); ok {
		m.call(call, moveCtx, cur)
		return
	}
	m.expr(value, readCtx, cur)
}

// droppedResults reports each result of value, a call bound by several
// names, blank saying which are _ and spans where each is written, that is
// owned and bound to _, which would drop it.
func (m *mover) droppedResults(value ast.Expr, blank []bool, spans []ast.Span) {
	call, ok := value.(*ast.Call)
	if !ok {
		return
	}
	fn := ast.Callee(call)
	name, named := m.c.calleeName(call)
	results := ast.CallResults(call)
	if fn == nil || !named || len(results) != len(blank) {
		return
	}
	for i, typ := range results {
		if blank[i] && m.c.table.Owned(typ) {
			m.errorNotes(spans[i], nil, "%s returns %s; bind it to a variable instead of _",
				name, m.c.droppedName(typ))
		}
	}
}

// droppedName names typ, an owned type, in an error about dropping one,
// and says it cannot be: a list or a map by its type, and anything
// holding one by its type and what it holds.
func (c *Checker) droppedName(typ types.Type) string {
	if c.table.IsList(typ) || c.table.IsMap(typ) {
		return c.typeName(typ) + ", which cannot be dropped"
	}
	if c.table.IsTypeParam(typ) {
		return c.typeName(typ) + ", which may hold a list and cannot be dropped"
	}
	if !c.table.HoldsList(typ) {
		return c.typeName(typ) + ", which holds a map and cannot be dropped"
	}
	return c.typeName(typ) + ", which holds a list and cannot be dropped"
}

// exprStmt follows e, an expression statement, which drops its value, or
// every result of a call of several: one of an owned type, which nothing
// would free, is reported, the first such result of several. pop,
// remove and replace written as a statement free the value they drop, so
// theirs is not.
func (m *mover) exprStmt(e ast.Expr, cur *flow) {
	call, ok := e.(*ast.Call)
	if !ok {
		m.expr(e, readCtx, cur)
		return
	}
	m.call(call, moveCtx, cur)
	if isTake(call) || isReplace(call) {
		return
	}
	results := []types.Type{ast.TypeOf(call)}
	name := "make"
	if callee, ok := m.c.calleeName(call); ok {
		name = callee
		if fn := ast.Callee(call); fn != nil {
			results = ast.CallResults(call)
		}
	}
	for _, typ := range results {
		if m.c.table.Owned(typ) {
			m.errorNotes(call.Spanned(), nil, "%s returns %s; bind it to a variable",
				name, m.c.droppedName(typ))
			return
		}
	}
}

// assignTarget follows target, a place assigned whole, after its value:
// a variable assigned whole owns its new value, and a field or an element
// needs the variable holding it usable. A place a range loop around it
// walks, or one holding that place, cannot be assigned.
func (m *mover) assignTarget(target ast.Expr, cur *flow) {
	m.rangeChange(target, "assign")
	if id, ok := target.(*ast.Ident); ok {
		if v := m.ownedLocal(id); v != nil {
			delete(cur.moved, v)
		}
		return
	}
	m.placeParts(target, cur)
}

// placeParts follows a field or an element being assigned or passed as
// mut: the variable it starts from must be usable, and every index on the
// way is read.
func (m *mover) placeParts(place ast.Expr, cur *flow) {
	switch x := place.(type) {
	case *ast.Ident:
		m.use(x, cur)
	case *ast.Selector:
		m.placeParts(x.X, cur)
	case *ast.Index:
		m.placeParts(x.X, cur)
		m.expr(x.Index, readCtx, cur)
	default:
		m.expr(place, readCtx, cur)
	}
}

// expr follows e, used in ctx.
func (m *mover) expr(e ast.Expr, ctx useCtx, cur *flow) {
	if _, _, payload, built := ast.Construction(e); built {
		m.construction(e, payload, ctx, cur)
		return
	}
	switch x := e.(type) {
	case *ast.Ident:
		m.ident(x, ctx, cur)
	case *ast.Selector, *ast.Index:
		m.part(x, ctx, cur)
	case *ast.Call:
		m.call(x, ctx, cur)
	case *ast.CompositeLit:
		m.literal(x, ctx, cur)
	case *ast.MutArg, *ast.OwnArg:
		m.argument(x, cur)
	case *ast.KeyValue:
		m.expr(x.Value, ctx, cur)
	default:
		for _, part := range readParts(e) {
			m.expr(part, readCtx, cur)
		}
	}
}

// argument follows e, a mut argument, whose place must be usable and may
// not be one a range loop or a switch around it holds, or an own argument,
// whose variable it moves.
func (m *mover) argument(e ast.Expr, cur *flow) {
	if x, ok := e.(*ast.MutArg); ok {
		m.rangeChange(x.X, "pass mut")
		m.placeParts(x.X, cur)
		return
	}
	m.expr(ast.ArgOperand(e), moveCtx, cur)
}

// part follows e, a field or an element read in ctx (see selector and
// element).
func (m *mover) part(e ast.Expr, ctx useCtx, cur *flow) {
	if x, ok := e.(*ast.Selector); ok {
		m.selector(x, ctx, cur)
		return
	}
	m.element(e.(*ast.Index), ctx, cur)
}

// selector follows a field read in ctx: moving one on its own is an
// error, and what it is selected from is read.
func (m *mover) selector(x *ast.Selector, ctx useCtx, cur *flow) {
	if ctx == moveCtx && m.c.table.Owned(x.Type) {
		m.errorNotes(
			x.Span,
			nil,
			"cannot move %s out of %s: a field cannot be moved on its own",
			m.c.text(x.Span),
			m.c.text(x.X.Spanned()),
		)
	}
	m.expr(x.X, readCtx, cur)
}

// element follows an element read in ctx: moving one of an owned type
// out of its list, map or view is an error, since the list or the map
// owns it, and what is indexed and the index are read.
func (m *mover) element(x *ast.Index, ctx useCtx, cur *flow) {
	if ctx == moveCtx && m.c.table.Owned(x.Type) {
		what, holder := "an element", "a list; take it out with pop or remove"
		switch typ := ast.TypeOf(x.X); {
		case m.c.table.IsView(typ):
			holder = "a view, which only borrows it"
		case m.c.table.IsMap(typ):
			what, holder = "a value", "a map; take it out with delete"
		}
		m.errorNotes(x.Span, nil, "cannot move %s out of %s: %s cannot be moved out of %s",
			m.c.text(x.Span), m.c.text(x.X.Spanned()), what, holder)
	}
	m.expr(x.X, readCtx, cur)
	m.expr(x.Index, readCtx, cur)
}

// readParts returns the operands e reads, in the order they are
// evaluated, when it is an element, a slice, an operator or a type
// assertion: what is indexed or sliced and the index or each bound
// written, each operand, or the any asked about. A literal or a type has
// none.
func readParts(e ast.Expr) []ast.Expr {
	switch x := e.(type) {
	case *ast.Index:
		return []ast.Expr{x.X, x.Index}
	case *ast.Slice:
		parts, _ := sliceParts(x)
		return parts
	case *ast.Binary:
		return []ast.Expr{x.X, x.Y}
	case *ast.Unary:
		return []ast.Expr{x.X}
	case *ast.TypeAssert:
		return []ast.Expr{x.X}
	default:
		return nil
	}
}

// ident follows a name used in ctx: a variable of an owned type must be
// usable, and in moveCtx it is moved, unless it cannot be (see canMove),
// which is all that is reported; a parameter of an owned type that is
// not own cannot be moved.
func (m *mover) ident(id *ast.Ident, ctx useCtx, cur *flow) {
	if v := m.ownedLocal(id); v != nil {
		if ctx == moveCtx && !m.canMove(id, v) {
			return
		}
		m.use(id, cur)
		if ctx == moveCtx {
			m.move(id, v, cur)
		}
		return
	}
	p, ok := id.Def.(*ast.Param)
	if !ok || ctx != moveCtx || !m.c.table.Owned(p.Type) {
		return
	}
	if p.Mut {
		m.errorNotes(
			id.Span,
			nil,
			"cannot move %s: it is a mut parameter, whose value stays its caller's",
			id.Name,
		)
		return
	}
	m.errorNotes(
		id.Span,
		nil,
		"cannot move %s: it is a parameter, which borrows its caller's value",
		id.Name,
	)
}

// movesBinding reports id, a name whose value is moved, when it names a
// union pattern's binding of an owned type, which refers to its payload in
// place, or a range loop's element or key variable that refers to an
// element of a list, a view or a map, or to a map's key, in place, and so
// cannot be moved, and returns whether it did; the error names which (see
// rangePart).
func (m *mover) movesBinding(id *ast.Ident) bool {
	v, ok := id.Def.(*ast.Var)
	if !ok || !m.c.table.Owned(v.Type) {
		return false
	}
	if x, inPlace := m.c.rangeBindings[v]; inPlace {
		part, holder := m.c.rangePart(v, x)
		m.errorNotes(
			id.Span,
			nil,
			"cannot move %s: it refers to %s of %s in place, and %s cannot be moved out of %s",
			id.Name,
			part,
			m.c.text(x.Spanned()),
			part,
			holder,
		)
		return true
	}
	if m.c.unionBindings[v] == nil {
		return false
	}
	m.errorNotes(
		id.Span,
		nil,
		"cannot move %s: it refers to a union's payload in place, and a payload cannot be moved out of a union",
		id.Name,
	)
	return true
}

// rangePart returns what v, a variable of a range loop over x that refers
// into it in place, refers to, a key or an element, and what x is, a map,
// a view or a list, as a message names them.
func (c *Checker) rangePart(v *ast.Var, x ast.Expr) (part, holder string) {
	typ := ast.TypeOf(x)
	switch {
	case c.rangeKeys[v]:
		return "a key", "a map"
	case c.table.IsMap(typ):
		return "an element", "a map"
	case c.table.IsView(typ):
		return "an element", "a view"
	default:
		return "an element", "a list"
	}
}

// use reports id, a name, when it names a variable that may have been
// moved where it is used, with a note at each place it was moved.
func (m *mover) use(id *ast.Ident, cur *flow) {
	v := m.ownedLocal(id)
	if v == nil {
		return
	}
	mf, moved := cur.moved[v]
	if !moved {
		return
	}
	notes := make([]diag.Diagnostic, len(mf.at))
	for i, at := range mf.at {
		notes[i] = note(at, declName(v)+" is moved here")
		if at == id.Span {
			notes[i] = note(at, declName(v)+" is moved here, in an earlier iteration of the loop")
		}
	}
	if mf.maybe {
		m.errorNotes(id.Span, notes, "%s is used after it may have been moved", id.Name)
	} else {
		m.errorNotes(id.Span, notes, "%s is used after it was moved", id.Name)
	}
	// Reported once: as far as the rest of the path goes, the use has
	// made it usable again, so a later use is not reported too.
	delete(cur.moved, v)
}

// canMove reports whether the value of v, named by id, can be moved where
// id is, and reports why not when it cannot: a union pattern's binding
// is never moved, deferred code cannot move a variable declared outside
// it, and a range loop cannot move a list it walks, or anything holding
// one.
func (m *mover) canMove(id *ast.Ident, v ast.Decl) bool {
	if m.movesBinding(id) {
		return false
	}
	if m.deferring && !m.deferLocals[v] {
		m.errorNotes(id.Span, nil,
			"a defer cannot move %s, which is declared outside it", declName(v))
		return false
	}
	return !m.rangeChange(id, "move")
}

// move moves the value of v, named by id, out of it: id is marked, so the
// backend empties v, and v is moved from here on. A defer that reads v
// and has yet to run would read a moved value, and is reported.
func (m *mover) move(id *ast.Ident, v ast.Decl, cur *flow) {
	id.Move = true
	m.deferReadsOf(v, id.Span)
	mf := cur.moved[v]
	mf.maybe = false
	mf.at = []ast.Span{id.Span}
	cur.moved[v] = mf
}

// deferReadsOf reports every read of v by a defer whose block has not yet
// ended, which v being moved at at would leave reading a moved value.
func (m *mover) deferReadsOf(v ast.Decl, at ast.Span) {
	for _, b := range m.blocks {
		for _, r := range b.reads {
			if r.v != v || m.reported[r.at] || !m.report {
				continue
			}
			m.reported[r.at] = true
			m.c.errorNotes(r.at, []diag.Diagnostic{note(at, declName(v)+" is moved here")},
				"%s is moved after this defer, which reads it when its block ends", declName(v))
		}
	}
}

// rangeChange reports place, which the statement being followed changes
// as what says, "assign", "pass mut" or "move", when a range loop around
// it walks a list it is, or holds: a place it is a prefix of; or one it
// is inside of, an element or a part of one, when the loop's element
// variable refers to the element in place, since any element may be the
// one it refers to; or when a clause of a switch on a union around it
// binds a payload of a place it is, or holds, in place; or when a range
// loop around it walks a view a call returns that may point into a place
// that may overlap it, either holding the other. It returns whether it
// did.
func (m *mover) rangeChange(place ast.Expr, what string) bool {
	for _, r := range m.ranging {
		switch {
		case r.view != nil:
			if !placesMayOverlap(place, r.place) {
				continue
			}
			m.errorNotes(
				place.Spanned(),
				nil,
				"%s is being ranged over, and the view it returns may point into %s, so this loop cannot %s %s",
				m.c.text(r.view.Span),
				m.c.text(r.place.Spanned()),
				what,
				m.c.text(place.Spanned()),
			)
			return true
		case prefixOf(place, r.place):
			m.errorNotes(place.Spanned(), nil, "%s is being ranged over, so this loop cannot %s %s",
				m.c.text(r.place.Spanned()), what, m.c.text(place.Spanned()))
			return true
		case r.bind != nil && prefixOf(r.place, place):
			m.errorNotes(place.Spanned(), nil,
				"%s is being ranged over, and %s refers into it, so this loop cannot %s %s",
				m.c.text(r.place.Spanned()), r.bind.Name, what, m.c.text(place.Spanned()))
			return true
		}
	}
	for _, mt := range m.matching {
		if !prefixOf(place, mt.place) {
			continue
		}
		m.errorNotes(place.Spanned(), nil,
			"%s is being matched, and %s refers into it, so this case cannot %s %s",
			m.c.text(mt.place.Spanned()), mt.bind.Name, what, m.c.text(place.Spanned()))
		return true
	}
	return false
}

// prefixOf reports whether the place a is b or holds it: both start from
// the same variable or parameter, and each step of a is b's step at the
// same depth, a field of the same name or an element at any index.
func prefixOf(a, b ast.Expr) bool {
	rootA, stepsA := path(a)
	rootB, stepsB := path(b)
	if rootA == nil || rootA != rootB || len(stepsA) > len(stepsB) {
		return false
	}
	for i, s := range stepsA {
		if (s.index == nil) != (stepsB[i].index == nil) ||
			s.index == nil && s.field != stepsB[i].field {
			return false
		}
	}
	return true
}

// call follows a call used in ctx: its arguments, a method call's
// receiver first, then, for one of an owned type, that its result has an
// owner. A plain receiver is read as a plain argument is, borrowed, so a
// temporary of an owned type is one no variable owns; a value passed to
// an own parameter, appended to a list of an owned type, or stored by
// replace, or a key and a value put into a map, is moved into it.
func (m *mover) call(call *ast.Call, ctx useCtx, cur *flow) {
	params := m.callParams(call)
	moved := m.appendsOwned(call) || isReplace(call) || isPut(call)
	for i, arg := range call.Operands() {
		if madeType(arg) {
			continue
		}
		argCtx := readCtx
		switch {
		case i < len(params) && params[i].view:
			argCtx = viewCtx
		case i < len(params) && params[i].own, moved && i > 0:
			argCtx = moveCtx
		}
		m.expr(arg, argCtx, cur)
	}
	m.temporary(call, ctx)
}

// appendsOwned reports whether call is a call to append of values, not a
// spread, onto a list of an owned type, which moves each value in.
func (m *mover) appendsOwned(call *ast.Call) bool {
	if !isAppend(call) || call.Spread || len(call.Args) == 0 {
		return false
	}
	mut, ok := call.Args[0].(*ast.MutArg)
	if !ok {
		return false
	}
	list := ast.TypeOf(mut.X)
	return m.c.table.IsList(list) && m.c.table.Owned(m.c.table.Elem(list))
}

// argMode is how a call passes one of its operands: to a view parameter,
// or spread into a variadic one, where viewArg checks a list that is not
// a place, or to an own parameter, which takes its value.
type argMode struct {
	view, own bool
}

// callParams says, for each operand of call, a method call's receiver
// first, how it is passed (see argMode), append's included.
func (m *mover) callParams(call *ast.Call) []argMode {
	if isAppend(call) {
		return appendViews(call)
	}
	fn := ast.Callee(call)
	if fn == nil {
		return nil
	}
	operands := call.Operands()
	modes := make([]argMode, len(operands))
	for i := range operands {
		if i >= len(fn.Params) {
			break
		}
		p := fn.Params[i]
		modes[i] = argMode{
			view: m.c.table.IsView(p.Type) && (!p.Variadic || call.Spread),
			own:  p.Own,
		}
	}
	return modes
}

// appendViews says, for each argument of call, a call to append, how it is
// passed: only a spread, its last argument, is a view.
func appendViews(call *ast.Call) []argMode {
	modes := make([]argMode, len(call.Args))
	if call.Spread && len(call.Args) > 0 {
		modes[len(call.Args)-1].view = true
	}
	return modes
}

// temporary reports e, a value no variable holds, used in ctx, when it is
// of an owned type and ctx gives it no owner.
func (m *mover) temporary(e ast.Expr, ctx useCtx) {
	typ := ast.TypeOf(e)
	if ctx == moveCtx || !m.c.table.Owned(typ) {
		return
	}
	if ctx == viewCtx && m.c.table.IsList(typ) {
		return
	}
	m.errorNotes(e.Spanned(), nil, "cannot use a temporary %s here; store it in a variable first",
		m.c.typeName(typ))
}

// construction follows e, a union built of payload, nil for none, used in
// ctx: an owned payload moves into it, as a struct literal's field's
// value does, and a union of an owned type must have an owner.
func (m *mover) construction(e, payload ast.Expr, ctx useCtx, cur *flow) {
	if payload != nil {
		m.expr(payload, m.ctxFor(ast.TypeOf(payload)), cur)
	}
	m.temporary(e, ctx)
}

// literal follows a composite literal used in ctx: an owned field's value
// moves into it, and a literal of an owned type must have an owner.
func (m *mover) literal(lit *ast.CompositeLit, ctx useCtx, cur *flow) {
	for _, e := range lit.Elems {
		kv, ok := e.(*ast.KeyValue)
		if !ok {
			m.expr(e, readCtx, cur)
			continue
		}
		fieldCtx := readCtx
		if key, isName := kv.Key.(*ast.Ident); isName {
			if f, _, found := m.c.table.Field(lit.Type, key.Name); found {
				fieldCtx = m.ctxFor(f.Type)
			}
		}
		m.expr(kv.Value, fieldCtx, cur)
	}
	m.temporary(lit, ctx)
}

// switchStmt follows a switch from cur: its tag, then each case's values,
// which a type switch's are types, and a union switch's patterns, and so
// read nothing, and body, every body starting from what is known after
// the tag, and returns their join, with cur when no default, or every
// case of a union, runs a body. A clause of a switch on a union that
// binds a payload of a place, in place, is followed with the place being
// matched (see rangeChange).
func (m *mover) switchStmt(s *ast.Switch, cur flow) flow {
	union := false
	if s.Tag != nil {
		m.expr(s.Tag, readCtx, &cur)
		union = m.c.table.IsUnion(ast.TypeOf(s.Tag)) && !s.IsTypeSwitch()
	}
	out := dead()
	for _, cl := range s.Cases {
		for _, v := range cl.Values {
			if !s.IsTypeSwitch() && !union {
				m.expr(v, readCtx, &cur)
			}
		}
		out = join(out, m.clause(s, cl, union, cur.clone()))
	}
	if s.Default() == nil && !s.Exhaustive {
		out = join(out, cur)
	}
	return out
}

// clause follows the body of cl, a case of s, from in, with the place s
// switches on being matched while it runs when s is on a union, union,
// and cl binds a payload of it in place.
func (m *mover) clause(s *ast.Switch, cl *ast.CaseClause, union bool, in flow) flow {
	bound := union && cl.Var != nil && m.c.unionBindings[cl.Var] != nil && ast.Root(s.Tag) != nil
	if !bound {
		return m.block(cl.Body, in)
	}
	m.matching = append(m.matching, matched{place: s.Tag, bind: cl.Var})
	defer func() { m.matching = m.matching[:len(m.matching)-1] }()
	return m.block(cl.Body, in)
}

// forStmt follows a loop from cur: its init once, then its condition, body
// and post statement twice (see loopTwice).
func (m *mover) forStmt(loop *ast.For, cur flow) flow {
	if loop.Init != nil {
		m.simpleStmt(loop.Init, &cur)
	}
	return m.loopTwice(cur, loop.Cond != nil, func(in flow) flow {
		if loop.Cond != nil {
			m.expr(loop.Cond, readCtx, &in)
		}
		return in
	}, loop.Body, loop.Post)
}

// rangeStmt follows a range loop from cur: what it ranges over, once,
// then its body twice, during which a list or a map it walks cannot be
// changed,
// and nor can any place a view a call returns, which it walks, may point
// into.
func (m *mover) rangeStmt(loop *ast.ForRange, cur flow) flow {
	m.expr(loop.X, readCtx, &cur)
	n := len(m.ranging)
	defer func() { m.ranging = m.ranging[:n] }()
	if typ := ast.TypeOf(
		loop.X,
	); (m.c.table.IsList(typ) || m.c.table.IsMap(typ)) &&
		ast.Root(loop.X) != nil {
		r := ranged{place: loop.X}
		for _, v := range []*ast.Var{loop.Var, loop.Elem} {
			if _, inPlace := m.c.rangeBindings[v]; inPlace && v.Name != ast.Blank {
				r.bind = v
			}
		}
		m.ranging = append(m.ranging, r)
	}
	if call := m.c.viewCallOf(loop.X); call != nil {
		for _, place := range m.c.viewBorrows(call) {
			m.ranging = append(m.ranging, ranged{place: place, view: call})
		}
	}
	return m.loopTwice(cur, true, func(in flow) flow { return in }, loop.Body, nil)
}

// loopTwice follows a loop's body from in, twice: first from in, without
// reporting, then from in joined with what the first pass left at the end
// of the body, its continues included, which is what the next iteration
// starts from. head follows what runs before each iteration's body, and
// post, when not nil, after it. It returns what is known after the loop:
// what its breaks left, joined, when it has a condition that can end it,
// mayEnd, with what the condition saw.
//
// A loop inside a pass that does not report, the first pass of a loop
// around it, is followed once, from in: the second pass is only for its
// errors, and a pass moves the same variables whatever it starts from, so
// what the loop leaves after one pass, joined with in, already holds every
// variable it may move. Following it twice there too would follow a loop
// nested n deep 2^n times.
func (m *mover) loopTwice(
	in flow,
	mayEnd bool,
	head func(flow) flow,
	body []ast.Stmt,
	post ast.Stmt,
) flow {
	pass := func(start flow) (flow, flow, *loopFlow) {
		start = head(start)
		lf := &loopFlow{breaks: dead(), continues: dead()}
		m.loops = append(m.loops, lf)
		end := m.block(body, start.clone())
		m.loops = m.loops[:len(m.loops)-1]
		end = join(end, lf.continues)
		if post != nil && !end.dead {
			m.simpleStmt(post, &end)
		}
		return start, end, lf
	}
	if m.report {
		m.report = false
		_, end, _ := pass(in.clone())
		m.report = true
		in = join(in, end)
	}
	start, end, lf := pass(in)
	out := lf.breaks
	if mayEnd {
		out = join(out, join(start, end))
	}
	return out
}

// deferStmt follows a defer's body where it is written, from a copy of
// cur, since it runs when its block ends and what it does leaves the
// statements after it untouched. It records the owned variables declared
// outside it that it reads, which nothing after it in its block may move.
func (m *mover) deferStmt(d *ast.Defer, cur flow) {
	outerDeferring, outerLocals, outerLoops := m.deferring, m.deferLocals, m.loops
	m.deferring, m.deferLocals, m.loops = true, make(map[ast.Decl]bool), nil
	in := cur.clone()
	m.block(d.Body, in)
	reads := m.deferReads(d)
	m.deferring, m.deferLocals, m.loops = outerDeferring, outerLocals, outerLoops
	if n := len(m.blocks); n > 0 {
		m.blocks[n-1].reads = append(m.blocks[n-1].reads, reads...)
	}
}

// deferReads returns every name in d's body of an owned local variable or
// own parameter declared outside d.
func (m *mover) deferReads(d *ast.Defer) []deferRead {
	var reads []deferRead
	for _, stmt := range d.Body {
		ast.Walk(stmt, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if v := m.ownedLocal(id); v != nil && !m.deferLocals[v] {
				reads = append(reads, deferRead{v: v, at: id.Span})
			}
			return true
		})
	}
	return reads
}
