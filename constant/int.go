package constant

import (
	"math"
	"math/bits"
	"strconv"
)

// Int is an integer constant between Min and Max, the range every integer
// literal and every intermediate result of constant arithmetic must lie in.
// It is a sign and a magnitude, so the whole range fits in one uint64 and a
// bool. The zero value is 0, and zero is never negative.
type Int struct {
	mag uint64
	neg bool
}

// minMag is the magnitude of the smallest Int, -2^63.
const minMag = uint64(1) << 63

// Min returns the smallest Int, -2^63, the smallest int64.
func Min() Int { return Int{mag: minMag, neg: true} }

// Max returns the largest Int, 2^64 - 1, the largest uint64.
func Max() Int { return Int{mag: math.MaxUint64} }

// FromInt64 returns v as an Int.
func FromInt64(v int64) Int {
	if v < 0 {
		// Negating through uint64 handles the smallest int64, whose
		// magnitude does not fit in an int64.
		mag := uint64(0) - uint64(v) //nolint:gosec // G115: two's complement on purpose
		return Int{mag: mag, neg: true}
	}
	return Int{mag: uint64(v)}
}

// FromUint64 returns v as an Int.
func FromUint64(v uint64) Int { return Int{mag: v} }

// Parse decodes decimal digits, with a minus sign before them when negative
// is true, and reports whether the value is in range.
func Parse(digits string, negative bool) (Int, bool) {
	mag, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return Int{}, false
	}
	return build(mag, negative)
}

// build builds the Int with magnitude mag and the given sign, normalizing a
// negative zero, and reports whether it is in range.
func build(mag uint64, negative bool) (Int, bool) {
	if mag == 0 {
		return Int{}, true
	}
	if negative && mag > minMag {
		return Int{}, false
	}
	return Int{mag: mag, neg: negative}, true
}

// IsZero reports whether x is 0.
func (x Int) IsZero() bool { return x.mag == 0 }

// IsNegative reports whether x is less than 0.
func (x Int) IsNegative() bool { return x.neg }

// Int64 returns x as an int64. x must fit in one.
func (x Int) Int64() int64 {
	if x.neg {
		return int64(uint64(0) - x.mag) //nolint:gosec // G115: two's complement on purpose
	}
	return int64(x.mag) //nolint:gosec // G115: x fits in an int64, the caller's guarantee
}

// Uint64 returns x as a uint64, keeping its low 64 bits for a negative x,
// as two's complement does.
func (x Int) Uint64() uint64 {
	if x.neg {
		return uint64(0) - x.mag
	}
	return x.mag
}

// String returns x in decimal.
func (x Int) String() string {
	s := strconv.FormatUint(x.mag, 10)
	if x.neg {
		return "-" + s
	}
	return s
}

// Fits reports whether x is a value of an integer type with the given
// number of bits and signedness.
func (x Int) Fits(bitSize int, signed bool) bool {
	switch {
	case signed && x.neg:
		return x.mag <= uint64(1)<<(bitSize-1)
	case signed:
		return x.mag <= uint64(1)<<(bitSize-1)-1
	case x.neg:
		return false
	default:
		return bitSize == 64 || x.mag <= uint64(1)<<bitSize-1
	}
}

// Cmp returns -1, 0 or 1 as x is less than, equal to, or greater than y.
func (x Int) Cmp(y Int) int {
	switch {
	case x.neg != y.neg:
		if x.neg {
			return -1
		}
		return 1
	case x.mag == y.mag:
		return 0
	case (x.mag < y.mag) != x.neg:
		return -1
	default:
		return 1
	}
}

// Neg returns -x, and reports whether it is in range.
func (x Int) Neg() (Int, bool) { return build(x.mag, !x.neg) }

// Add returns x + y, and reports whether it is in range.
func (x Int) Add(y Int) (Int, bool) {
	if x.neg == y.neg {
		sum, carry := bits.Add64(x.mag, y.mag, 0)
		if carry != 0 {
			return Int{}, false
		}
		return build(sum, x.neg)
	}
	// Opposite signs: the result takes the sign of the larger magnitude.
	if x.mag >= y.mag {
		return build(x.mag-y.mag, x.neg)
	}
	return build(y.mag-x.mag, y.neg)
}

// Sub returns x - y, and reports whether it is in range.
func (x Int) Sub(y Int) (Int, bool) {
	return x.Add(Int{mag: y.mag, neg: !y.neg && y.mag != 0})
}

// Mul returns x * y, and reports whether it is in range.
func (x Int) Mul(y Int) (Int, bool) {
	hi, lo := bits.Mul64(x.mag, y.mag)
	if hi != 0 {
		return Int{}, false
	}
	return build(lo, x.neg != y.neg)
}

// Quo returns x / y truncated toward zero, as Go and C divide, and reports
// whether it is in range. y must not be zero.
func (x Int) Quo(y Int) (Int, bool) { return build(x.mag/y.mag, x.neg != y.neg) }

// Rem returns the remainder of x / y, which takes the sign of x, and reports
// whether it is in range, which it always is. y must not be zero.
func (x Int) Rem(y Int) (Int, bool) { return build(x.mag%y.mag, x.neg) }
