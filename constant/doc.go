// Package constant holds the exact values of integer constant expressions.
//
// Tack computes a constant expression, such as 1 + 2 * 3 written as literals
// and operators, exactly, before giving it a type. Every literal and every
// intermediate result must lie between -2^63 and 2^64-1: the smallest int64
// through the largest uint64, which covers every value of every integer
// type. A result outside that range is an error rather than a wider number,
// so the compiler needs no arbitrary precision arithmetic, which matters for
// a compiler that will one day be written in Tack itself.
//
// An Int is a sign and a 64-bit magnitude. Each operation returns its result
// and whether it is in range; the type checker turns false into a
// diagnostic. Division truncates toward zero and a remainder takes the sign
// of the dividend, as in Go and C. Fits reports whether a value belongs to a
// particular integer type, which is how a constant is checked against the
// type it is given.
//
// It is a leaf package: the parser decodes literals into it, the type
// checker computes with it, and the C backend prints it.
package constant
