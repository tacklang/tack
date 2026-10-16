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
// by SetFields, so a struct can have a field of a type declared after it. A
// view type, []T, is added the first time it is seen, like an array, and
// found again by its element type, and so is a list type, list[T]. Error
// is a struct the Table never adds: IsStruct and Fields know it without
// an entry. A union type is added once for each declaration, by Union,
// as a struct type is, and its cases set afterwards, by SetCases. A type
// parameter is added once for each one a generic function declares, by
// TypeParam.
//
// While code is generated for one instance of a generic function, Bind
// makes each of its type parameters stand for a struct or union type:
// every question about a bound type parameter is then answered about its
// type, so code that asks the Table what a type is lowers the instance as
// it would a function written with that type. Unbind ends it.
type Table struct {
	ids   map[array]Type // each array type's ID, by its shape
	views map[Type]Type  // each view type's ID, by its element type
	lists map[Type]Type  // each list type's ID, by its element type
	// bound maps each type parameter Bind bound to the type it stands
	// for; it is nil when nothing is bound.
	bound map[Type]Type

	entries []entry // the type with ID numBasic + i, at i
}

// entry is one type a Table added: an array, with its shape, a view or a
// list, with its element type in arr.elem, a struct, with its name and
// fields, a union, with its name and cases, or a type parameter, with its
// name.
type entry struct {
	strct *structType // nil for anything but a struct
	union *unionType  // nil for anything but a union
	param string      // a type parameter's name, "" for any other type
	arr   array
	view  bool
	list  bool
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

// unionType is a declared union type: its name and its cases, in the
// order they were declared. cases is nil until SetCases is called.
type unionType struct {
	name  string
	cases []Case
}

// Case is one case of a union type: its name and, when Payload is set,
// the type of its payload, which is Invalid when that type could not be
// determined. A case with no payload has Payload false and Type Invalid.
type Case struct {
	Name    string
	Type    Type
	Payload bool
}

// NewTable returns a Table that knows the basic types.
func NewTable() *Table {
	return &Table{
		ids:   make(map[array]Type),
		views: make(map[Type]Type),
		lists: make(map[Type]Type),
	}
}

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

// View returns the type of read-only views of elements of type elem, []elem,
// adding it if the Table does not have it yet.
func (t *Table) View(elem Type) Type {
	if id, ok := t.views[elem]; ok {
		return id
	}
	id := t.add(entry{arr: array{elem: elem}, view: true})
	t.views[elem] = id
	return id
}

// List returns the type of owned, growable lists of elements of type elem,
// list[elem], adding it if the Table does not have it yet.
func (t *Table) List(elem Type) Type {
	if id, ok := t.lists[elem]; ok {
		return id
	}
	id := t.add(entry{arr: array{elem: elem}, list: true})
	t.lists[elem] = id
	return id
}

// Struct adds a new struct type named name, with no fields until SetFields
// gives it some, and returns it. Every call adds a type of its own, even
// for a name it has seen: struct types are told apart by declaration, not
// by shape.
func (t *Table) Struct(name string) Type {
	return t.add(entry{strct: &structType{name: name}})
}

// Union adds a new union type named name, with no cases until SetCases
// gives it some, and returns it. As for Struct, every call adds a type of
// its own: union types are told apart by declaration, not by shape.
func (t *Table) Union(name string) Type {
	return t.add(entry{union: &unionType{name: name}})
}

// SetCases gives the union type typ its cases, in declaration order. It
// does nothing if typ is not a union type of this Table.
func (t *Table) SetCases(typ Type, cases []Case) {
	if e, ok := t.entry(typ); ok && e.union != nil {
		e.union.cases = cases
	}
}

// IsUnion reports whether typ is a union type.
func (t *Table) IsUnion(typ Type) bool {
	e, ok := t.entry(typ)
	return ok && e.union != nil
}

// Cases returns the cases of the union type typ, in declaration order, or
// nil if typ is not one or has none yet. The caller must not change them.
func (t *Table) Cases(typ Type) []Case {
	e, ok := t.entry(typ)
	if !ok || e.union == nil {
		return nil
	}
	return e.union.cases
}

// Case returns the case of the union type typ named name, its position
// among the cases, which is its tag, and whether typ has one.
func (t *Table) Case(typ Type, name string) (Case, int, bool) {
	for i, c := range t.Cases(typ) {
		if c.Name == name {
			return c, i, true
		}
	}
	return Case{}, 0, false
}

// TypeParam adds a type parameter named name, one of a generic
// function's, and returns its ID: a type of its own, which is no other
// type, has no fields and holds nothing, but is owned and has no zero
// value, since it may stand for any struct or union type, one holding a
// list among them (see Owned and HasZero).
func (t *Table) TypeParam(name string) Type {
	return t.add(entry{param: name})
}

// IsTypeParam reports whether typ is a type parameter that Bind has not
// bound.
func (t *Table) IsTypeParam(typ Type) bool {
	e, ok := t.entry(typ)
	return ok && e.param != ""
}

// Bind makes each type parameter in args, a map from type parameter to
// type, stand for that type until Unbind is called: every question asked
// about it is answered about its type instead, and Bound returns that
// type. It replaces any earlier binding.
func (t *Table) Bind(args map[Type]Type) {
	t.bound = args
}

// Unbind ends the binding Bind made, so every type parameter stands for
// itself again.
func (t *Table) Unbind() {
	t.bound = nil
}

// Bound returns the type typ stands for: the type Bind bound it to, for a
// bound type parameter, and typ itself for any other type.
func (t *Table) Bound(typ Type) Type {
	if t == nil {
		return typ
	}
	if to, ok := t.bound[typ]; ok {
		return to
	}
	return typ
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
	typ = t.Bound(typ)
	if t == nil || typ < numBasic || int(typ-numBasic) >= len(t.entries) {
		return entry{}, false
	}
	return t.entries[typ-numBasic], true
}

// IsArray reports whether typ is an array type.
func (t *Table) IsArray(typ Type) bool {
	e, ok := t.entry(typ)
	return ok && e.strct == nil && e.union == nil && e.param == "" && !e.view && !e.list
}

// IsList reports whether typ is a list type, list[T].
func (t *Table) IsList(typ Type) bool {
	e, ok := t.entry(typ)
	return ok && e.list
}

// IsView reports whether typ is a view type, []T.
func (t *Table) IsView(typ Type) bool {
	e, ok := t.entry(typ)
	return ok && e.view
}

// IsStruct reports whether typ is a struct type: one a Table added for a
// declaration, or Error, the struct no declaration gives, whose fields
// are fixed. A nil Table knows Error too.
func (t *Table) IsStruct(typ Type) bool {
	if typ == Error {
		return true
	}
	e, ok := t.entry(typ)
	return ok && e.strct != nil
}

// errorFields are the fields of Error, in the order tack_error declares
// them in C: code, which is 0 when there is no error, and msg.
var errorFields = []Field{{Name: "code", Type: Int32}, {Name: "msg", Type: String}}

// Elem returns the element type of the array, view or list type typ, or
// Invalid if typ is none of them.
func (t *Table) Elem(typ Type) Type {
	if !t.IsArray(typ) && !t.IsView(typ) && !t.IsList(typ) {
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
// or nil if typ is not one or has none yet. Error's are code and msg. The
// caller must not change them.
func (t *Table) Fields(typ Type) []Field {
	if typ == Error {
		return errorFields
	}
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

// Owned reports whether a value of type typ owns memory it must free: a
// list, which owns its buffer, or an array, a struct or a union holding
// one, directly or through other arrays, structs and unions, a union
// holding one when any of its cases' payloads does. A value of an owned
// type moves rather than copies, and is freed when its owner's block
// ends.
func (t *Table) Owned(typ Type) bool {
	owns := func(typ Type) bool { return t.IsList(typ) || t.IsTypeParam(typ) }
	return t.holds(typ, owns, make(map[Type]bool))
}

// HasZero reports whether type typ has a zero value, which a variable
// declared without a value starts from: every type but a list, an
// Allocator, a union, and an array or a struct holding any of them. An
// empty list still needs an allocator to grow through, no Allocator
// stands for none, and no case of a union is the one a variable starts
// as.
func (t *Table) HasZero(typ Type) bool {
	noZero := func(typ Type) bool {
		return typ == Allocator || t.IsList(typ) || t.IsTypeParam(typ) || t.IsUnion(typ)
	}
	return !t.holds(typ, noZero, make(map[Type]bool))
}

// HoldsUnion reports whether typ is a union type, or an array or a struct
// holding one, directly or through other arrays, structs and unions.
func (t *Table) HoldsUnion(typ Type) bool {
	return t.holds(typ, t.IsUnion, make(map[Type]bool))
}

// holds reports whether typ is one of the types is reports true for, or
// an array, a struct or a union holding one, at any depth, a union
// through its cases' payloads. seen holds the structs and unions already
// being looked at further up, so that one holding itself, which the type
// checker reports elsewhere, cannot loop.
func (t *Table) holds(typ Type, is func(Type) bool, seen map[Type]bool) bool {
	switch {
	case is(typ):
		return true
	case t.IsArray(typ):
		return t.holds(t.Elem(typ), is, seen)
	case t.IsStruct(typ), t.IsUnion(typ):
		if seen[typ] {
			return false
		}
		seen[typ] = true
		for _, part := range t.parts(typ) {
			if t.holds(part, is, seen) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// parts returns the types of the fields of the struct type typ, or of the
// payloads of the union type typ, in order.
func (t *Table) parts(typ Type) []Type {
	var parts []Type
	for _, f := range t.Fields(typ) {
		parts = append(parts, f.Type)
	}
	for _, c := range t.Cases(typ) {
		if c.Payload {
			parts = append(parts, c.Type)
		}
	}
	return parts
}

// Count returns how many types the Table has added, so a caller can visit
// each: their IDs are numBasic through numBasic + Count() - 1, in the order
// they were added. An array, view or list type comes after its element type, but a struct
// or union type may come before the types of its fields or payloads, so a caller that needs
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
// basic type's own name, [n]elem for an array, []elem for a view,
// list[elem] for a list, or a struct or union type's declared name. A type the Table does not know, including one from another Table,
// is named "invalid" rather than guessed at. A nil Table knows the basic
// types.
func (t *Table) Name(typ Type) string {
	return t.NameWith(typ, nil)
}

// NameWith returns a type's name as Name does, but writes each struct or
// union type in it, an array's element type included, as qualify gives
// it, from the type and its declared name. A nil qualify writes the
// declared name, as Name does.
func (t *Table) NameWith(typ Type, qualify func(typ Type, name string) string) string {
	typ = t.Bound(typ)
	if typ >= 0 && typ < numBasic {
		return basicNames[typ]
	}
	e, ok := t.entry(typ)
	declared, isDeclared := e.declaredName()
	switch {
	case !ok:
		return "invalid"
	case isDeclared && qualify != nil:
		return qualify(typ, declared)
	case isDeclared:
		return declared
	case e.param != "":
		return e.param
	default:
		return t.compositeName(e, qualify)
	}
}

// declaredName returns the name e, a struct's or a union's entry, was
// declared with, and whether it is one.
func (e entry) declaredName() (string, bool) {
	switch {
	case e.strct != nil:
		return e.strct.name, true
	case e.union != nil:
		return e.union.name, true
	default:
		return "", false
	}
}

// compositeName returns the name of e, an array's, a view's or a list's
// entry, as NameWith writes it.
func (t *Table) compositeName(e entry, qualify func(typ Type, name string) string) string {
	switch {
	case e.view:
		return "[]" + t.NameWith(e.arr.elem, qualify)
	case e.list:
		return "list[" + t.NameWith(e.arr.elem, qualify) + "]"
	default:
		return "[" + strconv.FormatInt(e.arr.n, 10) + "]" + t.NameWith(e.arr.elem, qualify)
	}
}
