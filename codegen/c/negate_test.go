package c_test

import (
	"strings"
	"testing"
)

// Negations and negative literals are parenthesised in C, so two minus
// signs in a row can never be read as C's decrement operator.
func TestNegationNeverEmitsDecrement(t *testing.T) {
	got := generate(
		t,
		"package main\n\nfunc main() {\n    x := 5\n    exit(int32(- -x + - -5 + -(-5)))\n}\n",
	)
	if strings.Contains(got, "--") {
		t.Errorf("generated C contains --:\n%s", got)
	}
}

// negateRuntimeCases are programs using negation, and what each must exit
// with.
var negateRuntimeCases = []struct {
	name   string
	src    string
	status int
}{
	{"negative literal", "package main\n\nfunc main() {\n    exit(-5 + 12)\n}\n", 7},
	{
		"negated variable",
		"package main\n\nfunc main() {\n    x := 5\n    exit(int32(-x + 10))\n}\n",
		5,
	},
	{"double negation", "package main\n\nfunc main() {\n    x := 6\n    exit(int32(- -x))\n}\n", 6},
	{"negated negative literal", "package main\n\nfunc main() {\n    exit(- -9)\n}\n", 9},
	{
		"minus a negative",
		"package main\n\nfunc main() {\n    x := 3\n    exit(int32(x - -4))\n}\n",
		7,
	},
	{"negated sum", "package main\n\nfunc main() {\n    exit(-(1 - 12))\n}\n", 11},
	{
		"negated call",
		"package main\n\nfunc f() int64 {\n    return -12\n}\n\nfunc main() {\n    code, _ := int32(-f())\n    exit(code)\n}\n",
		12,
	},
	{
		"smallest int64 literal",
		"package main\n\nfunc main() {\n    if -9223372036854775808 == -9223372036854775807 - 1 {\n        exit(8)\n    }\n}\n",
		8,
	},
	{
		"global",
		"package main\n\nvar low = -9223372036854775808\n\nvar back = -(-40)\n\nfunc main() {\n    if low < 0 {\n        exit(int32(back + 2))\n    }\n}\n",
		42,
	},
}

func TestNegationAtRuntime(t *testing.T) {
	for _, tt := range negateRuntimeCases {
		t.Run(tt.name, func(t *testing.T) {
			if stdout, status := run(t, tt.src); stdout != "" || status != tt.status {
				t.Errorf("got stdout %q, status %d; want no output, %d", stdout, status, tt.status)
			}
		})
	}
}
