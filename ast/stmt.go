package ast

import "github.com/tacklang/tack/types"

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
//
// In a type switch, switch v := x.(type) or switch x.(type), Tag is a
// TypeAssert whose TypeExpr is nil, and each case value is a type as
// written, which a case matches when the value x refers to has that type
// exactly; see IsTypeSwitch. Bind is the variable v written in the
// header, nil when there is none. It is a binding, not an init
// statement, which a switch never has: each case declares a variable of
// its own named after it, the CaseClause's Var, and Bind is that name as
// written, the declaration the resolver reports a problem with the name
// at, and an unused v at, once for the whole switch.
//
// A switch whose tag is a union, switch e { case int(n): ... case name,
// eof: ... }, matches the case the union holds: each value names one of
// its cases, a pattern (see CasePattern), and a clause naming one case
// may bind that case's payload to a variable, its CaseClause's Var. The
// type checker sets Exhaustive when its clauses name every case, which a
// switch on a union must unless it has a default.
type Switch struct {
	Tag   Expr
	Bind  *Var
	Cases []*CaseClause
	Span  Span // 'switch' keyword through the closing brace

	// Exhaustive is set by the type checker on a switch on a union whose
	// clauses name every one of its cases, which then has no default and
	// always runs one of its clauses.
	Exhaustive bool `json:",omitempty"`
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

// IsTypeSwitch reports whether s is a type switch: its tag is x.(type),
// and its case values are types.
func (s *Switch) IsTypeSwitch() bool {
	ta, ok := s.Tag.(*TypeAssert)
	return ok && ta.TypeExpr == nil
}

// CaseClause is one case of a Switch: case Values...: Body, or, when
// Default is set, default: Body, whose Values is empty. Body is a block of
// its own, and may be empty. A CaseClause is part of its Switch, not a
// node of its own; Walk visits its values, its variable and its body.
//
// In a type switch with a binding, Var is the variable the case declares
// for its body, named as the switch's Bind is, and immutable: the type
// checker gives it the case's one type, or, in a case naming several and
// in the default, the type of the tag's operand, any. It has no Value; it
// takes the tag's. In a switch on a union, Var is the variable a clause
// naming one case binds that case's payload to, int(n)'s n, which the
// resolver declares, immutable, for the clause's body, and the type
// checker gives the payload's type; it refers to the payload in place.
// Var is nil in every other case. In a type switch,
// Types holds the type each value names, in order, set by the type
// checker, since a type as written has no type of its own to record.
//
// Bad is set for a clause the parser could not parse the header of, case
// or default and its values through the colon, or that it rejected, a
// second default: it reported the error, skipped the header, and parsed
// the statements after it as the clause's body, so the switch's other
// clauses are kept. Its Values is empty, Default is not set, Span covers
// the header skipped, and Body starts with a BadStmt standing for that
// header, so a stage that passes over a body after its first bad
// statement passes over this one too. Whatever the header named is
// unknown: a bad clause may have been any case or the default.
type CaseClause struct {
	Values  []Expr
	Types   []types.Type `json:",omitempty"`
	Var     *Var
	Body    []Stmt
	Span    Span // 'case' or 'default' keyword through the colon, or a bad clause's header
	Default bool
	Bad     bool `json:",omitempty"`
}

// Defer registers Body to run when the block holding the defer is left,
// by any path: falling off its end, or a return, break or continue that
// leaves it. Each block runs the defers it reached in reverse order, and a
// return, break or continue that leaves several blocks runs the innermost
// block's first. A defer that execution never reached does not run. Body
// is evaluated when it runs, not when the defer is reached, so it reads
// variables as they are then. It is always written as a block,
// defer { ... }, and Body holds the block's statements, which may be any
// statements but those leaving it and, at any depth, another defer; the
// parser builds a defer inside a deferred block, and the checker rejects
// it.
type Defer struct {
	Body []Stmt
	Span Span // 'defer' keyword through the deferred block's closing brace
}

var _ Stmt = (*Defer)(nil)

func (d *Defer) stmtKey() stmtKey { return _stmtKey }

func (d *Defer) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the 'defer' keyword through the
// deferred block's closing brace.
func (d *Defer) Spanned() Span { return d.Span }
