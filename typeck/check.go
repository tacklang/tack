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
	funcs map[string]*ast.Func
	diags []diag.Diagnostic
}

// New returns a Checker for file, which must already have been resolved.
func New(file *ast.File) *Checker {
	return &Checker{file: file, funcs: make(map[string]*ast.Func)}
}

// Check reports every badly typed call in the file, in source order. Checking
// continues past the first problem, so one run reports all of them.
func (c *Checker) Check() []diag.Diagnostic {
	if c.file == nil {
		panic("file is nil")
	}

	// Signatures are resolved for every function before any body is checked,
	// so a call does not depend on its callee appearing first.
	for _, decl := range c.file.Decls {
		if fn, ok := decl.(*ast.Func); ok {
			c.funcs[fn.Name] = fn
			c.resolveParamKinds(fn)
			c.resolveResultKind(fn)
		}
	}

	// Globals are checked before any body, so their kinds are known in every
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
	if fn.Result != nil && !blockTerminates(fn.Body) {
		c.errorf(fn.NameSpan, "%s must end in a return", fn.Name)
	}
}

func (c *Checker) checkStmt(stmt ast.Stmt, fn *ast.Func) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		c.checkExpr(s.X)

	case *ast.Return:
		c.checkReturn(s, fn)

	case *ast.Var:
		c.checkVar(s)

	case *ast.Assign:
		c.checkAssign(s)

	case *ast.If:
		c.checkExpr(s.Cond)
		if got := ast.KindOf(s.Cond); got != types.Bool && !reportedInvalid(s.Cond, got) {
			c.errorf(s.Cond.Spanned(), "condition must be a bool, got %s", got)
		}
		for _, inner := range s.Then {
			c.checkStmt(inner, fn)
		}
		for _, inner := range s.Else {
			c.checkStmt(inner, fn)
		}
	}
}

// checkVar records a variable's kind: its declared type if it has one,
// otherwise its value's. A declared type and a value must agree, and only a
// mutable variable may leave out its value and start from its zero value,
// since an immutable one could never hold anything else.
func (c *Checker) checkVar(v *ast.Var) {
	declared := types.Invalid
	if v.Type != nil {
		declared = c.typeKind(v.Type)
	}

	if v.Value == nil {
		if !v.Mutable {
			c.errorf(v.NameSpan, "immutable variable %s needs a value", v.Name)
		}
		v.Kind = declared
		return
	}

	got := c.valueKind(v.Value)
	switch {
	case v.Type == nil:
		v.Kind = got
	case got != types.Invalid && declared != types.Invalid && got != declared:
		c.errorf(
			v.Value.Spanned(),
			"cannot use %s as %s in declaration of %s",
			got,
			declared,
			v.Name,
		)
		v.Kind = declared
	default:
		v.Kind = declared
	}
}

// checkGlobal checks a package-level variable like a local, and also
// requires its initializer to be a constant expression, which it evaluates
// into v.Const. A global without a value takes its type's zero value.
func (c *Checker) checkGlobal(v *ast.Var) {
	if v.Value != nil && !isConstant(v.Value) {
		c.errorf(v.Value.Spanned(), "%s must be initialized with a constant expression", v.Name)
		if v.Type != nil {
			v.Kind = c.typeKind(v.Type)
		}
		return
	}

	before := len(c.diags)
	c.checkVar(v)
	if len(c.diags) > before {
		return
	}

	if v.Value == nil {
		v.Const = zeroConst(v.Kind)
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
	default:
		return false
	}
}

// zeroConst is a type's zero value as a constant.
func zeroConst(kind types.Kind) any {
	switch kind {
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

// fold evaluates a constant expression that has already been type-checked,
// following Go's integer arithmetic: division truncates toward zero and a
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
		return c.foldBinary(x, left.(int64), right.(int64))
	default:
		return nil, false
	}
}

// foldBinary applies an operator to two int constants.
func (c *Checker) foldBinary(bin *ast.Binary, a, b int64) (any, bool) {
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
// have the variable's type. Parameters are never assignable.
func (c *Checker) checkAssign(a *ast.Assign) {
	got := c.valueKind(a.Value)

	switch def := a.Target.Def.(type) {
	case *ast.Param:
		c.errorf(a.Target.Span, "cannot assign to parameter %s", a.Target.Name)
	case *ast.Var:
		if !def.Mutable {
			c.errorf(a.Target.Span, "cannot assign to immutable variable %s", a.Target.Name)
			return
		}
		if got != types.Invalid && got != def.Kind {
			c.errorf(
				a.Value.Spanned(),
				"cannot use %s as %s in assignment to %s",
				got,
				def.Kind,
				a.Target.Name,
			)
		}
	}
}

// valueKind checks an expression used as a value and returns its kind. A call
// to a function that returns nothing is reported here, since it has no value
// to give; the result is then types.Invalid.
func (c *Checker) valueKind(e ast.Expr) types.Kind {
	c.checkExpr(e)

	kind := ast.KindOf(e)
	if kind == types.Invalid {
		if call, ok := e.(*ast.Call); ok {
			if id, ok := call.Fn.(*ast.Ident); ok {
				c.errorf(e.Spanned(), "%s returns no value", id.Name)
			}
		}
	}
	return kind
}

// typeKind returns the kind a type expression denotes, reporting a type it
// does not know and returning types.Invalid for it.
func (c *Checker) typeKind(typ ast.Expr) types.Kind {
	id, ok := typ.(*ast.Ident)
	if !ok {
		c.errorf(typ.Spanned(), "unsupported type expression: %T", typ)
		return types.Invalid
	}

	kind, known := types.Lookup(id.Name)
	if !known {
		c.errorf(id.Span, "unknown type: %s", id.Name)
		return types.Invalid
	}
	return kind
}

// terminates reports whether control can fall out of the bottom of stmt. An if
// only terminates when it has an else and both branches do, since otherwise
// the condition being false falls through.
func terminates(stmt ast.Stmt) bool {
	switch s := stmt.(type) {
	case *ast.Return:
		return true
	case *ast.If:
		return len(s.Else) > 0 && blockTerminates(s.Then) && blockTerminates(s.Else)
	default:
		return false
	}
}

func blockTerminates(body []ast.Stmt) bool {
	return len(body) > 0 && terminates(body[len(body)-1])
}

func (c *Checker) checkReturn(ret *ast.Return, fn *ast.Func) {
	if ret.Value == nil {
		if fn.Result != nil {
			c.errorf(ret.Span, "%s must return a value of type %s", fn.Name, fn.ResultKind)
		}
		return
	}

	c.checkExpr(ret.Value)

	if fn.Result == nil {
		c.errorf(ret.Value.Spanned(), "%s declares no result, so it cannot return a value", fn.Name)
		return
	}

	// A result type that is unknown has been reported already, and there is
	// nothing to compare the value with.
	if fn.ResultKind == types.Invalid {
		return
	}

	if got := ast.KindOf(ret.Value); got != fn.ResultKind && !reportedInvalid(ret.Value, got) {
		c.errorf(
			ret.Value.Spanned(),
			"cannot use %s as %s in return from %s",
			got,
			fn.ResultKind,
			fn.Name,
		)
	}
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
	}
}

// checkBinary requires int operands for every operator. Arithmetic on strings
// would need concatenation, and comparing them would need a runtime
// comparison; neither exists.
func (c *Checker) checkBinary(bin *ast.Binary) {
	for _, operand := range []ast.Expr{bin.X, bin.Y} {
		if got := ast.KindOf(operand); got != types.Int && !reportedInvalid(operand, got) {
			c.errorf(operand.Spanned(), "cannot use %s as int in %s", got, bin.Op)
		}
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

	params := make([]types.Kind, len(fn.Params))
	for i, param := range fn.Params {
		params[i] = param.Kind
	}

	if c.checkArity(call, id.Name, len(params)) {
		c.checkArgTypes(call, id.Name, params)
	}
}

// resolveResultKind records what the declared result type denotes.
func (c *Checker) resolveResultKind(fn *ast.Func) {
	if fn.Result == nil {
		return
	}

	id, ok := fn.Result.(*ast.Ident)
	if !ok {
		c.errorf(fn.Result.Spanned(), "unsupported type expression: %T", fn.Result)
		return
	}

	kind, known := types.Lookup(id.Name)
	if !known {
		c.errorf(id.Span, "unknown type: %s", id.Name)
		return
	}
	fn.ResultKind = kind
}

// resolveParamKinds records what each declared parameter type denotes. A
// parameter whose type is unknown keeps types.Invalid, which every later check
// reports rather than silently accepting.
func (c *Checker) resolveParamKinds(fn *ast.Func) {
	for _, param := range fn.Params {
		id, ok := param.Type.(*ast.Ident)
		if !ok {
			c.errorf(param.Type.Spanned(), "unsupported type expression: %T", param.Type)
			continue
		}

		kind, known := types.Lookup(id.Name)
		if !known {
			c.errorf(id.Span, "unknown type: %s", id.Name)
			continue
		}
		param.Kind = kind
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

func (c *Checker) checkArgTypes(call *ast.Call, name string, params []types.Kind) {
	for i, arg := range call.Args {
		// A parameter of unknown type has been reported where it is declared,
		// and an argument whose type is unknown for the same reason has too.
		got := ast.KindOf(arg)
		if params[i] == types.Invalid || reportedInvalid(arg, got) {
			continue
		}
		if got == types.Invalid {
			// The common case is calling a function that returns nothing and
			// using it where a value is expected, which deserves saying so.
			if nested, ok := arg.(*ast.Call); ok {
				if id, ok := nested.Fn.(*ast.Ident); ok {
					c.errorf(arg.Spanned(), "%s returns no value", id.Name)
					continue
				}
			}
			c.errorf(arg.Spanned(), "unsupported expression in argument to %s: %T", name, arg)
			continue
		}
		if got != params[i] {
			c.errorf(arg.Spanned(), "cannot use %s as %s in argument to %s", got, params[i], name)
		}
	}
}

// reportedInvalid reports whether e has no type because of a problem already
// reported where it arose, so reporting it again where e is used would only
// repeat it: a name whose declared type is unknown, an operator applied to
// such a name, or a call to a function whose result type is unknown. A call
// to a function that returns nothing is not one of these; it is reported
// where the value is wanted.
func reportedInvalid(e ast.Expr, kind types.Kind) bool {
	if kind != types.Invalid {
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
			return ok && fn != nil && fn.Result != nil
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
