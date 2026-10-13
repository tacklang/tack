package source_test

import (
	"testing"

	"github.com/tacklang/tack/source"
)

// TestSetLooksUpByID checks a Set finds each file by its ID, keeps them in
// the order added, and finds nothing for an unknown ID, a nil Set included.
func TestSetLooksUpByID(t *testing.T) {
	a := &source.File{ID: "a"}
	b := &source.File{ID: "b"}
	s := source.NewSet(a, b)
	if s.Lookup("a") != a || s.Lookup("b") != b || s.Lookup("c") != nil {
		t.Errorf("lookups gave %p %p %p", s.Lookup("a"), s.Lookup("b"), s.Lookup("c"))
	}
	if got := s.Files(); len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("Files() = %v", got)
	}
	var none *source.Set
	if none.Lookup("a") != nil || none.Files() != nil {
		t.Errorf("a nil Set found something")
	}
	var zero source.Set
	zero.Add(a)
	if zero.Lookup("a") != a {
		t.Errorf("the zero Set did not take a file")
	}
}

// TestFileStandsForEverySpan checks a lone File is found for any ID.
func TestFileStandsForEverySpan(t *testing.T) {
	f := &source.File{ID: "a"}
	if f.Lookup("a") != f || f.Lookup("other") != f || f.Lookup("") != f {
		t.Errorf("a lone File was not found for every ID")
	}
}
