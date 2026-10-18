package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tacklang/tack/codegen"
)

// genericsWant is what examples/generics prints when every check passes,
// each check's name and then " ok", and last what its generic report
// prints through fmt.printf.
var genericsWant = strings.Join([]string{
	"nothing live at the start",
	"a mut method and a plain one through a generic function",
	"the same instance from a second call", "a field passed as mut",
	"a constraint with no methods", "a type argument holding a list",
	"two type parameters of two types", "a moved type argument", "buffers freed",
	"a write with room", "an error from a method through a generic function",
	"recursion", "recursion with another type", "recursion swapping its type arguments",
	"recursion freed", "a defer in a generic function", "defers freed",
	"two parameters of one type parameter",
	"a generic function and an interface of another package",
	"a type whose private methods satisfy another package's interface here",
	"a private type passed on through generic functions of two packages",
	"a constraint of another package", "a generic call inside another package",
	"packages freed",
}, " ok\n") + " ok\na counter holds 5\n"

// TestBuiltGenericsPassesEveryCheck builds examples/generics at every
// optimization level and checks the binary passes every check: generic
// functions called with several struct types, one holding a list among
// them, through mut and plain methods, passed on to narrower constraints,
// recursing, deferring, with several type parameters, and across
// packages, with c_allocator's count of live buffers back to zero after
// each check that made a list.
func TestBuiltGenericsPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	target := filepath.Join(examplesDir, "generics")
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "generics")
			r := runTack(t, "-O"+string(opt), "--out", bin, target)
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != genericsWant || code != 0 {
				t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 0", out, code, genericsWant)
			}
		})
	}
}

// interfaceParamsWant is what examples/interface-params prints when every
// check passes.
var interfaceParamsWant = strings.Join([]string{
	"a mut interface parameter",
	"two parameters of one interface, two types",
	"an interface of another package",
	"the same function, another type",
	"beside an explicit type parameter",
	"an own interface parameter",
}, " ok\n") + " ok\n"

// TestBuiltInterfaceParamsPassesEveryCheck builds examples/interface-params
// at every optimization level and checks the binary passes every check: an
// interface as a parameter's whole type, shorthand for a type parameter,
// mut, own, two of one interface bound to two types, one of another
// package, and beside an explicit type parameter.
func TestBuiltInterfaceParamsPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	target := filepath.Join(examplesDir, "interface-params")
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "interface-params")
			r := runTack(t, "-O"+string(opt), "--out", bin, target)
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if out, code := runBuilt(t, bin); out != interfaceParamsWant || code != 0 {
				t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 0",
					out, code, interfaceParamsWant)
			}
		})
	}
}
