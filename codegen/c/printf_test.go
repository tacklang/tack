package c_test

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/internal/stdtest"
	"github.com/tacklang/tack/loader"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/typeck"
)

// compileWithStd runs the front end on src, a single file main.tck that
// may import the repository's standard library, and fails the test if any
// stage rejects it.
func compileWithStd(t *testing.T, src string) *ast.Program {
	t.Helper()
	l := &loader.Loader{
		FS:       loader.FromFS(fstest.MapFS{"main.tck": {Data: []byte(src)}}),
		Builtins: stdtest.MustBuiltins(),
		Std:      stdtest.Std(),
	}
	res, err := l.File("main.tck")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diags) > 0 {
		t.Fatalf("load: %s", diag.RenderAllPretty(res.Files, res.Diags))
	}
	if diags := resolver.New(res.Program, stdtest.MustBuiltins()).Resolve(); len(diags) > 0 {
		t.Fatalf("resolve: %s", diag.RenderAllPretty(res.Files, diags))
	}
	if diags := typeck.New(res.Program, res.Files, stdtest.MustBuiltins()).Check(); len(diags) > 0 {
		t.Fatalf("typeck: %s", diag.RenderAllPretty(res.Files, diags))
	}
	return res.Program
}

// buildWithStd builds src, as compileWithStd compiles it, with clang's
// warnings as errors, but for an unused function, and the extra flags: the generated source, the C
// files of the std packages it imports, each against the program's tack.h
// as the driver compiles them, and std/builtin's C. It returns the
// binary's path.
func buildWithStd(t *testing.T, src string, flags ...string) string {
	t.Helper()
	return buildWithStdAndC(t, src, "", flags...)
}

// buildWithStdAndC builds src as buildWithStd does, and with it csrc, the
// C file of the program's extern funcs, compiled against the program's
// tack.h as the driver compiles it; an empty csrc is no C file.
func buildWithStdAndC(t *testing.T, src, csrc string, flags ...string) string {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	return buildProgram(t, compileWithStd(t, src), csrc, flags...)
}

// buildProgram builds prog, a program the front end accepted, as
// buildWithStdAndC builds its source: the generated C, csrc, when not
// empty, and the C files of every package of prog, each named relative to
// the repository's root, against the program's tack.h, with std/builtin's
// C. It returns the binary's path.
func buildProgram(t *testing.T, prog *ast.Program, csrc string, flags ...string) string {
	t.Helper()
	out, diags := c.New().Generate(prog, stdtest.MustBuiltins())
	if len(diags) > 0 {
		t.Fatalf("generate reported %+v", diags)
	}
	dir := t.TempDir()
	program := filepath.Join(dir, "program.c")
	header := filepath.Join(dir, c.HeaderName)
	writeFile(t, program, string(out))
	writeFile(t, header, string(c.New().Header(prog)))

	// A function of an imported package that the program never calls,
	// such as std/fmt's println, is still generated, as a static function
	// nothing calls, which -Wall reports; nothing else is let through.
	strict := append([]string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-Wno-unused-function"},
		flags...)
	strict = append(strict, "-I", stdtest.IncludeDir())
	objs := []string{}
	if csrc != "" {
		cfile := filepath.Join(dir, "program_c.c")
		writeFile(t, cfile, csrc)
		obj := filepath.Join(dir, "program_c.o")
		args := append(append([]string{}, strict...),
			"-c", "-I", dir, "-include", header, "-o", obj, cfile)
		if msg, err := exec.Command("clang", args...).CombinedOutput(); err != nil {
			t.Fatalf("clang %s: %v\n%s", strings.Join(args, " "), err, msg)
		}
		objs = append(objs, obj)
	}
	for _, pkg := range prog.Packages {
		for i, f := range pkg.CFiles {
			obj := filepath.Join(dir, fmt.Sprintf("%s%d.o", pkg.Name, i))
			args := append(append([]string{}, strict...),
				"-c", "-I", dir, "-include", header, "-o", obj, filepath.Join(stdtest.Home(), f))
			if msg, err := exec.Command("clang", args...).CombinedOutput(); err != nil {
				t.Fatalf("clang %s: %v\n%s", strings.Join(args, " "), err, msg)
			}
			objs = append(objs, obj)
		}
	}
	bin := filepath.Join(dir, "prog")
	args := append(append([]string{}, strict...), "-o", bin, program)
	args = append(append(args, objs...), stdtest.MustBuiltins().CFiles()...)
	if msg, err := exec.Command("clang", args...).CombinedOutput(); err != nil {
		t.Fatalf("clang %s: %v\n%s\n%s", strings.Join(args, " "), err, msg, out)
	}
	return bin
}

// printfExample returns examples/printf's program and the output it must
// write, byte for byte.
func printfExample(t *testing.T) (src, want string) {
	t.Helper()
	dir := filepath.Join("..", "..", "examples", "printf")
	return readFile(t, filepath.Join(dir, "main.tck")), readFile(t, filepath.Join(dir, "output"))
}

// TestPrintfExampleIsSanitizerClean builds examples/printf with clang's
// warnings as errors and the address and undefined behavior sanitizers
// on, std/fmt's C included, so a tack_any that points at a value no
// longer there, a read past a view or a format, or a zero string's null
// pointer moved, fails the run, and checks it writes exactly its output.
func TestPrintfExampleIsSanitizerClean(t *testing.T) {
	src, want := printfExample(t)
	bin := buildWithStd(t, src, "-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all")
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("printf failed: %v\n%s", err, out)
	}
	if string(out) != want {
		t.Errorf("printf wrote\n%q\nwant\n%q", out, want)
	}
}

// TestFmtWriteExampleIsSanitizerClean builds examples/fmt-write with
// clang's warnings as errors and the address and undefined behavior
// sanitizers on, std/fmt's C included, so an empty view's null pointer
// handed to fwrite, or a read past a view, fails the run, and checks it
// writes its output, then the 1 MiB list, byte i being i % 251.
func TestFmtWriteExampleIsSanitizerClean(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "fmt-write")
	src := readFile(t, filepath.Join(dir, "main.tck"))
	want := readFile(t, filepath.Join(dir, "output"))
	bin := buildWithStd(t, src, "-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all")
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("fmt-write failed: %v", err)
	}
	const big = 1 << 20
	if len(out) != len(want)+big {
		t.Fatalf("fmt-write wrote %d bytes, want %d", len(out), len(want)+big)
	}
	if string(out[:len(want)]) != want {
		t.Errorf("fmt-write wrote\n%q\nwant\n%q", out[:len(want)], want)
	}
	for i := range big {
		if got := out[len(want)+i]; got != byte(i%251) {
			t.Fatalf("byte %d of the large write is %#x, want %#x", i, got, byte(i%251))
		}
	}
}

// TestBytesExampleIsSanitizerClean builds examples/bytes, its C file and
// std's C with clang's warnings as errors and the address and undefined
// behavior sanitizers on, every finding fatal, and checks it writes
// exactly its output, every check ok: std/bytes, written in Tack, reads no
// view past its end, writes no array past its end, and leaks no list,
// which the example's own count of live buffers checks, through its
// failed appends and repeats too. Two of its repeats ask c_allocator for
// close to 2^63 bytes, which malloc refuses with a null pointer; the
// address sanitizer's malloc aborts instead unless told to return null as
// the real one does, so the run sets allocator_may_return_null.
func TestBytesExampleIsSanitizerClean(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "bytes")
	src := readFile(t, filepath.Join(dir, "main.tck"))
	csrc := readFile(t, filepath.Join(dir, "live.c"))
	want := readFile(t, filepath.Join(dir, "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/bytes/output expects a check to fail")
	}
	bin := buildWithStdAndC(
		t,
		src,
		csrc,
		"-O1",
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
	)
	var stderr strings.Builder
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=allocator_may_return_null=1")
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bytes failed: %v\n%s\nstderr:\n%s", err, out, stderr.String())
	}
	if string(out) != want {
		t.Errorf("bytes wrote\n%q\nwant\n%q", out, want)
	}
}

// intCase is one integer printf is given: its Tack type, the Tack
// expression of it, and the Go value of it, which Go's fmt formats for the
// expected output.
type intCase struct {
	val  any
	typ  string
	expr string
}

// intCases returns integers of every Tack integer type, its smallest and
// largest values, 0, 1 and -1 where it has them, and random ones from a
// fixed seed, so a run is repeatable. A target-width type's constants are
// at most 32 bits wide on every target, so its values are too.
func intCases() []intCase {
	r := rand.New(rand.NewPCG(1, 2))
	var cases []intCase
	signed := func(typ string, bits uint, conv func(int64) any) {
		lo, hi := int64(-1)<<(bits-1), int64(1)<<(bits-1)-1
		if bits == 64 {
			lo, hi = math.MinInt64, math.MaxInt64
		}
		vals := make([]int64, 0, 15)
		vals = append(vals, lo, lo+1, -1, 0, 1, hi-1, hi)
		for range 8 {
			v := int64(r.Uint64())
			if bits < 64 {
				v >>= 64 - bits
			}
			vals = append(vals, v)
		}
		for _, v := range vals {
			expr := typ + "(" + strconv.FormatInt(v, 10) + ")"
			if v == math.MinInt64 {
				expr = "int64(-9223372036854775807 - 1)"
			}
			cases = append(cases, intCase{conv(v), typ, expr})
		}
	}
	unsigned := func(typ string, bits uint, conv func(uint64) any) {
		hi := uint64(1)<<bits - 1
		if bits == 64 {
			hi = math.MaxUint64
		}
		vals := make([]uint64, 0, 12)
		vals = append(vals, 0, 1, hi-1, hi)
		for range 8 {
			vals = append(vals, r.Uint64()>>(64-bits))
		}
		for _, v := range vals {
			cases = append(
				cases,
				intCase{conv(v), typ, typ + "(" + strconv.FormatUint(v, 10) + ")"},
			)
		}
	}
	signed("int8", 8, func(v int64) any { return int8(v) })
	signed("int16", 16, func(v int64) any { return int16(v) })
	signed("int32", 32, func(v int64) any { return int32(v) })
	signed("int64", 64, func(v int64) any { return v })
	unsigned("uint8", 8, func(v uint64) any { return uint8(v) })
	unsigned("uint16", 16, func(v uint64) any { return uint16(v) })
	unsigned("uint32", 32, func(v uint64) any { return uint32(v) })
	unsigned("uint64", 64, func(v uint64) any { return v })
	unsigned("unsafe_size_t", 32, func(v uint64) any { return v })
	unsigned("unsafe_uintptr_t", 32, func(v uint64) any { return v })
	return cases
}

// TestPrintfFormatsIntegersLikeGo prints integers of every type with %d,
// %x and %v, through a known format and through one the compiler does
// not know, and checks each line against what Go's fmt writes for the
// same value, which writes a negative value's hex with a minus too.
func TestPrintfFormatsIntegersLikeGo(t *testing.T) {
	var src, want strings.Builder
	src.WriteString("package main\n\nimport fmt from \"std/fmt\"\n\n" +
		"func show(f string, args ...any) {\n    fmt.printf(f, args...)\n}\n\nfunc main() {\n")
	for _, ic := range intCases() {
		fmt.Fprintf(
			&src,
			"    fmt.printf(\"%%d %%x %%v|\", %s, %s, %s)\n",
			ic.expr,
			ic.expr,
			ic.expr,
		)
		fmt.Fprintf(&src, "    show(\"%%v %%x %%d\\n\", %s, %s, %s)\n", ic.expr, ic.expr, ic.expr)
		fmt.Fprintf(&want, "%d %x %v|%v %x %d\n", ic.val, ic.val, ic.val, ic.val, ic.val, ic.val)
	}
	src.WriteString("}\n")
	bin := buildWithStd(t, src.String(), "-O1", "-fsanitize=undefined", "-fno-sanitize-recover=all")
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if got := string(out); got != want.String() {
		gotLines, wantLines := strings.Split(got, "\n"), strings.Split(want.String(), "\n")
		for i := range min(len(gotLines), len(wantLines)) {
			if gotLines[i] != wantLines[i] {
				t.Errorf("line %d: got %q, want %q", i+1, gotLines[i], wantLines[i])
			}
		}
		if len(gotLines) != len(wantLines) {
			t.Errorf("got %d lines, want %d", len(gotLines), len(wantLines))
		}
	}
}

// goSprintf is Go's fmt.Sprintf, called through a variable so go vet
// does not reject the mistakes markerCases make on purpose.
var goSprintf = fmt.Sprintf

// markerCases are calls of show, which passes a format the compiler does
// not know to printf, whose mistakes Go's fmt writes the same way, with
// the same type names: the Go call's output is the expected one.
var markerCases = []struct {
	tack string
	goal string
}{
	{`show("%d|", "hi")`, goSprintf("%d|", "hi")},
	{`show("%s|", 5)`, goSprintf("%s|", int64(5))},
	{`show("%t|", 'a')`, goSprintf("%t|", uint8('a'))},
	{`show("%d|", true)`, goSprintf("%d|", true)},
	{`show("%x|", false)`, goSprintf("%x|", false)},
	{`show("%s|", int8(-1))`, goSprintf("%s|", int8(-1))},
	{`show("%t|", uint64(18446744073709551615))`, goSprintf("%t|", uint64(math.MaxUint64))},
	{`show("%d %d|", 1)`, goSprintf("%d %d|", int64(1))},
	{`show("%s|")`, goSprintf("%s|")},
	{`show("%z|", 1)`, goSprintf("%z|", int64(1))},
	{`show("%z|")`, goSprintf("%z|")},
	{`show("|", 1, "a", true, int16(-2))`, goSprintf("|", int64(1), "a", true, int16(-2))},
	{`show("%d|", 1, "")`, goSprintf("%d|", int64(1), "")},
	{`show("|%")`, goSprintf("|%")},
	{`show("%%|%%%d", 7)`, goSprintf("%%|%%%d", int64(7))},
	{`show("")`, goSprintf("")},
	{`show("", 1)`, goSprintf("", int64(1))},
	{
		`show("%v|%v|%v", "s", false, uint32(4294967295))`,
		goSprintf("%v|%v|%v", "s", false, uint32(math.MaxUint32)),
	},
}

// TestPrintfMarkersMatchGo passes printf formats it cannot check, each
// with a mistake, and checks the output against Go's fmt, which writes the
// same markers: %!verb(type=value), %!verb(MISSING), %!(EXTRA ...) and
// %!(NOVERB).
func TestPrintfMarkersMatchGo(t *testing.T) {
	var src, want strings.Builder
	src.WriteString("package main\n\nimport fmt from \"std/fmt\"\n\n" +
		"func show(f string, args ...any) {\n    fmt.printf(f, args...)\n}\n\nfunc main() {\n")
	for _, mc := range markerCases {
		src.WriteString("    " + mc.tack + "\n    fmt.printf(\"\\n\")\n")
		want.WriteString(mc.goal + "\n")
	}
	src.WriteString("}\n")
	bin := buildWithStd(
		t,
		src.String(),
		"-O1",
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
	)
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if string(out) != want.String() {
		t.Errorf("got\n%s\nwant\n%s", out, want.String())
	}
}

// harnessHeader stands in for a program's tack.h for a C file that calls
// printf's C directly: the view of anys, and the prototype.
const harnessHeader = "#include <tack/tack.h>\n" +
	"typedef struct {\n    const tack_any *ptr;\n    int64_t len;\n} tack_view_any;\n" +
	"void tack_fmt_printf(tack_string format, tack_view_any args);\n"

// TestPrintfCHandlesWhatTackNeverPasses calls printf's C from C with what
// no Tack program passes it, under the sanitizers: tack_anys whose ids
// are no type's, zero and past the last, and a zeroed format and view,
// whose pointers are null. Each must be written as a marker, never read.
func TestPrintfCHandlesWhatTackNeverPasses(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	dir := t.TempDir()
	header := filepath.Join(dir, c.HeaderName)
	writeFile(t, header, harnessHeader)
	harness := filepath.Join(dir, "harness.c")
	writeFile(t, harness, `#include "tack.h"
int main(void) {
    int64_t n = 5;
    tack_any bad[3] = {{0, &n}, {99, 0}, {TACK_TYPE_INT64, &n}};
    tack_fmt_printf(TACK_STR("%d %v %d|"), (tack_view_any){bad, 3});
    tack_fmt_printf(TACK_STR("|"), (tack_view_any){bad, 2});
    tack_fmt_printf((tack_string){0, 0}, (tack_view_any){0, 0});
    tack_fmt_printf((tack_string){0, 0}, (tack_view_any){bad + 2, 1});
    tack_fmt_printf(TACK_STR("%d"), (tack_view_any){0, 0});
    return 0;
}
`)
	bin := filepath.Join(dir, "harness")
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-O1",
		"-fsanitize=address,undefined", "-fno-sanitize-recover=all",
		"-I", dir, "-I", stdtest.IncludeDir(), "-o", bin, harness,
		filepath.Join(stdtest.Dir(), "fmt", "printf.c"),
	}
	if msg, err := exec.Command("clang", args...).CombinedOutput(); err != nil {
		t.Fatalf("clang: %v\n%s", err, msg)
	}
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
	want := "%!d(invalid) %!v(invalid) 5|" +
		"|%!(EXTRA invalid, invalid)" +
		"%!(EXTRA int64=5)" +
		"%!d(MISSING)"
	if string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// anyHelpersUse calls every unpack helper of std's tack.h, and
// TACK_ANY_IS, so a compile of it checks each one's body.
const anyHelpersUse = `#include <tack/tack.h>
bool use(tack_any a) {
    bool b; int8_t i8; int16_t i16; int32_t i32; int64_t i64;
    uint8_t u8; uint16_t u16; uint32_t u32; uint64_t u64;
    size_t size; uintptr_t addr; tack_string s;
    return tack_any_bool(a, &b) || tack_any_int8(a, &i8) || tack_any_int16(a, &i16) ||
        tack_any_int32(a, &i32) || tack_any_int64(a, &i64) || tack_any_uint8(a, &u8) ||
        tack_any_uint16(a, &u16) || tack_any_uint32(a, &u32) || tack_any_uint64(a, &u64) ||
        tack_any_unsafe_size_t(a, &size) || tack_any_unsafe_uintptr_t(a, &addr) ||
        tack_any_string(a, &s) || tack_any_signed(a, &i64) || tack_any_unsigned(a, &u64) ||
        TACK_ANY_IS(a, STRING);
}
`

// TestAnyHelpersCompileFor32Bits compiles std's tack.h's unpack helpers,
// and printf's C when clang has a C library for the target, for a 32-bit
// target with warnings as errors, where size_t and uintptr_t are 32 bits
// wide and a widening the helpers do could be one clang finds suspect.
// Only syntax and types are checked.
func TestAnyHelpersCompileFor32Bits(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	dir := t.TempDir()
	probe := filepath.Join(dir, "probe.c")
	writeFile(t, probe, "#include <stdbool.h>\n#include <stdint.h>\n#include <stddef.h>\nint x;\n")
	if err := exec.Command("clang", "-target", i386, "-fsyntax-only", probe).Run(); err != nil {
		t.Skipf("clang cannot compile for %s: %v", i386, err)
	}
	compile32(t, filepath.Join(dir, "helpers.c"), anyHelpersUse, "-ffreestanding")

	writeFile(t, probe, "#include <stdio.h>\nint x;\n")
	if err := exec.Command("clang", "-target", i386, "-fsyntax-only", probe).Run(); err != nil {
		t.Logf("printf's C not checked: clang has no C library for %s: %v", i386, err)
		return
	}
	writeFile(t, filepath.Join(dir, c.HeaderName), harnessHeader)
	compile32(t, filepath.Join(dir, "printf.c"),
		readFile(t, filepath.Join(stdtest.Dir(), "fmt", "printf.c")), "-I", dir)
}

// TestStdTackHDefinesAnyOnce checks std's tack.h is where tack_any is
// defined, and that the fixed type ids are the ones C is written against.
func TestStdTackHDefinesAnyOnce(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(stdtest.IncludeDir(), "tack", "tack.h"))
	if err != nil {
		t.Fatal(err)
	}
	h := string(data)
	for _, line := range []string{
		"typedef struct {\n    uint32_t type;\n    const void *ptr;\n} tack_any;\n",
		"#define TACK_TYPE_INVALID 0\n", "#define TACK_TYPE_BOOL 1\n",
		"#define TACK_TYPE_INT8 2\n", "#define TACK_TYPE_INT16 3\n",
		"#define TACK_TYPE_INT32 4\n", "#define TACK_TYPE_INT64 5\n",
		"#define TACK_TYPE_UINT8 6\n", "#define TACK_TYPE_UINT16 7\n",
		"#define TACK_TYPE_UINT32 8\n", "#define TACK_TYPE_UINT64 9\n",
		"#define TACK_TYPE_UNSAFE_SIZE_T 10\n", "#define TACK_TYPE_UNSAFE_UINTPTR_T 11\n",
		"#define TACK_TYPE_STRING 12\n",
	} {
		if strings.Count(h, line) != 1 {
			t.Errorf("std's tack.h does not hold %q exactly once", line)
		}
	}
}
