package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/codegen"
)

// unionMethodsWant is what examples/union-methods prints when every check
// passes, in the order main runs them.
var unionMethodsWant = strings.Join([]string{
	"nothing live at the start",
	"a method of a union", "a method on a union built in place",
	"a method on a case with no payload", "a method calling another on its receiver",
	"a mut method switching on its receiver", "a mut method reassigning its receiver",
	"a mut method assigning in a clause binding nothing",
	"a method on an element", "a mut method on an element", "methods on fields",
	"a method of a union holding a list", "a mut method setting a payload",
	"a mut method making a list", "a mut method replacing a list",
	"a mut method freeing a list", "a list made from nothing",
	"a mut method on a field holding a list",
	"values freed",
	"a bag filled", "unions and a struct through one generic function",
	"a union's method through a generic function",
	"a union and a struct as two type arguments",
	"a union passed on to another generic function",
	"a union's mut method through a generic function",
	"a union holding a list reset generically",
	"a list freed through a generic function", "a struct reset generically",
	"generics freed",
	"a pub method of another package's union", "a pub mut method of another package's union",
	"another package's union through its generic function",
	"a union with private methods through another package's generic function",
	"a union holding a list through another package",
	"another package's union through a generic function here",
	"a generic call inside another package with its own union",
	"a list through another package's generic function",
	"packages freed",
}, " ok\n") + " ok\n"

// TestBuiltUnionMethodsPassesEveryCheck builds examples/union-methods at
// every optimization level and checks the binary passes every check:
// methods on unions, plain and mut, switching on and reassigning their
// receivers, on locals, temporaries, elements and fields, on a union
// holding a list, unions and structs through the same generic functions,
// and unions crossing to another package's generic functions, with
// c_allocator's count of live buffers back to zero after each check that
// made a list.
func TestBuiltUnionMethodsPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	target := filepath.Join(examplesDir, "union-methods")
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "union-methods")
			r := runTack(t, "-O"+string(opt), "--out", bin, target)
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != unionMethodsWant || code != 0 {
				t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 0", out, code, unionMethodsWant)
			}
		})
	}
}
