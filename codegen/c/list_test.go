package c_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/ast"
	"github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/internal/stdtest"
)

// listsWant is how many checks examples/lists prints, each "ok" when it
// passes.
const listsWant = 98

// externListsWant is how many checks examples/extern-lists prints.
const externListsWant = 19

// listBulkWant is how many checks examples/list-bulk prints.
const listBulkWant = 66

// buildExternWith compiles src, a program with extern funcs, and csrc,
// the C file defining them, against the program's tack.h, and std's C
// files, all with clang's warnings on and treated as errors and the extra
// flags, and links them. It returns the binary's path.
func buildExternWith(t *testing.T, src, csrc string, flags ...string) string {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	prog := ast.SingleFile(compile(t, src))
	out, diags := c.New().Generate(prog, stdtest.MustBuiltins())
	if len(diags) > 0 {
		t.Fatalf("generate reported %+v", diags)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "program.c"), string(out))
	writeFile(t, filepath.Join(dir, c.HeaderName), string(c.New().Header(prog)))
	writeFile(t, filepath.Join(dir, "helpers.c"), csrc)
	strict := append(
		[]string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-I", stdtest.IncludeDir()},
		flags...)
	obj := filepath.Join(dir, "helpers.o")
	bin := filepath.Join(dir, "prog")
	for _, args := range [][]string{
		append(append([]string{}, strict...), "-c", "-include", filepath.Join(dir, c.HeaderName),
			"-o", obj, filepath.Join(dir, "helpers.c")),
		append(append(append([]string{}, strict...), "-o", bin, filepath.Join(dir, "program.c"), obj),
			stdtest.MustBuiltins().CFiles()...),
	} {
		if msg, err := exec.Command("clang", args...).CombinedOutput(); err != nil {
			t.Fatalf("clang %s: %v\n%s\n%s", strings.Join(args, " "), err, msg, out)
		}
	}
	return bin
}

// runChecks runs bin and checks it exits 0 having printed want checks,
// each "ok", and none "FAIL".
func runChecks(t *testing.T, bin string, want int) {
	t.Helper()
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", filepath.Base(bin), err, out)
	}
	if strings.Contains(string(out), "FAIL") || strings.Count(string(out), " ok\n") != want {
		t.Errorf("printed\n%s\nwant %d checks, every one ok", out, want)
	}
}

// exampleFiles returns the Tack and C sources of the example dir.
func exampleFiles(t *testing.T, dir, cfile string) (src, csrc string) {
	t.Helper()
	ex := filepath.Join("..", "..", "examples", dir)
	return readFile(t, filepath.Join(ex, "main.tck")), readFile(t, filepath.Join(ex, cfile))
}

// TestListsExampleIsSanitizerClean builds examples/lists, its C file and
// std's C with the address and undefined behavior sanitizers, every
// finding fatal, and checks every check it prints passed: no list is read
// after it is freed or past its end, freed twice, or leaked, which the
// example's own count of live buffers checks after every check.
func TestListsExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "lists", "live.c")
	bin := buildExternWith(
		t,
		src,
		csrc,
		"-O1",
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
	)
	runChecks(t, bin, listsWant)
}

// TestListBulkExampleIsSanitizerClean builds examples/list-bulk, its C
// file and std's C with the address and undefined behavior sanitizers,
// every finding fatal, and checks every check it prints passed: no bulk
// append or reserve writes past a buffer, reads a freed one or leaks one,
// and none that fails, made to by c_allocator's testing hook or by a
// count no buffer can hold, asks the allocator for memory it cannot give.
func TestListBulkExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "list-bulk", "bulk.c")
	bin := buildExternWith(
		t,
		src,
		csrc,
		"-O1",
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
	)
	runChecks(t, bin, listBulkWant)
}

// TestExternListsExampleIsWarningFree builds examples/extern-lists with
// clang's warnings as errors, its C file against its tack.h, so the list
// structs, tack_allocator and std's list macros agree between the
// generated source, the header, std's tack.h and the C file, and checks
// every check it prints passed, under the sanitizers too.
func TestExternListsExampleIsWarningFree(t *testing.T) {
	src, csrc := exampleFiles(t, "extern-lists", "lists.c")
	runChecks(t, buildExternWith(t, src, csrc), externListsWant)
	runChecks(t, buildExternWith(t, src, csrc, "-O1", "-fsanitize=address,undefined",
		"-fno-sanitize-recover=all"), externListsWant)
}

// listCText is a program whose generated C listCases check.
const listCText = `package main

type S struct {
    a list[int64]
    b list[string]
}

func f(mut l list[int64], v int64) error {
    return append(mut l, v)
}

func grown(l list[int64]) list[int64] {
    mut m := make(list[int64], c_allocator)
    for i := range len(l) + 1 {
        err := append(mut m, i)
        if err.code != 0 {
            break
        }
    }
    return m
}

func g(n int64) list[int64] {
    mut l := make(list[int64], c_allocator)
    defer {
        print("deferred")
    }
    for mut i int64 = 0; i < n; i++ {
        err := f(mut l, i)
        if err.code != 0 {
            break
        }
    }
    for mut k := make(list[int64], c_allocator); len(k) < 2; k = grown(k) {
        if len(k) == 5 {
            break
        }
    }
    return l
}

func main() {
    mut l := g(3)
    m := l
    l = make(list[int64], c_allocator)
    s := S{a: m, b: make(list[string], c_allocator)}
    print("x")
    if len(s.a) > len(l) {
        print("y")
    }
}
`

// TestListLowering checks how listCText's lists lower: one struct per
// list type, of its buffer, length, capacity and allocator; make as an
// empty list holding its allocator; append through std's macro, giving
// the out of memory error when it fails; a move through a temporary that
// empties the variable; an assignment that computes the new list, frees
// the old one and stores the new one; frees at a block's end, last
// declared first, after the defers declared after them and before those
// declared before them, a struct's lists last field first; and a list a
// loop's init statement declares, freed when its post statement assigns
// it and when the loop is done.
func TestListLowering(t *testing.T) {
	got := generate(t, listCText)
	for _, want := range []string{
		"    int64_t *ptr;\n    int64_t len;\n    int64_t cap;\n    tack_allocator alloc;\n} tack_list_",
		"= ((tack_list_",
		"){.alloc = TACK_C_ALLOCATOR});",
		"return (TACK_LIST_APPEND(p_l, p_v) ? (tack_error){0} : (tack_error){1, {tack_str",
		"v_m = (tack_o",
		", v_l = (tack_list_",
		"){0}, tack_o",
		"tack_list_free_(v_l.ptr, v_l.cap, sizeof (int64_t), _Alignof (int64_t), v_l.alloc);\n    v_l = tack_o",
		"tack_list_free_(v_s.f_b.ptr, v_s.f_b.cap, sizeof (tack_string), _Alignof (tack_string), v_s.f_b.alloc);\n" +
			"    tack_list_free_(v_s.f_a.ptr, v_s.f_a.cap, sizeof (int64_t), _Alignof (int64_t), v_s.f_a.alloc);\n" +
			"    tack_list_free_(v_m.ptr, v_m.cap, sizeof (int64_t), _Alignof (int64_t), v_m.alloc);\n" +
			"    tack_list_free_(v_l.ptr, v_l.cap, sizeof (int64_t), _Alignof (int64_t), v_l.alloc);\n}",
		"tack_list_free_(v_k.ptr, v_k.cap, sizeof (int64_t), _Alignof (int64_t), v_k.alloc), v_k = tack_o",
		"    }\n        tack_list_free_(v_k.ptr, v_k.cap, sizeof (int64_t), _Alignof (int64_t), v_k.alloc);\n    }\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated C does not contain\n%s\n\n%s", want, got)
		}
	}
	// g's defer, written after l, runs before l is freed, at every exit.
	mainAt := strings.Index(got, "static void tack_4_main4_main(void) {")
	if mainAt < 0 {
		t.Fatalf("no main in the generated C:\n%s", got)
	}
	deferred := strings.Index(got, "tack_builtin_print((tack_string){tack_str")
	freed := strings.LastIndex(got[:mainAt], "tack_list_free_(v_l.ptr")
	if deferred < 0 || freed < 0 || deferred > freed {
		t.Errorf("g's defer is not lowered before l's free:\n%s", got)
	}
	if strings.Contains(got, "const tack_list_") {
		t.Errorf("a list variable is const, which a move cannot empty:\n%s", got)
	}
	runChecks(t, buildWith(t, listCText), 0)
}

// TestListInDeferredCode checks a list declared in deferred code, inside a
// loop a break leaves, is freed through a next variable of the deferred
// code's own, apart from the function's, which a return out of a loop
// body freeing a list of its own uses, so the route the exit running the
// defer stored survives it: both returns still return what they returned,
// every list is freed, and the build is clean under the sanitizers.
func TestListInDeferredCode(t *testing.T) {
	src := `package main

func g(n int64) int64 {
    mut l := make(list[int64], c_allocator)
    defer {
        for i := range 3 {
            mut d := make(list[int64], c_allocator)
            err := append(mut d, i)
            if err.code != 0 || i == 1 {
                break
            }
        }
    }
    for i := range n {
        mut inner := make(list[int64], c_allocator)
        err := append(mut l, i)
        errInner := append(mut inner, i)
        if err.code != 0 || errInner.code != 0 {
            return -1
        }
        if i == 5 {
            return len(l) + len(inner) - 1
        }
    }
    return 0
}

func main() {
    if g(10) == 6 && g(2) == 0 {
        print("ok\n")
    } else {
        print("FAIL\n")
    }
}
`
	bin := buildWith(t, src, "-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all")
	out, err := exec.Command(bin).Output()
	if err != nil || string(out) != "ok\n" {
		t.Errorf("printed %q, %v; want ok", out, err)
	}
	got := generate(t, src)
	if strings.Count(got, "int tack_next") < 2 {
		t.Errorf("the deferred body has no next variable of its own:\n%s", got)
	}
}

// listGrowTest is a C program that drives std's list functions directly,
// each check printing its name and "ok" or "FAIL".
const listGrowTest = `#include <stdio.h>
#include <stdint.h>
#include <tack/tack.h>

typedef struct {
    int64_t *ptr;
    int64_t len;
    int64_t cap;
    tack_allocator alloc;
} ints;

typedef struct {
    char unused;
} empty;

typedef struct {
    empty *ptr;
    int64_t len;
    int64_t cap;
    tack_allocator alloc;
} empties;

static int evals = 0;

static ints *once(ints *l) {
    evals++;
    return l;
}

static void check(int cond, const char *name) {
    printf("%s %s\n", name, cond ? "ok" : "FAIL");
}

int main(void) {
    int64_t cap = 7;
    void *p = tack_list_grow_(0, 0, -1, 8, 8, TACK_C_ALLOCATOR, &cap);
    check(p == 0 && cap == 7, "negative need fails");

    p = tack_list_grow_(0, 0, INT64_MAX / 4, 16, 8, TACK_C_ALLOCATOR, &cap);
    check(p == 0 && cap == 7 && tack_c_allocator_live_() == 0, "size past ptrdiff_t fails");

    p = tack_list_grow_(0, 0, INT64_MAX, SIZE_MAX, 1, TACK_C_ALLOCATOR, &cap);
    check(p == 0 && cap == 7, "size that wraps fails");

    p = tack_list_grow_(0, 0, 1, 8, 4096, TACK_C_ALLOCATOR, &cap);
    check(p == 0 && cap == 7, "alignment past malloc's fails");

    tack_allocator stale = {0, 1}, none = {9, 0};
    p = tack_list_grow_(0, 0, 1, 8, 8, stale, &cap);
    check(p == 0 && tack_list_grow_(0, 0, 1, 8, 8, none, &cap) == 0, "no allocator fails");

    p = tack_list_grow_(0, 0, 1, 0, 1, TACK_C_ALLOCATOR, &cap);
    check(p != 0 && cap >= 1 && tack_c_allocator_live_() == 1, "zero-size elements grow");
    tack_list_free_(p, cap, 0, 1, TACK_C_ALLOCATOR);
    check(tack_c_allocator_live_() == 0, "zero-size elements freed");

    p = tack_list_grow_(0, 0, 1, 8, 8, TACK_C_ALLOCATOR, &cap);
    int64_t first = cap;
    void *same = tack_list_grow_(p, cap, cap, 8, 8, TACK_C_ALLOCATOR, &cap);
    check(p != 0 && first == 4 && same == p && cap == 4, "room already is no growth");
    p = tack_list_grow_(p, cap, cap + 1, 8, 8, TACK_C_ALLOCATOR, &cap);
    check(p != 0 && cap == 8 && tack_c_allocator_live_() == 1, "growth doubles");
    tack_list_free_(p, cap, 8, 8, stale);
    check(tack_c_allocator_live_() == 1, "free through no allocator does nothing");
    tack_list_free_(p, cap, 8, 8, TACK_C_ALLOCATOR);
    tack_list_free_(0, 0, 8, 8, TACK_C_ALLOCATOR);
    check(tack_c_allocator_live_() == 0, "freed, and a null buffer is nothing");

    ints l = {0, 0, 0, TACK_C_ALLOCATOR};
    tack_c_allocator_fail_after_(0);
    check(!TACK_LIST_APPEND(&l, 1) && l.ptr == 0 && l.len == 0 && l.cap == 0, "refused growth leaves the list");
    tack_c_allocator_fail_after_(-1);
    int ok = 1;
    for (int64_t i = 0; i < 1000; i++) {
        ok = ok && TACK_LIST_APPEND(once(&l), i * 2);
    }
    check(ok && evals == 1000 && l.len == 1000 && l.cap >= 1000, "appends grow");
    ok = 1;
    for (int64_t i = 0; i < TACK_LIST_LEN(l); i++) {
        ok = ok && TACK_LIST_AT(l, i) == i * 2;
    }
    check(ok, "elements survive growth");
    while (l.len < l.cap) {
        TACK_LIST_APPEND(&l, 0);
    }
    check(TACK_LIST_APPEND(&l, TACK_LIST_AT(l, 3)) && TACK_LIST_AT(l, l.len - 1) == 6,
          "a value read from the list before it grows");
    tack_list_free_(l.ptr, l.cap, sizeof *l.ptr, _Alignof(int64_t), l.alloc);

    ints r = {0, 0, 0, TACK_C_ALLOCATOR};
    check(TACK_LIST_RESERVE(&r, 0) && r.ptr == 0 && r.cap == 0 && tack_c_allocator_live_() == 0,
          "reserving nothing allocates nothing");
    check(!TACK_LIST_RESERVE(&r, -1) && r.ptr == 0 && r.cap == 0, "a negative reserve fails");
    check(!TACK_LIST_RESERVE(&r, INT64_MAX) && r.ptr == 0 && tack_c_allocator_live_() == 0,
          "a reserve past what fits fails");
    evals = 0;
    int64_t more = 3;
    check(TACK_LIST_RESERVE(once(&r), more++) && evals == 1 && more == 4 && r.cap == 4 && r.len == 0,
          "reserve evaluates its arguments once, and starts at 4");
    int64_t *at = r.ptr;
    tack_c_allocator_fail_after_(0);
    check(TACK_LIST_APPEND_N(&r, ((const int64_t[3]){1, 2, 3}), 3) && r.ptr == at && r.len == 3,
          "reserved room needs no allocation");
    check(!TACK_LIST_APPEND_N(&r, ((const int64_t[2]){4, 5}), 2) && r.len == 3 && r.cap == 4 && r.ptr == at,
          "a refused bulk append leaves the list");
    check(!TACK_LIST_RESERVE(&r, 2) && r.len == 3 && r.cap == 4 && r.ptr == at,
          "a refused reserve leaves the list");
    check(TACK_LIST_APPEND_N(&r, (const int64_t *)0, 0) && r.len == 3, "nothing appends nothing");
    tack_c_allocator_fail_after_(-1);
    check(!TACK_LIST_RESERVE(&r, INT64_MAX - 2) && !TACK_LIST_RESERVE(&r, INT64_MAX) && r.cap == 4,
          "len plus n past int64 fails");
    check(!TACK_LIST_APPEND_N(&r, ((const int64_t[1]){9}), INT64_MAX) && r.len == 3 && r.cap == 4,
          "a count past int64 fails");
    check(TACK_LIST_AT(r, 0) == 1 && TACK_LIST_AT(r, 1) == 2 && TACK_LIST_AT(r, 2) == 3,
          "elements kept after failures");
    check(TACK_LIST_APPEND(&r, 4) && r.len == r.cap, "a full list");
    check(TACK_LIST_APPEND_N(&r, ((const int64_t[2]){TACK_LIST_AT(r, 3), TACK_LIST_AT(r, 0)}), 2) &&
              r.len == 6 && r.cap == 8 && TACK_LIST_AT(r, 4) == 4 && TACK_LIST_AT(r, 5) == 1,
          "values read from the list before it grows");
    evals = 0;
    int64_t src[20], *srcp = src, count = 20;
    for (int64_t i = 0; i < 20; i++) {
        src[i] = 100 + i;
    }
    check(TACK_LIST_APPEND_N(once(&r), srcp++, count++) && evals == 1 && srcp == src + 1 && count == 21 &&
              r.len == 26 && r.cap == 26 && TACK_LIST_AT(r, 25) == 119,
          "a bulk append evaluates its arguments once, and fits exactly past doubling");
    tack_list_free_(r.ptr, r.cap, sizeof *r.ptr, _Alignof(int64_t), r.alloc);

    ints g = {0, 0, 0, TACK_C_ALLOCATOR};
    int64_t moves = 0, last = 0;
    ok = 1;
    for (int64_t i = 0; i < 100000; i++) {
        ok = ok && TACK_LIST_RESERVE(&g, 1) && TACK_LIST_APPEND(&g, i);
        if (g.cap != last) {
            moves++;
            last = g.cap;
        }
    }
    check(ok && g.len == 100000 && moves <= 18, "reserving one before each append still doubles");
    tack_list_free_(g.ptr, g.cap, sizeof *g.ptr, _Alignof(int64_t), g.alloc);

    empties e = {0, 0, 0, TACK_C_ALLOCATOR};
    empty x = {0};
    check(TACK_LIST_APPEND(&e, x) && TACK_LIST_APPEND(&e, x) && e.len == 2, "structs of one byte");
    tack_list_free_(e.ptr, e.cap, sizeof *e.ptr, _Alignof(empty), e.alloc);
    check(tack_c_allocator_live_() == 0, "nothing live");
    return 0;
}
`

// TestListGrowFunction drives std's list functions from C, compiled with
// the sanitizers and std's list.c: a size that overflows size_t or
// ptrdiff_t, a negative need, an alignment malloc does not give and a
// handle that is no allocator's all fail cleanly, leaving everything as
// it was; zero-size elements grow; room already there is no growth;
// growth doubles from 4; a refused growth leaves the list; the macros
// evaluate their list once, keep elements across growth and take a value
// read from the list before it moves; TACK_LIST_RESERVE and
// TACK_LIST_APPEND_N evaluate each argument once, allocate nothing for
// nothing, fail on a negative count and on a length past int64_t's, grow
// once, all or nothing, and grow by doubling when reserving one before
// each append; and every buffer is freed.
func TestListGrowFunction(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	dir := t.TempDir()
	main := filepath.Join(dir, "main.c")
	writeFile(t, main, listGrowTest)
	bin := filepath.Join(dir, "grow")
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-O1",
		"-fsanitize=address,undefined", "-fno-sanitize-recover=all",
		"-I", stdtest.IncludeDir(), "-o", bin, main, filepath.Join(stdtest.BuiltinDir(), "list.c"),
	}
	if out, err := exec.Command("clang", args...).CombinedOutput(); err != nil {
		t.Fatalf("clang: %v\n%s", err, out)
	}
	runChecks(t, bin, 32)
}

// TestListsCompileFor32Bits compiles examples/lists' generated C, and
// examples/extern-lists' C file against its tack.h, for a 32-bit target,
// where size_t is narrower than a list's int64_t length, with warnings as
// errors, and std's list.c too when the target's C library headers are
// there.
func TestListsCompileFor32Bits(t *testing.T) {
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
	src, _ := exampleFiles(t, "lists", "live.c")
	compile32(t, filepath.Join(dir, "lists.c"), generate(t, src))

	src, csrc := exampleFiles(t, "extern-lists", "lists.c")
	prog := ast.SingleFile(compile(t, src))
	header := filepath.Join(dir, c.HeaderName)
	writeFile(t, header, string(c.New().Header(prog)))
	compile32(t, filepath.Join(dir, "extern.c"), csrc, "-include", header)

	writeFile(t, probe, "#include <stdlib.h>\n")
	if err := exec.Command("clang", "-target", i386, "-fsyntax-only", probe).Run(); err != nil {
		t.Logf("no C library headers for %s, so std's list.c is not compiled for it: %v", i386, err)
		return
	}
	data, err := os.ReadFile(filepath.Join(stdtest.BuiltinDir(), "list.c"))
	if err != nil {
		t.Fatal(err)
	}
	compile32(t, filepath.Join(dir, "list.c"), string(data))
}

// bulkCText is a program whose generated C TestBulkAppendLowering checks:
// appends of several values, of none and of a spread, and reserves of a
// constant and of a variable count.
const bulkCText = `package main

func next(mut n int64) int64 {
    n++
    return n
}

func main() {
    mut l := make(list[int64], c_allocator)
    mut n int64 = 0
    e1 := append(mut l, next(mut n), next(mut n), next(mut n))
    e2 := append(mut l)
    a := [2]int64{7, 8}
    e3 := append(mut l, a[1:]...)
    e4 := reserve(mut l, 4)
    e5 := reserve(mut l, n - 4)
    ok := e1.code == 0 && e2.code == 0 && e3.code == 0 && e4.code == 0 && e5.code == 2
    if ok && len(l) == 4 && l[0] == 1 && l[1] == 2 && l[2] == 3 && l[3] == 8 {
        print("bulk ok\n")
    } else {
        print("bulk FAIL\n")
    }
}
`

// TestBulkAppendLowering checks how bulkCText's appends and reserves
// lower: values whose order shows held in temporaries, in order, then
// appended through TACK_LIST_APPEND_N from a compound literal array; no
// values as a null source and a count of 0; a spread view held in a
// temporary, so it is evaluated once; a constant count reserved with no
// test for a negative one; and a variable count tested first, giving the
// negative count error, code 2. The program builds with warnings as
// errors, and under the sanitizers, and does what it says. A program
// whose every reserve has a constant count pools no negative count
// message, which would be an unused array.
func TestBulkAppendLowering(t *testing.T) {
	got := generate(t, bulkCText)
	for _, want := range []string{
		"TACK_LIST_APPEND_N(&v_l, ((const int64_t[3]){tack_o",
		"TACK_LIST_APPEND_N(&v_l, 0, 0)",
		", TACK_LIST_APPEND_N(&v_l, tack_v",
		".ptr, tack_v",
		"(TACK_LIST_RESERVE(&v_l, 4) ? (tack_error){0} : (tack_error){1, {tack_str",
		" < 0 ? (tack_error){2, {tack_str",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated C does not contain\n%s\n\n%s", want, got)
		}
	}
	runChecks(t, buildWith(t, bulkCText), 1)
	runChecks(t, buildWith(t, bulkCText, "-O1", "-fsanitize=address,undefined",
		"-fno-sanitize-recover=all"), 1)

	constOnly := strings.Replace(bulkCText, "reserve(mut l, n - 4)", "reserve(mut l, 0)", 1)
	constOnly = strings.Replace(constOnly, "e5.code == 2", "e5.code == 0", 1)
	if got := generate(t, constOnly); strings.Contains(got, " < 0 ? (tack_error){2") ||
		strings.Contains(got, "{110, 101, 103, 97, 116, 105, 118, 101") {
		t.Errorf("a constant count is tested, or negative count pooled:\n%s", got)
	}
	runChecks(t, buildWith(t, constOnly), 1)
}
