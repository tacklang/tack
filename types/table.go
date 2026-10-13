package types

import "strconv"

// Table describes the types one compilation uses. The type checker creates
// it and records it on the file it checks, and every later stage reads types
// through it, so the name or shape of a type is always looked up in the
// compilation that gave it its ID.
//
// The basic types have fixed IDs and need no entry. Every other type is
// added to the Table and given the next free ID. An array type is added the
// first time it is seen and found again by its shape after that, so an array
// type written twice gets one ID and == stays type identity. A struct type
// is added once for each declaration, by Struct, since two declarations are
// two types even when their fields are alike; its fields are set afterwards,
// by SetFields, so a struct can have a field of a type declared after it.
type Table struct {
	ids     map[array]Type // each array type's ID, by its shape
	entries []entry        // the type with ID numBasic + i, at i
}

// entry is one type a Table added: an array, with its shape, or a struct,
// with its name and fields.
type entry struct {
	strct *structType // nil for an array
	arr   array
}

// array is the shape of an array type: its element type and its length.
type array struct {
	elem Type
	n    int64
}

// structType is a declared struct type: its name and its fields, in the
// order they were declared. fields is nil until SetFields is called.
type structType struct {
	name   string
	fields []Field
}

// Field is one field of a struct type: its name and its type.
type Field struct {
	Name string
	Type Type
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
	id := t.add(entry{arr: shape})
	t.ids[shape] = id
	return id
}

// Struct adds a new struct type named name, with no fields until SetFields
// gives it some, and returns it. Every call adds a type of its own, even
// for a name it has seen: struct types are told apart by declaration, not
// by shape.
func (t *Table) Struct(name string) Type {
	return t.add(entry{strct: &structType{name: name}})
}

// add gives e the next free ID and returns it.
func (t *Table) add(e entry) Type {
	id := numBasic + Type(len(t.entries))
	t.entries = append(t.entries, e)
	return id
}

// SetFields gives the struct type typ its fields, in declaration order. It
// does nothing if typ is not a struct type of this Table.
func (t *Table) SetFields(typ Type, fields []Field) {
	if e, ok := t.entry(typ); ok && e.strct != nil {
		e.strct.fields = fields
	}
}

// entry returns the type with ID typ, and whether typ is one this Table
// gave out. A nil Table gave out none.
func (t *Table) entry(typ Type) (entry, bool) {
	if t == nil || typ < numBasic || int(typ-numBasic) >= len(t.entries) {
		return entry{}, false
	}
	return t.entries[typ-numBasic], true
}

// IsArray reports whether typ is an array type.
func (t *Table) IsArray(typ Type) bool {
	e, ok := t.entry(typ)
	return ok && e.strct == nil
}

// IsStruct reports whether typ is a struct type.
func (t *Table) IsStruct(typ Type) bool {
	e, ok := t.entry(typ)
	return ok && e.strct != nil
}

// Elem returns the element type of the array type typ, or Invalid if typ is
// not one.
func (t *Table) Elem(typ Type) Type {
	if !t.IsArray(typ) {
		return Invalid
	}
	e, _ := t.entry(typ)
	return e.arr.elem
}

// Len returns the length of the array type typ, or 0 if typ is not one.
func (t *Table) Len(typ Type) int64 {
	if !t.IsArray(typ) {
		return 0
	}
	e, _ := t.entry(typ)
	return e.arr.n
}

// Fields returns the fields of the struct type typ, in declaration order,
// or nil if typ is not one or has none yet. The caller must not change
// them.
func (t *Table) Fields(typ Type) []Field {
	if !t.IsStruct(typ) {
		return nil
	}
	e, _ := t.entry(typ)
	return e.strct.fields
}

// Field returns the field of the struct type typ named name, its position
// among the fields, and whether typ has one.
func (t *Table) Field(typ Type, name string) (Field, int, bool) {
	for i, f := range t.Fields(typ) {
		if f.Name == name {
			return f, i, true
		}
	}
	return Field{}, 0, false
}

// Count returns how many types the Table has added, so a caller can visit
// each: their IDs are numBasic through numBasic + Count() - 1, in the order
// they were added. An array type comes after its element type, but a struct
// type may come before the types of its fields, so a caller that needs
// every type a type is made of first has to order them itself.
func (t *Table) Count() int {
	if t == nil {
		return 0
	}
	return len(t.entries)
}

// Added returns the ID of the i'th type the Table added, counting from 0.
func Added(i int) Type { return numBasic + Type(i) }

// Name returns a type's name as a program writes it, for diagnostics: a
// basic type's own name, [n]elem for an array, or a struct type's declared
// name. A type the Table does not know, including one from another Table,
// is named "invalid" rather than guessed at. A nil Table knows the basic
// types.
func (t *Table) Name(typ Type) string {
	return t.NameWith(typ, nil)
}

// NameWith returns a type's name as Name does, but writes each struct type
// in it, an array's element type included, as qualify gives it, from the
// struct type and its declared name. A nil qualify writes the declared
// name, as Name does.
func (t *Table) NameWith(typ Type, qualify func(typ Type, name string) string) string {
	if typ >= 0 && typ < numBasic {
		return basicNames[typ]
	}
	e, ok := t.entry(typ)
	switch {
	case !ok:
		return "invalid"
	case e.strct != nil && qualify != nil:
		return qualify(typ, e.strct.name)
	case e.strct != nil:
		return e.strct.name
	default:
		return "[" + strconv.FormatInt(e.arr.n, 10) + "]" + t.NameWith(e.arr.elem, qualify)
	}
}
