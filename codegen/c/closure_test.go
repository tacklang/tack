package c_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestClosuresExampleIsSanitizerClean builds examples/closures, its C
// file and std's C at -O2, and with the address and undefined behavior
// sanitizers at -O0 and -O1, every finding fatal, and checks it writes
// exactly its output, every check ok: closures passed to scope
// parameters of functions, generic functions, methods and std/sort's by
// read and write their captures by reference, nested four deep and
// passed on through scope parameters, return from themselves alone, and
// a closure reading a captured list copies and frees nothing, while
// lists a closure makes or replaces are freed exactly once, with every
// allocation refused in turn, and c_allocator holds nothing after each
// group.
func TestClosuresExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "closures", "live.c")
	want := readFile(t, filepath.Join("..", "..", "examples", "closures", "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/closures/output expects a check to fail")
	}
	for _, flags := range [][]string{
		{"-O2"},
		{"-O0", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
		{"-O1", "-fsanitize=address,undefined", useAfterReturn, "-fno-sanitize-recover=all"},
	} {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			out, code := runStatus(t, buildWithStdAndC(t, src, csrc, flags...))
			if out != want || code != 0 {
				t.Errorf("wrote\n%s\nand exited %d, want\n%s\nand 0", out, code, want)
			}
		})
	}
}
