package typeck

import (
	"path/filepath"
	"testing"
)

// TestRelationIndexMatchesScanOnExamples checks every program under
// examples/ with every query of the relations in force checking their
// index, kept in step with the facts in force, against one a scan of
// them builds, and the graph it builds against the one from that scan
// (see relationOracle), which panics on a difference.
func TestRelationIndexMatchesScanOnExamples(t *testing.T) {
	defer func(was bool) { relationOracle = was }(relationOracle)
	relationOracle = true
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
