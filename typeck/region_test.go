package typeck_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/tacklang/tack/internal/invalid"
	"github.com/tacklang/tack/typeck"
)

// guardedExits returns j proven, then n times an early exit bounding n
// from above, each tighter than the last, and a use of a[j], none of
// which writes n: the facts of each exit hold in the rest of the block,
// and no write there ends them.
func guardedExits(n int) []string {
	lines := provenJ()
	for k := range n {
		lines = append(lines, "if n > "+strconv.Itoa(1000000-k)+" {", "return -1", "}", "t +%= a[j]")
	}
	return append(lines, "return t")
}

// regionShapes are the programs whose regions are each the rest of a
// long block behind a short head: early exits with uses after each, with
// and without writes to what they prove, and an else if chain with as
// many uses after it as links.
var regionShapes = []reachShape{
	{"early exits", guardedExits, 16000},
	{"straight line", straightWrites, 4000},
	{"else if chain", elseIfWrites, 16000},
}

// TestRegionsShareTheRestOfABlock checks that a block of thousands of
// early exits, each giving the rest of the block facts, and an else if
// chain with as many uses after it, are checked walking and copying
// nodes linearly in the block (see checkGrowth): the nodes walked to
// find the writes in the regions the facts hold in, and the nodes copied
// into those regions. Each region is the rest of the block behind what
// an exit or link falls through to first, so walking the rest to index
// its writes, or copying it, once a region would make them quadratic.
// With the oracle on, every query walks its region too, so the test is
// skipped.
func TestRegionsShareTheRestOfABlock(t *testing.T) {
	if typeck.RegionOracle() {
		t.Skip("the region oracle walks every region it checks")
	}
	for _, shape := range regionShapes {
		t.Run(shape.name, func(t *testing.T) {
			diags, small := checkWork(t, inF(shape.lines(500)...))
			if len(diags) > 0 {
				t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
			}
			diags, grown := checkWork(t, inF(shape.lines(2000)...))
			if len(diags) > 0 {
				t.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
			}
			checkGrowth(t, "nodes walked for writes", small.Walked, grown.Walked)
			checkGrowth(t, "statements copied into regions", small.Copied, grown.Copied)
		})
	}
}

// BenchmarkRegions times checking each region shape at 1000, 4000 and
// 16000 exits or links, up to the most it is measured at.
func BenchmarkRegions(b *testing.B) {
	for _, shape := range regionShapes {
		for _, n := range []int{1000, 4000, 16000} {
			if n > shape.most {
				continue
			}
			src := inF(shape.lines(n)...)
			b.Run(shape.name+"/"+strconv.Itoa(n), func(b *testing.B) {
				for range b.N {
					_, c := checkerFor(b, src)
					if diags := errorsOf(c.Check()); len(diags) > 0 {
						b.Fatalf("got %d diagnostics, first %v", len(diags), diags[0])
					}
				}
			})
		}
	}
}

// TestRegionIndexMatchesWalkOnPrograms checks every case of
// examples/invalid.txtar and examples/warnings.txtar, and the region,
// reach and leaving shapes, each at a few sizes, with every query of a
// region's writes checked against an index of a walk of the region's
// nodes, as each region was indexed before its rest was shared (see
// typeck.CheckRegions), which panics on a difference. Whether a program
// is accepted does not matter here, only that both find the same writes.
func TestRegionIndexMatchesWalkOnPrograms(t *testing.T) {
	defer typeck.CheckRegions()()
	check := func(t *testing.T, name string, src []byte) {
		t.Helper()
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("%s: %v", name, r)
			}
		}()
		checkWith(src, nil)
	}
	for _, path := range []string{invalid.Path, invalid.WarningsPath} {
		cases, err := invalid.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			check(t, c.Name, c.Source)
		}
	}
	shapes := append(regionShapes[:len(regionShapes):len(regionShapes)], reachShapes...)
	shapes = append(shapes,
		reachShape{"deferred uses", deferredUses, 0},
		reachShape{"late default uses", lateDefaultUses, 0},
		reachShape{"stored uses", storedUses, 0},
		reachShape{"one use loop of ifs", oneUseLoopIfs, 0},
		reachShape{"terminating chain", endingElseIfs, 0},
		reachShape{"leaving chain", leavingElseIfs, 0},
		reachShape{"continuing chain", continuingElseIfs, 0},
		reachShape{"continuing levels", continuingLevels, 0},
	)
	for _, shape := range shapes {
		for _, n := range []int{1, 3, 17, 40} {
			check(t, fmt.Sprintf("%s/%d", shape.name, n), []byte(inF(shape.lines(n)...)))
		}
	}
}
