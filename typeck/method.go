package typeck

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// Methods. A method is a function whose first parameter, its receiver, is
// written before its name, func (b Builder) len() int64, and it is called
// on a value of the receiver's type, b.len(). The resolver has recorded
// each method on the struct type its receiver has (ast.TypeDecl.Methods);
// a call finds the method by the type of what it is called on, which is
// checked first, so it is found through any expression, a field, an
// element, a call's result or a qualified type of another package alike.
//
// The receiver is a parameter, and a call passes it first, under every
// rule an argument follows. A plain receiver is a plain parameter: any
// value of the type, a temporary included, read as a copy, and a struct
// holding a list borrowed, as a plain list parameter is. A mut receiver
// is a mut parameter: the call writes mut before the place, mut b.write(s),
// which must be a place the caller may change, and it is the one mut
// argument the call's other mut arguments must not overlap. Leaving out
// mut for a mut receiver, or writing it for a plain one, is reported, as
// for any parameter. A method another package cannot see, its visibility
// capped by its type's, is reported where it is called, as a field is.
//
// A method is only ever called: a method named without a call, b.write,
// is reported, as there are no method values.

// checkMethodCall checks call, a method call, x.m(args) or mut x.m(args):
// its receiver, which gives the type the method is found on, the method
// that type has, recorded in call.Method, and then the arguments against
// the method's parameters after its receiver, as checkCall checks a
// function's. A method that cannot be found is reported, and the
// arguments are then checked for errors of their own.
func (c *Checker) checkMethodCall(call *ast.Call) {
	sel, _ := call.Fn.(*ast.Selector)
	fn, recvMut := c.method(call, sel)
	if fn == nil {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return
	}
	call.Method = fn
	params := c.funcParams(fn.Params[1:])
	muts, counted := c.checkArgs(call, c.displayName(fn), params)
	if !counted {
		return
	}
	if recvMut != nil {
		muts = append([]*ast.MutArg{recvMut}, muts...)
	}
	c.checkExclusive(muts)
	c.checkListBorrows(call)
}

// method checks the receiver of call, a method call whose callee is sel,
// and returns the method of its type that sel names, and the receiver
// when it is a mut argument passed to a mut receiver with nothing
// reported, for the exclusivity check. It returns a nil method when the
// receiver had an error, or its type has no such method, or one the
// package being checked cannot see, each reported.
func (c *Checker) method(call *ast.Call, sel *ast.Selector) (*ast.Func, *ast.MutArg) {
	typ, checked := c.receiverType(sel.X)
	if typ == types.Invalid {
		return nil, nil
	}
	fn := c.methodOf(typ, sel)
	m, isMut := sel.X.(*ast.MutArg)
	if fn == nil {
		if isMut && !checked {
			c.expr(m.X)
		}
		return nil, nil
	}
	switch recvMut := fn.Recv().Mut; {
	case recvMut && isMut:
		return fn, c.mutReceiver(m, checked)
	case recvMut:
		c.receiverWithoutMut(call, sel, fn)
	case isMut:
		c.receiverWithMut(call, m, fn, checked)
	}
	return fn, nil
}

// mutReceiver checks m, the receiver mut x of a call to a method with a
// mut receiver, as a mut argument's place, unless checked says
// receiverType has, and returns m when nothing was reported, and nil
// otherwise.
func (c *Checker) mutReceiver(m *ast.MutArg, checked bool) *ast.MutArg {
	c.checkErrorMut(m)
	if !checked && c.place(m.X, m) == types.Invalid {
		return nil
	}
	return m
}

// receiverType returns the type of recv, a method call's receiver, and
// whether checking it has reported everything about it. A plain receiver
// is checked as a value. A mut one, mut x, is not checked yet when x is a
// place whose type placeType can tell, since what is wrong with it, if
// anything, depends on whether the method takes it as mut; any other x is
// checked as a mut argument's place, which reports it.
func (c *Checker) receiverType(recv ast.Expr) (types.Type, bool) {
	m, isMut := recv.(*ast.MutArg)
	if !isMut {
		return c.valueOf(recv, c.expr(recv)), true
	}
	if typ := c.placeType(m.X); typ != types.Invalid {
		return typ, false
	}
	return c.place(m.X, m), true
}

// placeType returns the type of e when it is a place, a variable or a
// parameter, a field of a place or an element of one, whose type its
// declarations already give, without checking it or reporting anything:
// every struct's fields are resolved before any body is checked. It
// returns types.Invalid for anything else, and for a place with a part
// that has no such type, a field its struct lacks among them.
func (c *Checker) placeType(e ast.Expr) types.Type {
	switch x := e.(type) {
	case *ast.Ident:
		return ast.TypeOf(x)
	case *ast.Selector:
		f, _, ok := c.table.Field(c.placeType(x.X), x.Name)
		if !ok {
			return types.Invalid
		}
		return f.Type
	case *ast.Index:
		return c.table.Elem(c.placeType(x.X))
	default:
		return types.Invalid
	}
}

// methodOf returns the method sel names of a value of type typ, reporting
// a type with no such method, a field named instead, and a method the
// package being checked cannot see.
func (c *Checker) methodOf(typ types.Type, sel *ast.Selector) *ast.Func {
	var fn *ast.Func
	if td := c.structs[typ]; td != nil && c.table.IsStruct(typ) {
		fn = td.Method(sel.Name)
	}
	if fn == nil {
		if _, _, isField := c.table.Field(typ, sel.Name); isField {
			c.errorf(sel.NameSpan, "%s is a field of %s, not a method; a field cannot be called",
				sel.Name, c.typeName(typ))
			return nil
		}
		c.errorf(sel.NameSpan, "%s has no method %s", c.typeName(typ), sel.Name)
		return nil
	}
	if msg := c.hiddenMethod(fn); msg != "" {
		c.errorf(sel.NameSpan, "%s", msg)
		return nil
	}
	return fn
}

// hiddenMethod returns why the code being checked may not call fn, a
// method, or "" when it may: fn is not visible from the package being
// checked, its visibility capped by its type's, as a field's is.
func (c *Checker) hiddenMethod(fn *ast.Func) string {
	owner := c.declPkg[fn]
	if owner == nil || c.pkg == nil || fn.RecvType == nil {
		return ""
	}
	vis := fn.Vis.Capped(fn.RecvType.Vis)
	if ast.Visible(vis, owner, c.pkg) {
		return ""
	}
	if vis == ast.Lib {
		return ast.LibMessage(fn.Name, owner, c.pkg)
	}
	return fmt.Sprintf("%s is not pub in %s", fn.Name, c.typeName(fn.RecvType.Type))
}

// receiverWithMut reports m, the receiver mut x of call, a call to fn,
// a method whose receiver is plain, after checking x for errors of its
// own unless checked says receiverType has: the fix is to leave out mut.
func (c *Checker) receiverWithMut(call *ast.Call, m *ast.MutArg, fn *ast.Func, checked bool) {
	if !checked {
		c.expr(m.X)
	}
	plain := ast.Span{File: call.Span.File, Start: m.X.Spanned().Start, End: call.Span.End}
	c.errorf(
		m.Span,
		"%s does not take its receiver as mut; call %s",
		c.displayName(fn),
		c.text(plain),
	)
}

// receiverWithoutMut reports call, whose callee sel names fn, a method with
// a mut receiver, called on a receiver written without mut: the fix is mut
// before the call when the receiver is a variable, a field or an element,
// and otherwise a place in its stead.
func (c *Checker) receiverWithoutMut(call *ast.Call, sel *ast.Selector, fn *ast.Func) {
	if ast.Root(sel.X) == nil {
		c.errorf(
			sel.X.Spanned(),
			"%s takes its receiver as mut; only a variable, a field or an element can be passed as mut",
			c.displayName(fn),
		)
		return
	}
	c.errorf(sel.X.Spanned(), "%s takes its receiver as mut; call mut %s",
		c.displayName(fn), c.text(call.Span))
}

// funcParams returns what checking an argument needs to know about each
// of ps, a function's parameters (see param).
func (c *Checker) funcParams(ps []*ast.Param) []param {
	params := make([]param, len(ps))
	for i, p := range ps {
		// A mut variadic was reported where it is declared; its values
		// are not asked to be mut too.
		params[i] = param{
			name:     p.Name,
			typ:      p.Type,
			mut:      p.Mut && !p.Variadic,
			variadic: p.Variadic,
		}
	}
	return params
}

// displayName names fn in a diagnostic: a function by its name, and a method
// by its receiver's type and its name, Builder.write, the type named as
// typeName names it, so lexer.Token.kind for a method of another package's
// type.
func (c *Checker) displayName(fn *ast.Func) string {
	if !fn.Method {
		return fn.Name
	}
	if recv := fn.Recv(); recv != nil && recv.Type != types.Invalid {
		return c.typeName(recv.Type) + "." + fn.Name
	}
	if fn.RecvType != nil {
		return fn.RecvType.Name + "." + fn.Name
	}
	return fn.Name
}

// calleeName names what call calls, in a diagnostic: a function or a
// builtin by its name as written, and a method as displayName does. It
// returns false for a method call whose method was not found, which has
// been reported.
func (c *Checker) calleeName(call *ast.Call) (string, bool) {
	if call.Receiver() != nil {
		if call.Method == nil {
			return "", false
		}
		return c.displayName(call.Method), true
	}
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Qualified(), true
}

// callText returns how call's callee is written, for a diagnostic that
// suggests a call to it: a function's name as written, f or lexer.First,
// or a method call up to the method's name, as written, b.write or mut
// b.write.
func (c *Checker) callText(call *ast.Call) string {
	if sel, ok := call.Fn.(*ast.Selector); ok {
		return c.text(ast.Span{File: call.Span.File, Start: call.Span.Start, End: sel.NameSpan.End})
	}
	if id, ok := call.Fn.(*ast.Ident); ok {
		return id.Qualified()
	}
	return c.text(call.Fn.Spanned())
}

// hasMethodBinding reports whether fn's body binds the results of a
// method call, a, b := x.m(), at any depth: which method that calls, and
// so whether it returns an error that holds what the binding binds beside
// it, is known only once the receiver is checked (see checkBody).
func hasMethodBinding(fn *ast.Func) bool {
	found := false
	for _, stmt := range fn.Body {
		ast.Walk(stmt, func(n ast.Node) bool {
			var value ast.Expr
			switch s := n.(type) {
			case *ast.MultiVar:
				value = s.Value
			case *ast.MultiAssign:
				value = s.Value
			}
			if call, ok := value.(*ast.Call); ok && call.Receiver() != nil {
				found = true
			}
			return !found
		})
	}
	return found
}

// nothingBound reports a binding at span of value, a method call whose
// results checked as valid, that binds nothing, every name, blank says, _,
// when the method returns no error: the call can be made on its own. The
// resolver reports such a binding of a function's call; which method a
// method call calls is known only here. One that drops an error was
// reported as dropping it.
func (c *Checker) nothingBound(value ast.Expr, blank []bool, span ast.Span) {
	call, ok := value.(*ast.Call)
	if !ok || call.Receiver() == nil {
		return
	}
	for _, b := range blank {
		if !b {
			return
		}
	}
	if _, isError := returnsError(call); isError {
		return
	}
	c.errorf(span, "every name is _, so nothing is bound; make the call on its own")
}
