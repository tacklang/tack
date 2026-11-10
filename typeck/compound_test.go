package typeck_test

import "testing"

// TestAcceptsCompoundAssignment lists compound assignments that must check
// cleanly.
func TestAcceptsCompoundAssignment(t *testing.T) {
	tests := []string{
		mainLines(
			"mut x = 1",
			"x += 2",
			"x -= 1",
			"x *%= 3",
			"x /= 2",
			"x %= 5",
			"code, fits := int32(x)",
			"if fits { exit(code) }",
		),
		mainLines(
			"mut x int64",
			"x += 1 + 2 * 3",
			"code, fits := int32(x)",
			"if fits { exit(code) }",
		),
		"package main\n\nfunc two() int64 {\n    return 2\n}\n\nfunc main() {\n    mut x = 1\n    x *= two()\n    code, fits := int32(x)\n    if fits { exit(code) }\n}\n",
		"package main\n\nmut total int64\n\nfunc main() {\n    total +%= 5\n    code, fits := int32(total)\n    if fits { exit(code) }\n}\n",
	}

	for _, src := range tests {
		t.Run(src, func(t *testing.T) {
			if _, diags := checkTree(t, src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// spanned is a diagnostic message and the source text it must cover.
type spanned struct{ msg, at string }

// invalidCompoundCases are compound assignments the checker must reject,
// with the diagnostics each must produce.
var invalidCompoundCases = []struct {
	name string
	src  string
	want []spanned
}{
	{
		"immutable",
		mainLines("x := 1", "x += 1", "exit(int32(x))"),
		[]spanned{{"cannot assign to immutable variable x", "x"}},
	},
	{
		"parameter",
		"package main\n\nfunc f(n int64) {\n    n +%= 1\n}\n\nfunc main() {\n    f(1)\n}\n",
		[]spanned{{"cannot assign to parameter n", "n"}},
	},
	{
		"immutable global",
		"package main\n\nvar limit = 3\n\nfunc main() {\n    limit -= 1\n    exit(int32(limit))\n}\n",
		[]spanned{{"cannot assign to immutable variable limit", "limit"}},
	},
	{
		"string target",
		mainLines("mut s = \"a\"", "s += \"b\"", "print(s)"),
		[]spanned{
			{"operator += is not defined on string", "s"},
		},
	},
	{
		"bool target",
		mainLines("mut b = true", "b *= 2", "if b {", "}"),
		[]spanned{{"operator *= is not defined on bool", "b"}},
	},
	{
		"string value",
		mainLines("mut x = 1", "x -= \"s\"", "code, fits := int32(x)", "if fits { exit(code) }"),
		[]spanned{{"cannot use string as int64 in -=", "\"s\""}},
	},
	{
		"bool value",
		mainLines("mut x = 1", "x %= 1 < 2", "code, fits := int32(x)", "if fits { exit(code) }"),
		[]spanned{{"cannot use bool as int64 in %=", "1 < 2"}},
	},
	{
		"both wrong",
		mainLines("mut b = true", "b /= false", "if b {", "}"),
		[]spanned{
			{"operator /= is not defined on bool", "b"},
		},
	},
	{
		"value from a function returning nothing",
		"package main\n\nfunc f() {\n}\n\nfunc main() {\n    mut x = 1\n    x += f()\n    code, fits := int32(x)\n    if fits { exit(code) }\n}\n",
		[]spanned{{"f returns no value", "f()"}},
	},
	{
		"error inside the value",
		mainLines(
			"mut x = 1",
			"x += 1 + \"s\"",
			"code, fits := int32(x)",
			"if fits { exit(code) }",
		),
		[]spanned{{"operator + is not defined on string", "\"s\""}},
	},
}

// TestRejectsInvalidCompoundAssignment checks the target must be a mutable
// int variable and the value an int, each reported where it is wrong.
func TestRejectsInvalidCompoundAssignment(t *testing.T) {
	for _, tt := range invalidCompoundCases {
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
