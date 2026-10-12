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

	// numBasic is the number of basic types, Invalid included. It is the
	// first ID a Table would hand out.
	numBasic
)

// basicNames holds each basic type's name as a program writes it, indexed by
// its ID.
var basicNames = [numBasic]string{
	Invalid: "invalid",
	Int64:   "int64",
	String:  "string",
	Bool:    "bool",
}

// Lookup returns the basic type a type name denotes. Invalid has no name a
// program can write, so it is never returned as a match.
func Lookup(name string) (Type, bool) {
	for t := Int64; t < numBasic; t++ {
		if basicNames[t] == name {
			return t, true
		}
	}
	return Invalid, false
}
