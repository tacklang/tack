package typeck

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// A function type, func(A, mut B) R, is the type of a value that names a
// function: a package-level function's name, f or pkg.f, used anywhere
// but as a call's callee, is a value of its type, its parameters' types,
// each mut where it is, and its result types, in order. Two functions of
// one signature have one type. Such a value is copyable and owns nothing:
// it can be passed, returned, held by a variable, a field, an element of
// an array, a list or a view, a map's value or a union's payload, and
// called through a name, f(x), where f is a variable or a parameter
// holding one. The call is checked against the value's type exactly as a
// call by name is checked against its function's signature, through a
// stand-in function with no body (see Call.Dynamic): its arguments' count
// and types, mut arguments to mut parameters, views, borrows, the
// exclusivity of mut arguments, moves into owned parameters, owned and
// error results, and the order its operands are evaluated in, the value
// first, then its arguments, left to right.
//
// A function type has no zero value, so nothing holding one is declared
// without a value, and the checked forms that would give a zero value
// cannot read one (see types.Table.HasZero). It has no ==, and no other
// operator, so it is no map's key, and it is not one of the types an any
// can hold. Nothing that holds one crosses to C: an extern func can take
// or return none, at any depth (checkExternFuncs). A package-level
// variable cannot hold one either: a global's value is a constant, known
// before any function's signature is (globalFuncValue).
//
// What a function type can say is what a value of it can be called
// with, so it is narrower than what a function can declare: no variadic
// parameter, no any parameter, no view result, and no type parameter
// anywhere in it, since nothing instantiates one. A function whose
// signature has any of those, a generic function, a method and an extern
// func is not a value (funcValue).
//
// The call graph is no longer fixed by the source: a call through a
// value calls whichever function the value names when it runs. Nothing
// the checker proves depends on the callee of a call beyond its
// signature, which the value's type gives, and no proof survives a call
// that could change what it is about but through a mut argument, which
// the call shows: a fact is never about a mutable global (see facts.go).
// Unbounded recursion stays a known violation (docs/guarantees.md),
// through a value as by name.

// funcType returns the function type a written func(A, mut B) R denotes,
// adding it to the table. A parameter may have any type a function's
// parameter may but any, and a view only when it is not mut; a result any
// type a function's result may but a view; an error only as the last
// result; and nothing in it may be a type parameter. Anything else is
// reported, and gives types.Invalid, as does a part that was reported.
func (c *Checker) funcType(ft *ast.FuncType) types.Type {
	ok := true
	params := make([]types.FuncParam, len(ft.Params))
	for i, p := range ft.Params {
		typ := c.funcTypeParam(p)
		ok = ok && typ != types.Invalid
		params[i] = types.FuncParam{Type: typ, Mut: p.Mut}
	}
	results := make([]types.Type, len(ft.Results))
	for i, r := range ft.Results {
		typ := c.funcTypeResult(r)
		ok = ok && typ != types.Invalid
		results[i] = typ
	}
	for i, typ := range results[:max(len(results)-1, 0)] {
		if typ == types.Error {
			c.errorf(ft.Results[i].Spanned(), "error must be the last result of a function type")
			ok = false
		}
	}
	if !ok {
		return types.Invalid
	}
	return c.table.Func(params, results)
}

// funcTypeParam returns the type p, a parameter of a function type, has:
// a view, which may not be mut, or any other type declaredType gives, but
// any or a type parameter, each reported.
func (c *Checker) funcTypeParam(p *ast.Param) types.Type {
	if isAnyName(p.TypeExpr) {
		c.errorf(p.TypeExpr.Spanned(), "a function type's parameter cannot be any")
		return types.Invalid
	}
	if vt, isView := p.TypeExpr.(*ast.ViewType); isView {
		typ := c.viewType(vt)
		if p.Mut && typ != types.Invalid {
			c.errorf(p.Span, "a view cannot be a mut parameter; views are read-only")
			return types.Invalid
		}
		return typ
	}
	return c.noTypeParam(p.TypeExpr, c.declaredType(p.TypeExpr))
}

// funcTypeResult returns the type r, a result of a function type,
// denotes: any type declaredType gives but a view or a type parameter,
// each reported.
func (c *Checker) funcTypeResult(r ast.Expr) types.Type {
	if vt, isView := r.(*ast.ViewType); isView {
		c.errorf(vt.Span, "a function type cannot return a view")
		return types.Invalid
	}
	return c.noTypeParam(r, c.declaredType(r))
}

// noTypeParam returns typ, the type written as written, unless it is a
// type parameter, which nothing in a function type may be: it is
// reported, and gives types.Invalid.
func (c *Checker) noTypeParam(written ast.Expr, typ types.Type) types.Type {
	if !c.table.IsTypeParam(typ) {
		return typ
	}
	c.errorf(written.Spanned(), "a function type cannot hold the type parameter %s",
		c.typeName(typ))
	return types.Invalid
}

// funcValue checks id, a name of the function fn used as a value, and
// returns the function's type, which it records on fn (see Func.Value).
// A builtin, whose fn is nil, was reported by the resolver. A generic
// function, an extern func, and one whose signature no function type can
// say, a variadic or any parameter or a view result, is reported; one
// whose signature has a type that was reported gives types.Invalid.
func (c *Checker) funcValue(id *ast.Ident, fn *ast.Func) types.Type {
	if fn == nil {
		return types.Invalid
	}
	if why := c.notValue(fn); why != "" {
		c.errorf(id.Span, "cannot use %s as a value: %s", id.Qualified(), why)
		return types.Invalid
	}
	if fn.Value != types.Invalid {
		return fn.Value
	}
	params := make([]types.FuncParam, len(fn.Params))
	for i, p := range fn.Params {
		if p.Type == types.Invalid {
			return types.Invalid
		}
		params[i] = types.FuncParam{Type: p.Type, Mut: p.Mut}
	}
	for _, typ := range fn.ResultTypes {
		if typ == types.Invalid {
			return types.Invalid
		}
	}
	fn.Value = c.table.Func(params, fn.ResultTypes)
	return fn.Value
}

// notValue returns why fn, a function, cannot be a value, or "" when it
// can.
func (c *Checker) notValue(fn *ast.Func) string {
	switch {
	case fn.Generic():
		return "it is generic, and a generic function is only called, " +
			"its type arguments inferred from the call"
	case fn.Extern:
		return "it is an extern func, and a function value does not cross to C"
	}
	for _, p := range fn.Params {
		switch {
		case p.Variadic:
			return fmt.Sprintf("its parameter %s is variadic, which no function type's can be",
				p.Name)
		case p.Type == types.Any:
			return fmt.Sprintf("its parameter %s is an any, which no function type's can be",
				p.Name)
		}
	}
	if len(fn.ResultTypes) == 1 && c.table.IsView(fn.ResultTypes[0]) {
		return "it returns a view, which no function type can"
	}
	return ""
}

// dynamicCall checks call, a call through id, a name of a variable or a
// parameter: it must hold a function value, and the call is then checked
// against the value's type as checkCall checks a call by name against
// its function's signature, through the stand-in it records in
// call.Dynamic. id is read as any value is, before the arguments.
func (c *Checker) dynamicCall(call *ast.Call, id *ast.Ident) {
	typ := c.expr(id)
	if typ != types.Invalid && !c.table.IsFunc(typ) {
		c.errorf(id.Span, "cannot call %s: it is %s, not a function", id.Qualified(),
			c.typeName(typ))
		typ = types.Invalid
	}
	if typ == types.Invalid {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return
	}
	call.Dynamic = c.dynamicFunc(typ)
	muts, counted := c.checkArgs(call, id.Qualified(), c.funcParams(call.Dynamic.Params))
	if !counted {
		return
	}
	c.checkExclusive(muts)
	c.checkListBorrows(call)
}

// dynamicFunc returns the stand-in for a call through a value of the
// function type typ: a function with no body whose parameters, named by
// their position, as an argument's message names them, and results are
// typ's, and whose Value is typ. Every call through a value of one type
// shares one.
func (c *Checker) dynamicFunc(typ types.Type) *ast.Func {
	if fn, ok := c.dynamics[typ]; ok {
		return fn
	}
	ps := c.table.FuncParams(typ)
	params := make([]*ast.Param, len(ps))
	for i, p := range ps {
		params[i] = &ast.Param{Name: fmt.Sprintf("argument %d", i+1), Type: p.Type, Mut: p.Mut}
	}
	fn := &ast.Func{
		Name:        c.typeName(typ),
		Params:      params,
		ResultTypes: append([]types.Type(nil), c.table.FuncResults(typ)...),
		Value:       typ,
		NoBody:      true,
	}
	c.dynamics[typ] = fn
	return fn
}

// isValueCall reports whether call calls through a name of a variable or
// a parameter, which holds the function it calls (see dynamicCall).
func isValueCall(call *ast.Call) bool {
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		return false
	}
	switch id.Def.(type) {
	case *ast.Var, *ast.Param:
		return true
	default:
		return false
	}
}

// hiddenValue reports id, a call's callee bound to the function or the
// builtin of its name, when the variable or the parameter of that name
// visible there, which the resolver passed over, holds a function value:
// a call by a bare name calls the function, so the call may mean the
// value instead, and one of the two must be renamed.
func (c *Checker) hiddenValue(id *ast.Ident) {
	if id.Hidden == nil {
		return
	}
	var typ types.Type
	var at ast.Span
	switch d := id.Hidden.(type) {
	case *ast.Var:
		typ, at = d.Type, d.NameSpan
	case *ast.Param:
		typ, at = d.Type, d.NameSpan
	}
	if !c.table.IsFunc(typ) {
		return
	}
	kind := "function"
	if fn, ok := id.Def.(*ast.Func); ok && fn == nil {
		kind = "builtin"
	}
	c.errorNotes(id.Span, []diag.Diagnostic{{
		Severity: diag.Note,
		Message:  fmt.Sprintf("the function value %s is declared here", id.Name),
		Span:     at,
	}}, "%s calls the %s %s, not the function value of that name; rename one of them",
		id.Name, kind, id.Name)
}

// holdsFunc reports whether typ is a function type, or holds one, at any
// depth, through a struct, an array, a union, a list, a view or a map's
// key or value. seen holds the structs and unions already being looked
// at.
func (c *Checker) holdsFunc(typ types.Type, seen map[types.Type]bool) bool {
	switch {
	case c.table.IsFunc(typ):
		return true
	case c.table.IsArray(typ), c.table.IsList(typ), c.table.IsView(typ):
		return c.holdsFunc(c.table.Elem(typ), seen)
	case c.table.IsMap(typ):
		return c.holdsFunc(c.table.Key(typ), seen) || c.holdsFunc(c.table.Elem(typ), seen)
	case (c.table.IsStruct(typ) || c.table.IsUnion(typ)) && !seen[typ]:
		seen[typ] = true
		for _, part := range c.partTypes(typ) {
			if c.holdsFunc(part, seen) {
				return true
			}
		}
	}
	return false
}

// checkExternFuncs reports each parameter and each result of fn, an
// extern func, whose type is or holds a function value, at any depth, and
// returns whether there was none: a function value does not cross to C.
func (c *Checker) checkExternFuncs(fn *ast.Func) bool {
	clean := true
	for _, p := range fn.Params {
		if c.holdsFunc(p.Type, make(map[types.Type]bool)) {
			c.errorf(
				p.TypeExpr.Spanned(),
				"parameter %s of extern func %s cannot be %s: a function value does not cross to C",
				p.Name,
				fn.Name,
				c.typeName(p.Type),
			)
			clean = false
		}
	}
	for i, typ := range fn.ResultTypes {
		if c.holdsFunc(typ, make(map[types.Type]bool)) {
			c.errorf(
				fn.Results[i].Spanned(),
				"extern func %s cannot return %s: a function value does not cross to C",
				fn.Name,
				c.typeName(typ),
			)
			clean = false
		}
	}
	return clean
}

// globalFuncValue reports v, a package-level variable, when its value
// names a function as a value, anywhere but as a call's callee, and
// returns whether it did. A global's value is a constant, checked before
// any function's signature is known, so it cannot hold one; checkGlobal
// reports a global whose type holds a function value too.
func (c *Checker) globalFuncValue(v *ast.Var) bool {
	if v.Value == nil || !namesFunc(v.Value) {
		return false
	}
	c.noGlobalFunc(v)
	if v.TypeExpr != nil {
		v.Type = c.declaredType(v.TypeExpr)
	}
	return true
}

// noGlobalFunc reports v, a package-level variable, as holding a function
// value, which none can.
func (c *Checker) noGlobalFunc(v *ast.Var) {
	c.errorf(v.NameSpan, "package-level variable %s cannot hold a function value", v.Name)
}

// namesFunc reports whether e names a function as a value anywhere in
// it: a name bound to a declared function, outside a call's callee.
func namesFunc(e ast.Expr) bool {
	found := false
	ast.Walk(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Call:
			for _, arg := range x.Args {
				found = found || namesFunc(arg)
			}
			if sel, ok := x.Fn.(*ast.Selector); ok {
				found = found || namesFunc(sel.X)
			}
			return false
		case *ast.Ident:
			if fn, ok := x.Def.(*ast.Func); ok && fn != nil {
				found = true
			}
		}
		return !found
	})
	return found
}

// funcParts returns the types a map type or a function type is made of: a
// map's key and value types, or a function type's parameters' types and
// its results, in order.
func (c *Checker) funcParts(typ types.Type) []types.Type {
	if c.table.IsMap(typ) {
		return []types.Type{c.table.Key(typ), c.table.Elem(typ)}
	}
	parts := make([]types.Type, 0, len(c.table.FuncParams(typ))+len(c.table.FuncResults(typ)))
	for _, p := range c.table.FuncParams(typ) {
		parts = append(parts, p.Type)
	}
	return append(parts, c.table.FuncResults(typ)...)
}
