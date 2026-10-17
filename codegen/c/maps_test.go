package c_test

import (
	"path/filepath"
	"testing"
)

// TestMapsExampleIsSanitizerClean builds examples/maps, its C file and
// std's C with clang's warnings as errors, at -O2 and with the address and
// undefined behavior sanitizers on at -O0 and -O1, every finding fatal,
// and checks every check passes: puts, lookups, replaces that keep their
// position, deletes, checked and bare, every range form in insertion
// order after deletes and puts again, string and owned keys, list and
// struct values, nested maps, 100000 entries, a put refused by the
// allocator at every allocation leaving the map as it was, and
// c_allocator holding nothing after each group.
func TestMapsExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "maps", "live.c")
	for _, flags := range [][]string{
		{"-O2"},
		{"-O0", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"},
		{"-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"},
	} {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, csrc, flags...), 19)
		})
	}
}

// TestMapHasExampleIsSanitizerClean builds examples/map-has, its C file
// and std's C as TestMapsExampleIsSanitizerClean does, and checks every
// check passes: reads, assignments that free the value they replace, mut
// arguments, appends, replaces and mut methods on values under has,
// constant keys, early exits, a proof in every pass of a loop, maps in
// maps, in a field, a union's payload and a range's value, appends under
// has refused by the allocator at every allocation, and c_allocator
// holding nothing after each group.
func TestMapHasExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "map-has", "live.c")
	for _, flags := range [][]string{
		{"-O2"},
		{"-O0", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"},
		{"-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"},
	} {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, csrc, flags...), 16)
		})
	}
}

// compositeKeys keys maps by arrays, structs holding strings, unions with
// and without payloads, lists, and the integers at their edges, and looks
// each key up through a value built apart from the one put, so a hash
// that disagreed with == would miss it.
const compositeKeys = `package main

func check(cond bool, name string) {
    print(name)
    if cond {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}

type Pair struct {
    name string
    n int64
}

type Shape union {
    circle int64
    square Pair
    dot
}

func digits(n int64) list[int64] {
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
    mut arrays := make(map[[2]int64]int64, c_allocator)
    e1 := put(mut arrays, [2]int64{1, 2}, 12)
    v1, ok1 := arrays[[2]int64{1, 2}]
    _, no1 := arrays[[2]int64{2, 1}]
    check(e1.code == 0 && ok1 && v1 == 12 && !no1, "array keys")
    mut pairs := make(map[Pair]int64, c_allocator)
    e2 := put(mut pairs, Pair{name: "ab", n: 1}, 7)
    s := "a"
    v2, ok2 := pairs[Pair{name: "ab", n: 1}]
    _, no2 := pairs[Pair{name: s, n: 1}]
    check(e2.code == 0 && ok2 && v2 == 7 && !no2, "struct keys")
    mut shapes := make(map[Shape]int64, c_allocator)
    e3 := put(mut shapes, Shape.square(Pair{name: "x", n: 2}), 1)
    e4 := put(mut shapes, Shape.dot, 2)
    e5 := put(mut shapes, Shape.circle(2), 3)
    v3, ok3 := shapes[Shape.square(Pair{name: "x", n: 2})]
    v4, ok4 := shapes[Shape.dot]
    v5, ok5 := shapes[Shape.circle(2)]
    check(e3.code == 0 && e4.code == 0 && e5.code == 0 && ok3 && v3 == 1 && ok4 && v4 == 2 && ok5 && v5 == 3,
        "union keys")
    mut lists := make(map[list[int64]]int64, c_allocator)
    e6 := put(mut lists, digits(3), 3)
    e7 := put(mut lists, digits(4), 4)
    k := digits(3)
    v6, ok6 := lists[k]
    e8 := put(mut lists, digits(3), 33)
    v7, _ := lists[k]
    check(e6.code == 0 && e7.code == 0 && ok6 && v6 == 3 && e8.code == 0 && v7 == 33 && len(lists) == 2, "list keys")
    mut edges := make(map[int64]int64, c_allocator)
    e9 := put(mut edges, -9223372036854775808, 1)
    e10 := put(mut edges, 9223372036854775807, 2)
    e11 := put(mut edges, -1, 3)
    a, oka := edges[-9223372036854775808]
    b, okb := edges[-1]
    check(e9.code == 0 && e10.code == 0 && e11.code == 0 && oka && a == 1 && okb && b == 3, "int64 edges")
    mut bytes := make(map[uint8]bool, c_allocator)
    for i := range 256 {
        c, _ := uint8(i)
        err := put(mut bytes, c, i % 2 == 0)
        if err.code != 0 {
            return
        }
    }
    x, okx := bytes[255]
    check(len(bytes) == 256 && okx && !x, "every uint8 key")
}
`

// TestMapCompositeKeysFindEqualKeys builds compositeKeys at -O0 and -O1
// with the address and undefined behavior sanitizers on, every finding
// fatal, and checks every key is found through an equal one built apart.
func TestMapCompositeKeysFindEqualKeys(t *testing.T) {
	for _, level := range []string{"-O0", "-O1"} {
		t.Run(level, func(t *testing.T) {
			runChecks(
				t,
				buildWithStd(
					t,
					compositeKeys,
					level,
					"-fsanitize=address,undefined",
					"-fno-sanitize-recover=all",
				),
				6,
			)
		})
	}
}
