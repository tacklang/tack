package typeck

import (
	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// A map, map[K]V, holds values of type V by keys of type K, in the order
// each key was first put, and owns both: it frees every key and value it
// still holds, then its buffers, when its owner's block ends, as a list
// does its elements, and moves rather than copies. K is any type with ==
// but error and any (see requireKey), so its keys are compared, and
// hashed, by their contents; V any type a list's element can be. A map
// has no zero value, no ==, and no literal; make(map[K]V, alloc) makes an
// empty one, which allocates nothing, and len(m) is how many keys it
// holds, never a known value and carrying no facts.
//
// err := put(mut m, k, v) puts v under k: in place of the value k has,
// which it frees, the key m holds staying and k being freed, or at the end
// of the order, growing m's buffers, when k is new. It allocates only for
// a new key, and when it cannot, the error is error{code: 1, msg: "out of
// memory"}, m is left as it was, and k and v are freed, as append frees
// the values it cannot add. Its error cannot be dropped. k and v are moved
// in, as append's values are.
//
// v, ok := m[k] reads the value under k, a copy, and ok is whether k is
// in m; v is V's zero value when it is not. An owned V would be copied,
// so the checked form cannot read one, and m[k] alone, which would need
// k proven in m, is reported.
//
// v, ok := delete(mut m, k) takes the value under k out of m, freeing the
// key m held; when k is not in m, ok is false, m is unchanged, and v is
// V's zero value, or, for a V with none, cannot be read until ok is
// checked true, as pop's x. Written as a statement, delete(mut m, k)
// frees the value it takes.
//
// A range over a map, for k, v := range m, for k := range m or for _, v
// := range m, walks its keys and values in insertion order. k is never
// assigned, and refers to the key in place when K is owned; v refers to
// the value in place when V is owned, and is a copy otherwise. The loop's
// body cannot change m, as a range over a list cannot change it (see
// move.go).

// mapType returns the map type a written map[K]V denotes, adding it to
// the table. Its key type must have == (see requireKey), and its value
// type may be anything a list's element may be (see requireElement). One
// that is not, or was reported, gives types.Invalid.
func (c *Checker) mapType(mt *ast.MapType) types.Type {
	key := c.declaredType(mt.Key)
	value := c.declaredType(mt.Value)
	if key == types.Invalid || value == types.Invalid {
		return types.Invalid
	}
	if !c.requireKey(key, mt.Key.Spanned()) ||
		!c.requireElement("a map", value, mt.Value.Spanned()) {
		return types.Invalid
	}
	return c.table.Map(key, value)
}

// requireKey reports key, the key type of a map written at at, unless it
// is a type whose values == compares, which a map compares and hashes its
// keys by: an integer, a bool, a string, or an array, a struct, a union or
// a list made of those, but not error, a builtin type whose keys would be
// compared by their messages, nor a type parameter. While struct fields
// are being resolved, a struct's fields may not be known yet, so the
// check waits until they are (see checkPendingKeys).
func (c *Checker) requireKey(key types.Type, at ast.Span) bool {
	switch {
	case key == types.Error, c.table.IsTypeParam(key):
		c.badKey(key, at)
		return false
	case c.inFields:
		c.pendingKeys = append(c.pendingKeys, pendingElem{holder: "a map", at: at, elem: key})
		return true
	case !c.comparable(key):
		c.badKey(key, at)
		return false
	default:
		return true
	}
}

// checkPendingKeys reports every key type requireKey put off that turned
// out to have no ==, once every struct's fields are known.
func (c *Checker) checkPendingKeys() {
	for _, p := range c.pendingKeys {
		if !c.comparable(p.elem) {
			c.badKey(p.elem, p.at)
		}
	}
	c.pendingKeys = nil
}

// badKey reports key, written at at, as a type no map can be keyed by.
func (c *Checker) badKey(key types.Type, at ast.Span) {
	why := "it has no =="
	switch {
	case key == types.Error:
		why = "an error is not a key"
	case c.table.IsTypeParam(key):
		why = "a map's key cannot be a type parameter"
	}
	c.errorf(at, "cannot use %s as a map key: %s", c.typeName(key), why)
}

// typeValue reports a type, written as typ, where a value is wanted,
// anywhere but as make's first argument, once it checks as the type got,
// and returns types.Invalid.
func (c *Checker) typeValue(typ ast.Expr, got types.Type) types.Type {
	if got != types.Invalid {
		c.errorf(typ.Spanned(), "cannot use %s as a value; it is a type", c.typeName(got))
	}
	return types.Invalid
}

// isPut reports whether call calls put.
func isPut(call *ast.Call) bool { return isBuiltinCall(call, "put") }

// isDelete reports whether call calls delete.
func isDelete(call *ast.Call) bool { return isBuiltinCall(call, "delete") }

// mapTarget checks arg, the map a call to builtin, put or delete, changes,
// which must be passed as mut, mut m, and be a place of a map type, and
// returns the map's type, types.Invalid when anything was reported.
func (c *Checker) mapTarget(arg ast.Expr, builtin string) types.Type {
	m, isMut := arg.(*ast.MutArg)
	if !isMut {
		c.expr(arg)
		c.missingMut(arg, param{name: "m"}, builtin)
		return types.Invalid
	}
	got := c.place(m.X, m)
	switch {
	case got == types.Invalid:
		return types.Invalid
	case !c.table.IsMap(got):
		c.errorf(m.X.Spanned(), "%s expects a map, got %s", builtin, c.typeName(got))
		return types.Invalid
	default:
		return got
	}
}

// putCall checks a call to put, err := put(mut m, k, v), records its
// type, error, on the call and returns it. Its first argument is the map,
// passed as mut, then the key, usable as the map's key type, and the
// value, usable as its value type, each moved in when owned, as append's
// values are. Anything else is reported, and gives types.Invalid.
func (c *Checker) putCall(call *ast.Call) types.Type {
	if !c.checkArity(call, "put", 3) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return types.Invalid
	}
	before := len(c.diags)
	typ := c.mapTarget(call.Args[0], "put")
	if typ == types.Invalid {
		c.argExpr(call.Args[1])
		c.argExpr(call.Args[2])
		return types.Invalid
	}
	c.checkArg(call.Args[1], param{name: "k", typ: c.table.Key(typ)}, "put")
	c.checkArg(call.Args[2], param{name: "v", typ: c.table.Elem(typ)}, "put")
	c.checkListBorrows(call)
	if len(c.diags) > before {
		return types.Invalid
	}
	call.Type = types.Error
	return types.Error
}

// deleteCall checks a call to delete, v, ok := delete(mut m, k), the
// checked form, the call c.checkedTake is, or delete(mut m, k) written as
// a statement, the call c.droppedCall is, records the type of the value
// it gives on the call and returns it. Its first argument is the map,
// passed as mut, and its second the key, usable as the map's key type.
// Anywhere else it is reported: a key may not be in the map, so what it
// gives must be checked. Anything else is reported, and gives
// types.Invalid.
func (c *Checker) deleteCall(call *ast.Call) types.Type {
	if !c.checkArity(call, "delete", 2) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return types.Invalid
	}
	before := len(c.diags)
	typ := c.mapTarget(call.Args[0], "delete")
	if typ == types.Invalid {
		c.argExpr(call.Args[1])
		return types.Invalid
	}
	c.checkArg(call.Args[1], param{name: "k", typ: c.table.Key(typ)}, "delete")
	c.checkListBorrows(call)
	if len(c.diags) > before {
		return types.Invalid
	}
	call.Type = c.table.Elem(typ)
	if c.checkedTake != call && c.droppedCall != call {
		c.errorf(call.Span, "delete gives the value and whether the key was there; "+
			"use v, ok := %s", c.text(call.Span))
		return types.Invalid
	}
	return call.Type
}

// mapElement checks m[k], ix, where m has the map type base and k has the
// type idx, as element does an index, and returns the map's value type.
// k must be usable as the map's key type, under assignment's rules, and
// may not pass the map, or a place holding it, as mut (see
// checkIndexBorrows). Bound with ok, the checked form, m[k] needs nothing
// more; anywhere else k is not known to be in m, which is reported, write
// saying m[k] is being assigned.
func (c *Checker) mapElement(ix *ast.Index, base, idx types.Type, write bool) types.Type {
	value := c.table.Elem(base)
	if !c.use(ix.Index, idx, c.table.Key(base), "as a key of "+c.typeName(base)) ||
		c.checkIndexBorrows(ix) {
		return value
	}
	if c.checkedIndex == ix {
		return value
	}
	key, m := c.text(ix.Index.Spanned()), c.text(ix.X.Spanned())
	if write {
		c.errorf(
			ix.Index.Spanned(),
			"key %s may not be in %s; use put(mut %s, %s, ...)",
			key,
			m,
			m,
			key,
		)
		return value
	}
	c.errorf(
		ix.Index.Spanned(),
		"key %s may not be in %s; use v, ok := %s",
		key,
		m,
		c.text(ix.Spanned()),
	)
	return value
}

// mapResults checks m[k], ix, a value of type value bound to want names,
// which must be two: the value, or its type's zero value when k is not in
// m, and whether k is. It copies the value, so a value of an owned type,
// which is only ever read in place, is reported, and so is one of another
// type with no zero value to give.
func (c *Checker) mapResults(
	ix *ast.Index,
	value types.Type,
	want int,
	span ast.Span,
) ([]types.Type, bool) {
	m := c.text(ix.X.Spanned())
	switch {
	case c.table.Owned(value):
		c.errorf(
			span,
			"the checked form cannot read a value of type %s: it would copy it out of %s; "+
				"range over %s to use its values in place",
			c.typeName(value),
			m,
			m,
		)
		return nil, false
	case !c.table.HasZero(value):
		c.errorf(span, "the checked form cannot read a value of type %s: "+
			"it has no zero value to give when the key is not in the map", c.typeName(value))
		return nil, false
	case want != 2:
		c.errorf(span, "assignment mismatch: %d variables but an index gives 2 values", want)
		return nil, false
	default:
		return []types.Type{value, types.Bool}, true
	}
}
