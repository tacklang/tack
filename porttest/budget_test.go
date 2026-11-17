package porttest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// budgetLogVar is the environment variable naming a measure log, written
// by a run with measureLogVar set, that TestBudget reads. Unset or empty,
// TestBudget skips.
const budgetLogVar = "PORTTEST_BUDGET_LOG"

// budgetShown is how many of the worst ratios TestBudget logs, of time
// and of memory each.
const budgetShown = 20

// budgetPair is one run of the port and the run of the Go compiler with
// the same test, directory and arguments, from a measure log.
type budgetPair struct {
	key        string
	port, goes measuredRun
}

// TestBudget reads the measure log $PORTTEST_BUDGET_LOG names, pairs each
// run of the port with the run of the Go compiler of the same test,
// working directory and arguments, and fails for each pair where the
// port took more than perfTimeFactor times Go's wall time and
// perfTimeSlack, or more than perfMemoryFactor times its peak memory, as
// TestResPerf bounds its program. It logs how many pairs it read, and the
// budgetShown worst ratios of time and of memory. A run with no partner,
// as a case the port declines has, whose Go run never happens, is
// counted, not compared; a log with no pair at all fails, since it
// checks nothing. It measures nothing again itself: the log's runs share
// the machine with the parallel cases, so a pair it fails is to be rerun
// alone, with go test -run naming its test and -parallel 1 into a fresh
// log, and that log checked, before the failure is taken as the port's.
// It needs no compiler and runs whenever the variable is set.
func TestBudget(t *testing.T) {
	path := os.Getenv(budgetLogVar)
	if path == "" {
		t.Skip("the budget check reads a measure log: set " + budgetLogVar)
	}
	pairs, unpaired, err := readBudgetLog(fromLogPath(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d pairs, %d runs with no partner", len(pairs), unpaired)
	if len(pairs) == 0 {
		t.Fatalf("%s holds no run of the port paired with one of Go's: nothing was checked", path)
	}
	byTime := append([]budgetPair(nil), pairs...)
	sort.SliceStable(byTime, func(i, j int) bool {
		return timeRatio(byTime[i]) > timeRatio(byTime[j])
	})
	for _, p := range byTime[:min(budgetShown, len(byTime))] {
		t.Logf("time %.2fx: port %v, Go %v: %s", timeRatio(p),
			time.Duration(p.port.WallNS), time.Duration(p.goes.WallNS), p.key)
	}
	byMem := append([]budgetPair(nil), pairs...)
	sort.SliceStable(byMem, func(i, j int) bool { return memRatio(byMem[i]) > memRatio(byMem[j]) })
	for _, p := range byMem[:min(budgetShown, len(byMem))] {
		t.Logf(
			"memory %.2fx: port %d KB, Go %d KB: %s",
			memRatio(p),
			p.port.RSSKB,
			p.goes.RSSKB,
			p.key,
		)
	}
	for _, p := range pairs {
		if pt, gt := time.Duration(
			p.port.WallNS,
		), time.Duration(
			p.goes.WallNS,
		); pt > perfTimeFactor*gt+perfTimeSlack {
			t.Errorf(
				"the port took %v, more than %d times Go's %v and %v: %s",
				pt,
				perfTimeFactor,
				gt,
				perfTimeSlack,
				p.key,
			)
		}
		if p.port.RSSKB > perfMemoryFactor*p.goes.RSSKB {
			t.Errorf("the port's peak memory was %d KB, more than %d times Go's %d KB: %s",
				p.port.RSSKB, perfMemoryFactor, p.goes.RSSKB, p.key)
		}
	}
}

// fromLogPath returns path made absolute as fromRoot does, when TestMain
// has found the module's root, and as it is otherwise.
func fromLogPath(path string) string {
	if env.root == "" {
		return path
	}
	return fromRoot(path)
}

// readBudgetLog reads the measure log at path and returns its pairs, in
// the order their port runs come, and how many runs it found no partner
// for. A test that ran the same command
// line twice keeps its last run of each.
func readBudgetLog(path string) ([]budgetPair, int, error) {
	f, err := os.Open(path) //nolint:gosec // G304: the log the variable names
	if err != nil {
		return nil, 0, fmt.Errorf("opening the measure log: %w", err)
	}
	defer f.Close() //nolint:errcheck // read only
	goRuns := map[string]measuredRun{}
	portRuns := map[string]measuredRun{}
	var order []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for n := 1; sc.Scan(); n++ {
		var r measuredRun
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, 0, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		key := r.Test + " | " + r.Dir + " | " + strings.Join(r.Args, " ")
		if r.Comp == "go" {
			goRuns[key] = r
			continue
		}
		if _, seen := portRuns[key]; !seen {
			order = append(order, key)
		}
		portRuns[key] = r
	}
	if err := sc.Err(); err != nil {
		return nil, 0, fmt.Errorf("reading the measure log: %w", err)
	}
	var pairs []budgetPair
	for _, key := range order {
		if g, ok := goRuns[key]; ok {
			pairs = append(pairs, budgetPair{key: key, port: portRuns[key], goes: g})
		}
	}
	unpaired := len(portRuns) + len(goRuns) - 2*len(pairs)
	return pairs, unpaired, nil
}

// timeRatio is the port's wall time over Go's in p.
func timeRatio(p budgetPair) float64 {
	return float64(p.port.WallNS) / float64(max(p.goes.WallNS, 1))
}

// memRatio is the port's peak memory over Go's in p.
func memRatio(p budgetPair) float64 {
	return float64(p.port.RSSKB) / float64(max(p.goes.RSSKB, 1))
}
