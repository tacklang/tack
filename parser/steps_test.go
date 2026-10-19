package parser_test

import (
	"testing"

	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/source"
)

// The tests that guard against parsing growing faster than its input
// count the tokens the parse looks at (see parser.Parser.Steps) rather
// than time it: a count does not change with what else the machine is
// doing, and it grows four times for an input four times as large when
// the parse is linear, and sixteen when it is quadratic.

// maxStepGrowth is how many times the steps may grow for an input four
// times as large: past four, for linear work with its constant overheads,
// and well short of sixteen, for quadratic work.
const maxStepGrowth = 6

// parseSteps lexes and parses src, with no limit on errors, and returns
// its errors and how many tokens the parse looked at.
func parseSteps(t *testing.T, src string) ([]diag.Diagnostic, int) {
	t.Helper()
	f := &source.File{ID: "steps", Filename: "steps.tck", Data: []byte(src)}
	lex := lexer.New(f)
	if diags := lex.Lex(); len(diags) > 0 {
		t.Fatal(diags)
	}
	p := parser.NewParser(f.ID, lex.Tokens())
	_, diags := p.Parse()
	return diags, p.Steps()
}

// checkStepGrowth checks that grown, the steps a parse of an input four
// times the size of one that took small took, is at most maxStepGrowth
// times small, ending the test when it is not, before a larger input
// that would take as long as the growth says.
func checkStepGrowth(t *testing.T, what string, small, grown int) {
	t.Helper()
	if small == 0 {
		t.Fatalf("no steps for %s", what)
	}
	if grown > maxStepGrowth*small {
		t.Fatalf("%s: steps grew from %d to %d, %.1f times, for an input four times as large",
			what, small, grown, float64(grown)/float64(small))
	}
}
