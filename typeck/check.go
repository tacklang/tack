package typeck

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// Checker type-checks one file. It is single-use: create one with New and
// call Check once.
type Checker struct {
	file  *ast.File
	table *types.Table // the file's types, recorded on it by Check

	// checkedDivision is the division whose value is being bound with ok,
	// which needs no proof that its divisor is not zero.
	checkedDivision *ast.Binary

	funcs   map[string]*ast.Func
	globals map[*ast.Var]bool // the package-level variables

	// facts counts, for each variable, the enclosing regions that prove it
	// is not zero; see facts.go.
	facts map[ast.Decl]int

	diags []diag.Diagnostic
	loops int // how many loops enclose the statement being checked
}

// New returns a Checker for file, which must already have been resolved.
func New(file *ast.File) *Checker {
	return &Checker{
		file:    file,
		table:   types.NewTable(),
		funcs:   make(map[string]*ast.Func),
		globals: make(map[*ast.Var]bool),
		facts:   make(map[ast.Decl]int),
	}
}

// Check reports every badly typed call in the file, in source order. Checking
// continues past the first problem, so one run reports all of them.
func (c *Checker) Check() []diag.Diagnostic {
	if c.file == nil {
		panic("file is nil")
	}
	c.file.Types = c.table

	// Signatures are resolved for every function before any body is checked,
	// so a call does not depend on its callee appearing first.
	for _, decl := range c.file.Decls {
		if fn, ok := decl.(*ast.Func); ok {
			c.funcs[fn.Name] = fn
			c.resolveParamTypes(fn)
			c.resolveResultTypes(fn)
		}
	}

	// Globals are checked before any body, so their types are known in every
	// function whatever the order of declarations.
	for _, decl := range c.file.Decls {
		if v, ok := decl.(*ast.Var); ok {
			c.globals[v] = true
		}
	}
	for _, decl := range c.file.Decls {
		if v, ok := decl.(*ast.Var); ok {
			c.checkGlobal(v)
		}
	}

	for _, decl := range c.file.Decls {
		if fn, ok := decl.(*ast.Func); ok {
			c.checkBody(fn)
		}
	}

	return c.diags
}

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
		c.checkGuarded(s.Then, c.factsFrom(s.Cond, true), fn)
		c.checkGuarded(s.Else, c.factsFrom(s.Cond, false), fn)

	case *ast.For:
		c.checkFor(s, fn)

	case *ast.ForRange:
		c.checkForRange(s, fn)

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
	results, ok := c.multiResults(m.Value, len(m.Vars), m.Span)
	if !ok {
		return
	}
	for i, v := range m.Vars {
		v.Type = results[i]
	}
}

// checkMultiAssign checks an assignment of several values, given as a
// MultiVar's are. Each target must be a mutable variable, named only once,
// whose type the value in its position widens to. When the value is wrong,
// the targets are still checked for being assignable.
func (c *Checker) checkMultiAssign(m *ast.MultiAssign) {
	results, ok := c.multiResults(m.Value, len(m.Targets), m.Span)
	seen := make(map[ast.Decl]bool, len(m.Targets))
	for i, target := range m.Targets {
		// _ discards its value, whatever its type, and may appear more
		// than once.
		if target.Name == ast.Blank {
			continue
		}
		if seen[target.Def] {
			c.errorf(target.Span, "%s is assigned more than once", target.Name)
			continue
		}
		seen[target.Def] = true

		typ, assignable := c.assignable(target)
		if !ok || !assignable || typ == types.Invalid || results[i] == types.Invalid ||
			types.Widens(results[i], typ) {
			continue
		}
		c.errorf(
			target.Span,
			"cannot use %s as %s in assignment to %s",
			c.table.Name(results[i]),
			c.table.Name(typ),
			target.Name,
		)
	}
}

// multiResults checks the value of a MultiVar or MultiAssign, which binds
// want names, and returns the type of each value it gives, in order, and
// whether it gives exactly want of them. It must be a call to a function
// with that many results, or a conversion that can fail, which gives two.
// Any other value is reported as one error covering span, the whole
// statement.
func (c *Checker) multiResults(value ast.Expr, want int, span ast.Span) ([]types.Type, bool) {
	if bin, ok := value.(*ast.Binary); ok && (bin.Op == ast.Div || bin.Op == ast.Rem) {
		return c.divisionResults(bin, want, span)
	}
	call, isCall := value.(*ast.Call)
	if !isCall {
		c.expr(value)
		c.errorf(span, "assignment mismatch: %d variables but 1 value", want)
		return nil, false
	}
	if call.Conv != types.Invalid {
		return c.conversionResults(call, want, span)
	}

	return c.callResults(call, want, span)
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
		c.errorf(span, "assignment mismatch: %d variables but %s returns no value", want, id.Name)
	case got != want:
		c.errorf(
			span,
			"assignment mismatch: %d variables but %s returns %d %s",
			want,
			id.Name,
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
	if call, ok := e.(*ast.Call); ok && call.Conv == types.Invalid {
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
// which break and continue are allowed.
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
	var facts []ast.Decl
	if loop.Cond != nil {
		facts = c.factsFrom(loop.Cond, true)
	}
	c.loops++
	c.checkGuarded(loop.Body, facts, fn)
	c.loops--
}

// checkForRange checks a range loop: the count must be an integer, of any
// type, an untyped constant count being an int64, and the loop variable, if
// any, takes the count's type.
func (c *Checker) checkForRange(loop *ast.ForRange, fn *ast.Func) {
	got := c.valueOf(loop.Count, c.expr(loop.Count))
	if got != types.Invalid && !types.IsInteger(got) {
		c.errorf(loop.Count.Spanned(), "cannot range over %s", c.table.Name(got))
		got = types.Invalid
	}
	if loop.Var != nil {
		loop.Var.Type = got
	}
	c.loops++
	c.checkBlock(loop.Body, fn)
	c.loops--
}

// checkBlock checks each statement of a block in fn. An if whose branch
// leaves the block gives the statements after it the facts of having got
// past it.
func (c *Checker) checkBlock(body []ast.Stmt, fn *ast.Func) {
	var assumed [][]ast.Decl
	for i, stmt := range body {
		c.checkStmt(stmt, fn)
		if facts := c.afterFacts(stmt); len(facts) > 0 {
			assumed = append(assumed, c.assume(facts, body[i+1:]))
		}
	}
	for _, facts := range assumed {
		c.forget(facts)
	}
}

// checkGuarded checks a block reached only when facts hold, assuming those
// the block cannot undo.
func (c *Checker) checkGuarded(body []ast.Stmt, facts []ast.Decl, fn *ast.Func) {
	assumed := c.assume(facts, body)
	c.checkBlock(body, fn)
	c.forget(assumed)
}

// checkCond checks the condition of an if or a loop, which must be a bool.
func (c *Checker) checkCond(cond ast.Expr) {
	got := c.valueOf(cond, c.expr(cond))
	if got != types.Invalid && got != types.Bool {
		c.errorf(cond.Spanned(), "condition must be a bool, got %s", c.table.Name(got))
	}
}

// checkInLoop reports a break or continue that no loop encloses.
func (c *Checker) checkInLoop(span ast.Span, keyword string) {
	if c.loops == 0 {
		c.errorf(span, "%s is not in a loop", keyword)
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
// requires its initializer to be a constant expression, which it evaluates
// into v.Const. A global without a value takes its type's zero value.
func (c *Checker) checkGlobal(v *ast.Var) {
	if v.Value != nil && !isConstant(v.Value) {
		c.errorf(v.Value.Spanned(), "%s must be initialized with a constant expression", v.Name)
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
	if value, ok := c.fold(v.Value); ok {
		v.Const = value
	}
}

// isConstant reports whether e is built only from literals and operators.
func isConstant(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.IntLit, *ast.StrLit, *ast.BoolLit:
		return true
	case *ast.Binary:
		return isConstant(x.X) && isConstant(x.Y)
	case *ast.Unary:
		return isConstant(x.X)
	default:
		return false
	}
}

// zeroConst is a type's zero value as a constant.
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
// computed exactly as it was checked, a string, or a bool. A bool expression
// is evaluated in full, the right side of && and || included, as Go does,
// since any error on either side has already been reported.
func (c *Checker) fold(e ast.Expr) (any, bool) {
	if v, ok := constValue(e); ok {
		return v, true
	}
	switch x := e.(type) {
	case *ast.StrLit:
		return x.Value, true
	case *ast.BoolLit:
		return x.Value, true
	case *ast.Binary:
		if b, ok := x.Const.(bool); ok {
			return b, true
		}
		return c.foldBoolBinary(x)
	case *ast.Unary:
		operand, ok := c.fold(x.X)
		if b, isBool := operand.(bool); ok && isBool && x.Op == ast.Not {
			return !b, true
		}
	}
	return nil, false
}

// foldBoolBinary computes an operator on two bool constants, folding both
// sides first.
func (c *Checker) foldBoolBinary(bin *ast.Binary) (any, bool) {
	left, okL := c.fold(bin.X)
	right, okR := c.fold(bin.Y)
	l, isBoolL := left.(bool)
	r, isBoolR := right.(bool)
	if !okL || !okR || !isBoolL || !isBoolR {
		return nil, false
	}
	return foldBools(bin.Op, l, r)
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
		c.use(a.Value, got, typ, "in assignment to "+a.Target.Name)
	}
}

// assignable checks that target can be assigned: a mutable variable, not a
// parameter. It returns the variable's type and whether it can.
func (c *Checker) assignable(target *ast.Ident) (types.Type, bool) {
	switch def := target.Def.(type) {
	case *ast.Param:
		c.errorf(target.Span, "cannot assign to parameter %s", target.Name)
	case *ast.Var:
		if !def.Mutable {
			c.errorf(target.Span, "cannot assign to immutable variable %s", target.Name)
			return types.Invalid, false
		}
		return def.Type, true
	}
	return types.Invalid, false
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
// type. It returns the target's type, and false when anything about it was
// reported, or its type is unknown, so the caller need not check further.
func (c *Checker) checkIntTarget(target *ast.Ident, op string) (types.Type, bool) {
	typ, ok := c.assignable(target)
	if !ok || typ == types.Invalid {
		return types.Invalid, false
	}
	if !types.IsInteger(typ) {
		c.errorf(target.Span, "operator %s is not defined on %s", op, c.table.Name(typ))
		return types.Invalid, false
	}
	return typ, true
}

// declaredType returns the type a type expression denotes, reporting a type it
// does not know and returning types.Invalid for it.
func (c *Checker) declaredType(expr ast.Expr) types.Type {
	id, ok := expr.(*ast.Ident)
	if !ok {
		c.errorf(expr.Spanned(), "unsupported type expression: %T", expr)
		return types.Invalid
	}

	typ, known := types.Lookup(id.Name)
	if !known {
		c.errorf(id.Span, "unknown type: %s", id.Name)
		return types.Invalid
	}
	return typ
}

// terminates reports whether control cannot fall out of the bottom of stmt.
// An if only terminates when it has an else and both branches do, since
// otherwise the condition being false falls through. A loop only terminates
// when it has no condition and no break leaves it, following Go.
func terminates(stmt ast.Stmt) bool {
	switch s := stmt.(type) {
	case *ast.Return:
		return true
	case *ast.If:
		return len(s.Else) > 0 && blockTerminates(s.Then) && blockTerminates(s.Else)
	case *ast.For:
		return s.Cond == nil && !breaks(s.Body)
	default:
		return false
	}
}

// breaks reports whether body holds a break that leaves the loop body
// belongs to: one directly in it or inside an if, but not one inside a
// nested loop, which leaves only that loop.
func breaks(body []ast.Stmt) bool {
	for _, stmt := range body {
		switch s := stmt.(type) {
		case *ast.Break:
			return true
		case *ast.If:
			if breaks(s.Then) || breaks(s.Else) {
				return true
			}
		}
	}
	return false
}

func blockTerminates(body []ast.Stmt) bool {
	return len(body) > 0 && terminates(body[len(body)-1])
}

// checkReturn checks that a return carries one value for each result fn
// declares, each usable as that result's type. A wrong count is one error,
// and then no value is compared with a result.
func (c *Checker) checkReturn(ret *ast.Return, fn *ast.Func) {
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
			c.table.Name(fn.ResultTypes[0]),
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
		id, ok := result.(*ast.Ident)
		if !ok {
			c.errorf(result.Spanned(), "unsupported type expression: %T", result)
			continue
		}

		typ, known := types.Lookup(id.Name)
		if !known {
			c.errorf(id.Span, "unknown type: %s", id.Name)
			continue
		}
		fn.ResultTypes[i] = typ
	}
}

// resolveParamTypes records what each declared parameter type denotes. A
// parameter whose type is unknown keeps types.Invalid, which every later check
// reports rather than silently accepting.
func (c *Checker) resolveParamTypes(fn *ast.Func) {
	for _, param := range fn.Params {
		id, ok := param.TypeExpr.(*ast.Ident)
		if !ok {
			c.errorf(param.TypeExpr.Spanned(), "unsupported type expression: %T", param.TypeExpr)
			continue
		}

		typ, known := types.Lookup(id.Name)
		if !known {
			c.errorf(id.Span, "unknown type: %s", id.Name)
			continue
		}
		param.Type = typ
	}
}

func (c *Checker) errorf(span ast.Span, format string, args ...any) {
	c.diags = append(c.diags, diag.Diagnostic{
		Severity: diag.Error,
		Message:  fmt.Sprintf(format, args...),
		Span:     span,
	})
}
