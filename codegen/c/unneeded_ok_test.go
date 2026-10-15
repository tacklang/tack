package c_test

import "testing"

// unneededOkCases bind an operation that cannot fail with ok: each must
// give the operation's value and true, and compile without a warning.
var unneededOkCases = []struct{ name, src string }{
	{
		"division by a constant",
		program(
			"mut n int64 = 9",
			"q, ok := n / 3",
			"r, fine := n % 4",
			"if ok && fine && q == 3 && r == 1 {",
			"    exit(1)",
			"}",
		),
	},
	{
		"division of constants",
		program(
			"q, ok := 7 / 2",
			"r, fine := -7 % 2",
			"if ok && fine && q == 3 && r == -1 {",
			"    exit(1)",
			"}",
		),
	},
	{
		"division of constants into a narrow variable",
		program(
			"mut q uint8 = 0",
			"mut ok := false",
			"q, ok = 200 / 2",
			"if ok && q == 100 {",
			"    exit(1)",
			"}",
		),
	},
	{
		"division of constants discarded",
		program("_, ok := 7 / 2", "if ok {", "    exit(1)", "}"),
	},
	{
		"guarded divisor",
		program(
			"mut d int64 = 2",
			"if d != 0 {",
			"    q, ok := 9 / d",
			"    if ok && q == 4 {",
			"        exit(1)",
			"    }",
			"}",
		),
	},
	{
		"constant array index",
		program("a := [3]int64{7, 8, 9}", "v, ok := a[2]", "if ok && v == 9 {", "    exit(1)", "}"),
	},
	{
		"proven array index",
		program(
			"a := [3]int64{7, 8, 9}",
			"mut sum int64 = 0",
			"for i := range a {",
			"    v, ok := a[i]",
			"    if ok {",
			"        sum += v",
			"    }",
			"}",
			"code, _ := int32(sum)",
			"exit(code)",
		),
	},
	{
		"constant index into a literal",
		program("b, ok := \"tack\"[3]", "if ok && b == 'k' {", "    exit(1)", "}"),
	},
	{
		"constant conversion",
		program("v, ok := uint8(255)", "if ok && v == 255 {", "    exit(1)", "}"),
	},
	{
		"known value conversion",
		program("x := -5", "v, ok := int8(x)", "if ok && v == -5 {", "    exit(1)", "}"),
	},
	{
		"widening conversion",
		program(
			"var b uint8 = 200",
			"v, ok := int16(b)",
			"w, same := uint8(b)",
			"if ok && same && v == 200 && w == 200 {",
			"    exit(1)",
			"}",
		),
	},
	{
		"conversion of a value not known to its own type",
		program(
			"mut x int64 = -9",
			"x *= 2",
			"v, ok := int64(x)",
			"if ok && v == -18 {",
			"    exit(1)",
			"}",
		),
	},
}

// TestUnneededOkAtRuntime builds each of unneededOkCases strictly, with the
// undefined behavior sanitizer on, and checks it exits with 1, or with the
// sum 24 for the loop.
func TestUnneededOkAtRuntime(t *testing.T) {
	for _, tt := range unneededOkCases {
		t.Run(tt.name, func(t *testing.T) {
			out, status := runSanitized(t, tt.src)
			want := 1
			if tt.name == "proven array index" {
				want = 24
			}
			if out != "" || status != want {
				t.Errorf("got stdout %q, status %d; want \"\", %d", out, status, want)
			}
		})
	}
}
