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

// TestSlicesExampleIsSanitizerClean builds examples/slices with clang's
// warnings as errors and the address and undefined behavior sanitizers
// on, so a slice that read past its string or array, moved a null
// pointer, or viewed a copy that no longer exists fails the run, and
// checks every check it prints passed.
func TestSlicesExampleIsSanitizerClean(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "examples", "slices", "main.tck"))
	if err != nil {
		t.Fatal(err)
	}
	atO0Too(t, []string{"-O1", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"}, func(t *testing.T, flags []string) {
		bin := buildWith(
			t,
			string(src),
			flags...,
		)
		out, err := exec.Command(bin).Output()
		if err != nil {
			t.Fatalf("slices failed: %v\n%s", err, out)
		}
		if strings.Contains(string(out), "FAIL") || strings.Count(string(out), " ok\n") != 28 {
			t.Errorf("slices printed\n%s", out)
		}
	})
}

// sliceTextCases are programs and lines the C generated for each must
// hold: a string's slice calls the string helper, which moves the pointer
// only past 0, a high bound left out calls the from helper, an array is
// viewed whole before it is sliced, a mutable global array is copied for
// an argument, and the checked form tests each bound by value.
var sliceTextCases = []struct{ name, src, want string }{
	{
		"string helper",
		"package main\n\nfunc main() {\n    s := \"abc\"\n    print(s[1:2])\n}\n",
		"static tack_string tack_slice_string(tack_string s, int64_t lo, int64_t hi) {\n" +
			"    return (tack_string){lo == 0 ? s.ptr : s.ptr + lo, hi - lo};\n}\n",
	},
	{
		"from helper",
		"package main\n\nfunc main() {\n    s := \"abc\"\n    print(s[1:])\n}\n",
		"    return (tack_string){lo == 0 ? s.ptr : s.ptr + lo, s.len - lo};\n",
	},
	{
		"string call",
		"package main\n\nfunc main() {\n    s := \"abc\"\n    print(s[1:2])\n}\n",
		"tack_slice_string(v_s, (int64_t)1, (int64_t)2)",
	},
	{
		"low bound left out",
		"package main\n\nfunc main() {\n    s := \"abc\"\n    print(s[:2])\n}\n",
		"tack_slice_string(v_s, 0, (int64_t)2)",
	},
	{
		"array viewed whole",
		"package main\n\nfunc f(v []int64) {\n}\n\nfunc main() {\n    a := [3]int64{1, 2, 3}\n    f(a[1:])\n}\n",
		"){v_a.e, 3}, (int64_t)1)",
	},
	{
		"mutable global copied",
		"package main\n\nmut g [3]int64\n\nfunc f(v []int64) {\n}\n\nfunc main() {\n    f(g[1:])\n}\n",
		"(tack_c0 = tack_4_main1_g, (tack_view_",
	},
	{
		"checked form",
		"package main\n\nfunc f(s string, i int8, u uint64) {\n    t, ok := s[i:u]\n    if ok {\n        print(t)\n" +
			"    }\n}\n\nfunc main() {\n    f(\"abc\", 1, 2)\n}\n",
		"(tack_r1 >= 0 && (tack_r1 < 0 || (uint64_t)tack_r1 <= (uint64_t)tack_r2) && " +
			"(tack_r0.len >= 0 && (uint64_t)tack_r0.len >= (uint64_t)tack_r2))",
	},
}

// TestSliceText checks the C generated for each of sliceTextCases holds
// its line.
func TestSliceText(t *testing.T) {
	for _, tt := range sliceTextCases {
		t.Run(tt.name, func(t *testing.T) {
			if got := generate(t, tt.src); !strings.Contains(got, tt.want) {
				t.Errorf("generated C is missing %q:\n%s", tt.want, got)
			}
		})
	}
}

// sliceBoundTypes are the integer types a checked slice's bounds are
// tested with, and values of each at its edges and around a length of 3.
var sliceBoundTypes = []struct {
	name   string
	values []*big.Int
}{
	{"int8", ints(-128, -1, 0, 1, 3, 4, 127)},
	{"int64", append(ints(-1, 0, 2, 3, 4), big.NewInt(math.MinInt64), big.NewInt(math.MaxInt64))},
	{"uint8", ints(0, 1, 3, 4, 255)},
	{"uint32", ints(0, 3, 4, 4294967295)},
	{"uint64", append(ints(0, 2, 3, 4), new(big.Int).SetUint64(math.MaxUint64))},
	{"unsafe_size_t", ints(0, 1, 3, 4, 4294967295)},
}

// ints returns each of vs as a big.Int.
func ints(vs ...int64) []*big.Int {
	out := make([]*big.Int, len(vs))
	for i, v := range vs {
		out[i] = big.NewInt(v)
	}
	return out
}

// TestCheckedSlicesAtTheEdges builds, with warnings as errors and the
// undefined behavior sanitizer on, a program that slices "abc" with the
// checked form at every pair of bound types and edge values, and with
// each bound left out, and checks every ok and every slice's length
// against the value math/big gives: in range exactly when
// 0 <= lo <= hi <= 3.
func TestCheckedSlicesAtTheEdges(t *testing.T) {
	var body []string
	n := 0
	check := func(lo, hi *big.Int, expr string) {
		in, want := checkedSliceWant(lo, hi)
		body = append(body,
			fmt.Sprintf("t%d, ok%d := %s", n, n, expr),
			fmt.Sprintf("if ok%d != %v || len(t%d) != %d {", n, in, n, want),
			fmt.Sprintf("    exit(%d)", n%200+1),
			"}")
		n++
	}
	for i, lt := range sliceBoundTypes {
		for _, lv := range lt.values {
			body = append(body, fmt.Sprintf("mut a%d_%d %s = %s", i, n, lt.name, lv))
			lo := fmt.Sprintf("a%d_%d", i, n)
			check(lv, nil, "s["+lo+":]")
			check(nil, lv, "s[:"+lo+"]")
			for _, ht := range sliceBoundTypes {
				for _, hv := range ht.values {
					body = append(body, fmt.Sprintf("mut b%d %s = %s", n, ht.name, hv))
					check(lv, hv, fmt.Sprintf("s[%s:b%d]", lo, n))
				}
			}
		}
	}
	src := "package main\n\nfunc main() {\n    s := \"abc\"\n    " + strings.Join(body, "\n    ") +
		"\n    exit(0)\n}\n"
	bin := buildWith(t, src, "-O1", "-fsanitize=undefined", "-fno-sanitize-recover=all")
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("checked slices failed: %v\n%s", err, out)
	}
}

// checkedSliceWant returns whether "abc"[lo:hi] is in range, a bound left
// out being nil, and the length of the slice it gives: 0 <= lo <= hi <= 3,
// with 0 and 3 for the bounds left out, and hi - lo, or 0 out of range.
func checkedSliceWant(lo, hi *big.Int) (bool, int64) {
	l, h := big.NewInt(0), big.NewInt(3)
	if lo != nil {
		l = lo
	}
	if hi != nil {
		h = hi
	}
	if l.Sign() < 0 || l.Cmp(h) > 0 || h.Cmp(big.NewInt(3)) > 0 {
		return false, 0
	}
	return true, new(big.Int).Sub(h, l).Int64()
}

// TestNullSlicesAreSanitizerClean checks slicing the zero string inside a
// zeroed struct, and a view of its bytes, whose pointers are null, at 0
// moves no pointer, under the undefined behavior sanitizer, which reports
// adding even 0 to a null pointer.
func TestNullSlicesAreSanitizerClean(t *testing.T) {
	src := "package main\n\ntype T struct {\n    s string\n}\n\n" +
		"func n(b []uint8) int64 {\n    return len(b[0:0]) + len(b[:]) +% len(b[len(b):])\n}\n\n" +
		"func main() {\n    mut z T\n    a := z.s[0:]\n    c := z.s[:0]\n    d, ok := z.s[0:0]\n" +
		"    if len(a) +% len(c) +% len(d) +% n([]uint8(z.s)) != 0 || !ok {\n        exit(1)\n    }\n}\n"
	bin := buildWith(t, src, "-O0", "-fsanitize=undefined", "-fno-sanitize-recover=all")
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("null slices failed: %v\n%s", err, out)
	}
}
