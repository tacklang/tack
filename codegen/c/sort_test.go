package c_test

import (
	"fmt"
	"math/rand/v2"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// sortProgram is a program that sorts the bytes of each of its arguments,
// as int64 values, with std/sort's by, ascending and then descending, and
// writes each order on a line, then whether is_sorted accepts it, what
// sortWant gives for the same arguments.
const sortProgram = `package main

import fmt from "std/fmt"
import os from "std/os"
import sort from "std/sort"

func less(a int64, b int64) bool {
    return a < b
}

func greater(a int64, b int64) bool {
    return a > b
}

func show(l []int64, ok bool) {
    for _, x := range l {
        fmt.printf("%d ", x)
    }
    fmt.printf("%t\n", ok)
}

func one(b []uint8) {
    mut l := make(list[int64], c_allocator)
    for _, c := range b {
        e := append(mut l, c)
        if e.code != 0 {
            print("out of memory\n")
            return
        }
    }
    sort.by(mut l, less)
    show(l, sort.is_sorted(l, less))
    sort.by(mut l, greater)
    show(l, sort.is_sorted(l, greater))
}

func main() {
    args, e := os.args(c_allocator)
    if e.code != 0 {
        print("args failed\n")
        return
    }
    for i, a := range args {
        if i > 0 {
            one(a.bytes())
        }
    }
}
`

// sortWant returns what sortProgram writes for args, from Go's slices
// package: each argument's bytes sorted ascending, then descending.
func sortWant(args []string) string {
	var w strings.Builder
	line := func(v []int64) {
		for _, x := range v {
			fmt.Fprintf(&w, "%d ", x)
		}
		w.WriteString("true\n")
	}
	for _, a := range args {
		v := make([]int64, len(a))
		for i := range len(a) {
			v[i] = int64(a[i])
		}
		slices.Sort(v)
		line(v)
		slices.Reverse(v)
		line(v)
	}
	return w.String()
}

// runSort runs bin, a build of sortProgram, on args and checks it writes
// what Go's slices package gives, line by line.
func runSort(t *testing.T, bin string, args []string) {
	t.Helper()
	out, err := exec.Command(bin, args...).Output()
	if err != nil {
		t.Fatalf("sort program failed: %v\n%s", err, out)
	}
	got, want := strings.Split(string(out), "\n"), strings.Split(sortWant(args), "\n")
	if len(got) != len(want) {
		t.Fatalf("wrote %d lines, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("argument %d: got %.80q, want %.80q", i/2, got[i], want[i])
		}
	}
}

// randomSortInputs returns n arguments from a fixed seed, so a failure
// repeats, of every length from 0 to 40, around the insertion sort's
// threshold of 12, and some of a few hundred bytes, drawn from a small
// alphabet so values repeat, or from every byte but 0.
func randomSortInputs(n int) []string {
	r := rand.New(rand.NewPCG(7, 8))
	inputs := make([]string, n)
	for i := range inputs {
		size := i % 41
		if i%10 == 9 {
			size = 100 + r.IntN(400)
		}
		b := make([]byte, size)
		for j := range b {
			if i%2 == 0 {
				b[j] = "abc"[r.IntN(3)]
			} else {
				b[j] = byte(1 + r.IntN(255))
			}
		}
		inputs[i] = string(b)
	}
	return inputs
}

// TestSortMatchesGo builds sortProgram and checks that std/sort orders
// 400 random byte strings, ascending and descending, exactly as Go's
// slices.Sort does, and that is_sorted accepts each result. The values
// are integers, so equal ones cannot be told apart, and an unstable sort
// must give the same sequence as any other.
func TestSortMatchesGo(t *testing.T) {
	bin := buildWithStd(t, sortProgram)
	runSort(t, bin, randomSortInputs(400))
}

// FuzzSortMatchesGo builds sortProgram once and checks, for each input
// the fuzzer makes, that std/sort orders its bytes as Go's slices.Sort
// does. An input holding a 0 byte cannot be a program's argument, so it
// is skipped.
func FuzzSortMatchesGo(f *testing.F) {
	for _, s := range []string{"", "a", "ba", "cba", "zyxwvutsrqponm", "aaaaaaaaaaaaaaaa", "\xff\x01\x80"} {
		f.Add(s)
	}
	bin := buildWithStd(f, sortProgram)
	f.Fuzz(func(t *testing.T, s string) {
		if strings.IndexByte(s, 0) >= 0 {
			t.Skip("a program's argument cannot hold a 0 byte")
		}
		runSort(t, bin, []string{s})
	})
}

// TestSortExampleIsSanitizerClean builds examples/sort, its C file and
// std's C with clang's warnings as errors and the address and undefined
// behavior sanitizers on, every finding fatal, and checks it writes
// exactly its output, every check ok: std/sort reads no element out of
// range, under less functions that order nothing too, and swaps owned
// elements without copying or freeing them, which the example's count of
// live buffers checks.
func TestSortExampleIsSanitizerClean(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "sort")
	src := readFile(t, filepath.Join(dir, "main.tck"))
	csrc := readFile(t, filepath.Join(dir, "live.c"))
	want := readFile(t, filepath.Join(dir, "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/sort/output expects a check to fail")
	}
	atO0Too(t, []string{"-O1", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"}, func(t *testing.T, flags []string) {
		bin := buildWithStdAndC(
			t,
			src,
			csrc,
			flags...,
		)
		var stderr strings.Builder
		cmd := exec.Command(bin)
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("sort failed: %v\n%s\nstderr:\n%s", err, out, stderr.String())
		}
		if string(out) != want {
			t.Errorf("sort wrote\n%q\nwant\n%q", out, want)
		}
	})
}
