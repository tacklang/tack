package ast

import "github.com/tacklang/tack/types"

type exprKey struct{}

var _exprKey exprKey

// Expr is an expression: anything that can appear where a value is expected.
type Expr interface {
	exprKey() exprKey
	Spanned() Span

	Node
}

// Call is a function call, Fn(Args...).
type Call struct {
	Fn   Expr // the callee: for now always *Ident, later could be anything
	Args []Expr
	Span Span // start of Fn through the closing RParen
}

var _ Expr = (*Call)(nil)

func (c *Call) exprKey() exprKey { return _exprKey }

func (c *Call) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the callee through the closing
// parenthesis.
func (c *Call) Spanned() Span { return c.Span }

// Ident is a use of a name. Def links it to what it names once the resolver
// has run.
type Ident struct {
	Def  Decl // filled in by the resolver, nil before
	Name string
	Span Span
}

var _ Expr = (*Ident)(nil)

func (i *Ident) exprKey() exprKey { return _exprKey }

func (i *Ident) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the name.
func (i *Ident) Spanned() Span { return i.Span }

// StrLit is a string literal.
type StrLit struct {
	Value string // decoded, no quotes
	Span  Span
}

var _ Expr = (*StrLit)(nil)

func (s *StrLit) exprKey() exprKey { return _exprKey }

func (s *StrLit) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the literal, quotes included.
func (s *StrLit) Spanned() Span { return s.Span }

// IntLit is an integer literal.
type IntLit struct {
	Span  Span
	Value int64 // decoded
}

var _ Expr = (*IntLit)(nil)

func (i *IntLit) exprKey() exprKey { return _exprKey }

func (i *IntLit) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the literal.
func (i *IntLit) Spanned() Span { return i.Span }

// BoolLit is the literal true or false.
type BoolLit struct {
	Span  Span
	Value bool
}

var _ Expr = (*BoolLit)(nil)

func (b *BoolLit) exprKey() exprKey { return _exprKey }

func (b *BoolLit) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the keyword.
func (b *BoolLit) Spanned() Span { return b.Span }

// BinaryOp is an infix operator.
type BinaryOp int

const (
	// InvalidOp is the zero value, never produced by the parser.
	InvalidOp BinaryOp = iota

	// The arithmetic operators yield the type of their operands.

	// Add is +.
	Add
	// Sub is -.
	Sub
	// Mul is *.
	Mul
	// Div is /.
	Div
	// Rem is %.
	Rem

	// The comparison operators yield a bool.

	// Eq is ==.
	Eq
	// Ne is !=.
	Ne
	// Lt is <.
	Lt
	// Le is <=.
	Le
	// Gt is >.
	Gt
	// Ge is >=.
	Ge

	// The logical operators take two bools and yield a bool. The right
	// operand is evaluated only when the left does not decide the result.

	// And is &&.
	And
	// Or is ||.
	Or
)

var binaryOpNames = map[BinaryOp]string{
	Add: "+", Sub: "-", Mul: "*", Div: "/", Rem: "%",
	Eq: "==", Ne: "!=", Lt: "<", Le: "<=", Gt: ">", Ge: ">=",
	And: "&&", Or: "||",
}

// String returns the operator as it is written in source.
func (o BinaryOp) String() string {
	if name, ok := binaryOpNames[o]; ok {
		return name
	}
	return "invalid operator"
}

// IsArithmetic reports whether the operator is one of + - * / %, which
// yield the type of their operands.
func (o BinaryOp) IsArithmetic() bool { return o >= Add && o <= Rem }

// IsComparison reports whether the operator is one of == != < <= > >=, which
// compare two operands of one type and yield a bool.
func (o BinaryOp) IsComparison() bool { return o >= Eq && o <= Ge }

// IsLogical reports whether the operator is && or ||.
func (o BinaryOp) IsLogical() bool { return o == And || o == Or }

// Binary is an infix operation, X Op Y.
type Binary struct {
	X, Y Expr
	Span Span // start of X through the end of Y
	Op   BinaryOp
}

var _ Expr = (*Binary)(nil)

func (b *Binary) exprKey() exprKey { return _exprKey }

func (b *Binary) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the start of X through the end of Y.
func (b *Binary) Spanned() Span { return b.Span }

// UnaryOp is a prefix operator.
type UnaryOp int

const (
	// InvalidUnaryOp is the zero value, never produced by the parser.
	InvalidUnaryOp UnaryOp = iota

	// Not is !, which negates a bool.
	Not
)

// String returns the operator as it is written in source.
func (o UnaryOp) String() string {
	if o == Not {
		return "!"
	}
	return "invalid operator"
}

// Unary is a prefix operation, Op X.
type Unary struct {
	X    Expr
	Span Span // the operator through the end of X
	Op   UnaryOp
}

var _ Expr = (*Unary)(nil)

func (u *Unary) exprKey() exprKey { return _exprKey }

func (u *Unary) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the operator through the end of X.
func (u *Unary) Spanned() Span { return u.Span }

// KindOf returns the type of an expression, or types.Invalid if it has none
// the compiler can determine. It reads what earlier passes recorded: a literal
// carries its type in its node, and an identifier carries it through the
// declaration the resolver bound it to.
func KindOf(expr Expr) types.Kind {
	switch e := expr.(type) {
	case *StrLit:
		return types.String
	case *IntLit:
		return types.Int
	case *BoolLit:
		return types.Bool
	case *Ident:
		return identKind(e)
	case *Binary:
		// A comparison or logical operator yields a bool whatever its
		// operands; arithmetic yields the type of its operands, which the
		// checker has already agreed match.
		if !e.Op.IsArithmetic() {
			return types.Bool
		}
		return KindOf(e.X)
	case *Unary:
		return types.Bool
	case *Call:
		return callKind(e)
	default:
		return types.Invalid
	}
}

// identKind returns the type of the parameter or variable a name is bound
// to, which is all an expression can name today.
func identKind(id *Ident) types.Kind {
	switch def := id.Def.(type) {
	case *Param:
		return def.Kind
	case *Var:
		return def.Kind
	default:
		return types.Invalid
	}
}

// callKind returns the type a call yields: whatever its callee returns. A
// builtin returns nothing. Its Def is a nil *Func, which still satisfies the
// type assertion, so it is checked for explicitly and lands on Invalid.
func callKind(call *Call) types.Kind {
	if id, ok := call.Fn.(*Ident); ok {
		if fn, ok := id.Def.(*Func); ok && fn != nil {
			return fn.ResultKind
		}
	}
	return types.Invalid
}
