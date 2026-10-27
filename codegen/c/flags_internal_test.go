package c

import (
	"slices"
	"testing"

	"github.com/tacklang/tack/codegen"
)

// TestClangFlagsKeepLoopsThatNeverEnd pins clangFlags, the flags every
// clang command of the backend starts with: warnings off,
// -fno-finite-loops, so clang marks no loop mustprogress and never
// deletes a loop that never ends (it marks no C function mustprogress,
// with the flag or without it), and the level.
func TestClangFlagsKeepLoopsThatNeverEnd(t *testing.T) {
	for _, opt := range codegen.Opts {
		want := []string{"-w", "-fno-finite-loops", "-O" + string(opt)}
		if got := clangFlags(opt); !slices.Equal(got, want) {
			t.Errorf("clangFlags(%q) = %q, want %q", opt, got, want)
		}
	}
}
