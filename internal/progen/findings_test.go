package progen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tacklang/tack/internal/stdtest"
)

// The findings the generator has made, each minimized to a program that
// shows it and kept as a test that fails while the finding stands, so it
// is skipped; each says what is wrong. Remove the skip once the compiler
// is fixed, and the test stays, so the finding does not come back.

// strictC compiles src through the front end and compiles its C with
// every warning an error, without linking, and returns clang's complaint,
// or the front end's, empty when there is none.
func strictC(t *testing.T, src string) string {
	t.Helper()
	skipWithoutClang(t)
	_, csrc, _, rejected := frontEnd(src)
	if rejected != "" {
		return rejected
	}
	path := filepath.Join(t.TempDir(), "out.c")
	if err := os.WriteFile(path, csrc, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("clang", "-std=c11", "-Wall", "-Wextra", "-Werror", "-fsyntax-only",
		"-I", stdtest.IncludeDir(), path).CombinedOutput()
	if err != nil {
		return string(out)
	}
	return ""
}

// TestFindingUnusedMapFreeWarns, fixed: a map type named only in a
// function nothing calls gets a free function, tack_free_N, that is never
// called, and was not marked unused, so -Wall reported it. The same held
// for an arithmetic helper only a std function nothing calls uses,
// tack_div_int64 in a program calling sort.is_sorted alone, which
// TestFindingUnusedStdConstWarns checks.
func TestFindingUnusedMapFreeWarns(t *testing.T) {
	src := "package main\n\nfunc f(m map[int64]int64) int64 {\n    return len(m)\n}\n\n" +
		"func main() {\n}\n"
	if msg := strictC(t, src); msg != "" {
		t.Error(msg)
	}
}

// TestFindingSelfAssignWarns: x = x, which the compiler accepts, lowers
// to a C self-assignment, v_x = v_x, which -Wall reports, for a variable
// of any type.
func TestFindingSelfAssignWarns(t *testing.T) {
	t.Skip("finding: x = x lowers to a C self-assignment, -Wself-assign")
	src := "package main\n\nfunc main() {\n    mut x := 1\n    x = x\n    if x != 1 {\n" +
		"        exit(1)\n    }\n}\n"
	if msg := strictC(t, src); msg != "" {
		t.Error(msg)
	}
}

// TestFindingSelfCompareWarns: a comparison of an integer variable with
// itself, x < x, which the compiler accepts, lowers to a C
// self-comparison, which -Wall reports as always false or true.
func TestFindingSelfCompareWarns(t *testing.T) {
	t.Skip("finding: x < x lowers to a C self-comparison, -Wtautological-compare")
	src := "package main\n\nfunc f(x int64) bool {\n    return x < x\n}\n\n" +
		"func main() {\n    if f(1) {\n        exit(1)\n    }\n}\n"
	if msg := strictC(t, src); msg != "" {
		t.Error(msg)
	}
}

// TestFindingEqualLiteralWithCallsDoesNotCompile, fixed: a comparison of
// a struct with a struct literal holding two calls lowered to C that took
// the address of a comma expression, &(o0 = f(1), o1 = f(2), (S){...}),
// which C rejects: the compiler accepted the program and the build failed
// in clang.
func TestFindingEqualLiteralWithCallsDoesNotCompile(t *testing.T) {
	src := "package main\n\ntype S struct {\n    a int64\n    b int64\n}\n\n" +
		"func f(x int64) int64 {\n    return x\n}\n\nfunc main() {\n    p := S{a: 1, b: 2}\n" +
		"    if p == (S{a: f(1), b: f(2)}) {\n        exit(1)\n    }\n}\n"
	if msg := strictC(t, src); msg != "" {
		t.Error(msg)
	}
}

// TestFindingOverlapCompareWarns: a condition that can never hold, as
// x < 3 && x > 5, which the compiler accepts, is written as is in C,
// which -Wall reports.
func TestFindingOverlapCompareWarns(t *testing.T) {
	t.Skip("finding: x < 3 && x > 5 is written as is in C, -Wtautological-overlap-compare")
	src := "package main\n\nfunc f(x int64) bool {\n    return x < 3 && x > 5\n}\n\n" +
		"func main() {\n    if f(1) {\n        exit(1)\n    }\n}\n"
	if msg := strictC(t, src); msg != "" {
		t.Error(msg)
	}
}

// TestFindingUnusedStdConstWarns, fixed: a program that imports std/sort
// and calls sort.is_sorted alone gets std/sort's global insertion_max,
// which only sort.by reads, as a static const nothing names, and
// tack_div_int64, which only sort.by calls, and -Wall reported both.
func TestFindingUnusedStdConstWarns(t *testing.T) {
	src := "package main\n\nimport sort from \"std/sort\"\n\n" +
		"func asc(a int64, b int64) bool {\n    return a < b\n}\n\nfunc main() {\n" +
		"    l := make(list[int64], c_allocator)\n    if !sort.is_sorted(l, asc) {\n" +
		"        exit(1)\n    }\n}\n"
	if msg := strictC(t, src); msg != "" {
		t.Error(msg)
	}
}
