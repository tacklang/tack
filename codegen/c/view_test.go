package c_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen/c"
)

// TestViewsExampleIsSanitizerClean builds examples/views with clang's
// warnings as errors and the address and undefined behavior sanitizers
// on, so a view that read past its array, a copy that went wrong or a
// pointer to a temporary that no longer exists fails the run, and checks
// every check it prints passed.
func TestViewsExampleIsSanitizerClean(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "examples", "views", "main.tck"))
	if err != nil {
		t.Fatal(err)
	}
	bin := buildWith(
		t,
		string(src),
		"-O1",
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
	)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("views failed: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "FAIL") || strings.Count(string(out), " ok\n") != 25 {
		t.Errorf("views printed\n%s", out)
	}
}

// viewHeaderTests are programs with extern funcs taking views, and a line
// tack.h must hold for each: the view type's name, from its element's
// tack.h name, and its definition, a pointer to const elements and a
// length.
var viewHeaderTests = []struct{ name, src, want string }{
	{
		"basic element",
		"extern func f(v []int64)\n",
		"typedef struct {\n    const int64_t *ptr;\n    int64_t len;\n} tack_view_int64;\n",
	},
	{
		"target-width element",
		"extern func f(v []unsafe_size_t)\n",
		"} tack_view_unsafe_size_t;\n",
	},
	{
		"string element",
		"extern func f(v []string)\n",
		"    const tack_string *ptr;\n    int64_t len;\n} tack_view_string;\n",
	},
	{
		"struct element",
		"type P struct {\n    x int64\n}\n\nextern func f(v []P)\n",
		"    const P *ptr;\n    int64_t len;\n} tack_view_P;\n",
	},
	{
		"array element",
		"extern func f(v [][2]bool)\n",
		"    const tack_array_2_bool *ptr;\n    int64_t len;\n} tack_view_tack_array_2_bool;\n",
	},
	{
		"prototype",
		"extern func f(v []uint8, n int64) int64\n",
		"int64_t f(tack_view_uint8 /* v */, int64_t /* n */); // extern func f(v []uint8, n int64) int64\n",
	},
}

// TestHeaderDefinesViews checks tack.h defines each view type an extern
// signature uses, named after its element, and declares the prototype
// with it.
func TestHeaderDefinesViews(t *testing.T) {
	for _, tt := range viewHeaderTests {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n\n" + tt.src + "\nfunc main() {\n}\n"
			got := string(c.New().Header(ast.SingleFile(compile(t, src))))
			if !strings.Contains(got, tt.want) {
				t.Errorf("tack.h is missing %q:\n%s", tt.want, got)
			}
		})
	}
}

// TestHeaderViewNameClash checks a struct whose tack.h name is a view
// type's is reported, as any two things tack.h would give one name are.
func TestHeaderViewNameClash(t *testing.T) {
	src := "package main\n\ntype tack_view_int64 struct {\n    x int64\n}\n\n" +
		"extern func f(v []int64, s tack_view_int64)\n\nfunc main() {\n}\n"
	_, diags := c.New().Generate(ast.SingleFile(compile(t, src)))
	if len(diags) != 1 ||
		!strings.Contains(diags[0].Message, "both named tack_view_int64 in tack.h") {
		t.Errorf("diagnostics = %+v, want one clash on tack_view_int64", diags)
	}
}
