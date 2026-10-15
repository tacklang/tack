package c_test

import (
	"strings"
	"testing"
)

// multipleResultsCases are programs using several results, with what each
// must print and the status it must exit with.
var multipleResultsCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"in order",
		"package main\n\nfunc pair() (int64, int64) {\n    return 4, 7\n}\n\n" +
			"func main() {\n    a, b := pair()\n    code, _ := int32(a * 10 + b)\n    exit(code)\n}\n",
		"", 47,
	},
	{
		"call runs once",
		"package main\n\nfunc noisy() (int64, int64) {\n    print(\"call\")\n    return 1, 2\n}\n\n" +
			"func main() {\n    a, b := noisy()\n    code, _ := int32(a + b)\n    exit(code)\n}\n",
		"call",
		3,
	},
	{
		"discarded call still runs",
		"package main\n\nfunc noisy() (int64, int64) {\n    print(\"call\")\n    return 1, 2\n}\n\n" +
			"func main() {\n    noisy()\n    noisy()\n}\n",
		"callcall",
		0,
	},
	{
		"mixed types",
		"package main\n\nfunc three() (string, bool, int64) {\n    return \"text\", true, 9\n}\n\n" +
			"func main() {\n    s, b, n := three()\n    print(s)\n    if b {\n        code, _ := int32(n)\n        exit(code)\n    }\n}\n",
		"text",
		9,
	},
	{
		"mutable one",
		"package main\n\nfunc pair() (int64, int64) {\n    return 4, 7\n}\n\n" +
			"func main() {\n    a, mut b := pair()\n    b += a\n    b++\n    code, _ := int32(b)\n    exit(code)\n}\n",
		"", 12,
	},
	{
		"recursion",
		"package main\n\nfunc fib(n int64) (int64, int64) {\n    if n == 0 {\n        return 0, 1\n    }\n" +
			"    a, b := fib(n - 1)\n    return b, a + b\n}\n\n" +
			"func main() {\n    x, y := fib(10)\n    code, _ := int32(x + y)\n    exit(code)\n}\n",
		"",
		144,
	},
	{
		"declared after its caller",
		"package main\n\nfunc main() {\n    a, b := later(5)\n    code, _ := int32(a - b)\n    exit(code)\n}\n\n" +
			"func later(n int64) (int64, int64) {\n    return n * 3, n\n}\n",
		"",
		10,
	},
	{
		"loop init",
		"package main\n\nfunc pair() (int64, int64) {\n    return 2, 5\n}\n\n" +
			"func main() {\n    mut sum := 0\n    for step, mut n := pair(); n > 0; n-- {\n        sum += step\n    }\n    code, _ := int32(sum)\n    exit(code)\n}\n",
		"", 10,
	},
	{
		"extremes",
		"package main\n\nfunc ends() (int64, int64) {\n    return -9223372036854775808, 9223372036854775807\n}\n\n" +
			"func main() {\n    lo, hi := ends()\n    if lo + 1 == 0 - hi {\n        exit(8)\n    }\n}\n",
		"",
		8,
	},
	{
		"eight results",
		"package main\n\nfunc many() (int64, int64, int64, int64, int64, int64, int64, int64) {\n    return 1, 2, 3, 4, 5, 6, 7, 8\n}\n\n" +
			"func main() {\n    a, b, c, d, e, f, g, h := many()\n    code, _ := int32(a + b * 2 + c + d + e + f + g + h * 10)\n    exit(code)\n}\n",
		"",
		110,
	},
	{
		"names the C side uses",
		"package main\n\nfunc results() (int64, int64) {\n    return 1, 2\n}\n\n" +
			"func tack_r0() (int64, int64) {\n    return 3, 4\n}\n\n" +
			"func main() {\n    r0, r1 := results()\n    tack_r1, int := tack_r0()\n    code, _ := int32(r0 + r1 + tack_r1 + int)\n    exit(code)\n}\n",
		"", 10,
	},
	{
		"string results from both branches",
		"package main\n\nfunc sign(n int64) (string, int64) {\n    if n < 0 {\n        return \"neg\", -1\n    }\n    return \"pos\", 1\n}\n\n" +
			"func main() {\n    a, x := sign(-5)\n    b, y := sign(5)\n    print(a)\n    print(b)\n    code, _ := int32(x + y + 3)\n    exit(code)\n}\n",
		"negpos",
		3,
	},
}

// TestMultipleResultsAtRuntime builds each program strictly and checks what
// it prints and its exit status, so results arrive in order, of their own
// types, and from a call that runs exactly once.
func TestMultipleResultsAtRuntime(t *testing.T) {
	for _, tt := range multipleResultsCases {
		t.Run(tt.name, func(t *testing.T) {
			out, status := run(t, tt.src)
			if out != tt.stdout || status != tt.status {
				t.Errorf("got stdout %q, status %d; want %q, %d", out, status, tt.stdout, tt.status)
			}
		})
	}
}

// A function with several results returns them in one struct, and a MultiVar
// holds that struct in a const temporary so the call is made once.
func TestMultipleResultsLowering(t *testing.T) {
	got := generate(t, "package main\n\nfunc pair() (int64, bool) {\n    return 1, true\n}\n\n"+
		"func main() {\n    a, mut b := pair()\n    b = false\n    if b {\n        code, _ := int32(a)\n        exit(code)\n    }\n}\n")

	for _, want := range []string{
		"typedef struct {\n    int64_t r0;\n    bool r1;\n} tack_results_4_main4_pair;",
		"static tack_results_4_main4_pair tack_4_main4_pair(void);",
		"return (tack_results_4_main4_pair){1, true};",
		"const tack_results_4_main4_pair tack_r0 = tack_4_main4_pair();",
		"const int64_t v_a = tack_r0.r0;",
		"    bool v_b = tack_r0.r1;",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated C is missing %q:\n%s", want, got)
		}
	}
}

// A program with no function of several results defines no results struct.
func TestNoResultsStructWithoutMultipleResults(t *testing.T) {
	got := generate(
		t,
		"package main\n\nfunc one() int64 {\n    return 1\n}\n\nfunc main() {\n    code, _ := int32(one())\n    exit(code)\n}\n",
	)
	if strings.Contains(got, "tack_results_") {
		t.Errorf("a results struct was emitted:\n%s", got)
	}
}

// multiAssignCases are programs assigning several results at once, with what
// each must print and the status it must exit with.
var multiAssignCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"arguments read before any target is written",
		"package main\n\nfunc swap(a int64, b int64) (int64, int64) {\n    return b, a\n}\n\n" +
			"func main() {\n    mut x, mut y := swap(1, 2)\n    x, y = swap(x, y)\n    code, _ := int32(x * 10 + y)\n    exit(code)\n}\n",
		"", 12,
	},
	{
		"post runs the call once per iteration",
		"package main\n\nfunc step(n int64) (int64, int64) {\n    print(\"s\")\n    return n - 1, n\n}\n\n" +
			"func main() {\n    mut total := 0\n    for mut n, mut last := step(4); n > 0; n, last = step(n) {\n        total += last\n    }\n    code, _ := int32(total)\n    exit(code)\n}\n",
		"ssss",
		9,
	},
	{
		// even describes the n before the post ran, so it skips 1, 3, 5, 7
		// and 9 and counts the other four.
		"post with continue",
		"package main\n\nfunc next(n int64) (int64, bool) {\n    return n + 1, n % 2 == 0\n}\n\n" +
			"func main() {\n    mut odd := 0\n    for mut n, mut even := next(0); n < 10; n, even = next(n) {\n        if even {\n            continue\n        }\n        odd++\n    }\n    code, _ := int32(odd)\n    exit(code)\n}\n",
		"", 4,
	},
	{
		"globals and strings",
		"package main\n\nmut label string\n\nmut count int64\n\nfunc both() (string, int64) {\n    return \"set\", 3\n}\n\n" +
			"func main() {\n    label, count = both()\n    print(label)\n    code, _ := int32(count)\n    exit(code)\n}\n",
		"set",
		3,
	},
}

// TestMultiAssignAtRuntime builds each program strictly and checks what it
// prints and its exit status.
func TestMultiAssignAtRuntime(t *testing.T) {
	for _, tt := range multiAssignCases {
		t.Run(tt.name, func(t *testing.T) {
			out, status := run(t, tt.src)
			if out != tt.stdout || status != tt.status {
				t.Errorf("got stdout %q, status %d; want %q, %d", out, status, tt.stdout, tt.status)
			}
		})
	}
}

// A discarded result still comes from a call that runs once, including as a
// loop's post statement.
func TestBlankAtRuntime(t *testing.T) {
	out, status := run(
		t,
		"package main\n\nfunc noisy(n int64) (int64, int64) {\n    print(\"c\")\n    return n - 1, n\n}\n\n"+
			"func main() {\n    _, top := noisy(3)\n    mut n := top\n    for n > 0 {\n        n, _ = noisy(n)\n    }\n"+
			"    for mut i := 0; i < 2; i, _ = noisy(i + 2) {\n    }\n    code, _ := int32(top)\n    exit(code)\n}\n",
	)
	// One call to set top, three in the first loop, two as the post.
	if out != "cccccc" || status != 3 {
		t.Errorf("got stdout %q, status %d; want %q, 3", out, status, "cccccc")
	}
}
