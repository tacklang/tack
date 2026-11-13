package c_test

import (
	"os/exec"
	"testing"
)

// rangeAcrossInstances ranges over a list of a type parameter's elements
// from one generic function, first for elements that own a buffer, bound
// in place, then for elements that own none, bound by value.
const rangeAcrossInstances = `package main

type Plain struct {
    n int64
}

type Owns struct {
    l list[int64]
}

type Any0 interface {
}

func count[T Any0](xs list[T]) int64 {
    mut n := 0
    for _, x := range xs {
        n +%= touch(x)
    }
    return n
}

func touch[T Any0](x T) int64 {
    return one(x)
}

func one[T Any0](x T) int64 {
    return 1
}

func main() {
    mut a := make(list[Owns], c_allocator)
    e1 := append(mut a, Owns{l: make(list[int64], c_allocator)})
    mut b := make(list[Plain], c_allocator)
    e2 := append(mut b, Plain{n: 1})
    if e1.code == 0 && e2.code == 0 && count(a) == 1 && count(b) == 1 {
        print("ok\n")
    }
}
`

// TestRangeBindsAfreshPerInstance checks a range loop in a generic
// function binds its element in place, or by value, as each instance's
// element type says, whichever instance is lowered first: an instance
// whose elements own nothing reads its element by value even after one
// whose elements own a buffer read it through a pointer.
func TestRangeBindsAfreshPerInstance(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	bin := buildWithStd(t, rangeAcrossInstances, "-O0", "-fsanitize=address,undefined",
		useAfterReturn, "-fno-sanitize-recover=all")
	got, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, got)
	}
	if string(got) != "ok\n" {
		t.Errorf("printed %q, want %q", got, "ok\n")
	}
}
