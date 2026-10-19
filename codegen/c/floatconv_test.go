package c_test

import (
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// floatSanitizers are the flags every build of a float test takes: the
// address and undefined behavior sanitizers, and float-cast-overflow
// named on its own, since not every clang puts converting an
// out-of-range float to an integer in -fsanitize=undefined, every finding
// fatal.
var floatSanitizers = []string{
	"-fsanitize=address,undefined,float-cast-overflow", "-fno-sanitize-recover=all",
}

// TestFloatExamplesAreSanitizerClean builds examples/floats and
// examples/float-conversions with clang's warnings as errors and
// floatSanitizers, at -O0 and -O2, and checks each writes exactly its
// output, every check ok: no float operation, and no conversion between
// floats and integers at any edge, is one C leaves undefined.
func TestFloatExamplesAreSanitizerClean(t *testing.T) {
	for _, name := range []string{"floats", "float-conversions"} {
		dir := filepath.Join("..", "..", "examples", name)
		src := readFile(t, filepath.Join(dir, "main.tck"))
		want := readFile(t, filepath.Join(dir, "output"))
		if strings.Contains(want, "FAIL") {
			t.Fatalf("examples/%s/output expects a check to fail", name)
		}
		for _, level := range []string{"-O0", "-O2"} {
			t.Run(name+level, func(t *testing.T) {
				bin := buildWithStd(t, src, append([]string{level}, floatSanitizers...)...)
				checkWrites(t, bin, want)
			})
		}
	}
}

// checkWrites runs bin and checks it exits 0, writing exactly want.
func checkWrites(t *testing.T, bin, want string) {
	t.Helper()
	var stderr strings.Builder
	cmd := exec.Command(bin)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v\n%s\nstderr:\n%s", err, out, stderr.String())
	}
	if string(out) != want {
		t.Errorf("wrote\n%s\nwant\n%s", out, want)
	}
}

// intTarget is an integer type a float converts to: its name, width, and
// whether it is signed.
type intTarget struct {
	name   string
	bits   int
	signed bool
}

// intTargets are every fixed-width integer type.
var intTargets = []intTarget{
	{"int8", 8, true},
	{"int16", 16, true},
	{"int32", 32, true},
	{"int64", 64, true},
	{"uint8", 8, false},
	{"uint16", 16, false},
	{"uint32", 32, false},
	{"uint64", 64, false},
}

// convertsTo returns what v, ok := T(x) gives for the float x and the
// integer type tt, as Tack defines it: x truncated toward zero, when that
// is a value of tt, and true, or "0" and false for NaN, an infinity, or a
// truncation outside tt, worked out exactly.
func convertsTo(x float64, tt intTarget) (string, bool) {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return "0", false
	}
	n, _ := new(big.Float).SetFloat64(math.Trunc(x)).Int(nil)
	lo, hi := new(big.Int), new(big.Int).Lsh(big.NewInt(1), uint(tt.bits))
	if tt.signed {
		hi.Rsh(hi, 1)
		lo.Neg(hi)
	}
	hi.Sub(hi, big.NewInt(1))
	if n.Cmp(lo) < 0 || n.Cmp(hi) > 0 {
		return "0", false
	}
	return n.String(), true
}

// edgeValues returns, for the float type of bits bits, values at the edges
// of every integer type, each as a float64 that is a value of that type:
// each power of two an integer type's range ends at, with the float
// values beside it and its halves, its negations, values just around 0
// and 1, and NaN and the infinities; and n random values across many
// magnitudes, from rng.
func edgeValues(bits int, n int, rng *rand.Rand) []float64 {
	round := func(x float64) float64 {
		if bits == 32 {
			return float64(float32(x))
		}
		return x
	}
	next := func(x, toward float64) float64 {
		if bits == 32 {
			return float64(math.Nextafter32(float32(x), float32(toward)))
		}
		return math.Nextafter(x, toward)
	}
	var vals []float64
	for _, p := range []int{7, 8, 15, 16, 31, 32, 63, 64} {
		e := math.Ldexp(1, p)
		for _, x := range []float64{e, e - 1, e + 1, e - 0.5, e + 0.5, next(e, 0), next(e, math.Inf(1))} {
			vals = append(vals, round(x), round(-x))
		}
	}
	vals = append(vals, 0, math.Copysign(0, -1), 0.5, -0.5, 0.999, -0.999, 1, -1, 1.5, -1.5,
		math.NaN(), math.Inf(1), math.Inf(-1), math.MaxFloat32, -math.MaxFloat32)
	for range n {
		x := math.Ldexp(rng.Float64(), rng.IntN(140)-10)
		if rng.IntN(2) == 0 {
			x = -x
		}
		vals = append(vals, round(x))
	}
	return vals
}

// floatSource returns how the float64 x, a value of the float type of
// bits bits, is written as a Tack expression of that type, from the
// variables zero and inf a program declares in it: a decimal literal that
// rounds back to exactly x, or, for NaN and the infinities, which have no
// literal, zero / zero, inf and -inf.
func floatSource(x float64, bits int) string {
	switch {
	case math.IsNaN(x):
		return "zero / zero"
	case math.IsInf(x, 1):
		return "inf"
	case math.IsInf(x, -1):
		return "-inf"
	case x == 0 && math.Signbit(x):
		return "-zero"
	}
	s := strconv.FormatFloat(x, 'e', -1, bits)
	if x < 0 {
		return "zero - " + s[1:]
	}
	return "zero + " + s
}

// floatConvProgram returns a program that converts each of vals, values
// of the float type of bits bits, to each integer type in the checked
// form, and prints "FAIL" and the case for each conversion that does not
// give what convertsTo says, and then "done".
func floatConvProgram(bits int, vals []float64) string {
	ft := fmt.Sprintf("float%d", bits)
	var b strings.Builder
	b.WriteString("package main\n\n")
	b.WriteString("// fail prints that the conversion named name went wrong.\n")
	b.WriteString(
		"func fail(name string) {\n    print(\"FAIL \")\n    print(name)\n    print(\"\\n\")\n}\n\n",
	)
	for _, tt := range intTargets {
		fmt.Fprintf(
			&b,
			"// to_%s reports whether converting f to %s gives want, with ok.\n",
			tt.name,
			tt.name,
		)
		fmt.Fprintf(&b, "func to_%s(f %s, want %s, wantOK bool) bool {\n", tt.name, ft, tt.name)
		fmt.Fprintf(&b, "    v, ok := %s(f)\n    return ok == wantOK && v == want\n}\n\n", tt.name)
	}
	fmt.Fprintf(&b, "func main() {\n    var zero %s = 0.0\n    inf := 1.0 / zero\n", ft)
	for i, x := range vals {
		for _, tt := range intTargets {
			want, ok := convertsTo(x, tt)
			fmt.Fprintf(&b, "    if !to_%s(%s, %s, %t) {\n        fail(\"%d %s\")\n    }\n",
				tt.name, floatSource(x, bits), want, ok, i, tt.name)
		}
	}
	b.WriteString("    print(\"done\\n\")\n}\n")
	return b.String()
}

// TestFloatToIntMatchesGo converts hundreds of float64 and float32 values,
// every edge of every integer type among them and random values of every
// magnitude, to every fixed-width integer type in the checked form, built
// with floatSanitizers, and checks each gives what Tack defines, worked
// out exactly in Go: no conversion is off by one at an edge, and none
// reaches a C cast that is undefined.
func TestFloatToIntMatchesGo(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, bits := range []int{64, 32} {
		t.Run(fmt.Sprintf("float%d", bits), func(t *testing.T) {
			src := floatConvProgram(bits, edgeValues(bits, 200, rng))
			bin := buildWithStd(t, src, append([]string{"-O1"}, floatSanitizers...)...)
			checkWrites(t, bin, "done\n")
		})
	}
}
