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
// literal's field moves it, and the variable it came from cannot be used
// again until it is assigned a new value. A plain parameter of an owned
// type borrows its argument, which its caller keeps, and a mut parameter
// refers to its caller's variable, so neither can be moved, and nor can
// a field, alone, out of the struct holding it.
//
// checkMoves follows each function body in the order it runs, keeping for
// each local variable of an owned type whether it has been moved, on every
// path that reaches the point being checked or on some of them, and where.
// Control flow is structured: an if and a switch join what their branches
// leave, a branch that returns, breaks or continues leaving nothing to
// join; a loop's body is followed twice, the second time from what the
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
// anything holding it, pass either as mut, or move it.
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
	moved map[*ast.Var]moveFact
	dead  bool
}

// live returns a flow at a reachable point where nothing has been moved.
func live() flow {
	return flow{moved: make(map[*ast.Var]moveFact)}
}

// dead returns a flow at a point no path reaches.
func dead() flow {
	return flow{dead: true, moved: make(map[*ast.Var]moveFact)}
}

// clone returns a copy of f that can be changed without changing f.
func (f flow) clone() flow {
	moved := make(map[*ast.Var]moveFact, len(f.moved))
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

// deferRead is a read, at, of an owned variable v declared outside the
// defer that reads it.
type deferRead struct {
	v  *ast.Var
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
	c *Checker

	// deferLocals holds the variables the deferred code being followed
	// declares, which it may move.
	deferLocals map[*ast.Var]bool

	// reported holds the defer reads already reported, so a read of a
	// variable moved twice later is reported once.
	reported map[ast.Span]bool

	loops   []*loopFlow  // the loops enclosing the point, innermost last
	blocks  []*blockFlow // the blocks enclosing it, innermost last
	ranging []ast.Expr   // the lists being ranged over around it, innermost last

	// report says whether errors are reported: not while following a
	// loop's body for the first time, which the second pass repeats.
	report bool

	// deferring says whether deferred code is being followed.
	deferring bool
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
	m := &mover{c: c, report: true, reported: make(map[ast.Span]bool)}
	m.block(fn.Body, live())
}

// ownedLocal returns the local variable id names when it holds an owned
// value, and nil otherwise.
func (m *mover) ownedLocal(id *ast.Ident) *ast.Var {
	v, ok := id.Def.(*ast.Var)
	if !ok || m.c.globals[v] || !m.c.table.Owned(v.Type) {
		return nil
	}
	return v
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
		for _, v := range s.Values {
			m.value(v, m.ctxFor(ast.TypeOf(v)), &cur)
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
		m.simpleStmt(stmt, &cur)
		return cur
	}
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
	if fn == nil || !named || len(fn.ResultTypes) != len(blank) {
		return
	}
	for i, typ := range fn.ResultTypes {
		if blank[i] && m.c.table.Owned(typ) {
			m.errorNotes(spans[i], nil, "%s returns %s; bind it to a variable instead of _",
				name, m.droppedName(typ))
		}
	}
}

// droppedName names typ, an owned type, in an error about dropping one,
// and says it cannot be: a list by its type, and anything holding one by
// its type and what it holds.
func (m *mover) droppedName(typ types.Type) string {
	if m.c.table.IsList(typ) {
		return m.c.typeName(typ) + ", which cannot be dropped"
	}
	return m.c.typeName(typ) + ", which holds a list and cannot be dropped"
}

// exprStmt follows e, an expression statement, which drops its value, or
// every result of a call of several: one of an owned type, which nothing
// would free, is reported, the first such result of several.
func (m *mover) exprStmt(e ast.Expr, cur *flow) {
	call, ok := e.(*ast.Call)
	if !ok {
		m.expr(e, readCtx, cur)
		return
	}
	m.call(call, moveCtx, cur)
	results := []types.Type{ast.TypeOf(call)}
	name := "make"
	if callee, ok := m.c.calleeName(call); ok {
		name = callee
		if fn := ast.Callee(call); fn != nil {
			results = fn.ResultTypes
		}
	}
	for _, typ := range results {
		if m.c.table.Owned(typ) {
			m.errorNotes(call.Spanned(), nil, "%s returns %s; bind it to a variable",
				name, m.droppedName(typ))
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
	switch x := e.(type) {
	case *ast.Ident:
		m.ident(x, ctx, cur)
	case *ast.Selector:
		m.selector(x, ctx, cur)
	case *ast.Call:
		m.call(x, ctx, cur)
	case *ast.CompositeLit:
		m.literal(x, ctx, cur)
	case *ast.MutArg:
		m.rangeChange(x.X, "pass mut")
		m.placeParts(x.X, cur)
	case *ast.KeyValue:
		m.expr(x.Value, ctx, cur)
	default:
		for _, part := range readParts(e) {
			m.expr(part, readCtx, cur)
		}
	}
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
// which is all that is reported; a parameter of an owned type cannot be
// moved.
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
		notes[i] = note(at, v.Name+" is moved here")
		if at == id.Span {
			notes[i] = note(at, v.Name+" is moved here, in an earlier iteration of the loop")
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
// id is, and reports why not when it cannot: deferred code cannot move a
// variable declared outside it, and a range loop cannot move a list it
// walks, or anything holding one.
func (m *mover) canMove(id *ast.Ident, v *ast.Var) bool {
	if m.deferring && !m.deferLocals[v] {
		m.errorNotes(id.Span, nil, "a defer cannot move %s, which is declared outside it", v.Name)
		return false
	}
	return !m.rangeChange(id, "move")
}

// move moves the value of v, named by id, out of it: id is marked, so the
// backend empties v, and v is moved from here on. A defer that reads v
// and has yet to run would read a moved value, and is reported.
func (m *mover) move(id *ast.Ident, v *ast.Var, cur *flow) {
	id.Move = true
	m.deferReadsOf(v, id.Span)
	mf := cur.moved[v]
	mf.maybe = false
	mf.at = []ast.Span{id.Span}
	cur.moved[v] = mf
}

// deferReadsOf reports every read of v by a defer whose block has not yet
// ended, which v being moved at at would leave reading a moved value.
func (m *mover) deferReadsOf(v *ast.Var, at ast.Span) {
	for _, b := range m.blocks {
		for _, r := range b.reads {
			if r.v != v || m.reported[r.at] || !m.report {
				continue
			}
			m.reported[r.at] = true
			m.c.errorNotes(r.at, []diag.Diagnostic{note(at, v.Name+" is moved here")},
				"%s is moved after this defer, which reads it when its block ends", v.Name)
		}
	}
}

// rangeChange reports place, which the statement being followed changes
// as what says, "assign", "pass mut" or "move", when a range loop around
// it walks a list it is, or holds: a place it is a prefix of. It returns
// whether it did.
func (m *mover) rangeChange(place ast.Expr, what string) bool {
	for _, ranged := range m.ranging {
		if !prefixOf(place, ranged) {
			continue
		}
		m.errorNotes(place.Spanned(), nil, "%s is being ranged over, so this loop cannot %s %s",
			m.c.text(ranged.Spanned()), what, m.c.text(place.Spanned()))
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
// temporary of an owned type is one no variable owns.
func (m *mover) call(call *ast.Call, ctx useCtx, cur *flow) {
	params := m.callParams(call)
	for i, arg := range call.Operands() {
		if _, isType := arg.(*ast.ListType); isType {
			continue
		}
		argCtx := readCtx
		if i < len(params) && params[i] {
			argCtx = viewCtx
		}
		m.expr(arg, argCtx, cur)
	}
	m.temporary(call, ctx)
}

// callParams says, for each operand of call, a method call's receiver
// first, whether it is passed to a view parameter, or spread into a
// variadic one, append's included, where viewArg checks a list that is
// not a place.
func (m *mover) callParams(call *ast.Call) []bool {
	if isAppend(call) {
		return appendViews(call)
	}
	fn := ast.Callee(call)
	if fn == nil {
		return nil
	}
	operands := call.Operands()
	views := make([]bool, len(operands))
	for i := range operands {
		if i >= len(fn.Params) {
			break
		}
		p := fn.Params[i]
		views[i] = m.c.table.IsView(p.Type) && (!p.Variadic || call.Spread)
	}
	return views
}

// appendViews says, for each argument of call, a call to append, whether
// it is a view: only a spread, its last argument, is.
func appendViews(call *ast.Call) []bool {
	views := make([]bool, len(call.Args))
	if call.Spread && len(call.Args) > 0 {
		views[len(call.Args)-1] = true
	}
	return views
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
// which a type switch's are types, and so read nothing, and body, every body starting from what is known after the tag, and
// returns their join, with cur when no default runs a body.
func (m *mover) switchStmt(s *ast.Switch, cur flow) flow {
	if s.Tag != nil {
		m.expr(s.Tag, readCtx, &cur)
	}
	out := dead()
	hasDefault := false
	for _, cl := range s.Cases {
		for _, v := range cl.Values {
			if !s.IsTypeSwitch() {
				m.expr(v, readCtx, &cur)
			}
		}
		hasDefault = hasDefault || cl.Default
		out = join(out, m.block(cl.Body, cur.clone()))
	}
	if !hasDefault {
		out = join(out, cur)
	}
	return out
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
// then its body twice, during which a list it walks cannot be changed.
func (m *mover) rangeStmt(loop *ast.ForRange, cur flow) flow {
	m.expr(loop.X, readCtx, &cur)
	if m.c.table.IsList(ast.TypeOf(loop.X)) && ast.Root(loop.X) != nil {
		m.ranging = append(m.ranging, loop.X)
		defer func() { m.ranging = m.ranging[:len(m.ranging)-1] }()
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
	m.deferring, m.deferLocals, m.loops = true, make(map[*ast.Var]bool), nil
	in := cur.clone()
	m.block(d.Body, in)
	reads := m.deferReads(d)
	m.deferring, m.deferLocals, m.loops = outerDeferring, outerLocals, outerLoops
	if n := len(m.blocks); n > 0 {
		m.blocks[n-1].reads = append(m.blocks[n-1].reads, reads...)
	}
}

// deferReads returns every name in d's body of an owned local variable
// declared outside d.
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
