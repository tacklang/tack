package c_test

import (
	"fmt"
	"strings"
	"testing"
)

// comparedStrings are strings whose every ordered pair is compared at run
// time: empty, prefixes of each other, embedded zero bytes, and bytes from
// 0x80 up, which compare above every ASCII byte only as unsigned bytes.
// None holds a quote, a backslash or a newline, so each is written into
// Tack source as it is.
var comparedStrings = []string{
	"", "a", "ab", "abc", "abd", "b", "z",
	"\x00", "a\x00", "a\x00b", "a\x00c", "a\x00\x00",
	"\x7f", "\x80", "\xff", "\xff\x00", "é",
}

// expectedBits gives the six comparisons of a and b as Go makes them, one
// bit each, in the order compareBits in the Tack program sets them.
func expectedBits(a, b string) int {
	bits := 0
	for i, holds := range []bool{a == b, a != b, a < b, a <= b, a > b, a >= b} {
		if holds {
			bits |= 1 << i
		}
	}
	return bits
}

// TestStringComparisonsMatchGo compares every ordered pair of
// comparedStrings with all six operators, in a function that takes them as
// parameters, so nothing is known at compile time, and checks each answer
// against Go's, which orders strings by unsigned bytes. The program is
// built with the undefined behavior sanitizer set to stop it, and exits
// with the number of the first pair that disagrees.
func TestStringComparisonsMatchGo(t *testing.T) {
	var src strings.Builder
	src.WriteString("package main\n\n")
	src.WriteString(
		"func bit(b bool, n int64) int64 {\n    if b {\n        return n\n    }\n    return 0\n}\n\n",
	)
	src.WriteString("func compareBits(a string, b string) int64 {\n")
	src.WriteString("    return bit(a == b, 1) | bit(a != b, 2) | bit(a < b, 4) |\n")
	src.WriteString("        bit(a <= b, 8) | bit(a > b, 16) | bit(a >= b, 32)\n}\n\n")
	src.WriteString("func main() {\n")
	pair := 0
	for _, a := range comparedStrings {
		for _, b := range comparedStrings {
			pair++
			fmt.Fprintf(
				&src,
				"    if compareBits(\"%s\", \"%s\") != %d {\n        exit(%d)\n    }\n",
				a,
				b,
				expectedBits(a, b),
				pair%250+1,
			)
		}
	}
	src.WriteString("    exit(0)\n}\n")

	out, status := runSanitized(t, src.String())
	if out != "" || status != 0 {
		t.Errorf(
			"got stdout %q, status %d; a pair numbered %d (mod 250) compared wrong",
			out,
			status,
			status,
		)
	}
}

// stringCompareCases are programs comparing strings, directly or inside
// arrays and structs, with what each must print and the status it must
// exit with.
var stringCompareCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"zero string equals the empty literal",
		program(
			"mut z string",
			"if z == \"\" && !(z < \"\") && z <= \"\" && \"a\" > z {",
			"    exit(3)",
			"}",
		),
		"",
		3,
	},
	{
		"zero elements of an array of strings",
		program(
			"mut a [3]string",
			"a[1] = \"x\"",
			"if a == [3]string{\"\", \"x\", \"\"} && a != [3]string{\"\", \"x\", \"y\"} && a[0] < a[1] {",
			"    exit(4)",
			"}",
		),
		"",
		4,
	},
	{
		"struct with string fields",
		structProgram(
			"type P struct {\n    first string\n    last string\n    n int8\n}",
			"mut p P",
			"q := P{first: \"a\", last: \"\", n: 0}",
			"if p == q {",
			"    exit(1)",
			"}",
			"p.first = \"a\"",
			"if p != q || p.first > q.first {",
			"    exit(2)",
			"}",
			"exit(5)",
		),
		"",
		5,
	},
	{
		"array of structs holding arrays of strings",
		structProgram(
			"type Tag struct {\n    names [2]string\n}",
			"a := [2]Tag{{names: [2]string{\"a\", \"\"}}, {names: [2]string{\"b\", \"c\"}}}",
			"mut b := a",
			"same := a == b",
			"b[1].names[1] = \"c\x00\"",
			"if same && a != b && a[1].names[1] < b[1].names[1] {",
			"    exit(6)",
			"}",
		),
		"",
		6,
	},
	{
		"compared in a loop condition",
		program(
			"mut w := \"apple\"",
			"mut n := 0",
			"for w < \"b\" {",
			"    n +%= 1",
			"    if n == 1 {",
			"        w = \"apricot\"",
			"    } else {",
			"        w = \"banana\"",
			"    }",
			"}",
			"code, fits := int32(n)",
			"if fits { exit(code) }",
		),
		"",
		2,
	},
}

// TestStringComparisonsAtRuntime builds each program with clang's undefined
// behavior sanitizer set to stop it, and checks what it prints and its
// exit status.
func TestStringComparisonsAtRuntime(t *testing.T) {
	for _, tt := range stringCompareCases {
		t.Run(tt.name, func(t *testing.T) {
			out, status := runSanitized(t, tt.src)
			if out != tt.stdout || status != tt.status {
				t.Errorf("got stdout %q, status %d; want %q, %d", out, status, tt.stdout, tt.status)
			}
		})
	}
}

// A comparison of two strings calls the comparison helper and compares
// its result with zero, with the operator it was written with.
func TestStringComparisonLowering(t *testing.T) {
	got := generate(t, program("s := \"a\"", "if s <= \"b\" {", "    exit(1)", "}"))
	for _, want := range []string{
		"static int tack_compare_strings(tack_string a, tack_string b) {",
		"(tack_compare_strings(v_s, (tack_string){",
		") <= 0)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated C is missing %q:\n%s", want, got)
		}
	}
}

// The helper is emitted only for a program that compares strings, so an
// unused static function never reaches the C compiler; comparing a struct
// that holds a string counts, since its equality helper calls it.
func TestStringCompareHelperOnlyWhenUsed(t *testing.T) {
	const helper = "tack_compare_strings"
	if got := generate(
		t,
		program("s := \"a\"", "print(s)", "if 1 < 2 {", "}"),
	); strings.Contains(
		got,
		helper,
	) {
		t.Errorf("helper emitted for a program that compares no strings:\n%s", got)
	}
	got := generate(t, structProgram(
		"type N struct {\n    s string\n}",
		"n := N{s: \"a\"}",
		"if n == n {",
		"    exit(1)",
		"}",
	))
	if !strings.Contains(got, "(tack_compare_strings(a->f_s, b->f_s) == 0)") {
		t.Errorf("struct equality does not compare its string field by content:\n%s", got)
	}
}
