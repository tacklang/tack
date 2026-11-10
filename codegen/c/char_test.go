package c_test

import (
	"testing"
)

// charCases are programs using character literals, with the status each
// must exit with. Each decides its status by what the literals computed.
var charCases = []struct {
	name   string
	src    string
	status int
}{
	{"value", program("exit('a')"), 97},
	{"escapes", program("exit('\\n' + '\\t' + '\\\\' + '\\'')"), 10 + 9 + 92 + 39},
	{"double quote", program("exit('\"')"), 34},
	{"the zero byte", program("c := '\x00' + 1", "exit(c)"), 1},
	{"high byte", "package main\n\nfunc main() {\n    exit('\xff' - 200)\n}\n", 55},
	{"wraps above at run time", program("mut c = 'a'", "c = c *% 3", "exit(c)"), 35},
	{"wraps below at run time", program("mut c = 'a'", "c -%= 'b'", "exit(c)"), 255},
	{"widens before arithmetic", program("var n int64 = 'a'", "exit(int32(n * 3 - 200))"), 91},
	{"uint8 arithmetic", program("c := 'z'", "exit(c - 'a' + 1)"), 26},
	{"compare", program("c := 'q'", "if c > 'p' && c < 'r' && c == 113 {", "    exit(1)", "}"), 1},
	{
		"divides by a known difference",
		program("x := 'd' / ('c' - 'a')", "exit(x)"),
		50,
	},
	{
		"index a table",
		program(
			"mut counts [256]int64",
			"counts['a'] = 3",
			"c := 'a'",
			"counts[c] +%= 4",
			"code, fits := int32(counts[97])",
			"if fits { exit(code) }",
		),
		7,
	},
	{
		"package level",
		"package main\n\nvar first = 'A'\nvar row = [2]uint8{'x', 'y' + 1}\nvar before = 'A' < 'B'\n\n" +
			"func main() {\n    if before && row[1] == 'z' {\n        exit(first + row[0] - 'x')\n    }\n}\n",
		65,
	},
}

// TestCharLiteralsAtRuntime builds each program with the undefined behavior
// sanitizer, so arithmetic on a uint8 that overflowed in C fails the test.
func TestCharLiteralsAtRuntime(t *testing.T) {
	for _, tt := range charCases {
		t.Run(tt.name, func(t *testing.T) {
			out, status := runSanitized(t, tt.src)
			if out != "" || status != tt.status {
				t.Errorf("got stdout %q, status %d; want no output, %d", out, status, tt.status)
			}
		})
	}
}
