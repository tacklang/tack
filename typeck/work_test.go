package typeck_test

import (
	"testing"

	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/typeck"
)

// The tests that guard against checking growing faster than the program
// checked count the provers' steps rather than time them: a count does
// not change with what else the machine is doing, so they hold under any
// load, and it grows four times for an input four times as large when the
// work is linear, and sixteen when it is quadratic.

// maxGrowth is how many times a count of steps may grow for an input four
// times as large: past four, for linear work with its constant overheads,
// and well short of sixteen, for quadratic work.
const maxGrowth = 6

// checkWork checks src and returns its diagnostics and the steps the
// provers took (see typeck.ProverWork).
func checkWork(t *testing.T, src string) ([]diag.Diagnostic, typeck.ProverWork) {
	t.Helper()
	_, c := checkerFor(t, src)
	diags := c.Check()
	return diags, c.ProverWork()
}

// checkGrowth checks that small, a count of what steps were taken on an
// input, is more than zero, so the input exercises them, and that grown,
// the count on one four times its size, is at most maxGrowth times it.
func checkGrowth(t *testing.T, what string, small, grown int) {
	t.Helper()
	if small == 0 {
		t.Fatalf("no %s", what)
	}
	if grown > maxGrowth*small {
		t.Errorf("%s grew from %d to %d, %.1f times, for an input four times as large",
			what, small, grown, float64(grown)/float64(small))
	}
}
