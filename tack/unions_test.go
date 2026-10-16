package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/codegen"
)

// unionsWant is what examples/unions prints when every check passes, in
// the order main runs them.
var unionsWant = strings.Join([]string{
	"nothing live at the start",
	"expression tree", "expression tree, another x", "default clause of a union switch",
	"a list of unions", "an element assigned whole", "an element read is a copy",
	"trees freed",
	"a case with no payload compared with ==", "same case, equal payloads",
	"same case, different payloads", "different cases", "a string payload compares by its bytes",
	"an empty string payload", "a range over an array of unions", "package-level unions",
	"the smallest int64 as a payload", "arrays of unions compared with ==", "a mutable union",
	"a union assigned",
	"a binding read in its clause", "a clause naming several cases", "a case with no payload",
	"break and continue in a union switch act on the loop",
	"a payload case named without binding it", "a case named like a variable in scope",
	"a switch on a call's result", "a switch on a call's result, no payload",
	"a list payload", "a struct payload holding a list", "reassigning frees the old payload",
	"a new list payload", "a case with no payload", "a union returned",
	"a list moved into a payload",
	"owned unions freed",
	"reassigned every iteration", "declared in a loop's body",
	"loops freed",
	"a union moved into a struct", "a union not moved", "a defer reading a union",
	"moves freed",
	"a union holding a union holding a list", "a union holding a union of no list",
	"a nested union assigned", "a nested union's lists freed",
	"nested unions freed",
	"a union of another package", "matching a union of another package",
	"a case with no payload of another package", "comparing unions of another package",
	"a payload built in another package",
}, " ok\n") + " ok\n"

// TestBuiltUnionsPassesEveryCheck builds examples/unions at every
// optimization level and checks the binary passes every check: an
// expression tree held in a list and evaluated recursively, tokens
// compared with ==, every kind of switch clause, switches on temporaries,
// unions holding lists built, reassigned, moved, read from a defer and
// nested, and the unions of another package, with c_allocator's count of
// live buffers back to zero after each check that made a list.
func TestBuiltUnionsPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	target := filepath.Join(examplesDir, "unions")
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "unions")
			r := runTack(t, "-O"+string(opt), "--out", bin, target)
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != unionsWant || code != 0 {
				t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 0", out, code, unionsWant)
			}
		})
	}
}
