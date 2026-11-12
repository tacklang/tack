package c_test

import (
	"path/filepath"
	"testing"
)

// poolsWant is how many checks examples/pools prints, live.c's check at
// process end included.
const poolsWant = 39

// TestPoolsExampleIsSanitizerClean builds examples/pools under each of
// sanitizerFlags and checks every check it prints passed: insert gives
// handles, has proves them, a proven handle reads and writes its element
// in place, remove frees an owned element at once, which c_allocator's
// count of live buffers shows, a stale, foreign, zero or reused-slot
// handle fails has, the checked form and both remove forms, a failed
// insert leaves the pool as it was, and drop, a ?, a generic function's
// instances, a struct, a list, a map keyed by handles, closures and exit
// free every element and every pool buffer exactly once, which the
// address sanitizer, with its leak checker, and the undefined behavior
// sanitizer check.
func TestPoolsExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "pools", "live.c")
	for _, flags := range sanitizerFlags {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, csrc, flags...), poolsWant)
		})
	}
}

// poolHooksC is the C half of the programs below: c_allocator's count of
// live buffers, and the runtime's testing hooks for a pool's 32-bit
// limits, which let a test reach each limit's behavior without 2^32
// removals, makes or slots: tack_pool_first_gen_, the generation every
// slot a pool grows from then on starts at, tack_pool_next_tag_, the tag
// the next pool made takes, and tack_pool_limit_, the most slots a pool
// may hold.
const poolHooksC = `#include <tack/tack.h>

#include "tack.h"

int64_t pools_live(void) {
    return tack_c_allocator_live_();
}

void pools_first_gen(int64_t n) {
    tack_pool_first_gen_(n);
}

void pools_next_tag(int64_t n) {
    tack_pool_next_tag_(n);
}

void pools_limit(int64_t n) {
    tack_pool_limit_(n);
}
`

// poolHooksPrelude declares poolHooksC's functions, check, and a list
// maker, for the programs of the tests below.
const poolHooksPrelude = `package main

extern("pools_live") func live() int64
extern("pools_first_gen") func first_gen(n int64)
extern("pools_next_tag") func next_tag(n int64)
extern("pools_limit") func limit(n int64)

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

`

// poolHooksFlags builds the programs below with the address, leak and
// undefined behavior sanitizers, a stack use after return included, every
// finding fatal; runPoolHooks builds each at -O0 too.
var poolHooksFlags = []string{
	"-O2", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all",
}

// runPoolHooks builds main, a program's functions after poolHooksPrelude,
// with poolHooksC under poolHooksFlags, at -O0 and at -O2, and checks each
// build prints want checks, every one ok.
func runPoolHooks(t *testing.T, main string, want int) {
	t.Helper()
	atO0Too(t, poolHooksFlags, func(t *testing.T, flags []string) {
		runChecks(t, buildWithStdAndC(t, poolHooksPrelude+main, poolHooksC, flags...), want)
	})
}

// generationWrapMain drives one slot to its last generation, 2^32 - 1,
// starting it two short of it with first_gen: the first insert and
// remove leave slot 0 at the last generation, the second insert issues
// it, and its remove, where the increment would wrap to 0, a generation
// never issued here but one any handle of a slot started at 0 holds,
// must retire the slot, checked before the increment. With the pool
// limited to that one slot, the next insert then has no slot to take and
// must fail, out of memory, freeing its value, rather than issue the
// slot again; with the limit lifted it grows a new slot. No stale handle
// matches at any point, and removing one changes nothing.
const generationWrapMain = `func main() {
    first_gen(4294967294)
    limit(1)
    mut p := make(pool[list[int64]], c_allocator)
    first, err := insert(mut p, numbers(1))
    if err.code != 0 {
        check(false, "first insert")
        return
    }
    remove(mut p, first)
    last, e2 := insert(mut p, numbers(2))
    if e2.code != 0 {
        check(false, "insert at the last generation")
        return
    }
    check(has(p, last) && !has(p, first) && last != first && len(p) == 1,
        "the last generation is issued and live")
    remove(mut p, last)
    check(!has(p, last) && !has(p, first) && len(p) == 0, "removing the last generation leaves no handle live")
    base := live()
    _, e3 := insert(mut p, numbers(3))
    check(e3.code == 1 && len(p) == 0 && live() == base,
        "a retired slot is never reused: the insert fails and frees its value")
    limit(-1)
    after, e4 := insert(mut p, numbers(4))
    if e4.code != 0 {
        check(false, "insert after the retired slot")
        return
    }
    v, found := remove(mut p, first)
    if found && len(v) >= 0 {
        check(false, "the first handle removes nothing")
    }
    check(has(p, after) && after != first && after != last && len(p) == 1,
        "insert after a retired slot grows a new one")
    remove(mut p, last)
    check(has(p, after) && len(p) == 1, "the stale handles remove nothing")
    mut n := 0
    for h, l := range p {
        if h == after {
            n +%= len(l)
        }
    }
    check(n == 4, "a range skips the retired slot")
}
`

// TestPoolGenerationRetiresOnWrap checks hard rule 3's guard for pools:
// a slot whose 32-bit generation would wrap is retired, never reused, so
// a handle issued 2^32 removes ago never matches again (see
// generationWrapMain).
func TestPoolGenerationRetiresOnWrap(t *testing.T) {
	runPoolHooks(t, generationWrapMain, 6)
}

// tagWrapMain makes p, whose tag is the counter's first, 1, then moves
// the counter two short of its last value, 2^32 - 1, with next_tag, and
// makes three more pools, the last two after the counter has reached its
// last value, where a wrap would come. Every make must succeed and give a
// pool that works, none of the three may accept p's handle, nor p theirs,
// and the two made at the last value share it, so each accepts the
// other's handle: tags are retired, never reused once passed, and the
// last one is never left. A handle one of those gives for a slot the
// other does not have still finds nothing there: the slot's index is
// checked against the pool's slots, so it never reads past them, which
// the address sanitizer would report.
const tagWrapMain = `// apart inserts into a fresh pool and reports whether it works and stays
// apart from p, whose live handle is hp.
func apart(p pool[int64], hp handle[int64]) bool {
    mut r := make(pool[int64], c_allocator)
    hr, err := insert(mut r, 2)
    if err.code != 0 {
        return false
    }
    return has(r, hr) && !has(p, hr) && !has(r, hp) && has(p, hp)
}

func main() {
    mut p := make(pool[int64], c_allocator)
    hp, err := insert(mut p, 1)
    if err.code != 0 {
        check(false, "first insert")
        return
    }
    next_tag(4294967294)
    check(apart(p, hp), "the pool before the counter's last value stays apart")
    check(apart(p, hp), "the first pool at the counter's last value stays apart")
    check(apart(p, hp), "the second pool at the counter's last value stays apart")
    mut a := make(pool[int64], c_allocator)
    mut b := make(pool[int64], c_allocator)
    ha, ea := insert(mut a, 3)
    hb, eb := insert(mut b, 4)
    if ea.code != 0 || eb.code != 0 {
        check(false, "inserts at the last tag")
        return
    }
    check(has(a, ha) && has(b, hb) && !has(a, hp) && !has(p, ha), "pools at the last tag work")
    check(has(a, hb) && has(b, ha), "pools at the last tag share it")
    for i := range 8 {
        _, e := insert(mut a, i)
        if e.code != 0 {
            check(false, "more inserts at the last tag")
            return
        }
    }
    far, e2 := insert(mut a, 6)
    if e2.code != 0 {
        check(false, "the last insert at the last tag")
        return
    }
    v, found := b[far]
    remove(mut b, far)
    w, taken := remove(mut b, far)
    check(!has(b, far) && !found && v == 0 && !taken && w == 0 && len(b) == 1 && has(b, hb),
        "a handle past another pool's slots, at a shared tag, reads none of them")
}
`

// TestPoolTagWrapKeepsMake checks that when the 32-bit pool tag counter
// would wrap, make(pool[T], ...) still succeeds and gives a pool that
// works (see tagWrapMain). A pool made after the counter's last value
// also rejects the handles of every pool made before it, but shares the
// last value with every other pool made after it.
func TestPoolTagWrapKeepsMake(t *testing.T) {
	runPoolHooks(t, tagWrapMain, 6)
}

// poolLimitMain checks a pool at its limit, which limit lowers from 2^32
// - 1 slots: inserts up to it succeed, one past it fails with code 1,
// frees the value it was given and leaves the pool as it was; a removed
// slot is taken again at the limit, since reuse needs no new slot; and
// the limit lifted lets the pool grow again.
const poolLimitMain = `func main() {
    limit(2)
    mut p := make(pool[list[int64]], c_allocator)
    a, e1 := insert(mut p, numbers(1))
    if e1.code != 0 {
        check(false, "first insert")
        return
    }
    b, e2 := insert(mut p, numbers(2))
    if e2.code != 0 {
        check(false, "second insert")
        return
    }
    check(has(p, a) && has(p, b) && len(p) == 2, "inserts up to the limit succeed")
    base := live()
    _, e3 := insert(mut p, numbers(3))
    check(e3.code == 1 && len(p) == 2 && has(p, a) && has(p, b) && live() == base,
        "an insert past the limit fails, frees its value and leaves the pool as it was")
    remove(mut p, a)
    c, e4 := insert(mut p, numbers(4))
    check(e4.code == 0 && len(p) == 2 && !has(p, a), "a freed slot is reused at the limit")
    if e4.code == 0 {
        check(has(p, c) && c != a, "the reused slot gives a new handle")
    }
    limit(-1)
    d, e5 := insert(mut p, numbers(5))
    check(e5.code == 0 && len(p) == 3, "the limit lifted lets the pool grow")
    if e5.code == 0 {
        check(has(p, d), "the new slot's handle is live")
    }
}
`

// TestPoolLimit checks what reaching the most slots a pool may hold does
// (see poolLimitMain).
func TestPoolLimit(t *testing.T) {
	runPoolHooks(t, poolLimitMain, 6)
}

// handleCompareMain compares handles every way a value of a comparable
// type is compared: == and != on handles, a switch on one, lists, arrays,
// structs and unions holding them, and each of those as a map's key, so
// every equality and hash helper that reaches a handle compares its
// three parts, and none compares the struct C cannot.
const handleCompareMain = `type S struct {
    h handle[int64]
    n int64
}

type U union {
    some handle[int64]
    none
}

func main() {
    mut p := make(pool[int64], c_allocator)
    h, err := insert(mut p, 1)
    if err.code != 0 {
        check(false, "insert")
        return
    }
    mut z handle[int64]
    mut y := z
    mut hit := 0
    switch h {
    case z:
        hit = 1
    case y:
        hit = 2
    default:
        hit = 3
    }
    y = h
    switch y {
    case h:
        hit +%= 10
    }
    check(hit == 13, "a switch on a handle")
    check(y == h && h != z && !(h == z), "handles compare")
    mut a := make(list[handle[int64]], c_allocator)
    mut b := make(list[handle[int64]], c_allocator)
    if append(mut a, h, z).code != 0 || append(mut b, h, z).code != 0 {
        check(false, "append")
        return
    }
    arr := [2]handle[int64]{h, z}
    arr2 := [2]handle[int64]{h, h}
    s := S{h: h, n: 1}
    t := S{h: z, n: 1}
    u := U.some(h)
    v := U.some(z)
    check(a == b && arr != arr2 && s != t && s == s && u != v && u == U.some(h),
        "lists, arrays, structs and unions of handles compare")
    mut ms := make(map[S]int64, c_allocator)
    mut mu := make(map[U]int64, c_allocator)
    mut ma := make(map[[2]handle[int64]]int64, c_allocator)
    mut mh := make(map[handle[int64]]int64, c_allocator)
    if put(mut ms, s, 1).code != 0 || put(mut mu, u, 2).code != 0 || put(mut ma, arr, 3).code != 0 ||
        put(mut mh, h, 4).code != 0 {
        check(false, "put")
        return
    }
    check(has(ms, s) && !has(ms, t) && has(mu, u) && !has(mu, v) && has(ma, arr) && !has(ma, arr2) &&
        has(mh, h) && !has(mh, z), "handles in map keys")
}
`

// TestHandlesCompare checks that handles, which lower to a struct C has
// no == for, compare by tack_handle_eq_ and hash by their three parts
// wherever a comparable value is compared or hashed (see
// handleCompareMain).
func TestHandlesCompare(t *testing.T) {
	runPoolHooks(t, handleCompareMain, 4)
}

// linkedMain keeps a linked list in a pool, each node holding the handle
// of the next, a type holding a handle of itself, and an owned list of
// its own: it links three nodes, unlinks and removes the middle one, so
// its handle goes stale while the others stay live, replaces the head's
// list in place, and walks what is left, every buffer freed exactly once.
const linkedMain = `type Node struct {
    next handle[Node]
    items list[int64]
}

func walk(p pool[Node], start handle[Node]) int64 {
    mut n := 0
    mut at := start
    for range 10 {
        h := at
        if !has(p, h) {
            return n
        }
        n +%= len(p[h].items)
        at = p[h].next
    }
    return n
}

func main() {
    base := live()
    mut p := make(pool[Node], c_allocator)
    mut none handle[Node]
    c, e1 := insert(mut p, Node{next: none, items: numbers(3)})
    if e1.code != 0 {
        check(false, "insert c")
        return
    }
    b, e2 := insert(mut p, Node{next: c, items: numbers(2)})
    if e2.code != 0 {
        check(false, "insert b")
        return
    }
    a, e3 := insert(mut p, Node{next: b, items: numbers(1)})
    if e3.code != 0 {
        check(false, "insert a")
        return
    }
    check(walk(p, a) == 6, "three linked nodes")
    if has(p, a) {
        p[a].next = c
    }
    remove(mut p, b)
    check(walk(p, a) == 4 && !has(p, b) && len(p) == 2, "the middle node unlinked and removed")
    if has(p, a) {
        old := replace(mut p[a].items, numbers(5))
        check(len(old) == 1, "replace takes a node's list out")
    }
    check(walk(p, a) == 8, "the head's list replaced in place")
    drop(own p)
    check(live() == base, "every buffer freed")
}
`

// TestPoolLinkedNodes checks the pattern pools are for: elements naming
// each other by handle, one removed from the middle (see linkedMain).
func TestPoolLinkedNodes(t *testing.T) {
	runPoolHooks(t, linkedMain, 5)
}
