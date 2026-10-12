package c_test

import "testing"

// rangeRuntimeCases are programs with range loops, and what each must print
// and exit with.
var rangeRuntimeCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"counts from zero",
		"package main\n\nfunc main() {\n    mut sum = 0\n    for i := range 10 {\n        sum += i\n    }\n    exit(sum)\n}\n",
		"",
		45,
	},
	{
		"no variable",
		"package main\n\nfunc main() {\n    for range 3 {\n        print(\"x\")\n    }\n}\n",
		"x\nx\nx\n", 0,
	},
	{
		"zero and negative counts",
		"package main\n\nfunc main() {\n    for range 0 {\n        exit(1)\n    }\n    for range -5 {\n        exit(2)\n    }\n    exit(3)\n}\n",
		"",
		3,
	},
	{
		"count evaluated once",
		"package main\n\nfunc three() int {\n    print(\"count\")\n    return 3\n}\n\nfunc main() {\n    mut n = 0\n    for range three() {\n        n++\n    }\n    exit(n)\n}\n",
		"count\n",
		3,
	},
	{
		"changing the count's variable changes nothing",
		"package main\n\nfunc main() {\n    mut n = 2\n    mut iterations = 0\n    for range n {\n        n += 10\n        iterations++\n    }\n    exit(iterations)\n}\n",
		"",
		2,
	},
	{
		"break and continue",
		"package main\n\nfunc main() {\n    mut sum = 0\n    for i := range 100 {\n        if i % 2 == 1 {\n            continue\n        }\n        if i > 8 {\n            break\n        }\n        sum += i\n    }\n    exit(sum)\n}\n",
		"",
		20,
	},
	{
		"nested",
		"package main\n\nfunc main() {\n    mut n = 0\n    for i := range 4 {\n        for j := range i {\n            n += j + 1\n        }\n    }\n    exit(n)\n}\n",
		"",
		10,
	},
	{
		"siblings reuse the name",
		"package main\n\nfunc main() {\n    mut n = 0\n    for i := range 3 {\n        n += i\n    }\n    for i := range 3 {\n        n += i * 10\n    }\n    exit(n)\n}\n",
		"",
		33,
	},
	{
		"returns from inside",
		"package main\n\nfunc find(want int) int {\n    for i := range 50 {\n        if i * i == want {\n            return i\n        }\n    }\n    return -1\n}\n\nfunc main() {\n    exit(find(49) + find(50) + 1)\n}\n",
		"",
		7,
	},
}

func TestRangeLoopsAtRuntime(t *testing.T) {
	for _, tt := range rangeRuntimeCases {
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
