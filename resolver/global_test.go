package resolver_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestAcceptsValidGlobalUse lists programs whose names must all resolve.
func TestAcceptsValidGlobalUse(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			"read in main",
			"package main\n\nvar limit = 3\n\nfunc main() {\n    exit(limit)\n}\n",
		},
		{
			"declared after the function that reads it",
			"package main\n\nfunc main() {\n    exit(limit)\n}\n\nvar limit = 3\n",
		},
		{
			"read in another function",
			"package main\n\nvar limit = 3\n\nfunc f() int64 {\n    return limit\n}\n\nfunc main() {\n    exit(f())\n}\n",
		},
		{
			"assigned in one function and read in another",
			"package main\n\nmut count int64\n\nfunc bump() {\n    count = count + 1\n}\n\nfunc main() {\n    bump()\n}\n",
		},
		{
			"two globals",
			"package main\n\nvar a = 1\n\nvar b = 2\n\nfunc main() {\n    exit(a + b)\n}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, diags := resolveTree(t, tt.src); len(diags) > 0 {
				t.Errorf("got %q, want no diagnostics", messagesOf(diags))
			}
		})
	}
}

// invalidGlobalUseCases are programs that misuse a package-level variable,
// with the diagnostics each must produce.
var invalidGlobalUseCases = []struct {
	name string
	src  string
	want []string
}{
	{
		"declared twice",
		"package main\n\nvar a = 1\n\nvar a = 2\n\nfunc main() {\n    exit(a)\n}\n",
		[]string{"a is already declared"},
	},
	{
		"named like a function",
		"package main\n\nfunc f() {\n}\n\nvar f = 1\n\nfunc main() {\n    f()\n}\n",
		[]string{"f is already declared"},
	},
	// The global is the one rejected, so it never comes into scope, but a
	// read of it follows from that and is not reported again.
	{
		"a function named like it",
		"package main\n\nvar f = 1\n\nfunc f() {\n}\n\nfunc main() {\n    exit(f)\n}\n",
		[]string{"f is already declared"},
	},
	{
		"named like a builtin",
		"package main\n\nvar print = 1\n\nfunc main() {\n    exit(print)\n}\n",
		[]string{"print is a builtin and cannot be redeclared"},
	},
	{
		"named main",
		"package main\n\nvar main = 1\n\nfunc main() {\n}\n",
		[]string{"main is already declared"},
	},
	{
		"a local reuses its name",
		"package main\n\nvar n = 1\n\nfunc main() {\n    n := 2\n    exit(n)\n}\n",
		[]string{"n is already declared"},
	},
	{
		"a parameter reuses its name",
		"package main\n\nvar n = 1\n\nfunc f(n int64) {\n    exit(n)\n}\n\nfunc main() {\n    f(1)\n}\n",
		[]string{"n is already declared"},
	},
	{
		"never read",
		"package main\n\nvar n = 1\n\nfunc main() {\n}\n",
		[]string{"declared and not used: n"},
	},
	{
		"only assigned",
		"package main\n\nmut n int64\n\nfunc main() {\n    n = 2\n}\n",
		[]string{"declared and not used: n"},
	},
}

func TestRejectsInvalidGlobalUse(t *testing.T) {
	for _, tt := range invalidGlobalUseCases {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := resolveTree(t, tt.src)
			got := messagesOf(diags)
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// Reads and assignments in any function bind to the package-level variable.
func TestBindsGlobalReferences(t *testing.T) {
	src := "package main\n\nmut count int64\n\nfunc main() {\n    count = count + 1\n}\n"
	file, diags := resolveTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %q", messagesOf(diags))
	}

	global := file.Decls[0].(*ast.Var)
	assign := file.Decls[1].(*ast.Func).Body[0].(*ast.Assign)
	if assign.Target.(*ast.Ident).Def != global {
		t.Errorf("target bound to %v, want the global", assign.Target.(*ast.Ident).Def)
	}
	if read := assign.Value.(*ast.Binary).X.(*ast.Ident); read.Def != global {
		t.Errorf("read bound to %v, want the global", read.Def)
	}
}

// A name in a global's initializer is bound like any other, so the type
// checker can say why it is not allowed there rather than that it is
// undefined.
func TestBindsNamesInGlobalInitializers(t *testing.T) {
	src := "package main\n\nvar a = 1\n\nvar b = a + 1\n\nfunc main() {\n    exit(b)\n}\n"
	file, diags := resolveTree(t, src)
	if len(diags) > 0 {
		t.Fatalf("got %q", messagesOf(diags))
	}

	ref := file.Decls[1].(*ast.Var).Value.(*ast.Binary).X.(*ast.Ident)
	if ref.Def != file.Decls[0] {
		t.Errorf("a in b's initializer bound to %v, want the global a", ref.Def)
	}
}
