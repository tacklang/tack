package types

import "math/bits"

// MaxValueSize is the most bytes a value of any type may hold, 2^31, and
// MaxGlobalsSize the most all of a program's globals may hold together,
// 2^30. The C backend's code runs under clang's default code models,
// which reach static data only within a few GiB of the code: within
// 2 GiB on x86-64, RIP-relative, and 4 GiB on arm64. On x86-64 exactly
// 2^31 bytes of statics fail to link, "32-bit RIP-relative reference out
// of range", so the globals' limit is half that, leaving room for the
// code and the C library's statics; bigger data goes in a list.
const (
	MaxValueSize   uint64 = 1 << 31
	MaxGlobalsSize uint64 = 1 << 30
)

// Size is how a value of a type is laid out in C, as the C backend lowers
// it: Bytes, what sizeof gives, and Align, what _Alignof gives. Overflow
// is set when the size does not fit a uint64, and Bytes is then
// meaningless.
type Size struct {
	Bytes    uint64
	Align    uint64
	Overflow bool
}

// TooLarge reports whether a value of this size holds more than
// MaxValueSize bytes.
func (s Size) TooLarge() bool {
	return s.Overflow || s.Bytes > MaxValueSize
}

// Size returns the size of typ as the C backend lays it out, for the
// LP64 targets it builds for, arm64 and x86-64: a pointer, an int64 and a
// size_t are 8 bytes, aligned to 8, and C pads each struct and union as
// it does there. A 32-bit target would give other sizes. Every part of
// the lowering counts: an array of no elements still holds one, a struct
// with no fields one char, and a union a tag before its payloads. A
// target-width integer, unsafe_size_t or unsafe_uintptr_t, counts at its
// widest, 8 bytes, so a size is never less than any target gives.
//
// Sizes are computed once each and kept, so they must be asked for only
// once every struct's fields and every union's cases are set; setting
// them forgets every size kept so far. A type parameter, Invalid or a
// type that contains itself, which the checker reports, has size 0.
func (t *Table) Size(typ Type) Size {
	return t.size(typ, map[Type]bool{})
}

// size is Size, with visiting holding the types whose sizes are being
// computed, so a type that contains itself ends the recursion.
func (t *Table) size(typ Type, visiting map[Type]bool) Size {
	typ = t.Bound(typ)
	if s, ok := basicSizes[typ]; ok {
		return s
	}
	if t == nil || visiting[typ] {
		return Size{}
	}
	if s, ok := t.sizes[typ]; ok {
		return s
	}
	visiting[typ] = true
	s := t.layout(typ, visiting)
	delete(visiting, typ)
	if s.Align == 0 {
		s = Size{}
	}
	if t.sizes == nil {
		t.sizes = make(map[Type]Size)
	}
	t.sizes[typ] = s
	return s
}

// layout computes the size of typ, a type that is not basic, from its
// parts.
func (t *Table) layout(typ Type, visiting map[Type]bool) Size {
	switch {
	case typ == Error:
		return t.structSize(errorFields, visiting)
	case t.IsView(typ), t.IsList(typ), t.IsMap(typ), t.IsFunc(typ):
		return t.handleSize(typ)
	case t.IsArray(typ):
		elem := t.size(t.Elem(typ), visiting)
		// An array type's length is never negative (see Array), and one
		// of no elements holds one.
		n := uint64(max(t.Len(typ), 1)) //nolint:gosec // G115: at least 1, so it converts exactly
		hi, lo := bits.Mul64(elem.Bytes, n)
		return Size{Bytes: lo, Align: elem.Align, Overflow: elem.Overflow || hi != 0}
	case t.IsStruct(typ):
		return t.structSize(t.Fields(typ), visiting)
	case t.IsUnion(typ):
		return t.unionSize(t.Cases(typ), visiting)
	default:
		return Size{}
	}
}

// handleSize returns the size of typ, a view, a list, a map or a
// function type, each a fixed handle on what it refers to or owns.
func (t *Table) handleSize(typ Type) Size {
	switch {
	case t.IsView(typ):
		return Size{Bytes: 16, Align: 8} // a pointer and an int64 length
	case t.IsList(typ):
		return Size{Bytes: 32, Align: 8} // a pointer, len, cap and an Allocator
	case t.IsMap(typ):
		// Its entries' pointer, length, how many are used, their
		// capacity, its slots' pointer and count, and an Allocator.
		return Size{Bytes: 56, Align: 8}
	default:
		return Size{Bytes: 8, Align: 8} // a pointer to the function
	}
}

// structSize lays out fields in order, each at the next offset its
// alignment allows, and pads the whole to the largest alignment, as C
// does; no fields is one char.
func (t *Table) structSize(fields []Field, visiting map[Type]bool) Size {
	if len(fields) == 0 {
		return Size{Bytes: 1, Align: 1}
	}
	acc := Size{Align: 1}
	for _, f := range fields {
		acc = place(acc, t.size(f.Type, visiting))
	}
	return padTo(acc, acc.Align)
}

// unionSize lays out a union as the C backend does: a struct holding the
// tag, the narrowest unsigned integer that numbers every case, then a C
// union of the payloads, as big as the biggest padded to the largest
// alignment, or one char when no case has a payload.
func (t *Table) unionSize(cases []Case, visiting map[Type]bool) Size {
	tag := Size{Bytes: 1, Align: 1}
	switch {
	case len(cases) > 1<<16:
		tag = Size{Bytes: 4, Align: 4}
	case len(cases) > 1<<8:
		tag = Size{Bytes: 2, Align: 2}
	}
	payloads := Size{Align: 1}
	payload := false
	for _, c := range cases {
		if !c.Payload {
			continue
		}
		payload = true
		p := t.size(c.Type, visiting)
		payloads.Bytes = max(payloads.Bytes, p.Bytes)
		payloads.Align = max(payloads.Align, p.Align)
		payloads.Overflow = payloads.Overflow || p.Overflow
	}
	if !payload {
		payloads = Size{Bytes: 1, Align: 1}
	}
	payloads = padTo(payloads, payloads.Align)
	return padTo(place(place(Size{Align: 1}, tag), payloads), max(tag.Align, payloads.Align))
}

// place adds a member of size m to a struct laid out so far as acc: at
// the next offset aligned for m, then m's bytes.
func place(acc, m Size) Size {
	acc = padTo(acc, max(m.Align, 1))
	sum, carry := bits.Add64(acc.Bytes, m.Bytes, 0)
	return Size{
		Bytes:    sum,
		Align:    max(acc.Align, m.Align),
		Overflow: acc.Overflow || m.Overflow || carry != 0,
	}
}

// padTo rounds s's bytes up to a multiple of align, a power of two,
// keeping s's alignment.
func padTo(s Size, align uint64) Size {
	sum, carry := bits.Add64(s.Bytes, align-1, 0)
	s.Bytes = sum &^ (align - 1)
	s.Overflow = s.Overflow || carry != 0
	return s
}

// basicSizes are the sizes of the basic types in C, for the LP64 targets
// Size describes: tack_string a pointer and an int64, tack_any a
// uint32_t type id and a pointer, tack_allocator two uint32_t, float and
// double IEEE 754's binary32 and binary64, and the target-width integers
// at their widest. Error is a struct, laid out
// from its fields.
var basicSizes = map[Type]Size{
	Int8:          {Bytes: 1, Align: 1},
	Uint8:         {Bytes: 1, Align: 1},
	Bool:          {Bytes: 1, Align: 1},
	Int16:         {Bytes: 2, Align: 2},
	Uint16:        {Bytes: 2, Align: 2},
	Int32:         {Bytes: 4, Align: 4},
	Uint32:        {Bytes: 4, Align: 4},
	Int64:         {Bytes: 8, Align: 8},
	Uint64:        {Bytes: 8, Align: 8},
	UnsafeSize:    {Bytes: 8, Align: 8},
	UnsafeUintptr: {Bytes: 8, Align: 8},
	String:        {Bytes: 16, Align: 8},
	Any:           {Bytes: 16, Align: 8},
	Allocator:     {Bytes: 8, Align: 4},
	Float32:       {Bytes: 4, Align: 4},
	Float64:       {Bytes: 8, Align: 8},
}
