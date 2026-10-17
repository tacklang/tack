package c_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestFuncValuesExampleIsSanitizerClean builds examples/func-values, its
// C file and std's C at -O2, and with the address and undefined behavior
// sanitizers at -O0 and -O1, every finding fatal, and checks every check
// it prints passed: functions passed, returned, stored in a struct, an
// array, a list, a map and a union, called through values with mut
// arguments, views, owned arguments and results, several results and
// errors, a struct holding a function of itself, from other packages, generic functions and deferred code, and
// appended to a list refused by the allocator at every allocation, with
// c_allocator holding nothing after each group.
func TestFuncValuesExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "func-values", "live.c")
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

// valueFirst calls through a function value with an argument that
// assigns the variable holding it, through mut, and with arguments that
// print, so the order shows: the call reads the value first, and calls
// the function it held then, and the arguments run left to right.
const valueFirst = `package main

func check(cond bool, name string) {
    print(name)
    if cond {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}

func double(x int64) int64 {
    return x * 2
}

func square(x int64) int64 {
    return x * x
}

func reassign(mut g func(int64) int64) int64 {
    g = square
    return 3
}

mut order int64

func mark(d int64) int64 {
    order = order * 10 + d
    return d
}

func add(a int64, b int64) int64 {
    return a + b
}

func main() {
    mut f := double
    first := f(reassign(mut f))
    second := f(3)
    check(first == 6 && second == 9, "the value read before its arguments")
    g := add
    check(g(mark(1), mark(2)) == 3 && order == 12, "arguments left to right")
}
`

// TestFuncValueReadBeforeArguments runs valueFirst, with the undefined
// behavior sanitizer, and checks both its checks pass.
func TestFuncValueReadBeforeArguments(t *testing.T) {
	out, status := runSanitized(t, valueFirst)
	if status != 0 || strings.Count(out, " ok\n") != 2 || strings.Contains(out, "FAIL") {
		t.Errorf("exit %d, printed\n%s\nwant 2 checks, every one ok", status, out)
	}
}
