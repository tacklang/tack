package main

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tacklang/tack/codegen"
)

// errorsWant is what examples/errors prints when every check passes.
var errorsWant = "success ok\n" +
	"failure ok\n" +
	"empty ok\n" +
	"error alone ok\n" +
	"propagated ok\n" +
	"nested ok\n" +
	"three results ok\n" +
	"the smallest int64 divided by -1 ok\n" +
	"loop ok\n" +
	"switch on code ok\n" +
	"switch default ok\n" +
	"rebinding ok\n" +
	"deferred ok\n" +
	"sentinel ok\n" +
	"sentinel success ok\n" +
	"mutable global ok\n" +
	"fields and arrays ok\n" +
	"zero value ok\n" +
	"fields assigned ok\n" +
	"code alone decides ok\n" +
	"message from a slice ok\n" +
	"passed on unchecked ok\n" +
	"passed on with two values ok\n" +
	"checked together ok\n"

// errorsModuleWant is what examples/errors-module prints when every check
// passes.
var errorsModuleWant = "parsed ok\n" +
	"sentinel of another package ok\n" +
	"propagated across packages ok\n" +
	"error alone ok\n"

// externErrorsWant is what examples/extern-errors prints when every check
// passes.
var externErrorsWant = "no error from C ok\n" +
	"error from C ok\n" +
	"results struct ok\n" +
	"error to C ok\n" +
	"struct field through a pointer ok\n" +
	"variadic errors ok\n"

// TestBuiltErrorsPassesEveryCheck builds the error examples at every
// optimization level and checks each binary passes every check:
// examples/errors, with errors returned alone and last, success and
// failure, propagation two calls deep, three results, checks in if and
// else, in a loop and in a switch on the code, rebinding, a defer after
// the check, sentinels, a mutable global, errors in structs and arrays,
// their zero value, assigned fields, a code of 0 with a message, and a
// message sliced from a string, values passed on unchecked beside their
// error; examples/errors-module, whose errors come
// from a function of another package; and examples/extern-errors, whose
// errors cross to C and back. Each check prints its name and "ok", or
// "FAIL".
func TestBuiltErrorsPassesEveryCheck(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	for _, tc := range []struct{ name, target, want string }{
		{"errors", example("errors"), errorsWant},
		{"errors-module", filepath.Join(examplesDir, "errors-module"), errorsModuleWant},
		{"extern-errors", example("extern-errors"), externErrorsWant},
	} {
		for _, opt := range codegen.Opts {
			t.Run(tc.name+"/"+string(opt), func(t *testing.T) {
				bin := filepath.Join(t.TempDir(), tc.name)
				r := runTack(t, "-O"+string(opt), "--out", bin, tc.target)
				if r.code != exitOK || r.stderr != "" {
					t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
				}
				if out, code := runBuilt(t, bin); out != tc.want || code != 0 {
					t.Errorf("printed\n%s\nand exited %d, want\n%s\nand 0", out, code, tc.want)
				}
			})
		}
	}
}
