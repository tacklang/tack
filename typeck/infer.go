package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// A generic function's parameter may be made of its type parameters, not
// only be one: l list[T], v []T, m map[string]T or f func(T, T) bool. A
// call infers each type parameter from its arguments, in order, matching
// each argument's type against its parameter's, part by part (unify): a
// list's element against a list's, a view's against the element of the
// array, list or view the argument is, a map's value against a map's, its
// key being no type parameter's, and a function type's parameters and
// results against those of the function value passed. A parameter all of
// whose type parameters an earlier argument inferred is checked as a
// parameter of the type it is with them in place (substitute), as any
// argument is. Once every type parameter is inferred, the call records its
// callee's parameters' and results' types with them in place, which is
// what its results are (see ast.CallResults) and what code generation
// passes its arguments as.

// mentions reports whether the type parameter tp is typ, or a part of it:
// an array's, a view's, a list's, an arena's, an id's, a span's, a
// pool's or a handle's element, a map's key or value, or a function
// type's parameter or result, at any depth.
func (c *Checker) mentions(typ, tp types.Type) bool {
	switch {
	case typ == tp:
		return true
	case c.table.IsArray(typ), c.table.IsView(typ), c.table.IsList(typ), c.table.IsArena(typ),
		c.table.IsRef(typ), c.table.IsPool(typ), c.table.IsHandle(typ):
		return c.mentions(c.table.Elem(typ), tp)
	case c.table.IsMap(typ), c.table.IsFunc(typ):
		for _, part := range c.funcParts(typ) {
			if c.mentions(part, tp) {
				return true
			}
		}
	}
	return false
}

// mentionsAny reports whether the inference being made has a type
// parameter typ mentions.
func (inf *inference) mentionsAny(c *Checker, typ types.Type) bool {
	if inf == nil {
		return false
	}
	for _, tp := range inf.fn.TypeParams {
		if c.mentions(typ, tp.Type) {
			return true
		}
	}
	return false
}

// lost reports whether typ mentions a type parameter of the callee being
// inferred whose type argument the call wrote and was reported, so it has
// none, and no argument may give it one.
func (inf *inference) lost(c *Checker, typ types.Type) bool {
	for _, tp := range inf.fn.TypeParams {
		if _, ok := inf.args[tp.Type]; !ok && inf.written[tp.Type] && c.mentions(typ, tp.Type) {
			return true
		}
	}
	return false
}

// complete reports whether every type parameter of the callee being
// inferred that typ mentions has a type argument already.
func (inf *inference) complete(c *Checker, typ types.Type) bool {
	for _, tp := range inf.fn.TypeParams {
		if _, ok := inf.args[tp.Type]; !ok && c.mentions(typ, tp.Type) {
			return false
		}
	}
	return true
}

// substitute returns typ with each type parameter args has a type for
// replaced by it, through every array, view, list, arena, id, span, pool,
// handle, map and function type
// typ is made of, adding each type it makes to the table. A type
// parameter args has no type for is left as it is.
func (c *Checker) substitute(typ types.Type, args map[types.Type]types.Type) types.Type {
	if to, ok := args[typ]; ok {
		return to
	}
	switch {
	case c.table.IsArray(typ):
		return c.table.Array(c.substitute(c.table.Elem(typ), args), c.table.Len(typ))
	case c.table.IsView(typ):
		return c.table.View(c.substitute(c.table.Elem(typ), args))
	case c.table.IsList(typ):
		return c.table.List(c.substitute(c.table.Elem(typ), args))
	case c.table.IsArena(typ):
		return c.table.Arena(c.substitute(c.table.Elem(typ), args))
	case c.table.IsID(typ):
		return c.table.ID(c.substitute(c.table.Elem(typ), args))
	case c.table.IsSpan(typ):
		return c.table.Span(c.substitute(c.table.Elem(typ), args))
	case c.table.IsPool(typ):
		return c.table.Pool(c.substitute(c.table.Elem(typ), args))
	case c.table.IsHandle(typ):
		return c.table.Handle(c.substitute(c.table.Elem(typ), args))
	case c.table.IsMap(typ):
		return c.table.Map(
			c.substitute(c.table.Key(typ), args),
			c.substitute(c.table.Elem(typ), args),
		)
	case c.table.IsFunc(typ):
		params := make([]types.FuncParam, len(c.table.FuncParams(typ)))
		for i, p := range c.table.FuncParams(typ) {
			params[i] = types.FuncParam{
				Type: c.substitute(p.Type, args), Mut: p.Mut, Own: p.Own, Scope: p.Scope,
			}
		}
		results := make([]types.Type, len(c.table.FuncResults(typ)))
		for i, r := range c.table.FuncResults(typ) {
			results[i] = c.substitute(r, args)
		}
		return c.table.Func(params, results)
	default:
		return typ
	}
}

// inferArg checks arg, passed to p, a parameter of the generic callee
// being inferred, named callee, whose type mentions one of its type
// parameters. A parameter that is one is typeArg's. One every type
// parameter it mentions has a type argument for already is checked as a
// parameter of that type with them in place, as checkArg checks any. Any
// other gives what it mentions their type arguments from arg's type (see
// unify), and is then checked against its type with them in place. It
// returns the mut argument when arg is one passed to a mut parameter with
// nothing reported, for the exclusivity check.
func (c *Checker) inferArg(arg ast.Expr, p param, callee string) *ast.MutArg {
	inf := c.inference
	if inf.lost(c, p.typ) {
		// What arg must be is unknown: its type argument was reported.
		c.argExpr(arg)
		return nil
	}
	if inf.infers(p.typ) && !inf.written[p.typ] {
		return c.typeArg(arg, p, callee)
	}
	if inf.complete(c, p.typ) {
		p.typ = c.substitute(p.typ, inf.args)
		c.inference = nil
		m := c.checkArg(arg, p, callee)
		c.inference = inf
		return m
	}
	return c.inferFrom(arg, p, callee)
}

// inferFrom checks arg, passed to p, a parameter of the generic callee
// being inferred, named callee, whose type mentions a type parameter not
// inferred yet, as inferArg says: a mut argument as a place (inferMut), a
// view parameter's as a view's (inferView), and any other as a value,
// reporting a mut argument to a plain parameter or a plain one to a mut
// parameter as checkArg does.
func (c *Checker) inferFrom(arg ast.Expr, p param, callee string) *ast.MutArg {
	arg, ok := c.ownArg(arg, p, callee)
	if !ok {
		return nil
	}
	m, isMut := arg.(*ast.MutArg)
	switch {
	case p.mut && isMut:
		c.checkErrorMut(m)
		return c.inferMut(m, p, callee)
	case p.mut:
		c.expr(arg)
		c.missingMut(arg, p, callee)
	case isMut:
		c.expr(m.X)
		text := c.text(m.X.Spanned())
		c.errorf(m.Span, "%s does not take %s as mut; pass %s", callee, text, text)
	case c.table.IsView(p.typ):
		c.inferView(arg, p, callee)
	default:
		got := c.valueOf(arg, c.expr(arg))
		if got != types.Invalid && c.unify(arg, p.typ, got, callee) {
			c.use(arg, got, c.substitute(p.typ, c.inference.args), "in argument to "+callee)
		}
	}
	return nil
}

// inferMut checks m, a mut argument passed to p, a mut parameter of the
// generic callee being inferred, named callee, whose type mentions a type
// parameter not yet inferred: m's place gives them their type arguments,
// and must then be of exactly p's type with them in place. It returns m
// when nothing was reported, and nil otherwise.
func (c *Checker) inferMut(m *ast.MutArg, p param, callee string) *ast.MutArg {
	got := c.place(m.X, m)
	if got == types.Invalid || !c.unify(m, p.typ, got, callee) {
		return nil
	}
	if want := c.substitute(p.typ, c.inference.args); got != want {
		c.errorf(m.X.Spanned(), "cannot use %s as %s in argument to %s",
			c.typeName(got), c.typeName(want), callee)
		return nil
	}
	return m
}

// inferView checks arg, passed to p, a view parameter of the generic
// callee being inferred, named callee, whose element type mentions a type
// parameter not yet inferred: the element type of the array, the list or
// the view arg is gives them their type arguments, and arg must then be
// what a view parameter of p's type with them in place takes (see
// viewArg). A string, which a []uint8 parameter takes only converted, and
// any other value is reported as viewArg reports it.
func (c *Checker) inferView(arg ast.Expr, p param, callee string) {
	got, conv := c.viewArgValue(arg)
	if got == types.Invalid {
		return
	}
	if !c.viewable(got) && !c.table.IsView(got) {
		c.errorf(arg.Spanned(), "cannot use %s as %s in argument to %s",
			c.typeName(got), c.typeName(p.typ), callee)
		return
	}
	if !c.unify(arg, c.table.Elem(p.typ), c.table.Elem(got), callee) {
		return
	}
	c.viewArgFits(arg, got, c.substitute(p.typ, c.inference.args), callee, conv)
}

// unify matches got, the type of arg, passed in a call to the generic
// callee being inferred, named callee, against want, a type made of the
// callee's type parameters, part by part, and gives each type parameter
// want has where got has a type that type as its type argument (see
// bindTypeArg). A part of want with no type parameter of the callee in it
// is left for the argument's check to compare. A part got lacks, a list
// where want has a map, a map of another key type, or a function type of
// another arity or other mut, own or scope parameters, is reported, as is
// a type argument bindTypeArg rejects, and gives false. A scope mark, like
// mut and own, belongs to a function type's parameter, never to the type a
// type parameter stands for, so no type argument can supply one, and a
// function type of other scope parameters is reported with want as
// written.
func (c *Checker) unify(arg ast.Expr, want, got types.Type, callee string) bool {
	inf := c.inference
	switch {
	case inf.infers(want):
		return c.bindTypeArg(arg, want, got, callee, false)
	case !inf.mentionsAny(c, want):
		return true
	}
	wants, gots, ok := c.matchingParts(want, got)
	if !ok {
		c.errorf(argPlace(arg).Spanned(), "cannot use %s as %s in argument to %s",
			c.typeName(got), c.typeName(want), callee)
		return false
	}
	for i := range wants {
		if !c.unify(arg, wants[i], gots[i], callee) {
			return false
		}
	}
	return true
}

// matchingParts returns the parts of want and of got that unify matches
// against each other, when the two are made the same way: two lists' or
// two views' elements, two maps' values when their keys are of one type,
// or two function types' parameters and results when they are of one
// shape (see sameShape). It returns false when they are not.
func (c *Checker) matchingParts(want, got types.Type) (wants, gots []types.Type, ok bool) {
	if c.sameElemKind(want, got) {
		return []types.Type{c.table.Elem(want)}, []types.Type{c.table.Elem(got)}, true
	}
	if c.table.IsFunc(want) && c.table.IsFunc(got) && c.sameShape(want, got) {
		return c.funcParts(want), c.funcParts(got), true
	}
	return nil, nil, false
}

// sameElemKind reports whether want and got are two lists, two views, two
// arenas, two ids, two spans, two pools, two handles, or two maps of one
// key type, whose elements or values unify matches.
func (c *Checker) sameElemKind(want, got types.Type) bool {
	switch {
	case c.table.IsList(want):
		return c.table.IsList(got)
	case c.table.IsArena(want):
		return c.table.IsArena(got)
	case c.table.IsID(want):
		return c.table.IsID(got)
	case c.table.IsSpan(want):
		return c.table.IsSpan(got)
	case c.table.IsPool(want):
		return c.table.IsPool(got)
	case c.table.IsHandle(want):
		return c.table.IsHandle(got)
	case c.table.IsView(want):
		return c.table.IsView(got)
	case c.table.IsMap(want):
		return c.table.IsMap(got) && c.table.Key(want) == c.table.Key(got)
	default:
		return false
	}
}

// sameShape reports whether the function types a and b have as many
// parameters, mut, own and scope at the same positions, and as many
// results.
func (c *Checker) sameShape(a, b types.Type) bool {
	pa, pb := c.table.FuncParams(a), c.table.FuncParams(b)
	if len(pa) != len(pb) || len(c.table.FuncResults(a)) != len(c.table.FuncResults(b)) {
		return false
	}
	for i := range pa {
		if pa[i].Mut != pb[i].Mut || pa[i].Own != pb[i].Own || pa[i].Scope != pb[i].Scope {
			return false
		}
	}
	return true
}

// argPlace returns arg, or the place a mut argument names, where a
// diagnostic about its type points.
func argPlace(arg ast.Expr) ast.Expr {
	if m, ok := arg.(*ast.MutArg); ok {
		return m.X
	}
	return arg
}

// recordSignature records on call, a call to the generic function fn whose
// type arguments args every type parameter has, its parameters' and
// results' types with them in place (see ast.Call.ParamTypes).
func (c *Checker) recordSignature(call *ast.Call, fn *ast.Func, args map[types.Type]types.Type) {
	call.ParamTypes = make([]types.Type, len(fn.Params))
	for i, p := range fn.Params {
		call.ParamTypes[i] = c.substitute(p.Type, args)
	}
	call.ResultTypes = make([]types.Type, len(fn.ResultTypes))
	for i, r := range fn.ResultTypes {
		call.ResultTypes[i] = c.substitute(r, args)
	}
}
