package typeck

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// The escape rule. A value is from a region r when its type owns memory
// and it may hold memory r handed out: it is made inside r's block by an
// expression given r, make(list[T], r), a call with r among its operands
// or with a value from r among them, at any depth, or a closure capturing
// r or a value from r among them, a literal holding a value from r, or
// it is a variable a value from r was declared with, assigned to, moved,
// appended, put, added or inserted into, or passed as mut to a call
// alongside one. A value from r may not reach anything that lives longer
// than r's block, and each way one could is reported, with a note at the
// region:
//   - a return, from the function or from a closure, whose results count
//     as outside every region;
//   - an assignment to a variable declared outside the block, or to a
//     field or an element of one, a global, a parameter and a closure's
//     parameter among them, all of which live outside every region;
//   - a call taking a value from r together with a mut argument declared
//     outside the block, or a closure that writes a variable declared
//     outside it, since the callee could store one into the other:
//     append, put, replace, add, add_all and insert among them, whose mut
//     target is what they store into. An operand the call passes as a
//     view, to a view parameter or spread, counts as owning nothing, as a
//     view does: through one the callee reads, and keeps nothing;
//   - a call through a scope parameter of a function type, or one that
//     passes such a parameter on, by name or captured in a closure,
//     taking any operand from r, in any position, r itself included: the
//     closure behind the parameter is the caller's, unknown here, and may
//     write anything it captures, which no call site in this function
//     shows.
//
// Regions nest, so the rule is checked by depth: each region is one
// deeper than the regions around it, in its function or the closure it
// is written in, each variable lives at the depth it is declared at, its
// home, 0 for a parameter, a closure's included, and for a global, and
// may hold memory from the region at its mark, at most its home, the
// innermost region a value it was given came from, 0 for none. A value
// whose depth is d may reach a variable whose home is at least d: the
// region at depth d holds the variable's block, and so outlives it. A
// value from an outer region may be used freely in an inner one, and one
// from an inner region may not reach a variable of the outer region's
// block declared outside the inner one's.
//
// The rule is checked per function, in the function and every closure in
// it, needing no annotation on a callee: a callee given r sees a scope
// Allocator, which it cannot keep (see scopealloc.go), and a value from r
// leaves it only through its results and its mut arguments, which the
// call's rules cover. A type that owns nothing, an integer, a string, an
// id, a handle, is never from a region, nor is a view, which cannot be
// stored; a type parameter may own memory, so a generic body is checked
// as if it did.
//
// Marks are found before anything is reported, by walking the function
// until no mark rises: a value may reach a variable after it was read, in
// a later pass of a loop, so the mark a read sees is the most the
// variable is ever given anywhere in the function. Marks only rise, and
// never past a home, so the walk ends. Each way out is then checked once,
// against those marks. A function holding no region is not walked.

// escaper walks a function's body to find and check the escape rule (see
// above).
type escaper struct {
	c *Checker

	// regions are the regions around the walk, outermost first, so the
	// region at depth d is regions[d-1].
	regions []*ast.Region

	// home and mark are each variable's home and mark (see above), 0 for
	// one not in them; depth is each region's allocator's region's depth.
	home, mark map[ast.Decl]int
	depth      map[*ast.Var]int

	// results are the results of the function and the closures around the
	// walk, innermost last, and closures says how many of them are a
	// closure's, which a return in one returns from.
	results  [][]types.Type
	closures int

	// reported counts what the walk has reported, so a call whose
	// closures reported a store does not report it again.
	reported int

	// report is whether this walk reports what escapes, the last one; the
	// walks before it only raise marks, and changed says whether one did.
	report, changed bool
}

// checkEscapes checks the escape rule in fn's body, which was checked
// with nothing reported, when it holds a region.
func (c *Checker) checkEscapes(fn *ast.Func) {
	if !holdsRegion(fn.Body) {
		return
	}
	e := &escaper{
		c:     c,
		home:  make(map[ast.Decl]int),
		mark:  make(map[ast.Decl]int),
		depth: make(map[*ast.Var]int),
	}
	for {
		e.changed = false
		e.walk(fn)
		if !e.changed {
			break
		}
	}
	e.report = true
	e.walk(fn)
}

// holdsRegion reports whether body holds a region, at any depth, in a
// closure included.
func holdsRegion(body []ast.Stmt) bool {
	found := false
	for _, stmt := range body {
		ast.Walk(stmt, func(n ast.Node) bool {
			if _, ok := n.(*ast.Region); ok {
				found = true
			}
			return !found
		})
	}
	return found
}

// walk walks fn's body once.
func (e *escaper) walk(fn *ast.Func) {
	e.results = [][]types.Type{fn.ResultTypes}
	e.closures = 0
	e.stmts(fn.Body)
}

// owned reports whether a value of type typ may hold memory: it owns
// some, or its type is not known, which is taken as owning.
func (e *escaper) owned(typ types.Type) bool {
	return typ == types.Invalid || e.c.table.Owned(typ)
}

// declare records v as declared at the walk's depth, given a value of
// depth d.
func (e *escaper) declare(v *ast.Var, d int) {
	if v == nil {
		return
	}
	e.home[v] = len(e.regions)
	e.raise(v, d)
}

// raise raises decl's mark to d, when its type may hold memory, never
// past its home: a value deeper than that was reported.
func (e *escaper) raise(decl ast.Decl, d int) {
	d = min(d, e.home[decl])
	if d > e.mark[decl] && e.owned(declType(decl)) {
		e.mark[decl] = d
		e.changed = true
	}
}

// declSrc returns the depth of what decl holds: its region's depth for a
// region's allocator, and its mark for anything else.
func (e *escaper) declSrc(decl ast.Decl) int {
	if v, ok := decl.(*ast.Var); ok {
		if d, ok := e.depth[v]; ok {
			return d
		}
	}
	return e.mark[decl]
}

// stmts walks the statements of a block.
func (e *escaper) stmts(body []ast.Stmt) {
	for _, stmt := range body {
		e.stmt(stmt)
	}
}

// stmt walks stmt and everything in it.
func (e *escaper) stmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.Region:
		e.regions = append(e.regions, s)
		e.depth[s.Var] = len(e.regions)
		e.home[s.Var] = len(e.regions)
		e.stmts(s.Body)
		e.regions = e.regions[:len(e.regions)-1]
	case *ast.Var:
		e.declare(s, e.src(s.Value))
	case *ast.MultiVar:
		d := e.src(s.Value)
		for _, v := range s.Vars {
			e.declare(v, d)
		}
	case *ast.Assign:
		d := e.src(s.Value)
		e.src(s.Target)
		e.store(s.Target, d, s.Value)
	case *ast.MultiAssign:
		d := e.src(s.Value)
		for _, t := range s.Targets {
			e.src(t)
			e.store(t, d, s.Value)
		}
	case *ast.ExprStmt:
		e.src(s.X)
	case *ast.IncDec:
		e.src(s.Target)
	case *ast.Return:
		e.ret(s)
	case *ast.If:
		e.src(s.Cond)
		e.stmts(s.Then)
		e.stmts(s.Else)
	case *ast.For:
		if s.Init != nil {
			e.stmt(s.Init)
		}
		e.src(s.Cond)
		if s.Post != nil {
			e.stmt(s.Post)
		}
		e.stmts(s.Body)
	case *ast.ForRange:
		// Its variables refer to what it ranges over, or copy from it.
		d := e.src(s.X)
		e.declare(s.Var, d)
		e.declare(s.Elem, d)
		e.stmts(s.Body)
	case *ast.Switch:
		e.switchStmt(s)
	case *ast.Defer:
		e.stmts(s.Body)
	}
}

// switchStmt walks s: its tag, whose depth its binding and each case's
// variable take, since each refers to the tag's value or a payload of
// it, then each case's values and body.
func (e *escaper) switchStmt(s *ast.Switch) {
	d := e.src(s.Tag)
	e.declare(s.Bind, d)
	for _, cl := range s.Cases {
		for _, v := range cl.Values {
			e.src(v)
		}
		e.declare(cl.Var, d)
		e.stmts(cl.Body)
	}
}

// ret walks a return, reporting a value from a region: a function's
// results, and a closure's, live outside every region.
func (e *escaper) ret(s *ast.Return) {
	results := e.results[len(e.results)-1]
	for i, v := range s.Values {
		d := e.src(v)
		if d == 0 || !e.report || (i < len(results) && !e.owned(results[i])) {
			continue
		}
		if e.closures > 0 {
			e.errorAt(
				v.Spanned(),
				d,
				"cannot return %s from a closure: a closure's results count as outside every region",
				e.holding(v, d),
			)
			continue
		}
		e.errorAt(
			v.Spanned(),
			d,
			"cannot return %s: the region gives its memory back when its block ends",
			e.holding(v, d),
		)
	}
}

// src walks x and returns its depth: the innermost region a value of it
// may hold memory from, 0 for none. Every call in it is checked on the
// way (see call).
func (e *escaper) src(x ast.Expr) int {
	switch x := x.(type) {
	case nil:
		return 0
	case *ast.Ident:
		return e.declSrc(x.Def)
	case *ast.FuncLit:
		return e.lit(x)
	case *ast.Call:
		return e.ifOwned(x, e.call(x))
	}
	d := 0
	ast.Walk(x, func(n ast.Node) bool {
		if n == ast.Node(x) {
			return true
		}
		if child, ok := n.(ast.Expr); ok {
			d = max(d, e.src(child))
		}
		return false
	})
	return e.ifOwned(x, d)
}

// ifOwned returns d, the depth of x, or 0 when x's type is known and owns
// nothing.
func (e *escaper) ifOwned(x ast.Expr, d int) int {
	if typ := ast.TypeOf(x); typ != types.Invalid && !e.c.table.Owned(typ) {
		return 0
	}
	return d
}

// lit walks x, a closure, and returns its depth, that of the deepest of
// its captures: a closure capturing a region's allocator, or a value from
// a region, counts as an argument from that region. Its body is walked
// as a function's, its parameters, which live outside every region, at
// home 0, and its returns its own.
func (e *escaper) lit(x *ast.FuncLit) int {
	e.results = append(e.results, x.Func.ResultTypes)
	e.closures++
	e.stmts(x.Func.Body)
	e.closures--
	e.results = e.results[:len(e.results)-1]
	d := 0
	for _, cp := range x.Captures {
		d = max(d, e.declSrc(cp.Decl))
	}
	return d
}

// call walks call, checks it and returns its depth, that of the deepest
// of its operands, which its result, if it owns memory, may hold; an
// operand passed as a view counts as owning nothing (see viewOperands). Each
// mut argument whose place may hold memory, and each variable a closure
// among the operands writes that may, could be given any operand's
// memory by the callee: each is reported when it lives outside the
// region the deepest operand is from, and its mark is raised to it
// otherwise. A call whose closures reported a store already is not
// reported again for it.
func (e *escaper) call(call *ast.Call) int {
	before := e.reported
	ops := call.Operands()
	srcs := make([]int, len(ops))
	views := viewOperands(call, e.c.table)
	d, from := 0, -1
	for i, op := range ops {
		srcs[i] = e.src(op)
		if views[op] {
			srcs[i] = 0
		}
		if srcs[i] > d {
			d, from = srcs[i], i
		}
	}
	if d == 0 {
		return 0
	}
	quiet := e.reported > before
	if e.report && !quiet {
		e.unknownWriter(call, ops, from, d)
	}
	for i, op := range ops {
		switch x := op.(type) {
		case *ast.MutArg:
			if e.owned(ast.TypeOf(x.X)) {
				e.target(call, x.X, d, ops, from, i, quiet)
			}
		case *ast.FuncLit:
			for _, cp := range x.Captures {
				if cp.Write && e.owned(declType(cp.Decl)) {
					id := &ast.Ident{Name: declName(cp.Decl), Def: cp.Decl, Span: cp.WriteAt}
					e.written(call, id, x, d, ops, from, quiet)
				}
			}
		}
	}
	return d
}

// unknownWriter reports call, given a value of depth d by the operand at
// ops[from], when it is a call through a scope parameter of a function
// type, or passes one on, by name or captured in a closure among its
// operands: the closure behind the parameter is the caller's, unknown
// here, and may write anything it captures, so it could keep the value
// past the region's end, whatever position it takes the value in.
func (e *escaper) unknownWriter(call *ast.Call, ops []ast.Expr, from, d int) {
	value := ops[from]
	what := e.holding(value, d)
	if e.isRegionAlloc(value) {
		what += ","
	}
	if p := e.scopeFunc(call.Fn); p != nil {
		e.errorAt(value.Spanned(), d,
			"cannot pass %s to %s, a scope parameter: the closure behind it could keep memory from "+
				"region %s in anything it captures",
			what, p.Name, e.regionName(d))
		return
	}
	for _, op := range ops {
		p := e.scopeFunc(op)
		if lit, ok := op.(*ast.FuncLit); ok {
			p = e.capturedScopeFunc(lit)
		}
		if p == nil {
			continue
		}
		e.errorAt(value.Spanned(), d,
			"cannot pass %s to %s alongside %s, a scope parameter: the closure behind it could keep "+
				"memory from region %s in anything it captures",
			what, e.calleeName(call), p.Name, e.regionName(d))
		return
	}
}

// scopeFunc returns the scope parameter of a function type x names, or
// nil when it names none.
func (e *escaper) scopeFunc(x ast.Expr) *ast.Param {
	id, ok := x.(*ast.Ident)
	if !ok {
		return nil
	}
	p, ok := id.Def.(*ast.Param)
	if !ok || !p.Scope || !e.c.table.IsFunc(p.Type) {
		return nil
	}
	return p
}

// capturedScopeFunc returns a scope parameter of a function type that
// lit captures, its nested closures' captures included, or nil.
func (e *escaper) capturedScopeFunc(lit *ast.FuncLit) *ast.Param {
	for _, cp := range lit.Captures {
		if p, ok := cp.Decl.(*ast.Param); ok && p.Scope && e.c.table.IsFunc(p.Type) {
			return p
		}
	}
	return nil
}

// viewOperands returns the operands of call that it passes as a view:
// each argument of a parameter whose type is a view, a variadic one's
// aside, and a spread, f(v...). A view owns nothing and is second class,
// so the callee can read through it but keep nothing it points to, and
// an operand passed as one is never from a region, whatever it is.
func viewOperands(call *ast.Call, table *types.Table) map[ast.Expr]bool {
	views := make(map[ast.Expr]bool)
	if call.Spread && len(call.Args) > 0 {
		views[call.Args[len(call.Args)-1]] = true
	}
	fn := ast.Callee(call)
	if fn == nil {
		return views
	}
	params := fn.Params
	if call.Receiver() != nil && len(params) > 0 {
		params = params[1:]
	}
	for i, arg := range call.Args {
		if i < len(params) && !params[i].Variadic && table.IsView(params[i].Type) {
			views[arg] = true
		}
	}
	return views
}

// target checks place, the mut argument at ops[at] of call, given a value
// of depth d by the operand at ops[from], and raises its root's mark.
func (e *escaper) target(
	call *ast.Call,
	place ast.Expr,
	d int,
	ops []ast.Expr,
	from, at int,
	quiet bool,
) {
	root := ast.Root(place)
	if root == nil || root.Def == nil {
		return
	}
	if e.home[root.Def] >= d {
		e.raise(root.Def, d)
		return
	}
	if !e.report || quiet || from == at {
		return
	}
	value := ops[from]
	where := e.outside(root, place, d)
	if verb, ok := storeVerb(call); ok {
		e.errorAt(value.Spanned(), d, "cannot %s: %s",
			fmt.Sprintf(verb, e.holding(value, d), e.c.text(place.Spanned())), where)
		return
	}
	e.errorAt(
		ops[at].Spanned(),
		d,
		"cannot pass mut %s to %s alongside %s: the call could keep memory from region %s in %s, and %s",
		e.c.text(place.Spanned()),
		e.calleeName(call),
		e.holding(value, d),
		e.regionName(d),
		e.c.text(place.Spanned()),
		where,
	)
}

// written checks id, a variable the closure lit among call's operands
// writes, given a value of depth d by the operand at ops[from], and
// raises its mark.
func (e *escaper) written(
	call *ast.Call,
	id *ast.Ident,
	lit *ast.FuncLit,
	d int,
	ops []ast.Expr,
	from int,
	quiet bool,
) {
	if e.home[id.Def] >= d {
		e.raise(id.Def, d)
		return
	}
	if !e.report || quiet {
		return
	}
	if ops[from] == ast.Expr(lit) {
		e.errorAt(
			lit.Func.NameSpan,
			d,
			"cannot pass a closure that writes %s and captures memory from region %s to %s: "+
				"the call could keep memory from region %s in %s, and %s",
			id.Name,
			e.regionName(d),
			e.calleeName(call),
			e.regionName(d),
			id.Name,
			e.outside(id, id, d),
		)
		return
	}
	e.errorAt(
		lit.Func.NameSpan,
		d,
		"cannot pass a closure that writes %s to %s alongside %s: the call could keep memory from region %s in %s, "+
			"and %s",
		id.Name,
		e.calleeName(call),
		e.holding(ops[from], d),
		e.regionName(d),
		id.Name,
		e.outside(id, id, d),
	)
}

// storeVerb returns, for a call to a builtin that stores a value in its
// mut target, append, put, replace, add, add_all or insert, how a message
// says the call stores a value in a place, as a format taking the value
// and the place, and whether call is one.
func storeVerb(call *ast.Call) (string, bool) {
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		return "", false
	}
	if fn, isFunc := id.Def.(*ast.Func); !isFunc || fn != nil {
		return "", false
	}
	switch id.Name {
	case "append":
		return "append %s to %s", true
	case "put":
		return "put %s in %s", true
	case "replace":
		return "replace the value of %[2]s with %[1]s", true
	case "add", "add_all":
		return "add %s to %s", true
	case "insert":
		return "insert %s into %s", true
	default:
		return "", false
	}
}

// store checks an assignment of value, of depth d, to target, and raises
// the mark of target's root.
func (e *escaper) store(target ast.Expr, d int, value ast.Expr) {
	root := ast.Root(target)
	if d == 0 || root == nil || root.Def == nil || !e.owned(ast.TypeOf(target)) {
		return
	}
	if e.home[root.Def] >= d {
		e.raise(root.Def, d)
		return
	}
	if !e.report {
		return
	}
	what := e.holding(value, d)
	if ast.TypeOf(value) == types.Invalid {
		what = fmt.Sprintf("a value of type %s holding memory from region %s",
			e.c.typeName(ast.TypeOf(target)), e.regionName(d))
	}
	e.errorAt(value.Spanned(), d, "cannot assign %s to %s: %s",
		what, e.c.text(target.Spanned()), e.outside(root, target, d))
}

// outside says, in a message, why place, rooted at root, outlives the
// region at depth d: where root is declared.
func (e *escaper) outside(root *ast.Ident, place ast.Expr, d int) string {
	subject := root.Name + " is"
	if place != ast.Expr(root) {
		subject = fmt.Sprintf("%s is part of %s,", e.c.text(place.Spanned()), root.Name)
	}
	switch decl := root.Def.(type) {
	case *ast.Param:
		if decl.Mut {
			return subject + " a mut parameter, which lives outside every region"
		}
		return subject + " a parameter, which lives outside every region"
	case *ast.Var:
		if e.c.globals[decl] {
			return subject + " a global, which lives outside every region"
		}
	}
	if place != ast.Expr(root) {
		return fmt.Sprintf(
			"%s which is declared outside the block of region %s",
			subject,
			e.regionName(d),
		)
	}
	return fmt.Sprintf("%s declared outside the block of region %s", subject, e.regionName(d))
}

// holding names x, a value of depth d, in a message, with the region it
// holds memory from.
func (e *escaper) holding(x ast.Expr, d int) string {
	if e.isRegionAlloc(x) {
		return fmt.Sprintf("%s, the allocator of region %s", e.c.text(x.Spanned()), e.regionName(d))
	}
	return fmt.Sprintf("%s holding memory from region %s", e.valueName(x), e.regionName(d))
}

// regionName returns the name of the region at depth d.
func (e *escaper) regionName(d int) string {
	return e.regions[d-1].Var.Name
}

// valueName names x, a value from a region, in a message: a closure as
// one, and anything else by its type, a list[int64] or a Holder.
func (e *escaper) valueName(x ast.Expr) string {
	if _, ok := x.(*ast.FuncLit); ok {
		return "a closure"
	}
	if typ := ast.TypeOf(x); typ != types.Invalid {
		return "a value of type " + e.c.typeName(typ)
	}
	return "a value"
}

// isRegionAlloc reports whether x names a region's allocator.
func (e *escaper) isRegionAlloc(x ast.Expr) bool {
	id, ok := x.(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := id.Def.(*ast.Var)
	if !ok {
		return false
	}
	_, ok = e.depth[v]
	return ok
}

// calleeName names what call calls in a message.
func (e *escaper) calleeName(call *ast.Call) string {
	if fn := ast.Callee(call); fn != nil && call.Dynamic == nil {
		return e.c.displayName(fn)
	}
	return e.c.text(call.Fn.Spanned())
}

// errorAt reports an escape at span, with a note at the region at depth
// d.
func (e *escaper) errorAt(span ast.Span, d int, format string, args ...any) {
	e.reported++
	e.c.errorNotes(span, e.c.regionNotes(e.regions[d-1]), format, args...)
}
