package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"unicode"

	"github.com/tacklang/tack/internal/stdtest"
)

// tablesPath is the committed tables file, below the repository's root.
var tablesPath = filepath.Join(stdtest.Dir(), "unicode", "tables.tck")

// TestTablesAreCurrent fails while src/std/unicode/tables.tck differs
// from what generate writes with the Go running the test, whose Unicode
// version, unicode.Version, the file names: a Go whose Unicode differs,
// or an edit by hand, fails it until make unicode-tables is run.
func TestTablesAreCurrent(t *testing.T) {
	got, err := os.ReadFile(tablesPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := generate(); !bytes.Equal(got, want) {
		t.Errorf(
			"%s is not what unicodegen writes for Unicode %s; run make unicode-tables and read the diff",
			tablesPath,
			unicode.Version,
		)
	}
}

// TestRunsMatchPredicates checks, for each class, the runs generate
// writes for it, as checkRuns does.
func TestRunsMatchPredicates(t *testing.T) {
	for _, c := range classes {
		checkRuns(t, c)
	}
}

// checkRuns checks that the runs of c are in ascending order with a gap
// between each two, that they hold exactly the runes its Go predicate
// holds for, over every rune, and that no value past unicode.MaxRune or
// below 0 is in c, as std/unicode answers for those without a search.
func checkRuns(t *testing.T, c class) {
	t.Helper()
	rs := runs(c.is)
	for i, r := range rs {
		if r[0] > r[1] || i > 0 && rs[i-1][1]+1 >= r[0] {
			t.Errorf("%s: run %d, %x to %x, is out of order", c.name, i, r[0], r[1])
		}
	}
	if r, ok := firstDifference(rs, c.is); ok {
		t.Errorf(
			"%s: rune %x is in the runs %t, the predicate says %t",
			c.name,
			r,
			!c.is(r),
			c.is(r),
		)
	}
	for _, r := range []rune{-1, -0x80000000, unicode.MaxRune + 1, 0x7fffffff} {
		if c.is(r) {
			t.Errorf(
				"%s: Go's predicate holds for %x, which std/unicode puts in no class",
				c.name,
				r,
			)
		}
	}
}

// firstDifference returns the first rune from 0 to unicode.MaxRune that
// rs, runs in ascending order, holds and is does not, or is holds and rs
// does not, and whether there is one.
func firstDifference(rs [][2]rune, is func(rune) bool) (rune, bool) {
	next := 0
	for r := rune(0); r <= unicode.MaxRune; r++ {
		for next < len(rs) && rs[next][1] < r {
			next++
		}
		if in := next < len(rs) && rs[next][0] <= r; in != is(r) {
			return r, true
		}
	}
	return 0, false
}
