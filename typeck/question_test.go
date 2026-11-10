package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// questionProgram returns a program declaring parse, returning an int64
// and an error, divide, two int64s and an error, ready, an error alone,
// numbers, a list and an error, and lookup, an int64 and a bool, then
// decls, then main, empty.
func questionProgram(decls ...string) string {
	return "package main\n\n" +
		"func parse(s string) (int64, error) {\n    if len(s) == 0 {\n" +
		"        return 0, error{code: 1, msg: \"empty\"}\n    }\n    return 1, noerror\n}\n\n" +
		"func divide(a int64, b int64) (int64, int64, error) {\n    if b == 0 || b == -1 {\n" +
		"        return 0, 0, error{code: 2, msg: \"zero\"}\n    }\n    return a / b, a % b, noerror\n}\n\n" +
		"func ready() error {\n    return noerror\n}\n\n" +
		"func numbers() (list[int64], error) {\n" +
		"    return make(list[int64], c_allocator), noerror\n}\n\n" +
		"func lookup(k int64) (int64, bool) {\n    return k, true\n}\n\n" +
		strings.Join(decls, "\n") + "\nfunc main() {\n}\n"
}

// questionAcceptedCases are uses of ? the checker accepts.
var questionAcceptedCases = []struct{ name, src string }{
	{"a value", questionProgram("func f() (int64, error) {\n    x := parse(\"1\")?\n    return x, noerror\n}\n")},
	{"a statement", questionProgram("func f() error {\n    ready()?\n    return noerror\n}\n")},
	{
		"a copyable result dropped",
		questionProgram("func f() error {\n    parse(\"1\")?\n    return noerror\n}\n"),
	},
	{
		"two results bound",
		questionProgram("func f() (int64, error) {\n    q, r := divide(7, 2)?\n    return q +% r, noerror\n}\n"),
	},
	{
		"two results assigned",
		questionProgram("func f() (int64, error) {\n    mut q, mut r := divide(7, 2)?\n" +
			"    q, r = divide(9, 2)?\n    return q +% r, noerror\n}\n"),
	},
	{
		"a value read with no check",
		questionProgram("func f() (int64, error) {\n    return parse(\"1\")? +% 1, noerror\n}\n"),
	},
	{
		"a list moved into a variable",
		questionProgram("func f() (int64, error) {\n    l := numbers()?\n    return len(l), noerror\n}\n"),
	},
	{
		"a list returned",
		questionProgram("func f() (list[int64], error) {\n    return numbers()?, noerror\n}\n"),
	},
	{
		"builtins",
		questionProgram("func f() error {\n    mut l := make(list[int64], c_allocator)\n" +
			"    reserve(mut l, 2)?\n    append(mut l, 1)?\n    mut m := make(map[int64]int64, c_allocator)\n" +
			"    put(mut m, 1, len(l))?\n    return noerror\n}\n"),
	},
	{
		"the contract proven before",
		questionProgram("func f(mut n int64) error ensures n > 0 {\n    n = 1\n    ready()?\n" +
			"    return noerror\n}\n"),
	},
	{
		"a ? in a contract-free function after a write",
		questionProgram("func f(mut n int64) error {\n    ready()?\n    n = 1\n    return noerror\n}\n"),
	},
}

// TestAcceptsQuestion checks each of questionAcceptedCases is accepted.
func TestAcceptsQuestion(t *testing.T) {
	for _, tt := range questionAcceptedCases {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := checkTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %v, want no diagnostics", diags)
			}
		})
	}
}

// questionRejectedCases are uses of ? the checker rejects, with the error
// each gives.
var questionRejectedCases = []struct{ name, src, want string }{
	{
		"in main",
		"package main\n\nfunc ready() error {\n    return noerror\n}\n\nfunc main() {\n    ready()?\n}\n",
		"cannot use ? in main, which returns no error",
	},
	{
		"in a function returning a bool last",
		questionProgram("func f() (int64, bool) {\n    ready()?\n    return 1, true\n}\n"),
		"cannot use ? in f, whose last result is bool, not error",
	},
	{
		"on a call returning a bool last",
		questionProgram("func f() (int64, error) {\n    x := lookup(1)?\n    return x, noerror\n}\n"),
		"cannot use ? on a call to lookup, whose last result is bool, not error",
	},
	{
		"on a stored error",
		questionProgram("func f() error {\n    err := ready()\n    err?\n    return noerror\n}\n"),
		"cannot use ? on err, which is not a call",
	},
	{
		"in a defer",
		questionProgram("func f() error {\n    defer {\n        ready()?\n    }\n    return noerror\n}\n"),
		"cannot return from a defer with ?",
	},
	{
		"a list dropped",
		questionProgram("func f() error {\n    numbers()?\n    return noerror\n}\n"),
		"numbers()? gives list[int64], which cannot be dropped; bind it to a variable",
	},
	{
		"a list lent",
		questionProgram("func size(l list[int64]) int64 {\n    return len(l)\n}\n\n" +
			"func f() (int64, error) {\n    return size(numbers()?), noerror\n}\n"),
		"cannot use a temporary list[int64] here; store it in a variable first",
	},
	{
		"the contract not yet proven",
		questionProgram("func f(mut n int64) error ensures n > 0 {\n    ready()?\n    n = 1\n" +
			"    return noerror\n}\n"),
		"f ensures n > 0, which may not hold at this ?",
	},
	{
		"the contract ended by a defer",
		questionProgram("func f(mut n int64) error ensures n > 0 {\n    n = 1\n    defer {\n" +
			"        n = 0\n    }\n    ready()?\n    n = 1\n    return noerror\n}\n"),
		"f ensures n > 0, which may not hold at this ?",
	},
	{
		"two values as one",
		questionProgram("func f() (int64, error) {\n    q := divide(7, 2)?\n    return q, noerror\n}\n"),
		"divide(7, 2)? gives 2 values",
	},
	{
		"no value as one",
		questionProgram("func f() (int64, error) {\n    x := ready()?\n    return 1, x\n}\n"),
		"ready()? gives no value",
	},
	{
		"the error bound",
		questionProgram("func f() (int64, error) {\n    v, err := parse(\"1\")?\n    return v, err\n}\n"),
		"assignment mismatch: 2 variables but parse(\"1\")? gives 1 value",
	},
	{
		"on swap",
		questionProgram("func f() error {\n    mut l := make(list[int64], c_allocator)\n" +
			"    err := append(mut l, 1, 2)\n    if err.code != 0 {\n        return err\n    }\n" +
			"    swap(mut l, 0, 1)?\n    return noerror\n}\n"),
		"cannot use ? on a call to swap, which returns no error",
	},
	{
		"plain overflow on what a ? gives",
		questionProgram("func f() (int64, error) {\n    return parse(\"1\")? + 1, noerror\n}\n"),
		"parse(\"1\")? + 1 may overflow int64",
	},
}

// TestRejectsQuestion checks each of questionRejectedCases gives its
// error.
func TestRejectsQuestion(t *testing.T) {
	for _, tt := range questionRejectedCases {
		t.Run(tt.name, func(t *testing.T) {
			diags := check(t, tt.src)
			for _, d := range diags {
				if strings.HasPrefix(d.Message, tt.want) {
					return
				}
			}
			t.Errorf("got %v, want %q", diags, tt.want)
		})
	}
}

// TestQuestionRecordsResults checks the checker records on a ? what it
// gives: the type of its one value, and every result before the error.
func TestQuestionRecordsResults(t *testing.T) {
	src := questionProgram(
		"func f() (int64, error) {\n    x := parse(\"1\")?\n    q, r := divide(x, 2)?\n" +
			"    ready()?\n    return q +% r, noerror\n}\n")
	tree, diags := checkTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %v, want no diagnostics", diags)
	}
	var got []*ast.Question
	ast.Walk(tree, func(n ast.Node) bool {
		if q, ok := n.(*ast.Question); ok {
			got = append(got, q)
		}
		return true
	})
	want := []struct {
		typ     types.Type
		results int
	}{{types.Int64, 1}, {types.Invalid, 2}, {types.Invalid, 0}}
	if len(got) != len(want) {
		t.Fatalf("found %d ?s, want %d", len(got), len(want))
	}
	for i, q := range got {
		if q.Type != want[i].typ || len(q.Results) != want[i].results ||
			ast.TypeOf(q) != want[i].typ {
			t.Errorf("? %d records %v and %d results, want %v and %d",
				i, q.Type, len(q.Results), want[i].typ, want[i].results)
		}
	}
}
