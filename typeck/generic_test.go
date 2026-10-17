package typeck_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// genericDecls declares interfaces, struct types satisfying them and
// generic functions, which the cases below call.
const genericDecls = `type Writer interface {
    mut write(b []uint8) error
    size() int64
}

type Sizer interface {
    size() int64
}

type Odd interface {
    take(a any, v []int64, mut p P, xs ...string) (int64, error)
}

type Empty interface {
}

type P struct {
    x int64
}

func (p P) size() int64 {
    return p.x
}

func (p P) take(a any, v []int64, mut q P, xs ...string) (int64, error) {
    return 0, noerror
}

type C struct {
    n int64
}

func (mut c C) write(b []uint8) error {
    c.n += len(b)
    return noerror
}

func (c C) size() int64 {
    return c.n
}

type L struct {
    l list[int64]
}

func (mut l L) write(b []uint8) error {
    return noerror
}

func (l L) size() int64 {
    return len(l.l)
}

func measure[S Sizer](s S) int64 {
    return s.size()
}

func render[W Writer](mut w W, s string) int64 {
    err := mut w.write([]uint8(s))
    if err.code != 0 {
        return 0
    }
    return measure(w)
}

func pair[A Sizer, B Sizer](a A, b B) int64 {
    return a.size() + b.size()
}

func use(n int64) {
}
`

// TestAcceptsGenericCode checks generic functions declared and called in
// every way the rules allow, each in a program of its own.
func TestAcceptsGenericCode(t *testing.T) {
	for _, tt := range []struct{ name, decls, body string }{
		{"two calls of one instance", "", "c := C{n: 1}\n    use(measure(c) + measure(c))"},
		{
			"one type parameter given one type twice", "func same[S Sizer](a S, b S) int64 {\n    return a.size()\n}\n",
			"c := C{n: 1}\n    use(same(c, C{n: 2}))",
		},
		{
			"a type parameter passed on as itself", "func twice[S Sizer](a S, b S) int64 {\n    return pair(a, b)\n}\n",
			"use(twice(P{x: 1}, P{x: 2}))",
		},
		{
			"a generic call nested in another's arguments", "",
			"mut c := C{n: 1}\n    l := L{l: make(list[int64], c_allocator)}\n    use(render(mut c, \"x\") + pair(c, l) + measure(l))",
		},
		{
			"a type holding a list moved after a call", "",
			"l := L{l: make(list[int64], c_allocator)}\n    use(measure(l))\n    m := l\n    use(m.size())",
		},
		{
			"an interface method of every kind of parameter",
			"func odd[O Odd](o O, mut p P) int64 {\n    v := [1]int64{2}\n    n, err := o.take(1, v, mut p, \"a\", \"b\")\n    if err.code != 0 {\n        return 0\n    }\n    return n\n}\n",
			"mut p := P{x: 1}\n    use(odd(p, mut p))",
		},
		{"a constraint with no methods", "func any1[E Empty](e E) {\n}\n", "any1(P{x: 1})\n    any1(C{n: 1})"},
		{
			"a type parameter named as a function", "func W() {\n}\n\nfunc f[W Sizer](w W) int64 {\n    return w.size()\n}\n",
			"W()\n    use(f(P{x: 1}))",
		},
		{
			"recursion swapping type arguments", "func flip[A Sizer, B Sizer](a A, b B, n int64) int64 {\n    if n == 0 {\n        return a.size()\n    }\n    return flip(b, a, n - 1)\n}\n",
			"use(flip(P{x: 1}, C{n: 2}, 3))",
		},
		{"an uncalled generic function", "func never[S Sizer](s S) int64 {\n    return s.size()\n}\n", "use(1)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n\n" + genericDecls + "\n" + tt.decls + "\nfunc main() {\n    " + tt.body + "\n}\n"
			if diags := check(t, src); len(diags) > 0 {
				t.Fatalf("rejected: %+v", diags)
			}
		})
	}
}

// instanceNames names each instance of file's program, its function and
// its type arguments, as fn[A,B].
func instanceNames(file *ast.File) []string {
	names := make([]string, 0, len(file.Instances))
	for _, inst := range file.Instances {
		args := make([]string, len(inst.TypeArgs))
		for i, a := range inst.TypeArgs {
			args[i] = file.Types.Name(a)
		}
		names = append(names, inst.Func.Name+"["+strings.Join(args, ",")+"]")
	}
	return names
}

// TestRecordsEachInstanceOnce checks the checker records every instance
// the program's calls need, once each however many calls need it, in the
// order it finds them: from the bodies of the functions that are not
// generic, in order, then from each instance's body, with the type
// parameters there standing for the instance's type arguments.
func TestRecordsEachInstanceOnce(t *testing.T) {
	src := "package main\n\n" + genericDecls + `
func flip[A Sizer, B Sizer](a A, b B, n int64) int64 {
    if n == 0 {
        return pair(a, b)
    }
    return flip(b, a, n - 1)
}

func never[S Sizer](s S) int64 {
    return measure(s)
}

func main() {
    mut c := C{n: 1}
    use(render(mut c, "x") + measure(c) + measure(c))
    use(flip(c, P{x: 1}, 3))
}
`
	file, diags := checkFile(t, src)
	if len(diags) > 0 {
		t.Fatalf("rejected: %+v", diags)
	}
	want := []string{"render[C]", "measure[C]", "flip[C,P]", "pair[C,P]", "flip[P,C]", "pair[P,C]"}
	if got := instanceNames(file); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("instances %q, want %q", got, want)
	}
}

// TestRecordsInferredTypeArgs checks a call to a generic function, sized
// here, records the type arguments it inferred, a struct type from main,
// and the caller's own type parameter inside another generic function,
// and has its callee's result type, which no type parameter can be.
func TestRecordsInferredTypeArgs(t *testing.T) {
	src := "package main\n\n" + genericDecls + `
func sized[S Sizer](s S) int64 {
    return s.size()
}

func again[S Sizer](s S) int64 {
    return sized(s)
}

func main() {
    use(sized(P{x: 1}) + again(P{x: 2}))
}
`
	file, diags := checkFile(t, src)
	if len(diags) > 0 {
		t.Fatalf("rejected: %+v", diags)
	}
	var got []string
	ast.Walk(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.Call); ok && call.TypeArgs != nil &&
			ast.Callee(call).Name == "sized" {
			got = append(
				got,
				file.Types.Name(call.TypeArgs[0])+" "+file.Types.Name(ast.TypeOf(call)),
			)
		}
		return true
	})
	if want := []string{"S int64", "P int64"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("calls inferred %q, want %q", got, want)
	}
}

// TestChecksAGenericBodyOnce checks a generic function's body is checked
// once, against its constraints, so what is wrong with it is reported
// once, whatever types its calls pass and whether any call does, and what
// is right with it holds for every call.
func TestChecksAGenericBodyOnce(t *testing.T) {
	bad := "func bad[S Sizer](s S) int64 {\n    return s.x\n}\n"
	for _, body := range []string{
		"use(1)",
		"use(bad(P{x: 1}))",
		"use(bad(P{x: 1}) + bad(C{n: 1}) + bad(P{x: 2}))",
	} {
		src := "package main\n\n" + genericDecls + "\n" + bad + "\nfunc main() {\n    " + body + "\n}\n"
		diags := check(t, src)
		if len(diags) != 1 || !strings.Contains(diags[0].Message, "S has no field x") {
			t.Errorf("main calling %s: got %+v, want one error about S", body, diags)
		}
	}
}

// TestNoInstancesInAProgramWithErrors checks a program with an error has
// no instances recorded: it is not compiled.
func TestNoInstancesInAProgramWithErrors(t *testing.T) {
	src := "package main\n\n" + genericDecls + "\nfunc main() {\n    use(measure(P{x: 1}))\n    use(\"x\")\n}\n"
	file, diags := checkFile(t, src)
	if len(diags) == 0 || file.Instances != nil {
		t.Errorf("got %d diagnostics and instances %q", len(diags), instanceNames(file))
	}
}

// TestAcceptsTypeParamResults checks a result of a generic function's own
// type parameter is allowed, whether it is the only one or one of
// several: a value of a type parameter is owned, so a body that returns
// one moves it out of where it got it, and a body that cannot get one has
// nothing to return, which the rules for returns report.
func TestAcceptsTypeParamResults(t *testing.T) {
	for _, tt := range []struct {
		name, decls string
		want        []string
	}{
		{"the only result", "func stuck[S Sizer](s S) S {\n    for {\n    }\n}\n", nil},
		{"one of several", "func duo[S Sizer](s S) (int64, S) {\n    for {\n    }\n}\n", nil},
		{"two of several", "func two[S Sizer](s S) (S, S) {\n    for {\n    }\n}\n", nil},
		{
			"a second type parameter",
			"func other[A Sizer, S Sizer](a A, s S) (int64, S) {\n    for {\n    }\n}\n",
			nil,
		},
		{
			"one popped from a list",
			"func last[S Sizer](mut l list[S], s S) S {\n    if len(l) > 0 {\n        return pop(mut l)\n    }\n" +
				"    for {\n    }\n}\n",
			nil,
		},
		{
			"a parameter, which it cannot move",
			"func same[S Sizer](s S) S {\n    return s\n}\n",
			[]string{"cannot move s: it is a parameter, which borrows its caller's value"},
		},
		{"no such result", "func sized[S Sizer](s S) (int64, bool) {\n    return s.size(), true\n}\n", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n\n" + genericDecls + "\n" + tt.decls + "\nfunc main() {\n}\n"
			var got []string
			for _, d := range check(t, src) {
				got = append(got, d.Message)
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("reported %q, want %q", got, tt.want)
			}
		})
	}
}
