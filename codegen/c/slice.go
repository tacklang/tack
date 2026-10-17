package c

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// A slice, x[lo:hi], lowers to a call to a helper that builds the string or
// view it is from the one it slices: the pointer moved past lo elements and
// the length hi - lo, or the length less lo when hi is left out, which the
// helper then reads off what it slices, so it is evaluated once:
//
//	static tack_string tack_slice_string(tack_string s, int64_t lo, int64_t hi) {
//	    return (tack_string){lo == 0 ? s.ptr : s.ptr + lo, hi - lo};
//	}
//
// The pointer is not moved for lo == 0, since the zero string inside a
// zeroed struct or array, and the zero view the checked form gives, have a
// null pointer, and adding even 0 to a null pointer is undefined in C;
// past 0, lo is at most the length, which is not 0, so the pointer is not
// null. An array is viewed whole first, (tack_view_3){a.e, n}, and that
// view sliced. The checker has proven the bounds in range, 0 <= lo <= hi
// <= len(x), so casting each to int64_t keeps its value. What is sliced
// and the bounds are evaluated left to right, as a call's arguments are
// (see sequenced).

// sliceHelper is one helper a program's slices call: the string or view
// type it slices, and whether its high bound is left out.
type sliceHelper struct {
	typ  types.Type
	from bool
}

// name returns the helper's C name: tack_slice_ and the type, string or
// the view's ID, with from_ before it for a high bound left out.
func (h sliceHelper) name() string {
	t := "string"
	if h.typ != types.String {
		t = fmt.Sprint(h.typ)
	}
	if h.from {
		return "tack_slice_from_" + t
	}
	return "tack_slice_" + t
}

// sliceHelpers lists each helper the program's slices call, a slice bound
// with ok included, by type and then with a high bound before without, so
// the helpers are emitted the same way every time.
func (g *generator) sliceHelpers() []sliceHelper {
	used := make(map[sliceHelper]bool)
	g.walk(func(n ast.Node) bool {
		if s, ok := n.(*ast.Slice); ok {
			used[sliceHelper{typ: s.Type, from: s.Hi == nil}] = true
		}
		return true
	})
	helpers := make([]sliceHelper, 0, len(used))
	for h := range used {
		helpers = append(helpers, h)
	}
	slices.SortFunc(helpers, func(a, b sliceHelper) int {
		if a.typ != b.typ {
			return int(a.typ) - int(b.typ)
		}
		if a.from == b.from {
			return 0
		}
		if a.from {
			return 1
		}
		return -1
	})
	return helpers
}

// emitSliceHelper writes the helper h names, which slices a string or a
// view of its type, as the comment above describes.
func (g *generator) emitSliceHelper(h sliceHelper) {
	ctype := g.cType(h.typ)
	params, hi := "int64_t lo, int64_t hi", "hi"
	if h.from {
		params, hi = "int64_t lo", "s.len"
	}
	fmt.Fprintf(&g.buf, "static %s %s(%s s, %s) {\n", ctype, h.name(), ctype, params)
	fmt.Fprintf(&g.buf, "    return (%s){lo == 0 ? s.ptr : s.ptr + lo, %s - lo};\n}\n\n", ctype, hi)
}

// elementExpr renders e, an element, x[i], or a slice not passed to a
// view parameter, x[i:j].
func (g *generator) elementExpr(e ast.Expr) (string, []diag.Diagnostic) {
	if s, ok := e.(*ast.Slice); ok {
		return g.sliceExpr(s, nil)
	}
	ix, _ := e.(*ast.Index)
	return g.indexExpr(ix)
}

// sliceExpr renders the slice s, proven in range. call is the call s is an
// argument of, to a view parameter, or nil: an array sliced there is
// copied first when viewArg would copy it (see sliceBase).
func (g *generator) sliceExpr(s *ast.Slice, call *ast.Call) (string, []diag.Diagnostic) {
	operands, rendered, ctypes, diags := g.sliceOperands(s, call)
	if len(diags) > 0 {
		return "", diags
	}
	h := sliceHelper{typ: s.Type, from: s.Hi == nil}
	return g.sequencedAs(operands, rendered, ctypes, func(ops []string) string {
		args := []string{ops[0], "0"}
		for i, b := range operands[1:] {
			if b == s.Lo {
				args[1] = "(int64_t)" + ops[i+1]
			} else {
				args = append(args, "(int64_t)"+ops[i+1])
			}
		}
		return fmt.Sprintf("%s(%s)", h.name(), strings.Join(args, ", "))
	}), nil
}

// sliceOperands renders what s slices, as a string or a view (see
// sliceBase), and each bound written, in order, with the C type a
// temporary holding each would have.
func (g *generator) sliceOperands(
	s *ast.Slice,
	call *ast.Call,
) (operands []ast.Expr, rendered, ctypes []string, diags []diag.Diagnostic) {
	base, diags := g.sliceBase(s, call)
	if len(diags) > 0 {
		return nil, nil, nil, diags
	}
	operands, rendered = []ast.Expr{s.X}, []string{base}
	ctypes = []string{g.cType(s.Type)}
	for _, b := range []ast.Expr{s.Lo, s.Hi} {
		if b == nil {
			continue
		}
		v, diags := g.expr(b, types.Invalid)
		if len(diags) > 0 {
			return nil, nil, nil, diags
		}
		operands, rendered = append(operands, b), append(rendered, v)
		ctypes = append(ctypes, g.operandType(b))
	}
	return operands, rendered, ctypes, nil
}

// sliceBase renders what s slices as the string or view the helper takes:
// a string or a view as it is, a slice of one through sliceExpr, a list
// as a view of its elements (see listView), and an
// array, which the checker has made sure is a place, as a view of all of
// it, its first element and its length. When s is an argument of call,
// the array is copied into a temporary first, and the view is of the
// copy, where viewCopies says a view of the array itself would be, so
// the callee sees the array as it was when the argument was evaluated.
func (g *generator) sliceBase(s *ast.Slice, call *ast.Call) (string, []diag.Diagnostic) {
	if inner, ok := s.X.(*ast.Slice); ok {
		return g.sliceExpr(inner, call)
	}
	typ := ast.TypeOf(s.X)
	if g.prog.Types.IsList(typ) {
		return g.listView(s.X, s.Type)
	}
	if !g.prog.Types.IsArray(typ) {
		return g.expr(s.X, types.Invalid)
	}
	place, diags := g.expr(s.X, types.Invalid)
	if len(diags) > 0 {
		return "", diags
	}
	n := g.prog.Types.Len(typ)
	view := g.viewName(s.Type)
	if call == nil || !g.viewCopies(s.X, call) {
		return fmt.Sprintf("(%s){%s.e, %d}", view, place, n), nil
	}
	t := g.temp("c")
	g.frame.temps = append(g.frame.temps, fmt.Sprintf("%s %s;", g.arrayName(typ), t))
	return fmt.Sprintf("(%s = %s, (%s){%s.e, %d})", t, place, view, t, n), nil
}

// sliceValues lowers t, ok := s[lo:hi], a string's slice: the string is
// held in a temporary, and each bound written in one of its own type, in
// that order, and the slice is taken only when the bounds are in range,
// giving "" otherwise. Each test compares values, whatever the bounds'
// types (see intComparison), and only those a bound's type can fail are
// made: 0 <= lo for a signed lo, lo <= hi, or 0 <= hi for a signed hi when
// lo is left out, or lo <= len(s) when hi is, and hi <= len(s).
func (g *generator) sliceValues(s *ast.Slice) (multiSource, []diag.Diagnostic) {
	operands, rendered, _, diags := g.sliceOperands(s, nil)
	if len(diags) > 0 {
		return multiSource{}, diags
	}
	src := multiSource{temps: []multiTemp{{typ: "tack_string", init: rendered[0]}}}
	for i, b := range operands[1:] {
		src.temps = append(src.temps, multiTemp{typ: g.cType(ast.TypeOf(b)), init: rendered[i+1]})
	}
	zero := g.zeroValue(types.String)
	h := sliceHelper{typ: types.String, from: s.Hi == nil}
	src.field = func(tmps []string, i int) string {
		x, lo, hi := tmps[0], "0", ""
		args := []string{x}
		if s.Lo != nil {
			lo = tmps[1]
			args = append(args, "(int64_t)"+lo)
		} else {
			args = append(args, "0")
		}
		if s.Hi != nil {
			hi = tmps[len(tmps)-1]
			args = append(args, "(int64_t)"+hi)
		}
		in := g.sliceIn(s, x, lo, hi)
		if i == 1 {
			return in
		}
		return fmt.Sprintf("(%s ? %s(%s) : %s)", in, h.name(), strings.Join(args, ", "), zero)
	}
	return src, nil
}

// sliceIn renders the C test of whether the bounds of s, held as lo and
// hi, are in range for the string held as x, as sliceValues describes. A
// bound left out is "".
func (g *generator) sliceIn(s *ast.Slice, x, lo, hi string) string {
	length := x + ".len"
	var tests []string
	nonNegative := func(b ast.Expr, v string) {
		if lowest, _ := bounds(ast.TypeOf(b)); lowest.IsNegative() {
			tests = append(tests, v+" >= 0")
		}
	}
	if s.Lo != nil {
		nonNegative(s.Lo, lo)
	}
	switch {
	case s.Lo != nil && s.Hi != nil:
		tests = append(tests, intComparison(ast.Le, ast.TypeOf(s.Lo), ast.TypeOf(s.Hi), lo, hi))
	case s.Hi != nil:
		nonNegative(s.Hi, hi)
	case s.Lo != nil:
		tests = append(tests, intComparison(ast.Le, ast.TypeOf(s.Lo), types.Int64, lo, length))
	}
	if s.Hi != nil {
		tests = append(tests, intComparison(ast.Le, ast.TypeOf(s.Hi), types.Int64, hi, length))
	}
	if len(tests) == 0 {
		return "true"
	}
	return "(" + strings.Join(tests, " && ") + ")"
}
