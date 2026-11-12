package c_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMemberNamesExampleIsSanitizerClean builds examples/member-names,
// its package geo, its C file and std's C with clang's warnings as errors
// and the address and undefined behavior sanitizers on, every finding
// fatal, at -O0 and -O2, and checks it writes exactly its output, every
// check ok: fields and methods named like builtin types and builtin
// functions, of this package and another, read, written, set by key,
// held in an arena, a pool and lists, passed through generic functions
// and interfaces, and read from C under their own names, each one beside
// the builtin it is named like, with every buffer freed exactly once.
func TestMemberNamesExampleIsSanitizerClean(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	dir := filepath.Join("..", "..", "examples", "member-names")
	want := readFile(t, filepath.Join(dir, "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/member-names/output expects a check to fail")
	}
	prog := compileExampleDir(t, "member-names")
	for _, opt := range []string{"-O0", "-O2"} {
		t.Run(opt, func(t *testing.T) {
			bin := buildProgram(t, prog, "", opt, "-fsanitize=address,undefined", useAfterReturn,
				"-fno-sanitize-recover=all")
			out, err := exec.Command(bin).Output()
			if err != nil {
				t.Fatalf("member-names failed: %v\n%s", err, out)
			}
			if string(out) != want {
				t.Errorf("member-names wrote\n%s\nwant\n%s", out, want)
			}
		})
	}
}
