package typeck

import (
	"path/filepath"
	"testing"
)

// TestFactIndexMatchesScanOnExamples checks every program under
// examples/ with every query of the facts in force reading their index,
// kept in step with them, and checking it, and what the query of the
// facts not shadowed reads, against a scan of every fact in force (see
// indexOracle), which panics on a difference.
func TestFactIndexMatchesScanOnExamples(t *testing.T) {
	defer func(was bool) { indexOracle = was }(indexOracle)
	indexOracle = true
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
