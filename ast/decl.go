package ast

import "github.com/tacklang/tack/types"

type declKey struct{}

var _declKey declKey

// Decl is a declaration: something a name can be bound to. Functions,
// parameters, and variables are the declarations today.
type Decl interface {
	declKey() declKey
	Spanned() Span

	Node
}

// Func is a function declaration, with its parameters, optional result type,
// and body.
type Func struct {
	Result     Expr // declared result type as written, nil if none
	Name       string
	Params     []*Param
	Body       []Stmt
	NameSpan   Span       // just the identifier
	Span       Span       // 'func' keyword through closing brace
	ResultKind types.Kind // filled in by the type checker, Invalid before
}

var _ Decl = (*Func)(nil)

func (f *Func) declKey() declKey { return _declKey }

func (f *Func) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the 'func' keyword through the
// closing brace.
func (f *Func) Spanned() Span { return f.Span }

// Param is one parameter in a function's parameter list.
type Param struct {
	Type     Expr // the type as written, e.g. *Ident{"int"}
	Name     string
	NameSpan Span
	Span     Span
	Kind     types.Kind // filled in by the type checker, Invalid before
}

// A parameter is a declaration so that an Ident referring to one can point at
// it through the same Def field a call uses for a function.
var _ Decl = (*Param)(nil)

func (p *Param) declKey() declKey { return _declKey }

func (p *Param) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the parameter's name and type.
func (p *Param) Spanned() Span { return p.Span }

// Var is a variable declaration: x := v, var x T = v, var x = v, or the same
// with mut in place of var for a mutable variable, where mut x T may also
// leave out the value to start from the type's zero value. Inside a function
// body it is also a statement.
type Var struct {
	Type  Expr // the declared type as written, nil if inferred from Value
	Value Expr // the initial value, nil if the variable starts at its zero value

	// Const is a package-level variable's initial value, computed by the
	// type checker: an int64, a string, or a bool. It is nil for a local.
	Const any

	Name     string
	NameSpan Span       // just the identifier
	Span     Span       // first token through the end of the declaration
	Kind     types.Kind // filled in by the type checker, Invalid before
	Mutable  bool       // declared with mut
}

// A variable is a declaration so that an Ident can point at it through Def,
// and a statement so that it can appear in a function body.
var (
	_ Decl = (*Var)(nil)
	_ Stmt = (*Var)(nil)
)

func (v *Var) declKey() declKey { return _declKey }

func (v *Var) stmtKey() stmtKey { return _stmtKey }

func (v *Var) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the whole declaration.
func (v *Var) Spanned() Span { return v.Span }
