package ast

type stmtKey struct{}

var _stmtKey stmtKey

// Stmt is a statement inside a function body.
type Stmt interface {
	stmtKey() stmtKey
	Spanned() Span

	Node
}

// ExprStmt is an expression evaluated for its effect, such as a call.
type ExprStmt struct {
	X    Expr
	Span Span // start of X through the terminating Semi's position
}

var _ Stmt = (*ExprStmt)(nil)

func (s *ExprStmt) stmtKey() stmtKey { return _stmtKey }

func (s *ExprStmt) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the start of X through the
// terminating semicolon's position.
func (s *ExprStmt) Spanned() Span { return s.Span }

// Return ends a function, optionally with a value. Value is nil for a bare
// return, which is only legal in a function that declares no result.
type Return struct {
	Value Expr
	Span  Span // 'return' keyword through the terminating Semi's position
}

var _ Stmt = (*Return)(nil)

func (r *Return) stmtKey() stmtKey { return _stmtKey }

func (r *Return) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the 'return' keyword through the
// terminating semicolon's position.
func (r *Return) Spanned() Span { return r.Span }

// If is a conditional. Else is nil when there is no else branch, and holds a
// single *If when the source chained "else if".
type If struct {
	Cond Expr
	Then []Stmt
	Else []Stmt
	Span Span
}

var _ Stmt = (*If)(nil)

func (i *If) stmtKey() stmtKey { return _stmtKey }

func (i *If) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the whole statement, else branch
// included.
func (i *If) Spanned() Span { return i.Span }
