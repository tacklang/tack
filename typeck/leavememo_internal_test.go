package typeck

import (
	"path/filepath"
	"testing"
)

// TestRememberedLeavingMatchesAfreshOnExamples checks every program
// under examples/ with every answer the checker remembers for whether a
// block leaves or a statement terminates checked against one worked out
// afresh (see leaveOracle), which panics on a difference.
func TestRememberedLeavingMatchesAfreshOnExamples(t *testing.T) {
	defer func(was bool) { leaveOracle = was }(leaveOracle)
	leaveOracle = true
	dirs, err := filepath.Glob(filepath.Join("..", "examples", "*"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, dir := range dirs {
		if c, _ := checkedExample(t, dir); c != nil {
			checked++
		}
	}
	if checked < 50 {
		t.Fatalf("only %d examples checked cleanly", checked)
	}
}
