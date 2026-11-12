package c_test

import (
	"path/filepath"
	"testing"
)

// arenaUnionsWant is how many checks examples/arena-unions prints.
const arenaUnionsWant = 39

// TestArenaUnionsExampleIsSanitizerClean builds examples/arena-unions,
// its C file and std's C under each of sanitizerFlags, and checks every
// check it prints passed: arenas and pools of a union owning nothing, of
// one whose cases are all empty, of one holding a list and a Text, of one
// holding a struct holding a list, and of unions nested in unions, owned
// and not, built, read, written, ranged over, removed from and dropped; a
// union owning nothing through ? and assigned beside an error; and adds
// and inserts refused out of memory freeing the unions they were given,
// with c_allocator's count of live buffers back to zero after each group.
// A union owning nothing has no free, so none of these may call one.
func TestArenaUnionsExampleIsSanitizerClean(t *testing.T) {
	src, csrc := exampleFiles(t, "arena-unions", "live.c")
	for _, flags := range sanitizerFlags {
		t.Run(filepath.Join(flags...), func(t *testing.T) {
			runChecks(t, buildWithStdAndC(t, src, csrc, flags...), arenaUnionsWant)
		})
	}
}
