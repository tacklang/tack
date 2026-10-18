package c_test

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/internal/stdtest"
)

// sanitizers are clang's flags for the address and undefined behavior
// sanitizers, every finding fatal.
var sanitizers = []string{"-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"}

// runStatus runs bin and returns what it wrote to stdout and its exit
// status, failing the test when it could not be run at all.
func runStatus(t *testing.T, bin string) (string, int) {
	t.Helper()
	out, err := exec.Command(bin).Output()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exitErr):
		return string(out), exitErr.ExitCode()
	default:
		t.Fatalf("running %s: %v", bin, err)
		return "", 0
	}
}

// TestExitFreesExampleIsSanitizerClean builds examples/exit-frees, its C
// file and std's C with clang's warnings as errors and the sanitizers on,
// and checks it writes exactly its output and exits with status 3: exit,
// called eleven frames down with lists of lists, of unions and of structs
// holding lists still live in every frame, some moved by realloc, some
// left part way by appends made to fail, and c_allocator refusing every
// request, frees every buffer before the process ends, which live.c's
// destructor checks, and frees none twice, which the sanitizers check.
func TestExitFreesExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "exit-frees", "live.c")
	want := readFile(t, filepath.Join("..", "..", "examples", "exit-frees", "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/exit-frees/output expects a check to fail")
	}
	out, code := runStatus(t, buildWithStdAndC(t, src, csrc, sanitizers...))
	if out != want || code != 3 {
		t.Errorf("wrote\n%s\nand exited %d, want\n%s\nand 3", out, code, want)
	}
}

// exitPathPrelude declares the hooks of examples/exit-frees/live.c, whose
// destructor checks nothing is live as the process ends, a function
// making a list of n numbers, and one making such a list and freeing it.
const exitPathPrelude = `package main

extern("exit_live") func live() int64
extern("exit_fail_after") func fail_after(n int64)

func numbers(n int64) list[int64] {
    mut l := make(list[int64], c_allocator)
    for i := range n {
        err := append(mut l, i)
        if err.code != 0 {
            return l
        }
    }
    return l
}

func made(n int64) int64 {
    l := numbers(n)
    return len(l)
}

`

// TestNothingLiveAtProcessEndOnEveryPath builds programs that end in each
// way a program can, under the sanitizers with examples/exit-frees/live.c,
// and checks each ends with nothing live: a return from main, which frees
// every list on the way out, exit from main with lists live, exit with
// nothing ever allocated, exit after every list was freed, exit with a
// list of lists live and c_allocator refusing every request, and exit
// from inside a range over a list, whose buffer the loop is reading.
func TestNothingLiveAtProcessEndOnEveryPath(t *testing.T) {
	csrc := readFile(t, filepath.Join("..", "..", "examples", "exit-frees", "live.c"))
	const end = "nothing live at process end ok\n"
	for _, tc := range []struct {
		name, main string
		code       int
	}{
		{"return from main", "    l := numbers(9)\n    if len(l) != 9 || live() != 1 {\n" +
			"        exit(1)\n    }\n", 0},
		{"exit from main", "    a := numbers(3)\n    b := numbers(40)\n" +
			"    if len(a) + len(b) == 43 && live() == 2 {\n        exit(7)\n    }\n", 7},
		{"exit with nothing allocated", "    exit(0)\n", 0},
		{"exit after every list is freed", "    if made(5) == 5 && live() == 0 {\n" +
			"        exit(4)\n    }\n", 4},
		{"exit while refusing every request", "    mut ll := make(list[list[int64]], c_allocator)\n" +
			"    err := append(mut ll, numbers(1), numbers(20), numbers(0))\n" +
			"    if err.code == 0 && live() == 3 {\n        fail_after(0)\n        exit(5)\n    }\n", 5},
		{"exit inside a range", "    l := numbers(6)\n    for _, x := range l {\n" +
			"        if x == 3 {\n            exit(6)\n        }\n    }\n", 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := exitPathPrelude + "func main() {\n" + tc.main + "}\n"
			out, code := runStatus(t, buildWithStdAndC(t, src, csrc, sanitizers...))
			if out != end || code != tc.code {
				t.Errorf("wrote %q and exited %d, want %q and %d", out, code, end, tc.code)
			}
		})
	}
}

// TestExitEndsFunctionsWithResults builds, with clang's warnings as
// errors, at -O0 and -O2 and under the sanitizers, programs whose
// functions with results end in exit, with no return after it, and checks
// each exits with its status, the deferred code of the block it exits
// never run, a loop's passes before it having run theirs, and nothing
// live: a body ending in exit with a list live, an if whose branches
// return and exit, a switch whose default exits, and an exit nested in
// a loop with no condition, inside a block holding a list and a defer,
// and a list moved in a branch that exits, which the code after the
// branch reads, since the move never reaches it.
// The C function's result is never reached, which only a declaration of
// exit as never returning lets clang accept.
func TestExitEndsFunctionsWithResults(t *testing.T) {
	csrc := readFile(t, filepath.Join("..", "..", "examples", "exit-frees", "live.c"))
	const end = "nothing live at process end ok\n"
	for _, tc := range []struct {
		name, funcs, main string
		deferred          int
		code              int
	}{
		{
			"a body ending in exit", "func f(n int64) int32 {\n    l := numbers(n)\n" +
				"    defer {\n        print(\"deferred\\n\")\n    }\n" +
				"    if len(l) == n {\n        exit(3)\n    }\n    exit(4)\n}\n",
			"    exit(f(5))\n", 0, 3,
		},
		{
			"an if returning and exiting", "func f(k bool) list[int64] {\n" +
				"    if k {\n        return numbers(2)\n    } else {\n        l := numbers(4)\n" +
				"        if len(l) == 4 {\n            exit(4)\n        }\n        exit(1)\n    }\n}\n",
			"    a := f(true)\n    b := f(false)\n    if len(a) + len(b) > 0 {\n        exit(1)\n    }\n", 0, 4,
		},
		{
			"a switch whose default exits", "func f(d int32) int32 {\n    switch d {\n" +
				"    case 1:\n        return 10\n    default:\n        exit(d)\n    }\n}\n",
			"    exit(f(1) + f(9))\n", 0, 9,
		},
		{
			"a move in a branch that exits", "func take(own l list[int64]) int64 {\n    return len(l)\n}\n\n" +
				"func f(k bool) int32 {\n    l := numbers(3)\n    if k {\n        if take(own l) == 3 {\n" +
				"            exit(6)\n        }\n        exit(1)\n    }\n    if len(l) == 3 {\n        return 2\n    }\n" +
				"    return 0\n}\n",
			"    if f(false) == 2 {\n        exit(f(true))\n    }\n", 0, 6,
		},
		{
			"an exit in a loop with no condition", "func f() int32 {\n    mut i int64 = 0\n" +
				"    for {\n        l := numbers(i)\n        defer {\n            print(\"deferred\\n\")\n" +
				"        }\n        if len(l) == 3 {\n            exit(2)\n        }\n        i++\n    }\n}\n",
			"    exit(f())\n", 3, 2,
		},
	} {
		for _, opt := range []string{"-O0", "-O2"} {
			t.Run(tc.name+"/"+opt, func(t *testing.T) {
				src := exitPathPrelude + tc.funcs + "\nfunc main() {\n" + tc.main + "}\n"
				flags := append([]string{opt}, sanitizers[1:]...)
				out, code := runStatus(t, buildWithStdAndC(t, src, csrc, flags...))
				want := strings.Repeat("deferred\n", tc.deferred) + end
				if out != want || code != tc.code {
					t.Errorf("wrote %q and exited %d, want %q and %d", out, code, want, tc.code)
				}
			})
		}
	}
}

// allocatorTest drives c_allocator through std's list functions from C:
// buffers for elements of every fundamental alignment stay aligned for
// it through every move realloc makes, keep their elements, and are each
// freed once whether freed one at a time, left by a growth made to fail,
// or released all at once; releasing frees exactly what is still held,
// and the allocator works again after it.
const allocatorTest = `#include <stdio.h>
#include <stddef.h>
#include <tack/tack.h>

static void check(int cond, const char *name) {
    printf("%s %s\n", name, cond ? "ok" : "FAIL");
}

static int aligned(void *p, size_t a) {
    return p != 0 && (uintptr_t)p % a == 0;
}

// grow grows a list of cap elements of size elem and alignment align to
// at least need, writing byte pattern seed into the new room, and checks
// the old elements survived the move.
static unsigned char *grow(unsigned char *p, int64_t *cap, int64_t need, size_t elem, size_t align, int seed) {
    int64_t newcap = *cap;
    unsigned char *q = tack_list_grow_(p, *cap, need, elem, align, TACK_C_ALLOCATOR, &newcap);
    if (q == 0) {
        return 0;
    }
    for (int64_t i = 0; i < *cap * (int64_t)elem; i++) {
        if (q[i] != (unsigned char)(seed + i)) {
            return 0;
        }
    }
    for (int64_t i = 0; i < newcap * (int64_t)elem; i++) {
        q[i] = (unsigned char)(seed + i);
    }
    *cap = newcap;
    return q;
}

int main(void) {
    size_t sizes[] = {1, sizeof(int64_t), sizeof(long double), sizeof(max_align_t), 3, 24};
    size_t aligns[] = {1, _Alignof(int64_t), _Alignof(long double), _Alignof(max_align_t), 1, 8};
    enum { N = sizeof sizes / sizeof sizes[0] };
    unsigned char *bufs[N] = {0};
    int64_t caps[N] = {0};
    int ok = 1;

    // Interleaved growth, so every realloc relinks a buffer between others.
    for (int round = 0; round < 8; round++) {
        for (int i = 0; i < N; i++) {
            unsigned char *q = grow(bufs[i], &caps[i], caps[i] + 1, sizes[i], aligns[i], i);
            ok = ok && q != 0 && aligned(q, _Alignof(max_align_t));
            if (q != 0) {
                bufs[i] = q;
            }
        }
    }
    check(ok, "aligned and kept through every move");
    check(tack_c_allocator_live_() == N, "one buffer each");

    tack_list_free_(bufs[2], caps[2], sizes[2], aligns[2], TACK_C_ALLOCATOR);
    tack_list_free_(bufs[0], caps[0], sizes[0], aligns[0], TACK_C_ALLOCATOR);
    bufs[0] = bufs[2] = 0;
    check(tack_c_allocator_live_() == N - 2, "freed one at a time");

    tack_c_allocator_fail_after_(0);
    int64_t cap = caps[1];
    check(grow(bufs[1], &cap, caps[1] * 4, sizes[1], aligns[1], 1) == 0 && cap == caps[1],
          "a refused growth fails");
    tack_c_allocator_fail_after_(-1);
    check(grow(bufs[1], &caps[1], caps[1] * 4, sizes[1], aligns[1], 1) != 0,
          "the buffer a refused growth left is kept");

    int64_t huge = 0;
    int64_t hcap = PTRDIFF_MAX - 1;
    check(tack_list_grow_(0, 0, hcap, 1, 1, TACK_C_ALLOCATOR, &huge) == 0 && huge == 0,
          "no room for the header past the largest size");

    tack_allocators_release_all_();
    check(tack_c_allocator_live_() == 0, "release frees the rest");

    int64_t c2 = 0;
    unsigned char *again = grow(0, &c2, 3, 8, 8, 9);
    check(again != 0 && tack_c_allocator_live_() == 1, "allocates again after a release");
    tack_allocators_release_all_();
    tack_allocators_release_all_();
    check(tack_c_allocator_live_() == 0, "release again frees it once");
    return 0;
}
`

// TestAllocatorTracksEveryBuffer runs allocatorTest, compiled with the
// sanitizers and std's list.c, and checks every check it prints passed.
func TestAllocatorTracksEveryBuffer(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	dir := t.TempDir()
	main := filepath.Join(dir, "main.c")
	writeFile(t, main, allocatorTest)
	bin := filepath.Join(dir, "alloc")
	args := append([]string{"-std=c11", "-Wall", "-Wextra", "-Werror"}, sanitizers...)
	args = append(args, "-I", stdtest.IncludeDir(), "-o", bin, main,
		stdtest.RuntimeCFile("list.c"))
	if out, err := exec.Command("clang", args...).CombinedOutput(); err != nil {
		t.Fatalf("clang: %v\n%s", err, out)
	}
	runChecks(t, bin, 9)
}
