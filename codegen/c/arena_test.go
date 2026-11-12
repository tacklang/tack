package c_test

import (
	"path/filepath"
	"testing"
)

// arenasWant is how many checks examples/arenas prints.
const arenasWant = 51

// TestArenasExampleIsSanitizerClean builds examples/arenas, its C file
// and std's C under each of sanitizerFlags, at -O2 and with the address,
// undefined behavior and leak sanitizers at -O0 and -O1, every finding
// fatal, and checks every check it prints passed: make with a sentinel
// allocating nothing; add giving ids and add_all spans; reads and writes
// through ids with no proof; the zero id, ids of another arena in range
// and past the end, before the first add and after it, reading and
// writing only the slot the design names; spans empty, past the end,
// partly past it and ending at the last id; reserve; adds refused by
// c_allocator leaving the arena as it was; an arena of lists, its
// sentinel owned too; arenas in structs, passed as mut and own, in
// generic code, under closures, as map keys, and through ? and defers,
// with c_allocator's count of live buffers back to zero after each group.
func TestArenasExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "arenas", "live.c")
	for _, flags := range sanitizerFlags {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, csrc, flags...), arenasWant)
		})
	}
}

// arenaClampPrelude is the start of every program of
// TestArenaClampNeverReadsOutOfBounds: check, a list maker, a struct
// holding an array and a list, and an arena of each element type the
// programs use, holding n ids, its sentinel's and n - 1 more, which the
// program uses on smaller arenas of that type.
const arenaClampPrelude = `package main

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

func source(n int64) arena[int64] {
    mut b := make(arena[int64], 0, c_allocator)
    for i := range n -% 1 {
        _, err := add(mut b, i)
        if err.code != 0 {
            return b
        }
    }
    return b
}

type Wide struct {
    a [4]int64
    l list[int64]
}

func arrays(n int64) arena[[4]int64] {
    mut b := make(arena[[4]int64], [4]int64{0, 0, 0, 0}, c_allocator)
    for range n -% 1 {
        _, err := add(mut b, [4]int64{0, 0, 0, 0})
        if err.code != 0 {
            return b
        }
    }
    return b
}

func wides(n int64) arena[Wide] {
    mut b := make(arena[Wide], Wide{a: [4]int64{0, 0, 0, 0}, l: numbers(0)}, c_allocator)
    for range n -% 1 {
        _, err := add(mut b, Wide{a: [4]int64{0, 0, 0, 0}, l: numbers(0)})
        if err.code != 0 {
            return b
        }
    }
    return b
}

func lists(n int64) arena[list[int64]] {
    mut b := make(arena[list[int64]], numbers(0), c_allocator)
    for range n -% 1 {
        _, err := add(mut b, numbers(0))
        if err.code != 0 {
            return b
        }
    }
    return b
}

`

// arenaClampCases are the programs of TestArenaClampNeverReadsOutOfBounds:
// each a name, the body of main, after arenaClampPrelude, and how many
// checks it prints.
var arenaClampCases = []struct {
	name, body string
	want       int
}{
	{"sentinel-only int64", `
    mut a := make(arena[int64], 5, c_allocator)
    b := source(1000)
    mut z id[int64]
    mut sum int64 = 0
    for i := range b {
        sum = sum +% a[i]
        a[i] = a[i] +% 1
    }
    check(sum == 504500 && a[z] == 1005 && len(a) == 1,
        "every id of a bigger arena clamps to the inline sentinel")
`, 1},
	{"sentinel-only array", `
    mut a := make(arena[[4]int64], [4]int64{1, 2, 3, 4}, c_allocator)
    b := arrays(64)
    mut sum int64 = 0
    for i := range b {
        sum = sum +% a[i][3]
        a[i][0] = 9
    }
    mut z id[[4]int64]
    check(sum == 256 && a[z][0] == 9 && a[z][3] == 4, "an array sentinel is read and written in place")
`, 1},
	{"sentinel-only struct", `
    mut a := make(arena[Wide], Wide{a: [4]int64{1, 2, 3, 4}, l: numbers(2)}, c_allocator)
    b := wides(64)
    mut n int64 = 0
    for i := range b {
        n = n +% len(a[i].l) +% a[i].a[2]
        err := append(mut a[i].l, 7)
        if err.code != 0 {
            n = -1000
        }
    }
    mut z id[Wide]
    check(n > 0 && len(a[z].l) == 66, "a struct sentinel's fields are read and grown in place")
`, 1},
	{"sentinel-only lists", `
    mut a := make(arena[list[int64]], numbers(3), c_allocator)
    b := lists(16)
    mut n int64 = 0
    for i := range b {
        n = n +% len(a[i])
        a[i] = numbers(4)
    }
    mut z id[list[int64]]
    check(n == 63 && len(a[z]) == 4 && len(a) == 1, "a list sentinel is replaced through every id")
`, 1},
	{"grown arena", `
    mut a := make(arena[int64], -1, c_allocator)
    b := source(4096)
    mut z id[int64]
    mut bad int64 = 0
    for k := range 300 {
        e, err := add(mut a, k)
        if err.code != 0 {
            bad = bad +% 1
        } else if a[e] != k {
            bad = bad +% 1
        }
        mut pos int64 = 0
        for i := range b {
            mut want int64 = -1
            if pos > 0 && pos < len(a) {
                want = pos -% 1
            }
            if a[i] != want {
                bad = bad +% 1
            }
            pos = pos +% 1
        }
        if a[z] != -1 {
            bad = bad +% 1
        }
    }
    check(bad == 0 && len(a) == 301, "ids far past a grown arena read the sentinel after every add")
`, 1},
}

// TestArenaClampNeverReadsOutOfBounds builds programs that index arenas
// with ids no add of theirs gave, under the sanitizers at -O0 and -O1,
// and checks every read and write lands on the sentinel: a sentinel-only
// arena of int64, of an array, of a struct holding an array and a list,
// and of lists, read, written, field-accessed and grown through the zero
// id and through every id of a bigger arena, before its first add; and
// an arena grown past several buffers, indexed with ids far past its end
// after each add. The address sanitizer stops any read past the inline
// sentinel or the buffer; a sentinel-only arena has no buffer to read.
func TestArenaClampNeverReadsOutOfBounds(t *testing.T) {
	for _, tc := range arenaClampCases {
		t.Run(tc.name, func(t *testing.T) {
			src := arenaClampPrelude + "func main() {" + tc.body + "}\n"
			for _, flags := range sanitizerFlags[1:] {
				t.Run(filepath.Join(flags...), func(t *testing.T) {
					runChecks(t, buildWithStdAndC(t, src, "", flags...), tc.want)
				})
			}
		})
	}
}

// TestArenaFreedAtExit builds programs that call exit with arenas live,
// under the sanitizers with examples/exit-frees/live.c, whose destructor
// checks nothing is live as the process ends: an arena of lists grown
// past its first buffer, its sentinel owned; an arena held in a struct;
// a sentinel-only arena whose owned sentinel is its only buffer; an
// arena whose first add c_allocator refused; and an arena with a buffer
// while c_allocator refuses every request.
func TestArenaFreedAtExit(t *testing.T) {
	csrc := readFile(t, filepath.Join("..", "..", "examples", "exit-frees", "live.c"))
	const end = "nothing live at process end ok\n"
	for _, tc := range []struct {
		name, main string
		code       int
	}{
		{"an arena of lists", "    mut a := make(arena[list[int64]], numbers(2), c_allocator)\n" +
			"    for i := range 40 {\n        _, err := add(mut a, numbers(i))\n" +
			"        if err.code != 0 {\n            exit(1)\n        }\n    }\n" +
			"    if len(a) == 41 && live() > 40 {\n        exit(3)\n    }\n", 3},
		{"an arena in a struct", "    mut h := Holder{a: make(arena[int64], 0, c_allocator)}\n" +
			"    for i := range 20 {\n        _, err := add(mut h.a, i)\n" +
			"        if err.code != 0 {\n            exit(1)\n        }\n    }\n" +
			"    if len(h.a) == 21 {\n        exit(4)\n    }\n", 4},
		{"a sentinel-only arena", "    a := make(arena[list[int64]], numbers(5), c_allocator)\n" +
			"    if len(a) == 1 && live() == 1 {\n        exit(5)\n    }\n", 5},
		{"an arena whose first add was refused", "    mut a := make(arena[list[int64]], numbers(1), c_allocator)\n" +
			"    l := numbers(4)\n    fail_after(0)\n    _, err := add(mut a, l)\n" +
			"    if err.code == 1 && len(a) == 1 && live() == 1 {\n        exit(6)\n    }\n", 6},
		{"exit while refusing every request", "    mut a := make(arena[list[int64]], numbers(1), c_allocator)\n" +
			"    _, err := add(mut a, numbers(3))\n    fail_after(0)\n" +
			"    if err.code == 0 && live() == 3 {\n        exit(7)\n    }\n", 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := exitPathPrelude + "type Holder struct {\n    a arena[int64]\n}\n\n" +
				"func main() {\n" + tc.main + "}\n"
			out, code := runStatus(t, buildWithStdAndC(t, src, csrc, sanitizers...))
			if out != end || code != tc.code {
				t.Errorf("wrote %q and exited %d, want %q and %d", out, code, end, tc.code)
			}
		})
	}
}
