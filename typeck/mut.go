package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// A parameter written mut p T is a mutable reference to the caller's
// variable for the duration of a call: the callee reads and writes the
// caller's variable through p, p.x or p[i]. The call passes it as
// mut x, and only to a mut parameter: a mut parameter takes nothing else,
// and a plain parameter never takes mut x. What follows mut must be a
// place the caller may change: a mutable local, a mut parameter, a field
// of one, or an element of one at an index proven in range, nested in any
// combination. A mutable global is reachable from every function, so it is
// never passed as mut; an immutable variable, a plain parameter, a
// string's byte and a computed value are not places that can change. The
// argument's type is the parameter's exactly: a reference to an int32
// cannot stand for one to an int64, as widening a value could. A mut
// written anywhere but before a call's argument is an error.
//
// A reference exists only as a parameter. Nothing can store one: q := p
// copies the caller's value, and return p returns a copy. Passing p on as
// mut p to another call is how a reference reaches further.

// param is what checking an argument needs to know about the parameter
// it is passed to: its name, its type, and whether it is mut, or
// variadic, the last parameter, args ...T, whose type is the view []T.
type param struct {
	name     string
	typ      types.Type
	mut      bool
	variadic bool
}

// calleeParams returns the parameters of the function or builtin id
// names, none when it names neither.
func (c *Checker) calleeParams(id *ast.Ident) []param {
	if fn, ok := id.Def.(*ast.Func); ok && fn != nil {
		params := make([]param, len(fn.Params))
		for i, p := range fn.Params {
			// A mut variadic was reported where it is declared; its
			// values are not asked to be mut too.
			params[i] = param{
				name:     p.Name,
				typ:      p.Type,
				mut:      p.Mut && !p.Variadic,
				variadic: p.Variadic,
			}
		}
		return params
	}
	fn, _ := c.builtins.Lookup(id.Name)
	params := make([]param, len(fn.Params))
	for i, typ := range fn.Params {
		params[i] = param{typ: typ}
	}
	return params
}

// checkArg checks arg, passed to p in a call to callee: a mut argument to
// a mut parameter must name a place of exactly its type, and any other
// argument must be usable as its plain parameter's type, or be what
// viewArg allows a view parameter, or anyArg an any parameter, to take. A
// mut argument to a plain parameter, or a plain one to a mut parameter, is
// reported, and what it holds is still checked for errors of its own. It returns the mut
// argument when arg is one passed to a mut parameter with nothing
// reported, for the exclusivity check, and nil otherwise.
func (c *Checker) checkArg(arg ast.Expr, p param, callee string) *ast.MutArg {
	m, isMut := arg.(*ast.MutArg)
	switch {
	case p.mut && isMut:
		return c.mutToMut(m, p, callee)
	case p.mut:
		c.expr(arg)
		c.missingMut(arg, p, callee)
	case isMut:
		c.expr(m.X)
		text := c.text(m.X.Spanned())
		c.errorf(m.Span, "%s does not take %s as mut; pass %s", callee, text, text)
	case c.table.IsView(p.typ):
		c.viewArg(arg, p.typ, callee)
	case p.typ == types.Any:
		c.anyArg(arg)
	default:
		c.use(arg, c.expr(arg), p.typ, "in argument to "+callee)
	}
	return nil
}

// mutToMut checks m, a mut argument passed to the mut parameter p of
// callee: its place must be one of exactly p's type. It returns m when
// nothing was reported, and nil otherwise.
func (c *Checker) mutToMut(m *ast.MutArg, p param, callee string) *ast.MutArg {
	got := c.place(m.X, m)
	switch {
	case got == types.Invalid:
		return nil
	case p.typ != types.Invalid && got != p.typ:
		c.errorf(m.X.Spanned(), "cannot use %s as %s in argument to %s",
			c.typeName(got), c.typeName(p.typ), callee)
		return nil
	default:
		return m
	}
}

// missingMut reports arg, a plain argument passed to the mut parameter p
// of callee: the fix is mut before it when it is a variable, a field or an
// element, and otherwise a place in its stead.
func (c *Checker) missingMut(arg ast.Expr, p param, callee string) {
	if ast.Root(arg) == nil {
		c.errorf(arg.Spanned(),
			"%s takes %s as mut; only a variable, a field or an element can be passed as mut",
			callee, p.name)
		return
	}
	c.errorf(
		arg.Spanned(),
		"%s takes %s as mut; pass mut %s",
		callee,
		p.name,
		c.text(arg.Spanned()),
	)
}

// argExpr checks an argument of a call whose argument count is wrong,
// which is matched with no parameter: only for errors of its own, so a
// mut argument is not reported as passed to a plain parameter.
func (c *Checker) argExpr(arg ast.Expr) {
	if m, ok := arg.(*ast.MutArg); ok {
		c.expr(m.X)
		return
	}
	c.expr(arg)
}

// place checks e, the place the mut argument m names, or part of it, and
// returns its type: a mutable local or a mut parameter, a field of a
// place, or an element of a place whose index is proven in range. Anything
// else, a slice included, is reported, and gives types.Invalid.
func (c *Checker) place(e ast.Expr, m *ast.MutArg) types.Type {
	switch x := e.(type) {
	case *ast.Ident:
		return c.placeName(x, m)
	case *ast.Selector:
		base := c.place(x.X, m)
		if base == types.Invalid {
			return types.Invalid
		}
		x.Type = c.field(x, base)
		return x.Type
	case *ast.Index:
		base := c.place(x.X, m)
		switch base {
		case types.Invalid:
			c.expr(x.Index)
			return types.Invalid
		case types.String:
			c.expr(x.Index)
			c.errorf(m.Span, "cannot pass %s: strings are immutable", c.text(m.Span))
			return types.Invalid
		}
		x.Type = c.element(x, base, true)
		return x.Type
	case *ast.Slice:
		c.notPlace(m.Span, "pass "+c.text(m.Span))
		return types.Invalid
	default:
		// Something already reported has no type, and is not reported
		// again as not being a place.
		if c.expr(e) != types.Invalid {
			c.errorf(m.Span, "only a variable, a field or an element can be passed as mut")
		}
		return types.Invalid
	}
}

// placeName checks the variable or parameter id names, where a mut
// argument m starts: a mutable local or a mut parameter, whose type it
// returns. A mutable global, an immutable variable and a plain parameter
// are reported, and give types.Invalid.
func (c *Checker) placeName(id *ast.Ident, m *ast.MutArg) types.Type {
	typ := c.ident(id)
	switch def := id.Def.(type) {
	case *ast.Param:
		if def.Mut {
			return typ
		}
		if c.table.IsView(def.Type) {
			c.readOnly(m.Span, "pass "+c.text(m.Span), id)
			return types.Invalid
		}
		c.errorf(
			m.Span,
			"cannot pass %s: %s is immutable; declare it with mut",
			c.text(m.Span),
			id.Name,
		)
	case *ast.Var:
		return c.placeVar(id, def, typ, m)
	default:
		if typ != types.Invalid {
			c.errorf(m.Span, "only a variable, a field or an element can be passed as mut")
		}
	}
	return types.Invalid
}

// placeVar checks def, the variable id names, of type typ, where a mut
// argument m starts: a mutable local, whose type it returns. A mutable
// global and an immutable variable are reported, and give types.Invalid.
func (c *Checker) placeVar(id *ast.Ident, def *ast.Var, typ types.Type, m *ast.MutArg) types.Type {
	switch {
	case def.Mutable && c.globals[def]:
		c.errorf(m.Span, "a mutable global cannot be passed as mut; copy it into a local")
	case def.Mutable:
		return typ
	case c.globals[def]:
		c.errorf(m.Span, "cannot pass %s: %s is immutable; copy it into a mutable local",
			c.text(m.Span), id.Qualified())
	default:
		c.errorf(
			m.Span,
			"cannot pass %s: %s is immutable; declare it with mut",
			c.text(m.Span),
			id.Name,
		)
	}
	return types.Invalid
}

// mutElsewhere reports a mut argument written anywhere but as a call's
// argument, after checking what it holds for errors of its own. mut
// written several times in a row, mut mut x, is one error.
func (c *Checker) mutElsewhere(m *ast.MutArg) types.Type {
	x := m.X
	for {
		inner, ok := x.(*ast.MutArg)
		if !ok {
			break
		}
		x = inner.X
	}
	c.expr(x)
	c.errorf(m.Span, "%s can only be passed to a mut parameter", c.text(m.Span))
	return types.Invalid
}
