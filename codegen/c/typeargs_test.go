package c_test

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// typeArgsChecks is how many checks examples/type-args prints.
const typeArgsChecks = 33

// TestTypeArgsExampleIsSanitizerClean builds examples/type-args, its
// package lists, its C file and std's C at -O2, and with the address and
// undefined behavior sanitizers at -O0 and -O1, every finding fatal, and
// checks every check it prints passed: empty values of basic types, a
// struct, a Text, a list, a map and a list of lists, each given only by a
// written type argument; std/sort's sort.by[int64]; lists.new[int64]
// and lists.filled[User], whose error is passed on with ? and made to
// fail at every allocation in turn;
// partial type argument lists, the rest inferred, an interface
// parameter's among them; an untyped constant and an int32 passed for a
// written int32 and int64; written types an interface constrains, a
// struct holding a list moved in by an own parameter and a union; a
// written type switched on, recursively through a list's element; a
// type parameter passed on as a type argument;
// a type parameter only a fields loop names; empty[T] in a fields loop's
// body; written type arguments in a closure and a defer; closures passed
// for a function type the type arguments settle, wholly or in part; and
// a list and a struct made through a region, leaving c_allocator
// holding nothing.
func TestTypeArgsExampleIsSanitizerClean(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	prog := compileExampleDir(t, "type-args")
	for _, flags := range [][]string{
		{"-O2"},
		{"-O0", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
		{"-O1", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
	} {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildProgram(t, prog, "", flags...), typeArgsChecks)
		})
	}
}
