package c_test

import (
	"strings"
	"testing"
)

// ignoredParams ignores parameters every way Tack allows: a plain
// function's, a mut one's, a method's receiver and parameter, a generic
// function's value of its type parameter, a callback's, passed as a
// function value, one beside one read only in deferred code, an array parameter
// read only through len, a constant, and a function of several results
// named as a value, whose thunk passes every parameter on.
const ignoredParams = `package main

type Any interface {
}

type Box struct {
    n int64
}

func check(cond bool, name string) {
    print(name)
    if cond {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}

func plain(a int64, s string, b bool) int64 {
    return 1
}

func muts(mut x int64, mut l list[int64]) int64 {
    return 2
}

func (b Box) size(scale int64) int64 {
    return 3
}

func (mut b Box) reset(to int64) {
}

func one[T Any](x T, l list[T]) int64 {
    return 4
}

func always(x int64) bool {
    return true
}

func pick(f func(int64) bool, x int64) bool {
    return f(0)
}

func later(x int64, y int64) int64 {
    mut n := 0
    defer {
        n = x
    }
    return n + 5
}

func width(a [3]int64) int64 {
    return len(a)
}

func pair(a int64, b int64) (int64, int64) {
    return 6, 7
}

func main() {
    mut x := 1
    mut l := make(list[int64], c_allocator)
    mut b := Box{n: 1}
    mut b.reset(9)
    p := pair
    q, r := p(1, 2)
    boxes := make(list[Box], c_allocator)
    check(plain(1, "s", true) == 1 && muts(mut x, mut l) == 2 && b.size(2) == 3, "plain, mut and method")
    check(one(b, boxes) == 4 && pick(always, 1) && later(1, 2) == 5, "generic, callback and defer")
    check(width([3]int64{1, 2, 3}) == 3 && q == 6 && r == 7, "len of an array and a thunk")
}
`

// TestIgnoredParamsCompileStrictly builds ignoredParams, whose functions
// ignore their parameters every way Tack allows, with every warning an
// error, at -O0 and -O2, and checks it runs: no ignored parameter makes C
// warn.
func TestIgnoredParamsCompileStrictly(t *testing.T) {
	for _, opt := range []string{"-O0", "-O2"} {
		t.Run(opt, func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, ignoredParams, "", opt), 3)
		})
	}
}

// TestUsedParamsHaveNoCast checks that a parameter the lowered body names
// gets no (void) cast, and one it never names gets exactly one.
func TestUsedParamsHaveNoCast(t *testing.T) {
	got := generate(t, "package main\n\nfunc f(a int64, b int64) int64 {\n    return a\n}\n\n"+
		"func main() {\n    code, _ := int32(f(1, 2))\n    exit(code)\n}\n")
	if strings.Contains(got, "(void)p_a;") || strings.Count(got, "(void)p_b;") != 1 {
		t.Errorf("want one (void) cast, of b alone:\n%s", got)
	}
}
