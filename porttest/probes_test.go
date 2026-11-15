package porttest

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// probesStage is the stage of the programs in testdata/probes, each one
// a reviewer's probe that found the port giving other output than Go,
// and that no case of the corpus covered: compound assignments whose
// value ends the proof of the target's index, an append spread whose
// count is not known, an outer index a checked index is in, a map's
// value assigned under a key of the wrong type, nests of operators and
// calls deep enough to have overflowed the port's stack, the bytes of a
// known local string, a tagless switch whose value writes what a case
// falling into it proved, a checked division of untyped constants,
// function types whose dump differed, a call whose argument's own call
// reported an escape, which Go reports no escape of again, a struct
// literal of a struct declaring a field twice, and a region in a
// closure Go never checks; and, from the port of loops and contracts,
// the bounds loops keep, a probe's frames past their cap, counting
// loops whose steps may wrap, contracts carried to callers, broken by
// defers and on generic functions, an ensures clause's && nested on its
// right and its arithmetic, unreachable places of every kind, the
// lengths appends prove and an error declared with its type, which
// proves none, and three found beside them: a ? on add, add_all and
// insert, an element of a span's view of an arena bound with ok, and
// printf's %v given a view; and, from the backend's handover, what the
// checker records for the backend beyond types, testing.Site arguments
// in every position among them, and a copy of the builtins'
// documentation in a module std that differs from the compiler's; and,
// from the header, names tack.h reserves in every place a name can take
// one, names two things in tack.h share, and a field tack.h cannot name
// deep in arrays and structs held by value; and, from the backend's
// values, the methods the unused pass counts as used, called as x.m(),
// mut x.m() and T.m(x), which it warned of as unused until the backend
// lowered them.
const probesStage = "probes"

func init() {
	stages = append(stages, stage{
		name:   probesStage,
		args:   checkArgs,
		inputs: probeInputs,
		stdout: compareBytes,
	})
}

// probeInputs returns the programs of testdata/probes, each run with
// --check --max-errors 0 from that directory, and each directory of it,
// run so from that directory with the target its file target names, in
// the order of their names.
func probeInputs() ([]input, error) {
	dir := filepath.Join(env.root, "porttest", "testdata", "probes")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var ins []input
	for _, e := range entries {
		if e.IsDir() {
			sub := filepath.Join(dir, e.Name())
			target, err := os.ReadFile(filepath.Join(sub, "target"))
			if err != nil {
				return nil, fmt.Errorf("reading the target of probe %s: %w", e.Name(), err)
			}
			ins = append(ins, input{name: e.Name(), dir: sub, file: strings.TrimSpace(string(target))})
			continue
		}
		if !strings.HasSuffix(e.Name(), ".tck") {
			continue
		}
		ins = append(ins, input{name: e.Name(), dir: dir, file: e.Name()})
	}
	slices.SortFunc(ins, func(a, b input) int { return strings.Compare(a.name, b.name) })
	return ins, nil
}

// TestProbesPairStack runs the programs of the probes stage again with
// the port's stack held to pairStack KB, as TestCheckDeepPairs runs its
// nests: those that nest deeply must leave half a megabyte of deepStack
// spare. It runs only as TestPort does.
func TestProbesPairStack(t *testing.T) {
	ready(t)
	st := stageNamed(probesStage)
	if st == nil {
		t.Fatal("no probes stage")
	}
	ins, err := probeInputs()
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range ins {
		t.Run(in.name, func(t *testing.T) {
			t.Parallel()
			runCaseWith(t, *st, in, env.portPairs)
		})
	}
}

// TestProbesTypes runs the programs of the probes stage with
// --port-types too, compared as TestTypes compares a case of the type
// checker's: some probes found the port's dump differing from Go's
// checker. It runs only as TestPort does.
func TestProbesTypes(t *testing.T) {
	ready(t)
	ins, err := probeInputs()
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range ins {
		t.Run(in.name, func(t *testing.T) {
			t.Parallel()
			checkTypes(t, in, env.portRun, false)
		})
	}
}
