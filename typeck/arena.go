package typeck

import (
	"slices"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/types"
)

// An arena, arena[T], is an owned, append-only store of elements of type
// T, stored by value. make(arena[T], sentinel, alloc) makes one holding
// only sentinel, its element at id 0, which allocates nothing until add
// grows it through the Allocator alloc. Nothing removes, pops, swaps,
// slices or shrinks an arena's elements for as long as it lives, so an id
// it gave keeps naming the same element, and a lookup needs no proof.
//
// An id, id[T], names one element of an arena[T], and a span, span[T], a
// run of consecutive ones, its start and its length. Both are copyable,
// have == and != and a zero value, the sentinel's id and the empty span,
// and may be a map's key; neither has an order or arithmetic, and neither
// converts to or from an integer. The id and span types of two element
// types are two types.
//
// e, err := add(mut a, v) moves v into the arena a and gives its id, and
// s, err := add_all(mut a, x, y) or add_all(mut a, v...) its values' span,
// all or none, the empty span for none; err says the arena could not
// grow, out of memory or past 2^32 - 1 elements, and the arena is then as
// it was. Neither error can be discarded, and the id or span is held by
// its error, as any result beside an error is (see errors.go).
// reserve(mut a, n) makes room for n more elements, as a list's does.
//
// a[e] is the element e names, a place as a list's element is: read in
// place, a field of it read or written, assigned, which frees the element
// it replaces, passed as mut, given to replace, and never moved out. An id
// at or past len(a), from another arena or the zero id, names the
// sentinel. a[s] is a read-only view of the span's elements, under every
// rule of a view a call returns (see viewresult.go): passed to a view
// parameter, given to len, ranged over, indexed or sliced, and never
// stored; a span not wholly in a gives an empty view. len(a) counts the
// sentinel, and a range over an arena gives every id, the sentinel's
// first, with its element, read in place as a list's is.
//
// An arena is owned as a list is: moved, freed with its owner, its
// elements last first and its sentinel last, borrowed by a plain
// parameter and grown through a mut one, and no argument of a call may
// read it in place, an element or a span of it included, while another
// passes it as mut, since add may move its buffer (see exclusive.go). It
// has no zero value and no ==, and is not a global, an any, nor passed
// to or returned from an extern func, and nor is an id or a span.

// arenaType returns the arena type a written arena[T] denotes, adding it
// to the table, and list[T] and []T with it (see types.Table.Arena). Its element
// type may be any type, an owned one or a type parameter included, as a
// list's may; one that was reported gives types.Invalid.
func (c *Checker) arenaType(at *ast.ArenaType) types.Type {
	elem := c.declaredType(at.Elem)
	if elem == types.Invalid {
		return types.Invalid
	}
	return c.table.Arena(elem)
}

// refType returns the id or span type a written id[T] or span[T] denotes,
// adding it to the table. Its element type may be any type an arena's
// may: an id holds no element, so a struct may hold an id of itself.
func (c *Checker) refType(rt *ast.RefType) types.Type {
	elem := c.declaredType(rt.Elem)
	if elem == types.Invalid {
		return types.Invalid
	}
	if rt.Name == "id" {
		return c.table.ID(elem)
	}
	return c.table.Span(elem)
}

// isAdd reports whether call calls the builtin add.
func isAdd(call *ast.Call) bool { return isBuiltinCall(call, "add") }

// isAddAll reports whether call calls the builtin add_all.
func isAddAll(call *ast.Call) bool { return isBuiltinCall(call, "add_all") }

// addsToArena reports whether call calls add or add_all, the builtins
// that give an id or a span and an error.
func addsToArena(call *ast.Call) bool { return isAdd(call) || isAddAll(call) }

// addCall checks a call to add, e, err := add(mut a, v), records the id
// type it gives on the call and its two results, the id and an error, in
// Call.ResultTypes, and returns the id type. Its first argument is the
// arena to grow, passed as mut, a place of an arena type; its second a
// value usable as the arena's element type, moved in when it is owned
// (see move.go). Anything else is reported, and gives types.Invalid.
func (c *Checker) addCall(call *ast.Call) types.Type {
	if !c.checkArity(call, "add", 2) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return types.Invalid
	}
	before := len(c.diags)
	elem := c.arenaTarget(call.Args[0], "add")
	c.checkArg(call.Args[1], param{name: "v", typ: elem}, "add")
	c.checkListBorrows(call)
	if len(c.diags) > before || elem == types.Invalid {
		return types.Invalid
	}
	return c.added(call, c.table.ID(elem))
}

// addAllCall checks a call to add_all, s, err := add_all(mut a, x, y) or
// s, err := add_all(mut a, v...), records the span type it gives on the
// call and its two results, the span and an error, in Call.ResultTypes,
// and returns the span type. add_all is variadic, as append is: its first
// argument is the arena to grow, passed as mut; after it come any number
// of values, none included, each usable as the element type and moved
// in when it is owned, or a view of them spread whole, which an owned
// element type, whose elements would be copied, cannot be. Anything else
// is reported, and gives types.Invalid.
func (c *Checker) addAllCall(call *ast.Call) types.Type {
	if !c.checkVariadicArity(call, "add_all", []param{{name: "a"}}, param{name: "elems"}) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return types.Invalid
	}
	before := len(c.diags)
	elem := c.arenaTarget(call.Args[0], "add_all")
	if call.Spread {
		c.appendSpread(call.Args[1], elem, "add_all")
		if len(c.diags) == before && c.table.Owned(elem) {
			c.errorf(
				call.Args[1].Spanned(),
				"cannot spread %s into add_all: its elements would be copied, and %s cannot be",
				c.text(call.Args[1].Spanned()),
				c.ownedPhrase(elem),
			)
		}
	} else {
		value := param{name: "elems", typ: elem}
		for _, arg := range call.Args[1:] {
			c.checkArg(arg, value, "add_all")
		}
	}
	c.checkListBorrows(call)
	if len(c.diags) > before || elem == types.Invalid {
		return types.Invalid
	}
	return c.added(call, c.table.Span(elem))
}

// added records typ, the id or span type a call to add or add_all gives,
// on call, with its results, typ and an error, and returns typ.
func (c *Checker) added(call *ast.Call, typ types.Type) types.Type {
	call.Type = typ
	call.ResultTypes = []types.Type{typ, types.Error}
	return typ
}

// arenaTarget checks arg, the arena a call to builtin, add or add_all,
// grows, which must be passed as mut, mut a, and be a place of an arena
// type, and returns the arena's element type, types.Invalid when anything
// was reported.
func (c *Checker) arenaTarget(arg ast.Expr, builtin string) types.Type {
	m, isMut := arg.(*ast.MutArg)
	if !isMut {
		c.expr(arg)
		c.missingMut(arg, param{name: "a"}, builtin)
		return types.Invalid
	}
	got := c.place(m.X, m)
	switch {
	case got == types.Invalid:
		return types.Invalid
	case !c.table.IsArena(got):
		c.errorf(m.X.Spanned(), "%s expects an arena, got %s", builtin, c.typeName(got))
		return types.Invalid
	default:
		return c.table.Elem(got)
	}
}

// makeArena checks make(arena[T], sentinel, alloc), whose arena type typ
// has been checked: the sentinel, usable as T and moved in when owned (see
// move.go), the arena's element at id 0, and an Allocator to grow it
// through. It records typ on the call and returns it, or types.Invalid
// when anything was reported.
func (c *Checker) makeArena(call *ast.Call, typ types.Type) types.Type {
	if !c.checkArity(call, "make", 3) {
		for _, arg := range call.Args[1:] {
			c.argExpr(arg)
		}
		return types.Invalid
	}
	before := len(c.diags)
	if typ != types.Invalid {
		c.checkArg(call.Args[1], param{name: "sentinel", typ: c.table.Elem(typ)}, "make")
	} else {
		c.argExpr(call.Args[1])
	}
	alloc := call.Args[2]
	c.use(alloc, c.expr(alloc), types.Allocator, "in argument to make")
	if len(c.diags) > before || typ == types.Invalid {
		return types.Invalid
	}
	call.Type = typ
	return typ
}

// arenaIndex checks ix, an element or a span's view of the arena of type
// base read as a value, as element checks an element, its index checked
// as no part of an assignment's target, and returns its type (see
// arenaElement); viewOK says ix is where a view may be.
func (c *Checker) arenaIndex(ix *ast.Index, base types.Type, viewOK bool) types.Type {
	assigning := c.assigning
	c.assigning = nil
	idx := c.expr(ix.Index)
	c.assigning = assigning
	return c.arenaElement(ix, base, idx, viewOK)
}

// arenaElement checks the index of ix, an element or a span's view of the
// arena of type base, whose index has type idx, and returns the type of
// what it gives: the element type for an id of it, which needs no proof,
// or a view of the element type for a span of it (see arenaSpan), viewOK
// saying ix is where a view may be. An index of any other type is
// reported, as is an element read in place while its index can change
// what it is read from, or assigned while its value can (see
// checkIndexBorrows and checkAssignBorrows), and the checked form, which
// an element, always there, has none of.
func (c *Checker) arenaElement(ix *ast.Index, base, idx types.Type, viewOK bool) types.Type {
	elem := c.table.Elem(base)
	switch {
	case idx == types.Invalid:
		return types.Invalid
	case idx == c.table.Span(elem):
		return c.arenaSpan(ix, elem, viewOK)
	case idx != c.table.ID(elem):
		c.badArenaIndex(ix, base, idx)
		return types.Invalid
	}
	if c.checkIndexBorrows(ix) || c.checkAssignBorrows(ix, base) {
		c.assigning = nil
	}
	if c.checkedIndex == ix {
		c.errorf(ix.Span, "the checked form cannot read an element of an arena: "+
			"every id names one, the sentinel for an id the arena did not give; use %s",
			c.text(ix.Span))
		return types.Invalid
	}
	return elem
}

// arenaSpan checks ix, a span's view of an arena of elements of type
// elem, and returns the view's type, []elem: a view, which only a place a
// view a call returns may be is (see viewresult.go), and which viewOK
// says ix is, and which reads the arena in place, so its span can change
// nothing it reads (see checkIndexBorrows). Anything else is reported,
// and gives types.Invalid.
func (c *Checker) arenaSpan(ix *ast.Index, elem types.Type, viewOK bool) types.Type {
	if c.checkIndexBorrows(ix) {
		return types.Invalid
	}
	view := c.table.View(elem)
	if !viewOK {
		c.errorf(ix.Span, "%s is a view, %s, which can only be passed to a view parameter, "+
			"given to len, indexed, sliced, ranged over or returned",
			c.text(ix.Span), c.typeName(view))
		return types.Invalid
	}
	return view
}

// badArenaIndex reports idx, the type of the index of ix, an element of
// the arena of type base, which is neither an id nor a span of its
// element type: an integer, which names no element, is told the id type
// to use, an untyped constant as the int64 it would become.
func (c *Checker) badArenaIndex(ix *ast.Index, base, idx types.Type) {
	if !types.IsUntyped(idx) && !types.IsInteger(idx) {
		c.errorf(ix.Index.Spanned(), "cannot index %s with %s", c.typeName(base), c.typeName(idx))
		return
	}
	if idx = c.valueOf(ix.Index, idx); idx != types.Invalid {
		c.errorf(ix.Index.Spanned(), "cannot index %s with %s; index it with an %s",
			c.typeName(base), c.typeName(idx), c.typeName(c.table.ID(c.table.Elem(base))))
	}
}

// isSpanView reports whether e, once checked, is a span's view of an
// arena, a[s].
func (c *Checker) isSpanView(e ast.Expr) bool {
	ix, ok := e.(*ast.Index)
	return ok && c.table.IsArena(ast.TypeOf(ix.X)) && c.table.IsSpan(ast.TypeOf(ix.Index))
}

// refConversion reports call, a conversion to an id or a span type rt,
// id[T](n), which converts nothing: no id or span is made of an integer,
// or of anything else, so none names a slot no add gave. Its arguments
// are checked for errors of their own first.
func (c *Checker) refConversion(call *ast.Call, rt *ast.RefType) {
	typ := c.refType(rt)
	reported := typ == types.Invalid
	for _, arg := range call.Args {
		got := c.valueOf(arg, c.expr(arg))
		if !reported && got != types.Invalid {
			c.errorf(call.Span, "cannot convert %s to %s", c.typeName(got), c.typeName(typ))
			reported = true
		}
	}
}

// isRefConversion reports whether call converts to an id or a span type,
// id[T](n).
func isRefConversion(call *ast.Call) bool {
	_, ok := call.Fn.(*ast.RefType)
	return ok
}

// borrowHolder names what place, read in place, is borrowed from, for an
// error about a borrow: "an arena" when it is an arena, an element of
// one or a span's view of one, and otherwise what, the holder the caller
// names, "a list" or "a map".
func (c *Checker) borrowHolder(place ast.Expr, what string) string {
	if c.table.IsArena(ast.TypeOf(place)) {
		return "an arena"
	}
	if ix, ok := place.(*ast.Index); ok && c.table.IsArena(ast.TypeOf(ix.X)) {
		return "an arena"
	}
	return what
}

// arenaKind names typ when it is an arena, an id or a span type, "an
// arena", "an id" or "a span", for a message, and is "" for any other.
func (c *Checker) arenaKind(typ types.Type) string {
	switch {
	case c.table.IsArena(typ):
		return "an arena"
	case c.table.IsID(typ):
		return "an id"
	case c.table.IsSpan(typ):
		return "a span"
	default:
		return ""
	}
}

// mutArenaElement returns the place arg passes as mut, and the arena it
// is in, when it is in an arena's buffer, an element of an arena or a
// field or an element of one, at any depth, and nils for anything else.
func (c *Checker) mutArenaElement(arg ast.Expr) (place, arena ast.Expr) {
	m, isMut := arg.(*ast.MutArg)
	if !isMut || ast.Root(m.X) == nil {
		return nil, nil
	}
	for x := m.X; ; {
		switch t := x.(type) {
		case *ast.Index:
			if c.table.IsArena(ast.TypeOf(t.X)) {
				return m.X, t.X
			}
			x = t.X
		case *ast.Selector:
			x = t.X
		default:
			return nil, nil
		}
	}
}

// checkMutArenaElements reports each operand of call that passes as mut
// a place in an arena's buffer (see mutArenaElement) while another
// operand holds, at any depth, a mut argument of that arena or of a place
// holding it, or a closure that may write it, with a note there: the
// place's address is taken before the other operands run, and growing
// the arena may move its buffer, which would leave the address
// dangling. A list's element passed so is held by the proof of its
// index, which such a mut argument ends; an arena's needs none. A mut
// argument of the arena that is itself an operand of call is the
// exclusivity check's to report (see checkExclusive), and one of an
// element or a field of one cannot move the arena's buffer.
func (c *Checker) checkMutArenaElements(call *ast.Call) {
	operands := call.Operands()
	for i, arg := range operands {
		place, arena := c.mutArenaElement(arg)
		if place == nil {
			continue
		}
		_, steps := path(arena)
		for _, w := range writersOutside(operands, i) {
			_, inside := path(w.place)
			if w.mut != nil && slices.Contains(operands, ast.Expr(w.mut)) ||
				!placesMayOverlap(arena, w.place) || len(inside) > len(steps) {
				continue
			}
			c.errorNotes(arg.Spanned(), []diag.Diagnostic{w.note()},
				"cannot pass mut %s alongside %s in this call: an arena is not copied for a call, "+
					"and %s during it",
				c.text(place.Spanned()), w.what(c), w.only(c))
			break
		}
	}
}
