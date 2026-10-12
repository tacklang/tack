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

// TestRangeScope checks the range variable belongs to the loop, and the
// count is resolved outside it.
func TestRangeScope(t *testing.T) {
	tests := []struct {
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
			"siblings reuse the name",
			"package main\n\nfunc main() {\n    for i := range 3 {\n        exit(i)\n    }\n    for i := range 2 {\n        exit(i)\n    }\n}\n",
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := resolveTree(t, tt.src)
			if got := messagesOf(diags); strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
