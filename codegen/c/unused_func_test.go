package c_test

import "testing"

// everyStd imports every package of std and calls one function of each,
// so most of std's functions, every one of them a static C function, go
// uncalled, as do a function of the program's own that only calls itself,
// a generic function nothing calls, and a map type's helpers no code
// calls.
const everyStd = `package main

import bytes from "std/bytes"
import conv from "std/conv"
import fmt from "std/fmt"
import os from "std/os"
import path from "std/path"
import text from "std/text"

type Any interface {
}

func check(cond bool, name string) {
    print(name)
    if cond {
        print(" ok\n")
    } else {
        print(" FAIL\n")
    }
}

func never(x int64) int64 {
    if x <= 0 {
        return 0
    }
    return never(x - 1)
}

func alone[T Any](l list[T]) int64 {
    return len(l)
}

func main() {
    fmt.println("std")
    n, err := conv.parse_int([]uint8("42"), 10, 64)
    args, aerr := os.args(c_allocator)
    t := text.new(c_allocator)
    m := make(map[string]int64, c_allocator)
    check(bytes.equal([]uint8("a"), []uint8("a")) && err.code == 0 && n == 42, "bytes and conv")
    check(aerr.code == 0 && len(args) > 0 && path.is_abs([]uint8("/a")) && t.len() == 0 && len(m) == 0,
        "os, path and text")
}
`

// TestUncalledFunctionsCompileStrictly builds everyStd with every warning
// an error, at -O0 and -O2, and checks it runs: no static function the
// program leaves uncalled, of std or its own, makes C warn.
func TestUncalledFunctionsCompileStrictly(t *testing.T) {
	for _, opt := range []string{"-O0", "-O2"} {
		t.Run(opt, func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, everyStd, "", opt), 2)
		})
	}
}
