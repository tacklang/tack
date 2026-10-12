package resolver_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// pair declares a function with two results, for the tests below to call.
const pair = "func pair(n int64) (int64, int64) {\n    return n, n\n}\n\n"

// TestResolvesMultiVar checks each name a MultiVar declares is bound by
// later uses, and that the call is resolved before any of them exists.
func TestResolvesMultiVar(t *testing.T) {
	file, diags := resolveTree(t, "package main\n\n"+pair+
		"func main() {\n    a, mut b := pair(1)\n    b = a\n    exit(a + b)\n}\n")
	if len(diags) > 0 {
		t.Fatalf("resolve reported %v", messagesOf(diags))
	}

	body := file.Decls[1].(*ast.Func).Body
	m := body[0].(*ast.MultiVar)
	if m.Value.(*ast.Call).Fn.(*ast.Ident).Def != file.Decls[0] {
		t.Error("the call is not bound to pair")
	}
	assign := body[1].(*ast.Assign)
	if assign.Target.(*ast.Ident).Def != m.Vars[1] {
		t.Error("b = a does not write the b the MultiVar declared")
	}
	if assign.Value.(*ast.Ident).Def != m.Vars[0] {
		t.Error("b = a does not read the a the MultiVar declared")
	}
}

// badMultiVarNameCases are MultiVars that break a scoping rule, with every
// message each must produce.
var badMultiVarNameCases = []struct {
	name string
	src  string
	want []string
}{
	{
		"same name twice",
		pair + "func main() {\n    a, a := pair(1)\n    exit(a)\n}\n",
		[]string{"a is already declared"},
	},
	{
		"name used in its own call",
		pair + "func main() {\n    a, b := pair(a)\n    exit(a + b)\n}\n",
		[]string{"undefined: a"},
	},
	{
		"one name never read",
		pair + "func main() {\n    a, b := pair(1)\n    exit(a)\n}\n",
		[]string{"declared and not used: b"},
	},
	{
		"neither name read",
		pair + "func main() {\n    a, b := pair(1)\n}\n",
		[]string{"declared and not used: a", "declared and not used: b"},
	},
	{
		"assigned but never read",
		pair + "func main() {\n    a, mut b := pair(1)\n    b = 2\n    exit(a)\n}\n",
		[]string{"declared and not used: b"},
	},
	{
		"reuses a parameter",
		pair + "func f(a int64) {\n    a, b := pair(1)\n    exit(a + b)\n}\n\nfunc main() {\n}\n",
		[]string{"a is already declared"},
	},
	{
		"reuses a global",
		"var b = 1\n\n" + pair + "func main() {\n    a, b := pair(1)\n    exit(a + b)\n}\n",
		[]string{"b is already declared"},
	},
	{
		"reuses an earlier local",
		pair + "func main() {\n    a := 1\n    a, b := pair(1)\n    exit(a + b)\n}\n",
		[]string{"a is already declared"},
	},
	{
		"out of scope after its block",
		pair + "func main() {\n    if true {\n        a, b := pair(1)\n        exit(a + b)\n    }\n    exit(a)\n}\n",
		[]string{"undefined: a"},
	},
	{
		"undefined callee",
		"func main() {\n    a, b := nope(1)\n    exit(a + b)\n}\n",
		[]string{"undefined: nope"},
	},
}

// TestRejectsBadMultiVarNames checks the scoping rules hold for each name of
// a MultiVar as they do for a single declaration.
func TestRejectsBadMultiVarNames(t *testing.T) {
	for _, tt := range badMultiVarNameCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := resolveTree(t, "package main\n\n"+tt.src)
			got := messagesOf(diags)
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// A loop's init MultiVar declares its names in the loop's own scope.
func TestMultiVarInLoopInitIsScopedToTheLoop(t *testing.T) {
	_, diags := resolveTree(t, "package main\n\n"+pair+
		"func main() {\n    for i, mut n := pair(3); n > i; n-- {\n    }\n    exit(n)\n}\n")
	if got := messagesOf(diags); strings.Join(got, "\n") != "undefined: n" {
		t.Errorf("got %q, want only %q", got, "undefined: n")
	}
}

// main may declare no results, whether one or several.
func TestMainWithSeveralResultsIsRejected(t *testing.T) {
	src := "package main\n\nfunc main() (int64, string) {\n    return 1, \"x\"\n}\n"
	_, diags := resolveTree(t, src)
	if len(diags) != 1 || diags[0].Message != "main function must not declare a result" {
		t.Fatalf("got %q", messagesOf(diags))
	}
	if got := src[diags[0].Span.Start:diags[0].Span.End]; got != "int64, string" {
		t.Errorf("diagnostic covers %q, want %q", got, "int64, string")
	}
}

// A MultiAssign binds each target to the variable it names, and writing to
// a variable that way does not count as reading it.
func TestResolvesMultiAssign(t *testing.T) {
	file, diags := resolveTree(t, "package main\n\n"+pair+
		"func main() {\n    mut a, mut b := pair(1)\n    a, b = pair(2)\n    exit(a)\n}\n")
	if got := messagesOf(diags); strings.Join(got, "\n") != "declared and not used: b" {
		t.Fatalf("got %q, want only b unused", got)
	}

	body := file.Decls[1].(*ast.Func).Body
	decl, assign := body[0].(*ast.MultiVar), body[1].(*ast.MultiAssign)
	for i, target := range assign.Targets {
		if target.(*ast.Ident).Def != decl.Vars[i] {
			t.Errorf("target %s is not bound to its declaration", target.(*ast.Ident).Name)
		}
	}

	_, diags = resolveTree(t, "package main\n\n"+pair+"func main() {\n    a, nope = pair(2)\n}\n")
	if got := messagesOf(diags); strings.Join(got, "\n") != "undefined: a\nundefined: nope" {
		t.Errorf("got %q", got)
	}
}
