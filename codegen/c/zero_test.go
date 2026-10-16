package c_test

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tacklang/tack/internal/stdtest"
)

// hugeGlobals are programs that each zero one global array whose element
// count is a multiple of 2^32, the counts for which clang rejects a {0}
// initializer as one for an aggregate with no elements: an array of
// int64, a struct holding an array of empty structs, and an array of
// zero-length arrays. Each lies in static storage, which the OS gives out
// a page at a time, and its program touches only a few pages of it.
var hugeGlobals = []struct{ name, decl, use string }{
	{
		"an array of int64",
		"mut g [4294967296]int64\n",
		"    g[5] = 1\n    last, ok := g[4294967295]\n" +
			"    check(ok && last == 0 && g[5] == 1 && len(g) == 4294967296)\n",
	},
	{
		"a struct holding one",
		"type E struct {\n}\n\ntype S struct {\n    n    int64\n    none [4294967296]E\n}\n\nmut s S\n",
		"    s.n = 3\n    check(s.n == 3 && len(s.none) == 4294967296)\n",
	},
	{
		"an array of empty arrays",
		"mut z [8589934592][0]int64\n",
		"    check(len(z) == 8589934592)\n",
	},
}

// TestHugeGlobalArraysZeroed builds each of hugeGlobals with clang's
// warnings as errors, at -O2, and checks it prints ok: a global array of
// any element count is zeroed with an initializer clang takes. It is not
// built at -O0, where on arm64 a static variable placed more than 4 GiB
// past the code that reads it, as one after the huge array is, cannot be
// linked: a separate bug, of linking rather than of the initializer.
func TestHugeGlobalArraysZeroed(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	for _, tc := range hugeGlobals {
		src := "package main\n\n" + tc.decl + "\nfunc check(cond bool) {\n" +
			"    if cond {\n        print(\"ok\\n\")\n    } else {\n        print(\"FAIL\\n\")\n    }\n}\n\n" +
			"func main() {\n" + tc.use + "}\n"
		t.Run(tc.name, func(t *testing.T) {
			out, err := exec.Command(buildWithStd(t, src, "-O2")).Output()
			if err != nil || string(out) != "ok\n" {
				t.Errorf("printed %q, err %v; want ok", out, err)
			}
		})
	}
}

// hugeLocals zeroes arrays whose element counts are multiples of 2^32, a
// global and, in each place the backend writes a zero value for a local, a
// variable with no value, a struct holding one, a single result and
// several results, whose variables start zeroed. Even a [0]int64 or an
// empty struct takes a byte or more in C, so these frames are gigabytes
// and the program cannot run: it is only compiled.
const hugeLocals = `package main

type E struct {
}

type S struct {
    n    int64
    none [4294967296][0]int64
}

mut g [4294967296]int64

func zero_s() S {
    mut s S
    return s
}

func one() [4294967296]E {
    mut r [4294967296]E
    return r
}

func two() ([4294967296][0]int64, int64) {
    mut r [4294967296][0]int64
    return r, 7
}

func main() {
    mut l [8589934592][0]int64
    s := zero_s()
    r := one()
    p, n := two()
    if len(g) + len(l) + len(s.none) + len(r) + len(p) + n == 0 {
        print("never\n")
    }
}
`

// TestHugeLocalArraysZeroed checks the C of hugeLocals passes clang's
// syntax and semantic checks with its warnings as errors: each zero value
// it writes, for a global or a local, is an initializer clang takes for an
// array of any element count. Only the frame size, which clang checks when it
// generates code, is not looked at.
func TestHugeLocalArraysZeroed(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	src := filepath.Join(t.TempDir(), "program.c")
	writeFile(t, src, generate(t, hugeLocals))
	args := []string{
		"-std=c11", "-Wall", "-Wextra", "-Werror", "-fsyntax-only",
		"-I", stdtest.IncludeDir(), src,
	}
	if out, err := exec.Command("clang", args...).CombinedOutput(); err != nil {
		t.Fatalf("clang: %v\n%s", err, out)
	}
}
