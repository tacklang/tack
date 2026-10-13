package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// arrayType returns the array type a written [n]T denotes, adding it to the
// table. The length must be a constant: a constant expression, an immutable
// variable holding one, or len of an array or a string literal, and must
// not be negative. A problem is reported and gives types.Invalid.
func (c *Checker) arrayType(a *ast.ArrayType) types.Type {
	elem := c.declaredType(a.Elem)
	if a.Len == nil {
		c.errorf(a.Span, "[...] can only be used in an array literal")
		return types.Invalid
	}
	n, ok := c.arrayLength(a.Len)
	if !ok || elem == types.Invalid {
		return types.Invalid
	}
	return c.table.Array(elem, n)
}

// arrayLength checks the length of an array type and returns it.
func (c *Checker) arrayLength(e ast.Expr) (int64, bool) {
	got := c.expr(e)
	if got == types.Invalid {
		return 0, false
	}
	v, known := c.knownValue(e)
	if !types.IsInteger(got) || !known {
		c.errorf(e.Spanned(), "array length must be a constant integer")
		return 0, false
	}
	if got == types.UntypedInt && !c.settle(e, types.Int64) {
		return 0, false
	}
	if v.IsNegative() {
		c.errorf(e.Spanned(), "array length must not be negative, got %s", v)
		return 0, false
	}
	return v.Int64(), true
}

// compositeLit checks an array or struct literal written with its type, or
// [...] for an array's length, and returns its type.
func (c *Checker) compositeLit(lit *ast.CompositeLit) types.Type {
	if lit.TypeExpr == nil {
		c.errorf(lit.Span, "a literal without a type needs an array literal around it")
		lit.Type = types.Invalid
		c.discardElems(lit)
		return types.Invalid
	}

	a, ok := lit.TypeExpr.(*ast.ArrayType)
	if !ok || a.Len != nil {
		return c.literalOf(lit, c.declaredType(lit.TypeExpr))
	}
	// [...]T{...} takes its length from its elements.
	elem := c.declaredType(a.Elem)
	if elem == types.Invalid {
		return c.literalOf(lit, types.Invalid)
	}
	return c.literalOf(lit, c.table.Array(elem, int64(len(lit.Elems))))
}

// literalOf checks the elements of a literal of type typ, an array or a
// struct. An array literal has exactly one element for each of the array's,
// each usable as its element type and none with a key. A literal element
// written without its type takes the element type, which must be an array
// or a struct.
func (c *Checker) literalOf(lit *ast.CompositeLit, typ types.Type) types.Type {
	lit.Type = typ
	switch {
	case typ == types.Invalid:
		c.discardElems(lit)
		return types.Invalid
	case c.table.IsStruct(typ):
		return c.structLit(lit, typ)
	case !c.table.IsArray(typ):
		c.errorf(
			lit.Span,
			"%s cannot be written as a literal; only arrays and structs can",
			c.table.Name(typ),
		)
		lit.Type = types.Invalid
		return types.Invalid
	}

	elem := c.table.Elem(typ)
	for _, e := range lit.Elems {
		c.arrayElem(e, elem)
	}
	if got, want := int64(len(lit.Elems)), c.table.Len(typ); got != want {
		c.errorf(
			lit.Span,
			"%s literal has %d %s, want %d",
			c.table.Name(typ),
			got,
			plural(int(got), "element"),
			want,
		)
	}
	return typ
}

// arrayElem checks one element e of an array literal whose element type is
// elem. A literal written without its type takes elem, when that is an
// array or a struct; a key is reported, since an array literal lists every
// element in order.
func (c *Checker) arrayElem(e ast.Expr, elem types.Type) {
	if kv, ok := e.(*ast.KeyValue); ok {
		c.errorf(kv.Span, "keys are not allowed in an array literal")
		c.discard(kv.Value)
		return
	}
	if inner, ok := e.(*ast.CompositeLit); ok && inner.TypeExpr == nil &&
		(c.table.IsArray(elem) || c.table.IsStruct(elem)) {
		c.literalOf(inner, elem)
		return
	}
	c.use(e, c.expr(e), elem, "in array literal")
}

// index checks an element read as a value, x[i], and returns its type.
func (c *Checker) index(ix *ast.Index) types.Type {
	ix.Type = c.element(ix, c.expr(ix.X), false)
	return ix.Type
}

// element checks the index of x[i], where x has type base, and returns the
// element's type. base must be an array or a string, and the index an
// integer that is in range: a constant is checked against the array's
// length here. write says x[i] is being assigned, which only matters for
// what the error suggests; a string's byte is never assigned, which
// elementTarget reports first.
func (c *Checker) element(ix *ast.Index, base types.Type, write bool) types.Type {
	idx := c.valueOf(ix.Index, c.expr(ix.Index))
	if base == types.Invalid {
		return types.Invalid
	}
	if base == types.String {
		return c.stringElement(ix, idx)
	}
	if !c.table.IsArray(base) {
		c.errorf(ix.X.Spanned(), "cannot index %s", c.table.Name(base))
		return types.Invalid
	}

	elem, n := c.table.Elem(base), c.table.Len(base)
	switch {
	case idx == types.Invalid:
	case !types.IsInteger(idx):
		c.errorf(ix.Index.Spanned(), "index must be an integer, got %s", c.table.Name(idx))
	case c.checkedIndex == ix:
		// Bound with ok, the index needs no proof; a constant one is still
		// checked here, since one out of range always fails.
		if v, known := c.knownValue(ix.Index); known && !c.constantInRange(v, n) {
			c.errorf(ix.Index.Spanned(), "index %s is out of range for %s", v, c.table.Name(base))
			return types.Invalid
		}
	default:
		c.requireInRange(ix.X, ix.Index, base, n, write)
	}
	return elem
}

// constantInRange reports whether the constant index v is in [0, n).
func (c *Checker) constantInRange(v constant.Int, n int64) bool {
	return !v.IsNegative() && v.Cmp(constant.FromInt64(n)) < 0
}

// indexResults checks an element bound to want names, which must be two:
// the element, or its type's zero value when the index is out of range, and
// whether it was in range. The form needs no proof, and is allowed where
// one exists, when ok is always true; a constant index out of range, which
// always fails, is reported by element.
func (c *Checker) indexResults(ix *ast.Index, want int, span ast.Span) ([]types.Type, bool) {
	c.checkedIndex = ix
	elem := c.expr(ix)
	c.checkedIndex = nil
	if elem == types.Invalid {
		return nil, false
	}

	if want != 2 {
		c.errorf(span, "assignment mismatch: %d variables but an index gives 2 values", want)
		return nil, false
	}
	return []types.Type{elem, types.Bool}, true
}

// requireInRange reports an index that is not proven to be in range for
// the array of, of type typ, with n elements: a constant outside it, or
// anything else no fact covers, with notes that explain why no fact did.
func (c *Checker) requireInRange(of, index ast.Expr, typ types.Type, n int64, write bool) {
	if v, ok := c.knownValue(index); ok {
		if !c.constantInRange(v, n) {
			c.errorf(index.Spanned(), "index %s is out of range for %s", v, c.table.Name(typ))
		}
		return
	}
	if c.inRange(index, n) {
		return
	}

	id, isName := index.(*ast.Ident)
	var notes []diag.Diagnostic
	if isName {
		e := extent{of: of, unit: "element", n: n, known: true}
		notes = c.explain(c.indexProof(id, e, func() bool { return c.inRange(index, n) }))
	}
	switch {
	case isName && write:
		c.errorNotes(
			index.Spanned(),
			notes,
			"index %s may be out of range; check 0 <= %s && %s < %d first",
			id.Name,
			id.Name,
			id.Name,
			n,
		)
	case isName:
		c.errorNotes(
			index.Spanned(),
			notes,
			"index %s may be out of range; check 0 <= %s && %s < %d first, or use v, ok := ...[%s]",
			id.Name,
			id.Name,
			id.Name,
			n,
			id.Name,
		)
	case write:
		c.errorf(index.Spanned(), "index may be out of range")
	default:
		c.errorf(index.Spanned(), "index may be out of range; use v, ok := ...[...]")
	}
}

// lenCall checks a call to the builtin len, which takes one array or string
// and gives an int64. The length of an array, and of a string literal, is a
// constant, recorded on the call, unless working it out runs a call, which
// it must then still make. A call whose arguments are wrong, or whose
// argument has no type, gives types.Invalid, so a length it was to give
// is not reported as unknown too.
func (c *Checker) lenCall(call *ast.Call) types.Type {
	if !c.checkArity(call, "len", 1) {
		for _, arg := range call.Args {
			c.expr(arg)
		}
		return types.Invalid
	}

	arg := call.Args[0]
	got := c.expr(arg)
	switch {
	case got == types.Invalid:
		return types.Invalid
	case c.table.IsArray(got):
		if !hasCall(arg) {
			call.Const = constant.FromInt64(c.table.Len(got))
		}
	case got == types.String:
		if lit, ok := arg.(*ast.StrLit); ok {
			call.Const = constant.FromInt64(int64(len(lit.Value)))
		}
	default:
		if got = c.valueOf(arg, got); got != types.Invalid {
			c.errorf(arg.Spanned(), "len expects an array or a string, got %s", c.table.Name(got))
		}
		return types.Invalid
	}
	return types.Int64
}

// hasCall reports whether evaluating e calls a function, so its value
// cannot be known without running it. A len that is itself constant does
// not count.
func hasCall(e ast.Expr) bool {
	found := false
	ast.Walk(e, func(n ast.Node) bool {
		if call, ok := n.(*ast.Call); ok && call.Const == nil {
			found = true
		}
		return !found
	})
	return found
}

// comparable reports whether values of type typ support == and !=: every
// integer, bool and string, an array whose elements do, and a struct whose
// fields all do, which a struct with none does.
func (c *Checker) comparable(typ types.Type) bool {
	return c.comparableIn(typ, make(map[types.Type]bool))
}

// comparableIn is comparable for typ, where seen holds the structs whose
// fields are being looked at further up, so that a struct containing
// itself, which is reported elsewhere, cannot loop.
func (c *Checker) comparableIn(typ types.Type, seen map[types.Type]bool) bool {
	switch {
	case types.IsInteger(typ), typ == types.Bool, typ == types.String:
		return true
	case c.table.IsArray(typ):
		return c.comparableIn(c.table.Elem(typ), seen)
	case c.table.IsStruct(typ):
		if seen[typ] {
			return true
		}
		seen[typ] = true
		for _, f := range c.table.Fields(typ) {
			if !c.comparableIn(f.Type, seen) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
