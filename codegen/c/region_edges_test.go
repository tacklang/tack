package c_test

import "testing"

// regionEdgesMain checks regions in the places the lowering has to get
// right beyond examples/regions: a region named _, an empty one, one in a
// defer, one in a closure's body, one in a generic function and one in a
// method; a break out of a region in a switch's case in a loop, and out of
// two nested regions at once; a continue out of a region in a three-clause
// loop, whose post statement still runs; a return out of three nested
// regions in a loop; and a switch whose cases fall through inside a
// region; an arena and a pool of lists, made through a region; and a
// region's list copied into an outer one through a view parameter and a
// spread, which pass nothing the region holds on; and a scope function
// parameter called inside a region with values not from it. Each
// check also reads c_allocator's count of live buffers, which
// must be back where it was once the regions have ended.
const regionEdgesMain = `func check(cond bool, name string) {
    print(name)
    if cond {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}

type Summer interface {
}

func sumThrough[T Summer](x T, n int64) int64 {
    region r {
        l := fillIn(n, r)
        mut s int64 = 0
        for _, v := range l {
            s = s +% v
        }
        return s
    }
}

type Box struct {
    n int64
}

func (b Box) total() int64 {
    region r {
        l := fillIn(b.n, r)
        mut s int64 = 0
        for _, v := range l {
            s = s +% v
        }
        return s
    }
}

func each(n int64, scope f func(int64)) {
    for i := range n {
        f(i)
    }
}

func deferred(mut out int64) {
    defer {
        region r {
            l := fillIn(4, r)
            out = len(l)
        }
    }
    out = -1
}

func copyInto(mut out list[int64], v []int64) error {
    return append(mut out, v...)
}

func viaScope(scope g func(int64, own list[int64])) int64 {
    mut n int64 = 0
    region r {
        l := fillIn(3, r)
        g(len(l), fillIn(2, c_allocator))
        n = len(l)
    }
    return n
}

func deepReturn(n int64) int64 {
    for i := range n {
        region a {
            x := fillIn(1, a)
            region b {
                y := fillIn(2, b)
                region c {
                    z := fillIn(3, c)
                    if i == 2 {
                        return len(x) +% len(y) +% len(z) +% i
                    }
                }
            }
        }
    }
    return -1
}

func main() {
    region w {
        l := fillIn(1, w)
        check(len(l) == 1, "a first region")
    }
    base := live()

    mut blank int64 = 0
    region _ {
        blank = 7
    }
    region _ {
    }
    check(blank == 7 && live() == base, "a region named _ and an empty one")

    mut got int64 = 0
    deferred(mut got)
    check(got == 4 && live() == base, "a region in a defer")

    mut inClosure int64 = 0
    each(3, func(i int64) {
        region r {
            l := fillIn(i +% 1, r)
            inClosure = inClosure +% len(l)
        }
    })
    check(inClosure == 6 && live() == base, "a region in a closure's body")

    check(sumThrough(Box{n: 0}, 10) == 55 && Box{n: 4}.total() == 10 && live() == base,
        "a region in a generic function and in a method")

    mut passes int64 = 0
    for i := range 10 {
        switch i {
        case 3:
            region r {
                l := fillIn(5, r)
                if len(l) == 5 {
                    break
                }
            }
        default:
            passes = passes +% 1
        }
        if i == 3 {
            break
        }
    }
    check(passes == 3 && live() == base, "a break out of a region in a switch in a loop")

    mut both int64 = 0
    for {
        region outer {
            a := fillIn(2, outer)
            region inner {
                b := fillIn(3, inner)
                both = len(a) +% len(b)
                break
            }
        }
    }
    check(both == 5 && live() == base, "a break out of two nested regions")

    mut posts int64 = 0
    mut kept int64 = 0
    for mut i int64 = 0; i < 6; i +%= 1 {
        posts = posts +% 1
        region r {
            l := fillIn(i, r)
            if i % 2 == 0 {
                continue
            }
            kept = kept +% len(l)
        }
    }
    check(posts == 6 && kept == 9 && live() == base, "a continue out of a region runs the loop's post")

    check(deepReturn(5) == 8 && live() == base, "a return out of three nested regions in a loop")

    mut arenaSum int64 = 0
    mut poolLen int64 = 0
    region r {
        mut a := make(arena[list[int64]], fillIn(1, r), r)
        for i := range 20 {
            _, ea := add(mut a, fillIn(i, r))
            if ea.code != 0 {
                arenaSum = -1000
            }
        }
        for _, v := range a {
            arenaSum = arenaSum +% len(v)
        }
        mut p := make(pool[list[int64]], r)
        for i := range 30 {
            h, ep := insert(mut p, fillIn(i, r))
            if ep.code == 0 && i % 3 == 0 {
                remove(mut p, h)
            }
        }
        poolLen = len(p)
    }
    check(arenaSum == 191 && poolLen == 20 && live() == base, "an arena and a pool of lists through a region")

    mut copied := make(list[int64], c_allocator)
    region r {
        l := fillIn(5, r)
        e1 := copyInto(mut copied, l)
        e2 := append(mut copied, l...)
        if e1.code != 0 || e2.code != 0 {
            copied = make(list[int64], c_allocator)
        }
    }
    check(len(copied) == 10 && copied[0] == 1 && live() == base +% 1,
        "a region's list copied out through a view and a spread")
    drop(own copied)

    mut seen int64 = 0
    mut keptOut := make(list[int64], c_allocator)
    n := viaScope(func(k int64, own x list[int64]) {
        seen = seen +% k
        keptOut = x
    })
    check(n == 3 && seen == 3 && len(keptOut) == 2 && live() == base +% 1,
        "a scope function parameter called in a region with values not from it")
    drop(own keptOut)

    mut fell int64 = 0
    region r {
        l := fillIn(2, r)
        switch len(l) {
        case 2:
            fell = fell +% 1
            fallthrough
        case 3:
            fell = fell +% 10
        default:
            fell = -1
        }
    }
    check(fell == 11 && live() == base, "a switch falling through inside a region")
}
`

// TestRegionEdgesAreSanitizerClean builds regionEdgesMain with
// regionHooksC under the sanitizers at -O0 and -O1, the leak sanitizer
// among them, and checks every check it prints passed, the destructor's
// that nothing is live at the end among them.
func TestRegionEdgesAreSanitizerClean(t *testing.T) {
	src := regionHooksPrelude + regionEdgesMain
	atO0Too(t, sanitizers, func(t *testing.T, flags []string) {
		runRegionChecks(t, buildWithStdAndC(t, src, regionHooksC, flags...), 14)
	})
}

// regionDepthC is the C half of TestRegionDepthsSaturate: regionHooksC
// and a window on tack_region_limit_.
const regionDepthC = regionHooksC + `
void region_limit(int64_t n) {
    tack_region_limit_(n);
}
`

// regionDepthMain opens regions 8 deep with the deepest index lowered to
// 5, as if 65534 had been reached: the five outermost work, the three
// past them open with the dead handle, their allocations failing with
// code 1, and their ends only lower the depth, so a region opened at
// depth 4 again afterwards works, and so does one at depth 5 once the
// limit is lifted. Nothing is live once they have all ended.
const regionDepthMain = `extern("region_limit") func limit(n int64)

func check(cond bool, name string) {
    print(name)
    if cond {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}

func down(depth int64, mut worked int64, mut dead int64, mut other int64) {
    if depth == 8 {
        return
    }
    region r {
        mut l := make(list[int64], r)
        e := append(mut l, depth)
        if e.code == 0 && depth < 5 {
            worked = worked +% 1
        } else if e.code == 1 && depth >= 5 {
            dead = dead +% 1
        } else {
            other = other +% 1
        }
        down(depth +% 1, mut worked, mut dead, mut other)
    }
}

func at(depth int64, target int64) int64 {
    region r {
        if depth < target {
            return at(depth +% 1, target)
        }
        mut l := make(list[int64], r)
        e := append(mut l, 1)
        return int64(e.code)
    }
}

func main() {
    region w {
        l := fillIn(1, w)
        check(len(l) == 1, "a region before the limit works")
    }
    base := live()
    limit(5)
    mut worked int64 = 0
    mut dead int64 = 0
    mut other int64 = 0
    down(0, mut worked, mut dead, mut other)
    check(worked == 5 && dead == 3 && other == 0 && live() == base,
        "regions past the deepest index open dead and end cleanly")
    check(at(0, 4) == 0 && at(0, 5) == 1, "a region below the deepest index works after dead ones ended")
    limit(-1)
    check(at(0, 5) == 0 && live() == base, "a region at the old limit works once it is lifted")
}
`

// TestRegionDepthsSaturate builds regionDepthMain under the sanitizers at
// -O0 and -O1 and checks every check it prints passed, the destructor's
// among them: a region opened as deep as its 16-bit index cannot reach
// gets the dead handle, without breaking the regions around it.
func TestRegionDepthsSaturate(t *testing.T) {
	src := regionHooksPrelude + regionDepthMain
	atO0Too(t, sanitizers, func(t *testing.T, flags []string) {
		runRegionChecks(t, buildWithStdAndC(t, src, regionDepthC, flags...), 5)
	})
}

// regionKindsMain checks arenas, pools, maps, lists of lists, ids and
// handles made through a region while it is live, and std's functions
// that take a scope Allocator, os.read_file, os.list_dir,
// os.make_temp_dir, path.join, errors.from_bytes and text, given a
// region's allocator, each with c_allocator's count of live buffers read
// inside the region and after it, and again with c_allocator refusing a
// request at every step.
const regionKindsMain = `import os from "std/os"
import path from "std/path"
import text from "std/text"
import errors from "std/errors"

extern("region_live") func live() int64
extern("region_fail_after") func fail_after(n int64)

func check(cond bool, name string) {
    print(name)
    if cond {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}

func kinds(mut inside int64) bool {
    mut ok := true
    region r {
        mut a := make(arena[int64], -1, r)
        mut p := make(pool[int64], r)
        mut m := make(map[int64]int64, r)
        mut l := make(list[list[int64]], r)
        mut ids := make(list[id[int64]], r)
        mut hs := make(list[handle[int64]], r)
        for i := range 20000 {
            x, e1 := add(mut a, i)
            if e1.code != 0 {
                return false
            }
            e1b := append(mut ids, x)
            h, e2 := insert(mut p, i +% 1)
            if e2.code != 0 {
                return false
            }
            e2b := append(mut hs, h)
            e3 := put(mut m, i, i +% 2)
            mut inner := make(list[int64], r)
            e4 := append(mut inner, i, i)
            e5 := append(mut l, inner)
            if e1b.code != 0 || e2b.code != 0 || e3.code != 0 || e4.code != 0 || e5.code != 0 {
                return false
            }
            if i % 3 == 0 {
                remove(mut p, h)
            }
        }
        s, e6 := add_all(mut a, 1, 2, 3)
        if e6.code != 0 {
            return false
        }
        if len(a[s]) != 3 {
            ok = false
        }
        for i, x := range ids {
            if a[x] != i {
                ok = false
            }
        }
        for i, h := range hs {
            v, found := p[h]
            if i % 3 == 0 {
                if found {
                    ok = false
                }
            } else if !found || v != i +% 1 {
                ok = false
            }
        }
        for i := range 20000 {
            v, found := m[i]
            if !found || v != i +% 2 {
                ok = false
            }
        }
        for _, inner := range l {
            if len(inner) != 2 {
                ok = false
            }
        }
        // A list dropped while the region is live, and the count of
        // c_allocator's buffers there: the region's chunks.
        inside = live()
        mut d := make(list[int64], r)
        e7 := append(mut d, 1, 2, 3)
        if e7.code != 0 {
            ok = false
        }
        drop(own d)
    }
    return ok
}

func stdThrough(mut inside int64) bool {
    mut ok := true
    region r {
        j, e := path.join([]uint8("a/b"), []uint8("../c"), r)
        if e.code != 0 {
            return false
        }
        if j.len() != 3 {
            ok = false
        }
        f := errors.from_bytes(error{code: 3, msg: "x"}, []uint8("detail"), r)
        if f.err.code != 3 {
            ok = false
        }
        mut t := text.new(r)
        for i := range 70000 {
            e3 := mut t.append([]uint8("abc"))
            if e3.code != 0 {
                return false
            }
            if i < 0 {
                ok = false
            }
        }
        if t.len() != 210000 {
            ok = false
        }
        dir, e4 := os.make_temp_dir(r)
        if e4.code != 0 {
            return false
        }
        if dir.len() == 0 {
            ok = false
        }
        fp, ej := path.join(dir.bytes(), []uint8("f"), r)
        if ej.code != 0 {
            e9 := os.remove_path(dir.bytes())
            if e9.code != 0 {
                print("")
            }
            return false
        }
        ef := os.write_file(fp.bytes(), []uint8("hello"))
        names, e5 := os.list_dir(dir.bytes(), r)
        if e5.code == 0 && len(names) != 1 {
            ok = false
        }
        data, e6 := os.read_file(fp.bytes(), r)
        if e6.code == 0 && len(data) != 5 {
            ok = false
        }
        inside = live()
        er1 := os.remove_path(fp.bytes())
        er := os.remove_path(dir.bytes())
        if er.code != 0 || ef.code != 0 || er1.code != 0 {
            ok = false
        }
    }
    return ok
}

func main() {
    region w {
        k := make(list[int64], w)
        check(len(k) == 0, "a first region")
    }
    base := live()
    mut inside int64 = 0
    check(kinds(mut inside) && inside > base, "arenas, pools, maps and lists of lists through a live region")
    check(live() == base, "what kinds made through a region is freed at its end")
    inside = 0
    check(stdThrough(mut inside) && inside > base,
        "os.read_file, os.list_dir, os.make_temp_dir, path.join, errors.from_bytes and text through a region")
    check(live() == base, "what std made through a region is freed at its end")
    fail_after(3)
    check(!kinds(mut inside), "kinds fail when c_allocator refuses a chunk")
    fail_after(-1)
    check(live() == base, "kinds refused a chunk free what they took")
    for n := range 40 {
        fail_after(n)
        x := stdThrough(mut inside)
        if x {
            print("")
        }
        fail_after(-1)
    }
    check(live() == base, "std refused at every step frees what it took")
}
`

// TestRegionKindsAndStdAreSanitizerClean builds regionKindsMain with
// regionHooksC under the sanitizers at -O0 and -O1 and checks every check
// it prints passed, the destructor's among them.
func TestRegionKindsAndStdAreSanitizerClean(t *testing.T) {
	src := regionKindsPrelude + regionKindsMain
	atO0Too(t, sanitizers, func(t *testing.T, flags []string) {
		runRegionChecks(t, buildWithStdAndC(t, src, regionHooksC, flags...), 9)
	})
}

// regionKindsPrelude is the package clause of regionKindsMain, whose
// imports must come before regionHooksPrelude's declarations, so it
// declares the hooks itself.
const regionKindsPrelude = `package main

`
