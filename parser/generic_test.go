package parser_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestParsesTypeParams checks a function's type parameters, in brackets
// after its name, each a name and a constraint, plain or qualified, with
// commas between them optional as between parameters, and a trailing one
// allowed.
func TestParsesTypeParams(t *testing.T) {
	for _, tt := range []struct {
		name, decl, span    string
		params, constraints []string
	}{
		{"one", "func f[W Writer](w W) {\n}\n", "[W Writer]", []string{"W"}, []string{"Writer"}},
		{
			"two", "func f[A Writer, B s.Sizer](a A, b B) {\n}\n", "[A Writer, B s.Sizer]",
			[]string{"A", "B"},
			[]string{"Writer", "s.Sizer"},
		},
		{"no comma", "func f[A I B J](a A) {\n}\n", "[A I B J]", []string{"A", "B"}, []string{"I", "J"}},
		{"trailing comma", "func f[A I,](a A) {\n}\n", "[A I,]", []string{"A"}, []string{"I"}},
		{"pub", "pub func F[A I](a A) A {\n    return a\n}\n", "[A I]", []string{"A"}, []string{"I"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n\n" + tt.decl
			file, diags := parse(t, src)
			if len(diags) > 0 {
				t.Fatalf("parse reported %+v", diags)
			}
			fn := file.Decls[0].(*ast.Func)
			if !fn.Generic() || len(fn.TypeParams) != len(tt.params) {
				t.Fatalf("got %d type parameters, want %d", len(fn.TypeParams), len(tt.params))
			}
			for i, tp := range fn.TypeParams {
				c, _ := tp.Constraint.(*ast.Ident)
				if tp.Name != tt.params[i] || c == nil || c.Qualified() != tt.constraints[i] {
					t.Errorf("type parameter %d is %s %v, want %s %s",
						i, tp.Name, tp.Constraint, tt.params[i], tt.constraints[i])
				}
			}
			if got := src[fn.TypeParamsSpan.Start:fn.TypeParamsSpan.End]; got != tt.span {
				t.Errorf("type parameters cover %q, want %q", got, tt.span)
			}
		})
	}
}

// TestParsesTypeArgs checks a call that writes type arguments, f[T](x),
// in a statement and as an operand, plain or qualified, each a type of any
// form, list[int64] and map[text.Text]int64 among them, which an index
// could not hold, and the span of the brackets around them.
func TestParsesTypeArgs(t *testing.T) {
	for _, tt := range []struct {
		name, body, callee, brackets string
		args                         []string
	}{
		{"one", "x := f[T](a)", "f", "[T]", []string{"T"}},
		{"statement", "f[T](a)", "f", "[T]", []string{"T"}},
		{"two", "x := f[A, B](a, b)", "f", "[A, B]", []string{"A", "B"}},
		{"qualified", "x := p.f[A](a)", "p.f", "[A]", []string{"A"}},
		{"qualified statement", "p.f[A, B]()", "p.f", "[A, B]", []string{"A", "B"}},
		{"qualified type", "x := f[p.T]()", "f", "[p.T]", []string{"p.T"}},
		{"list", "x := f[list[int64]](a)", "f", "[list[int64]]", []string{"list[int64]"}},
		{
			"map", "x := f[map[p.Text]int64](a)", "f", "[map[p.Text]int64]",
			[]string{"map[p.Text]int64"},
		},
		{"view", "x := f[[]int64]()", "f", "[[]int64]", []string{"[]int64"}},
		{"array", "x := f[[4]uint8]()", "f", "[[4]uint8]", []string{"[4]uint8"}},
		{"func", "x := f[func(int64) bool]()", "f", "[func(int64) bool]", []string{"func(int64) bool"}},
		{"nested call", "x := f[A](g[B](b))", "f", "[A]", []string{"A"}},
		{"question", "x := f[A](a)?", "f", "[A]", []string{"A"}},
		{"in if", "if f[A](a) {\n    }", "f", "[A]", []string{"A"}},
		{"method on result", "f[A](a).m()", "f", "[A]", []string{"A"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n\nimport p from \"m/p\"\n\nfunc main() {\n    " + tt.body + "\n}\n"
			call := firstTypeArgsCall(t, src)
			if id, ok := call.Fn.(*ast.Ident); !ok || id.Qualified() != tt.callee {
				t.Errorf("callee is %v, want %s", call.Fn, tt.callee)
			}
			if len(call.TypeArgExprs) != len(tt.args) {
				t.Fatalf("got %d type arguments, want %d", len(call.TypeArgExprs), len(tt.args))
			}
			for i, arg := range call.TypeArgExprs {
				span := arg.Spanned()
				if got := src[span.Start:span.End]; got != tt.args[i] {
					t.Errorf("type argument %d covers %q, want %q", i, got, tt.args[i])
				}
			}
			if got := src[call.TypeArgsSpan.Start:call.TypeArgsSpan.End]; got != tt.brackets {
				t.Errorf("brackets cover %q, want %q", got, tt.brackets)
			}
		})
	}
}

// firstTypeArgsCall parses src, which must parse with nothing reported,
// and returns the first call in it that writes type arguments, failing t
// when there is none.
func firstTypeArgsCall(t *testing.T, src string) *ast.Call {
	t.Helper()
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	var call *ast.Call
	ast.Walk(file, func(n ast.Node) bool {
		if c, ok := n.(*ast.Call); ok && call == nil && c.TypeArgExprs != nil {
			call = c
		}
		return true
	})
	if call == nil {
		t.Fatalf("no call with type arguments in %q", src)
	}
	return call
}

// TestParsesInterfaces checks an interface type's methods, one per line
// or separated by semicolons, each mut or not, with parameters and
// results as a function's.
func TestParsesInterfaces(t *testing.T) {
	src := "package main\n\ntype W interface {\n    mut write(b []uint8) error\n" +
		"    size() int64; get(i int64, mut x P, xs ...string) (int64, error)\n}\n"
	file, diags := parse(t, src)
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	w := file.Decls[0].(*ast.TypeDecl).Interface()
	if w == nil || len(w.Methods) != 3 {
		t.Fatalf("got %v, want an interface of 3 methods", file.Decls[0])
	}
	for i, want := range []string{
		"mut write(b []uint8) error 1 1",
		"size() int64 0 1",
		"get(i int64, mut x P, xs ...string) (int64, error) 3 2",
	} {
		m := w.Methods[i]
		got := fmt.Sprintf("%s %d %d", src[m.Span.Start:m.Span.End], len(m.Params), len(m.Results))
		if got != want || m.Mut != strings.HasPrefix(want, "mut ") ||
			!strings.Contains(want, m.Name+"(") {
			t.Errorf(
				"method %d %s, mut=%v, covers and counts %q, want %q",
				i,
				m.Name,
				m.Mut,
				got,
				want,
			)
		}
	}
}

// TestParsesEmptyInterfaces checks an interface may ask for no method,
// its braces on two lines or one.
func TestParsesEmptyInterfaces(t *testing.T) {
	file, diags := parse(t, "package main\n\ntype E interface {\n}\n\ntype F interface {}\n")
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	for _, d := range file.Decls {
		if it := d.(*ast.TypeDecl).Interface(); it == nil || len(it.Methods) != 0 {
			t.Errorf("got %v, want an empty interface", d)
		}
	}
}

// genericParseErrors are malformed type parameters, interfaces and calls
// with type arguments, each with the message the parser must give and the
// source it must cover, "" for a point.
var genericParseErrors = []struct{ name, src, msg, at string }{
	{"empty type parameters", "func f[]() {\n}\n", "empty type parameter list", "[]"},
	{
		"no constraint",
		"func f[W](w W) {\n}\n",
		"type parameter W has no constraint; write the interface it must satisfy after it, as in [W Writer]",
		"W",
	},
	{
		"no constraint before another",
		"func f[W, V I](w W) {\n}\n",
		"type parameter W has no constraint; write the interface it must satisfy after it, as in [W Writer]",
		"W",
	},
	{
		"unclosed type parameters",
		"func f[W I(w W) {\n}\n",
		"expected 'type parameter name' found '('",
		"",
	},
	{
		"mut type parameter",
		"func f[mut W I](w W) {\n}\n",
		"expected 'type parameter name' found 'mut'",
		"",
	},
	{
		"variadic constraint",
		"func f[W ...I](w W) {\n}\n",
		"can only use ... with the final parameter",
		"...",
	},
	{
		"method type parameters", "func (b B) f[W I](w W) {\n}\n",
		"a method cannot have type parameters; only a function can be generic", "[W I]",
	},
	{
		"extern type parameters", "extern func f[W I](w W)\n",
		"extern func f cannot have type parameters; its body is written in C", "[W I]",
	},
	{
		"empty type arguments", "func main() {\n    x := f[](a)\n}\n",
		"empty type argument list", "[]",
	},
	{
		"empty type arguments statement", "func main() {\n    f[](a)\n}\n",
		"empty type argument list", "[]",
	},
	{
		"trailing comma in type arguments", "func main() {\n    x := f[A,](a)\n}\n",
		"expected 'type name' found ']'", "",
	},
	{
		"no comma between type arguments", "func main() {\n    x := f[A B](a)\n}\n",
		"expected ']' found 'B'", "",
	},
	{
		"element called",
		"func main() {\n    x := fs[0](a)\n}\n",
		"cannot call an element, fs[...](...); only a name is called: bind it first, as in g := fs[...]",
		"fs[0]",
	},
	{
		"method type arguments",
		"func main() {\n    x := b.m[A](a)\n}\n",
		"cannot call b.m[...](...): only a name is called, and only a function can be generic",
		"b.m[A]",
	},
	{
		"method type arguments statement", "func main() {\n    b.c.m[A, B](a)\n}\n",
		"cannot call b.c.m[...](...): only a name is called, and only a function can be generic",
		"b.c.m[A, B]",
	},
	{
		"method expression type arguments",
		"func main() {\n    x := Pair.first[A](p)\n}\n",
		"cannot call Pair.first[...](...): only a name is called, and only a function can be generic",
		"Pair.first[A]",
	},
	{
		"element of a field called",
		"func main() {\n    x := s.fs[0](a)\n}\n",
		"cannot call an element, s.fs[...](...); only a name is called: bind it first, as in g := s.fs[...]",
		"s.fs[0]",
	},
	{
		"element of a call's result called",
		"func main() {\n    x := f().fs[-1](a)\n}\n",
		"cannot call an element, (...).fs[...](...); only a name is called: bind it first, as in g := (...).fs[...]",
		"f().fs[-1]",
	},
	{
		"qualified element called",
		"import p from \"m/p\"\n\nfunc main() {\n    x := p.fs[\"k\"](a)\n}\n",
		"cannot call an element, p.fs[...](...); only a name is called: bind it first, as in g := p.fs[...]",
		"p.fs[\"k\"]",
	},
	{
		"unclosed type arguments", "func main() {\n    x := f[A(a)\n}\n",
		"expected ']' found ';'", "",
	},
	{
		"embedded interface",
		"type W interface {\n    S\n}\n",
		"embedded interfaces are not supported",
		"S",
	},
	{
		"embedded qualified",
		"type W interface {\n    p.S\n}\n",
		"embedded interfaces are not supported",
		"p",
	},
	{
		"field",
		"type W interface {\n    n int64\n}\n",
		"an interface cannot have fields; it holds only methods",
		"n",
	},
	{
		"pub method", "type W interface {\n    pub f()\n}\n",
		"an interface's method cannot be pub; it is as visible as its interface", "pub",
	},
	{
		"lib method", "type W interface {\n    lib f()\n}\n",
		"an interface's method cannot be lib; it is as visible as its interface", "lib",
	},
	{
		"method body",
		"type W interface {\n    f() {\n    }\n}\n",
		"interface method f cannot have a body",
		"{",
	},
	{
		"func keyword",
		"type W interface {\n    func f()\n}\n",
		"expected 'method name' found 'func'",
		"",
	},
	{
		"unclosed interface",
		"type W interface {\n    f()\n",
		"expected 'method name' found 'EOF'",
		"",
	},
	{"no brace", "type W interface\n", "expected '{' found 'EOF'", ""},
	{"two methods on a line", "type W interface {\n    f() g()\n}\n", "expected ';' found '('", ""},
	{
		"named type",
		"type C int64\n",
		"only struct, interface and union types can be declared",
		"int64",
	},
}

// TestRejectsMalformedGenerics checks each malformed generic function,
// interface and call is reported, alone, with its message, where it
// belongs.
func TestRejectsMalformedGenerics(t *testing.T) {
	for _, tt := range genericParseErrors {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n\n" + tt.src
			file, diags := parse(t, src)
			if file == nil || countBad(file) == 0 || len(diags) != 1 {
				t.Fatalf("got %v, %v", file, diags)
			}
			if diags[0].Message != tt.msg {
				t.Errorf("message = %q, want %q", diags[0].Message, tt.msg)
			}
			if tt.at != "" {
				if got := src[diags[0].Span.Start:diags[0].Span.End]; got != tt.at {
					t.Errorf("covers %q, want %q", got, tt.at)
				}
			}
		})
	}
}

// genericSeeds are sources about generics the fuzzer starts from.
var genericSeeds = []string{
	"package main\ntype W interface {\nmut write(b []uint8) error\nsize() int64\n}\n" +
		"func f[A W, B W](mut a A, b B) (A, error) { x := g(b)\nreturn x, mut a.write([]uint8(\"x\")) }\n",
	"package main\nfunc main() { f[A](x) }\n",
	"package main\nfunc sort[T Any](mut l list[T], lt func(T, T) bool, v []T, m map[string]T) T {\n" +
		"swap(mut l, 0, 1)\nreturn pop(mut l) }\n",
	"package main\nfunc main() { x := p.f[A, B[C]](y) }\n",
	"package main\nimport p from \"m/p\"\nfunc main() { x := p.f[list[int64], map[p.T]func(A) B](y)?\n" +
		"f[[]A, [3]B](g[C](z)).m()\nif h[D](x) { k[E]() } }\n",
	"package main\nfunc main() { f[",
	"package main\nfunc main() { f[A",
	"package main\nfunc main() { f[A](",
	"package main\nfunc main() { x := f[A][B](c) }\n",
	"package main\nfunc main() { x := b.m[A](c)\ny := f[A]\n}\n",
	"package main\nfunc main() { x := fs[0](1)\ny := s.fs[i](2)\nm[k](3)\nz := f().g[-1](4) }\n",
	"package main\ntype W interface {",
	"package main\ntype W interface { mut",
	"package main\nfunc f[",
	"package main\nfunc f[A",
	"package main\nfunc f[A I,,] () {}\n",
	"package main\ntype W interface { f(x ...int64) (int64, error); mut g() }\n",
	"package main\nfunc f[T I](t T) bool { g := W.size\nreturn h(t, T.less, Pair.first) && T.m(t) }\n",
}

// FuzzParseGenerics parses arbitrary source, seeded with generic
// functions, interfaces and calls with type arguments, and checks the
// parser never panics, gives a tree exactly when it reports nothing, and
// reports each problem at a span inside the source.
func FuzzParseGenerics(f *testing.F) {
	for _, src := range genericSeeds {
		f.Add(src)
	}
	for _, tt := range genericParseErrors {
		f.Add("package main\n\n" + tt.src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		checkParse(t, src)
	})
}
