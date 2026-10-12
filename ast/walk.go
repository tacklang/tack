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
		walkAll(d.Results, v)
		walkAll(d.Body, v)
	case *Param:
		Walk(d.TypeExpr, v)
	case *Var:
		if d.TypeExpr != nil {
			Walk(d.TypeExpr, v)
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
		walkAll(s.Values, v)
	case *If:
		Walk(s.Cond, v)
		walkAll(s.Then, v)
		walkAll(s.Else, v)
	case *For, *ForRange:
		walkLoop(s, v)
	case *Break, *Continue:
		// leaves
	default:
		walkSimpleStmt(s, v)
	}
}

// walkLoop walks the children of a loop, its header's parts in source order
// and then its body. A part that is left out is nil, which Walk skips.
func walkLoop(s Stmt, v Visitor) {
	switch s := s.(type) {
	case *For:
		Walk(s.Init, v)
		Walk(s.Cond, v)
		Walk(s.Post, v)
		walkAll(s.Body, v)
	case *ForRange:
		if s.Var != nil {
			Walk(s.Var, v)
		}
		Walk(s.Count, v)
		walkAll(s.Body, v)
	}
}

// walkSimpleStmt walks the children of a simple statement: one that holds no
// other statement. A Var never reaches it, since Walk treats it as a
// declaration.
func walkSimpleStmt(s Stmt, v Visitor) {
	switch s := s.(type) {
	case *ExprStmt:
		Walk(s.X, v)
	case *Assign:
		Walk(s.Target, v)
		Walk(s.Value, v)
	case *IncDec:
		Walk(s.Target, v)
	case *MultiVar:
		walkAll(s.Vars, v)
		Walk(s.Value, v)
	case *MultiAssign:
		walkAll(s.Targets, v)
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
	case *Unary:
		Walk(e.X, v)
	case *Call:
		Walk(e.Fn, v)
		walkAll(e.Args, v)
	case *Ident, *StrLit, *IntLit, *BoolLit:
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
