package typeck

import (
	"fmt"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// Unions. type Expr union { int int64; name string; eof } declares a
// union type: a value of it holds exactly one of its cases, with that
// case's payload, of the type written after its name, or none. Each
// declaration is a type of its own (types.Table.Union), whose cases are
// resolved with every struct's fields, before anything is checked, a
// payload's type being any a field may have. A union holding itself,
// directly or through a struct, an array or another union, is reported
// with the structs that do (see reportCycles), as infinitely large.
//
// A value is built by naming a case of the union, Expr.int(5), with its
// payload, used as the case's type as an argument to a parameter of it
// is, or Expr.eof, for a case with no payload; from another package,
// ast.Expr.int(5). There is no zero value, so a variable of a union, or
// of anything holding one, is always declared with a value. Two unions of
// one type compare with == and != when every payload's type does: the
// same case, with equal payloads.
//
// A switch on a union matches the case its tag holds (checkUnionSwitch):
// each of its values names a case, and a clause naming one case may bind
// its payload, case int(n), to a variable of the payload's type, which
// refers to the payload in place and is immutable; a clause naming
// several binds nothing. The clauses cover every case, or the switch has
// a default, but not both. While a binding refers into the switched
// place its clause cannot change that place (see move.go). A union
// holding a list is owned as a struct holding one is, and a payload is
// never moved out of it.
//
// A union cannot cross to C, be passed as any, or be converted. It may
// have methods, declared and called as a struct's are (see method.go),
// none named like one of its cases, and so satisfy interfaces and be a
// type argument to a generic function (see generic.go).

// unionValue is the computed value of a package-level union, as fold
// gives it: its case's position among the union's cases and its payload's
// value, nil for a case with no payload (see ast.UnionConst).
type unionValue = ast.UnionConst

// resolveCases records the cases of the union type typ in the table the
// first time they are needed, as resolveFields does a struct's fields: a
// global's initializer may build one before its turn. Needing them again
// while they are being resolved, at, is reported as an invalid recursive
// type, and gives false.
func (c *Checker) resolveCases(typ types.Type, at ast.Span) bool {
	switch c.unionState[typ] {
	case checked:
		return true
	case checking:
		c.errorf(at, "invalid recursive type %s", c.typeName(typ))
		return false
	}

	c.unionState[typ] = checking
	td := c.unions[typ]
	defer c.enter(td)()
	cases := make([]types.Case, 0)
	if ut := td.Union(); ut != nil {
		outer, outerUnion := c.inFields, c.inUnion
		c.inFields, c.inUnion = true, true
		for _, uc := range ut.Cases {
			tc := types.Case{Name: uc.Name}
			if uc.TypeExpr != nil {
				tc.Payload, tc.Type = true, c.declaredType(uc.TypeExpr)
			}
			cases = append(cases, tc)
		}
		c.inFields, c.inUnion = outer, outerUnion
	}
	c.table.SetCases(typ, cases)
	c.unionState[typ] = checked
	return true
}

// typeDecl returns the declaration of typ, a struct or a union type a
// program declares, or nil for any other type.
func (c *Checker) typeDecl(typ types.Type) *ast.TypeDecl {
	if td := c.structs[typ]; td != nil {
		return td
	}
	return c.unions[typ]
}

// construction checks e, which builds a value of a union (see
// ast.Construction), and returns its type, which it records on e: Expr.int(5),
// whose payload is used as the case's type, as an argument is, or
// Expr.eof, for a case with no payload. A case the union does not have, a
// payload case named without its payload, and a case with no payload
// called are reported, and give types.Invalid.
func (c *Checker) construction(e ast.Expr) types.Type {
	td, sel, _, _ := ast.Construction(e)
	typ := td.Type
	call, isCall := e.(*ast.Call)
	built := c.buildCase(typ, sel, call, isCall)
	if isCall {
		call.Type = built
	} else {
		sel.Type = built
	}
	return built
}

// buildCase checks the case sel names of the union typ being built, by
// call when isCall, and returns typ, or types.Invalid when anything was
// reported. The arguments of a call that cannot build the case are
// checked for errors of their own.
func (c *Checker) buildCase(
	typ types.Type,
	sel *ast.Selector,
	call *ast.Call,
	isCall bool,
) types.Type {
	if typ == types.Invalid || !c.resolveCases(typ, sel.Span) {
		c.discardArgs(call, isCall)
		return types.Invalid
	}
	uc, _, ok := c.table.Case(typ, sel.Name)
	name := c.typeName(typ) + "." + sel.Name
	switch {
	case !ok:
		c.errorf(sel.NameSpan, "%s has no case %s", c.typeName(typ), sel.Name)
	case !uc.Payload && isCall:
		c.errorf(call.Span, "%s has no payload; write %s", name, c.text(sel.Span))
	case !uc.Payload:
		return typ
	case !isCall:
		c.errorf(sel.Span, "%s needs its %s payload, as in %s(...)",
			name, c.typeName(uc.Type), c.text(sel.Span))
		return types.Invalid
	default:
		return c.payload(typ, uc, name, call)
	}
	c.discardArgs(call, isCall)
	return types.Invalid
}

// discardArgs checks the arguments of call, when isCall says there is
// one, a construction that cannot use them, for errors of their own.
func (c *Checker) discardArgs(call *ast.Call, isCall bool) {
	if !isCall {
		return
	}
	for _, arg := range call.Args {
		c.argExpr(arg)
	}
}

// payload checks the payload call passes to build the case uc, named name,
// of the union typ: one argument, used as the case's type as an argument
// to a parameter of it is. It returns typ, or types.Invalid when anything
// was reported, the arguments of a call of the wrong count checked for
// errors of their own.
func (c *Checker) payload(typ types.Type, uc types.Case, name string, call *ast.Call) types.Type {
	if !c.checkArity(call, name, 1) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return types.Invalid
	}
	arg := call.Args[0]
	if !c.use(arg, c.expr(arg), uc.Type, "in argument to "+name) {
		return types.Invalid
	}
	return typ
}

// checkUnionSwitch checks s, a switch in fn whose tag, already checked, is
// of the union type typ: each clause in the order tried, its values as
// patterns naming typ's cases, the variable a clause naming one case
// binds, of that case's payload's type, and its body; then that the
// clauses cover every case, or the switch has a default, and not both. A
// switch on a union gives no facts, as a type switch gives none: which
// case a union holds is known only at run time.
func (c *Checker) checkUnionSwitch(s *ast.Switch, typ types.Type, fn *ast.Func) {
	if !c.resolveCases(typ, s.Tag.Spanned()) {
		return
	}
	covered := c.checkCoverage(s, typ)
	seen := make(map[string]ast.Expr)
	for _, cl := range tryOrder(s) {
		c.checkPatterns(typ, cl, seen, s.Tag)
		rejected := cl.Default && covered
		if rejected {
			c.errorf(cl.Span, "every case of %s is already covered, so this default never runs",
				c.typeName(typ))
			c.inCoveredDefault++
		}
		c.inClause(cl, unionSwitch, func() {
			c.checkGuarded(cl.Body, &guard{span: caseSpan(cl), kind: guardCase}, nil, fn)
		})
		if rejected {
			c.inCoveredDefault--
		}
		if cl.Var != nil && cl.Var.Type != types.Invalid && !reads(cl.Body, cl.Var) {
			c.unusedf(cl.Var.NameSpan, "declared and not used: %s", cl.Var.Name)
			cl.Var.Unused = true
		}
	}
}

// checkPatterns checks the values of cl, a clause of a switch on the
// union typ, each a pattern naming one of its cases, none named earlier in
// the switch, which seen records, and gives the variable the clause binds,
// if any, its payload's type, referring into tag, what the switch matches
// (see checkBinding). A value of any other
// shape is reported. Whatever the resolver bound a pattern's names to is
// unbound, since a case's name is found in the union and not where the
// switch is.
func (c *Checker) checkPatterns(
	typ types.Type,
	cl *ast.CaseClause,
	seen map[string]ast.Expr,
	tag ast.Expr,
) {
	bound := false
	for _, v := range cl.Values {
		name, bind, ok := ast.CasePattern(v)
		if !ok {
			c.errorf(v.Spanned(), "a case of a switch on %s names one of its cases, as in case %s",
				c.typeName(typ), examplePattern(c.table.Cases(typ)))
			continue
		}
		if call, isCall := v.(*ast.Call); isCall {
			call.Conv = types.Invalid
		}
		name.Def = nil
		uc, _, found := c.table.Case(typ, name.Name)
		if !found {
			c.errorf(name.Span, "%s has no case %s", c.typeName(typ), name.Name)
			continue
		}
		c.notTwice(name, v, seen)
		if bind != nil {
			bound = c.checkBinding(cl, uc, name, bind, tag) || bound
		}
	}
	if cl.Var != nil && !bound {
		// The binding was reported, or the case has no payload: its
		// variable has no type, so nothing built on it is reported.
		cl.Var.Type = types.Invalid
	}
}

// examplePattern returns a pattern an error suggests, for a union whose
// cases are cases: its first case's, with a binding when it has a
// payload, or eof's for a union with no cases.
func examplePattern(cases []types.Case) string {
	if len(cases) == 0 {
		return "eof"
	}
	if cases[0].Payload {
		return cases[0].Name + "(v)"
	}
	return cases[0].Name
}

// notTwice reports v, a pattern naming the case name, when an earlier
// pattern of the switch, which seen records by case name, named it too,
// and otherwise records it.
func (c *Checker) notTwice(name *ast.Ident, v ast.Expr, seen map[string]ast.Expr) {
	prev, dup := seen[name.Name]
	if !dup {
		seen[name.Name] = v
		return
	}
	c.diags = append(c.diags, diag.Diagnostic{
		Severity: diag.Error,
		Message:  fmt.Sprintf("%s appears twice in this switch", name.Name),
		Span:     v.Spanned(),
		Related: []diag.Diagnostic{{
			Severity: diag.Note,
			Message:  "previous case",
			Span:     prev.Spanned(),
		}},
	})
}

// checkBinding checks bind, the binding of a pattern naming uc, the case
// called name, in cl: the case has a payload, the clause names no other
// case, and bind is a name the clause can declare, not _, which binds
// nothing, a type's name, or a name visible at the switch, which the
// resolver bound bind to, and which is left bound, since a body binding
// a method call's results is checked twice (see checkBody). It gives the
// clause's variable the payload's type, records tag, what the switch
// matches, as what it refers into (unionBindings), and reports whether it
// did.
func (c *Checker) checkBinding(
	cl *ast.CaseClause,
	uc types.Case,
	name, bind *ast.Ident,
	tag ast.Expr,
) bool {
	switch {
	case !uc.Payload:
		c.errorf(bind.Span, "%s has no payload to bind; write case %s", name.Name, name.Name)
		return false
	case bind.Name == ast.Blank:
		c.errorf(bind.Span, "every name is _, so nothing is bound; write case %s", name.Name)
		return false
	case len(cl.Values) > 1:
		c.errorf(
			bind.Span,
			"a clause naming several cases binds nothing; write %s, or give %s(%s) a clause of its own",
			name.Name,
			name.Name,
			bind.Name,
		)
		return false
	}
	if _, isType := types.Lookup(bind.Name); isType {
		c.errorf(bind.Span, "cannot use %s as a name; it is a type", bind.Name)
		return false
	}
	if bind.Def != nil {
		c.redeclared(bind, bind.Def)
		return false
	}
	if cl.Var == nil {
		return false
	}
	cl.Var.Type = uc.Type
	c.unionBindings[cl.Var] = tag
	return uc.Type != types.Invalid
}

// redeclared reports id, a binding, whose name existing, visible where it
// is declared, already has: an import's, reported as such, or any
// other's, as declared already, with a note at it, as the resolver
// reports a variable reusing a name.
func (c *Checker) redeclared(id *ast.Ident, existing ast.Decl) {
	if _, ok := existing.(*ast.Import); ok {
		c.errorf(id.Span, "%s is an imported package and cannot be redeclared", id.Name)
		return
	}
	c.errorNotes(id.Span, []diag.Diagnostic{
		note(declNameSpan(existing), "previous declaration of "+id.Name),
	}, "%s is already declared", id.Name)
}

// declNameSpan returns the span of the name a declaration introduces.
func declNameSpan(decl ast.Decl) ast.Span {
	switch d := decl.(type) {
	case *ast.Func:
		return d.NameSpan
	case *ast.Param:
		return d.NameSpan
	case *ast.Var:
		return d.NameSpan
	case *ast.TypeDecl:
		return d.NameSpan
	case *ast.TypeParam:
		return d.NameSpan
	case *ast.Import:
		return d.NameSpan
	default:
		return decl.Spanned()
	}
}

// reads reports whether a name in body, at any depth, refers to v.
func reads(body []ast.Stmt, v *ast.Var) bool {
	found := false
	for _, stmt := range body {
		ast.Walk(stmt, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Def == v {
				found = true
			}
			return !found
		})
	}
	return found
}

// checkCoverage reports s, a switch on the union typ, when its patterns
// neither cover every case nor does it have a default, listing the cases
// it misses, at its header, before anything in its clauses is reported.
// A switch with a bad clause is not reported: that clause may have named
// the cases missing, or been the default (see ast.CaseClause). It records
// on s whether it covers every case without a default, and reports
// whether its patterns cover every case, which makes a default one that
// never runs, reported at the default.
func (c *Checker) checkCoverage(s *ast.Switch, typ types.Type) bool {
	named := make(map[string]bool)
	for _, cl := range s.Cases {
		for _, v := range cl.Values {
			if name, _, ok := ast.CasePattern(v); ok {
				named[name.Name] = true
			}
		}
	}
	var missing []string
	for _, uc := range c.table.Cases(typ) {
		if !named[uc.Name] {
			missing = append(missing, uc.Name)
		}
	}
	switch {
	case s.Default() != nil:
	case len(missing) > 0:
		c.reportUncovered(s, typ, missing)
	default:
		s.Exhaustive = true
	}
	return len(missing) == 0
}

// reportUncovered reports s, a switch on the union typ with no default, for
// not naming the cases missing, at its header, unless it has a bad
// clause, which may have named them or been the default.
func (c *Checker) reportUncovered(s *ast.Switch, typ types.Type, missing []string) {
	if hasBadClause(s) {
		return
	}
	header := ast.Span{File: s.Span.File, Start: s.Span.Start, End: s.Tag.Spanned().End}
	pronoun := "it"
	if len(missing) > 1 {
		pronoun = "them"
	}
	c.errorf(header, "switch on %s misses %s %s; cover %s or add a default",
		c.typeName(typ), plural(len(missing), "case"), strings.Join(missing, ", "), pronoun)
}

// unresolvedCase reports v, a value of a case of an expression switch
// whose tag, of type tagType, is no union, when the resolver left any of
// it as a union's pattern (see Patterns in package resolver): a name
// bound to no variable or parameter, a call's callee bound to nothing a
// call can call, or a call's one argument, a name, bound to no variable
// or parameter. It reports what resolving v as a value would have, a
// name that names nothing, or a type, a package or a function where a
// value is wanted, and checks a call's other arguments for errors of
// their own. It reports whether v was such a value, which is then
// checked no further. A tag whose type was reported, tagType Invalid,
// reports nothing more.
func (c *Checker) unresolvedCase(v ast.Expr, tagType types.Type) bool {
	switch x := v.(type) {
	case *ast.Ident:
		if x.Pkg != "" || isValueDecl(x.Def) {
			return false
		}
		if tagType != types.Invalid {
			c.unresolvedValue(x)
		}
		return true
	case *ast.Call:
		return c.unresolvedCall(x, tagType)
	default:
		return false
	}
}

// unresolvedCall is unresolvedCase for call.
func (c *Checker) unresolvedCall(call *ast.Call, tagType types.Type) bool {
	fn, isName := call.Fn.(*ast.Ident)
	if !isName || fn.Pkg != "" || call.Spread {
		return false
	}
	_, isFunc := fn.Def.(*ast.Func)
	calleeBad := !isFunc && call.Conv == types.Invalid
	arg, isBind := patternBinding(call)
	if !calleeBad && !isBind {
		return false
	}
	if tagType != types.Invalid {
		c.reportUnresolvedCall(call, calleeBad, arg, isBind)
	}
	return true
}

// reportUnresolvedCall reports call, a case value the resolver left a
// pattern: its callee, when calleeBad says it names nothing a call can
// call, and its one argument, arg, when isBind says it is a binding,
// which names no variable or parameter; any other arguments are checked
// for errors of their own.
func (c *Checker) reportUnresolvedCall(
	call *ast.Call,
	calleeBad bool,
	arg *ast.Ident,
	isBind bool,
) {
	if calleeBad {
		if fn, ok := call.Fn.(*ast.Ident); ok {
			c.unresolvedCallee(fn, call)
		}
	}
	if isBind {
		c.unresolvedValue(arg)
		return
	}
	for _, a := range call.Args {
		c.discard(a)
	}
}

// patternBinding returns the argument of call when the resolver left it a
// pattern's binding: it is the one argument, a name not qualified, bound
// to no variable or parameter.
func patternBinding(call *ast.Call) (*ast.Ident, bool) {
	if len(call.Args) != 1 {
		return nil, false
	}
	arg, ok := call.Args[0].(*ast.Ident)
	return arg, ok && arg.Pkg == "" && !isValueDecl(arg.Def)
}

// isValueDecl reports whether decl, what a name is bound to, is a
// variable or a parameter, a value a name can stand for.
func isValueDecl(decl ast.Decl) bool {
	switch decl.(type) {
	case *ast.Var, *ast.Param:
		return true
	default:
		return false
	}
}

// unresolvedCallee reports id, the callee of call, a pattern, as the
// resolver reports a callee: a name bound to nothing as undefined, and a
// type's or a package's name as one. A function, a builtin or a
// conversion's type is a callee.
func (c *Checker) unresolvedCallee(id *ast.Ident, call *ast.Call) {
	if call.Conv != types.Invalid {
		return
	}
	switch id.Def.(type) {
	case *ast.Func:
	case *ast.TypeDecl, *ast.TypeParam:
		c.errorf(id.Span, "cannot call %s; it is a type", id.Name)
	case *ast.Import:
		c.errorf(id.Span, "%s is a package, not a function", id.Name)
	default:
		c.errorf(id.Span, "undefined: %s", id.Name)
	}
}

// unresolvedValue reports id, a name of a pattern used as a value, as the
// resolver reports a value: _, a type's name and a package's as such, and
// a name bound to nothing as undefined.
func (c *Checker) unresolvedValue(id *ast.Ident) {
	switch id.Def.(type) {
	case *ast.TypeDecl, *ast.TypeParam:
		c.errorf(id.Span, "cannot use %s as a value; it is a type", id.Name)
		return
	case *ast.Import:
		c.errorf(id.Span, "%s is a package, not a value", id.Name)
		return
	}
	if id.Name == ast.Blank {
		c.errorf(id.Span, "cannot use _ as a value")
		return
	}
	if _, isType := types.Lookup(id.Name); isType {
		c.errorf(id.Span, "cannot use %s as a value; it is a type", id.Name)
		return
	}
	c.errorf(id.Span, "undefined: %s", id.Name)
}

// isConstantConstruction reports whether e builds a union, built, and
// whether it is a constant expression, isConst: every argument it passes,
// its payload, if any, is one. Whether the case takes that payload is
// found when e is checked.
func (c *Checker) isConstantConstruction(e ast.Expr) (isConst, built bool) {
	if _, _, _, ok := ast.Construction(e); !ok {
		return false, false
	}
	if call, isCall := e.(*ast.Call); isCall {
		return c.allConstant(call.Args), true
	}
	return true, true
}

// foldConstruction computes e, a checked construction of a constant
// payload, or of none, as a unionValue.
func (c *Checker) foldConstruction(e ast.Expr) (any, bool) {
	td, sel, payload, _ := ast.Construction(e)
	_, i, ok := c.table.Case(td.Type, sel.Name)
	if !ok {
		return nil, false
	}
	if payload == nil {
		return unionValue{Case: i}, true
	}
	v, ok := c.fold(payload)
	if !ok {
		return nil, false
	}
	return unionValue{Case: i, Payload: v}, true
}

// externUnion reports typ, a parameter's or a result's type of an extern
// func, written at at, when it is or holds a union, through a struct, an
// array, a view or a list: a union does not cross to C yet. what names
// it, "parameter e of extern func f" or "extern func f cannot return".
func (c *Checker) externUnion(typ types.Type, at ast.Span, what string) {
	if !c.crossesUnion(typ) {
		return
	}
	name := c.typeName(typ)
	if !c.table.IsUnion(typ) {
		name += ", which holds a union"
	}
	c.errorf(at, "%s %s: a union cannot cross to C yet", what, name)
}

// crossesUnion reports whether typ is or holds a union, through a
// struct, an array, a view or a list, which C would have to see.
func (c *Checker) crossesUnion(typ types.Type) bool {
	for c.table.IsView(typ) || c.table.IsList(typ) {
		typ = c.table.Elem(typ)
	}
	return c.table.HoldsUnion(typ)
}

// unionExposure reports every payload type of td, a union visible beyond
// its package, that is less visible than it.
func (c *Checker) unionExposure(td *ast.TypeDecl) {
	if td.Vis == ast.Private {
		return
	}
	cases := c.table.Cases(td.Type)
	for i, uc := range td.Union().Cases {
		if uc.TypeExpr != nil && i < len(cases) {
			c.exposure(td.Vis, "case "+uc.Name+" of "+td.Name, cases[i].Type, uc.TypeExpr.Spanned())
		}
	}
}
