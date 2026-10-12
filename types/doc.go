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
// unsigned Uint8 to Uint64, String, a sequence of bytes, and Bool.
// UntypedInt is the type of an integer constant expression until it is
// given one where it is used; no program can name it. IsInteger, IsUnsigned
// and Bits describe the integer types, and Widens says which can be used as
// which without a conversion: every value of the one must be a value of the
// other. The language has no other types yet. Composite
// types will get their IDs from the Table of the compilation that uses them,
// the first time each is seen, so the same type written twice gets one ID.
//
// The type checker creates one Table per file and records it on the
// ast.File. Name maps an ID back to the type's name as a program writes it,
// for diagnostics; Type deliberately has no String method, so naming a type
// always goes through the Table that knows it, and go vet reports a Type
// passed to %s. Lookup maps a basic type name as written in source, such as
// "int64", to its Type.
//
// It is a leaf package: the AST, the type checker, the builtins and the
// backends depend on it, and it depends on nothing.
package types
