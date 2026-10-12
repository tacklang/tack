package typeck

import (
	"fmt"
	"math"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// Checker type-checks one file. It is single-use: create one with New and
// call Check once.
type Checker struct {
	file  *ast.File
	table *types.Table // the file's types, recorded on it by Check
	funcs map[string]*ast.Func
	diags []diag.Diagnostic
	loops int // how many loops enclose the statement being checked
}

// New returns a Checker for file, which must already have been resolved.
func New(file *ast.File) *Checker {
	return &Checker{file: file, table: types.NewTable(), funcs: make(map[string]*ast.Func)}
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
	for _, stmt := range fn.Body {
		c.checkStmt(stmt, fn)
	}

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
		c.checkBlock(s.Then, fn)
		c.checkBlock(s.Else, fn)

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
		c.checkExpr(s.X)
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
// a call to a function with exactly one result for each variable, and each
// variable takes the type of the result in its position. On a wrong count
// every variable keeps types.Invalid, so its uses are not reported again.
func (c *Checker) checkMultiVar(m *ast.MultiVar) {
	c.checkExpr(m.Value)

	results, ok := c.multiResults(m.Value, len(m.Vars), m.Span)
	if !ok {
		return
	}
	for i, v := range m.Vars {
		v.Type = results[i]
	}
}

// checkMultiAssign checks an assignment of several results. Its value must
// be a call with exactly one result for each target, and each target must be
// a mutable variable of its result's type, named only once. When the count
// is wrong, the targets are still checked for being assignable.
func (c *Checker) checkMultiAssign(m *ast.MultiAssign) {
	c.checkExpr(m.Value)

	results, ok := c.multiResults(m.Value, len(m.Targets), m.Span)
	seen := make(map[ast.Decl]bool, len(m.Targets))
	for i, target := range m.Targets {
		// _ discards its result, whatever its type, and may appear more
		// than once.
		if target.Name == ast.Blank {
			continue
		}
		if seen[target.Def] {
			c.errorf(target.Span, "%s is assigned more than once", target.Name)
			continue
		}
		seen[target.Def] = true

		got := types.Invalid
		if ok {
			got = results[i]
		}
		c.assignTo(target, got, target.Span)
	}
}

// multiResults returns the result types of value, which binds want
// variables, and whether it is a call with exactly that many results. It
// reports any other value as one error covering span, the whole statement.
func (c *Checker) multiResults(value ast.Expr, want int, span ast.Span) ([]types.Type, bool) {
	call, isCall := value.(*ast.Call)
	if !isCall {
		c.errorf(span, "assignment mismatch: %d variables but 1 value", want)
		return nil, false
	}
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
	c.loops++
	c.checkBlock(loop.Body, fn)
	c.loops--
}

// checkForRange checks a range loop: the count must be an int, and the loop
// variable, if any, is an int.
func (c *Checker) checkForRange(loop *ast.ForRange, fn *ast.Func) {
	if got := c.valueType(
		loop.Count,
	); got != types.Int && got != types.Invalid &&
		!alreadyReported(loop.Count, got) {
		c.errorf(loop.Count.Spanned(), "cannot range over %s", c.table.Name(got))
	}
	if loop.Var != nil {
		loop.Var.Type = types.Int
	}
	c.loops++
	c.checkBlock(loop.Body, fn)
	c.loops--
}

// checkBlock checks each statement of a block in fn.
func (c *Checker) checkBlock(body []ast.Stmt, fn *ast.Func) {
	for _, stmt := range body {
		c.checkStmt(stmt, fn)
	}
}

// checkCond checks the condition of an if or a loop, which must be a bool.
func (c *Checker) checkCond(cond ast.Expr) {
	c.checkExpr(cond)
	if got := ast.TypeOf(cond); got != types.Bool && !reportedInvalid(cond, got) {
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
// otherwise its value's. A declared type and a value must agree, and only a
// mutable variable may leave out its value and start from its zero value,
// since an immutable one could never hold anything else.
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

	got := c.valueType(v.Value)
	switch {
	case v.TypeExpr == nil:
		v.Type = got
	case got != types.Invalid && declared != types.Invalid && got != declared:
		c.errorf(
			v.Value.Spanned(),
			"cannot use %s as %s in declaration of %s",
			c.table.Name(got),
			c.table.Name(declared),
			v.Name,
		)
		v.Type = declared
	default:
		v.Type = declared
	}
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
	switch typ {
	case types.Int:
		return int64(0)
	case types.String:
		return ""
	case types.Bool:
		return false
	default:
		return nil
	}
}

// fold evaluates a constant expression that has already been type-checked.
// Every operand is evaluated, the right side of && and || included, so an
// error in one is reported even where a running program would skip it, as
// in Go. Arithmetic follows Go's integer arithmetic: division truncates toward zero and a
// remainder takes the sign of the dividend. A result that does not fit in an
// int, and a division or remainder by zero, is reported and reports false.
func (c *Checker) fold(e ast.Expr) (any, bool) {
	switch x := e.(type) {
	case *ast.IntLit:
		return x.Value, true
	case *ast.StrLit:
		return x.Value, true
	case *ast.BoolLit:
		return x.Value, true
	case *ast.Binary:
		left, ok := c.fold(x.X)
		if !ok {
			return nil, false
		}
		right, ok := c.fold(x.Y)
		if !ok {
			return nil, false
		}
		return c.foldBinary(x, left, right)
	case *ast.Unary:
		operand, ok := c.fold(x.X)
		if !ok {
			return nil, false
		}
		return c.foldUnary(x, operand)
	default:
		return nil, false
	}
}

// foldBinary applies an operator to two constants of the same type: two
// bools under == != && or ||, or two ints under an arithmetic or comparison
// operator, as the checker has already required. Operands of any other types
// fold to nothing rather than guess.
func (c *Checker) foldBinary(bin *ast.Binary, left, right any) (any, bool) {
	switch x := left.(type) {
	case bool:
		y, ok := right.(bool)
		if !ok {
			return nil, false
		}
		return foldBools(bin.Op, x, y)
	case int64:
		y, ok := right.(int64)
		if !ok {
			return nil, false
		}
		return c.foldInts(bin, x, y)
	default:
		return nil, false
	}
}

// foldUnary applies a prefix operator to a constant: ! to a bool, or - to an
// int, where negating the smallest int overflows. An operand of any other
// type folds to nothing.
func (c *Checker) foldUnary(u *ast.Unary, operand any) (any, bool) {
	switch x := operand.(type) {
	case bool:
		return !x, u.Op == ast.Not
	case int64:
		if u.Op != ast.Neg {
			return nil, false
		}
		if x == math.MinInt64 {
			c.errorf(u.Span, "constant expression overflows int")
			return nil, false
		}
		return -x, true
	default:
		return nil, false
	}
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

// foldInts applies an operator to two int constants.
func (c *Checker) foldInts(bin *ast.Binary, a, b int64) (any, bool) {
	if bin.Op.IsLogical() {
		return nil, false
	}
	if bin.Op.IsComparison() {
		return compareInts(bin.Op, a, b), true
	}

	if (bin.Op == ast.Div || bin.Op == ast.Rem) && b == 0 {
		c.errorf(bin.Span, "division by zero in constant expression")
		return nil, false
	}

	result, overflow := arith(bin.Op, a, b)
	if overflow {
		c.errorf(bin.Span, "constant expression overflows int")
		return nil, false
	}
	return result, true
}

// arith computes a op b for an arithmetic operator, and reports whether the
// true result does not fit in an int64. b is never zero for / and %.
func arith(op ast.BinaryOp, a, b int64) (int64, bool) {
	switch op {
	case ast.Add:
		sum := a + b
		// Overflow is only possible with operands of one sign, and shows as
		// a result of the other sign.
		return sum, (a >= 0) == (b >= 0) && (sum >= 0) != (a >= 0)
	case ast.Sub:
		diff := a - b
		return diff, (a >= 0) != (b >= 0) && (diff >= 0) != (a >= 0)
	case ast.Mul:
		return mul(a, b)
	case ast.Div:
		return a / b, a == math.MinInt64 && b == -1
	default: // ast.Rem; Go defines math.MinInt64 % -1 as 0, without overflow.
		if b == -1 {
			return 0, false
		}
		return a % b, false
	}
}

// mul computes a * b and reports whether the true product does not fit in
// an int64. Dividing the wrapped product back recovers a exactly when it did
// not wrap, except for -1 times the smallest int64, which wraps to itself.
func mul(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, false
	}
	if (a == -1 && b == math.MinInt64) || (b == -1 && a == math.MinInt64) {
		return math.MinInt64, true
	}
	product := a * b
	return product, product/b != a
}

// compareInts applies a comparison operator to two ints.
func compareInts(op ast.BinaryOp, a, b int64) bool {
	switch op {
	case ast.Eq:
		return a == b
	case ast.Ne:
		return a != b
	case ast.Lt:
		return a < b
	case ast.Le:
		return a <= b
	case ast.Gt:
		return a > b
	default: // ast.Ge
		return a >= b
	}
}

// checkAssign requires the target to be a mutable variable and the value to
// have the variable's type. Parameters are never assignable. A compound
// assignment also requires both to be ints, since it applies arithmetic.
func (c *Checker) checkAssign(a *ast.Assign) {
	got := c.valueType(a.Value)
	if a.Op != ast.InvalidOp {
		c.checkCompound(a, got)
		return
	}

	c.assignTo(a.Target, got, a.Value.Spanned())
}

// assignTo checks that a value of type got may be assigned to target: that
// target is a mutable variable, not a parameter, of that type. A wrong type
// is reported over at, and an Invalid got is taken as already reported.
func (c *Checker) assignTo(target *ast.Ident, got types.Type, at ast.Span) {
	switch def := target.Def.(type) {
	case *ast.Param:
		c.errorf(target.Span, "cannot assign to parameter %s", target.Name)
	case *ast.Var:
		if !def.Mutable {
			c.errorf(target.Span, "cannot assign to immutable variable %s", target.Name)
			return
		}
		if got != types.Invalid && got != def.Type {
			c.errorf(
				at,
				"cannot use %s as %s in assignment to %s",
				c.table.Name(got),
				c.table.Name(def.Type),
				target.Name,
			)
		}
	}
}

// checkCompound checks a compound assignment whose value has type got. The
// target and the value are each reported where they are not ints, unless
// the target cannot be assigned at all.
func (c *Checker) checkCompound(a *ast.Assign, got types.Type) {
	op := a.Op.String() + "="
	if !c.checkIntTarget(a.Target, op) {
		return
	}

	if got != types.Int && got != types.Invalid && !alreadyReported(a.Value, got) {
		c.errorf(a.Value.Spanned(), "cannot use %s as int in %s", c.table.Name(got), op)
	}
}

// checkIntTarget checks the target of an operator that updates an int in
// place, such as += or ++: it must be a mutable variable, and an int. It
// reports false when the target cannot be assigned at all, so the caller
// need not report anything more about the statement.
func (c *Checker) checkIntTarget(target *ast.Ident, op string) bool {
	switch def := target.Def.(type) {
	case *ast.Param:
		c.errorf(target.Span, "cannot assign to parameter %s", target.Name)
		return false
	case *ast.Var:
		if !def.Mutable {
			c.errorf(target.Span, "cannot assign to immutable variable %s", target.Name)
			return false
		}
		if def.Type != types.Int && def.Type != types.Invalid {
			c.errorf(target.Span, "cannot use %s as int in %s", c.table.Name(def.Type), op)
		}
	}
	return true
}

// valueType checks an expression used as a value and returns its type. A call
// to a function that returns nothing, or several values, is reported here,
// since it has no one value to give; the result is then types.Invalid.
func (c *Checker) valueType(e ast.Expr) types.Type {
	c.checkExpr(e)

	typ := ast.TypeOf(e)
	if typ == types.Invalid {
		if call, ok := e.(*ast.Call); ok {
			if id, ok := call.Fn.(*ast.Ident); ok {
				c.errorf(e.Spanned(), "%s", notOneValue(id))
			}
		}
	}
	return typ
}

// notOneValue says why a call to the function id names cannot be used as a
// value: it returns nothing, or more than one value. A builtin returns
// nothing.
func notOneValue(id *ast.Ident) string {
	if fn, ok := id.Def.(*ast.Func); ok && fn != nil && len(fn.Results) > 1 {
		return fmt.Sprintf("%s returns %d values", id.Name, len(fn.Results))
	}
	return fmt.Sprintf("%s returns no value", id.Name)
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
// declares, each of that result's type. A wrong count is one error, and then
// no value is compared with a result.
func (c *Checker) checkReturn(ret *ast.Return, fn *ast.Func) {
	for _, v := range ret.Values {
		c.checkExpr(v)
	}

	got, want := len(ret.Values), len(fn.Results)
	switch {
	case got == want:
		c.checkReturnTypes(ret, fn)
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

// checkReturnTypes checks each value of a return that has as many as fn
// declares results against the result in the same position.
func (c *Checker) checkReturnTypes(ret *ast.Return, fn *ast.Func) {
	for i, v := range ret.Values {
		// A result type that is unknown has been reported already, and there
		// is nothing to compare the value with.
		want := fn.ResultTypes[i]
		got := ast.TypeOf(v)
		if want == types.Invalid || got == want || reportedInvalid(v, got) {
			continue
		}

		if call, ok := v.(*ast.Call); ok && got == types.Invalid {
			if id, ok := call.Fn.(*ast.Ident); ok {
				c.errorf(v.Spanned(), "%s", notOneValue(id))
				continue
			}
		}
		c.errorf(
			v.Spanned(),
			"cannot use %s as %s in return from %s",
			c.table.Name(got),
			c.table.Name(want),
			fn.Name,
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

// checkExpr checks an expression and everything nested inside it.
func (c *Checker) checkExpr(e ast.Expr) {
	switch x := e.(type) {
	case *ast.Call:
		c.checkCall(x)
		for _, arg := range x.Args {
			c.checkExpr(arg)
		}

	case *ast.Binary:
		c.checkExpr(x.X)
		c.checkExpr(x.Y)
		c.checkBinary(x)

	case *ast.Unary:
		c.checkExpr(x.X)
		want := types.Bool
		if x.Op == ast.Neg {
			want = types.Int
		}
		c.requireOperand(x.X, x.Op, want)
	}
}

// checkBinary requires int operands for every arithmetic operator, since
// arithmetic on strings would need concatenation, which does not exist, and
// bool operands for && and ||. Comparisons are checked by checkComparison.
func (c *Checker) checkBinary(bin *ast.Binary) {
	switch {
	case bin.Op.IsComparison():
		c.checkComparison(bin)
	case bin.Op.IsLogical():
		c.requireOperand(bin.X, bin.Op, types.Bool)
		c.requireOperand(bin.Y, bin.Op, types.Bool)
	default:
		c.requireInts(bin)
	}
}

// requireOperand reports an operand of a logical operator or a negation that
// is not of type want, covering only that operand, unless its type is
// unreliable because of an error already reported.
func (c *Checker) requireOperand(operand ast.Expr, op fmt.Stringer, want types.Type) {
	if got := ast.TypeOf(operand); got != want && !alreadyReported(operand, got) {
		c.errorf(operand.Spanned(), "operator %s is not defined on %s", op, c.table.Name(got))
	}
}

// requireInts reports each operand of bin that is not an int, unless its
// type is unknown because of an error already reported.
func (c *Checker) requireInts(bin *ast.Binary) {
	for _, operand := range []ast.Expr{bin.X, bin.Y} {
		if got := ast.TypeOf(operand); got != types.Int && !reportedInvalid(operand, got) {
			c.errorf(operand.Spanned(), "cannot use %s as int in %s", c.table.Name(got), bin.Op)
		}
	}
}

// checkComparison requires both operands of a comparison to have one type,
// and that type to support the operator: an int supports all six, a bool
// only == and !=, and a string none, since comparing strings would need a
// runtime comparison. Either problem is one error covering the whole
// comparison. An operand with no type of its own falls back to requireInts,
// and one whose error is already reported is not reported again.
func (c *Checker) checkComparison(bin *ast.Binary) {
	x, y := ast.TypeOf(bin.X), ast.TypeOf(bin.Y)
	if alreadyReported(bin.X, x) || alreadyReported(bin.Y, y) {
		return
	}
	if x == types.Invalid || y == types.Invalid {
		c.requireInts(bin)
		return
	}

	switch {
	case x != y:
		c.errorf(
			bin.Span,
			"mismatched types %s and %s in %s",
			c.table.Name(x),
			c.table.Name(y),
			bin.Op,
		)
	case !supports(x, bin.Op):
		c.errorf(bin.Span, "operator %s is not defined on %s", bin.Op, c.table.Name(x))
	}
}

// supports reports whether values of type typ can be compared with op.
func supports(typ types.Type, op ast.BinaryOp) bool {
	switch typ {
	case types.Int:
		return true
	case types.Bool:
		return op == ast.Eq || op == ast.Ne
	default:
		return false
	}
}

// alreadyReported reports whether an operand's type is unreliable because of
// an error already reported: one of the cases reportedInvalid covers, or
// arithmetic or a negation on something other than ints, which was reported
// where it was checked and whose type is only its operand's.
func alreadyReported(e ast.Expr, typ types.Type) bool {
	if reportedInvalid(e, typ) {
		return true
	}
	switch x := e.(type) {
	case *ast.Binary:
		return x.Op.IsArithmetic() && (ast.TypeOf(x.X) != types.Int || ast.TypeOf(x.Y) != types.Int)
	case *ast.Unary:
		return x.Op == ast.Neg && ast.TypeOf(x.X) != types.Int
	default:
		return false
	}
}

func (c *Checker) checkCall(call *ast.Call) {
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		c.errorf(call.Spanned(), "cannot call %T", call.Fn)
		return
	}

	if fn, isBuiltin := builtin.Lookup(id.Name); isBuiltin {
		if c.checkArity(call, id.Name, len(fn.Params)) {
			c.checkArgTypes(call, id.Name, fn.Params)
		}
		return
	}

	fn, ok := c.funcs[id.Name]
	if !ok {
		// The resolver reports undefined names, and the pipeline stops there,
		// so reaching this means the name is defined as something not callable.
		return
	}

	params := make([]types.Type, len(fn.Params))
	for i, param := range fn.Params {
		params[i] = param.Type
	}

	if c.checkArity(call, id.Name, len(params)) {
		c.checkArgTypes(call, id.Name, params)
	}
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

// checkArity reports a call passing the wrong number of arguments, and returns
// whether the count was right.
func (c *Checker) checkArity(call *ast.Call, name string, want int) bool {
	if len(call.Args) == want {
		return true
	}

	c.errorf(call.Spanned(), "%s expects %d argument(s), got %d", name, want, len(call.Args))
	return false
}

func (c *Checker) checkArgTypes(call *ast.Call, name string, params []types.Type) {
	for i, arg := range call.Args {
		// A parameter of unknown type has been reported where it is declared,
		// and an argument whose type is unknown for the same reason has too.
		got := ast.TypeOf(arg)
		if params[i] == types.Invalid || reportedInvalid(arg, got) {
			continue
		}
		if got == types.Invalid {
			// The common case is calling a function that returns nothing and
			// using it where a value is expected, which deserves saying so.
			if nested, ok := arg.(*ast.Call); ok {
				if id, ok := nested.Fn.(*ast.Ident); ok {
					c.errorf(arg.Spanned(), "%s", notOneValue(id))
					continue
				}
			}
			c.errorf(arg.Spanned(), "unsupported expression in argument to %s: %T", name, arg)
			continue
		}
		if got != params[i] {
			c.errorf(
				arg.Spanned(),
				"cannot use %s as %s in argument to %s",
				c.table.Name(got),
				c.table.Name(params[i]),
				name,
			)
		}
	}
}

// reportedInvalid reports whether e has no type because of a problem already
// reported where it arose, so reporting it again where e is used would only
// repeat it: a name whose declared type is unknown, an operator applied to
// such a name, or a call to a function whose result type is unknown. A call
// to a function that returns nothing is not one of these; it is reported
// where the value is wanted.
func reportedInvalid(e ast.Expr, typ types.Type) bool {
	if typ != types.Invalid {
		return false
	}

	switch x := e.(type) {
	case *ast.Ident:
		// Only a declaration reports its own unknown type. A name bound to
		// nothing was never reported, so it is not covered here.
		switch x.Def.(type) {
		case *ast.Param, *ast.Var:
			return true
		}
		return false
	case *ast.Binary:
		return true
	case *ast.Call:
		if id, ok := x.Fn.(*ast.Ident); ok {
			fn, ok := id.Def.(*ast.Func)
			return ok && fn != nil && len(fn.Results) == 1
		}
	}
	return false
}

func (c *Checker) errorf(span ast.Span, format string, args ...any) {
	c.diags = append(c.diags, diag.Diagnostic{
		Severity: diag.Error,
		Message:  fmt.Sprintf(format, args...),
		Span:     span,
	})
}
