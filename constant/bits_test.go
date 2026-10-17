package constant_test

import (
	"math/big"
	"math/rand/v2"
	"testing"

	"github.com/tacklang/tack/constant"
)

// bitValues returns edges and n random values in range, from a fixed seed.
func bitValues(n int) []*big.Int {
	r := rand.New(rand.NewPCG(1, 2))
	values := append([]*big.Int{}, edges...)
	for range n {
		v := new(big.Int).SetUint64(r.Uint64())
		if r.IntN(2) == 0 {
			v.Rsh(v, 1).Neg(v)
		}
		values = append(values, v)
	}
	return values
}

// TestBitwiseMatchesBig checks & | ^ &^ on every pair of edges and random
// values against math/big's, which work on the same infinitely wide two's
// complement form: a result must match exactly when in range, and be
// reported out of range otherwise, as -2^64 is for -(2^63) & -(2^64 - 1).
func TestBitwiseMatchesBig(t *testing.T) {
	values := bitValues(40)
	for _, a := range values {
		for _, b := range values {
			check(t, "&", a, b, constant.Int.And, new(big.Int).And(a, b))
			check(t, "|", a, b, constant.Int.Or, new(big.Int).Or(a, b))
			check(t, "^", a, b, constant.Int.Xor, new(big.Int).Xor(a, b))
			check(t, "&^", a, b, constant.Int.AndNot, new(big.Int).AndNot(a, b))
		}
	}
}

// TestComplementMatchesBig checks ^x, -x - 1, against math/big's Not.
func TestComplementMatchesBig(t *testing.T) {
	for _, a := range bitValues(40) {
		check(t, "^", a, a, func(x, _ constant.Int) (constant.Int, bool) { return x.Not() },
			new(big.Int).Not(a))
	}
}

// TestShiftsMatchBig checks << and >> of every edge and random value by
// counts from 0 past 64 and up to the largest uint64 against math/big's,
// whose >> rounds toward negative infinity, as an arithmetic shift does.
func TestShiftsMatchBig(t *testing.T) {
	counts := []uint64{0, 1, 2, 7, 8, 31, 32, 62, 63, 64, 65, 127, 128, 255, 1 << 32, 1<<64 - 1}
	for _, a := range bitValues(40) {
		for _, n := range counts {
			x, ok := fromBig(a)
			if !ok {
				continue
			}
			shift := uint(min(n, 1000))
			want := new(big.Int).Lsh(a, shift)
			got, inRange := x.Shl(n)
			compareShift(t, a, "<<", n, got, inRange, want)
			want = new(big.Int).Rsh(a, shift)
			got, inRange = x.Shr(n)
			compareShift(t, a, ">>", n, got, inRange, want)
		}
	}
}

// compareShift reports a shift of a by n whose result, got, in range
// when inRange, differs from want, or whose range differs from want's.
func compareShift(
	t *testing.T,
	a *big.Int,
	op string,
	n uint64,
	got constant.Int,
	inRange bool,
	want *big.Int,
) {
	t.Helper()
	_, wantIn := fromBig(want)
	if inRange != wantIn {
		t.Errorf("%s %s %d: in range = %v, want %v", a, op, n, inRange, wantIn)
		return
	}
	if inRange && got.String() != want.String() {
		t.Errorf("%s %s %d = %s, want %s", a, op, n, got, want)
	}
}
