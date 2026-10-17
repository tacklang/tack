package constant

// The bitwise operators work on an Int as Go's work on its untyped
// constants: on the infinitely wide two's complement form of the value, in
// which a negative value has every bit above its magnitude set. Every Int
// lies in [-2^63, 2^64), so 65 bits hold that form exactly: a sign, set for
// a negative value and copied into every bit above, and the 64 bits below
// it. An operator works on the two parts on its own, and the result, also
// 65 bits, is in range unless it is negative with a magnitude past 2^63.

// twos returns x in its 65-bit two's complement form: its 64 low bits, and
// whether the sign bit, and so every bit above them, is set.
func (x Int) twos() (lo uint64, neg bool) {
	if x.neg {
		return 0 - x.mag, true
	}
	return x.mag, false
}

// fromTwos returns the Int whose 65-bit two's complement form is lo and
// neg, and reports whether it is in range. A negative value whose low bits
// are all zero is -2^64, which is not.
func fromTwos(lo uint64, neg bool) (Int, bool) {
	if !neg {
		return Int{mag: lo}, true
	}
	if lo == 0 {
		return Int{}, false
	}
	return build(0-lo, true)
}

// And returns x & y, and reports whether it is in range.
func (x Int) And(y Int) (Int, bool) {
	a, an := x.twos()
	b, bn := y.twos()
	return fromTwos(a&b, an && bn)
}

// Or returns x | y, and reports whether it is in range.
func (x Int) Or(y Int) (Int, bool) {
	a, an := x.twos()
	b, bn := y.twos()
	return fromTwos(a|b, an || bn)
}

// Xor returns x ^ y, and reports whether it is in range.
func (x Int) Xor(y Int) (Int, bool) {
	a, an := x.twos()
	b, bn := y.twos()
	return fromTwos(a^b, an != bn)
}

// AndNot returns x &^ y, x with every bit set in y cleared, and reports
// whether it is in range.
func (x Int) AndNot(y Int) (Int, bool) {
	a, an := x.twos()
	b, bn := y.twos()
	return fromTwos(a&^b, an && !bn)
}

// Not returns ^x, every bit of x flipped, which is -x - 1, and reports
// whether it is in range.
func (x Int) Not() (Int, bool) {
	a, an := x.twos()
	return fromTwos(^a, !an)
}

// Shl returns x << n, x times 2^n, exactly, and reports whether it is in
// range. Nothing is lost off the top, as nothing is in any constant
// arithmetic.
func (x Int) Shl(n uint64) (Int, bool) {
	switch {
	case x.mag == 0:
		return Int{}, true
	case n >= 64 || x.mag>>(64-n) != 0:
		return Int{}, false
	}
	return build(x.mag<<n, x.neg)
}

// Shr returns x >> n, x divided by 2^n rounded down, toward negative
// infinity, as an arithmetic shift does. It is always in range: a
// non-negative x shifted far enough is 0, and a negative one -1.
func (x Int) Shr(n uint64) (Int, bool) {
	if !x.neg {
		if n >= 64 {
			return Int{}, true
		}
		return Int{mag: x.mag >> n}, true
	}
	if n >= 64 {
		return Int{mag: 1, neg: true}, true
	}
	q := x.mag >> n
	if x.mag&(uint64(1)<<n-1) != 0 {
		q++
	}
	return build(q, true)
}
