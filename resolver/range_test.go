package resolver_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// The range variable is bound in the body.
func TestBindsRangeVariable(t *testing.T) {
	src := "package main\n\nfunc main() {\n    for i := range 3 {\n        exit(i)\n    }\n}\n"
	file, diags := resolveTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %q, want no diagnostics", messagesOf(diags))
	}
	loop := file.Decls[0].(*ast.Func).Body[0].(*ast.ForRange)
	arg := loop.Body[0].(*ast.ExprStmt).X.(*ast.Call).Args[0].(*ast.Ident)
	if arg.Def != loop.Var {
		t.Errorf("i in the body bound to %v, want the range variable", arg.Def)
	}
}

// rangeScopeCases are range loops whose variables' scope matters, with the
// diagnostics each must produce: the variables belong to the loop, and what
// it ranges over is resolved outside it.
var rangeScopeCases = []struct {
	name string
	src  string
	want []string
}{
	{
		"used after the loop",
		"package main\n\nfunc main() {\n    for i := range 3 {\n        exit(i)\n    }\n    exit(i)\n}\n",
		[]string{"undefined: i"},
	},
	{
		"unused",
		"package main\n\nfunc main() {\n    for i := range 3 {\n    }\n}\n",
		[]string{"declared and not used: i"},
	},
	{
		"shadows an outer variable",
		"package main\n\nfunc main() {\n    i := 1\n    for i := range 3 {\n        exit(i)\n    }\n    exit(i)\n}\n",
		[]string{"i is already declared"},
	},
	{
		"count names the variable",
		"package main\n\nfunc main() {\n    for i := range i {\n        exit(i)\n    }\n}\n",
		[]string{"undefined: i"},
	},
	{
		"count reads an outer variable",
		"package main\n\nfunc main() {\n    n := 3\n    for range n {\n    }\n}\n",
		nil,
	},
	{
		"no variable",
		"package main\n\nfunc main() {\n    for range 3 {\n        x := 1\n    }\n}\n",
		[]string{"declared and not used: x"},
	},
	{
		"element unused",
		"package main\n\nfunc main() {\n    a := [2]int64{1, 2}\n    for i, x := range a {\n        exit(i)\n    }\n}\n",
		[]string{"declared and not used: x"},
	},
	{
		"index unused",
		"package main\n\nfunc main() {\n    a := [2]int64{1, 2}\n    for i, x := range a {\n        exit(x)\n    }\n}\n",
		[]string{"declared and not used: i"},
	},
	{
		"element used after the loop",
		"package main\n\nfunc main() {\n    for _, x := range \"ab\" {\n        exit(x)\n    }\n    exit(x)\n}\n",
		[]string{"undefined: x"},
	},
	{
		"element shadows an outer variable",
		"package main\n\nfunc main() {\n    x := 1\n    for _, x := range \"ab\" {\n        exit(x)\n    }\n    exit(x)\n}\n",
		[]string{"x is already declared"},
	},
	{
		"element shadows a parameter",
		"package main\n\nfunc f(x int64) {\n    for _, x := range \"ab\" {\n        exit(x)\n    }\n}\n\nfunc main() {\n    f(1)\n}\n",
		[]string{"x is already declared"},
	},
	{
		"nested loop reuses a name",
		"package main\n\nfunc main() {\n    for i, x := range \"ab\" {\n        for _, x := range \"cd\" {\n            exit(i + x)\n        }\n    }\n}\n",
		[]string{"x is already declared"},
	},
	{
		"index and element share a name",
		"package main\n\nfunc main() {\n    for x, x := range \"ab\" {\n        exit(x)\n    }\n}\n",
		[]string{"x is already declared"},
	},
	{
		"ranged expression names the element",
		"package main\n\nfunc main() {\n    for _, x := range x {\n        exit(x)\n    }\n}\n",
		[]string{"undefined: x"},
	},
	{
		"ranges over the variable it is named after elsewhere",
		"package main\n\nfunc main() {\n    s := \"ab\"\n    for i, b := range s {\n        exit(i + b)\n    }\n    for _, b := range s {\n        exit(b)\n    }\n}\n",
		nil,
	},
	{
		"blank index",
		"package main\n\nfunc main() {\n    for _, x := range \"ab\" {\n        exit(x)\n    }\n}\n",
		nil,
	},
	{
		"blank element",
		"package main\n\nfunc main() {\n    for i, _ := range \"ab\" {\n        exit(i)\n    }\n}\n",
		nil,
	},
	{
		"siblings reuse the name",
		"package main\n\nfunc main() {\n    for i := range 3 {\n        exit(i)\n    }\n    for i := range 2 {\n        exit(i)\n    }\n}\n",
		nil,
	},
}

// TestRangeScope checks each of rangeScopeCases gives exactly its
// diagnostics.
func TestRangeScope(t *testing.T) {
	for _, tt := range rangeScopeCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := resolveTree(t, tt.src)
			if got := messagesOf(diags); strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// The element variable is bound in the body, and a _ index is not declared,
// so it binds nothing.
func TestBindsRangeElement(t *testing.T) {
	src := "package main\n\nfunc main() {\n    for _, b := range \"ab\" {\n        exit(b)\n    }\n}\n"
	file, diags := resolveTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %q, want no diagnostics", messagesOf(diags))
	}
	loop := file.Decls[0].(*ast.Func).Body[0].(*ast.ForRange)
	arg := loop.Body[0].(*ast.ExprStmt).X.(*ast.Call).Args[0].(*ast.Ident)
	if arg.Def != loop.Elem {
		t.Errorf("b in the body bound to %v, want the element variable", arg.Def)
	}
	if loop.Var.Name != ast.Blank {
		t.Errorf("index is %q, want _", loop.Var.Name)
	}
}
