package ast

import (
	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/types"
)

type exprKey struct{}

var _exprKey exprKey

// Expr is an expression: anything that can appear where a value is expected.
type Expr interface {
	exprKey() exprKey
	Spanned() Span

	Node
}

// Call is a function call, Fn(Args...), or a conversion, when Fn names a
// type: int8(x) converts x to int8.
type Call struct {
	Fn Expr // the callee: for now always *Ident, later could be anything

	// Const is the value of a call the type checker can compute, a
	// constant.Int: len of an array or a string literal. It is nil for any
	// other call.
	Const any

	Args []Expr
	Span Span // start of Fn through the closing RParen

	// Conv is the type a conversion converts to, set by the resolver when
	// Fn names a type, and Invalid for a call to a function or builtin.
	Conv types.Type
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

// IntLit is an integer literal. A - written right before the digits is part
// of the literal, which is how the smallest int64 is written. Its value is
// exact, anywhere from the smallest int64 to the largest uint64, and it has
// no type of its own until the type checker gives it one from where it is
// used.
type IntLit struct {
	Span  Span
	Value constant.Int // decoded

	// Type is the type the literal is used as, set by the type checker:
	// types.UntypedInt while it is part of a larger constant expression.
	Type types.Type
}

var _ Expr = (*IntLit)(nil)

func (i *IntLit) exprKey() exprKey { return _exprKey }

func (i *IntLit) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the literal.
func (i *IntLit) Spanned() Span { return i.Span }

// CharLit is a character literal, such as 'a' or '\n': one byte, written as
// itself or as an escape sequence. Unlike an integer literal it has a type
// of its own, always uint8, and its value is a constant of that type.
type CharLit struct {
	Span  Span
	Value byte // decoded
}

var _ Expr = (*CharLit)(nil)

func (c *CharLit) exprKey() exprKey { return _exprKey }

func (c *CharLit) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the literal, quotes included.
func (c *CharLit) Spanned() Span { return c.Span }

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

	// Const is the value of a constant expression, made only of literals
	// and operators: a constant.Int or a bool, computed by the type checker.
	// It is nil for any other expression.
	Const any

	Span Span // start of X through the end of Y
	Op   BinaryOp

	// Type is the expression's type, set by the type checker.
	Type types.Type
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
	// Neg is -, which negates an int: -x means 0 - x.
	Neg
)

// String returns the operator as it is written in source.
func (o UnaryOp) String() string {
	switch o {
	case Not:
		return "!"
	case Neg:
		return "-"
	default:
		return "invalid operator"
	}
}

// Unary is a prefix operation, Op X.
type Unary struct {
	X Expr

	// Const is the value of a constant expression, as for Binary.
	Const any

	Span Span // the operator through the end of X
	Op   UnaryOp

	// Type is the expression's type, set by the type checker.
	Type types.Type
}

var _ Expr = (*Unary)(nil)

func (u *Unary) exprKey() exprKey { return _exprKey }

func (u *Unary) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the operator through the end of X.
func (u *Unary) Spanned() Span { return u.Span }

// TypeOf returns the type of an expression, or types.Invalid if it has none
// the compiler can determine. It reads what earlier passes recorded: a
// literal carries its type in its node, or has one fixed type, as a
// character literal is a uint8, an operator expression the type the checker
// gave it, and an identifier its declaration's. An integer literal
// the checker has not reached is types.UntypedInt.
func TypeOf(expr Expr) types.Type {
	switch e := expr.(type) {
	case *StrLit:
		return types.String
	case *IntLit:
		if e.Type == types.Invalid {
			return types.UntypedInt
		}
		return e.Type
	case *CharLit:
		return types.Uint8
	case *BoolLit:
		return types.Bool
	case *Ident:
		return identType(e)
	case *Call:
		return callType(e)
	default:
		return recordedType(expr)
	}
}

// recordedType returns the type the type checker recorded on an operator
// expression, an array literal or an element, or Invalid for any other
// expression.
func recordedType(expr Expr) types.Type {
	switch e := expr.(type) {
	case *Binary:
		return e.Type
	case *Unary:
		return e.Type
	case *CompositeLit:
		return e.Type
	case *Index:
		return e.Type
	default:
		return types.Invalid
	}
}

// identType returns the type of the parameter or variable a name is bound
// to, which is all an expression can name today.
func identType(id *Ident) types.Type {
	switch def := id.Def.(type) {
	case *Param:
		return def.Type
	case *Var:
		return def.Type
	default:
		return types.Invalid
	}
}

// callType returns the type a call yields: the type a conversion converts
// to, or its callee's result, when it
// declares exactly one. A call to a function with several results yields
// them only to a MultiVar, never as one value, so its type here is Invalid,
// as is that of a call to a function or builtin that returns nothing. A
// builtin's Def is a nil *Func, which still satisfies the
// type assertion, so it is checked for explicitly and lands on Invalid.
func callType(call *Call) types.Type {
	if call.Conv != types.Invalid {
		return call.Conv
	}
	if id, ok := call.Fn.(*Ident); ok && id.Name == "len" {
		// The builtin len, the one builtin with a value, since no function
		// can be declared with a builtin's name.
		if fn, ok := id.Def.(*Func); ok && fn == nil {
			return types.Int64
		}
	}
	if id, ok := call.Fn.(*Ident); ok {
		if fn, ok := id.Def.(*Func); ok && fn != nil && len(fn.ResultTypes) == 1 {
			return fn.ResultTypes[0]
		}
	}
	return types.Invalid
}

// ArrayType is an array type as written: [Len]Elem, or [...]Elem in an
// array literal, whose length its elements give. It is an expression only so
// it can stand where a type is written; it never has a value.
type ArrayType struct {
	Len  Expr // the length, a constant expression; nil for [...]
	Elem Expr // the element type as written
	Span Span // the opening bracket through the end of Elem
}

var _ Expr = (*ArrayType)(nil)

func (a *ArrayType) exprKey() exprKey { return _exprKey }

func (a *ArrayType) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the opening bracket through the end
// of the element type.
func (a *ArrayType) Spanned() Span { return a.Span }

// CompositeLit is an array literal, [3]int64{1, 2, 3}, with exactly one
// element for each of the array's. TypeExpr is nil for a literal written
// inside another, as in [2][2]bool{{true, false}, {false, true}}, which
// takes the outer literal's element type.
type CompositeLit struct {
	TypeExpr Expr // the array type as written, nil if left out
	Elems    []Expr
	Span     Span // the type, or the opening brace, through the closing brace

	// Type is the literal's array type, set by the type checker.
	Type types.Type
}

var _ Expr = (*CompositeLit)(nil)

func (c *CompositeLit) exprKey() exprKey { return _exprKey }

func (c *CompositeLit) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the type, or the opening brace,
// through the closing brace.
func (c *CompositeLit) Spanned() Span { return c.Span }

// Index is an element of an array, X[Index]. It is also an assignment's
// target when X is a mutable variable, or an element of one.
type Index struct {
	X     Expr
	Index Expr
	Span  Span // the start of X through the closing bracket

	// Type is the element's type, set by the type checker.
	Type types.Type
}

var _ Expr = (*Index)(nil)

func (i *Index) exprKey() exprKey { return _exprKey }

func (i *Index) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the start of X through the closing
// bracket.
func (i *Index) Spanned() Span { return i.Span }

// Root returns the name a chain of indexes starts from: the variable that
// an assignment to target writes, whether target is the name itself or an
// element of it, a[i][j]. It is nil when the chain starts from anything
// else, such as a call.
func Root(target Expr) *Ident {
	for {
		switch t := target.(type) {
		case *Ident:
			return t
		case *Index:
			target = t.X
		default:
			return nil
		}
	}
}

// IsBlank reports whether e is the name _.
func IsBlank(e Expr) bool {
	id, ok := e.(*Ident)
	return ok && id.Name == Blank
}
