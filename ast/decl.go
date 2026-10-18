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
//
// A method, func (b Builder) len() int64, is a Func too, with Method set:
// its receiver is its first parameter, Params[0], which Recv returns, so
// inside its body it is a parameter like any other, and a call passes it
// first. Name is the method's name alone, which lives in its type's
// namespace rather than the package's.
//
// A generic function, func render[W Writer](mut w W, s string), has its
// type parameters in TypeParams, in order, and TypeParamsSpan is the
// brackets around them. It is type-checked once, against its type
// parameters' constraints, and compiled once for each Instance the type
// checker records on the Program.
type Func struct {
	Name       string
	TypeParams []*TypeParam `json:",omitempty"`
	Params     []*Param     // a method's receiver first
	Results    []Expr       // declared result types as written, empty if none
	Body       []Stmt

	// Ensures is the function's contract, the condition written after
	// ensures between its results and its body, func f(d int64) ensures
	// d != 0, or nil when it has none: facts about its parameters the
	// type checker proves hold whenever the function returns, and gives
	// its callers. It is never evaluated, so it has no code, and Walk
	// does not visit it. EnsuresSpan is the ensures keyword, the zero
	// Span when there is none.
	Ensures     Expr `json:",omitempty"`
	EnsuresSpan Span `json:"-"`

	// ResultTypes holds what each of Results denotes, in order. The type
	// checker fills it in; before then it is nil, and an entry it could not
	// determine is Invalid.
	ResultTypes []types.Type

	// RecvType is the declaration of the struct or union type a method's
	// receiver has, set by the resolver once it has found it to be a
	// struct or union type of the method's package; it is nil for a
	// function, and for a method whose receiver type was reported.
	RecvType *TypeDecl `json:"-"`

	// Interface is set only on the stand-in the type checker makes for
	// calling an interface method on a value of a type parameter, w.size()
	// for a w of type W: a method with no body, never declared, whose
	// receiver has type W and whose other parameters and results are the
	// interface method's, which a call is checked against. Code generation
	// calls, in each instance, the method of that name of the struct or
	// union type W stands for.
	Interface *IfaceMethod `json:"-"`

	NameSpan       Span // just the identifier
	TypeParamsSpan Span `json:"-"` // the brackets around the type parameters, if any
	Span           Span // pub or lib, or extern, if written, or the 'func' keyword, through closing brace

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

	// Value is the function's type as a value, func(A) R, which a name
	// naming it has where it is used as one, set by the type checker when
	// such a use is allowed; Invalid otherwise. On the stand-in for a call
	// through a function value it is the value's type (see Call.Dynamic).
	Value types.Type `json:",omitempty"`

	Vis Visibility `json:",omitempty"`

	// Extern is whether the function was declared extern func: its body
	// is written in C, and it has none in Tack.
	Extern bool `json:",omitempty"`

	// NoBody is whether the declaration has no body at all, ending after
	// its signature, as an extern func does, rather than an empty one, {}.
	// Body is empty either way.
	NoBody bool `json:",omitempty"`

	// Method is whether the function is a method, declared with a
	// receiver, which is Params[0].
	Method bool `json:",omitempty"`
}

var _ Decl = (*Func)(nil)

// Recv returns a method's receiver, its first parameter, or nil for a
// function, or for a method declared without one, which the parser has
// reported.
func (f *Func) Recv() *Param {
	if !f.Method || len(f.Params) == 0 {
		return nil
	}
	return f.Params[0]
}

func (f *Func) declKey() declKey { return _declKey }

func (f *Func) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the 'func' keyword, or the
// visibility keyword before it, through the closing brace.
func (f *Func) Spanned() Span { return f.Span }

// Param is one parameter in a function's parameter list. A parameter
// written mut p T is a mutable reference to the caller's variable for the
// duration of a call: Mut is set, and MutSpan is the mut keyword. A
// parameter written args ...T, which only the last parameter may be, is
// variadic: Variadic is set, EllipsisSpan is the ..., TypeExpr is T, the
// element type, and the type checker gives it the type of a view of T,
// []T, which a call fills with the values it passes for it. A parameter
// written own p T takes its argument's value from the caller, who writes
// own x and cannot use x again: Own is set, and OwnSpan is the own keyword;
// the function owns the value, an immutable local it frees when it ends.
// Any other parameter is an immutable copy of its argument.
type Param struct {
	TypeExpr     Expr // the type as written, e.g. *Ident{"int64"}; a variadic's element type
	Name         string
	NameSpan     Span
	MutSpan      Span       `json:"-"`
	OwnSpan      Span       `json:"-"`
	EllipsisSpan Span       `json:"-"`
	Span         Span       // mut or own, if written, or the name, through the end of the type
	Type         types.Type // filled in by the type checker, Invalid before
	Mut          bool       `json:",omitempty"`
	Own          bool       `json:",omitempty"`
	Variadic     bool       `json:",omitempty"`
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
	// type checker: a constant.Int, a string, a bool, or, for an array or
	// a struct, a []any of its elements' or fields' values in order. It is
	// nil for a local, and for an array or struct global declared without
	// a value, which starts at its zero value.
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

// TypeDecl is a type declaration at package level: type Name struct { ...
// }, which declares a struct type, type Name interface { ... }, which
// declares an interface, or type Name union { ... }, which declares a
// union type. Other named types are not supported yet, so TypeExpr is
// always a *StructType, an *InterfaceType (see Interface) or a
// *UnionType (see Union). An interface is no type of the table, so its
// Type stays Invalid.
type TypeDecl struct {
	TypeExpr Expr // the type as written, a *StructType, an *InterfaceType or a *UnionType

	// Methods are the methods declared on the type, in program order, as
	// the resolver records them: each one it accepted, whose receiver has
	// the type and whose name no field or other method of the type has.
	// They are left out of the JSON dump, which shows each with its file.
	Methods []*Func `json:"-"`

	Name     string
	NameSpan Span // just the identifier
	Span     Span // pub or lib, if written, or the 'type' keyword, through the end of the type

	// Vis is who may name the type; VisSpan is the keyword that gave it,
	// the zero Span for Private.
	VisSpan Span       `json:"-"`
	Type    types.Type // the declared type, set by the type checker
	Vis     Visibility `json:",omitempty"`
}

// Method returns the method of the type named name, or nil when it has
// none.
func (d *TypeDecl) Method(name string) *Func {
	for _, m := range d.Methods {
		if m.Name == name {
			return m
		}
	}
	return nil
}

var _ Decl = (*TypeDecl)(nil)

func (d *TypeDecl) declKey() declKey { return _declKey }

func (d *TypeDecl) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the 'type' keyword, or the
// visibility keyword before it, through the end of the type.
func (d *TypeDecl) Spanned() Span { return d.Span }
