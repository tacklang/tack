package c_test

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tacklang/tack/internal/stdtest"
)

// Zero values at the size limit. clang rejects a {0} initializer for an
// array whose element count is a multiple of 2^32, which is why the
// backend writes every zero value as {}. No program the checker accepts
// has such an array any more: a value holds at most 2^31 bytes, and every
// element holds at least one, since even a [0]T or an empty struct lowers
// to something of a byte or more, so an array has fewer than 2^32
// elements. These tests zero the largest values the checker accepts
// instead, so the zero value written for every kind of variable is one
// clang takes at the limit: globals of exactly 2^30 bytes, all a
// program's globals may hold, and locals of exactly 2^31, the most a
// value may.

// limitGlobals are programs that each zero one global of exactly 2^30
// bytes, all a program's globals may hold: an array of bytes, an array of
// arrays, and a struct, padded to the limit. Each lies
// in static storage, which the OS gives out a page at a time, and its
// program touches only its first and last pages.
var limitGlobals = []struct{ name, decl, use string }{
	{
		"an array of bytes",
		"mut g [1073741824]uint8\n",
		"    g[1073741823] = 1\n    check(g[0] == 0 && g[1073741823] == 1 && len(g) == 1073741824)\n",
	},
	{
		"an array of arrays",
		"mut g [2][536870912]uint8\n",
		"    g[1][536870911] = 1\n    check(g[0][0] == 0 && g[1][536870911] == 1)\n",
	},
	{
		"a struct",
		"type S struct {\n    n    int8\n    rest [134217727]int64\n}\n\nmut g S\n",
		"    g.rest[134217726] = 1\n    check(g.n == 0 && g.rest[134217726] == 1)\n",
	},
}

// TestLimitGlobalsZeroed builds each of limitGlobals with clang's warnings
// as errors, at no optimization and at -O2, and checks it prints ok: a
// global as large as a program's globals may be is zeroed with an
// initializer clang takes, and links at either level.
func TestLimitGlobalsZeroed(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	for _, tc := range limitGlobals {
		src := "package main\n\n" + tc.decl + "\nfunc check(cond bool) {\n" +
			"    if cond {\n        print(\"ok\\n\")\n    } else {\n        print(\"FAIL\\n\")\n    }\n}\n\n" +
			"func main() {\n" + tc.use + "}\n"
		for _, opt := range []string{"-O0", "-O2"} {
			t.Run(tc.name+"/"+opt, func(t *testing.T) {
				out, err := exec.Command(buildWithStd(t, src, opt)).Output()
				if err != nil || string(out) != "ok\n" {
					t.Errorf("printed %q, err %v; want ok", out, err)
				}
			})
		}
	}
}

// limitLocals zeroes values of exactly 2^31 bytes in each place the
// backend writes a zero value for a local: a variable with no value, a
// struct holding an array, a single result and several results, whose
// variables start zeroed. Its frames are gigabytes, so it cannot run: it
// is only compiled.
const limitLocals = `package main

type S struct {
    n    int8
    rest [268435455]int64
}

func zero_s() S {
    mut s S
    return s
}

func one() [2147483648]uint8 {
    mut r [2147483648]uint8
    return r
}

func two() ([2][1073741824]uint8, int64) {
    mut r [2][1073741824]uint8
    return r, 7
}

func main() {
    mut l [2147483648]uint8
    s := zero_s()
    r := one()
    p, n := two()
    if len(l) + int64(s.n) + len(r) + len(p) +% n == 0 {
        print("never\n")
    }
}
`

// TestLimitLocalsZeroed checks the C of limitLocals passes clang's syntax
// and semantic checks with its warnings as errors: each zero value it
// writes for a local is an initializer clang takes at the limit. Only
// the frame size, which clang checks when it generates code, is not
// looked at.
func TestLimitLocalsZeroed(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	src := filepath.Join(t.TempDir(), "program.c")
	writeFile(t, src, generate(t, limitLocals))
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-fsyntax-only",
		"-I", stdtest.IncludeDir(), src,
	}
	if out, err := exec.Command("clang", args...).CombinedOutput(); err != nil {
		t.Fatalf("clang: %v\n%s", err, out)
	}
}
