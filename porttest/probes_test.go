package porttest

import (
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
// calls deep enough to have overflowed the port's stack, and a call
// whose argument's own call reported an escape, which Go reports no
// escape of again.
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
// --check --max-errors 0 from that directory, in the order of their
// names.
func probeInputs() ([]input, error) {
	dir := filepath.Join(env.root, "porttest", "testdata", "probes")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var ins []input
	for _, e := range entries {
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
