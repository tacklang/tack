package c_test

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen/c"
)

// anyProgram declares an extern func with an any parameter and one with
// ...any, around the lines of main's body.
func anyProgram(lines ...string) string {
	return "package main\n\nextern func one(a any)\n\nextern func many(xs ...any)\n\n" +
		"extern func both(mut x int64, a any)\n\n" +
		"mut g int64\n\nvar k = \"k\"\n\n" +
		"func get() int64 {\n    g +%= 1\n    return g\n}\n\n" +
		"func on(a any, xs ...any) {\n    one(a)\n    many(xs...)\n    many(a, a)\n}\n\n" +
		"func useK() {\n    one(k)\n}\n\n" +
		"func main() {\n    " + strings.Join(lines, "\n    ") + "\n}\n"
}

// anyCTests are calls passing values as any, and the C each must contain.
var anyCTests = []struct{ name, src, want string }{
	{
		"immutable local, by address",
		anyProgram("n := get()", "one(n)"),
		"tack_4_main3_one((tack_any){TACK_TYPE_INT64, &v_n});",
	},
	{
		"mutable local never passed as mut, by address",
		anyProgram("mut n int64 = 1", "n++", "one(n)"),
		"tack_4_main3_one((tack_any){TACK_TYPE_INT64, &v_n});",
	},
	{
		"immutable global, by address",
		anyProgram("useK()", "on(1)"),
		"tack_4_main3_one((tack_any){TACK_TYPE_STRING, &tack_4_main1_k});",
	},
	{
		"constant, copied",
		anyProgram("one(5)"),
		"tack_4_main3_one((tack_a0 = 5, (tack_any){TACK_TYPE_INT64, &tack_a0}));",
	},
	{
		"character, a uint8",
		anyProgram("one('a')"),
		"(tack_a0 = 97, (tack_any){TACK_TYPE_UINT8, &tack_a0})",
	},
	{
		"mutable global, copied",
		anyProgram("one(g)"),
		"tack_4_main3_one((tack_a0 = tack_4_main1_g, (tack_any){TACK_TYPE_INT64, &tack_a0}));",
	},
	{
		"local passed as mut in the call, copied",
		anyProgram("mut x int64 = 1", "both(mut x, x)"),
		"tack_4_main4_both(&v_x, (tack_a0 = v_x, (tack_any){TACK_TYPE_INT64, &tack_a0}));",
	},
	{
		"call result, copied",
		anyProgram("one(get())"),
		"(tack_a0 = tack_4_main3_get(), (tack_any){TACK_TYPE_INT64, &tack_a0})",
	},
	{
		"held for the order of evaluation, by the holding temporary's address",
		anyProgram("many(g, get(), g)"),
		"(tack_o0 = tack_4_main1_g, tack_o1 = tack_4_main3_get(), tack_o2 = tack_4_main1_g, " +
			"tack_4_main4_many((tack_view_20){(const tack_any[3]){(tack_any){TACK_TYPE_INT64, &tack_o0}, " +
			"(tack_any){TACK_TYPE_INT64, &tack_o1}, (tack_any){TACK_TYPE_INT64, &tack_o2}}, 3}));",
	},
	{
		"no values",
		anyProgram("many()"),
		"tack_4_main4_many((tack_view_20){0, 0});",
	},
	{
		"an any passed on as itself",
		anyProgram("on(1)"),
		"    tack_4_main3_one(p_a);\n    tack_4_main4_many(p_xs);\n" +
			"    tack_4_main4_many((tack_view_20){(const tack_any[2]){p_a, p_a}, 2});\n",
	},
	{
		"view of anys",
		anyProgram("many()"),
		"typedef struct {\n    const tack_any *ptr;\n    int64_t len;\n} tack_view_20;\n",
	},
	{
		"any parameter",
		anyProgram("many()"),
		"static void tack_4_main2_on(tack_any p_a, tack_view_20 p_xs);\n",
	},
}

// TestGeneratesAnyArguments checks the C each of anyCTests generates.
func TestGeneratesAnyArguments(t *testing.T) {
	for _, tt := range anyCTests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generate(t, tt.src); !strings.Contains(got, tt.want) {
				t.Errorf("want\n%s\nin\n%s", tt.want, got)
			}
		})
	}
}

// viewAnyProgram declares one, an extern func taking an any, setFirst,
// taking a mut array and an any, show, passing its view parameter on as
// any, and decls, around the lines of main's body.
func viewAnyProgram(decls string, lines ...string) string {
	return "package main\n\nextern func one(a any)\n\n" +
		"extern(\"set_first\") func setFirst(mut a [2]uint8, v any)\n\n" +
		"func show(v []uint8) {\n    one(v)\n}\n\n" + decls +
		"func main() {\n    " + strings.Join(lines, "\n    ") + "\n}\n"
}

// digits matches each run of digits in C, which normalized replaces.
var digits = regexp.MustCompile(`[0-9]+`)

// normalized is c with each run of digits replaced by N, so a check of
// its shape does not depend on the type IDs and temporaries it numbers.
func normalized(c string) string {
	return digits.ReplaceAllString(c, "N")
}

// viewAnyCTests are calls passing views as any, and the C each must
// contain, compared with its digits normalized.
var viewAnyCTests = []struct{ name, src, want string }{
	{
		"view parameter, by address",
		viewAnyProgram("", "a := [2]uint8{1, 2}", "show(a)"),
		"    tack_4_main3_one((tack_any){TACK_TYPE_VIEW_UINT8, &p_v});\n",
	},
	{
		"slice of an array, held in a temporary",
		viewAnyProgram("", "a := [2]uint8{1, 2}", "one(a[:])"),
		"(tack_a0 = tack_slice_from_22((tack_view_22){v_a.e, 2}, 0), (tack_any){TACK_TYPE_VIEW_UINT8, &tack_a0})",
	},
	{
		"slice of an array passed as mut in the call, of a copy",
		viewAnyProgram("", "mut a := [2]uint8{1, 2}", "setFirst(mut a, a[1:])"),
		"tack_4_main8_setFirst(&v_a, (tack_a1 = tack_slice_from_22((tack_c0 = v_a, (tack_view_22){tack_c0.e, 2}), " +
			"(int64_t)1), (tack_any){TACK_TYPE_VIEW_UINT8, &tack_a1}));",
	},
	{
		"slice of a mutable global array, of a copy",
		viewAnyProgram("mut grid [2]uint8 = [2]uint8{1, 2}\n\n", "one(grid[:])"),
		"(tack_a1 = tack_slice_from_22((tack_c0 = tack_4_main4_grid, (tack_view_22){tack_c0.e, 2}), 0), " +
			"(tack_any){TACK_TYPE_VIEW_UINT8, &tack_a1})",
	},
	{
		"slice of a list, its buffer",
		viewAnyProgram("", "l := make(list[int64], c_allocator)", "one(l[:])"),
		"(tack_a0 = tack_slice_from_23((tack_view_23){v_l.ptr, v_l.len}, 0), (tack_any){TACK_TYPE_VIEW_INT64, &tack_a0})",
	},
}

// TestGeneratesViewsAsAny checks the C each of viewAnyCTests generates.
func TestGeneratesViewsAsAny(t *testing.T) {
	for _, tt := range viewAnyCTests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generate(t, tt.src); !strings.Contains(normalized(got), normalized(tt.want)) {
				t.Errorf("want\n%s\nin\n%s", tt.want, got)
			}
		})
	}
}

// TestAnyArgumentsRunStrictly builds a program passing values as any
// through Tack functions to C under the sanitizers, and checks C reads
// each as it was at the call, and the type id it was passed with.
func TestAnyArgumentsRunStrictly(t *testing.T) {
	src := anyProgram(
		"mut x int64 = 1",
		"both(mut x, x)",
		"one(g)",
		"on(get(), x, \"s\", g)",
		"useK()",
		"if x != 7 {",
		"    exit(1)",
		"}",
	)
	csrc := "#include \"tack.h\"\n#include <stdlib.h>\n" +
		"static int64_t seen[9];\nstatic int n;\n" +
		"static void record(tack_any a) {\n" +
		"    int64_t v = -1;\n    tack_string s;\n" +
		"    if (tack_any_string(a, &s)) { v = 100 + s.len; } else if (!tack_any_int64(a, &v)) { abort(); }\n" +
		"    seen[n++] = v;\n}\n" +
		"void one(tack_any a) { record(a); }\n" +
		"void many(tack_view_any xs) { for (int64_t i = 0; i < xs.len; i++) { record(xs.ptr[i]); } }\n" +
		"void both(int64_t *x, tack_any a) { *x = 7; record(a); }\n" +
		"__attribute__((destructor)) static void check(void) {\n" +
		"    static const int64_t want[9] = {1, 0, 1, 7, 101, 1, 1, 1, 101};\n" +
		"    if (n != 9) { abort(); }\n" +
		"    for (int i = 0; i < 9; i++) { if (seen[i] != want[i]) { abort(); } }\n}\n"
	bin := buildExtern(t, src, csrc)
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Errorf("running: %v\n%s", err, out)
	}
}

// TestExternAnyExampleIsWarningFree builds examples/extern-any with
// clang's warnings as errors, its C file against its tack.h, so tack_any,
// the view of them, the type ids, a view's among them, the view struct C
// reads one through, and the unpack helpers agree between the generated
// source, the header, std's tack.h and the C file, and checks every check
// it prints passed.
func TestExternAnyExampleIsWarningFree(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "extern-any")
	src := readFile(t, filepath.Join(dir, "main.tck"))
	csrc := readFile(t, filepath.Join(dir, "any.c"))
	out, err := exec.Command(buildExtern(t, src, csrc)).Output()
	if err != nil {
		t.Fatalf("extern-any failed: %v", err)
	}
	if strings.Contains(string(out), "FAIL") || strings.Count(string(out), " ok\n") != 15 {
		t.Errorf("extern-any printed\n%s", out)
	}
}

// TestHeaderDeclaresAny checks tack.h includes std's header, which
// defines tack_any, for a signature with any, defines tack_view_any for
// one with ...any, and declares each prototype with them.
func TestHeaderDeclaresAny(t *testing.T) {
	src := "package main\n\nextern func f(a any, xs ...any) int64\n\nextern func g(v []any)\n\n" +
		"func main() {\n}\n"
	got := string(c.New().Header(ast.SingleFile(compile(t, src))))
	for _, want := range []string{
		stdInclude,
		"typedef struct {\n    const tack_any *ptr;\n    int64_t len;\n} tack_view_any;\n",
		"int64_t f(tack_any /* a */, tack_view_any /* xs */); // extern func f(a any, xs ...any) int64\n",
		"void g(tack_view_any /* v */); // extern func g(v []any)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("tack.h lacks\n%s\nin\n%s", want, got)
		}
	}
	if strings.Count(got, "} tack_view_any;") != 1 {
		t.Errorf("tack.h defines tack_view_any more than once:\n%s", got)
	}
	only := "package main\n\nextern func f(a any)\n\nfunc main() {\n}\n"
	if got := string(
		c.New().Header(ast.SingleFile(compile(t, only))),
	); !strings.Contains(
		got,
		stdInclude,
	) {
		t.Errorf("tack.h for an any parameter does not include std's header:\n%s", got)
	}
}
