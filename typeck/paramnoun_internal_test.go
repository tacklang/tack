package typeck

import (
	"testing"

	"github.com/tacklang/tack/ast"
)

// TestOrdinal checks the English ordinals a parameter named _ is named
// by, the teens' th included.
func TestOrdinal(t *testing.T) {
	for n, want := range map[int]string{
		1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 10: "10th", 11: "11th", 12: "12th",
		13: "13th", 21: "21st", 22: "22nd", 23: "23rd", 101: "101st", 111: "111th", 112: "112th",
	} {
		if got := ordinal(n); got != want {
			t.Errorf("ordinal(%d) = %q, want %q", n, got, want)
		}
	}
}

// TestParamNouns checks a named parameter keeps its name in every form,
// a _ one is named by its position among the parameters written in the
// parentheses, and a _ receiver is the receiver.
func TestParamNouns(t *testing.T) {
	ps := []*ast.Param{{Name: ast.Blank}, {Name: "n"}, {Name: ast.Blank}}
	for _, tt := range []struct {
		got, want string
	}{
		{paramNoun(ps, 1, false), "parameter n"},
		{itsParam(ps, 1, false), "its parameter n"},
		{argParamName(ps, 1, false), "n"},
		{paramNoun(ps, 0, false), "the 1st parameter"},
		{paramNoun(ps, 2, false), "the 3rd parameter"},
		{itsParam(ps, 2, false), "its 3rd parameter"},
		{argParamName(ps, 2, false), "its 3rd parameter"},
		{argParamName(ps, 0, true), "its receiver"},
		{argParamName(ps, 2, true), "its 2nd parameter"},
		{paramNoun(ps, 0, true), "the receiver"},
		{itsParam(ps, 0, true), "its receiver"},
		{paramNoun(ps, 2, true), "the 2nd parameter"},
	} {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}
