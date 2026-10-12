package c_test

import (
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/types"
)

// TestEmitsGlobalsAsStaticVariables pins the C declaration for each kind of
// package-level variable: the computed value, never the source expression,
// and const unless the variable is mutable.
func TestEmitsGlobalsAsStaticVariables(t *testing.T) {
	tests := []struct {
		decl string
		read string
		want string
	}{
		{"var x = 40 + 2", "exit(x)", "static const int64_t tack_main_x = 42;"},
		{"mut x int64", "exit(x)", "static int64_t tack_main_x = 0;"},
		{
			"var x = 0 - 9223372036854775807 - 1", "exit(x)",
			"static const int64_t tack_main_x = (-9223372036854775807 - 1);",
		},
		{"var x = 1 < 2", "if x {\n    }", "static const bool tack_main_x = true;"},
		{"mut x bool", "if x {\n    }", "static bool tack_main_x = false;"},
		{"var x = \"hi\"", "print(x)", "static const tack_string tack_main_x = {str0, 2};"},
		{"mut x string", "print(x)", "static tack_string tack_main_x = {str0, 0};"},
	}

	for _, tt := range tests {
		t.Run(tt.decl, func(t *testing.T) {
			got := generate(t, "package main\n\n"+tt.decl+"\n\nfunc main() {\n    "+tt.read+"\n}\n")
			if !strings.Contains(got, tt.want) {
				t.Errorf("generated C is missing %q:\n%s", tt.want, got)
			}
		})
	}
}

// Globals are declared before any function, so a function can use one
// declared after it in the Tack source.
func TestGlobalsAreDeclaredBeforeFunctions(t *testing.T) {
	got := generate(t, "package main\n\nfunc main() {\n    exit(x)\n}\n\nvar x = 1\n")

	global := strings.Index(got, "tack_main_x = 1;")
	fn := strings.Index(got, "static void tack_main_main(void) {")
	if global < 0 || fn < 0 || global > fn {
		t.Errorf("global not declared before the functions:\n%s", got)
	}
}

// globalRuntimeCases are programs and what each must print and exit with.
var globalRuntimeCases = []struct {
	name   string
	src    string
	stdout string
	status int
}{
	{
		"read",
		"package main\n\nvar limit = 6 * 7\n\nfunc main() {\n    exit(limit)\n}\n", "", 42,
	},
	{
		"declared after its reader",
		"package main\n\nfunc main() {\n    exit(limit)\n}\n\nvar limit = 9\n", "", 9,
	},
	{
		"updated across calls",
		"package main\n\nmut count int64\n\nfunc bump() {\n    count = count + 1\n}\n\n" +
			"func main() {\n    bump()\n    bump()\n    bump()\n    exit(count)\n}\n", "", 3,
	},
	{
		"string",
		"package main\n\nvar name = \"tack\"\n\nfunc main() {\n    print(name)\n}\n", "tack", 0,
	},
	{
		"zero string reassigned",
		"package main\n\nmut msg string\n\nfunc main() {\n    print(msg)\n    msg = \"set\"\n    print(msg)\n}\n",
		"set",
		0,
	},
	{
		"bool",
		"package main\n\nvar on = 2 > 1\n\nfunc main() {\n    if on {\n        exit(5)\n    }\n}\n",
		"",
		5,
	},
	{
		"smallest int64",
		"package main\n\nvar min = 0 - 9223372036854775807 - 1\n\n" +
			"func main() {\n    if min == 0 - 9223372036854775807 - 1 {\n        exit(7)\n    }\n}\n", "", 7,
	},
	{
		"names the C side uses",
		"package main\n\nvar str0 = 1\n\nvar int = 2\n\nvar tack_print = 3\n\n" +
			"func main() {\n    print(\"x\")\n    exit(str0 + int + tack_print)\n}\n", "x", 6,
	},
	{
		"global and local together",
		"package main\n\nmut total int64\n\nfunc add(n int64) {\n    total = total + n\n}\n\n" +
			"func main() {\n    step := 5\n    add(step)\n    add(step * 2)\n    exit(total)\n}\n", "", 15,
	},
}

func TestGlobalsAtRuntime(t *testing.T) {
	for _, tt := range globalRuntimeCases {
		t.Run(tt.name, func(t *testing.T) {
			stdout, status := run(t, tt.src)
			if stdout != tt.stdout || status != tt.status {
				t.Errorf(
					"got stdout %q, status %d; want %q, %d",
					stdout,
					status,
					tt.stdout,
					tt.status,
				)
			}
		})
	}
}

// An assignment the type checker would have rejected, handed to the backend
// anyway, is reported rather than crashing it.
func TestAssignmentToANonVariableIsReported(t *testing.T) {
	param := &ast.Param{Name: "n", Type: types.Int64}
	file := &ast.File{
		Package: "main",
		Decls: []ast.Decl{
			&ast.Func{Name: "main", Params: []*ast.Param{param}, Body: []ast.Stmt{
				&ast.Assign{
					Target: &ast.Ident{Name: "n", Def: param},
					Value:  &ast.IntLit{Value: constant.FromInt64(1), Type: types.Int64},
				},
			}},
		},
	}

	out, diags := c.New().Generate(file)
	if len(diags) == 0 {
		t.Fatalf("generate accepted an assignment to a parameter and emitted:\n%s", out)
	}
}
