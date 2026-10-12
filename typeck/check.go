package typeck

import (
	"fmt"

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

	case *ast.If:
		c.checkExpr(s.Cond)
		if got := ast.KindOf(s.Cond); got != types.Bool {
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

	if got := ast.KindOf(ret.Value); got != fn.ResultKind {
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
		if got := ast.KindOf(operand); got != types.Int {
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
		got := ast.KindOf(arg)
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

func (c *Checker) errorf(span ast.Span, format string, args ...any) {
	c.diags = append(c.diags, diag.Diagnostic{
		Severity: diag.Error,
		Message:  fmt.Sprintf(format, args...),
		Span:     span,
	})
}
