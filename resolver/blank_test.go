package resolver_test

import (
	"strings"
	"testing"
)

// three declares a function with three results, for the tests below.
const three = "func three() (int64, string, bool) {\n    return 1, \"s\", true\n}\n\n"

// TestAcceptsBlankInMultiBindings checks _ discards a result wherever it
// stands in a declaration or assignment of several, as often as needed.
func TestAcceptsBlankInMultiBindings(t *testing.T) {
	for _, body := range []string{
		"a, _, _ := three()\n    exit(a)",
		"_, s, _ := three()\n    print(s)",
		"_, _, b := three()\n    if b {\n    }",
		"mut a := 0\n    a, _, _ = three()\n    exit(a)",
		"mut b := false\n    _, _, b = three()\n    if b {\n    }",
		"for a, _, _ := three(); a < 3; {\n        break\n    }",
	} {
		t.Run(body, func(t *testing.T) {
			_, diags := resolveTree(t, "package main\n\n"+three+"func main() {\n    "+body+"\n}\n")
			if len(diags) > 0 {
				t.Errorf("got %q, want no diagnostics", messagesOf(diags))
			}
		})
	}
}

// blankMisuseCases are uses of _ outside a binding of several, or that bind
// nothing, with the one message each must produce and what it must cover.
var blankMisuseCases = []struct {
	name, src, msg, at string
}{
	{"short declaration", "func main() {\n    _ := 1\n}\n", "cannot use _ as a name", "_"},
	{"var declaration", "func main() {\n    var _ = 1\n}\n", "cannot use _ as a name", "_"},
	{"mut declaration", "func main() {\n    mut _ int64\n}\n", "cannot use _ as a name", "_"},
	{"global", "var _ = 1\n\nfunc main() {\n}\n", "cannot use _ as a name", "_"},
	{
		"parameter",
		"func f(_ int64) {\n}\n\nfunc main() {\n    f(1)\n}\n",
		"cannot use _ as a name",
		"_",
	},
	{"function", "func _() {\n}\n\nfunc main() {\n}\n", "cannot use _ as a name", "_"},
	{
		"range variable",
		"func main() {\n    for _ := range 3 {\n    }\n}\n",
		"cannot use _ as a name",
		"_",
	},
	{
		"range binds nothing",
		"func main() {\n    for _, _ := range \"ab\" {\n    }\n}\n",
		"every name is _, so nothing is bound; remove the names and :=",
		"_, _",
	},
	{
		"loop init",
		"func main() {\n    for _ := 0; ; {\n        break\n    }\n}\n",
		"cannot use _ as a name",
		"_",
	},
	{"read", "func main() {\n    exit(_)\n}\n", "cannot use _ as a value", "_"},
	{
		"read after a binding",
		three + "func main() {\n    a, _, _ := three()\n    exit(a + _)\n}\n",
		"cannot use _ as a value",
		"_",
	},
	{"assigned", "func main() {\n    _ = 1\n}\n", "cannot assign to _", "_"},
	{"compound", "func main() {\n    _ += 1\n}\n", "cannot assign to _", "_"},
	{"incremented", "func main() {\n    _++\n}\n", "cannot assign to _", "_"},
	{
		"declares nothing",
		three + "func main() {\n    _, _, _ := three()\n}\n",
		"every name is _, so nothing is bound; make the call on its own", "_, _, _ := three()",
	},
	{
		"assigns nothing",
		three + "func main() {\n    _, _, _ = three()\n}\n",
		"every name is _, so nothing is bound; make the call on its own", "_, _, _ = three()",
	},
	{
		"mut on _",
		three + "func main() {\n    a, mut _, _ := three()\n    exit(a)\n}\n",
		"cannot use mut on _",
		"mut _",
	},
}

// TestRejectsBlankMisuse checks each use of _ that is not discarding a
// result is reported once, over the _.
func TestRejectsBlankMisuse(t *testing.T) {
	for _, tt := range blankMisuseCases {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n\n" + tt.src
			_, diags := resolveTree(t, src)
			if got := messagesOf(diags); strings.Join(got, "\n") != tt.msg {
				t.Fatalf("got %q, want only %q", got, tt.msg)
			}
			if got := src[diags[0].Span.Start:diags[0].Span.End]; got != tt.at {
				t.Errorf("diagnostic covers %q, want %q", got, tt.at)
			}
		})
	}
}

// TestLeavesAllBlankErrorBindingsToTheChecker checks a binding of nothing
// but _ to a call whose last result is written error is not reported as
// binding nothing, since making the call on its own, which that error
// suggests, drops the error too: the type checker reports the error as
// dropped instead.
func TestLeavesAllBlankErrorBindingsToTheChecker(t *testing.T) {
	fn := "func f() (int64, error) {\n    return 1, noerror\n}\n\n"
	for _, body := range []string{"_, _ := f()", "_, _ = f()"} {
		src := "package main\n\n" + fn + "func main() {\n    " + body + "\n}\n"
		if _, diags := resolveTree(t, src); len(diags) > 0 {
			t.Errorf("%s: got %q, want none", body, messagesOf(diags))
		}
	}
}
