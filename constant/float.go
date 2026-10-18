package constant

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Float is the exact value of an untyped float constant: a rational
// number, held by math/big, never rounded until the constant is given a
// float type (see Float64, Float32). Every result of an operation must
// stay within MaxBits bits of numerator and of denominator: past that, the
// operation reports false, as an Int's does out of its range, so a
// constant expression can never take more than a bounded amount of memory
// or time. The zero value is 0. A Float is never changed once made: each
// operation returns a new one.
type Float struct {
	r *big.Rat
}

// MaxBits is how many bits the numerator and the denominator of a Float
// may each take. It is far more than the largest and the smallest float64
// need, about 1024 and 1075 bits, so any constant that rounds to a value
// of a float type, and any reasonable expression on such constants, is
// held exactly.
const MaxBits = 8192

// maxExponent is the largest power of ten a float literal may scale its
// digits by, in either direction, before ParseFloat refuses it rather
// than compute it: 10^2500 already takes more than MaxBits bits.
const maxExponent = 2500

// rat returns x's value as a big.Rat, which the caller must not change.
func (x Float) rat() *big.Rat {
	if x.r == nil {
		return new(big.Rat)
	}
	return x.r
}

// fromRat returns r as a Float, and whether it is within MaxBits.
func fromRat(r *big.Rat) (Float, bool) {
	if r.Num().BitLen() > MaxBits || r.Denom().BitLen() > MaxBits {
		return Float{}, false
	}
	return Float{r: r}, true
}

// FloatFromInt returns the Float equal to the integer x.
func FloatFromInt(x Int) Float {
	n := new(big.Int).SetUint64(x.mag)
	if x.neg {
		n.Neg(n)
	}
	return Float{r: new(big.Rat).SetInt(n)}
}

// ParseFloat decodes a decimal float literal as the lexer accepts it:
// digits with _ between them, a fraction point, an exponent, e or E with
// an optional sign and digits, or both, as in 1.5, .5, 1., 1e9 and
// 1.5e-3. It reports false for text that is not such a literal, and for
// one too large or too precise to hold exactly within MaxBits.
func ParseFloat(text string) (Float, bool) {
	text = strings.ReplaceAll(text, "_", "")
	mant, exp := text, int64(0)
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		e, err := strconv.ParseInt(text[i+1:], 10, 32)
		if err != nil {
			return Float{}, false
		}
		mant, exp = text[:i], e
	}
	whole, frac, _ := strings.Cut(mant, ".")
	digits := whole + frac
	if digits == "" || strings.Trim(digits, "0123456789") != "" {
		return Float{}, false
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return Float{}, true
	}
	// The value is digits * 10^scale.
	scale := exp - int64(len(frac))
	if scale > maxExponent || scale < -maxExponent || len(digits) > maxExponent {
		return Float{}, false
	}
	n, _ := new(big.Int).SetString(digits, 10)
	p := new(big.Int).Exp(big.NewInt(10), big.NewInt(abs(scale)), nil)
	r := new(big.Rat)
	if scale >= 0 {
		r.SetInt(n.Mul(n, p))
	} else {
		r.SetFrac(n, p)
	}
	return fromRat(r)
}

// abs returns the magnitude of v.
func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// IsZero reports whether x is 0.
func (x Float) IsZero() bool { return x.rat().Sign() == 0 }

// Cmp compares x and y, returning -1, 0 or 1 as x is less than, equal to,
// or greater than y.
func (x Float) Cmp(y Float) int { return x.rat().Cmp(y.rat()) }

// Neg returns -x.
func (x Float) Neg() Float { return Float{r: new(big.Rat).Neg(x.rat())} }

// Add returns x + y, and whether it is within MaxBits.
func (x Float) Add(y Float) (Float, bool) { return fromRat(new(big.Rat).Add(x.rat(), y.rat())) }

// Sub returns x - y, and whether it is within MaxBits.
func (x Float) Sub(y Float) (Float, bool) { return fromRat(new(big.Rat).Sub(x.rat(), y.rat())) }

// Mul returns x * y, and whether it is within MaxBits.
func (x Float) Mul(y Float) (Float, bool) { return fromRat(new(big.Rat).Mul(x.rat(), y.rat())) }

// Quo returns x / y, exactly, and whether it is within MaxBits. y must not
// be zero.
func (x Float) Quo(y Float) (Float, bool) { return fromRat(new(big.Rat).Quo(x.rat(), y.rat())) }

// Float64 returns the float64 nearest x, ties to even, and whether it is
// finite: a value too large for float64 rounds to an infinity, which no
// constant may become. A value too small rounds to zero, or a subnormal,
// as at run time.
func (x Float) Float64() (float64, bool) {
	f, _ := x.rat().Float64()
	return f, !math.IsInf(f, 0)
}

// Float32 returns the float32 nearest x, ties to even, and whether it is
// finite, as Float64 does.
func (x Float) Float32() (float32, bool) {
	f, _ := x.rat().Float32()
	return f, !math.IsInf(float64(f), 0)
}

// String returns x in decimal, rounded to 10 significant digits, with an
// exponent when it is very large or very small, as in 1.5, 0.1 or
// 1e+39, for a diagnostic.
func (x Float) String() string {
	return new(big.Float).SetPrec(64).SetRat(x.rat()).Text('g', 10)
}
