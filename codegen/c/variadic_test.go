package c_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen/c"
)

// TestVariadicsExampleIsSanitizerClean builds examples/variadics with
// clang's warnings as errors and the address and undefined behavior
// sanitizers on, so an array of values that did not live through the
// call, a view of none that was read or moved, or a copy that went wrong
// fails the run, and checks every check it prints passed.
func TestVariadicsExampleIsSanitizerClean(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "examples", "variadics", "main.tck"))
	if err != nil {
		t.Fatal(err)
	}
	atO0Too(t, []string{"-O1", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"}, func(t *testing.T, flags []string) {
		bin := buildWith(
			t,
			string(src),
			flags...,
		)
		out, err := exec.Command(bin).Output()
		if err != nil {
			t.Fatalf("variadics failed: %v\n%s", err, out)
		}
		if strings.Contains(string(out), "FAIL") || strings.Count(string(out), " ok\n") != 35 {
			t.Errorf("variadics printed\n%s", out)
		}
	})
}

// variadicTextCases are programs and the line of main's body the C
// generated for each must hold: values packed into a compound literal
// array of the element type and viewed, none as a view with a null
// pointer and no length, a spread passed as any view argument is, values
// held in temporaries, in order, when a call among them makes the order
// show, and a value read alongside a mut argument of the same variable
// copied into the array.
var variadicTextCases = []struct{ name, src, want string }{
	{
		"values",
		"func f(s string, n ...int64) {\n}\n\nfunc main() {\n    var b int8 = 1\n    f(\"\", b, 2, 3)\n}\n",
		"(tack_view_N){(const int64_t[3]){v_b, 2, 3}, 3});",
	},
	{
		"no values",
		"func f(n ...int64) {\n}\n\nfunc main() {\n    f()\n}\n",
		"f((tack_view_N){0, 0});",
	},
	{
		"strings",
		"func f(n ...string) {\n}\n\nfunc main() {\n    s := \"a\"\n    f(s, s)\n}\n",
		"(tack_view_N){(const tack_string[2]){v_s, v_s}, 2});",
	},
	{
		"spread",
		"func f(n ...int64) {\n}\n\nfunc main() {\n    a := [2]int64{1, 2}\n    f(a...)\n}\n",
		"f((tack_view_N){v_a.e, 2});",
	},
	{
		"order",
		"func g() int64 {\n    return 1\n}\n\nfunc f(a int64, n ...int64) {\n}\n\nfunc main() {\n    f(g(), 2, g())\n}\n",
		"(tack_o0 = tack_4_main1_g(), tack_o1 = tack_4_main1_g(), tack_4_main1_f(tack_o0, " +
			"(tack_view_N){(const int64_t[2]){2, tack_o1}, 2}));",
	},
	{
		"alongside mut",
		"func f(mut a int64, n ...int64) {\n}\n\nfunc main() {\n    mut x int64 = 1\n    f(mut x, x)\n}\n",
		"f(&v_x, (tack_view_N){(const int64_t[1]){v_x}, 1});",
	},
}

// TestVariadicCallText checks the C each of variadicTextCases generates,
// with every view type's ID, which depends on the types the program met
// before it, written N.
func TestVariadicCallText(t *testing.T) {
	ids := regexp.MustCompile(`tack_view_[0-9]+`)
	for _, tt := range variadicTextCases {
		t.Run(tt.name, func(t *testing.T) {
			got := ids.ReplaceAllString(generate(t, "package main\n\n"+tt.src), "tack_view_N")
			if !strings.Contains(got, tt.want) {
				t.Errorf("generated C is missing %q:\n%s", tt.want, got)
			}
		})
	}
}

// variadicHeaderTests are programs with extern funcs taking variadic
// parameters, and a line tack.h must hold for each: the view type a
// variadic is in C, and the prototype taking it, with the Tack
// declaration beside it written with its ....
var variadicHeaderTests = []struct{ name, src, want string }{
	{
		"view type",
		"extern func f(n ...int64)\n",
		"typedef struct {\n    const int64_t *ptr;\n    int64_t len;\n} tack_view_int64;\n",
	},
	{
		"prototype",
		"extern func f(s string, n ...int64) int64\n",
		"int64_t f(tack_string /* s */, tack_view_int64 /* n */); " +
			"// extern func f(s string, n ...int64) int64\n",
	},
	{
		"array element",
		"extern(\"g\") func f(rows ...[2]bool)\n",
		"void g(tack_view_tack_array_2_bool /* rows */); // extern func f(rows ...[2]bool)\n",
	},
}

// TestHeaderDeclaresVariadics checks tack.h declares each extern func
// taking a variadic parameter as taking the view of its element type.
func TestHeaderDeclaresVariadics(t *testing.T) {
	for _, tt := range variadicHeaderTests {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n\n" + tt.src + "\nfunc main() {\n}\n"
			got := string(c.New().Header(ast.SingleFile(compile(t, src))))
			if !strings.Contains(got, tt.want) {
				t.Errorf("tack.h is missing %q:\n%s", tt.want, got)
			}
		})
	}
}
