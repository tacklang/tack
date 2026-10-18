package typeck_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/typeck"
)

// checkMaybeLibrary checks src as checkTree does, but as a library, with
// no main function needed, when its package is not main, and returns
// every diagnostic of the resolver and the type checker. An error before
// them aborts the test.
func checkMaybeLibrary(t *testing.T, src string) []diag.Diagnostic {
	t.Helper()
	f := &source.File{ID: "test", Filename: "case.tck", Data: []byte(src)}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatalf("lex: %s", diag.RenderAllPretty(f, diags))
	}
	file, diags := parser.NewParser(f.ID, lex.Tokens()).Parse()
	if len(diags) > 0 {
		t.Fatalf("parse: %s", diag.RenderAllPretty(f, diags))
	}
	prog := ast.SingleFile(file)
	r := resolver.New(prog, stdtest.MustBuiltins())
	r.MainOptional = !strings.HasPrefix(src, "package main\n")
	diags = r.Resolve()
	return append(diags, typeck.New(prog, f, stdtest.MustBuiltins()).Check()...)
}

// TestUnusedDeclarations checks the warnings for parameters, private
// types and private functions and methods nothing uses, and the uses and
// exemptions that keep each from one: each program gets exactly the
// warnings listed, and no error.
func TestUnusedDeclarations(t *testing.T) {
	for _, tt := range unusedDeclCases {
		t.Run(tt.name, func(t *testing.T) {
			diags := checkMaybeLibrary(t, tt.src)
			var got []string
			for _, d := range diags {
				if d.Severity != diag.Warning {
					t.Errorf("got %v %q, want only warnings", d.Severity, d.Message)
					continue
				}
				got = append(got, d.Message)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got warnings %q, want %q", got, tt.want)
			}
		})
	}
}

// unusedDeclCases are TestUnusedDeclarations' programs, each with the
// warnings it must get, in order.
var unusedDeclCases = []struct {
	name, src string
	want      []string
}{
	{"a parameter never read", `package main

func f(a int64, b int64) int64 {
    return a
}

func main() {
    if f(1, 2) == 1 {
        print("x")
    }
}
`, []string{"declared and not used: b"}},
	{"a parameter read in the ensures clause", `package main

func f(a int64) ensures a != 0 {
    if a == 0 {
        exit(1)
    }
}

func main() {
    f(1)
}
`, nil},
	{"the receiver is exempt", `package main

type T struct {
    x int64
}

func (t T) one() int64 {
    return 1
}

func main() {
    t := T{x: 1}
    if t.one() == 1 {
        print("x")
    }
}
`, nil},
	{"an extern func's parameters are exempt", `package main

extern func cput(n int64)

func main() {
    cput(1)
}
`, nil},
	{"a private extern func never called", `package main

extern func cput(n int64)

func main() {
}
`, []string{"declared and not used: cput"}},
	{"pub and lib functions' parameters are exempt", `package calc

pub func f(a int64) int64 {
    return 1
}

lib func g(a int64) int64 {
    return 2
}
`, nil},
	{"a function used as a value keeps its parameters", `package main

func apply(f func(int64) int64, n int64) int64 {
    return f(n)
}

func one(n int64) int64 {
    return 1
}

func main() {
    if apply(one, 5) == 1 {
        print("x")
    }
}
`, nil},
	{"a private function never named", `package main

func helper() int64 {
    return 1
}

func main() {
}
`, []string{"declared and not used: helper"}},
	{"a function named only by itself", `package main

func loop(n int64) int64 {
    if n == 0 {
        return 0
    }
    return loop(n - 1)
}

func main() {
}
`, []string{"declared and not used: loop"}},
	{"a function used only as a value", `package main

func one() int64 {
    return 1
}

func main() {
    f := one
    if f() == 1 {
        print("x")
    }
}
`, nil},
	{"pub and lib functions and types are exempt", `package calc

pub func f() int64 {
    return 1
}

lib func g() int64 {
    return 2
}

pub type P struct {
    x int64
}

lib type L struct {
    x int64
}
`, nil},
	{"a private type never named", `package main

type S struct {
    x int64
}

type U union {
    a int64
    none
}

type I interface {
    size() int64
}

func main() {
}
`, []string{
		"declared and not used: S",
		"declared and not used: U",
		"declared and not used: I",
	}},
	{"a type named only by its own methods", `package main

type Node struct {
    n int64
}

func (n Node) count() Node {
    return Node{n: n.n + 1}
}

func main() {
}
`, []string{"declared and not used: Node", "declared and not used: Node.count"}},
	{"a type named only in another type's field", `package main

type Inner struct {
    x int64
}

type Outer struct {
    in Inner
}

func main() {
    o := Outer{in: Inner{x: 1}}
    if o.in.x == 1 {
        print("x")
    }
}
`, nil},
	{"a type named only in a generic call's argument", `package main

type T struct {
    x int64
}

type Any interface {
}

func size[V Any](l []V) int64 {
    return len(l)
}

func main() {
    a := [1]T{T{x: 1}}
    if size(a[:]) == 1 {
        print("x")
    }
}
`, nil},
	{"a method never called", `package main

type T struct {
    x int64
}

func (t T) used() int64 {
    return t.x
}

func (t T) spare() int64 {
    return t.x + 1
}

func main() {
    t := T{x: 1}
    if t.used() == 1 {
        print("x")
    }
}
`, []string{"declared and not used: T.spare"}},
	{"a method a constraint needs at an instantiation", `package main

type Sizer interface {
    size() int64
}

type T struct {
    x int64
}

func (t T) size() int64 {
    return t.x
}

func total[S Sizer](s S) int64 {
    return s.size()
}

func main() {
    if total(T{x: 1}) == 1 {
        print("x")
    }
}
`, nil},
	{"a method a constraint needs keeps its parameters", `package main

type Scaler interface {
    scale(by int64) int64
}

type T struct {
    x int64
}

func (t T) scale(by int64) int64 {
    return t.x
}

func run[S Scaler](s S) int64 {
    return s.scale(2)
}

func main() {
    if run(T{x: 1}) == 1 {
        print("x")
    }
}
`, nil},
	{"a method a constraint needs, through an interface parameter", `package main

type Sizer interface {
    size() int64
}

type T struct {
    x int64
}

func (t T) size() int64 {
    return t.x
}

func total(s Sizer) int64 {
    return s.size()
}

func main() {
    if total(T{x: 1}) == 1 {
        print("x")
    }
}
`, nil},
	{"func main is exempt", `package main

func main() {
}
`, nil},
	{"a pub method is exempt", `package calc

pub type T struct {
    x int64
}

pub func (t T) get() int64 {
    return t.x
}
`, nil},
	{"an unused function's parameters are not reported apart", `package main

func spare(a int64) {
}

func main() {
}
`, []string{"declared and not used: spare"}},
}

// TestUnusedDeclarationsWaitForErrors checks that a program with an error
// gets no warning about an unused parameter, type or function: what an
// error left unchecked may have used it.
func TestUnusedDeclarationsWaitForErrors(t *testing.T) {
	_, diags := checkTree(t, `package main

func helper(a int64) int64 {
    return 1
}

func main() {
    var s string = 5
    print(s)
}
`)
	for _, d := range diags {
		if d.Severity == diag.Warning {
			t.Errorf("got the warning %q with an error", d.Message)
		}
	}
	if diag.ErrorCount(diags) != 1 {
		t.Errorf("got %v, want the one error", diags)
	}
}
