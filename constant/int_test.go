package constant_test

import (
	"math"
	"math/big"
	"testing"

	"github.com/tacklang/tack/constant"
)

// lo and hi bound the range every Int must lie in.
var (
	lo = big.NewInt(math.MinInt64)
	hi = new(big.Int).SetUint64(math.MaxUint64)
)

// fromBig returns b as an Int, and whether it is in range.
func fromBig(b *big.Int) (constant.Int, bool) {
	if b.Cmp(lo) < 0 || b.Cmp(hi) > 0 {
		return constant.Int{}, false
	}
	return constant.Parse(new(big.Int).Abs(b).String(), b.Sign() < 0)
}

// toBig returns x as a big.Int, through its decimal form.
func toBig(t *testing.T, x constant.Int) *big.Int {
	t.Helper()
	b, ok := new(big.Int).SetString(x.String(), 10)
	if !ok {
		t.Fatalf("%q is not a decimal number", x.String())
	}
	return b
}

// edges are the values around every boundary that matters.
var edges = []*big.Int{
	big.NewInt(0),
	big.NewInt(1),
	big.NewInt(-1),
	big.NewInt(2),
	big.NewInt(-2),
	big.NewInt(
		127,
	),
	big.NewInt(128),
	big.NewInt(-128),
	big.NewInt(-129),
	big.NewInt(255),
	big.NewInt(256),
	big.NewInt(math.MaxInt32),
	big.NewInt(math.MinInt32),
	big.NewInt(math.MaxUint32),
	big.NewInt(math.MaxInt64),
	lo,
	hi,
	new(big.Int).Add(big.NewInt(math.MaxInt64), big.NewInt(1)),
	new(big.Int).Sub(hi, big.NewInt(1)),
	new(big.Int).Add(lo, big.NewInt(1)),
}

// check compares one operation on a and b with the same on big.Int: the
// result must match exactly when it is in range, and be reported out of
// range otherwise.
func check(
	t *testing.T,
	name string,
	a, b *big.Int,
	op func(x, y constant.Int) (constant.Int, bool),
	want *big.Int,
) {
	t.Helper()
	x, okx := fromBig(a)
	y, oky := fromBig(b)
	if !okx || !oky {
		return
	}
	got, ok := op(x, y)
	_, inRange := fromBig(want)
	if ok != inRange {
		t.Errorf("%s %s %s: in range = %v, want %v", a, name, b, ok, inRange)
		return
	}
	if ok && toBig(t, got).Cmp(want) != 0 {
		t.Errorf("%s %s %s = %s, want %s", a, name, b, got, want)
	}
}

// TestArithmeticMatchesBigInt checks every operation on every pair of edge
// values against math/big, which has no range to fall out of.
func TestArithmeticMatchesBigInt(t *testing.T) {
	for _, a := range edges {
		for _, b := range edges {
			arith(t, a, b)
		}
	}
}

// arith checks every binary operation on a and b.
func arith(t *testing.T, a, b *big.Int) {
	t.Helper()
	check(t, "+", a, b, constant.Int.Add, new(big.Int).Add(a, b))
	check(t, "-", a, b, constant.Int.Sub, new(big.Int).Sub(a, b))
	check(t, "*", a, b, constant.Int.Mul, new(big.Int).Mul(a, b))
	if b.Sign() != 0 {
		check(t, "/", a, b, constant.Int.Quo, new(big.Int).Quo(a, b))
		check(t, "%", a, b, constant.Int.Rem, new(big.Int).Rem(a, b))
	}

	x, okx := fromBig(a)
	y, oky := fromBig(b)
	if okx && oky {
		if got, want := x.Cmp(y), a.Cmp(b); got != want {
			t.Errorf("Cmp(%s, %s) = %d, want %d", a, b, got, want)
		}
	}
}

// FuzzArithmetic checks the operations on arbitrary 64-bit values, read as
// signed or unsigned, against math/big.
func FuzzArithmetic(f *testing.F) {
	f.Add(uint64(math.MaxUint64), true, uint64(1), false)
	f.Add(uint64(1)<<63, true, uint64(1)<<63, false)
	f.Fuzz(func(t *testing.T, a uint64, aSigned bool, b uint64, bSigned bool) {
		toBigInt := func(v uint64, signed bool) *big.Int {
			if signed {
				return big.NewInt(int64(v))
			}
			return new(big.Int).SetUint64(v)
		}
		arith(t, toBigInt(a, aSigned), toBigInt(b, bSigned))
	})
}

// TestNeg checks negation at the edges of the range.
func TestNeg(t *testing.T) {
	for _, a := range edges {
		check(
			t,
			"neg",
			a,
			big.NewInt(0),
			func(x, _ constant.Int) (constant.Int, bool) { return x.Neg() },
			new(big.Int).Neg(a),
		)
	}
}

// TestFits checks the range of every integer type against its limits.
func TestFits(t *testing.T) {
	types := []struct {
		bits   int
		signed bool
	}{{8, true}, {16, true}, {32, true}, {64, true}, {8, false}, {16, false}, {32, false}, {64, false}}

	for _, typ := range types {
		var lowest, highest *big.Int
		if typ.signed {
			lowest = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), uint(typ.bits-1)))
			highest = new(
				big.Int,
			).Sub(new(big.Int).Lsh(big.NewInt(1), uint(typ.bits-1)), big.NewInt(1))
		} else {
			lowest = big.NewInt(0)
			highest = new(
				big.Int,
			).Sub(new(big.Int).Lsh(big.NewInt(1), uint(typ.bits)), big.NewInt(1))
		}
		for _, v := range edges {
			x, ok := fromBig(v)
			if !ok {
				continue
			}
			want := v.Cmp(lowest) >= 0 && v.Cmp(highest) <= 0
			if got := x.Fits(typ.bits, typ.signed); got != want {
				t.Errorf(
					"%s fits %d-bit signed=%v: got %v, want %v",
					v,
					typ.bits,
					typ.signed,
					got,
					want,
				)
			}
		}
	}
}

// TestParse checks decoding at the edges, including the two values one past
// each end of the range.
func TestParse(t *testing.T) {
	tests := []struct {
		digits string
		want   string
		neg    bool
		ok     bool
	}{
		{"0", "0", false, true},
		{"0", "0", true, true},
		{"18446744073709551615", "18446744073709551615", false, true},
		{"18446744073709551616", "", false, false},
		{"9223372036854775808", "-9223372036854775808", true, true},
		{"9223372036854775809", "", true, false},
		{"007", "7", false, true},
		{"", "", false, false},
		{"1a", "", false, false},
	}
	for _, tt := range tests {
		got, ok := constant.Parse(tt.digits, tt.neg)
		if ok != tt.ok || (ok && got.String() != tt.want) {
			t.Errorf(
				"Parse(%q, %v) = %v, %v; want %q, %v",
				tt.digits,
				tt.neg,
				got,
				ok,
				tt.want,
				tt.ok,
			)
		}
	}
}

// Conversions to the machine types keep the low 64 bits, as C does.
func TestMachineValues(t *testing.T) {
	if got := constant.FromInt64(math.MinInt64).Int64(); got != math.MinInt64 {
		t.Errorf("Int64 of the smallest int64 = %d", got)
	}
	if got := constant.Max().Uint64(); got != math.MaxUint64 {
		t.Errorf("Uint64 of Max = %d", got)
	}
	if got := constant.FromInt64(-1).Uint64(); got != math.MaxUint64 {
		t.Errorf("Uint64 of -1 = %d, want the largest uint64", got)
	}
	if got := constant.Min().String(); got != "-9223372036854775808" {
		t.Errorf("Min = %s", got)
	}
}
