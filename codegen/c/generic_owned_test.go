package c_test

import (
	"path/filepath"
	"testing"
)

// TestGenericsOwnedExampleIsSanitizerClean builds examples/generics-owned,
// its C file and std's C at -O2, and with the address and undefined
// behavior sanitizers at -O0 and -O1, every finding fatal, and checks
// every check it prints passed: generic functions over lists, views and
// maps of a type parameter, instantiated with int64, lists, Texts and
// structs holding function values, swapping, sorting, moving and taking
// owned elements, and moving them between lists with every allocation
// refused in turn, with c_allocator holding nothing after each group.
func TestGenericsOwnedExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "generics-owned", "live.c")
	for _, flags := range [][]string{
		{"-O2"},
		{"-O0", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"},
		{"-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"},
	} {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, csrc, flags...), 11)
		})
	}
}

// ownedShapes uses a type parameter in the shapes the example does not:
// a map of T values read under has, a T dropped by deferred code, a T
// among several results, a type parameter passed on whole to another
// generic function, and type arguments that are error, a function type,
// an Allocator and a map.
const ownedShapes = `package main

type Any interface {
}

type Sizer interface {
    size() int64
}

type Box struct {
    items list[int64]
}

func (b Box) size() int64 {
    return len(b.items)
}

func check(cond bool, name string) {
    print(name)
    if cond {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}

func sized[T Sizer](m map[string]T, k string) int64 {
    if has(m, k) {
        return m[k].size()
    }
    return -1
}

func drain[T Any](mut l list[T]) {
    defer {
        pop(mut l)
    }
    if len(l) > 0 {
        remove(mut l, 0)
    }
}

func split[T Any](mut l list[T], mk func() T) (T, int64) {
    if len(l) > 0 {
        return pop(mut l), len(l)
    }
    return mk(), 0
}

func inner[U Any](mut l list[U]) int64 {
    last := len(l) - 1
    if len(l) > 1 && 0 < last && last < len(l) {
        swap(mut l, 0, last)
    }
    return len(l)
}

func outer[T Any](mut l list[T]) int64 {
    return inner(mut l)
}

func one[E Any](e E) int64 {
    return 1
}

func twice(x int64) int64 {
    return x *% 2
}

func box(n int64) Box {
    mut l := make(list[int64], c_allocator)
    for i := range n {
        err := append(mut l, i)
        if err.code != 0 {
            return Box{items: l}
        }
    }
    return Box{items: l}
}

func empty() Box {
    return box(0)
}

func main() {
    mut m := make(map[string]Box, c_allocator)
    e1 := put(mut m, "a", box(3))
    check(e1.code == 0 && sized(m, "a") == 3 && sized(m, "b") == -1, "a map of T under has")
    mut l := make(list[Box], c_allocator)
    e2 := append(mut l, box(1), box(2), box(3))
    drain(mut l)
    check(e2.code == 0 && len(l) == 1, "deferred code dropping a T")
    b, n := split(mut l, empty)
    c, k := split(mut l, empty)
    check(b.size() == 2 && n == 0 && c.size() == 0 && k == 0, "a T among several results")
    e3 := append(mut l, box(4), box(5))
    check(e3.code == 0 && outer(mut l) == 2 && len(l) == 2 && l[0].size() == 5, "a type parameter passed on")
    mut ms := make(list[map[string]int64], c_allocator)
    e4 := append(mut ms, make(map[string]int64, c_allocator))
    check(e4.code == 0 && one(noerror) +% one(twice) +% one(c_allocator) +% inner(mut ms) == 4,
        "type arguments that are no struct")
}
`

// TestOwnedTypeParamShapes builds ownedShapes with the address and
// undefined behavior sanitizers and checks all its checks pass.
func TestOwnedTypeParamShapes(t *testing.T) {
	runChecks(t, buildWithStdAndC(t, ownedShapes, "", "-O1", "-fsanitize=address,undefined",
		"-fno-sanitize-recover=all"), 5)
}
