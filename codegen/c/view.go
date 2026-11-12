package c

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// emitViewDef defines the C struct a view type lowers to: a pointer to its
// first element, to const, since a view is read-only, and its number of
// elements, the layout of a string, whose bytes a []uint8 views.
func (g *generator) emitViewDef(typ types.Type) {
	fmt.Fprintf(
		&g.buf,
		"typedef struct {\n    const %s *ptr;\n    int64_t len;\n} %s;\n\n",
		g.cType(g.prog.Types.Elem(typ)),
		g.viewName(typ),
	)
}

// viewName is the name of the C struct a view type lowers to, made from
// its ID as an array's is, tack_view_ and a digit.
func viewNameOf(typ types.Type) string {
	return fmt.Sprintf("tack_view_%d", typ)
}

// hasLen reports whether typ is a string, a view or a list, which lower to
// a pointer and a length, read as x.ptr[i] and x.len.
func (g *generator) hasLen(typ types.Type) bool {
	return typ == types.String || g.prog.Types.IsView(typ) || g.prog.Types.IsList(typ)
}

// argTypes returns the C type of each of call's arguments as callExpr
// renders them: an argument's own type, a pointer to it for a mut
// argument, or, for one passed to a view parameter, a spread among them,
// the view's, and for one passed to a scope parameter a tack_scope. A
// value for a variadic parameter is its own type, before it is packed
// into the variadic's view (see packed).
func (g *generator) argTypes(call *ast.Call, params []*ast.Param) []string {
	out := make([]string, len(call.Args))
	for i, a := range call.Args {
		if p := g.viewParamAt(call, params, i); p != nil {
			out[i] = g.cType(p.Type)
			continue
		}
		if i < len(params) && g.scopeFunc(params[i]) {
			out[i] = scopeType
			continue
		}
		out[i] = g.operandType(a)
	}
	return out
}

// viewParamAt returns the parameter of params, a call's callee's, that
// the call's argument i is passed to as a view, or nil when it is passed
// as itself: a view parameter in its position, or a variadic one that the
// call spreads a view into. A value for a variadic parameter, one of the
// call's arguments after the fixed ones when it does not spread, is not a
// view.
func (g *generator) viewParamAt(call *ast.Call, params []*ast.Param, i int) *ast.Param {
	if i >= len(params) {
		return nil
	}
	p := params[i]
	if p.Variadic && !call.Spread || !g.prog.Types.IsView(p.Type) {
		return nil
	}
	return p
}

// viewArg renders arg, passed to a parameter of the view type view in
// call: a view passed on, as it is; a string's bytes, []uint8(s), as the
// string's pointer and length; a slice by sliceExpr, which copies an
// array as this does; a list as its buffer and length (see listView); or an array, which the checker has made
// sure is a place, as a pointer to its first element and its length. The
// array is copied into a temporary first, and the view points at the
// copy, when the callee could otherwise see it change during the call
// (see viewCopies), so it sees the array as it was when the argument was
// evaluated, as it would a plain argument's copy. A nil call is a view
// returned rather than passed, which the checker has made sure points
// into its function's caller's data, so it is never copied.
func (g *generator) viewArg(
	arg ast.Expr,
	view types.Type,
	call *ast.Call,
) (string, []diag.Diagnostic) {
	if conv, ok := arg.(*ast.Call); ok && conv.Conv != types.Invalid {
		return g.bytesView(conv, view)
	}
	if s, ok := arg.(*ast.Slice); ok {
		return g.sliceExpr(s, call)
	}
	typ := ast.TypeOf(arg)
	if g.prog.Types.IsList(typ) {
		return g.listView(arg, view)
	}
	if !g.prog.Types.IsArray(typ) {
		return g.expr(arg, types.Invalid)
	}
	place, diags := g.expr(arg, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	n := g.prog.Types.Len(typ)
	if call == nil || !g.viewCopies(arg, call) {
		return fmt.Sprintf("(%s){%s.e, %d}", g.viewName(view), place, n), nil
	}
	t := g.temp("c")
	g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", g.arrayName(typ), t))
	return fmt.Sprintf("(%s = %s, (%s){%s.e, %d})", t, place, g.viewName(view), t, n), nil
}

// listView renders l, a list the checker has made sure is a place, as a
// view of type view of its elements: its buffer and its length. No copy
// is ever taken: the checker rejects a call that could change the list
// while it is viewed (see typeck's checkListBorrows). The place is read
// twice, which, having no call in it that could run twice, is the same
// list both times.
func (g *generator) listView(l ast.Expr, view types.Type) (string, []diag.Diagnostic) {
	place, diags := g.expr(l, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	return fmt.Sprintf("(%s){%s.ptr, %s.len}", g.viewName(view), place, place), nil
}

// bytesView renders []uint8(s), conv, as a view of type view of the
// string's bytes: its pointer and its length. A literal is its pooled
// array and that array's length, as a string value of it is; a name is
// read twice, which is the same string both times; anything else is held
// in a temporary first, so it is evaluated once.
func (g *generator) bytesView(conv *ast.Call, view types.Type) (string, []diag.Diagnostic) {
	if lit, ok := conv.Args[0].(*ast.StrLit); ok {
		return fmt.Sprintf("(%s){%s}", g.viewName(view), g.strParts(lit.Value)), nil
	}
	s, diags := g.expr(conv.Args[0], types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	if _, isName := conv.Args[0].(*ast.Ident); isName {
		return fmt.Sprintf("(%s){%s.ptr, %s.len}", g.viewName(view), s, s), nil
	}
	t := g.temp("s")
	g.frame.temps = append(g.frame.temps, fmt.Sprintf("tack_string %s;", t))
	return fmt.Sprintf("(%s = %s, (%s){%s.ptr, %s.len})", t, s, g.viewName(view), t, t), nil
}

// viewCopies reports whether the array place, viewed by an argument of
// call, must be copied for the view: it is part of a mutable global,
// which any function can assign during the call, or call's arguments pass
// a place as mut, directly or in a call nested in one, that may overlap
// it, which the callee, or that nested call before it, could change, or
// hold a closure, at any depth, that may write the variable it is part
// of, which the call the closure is passed to could run.
// Places compare as paths, as the checker compares two mut arguments, but
// more simply: two different fields never overlap, and two elements
// always may. A copy where none was needed changes nothing a program can
// see.
func (g *generator) viewCopies(place ast.Expr, call *ast.Call) bool {
	root := ast.Root(place)
	if v, isVar := root.Def.(*ast.Var); isVar && g.globals[v] && v.Mutable {
		return true
	}
	found := false
	for _, a := range call.Args {
		ast.Walk(a, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.MutArg:
				found = found || g.mayOverlap(place, x.X)
			case *ast.FuncLit:
				for _, cp := range x.Captures {
					found = found || cp.Write && cp.Decl == root.Def
				}
				return false
			}
			return !found
		})
	}
	return found
}

// mayOverlap reports whether the places a and b may share memory: they
// start from the same variable or parameter, and no field of one differs
// from the other's at the same depth.
func (g *generator) mayOverlap(a, b ast.Expr) bool {
	rootA, stepsA := g.placePath(a)
	rootB, stepsB := g.placePath(b)
	if rootA == nil || rootA != rootB {
		return false
	}
	for i := range min(len(stepsA), len(stepsB)) {
		if stepsA[i] != "" && stepsB[i] != "" && stepsA[i] != stepsB[i] {
			return false
		}
	}
	return true
}

// placePath returns the variable or parameter a place starts from, and
// a step for each field and element after it, outermost first: a field's
// name, the one a fields loop's v.[f] stands for in the block being
// lowered among them, or "" for an element.
func (g *generator) placePath(place ast.Expr) (ast.Decl, []string) {
	var steps []string
	for {
		switch x := place.(type) {
		case *ast.Selector:
			steps = append(steps, g.fieldName(x))
			place = x.X
		case *ast.Index:
			steps = append(steps, "")
			place = x.X
		case *ast.Ident:
			slices.Reverse(steps)
			return x.Def, steps
		default:
			return nil, nil
		}
	}
}

// callArgs renders call's arguments for a call to fn, whose parameters
// they are passed to: one passed as a view, a spread included, by
// viewArg, a view passed as any by viewArg too, as a view of its own
// type, so an array it slices is copied when the call could change it,
// one passed to a scope parameter by scopeArg, and any other, a value for
// a variadic parameter included, as itself.
func (g *generator) callArgs(call *ast.Call, fn *ast.Func) ([]string, []diag.Diagnostic) {
	params := paramsOf(fn)
	args := make([]string, len(call.Args))
	guard := g.guardOwned(call.Args)
	defer guard.done()
	for i, a := range call.Args {
		var (
			expr  string
			diags []diag.Diagnostic
		)
		if p := g.viewParamAt(call, params, i); p != nil {
			expr, diags = g.viewArg(a, p.Type, call)
		} else if i < len(params) && g.scopeFunc(params[i]) {
			expr, diags = g.scopeArg(a, params[i].Type)
		} else if typ := ast.TypeOf(a); g.prog.Types.IsView(typ) && g.boxesAt(call, params, i) {
			expr, diags = g.viewArg(a, typ, call)
		} else {
			expr, diags = g.expr(a, ast.TypeOf(a))
		}
		if len(diags) > 0 {
			return nil, diags
		}
		args[i] = guard.hold(i, expr)
	}
	return args, nil
}

// paramsOf returns fn's parameters, none for a nil fn, a builtin.
func paramsOf(fn *ast.Func) []*ast.Param {
	if fn == nil {
		return nil
	}
	return fn.Params
}

// packs reports whether a call to a function with parameters params
// passes values to a variadic parameter one by one, which the call then
// packs into a view (see packed), rather than spreading a view into it.
func packs(call *ast.Call, params []*ast.Param) bool {
	return len(params) > 0 && params[len(params)-1].Variadic && !call.Spread
}

// packed renders the view of type view that a call passes to a variadic
// parameter, of the values it gives it, already rendered and evaluated in
// order as values: a compound literal array of them, on the caller's
// stack, (const T[n]){a, b, c}, which lives to the end of the block
// holding the call, so for the whole call, and the view of it, with n as
// its length. With no values, the view is {0, 0}: a null pointer, which
// nothing reads, since its length is 0, and which a slice of it never
// moves (see slice.go), as for the bytes of a zero string; C has no array
// of no elements.
func (g *generator) packed(view types.Type, values []string) string {
	if len(values) == 0 {
		return fmt.Sprintf("(%s){0, 0}", g.viewName(view))
	}
	return fmt.Sprintf(
		"(%s){(const %s[%d]){%s}, %d}",
		g.viewName(view),
		g.cType(g.prog.Types.Elem(view)),
		len(values),
		strings.Join(values, ", "),
		len(values),
	)
}
