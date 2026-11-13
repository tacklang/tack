package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// Method expressions. A method named through its type, Pair.first, or
// through its package and its type, lexer.Token.Kind, is a method
// expression (see ast.MethodExpr): the method as a function value whose
// first parameter is its receiver, so func (p Pair) has(x int64) bool
// gives a func(Pair, int64) bool, and a mut receiver a mut first
// parameter, func(mut Pair, int64). Its type is the function type of the
// method's signature, receiver included, exactly as a function's name
// used as a value has its signature's (see funcValue), so a method and a
// function of one signature are values of one type, and the value is used
// as any function value is. A method whose signature no function type can
// say, a variadic or any parameter or a view result, is no value.
//
// Called, Pair.first(p), it is a call of the method by name: its
// arguments are checked against every parameter of the method, the
// receiver first, as a function's are against its own, and it calls
// that method, as p.first() does, with the method's ensures in force
// after it (call.Method). No function value is made.
//
// The type must be a struct or a union the code can name, and the method
// one it can see, its visibility capped by its type's, as a method call
// requires; what is checked is the name, so a value made where the method
// is visible may be passed anywhere, as a private function's value
// returned from a pub one may. A union's methods share its namespace with
// its cases, which never have a method's name: a name that is a case
// builds the union (see construction), and any other is a method
// expression. A field named through its type, a basic type's name, an
// interface's method and a type parameter's are reported: a basic type
// has no methods, an interface is only a constraint, and a type
// parameter's methods are only called on a value of it.

// methodExpr checks sel, a method expression used as a value, and
// returns its function type, which it records on sel with the method
// (see Selector.Method), or types.Invalid when anything was reported.
func (c *Checker) methodExpr(sel *ast.Selector) types.Type {
	fn := c.namedMethod(sel, false)
	if fn == nil {
		return types.Invalid
	}
	if why := c.notValue(fn); why != "" {
		c.errorf(sel.Span, "cannot use %s as a value: %s", c.displayName(fn), why)
		return types.Invalid
	}
	typ := c.signatureValue(fn)
	if typ == types.Invalid {
		return types.Invalid
	}
	sel.Method, sel.Type = fn, typ
	return typ
}

// checkMethodExprCall checks call, whose callee is sel, a method
// expression, Pair.first(p): a call of the method sel names, recorded in
// call.Method, its arguments checked against every one of the method's
// parameters, the receiver first, as checkNamedCall checks a function's.
// A method that cannot be named is reported, and the arguments are then
// checked for errors of their own.
func (c *Checker) checkMethodExprCall(call *ast.Call, sel *ast.Selector) {
	fn := c.namedMethod(sel, true)
	if fn == nil {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return
	}
	call.Method = fn
	muts, counted := c.checkArgs(call, c.displayName(fn), c.funcParams(fn.Params, true))
	if !counted {
		return
	}
	c.checkExclusive(muts)
	c.checkListBorrows(call)
	c.checkCaptures(call)
}

// namedMethod returns the method sel, a method expression, names, called
// when called says so, or nil after reporting why it names none: its
// type is a basic type, an interface or a type parameter, whose methods
// are not named through it, or a struct or a union with no method of
// that name, a field of that name named instead, or one the package being
// checked cannot see. An operand the resolver reported names nothing, and
// nothing more is said.
func (c *Checker) namedMethod(sel *ast.Selector, called bool) *ast.Func {
	id, _ := sel.X.(*ast.Ident)
	written := c.text(sel.X.Spanned())
	switch d := id.Def.(type) {
	case nil:
		c.basicTypeMember(sel, written)
		return nil
	case *ast.TypeParam:
		if called {
			c.errorf(sel.Span, "cannot call %s through its type: %s is a type parameter, "+
				"and a type parameter's methods can only be called on a value of it",
				c.text(sel.Span), written)
		} else {
			c.errorf(sel.Span, "cannot use %s as a value: %s is a type parameter, "+
				"and a type parameter's methods can only be called on a value of it",
				c.text(sel.Span), written)
		}
		return nil
	case *ast.TypeDecl:
		if d.Interface() != nil {
			c.interfaceMember(sel, written, called)
			return nil
		}
		return c.declaredMethod(sel, d)
	default:
		return nil
	}
}

// basicTypeMember reports sel, a name selected through a basic type
// written as written: a field of the type, error.code, is no method, and
// a basic type has no methods to name.
func (c *Checker) basicTypeMember(sel *ast.Selector, written string) {
	typ, _ := types.Lookup(written)
	if _, _, isField := c.table.Field(typ, sel.Name); isField {
		c.errorf(sel.NameSpan, "%s is a field of %s, not a method; "+
			"only a method can be named through its type", sel.Name, written)
		return
	}
	c.errorf(sel.NameSpan, "%s has no method %s", written, sel.Name)
}

// interfaceMember reports sel, a name selected through an interface
// written as written: an interface is only a constraint, so its methods
// are not values, nor called through it.
func (c *Checker) interfaceMember(sel *ast.Selector, written string, called bool) {
	if called {
		c.errorf(sel.Span, "cannot call %s: %s is an interface; "+
			"only a struct or union type's methods can be called through their type",
			c.text(sel.Span), written)
		return
	}
	c.errorf(sel.Span, "cannot use %s as a value: %s is an interface; "+
		"only a struct or union type's methods are values", c.text(sel.Span), written)
}

// declaredMethod returns the method sel names of td, a struct or a union
// type, or nil after reporting that it has none of that name, a field of
// that name named instead, or that the package being checked cannot see
// it, as methodOf does for a method call.
func (c *Checker) declaredMethod(sel *ast.Selector, td *ast.TypeDecl) *ast.Func {
	typ := td.Type
	if typ == types.Invalid {
		return nil
	}
	if fn := td.Method(sel.Name); fn != nil {
		if msg := c.hiddenMethod(fn); msg != "" {
			c.errorf(sel.NameSpan, "%s", msg)
			return nil
		}
		return fn
	}
	if c.table.IsStruct(typ) && c.resolveFields(typ, sel.Span) {
		if _, _, isField := c.table.Field(typ, sel.Name); isField {
			c.errorf(sel.NameSpan, "%s is a field of %s, not a method; "+
				"only a method can be named through its type", sel.Name, c.typeName(typ))
			return nil
		}
	}
	if c.lostMethodOf(typ, sel.Name) {
		return nil
	}
	if c.table.IsUnion(typ) {
		c.errorf(sel.NameSpan, "%s has no case or method %s", c.typeName(typ), sel.Name)
		return nil
	}
	c.errorf(sel.NameSpan, "%s has no method %s", c.typeName(typ), sel.Name)
	return nil
}
