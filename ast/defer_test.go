package ast

import "testing"

// TestWalkVisitsDeferBody checks that Walk reaches every statement a defer
// holds, and what is nested in them, a defer inside a deferred block
// included, which the parser builds and the checker must find, since a pass
// that searches a region for assignments must see the ones a defer makes.
func TestWalkVisitsDeferBody(t *testing.T) {
	target, value := &Ident{Name: "x"}, &IntLit{}
	assign := &Assign{Target: target, Value: value}
	call := &ExprStmt{X: &Call{Fn: &Ident{Name: "f"}}}
	inner := &Defer{Body: []Stmt{assign}}
	outer := &Defer{Body: []Stmt{call, inner}}

	want := map[Node]bool{
		outer:  true,
		call:   true,
		inner:  true,
		assign: true,
		target: true,
		value:  true,
	}
	Walk(outer, func(n Node) bool {
		delete(want, n)
		return true
	})
	if len(want) > 0 {
		t.Errorf("Walk missed %d nodes", len(want))
	}
}
