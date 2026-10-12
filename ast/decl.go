package ast

import "github.com/tacklang/tack/types"

type declKey struct{}

var _declKey declKey

// Decl is a declaration: something a name can be bound to. Top-level
// functions and parameters are the only declarations today.
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
