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
// and UnsafeUintptr, String, a sequence of bytes, and Bool. UntypedInt is
// the type of an integer constant expression until it is given one where it
// is used; no program can name it. Any, any, is a reference to a value of
// some other type known only at run time; it widens to and from nothing
// but itself, and InAny says which types a value of can be passed as one:
// the twelve above, the integers, String and Bool. IsInteger, IsUnsigned
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
// any are a separate table, fixed for good, in std's tack.h.
//
// Composite types get their IDs from the Table of the compilation that uses
// them. An array type is added the first time it is seen, and found by its
// element type and length after that, so the same array type written twice
// gets one ID. A view type, []T, a read-only view of elements of type T,
// is added the first time it is seen too, and found by its element type
// after that; it has no length, and an array type is never a view type,
// so []int64 and [3]int64 are two types. A struct type is added once per declaration, so two structs
// with the same fields are two types; it is named by its declaration and
// given its fields, a name and a type each, in order, after it is added,
// which lets a field name a struct declared after its own. The Table does
// not check that a struct does not contain itself; the type checker does.
//
// The type checker creates one Table per program and records it on the
// ast.Program and on every ast.File of it. Name maps an ID back to the
// type's name as a program writes it, for diagnostics: [3]int64 for an array, []int64 for a view, the declared name for a struct; Type deliberately has no String method, so naming a type
// always goes through the Table that knows it, and go vet reports a Type
// passed to %s. Lookup maps a basic type name as written in source, such as
// "int64", to its Type.
//
// It is a leaf package: the AST, the type checker, the builtins and the
// backends depend on it, and it depends on nothing.
package types
