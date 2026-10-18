package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// Own parameters. A parameter written own p T takes its argument's value
// from the caller, which writes own x, as mut is written at both ends: the
// caller's variable is moved into the call, and cannot be used again until
// it is assigned a new value, and the function owns the value, an
// immutable local it frees when it ends, at every exit, unless it moves it
// on first: returns it, stores it, passes it as own again, or moves it
// into a mutable variable, mut u := p, to change it. A plain parameter of
// an owned type only borrows; own is what lets a function keep what it is
// given.
//
// An own parameter's type must be owned, a list, a map, a struct, array
// or union holding one, or a type parameter, which stands for any of
// them: a copyable value is only ever copied, so own on one is reported,
// and so is own on a view, an any, a variadic parameter, a method's
// receiver or an extern func's parameter, where C would have to free it.
//
// own before an argument names the variable whose value moves: a local,
// or an own parameter of the caller, which owns its value as a local does.
// A plain parameter borrows its value and a mut parameter's stays its
// caller's, so neither can be passed as own (see move.go), and a field or
// an element cannot be moved out of what holds it. A value no variable
// holds, a call's result or a literal, is passed to an own parameter with
// no own, since nothing else could use it again; and so is a value whose
// type owns nothing, which a generic function's own parameter may be
// given, its type argument a copyable one: it is copied, and own on it is
// reported. Passing a variable as own to a parameter that is not own, or
// a variable of an owned type to an own parameter without own, is
// reported, as mut is.
//
// A variable passed as own leaves its caller when the call begins, so
// nothing else in the same call may name it: no other argument, at any
// depth, a call nested in one included, no receiver and no function value
// being called. A list is not copied for a call, so an argument that read
// it would read a buffer the callee may grow or free (see
// checkOwnExclusive).

// ownParamType returns typ, the type param, an own parameter, is declared
// with, or types.Invalid, after reporting it, when own cannot be written
// on it: a variadic parameter, a view, an any, or a type that owns
// nothing.
func (c *Checker) ownParamType(param *ast.Param, typ types.Type) types.Type {
	if typ == types.Invalid {
		return typ
	}
	switch {
	case param.Variadic:
		c.errorf(param.OwnSpan, "a variadic parameter cannot be own; take a list as own instead")
	case c.table.IsView(typ):
		c.errorf(param.OwnSpan,
			"a view cannot be an own parameter: it only borrows what it points into")
	case typ == types.Any:
		c.errorf(param.OwnSpan,
			"an any cannot be an own parameter: it only borrows the value it holds")
	case !c.table.Owned(typ):
		c.errorf(param.OwnSpan,
			"%s cannot be an own parameter: it owns no list or map, so it is copied; drop own",
			c.typeName(typ))
	default:
		return typ
	}
	return types.Invalid
}

// ownReceiver reports fn's receiver when it is written own, which a method
// cannot take.
func (c *Checker) ownReceiver(fn *ast.Func) {
	if fn.Method && len(fn.Params) > 0 && fn.Params[0].Own {
		c.errorf(fn.Params[0].OwnSpan,
			"a method cannot take its receiver as own; take it as a plain or a mut receiver")
	}
}

// checkExternOwn reports each own parameter of fn, an extern func, since
// C code is not held to free what it is given, and returns whether it
// found none.
func (c *Checker) checkExternOwn(fn *ast.Func) bool {
	ok := true
	for _, p := range fn.Params {
		if p.Own {
			c.errorf(
				p.OwnSpan,
				"parameter %s of extern func %s cannot be own: C is not held to free what it is given",
				p.Name,
				fn.Name,
			)
			ok = false
		}
	}
	return ok
}

// ownArg checks how arg, passed to p in a call to callee, is written
// against whether p is own, and returns what is checked as the argument's
// value in its stead: the variable an own argument names, or arg itself.
// It returns false, having checked what arg holds for errors of its own,
// when it reported arg: own before an argument to a parameter that is not
// own, or before anything but a variable (see ownName), or a variable of
// an owned type passed to an own parameter without own.
func (c *Checker) ownArg(arg ast.Expr, p param, callee string) (ast.Expr, bool) {
	o, isOwn := arg.(*ast.OwnArg)
	switch {
	case isOwn && !p.own:
		c.expr(o.X)
		text := c.text(o.X.Spanned())
		c.errorf(o.Span, "%s does not take %s as own; pass %s", callee, text, text)
		return nil, false
	case isOwn:
		return o.X, c.ownName(o)
	case p.own:
		if id, named := arg.(*ast.Ident); named && c.table.Owned(declType(id.Def)) {
			c.expr(arg)
			c.errorf(arg.Spanned(), "%s takes %s as own; pass own %s", callee, p.name, id.Name)
			return nil, false
		}
	}
	return arg, true
}

// ownName checks o, an own argument passed to an own parameter, after
// which only its variable's value is checked, and reports whether it can
// be: what follows own must be the name of a variable or a parameter, of
// a type that owns something, since a value that owns nothing is copied.
// A field, an element and any other value are reported, after checking
// them for errors of their own.
func (c *Checker) ownName(o *ast.OwnArg) bool {
	text := c.text(o.Span)
	switch x := o.X.(type) {
	case *ast.Ident:
		switch x.Def.(type) {
		case *ast.Var, *ast.Param:
		default:
			c.expr(x)
			c.errorf(o.Span, "cannot pass %s: only a variable can be passed as own", text)
			return false
		}
		typ := declType(x.Def)
		if typ != types.Invalid && !c.table.Owned(typ) {
			c.expr(x)
			c.errorf(o.Span, "cannot pass %s: %s owns no list or map, so it is copied; pass %s",
				text, c.typeName(typ), x.Name)
			return false
		}
		return true
	case *ast.Selector:
		c.expr(x)
		c.errorf(o.Span, "cannot pass %s: a field cannot be moved on its own", text)
	case *ast.Index:
		c.expr(x)
		c.errorf(o.Span, "cannot pass %s: an element cannot be moved out of what holds it", text)
	default:
		c.expr(x)
		c.errorf(
			o.Span,
			"cannot pass %s: only a variable can be passed as own; pass %s without own",
			text,
			c.text(x.Spanned()),
		)
	}
	return false
}

// declType returns the type the variable or parameter d is declared with,
// and types.Invalid for anything else or one not yet known.
func declType(d ast.Decl) types.Type {
	switch x := d.(type) {
	case *ast.Var:
		return x.Type
	case *ast.Param:
		return x.Type
	default:
		return types.Invalid
	}
}

// argElsewhere reports e, a mut or an own argument written anywhere but
// as a call's argument (see mutElsewhere and ownElsewhere).
func (c *Checker) argElsewhere(e ast.Expr) types.Type {
	if m, ok := e.(*ast.MutArg); ok {
		return c.mutElsewhere(m)
	}
	return c.ownElsewhere(e.(*ast.OwnArg))
}

// ownElsewhere reports an own argument written anywhere but as a call's
// argument, after checking what it holds for errors of its own.
func (c *Checker) ownElsewhere(o *ast.OwnArg) types.Type {
	c.expr(o.X)
	c.errorf(o.Span, "%s can only be passed to an own parameter", c.text(o.Span))
	return types.Invalid
}

// checkOwnExclusive reports every name in call, its function, receiver
// and arguments, at any depth, of a variable that an own argument
// anywhere in it passes, with a note at that own argument: the variable
// leaves for the callee as its call begins, and a list is never copied
// for a call, so anything else reading it would read what the callee may
// grow or free. A variable passed as own twice is reported at the second.
// A nested call reports first, being checked first, and an enclosing one
// does not report the same name again.
func (c *Checker) checkOwnExclusive(call *ast.Call) {
	owns := ownArgsIn(call)
	if len(owns) == 0 {
		return
	}
	ast.Walk(call, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Def != nil && !c.ownReported[id.Span] {
			if o := ownedElsewhere(id, owns); o != nil {
				c.ownReported[id.Span] = true
				c.errorNotes(
					id.Span,
					[]diag.Diagnostic{note(o.Span, id.Name+" is passed as own here")},
					"cannot use %s alongside %s in this call: it is moved into the call, "+
						"and nothing else in the call may reach it",
					id.Name,
					c.text(o.Span),
				)
			}
		}
		return true
	})
}

// ownArgsIn returns every own argument in call, at any depth, whose
// operand is a name, in the order they are written.
func ownArgsIn(call *ast.Call) []*ast.OwnArg {
	var owns []*ast.OwnArg
	ast.Walk(call, func(n ast.Node) bool {
		if o, ok := n.(*ast.OwnArg); ok {
			if _, named := o.X.(*ast.Ident); named {
				owns = append(owns, o)
			}
		}
		return true
	})
	return owns
}

// ownedElsewhere returns the own argument of owns, every one in a call
// whose operand is a name, that passes the variable id names when id is
// another name of it, and nil when there is none. When id is itself
// passed as own, only an earlier own argument counts, so of two the
// second is the one reported.
func ownedElsewhere(id *ast.Ident, owns []*ast.OwnArg) *ast.OwnArg {
	isOwned := false
	for _, o := range owns {
		isOwned = isOwned || o.X == ast.Expr(id)
	}
	for _, o := range owns {
		x := o.X.(*ast.Ident)
		if x == id || x.Def != id.Def || isOwned && o.Span.Start > id.Span.Start {
			continue
		}
		return o
	}
	return nil
}
