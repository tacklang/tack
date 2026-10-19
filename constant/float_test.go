package constant_test

import (
	"math"
	"strings"
	"testing"

	"github.com/tacklang/tack/constant"
)

// TestParseFloat checks every literal form a float takes, its value as
// the nearest float64, and the literals refused: malformed ones, and ones
// too large or too precise to hold exactly.
func TestParseFloat(t *testing.T) {
	for _, tc := range []struct {
		text string
		want float64
	}{
		{"1.5", 1.5},
		{".5", 0.5},
		{"1.", 1},
		{"1e9", 1e9},
		{"1.5e-3", 1.5e-3},
		{"1E+2", 100},
		{"0.0", 0},
		{"00.5", 0.5},
		{"1_000.000_5", 1000.0005},
		{"1_0e1_0", 1e11},
		{"5e-324", 5e-324},
		{"1.7976931348623157e308", math.MaxFloat64},
		{"0e99999", 0},
		{"0.1", 0.1},
		{"2.5e0", 2.5},
	} {
		f, ok := constant.ParseFloat(tc.text)
		if !ok {
			t.Errorf("%s: not parsed", tc.text)
			continue
		}
		if got, finite := f.Float64(); !finite || got != tc.want {
			t.Errorf("%s: %v (finite %v), want %v", tc.text, got, finite, tc.want)
		}
	}
	for _, text := range []string{
		"", ".", "e5", "1e", "1e+", "1x", "1e2500000000", "1e3000", "1e-3000",
		"0x1p3", strings.Repeat("9", 3000) + ".0",
	} {
		if _, ok := constant.ParseFloat(text); ok {
			t.Errorf("%q parsed", text)
		}
	}
}

// TestFloatArithmeticIsExact checks that untyped float constants are
// computed exactly, and only rounded when given a type.
func TestFloatArithmeticIsExact(t *testing.T) {
	p := func(s string) constant.Float {
		f, ok := constant.ParseFloat(s)
		if !ok {
			t.Fatalf("%s not parsed", s)
		}
		return f
	}
	sum, _ := p("0.1").Add(p("0.2"))
	if sum.Cmp(p("0.3")) != 0 {
		t.Errorf("0.1 + 0.2 = %s, want 0.3 exactly", sum)
	}
	third, _ := constant.FloatFromInt(constant.FromInt64(1)).Quo(p("3.0"))
	back, _ := third.Mul(p("3"))
	if back.Cmp(p("1")) != 0 {
		t.Errorf("1 / 3 * 3 = %s, want 1", back)
	}
	if _, ok := p("1e39").Float32(); ok {
		t.Error("1e39 is a finite float32")
	}
	if _, ok := p("1e308").Float64(); !ok {
		t.Error("1e308 is not a finite float64")
	}
	if f, _ := p("1e-50").Float32(); f != 0 {
		t.Errorf("1e-50 as float32 is %v, want 0", f)
	}
	if neg := p("1.5").Neg(); neg.Cmp(p("0")) >= 0 || neg.String() != "-1.5" {
		t.Errorf("-1.5 is %s", neg)
	}
}

// TestFloatStaysBounded checks that repeated arithmetic whose exact
// result would grow without bound reports false once it passes MaxBits,
// rather than growing.
func TestFloatStaysBounded(t *testing.T) {
	x, _ := constant.ParseFloat("1e300")
	ok := true
	for range 100 {
		if x, ok = x.Mul(x); !ok {
			break
		}
	}
	if ok {
		t.Error("squaring 1e300 100 times stayed within MaxBits")
	}
}

// TestFloatInt checks a float constant's integer value: an integer from
// the smallest int64 to the largest uint64 has one, and a fraction, or an
// integer past that range, has none.
func TestFloatInt(t *testing.T) {
	for _, tc := range []struct {
		text, want string
		ok         bool
	}{
		{"2.0", "2", true},
		{"1e18", "1000000000000000000", true},
		{"18446744073709551615.0", "18446744073709551615", true},
		{"18446744073709551616.0", "", false},
		{"2.5", "", false},
		{"0.0", "0", true},
	} {
		f, _ := constant.ParseFloat(tc.text)
		n, ok := f.Int()
		if ok != tc.ok || ok && n.String() != tc.want {
			t.Errorf("%s: %s, %v; want %s, %v", tc.text, n, ok, tc.want, tc.ok)
		}
	}
	f, _ := constant.ParseFloat("9223372036854775808.0")
	if n, ok := f.Neg().Int(); !ok || n.Cmp(constant.Min()) != 0 {
		t.Errorf("-2^63 is %s, %v", n, ok)
	}
	if f, _ := constant.ParseFloat("1e30"); !f.IsInteger() {
		t.Error("1e30 is not an integer")
	}
}
