package ast

import "testing"

// TestWalkVisitsSwitchParts checks that Walk reaches a switch's tag, every
// case value and every statement of every case body, the default's
// included, and that a tagless switch's missing tag is skipped.
func TestWalkVisitsSwitchParts(t *testing.T) {
	tag, v1, v2 := &Ident{Name: "x"}, &IntLit{}, &IntLit{}
	inCase, inDefault := &Break{}, &Continue{}
	tagged := &Switch{Tag: tag, Cases: []*CaseClause{
		{Values: []Expr{v1, v2}, Body: []Stmt{inCase}},
		{Default: true, Body: []Stmt{inDefault}},
	}}
	cond := &BoolLit{Value: true}
	tagless := &Switch{Cases: []*CaseClause{{Values: []Expr{cond}}}}

	want := map[Node]bool{
		tagged: true, tag: true, v1: true, v2: true, inCase: true,
		inDefault: true, tagless: true, cond: true,
	}
	for _, s := range []*Switch{tagged, tagless} {
		Walk(s, func(n Node) bool {
			if !want[n] {
				t.Errorf("Walk visited %T, which it should not", n)
			}
			delete(want, n)
			return true
		})
	}
	if len(want) > 0 {
		t.Errorf("Walk missed %d nodes", len(want))
	}
	if tagged.Default() != tagged.Cases[1] || tagless.Default() != nil {
		t.Errorf("Default() found the wrong case")
	}
}
