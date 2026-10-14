package typeck

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/types"
)

// Checker type-checks a program. It is single-use: create one with New and
// call Check once.
type Checker struct {
	prog  *ast.Program
	table *types.Table // the program's types, recorded on it by Check

	// files holds the source text of the program's files, which notes
	// quote; see text.
	files source.Files

	// checkedDivision is the division whose value is being bound with ok,
	// which needs no proof that its divisor is not zero.
	checkedDivision *ast.Binary

	// checkedIndex is the element whose value is being bound with ok, which
	// needs no proof that its index is in range.
	checkedIndex *ast.Index

	globals map[*ast.Var]bool // the package-level variables

	// globalState says how far each package-level variable's check has
	// got; see ensureGlobal.
	globalState map[*ast.Var]progress

	// structs maps each struct type to its declaration, and structOrder
	// lists them in source order. structState says how far each one's
	// fields have been resolved; see resolveFields.
	structs     map[types.Type]*ast.TypeDecl
	structOrder []types.Type
	structState map[types.Type]progress

	// knownVars remembers, for each local variable knownVar has been asked
	// about, what it found.
	knownVars map[*ast.Var]knownResult

	// active holds the facts in force where checking has reached, innermost
	// last, and guards the guards they came from, with any guard in scope
	// that gave none; see facts.go and explain.go.
	active []fact
	guards []*guard

	// declPkg holds the package of each package-level declaration, and
	// structPkg that of each struct type. pkg is the package whose code is
	// being checked, which a type of another package is named relative to:
	// see typeName.
	declPkg   map[ast.Decl]*ast.Package
	structPkg map[types.Type]*ast.Package
	pkg       *ast.Package

	// initStack holds the package-level variables being checked, the
	// outermost first, so an initialization cycle can be named in full.
	initStack []*ast.Var

	diags []diag.Diagnostic
	loops int // how many loops enclose the statement being checked

	// deferred is how many defers enclose the statement being checked, and
	// deferLoops how many loops enclose the innermost of them: a break or
	// continue with no more loops around it than that would leave the
	// deferred code.
	deferred   int
	deferLoops int

	// inRejectedDefer is whether the statement being checked is inside a
	// defer already reported as a defer inside a defer, where a further
	// nested defer is not reported again.
	inRejectedDefer bool

	// inFields is whether a struct's field types are being resolved, where
	// an array length must name no variable.
	inFields bool
}

// New returns a Checker for prog, which must already have been resolved,
// and whose spans point into files.
func New(prog *ast.Program, files source.Files) *Checker {
	return &Checker{
		prog:    prog,
		files:   files,
		table:   types.NewTable(),
		globals: make(map[*ast.Var]bool),

		globalState: make(map[*ast.Var]progress),
		structs:     make(map[types.Type]*ast.TypeDecl),
		structState: make(map[types.Type]progress),

		knownVars: make(map[*ast.Var]knownResult),

		declPkg:   declPackages(prog),
		structPkg: make(map[types.Type]*ast.Package),
	}
}

// declPackages returns the package of each package-level declaration of
// prog.
func declPackages(prog *ast.Program) map[ast.Decl]*ast.Package {
	m := make(map[ast.Decl]*ast.Package)
	if prog == nil {
		return m
	}
	for _, pkg := range prog.Packages {
		for _, decl := range pkg.Decls() {
			m[decl] = pkg
		}
	}
	return m
}

// typeName returns a type's name as a diagnostic about the package being
// checked writes it: a struct type of another package qualified by that
// package's name, lexer.Token, and any other type as types.Table.Name
// does.
func (c *Checker) typeName(typ types.Type) string {
	return c.table.NameWith(typ, func(st types.Type, name string) string {
		if pkg := c.structPkg[st]; pkg != nil && c.pkg != nil && pkg != c.pkg {
			return pkg.Name + "." + name
		}
		return name
	})
}

// enter makes the package of decl, a package-level declaration, the one
// being checked, and returns a function that puts back the one it replaced.
func (c *Checker) enter(decl ast.Decl) (restore func()) {
	old := c.pkg
	if pkg := c.declPkg[decl]; pkg != nil {
		c.pkg = pkg
	}
	return func() { c.pkg = old }
}

// Check reports every badly typed call in the program, in source order. Checking
// continues past the first problem, so one run reports all of them.
func (c *Checker) Check() []diag.Diagnostic {
	if c.prog == nil {
		panic("program is nil")
	}
	c.prog.Types = c.table
	for _, f := range c.prog.Files() {
		f.Types = c.table
	}
	decls := c.prog.Decls()

	// Every struct type exists before anything is checked, so any type can
	// name any struct. Their fields are resolved next, then the globals,
	// so the types and values of both are known in every function, and in
	// every signature, whose array lengths may name a global, whatever the
	// order of declarations. A field's array length names no variable, so
	// fields never need a global; a global's initializer may be a struct
	// literal, whose fields are resolved early if it comes first. A
	// global's initializer is a constant, so it never needs a function.
	c.declareStructs()
	c.checkGlobals(decls)

	// Signatures are resolved for every function before any body is checked,
	// so a call does not depend on its callee appearing first.
	for _, decl := range decls {
		if fn, ok := decl.(*ast.Func); ok {
			restore := c.enter(fn)
			c.resolveParamTypes(fn)
			c.resolveResultTypes(fn)
			restore()
		}
	}

	c.checkExposure(decls)

	for _, decl := range decls {
		fn, ok := decl.(*ast.Func)
		if !ok {
			continue
		}
		restore := c.enter(fn)
		if fn.Extern {
			c.checkExtern(fn)
		} else {
			c.checkBody(fn)
		}
		restore()
	}

	return c.diags
}

// checkGlobals records which of decls are package-level variables, then
// resolves every struct's fields, then checks each variable, so that a
// global's initializer may write a literal of any struct.
func (c *Checker) checkGlobals(decls []ast.Decl) {
	for _, decl := range decls {
		if v, ok := decl.(*ast.Var); ok {
			c.globals[v] = true
		}
	}
	c.checkStructs()
	for _, decl := range decls {
		if v, ok := decl.(*ast.Var); ok {
			c.ensureGlobal(v, v.NameSpan)
		}
	}
}

// checkBody checks the body of fn, and that it ends in a return if fn
// declares a result.
func (c *Checker) checkBody(fn *ast.Func) {
	c.checkBlock(fn.Body, fn)

	// The rule is syntactic, as it is in Go: a call that never comes back,
	// like exit, does not satisfy it.
	if fn.Results != nil && !blockTerminates(fn.Body) {
		c.errorf(fn.NameSpan, "%s must end in a return", fn.Name)
	}
}

// checkStmt checks a statement of fn and everything nested in it.
func (c *Checker) checkStmt(stmt ast.Stmt, fn *ast.Func) {
	switch s := stmt.(type) {
	case *ast.Return:
		c.checkReturn(s, fn)

	case *ast.If:
		c.checkCond(s.Cond)
		then := &guard{cond: s.Cond, span: s.Cond.Spanned(), kind: guardCond}
		c.checkGuarded(s.Then, then, c.factsFrom(s.Cond, true), fn)
		els := &guard{cond: s.Cond, span: s.Cond.Spanned(), kind: guardElse}
		c.checkGuarded(s.Else, els, c.factsFrom(s.Cond, false), fn)

	case *ast.For:
		c.checkFor(s, fn)

	case *ast.ForRange:
		c.checkForRange(s, fn)

	case *ast.Switch:
		c.checkSwitch(s, fn)

	case *ast.Defer:
		c.checkDefer(s, fn)

	case *ast.Break:
		c.checkInLoop(s.Span, "break")

	case *ast.Continue:
		c.checkInLoop(s.Span, "continue")

	default:
		c.checkSimpleStmt(stmt)
	}
}

// checkSimpleStmt checks a statement that holds no other statement: a
// declaration, an assignment, an increment or decrement, or an expression.
func (c *Checker) checkSimpleStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		c.discard(s.X)
	case *ast.Var:
		c.checkVar(s)
	case *ast.Assign:
		c.checkAssign(s)
	case *ast.IncDec:
		c.checkIntTarget(s.Target, s.Op())
	case *ast.MultiVar:
		c.checkMultiVar(s)
	case *ast.MultiAssign:
		c.checkMultiAssign(s)
	}
}

// checkMultiVar checks a declaration of several variables. Its value must be
// a call to a function with exactly one result for each variable, or a
// conversion that can fail, which gives the converted value and whether it
// fit, and each variable takes the type of the value in its position. When
// the value is wrong every variable keeps types.Invalid, so its uses are not
// reported again.
func (c *Checker) checkMultiVar(m *ast.MultiVar) {
	b := binding{op: ":="}
	if len(m.Vars) == 2 {
		b.first, b.second = m.Vars[0].Name, m.Vars[1].Name
	}
	results, ok := c.multiResults(m.Value, len(m.Vars), m.Span, b)
	if !ok {
		return
	}
	for i, v := range m.Vars {
		v.Type = c.settleResult(m.Value, results[i], types.Invalid)
	}
}

// checkMultiAssign checks an assignment of several values, given as a
// MultiVar's are. Each target must be a mutable variable, named only once,
// whose type the value in its position widens to. When the value is wrong,
// the targets are still checked for being assignable.
func (c *Checker) checkMultiAssign(m *ast.MultiAssign) {
	b := binding{op: "="}
	if len(m.Targets) == 2 {
		b.first, b.second = c.text(m.Targets[0].Spanned()), c.text(m.Targets[1].Spanned())
	}
	results, ok := c.multiResults(m.Value, len(m.Targets), m.Span, b)
	seen := make(map[ast.Decl]bool, len(m.Targets))
	for i, target := range m.Targets {
		got := types.Invalid
		if ok {
			got = results[i]
		}
		c.checkMultiTarget(m.Value, target, got, seen)
	}
}

// checkMultiTarget checks one target of a MultiAssign whose value, value,
// gives got in the target's position, Invalid when the value is wrong.
// seen holds the variables already named, each of which may be named only
// once.
func (c *Checker) checkMultiTarget(value, target ast.Expr, got types.Type, seen map[ast.Decl]bool) {
	// _ discards its value, whatever its type, and may appear more than
	// once.
	if ast.IsBlank(target) {
		c.settleResult(value, got, types.Invalid)
		return
	}
	if name, isName := target.(*ast.Ident); isName {
		if seen[name.Def] {
			c.errorf(name.Span, "%s is assigned more than once", name.Name)
			return
		}
		seen[name.Def] = true
	}

	typ, assignable := c.assignable(target)
	got = c.settleResult(value, got, typ)
	if !assignable || typ == types.Invalid || got == types.Invalid || types.Widens(got, typ) {
		return
	}
	c.errorf(
		target.Spanned(),
		"cannot use %s as %s in assignment to %s",
		c.typeName(got),
		c.typeName(typ),
		targetName(target),
	)
}

// multiResults checks the value of a MultiVar or MultiAssign, which binds
// want names as b says, and returns the type of each value it gives, in
// order, and whether it gives exactly want of them. It must be a call to a
// function with that many results, or a division, an index or a
// conversion, which gives two.
// Any other value is reported as one error covering span, the whole
// statement, unless it binds two names and holds a division, a remainder
// or an index, when it is the checked form misused; see notWhole.
func (c *Checker) multiResults(
	value ast.Expr,
	want int,
	span ast.Span,
	b binding,
) ([]types.Type, bool) {
	if bin, ok := value.(*ast.Binary); ok && (bin.Op == ast.Div || bin.Op == ast.Rem) {
		return c.divisionResults(bin, want, span)
	}
	if ix, ok := value.(*ast.Index); ok {
		return c.indexResults(ix, want, span)
	}
	call, isCall := value.(*ast.Call)
	if !isCall {
		if op := fallible(value); want == 2 && op != nil {
			c.notWhole(value, op, b)
			return nil, false
		}
		c.expr(value)
		c.errorf(span, "assignment mismatch: %d variables but 1 value", want)
		return nil, false
	}
	if call.Conv != types.Invalid {
		return c.conversionResults(call, want, span)
	}

	return c.callResults(call, want, span)
}

// binding is how a MultiVar or MultiAssign binds its value: op, := or =,
// and, when it binds two names, the first and second as written.
type binding struct {
	first, second, op string
}

// fallible returns the first division, remainder or index in value, in
// source order, outside any call, or nil when there is none.
func fallible(value ast.Expr) ast.Expr {
	var found ast.Expr
	ast.Walk(value, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		switch x := n.(type) {
		case *ast.Call:
			return false
		case *ast.Binary:
			if x.Op == ast.Div || x.Op == ast.Rem {
				found = x
			}
		case *ast.Index:
			found = x
		}
		return found == nil
	})
	return found
}

// notWhole reports value, bound to two names as b says, which holds op, a
// division, a remainder or an index, but is more than it: the checked form
// must be the whole value. The error names the form with op alone, and
// what to compute from its first value, the value with op replaced by the
// first name, or, when that is _, only says to use it. value is then
// checked with op bound with ok, so op needs no proof, except that a
// divisor known to be zero is still reported.
func (c *Checker) notWhole(value, op ast.Expr, b binding) {
	whole, part := value.Spanned(), op.Spanned()
	rest := "then use its value"
	if b.first != "_" {
		text := c.text(whole)
		rest = "then compute " + text[:part.Start-whole.Start] + b.first + text[part.End-whole.Start:]
	}
	c.errorf(whole, "the checked form must be the whole value: %s, %s %s %s, %s",
		b.first, b.second, b.op, c.text(part), rest)

	switch x := op.(type) {
	case *ast.Binary:
		if v, known := c.knownValue(x.Y); !known || !v.IsZero() {
			c.checkedDivision = x
		}
	case *ast.Index:
		c.checkedIndex = x
	}
	c.expr(value)
	c.checkedDivision, c.checkedIndex = nil, nil
}

// settleResult returns the type of the value in one position of a MultiVar
// or MultiAssign, got, once it has one. Only the quotient of a checked
// division of two untyped constants, value, lacks one: it settles to typ,
// the type of the variable it is assigned to, when that is an integer type,
// and to int64 otherwise, and is Invalid if it does not fit.
func (c *Checker) settleResult(value ast.Expr, got, typ types.Type) types.Type {
	if got != types.UntypedInt {
		return got
	}
	if types.IsInteger(typ) {
		if !c.settle(value, typ) {
			return types.Invalid
		}
		return typ
	}
	return c.valueOf(value, got)
}

// callResults checks a call bound to want names: its callee must declare
// exactly that many results.
func (c *Checker) callResults(call *ast.Call, want int, span ast.Span) ([]types.Type, bool) {
	c.checkCall(call)
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		return nil, false
	}

	// A builtin's Def is a nil *Func, and a builtin returns nothing.
	var results []types.Type
	if fn, ok := id.Def.(*ast.Func); ok && fn != nil {
		results = fn.ResultTypes
	}
	switch got := len(results); {
	case got == 0:
		c.errorf(
			span,
			"assignment mismatch: %d variables but %s returns no value",
			want,
			id.Qualified(),
		)
	case got != want:
		c.errorf(
			span,
			"assignment mismatch: %d variables but %s returns %d %s",
			want,
			id.Qualified(),
			got,
			plural(got, "value"),
		)
	default:
		return results, true
	}
	return nil, false
}

// conversionResults checks a conversion bound to want names, which must be
// two: the converted value and whether it fit.
func (c *Checker) conversionResults(call *ast.Call, want int, span ast.Span) ([]types.Type, bool) {
	if !c.checkedConversion(call) {
		return nil, false
	}
	if want != 2 {
		c.errorf(span, "assignment mismatch: %d variables but a conversion gives 2 values", want)
		return nil, false
	}
	return []types.Type{call.Conv, types.Bool}, true
}

// discard checks an expression whose value is not used: a call statement,
// or a value checked only for what is inside it. A call may return
// nothing, or several values, here.
func (c *Checker) discard(e ast.Expr) {
	if call, ok := e.(*ast.Call); ok && call.Conv == types.Invalid && !isLen(call) {
		c.checkCall(call)
		return
	}
	c.expr(e)
}

// plural returns word, with an s added unless n is one.
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// checkFor checks a loop's clauses, in source order, then its body, inside
// which break and continue are allowed. The body knows what the condition
// says, and, when the loop counts its variable, the bound it counts from;
// see counting.go.
func (c *Checker) checkFor(loop *ast.For, fn *ast.Func) {
	if loop.Init != nil {
		c.checkSimpleStmt(loop.Init)
	}
	if loop.Cond != nil {
		c.checkCond(loop.Cond)
	}
	if loop.Post != nil {
		c.checkSimpleStmt(loop.Post)
	}
	m := c.mark()
	if loop.Cond != nil {
		c.assumeLoop(loop)
	}
	c.loops++
	c.checkBlock(loop.Body, fn)
	c.loops--
	c.release(m)
}

// assumeLoop puts in force for a loop's body what its condition says and,
// when the loop counts, the bound it counts from. A loop that would count
// but for a step that could wrap gets that bound dropped, to explain a
// missing proof.
func (c *Checker) assumeLoop(loop *ast.For) {
	facts := c.factsFrom(loop.Cond, true)
	c.assume(&guard{cond: loop.Cond, span: loop.Cond.Spanned(), kind: guardCond}, facts, loop.Body)
	f, how := c.countingFact(loop, facts)
	if how == notCounting {
		return
	}
	if how == wraps {
		f.drop = wrapDrop(loop, f)
	}
	c.assume(&guard{span: loop.Init.Spanned(), kind: guardCount}, []fact{f}, loop.Body)
}

// checkForRange checks a range loop. It ranges over an integer, of any
// type, an untyped constant count being an int64, which gives the loop
// variable the count's type; or over an array or a string, which gives the
// index an int64 and the element the array's element type, or a string's
// byte, a uint8. An integer gives only one value, so an element variable
// over one is reported.
func (c *Checker) checkForRange(loop *ast.ForRange, fn *ast.Func) {
	got := c.valueOf(loop.X, c.expr(loop.X))
	index, elem := c.rangeTypes(loop, got)
	if loop.Var != nil {
		loop.Var.Type = index
	}
	if loop.Elem != nil {
		loop.Elem.Type = elem
	}
	header := ast.Span{File: loop.Span.File, Start: loop.Span.Start, End: loop.X.Spanned().End}
	c.loops++
	c.checkGuarded(loop.Body, &guard{span: header, kind: guardRange}, c.rangeFacts(loop, got), fn)
	c.loops--
}

// rangeTypes returns the types a range over a value of type got gives its
// index and its element, reporting a type that cannot be ranged over and
// an element variable over an integer. Either type is types.Invalid where
// there is none.
func (c *Checker) rangeTypes(loop *ast.ForRange, got types.Type) (index, elem types.Type) {
	switch {
	case got == types.Invalid:
		return types.Invalid, types.Invalid
	case types.IsInteger(got):
		if loop.Elem != nil {
			c.errorf(
				ast.Span{
					File:  loop.Var.Span.File,
					Start: loop.Var.Span.Start,
					End:   loop.X.Spanned().End,
				},
				"assignment mismatch: 2 variables but range over %s gives 1 value",
				c.typeName(got),
			)
		}
		return got, types.Invalid
	case c.table.IsArray(got):
		return types.Int64, c.table.Elem(got)
	case got == types.String:
		return types.Int64, types.Uint8
	default:
		c.errorf(loop.X.Spanned(), "cannot range over %s", c.typeName(got))
		return types.Invalid, types.Invalid
	}
}

// rangeFacts returns what a range loop's body knows about its index
// variable, a range over a value of type got: it is at least 0, and below
// the count or the length when that is known. A count's is known when it is
// a known value, an array's always, and a string's when it is a known
// string, a literal or an immutable variable holding one.
// When the count is len(s) of a string variable, or the loop ranges over a
// string variable s, the index is below len(s) instead, which assume keeps
// only while the body never assigns s; s then still holds the string the
// loop copied.
func (c *Checker) rangeFacts(loop *ast.ForRange, got types.Type) []fact {
	if loop.Var == nil || loop.Var.Name == ast.Blank {
		return nil
	}
	f := fact{decl: loop.Var, hasLo: true}
	one := constant.FromInt64(1)
	switch {
	case types.IsInteger(got):
		if n, ok := c.knownValue(loop.X); ok {
			f.hi, f.hasHi = n.Sub(one)
		}
		f.lenOf, _ = c.stringLen(loop.X)
	case c.table.IsArray(got):
		f.hi, f.hasHi = constant.FromInt64(c.table.Len(got)).Sub(one)
	case got == types.String:
		if n, ok := c.knownLen(loop.X); ok {
			f.hi, f.hasHi = constant.FromInt64(n).Sub(one)
		}
		f.lenOf, _ = c.stringVar(loop.X)
	}
	return []fact{f}
}

// checkBlock checks each statement of a block in fn. An if whose branch
// leaves the block gives the statements after it the facts of having got
// past it.
func (c *Checker) checkBlock(body []ast.Stmt, fn *ast.Func) {
	m := c.mark()
	for i, stmt := range body {
		c.checkStmt(stmt, fn)
		g, facts := c.afterFacts(stmt)
		if g == nil {
			// An if whose branch ends in exit gives nothing, but what it
			// would give is kept, dropped, to explain a missing proof.
			if g, facts = c.exitAfterFacts(stmt); len(facts) == 0 {
				g = nil
			}
		}
		if g != nil {
			c.assume(g, facts, body[i+1:])
		}
	}
	c.release(m)
}

// checkGuarded checks a block reached only when facts, which come from g,
// hold, assuming those the block cannot undo.
func (c *Checker) checkGuarded(body []ast.Stmt, g *guard, facts []fact, fn *ast.Func) {
	m := c.mark()
	c.assume(g, facts, body)
	c.checkBlock(body, fn)
	c.release(m)
}

// checkCond checks the condition of an if or a loop, which must be a bool.
func (c *Checker) checkCond(cond ast.Expr) {
	got := c.valueOf(cond, c.expr(cond))
	if got != types.Invalid && got != types.Bool {
		c.errorf(cond.Spanned(), "condition must be a bool, got %s", c.typeName(got))
	}
}

// checkInLoop reports a break or continue that no loop encloses, and one
// in deferred code whose loop is outside it.
func (c *Checker) checkInLoop(span ast.Span, keyword string) {
	switch {
	case c.loops == 0:
		c.errorf(span, "%s is not in a loop", keyword)
	case c.deferred > 0 && c.loops == c.deferLoops:
		c.errorf(span, "cannot %s out of a defer", keyword)
	}
}

// checkVar records a variable's type: its declared type if it has one,
// otherwise its value's, an untyped constant being an int64. A value must be
// usable as the declared type, and only a mutable variable may leave out its
// value and start from its zero value, since an immutable one could never
// hold anything else.
func (c *Checker) checkVar(v *ast.Var) {
	declared := types.Invalid
	if v.TypeExpr != nil {
		declared = c.declaredType(v.TypeExpr)
	}

	if v.Value == nil {
		if !v.Mutable {
			c.errorf(v.NameSpan, "immutable variable %s needs a value", v.Name)
		}
		v.Type = declared
		return
	}

	got := c.expr(v.Value)
	if v.TypeExpr == nil {
		v.Type = c.valueOf(v.Value, got)
		return
	}
	c.use(v.Value, got, declared, "in declaration of "+v.Name)
	v.Type = declared
}

// checkGlobal checks a package-level variable like a local, and also
// requires its initializer to be a constant expression, one the compiler
// can compute, which it evaluates into v.Const. An initializer that calls
// anything but len or a conversion, or names a mutable global, is
// reported before it is checked, since the functions it could call have
// no signatures yet. A global without a value takes its type's zero
// value.
func (c *Checker) checkGlobal(v *ast.Var) {
	if v.Value != nil && !c.isConstant(v.Value) {
		c.notConstant(v)
		if v.TypeExpr != nil {
			v.Type = c.declaredType(v.TypeExpr)
		}
		return
	}

	before := len(c.diags)
	c.checkVar(v)
	if len(c.diags) > before {
		return
	}

	if v.Value == nil {
		v.Const = zeroConst(v.Type)
		return
	}
	value, ok := c.fold(v.Value)
	if !ok {
		if !c.namesFailedGlobal(v.Value) {
			c.notConstant(v)
		}
		return
	}
	v.Const = value
}

// namesFailedGlobal reports whether e names an immutable global with no
// computed value, whose initializer has been reported, so that e having no
// value either is not reported again.
func (c *Checker) namesFailedGlobal(e ast.Expr) bool {
	found := false
	ast.Walk(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if g := c.constantGlobal(id); g != nil && g.Const == nil {
				found = true
			}
		}
		return !found
	})
	return found
}

// notConstant reports that the initializer of the global v is not a
// constant expression.
func (c *Checker) notConstant(v *ast.Var) {
	c.errorf(v.Value.Spanned(), "%s must be initialized with a constant expression", v.Name)
}

// isConstant reports whether e is built only from literals, names of
// immutable package-level variables, of its own package or an imported
// one, operators, array and struct literals, fields and elements, len and
// conversions, all of them of such parts, except that len may take
// anything that calls nothing, since an array's length is its type's,
// whatever holds it. A call to anything else, a function or a builtin, is
// never constant, since nothing runs before main to compute it, and a
// mutable global never is, since any function can change it. Whether the
// expression's value can be computed is found once it is checked, by fold.
func (c *Checker) isConstant(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.IntLit, *ast.CharLit, *ast.StrLit, *ast.BoolLit:
		return true
	case *ast.Ident:
		return c.constantGlobal(x) != nil
	case *ast.Call:
		if isLen(x) && len(x.Args) == 1 && !hasCall(x.Args[0]) {
			return true
		}
		return (x.Conv != types.Invalid || isLen(x)) && c.allConstant(x.Args)
	}
	parts, ok := subExprs(e)
	return ok && c.allConstant(parts)
}

// subExprs returns the operands of e when it is an operator, a literal of
// an array or a struct, one of its keyed elements, a field or an element,
// and false for anything else.
func subExprs(e ast.Expr) ([]ast.Expr, bool) {
	switch x := e.(type) {
	case *ast.KeyValue:
		return []ast.Expr{x.Value}, true
	case *ast.CompositeLit:
		return x.Elems, true
	case *ast.Binary:
		return []ast.Expr{x.X, x.Y}, true
	case *ast.Unary:
		return []ast.Expr{x.X}, true
	case *ast.Selector:
		return []ast.Expr{x.X}, true
	case *ast.Index:
		return []ast.Expr{x.X, x.Index}, true
	default:
		return nil, false
	}
}

// allConstant reports whether every one of elems is constant, as
// isConstant says.
func (c *Checker) allConstant(elems []ast.Expr) bool {
	for _, elem := range elems {
		if !c.isConstant(elem) {
			return false
		}
	}
	return true
}

// constantGlobal returns the variable id names when it is an immutable
// package-level variable, whose value is a constant, and nil otherwise.
func (c *Checker) constantGlobal(id *ast.Ident) *ast.Var {
	if v, ok := id.Def.(*ast.Var); ok && !v.Mutable && c.globals[v] {
		return v
	}
	return nil
}

// zeroConst is a type's zero value as a constant. An array's or a struct's
// is nil, since every element or field of it is zero, which the backend
// writes without listing them.
func zeroConst(typ types.Type) any {
	switch {
	case types.IsInteger(typ):
		return constant.Int{}
	case typ == types.String:
		return ""
	case typ == types.Bool:
		return false
	default:
		return nil
	}
}

// fold returns the value of a checked constant expression: a constant.Int,
// computed exactly as it was checked, a string, a bool, or an array's
// elements, or a struct's fields in declaration order, as a []any of
// those. A field or an element is computed from the struct or array it is
// part of (foldPart). A bool expression
// is evaluated in full, the right side of && and || included, as Go does,
// since any error on either side has already been reported. Arithmetic in
// a type of its own, as on character literals, gives a value of that type
// at every step, since checking the expression reported any step that does
// not, as it does in a function, and checkGlobal folds only an initializer
// that checked without errors.
func (c *Checker) fold(e ast.Expr) (any, bool) {
	if v, ok, isInt := c.foldInt(e); isInt {
		return v, ok
	}
	switch x := e.(type) {
	case *ast.StrLit, *ast.BoolLit, *ast.Ident:
		return c.foldLeaf(x)
	case *ast.CompositeLit:
		if c.table.IsStruct(x.Type) {
			return c.foldStruct(x)
		}
		return c.foldElems(x)
	case *ast.Binary:
		if b, ok := x.Const.(bool); ok {
			return b, true
		}
		return c.foldBoolBinary(x)
	case *ast.Unary:
		return c.foldNot(x)
	case *ast.Selector, *ast.Index:
		return c.foldPart(x)
	}
	return nil, false
}

// foldPart computes a field or an element whose struct, array or string is
// known: a literal of constants, the value of an immutable global, or a
// part of one. An element's index must be a known value in range, and a
// string's byte is a constant.Int. A part of anything else, a local's
// included, has no value here.
func (c *Checker) foldPart(e ast.Expr) (any, bool) {
	if sel, ok := e.(*ast.Selector); ok {
		base, ok := c.fold(sel.X)
		fields, isStruct := base.([]any)
		_, i, found := c.table.Field(ast.TypeOf(sel.X), sel.Name)
		if !ok || !isStruct || !found || i >= len(fields) {
			return nil, false
		}
		return fields[i], true
	}
	ix, ok := e.(*ast.Index)
	if !ok {
		return nil, false
	}
	i, ok := c.knownValue(ix.Index)
	if !ok || i.IsNegative() || !i.Fits(64, true) {
		return nil, false
	}
	return c.foldElement(ix.X, i.Int64())
}

// foldElement computes element i, not negative, of x, a known array or
// string: a string's byte as a constant.Int, and an array's element as
// fold gives it. An index past the end has no value.
func (c *Checker) foldElement(x ast.Expr, i int64) (any, bool) {
	if ast.TypeOf(x) == types.String {
		s, ok := c.knownString(x)
		if !ok || i >= int64(len(s)) {
			return nil, false
		}
		return constant.FromInt64(int64(s[i])), true
	}
	base, ok := c.fold(x)
	elems, isArray := base.([]any)
	if !ok || !isArray || i >= int64(len(elems)) {
		return nil, false
	}
	return elems[i], true
}

// foldLeaf computes e, a string or bool literal, or the name of an
// immutable global, which, checked before the initializer naming it, holds
// its computed value; a mutable one is never folded.
func (c *Checker) foldLeaf(e ast.Expr) (any, bool) {
	switch x := e.(type) {
	case *ast.StrLit:
		return x.Value, true
	case *ast.BoolLit:
		return x.Value, true
	case *ast.Ident:
		if v := c.constantGlobal(x); v != nil && v.Const != nil {
			return v.Const, true
		}
	}
	return nil, false
}

// foldInt computes e when it is an integer constant expression, untyped or
// typed, and reports whether it has a value and whether it is one. A typed
// one is a known value: a step that overflows its type was reported when
// e was checked, so e is never folded.
func (c *Checker) foldInt(e ast.Expr) (v constant.Int, ok, isInt bool) {
	if v, ok = constValue(e); ok {
		return v, true, true
	}
	typ := ast.TypeOf(e)
	if !types.IsInteger(typ) || typ == types.UntypedInt {
		return constant.Int{}, false, false
	}
	v, ok = c.knownValue(e)
	return v, ok, true
}

// foldNot computes ! on a bool constant.
func (c *Checker) foldNot(u *ast.Unary) (any, bool) {
	operand, ok := c.fold(u.X)
	if b, isBool := operand.(bool); ok && isBool && u.Op == ast.Not {
		return !b, true
	}
	return nil, false
}

// foldElems computes the elements of a constant array literal, as a []any.
func (c *Checker) foldElems(lit *ast.CompositeLit) (any, bool) {
	elems := make([]any, len(lit.Elems))
	for i, elem := range lit.Elems {
		v, ok := c.fold(elem)
		if !ok {
			return nil, false
		}
		elems[i] = v
	}
	return elems, true
}

// foldBoolBinary computes an operator that gives a bool, on two bool
// constants, or a comparison of two integer or two string ones, or ==
// and != on two bools, arrays or structs, folding both sides first;
// strings, alone or in an array or a struct, only in a package-level
// initializer. It records no Binary.Const.
func (c *Checker) foldBoolBinary(bin *ast.Binary) (any, bool) {
	left, okL := c.fold(bin.X)
	right, okR := c.fold(bin.Y)
	if !okL || !okR {
		return nil, false
	}
	switch {
	case bin.Op == ast.Eq || bin.Op == ast.Ne:
		equal, ok := c.equalConsts(left, right)
		return equal == (bin.Op == ast.Eq), ok
	case bin.Op.IsComparison():
		cmp, ok := c.compareConsts(left, right)
		return compare(bin.Op, cmp), ok
	}
	l, isBoolL := left.(bool)
	r, isBoolR := right.(bool)
	if !isBoolL || !isBoolR {
		return nil, false
	}
	return foldBools(bin.Op, l, r)
}

// compareConsts compares two integer or two string constants, giving -1, 0
// or 1, and reports whether it could. Strings compare by their bytes,
// unsigned, as strings.Compare does and as the backend does at run time.
// Only a global's initializer, checked while initStack holds it, compares
// them here: inside a function, as for a switch's case values, a
// comparison of strings is left to run time.
func (c *Checker) compareConsts(left, right any) (int, bool) {
	if a, isInt := left.(constant.Int); isInt {
		b, _ := right.(constant.Int)
		return a.Cmp(b), true
	}
	if a, isString := left.(string); isString && len(c.initStack) > 0 {
		b, _ := right.(string)
		return strings.Compare(a, b), true
	}
	return 0, false
}

// equalConsts reports whether two constants of one type are equal, and
// whether it could tell: two bools, two arrays or two structs, element by
// element or field by field, or two integers or strings as compareConsts
// compares them.
func (c *Checker) equalConsts(left, right any) (equal, ok bool) {
	switch a := left.(type) {
	case bool:
		b, isBool := right.(bool)
		return a == b, isBool
	case []any:
		b, isComposite := right.([]any)
		if !isComposite || len(a) != len(b) {
			return false, false
		}
		equal = true
		for i := range a {
			same, known := c.equalConsts(a[i], b[i])
			if !known {
				return false, false
			}
			equal = equal && same
		}
		return equal, true
	}
	cmp, ok := c.compareConsts(left, right)
	return cmp == 0, ok
}

// foldBools applies an operator to two bool constants.
func foldBools(op ast.BinaryOp, x, y bool) (any, bool) {
	switch op {
	case ast.Eq:
		return x == y, true
	case ast.Ne:
		return x != y, true
	case ast.And:
		return x && y, true
	case ast.Or:
		return x || y, true
	default:
		return nil, false
	}
}

// checkAssign requires the target to be a mutable variable and the value to
// be usable as the variable's type. Parameters are never assignable. A
// compound assignment also requires the target to be an integer, since it
// applies arithmetic.
func (c *Checker) checkAssign(a *ast.Assign) {
	got := c.expr(a.Value)
	if a.Op != ast.InvalidOp {
		c.checkCompound(a, got)
		return
	}

	if typ, ok := c.assignable(a.Target); ok {
		c.use(a.Value, got, typ, "in assignment to "+targetName(a.Target))
	}
}

// assignable checks that target can be assigned: a mutable variable, not a
// parameter, or an element of one, whose index must be in range, or a
// field of one. It returns the type target holds and whether it can be
// assigned.
func (c *Checker) assignable(target ast.Expr) (types.Type, bool) {
	switch t := target.(type) {
	case *ast.Ident:
		switch def := t.Def.(type) {
		case *ast.Param:
			c.errorf(t.Span, "cannot assign to parameter %s", t.Name)
		case *ast.Var:
			if !def.Mutable {
				c.errorf(t.Span, "cannot assign to immutable variable %s", t.Qualified())
				return types.Invalid, false
			}
			return def.Type, true
		}
		return types.Invalid, false
	case *ast.Index:
		return c.elementTarget(t)
	case *ast.Selector:
		return c.fieldTarget(t)
	default:
		c.errorf(target.Spanned(), "cannot assign to this expression")
		return types.Invalid, false
	}
}

// elementTarget checks an element being assigned, x[i]: x must be a
// mutable variable, or an element or field of one, since an array follows
// its variable, and the index must be in range. A string's bytes are never
// assigned. It returns the element's type.
func (c *Checker) elementTarget(ix *ast.Index) (types.Type, bool) {
	switch x := ix.X.(type) {
	case *ast.Ident:
		if ast.TypeOf(x) == types.String {
			return c.stringTarget(ix)
		}
	case *ast.Index, *ast.Selector:
	default:
		c.errorf(ix.X.Spanned(), "cannot assign to this expression")
		return types.Invalid, false
	}

	base, ok := c.containerTarget(ix.X, "element")
	if !ok {
		c.expr(ix.Index)
		return types.Invalid, false
	}
	if base == types.String {
		return c.stringTarget(ix)
	}
	ix.Type = c.element(ix, base, true)
	return ix.Type, ix.Type != types.Invalid
}

// containerTarget checks x, which holds an element or a field, what, being
// assigned: x must be a mutable variable, not a parameter, or itself an
// element or field of one. It returns x's type, and false when anything was
// reported or x's type is unknown.
func (c *Checker) containerTarget(x ast.Expr, what string) (types.Type, bool) {
	switch x := x.(type) {
	case *ast.Ident:
		switch def := x.Def.(type) {
		case *ast.Param:
			c.errorf(x.Span, "cannot assign to %s of parameter %s", what, x.Name)
		case *ast.Var:
			if def.Mutable {
				return def.Type, def.Type != types.Invalid
			}
			c.errorf(x.Span, "cannot assign to %s of immutable variable %s", what, x.Qualified())
		}
		return types.Invalid, false
	case *ast.Index:
		return c.elementTarget(x)
	case *ast.Selector:
		return c.fieldTarget(x)
	default:
		c.errorf(x.Spanned(), "cannot assign to this expression")
		return types.Invalid, false
	}
}

// stringTarget reports an assignment to a byte of a string, ix, which is
// never allowed, after checking the index for errors of its own.
func (c *Checker) stringTarget(ix *ast.Index) (types.Type, bool) {
	c.expr(ix.Index)
	c.errorf(ix.Span, "cannot assign to an element of a string; strings are immutable")
	return types.Invalid, false
}

// targetName names an assignment target in a message: the variable, an
// element of it, or a field of it.
func targetName(target ast.Expr) string {
	root := ast.Root(target)
	sel, isField := target.(*ast.Selector)
	switch {
	case isField && root == nil:
		return "field " + sel.Name
	case isField:
		return "field " + sel.Name + " of " + root.Name
	case root == nil:
		return "an element"
	}
	if _, isName := target.(*ast.Ident); isName {
		return root.Name
	}
	return "an element of " + root.Name
}

// checkCompound checks a compound assignment whose value has type got: the
// target must be an integer variable it can update, and the value usable
// as the target's type.
func (c *Checker) checkCompound(a *ast.Assign, got types.Type) {
	op := a.Op.String() + "="
	if typ, ok := c.checkIntTarget(a.Target, op); ok {
		usable := c.use(a.Value, got, typ, "in "+op)
		if usable && (a.Op == ast.Div || a.Op == ast.Rem) {
			c.requireNonzero(a.Value, false)
		}
	}
}

// checkIntTarget checks the target of an operator that updates an integer
// in place, such as += or ++: it must be a mutable variable of an integer
// type, or an integer element of one. It returns the target's type, and
// false when anything about it was reported, or its type is unknown, so the
// caller need not check further.
func (c *Checker) checkIntTarget(target ast.Expr, op string) (types.Type, bool) {
	typ, ok := c.assignable(target)
	if !ok || typ == types.Invalid {
		return types.Invalid, false
	}
	if !types.IsInteger(typ) {
		c.errorf(target.Spanned(), "operator %s is not defined on %s", op, c.typeName(typ))
		return types.Invalid, false
	}
	return typ, true
}

// declaredType returns the type a type expression denotes, reporting a type it
// does not know and returning types.Invalid for it. A name denotes a basic
// type, or the struct type the resolver bound it to.
func (c *Checker) declaredType(expr ast.Expr) types.Type {
	if a, ok := expr.(*ast.ArrayType); ok {
		return c.arrayType(a)
	}
	id, ok := expr.(*ast.Ident)
	if !ok {
		c.errorf(expr.Spanned(), "unsupported type expression: %T", expr)
		return types.Invalid
	}

	if td, ok := id.Def.(*ast.TypeDecl); ok {
		return td.Type
	}
	typ, known := types.Lookup(id.Name)
	if !known || id.Pkg != "" {
		c.errorf(id.Span, "unknown type: %s", id.Qualified())
		return types.Invalid
	}
	return typ
}

// terminates reports whether control cannot fall out of the bottom of stmt.
// An if only terminates when it has an else and both branches do, since
// otherwise the condition being false falls through, and a switch likewise
// only when it has a default and every case does. A loop only terminates
// when it has no condition and no break leaves it, following Go.
func terminates(stmt ast.Stmt) bool {
	switch s := stmt.(type) {
	case *ast.Return:
		return true
	case *ast.If:
		return len(s.Else) > 0 && blockTerminates(s.Then) && blockTerminates(s.Else)
	case *ast.For:
		return s.Cond == nil && !breaks(s.Body)
	case *ast.Switch:
		return switchTerminates(s)
	default:
		return false
	}
}

// breaks reports whether body holds a break that leaves the loop body
// belongs to: one directly in it or inside an if or a switch, which a
// break does not leave, but not one inside a nested loop, which leaves only
// that loop.
func breaks(body []ast.Stmt) bool {
	for _, stmt := range body {
		switch s := stmt.(type) {
		case *ast.Break:
			return true
		case *ast.If:
			if breaks(s.Then) || breaks(s.Else) {
				return true
			}
		case *ast.Switch:
			for _, cl := range s.Cases {
				if breaks(cl.Body) {
					return true
				}
			}
		}
	}
	return false
}

func blockTerminates(body []ast.Stmt) bool {
	return len(body) > 0 && terminates(body[len(body)-1])
}

// checkReturn checks a return of fn: see checkResults. A return in
// deferred code is an error of its own, since nothing may leave deferred
// code, and its values are then only checked for what is inside them.
func (c *Checker) checkReturn(ret *ast.Return, fn *ast.Func) {
	if c.deferred == 0 {
		c.checkResults(ret, fn)
		return
	}
	c.errorf(ret.Span, "cannot return from a defer")
	for _, v := range ret.Values {
		c.discard(v)
	}
}

// checkResults checks that a return carries one value for each result fn
// declares, each usable as that result's type. A wrong count is one error,
// and then no value is compared with a result.
func (c *Checker) checkResults(ret *ast.Return, fn *ast.Func) {
	got, want := len(ret.Values), len(fn.Results)
	if got == want {
		for i, v := range ret.Values {
			c.use(v, c.expr(v), fn.ResultTypes[i], "in return from "+fn.Name)
		}
		return
	}

	for _, v := range ret.Values {
		c.discard(v)
	}
	switch {
	case want == 0:
		c.errorf(spanOf(ret.Values), "%s declares no result, so it cannot return a value", fn.Name)
	case got == 0 && want == 1:
		c.errorf(
			ret.Span,
			"%s must return a value of type %s",
			fn.Name,
			c.typeName(fn.ResultTypes[0]),
		)
	case got < want:
		c.errorf(ret.Span, "not enough return values: %s returns %d, got %d", fn.Name, want, got)
	default:
		c.errorf(
			spanOf(ret.Values),
			"too many return values: %s returns %d, got %d",
			fn.Name,
			want,
			got,
		)
	}
}

// spanOf returns the source range from the start of the first expression in
// a list through the end of the last. The list must not be empty.
func spanOf(exprs []ast.Expr) ast.Span {
	span := exprs[0].Spanned()
	span.End = exprs[len(exprs)-1].Spanned().End
	return span
}

// resolveResultTypes records what each declared result type denotes. A
// result whose type is unknown keeps types.Invalid, as a parameter does.
func (c *Checker) resolveResultTypes(fn *ast.Func) {
	fn.ResultTypes = make([]types.Type, len(fn.Results))
	for i, result := range fn.Results {
		fn.ResultTypes[i] = c.declaredType(result)
	}
}

// resolveParamTypes records what each declared parameter type denotes. A
// parameter whose type is unknown keeps types.Invalid, which every later check
// reports rather than silently accepting.
func (c *Checker) resolveParamTypes(fn *ast.Func) {
	for _, param := range fn.Params {
		param.Type = c.declaredType(param.TypeExpr)
	}
}

func (c *Checker) errorf(span ast.Span, format string, args ...any) {
	c.errorNotes(span, nil, format, args...)
}

// errorNotes reports an error at span, with notes that explain it.
func (c *Checker) errorNotes(span ast.Span, notes []diag.Diagnostic, format string, args ...any) {
	c.diags = append(c.diags, diag.Diagnostic{
		Severity: diag.Error,
		Message:  fmt.Sprintf(format, args...),
		Span:     span,
		Related:  notes,
	})
}
