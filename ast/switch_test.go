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

// TestWalkVisitsTypeSwitchParts checks that Walk reaches a type switch's
// binding, its tag's operand, every case's type and variable and every
// statement of its body, and a type assertion's operand and type, and
// that IsTypeSwitch tells a type switch from the others.
func TestWalkVisitsTypeSwitchParts(t *testing.T) {
	x, typ := &Ident{Name: "a"}, &Ident{Name: "int64"}
	tag := &TypeAssert{X: x}
	bind, clVar, defVar := &Var{Name: "v"}, &Var{Name: "v"}, &Var{Name: "v"}
	inCase, inDefault := &Break{}, &Continue{}
	s := &Switch{Tag: tag, Bind: bind, Cases: []*CaseClause{
		{Values: []Expr{typ}, Var: clVar, Body: []Stmt{inCase}},
		{Default: true, Var: defVar, Body: []Stmt{inDefault}},
	}}
	operand, asserted := &Ident{Name: "b"}, &Ident{Name: "string"}
	assert := &TypeAssert{X: operand, TypeExpr: asserted}

	want := map[Node]bool{
		s: true, tag: true, x: true, typ: true, bind: true, clVar: true, defVar: true,
		inCase: true, inDefault: true, assert: true, operand: true, asserted: true,
	}
	for _, n := range []Node{s, assert} {
		Walk(n, func(n Node) bool {
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
	exprSwitch := &Switch{Tag: assert}
	if !s.IsTypeSwitch() || exprSwitch.IsTypeSwitch() || (&Switch{}).IsTypeSwitch() {
		t.Errorf("IsTypeSwitch misjudged a switch")
	}
}
