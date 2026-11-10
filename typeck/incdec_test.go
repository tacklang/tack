package typeck_test

import "testing"

// invalidIncDecCases are increments and decrements the checker must reject,
// with the diagnostics each must produce.
var invalidIncDecCases = []struct {
	name string
	src  string
	want []spanned
}{
	{
		"immutable",
		mainLines("x := 1", "x++", "exit(int32(x))"),
		[]spanned{{"cannot assign to immutable variable x", "x"}},
	},
	{
		"parameter",
		"package main\n\nfunc f(n int64) {\n    n--\n}\n\nfunc main() {\n    f(1)\n}\n",
		[]spanned{{"cannot assign to parameter n", "n"}},
	},
	{
		"bool",
		mainLines("mut b = true", "b++", "if b {", "}"),
		[]spanned{{"operator ++ is not defined on bool", "b"}},
	},
	{
		"string",
		mainLines("mut s = \"a\"", "s--", "print(s)"),
		[]spanned{{"operator -- is not defined on string", "s"}},
	},
	{
		"mutable global, which no fact bounds",
		"package main\n\nmut n int64\n\nfunc main() {\n    n++\n    code, fits := int32(n)\n    if fits { exit(code) }\n}\n",
		[]spanned{{"n++ may overflow int64; use n +%= 1 to wrap, or v, ok := n + 1 and n = v when ok", "n++"}},
	},
}

// TestIncDec checks ++ and -- need a mutable int variable.
func TestIncDec(t *testing.T) {
	ok := []string{
		mainLines(
			"mut x = 1",
			"x++",
			"x--",
			"x++",
			"code, fits := int32(x)",
			"if fits { exit(code) }",
		),
		"package main\n\nmut n int64\n\nfunc main() {\n    n +%= 1\n    code, fits := int32(n)\n    if fits { exit(code) }\n}\n",
	}
	for _, src := range ok {
		if _, diags := checkTree(t, src); len(diags) > 0 {
			t.Errorf("%q: got %v, want no diagnostics", src, diags)
		}
	}

	for _, tt := range invalidIncDecCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := checkTree(t, tt.src)
			if len(diags) != len(tt.want) {
				t.Fatalf("got %v, want %v", diags, tt.want)
			}
			for i, d := range diags {
				if d.Message != tt.want[i].msg {
					t.Errorf("message %d = %q, want %q", i, d.Message, tt.want[i].msg)
				}
				if got := tt.src[d.Span.Start:d.Span.End]; got != tt.want[i].at {
					t.Errorf("diagnostic %d covers %q, want %q", i, got, tt.want[i].at)
				}
			}
		})
	}
}
