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

// stddefCases are programs and whether the C generated for each must
// include <stddef.h>: exactly when something in it is an unsafe_size_t,
// C's size_t, whether a variable, a parameter, a result, a conversion's
// target, an array's element or a struct's field. uintptr_t comes from
// <stdint.h>, which every program includes, so unsafe_uintptr_t alone
// needs nothing more.
var stddefCases = []struct {
	name, src string
	want      bool
}{
	{"no target-width type", "package main\n\nfunc main() {\n    x := 1\n    exit(x)\n}\n", false},
	{
		"unsafe_uintptr_t only",
		"package main\n\nfunc f(p unsafe_uintptr_t) unsafe_uintptr_t {\n    return p + 1\n}\n\n" +
			"func main() {\n    var w uint64 = f(1)\n    if w == 2 {\n        exit(1)\n    }\n}\n",
		false,
	},
	{
		"a parameter",
		"package main\n\nfunc f(s unsafe_size_t) {\n}\n\nfunc main() {\n    f(1)\n}\n",
		true,
	},
	{
		"a result",
		"package main\n\nfunc f() unsafe_size_t {\n    return 1\n}\n\nfunc main() {\n    var w uint64 = f()\n" +
			"    if w == 1 {\n        exit(1)\n    }\n}\n",
		true,
	},
	{
		"a conversion",
		"package main\n\nfunc main() {\n    var w uint64 = unsafe_size_t(3)\n    if w == 3 {\n        exit(1)\n    }\n}\n",
		true,
	},
	{
		"a struct field",
		"package main\n\ntype B struct {\n    n unsafe_size_t\n}\n\nfunc main() {\n    b := B{n: 0}\n" +
			"    if b.n == 0 {\n        exit(1)\n    }\n}\n",
		true,
	},
	{
		"an array element",
		"package main\n\nfunc main() {\n    a := [2]unsafe_size_t{1, 2}\n    if a[0] == 1 {\n        exit(1)\n    }\n}\n",
		true,
	},
}

// TestStddefOnlyWhereSizeTIsUsed checks the generated source includes
// <stddef.h> only for a program that uses size_t, so every other program's
// preamble is unchanged.
func TestStddefOnlyWhereSizeTIsUsed(t *testing.T) {
	for _, tt := range stddefCases {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Contains(generate(t, tt.src), "#include <stddef.h>\n")
			if got != tt.want {
				t.Errorf(
					"includes <stddef.h> = %v, want %v:\n%s",
					got,
					tt.want,
					generate(t, tt.src),
				)
			}
		})
	}
}

// headerCases are extern funcs and whether tack.h must include
// <stddef.h> for them: when size_t is in a signature, directly, behind a
// mut parameter, or inside a struct or an array it uses.
var headerCases = []struct {
	name, decl string
	want       bool
}{
	{"no target-width type", "extern func f(x int64) int64", false},
	{"unsafe_uintptr_t only", "extern func f(p unsafe_uintptr_t) unsafe_uintptr_t", false},
	{"a parameter", "extern func f(n unsafe_size_t)", true},
	{"a mut parameter", "extern func f(mut n unsafe_size_t)", true},
	{"a result", "extern func f() unsafe_size_t", true},
	{"one of several results", "extern func f() (bool, unsafe_size_t)", true},
	{"an array element", "extern func f(a [2]unsafe_size_t)", true},
	{"a struct field", "type B struct {\n    n unsafe_size_t\n}\n\nextern func f(b B)", true},
}

// TestHeaderIncludesStddefForSizeT checks tack.h includes <stddef.h>
// exactly when an extern func's signature uses size_t, and declares the
// target-width types as size_t and uintptr_t.
func TestHeaderIncludesStddefForSizeT(t *testing.T) {
	for _, tt := range headerCases {
		t.Run(tt.name, func(t *testing.T) {
			src := "package main\n\n" + tt.decl + "\n\nfunc main() {\n}\n"
			got := string(c.New().Header(ast.SingleFile(compile(t, src))))
			if strings.Contains(got, "#include <stddef.h>\n") != tt.want {
				t.Errorf("tack.h includes <stddef.h> = %v, want %v:\n%s", !tt.want, tt.want, got)
			}
		})
	}
}

// TestHeaderNamesTargetWidthTypes checks the C names tack.h gives the
// target-width types, their arrays, and a mut parameter of one.
func TestHeaderNamesTargetWidthTypes(t *testing.T) {
	src := "package main\n\nextern func f(mut s unsafe_size_t, p unsafe_uintptr_t, a [2]unsafe_uintptr_t) unsafe_size_t\n\n" +
		"func main() {\n}\n"
	got := string(c.New().Header(ast.SingleFile(compile(t, src))))
	for _, want := range []string{
		"typedef struct {\n    uintptr_t e[2];\n} tack_array_2_unsafe_uintptr_t;\n",
		"size_t f(size_t * /* s */, uintptr_t /* p */, tack_array_2_unsafe_uintptr_t /* a */); " +
			"// extern func f(mut s unsafe_size_t, p unsafe_uintptr_t, a [2]unsafe_uintptr_t) unsafe_size_t\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("tack.h is missing %q:\n%s", want, got)
		}
	}
}

// i386 is the clang target the 32-bit checks compile for: one where size_t
// and uintptr_t are 32 bits wide, unlike the 64-bit host the other tests
// run on.
const i386 = "i386-unknown-linux-gnu"

// width32Program uses the target-width types every way the backend
// lowers them, and calls no builtin, so the C generated for it needs no C
// library header, which a 32-bit target's may not be installed on the
// host: checked conversions to and from every other integer type,
// arithmetic, comparisons with wider and narrower types, and with signed
// types and each other, by value, constants of other types included,
// widening, a struct, an array, a switch, one with case values of other
// integer types, a range count, globals, a mut parameter, indexes of
// an array, a string and a view, proven by a guard, a counting loop or a
// known value, and checked, and slices of each with target-width bounds,
// proven by a guard and checked.
const width32Program = `package main

type B struct {
    n unsafe_size_t
    p unsafe_uintptr_t
}

var limit unsafe_size_t = 4294967295

mut count unsafe_uintptr_t

func grow(mut n unsafe_size_t, by uint32) {
    n += by
}

func convert(s unsafe_size_t, p unsafe_uintptr_t, i int64, u uint64, i8 int8, u32 uint32) bool {
    a, okA := unsafe_size_t(i)
    b, okB := unsafe_size_t(u)
    c, okC := unsafe_size_t(p)
    d, okD := unsafe_uintptr_t(s)
    e, okE := int64(s)
    f, okF := uint32(p)
    g, okG := int8(s)
    h, okH := unsafe_size_t(i8)
    k, okK := uint64(s)
    m, okM := unsafe_size_t(u32)
    return okA && okB && okC && okD && okE && okF && okG && okH && okK && okM &&
        a == b && c == 1 && d == 2 && e == 1 && f == 2 && g == 3 && h == 4 && k == 5 && m == 6
}

func compare(s unsafe_size_t, u uint64, u32 uint32, u8 uint8) bool {
    var w uint64 = s
    return s < u && s > u32 && s != u8 && w == u && s == 4294967295 && s / 2 == 1 && s % 3 == 0
}

func mixed(s unsafe_size_t, p unsafe_uintptr_t, i int64, i8 int8, u64 uint64) bool {
    mut hits := 0
    switch s {
    case int64(-1), i8:
        hits++
    case p, i:
        hits--
    }
    return s > i && s >= i8 && p != i && p < int64(9223372036854775807) && s != p &&
        s <= u64 && i < s && i8 == p && s > int8(-1) && p == uint8(255) && hits == 0
}

func index(a [4]int64, str string, v []int64, s unsafe_size_t, p unsafe_uintptr_t) int64 {
    x, okA := a[s]
    y, okS := str[p]
    z, okV := v[s]
    mut sum int64 = x + y + z
    if okA && okS && okV && s < len(v) && p < len(a) && s < len(str) {
        sum += v[s] + a[p] + str[s]
    }
    for mut i unsafe_uintptr_t = 0; i < len(v); i++ {
        sum += v[i]
    }
    return sum + a[unsafe_size_t(3)]
}

func slices(a [4]int64, str string, v []int64, s unsafe_size_t, p unsafe_uintptr_t, i int64) int64 {
    t, okT := str[s:p]
    w, okW := str[i:s]
    x, okX := str[p:]
    mut n int64 = len(t) + len(w) + len(x)
    if okT && okW && okX && s <= p && p <= len(v) && s <= len(a) {
        n += len(v[s:p]) + len(a[s:]) + len(str[:0]) + index(a, str, v[:p], s, p)
    }
    return n
}

func main() {
    mut b := B{n: 1, p: 2}
    if mixed(b.n, b.p, -1, 3, 4) {
        b.n = 0
    }
    grow(mut b.n, 4294967295)
    a := [2]unsafe_uintptr_t{b.p, 3}
    a4 := [4]int64{1, 2, 3, 4}
    count += a[1] * b.p - 1
    for i := range b.p {
        count += i
    }
    switch b.n {
    case 0, 4294967295:
        count++
    default:
        count--
    }
    if convert(b.n, b.p, 1, 2, 3, 4) && compare(limit, 5, 6, 7) {
        count = 0
    }
    if count == 1 {
        count = 2
    }
    if index(a4, "abc", a4, b.n, b.p) == 0 {
        count = 3
    }
    if slices(a4, "abc", a4, b.n, b.p, 1) == 0 {
        count = 4
    }
}
`

// TestTargetWidthsCompileFor32Bits compiles the C generated for
// width32Program, and examples/extern-sizes' tack.h and C file, for a
// 32-bit target, with warnings as errors, so a comparison or conversion
// that is fine where size_t is 64 bits wide but suspect where it is 32,
// such as one clang can tell is always true, fails here. Only syntax and
// types are checked: the host cannot run the result. The test is skipped
// when clang cannot target i386.
func TestTargetWidthsCompileFor32Bits(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	dir := t.TempDir()
	probe := filepath.Join(dir, "probe.c")
	writeFile(
		t,
		probe,
		"#include <stdbool.h>\n#include <stdint.h>\n#include <stddef.h>\nsize_t n = SIZE_MAX;\n",
	)
	if err := exec.Command("clang", "-target", i386, "-fsyntax-only", probe).Run(); err != nil {
		t.Skipf("clang cannot compile for %s: %v", i386, err)
	}

	compile32(t, filepath.Join(dir, "program.c"), generate(t, width32Program))

	ex := filepath.Join("..", "..", "examples", "extern-sizes")
	prog := ast.SingleFile(compile(t, readFile(t, filepath.Join(ex, "main.tck"))))
	header := filepath.Join(dir, c.HeaderName)
	writeFile(t, header, string(c.New().Header(prog)))
	compile32(
		t,
		filepath.Join(dir, "sizes.c"),
		readFile(t, filepath.Join(ex, "sizes.c")),
		"-include",
		header,
	)
}

// TestTargetWidthIndexesRunStrictly runs, compiled with warnings as
// errors on the 64-bit host, a program reading a string parameter at a
// checked index of each target-width type and of uint64, in range, at the
// length and at the largest value, each of which C would compare with the
// length's int64_t across signs but for the casts, and reading an array at
// a proven one.
func TestTargetWidthIndexesRunStrictly(t *testing.T) {
	src := "package main\n\n" +
		"func at(str string, s unsafe_size_t, p unsafe_uintptr_t, w uint64, top unsafe_size_t) bool {\n" +
		"    v, inS := str[s]\n" +
		"    _, atLen := str[p]\n" +
		"    x, inW := str[w]\n" +
		"    _, atTop := str[top]\n" +
		"    return inS && v == 'c' && !atLen && inW && x == 'b' && !atTop\n" +
		"}\n\n" +
		"func main() {\n" +
		"    a := [3]int64{4, 5, 6}\n" +
		"    mut s unsafe_size_t = 0\n" +
		"    s--\n" +
		"    if at(\"abc\", 2, 3, 1, s) && s > len(a) {\n" +
		"        s = 2\n" +
		"    }\n" +
		"    if s < len(a) {\n" +
		"        exit(a[s])\n" +
		"    }\n" +
		"}\n"
	if out, status := run(t, src); out != "" || status != 6 {
		t.Errorf("got stdout %q, status %d; want \"\", 6", out, status)
	}
}

// compile32 writes src to path and checks it compiles for i386 with
// warnings as errors, passing clang the extra flags as well.
func compile32(t *testing.T, path, src string, flags ...string) {
	t.Helper()
	writeFile(t, path, src)
	args := append(
		[]string{"-target", i386, "-std=c11", "-Wall", "-Wextra", "-Werror", "-fsyntax-only"},
		flags...)
	if out, err := exec.Command("clang", append(args, path)...).CombinedOutput(); err != nil {
		t.Errorf("%s did not compile cleanly for %s: %v\n%s", filepath.Base(path), i386, err, out)
	}
}

// readFile returns the contents of the file at path.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// writeFile writes data to the file at path.
func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
