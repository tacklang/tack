package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// builderDecls declares a struct holding a list and methods on it, which
// the cases below call.
const builderDecls = `type B struct {
    buf list[int64]
    n   int64
}

func newB() B {
    return B{buf: make(list[int64], c_allocator), n: 0}
}

func (b B) len() int64 {
    return len(b.buf)
}

func (mut b B) append(x int64) error {
    return append(mut b.buf, x)
}

func (b B) pair() (int64, error) {
    return b.n, noerror
}

func (mut b B) bump() {
    b.n +%= 1
}

func (b B) sum(v []int64, xs ...int64) int64 {
    return b.n
}

type W struct {
    b  B
    bs [2]P
}

type P struct {
    x int64
}

func (p P) get() int64 {
    return p.x
}

func (mut p P) set(x int64) {
    p.x = x
}

func use(n int64) {
}`

// methodCases are programs calling methods that must check cleanly.
var methodCases = []struct{ name, src string }{
	{"plain on a local", withTypes(builderDecls, "b := newB()", "use(b.len())")},
	{"mut on a local", withTypes(builderDecls, "mut b := newB()", "mut b.bump()", "use(b.n)")},
	{"error checked", withTypes(builderDecls, "mut b := newB()", "err := mut b.append(1)",
		"if err.code != 0 {", "    return", "}")},
	{"result beside an error", withTypes(builderDecls, "b := newB()", "n, err := b.pair()",
		"if err.code == 0 {", "    use(n)", "}")},
	{"checked in a loop before a read", withTypes(builderDecls, "b := newB()", "mut n int64 = 0",
		"mut err := noerror", "for range 2 {", "    if err.code == 0 {", "        use(n)",
		"    }", "    n, err = b.pair()", "}")},
	{"through a field", withTypes(builderDecls, "mut w := W{b: newB(), bs: [2]P{P{x: 1}, P{x: 2}}}",
		"mut w.b.bump()", "mut w.bs[1].set(3)", "use(w.b.len() +% w.bs[0].get())")},
	{"element at a proven index", withTypes(builderDecls, "mut ps := [2]P{P{x: 1}, P{x: 2}}",
		"for i := range ps {", "    mut ps[i].set(i)", "}")},
	{"temporary plain receiver", withTypes(builderDecls, "use(P{x: 1}.get())")},
	{"view and variadic", withTypes(builderDecls, "b := newB()", "a := [2]int64{1, 2}",
		"use(b.sum(a, 1, 2) +% b.sum(b.buf) +% b.sum(a, a[:]...))")},
	{"plain argument reading a mut receiver", withTypes(builderDecls,
		"mut p := P{x: 1}", "mut p.set(p.get())")},
	{"two fields of one struct", withTypes(builderDecls,
		"mut w := W{b: newB(), bs: [2]P{P{x: 1}, P{x: 2}}}", "mut w.bs[0].set(w.b.len())")},
	{"distinct elements", withTypes(builderDecls+"\n\nfunc (mut p P) swap(mut q P) {\n}",
		"mut ps := [2]P{P{x: 1}, P{x: 2}}", "mut ps[0].swap(mut ps[1])")},
	{"mut parameter as receiver", withTypes(builderDecls +
		"\n\nfunc grow(mut b B) {\n    mut b.bump()\n    use(b.len())\n}")},
	{"plain parameter as plain receiver", withTypes(builderDecls +
		"\n\nfunc size(b B) int64 {\n    return b.len()\n}")},
	{"receiver inside a defer", withTypes(builderDecls, "mut b := newB()", "defer {",
		"    mut b.bump()", "}")},
	{"methods calling methods declared later", withTypes(
		"type T struct {\n    n int64\n}\n\nfunc (t T) a() int64 {\n    return t.b() +% 1\n}\n\n"+
			"func (t T) b() int64 {\n    if t.n > 0 {\n        return 0\n    }\n    return t.a()\n}\n\n"+
			"func use(n int64) {\n}",
		"t := T{n: 1}", "use(t.a())")},
}

// TestAcceptsMethodCalls checks each of methodCases checks cleanly.
func TestAcceptsMethodCalls(t *testing.T) {
	for _, tc := range methodCases {
		t.Run(tc.name, func(t *testing.T) {
			if diags := check(t, tc.src); len(diags) > 0 {
				t.Errorf("reported %v", diags)
			}
		})
	}
}

// TestRecordsTheMethodACallCalls checks the checker records the method a
// call calls, from its receiver's type, and that the call's type is the
// method's result.
func TestRecordsTheMethodACallCalls(t *testing.T) {
	file, diags := checkFile(t, withTypes(builderDecls, "b := newB()", "n := b.len()", "use(n)"))
	if len(diags) > 0 {
		t.Fatalf("reported %v", diags)
	}
	var call *ast.Call
	ast.Walk(file, func(n ast.Node) bool {
		if c, ok := n.(*ast.Call); ok && c.Receiver() != nil {
			call = c
		}
		return true
	})
	if call == nil || call.Method == nil || call.Method.Name != "len" || !call.Method.Method {
		t.Fatalf("method call %v records %v", call, call.Method)
	}
	if ast.Callee(call) != call.Method {
		t.Error("Callee is not the method")
	}
	if got := ast.TypeOf(call); got != types.Int64 {
		t.Errorf("call's type is %v, want int64", got)
	}
}

// TestMethodMessagesNameTheType checks a message about a method's call or
// declaration names it by its receiver's type and its name.
func TestMethodMessagesNameTheType(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{withTypes(builderDecls, "b := newB()", "b.len(1)"), "B.len expects 0 argument(s), got 1"},
		{withTypes(builderDecls, "mut b := newB()", "x := mut b.bump()", "use(x)"), "B.bump returns no value"},
		{withTypes(builderDecls, "b := newB()", "x := b.pair()", "use(x)"), "B.pair returns 2 values"},
		{
			withTypes(builderDecls, "mut b := newB()", "b.bump()"),
			"B.bump takes its receiver as mut; call mut b.bump()",
		},
		{
			withTypes(builderDecls, "mut b := newB()", "use(mut b.len())"),
			"B.len does not take its receiver as mut; call b.len()",
		},
		{withTypes(builderDecls, "b := newB()", "use(b.nope())"), "B has no method nope"},
		{
			withTypes(builderDecls, "b := newB()", "use(b.n())"),
			"n is a field of B, not a method; a field cannot be called",
		},
		{
			withTypes(builderDecls, "b := newB()", "x := b.len", "use(x)"),
			"len is a method of B, not a field; a method can only be called",
		},
		{withTypes(builderDecls, "use(newB().sum(1))"), "cannot use int64 as []int64 in argument to B.sum"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			diags := check(t, tc.src)
			if len(diags) == 0 || !strings.Contains(diags[0].Message, tc.want) {
				t.Errorf("reported %v, want %q", diags, tc.want)
			}
		})
	}
}
