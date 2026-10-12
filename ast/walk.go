package ast

import "fmt"

// Visitor is called for each node. Returning false stops the walk from
// descending into that node's children.
type Visitor func(Node) bool

// Walk calls v for n, then for each of n's children in source order unless v
// returned false. This is the only place in the compiler that knows how nodes
// nest; passes that traverse the tree should go through it.
func Walk(n Node, v Visitor) {
	if n == nil || !v(n) {
		return
	}

	switch n := n.(type) {
	case *File:
		walkAll(n.Decls, v)
	case Decl:
		walkDecl(n, v)
	case Stmt:
		walkStmt(n, v)
	case Expr:
		walkExpr(n, v)
	default:
		panic(fmt.Sprintf("ast.Walk: unhandled node type %T", n))
	}
}

// walkDecl walks the children of a declaration.
func walkDecl(d Decl, v Visitor) {
	switch d := d.(type) {
	case *Func:
		walkAll(d.Params, v)
		if d.Result != nil {
			Walk(d.Result, v)
		}
		walkAll(d.Body, v)
	case *Param:
		Walk(d.Type, v)
	case *Var:
		if d.Type != nil {
			Walk(d.Type, v)
		}
		if d.Value != nil {
			Walk(d.Value, v)
		}
	default:
		panic(fmt.Sprintf("ast.Walk: unhandled declaration type %T", d))
	}
}

// walkStmt walks the children of a statement.
func walkStmt(s Stmt, v Visitor) {
	switch s := s.(type) {
	case *Return:
		if s.Value != nil {
			Walk(s.Value, v)
		}
	case *If:
		Walk(s.Cond, v)
		walkAll(s.Then, v)
		walkAll(s.Else, v)
	case *ExprStmt:
		Walk(s.X, v)
	case *Assign:
		Walk(s.Target, v)
		Walk(s.Value, v)
	default:
		panic(fmt.Sprintf("ast.Walk: unhandled statement type %T", s))
	}
}

// walkExpr walks the children of an expression.
func walkExpr(e Expr, v Visitor) {
	switch e := e.(type) {
	case *Binary:
		Walk(e.X, v)
		Walk(e.Y, v)
	case *Call:
		Walk(e.Fn, v)
		walkAll(e.Args, v)
	case *Ident, *StrLit, *IntLit:
		// leaves
	default:
		panic(fmt.Sprintf("ast.Walk: unhandled expression type %T", e))
	}
}

// walkAll walks each node of a list in order.
func walkAll[N Node](nodes []N, v Visitor) {
	for _, n := range nodes {
		Walk(n, v)
	}
}
