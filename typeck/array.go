package typeck

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// arrayType returns the array type a written [n]T denotes, adding it to the
// table. The length must be a constant: a constant expression, an immutable
// variable holding one, or len of an array or a string literal, and must
// not be negative. Its elements must be copyable, so no list, and no
// struct holding one (see requireCopyable). A problem is reported and
// gives types.Invalid.
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
	if !c.requireCopyable("an array", elem, a.Span) {
		return types.Invalid
	}
	typ := c.table.Array(elem, n)
	c.noteArraySize(typ, a.Span)
	return typ
}

// arrayLength checks the length of an array type and returns it. In a
// struct's field, the length must name no variable, not even an immutable
// global holding a constant, so a struct's layout never depends on a
// variable's initializer; that is checked before the length is, so a
// global named there is never checked on its behalf.
func (c *Checker) arrayLength(e ast.Expr) (int64, bool) {
	if c.inFields {
		if v := firstVar(e); v != nil {
			where := "a struct field"
			if c.inUnion {
				where = "a union's case"
			}
			c.errorf(v.Span, "array length in %s must not name variable %s", where, v.Name)
			return 0, false
		}
	}
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

// firstVar returns the first name in e, in source order, that refers to a
// variable, or nil if there is none.
func firstVar(e ast.Expr) *ast.Ident {
	var found *ast.Ident
	ast.Walk(e, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if id, ok := n.(*ast.Ident); ok {
			if _, isVar := id.Def.(*ast.Var); isVar {
				found = id
			}
		}
		return true
	})
	return found
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

	if vt, ok := lit.TypeExpr.(*ast.ViewType); ok {
		return c.literalOf(lit, c.viewType(vt))
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
			c.typeName(typ),
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
			c.typeName(typ),
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

// index checks an element read as a value, x[i], and returns its type. x
// may be a call returning a view.
func (c *Checker) index(ix *ast.Index) types.Type {
	c.allowViewCall(ix.X)
	ix.Type = c.element(ix, c.expr(ix.X), false)
	return ix.Type
}

// element checks the index of x[i], where x has type base, and returns the
// element's type. base must be an array, a string, a view or a list, and the index an
// integer that is in range: a constant is checked against the array's
// length here. write says x[i] is being assigned, which only matters for
// what the error suggests; a string's byte is never assigned, which
// elementTarget reports first. An element of a list or a view read in
// place while its index, or the value it is being assigned, can change
// what it is read from is reported instead of any proof the index needs
// (see checkIndexBorrows and checkAssignBorrows).
func (c *Checker) element(ix *ast.Index, base types.Type, write bool) types.Type {
	// The index is no part of the target being assigned, even when x[i]
	// is: a mut argument in it is the index's own (see checkIndexBorrows).
	assigning := c.assigning
	c.assigning = nil
	idx := c.expr(ix.Index)
	c.assigning = assigning
	if c.table.IsMap(base) {
		// A key is used as the map's key type, an untyped constant
		// taking it, as a value assigned does.
		return c.mapElement(ix, base, idx, write)
	}
	idx = c.valueOf(ix.Index, idx)
	if base == types.Invalid {
		return types.Invalid
	}
	if elem, ok := c.lengthElem(base); ok {
		return c.lengthElement(ix, base, idx, elem, write)
	}
	if !c.table.IsArray(base) {
		c.errorf(ix.X.Spanned(), "cannot index %s", c.typeName(base))
		return types.Invalid
	}

	elem, n := c.table.Elem(base), c.table.Len(base)
	switch {
	case !c.indexType(ix, idx):
	case c.checkedIndex == ix:
		// Bound with ok, the index needs no proof; a constant one is still
		// checked here, since one out of range always fails.
		if v, known := c.knownValue(ix.Index); known && !c.constantInRange(v, n) {
			c.errorf(ix.Index.Spanned(), "index %s is out of range for %s", v, c.typeName(base))
			return types.Invalid
		}
	default:
		c.requireInRange(ix, base, n, write)
	}
	return elem
}

// lengthElement checks the index idx of ix, an element of type elem of a
// string, a view or a list of type base, as element does, and returns
// elem. An index that can change the list or the view it reads, or an
// element assigned a value that can change its list, is reported instead
// of any proof the index needs, and ends the assignment's check.
func (c *Checker) lengthElement(ix *ast.Index, base, idx, elem types.Type, write bool) types.Type {
	if c.checkIndexBorrows(ix) || c.checkAssignBorrows(ix, base) {
		c.assigning = nil
		return elem
	}
	return c.stringElement(ix, idx, elem, write)
}

// lengthElem returns what an element of a value of type base is, when
// base is a string, a view or a list, whose length is known only at run
// time, if at all: a string's byte, a uint8, or the view's or list's
// element type; and whether it is one.
func (c *Checker) lengthElem(base types.Type) (types.Type, bool) {
	switch {
	case base == types.String:
		return types.Uint8, true
	case c.table.IsView(base), c.table.IsList(base):
		return c.table.Elem(base), true
	default:
		return types.Invalid, false
	}
}

// indexType reports whether idx, the type of the index of ix, an element
// of an array, a string or a view, is one an index can have, and reports
// it when it is not: any integer, a target-width one included. Every
// proof compares an index with a length by value, and a target-width
// index's values are bounded by its widest, 64 bits (types.MaxBits), so
// one is proven as a uint64 is. An Invalid idx has been reported already.
func (c *Checker) indexType(ix *ast.Index, idx types.Type) bool {
	switch {
	case idx == types.Invalid:
		return false
	case !types.IsInteger(idx):
		c.errorf(ix.Index.Spanned(), "index must be an integer, got %s", c.typeName(idx))
		return false
	default:
		return true
	}
}

// constantInRange reports whether the constant index v is in [0, n).
func (c *Checker) constantInRange(v constant.Int, n int64) bool {
	return !v.IsNegative() && v.Cmp(constant.FromInt64(n)) < 0
}

// indexResults checks an element bound to want names, which must be two:
// the element, or its type's zero value when the index is out of range, and
// whether it was in range. The form needs no proof, and is allowed where
// one exists, when ok is always true; a constant index out of range, which
// always fails, is reported by element. It copies the element, so an
// element of an owned type, which is only ever read in place, is
// reported, with the guard that lets it be used where it is, or, for a
// view a call returns, whose elements no guard proves, the range that
// does; and an element of another type with no zero value, an Allocator
// or an array or struct holding one, has nothing to give when out of
// range, and is reported.
func (c *Checker) indexResults(ix *ast.Index, want int, span ast.Span) ([]types.Type, bool) {
	c.checkedIndex = ix
	elem := c.expr(ix)
	c.checkedIndex = nil
	if elem == types.Invalid {
		return nil, false
	}
	if c.table.IsMap(ast.TypeOf(ix.X)) {
		return c.mapResults(ix, elem, want, span)
	}
	if c.table.Owned(elem) && c.isViewCall(ix.X) {
		c.errorf(
			span,
			"the checked form cannot read an element of type %s: it would copy it out of %s; "+
				"range over %s to use its elements in place",
			c.typeName(elem),
			c.text(ix.X.Spanned()),
			c.text(ix.X.Spanned()),
		)
		return nil, false
	}
	if c.table.Owned(elem) {
		x := c.text(ix.X.Spanned())
		guard := inBounds(ix.Index, c.text(ix.Index.Spanned()), "len("+x+")")
		if v, known := c.knownValue(ix.Index); known {
			guard = "len(" + x + ") > " + v.String()
		}
		c.errorf(
			span,
			"the checked form cannot read an element of type %s: it would copy it out of %s; "+
				"check %s first and use %s in place",
			c.typeName(elem),
			x,
			guard,
			c.text(ix.Spanned()),
		)
		return nil, false
	}
	if !c.table.HasZero(elem) {
		c.errorf(span, "the checked form cannot read an element of type %s: "+
			"it has no zero value to give when the index is out of range", c.typeName(elem))
		return nil, false
	}

	if want != 2 {
		c.errorf(span, "assignment mismatch: %d variables but an index gives 2 values", want)
		return nil, false
	}
	return []types.Type{elem, types.Bool}, true
}

// requireInRange reports the index of ix, an element of an array of type
// typ, with n elements, when it is not proven to be in range: a constant
// outside it, or anything else no fact covers, with notes that explain why
// no fact did.
func (c *Checker) requireInRange(ix *ast.Index, typ types.Type, n int64, write bool) {
	index := ix.Index
	if v, ok := c.knownValue(index); ok {
		if !c.constantInRange(v, n) {
			c.errorf(index.Spanned(), "index %s is out of range for %s", v, c.typeName(typ))
		}
		return
	}
	if c.inRange(index, n) {
		return
	}
	msg, notes := c.indexError(ix, fmt.Sprint(n), func(i ast.Expr) []diag.Diagnostic {
		e := c.newExtent(ix.X, "element", n, true)
		notes := c.explain(c.indexProof(i, e, func() bool { return c.inRange(index, n) }))
		return append(notes, c.indexRelationNotes(i, ix.X)...)
	})
	if !write {
		msg += orUse("v, ok := ...["+c.operandName(index)+"]", strings.HasSuffix(msg, " first"))
	}
	c.errorNotes(index.Spanned(), notes, "%s", msg)
}

// indexError returns the error for the index of ix, not a known value, that
// no fact proves in range, before any mention of the checked form, and the
// notes that explain it. A subject, a variable or a path below an
// immutable one, gets the check that would prove it, below bound, with no
// lower bound for an unsigned one, except a mutable global, which no check
// proves; explain gives the notes for a subject. An index inside an
// element being bound with ok is also explained.
func (c *Checker) indexError(
	ix *ast.Index,
	bound string,
	explain func(i ast.Expr) []diag.Diagnostic,
) (string, []diag.Diagnostic) {
	msg, notes := "index may be out of range", c.operandNotes(ix.Index)
	isSubject := c.isSubject(ix.Index)
	name := c.nameOf(ix.Index)
	switch v, isGlobal := c.mutableGlobal(ix.Index); {
	case isGlobal:
		msg, notes = "index "+c.operandName(ix.Index)+" may be out of range", globalNote(v)
	case isSubject && bound == "":
		msg, notes = "index "+name+" may be out of range", explain(ix.Index)
	case isSubject:
		msg = "index " + name + " may be out of range; " + boundsAdvice(ix.Index, name, bound)
		notes = explain(ix.Index)
	}
	return msg, append(notes, c.outerIndexNote(ix)...)
}

// boundsAdvice returns what to do first to prove i, a subject called name,
// below bound: check it. Any integer compares with bound by value, so the
// check is the same whatever i's type.
func boundsAdvice(i ast.Expr, name, bound string) string {
	return "check " + inBounds(i, name, bound) + " first"
}

// inBounds writes the check that proves i, a subject called name, below
// bound and, unless its type is unsigned and so never negative, at least
// 0.
func inBounds(i ast.Expr, name, bound string) string {
	if types.IsUnsigned(ast.TypeOf(i)) {
		return name + " < " + bound
	}
	return "0 <= " + name + " && " + name + " < " + bound
}

// lenCall checks a call to the builtin len, which takes one array, string,
// view, list or map, a slice of an array, a view or a list included, and
// gives an int64, a map's being how many keys it holds. The length of an array, and of a string
// literal, is a constant, recorded on the call, unless working it out runs
// a call, which it must then still make; a view's or a list's is never known, as a
// string parameter's is not. A call whose arguments are wrong, or whose
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
	c.allowView(arg)
	got := c.expr(arg)
	switch {
	case got == types.Invalid:
		return types.Invalid
	case c.table.IsArray(got):
		if !hasCall(arg) {
			call.Const = constant.FromInt64(c.table.Len(got))
		}
	case got == types.String, c.table.IsView(got), c.table.IsList(got), c.table.IsMap(got):
		if lit, ok := arg.(*ast.StrLit); ok {
			call.Const = constant.FromInt64(int64(len(lit.Value)))
		}
	default:
		if got = c.valueOf(arg, got); got != types.Invalid {
			c.errorf(
				arg.Spanned(),
				"len expects an array, a string, a view, a list or a map, got %s",
				c.typeName(got),
			)
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
// integer, bool and string, an array or a list whose elements do, a
// struct whose fields all do, which a struct with none does, and a union
// whose payloads all do, which a union with none does. A view does not,
// nor an Allocator or an any.
func (c *Checker) comparable(typ types.Type) bool {
	return c.comparableIn(typ, make(map[types.Type]bool))
}

// comparableIn is comparable for typ, where seen holds the structs whose
// fields are being looked at further up, so that a struct containing
// itself, which is reported elsewhere, cannot loop.
func (c *Checker) comparableIn(typ types.Type, seen map[types.Type]bool) bool {
	switch {
	case types.IsNumeric(typ), typ == types.Bool, typ == types.String:
		return true
	case c.table.IsArray(typ), c.table.IsList(typ):
		return c.comparableIn(c.table.Elem(typ), seen)
	case c.table.IsStruct(typ), c.table.IsUnion(typ):
		if seen[typ] {
			return true
		}
		seen[typ] = true
		for _, part := range c.partTypes(typ) {
			if !c.comparableIn(part, seen) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// holdsFloat reports whether typ is a float type, or an array, a list, a
// view, a struct or a union holding one at any depth.
func (c *Checker) holdsFloat(typ types.Type) bool {
	return c.holdsFloatIn(typ, make(map[types.Type]bool))
}

// holdsFloatIn is holdsFloat for typ, where seen holds the structs and
// unions being looked at further up, as for comparableIn.
func (c *Checker) holdsFloatIn(typ types.Type, seen map[types.Type]bool) bool {
	switch {
	case types.IsFloat(typ):
		return true
	case c.table.IsArray(typ), c.table.IsList(typ), c.table.IsView(typ):
		return c.holdsFloatIn(c.table.Elem(typ), seen)
	case c.table.IsStruct(typ), c.table.IsUnion(typ):
		if seen[typ] {
			return false
		}
		seen[typ] = true
		return slices.ContainsFunc(c.partTypes(typ), func(part types.Type) bool {
			return c.holdsFloatIn(part, seen)
		})
	default:
		return false
	}
}

// partTypes returns the types a struct's fields, or a union's payloads,
// have, in order.
func (c *Checker) partTypes(typ types.Type) []types.Type {
	var parts []types.Type
	for _, f := range c.table.Fields(typ) {
		parts = append(parts, f.Type)
	}
	for _, uc := range c.table.Cases(typ) {
		if uc.Payload {
			parts = append(parts, uc.Type)
		}
	}
	return parts
}
