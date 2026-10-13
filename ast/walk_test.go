package ast

import "testing"

// TestWalkVisitsStructNodes checks that Walk reaches every part of a type
// declaration, a struct literal and a field selection, in source order,
// rather than panicking on a node it does not know.
func TestWalkVisitsStructNodes(t *testing.T) {
	length := &IntLit{}
	decl := &TypeDecl{
		Name: "P",
		TypeExpr: &StructType{Fields: []*Field{
			{Name: "x", TypeExpr: &Ident{Name: "int64"}},
			{Name: "a", TypeExpr: &ArrayType{Len: length, Elem: &Ident{Name: "bool"}}},
		}},
	}
	key, value := &Ident{Name: "x"}, &IntLit{}
	lit := &CompositeLit{
		TypeExpr: &Ident{Name: "P"},
		Elems:    []Expr{&KeyValue{Key: key, Value: value}},
	}
	sel := &Selector{X: lit, Name: "x"}

	var seen []Node
	for _, n := range []Node{decl, sel} {
		Walk(n, func(n Node) bool {
			seen = append(seen, n)
			return true
		})
	}

	want := map[Node]bool{length: true, key: true, value: true, lit: true, sel: true, decl: true}
	for _, n := range seen {
		delete(want, n)
	}
	if len(want) > 0 {
		t.Errorf("Walk missed %d nodes; visited %d", len(want), len(seen))
	}
}

// TestRootFollowsFieldsAndElements checks that the variable an assignment
// writes is found through any mix of indexes and fields, and that a chain
// starting from a call has none.
func TestRootFollowsFieldsAndElements(t *testing.T) {
	p := &Ident{Name: "p"}
	target := &Selector{X: &Index{X: &Selector{X: p, Name: "a"}, Index: &IntLit{}}, Name: "x"}
	if got := Root(target); got != p {
		t.Errorf("Root(p.a[0].x) = %v, want p", got)
	}
	if got := Root(&Selector{X: &Call{Fn: &Ident{Name: "f"}}, Name: "x"}); got != nil {
		t.Errorf("Root(f().x) = %v, want nil", got)
	}
}
