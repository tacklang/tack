package porttest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmitProbes runs --emit, compared as emit-single compares its
// cases, on each program of testdata/emit-probes, the port's stack held
// to deepStack KB: programs written against the port of generics and
// functions that no case of the corpus covers, each one Go's backend
// lowers to C that compiles and, run under the address and undefined
// behavior sanitizers, leaks nothing. Instances of one generic function
// whose own parameter owns under one binding and not under the next,
// met in both orders, which a port that kept what a type owns across
// bindings frees wrongly; interface methods called through stand-ins
// on structs and unions, by mut and by value, and through an interface
// parameter; closures in fields loops and in switches on type
// parameters inside instances, nested, capturing the loop's field and
// the narrowed value; make of a type parameter in a closure; a function
// of several results and one taking its argument as own called through
// values; a plain function value, and a closure with a defer and a ?,
// passed to scope parameters; captures of owned values written through
// nested closures and closures in loops; function values in structs and
// lists, method expressions taking a mut receiver, and calls through
// values of several results and of an error. It runs only as TestPort
// does.
func TestEmitProbes(t *testing.T) {
	ready(t)
	dir := filepath.Join(env.root, "porttest", "testdata", "emit-probes")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	srcs := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tck") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		srcs[e.Name()] = string(src)
	}
	if len(srcs) == 0 {
		t.Fatal("testdata/emit-probes holds no program")
	}
	runEmitDeep(t, emitSingleStage, srcs, env.portRun)
}
