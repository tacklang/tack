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

// Assign sets a variable to a new value, Target = Value. A compound
// assignment, such as Target += Value, records its arithmetic operator in Op
// and means Target = Target Op Value; a plain assignment's Op is InvalidOp.
type Assign struct {
	Target *Ident
	Value  Expr
	Span   Span // start of Target through the terminating Semi's position
	Op     BinaryOp
}

var _ Stmt = (*Assign)(nil)

func (a *Assign) stmtKey() stmtKey { return _stmtKey }

func (a *Assign) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the target through the terminating
// semicolon's position.
func (a *Assign) Spanned() Span { return a.Span }

// For is a loop. Cond is nil for a loop with no condition, which runs until
// something in its body leaves it.
type For struct {
	Cond Expr
	Body []Stmt
	Span Span // 'for' keyword through the closing brace
}

var _ Stmt = (*For)(nil)

func (f *For) stmtKey() stmtKey { return _stmtKey }

func (f *For) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the 'for' keyword through the
// closing brace.
func (f *For) Spanned() Span { return f.Span }

// Break leaves the innermost loop containing it.
type Break struct {
	Span Span // the 'break' keyword
}

var _ Stmt = (*Break)(nil)

func (b *Break) stmtKey() stmtKey { return _stmtKey }

func (b *Break) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the 'break' keyword.
func (b *Break) Spanned() Span { return b.Span }

// Continue starts the next iteration of the innermost loop containing it.
type Continue struct {
	Span Span // the 'continue' keyword
}

var _ Stmt = (*Continue)(nil)

func (c *Continue) stmtKey() stmtKey { return _stmtKey }

func (c *Continue) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the 'continue' keyword.
func (c *Continue) Spanned() Span { return c.Span }

// IncDec adds one to a variable (Target++) or, when Dec is set, subtracts one
// (Target--). It is a statement, never a value.
type IncDec struct {
	Target *Ident
	Span   Span // start of Target through the operator
	Dec    bool
}

var _ Stmt = (*IncDec)(nil)

func (s *IncDec) stmtKey() stmtKey { return _stmtKey }

func (s *IncDec) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the target through the operator.
func (s *IncDec) Spanned() Span { return s.Span }

// Op returns the operator as written in source.
func (s *IncDec) Op() string {
	if s.Dec {
		return "--"
	}
	return "++"
}
