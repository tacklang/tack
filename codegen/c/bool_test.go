package c_test

import (
	"strings"
	"testing"
)

// TestEmitsBoolLiteralsAsCBools pins the C a bool literal lowers to.
func TestEmitsBoolLiteralsAsCBools(t *testing.T) {
	got := generate(
		t,
		"package main\n\nfunc main() {\n    x := true\n    y := false\n    if x {\n        if y {\n        }\n    }\n}\n",
	)
	for _, want := range []string{"const bool v_x = true;", "const bool v_y = false;"} {
		if !strings.Contains(got, want) {
			t.Errorf("generated C is missing %q:\n%s", want, got)
		}
	}
}

// boolRuntimeCases are programs using bool literals, and what each must
// print and exit with.
var boolRuntimeCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"true condition runs the branch",
		"package main\n\nfunc main() {\n    if true {\n        exit(3)\n    }\n}\n", "", 3,
	},
	{
		"false condition takes the else",
		"package main\n\nfunc main() {\n    if false {\n        exit(1)\n    } else {\n        exit(4)\n    }\n}\n",
		"",
		4,
	},
	{
		"variable flipped",
		"package main\n\nfunc main() {\n    mut done = false\n    if 1 < 2 {\n        done = true\n    }\n" +
			"    if done {\n        exit(5)\n    }\n}\n",
		"",
		5,
	},
	{
		"returned and passed",
		"package main\n\nfunc not(b bool) bool {\n    if b {\n        return false\n    }\n    return true\n}\n\n" +
			"func main() {\n    if not(false) {\n        if not(true) {\n            exit(1)\n        }\n        exit(6)\n    }\n}\n",
		"",
		6,
	},
	{
		"package-level",
		"package main\n\nvar on = true\n\nmut off = false\n\nfunc main() {\n    if on {\n        if off {\n            exit(1)\n        }\n" +
			"        off = true\n        if off {\n            print(\"on\")\n            exit(7)\n        }\n    }\n}\n",
		"on\n",
		7,
	},
}

func TestBoolLiteralsAtRuntime(t *testing.T) {
	for _, tt := range boolRuntimeCases {
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
