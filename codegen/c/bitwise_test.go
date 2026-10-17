package c_test

import (
	"fmt"
	"math/big"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

// bitType is an integer type the bitwise matrix covers: its name, its
// width in bits on this target, and whether it is signed.
type bitType struct {
	name   string
	bits   uint
	signed bool
	target bool
}

// bitTypes are every integer type, the target-width ones at this target's
// width, which the test builds for.
var bitTypes = []bitType{
	{name: "int8", bits: 8, signed: true},
	{name: "int16", bits: 16, signed: true},
	{name: "int32", bits: 32, signed: true},
	{name: "int64", bits: 64, signed: true},
	{name: "uint8", bits: 8, signed: false},
	{name: "uint16", bits: 16, signed: false},
	{name: "uint32", bits: 32, signed: false},
	{name: "uint64", bits: 64, signed: false},
	{name: "unsafe_size_t", bits: uint(unsafe.Sizeof(uintptr(0))) * 8, target: true},
	{name: "unsafe_uintptr_t", bits: uint(unsafe.Sizeof(uintptr(0))) * 8, target: true},
}

// countType is an unsigned type a shift count is given as, with the
// counts the matrix shifts by in it.
type countType struct {
	counts func(bits uint) []*big.Int
	name   string
}

// countTypes are the count types the matrix shifts by: uint8, whose
// largest, 255, is past every width, uint32, and uint64, whose largest
// counts are far past it.
var countTypes = []countType{
	{name: "uint8", counts: func(bits uint) []*big.Int {
		return bigs(0, 1, int64(bits)-1, int64(bits), int64(bits)+1, 255)
	}},
	{name: "uint32", counts: func(bits uint) []*big.Int {
		return append(bigs(0, 1, int64(bits)-1, int64(bits), int64(bits)+1), big.NewInt(1<<32-1))
	}},
	{name: "uint64", counts: func(bits uint) []*big.Int {
		return append(bigs(0, 1, int64(bits)-1, int64(bits), int64(bits)+1, 255),
			new(big.Int).Lsh(big.NewInt(1), 63), new(big.Int).SetUint64(1<<64-1))
	}},
}

// bigs returns vs as big.Ints.
func bigs(vs ...int64) []*big.Int {
	out := make([]*big.Int, len(vs))
	for i, v := range vs {
		out[i] = big.NewInt(v)
	}
	return out
}

// values returns the boundary values of t: 0, 1, -1, the smallest and the
// largest for a signed type, and 0, 1, the top bit alone, the largest and
// every other bit for an unsigned one. A target-width type's constants
// must be values on every target, so its are those of a uint32.
func (t bitType) values() []*big.Int {
	if t.target {
		return bitType{bits: 32}.values()
	}
	one := big.NewInt(1)
	top := new(big.Int).Lsh(one, t.bits-1)
	if t.signed {
		return []*big.Int{
			big.NewInt(0),
			one,
			big.NewInt(-1),
			new(big.Int).Neg(top),
			new(big.Int).Sub(top, one),
		}
	}
	most := new(big.Int).Sub(new(big.Int).Lsh(one, t.bits), one)
	alternate, _ := new(big.Int).SetString(strings.Repeat("5", int(t.bits/4)), 16)
	return []*big.Int{big.NewInt(0), one, top, most, alternate}
}

// wrap returns v reduced to t's width, as a value of t: its low bits,
// read as two's complement when t is signed.
func (t bitType) wrap(v *big.Int) *big.Int {
	mod := new(big.Int).Lsh(big.NewInt(1), t.bits)
	r := new(big.Int).Mod(v, mod)
	if t.signed && r.Cmp(new(big.Int).Rsh(mod, 1)) >= 0 {
		r.Sub(r, mod)
	}
	return r
}

// shl returns a << n in t: 0 for a count at or past the width, and the
// low bits of a times 2^n otherwise.
func (t bitType) shl(a, n *big.Int) *big.Int {
	if n.Cmp(big.NewInt(int64(t.bits))) >= 0 {
		return big.NewInt(0)
	}
	return t.wrap(new(big.Int).Lsh(a, uint(n.Uint64())))
}

// shr returns a >> n in t: a divided by 2^n, rounded down, which for a
// count past the width is 0, or -1 for a negative a.
func (t bitType) shr(a, n *big.Int) *big.Int {
	shift := min(n.Uint64(), uint64(t.bits)+1)
	return new(big.Int).Rsh(a, uint(shift))
}

// results returns the type an array of t's results is written in, and
// how a result is converted to compare with one: t itself, as it is, for
// a type whose every value can be written, and uint64, which a
// target-width type widens to, for a target-width type.
func (t bitType) results() (typ, conv string) {
	if t.target {
		return "uint64", "uint64"
	}
	return t.name, ""
}

// literal writes the values of vs as a Tack array literal of type t.
func literal(t string, vs []*big.Int) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = v.String()
	}
	return fmt.Sprintf("[%d]%s{%s}", len(vs), t, strings.Join(parts, ", "))
}

// bitMatrixProgram returns a program that checks every bitwise and shift
// operator, and its compound form, on every pair of t's boundary values,
// and every shift of them by every count of every count type, against the
// results computed here, and ^ on each value. Each operator's check
// prints its type, its operator and "ok", or "FAIL".
func bitMatrixProgram() string {
	var b strings.Builder
	b.WriteString("package main\n\nfunc check(cond bool, name string) {\n    print(name)\n" +
		"    if cond {\n        print(\" ok\\n\")\n    } else {\n        print(\" FAIL\\n\")\n    }\n}\n\n")
	calls := make([]string, 0, len(bitTypes))
	for _, t := range bitTypes {
		name := "test_" + t.name
		calls = append(calls, "    "+name+"()")
		bitMatrixFunc(&b, t, name)
	}
	b.WriteString("func main() {\n" + strings.Join(calls, "\n") + "\n}\n")
	return b.String()
}

// bitMatrixFunc writes the function name that checks t's matrix.
func bitMatrixFunc(b *strings.Builder, t bitType, name string) {
	vals := t.values()
	n := len(vals)
	fmt.Fprintf(b, "func %s() {\n    vals := %s\n", name, literal(t.name, vals))
	ops := []struct {
		do       func(x, y *big.Int) *big.Int
		name, op string
	}{
		{name: "and", op: "&", do: func(x, y *big.Int) *big.Int { return new(big.Int).And(x, y) }},
		{name: "or", op: "|", do: func(x, y *big.Int) *big.Int { return new(big.Int).Or(x, y) }},
		{name: "xor", op: "^", do: func(x, y *big.Int) *big.Int { return new(big.Int).Xor(x, y) }},
		{
			name: "andnot",
			op:   "&^",
			do:   func(x, y *big.Int) *big.Int { return new(big.Int).AndNot(x, y) },
		},
	}
	nots := make([]*big.Int, 0, len(vals))
	for _, x := range vals {
		nots = append(nots, t.wrap(new(big.Int).Not(x)))
	}
	rt, conv := t.results()
	fmt.Fprintf(b, "    nots := %s\n    mut okNot := true\n", literal(rt, nots))
	fmt.Fprintf(
		b,
		"    for i := range %d {\n        okNot = okNot && %s(^vals[i]) == nots[i]\n    }\n",
		n,
		conv,
	)
	fmt.Fprintf(b, "    check(okNot, %q)\n", t.name+" ^x")
	for _, o := range ops {
		want := make([]*big.Int, 0, len(vals)*len(vals))
		for _, x := range vals {
			for _, y := range vals {
				want = append(want, t.wrap(o.do(x, y)))
			}
		}
		fmt.Fprintf(b, "    %ss := %s\n    mut ok_%s := true\n", o.name, literal(rt, want), o.name)
		fmt.Fprintf(b, "    for i := range %d {\n        for j := range %d {\n", n, n)
		fmt.Fprintf(b, "            w, in := %ss[i * %d + j]\n", o.name, n)
		fmt.Fprintf(b, "            mut c := vals[i]\n            c %s= vals[j]\n", o.op)
		fmt.Fprintf(
			b,
			"            ok_%s = ok_%s && in && %s(vals[i] %s vals[j]) == w && %s(c) == w\n",
			o.name,
			o.name,
			conv,
			o.op,
			conv,
		)
		b.WriteString("        }\n    }\n")
		fmt.Fprintf(b, "    check(ok_%s, %q)\n", o.name, t.name+" "+o.op)
	}
	for _, ct := range countTypes {
		fmt.Fprintf(b, "    %s_%s(vals)\n", name, ct.name)
	}
	b.WriteString("}\n\n")
	for _, ct := range countTypes {
		bitShifts(b, t, ct, vals, name+"_"+ct.name)
	}
}

// bitShifts writes the function name, which checks << and >> of each of
// vals, of type t, by every count of ct.
func bitShifts(b *strings.Builder, t bitType, ct countType, vals []*big.Int, name string) {
	counts := ct.counts(t.bits)
	shls := make([]*big.Int, 0, len(vals)*len(counts))
	shrs := make([]*big.Int, 0, len(vals)*len(counts))
	for _, x := range vals {
		for _, k := range counts {
			shls = append(shls, t.shl(x, k))
			shrs = append(shrs, t.shr(x, k))
		}
	}
	m := len(counts)
	rt, conv := t.results()
	fmt.Fprintf(b, "func %s(vals [%d]%s) {\n", name, len(vals), t.name)
	fmt.Fprintf(b, "        counts := %s\n        shls := %s\n        shrs := %s\n",
		literal(ct.name, counts), literal(rt, shls), literal(rt, shrs))
	b.WriteString("        mut okShl := true\n        mut okShr := true\n")
	fmt.Fprintf(b, "        for i := range %d {\n            for j := range %d {\n", len(vals), m)
	fmt.Fprintf(
		b,
		"                l, inL := shls[i * %d + j]\n                r, inR := shrs[i * %d + j]\n",
		m,
		m,
	)
	b.WriteString("                mut x := vals[i]\n                x <<= counts[j]\n" +
		"                mut y := vals[i]\n                y >>= counts[j]\n")
	fmt.Fprintf(
		b,
		"                okShl = okShl && inL && %s(vals[i] << counts[j]) == l && %s(x) == l\n",
		conv,
		conv,
	)
	fmt.Fprintf(
		b,
		"                okShr = okShr && inR && %s(vals[i] >> counts[j]) == r && %s(y) == r\n",
		conv,
		conv,
	)
	b.WriteString(
		"            }\n        }\n")
	fmt.Fprintf(b, "        check(okShl, %q)\n        check(okShr, %q)\n}\n\n",
		t.name+" << "+ct.name, t.name+" >> "+ct.name)
}

// TestBitwiseMatrixRuns builds bitMatrixProgram at every optimization
// level with the undefined behavior sanitizer on, every finding fatal, and
// checks every check passes: & | ^ &^ and ^x, and their compound forms, at
// every pair of boundary values of every integer type, the target-width
// ones included, and << and >> of each by counts of 0, 1, one less than
// the width, the width, one more, and 255 and past as uint8, uint32 and
// uint64 counts, giving 0, or -1 for >> of a negative value, at and past
// the width, against results computed exactly here.
func TestBitwiseMatrixRuns(t *testing.T) {
	src := bitMatrixProgram()
	checks := len(bitTypes) * (5 + 2*len(countTypes))
	for _, level := range []string{"-O0", "-O1", "-O2", "-O3"} {
		t.Run(level, func(t *testing.T) {
			bin := buildWithStd(t, src, level, "-fsanitize=undefined", "-fno-sanitize-recover=all")
			runChecks(t, bin, checks)
		})
	}
}

// TestBitwiseMatrixValues checks the expected values the matrix is built
// from at a few points worked by hand, so a mistake in computing them
// cannot hide a matching mistake in the compiler.
func TestBitwiseMatrixValues(t *testing.T) {
	i8, u8 := bitTypes[0], bitTypes[4]
	for _, tt := range []struct {
		got  *big.Int
		want int64
	}{
		{i8.shl(big.NewInt(1), big.NewInt(7)), -128},
		{i8.shl(big.NewInt(-1), big.NewInt(8)), 0},
		{i8.shr(big.NewInt(-128), big.NewInt(255)), -1},
		{i8.shr(big.NewInt(127), big.NewInt(7)), 0},
		{u8.shl(big.NewInt(255), big.NewInt(1)), 254},
		{u8.shr(big.NewInt(255), big.NewInt(8)), 0},
		{i8.wrap(new(big.Int).Not(big.NewInt(-128))), 127},
		{u8.wrap(new(big.Int).Not(big.NewInt(0))), 255},
	} {
		if tt.got.Int64() != tt.want {
			t.Errorf("got %s, want %s", tt.got, strconv.FormatInt(tt.want, 10))
		}
	}
}

// TestBitwiseExampleRuns builds examples/bitwise at every optimization
// level with the undefined behavior sanitizer on, every finding fatal, and
// checks every check passes: each operator and compound form at its
// boundaries, constants, precedence, and FNV-1a written with ^.
func TestBitwiseExampleRuns(t *testing.T) {
	src := readFile(t, filepath.Join("..", "..", "examples", "bitwise", "main.tck"))
	for _, level := range []string{"-O0", "-O1", "-O2", "-O3"} {
		t.Run(level, func(t *testing.T) {
			bin := buildWithStd(t, src, level, "-fsanitize=undefined", "-fno-sanitize-recover=all")
			runChecks(t, bin, 22)
		})
	}
}
