package c_test

import "testing"

// noisy is a function that prints its name and returns its argument, so a
// program shows which operands were evaluated.
const noisy = "func say(s string, b bool) bool {\n    print(s)\n    return b\n}\n\n"

// logicalRuntimeCases are programs using && || and !, and what each must
// print and exit with. The printed output shows which operands ran.
var logicalRuntimeCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"&& skips its right side after false",
		"package main\n\n" + noisy +
			"func main() {\n    if say(\"l\", false) && say(\"r\", true) {\n        exit(1)\n    }\n    exit(2)\n}\n",
		"l", 2,
	},
	{
		"&& evaluates its right side after true",
		"package main\n\n" + noisy +
			"func main() {\n    if say(\"l\", true) && say(\"r\", true) {\n        exit(3)\n    }\n}\n",
		"lr", 3,
	},
	{
		"|| skips its right side after true",
		"package main\n\n" + noisy +
			"func main() {\n    if say(\"l\", true) || say(\"r\", false) {\n        exit(4)\n    }\n}\n",
		"l", 4,
	},
	{
		"|| evaluates its right side after false",
		"package main\n\n" + noisy +
			"func main() {\n    if say(\"l\", false) || say(\"r\", false) {\n        exit(1)\n    }\n    exit(5)\n}\n",
		"lr", 5,
	},
	{
		"chain stops at the first deciding operand",
		"package main\n\n" + noisy +
			"func main() {\n    if say(\"a\", false) || say(\"b\", true) && say(\"c\", false) || say(\"d\", true) {\n" +
			"        exit(6)\n    }\n}\n",
		"abcd", 6,
	},
	{
		"&& guards a division",
		"package main\n\nfunc safe(n int64) bool {\n    return n != 0 && 10 / n > 1\n}\n\n" +
			"func main() {\n    if safe(0) {\n        exit(1)\n    }\n    if safe(2) {\n        exit(7)\n    }\n}\n",
		"", 7,
	},
	{
		"not",
		"package main\n\nfunc main() {\n    x := 1 > 2\n    if !x {\n        if !!x {\n            exit(1)\n        }\n" +
			"        if !(x || false) == true {\n            exit(8)\n        }\n    }\n}\n",
		"",
		8,
	},
	{
		"stored and returned",
		"package main\n\nfunc xor(a bool, b bool) bool {\n    return (a || b) && !(a && b)\n}\n\n" +
			"func main() {\n    mut n = 0\n    if xor(true, false) {\n        n = n + 1\n    }\n    if xor(true, true) {\n        n = n + 10\n    }\n" +
			"    r := xor(false, false) || xor(false, true)\n    if r {\n        n = n + 100\n    }\n    exit(n)\n}\n",
		"", 101,
	},
	{
		"package-level",
		"package main\n\nvar on = !false && (1 < 2 || false)\n\nfunc main() {\n    if on {\n        exit(9)\n    }\n}\n",
		"",
		9,
	},
}

func TestLogicalOperatorsAtRuntime(t *testing.T) {
	for _, tt := range logicalRuntimeCases {
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
