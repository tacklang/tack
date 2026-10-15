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
}

// TestIncDec checks ++ and -- need a mutable int variable.
func TestIncDec(t *testing.T) {
	ok := []string{
		mainLines("mut x = 1", "x++", "x--", "x++", "code, _ := int32(x)", "exit(code)"),
		"package main\n\nmut n int64\n\nfunc main() {\n    n++\n    code, _ := int32(n)\n    exit(code)\n}\n",
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
