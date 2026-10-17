package c_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestOsExampleIsSanitizerClean builds examples/os, its C file and std's
// C, std/os's os.c among them, with clang's warnings as errors and the
// address and undefined behavior sanitizers on, every finding fatal, and
// runs it as tack/main_test.go's TestBuiltOsPassesEveryCheck does, with
// the arguments one, "" and "two words", TACK_OS_EXAMPLE set and at most
// 64 open files, checking it writes exactly its output, every check ok: os.c reads no buffer past
// its end, and std/os leaks no list, which the example's own count of
// live buffers checks, through every failure it forces too. TMPDIR is a
// directory of the test's, which must be empty afterwards.
func TestOsExampleIsSanitizerClean(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "os")
	src := readFile(t, filepath.Join(dir, "main.tck"))
	csrc := readFile(t, filepath.Join(dir, "live.c"))
	want := readFile(t, filepath.Join(dir, "output"))
	if strings.Contains(want, "FAIL") {
		t.Fatal("examples/os/output expects a check to fail")
	}
	bin := buildWithStdAndC(
		t,
		src,
		csrc,
		"-O1",
		"-fsanitize=address,undefined",
		"-fno-sanitize-recover=all",
	)
	tmp := t.TempDir()
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TACK_OS_EXAMPLE") && !strings.HasPrefix(kv, "TMPDIR=") {
			env = append(env, kv)
		}
	}
	var stderr strings.Builder
	cmd := exec.Command("sh", "-c", `ulimit -n 64 && exec "$0" "$@"`, bin, "one", "", "two words")
	env = append(env, "TACK_OS_EXAMPLE=set by the test", "TACK_OS_EXAMPLE_EMPTY=", "TMPDIR="+tmp)
	cmd.Env = env
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("os failed: %v\n%s\nstderr:\n%s", err, out, stderr.String())
	}
	if string(out) != want {
		t.Errorf("os wrote\n%q\nwant\n%q", out, want)
	}
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Errorf("left %d entries in TMPDIR, the first %s", len(left), left[0].Name())
	}
}
