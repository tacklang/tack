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

	// The comparison operators yield a bool. They are kept last so that
	// IsComparison is a single bound check.

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
)

var binaryOpNames = map[BinaryOp]string{
	Add: "+", Sub: "-", Mul: "*", Div: "/", Rem: "%",
	Eq: "==", Ne: "!=", Lt: "<", Le: "<=", Gt: ">", Ge: ">=",
}

// String returns the operator as it is written in source.
func (o BinaryOp) String() string {
	if name, ok := binaryOpNames[o]; ok {
		return name
	}
	return "invalid operator"
}

// IsComparison reports whether the operator yields a bool rather than the type
// of its operands.
func (o BinaryOp) IsComparison() bool { return o >= Eq }

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
	case *Ident:
		// A parameter is the only thing an expression can name today.
		if param, ok := e.Def.(*Param); ok {
			return param.Kind
		}
		return types.Invalid
	case *Binary:
		// A comparison yields a bool whatever it compared; arithmetic yields
		// the type of its operands, which the checker has already agreed match.
		if e.Op.IsComparison() {
			return types.Bool
		}
		return KindOf(e.X)
	case *Call:
		return callKind(e)
	default:
		return types.Invalid
	}
}

// callKind returns the type a call yields: whatever its callee returns. A
// builtin returns nothing, and its Def is nil, so it lands on Invalid.
func callKind(call *Call) types.Kind {
	if id, ok := call.Fn.(*Ident); ok {
		if fn, ok := id.Def.(*Func); ok {
			return fn.ResultKind
		}
	}
	return types.Invalid
}
