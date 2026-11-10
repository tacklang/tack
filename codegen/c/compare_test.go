package c_test

import (
	"fmt"
	"math"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// comparedType is an integer type with the values at its edges that a
// comparison with another type is tested at. A target-width type's are
// the constants it may hold on every target.
type comparedType struct {
	name   string
	values []*big.Int
}

// signedEdges returns the edge values of a signed type of the given
// width: its smallest, -1, 0, 1 and its largest.
func signedEdges(bits uint) []*big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), bits-1)
	return []*big.Int{
		new(big.Int).Neg(limit),
		big.NewInt(-1),
		big.NewInt(0),
		big.NewInt(1),
		new(big.Int).Sub(limit, big.NewInt(1)),
	}
}

// unsignedEdges returns the edge values of an unsigned type of the given
// width: 0, 1, the largest value of the signed type as wide, the one
// after it, and its own largest.
func unsignedEdges(bits uint) []*big.Int {
	half := new(big.Int).Lsh(big.NewInt(1), bits-1)
	return []*big.Int{
		big.NewInt(0),
		big.NewInt(1),
		new(big.Int).Sub(half, big.NewInt(1)),
		half,
		new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits), big.NewInt(1)),
	}
}

// comparedTypes is every integer type, with its edge values.
var comparedTypes = []comparedType{
	{"int8", signedEdges(8)},
	{"int16", signedEdges(16)},
	{"int32", signedEdges(32)},
	{"int64", signedEdges(64)},
	{"uint8", unsignedEdges(8)},
	{"uint16", unsignedEdges(16)},
	{"uint32", unsignedEdges(32)},
	{"uint64", unsignedEdges(64)},
	{"unsafe_size_t", unsignedEdges(32)},
	{"unsafe_uintptr_t", unsignedEdges(32)},
}

// comparisonOps are the six comparisons, each with the bit it sets in
// what a compare function returns, and whether it holds for the result
// of comparing two values, -1, 0 or 1.
var comparisonOps = []struct {
	holds func(cmp int) bool
	op    string
	bit   int
}{
	{func(c int) bool { return c == 0 }, "==", 1},
	{func(c int) bool { return c != 0 }, "!=", 2},
	{func(c int) bool { return c < 0 }, "<", 4},
	{func(c int) bool { return c <= 0 }, "<=", 8},
	{func(c int) bool { return c > 0 }, ">", 16},
	{func(c int) bool { return c >= 0 }, ">=", 32},
}

// compareFunc returns a Tack function, c_X_Y, that compares an X with a Y
// with each of the six operators and returns the bits of those that held.
func compareFunc(x, y string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "func c_%s_%s(a %s, b %s) int64 {\n    mut r := 0\n", x, y, x, y)
	for _, op := range comparisonOps {
		fmt.Fprintf(&b, "    if a %s b {\n        r |= %d\n    }\n", op.op, op.bit)
	}
	b.WriteString("    return r\n}\n\n")
	return b.String()
}

// valueBits returns the bits a compare function must return for a and
// b, by their values.
func valueBits(a, b *big.Int) int {
	bits := 0
	for _, op := range comparisonOps {
		if op.holds(a.Cmp(b)) {
			bits += op.bit
		}
	}
	return bits
}

// mixedCompareProgram returns a program that compares every integer type
// with every other, itself included, at the edge values of both, with all
// six operators, and prints a line naming each comparison whose result is
// not what the values say, then "done".
func mixedCompareProgram() string {
	var funcs, calls strings.Builder
	for _, x := range comparedTypes {
		for _, y := range comparedTypes {
			funcs.WriteString(compareFunc(x.name, y.name))
			for _, a := range x.values {
				for _, b := range y.values {
					fmt.Fprintf(
						&calls,
						"    if c_%s_%s(%s, %s) != %d {\n        print(\"FAIL %s %s %s %s\\n\")\n    }\n",
						x.name,
						y.name,
						a,
						b,
						valueBits(a, b),
						x.name,
						a,
						y.name,
						b,
					)
				}
			}
		}
	}
	return "package main\n\n" + funcs.String() + "func main() {\n" + calls.String() +
		"    print(\"done\\n\")\n}\n"
}

// TestMixedComparisonsCompareValues builds a program comparing every pair
// of integer types at the edges of both, with all six operators, strictly
// and with the undefined behavior sanitizer stopping the program, at no
// optimization and at a high one, and checks every comparison gave what
// the operands' values say: -1 is less than every unsigned value, and
// int8 -1 is not uint8 255, unlike in C's usual arithmetic conversions.
func TestMixedComparisonsCompareValues(t *testing.T) {
	src := mixedCompareProgram()
	for _, opt := range []string{"-O0", "-O2"} {
		t.Run(opt, func(t *testing.T) {
			bin := buildWith(t, src, opt, "-fsanitize=undefined", "-fno-sanitize-recover=all")
			out, err := exec.Command(bin).CombinedOutput()
			if err != nil || string(out) != "done\n" {
				t.Errorf("got %v, output:\n%s", err, out)
			}
		})
	}
}

// TestMixedComparisonLowering checks the C each kind of comparison of two
// integer types is lowered to: one type compares as it is, two unsigned
// ones as uint64_t, a signed one and an unsigned one of at most 32 bits
// as int64_t, and a signed one and a uint64 or a target-width one by the
// sign first, whichever side each is on; a pure operand is read as it is,
// and one that calls a function is held in a temporary first, in order.
func TestMixedComparisonLowering(t *testing.T) {
	for _, tt := range []struct{ name, params, cond, want string }{
		{"one type", "a int64, b int64", "a < b", "return (p_a < p_b);"},
		{"two unsigned", "a uint8, b uint64", "a < b", "return ((uint64_t)p_a < (uint64_t)p_b);"},
		{
			"two target widths", "a unsafe_size_t, b unsafe_uintptr_t", "a == b",
			"return ((uint64_t)p_a == (uint64_t)p_b);",
		},
		{"two signed", "a int8, b int64", "a >= b", "return ((int64_t)p_a >= (int64_t)p_b);"},
		{
			"signed and narrow unsigned", "a int32, b uint32", "a != b",
			"return ((int64_t)p_a != (int64_t)p_b);",
		},
		{
			"signed below uint64", "a int64, b uint64", "a < b",
			"return (p_a < 0 || (uint64_t)p_a < (uint64_t)p_b);",
		},
		{
			"uint64 above signed", "a uint64, b int8", "a > b",
			"return (p_b < 0 || (uint64_t)p_b < (uint64_t)p_a);",
		},
		{
			"signed equal to size", "a int16, b unsafe_size_t", "a == b",
			"return (p_a >= 0 && (uint64_t)p_a == (uint64_t)p_b);",
		},
		{
			"uintptr at least signed", "a unsafe_uintptr_t, b int32", "a >= b",
			"return (p_b < 0 || (uint64_t)p_b <= (uint64_t)p_a);",
		},
		{
			"calls held in order", "a int64, b uint64", "s(a) <= u(b)",
			"return (tack_o0 = tack_4_main1_s(p_a), tack_o1 = tack_4_main1_u(p_b), " +
				"(tack_o0 < 0 || (uint64_t)tack_o0 <= (uint64_t)tack_o1));",
		},
		{
			"only the call held", "a int64, b uint64", "a != u(b)",
			"return (tack_o0 = tack_4_main1_u(p_b), (p_a < 0 || (uint64_t)p_a != (uint64_t)tack_o0));",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n\nfunc s(v int64) int64 {\n    return v\n}\n\n" +
				"func u(v uint64) uint64 {\n    return v\n}\n\n" +
				"func f(" + tt.params + ") bool {\n    return " + tt.cond + "\n}\n\nfunc main() {\n}\n"
			if got := generate(t, src); !strings.Contains(got, tt.want) {
				t.Errorf("want %q in\n%s", tt.want, got)
			}
		})
	}
}

// TestMixedSwitchLowering checks a switch compares its tag with a case
// value of another integer type by value, with no parentheses around a
// single test, and holds a value that calls a function in a temporary
// first.
func TestMixedSwitchLowering(t *testing.T) {
	src := "package main\n\nfunc u(v uint64) uint64 {\n    return v\n}\n\n" +
		"func f(n int64, b uint64, c uint8) int64 {\n    switch n {\n    case b:\n        return 1\n" +
		"    case c, u(b):\n        return 2\n    }\n    return 0\n}\n\nfunc main() {\n}\n"
	got := generate(t, src)
	for _, want := range []string{
		"if (tack_s0 >= 0 && (uint64_t)tack_s0 == (uint64_t)p_b) {",
		"} else if (((int64_t)tack_s0 == (int64_t)p_c) || (tack_o1 = tack_4_main1_u(p_b), " +
			"(tack_s0 >= 0 && (uint64_t)tack_s0 == (uint64_t)tack_o1))) {",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in\n%s", want, got)
		}
	}
}

// TestMixedCompareExampleIsSanitizerClean builds examples/mixed-compare
// with clang's warnings as errors and the address and undefined behavior
// sanitizers on, so a comparison that converted a negative value, or a
// warning about one clang can tell is always true or false, fails the
// run, and checks every check it prints passed.
func TestMixedCompareExampleIsSanitizerClean(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "examples", "mixed-compare", "main.tck"))
	if err != nil {
		t.Fatal(err)
	}
	bin := buildWith(
		t,
		string(src),
		"-O1",
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
	)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("mixed-compare failed: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "FAIL") || strings.Count(string(out), " ok\n") != 8 {
		t.Errorf("mixed-compare printed\n%s", out)
	}
}

// TestComparedTypesCoverEveryEdge keeps the table the comparison program
// is built from honest: each type's values are its edges, the largest
// uint64 and the smallest int64 among them.
func TestComparedTypesCoverEveryEdge(t *testing.T) {
	var found []string
	for _, ct := range comparedTypes {
		for _, v := range ct.values {
			if v.Cmp(new(big.Int).SetUint64(math.MaxUint64)) == 0 ||
				v.Cmp(big.NewInt(math.MinInt64)) == 0 {
				found = append(found, ct.name)
			}
		}
	}
	if strings.Join(found, " ") != "int64 uint64" {
		t.Errorf("the 64-bit edges are in %v, want int64 and uint64", found)
	}
}
