package c_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/internal/stdtest"
)

// TestOwnedListsExampleIsSanitizerClean builds examples/owned-lists, its
// C file and std's C with clang's warnings as errors and the address and
// undefined behavior sanitizers on, every finding fatal, and checks it
// writes exactly its output, every check ok: no element of a list of
// lists, of structs holding lists or of unions holding lists is read
// after it is freed, freed twice or leaked, whether it was replaced,
// popped, removed, appended by an append that failed, or left in the list
// when the list was reassigned, which the example's own count of live
// buffers checks after every group.
func TestOwnedListsExampleIsSanitizerClean(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "owned-lists")
	src := readFile(t, filepath.Join(dir, "main.tck"))
	csrc := readFile(t, filepath.Join(dir, "live.c"))
	want := readFile(t, filepath.Join(dir, "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/owned-lists/output expects a check to fail")
	}
	bin := buildWithStdAndC(t, src, csrc,
		"-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all")
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("owned-lists failed: %v\n%s", err, out)
	}
	if string(out) != want {
		t.Errorf("owned-lists wrote\n%s\nwant\n%s", out, want)
	}
}

// ownedCText holds lists of owned elements in the ways the lowering
// checks look for: a list of lists freed with its elements, an element
// replaced, a range binding an element in place, a checked pop and a
// checked remove, and values moved into a list by an append that may
// fail.
const ownedCText = `package main

func check(cond bool, name string) {
    print(name)
    if cond {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}

func words() list[list[int64]] {
    mut l := make(list[list[int64]], c_allocator)
    for i := range 3 {
        mut w := make(list[int64], c_allocator)
        e1 := append(mut w, i, i + 1)
        e2 := append(mut l, w)
        if e1.code != 0 || e2.code != 0 {
            return l
        }
    }
    return l
}

func main() {
    mut l := words()
    mut sum := 0
    for _, w := range l {
        for _, x := range w {
            sum += x
        }
    }
    check(sum == 9, "summed in place")
    if len(l) > 0 {
        l[0] = make(list[int64], c_allocator)
    }
    last, ok := pop(mut l)
    if ok {
        check(len(last) == 2 && len(l) == 2, "popped")
    }
    first, ok2 := remove(mut l, 0)
    if ok2 {
        check(len(first) == 0 && len(l) == 1, "removed")
    }
    a := make(list[int64], c_allocator)
    b := make(list[int64], c_allocator)
    err := append(mut l, a, b)
    check(err.code == 0 && len(l) == 3, "two moved in")
}
`

// TestOwnedListLowering checks how ownedCText's lists of lists lower: a
// free helper per list type whose elements are owned, freeing each
// element last first and then the buffer, which every free of such a list
// calls; an element replaced by computing the new list, freeing the old
// one through the element's own free and storing the new one; a range
// element bound as a pointer to the element; a checked pop and remove
// that read whether there is an element before taking it through std's
// macros, or give a zeroed list; and an append of owned values that holds
// each in a temporary and frees them, last first, when the buffer cannot
// grow. The program runs clean under the sanitizers.
func TestOwnedListLowering(t *testing.T) {
	got := generate(t, ownedCText)
	for _, want := range []string{
		"    for (int64_t i = v->len; i > 0; i--) {\n" +
			"        tack_list_free_(v->ptr[i - 1].ptr, v->ptr[i - 1].cap, sizeof (int64_t), _Alignof (int64_t), v->ptr[i - 1].alloc);\n" +
			"    }\n    tack_list_free_(v->ptr, v->cap, sizeof (tack_list_",
		"tack_list_free_(v_l.ptr[0].ptr, v_l.ptr[0].cap, sizeof (int64_t), _Alignof (int64_t), v_l.ptr[0].alloc);\n" +
			"        v_l.ptr[0] = tack_o",
		"const tack_list_",
		" *const v_w = &tack_r",
		"->len > 0;",
		" ? TACK_LIST_POP(tack_r",
		" >= 0 && tack_r",
		" ? TACK_LIST_REMOVE(tack_r",
		"TACK_LIST_APPEND_N(&v_l, ((const tack_list_",
		"[2]){tack_o",
		": (tack_list_free_(tack_o",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated C does not contain\n%s\n\n%s", want, got)
		}
	}
	if !strings.Contains(got, "tack_free_") || strings.Count(got, "static void tack_free_") != 1 {
		t.Errorf("want one free helper, for list[list[int64]], called where l is freed:\n%s", got)
	}
	runChecks(
		t,
		buildWith(
			t,
			ownedCText,
			"-O1",
			"-fsanitize=address,undefined",
			"-fno-sanitize-recover=all",
		),
		4,
	)
}

// takeMacrosTest drives std's TACK_LIST_POP and TACK_LIST_REMOVE from C,
// on a list of ints and on a list of lists, whose elements a list frees
// only while it holds them.
const takeMacrosTest = `#include <stdio.h>
#include <tack/tack.h>

typedef struct {
    int64_t *ptr;
    int64_t len;
    int64_t cap;
    tack_allocator alloc;
} ints;

typedef struct {
    ints *ptr;
    int64_t len;
    int64_t cap;
    tack_allocator alloc;
} lists;

static int evals = 0;

static ints *once(ints *l) {
    evals++;
    return l;
}

static void check(int cond, const char *name) {
    printf("%s %s\n", name, cond ? "ok" : "FAIL");
}

int main(void) {
    ints l = {0, 0, 0, TACK_C_ALLOCATOR};
    int ok = 1;
    for (int64_t i = 0; i < 6; i++) {
        ok = ok && TACK_LIST_APPEND(&l, i * 10);
    }
    check(ok && l.len == 6, "six appended");
    check(TACK_LIST_POP(once(&l)) == 50 && evals == 1 && l.len == 5, "pop takes the last, evaluating once");
    int64_t i = 0;
    evals = 0;
    check(TACK_LIST_REMOVE(once(&l), i++) == 0 && evals == 1 && i == 1 && l.len == 4, "remove the first");
    check(l.ptr[0] == 10 && l.ptr[1] == 20 && l.ptr[2] == 30 && l.ptr[3] == 40, "the rest moved down, in order");
    check(TACK_LIST_REMOVE(&l, 3) == 40 && l.len == 3 && l.ptr[2] == 30, "remove the last");
    check(TACK_LIST_REMOVE(&l, 1) == 20 && l.len == 2 && l.ptr[0] == 10 && l.ptr[1] == 30, "remove the middle");
    check(TACK_LIST_POP(&l) == 30 && TACK_LIST_POP(&l) == 10 && l.len == 0 && l.cap == 8, "popped empty, buffer kept");
    tack_list_free_(l.ptr, l.cap, sizeof *l.ptr, _Alignof(int64_t), l.alloc);

    lists ls = {0, 0, 0, TACK_C_ALLOCATOR};
    for (int64_t j = 0; j < 3; j++) {
        ints w = {0, 0, 0, TACK_C_ALLOCATOR};
        ok = ok && TACK_LIST_APPEND(&w, j) && TACK_LIST_APPEND(&ls, w);
    }
    check(ok && ls.len == 3 && tack_c_allocator_live_() == 4, "three lists in a list");
    ints taken = TACK_LIST_REMOVE(&ls, 0);
    check(taken.len == 1 && taken.ptr[0] == 0 && ls.ptr[0].ptr[0] == 1 && ls.len == 2, "a list removed");
    tack_list_free_(taken.ptr, taken.cap, sizeof *taken.ptr, _Alignof(int64_t), taken.alloc);
    for (int64_t j = ls.len; j > 0; j--) {
        tack_list_free_(ls.ptr[j - 1].ptr, ls.ptr[j - 1].cap, sizeof (int64_t), _Alignof(int64_t), ls.ptr[j - 1].alloc);
    }
    tack_list_free_(ls.ptr, ls.cap, sizeof *ls.ptr, _Alignof(ints), ls.alloc);
    check(tack_c_allocator_live_() == 0, "nothing live");
    return 0;
}
`

// TestTakeMacros drives std's TACK_LIST_POP and TACK_LIST_REMOVE from C,
// compiled with the sanitizers and std's list.c: each evaluates its list,
// and remove its index, once; pop takes the last element and remove the
// one at its index, the first, the last or one in the middle, moving the
// rest down in order; neither frees the buffer, so popping a list empty
// keeps its capacity; and a list taken out of a list of lists is its
// taker's to free.
func TestTakeMacros(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	dir := t.TempDir()
	main := filepath.Join(dir, "main.c")
	writeFile(t, main, takeMacrosTest)
	bin := filepath.Join(dir, "take")
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-O1",
		"-fsanitize=address,undefined", "-fno-sanitize-recover=all",
		"-I", stdtest.IncludeDir(), "-o", bin, main, filepath.Join(stdtest.BuiltinDir(), "list.c"),
	}
	if out, err := exec.Command("clang", args...).CombinedOutput(); err != nil {
		t.Fatalf("clang: %v\n%s", err, out)
	}
	runChecks(t, bin, 10)
}
