package ast

import "github.com/tacklang/tack/types"

// TypeParam is one type parameter of a generic function: W in func
// render[W Writer](mut w W, s string). It names a type within its
// function, its signature and its body, standing for whichever struct
// type a call passes, which must satisfy Constraint, the name of an
// interface, as written. It is a declaration so that a name written as a
// type can be bound to it, as one is to a TypeDecl.
type TypeParam struct {
	Constraint Expr // the constraint as written: an interface's name, qualified or not

	// Iface is the interface Constraint names, set by the type checker;
	// it is nil before, and when the constraint was reported. It is left
	// out of the JSON dump, which shows the interface where it is
	// declared.
	Iface *TypeDecl `json:"-"`

	Name     string
	NameSpan Span
	Span     Span // the name through the end of the constraint

	// Type is the type the parameter stands for in its function, one of
	// its own in the type table, set by the type checker; Invalid before.
	Type types.Type
}

var _ Decl = (*TypeParam)(nil)

func (p *TypeParam) declKey() declKey { return _declKey }

func (p *TypeParam) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the parameter's name and
// constraint.
func (p *TypeParam) Spanned() Span { return p.Span }

// InterfaceType is an interface type as written, interface { mut write(b
// []uint8) error; size() int64 }: the methods a type must have to satisfy
// it, in the order they are declared. It appears only in a TypeDecl, and
// an interface is used only as a type parameter's constraint.
type InterfaceType struct {
	Methods []*IfaceMethod
	Span    Span // the 'interface' keyword through the closing brace
}

var _ Expr = (*InterfaceType)(nil)

func (t *InterfaceType) exprKey() exprKey { return _exprKey }

func (t *InterfaceType) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range from the 'interface' keyword through
// the closing brace.
func (t *InterfaceType) Spanned() Span { return t.Span }

// Method returns the method of the interface named name, or nil when it
// has none.
func (t *InterfaceType) Method(name string) *IfaceMethod {
	for _, m := range t.Methods {
		if m.Name == name {
			return m
		}
	}
	return nil
}

// IfaceMethod is one method an interface asks for: its name, whether it
// takes its receiver as mut, written mut before the name, and its
// parameters and results, as a function's are written, with no receiver
// and no body. Its name lives in its interface's namespace.
type IfaceMethod struct {
	Params  []*Param
	Results []Expr // the result types as written, empty if none

	// ResultTypes holds what each of Results denotes, in order, set by
	// the type checker, as Func.ResultTypes is.
	ResultTypes []types.Type

	Name     string
	NameSpan Span
	MutSpan  Span `json:"-"`
	Span     Span // mut, if written, or the name, through the end of the results
	Mut      bool `json:",omitempty"`
}

var _ Decl = (*IfaceMethod)(nil)

func (m *IfaceMethod) declKey() declKey { return _declKey }

func (m *IfaceMethod) astNode() nodeKey { return _nodeKey }

// Spanned returns the source range of the method's declaration.
func (m *IfaceMethod) Spanned() Span { return m.Span }

// Interface returns the interface type d declares, or nil when d declares
// a struct type.
func (d *TypeDecl) Interface() *InterfaceType {
	it, _ := d.TypeExpr.(*InterfaceType)
	return it
}

// Generic reports whether fn is a generic function, declared with type
// parameters.
func (fn *Func) Generic() bool {
	return len(fn.TypeParams) > 0
}

// Instance is one instance of a generic function: the function, compiled
// with each of its type parameters, in order, standing for the type in
// TypeArgs at the same position.
type Instance struct {
	Func     *Func
	TypeArgs []types.Type
}

// CallResults returns the types of the values call gives: its callee's
// result types, a function's or the method the checker found, with each
// of a generic callee's type parameters replaced by the type argument the
// checker inferred for it at this call, as Call.ResultTypes records them,
// through every type made of one; before the checker records those, a
// result that is a whole type parameter is replaced by the type in
// Call.TypeArgs, or by Invalid when it inferred none, which it has
// reported. It returns nil for a call to a builtin or a conversion, which
// have no callee.
func CallResults(call *Call) []types.Type {
	fn := Callee(call)
	if fn == nil {
		return nil
	}
	if !fn.Generic() {
		return fn.ResultTypes
	}
	if call.ResultTypes != nil {
		return call.ResultTypes
	}
	out := make([]types.Type, len(fn.ResultTypes))
	for i, typ := range fn.ResultTypes {
		out[i] = Substitute(fn, call.TypeArgs, typ)
	}
	return out
}

// Substitute returns typ with each of fn's type parameters replaced by
// the type at its position in args: the type argument when typ is one of
// them, or Invalid when args has none for it, and typ itself otherwise. A
// type made of a type parameter, a list, a view, a map or a function type,
// needs a table to rewrite, so the type checker records a call's results
// rewritten in Call.ResultTypes, which CallResults gives instead.
func Substitute(fn *Func, args []types.Type, typ types.Type) types.Type {
	if typ == types.Invalid {
		return typ
	}
	for i, tp := range fn.TypeParams {
		if tp.Type != typ {
			continue
		}
		if i < len(args) {
			return args[i]
		}
		return types.Invalid
	}
	return typ
}
