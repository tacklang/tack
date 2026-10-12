package types

import "strconv"

// Table describes the types one compilation uses. The type checker creates
// it and records it on the file it checks, and every later stage reads types
// through it, so the name or shape of a type is always looked up in the
// compilation that gave it its ID.
//
// The basic types have fixed IDs and need no entry. Every other type is
// added the first time it is seen and given the next free ID, and found
// again by its shape after that, so a type written twice gets one ID and ==
// stays type identity. Array types are the only such types today.
type Table struct {
	ids     map[array]Type // each array type's ID, by its shape
	entries []array        // the array type with ID numBasic + i, at i
}

// array is the shape of an array type: its element type and its length.
type array struct {
	elem Type
	n    int64
}

// NewTable returns a Table that knows the basic types.
func NewTable() *Table { return &Table{ids: make(map[array]Type)} }

// Array returns the type of arrays of n elements of type elem, adding it if
// the Table does not have it yet. n must not be negative.
func (t *Table) Array(elem Type, n int64) Type {
	shape := array{elem: elem, n: n}
	if id, ok := t.ids[shape]; ok {
		return id
	}
	id := numBasic + Type(len(t.entries))
	t.entries = append(t.entries, shape)
	t.ids[shape] = id
	return id
}

// entry returns the array type with ID typ, and whether typ is one this
// Table gave out. A nil Table gave out none.
func (t *Table) entry(typ Type) (array, bool) {
	if t == nil || typ < numBasic || int(typ-numBasic) >= len(t.entries) {
		return array{}, false
	}
	return t.entries[typ-numBasic], true
}

// IsArray reports whether typ is an array type.
func (t *Table) IsArray(typ Type) bool {
	_, ok := t.entry(typ)
	return ok
}

// Elem returns the element type of the array type typ, or Invalid if typ is
// not one.
func (t *Table) Elem(typ Type) Type {
	a, ok := t.entry(typ)
	if !ok {
		return Invalid
	}
	return a.elem
}

// Len returns the length of the array type typ, or 0 if typ is not one.
func (t *Table) Len(typ Type) int64 {
	a, _ := t.entry(typ)
	return a.n
}

// Count returns how many types the Table has added, so a caller can visit
// each: their IDs are numBasic through numBasic + Count() - 1, in the order
// they were added, which puts every element type before an array of it.
func (t *Table) Count() int {
	if t == nil {
		return 0
	}
	return len(t.entries)
}

// Added returns the ID of the i'th type the Table added, counting from 0.
func Added(i int) Type { return numBasic + Type(i) }

// Name returns a type's name as a program writes it, for diagnostics: a
// basic type's own name, or [n]elem for an array. A type the Table does not
// know, including one from another Table, is named "invalid" rather than
// guessed at. A nil Table knows the basic types.
func (t *Table) Name(typ Type) string {
	if typ >= 0 && typ < numBasic {
		return basicNames[typ]
	}
	if a, ok := t.entry(typ); ok {
		return "[" + strconv.FormatInt(a.n, 10) + "]" + t.Name(a.elem)
	}
	return "invalid"
}
