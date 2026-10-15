package c_test

import (
	"regexp"
	"strings"
	"testing"
)

// orderPrelude declares say, which prints its name and returns its value,
// and the helpers the order cases call.
const orderPrelude = "package main\n\n" +
	"type P struct {\n    a int64\n    b int64\n}\n\n" +
	"mut g int64\n\n" +
	"func say(s string, n int64) int64 {\n    print(s)\n    return n\n}\n\n" +
	"func bump() int64 {\n    g++\n    return g\n}\n\n" +
	"func two(a int64, b int64) int64 {\n    return a * 10 + b\n}\n\n" +
	"func pair() (int64, int64) {\n    return say(\"a\", 1), say(\"b\", 2)\n}\n\n" +
	"func keep() {\n    if g > 100 {\n        a, b := pair()\n        code, _ := int32(two(a, b) + bump())\n        exit(code)\n    }\n}\n\n"

// orderProgram is orderPrelude and a main holding lines, after a call to
// keep, which does nothing but use every helper, so the strict build does
// not reject one as unused.
func orderProgram(lines ...string) string {
	return orderPrelude + "func main() {\n    keep()\n    " + strings.Join(
		lines,
		"\n    ",
	) + "\n}\n"
}

// orderCases are programs whose operands print as they are evaluated, with
// what each must print, always left to right, and the status it must exit
// with, which checks the values too.
var orderCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"binary operands",
		orderProgram(
			"code, _ := int32(say(\"a\", 1) - say(\"b\", 2) * say(\"c\", 3))",
			"exit(code)",
		),
		"abc",
		251,
	},
	{"comparison", orderProgram("if say(\"a\", 1) < say(\"b\", 2) {", "    exit(1)", "}"), "ab", 1},
	{
		"arguments",
		orderProgram("code, _ := int32(two(say(\"a\", 1), say(\"b\", 2)))", "exit(code)"),
		"ab",
		12,
	},
	{
		"results",
		orderProgram("x, y := pair()", "code, _ := int32(two(x, y))", "exit(code)"),
		"ab",
		12,
	},
	{
		"array elements",
		orderProgram(
			"a := [2]int64{say(\"a\", 1), say(\"b\", 2)}",
			"code, _ := int32(two(a[0], a[1]))",
			"exit(code)",
		),
		"ab",
		12,
	},
	{
		"struct fields as written",
		orderProgram(
			"p := P{b: say(\"b\", 2), a: say(\"a\", 1)}",
			"code, _ := int32(two(p.a, p.b))",
			"exit(code)",
		),
		"ba",
		12,
	},
	{
		"struct equality",
		orderProgram(
			"if (P{a: say(\"a\", 1), b: 0}) == (P{a: say(\"b\", 1), b: 0}) {",
			"    exit(1)",
			"}",
		),
		"ab",
		1,
	},
	{
		"a global read before a call",
		orderProgram("code, _ := int32(g + bump())", "exit(code)"),
		"",
		1,
	},
	{
		"a call before a global read",
		orderProgram("code, _ := int32(bump() + g)", "exit(code)"),
		"",
		2,
	},
	{
		"a global argument before a call",
		orderProgram("code, _ := int32(two(g, bump()))", "exit(code)"),
		"",
		1,
	},
	{
		"compound assignment of a global",
		orderProgram("g = 5", "g += bump()", "code, _ := int32(g)", "exit(code)"),
		"",
		11,
	},
	{
		"in a loop condition",
		orderProgram(
			"mut n := 0",
			"for say(\"a\", 1) < say(\"b\", 2) && n < 2 {",
			"    n++",
			"}",
			"code, _ := int32(n)",
			"exit(code)",
		),
		"ababab",
		2,
	},
	{
		"on the right of &&",
		orderProgram(
			"if false && two(say(\"a\", 1), say(\"b\", 2)) == 12 {",
			"    exit(9)",
			"}",
			"exit(3)",
		),
		"",
		3,
	},
	{
		"in a return",
		orderPrelude + "func f() int64 {\n    return two(say(\"a\", 1), say(\"b\", 2))\n}\n\n" +
			"func main() {\n    keep()\n    code, _ := int32(f())\n    exit(code)\n}\n",
		"ab", 12,
	},
}

// TestOperandsRunLeftToRight builds each of orderCases strictly, with the
// undefined behavior sanitizer on, and checks what it prints and exits with.
func TestOperandsRunLeftToRight(t *testing.T) {
	for _, tt := range orderCases {
		t.Run(tt.name, func(t *testing.T) {
			out, status := runSanitized(t, tt.src)
			if out != tt.stdout || status != tt.status {
				t.Errorf("got stdout %q, status %d; want %q, %d", out, status, tt.stdout, tt.status)
			}
		})
	}
}

// orderTemp matches a temporary that holds an operand.
var orderTemp = regexp.MustCompile(`tack_o\d+`)

// TestHoldsOperandsOnlyWhenOrderShows checks operands go into temporaries
// only where their order could be seen: two that may call, or one that
// calls and one that reads a mutable global. Constants, locals and
// parameters are never held.
func TestHoldsOperandsOnlyWhenOrderShows(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		held  bool
	}{
		{
			"two calls",
			[]string{"code, _ := int32(say(\"a\", 1) + say(\"b\", 2))", "exit(code)"},
			true,
		},
		{
			"a call and a mutable global",
			[]string{"code, _ := int32(g + bump())", "exit(code)"},
			true,
		},
		{
			"a call and a local",
			[]string{"x := 1", "code, _ := int32(x + bump())", "exit(code)"},
			false,
		},
		{
			"a call and a constant",
			[]string{"code, _ := int32(two(1, bump()))", "exit(code)"},
			false,
		},
		{"two mutable globals", []string{"code, _ := int32(g + g)", "exit(code)"}, false},
		{"one call", []string{"code, _ := int32(bump())", "exit(code)"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generate(t, orderProgram(tt.lines...))
			_, main, found := strings.Cut(got, "static void tack_4_main4_main(void) {")
			if !found {
				t.Fatalf("no main in:\n%s", got)
			}
			if held := orderTemp.MatchString(main); held != tt.held {
				t.Errorf("held = %v, want %v:\n%s", held, tt.held, main)
			}
		})
	}
}
