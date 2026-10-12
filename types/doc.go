// Package types holds Tack's type representation.
//
// The language has no composite types yet, so a type is a Kind: Int, a signed
// 64-bit integer; String, a sequence of bytes; or Bool, which only comparisons
// produce today. The zero value, Invalid, marks an expression that has not
// been checked or whose type could not be determined. No program can write it
// as a type name, so Lookup never returns it.
//
// Lookup maps a type name as written in source, such as "int", to its Kind,
// and Kind.String maps back. The type checker uses Lookup to resolve declared
// parameter and result types, and diagnostics use String to name a type.
//
// It is a leaf package: the AST, the type checker, the builtins and the
// backends depend on it, and it depends on nothing.
package types
