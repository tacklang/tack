package c

import (
	"fmt"
	"slices"

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
		viewName(typ),
	)
}

// viewName is the name of the C struct a view type lowers to, made from
// its ID as an array's is, tack_view_ and a digit.
func viewName(typ types.Type) string {
	return fmt.Sprintf("tack_view_%d", typ)
}

// hasLen reports whether typ is a string or a view, which lower to a
// pointer and a length, read as x.ptr[i] and x.len.
func (g *generator) hasLen(typ types.Type) bool {
	return typ == types.String || g.prog.Types.IsView(typ)
}

// argTypes returns the C type of each of call's arguments as callExpr
// renders them: an argument's own type, a pointer to it for a mut
// argument, or, for one passed to a view parameter, the view's.
func (g *generator) argTypes(call *ast.Call, params []*ast.Param) []string {
	out := make([]string, len(call.Args))
	for i, a := range call.Args {
		if i < len(params) && g.prog.Types.IsView(params[i].Type) {
			out[i] = g.cType(params[i].Type)
			continue
		}
		out[i] = g.operandType(a)
	}
	return out
}

// viewArg renders arg, passed to a parameter of the view type view in
// call: a view passed on, as it is; a string's bytes, []uint8(s), as the
// string's pointer and length; a slice by sliceExpr, which copies an
// array as this does; or an array, which the checker has made
// sure is a place, as a pointer to its first element and its length. The
// array is copied into a temporary first, and the view points at the
// copy, when the callee could otherwise see it change during the call
// (see viewCopies), so it sees the array as it was when the argument was
// evaluated, as it would a plain argument's copy.
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
	if !g.prog.Types.IsArray(typ) {
		return g.expr(arg, types.Invalid)
	}
	place, diags := g.expr(arg, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	n := g.prog.Types.Len(typ)
	if !g.viewCopies(arg, call) {
		return fmt.Sprintf("(%s){%s.e, %d}", viewName(view), place, n), nil
	}
	t := g.temp("c")
	g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", arrayName(typ), t))
	return fmt.Sprintf("(%s = %s, (%s){%s.e, %d})", t, place, viewName(view), t, n), nil
}

// bytesView renders []uint8(s), conv, as a view of type view of the
// string's bytes: its pointer and its length. A literal is its pooled
// array and that array's length, as a string value of it is; a name is
// read twice, which is the same string both times; anything else is held
// in a temporary first, so it is evaluated once.
func (g *generator) bytesView(conv *ast.Call, view types.Type) (string, []diag.Diagnostic) {
	if lit, ok := conv.Args[0].(*ast.StrLit); ok {
		return fmt.Sprintf("(%s){%s}", viewName(view), g.strParts(lit.Value)), nil
	}
	s, diags := g.expr(conv.Args[0], types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	if _, isName := conv.Args[0].(*ast.Ident); isName {
		return fmt.Sprintf("(%s){%s.ptr, %s.len}", viewName(view), s, s), nil
	}
	t := g.temp("s")
	g.frame.temps = append(g.frame.temps, fmt.Sprintf("tack_string %s;", t))
	return fmt.Sprintf("(%s = %s, (%s){%s.ptr, %s.len})", t, s, viewName(view), t, t), nil
}

// viewCopies reports whether the array place, viewed by an argument of
// call, must be copied for the view: it is part of a mutable global,
// which any function can assign during the call, or call's arguments pass
// a place as mut, directly or in a call nested in one, that may overlap
// it, which the callee, or that nested call before it, could change.
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
			if m, ok := n.(*ast.MutArg); ok && mayOverlap(place, m.X) {
				found = true
			}
			return !found
		})
	}
	return found
}

// mayOverlap reports whether the places a and b may share memory: they
// start from the same variable or parameter, and no field of one differs
// from the other's at the same depth.
func mayOverlap(a, b ast.Expr) bool {
	rootA, stepsA := placePath(a)
	rootB, stepsB := placePath(b)
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
// name, or "" for an element.
func placePath(place ast.Expr) (ast.Decl, []string) {
	var steps []string
	for {
		switch x := place.(type) {
		case *ast.Selector:
			steps = append(steps, x.Name)
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
// they are passed to: a view parameter's by viewArg, and any other as
// itself.
func (g *generator) callArgs(call *ast.Call, fn *ast.Func) ([]string, []diag.Diagnostic) {
	var params []*ast.Param
	if fn != nil {
		params = fn.Params
	}
	args := make([]string, len(call.Args))
	for i, a := range call.Args {
		var (
			expr  string
			diags []diag.Diagnostic
		)
		if i < len(params) && g.prog.Types.IsView(params[i].Type) {
			expr, diags = g.viewArg(a, params[i].Type, call)
		} else {
			expr, diags = g.expr(a, ast.TypeOf(a))
		}
		if len(diags) > 0 {
			return nil, diags
		}
		args[i] = expr
	}
	return args, nil
}
