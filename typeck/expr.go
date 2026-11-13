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
// so a caller never reports anything more about it. e is checked with
// c.at at its end (see exprAt).
func (c *Checker) expr(e ast.Expr) types.Type {
	outer := c.at
	c.at = e.Spanned().End
	typ := c.exprAt(e)
	c.at = outer
	return typ
}

// exprAt checks e, with c.at at its end, so that what checking e proves,
// once its operands are checked, is judged where e is evaluated, and
// returns its type.
func (c *Checker) exprAt(e ast.Expr) types.Type {
	switch x := e.(type) {
	case *ast.IntLit, *ast.FloatLit, *ast.CharLit, *ast.StrLit, *ast.BoolLit, *ast.NoErrorLit,
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
	case *ast.Question:
		return c.question(x)
	default:
		return c.compositeExpr(e)
	}
}

// ident checks a name used as a value and returns its type: its parameter's
// or variable's, or its function's, used as a value (see funcValue). A
// package-level variable not checked yet, which a struct's
// field may need first, is checked now. A variable held by an error cannot
// be read before the error is checked (requireChecked). A scope parameter
// is used only where scopeUse marks it, and so is a scope allocator, a
// region's or a scope Allocator parameter (see isScopeAlloc).
func (c *Checker) ident(id *ast.Ident) types.Type {
	// The resolver reports a name bound to nothing, and the pipeline stops
	// there; a tree that skipped it is reported here instead of passing as
	// already reported.
	if id.Def == nil {
		c.errorf(id.Span, "undefined: %s", id.Qualified())
		return types.Invalid
	}
	if c.unbound(id) {
		return types.Invalid
	}
	if c.fieldVar(id) != nil {
		return c.fieldVarValue(id)
	}
	if fn, ok := id.Def.(*ast.Func); ok {
		return c.funcValue(id, fn)
	}
	if v, ok := id.Def.(*ast.Var); ok && c.globals[v] {
		c.ensureGlobal(v, id.Span)
	}
	if c.isScopeParam(id) && c.scopeUse != id {
		c.scopeElsewhere(id)
		return types.Invalid
	}
	if c.isScopeAlloc(id) && c.scopeUse != id {
		c.scopeAllocElsewhere(id)
		return types.Invalid
	}
	c.requireChecked(id)
	switch def := id.Def.(type) {
	case *ast.Var:
		return c.narrowIdent(id, def.Type)
	case *ast.Param:
		return c.narrowIdent(id, def.Type)
	}
	return ast.TypeOf(id)
}

// compositeExpr checks a literal of an array or a struct, an element, a
// slice, a field or a type assertion, and returns its type, or reports a
// mut argument written where no mut parameter takes it, a list, a map, an
// arena, a pool, an id, a span or a handle type written where a value is,
// or a closure written anywhere but as an
// argument to a scope parameter, which scopeArg checks.
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
	case *ast.MutArg, *ast.OwnArg:
		return c.argElsewhere(x)
	case *ast.ListType:
		return c.listTypeValue(x)
	case *ast.MapType:
		return c.typeValue(x, c.mapType(x))
	case *ast.ArenaType:
		return c.typeValue(x, c.arenaType(x))
	case *ast.PoolType:
		return c.typeValue(x, c.poolType(x))
	case *ast.RefType:
		return c.typeValue(x, c.refType(x))
	case *ast.TypeAssert:
		return c.typeAssert(x)
	case *ast.FuncLit:
		return c.funcLitElsewhere(x)
	case *ast.FieldsOf:
		return c.fieldsElsewhere(x)
	default:
		c.errorf(e.Spanned(), "unsupported expression: %T", e)
		return types.Invalid
	}
}

// literalType returns the type of a literal: a string's or a bool's, a
// character's, which is a uint8, noerror's, an error, c_allocator's, an
// Allocator, or an integer's or a float's, which is untyped until settled.
func literalType(e ast.Expr) types.Type {
	switch x := e.(type) {
	case *ast.IntLit:
		x.Type = types.UntypedInt
		return types.UntypedInt
	case *ast.FloatLit:
		x.Type = types.UntypedFloat
		return types.UntypedFloat
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

// callValue checks a call whose single value is used. A union's
// construction, Expr.int(5), gives the union (see construction). A
// conversion gives the type it converts to, but for one to a view, []uint8(s), which only a
// view parameter may take (see viewArg). len, make and append give their
// values (see genericCall). A call to a function that returns nothing, or
// several values, has no one value to give and is reported. A function
// whose one result type is unknown was reported where it is declared. A
// view a function returns is allowed only where viewCall marks the call
// (see viewCallValue).
func (c *Checker) callValue(call *ast.Call) types.Type {
	viewOK := c.viewCall == call
	c.viewCall = nil
	if typ, special := c.specialCallValue(call); special {
		return typ
	}
	if generic := c.genericCall(call); generic != nil {
		typ := generic(call)
		if typ != types.Invalid && givesWithError(call) && c.droppedCall != call {
			// add, add_all and insert give two values, the id, span or
			// handle and an error.
			id, _ := call.Fn.(*ast.Ident)
			c.errorf(call.Spanned(), "%s returns 2 values", id.Name)
			return types.Invalid
		}
		return typ
	}
	c.checkCall(call)
	if isValueCall(call) && call.Dynamic == nil {
		// What the call calls through was reported.
		return types.Invalid
	}

	name, ok := c.calleeName(call)
	if !ok {
		return types.Invalid
	}
	fn := ast.Callee(call)
	if results := ast.CallResults(call); fn != nil && len(results) == 1 {
		return c.viewCallValue(call, results[0], viewOK)
	}
	c.errorf(call.Spanned(), "%s", notOneValue(fn, name))
	return types.Invalid
}

// specialCallValue checks call when it calls no function: when it builds a
// union, converts a value, to a view type, an id, a span or a handle
// type, which it reports, or another, or calls a name
// bound to ast.Unbound (see unboundCall). It returns the call's type, and
// whether call was one of those.
func (c *Checker) specialCallValue(call *ast.Call) (types.Type, bool) {
	switch _, _, _, built := ast.Construction(call); {
	case built:
		return c.construction(call), true
	case isViewConversion(call):
		return c.viewConversionElsewhere(call), true
	case isRefConversion(call):
		c.refConversion(call, call.Fn.(*ast.RefType))
		return types.Invalid, true
	case call.Conv != types.Invalid:
		return c.conversion(call), true
	case c.unboundCall(call):
		return types.Invalid, true
	}
	return types.Invalid, false
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

// notOneValue says why a call to fn, named name, cannot be used as a
// value: it returns nothing, or more than one value. A builtin, whose fn is
// nil, returns nothing.
func notOneValue(fn *ast.Func, name string) string {
	if fn != nil && len(fn.ResultTypes) > 1 {
		return fmt.Sprintf("%s returns %d values", name, len(fn.ResultTypes))
	}
	return fmt.Sprintf("%s returns no value", name)
}

// checkCall checks a call to a function, a builtin, a method (see
// checkMethodCall, and checkMethodExprCall for one called through its
// type) or a function value (see dynamicCall): its argument count,
// that each argument can be passed to the parameter in its position (see
// checkArg), or to a variadic one (see checkVariadicCall), that no two of
// its mut arguments overlap (see checkExclusive), that none reads a list
// another one passes as mut (see checkListBorrows), and, for std/fmt's
// printf, eprintf and appendf, a known format against the values (see
// checkPrintf). Its result, if any, is the caller's to use or discard.
func (c *Checker) checkCall(call *ast.Call) {
	if call.Receiver() != nil {
		c.checkMethodCall(call)
		return
	}
	if sel, isMethodExpr := ast.MethodExpr(call.Fn); isMethodExpr {
		c.checkMethodExprCall(call, sel)
		return
	}
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		c.errorf(call.Spanned(), "cannot call %T", call.Fn)
		return
	}
	switch {
	case c.unboundCall(call):
	case isValueCall(call):
		c.dynamicCall(call, id)
	default:
		c.checkNamedCall(call, id)
	}
}

// checkNamedCall checks call, a call to a function or a builtin named id,
// as checkCall says. Type arguments written for a function that declares
// no type parameters, f[T](x), are reported, and the call is checked as
// if they were not written (see bindWritten); the resolver reported them
// on anything else.
func (c *Checker) checkNamedCall(call *ast.Call, id *ast.Ident) {
	c.hiddenValue(id)
	if fn, ok := id.Def.(*ast.Func); ok && fn != nil && call.TypeArgExprs != nil &&
		len(declaredTypeParams(fn)) == 0 {
		c.errorf(
			ast.TypeArgsSpan(call),
			"cannot write type arguments for %s: it declares no type parameters",
			id.Qualified(),
		)
	}

	params := c.calleeParams(id)
	var (
		muts    []*ast.MutArg
		counted bool
	)
	if fn, ok := id.Def.(*ast.Func); ok && fn != nil && fn.Generic() {
		muts, counted = c.checkGenericCall(call, fn, id.Qualified())
	} else {
		muts, counted = c.checkArgs(call, id.Qualified(), params)
	}
	if !counted {
		return
	}
	c.checkExclusive(muts)
	c.checkListBorrows(call)
	c.checkCaptures(call)
	if fn, ok := id.Def.(*ast.Func); ok && isVariadic(params) {
		c.checkPrintf(call, fn)
	}
}

// checkArgs checks the arguments of call, Args, against params, the
// parameters callee, as a message names it, takes for them: their count,
// and each argument against its parameter (see checkArg), or a variadic
// parameter's values (see checkVariadicCall). It returns the mut
// arguments passed to mut parameters with nothing reported, for the
// exclusivity check, and false when a callee that is not variadic was
// passed the wrong number of arguments, so that no argument was matched
// with a parameter.
func (c *Checker) checkArgs(call *ast.Call, callee string, params []param) ([]*ast.MutArg, bool) {
	if isVariadic(params) {
		return c.checkVariadicCall(call, callee, params), true
	}
	if !c.checkArity(call, callee, len(params)) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return nil, false
	}
	var muts []*ast.MutArg
	for i, arg := range call.Args {
		if m := c.checkArg(arg, params[i], callee); m != nil {
			muts = append(muts, m)
		}
	}
	return muts, true
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
// constant takes want as its type if its value fits, an untyped integer
// constant an integer or a float type and an untyped float constant a
// float type; a typed value must be of type want or widen to it without
// loss. An Invalid got or want has been reported already. It reports
// whether e is a usable value of want.
func (c *Checker) use(e ast.Expr, got, want types.Type, context string) bool {
	if got == types.Invalid || want == types.Invalid {
		return false
	}
	if types.IsUntyped(got) {
		switch {
		case types.IsFloat(want) && !types.IsUntyped(want):
			return c.settleFloat(e, want)
		case got == types.UntypedInt && types.IsInteger(want):
			return c.settle(e, want)
		}
		if got = c.valueOf(e, got); got == types.Invalid {
			return false
		}
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
// an untyped integer constant becomes an int64, and an untyped float
// constant a float64, if its value fits. It returns the resulting type,
// Invalid if the value does not fit.
func (c *Checker) valueOf(e ast.Expr, got types.Type) types.Type {
	switch got {
	case types.UntypedInt:
		if !c.settle(e, types.Int64) {
			return types.Invalid
		}
		return types.Int64
	case types.UntypedFloat:
		if !c.settleFloat(e, types.Float64) {
			return types.Invalid
		}
		return types.Float64
	default:
		return got
	}
}

// settleFloat gives the untyped constant expression e, an integer or a
// float one, the float type typ, if its value rounds to a finite value of
// typ, and reports whether it does. A value too large for typ is
// reported as overflowing it; one too small rounds to zero, or to a
// subnormal, as at run time. As for settle, the type is recorded on e
// alone, and the backend writes out e's value, rounded to typ.
func (c *Checker) settleFloat(e ast.Expr, typ types.Type) bool {
	value, _ := floatValue(e)
	var finite bool
	if typ == types.Float32 {
		_, finite = value.Float32()
	} else {
		_, finite = value.Float64()
	}
	if !finite {
		c.errorf(e.Spanned(), "constant %s overflows %s", value, c.typeName(typ))
		return false
	}
	switch x := e.(type) {
	case *ast.IntLit:
		x.Type = typ
	case *ast.FloatLit:
		x.Type = typ
	case *ast.Binary:
		x.Type = typ
	case *ast.Unary:
		x.Type = typ
	}
	return true
}

// floatValue returns the exact value of an untyped constant expression,
// an integer or a float one, as a float constant, and whether e is one.
func floatValue(e ast.Expr) (constant.Float, bool) {
	var v any
	switch x := e.(type) {
	case *ast.FloatLit:
		return x.Value, true
	case *ast.IntLit:
		return constant.FloatFromInt(x.Value), true
	case *ast.Binary:
		v = x.Const
	case *ast.Unary:
		v = x.Const
	case *ast.Call:
		v = x.Const
	}
	switch k := v.(type) {
	case constant.Float:
		return k, true
	case constant.Int:
		return constant.FloatFromInt(k), true
	default:
		return constant.Float{}, false
	}
}

// constValue returns the value of an integer constant expression, and
// whether e is one. A character literal is one too, though of type uint8
// rather than untyped. An integer constant given a float type is not
// one: its value is a float's from then on (see floatValue).
func constValue(e ast.Expr) (constant.Int, bool) {
	if types.IsFloat(ast.TypeOf(e)) {
		return constant.Int{}, false
	}
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
		// result: after a true left side for &&, a false one for ||. What
		// the left side says holds in the right side only while nothing
		// in the right side assigns it.
		// The left side's facts are recorded once it is checked, when it
		// is an operator too (see joinFacts).
		m := c.mark()
		left := &guard{cond: bin.X, span: bin.X.Spanned(), kind: guardCond}
		facts := c.takeChain(bin.X, bin.Op == ast.And)
		c.assume(left, facts, []ast.Node{bin.Y})
		y = c.expr(bin.Y)
		c.release(m)
		c.joinFacts(bin, facts)
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
	case bin.Op.IsShift():
		return c.shift(bin, x, y)
	case bin.Op.IsWrapping():
		return c.wrapping(bin, x, y)
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

// arithmetic checks + - * / % & | ^ or &^ on operands of types x and y,
// which must be integers, and returns the type of the result. Two untyped constants give
// an untyped constant, computed exactly; an untyped constant and a typed
// operand give the typed one's type, which the constant must fit; and two
// typed operands give whichever of the two the other widens to.
func (c *Checker) arithmetic(bin *ast.Binary, x, y types.Type) types.Type {
	if types.IsFloat(x) || types.IsFloat(y) {
		return c.floatArithmetic(bin, x, y)
	}
	return c.intArithmetic(bin, x, y)
}

// intArithmetic is arithmetic on operands of which neither is a float.
func (c *Checker) intArithmetic(bin *ast.Binary, x, y types.Type) types.Type {
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
	checked := c.checkedDivision == bin
	nonzero := true
	if (bin.Op == ast.Div || bin.Op == ast.Rem) && !checked {
		nonzero = c.requireNonzero(bin.Y, true)
	}
	if c.knownOverflow(bin, typ) {
		return types.Invalid
	}
	if bin.Op == ast.Div && !checked && nonzero {
		// The proof reads bin's type, which its check records only once
		// it returns it.
		bin.Type = typ
		c.requireNoQuotientOverflow(bin, typ, nil)
	}
	if isPlainArith(bin) {
		c.requireNoOverflow(bin, typ)
	}
	return typ
}

// floatArithmetic checks arithmetic with a float among its operands, of
// types x and y: only + - * and / are defined on a float, and both
// operands must be numbers. Two untyped constants give an untyped float
// constant, computed exactly (see foldFloat); an untyped constant and a
// typed float give the float's type, which the constant must fit; and two
// typed operands must have the same float type, since no integer, and no
// other float type, is converted to a float implicitly. A float division
// needs no proof that its divisor is not zero: it gives an infinity, or
// NaN, and never traps.
func (c *Checker) floatArithmetic(bin *ast.Binary, x, y types.Type) types.Type {
	if !c.operandsDefined(bin, x, y, types.IsNumeric) || !c.floatOperator(bin, x, y) {
		return types.Invalid
	}
	if types.IsUntyped(x) && types.IsUntyped(y) {
		return c.foldFloat(bin)
	}
	return c.typedFloatArithmetic(bin, x, y)
}

// floatOperator reports bin, whose operands, of types x and y, are a
// float and a number, unless its operator is defined on floats, + - * or
// /, naming the float operand's type, and returns whether it is.
func (c *Checker) floatOperator(bin *ast.Binary, x, y types.Type) bool {
	if floatOp(bin.Op) {
		return true
	}
	operand, got := bin.X, x
	if !types.IsFloat(x) {
		operand, got = bin.Y, y
	}
	if got = c.valueOf(operand, got); got != types.Invalid {
		c.errorf(operand.Spanned(), "operator %s is not defined on %s", bin.Op, c.typeName(got))
	}
	return false
}

// typedFloatArithmetic returns the type float arithmetic on operands of
// types x and y works in, when at least one is typed (see
// floatArithmetic).
func (c *Checker) typedFloatArithmetic(bin *ast.Binary, x, y types.Type) types.Type {
	switch {
	case types.IsUntyped(x) && types.IsFloat(y):
		if !c.settleFloat(bin.X, y) {
			return types.Invalid
		}
		return y
	case types.IsUntyped(y) && types.IsFloat(x):
		if !c.settleFloat(bin.Y, x) {
			return types.Invalid
		}
		return x
	}
	if x, y = c.valueOf(bin.X, x), c.valueOf(bin.Y, y); x == types.Invalid || y == types.Invalid {
		return types.Invalid
	}
	if x != y {
		c.errorf(bin.Span, "mismatched types %s and %s in %s", c.typeName(x), c.typeName(y), bin.Op)
		return types.Invalid
	}
	return x
}

// foldFloat computes + - * or / on two untyped constants, at least one of
// them a float, exactly, recording the value on bin. A division by zero,
// or a result too large or too precise to hold exactly, is reported.
func (c *Checker) foldFloat(bin *ast.Binary) types.Type {
	a, _ := floatValue(bin.X)
	b, _ := floatValue(bin.Y)
	var (
		result constant.Float
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
		result, ok = a.Quo(b)
	}
	if !ok {
		c.errorf(bin.Span, "constant expression is out of range")
		return types.Invalid
	}
	bin.Const = result
	return types.UntypedFloat
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
// compound assignment cannot. It returns whether the divisor is proven,
// and so was not reported.
func (c *Checker) requireNonzero(divisor ast.Expr, withOk bool) bool {
	if v, ok := c.knownValue(divisor); ok {
		if v.IsZero() {
			c.errorf(divisor.Spanned(), "division by zero")
			return false
		}
		return true
	}
	if c.nonzero(divisor) {
		return true
	}
	msg, notes := c.divisorError(divisor)
	if withOk {
		msg += orUse("q, ok := ... / "+c.operandName(divisor), strings.HasSuffix(msg, " first"))
	}
	c.errorNotes(divisor.Spanned(), notes, "%s", msg)
	return false
}

// divisorError returns the error for a divisor no fact proves not zero,
// before any mention of the checked form, and the notes that explain it.
// A subject, a variable or a path below an immutable one, gets the check
// that would prove it, except a mutable global, which no check proves.
// The length of a string, view or list subject gets the notes a subject
// does, with no check in the error.
func (c *Checker) divisorError(divisor ast.Expr) (string, []diag.Diagnostic) {
	if v, ok := c.mutableGlobal(divisor); ok {
		return "divisor " + c.operandName(divisor) + " may be zero", globalNote(v)
	}
	if c.isSubject(divisor) {
		name := c.nameOf(divisor)
		return "divisor " + name + " may be zero; check " + name + " != 0 first",
			c.explain(c.divisorProof(divisor))
	}
	if c.isLengthSubject(divisor) {
		return "divisor may be zero", c.explain(c.divisorProof(divisor))
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
	if types.IsFloat(typ) {
		c.errorf(span, "dividing floats cannot fail, so it has no checked form; use a / b")
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

// foldArithmetic computes an arithmetic or a bitwise expression on two
// untyped constants exactly, recording the value on bin. A division by zero, or a result
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
	case ast.BitAnd, ast.BitOr, ast.BitXor, ast.AndNot:
		result, ok = bitwiseStep(bin.Op, a, b)
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
// six, a bool, an array, a list, a struct or a union only == and !=. A
// mismatch, or an unsupported operator, is one error covering the whole
// comparison; a comparison of values holding lists is then checked for
// borrows (checkCompareBorrows). Two
// untyped constants are compared exactly, and the result recorded on bin;
// two string literals are not folded, and are compared at run time.
func (c *Checker) comparison(bin *ast.Binary, x, y types.Type) {
	if x == types.Invalid || y == types.Invalid {
		return
	}
	if types.IsUntyped(x) && types.IsUntyped(y) {
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
	case c.table.Owned(common):
		c.checkCompareBorrows(bin)
	}
}

// foldComparison compares bin's two untyped constant operands exactly, as
// floats when either is one, and records the result on bin.
func foldComparison(bin *ast.Binary) {
	if a, ok := constValue(bin.X); ok {
		if b, ok := constValue(bin.Y); ok {
			bin.Const = compare(bin.Op, a.Cmp(b))
			return
		}
	}
	a, _ := floatValue(bin.X)
	b, _ := floatValue(bin.Y)
	bin.Const = compare(bin.Op, a.Cmp(b))
}

// untypedAs settles operand, of type got, to the type other of the operand
// it is compared with, when got is untyped: to other itself if other is a
// float type, or an integer type for an untyped integer, and to its own
// type otherwise (see valueOf), so a mismatch names it as one. It returns
// the operand's type, Invalid if the constant does not fit.
func (c *Checker) untypedAs(operand ast.Expr, got, other types.Type) types.Type {
	if !types.IsUntyped(got) {
		return got
	}
	if types.IsFloat(other) && !types.IsUntyped(other) {
		if !c.settleFloat(operand, other) {
			return types.Invalid
		}
		return other
	}
	if got == types.UntypedInt && types.IsInteger(other) && other != types.UntypedInt {
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
// integer, a float or a string with all six, and any other comparable
// type, a bool, an array or a struct, with == and != only. Floats compare
// as IEEE 754 says: NaN is equal to nothing, itself included, and -0 is
// equal to 0.
func (c *Checker) supports(typ types.Type, op ast.BinaryOp) bool {
	if types.IsNumeric(typ) || typ == types.String {
		return true
	}
	return (op == ast.Eq || op == ast.Ne) && c.comparable(typ)
}

// unary checks a prefix expression and returns its type. ! takes a bool, ^
// any integer (see complement). - takes a float, or a signed integer, and negating an untyped constant is computed
// exactly, as is negating a known value of a signed type, which must not
// overflow it; it is not defined on unsigned types, where -x is almost always a
// mistake, and 0 - x says what wrapping is wanted.
func (c *Checker) unary(u *ast.Unary) types.Type {
	x := c.expr(u.X)
	switch u.Op {
	case ast.Not:
		if c.require(u.X, x, "!", types.Bool) {
			return types.Bool
		}
		return types.Invalid
	case ast.Complement:
		return c.complement(u, x)
	case ast.NegWrap:
		return c.wrapNegation(u, x)
	}
	return c.negation(u, x)
}

// negation checks -x, u, x of type x, and returns its type (see unary).
func (c *Checker) negation(u *ast.Unary, x types.Type) types.Type {
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
	case x == types.UntypedFloat:
		value, _ := floatValue(u.X)
		u.Const = value.Neg()
		return types.UntypedFloat
	case types.IsFloat(x):
		return x
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
		c.requireNoOverflow(u, typ)
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
// widens to T, a known value, as knownValue computes it, that is a value
// of T on every target, or a subject the facts in force prove one (see
// factsFit). A constant or known value that is not one of T's is reported
// as overflowing T, a known value that is one of a target-width T's on
// some targets only as needing the two-value form v, ok := T(x), since no
// fact changes a known value, and any other conversion as needing a check
// or that form (see conversionError).
func (c *Checker) conversion(call *ast.Call) types.Type {
	from, ok := c.conversionOperand(call)
	if !ok {
		return types.Invalid
	}
	to := call.Conv
	if types.IsFloat(from) || types.IsFloat(to) {
		return c.floatConversion(call, from, to)
	}
	return c.intConversion(call, from, to)
}

// intConversion is conversion of an integer, of type from, to the integer
// type to.
func (c *Checker) intConversion(call *ast.Call, from, to types.Type) types.Type {
	arg := call.Args[0]
	switch {
	case from == types.UntypedInt:
		if !c.settle(arg, to) {
			return types.Invalid
		}
	case types.Widens(from, to):
	default:
		v, known := c.knownValue(arg)
		switch {
		case known && fitsSomewhere(v, to):
			c.errorf(
				call.Span,
				"converting %s to %s can fail; use v, ok := %s(...)",
				c.typeName(from),
				c.typeName(to),
				c.typeName(to),
			)
		case known:
			if !c.knownFits(arg, v, to) {
				return types.Invalid
			}
		case !c.factsFit(arg, to):
			c.conversionError(call, from, to)
		}
	}
	return to
}

// floatConversion checks a conversion whose single value is used, call, to
// or from a float, from from to to, and returns to. An integer converts to
// a float, and a float to the other float type, in this form: it never
// fails, rounding to the nearest value of to, ties to even, and a float64
// too large for a float32 to an infinity. A constant converts exactly (see
// constConversion). A float converts to an integer only in the checked
// form, v, ok := T(f), since floats carry no facts that would prove it in
// range, and here is reported as needing it.
func (c *Checker) floatConversion(call *ast.Call, from, to types.Type) types.Type {
	switch {
	case types.IsUntyped(from):
		if !c.constConversion(call, from, to) {
			return types.Invalid
		}
	case types.IsInteger(to):
		c.errorf(
			call.Span,
			"converting %s to %s can fail; use v, ok := %s(...)",
			c.typeName(from),
			c.typeName(to),
			c.typeName(to),
		)
		return types.Invalid
	}
	return to
}

// constConversion checks the conversion call, to or from a float, of an
// untyped constant, of type from, to the type to, and records its value
// on call, exactly, for the backend to write out: to a float, the
// constant's exact value, which must round to a finite value of to; to an
// integer, from a float constant, its value, which must be an integer, as
// in Go, int64(2.0), and a value of to. It reports whether the conversion
// is valid.
func (c *Checker) constConversion(call *ast.Call, from, to types.Type) bool {
	arg := call.Args[0]
	value, _ := floatValue(arg)
	if types.IsFloat(to) {
		if !c.settleFloat(arg, to) {
			return false
		}
		call.Const = value
		return true
	}
	n, isInt := value.Int()
	switch {
	case !isInt && !value.IsInteger():
		c.errorf(call.Span, "cannot convert constant %s to %s: it is not an integer",
			value, c.typeName(to))
		return false
	case !isInt || !fitsPortably(n, to):
		c.errorf(
			arg.Spanned(),
			"constant %s overflows %s%s",
			value,
			c.typeName(to),
			widthNote(n, to),
		)
		return false
	}
	c.valueOf(arg, from)
	call.Const = n
	return true
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
// value is used alone or with ok: one argument, an integer or a float type
// to convert to, and an integer or a float to convert. It returns the argument's type, and false
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
	if !types.IsNumeric(to) || !types.IsNumeric(from) {
		from = c.valueOf(call.Args[0], from)
		if from != types.Invalid {
			c.errorf(call.Span, "cannot convert %s to %s", c.typeName(from), name)
		}
		return types.Invalid, false
	}
	return from, true
}

// checkedConversion checks a conversion in its two-value form,
// v, ok := T(x), which is for conversions that can fail, a float's to an
// integer among them, and is allowed for
// one that cannot, from an untyped constant, a known value that fits T, a
// type that widens to T, or anything to a float, when ok is always true. Only a constant or known
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
	case types.IsFloat(from) || types.IsFloat(to):
		return c.checkedFloatConversion(call, from, to)
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

// checkedFloatConversion is checkedConversion to or from a float: a
// constant's is checked as in the plain form (see constConversion), and
// anything else is valid, a float's to an integer being tested at run
// time and anything to a float never failing.
func (c *Checker) checkedFloatConversion(call *ast.Call, from, to types.Type) bool {
	if types.IsUntyped(from) {
		return c.constConversion(call, from, to)
	}
	return true
}

// complement checks ^x, x of type x, and returns its type: x's, for any
// integer type, flipping every bit, and an untyped constant's complement,
// -x - 1, computed exactly, for an untyped constant. Its value, when
// known, is never out of its type's range, so there is nothing to report
// about it.
func (c *Checker) complement(u *ast.Unary, x types.Type) types.Type {
	switch {
	case x == types.Invalid:
		return types.Invalid
	case x == types.UntypedInt:
		value, _ := constValue(u.X)
		flipped, ok := value.Not()
		if !ok {
			c.errorf(u.Span, "constant expression is out of range")
			return types.Invalid
		}
		u.Const = flipped
		return types.UntypedInt
	case types.IsInteger(x):
		return x
	default:
		if x = c.valueOf(u.X, x); x != types.Invalid {
			c.errorf(u.X.Spanned(), "operator ^ is not defined on %s", c.typeName(x))
		}
		return types.Invalid
	}
}

// shift checks x << n or x >> n, bin, on operands of types x and y, and
// returns the type of the result: x's. x must be an integer and n an
// unsigned integer or an untyped constant that is not negative (see
// shiftCount). Two untyped constants give an untyped constant, computed
// exactly. An untyped x shifted by a typed count is an int64, as an
// untyped constant with nothing else to give it a type is; an untyped
// count is a uint64. Known operands are computed as for arithmetic, and
// a result that is not a value of the type is reported (see
// knownOverflow).
func (c *Checker) shift(bin *ast.Binary, x, y types.Type) types.Type {
	if !c.shiftOperands(bin, x, y) {
		return types.Invalid
	}
	if x == types.UntypedInt && y == types.UntypedInt {
		return c.foldShift(bin)
	}
	typ := x
	if x == types.UntypedInt {
		typ = types.Int64
		if !c.settle(bin.X, typ) {
			return types.Invalid
		}
	}
	if y == types.UntypedInt && !c.settle(bin.Y, types.Uint64) {
		return types.Invalid
	}
	if c.knownOverflow(bin, typ) {
		return types.Invalid
	}
	return typ
}

// shiftOperands reports the operands of bin, a shift, of types x and y,
// unless x is an integer and y a count (see shiftCount), and returns
// whether they are. An Invalid operand has been reported already.
func (c *Checker) shiftOperands(bin *ast.Binary, x, y types.Type) bool {
	if x == types.Invalid || y == types.Invalid {
		return false
	}
	leftOK := types.IsInteger(x)
	if !leftOK {
		if got := c.valueOf(bin.X, x); got != types.Invalid {
			c.errorf(bin.X.Spanned(), "operator %s is not defined on %s", bin.Op, c.typeName(got))
		}
	}
	return c.shiftCount(bin.Y, y) && leftOK
}

// shiftCount checks n, of type y, the count of a shift: an unsigned
// integer of any width, or an untyped constant that is not negative,
// which becomes a uint64 unless the shift is of two untyped constants. It
// reports any other count and returns whether n is one.
func (c *Checker) shiftCount(n ast.Expr, y types.Type) bool {
	switch {
	case y == types.Invalid:
		return false
	case y == types.UntypedInt:
		if v, _ := constValue(n); v.IsNegative() {
			c.errorf(n.Spanned(), "shift count must not be negative, got %s", v)
			return false
		}
		return true
	case types.IsInteger(y) && types.IsUnsigned(y):
		return true
	}
	if got := c.valueOf(n, y); got != types.Invalid {
		c.errorf(n.Spanned(), "shift count must be an unsigned integer, got %s", c.typeName(got))
	}
	return false
}

// foldShift computes a shift of an untyped constant by an untyped count,
// exactly, recording the value on bin: x << n is x times 2^n, which must
// stay in the range constants lie in, and x >> n x divided by 2^n,
// rounded down.
func (c *Checker) foldShift(bin *ast.Binary) types.Type {
	a, _ := constValue(bin.X)
	n, _ := constValue(bin.Y)
	result, ok := shiftStep(bin.Op, a, n)
	if !ok {
		c.errorf(bin.Span, "constant expression is out of range")
		return types.Invalid
	}
	bin.Const = result
	return types.UntypedInt
}
