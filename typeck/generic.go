package typeck

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// Interfaces and generic functions. An interface, type Writer interface {
// mut write(b []uint8) error; size() int64 }, lists methods, and is only
// ever a type parameter's constraint: as the whole type of a function's
// parameter it is shorthand for a type parameter of its own (see
// implicitTypeParams), and written as any other type, a variable's, a
// method's parameter's, a field's or an element's, it is reported.
//
// A generic function, func render[W Writer](mut w W, s string) error,
// declares type parameters, each a type of its own in the table (see
// types.Table.TypeParam) constrained by an interface. Its body is checked
// once, against the constraints, never per call: a value of a type
// parameter has the constraint's methods, called as a struct's are, and
// nothing else, no field, no ==, no zero value and no any, and it is owned
// as a list is, since it may stand for a struct holding one, so it moves
// rather than copies and a parameter of one borrows. So whatever a call
// passes, a body that checked once is right for it, int64 as much as a
// struct holding a list. A list, a view, a map's value and a function
// type may be made of one, and a result may be one: a body makes a value
// of one by moving it out of a list, or getting it from a function value
// or a call, and the rules for returns report a body that cannot.
//
// A call infers each type parameter from the arguments (see infer.go):
// any type but a view, an any or one made of a type parameter, so, in
// another generic function, one of its type parameters is passed on only
// whole (notTypeArg); it must satisfy the constraint, which a type that
// is no struct or union does only when the constraint asks for no
// method. Two arguments for one type parameter must be of one type. The
// inferred types are recorded in Call.TypeArgs, and the callee's
// parameters' and results' types with them in place in Call.ParamTypes
// and Call.ResultTypes (see ast.CallResults).
//
// A struct or union type satisfies an interface when, for each of the
// interface's methods, it has a method of that name taking its receiver as
// mut exactly when the interface's does, with the same parameters, each of
// the same type, mut or variadic as the interface's, and the same results.
// Where a call binds a type parameter to such a type, each of those methods
// must also be visible from the package holding the call, as if that code
// called it itself: private within its package, lib within its module, pub
// anywhere. The interface's package plays no part. A type parameter
// satisfies an interface when its own constraint has every method the
// interface asks for, alike in the same way, and is checked for no
// visibility: the call that bound the outermost type argument to a struct
// or union type checked every method of its constraint, and a constraint a
// type parameter passes on to can ask only for methods of that one, so
// whatever generic function the type argument reaches through a chain of
// calls, it can call no method that call did not check.
//
// Once every body is checked, the instances of generic functions the
// program needs are found (see instantiate in instance.go).

// inference is what a call to a generic function has inferred so far: its
// callee, and the type argument found for each of the callee's type
// parameters.
type inference struct {
	fn   *ast.Func
	args map[types.Type]types.Type
}

// infers reports whether typ is a type parameter of the callee whose call
// inf is inferring, which an argument passed to a parameter of that type
// gives a type argument.
func (inf *inference) infers(typ types.Type) bool {
	if inf == nil {
		return false
	}
	for _, tp := range inf.fn.TypeParams {
		if tp.Type == typ {
			return true
		}
	}
	return false
}

// resolveInterfaces records the types of every interface's methods'
// parameters and results, as a function's are resolved, and reports an
// error result that is not last. Every struct's fields and every global
// are known by then, so a method's array length may name a global.
func (c *Checker) resolveInterfaces(decls []ast.Decl) {
	for _, decl := range decls {
		td, ok := decl.(*ast.TypeDecl)
		if !ok || td.Interface() == nil {
			continue
		}
		restore := c.enter(td)
		for _, m := range td.Interface().Methods {
			for _, p := range m.Params {
				p.Type = c.paramType(p)
			}
			m.ResultTypes = make([]types.Type, len(m.Results))
			for i, r := range m.Results {
				m.ResultTypes[i] = c.resultType(r, len(m.Results), "")
			}
			for i, typ := range m.ResultTypes[:max(len(m.ResultTypes)-1, 0)] {
				if typ == types.Error {
					c.errorf(m.Results[i].Spanned(), "error must be the last result of %s.%s",
						td.Name, m.Name)
				}
			}
		}
		restore()
	}
}

// implicitTypeParams turns each parameter of fn whose whole type is an
// interface, s Shape, into one of a type parameter of its own constrained
// by it, appended to fn's type parameters: func area(s Shape) is shorthand
// for func area[S Shape](s S), and everything after this, inference,
// instances and code generation, sees an ordinary generic function. Two
// such parameters are two type parameters, so a call may pass each a
// different type. A method or an extern func cannot be generic, and a
// variadic parameter's elements would all share one type, so their
// interface parameters are left as written, to be reported as an
// interface used as a type, as an interface inside another type, a
// list's element or a function type's parameter, and a result, are.
func implicitTypeParams(fn *ast.Func) {
	if fn.Method || fn.Extern {
		return
	}
	for _, p := range fn.Params {
		id, ok := p.TypeExpr.(*ast.Ident)
		if !ok || p.Variadic {
			continue
		}
		if td, ok := id.Def.(*ast.TypeDecl); !ok || td.Interface() == nil {
			continue
		}
		tp := &ast.TypeParam{
			Constraint: id,
			Name:       id.Qualified(),
			NameSpan:   id.Span,
			Span:       id.Span,
			Implicit:   true,
		}
		fn.TypeParams = append(fn.TypeParams, tp)
		p.TypeExpr = &ast.Ident{Def: tp, Name: tp.Name, Span: id.Span}
	}
}

// resolveTypeParams gives each type parameter of fn a type of its own and
// finds the interface its constraint names, reporting a constraint that
// names anything else. It runs before fn's parameter types are resolved,
// which may name the type parameters.
func (c *Checker) resolveTypeParams(fn *ast.Func) {
	for _, tp := range fn.TypeParams {
		tp.Type = c.table.TypeParam(tp.Name)
		c.typeParams[tp.Type] = tp
		tp.Iface = c.constraint(tp)
	}
}

// constraint returns the interface tp's constraint names, reporting
// anything else written there: a name of no type, as an unknown type, and
// a type that is not an interface, a struct, a basic type, any included,
// an array, a view or a list. It returns nil when it reported.
func (c *Checker) constraint(tp *ast.TypeParam) *ast.TypeDecl {
	id, isName := tp.Constraint.(*ast.Ident)
	if isName {
		if td, ok := id.Def.(*ast.TypeDecl); ok && td.Interface() != nil {
			return td
		}
		if c.unbound(id) {
			return nil
		}
		if _, isBasic := types.Lookup(id.Name); !isBasic && id.Def == nil {
			c.errorf(id.Span, "unknown type: %s", id.Qualified())
			return nil
		}
	}
	c.errorf(tp.Constraint.Spanned(),
		"%s is not an interface; the constraint of type parameter %s must be one",
		c.text(tp.Constraint.Spanned()), tp.Name)
	return nil
}

// checkTypeParamsUsed reports each type parameter of fn that no
// parameter's type mentions, as the whole type or a part of it, a list's,
// a view's or a function type's among them: a call infers a type argument
// only from the arguments, so it could infer none for one. A result alone
// does not name one usably. A parameter whose type was reported may have
// mentioned it, and counts as doing so.
func (c *Checker) checkTypeParamsUsed(fn *ast.Func) {
	for _, tp := range fn.TypeParams {
		used := false
		for _, p := range fn.Params {
			if p.Type == types.Invalid || c.mentions(p.Type, tp.Type) {
				used = true
			}
		}
		if !used {
			c.errorf(tp.NameSpan, "%s appears in no parameter of %s, so no call can infer it",
				tp.Name, fn.Name)
		}
	}
}

// namedType returns the type id, a name written as a type, names when it
// is bound to a declaration, and whether it is: a struct type's, a type
// parameter's, or, for an interface, which is no type a value can have,
// types.Invalid, reported, since only a constraint may name one.
func (c *Checker) namedType(id *ast.Ident) (types.Type, bool) {
	switch def := id.Def.(type) {
	case *ast.TypeDecl:
		if def.Interface() != nil {
			c.errorf(id.Span, "%s is an interface; it can only constrain a type parameter",
				id.Qualified())
			return types.Invalid, true
		}
		return def.Type, true
	case *ast.TypeParam:
		return def.Type, true
	default:
		return types.Invalid, false
	}
}

// ifaceName names td, an interface, in a diagnostic, as typeName names a
// struct: qualified by its package's name when that is not the package
// being checked.
func (c *Checker) ifaceName(td *ast.TypeDecl) string {
	if pkg := c.declPkg[td]; pkg != nil && c.pkg != nil && pkg != c.pkg {
		return pkg.Name + "." + td.Name
	}
	return td.Name
}

// standInKey identifies the stand-in for one interface method called on
// values of one type parameter.
type standInKey struct {
	name string
	typ  types.Type
}

// typeParamMethod returns the method sel names of a value of typ, a type
// parameter: a stand-in for its constraint's method of that name (see
// ast.Func.Interface), whose receiver has type typ, so a call is checked
// against it as against a struct's method. A name the constraint has no
// method of is reported, a method named without a call included; a
// constraint that was reported has none, and nothing more is reported.
func (c *Checker) typeParamMethod(typ types.Type, sel *ast.Selector) *ast.Func {
	tp := c.typeParams[typ]
	if tp == nil || tp.Iface == nil {
		return nil
	}
	m := tp.Iface.Interface().Method(sel.Name)
	if m == nil {
		c.errorf(sel.NameSpan, "%s has no method %s", tp.Name, sel.Name)
		return nil
	}
	key := standInKey{typ: typ, name: m.Name}
	if fn := c.standIns[key]; fn != nil {
		return fn
	}
	recv := &ast.Param{Name: tp.Name, NameSpan: tp.NameSpan, Span: tp.Span, Type: typ, Mut: m.Mut}
	fn := &ast.Func{
		Name:        m.Name,
		Params:      append([]*ast.Param{recv}, m.Params...),
		Results:     m.Results,
		ResultTypes: m.ResultTypes,
		Interface:   m,
		NameSpan:    m.NameSpan,
		Span:        m.Span,
		NoBody:      true,
		Method:      true,
	}
	c.standIns[key] = fn
	return fn
}

// typeArg checks arg, passed to p, a parameter of a generic callee whose
// type is one of its type parameters, in a call being inferred: as a mut
// argument's place when p is mut, and as a value otherwise, reporting a
// mut argument to a plain parameter or a plain one to a mut parameter, as
// checkArg does. Its type then gives p's type parameter its type argument
// (see bindTypeArg). It returns the mut argument when arg is one passed to
// a mut parameter with nothing reported, for the exclusivity check.
func (c *Checker) typeArg(arg ast.Expr, p param, callee string) *ast.MutArg {
	arg, ok := c.ownArg(arg, p, callee)
	if !ok {
		return nil
	}
	m, isMut := arg.(*ast.MutArg)
	var got types.Type
	switch {
	case p.mut && isMut:
		c.checkErrorMut(m)
		got = c.place(m.X, m)
	case p.mut:
		c.expr(arg)
		c.missingMut(arg, p, callee)
		return nil
	case isMut:
		c.expr(m.X)
		text := c.text(m.X.Spanned())
		c.errorf(m.Span, "%s does not take %s as mut; pass %s", callee, text, text)
		return nil
	default:
		got = c.valueOf(arg, c.expr(arg))
	}
	if got == types.Invalid || !c.bindTypeArg(arg, p.typ, got, callee) {
		return nil
	}
	return m
}

// bindTypeArg makes got, the type of arg, the type argument of tp, a type
// parameter of the callee being inferred, named callee: got must be a
// struct or union type a package declares, or a type parameter, satisfy
// tp's constraint, and be the type any earlier argument gave tp. Anything
// else is reported at arg, and gives false.
func (c *Checker) bindTypeArg(arg ast.Expr, tp, got types.Type, callee string) bool {
	param := c.typeParams[tp]
	if param == nil || c.inference == nil {
		return false
	}
	at := arg
	if m, ok := arg.(*ast.MutArg); ok {
		at = m.X
	}
	if why := c.notTypeArg(got); why != "" {
		c.errorf(at.Spanned(), "cannot use %s as %s in argument to %s: %s",
			c.typeName(got), param.Name, callee, why)
		return false
	}
	if prev, ok := c.inference.args[tp]; ok && prev != got {
		c.errorf(
			at.Spanned(),
			"cannot use %s as %s in argument to %s: %s is %s in this call, from an earlier argument",
			c.typeName(got),
			param.Name,
			callee,
			param.Name,
			c.typeName(prev),
		)
		return false
	}
	if param.Iface != nil && !c.satisfies(at, got, param.Iface) {
		return false
	}
	c.inference.args[tp] = got
	return true
}

// satisfies reports whether got, a type argument passed as at, satisfies
// iface, and reports at at why it does not: with "here" when the reason
// is a method the package being checked cannot see, which another package
// might.
func (c *Checker) satisfies(at ast.Expr, got types.Type, iface *ast.TypeDecl) bool {
	why, here := c.unsatisfied(got, iface)
	if why == "" {
		return true
	}
	where := ""
	if here {
		where = " here"
	}
	c.errorf(at.Spanned(), "%s does not satisfy %s%s: %s",
		c.typeName(got), c.ifaceName(iface), where, why)
	return false
}

// checkGenericCall checks call, a call to fn, a generic function named
// callee, against its parameters, inferring a type argument for each of
// its type parameters from the arguments passed to parameters of that
// type (see typeArg), and records them in call.TypeArgs when every one
// was inferred. It returns the mut arguments passed to mut parameters with
// nothing reported and whether the count was right, as checkArgs does.
func (c *Checker) checkGenericCall(
	call *ast.Call,
	fn *ast.Func,
	callee string,
) ([]*ast.MutArg, bool) {
	outer := c.inference
	inf := &inference{fn: fn, args: make(map[types.Type]types.Type)}
	c.inference = inf
	muts, counted := c.checkArgs(call, callee, c.funcParams(fn.Params))
	c.inference = outer
	call.TypeArgs, call.ParamTypes, call.ResultTypes = nil, nil, nil
	if !counted {
		return muts, counted
	}
	args := make([]types.Type, len(fn.TypeParams))
	for i, tp := range fn.TypeParams {
		arg, ok := inf.args[tp.Type]
		if !ok {
			return muts, counted
		}
		args[i] = arg
	}
	call.TypeArgs = args
	c.recordSignature(call, fn, inf.args)
	return muts, counted
}

// notTypeArg returns why a value of type got cannot give a type parameter
// its type argument, or "" when it can: a view and an any refer to what
// another value holds, and neither is ever a list's element or a
// variable's type, as a type parameter's values are; and a type made of
// a type parameter, list[T] in a generic body, would make every instance
// need another, without end, so a type parameter is passed on only whole.
func (c *Checker) notTypeArg(got types.Type) string {
	switch {
	case c.table.IsView(got):
		return "a type argument cannot be a view"
	case got == types.Any:
		return "a type argument cannot be an any"
	case c.table.HasTypeParam(got) && !c.table.IsTypeParam(got):
		return "a type argument cannot be made of a type parameter, only be one"
	default:
		return ""
	}
}

// methodSig is what satisfying an interface compares of two methods: the
// parameters after the receiver, the result types, and whether the
// receiver is mut.
type methodSig struct {
	params  []*ast.Param
	results []types.Type
	mut     bool
}

// unsatisfied returns why typ, a struct or union type or a type parameter,
// does not satisfy iface, an interface, or "" when it does: a method iface
// asks for that typ lacks, cannot be seen from the package being checked,
// which holds the call binding typ, takes its receiver as mut where iface's
// does not or the other way round, or has another signature. Only the first
// such method, in iface's order, is named. here reports that the method
// cannot be seen, which another package might.
func (c *Checker) unsatisfied(typ types.Type, iface *ast.TypeDecl) (why string, here bool) {
	tp := c.typeParams[typ]
	if tp != nil && tp.Iface == nil {
		// Its constraint was reported; nothing is known of its methods.
		return "", false
	}
	for _, want := range iface.Interface().Methods {
		have, owner, why, here := c.methodFor(typ, tp, want.Name)
		if why != "" && c.lostMethodOf(typ, want.Name) {
			// A bad declaration may have been the method; whether it
			// would satisfy want is unknown, and nothing is reported.
			return "", false
		}
		if why != "" {
			return why, here
		}
		if why := c.methodMismatch(have, owner, want, iface); why != "" {
			return why, false
		}
	}
	return "", false
}

// methodMismatch returns why have, the method owner names, does not
// satisfy want, iface's method of its name: one takes its receiver as mut
// and the other does not, or their signatures differ. It returns "" when
// it does.
func (c *Checker) methodMismatch(have methodSig, owner string, want *ast.IfaceMethod,
	iface *ast.TypeDecl,
) string {
	wanted := c.ifaceName(iface) + "'s " + want.Name
	switch {
	case have.mut && !want.Mut:
		return fmt.Sprintf("%s takes its receiver as mut, and %s does not", owner, wanted)
	case !have.mut && want.Mut:
		return fmt.Sprintf("%s does not take its receiver as mut, and %s does", owner, wanted)
	}
	wantSig := methodSig{params: want.Params, results: want.ResultTypes, mut: want.Mut}
	if !sameSig(have, wantSig) {
		return fmt.Sprintf("%s is %s, and %s is %s",
			owner, c.sigText(have), wanted, c.sigText(wantSig))
	}
	return ""
}

// methodFor returns the method named name that typ, a struct or union
// type or, tp being its declaration, a type parameter, has to satisfy an
// interface's method with, as a signature, and how a diagnostic names it:
// the type's method, T.name, or the one of tp's constraint, J's name.
// When typ has none, it returns why instead; when the type's method is not
// visible from the package being checked, as hiddenMethod finds, it
// returns why with here true. A type parameter's methods are its
// constraint's, which every package that can name it sees.
func (c *Checker) methodFor(
	typ types.Type,
	tp *ast.TypeParam,
	name string,
) (have methodSig, owner, why string, here bool) {
	if tp != nil {
		m := tp.Iface.Interface().Method(name)
		if m == nil {
			return methodSig{}, "", fmt.Sprintf("its constraint %s has no method %s",
				c.ifaceName(tp.Iface), name), false
		}
		return methodSig{params: m.Params, results: m.ResultTypes, mut: m.Mut},
			c.ifaceName(tp.Iface) + "'s " + name, "", false
	}
	td := c.typeDecl(typ)
	if td == nil {
		return methodSig{}, "", fmt.Sprintf("%s has no method %s", c.typeName(typ), name), false
	}
	fn := td.Method(name)
	if fn == nil {
		return methodSig{}, "", fmt.Sprintf("%s has no method %s", c.typeName(typ), name), false
	}
	if why := c.hiddenMethod(fn); why != "" {
		return methodSig{}, "", why, true
	}
	return methodSig{params: fn.Params[1:], results: fn.ResultTypes, mut: fn.Recv().Mut},
		c.typeName(typ) + "." + name, "", false
}

// sameSig reports whether a and b have the same parameters, each of one
// type and alike in being mut, own, scope or variadic, and the same
// results. The
// receivers are compared apart.
func sameSig(a, b methodSig) bool {
	if len(a.params) != len(b.params) || len(a.results) != len(b.results) {
		return false
	}
	for i, p := range a.params {
		q := b.params[i]
		if p.Type != q.Type || p.Mut != q.Mut || p.Own != q.Own || p.Scope != q.Scope ||
			p.Variadic != q.Variadic {
			return false
		}
	}
	for i, r := range a.results {
		if r != b.results[i] {
			return false
		}
	}
	return true
}

// sigText writes s as a diagnostic shows a method's signature, without
// its receiver or its parameters' names: func(mut Point, own Text,
// scope func(), ...string) (int64, error).
func (c *Checker) sigText(s methodSig) string {
	params := make([]string, len(s.params))
	for i, p := range s.params {
		switch {
		case p.Variadic:
			params[i] = "..." + c.typeName(c.table.Elem(p.Type))
		case p.Mut:
			params[i] = "mut " + c.typeName(p.Type)
		case p.Own:
			params[i] = "own " + c.typeName(p.Type)
		case p.Scope:
			params[i] = "scope " + c.typeName(p.Type)
		default:
			params[i] = c.typeName(p.Type)
		}
	}
	text := "func(" + strings.Join(params, ", ") + ")"
	results := make([]string, len(s.results))
	for i, r := range s.results {
		results[i] = c.typeName(r)
	}
	switch len(results) {
	case 0:
		return text
	case 1:
		return text + " " + results[0]
	default:
		return text + " (" + strings.Join(results, ", ") + ")"
	}
}

// constraintExposure reports, for fn, a function visible as vis, every
// type parameter whose constraint is less visible than fn, as funcExposure
// reports a parameter's type: a package that can call fn must be able to
// name what its arguments must satisfy.
func (c *Checker) constraintExposure(vis ast.Visibility, what string, fn *ast.Func) {
	for _, tp := range fn.TypeParams {
		if tp.Iface == nil || tp.Iface.Vis >= vis {
			continue
		}
		need := "pub"
		if vis == ast.Lib {
			need = "lib or pub"
		}
		c.errorf(tp.Constraint.Spanned(), "%s %s exposes %s, which is not %s",
			vis, what, c.ifaceName(tp.Iface), need)
	}
}

// ifaceExposure reports every type a method of td, an interface visible
// beyond its package, names that is less visible than td, as
// funcExposure reports a function's.
func (c *Checker) ifaceExposure(td *ast.TypeDecl) {
	if td.Vis == ast.Private {
		return
	}
	what := "interface " + td.Name
	for _, m := range td.Interface().Methods {
		for _, p := range m.Params {
			c.exposure(td.Vis, what, p.Type, p.TypeExpr.Spanned())
		}
		for i, r := range m.Results {
			if i < len(m.ResultTypes) {
				c.exposure(td.Vis, what, m.ResultTypes[i], r.Spanned())
			}
		}
	}
}

// typeParamField reports sel, a field selected from a value of typ, a type
// parameter, which has no fields: only its constraint's methods. A method
// of the constraint named without a call is reported as such, as a
// struct's is.
func (c *Checker) typeParamField(sel *ast.Selector, typ types.Type) {
	if tp := c.typeParams[typ]; tp != nil && tp.Iface != nil &&
		tp.Iface.Interface().Method(sel.Name) != nil {
		c.errorf(sel.NameSpan, "%s is a method of %s, not a field; a method can only be called",
			sel.Name, c.typeName(typ))
		return
	}
	c.errorf(sel.NameSpan, "%s has no field %s: a type parameter has only its constraint's methods",
		c.typeName(typ), sel.Name)
}
