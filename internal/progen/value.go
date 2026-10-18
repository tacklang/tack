package progen

// Value is a value a generated program computes, as the interpreter holds
// it. Which fields mean anything follows from its type.
type Value struct {
	List  *[]Value // a list, shared by every name of it
	Map   *omap    // a map, shared likewise
	Fn    *Func    // a function value
	Pay   *Value   // a union's payload, nil for none
	S     string   // a string
	Elems []Value  // an array's elements, a struct's fields
	Bits  uint64   // an integer, as norm holds it
	Tag   int      // a union's case
	Bool  bool     // a bool
	Valid bool     // whether a union or a function value holds anything
}

// omap is a map in insertion order, as Tack's maps are: a key put again
// keeps its place, and one deleted and put again goes last.
type omap struct {
	vals map[uint64]Value
	keys []uint64
}

// newMap returns an empty omap.
func newMap() *omap {
	return &omap{vals: make(map[uint64]Value)}
}

// put puts v under k.
func (m *omap) put(k uint64, v Value) {
	if _, ok := m.vals[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.vals[k] = v
}

// get returns the value under k, and whether there is one.
func (m *omap) get(k uint64) (Value, bool) {
	v, ok := m.vals[k]
	return v, ok
}

// remove takes k out, and reports whether it was there.
func (m *omap) remove(k uint64) (Value, bool) {
	v, ok := m.vals[k]
	if !ok {
		return Value{}, false
	}
	delete(m.vals, k)
	for i, key := range m.keys {
		if key == k {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			break
		}
	}
	return v, true
}

// copyValue returns a copy of v as Tack copies a value of a copyable
// type: an array's elements and a struct's fields are copied too.
func copyValue(v Value) Value {
	if v.Elems != nil {
		elems := make([]Value, len(v.Elems))
		for i, e := range v.Elems {
			elems[i] = copyValue(e)
		}
		v.Elems = elems
	}
	if v.Pay != nil {
		p := copyValue(*v.Pay)
		v.Pay = &p
	}
	return v
}

// zero returns the zero value of t, which every copyable type the
// generator writes has; a list or a map is made empty.
func zero(t *Type) Value {
	switch t.Kind {
	case KArray:
		elems := make([]Value, t.Len)
		for i := range elems {
			elems[i] = zero(t.Elem)
		}
		return Value{Elems: elems}
	case KStruct:
		elems := make([]Value, len(t.Struct.Fields))
		for i, f := range t.Struct.Fields {
			elems[i] = zero(f.T)
		}
		return Value{Elems: elems}
	case KList:
		l := []Value{}
		return Value{List: &l}
	case KMap:
		return Value{Map: newMap()}
	}
	return Value{}
}

// equal reports whether a and b, values of t, are equal, as Tack's ==.
func equal(t *Type, a, b Value) bool {
	switch t.Kind {
	case KInt:
		return a.Bits == b.Bits
	case KBool:
		return a.Bool == b.Bool
	case KString:
		return a.S == b.S
	case KArray:
		return equalElems(t.Elem, a.Elems, b.Elems)
	case KStruct:
		for i, f := range t.Struct.Fields {
			if !equal(f.T, a.Elems[i], b.Elems[i]) {
				return false
			}
		}
		return true
	case KUnion:
		return equalUnion(t.Union, a, b)
	}
	return false
}

// equalElems reports whether two arrays of elem are equal.
func equalElems(elem *Type, a, b []Value) bool {
	for i := range a {
		if !equal(elem, a[i], b[i]) {
			return false
		}
	}
	return true
}

// equalUnion reports whether two values of u are equal: the same case,
// and equal payloads.
func equalUnion(u *UnionDecl, a, b Value) bool {
	if a.Tag != b.Tag {
		return false
	}
	p := u.Cases[a.Tag].Payload
	return p == nil || equal(p, *a.Pay, *b.Pay)
}
