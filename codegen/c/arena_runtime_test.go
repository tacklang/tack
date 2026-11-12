package c_test

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

// arenaHooksC is the C half of the programs below: c_allocator's count of
// live buffers and its refusals, and the runtime's testing hook for the
// most elements an arena may hold, tack_arena_limit_, which lets a test
// reach the 2^32 - 1 limit's behavior without allocating 2^32 elements.
const arenaHooksC = `#include <tack/tack.h>

#include "tack.h"

int64_t arena_live(void) {
    return tack_c_allocator_live_();
}

void arena_fail_after(int64_t n) {
    tack_c_allocator_fail_after_(n);
}

void arena_limit(int64_t n) {
    tack_arena_limit_(n);
}
`

// arenaHooksPrelude declares arenaHooksC's functions, check, and a list
// maker, for the programs of TestArenaLimit.
const arenaHooksPrelude = `package main

extern("arena_live") func live() int64
extern("arena_fail_after") func fail_after(n int64)
extern("arena_limit") func limit(n int64)

func check(cond bool, name string) {
    print(name)
    if cond {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}

func numbers(n int64) list[int64] {
    mut l := make(list[int64], c_allocator)
    for i := range n {
        err := append(mut l, i +% 1)
        if err.code != 0 {
            return l
        }
    }
    return l
}

func total(v []int64) int64 {
    mut t int64 = 0
    for _, x := range v {
        t = t +% x
    }
    return t
}

`

// arenaLimitMain checks an arena at its limit, which arena_limit lowers
// from 2^32 - 1, its sentinel counted: add, add_all and reserve that would
// pass it fail with code 1, all or nothing, the arena as it was and every
// owned value they were given freed; ones that reach it exactly succeed;
// add_all of nothing succeeds at the limit; an arena already past a
// limit lowered under it grows no more; and the limit restored, by a
// negative count or one past 2^32 - 1, lets it grow again.
const arenaLimitMain = `func main() {
    limit(4)
    mut a := make(arena[int64], -1, c_allocator)
    _, e1 := add(mut a, 1)
    _, e2 := add(mut a, 2)
    last, e3 := add(mut a, 3)
    _, e4 := add(mut a, 4)
    mut z id[int64]
    check(e1.code == 0 && e2.code == 0 && e3.code == 0 && len(a) == 4 && a[last] == 3,
        "adds up to the limit succeed")
    check(e4.code == 1 && e3.code == 0 && len(a) == 4 && a[last] == 3 && a[z] == -1,
        "an add past the limit fails and leaves the arena as it was")
    none, eNone := add_all(mut a)
    check(eNone.code == 0 && len(a[none]) == 0 && len(a) == 4, "add_all of nothing succeeds at the limit")
    errR := reserve(mut a, 1)
    errR0 := reserve(mut a, 0)
    check(errR.code == 1 && errR0.code == 0 && len(a) == 4, "reserve past the limit fails")

    limit(6)
    _, eAll := add_all(mut a, 5, 6, 7)
    check(eAll.code == 1 && len(a) == 4, "an add_all past the limit adds nothing")
    l := numbers(3)
    _, eSpread := add_all(mut a, l...)
    check(eSpread.code == 1 && len(a) == 4, "a spread past the limit adds nothing")
    t, eFit := add_all(mut a, 5, 6)
    check(eFit.code == 0 && len(a[t]) == 2 && total(a[t]) == 11 && len(a) == 6,
        "an add_all that reaches the limit exactly succeeds")

    limit(5)
    _, eBelow := add(mut a, 8)
    _, eBelowAll := add_all(mut a, 8)
    check(eBelow.code == 1 && eBelowAll.code == 1 && len(a) == 6,
        "an arena past a lowered limit grows no more")

    limit(2)
    mut b := make(arena[list[int64]], numbers(1), c_allocator)
    _, eb1 := add(mut b, numbers(2))
    held := live()
    _, eb2 := add(mut b, numbers(3))
    afterAdd := live()
    m := numbers(4)
    n := numbers(5)
    _, eb3 := add_all(mut b, m, n)
    check(eb1.code == 0 && eb2.code == 1 && eb3.code == 1 && held == afterAdd && live() == held && len(b) == 2,
        "an add or add_all of owned values past the limit frees them")

    limit(-1)
    big, eBig := add(mut a, 9)
    limit(4294967296)
    big2, eBig2 := add(mut a, 10)
    check(eBig.code == 0 && eBig2.code == 0 && a[big] == 9 && a[big2] == 10 && len(a) == 8,
        "the limit restored lets the arena grow again")
}
`

// TestArenaLimit builds arenaLimitMain under each of sanitizerFlags, and
// checks every check it prints passed: the 2^32 - 1 limit on an arena's
// length, reached through the runtime's testing hook, makes add, add_all
// and reserve fail all or nothing, freeing what they were given, and
// lets them succeed up to it exactly.
func TestArenaLimit(t *testing.T) {
	src := arenaHooksPrelude + arenaLimitMain
	for _, flags := range sanitizerFlags {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, arenaHooksC, flags...), 10)
		})
	}
}

// arenaSpanClampMain checks spans of a bigger arena on a sentinel-only
// arena, whose list holds no buffer, and on one grown after them: every
// span not wholly in the arena, past its end or partly past it, is an
// empty view, ranged over, given to len and passed to a view parameter,
// and every one wholly in it views exactly its elements.
const arenaSpanClampMain = `func main() {
    mut a := make(arena[int64], 5, c_allocator)
    mut b := make(arena[int64], 0, c_allocator)
    mut bad int64 = 0
    for k := range 50 {
        s, err := add_all(mut b, k, k, k)
        if err.code != 0 {
            bad = bad +% 1
        } else {
            for _, v := range a[s] {
                bad = bad +% 1 +% v
            }
            bad = bad +% len(a[s]) +% total(a[s])
        }
    }
    check(bad == 0 && len(a) == 1, "every span of a bigger arena is empty on a sentinel-only one")
    first, err := add_all(mut a, 1, 2, 3, 4)
    if err.code != 0 {
        check(false, "spans past a grown arena's end are empty")
        return
    }
    mut c := make(arena[int64], 0, c_allocator)
    mut wrong int64 = 0
    for k := range 5 {
        s, errS := add_all(mut c, k, k)
        // c's spans start at 1, 3, 5, 7 and 9, each 2 long; a has ids 1 to 4.
        mut want int64 = 0
        if k < 2 {
            want = 2
        }
        if errS.code != 0 || len(a[s]) != want {
            wrong = wrong +% 1
        }
    }
    check(wrong == 0 && len(a[first]) == 4 && total(a[first]) == 10,
        "spans past a grown arena's end are empty")
}
`

// TestArenaSpansClampToTheArena builds arenaSpanClampMain under the
// sanitizers at -O0 and -O1, whose address sanitizer stops any read
// outside a buffer, and checks every check it prints passed.
func TestArenaSpansClampToTheArena(t *testing.T) {
	src := arenaHooksPrelude + arenaSpanClampMain
	for _, flags := range sanitizerFlags[1:] {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, arenaHooksC, flags...), 2)
		})
	}
}

// arenaAbortC is the C half of TestArenaFreedAtUnreachable: arena_abort
// reaches TACK_UNREACHABLE, as the generated C of an unreachable statement
// does, from a handler for SIGABRT that reports how many buffers
// c_allocator still holds once the abort comes, then lets it end the
// process.
const arenaAbortC = `#include <signal.h>
#include <stdio.h>

#include <tack/tack.h>

#include "tack.h"

int64_t arena_live(void) {
    return tack_c_allocator_live_();
}

static void report(int sig) {
    printf("live %lld\n", (long long)tack_c_allocator_live_());
    fflush(stdout);
    signal(sig, SIG_DFL);
    raise(sig);
}

void arena_abort(void) {
    signal(SIGABRT, report);
    TACK_UNREACHABLE("main.tck:1");
}
`

// arenaAbortSrc holds an arena of lists grown past its first buffer, its
// sentinel owned, and one in a struct, when it calls arena_abort.
const arenaAbortSrc = `package main

extern("arena_live") func live() int64
extern("arena_abort") func abort()

type Holder struct {
    a arena[list[int64]]
}

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

func main() {
    mut a := make(arena[list[int64]], numbers(3), c_allocator)
    for i := range 20 {
        _, err := add(mut a, numbers(i +% 1))
        if err.code != 0 {
            exit(1)
        }
    }
    mut h := Holder{a: make(arena[list[int64]], numbers(2), c_allocator)}
    _, err := add(mut h.a, numbers(4))
    if err.code != 0 || live() != 25 {
        exit(2)
    }
    print("held\n")
    abort()
}
`

// TestArenaFreedAtUnreachable builds arenaAbortSrc under the sanitizers
// and checks that reaching TACK_UNREACHABLE with arenas live frees every
// buffer they hold, their sentinels' among them, before the process ends
// by SIGABRT: the abort releases every allocator, as exit does, and an
// arena's buffers are its allocator's.
func TestArenaFreedAtUnreachable(t *testing.T) {
	atO0Too(t, sanitizers, func(t *testing.T, flags []string) {
		bin := buildWithStdAndC(t, arenaAbortSrc, arenaAbortC, flags...)
		cmd := exec.CommandContext(t.Context(), bin)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		err := cmd.Run()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("ran with %v, want an abort", err)
		}
		status, ok := exitErr.Sys().(syscall.WaitStatus)
		if !ok || !status.Signaled() || status.Signal() != syscall.SIGABRT {
			t.Errorf("ended with %v, want SIGABRT", exitErr)
		}
		if want := "held\nlive 0\n"; stdout.String() != want {
			t.Errorf(
				"stdout %q, want %q: the abort did not free every arena's buffers",
				stdout.String(),
				want,
			)
		}
	})
}

// arenaBorrowMain runs, as accepted programs, the safe forms of what an
// arena's borrow rules reject: a view a call returns, ranged over and
// passed, with the arena grown only after; an element passed as mut, or
// replaced, beside a value read from the arena that grows nothing; an
// element assigned a value computed from a grow first held in a local;
// and an id read only once its error is checked.
const arenaBorrowMain = `func get(a arena[int64], s span[int64]) []int64 {
    return a[s]
}

func grow(mut a arena[int64]) int64 {
    _, err := add(mut a, 100)
    if err.code != 0 {
        return 0
    }
    return 1
}

func set(mut x int64, y int64) {
    x = y
}

func main() {
    mut a := make(arena[int64], 0, c_allocator)
    s, err := add_all(mut a, 1, 2, 3)
    if err.code != 0 {
        check(false, "a returned view ranged over, then the arena grown")
        return
    }
    mut t int64 = 0
    for _, v := range get(a, s) {
        t = t +% v
    }
    n := total(get(a, s))
    for range 40 {
        _, e := add(mut a, t)
        if e.code != 0 {
            t = -1
        }
    }
    check(t == 6 && n == 6 && total(a[s]) == 6 && len(a) == 44, "a returned view ranged over, then the arena grown")
    mut z id[int64]
    set(mut a[z], a[z] +% 5)
    old := replace(mut a[z], a[z] +% 1)
    check(old == 5 && a[z] == 6, "an element passed as mut beside a read of it")
    g := grow(mut a)
    a[z] = g
    check(a[z] == 1 && len(a) == 45, "an element assigned a grow held first")
    e, errE := add(mut a, 9)
    if errE.code == 0 {
        a[e] = a[e] +% 1
    }
    check(errE.code == 0 && a[e] == 10, "an id read once its error is checked")
}
`

// TestArenaBorrowVariants builds arenaBorrowMain under each of
// sanitizerFlags, whose address sanitizer stops any use of a buffer an
// add moved, and checks every check it prints passed.
func TestArenaBorrowVariants(t *testing.T) {
	src := arenaHooksPrelude + arenaBorrowMain
	for _, flags := range sanitizerFlags {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, arenaHooksC, flags...), 4)
		})
	}
}

// arenaHeapRangeMain returns views of arrays reached through a range over
// an arena that is an element of a list parameter, which the caller's
// heap holds, sentinel and all: of an element's array field, the
// sentinel's first, and of a union's payload bound inside such a range.
const arenaHeapRangeMain = `type B struct {
    l   list[int64]
    arr [4]int64
}

type U union {
    arr [4]int64
    l   list[int64]
}

func first(l list[arena[B]], v []int64) []int64 {
    if len(l) > 0 {
        for _, x := range l[0] {
            return x.arr
        }
    }
    return v
}

func firstU(l list[arena[U]], v []int64) []int64 {
    if len(l) > 0 {
        for _, u := range l[0] {
            switch u {
            case arr(x):
                return x
            default:
            }
        }
    }
    return v
}

func main() {
    mut l := make(list[arena[B]], c_allocator)
    e := append(mut l, make(arena[B], B{l: numbers(2), arr: [4]int64{1, 2, 3, 4}}, c_allocator))
    mut lu := make(list[arena[U]], c_allocator)
    eu := append(mut lu, make(arena[U], U.arr([4]int64{5, 6, 7, 8}), c_allocator))
    w := [2]int64{9, 9}
    check(e.code == 0 && total(first(l, w[:])) == 10, "a view of a range binding over an arena in a list")
    check(eu.code == 0 && total(firstU(lu, w[:])) == 26, "a view of a union binding over an arena in a list")
}
`

// TestArenaHeapRangeViews builds arenaHeapRangeMain under each of
// sanitizerFlags, whose address sanitizer stops a view into a stack copy
// that ended, and checks every check it prints passed.
func TestArenaHeapRangeViews(t *testing.T) {
	src := arenaHooksPrelude + arenaHeapRangeMain
	for _, flags := range sanitizerFlags {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, arenaHooksC, flags...), 2)
		})
	}
}
