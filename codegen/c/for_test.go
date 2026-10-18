package c_test

import "testing"

// loopRuntimeCases are programs with loops, and what each must print and
// exit with.
var loopRuntimeCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"counts",
		"package main\n\nfunc main() {\n    mut i = 0\n    for i < 10 {\n        i = i + 1\n    }\n    code, fits := int32(i)\n    if fits { exit(code) }\n}\n",
		"",
		10,
	},
	{
		"false condition never runs the body",
		"package main\n\nfunc main() {\n    for false {\n        exit(1)\n    }\n    exit(2)\n}\n",
		"", 2,
	},
	{
		"break leaves forever",
		"package main\n\nfunc main() {\n    mut i = 0\n    for {\n        i = i + 1\n        if i == 7 {\n            break\n        }\n    }\n    code, fits := int32(i)\n    if fits { exit(code) }\n}\n",
		"",
		7,
	},
	{
		"continue skips the rest of the body",
		"package main\n\nfunc main() {\n    mut i = 0\n    mut odd = 0\n    for i < 10 {\n        i = i + 1\n        if i % 2 == 0 {\n            continue\n        }\n" +
			"        odd = odd + 1\n    }\n    code, fits := int32(odd)\n    if fits { exit(code) }\n}\n",
		"",
		5,
	},
	{
		"break leaves only the inner loop",
		"package main\n\nfunc main() {\n    mut n = 0\n    mut i = 0\n    for i < 3 {\n        i = i + 1\n        for {\n            n = n + 1\n            break\n        }\n" +
			"        n = n + 10\n    }\n    code, fits := int32(n)\n    if fits { exit(code) }\n}\n",
		"",
		33,
	},
	{
		"returns from forever",
		"package main\n\nfunc first(over int64) int64 {\n    mut i = 0\n    for {\n        if i * i > over {\n            return i\n        }\n        i = i + 1\n    }\n}\n\n" +
			"func main() {\n    code, fits := int32(first(50))\n    if fits { exit(code) }\n}\n",
		"",
		8,
	},
	{
		"fresh variable each iteration",
		"package main\n\nfunc main() {\n    mut i = 0\n    mut sum = 0\n    for i < 4 {\n        sq := i * i\n        sum = sum + sq\n        i = i + 1\n    }\n    code, fits := int32(sum)\n    if fits { exit(code) }\n}\n",
		"",
		14,
	},
	{
		"condition re-evaluated each time",
		"package main\n\nmut calls int64\n\nfunc more() bool {\n    calls = calls + 1\n    print(\"check\")\n    return calls < 3\n}\n\n" +
			"func main() {\n    for more() {\n    }\n    code, fits := int32(calls)\n    if fits { exit(code) }\n}\n",
		"checkcheckcheck",
		3,
	},
	{
		"iterative fib",
		"package main\n\nfunc fib(n int64) int64 {\n    mut a = 0\n    mut b = 1\n    mut i = 0\n    for i < n {\n        next := a + b\n        a = b\n        b = next\n        i = i + 1\n    }\n    return a\n}\n\n" +
			"func main() {\n    code, fits := int32(fib(10))\n    if fits { exit(code) }\n}\n",
		"",
		55,
	},
	{
		"exit inside forever",
		"package main\n\nfunc main() {\n    for {\n        print(\"once\")\n        exit(4)\n    }\n}\n",
		"once",
		4,
	},
}

func TestLoopsAtRuntime(t *testing.T) {
	for _, tt := range loopRuntimeCases {
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
