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
	// UntypedInt is the type of an integer constant expression, made only
	// of literals and operators, until the place it is used gives it a
	// type: the type it is assigned, passed or compared to, or Int64 where
	// nothing does. No program can write it.
	UntypedInt

	// numBasic is the number of basic types, Invalid included. It is the
	// first ID a Table would hand out.
	numBasic
)

// basicNames holds each basic type's name as a program writes it, indexed by
// its ID. UntypedInt is named as Go names it, though no message should need
// to.
var basicNames = [numBasic]string{
	Invalid:    "invalid",
	Int64:      "int64",
	String:     "string",
	Bool:       "bool",
	Int8:       "int8",
	Int16:      "int16",
	Int32:      "int32",
	Uint8:      "uint8",
	Uint16:     "uint16",
	Uint32:     "uint32",
	Uint64:     "uint64",
	UntypedInt: "untyped int",
}

// Lookup returns the basic type a type name denotes. Invalid and UntypedInt
// have no name a program can write, so neither is ever returned as a match.
func Lookup(name string) (Type, bool) {
	for t := Int64; t < UntypedInt; t++ {
		if basicNames[t] == name {
			return t, true
		}
	}
	return Invalid, false
}

// IsInteger reports whether t is one of the integer types, UntypedInt
// included.
func IsInteger(t Type) bool {
	return t == Int64 || (t >= Int8 && t <= UntypedInt)
}

// IsUnsigned reports whether t is an unsigned integer type.
func IsUnsigned(t Type) bool { return t >= Uint8 && t <= Uint64 }

// Bits returns the width of a sized integer type, or 0 for any other type.
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

// Widens reports whether every value of the sized integer type from is also
// a value of to, so a value of from can be used where to is expected without
// a conversion: a signed type to a wider signed one, and an unsigned type to
// a wider type of either kind. A type widens to itself. No other pair does,
// and no non-integer type widens to anything but itself.
func Widens(from, to Type) bool {
	if from == to {
		return true
	}
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
