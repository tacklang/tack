package c_test

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// blankParamsChecks is how many checks examples/blank-params prints.
const blankParamsChecks = 18

// TestBlankParamsExampleIsSanitizerClean builds examples/blank-params,
// its C file and std's C at -O2, and with the address and undefined
// behavior sanitizers at -O0 and -O1, every finding fatal, and checks
// every check it prints passed: arguments to _ parameters evaluated in
// order, several _ parameters in one function and an extern func's, _
// receivers, own _ parameters freeing a list, a Text and a struct
// holding both, a mut _ parameter beside another mut one, _ in closures
// and function values, a scope _ parameter, and a fields loop resetting
// each field, a nested struct's included, through a generic function
// that makes a value of the type its _ parameter infers, leaving
// c_allocator holding nothing.
func TestBlankParamsExampleIsSanitizerClean(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	prog := compileExampleDir(t, "blank-params")
	for _, flags := range [][]string{
		{"-O2"},
		{"-O0", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
		{"-O1", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
	} {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildProgram(t, prog, "", flags...), blankParamsChecks)
		})
	}
}
