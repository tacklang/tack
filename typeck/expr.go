package typeck

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
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
	case *ast.IntLit, *ast.CharLit, *ast.StrLit, *ast.BoolLit, *ast.NoErrorLit,
		*ast.CAllocatorLit:
		return literalType(x)
	case *ast.Ident:
		return c.ident(x)
	case *ast.Call:
		return c.callValue(x)
	case *ast.Binary:
		x.Type = c.binary(x)
		return x.Type
	case *ast.Unary:
		x.Type = c.unary(x)
		return x.Type
	default:
		return c.compositeExpr(e)
	}
}

// ident checks a name used as a value and returns its type: its parameter's
// or variable's. A package-level variable not checked yet, which a struct's
// field may need first, is checked now. A variable held by an error cannot
// be read before the error is checked (requireChecked).
func (c *Checker) ident(id *ast.Ident) types.Type {
	// The resolver reports a name bound to nothing, and the pipeline stops
	// there; a tree that skipped it is reported here instead of passing as
	// already reported.
	if id.Def == nil {
		c.errorf(id.Span, "undefined: %s", id.Qualified())
		return types.Invalid
	}
	if v, ok := id.Def.(*ast.Var); ok && c.globals[v] {
		c.ensureGlobal(v, id.Span)
	}
	c.requireChecked(id)
	return ast.TypeOf(id)
}

// compositeExpr checks a literal of an array or a struct, an element, a
// slice, a field or a type assertion, and returns its type, or reports a
// mut argument written where no mut parameter takes it, or a list type
// written where a value is.
func (c *Checker) compositeExpr(e ast.Expr) types.Type {
	switch x := e.(type) {
	case *ast.CompositeLit:
		return c.compositeLit(x)
	case *ast.Index:
		return c.index(x)
	case *ast.Slice:
		return c.slice(x)
	case *ast.Selector:
		return c.selector(x)
	case *ast.MutArg:
		return c.mutElsewhere(x)
	case *ast.ListType:
		return c.listTypeValue(x)
	case *ast.TypeAssert:
		return c.typeAssert(x)
	default:
		c.errorf(e.Spanned(), "unsupported expression: %T", e)
		return types.Invalid
	}
}

// literalType returns the type of a literal: a string's or a bool's, a
// character's, which is a uint8, noerror's, an error, c_allocator's, an
// Allocator, or an integer's, which is untyped until settled.
func literalType(e ast.Expr) types.Type {
	switch x := e.(type) {
	case *ast.IntLit:
		x.Type = types.UntypedInt
		return types.UntypedInt
	case *ast.CharLit:
		return types.Uint8
	case *ast.StrLit:
		return types.String
	case *ast.NoErrorLit:
		return types.Error
	case *ast.CAllocatorLit:
		return types.Allocator
	default:
		return types.Bool
	}
}

// callValue checks a call whose single value is used. A conversion gives
// the type it converts to, but for one to a view, []uint8(s), which only a
// view parameter may take (see viewArg). len, make and append give their
// values (see genericCall). A call to a function that returns nothing, or
// several values, has no one value to give and is reported. A function
// whose one result type is unknown was reported where it is declared.
func (c *Checker) callValue(call *ast.Call) types.Type {
	if isViewConversion(call) {
		return c.viewConversionElsewhere(call)
	}
	if call.Conv != types.Invalid {
		return c.conversion(call)
	}
	if generic := c.genericCall(call); generic != nil {
		return generic(call)
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
		return fmt.Sprintf("%s returns %d values", id.Qualified(), len(fn.Results))
	}
	return fmt.Sprintf("%s returns no value", id.Qualified())
}

// checkCall checks a call to a function or builtin: its argument count,
// that each argument can be passed to the parameter in its position (see
// checkArg), or to a variadic one (see checkVariadicCall), that no two of
// its mut arguments overlap (see checkExclusive), that none reads a list
// another one passes as mut (see checkListBorrows), and, for std/fmt's
// printf, a known format against the values (see checkPrintf). Its
// result, if any, is the caller's to use or discard.
func (c *Checker) checkCall(call *ast.Call) {
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		c.errorf(call.Spanned(), "cannot call %T", call.Fn)
		return
	}

	params := c.calleeParams(id)
	if isVariadic(params) {
		c.checkExclusive(c.checkVariadicCall(call, id.Qualified(), params))
		c.checkListBorrows(call)
		if fn, ok := id.Def.(*ast.Func); ok {
			c.checkPrintf(call, fn)
		}
		return
	}
	if !c.checkArity(call, id.Qualified(), len(params)) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return
	}
	var muts []*ast.MutArg
	for i, arg := range call.Args {
		if m := c.checkArg(arg, params[i], id.Qualified()); m != nil {
			muts = append(muts, m)
		}
	}
	c.checkExclusive(muts)
	c.checkListBorrows(call)
}

// checkArity reports a call to name, or a conversion to it, that is not
// variadic, passing the wrong number of arguments or a spread, and
// returns whether the count was right and there was none.
func (c *Checker) checkArity(call *ast.Call, name string, want int) bool {
	if c.spreadElsewhere(call, name) {
		return false
	}
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
			c.typeName(got),
			c.typeName(want),
			context,
		)
		return false
	}
	return true
}

// settle gives the untyped constant expression e the integer type typ, if
// its value is one of typ's, and reports whether it is. The type is only
// recorded on e itself: the backend writes out e's value, never its parts.
// A constant of a target-width type must be one of its values on every
// target, from 0 to types.PortableMax.
func (c *Checker) settle(e ast.Expr, typ types.Type) bool {
	value, _ := constValue(e)
	if !fitsPortably(value, typ) {
		c.errorf(
			e.Spanned(),
			"constant %s overflows %s%s",
			value,
			c.typeName(typ),
			widthNote(value, typ),
		)
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

// fitsPortably reports whether v is a value of the integer type typ on
// every target: for a target-width type, whether it lies between 0 and
// types.PortableMax.
func fitsPortably(v constant.Int, typ types.Type) bool {
	return v.Fits(types.PortableBits(typ), !types.IsUnsigned(typ))
}

// widthNote returns what an error about the value v overflowing the
// integer type typ adds to say why: for a target-width type, and a v too
// large for it rather than negative, that its width is unknown, so a
// constant of it must be at most types.PortableMax. It is empty for any
// other type or value.
func widthNote(v constant.Int, typ types.Type) string {
	if !types.TargetWidth(typ) || v.IsNegative() {
		return ""
	}
	return fmt.Sprintf(
		": its width is unknown, so a constant must be at most %d",
		uint64(types.PortableMax),
	)
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
// whether e is one. A character literal is one too, though of type uint8
// rather than untyped.
func constValue(e ast.Expr) (constant.Int, bool) {
	switch x := e.(type) {
	case *ast.IntLit:
		return x.Value, true
	case *ast.CharLit:
		return constant.FromInt64(int64(x.Value)), true
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
		m := c.mark()
		left := &guard{cond: bin.X, span: bin.X.Spanned(), kind: guardCond}
		c.assume(left, c.factsFrom(bin.X, bin.Op == ast.And), nil)
		y = c.expr(bin.Y)
		c.release(m)
	} else {
		y = c.expr(bin.Y)
	}

	switch {
	case bin.Op.IsLogical():
		if c.operandsDefined(bin, x, y, isBool) {
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
		c.errorf(operand.Spanned(), "operator %s is not defined on %s", op, c.typeName(got))
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
	if typ == types.Invalid {
		return types.Invalid
	}
	if (bin.Op == ast.Div || bin.Op == ast.Rem) && c.checkedDivision != bin {
		c.requireNonzero(bin.Y, true)
	}
	if c.knownOverflow(bin, typ) {
		return types.Invalid
	}
	return typ
}

// knownOverflow reports typed arithmetic whose operands are both known
// values but whose exact result is not a value of its type typ, as in
// 'a' * 3 in uint8, and returns whether it did. Such arithmetic is an error
// wherever it is written, so the program never wraps a value the compiler
// knew. A known zero divisor is not reported here, since requireNonzero
// reports it, and arithmetic on a value not known wraps at run time.
//
// A target-width type's values are known only up to types.PortableMax,
// since where it wraps above that depends on the target. A result past it
// is an error in a constant expression, which must stay a value of its
// type on every target at every step, as a literal must: one made of
// literals, conversions and operators alone (see isConstantExpr), and a
// package-level variable's initializer, checked while initStack holds it,
// which must be one. Anywhere else such a result is just not known, and
// left to run time. A result below 0 wraps on every target, so it is an
// error wherever it is written.
func (c *Checker) knownOverflow(bin *ast.Binary, typ types.Type) bool {
	a, okA := c.knownValue(bin.X)
	b, okB := c.knownValue(bin.Y)
	if !okA || !okB || (bin.Op == ast.Div || bin.Op == ast.Rem) && b.IsZero() {
		return false
	}
	if _, fits := knownStep(bin.Op, a, b, typ); fits {
		return false
	}
	note, overflows := c.targetOverflow(bin, typ, a, b)
	if !overflows {
		return false
	}
	c.errorf(bin.Span, "constant expression overflows %s%s", c.typeName(typ), note)
	return true
}

// targetOverflow says whether bin, arithmetic on the known values a and b
// in the type typ whose exact result is not a value of typ on every
// target, overflows, as knownOverflow describes, and what the error adds
// to say why. For a type other than a target-width one, it always
// overflows, and nothing is added.
func (c *Checker) targetOverflow(
	bin *ast.Binary,
	typ types.Type,
	a, b constant.Int,
) (string, bool) {
	exact, inRange := knownStep(bin.Op, a, b, types.UntypedInt)
	if !types.TargetWidth(typ) || !inRange {
		return "", true
	}
	if !exact.IsNegative() && !isConstantExpr(bin) && len(c.initStack) == 0 {
		return "", false
	}
	return widthNote(exact, typ), true
}

// isConstantExpr reports whether e is made of literals, conversions and
// operators alone, and len of an array or of a string literal, which are
// constants: it names no variable, so its value is the same wherever it is
// written.
func isConstantExpr(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.IntLit, *ast.CharLit:
		return true
	case *ast.Binary:
		return isConstantExpr(x.X) && isConstantExpr(x.Y)
	case *ast.Unary:
		return isConstantExpr(x.X)
	case *ast.Call:
		if x.Conv != types.Invalid {
			return len(x.Args) == 1 && isConstantExpr(x.Args[0])
		}
		return x.Const != nil
	default:
		return false
	}
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
	c.errorf(bin.Span, "mismatched types %s and %s in %s", c.typeName(x), c.typeName(y), bin.Op)
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
	msg, notes := c.divisorError(divisor)
	if withOk {
		msg += orUse("q, ok := ... / "+c.operandName(divisor), strings.HasSuffix(msg, " first"))
	}
	c.errorNotes(divisor.Spanned(), notes, "%s", msg)
}

// divisorError returns the error for a divisor no fact proves not zero,
// before any mention of the checked form, and the notes that explain it.
// A subject, a variable or a path below an immutable one, gets the check
// that would prove it, except a mutable global, which no check proves.
func (c *Checker) divisorError(divisor ast.Expr) (string, []diag.Diagnostic) {
	if v, ok := c.mutableGlobal(divisor); ok {
		return "divisor " + c.operandName(divisor) + " may be zero", globalNote(v)
	}
	if c.isSubject(divisor) {
		name := c.nameOf(divisor)
		return "divisor " + name + " may be zero; check " + name + " != 0 first",
			c.explain(c.divisorProof(divisor))
	}
	return "divisor may be zero", c.operandNotes(divisor)
}

// operandName returns how an error names a divisor or an index: by its
// name when it is a subject, a variable or a path below an immutable one,
// and as ... otherwise.
func (c *Checker) operandName(e ast.Expr) string {
	if c.isSubject(e) {
		return c.nameOf(e)
	}
	return "..."
}

// orUse returns what an error adds to suggest the checked form, form:
// after the check it suggests, when afterCheck is set, or straight after
// the error otherwise.
func orUse(form string, afterCheck bool) string {
	if afterCheck {
		return ", or use " + form
	}
	return "; use " + form
}

// divisionResults checks a division or remainder bound to want names, which
// must be two: the result, or 0 when the divisor is zero, and whether it was
// not. The form needs no proof, and is allowed where one exists, when ok is
// always true; only a divisor known to be zero, which always fails, is
// reported. The quotient of two untyped constants is still untyped, and
// settles where it is bound (settleResult).
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
	if want != 2 {
		c.errorf(span, "assignment mismatch: %d variables but a division gives 2 values", want)
		return nil, false
	}
	return []types.Type{typ, types.Bool}, true
}

// integerOperands reports the operands of bin, of types x and y, unless
// both are integers, as operandsDefined does, and returns whether they are.
func (c *Checker) integerOperands(bin *ast.Binary, x, y types.Type) bool {
	return c.operandsDefined(bin, x, y, types.IsInteger)
}

// isBool reports whether typ is bool, the one type && || and ! are defined
// on.
func isBool(typ types.Type) bool {
	return typ == types.Bool
}

// operandsDefined reports whether bin's operator is defined on both of its
// operands, of types x and y, by defined. When it is not, the expression is
// reported once, at the first operand it is not defined on, however many
// are; an untyped constant is named as the int64 it would otherwise become.
// An Invalid operand has been reported already, so nothing built on it is
// reported again.
func (c *Checker) operandsDefined(
	bin *ast.Binary,
	x, y types.Type,
	defined func(types.Type) bool,
) bool {
	if x == types.Invalid || y == types.Invalid {
		return false
	}
	operand, got := bin.X, x
	switch {
	case defined(x) && defined(y):
		return true
	case defined(x):
		operand, got = bin.Y, y
	}
	if got = c.valueOf(operand, got); got != types.Invalid {
		c.errorf(operand.Spanned(), "operator %s is not defined on %s", bin.Op, c.typeName(got))
	}
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

// comparison checks a comparison of operands of types x and y. Two
// integers, of any types, compare by their values, with all six operators,
// once an untyped constant has taken the other operand's type, which it
// must fit: int8(-1) < uint64(0) holds. Any other operands must have one
// type, and that type must support the operator: a string supports all
// six, a bool, an array or a struct only == and !=. A mismatch, or an
// unsupported operator, is one error covering the whole comparison. Two
// untyped constants are compared exactly, and the result recorded on bin;
// two string literals are not folded, and are compared at run time.
func (c *Checker) comparison(bin *ast.Binary, x, y types.Type) {
	if x == types.Invalid || y == types.Invalid {
		return
	}
	if x == types.UntypedInt && y == types.UntypedInt {
		foldComparison(bin)
		return
	}

	x, y = c.untypedAs(bin.X, x, y), c.untypedAs(bin.Y, y, x)
	if x == types.Invalid || y == types.Invalid || types.IsInteger(x) && types.IsInteger(y) {
		return
	}
	c.comparisonOfOneType(bin, x, y)
}

// comparisonOfOneType checks a comparison of operands of types x and y,
// not both integers, which must have one type, after widening, that
// supports the operator, and reports a mismatch or an unsupported operator
// as one error covering the whole comparison.
func (c *Checker) comparisonOfOneType(bin *ast.Binary, x, y types.Type) {
	common, ok := commonType(x, y)
	switch {
	case !ok:
		c.errorf(
			bin.Span,
			"mismatched types %s and %s in %s",
			c.typeName(x),
			c.typeName(y),
			bin.Op,
		)
	case !c.supports(common, bin.Op):
		c.errorf(bin.Span, "operator %s is not defined on %s", bin.Op, c.typeName(common))
	}
}

// foldComparison compares bin's two untyped constant operands exactly, and
// records the result on bin.
func foldComparison(bin *ast.Binary) {
	a, _ := constValue(bin.X)
	b, _ := constValue(bin.Y)
	bin.Const = compare(bin.Op, a.Cmp(b))
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
// integer or a string with all six, and any other comparable type, a bool,
// an array or a struct, with == and != only.
func (c *Checker) supports(typ types.Type, op ast.BinaryOp) bool {
	if types.IsInteger(typ) || typ == types.String {
		return true
	}
	return (op == ast.Eq || op == ast.Ne) && c.comparable(typ)
}

// unary checks a prefix expression and returns its type. ! takes a bool. -
// takes a signed integer, and negating an untyped constant is computed
// exactly, as is negating a known value of a signed type, which must not
// overflow it; it is not defined on unsigned types, where -x is almost always a
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
		return c.knownNegation(u, x)
	default:
		c.errorf(u.X.Spanned(), "operator - is not defined on %s", c.typeName(x))
		return types.Invalid
	}
}

// knownNegation returns the type of -x for x of the signed type typ, and
// reports negating a known value whose negation is not one of typ's, the
// type's smallest value, as knownOverflow does arithmetic: it is an error
// wherever it is written, and the result is Invalid.
func (c *Checker) knownNegation(u *ast.Unary, typ types.Type) types.Type {
	v, known := c.knownValue(u.X)
	if !known {
		return typ
	}
	if _, fits := fitting(v.Neg())(typ); fits {
		return typ
	}
	c.errorf(u.Span, "constant expression overflows %s", c.typeName(typ))
	return types.Invalid
}

// conversion checks a conversion whose single value is used, T(x), and
// returns T. It must convert an integer to an integer type, and be one that
// cannot fail: an untyped constant that fits T, a value of a type that
// widens to T, or a known value, as knownValue computes it, that is a value
// of T on every target. A constant or known value that is not one of T's
// is reported as overflowing T, and any other conversion as needing the
// two-value form v, ok := T(x), a known value that is one of a
// target-width T's on some targets only among them.
func (c *Checker) conversion(call *ast.Call) types.Type {
	from, ok := c.conversionOperand(call)
	if !ok {
		return types.Invalid
	}
	to := call.Conv
	arg := call.Args[0]
	switch {
	case from == types.UntypedInt:
		if !c.settle(arg, to) {
			return types.Invalid
		}
	case types.Widens(from, to):
	default:
		v, known := c.knownValue(arg)
		if !known || fitsSomewhere(v, to) {
			c.errorf(
				call.Span,
				"converting %s to %s can fail; use v, ok := %s(...)",
				c.typeName(from),
				c.typeName(to),
				c.typeName(to),
			)
		} else if !c.knownFits(arg, v, to) {
			return types.Invalid
		}
	}
	return to
}

// knownFits reports whether v, the known value of e, is a value of the
// integer type typ on every target, and reports e as overflowing typ when
// it is not, as settle reports an untyped constant.
func (c *Checker) knownFits(e ast.Expr, v constant.Int, typ types.Type) bool {
	if fitsPortably(v, typ) {
		return true
	}
	c.errorf(e.Spanned(), "constant %s overflows %s%s", v, c.typeName(typ), widthNote(v, typ))
	return false
}

// fitsSomewhere reports whether v is a value of the target-width type typ
// on some targets but not on others: it lies past types.PortableMax, but
// within 64 bits. Converting a known value like that can fail, as
// converting an unknown one can, and is checked at run time.
func fitsSomewhere(v constant.Int, typ types.Type) bool {
	return types.TargetWidth(typ) && !fitsPortably(v, typ) && v.Fits(64, false)
}

// conversionOperand checks what every conversion requires, whether its
// value is used alone or with ok: one argument, an integer type to convert
// to, and an integer to convert. It returns the argument's type, and false
// if anything was reported.
func (c *Checker) conversionOperand(call *ast.Call) (types.Type, bool) {
	to := call.Conv
	name := c.typeName(to)
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
			c.errorf(call.Span, "cannot convert %s to %s", c.typeName(from), name)
		}
		return types.Invalid, false
	}
	return from, true
}

// checkedConversion checks a conversion in its two-value form,
// v, ok := T(x), which is for conversions that can fail, and is allowed for
// one that cannot, from an untyped constant, a known value that fits T, or
// a type that widens to T, when ok is always true. Only a constant or known
// value that does not fit, which always fails, is reported; a known value
// that fits a target-width T on some targets only is checked at run time.
// It reports whether the conversion is valid in this form.
func (c *Checker) checkedConversion(call *ast.Call) bool {
	from, ok := c.conversionOperand(call)
	if !ok {
		return false
	}
	to := call.Conv
	arg := call.Args[0]
	switch {
	case types.Widens(from, to):
		return true
	case from == types.UntypedInt:
		return c.settle(arg, to)
	}
	if v, known := c.knownValue(arg); known && !fitsSomewhere(v, to) {
		return c.knownFits(arg, v, to)
	}
	return true
}
