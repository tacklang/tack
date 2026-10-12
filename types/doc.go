// Package types holds Tack's type representation.
//
// A Type is an ID: a small integer that a Table gives meaning to. Every
// distinct type has exactly one ID within a compilation, so == on two Types
// is type identity, with no structural comparison to remember. The zero
// value, Invalid, marks an expression that has not been checked or whose type
// could not be determined, so an AST field the type checker has not filled in
// holds Invalid rather than nil.
//
// The basic types, Int, a signed 64-bit integer; String, a sequence of bytes;
// and Bool, have fixed IDs that mean the same thing in every Table, so code
// can name them as constants. The language has no other types yet. Composite
// types will get their IDs from the Table of the compilation that uses them,
// the first time each is seen, so the same type written twice gets one ID.
//
// The type checker creates one Table per file and records it on the
// ast.File. Name maps an ID back to the type's name as a program writes it,
// for diagnostics; Type deliberately has no String method, so naming a type
// always goes through the Table that knows it, and go vet reports a Type
// passed to %s. Lookup maps a basic type name as written in source, such as
// "int", to its Type.
//
// It is a leaf package: the AST, the type checker, the builtins and the
// backends depend on it, and it depends on nothing.
package types
