package typeck

import (
	"path/filepath"
	"testing"
)

// TestRegionIndexMatchesWalkOnExamples checks every program under
// examples/ with every query of a region's writes checked against an
// index of a walk of all the region's nodes (see regionOracle), which
// panics on a difference.
func TestRegionIndexMatchesWalkOnExamples(t *testing.T) {
	defer func(was bool) { regionOracle = was }(regionOracle)
	regionOracle = true
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
