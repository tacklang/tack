// Package types holds Tack's type representation.
//
// A Type is an ID: a small integer that a Table gives meaning to. Every
// distinct type has exactly one ID within a compilation, so == on two Types
// is type identity, with no structural comparison to remember. The zero
// value, Invalid, marks an expression that has not been checked or whose type
// could not be determined, so an AST field the type checker has not filled in
// holds Invalid rather than nil.
//
// The basic types have fixed IDs that mean the same thing in every Table, so
// code can name them as constants: the signed integers Int8 to Int64, the
// unsigned Uint8 to Uint64, the unsigned target-width integers UnsafeSize
// and UnsafeUintptr, the IEEE 754 floats Float32 and Float64, String, a
// sequence of bytes, and Bool. UntypedInt is the type of an integer
// constant expression, and UntypedFloat of a float one, until it is given
// one where it is used; no program can name either. The float types come
// after every other basic type, so adding them moved no other basic type's
// ID; a Table's own types start after them. IsFloat, IsNumeric and
// IsUntyped describe them. Any, any, is a reference to a value of
// some other type known only at run time; it widens to and from nothing
// but itself, and InAny says which types a value of can be passed as one:
// the twelve above, the integers, String and Bool, to which a Table's
// InAny adds a view of any of the twelve. Error, error, is a
// struct no declaration gives, of a code, an Int32, and a message, a
// String: a Table reports it as a struct with those fields, fixed, in
// that order, though it never adds it, so it has no entry and no
// SetFields changes it. Allocator, Allocator, is an opaque handle on an
// allocator, copyable, with no zero value, no order and no ==. IsInteger, IsUnsigned
// and Bits describe the integer types, and Widens says which can be used
// as which without a conversion: every value of the one must be a value of
// the other.
//
// A target-width type (TargetWidth), unsafe_size_t or unsafe_uintptr_t, is
// C's size_t or uintptr_t, as wide as the target makes it, from 32 to 64
// bits. The compiler does not know which, so it has no Bits; PortableBits
// is the width every target gives it, 32, which bounds its constants at
// PortableMax, and MaxBits the widest any target does, 64, which bounds
// what it can hold anywhere. Widens holds only for a pair that widens at
// every width in that range: uint8, uint16 and uint32 into either, and
// either into uint64, but neither into the other.
//
// Adding a basic type moves the first ID a Table hands out, so the IDs of
// arrays and structs, which the C backend names its types after, all
// move with it. The IDs are the compiler's own: the type ids C sees in an
// any are a separate table, fixed for good, in the runtime's tack.h.
//
// Composite types get their IDs from the Table of the compilation that uses
// them. An array type is added the first time it is seen, and found by its
// element type and length after that, so the same array type written twice
// gets one ID. A view type, []T, a read-only view of elements of type T,
// is added the first time it is seen too, and found by its element type
// after that; it has no length, and an array type is never a view type,
// so []int64 and [3]int64 are two types. A list type, list[T], an owned,
// growable list of elements of type T, is added and found by its element
// type in the same way, and a map type, map[K]V, by its key and value
// types. A function type, func(A, mut B) R, the type of a value naming
// a function, is added the first time it is seen and found by its
// signature after that, its parameters' types, each with whether it is
// mut or own (FuncParam), and its result types, so two functions of one
// signature have one type; FuncParams and FuncResults give them. It is
// copyable, owns nothing, has no zero value, and is a pointer in size.
// A struct type is added once per declaration, so two structs
// with the same fields are two types; it is named by its declaration and
// given its fields, a name and a type each, in order, after it is added,
// which lets a field name a struct declared after its own. A union type
// is added once per declaration too, named by it, and given its cases
// after it is added, each a name and, when it has one, its payload's type
// (Case), so a payload may name a type declared after it. The Table does
// not check that a struct or a union does not contain itself; the type
// checker does.
//
// Owned says which types own memory they must free: a list, and an
// array, a struct or a union holding one at any depth, a union through
// any of its cases' payloads. A value of such a type moves rather than
// copies. HasZero says which types have a zero value: every type but a
// list, a map, an Allocator, a union, none of whose cases is one a
// variable could start as, a function type, no function being the one a
// value starts as, and an array or a struct holding any of them.
// HoldsUnion says which types are or hold a union.
// A type parameter, one of a generic function's, is a type of its own,
// added once for each declared, named by its name, which is no array,
// struct, view or list; it may stand for a struct holding a list, so it
// is owned and has no zero value. While code is generated for one instance
// of a generic function, Bind binds each of its type parameters to its
// type argument, and every question the Table answers about a bound type
// parameter, Owned, Fields, Name and the rest, is answered about its type
// argument, until Unbind; Bound gives the type a type parameter stands
// for. A type made of a type parameter, list[T], []T, map[K]T or a
// function type, stands then for the type made the same way of what its
// parts stand for, which Bound adds to the Table if it has not yet, so an
// instance's list[T] is its caller's list[int64]. HasTypeParam says which
// types are or are made of a type parameter nothing binds. The checker
// never binds one.
//
// Size gives the size and alignment of a type as the C backend lays it
// out, for the 64-bit targets it builds for, arm64 and x86-64: a
// pointer, an int64 and a target-width integer are 8 bytes, aligned to 8,
// C pads every struct, and the backend's lowering counts, an array of no
// elements holding one, a struct of no fields one char, and a union a tag
// before a C union of its payloads. Its arithmetic saturates, so a size
// that does not fit a uint64 is reported as overflowing rather than
// wrapping; MaxValueSize and MaxGlobalsSize are the limits the checker
// holds values and a program's globals to. Sizes are kept once computed,
// and forgotten when a struct's fields or a union's cases are set.
//
// The type checker creates one Table per program and records it on the
// ast.Program and on every ast.File of it. Name maps an ID back to the
// type's name as a program writes it, for diagnostics: [3]int64 for an array, []int64 for a view, list[int64] for a list, the declared name for a struct or a union; Type deliberately has no String method, so naming a type
// always goes through the Table that knows it, and go vet reports a Type
// passed to %s. Lookup maps a basic type name as written in source, such as
// "int64", to its Type.
//
// It is a leaf package: the AST, the type checker, the builtins and the
// backends depend on it, and it depends on nothing.
package types
