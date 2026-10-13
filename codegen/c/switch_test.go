package c_test

import (
	"strconv"
	"strings"
	"testing"
)

// inMainProgram wraps lines, one per statement, in a main function, after
// decls, the program's other declarations.
func inMainProgram(decls string, lines ...string) string {
	return "package main\n\n" + decls + "\nfunc main() {\n    " + strings.Join(
		lines,
		"\n    ",
	) + "\n}\n"
}

// switchRuntimeCases are programs with switches, built warning-free, and
// what each must print and exit with.
var switchRuntimeCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"empty switch still evaluates its tag",
		inMainProgram("func t() int64 {\n    print(\"t\")\n    return 1\n}\n",
			"switch t() {", "}", "switch {", "}", "exit(3)"),
		"t", 3,
	},
	{
		"only a default",
		inMainProgram("func t() int64 {\n    print(\"t\")\n    return 1\n}\n",
			"switch t() {", "default:", "    exit(4)", "}"),
		"t", 4,
	},
	{
		"tagless only a default",
		inMainProgram("", "switch {", "default:", "    exit(5)", "}"),
		"", 5,
	},
	{
		"struct tag no case compares",
		inMainProgram(
			"type P struct {\n    x int64\n}\n",
			"switch (P{x: 1}) {",
			"default:",
			"}",
			"exit(6)",
		),
		"",
		6,
	},
	{
		"struct with no fields always equal",
		inMainProgram(
			"type E struct {\n}\n",
			"mut e E",
			"switch e {",
			"case E{}:",
			"    exit(7)",
			"}",
			"exit(1)",
		),
		"",
		7,
	},
	{
		"strings with zero and high bytes",
		inMainProgram("",
			"s := \"a\x00b\"",
			"switch s {",
			"case \"a\":",
			"    exit(1)",
			"case \"a\x00c\", \"\xffb\":",
			"    exit(2)",
			"case \"a\x00b\":",
			"    exit(8)",
			"}",
			"exit(3)"),
		"", 8,
	},
	{
		"a single string case",
		inMainProgram("", "switch \"x\" {", "case \"x\":", "    exit(9)", "}", "exit(1)"),
		"", 9,
	},
	{
		"array of strings",
		inMainProgram("",
			"a := [2]string{\"p\", \"q\"}",
			"switch a {",
			"case [2]string{\"p\", \"r\"}:",
			"    exit(1)",
			"case [2]string{\"p\", \"q\"}:",
			"    exit(10)",
			"}"),
		"", 10,
	},
	{
		"extremes of the integer types",
		inMainProgram("",
			"var u uint64 = 18446744073709551615",
			"var m int64 = -9223372036854775808",
			"mut n = 0",
			"switch u {",
			"case 18446744073709551614:",
			"    exit(1)",
			"case 18446744073709551615:",
			"    n++",
			"}",
			"switch m {",
			"case 9223372036854775807:",
			"    exit(2)",
			"case -9223372036854775808:",
			"    n++",
			"}",
			"exit(n + 9)"),
		"", 11,
	},
	{
		"narrow tag, wide value",
		inMainProgram("",
			"var b int8 = -1",
			"var w int64 = -1",
			"switch b {",
			"case w:",
			"    exit(12)",
			"}",
			"exit(1)"),
		"", 12,
	},
	{
		"bool conditions",
		inMainProgram("",
			"b := false",
			"switch {",
			"case b:",
			"    exit(1)",
			"case !b && true, b:",
			"    exit(13)",
			"}"),
		"", 13,
	},
	{
		"break in a switch leaves the loop",
		inMainProgram("",
			"mut i = 0",
			"for {",
			"    i++",
			"    switch i {",
			"    case 7:",
			"        break",
			"    }",
			"    print(\".\")",
			"}",
			"exit(i + 7)"),
		"......", 14,
	},
	{
		"continue in a switch in a three-clause loop runs the post",
		inMainProgram("",
			"mut n = 0",
			"for mut i = 0; i < 10; i++ {",
			"    switch {",
			"    case i % 2 == 0:",
			"        continue",
			"    default:",
			"        n++",
			"    }",
			"}",
			"exit(n + 10)"),
		"", 15,
	},
	{
		"values stop at the first match",
		inMainProgram("func v(n int64) int64 {\n    print(\"v\")\n    return n\n}\n",
			"switch 2 {",
			"case v(1), v(2), v(3):",
			"    print(\"!\")",
			"case v(2):",
			"    exit(1)",
			"}",
			"exit(16)"),
		"vv!", 16,
	},
	{
		"default runs last wherever written",
		inMainProgram("func v(n int64) int64 {\n    print(\"v\")\n    return n\n}\n",
			"switch 5 {",
			"default:",
			"    print(\"d\")",
			"case v(1):",
			"    exit(1)",
			"case v(2), v(3):",
			"    exit(2)",
			"}",
			"exit(17)"),
		"vvvd", 17,
	},
}

// TestSwitchesAtRuntime builds each program warning-free and checks what it
// does.
func TestSwitchesAtRuntime(t *testing.T) {
	for _, tt := range switchRuntimeCases {
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

// TestSwitchLowersToIfs checks a switch never becomes a C switch, whose
// break would leave it rather than the loop, and that the tag is held in
// one temporary.
func TestSwitchLowersToIfs(t *testing.T) {
	out := generate(t, inMainProgram("",
		"for i := range 3 {",
		"    switch i {",
		"    case 1:",
		"        break",
		"    default:",
		"        continue",
		"    }",
		"}"))
	if strings.Contains(out, "switch") {
		t.Errorf("generated a C switch:\n%s", out)
	}
	if got := strings.Count(out, "= v_i;"); got != 1 {
		t.Errorf("tag read %d times, want once:\n%s", got, out)
	}
}

// TestDeepAndWideSwitchesAtRuntime builds a switch nested fifty deep and
// one with five hundred cases, and checks each picks the right case.
func TestDeepAndWideSwitchesAtRuntime(t *testing.T) {
	const depth = 50
	deep := strings.Repeat(
		"switch n {\n    case 1:\n    ",
		depth,
	) + "exit(18)\n" + strings.Repeat(
		"}\n",
		depth,
	)
	if _, status := run(t, inMainProgram("", "n := 1", deep, "exit(1)")); status != 18 {
		t.Errorf("deep switch exited %d, want 18", status)
	}

	const width = 500
	lines := make([]string, 0, 2*width+4)
	lines = append(lines, "n := 437", "switch n {")
	for i := range width {
		lines = append(lines, "case "+strconv.Itoa(i)+":", "    exit("+strconv.Itoa(i%200)+")")
	}
	lines = append(lines, "}", "exit(255)")
	if _, status := run(t, inMainProgram("", lines...)); status != 437%200 {
		t.Errorf("wide switch exited %d, want %d", status, 437%200)
	}
}
