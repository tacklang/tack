package ast

// BadDecl stands for a top-level declaration, an import or a package
// clause the parser could not parse: it reported the error, skipped to the
// next line that starts a declaration, and put a BadDecl in the file's
// declarations where the damaged text was. Span covers that text. Later
// stages report nothing about one: whatever it would have declared is
// unknown, and the error that made it has been reported.
//
// Name is the name the declaration was declaring, when the parser got as
// far as reading it, and empty otherwise: a function's, a type's, a
// global's or an import's, or, when Method is set, a method's, of a type
// not known. Method is never set without Name. The resolver and the type checker treat that name as
// invalid, reporting nothing about a use of it, since what it named is
// unknown; a damaged declaration with no Name could have declared any
// name of its package.
type BadDecl struct {
	Name   string
	Span   Span // from the declaration's first token through the last one skipped
	Method bool
}

var _ Decl = (*BadDecl)(nil)

// declKey marks BadDecl as a Decl, sealing Decl to this package's types.
func (b *BadDecl) declKey() declKey { return _declKey }

// astNode marks BadDecl as a Node, sealing Node to this package's types.
func (b *BadDecl) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the damaged declaration.
func (b *BadDecl) Spanned() Span { return b.Span }

// BadStmt stands for a statement the parser could not parse: it reported
// the error, skipped to the end of the statement, the semicolon at its own
// depth or the closing brace of the block holding it, and put a BadStmt in
// the block where the damaged text was. Span covers that text. Later
// stages report nothing about one, as for a BadDecl.
type BadStmt struct {
	Span Span // from the statement's first token through the last one skipped
}

var _ Stmt = (*BadStmt)(nil)

// stmtKey marks BadStmt as a Stmt, sealing Stmt to this package's types.
func (b *BadStmt) stmtKey() stmtKey { return _stmtKey }

// astNode marks BadStmt as a Node, sealing Node to this package's types.
func (b *BadStmt) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the damaged statement.
func (b *BadStmt) Spanned() Span { return b.Span }

// Unbound is what the resolver binds a name to when the name names nothing
// usable and that is already known to be an error: the resolver reported
// the name, or the declaration it names, or the name may have been
// declared by source the parser could not parse (see BadDecl, BadStmt).
// The type checker gives such a name an invalid type and reports nothing
// more about it. A name the resolver leaves bound to nothing, with a nil
// Def, is reported by the type checker instead. It is a BadDecl only so
// that it is a Decl; it stands for no source.
var Unbound Decl = &BadDecl{}

// BadStmts returns the bad statements in body, at any depth, in source
// order.
func BadStmts(body []Stmt) []*BadStmt {
	var bad []*BadStmt
	for _, s := range body {
		Walk(s, func(n Node) bool {
			if b, ok := n.(*BadStmt); ok {
				bad = append(bad, b)
			}
			return true
		})
	}
	return bad
}
