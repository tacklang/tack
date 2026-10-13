package resolver_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// deferScopeCases are defers whose names' scopes matter, with the
// diagnostics each must produce: what a defer defers is resolved where the
// defer is written, as a block of its own.
var deferScopeCases = []struct {
	name string
	src  string
	want []string
}{
	{"reads a variable declared before", inMainOf("x := 1", "defer exit(x)"), nil},
	{
		"reads a variable declared after",
		inMainOf("defer exit(x)", "x := 1", "exit(x)"),
		[]string{"undefined: x"},
	},
	{
		"assigns a variable declared after",
		inMainOf("defer x = 2", "mut x := 1", "exit(x)"),
		[]string{"undefined: x"},
	},
	{"a read only in a defer counts", inMainOf("x := 1", "defer exit(x)"), nil},
	{
		"a block's own variable is not seen after it",
		inMainOf("defer {", "    x := 1", "    exit(x)", "}", "exit(x)"),
		[]string{"undefined: x"},
	},
	{
		"a block's variable may reuse a later name",
		inMainOf("defer {", "    x := 1", "    exit(x)", "}", "x := 2", "exit(x)"),
		nil,
	},
	{
		"a block's variable may not reuse an earlier name",
		inMainOf("x := 1", "defer {", "    x := 2", "    exit(x)", "}", "exit(x)"),
		[]string{"x is already declared"},
	},
	{
		"unused in a block",
		inMainOf("defer {", "    x := 1", "}"),
		[]string{"declared and not used: x"},
	},
	{
		"two blocks declare the same name",
		inMainOf(
			"defer {",
			"    x := 1",
			"    exit(x)",
			"}",
			"defer {",
			"    x := 2",
			"    exit(x)",
			"}",
		),
		nil,
	},
	{"undefined callee", inMainOf("defer g()"), []string{"undefined: g"}},
	{"assigns _", inMainOf("defer _ = 1"), []string{"cannot assign to _"}},
	{
		"nested defer sees the outer block's names",
		inMainOf("defer {", "    x := 1", "    defer exit(x)", "}"),
		nil,
	},
}

// TestDeferScope checks a defer's statement is resolved where the defer is,
// in a block of its own.
func TestDeferScope(t *testing.T) {
	for _, tt := range deferScopeCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := resolveTree(t, tt.src)
			got := messagesOf(diags)
			if len(got) != len(tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			for i := range got {
				if !strings.Contains(got[i], tt.want[i]) {
					t.Errorf("got %q, want %q", got, tt.want)
				}
			}
		})
	}
}

// A deferred call's argument binds to the variable it names.
func TestBindsDeferredNames(t *testing.T) {
	file, diags := resolveTree(t, inMainOf("mut n := 1", "defer n++", "defer exit(n)"))
	if len(diags) > 0 {
		t.Fatalf("got %q, want no diagnostics", messagesOf(diags))
	}
	body := file.Decls[0].(*ast.Func).Body
	n := body[0].(*ast.Var)
	inc := body[1].(*ast.Defer).Body[0].(*ast.IncDec)
	if inc.Target.(*ast.Ident).Def != n {
		t.Errorf("n++ bound to %v, want n", inc.Target.(*ast.Ident).Def)
	}
	call := body[2].(*ast.Defer).Body[0].(*ast.ExprStmt).X.(*ast.Call)
	if call.Args[0].(*ast.Ident).Def != n {
		t.Errorf("exit(n) bound to %v, want n", call.Args[0].(*ast.Ident).Def)
	}
}
