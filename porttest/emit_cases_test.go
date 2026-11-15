package porttest

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

// classifiedInput is one case of a stage that classifies its cases,
// with what its run covers.
type classifiedInput struct {
	cov   covered
	stage string
	in    input
}

// classifyAll returns every case of every stage that classifies its
// cases, in stage and input order, each with what classify covers on
// it, fresh as classify takes it, as many classified at once as
// GOMAXPROCS.
func classifyAll(fresh bool) ([]classifiedInput, error) {
	var all []classifiedInput
	classifiers := map[string]stage{}
	for _, st := range stages {
		if st.classify == nil {
			continue
		}
		ins, err := st.inputs()
		if err != nil {
			return nil, err
		}
		classifiers[st.name] = st
		for _, in := range ins {
			all = append(all, classifiedInput{stage: st.name, in: in})
		}
	}
	work := make(chan int)
	errs := make([]error, len(all))
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for i := range work {
				all[i].cov, errs[i] = classifiers[all[i].stage].classify(all[i].in, fresh)
			}
		})
	}
	for i := range all {
		work <- i
	}
	close(work)
	wg.Wait()
	return all, errorsJoin(errs)
}

// errorsJoin returns the errors of errs that are not nil, joined, or nil.
func errorsJoin(errs []error) error {
	var msgs []string
	for _, err := range errs {
		if err != nil {
			msgs = append(msgs, err.Error())
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return fmt.Errorf("%d cases could not be classified:\n%s", len(msgs), strings.Join(msgs, "\n"))
}

// censusOrder is the order the census lists the sets in: by how many
// stages, then by the stages.
func censusOrder(a, b emitSet) int {
	if n, m := popCount(a), popCount(b); n != m {
		return n - m
	}
	return int(a) - int(b)
}

// popCount returns how many stages s holds.
func popCount(s emitSet) int {
	n := 0
	for st := range emitStages {
		if s.has(st) {
			n++
		}
	}
	return n
}

// TestEmitCensus classifies every case of every stage that classifies
// its cases and logs, for each, how many cases need each set of emit
// stages, and for all of them together how many need each set, how many
// each stage would leave required were the stages ported in each order
// of the plan, and the same counts were the header's part of every
// program, newHeader's walk of a program with no extern func, the
// core's (see stagesOf). It is the census the stages are ordered by; it
// fails only when a case cannot be classified. It runs only as TestPort
// does.
func TestEmitCensus(t *testing.T) {
	ready(t)
	all, err := classifyAll(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, baselineH := range []emitStage{emitH, emitE1} {
		text, err := census(all, baselineH)
		if err != nil {
			t.Fatal(err)
		}
		t.Log(text)
	}
	var front []string
	for _, c := range all {
		set, err := stagesOf(c.cov, emitH)
		if err != nil {
			t.Fatal(err)
		}
		if c.stage == emitHeaderStage && set == emitSet(0).with(emitE0) {
			front = append(front, c.in.name)
		}
	}
	t.Logf(
		"%d cases of %s never reach the backend: %s",
		len(front),
		emitHeaderStage,
		strings.Join(front, " "),
	)
}

// censusOrders are the orders the census counts the required cases in,
// each one the plan allows.
var censusOrders = [][]emitStage{
	{emitE0, emitE1, emitH, emitE2, emitE3, emitE4},
	{emitE0, emitH, emitE1, emitE2, emitE3, emitE4},
	{emitE0, emitE1, emitH, emitE3, emitE2, emitE4},
}

// census returns the census of all, with baselineH as stagesOf takes it:
// how many cases need each set of stages, by stage and in all, and how
// many each stage leaves required in each of censusOrders.
func census(all []classifiedInput, baselineH emitStage) (string, error) {
	byStage := map[string]map[emitSet]int{}
	total := map[emitSet]int{}
	var stageOrder []string
	for _, c := range all {
		set, err := stagesOf(c.cov, baselineH)
		if err != nil {
			return "", err
		}
		if byStage[c.stage] == nil {
			byStage[c.stage] = map[emitSet]int{}
			stageOrder = append(stageOrder, c.stage)
		}
		byStage[c.stage][set]++
		total[set]++
	}
	var b strings.Builder
	if baselineH == emitH {
		b.WriteString("cases by the emit stages they need:\n")
	} else {
		b.WriteString(
			"were the header's part of every program E1's, cases by the emit stages they need:\n",
		)
	}
	for _, name := range stageOrder {
		fmt.Fprintf(&b, "  %s:", name)
		writeCounts(&b, byStage[name])
	}
	b.WriteString("  all:")
	writeCounts(&b, total)
	for _, order := range censusOrders {
		fmt.Fprintf(&b, "  required as ported in the order %v:", order)
		var req emitSet
		for _, st := range order {
			req = req.with(st)
			fmt.Fprintf(&b, " +%s %d", st, requiredBy(total, req))
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// requiredBy returns how many of the cases counted by set in total need
// only stages of req.
func requiredBy(total map[emitSet]int, req emitSet) int {
	n := 0
	for set, count := range total {
		if set&^req == 0 {
			n += count
		}
	}
	return n
}

// writeCounts writes counts, by set, in censusOrder, to b, then a
// newline.
func writeCounts(b *strings.Builder, counts map[emitSet]int) {
	n := 0
	for _, set := range slices.SortedFunc(maps.Keys(counts), censusOrder) {
		fmt.Fprintf(b, " %s %d,", set, counts[set])
		n += counts[set]
	}
	fmt.Fprintf(b, " in all %d\n", n)
}

// TestEmitClassifyDeterministic classifies every case of every stage
// that classifies its cases anew, the cache left alone, and checks that
// it needs the stages it needed as the cache holds it, written by an
// earlier run or by this one: a classification that changed between
// runs would make a case required in one run and not in another. The
// blocks themselves may differ within a function whose branches depend
// on the order Go ranges over a map in, as sliceHelpers' sort does,
// while its output does not; those cases are counted in the log, with
// the first. It runs only as TestPort does.
//
//nolint:cyclop // a run of steps, each returning its error or skipping; split, it reads no plainer
func TestEmitClassifyDeterministic(t *testing.T) {
	ready(t)
	cached, err := classifyAll(false)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := classifyAll(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(cached) != len(fresh) {
		t.Fatalf("%d cases, then %d", len(cached), len(fresh))
	}
	blocks, first := 0, ""
	for i := range cached {
		a, b := cached[i], fresh[i]
		if a.stage != b.stage || a.in.name != b.in.name {
			t.Fatalf("case %d is %s %s, then %s %s", i, a.stage, a.in.name, b.stage, b.in.name)
		}
		for _, baselineH := range []emitStage{emitH, emitE1} {
			x, errA := stagesOf(a.cov, baselineH)
			y, errB := stagesOf(b.cov, baselineH)
			if errA != nil || errB != nil {
				t.Fatal(errors.Join(errA, errB))
			}
			if x != y {
				t.Errorf("%s %s needs %s, then %s", a.stage, a.in.name, x, y)
			}
		}
		if d := coveredDiff(a.cov, b.cov); d != "" {
			if blocks == 0 {
				first = a.stage + " " + a.in.name + ": " + d
			}
			blocks++
		}
	}
	t.Logf("%d of %d cases run other blocks run again, needing the same stages; the first, %s",
		blocks, len(cached), first)
}

// coveredDiff returns the first file whose blocks a and b differ in, and
// the first block one has that the other has not, or "".
func coveredDiff(a, b covered) string {
	files := slices.Sorted(maps.Keys(a))
	for f := range b {
		if _, ok := a[f]; !ok {
			files = append(files, f)
		}
	}
	for _, f := range files {
		x, y := a[f], b[f]
		if slices.Equal(x, y) {
			continue
		}
		for _, s := range x {
			if !slices.Contains(y, s) {
				return fmt.Sprintf("%s:%s first, not again", f, s)
			}
		}
		for _, s := range y {
			if !slices.Contains(x, s) {
				return fmt.Sprintf("%s:%s again, not first", f, s)
			}
		}
	}
	return ""
}

// TestEmitHeaderRejects checks that each case the backend rejects,
// whose expected output's stage is codegen, as check-backend runs it,
// needs only E0 and H: tack.h's check of what it can name, headerDiags,
// returns before generate lowers anything. It runs only as TestPort
// does.
func TestEmitHeaderRejects(t *testing.T) {
	ready(t)
	st := stageNamed(checkStageInfo[stageBackend].stage)
	if st == nil {
		t.Fatal("no check-backend stage")
	}
	cases, err := classifiedCases()
	if err != nil {
		t.Fatal(err)
	}
	want := emitSet(0).with(emitE0).with(emitH)
	n := 0
	for _, c := range cases {
		if !bytes.HasPrefix(c.expected, []byte("stage: codegen\n")) {
			continue
		}
		n++
		in, err := checkInput(c)
		if err != nil {
			t.Fatal(err)
		}
		cov, err := st.classify(in, false)
		if err != nil {
			t.Fatal(err)
		}
		needs, err := stagesOf(cov, emitH)
		if err != nil {
			t.Fatal(err)
		}
		if needs != want {
			t.Errorf("%s needs %s, want %s; it runs, of E1: %s", in.name, needs, want,
				strings.Join(stageFuncs(cov, emitE1), ", "))
		}
	}
	t.Logf("%d cases the backend rejects", n)
	if n == 0 {
		t.Error("no case the backend rejects")
	}
}

// stageFuncs returns the functions, as file and funcKey, of the blocks
// cov runs that stagesOf gives the stage st, sorted.
func stageFuncs(cov covered, st emitStage) []string {
	funcs := map[string]bool{}
	for file, starts := range cov {
		for _, s := range starts {
			one := covered{file: {s}}
			if set, err := stagesOf(one, emitH); err == nil && set.has(st) {
				line, col, _ := parseStart(s)
				funcs[strings.TrimPrefix(file, "codegen/c/")+" "+cover.staged[file].funcAt(line, col)] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(funcs))
}

// TestEmitBaseline logs, for each checkpoint, the functions of a later
// stage whose blocks its baseline programs run, which count for the
// checkpoint (see blockStages), so its port does their
// not-taken paths, and those of H's E1's run, which every lowered
// program needs H for. It runs only as TestPort does.
func TestEmitBaseline(t *testing.T) {
	ready(t)
	if err := coverSetup(); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, cp := range []emitStage{emitE1, emitE2, emitE3} {
		byStage, err := laterFuncs(cp)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "%s's baselines run, of later stages:\n", cp)
		for st := range emitStages {
			if len(byStage[st]) > 0 {
				fmt.Fprintf(
					&b,
					"  %s: %s\n",
					st,
					strings.Join(slices.Sorted(maps.Keys(byStage[st])), ", "),
				)
			}
		}
	}
	t.Log(b.String())
}

// laterFuncs returns, by stage, the functions, as file and funcKey, of
// the blocks of a later stage that the checkpoint cp's baselines run
// and that count for cp, and, for E1, of H's that they run.
//
//nolint:cyclop // a run of steps, each returning its error or skipping; split, it reads no plainer
func laterFuncs(cp emitStage) (map[emitStage]map[string]bool, error) {
	byStage := map[emitStage]map[string]bool{}
	for file, starts := range cover.baseline[cp] {
		sf := cover.staged[file]
		for s := range starts {
			line, col, err := parseStart(s)
			if err != nil {
				return nil, err
			}
			st := sf.stageAt(line, col)
			counted := st == emitH && cp == emitE1 ||
				st != emitH && st > cp && blockStages(file, s, st, emitH).has(cp)
			if cover.idle[file][s] || !counted {
				continue
			}
			if byStage[st] == nil {
				byStage[st] = map[string]bool{}
			}
			byStage[st][strings.TrimPrefix(file, "codegen/c/")+" "+sf.funcAt(line, col)] = true
		}
	}
	return byStage, nil
}

// emitPinned are cases whose stage sets were read by hand, by stage and
// input name: hello, which needs the core and the header's check alone,
// and an extern func's tack.h, whose program holds no value beyond the
// core's. TestEmitPinned checks them.
var emitPinned = []struct {
	stage, name string
	want        emitSet
}{
	{emitSingleStage, "examples/hello/main.tck", emitSet(0).with(emitE0).with(emitH).with(emitE1)},
	{emitHeaderStage, "examples/extern/main.tck", emitSet(0).with(emitE0).with(emitH).with(emitE1)},
}

// TestEmitPinned checks the stage sets of emitPinned's cases. It runs
// only as TestPort does.
func TestEmitPinned(t *testing.T) {
	ready(t)
	for _, p := range emitPinned {
		st := stageNamed(p.stage)
		if st == nil {
			t.Fatalf("no %s stage", p.stage)
		}
		ins, err := st.inputs()
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(ins, func(in input) bool { return in.name == p.name })
		if i < 0 {
			t.Errorf("%s has no case %s", p.stage, p.name)
			continue
		}
		got, err := classifyCase(*st, ins[i], false)
		if err != nil {
			t.Fatal(err)
		}
		if got != p.want {
			t.Errorf("%s %s needs %s, want %s", p.stage, p.name, got, p.want)
		}
	}
}
