package resolver_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// A variable declared in a loop's init is visible in its condition, post
// statement and body, and bound there.
func TestBindsThreeClauseLoopVariable(t *testing.T) {
	src := "package main\n\nfunc main() {\n    for mut i := 0; i < 3; i++ {\n        exit(i)\n    }\n}\n"
	file, diags := resolveTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %q, want no diagnostics", messagesOf(diags))
	}
	loop := file.Decls[0].(*ast.Func).Body[0].(*ast.For)
	i := loop.Init.(*ast.Var)
	if id := loop.Cond.(*ast.Binary).X.(*ast.Ident); id.Def != i {
		t.Errorf("i in the condition bound to %v", id.Def)
	}
	if post := loop.Post.(*ast.IncDec); post.Target.Def != i {
		t.Errorf("i in the post statement bound to %v", post.Target.Def)
	}
	arg := loop.Body[0].(*ast.ExprStmt).X.(*ast.Call).Args[0].(*ast.Ident)
	if arg.Def != i {
		t.Errorf("i in the body bound to %v", arg.Def)
	}
}

// TestThreeClauseLoopScope checks the init variable belongs to the loop: not
// visible after it, reported when unused, and held to the no-shadowing rule
// like any other.
func TestThreeClauseLoopScope(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			"used after the loop",
			"package main\n\nfunc main() {\n    for mut i := 0; i < 3; i++ {\n    }\n    exit(i)\n}\n",
			[]string{"undefined: i"},
		},
		{
			"only updated",
			"package main\n\nfunc main() {\n    for mut i := 0; ; i++ {\n        break\n    }\n}\n",
			[]string{"declared and not used: i"},
		},
		{
			"shadows an outer variable",
			"package main\n\nfunc main() {\n    i := 1\n    for mut i := 0; i < 3; i++ {\n    }\n    exit(i)\n}\n",
			[]string{"i is already declared"},
		},
		{
			"body redeclares it",
			"package main\n\nfunc main() {\n    for mut i := 0; i < 3; i++ {\n        i := 5\n        exit(i)\n    }\n}\n",
			[]string{"i is already declared"},
		},
		{
			"sibling loops reuse the name",
			"package main\n\nfunc main() {\n    for mut i := 0; i < 3; i++ {\n    }\n    for mut i := 0; i < 3; i++ {\n    }\n}\n",
			nil,
		},
		{
			"post cannot see the body",
			"package main\n\nfunc main() {\n    for ;; exit(x) {\n        x := 1\n        exit(x)\n    }\n}\n",
			[]string{"undefined: x"},
		},
		{
			"undefined in init",
			"package main\n\nfunc main() {\n    for missing = 0; ; {\n        break\n    }\n}\n",
			[]string{"undefined: missing"},
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
