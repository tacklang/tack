package porttest

import (
	"fmt"
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
// values of several results and of an error. And programs where Go's
// backend writes C that does not compile, each a known divergence (see
// the package doc), whose C the port writes as the divergence list
// records it, which compiles and runs under the sanitizers, leaking
// nothing: builtins that only give a value written as statements, the
// zero value of a type parameter standing for a string, an error or an
// Allocator, and the zero value of a string where nothing pools the
// empty string. Its cases are emit-probes's in the divergence list. It
// runs only as TestPort does.
func TestEmitProbes(t *testing.T) {
	ready(t)
	srcs, err := emitProbeSources()
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) == 0 {
		t.Fatal("testdata/emit-probes holds no program")
	}
	runEmitSuite(t, emitSingleStage, emitProbesSuite, srcs, env.portRun)
}

// emitProbeSources returns the programs of testdata/emit-probes, each
// source by its file's name.
func emitProbeSources() (map[string]string, error) {
	dir := filepath.Join(env.root, "porttest", "testdata", "emit-probes")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("listing the emit probes: %w", err)
	}
	srcs := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tck") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading an emit probe: %w", err)
		}
		srcs[e.Name()] = string(src)
	}
	return srcs, nil
}
