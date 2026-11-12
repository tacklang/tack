package typeck

import (
	"fmt"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// A pool, pool[T], is an owned store of elements of type T, stored by
// value, any of which can be removed on its own. make(pool[T], alloc)
// makes an empty one, which allocates nothing until insert grows it
// through the Allocator alloc. Each element sits in a slot whose
// generation moves on when it is removed, and each pool has a tag of its
// own, so a handle given before a removal, or by another pool, names no
// element of it again.
//
// A handle, handle[T], names one element of a pool[T]: its slot, the
// slot's generation and the pool's tag. It is copyable, has == and != and
// a zero value, one no pool gives, and may be a map's key; it has no
// order or arithmetic, and converts to and from no integer. The handle
// types of two element types are two types.
//
// h, err := insert(mut p, v) moves v into the pool p and gives its handle;
// err says the pool could not grow, out of memory, and the pool is then as
// it was and v freed. Its error cannot be discarded, and the handle is
// held by its error, as any result beside an error is (see errors.go).
//
// has(p, h) reports whether h names a live element of p, and tested as a
// condition it proves so, by the fact a map's has gives (see memberFact):
// it holds up to any write to p, an insert, a remove, an assignment or a
// mut argument, that can reach the use, and a back edge of a loop that
// writes p is one. p[h] is then a place, as a proven m[k] is: read in
// place, written, passed as mut, appended to or replaced, never moved
// out. p[h] anywhere else is reported, as m[k] is, the checked form v, ok
// := p[h] aside, which reads a copyable T with a zero value, the zero
// value for a handle that names no live element. v, ok := remove(mut p,
// h) moves the element out, ok false and v T's zero value, or nothing to
// read for a T with none, when h names none; written as a statement,
// remove(mut p, h) frees the element it takes and needs no proof.
//
// len(p) is how many live elements p holds, never known. A range over a
// pool, for h, v := range p, gives every live element's handle and the
// element, bound in place when it is owned, and the loop's body cannot
// change p, as a range over a list cannot change it (see move.go); the
// body knows has(p, h) for its handle, as it knows a list range's index
// in range (see poolRangeFacts).
//
// A pool is owned as a list is: moved, freed with its owner, its live
// elements last first, borrowed by a plain parameter and changed through
// a mut one, and no argument of a call may read it in place, an element
// of it included, while another passes it as mut. It has no zero value,
// no == and no literal, and is not a global, an any, nor passed to or
// returned from an extern func, and nor is a handle. A type may hold a
// handle of itself, but not a pool of itself.

// poolType returns the pool type a written pool[T] denotes, adding it to
// the table. Its element type may be any type, an owned one or a type
// parameter included, as a list's may; one that was reported gives
// types.Invalid.
func (c *Checker) poolType(pt *ast.PoolType) types.Type {
	elem := c.declaredType(pt.Elem)
	if elem == types.Invalid {
		return types.Invalid
	}
	return c.table.Pool(elem)
}

// isInsert reports whether call calls the builtin insert.
func isInsert(call *ast.Call) bool { return isBuiltinCall(call, "insert") }

// givesWithError reports whether call calls add, add_all or insert, the
// builtins that give an id, a span or a handle and an error.
func givesWithError(call *ast.Call) bool { return addsToArena(call) || isInsert(call) }

// insertCall checks a call to insert, h, err := insert(mut p, v), records
// the handle type it gives on the call and its two results, the handle
// and an error, in Call.ResultTypes, and returns the handle type. Its
// first argument is the pool to grow, passed as mut, a place of a pool
// type; its second a value usable as the pool's element type, moved in
// when it is owned (see move.go). Anything else is reported, and gives
// types.Invalid.
func (c *Checker) insertCall(call *ast.Call) types.Type {
	if !c.checkArity(call, "insert", 2) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return types.Invalid
	}
	before := len(c.diags)
	typ := c.poolTarget(call.Args[0], "insert")
	elem := c.table.Elem(typ)
	if typ == types.Invalid {
		elem = types.Invalid
	}
	c.checkArg(call.Args[1], param{name: "v", typ: elem}, "insert")
	c.checkListBorrows(call)
	if len(c.diags) > before || elem == types.Invalid {
		return types.Invalid
	}
	return c.added(call, c.table.Handle(elem))
}

// poolTarget checks arg, the pool a call to builtin, insert, grows, which
// must be passed as mut, mut p, and be a place of a pool type, and
// returns the pool's type, types.Invalid when anything was reported.
func (c *Checker) poolTarget(arg ast.Expr, builtin string) types.Type {
	m, isMut := arg.(*ast.MutArg)
	if !isMut {
		c.expr(arg)
		c.missingMut(arg, param{name: "p"}, builtin)
		return types.Invalid
	}
	got := c.place(m.X, m)
	switch {
	case got == types.Invalid:
		return types.Invalid
	case !c.table.IsPool(got):
		c.errorf(m.X.Spanned(), "%s expects a pool, got %s", builtin, c.typeName(got))
		return types.Invalid
	default:
		return got
	}
}

// poolRemove checks the handle of call, a call to remove whose first
// argument, mut p, is a place of the pool type typ: its second argument,
// usable as a handle of the pool's element type. It records the element
// type on the call and returns it. Only the checked form, v, ok :=
// remove(mut p, h), the call c.checkedTake is, and the statement,
// remove(mut p, h), the call c.droppedCall is, may be written, and
// neither needs a proof: a handle that names no live element removes
// nothing. A plain form would have nothing to give for such a handle, so
// it is reported, and gives types.Invalid, as anything else wrong does.
func (c *Checker) poolRemove(call *ast.Call, typ types.Type) types.Type {
	before := len(c.diags)
	elem := c.table.Elem(typ)
	c.checkArg(call.Args[1], param{name: "h", typ: c.table.Handle(elem)}, "remove")
	c.checkListBorrows(call)
	if len(c.diags) > before {
		return types.Invalid
	}
	call.Type = elem
	if c.checkedTake != call && c.droppedCall != call {
		c.errorf(call.Span, "remove gives the element and whether the handle was live; "+
			"use v, ok := %s", c.text(call.Span))
		return types.Invalid
	}
	return elem
}

// poolElement checks p[h], ix, where p has the pool type base and h has
// the type idx, as mapElement checks m[k], and returns the pool's element
// type. h must be usable as a handle of the element type, under
// assignment's rules, and may not pass the pool, or a place holding it, as
// mut (see checkIndexBorrows); nor may the value assigned to p[h] (see
// checkAssignBorrows). Bound with ok, the checked form, p[h] needs nothing
// more, though only a copyable element with a zero value can be read so
// (see poolResults); anywhere else h must be proven live in p, by has
// (see provenIn), and one that is not is reported, write saying p[h] is
// being assigned, which no checked form can do.
func (c *Checker) poolElement(ix *ast.Index, base, idx types.Type, write bool) types.Type {
	elem := c.table.Elem(base)
	handle := c.table.Handle(elem)
	if !c.use(ix.Index, idx, handle, "to index "+c.typeName(base)) ||
		c.checkIndexBorrows(ix) || c.checkAssignBorrows(ix, base) {
		c.assigning = nil
		return elem
	}
	if c.checkedIndex == ix || c.provenIn(ix.X, ix.Index) {
		return elem
	}
	h, p := c.text(ix.Index.Spanned()), c.text(ix.X.Spanned())
	msg := fmt.Sprintf("handle %s may not be live in %s; check has(%s, %s) first", h, p, p, h)
	if !write && !c.table.Owned(elem) && c.table.HasZero(elem) {
		msg += ", or use v, ok := " + c.text(ix.Spanned())
	}
	c.errorNotes(ix.Index.Spanned(), c.memberNotes(ix), "%s", msg)
	return elem
}

// poolResults checks p[h], ix, an element of type elem bound to want
// names, which must be two: the element, or its type's zero value when h
// names no live element of p, and whether it does. It copies the
// element, so an element of an owned type, a type parameter's included,
// which is only ever read in place, is reported, and so is one of another
// type with no zero value to give.
func (c *Checker) poolResults(
	ix *ast.Index,
	elem types.Type,
	want int,
	span ast.Span,
) ([]types.Type, bool) {
	p := c.text(ix.X.Spanned())
	switch {
	case c.table.Owned(elem):
		c.errorf(
			span,
			"the checked form cannot read an element of type %s: it would copy it out of %s; "+
				"check has(%s, %s) first and use %s in place",
			c.typeName(elem),
			p,
			p,
			c.text(ix.Index.Spanned()),
			c.text(ix.Spanned()),
		)
		return nil, false
	case !c.table.HasZero(elem):
		c.errorf(span, "the checked form cannot read an element of type %s: "+
			"it has no zero value to give when the handle is stale; check has(%s, %s) first",
			c.typeName(elem), p, c.text(ix.Index.Spanned()))
		return nil, false
	case want != 2:
		c.errorf(span, "assignment mismatch: %d variables but an index gives 2 values", want)
		return nil, false
	default:
		return []types.Type{elem, types.Bool}, true
	}
}

// hasTarget returns the handle or key type the second argument of has
// must be usable as, for typ, the type of its first, a map or a pool, and
// reports typ, valueOf settling it first, and gives types.Invalid when it
// is neither.
func (c *Checker) hasTarget(arg ast.Expr, typ types.Type) types.Type {
	switch {
	case typ == types.Invalid:
		return types.Invalid
	case c.table.IsMap(typ):
		return c.table.Key(typ)
	case c.table.IsPool(typ):
		return c.table.Handle(c.table.Elem(typ))
	}
	if typ = c.valueOf(arg, typ); typ != types.Invalid {
		c.errorf(arg.Spanned(), "has expects a map or a pool, got %s", c.typeName(typ))
	}
	return types.Invalid
}

// poolKind names typ when it is a pool or a handle type, "a pool" or "a
// handle", for a message, and is "" for any other.
func (c *Checker) poolKind(typ types.Type) string {
	switch {
	case c.table.IsPool(typ):
		return "a pool"
	case c.table.IsHandle(typ):
		return "a handle"
	default:
		return ""
	}
}

// poolRangeFacts returns what the body of loop, a range over a pool with
// a handle variable, knows about the handle: that it names a live
// element of the pool, the fact has(p, h) gives (see memberFact), when
// the pool is a subject has could prove a handle in (see keyOf). The
// range locks the pool, so no insert, remove, assignment or mut use of
// it can run in the body (see move.go), and checkForRange drops the fact
// at any write to the pool or the handle in the body all the same, as it
// drops a list range's index facts.
func (c *Checker) poolRangeFacts(loop *ast.ForRange) []fact {
	p, ok := c.keyOf(loop.X)
	if !ok {
		return nil
	}
	return []fact{{about: declKey(loop.Var), inMap: p}}
}
