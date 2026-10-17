package parser_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
)

// methodDecls are method declarations, each with its name, its
// receiver's name and text, how many parameters it has, the receiver
// among them, its visibility and whether its receiver is mut.
var methodDecls = []struct {
	decl, name, recv, recvSpan string
	params                     int
	vis                        ast.Visibility
	mut                        bool
}{
	{
		"func (b Builder) len() int64 {\n    return 0\n}\n",
		"len",
		"b",
		"b Builder",
		1,
		ast.Private,
		false,
	},
	{
		"func (mut b Builder) write(s string) error {\n    return noerror\n}\n",
		"write",
		"b",
		"mut b Builder",
		2,
		ast.Private,
		true,
	},
	{
		"pub func (c Counter) Add(k int64, j int64) {\n}\n",
		"Add",
		"c",
		"c Counter",
		3,
		ast.Pub,
		false,
	},
	{
		"lib func (mut t lexer.Token) kind() {\n}\n",
		"kind",
		"t",
		"mut t lexer.Token",
		1,
		ast.Lib,
		true,
	},
	{"func (b B,) trailing() {\n}\n", "trailing", "b", "b B", 1, ast.Private, false},
	{"func (b B) variadic(xs ...int64) {\n}\n", "variadic", "b", "b B", 2, ast.Private, false},
	{"func (print B) print() {\n}\n", "print", "print", "print B", 1, ast.Private, false},
}

// TestParsesMethodDecls checks that a receiver before a function's name
// makes the function a method, with the receiver as its first parameter,
// mut or plain, and the parameters written after the name following it.
func TestParsesMethodDecls(t *testing.T) {
	for _, tt := range methodDecls {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n\n" + tt.decl
			file, diags := parse(t, src)
			if len(diags) > 0 {
				t.Fatalf("parse reported %+v", diags)
			}
			fn := file.Decls[0].(*ast.Func)
			if !fn.Method || fn.Name != tt.name || fn.Vis != tt.vis {
				t.Fatalf("got method=%v name=%s vis=%s, want a %s method %s",
					fn.Method, fn.Name, fn.Vis, tt.vis, tt.name)
			}
			if len(fn.Params) != tt.params {
				t.Fatalf("got %d parameters, want %d", len(fn.Params), tt.params)
			}
			checkReceiver(t, src, fn, tt.recv, tt.recvSpan, tt.mut)
			if got := src[fn.NameSpan.Start:fn.NameSpan.End]; got != tt.name {
				t.Errorf("name spans %q, want %q", got, tt.name)
			}
			if got := src[fn.Span.Start:fn.Span.End]; got != strings.TrimSuffix(tt.decl, "\n") {
				t.Errorf("method spans %q", got)
			}
		})
	}
}

// checkReceiver fails the test unless fn's receiver, in src, is its first
// parameter, named name, written as span, and mut as mut says.
func checkReceiver(t *testing.T, src string, fn *ast.Func, name, span string, mut bool) {
	t.Helper()
	recv := fn.Recv()
	if recv != fn.Params[0] || recv.Name != name || recv.Mut != mut {
		t.Errorf("receiver %s mut=%v, want %s mut=%v", recv.Name, recv.Mut, name, mut)
	}
	if got := src[recv.Span.Start:recv.Span.End]; got != span {
		t.Errorf("receiver spans %q, want %q", got, span)
	}
}

// TestFunctionIsNotAMethod checks that a function without a receiver is
// not a method and has no receiver.
func TestFunctionIsNotAMethod(t *testing.T) {
	file, diags := parse(t, "package main\n\nfunc f(b B) {\n}\n")
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	fn := file.Decls[0].(*ast.Func)
	if fn.Method || fn.Recv() != nil {
		t.Errorf("f parsed as a method, receiver %v", fn.Recv())
	}
}

// methodCall describes the method call a statement holds: its receiver's
// text, whether the receiver is mut, its method's name, how many
// arguments it has, and the call's own text.
type methodCall struct {
	recv, name, call string
	args             int
	mut, spread      bool
}

// methodCallCases are statements holding a method call, each with the
// call it must parse to.
var methodCallCases = []struct {
	body string
	want methodCall
}{
	{"b.len()", methodCall{"b", "len", "b.len()", 0, false, false}},
	{"mut b.write(s)", methodCall{"b", "write", "mut b.write(s)", 1, true, false}},
	{
		"mut w.out.write(s, 1)",
		methodCall{"w.out", "write", "mut w.out.write(s, 1)", 2, true, false},
	},
	{
		"mut a[i].p.scale(2)",
		methodCall{"a[i].p", "scale", "mut a[i].p.scale(2)", 1, true, false},
	},
	{"x := b.len()", methodCall{"b", "len", "b.len()", 0, false, false}},
	{"err := mut b.write(s)", methodCall{"b", "write", "mut b.write(s)", 1, true, false}},
	{"x, err := mut b.next()", methodCall{"b", "next", "mut b.next()", 0, true, false}},
	{"x = makeP().norm()", methodCall{"makeP()", "norm", "makeP().norm()", 0, false, false}},
	{
		"x = (P{x: 1}).norm()",
		methodCall{"P{x: 1}", "norm", "(P{x: 1}).norm()", 0, false, false},
	},
	{"x = P{x: 1}.norm()", methodCall{"P{x: 1}", "norm", "P{x: 1}.norm()", 0, false, false}},
	{"x = b.f().g()", methodCall{"b.f()", "g", "b.f().g()", 0, false, false}},
	{"makeP().scale(2)", methodCall{"makeP()", "scale", "makeP().scale(2)", 1, false, false}},
	{
		"mut makeP().scale(2)",
		methodCall{"makeP()", "scale", "mut makeP().scale(2)", 1, true, false},
	},
	{
		"a[f(1)].p.g(h(2), 3)",
		methodCall{"a[f(1)].p", "g", "a[f(1)].p.g(h(2), 3)", 2, false, false},
	},
	{"b.f(1).g(2).h(3)", methodCall{"b.f(1).g(2)", "h", "b.f(1).g(2).h(3)", 1, false, false}},
	{"mut b.f().g()", methodCall{"b.f()", "g", "mut b.f().g()", 0, true, false}},
	{"mut b.all(xs...)", methodCall{"b", "all", "mut b.all(xs...)", 1, true, true}},
	{"lexer.T.f()", methodCall{"lexer.T", "f", "lexer.T.f()", 0, false, false}},
	{"t.kind()", methodCall{"t", "kind", "t.kind()", 0, false, false}},
	{"x = -b.len()", methodCall{"b", "len", "b.len()", 0, false, false}},
	{"return mut b.write(s)", methodCall{"b", "write", "mut b.write(s)", 1, true, false}},
	{"if mut it.next() {\n    }", methodCall{"it", "next", "mut it.next()", 0, true, false}},
	{"for mut it.next() {\n    }", methodCall{"it", "next", "mut it.next()", 0, true, false}},
	{
		"for mut b.reset(); i < 3; i++ {\n    }",
		methodCall{"b", "reset", "mut b.reset()", 0, true, false},
	},
	{"switch mut b.kind() {\n    }", methodCall{"b", "kind", "mut b.kind()", 0, true, false}},
	{
		"defer {\n        mut b.close()\n    }",
		methodCall{"b", "close", "mut b.close()", 0, true, false},
	},
}

// TestParsesMethodCalls checks a method call wherever a call can be, as a
// statement, a value and a condition, with mut covering the receiver, the
// place before the last .name(, and a call or literal as a receiver.
func TestParsesMethodCalls(t *testing.T) {
	for _, tt := range methodCallCases {
		t.Run(tt.body, func(t *testing.T) {
			src := "package main\n\nimport lexer from \"m/lexer\"\n\nfunc main() {\n    " + tt.body + "\n}\n"
			file, diags := parse(t, src)
			if len(diags) > 0 {
				t.Fatalf("parse reported %+v", diags)
			}
			call := firstMethodCall(file)
			if call == nil {
				t.Fatal("no method call in the tree")
			}
			sel := call.Fn.(*ast.Selector)
			recv := call.Receiver()
			m, isMut := recv.(*ast.MutArg)
			if isMut {
				recv = m.X
				if got := src[m.Span.Start:m.Span.End]; got != "mut "+tt.want.recv {
					t.Errorf("mut receiver spans %q, want %q", got, "mut "+tt.want.recv)
				}
			}
			got := methodCall{
				recv:   strings.Trim(src[recv.Spanned().Start:recv.Spanned().End], "()"),
				name:   sel.Name,
				call:   src[call.Span.Start:call.Span.End],
				args:   len(call.Args),
				mut:    isMut,
				spread: call.Spread,
			}
			if got.recv != tt.want.recv &&
				src[recv.Spanned().Start:recv.Spanned().End] != tt.want.recv {
				t.Errorf("receiver %q, want %q", got.recv, tt.want.recv)
			}
			got.recv = tt.want.recv
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// firstMethodCall returns the outermost method call in file, the first in
// source order, or nil.
func firstMethodCall(file *ast.File) *ast.Call {
	var found *ast.Call
	ast.Walk(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.Call); ok && call.Receiver() != nil && found == nil {
			found = call
		}
		return found == nil
	})
	return found
}

// TestMutCallIsNotADeclaration checks that a statement starting with mut
// and a name is a declaration unless a method call follows the place.
func TestMutCallIsNotADeclaration(t *testing.T) {
	for _, tt := range []struct {
		body string
		decl bool
	}{
		{"mut x := 1", true},
		{"mut x int64 = 1", true},
		{"mut x int64", true},
		{"mut b.write(s)", false},
		{"mut b.out.write(s)", false},
		{"mut a[f(1)].g(2)", false},
	} {
		t.Run(tt.body, func(t *testing.T) {
			file, diags := parse(t, "package main\n\nfunc main() {\n    "+tt.body+"\n}\n")
			if len(diags) > 0 {
				t.Fatalf("parse reported %+v", diags)
			}
			_, isVar := file.Decls[0].(*ast.Func).Body[0].(*ast.Var)
			if isVar != tt.decl {
				t.Errorf("declaration = %v, want %v", isVar, tt.decl)
			}
		})
	}
}

// TestMutCoversWholeCallInParentheses checks that mut before a method call
// in parentheses covers the whole call, which the type checker reports as
// not a place, rather than its receiver.
func TestMutCoversWholeCallInParentheses(t *testing.T) {
	file, diags := parse(t, "package main\n\nfunc main() {\n    x := mut (b.f())\n}\n")
	if len(diags) > 0 {
		t.Fatalf("parse reported %+v", diags)
	}
	v := file.Decls[0].(*ast.Func).Body[0].(*ast.Var)
	m, ok := v.Value.(*ast.MutArg)
	if !ok {
		t.Fatalf("value is %T, want a mut argument", v.Value)
	}
	if call, ok := m.X.(*ast.Call); !ok || call.Receiver() == nil {
		t.Errorf("mut covers %T, want the method call", m.X)
	}
	if _, isMut := m.X.(*ast.Call).Receiver().(*ast.MutArg); isMut {
		t.Error("the receiver was made mut too")
	}
}

// methodParseErrors are malformed methods and method calls, each with the
// message the parser must give and the source it must cover, "" for a
// point.
var methodParseErrors = []struct{ name, src, msg, at string }{
	{"open parenthesis only", "func (", "expected 'receiver name' found 'EOF'", ""},
	{"receiver name only", "func (b", "expected 'type name' found ';'", ""},
	{"mut only", "func (mut", "expected 'receiver name' found 'EOF'", ""},
	{"receiver unclosed", "func (b Builder", "expected ')' found ';'", ""},
	{"receiver then nothing", "func (b Builder)", "expected 'method name' found ';'", ""},
	{"no method name", "func (b Builder) () {\n}\n", "expected 'method name' found '('", ""},
	{
		"number as method name",
		"func (b Builder) 1() {\n}\n",
		"expected 'method name' found '1'",
		"",
	},
	{
		"keyword as method name",
		"func (b Builder) range() {\n}\n",
		"expected 'method name' found 'range'",
		"",
	},
	{"no receiver", "func () m() {\n}\n", "method has no receiver", "()"},
	{"two receivers", "func (a A, b B) m() {\n}\n", "method has multiple receivers", ","},
	{
		"two receivers without a comma",
		"func (a A b B) m() {\n}\n",
		"method has multiple receivers",
		"b",
	},
	{
		"two receivers, second mut",
		"func (a A mut b B) m() {\n}\n",
		"method has multiple receivers",
		"mut",
	},
	{"variadic receiver", "func (b ...B) m() {\n}\n", "a receiver cannot be variadic", "..."},
	{"receiver without a type", "func (b) m() {\n}\n", "expected 'type name' found ')'", ""},
	{"receiver without a name", "func (B) m() {\n}\n", "expected 'type name' found ')'", ""},
	{"two receiver lists", "func (a A) (b B) m() {\n}\n", "expected 'method name' found '('", ""},
	{"receiver after the name", "func m (b B) () {\n}\n", "expected 'type name' found ')'", ""},
	{
		"method value as a statement",
		"func main() {\n    b.len\n}\n",
		"expected 'statement' found 'b'",
		"",
	},
	{
		"call result indexed as a statement",
		"func main() {\n    b.f()[0]\n}\n",
		"expected 'statement' found 'b'",
		"",
	},
	{"method call assigned", "func main() {\n    b.f() = 1\n}\n", "expected ';' found '='", ""},
	{
		"field of a method call assigned",
		"func main() {\n    b.f().x = 1\n}\n",
		"expected 'statement' found 'b'",
		"",
	},
	{"field of a call written", "func main() {\n    f().x = 1\n}\n", "expected ';' found '.'", ""},
	{"mut function call", "func main() {\n    mut f()\n}\n", "expected 'type or =' found '('", ""},
	{"mut call assigned", "func main() {\n    mut b.f() = 1\n}\n", "expected ';' found '='", ""},
	{"mut call declared", "func main() {\n    mut b.f() := 1\n}\n", "expected ';' found ':='", ""},
	{"unclosed arguments", "func main() {\n    mut b.f(1\n}\n", "expected ',' found ';'", ""},
	{
		"selector without a name after mut",
		"func main() {\n    mut b.\n}\n",
		"expected 'type or =' found '.'",
		"",
	},
}

// TestRejectsMalformedMethods checks each malformed method or method call
// is reported as listed, by the parser, with no tree.
func TestRejectsMalformedMethods(t *testing.T) {
	for _, tt := range methodParseErrors {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n\n" + tt.src
			file, diags := parse(t, src)
			if file != nil || len(diags) != 1 {
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

// TestDeepMethodChains checks long chains of method calls and of fields
// before one parse, in time linear in their length: the lookahead that
// tells a mut call from a declaration scans each chain once. A chain is
// flat, however long; calls nested in one another nest, so they stop at
// the parser's limit, a call's arguments in the thousandth.
func TestDeepMethodChains(t *testing.T) {
	const n = 5000
	calls := "b" + strings.Repeat(".f()", n)
	fields := "mut b" + strings.Repeat(".x", n) + ".f()"
	nested := strings.Repeat("g(", 999) + "b.f()" + strings.Repeat(")", 999)
	for _, body := range []string{"x := " + calls, calls, fields, "x := " + nested} {
		file, diags := parse(t, "package main\n\nfunc main() {\n    "+body+"\n}\n")
		if file == nil || len(diags) > 0 {
			t.Errorf("a chain of %d did not parse: %v", n, diags)
		}
	}
}

// TestManyMethods checks a file declaring thousands of methods parses,
// each one a method of its own.
func TestManyMethods(t *testing.T) {
	var b strings.Builder
	b.WriteString("package main\n\ntype T struct {\n}\n")
	const n = 3000
	for i := range n {
		b.WriteString("func (t T) m")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString(strings.Repeat("y", i/7))
		b.WriteString("() {\n}\n")
	}
	file, diags := parse(t, b.String())
	if len(diags) > 0 {
		t.Fatalf("parse reported %v", diags[0])
	}
	if got := len(file.Decls); got != n+1 {
		t.Errorf("got %d declarations, want %d", got, n+1)
	}
}

// methodSeeds are sources about methods the fuzzer starts from.
var methodSeeds = []string{
	"package main\nfunc (b B) f() {}\nfunc main() { b.f() }\n",
	"package main\nfunc (mut b B) f(x ...int64) error { return mut b.g(x...) }\n",
	"package main\nfunc main() { mut w.out[1].f(mut a, b).g() }\n",
	"package main\nfunc main() { x, err := mut b.f(); if mut b.ok() {} }\n",
	"package main\nfunc (",
	"package main\nfunc (mut b B",
	"package main\nfunc () m() {}\n",
	"package main\nfunc main() { mut mut b.f() }\n",
	"package main\nfunc main() { for mut b.f(); mut b.g(); mut b.h() {} }\n",
}

// FuzzParseMethods parses arbitrary source, seeded with method
// declarations and calls, and checks the parser never panics, gives a
// tree exactly when it reports nothing, and reports each problem at a
// span inside the source.
func FuzzParseMethods(f *testing.F) {
	for _, src := range methodSeeds {
		f.Add(src)
	}
	for _, tt := range methodParseErrors {
		f.Add("package main\n\n" + tt.src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		file := &source.File{ID: "fuzz", Filename: "fuzz.tck", Data: []byte(src)}
		lex := lexer.New(file)
		if diags := lex.Lex(); len(diags) > 0 {
			return
		}
		tree, diags := parser.NewParser(file.ID, lex.Tokens()).Parse()
		if (tree == nil) == (len(diags) == 0) {
			t.Fatalf("tree %v with %d diagnostics", tree != nil, len(diags))
		}
		for _, d := range diags {
			checkSpan(t, src, d)
		}
	})
}

// checkSpan fails the test unless d's span lies inside src, in order.
func checkSpan(t *testing.T, src string, d diag.Diagnostic) {
	t.Helper()
	if d.Span.Start < 0 || d.Span.Start > d.Span.End || d.Span.End > len(src) {
		t.Fatalf("%q at %d:%d is outside the source of %d bytes",
			d.Message, d.Span.Start, d.Span.End, len(src))
	}
}
