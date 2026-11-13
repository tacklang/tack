package typeck

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// Call sites. A function or a method may declare one parameter of type
// testing.Site, std/testing's Site, which the checker recognizes by its
// package's path and its name, as it does text.Text. A call writes no
// argument for it: the checker checks the arguments written against the
// other parameters, then passes the call's own place in its stead, in
// Call.Site, which it puts among Args at the parameter's position, so
// that every stage after it sees one argument for each parameter. The
// place is a literal of Site made from the call's span: file, the name
// diagnostics give the call's file, line and col, where the span
// starts, and excerpt, what diag.Excerpt gives for the span, exactly
// what a diagnostic at the call prints below its header. All four are
// constants, worked out once, here; the program never reads its source.
// A call written in a function that has a Site parameter of its own
// passes that parameter on instead, by name, so a helper that declares
// one reports the place its own caller was written; a closure has none,
// so a call in a closure's body passes its own place, wherever the
// closure is written.
//
// Each body may be checked more than once (see checkBody), so a call
// checked again first takes out the arguments it was given (dropSite).
//
// A parameter is a Site parameter when its type is written testing.Site
// (noteSiteParam), whether or not own or scope then made its type one
// that is reported, so that a call never reports a missing argument for
// a parameter whose own problem was reported where it is declared. A
// second Site parameter, reported too, takes no argument either and is
// passed one as the first is.
//
// What could pass a Site other than a call by name is reported: a
// function with a Site parameter cannot be a value (notValue), so no
// closure has one either, nor a function type or an interface method,
// and no method with one satisfies an interface method that has none; an
// extern func has none, as C could not be passed one; none is mut, since
// what is passed is a value, not a place; and no variadic parameter's
// element is a Site. An argument of type testing.Site written where the
// call passes one is reported. main declares no parameters at all (see
// the resolver). A Site value is otherwise a value like any other: a
// field may hold one, a function may return one, and a generic
// function's parameter of a type parameter may be given one.
//
// The literal names Site's fields, so std/testing's Site must declare
// exactly those, which checkSiteShape checks: a module named std holds
// a std/testing of its own.

// stdTesting is the import path of std/testing, whose Site the checker
// passes a call's place in.
const stdTesting = "std/testing"

// siteFields are the fields testing.Site declares, in order, every one
// pub, which the literal of a call's place gives a value each.
var siteFields = []types.Field{
	{Name: "file", Type: types.String},
	{Name: "line", Type: types.Int64},
	{Name: "col", Type: types.Int64},
	{Name: "excerpt", Type: types.String},
}

// isSite reports whether typ is testing.Site, std/testing's Site.
func (c *Checker) isSite(typ types.Type) bool {
	td := c.structs[typ]
	pkg := c.structPkg[typ]
	return td != nil && pkg != nil && pkg.Path == stdTesting && td.Name == "Site"
}

// checkSiteShape reports std/testing's Site, at its name, when its
// fields are not exactly siteFields, in order and each pub: the literal
// of a call's place could not be lowered otherwise. Every struct's fields
// are resolved by then.
func (c *Checker) checkSiteShape() {
	for _, typ := range c.structOrder {
		td := c.structs[typ]
		if !c.isSite(typ) {
			continue
		}
		st, _ := td.TypeExpr.(*ast.StructType)
		fields := c.table.Fields(typ)
		ok := st != nil && len(st.Fields) == len(siteFields) && len(fields) == len(siteFields)
		for i := 0; ok && i < len(siteFields); i++ {
			ok = fields[i] == siteFields[i] && st.Fields[i].Vis == ast.Pub
		}
		if !ok {
			c.errorf(td.NameSpan, "testing.Site must be struct { pub file string; pub line int64; "+
				"pub col int64; pub excerpt string }: a call fills in those fields")
		}
	}
}

// noteSiteParam records param, a parameter whose type is written as typ,
// as a Site parameter when typ is testing.Site, before own or scope is
// asked about (see paramType). A variadic one is not: its type is a view,
// and its element being a Site is reported (see variadicParam).
func (c *Checker) noteSiteParam(param *ast.Param, typ types.Type) {
	if param.Variadic || !c.isSite(typ) {
		return
	}
	if c.siteParams == nil {
		c.siteParams = make(map[*ast.Param]types.Type)
	}
	c.siteParams[param] = typ
}

// declaredSite returns the Site type param's type is written as, and
// whether it is one (see noteSiteParam).
func (c *Checker) declaredSite(param *ast.Param) (types.Type, bool) {
	typ, ok := c.siteParams[param]
	return typ, ok
}

// siteParam returns the Site parameter of fn, a function or a method,
// its receiver aside, or nil when it has none. A closure's Site
// parameter, which is reported (see notValue), is never one.
func (c *Checker) siteParam(fn *ast.Func) *ast.Param {
	if fn == nil || fn.Closure {
		return nil
	}
	for i, p := range fn.Params {
		if _, ok := c.declaredSite(p); ok && (!fn.Method || i > 0) {
			return p
		}
	}
	return nil
}

// checkSiteParams reports what fn, a function or a method whose
// parameters' types are resolved, may not declare of Site parameters: a
// second one, one of an extern func, and a mut one.
func (c *Checker) checkSiteParams(fn *ast.Func) {
	seen := false
	for i, p := range fn.Params {
		if _, ok := c.declaredSite(p); !ok || fn.Method && i == 0 {
			continue
		}
		switch {
		case seen:
			c.errorf(p.Span, "a second testing.Site parameter: a function takes at most one")
		case fn.Extern:
			c.errorf(p.Span, "extern func %s cannot take a testing.Site: "+
				"a call passes it implicitly, which a call from C cannot", fn.Name)
		case p.Mut:
			c.errorf(p.MutSpan, "a testing.Site parameter cannot be mut: "+
				"a call passes its place as a value")
		}
		seen = true
	}
}

// withoutSite returns params, those a call's arguments are checked
// against, without the Site parameters among them, which the call passes
// itself, and their indexes in params, nil when there is none. Every
// parameter left is marked as one of a callee with a Site, for the
// messages about a Site argument passed to it and about the count (see
// checkArg and siteCount). Only a function already reported has two.
func withoutSite(params []param) ([]param, []int) {
	var at []int
	rest := make([]param, 0, len(params))
	for i, p := range params {
		if p.site {
			at = append(at, i)
			continue
		}
		rest = append(rest, p)
	}
	if at == nil {
		return params, nil
	}
	for i := range rest {
		rest[i].siteCallee = true
	}
	return rest, at
}

// siteCount is what a message about the number of arguments of a call
// adds when the callee has a Site parameter, site, which takes none
// written: that the count leaves it out.
func siteCount(site bool) string {
	if site {
		return ", not counting its testing.Site"
	}
	return ""
}

// siteArity reports call, a call to callee, whose parameters but its
// Site ones are want plain ones, written with another number of
// arguments or with a spread: each argument is checked for errors of its
// own, and then one of type testing.Site, written for the parameter the
// call passes itself, is reported, or else a spread, as spreadElsewhere
// reports one, or the count, as checkArity reports it, saying the count
// leaves the Site out.
func (c *Checker) siteArity(call *ast.Call, callee string, want int) {
	for _, arg := range call.Args {
		c.argExpr(arg)
	}
	for _, arg := range call.Args {
		if c.isSite(ast.TypeOf(arg)) {
			c.explicitSite(arg, callee)
			return
		}
	}
	if c.spreadElsewhere(call, callee) {
		return
	}
	c.errorf(call.Spanned(), "%s expects %d argument(s), got %d%s",
		callee, want, len(call.Args), siteCount(true))
}

// explicitSite reports arg, an argument of type testing.Site written in
// a call to callee, which passes its own.
func (c *Checker) explicitSite(arg ast.Expr, callee string) {
	c.errorf(arg.Spanned(), "cannot pass a testing.Site to %s: the call passes its own", callee)
}

// dropSite takes out of call's arguments every Site argument an earlier
// check of it passed (see passSite), so that it is checked as written.
func (c *Checker) dropSite(call *ast.Call) {
	if call.Site == nil {
		return
	}
	args := make([]ast.Expr, 0, len(call.Args))
	for _, arg := range call.Args {
		if !c.siteArgs[arg] {
			args = append(args, arg)
		}
	}
	call.Args = args
	call.Site = nil
}

// passSite puts a Site argument of call, a call whose written arguments
// were checked against params but its Site parameters, at each index of
// at, those parameters' indexes in increasing order, among its
// arguments, records the first in call.Site and every one in siteArgs:
// the Site parameter of the function being checked, or the call's own
// place (see siteLit). A call whose arguments are too few to reach an
// index, a variadic call already reported, gets none from there on.
func (c *Checker) passSite(call *ast.Call, params []param, at []int) {
	if c.siteArgs == nil {
		c.siteArgs = make(map[ast.Expr]bool)
	}
	end := ast.Span{File: call.Span.File, Start: call.Span.End, End: call.Span.End}
	for _, i := range at {
		if i > len(call.Args) {
			return
		}
		var arg ast.Expr
		if p := c.siteParam(c.fn); p != nil {
			arg = &ast.Ident{Name: p.Name, Def: p, Span: end}
		} else {
			arg = c.siteLit(call, params[i].siteType, end)
		}
		args := make([]ast.Expr, 0, len(call.Args)+1)
		args = append(args, call.Args[:i]...)
		args = append(args, arg)
		call.Args = append(args, call.Args[i:]...)
		c.siteArgs[arg] = true
		if call.Site == nil {
			call.Site = arg
		}
	}
}

// siteLit returns a literal of typ, testing.Site, holding call's place,
// as the top of this file says, every node of it at span, where a
// diagnostic about it, which none should be, would point.
func (c *Checker) siteLit(call *ast.Call, typ types.Type, span ast.Span) *ast.CompositeLit {
	f := c.files.Lookup(call.Span.File)
	if f == nil {
		panic("typeck: a span outside the source given to New")
	}
	line, col := f.Position(call.Span.Start)
	field := func(name string, value ast.Expr) ast.Expr {
		return &ast.KeyValue{Key: &ast.Ident{Name: name, Span: span}, Value: value, Span: span}
	}
	number := func(n int) ast.Expr {
		return &ast.IntLit{Span: span, Value: constant.FromInt64(int64(n)), Type: types.Int64}
	}
	return &ast.CompositeLit{
		Span: span,
		Type: typ,
		Elems: []ast.Expr{
			field("file", &ast.StrLit{Value: f.Filename, Span: span}),
			field("line", number(line)),
			field("col", number(col)),
			field("excerpt", &ast.StrLit{Value: diag.Excerpt(f, call.Span), Span: span}),
		},
	}
}

// siteNotValue returns why a function whose parameters are ps, a
// method's receiver first when recv says so, cannot be a value because
// of a Site parameter, or "" when it has none.
func (c *Checker) siteNotValue(ps []*ast.Param, recv bool) string {
	for i, p := range ps {
		if _, ok := c.declaredSite(p); ok && (!recv || i > 0) {
			return fmt.Sprintf("%s is a testing.Site, which no function type's parameter can be",
				itsParam(ps, i, recv))
		}
	}
	return ""
}
