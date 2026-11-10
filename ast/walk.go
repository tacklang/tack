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
		walkAll(d.TypeParams, v)
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
	case *TypeDecl:
		Walk(d.TypeExpr, v)
	case *Import, *BadDecl:
		// leaves
	default:
		walkGenericDecl(d, v)
	}
}

// walkGenericDecl walks the children of a declaration of a generic
// function's or an interface's: a type parameter's constraint, or an
// interface method's parameters and then its results.
func walkGenericDecl(d Decl, v Visitor) {
	switch d := d.(type) {
	case *TypeParam:
		Walk(d.Constraint, v)
	case *IfaceMethod:
		walkAll(d.Params, v)
		walkAll(d.Results, v)
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
	case *Switch:
		walkSwitch(s, v)
	case *Defer:
		walkAll(s.Body, v)
	case *Break, *Continue, *Unreachable, *BadStmt:
		// leaves
	default:
		walkSimpleStmt(s, v)
	}
}

// walkSwitch walks a switch's children in source order: the variable a
// type switch binds, if written, its tag, and each case's values, then the
// variable it declares, if any, then its body.
func walkSwitch(s *Switch, v Visitor) {
	if s.Bind != nil {
		Walk(s.Bind, v)
	}
	Walk(s.Tag, v)
	for _, cl := range s.Cases {
		walkAll(cl.Values, v)
		if cl.Var != nil {
			Walk(cl.Var, v)
		}
		walkAll(cl.Body, v)
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
		if s.Elem != nil {
			Walk(s.Elem, v)
		}
		Walk(s.X, v)
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
	case *Unary, *MutArg, *OwnArg, *Selector, *TypeAssert, *Question:
		walkOperand(e, v)
	case *Call:
		Walk(e.Fn, v)
		walkAll(e.Args, v)
	case *Ident, *StrLit, *IntLit, *FloatLit, *CharLit, *BoolLit, *NoErrorLit, *CAllocatorLit:
		// leaves
	case *StructType, *InterfaceType, *UnionType, *MapType, *FuncType:
		walkTypeExpr(e, v)
	case *KeyValue:
		Walk(e.Key, v)
		Walk(e.Value, v)
	default:
		walkArrayExpr(e, v)
	}
}

// walkTypeExpr walks the children of a type as written: a struct's
// fields' types, an interface's methods, the types of a union's payloads,
// a map type's key and value types, or a function type's parameters'
// types and its results, in order.
func walkTypeExpr(e Expr, v Visitor) {
	switch e := e.(type) {
	case *StructType:
		for _, f := range e.Fields {
			Walk(f.TypeExpr, v)
		}
	case *InterfaceType:
		walkAll(e.Methods, v)
	case *UnionType:
		for _, c := range e.Cases {
			if c.TypeExpr != nil {
				Walk(c.TypeExpr, v)
			}
		}
	case *MapType:
		Walk(e.Key, v)
		Walk(e.Value, v)
	case *FuncType:
		for _, p := range e.Params {
			Walk(p.TypeExpr, v)
		}
		walkAll(e.Results, v)
	}
}

// walkOperand walks the children of an expression with one operand: a
// prefix operator's operand, a mut argument's place, an own argument's
// variable, the struct a field
// is selected from, the call a ? follows, or a type assertion's operand
// and then its type, if written.
func walkOperand(e Expr, v Visitor) {
	switch e := e.(type) {
	case *Unary:
		Walk(e.X, v)
	case *MutArg:
		Walk(e.X, v)
	case *OwnArg:
		Walk(e.X, v)
	case *Selector:
		Walk(e.X, v)
	case *Question:
		Walk(e.X, v)
	case *TypeAssert:
		Walk(e.X, v)
		if e.TypeExpr != nil {
			Walk(e.TypeExpr, v)
		}
	}
}

// walkArrayExpr walks the children of an array type, a view type, a list
// type, a composite literal, an element or a slice. A part that is left out is
// nil, and skipped.
func walkArrayExpr(e Expr, v Visitor) {
	switch e := e.(type) {
	case *ArrayType:
		if e.Len != nil {
			Walk(e.Len, v)
		}
		Walk(e.Elem, v)
	case *ViewType:
		Walk(e.Elem, v)
	case *ListType:
		Walk(e.Elem, v)
	case *CompositeLit:
		if e.TypeExpr != nil {
			Walk(e.TypeExpr, v)
		}
		walkAll(e.Elems, v)
	case *Index:
		Walk(e.X, v)
		Walk(e.Index, v)
	case *Slice:
		walkSlice(e, v)
	default:
		panic(fmt.Sprintf("ast.Walk: unhandled expression type %T", e))
	}
}

// walkSlice walks a slice's children: what it slices, then each bound
// that is written.
func walkSlice(s *Slice, v Visitor) {
	Walk(s.X, v)
	if s.Lo != nil {
		Walk(s.Lo, v)
	}
	if s.Hi != nil {
		Walk(s.Hi, v)
	}
}

// walkAll walks each node of a list in order.
func walkAll[N Node](nodes []N, v Visitor) {
	for _, n := range nodes {
		Walk(n, v)
	}
}
