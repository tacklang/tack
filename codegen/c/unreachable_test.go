package c_test

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"github.com/tacklang/tack/internal/stdtest"
)

// unreachableAbortTest calls TACK_UNREACHABLE, as the generated C of an
// unreachable statement does, while c_allocator holds a list's buffer.
// A handler for SIGABRT reports how many buffers are still held when the
// abort comes, then lets the abort end the process as it would have.
const unreachableAbortTest = `#include <signal.h>
#include <stdio.h>
#include <tack/tack.h>

static void report(int sig) {
    printf("live %lld\n", (long long)tack_c_allocator_live_());
    fflush(stdout);
    signal(sig, SIG_DFL);
    raise(sig);
}

int main(void) {
    int64_t cap = 0;
    void *p = tack_list_grow_(0, 0, 4, sizeof(int64_t), _Alignof(int64_t), TACK_C_ALLOCATOR, &cap);
    printf("held %lld %d\n", (long long)tack_c_allocator_live_(), p != 0);
    fflush(stdout);
    signal(SIGABRT, report);
    TACK_UNREACHABLE("dir/main.tck:7");
}
`

// TestUnreachableAborts runs TACK_UNREACHABLE against the runtime's
// unreachable.c and list.c: it writes the ruled message, naming the file
// and line it was given, to stderr, has every allocator free what it holds
// before it aborts, as exit does, and ends the process by SIGABRT, status
// 134 to a shell. A function with a result may end in it, which -Werror
// with -Wreturn-type checks the _Noreturn of its prototype allows.
func TestUnreachableAborts(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("%v", err)
	}
	dir := t.TempDir()
	main := filepath.Join(dir, "main.c")
	writeFile(t, main, unreachableAbortTest+
		"\nint64_t never(void) {\n    TACK_UNREACHABLE(\"dir/main.tck:9\");\n}\n")
	bin := filepath.Join(dir, "abort")
	args := append(slices.Clone(strictCFlags), "-Wreturn-type",
		"-I", stdtest.IncludeDir(), "-o", bin, main,
		stdtest.RuntimeCFile("unreachable.c"), stdtest.RuntimeCFile("list.c"))
	if out, err := exec.Command("clang", args...).CombinedOutput(); err != nil {
		t.Fatalf("clang: %v\n%s", err, out)
	}
	cmd := exec.CommandContext(t.Context(), bin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("ran with %v, want an abort", err)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGABRT {
		t.Errorf("ended with %v, want SIGABRT, status 134", exitErr)
	}
	if want := "tack: unreachable reached at dir/main.tck:7 (compiler bug)\n"; stderr.String() != want {
		t.Errorf("stderr %q, want %q", stderr.String(), want)
	}
	if want := "held 1 1\nlive 0\n"; stdout.String() != want {
		t.Errorf("stdout %q, want %q: the abort did not free what the allocator held",
			stdout.String(), want)
	}
}
