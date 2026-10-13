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

// Return ends a function, with one value for each result the function
// declares. Values is empty for a bare return, which is only legal in a
// function that declares no result.
type Return struct {
	Values []Expr
	Span   Span // 'return' keyword through the terminating Semi's position
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
	Target Expr // a variable's name, or an element of one, a[i]
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
// something in its body leaves it. A three-clause loop, for Init; Cond; Post,
// also has Init, run once before the first condition, and Post, run after
// each iteration, including one ended by continue; either may be nil, and
// both are nil in a loop written without semicolons. A variable Init
// declares belongs to the loop.
type For struct {
	Init Stmt
	Cond Expr
	Post Stmt
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
	Target Expr // a variable's name, or an element of one
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

// MultiVar declares one variable for each result of a call, by position:
// q, mut r := divmod(a, b). Each Var is immutable unless its own name was
// written with mut, and has no Value of its own; Value is the call that
// provides them all.
type MultiVar struct {
	Value Expr
	Vars  []*Var
	Span  Span // start of the first name through the end of Value
}

var _ Stmt = (*MultiVar)(nil)

func (m *MultiVar) stmtKey() stmtKey { return _stmtKey }

func (m *MultiVar) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the first name through the end of
// the value.
func (m *MultiVar) Spanned() Span { return m.Span }

// MultiAssign assigns each result of a call to an existing variable, by
// position: q, r = divmod(a, b). Every target must be a mutable variable.
type MultiAssign struct {
	Value   Expr
	Targets []Expr // each a variable's name, _, or an element of a variable
	Span    Span   // start of the first target through the end of Value
}

var _ Stmt = (*MultiAssign)(nil)

func (m *MultiAssign) stmtKey() stmtKey { return _stmtKey }

func (m *MultiAssign) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the first target through the end of
// the value.
func (m *MultiAssign) Spanned() Span { return m.Span }

// ForRange is a range loop. Over an int, for i := range X runs its body
// once for each of 0, 1, ... X-1, and not at all when X is zero or less.
// Over an array or a string, for i, x := range X runs it once for each
// element, or byte, in order, with i its index and x its value, walking a
// copy of X taken before the first iteration, so the body changing X does
// not change what later iterations see. X is evaluated exactly once, before
// the first iteration.
//
// Var is the index variable, or the count's, and Elem the element variable;
// each is a fresh immutable variable every iteration. Both are nil in the
// form for range X, and Elem is nil when only one name is written. Var may
// be _, which binds nothing, when Elem is written: for _, x := range X.
type ForRange struct {
	Var  *Var
	Elem *Var
	X    Expr
	Body []Stmt
	Span Span // 'for' keyword through the closing brace
}

var _ Stmt = (*ForRange)(nil)

func (f *ForRange) stmtKey() stmtKey { return _stmtKey }

func (f *ForRange) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the 'for' keyword through the
// closing brace.
func (f *ForRange) Spanned() Span { return f.Span }

// Switch is a multi-way branch. In an expression switch, Tag is the value
// each case's values are compared with, by ==; in a tagless switch, Tag is
// nil and each case value is a bool condition. Tag is evaluated exactly
// once, before any case. The cases are tried in source order, and the
// values within a case left to right, until one matches, and the values
// after it are not evaluated; the default case, if there is one, is tried
// last wherever it is written. The matching case's body runs, and then
// the statement after the switch: there is no fallthrough. A break or
// continue in a body acts on the loop enclosing the switch, not on the
// switch.
type Switch struct {
	Tag   Expr
	Cases []*CaseClause
	Span  Span // 'switch' keyword through the closing brace
}

var _ Stmt = (*Switch)(nil)

func (s *Switch) stmtKey() stmtKey { return _stmtKey }

func (s *Switch) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the 'switch' keyword through the
// closing brace.
func (s *Switch) Spanned() Span { return s.Span }

// Default returns the switch's default case, or nil when it has none. The
// parser allows at most one.
func (s *Switch) Default() *CaseClause {
	for _, cl := range s.Cases {
		if cl.Default {
			return cl
		}
	}
	return nil
}

// CaseClause is one case of a Switch: case Values...: Body, or, when
// Default is set, default: Body, whose Values is empty. Body is a block of
// its own, and may be empty. A CaseClause is part of its Switch, not a
// node of its own; Walk visits its values and body.
type CaseClause struct {
	Values  []Expr
	Body    []Stmt
	Span    Span // 'case' or 'default' keyword through the colon
	Default bool
}

// Defer registers Body to run when the block holding the defer is left,
// by any path: falling off its end, or a return, break or continue that
// leaves it. Each block runs the defers it reached in reverse order, and a
// return, break or continue that leaves several blocks runs the innermost
// block's first. A defer that execution never reached does not run. Body
// is evaluated when it runs, not when the defer is reached, so it reads
// variables as they are then. It is a block of its own, which no return,
// and no break or continue aimed at a loop outside it, may leave.
//
// Written as defer { ... }, Block is set and Body holds the block's
// statements, which may be any statements but those leaving it. Written
// as defer followed by one statement, which must be a call, an assignment,
// a compound assignment, an increment or a decrement, Body holds just that
// statement.
type Defer struct {
	Body  []Stmt
	Span  Span // 'defer' keyword through the end of the deferred statement
	Block bool
}

var _ Stmt = (*Defer)(nil)

func (d *Defer) stmtKey() stmtKey { return _stmtKey }

func (d *Defer) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the 'defer' keyword through the
// end of the deferred statement or block.
func (d *Defer) Spanned() Span { return d.Span }
