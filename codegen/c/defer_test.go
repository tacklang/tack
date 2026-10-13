package c_test

import (
	"strconv"
	"strings"
	"testing"
)

// printer declares p, which prints its argument's digit, so a program can
// show the order things run in.
const printer = "func p(d int64) {\n    s := [10]string{\"0\", \"1\", \"2\", \"3\", \"4\", \"5\", \"6\", \"7\", \"8\", \"9\"}\n" +
	"    if d >= 0 && d < 10 {\n        print(s[d])\n    }\n}\n"

// deferRuntimeCases are programs with defers, built warning-free, and what
// each must print and exit with.
var deferRuntimeCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"main's defers run when it ends",
		inMainProgram("", "defer print(\"b\")", "print(\"a\")"),
		"ab", 0,
	},
	{
		"exit runs no defer",
		inMainProgram("", "defer print(\"b\")", "print(\"a\")", "exit(3)"),
		"a", 3,
	},
	{
		"a defer may exit",
		inMainProgram("", "defer exit(4)", "defer print(\"b\")", "print(\"a\")"),
		"ab", 4,
	},
	{
		"an empty deferred block",
		inMainProgram("", "defer {}", "defer {", "}", "exit(5)"),
		"", 5,
	},
	{
		"a deferred multi-assign",
		inMainProgram("func two() (int64, int64) {\n    return 2, 4\n}\n",
			"mut a := 0", "mut b := 0",
			"if true {", "    defer a, b = two()", "}", "exit(a + b)"),
		"", 6,
	},
	{
		"a deferred block's variable and a later one share a name",
		inMainProgram("",
			"defer {", "    x := 3", "    exit(x)", "}",
			"x := 4", "if x == 5 {", "    exit(1)", "}"),
		"", 3,
	},
	{
		"a returned string is the value before the defers",
		inMainProgram(
			"func f() string {\n    mut s := \"old\"\n    defer s = \"new\"\n    defer print(s)\n    return s\n}\n",
			"print(f())",
			"exit(7)",
		),
		"oldold",
		7,
	},
	{
		"a returned array and struct are copies",
		inMainProgram("type P struct {\n    x int64\n}\n"+
			"func f() ([2]int64, P) {\n    mut a := [2]int64{1, 2}\n    mut q := P{x: 3}\n"+
			"    defer {\n        a[0] = 9\n        q.x = 9\n    }\n    return a, q\n}\n",
			"a, q := f()", "exit(a[0] + a[1] + q.x + 2)"),
		"", 8,
	},
	{
		"a bare return runs the defers",
		inMainProgram(
			"func f(n int64) {\n    defer print(\"x\")\n    if n > 0 {\n        defer print(\"y\")\n        return\n    }\n    print(\"z\")\n}\n",
			"f(1)",
			"f(0)",
			"exit(9)",
		),
		"yxzx",
		9,
	},
	{
		"a closed block's defers are not run again",
		inMainProgram(
			"func f() int64 {\n    if true {\n        defer print(\"a\")\n    }\n    return 1\n}\n",
			"exit(f() + 9)",
		),
		"a",
		10,
	},
	{
		"continue runs defers before a multi-assign post",
		inMainProgram(
			printer+"func two(a int64, b int64) (int64, int64) {\n    return a + 1, b + 2\n}\n",
			"mut i := 0",
			"mut j := 0",
			"for ; i < 3; i, j = two(i, j) {",
			"    defer p(i)",
			"    if i == 1 {",
			"        continue",
			"    }",
			"    defer p(j)",
			"}",
			"exit(i + j)",
		),
		"00142",
		9,
	},
	{
		"break in a case runs the case's and the body's defers",
		inMainProgram(printer,
			"for i := range 5 {",
			"    defer p(1)",
			"    switch i {",
			"    case 2:",
			"        defer p(2)",
			"        break",
			"    default:",
			"        defer p(3)",
			"        continue",
			"    }",
			"}",
			"exit(11)"),
		"313121", 11,
	},
	{
		"range over a string with a deferred index",
		inMainProgram(printer, "for i, c := range \"abc\" {", "    defer p(i)",
			"    if c == 'b' {", "        break", "    }", "}", "exit(12)"),
		"01", 12,
	},
	{
		"a deferred loop with its own defers",
		inMainProgram(printer,
			"defer {",
			"    for i := range 3 {",
			"        defer p(i)",
			"        if i == 1 {",
			"            continue",
			"        }",
			"        defer {",
			"            defer p(8)",
			"            p(9)",
			"        }",
			"    }",
			"}"),
		"9801982", 0,
	},
	{
		"a deferred division proven by the if around it",
		inMainProgram(printer, "d := 3", "if d > 0 {", "    defer p(9 / d)", "}", "exit(13)"),
		"3", 13,
	},
}

// TestDefersAtRuntime builds each program warning-free and checks what it
// does.
func TestDefersAtRuntime(t *testing.T) {
	for _, tt := range deferRuntimeCases {
		t.Run(tt.name, func(t *testing.T) {
			stdout, status := run(t, tt.src)
			if stdout != tt.stdout || status != tt.status {
				t.Errorf(
					"got stdout %q, status %d; want %q, %d",
					stdout,
					status,
					tt.stdout,
					tt.status,
				)
			}
		})
	}
}

// TestManyDefersRunLastFirst builds a block of three hundred defers and
// checks they run in reverse.
func TestManyDefersRunLastFirst(t *testing.T) {
	const n = 300
	lines := make([]string, 0, n+1)
	var want strings.Builder
	for i := range n {
		lines = append(lines, "defer p("+strconv.Itoa(i%10)+")")
	}
	for i := n - 1; i >= 0; i-- {
		want.WriteString(strconv.Itoa(i % 10))
	}
	if stdout, _ := run(t, inMainProgram(printer, lines...)); stdout != want.String() {
		t.Errorf("printed %q, want %q", stdout, want.String())
	}
}

// TestDeepDefersUnwindInnermostFirst returns from fifty nested blocks,
// each with a defer, and checks every one runs, innermost first; then
// nests deferred blocks fifty deep, each deferring the next.
func TestDeepDefersUnwindInnermostFirst(t *testing.T) {
	const depth = 50
	var body, want strings.Builder
	for i := range depth {
		body.WriteString("if true {\ndefer p(" + strconv.Itoa(i%10) + ")\n")
	}
	body.WriteString("return\n" + strings.Repeat("}\n", depth))
	for i := depth - 1; i >= 0; i-- {
		want.WriteString(strconv.Itoa(i % 10))
	}
	fn := "func f() {\n" + body.String() + "}\n"
	if stdout, _ := run(t, inMainProgram(printer+fn, "f()")); stdout != want.String() {
		t.Errorf("return printed %q, want %q", stdout, want.String())
	}

	nested := strings.Repeat("defer {\np(1)\n", depth) + strings.Repeat("}\n", depth)
	if stdout, _ := run(t, inMainProgram(printer, nested)); stdout != strings.Repeat("1", depth) {
		t.Errorf("nested defers printed %q, want %d ones", stdout, depth)
	}
}

// TestDeferEmitsNoFlags checks a defer emits nothing where it is written,
// no reached flag, and that a return with no defer pending keeps its plain
// form.
func TestDeferEmitsNoFlags(t *testing.T) {
	out := generate(t, inMainProgram(
		"func f(n int64) int64 {\n    if n > 0 {\n        defer exit(2)\n    }\n    return n\n}\n",
		"exit(f(1))"))
	if !strings.Contains(out, "return p_n;") {
		t.Errorf("return with no defer pending was not plain:\n%s", out)
	}
	if strings.Count(out, "exit(2)") != 1 {
		t.Errorf("defer copied %d times, want once:\n%s", strings.Count(out, "exit(2)"), out)
	}
	if strings.Contains(out, "bool tack_") {
		t.Errorf("generated a flag:\n%s", out)
	}
}
