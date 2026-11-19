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
// lowered them; and, from tack test's port, a module named std whose
// std/testing's T.errorf and T.fatalf take no testing.Site, whose formats
// Go reads at its second argument all the same, and three libraries
// whose tack.h names the header ruling of 2026-11-18 changes, checked as
// --check checks a library, each a known divergence; and, from the
// Unicode 18.0.0 upgrade, a printf verb of a rune 18.0.0 assigns, which
// Go quotes and the port does not, a known divergence too.
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
	for _, in := range asSuite(ins, probesStage) {
		t.Run(in.name, func(t *testing.T) {
			t.Parallel()
			runCaseWith(t, *st, in, env.portPairs)
		})
	}
}

// TestProbesTypes runs the programs of the probes stage with
// --port-types too, compared as TestTypes compares a case of the type
// checker's: some probes found the port's dump differing from Go's
// checker. A probe holding a rune whose class Go's unicode package and
// the port's std/unicode differ on, in a file or, for a directory probe,
// in a path below it, skips (see skipUnicodeDrift and probeBytes): the
// dump holds the checker's messages, which quote it apart, and no entry
// of the divergence list applies to a dump; the probes stage compares
// such a probe's --check with its entry, unicode18-printf-verb. It runs
// only as TestPort does.
func TestProbesTypes(t *testing.T) {
	ready(t)
	ins, err := probeInputs()
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range ins {
		t.Run(in.name, func(t *testing.T) {
			t.Parallel()
			srcs, err := probeBytes(in)
			if err != nil {
				t.Fatal(err)
			}
			skipUnicodeDrift(t, srcs...)
			checkTypes(t, in, env.portRun, false)
		})
	}
}

// probeBytes returns what in, a probe, gives the compilers: the bytes of
// its file, for a probe of one file, which probeInputs runs from
// testdata/probes; or, for a directory probe, which it runs from the
// probe's own directory, the path of every file and directory below that
// directory and the bytes of every file, each as one entry.
func probeBytes(in input) ([][]byte, error) {
	if filepath.Base(in.dir) == "probes" {
		src, err := os.ReadFile(filepath.Join(in.dir, in.file)) //nolint:gosec // G304: a probe of this module
		if err != nil {
			return nil, err
		}
		return [][]byte{src}, nil
	}
	var out [][]byte
	err := filepath.WalkDir(in.dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(in.dir, p)
		if err != nil {
			return err
		}
		out = append(out, []byte(filepath.ToSlash(rel)))
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p) //nolint:gosec // G304: a probe of this module
		if err != nil {
			return err
		}
		out = append(out, data)
		return nil
	})
	return out, err
}
