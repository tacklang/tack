package typeck

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
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
// the values it cannot add. Its error cannot be discarded. k and v are moved
// in, as append's values are.
//
// v, ok := m[k] reads the value under k, a copy, and ok is whether k is
// in m; v is V's zero value when it is not. An owned V would be copied,
// so the checked form cannot read one. has(m, k) reports whether k is in
// m, and tested as a condition it proves so, by a fact (see memberFact)
// that holds as an index's does, up to any write to m, a put, a delete,
// an assignment or a mut argument, that can reach the use. m[k] is then a
// place, as a proven l[i] is: read in place, written, passed as mut,
// appended to or replaced, its value staying in m's entries, which
// nothing moves while the proof holds. m[k] anywhere else is reported.
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
	if !c.requireKey(key, mt.Key.Spanned()) {
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
	case !c.comparable(key) || c.holdsFloat(key):
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
		if !c.comparable(p.elem) || c.holdsFloat(p.elem) {
			c.badKey(p.elem, p.at)
		}
	}
	c.pendingKeys = nil
}

// badKey reports key, written at at, as a type no map can be keyed by. A
// float, or anything holding one, has == but is no key: NaN equals
// nothing, itself included, so a key holding it could never be found.
func (c *Checker) badKey(key types.Type, at ast.Span) {
	why := "it has no =="
	switch {
	case key == types.Error:
		why = "an error is not a key"
	case c.table.IsTypeParam(key):
		why = "a map's key cannot be a type parameter"
	case types.IsFloat(key):
		why = "a float is not a key"
	case c.comparable(key) && c.holdsFloat(key):
		why = "it holds a float, which is not a key"
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
// checkIndexBorrows); nor may the value assigned to m[k] (see
// checkAssignBorrows). Bound with ok, the checked form, m[k] needs nothing
// more; anywhere else k must be proven in m, by has (see provenIn), and
// one that is not is reported, write saying m[k] is being assigned.
func (c *Checker) mapElement(ix *ast.Index, base, idx types.Type, write bool) types.Type {
	value := c.table.Elem(base)
	if !c.use(ix.Index, idx, c.table.Key(base), "as a key of "+c.typeName(base)) ||
		c.checkIndexBorrows(ix) || c.checkAssignBorrows(ix, base) {
		c.assigning = nil
		return value
	}
	if c.checkedIndex == ix || c.provenIn(ix.X, ix.Index) {
		return value
	}
	key, m := c.text(ix.Index.Spanned()), c.text(ix.X.Spanned())
	msg := fmt.Sprintf("key %s may not be in %s; check has(%s, %s) first", key, m, m, key)
	switch {
	case write:
		msg += fmt.Sprintf(", or use put(mut %s, %s, ...)", m, key)
	case !c.table.Owned(value) && c.table.HasZero(value):
		msg += ", or use v, ok := " + c.text(ix.Spanned())
	}
	c.errorNotes(ix.Index.Spanned(), c.memberNotes(ix), "%s", msg)
	return value
}

// isHas reports whether call calls has.
func isHas(call *ast.Call) bool { return isBuiltinCall(call, "has") }

// hasKeyCall checks a call to has, has(m, k), records its type, bool, on
// the call and returns it. Its first argument is the map, read in place,
// as len reads one, and its second the key, usable as the map's key type
// and borrowed, not moved, when owned. Tested as a condition, it proves k
// is in m (see memberFact). Anything else is reported, and gives
// types.Invalid.
func (c *Checker) hasKeyCall(call *ast.Call) types.Type {
	if !c.checkArity(call, "has", 2) {
		for _, arg := range call.Args {
			c.argExpr(arg)
		}
		return types.Invalid
	}
	before := len(c.diags)
	typ := c.expr(call.Args[0])
	if typ != types.Invalid && !c.table.IsMap(typ) {
		if typ = c.valueOf(call.Args[0], typ); typ != types.Invalid {
			c.errorf(call.Args[0].Spanned(), "has expects a map, got %s", c.typeName(typ))
		}
		typ = types.Invalid
	}
	if typ == types.Invalid {
		c.argExpr(call.Args[1])
		return types.Invalid
	}
	c.checkArg(call.Args[1], param{name: "k", typ: c.table.Key(typ)}, "has")
	c.checkListBorrows(call)
	if len(c.diags) > before {
		return types.Invalid
	}
	call.Type = types.Bool
	return types.Bool
}

// memberFact returns the fact has(m, k), call, gives when it is true: that
// k is in m. m must be a subject (see keyOf) and k a key that cannot
// change (see memberOf); a call to has on anything else gives none. The
// fact ends at any write to m that can reach its use, a put, a delete, an
// assignment or a mut argument, but not one to a value m holds, which
// changes none of its keys (see writes).
func (c *Checker) memberFact(call *ast.Call) []fact {
	if call.Type != types.Bool || len(call.Args) != 2 {
		return nil
	}
	m, ok := c.keyOf(call.Args[0])
	if !ok {
		return nil
	}
	about, known, ok := c.memberOf(call.Args[1])
	if !ok {
		return nil
	}
	return from([]fact{{about: about, known: known, inMap: m}}, call)
}

// memberOf returns how a fact that k is in a map knows k: by its value,
// written out as known, when k is a known integer or string, so has(m, 1)
// proves m[1]; or as the subject about, when k is a name that nothing can
// assign, an immutable variable or a parameter declared without mut, or a
// path below one. A mutable variable is not one, even though facts about
// its value can be, since writing an element of a list it holds would
// change the key without ending a fact about it (see writes). It reports
// false for any other key.
func (c *Checker) memberOf(k ast.Expr) (about factKey, known string, ok bool) {
	if v, isKnown := c.knownValue(k); isKnown {
		return factKey{}, "int " + v.String(), true
	}
	if s, isKnown := c.knownString(k); isKnown {
		return factKey{}, "string " + strconv.Quote(s), true
	}
	if id, isName := k.(*ast.Ident); isName {
		return declKey(id.Def), "", immutableName(id)
	}
	about, ok = c.pathKey(k)
	return about, "", ok
}

// memberFacts returns every fact in force, held or dropped, that says the
// key of ix, m[k], is in m.
func (c *Checker) memberFacts(ix *ast.Index) []fact {
	m, ok := c.keyOf(ix.X)
	if !ok {
		return nil
	}
	about, known, ok := c.memberOf(ix.Index)
	if !ok {
		return nil
	}
	var facts []fact
	for f := range c.factsAbout(about) {
		if f.inMap == m && f.known == known {
			facts = append(facts, f)
		}
	}
	return facts
}

// provenIn reports whether a fact in force proves the key k in the map m,
// as m[k] reads it.
func (c *Checker) provenIn(m, k ast.Expr) bool {
	return slices.ContainsFunc(c.memberFacts(&ast.Index{X: m, Index: k}), c.holds)
}

// memberNotes returns the notes that explain why the key of ix, m[k], is
// not proven in m: for each has that proved it but whose proof a write
// ended, the guard and the write; or, when m is a map or the key one has
// cannot prove anything about, that.
func (c *Checker) memberNotes(ix *ast.Index) []diag.Diagnostic {
	if _, ok := c.keyOf(ix.X); !ok {
		return []diag.Diagnostic{
			note(ix.X.Spanned(), "has proves a key only in a map that is a variable "+
				"or a parameter, or a field or an element of one declared without mut"),
		}
	}
	if _, _, ok := c.memberOf(ix.Index); !ok {
		return []diag.Diagnostic{
			note(ix.Index.Spanned(), "has proves only a key that cannot change: "+
				"a constant, a variable or parameter declared without mut, or a field of one"),
		}
	}
	var dropped []fact
	for _, f := range c.memberFacts(ix) {
		if f.drop = c.dropOf(f); f.drop != nil {
			dropped = append(dropped, f)
		}
	}
	key, m := c.text(ix.Index.Spanned()), c.text(ix.X.Spanned())
	return dropNotes(dropped, func([]fact) string { return key + " is in " + m })
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
				"check has(%s, %s) first and use %s in place",
			c.typeName(value),
			m,
			m,
			c.text(ix.Index.Spanned()),
			c.text(ix.Spanned()),
		)
		return nil, false
	case !c.table.HasZero(value):
		c.errorf(span, "the checked form cannot read a value of type %s: "+
			"it has no zero value to give when the key is not in the map; check has(%s, %s) first",
			c.typeName(value), m, c.text(ix.Index.Spanned()))
		return nil, false
	case want != 2:
		c.errorf(span, "assignment mismatch: %d variables but an index gives 2 values", want)
		return nil, false
	default:
		return []types.Type{value, types.Bool}, true
	}
}
