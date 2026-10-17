package ast

import (
	"encoding/json"
	"fmt"

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

func (c *Call) exprKey() exprKey { return _exprKey }

func (c *Call) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the callee through the closing
// parenthesis.
func (c *Call) Spanned() Span { return c.Span }

// Receiver returns what a method call is called on: x in x.m(...), or the
// *MutArg mut x in mut x.m(...). It returns nil for a call that is not a
// method call, a union's construction, Expr.int(5), among them, whose
// selector's operand names a type (see Construction).
func (c *Call) Receiver() Expr {
	if sel, ok := c.Fn.(*Selector); ok {
		if _, _, _, built := Construction(c); built {
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
// to, or the method the type checker found for a method call. It returns
// nil for a builtin, whose declaration is a nil *Func, for a conversion,
// and for a method call the type checker has not found a method for.
func Callee(call *Call) *Func {
	if call.Method != nil {
		return call.Method
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
	Def     Decl // filled in by the resolver, nil before
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

func (i *Ident) exprKey() exprKey { return _exprKey }

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

// NoErrorLit is the literal noerror: the error value that is no error,
// with code 0 and an empty message, the zero value of type error.
type NoErrorLit struct {
	Span Span
}

var _ Expr = (*NoErrorLit)(nil)

func (n *NoErrorLit) exprKey() exprKey { return _exprKey }

func (n *NoErrorLit) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the keyword.
func (n *NoErrorLit) Spanned() Span { return n.Span }

// CAllocatorLit is the literal c_allocator: the allocator, of type
// Allocator, that lowers to C's malloc, realloc and free.
type CAllocatorLit struct {
	Span Span
}

var _ Expr = (*CAllocatorLit)(nil)

func (c *CAllocatorLit) exprKey() exprKey { return _exprKey }

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
)

var binaryOpNames = map[BinaryOp]string{
	Add: "+", Sub: "-", Mul: "*", Div: "/", Rem: "%",
	Eq: "==", Ne: "!=", Lt: "<", Le: "<=", Gt: ">", Ge: ">=",
	And: "&&", Or: "||",
	BitAnd: "&", BitOr: "|", BitXor: "^", AndNot: "&^", Shl: "<<", Shr: ">>",
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

// IsInteger reports whether the operator works on integers and yields one:
// an arithmetic, a bitwise or a shift operator.
func (o BinaryOp) IsInteger() bool { return o.IsArithmetic() || o.IsBitwise() || o.IsShift() }

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
	// Complement is ^, which flips every bit of an int: ^x means x ^ m,
	// with m all ones for an unsigned type and -1 for a signed one.
	Complement
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
// it is one: an integer literal's recorded type, or untyped before the
// checker reaches it, and every other literal's fixed type.
func literalType(expr Expr) (types.Type, bool) {
	switch e := expr.(type) {
	case *StrLit:
		return types.String, true
	case *IntLit:
		if e.Type == types.Invalid {
			return types.UntypedInt, true
		}
		return e.Type, true
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

// recordedType returns the type the type checker recorded on an operator
// expression, a composite literal, an element, a slice, a field or a type
// assertion, the type it asserts, the type of the place a mut argument
// names, or Invalid for any other expression.
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
	case *MutArg:
		return TypeOf(e.X)
	case *TypeAssert:
		return e.Type
	default:
		return types.Invalid
	}
}

// identType returns the type of the parameter or variable a name is bound
// to, which is all an expression can name today. A name bound to a type
// has no value, so its type is Invalid.
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

func (t *TypeAssert) exprKey() exprKey { return _exprKey }

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

func (a *ArrayType) exprKey() exprKey { return _exprKey }

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

func (v *ViewType) exprKey() exprKey { return _exprKey }

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

func (l *ListType) exprKey() exprKey { return _exprKey }

func (l *ListType) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the keyword list through the
// closing bracket.
func (l *ListType) Spanned() Span { return l.Span }

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

func (s *Slice) exprKey() exprKey { return _exprKey }

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

func (s *StructType) exprKey() exprKey { return _exprKey }

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
// (see Construction); Type is then the union's.
type Selector struct {
	X        Expr
	Name     string
	NameSpan Span // just the field name
	Span     Span // the start of X through the field name

	// Type is the field's type, set by the type checker.
	Type types.Type
}

var _ Expr = (*Selector)(nil)

func (s *Selector) exprKey() exprKey { return _exprKey }

func (s *Selector) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the start of X through the field
// name.
func (s *Selector) Spanned() Span { return s.Span }

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

func (k *KeyValue) exprKey() exprKey { return _exprKey }

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

func (m *MutArg) exprKey() exprKey { return _exprKey }

func (m *MutArg) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the mut keyword through the end of
// X.
func (m *MutArg) Spanned() Span { return m.Span }
