package c_test

import (
	"testing"
)

// stringIndexCases are programs reading bytes of strings, proven in range or
// bound with ok, with what each must print and the status it must exit
// with.
var stringIndexCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"checked at every boundary",
		program(
			"s := \"abc\"",
			"mut i := 0",
			"v0, ok0 := s[i]",
			"i = 2",
			"v2, ok2 := s[i]",
			"i = 3",
			"v3, ok3 := s[i]",
			"i = -1",
			"vn, okn := s[i]",
			"if ok0 && v0 == 'a' && ok2 && v2 == 'c' && !ok3 && v3 == 0 && !okn && vn == 0 {",
			"    exit(7)",
			"}",
		),
		"",
		7,
	},
	{
		"checked at the extremes of int64",
		program(
			"s := \"abc\"",
			"mut i := 9223372036854775807",
			"_, high := s[i]",
			"i = -9223372036854775808",
			"_, low := s[i]",
			"if !high && !low {",
			"    exit(3)",
			"}",
		),
		"",
		3,
	},
	{
		"checked with narrow and wide index types",
		program(
			"s := \"abc\"",
			"mut u uint8 = 255",
			"mut b int8 = -128",
			"mut w uint64 = 18446744073709551615",
			"mut one uint64 = 1",
			"_, a := s[u]",
			"_, c := s[b]",
			"_, d := s[w]",
			"v, e := s[one]",
			"if !a && !c && !d && e && v == 'b' {",
			"    exit(4)",
			"}",
		),
		"",
		4,
	},
	{
		"checked on the empty string",
		program(
			"mut s := \"\"",
			"mut i := 0",
			"v, ok := s[i]",
			"_, zero := s[0]",
			"if !ok && !zero && v == 0 && len(s) == 0 {",
			"    exit(5)",
			"}",
		),
		"",
		5,
	},
	{
		"bytes of UTF-8",
		program(
			"s := \"é\\t\"",
			"if len(s) == 3 && s[0] == 195 && s[1] == 169 && s[2] == '\\t' && \"é\"[1] == 169 {",
			"    exit(6)",
			"}",
		),
		"",
		6,
	},
	{
		"string before index, each once, even out of range",
		"package main\n\nfunc str() string {\n    print(\"s\")\n    return \"ab\"\n}\n\n" +
			"func idx(i int64) int64 {\n    print(\"i\")\n    return i\n}\n\n" +
			"func main() {\n    v, ok := str()[idx(5)]\n    w, fine := str()[idx(1)]\n" +
			"    if !ok && v == 0 && fine {\n        exit(w)\n    }\n}\n",
		"sisi",
		'b',
	},
	{
		"sum by range",
		"package main\n\nfunc sum(s string) int64 {\n    mut n := 0\n    for i := range len(s) {\n" +
			"        n += s[i]\n    }\n    return n\n}\n\n" +
			"func main() {\n    if sum(\"\") == 0 && sum(\"abc\") == 294 {\n        exit(sum(\"\\n\\n\"))\n    }\n}\n",
		"",
		20,
	},
	{
		"guard on a mutable string reads its current value",
		program(
			"mut s := \"xy\"",
			"mut total := 0",
			"if len(s) > 1 {",
			"    total += s[1]",
			"}",
			"s = \"pq\"",
			"if len(s) > 1 {",
			"    total += s[1] - 'q'",
			"}",
			"s = \"\"",
			"if len(s) > 0 {",
			"    exit(1)",
			"}",
			"exit(total)",
		),
		"",
		'y',
	},
	{
		"early return guard",
		"package main\n\nfunc at(s string, i int64) uint8 {\n    if i < 0 || i >= len(s) {\n        return 0\n    }\n" +
			"    return s[i]\n}\n\nfunc main() {\n" +
			"    if at(\"ab\", -1) == 0 && at(\"ab\", 2) == 0 && at(\"\", 0) == 0 {\n        exit(at(\"ab\", 1))\n    }\n}\n",
		"",
		'b',
	},
	{
		"checked index as a loop post",
		program(
			"s := \"abcd\"",
			"mut i := 0",
			"mut v := 'a'",
			"mut ok := true",
			"mut n := 0",
			"for ; ok; v, ok = s[i] {",
			"    n += v",
			"    i++",
			"}",
			"exit(n - 300)",
		),
		// The first pass adds the 'a' v starts at, and the post reads from
		// s[1], so s[0] is never added.
		"",
		97 + 98 + 99 + 100 - 300,
	},
	{
		"string elements of an array",
		program(
			"names := [2]string{\"ab\", \"\"}",
			"mut i := 1",
			"v, ok := names[0][i]",
			"_, none := names[1][0]",
			"if ok && !none {",
			"    exit(v)",
			"}",
		),
		"",
		'b',
	},
	{
		"byte widens",
		program(
			"s := \"z\"",
			"if len(s) > 0 {",
			"    var n int64 = s[0]",
			"    var m uint16 = s[0]",
			"    exit(n + m - 200)",
			"}",
		),
		"",
		2*'z' - 200,
	},
}

// TestStringIndexesAtRuntime builds each program with clang's undefined
// behavior sanitizer set to stop it, and checks what each read gave.
func TestStringIndexesAtRuntime(t *testing.T) {
	for _, tt := range stringIndexCases {
		t.Run(tt.name, func(t *testing.T) {
			out, status := runSanitized(t, tt.src)
			if out != tt.stdout || status != tt.status {
				t.Errorf("got stdout %q, status %d; want %q, %d", out, status, tt.stdout, tt.status)
			}
		})
	}
}

// TestCheckedUint64IndexIsWarningFree builds, with clang's warnings as
// errors, a program reading a string parameter and a view parameter at
// checked uint64 indexes, in range, at the length and at the largest
// uint64, and checks each result. A uint64 index compared with a length
// read from a parameter, an int64_t, differs from it in sign, which
// -Wsign-compare rejects unless both sides are cast to uint64_t first.
func TestCheckedUint64IndexIsWarningFree(t *testing.T) {
	src := "package main\n\n" +
		"func inString(s string, in uint64, at uint64, top uint64) bool {\n" +
		"    v, ok := s[in]\n" +
		"    w, atLen := s[at]\n" +
		"    _, atTop := s[top]\n" +
		"    return ok && v == 'c' && !atLen && w == 0 && !atTop\n" +
		"}\n\n" +
		"func inView(v []int64, in uint64, at uint64, top uint64) bool {\n" +
		"    x, ok := v[in]\n" +
		"    y, atLen := v[at]\n" +
		"    _, atTop := v[top]\n" +
		"    return ok && x == 30 && !atLen && y == 0 && !atTop\n" +
		"}\n\n" +
		"func main() {\n" +
		"    a := [3]int64{10, 20, 30}\n" +
		"    mut status := 0\n" +
		"    if inString(\"abc\", 2, 3, 18446744073709551615) {\n" +
		"        status += 1\n" +
		"    }\n" +
		"    if inView(a, 2, 3, 18446744073709551615) {\n" +
		"        status += 2\n" +
		"    }\n" +
		"    exit(status)\n" +
		"}\n"
	if out, status := run(t, src); out != "" || status != 3 {
		t.Errorf("got stdout %q, status %d; want \"\", 3", out, status)
	}
}
