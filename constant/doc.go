// Package constant holds the exact values of integer and float constant
// expressions.
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
// of the dividend, as in Go and C. The bitwise operators work on a value's
// infinitely wide two's complement form, as Go's do on its untyped
// constants, which 65 bits hold for every value in range (see bits.go); a
// left shift is exact, a multiplication by a power of two, and a right
// shift rounds toward negative infinity. Fits reports whether a value belongs to a
// particular integer type, which is how a constant is checked against the
// type it is given.
//
// A Float is an untyped float constant's exact value: a rational number,
// held by math/big, rounded only when the constant is given a float type
// (Float64, Float32), to the nearest value, ties to even, a value too
// large becoming an infinity, which the type checker reports as an
// overflow. Unlike an Int, it needs arbitrary precision: 0.1 is 1/10, and
// 1e-300 a denominator of a thousand bits, so the bound is on size instead,
// MaxBits bits each of numerator and denominator, which every operation
// checks, and ParseFloat refuses a literal past it before computing it, so
// no constant takes unbounded memory or time. That bound is far past what
// any float64 needs. A self-hosted compiler will need a big rational of
// its own to do the same.
//
// It is a leaf package: the parser decodes literals into it, the type
// checker computes with it, and the C backend prints it.
package constant
