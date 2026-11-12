package types

import (
	"strconv"
	"strings"
)

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
// found again by its element type, and so are a list type, list[T], an
// arena type, arena[T], an id type, id[T], a span type, span[T], a pool
// type, pool[T], and a handle type, handle[T], a
// map type, map[K]V, by its key and value types, and a function type,
// func(A, mut B) R, by its parameters, each a type and whether it is
// mut, and its results. Error
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
	ids   map[array]Type  // each array type's ID, by its shape
	views map[Type]Type   // each view type's ID, by its element type
	lists map[Type]Type   // each list type's ID, by its element type
	maps  map[mapKey]Type // each map type's ID, by its key and value types
	funcs map[string]Type // each function type's ID, by its signature (see funcKey)

	// arenas, refIDs and spans hold each arena, id and span type's ID,
	// by its element type, and pools and handles each pool and handle
	// type's.
	arenas, refIDs, spans, pools, handles map[Type]Type

	// bound maps each type parameter Bind bound to the type it stands
	// for; it is nil when nothing is bound. substituted keeps, while it
	// is, what each type made of type parameters stands for (see Bound).
	bound       map[Type]Type
	substituted map[Type]Type
	// sizes keeps each size Size has computed; SetFields and SetCases
	// forget them all.
	sizes map[Type]Size

	entries []entry // the type with ID numBasic + i, at i

	// Errors counts the errors of the compilation the table was made for,
	// as the type checker left them when it was done: the ones reported
	// before it ran and its own. A backend generates code only from a
	// table with none.
	Errors int
}

// entry is one type a Table added: an array, with its shape, a view, a
// list, an arena, an id, a span, a pool or a handle, with its element type
// in arr.elem, a
// map, with its value type in arr.elem and its key type in key, a struct, with its name and fields, a
// union, with its name and cases, a function type, with its signature,
// or a type parameter, with its name.
type entry struct {
	strct  *structType // nil for anything but a struct
	union  *unionType  // nil for anything but a union
	fn     *funcType   // nil for anything but a function type
	param  string      // a type parameter's name, "" for any other type
	arr    array
	key    Type
	view   bool
	list   bool
	mapped bool
	ref    refKind // an arena's, an id's, a span's, a pool's or a handle's, refNone for any other
}

// refKind says which of the types made of an arena's or a pool's element
// type an entry is: an arena, an id, a span, a pool or a handle, with its
// element type in arr.elem, or none of them.
type refKind int

// The kinds of types made of an arena's or a pool's element type.
const (
	refNone refKind = iota
	refArena
	refID
	refSpan
	refPool
	refHandle
)

// funcType is a function type's signature: its parameters, in order, and
// its result types, in order, none for a function that returns nothing.
type funcType struct {
	params  []FuncParam
	results []Type
}

// FuncParam is one parameter of a function type: its type, and whether
// it is mut, a reference to the caller's place rather than a copy of its
// value, or own, taking its caller's value, which the function then owns.
// A parameter is never both. Scope is whether it is scope, a function
// value that never outlives the call, which may be a closure, or an
// Allocator that does not, which may be a region's; only a parameter
// whose type is a function type or Allocator is, and never a mut or an
// own one.
type FuncParam struct {
	Type  Type
	Mut   bool
	Own   bool
	Scope bool
}

// mapKey identifies a map type by its key and value types.
type mapKey struct {
	key, value Type
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
		ids:     make(map[array]Type),
		views:   make(map[Type]Type),
		lists:   make(map[Type]Type),
		maps:    make(map[mapKey]Type),
		funcs:   make(map[string]Type),
		arenas:  make(map[Type]Type),
		refIDs:  make(map[Type]Type),
		spans:   make(map[Type]Type),
		pools:   make(map[Type]Type),
		handles: make(map[Type]Type),
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

// Arena returns the type of owned, append-only arenas of elements of type
// elem, arena[elem], adding it if the Table does not have it yet. It adds
// list[elem] and []elem first, if the Table does not have them: an arena
// lowers to a list of its elements and its sentinel, and a span of it is
// read as a view of them, so the backend needs both types wherever it
// needs the arena's.
func (t *Table) Arena(elem Type) Type {
	if id, ok := t.arenas[elem]; ok {
		return id
	}
	t.List(elem)
	t.View(elem)
	id := t.add(entry{arr: array{elem: elem}, ref: refArena})
	t.arenas[elem] = id
	return id
}

// ID returns the type of ids of elements of type elem in an arena,
// id[elem], adding it if the Table does not have it yet.
func (t *Table) ID(elem Type) Type {
	if id, ok := t.refIDs[elem]; ok {
		return id
	}
	id := t.add(entry{arr: array{elem: elem}, ref: refID})
	t.refIDs[elem] = id
	return id
}

// Span returns the type of spans, runs of consecutive elements of type
// elem in an arena, span[elem], adding it if the Table does not have it
// yet.
func (t *Table) Span(elem Type) Type {
	if id, ok := t.spans[elem]; ok {
		return id
	}
	id := t.add(entry{arr: array{elem: elem}, ref: refSpan})
	t.spans[elem] = id
	return id
}

// Pool returns the type of owned pools of elements of type elem,
// pool[elem], adding it if the Table does not have it yet.
func (t *Table) Pool(elem Type) Type {
	if id, ok := t.pools[elem]; ok {
		return id
	}
	id := t.add(entry{arr: array{elem: elem}, ref: refPool})
	t.pools[elem] = id
	return id
}

// Handle returns the type of handles of elements of type elem in a pool,
// handle[elem], adding it if the Table does not have it yet.
func (t *Table) Handle(elem Type) Type {
	if id, ok := t.handles[elem]; ok {
		return id
	}
	id := t.add(entry{arr: array{elem: elem}, ref: refHandle})
	t.handles[elem] = id
	return id
}

// Map returns the type of owned maps holding values of type value by keys
// of type key, map[key]value, adding it if the Table does not have it
// yet.
func (t *Table) Map(key, value Type) Type {
	k := mapKey{key: key, value: value}
	if id, ok := t.maps[k]; ok {
		return id
	}
	id := t.add(entry{arr: array{elem: value}, key: key, mapped: true})
	t.maps[k] = id
	return id
}

// Func returns the function type with the parameters params and the
// results results, in order, func(A, mut B) R, adding it if the Table does
// not have it yet: two functions of one signature have one type. The
// Table keeps its own copies of params and results.
func (t *Table) Func(params []FuncParam, results []Type) Type {
	key := funcKey(params, results)
	if id, ok := t.funcs[key]; ok {
		return id
	}
	id := t.add(entry{fn: &funcType{
		params:  append([]FuncParam(nil), params...),
		results: append([]Type(nil), results...),
	}})
	t.funcs[key] = id
	return id
}

// funcKey writes out a function type's signature, the ID of each
// parameter's type, marked when it is mut, own or scope, then the ID of each result's,
// so that two signatures have one key exactly when they are alike.
func funcKey(params []FuncParam, results []Type) string {
	var b strings.Builder
	for _, p := range params {
		if p.Mut {
			b.WriteByte('m')
		}
		if p.Own {
			b.WriteByte('o')
		}
		if p.Scope {
			b.WriteByte('s')
		}
		b.WriteString(strconv.Itoa(int(p.Type)))
		b.WriteByte(',')
	}
	b.WriteByte(';')
	for _, r := range results {
		b.WriteString(strconv.Itoa(int(r)))
		b.WriteByte(',')
	}
	return b.String()
}

// IsFunc reports whether typ is a function type, func(A) R.
func (t *Table) IsFunc(typ Type) bool {
	e, ok := t.entry(typ)
	return ok && e.fn != nil
}

// FuncParams returns the parameters of the function type typ, in order,
// or nil if typ is not one. The caller must not change them.
func (t *Table) FuncParams(typ Type) []FuncParam {
	e, ok := t.entry(typ)
	if !ok || e.fn == nil {
		return nil
	}
	return e.fn.params
}

// FuncResults returns the result types of the function type typ, in
// order, or nil if typ is not one or returns nothing. The caller must not
// change them.
func (t *Table) FuncResults(typ Type) []Type {
	e, ok := t.entry(typ)
	if !ok || e.fn == nil {
		return nil
	}
	return e.fn.results
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
		t.sizes = nil
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
// type. A type made of type parameters, list[T], []T, map[K]T or func(T)
// R, stands for the type made the same way of what they stand for, which
// the Table adds if it has not yet. It replaces any earlier binding.
func (t *Table) Bind(args map[Type]Type) {
	t.bound = args
	t.substituted = make(map[Type]Type)
}

// Unbind ends the binding Bind made, so every type parameter stands for
// itself again.
func (t *Table) Unbind() {
	t.bound = nil
	t.substituted = nil
}

// Bound returns the type typ stands for: the type Bind bound it to, for a
// bound type parameter, the type made the same way of what its parts
// stand for, for an array, a view, a list, a map or a function type made
// of a bound type parameter, and typ itself for any other type.
func (t *Table) Bound(typ Type) Type {
	if t == nil || t.bound == nil {
		return typ
	}
	if to, ok := t.bound[typ]; ok {
		return to
	}
	if to, ok := t.substituted[typ]; ok {
		return to
	}
	to := t.substitute(typ)
	t.substituted[typ] = to
	return to
}

// substitute returns the type made as typ is, an array, a view, a list,
// a map or a function type, of what each of its parts stands for under
// the binding, adding it if the Table does not have it, or typ itself
// when no part stands for another type, as for any other type.
func (t *Table) substitute(typ Type) Type {
	if typ < numBasic || int(typ-numBasic) >= len(t.entries) {
		return typ
	}
	e := t.entries[typ-numBasic]
	switch {
	case e.fn != nil:
		return t.substituteFunc(typ, e.fn)
	case e.mapped:
		key, value := t.Bound(e.key), t.Bound(e.arr.elem)
		if key == e.key && value == e.arr.elem {
			return typ
		}
		return t.Map(key, value)
	case e.strct != nil, e.union != nil, e.param != "":
		return typ
	default:
		return t.substituteElem(typ, e)
	}
}

// substituteElem returns typ, a view, a list, an arena, an id, a span, a
// pool, a handle or an array whose entry is
// e, with its element type replaced by what it stands for, or typ itself
// when that is its own.
func (t *Table) substituteElem(typ Type, e entry) Type {
	elem := t.Bound(e.arr.elem)
	switch {
	case elem == e.arr.elem:
		return typ
	case e.view:
		return t.View(elem)
	case e.list:
		return t.List(elem)
	case e.ref == refArena:
		return t.Arena(elem)
	case e.ref == refID:
		return t.ID(elem)
	case e.ref == refSpan:
		return t.Span(elem)
	case e.ref == refPool:
		return t.Pool(elem)
	case e.ref == refHandle:
		return t.Handle(elem)
	default:
		return t.Array(elem, e.arr.n)
	}
}

// substituteFunc returns the function type typ, whose signature is fn,
// with each of its parameters' and results' types replaced by what it
// stands for, or typ itself when none stands for another type.
func (t *Table) substituteFunc(typ Type, fn *funcType) Type {
	changed := false
	params := make([]FuncParam, len(fn.params))
	for i, p := range fn.params {
		params[i] = FuncParam{Type: t.Bound(p.Type), Mut: p.Mut, Own: p.Own, Scope: p.Scope}
		changed = changed || params[i].Type != p.Type
	}
	results := make([]Type, len(fn.results))
	for i, r := range fn.results {
		results[i] = t.Bound(r)
		changed = changed || results[i] != r
	}
	if !changed {
		return typ
	}
	return t.Func(params, results)
}

// HasTypeParam reports whether typ is a type parameter, or is made of
// one, as an array's, a view's or a list's element, a map's key or value,
// or a function type's parameter or result, that no binding stands for.
// A struct or a union is never made of one.
func (t *Table) HasTypeParam(typ Type) bool {
	e, ok := t.entry(t.Bound(typ))
	switch {
	case !ok, e.strct != nil, e.union != nil:
		return false
	case e.param != "":
		return true
	}
	for _, part := range e.madeOf() {
		if t.HasTypeParam(part) {
			return true
		}
	}
	return false
}

// madeOf returns the types e, an array's, a view's, a list's, a map's or
// a function type's entry, is made of: an element type, a map's key and
// value types, or a function type's parameters' and results' types.
func (e entry) madeOf() []Type {
	switch {
	case e.fn != nil:
		parts := make([]Type, 0, len(e.fn.params)+len(e.fn.results))
		for _, p := range e.fn.params {
			parts = append(parts, p.Type)
		}
		return append(parts, e.fn.results...)
	case e.mapped:
		return []Type{e.key, e.arr.elem}
	default:
		return []Type{e.arr.elem}
	}
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
		t.sizes = nil
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
	return ok && e.strct == nil && e.union == nil && e.fn == nil && e.param == "" && !e.view &&
		!e.list && !e.mapped && e.ref == refNone
}

// IsArena reports whether typ is an arena type, arena[T].
func (t *Table) IsArena(typ Type) bool {
	e, ok := t.entry(typ)
	return ok && e.ref == refArena
}

// IsID reports whether typ is an id type, id[T].
func (t *Table) IsID(typ Type) bool {
	e, ok := t.entry(typ)
	return ok && e.ref == refID
}

// IsSpan reports whether typ is a span type, span[T].
func (t *Table) IsSpan(typ Type) bool {
	e, ok := t.entry(typ)
	return ok && e.ref == refSpan
}

// IsRef reports whether typ is an id or a span type, which names
// elements of an arena and holds none.
func (t *Table) IsRef(typ Type) bool {
	return t.IsID(typ) || t.IsSpan(typ)
}

// IsPool reports whether typ is a pool type, pool[T].
func (t *Table) IsPool(typ Type) bool {
	e, ok := t.entry(typ)
	return ok && e.ref == refPool
}

// IsHandle reports whether typ is a handle type, handle[T].
func (t *Table) IsHandle(typ Type) bool {
	e, ok := t.entry(typ)
	return ok && e.ref == refHandle
}

// Buffered reports whether typ is a list or an arena type, each of which
// owns a buffer of its elements that growing it may move.
func (t *Table) Buffered(typ Type) bool {
	return t.IsList(typ) || t.IsArena(typ)
}

// IsList reports whether typ is a list type, list[T].
func (t *Table) IsList(typ Type) bool {
	e, ok := t.entry(typ)
	return ok && e.list
}

// IsMap reports whether typ is a map type, map[K]V.
func (t *Table) IsMap(typ Type) bool {
	e, ok := t.entry(typ)
	return ok && e.mapped
}

// Key returns the key type of the map type typ, or Invalid if typ is not
// one.
func (t *Table) Key(typ Type) Type {
	e, ok := t.entry(typ)
	if !ok || !e.mapped {
		return Invalid
	}
	return e.key
}

// IsView reports whether typ is a view type, []T.
func (t *Table) IsView(typ Type) bool {
	e, ok := t.entry(typ)
	return ok && e.view
}

// InAny reports whether a value of type typ can be passed as any: one of
// the fourteen basic types the package-level InAny accepts, or a view of
// one of them, []uint8, []string or []float64, which an any refers to as it does a
// basic value. A view of any other type cannot.
func (t *Table) InAny(typ Type) bool {
	return InAny(typ) || t.IsView(typ) && InAny(t.Elem(typ))
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

// Elem returns the element type of the array, view, list, arena, id,
// span, pool or handle type typ, or the value type of the map type typ, or
// Invalid if typ is none of them.
func (t *Table) Elem(typ Type) Type {
	if !t.IsArray(typ) && !t.IsView(typ) && !t.IsList(typ) && !t.IsMap(typ) && !t.IsArena(typ) &&
		!t.IsRef(typ) && !t.IsPool(typ) && !t.IsHandle(typ) {
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
// list, which owns its buffer, an arena, which owns its buffer and its
// sentinel, a pool, which owns its buffer and its live elements, a map,
// which owns its buffers, keys and
// values, or an array, a struct or a union holding
// one, directly or through other arrays, structs and unions, a union
// holding one when any of its cases' payloads does. A value of an owned
// type moves rather than copies, and is freed when its owner's block
// ends.
func (t *Table) Owned(typ Type) bool {
	owns := func(typ Type) bool {
		return t.IsList(typ) || t.IsArena(typ) || t.IsPool(typ) || t.IsMap(typ) ||
			t.IsTypeParam(typ)
	}
	return t.holds(typ, owns, make(map[Type]bool))
}

// HasZero reports whether type typ has a zero value, which a variable
// declared without a value starts from: every type but a list, an
// arena, a pool, a map, an Allocator, a union, a function type, and an
// array or a struct holding any of them. An arena needs its sentinel; an
// id's zero value is the sentinel's id, a span's the empty span, and a
// handle's one no pool gives. An empty list, pool or map still needs an allocator to grow
// through, no Allocator stands for none, no case of a union is the one a
// variable starts as, and no function is the one a function value starts
// as.
func (t *Table) HasZero(typ Type) bool {
	noZero := func(typ Type) bool {
		return typ == Allocator || t.IsList(typ) || t.IsArena(typ) || t.IsPool(typ) ||
			t.IsMap(typ) || t.IsTypeParam(typ) || t.IsUnion(typ) || t.IsFunc(typ)
	}
	return !t.holds(typ, noZero, make(map[Type]bool))
}

// HoldsList reports whether typ is a list type, or an array, a struct or a
// union holding one, directly or through other arrays, structs and
// unions.
func (t *Table) HoldsList(typ Type) bool {
	return t.holds(typ, t.IsList, make(map[Type]bool))
}

// HoldsArena reports whether typ is an arena type, or an array, a struct
// or a union holding one, directly or through other arrays, structs and
// unions.
func (t *Table) HoldsArena(typ Type) bool {
	return t.holds(typ, t.IsArena, make(map[Type]bool))
}

// HoldsPool reports whether typ is a pool type, or an array, a struct or
// a union holding one, directly or through other arrays, structs and
// unions.
func (t *Table) HoldsPool(typ Type) bool {
	return t.holds(typ, t.IsPool, make(map[Type]bool))
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
// they were added. An array, view, list, map or function type comes after the types it is
// made of, but a struct or union type may come before the types of its fields or payloads,
// so a caller that needs every type a type is made of first has to order them itself.
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
// list[elem] for a list, arena[elem], id[elem] and span[elem] for an
// arena, an id and a span, pool[elem] and handle[elem] for a pool and a
// handle, map[key]value for a map, func(A, mut B) (R, S)
// for a function type, or a struct or union type's declared name. A type the Table does not know, including one from another Table,
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

// compositeName returns the name of e, an array's, a view's, a list's, a
// map's or a function type's entry, as NameWith writes it.
func (t *Table) compositeName(e entry, qualify func(typ Type, name string) string) string {
	switch {
	case e.view:
		return "[]" + t.NameWith(e.arr.elem, qualify)
	case e.list:
		return "list[" + t.NameWith(e.arr.elem, qualify) + "]"
	case e.ref == refArena:
		return "arena[" + t.NameWith(e.arr.elem, qualify) + "]"
	case e.ref == refID:
		return "id[" + t.NameWith(e.arr.elem, qualify) + "]"
	case e.ref == refSpan:
		return "span[" + t.NameWith(e.arr.elem, qualify) + "]"
	case e.ref == refPool:
		return "pool[" + t.NameWith(e.arr.elem, qualify) + "]"
	case e.ref == refHandle:
		return "handle[" + t.NameWith(e.arr.elem, qualify) + "]"
	case e.mapped:
		return "map[" + t.NameWith(e.key, qualify) + "]" + t.NameWith(e.arr.elem, qualify)
	case e.fn != nil:
		return t.funcName(e.fn, qualify)
	default:
		return "[" + strconv.FormatInt(e.arr.n, 10) + "]" + t.NameWith(e.arr.elem, qualify)
	}
}

// funcName returns the name of the function type fn as NameWith writes
// it: func, its parameters' types in parentheses, mut before a mut one,
// own before an own one and scope before a scope one,
// and its results, none, one alone, or several in parentheses.
func (t *Table) funcName(fn *funcType, qualify func(typ Type, name string) string) string {
	params := make([]string, len(fn.params))
	for i, p := range fn.params {
		params[i] = t.NameWith(p.Type, qualify)
		if p.Mut {
			params[i] = "mut " + params[i]
		}
		if p.Own {
			params[i] = "own " + params[i]
		}
		if p.Scope {
			params[i] = "scope " + params[i]
		}
	}
	name := "func(" + strings.Join(params, ", ") + ")"
	results := make([]string, len(fn.results))
	for i, r := range fn.results {
		results[i] = t.NameWith(r, qualify)
	}
	switch len(results) {
	case 0:
		return name
	case 1:
		return name + " " + results[0]
	default:
		return name + " (" + strings.Join(results, ", ") + ")"
	}
}
