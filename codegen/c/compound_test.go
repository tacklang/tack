package c_test

import "testing"

// compoundRuntimeCases are programs using compound assignment, and what each
// must exit with.
var compoundRuntimeCases = []struct {
	name   string
	src    string
	status int
}{
	{"+=", "package main\n\nfunc main() {\n    mut x = 40\n    x += 2\n    exit(x)\n}\n", 42},
	{"-=", "package main\n\nfunc main() {\n    mut x = 50\n    x -= 8\n    exit(x)\n}\n", 42},
	{"*=", "package main\n\nfunc main() {\n    mut x = 6\n    x *= 7\n    exit(x)\n}\n", 42},
	{
		"/= truncates",
		"package main\n\nfunc main() {\n    mut x = 0 - 7\n    x /= 2\n    exit(x + 10)\n}\n",
		7,
	},
	{
		"%= takes the dividend's sign",
		"package main\n\nfunc main() {\n    mut x = 0 - 7\n    x %= 3\n    exit(x + 10)\n}\n",
		9,
	},
	{
		"value grouped as a whole",
		"package main\n\nfunc main() {\n    mut x = 10\n    x -= 2 + 3\n    exit(x)\n}\n",
		5,
	},
	{
		"value uses the target",
		"package main\n\nfunc main() {\n    mut x = 5\n    x *= x + 1\n    exit(x)\n}\n",
		30,
	},
	{
		"global from a function",
		"package main\n\nmut total int64\n\nfunc add(n int64) {\n    total += n\n}\n\nfunc main() {\n    add(3)\n    add(4)\n    exit(total)\n}\n",
		7,
	},
	{
		"in a loop",
		"package main\n\nfunc main() {\n    mut i = 0\n    mut sum = 0\n    for i < 5 {\n        i += 1\n        sum += i\n    }\n    exit(sum)\n}\n",
		15,
	},
}

func TestCompoundAssignmentAtRuntime(t *testing.T) {
	for _, tt := range compoundRuntimeCases {
		t.Run(tt.name, func(t *testing.T) {
			if stdout, status := run(t, tt.src); stdout != "" || status != tt.status {
				t.Errorf("got stdout %q, status %d; want no output, %d", stdout, status, tt.status)
			}
		})
	}
}
