package ast

// BadDecl stands for a top-level declaration, an import or a package
// clause the parser could not parse: it reported the error, skipped to the
// next line that starts a declaration, and put a BadDecl in the file's
// declarations where the damaged text was. Span covers that text. Later
// stages report nothing about one: whatever it would have declared is
// unknown, and the error that made it has been reported.
type BadDecl struct {
	Span Span // from the declaration's first token through the last one skipped
}

var _ Decl = (*BadDecl)(nil)

func (b *BadDecl) declKey() declKey { return _declKey }

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

func (b *BadStmt) stmtKey() stmtKey { return _stmtKey }

func (b *BadStmt) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the damaged statement.
func (b *BadStmt) Spanned() Span { return b.Span }
