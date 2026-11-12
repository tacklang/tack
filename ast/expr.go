package ast

import (
	"encoding/json"
	"fmt"

	"github.com/tacklang/tack/constant"
	"github.com/tacklang/tack/types"
)

// exprKey is the type exprKey returns, the method that seals Expr: an empty
// struct, of no use but as the marker's result, whose one value is _exprKey.
type exprKey struct{}

var _exprKey exprKey

// Expr is an expression: anything that can appear where a value is expected.
type Expr interface {
	exprKey() exprKey
	Spanned() Span

	Node
}

// Call is a function call, Fn(Args...), or a conversion, when Fn names a
// type: int8(x) converts x to int8, and []uint8(s) views the bytes of the
// string s. A call written f(a, v...) spreads its last argument, v, into
// f's variadic parameter.
//
// A method call, x.m(Args...), has a *Selector as Fn, whose X is the
// receiver, x, and whose Name is the method's: the receiver is passed
// first, before Args, as a method's receiver is its first parameter. A
// call to a mut receiver, mut x.m(Args...), has a *MutArg as the
// selector's X, mut x, so the receiver is passed as a mut argument is.
// The type checker finds the method from the receiver's type and records
// it in Method.
type Call struct {
	Fn Expr // the callee: an *Ident, a *ViewType for a view conversion, or a *Selector for a method call

	// Method is the method a method call calls, set by the type checker;
	// it is nil for any other call, and for a method call it could not
	// check. It is left out of the JSON dump: a method's body can call it.
	Method *Func `json:"-"`

	// Dynamic is the stand-in the type checker makes for a call through a
	// function value, f(x) where Fn names a variable or a parameter of a
	// function type: a Func with no body, never declared, whose
	// parameters and results are the type's, which the call is checked
	// against, and whose Value is that type. It is nil for any other
	// call, and for a call through a value the checker reported. Which
	// function runs is known only when the call is made.
	Dynamic *Func `json:"-"`

	// Const is the value of a call the type checker can compute, a
	// constant.Int: len of an array or a string literal. It is nil for any
	// other call.
	Const any

	Args []Expr

	// TypeArgs holds the type arguments of a call to a generic function,
	// one for each of its type parameters, in order, as the type checker
	// inferred them from the arguments: struct types, or, in the body of
	// another generic function, that function's type parameters. It is
	// nil for any other call, and for one whose inference was reported.
	TypeArgs []types.Type `json:",omitempty"`

	// ParamTypes and ResultTypes are the types of a generic callee's
	// parameters and results at this call, with each of its type
	// parameters replaced by the call's type argument, through every list,
	// view, map and function type made of one, as the type checker found
	// them once it inferred every type argument. ResultTypes also holds
	// the two results of a call to add, add_all or insert, the id, span
	// or handle it gives and an error, the only builtins that give two
	// and an error. Both are nil
	// for any other call, and for one whose inference was reported.
	ParamTypes  []types.Type `json:",omitempty"`
	ResultTypes []types.Type `json:",omitempty"`

	Span Span // start of Fn through the closing RParen

	// EllipsisSpan is the ... after the last argument, f(v...), a spread,
	// which passes the view v on as the whole of a variadic parameter;
	// Spread is set when it is written.
	EllipsisSpan Span `json:"-"`
	Spread       bool `json:",omitempty"`

	// Conv is the type a conversion converts to, set by the resolver when
	// Fn names a basic type, and by the type checker when Fn is a view
	// type, and Invalid for a call to a function or builtin.
	Conv types.Type

	// Type is the type of the value of a call to make or append, the
	// generic builtins with a value other than len, set by the type
	// checker: make's list type, and append's error. It is Invalid for
	// any other call, whose type its callee gives.
	Type types.Type `json:",omitempty"`
}

var _ Expr = (*Call)(nil)

// exprKey marks Call as an Expr, sealing Expr to this package's types.
func (c *Call) exprKey() exprKey { return _exprKey }

// astNode marks Call as a Node, sealing Node to this package's types.
func (c *Call) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the callee through the closing
// parenthesis.
func (c *Call) Spanned() Span { return c.Span }

// Receiver returns what a method call is called on: x in x.m(...), or the
// *MutArg mut x in mut x.m(...). It returns nil for a call that is not a
// method call, a union's construction, Expr.int(5), and a call of a
// method expression, Pair.first(p), among them, whose selector's operand
// names a type (see TypeOperand): such a call passes its receiver as the
// first of its Args.
func (c *Call) Receiver() Expr {
	if sel, ok := c.Fn.(*Selector); ok {
		if TypeOperand(sel.X) {
			return nil
		}
		return sel.X
	}
	return nil
}

// Operands returns every value the call passes, in the order they are
// evaluated and passed: a method call's receiver first, then Args. For
// any other call it is Args.
func (c *Call) Operands() []Expr {
	recv := c.Receiver()
	if recv == nil {
		return c.Args
	}
	return append([]Expr{recv}, c.Args...)
}

// Callee returns the function call calls: the function its name is bound
// to, the method the type checker found for a method call, or the
// stand-in it made for a call through a function value (see
// Call.Dynamic). It returns nil for a builtin, whose declaration is a nil
// *Func, for a conversion, and for a method call or a call through a
// value the type checker has not checked.
func Callee(call *Call) *Func {
	if call.Method != nil {
		return call.Method
	}
	if call.Dynamic != nil {
		return call.Dynamic
	}
	if id, ok := call.Fn.(*Ident); ok {
		if fn, ok := id.Def.(*Func); ok && fn != nil {
			return fn
		}
	}
	return nil
}

// Ident is a use of a name. Def links it to what it names once the resolver
// has run. A qualified name, x.name where x is an import's name, is one
// Ident too: Pkg is x, PkgSpan where x is written, Name is name, and Span
// covers all of it; Def is the declaration of name in the imported
// package.
type Ident struct {
	Def Decl // filled in by the resolver, nil before

	// Hidden is a variable or a parameter named like a called function or
	// builtin, visible where the call is written, which the resolver
	// passed over to bind the call to the function: a call by a bare name
	// calls the function of that name before any local. The type checker
	// reports the call when Hidden holds a function value, which the call
	// could be meant to call. It is nil for any other name.
	Hidden Decl `json:"-"`

	Pkg     string
	Name    string
	PkgSpan Span
	Span    Span

	// Move is set by the type checker on a name whose use moves the value
	// of the variable it names, a list or a struct holding one, out of it:
	// the whole value of a declaration, an assignment, a return or a
	// struct literal's field. The variable cannot be used again until it
	// is assigned (see typeck), and code generation empties it.
	Move bool `json:",omitempty"`
}

// Qualified returns the name as written: pkg.name for a qualified one, the
// name alone otherwise.
func (i *Ident) Qualified() string {
	if i.Pkg == "" {
		return i.Name
	}
	return i.Pkg + "." + i.Name
}

var _ Expr = (*Ident)(nil)

// exprKey marks Ident as an Expr, sealing Expr to this package's types.
func (i *Ident) exprKey() exprKey { return _exprKey }

// astNode marks Ident as a Node, sealing Node to this package's types.
func (i *Ident) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the name.
func (i *Ident) Spanned() Span { return i.Span }

// DeclRef is how an Ident's Def appears in the JSON dump: the kind of
// declaration and where its name is written, enough to find it in the tree
// without copying it. Following Def instead would never end for a recursive
// function, whose body names the function itself.
type DeclRef struct {
	Kind     string // "Func", "Param", "Var", "Type", "TypeParam", or "Import"
	Name     string
	NameSpan Span
}

// MarshalJSON renders the identifier with Def as a *DeclRef. A builtin's Def,
// a nil *Func, and an unresolved one, nil, both render as null. Pkg and
// PkgSpan are left out of a name that is not qualified.
func (i *Ident) MarshalJSON() ([]byte, error) {
	// plain has Ident's fields but not this method, so marshaling it does
	// not recurse. The outer fields, being shallower, hide plain's.
	type plain Ident
	var pkgSpan *Span
	if i.Pkg != "" {
		pkgSpan = &i.PkgSpan
	}
	js, err := json.Marshal(struct {
		Def     *DeclRef
		PkgSpan *Span `json:",omitempty"`
		*plain
		Pkg string `json:",omitempty"`
	}{declRef(i.Def), pkgSpan, (*plain)(i), i.Pkg})
	if err != nil {
		return nil, fmt.Errorf("marshaling identifier %s: %w", i.Name, err)
	}
	return js, nil
}

// declRef returns the reference that stands for d in the JSON dump, or nil
// when d is nil or a nil pointer.
func declRef(d Decl) *DeclRef {
	switch d := d.(type) {
	case *Func:
		if d != nil {
			return &DeclRef{Kind: "Func", Name: d.Name, NameSpan: d.NameSpan}
		}
	case *Param:
		if d != nil {
			return &DeclRef{Kind: "Param", Name: d.Name, NameSpan: d.NameSpan}
		}
	case *Var:
		if d != nil {
			return &DeclRef{Kind: "Var", Name: d.Name, NameSpan: d.NameSpan}
		}
	case *TypeParam:
		if d != nil {
			return &DeclRef{Kind: "TypeParam", Name: d.Name, NameSpan: d.NameSpan}
		}
	default:
		return packageDeclRef(d)
	}
	return nil
}

// packageDeclRef is declRef for the declarations only found at package
// level, types and imports.
func packageDeclRef(d Decl) *DeclRef {
	switch d := d.(type) {
	case *TypeDecl:
		if d != nil {
			return &DeclRef{Kind: "Type", Name: d.Name, NameSpan: d.NameSpan}
		}
	case *Import:
		if d != nil {
			return &DeclRef{Kind: "Import", Name: d.Name, NameSpan: d.NameSpan}
		}
	}
	return nil
}

// StrLit is a string literal.
type StrLit struct {
	Value string // decoded, no quotes
	Span  Span
}

var _ Expr = (*StrLit)(nil)

// exprKey marks StrLit as an Expr, sealing Expr to this package's types.
func (s *StrLit) exprKey() exprKey { return _exprKey }

// astNode marks StrLit as a Node, sealing Node to this package's types.
func (s *StrLit) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the literal, quotes included.
func (s *StrLit) Spanned() Span { return s.Span }

// FloatLit is a float literal, 1.5, .5, 1., 1e9 or 1.5e-3. Its value is
// exact, as an untyped float constant's is (see constant.Float), and it
// has no type of its own until the type checker gives it one from where
// it is used. A - before it is a Unary, negated exactly as a constant.
type FloatLit struct {
	Value constant.Float // decoded
	Span  Span

	// Type is the type the literal is used as, set by the type checker:
	// types.UntypedFloat while it is part of a larger constant expression.
	Type types.Type
}

var _ Expr = (*FloatLit)(nil)

// exprKey marks FloatLit as an Expr, sealing Expr to this package's types.
func (f *FloatLit) exprKey() exprKey { return _exprKey }

// astNode marks FloatLit as a Node, sealing Node to this package's types.
func (f *FloatLit) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the literal.
func (f *FloatLit) Spanned() Span { return f.Span }

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

// exprKey marks IntLit as an Expr, sealing Expr to this package's types.
func (i *IntLit) exprKey() exprKey { return _exprKey }

// astNode marks IntLit as a Node, sealing Node to this package's types.
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

// exprKey marks CharLit as an Expr, sealing Expr to this package's types.
func (c *CharLit) exprKey() exprKey { return _exprKey }

// astNode marks CharLit as a Node, sealing Node to this package's types.
func (c *CharLit) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the literal, quotes included.
func (c *CharLit) Spanned() Span { return c.Span }

// BoolLit is the literal true or false.
type BoolLit struct {
	Span  Span
	Value bool
}

var _ Expr = (*BoolLit)(nil)

// exprKey marks BoolLit as an Expr, sealing Expr to this package's types.
func (b *BoolLit) exprKey() exprKey { return _exprKey }

// astNode marks BoolLit as a Node, sealing Node to this package's types.
func (b *BoolLit) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the keyword.
func (b *BoolLit) Spanned() Span { return b.Span }

// NoErrorLit is the literal noerror: the error value that is no error,
// with code 0 and an empty message, the zero value of type error.
type NoErrorLit struct {
	Span Span
}

var _ Expr = (*NoErrorLit)(nil)

// exprKey marks NoErrorLit as an Expr, sealing Expr to this package's types.
func (n *NoErrorLit) exprKey() exprKey { return _exprKey }

// astNode marks NoErrorLit as a Node, sealing Node to this package's types.
func (n *NoErrorLit) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the keyword.
func (n *NoErrorLit) Spanned() Span { return n.Span }

// CAllocatorLit is the literal c_allocator: the allocator, of type
// Allocator, that lowers to C's malloc, realloc and free.
type CAllocatorLit struct {
	Span Span
}

var _ Expr = (*CAllocatorLit)(nil)

// exprKey marks CAllocatorLit as an Expr, sealing Expr to this package's types.
func (c *CAllocatorLit) exprKey() exprKey { return _exprKey }

// astNode marks CAllocatorLit as a Node, sealing Node to this package's types.
func (c *CAllocatorLit) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the keyword.
func (c *CAllocatorLit) Spanned() Span { return c.Span }

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

	// The bitwise operators take two integers of one type and yield that
	// type, as the arithmetic operators do.

	// BitAnd is &.
	BitAnd
	// BitOr is |.
	BitOr
	// BitXor is ^.
	BitXor
	// AndNot is &^, a & ^b.
	AndNot

	// The shift operators take an integer and an unsigned count and yield
	// the integer's type.

	// Shl is <<.
	Shl
	// Shr is >>.
	Shr

	// The wrapping operators take two integers and yield their common
	// type, as + - and * do, but wrap around at the type's width, on the
	// two's complement form of a signed type, instead of needing a proof
	// that the result fits.

	// AddWrap is +%.
	AddWrap
	// SubWrap is -%.
	SubWrap
	// MulWrap is *%.
	MulWrap
)

var binaryOpNames = map[BinaryOp]string{
	Add: "+", Sub: "-", Mul: "*", Div: "/", Rem: "%",
	Eq: "==", Ne: "!=", Lt: "<", Le: "<=", Gt: ">", Ge: ">=",
	And: "&&", Or: "||",
	BitAnd: "&", BitOr: "|", BitXor: "^", AndNot: "&^", Shl: "<<", Shr: ">>",
	AddWrap: "+%", SubWrap: "-%", MulWrap: "*%",
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

// IsBitwise reports whether the operator is one of & | ^ &^, which take two
// integers of one type and yield it.
func (o BinaryOp) IsBitwise() bool { return o >= BitAnd && o <= AndNot }

// IsShift reports whether the operator is << or >>, which shift an integer
// by an unsigned count and yield the integer's type.
func (o BinaryOp) IsShift() bool { return o == Shl || o == Shr }

// IsWrapping reports whether the operator is one of +% -% *%, which wrap
// at their type's width.
func (o BinaryOp) IsWrapping() bool { return o >= AddWrap && o <= MulWrap }

// Unwrapped returns the operator a wrapping one wraps: + for +%, - for -%
// and * for *%. Any other operator is returned as it is.
func (o BinaryOp) Unwrapped() BinaryOp {
	switch o {
	case AddWrap:
		return Add
	case SubWrap:
		return Sub
	case MulWrap:
		return Mul
	default:
		return o
	}
}

// IsInteger reports whether the operator works on integers and yields one:
// an arithmetic, a bitwise, a shift or a wrapping operator.
func (o BinaryOp) IsInteger() bool {
	return o.IsArithmetic() || o.IsBitwise() || o.IsShift() || o.IsWrapping()
}

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

// exprKey marks Binary as an Expr, sealing Expr to this package's types.
func (b *Binary) exprKey() exprKey { return _exprKey }

// astNode marks Binary as a Node, sealing Node to this package's types.
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
	// Complement is ^, which flips every bit of an int: ^x means x ^ m,
	// with m all ones for an unsigned type and -1 for a signed one.
	Complement
	// NegWrap is -%, which negates an int wrapping at its type's width:
	// -%x means 0 -% x, on a signed type or an unsigned one.
	NegWrap
)

// String returns the operator as it is written in source.
func (o UnaryOp) String() string {
	switch o {
	case Not:
		return "!"
	case Neg:
		return "-"
	case Complement:
		return "^"
	case NegWrap:
		return "-%"
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

// exprKey marks Unary as an Expr, sealing Expr to this package's types.
func (u *Unary) exprKey() exprKey { return _exprKey }

// astNode marks Unary as a Node, sealing Node to this package's types.
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
	if typ, ok := literalType(expr); ok {
		return typ
	}
	switch e := expr.(type) {
	case *Ident:
		return identType(e)
	case *Call:
		return callType(e)
	default:
		return recordedType(expr)
	}
}

// literalType returns the type of expr when it is a literal, and whether
// it is one: an integer or a float literal's recorded type, or untyped
// before the checker reaches it, and every other literal's fixed type.
func literalType(expr Expr) (types.Type, bool) {
	switch e := expr.(type) {
	case *StrLit:
		return types.String, true
	case *IntLit:
		return orUntyped(e.Type, types.UntypedInt), true
	case *FloatLit:
		return orUntyped(e.Type, types.UntypedFloat), true
	case *CharLit:
		return types.Uint8, true
	case *BoolLit:
		return types.Bool, true
	case *NoErrorLit:
		return types.Error, true
	case *CAllocatorLit:
		return types.Allocator, true
	default:
		return types.Invalid, false
	}
}

// orUntyped returns recorded, the type the checker recorded on a literal,
// or untyped, its untyped type, before the checker has reached it.
func orUntyped(recorded, untyped types.Type) types.Type {
	if recorded == types.Invalid {
		return untyped
	}
	return recorded
}

// recordedType returns the type the type checker recorded on an operator
// expression, a composite literal, an element, a slice, a field or a type
// assertion, the type it asserts, the one value a ? gives, the type of
// the place a mut argument names or of the variable an own argument
// names, a closure's function type, or Invalid for any other expression.
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
	case *Slice:
		return e.Type
	case *Selector:
		return e.Type
	case *MutArg, *OwnArg:
		return TypeOf(ArgOperand(e))
	case *TypeAssert:
		return e.Type
	case *Question:
		return e.Type
	case *FuncLit:
		return e.Type
	default:
		return types.Invalid
	}
}

// identType returns the type of the parameter or variable a name is bound
// to, or of the function it names, used as a value, as the type checker
// recorded it (see Func.Value). A name bound to a type has no value, and
// a builtin's Def is a nil *Func, so the type of either is Invalid.
func identType(id *Ident) types.Type {
	switch def := id.Def.(type) {
	case *Param:
		return def.Type
	case *Var:
		return def.Type
	case *Func:
		if def == nil {
			return types.Invalid
		}
		return def.Value
	default:
		return types.Invalid
	}
}

// callType returns the type a call yields: the type a conversion converts
// to, the type the checker recorded for a call to make or append, or its
// callee's result, a function's or the method the checker found, when it
// declares exactly one, a generic callee's type parameter replaced by the
// call's type argument (see CallResults). A call to a function with several results yields
// them only to a MultiVar, never as one value, so its type here is Invalid,
// as is that of a call to a function or builtin that returns nothing. A
// builtin's Def is a nil *Func, which still satisfies the
// type assertion, so it is checked for explicitly and lands on Invalid.
func callType(call *Call) types.Type {
	if typ := max(call.Conv, call.Type); typ != types.Invalid {
		// A conversion records Conv, and make or append Type; no call
		// records both.
		return typ
	}
	if id, ok := call.Fn.(*Ident); ok && id.Name == "len" {
		// The builtin len, the one builtin with a value, since no function
		// can be declared with a builtin's name.
		if fn, ok := id.Def.(*Func); ok && fn == nil {
			return types.Int64
		}
	}
	if results := CallResults(call); len(results) == 1 {
		return results[0]
	}
	return types.Invalid
}

// TypeAssert asks which type the value of X, an any, holds: X.(T), whose
// TypeExpr is T as written, or X.(type), whose TypeExpr is nil. X.(T) is
// only ever bound with ok, v, ok := X.(T), giving the value X holds when
// its type is exactly T, and T's zero value otherwise, and whether it was
// T. X.(type) is only ever the Tag of a type switch (see Switch). The
// parser takes either wherever a field may follow an operand; the type
// checker reports one anywhere else.
type TypeAssert struct {
	X        Expr
	TypeExpr Expr // the type as written; nil for X.(type)
	Span     Span // the start of X through the closing parenthesis

	// Type is the type X.(T) asserts, T, set by the type checker; it is
	// Invalid for X.(type).
	Type types.Type
}

var _ Expr = (*TypeAssert)(nil)

// exprKey marks TypeAssert as an Expr, sealing Expr to this package's types.
func (t *TypeAssert) exprKey() exprKey { return _exprKey }

// astNode marks TypeAssert as a Node, sealing Node to this package's types.
func (t *TypeAssert) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the start of X through the
// closing parenthesis.
func (t *TypeAssert) Spanned() Span { return t.Span }

// ArrayType is an array type as written: [Len]Elem, or [...]Elem in an
// array literal, whose length its elements give. It is an expression only so
// it can stand where a type is written; it never has a value.
type ArrayType struct {
	Len  Expr // the length, a constant expression; nil for [...]
	Elem Expr // the element type as written
	Span Span // the opening bracket through the end of Elem
}

var _ Expr = (*ArrayType)(nil)

// exprKey marks ArrayType as an Expr, sealing Expr to this package's types.
func (a *ArrayType) exprKey() exprKey { return _exprKey }

// astNode marks ArrayType as a Node, sealing Node to this package's types.
func (a *ArrayType) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the opening bracket through the end
// of the element type.
func (a *ArrayType) Spanned() Span { return a.Span }

// ViewType is a view type as written, []Elem: a read-only view of elements
// of type Elem, which only a parameter or a function's only result may
// have. Like ArrayType it is an
// expression only so it can stand where a type is written, and as the
// callee of a conversion, []uint8(s), whose Call has it as Fn; it never
// has a value of its own.
type ViewType struct {
	Elem Expr // the element type as written
	Span Span // the opening bracket through the end of Elem
}

var _ Expr = (*ViewType)(nil)

// exprKey marks ViewType as an Expr, sealing Expr to this package's types.
func (v *ViewType) exprKey() exprKey { return _exprKey }

// astNode marks ViewType as a Node, sealing Node to this package's types.
func (v *ViewType) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the opening bracket through the
// end of the element type.
func (v *ViewType) Spanned() Span { return v.Span }

// ListType is a list type as written, list[Elem]: an owned, growable list
// of elements of type Elem. Like ArrayType it is an expression only so it
// can stand where a type is written, and as make's first argument,
// make(list[int64], c_allocator), which takes a type; it never has a
// value of its own.
type ListType struct {
	Elem Expr // the element type as written
	Span Span // the keyword list through the closing bracket
}

var _ Expr = (*ListType)(nil)

// exprKey marks ListType as an Expr, sealing Expr to this package's types.
func (l *ListType) exprKey() exprKey { return _exprKey }

// astNode marks ListType as a Node, sealing Node to this package's types.
func (l *ListType) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the keyword list through the
// closing bracket.
func (l *ListType) Spanned() Span { return l.Span }

// ArenaType is an arena type as written, arena[Elem]: an owned,
// append-only store of elements of type Elem, indexed by ids. Like
// ListType it is an expression only so it can stand where a type is
// written, and as make's first argument, make(arena[int64], 0,
// c_allocator); it never has a value of its own.
type ArenaType struct {
	Elem Expr // the element type as written
	Span Span // the keyword arena through the closing bracket
}

var _ Expr = (*ArenaType)(nil)

// exprKey marks ArenaType as an Expr, sealing Expr to this package's types.
func (a *ArenaType) exprKey() exprKey { return _exprKey }

// astNode marks ArenaType as a Node, sealing Node to this package's types.
func (a *ArenaType) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the keyword arena through the
// closing bracket.
func (a *ArenaType) Spanned() Span { return a.Span }

// PoolType is a pool type as written, pool[Elem]: an owned store of
// elements of type Elem that can be removed one at a time, named by
// handles. Like ArenaType it is an expression only so it can stand where
// a type is written, and as make's first argument, make(pool[int64],
// c_allocator); it never has a value of its own.
type PoolType struct {
	Elem Expr // the element type as written
	Span Span // the keyword pool through the closing bracket
}

var _ Expr = (*PoolType)(nil)

// exprKey marks PoolType as an Expr, sealing Expr to this package's types.
func (p *PoolType) exprKey() exprKey { return _exprKey }

// astNode marks PoolType as a Node, sealing Node to this package's types.
func (p *PoolType) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the keyword pool through the
// closing bracket.
func (p *PoolType) Spanned() Span { return p.Span }

// RefType is a type naming elements of an arena or a pool, as written:
// id[Elem], one element of an arena[Elem], span[Elem], a run of them, or
// handle[Elem], one element of a pool[Elem]. Name is "id", "span" or
// "handle", a builtin type's name, which is reserved, so a name followed
// by a bracket is always one of these, as a type and as an operand, where
// a conversion to one, id[int64](n), calls it. It never has a value of
// its own.
type RefType struct {
	Name     string // "id", "span" or "handle"
	Elem     Expr   // the element type as written
	NameSpan Span   // the name alone
	Span     Span   // the name through the closing bracket
}

var _ Expr = (*RefType)(nil)

// exprKey marks RefType as an Expr, sealing Expr to this package's types.
func (r *RefType) exprKey() exprKey { return _exprKey }

// astNode marks RefType as a Node, sealing Node to this package's types.
func (r *RefType) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the name through the closing
// bracket.
func (r *RefType) Spanned() Span { return r.Span }

// MapType is a map type as written, map[Key]Value: an owned map holding
// values of type Value by keys of type Key. Like ListType it is an
// expression only so it can stand where a type is written, and as make's
// first argument, make(map[string]int64, c_allocator); it never has a
// value of its own.
type MapType struct {
	Key   Expr // the key type as written
	Value Expr // the value type as written
	Span  Span // the keyword map through the end of the value type
}

var _ Expr = (*MapType)(nil)

// exprKey marks MapType as an Expr, sealing Expr to this package's types.
func (m *MapType) exprKey() exprKey { return _exprKey }

// astNode marks MapType as a Node, sealing Node to this package's types.
func (m *MapType) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the keyword map through the end
// of the value type.
func (m *MapType) Spanned() Span { return m.Span }

// FuncType is a function type as written, func(A, mut B, own C) R: the
// type of a value naming a function whose parameters have Params' types,
// in order, each mut or own where it is written so, and whose results are
// Results, none, one alone, or several in parentheses, func(A) (R, error).
// Each of Params has no name, only its TypeExpr, Mut and MutSpan, Own and
// OwnSpan, and Span, which covers mut or own and the type. Like MapType it is an expression only so it can stand where
// a type is written; it never has a value of its own.
type FuncType struct {
	Params  []*Param
	Results []Expr // the result types as written, empty if none
	Span    Span   // the keyword func through the end of the last result, or the closing parenthesis
}

var _ Expr = (*FuncType)(nil)

// exprKey marks FuncType as an Expr, sealing Expr to this package's types.
func (f *FuncType) exprKey() exprKey { return _exprKey }

// astNode marks FuncType as a Node, sealing Node to this package's types.
func (f *FuncType) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the keyword func through the end
// of the type.
func (f *FuncType) Spanned() Span { return f.Span }

// CompositeLit is an array literal, [3]int64{1, 2, 3}, with exactly one
// element for each of the array's, or a struct literal, Point{x: 1, y: 2},
// whose elements are each a *KeyValue naming a field. TypeExpr is nil for a
// literal written inside an array literal, as in
// [2][2]bool{{true, false}, {false, true}}, which takes the outer literal's
// element type. The parser accepts a key on any element and an element
// without one in any literal; which literal may have which is the type
// checker's to judge.
type CompositeLit struct {
	TypeExpr Expr // the type as written, nil if left out
	Elems    []Expr
	Span     Span // the type, or the opening brace, through the closing brace

	// Type is the literal's array or struct type, set by the type checker.
	Type types.Type
}

var _ Expr = (*CompositeLit)(nil)

// exprKey marks CompositeLit as an Expr, sealing Expr to this package's types.
func (c *CompositeLit) exprKey() exprKey { return _exprKey }

// astNode marks CompositeLit as a Node, sealing Node to this package's types.
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

// exprKey marks Index as an Expr, sealing Expr to this package's types.
func (i *Index) exprKey() exprKey { return _exprKey }

// astNode marks Index as a Node, sealing Node to this package's types.
func (i *Index) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the start of X through the closing
// bracket.
func (i *Index) Spanned() Span { return i.Span }

// Slice is a slice of a string, an array or a view, X[Lo:Hi]: the bytes or
// elements of X from Lo up to, but not including, Hi. Lo is nil when it is
// left out, X[:Hi], and means 0, and Hi is nil when it is left out, X[Lo:],
// and means len(X). A slice is a value, never a place, so it is never an
// assignment's target or a mut argument; the parser takes it wherever an
// index may be written, and the type checker reports it anywhere else.
type Slice struct {
	X         Expr
	Lo        Expr // nil for X[:Hi]
	Hi        Expr // nil for X[Lo:]
	ColonSpan Span // the colon between the bounds
	Span      Span // the start of X through the closing bracket

	// Type is the slice's type, set by the type checker: a string for a
	// string's slice, and a view of the elements for an array's or a
	// view's.
	Type types.Type
}

var _ Expr = (*Slice)(nil)

// exprKey marks Slice as an Expr, sealing Expr to this package's types.
func (s *Slice) exprKey() exprKey { return _exprKey }

// astNode marks Slice as a Node, sealing Node to this package's types.
func (s *Slice) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the start of X through the closing
// bracket.
func (s *Slice) Spanned() Span { return s.Span }

// Bounds returns the source range of the slice's bounds as written: from
// the start of Lo, or the colon when Lo is left out, through the end of
// Hi, or the colon when Hi is left out.
func (s *Slice) Bounds() Span {
	span := s.ColonSpan
	if s.Lo != nil {
		span.Start = s.Lo.Spanned().Start
	}
	if s.Hi != nil {
		span.End = s.Hi.Spanned().End
	}
	return span
}

// Root returns the name a chain of indexes and field selections starts
// from: the variable that an assignment to target writes, whether target is
// the name itself, an element of it, a[i][j], or a field, p.a[i].x. It is
// nil when the chain starts from anything else, such as a call.
func Root(target Expr) *Ident {
	for {
		switch t := target.(type) {
		case *Ident:
			return t
		case *Index:
			target = t.X
		case *Selector:
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

// StructType is a struct type as written, struct { x int64; y int64 }: its
// fields in the order they are declared, each with a name and a type. It
// appears only in a TypeDecl; anonymous struct types are not supported.
type StructType struct {
	Fields []*Field
	Span   Span // the 'struct' keyword through the closing brace
}

var _ Expr = (*StructType)(nil)

// exprKey marks StructType as an Expr, sealing Expr to this package's types.
func (s *StructType) exprKey() exprKey { return _exprKey }

// astNode marks StructType as a Node, sealing Node to this package's types.
func (s *StructType) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the 'struct' keyword through the
// closing brace.
func (s *StructType) Spanned() Span { return s.Span }

// Field is one field of a struct type: a name and the field's type as
// written. It is part of its StructType rather than a node of its own; Walk
// visits its type.
type Field struct {
	TypeExpr Expr
	Name     string
	NameSpan Span // just the identifier
	Span     Span // pub or lib, if written, or the name, through the end of the type

	// Vis is who may use the field by name outside its package, capped by
	// its struct's visibility; VisSpan is the keyword that gave it, the
	// zero Span for Private.
	VisSpan Span       `json:"-"`
	Vis     Visibility `json:",omitempty"`
}

// Selector is a field of a struct value, X.Name. It is also an assignment's
// target when X is a mutable variable, or an element or field of one. The
// name is a field's, which the type checker finds from X's type, so it is
// not an Ident and the resolver never binds it. As a Call's Fn it names a
// method of X's type instead, whose receiver X is (see Call), and has no
// Type of its own. When X names a union type, Expr.eof, or Expr.int as a
// Call's Fn, it names one of the union's cases, and builds a value of it
// (see Construction); Type is then the union's. When X names a type and
// Name is no case of it, Pair.first or lexer.Token.Kind, the selector is
// a method expression (see MethodExpr): the method as a function value,
// whose Type is its function type and Method the method, or, as a Call's
// Fn, the method called with its receiver as the first argument.
//
// X.[f], written in a fields loop's body, is the field f of X, where f is
// the loop's variable (see FieldsOf): Field is then the name f, which the
// resolver binds to that variable, Name is "[f]", which no field's name
// can be, so that a pass comparing fields by name never takes it for a
// named one, and NameSpan spans the brackets and the name. Type is the
// type parameter of the loop's FieldsOf.Field.
type Selector struct {
	X        Expr
	Name     string
	NameSpan Span // just the field name, or the brackets and the name of X.[f]
	Span     Span // the start of X through the field name, or the closing bracket

	// Field is f in X.[f], and nil for a field named as written.
	Field *Ident `json:",omitempty"`

	// Type is the field's type, set by the type checker, or a method
	// expression's function type.
	Type types.Type

	// Method is the method a method expression names, Pair.first used as
	// a value (see MethodExpr), set by the type checker; it is nil for any
	// other selector, and for a method expression it reported. It is left
	// out of the JSON dump, as Call.Method is.
	Method *Func `json:"-"`
}

var _ Expr = (*Selector)(nil)

// exprKey marks Selector as an Expr, sealing Expr to this package's types.
func (s *Selector) exprKey() exprKey { return _exprKey }

// astNode marks Selector as a Node, sealing Node to this package's types.
func (s *Selector) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the start of X through the field
// name.
func (s *Selector) Spanned() Span { return s.Span }

// Question is the postfix ? operator, X?, which passes X's error on. X
// is the call, written in parentheses or not, the parser found before the
// ?; the type checker reports any X that is not a call whose last result
// is an error, or a ? outside a function whose last result is one. When
// the call's error is set, its code not 0, the function holding the ?
// returns it, unwrapped, as a return there would; otherwise the ? gives
// the call's other results: one is its value, Type, none leave it a
// statement, and several are bound together, a, b := f()?. Results gives
// them all, whatever their count.
type Question struct {
	X    Expr
	Span Span // the start of X through the ?

	// Type is the type of the value X? gives, set by the type checker:
	// the call's one result before its error, or Invalid when the call
	// has no other result, or several.
	Type types.Type

	// Results are the call's results before its error, in order, set by
	// the type checker, a generic callee's with its type arguments in
	// place; nil until it has checked the ?, and when it gives none.
	Results []types.Type `json:",omitempty"`
}

var _ Expr = (*Question)(nil)

// exprKey marks Question as an Expr, sealing Expr to this package's types.
func (q *Question) exprKey() exprKey { return _exprKey }

// astNode marks Question as a Node, sealing Node to this package's types.
func (q *Question) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the start of X through the ?.
func (q *Question) Spanned() Span { return q.Span }

// Mark returns where the ? itself is written, the last byte of its span.
func (q *Question) Mark() Span {
	return Span{File: q.Span.File, Start: q.Span.End - 1, End: q.Span.End}
}

// Questioned returns the call X? passes the error of, when X is one, or
// nil.
func Questioned(q *Question) *Call {
	call, _ := q.X.(*Call)
	return call
}

// KeyValue is an element of a composite literal written with a key,
// Key: Value. In a struct literal the key is an *Ident naming a field,
// which the resolver leaves unbound, since a field's name is not looked up
// among variables. It has no value of its own. Parentheses leave no node
// behind, so KeyParens records a key written in them, (x): v, which the
// type checker rejects.
type KeyValue struct {
	Key       Expr
	Value     Expr
	Span      Span // the start of Key, its parentheses included, through the end of Value
	KeyParens bool // the key was written in parentheses
}

var _ Expr = (*KeyValue)(nil)

// exprKey marks KeyValue as an Expr, sealing Expr to this package's types.
func (k *KeyValue) exprKey() exprKey { return _exprKey }

// astNode marks KeyValue as a Node, sealing Node to this package's types.
func (k *KeyValue) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the start of the key through the
// end of the value.
func (k *KeyValue) Spanned() Span { return k.Span }

// MutArg is an argument passed to a mut parameter, mut X: X is the place,
// a variable, a field or an element, the parameter refers to for the
// duration of the call. The parser accepts mut before any operand, so
// that one written anywhere but as a call's argument, or before something
// that is not a place, gets a precise error from the type checker.
type MutArg struct {
	X    Expr
	Span Span // the mut keyword through the end of X
}

var _ Expr = (*MutArg)(nil)

// exprKey marks MutArg as an Expr, sealing Expr to this package's types.
func (m *MutArg) exprKey() exprKey { return _exprKey }

// astNode marks MutArg as a Node, sealing Node to this package's types.
func (m *MutArg) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the mut keyword through the end of
// X.
func (m *MutArg) Spanned() Span { return m.Span }

// OwnArg is an argument passed to an own parameter, own X: X names the
// variable or own parameter whose value the call moves into the callee.
// The parser accepts own before any operand, so that one written anywhere
// but as a call's argument, or before something that is not a variable,
// gets a precise error from the type checker.
type OwnArg struct {
	X    Expr
	Span Span // the own keyword through the end of X
}

var _ Expr = (*OwnArg)(nil)

// exprKey marks OwnArg as an Expr, sealing Expr to this package's types.
func (o *OwnArg) exprKey() exprKey { return _exprKey }

// astNode marks OwnArg as a Node, sealing Node to this package's types.
func (o *OwnArg) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the own keyword through the end of
// X.
func (o *OwnArg) Spanned() Span { return o.Span }

// ArgOperand returns what e, a mut or an own argument, is written before:
// X of either, and nil for any other expression.
func ArgOperand(e Expr) Expr {
	switch x := e.(type) {
	case *MutArg:
		return x.X
	case *OwnArg:
		return x.X
	default:
		return nil
	}
}

// FuncLit is a function literal, a closure, written as Go writes one:
// func(a int64, b int64) bool { ... }. Func holds it as a function with no
// name, Closure set, whose Params, Results and Body are the literal's and
// whose Span runs from the keyword func through the closing brace; its
// parameters are declared in the literal's body, as a function's are in
// its, and a return in the body returns from the closure alone. The body
// may name the variables and parameters of the functions and closures it
// is written in, its captures, which it reads and writes by reference,
// never as a copy.
//
// A closure never escapes the call it is passed to: the type checker
// allows one only as an argument to a parameter marked scope (see
// Param.Scope), and reports one anywhere else.
type FuncLit struct {
	Func *Func

	// Type is the closure's function type, which its parameters' types,
	// mut, own or scope where they are so, and its results give, set by
	// the type checker; Invalid before, and when a part was reported.
	Type types.Type

	// Captures are the variables and parameters declared outside the
	// literal that its body names, in the order their first names are
	// written, a nested closure's included, set by the type checker once
	// it has checked the body; nil before, and for a literal it did not
	// check.
	Captures []*Capture `json:"-"`

	Span Span // the keyword func through the closing brace
}

var _ Expr = (*FuncLit)(nil)

// exprKey marks FuncLit as an Expr, sealing Expr to this package's types.
func (f *FuncLit) exprKey() exprKey { return _exprKey }

// astNode marks FuncLit as a Node, sealing Node to this package's types.
func (f *FuncLit) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the keyword func through the
// closing brace.
func (f *FuncLit) Spanned() Span { return f.Span }

// Capture is a variable or a parameter declared outside a closure that the
// closure's body names: Decl, the *Var or *Param, and At, the first name
// of it in the body. Write is whether the body, or a closure nested in it,
// may change it: it assigns it, or a field or an element of it, increments
// or decrements one, or passes one as mut, a mut receiver included; and
// WriteAt is the first place that does, the target or the mut argument's
// place, the zero Span when none does.
type Capture struct {
	Decl    Decl
	At      Span
	WriteAt Span
	Write   bool
}
