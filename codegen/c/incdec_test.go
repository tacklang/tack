package c_test

import "testing"

// incDecRuntimeCases are programs using ++ and --, and what each must exit
// with. A mutable global has no facts, so nothing proves its ++, and the
// global case steps it with +%= 1, its wrapping form.
var incDecRuntimeCases = []struct {
	name   string
	src    string
	status int
}{
	{
		"++",
		"package main\n\nfunc main() {\n    mut x = 41\n    x++\n    code, fits := int32(x)\n    if fits { exit(code) }\n}\n",
		42,
	},
	{
		"--",
		"package main\n\nfunc main() {\n    mut x = 43\n    x--\n    code, fits := int32(x)\n    if fits { exit(code) }\n}\n",
		42,
	},
	{
		"through zero",
		"package main\n\nfunc main() {\n    mut x = 1\n    x--\n    x--\n    code, fits := int32(x + 5)\n    if fits { exit(code) }\n}\n",
		4,
	},
	{
		"counting loop",
		"package main\n\nfunc main() {\n    mut i = 0\n    mut n = 0\n    for i < 10 {\n        i++\n        if n < 100 {\n            n++\n        }\n        if n < 100 {\n            n++\n        }\n    }\n    code, fits := int32(n)\n    if fits { exit(code) }\n}\n",
		20,
	},
	{
		"global",
		"package main\n\nmut hits int64\n\nfunc hit() {\n    hits +%= 1\n}\n\nfunc main() {\n    hit()\n    hit()\n    code, fits := int32(hits)\n    if fits { exit(code) }\n}\n",
		2,
	},
}

// Each of incDecRuntimeCases, built with clang's warnings as errors (see
// run), prints nothing and exits as the case says.
func TestIncDecAtRuntime(t *testing.T) {
	for _, tt := range incDecRuntimeCases {
		t.Run(tt.name, func(t *testing.T) {
			if stdout, status := run(t, tt.src); stdout != "" || status != tt.status {
				t.Errorf("got stdout %q, status %d; want no output, %d", stdout, status, tt.status)
			}
		})
	}
}
