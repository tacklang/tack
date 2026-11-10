package progen

import (
	"math"
	"strconv"
)

// IntType is one of Tack's fixed-width integer types: its name, its width
// in bits and whether it is signed.
type IntType struct {
	Name   string
	Bits   int
	Signed bool
}

// The integer types the generator writes, every fixed-width one Tack has.
var (
	Int8   = &IntType{"int8", 8, true}
	Int16  = &IntType{"int16", 16, true}
	Int32  = &IntType{"int32", 32, true}
	Int64  = &IntType{"int64", 64, true}
	Uint8  = &IntType{"uint8", 8, false}
	Uint16 = &IntType{"uint16", 16, false}
	Uint32 = &IntType{"uint32", 32, false}
	Uint64 = &IntType{"uint64", 64, false}
)

// intTypes lists them, int64 first, the type most values have.
var intTypes = []*IntType{Int64, Int8, Int16, Int32, Uint8, Uint16, Uint32, Uint64}

// Widens reports whether every value of from is a value of to, so Tack
// takes a from where a to is wanted without a conversion: the same type,
// a signed one to a wider signed one, or an unsigned one to a wider type.
func Widens(from, to *IntType) bool {
	switch {
	case from == to:
		return true
	case from.Signed && !to.Signed:
		return false
	default:
		return from.Bits < to.Bits
	}
}

// norm returns bits, a value's two's complement bits, cut to t's width
// and extended back to 64 bits as t's values are held: sign-extended for
// a signed t, zero-extended for an unsigned one.
func norm(t *IntType, bits uint64) uint64 {
	if t.Bits == 64 {
		return bits
	}
	shift := uint(64 - t.Bits)
	if t.Signed {
		return uint64(int64(bits<<shift) >> shift) //nolint:gosec // two's complement on purpose
	}
	return bits << shift >> shift
}

// fits reports whether the value of t held in bits is a value of to.
func fits(t *IntType, bits uint64, to *IntType) bool {
	if t.Signed && int64(bits) < 0 { //nolint:gosec // two's complement on purpose
		return to.Signed && norm(to, bits) == bits
	}
	if !t.Signed && bits > math.MaxInt64 {
		return to == Uint64
	}
	return norm(to, bits) == bits
}

// arith applies the arithmetic or bitwise operator op to a and b, values
// of t, as Tack does at run time: + - and * and their wrapping forms +%
// -% and *% wrapping at t's width, which a plain one, proven not to
// overflow, never does, the smallest signed value % -1 being 0. Division
// by zero, and the smallest signed value divided by -1, are never asked
// for; the generator proves or checks every division (see overflowsDiv).
func arith(t *IntType, op string, a, b uint64) uint64 {
	switch op {
	case "+", "+%":
		return norm(t, a+b)
	case "-", "-%":
		return norm(t, a-b)
	case "*", "*%":
		return norm(t, a*b)
	case "&":
		return a & b
	case "|":
		return a | b
	case "^":
		return norm(t, a^b)
	case "&^":
		return a &^ b
	case "/", "%":
		return divide(t, op, a, b)
	}
	panic("progen: unknown operator " + op)
}

// overflowsDiv reports whether a / b, values of t, is t's smallest value
// divided by -1, which overflows a signed t.
func overflowsDiv(t *IntType, a, b uint64) bool {
	least := int64(-1) << (t.Bits - 1)
	return t.Signed && int64(b) == -1 && int64(a) == least //nolint:gosec // two's complement on purpose
}

// divide is arith for / and %.
func divide(t *IntType, op string, a, b uint64) uint64 {
	if !t.Signed {
		if op == "/" {
			return a / b
		}
		return a % b
	}
	x, y := int64(a), int64(b) //nolint:gosec // two's complement on purpose
	if y == -1 {
		if op == "/" {
			return norm(t, uint64(-x)) //nolint:gosec // wraps on purpose
		}
		return 0
	}
	if op == "/" {
		return norm(t, uint64(x/y)) //nolint:gosec // two's complement on purpose
	}
	return norm(t, uint64(x%y)) //nolint:gosec // two's complement on purpose
}

// shift shifts a, a value of t, left or right, as op says, by n bits: a
// count at or past t's width shifts every bit out, giving 0, or -1 for a
// negative value shifted right, which is arithmetic for a signed t.
func shift(t *IntType, op string, a, n uint64) uint64 {
	if op == "<<" {
		if n >= uint64(t.Bits) { //nolint:gosec // widths are small
			return 0
		}
		return norm(t, a<<n)
	}
	if t.Signed {
		if n >= uint64(t.Bits) { //nolint:gosec // widths are small
			n = 63
		}
		return norm(t, uint64(int64(a)>>n)) //nolint:gosec // two's complement on purpose
	}
	if n >= uint64(t.Bits) { //nolint:gosec // widths are small
		return 0
	}
	return a >> n
}

// compare compares a, a value of ta, with b, a value of tb, by their
// mathematical values, as Tack compares any two integers, returning -1, 0
// or 1.
func compare(ta *IntType, a uint64, tb *IntType, b uint64) int {
	an, bn := negative(ta, a), negative(tb, b)
	switch {
	case an != bn:
		if an {
			return -1
		}
		return 1
	case an:
		return cmpSigned(int64(a), int64(b)) //nolint:gosec // two's complement on purpose
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// negative reports whether the value of t held in bits is below zero.
func negative(t *IntType, bits uint64) bool {
	return t.Signed && int64(bits) < 0 //nolint:gosec // two's complement on purpose
}

// cmpSigned compares two int64s.
func cmpSigned(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// literal returns the Tack text of the value of t held in bits, in
// decimal, with a minus sign for a negative one.
func literal(t *IntType, bits uint64) string {
	if t.Signed {
		return strconv.FormatInt(int64(bits), 10) //nolint:gosec // two's complement on purpose
	}
	return strconv.FormatUint(bits, 10)
}

// asInt64 returns the value of t held in bits as an int64, which a caller
// knows it fits.
func asInt64(bits uint64) int64 {
	return int64(bits) //nolint:gosec // two's complement on purpose
}
