package typeck_test

import (
	"testing"

	"github.com/tacklang/tack/diag"
)

// TestReadErrorsAndOksAreChecked checks that an error or an ok a
// variable holds counts as checked once anything reads it: its code or
// its message, the error passed on as a value or returned, the ok in a
// condition; and that a variable a call gave a value that holds neither,
// never read, is a warning, declared and not used, as any other is, with
// the note at a call by its name that passed over it.
func TestReadErrorsAndOksAreChecked(t *testing.T) {
	accepted := []string{
		mainLines("mut l := make(list[int64], c_allocator)", "err := append(mut l, 1)",
			"if err.code != 0 {", "    exit(1)", "}"),
		mainLines("mut l := make(list[int64], c_allocator)", "err := append(mut l, 1)",
			"print(err.msg)"),
		"package main\n\nfunc keep(e error) {\n    print(e.msg)\n}\n\nfunc main() {\n" +
			"    mut l := make(list[int64], c_allocator)\n    err := append(mut l, 1)\n" +
			"    keep(err)\n}\n",
		"package main\n\nfunc grow(mut l list[int64]) error {\n    err := append(mut l, 1)\n" +
			"    return err\n}\n\nfunc main() {\n    mut l := make(list[int64], c_allocator)\n" +
			"    err := grow(mut l)\n    if err.code != 0 {\n        exit(1)\n    }\n}\n",
		"package main\n\nfunc f(l []int64, i int64) int64 {\n    x, ok := l[i]\n" +
			"    if !ok {\n        return 0\n    }\n    return x\n}\n\nfunc main() {\n" +
			"    a := [1]int64{1}\n    if f(a[:], 0) == 1 {\n        print(\"x\")\n    }\n}\n",
		"package main\n\nfunc f(n int64, d int64) bool {\n    q, ok := n / d\n" +
			"    return ok && q > 0\n}\n\nfunc main() {\n    if f(1, 1) {\n        print(\"x\")\n    }\n}\n",
	}
	for _, src := range accepted {
		if _, diags := checkTreeAll(t, src); diag.HasErrors(diags) {
			t.Errorf("got %v, want no error:\n%s", diags, src)
		}
	}

	src := "package main\n\nfunc twice(n int64) int64 {\n    return n * 2\n}\n\n" +
		"func main() {\n    twice := twice(2)\n    if twice(3) == 6 {\n        print(\"x\")\n    }\n}\n"
	_, diags := checkTreeAll(t, src)
	if len(diags) != 1 || diags[0].Severity != diag.Warning ||
		diags[0].Message != "declared and not used: twice" || !diags[0].Unused {
		t.Fatalf("got %+v, want the warning that twice is not used", diags)
	}
	if len(diags[0].Related) != 1 || diags[0].Related[0].Message !=
		"this call calls the function twice, not the variable; rename one of them" {
		t.Errorf("got notes %+v, want the one at the call that passed over twice", diags[0].Related)
	}
}
