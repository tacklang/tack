package resolver_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// Names in a loop's condition and body are bound, and a read in either
// counts as a use.
func TestBindsNamesInLoops(t *testing.T) {
	src := "package main\n\nfunc main() {\n    mut i = 0\n    limit := 3\n    for i < limit {\n        i = i + 1\n    }\n}\n"
	file, diags := resolveTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %q, want no diagnostics", messagesOf(diags))
	}

	body := file.Decls[0].(*ast.Func).Body
	loop := body[2].(*ast.For)
	if id := loop.Cond.(*ast.Binary).Y.(*ast.Ident); id.Def != body[1].(*ast.Var) {
		t.Errorf("limit in the condition bound to %v, want its declaration", id.Def)
	}
	if assign := loop.Body[0].(*ast.Assign); assign.Target.(*ast.Ident).Def != body[0].(*ast.Var) {
		t.Errorf("i in the body bound to %v, want its declaration", assign.Target.(*ast.Ident).Def)
	}
}

// TestLoopBodyIsABlock checks a loop body is a scope of its own, with the
// same rules as any other block.
func TestLoopBodyIsABlock(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			"declared in the body, used after",
			"package main\n\nfunc main() {\n    for {\n        x := 1\n        exit(x)\n    }\n    exit(x)\n}\n",
			[]string{"undefined: x"},
		},
		{
			"unused in the body",
			"package main\n\nfunc main() {\n    for {\n        x := 1\n        break\n    }\n}\n",
			[]string{"declared and not used: x"},
		},
		{
			"shadows an outer variable",
			"package main\n\nfunc main() {\n    x := 1\n    for {\n        x := 2\n        exit(x)\n    }\n    exit(x)\n}\n",
			[]string{"x is already declared"},
		},
		{
			"undefined in the condition",
			"package main\n\nfunc main() {\n    for missing {\n    }\n}\n",
			[]string{"undefined: missing"},
		},
		{
			"same name in two sibling loops",
			"package main\n\nfunc main() {\n    for {\n        x := 1\n        exit(x)\n    }\n    for {\n        x := 2\n        exit(x)\n    }\n}\n",
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
