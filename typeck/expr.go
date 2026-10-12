package typeck

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/builtin"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/types"
)

// expr checks e, which is used as a value, and everything inside it, and
// returns its type, which it also records on e. An integer constant
// expression, made only of literals and operators, has types.UntypedInt
// and its exact value recorded, until settle gives it the type of the place
// it is used. types.Invalid means a problem in e has already been reported,
// so a caller never reports anything more about it.
func (c *Checker) expr(e ast.Expr) types.Type {
	switch x := e.(type) {
	case *ast.IntLit, *ast.StrLit, *ast.BoolLit:
		return literalType(x)
	case *ast.Ident:
		// The resolver reports a name bound to nothing, and the pipeline
		// stops there; a tree that skipped it is reported here instead of
		// passing as already reported.
		if x.Def == nil {
			c.errorf(x.Span, "undefined: %s", x.Name)
			return types.Invalid
		}
		return ast.TypeOf(x)
	case *ast.Call:
		return c.callValue(x)
	case *ast.Binary:
		x.Type = c.binary(x)
		return x.Type
	case *ast.Unary:
		x.Type = c.unary(x)
		return x.Type
	case *ast.CompositeLit:
		return c.compositeLit(x)
	case *ast.Index:
		return c.index(x)
	default:
		c.errorf(e.Spanned(), "unsupported expression: %T", e)
		return types.Invalid
	}
}

// literalType returns the type of a literal: a string's or a bool's, or an
// integer's, which is untyped until settled.
func literalType(e ast.Expr) types.Type {
	switch x := e.(type) {
	case *ast.IntLit:
		x.Type = types.UntypedInt
		return types.UntypedInt
	case *ast.StrLit:
		return types.String
	default:
		return types.Bool
	}
}

// callValue checks a call whose single value is used. A conversion gives
// the type it converts to. A call to a function that returns nothing, or
// several values, has no one value to give and is reported. A function
// whose one result type is unknown was reported where it is declared.
func (c *Checker) callValue(call *ast.Call) types.Type {
	if call.Conv != types.Invalid {
		return c.conversion(call)
	}
	if isLen(call) {
		return c.lenCall(call)
	}
	c.checkCall(call)

	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		return types.Invalid
	}
	if fn, ok := id.Def.(*ast.Func); ok && fn != nil && len(fn.ResultTypes) == 1 {
		return fn.ResultTypes[0]
	}
	c.errorf(call.Spanned(), "%s", notOneValue(id))
	return types.Invalid
}

// isLen reports whether call calls the builtin len, which has a nil
// declaration like every builtin.
func isLen(call *ast.Call) bool {
	id, ok := call.Fn.(*ast.Ident)
	if !ok || id.Name != "len" {
		return false
	}
	fn, ok := id.Def.(*ast.Func)
	return ok && fn == nil
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

// checkCall checks a call to a function or builtin: its argument count, and
// that each argument can be used as the parameter in its position. Its
// result, if any, is the caller's to use or discard.
func (c *Checker) checkCall(call *ast.Call) {
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		c.errorf(call.Spanned(), "cannot call %T", call.Fn)
		return
	}

	var params []types.Type
	if fn, isBuiltin := builtin.Lookup(id.Name); isBuiltin {
		params = fn.Params
	} else if fn, ok := c.funcs[id.Name]; ok {
		params = make([]types.Type, len(fn.Params))
		for i, param := range fn.Params {
			params[i] = param.Type
		}
	}

	if !c.checkArity(call, id.Name, len(params)) {
		for _, arg := range call.Args {
			c.expr(arg)
		}
		return
	}
	for i, arg := range call.Args {
		c.use(arg, c.expr(arg), params[i], "in argument to "+id.Name)
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

// use checks that e, of type got, can be used where a value of type want is
// expected, as described by context ("in argument to f"). An untyped
// constant takes want as its type if its value fits; a typed value must be
// of type want or widen to it without loss. An Invalid got or want has been
// reported already. It reports whether e is a usable value of want.
func (c *Checker) use(e ast.Expr, got, want types.Type, context string) bool {
	if got == types.Invalid || want == types.Invalid {
		return false
	}
	if got == types.UntypedInt {
		if types.IsInteger(want) {
			return c.settle(e, want)
		}
		if !c.settle(e, types.Int64) {
			return false
		}
		got = types.Int64
	}
	if !types.Widens(got, want) {
		c.errorf(
			e.Spanned(),
			"cannot use %s as %s %s",
			c.table.Name(got),
			c.table.Name(want),
			context,
		)
		return false
	}
	return true
}

// settle gives the untyped constant expression e the integer type typ, if
// its value is one of typ's, and reports whether it is. The type is only
// recorded on e itself: the backend writes out e's value, never its parts.
func (c *Checker) settle(e ast.Expr, typ types.Type) bool {
	value, _ := constValue(e)
	if !value.Fits(types.Bits(typ), !types.IsUnsigned(typ)) {
		c.errorf(e.Spanned(), "constant %s overflows %s", value, c.table.Name(typ))
		return false
	}
	switch x := e.(type) {
	case *ast.IntLit:
		x.Type = typ
	case *ast.Binary:
		x.Type = typ
	case *ast.Unary:
		x.Type = typ
	}
	return true
}

// valueOf gives e, of type got, a type of its own where nothing else does:
// an untyped constant becomes an int64, if its value fits. It returns the
// resulting type, Invalid if the value does not fit.
func (c *Checker) valueOf(e ast.Expr, got types.Type) types.Type {
	if got != types.UntypedInt {
		return got
	}
	if !c.settle(e, types.Int64) {
		return types.Invalid
	}
	return types.Int64
}

// constValue returns the value of an integer constant expression, and
// whether e is one.
func constValue(e ast.Expr) (constant.Int, bool) {
	switch x := e.(type) {
	case *ast.IntLit:
		return x.Value, true
	case *ast.Binary:
		v, ok := x.Const.(constant.Int)
		return v, ok
	case *ast.Unary:
		v, ok := x.Const.(constant.Int)
		return v, ok
	case *ast.Call:
		v, ok := x.Const.(constant.Int)
		return v, ok
	default:
		return constant.Int{}, false
	}
}

// binary checks an infix expression and returns its type: a bool for a
// comparison or a logical operator, and the operands' common type for
// arithmetic.
func (c *Checker) binary(bin *ast.Binary) types.Type {
	x := c.expr(bin.X)
	var y types.Type
	if bin.Op.IsLogical() {
		// The right side runs only when the left did not decide the
		// result: after a true left side for &&, a false one for ||.
		assumed := c.assume(c.factsFrom(bin.X, bin.Op == ast.And), nil)
		y = c.expr(bin.Y)
		c.forget(assumed)
	} else {
		y = c.expr(bin.Y)
	}

	switch {
	case bin.Op.IsLogical():
		okX := c.require(bin.X, x, bin.Op.String(), types.Bool)
		okY := c.require(bin.Y, y, bin.Op.String(), types.Bool)
		if okX && okY {
			return types.Bool
		}
		return types.Invalid
	case bin.Op.IsComparison():
		c.comparison(bin, x, y)
		return types.Bool
	default:
		return c.arithmetic(bin, x, y)
	}
}

// require reports operand, of type got, unless it is of type want, as an
// operand the operator op is not defined on. An untyped constant is named
// as the int64 it would otherwise become.
func (c *Checker) require(operand ast.Expr, got types.Type, op string, want types.Type) bool {
	if got == types.Invalid {
		return false
	}
	if got == want {
		return true
	}
	if got = c.valueOf(operand, got); got != types.Invalid {
		c.errorf(operand.Spanned(), "operator %s is not defined on %s", op, c.table.Name(got))
	}
	return false
}

// arithmetic checks + - * / or % on operands of types x and y, which must be
// integers, and returns the type of the result. Two untyped constants give
// an untyped constant, computed exactly; an untyped constant and a typed
// operand give the typed one's type, which the constant must fit; and two
// typed operands give whichever of the two the other widens to.
func (c *Checker) arithmetic(bin *ast.Binary, x, y types.Type) types.Type {
	if !c.integerOperands(bin, x, y) {
		return types.Invalid
	}

	if x == types.UntypedInt && y == types.UntypedInt {
		return c.foldArithmetic(bin)
	}
	typ := c.typedArithmetic(bin, x, y)
	if typ != types.Invalid && (bin.Op == ast.Div || bin.Op == ast.Rem) &&
		c.checkedDivision != bin {
		c.requireNonzero(bin.Y, true)
	}
	return typ
}

// typedArithmetic returns the type arithmetic on operands of types x and y
// works in, when at least one is typed: an untyped constant takes the other
// operand's type, which it must fit, and two typed operands combine in the
// one the other widens to.
func (c *Checker) typedArithmetic(bin *ast.Binary, x, y types.Type) types.Type {
	switch {
	case x == types.UntypedInt:
		if !c.settle(bin.X, y) {
			return types.Invalid
		}
		return y
	case y == types.UntypedInt:
		if !c.settle(bin.Y, x) {
			return types.Invalid
		}
		return x
	}
	if common, ok := commonType(x, y); ok {
		return common
	}
	c.errorf(bin.Span, "mismatched types %s and %s in %s", c.table.Name(x), c.table.Name(y), bin.Op)
	return types.Invalid
}

// requireNonzero reports a divisor that is not proven non-zero where it is
// used: a constant zero, or anything else no fact covers. withOk says
// whether the division could be written q, ok := a / b instead, which a
// compound assignment cannot.
func (c *Checker) requireNonzero(divisor ast.Expr, withOk bool) {
	if v, ok := c.knownValue(divisor); ok {
		if v.IsZero() {
			c.errorf(divisor.Spanned(), "division by zero")
		}
		return
	}
	if c.nonzero(divisor) {
		return
	}

	id, isName := divisor.(*ast.Ident)
	switch {
	case isName && withOk:
		c.errorf(
			divisor.Spanned(),
			"divisor %s may be zero; check %s != 0 first, or use q, ok := ... / %s",
			id.Name,
			id.Name,
			id.Name,
		)
	case isName:
		c.errorf(divisor.Spanned(), "divisor %s may be zero; check %s != 0 first", id.Name, id.Name)
	case withOk:
		c.errorf(divisor.Spanned(), "divisor may be zero; use q, ok := ... / ...")
	default:
		c.errorf(divisor.Spanned(), "divisor may be zero")
	}
}

// divisionResults checks a division or remainder bound to want names, which
// must be two: the result, or 0 when the divisor is zero, and whether it was
// not. The form is for a divisor that may be zero, so one proven not to be
// is reported, as is a divisor that is the constant zero.
func (c *Checker) divisionResults(bin *ast.Binary, want int, span ast.Span) ([]types.Type, bool) {
	c.checkedDivision = bin
	typ := c.expr(bin)
	c.checkedDivision = nil
	if typ == types.Invalid {
		return nil, false
	}

	if v, ok := c.knownValue(bin.Y); ok && v.IsZero() {
		c.errorf(bin.Y.Spanned(), "division by zero")
		return nil, false
	}
	if c.nonzero(bin.Y) || typ == types.UntypedInt {
		c.errorf(bin.Span, "the divisor cannot be zero here; drop the ok")
		return nil, false
	}
	if want != 2 {
		c.errorf(span, "assignment mismatch: %d variables but a division gives 2 values", want)
		return nil, false
	}
	return []types.Type{typ, types.Bool}, true
}

// integerOperands reports each operand of bin that is not an integer, and
// returns whether both are integers. An Invalid operand is not reported
// again, but leaves bin without a type.
func (c *Checker) integerOperands(bin *ast.Binary, x, y types.Type) bool {
	okX := x == types.Invalid || c.requireInteger(bin.X, x, bin.Op.String())
	okY := y == types.Invalid || c.requireInteger(bin.Y, y, bin.Op.String())
	return x != types.Invalid && y != types.Invalid && okX && okY
}

// requireInteger reports operand, of type got, unless it is an integer, as
// an operand op is not defined on.
func (c *Checker) requireInteger(operand ast.Expr, got types.Type, op string) bool {
	if types.IsInteger(got) {
		return true
	}
	c.errorf(operand.Spanned(), "operator %s is not defined on %s", op, c.table.Name(got))
	return false
}

// commonType returns the type two typed operands are combined in: the one
// the other widens to, if either does.
func commonType(x, y types.Type) (types.Type, bool) {
	switch {
	case types.Widens(x, y):
		return y, true
	case types.Widens(y, x):
		return x, true
	default:
		return types.Invalid, false
	}
}

// foldArithmetic computes an arithmetic expression on two untyped constants
// exactly, recording the value on bin. A division by zero, or a result
// outside the range constants must stay in, is reported.
func (c *Checker) foldArithmetic(bin *ast.Binary) types.Type {
	a, _ := constValue(bin.X)
	b, _ := constValue(bin.Y)

	var (
		result constant.Int
		ok     bool
	)
	switch bin.Op {
	case ast.Add:
		result, ok = a.Add(b)
	case ast.Sub:
		result, ok = a.Sub(b)
	case ast.Mul:
		result, ok = a.Mul(b)
	default:
		if b.IsZero() {
			c.errorf(bin.Span, "division by zero in constant expression")
			return types.Invalid
		}
		if bin.Op == ast.Div {
			result, ok = a.Quo(b)
		} else {
			result, ok = a.Rem(b)
		}
	}
	if !ok {
		c.errorf(bin.Span, "constant expression is out of range")
		return types.Invalid
	}
	bin.Const = result
	return types.UntypedInt
}

// comparison checks a comparison of operands of types x and y. They must
// have one type, after an untyped constant takes the other's or two integer
// types widen to the wider, and that type must support the operator: an
// integer supports all six, a bool only == and !=, and a string none. A
// mismatch, or an unsupported operator, is one error covering the whole
// comparison. Two untyped constants are compared exactly, and the result
// recorded on bin.
func (c *Checker) comparison(bin *ast.Binary, x, y types.Type) {
	if x == types.Invalid || y == types.Invalid {
		return
	}
	if x == types.UntypedInt && y == types.UntypedInt {
		a, _ := constValue(bin.X)
		b, _ := constValue(bin.Y)
		bin.Const = compare(bin.Op, a.Cmp(b))
		return
	}

	x, y = c.untypedAs(bin.X, x, y), c.untypedAs(bin.Y, y, x)
	if x == types.Invalid || y == types.Invalid {
		return
	}
	common, ok := commonType(x, y)
	switch {
	case !ok:
		c.errorf(
			bin.Span,
			"mismatched types %s and %s in %s",
			c.table.Name(x),
			c.table.Name(y),
			bin.Op,
		)
	case !c.supports(common, bin.Op):
		c.errorf(bin.Span, "operator %s is not defined on %s", bin.Op, c.table.Name(common))
	}
}

// untypedAs settles operand, of type got, to the type other of the operand
// it is compared with, when got is untyped: to other itself if other is an
// integer type, and to int64 otherwise, so a mismatch names it as one. It
// returns the operand's type, Invalid if the constant does not fit.
func (c *Checker) untypedAs(operand ast.Expr, got, other types.Type) types.Type {
	if got != types.UntypedInt {
		return got
	}
	if types.IsInteger(other) && other != types.UntypedInt {
		if !c.settle(operand, other) {
			return types.Invalid
		}
		return other
	}
	return c.valueOf(operand, got)
}

// compare applies a comparison operator to the result of comparing two
// values, -1, 0 or 1.
func compare(op ast.BinaryOp, cmp int) bool {
	switch op {
	case ast.Eq:
		return cmp == 0
	case ast.Ne:
		return cmp != 0
	case ast.Lt:
		return cmp < 0
	case ast.Le:
		return cmp <= 0
	case ast.Gt:
		return cmp > 0
	default: // ast.Ge
		return cmp >= 0
	}
}

// supports reports whether values of type typ can be compared with op: an
// integer with all six, and a bool or an array of comparable elements with
// == and != only.
func (c *Checker) supports(typ types.Type, op ast.BinaryOp) bool {
	if types.IsInteger(typ) {
		return true
	}
	return (op == ast.Eq || op == ast.Ne) && c.comparable(typ)
}

// unary checks a prefix expression and returns its type. ! takes a bool. -
// takes a signed integer, and negating an untyped constant is computed
// exactly; it is not defined on unsigned types, where -x is almost always a
// mistake, and 0 - x says what wrapping is wanted.
func (c *Checker) unary(u *ast.Unary) types.Type {
	x := c.expr(u.X)
	if u.Op == ast.Not {
		if c.require(u.X, x, "!", types.Bool) {
			return types.Bool
		}
		return types.Invalid
	}

	switch {
	case x == types.Invalid:
		return types.Invalid
	case x == types.UntypedInt:
		value, _ := constValue(u.X)
		negated, ok := value.Neg()
		if !ok {
			c.errorf(u.Span, "constant expression is out of range")
			return types.Invalid
		}
		u.Const = negated
		return types.UntypedInt
	case types.IsInteger(x) && !types.IsUnsigned(x):
		return x
	default:
		c.errorf(u.X.Spanned(), "operator - is not defined on %s", c.table.Name(x))
		return types.Invalid
	}
}

// conversion checks a conversion whose single value is used, T(x), and
// returns T. It must convert an integer to an integer type, and be one that
// cannot fail: an untyped constant that fits T, or a value of a type that
// widens to T. Any other is reported, since it needs the two-value form
// v, ok := T(x).
func (c *Checker) conversion(call *ast.Call) types.Type {
	from, ok := c.conversionOperand(call)
	if !ok {
		return types.Invalid
	}
	to := call.Conv
	if from == types.UntypedInt {
		if !c.settle(call.Args[0], to) {
			return types.Invalid
		}
		return to
	}
	if !types.Widens(from, to) {
		c.errorf(
			call.Span,
			"converting %s to %s can fail; use v, ok := %s(...)",
			c.table.Name(from),
			c.table.Name(to),
			c.table.Name(to),
		)
	}
	return to
}

// conversionOperand checks what every conversion requires, whether its
// value is used alone or with ok: one argument, an integer type to convert
// to, and an integer to convert. It returns the argument's type, and false
// if anything was reported.
func (c *Checker) conversionOperand(call *ast.Call) (types.Type, bool) {
	to := call.Conv
	name := c.table.Name(to)
	if !c.checkArity(call, name, 1) {
		for _, arg := range call.Args {
			c.expr(arg)
		}
		return types.Invalid, false
	}

	from := c.expr(call.Args[0])
	if from == types.Invalid {
		return types.Invalid, false
	}
	if !types.IsInteger(to) || !types.IsInteger(from) {
		from = c.valueOf(call.Args[0], from)
		if from != types.Invalid {
			c.errorf(call.Span, "cannot convert %s to %s", c.table.Name(from), name)
		}
		return types.Invalid, false
	}
	return from, true
}

// checkedConversion checks a conversion in its two-value form,
// v, ok := T(x), which is for conversions that can fail. One that cannot,
// from an untyped constant or a type that widens to T, is reported, since
// it has only one correct spelling. It reports whether the conversion is
// valid in this form.
func (c *Checker) checkedConversion(call *ast.Call) bool {
	from, ok := c.conversionOperand(call)
	if !ok {
		return false
	}
	to := call.Conv
	switch {
	case from == types.UntypedInt:
		if c.settle(call.Args[0], to) {
			c.errorf(call.Span, "a constant needs no ok to convert; use %s(...)", c.table.Name(to))
		}
		return false
	case types.Widens(from, to):
		c.errorf(
			call.Span,
			"converting %s to %s cannot fail; use %s(...)",
			c.table.Name(from),
			c.table.Name(to),
			c.table.Name(to),
		)
		return false
	}
	return true
}
