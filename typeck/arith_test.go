package typeck_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// arith wraps lines in f, which takes the parameters params and returns
// an int64, then a return of 0 and an empty main.
func arith(params string, lines ...string) string {
	body := ""
	for _, l := range lines {
		body += "    " + l + "\n"
	}
	return "package main\n\nfunc f(" + params + ") int64 {\n" + body +
		"    return 0\n}\n\nfunc main() {\n}\n"
}

// zeros returns an array literal of n int64 zeros.
func zeros(n int) string {
	return fmt.Sprintf("[%d]int64{%s}", n, strings.Repeat("0, ", n-1)+"0")
}

// arithStruct is arith with a struct type P, holding the int64 fields n
// and m, declared before f.
func arithStruct(params string, lines ...string) string {
	return strings.Replace(arith(params, lines...), "package main\n",
		"package main\n\ntype P struct {\n    n int64\n    m int64\n}\n", 1)
}

// provenArithmeticCases are uses of arithmetic that its operands' bounds,
// or the relations in force, prove: no operation in them can wrap where
// they are used, at the boundaries of the int64, int32, int8, uint64 and
// uint8 types and of the target-width ones.
var provenArithmeticCases = []struct{ name, src string }{
	{"negation of v + 1", arith("v int64", "if v < 0 {", "    x := uint64(-(v + 1))", "}")},
	{"int64 v * 2 + 1 at the edge", arith("v int64",
		"if v >= 0 && v <= 4611686018427387903 {", "    x := uint64(v * 2 + 1)", "}")},
	{
		"int64 sum to the largest",
		arith(
			"v int64",
			"if v >= 0 && v <= 4611686018427387904 {",
			"    x := uint64(v + 4611686018427387903)",
			"}",
		),
	},
	{"int64 v - 1 to the least", arith("v int64",
		"if v > -9223372036854775807 - 1 && v <= 0 {", "    return 1 / (v - 1)", "}")},
	{"int32 x * 2 + 1 at the edge", arith("x int32",
		"if 0 <= x && x <= 1073741823 {", "    y := uint32(x * 2 + 1)", "}")},
	{"int32 negation away from the least", arith("x int32",
		"if x > -2147483648 && x < 0 {", "    y := uint32(-x)", "}")},
	{"int8 b * 2 - 1", arith("b int8", "if 1 <= b && b <= 63 {", "    y := uint8(b * 2 - 1)", "}")},
	{"int8 0 - b", arith("b int8", "if b > -128 && b <= 0 {", "    y := uint8(0 - b)", "}")},
	{"uint8 u * 2 + 1", arith("u uint8", "if u <= 63 {", "    y := int8(u * 2 + 1)", "}")},
	{
		"uint8 u - 1 away from 0",
		arith("u uint8", "if u > 1 {", "    return int64(100 / (u - 1))", "}"),
	},
	{"uint64 u - 1", arith("u uint64", "a := [4]int64{1, 2, 3, 4}", "if 0 < u && u <= 4 {",
		"    return a[u - 1]", "}")},
	{"x * 0 as an index", arith("x int64", "a := [1]int64{7}", "return a[x * 0]")},
	{"mixed signs", arith("i int64, j int64", "a := "+zeros(19),
		"if -3 <= i && i <= 3 && -3 <= j && j <= 3 {", "    return a[i * j + 9]", "}")},
	{"mixed signs at large magnitudes", arith("i int64, j int64",
		"if 0 <= i && i <= 3037000499 && -3037000499 <= j && j <= 0 {",
		"    x := uint64(-(i * j))", "}")},
	{"two variables", arith("i int64, j int64", "a := "+zeros(7),
		"if 0 <= i && i < 4 && 0 <= j && j < 4 {", "    return a[i + j]", "}")},
	{"a divisor x - 5", arith("x int64", "if x > 5 {", "    return 100 / (x - 5)", "}")},
	{"a remainder by x - 5", arith("x int64", "if x > 5 {", "    return 100 % (x - 5)", "}")},
	{"a compound division by x - 5", arith("x int64", "mut y int64 = 100",
		"if x > 5 {", "    y /= x - 5", "}", "return y")},
	{"a product of nonzero values", arith("x int64",
		"if x != 0 && -10 < x && x < 10 {", "    return 100 / (x * 3)", "}")},
	{"a negated nonzero value", arith("x int64",
		"if x != 0 && -100 < x && x < 100 {", "    return 100 / -x", "}")},
	{"a nonzero unsigned value less one", arith("u uint64", "a := [4]int64{1, 2, 3, 4}",
		"if u != 0 && u <= 4 {", "    return a[u - 1]", "}")},
	{"the last byte", arith("s string", "if len(s) > 0 {", "    return int64(s[len(s) - 1])", "}")},
	{"a view's next element", arith("a []int64, i int64",
		"if 0 <= i && i < len(a) - 1 {", "    return a[i + 1]", "}")},
	{"a slice of one byte", arith("s string, i int64",
		"if 0 <= i && i < len(s) {", "    print(s[i : i + 1])", "}")},
	{"a slice to two before the end", arith("s string",
		"if len(s) >= 2 {", "    print(s[:len(s) - 2])", "}")},
	{"a slice from i + 1", arith("s string, i int64",
		"if 0 <= i && i < len(s) {", "    print(s[i + 1:])", "}")},
	{"a relation keeps i + 1 from wrapping", arith("i int64, n int64", "a := "+zeros(11),
		"if 0 <= i && i < n && n <= 10 {", "    return a[i + 1]", "}")},
	{"an int8 relation through i + 1", arith("a []int64, b int8",
		"if 0 <= b && b < 100 && b + 1 < len(a) {", "    return a[b + 1]", "}")},
	{"fields of an immutable parameter", arithStruct("p P", "a := "+zeros(7),
		"if 0 <= p.n && p.n <= 3 && 0 <= p.m && p.m <= 3 {", "    return a[p.n + p.m]", "}")},
	{"past an early exit", arith("v int64", "if v >= 0 {", "    return 0", "}",
		"x := uint64(-(v + 1))")},
	{"a write in a branch that leaves", arith("i int64, c bool", "mut j = i", "a := "+zeros(3),
		"if 0 <= j && j < 2 {", "    if c {", "        j = 7", "        return 0", "    }",
		"    return a[j + 1]", "}")},
	{"an unsafe_size_t product", arith("n unsafe_size_t",
		"if n <= 1000 {", "    x := uint32(n * 4)", "}")},
	{"an unsafe_size_t sum to the portable largest", arith("n unsafe_size_t",
		"if n <= 4294967294 {", "    x := uint32(n + 1)", "}")},
	{"an int64 converted to unsafe_size_t", arith("n int64",
		"if 0 <= n && n <= 1000 {", "    x := unsafe_size_t(n * 4)", "}")},
	{"nested negations", arith("i int64", "a := "+zeros(4),
		"if 0 <= i && i <= 3 {", "    return a[-(-(-(i - 3)))]", "}")},
	{"a divisor from a length", arith("s string",
		"if len(s) > 1 {", "    return 100 / (len(s) - 1)", "}")},
	{"u - 1 related below a length", arith("a []int64, u uint64",
		"if u != 0 && u < len(a) {", "    return a[u - 1]", "}")},
	{"a product of negatives", arith("x int64", "a := "+zeros(13),
		"if -5 <= x && x <= -1 {", "    return a[x * 3 + 15]", "}")},
	{"a product as a slice's low bound", arith("s string, i int64",
		"if 0 <= i && i <= 4 && len(s) >= 9 {", "    print(s[2*i : 9])", "}")},
	{"a low bound i - 1 related through j", arith("s string, i int64, j int64",
		"if 0 <= j && j < i && i <= len(s) {", "    print(s[i - 1 : i])", "}")},
	{"a length as a slice's bound, related", arith("t string, s string",
		"if len(s) <= len(t) && len(s) < 5 {", "    print(t[:len(s)])", "}")},
	{"a length plus 0 as a slice's bound, related", arith("t string, s string",
		"if len(s) <= len(t) && len(s) < 5 {", "    print(t[:len(s) + 0])", "}")},
	{"a length as a slice's low bound, related", arith("t string, s string",
		"if len(s) <= len(t) - 5 {", "    print(t[len(s):])", "}")},
	{"a divisor's length checked above zero", arith("s string",
		"if len(s) < 10 && len(s) > 0 {", "    return 10 / len(s)", "}")},
	{"a slice's high length checked against the sliced length", arith("t string, s string",
		"if len(s) < 5 && len(s) <= len(t) {", "    print(t[:len(s)])", "}")},
	{"a slice's low length checked against the sliced length", arith("t string, s string",
		"if len(s) < 5 && len(s) <= len(t) {", "    print(t[len(s):])", "}")},
	{"a length copied for a known string's slice", arith("w string",
		"n := len(w)", "if n <= 3 {", "    print(\"abc\"[:n])", "}")},
	{"a length copied for an index", arith("s string", "a := "+zeros(3),
		"n := len(s)", "if n < 3 {", "    return a[n]", "}")},
	{"a length copied for a conversion", arith("s string",
		"n := len(s)", "if n <= 255 {", "    return int64(uint8(n))", "}")},
	// A length's upper bounds bound it, as a slice's bound and as an
	// operand, whatever other relations are in force.
	{"a length bounded above, into a literal", arith("w string",
		"if len(w) <= 3 {", "    print(\"abc\"[:len(w)])", "}")},
	{"a length bounded above, with another relation", arith("w string, i int64, j int64",
		"if len(w) <= 3 && i <= j {", "    print(\"abc\"[:len(w)])", "}")},
	{"a length bounded above, below a least length", arith("w string, t string",
		"if len(t) >= 3 && len(w) <= 3 {", "    print(t[:len(w)])", "}")},
	{"a length bounded above, related elsewhere too", arith("w string, t string, u string",
		"if len(t) >= 3 && len(w) <= 3 && len(w) <= len(u) {", "    print(t[:len(w)])", "}")},
	{"a length related to a known string's", arith("w string",
		"t := \"abc\"", "if len(w) <= len(t) {", "    print(t[:len(w)])", "}")},
	{"a length related to an array's", arith("w string",
		"a := "+zeros(3), "if len(w) <= len(a) {", "    return total(a[:len(w)])", "}") +
		"\nfunc total(v []int64) int64 {\n    return 0\n}\n"},
	{"a length bounded above as a slice's low bound", arith("s string, t string",
		"if len(s) <= 3 && len(t) >= 3 {", "    print(t[len(s):3])", "}")},
	{
		"a length bounded above by a uint64",
		arith(
			"s string",
			"if len(s) <= uint64(18446744073709551615) && uint64(len(s) - 0) == 0 {",
			"    return 1",
			"}",
		),
	},
	{"a length checked within a known string's, under a looser check", arith("w string",
		"if len(w) <= 4 && len(w) <= 3 {", "    print(\"abc\"[:len(w)])", "}")},
	{"a length bounded below and checked within a known string's", arith("w string",
		"if len(w) >= 1 && len(w) <= 3 {", "    print(\"abc\"[:len(w)])", "}")},
	{"lengths in order, the high one guarded inside", arith("s string, t string, u string",
		"if len(s) <= len(t) {", "    if len(u) < 5 && len(u) <= len(t) && len(s) <= len(u) {",
		"        print(t[len(s):len(u)])", "    }", "}")},
	{"lengths in order, slicing the low one's value", arith("s string, u string",
		"if len(u) <= len(s) {", "    if len(u) < 5 && len(s) <= len(u) {",
		"        print(s[len(s):len(u)])", "    }", "}")},
	{
		"a length bounded above in an outer guard",
		arith(
			"w string",
			"if len(w) < 5 && len(w) <= 3 {",
			"    if len(w) > 1 {",
			"        print(\"abc\"[:len(w)])",
			"    }",
			"}",
		),
	},
	{"a length plus one into uint8", arith("s string",
		"if len(s) <= 200 {", "    return int64(uint8(len(s) + 1))", "}")},
	{"lengths in order, checked", arith("t string, s string, u string",
		"if len(s) < 5 && len(u) < 5 && len(u) <= len(t) && len(s) <= len(u) {",
		"    print(t[len(s):len(u)])", "}")},
	{"a length below a list's after an append", arith("n int64",
		"mut l := make(list[int64], c_allocator)",
		"err := append(mut l, 1, 2, 3)",
		"if err.code == 0 && len(l) <= 3 {", "    print(\"abc\"[:len(l)])", "}")},
	{"a named known value", arith("i int64", "a := "+zeros(3), "k := 1",
		"if 0 <= i && i < 2 {", "    return a[i + k]", "}")},
	// A length alone is bounded as it is in arithmetic: whatever proves
	// len(s) + 0 or len(s) - 0 proves len(s).
	{"a length as an index, related", arith("s string, t string",
		"if len(s) < len(t) {", "    return int64(t[len(s)])", "}")},
	{"a length as a view's index, related", arith("s string, v []int64",
		"if len(s) < len(v) {", "    return v[len(s)]", "}")},
	{"a length as a list's index, related", arith("s string, n int64",
		"mut l := make(list[int64], c_allocator)", "if len(s) < len(l) {",
		"    return l[len(s)]", "}")},
	{"a length as an index, related through a variable", arith("s string, t string, n int64",
		"if len(s) < n && n <= len(t) {", "    return int64(t[len(s)])", "}")},
	{"a length as an array's index", arith("s string", "a := "+zeros(3),
		"if len(s) < 3 {", "    return a[len(s)]", "}")},
	{"a length as a known string's index", arith("s string",
		"if len(s) <= 2 {", "    return int64(\"abc\"[len(s)])", "}")},
	{"a length below another's less one, as an index", arith("s string, t string",
		"if len(s) < len(t) - 1 {", "    return int64(t[len(s)])", "}")},
	{"a length converted", arith("s string",
		"if len(s) <= 255 {", "    return int64(uint8(len(s)))", "}")},
	{"a length converted, related", arith("s string, n int64",
		"if len(s) <= n && n <= 255 {", "    return int64(uint8(len(s)))", "}")},
	{"a length converted to uint64", arith("s string", "x := uint64(len(s))")},
	{"a length converted to int8 at its top", arith("v []int64",
		"if len(v) < 128 {", "    return int64(int8(len(v)))", "}")},
	{"a length converted to unsafe_size_t", arith("s string",
		"if len(s) <= 4294967295 {", "    x := unsafe_size_t(len(s))", "}")},
	{"a field's length as an index", arithStruct("p Q", "a := "+zeros(3),
		"if len(p.s) < 3 {", "    return a[len(p.s)]", "}") +
		"\ntype Q struct {\n    s string\n}\n"},
	{"a length as a divisor, checked above zero", arith("s string",
		"if len(s) >= 1 {", "    return 10 / len(s)", "}")},
	{"a length as a slice's bound, related", arith("s string, t string",
		"if len(s) < len(t) {", "    print(t[len(s):len(s) + 1])", "}")},
}

// TestAcceptsProvenArithmetic checks each proven use gives no diagnostic.
func TestAcceptsProvenArithmetic(t *testing.T) {
	for _, tt := range provenArithmeticCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// unprovenArithmeticCases are uses of arithmetic that could overflow, or
// whose bound does not prove the use, with the error each gives for the
// use and the text it covers; an overflow is reported as well, before it
// (see unprovenArithmeticOverflows).
var unprovenArithmeticCases = []struct {
	name string
	src  string
	want spanned
}{
	{
		"int64 v + 1 wraps at the largest",
		arith("v int64", "if v >= 0 {", "    x := uint64(v + 1)", "}"),
		spanned{"converting int64 to uint64 can fail; use v, ok := uint64(...)", "uint64(v + 1)"},
	},
	{
		"int64 v * 2 + 1 one past the edge",
		arith("v int64",
			"if v >= 0 && v <= 4611686018427387904 {", "    x := uint64(v * 2 + 1)", "}"),
		spanned{
			"converting int64 to uint64 can fail; use v, ok := uint64(...)",
			"uint64(v * 2 + 1)",
		},
	},
	{
		"int64 negation of the least", arith("v int64", "if v < 0 {", "    x := uint64(-v)", "}"),
		spanned{"converting int64 to uint64 can fail; use v, ok := uint64(...)", "uint64(-v)"},
	},
	{
		"int32 x * 2 + 1 one past the edge",
		arith("x int32",
			"if 0 <= x && x <= 1073741824 {", "    y := uint32(x * 2 + 1)", "}"),
		spanned{
			"converting int32 to uint32 can fail; use v, ok := uint32(...)",
			"uint32(x * 2 + 1)",
		},
	},
	{
		"int32 x * 50000 wraps",
		arith("x int32",
			"if 0 <= x && x <= 50000 {", "    y := uint32(x * 50000)", "}"),
		spanned{
			"converting int32 to uint32 can fail; use v, ok := uint32(...)",
			"uint32(x * 50000)",
		},
	},
	{
		"int8 negation of the least", arith("b int8", "if b < 0 {", "    y := uint8(-b)", "}"),
		spanned{"converting int8 to uint8 can fail; use v, ok := uint8(...)", "uint8(-b)"},
	},
	{
		"int8 0 - b at the least", arith("b int8", "if b <= 0 {", "    y := uint8(0 - b)", "}"),
		spanned{"converting int8 to uint8 can fail; use v, ok := uint8(...)", "uint8(0 - b)"},
	},
	{
		"uint8 u * 2 wraps",
		arith("u uint8", "if u <= 128 {", "    return 100 / int64(u * 2 + 1)", "}"),
		spanned{"divisor may be zero; use q, ok := ... / ...", "int64(u * 2 + 1)"},
	},
	{
		"uint8 u + 1 wraps at the top only", arith("u uint8", "return 100 / int64(u + 1)"),
		spanned{"divisor may be zero; use q, ok := ... / ...", "int64(u + 1)"},
	},
	{
		"uint8 u * 2 + 1 past int8",
		arith("u uint8", "if u <= 64 {", "    y := int8(u * 2 + 1)", "}"),
		spanned{"converting uint8 to int8 can fail; use v, ok := int8(...)", "int8(u * 2 + 1)"},
	},
	{
		"uint64 u - 1 wraps at 0", arith("u uint64", "a := [4]int64{1, 2, 3, 4}", "if u <= 4 {",
			"    return a[u - 1]", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "u - 1"},
	},
	{
		"uint64 u * 2 wraps to 0",
		arith("u uint64", "if u > 0 && 100 / (u * 2) == 0 {", "    return 1", "}"),
		spanned{"divisor may be zero; use q, ok := ... / ...", "u * 2"},
	},
	{
		"int64 x + 1 wraps at the top only",
		arith("x int64", "if x >= 0 {", "    return 100 / (x + 1)", "}"),
		spanned{"divisor may be zero; use q, ok := ... / ...", "x + 1"},
	},
	{
		"mixed signs one past the end", arith("i int64, j int64", "a := "+zeros(18),
			"if -3 <= i && i <= 3 && -3 <= j && j <= 3 {", "    return a[i * j + 9]", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "i * j + 9"},
	},
	{
		"mixed signs past int64",
		arith("i int64, j int64",
			"if 0 <= i && i <= 3037000500 && -3037000500 <= j && j <= 0 {",
			"    x := uint64(-(i * j))", "}"),
		spanned{
			"converting int64 to uint64 can fail; use v, ok := uint64(...)",
			"uint64(-(i * j))",
		},
	},
	{
		"int8 b * 2 wraps before - 1",
		arith("b int8", "if 1 <= b && b <= 64 {", "    y := uint8(b * 2 - 1)", "}"),
		spanned{"converting int8 to uint8 can fail; use v, ok := uint8(...)", "uint8(b * 2 - 1)"},
	},
	{
		"two reads of one value are bounded apart",
		arith("u uint64", "if u <= 6148914691236517205 {",
			"    x := int64(u * 3 - u * 2)", "}"),
		spanned{
			"converting uint64 to int64 can fail; use v, ok := int64(...)",
			"int64(u * 3 - u * 2)",
		},
	},
	{
		"x * 0 as a divisor", arith("x int64", "return 100 / (x * 0)"),
		spanned{"divisor may be zero; use q, ok := ... / ...", "x * 0"},
	},
	{
		"x - x is not 0", arith("x int64", "a := [1]int64{7}", "if 0 <= x && x < 3 {",
			"    return a[x - x]", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "x - x"},
	},
	{
		"a sum of two may reach the end", arith("i int64, j int64", "a := "+zeros(6),
			"if 0 <= i && i < 4 && 0 <= j && j < 4 {", "    return a[i + j]", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "i + j"},
	},
	{
		"a fact ended by a write", arith("i int64", "mut j = i", "a := "+zeros(3),
			"if 0 <= j && j < 2 {", "    j = 5", "    return a[j + 1]", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "j + 1"},
	},
	{
		"a fact ended by a mut argument", arith("i int64", "mut j = i", "a := "+zeros(3),
			"if 0 <= j && j < 2 {", "    bump(mut j)", "    return a[j + 1]", "}") +
			"\nfunc bump(mut n int64) {\n    n = 9\n}\n",
		spanned{"index may be out of range; use v, ok := ...[...]", "j + 1"},
	},
	{
		"a field of a mutable variable", arithStruct("q P", "mut p = q", "a := "+zeros(7),
			"if 0 <= p.n && p.n <= 3 {", "    return a[p.n + 1]", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "p.n + 1"},
	},
	{
		"a mutable global",
		strings.Replace(arith("i int64", "a := "+zeros(3), "if 0 <= g && g < 2 {",
			"    return a[g + 1]", "}"), "package main\n", "package main\n\nmut g int64 = 1\n", 1),
		spanned{"index may be out of range; use v, ok := ...[...]", "g + 1"},
	},
	{
		"a relation through i + 1 that may wrap", arith("a []int64, i int64",
			"if 0 <= i && i + 1 < len(a) {", "    return a[i + 1]", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "i + 1"},
	},
	{
		"an int8 relation through b + 1 that may wrap", arith("a []int64, b int8",
			"if 0 <= b && b + 1 < len(a) {", "    return a[b + 1]", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "b + 1"},
	},
	{
		"slice bounds out of order", arith("s string, i int64",
			"if 0 <= i && i < len(s) {", "    print(s[i + 1 : i])", "}"),
		spanned{"slice bounds ...:i may be out of range; use v, ok := ...[...:i]", "i + 1 : i"},
	},
	{
		"a slice from i + 1 past the end", arith("s string, i int64",
			"if 0 <= i && i <= len(s) {", "    print(s[i + 1:])", "}"),
		spanned{"slice bound ... may be out of range; use v, ok := ...[...:]", "i + 1:"},
	},
	{
		"an unsafe_size_t sum past the portable largest",
		arith("n unsafe_size_t",
			"if n <= 4294967295 {", "    x := uint32(n + 1)", "}"),
		spanned{
			"converting unsafe_size_t to uint32 can fail; use v, ok := uint32(...)",
			"uint32(n + 1)",
		},
	},
	{
		"an unsafe_size_t difference with no upper bound",
		arith("n unsafe_size_t",
			"if n >= 1 {", "    x := uint32(n - 1)", "}"),
		spanned{
			"converting unsafe_size_t to uint32 can fail; use v, ok := uint32(...)",
			"uint32(n - 1)",
		},
	},
	{
		"a division inside", arith("i int64", "a := "+zeros(3), "if 0 <= i && i < 2 {",
			"    return a[i / 1 + 1]", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "i / 1 + 1"},
	},
	{
		"a call inside", arith("i int64", "a := "+zeros(3), "if 0 <= i && i < 2 {",
			"    return a[i + one()]", "}") + "\nfunc one() int64 {\n    return 1\n}\n",
		spanned{"index may be out of range; use v, ok := ...[...]", "i + one()"},
	},
	{
		"a conversion inside", arith("i int32", "a := "+zeros(3), "if 0 <= i && i < 2 {",
			"    return a[int64(i) + 1]", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "int64(i) + 1"},
	},
	{
		"lengths that may be out of order",
		arith(
			"t string, s string, u string",
			"if len(s) < 5 && len(u) < 5 && len(u) <= len(t) {",
			"    print(t[len(s):len(u)])",
			"}",
		),
		spanned{
			"slice bounds ...:... may be out of range; use v, ok := ...[...:...]",
			"len(s):len(u)",
		},
	},
	{
		"a length bounded above past a known length", arith("w string",
			"if len(w) <= 4 {", "    print(\"abc\"[:len(w)])", "}"),
		spanned{"slice bound ... may be out of range; use v, ok := ...[:...]", ":len(w)"},
	},
	{
		"a length plus one past uint8", arith("s string",
			"if len(s) <= 255 {", "    return int64(uint8(len(s) + 1))", "}"),
		spanned{"converting int64 to uint8 can fail; use v, ok := uint8(...)", "uint8(len(s) + 1)"},
	},
	{
		"a length bounded above, ended by an append", arith("t string, n int64",
			"mut l := make(list[int64], c_allocator)",
			"if len(l) <= 3 {",
			"    err := append(mut l, 1, 2, 3, 4)",
			"    if err.code == 0 {",
			"        print(\"abcd\"[:len(l)])",
			"    }",
			"}"),
		spanned{"slice bound ... may be out of range; use v, ok := ...[:...]", ":len(l)"},
	},
	{
		"a length bounded above, ended by an assignment", arith("t string, w string",
			"mut v := w",
			"if len(v) <= 3 {",
			"    v = t",
			"    print(\"abc\"[:len(v)])",
			"}"),
		spanned{"slice bound ... may be out of range; use v, ok := ...[:...]", ":len(v)"},
	},
	{
		"a length bounded above, ended in a loop", arith("t string, w string",
			"mut v := w",
			"if len(v) <= 3 {",
			"    for range 2 {",
			"        print(\"abc\"[:len(v)])",
			"        v = t",
			"    }",
			"}"),
		spanned{"slice bound ... may be out of range; use v, ok := ...[:...]", ":len(v)"},
	},
	{
		"a length past a slice's end", arith("t string, s string",
			"if len(s) < 5 {", "    print(t[:len(s)])", "}"),
		spanned{"slice bound ... may be out of range; use v, ok := ...[:...]", ":len(s)"},
	},
	{
		"a view's last element, unguarded", arith("v []int64", "return v[len(v) - 1]"),
		spanned{"index may be out of range; use v, ok := ...[...]", "len(v) - 1"},
	},
	{
		"a string's last byte, unguarded", arith("s string", "return int64(s[len(s) - 1])"),
		spanned{"index may be out of range; use v, ok := ...[...]", "len(s) - 1"},
	},
	{
		"one past a known string", arith("i int64, j int64", "s := \"abc\"",
			"if 0 <= i && i <= j && j <= 2 {", "    return int64(s[i + 1])", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "i + 1"},
	},
	{
		"one past a string literal", arith("i int64, j int64",
			"if 0 <= i && i <= j && j <= 2 {", "    return int64(\"abc\"[i + 1])", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "i + 1"},
	},
	{
		"a length's top is the largest int64", arith("s string", "a := "+zeros(2),
			"if len(s) >= 2147483647 {", "    return a[len(s) - 2147483647]", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "len(s) - 2147483647"},
	},
	{
		"a length plus one may wrap",
		arith("s string",
			"if uint64(len(s) + 1) == 0 {", "    return 1", "}"),
		spanned{
			"converting int64 to uint64 can fail; use v, ok := uint64(...)",
			"uint64(len(s) + 1)",
		},
	},
	{
		"an index at the least length", arith("v []int64, i int64",
			"if len(v) >= 3 && 0 <= i && i <= 1 {", "    return v[i + 2]", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "i + 2"},
	},
	{
		"a subject at the least length",
		arith("v []int64, i int64",
			"if len(v) >= 3 && 0 <= i && i <= 3 {", "    return v[i]", "}"),
		spanned{
			"index i may be out of range; check 0 <= i && i < len(v) first, or use v, ok := ...[i]",
			"i",
		},
	},
	{
		"a complement is no negation", arith("i int64", "a := "+zeros(4),
			"if -3 <= i && i <= 0 {", "    return a[^i]", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "^i"},
	},
	{
		"a call times 0", arith("i int64", "a := "+zeros(1), "return a[one() * 0]") +
			"\nfunc one() int64 {\n    return 1\n}\n",
		spanned{"index may be out of range; use v, ok := ...[...]", "one() * 0"},
	},
	{
		"a call's type range", arith("i int64", "a := "+zeros(256), "return a[byte8()]") +
			"\nfunc byte8() uint8 {\n    return 7\n}\n",
		spanned{"index may be out of range; use v, ok := ...[...]", "byte8()"},
	},
	{
		"a product that may be zero", arith("x int64, y int64",
			"if x != 0 && -5 < x && x < 5 && -5 < y && y < 5 {", "    return 100 / (x * y)", "}"),
		spanned{"divisor may be zero; use q, ok := ... / ...", "x * y"},
	},
	{
		"a negation that may be zero", arith("x int64",
			"if -5 < x && x < 2 {", "    return 100 / -x", "}"),
		spanned{"divisor may be zero; use q, ok := ... / ...", "-x"},
	},
	{
		"an int8 chain through b + 1 that may wrap", arith("a []int64, b int8, n int64",
			"if 0 <= b && b < n && n <= len(a) {", "    return a[b + 1]", "}"),
		spanned{"index may be out of range; use v, ok := ...[...]", "b + 1"},
	},
	{
		"an int8 relation through b + 1 that may wrap, converted", arith("b int8, n int8",
			"if 0 <= n && n <= b {", "    return int64(uint8(b + 1))", "}"),
		spanned{"converting int8 to uint8 can fail; use v, ok := uint8(...)", "uint8(b + 1)"},
	},
}

// unprovenArithmeticOverflows holds, for each of unprovenArithmeticCases
// whose arithmetic may overflow, the errors reported for that arithmetic
// before the one the case is for: a plain operation that could wrap is
// itself an error now, which the case's use, not proven either, follows.
var unprovenArithmeticOverflows = map[string][]spanned{
	"int64 v + 1 wraps at the largest":                        {{"v + 1 may overflow int64; bound v first, or use v, ok := v + 1, or v +% 1 to wrap", "v + 1"}},
	"int64 v * 2 + 1 one past the edge":                       {{"v * 2 may overflow int64; bound v first, or use v, ok := v * 2, or v *% 2 to wrap", "v * 2"}},
	"int64 negation of the least":                             {{"-v may overflow int64; check v > -9223372036854775808 first, or use v, ok := -v, or -%v to wrap", "-v"}},
	"int32 x * 2 + 1 one past the edge":                       {{"x * 2 may overflow int32; bound x first, or use v, ok := x * 2, or x *% 2 to wrap", "x * 2"}},
	"int32 x * 50000 wraps":                                   {{"x * 50000 may overflow int32; bound x first, or use v, ok := x * 50000, or x *% 50000 to wrap", "x * 50000"}},
	"int8 negation of the least":                              {{"-b may overflow int8; check b > -128 first, or use v, ok := -b, or -%b to wrap", "-b"}},
	"int8 0 - b at the least":                                 {{"0 - b may overflow int8; bound b first, or use v, ok := 0 - b, or 0 -% b to wrap", "0 - b"}},
	"uint8 u * 2 wraps":                                       {{"u * 2 may overflow uint8; bound u first, or use v, ok := u * 2, or u *% 2 to wrap", "u * 2"}},
	"uint8 u + 1 wraps at the top only":                       {{"u + 1 may overflow uint8; bound u first, or use v, ok := u + 1, or u +% 1 to wrap", "u + 1"}},
	"uint64 u - 1 wraps at 0":                                 {{"u - 1 may go below 0 in uint64; check u >= 1 first, or use v, ok := u - 1, or u -% 1 to wrap", "u - 1"}},
	"uint64 u * 2 wraps to 0":                                 {{"u * 2 may overflow uint64; bound u first, or use v, ok := u * 2, or u *% 2 to wrap", "u * 2"}},
	"int64 x + 1 wraps at the top only":                       {{"x + 1 may overflow int64; bound x first, or use v, ok := x + 1, or x +% 1 to wrap", "x + 1"}},
	"mixed signs past int64":                                  {{"i * j may overflow int64; bound i and j first, or use v, ok := i * j, or i *% j to wrap", "i * j"}},
	"int8 b * 2 wraps before - 1":                             {{"b * 2 may overflow int8; bound b first, or use v, ok := b * 2, or b *% 2 to wrap", "b * 2"}},
	"two reads of one value are bounded apart":                {{"u * 3 - u * 2 may go below 0 in uint64; use v, ok := u * 3 - u * 2, or u * 3 -% u * 2 to wrap", "u * 3 - u * 2"}},
	"a fact ended by a mut argument":                          {{"j + 1 may overflow int64; bound j first, or use v, ok := j + 1, or j +% 1 to wrap", "j + 1"}},
	"a field of a mutable variable":                           {{"p.n + 1 may overflow int64; use v, ok := p.n + 1, or p.n +% 1 to wrap", "p.n + 1"}},
	"a mutable global":                                        {{"g + 1 may overflow int64; use v, ok := g + 1, or g +% 1 to wrap", "g + 1"}},
	"a relation through i + 1 that may wrap":                  {{"i + 1 may overflow int64; bound i first, or use v, ok := i + 1, or i +% 1 to wrap", "i + 1"}, {"i + 1 may overflow int64; bound i first, or use v, ok := i + 1, or i +% 1 to wrap", "i + 1"}},
	"an int8 relation through b + 1 that may wrap":            {{"b + 1 may overflow int8; bound b first, or use v, ok := b + 1, or b +% 1 to wrap", "b + 1"}, {"b + 1 may overflow int8; bound b first, or use v, ok := b + 1, or b +% 1 to wrap", "b + 1"}},
	"a slice from i + 1 past the end":                         {{"i + 1 may overflow int64; bound i first, or use v, ok := i + 1, or i +% 1 to wrap", "i + 1"}},
	"an unsafe_size_t sum past the portable largest":          {{"n + 1 may overflow unsafe_size_t; bound n first, or use v, ok := n + 1, or n +% 1 to wrap", "n + 1"}},
	"a call inside":                                           {{"i + one() may overflow int64; bound i first, or use v, ok := i + one(), or i +% one() to wrap", "i + one()"}},
	"a length plus one may wrap":                              {{"len(s) + 1 may overflow int64; use v, ok := len(s) + 1, or len(s) +% 1 to wrap", "len(s) + 1"}},
	"an int8 chain through b + 1 that may wrap":               {{"b + 1 may overflow int8; bound b first, or use v, ok := b + 1, or b +% 1 to wrap", "b + 1"}},
	"an int8 relation through b + 1 that may wrap, converted": {{"b + 1 may overflow int8; bound b first, or use v, ok := b + 1, or b +% 1 to wrap", "b + 1"}},
}

// TestRejectsUnprovenArithmetic checks each unproven use is one error,
// covering the use, after the errors for any arithmetic in or before it
// that may overflow (see unprovenArithmeticOverflows), which are reported
// as that arithmetic is checked, before the use around it.
func TestRejectsUnprovenArithmetic(t *testing.T) {
	for _, tt := range unprovenArithmeticCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			want := append(slices.Clone(unprovenArithmeticOverflows[tt.name]), tt.want)
			if len(diags) != len(want) {
				t.Fatalf("got %v, want %v", diags, want)
			}
			for i, w := range want {
				if diags[i].Message != w.msg {
					t.Errorf("message %d = %q, want %q", i, diags[i].Message, w.msg)
				}
				if got := tt.src[diags[i].Span.Start:diags[i].Span.End]; got != w.at {
					t.Errorf("diagnostic %d covers %q, want %q", i, got, w.at)
				}
			}
		})
	}
}

// overflowOnlyCases are uses of arithmetic that may overflow, whose use
// is in range whatever the value: a uint8 index into 256 elements. The
// arithmetic is the one error, covering it.
var overflowOnlyCases = []struct {
	name string
	src  string
	want spanned
}{
	{
		"a uint8 index past its largest into 256 elements",
		arith("u uint8", "a := "+zeros(256), "return a[u + 1]"),
		spanned{"u + 1 may overflow uint8; bound u first, or use v, ok := u + 1, or u +% 1 to wrap", "u + 1"},
	},
	{
		"a uint8 index below 0 into 256 elements",
		arith("u uint8", "a := "+zeros(256), "return a[u - 1]"),
		spanned{
			"u - 1 may go below 0 in uint8; check u >= 1 first, or use v, ok := u - 1, or u -% 1 to wrap",
			"u - 1",
		},
	},
}

// TestOverflowStandsAlone checks each of overflowOnlyCases is one error,
// covering the arithmetic.
func TestOverflowStandsAlone(t *testing.T) {
	for _, tt := range overflowOnlyCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != 1 || diags[0].Message != tt.want.msg {
				t.Fatalf("got %v, want only %q", diags, tt.want.msg)
			}
			if got := tt.src[diags[0].Span.Start:diags[0].Span.End]; got != tt.want.at {
				t.Errorf("diagnostic covers %q, want %q", got, tt.want.at)
			}
		})
	}
}

// deepArithmetic returns f indexing a with i plus 1 less 1, n times
// over, under 0 <= i && i < 3, then with i negated 2n times, and dividing
// by the first plus 1, so that every bound reads at least n operators;
// the elements and the quotient are summed with +%, which needs no bound.
func deepArithmetic(n int) string {
	sum, neg := "i", "i"
	for range n {
		sum += " + 1 - 1"
		neg = "-(-(" + neg + "))"
	}
	return arith("i int64", "a := "+zeros(3), "if 0 <= i && i < 3 {",
		fmt.Sprintf("    return a[%s] +%% a[%s] +%% 6 / (%s + 1)", sum, neg, sum), "}")
}

// TestDeepArithmeticBoundsQuickly checks that bounding arithmetic nested
// thousands deep is proven, and reads each operator a bounded number of
// times, so the steps grow linearly with the depth (see checkGrowth).
func TestDeepArithmeticBoundsQuickly(t *testing.T) {
	const n = 60
	diags, small := checkWork(t, deepArithmetic(n))
	if len(diags) > 0 {
		t.Fatalf("got %v, want no diagnostics", diags)
	}
	_, grown := checkWork(t, deepArithmetic(4*n))
	checkGrowth(t, "arithmetic bounds read", small.Ranged, grown.Ranged)
	checkGrowth(t, "known arithmetic worked out", small.Arithmetic, grown.Arithmetic)
}
