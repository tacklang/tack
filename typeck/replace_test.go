package typeck_test

import "testing"

// replaceProgram returns a program declaring Stack, a struct holding a
// list, and use, which borrows a list, and a function f taking params
// whose body is lines.
func replaceProgram(params string, lines ...string) string {
	body := ""
	for _, l := range lines {
		body += "    " + l + "\n"
	}
	return "package main\n\ntype Stack struct {\n    frames list[int64]\n}\n\n" +
		"func use(l list[int64]) {\n}\n\nfunc g(mut l list[int64]) list[int64] {\n" +
		"    return make(list[int64], c_allocator)\n}\n\nfunc f(" + params + ") {\n" + body + "}\n\nfunc main() {\n}\n"
}

// TestAcceptsReplace checks replace on every kind of place a mut argument
// can name, with copyable and owned values, as a statement, reading the
// old value of a copyable place, given a call that changes the place
// itself, and keeping the fact its own call needs.
func TestAcceptsReplace(t *testing.T) {
	for _, tt := range []struct{ name, src string }{
		{"a field taken out and put back", replaceProgram("mut s Stack",
			"mut l := replace(mut s.frames, make(list[int64], c_allocator))",
			"err := append(mut l, 1)", "e := replace(mut s.frames, l)", "use(e)", "if err.code != 0 {", "}")},
		{"a mutable local", replaceProgram("", "mut x := 1", "old := replace(mut x, 2)", "if old == x {", "}")},
		{"a value read from the old one", replaceProgram("mut x int64", "old := replace(mut x, x *% 2)",
			"if old == x {", "}")},
		{"a widened constant", replaceProgram("mut x uint8", "old := replace(mut x, 255)", "if old == x {", "}")},
		{"an owned list element", replaceProgram("mut ll list[list[int64]]", "if len(ll) > 0 {",
			"    old := replace(mut ll[0], make(list[int64], c_allocator))", "    use(old)", "}")},
		{"an array element", replaceProgram("mut a [3]int64", "old := replace(mut a[1], 9)", "if old == 0 {", "}")},
		{"a statement", replaceProgram("mut s Stack", "replace(mut s.frames, make(list[int64], c_allocator))")},
		{"a call changing the place itself", replaceProgram("mut ll list[list[int64]]", "if len(ll) > 0 {",
			"    old := replace(mut ll[0], g(mut ll[0]))", "    use(old)", "}")},
		{"its own fact", replaceProgram("mut d int64", "if d != 0 {", "    old := replace(mut d, 100 / d)",
			"    if old == d {", "    }", "}")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if diags := check(t, tt.src); len(diags) > 0 {
				t.Fatalf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// TestRejectsReplace checks a place that cannot be passed as mut, a value
// of the wrong type, an owned value that is the place itself or moved out
// of where nothing may be moved, a value whose own call ends the index's
// proof, and a fact the replace ends for what comes after it.
func TestRejectsReplace(t *testing.T) {
	for _, tt := range []struct{ name, src, want string }{
		{
			"not mut", replaceProgram("mut x int64", "old := replace(x, 1)", "if old == 0 {", "}"),
			"replace takes place as mut; pass mut x",
		},
		{
			"an immutable local", replaceProgram("", "x := 1", "old := replace(mut x, 2)", "exit(int32(old))"),
			"cannot pass mut x: x is immutable; declare it with mut",
		},
		{
			"a wrong type", replaceProgram("mut x int64", "old := replace(mut x, \"s\")", "if old == 0 {", "}"),
			"cannot use string as int64 in argument to replace",
		},
		{
			"the place itself", replaceProgram("", "mut l := make(list[int64], c_allocator)", "old := replace(mut l, l)", "use(old)"),
			"cannot pass l alongside mut l in this call: a list is not copied for a call, " +
				"and a mut argument must be the only way to reach l during it",
		},
		{
			"an element of the same list", replaceProgram("mut ll list[list[int64]]", "if len(ll) > 1 {",
				"    old := replace(mut ll[0], ll[1])", "    use(old)", "}"),
			"cannot pass ll[1] alongside mut ll[0] in this call: a list is not copied for a call, " +
				"and a mut argument must be the only way to reach ll[0] during it",
		},
		{
			"a value changing the list", replaceProgram("mut ll list[list[int64]]", "if len(ll) > 0 {",
				"    old := replace(mut ll[0], ll2(mut ll))", "    use(old)", "}") +
				"\nfunc ll2(mut ll list[list[int64]]) list[int64] {\n    return make(list[int64], c_allocator)\n}\n",
			"index 0 may be out of range; check len(ll) > 0 first",
		},
		{
			"a fact after it", replaceProgram("mut d int64", "if d != 0 {", "    old := replace(mut d, 0)",
				"    if old == 100 / d {", "    }", "}"),
			"divisor d may be zero; check d != 0 first, or use q, ok := ... / d",
		},
		{
			"too few arguments", replaceProgram("mut x int64", "old := replace(mut x)", "if old == 0 {", "}"),
			"replace expects 2 argument(s), got 1",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			diags := check(t, tt.src)
			if len(diags) != 1 || diags[0].Message != tt.want {
				t.Fatalf("got %v, want only %q", diags, tt.want)
			}
		})
	}
}
