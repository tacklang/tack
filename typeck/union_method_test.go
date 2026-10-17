package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/types"
)

// unionMethodDecls declares unions with methods, one holding a list, an
// interface they satisfy beside a struct, and generic functions taking
// them, which the cases below call.
const unionMethodDecls = `type Sizer interface {
    size() int64
}

type Resetter interface {
    mut reset()
}

type E union {
    int int64
    eof
}

func (e E) size() int64 {
    switch e {
    case int(n):
        return n
    case eof:
        return 0
    }
}

func (e E) read(n int64) int64 {
    return n
}

func (e E) len() int64 {
    return 1
}

func (mut e E) reset() {
    e = E.eof
}

func (mut e E) bump() {
    mut next := e
    switch e {
    case int(n):
        next = E.int(n + 1)
    case eof:
        e = E.int(0)
        return
    }
    e = next
}

type V union {
    items list[int64]
    none
}

func (v V) size() int64 {
    switch v {
    case items(l):
        return len(l)
    case none:
        return 0
    }
}

func (mut v V) append(x int64) error {
    v = V.none
    return noerror
}

func (mut v V) reset() {
    v = V.none
}

type P struct {
    x int64
}

func (p P) size() int64 {
    return p.x
}

func measure[S Sizer](s S) int64 {
    return s.size()
}

func pair[A Sizer, B Sizer](a A, b B) int64 {
    return a.size() + b.size()
}

func wipe[R Resetter](mut r R) {
    mut r.reset()
}

func use(n int64) {
}`

// TestAcceptsUnionMethods checks methods on unions declared and called in
// every way the rules allow, and unions passed to generic functions, each
// in a program of its own.
func TestAcceptsUnionMethods(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"plain and mut methods", withTypes(unionMethodDecls, "mut e := E.int(1)", "mut e.bump()",
			"mut e.reset()", "use(e.size() + e.len())")},
		{"methods on built unions", withTypes(unionMethodDecls, "use(E.int(2).size() + E.eof.len())")},
		{"a builtin-named mut method", withTypes(unionMethodDecls,
			"mut v := V.items(make(list[int64], c_allocator))", "err := mut v.append(1)",
			"use(err.code + v.size())")},
		{"a plain call with a binding inside its clause", withTypes(unionMethodDecls,
			"mut e := E.int(1)", "switch e {", "case int(n):", "    use(e.read(n))", "default:",
			"    mut e.reset()", "}")},
		{"methods on a field and an element", withTypes(unionMethodDecls+
			"\n\ntype H struct {\n    e E\n}",
			"mut s := H{e: E.eof}", "mut es := [2]E{E.eof, E.int(1)}", "mut s.e.bump()",
			"mut es[1].reset()", "use(s.e.size() + es[0].size())")},
		{"unions and a struct through one generic function", withTypes(unionMethodDecls,
			"v := V.none", "use(measure(E.int(1)) + measure(v) + measure(P{x: 1}))",
			"use(pair(E.eof, v) + pair(v, P{x: 2}))")},
		{"a union's mut method through a generic function", withTypes(unionMethodDecls,
			"mut e := E.int(1)", "mut v := V.none", "wipe(mut e)", "wipe(mut v)", "use(e.size())")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if diags := check(t, tc.src); len(diags) > 0 {
				t.Errorf("reported %v", diags)
			}
		})
	}
}

// TestRecordsUnionMethodCalls checks a method call on a union records the
// union's method, and that a union's instances of generic functions are
// recorded beside a struct's, once each.
func TestRecordsUnionMethodCalls(t *testing.T) {
	file, diags := checkFile(t, withTypes(unionMethodDecls, "e := E.int(1)", "use(e.size())",
		"use(measure(e) + measure(P{x: 1}) + measure(E.eof) + pair(e, P{x: 2}))"))
	if len(diags) > 0 {
		t.Fatalf("reported %v", diags)
	}
	call := methodCallOn(file, "e")
	if call == nil {
		t.Fatal("no method call on e")
	}
	if m := call.Method; m == nil || m.RecvType == nil || m.RecvType.Name != "E" {
		t.Fatalf("method call %v records %v", call, m)
	}
	if got := ast.TypeOf(call); got != types.Int64 {
		t.Errorf("call's type is %v, want int64", got)
	}
	want := []string{"measure[E]", "measure[P]", "pair[E,P]"}
	if got := instanceNames(file); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("instances %q, want %q", got, want)
	}
}

// methodCallOn returns the first method call in file whose receiver is
// the name recv, or nil when it has none.
func methodCallOn(file *ast.File, recv string) *ast.Call {
	var call *ast.Call
	ast.Walk(file, func(n ast.Node) bool {
		c, ok := n.(*ast.Call)
		if !ok || call != nil {
			return call == nil
		}
		if id, isName := c.Receiver().(*ast.Ident); isName && id.Name == recv {
			call = c
		}
		return true
	})
	return call
}

// TestUnionMethodMessages checks a message about a union's method names it
// by the union and the method, and that a union's case is no method.
func TestUnionMethodMessages(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{withTypes(unionMethodDecls, "e := E.eof", "use(e.size(1))"), "E.size expects 0 argument(s), got 1"},
		{withTypes(unionMethodDecls, "mut e := E.eof", "e.bump()"), "E.bump takes its receiver as mut; call mut e.bump()"},
		{withTypes(unionMethodDecls, "mut e := E.eof", "use(mut e.size())"), "E.size does not take its receiver as mut; call e.size()"},
		{withTypes(unionMethodDecls, "e := E.eof", "use(e.int(1))"), "E has no method int"},
		{withTypes(unionMethodDecls, "e := E.eof", "x := e.size", "use(x)"), "size is a method of E, not a field; a method can only be called"},
		{withTypes(unionMethodDecls, "E.eof.bump()"), "E.bump takes its receiver as mut; only a variable, a field or an element can be passed as mut"},
		{withTypes(unionMethodDecls, "mut E.eof.bump()"), "only a variable, a field or an element can be passed as mut"},
		{withTypes(unionMethodDecls, "use(measure(1))"), "int64 does not satisfy Sizer: int64 has no method size"},
		{
			withTypes(unionMethodDecls, "mut e := E.int(1)", "switch e {", "case int(n):", "    mut e.reset()", "    use(n)", "default:", "}"),
			"e is being matched, and n refers into it, so this case cannot pass mut e",
		},
	} {
		t.Run(tc.want, func(t *testing.T) {
			diags := check(t, tc.src)
			if len(diags) == 0 || !strings.Contains(diags[0].Message, tc.want) {
				t.Errorf("reported %v, want %q", diags, tc.want)
			}
		})
	}
}
