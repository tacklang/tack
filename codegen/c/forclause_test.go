package c_test

import "testing"

// clauseRuntimeCases are programs with three-clause loops, and what each must
// print and exit with.
var clauseRuntimeCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"counts",
		"package main\n\nfunc main() {\n    mut n = 0\n    for mut i := 0; i < 10; i++ {\n        n += i\n    }\n    exit(n)\n}\n",
		"",
		45,
	},
	{
		"continue runs the post statement",
		"package main\n\nfunc main() {\n    mut n = 0\n    for mut i := 0; i < 10; i++ {\n        if i % 2 == 0 {\n            continue\n        }\n        n++\n    }\n    exit(n)\n}\n",
		"",
		5,
	},
	{
		"break skips the post statement",
		"package main\n\nfunc main() {\n    mut i = 0\n    for i = 0; i < 10; i++ {\n        if i == 4 {\n            break\n        }\n    }\n    exit(i)\n}\n",
		"",
		4,
	},
	{
		"clauses run in order",
		"package main\n\nfunc say(s string) bool {\n    print(s)\n    return true\n}\n\n" +
			"func main() {\n    mut n = 0\n    for print(\"init\"); n < 2 && say(\"cond\"); print(\"post\") {\n        print(\"body\")\n        n++\n    }\n}\n",
		"initcondbodypostcondbodypost", 0,
	},
	{
		"post that prints",
		"package main\n\nfunc main() {\n    mut i = 0\n    for ; i < 2; print(\"x\") {\n        i++\n    }\n}\n",
		"xx",
		0,
	},
	{
		"equality condition",
		"package main\n\nfunc main() {\n    mut n = 0\n    for mut i := 0; i == 0 || n == 0; i++ {\n        n++\n    }\n    exit(n)\n}\n",
		"",
		1,
	},
	{
		"nothing but semicolons",
		"package main\n\nfunc main() {\n    mut n = 0\n    for ;; {\n        n++\n        if n == 3 {\n            break\n        }\n    }\n    exit(n)\n}\n",
		"",
		3,
	},
	{
		"sibling loops reuse the name",
		"package main\n\nfunc main() {\n    mut n = 0\n    for mut i := 0; i < 3; i++ {\n        n++\n    }\n    for mut i := 0; i < 4; i++ {\n        n++\n    }\n    exit(n)\n}\n",
		"",
		7,
	},
	{
		"nested",
		"package main\n\nfunc main() {\n    mut n = 0\n    for mut i := 0; i < 3; i++ {\n        for mut j := 0; j < 4; j++ {\n            if j == 2 {\n                continue\n            }\n            n++\n        }\n    }\n    exit(n)\n}\n",
		"",
		9,
	},
	{
		"returns from a loop without a condition",
		"package main\n\nfunc first(over int) int {\n    for mut i := 0; ; i++ {\n        if i * i > over {\n            return i\n        }\n    }\n}\n\nfunc main() {\n    exit(first(30))\n}\n",
		"",
		6,
	},
}

func TestThreeClauseLoopsAtRuntime(t *testing.T) {
	for _, tt := range clauseRuntimeCases {
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
