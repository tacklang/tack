package ast

import "github.com/tacklang/tack/types"

type declKey struct{}

var _declKey declKey

// Decl is a declaration: something a name can be bound to. Functions,
// parameters, variables, and types are the declarations today.
type Decl interface {
	declKey() declKey
	Spanned() Span

	Node
}

// Func is a function declaration, with its parameters, its result types, and
// its body. A function may declare no result, one, or several; a call to one
// with several gives all of them at once, to be bound by a MultiVar.
type Func struct {
	Name    string
	Params  []*Param
	Results []Expr // declared result types as written, empty if none
	Body    []Stmt

	// ResultTypes holds what each of Results denotes, in order. The type
	// checker fills it in; before then it is nil, and an entry it could not
	// determine is Invalid.
	ResultTypes []types.Type

	NameSpan Span // just the identifier
	Span     Span // pub or lib, or extern, if written, or the 'func' keyword, through closing brace

	// Vis is who may call the function by name; VisSpan is the keyword
	// that gave it, the zero Span for Private.
	VisSpan Span `json:"-"`

	// CName is the C symbol an extern func's body is defined under: the
	// string extern("name") gives, or the function's own name, and empty
	// for a function that is not extern. ExternSpan is the extern keyword,
	// through the closing parenthesis if a C name is given, and CNameSpan
	// that string, the zero Span when there is none.
	CName      string `json:",omitempty"`
	ExternSpan Span   `json:"-"`
	CNameSpan  Span   `json:"-"`

	Vis Visibility `json:",omitempty"`

	// Extern is whether the function was declared extern func: its body
	// is written in C, and it has none in Tack.
	Extern bool `json:",omitempty"`

	// NoBody is whether the declaration has no body at all, ending after
	// its signature, as an extern func does, rather than an empty one, {}.
	// Body is empty either way.
	NoBody bool `json:",omitempty"`
}

var _ Decl = (*Func)(nil)

func (f *Func) declKey() declKey { return _declKey }

func (f *Func) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the 'func' keyword, or the
// visibility keyword before it, through the closing brace.
func (f *Func) Spanned() Span { return f.Span }

// Param is one parameter in a function's parameter list. A parameter
// written mut p T is a mutable reference to the caller's variable for the
// duration of a call: Mut is set, and MutSpan is the mut keyword. Any
// other parameter is an immutable copy of its argument.
type Param struct {
	TypeExpr Expr // the type as written, e.g. *Ident{"int64"}
	Name     string
	NameSpan Span
	MutSpan  Span       `json:"-"`
	Span     Span       // mut, if written, or the name, through the end of the type
	Type     types.Type // filled in by the type checker, Invalid before
	Mut      bool       `json:",omitempty"`
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
	TypeExpr Expr // the declared type as written, nil if inferred from Value
	Value    Expr // the initial value, nil if the variable starts at its zero value

	// Const is a package-level variable's initial value, computed by the
	// type checker: an int64, a string, or a bool. It is nil for a local.
	Const any

	Name     string
	NameSpan Span // just the identifier
	Span     Span // first token through the end of the declaration

	// Vis is who may use a package-level variable by name, always Private
	// for a local; VisSpan is the keyword that gave it, the zero Span for
	// Private. Span starts at that keyword.
	VisSpan Span `json:"-"`

	Type    types.Type // filled in by the type checker, Invalid before
	Mutable bool       // declared with mut
	Vis     Visibility `json:",omitempty"`
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

// Import is an import declaration, import x from "path": it declares the
// name x in its file, standing for the package whose import path is path.
// An import is not one of a file's Decls; it is a declaration so that a
// name can be bound to it.
type Import struct {
	// Package is the package Path names. The loader sets it; it is nil
	// before, and left out of the JSON dump, since the package is dumped
	// on its own.
	Package *Package `json:"-"`

	Name     string
	Path     string // the path as the string literal gives it
	NameSpan Span
	PathSpan Span // the string literal
	Span     Span // 'import' keyword through the path
}

var _ Decl = (*Import)(nil)

func (i *Import) declKey() declKey { return _declKey }

func (i *Import) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the 'import' keyword through the
// path.
func (i *Import) Spanned() Span { return i.Span }

// TypeDecl is a type declaration at package level, type Name struct { ... },
// which declares a struct type. Other named types are not supported yet, so
// TypeExpr is always a *StructType.
type TypeDecl struct {
	TypeExpr Expr // the type as written, a *StructType
	Name     string
	NameSpan Span // just the identifier
	Span     Span // pub or lib, if written, or the 'type' keyword, through the end of the type

	// Vis is who may name the type; VisSpan is the keyword that gave it,
	// the zero Span for Private.
	VisSpan Span       `json:"-"`
	Type    types.Type // the declared type, set by the type checker
	Vis     Visibility `json:",omitempty"`
}

var _ Decl = (*TypeDecl)(nil)

func (d *TypeDecl) declKey() declKey { return _declKey }

func (d *TypeDecl) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the 'type' keyword, or the
// visibility keyword before it, through the end of the type.
func (d *TypeDecl) Spanned() Span { return d.Span }
