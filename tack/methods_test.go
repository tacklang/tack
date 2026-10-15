package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/codegen"
)

// methodsWant is what examples/methods prints when every check passes. The
// last line is written by fmt.printf, called from a method that forwards
// its variadic to it, and then " ok".
var methodsWant = strings.Join([]string{
	"nothing live at the start", "empty builder", "write through a mut receiver",
	"methods named len and append", "several results", "several results past the end",
	"result beside an error", "recursion, and a function of the same name",
	"variadic method", "variadic method with no values", "spread into a variadic method",
	"any parameter", "a method calling an extern func", "builds freed",
	"defer in a method", "error from a method", "lines freed",
	"mut receiver through a field", "deep chain", "array element in a field",
	"mut parameter passed on as a receiver", "chains freed",
	"temporary receiver", "literal receiver", "array elements",
	"plain receiver of an element", "receiver first, then left to right",
	"plain receiver copied before the call",
	"pub methods of another package", "lib method in the module",
	"pub method of a private type", "a list of another package through its methods",
	"packages freed", "declared in another file, before the type", "tallies freed",
	"forwarded 42",
}, " ok\n") + " ok\n"

// TestBuiltMethodsPassesEveryCheck builds examples/methods at every
// optimization level and checks the binary passes every check: plain and
// mut methods called on locals, fields, array elements, mut parameters,
// temporaries and literals, chains of fields before a mut receiver,
// methods named like builtins, several results, errors, variadics, any,
// recursion, defers, the order a receiver and the arguments are evaluated
// in, methods of another package, and methods declared in another file,
// with c_allocator's count of live buffers back to zero after each check
// that made a list.
func TestBuiltMethodsPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	target := filepath.Join(examplesDir, "methods")
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "methods")
			r := runTack(t, "-O"+string(opt), "--out", bin, target)
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != methodsWant || code != 0 {
				t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 0", out, code, methodsWant)
			}
		})
	}
}
