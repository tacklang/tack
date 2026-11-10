package c_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// questionFormsWant is how many checks examples/question-forms prints.
const questionFormsWant = 15

// sanitizerFlags are the builds the ? examples run under: optimized, and
// with the address and undefined behavior sanitizers at two levels, every
// finding fatal.
var sanitizerFlags = [][]string{
	{"-O2"},
	{"-O0", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"},
	{"-O1", "-fsanitize=address,undefined", "-fno-sanitize-recover=all"},
}

// TestQuestionExampleIsSanitizerClean builds examples/question, its C
// file and std's C under each of sanitizerFlags, and checks it writes
// exactly its output, every check ok: a ? that passes gives the call's
// other results and one that fails returns the call's own error, from
// functions with one, two and three results and an error alone, through
// chains and recursion; the defers reached before a failing ? run, last
// first, and those after it do not; every list, struct holding a list
// and own parameter the function holds when its ? returns is freed, a
// list returned beside the failing error included, which c_allocator's
// count of live buffers checks; and a ? in arguments, operands, &&,
// conditions and loops runs left to right and makes no call after it.
func TestQuestionExampleIsSanitizerClean(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "question")
	src := readFile(t, filepath.Join(dir, "main.tck"))
	csrc := readFile(t, filepath.Join(dir, "live.c"))
	want := readFile(t, filepath.Join(dir, "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/question/output expects a check to fail")
	}
	for _, flags := range sanitizerFlags {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			bin := buildWithStdAndC(t, src, csrc, flags...)
			out, err := exec.Command(bin).Output()
			if err != nil {
				t.Fatalf("question failed: %v\n%s", err, out)
			}
			if string(out) != want {
				t.Errorf("question wrote\n%s\nwant\n%s", out, want)
			}
		})
	}
}

// TestQuestionFormsExampleIsSanitizerClean builds
// examples/question-forms, whose forms of ? each encode an assumption
// its ruling did not settle, under each of sanitizerFlags, and checks
// every check it prints passed.
func TestQuestionFormsExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "question-forms", "live.c")
	for _, flags := range sanitizerFlags {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, csrc, flags...), questionFormsWant)
		})
	}
}
