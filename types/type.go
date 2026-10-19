package types

// Type is a Tack type, identified by a number. Every distinct type has
// exactly one ID within a compilation, so two types are the same type exactly
// when their IDs are equal, and == is type identity.
//
// The basic types have fixed IDs, the same in every Table, so they can be
// named as constants without one. Any other type would be given an ID by the
// Table of the compilation that uses it, and means nothing without that
// Table. The language has no such types yet.
type Type int

const (
	// Invalid is the zero value, carried by an expression that has not been
	// checked or whose type could not be determined.
	Invalid Type = iota
	// Int64 is a signed 64-bit integer.
	Int64
	// String is a string of bytes.
	String
	// Bool is true or false.
	Bool
	// Int8 is a signed 8-bit integer.
	Int8
	// Int16 is a signed 16-bit integer.
	Int16
	// Int32 is a signed 32-bit integer.
	Int32
	// Uint8 is an unsigned 8-bit integer.
	Uint8
	// Uint16 is an unsigned 16-bit integer.
	Uint16
	// Uint32 is an unsigned 32-bit integer.
	Uint32
	// Uint64 is an unsigned 64-bit integer.
	Uint64
	// UnsafeSize is unsafe_size_t, C's size_t: an unsigned integer as wide
	// as the target makes it, from 32 to 64 bits. The compiler does not
	// know which, so its rules hold whatever the width is (see TargetWidth).
	UnsafeSize
	// UnsafeUintptr is unsafe_uintptr_t, C's uintptr_t: an unsigned
	// integer wide enough to hold an address, from 32 to 64 bits, like
	// UnsafeSize, though not necessarily as wide as it.
	UnsafeUintptr
	// UntypedInt is the type of an integer constant expression, made only
	// of literals and operators, until the place it is used gives it a
	// type: the type it is assigned, passed or compared to, or Int64 where
	// nothing does. No program can write it.
	UntypedInt
	// Any is any, a reference to a value of some other type, which one
	// known only at run time: in C, a type id and a pointer to the value.
	// It can only be written as the type of a parameter, or the element
	// type of a variadic one or of a view parameter, though an immutable
	// local declared from one is one too; a value of it can be passed on
	// to another any parameter, or asked which of the types InAny accepts
	// it holds. Only a value of such a type can be passed as one.
	Any
	// Error is error, a code and a message, code 0 meaning no error: in C,
	// tack_error, defined in the runtime's tack.h. It is a struct whose fields
	// are fixed, code, an int32, and msg, a string (see errorFields), so
	// a Table reports it as a struct, though no declaration gives it.
	Error
	// Allocator is Allocator, an opaque handle on an allocator that a
	// list allocates through and is freed through: in C, tack_allocator,
	// an index into the allocators the runtime knows and a generation,
	// defined in the runtime's tack.h. It is copyable, has no zero value, no
	// order and no ==; c_allocator is its one value today.
	Allocator
	// Float32 is an IEEE 754 binary32 float: in C, float.
	Float32
	// Float64 is an IEEE 754 binary64 float: in C, double.
	Float64
	// UntypedFloat is the type of a float constant expression, made only
	// of literals and operators with a float literal among them, until the
	// place it is used gives it a float type, or Float64 where nothing
	// does. Its value is exact until then (see constant.Float). No program
	// can write it.
	UntypedFloat

	// numBasic is the number of basic types, Invalid included. It is the
	// first ID a Table would hand out.
	numBasic
)

// basicNames holds each basic type's name as a program writes it, indexed by
// its ID. UntypedInt is named as Go names it, though no message should need
// to.
var basicNames = [numBasic]string{
	Invalid:       "invalid",
	Int64:         "int64",
	String:        "string",
	Bool:          "bool",
	Int8:          "int8",
	Int16:         "int16",
	Int32:         "int32",
	Uint8:         "uint8",
	Uint16:        "uint16",
	Uint32:        "uint32",
	Uint64:        "uint64",
	UnsafeSize:    "unsafe_size_t",
	UnsafeUintptr: "unsafe_uintptr_t",
	UntypedInt:    "untyped int",
	Any:           "any",
	Error:         "error",
	Allocator:     "Allocator",
	Float32:       "float32",
	Float64:       "float64",
	UntypedFloat:  "untyped float",
}

// Lookup returns the basic type a type name denotes. Invalid, UntypedInt
// and UntypedFloat have no name a program can write, so none is ever
// returned as a match.
func Lookup(name string) (Type, bool) {
	for t := Int64; t < numBasic; t++ {
		if !IsUntyped(t) && basicNames[t] == name {
			return t, true
		}
	}
	return Invalid, false
}

// Names returns the name of every basic type a program can write, in the
// order of their IDs: the builtin types.
func Names() []string {
	var names []string
	for t := Int64; t < numBasic; t++ {
		if !IsUntyped(t) {
			names = append(names, basicNames[t])
		}
	}
	return names
}

// InAny reports whether a value of type t can be passed as any: t is one
// of the fourteen basic types a value can have, every integer type, bool,
// string, float32 and float64. An array, a view, a list, a struct, error,
// Allocator, any itself and an untyped constant cannot; an untyped
// constant is given int64, or float64, first.
func InAny(t Type) bool {
	return t >= Int64 && t < UntypedInt || t == Float32 || t == Float64
}

// IsUntyped reports whether t is the type of an untyped constant,
// UntypedInt or UntypedFloat.
func IsUntyped(t Type) bool { return t == UntypedInt || t == UntypedFloat }

// IsFloat reports whether t is one of the float types, UntypedFloat
// included.
func IsFloat(t Type) bool { return t == Float32 || t == Float64 || t == UntypedFloat }

// IsNumeric reports whether t is an integer or a float type, an untyped
// one included: one the arithmetic operators + - * and / work on.
func IsNumeric(t Type) bool { return IsInteger(t) || IsFloat(t) }

// IsInteger reports whether t is one of the integer types, UntypedInt
// included.
func IsInteger(t Type) bool {
	return t == Int64 || (t >= Int8 && t <= UntypedInt)
}

// IsUnsigned reports whether t is an unsigned integer type, a
// target-width one included.
func IsUnsigned(t Type) bool { return t >= Uint8 && t <= UnsafeUintptr }

// TargetWidth reports whether t is one of the integer types whose width is
// the target's, unsafe_size_t and unsafe_uintptr_t: somewhere from 32 to
// 64 bits, and not known to the compiler. Every rule about such a type is
// one that holds at any width in that range, so a program valid on one
// target is valid on every other.
func TargetWidth(t Type) bool { return t == UnsafeSize || t == UnsafeUintptr }

// PortableMax is the largest value every target-width type holds on every
// target: the largest 32-bit unsigned value. A constant of such a type
// must lie between 0 and it.
const PortableMax = 1<<32 - 1

// Bits returns the width of a fixed-width integer type, or 0 for any other
// type, a target-width one included, whose width is not known.
func Bits(t Type) int {
	switch t {
	case Int8, Uint8:
		return 8
	case Int16, Uint16:
		return 16
	case Int32, Uint32:
		return 32
	case Int64, Uint64:
		return 64
	default:
		return 0
	}
}

// PortableBits returns the width in which every value of the integer type
// t is a value of t on every target: Bits for a fixed-width type, and 32
// for a target-width type, the narrowest it can be. A constant of t must
// fit it. It is 0 for any other type.
func PortableBits(t Type) int {
	if TargetWidth(t) {
		return 32
	}
	return Bits(t)
}

// MaxBits returns the widest the integer type t can be on any target:
// Bits for a fixed-width type, and 64 for a target-width type. Every value
// t can hold anywhere fits it. It is 0 for any other type.
func MaxBits(t Type) int {
	if TargetWidth(t) {
		return 64
	}
	return Bits(t)
}

// Widens reports whether every value of the integer type from is also a
// value of to, on every target, so a value of from can be used where to is
// expected without a conversion: a signed type to a wider signed one, and
// an unsigned type to a wider type of either kind. A target-width type
// takes uint8, uint16 and uint32, which fit it at its narrowest, and
// widens to uint64, which holds it at its widest; nothing else widens to
// or from one, not even the other target-width type, which may be
// narrower. A type widens to itself. No other pair does, and no
// non-integer type widens to anything but itself.
func Widens(from, to Type) bool {
	switch {
	case from == to:
		return true
	case TargetWidth(to):
		return from == Uint8 || from == Uint16 || from == Uint32
	case TargetWidth(from):
		return to == Uint64
	default:
		return fixedWidens(from, to)
	}
}

// fixedWidens is Widens for two different types of which neither is a
// target-width type: both must be fixed-width integer types, the one
// narrower than the other, and a signed type never widens to an unsigned
// one.
func fixedWidens(from, to Type) bool {
	fb, tb := Bits(from), Bits(to)
	if fb == 0 || tb == 0 {
		return false
	}
	switch {
	case IsUnsigned(from) && IsUnsigned(to):
		return fb < tb
	case IsUnsigned(from):
		return fb < tb
	case IsUnsigned(to):
		return false
	default:
		return fb < tb
	}
}
